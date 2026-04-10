package httpapi

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/pbkdf2"
)

// AgentNetKeyStore is a simple in-memory store for encrypted AgentNet identity keys.
// In production this would be backed by a database; here we use a sync.Map
// keyed by normalized email.
var AgentNetKeyStore sync.Map

// AgentNetRestoreAttempts tracks failed restore attempts per email for rate limiting.
var AgentNetRestoreAttempts sync.Map // email -> *AgentNetAttemptEntry

type AgentNetAttemptEntry struct {
	Count    int
	LastFail time.Time
}

const (
	AgentNetMaxRestoreAttempts = 5
	AgentNetLockoutDuration   = 15 * time.Minute
)

type AgentNetKeyEntry struct {
	Email         string `json:"email"`
	EncryptedData string `json:"encrypted_data"` // base64(IV + AES-GCM ciphertext)
	CreatedAt     string `json:"created_at,omitempty"`
}

type AgentNetKeyBackupRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	KeyData  string `json:"key_data"` // base64-encoded raw identity.key content
}

type AgentNetKeyRestoreRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// deriveKey derives a 256-bit AES key from password + email salt using PBKDF2.
func deriveKey(password, email string) []byte {
	salt := []byte("AgentNet-key-backup:" + strings.ToLower(strings.TrimSpace(email)))
	return pbkdf2.Key([]byte(password), salt, 100_000, 32, sha256.New)
}

// encryptKeyData encrypts raw key bytes with AES-256-GCM.
func encryptKeyData(plaintext, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// decryptKeyData decrypts AES-256-GCM ciphertext.
func decryptKeyData(encoded string, key []byte) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, io.ErrUnexpectedEOF
	}
	return gcm.Open(nil, data[:nonceSize], data[nonceSize:], nil)
}

// jsonError writes a JSON error response with the given status code.
func AgentNetJSONError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": msg})
}

// AgentNetKeyBackupHandler handles POST /api/agentnet/key/backup
func AgentNetKeyBackupHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req AgentNetKeyBackupRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			AgentNetJSONError(w, "invalid request", http.StatusBadRequest)
			return
		}
		email := strings.ToLower(strings.TrimSpace(req.Email))
		if email == "" || req.Password == "" || req.KeyData == "" {
			AgentNetJSONError(w, "email, password, and key_data are required", http.StatusBadRequest)
			return
		}
		if len(req.Password) < 6 {
			AgentNetJSONError(w, "password must be at least 6 characters", http.StatusBadRequest)
			return
		}

		// Decode the raw key data
		rawKey, err := base64.StdEncoding.DecodeString(req.KeyData)
		if err != nil {
			AgentNetJSONError(w, "invalid key_data encoding", http.StatusBadRequest)
			return
		}
		if len(rawKey) > 10*1024 {
			AgentNetJSONError(w, "key data too large", http.StatusBadRequest)
			return
		}

		// Encrypt with password-derived key
		aesKey := deriveKey(req.Password, email)
		encrypted, err := encryptKeyData(rawKey, aesKey)
		if err != nil {
			AgentNetJSONError(w, "encryption failed", http.StatusInternalServerError)
			return
		}

		entry := AgentNetKeyEntry{
			Email:         email,
			EncryptedData: encrypted,
		}
		AgentNetKeyStore.Store(email, entry)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}
}

// AgentNetKeyRestoreHandler handles POST /api/agentnet/key/restore
func AgentNetKeyRestoreHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req AgentNetKeyRestoreRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			AgentNetJSONError(w, "invalid request", http.StatusBadRequest)
			return
		}
		email := strings.ToLower(strings.TrimSpace(req.Email))
		if email == "" || req.Password == "" {
			AgentNetJSONError(w, "email and password are required", http.StatusBadRequest)
			return
		}

		val, ok := AgentNetKeyStore.Load(email)
		if !ok {
			AgentNetJSONError(w, "no backup found for this email", http.StatusNotFound)
			return
		}

		// Rate limit: check for too many failed attempts
		if raw, exists := AgentNetRestoreAttempts.Load(email); exists {
			attempt := raw.(*AgentNetAttemptEntry)
			if attempt.Count >= AgentNetMaxRestoreAttempts && time.Since(attempt.LastFail) < AgentNetLockoutDuration {
				AgentNetJSONError(w, "too many failed attempts, try again later", http.StatusTooManyRequests)
				return
			}
			// Reset if lockout has expired
			if time.Since(attempt.LastFail) >= AgentNetLockoutDuration {
				AgentNetRestoreAttempts.Delete(email)
			}
		}

		entry := val.(AgentNetKeyEntry)

		aesKey := deriveKey(req.Password, email)
		plaintext, err := decryptKeyData(entry.EncryptedData, aesKey)
		if err != nil {
			// Track failed attempt
			raw, _ := AgentNetRestoreAttempts.LoadOrStore(email, &AgentNetAttemptEntry{})
			attempt := raw.(*AgentNetAttemptEntry)
			attempt.Count++
			attempt.LastFail = time.Now()
			AgentNetJSONError(w, "wrong password or corrupted backup", http.StatusForbidden)
			return
		}

		// Clear failed attempts on success
		AgentNetRestoreAttempts.Delete(email)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":       true,
			"key_data": base64.StdEncoding.EncodeToString(plaintext),
		})
	}
}
