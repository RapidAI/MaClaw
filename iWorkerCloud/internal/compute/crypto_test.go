package compute

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
)

func TestEncryptDecryptAPIKey(t *testing.T) {
	key := make([]byte, AES256KeyLen)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}

	plaintext := "sk-test-api-key-1234567890"
	ciphertext, nonce, err := EncryptAPIKey(plaintext, key)
	if err != nil {
		t.Fatalf("EncryptAPIKey: %v", err)
	}
	if len(ciphertext) == 0 {
		t.Fatal("ciphertext is empty")
	}
	if len(nonce) == 0 {
		t.Fatal("nonce is empty")
	}

	got, err := DecryptAPIKey(ciphertext, nonce, key)
	if err != nil {
		t.Fatalf("DecryptAPIKey: %v", err)
	}
	if got != plaintext {
		t.Errorf("got %q, want %q", got, plaintext)
	}
}

func TestEncryptAPIKey_EmptyString(t *testing.T) {
	key := make([]byte, AES256KeyLen)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}

	ciphertext, nonce, err := EncryptAPIKey("", key)
	if err != nil {
		t.Fatalf("EncryptAPIKey: %v", err)
	}

	got, err := DecryptAPIKey(ciphertext, nonce, key)
	if err != nil {
		t.Fatalf("DecryptAPIKey: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestEncryptAPIKey_InvalidKeyLength(t *testing.T) {
	_, _, err := EncryptAPIKey("test", []byte("short"))
	if err == nil {
		t.Fatal("expected error for short key")
	}
}

func TestDecryptAPIKey_InvalidKeyLength(t *testing.T) {
	_, err := DecryptAPIKey([]byte("ct"), []byte("nonce"), []byte("short"))
	if err == nil {
		t.Fatal("expected error for short key")
	}
}

func TestDecryptAPIKey_WrongKey(t *testing.T) {
	key1 := make([]byte, AES256KeyLen)
	key2 := make([]byte, AES256KeyLen)
	if _, err := rand.Read(key1); err != nil {
		t.Fatal(err)
	}
	if _, err := rand.Read(key2); err != nil {
		t.Fatal(err)
	}

	ciphertext, nonce, err := EncryptAPIKey("secret", key1)
	if err != nil {
		t.Fatal(err)
	}

	_, err = DecryptAPIKey(ciphertext, nonce, key2)
	if err == nil {
		t.Fatal("expected error when decrypting with wrong key")
	}
}

func TestDecryptAPIKey_TamperedCiphertext(t *testing.T) {
	key := make([]byte, AES256KeyLen)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}

	ciphertext, nonce, err := EncryptAPIKey("secret", key)
	if err != nil {
		t.Fatal(err)
	}

	// Tamper with ciphertext.
	ciphertext[0] ^= 0xff

	_, err = DecryptAPIKey(ciphertext, nonce, key)
	if err == nil {
		t.Fatal("expected error for tampered ciphertext")
	}
}

func TestLoadOrGenerateKey_GenerateNew(t *testing.T) {
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

func TestLoadOrGenerateKey_LoadExisting(t *testing.T) {
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
		t.Error("loaded key differs from generated key")
	}
}

func TestLoadOrGenerateKey_NestedDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "deep")

	key, err := LoadOrGenerateKey(dir)
	if err != nil {
		t.Fatalf("LoadOrGenerateKey: %v", err)
	}
	if len(key) != AES256KeyLen {
		t.Fatalf("key length: got %d, want %d", len(key), AES256KeyLen)
	}
}
