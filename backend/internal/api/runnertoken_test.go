package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/opencuttles/opencuttles/backend/internal/domain"
)

// onboard registers a desktop device and returns its id and the one-time
// enrollment token.
func onboardDevice(t *testing.T, handler http.Handler, cookies []*http.Cookie, name string) (string, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances",
		bytes.NewBufferString(`{"name":"`+name+`","platform":"windows"}`))
	req.Header.Set("Content-Type", "application/json")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("onboard: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Instance        domain.Instance `json:"instance"`
		EnrollmentToken string          `json:"enrollmentToken"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode onboard response: %v", err)
	}
	if out.EnrollmentToken == "" || out.Instance.ID == "" {
		t.Fatalf("onboard returned no token or id: %s", rec.Body.String())
	}
	return out.Instance.ID, out.EnrollmentToken
}

func hashToken(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}

// The gap this closes: enrollment tokens were permanent and unrevocable. A
// leaked token granted screenshot and input on a real desktop forever, and the
// only remedy was deleting the device.
func TestRevokeRunnerTokenInvalidatesIt(t *testing.T) {
	t.Setenv("OPENCUTTLES_SECURE_COOKIES", "0")
	ctx := context.Background()

	handler, db := testServerWithStore(t)
	cookies := adminCookies(t, handler)
	id, token := onboardDevice(t, handler, cookies, "desk-1")

	// The token authenticates before revocation.
	if _, err := db.FindDesktopByTokenHash(ctx, hashToken(token)); err != nil {
		t.Fatalf("token should authenticate before revocation: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/instances/"+id+"/token", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke: %d %s", rec.Code, rec.Body.String())
	}

	// ...and not after.
	if _, err := db.FindDesktopByTokenHash(ctx, hashToken(token)); err == nil {
		t.Fatal("revoked token still authenticates")
	}
	// The device itself survives — revocation is not deletion.
	if _, err := db.GetInstance(ctx, id); err != nil {
		t.Fatalf("device was removed by a token revocation: %v", err)
	}
}

// A revoked device stores an empty hash. A runner presenting no token must not
// match it — otherwise one revocation would hand every revoked device to any
// caller sending an empty credential.
func TestEmptyTokenNeverMatchesARevokedDevice(t *testing.T) {
	t.Setenv("OPENCUTTLES_SECURE_COOKIES", "0")
	ctx := context.Background()

	handler, db := testServerWithStore(t)
	cookies := adminCookies(t, handler)
	id, _ := onboardDevice(t, handler, cookies, "desk-1")

	if ok, err := db.SetDesktopTokenHash(ctx, id, ""); err != nil || !ok {
		t.Fatalf("revoke: ok=%v err=%v", ok, err)
	}
	if _, err := db.FindDesktopByTokenHash(ctx, ""); err == nil {
		t.Fatal("an empty token matched a revoked device")
	}
}

func TestRotateRunnerTokenIssuesNewAndInvalidatesOld(t *testing.T) {
	t.Setenv("OPENCUTTLES_SECURE_COOKIES", "0")
	ctx := context.Background()

	handler, db := testServerWithStore(t)
	cookies := adminCookies(t, handler)
	id, oldToken := onboardDevice(t, handler, cookies, "desk-1")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances/"+id+"/token/rotate", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("rotate: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		EnrollmentToken string `json:"enrollmentToken"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.EnrollmentToken == "" {
		t.Fatalf("rotate returned no token: %s", rec.Body.String())
	}
	if out.EnrollmentToken == oldToken {
		t.Fatal("rotate reissued the same token")
	}

	if _, err := db.FindDesktopByTokenHash(ctx, hashToken(oldToken)); err == nil {
		t.Fatal("the old token still authenticates after rotation")
	}
	if _, err := db.FindDesktopByTokenHash(ctx, hashToken(out.EnrollmentToken)); err != nil {
		t.Fatalf("the new token does not authenticate: %v", err)
	}
}

// Rotating one device's credential must not disturb another's.
func TestRotateDoesNotAffectOtherDevices(t *testing.T) {
	t.Setenv("OPENCUTTLES_SECURE_COOKIES", "0")
	ctx := context.Background()

	handler, db := testServerWithStore(t)
	cookies := adminCookies(t, handler)
	idA, _ := onboardDevice(t, handler, cookies, "desk-a")
	_, tokenB := onboardDevice(t, handler, cookies, "desk-b")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances/"+idA+"/token/rotate", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("rotate: %d", rec.Code)
	}

	if _, err := db.FindDesktopByTokenHash(ctx, hashToken(tokenB)); err != nil {
		t.Fatalf("rotating device A invalidated device B's token: %v", err)
	}
}

func TestTokenRoutesRejectAndroidInstances(t *testing.T) {
	t.Setenv("OPENCUTTLES_SECURE_COOKIES", "0")

	handler, db := testServerWithStore(t)
	cookies := adminCookies(t, handler)

	android, err := db.CreateInstance(context.Background(), domain.CreateInstanceRequest{Name: "phone-1"})
	if err != nil {
		t.Fatalf("create android instance: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances/"+android.ID+"/token/rotate", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("rotate on an Android instance = %d %s, want 400", rec.Code, rec.Body.String())
	}
}
