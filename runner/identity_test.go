package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// issueTestBundle mints a CA and one client certificate under it, mirroring what
// the appliance's runnerca package hands out at enrollment.
func issueTestBundle(t *testing.T, deviceID string) (bundle []byte, caPool *x509.CertPool) {
	t.Helper()

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test Runner CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}

	clientKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	clientTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: deviceID},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	clientDER, err := x509.CreateCertificate(rand.Reader, clientTmpl, caCert, &clientKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	clientKeyDER, err := x509.MarshalECPrivateKey(clientKey)
	if err != nil {
		t.Fatal(err)
	}

	raw, err := json.Marshal(identity{
		ClientCertPEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: clientDER})),
		ClientKeyPEM:  string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: clientKeyDER})),
		CACertPEM:     string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})),
	})
	if err != nil {
		t.Fatal(err)
	}

	pool := x509.NewCertPool()
	pool.AddCert(caCert)
	return raw, pool
}

func TestSaveAndLoadIdentity(t *testing.T) {
	raw, _ := issueTestBundle(t, "device-abc")
	path := filepath.Join(t.TempDir(), "sub", identityFile)

	if err := saveIdentity(path, raw); err != nil {
		t.Fatalf("save: %v", err)
	}
	cert, err := loadIdentity(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	cn, notAfter := certExpiry(cert)
	if cn != "device-abc" {
		t.Fatalf("common name = %q, want the device id", cn)
	}
	if notAfter == "" {
		t.Fatal("no expiry reported — the runner could not warn before a device drops off")
	}
}

// The file holds a private key whose whole purpose is to be non-copyable.
func TestSaveIdentityIsOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits are not meaningful on Windows")
	}
	raw, _ := issueTestBundle(t, "device-abc")
	path := filepath.Join(t.TempDir(), identityFile)
	if err := saveIdentity(path, raw); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("identity file mode is %o, want 600 — the private key is readable by others", mode)
	}
}

// Windows PowerShell 5.1 — the default shell on Windows 10 and 11 — writes a
// UTF-8 BOM with `Set-Content -Encoding utf8`. Go's JSON decoder rejects one,
// so a bundle saved that way would fail with "invalid character 'ï'", which
// points at nothing useful.
func TestLoadIdentityToleratesAUTF8BOM(t *testing.T) {
	raw, _ := issueTestBundle(t, "device-abc")
	path := filepath.Join(t.TempDir(), identityFile)
	if err := os.WriteFile(path, append([]byte{0xEF, 0xBB, 0xBF}, raw...), 0o600); err != nil {
		t.Fatal(err)
	}
	cert, err := loadIdentity(path)
	if err != nil {
		t.Fatalf("a BOM-prefixed bundle was rejected: %v", err)
	}
	if cn, _ := certExpiry(cert); cn != "device-abc" {
		t.Fatalf("common name = %q", cn)
	}
}

// Absence is normal: mutual TLS is opt-in and most appliances don't use it.
func TestLoadIdentityMissingFileIsNotAnError(t *testing.T) {
	_, err := loadIdentity(filepath.Join(t.TempDir(), "nope.json"))
	if !errors.Is(err, errNoIdentity) {
		t.Fatalf("err = %v, want errNoIdentity", err)
	}
}

// Bad material must be rejected at install time with a message naming the file,
// not at the first connection as an opaque TLS handshake error.
func TestSaveIdentityRejectsBadBundles(t *testing.T) {
	good, _ := issueTestBundle(t, "device-abc")
	other, _ := issueTestBundle(t, "device-xyz")

	var goodID, otherID identity
	if err := json.Unmarshal(good, &goodID); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(other, &otherID); err != nil {
		t.Fatal(err)
	}
	// A cert and key from different bundles: individually valid, not a pair.
	mismatched, err := json.Marshal(identity{
		ClientCertPEM: goodID.ClientCertPEM,
		ClientKeyPEM:  otherID.ClientKeyPEM,
		CACertPEM:     goodID.CACertPEM,
	})
	if err != nil {
		t.Fatal(err)
	}
	noKey, err := json.Marshal(identity{ClientCertPEM: goodID.ClientCertPEM})
	if err != nil {
		t.Fatal(err)
	}

	for name, raw := range map[string][]byte{
		"not JSON":               []byte("this is not json"),
		"empty object":           []byte("{}"),
		"certificate but no key": noKey,
		"mismatched pair":        mismatched,
		"garbage PEM":            []byte(`{"clientCertPem":"nope","clientKeyPem":"nope"}`),
	} {
		path := filepath.Join(t.TempDir(), identityFile)
		if err := saveIdentity(path, raw); err == nil {
			t.Errorf("%s was accepted", name)
			continue
		}
		// Nothing must be left behind: a half-written identity would fail later
		// and more confusingly than the install failing now.
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s: a rejected bundle was still written to disk", name)
		}
	}
}

func TestTLSConfigCarriesTheClientCertificate(t *testing.T) {
	raw, _ := issueTestBundle(t, "device-abc")
	path := filepath.Join(t.TempDir(), identityFile)
	if err := saveIdentity(path, raw); err != nil {
		t.Fatal(err)
	}
	cert, err := loadIdentity(path)
	if err != nil {
		t.Fatal(err)
	}

	// A client certificate must not disturb how the appliance itself is verified:
	// the two directions are independent.
	pinned := tlsConfigFor([]byte(strings.Repeat("x", 32)), false, cert)
	if len(pinned.Certificates) != 1 {
		t.Fatal("the client certificate was not installed on the TLS config")
	}
	if pinned.VerifyPeerCertificate == nil {
		t.Fatal("adding a client certificate disabled pin verification")
	}

	// And with no identity, nothing is offered.
	if got := tlsConfigFor(nil, false, nil); len(got.Certificates) != 0 {
		t.Fatal("a certificate was offered when none was configured")
	}
}

// The test that matters: an appliance demanding a client certificate accepts the
// runner, and refuses it when the identity is absent. Everything else here is
// bookkeeping — this is the behavior the feature exists for.
func TestRunnerPresentsClientCertificateToMTLSServer(t *testing.T) {
	raw, caPool := issueTestBundle(t, "device-abc")
	path := filepath.Join(t.TempDir(), identityFile)
	if err := saveIdentity(path, raw); err != nil {
		t.Fatal(err)
	}
	cert, err := loadIdentity(path)
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
			http.Error(w, "no client certificate", http.StatusUnauthorized)
			return
		}
		// The appliance binds the certificate to a device by its Common Name.
		_, _ = io.WriteString(w, r.TLS.PeerCertificates[0].Subject.CommonName)
	}))
	srv.TLS = &tls.Config{
		MinVersion: tls.VersionTLS12,
		ClientAuth: tls.RequireAndVerifyClientCert,
		ClientCAs:  caPool,
	}
	srv.StartTLS()
	defer srv.Close()

	// configureTLS mutates the package-level transport; restore it afterwards so
	// this test can't leak into another.
	original := sharedTransport.TLSClientConfig
	defer func() {
		sharedTransport.TLSClientConfig = original
		sharedTransport.CloseIdleConnections()
	}()

	// The appliance's own certificate is httptest's throwaway, so verify it the
	// way a real runner verifies a self-signed appliance: by pin.
	pin, err := parsePin(spkiPin(srv.Certificate()))
	if err != nil {
		t.Fatal(err)
	}

	configureTLS(pin, false, cert)
	sharedTransport.CloseIdleConnections()
	resp, err := httpClient(resultTimeout).Do(mustGet(t, srv.URL))
	if err != nil {
		t.Fatalf("the mTLS handshake failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != "device-abc" {
		t.Fatalf("server saw HTTP %d / %q, want 200 / device-abc", resp.StatusCode, body)
	}

	// Without the identity the same appliance must refuse us. If this passes
	// while the server still requires a certificate, the feature is decorative.
	configureTLS(pin, false, nil)
	sharedTransport.CloseIdleConnections()
	resp2, err := httpClient(resultTimeout).Do(mustGet(t, srv.URL))
	if err == nil {
		defer resp2.Body.Close()
		if resp2.StatusCode == http.StatusOK {
			t.Fatal("an appliance requiring mutual TLS accepted a runner with no client certificate")
		}
	}
}

// install must not put PEM material in the auto-start entry: it is multi-line,
// far too long for a registry value or a .desktop Exec line, and quotedArgs
// relies on its values needing no escaping.
func TestIdentityNeverReachesTheAutostartEntry(t *testing.T) {
	e := enrollment{
		Appliance:   "https://appliance.local",
		Token:       "deadbeef",
		Pin:         "sha256/AAAA",
		IdentitySrc: filepath.Join("C:\\Users\\someone", "bundle with spaces.json"),
	}
	for _, arg := range runArgs(e) {
		if strings.Contains(arg, "bundle with spaces") || strings.Contains(arg, "identity") {
			t.Fatalf("the identity path leaked into the auto-start arguments: %q", arg)
		}
	}
	for _, rendered := range []string{
		winRunCommand(`C:\bin\runner.exe`, e),
		desktopEntry("/usr/bin/runner", e),
		launchAgentPlist("/usr/bin/runner", e),
	} {
		if strings.Contains(rendered, "bundle with spaces") {
			t.Fatalf("the identity path leaked into an auto-start entry:\n%s", rendered)
		}
	}
}
