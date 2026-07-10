package secretbox

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"testing"
)

func newKey(t *testing.T) string {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(k)
}

func TestRoundTrip(t *testing.T) {
	box, err := New(newKey(t))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	const secret = "sk-test-1234567890"
	token, err := box.Seal(secret)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if token == secret {
		t.Fatal("ciphertext must not equal plaintext")
	}
	got, err := box.Open(token)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if got != secret {
		t.Fatalf("round-trip mismatch: got %q", got)
	}
}

func TestWrongKeyFails(t *testing.T) {
	a, _ := New(newKey(t))
	b, _ := New(newKey(t))
	token, _ := a.Seal("secret")
	if _, err := b.Open(token); err == nil {
		t.Fatal("decrypting with a different key must fail")
	}
}

func TestNoKey(t *testing.T) {
	if _, err := New(""); !errors.Is(err, ErrNoKey) {
		t.Fatalf("empty key should return ErrNoKey, got %v", err)
	}
}

func TestBadKeyLength(t *testing.T) {
	if _, err := New(base64.StdEncoding.EncodeToString([]byte("too-short"))); err == nil {
		t.Fatal("a non-32-byte key should be rejected")
	}
}
