package main

// Client-certificate identity for mutual TLS.
//
// The appliance hands this material out once, at enrollment or token rotation,
// as the "clientBundle" field of the response. It is what makes a stolen
// enrollment token insufficient on its own: the token is replayable by anyone
// who observes it, but the private key here never leaves this machine.
//
// Unlike the appliance URL, token, and pin, this cannot ride in the auto-start
// entry's argument list — PEM blocks are multi-line and far too long for a
// registry value or a .desktop Exec line, and putting them there would also
// break the no-escaping-needed invariant quotedArgs relies on. So install writes
// the bundle to a fixed path and the runner reads it from there on every start.

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// identityFile is the bundle's filename inside dataDir().
const identityFile = "runner-identity.json"

// identity mirrors the appliance's runnerca.Bundle JSON exactly, so the file the
// dashboard hands out can be saved verbatim with no reshaping.
type identity struct {
	ClientCertPEM string `json:"clientCertPem"`
	ClientKeyPEM  string `json:"clientKeyPem"`
	CACertPEM     string `json:"caCertPem"`
}

// identityPath is where the runner keeps its client identity.
func identityPath() string { return filepath.Join(dataDir(), identityFile) }

// loadIdentity reads and parses the bundle at path.
//
// A missing file is not an error: mutual TLS is opt-in, and the overwhelmingly
// common case is an appliance that does not use it. Callers distinguish the two
// with errNoIdentity.
func loadIdentity(path string) (*tls.Certificate, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, errNoIdentity
	}
	if err != nil {
		return nil, fmt.Errorf("reading the client identity at %s: %w", path, err)
	}
	return parseIdentity(raw, path)
}

var errNoIdentity = errors.New("no client identity file")

// parseIdentity turns the bundle's bytes into a usable TLS certificate.
func parseIdentity(raw []byte, path string) (*tls.Certificate, error) {
	// Windows PowerShell 5.1 writes a UTF-8 BOM with `Set-Content -Encoding utf8`,
	// and Go's JSON decoder rejects one. The install command avoids producing it,
	// but an operator saving the bundle by hand easily can, and the resulting
	// "invalid character 'ï'" says nothing useful about the cause.
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})

	var id identity
	if err := json.Unmarshal(raw, &id); err != nil {
		return nil, fmt.Errorf("the client identity at %s is not valid JSON: %w", path, err)
	}
	if strings.TrimSpace(id.ClientCertPEM) == "" || strings.TrimSpace(id.ClientKeyPEM) == "" {
		return nil, fmt.Errorf("the client identity at %s has no certificate or no key — "+
			"re-enroll this device to get a complete bundle", path)
	}
	cert, err := tls.X509KeyPair([]byte(id.ClientCertPEM), []byte(id.ClientKeyPEM))
	if err != nil {
		return nil, fmt.Errorf("the client identity at %s is not a usable certificate/key pair: %w", path, err)
	}
	return &cert, nil
}

// saveIdentity writes a bundle to path with owner-only permissions.
//
// The mode matters more than usual: this file contains a private key whose whole
// purpose is to be non-copyable. A world-readable key would leave the feature
// providing reassurance rather than security.
func saveIdentity(path string, raw []byte) error {
	if _, err := parseIdentity(raw, path); err != nil {
		// Validate before writing, so a bad paste fails at install time with a
		// clear message instead of at the first connection with a TLS error.
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// O_TRUNC rather than a rename: the file is small, and an interrupted write
	// leaves a bundle that fails to parse — which is reported — rather than one
	// that parses into a mismatched cert/key pair.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("writing the client identity to %s: %w", path, err)
	}
	if _, err := f.Write(raw); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// certExpiry reports when the client certificate stops being accepted, so the
// runner can warn before a device silently drops off.
func certExpiry(cert *tls.Certificate) (leafSubject string, notAfter string) {
	if cert == nil || len(cert.Certificate) == 0 {
		return "", ""
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return "", ""
	}
	return leaf.Subject.CommonName, leaf.NotAfter.Format("2006-01-02")
}
