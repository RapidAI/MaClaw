package main

import (
	"crypto/sha256"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/memory"
)

// sideEffectTools lists tools that produce persistent external changes.
// This is the mechanism-level signal for "task with tangible output":
// if the agent loop called any of these, the conversation produced
// something worth remembering. Pure-read tools (web_search, read_file,
// list_directory, memory, screenshot) are excluded because they do not
// change external state.
var sideEffectTools = map[string]bool{
	"write_file": true, "edit_file": true, "bash": true,
	"ssh":          true,
	"generate_pdf": true, "send_file": true,
	"manage_skill": true, "run_skill": true,
	"create_session": true, "send_and_observe": true,
	"browser":         true,
	"manage_schedule": true, "manage_template": true,
	"async_wait": true,
}

// sedimentTaskEntry saves a lightweight project_knowledge entry when the
// agent loop produced tangible output, so the task appears in the recent
// tasks list.
//
// Gate: at least one side-effect tool call must be present in the
// conversation history. This objectively separates "tasks with output"
// from pure chat / lookups / reads.
//
// Project path: every task gets a unique standalone path derived from its
// title hash, so each task appears as a separate item in the sidebar.
// The current project path is added as a secondary tag for search affinity
// but does NOT determine the index key (inferProjectPath picks the first
// path-like tag, which is the standalone path).
func (h *IMMessageHandler) sedimentTaskEntry(userID string, history []agent.ConversationEntry) {
	if h.memoryStore == nil || len(history) == 0 {
		return
	}

	// Check for at least one side-effect tool call.
	hasSideEffect := false
	for _, e := range history {
		if hasSideEffect {
			break
		}
		if e.Role != "assistant" || e.ToolCalls == nil {
			continue
		}
		names := make(map[string]bool)
		collectToolNameSet(e.ToolCalls, names)
		for name := range names {
			if sideEffectTools[name] {
				hasSideEffect = true
				break
			}
		}
	}
	if !hasSideEffect {
		return
	}

	// First user message = task description.
	var userRequest string
	for _, e := range history {
		if e.Role == "user" {
			if s, ok := e.Content.(string); ok && strings.TrimSpace(s) != "" {
				userRequest = strings.TrimSpace(s)
				break
			}
		}
	}
	if userRequest == "" {
		return
	}

	// Skip system-generated switch messages.
	if r := []rune(userRequest); len(r) > 0 && (r[0] == 0x1F516 || r[0] == 0x1F4C2 || r[0] == 0x1F4C1) {
		return
	}

	title := buildSedimentTitle(userRequest)
	if title == "" {
		return
	}

	// Brief content: request + last assistant snippet.
	var lastReply string
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == "assistant" {
			if s, ok := history[i].Content.(string); ok && strings.TrimSpace(s) != "" {
				lastReply = strings.TrimSpace(s)
				break
			}
		}
	}
	var buf strings.Builder
	buf.WriteString("Task: ")
	buf.WriteString(truncSediment(userRequest, 200))
	if lastReply != "" {
		buf.WriteString("\nResult: ")
		buf.WriteString(truncSediment(lastReply, 300))
	}

	// Determine project path.
	// Every task gets a standalone path (appears as its own list item).
	// The current project path is added as a secondary tag for affinity.
	standalonePath, projectTag := h.resolveTaskProjectPath(title)
	tags := []string{"task_sediment", "auto"}
	if standalonePath != "" {
		tags = append(tags, standalonePath)
	}
	if projectTag != "" && projectTag != standalonePath {
		tags = append(tags, projectTag)
	}

	entry := memory.Entry{
		Content:    buf.String(),
		Title:      title,
		Category:   memory.CategoryProjectKnowledge,
		Tags:       tags,
		Scope:      memory.ScopeProject,
		SourceType: "task_sediment",
		OwnerID:    userID, // multi-tenant: associate with the user who ran this task
	}
	if err := h.memoryStore.Save(entry); err != nil {
		log.Printf("[task_sediment] save failed: %v", err)
	} else if h.app != nil {
		h.app.triggerMemoryPipelineSoon(45 * time.Second)
	}
}

// resolveTaskProjectPath returns the standalone path and project tag for a
// task sediment entry.
//
// Every task gets its own standalone path so it appears as a separate item
// in the recent tasks list. The standalone path is derived from the task
// title (deterministic hash), ensuring:
//   - Different tasks → different paths → separate list items
//   - Same task re-run → same path → updates existing item (idempotent)
//
// The current project path is returned as a secondary tag for search
// affinity. The caller adds both to Tags; inferProjectPath picks the
// standalone path (first path-like tag) as the index key.
func (h *IMMessageHandler) resolveTaskProjectPath(title string) (standalone string, projectTag string) {
	maclawDataDir := ""
	if h.app != nil {
		maclawDataDir = h.app.GetDataDir()
	}
	standalone = buildStandaloneTaskPath(maclawDataDir, title)
	projectTag = h.getCurrentProjectPath()
	return
}

// buildStandaloneTaskPath creates a synthetic path for standalone tasks
// (tasks not tied to a specific project directory). The path is:
//
//	{maclawDataDir}/tasks/{title-hash-prefix}
//
// This is a real directory path that passes looksLikeProjectPath validation
// in ProjectIndex.inferProjectPath (Windows drive letter + 2+ segments + no
// short file extension).
//
// Properties:
//   - Deterministic: same title → same path (idempotent, no duplicate entries)
//   - Unique: different titles → different paths (each task gets its own entry)
//   - Valid: passes all ProjectIndex path validation checks
func buildStandaloneTaskPath(maclawDataDir, title string) string {
	if maclawDataDir == "" {
		return ""
	}
	// Use a short hash of the title for uniqueness + determinism.
	h := sha256.Sum256([]byte(title))
	slug := fmt.Sprintf("%x", h[:6]) // 12 hex chars
	return filepath.Join(maclawDataDir, "tasks", slug)
}

func buildSedimentTitle(req string) string {
	t := strings.TrimLeftFunc(req, func(r rune) bool {
		return r == '#' || r == '*' || r == '-' || r == ' '
	})
	runes := []rune(t)
	if len(runes) > 50 {
		cut := 50
		for i := cut; i > 30; i-- {
			if runes[i] == ' ' || runes[i] == ',' || runes[i] == '.' ||
				runes[i] == '\u3002' || runes[i] == '\uff0c' {
				cut = i
				break
			}
		}
		t = string(runes[:cut])
	}
	return strings.TrimSpace(t)
}

func truncSediment(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max]) + "..."
}
