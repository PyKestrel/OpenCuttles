package auth

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/opencuttles/opencuttles/backend/internal/domain"
	"github.com/opencuttles/opencuttles/backend/internal/store"
)

func TestBootstrapLoginAndAuthenticate(t *testing.T) {
	ctx := context.Background()
	db := openTestStore(t)
	service := NewService(db)

	required, err := service.BootstrapRequired(ctx)
	if err != nil {
		t.Fatalf("bootstrap required: %v", err)
	}
	if !required {
		t.Fatalf("bootstrap should be required for empty store")
	}

	principal, err := service.BootstrapAdmin(ctx, domain.BootstrapAdminRequest{
		Username:    "Admin",
		DisplayName: "Admin User",
		Password:    "very-strong-password",
	})
	if err != nil {
		t.Fatalf("bootstrap admin: %v", err)
	}
	if principal.Role != domain.RoleAdmin {
		t.Fatalf("role = %q", principal.Role)
	}

	token, login, err := service.Login(ctx, domain.LoginRequest{Username: "admin", Password: "very-strong-password"})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if login.Principal.Username != "admin" {
		t.Fatalf("username = %q", login.Principal.Username)
	}

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: token})
	authenticated, _, err := service.AuthenticateRequest(ctx, req)
	if err != nil {
		t.Fatalf("authenticate request: %v", err)
	}
	if authenticated.UserID != principal.UserID {
		t.Fatalf("authenticated user = %q, want %q", authenticated.UserID, principal.UserID)
	}
}

func TestPasswordHashVerification(t *testing.T) {
	hash, err := HashPassword("very-strong-password")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if !VerifyPassword("very-strong-password", hash) {
		t.Fatalf("expected password to verify")
	}
	if VerifyPassword("wrong-password", hash) {
		t.Fatalf("wrong password verified")
	}
}

func openTestStore(t *testing.T) *store.SQLite {
	t.Helper()
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "opencuttles.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	return db
}
