package memoryshot

import (
	"time"
)

const (
	CurrentVersion = "1.0"
	MaxBackups     = 5
)

// Snapshot represents a complete memory snapshot of the user's session.
type Snapshot struct {
	Version        string        `json:"version"`
	SavedAt        time.Time     `json:"saved_at"`
	ChatHistory    []ChatMessage `json:"chat_history"`     // Complete chat history
	CurrentProject string        `json:"current_project"`  // Current active project path
	ActiveTool     *ToolState    `json:"active_tool"`      // Currently running tool (if any)
	UIState        UIState       `json:"ui_state"`         // UI-specific state
}

// ChatMessage represents a single message in the chat history.
type ChatMessage struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	Timestamp int64  `json:"timestamp"` // Unix timestamp for ordering
}

// ToolState represents the state of a running tool/session.
type ToolState struct {
	Name        string            `json:"name"`
	ProjectPath string            `json:"project_path"`
	Status      string            `json:"status"` // running, paused, etc.
	Metadata    map[string]string `json:"metadata"` // Tool-specific data
}

// UIState stores UI-specific state that can be restored.
type UIState struct {
	ActiveTab      string `json:"active_tab"`       // Currently active tab/view
	ChatScrollPos  int    `json:"chat_scroll_pos"`  // Chat view scroll position
	SessionViewMode string `json:"session_view_mode"` // List or detail view
}

// IsEmpty returns true if the snapshot has no meaningful data.
func (s *Snapshot) IsEmpty() bool {
	return len(s.ChatHistory) == 0 && s.ActiveTool == nil && s.CurrentProject == ""
}

// Clone creates a deep copy of the snapshot.
func (s *Snapshot) Clone() *Snapshot {
	if s == nil {
		return nil
	}

	clone := &Snapshot{
		Version:        s.Version,
		SavedAt:        s.SavedAt,
		CurrentProject: s.CurrentProject,
		UIState:        s.UIState,
	}

	if len(s.ChatHistory) > 0 {
		clone.ChatHistory = make([]ChatMessage, len(s.ChatHistory))
		copy(clone.ChatHistory, s.ChatHistory)
	}

	if s.ActiveTool != nil {
		clone.ActiveTool = &ToolState{
			Name:        s.ActiveTool.Name,
			ProjectPath: s.ActiveTool.ProjectPath,
			Status:      s.ActiveTool.Status,
			Metadata:    make(map[string]string),
		}
		for k, v := range s.ActiveTool.Metadata {
			clone.ActiveTool.Metadata[k] = v
		}
	}

	return clone
}
