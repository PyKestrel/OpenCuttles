package main

import (
	"net/http"
	"reflect"
	"testing"
	"time"
)

// The runner previously used bare &http.Client{} values, which fall back to
// http.DefaultTransport and therefore honor HTTP_PROXY/HTTPS_PROXY/NO_PROXY for
// free. Hand-building a Transport for TLS silently drops that, which would break
// every runner behind a corporate proxy — the exact environment an
// outbound-only agent is most likely to live in.
//
// This compares function identity rather than driving it with environment
// variables, because http.ProxyFromEnvironment reads the environment exactly
// once per process and caches it — so an env-based test passes or fails
// depending on which test ran first. The invariant worth pinning is simply that
// we wired the stdlib resolver rather than forgetting it; the resolver's own
// NO_PROXY handling is Go's to test, not ours.
func TestSharedTransportUsesProxyFromEnvironment(t *testing.T) {
	if sharedTransport.Proxy == nil {
		t.Fatal("sharedTransport has no Proxy func — every proxied deployment would break")
	}
	got := reflect.ValueOf(sharedTransport.Proxy).Pointer()
	want := reflect.ValueOf(http.ProxyFromEnvironment).Pointer()
	if got != want {
		t.Fatal("sharedTransport.Proxy is not http.ProxyFromEnvironment")
	}
}

// The stream is long-lived and must not carry a client deadline; the other two
// request classes must.
func TestHTTPClientTimeouts(t *testing.T) {
	stream, ok := httpClient(streamTimeout).(*http.Client)
	if !ok {
		t.Fatal("httpClient did not return an *http.Client")
	}
	if stream.Timeout != 0 {
		t.Fatalf("stream client timeout = %s, want 0 (SSE is long-lived)", stream.Timeout)
	}

	result := httpClient(resultTimeout).(*http.Client)
	if result.Timeout != resultTimeout {
		t.Fatalf("result client timeout = %s, want %s", result.Timeout, resultTimeout)
	}
	// A full-resolution screenshot on a slow uplink needs well over the old 30s.
	if resultTimeout < time.Minute {
		t.Fatalf("resultTimeout = %s is too tight for a large capture on a slow link", resultTimeout)
	}
}

func TestAllClientsShareOneTransport(t *testing.T) {
	a := httpClient(streamTimeout).(*http.Client)
	b := httpClient(resultTimeout).(*http.Client)
	c := httpClient(buildTimeout).(*http.Client)
	if a.Transport != b.Transport || b.Transport != c.Transport {
		t.Fatal("clients do not share a transport — TLS config would have to be set in three places")
	}
	if a.Transport != http.RoundTripper(sharedTransport) {
		t.Fatal("clients are not using sharedTransport")
	}
}
