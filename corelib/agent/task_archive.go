package agent

// task_archive.go — Persistent storage for archived tasks.
//
// TaskArchive stores completed/abandoned tasks so they can be recalled later.
// It is a lightweight append-only store with a per-user cap, persisted to disk.

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// TaskArchive stores archived tasks per user with disk persistence.
type TaskArchive struct {
	mu       sync.RWMutex
	tasks    map[string][]ArchivedTask // userID -> tasks (newest first)
	maxTasks int
	path     string
	dirty    bool
}

// NewTaskArchive creates a TaskArchive. If path is non-empty, it loads
// existing data from disk.
func NewTaskArchive(path string, maxTasks int) *TaskArchive {
	if maxTasks <= 0 {
		maxTasks = 10
	}
	ta := &TaskArchive{
		tasks:    make(map[string][]ArchivedTask),
		maxTasks: maxTasks,
		path:     path,
	}
	if path != "" {
		ta.loadFromDisk()
	}
	return ta
}

// Archive stores a task snapshot. If the user already has maxTasks archived,
// the oldest is evicted.
func (ta *TaskArchive) Archive(task ArchivedTask) {
	if strings.TrimSpace(task.UserID) == "" || strings.TrimSpace(task.ID) == "" {
		return
	}
	if task.ArchivedAt.IsZero() {
		task.ArchivedAt = time.Now()
	}

	ta.mu.Lock()
	defer ta.mu.Unlock()

	tasks := ta.tasks[task.UserID]

	// Deduplicate: if a task with the same ID exists, update it.
	found := false
	for i, t := range tasks {
		if t.ID == task.ID {
			tasks[i] = task
			found = true
			break
		}
	}
	if !found {
		// Prepend (newest first).
		tasks = append([]ArchivedTask{task}, tasks...)
	}

	// Evict oldest if over capacity.
	if len(tasks) > ta.maxTasks {
		tasks = tasks[:ta.maxTasks]
	}

	ta.tasks[task.UserID] = tasks
	ta.dirty = true
	ta.flushLocked()
}

// List returns archived tasks for a user, newest first.
func (ta *TaskArchive) List(userID string) []ArchivedTask {
	ta.mu.RLock()
	defer ta.mu.RUnlock()
	tasks := ta.tasks[userID]
	result := make([]ArchivedTask, len(tasks))
	copy(result, tasks)
	return result
}

// Get returns a specific archived task by ID.
func (ta *TaskArchive) Get(userID, taskID string) (ArchivedTask, bool) {
	ta.mu.RLock()
	defer ta.mu.RUnlock()
	for _, t := range ta.tasks[userID] {
		if t.ID == taskID {
			return t, true
		}
	}
	return ArchivedTask{}, false
}

// Remove deletes an archived task.
func (ta *TaskArchive) Remove(userID, taskID string) {
	ta.mu.Lock()
	defer ta.mu.Unlock()
	tasks := ta.tasks[userID]
	for i, t := range tasks {
		if t.ID == taskID {
			ta.tasks[userID] = append(tasks[:i], tasks[i+1:]...)
			ta.dirty = true
			ta.flushLocked()
			return
		}
	}
}

// --- Persistence ---

type taskArchiveSnapshot struct {
	Tasks map[string][]ArchivedTask `json:"tasks"`
}

func (ta *TaskArchive) loadFromDisk() {
	if ta.path == "" {
		return
	}
	data, err := os.ReadFile(ta.path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[TaskArchive] failed to load from %s: %v", ta.path, err)
		}
		return
	}
	var snap taskArchiveSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		log.Printf("[TaskArchive] failed to parse %s: %v", ta.path, err)
		return
	}
	if snap.Tasks != nil {
		ta.tasks = snap.Tasks
		// Ensure sorted newest first.
		for uid, tasks := range ta.tasks {
			sort.SliceStable(tasks, func(i, j int) bool {
				return tasks[i].ArchivedAt.After(tasks[j].ArchivedAt)
			})
			// Enforce cap.
			if len(tasks) > ta.maxTasks {
				ta.tasks[uid] = tasks[:ta.maxTasks]
			}
		}
	}
	log.Printf("[TaskArchive] loaded %d users from disk", len(ta.tasks))
}

// flushLocked writes the current state to disk. Must be called with mu held.
// Uses synchronous I/O for simplicity since archive writes are infrequent
// (only on task switch). If this becomes a bottleneck, switch to the
// debounced async pattern used by ConversationMemory.
func (ta *TaskArchive) flushLocked() {
	if ta.path == "" || !ta.dirty {
		return
	}
	snap := taskArchiveSnapshot{Tasks: ta.tasks}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		log.Printf("[TaskArchive] failed to marshal: %v", err)
		return
	}
	dir := filepath.Dir(ta.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("[TaskArchive] failed to create dir %s: %v", dir, err)
		return
	}
	if err := os.WriteFile(ta.path, data, 0o644); err != nil {
		log.Printf("[TaskArchive] failed to write %s: %v", ta.path, err)
		return
	}
	ta.dirty = false
}

// --- Helper: build ArchivedTask from conversation ---

// BuildArchivedTask creates an ArchivedTask from the current conversation state.
func BuildArchivedTask(userID string, history []ConversationEntry, status string, projectPath string) ArchivedTask {
	taskID := fmt.Sprintf("task-%d", time.Now().UnixMilli())

	// Extract original user request.
	var lastRequest string
	for _, e := range history {
		if e.Role == "user" {
			if text, ok := e.Content.(string); ok && strings.TrimSpace(text) != "" {
				if len([]rune(text)) >= 4 { // skip very short messages
					lastRequest = TruncateRunes(strings.TrimSpace(text), 300)
					break
				}
			}
		}
	}

	// Build a one-line summary from the first user message.
	summary := TruncateRunes(lastRequest, 100)

	// Extract file paths mentioned in the conversation.
	filePaths := extractMentionedFilePaths(history)

	// Build compressed history: keep the first user message, last assistant
	// message, and a middle summary marker.
	compressed := buildCompressedHistory(history)

	return ArchivedTask{
		ID:                taskID,
		UserID:            userID,
		Summary:           summary,
		LastRequest:       lastRequest,
		FilePaths:         filePaths,
		ProjectPath:       projectPath,
		Status:            status,
		CreatedAt:         time.Now(),
		ArchivedAt:        time.Now(),
		CompressedHistory: compressed,
	}
}

// extractMentionedFilePaths scans conversation for file path patterns.
func extractMentionedFilePaths(history []ConversationEntry) []string {
	seen := make(map[string]bool)
	var paths []string
	for _, e := range history {
		text, ok := e.Content.(string)
		if !ok || text == "" {
			continue
		}
		// Look for common file path patterns.
		for _, word := range strings.Fields(text) {
			// Simple heuristic: contains path separator and has an extension.
			if (strings.Contains(word, "/") || strings.Contains(word, "\\")) &&
				strings.Contains(word, ".") &&
				len(word) > 3 && len(word) < 200 {
				// Clean up common surrounding characters.
				clean := strings.Trim(word, "\"'`()[]{},:;")
				if clean != "" && !seen[clean] {
					seen[clean] = true
					paths = append(paths, clean)
					if len(paths) >= 10 {
						return paths
					}
				}
			}
		}
	}
	return paths
}

// buildCompressedHistory creates a minimal conversation snapshot for recall.
func buildCompressedHistory(history []ConversationEntry) []ConversationEntry {
	if len(history) == 0 {
		return nil
	}

	var compressed []ConversationEntry

	// Keep the first user message (original task request).
	for _, e := range history {
		if e.Role == "user" {
			if text, ok := e.Content.(string); ok && strings.TrimSpace(text) != "" {
				compressed = append(compressed, ConversationEntry{
					Role:    "user",
					Content: TruncateRunes(strings.TrimSpace(text), 500),
				})
				break
			}
		}
	}

	// Keep the last assistant message (final result/status).
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == "assistant" {
			if text, ok := history[i].Content.(string); ok && strings.TrimSpace(text) != "" {
				compressed = append(compressed, ConversationEntry{
					Role:    "assistant",
					Content: TruncateRunes(strings.TrimSpace(text), 500),
				})
				break
			}
		}
	}

	return compressed
}
