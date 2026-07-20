package store

import (
	"context"
	"testing"
	"time"

	"github.com/opencuttles/opencuttles/backend/internal/domain"
)

// The failure this guards: a restart mid-run strands cycle_runs.status='running',
// and ListDueCycles excludes cycles with a run in flight — so the schedule stops
// firing permanently, silently, with no error surfaced anywhere.
func TestFailStrandedCycleRunsUnblocksSchedule(t *testing.T) {
	ctx := context.Background()
	db := openTestStore(t)

	tc, err := db.CreateTestCase(ctx, domain.TestCase{Summary: "Login works"})
	if err != nil {
		t.Fatalf("create case: %v", err)
	}
	due := time.Now().UTC().Add(-time.Minute)
	cycle, err := db.CreateTestCycle(ctx, domain.TestCycle{
		Name:    "Nightly",
		Enabled: true,
		Cron:    "0 2 * * *",
		CaseIDs: []string{tc.ID},
	})
	if err != nil {
		t.Fatalf("create cycle: %v", err)
	}
	if err := db.SetCycleSchedule(ctx, cycle.ID, nil, &due); err != nil {
		t.Fatalf("set schedule: %v", err)
	}

	// Simulate a run that was interrupted: cycle run and its case run both left
	// 'running', exactly as an abrupt shutdown leaves them.
	run, err := db.CreateCycleRun(ctx, domain.CycleRun{CycleID: cycle.ID, Trigger: "cron"})
	if err != nil {
		t.Fatalf("create cycle run: %v", err)
	}
	caseRun, err := db.CreateCaseRun(ctx, run.ID, tc.ID, "")
	if err != nil {
		t.Fatalf("create case run: %v", err)
	}

	// Precondition: the stranded run really does block the schedule.
	blocked, err := db.ListDueCycles(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("list due: %v", err)
	}
	if len(blocked) != 0 {
		t.Fatalf("expected the stranded run to block scheduling, got %d due cycles", len(blocked))
	}

	swept, err := db.FailStrandedCycleRuns(ctx, "interrupted by API restart")
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if swept != 1 {
		t.Fatalf("swept = %d, want 1", swept)
	}

	// The schedule is live again.
	dueNow, err := db.ListDueCycles(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("list due after sweep: %v", err)
	}
	if len(dueNow) != 1 || dueNow[0].ID != cycle.ID {
		t.Fatalf("cycle still blocked after sweep: %+v", dueNow)
	}

	// The cycle run reads failed and closed.
	got, err := db.GetCycleRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get cycle run: %v", err)
	}
	if got.Status != "failed" {
		t.Fatalf("cycle run status = %q, want failed", got.Status)
	}
	if got.FinishedAt == nil {
		t.Fatal("cycle run left without a finished_at")
	}

	// And so does the child case run — a 'failed' cycle containing a still
	// 'running' case would be an inconsistent report.
	caseRuns, err := db.ListTestRunsByCycleRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("list case runs: %v", err)
	}
	if len(caseRuns) != 1 || caseRuns[0].ID != caseRun.ID {
		t.Fatalf("unexpected case runs: %+v", caseRuns)
	}
	if caseRuns[0].Status != "failed" {
		t.Fatalf("case run status = %q, want failed", caseRuns[0].Status)
	}
	if caseRuns[0].Error == "" {
		t.Fatal("case run should carry the interruption reason")
	}
}

// The sweep must not touch runs that already completed, and must be safe to
// re-run (it executes on every boot).
func TestFailStrandedCycleRunsLeavesFinishedRunsAlone(t *testing.T) {
	ctx := context.Background()
	db := openTestStore(t)

	cycle, err := db.CreateTestCycle(ctx, domain.TestCycle{Name: "Nightly", Enabled: true})
	if err != nil {
		t.Fatalf("create cycle: %v", err)
	}
	run, err := db.CreateCycleRun(ctx, domain.CycleRun{CycleID: cycle.ID, Trigger: "manual"})
	if err != nil {
		t.Fatalf("create cycle run: %v", err)
	}
	finishedAt := time.Now().UTC()
	run.Status = "passed"
	run.FinishedAt = &finishedAt
	if err := db.UpdateCycleRun(ctx, run); err != nil {
		t.Fatalf("finish run: %v", err)
	}

	swept, err := db.FailStrandedCycleRuns(ctx, "interrupted by API restart")
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if swept != 0 {
		t.Fatalf("swept = %d, want 0 — a completed run was rewritten", swept)
	}
	got, err := db.GetCycleRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if got.Status != "passed" {
		t.Fatalf("completed run status changed to %q", got.Status)
	}

	// Idempotent on a clean database.
	if swept, err := db.FailStrandedCycleRuns(ctx, "again"); err != nil || swept != 0 {
		t.Fatalf("second sweep = %d err=%v, want 0/nil", swept, err)
	}
}
