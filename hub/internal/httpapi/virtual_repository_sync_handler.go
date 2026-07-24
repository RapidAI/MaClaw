package httpapi

// Per-user virtual-repository synchronization.  The Hub deliberately stores a
// single encrypted document per user: repository definitions and credential
// secrets never enter the general settings store, are never returned in logs,
// and are isolated by tenant + user id.

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

const virtualRepositorySyncMaxBytes int64 = 2 << 20

type virtualRepositorySyncRequest struct {
	Payload         json.RawMessage `json:"payload"`
	IfMatchRevision string          `json:"if_match_revision,omitempty"`
}

type virtualRepositorySyncMeta struct {
	TenantID  string `json:"tenant_id"`
	UserID    string `json:"user_id"`
	Revision  string `json:"revision"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type virtualRepositorySyncStored struct {
	Version    int    `json:"version"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

// Store the encrypted document and its revision together. The former two-file
// layout could be interrupted after document.json was replaced but before
// meta.json was replaced, making the next read permanently inconsistent.
type virtualRepositorySyncRecord struct {
	Meta     virtualRepositorySyncMeta   `json:"meta"`
	Document virtualRepositorySyncStored `json:"document"`
}

type virtualRepositorySyncView struct {
	HasDocument bool   `json:"has_document"`
	Revision    string `json:"revision,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
	LimitBytes  int64  `json:"limit_bytes"`
}

var virtualRepositorySyncFileMu sync.Mutex

// VirtualRepositorySyncHandler is authenticated with the existing desktop
// machine credential.  The authenticated machine determines the owner; a
// client cannot choose another user's document.
func VirtualRepositorySyncHandler(identity veMachineAuthenticator, baseDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := authenticateVEMachine(w, r, identity)
		if !ok {
			return
		}
		if strings.TrimSpace(principal.UserID) == "" {
			writeError(w, http.StatusUnauthorized, "MACHINE_UNAUTHORIZED", "machine is not associated with a user")
			return
		}
		switch r.Method {
		case http.MethodGet:
			handleVirtualRepositorySyncGet(w, r, principal, baseDir)
		case http.MethodPut:
			handleVirtualRepositorySyncPut(w, r, principal, baseDir)
		default:
			w.Header().Set("Allow", "GET, PUT")
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed")
		}
	}
}

func handleVirtualRepositorySyncGet(w http.ResponseWriter, _ *http.Request, p *auth.MachinePrincipal, baseDir string) {
	virtualRepositorySyncFileMu.Lock()
	defer virtualRepositorySyncFileMu.Unlock()
	meta, stored, err := loadVirtualRepositorySyncDocument(baseDir, p)
	if errors.Is(err, os.ErrNotExist) {
		writeJSON(w, http.StatusOK, virtualRepositorySyncView{LimitBytes: virtualRepositorySyncMaxBytes})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "VREPO_SYNC_READ_FAILED", "virtual repository sync data is unavailable")
		return
	}
	plain, err := decryptVirtualRepositorySync(baseDir, p, stored)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "VREPO_SYNC_DECRYPT_FAILED", "virtual repository sync data is unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Virtual-Repository-Sync-Revision", meta.Revision)
	w.Header().Set("X-Virtual-Repository-Sync-Updated-At", meta.UpdatedAt)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(plain)
}

func handleVirtualRepositorySyncPut(w http.ResponseWriter, r *http.Request, p *auth.MachinePrincipal, baseDir string) {
	var req virtualRepositorySyncRequest
	decoder := json.NewDecoder(io.LimitReader(r.Body, virtualRepositorySyncMaxBytes+1024))
	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid virtual repository sync request")
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid virtual repository sync request")
		return
	}
	if len(req.Payload) == 0 || int64(len(req.Payload)) > virtualRepositorySyncMaxBytes {
		writeError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "virtual repository sync payload is invalid or too large")
		return
	}
	var value any
	if err := json.Unmarshal(req.Payload, &value); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "virtual repository sync payload must be valid JSON")
		return
	}
	plain, err := json.Marshal(value) // canonicalized before hashing/encryption
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "virtual repository sync payload is invalid")
		return
	}
	virtualRepositorySyncFileMu.Lock()
	defer virtualRepositorySyncFileMu.Unlock()
	existing, _, loadErr := loadVirtualRepositorySyncDocument(baseDir, p)
	if loadErr != nil && !errors.Is(loadErr, os.ErrNotExist) {
		writeError(w, http.StatusInternalServerError, "VREPO_SYNC_READ_FAILED", "virtual repository sync data is unavailable")
		return
	}
	// Revisions are content hashes of the canonical payload. When two devices
	// converge on the same document, a stale if_match is not a real conflict:
	// the store already holds exactly what the client is writing.
	revision := virtualRepositorySyncRevision(plain)
	if existing != nil && existing.Revision == revision {
		writeJSON(w, http.StatusOK, virtualRepositorySyncView{HasDocument: true, Revision: existing.Revision, UpdatedAt: existing.UpdatedAt, LimitBytes: virtualRepositorySyncMaxBytes})
		return
	}
	if expected := strings.TrimSpace(req.IfMatchRevision); expected == "*" && existing != nil {
		// "*" is the create-only precondition used by a desktop that just
		// observed no document. It closes the first-sync race where two newly
		// registered machines could otherwise both PUT without a revision and
		// the second one silently overwrite the first.
		writeErrorWithFields(w, http.StatusConflict, "VREPO_SYNC_CONFLICT", "virtual repositories changed on another device", map[string]any{"revision": existing.Revision, "updated_at": existing.UpdatedAt})
		return
	} else if expected != "" && expected != "*" && existing != nil && existing.Revision != expected {
		writeErrorWithFields(w, http.StatusConflict, "VREPO_SYNC_CONFLICT", "virtual repositories changed on another device", map[string]any{"revision": existing.Revision, "updated_at": existing.UpdatedAt})
		return
	}
	stored, err := encryptVirtualRepositorySync(baseDir, p, plain)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "VREPO_SYNC_ENCRYPT_FAILED", "virtual repository sync data could not be encrypted")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	meta := &virtualRepositorySyncMeta{TenantID: p.TenantID, UserID: p.UserID, Revision: revision, CreatedAt: now, UpdatedAt: now}
	if existing != nil {
		meta.CreatedAt = existing.CreatedAt
	}
	if err := saveVirtualRepositorySyncDocument(baseDir, p, meta, stored); err != nil {
		writeError(w, http.StatusInternalServerError, "VREPO_SYNC_SAVE_FAILED", "virtual repository sync data could not be saved")
		return
	}
	writeJSON(w, http.StatusOK, virtualRepositorySyncView{HasDocument: true, Revision: revision, UpdatedAt: now, LimitBytes: virtualRepositorySyncMaxBytes})
}

func virtualRepositorySyncRevision(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func virtualRepositorySyncDir(base string, p *auth.MachinePrincipal) string {
	key := sha256.Sum256([]byte(strings.TrimSpace(p.TenantID) + "\x00" + strings.TrimSpace(p.UserID)))
	return filepath.Join(base, "users", hex.EncodeToString(key[:]))
}
func loadVirtualRepositorySyncDocument(base string, p *auth.MachinePrincipal) (*virtualRepositorySyncMeta, *virtualRepositorySyncStored, error) {
	dir := virtualRepositorySyncDir(base, p)
	// New writes are a single atomic record. Retain the legacy pair as a
	// read-only migration path for Hubs upgraded in place.
	if recordData, err := os.ReadFile(filepath.Join(dir, "record.json")); err == nil {
		var record virtualRepositorySyncRecord
		if json.Unmarshal(recordData, &record) != nil || !validVirtualRepositorySyncRecord(&record, p) {
			return nil, nil, fmt.Errorf("invalid virtual repository sync document")
		}
		return &record.Meta, &record.Document, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, nil, err
	}
	metaData, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		return nil, nil, err
	}
	storedData, err := os.ReadFile(filepath.Join(dir, "document.json"))
	if err != nil {
		return nil, nil, err
	}
	var meta virtualRepositorySyncMeta
	var stored virtualRepositorySyncStored
	if json.Unmarshal(metaData, &meta) != nil || json.Unmarshal(storedData, &stored) != nil || !validVirtualRepositorySyncRecord(&virtualRepositorySyncRecord{Meta: meta, Document: stored}, p) {
		return nil, nil, fmt.Errorf("invalid virtual repository sync document")
	}
	return &meta, &stored, nil
}

func validVirtualRepositorySyncRecord(record *virtualRepositorySyncRecord, p *auth.MachinePrincipal) bool {
	return record != nil && record.Meta.TenantID == p.TenantID && record.Meta.UserID == p.UserID && strings.TrimSpace(record.Meta.Revision) != "" && record.Document.Version == 1 && strings.TrimSpace(record.Document.Nonce) != "" && strings.TrimSpace(record.Document.Ciphertext) != ""
}

func saveVirtualRepositorySyncDocument(base string, p *auth.MachinePrincipal, meta *virtualRepositorySyncMeta, stored *virtualRepositorySyncStored) error {
	dir := virtualRepositorySyncDir(base, p)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	record := virtualRepositorySyncRecord{Meta: *meta, Document: *stored}
	if !validVirtualRepositorySyncRecord(&record, p) {
		return errors.New("invalid virtual repository sync document")
	}
	recordData, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return writeVirtualRepositorySyncAtomic(filepath.Join(dir, "record.json"), recordData)
}
func writeVirtualRepositorySyncAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp.")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}
func virtualRepositorySyncMasterKey(base string) ([]byte, error) {
	if raw := strings.TrimSpace(os.Getenv("MACLAW_VREPO_SYNC_MASTER_KEY")); raw != "" {
		key, err := base64.RawStdEncoding.DecodeString(raw)
		if err != nil || len(key) != 32 {
			return nil, fmt.Errorf("MACLAW_VREPO_SYNC_MASTER_KEY must be a base64 32-byte key")
		}
		return key, nil
	}
	path := filepath.Join(base, "master.key")
	if key, err := os.ReadFile(path); err == nil {
		if len(key) != 32 {
			return nil, fmt.Errorf("invalid virtual repository sync master key")
		}
		return key, nil
	}
	if err := os.MkdirAll(base, 0o700); err != nil {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	// A second Hub request can create the key between the first read and this
	// write. CreateNew guarantees that all users on this Hub derive from one
	// stable master key instead of silently replacing it.
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		key, err = os.ReadFile(path)
		if err != nil || len(key) != 32 {
			return nil, fmt.Errorf("invalid virtual repository sync master key")
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
func virtualRepositorySyncUserKey(base string, p *auth.MachinePrincipal) ([]byte, error) {
	master, err := virtualRepositorySyncMasterKey(base)
	if err != nil {
		return nil, err
	}
	mac := hmac.New(sha256.New, master)
	_, _ = mac.Write([]byte("maclaw-vrepo-sync-v1\x00" + p.TenantID + "\x00" + p.UserID))
	return mac.Sum(nil), nil
}
func encryptVirtualRepositorySync(base string, p *auth.MachinePrincipal, plain []byte) (*virtualRepositorySyncStored, error) {
	key, err := virtualRepositorySyncUserKey(base, p)
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
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	aad := []byte(p.TenantID + "\x00" + p.UserID)
	return &virtualRepositorySyncStored{Version: 1, Nonce: base64.RawStdEncoding.EncodeToString(nonce), Ciphertext: base64.RawStdEncoding.EncodeToString(gcm.Seal(nil, nonce, plain, aad))}, nil
}
func decryptVirtualRepositorySync(base string, p *auth.MachinePrincipal, stored *virtualRepositorySyncStored) ([]byte, error) {
	key, err := virtualRepositorySyncUserKey(base, p)
	if err != nil {
		return nil, err
	}
	nonce, err := base64.RawStdEncoding.DecodeString(stored.Nonce)
	if err != nil {
		return nil, err
	}
	ciphertext, err := base64.RawStdEncoding.DecodeString(stored.Ciphertext)
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
	return gcm.Open(nil, nonce, ciphertext, []byte(p.TenantID+"\x00"+p.UserID))
}
