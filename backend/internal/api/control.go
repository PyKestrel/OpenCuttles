package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/opencuttles/opencuttles/backend/internal/devicecontrol"
	"github.com/opencuttles/opencuttles/backend/internal/domain"
)

// registerControlRoutes wires the interactive device-control surface. These
// paths are nested under an instance and guarded by PermissionControl. They use
// Go 1.22 wildcard patterns ({id}); the more specific patterns win over the
// generic "/api/v1/instances/" prefix handler.
func (s *Server) registerControlRoutes() {
	c := func(h http.HandlerFunc) http.HandlerFunc { return s.require(domain.PermissionControl, h) }
	m := s.mux
	m.HandleFunc("GET /api/v1/instances/{id}/control/screenshot", c(s.controlScreenshot))
	m.HandleFunc("GET /api/v1/instances/{id}/control/ui-tree", c(s.controlUITree))
	m.HandleFunc("GET /api/v1/instances/{id}/control/apps", c(s.controlListApps))
	m.HandleFunc("GET /api/v1/instances/{id}/control/current-activity", c(s.controlCurrentActivity))
	m.HandleFunc("GET /api/v1/instances/{id}/control/perf", c(s.controlPerf))
	m.HandleFunc("GET /api/v1/instances/{id}/control/logcat", c(s.controlLogcat))
	m.HandleFunc("POST /api/v1/instances/{id}/control/input/{action}", c(s.controlInput))
	m.HandleFunc("POST /api/v1/instances/{id}/control/apps/launch", c(s.controlLaunchApp))
	m.HandleFunc("POST /api/v1/instances/{id}/control/apps/install", c(s.controlInstallApp))
	m.HandleFunc("POST /api/v1/instances/{id}/control/shell", c(s.controlShell))
	m.HandleFunc("POST /api/v1/instances/{id}/control/rotate", c(s.controlRotate))
}

// writeControlError maps devicecontrol sentinels to appropriate HTTP statuses.
func writeControlError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, devicecontrol.ErrExecutionDisabled):
		writeError(w, clientError{status: http.StatusServiceUnavailable, message: err.Error()})
	case errors.Is(err, devicecontrol.ErrNotRunning):
		writeError(w, clientError{status: http.StatusConflict, message: err.Error()})
	default:
		writeError(w, err)
	}
}

func (s *Server) controlScreenshot(w http.ResponseWriter, r *http.Request) {
	png, err := s.devices.Screenshot(r.Context(), r.PathValue("id"))
	if err != nil {
		writeControlError(w, err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(png)
}

func (s *Server) controlUITree(w http.ResponseWriter, r *http.Request) {
	tree, err := s.devices.UITree(r.Context(), r.PathValue("id"))
	if err != nil {
		writeControlError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tree)
}

func (s *Server) controlListApps(w http.ResponseWriter, r *http.Request) {
	thirdParty := r.URL.Query().Get("thirdParty") == "1"
	apps, err := s.devices.ListApps(r.Context(), r.PathValue("id"), thirdParty)
	if err != nil {
		writeControlError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"packages": apps})
}

func (s *Server) controlCurrentActivity(w http.ResponseWriter, r *http.Request) {
	activity, err := s.devices.CurrentActivity(r.Context(), r.PathValue("id"))
	if err != nil {
		writeControlError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"activity": activity})
}

func (s *Server) controlPerf(w http.ResponseWriter, r *http.Request) {
	snap, err := s.devices.Perf(r.Context(), r.PathValue("id"), r.URL.Query().Get("package"))
	if err != nil {
		writeControlError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

func (s *Server) controlLogcat(w http.ResponseWriter, r *http.Request) {
	lines, _ := strconv.Atoi(r.URL.Query().Get("lines"))
	out, err := s.devices.Logcat(r.Context(), r.PathValue("id"), lines)
	if err != nil {
		writeControlError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"logcat": out})
}

type inputRequest struct {
	X        int    `json:"x"`
	Y        int    `json:"y"`
	X2       int    `json:"x2"`
	Y2       int    `json:"y2"`
	Duration int    `json:"duration"`
	Text     string `json:"text"`
	Key      string `json:"key"`
}

func (s *Server) controlInput(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	action := r.PathValue("action")
	var req inputRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	var err error
	switch action {
	case "tap":
		err = s.devices.Tap(r.Context(), id, req.X, req.Y)
	case "swipe":
		err = s.devices.Swipe(r.Context(), id, req.X, req.Y, req.X2, req.Y2, req.Duration)
	case "longpress":
		err = s.devices.LongPress(r.Context(), id, req.X, req.Y, req.Duration)
	case "text":
		err = s.devices.Text(r.Context(), id, req.Text)
	case "key":
		err = s.devices.Key(r.Context(), id, req.Key)
	default:
		writeError(w, notFound("unknown input action"))
		return
	}
	if err != nil {
		writeControlError(w, err)
		return
	}
	s.auditControl(r, id, "input_"+action)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) controlLaunchApp(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Package string `json:"package"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	if strings.TrimSpace(req.Package) == "" {
		writeError(w, badRequest("package is required"))
		return
	}
	if err := s.devices.LaunchApp(r.Context(), id, req.Package); err != nil {
		writeControlError(w, err)
		return
	}
	s.auditControl(r, id, "launch_app")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// controlInstallApp accepts a multipart upload (field "apk"), stages it to a
// temp file on the host, and installs it via adb.
func (s *Server) controlInstallApp(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	// Bound the body before parsing: ParseMultipartForm's argument is only the
	// in-memory buffer, and anything beyond it spills to disk unbounded.
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes())
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		if isMaxBytesError(err) {
			writeError(w, clientError{
				status:  http.StatusRequestEntityTooLarge,
				message: "apk exceeds the upload size limit",
			})
			return
		}
		writeError(w, badRequest("expected multipart apk upload"))
		return
	}
	file, header, err := r.FormFile("apk")
	if err != nil {
		writeError(w, badRequest("missing apk file"))
		return
	}
	defer file.Close()
	if !strings.HasSuffix(strings.ToLower(header.Filename), ".apk") {
		writeError(w, badRequest("file must be an .apk"))
		return
	}
	tmp, err := os.CreateTemp("", "opencuttles-*.apk")
	if err != nil {
		writeError(w, err)
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := io.Copy(tmp, file); err != nil {
		_ = tmp.Close()
		writeError(w, err)
		return
	}
	_ = tmp.Close()
	if err := s.devices.InstallAPK(r.Context(), id, tmpPath); err != nil {
		writeControlError(w, err)
		return
	}
	s.auditControl(r, id, "install_apk")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "file": filepath.Base(header.Filename)})
}

func (s *Server) controlShell(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Command string `json:"command"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	if strings.TrimSpace(req.Command) == "" {
		writeError(w, badRequest("command is required"))
		return
	}
	out, err := s.devices.Shell(r.Context(), id, req.Command)
	if err != nil {
		writeControlError(w, err)
		return
	}
	s.auditControl(r, id, "shell")
	writeJSON(w, http.StatusOK, map[string]string{"output": out})
}

func (s *Server) controlRotate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Orientation int `json:"orientation"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	if err := s.devices.Rotate(r.Context(), id, req.Orientation); err != nil {
		writeControlError(w, err)
		return
	}
	s.auditControl(r, id, "rotate")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// decodeJSON decodes a small JSON body, tolerating an empty body (for actions
// whose parameters are all optional).
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(v); err != nil && !errors.Is(err, io.EOF) {
		return badRequest("invalid request payload")
	}
	return nil
}

// auditControl records a device-control action against the acting principal.
func (s *Server) auditControl(r *http.Request, instanceID, action string) {
	principal, _ := principalFromContext(r.Context())
	s.audit(r, principal, action, "instance", instanceID, "succeeded", "")
}
