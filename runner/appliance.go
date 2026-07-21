package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"strings"
)

// pinPrefix is the conventional marker for an SPKI SHA-256 pin. Accepted but not
// required, so operators can paste either form.
const pinPrefix = "sha256/"

// normalizeAppliance canonicalizes the --appliance value and decides whether the
// scheme is acceptable.
//
// A bare host now becomes https:// rather than being passed through as-is.
// Plaintext is refused unless the operator explicitly opts in, because this
// channel carries device control *and* build artifacts that the runner
// downloads and executes — over http:// anyone who can MITM the link gets code
// execution on this machine, not merely a copy of the traffic.
func normalizeAppliance(raw string, allowInsecure bool) (string, error) {
	s := strings.TrimRight(strings.TrimSpace(raw), "/")
	if s == "" {
		return "", fmt.Errorf("no appliance URL given")
	}

	switch {
	case strings.HasPrefix(strings.ToLower(s), "https://"):
		return s, nil

	case strings.HasPrefix(strings.ToLower(s), "http://"):
		if !allowInsecure {
			return "", fmt.Errorf(
				"refusing to connect to %s over plaintext HTTP: this channel carries "+
					"device control and executable build artifacts. Use https://, or pass "+
					"--insecure if this is a throwaway development appliance", s)
		}
		return s, nil

	case strings.Contains(s, "://"):
		scheme := s[:strings.Index(s, "://")]
		return "", fmt.Errorf("unsupported scheme %q in appliance URL (expected https)", scheme)

	default:
		// Bare host or host:port — assume HTTPS rather than silently downgrading.
		return "https://" + s, nil
	}
}

// parsePin validates an SPKI SHA-256 pin and returns the raw 32-byte digest.
// An empty pin is valid and means "verify against the system trust store".
func parsePin(raw string) ([]byte, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil, nil
	}
	s = strings.TrimPrefix(s, pinPrefix)

	sum, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		// Tolerate the URL-safe alphabet, since pins get passed through URLs and
		// copy-paste. Padding is optional in that form.
		if alt, altErr := base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "=")); altErr == nil {
			sum = alt
		} else {
			return nil, fmt.Errorf("pin is not valid base64: %w", err)
		}
	}
	if len(sum) != sha256.Size {
		return nil, fmt.Errorf("pin decodes to %d bytes, want %d (expected a SHA-256 of the certificate's public key)",
			len(sum), sha256.Size)
	}
	return sum, nil
}

// spkiPin returns the pin value for a certificate: base64 of the SHA-256 of its
// SubjectPublicKeyInfo. Pinning the key rather than the whole certificate is
// what lets the appliance renew its cert without every enrolled device having
// to be re-enrolled.
func spkiPin(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return pinPrefix + base64.StdEncoding.EncodeToString(sum[:])
}

// tlsConfigFor builds the TLS configuration for a connection to the appliance.
//
// Two modes:
//
//   - No pin: ordinary verification against the system trust store. This is the
//     right mode for an appliance with a real domain and an ACME certificate.
//   - Pin: the certificate is authenticated *solely* by its public key hash.
//     InsecureSkipVerify is set, which looks alarming but is deliberate — it
//     disables chain and hostname checking so a self-signed appliance
//     certificate is usable, and the pin then provides an equally strong (and
//     narrower) identity guarantee. Without this mode, appliances reached by IP
//     address could not use TLS at all, which is exactly what pushed operators
//     onto plaintext in the first place.
// clientCert, when non-nil, is presented to appliances that require mutual TLS.
// It is ignored by appliances that don't ask for one, so passing it always is
// safe and means the runner needs no separate "mTLS mode" switch.
//
// The bundle's caCertPem is deliberately *not* used as a server trust root. That
// CA signs runner client certificates only (it is MaxPathLenZero and clientAuth
// -scoped), while the appliance presents its own certificate on the mTLS
// listener — the same one ensure-tls.sh generates and publishes a pin for. So
// the appliance is authenticated by the pin here exactly as on the ordinary
// port, and the two directions stay independent.
func tlsConfigFor(pin []byte, insecure bool, clientCert *tls.Certificate) *tls.Config {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	switch {
	case insecure:
		// Development escape hatch. main warns loudly when this is set.
		cfg.InsecureSkipVerify = true //nolint:gosec // opt-in, dev only
	case len(pin) == 0:
		// Ordinary verification against the system trust store.
	default:
		//nolint:gosec // not unverified: VerifyPeerCertificate pins the key below.
		cfg.InsecureSkipVerify = true
		cfg.VerifyPeerCertificate = verifyPin(pin)
	}
	if clientCert != nil {
		cfg.Certificates = []tls.Certificate{*clientCert}
	}
	return cfg
}

// verifyPin accepts the handshake only if some presented certificate's public
// key matches the pin.
//
// It checks every certificate in the chain, not just the leaf, so pinning an
// intermediate or the appliance's own CA also works — useful if the appliance
// later issues leaf certs from an internal CA.
func verifyPin(pin []byte) func([][]byte, [][]*x509.Certificate) error {
	return func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
		if len(rawCerts) == 0 {
			return fmt.Errorf("appliance presented no certificate")
		}
		for _, raw := range rawCerts {
			cert, err := x509.ParseCertificate(raw)
			if err != nil {
				continue // a malformed entry can't match; keep checking the rest
			}
			sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
			if subtle.ConstantTimeCompare(sum[:], pin) == 1 {
				return nil
			}
		}
		leaf, err := x509.ParseCertificate(rawCerts[0])
		if err != nil {
			return fmt.Errorf("appliance certificate does not match the expected pin and could not be parsed")
		}
		return fmt.Errorf("appliance certificate does not match the expected pin "+
			"(presented %s for %q). If the appliance's certificate was regenerated, "+
			"re-enroll this device to get the new pin", spkiPin(leaf), leaf.Subject.CommonName)
	}
}
