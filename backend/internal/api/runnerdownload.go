package api

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/opencuttles/opencuttles/backend/internal/auth"
	"github.com/opencuttles/opencuttles/backend/internal/domain"
	"github.com/opencuttles/opencuttles/backend/internal/runnerdist"
)

// authorizeRunnerDownload accepts either path that legitimately needs the binary:
// a dashboard session (the download button) or a valid device enrollment token
// (the one-line installer, run on the target machine where there is no cookie).
// The binary is generic — it carries no secret — so this is about gating access,
// not protecting the artifact.
func (s *Server) authorizeRunnerDownload(r *http.Request) bool {
	if _, ok := s.authenticateRunner(r); ok {
		return true
	}
	if principal, _, err := s.auth.AuthenticateRequest(r.Context(), r); err == nil && auth.HasPermission(principal, domain.PermissionOperate) {
		return true
	}
	return false
}

// listRunnerDownloads reports which prebuilt runner binaries this server bundles,
// so the onboarding UI can offer only the ones actually available (and prompt to
// build them if none are).
func (s *Server) listRunnerDownloads(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"runners": runnerdist.List()})
}

// downloadRunner streams the embedded runner binary for a platform (+ optional
// arch) as an attachment. Authorized by a session (button) or an enrollment
// token (one-line installer) — see authorizeRunnerDownload.
func (s *Server) downloadRunner(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeRunnerDownload(r) {
		writeError(w, clientError{status: http.StatusUnauthorized, message: "a dashboard session or a device enrollment token is required"})
		return
	}
	platform := strings.TrimSpace(r.URL.Query().Get("platform"))
	arch := strings.TrimSpace(r.URL.Query().Get("arch"))
	if platform == "" {
		writeError(w, badRequest("platform is required (windows, linux, or macos)"))
		return
	}
	f, target, err := runnerdist.Open(platform, arch)
	if err != nil {
		// No bundled runner is a build-time gap, not a bad request: report it as
		// unavailable with a clear message rather than a generic 400/500.
		writeError(w, clientError{status: http.StatusNotFound, message: err.Error()})
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+target.DownloadName+"\"")
	if target.SizeBytes > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(target.SizeBytes, 10))
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, f)
}
