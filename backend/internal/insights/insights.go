// Package insights turns raw cycle case-runs into the cross-run view a test
// team actually acts on: how often a case passes, whether it is flaky, and how
// its recent runs trend. A single-run report answers "did it pass?"; this
// answers "can I trust this case at all?".
package insights

import (
	"sort"
	"time"

	"github.com/opencuttles/opencuttles/backend/internal/domain"
)

// HistoryPoint is one past execution of a case, oldest-first in CaseHealth so a
// sparkline can render it directly.
type HistoryPoint struct {
	RunID      string    `json:"runId"`
	CycleRunID string    `json:"cycleRunId,omitempty"`
	Status     string    `json:"status"` // pass | fail | blocked
	StartedAt  time.Time `json:"startedAt"`
	DurationMs int64     `json:"durationMs"`
}

// CaseHealth summarizes a case's recent executions.
type CaseHealth struct {
	CaseID     string         `json:"caseId"`
	Summary    string         `json:"summary"`
	Runs       int            `json:"runs"`
	Pass       int            `json:"pass"`
	Fail       int            `json:"fail"`
	Blocked    int            `json:"blocked"`
	PassRate   float64        `json:"passRate"` // 0..1 over Runs
	Flips      int            `json:"flips"`    // status changes between consecutive runs
	Flaky      bool           `json:"flaky"`
	LastStatus string         `json:"lastStatus"`
	AvgMs      int64          `json:"avgMs"`
	History    []HistoryPoint `json:"history"`
}

// maxHistory caps the per-case window so one case with thousands of runs can't
// dominate the response.
const maxHistory = 20

// CaseHealthFrom groups finished case-runs by case and summarizes each. runs may
// be in any order; results are sorted flaky-first, then by ascending pass rate,
// so the cases needing attention come first.
func CaseHealthFrom(runs []domain.TestRun) []CaseHealth {
	byCase := map[string][]domain.TestRun{}
	for _, r := range runs {
		if r.CaseID == "" {
			continue
		}
		byCase[r.CaseID] = append(byCase[r.CaseID], r)
	}

	out := make([]CaseHealth, 0, len(byCase))
	for caseID, caseRuns := range byCase {
		// Oldest-first so History reads left-to-right and flips count in order.
		sort.Slice(caseRuns, func(i, j int) bool { return caseRuns[i].StartedAt.Before(caseRuns[j].StartedAt) })
		if len(caseRuns) > maxHistory {
			caseRuns = caseRuns[len(caseRuns)-maxHistory:]
		}

		h := CaseHealth{CaseID: caseID, Runs: len(caseRuns)}
		var totalMs int64
		prev := ""
		for _, r := range caseRuns {
			status := runStatus(r)
			switch status {
			case domain.StepPass:
				h.Pass++
			case domain.StepBlocked:
				h.Blocked++
			default:
				h.Fail++
			}
			if prev != "" && prev != status {
				h.Flips++
			}
			prev = status
			if r.TestName != "" {
				h.Summary = r.TestName
			}
			// Clamp: a clock step backwards must not yield a negative duration.
			var ms int64
			if r.FinishedAt != nil {
				if d := r.FinishedAt.Sub(r.StartedAt).Milliseconds(); d > 0 {
					ms = d
				}
			}
			totalMs += ms
			h.History = append(h.History, HistoryPoint{
				RunID: r.ID, CycleRunID: r.CycleRunID, Status: status,
				StartedAt: r.StartedAt, DurationMs: ms,
			})
		}
		h.LastStatus = prev
		if h.Runs > 0 {
			h.PassRate = float64(h.Pass) / float64(h.Runs)
			h.AvgMs = totalMs / int64(h.Runs)
		}
		// Flaky = the case has changed its mind more than once across recent runs
		// while producing at least one pass and one non-pass. A case that fails
		// consistently is broken, not flaky — that distinction is the point.
		h.Flaky = h.Flips >= 2 && h.Pass > 0 && (h.Fail+h.Blocked) > 0
		out = append(out, h)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Flaky != out[j].Flaky {
			return out[i].Flaky // flaky first — they need attention most
		}
		if out[i].PassRate != out[j].PassRate {
			return out[i].PassRate < out[j].PassRate
		}
		return out[i].CaseID < out[j].CaseID
	})
	return out
}

// runStatus derives a case run's verdict, preferring explicit step statuses over
// the pass flag so a blocked/unverified case isn't reported as a hard failure.
func runStatus(r domain.TestRun) string {
	if r.Passed {
		return domain.StepPass
	}
	for _, st := range r.Steps {
		if st.Status == domain.StepFail {
			return domain.StepFail
		}
	}
	if len(r.Steps) == 0 && r.Status == "" {
		return domain.StepBlocked
	}
	for _, st := range r.Steps {
		if st.Status == domain.StepBlocked {
			return domain.StepBlocked
		}
	}
	// Not passed, no explicit failed step: unverified/incomplete.
	return domain.StepBlocked
}
