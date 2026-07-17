package insights

import (
	"testing"
	"time"

	"github.com/opencuttles/opencuttles/backend/internal/domain"
)

var base = time.Date(2026, 7, 17, 9, 0, 0, 0, time.UTC)

// run builds a finished case-run with the given verdict, n minutes after base.
func run(caseID, summary string, n int, status string) domain.TestRun {
	start := base.Add(time.Duration(n) * time.Minute)
	fin := start.Add(30 * time.Second)
	r := domain.TestRun{
		ID: caseID + "-" + status + "-" + time.Duration(n).String(), CaseID: caseID, TestName: summary,
		StartedAt: start, FinishedAt: &fin,
	}
	switch status {
	case domain.StepPass:
		r.Passed = true
		r.Status = "passed"
		r.Steps = []domain.StepResult{{Status: domain.StepPass}}
	case domain.StepFail:
		r.Status = "failed"
		r.Steps = []domain.StepResult{{Status: domain.StepFail}}
	default:
		r.Status = "failed"
		r.Steps = []domain.StepResult{{Status: domain.StepBlocked}}
	}
	return r
}

func find(t *testing.T, hs []CaseHealth, caseID string) CaseHealth {
	t.Helper()
	for _, h := range hs {
		if h.CaseID == caseID {
			return h
		}
	}
	t.Fatalf("case %q missing from health", caseID)
	return CaseHealth{}
}

// A case that alternates pass/fail is flaky; one that fails every time is
// broken, not flaky. Conflating them is the mistake this guards against.
func TestFlakyVsConsistentlyBroken(t *testing.T) {
	var runs []domain.TestRun
	// flappy: pass, fail, pass, fail -> 3 flips
	for i, s := range []string{domain.StepPass, domain.StepFail, domain.StepPass, domain.StepFail} {
		runs = append(runs, run("flappy", "Flappy case", i, s))
	}
	// broken: fail every time -> 0 flips
	for i, s := range []string{domain.StepFail, domain.StepFail, domain.StepFail} {
		runs = append(runs, run("broken", "Broken case", i, s))
	}
	// solid: pass every time
	for i, s := range []string{domain.StepPass, domain.StepPass, domain.StepPass} {
		runs = append(runs, run("solid", "Solid case", i, s))
	}

	health := CaseHealthFrom(runs)
	if len(health) != 3 {
		t.Fatalf("want 3 cases, got %d", len(health))
	}

	flappy := find(t, health, "flappy")
	if !flappy.Flaky {
		t.Errorf("alternating case should be flaky: %+v", flappy)
	}
	if flappy.Flips != 3 {
		t.Errorf("flips = %d, want 3", flappy.Flips)
	}
	if flappy.PassRate != 0.5 {
		t.Errorf("pass rate = %v, want 0.5", flappy.PassRate)
	}

	broken := find(t, health, "broken")
	if broken.Flaky {
		t.Errorf("a consistently failing case is broken, not flaky: %+v", broken)
	}
	if broken.PassRate != 0 || broken.Fail != 3 {
		t.Errorf("broken case wrong: %+v", broken)
	}

	solid := find(t, health, "solid")
	if solid.Flaky || solid.PassRate != 1 {
		t.Errorf("solid case wrong: %+v", solid)
	}

	// Flaky sorts first so it surfaces where it matters.
	if health[0].CaseID != "flappy" {
		t.Errorf("flaky case should sort first, got %q", health[0].CaseID)
	}
	// Then ascending pass rate: broken (0) before solid (1).
	if health[1].CaseID != "broken" || health[2].CaseID != "solid" {
		t.Errorf("order = %q,%q; want broken,solid", health[1].CaseID, health[2].CaseID)
	}
}

func TestHistoryIsOldestFirstAndCapped(t *testing.T) {
	var runs []domain.TestRun
	for i := 0; i < maxHistory+5; i++ {
		runs = append(runs, run("c1", "Case", i, domain.StepPass))
	}
	h := find(t, CaseHealthFrom(runs), "c1")
	if h.Runs != maxHistory || len(h.History) != maxHistory {
		t.Fatalf("history not capped: runs=%d history=%d want %d", h.Runs, len(h.History), maxHistory)
	}
	for i := 1; i < len(h.History); i++ {
		if h.History[i].StartedAt.Before(h.History[i-1].StartedAt) {
			t.Fatalf("history not oldest-first at %d", i)
		}
	}
	// Capping keeps the NEWEST runs, so the last point is the most recent.
	newest := base.Add(time.Duration(maxHistory+4) * time.Minute)
	if !h.History[len(h.History)-1].StartedAt.Equal(newest) {
		t.Errorf("last point = %s, want newest %s", h.History[len(h.History)-1].StartedAt, newest)
	}
	if h.LastStatus != domain.StepPass || h.AvgMs != 30000 {
		t.Errorf("summary wrong: last=%q avgMs=%d", h.LastStatus, h.AvgMs)
	}
}

func TestBlockedCountsSeparatelyFromFail(t *testing.T) {
	runs := []domain.TestRun{
		run("c1", "Case", 0, domain.StepPass),
		run("c1", "Case", 1, domain.StepBlocked),
	}
	h := find(t, CaseHealthFrom(runs), "c1")
	if h.Blocked != 1 || h.Fail != 0 || h.Pass != 1 {
		t.Errorf("blocked should not be counted as fail: %+v", h)
	}
}

func TestIgnoresRunsWithoutCase(t *testing.T) {
	r := run("", "Ad-hoc", 0, domain.StepPass)
	if got := CaseHealthFrom([]domain.TestRun{r}); len(got) != 0 {
		t.Errorf("ad-hoc runs (no case id) must be ignored, got %+v", got)
	}
}
