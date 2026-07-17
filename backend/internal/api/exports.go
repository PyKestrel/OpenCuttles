package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/opencuttles/opencuttles/backend/internal/export"
)

// exportCases streams all test cases (optionally filtered to a folder subtree)
// in the QMetry repeated-row-per-step layout, as CSV or XLSX.
func (s *Server) exportCases(w http.ResponseWriter, r *http.Request) {
	cases, err := s.store.ListTestCases(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	if folder := strings.TrimSpace(r.URL.Query().Get("folder")); folder != "" {
		folder = strings.Trim(strings.ReplaceAll(folder, "\\", "/"), "/")
		filtered := cases[:0]
		for _, c := range cases {
			if c.FolderPath == folder || strings.HasPrefix(c.FolderPath, folder+"/") {
				filtered = append(filtered, c)
			}
		}
		cases = filtered
	}

	format := strings.ToLower(r.URL.Query().Get("format"))
	switch format {
	case "", "csv":
		data, err := export.CasesCSV(cases)
		writeDownload(w, err, data, "text/csv; charset=utf-8", "cases-export.csv")
	case "xlsx":
		data, err := export.CasesXLSX(cases)
		writeDownload(w, err, data, xlsxContentType, "cases-export.xlsx")
	default:
		writeError(w, badRequest("format must be csv or xlsx"))
	}
}

// exportCycleRun streams a finished cycle run's results as JUnit XML (CI),
// CSV, or XLSX.
func (s *Server) exportCycleRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	run, err := s.store.GetCycleRun(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	runs, err := s.store.ListTestRunsByCycleRun(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	namer := s.caseNamer(r.Context())

	format := strings.ToLower(r.URL.Query().Get("format"))
	switch format {
	case "", "junit":
		data, err := export.JUnit(run, runs, namer)
		writeDownload(w, err, data, "application/xml; charset=utf-8", fmt.Sprintf("cyclerun-%s-junit.xml", id))
	case "csv":
		data, err := export.ResultsCSV(runs, namer)
		writeDownload(w, err, data, "text/csv; charset=utf-8", fmt.Sprintf("cyclerun-%s-results.csv", id))
	case "xlsx":
		data, err := export.ResultsXLSX(runs, namer)
		writeDownload(w, err, data, xlsxContentType, fmt.Sprintf("cyclerun-%s-results.xlsx", id))
	default:
		writeError(w, badRequest("format must be junit, csv, or xlsx"))
	}
}

// caseNamer returns a memoized case-id → summary resolver for exports.
func (s *Server) caseNamer(ctx context.Context) export.CaseNamer {
	cache := map[string]string{}
	return func(caseID string) string {
		if caseID == "" {
			return ""
		}
		if n, ok := cache[caseID]; ok {
			return n
		}
		name := ""
		if tc, err := s.store.GetTestCase(ctx, caseID); err == nil {
			name = tc.Summary
		}
		cache[caseID] = name
		return name
	}
}

const xlsxContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

func writeDownload(w http.ResponseWriter, err error, data []byte, contentType, filename string) {
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
