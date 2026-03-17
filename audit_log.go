package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// AuditEntry represents a single audit log record for a tool invocation.
type AuditEntry struct {
	Timestamp    time.Time              `json:"timestamp"`
	UserID       string                 `json:"user_id"`
	SessionID    string                 `json:"session_id"`
	ToolName     string                 `json:"tool_name"`
	Arguments    map[string]interface{} `json:"arguments"`
	RiskLevel    RiskLevel              `json:"risk_level"`
	PolicyAction PolicyAction           `json:"policy_action"`
	Result       string                 `json:"result"`
}

// AuditFilter defines criteria for querying audit log entries.
type AuditFilter struct {
	StartTime  *time.Time
	EndTime    *time.Time
	ToolName   string
	RiskLevels []RiskLevel
}

// AuditLog manages audit log files with date-based splitting, size-based
// rotation, and 30-day retention.
type AuditLog struct {
	mu      sync.Mutex
	dir     string   // log directory
	current *os.File // currently open log file
	curDate string   // date string of the current file (YYYY-MM-DD)
	curSize int64    // approximate bytes written to the current file
}

const (
	auditMaxFileSize   = 50 * 1024 * 1024 // 50 MB
	auditRetentionDays = 30
)

// NewAuditLog creates an AuditLog that writes to the given directory.
// The directory is created if it does not exist.
func NewAuditLog(dir string) (*AuditLog, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("audit log: create dir: %w", err)
	}
	return &AuditLog{dir: dir}, nil
}

// Log writes an audit entry as a single JSON line to the current log file.
// It handles date-based file splitting and size-based rotation automatically.
func (l *AuditLog) Log(entry AuditEntry) error {
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("audit log: marshal: %w", err)
	}
	line := append(data, '\n')

	l.mu.Lock()
	defer l.mu.Unlock()

	dateStr := entry.Timestamp.Format("2006-01-02")

	// Rotate if the date changed or the file exceeds the size limit.
	if l.current == nil || l.curDate != dateStr || l.curSize+int64(len(line)) > auditMaxFileSize {
		if err := l.rotateLocked(dateStr); err != nil {
			return err
		}
	}

	n, err := l.current.Write(line)
	if err != nil {
		return fmt.Errorf("audit log: write: %w", err)
	}
	l.curSize += int64(n)

	return nil
}

// Query returns audit entries matching the given filter. It scans all
// relevant log files based on the time range.
func (l *AuditLog) Query(filter AuditFilter) ([]AuditEntry, error) {
	l.mu.Lock()
	// Flush the current file so queries see the latest data.
	if l.current != nil {
		_ = l.current.Sync()
	}
	l.mu.Unlock()

	files, err := l.logFiles()
	if err != nil {
		return nil, fmt.Errorf("audit log: list files: %w", err)
	}

	var results []AuditEntry
	for _, f := range files {
		// Quick date-range check based on filename.
		if !l.fileInRange(f, filter) {
			continue
		}

		entries, err := l.readFile(f)
		if err != nil {
			continue // skip corrupt files
		}
		for _, e := range entries {
			if matchesFilter(e, filter) {
				results = append(results, e)
			}
		}
	}

	// Sort by timestamp ascending.
	sort.Slice(results, func(i, j int) bool {
		return results[i].Timestamp.Before(results[j].Timestamp)
	})

	return results, nil
}

// CleanOldLogs removes log files older than 30 days.
func (l *AuditLog) CleanOldLogs() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.cleanOldLogsLocked()
}

// Close closes the current log file.
func (l *AuditLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.current != nil {
		err := l.current.Close()
		l.current = nil
		return err
	}
	return nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// rotateLocked closes the current file and opens a new one for the given date.
// Must be called with l.mu held.
func (l *AuditLog) rotateLocked(dateStr string) error {
	if l.current != nil {
		_ = l.current.Close()
		l.current = nil
	}

	// Find a filename that doesn't exceed the size limit.
	path := l.filePathForDate(dateStr, 0)
	seq := 0
	for {
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			break
		}
		if err != nil {
			return fmt.Errorf("audit log: stat: %w", err)
		}
		if info.Size() < auditMaxFileSize {
			break
		}
		seq++
		path = l.filePathForDate(dateStr, seq)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("audit log: open: %w", err)
	}

	info, err := f.Stat()
	if err != nil {
		f.Close()
		return fmt.Errorf("audit log: stat new file: %w", err)
	}

	l.current = f
	l.curDate = dateStr
	l.curSize = info.Size()

	// Clean old logs on rotation.
	_ = l.cleanOldLogsLocked()

	return nil
}

// filePathForDate returns the log file path for a given date and sequence number.
// Sequence 0 produces "audit-2024-01-15.jsonl", sequence 1 produces
// "audit-2024-01-15.1.jsonl", etc.
func (l *AuditLog) filePathForDate(dateStr string, seq int) string {
	if seq == 0 {
		return filepath.Join(l.dir, fmt.Sprintf("audit-%s.jsonl", dateStr))
	}
	return filepath.Join(l.dir, fmt.Sprintf("audit-%s.%d.jsonl", dateStr, seq))
}

// logFiles returns all audit log files in the directory, sorted by name.
func (l *AuditLog) logFiles() ([]string, error) {
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, "audit-") && strings.Contains(name, ".jsonl") {
			files = append(files, filepath.Join(l.dir, name))
		}
	}
	sort.Strings(files)
	return files, nil
}

// fileInRange checks whether a log file's date falls within the filter's time range.
func (l *AuditLog) fileInRange(path string, filter AuditFilter) bool {
	dateStr := extractDateFromFilename(filepath.Base(path))
	if dateStr == "" {
		return true // can't determine, include it
	}
	fileDate, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return true
	}

	if filter.StartTime != nil {
		startDate := time.Date(filter.StartTime.Year(), filter.StartTime.Month(), filter.StartTime.Day(), 0, 0, 0, 0, time.UTC)
		if fileDate.Before(startDate) {
			return false
		}
	}
	if filter.EndTime != nil {
		endDate := time.Date(filter.EndTime.Year(), filter.EndTime.Month(), filter.EndTime.Day(), 23, 59, 59, 0, time.UTC)
		if fileDate.After(endDate) {
			return false
		}
	}
	return true
}

// extractDateFromFilename extracts the date portion from a filename like
// "audit-2024-01-15.jsonl" or "audit-2024-01-15.1.jsonl".
func extractDateFromFilename(name string) string {
	// Remove "audit-" prefix.
	name = strings.TrimPrefix(name, "audit-")
	// The date is the first 10 characters (YYYY-MM-DD).
	if len(name) >= 10 {
		return name[:10]
	}
	return ""
}

// readFile reads all audit entries from a single JSONL file.
func (l *AuditLog) readFile(path string) ([]AuditEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []AuditEntry
	scanner := bufio.NewScanner(f)
	// Increase buffer for potentially large JSON lines.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry AuditEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue // skip malformed lines
		}
		entries = append(entries, entry)
	}
	return entries, scanner.Err()
}

// matchesFilter checks whether an entry matches the given filter criteria.
func matchesFilter(entry AuditEntry, filter AuditFilter) bool {
	if filter.StartTime != nil && entry.Timestamp.Before(*filter.StartTime) {
		return false
	}
	if filter.EndTime != nil && entry.Timestamp.After(*filter.EndTime) {
		return false
	}
	if filter.ToolName != "" && entry.ToolName != filter.ToolName {
		return false
	}
	if len(filter.RiskLevels) > 0 {
		found := false
		for _, rl := range filter.RiskLevels {
			if strings.EqualFold(string(rl), string(entry.RiskLevel)) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// cleanOldLogsLocked removes log files older than auditRetentionDays.
// Must be called with l.mu held.
func (l *AuditLog) cleanOldLogsLocked() error {
	cutoff := time.Now().AddDate(0, 0, -auditRetentionDays)
	cutoffDate := cutoff.Format("2006-01-02")

	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, "audit-") || !strings.Contains(name, ".jsonl") {
			continue
		}
		dateStr := extractDateFromFilename(name)
		if dateStr == "" {
			continue
		}
		if dateStr < cutoffDate {
			_ = os.Remove(filepath.Join(l.dir, name))
		}
	}
	return nil
}
