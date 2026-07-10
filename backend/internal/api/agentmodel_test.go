package api

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func adminCookies(t *testing.T, handler http.Handler) []*http.Cookie {
	t.Helper()
	const creds = `{"username":"admin","password":"very-strong-password"}`
	bs := httptest.NewRequest(http.MethodPost, "/api/v1/bootstrap", bytes.NewBufferString(creds))
	bs.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, bs)
	if rec.Code != http.StatusCreated {
		t.Fatalf("bootstrap status = %d", rec.Code)
	}
	login := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(creds))
	login.Header.Set("Content-Type", "application/json")
	lrec := httptest.NewRecorder()
	handler.ServeHTTP(lrec, login)
	if lrec.Code != http.StatusOK {
		t.Fatalf("login status = %d", lrec.Code)
	}
	return lrec.Result().Cookies()
}

func TestAgentModelKeyStaysSecret(t *testing.T) {
	t.Setenv("OPENCUTTLES_SECURE_COOKIES", "0")
	t.Setenv("OPENCUTTLES_BOOTSTRAP_TOKEN", "")
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	t.Setenv("OPENCUTTLES_SECRET_KEY", base64.StdEncoding.EncodeToString(key))
	t.Setenv("OPENCUTTLES_MCP_TOKEN", "svc-token-123")

	handler := testServer(t)
	cookies := adminCookies(t, handler)

	do := func(method, path, body string, withCookies bool, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if withCookies {
			for _, c := range cookies {
				req.AddCookie(c)
			}
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	const secret = "sk-super-secret-key"

	// Admin sets a provider with an API key.
	set := do(http.MethodPost, "/api/v1/agent/model",
		`{"providerId":"openai","api":"openai-responses","baseUrl":"https://api.openai.com/v1","model":"gpt-4o-mini","apiKey":"`+secret+`"}`,
		true, "")
	if set.Code != http.StatusOK {
		t.Fatalf("put model: %d %s", set.Code, set.Body.String())
	}

	// Admin GET must report keySet but never echo the key.
	get := do(http.MethodGet, "/api/v1/agent/model", "", true, "")
	if get.Code != http.StatusOK {
		t.Fatalf("get model: %d", get.Code)
	}
	if strings.Contains(get.Body.String(), secret) {
		t.Fatalf("admin GET leaked the API key: %s", get.Body.String())
	}
	var gm map[string]any
	_ = json.Unmarshal(get.Body.Bytes(), &gm)
	if gm["keySet"] != true {
		t.Fatalf("keySet should be true, got %v", gm["keySet"])
	}

	// A logged-in admin session must NOT be able to read the plaintext via the
	// runtime endpoint (service-token only).
	if r := do(http.MethodGet, "/api/v1/agent/runtime", "", true, ""); r.Code != http.StatusUnauthorized {
		t.Fatalf("runtime via session = %d, want 401", r.Code)
	}

	// The sidecar (service token) gets the decrypted key.
	rt := do(http.MethodGet, "/api/v1/agent/runtime", "", false, "svc-token-123")
	if rt.Code != http.StatusOK {
		t.Fatalf("runtime: %d %s", rt.Code, rt.Body.String())
	}
	var rc map[string]any
	_ = json.Unmarshal(rt.Body.Bytes(), &rc)
	if rc["apiKey"] != secret {
		t.Fatalf("runtime apiKey = %v, want decrypted secret", rc["apiKey"])
	}
	if rc["configured"] != true {
		t.Fatalf("configured should be true")
	}

	// Updating without apiKey preserves the stored key and updates other fields.
	upd := do(http.MethodPost, "/api/v1/agent/model",
		`{"providerId":"openai","api":"openai-responses","baseUrl":"https://api.openai.com/v1","model":"gpt-4o"}`,
		true, "")
	if upd.Code != http.StatusOK {
		t.Fatalf("update: %d", upd.Code)
	}
	rt2 := do(http.MethodGet, "/api/v1/agent/runtime", "", false, "svc-token-123")
	var rc2 map[string]any
	_ = json.Unmarshal(rt2.Body.Bytes(), &rc2)
	if rc2["apiKey"] != secret {
		t.Fatalf("keyless update should preserve the key, got %v", rc2["apiKey"])
	}
	if rc2["model"] != "gpt-4o" {
		t.Fatalf("model not updated, got %v", rc2["model"])
	}
}
