package scenario

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/opencuttles/opencuttles/backend/internal/domain"
)

// fakeCycleStore is an in-memory CycleStore + RunSink for the executor test.
type fakeCycleStore struct {
	mu        sync.Mutex
	cycle     domain.TestCycle
	cases     map[string]domain.TestCase
	instance  domain.Instance
	cycleRuns map[string]domain.CycleRun
	caseRuns  map[string]domain.TestRun
	runSeq    int
}

func (f *fakeCycleStore) GetTestCycle(_ context.Context, id string) (domain.TestCycle, error) {
	return f.cycle, nil
}
func (f *fakeCycleStore) GetTestCase(_ context.Context, id string) (domain.TestCase, error) {
	return f.cases[id], nil
}
func (f *fakeCycleStore) GetInstance(_ context.Context, id string) (domain.Instance, error) {
	return f.instance, nil
}
func (f *fakeCycleStore) GetBuild(_ context.Context, id string) (domain.Build, error) {
	return domain.Build{}, nil
}
func (f *fakeCycleStore) CreateCycleRun(_ context.Context, run domain.CycleRun) (domain.CycleRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runSeq++
	run.ID = "cyc_run"
	run.Status = "running"
	run.StartedAt = time.Now()
	f.cycleRuns[run.ID] = run
	return run, nil
}
func (f *fakeCycleStore) UpdateCycleRun(_ context.Context, run domain.CycleRun) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cycleRuns[run.ID] = run
	return nil
}
func (f *fakeCycleStore) CreateCaseRun(_ context.Context, cycleRunID, caseID, instanceID string) (domain.TestRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runSeq++
	run := domain.TestRun{ID: "run_" + caseID, CycleRunID: cycleRunID, CaseID: caseID, InstanceID: instanceID, Status: "running", Steps: []domain.StepResult{}}
	f.caseRuns[run.ID] = run
	return run, nil
}
func (f *fakeCycleStore) GetTestRun(_ context.Context, id string) (domain.TestRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.caseRuns[id], nil
}
func (f *fakeCycleStore) UpdateTestRun(_ context.Context, run domain.TestRun) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.caseRuns[run.ID] = run
	return nil
}
func (f *fakeCycleStore) AppendStep(_ context.Context, runID string, step domain.StepResult) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	run := f.caseRuns[runID]
	step.Index = len(run.Steps)
	run.Steps = append(run.Steps, step)
	f.caseRuns[runID] = run
	return nil
}

type fakeBinder struct {
	mu       sync.Mutex
	bound    string
	binds    int
	unbinds  int
	setCalls int
}

func (b *fakeBinder) SetActive(string) { b.mu.Lock(); b.setCalls++; b.mu.Unlock() }
func (b *fakeBinder) BindRun(_, runID, _ string, _ []string) {
	b.mu.Lock()
	b.bound = runID
	b.binds++
	b.mu.Unlock()
}
func (b *fakeBinder) UnbindRun(string) { b.mu.Lock(); b.bound = ""; b.unbinds++; b.mu.Unlock() }
func (b *fakeBinder) current() string  { b.mu.Lock(); defer b.mu.Unlock(); return b.bound }

func TestCycleExecutorFanOutAndAggregate(t *testing.T) {
	store := &fakeCycleStore{
		cases: map[string]domain.TestCase{
			"c1": {ID: "c1", Summary: "Case one", Steps: []domain.TestStep{{Action: "do a", Expected: "a ok"}}},
			"c2": {ID: "c2", Summary: "Case two", Steps: []domain.TestStep{{Action: "do b", Expected: "b ok"}}},
		},
		cycle:     domain.TestCycle{ID: "cyc1", Name: "Reg", Platform: domain.PlatformWindows, CaseIDs: []string{"c1", "c2"}},
		instance:  domain.Instance{ID: "win_1", Platform: domain.PlatformWindows, State: domain.StateOnline},
		cycleRuns: map[string]domain.CycleRun{},
		caseRuns:  map[string]domain.TestRun{},
	}
	binder := &fakeBinder{}

	// Fake agent: appends a pass step for the first case and a fail step for the
	// second, simulating report_step_result, then returns a terminal summary.
	var calls int
	var callMu sync.Mutex
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callMu.Lock()
		n := calls
		calls++
		callMu.Unlock()
		status := domain.StepPass
		if n == 1 {
			status = domain.StepFail
		}
		_ = store.AppendStep(r.Context(), binder.current(), domain.StepResult{Text: "step", Status: status, Pass: status == domain.StepPass})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":{"text":"done"}}`))
	}))
	defer agent.Close()

	exec := NewCycleExecutor(store, nil, binder, agent.URL, nil)
	if _, err := exec.Start(context.Background(), "cyc1", "win_1", domain.CycleTriggerManual, ""); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Wait for completion.
	deadline := time.Now().Add(10 * time.Second)
	for {
		store.mu.Lock()
		run := store.cycleRuns["cyc_run"]
		store.mu.Unlock()
		if run.FinishedAt != nil {
			if run.Status != "failed" {
				t.Fatalf("cycle status = %q, want failed (one case failed)", run.Status)
			}
			if run.Totals.Cases != 2 || run.Totals.Pass != 1 || run.Totals.Fail != 1 {
				t.Fatalf("totals wrong: %+v", run.Totals)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("cycle run did not finish; status=%q", run.Status)
		}
		time.Sleep(20 * time.Millisecond)
	}

	if binder.binds != 2 || binder.unbinds != 2 {
		t.Fatalf("expected 2 binds/unbinds, got %d/%d", binder.binds, binder.unbinds)
	}
	// Each case run captured its step.
	if r1 := store.caseRuns["run_c1"]; r1.Status != "passed" || len(r1.Steps) != 1 {
		t.Fatalf("case1 run wrong: %+v", r1)
	}
	if r2 := store.caseRuns["run_c2"]; r2.Status != "failed" || len(r2.Steps) != 1 {
		t.Fatalf("case2 run wrong: %+v", r2)
	}
}
