package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const confirmationTTL = 2 * time.Hour

type pendingConfirmation struct {
	ID              string             `json:"id"`
	UserID          string             `json:"user_id"`
	OriginalText    string             `json:"original_text"`
	ResumeText      string             `json:"resume_text"`
	Summary         string             `json:"summary"`
	TaskType        string             `json:"task_type"`
	TargetPaths     []string           `json:"target_paths,omitempty"`
	PlannedActions  []string           `json:"planned_actions,omitempty"`
	RiskFlags       []string           `json:"risk_flags,omitempty"`
	RevisionHints   []string           `json:"revision_hints,omitempty"`
	Status          confirmationStatus `json:"status"`
	CreatedAt       time.Time          `json:"created_at"`
	UpdatedAt       time.Time          `json:"updated_at"`
	LastProjectPath string             `json:"last_project_path,omitempty"`

	// EnhancedSummary is the LLM-generated structured understanding of the
	// user's request. When non-empty, it replaces the raw-text Summary in
	// the confirmation card shown to the user.
	EnhancedSummary string `json:"enhanced_summary,omitempty"`

	// EnhancedInstruction is the LLM-generated structured instruction that
	// replaces OriginalText as the agent loop input after user confirms.
	// This gives the agent a clearer, more actionable directive than the
	// user's raw conversational input.
	EnhancedInstruction string `json:"enhanced_instruction,omitempty"`

	// Workflow confirmation fields are set when the user is asked whether to
	// start a matched multi-phase workflow. If the user declines, the original
	// request falls through to the normal agent loop once.
	WorkflowType        string   `json:"workflow_type,omitempty"`
	WorkflowSummary     string   `json:"workflow_summary,omitempty"`
	WorkflowGoals       []string `json:"workflow_goals,omitempty"`
	WorkflowConstraints []string `json:"workflow_constraints,omitempty"`
	WorkflowConfidence  float64  `json:"workflow_confidence,omitempty"`
	WorkflowStartReply  string   `json:"workflow_start_reply,omitempty"`
}

type confirmationSnapshot struct {
	Items map[string]*pendingConfirmation `json:"items"`
}

type aiConfirmationStore struct {
	mu        sync.RWMutex
	items     map[string]*pendingConfirmation
	storePath string
}

func newAIConfirmationStore(storePath string) *aiConfirmationStore {
	s := &aiConfirmationStore{
		items:     make(map[string]*pendingConfirmation),
		storePath: strings.TrimSpace(storePath),
	}
	_ = s.loadFromDisk()
	return s
}

func (s *aiConfirmationStore) get(userID string) *pendingConfirmation {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item := s.items[userID]
	if item == nil {
		return nil
	}
	clone := *item
	clone.Status = normalizeConfirmationStatus(clone.Status.String())
	clone.TargetPaths = append([]string(nil), item.TargetPaths...)
	clone.PlannedActions = append([]string(nil), item.PlannedActions...)
	clone.RiskFlags = append([]string(nil), item.RiskFlags...)
	clone.RevisionHints = append([]string(nil), item.RevisionHints...)
	clone.WorkflowGoals = append([]string(nil), item.WorkflowGoals...)
	clone.WorkflowConstraints = append([]string(nil), item.WorkflowConstraints...)
	return &clone
}

func (s *aiConfirmationStore) set(item *pendingConfirmation) {
	if item == nil || strings.TrimSpace(item.UserID) == "" {
		return
	}
	clone := *item
	clone.Status = normalizeConfirmationStatus(clone.Status.String())
	clone.TargetPaths = append([]string(nil), item.TargetPaths...)
	clone.PlannedActions = append([]string(nil), item.PlannedActions...)
	clone.RiskFlags = append([]string(nil), item.RiskFlags...)
	clone.RevisionHints = append([]string(nil), item.RevisionHints...)
	clone.WorkflowGoals = append([]string(nil), item.WorkflowGoals...)
	clone.WorkflowConstraints = append([]string(nil), item.WorkflowConstraints...)
	s.mu.Lock()
	s.items[item.UserID] = &clone
	s.mu.Unlock()
	_ = s.saveToDisk()
}

func (s *aiConfirmationStore) clear(userID string) {
	s.mu.Lock()
	delete(s.items, userID)
	s.mu.Unlock()
	_ = s.saveToDisk()
}

func (s *aiConfirmationStore) clearExpired(now time.Time) {
	s.mu.Lock()
	changed := false
	for userID, item := range s.items {
		if item == nil || now.Sub(item.UpdatedAt) > confirmationTTL {
			delete(s.items, userID)
			changed = true
		}
	}
	s.mu.Unlock()
	if changed {
		_ = s.saveToDisk()
	}
}

func (s *aiConfirmationStore) stop() {}

func (s *aiConfirmationStore) saveToDisk() error {
	if s == nil || s.storePath == "" {
		return nil
	}
	s.mu.RLock()
	snapshot := confirmationSnapshot{Items: make(map[string]*pendingConfirmation, len(s.items))}
	for userID, item := range s.items {
		if item == nil {
			continue
		}
		clone := *item
		clone.Status = normalizeConfirmationStatus(clone.Status.String())
		clone.TargetPaths = append([]string(nil), item.TargetPaths...)
		clone.PlannedActions = append([]string(nil), item.PlannedActions...)
		clone.RiskFlags = append([]string(nil), item.RiskFlags...)
		clone.RevisionHints = append([]string(nil), item.RevisionHints...)
		clone.WorkflowGoals = append([]string(nil), item.WorkflowGoals...)
		clone.WorkflowConstraints = append([]string(nil), item.WorkflowConstraints...)
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

func (s *aiConfirmationStore) loadFromDisk() error {
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
		clone.Status = normalizeConfirmationStatus(clone.Status.String())
		clone.TargetPaths = append([]string(nil), item.TargetPaths...)
		clone.PlannedActions = append([]string(nil), item.PlannedActions...)
		clone.RiskFlags = append([]string(nil), item.RiskFlags...)
		clone.RevisionHints = append([]string(nil), item.RevisionHints...)
		clone.WorkflowGoals = append([]string(nil), item.WorkflowGoals...)
		clone.WorkflowConstraints = append([]string(nil), item.WorkflowConstraints...)
		s.items[userID] = &clone
	}
	return nil
}
