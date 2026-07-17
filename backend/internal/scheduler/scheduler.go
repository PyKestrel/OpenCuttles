package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/opencuttles/opencuttles/backend/internal/domain"
)

// Store is the persistence surface the scheduler needs.
type Store interface {
	ListDueCycles(ctx context.Context, now time.Time) ([]domain.TestCycle, error)
	SetCycleSchedule(ctx context.Context, id string, lastRun, nextRun *time.Time) error
	ListInstances(ctx context.Context) ([]domain.Instance, error)
}

// Executor starts a cycle run (satisfied by *scenario.CycleExecutor).
type Executor interface {
	Start(ctx context.Context, cycleID, instanceID, trigger, buildID string) (domain.CycleRun, error)
}

// Scheduler is a single background loop that fires cron-scheduled test cycles.
type Scheduler struct {
	store    Store
	executor Executor
	logger   *slog.Logger
	interval time.Duration
}

func New(store Store, executor Executor, logger *slog.Logger) *Scheduler {
	return &Scheduler{store: store, executor: executor, logger: logger, interval: 30 * time.Second}
}

// Run ticks until ctx is cancelled, triggering due cycles. Cron/next-run state
// lives on the cycle rows, so schedules survive restarts (overdue cycles fire on
// the next tick, then advance).
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	s.tick(ctx) // fire any already-due cycles promptly on boot
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *Scheduler) tick(ctx context.Context) {
	now := time.Now().UTC()
	due, err := s.store.ListDueCycles(ctx, now)
	if err != nil {
		s.warn("list due cycles failed", "error", err)
		return
	}
	for _, cycle := range due {
		// Always advance next-run first so a run that can't start doesn't spin.
		// The cycle's timezone (empty = UTC) governs its wall-clock fields.
		next, nerr := NextIn(cycle.Cron, cycle.Timezone, now)
		var nextPtr *time.Time
		if nerr == nil {
			nextPtr = &next
		}
		last := now
		if err := s.store.SetCycleSchedule(ctx, cycle.ID, &last, nextPtr); err != nil {
			s.warn("advance schedule failed", "cycle", cycle.ID, "error", err)
		}

		target, terr := s.resolveTarget(ctx, cycle)
		if terr != nil {
			s.warn("skip due cycle: no target device", "cycle", cycle.ID, "platform", cycle.Platform, "error", terr)
			continue
		}
		if _, err := s.executor.Start(ctx, cycle.ID, target, domain.CycleTriggerCron, cycle.BuildID); err != nil {
			s.warn("start scheduled cycle failed", "cycle", cycle.ID, "error", err)
		}
	}
}

func (s *Scheduler) resolveTarget(ctx context.Context, cycle domain.TestCycle) (string, error) {
	instances, err := s.store.ListInstances(ctx)
	if err != nil {
		return "", err
	}
	for _, inst := range instances {
		p := inst.Platform
		if p == "" {
			p = domain.PlatformAndroid
		}
		if p != cycle.Platform {
			continue
		}
		if inst.State == domain.StateRunning || inst.State == domain.StateOnline {
			return inst.ID, nil
		}
	}
	return "", errNoTarget
}

var errNoTarget = errorString("no running/online device matches the cycle platform")

type errorString string

func (e errorString) Error() string { return string(e) }

func (s *Scheduler) warn(msg string, kv ...any) {
	if s.logger != nil {
		s.logger.Warn(msg, kv...)
	}
}
