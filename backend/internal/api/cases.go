package api

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/opencuttles/opencuttles/backend/internal/domain"
	"github.com/opencuttles/opencuttles/backend/internal/insights"
	"github.com/opencuttles/opencuttles/backend/internal/qmetry"
	"github.com/opencuttles/opencuttles/backend/internal/scenario"
	"github.com/opencuttles/opencuttles/backend/internal/scheduler"
)

// registerCaseRoutes wires QMetry-style test cases, cycles, cycle runs, and
// builds. Cases/cycles are guarded by PermissionTest; build upload by
// PermissionOperate.
func (s *Server) registerCaseRoutes() {
	t := func(h http.HandlerFunc) http.HandlerFunc { return s.require(domain.PermissionTest, h) }
	o := func(h http.HandlerFunc) http.HandlerFunc { return s.require(domain.PermissionOperate, h) }
	m := s.mux

	// Test cases
	m.HandleFunc("GET /api/v1/cases", t(s.listCases))
	m.HandleFunc("POST /api/v1/cases", t(s.createCase))
	m.HandleFunc("GET /api/v1/cases/folders", t(s.listCaseFolders))
	m.HandleFunc("POST /api/v1/cases/folders", t(s.createCaseFolder))
	m.HandleFunc("DELETE /api/v1/cases/folders", t(s.deleteCaseFolder))
	m.HandleFunc("POST /api/v1/cases/import", t(s.importCases))
	m.HandleFunc("POST /api/v1/cases/draft", t(s.draftCases))
	m.HandleFunc("GET /api/v1/cases/export", t(s.exportCases))
	m.HandleFunc("GET /api/v1/cases/health", t(s.caseHealth))
	m.HandleFunc("GET /api/v1/cases/{id}", t(s.getCase))
	m.HandleFunc("PUT /api/v1/cases/{id}", t(s.updateCase))
	m.HandleFunc("DELETE /api/v1/cases/{id}", t(s.deleteCase))

	// Test cycles
	m.HandleFunc("GET /api/v1/cycles", t(s.listCycles))
	m.HandleFunc("POST /api/v1/cycles", t(s.createCycle))
	m.HandleFunc("GET /api/v1/cycles/{id}", t(s.getCycle))
	m.HandleFunc("PUT /api/v1/cycles/{id}", t(s.updateCycle))
	m.HandleFunc("DELETE /api/v1/cycles/{id}", t(s.deleteCycle))
	m.HandleFunc("PUT /api/v1/cycles/{id}/cases", t(s.updateCycleCases))
	m.HandleFunc("PUT /api/v1/cycles/{id}/schedule", t(s.updateCycleSchedule))
	m.HandleFunc("POST /api/v1/cycles/{id}/run", t(s.runCycle))

	// Cycle runs (reports)
	m.HandleFunc("GET /api/v1/cycle-runs", t(s.listCycleRuns))
	m.HandleFunc("GET /api/v1/cycle-runs/{id}", t(s.getCycleRun))
	m.HandleFunc("GET /api/v1/cycle-runs/{id}/export", t(s.exportCycleRun))
	m.HandleFunc("DELETE /api/v1/cycle-runs/{id}", t(s.deleteCycleRun))

	// Builds
	m.HandleFunc("GET /api/v1/builds", t(s.listBuilds))
	m.HandleFunc("POST /api/v1/builds", o(s.uploadBuild))
}

// ---- cases ----

func (s *Server) listCases(w http.ResponseWriter, r *http.Request) {
	cases, err := s.store.ListTestCases(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cases)
}

func (s *Server) listCaseFolders(w http.ResponseWriter, r *http.Request) {
	folders, err := s.store.ListCaseFolders(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, folders)
}

func (s *Server) createCaseFolder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	if err := s.store.CreateCaseFolder(r.Context(), req.Path); err != nil {
		writeError(w, badRequest(err.Error()))
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
}

func (s *Server) deleteCaseFolder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	if err := s.store.DeleteCaseFolder(r.Context(), req.Path); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) createCase(w http.ResponseWriter, r *http.Request) {
	var c domain.TestCase
	if err := decodeJSON(w, r, &c); err != nil {
		writeError(w, err)
		return
	}
	if strings.TrimSpace(c.Summary) == "" {
		writeError(w, badRequest("summary is required"))
		return
	}
	created, err := s.store.CreateTestCase(r.Context(), c)
	if err != nil {
		writeError(w, badRequest(err.Error()))
		return
	}
	principal, _ := principalFromContext(r.Context())
	s.audit(r, principal, "create_case", "case", created.ID, "succeeded", created.Summary)
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) getCase(w http.ResponseWriter, r *http.Request) {
	c, err := s.store.GetTestCase(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) updateCase(w http.ResponseWriter, r *http.Request) {
	var c domain.TestCase
	if err := decodeJSON(w, r, &c); err != nil {
		writeError(w, err)
		return
	}
	c.ID = r.PathValue("id")
	if _, err := s.store.GetTestCase(r.Context(), c.ID); err != nil {
		writeError(w, err)
		return
	}
	if err := s.store.UpdateTestCase(r.Context(), c); err != nil {
		writeError(w, badRequest(err.Error()))
		return
	}
	principal, _ := principalFromContext(r.Context())
	s.audit(r, principal, "update_case", "case", c.ID, "succeeded", c.Summary)
	updated, _ := s.store.GetTestCase(r.Context(), c.ID)
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) deleteCase(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.DeleteTestCase(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	principal, _ := principalFromContext(r.Context())
	s.audit(r, principal, "delete_case", "case", id, "succeeded", "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) importCases(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, badRequest("invalid upload"))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, badRequest("a 'file' field is required"))
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 32<<20))
	if err != nil {
		writeError(w, badRequest("could not read upload"))
		return
	}
	res, err := qmetry.ParseFile(header.Filename, data)
	if err != nil {
		writeError(w, badRequest(err.Error()))
		return
	}
	created, err := s.store.BulkCreateCases(r.Context(), res.Cases)
	if err != nil {
		writeError(w, err)
		return
	}
	res.CasesParsed = created
	principal, _ := principalFromContext(r.Context())
	s.audit(r, principal, "import_cases", "case", header.Filename, "succeeded", "")
	writeJSON(w, http.StatusOK, res)
}

// ---- cycles ----

func (s *Server) listCycles(w http.ResponseWriter, r *http.Request) {
	cycles, err := s.store.ListTestCycles(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cycles)
}

func (s *Server) createCycle(w http.ResponseWriter, r *http.Request) {
	var c domain.TestCycle
	if err := decodeJSON(w, r, &c); err != nil {
		writeError(w, err)
		return
	}
	if strings.TrimSpace(c.Name) == "" {
		writeError(w, badRequest("name is required"))
		return
	}
	if err := s.applyCron(&c); err != nil {
		writeError(w, err)
		return
	}
	created, err := s.store.CreateTestCycle(r.Context(), c)
	if err != nil {
		writeError(w, badRequest(err.Error()))
		return
	}
	principal, _ := principalFromContext(r.Context())
	s.audit(r, principal, "create_cycle", "cycle", created.ID, "succeeded", created.Name)
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) getCycle(w http.ResponseWriter, r *http.Request) {
	c, err := s.store.GetTestCycle(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) updateCycle(w http.ResponseWriter, r *http.Request) {
	var c domain.TestCycle
	if err := decodeJSON(w, r, &c); err != nil {
		writeError(w, err)
		return
	}
	c.ID = r.PathValue("id")
	existing, err := s.store.GetTestCycle(r.Context(), c.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	c.CreatedAt = existing.CreatedAt
	if err := s.applyCron(&c); err != nil {
		writeError(w, err)
		return
	}
	if err := s.store.UpdateTestCycle(r.Context(), c); err != nil {
		writeError(w, badRequest(err.Error()))
		return
	}
	principal, _ := principalFromContext(r.Context())
	s.audit(r, principal, "update_cycle", "cycle", c.ID, "succeeded", c.Name)
	updated, _ := s.store.GetTestCycle(r.Context(), c.ID)
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) deleteCycle(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.DeleteTestCycle(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	principal, _ := principalFromContext(r.Context())
	s.audit(r, principal, "delete_cycle", "cycle", id, "succeeded", "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) updateCycleCases(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CaseIDs []string `json:"caseIds"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	c, err := s.store.GetTestCycle(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	c.CaseIDs = req.CaseIDs
	if err := s.store.UpdateTestCycle(r.Context(), c); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) updateCycleSchedule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Cron       string `json:"cron"`
		Timezone   string `json:"timezone"`
		OnNewBuild bool   `json:"onNewBuild"`
		Enabled    bool   `json:"enabled"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	c, err := s.store.GetTestCycle(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	c.Cron = strings.TrimSpace(req.Cron)
	c.Timezone = strings.TrimSpace(req.Timezone)
	c.OnNewBuild = req.OnNewBuild
	c.Enabled = req.Enabled
	if err := s.applyCron(&c); err != nil {
		writeError(w, err)
		return
	}
	if err := s.store.UpdateTestCycle(r.Context(), c); err != nil {
		writeError(w, err)
		return
	}
	principal, _ := principalFromContext(r.Context())
	s.audit(r, principal, "schedule_cycle", "cycle", c.ID, "succeeded", c.Cron)
	writeJSON(w, http.StatusOK, c)
}

// applyCron validates a cycle's cron + timezone and (re)computes its next run
// time. The cron's wall-clock fields are interpreted in the cycle's timezone
// (empty = UTC), so "0 9 * * *" means 9am where the user actually is.
func (s *Server) applyCron(c *domain.TestCycle) error {
	c.Timezone = strings.TrimSpace(c.Timezone)
	if !scheduler.ValidTimezone(c.Timezone) {
		return badRequest("unknown timezone: " + c.Timezone)
	}
	if strings.TrimSpace(c.Cron) == "" {
		c.Cron = ""
		c.NextRunAt = nil
		return nil
	}
	next, err := scheduler.NextIn(c.Cron, c.Timezone, time.Now().UTC())
	if err != nil {
		return badRequest("invalid cron expression: " + c.Cron)
	}
	c.NextRunAt = &next
	return nil
}

func (s *Server) runCycle(w http.ResponseWriter, r *http.Request) {
	var req struct {
		InstanceID string `json:"instanceId,omitempty"`
		BuildID    string `json:"buildId,omitempty"`
	}
	// Body is optional (both fields default), but a malformed body must not be
	// silently ignored — that would run the cycle against a zero-value target.
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	cycle, err := s.store.GetTestCycle(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	target, err := s.resolveCycleTarget(r.Context(), cycle, req.InstanceID)
	if err != nil {
		writeError(w, err)
		return
	}
	buildID := req.BuildID
	if buildID == "" {
		buildID = cycle.BuildID
	}
	run, err := s.cycles.Start(r.Context(), cycle.ID, target, domain.CycleTriggerManual, buildID)
	if err != nil {
		writeError(w, badRequest(err.Error()))
		return
	}
	principal, _ := principalFromContext(r.Context())
	s.audit(r, principal, "run_cycle", "cycle", cycle.ID, "accepted", run.ID)
	writeJSON(w, http.StatusAccepted, run)
}

// resolveCycleTarget picks the device a cycle runs on: an explicit id, else a
// running/online instance matching the cycle's platform.
func (s *Server) resolveCycleTarget(ctx context.Context, cycle domain.TestCycle, explicit string) (string, error) {
	if explicit != "" {
		if _, err := s.store.GetInstance(ctx, explicit); err != nil {
			return "", badRequest("unknown instanceId")
		}
		return explicit, nil
	}
	instances, err := s.store.ListInstances(ctx)
	if err != nil {
		return "", err
	}
	for _, inst := range instances {
		p := inst.Platform
		if p == "" {
			p = domain.PlatformAndroid
		}
		if p != cycle.Platform {
			continue
		}
		if inst.State == domain.StateRunning || inst.State == domain.StateOnline {
			return inst.ID, nil
		}
	}
	return "", badRequest("no running/online " + cycle.Platform + " device is available to run this cycle")
}

// ---- cycle runs ----

// listCycleRuns returns a page of cycle runs. It stays backward compatible: with
// no query params it responds with a bare array (what the UI polled before);
// with ?limit/?offset it returns {runs,total,limit,offset} so older history is
// reachable instead of being silently truncated.
func (s *Server) listCycleRuns(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	paged := q.Has("limit") || q.Has("offset")
	limit := atoiDefault(q.Get("limit"), 200)
	offset := atoiDefault(q.Get("offset"), 0)

	runs, total, err := s.store.ListCycleRunsPage(r.Context(), limit, offset)
	if err != nil {
		writeError(w, err)
		return
	}
	if !paged {
		writeJSON(w, http.StatusOK, runs)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Runs   []domain.CycleRun `json:"runs"`
		Total  int               `json:"total"`
		Limit  int               `json:"limit"`
		Offset int               `json:"offset"`
	}{Runs: runs, Total: total, Limit: limit, Offset: offset})
}

// caseHealth reports per-case pass rate, flakiness, and recent history — the
// cross-run view a single run's report can't give.
func (s *Server) caseHealth(w http.ResponseWriter, r *http.Request) {
	window := atoiDefault(r.URL.Query().Get("window"), 2000)
	runs, err := s.store.ListRecentCaseRuns(r.Context(), window)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, insights.CaseHealthFrom(runs))
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

func (s *Server) getCycleRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	run, err := s.store.GetCycleRun(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	cases, err := s.store.ListTestRunsByCycleRun(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Run   domain.CycleRun  `json:"run"`
		Cases []domain.TestRun `json:"cases"`
	}{Run: run, Cases: cases})
}

// deleteCycleRun removes a cycle run, its child case runs, and their on-disk
// artifact directories.
func (s *Server) deleteCycleRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cases, err := s.store.ListTestRunsByCycleRun(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.store.DeleteCycleRun(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	root := scenario.ArtifactRoot()
	for _, cr := range cases {
		_ = os.RemoveAll(filepath.Join(root, cr.ID))
	}
	principal, _ := principalFromContext(r.Context())
	s.audit(r, principal, "delete_cycle_run", "cycle-run", id, "succeeded", "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
