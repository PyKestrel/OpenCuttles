// Package qmetry parses QMetry Test Management (QTM4J) test-case exports —
// CSV or XLSX — into Testral test cases. QMetry lays multi-step cases out as
// repeated rows: the first row of a case carries the case-level columns plus its
// first step, and each additional step is a new row with the case columns blank.
package qmetry

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"

	"github.com/opencuttles/opencuttles/backend/internal/domain"
)

// Result is the outcome of parsing an export file.
type Result struct {
	Cases       []domain.TestCase `json:"-"`
	CasesParsed int               `json:"casesParsed"`
	StepsParsed int               `json:"stepsParsed"`
	RowsSkipped int               `json:"rowsSkipped"`
	Warnings    []string          `json:"warnings"`
}

// canonical field → header aliases (compared after lower-casing + stripping
// non-alphanumerics, so "Fix Version" == "fixversion").
var headerAliases = map[string][]string{
	"summary":      {"summary", "title", "name", "testcasesummary", "testcasename", "testcase"},
	"description":  {"description", "objective"},
	"precondition": {"precondition", "preconditions"},
	"priority":     {"priority"},
	"status":       {"status"},
	"components":   {"components", "component"},
	"labels":       {"labels", "label", "tags", "tag"},
	"folder":       {"folder", "folderpath", "path"},
	"externalkey":  {"key", "testcasekey", "testcaseid", "issuekey", "id"},
	"stepsummary":  {"stepsummary", "step", "teststep", "action", "stepaction", "stepdescription", "teststepdescription"},
	"testdata":     {"testdata", "data", "inputdata", "stepdata", "input"},
	"expected":     {"expectedresult", "expected", "expectedresults", "expectedoutcome"},
}

// ParseFile sniffs the format from the filename and parses the bytes.
func ParseFile(filename string, data []byte) (Result, error) {
	lower := strings.ToLower(filename)
	switch {
	case strings.HasSuffix(lower, ".xlsx") || strings.HasSuffix(lower, ".xlsm"):
		return parseXLSX(data)
	case strings.HasSuffix(lower, ".csv"), strings.HasSuffix(lower, ".tsv"), filename == "":
		return parseCSV(data)
	default:
		// Fall back to CSV sniffing for unknown extensions.
		return parseCSV(data)
	}
}

func parseCSV(data []byte) (Result, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1 // tolerate ragged rows
	rows, err := r.ReadAll()
	if err != nil {
		return Result{}, fmt.Errorf("parse csv: %w", err)
	}
	return parseRows(rows), nil
}

func parseXLSX(data []byte) (Result, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return Result{}, fmt.Errorf("open xlsx: %w", err)
	}
	defer f.Close()
	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return Result{Warnings: []string{"workbook has no sheets"}}, nil
	}
	rows, err := f.GetRows(sheets[0])
	if err != nil {
		return Result{}, fmt.Errorf("read sheet %q: %w", sheets[0], err)
	}
	return parseRows(rows), nil
}

// parseRows applies header mapping + the repeated-row-per-step algorithm.
func parseRows(rows [][]string) Result {
	var res Result
	if len(rows) == 0 {
		res.Warnings = append(res.Warnings, "file is empty")
		return res
	}
	cols, unmatched := mapHeaders(rows[0])
	for _, u := range unmatched {
		res.Warnings = append(res.Warnings, "unmapped column: "+u)
	}
	if _, ok := cols["summary"]; !ok {
		res.Warnings = append(res.Warnings, "no Summary/Title column found — cannot import")
		return res
	}

	get := func(row []string, field string) string {
		if i, ok := cols[field]; ok && i < len(row) {
			return strings.TrimSpace(row[i])
		}
		return ""
	}

	var cur *domain.TestCase
	flush := func() {
		if cur != nil {
			for i := range cur.Steps {
				cur.Steps[i].Index = i
			}
			res.Cases = append(res.Cases, *cur)
		}
		cur = nil
	}

	for _, row := range rows[1:] {
		if isBlankRow(row) {
			continue
		}
		summary := get(row, "summary")
		if summary != "" {
			flush()
			cur = &domain.TestCase{
				Summary:      summary,
				Description:  get(row, "description"),
				Precondition: get(row, "precondition"),
				Priority:     get(row, "priority"),
				Status:       get(row, "status"),
				Labels:       splitList(get(row, "labels")),
				Components:   splitList(get(row, "components")),
				FolderPath:   normalizeFolder(get(row, "folder")),
				ExternalKey:  get(row, "externalkey"),
			}
		}
		action := get(row, "stepsummary")
		testData := get(row, "testdata")
		expected := get(row, "expected")
		if action != "" || testData != "" || expected != "" {
			if cur == nil {
				res.RowsSkipped++ // a step row with no preceding case
				continue
			}
			cur.Steps = append(cur.Steps, domain.TestStep{Action: action, TestData: testData, Expected: expected})
			res.StepsParsed++
		} else if summary == "" {
			res.RowsSkipped++
		}
	}
	flush()
	res.CasesParsed = len(res.Cases)
	return res
}

// mapHeaders returns canonical field → column index, plus the headers that
// matched nothing (for warnings). First match wins on duplicate aliases.
func mapHeaders(header []string) (map[string]int, []string) {
	cols := map[string]int{}
	var unmatched []string
	for i, h := range header {
		norm := normalizeHeader(h)
		if norm == "" {
			continue
		}
		matched := false
		for canonical, aliases := range headerAliases {
			if _, taken := cols[canonical]; taken {
				continue
			}
			for _, a := range aliases {
				if norm == a {
					cols[canonical] = i
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
		if !matched {
			unmatched = append(unmatched, strings.TrimSpace(h))
		}
	}
	return cols, unmatched
}

func normalizeHeader(h string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(h) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func splitList(s string) []string {
	if s == "" {
		return []string{}
	}
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' })
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// normalizeFolder trims and collapses to a "/"-separated path (QMetry supports
// comma-separated multi-folder; we keep the first).
func normalizeFolder(s string) string {
	if s == "" {
		return ""
	}
	if i := strings.IndexByte(s, ','); i >= 0 {
		s = s[:i]
	}
	s = strings.ReplaceAll(s, "\\", "/")
	return strings.Trim(strings.TrimSpace(s), "/")
}

func isBlankRow(row []string) bool {
	for _, c := range row {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}
