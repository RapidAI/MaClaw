package ve

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

// QuotaStore manages encrypted persistence of the virtual employee quota
// assigned to this Hub by HubCenter. The quota is encrypted with AES-256-GCM
// and integrity-protected with HMAC-SHA256 to prevent tampering.
type QuotaStore struct {
	mu         sync.RWMutex
	keyMat     []byte // raw Hub private key material (used to derive AES key)
	filePath   string
	cachedQuota int    // in-memory cache after successful decrypt; -1 = not loaded
	hubID      string // current Hub's identity for validation
}

// encryptedQuotaFile is the on-disk JSON format.
type encryptedQuotaFile struct {
	Ciphertext []byte `json:"ciphertext"` // AES-256-GCM encrypted payload
	Nonce      []byte `json:"nonce"`      // 12-byte GCM nonce
	Version    int    `json:"version"`    // schema version for future migration
}

// quotaPayload is the plaintext structure encrypted inside the file.
type quotaPayload struct {
	Quota     int       `json:"quota"`
	HubID     string    `json:"hub_id"`
	Timestamp time.Time `json:"timestamp"`
	MAC       []byte    `json:"mac"` // HMAC-SHA256 over quota+hub_id+timestamp
}

const (
	quotaFileVersion   = 1
	quotaMaxAge        = 24 * time.Hour
	quotaMaxValue      = 10000
	gcmNonceSize       = 12
)

// NewQuotaStore creates a new QuotaStore.
// keyMat is the Hub's private key material (raw bytes).
// hubID is the current Hub's identity string for validation.
// filePath is where the encrypted quota file is stored.
func NewQuotaStore(keyMat []byte, hubID, filePath string) *QuotaStore {
	return &QuotaStore{
		keyMat:      keyMat,
		hubID:       hubID,
		filePath:    filePath,
		cachedQuota: -1,
	}
}

// SaveQuota encrypts and persists the quota value.
func (s *QuotaStore) SaveQuota(quota int) error {
	if quota < 0 || quota > quotaMaxValue {
		return fmt.Errorf("quota value %d out of valid range [0, %d]", quota, quotaMaxValue)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// 1. Build payload
	now := time.Now().UTC()
	mac := s.computeMAC(quota, s.hubID, now)
	payload := quotaPayload{
		Quota:     quota,
		HubID:     s.hubID,
		Timestamp: now,
		MAC:       mac,
	}

	plaintext, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal quota payload: %w", err)
	}

	// 2. Encrypt with AES-256-GCM
	aesKey := s.deriveAESKey()
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return fmt.Errorf("create AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("create GCM: %w", err)
	}

	nonce := make([]byte, gcmNonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

	// 3. Persist to disk
	file := encryptedQuotaFile{
		Ciphertext: ciphertext,
		Nonce:      nonce,
		Version:    quotaFileVersion,
	}
	data, err := json.Marshal(file)
	if err != nil {
		return fmt.Errorf("marshal encrypted file: %w", err)
	}
	if err := os.WriteFile(s.filePath, data, 0o600); err != nil {
		return fmt.Errorf("write quota file: %w", err)
	}

	// 4. Update cache
	s.cachedQuota = quota
	return nil
}

// LoadQuota decrypts and validates the stored quota.
// Returns the quota value or an error if validation fails.
func (s *QuotaStore) LoadQuota() (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.loadQuotaLocked()
}

func (s *QuotaStore) loadQuotaLocked() (int, error) {
	// 1. Read file
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return 0, fmt.Errorf("read quota file: %w", err)
	}

	var file encryptedQuotaFile
	if err := json.Unmarshal(data, &file); err != nil {
		return 0, fmt.Errorf("unmarshal quota file: %w", err)
	}

	// 2. Decrypt
	aesKey := s.deriveAESKey()
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return 0, fmt.Errorf("create AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return 0, fmt.Errorf("create GCM: %w", err)
	}

	if len(file.Nonce) != gcmNonceSize {
		return 0, errors.New("invalid nonce size")
	}

	plaintext, err := gcm.Open(nil, file.Nonce, file.Ciphertext, nil)
	if err != nil {
		return 0, fmt.Errorf("decrypt quota (possible key mismatch or data corruption): %w", err)
	}

	// 3. Parse payload
	var payload quotaPayload
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		return 0, fmt.Errorf("unmarshal quota payload: %w", err)
	}

	// 4. Verify MAC
	expectedMAC := s.computeMAC(payload.Quota, payload.HubID, payload.Timestamp)
	if !hmac.Equal(payload.MAC, expectedMAC) {
		return 0, errors.New("MAC verification failed: quota data may have been tampered with")
	}

	// 5. Verify Hub ID
	if payload.HubID != s.hubID {
		return 0, fmt.Errorf("hub ID mismatch: stored=%q, current=%q", payload.HubID, s.hubID)
	}

	// 6. Verify timestamp freshness
	if time.Since(payload.Timestamp) > quotaMaxAge {
		return 0, fmt.Errorf("quota timestamp expired: granted at %s, max age %s", payload.Timestamp.Format(time.RFC3339), quotaMaxAge)
	}

	// 7. Validate range
	if payload.Quota < 0 || payload.Quota > quotaMaxValue {
		return 0, fmt.Errorf("quota value %d out of valid range", payload.Quota)
	}

	return payload.Quota, nil
}

// GetEffectiveQuota returns the current effective quota.
// On any error (file missing, decryption failure, validation failure),
// returns 0 and logs a security warning.
func (s *QuotaStore) GetEffectiveQuota() int {
	s.mu.RLock()
	if s.cachedQuota >= 0 {
		q := s.cachedQuota
		s.mu.RUnlock()
		return q
	}
	s.mu.RUnlock()

	// Try to load from disk
	s.mu.Lock()
	defer s.mu.Unlock()

	// Double-check after acquiring write lock
	if s.cachedQuota >= 0 {
		return s.cachedQuota
	}

	quota, err := s.loadQuotaLocked()
	if err != nil {
		log.Printf("[ve-quota] WARNING: quota unavailable, defaulting to 0: %v", err)
		s.cachedQuota = 0
		return 0
	}

	s.cachedQuota = quota
	return quota
}

// InvalidateCache forces the next GetEffectiveQuota call to re-read from disk.
func (s *QuotaStore) InvalidateCache() {
	s.mu.Lock()
	s.cachedQuota = -1
	s.mu.Unlock()
}

// deriveAESKey derives a 32-byte AES-256 key from the Hub private key material.
func (s *QuotaStore) deriveAESKey() []byte {
	h := sha256.Sum256(s.keyMat)
	return h[:]
}

// computeMAC computes HMAC-SHA256 over quota||hubID||timestamp.
func (s *QuotaStore) computeMAC(quota int, hubID string, ts time.Time) []byte {
	mac := hmac.New(sha256.New, s.keyMat)
	// Write quota as fixed-width string to prevent ambiguity
	fmt.Fprintf(mac, "%d|%s|%d", quota, hubID, ts.UnixMilli())
	return mac.Sum(nil)
}
