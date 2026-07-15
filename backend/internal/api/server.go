package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/opencuttles/opencuttles/backend/internal/auth"
	"github.com/opencuttles/opencuttles/backend/internal/catalog"
	"github.com/opencuttles/opencuttles/backend/internal/devicecontrol"
	"github.com/opencuttles/opencuttles/backend/internal/domain"
	mcpserver "github.com/opencuttles/opencuttles/backend/internal/mcp"
	"github.com/opencuttles/opencuttles/backend/internal/orchestrator"
	"github.com/opencuttles/opencuttles/backend/internal/runnerhub"
	"github.com/opencuttles/opencuttles/backend/internal/scenario"
	"github.com/opencuttles/opencuttles/backend/internal/scheduler"
	"github.com/opencuttles/opencuttles/backend/internal/secretbox"
	"github.com/opencuttles/opencuttles/backend/internal/store"
	"github.com/opencuttles/opencuttles/backend/internal/vision"
	"github.com/opencuttles/opencuttles/backend/internal/web"
)

type Server struct {
	store         *store.SQLite
	orch          *orchestrator.Service
	auth          *auth.Service
	devices       *devicecontrol.Service
	logger        *slog.Logger
	mux           *http.ServeMux
	secureCookies bool
	allowedOrigin string
	authLimiter   *rateLimiter
	webAssets     fs.FS
	operatorPort  int
	mcpHandler    http.Handler
	mcp           *mcpserver.Service
	mcpToken      string
	agentTarget   string
	tests         *scenario.Runner
	cycles        *scenario.CycleExecutor
	secrets       *secretbox.Box
	runners       *runnerhub.Hub
}

// operatorPortFromEnv resolves the host-wide cuttlefish-operator HTTPS port.
// Newer Cuttlefish (>=1.x) serves on 1443; older builds used 8443. Override with
// OPENCUTTLES_OPERATOR_PORT.
func operatorPortFromEnv() int {
	if v := os.Getenv("OPENCUTTLES_OPERATOR_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 {
			return p
		}
	}
	return 1443
}

func NewServer(store *store.SQLite, orch *orchestrator.Service, authService *auth.Service, devices *devicecontrol.Service, logger *slog.Logger, secureCookies bool, allowedOrigin string) http.Handler {
	server := &Server{
		store:         store,
		orch:          orch,
		auth:          authService,
		devices:       devices,
		logger:        logger,
		mux:           http.NewServeMux(),
		secureCookies: secureCookies,
		allowedOrigin: allowedOrigin,
		authLimiter:   newRateLimiter(),
	}
	if assets, ok := web.Assets(); ok {
		server.webAssets = assets
	}
	server.operatorPort = operatorPortFromEnv()
	visionClient := vision.NewFromEnv()
	server.mcp = mcpserver.New(devices, store, visionClient, logger)
	server.mcp.SetSink(store) // report_step_result → store.AppendStep
	server.mcpHandler = server.mcp.Handler()
	server.tests = scenario.New(store, devices, visionClient, logger)
	// Desktop runner tunnel: authenticate runners by enrollment token, flip the
	// device online/offline as they connect/drop, and let devicecontrol drive
	// them over the tunnel.
	server.runners = runnerhub.New()
	server.runners.TokenAuth = server.authenticateRunner
	server.runners.OnOnline = func(deviceID string, online bool) {
		state := domain.StateOffline
		if online {
			state = domain.StateOnline
		}
		if _, err := store.UpdateInstanceState(context.Background(), deviceID, state, ""); err != nil && logger != nil {
			logger.Warn("runner state update failed", "device", deviceID, "online", online, "error", err)
		}
	}
	devices.SetRunners(server.runners)
	server.mcpToken = os.Getenv("OPENCUTTLES_MCP_TOKEN")
	// Optional at-rest encryption for stored secrets (agent provider API keys).
	// Absent key → secret storage stays disabled; keyless providers still work.
	if box, err := secretbox.New(os.Getenv("OPENCUTTLES_SECRET_KEY")); err == nil {
		server.secrets = box
	} else if !errors.Is(err, secretbox.ErrNoKey) && logger != nil {
		logger.Warn("OPENCUTTLES_SECRET_KEY invalid; agent API-key storage disabled", "error", err)
	}
	server.agentTarget = os.Getenv("OPENCUTTLES_AGENT_URL")
	if server.agentTarget == "" {
		server.agentTarget = "http://127.0.0.1:8790"
	}
	// Agent-driven test-cycle executor: fans a cycle out to a headless agent run
	// per case, capturing per-step evidence via the report_step_result MCP tool.
	server.cycles = scenario.NewCycleExecutor(store, devices, server.mcp, server.agentTarget, logger)
	// Cron scheduler: fires due cycles for the lifetime of the process.
	go scheduler.New(store, server.cycles, logger).Run(context.Background())
	server.routes()
	return server.withMiddleware(server.mux)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/v1/healthz", s.health)
	s.mux.HandleFunc("GET /api/v1/bootstrap", s.bootstrapStatus)
	s.mux.HandleFunc("POST /api/v1/bootstrap", s.bootstrapAdmin)
	s.mux.HandleFunc("POST /api/v1/auth/login", s.login)
	s.mux.HandleFunc("POST /api/v1/auth/logout", s.require(domain.PermissionView, s.logout))
	s.mux.HandleFunc("GET /api/v1/auth/me", s.require(domain.PermissionView, s.me))
	s.mux.HandleFunc("GET /api/v1/host", s.require(domain.PermissionView, s.host))
	s.mux.HandleFunc("GET /api/v1/health", s.require(domain.PermissionView, s.deepHealth))
	s.mux.HandleFunc("GET /api/v1/metrics", s.require(domain.PermissionView, s.metrics))
	s.mux.HandleFunc("GET /api/v1/android-versions", s.require(domain.PermissionView, s.listAndroidVersions))
	s.mux.HandleFunc("GET /api/v1/images", s.require(domain.PermissionView, s.listImages))
	s.mux.HandleFunc("POST /api/v1/images", s.require(domain.PermissionOperate, s.createImage))
	s.mux.HandleFunc("GET /api/v1/instances", s.require(domain.PermissionView, s.listInstances))
	s.mux.HandleFunc("POST /api/v1/instances", s.require(domain.PermissionOperate, s.createInstance))
	s.mux.HandleFunc("GET /api/v1/instances/", s.require(domain.PermissionView, s.instanceRoute))
	s.mux.HandleFunc("POST /api/v1/instances/", s.require(domain.PermissionOperate, s.instanceRoute))
	s.mux.HandleFunc("DELETE /api/v1/instances/", s.require(domain.PermissionOperate, s.instanceRoute))
	// Interactive device control (input, screenshot, apps, shell) nested under
	// an instance; guarded by PermissionControl.
	s.registerControlRoutes()
	// Vision-grounded test authoring/execution/reports; guarded by PermissionTest.
	s.registerTestRoutes()
	s.registerCaseRoutes()
	// MCP endpoint: device tools for the local agent. Authenticated by the
	// service token (OPENCUTTLES_MCP_TOKEN) or a session with the control
	// permission. The streamable handler owns method routing under this path.
	s.mux.Handle("/api/v1/mcp", s.mcpAuth(s.mcpHandler))
	s.mux.Handle("/api/v1/mcp/", s.mcpAuth(s.mcpHandler))
	// Desktop runner dial-home tunnel. Authenticated by the per-device enrollment
	// token inside the hub (not a browser session): the runner opens the SSE
	// stream and POSTs command results back.
	s.mux.HandleFunc("GET /api/v1/runner/stream", s.runners.StreamHandler)
	s.mux.HandleFunc("POST /api/v1/runner/result", s.runners.ResultHandler)
	s.mux.HandleFunc("GET /api/v1/runner/build/{id}", s.runnerBuildArtifact)
	// Flue agent sidecar: reverse-proxy its HTTP endpoints (POST invoke + GET
	// event stream at /agents/<name>/<id>) so the SPA reaches it same-origin.
	// Guarded by the control permission; SSE streaming is flushed immediately.
	s.mux.HandleFunc("/agents/", s.require(domain.PermissionControl, s.agentProxy))
	// Agent model configuration. Admin-only read/write (API keys are write-only
	// and never returned). The runtime endpoint returns the effective config
	// including the decrypted key and is guarded by the service token ONLY (no
	// browser session), so keys never reach a user agent.
	s.mux.HandleFunc("GET /api/v1/agent/model", s.require(domain.PermissionAdmin, s.getAgentModel))
	s.mux.HandleFunc("POST /api/v1/agent/model", s.require(domain.PermissionAdmin, s.putAgentModel))
	s.mux.HandleFunc("POST /api/v1/agent/model/test", s.require(domain.PermissionAdmin, s.testAgentModel))
	s.mux.HandleFunc("GET /api/v1/agent/runtime", s.serviceTokenOnly(s.getAgentRuntime))
	s.mux.HandleFunc("GET /api/v1/operations", s.require(domain.PermissionView, s.listOperations))
	s.mux.HandleFunc("GET /api/v1/audit", s.require(domain.PermissionAdmin, s.listAudit))
	// Cuttlefish-operator WebRTC signaling endpoints. The operator's client
	// (client.html) builds these as root-absolute URLs against the page origin
	// (see server_connector.js: httpUrl() and the wss .../devices/<id>/connect),
	// so they land on OpenCuttles rather than the operator. Reverse-proxy them to
	// the operator (WebSocket-aware) so the embedded console can connect.
	s.mux.HandleFunc("/infra_config", s.require(domain.PermissionOpenConsole, s.operatorProxy))
	s.mux.HandleFunc("/polled_connections", s.require(domain.PermissionOpenConsole, s.operatorProxy))
	s.mux.HandleFunc("/polled_connections/", s.require(domain.PermissionOpenConsole, s.operatorProxy))
	s.mux.HandleFunc("/devices/", s.require(domain.PermissionOpenConsole, s.operatorProxy))
	// SPA + static assets (embedded). Least-specific pattern, so it only
	// catches paths not handled by the /api routes above.
	s.mux.HandleFunc("/", s.serveStatic)
}

// operatorProxy reverse-proxies a request to the host-wide cuttlefish-operator,
// preserving the path. Used for the WebRTC signaling endpoints the operator's
// client references at the origin root (/infra_config, /polled_connections,
// /devices/<id>/connect). WebSocket upgrades are handled by ReverseProxy.
func (s *Server) operatorProxy(w http.ResponseWriter, r *http.Request) {
	target, err := url.Parse("https://127.0.0.1:" + strconv.Itoa(s.operatorPort))
	if err != nil {
		writeError(w, err)
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = target.Host
	}
	proxy.ErrorHandler = func(rw http.ResponseWriter, _ *http.Request, perr error) {
		writeError(rw, clientError{status: http.StatusBadGateway, message: "operator unavailable: " + perr.Error()})
	}
	proxy.ServeHTTP(w, r)
}

// agentProxy reverse-proxies the Flue agent sidecar, preserving the /agents/...
// path. The sidecar streams agent events over SSE, so response buffering is
// disabled (FlushInterval -1) to forward tokens as they arrive.
func (s *Server) agentProxy(w http.ResponseWriter, r *http.Request) {
	target, err := url.Parse(s.agentTarget)
	if err != nil {
		writeError(w, err)
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.FlushInterval = -1
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = target.Host
	}
	proxy.ErrorHandler = func(rw http.ResponseWriter, _ *http.Request, perr error) {
		writeError(rw, clientError{status: http.StatusBadGateway, message: "agent sidecar unavailable: " + perr.Error()})
	}
	proxy.ServeHTTP(w, r)
}

// serveStatic serves the embedded single-page app. Unknown non-API paths fall
// back to index.html so client-side routing works.
func (s *Server) serveStatic(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeError(w, notFound("route not found"))
		return
	}
	if s.webAssets == nil {
		writeError(w, notFound("frontend is not built into this binary"))
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, notFound("route not found"))
		return
	}

	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name == "" {
		name = "index.html"
	}
	data, err := fs.ReadFile(s.webAssets, name)
	if err != nil {
		// SPA fallback for client-side routes.
		name = "index.html"
		data, err = fs.ReadFile(s.webAssets, name)
		if err != nil {
			writeError(w, notFound("not found"))
			return
		}
	}

	ctype := mime.TypeByExtension(path.Ext(name))
	if ctype == "" {
		ctype = http.DetectContentType(data)
	}
	w.Header().Set("Content-Type", ctype)
	if name == "index.html" {
		w.Header().Set("Cache-Control", "no-cache")
	} else if strings.HasPrefix(name, "assets/") {
		// Vite emits content-hashed filenames under assets/.
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	_, _ = w.Write(data)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) bootstrapStatus(w http.ResponseWriter, r *http.Request) {
	required, err := s.auth.BootstrapRequired(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, domain.BootstrapStatus{Required: required})
}

func (s *Server) bootstrapAdmin(w http.ResponseWriter, r *http.Request) {
	if !s.authLimiter.allow("bootstrap:" + clientIP(r)) {
		writeError(w, clientError{status: http.StatusTooManyRequests, message: "too many bootstrap attempts"})
		return
	}
	var req domain.BootstrapAdminRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, badRequest("invalid bootstrap payload"))
		return
	}
	if err := validateBootstrapToken(req.Token); err != nil {
		writeError(w, clientError{status: http.StatusForbidden, message: err.Error()})
		return
	}
	principal, err := s.auth.BootstrapAdmin(r.Context(), req)
	if err != nil {
		writeError(w, badRequest(err.Error()))
		return
	}
	s.audit(r, principal, "bootstrap_admin", "user", principal.UserID, "succeeded", "local admin created")
	writeJSON(w, http.StatusCreated, principal)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !s.authLimiter.allow("login:" + clientIP(r)) {
		writeError(w, clientError{status: http.StatusTooManyRequests, message: "too many login attempts"})
		return
	}
	var req domain.LoginRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, badRequest("invalid login payload"))
		return
	}
	token, response, err := s.auth.Login(r.Context(), req)
	if err != nil {
		s.audit(r, domain.Principal{Username: req.Username}, "login", "session", "", "failed", "invalid credentials")
		writeError(w, clientError{status: http.StatusUnauthorized, message: "invalid credentials"})
		return
	}
	auth.SetSessionCookie(w, token, response.ExpiresAt, s.secureCookies)
	s.audit(r, response.Principal, "login", "session", "", "succeeded", "session created")
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	_, token, _ := s.auth.AuthenticateRequest(r.Context(), r)
	_ = s.auth.Logout(r.Context(), token)
	auth.ClearSessionCookie(w, s.secureCookies)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromContext(r.Context())
	if !ok {
		writeError(w, clientError{status: http.StatusUnauthorized, message: "unauthorized"})
		return
	}
	writeJSON(w, http.StatusOK, principal)
}

func (s *Server) host(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.orch.Host(r.Context()))
}

func (s *Server) deepHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.orch.Health(r.Context()))
}

func (s *Server) metrics(w http.ResponseWriter, r *http.Request) {
	host := s.orch.Host(r.Context())
	instances, err := s.store.ListInstances(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	running := 0
	for _, instance := range instances {
		if instance.State == domain.StateRunning {
			running++
		}
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	_, _ = fmt.Fprintf(w, "opencuttles_host_cpu_count %d\n", host.CPUCount)
	_, _ = fmt.Fprintf(w, "opencuttles_instances_total %d\n", len(instances))
	_, _ = fmt.Fprintf(w, "opencuttles_instances_running %d\n", running)
}

func (s *Server) listAndroidVersions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, catalog.AndroidVersions())
}

func (s *Server) listImages(w http.ResponseWriter, r *http.Request) {
	images, err := s.store.ListImages(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, images)
}

func (s *Server) createImage(w http.ResponseWriter, r *http.Request) {
	var req domain.CreateImageRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, badRequest("invalid image payload"))
		return
	}
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Path) == "" {
		writeError(w, badRequest("image name and path are required"))
		return
	}
	image, err := s.store.CreateImage(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	principal, _ := principalFromContext(r.Context())
	s.audit(r, principal, "create_image", "image", image.ID, "succeeded", image.Name)
	writeJSON(w, http.StatusCreated, image)
}

func (s *Server) listInstances(w http.ResponseWriter, r *http.Request) {
	instances, err := s.store.ListInstances(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, instances)
}

func (s *Server) createInstance(w http.ResponseWriter, r *http.Request) {
	var req domain.CreateInstanceRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, badRequest("invalid instance payload"))
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, badRequest("instance name is required"))
		return
	}
	// Desktop targets are onboarded (not provisioned): register the device and
	// return a one-time enrollment token instead of deploying a Cuttlefish VM.
	if isDesktopPlatform(req.Platform) {
		s.onboardDesktop(w, r, req)
		return
	}
	// Resolve a chosen Android version into a backing image (fetched on deploy).
	if strings.TrimSpace(req.ImageID) == "" && strings.TrimSpace(req.AndroidVersion) != "" {
		version, ok := catalog.Lookup(req.AndroidVersion)
		if !ok {
			writeError(w, badRequest("unknown android version"))
			return
		}
		image, err := s.store.GetOrCreateVersionImage(r.Context(), version.ID, version.Label, catalog.DefaultBuild(version))
		if err != nil {
			writeError(w, err)
			return
		}
		req.ImageID = image.ID
	}
	instance, err := s.store.CreateInstance(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	principal, _ := principalFromContext(r.Context())
	// One-click: create == deploy. Kick off fetch + launch asynchronously.
	if _, err := s.orch.Deploy(r.Context(), instance.ID); err != nil {
		s.audit(r, principal, "deploy_instance", "instance", instance.ID, "failed", err.Error())
	} else {
		s.audit(r, principal, "create_instance", "instance", instance.ID, "accepted", instance.Name)
	}
	writeJSON(w, http.StatusCreated, instance)
}

// onboardDesktop registers a desktop target and returns the enrollment token
// exactly once. The runner presents this token to open the dial-home tunnel.
func (s *Server) onboardDesktop(w http.ResponseWriter, r *http.Request, req domain.CreateInstanceRequest) {
	token, err := newRunnerToken()
	if err != nil {
		writeError(w, err)
		return
	}
	sum := sha256.Sum256([]byte(token))
	instance, err := s.store.CreateDesktopInstance(r.Context(), req.Name, req.Platform, hex.EncodeToString(sum[:]))
	if err != nil {
		writeError(w, err)
		return
	}
	principal, _ := principalFromContext(r.Context())
	s.audit(r, principal, "onboard_device", "instance", instance.ID, "accepted", req.Platform)
	writeJSON(w, http.StatusCreated, map[string]any{
		"instance":        instance,
		"enrollmentToken": token,
	})
}

func isDesktopPlatform(p string) bool {
	switch p {
	case domain.PlatformWindows, domain.PlatformLinux, domain.PlatformMacOS:
		return true
	default:
		return false
	}
}

func isDesktopInstance(inst domain.Instance) bool {
	return isDesktopPlatform(inst.Platform)
}

// newRunnerToken returns a 32-byte random enrollment token as hex.
func newRunnerToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// authenticateRunner resolves a runner request's bearer/X-Runner-Token to its
// device id by matching the token hash. Wired into the runner hub.
func (s *Server) authenticateRunner(r *http.Request) (string, bool) {
	tok := runnerTokenFromRequest(r)
	if tok == "" {
		return "", false
	}
	sum := sha256.Sum256([]byte(tok))
	inst, err := s.store.FindDesktopByTokenHash(r.Context(), hex.EncodeToString(sum[:]))
	if err != nil {
		return "", false
	}
	return inst.ID, true
}

// runnerBuildArtifact serves an uploaded build artifact to an authenticated
// runner so it can install it locally (desktop build-install pull path).
func (s *Server) runnerBuildArtifact(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authenticateRunner(r); !ok {
		writeError(w, clientError{status: http.StatusUnauthorized, message: "runner token required"})
		return
	}
	build, err := s.store.GetBuild(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	if build.Path == "" {
		writeError(w, badRequest("build has no stored artifact"))
		return
	}
	w.Header().Set("Content-Disposition", "attachment; filename=\""+build.Filename+"\"")
	http.ServeFile(w, r, build.Path)
}

func runnerTokenFromRequest(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	return strings.TrimSpace(r.Header.Get("X-Runner-Token"))
}

func (s *Server) instanceRoute(w http.ResponseWriter, r *http.Request) {
	id, action := splitInstancePath(r.URL.Path)
	if id == "" {
		writeError(w, badRequest("missing instance id"))
		return
	}

	switch {
	case r.Method == http.MethodGet && action == "":
		instance, err := s.store.GetInstance(r.Context(), id)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, instance)
	case r.Method == http.MethodGet && action == "console":
		principal, _ := principalFromContext(r.Context())
		if !auth.HasPermission(principal, domain.PermissionOpenConsole) {
			writeError(w, clientError{status: http.StatusForbidden, message: "console access denied"})
			return
		}
		s.consoleProxy(w, r, id)
	case r.Method == http.MethodPost && action == "start":
		// Desktops come online via their runner dialing home, not a start command.
		if inst, err := s.store.GetInstance(r.Context(), id); err == nil && isDesktopInstance(inst) {
			writeJSON(w, http.StatusAccepted, map[string]any{"instance": inst})
			return
		}
		instance, operation, err := s.orch.StartInstance(r.Context(), id)
		if err != nil {
			writeError(w, err)
			return
		}
		principal, _ := principalFromContext(r.Context())
		s.audit(r, principal, "start_instance", "instance", id, "accepted", operation.ID)
		writeJSON(w, http.StatusAccepted, map[string]any{"instance": instance, "operation": operation})
	case r.Method == http.MethodPost && action == "stop":
		if inst, err := s.store.GetInstance(r.Context(), id); err == nil && isDesktopInstance(inst) {
			writeJSON(w, http.StatusAccepted, map[string]any{"instance": inst})
			return
		}
		instance, operation, err := s.orch.StopInstance(r.Context(), id)
		if err != nil {
			writeError(w, err)
			return
		}
		principal, _ := principalFromContext(r.Context())
		s.audit(r, principal, "stop_instance", "instance", id, "accepted", operation.ID)
		writeJSON(w, http.StatusAccepted, map[string]any{"instance": instance, "operation": operation})
	case r.Method == http.MethodDelete && action == "":
		// Desktops aren't provisioned, so delete just removes the registration.
		if inst, err := s.store.GetInstance(r.Context(), id); err == nil && isDesktopInstance(inst) {
			if err := s.store.DeleteInstance(r.Context(), id); err != nil {
				writeError(w, err)
				return
			}
			principal, _ := principalFromContext(r.Context())
			s.audit(r, principal, "delete_device", "instance", id, "succeeded", inst.Platform)
			writeJSON(w, http.StatusAccepted, map[string]any{"status": "deleted"})
			return
		}
		operation, err := s.orch.DeleteInstance(r.Context(), id)
		if err != nil {
			writeError(w, err)
			return
		}
		principal, _ := principalFromContext(r.Context())
		s.audit(r, principal, "delete_instance", "instance", id, "accepted", operation.ID)
		writeJSON(w, http.StatusAccepted, operation)
	default:
		writeError(w, notFound("route not found"))
	}
}

func (s *Server) listOperations(w http.ResponseWriter, r *http.Request) {
	operations, err := s.store.ListOperations(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, operations)
}

func (s *Server) listAudit(w http.ResponseWriter, r *http.Request) {
	events, err := s.store.ListAuditEvents(r.Context(), 100)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, events)
}

func (s *Server) consoleProxy(w http.ResponseWriter, r *http.Request, instanceID string) {
	instance, err := s.store.GetInstance(r.Context(), instanceID)
	if err != nil {
		writeError(w, err)
		return
	}
	if instance.State != domain.StateRunning {
		writeError(w, clientError{status: http.StatusConflict, message: "console is available only while the instance is running"})
		return
	}
	// The interactive console is served by the host-wide cuttlefish-operator on
	// an HTTPS port (self-signed; 1443 on current Cuttlefish), multiplexed per
	// device via deviceId. We reverse proxy it under this instance's console
	// prefix and reuse OpenCuttles auth.
	target, err := url.Parse("https://127.0.0.1:" + strconv.Itoa(s.operatorPort))
	if err != nil {
		writeError(w, err)
		return
	}
	prefix := "/api/v1/instances/" + instance.ID + "/console"
	principal, _ := principalFromContext(r.Context())
	if strings.HasSuffix(r.URL.Path, "/console") || strings.Contains(r.URL.Path, "client.html") {
		s.audit(r, principal, "open_console", "instance", instance.ID, "succeeded", "console proxied")
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		path := strings.TrimPrefix(r.URL.Path, prefix)
		if path == "" {
			path = "/"
		}
		req.URL.Path = path
		req.Host = target.Host
		// Disable upstream compression so response bodies can be rewritten.
		req.Header.Set("Accept-Encoding", "identity")
	}
	proxy.ModifyResponse = func(resp *http.Response) error {
		if strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
			return rewriteConsoleHTML(resp, prefix)
		}
		return nil
	}
	proxy.ErrorHandler = func(rw http.ResponseWriter, _ *http.Request, perr error) {
		writeError(rw, clientError{status: http.StatusBadGateway, message: "console backend unavailable: " + perr.Error()})
	}
	proxy.ServeHTTP(w, r)
}

// rewriteConsoleHTML makes the operator's root-absolute asset references resolve
// under the per-instance console prefix and injects a <base> for relative URLs.
func rewriteConsoleHTML(resp *http.Response, prefix string) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()

	// The operator serves the client page at /devices/<id>/files/client.html and
	// references its assets relatively, so they resolve correctly against the
	// proxied document URL without a <base> tag (injecting one would break them).
	// Only root-absolute references need rewriting to stay under this prefix.
	html := string(body)
	replacer := strings.NewReplacer(
		`src="/`, `src="`+prefix+`/`,
		`href="/`, `href="`+prefix+`/`,
		`src='/`, `src='`+prefix+`/`,
		`href='/`, `href='`+prefix+`/`,
	)
	html = replacer.Replace(html)

	buf := []byte(html)
	resp.Body = io.NopCloser(bytes.NewReader(buf))
	resp.ContentLength = int64(len(buf))
	resp.Header.Set("Content-Length", strconv.Itoa(len(buf)))
	resp.Header.Del("Content-Encoding")
	return nil
}

func (s *Server) withMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = newRequestID()
		}
		ctx := context.WithValue(r.Context(), requestIDKey{}, requestID)
		w.Header().Set("X-Request-ID", requestID)
		if s.allowedOrigin != "" {
			w.Header().Set("Access-Control-Allow-Origin", s.allowedOrigin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,DELETE,OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		start := time.Now()
		next.ServeHTTP(w, r.WithContext(ctx))
		if s.logger != nil {
			s.logger.Info("request", "method", r.Method, "path", r.URL.Path, "request_id", requestID, "duration", time.Since(start))
		}
	})
}

// mcpAuth guards the MCP endpoint. It accepts the service token (bearer or
// X-MCP-Token header) when OPENCUTTLES_MCP_TOKEN is configured, otherwise it
// falls back to a session with the control permission. This lets the headless
// Flue sidecar authenticate with a token while browser sessions still work.
func (s *Server) mcpAuth(next http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.mcpToken != "" && mcpTokenFromRequest(r) != "" &&
			subtle.ConstantTimeCompare([]byte(mcpTokenFromRequest(r)), []byte(s.mcpToken)) == 1 {
			next.ServeHTTP(w, r)
			return
		}
		principal, _, err := s.auth.AuthenticateRequest(r.Context(), r)
		if err == nil && auth.HasPermission(principal, domain.PermissionControl) {
			ctx := context.WithValue(r.Context(), principalKey{}, principal)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		writeError(w, clientError{status: http.StatusUnauthorized, message: "unauthorized"})
	}
}

// serviceTokenOnly guards endpoints that only the local sidecar may call —
// notably the agent runtime config, which returns a decrypted API key. Unlike
// mcpAuth it does NOT fall back to a browser session, so no user agent can pull
// the plaintext secret. Requires OPENCUTTLES_MCP_TOKEN to be configured.
func (s *Server) serviceTokenOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.mcpToken != "" && mcpTokenFromRequest(r) != "" &&
			subtle.ConstantTimeCompare([]byte(mcpTokenFromRequest(r)), []byte(s.mcpToken)) == 1 {
			next(w, r)
			return
		}
		writeError(w, clientError{status: http.StatusUnauthorized, message: "service token required"})
	}
}

// mcpTokenFromRequest reads the MCP service token from an Authorization bearer
// header or the X-MCP-Token header.
func mcpTokenFromRequest(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	return strings.TrimSpace(r.Header.Get("X-MCP-Token"))
}

func (s *Server) require(permission string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, _, err := s.auth.AuthenticateRequest(r.Context(), r)
		if err != nil {
			writeError(w, clientError{status: http.StatusUnauthorized, message: "unauthorized"})
			return
		}
		if !auth.HasPermission(principal, permission) {
			s.audit(r, principal, "authorize", "api", r.URL.Path, "denied", permission)
			writeError(w, clientError{status: http.StatusForbidden, message: "forbidden"})
			return
		}
		ctx := context.WithValue(r.Context(), principalKey{}, principal)
		next(w, r.WithContext(ctx))
	}
}

func splitInstancePath(path string) (string, string) {
	trimmed := strings.TrimPrefix(path, "/api/v1/instances/")
	parts := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(parts) == 0 {
		return "", ""
	}
	id := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}
	return id, action
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	var ce clientError
	switch {
	case errors.As(err, &ce):
		status = ce.status
	case store.IsNotFound(err):
		status = http.StatusNotFound
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

type clientError struct {
	status  int
	message string
}

func (e clientError) Error() string {
	return e.message
}

func badRequest(message string) error {
	return clientError{status: http.StatusBadRequest, message: message}
}

func notFound(message string) error {
	return clientError{status: http.StatusNotFound, message: message}
}

type principalKey struct{}
type requestIDKey struct{}

func principalFromContext(ctx context.Context) (domain.Principal, bool) {
	principal, ok := ctx.Value(principalKey{}).(domain.Principal)
	return principal, ok
}

func requestIDFromContext(ctx context.Context) string {
	value, _ := ctx.Value(requestIDKey{}).(string)
	return value
}

func (s *Server) audit(r *http.Request, principal domain.Principal, action, resource, resourceID, outcome, message string) {
	if _, err := s.store.CreateAuditEvent(r.Context(), domain.AuditEvent{
		ActorID:    principal.UserID,
		ActorName:  principal.Username,
		Action:     action,
		Resource:   resource,
		ResourceID: resourceID,
		Outcome:    outcome,
		Message:    message,
		SourceIP:   clientIP(r),
		UserAgent:  r.UserAgent(),
		RequestID:  requestIDFromContext(r.Context()),
	}); err != nil && s.logger != nil {
		s.logger.Error("audit event failed", "error", err, "action", action, "resource", resource, "resource_id", resourceID)
	}
}

func clientIP(r *http.Request) string {
	if os.Getenv("OPENCUTTLES_TRUST_PROXY_HEADERS") == "1" {
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			return strings.TrimSpace(strings.Split(forwarded, ",")[0])
		}
		if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
			return strings.TrimSpace(realIP)
		}
	}
	return r.RemoteAddr
}

func validateBootstrapToken(token string) error {
	expected := os.Getenv("OPENCUTTLES_BOOTSTRAP_TOKEN")
	if expected == "" {
		if os.Getenv("OPENCUTTLES_SECURE_COOKIES") == "0" {
			return nil
		}
		return fmt.Errorf("bootstrap token is not configured")
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(expected)) != 1 {
		return fmt.Errorf("invalid bootstrap token")
	}
	return nil
}

type rateLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{attempts: map[string][]time.Time{}}
}

func (r *rateLimiter) allow(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	windowStart := now.Add(-5 * time.Minute)
	attempts := r.attempts[key]
	filtered := attempts[:0]
	for _, attempt := range attempts {
		if attempt.After(windowStart) {
			filtered = append(filtered, attempt)
		}
	}
	if len(filtered) >= 10 {
		r.attempts[key] = filtered
		return false
	}
	r.attempts[key] = append(filtered, now)
	return true
}

func newRequestID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
