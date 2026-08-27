package cloudworkspace

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	masterKeyEnv  = "MACLAW_CWS_MASTER_KEY"
	masterKeyFile = "master.key"
	dekInfoPrefix = "maclaw-cws-v1"
)

func loadMasterKey(dir string) ([]byte, error) {
	if raw := strings.TrimSpace(os.Getenv(masterKeyEnv)); raw != "" {
		key, err := base64.RawStdEncoding.DecodeString(raw)
		if err != nil || len(key) != 32 {
			return nil, fmt.Errorf("%s must be a base64 32-byte key", masterKeyEnv)
		}
		return key, nil
	}
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, fmt.Errorf("cloud workspace master key directory is required")
	}
	path := filepath.Join(dir, masterKeyFile)
	if key, err := os.ReadFile(path); err == nil {
		if len(key) != 32 {
			return nil, fmt.Errorf("invalid cloud workspace master key")
		}
		return key, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	// CreateNew so concurrent Hub processes share one master key instead of
	// silently replacing it and stranding existing ciphertext.
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		key, err = os.ReadFile(path)
		if err != nil || len(key) != 32 {
			return nil, fmt.Errorf("invalid cloud workspace master key")
		}
		return key, nil
	}
	if err != nil {
		return nil, err
	}
	if _, err := file.Write(key); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	return key, nil
}

func deriveDEK(master []byte, tenantID, userID, workspaceID string) []byte {
	mac := hmac.New(sha256.New, master)
	_, _ = mac.Write([]byte(dekInfoPrefix + "\x00" + tenantID + "\x00" + userID + "\x00" + workspaceID))
	return mac.Sum(nil)
}

func objectAAD(tenantID, userID, workspaceID string) []byte {
	return []byte(tenantID + "|" + userID + "|" + workspaceID)
}

func seal(dek, aad, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, aad), nil
}

func open(dek, aad, blob []byte) ([]byte, error) {
	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	ns := gcm.NonceSize()
	if len(blob) < ns {
		return nil, ErrBlobCorrupt
	}
	plain, err := gcm.Open(nil, blob[:ns], blob[ns:], aad)
	if err != nil {
		return nil, ErrBlobCorrupt
	}
	return plain, nil
}

func plaintextSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
