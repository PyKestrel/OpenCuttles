package api

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/opencuttles/opencuttles/backend/internal/runnerca"
)

// Optional mutual TLS for desktop runners.
//
// Off by default. When enabled, runners must present a client certificate
// issued by the appliance's own CA in addition to their enrollment token, so a
// token copied off a machine is not enough on its own — the attacker also needs
// a private key that never leaves the enrolled device.
//
// The certificate is verified by this process on its own TLS listener rather
// than by Caddy with the identity forwarded in a header. Trusting a header for
// authentication would be the same shape as the X-Forwarded-For spoofing this
// codebase already had to fix, and it would hold only for as long as the
// backend port stays unreachable.

const (
	// Settings keys. The CA private key is sealed with OPENCUTTLES_SECRET_KEY;
	// the certificate is public material and stored as-is.
	settingRunnerCACert = "runner.ca.cert"
	settingRunnerCAKey  = "runner.ca.key"
)

// mtlsListen returns the address the runner mTLS listener should bind, or "" if
// the feature is disabled.
func mtlsListen() string {
	return strings.TrimSpace(os.Getenv("OPENCUTTLES_RUNNER_MTLS_LISTEN"))
}

// MTLSEnabled reports whether runner mutual TLS is switched on.
func MTLSEnabled() bool { return mtlsListen() != "" }

// runnerCA loads the appliance's runner CA, creating it on first use.
//
// The private key is sealed at rest, so this requires OPENCUTTLES_SECRET_KEY.
// Refusing to proceed without it is deliberate: a CA key sitting in the
// database in plaintext would be a worse outcome than the feature being
// unavailable.
func (s *Server) runnerCA(ctx context.Context) (*runnerca.CA, error) {
	if s.secrets == nil {
		return nil, fmt.Errorf("runner mTLS needs OPENCUTTLES_SECRET_KEY so the CA key can be encrypted at rest")
	}

	certPEM, err := s.store.GetSetting(ctx, settingRunnerCACert)
	if err != nil {
		return nil, err
	}
	sealedKey, err := s.store.GetSetting(ctx, settingRunnerCAKey)
	if err != nil {
		return nil, err
	}

	if certPEM != "" && sealedKey != "" {
		keyPEM, err := s.secrets.Open(sealedKey)
		if err != nil {
			return nil, fmt.Errorf("decrypt runner CA key (has OPENCUTTLES_SECRET_KEY changed?): %w", err)
		}
		return runnerca.Load(certPEM, keyPEM)
	}

	// First use: mint the CA and store it.
	newCert, newKey, err := runnerca.Generate()
	if err != nil {
		return nil, err
	}
	sealed, err := s.secrets.Seal(newKey)
	if err != nil {
		return nil, fmt.Errorf("seal runner CA key: %w", err)
	}
	if err := s.store.SetSetting(ctx, settingRunnerCACert, newCert); err != nil {
		return nil, err
	}
	if err := s.store.SetSetting(ctx, settingRunnerCAKey, sealed); err != nil {
		return nil, err
	}
	if s.logger != nil {
		s.logger.Info("generated the runner certificate authority")
	}
	return runnerca.Load(newCert, newKey)
}

// issueRunnerBundle mints a client identity for a device, when mTLS is enabled.
// Returns a nil bundle when the feature is off, so enrollment responses simply
// omit it.
func (s *Server) issueRunnerBundle(ctx context.Context, deviceID string) (*runnerca.Bundle, error) {
	if !MTLSEnabled() {
		return nil, nil
	}
	ca, err := s.runnerCA(ctx)
	if err != nil {
		return nil, err
	}
	bundle, err := ca.Issue(deviceID)
	if err != nil {
		return nil, err
	}
	return &bundle, nil
}

// verifyRunnerClientCert checks that a request carries a client certificate this
// appliance issued for exactly this device.
//
// The device id binding is the point. Verifying only that the certificate chains
// to our CA would let any enrolled machine authenticate as any other — the CN
// check is what makes a certificate device-specific.
func verifyRunnerClientCert(r *http.Request, deviceID string) error {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return fmt.Errorf("no client certificate presented")
	}
	// The chain is already verified by the TLS stack (RequireAndVerifyClientCert
	// against the CA pool); all that remains is the identity binding.
	leaf := r.TLS.PeerCertificates[0]
	if got := runnerca.DeviceIDFromCert(leaf); got != deviceID {
		return fmt.Errorf("client certificate is for device %q, not %q", got, deviceID)
	}
	return nil
}

// runnerMux is the only surface exposed on the mTLS listener: the dial-home
// tunnel and artifact fetch. The dashboard, API, and MCP endpoint are
// deliberately absent — this port exists for runners.
func (s *Server) runnerMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/runner/stream", s.runners.StreamHandler)
	mux.HandleFunc("POST /api/v1/runner/result", s.runners.ResultHandler)
	mux.HandleFunc("GET /api/v1/runner/build/{id}", s.runnerBuildArtifact)
	return mux
}

// MTLSServer builds the runner listener, or returns nil when mTLS is disabled.
//
// It needs a server certificate of its own; by default it reuses the appliance
// certificate that ensure-tls.sh already generates and publishes a pin for, so
// runners verify this endpoint with the same pin they already hold.
func (s *Server) MTLSServer(ctx context.Context) (*http.Server, error) {
	addr := mtlsListen()
	if addr == "" {
		return nil, nil
	}
	certFile := envOr("OPENCUTTLES_TLS_CERT", "/etc/opencuttles/tls/appliance.crt")
	keyFile := envOr("OPENCUTTLES_TLS_KEY", "/etc/opencuttles/tls/appliance.key")
	serverCert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("runner mTLS needs a server certificate (%s / %s): %w", certFile, keyFile, err)
	}
	ca, err := s.runnerCA(ctx)
	if err != nil {
		return nil, err
	}
	return &http.Server{
		Addr:              addr,
		Handler:           s.runnerMux(),
		ReadHeaderTimeout: 10 * time.Second,
		TLSConfig: &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{serverCert},
			// The TLS stack rejects anything that does not chain to our CA before
			// a handler ever runs.
			ClientAuth: tls.RequireAndVerifyClientCert,
			ClientCAs:  ca.Pool(),
		},
	}, nil
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// mtlsEndpoint is the base URL a runner should dial when mutual TLS is on.
//
// The listener binds its own port, so the dashboard origin is not the right
// address. OPENCUTTLES_RUNNER_MTLS_URL overrides it for deployments where the
// appliance is reached by a different name or through port forwarding.
func mtlsEndpoint(origin string) string {
	if explicit := strings.TrimSpace(os.Getenv("OPENCUTTLES_RUNNER_MTLS_URL")); explicit != "" {
		return strings.TrimRight(explicit, "/")
	}
	host := origin
	if i := strings.Index(host, "://"); i >= 0 {
		host = host[i+3:]
	}
	if i := strings.IndexAny(host, ":/"); i >= 0 {
		host = host[:i]
	}
	// Take the port from the listen address; the host comes from the origin so
	// runners dial the name the operator configured, not a bind address like
	// 0.0.0.0.
	addr := mtlsListen()
	port := addr
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		port = addr[i+1:]
	}
	if host == "" || port == "" {
		return ""
	}
	return "https://" + host + ":" + port
}
