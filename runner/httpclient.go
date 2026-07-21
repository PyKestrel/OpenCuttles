package main

import (
	"net"
	"net/http"
	"time"
)

// Timeouts for the three kinds of request the runner makes.
const (
	// streamTimeout is zero on purpose: the SSE stream is long-lived and must
	// not be cut off by a client deadline. Liveness comes from the appliance's
	// periodic ping and from the reconnect loop.
	streamTimeout = 0

	// resultTimeout bounds the result upload. This was 30s, which is a LAN
	// number: results carry full-resolution screenshots, and a large desktop
	// capture on a slow uplink legitimately needs longer than that. Too short a
	// value here turns a slow link into a stream of failed commands.
	resultTimeout = 2 * time.Minute

	// buildTimeout bounds fetching an app build, which can be hundreds of MB.
	buildTimeout = 10 * time.Minute
)

// doer is the seam every runner request goes through.
//
// It is an interface rather than *http.Client so an alternative transport
// (HTTP/3, or a MASQUE tunnel) can be slotted in later without touching the
// tunnel or control logic. Nothing depends on the concrete type today.
type doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// sharedTransport backs every request. One transport means connection reuse and
// one place to configure TLS, instead of the three independent http.Client
// literals this replaces.
//
// Proxy is set explicitly and deliberately. http.DefaultTransport applies
// ProxyFromEnvironment for free, so the previous bare &http.Client{} honored
// HTTP_PROXY/HTTPS_PROXY/NO_PROXY without anyone thinking about it. Hand-building
// a Transport silently drops that, which would break every runner behind a
// corporate proxy — exactly the environments where an outbound-only agent is
// most likely to be deployed.
var sharedTransport = &http.Transport{
	Proxy: http.ProxyFromEnvironment,
	DialContext: (&net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext,
	ForceAttemptHTTP2:     true,
	MaxIdleConns:          10,
	IdleConnTimeout:       90 * time.Second,
	TLSHandshakeTimeout:   15 * time.Second,
	ExpectContinueTimeout: 1 * time.Second,
}

// httpClient returns a client for one request class. Callers pass the timeout
// that suits their payload; all of them share the transport above.
func httpClient(timeout time.Duration) doer {
	return &http.Client{Transport: sharedTransport, Timeout: timeout}
}
