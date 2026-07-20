package api

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// captureServer stands in for an attacker-named endpoint and records everything
// it receives, so a test can assert the stored key never reached it.
type captureServer struct {
	mu      sync.Mutex
	seen    []string
	srv     *httptest.Server
	touched bool
}

func newCaptureServer(t *testing.T) *captureServer {
	t.Helper()
	c := &captureServer{}
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.mu.Lock()
		c.touched = true
		for name, values := range r.Header {
			for _, v := range values {
				c.seen = append(c.seen, name+": "+v)
			}
		}
		c.mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	t.Cleanup(c.srv.Close)
	return c
}

func (c *captureServer) wasTouched() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.touched
}

func (c *captureServer) sawSecret(secret string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, line := range c.seen {
		if strings.Contains(line, secret) {
			return true
		}
	}
	return false
}

// The vulnerability: /agent/model/test used to decrypt the stored provider key
// and send it to whatever baseUrl the request named — turning "test connection"
// into key exfiltration (and an SSRF that carries a live credential).
func TestTestAgentModelDoesNotSendStoredKeyToAnotherHost(t *testing.T) {
	t.Setenv("OPENCUTTLES_SECURE_COOKIES", "0")
	t.Setenv("OPENCUTTLES_BOOTSTRAP_TOKEN", "")
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	t.Setenv("OPENCUTTLES_SECRET_KEY", base64.StdEncoding.EncodeToString(key))

	handler := testServer(t)
	cookies := adminCookies(t, handler)

	post := func(path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		for _, c := range cookies {
			req.AddCookie(c)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	const secret = "sk-stored-provider-key"
	attacker := newCaptureServer(t)

	// Store a key against the real provider endpoint.
	if r := post("/api/v1/agent/model",
		`{"providerId":"openai","api":"openai-responses","baseUrl":"https://api.openai.com/v1","model":"gpt-4o-mini","apiKey":"`+secret+`"}`,
	); r.Code != http.StatusOK {
		t.Fatalf("store model: %d %s", r.Code, r.Body.String())
	}

	// Now "test" against a DIFFERENT host, supplying no key of our own.
	probe := post("/api/v1/agent/model/test",
		`{"providerId":"custom","api":"openai-completions","baseUrl":"`+attacker.srv.URL+`","model":"whatever"}`)
	if probe.Code != http.StatusOK {
		t.Fatalf("probe: %d %s", probe.Code, probe.Body.String())
	}
	// Guard against a vacuous pass: the probe must actually have reached the
	// host, so that "no secret observed" means the key was withheld rather than
	// that no request was made at all.
	if !attacker.wasTouched() {
		t.Fatal("probe never reached the target; the secret assertion would be vacuous")
	}
	if attacker.sawSecret(secret) {
		t.Fatal("stored provider key was sent to a caller-supplied host")
	}
	if strings.Contains(probe.Body.String(), secret) {
		t.Fatalf("probe response echoed the stored key: %s", probe.Body.String())
	}
}

// The legitimate flow must keep working: re-testing the endpoint the key was
// saved against still uses the stored key.
func TestTestAgentModelReusesStoredKeyForSameEndpoint(t *testing.T) {
	t.Setenv("OPENCUTTLES_SECURE_COOKIES", "0")
	t.Setenv("OPENCUTTLES_BOOTSTRAP_TOKEN", "")
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	t.Setenv("OPENCUTTLES_SECRET_KEY", base64.StdEncoding.EncodeToString(key))

	handler := testServer(t)
	cookies := adminCookies(t, handler)

	post := func(path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		for _, c := range cookies {
			req.AddCookie(c)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	const secret = "sk-stored-provider-key"
	provider := newCaptureServer(t)

	if r := post("/api/v1/agent/model",
		`{"providerId":"custom","api":"openai-completions","baseUrl":"`+provider.srv.URL+`","model":"m","apiKey":"`+secret+`"}`,
	); r.Code != http.StatusOK {
		t.Fatalf("store model: %d %s", r.Code, r.Body.String())
	}

	// Same baseUrl, trailing slash — should still match and reuse the key.
	probe := post("/api/v1/agent/model/test",
		`{"providerId":"custom","api":"openai-completions","baseUrl":"`+provider.srv.URL+`/","model":"m"}`)
	if probe.Code != http.StatusOK {
		t.Fatalf("probe: %d %s", probe.Code, probe.Body.String())
	}
	if !provider.sawSecret(secret) {
		t.Fatal("stored key was not reused when testing its own endpoint")
	}
	var body map[string]any
	_ = json.Unmarshal(probe.Body.Bytes(), &body)
	if body["ok"] != true {
		t.Fatalf("probe against a reachable endpoint should succeed: %v", body)
	}
}

func TestSameEndpoint(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"https://api.openai.com/v1", "https://api.openai.com/v1", true},
		{"https://api.openai.com/v1", "https://api.openai.com/v1/", true},
		{"https://api.openai.com/v1", " https://api.openai.com/v1 ", true},
		{"https://API.OpenAI.com/v1", "https://api.openai.com/v1", true},
		{"https://api.openai.com/v1", "https://evil.example/v1", false},
		{"", "", false},
		{"", "https://evil.example/v1", false},
		{"https://api.openai.com/v1", "", false},
	}
	for _, tc := range cases {
		if got := sameEndpoint(tc.a, tc.b); got != tc.want {
			t.Errorf("sameEndpoint(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}
