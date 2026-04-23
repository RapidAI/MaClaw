package agent

// confirmation_store.go implements the pre-execution confirmation store.
//
// This is a pure data structure with no GUI dependencies. It manages
// pending confirmations that gate agent execution until the user approves.
//
// Migrated from gui/im_confirmation_store.go as part of the agent-unification plan.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ConfirmationTTL is the maximum age of a pending confirmation before it
// is automatically expired.
const ConfirmationTTL = 2 * time.Hour

// PendingConfirmation represents a task awaiting user approval before execution.
type PendingConfirmation struct {
	ID              string    `json:"id"`
	UserID          string    `json:"user_id"`
	OriginalText    string    `json:"original_text"`
	ResumeText      string    `json:"resume_text"`
	Summary         string    `json:"summary"`
	TaskType        string    `json:"task_type"`
	TargetPaths     []string  `json:"target_paths,omitempty"`
	PlannedActions  []string  `json:"planned_actions,omitempty"`
	RiskFlags       []string  `json:"risk_flags,omitempty"`
	RevisionHints   []string  `json:"revision_hints,omitempty"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	LastProjectPath string    `json:"last_project_path,omitempty"`

	// EnhancedSummary is the LLM-generated structured understanding of the
	// user's request. When non-empty, it replaces the raw-text Summary in
	// the confirmation card shown to the user.
	EnhancedSummary string `json:"enhanced_summary,omitempty"`

	// EnhancedInstruction is the LLM-generated structured instruction that
	// replaces OriginalText as the agent loop input after user confirms.
	// This gives the agent a clearer, more actionable directive than the
	// user's raw conversational input.
	EnhancedInstruction string `json:"enhanced_instruction,omitempty"`
}

// confirmationSnapshot is the on-disk serialization format.
type confirmationSnapshot struct {
	Items map[string]*PendingConfirmation `json:"items"`
}

// ConfirmationStore manages pending confirmations with thread-safe access
// and optional disk persistence.
type ConfirmationStore struct {
	mu        sync.RWMutex
	items     map[string]*PendingConfirmation
	storePath string
}

// NewConfirmationStore creates a new store. If storePath is non-empty, the
// store will persist to disk on every mutation and load existing data on
// creation.
func NewConfirmationStore(storePath string) *ConfirmationStore {
	s := &ConfirmationStore{
		items:     make(map[string]*PendingConfirmation),
		storePath: strings.TrimSpace(storePath),
	}
	_ = s.loadFromDisk()
	return s
}

// Get returns a deep copy of the pending confirmation for the given user,
// or nil if none exists.
func (s *ConfirmationStore) Get(userID string) *PendingConfirmation {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item := s.items[userID]
	if item == nil {
		return nil
	}
	clone := *item
	clone.TargetPaths = append([]string(nil), item.TargetPaths...)
	clone.PlannedActions = append([]string(nil), item.PlannedActions...)
	clone.RiskFlags = append([]string(nil), item.RiskFlags...)
	clone.RevisionHints = append([]string(nil), item.RevisionHints...)
	return &clone
}

// Set stores a pending confirmation (deep-copied) and persists to disk.
func (s *ConfirmationStore) Set(item *PendingConfirmation) {
	if item == nil || strings.TrimSpace(item.UserID) == "" {
		return
	}
	clone := *item
	clone.TargetPaths = append([]string(nil), item.TargetPaths...)
	clone.PlannedActions = append([]string(nil), item.PlannedActions...)
	clone.RiskFlags = append([]string(nil), item.RiskFlags...)
	clone.RevisionHints = append([]string(nil), item.RevisionHints...)
	s.mu.Lock()
	s.items[item.UserID] = &clone
	s.mu.Unlock()
	_ = s.saveToDisk()
}

// Clear removes the pending confirmation for the given user.
func (s *ConfirmationStore) Clear(userID string) {
	s.mu.Lock()
	delete(s.items, userID)
	s.mu.Unlock()
	_ = s.saveToDisk()
}

// ClearExpired removes all confirmations older than ConfirmationTTL.
func (s *ConfirmationStore) ClearExpired(now time.Time) {
	s.mu.Lock()
	changed := false
	for userID, item := range s.items {
		if item == nil || now.Sub(item.UpdatedAt) > ConfirmationTTL {
			delete(s.items, userID)
			changed = true
		}
	}
	s.mu.Unlock()
	if changed {
		_ = s.saveToDisk()
	}
}

// Stop is a no-op placeholder for interface compatibility.
func (s *ConfirmationStore) Stop() {}

func (s *ConfirmationStore) saveToDisk() error {
	if s == nil || s.storePath == "" {
		return nil
	}
	s.mu.RLock()
	snapshot := confirmationSnapshot{Items: make(map[string]*PendingConfirmation, len(s.items))}
	for userID, item := range s.items {
		if item == nil {
			continue
		}
		clone := *item
		clone.TargetPaths = append([]string(nil), item.TargetPaths...)
		clone.PlannedActions = append([]string(nil), item.PlannedActions...)
		clone.RiskFlags = append([]string(nil), item.RiskFlags...)
		clone.RevisionHints = append([]string(nil), item.RevisionHints...)
		snapshot.Items[userID] = &clone
	}
	s.mu.RUnlock()
	if err := os.MkdirAll(filepath.Dir(s.storePath), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	tmp := s.storePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.storePath)
}

func (s *ConfirmationStore) loadFromDisk() error {
	if s == nil || s.storePath == "" {
		return nil
	}
	data, err := os.ReadFile(s.storePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var snapshot confirmationSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for userID, item := range snapshot.Items {
		if item == nil {
			continue
		}
		clone := *item
		clone.TargetPaths = append([]string(nil), item.TargetPaths...)
		clone.PlannedActions = append([]string(nil), item.PlannedActions...)
		clone.RiskFlags = append([]string(nil), item.RiskFlags...)
		clone.RevisionHints = append([]string(nil), item.RevisionHints...)
		s.items[userID] = &clone
	}
	return nil
}
