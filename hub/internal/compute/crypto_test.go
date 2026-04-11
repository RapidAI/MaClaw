package compute

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := make([]byte, AES256KeyLen)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}

	plaintext := "sk-test-api-key-12345"
	ct, nonce, err := EncryptAPIKey(plaintext, key)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	got, err := DecryptAPIKey(ct, nonce, key)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if got != plaintext {
		t.Fatalf("round-trip mismatch: got %q, want %q", got, plaintext)
	}
}

func TestEncryptInvalidKeyLength(t *testing.T) {
	_, _, err := EncryptAPIKey("test", []byte("short"))
	if err == nil {
		t.Fatal("expected error for short key")
	}
}

func TestDecryptInvalidKeyLength(t *testing.T) {
	_, err := DecryptAPIKey([]byte("ct"), []byte("nonce"), []byte("short"))
	if err == nil {
		t.Fatal("expected error for short key")
	}
}

func TestDecryptWrongKey(t *testing.T) {
	key1 := make([]byte, AES256KeyLen)
	key2 := make([]byte, AES256KeyLen)
	rand.Read(key1)
	rand.Read(key2)

	ct, nonce, err := EncryptAPIKey("secret", key1)
	if err != nil {
		t.Fatal(err)
	}

	_, err = DecryptAPIKey(ct, nonce, key2)
	if err == nil {
		t.Fatal("expected error decrypting with wrong key")
	}
}

func TestLoadOrGenerateKey_NewKey(t *testing.T) {
	dir := t.TempDir()

	key, err := LoadOrGenerateKey(dir)
	if err != nil {
		t.Fatalf("LoadOrGenerateKey: %v", err)
	}
	if len(key) != AES256KeyLen {
		t.Fatalf("key length: got %d, want %d", len(key), AES256KeyLen)
	}

	// File should exist.
	if _, err := os.Stat(filepath.Join(dir, encKeyFile)); err != nil {
		t.Fatalf("key file not created: %v", err)
	}
}

func TestLoadOrGenerateKey_Reload(t *testing.T) {
	dir := t.TempDir()

	key1, err := LoadOrGenerateKey(dir)
	if err != nil {
		t.Fatal(err)
	}

	key2, err := LoadOrGenerateKey(dir)
	if err != nil {
		t.Fatal(err)
	}

	if string(key1) != string(key2) {
		t.Fatal("reloaded key differs from original")
	}
}
