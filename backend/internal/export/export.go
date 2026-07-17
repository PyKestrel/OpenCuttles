// Package export renders Testral test cases and cycle-run results into the
// interchange formats CI systems and QMetry expect: JUnit XML for pipelines,
// and CSV/XLSX (the QMetry repeated-row-per-step layout for cases) for
// spreadsheets and round-tripping back through the importer.
package export

import (
	"bytes"
	"encoding/csv"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"

	"github.com/opencuttles/opencuttles/backend/internal/domain"
)

// CaseNamer resolves a case id to a human-readable name (its summary). It may
// return "" when the case is unknown; callers fall back to the run's TestName.
type CaseNamer func(caseID string) string

// ---- test cases (QMetry round-trip) ----

var caseHeader = []string{
	"Summary", "Description", "Precondition", "Priority", "Status",
	"Labels", "Components", "Folder", "Key",
	"Step Summary", "Test Data", "Expected Result",
}

// caseRows renders cases into the repeated-row-per-step layout the importer
// consumes: the first row of a case carries the case columns + its first step;
// each further step is a row with the case columns blank. A case with no steps
// is a single row with empty step columns.
func caseRows(cases []domain.TestCase) [][]string {
	rows := [][]string{caseHeader}
	for _, c := range cases {
		caseCols := []string{
			c.Summary, c.Description, c.Precondition, c.Priority, c.Status,
			strings.Join(c.Labels, ", "), strings.Join(c.Components, ", "),
			c.FolderPath, c.ExternalKey,
		}
		if len(c.Steps) == 0 {
			rows = append(rows, appendCols(caseCols, "", "", ""))
			continue
		}
		for i, st := range c.Steps {
			if i == 0 {
				rows = append(rows, appendCols(caseCols, st.Action, st.TestData, st.Expected))
			} else {
				blank := make([]string, len(caseCols))
				rows = append(rows, appendCols(blank, st.Action, st.TestData, st.Expected))
			}
		}
	}
	return rows
}

// CasesCSV renders test cases as CSV bytes.
func CasesCSV(cases []domain.TestCase) ([]byte, error) { return toCSV(caseRows(cases)) }

// CasesXLSX renders test cases as an XLSX workbook.
func CasesXLSX(cases []domain.TestCase) ([]byte, error) { return toXLSX("Cases", caseRows(cases)) }

// ---- cycle-run results ----

var resultHeader = []string{"Case", "Case Result", "Step #", "Step", "Step Result", "Duration (ms)", "Note"}

func resultRows(runs []domain.TestRun, name CaseNamer) [][]string {
	rows := [][]string{resultHeader}
	for _, r := range runs {
		cn := caseName(r, name)
		cv := verdictLabel(caseVerdict(r))
		if len(r.Steps) == 0 {
			rows = append(rows, []string{cn, cv, "", "", "", "", r.Error})
			continue
		}
		for i, st := range r.Steps {
			rows = append(rows, []string{
				cn, cv, strconv.Itoa(i + 1), st.Text, stepStatus(st),
				strconv.FormatInt(st.DurationMs, 10), st.Detail,
			})
		}
	}
	return rows
}

// ResultsCSV renders a cycle run's per-case/per-step results as CSV.
func ResultsCSV(runs []domain.TestRun, name CaseNamer) ([]byte, error) {
	return toCSV(resultRows(runs, name))
}

// ResultsXLSX renders a cycle run's per-case/per-step results as XLSX.
func ResultsXLSX(runs []domain.TestRun, name CaseNamer) ([]byte, error) {
	return toXLSX("Results", resultRows(runs, name))
}

// ---- JUnit XML (CI) ----

type junitSuites struct {
	XMLName  xml.Name     `xml:"testsuites"`
	Name     string       `xml:"name,attr"`
	Tests    int          `xml:"tests,attr"`
	Failures int          `xml:"failures,attr"`
	Skipped  int          `xml:"skipped,attr"`
	Time     string       `xml:"time,attr"`
	Suites   []junitSuite `xml:"testsuite"`
}

type junitSuite struct {
	Name      string      `xml:"name,attr"`
	Tests     int         `xml:"tests,attr"`
	Failures  int         `xml:"failures,attr"`
	Skipped   int         `xml:"skipped,attr"`
	Time      string      `xml:"time,attr"`
	Timestamp string      `xml:"timestamp,attr"`
	Cases     []junitCase `xml:"testcase"`
}

type junitCase struct {
	Name      string        `xml:"name,attr"`
	Classname string        `xml:"classname,attr"`
	Time      string        `xml:"time,attr"`
	Failure   *junitFailure `xml:"failure,omitempty"`
	Skipped   *junitSkipped `xml:"skipped,omitempty"`
	SystemOut string        `xml:"system-out,omitempty"`
}

type junitFailure struct {
	Message string `xml:"message,attr"`
	Body    string `xml:",chardata"`
}

type junitSkipped struct {
	Message string `xml:"message,attr,omitempty"`
}

// JUnit renders a cycle run and its child case runs as a JUnit XML document.
// pass → clean testcase, fail (any failed step) → <failure>, blocked/incomplete
// → <skipped>, so CI reports the case verdict faithfully.
func JUnit(run domain.CycleRun, runs []domain.TestRun, name CaseNamer) ([]byte, error) {
	suiteName := run.CycleName
	if suiteName == "" {
		suiteName = "cycle " + run.CycleID
	}
	suite := junitSuite{Name: suiteName, Timestamp: run.StartedAt.UTC().Format("2006-01-02T15:04:05Z")}
	var failures, skipped int
	for _, r := range runs {
		tc := junitCase{Name: caseName(r, name), Classname: suiteName, Time: durationSecs(r)}
		switch caseVerdict(r) {
		case "fail":
			failures++
			msg := r.Error
			if msg == "" {
				msg = "case failed"
			}
			tc.Failure = &junitFailure{Message: msg, Body: stepLog(r)}
		case "skip":
			skipped++
			tc.Skipped = &junitSkipped{Message: r.Error}
			tc.SystemOut = stepLog(r)
		default:
			tc.SystemOut = stepLog(r)
		}
		suite.Cases = append(suite.Cases, tc)
	}
	suite.Tests = len(runs)
	suite.Failures = failures
	suite.Skipped = skipped
	suite.Time = totalSecs(runs)

	doc := junitSuites{
		Name: suiteName, Tests: len(runs), Failures: failures, Skipped: skipped,
		Time: suite.Time, Suites: []junitSuite{suite},
	}
	body, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), body...), nil
}

// ---- helpers ----

func appendCols(base []string, extra ...string) []string {
	out := make([]string, 0, len(base)+len(extra))
	out = append(out, base...)
	out = append(out, extra...)
	return out
}

func toCSV(rows [][]string) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.WriteAll(rows); err != nil {
		return nil, err
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

func toXLSX(sheet string, rows [][]string) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()
	f.SetSheetName(f.GetSheetName(0), sheet)
	for r, row := range rows {
		for c, val := range row {
			cell, err := excelize.CoordinatesToCellName(c+1, r+1)
			if err != nil {
				return nil, err
			}
			if err := f.SetCellStr(sheet, cell, val); err != nil {
				return nil, err
			}
		}
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func caseName(r domain.TestRun, name CaseNamer) string {
	if name != nil {
		if n := name(r.CaseID); n != "" {
			return n
		}
	}
	if r.TestName != "" {
		return r.TestName
	}
	if r.CaseID != "" {
		return r.CaseID
	}
	return r.ID
}

// caseVerdict derives pass/fail/skip from a child run: an explicit failed step
// is a fail; otherwise a non-passed run (blocked/incomplete/unverified) is a
// skip. A test tool must not report an unverified case as a hard failure.
func caseVerdict(r domain.TestRun) string {
	if r.Passed {
		return "pass"
	}
	for _, st := range r.Steps {
		if st.Status == domain.StepFail {
			return "fail"
		}
	}
	return "skip"
}

func verdictLabel(v string) string {
	switch v {
	case "pass":
		return "Passed"
	case "fail":
		return "Failed"
	default:
		return "Blocked"
	}
}

func stepStatus(st domain.StepResult) string {
	if st.Status != "" {
		return st.Status
	}
	if st.Pass {
		return domain.StepPass
	}
	return domain.StepFail
}

func stepLog(r domain.TestRun) string {
	var b strings.Builder
	for i, st := range r.Steps {
		fmt.Fprintf(&b, "%d. %s [%s]", i+1, st.Text, stepStatus(st))
		if st.Detail != "" {
			b.WriteString(" — " + st.Detail)
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// runSeconds is a run's wall-clock duration, never negative: JUnit's time
// attribute must be non-negative or CI parsers reject the report, and a clock
// step backwards mid-run would otherwise produce one.
func runSeconds(r domain.TestRun) float64 {
	if r.FinishedAt == nil {
		return 0
	}
	if s := r.FinishedAt.Sub(r.StartedAt).Seconds(); s > 0 {
		return s
	}
	return 0
}

func durationSecs(r domain.TestRun) string {
	return strconv.FormatFloat(runSeconds(r), 'f', 3, 64)
}

func totalSecs(runs []domain.TestRun) string {
	var total float64
	for _, r := range runs {
		total += runSeconds(r)
	}
	return strconv.FormatFloat(total, 'f', 3, 64)
}
