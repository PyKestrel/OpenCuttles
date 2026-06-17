package api

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
