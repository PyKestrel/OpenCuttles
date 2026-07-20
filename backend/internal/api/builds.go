package api

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/opencuttles/opencuttles/backend/internal/domain"
	"github.com/opencuttles/opencuttles/backend/internal/scenario"
)

// buildExtensions lists accepted app-under-test artifact extensions per platform.
var buildExtensions = map[string][]string{
	domain.PlatformAndroid: {".apk"},
	domain.PlatformWindows: {".exe", ".msi"},
	domain.PlatformLinux:   {".deb", ".rpm", ".appimage", ".run", ".sh"},
	domain.PlatformMacOS:   {".dmg", ".pkg", ".zip"},
}

func (s *Server) listBuilds(w http.ResponseWriter, r *http.Request) {
	builds, err := s.store.ListBuilds(r.Context(), r.URL.Query().Get("platform"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, builds)
}

// uploadBuild stores an app-under-test artifact for a platform and triggers the
// on-new-build cycles for that platform.
func (s *Server) uploadBuild(w http.ResponseWriter, r *http.Request) {
	// Bound the body before parsing: ParseMultipartForm's argument is only the
	// in-memory buffer, and anything beyond it spills to disk unbounded.
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes())
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		if isMaxBytesError(err) {
			writeError(w, clientError{
				status:  http.StatusRequestEntityTooLarge,
				message: "build exceeds the upload size limit",
			})
			return
		}
		writeError(w, badRequest("invalid upload"))
		return
	}
	platform := strings.TrimSpace(r.FormValue("platform"))
	if platform == "" {
		platform = domain.PlatformAndroid
	}
	if _, ok := buildExtensions[platform]; !ok {
		writeError(w, badRequest("unknown platform "+platform))
		return
	}
	file, header, err := r.FormFile("artifact")
	if err != nil {
		writeError(w, badRequest("an 'artifact' file field is required"))
		return
	}
	defer file.Close()

	if !hasAllowedExt(header.Filename, buildExtensions[platform]) {
		writeError(w, badRequest("unexpected file type for "+platform+" (want "+strings.Join(buildExtensions[platform], "/")+")"))
		return
	}

	// Persist the artifact under the builds root (kept, unlike APK-install temps).
	build, err := s.store.CreateBuild(r.Context(), domain.Build{
		Platform: platform,
		Filename: filepath.Base(header.Filename),
		Version:  strings.TrimSpace(r.FormValue("version")),
		Note:     strings.TrimSpace(r.FormValue("note")),
	})
	if err != nil {
		writeError(w, err)
		return
	}
	dir := filepath.Join(scenario.ArtifactRoot(), "builds", build.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeError(w, err)
		return
	}
	dstPath := filepath.Join(dir, build.Filename)
	dst, err := os.Create(dstPath)
	if err != nil {
		writeError(w, err)
		return
	}
	size, copyErr := io.Copy(dst, file)
	_ = dst.Close()
	if copyErr != nil {
		writeError(w, copyErr)
		return
	}
	build.Path = dstPath
	build.SizeBytes = size
	_ = s.store.SetBuildLocation(r.Context(), build.ID, dstPath, size)

	principal, _ := principalFromContext(r.Context())
	s.audit(r, principal, "upload_build", "build", build.ID, "succeeded", build.Filename)

	// Fan out to on-new-build cycles (detached; each run resolves its own target).
	go s.triggerBuildCycles(context.Background(), build)

	writeJSON(w, http.StatusCreated, build)
}

func (s *Server) triggerBuildCycles(ctx context.Context, build domain.Build) {
	cycles, err := s.store.ListCyclesForBuild(ctx, build.Platform)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("list on-new-build cycles failed", "platform", build.Platform, "error", err)
		}
		return
	}
	for _, cycle := range cycles {
		target, err := s.resolveCycleTarget(ctx, cycle, "")
		if err != nil {
			if s.logger != nil {
				s.logger.Warn("no target for build-triggered cycle", "cycle", cycle.ID, "error", err)
			}
			continue
		}
		if _, err := s.cycles.Start(ctx, cycle.ID, target, domain.CycleTriggerBuild, build.ID); err != nil && s.logger != nil {
			s.logger.Warn("build-triggered cycle start failed", "cycle", cycle.ID, "error", err)
		}
	}
}

func hasAllowedExt(name string, exts []string) bool {
	lower := strings.ToLower(name)
	for _, e := range exts {
		if strings.HasSuffix(lower, e) {
			return true
		}
	}
	return false
}
