package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Rollout Persistence for CodingSubAgent
//
// Codex-inspired improvement: each task execution is persisted as an
// append-only JSONL file. If the process crashes mid-task, the rollout
// records what was already done — avoiding blind retries on modified files.
//
// Design principles:
// - Append-only (no seeks, no rewrites) — crash-safe
// - Only metadata (paths, hashes, status) — not full file content
// - Rollout checked on task start: crash recovery injects modified-files context
// - Completed rollouts cleaned up after 24h
// ---------------------------------------------------------------------------

// SubAgentRollout manages append-only persistence of coding task execution.
type SubAgentRollout struct {
	mu     sync.Mutex
	file   *os.File
	taskID string
	seqNum uint64
	closed bool
}

// RolloutEntry is a single record in the rollout JSONL file.
type RolloutEntry struct {
	Seq       uint64 `json:"seq"`
	Timestamp string `json:"ts"`
	Type      string `json:"type"`                // "tool_call" | "tool_result" | "status" | "compaction"
	Tool      string `json:"tool,omitempty"`      // tool name
	ArgsHash  string `json:"args_hash,omitempty"` // SHA256 prefix (not full args)
	FilePath  string `json:"file,omitempty"`      // write/edit target path
	Succeeded bool   `json:"ok,omitempty"`
	Summary   string `json:"summary,omitempty"` // truncated result (max 300 runes)
	Status    string `json:"status,omitempty"`  // "running" | "completed" | "failed"
}

// RolloutRecoveryContext holds state recovered from a crashed rollout.
type RolloutRecoveryContext struct {
	FilesModified []string
	FilesCreated  []string
	LastCommands  []string
	Iterations    int
}

// NewSubAgentRollout creates or opens a rollout file for a task.
func NewSubAgentRollout(dir, taskID string) (*SubAgentRollout, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create rollout dir: %w", err)
	}
	path := filepath.Join(dir, taskID+".jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("open rollout file: %w", err)
	}
	r := &SubAgentRollout{file: f, taskID: taskID}
	r.appendEntry(RolloutEntry{Type: "status", Status: "running"})
	return r, nil
}

// AppendToolCall records a tool invocation.
func (r *SubAgentRollout) AppendToolCall(toolName, argsJSON, filePath string) {
	if r == nil {
		return
	}
	r.appendEntry(RolloutEntry{
		Type:     "tool_call",
		Tool:     toolName,
		ArgsHash: shortHash(argsJSON),
		FilePath: filePath,
	})
}

// AppendToolResult records a tool result.
func (r *SubAgentRollout) AppendToolResult(toolName string, succeeded bool, summary string) {
	if r == nil {
		return
	}
	r.appendEntry(RolloutEntry{
		Type:      "tool_result",
		Tool:      toolName,
		Succeeded: succeeded,
		Summary:   truncateRunesForRollout(summary, 300),
	})
}

// AppendCompaction records that a mid-task compaction occurred.
func (r *SubAgentRollout) AppendCompaction() {
	if r == nil {
		return
	}
	r.appendEntry(RolloutEntry{Type: "compaction"})
}

// Complete marks the rollout as successfully completed.
func (r *SubAgentRollout) Complete() {
	if r == nil {
		return
	}
	r.appendEntry(RolloutEntry{Type: "status", Status: "completed"})
	r.Close()
}

// Fail marks the rollout as failed.
func (r *SubAgentRollout) Fail(errMsg string) {
	if r == nil {
		return
	}
	r.appendEntry(RolloutEntry{Type: "status", Status: "failed", Summary: truncateRunesForRollout(errMsg, 200)})
	r.Close()
}

// Close closes the rollout file.
func (r *SubAgentRollout) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.closed && r.file != nil {
		r.file.Close()
		r.closed = true
	}
}

func (r *SubAgentRollout) appendEntry(entry RolloutEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.file == nil {
		return
	}
	r.seqNum++
	entry.Seq = r.seqNum
	entry.Timestamp = time.Now().Format(time.RFC3339)
	data, _ := json.Marshal(entry)
	data = append(data, '\n')
	r.file.Write(data)
}

// LoadRolloutRecovery checks if a crashed rollout exists for a task and
// builds recovery context from it. Returns nil if no recovery needed.
func LoadRolloutRecovery(dir, taskID string) *RolloutRecoveryContext {
	path := filepath.Join(dir, taskID+".jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil // no rollout file
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 0 {
		return nil
	}

	// Check if rollout has a terminal status
	var lastStatus string
	modified := make(map[string]bool)
	created := make(map[string]bool)
	var commands []string
	iterations := 0

	for _, line := range lines {
		var entry RolloutEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		switch entry.Type {
		case "status":
			lastStatus = entry.Status
		case "tool_call":
			iterations++
			if entry.FilePath != "" {
				if entry.Tool == "write_file" {
					created[entry.FilePath] = true
				} else if entry.Tool == "edit_file" || entry.Tool == "edit_lines" {
					modified[entry.FilePath] = true
				}
			}
		case "tool_result":
			if entry.Tool == "bash" && entry.Summary != "" {
				commands = append(commands, entry.Summary)
			}
		}
	}

	// Only recover if status is "running" (crashed) — not completed/failed
	if lastStatus != "running" {
		return nil
	}

	ctx := &RolloutRecoveryContext{Iterations: iterations}
	for f := range modified {
		ctx.FilesModified = append(ctx.FilesModified, f)
	}
	for f := range created {
		ctx.FilesCreated = append(ctx.FilesCreated, f)
	}
	if len(commands) > 5 {
		commands = commands[len(commands)-5:]
	}
	ctx.LastCommands = commands

	log.Printf("[coding-subagent-rollout] recovered crashed task %s: %d modified, %d created, %d iterations",
		taskID, len(ctx.FilesModified), len(ctx.FilesCreated), iterations)
	return ctx
}

// BuildRecoveryPromptSection formats recovery context for injection into system prompt.
func (r *RolloutRecoveryContext) BuildRecoveryPromptSection() string {
	if r == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n## Crash Recovery\n")
	b.WriteString("上一次执行此任务时进程中断。以下是已完成的工作：\n")

	if len(r.FilesModified) > 0 {
		b.WriteString("已修改文件：")
		b.WriteString(strings.Join(r.FilesModified, ", "))
		b.WriteString("\n")
	}
	if len(r.FilesCreated) > 0 {
		b.WriteString("已创建文件：")
		b.WriteString(strings.Join(r.FilesCreated, ", "))
		b.WriteString("\n")
	}
	if len(r.LastCommands) > 0 {
		b.WriteString("最近执行的命令：\n")
		for _, cmd := range r.LastCommands {
			b.WriteString(fmt.Sprintf("  - %s\n", cmd))
		}
	}
	b.WriteString(fmt.Sprintf("中断点：约第 %d 轮迭代\n", r.Iterations))
	b.WriteString("\n请检查这些文件的当前状态，确认上次修改的正确性，然后继续完成任务。\n")
	return b.String()
}

// CleanOldRollouts removes completed/failed rollouts older than maxAge.
func CleanOldRollouts(dir string, maxAge time.Duration) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-maxAge)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
}

func shortHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h[:4])
}

func truncateRunesForRollout(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes])
}
