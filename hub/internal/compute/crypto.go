package compute

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	// AES256KeyLen is the required key length for AES-256 (32 bytes).
	AES256KeyLen = 32
	// encKeyFile is the filename used to persist the auto-generated encryption key.
	encKeyFile = "compute_enc.key"
)

// EncryptAPIKey encrypts a plaintext API key using AES-256-GCM.
// The key must be exactly 32 bytes. Returns the ciphertext and nonce separately.
func EncryptAPIKey(plaintext string, key []byte) (ciphertext, nonce []byte, err error) {
	if len(key) != AES256KeyLen {
		return nil, nil, fmt.Errorf("invalid key length: got %d, want %d", len(key), AES256KeyLen)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, fmt.Errorf("aes cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, fmt.Errorf("gcm: %w", err)
	}

	nonce = make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext = gcm.Seal(nil, nonce, []byte(plaintext), nil)
	return ciphertext, nonce, nil
}

// DecryptAPIKey decrypts an AES-256-GCM encrypted API key.
// The key must be exactly 32 bytes.
func DecryptAPIKey(ciphertext, nonce, key []byte) (string, error) {
	if len(key) != AES256KeyLen {
		return "", fmt.Errorf("invalid key length: got %d, want %d", len(key), AES256KeyLen)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("aes cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("gcm: %w", err)
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}

	return string(plaintext), nil
}

// LoadOrGenerateKey loads the AES-256 encryption key from the given dataDir.
// If the key file does not exist, a new random 32-byte key is generated,
// persisted to disk as a hex-encoded file, and returned.
func LoadOrGenerateKey(dataDir string) ([]byte, error) {
	keyPath := filepath.Join(dataDir, encKeyFile)

	// Try loading existing key.
	if data, err := os.ReadFile(keyPath); err == nil {
		key, err := hex.DecodeString(string(data))
		if err != nil {
			return nil, fmt.Errorf("decode encryption key: %w", err)
		}
		if len(key) != AES256KeyLen {
			return nil, fmt.Errorf("stored key has wrong length: got %d, want %d", len(key), AES256KeyLen)
		}
		return key, nil
	}

	// Generate new key.
	key := make([]byte, AES256KeyLen)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generate encryption key: %w", err)
	}

	// Persist to disk.
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	if err := os.WriteFile(keyPath, []byte(hex.EncodeToString(key)), 0o600); err != nil {
		return nil, fmt.Errorf("write encryption key: %w", err)
	}

	return key, nil
}
