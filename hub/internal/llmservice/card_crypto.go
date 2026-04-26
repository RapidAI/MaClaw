package llmservice

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

// Card code encryption uses AES-256-GCM.
// The key is derived from a seed string (env var or fixed fallback).
//
// Threat model: this prevents casual plaintext exposure of card codes in the
// JSON store. It is NOT designed to resist an attacker who has both the store
// file and the source code. For stronger isolation, set MACLAW_CARD_ENCRYPTION_KEY
// to a unique secret per installation.

const cardCryptoEnvKey = "MACLAW_CARD_ENCRYPTION_KEY"

var (
	cardCryptoKey     []byte
	cardCryptoKeyOnce sync.Once
)

// deriveCardKey returns a 32-byte AES-256 key derived from a seed string.
func deriveCardKey(seed string) []byte {
	h := sha256.Sum256([]byte("maclaw-card-v1:" + seed))
	return h[:]
}

// resolveCardKey lazily resolves the encryption key once.
func resolveCardKey() []byte {
	cardCryptoKeyOnce.Do(func() {
		seed := strings.TrimSpace(os.Getenv(cardCryptoEnvKey))
		if seed == "" {
			// Fallback: fixed default seed, shared across all installations
			// that don't set the env var. Sufficient to prevent casual plaintext
			// exposure; set MACLAW_CARD_ENCRYPTION_KEY for real isolation.
			seed = "maclaw-hub-default-card-key"
		}
		cardCryptoKey = deriveCardKey(seed)
	})
	return cardCryptoKey
}

// EncryptCardCode encrypts a card code using AES-256-GCM.
// Returns a base64-encoded string containing nonce + ciphertext.
func EncryptCardCode(plainCode string) (string, error) {
	plainCode = NormalizeCardCode(plainCode)
	if plainCode == "" {
		return "", fmt.Errorf("empty card code")
	}
	key := resolveCardKey()
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("aes.NewCipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("cipher.NewGCM: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("rand nonce: %w", err)
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plainCode), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// DecryptCardCode decrypts a base64-encoded AES-256-GCM ciphertext back to
// the original card code. Returns empty string on any failure (graceful degradation).
func DecryptCardCode(encrypted string) string {
	encrypted = strings.TrimSpace(encrypted)
	if encrypted == "" {
		return ""
	}
	data, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		return ""
	}
	key := resolveCardKey()
	block, err := aes.NewCipher(key)
	if err != nil {
		return ""
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return ""
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize+gcm.Overhead()+1 {
		return ""
	}
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return ""
	}
	return string(plaintext)
}
