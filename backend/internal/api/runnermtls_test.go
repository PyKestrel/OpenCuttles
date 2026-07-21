package api

import (
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/opencuttles/opencuttles/backend/internal/runnerca"
)

// enableMTLS turns the feature on and gives the server a secret key so the CA
// can be sealed at rest.
func enableMTLS(t *testing.T) {
	t.Helper()
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	t.Setenv("OPENCUTTLES_SECRET_KEY", base64.StdEncoding.EncodeToString(key))
	t.Setenv("OPENCUTTLES_RUNNER_MTLS_LISTEN", "0.0.0.0:8443")
	t.Setenv("OPENCUTTLES_SECURE_COOKIES", "0")
}

// onboardWithBundle enrolls a desktop device and returns its id, token, and
// issued client identity.
func onboardWithBundle(t *testing.T, handler http.Handler, cookies []*http.Cookie) (string, string, runnerca.Bundle) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances",
		strings.NewReader(`{"name":"desk-1","platform":"windows"}`))
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
		Instance struct {
			ID string `json:"id"`
		} `json:"instance"`
		EnrollmentToken string           `json:"enrollmentToken"`
		ClientBundle    *runnerca.Bundle `json:"clientBundle"`
		MTLSEndpoint    string           `json:"mtlsEndpoint"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.ClientBundle == nil {
		t.Fatalf("mTLS is enabled but enrollment returned no client bundle: %s", rec.Body.String())
	}
	return out.Instance.ID, out.EnrollmentToken, *out.ClientBundle
}

// mtlsTestServer starts the runner mux over TLS with client-cert verification,
// mirroring MTLSServer's configuration.
func mtlsTestServer(t *testing.T, srv *Server) *httptest.Server {
	t.Helper()
	ca, err := srv.runnerCA(t.Context())
	if err != nil {
		t.Fatalf("runner CA: %v", err)
	}
	ts := httptest.NewUnstartedServer(srv.runnerMux())
	ts.TLS = &tls.Config{
		MinVersion: tls.VersionTLS12,
		ClientAuth: tls.RequireAndVerifyClientCert,
		ClientCAs:  ca.Pool(),
	}
	ts.StartTLS()
	t.Cleanup(ts.Close)
	return ts
}

// clientFor builds an HTTP client presenting the given identity and trusting the
// test server.
func clientFor(t *testing.T, ts *httptest.Server, bundle *runnerca.Bundle) *http.Client {
	t.Helper()
	roots := x509.NewCertPool()
	roots.AddCert(ts.Certificate())
	cfg := &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	if bundle != nil {
		pair, err := tls.X509KeyPair([]byte(bundle.ClientCertPEM), []byte(bundle.ClientKeyPEM))
		if err != nil {
			t.Fatalf("client key pair: %v", err)
		}
		cfg.Certificates = []tls.Certificate{pair}
	}
	return &http.Client{Transport: &http.Transport{TLSClientConfig: cfg}}
}

func TestMTLSEnrollmentIssuesAUsableIdentity(t *testing.T) {
	enableMTLS(t)
	_, handler := newTestAPIServer(t)
	cookies := adminCookies(t, handler)
	_, _, bundle := onboardWithBundle(t, handler, cookies)

	if _, err := tls.X509KeyPair([]byte(bundle.ClientCertPEM), []byte(bundle.ClientKeyPEM)); err != nil {
		t.Fatalf("issued bundle is not a usable key pair: %v", err)
	}
	if bundle.CACertPEM == "" {
		t.Fatal("bundle carries no CA certificate")
	}
}

// The whole point of the feature: the correct token is not enough on its own.
func TestMTLSRejectsAValidTokenWithoutACertificate(t *testing.T) {
	enableMTLS(t)
	apiSrv, handler := newTestAPIServer(t)
	cookies := adminCookies(t, handler)
	_, token, _ := onboardWithBundle(t, handler, cookies)

	ts := mtlsTestServer(t, apiSrv)

	// No client certificate at all — the TLS handshake itself must fail.
	resp, err := clientFor(t, ts, nil).Get(ts.URL + "/api/v1/runner/stream")
	if err == nil {
		resp.Body.Close()
		t.Fatal("connected with no client certificate")
	}
	_ = token
}

// A certificate issued for one device must not authenticate as another. Without
// the CN binding, any enrolled machine could impersonate any other.
func TestMTLSRejectsAnotherDevicesCertificate(t *testing.T) {
	enableMTLS(t)
	apiSrv, handler := newTestAPIServer(t)
	cookies := adminCookies(t, handler)

	_, tokenA, _ := onboardWithBundle(t, handler, cookies)
	_, _, bundleB := onboardWithBundle(t, handler, cookies)

	ts := mtlsTestServer(t, apiSrv)

	// Device A's token with device B's certificate.
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/runner/build/nonexistent", nil)
	req.Header.Set("Authorization", "Bearer "+tokenA)
	resp, err := clientFor(t, ts, &bundleB).Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("mismatched cert/token = %d, want 401: %s", resp.StatusCode, body)
	}
}

// A certificate from a CA we do not control must not get through.
func TestMTLSRejectsAForeignCA(t *testing.T) {
	enableMTLS(t)
	apiSrv, handler := newTestAPIServer(t)
	cookies := adminCookies(t, handler)
	_, _, _ = onboardWithBundle(t, handler, cookies)

	foreignCertPEM, foreignKeyPEM, err := runnerca.Generate()
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := runnerca.Load(foreignCertPEM, foreignKeyPEM)
	if err != nil {
		t.Fatal(err)
	}
	impostor, err := foreign.Issue("dev_whatever")
	if err != nil {
		t.Fatal(err)
	}

	ts := mtlsTestServer(t, apiSrv)
	resp, err := clientFor(t, ts, &impostor).Get(ts.URL + "/api/v1/runner/stream")
	if err == nil {
		resp.Body.Close()
		t.Fatal("a certificate from a foreign CA was accepted")
	}
}

// With mTLS enabled, the plain (Caddy-fronted) listener must refuse runners.
// Otherwise an attacker holding a stolen token would just use that port and the
// certificate requirement would be decorative.
func TestMTLSClosesThePlaintextRunnerPath(t *testing.T) {
	enableMTLS(t)
	_, handler := newTestAPIServer(t)
	cookies := adminCookies(t, handler)
	_, token, _ := onboardWithBundle(t, handler, cookies)

	// Same token, but over the main handler where there is no client certificate.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/runner/build/anything", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("plaintext runner path = %d, want 401 while mTLS is enabled", rec.Code)
	}
}

// Disabled is the default, and must leave the existing token-only path working.
func TestMTLSDisabledLeavesTokenAuthAlone(t *testing.T) {
	t.Setenv("OPENCUTTLES_RUNNER_MTLS_LISTEN", "")
	t.Setenv("OPENCUTTLES_SECURE_COOKIES", "0")

	apiSrv, handler := newTestAPIServer(t)
	cookies := adminCookies(t, handler)

	if MTLSEnabled() {
		t.Fatal("mTLS should be off by default")
	}
	id, token := onboardDevice(t, handler, cookies, "desk-1")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/runner/build/anything", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	// 404/400 for the missing build is fine — anything but 401 proves the token
	// alone still authenticates.
	if rec.Code == http.StatusUnauthorized {
		t.Fatal("token-only auth broke while mTLS is disabled")
	}

	if bundle, err := apiSrv.issueRunnerBundle(t.Context(), id); err != nil || bundle != nil {
		t.Fatalf("disabled mTLS should issue no bundle: %v %v", bundle, err)
	}
}

func TestMTLSEndpointDerivation(t *testing.T) {
	t.Setenv("OPENCUTTLES_RUNNER_MTLS_LISTEN", "0.0.0.0:8443")
	t.Setenv("OPENCUTTLES_RUNNER_MTLS_URL", "")

	// The host comes from the configured origin, not the bind address — runners
	// must dial a reachable name, never 0.0.0.0.
	if got := mtlsEndpoint("https://appliance.example"); got != "https://appliance.example:8443" {
		t.Fatalf("mtlsEndpoint = %q", got)
	}
	if got := mtlsEndpoint("https://10.1.0.104"); got != "https://10.1.0.104:8443" {
		t.Fatalf("mtlsEndpoint with IP = %q", got)
	}
	// An explicit override wins, for port-forwarded deployments.
	t.Setenv("OPENCUTTLES_RUNNER_MTLS_URL", "https://runners.example:9443/")
	if got := mtlsEndpoint("https://appliance.example"); got != "https://runners.example:9443" {
		t.Fatalf("override ignored: %q", got)
	}
}
