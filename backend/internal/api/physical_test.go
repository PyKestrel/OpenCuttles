package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/opencuttles/opencuttles/backend/internal/domain"
)

func registerPhysical(t *testing.T, handler http.Handler, cookies []*http.Cookie, name, target string) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"name":"` + name + `","platform":"android","source":"physical","adbTarget":"` + target + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// Registering a handset must not deploy anything and must not mint a
// credential: there is no VM to launch and no runner to install.
func TestRegisterPhysicalAndroid(t *testing.T) {
	t.Setenv("OPENCUTTLES_SECURE_COOKIES", "0")
	handler := testServer(t)
	cookies := adminCookies(t, handler)

	rec := registerPhysical(t, handler, cookies, "Pixel 8", "R5CT30ABCDE")
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", rec.Code, rec.Body.String())
	}

	var inst domain.Instance
	if err := json.Unmarshal(rec.Body.Bytes(), &inst); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if inst.Source() != domain.SourcePhysical {
		t.Fatalf("source = %q", inst.Source())
	}
	if inst.ADBTarget != "R5CT30ABCDE" {
		t.Fatalf("adbTarget = %q", inst.ADBTarget)
	}
	// A desktop onboard returns an enrollment token; a handset must not.
	if strings.Contains(rec.Body.String(), "enrollmentToken") {
		t.Fatalf("a physical device was issued an enrollment token: %s", rec.Body.String())
	}
}

func TestRegisterPhysicalRejectsABadTarget(t *testing.T) {
	t.Setenv("OPENCUTTLES_SECURE_COOKIES", "0")
	handler := testServer(t)
	cookies := adminCookies(t, handler)

	// A target adb would read as a flag must be refused at the API, not at
	// first use where it would surface as an inscrutable adb error.
	rec := registerPhysical(t, handler, cookies, "Pixel", "-e")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad target = %d %s, want 400", rec.Code, rec.Body.String())
	}
}

// Start and stop are meaningless for a device we do not provision, and delete
// must only deregister — it must not try to tear down a VM that isn't there.
func TestPhysicalDeviceLifecycleIsRegistrationOnly(t *testing.T) {
	t.Setenv("OPENCUTTLES_SECURE_COOKIES", "0")
	handler, db := testServerWithStore(t)
	cookies := adminCookies(t, handler)

	rec := registerPhysical(t, handler, cookies, "Pixel 8", "R5CT30ABCDE")
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", rec.Code, rec.Body.String())
	}
	var inst domain.Instance
	_ = json.Unmarshal(rec.Body.Bytes(), &inst)

	do := func(method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		for _, c := range cookies {
			req.AddCookie(c)
		}
		r := httptest.NewRecorder()
		handler.ServeHTTP(r, req)
		return r
	}

	for _, action := range []string{"start", "stop"} {
		got := do(http.MethodPost, "/api/v1/instances/"+inst.ID+"/"+action)
		if got.Code != http.StatusAccepted {
			t.Fatalf("%s = %d %s, want 202 no-op", action, got.Code, got.Body.String())
		}
	}

	del := do(http.MethodDelete, "/api/v1/instances/"+inst.ID)
	if del.Code != http.StatusAccepted {
		t.Fatalf("delete = %d %s", del.Code, del.Body.String())
	}
	if _, err := db.GetInstance(t.Context(), inst.ID); err == nil {
		t.Fatal("the device row survived deletion")
	}
}

// Registering with no source keeps the existing Cuttlefish path, so the fork
// cannot capture ordinary Android creation.
func TestCreateWithoutSourceStillDeploysCuttlefish(t *testing.T) {
	t.Setenv("OPENCUTTLES_SECURE_COOKIES", "0")
	handler, db := testServerWithStore(t)
	cookies := adminCookies(t, handler)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances",
		bytes.NewBufferString(`{"name":"cvd-1"}`))
	req.Header.Set("Content-Type", "application/json")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var inst domain.Instance
	_ = json.Unmarshal(rec.Body.Bytes(), &inst)

	stored, err := db.GetInstance(t.Context(), inst.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.Source() != domain.SourceCuttlefish {
		t.Fatalf("source = %q, want cuttlefish", stored.Source())
	}
	if !stored.IsProvisioned() {
		t.Fatal("a cuttlefish instance should be provisioned")
	}
}
