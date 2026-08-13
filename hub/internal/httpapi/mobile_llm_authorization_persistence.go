package httpapi

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const mobileLLMAuthorizationEncryptionKeyEnv = "MACLAW_MOBILE_LLM_ENCRYPTION_KEY"
const mobileLLMAuthorizationPersistenceVersion = 1
const mobileLLMAuthorizationPersistenceKeyPrefix = "mobile_llm_authorization:"

type mobileLLMAuthorizationCiphertext struct {
	Version    int    `json:"version"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

var mobileLLMAuthorizationPersistence = struct {
	sync.RWMutex
	system store.SystemSettingsRepository
	key    []byte
}{}

// ConfigureMobileLLMAuthorizationPersistence configures optional encrypted
// persistence for desktop GUI delegated LLM credentials. The key must be an
// exact 32-byte base64 or hexadecimal value. Without it, credentials remain
// memory-only rather than being stored in plaintext.
func ConfigureMobileLLMAuthorizationPersistence(system store.SystemSettingsRepository) {
	key, _ := mobileLLMAuthorizationEncryptionKey(os.Getenv(mobileLLMAuthorizationEncryptionKeyEnv))
	mobileLLMAuthorizationPersistence.Lock()
	mobileLLMAuthorizationPersistence.system = system
	mobileLLMAuthorizationPersistence.key = key
	mobileLLMAuthorizationPersistence.Unlock()
}

func mobileLLMAuthorizationEncryptionKey(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if decoded, err := base64.StdEncoding.DecodeString(raw); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	if decoded, err := hex.DecodeString(raw); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	return nil, errInvalidMobileLLMAuthorizationEncryptionKey
}

var errInvalidMobileLLMAuthorizationEncryptionKey = &mobileLLMAuthorizationPersistenceError{}

type mobileLLMAuthorizationPersistenceError struct{}

func (*mobileLLMAuthorizationPersistenceError) Error() string {
	return mobileLLMAuthorizationEncryptionKeyEnv + " must be a base64 or hexadecimal 32-byte key"
}

func mobilePersistedLLMAuthorization(ctx context.Context, tenantID, userID string) (mobileLlmAuthorizationRecord, bool) {
	if mobileKnowledgeOwnerIsPurged(tenantID, userID) {
		return mobileLlmAuthorizationRecord{}, false
	}
	system, key := mobileLLMAuthorizationPersistenceConfig()
	if system == nil || len(key) != 32 {
		return mobileLlmAuthorizationRecord{}, false
	}
	raw, err := ScopedSystemSettingsForTenant(tenantID, system).Get(ctx, mobileLLMAuthorizationPersistenceKey(userID))
	if err != nil || strings.TrimSpace(raw) == "" {
		return mobileLlmAuthorizationRecord{}, false
	}
	var encrypted mobileLLMAuthorizationCiphertext
	if json.Unmarshal([]byte(raw), &encrypted) != nil || encrypted.Version != mobileLLMAuthorizationPersistenceVersion {
		return mobileLlmAuthorizationRecord{}, false
	}
	nonce, err := base64.StdEncoding.DecodeString(encrypted.Nonce)
	if err != nil {
		return mobileLlmAuthorizationRecord{}, false
	}
	ciphertext, err := base64.StdEncoding.DecodeString(encrypted.Ciphertext)
	if err != nil {
		return mobileLlmAuthorizationRecord{}, false
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return mobileLlmAuthorizationRecord{}, false
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil || len(nonce) != gcm.NonceSize() {
		return mobileLlmAuthorizationRecord{}, false
	}
	plain, err := gcm.Open(nil, nonce, ciphertext, mobileLLMAuthorizationAAD(tenantID, userID))
	if err != nil {
		return mobileLlmAuthorizationRecord{}, false
	}
	var record mobileLlmAuthorizationRecord
	if json.Unmarshal(plain, &record) != nil || record.TenantID != strings.TrimSpace(tenantID) || record.OwnerID != strings.TrimSpace(userID) || strings.TrimSpace(record.APIKey) == "" {
		return mobileLlmAuthorizationRecord{}, false
	}
	return record, true
}

func persistMobileLLMAuthorization(ctx context.Context, record mobileLlmAuthorizationRecord) error {
	mobileKnowledgePurgeState.RLock()
	defer mobileKnowledgePurgeState.RUnlock()
	if mobileKnowledgeOwnerIsPurgedLocked(record.TenantID, record.OwnerID) {
		return errMobileOwnerPurged
	}
	system, key := mobileLLMAuthorizationPersistenceConfig()
	if system == nil || len(key) != 32 {
		return nil
	}
	plain, err := json.Marshal(record)
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}
	ciphertext := gcm.Seal(nil, nonce, plain, mobileLLMAuthorizationAAD(record.TenantID, record.OwnerID))
	encoded, err := json.Marshal(mobileLLMAuthorizationCiphertext{
		Version:    mobileLLMAuthorizationPersistenceVersion,
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
	})
	if err != nil {
		return err
	}
	return ScopedSystemSettingsForTenant(record.TenantID, system).Set(ctx, mobileLLMAuthorizationPersistenceKey(record.OwnerID), string(encoded))
}

// mobileStoreLLMAuthorization updates the in-memory mirror under the same
// lifecycle guard as persistent writes. This prevents a QR authorization that
// was already in progress when unlink started from recreating a usable cache.
func mobileStoreLLMAuthorization(record mobileLlmAuthorizationRecord) bool {
	mobileKnowledgePurgeState.RLock()
	defer mobileKnowledgePurgeState.RUnlock()
	if mobileKnowledgeOwnerIsPurgedLocked(record.TenantID, record.OwnerID) {
		return false
	}
	mobileLlmAuthorizations.Lock()
	mobileLlmAuthorizations.authorizations[mobileLlmAuthorizationKey(record.TenantID, record.OwnerID)] = record
	mobileLlmAuthorizations.Unlock()
	return true
}

func mobileLLMAuthorizationWasPurged(err error) bool {
	return errors.Is(err, errMobileOwnerPurged)
}

func deletePersistedMobileLLMAuthorization(ctx context.Context, tenantID, userID string) error {
	system, key := mobileLLMAuthorizationPersistenceConfig()
	if system == nil || len(key) != 32 {
		return nil
	}
	return ScopedSystemSettingsForTenant(tenantID, system).Set(ctx, mobileLLMAuthorizationPersistenceKey(userID), "")
}

func mobileLLMAuthorizationPersistenceConfig() (store.SystemSettingsRepository, []byte) {
	mobileLLMAuthorizationPersistence.RLock()
	defer mobileLLMAuthorizationPersistence.RUnlock()
	return mobileLLMAuthorizationPersistence.system, append([]byte(nil), mobileLLMAuthorizationPersistence.key...)
}

func mobileLLMAuthorizationPersistenceKey(userID string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(userID)))
	return mobileLLMAuthorizationPersistenceKeyPrefix + hex.EncodeToString(sum[:])
}

func mobileLLMAuthorizationAAD(tenantID, userID string) []byte {
	return []byte(strings.TrimSpace(tenantID) + "\x00" + strings.TrimSpace(userID))
}
