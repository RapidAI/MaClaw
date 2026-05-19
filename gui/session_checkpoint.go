package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/memory"
)

// SessionCheckpoint captures the progress state of a session at exit time,
// enabling the next session on the same project to resume where it left off.
type SessionCheckpoint struct {
	SessionID   string    `json:"session_id"`
	UserID      string    `json:"user_id,omitempty"`
	SlotID      string    `json:"slot_id,omitempty"`
	Tool        string    `json:"tool"`
	ProjectPath string    `json:"project_path"`
	Status      string    `json:"status"`
	Summary     string    `json:"summary"`
	LastTask    string    `json:"last_task"`
	FileChanges []string  `json:"file_changes,omitempty"`
	Decisions   []string  `json:"decisions,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// SessionCheckpointer archives session progress into MemoryStore when a
// session exits, and retrieves the latest checkpoint when a new session
// starts on the same project.
type SessionCheckpointer struct {
	memoryStore   *memory.Store
	contextBridge *ContextBridge
}

// NewSessionCheckpointer creates a SessionCheckpointer.
func NewSessionCheckpointer(ms *memory.Store, cb *ContextBridge) *SessionCheckpointer {
	if ms == nil {
		return nil
	}
	return &SessionCheckpointer{
		memoryStore:   ms,
		contextBridge: cb,
	}
}

// SaveCheckpoint extracts progress from a completed session and stores it
// as a memory entry. It captures the session summary, recent events, and
// file changes from the context bridge.
func (c *SessionCheckpointer) SaveCheckpoint(session *RemoteSession) error {
	if session == nil || c.memoryStore == nil {
		return nil
	}

	session.mu.RLock()
	cp := SessionCheckpoint{
		SessionID:   session.ID,
		UserID:      desktopUserID,
		Tool:        session.Tool,
		ProjectPath: session.ProjectPath,
		Status:      string(session.Status),
		Summary:     session.Summary.ProgressSummary,
		LastTask:    session.Summary.CurrentTask,
		CreatedAt:   time.Now(),
	}
	if session.ResumeContext != nil {
		cp.LastTask = firstNonEmptyTraceText(cp.LastTask, session.ResumeContext.OriginalTask)
	}

	// Collect recent event summaries as a progress trail.
	var eventSummaries []string
	for _, evt := range session.Events {
		if evt.Summary != "" {
			eventSummaries = append(eventSummaries, fmt.Sprintf("[%s] %s", evt.Type, evt.Summary))
		}
	}
	session.mu.RUnlock()

	// Pull file changes and decisions from context bridge if available.
	if c.contextBridge != nil && cp.ProjectPath != "" {
		ctx := c.contextBridge.GetContext(cp.ProjectPath)
		if ctx != nil {
			for _, fc := range ctx.FileChanges {
				if fc.SessionID == cp.SessionID {
					cp.FileChanges = append(cp.FileChanges, fmt.Sprintf("%s: %s", fc.Action, fc.File))
				}
			}
			for _, d := range ctx.Decisions {
				if d.SessionID == cp.SessionID {
					cp.Decisions = append(cp.Decisions, d.Description)
				}
			}
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Session checkpoint [%s]\n", cp.CreatedAt.Format("2006-01-02 15:04")))
	sb.WriteString(fmt.Sprintf("Tool: %s | Project: %s | Status: %s\n", cp.Tool, cp.ProjectPath, cp.Status))
	if cp.LastTask != "" {
		sb.WriteString(fmt.Sprintf("Last task: %s\n", cp.LastTask))
	}
	if cp.Summary != "" {
		sb.WriteString(fmt.Sprintf("Progress summary: %s\n", cp.Summary))
	}
	if len(eventSummaries) > 0 {
		sb.WriteString("Recent events:\n")
		start := 0
		if len(eventSummaries) > 10 {
			start = len(eventSummaries) - 10
		}
		for _, es := range eventSummaries[start:] {
			sb.WriteString(fmt.Sprintf("  - %s\n", es))
		}
	}
	if len(cp.FileChanges) > 0 {
		sb.WriteString("File changes:\n")
		limit := len(cp.FileChanges)
		if limit > 15 {
			limit = 15
		}
		for _, fc := range cp.FileChanges[:limit] {
			sb.WriteString(fmt.Sprintf("  - %s\n", fc))
		}
	}
	if len(cp.Decisions) > 0 {
		sb.WriteString("Key decisions:\n")
		for _, d := range cp.Decisions {
			sb.WriteString(fmt.Sprintf("  - %s\n", d))
		}
	}

	tags := []string{
		"session_checkpoint",
		cp.ProjectPath,
		cp.Tool,
		cp.SessionID,
		cp.UserID,
		cp.SlotID,
	}
	_, err := c.memoryStore.UpsertSessionCheckpoint(memory.SessionCheckpointUpsertOptions{
		Title:            firstNonEmptyTraceText(cp.LastTask, "Session checkpoint"),
		Content:          sb.String(),
		Tags:             tags,
		IdentityTagCount: 4,
		OwnerID:          cp.UserID,
	})
	return err
}

// RecallCheckpoint retrieves the most recent session checkpoint for a given
// project path. Returns empty string if no checkpoint exists.
func (c *SessionCheckpointer) RecallCheckpoint(projectPath string) string {
	if c.memoryStore == nil || projectPath == "" {
		return ""
	}

	entries := c.memoryStore.Search(memory.CategorySessionCheckpoint, projectPath, 3)
	if len(entries) == 0 {
		return ""
	}

	latest := entries[0]
	for _, e := range entries[1:] {
		if e.UpdatedAt.After(latest.UpdatedAt) {
			latest = e
		}
	}

	c.memoryStore.TouchAccess([]string{latest.ID})
	return latest.Content
}

// BuildResumePrompt constructs a prompt fragment that can be injected into
// a new session's initial message, giving the model context about what was
// done previously.
func (c *SessionCheckpointer) BuildResumePrompt(projectPath string) string {
	checkpoint := c.RecallCheckpoint(projectPath)
	if checkpoint == "" {
		return ""
	}
	return buildCheckpointResumePrompt(checkpoint)
}

func buildCheckpointResumePrompt(checkpoint string) string {
	if strings.TrimSpace(checkpoint) == "" {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Previous Session Progress\n\n")
	sb.WriteString("The following checkpoint summarizes prior work on this project. Continue from it when relevant.\n\n")
	sb.WriteString(checkpoint)
	sb.WriteString("\nContinue based on this checkpoint. If the prior task is already complete, tell the user.\n")

	result := sb.String()
	if len(result) > 8000 {
		runes := []rune(result)
		if len(runes) > 2000 {
			runes = runes[:2000]
		}
		result = string(runes) + "\n...(truncated)"
	}
	return result
}

func (c *SessionCheckpointer) BuildResumePromptForSlot(slot *agent.UnfinishedTaskSlot) string {
	if slot == nil {
		return ""
	}
	if strings.TrimSpace(slot.ResumePrompt) != "" {
		return slot.ResumePrompt
	}
	if strings.TrimSpace(slot.ProjectPath) == "" {
		return ""
	}
	return c.BuildResumePrompt(slot.ProjectPath)
}
