package api

import (
	"bytes"
	"context"
	"crypto/rand"
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
	"github.com/opencuttles/opencuttles/backend/internal/domain"
	"github.com/opencuttles/opencuttles/backend/internal/orchestrator"
	"github.com/opencuttles/opencuttles/backend/internal/store"
	"github.com/opencuttles/opencuttles/backend/internal/web"
)

type Server struct {
	store         *store.SQLite
	orch          *orchestrator.Service
	auth          *auth.Service
	logger        *slog.Logger
	mux           *http.ServeMux
	secureCookies bool
	allowedOrigin string
	authLimiter   *rateLimiter
	webAssets     fs.FS
	operatorPort  int
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

func NewServer(store *store.SQLite, orch *orchestrator.Service, authService *auth.Service, logger *slog.Logger, secureCookies bool, allowedOrigin string) http.Handler {
	server := &Server{
		store:         store,
		orch:          orch,
		auth:          authService,
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
	s.mux.HandleFunc("GET /api/v1/operations", s.require(domain.PermissionView, s.listOperations))
	s.mux.HandleFunc("GET /api/v1/audit", s.require(domain.PermissionAdmin, s.listAudit))
	// SPA + static assets (embedded). Least-specific pattern, so it only
	// catches paths not handled by the /api routes above.
	s.mux.HandleFunc("/", s.serveStatic)
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
		instance, operation, err := s.orch.StartInstance(r.Context(), id)
		if err != nil {
			writeError(w, err)
			return
		}
		principal, _ := principalFromContext(r.Context())
		s.audit(r, principal, "start_instance", "instance", id, "accepted", operation.ID)
		writeJSON(w, http.StatusAccepted, map[string]any{"instance": instance, "operation": operation})
	case r.Method == http.MethodPost && action == "stop":
		instance, operation, err := s.orch.StopInstance(r.Context(), id)
		if err != nil {
			writeError(w, err)
			return
		}
		principal, _ := principalFromContext(r.Context())
		s.audit(r, principal, "stop_instance", "instance", id, "accepted", operation.ID)
		writeJSON(w, http.StatusAccepted, map[string]any{"instance": instance, "operation": operation})
	case r.Method == http.MethodDelete && action == "":
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
