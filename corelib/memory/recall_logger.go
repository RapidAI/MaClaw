package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// recallLogger is the dedicated logger for memory recall diagnostics.
// It writes to a separate file (<MaclawBaseDir>/logs/memory_recall.log)
// to avoid polluting the main maclaw.log.
//
// Thread safety: all writes go through logRecallEntryLocked which holds
// recallLogMu for the entire write operation (rotation check + multi-line
// output). This guarantees each log entry is atomic—no interleaving between
// concurrent recall operations.
var (
	recallLogEnabled atomic.Bool
	recallLogMu      sync.Mutex
	recallLogFile    *os.File
	recallLogBytes   int64 // protected by recallLogMu, not atomic
)

const recallLogMaxSize = 5 * 1024 * 1024 // 5 MB

// SetMemoryRecallLogEnabled enables or disables memory recall logging.
// When enabled, recall details are written to a dedicated log file.
func SetMemoryRecallLogEnabled(enabled bool) {
	recallLogEnabled.Store(enabled)
	if enabled {
		recallLogMu.Lock()
		if recallLogFile == nil {
			openRecallLogFileLocked()
		}
		recallLogMu.Unlock()
	}
}

// IsMemoryRecallLogEnabled reports whether memory recall logging is active.
func IsMemoryRecallLogEnabled() bool {
	return recallLogEnabled.Load()
}

// openRecallLogFileLocked opens (or reopens after rotation) the log file.
// Caller must hold recallLogMu.
func openRecallLogFileLocked() {
	dir := corelib.MaclawLogsDir()
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	logPath := filepath.Join(dir, "memory_recall.log")

	// Rotate if existing log exceeds max size.
	if info, err := os.Stat(logPath); err == nil && info.Size() > recallLogMaxSize {
		prev := logPath + ".1"
		_ = os.Remove(prev)
		_ = os.Rename(logPath, prev)
	}

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	recallLogFile = f
	recallLogBytes = 0
}

// CloseRecallLog closes the recall log file. Call on shutdown.
func CloseRecallLog() {
	recallLogMu.Lock()
	defer recallLogMu.Unlock()
	if recallLogFile != nil {
		_ = recallLogFile.Close()
		recallLogFile = nil
	}
}

// --- Recall Log Entry Types ---

// RecallLogEntry represents a single recall operation logged to file.
type RecallLogEntry struct {
	Timestamp      string              `json:"timestamp"`
	Operation      string              `json:"operation"` // "dynamic", "dynamic_tool", "dynamic_strict", "project"
	Query          string              `json:"query"`
	Category       string              `json:"category,omitempty"`
	ProjectPath    string              `json:"project_path,omitempty"`
	OwnerID        string              `json:"owner_id,omitempty"`
	ElapsedMs      int64               `json:"elapsed_ms"`
	TotalEntries   int                 `json:"total_entries"` // total active entries in store
	ResultCount    int                 `json:"result_count"`
	Results        []RecallLogResult   `json:"results,omitempty"`
	QueryExpansion *RecallLogExpansion `json:"query_expansion,omitempty"`
}

// RecallLogResult represents a single recalled entry in the log.
type RecallLogResult struct {
	ID       string   `json:"id"`
	Category string   `json:"category"`
	Title    string   `json:"title,omitempty"`
	Content  string   `json:"content"` // truncated to 200 chars
	Tags     []string `json:"tags,omitempty"`
}

// RecallLogExpansion represents the query expansion details.
type RecallLogExpansion struct {
	Entities    []string `json:"entities,omitempty"`
	QueryTokens []string `json:"query_tokens,omitempty"`
}

// LogRecallOperation logs a recall operation to the dedicated recall log file.
// The totalEntries parameter is the total number of active entries in the memory
// store at the time of recall — provides context for interpreting result count.
func LogRecallOperation(op string, query string, category Category, projectPath string, ownerID string, elapsed time.Duration, results []Entry, totalEntries int, expanded *ExpandResult) {
	if !recallLogEnabled.Load() {
		return
	}

	entry := RecallLogEntry{
		Timestamp:    time.Now().Format("2006-01-02 15:04:05.000"),
		Operation:    op,
		Query:        truncateString(query, 500),
		Category:     string(category),
		ProjectPath:  projectPath,
		OwnerID:      ownerID,
		ElapsedMs:    elapsed.Milliseconds(),
		TotalEntries: totalEntries,
		ResultCount:  len(results),
	}

	// Add results with truncated content.
	if len(results) > 0 {
		entry.Results = make([]RecallLogResult, 0, len(results))
		for _, r := range results {
			entry.Results = append(entry.Results, RecallLogResult{
				ID:       r.ID,
				Category: string(r.Category),
				Title:    truncateString(r.Title, 100),
				Content:  truncateString(r.Content, 200),
				Tags:     r.Tags,
			})
		}
	}

	// Add query expansion info.
	if expanded != nil && (len(expanded.Entities) > 0 || len(expanded.QueryTokens) > 0) {
		entry.QueryExpansion = &RecallLogExpansion{
			Entities:    expanded.Entities,
			QueryTokens: expanded.QueryTokens,
		}
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return
	}

	// Build the complete log block as a single string to ensure atomicity.
	var buf strings.Builder
	fmt.Fprintf(&buf, "%s [memory_recall] op=%s query=%q results=%d/%d elapsed=%dms\n",
		entry.Timestamp, op, truncateString(query, 80), len(results), totalEntries, elapsed.Milliseconds())
	buf.Write(data)
	buf.WriteByte('\n')
	buf.WriteString(strings.Repeat("-", 80))
	buf.WriteByte('\n')

	logRecallEntryLocked(buf.String())
}

// logRecallEntryLocked writes a pre-formatted log block to the file under mutex.
// This ensures multi-line entries are never interleaved between goroutines.
func logRecallEntryLocked(block string) {
	recallLogMu.Lock()
	defer recallLogMu.Unlock()

	if recallLogFile == nil {
		openRecallLogFileLocked()
	}
	if recallLogFile == nil {
		return
	}

	// Rotate if accumulated writes exceed max size.
	if recallLogBytes >= recallLogMaxSize {
		_ = recallLogFile.Close()
		recallLogFile = nil
		openRecallLogFileLocked()
		if recallLogFile == nil {
			return
		}
	}

	n, _ := recallLogFile.WriteString(block)
	recallLogBytes += int64(n)
}

func truncateString(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}
