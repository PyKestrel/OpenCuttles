package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The bug these guard: clientIP used to return r.RemoteAddr verbatim ("IP:port").
// Since the source port changes on every TCP connection, every request landed in
// its own rate-limit bucket and the login limiter never tripped.
func TestClientIPStripsPort(t *testing.T) {
	cases := []struct {
		name       string
		remoteAddr string
		want       string
	}{
		{"ipv4 with port", "203.0.113.9:54321", "203.0.113.9"},
		{"ipv6 with port", "[2001:db8::1]:443", "2001:db8::1"},
		{"no port", "203.0.113.9", "203.0.113.9"},
		{"bare ipv6", "2001:db8::1", "2001:db8::1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tc.remoteAddr
			if got := clientIP(r); got != tc.want {
				t.Fatalf("clientIP(%q) = %q, want %q", tc.remoteAddr, got, tc.want)
			}
		})
	}
}

func TestClientIPSamePeerDifferentPortsShareABucket(t *testing.T) {
	first := httptest.NewRequest(http.MethodGet, "/", nil)
	first.RemoteAddr = "203.0.113.9:1111"
	second := httptest.NewRequest(http.MethodGet, "/", nil)
	second.RemoteAddr = "203.0.113.9:2222"

	if clientIP(first) != clientIP(second) {
		t.Fatalf("same peer on different ports produced different keys: %q vs %q",
			clientIP(first), clientIP(second))
	}
}

func TestClientIPIgnoresProxyHeadersUnlessTrusted(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.9:54321"
	r.Header.Set("X-Forwarded-For", "198.51.100.7")
	r.Header.Set("X-Real-IP", "198.51.100.8")

	// OPENCUTTLES_TRUST_PROXY_HEADERS is unset here.
	if got := clientIP(r); got != "203.0.113.9" {
		t.Fatalf("untrusted proxy headers were honored: got %q", got)
	}
}

// With a proxy in front, only the LAST X-Forwarded-For element was observed by
// our own Caddy. Earlier elements are whatever the client sent, so trusting the
// first lets an attacker rotate them for a fresh rate-limit bucket per request
// and forge audit_events.source_ip.
func TestClientIPTrustedProxyUsesLastForwardedHop(t *testing.T) {
	t.Setenv("OPENCUTTLES_TRUST_PROXY_HEADERS", "1")

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "127.0.0.1:8080"
	r.Header.Set("X-Forwarded-For", "1.2.3.4, 5.6.7.8, 203.0.113.9")

	if got := clientIP(r); got != "203.0.113.9" {
		t.Fatalf("expected the last (proxy-appended) hop, got %q", got)
	}
}

func TestClientIPTrustedProxyFallsBackToRealIP(t *testing.T) {
	t.Setenv("OPENCUTTLES_TRUST_PROXY_HEADERS", "1")

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "127.0.0.1:8080"
	r.Header.Set("X-Real-IP", "198.51.100.7:9999")

	if got := clientIP(r); got != "198.51.100.7" {
		t.Fatalf("expected the X-Real-IP host, got %q", got)
	}
}

// An empty or whitespace-only header must not produce an empty key — that would
// collapse every such caller into one shared bucket.
func TestClientIPTrustedProxyIgnoresBlankHeaders(t *testing.T) {
	t.Setenv("OPENCUTTLES_TRUST_PROXY_HEADERS", "1")

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.9:54321"
	r.Header.Set("X-Forwarded-For", "   ")

	if got := clientIP(r); got != "203.0.113.9" {
		t.Fatalf("blank XFF should fall through to RemoteAddr, got %q", got)
	}
}

func TestRateLimiterTripsPerKey(t *testing.T) {
	rl := newRateLimiter()

	for i := 0; i < 10; i++ {
		if !rl.allow("login:203.0.113.9") {
			t.Fatalf("request %d was rejected before the cap", i+1)
		}
	}
	if rl.allow("login:203.0.113.9") {
		t.Fatal("11th request should have been rate limited")
	}
	// A different caller must be unaffected.
	if !rl.allow("login:198.51.100.7") {
		t.Fatal("a different IP was caught by another IP's limit")
	}
	// So must a different action from the same caller.
	if !rl.allow("bootstrap:203.0.113.9") {
		t.Fatal("a different action was caught by the login limit")
	}
}

// The bootstrap bypass used to key on OPENCUTTLES_SECURE_COOKIES=0, which
// quickstart sets for every IP-address / single-label-hostname install — a
// normal production mode. On such a host an empty token meant anyone reaching
// the port could claim the admin account.
func TestBootstrapTokenBypassRequiresExplicitDevMode(t *testing.T) {
	t.Run("insecure cookies alone does not authorize", func(t *testing.T) {
		t.Setenv("OPENCUTTLES_BOOTSTRAP_TOKEN", "")
		t.Setenv("OPENCUTTLES_SECURE_COOKIES", "0")
		t.Setenv("OPENCUTTLES_DEV_MODE", "")
		if err := validateBootstrapToken(""); err == nil {
			t.Fatal("an HTTP-mode production install accepted an empty bootstrap token")
		}
	})

	t.Run("explicit dev mode authorizes", func(t *testing.T) {
		t.Setenv("OPENCUTTLES_BOOTSTRAP_TOKEN", "")
		t.Setenv("OPENCUTTLES_DEV_MODE", "1")
		if err := validateBootstrapToken(""); err != nil {
			t.Fatalf("dev mode should allow tokenless bootstrap: %v", err)
		}
	})

	t.Run("a configured token still wins over dev mode", func(t *testing.T) {
		t.Setenv("OPENCUTTLES_BOOTSTRAP_TOKEN", "real-token")
		t.Setenv("OPENCUTTLES_DEV_MODE", "1")
		if err := validateBootstrapToken("wrong"); err == nil {
			t.Fatal("dev mode must not bypass a configured token")
		}
		if err := validateBootstrapToken("real-token"); err != nil {
			t.Fatalf("correct token rejected: %v", err)
		}
	})
}
