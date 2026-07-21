// Package runnerca is a small internal certificate authority for desktop
// runners.
//
// It exists to make a stolen enrollment token insufficient on its own. A bearer
// token is replayable by anyone who observes it; a client certificate adds
// proof-of-possession, so an attacker also needs the private key that never
// leaves the enrolled machine.
//
// This is deliberately narrow. It is not a general PKI: one CA, leaf
// certificates for runners only, no intermediates, no CRL/OCSP. Revocation is
// handled the way it already is for tokens — the appliance checks the device
// record on every connection, so revoking a device stops its certificate from
// working without any certificate-level revocation machinery.
package runnerca

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"
)

const (
	// caValidity is long because re-issuing the CA invalidates every runner
	// certificate at once. It is stored encrypted at rest, and revocation works
	// per-device without touching it.
	caValidity = 10 * 365 * 24 * time.Hour

	// leafValidity bounds how long a stolen key stays useful if a device is lost
	// without anyone noticing. Runners renew well before this.
	leafValidity = 90 * 24 * time.Hour
)

// CA is a loaded certificate authority.
type CA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	// pemCert is the CA certificate in PEM, handed to runners so they can be
	// verified against it and to the TLS listener as its client-cert root.
	pemCert []byte
}

// Bundle is what an enrolled runner receives: its own certificate and key, plus
// the CA to verify the appliance's side.
type Bundle struct {
	ClientCertPEM string `json:"clientCertPem"`
	ClientKeyPEM  string `json:"clientKeyPem"`
	CACertPEM     string `json:"caCertPem"`
}

// Generate creates a new CA. The caller is responsible for storing the returned
// PEMs; the private key must be encrypted at rest.
func Generate() (caCertPEM, caKeyPEM string, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("generate CA key: %w", err)
	}
	serial, err := randomSerial()
	if err != nil {
		return "", "", err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "OpenCuttles Runner CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(caValidity),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		// The CA signs client certificates only; it must never be usable to
		// impersonate the appliance itself.
		MaxPathLen:     0,
		MaxPathLenZero: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return "", "", fmt.Errorf("self-sign CA: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return "", "", fmt.Errorf("marshal CA key: %w", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})),
		nil
}

// Load reconstructs a CA from its stored PEMs.
func Load(caCertPEM, caKeyPEM string) (*CA, error) {
	certBlock, _ := pem.Decode([]byte(caCertPEM))
	if certBlock == nil {
		return nil, fmt.Errorf("CA certificate is not valid PEM")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse CA certificate: %w", err)
	}
	keyBlock, _ := pem.Decode([]byte(caKeyPEM))
	if keyBlock == nil {
		return nil, fmt.Errorf("CA key is not valid PEM")
	}
	key, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse CA key: %w", err)
	}
	return &CA{cert: cert, key: key, pemCert: pem.EncodeToMemory(certBlock)}, nil
}

// CertPEM returns the CA certificate, for use as a trust root.
func (c *CA) CertPEM() string { return string(c.pemCert) }

// Pool returns the CA as a cert pool, for verifying runner client certificates.
func (c *CA) Pool() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(c.cert)
	return pool
}

// Issue mints a client certificate for one device.
//
// The device id goes in the Common Name, which is what binds a certificate to a
// device: presenting a valid certificate for device A does not authenticate as
// device B, because the appliance compares the CN against the device the token
// resolved to.
func (c *CA) Issue(deviceID string) (Bundle, error) {
	if deviceID == "" {
		return Bundle{}, fmt.Errorf("device id is required")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return Bundle{}, fmt.Errorf("generate client key: %w", err)
	}
	serial, err := randomSerial()
	if err != nil {
		return Bundle{}, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: deviceID, Organization: []string{"OpenCuttles Runner"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(leafValidity),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, c.cert, &key.PublicKey, c.key)
	if err != nil {
		return Bundle{}, fmt.Errorf("sign client certificate: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return Bundle{}, fmt.Errorf("marshal client key: %w", err)
	}
	return Bundle{
		ClientCertPEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		ClientKeyPEM:  string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})),
		CACertPEM:     c.CertPEM(),
	}, nil
}

// DeviceIDFromCert returns the device a client certificate was issued for.
func DeviceIDFromCert(cert *x509.Certificate) string {
	if cert == nil {
		return ""
	}
	return cert.Subject.CommonName
}

func randomSerial() (*big.Int, error) {
	// 128 random bits, per CA/Browser Forum guidance; collisions would let two
	// certificates be confused for one another in logs and audits.
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil, fmt.Errorf("generate serial: %w", err)
	}
	return serial, nil
}
