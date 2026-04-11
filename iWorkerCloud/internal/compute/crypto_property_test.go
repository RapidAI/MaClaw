// Feature: compute-power-management, Property 1: API Key encryption round-trip
package compute

import (
	"crypto/rand"
	"io"
	"testing"
	"testing/quick"
)

func TestPropertyCryptoRoundTrip(t *testing.T) {
	// Generate a random 32-byte AES-256 key for the test suite.
	key := make([]byte, AES256KeyLen)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		t.Fatalf("generate key: %v", err)
	}

	f := func(plaintext string) bool {
		if plaintext == "" {
			return true // skip empty strings
		}

		ciphertext, nonce, err := EncryptAPIKey(plaintext, key)
		if err != nil {
			t.Logf("encrypt error: %v", err)
			return false
		}

		decrypted, err := DecryptAPIKey(ciphertext, nonce, key)
		if err != nil {
			t.Logf("decrypt error: %v", err)
			return false
		}

		return decrypted == plaintext
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Errorf("Property 1 failed: %v", err)
	}
}
