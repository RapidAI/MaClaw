package main

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// FileChangeRecord tracks a file change event.
type FileChangeRecord struct {
	File      string    `json:"file"`
	Action    string    `json:"action"` // "create", "modify", "delete"
	Timestamp time.Time `json:"timestamp"`
	SessionID string    `json:"session_id"`
}

// DecisionRecord tracks a key decision made during a session.
type DecisionRecord struct {
	Description string    `json:"description"`
	Timestamp   time.Time `json:"timestamp"`
	SessionID   string    `json:"session_id"`
}

// ProjectContext holds shared context for a project across sessions.
type ProjectContext struct {
	ProjectPath string             `json:"project_path"`
	FileChanges []FileChangeRecord `json:"file_changes"`
	Decisions   []DecisionRecord   `json:"decisions"`
	Notes       []string           `json:"notes"`
	LastUpdated time.Time          `json:"last_updated"`
}

const maxContextRecords = 100

// ContextBridge manages cross-session context sharing for projects.
type ContextBridge struct {
	contexts map[string]*ProjectContext
	mu       sync.RWMutex
}

// NewContextBridge creates a new context bridge.
func NewContextBridge() *ContextBridge {
	return &ContextBridge{
		contexts: make(map[string]*ProjectContext),
	}
}

// getOrCreate returns the context for a project, creating if needed.
func (b *ContextBridge) getOrCreate(projectPath string) *ProjectContext {
	ctx, ok := b.contexts[projectPath]
	if !ok {
		ctx = &ProjectContext{ProjectPath: projectPath}
		b.contexts[projectPath] = ctx
	}
	return ctx
}

// ExtractFromEvents processes ImportantEvent entries and extracts context.
func (b *ContextBridge) ExtractFromEvents(projectPath string, events []ImportantEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()

	ctx := b.getOrCreate(projectPath)
	for _, e := range events {
		switch e.Type {
		case "file.change", "file.create", "file.delete":
			action := "modify"
			if e.Type == "file.create" {
				action = "create"
			} else if e.Type == "file.delete" {
				action = "delete"
			}
			ts := time.Unix(e.CreatedAt, 0)
			ctx.FileChanges = append(ctx.FileChanges, FileChangeRecord{
				File:      e.Summary,
				Action:    action,
				Timestamp: ts,
				SessionID: e.SessionID,
			})
		case "command.execute":
			ts := time.Unix(e.CreatedAt, 0)
			if isSignificantCommand(e.Summary) {
				ctx.Decisions = append(ctx.Decisions, DecisionRecord{
					Description: e.Summary,
					Timestamp:   ts,
					SessionID:   e.SessionID,
				})
			}
		}
	}

	// Trim to max records.
	if len(ctx.FileChanges) > maxContextRecords {
		ctx.FileChanges = ctx.FileChanges[len(ctx.FileChanges)-maxContextRecords:]
	}
	if len(ctx.Decisions) > maxContextRecords {
		ctx.Decisions = ctx.Decisions[len(ctx.Decisions)-maxContextRecords:]
	}
	ctx.LastUpdated = time.Now()
}

func isSignificantCommand(cmd string) bool {
	significant := []string{"git commit", "git merge", "npm install", "pip install",
		"go mod", "make", "deploy", "build", "test", "migrate"}
	lower := strings.ToLower(cmd)
	for _, s := range significant {
		if strings.Contains(lower, s) {
			return true
		}
	}
	return false
}

// BuildContextPrompt generates a concise context summary for injection
// into a new session's system prompt. Targets ~2000 tokens.
func (b *ContextBridge) BuildContextPrompt(projectPath string) string {
	b.mu.RLock()
	defer b.mu.RUnlock()

	ctx, ok := b.contexts[projectPath]
	if !ok || ctx == nil {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## 项目上下文\n\n")

	// Recent file changes (last 20).
	if len(ctx.FileChanges) > 0 {
		sb.WriteString("### 最近文件变更\n")
		start := 0
		if len(ctx.FileChanges) > 20 {
			start = len(ctx.FileChanges) - 20
		}
		for _, fc := range ctx.FileChanges[start:] {
			sb.WriteString(fmt.Sprintf("- [%s] %s: %s\n", fc.Action, fc.Timestamp.Format("01-02 15:04"), fc.File))
		}
		sb.WriteString("\n")
	}

	// Key decisions (last 10).
	if len(ctx.Decisions) > 0 {
		sb.WriteString("### 关键决策\n")
		start := 0
		if len(ctx.Decisions) > 10 {
			start = len(ctx.Decisions) - 10
		}
		for _, d := range ctx.Decisions[start:] {
			sb.WriteString(fmt.Sprintf("- %s (%s)\n", d.Description, d.Timestamp.Format("01-02 15:04")))
		}
		sb.WriteString("\n")
	}

	// User notes.
	if len(ctx.Notes) > 0 {
		sb.WriteString("### 用户备注\n")
		for _, n := range ctx.Notes {
			sb.WriteString(fmt.Sprintf("- %s\n", n))
		}
		sb.WriteString("\n")
	}

	result := sb.String()
	// Rough token limit (~4 chars per token, target 2000 tokens = 8000 chars).
	if len(result) > 8000 {
		result = result[:8000] + "\n...(已截断)"
	}
	return result
}

// AddNote adds a user note to a project's context.
func (b *ContextBridge) AddNote(projectPath, note string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	ctx := b.getOrCreate(projectPath)
	ctx.Notes = append(ctx.Notes, note)
	if len(ctx.Notes) > 50 {
		ctx.Notes = ctx.Notes[len(ctx.Notes)-50:]
	}
	ctx.LastUpdated = time.Now()
}

// GetContext returns the project context (read-only snapshot).
func (b *ContextBridge) GetContext(projectPath string) *ProjectContext {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.contexts[projectPath]
}
