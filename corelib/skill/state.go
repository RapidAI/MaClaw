package skill

// state.go implements cross-invocation state persistence for skills.
//
// This is the "baton relay" pattern from the "5 Skill Architecture Patterns"
// article: a skill writes a next-prompt.md (baton) at the end of each run,
// and reads it at the start of the next run. The LLM doesn't need to
// remember "where we left off" — it reads the baton file.
//
// State is stored in {skillDir}/.state/state.json. Each skill has its own
// independent state — no cross-skill shared state (pipeline uses vars).
//
// Lifecycle:
//   - LoadState: called before skill execution, returns empty state if none
//   - SaveState: called after skill execution, persists vars + history
//   - ClearState: called by manage_skill(action=state, sub_action=clear)
//
// State is NOT session-scoped (that's capture/vars). State is NOT
// process-scoped (that's in-flight task marker). State is business-scoped:
// it persists across days/weeks of user interaction with the same skill.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SkillState is the cross-invocation persistent state for a skill.
type SkillState struct {
	// CurrentPhase is a skill-defined phase identifier (e.g. "literature_review").
	CurrentPhase string `json:"current_phase,omitempty"`

	// Vars are persistent variables that survive across invocations.
	// Keys are skill-defined (e.g. "session_id", "completed_modules").
	Vars map[string]string `json:"vars,omitempty"`

	// History records the last N execution summaries (max 10).
	History []StateHistoryEntry `json:"history,omitempty"`

	// NextPrompt is the "baton" content — injected into LLM context
	// at the start of the next invocation. The skill writes this at
	// the end of each run to tell the next run what to do.
	NextPrompt string `json:"next_prompt,omitempty"`

	// UpdatedAt is the last modification timestamp (RFC3339).
	UpdatedAt string `json:"updated_at"`
}

// StateHistoryEntry records one execution of the skill.
type StateHistoryEntry struct {
	Timestamp string `json:"timestamp"`
	Phase     string `json:"phase,omitempty"`
	Summary   string `json:"summary"`
	Success   bool   `json:"success"`
}

const (
	stateDir      = ".state"
	stateFile     = "state.json"
	maxHistoryLen = 10
)

// LoadState loads the skill's persistent state from {skillDir}/.state/state.json.
// Returns an empty SkillState (not nil) if the file doesn't exist or is corrupted.
// Corrupted state (e.g. from a process kill during SaveState) is logged and
// treated as empty — the skill starts fresh rather than failing to execute.
func LoadState(skillDir string) (*SkillState, error) {
	path := filepath.Join(skillDir, stateDir, stateFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &SkillState{}, nil
		}
		return nil, err
	}
	var state SkillState
	if err := json.Unmarshal(data, &state); err != nil {
		// Corrupted JSON — likely from a process kill during SaveState.
		// Degrade to empty state rather than blocking skill execution.
		// The .tmp file from atomic write may still exist; clean it up.
		os.Remove(path + ".tmp")
		return &SkillState{}, nil
	}
	return &state, nil
}

// SaveState persists the skill's state to {skillDir}/.state/state.json.
// Creates the .state directory if it doesn't exist.
// Uses atomic write (temp file + rename) to prevent corruption if the
// process is killed mid-write (same pattern as ConversationMemory.FlushNow).
func SaveState(skillDir string, state *SkillState) error {
	dir := filepath.Join(skillDir, stateDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	state.UpdatedAt = time.Now().Format(time.RFC3339)
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	target := filepath.Join(dir, stateFile)
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, target)
}

// ClearState removes the skill's persistent state directory.
func ClearState(skillDir string) error {
	return os.RemoveAll(filepath.Join(skillDir, stateDir))
}

// AppendHistory adds an entry to the history, keeping only the last maxHistoryLen entries.
func (s *SkillState) AppendHistory(entry StateHistoryEntry) {
	if entry.Timestamp == "" {
		entry.Timestamp = time.Now().Format(time.RFC3339)
	}
	s.History = append(s.History, entry)
	if len(s.History) > maxHistoryLen {
		s.History = s.History[len(s.History)-maxHistoryLen:]
	}
}

// SetVar sets a persistent variable. Creates the Vars map if nil.
func (s *SkillState) SetVar(key, value string) {
	if s.Vars == nil {
		s.Vars = make(map[string]string)
	}
	s.Vars[key] = value
}

// GetVar returns a persistent variable value, or empty string if not set.
func (s *SkillState) GetVar(key string) string {
	if s.Vars == nil {
		return ""
	}
	return s.Vars[key]
}

// IsEmpty returns true if the state has no meaningful content.
func (s *SkillState) IsEmpty() bool {
	return s.CurrentPhase == "" &&
		len(s.Vars) == 0 &&
		len(s.History) == 0 &&
		s.NextPrompt == ""
}

// FormatForContext generates a human-readable summary of the state
// for injection into LLM context.
func (s *SkillState) FormatForContext() string {
	if s.IsEmpty() {
		return ""
	}
	var parts []string
	if s.CurrentPhase != "" {
		parts = append(parts, "当前阶段: "+s.CurrentPhase)
	}
	if s.NextPrompt != "" {
		parts = append(parts, "接力棒: "+s.NextPrompt)
	}
	if len(s.Vars) > 0 {
		vars := []string{}
		for k, v := range s.Vars {
			vars = append(vars, k+"="+v)
		}
		parts = append(parts, "持久变量: "+strings.Join(vars, ", "))
	}
	if len(s.History) > 0 {
		last := s.History[len(s.History)-1]
		status := "成功"
		if !last.Success {
			status = "失败"
		}
		parts = append(parts, "上次执行: "+last.Summary+" ("+status+", "+last.Timestamp+")")
	}
	return strings.Join(parts, "\n")
}
