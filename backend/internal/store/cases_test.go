package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/opencuttles/opencuttles/backend/internal/domain"
)

func openTestStore(t *testing.T) *SQLite {
	t.Helper()
	tempDir := t.TempDir()
	t.Setenv("OPENCUTTLES_IMAGE_ROOT", filepath.Join(tempDir, "images"))
	db, err := OpenSQLite(filepath.Join(tempDir, "opencuttles.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestTestCaseCycleRunRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := openTestStore(t)

	// Cases with structured steps.
	c1, err := db.CreateTestCase(ctx, domain.TestCase{
		Summary:      "Login works",
		Precondition: "App installed",
		Priority:     "high",
		Labels:       []string{"smoke", "auth"},
		FolderPath:   "Auth/Login",
		Steps: []domain.TestStep{
			{Action: "Open the app", Expected: "Login screen is shown"},
			{Action: "Enter credentials and submit", TestData: "user/pass", Expected: "Home screen is shown"},
		},
	})
	if err != nil {
		t.Fatalf("create case: %v", err)
	}
	if c1.ID == "" || c1.Steps[1].Index != 1 {
		t.Fatalf("case not initialized: %+v", c1)
	}
	c2, _ := db.CreateTestCase(ctx, domain.TestCase{Summary: "Logout works", FolderPath: "Auth/Login"})

	got, err := db.GetTestCase(ctx, c1.ID)
	if err != nil || got.Summary != "Login works" || len(got.Steps) != 2 || got.Steps[1].TestData != "user/pass" {
		t.Fatalf("get case round-trip failed: %+v err=%v", got, err)
	}
	if len(got.Labels) != 2 {
		t.Fatalf("labels round-trip: %+v", got.Labels)
	}

	folders, err := db.ListCaseFolders(ctx)
	if err != nil || len(folders) != 1 || folders[0] != "Auth/Login" {
		t.Fatalf("folders = %v err=%v", folders, err)
	}

	// Cycle referencing both cases.
	cyc, err := db.CreateTestCycle(ctx, domain.TestCycle{
		Name:     "Regression",
		Platform: domain.PlatformWindows,
		CaseIDs:  []string{c1.ID, c2.ID},
		Enabled:  true,
	})
	if err != nil {
		t.Fatalf("create cycle: %v", err)
	}
	if len(cyc.CaseIDs) != 2 || cyc.Platform != domain.PlatformWindows {
		t.Fatalf("cycle not persisted: %+v", cyc)
	}

	// ListDueCycles: not due until a cron + past next_run_at is set.
	due, _ := db.ListDueCycles(ctx, time.Now())
	if len(due) != 0 {
		t.Fatalf("expected no due cycles, got %d", len(due))
	}
	cyc.Cron = "*/5 * * * *"
	if err := db.UpdateTestCycle(ctx, cyc); err != nil {
		t.Fatalf("update cycle: %v", err)
	}
	past := time.Now().Add(-time.Minute)
	if err := db.SetCycleSchedule(ctx, cyc.ID, nil, &past); err != nil {
		t.Fatalf("set schedule: %v", err)
	}
	due, _ = db.ListDueCycles(ctx, time.Now())
	if len(due) != 1 || due[0].ID != cyc.ID {
		t.Fatalf("expected 1 due cycle, got %d", len(due))
	}

	// Cycle run + per-case runs.
	cr, err := db.CreateCycleRun(ctx, domain.CycleRun{CycleID: cyc.ID, Trigger: domain.CycleTriggerManual, InstanceID: "win_1"})
	if err != nil {
		t.Fatalf("create cycle run: %v", err)
	}
	// A running cycle run blocks the due list (overlap guard).
	due, _ = db.ListDueCycles(ctx, time.Now())
	if len(due) != 0 {
		t.Fatalf("running cycle run should block due list, got %d", len(due))
	}

	run1, _ := db.CreateCaseRun(ctx, cr.ID, c1.ID, "win_1")
	if err := db.AppendStep(ctx, run1.ID, domain.StepResult{Text: "Open the app", Status: domain.StepPass, Pass: true}); err != nil {
		t.Fatalf("append step: %v", err)
	}
	_, _ = db.CreateCaseRun(ctx, cr.ID, c2.ID, "win_1")

	children, err := db.ListTestRunsByCycleRun(ctx, cr.ID)
	if err != nil || len(children) != 2 {
		t.Fatalf("expected 2 child runs, got %d err=%v", len(children), err)
	}
	// Case summary hydrated as the run name; appended step present. Look up by
	// case id since same-tick runs can share a start timestamp.
	byCase := map[string]domain.TestRun{}
	for _, r := range children {
		byCase[r.CaseID] = r
	}
	if c := byCase[c1.ID]; c.TestName != "Login works" || len(c.Steps) != 1 || c.Steps[0].Status != domain.StepPass {
		t.Fatalf("case1 run wrong: %+v", c)
	}
	if c := byCase[c2.ID]; c.TestName != "Logout works" {
		t.Fatalf("case2 run wrong: %+v", c)
	}

	cr.Status = "passed"
	cr.Totals = domain.CycleTotals{Cases: 2, Pass: 2}
	fin := time.Now().UTC()
	cr.FinishedAt = &fin
	if err := db.UpdateCycleRun(ctx, cr); err != nil {
		t.Fatalf("update cycle run: %v", err)
	}
	gotRun, err := db.GetCycleRun(ctx, cr.ID)
	if err != nil || gotRun.Status != "passed" || gotRun.Totals.Pass != 2 || gotRun.CycleName != "Regression" {
		t.Fatalf("cycle run round-trip: %+v err=%v", gotRun, err)
	}

	// Builds.
	b, err := db.CreateBuild(ctx, domain.Build{Platform: domain.PlatformWindows, Filename: "app.msi", Path: "/tmp/app.msi", SizeBytes: 1024})
	if err != nil || b.Status != "uploaded" {
		t.Fatalf("create build: %+v err=%v", b, err)
	}
	builds, _ := db.ListBuilds(ctx, domain.PlatformWindows)
	if len(builds) != 1 {
		t.Fatalf("expected 1 build, got %d", len(builds))
	}
	if builds2, _ := db.ListBuilds(ctx, domain.PlatformAndroid); len(builds2) != 0 {
		t.Fatalf("android build filter should be empty")
	}
	cyclesForBuild, _ := db.ListCyclesForBuild(ctx, domain.PlatformWindows)
	if len(cyclesForBuild) != 0 {
		t.Fatalf("cycle has on_new_build=false, should not match")
	}
}

func TestBulkCreateCases(t *testing.T) {
	ctx := context.Background()
	db := openTestStore(t)
	n, err := db.BulkCreateCases(ctx, []domain.TestCase{
		{Summary: "A", Steps: []domain.TestStep{{Action: "do a"}}},
		{Summary: ""}, // skipped (no summary)
		{Summary: "B"},
	})
	if err != nil || n != 2 {
		t.Fatalf("bulk create = %d err=%v", n, err)
	}
	all, _ := db.ListTestCases(ctx)
	if len(all) != 2 {
		t.Fatalf("expected 2 cases, got %d", len(all))
	}
}
