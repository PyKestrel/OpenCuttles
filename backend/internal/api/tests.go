package api

import (
	"net/http"
	"path/filepath"
	"strings"

	"github.com/opencuttles/opencuttles/backend/internal/domain"
	"github.com/opencuttles/opencuttles/backend/internal/scenario"
)

// registerTestRoutes wires vision-grounded test authoring, execution, and
// reports, guarded by PermissionTest.
func (s *Server) registerTestRoutes() {
	t := func(h http.HandlerFunc) http.HandlerFunc { return s.require(domain.PermissionTest, h) }
	m := s.mux
	m.HandleFunc("GET /api/v1/tests", t(s.listTests))
	m.HandleFunc("POST /api/v1/tests", t(s.createTest))
	m.HandleFunc("DELETE /api/v1/tests/{id}", t(s.deleteTest))
	m.HandleFunc("POST /api/v1/tests/{id}/run", t(s.runTest))
	m.HandleFunc("GET /api/v1/tests/runs", t(s.listTestRuns))
	m.HandleFunc("GET /api/v1/tests/runs/{id}", t(s.getTestRun))
	m.HandleFunc("GET /api/v1/tests/runs/{id}/artifacts/{name}", t(s.testArtifact))
}

func (s *Server) listTests(w http.ResponseWriter, r *http.Request) {
	tests, err := s.store.ListTests(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tests)
}

func (s *Server) createTest(w http.ResponseWriter, r *http.Request) {
	var req domain.CreateTestRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, badRequest("test name is required"))
		return
	}
	test, err := s.store.CreateTest(r.Context(), req)
	if err != nil {
		writeError(w, badRequest(err.Error()))
		return
	}
	principal, _ := principalFromContext(r.Context())
	s.audit(r, principal, "create_test", "test", test.ID, "succeeded", test.Name)
	writeJSON(w, http.StatusCreated, test)
}

func (s *Server) deleteTest(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.DeleteTest(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	principal, _ := principalFromContext(r.Context())
	s.audit(r, principal, "delete_test", "test", id, "succeeded", "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) runTest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		InstanceID string `json:"instanceId"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	if strings.TrimSpace(req.InstanceID) == "" {
		writeError(w, badRequest("instanceId is required"))
		return
	}
	run, err := s.tests.Start(r.Context(), r.PathValue("id"), req.InstanceID)
	if err != nil {
		writeError(w, badRequest(err.Error()))
		return
	}
	principal, _ := principalFromContext(r.Context())
	s.audit(r, principal, "run_test", "test", run.TestID, "accepted", run.ID)
	writeJSON(w, http.StatusAccepted, run)
}

func (s *Server) listTestRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := s.store.ListTestRuns(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, runs)
}

func (s *Server) getTestRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.store.GetTestRun(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

// testArtifact serves a run's evidence files (per-step PNGs, session MP4) from
// the artifact root. The name is constrained to a flat basename inside the
// run's own directory.
func (s *Server) testArtifact(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	name := r.PathValue("name")
	if name != filepath.Base(name) || strings.HasPrefix(name, ".") {
		writeError(w, badRequest("invalid artifact name"))
		return
	}
	// Existence check via the run record avoids serving arbitrary directories.
	if _, err := s.store.GetTestRun(r.Context(), runID); err != nil {
		writeError(w, err)
		return
	}
	path := filepath.Join(scenario.ArtifactRoot(), runID, name)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, path)
}
