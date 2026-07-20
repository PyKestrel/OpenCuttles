package api

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/opencuttles/opencuttles/backend/internal/auth"
	"github.com/opencuttles/opencuttles/backend/internal/devicecontrol"
	"github.com/opencuttles/opencuttles/backend/internal/domain"
	"github.com/opencuttles/opencuttles/backend/internal/orchestrator"
	"github.com/opencuttles/opencuttles/backend/internal/store"
)

type noopRunner struct{}

func (noopRunner) Run(ctx context.Context, command string, args ...string) (orchestrator.CommandResult, error) {
	return orchestrator.CommandResult{Command: command, Args: args}, nil
}

func (noopRunner) RunInDir(ctx context.Context, _ string, command string, args ...string) (orchestrator.CommandResult, error) {
	return orchestrator.CommandResult{Command: command, Args: args}, nil
}

func (noopRunner) LookPath(command string) (string, error) {
	return "/usr/bin/" + command, nil
}

func TestProtectedRoutesRequireSession(t *testing.T) {
	handler := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/host", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestBootstrapLoginAndAccessHost(t *testing.T) {
	t.Setenv("OPENCUTTLES_SECURE_COOKIES", "0")
	t.Setenv("OPENCUTTLES_BOOTSTRAP_TOKEN", "")
	handler := testServer(t)

	bootstrap := httptest.NewRequest(http.MethodPost, "/api/v1/bootstrap", bytes.NewBufferString(`{"username":"admin","password":"very-strong-password"}`))
	bootstrap.Header.Set("Content-Type", "application/json")
	bootstrapRec := httptest.NewRecorder()
	handler.ServeHTTP(bootstrapRec, bootstrap)
	if bootstrapRec.Code != http.StatusCreated {
		t.Fatalf("bootstrap status = %d", bootstrapRec.Code)
	}

	login := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"admin","password":"very-strong-password"}`))
	login.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, login)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status = %d", loginRec.Code)
	}

	host := httptest.NewRequest(http.MethodGet, "/api/v1/host", nil)
	for _, cookie := range loginRec.Result().Cookies() {
		host.AddCookie(cookie)
	}
	hostRec := httptest.NewRecorder()
	handler.ServeHTTP(hostRec, host)
	if hostRec.Code != http.StatusOK {
		t.Fatalf("host status = %d", hostRec.Code)
	}
}

func TestAndroidVersionsEndpoint(t *testing.T) {
	t.Setenv("OPENCUTTLES_SECURE_COOKIES", "0")
	t.Setenv("OPENCUTTLES_BOOTSTRAP_TOKEN", "")
	handler := testServer(t)

	bootstrap := httptest.NewRequest(http.MethodPost, "/api/v1/bootstrap", bytes.NewBufferString(`{"username":"admin","password":"very-strong-password"}`))
	bootstrap.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(httptest.NewRecorder(), bootstrap)

	login := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"admin","password":"very-strong-password"}`))
	login.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, login)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status = %d", loginRec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/android-versions", nil)
	for _, cookie := range loginRec.Result().Cookies() {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("android-versions status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "aosp-main") {
		t.Fatalf("expected catalog to include aosp-main, got %s", rec.Body.String())
	}
}

// TestMetricsIncludesTestOutcomes checks /metrics reports the product's actual
// outcomes (pass/fail/blocked), not just host infrastructure — a dashboard has
// to be able to watch test health.
func TestMetricsIncludesTestOutcomes(t *testing.T) {
	t.Setenv("OPENCUTTLES_SECURE_COOKIES", "0")
	t.Setenv("OPENCUTTLES_BOOTSTRAP_TOKEN", "")
	handler, db := testServerWithStore(t)
	ctx := context.Background()

	tc, err := db.CreateTestCase(ctx, domain.TestCase{Summary: "Login works"})
	if err != nil {
		t.Fatalf("create case: %v", err)
	}
	cycle, err := db.CreateTestCycle(ctx, domain.TestCycle{Name: "Smoke", Platform: domain.PlatformAndroid, CaseIDs: []string{tc.ID}, Enabled: true})
	if err != nil {
		t.Fatalf("create cycle: %v", err)
	}
	run, err := db.CreateCycleRun(ctx, domain.CycleRun{CycleID: cycle.ID, Trigger: domain.CycleTriggerManual})
	if err != nil {
		t.Fatalf("create cycle run: %v", err)
	}
	finished := time.Now().UTC()
	run.Status = "failed"
	run.FinishedAt = &finished
	run.Totals = domain.CycleTotals{Cases: 3, Pass: 1, Fail: 1, Blocked: 1}
	if err := db.UpdateCycleRun(ctx, run); err != nil {
		t.Fatalf("update cycle run: %v", err)
	}

	body := scrapeMetrics(t, handler)
	for _, want := range []string{
		"opencuttles_test_cases_total 1",
		"opencuttles_test_cycles_total 1",
		"opencuttles_test_cycles_enabled 1",
		`opencuttles_cycle_last_run_status{status="failed"} 1`,
		`opencuttles_cycle_cases{result="pass"} 1`,
		`opencuttles_cycle_cases{result="fail"} 1`,
		`opencuttles_cycle_cases{result="blocked"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics missing %q\n---\n%s", want, body)
		}
	}
	// The host metrics must survive alongside the new ones.
	if !strings.Contains(body, "opencuttles_instances_total") {
		t.Errorf("host metrics lost: %s", body)
	}
}

// scrapeMetrics bootstraps an admin, logs in, and GETs /metrics.
func scrapeMetrics(t *testing.T, handler http.Handler) string {
	t.Helper()
	do := func(method, path, body string, cookies []*http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		for _, c := range cookies {
			req.AddCookie(c)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	if rec := do(http.MethodPost, "/api/v1/bootstrap", `{"username":"admin","password":"very-strong-password"}`, nil); rec.Code != http.StatusCreated {
		t.Fatalf("bootstrap status = %d", rec.Code)
	}
	login := do(http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"very-strong-password"}`, nil)
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d", login.Code)
	}
	rec := do(http.MethodGet, "/api/v1/metrics", "", login.Result().Cookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("metrics status = %d", rec.Code)
	}
	return rec.Body.String()
}

// TestRewriteConsoleHTML pins the console proxy's rewriting contract: only
// root-absolute asset references are re-pointed under the instance prefix.
// Relative references and the absence of a <base> tag both matter — the
// operator's client page resolves its assets relative to the proxied document
// URL, so injecting a <base> would break them.
func TestRewriteConsoleHTML(t *testing.T) {
	const prefix = "/api/v1/instances/abc/console"
	resp := &http.Response{
		Header: http.Header{"Content-Type": []string{"text/html"}},
		Body: io.NopCloser(strings.NewReader(
			`<html><head><title>console</title></head><body>` +
				`<script src="/js/app.js"></script><link href="/style.css">` +
				`<script src='/js/single.js'></script><img src="assets/logo.png">` +
				`</body></html>`,
		)),
	}
	if err := rewriteConsoleHTML(resp, prefix); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	html := string(body)

	// Root-absolute references are rewritten to stay under the proxy prefix.
	if !strings.Contains(html, `src="`+prefix+`/js/app.js"`) {
		t.Errorf("script src not rewritten: %s", html)
	}
	if !strings.Contains(html, `href="`+prefix+`/style.css"`) {
		t.Errorf("link href not rewritten: %s", html)
	}
	if !strings.Contains(html, `src='`+prefix+`/js/single.js'`) {
		t.Errorf("single-quoted src not rewritten: %s", html)
	}
	// Relative references must be left alone; they already resolve against the
	// proxied document URL.
	if !strings.Contains(html, `src="assets/logo.png"`) {
		t.Errorf("relative src should be untouched: %s", html)
	}
	// A <base> tag must never be injected — it would break those relative refs.
	if strings.Contains(html, "<base") {
		t.Errorf("a <base> tag must not be injected (it breaks relative assets): %s", html)
	}
	// The rewritten length must be advertised, or the proxied response truncates.
	if resp.Header.Get("Content-Length") != strconv.Itoa(len(body)) {
		t.Errorf("Content-Length = %q, want %d", resp.Header.Get("Content-Length"), len(body))
	}
}

func TestServeStaticSPA(t *testing.T) {
	srv := &Server{
		webAssets: fstest.MapFS{
			"index.html":    {Data: []byte("<!doctype html><title>OpenCuttles</title>")},
			"assets/app.js": {Data: []byte("console.log('hi')")},
		},
	}

	// Existing asset is served directly.
	rec := httptest.NewRecorder()
	srv.serveStatic(rec, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("asset status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "console.log") {
		t.Fatalf("asset body = %q", rec.Body.String())
	}

	// Unknown client-side route falls back to index.html.
	rec = httptest.NewRecorder()
	srv.serveStatic(rec, httptest.NewRequest(http.MethodGet, "/instances/abc", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("spa fallback status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "OpenCuttles") {
		t.Fatalf("spa fallback body = %q", rec.Body.String())
	}

	// Unknown API routes must not be swallowed by the SPA handler.
	rec = httptest.NewRecorder()
	srv.serveStatic(rec, httptest.NewRequest(http.MethodGet, "/api/v1/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("api 404 status = %d", rec.Code)
	}
}

func testServer(t *testing.T) http.Handler {
	t.Helper()
	handler, _ := testServerWithStore(t)
	return handler
}

// testServerWithStore is testServer but also hands back the store, for tests
// that need to seed data behind the API.
func testServerWithStore(t *testing.T) (http.Handler, *store.SQLite) {
	t.Helper()
	// Tests bootstrap their admin without a token. That bypass now requires an
	// explicit dev-mode opt-in, so a production install can't inherit it (see
	// TestBootstrapTokenBypassRequiresExplicitDevMode). Individual tests can
	// still override this with their own t.Setenv.
	t.Setenv("OPENCUTTLES_DEV_MODE", "1")

	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "opencuttles.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	authService := auth.NewService(db)
	orch := orchestrator.NewService(db, noopRunner{}, slog.Default())
	devices := devicecontrol.NewService(db, nil, slog.Default())
	return NewServer(db, orch, authService, devices, slog.Default(), false, ""), db
}
