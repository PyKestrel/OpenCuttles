// Package secretbox provides authenticated symmetric encryption (AES-256-GCM)
// for secrets stored at rest, keyed by a base64 master key supplied via the
// OPENCUTTLES_SECRET_KEY environment variable. It is used to encrypt agent
// provider API keys before they are written to SQLite.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// ErrNoKey is returned by New when no master key is configured, so callers can
// degrade gracefully (features that need secret storage stay disabled).
var ErrNoKey = errors.New("secretbox: no master key configured")

// Box encrypts and decrypts short secrets with a fixed key.
type Box struct{ gcm cipher.AEAD }

// New builds a Box from a base64-encoded 32-byte key. An empty key yields
// ErrNoKey. Generate one with: openssl rand -base64 32
func New(base64Key string) (*Box, error) {
	if base64Key == "" {
		return nil, ErrNoKey
	}
	key, err := base64.StdEncoding.DecodeString(base64Key)
	if err != nil {
		return nil, fmt.Errorf("secretbox: decode key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("secretbox: key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{gcm: gcm}, nil
}

// Seal encrypts plaintext, returning base64(nonce || ciphertext || tag).
func (b *Box) Seal(plaintext string) (string, error) {
	nonce := make([]byte, b.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := b.gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Open reverses Seal. It fails if the token is malformed or the tag is invalid
// (wrong key or tampering).
func (b *Box) Open(token string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		return "", fmt.Errorf("secretbox: decode token: %w", err)
	}
	ns := b.gcm.NonceSize()
	if len(raw) < ns {
		return "", errors.New("secretbox: ciphertext too short")
	}
	nonce, ct := raw[:ns], raw[ns:]
	plain, err := b.gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("secretbox: open: %w", err)
	}
	return string(plain), nil
}
