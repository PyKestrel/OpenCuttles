package runnerca

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"
)

func newCA(t *testing.T) *CA {
	t.Helper()
	certPEM, keyPEM, err := Generate()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	ca, err := Load(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return ca
}

func TestIssuedCertificateVerifiesAgainstTheCA(t *testing.T) {
	ca := newCA(t)
	bundle, err := ca.Issue("dev_abc123")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	block, _ := pem.Decode([]byte(bundle.ClientCertPEM))
	if block == nil {
		t.Fatal("client cert is not PEM")
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse leaf: %v", err)
	}

	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots:     ca.Pool(),
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}); err != nil {
		t.Fatalf("issued certificate does not chain to its CA: %v", err)
	}
	if got := DeviceIDFromCert(leaf); got != "dev_abc123" {
		t.Fatalf("device id from cert = %q, want dev_abc123", got)
	}
	// The bundle must be directly usable as a TLS client identity.
	if _, err := tls.X509KeyPair([]byte(bundle.ClientCertPEM), []byte(bundle.ClientKeyPEM)); err != nil {
		t.Fatalf("bundle is not a usable key pair: %v", err)
	}
}

// A certificate from a different CA must not verify. This is the property that
// makes the whole thing worth having.
func TestForeignCertificateIsRejected(t *testing.T) {
	ours := newCA(t)
	theirs := newCA(t)

	bundle, err := theirs.Issue("dev_abc123")
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode([]byte(bundle.ClientCertPEM))
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots:     ours.Pool(),
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}); err == nil {
		t.Fatal("a certificate signed by another CA was accepted")
	}
}

// Each device gets a distinct identity and key. If two devices shared a key,
// one compromised machine would authenticate as any other.
func TestEachDeviceGetsADistinctIdentity(t *testing.T) {
	ca := newCA(t)
	a, err := ca.Issue("dev_a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := ca.Issue("dev_b")
	if err != nil {
		t.Fatal(err)
	}
	if a.ClientKeyPEM == b.ClientKeyPEM {
		t.Fatal("two devices were issued the same private key")
	}
	if a.ClientCertPEM == b.ClientCertPEM {
		t.Fatal("two devices were issued the same certificate")
	}

	parse := func(p string) *x509.Certificate {
		blk, _ := pem.Decode([]byte(p))
		c, err := x509.ParseCertificate(blk.Bytes)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	ca1, cb1 := parse(a.ClientCertPEM), parse(b.ClientCertPEM)
	if ca1.SerialNumber.Cmp(cb1.SerialNumber) == 0 {
		t.Fatal("serial numbers collided")
	}
	if DeviceIDFromCert(ca1) == DeviceIDFromCert(cb1) {
		t.Fatal("device ids collided")
	}
}

// The CA must not be able to masquerade as a server, and must not be able to
// issue further CAs.
func TestCAConstraints(t *testing.T) {
	ca := newCA(t)
	if !ca.cert.IsCA {
		t.Fatal("CA certificate is not marked as a CA")
	}
	if !ca.cert.MaxPathLenZero {
		t.Fatal("CA should not be able to issue intermediate CAs")
	}
	if ca.cert.KeyUsage&x509.KeyUsageCertSign == 0 {
		t.Fatal("CA cannot sign certificates")
	}

	bundle, err := ca.Issue("dev_a")
	if err != nil {
		t.Fatal(err)
	}
	blk, _ := pem.Decode([]byte(bundle.ClientCertPEM))
	leaf, err := x509.ParseCertificate(blk.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if leaf.IsCA {
		t.Fatal("a runner certificate must not be a CA")
	}
	// Client auth only: a leaf that also carried serverAuth could be used to
	// impersonate the appliance to another runner.
	for _, u := range leaf.ExtKeyUsage {
		if u == x509.ExtKeyUsageServerAuth {
			t.Fatal("runner certificate carries serverAuth")
		}
	}
	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots:     ca.Pool(),
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err == nil {
		t.Fatal("runner certificate verified for server use")
	}
}

func TestLeafExpiryIsBounded(t *testing.T) {
	ca := newCA(t)
	bundle, err := ca.Issue("dev_a")
	if err != nil {
		t.Fatal(err)
	}
	blk, _ := pem.Decode([]byte(bundle.ClientCertPEM))
	leaf, err := x509.ParseCertificate(blk.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	life := leaf.NotAfter.Sub(leaf.NotBefore)
	// A stolen key should stop being useful without anyone having to notice.
	if life > 200*24*time.Hour {
		t.Fatalf("runner certificate lifetime %s is too long", life)
	}
}

func TestLoadRejectsGarbage(t *testing.T) {
	certPEM, keyPEM, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, cert, key string }{
		{"empty", "", ""},
		{"cert not pem", "nope", keyPEM},
		{"key not pem", certPEM, "nope"},
		{"swapped", keyPEM, certPEM},
	} {
		if _, err := Load(tc.cert, tc.key); err == nil {
			t.Errorf("%s: Load accepted invalid material", tc.name)
		}
	}
}

func TestIssueRequiresADeviceID(t *testing.T) {
	if _, err := newCA(t).Issue(""); err == nil {
		t.Fatal("issuing without a device id should fail")
	}
}
