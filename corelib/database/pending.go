package database

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// PendingMutation is the sanitized record of a mutation that has been
// approved by the user and is waiting for exactly one commit. It never stores
// the approval token or parameter values — only their digests — so the file
// on disk cannot be replayed by a reader and carries no sensitive payloads.
type PendingMutation struct {
	ID                string    `json:"id"`
	TokenHash         string    `json:"token_hash"`
	ProfileID         string    `json:"profile_id"`
	SQLFingerprint    string    `json:"sql_fingerprint"`
	ParamsFingerprint string    `json:"params_fingerprint,omitempty"`
	SchemaVersion     int       `json:"schema_version,omitempty"`
	OwnerID           string    `json:"owner_id,omitempty"`
	SessionID         string    `json:"session_id,omitempty"`
	OperationID       string    `json:"operation_id,omitempty"`
	Attempt           int       `json:"attempt,omitempty"`
	ParentActionID    string    `json:"parent_action_id,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	ExpiresAt         time.Time `json:"expires_at"`
}

// PendingStore persists pending mutations so an approval that pauses across a
// process restart is either honored once (before expiry) or destroyed — never
// silently forgotten inside a dying process while a commit later claims its
// approval. The file is append-agnostic state, rewritten atomically.
type PendingStore struct {
	mu    sync.Mutex
	path  string
	items map[string]PendingMutation
}

// pendingStoreRegistry keeps one in-process PendingStore per absolute path.
// Hosts that construct a store per handler over the same
// database_pending.json would otherwise split-brain: independent instances
// flush their own snapshots over each other and resurrect consumed entries.
var pendingStoreRegistry sync.Map // map[string]*PendingStore

// NewPendingStore opens (or creates) the pending-mutation file at path and
// returns the process-wide store for that absolute path: every caller sharing
// a path shares the same in-memory state and lock. Expired entries are purged
// on load. A corrupted file fails closed: the store starts empty and the
// unreadable file is renamed aside for forensics instead of being trusted or
// silently truncated.
func NewPendingStore(path string) (*PendingStore, error) {
	key := pendingStoreKey(path)
	if existing, ok := pendingStoreRegistry.Load(key); ok {
		return existing.(*PendingStore), nil
	}
	store, err := openPendingStore(path)
	if err != nil {
		return nil, err
	}
	actual, _ := pendingStoreRegistry.LoadOrStore(key, store)
	return actual.(*PendingStore), nil
}

// pendingStoreKey normalizes a store path so equivalent spellings (relative
// vs absolute, symlink/junction aliases, and — on Windows — case variants)
// share one in-process PendingStore instead of split-braining.
func pendingStoreKey(path string) string {
	key, err := filepath.Abs(path)
	if err != nil {
		key = path
	}
	if resolved, err := filepath.EvalSymlinks(key); err == nil {
		key = resolved
	}
	if runtime.GOOS == "windows" {
		key = strings.ToLower(key)
	}
	return key
}

// resetPendingStoresForTest drops every cached store so a test can simulate a
// process restart over the same path. Not for production use.
func resetPendingStoresForTest() {
	pendingStoreRegistry.Range(func(key, _ any) bool {
		pendingStoreRegistry.Delete(key)
		return true
	})
}

func openPendingStore(path string) (*PendingStore, error) {
	s := &PendingStore{path: path, items: make(map[string]PendingMutation)}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, err
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return s, nil
	}
	var loaded []PendingMutation
	if err := json.Unmarshal(data, &loaded); err != nil {
		quarantine := path + ".corrupt-" + time.Now().UTC().Format("20060102150405")
		if renameErr := os.Rename(path, quarantine); renameErr != nil {
			return nil, fmt.Errorf("pending store unreadable and cannot be quarantined: %w", err)
		}
		return s, nil
	}
	now := time.Now()
	for _, item := range loaded {
		if item.ID == "" || item.TokenHash == "" || !item.ExpiresAt.After(now) {
			continue
		}
		s.items[item.ID] = item
	}
	return s, nil
}

// Put records or replaces a pending mutation and persists the store.
func (s *PendingStore) Put(item PendingMutation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeExpiredLocked(time.Now())
	s.items[item.ID] = item
	return s.flushLocked()
}

// Consume atomically removes and returns the pending mutation with the given
// ID, enforcing expiry, the one-time token digest and the bound fingerprints.
// Any mismatch destroys nothing but returns an error, so a stale preview can
// never be committed.
func (s *PendingStore) Consume(id, tokenHash, sqlFingerprint, paramsFingerprint, profileID string, schemaVersion int) (PendingMutation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[id]
	if !ok {
		return PendingMutation{}, fmt.Errorf("permission: approval context is required")
	}
	if !item.ExpiresAt.After(time.Now()) {
		delete(s.items, id)
		_ = s.flushLocked()
		return PendingMutation{}, fmt.Errorf("permission: approval context expired")
	}
	if item.TokenHash != tokenHash {
		return PendingMutation{}, fmt.Errorf("permission: approval token mismatch")
	}
	if item.SQLFingerprint != sqlFingerprint ||
		(item.ParamsFingerprint != "" && item.ParamsFingerprint != paramsFingerprint) ||
		item.ProfileID != profileID || item.SchemaVersion != schemaVersion {
		return PendingMutation{}, fmt.Errorf("permission: approval context does not match the pending mutation")
	}
	delete(s.items, id)
	if err := s.flushLocked(); err != nil {
		return PendingMutation{}, err
	}
	return item, nil
}

// PurgeProfiles drops every pending mutation bound to the given profiles.
// Hosts call this when a profile is changed, disabled or rotated so approvals
// issued against the old configuration cannot be committed.
func (s *PendingStore) PurgeProfiles(profileIDs map[string]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for id, item := range s.items {
		if profileIDs[item.ProfileID] {
			delete(s.items, id)
			changed = true
		}
	}
	if changed {
		_ = s.flushLocked()
	}
}

// Len reports the number of live pending mutations (for diagnostics/tests).
func (s *PendingStore) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeExpiredLocked(time.Now())
	return len(s.items)
}

func (s *PendingStore) purgeExpiredLocked(now time.Time) {
	for id, item := range s.items {
		if !item.ExpiresAt.After(now) {
			delete(s.items, id)
		}
	}
}

// flushLocked rewrites the store atomically. Expired entries are dropped on
// every write so the file stays small without a background sweeper.
func (s *PendingStore) flushLocked() error {
	s.purgeExpiredLocked(time.Now())
	items := make([]PendingMutation, 0, len(s.items))
	for _, item := range s.items {
		items = append(items, item)
	}
	data, err := json.Marshal(items)
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// ApprovalRequest describes a mutation preview the user has just approved.
// The host (GUI task-panel, srv approval endpoint) builds it from the dry-run
// it is about to commit; the model never sees any of these fields.
// ProfileID may be empty for workspace file operations that carry no
// connection; such approvals are issued without profile/schema binding.
type ApprovalRequest struct {
	ProfileID         string
	SQLFingerprint    string
	ParamsFingerprint string
	OwnerID           string
	SessionID         string
	TTL               time.Duration
	OperationID       string
	Attempt           int
	ParentActionID    string
}

// IssueApproval mints a one-time approval context bound to the pending
// mutation. It is the only sanctioned way to obtain a commit token when the
// manager has a PendingStore configured: consumeApproval then refuses tokens
// it did not issue. The schema version is taken from the live profile so a
// host cannot approve against a stale configuration snapshot.
func (m *Manager) IssueApproval(req ApprovalRequest) (ApprovalContext, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return ApprovalContext{}, ErrManagerClosed
	}
	if !m.enabled || m.migrationError != "" || !SchemaContractLocked() {
		m.mu.Unlock()
		m.metrics.approvalsDenied.Add(1)
		return ApprovalContext{}, fmt.Errorf("permission: database tool is disabled")
	}
	profileID := strings.TrimSpace(req.ProfileID)
	profile, profileFound := m.profiles[profileID]
	store := m.pending
	gate := m.issueGate
	m.mu.Unlock()
	if gate != nil {
		if err := gate(req); err != nil {
			m.metrics.approvalsDenied.Add(1)
			return ApprovalContext{}, err
		}
	}
	if store == nil {
		m.metrics.approvalsDenied.Add(1)
		return ApprovalContext{}, fmt.Errorf("permission: no pending mutation store configured")
	}
	if profileID != "" && !profileFound {
		return ApprovalContext{}, fmt.Errorf("permission: profile not found")
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return ApprovalContext{}, err
	}
	token := hex.EncodeToString(raw)
	idSum := sha256.Sum256([]byte(token))
	now := time.Now()
	ttl := req.TTL
	if ttl <= 0 || ttl > 30*time.Minute {
		ttl = resultTTL
	}
	approval := ApprovalContext{
		Token:             token,
		ID:                "db-appr-" + hex.EncodeToString(idSum[:8]),
		SQLFingerprint:    strings.TrimSpace(req.SQLFingerprint),
		ParamsFingerprint: strings.TrimSpace(req.ParamsFingerprint),
		ExpiresAt:         now.Add(ttl),
		OperationID:       strings.TrimSpace(req.OperationID),
		Attempt:           req.Attempt,
		ParentActionID:    strings.TrimSpace(req.ParentActionID),
	}
	if approval.Attempt < 0 {
		approval.Attempt = 0
	}
	if profileFound {
		approval.ProfileID = profile.ID
		approval.SchemaVersion = profile.SchemaVersion
	}
	if approval.SQLFingerprint == "" {
		return ApprovalContext{}, fmt.Errorf("syntax: approval requires a SQL fingerprint")
	}
	err := store.Put(PendingMutation{
		ID:                approval.ID,
		TokenHash:         approvalTokenHash(token),
		ProfileID:         approval.ProfileID,
		SQLFingerprint:    approval.SQLFingerprint,
		ParamsFingerprint: approval.ParamsFingerprint,
		SchemaVersion:     approval.SchemaVersion,
		OwnerID:           strings.TrimSpace(req.OwnerID),
		SessionID:         strings.TrimSpace(req.SessionID),
		OperationID:       approval.OperationID,
		Attempt:           approval.Attempt,
		ParentActionID:    approval.ParentActionID,
		CreatedAt:         now,
		ExpiresAt:         approval.ExpiresAt,
	})
	if err != nil {
		m.metrics.approvalsDenied.Add(1)
		return ApprovalContext{}, err
	}
	m.metrics.approvalsIssued.Add(1)
	return approval, nil
}

func approvalTokenHash(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}
