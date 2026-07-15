package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/opencuttles/opencuttles/backend/internal/domain"
)

type fakeSchedStore struct {
	due       []domain.TestCycle
	instances []domain.Instance
	scheduled int
	lastNext  *time.Time
}

func (f *fakeSchedStore) ListDueCycles(context.Context, time.Time) ([]domain.TestCycle, error) {
	return f.due, nil
}
func (f *fakeSchedStore) SetCycleSchedule(_ context.Context, _ string, _, next *time.Time) error {
	f.scheduled++
	f.lastNext = next
	return nil
}
func (f *fakeSchedStore) ListInstances(context.Context) ([]domain.Instance, error) {
	return f.instances, nil
}

type fakeExec struct{ starts int }

func (f *fakeExec) Start(context.Context, string, string, string, string) (domain.CycleRun, error) {
	f.starts++
	return domain.CycleRun{}, nil
}

func TestSchedulerFiresDueCycle(t *testing.T) {
	now := time.Now().UTC()
	store := &fakeSchedStore{
		due:       []domain.TestCycle{{ID: "c1", Cron: "*/5 * * * *", Platform: domain.PlatformWindows, Enabled: true}},
		instances: []domain.Instance{{ID: "w1", Platform: domain.PlatformWindows, State: domain.StateOnline}},
	}
	exec := &fakeExec{}
	New(store, exec, nil).tick(context.Background())

	if exec.starts != 1 {
		t.Fatalf("executor starts = %d, want 1", exec.starts)
	}
	if store.scheduled != 1 || store.lastNext == nil || !store.lastNext.After(now) {
		t.Fatalf("next run not advanced to the future: %+v", store.lastNext)
	}
}

func TestSchedulerAdvancesWithoutTarget(t *testing.T) {
	// No matching online device: the cycle must still advance (not spin), but not
	// start a run.
	store := &fakeSchedStore{
		due:       []domain.TestCycle{{ID: "c1", Cron: "@hourly", Platform: domain.PlatformWindows, Enabled: true}},
		instances: nil,
	}
	exec := &fakeExec{}
	New(store, exec, nil).tick(context.Background())

	if exec.starts != 0 {
		t.Fatalf("executor should not start without a target, got %d", exec.starts)
	}
	if store.scheduled != 1 {
		t.Fatalf("schedule should still advance, scheduled = %d", store.scheduled)
	}
}
