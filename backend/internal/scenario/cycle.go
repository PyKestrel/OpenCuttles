package scenario

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/opencuttles/opencuttles/backend/internal/devicecontrol"
	"github.com/opencuttles/opencuttles/backend/internal/domain"
)

// CycleStore is the persistence surface the cycle executor needs.
type CycleStore interface {
	GetTestCycle(ctx context.Context, id string) (domain.TestCycle, error)
	GetTestCase(ctx context.Context, id string) (domain.TestCase, error)
	GetInstance(ctx context.Context, id string) (domain.Instance, error)
	GetBuild(ctx context.Context, id string) (domain.Build, error)
	CreateCycleRun(ctx context.Context, run domain.CycleRun) (domain.CycleRun, error)
	UpdateCycleRun(ctx context.Context, run domain.CycleRun) error
	CreateCaseRun(ctx context.Context, cycleRunID, caseID, instanceID string) (domain.TestRun, error)
	GetTestRun(ctx context.Context, id string) (domain.TestRun, error)
	UpdateTestRun(ctx context.Context, run domain.TestRun) error
}

// Binder is the MCP service subset the executor uses to point the agent at the
// device and bind report_step_result to the case run (satisfied by *mcp.Service).
type Binder interface {
	SetActive(id string)
	BindRun(deviceID, runID, runDir string, stepTexts []string)
	UnbindRun(deviceID string)
}

// CycleExecutor runs a test cycle by driving the headless Flue agent once per
// case: the agent interprets each case's steps, verifies expected results, and
// reports per-step outcomes back through the report_step_result MCP tool.
type CycleExecutor struct {
	store    CycleStore
	devices  *devicecontrol.Service
	binder   Binder
	agentURL string
	logger   *slog.Logger
	artifact string
	http     *http.Client

	mu      sync.Mutex
	running map[string]bool // deviceID → a cycle run is in flight (serialize per device)
}

// NewCycleExecutor builds the executor. agentURL is the Flue sidecar base (e.g.
// http://127.0.0.1:8790); the executor calls it directly, bypassing the
// permission-guarded /agents proxy.
func NewCycleExecutor(store CycleStore, devices *devicecontrol.Service, binder Binder, agentURL string, logger *slog.Logger) *CycleExecutor {
	return &CycleExecutor{
		store:    store,
		devices:  devices,
		binder:   binder,
		agentURL: strings.TrimRight(agentURL, "/"),
		logger:   logger,
		artifact: ArtifactRoot(),
		running:  map[string]bool{},
		http:     &http.Client{Timeout: 20 * time.Minute},
	}
}

// Start creates a cycle run and executes it asynchronously.
func (e *CycleExecutor) Start(ctx context.Context, cycleID, instanceID, trigger, buildID string) (domain.CycleRun, error) {
	cycle, err := e.store.GetTestCycle(ctx, cycleID)
	if err != nil {
		return domain.CycleRun{}, err
	}
	if _, err := e.store.GetInstance(ctx, instanceID); err != nil {
		return domain.CycleRun{}, err
	}
	run, err := e.store.CreateCycleRun(ctx, domain.CycleRun{
		CycleID:    cycle.ID,
		Trigger:    trigger,
		BuildID:    buildID,
		InstanceID: instanceID,
	})
	if err != nil {
		return domain.CycleRun{}, err
	}
	run.CycleName = cycle.Name
	go e.execute(context.Background(), cycle, instanceID, run)
	return run, nil
}

func (e *CycleExecutor) execute(ctx context.Context, cycle domain.TestCycle, instanceID string, run domain.CycleRun) {
	// Serialize per device: the report_step_result binding is device-keyed.
	e.mu.Lock()
	busy := e.running[instanceID]
	if !busy {
		e.running[instanceID] = true
	}
	e.mu.Unlock()
	if busy {
		e.finishCycle(ctx, run, "failed", "another cycle run is already using this device")
		return
	}
	defer func() {
		e.mu.Lock()
		delete(e.running, instanceID)
		e.mu.Unlock()
	}()

	instance, err := e.store.GetInstance(ctx, instanceID)
	if err != nil {
		e.finishCycle(ctx, run, "failed", err.Error())
		return
	}

	// Install the build once before the cases run (best-effort).
	if run.BuildID != "" {
		e.installBuild(ctx, instance, run.BuildID)
	}

	totals := domain.CycleTotals{Cases: len(cycle.CaseIDs)}
	for i, caseID := range cycle.CaseIDs {
		select {
		case <-ctx.Done():
			e.finishCycle(ctx, run, "failed", "cycle run cancelled")
			return
		default:
		}
		category := e.runCase(ctx, cycle, run, instance, i, caseID)
		switch category {
		case domain.StepPass:
			totals.Pass++
		case domain.StepBlocked:
			totals.Blocked++
		case "notrun":
			totals.NotRun++
		default:
			totals.Fail++
		}
		run.Totals = totals
		_ = e.store.UpdateCycleRun(ctx, run)
	}

	status := "passed"
	if totals.Fail > 0 || totals.Blocked > 0 {
		status = "failed"
	}
	run.Totals = totals
	e.finishCycle(ctx, run, status, "")
}

// runCase executes one case via a headless agent run and returns its rollup
// category (pass/fail/blocked/notrun).
func (e *CycleExecutor) runCase(ctx context.Context, cycle domain.TestCycle, cycleRun domain.CycleRun, instance domain.Instance, index int, caseID string) string {
	tc, err := e.store.GetTestCase(ctx, caseID)
	if err != nil {
		return "notrun"
	}
	caseRun, err := e.store.CreateCaseRun(ctx, cycleRun.ID, caseID, instance.ID)
	if err != nil {
		return "fail"
	}
	runDir := filepath.Join(e.artifact, caseRun.ID)
	_ = os.MkdirAll(runDir, 0o755)

	stepTexts := make([]string, len(tc.Steps))
	for i, st := range tc.Steps {
		stepTexts[i] = st.Action
	}
	e.binder.SetActive(instance.ID)
	e.binder.BindRun(instance.ID, caseRun.ID, runDir, stepTexts)

	convID := fmt.Sprintf("cyc-%s-%d", cycleRun.ID, index)
	caseCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	terminal, agentErr := e.callAgent(caseCtx, convID, renderCasePrompt(tc))
	cancel()
	e.binder.UnbindRun(instance.ID)

	// Reload the run to pick up steps captured by report_step_result.
	if reloaded, err := e.store.GetTestRun(ctx, caseRun.ID); err == nil {
		caseRun = reloaded
	}
	category, passed, detail := classifyCase(caseRun.Steps, agentErr, terminal)

	now := time.Now().UTC()
	caseRun.FinishedAt = &now
	caseRun.Passed = passed
	if passed {
		caseRun.Status = "passed"
	} else {
		caseRun.Status = "failed"
	}
	if detail != "" {
		caseRun.Error = detail
	}
	if err := e.store.UpdateTestRun(ctx, caseRun); err != nil && e.logger != nil {
		e.logger.Error("persist case run failed", "run", caseRun.ID, "error", err)
	}
	return category
}

// classifyCase derives a rollup category, the run pass flag, and an optional
// detail from the captured steps + the agent's terminal summary.
func classifyCase(steps []domain.StepResult, agentErr error, terminal string) (category string, passed bool, detail string) {
	if agentErr != nil {
		return "fail", false, agentErr.Error()
	}
	if len(steps) == 0 {
		// Agent under-reported: synthesize from its terminal summary so the report
		// is never empty.
		low := strings.ToLower(terminal)
		if strings.Contains(low, "fail") || strings.Contains(low, "block") || strings.Contains(low, "could not") {
			return "fail", false, "no per-step reports; agent summary indicated failure"
		}
		return domain.StepPass, true, "no per-step reports; treated the agent summary as pass"
	}
	hasFail, hasBlocked := false, false
	for _, s := range steps {
		switch s.Status {
		case domain.StepFail:
			hasFail = true
		case domain.StepBlocked:
			hasBlocked = true
		}
	}
	switch {
	case hasFail:
		return "fail", false, ""
	case hasBlocked:
		return domain.StepBlocked, false, ""
	default:
		return domain.StepPass, true, ""
	}
}

func (e *CycleExecutor) finishCycle(ctx context.Context, run domain.CycleRun, status, errMsg string) {
	now := time.Now().UTC()
	run.FinishedAt = &now
	run.Status = status
	if err := e.store.UpdateCycleRun(ctx, run); err != nil && e.logger != nil {
		e.logger.Error("persist cycle run failed", "run", run.ID, "error", err)
	}
	if errMsg != "" && e.logger != nil {
		e.logger.Warn("cycle run ended with error", "run", run.ID, "error", errMsg)
	}
}

func (e *CycleExecutor) installBuild(ctx context.Context, instance domain.Instance, buildID string) {
	build, err := e.store.GetBuild(ctx, buildID)
	if err != nil {
		return
	}
	if instance.Platform == "" || instance.Platform == domain.PlatformAndroid {
		if err := e.devices.InstallAPK(ctx, instance.ID, build.Path); err != nil && e.logger != nil {
			e.logger.Warn("install build (adb) failed", "build", buildID, "error", err)
		}
		return
	}
	// Desktop: the runner fetches the artifact and installs it silently.
	if err := e.devices.InstallDesktopBuild(ctx, instance.ID, build.ID, build.Filename, ""); err != nil && e.logger != nil {
		e.logger.Warn("desktop build install failed; running against current state", "build", buildID, "error", err)
	}
}

// callAgent runs one case synchronously via the Flue sidecar and returns the
// agent's terminal assistant text.
func (e *CycleExecutor) callAgent(ctx context.Context, convID, message string) (string, error) {
	body, _ := json.Marshal(map[string]any{"message": message})
	endpoint := fmt.Sprintf("%s/agents/opencuttles/%s?wait=result", e.agentURL, url.PathEscape(convID))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("agent run failed: %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var parsed struct {
		Result struct {
			Text string `json:"text"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &parsed); err == nil && parsed.Result.Text != "" {
		return parsed.Result.Text, nil
	}
	return string(raw), nil
}

func renderCasePrompt(tc domain.TestCase) string {
	var b strings.Builder
	b.WriteString("You are running an AUTOMATED test case on the active device. Execute each step in order, then verify its Expected Result before moving on.\n\n")
	b.WriteString("Test case: " + tc.Summary + "\n")
	if strings.TrimSpace(tc.Precondition) != "" {
		b.WriteString("Precondition: " + tc.Precondition + "\n")
	}
	b.WriteString("\nSteps:\n")
	for i, st := range tc.Steps {
		line := fmt.Sprintf("%d. %s", i+1, strings.TrimSpace(st.Action))
		if strings.TrimSpace(st.TestData) != "" {
			line += "  [data: " + strings.TrimSpace(st.TestData) + "]"
		}
		if strings.TrimSpace(st.Expected) != "" {
			line += "  → expected: " + strings.TrimSpace(st.Expected)
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\nFor EACH numbered step: perform the action, verify its Expected Result on screen, then call report_step_result with that step's 1-based index, a status of \"pass\" / \"fail\" / \"blocked\", and a short note of what you observed. If a step's expected result cannot be achieved, report it \"fail\" and stop. When finished, reply with a single line: PASS or FAIL and one sentence why.")
	return b.String()
}
