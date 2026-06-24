package api

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencuttles/opencuttles/backend/internal/auth"
	"github.com/opencuttles/opencuttles/backend/internal/orchestrator"
	"github.com/opencuttles/opencuttles/backend/internal/store"
)

type noopRunner struct{}

func (noopRunner) Run(ctx context.Context, command string, args ...string) (orchestrator.CommandResult, error) {
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

func TestRewriteConsoleHTML(t *testing.T) {
	const prefix = "/api/v1/instances/abc/console"
	resp := &http.Response{
		Header: http.Header{"Content-Type": []string{"text/html"}},
		Body: io.NopCloser(strings.NewReader(
			`<html><head><title>console</title></head><body><script src="/js/app.js"></script><link href="/style.css"></body></html>`,
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
	if !strings.Contains(html, `<base href="`+prefix+`/">`) {
		t.Fatalf("missing base href: %s", html)
	}
	if !strings.Contains(html, `src="`+prefix+`/js/app.js"`) {
		t.Fatalf("script src not rewritten: %s", html)
	}
	if !strings.Contains(html, `href="`+prefix+`/style.css"`) {
		t.Fatalf("link href not rewritten: %s", html)
	}
}

func testServer(t *testing.T) http.Handler {
	t.Helper()
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "opencuttles.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	authService := auth.NewService(db)
	orch := orchestrator.NewService(db, noopRunner{}, slog.Default())
	return NewServer(db, orch, authService, slog.Default(), false, "")
}
