package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

// TrajectoryEntry represents a single turn in an LLM conversation trajectory.
type TrajectoryEntry struct {
	Timestamp  string      `json:"timestamp"`
	Role       string      `json:"role"`                  // "system", "user", "assistant", "tool"
	Content    interface{} `json:"content"`                // text or multimodal content
	ToolCalls  interface{} `json:"tool_calls,omitempty"`   // assistant tool calls
	ToolCallID string      `json:"tool_call_id,omitempty"` // tool result correlation
	Reasoning  string      `json:"reasoning,omitempty"`    // reasoning_content if present
}

// TrajectorySession holds all entries for a single agent loop session.
type TrajectorySession struct {
	SessionID string            `json:"session_id"`
	StartTime string            `json:"start_time"`
	EndTime   string            `json:"end_time,omitempty"`
	Provider  string            `json:"provider"`
	Model     string            `json:"model"`
	Protocol  string            `json:"protocol"`
	UserID    string            `json:"user_id"`
	Platform  string            `json:"platform"`
	Tools     []interface{}     `json:"tools,omitempty"`
	Entries   []TrajectoryEntry `json:"entries"`
}

// TrajectoryRecorder records LLM interaction trajectories to disk.
type TrajectoryRecorder struct {
	mu      sync.Mutex
	dir     string // ~/.maclaw/trajectories
	session *TrajectorySession
}

// safeFilenameRe strips characters that are invalid in file names.
var safeFilenameRe = regexp.MustCompile(`[^a-zA-Z0-9_\-]`)

// NewTrajectoryRecorder creates a recorder that writes to ~/.maclaw/trajectories.
func NewTrajectoryRecorder() *TrajectoryRecorder {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return &TrajectoryRecorder{
		dir: filepath.Join(home, ".maclaw", "trajectories"),
	}
}

// StartSession begins recording a new trajectory session.
func (r *TrajectoryRecorder) StartSession(sessionID, provider, model, protocol, userID, platform string, tools []map[string]interface{}) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var toolsCopy []interface{}
	for _, t := range tools {
		toolsCopy = append(toolsCopy, t)
	}

	r.session = &TrajectorySession{
		SessionID: sessionID,
		StartTime: time.Now().Format(time.RFC3339),
		Provider:  provider,
		Model:     model,
		Protocol:  protocol,
		UserID:    userID,
		Platform:  platform,
		Tools:     toolsCopy,
	}
}

// Record appends a conversation entry to the current session.
func (r *TrajectoryRecorder) Record(role string, content interface{}, toolCalls interface{}, toolCallID string, reasoning string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.session == nil {
		return
	}
	r.session.Entries = append(r.session.Entries, TrajectoryEntry{
		Timestamp:  time.Now().Format(time.RFC3339Nano),
		Role:       role,
		Content:    content,
		ToolCalls:  toolCalls,
		ToolCallID: toolCallID,
		Reasoning:  reasoning,
	})
}

// Flush writes the current session to a JSON file and resets state.
// Safe to call multiple times; no-op if session is nil or empty.
func (r *TrajectoryRecorder) Flush() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.session == nil || len(r.session.Entries) == 0 {
		return
	}
	r.session.EndTime = time.Now().Format(time.RFC3339)

	if err := os.MkdirAll(r.dir, 0755); err != nil {
		log.Printf("[Trajectory] failed to create dir %s: %v", r.dir, err)
		r.session = nil
		return
	}

	// Use millisecond-precision timestamp + sanitized session ID for uniqueness.
	ts := time.Now().Format("2006-01-02_15-04-05.000")
	safeID := safeFilenameRe.ReplaceAllString(r.session.SessionID, "_")
	if len(safeID) > 32 {
		safeID = safeID[:32]
	}
	filename := fmt.Sprintf("%s_%s.json", ts, safeID)
	path := filepath.Join(r.dir, filename)

	data, err := json.MarshalIndent(r.session, "", "  ")
	if err != nil {
		log.Printf("[Trajectory] marshal error: %v", err)
		r.session = nil
		return
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		log.Printf("[Trajectory] write error: %v", err)
		r.session = nil
		return
	}
	log.Printf("[Trajectory] saved %s (%d entries)", path, len(r.session.Entries))
	r.session = nil
}
