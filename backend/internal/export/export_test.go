package export

import (
	"encoding/xml"
	"strings"
	"testing"
	"time"

	"github.com/opencuttles/opencuttles/backend/internal/domain"
	"github.com/opencuttles/opencuttles/backend/internal/qmetry"
)

func sampleCases() []domain.TestCase {
	return []domain.TestCase{
		{
			Summary:      "Login works",
			Description:  "Verify sign-in",
			Precondition: "App installed",
			Priority:     "High",
			Status:       "Ready",
			Labels:       []string{"smoke", "auth"},
			Components:   []string{"login"},
			FolderPath:   "Auth/Signin",
			ExternalKey:  "TC-1",
			Steps: []domain.TestStep{
				{Action: "Open app", Expected: "Login screen shows"},
				{Action: "Enter creds", TestData: "user/pass", Expected: "Home screen"},
			},
		},
		{
			Summary: "Empty case",
			Labels:  []string{},
			// no steps
		},
	}
}

// TestCasesCSVRoundTrip exports cases to CSV and re-imports them, asserting the
// case + step content survives the round-trip through the importer.
func TestCasesCSVRoundTrip(t *testing.T) {
	data, err := CasesCSV(sampleCases())
	if err != nil {
		t.Fatalf("CasesCSV: %v", err)
	}
	res, err := qmetry.ParseFile("cases.csv", data)
	if err != nil {
		t.Fatalf("re-import: %v", err)
	}
	if res.CasesParsed != 2 {
		t.Fatalf("want 2 cases, got %d (warnings: %v)", res.CasesParsed, res.Warnings)
	}
	c := res.Cases[0]
	if c.Summary != "Login works" || c.Precondition != "App installed" || c.Priority != "High" {
		t.Fatalf("case-level fields lost: %+v", c)
	}
	if len(c.Labels) != 2 || c.Labels[0] != "smoke" || c.Components[0] != "login" {
		t.Fatalf("labels/components lost: %+v", c)
	}
	if c.FolderPath != "Auth/Signin" || c.ExternalKey != "TC-1" {
		t.Fatalf("folder/key lost: %+v", c)
	}
	if len(c.Steps) != 2 || c.Steps[1].TestData != "user/pass" || c.Steps[1].Expected != "Home screen" {
		t.Fatalf("steps lost: %+v", c.Steps)
	}
	if res.Cases[1].Summary != "Empty case" || len(res.Cases[1].Steps) != 0 {
		t.Fatalf("empty case wrong: %+v", res.Cases[1])
	}
}

// TestCasesXLSXRoundTrip does the same through the XLSX path.
func TestCasesXLSXRoundTrip(t *testing.T) {
	data, err := CasesXLSX(sampleCases())
	if err != nil {
		t.Fatalf("CasesXLSX: %v", err)
	}
	res, err := qmetry.ParseFile("cases.xlsx", data)
	if err != nil {
		t.Fatalf("re-import: %v", err)
	}
	if res.CasesParsed != 2 || len(res.Cases[0].Steps) != 2 {
		t.Fatalf("xlsx round-trip lost data: %+v", res)
	}
}

func TestJUnitVerdicts(t *testing.T) {
	start := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	fin := start.Add(3 * time.Second)
	runs := []domain.TestRun{
		{ID: "r1", CaseID: "c1", Passed: true, StartedAt: start, FinishedAt: &fin,
			Steps: []domain.StepResult{{Text: "step", Status: domain.StepPass}}},
		{ID: "r2", CaseID: "c2", Passed: false, Error: "boom", StartedAt: start, FinishedAt: &fin,
			Steps: []domain.StepResult{{Text: "step", Status: domain.StepFail}}},
		{ID: "r3", CaseID: "c3", Passed: false, Error: "incomplete", StartedAt: start, FinishedAt: &fin,
			Steps: []domain.StepResult{{Text: "step", Status: domain.StepBlocked}}},
	}
	run := domain.CycleRun{ID: "cr1", CycleID: "cy1", CycleName: "Smoke", StartedAt: start}
	names := map[string]string{"c1": "Alpha", "c2": "Beta", "c3": "Gamma"}
	data, err := JUnit(run, runs, func(id string) string { return names[id] })
	if err != nil {
		t.Fatalf("JUnit: %v", err)
	}

	var doc junitSuites
	if err := xml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal junit: %v\n%s", err, data)
	}
	if doc.Tests != 3 || doc.Failures != 1 || doc.Skipped != 1 {
		t.Fatalf("suite totals wrong: tests=%d failures=%d skipped=%d", doc.Tests, doc.Failures, doc.Skipped)
	}
	cases := doc.Suites[0].Cases
	if cases[0].Name != "Alpha" || cases[0].Failure != nil || cases[0].Skipped != nil {
		t.Fatalf("pass case wrong: %+v", cases[0])
	}
	if cases[1].Failure == nil || cases[1].Failure.Message != "boom" {
		t.Fatalf("fail case wrong: %+v", cases[1])
	}
	if cases[2].Skipped == nil {
		t.Fatalf("blocked case should be skipped: %+v", cases[2])
	}
	if !strings.Contains(string(data), "<?xml") {
		t.Fatal("missing xml header")
	}
}

// JUnit's time attribute must never be negative — CI parsers reject the report.
// A run whose finish precedes its start (clock step back, or inconsistent data)
// must clamp to 0 rather than emit "-345432.000".
func TestJUnitNeverEmitsNegativeTime(t *testing.T) {
	start := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	before := start.Add(-4 * time.Hour) // finished BEFORE it started
	runs := []domain.TestRun{
		{ID: "r1", CaseID: "c1", Passed: true, StartedAt: start, FinishedAt: &before},
	}
	data, err := JUnit(domain.CycleRun{ID: "cr", CycleName: "S", StartedAt: start}, runs, nil)
	if err != nil {
		t.Fatalf("JUnit: %v", err)
	}
	if strings.Contains(string(data), `time="-`) {
		t.Fatalf("negative time attribute emitted:\n%s", data)
	}

	var doc junitSuites
	if err := xml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if doc.Suites[0].Cases[0].Time != "0.000" {
		t.Errorf("time = %q, want 0.000", doc.Suites[0].Cases[0].Time)
	}
}
