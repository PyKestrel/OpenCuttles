package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNormalizeApplianceRejectsPlaintext(t *testing.T) {
	// The whole point: http:// is refused by default. This channel carries
	// build artifacts the runner downloads and executes, so a MITM on plaintext
	// is code execution, not just eavesdropping.
	if _, err := normalizeAppliance("http://10.1.0.104", false); err == nil {
		t.Fatal("plaintext http:// was accepted without --insecure")
	}
	got, err := normalizeAppliance("http://10.1.0.104", true)
	if err != nil || got != "http://10.1.0.104" {
		t.Fatalf("--insecure should allow http: got %q err=%v", got, err)
	}
}

func TestNormalizeApplianceDefaultsToHTTPS(t *testing.T) {
	cases := map[string]string{
		"10.1.0.104":              "https://10.1.0.104",
		"testral.example":         "https://testral.example",
		"testral.example:8443":    "https://testral.example:8443",
		"https://testral.example": "https://testral.example",
		// Trailing slashes and surrounding space are normalized away.
		"  https://testral.example/  ": "https://testral.example",
		"https://testral.example///":   "https://testral.example",
	}
	for in, want := range cases {
		got, err := normalizeAppliance(in, false)
		if err != nil {
			t.Errorf("normalizeAppliance(%q) errored: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("normalizeAppliance(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeApplianceRejectsJunk(t *testing.T) {
	for _, in := range []string{"", "   ", "ftp://host", "ws://host"} {
		if got, err := normalizeAppliance(in, false); err == nil {
			t.Errorf("normalizeAppliance(%q) = %q, want an error", in, got)
		}
	}
}

// A scheme we don't understand must be rejected even under --insecure:
// --insecure relaxes verification, it does not mean "anything goes".
func TestNormalizeApplianceInsecureStillRejectsUnknownSchemes(t *testing.T) {
	if _, err := normalizeAppliance("ftp://host", true); err == nil {
		t.Fatal("--insecure should not make ftp:// acceptable")
	}
}

func TestParsePin(t *testing.T) {
	raw := sha256.Sum256([]byte("some public key"))
	std := base64.StdEncoding.EncodeToString(raw[:])

	for _, in := range []string{std, pinPrefix + std, "  " + pinPrefix + std + "  "} {
		got, err := parsePin(in)
		if err != nil {
			t.Fatalf("parsePin(%q) errored: %v", in, err)
		}
		if string(got) != string(raw[:]) {
			t.Fatalf("parsePin(%q) returned the wrong digest", in)
		}
	}

	// Empty is valid and means "use the system trust store".
	if got, err := parsePin(""); err != nil || got != nil {
		t.Fatalf("empty pin should be accepted as no-pin: %v %v", got, err)
	}
}

func TestParsePinRejectsWrongLengthAndJunk(t *testing.T) {
	short := base64.StdEncoding.EncodeToString([]byte("too short"))
	if _, err := parsePin(short); err == nil {
		t.Fatal("a pin that isn't 32 bytes must be rejected")
	}
	if _, err := parsePin("not!base64!"); err == nil {
		t.Fatal("non-base64 must be rejected")
	}
	// A truncated-but-valid-base64 value is the realistic copy-paste error.
	full := sha256.Sum256([]byte("k"))
	truncated := base64.StdEncoding.EncodeToString(full[:20])
	if _, err := parsePin(truncated); err == nil {
		t.Fatal("a truncated pin must be rejected, not silently accepted")
	}
}

// selfSignedCert builds a throwaway certificate for pin tests.
func selfSignedCert(t *testing.T, cn string) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("cert: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return cert
}

func TestVerifyPinAcceptsMatchingCertificate(t *testing.T) {
	cert := selfSignedCert(t, "appliance.local")
	pin, err := parsePin(spkiPin(cert))
	if err != nil {
		t.Fatalf("parse our own pin: %v", err)
	}
	if err := verifyPin(pin)([][]byte{cert.Raw}, nil); err != nil {
		t.Fatalf("the pinned certificate was rejected: %v", err)
	}
}

func TestVerifyPinRejectsDifferentCertificate(t *testing.T) {
	expected := selfSignedCert(t, "appliance.local")
	imposter := selfSignedCert(t, "appliance.local") // same name, different key

	pin, _ := parsePin(spkiPin(expected))
	err := verifyPin(pin)([][]byte{imposter.Raw}, nil)
	if err == nil {
		t.Fatal("a certificate with a different key was accepted — the pin is not being enforced")
	}
	// The message must tell the operator how to recover, since a legitimately
	// regenerated appliance cert produces exactly this error.
	if !strings.Contains(err.Error(), "re-enroll") {
		t.Errorf("pin failure should explain recovery, got: %v", err)
	}
}

// Pinning the key rather than the whole certificate is what lets the appliance
// renew without re-enrolling every device.
func TestVerifyPinSurvivesCertificateRenewal(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	mk := func(serial int64, notAfter time.Time) *x509.Certificate {
		tmpl := &x509.Certificate{
			SerialNumber: big.NewInt(serial),
			Subject:      pkix.Name{CommonName: "appliance.local"},
			NotBefore:    time.Now().Add(-time.Hour),
			NotAfter:     notAfter,
		}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
		if err != nil {
			t.Fatal(err)
		}
		c, err := x509.ParseCertificate(der)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	original := mk(1, time.Now().Add(time.Hour))
	renewed := mk(2, time.Now().Add(24*time.Hour)) // same key, new cert

	pin, _ := parsePin(spkiPin(original))
	if err := verifyPin(pin)([][]byte{renewed.Raw}, nil); err != nil {
		t.Fatalf("a renewed certificate with the same key should still match: %v", err)
	}
}

// Pinning any certificate in the presented chain (not only the leaf) means an
// operator can pin the appliance's CA instead.
func TestVerifyPinMatchesNonLeafInChain(t *testing.T) {
	leaf := selfSignedCert(t, "leaf")
	issuer := selfSignedCert(t, "issuer")

	pin, _ := parsePin(spkiPin(issuer))
	if err := verifyPin(pin)([][]byte{leaf.Raw, issuer.Raw}, nil); err != nil {
		t.Fatalf("pinning a non-leaf chain member should work: %v", err)
	}
}

func TestVerifyPinRejectsEmptyAndMalformedChains(t *testing.T) {
	pin, _ := parsePin(spkiPin(selfSignedCert(t, "x")))
	if err := verifyPin(pin)(nil, nil); err == nil {
		t.Fatal("an empty chain must be rejected")
	}
	if err := verifyPin(pin)([][]byte{[]byte("not a certificate")}, nil); err == nil {
		t.Fatal("a malformed chain must be rejected")
	}
}

func TestTLSConfigModes(t *testing.T) {
	cert := selfSignedCert(t, "appliance.local")
	pin, _ := parsePin(spkiPin(cert))

	// No pin: ordinary verification against the system trust store.
	plain := tlsConfigFor(nil, false, nil)
	if plain.InsecureSkipVerify {
		t.Error("without a pin, verification must not be skipped")
	}
	if plain.MinVersion < 0x0303 { // TLS 1.2
		t.Error("MinVersion should be at least TLS 1.2")
	}

	// Pinned: chain verification is bypassed, but VerifyPeerCertificate must be
	// present — that combination is what authenticates a self-signed appliance.
	// InsecureSkipVerify without a verifier would be a silent downgrade to no
	// authentication at all, so assert both together.
	pinned := tlsConfigFor(pin, false, nil)
	if !pinned.InsecureSkipVerify {
		t.Error("pinned mode needs InsecureSkipVerify so a self-signed cert is usable")
	}
	if pinned.VerifyPeerCertificate == nil {
		t.Fatal("pinned mode has no verifier — the connection would be unauthenticated")
	}
	if err := pinned.VerifyPeerCertificate([][]byte{cert.Raw}, nil); err != nil {
		t.Errorf("the pinned cert should verify: %v", err)
	}
}

// The unit tests above exercise the verifier directly. This one drives a real
// TLS handshake against a self-signed server through the shared transport, which
// is the combination that actually ships: configureTLS + sharedTransport + a
// live connection.
func TestPinnedConnectionAgainstRealTLSServer(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// configureTLS mutates the package-level transport; put it back so test
	// order can't matter.
	original := sharedTransport.TLSClientConfig
	t.Cleanup(func() {
		sharedTransport.TLSClientConfig = original
		sharedTransport.CloseIdleConnections()
	})

	serverCert := srv.Certificate()

	t.Run("correct pin connects", func(t *testing.T) {
		pin, err := parsePin(spkiPin(serverCert))
		if err != nil {
			t.Fatalf("pin: %v", err)
		}
		configureTLS(pin, false, nil)
		sharedTransport.CloseIdleConnections()

		resp, err := httpClient(resultTimeout).Do(mustGet(t, srv.URL))
		if err != nil {
			t.Fatalf("pinned connection was refused: %v", err)
		}
		resp.Body.Close()
	})

	t.Run("wrong pin is refused", func(t *testing.T) {
		other := selfSignedCert(t, "somewhere-else")
		pin, _ := parsePin(spkiPin(other))
		configureTLS(pin, false, nil)
		sharedTransport.CloseIdleConnections()

		resp, err := httpClient(resultTimeout).Do(mustGet(t, srv.URL))
		if err == nil {
			resp.Body.Close()
			t.Fatal("a server whose key does not match the pin was accepted")
		}
		if !strings.Contains(err.Error(), "pin") {
			t.Errorf("error should name the pin, got: %v", err)
		}
	})

	t.Run("no pin rejects a self-signed server", func(t *testing.T) {
		// Without a pin we fall back to the system trust store, which must not
		// trust a self-signed appliance. This is the case that used to push
		// operators onto plaintext.
		configureTLS(nil, false, nil)
		sharedTransport.CloseIdleConnections()

		resp, err := httpClient(resultTimeout).Do(mustGet(t, srv.URL))
		if err == nil {
			resp.Body.Close()
			t.Fatal("a self-signed server was trusted with no pin and no CA")
		}
	})

	t.Run("insecure connects to anything", func(t *testing.T) {
		configureTLS(nil, true, nil)
		sharedTransport.CloseIdleConnections()

		resp, err := httpClient(resultTimeout).Do(mustGet(t, srv.URL))
		if err != nil {
			t.Fatalf("--insecure should connect regardless: %v", err)
		}
		resp.Body.Close()
	})
}

func mustGet(t *testing.T, url string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	return req
}
