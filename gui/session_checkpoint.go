package main

import (
	"fmt"
	"strings"
	"time"
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
	memoryStore   *MemoryStore
	contextBridge *ContextBridge
}

// NewSessionCheckpointer creates a SessionCheckpointer.
func NewSessionCheckpointer(ms *MemoryStore, cb *ContextBridge) *SessionCheckpointer {
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
		UserID:      "desktop-user",
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

	// Build a human-readable checkpoint content string.
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("会话进度快照 [%s]\n", cp.CreatedAt.Format("2006-01-02 15:04")))
	sb.WriteString(fmt.Sprintf("工具: %s | 项目: %s | 状态: %s\n", cp.Tool, cp.ProjectPath, cp.Status))
	if cp.LastTask != "" {
		sb.WriteString(fmt.Sprintf("最后任务: %s\n", cp.LastTask))
	}
	if cp.Summary != "" {
		sb.WriteString(fmt.Sprintf("进度摘要: %s\n", cp.Summary))
	}
	if len(eventSummaries) > 0 {
		sb.WriteString("事件记录:\n")
		// Keep last 10 events to stay concise.
		start := 0
		if len(eventSummaries) > 10 {
			start = len(eventSummaries) - 10
		}
		for _, es := range eventSummaries[start:] {
			sb.WriteString(fmt.Sprintf("  - %s\n", es))
		}
	}
	if len(cp.FileChanges) > 0 {
		sb.WriteString("文件变更:\n")
		limit := len(cp.FileChanges)
		if limit > 15 {
			limit = 15
		}
		for _, fc := range cp.FileChanges[:limit] {
			sb.WriteString(fmt.Sprintf("  - %s\n", fc))
		}
	}
	if len(cp.Decisions) > 0 {
		sb.WriteString("关键决策:\n")
		for _, d := range cp.Decisions {
			sb.WriteString(fmt.Sprintf("  - %s\n", d))
		}
	}

	entry := MemoryEntry{
		Content:  sb.String(),
		Category: MemCategorySessionCheckpoint,
		Tags: []string{
			"session_checkpoint",
			cp.ProjectPath,
			cp.Tool,
			cp.SessionID,
			cp.UserID,
			cp.SlotID,
		},
	}
	return c.memoryStore.Save(entry)
}

// RecallCheckpoint retrieves the most recent session checkpoint for a given
// project path. Returns empty string if no checkpoint exists.
func (c *SessionCheckpointer) RecallCheckpoint(projectPath string) string {
	if c.memoryStore == nil || projectPath == "" {
		return ""
	}

	entries := c.memoryStore.Search(MemCategorySessionCheckpoint, projectPath, 3)
	if len(entries) == 0 {
		return ""
	}

	// Return the most recent checkpoint (highest AccessCount or latest UpdatedAt).
	latest := entries[0]
	for _, e := range entries[1:] {
		if e.UpdatedAt.After(latest.UpdatedAt) {
			latest = e
		}
	}

	// Touch access count so frequently resumed projects stay in memory.
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
	sb.WriteString("## 上次会话进度\n\n")
	sb.WriteString("以下是上次在此项目上的工作进度，请在此基础上继续：\n\n")
	sb.WriteString(checkpoint)
	sb.WriteString("\n请基于以上进度继续工作。如果上次的任务已完成，请告知用户。\n")

	result := sb.String()
	if len(result) > 8000 {
		runes := []rune(result)
		if len(runes) > 2000 {
			runes = runes[:2000]
		}
		result = string(runes) + "\n...(已截断)"
	}
	return result
}

func (c *SessionCheckpointer) BuildResumePromptForSlot(slot *unfinishedTaskSlot) string {
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
