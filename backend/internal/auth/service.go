package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"net/http"
	"strings"
	"time"

	"github.com/opencuttles/opencuttles/backend/internal/domain"
	"github.com/opencuttles/opencuttles/backend/internal/store"
)

const (
	SessionCookieName = "opencuttles_session"
	sessionDuration   = 12 * time.Hour
	pbkdf2Iterations  = 210000
	pbkdf2KeyLen      = 32
)

var ErrUnauthorized = errors.New("unauthorized")

type Service struct {
	store *store.SQLite
}

func NewService(store *store.SQLite) *Service {
	return &Service{store: store}
}

func (s *Service) BootstrapRequired(ctx context.Context) (bool, error) {
	count, err := s.store.UserCount(ctx)
	return count == 0, err
}

func (s *Service) BootstrapAdmin(ctx context.Context, req domain.BootstrapAdminRequest) (domain.Principal, error) {
	required, err := s.BootstrapRequired(ctx)
	if err != nil {
		return domain.Principal{}, err
	}
	if !required {
		return domain.Principal{}, errors.New("admin user already exists")
	}
	username := normalizeUsername(req.Username)
	if username == "" || len(req.Password) < 12 {
		return domain.Principal{}, errors.New("username and a 12 character password are required")
	}
	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" {
		displayName = username
	}
	passwordHash, err := HashPassword(req.Password)
	if err != nil {
		return domain.Principal{}, err
	}
	user, err := s.store.CreateUser(ctx, username, displayName, domain.RoleAdmin, passwordHash)
	if err != nil {
		return domain.Principal{}, err
	}
	return PrincipalForUser(user), nil
}

func (s *Service) Login(ctx context.Context, req domain.LoginRequest) (string, domain.LoginResponse, error) {
	user, err := s.store.GetUserByUsername(ctx, normalizeUsername(req.Username))
	if err != nil || user.Disabled {
		return "", domain.LoginResponse{}, ErrUnauthorized
	}
	if !VerifyPassword(req.Password, user.PasswordHash) {
		return "", domain.LoginResponse{}, ErrUnauthorized
	}
	token, err := randomToken(32)
	if err != nil {
		return "", domain.LoginResponse{}, err
	}
	expiresAt := time.Now().UTC().Add(sessionDuration)
	if _, err := s.store.CreateSession(ctx, user.ID, TokenHash(token), expiresAt); err != nil {
		return "", domain.LoginResponse{}, err
	}
	return token, domain.LoginResponse{Principal: PrincipalForUser(user), ExpiresAt: expiresAt}, nil
}

func (s *Service) AuthenticateRequest(ctx context.Context, r *http.Request) (domain.Principal, string, error) {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil || cookie.Value == "" {
		return domain.Principal{}, "", ErrUnauthorized
	}
	_, user, err := s.store.GetSessionUser(ctx, TokenHash(cookie.Value))
	if err != nil {
		return domain.Principal{}, "", ErrUnauthorized
	}
	return PrincipalForUser(user), cookie.Value, nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.store.DeleteSession(ctx, TokenHash(token))
}

func PrincipalForUser(user domain.User) domain.Principal {
	return domain.Principal{
		UserID:      user.ID,
		Username:    user.Username,
		DisplayName: user.DisplayName,
		Role:        user.Role,
		Permissions: PermissionsForRole(user.Role),
	}
}

func PermissionsForRole(role string) []string {
	switch role {
	case domain.RoleAdmin:
		return []string{domain.PermissionAdmin, domain.PermissionOperate, domain.PermissionOpenConsole, domain.PermissionControl, domain.PermissionTest, domain.PermissionView}
	case domain.RoleOperator:
		return []string{domain.PermissionOperate, domain.PermissionOpenConsole, domain.PermissionControl, domain.PermissionTest, domain.PermissionView}
	default:
		return []string{domain.PermissionView}
	}
}

func HasPermission(principal domain.Principal, permission string) bool {
	for _, candidate := range principal.Permissions {
		if candidate == permission || candidate == domain.PermissionAdmin {
			return true
		}
	}
	return false
}

func HashPassword(password string) (string, error) {
	salt, err := randomBytes(16)
	if err != nil {
		return "", err
	}
	key := pbkdf2Key([]byte(password), salt, pbkdf2Iterations, pbkdf2KeyLen, sha256.New)
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", pbkdf2Iterations, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

func VerifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	var iterations int
	if _, err := fmt.Sscanf(parts[1], "%d", &iterations); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	actual := pbkdf2Key([]byte(password), salt, iterations, len(expected), sha256.New)
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func TokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func SetSessionCookie(w http.ResponseWriter, token string, expiresAt time.Time, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func ClearSessionCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func normalizeUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

func randomToken(size int) (string, error) {
	b, err := randomBytes(size)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func randomBytes(size int) ([]byte, error) {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}

func pbkdf2Key(password, salt []byte, iter, keyLen int, h func() hash.Hash) []byte {
	prf := hmac.New(h, password)
	hashLen := prf.Size()
	numBlocks := (keyLen + hashLen - 1) / hashLen
	var out []byte
	var block [4]byte
	for i := 1; i <= numBlocks; i++ {
		block[0] = byte(i >> 24)
		block[1] = byte(i >> 16)
		block[2] = byte(i >> 8)
		block[3] = byte(i)
		prf.Reset()
		prf.Write(salt)
		prf.Write(block[:])
		u := prf.Sum(nil)
		t := append([]byte(nil), u...)
		for j := 1; j < iter; j++ {
			prf.Reset()
			prf.Write(u)
			u = prf.Sum(nil)
			for k := range t {
				t[k] ^= u[k]
			}
		}
		out = append(out, t...)
	}
	return out[:keyLen]
}
