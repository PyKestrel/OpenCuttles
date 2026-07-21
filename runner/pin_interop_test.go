package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// scripts/ubuntu/ensure-tls.sh publishes the appliance pin by piping openssl:
//
//	openssl x509 -pubkey -noout | openssl pkey -pubin -outform der \
//	  | openssl dgst -sha256 -binary | openssl base64
//
// The runner computes the same value in Go. If those two ever disagree, every
// deployment silently breaks: the dashboard hands out a pin the runner will
// never match, and the failure looks like a certificate problem rather than a
// tooling mismatch. This pins the interop.
func TestOpenSSLAndGoAgreeOnPin(t *testing.T) {
	if _, err := exec.LookPath("openssl"); err != nil {
		t.Skip("openssl not available")
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(42),
		Subject:      pkix.Name{CommonName: "10.1.0.104"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}

	certPath := filepath.Join(t.TempDir(), "appliance.crt")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(certPath, pemBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	// Exactly the pipeline ensure-tls.sh runs.
	script := `openssl x509 -in "$1" -pubkey -noout ` +
		`| openssl pkey -pubin -outform der ` +
		`| openssl dgst -sha256 -binary ` +
		`| openssl base64`
	out, err := exec.Command("sh", "-c", script, "sh", certPath).Output()
	if err != nil {
		t.Skipf("could not run the openssl pipeline here: %v", err)
	}
	fromShell := "sha256/" + strings.TrimSpace(string(out))
	fromGo := spkiPin(cert)

	if fromShell != fromGo {
		t.Fatalf("pin mismatch between ensure-tls.sh and the runner:\n  openssl: %s\n  go:      %s",
			fromShell, fromGo)
	}
	// And the shell-produced value must survive the runner's own parse+verify.
	pin, err := parsePin(fromShell)
	if err != nil {
		t.Fatalf("runner rejected the pin ensure-tls.sh would publish: %v", err)
	}
	if err := verifyPin(pin)([][]byte{cert.Raw}, nil); err != nil {
		t.Fatalf("runner would refuse an appliance using this certificate: %v", err)
	}
}
