package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/security"
)

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
	auditMaxFileSize     = 50 * 1024 * 1024 // 50 MB
	auditRetentionDays   = 30
	auditNewestLineLimit = 1024 * 1024
)

var auditNewestReadChunk = 256 * 1024

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
func (l *AuditLog) Log(entry security.AuditEntry) error {
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}
	categories := security.RedactedAuditCategories(entry)
	entry = security.SanitizeAuditEntry(entry)
	if len(categories) > 0 {
		entry.SensitiveDetected = true
		entry.SensitiveCategories = categories
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
func (l *AuditLog) Query(filter security.AuditFilter) ([]security.AuditEntry, error) {
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

	var results []security.AuditEntry
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

// QueryNewestMatching returns the newest matching entries in chronological
// order, walking log files from newest date/seq toward oldest and stopping
// once limit matches are collected.
func (l *AuditLog) QueryNewestMatching(match func(security.AuditEntry) bool, limit int) ([]security.AuditEntry, error) {
	if l == nil || match == nil || limit <= 0 {
		return nil, nil
	}
	l.mu.Lock()
	if l.current != nil {
		_ = l.current.Sync()
	}
	l.mu.Unlock()

	files, err := l.logFilesNewestFirst()
	if err != nil {
		return nil, fmt.Errorf("audit log: list files: %w", err)
	}
	newest := make([]security.AuditEntry, 0, limit)
	for _, f := range files {
		entries, err := l.collectNewestFromFile(f, match, limit-len(newest))
		if len(entries) > 0 {
			newest = append(newest, entries...)
			if len(newest) >= limit {
				newest = newest[:limit]
				reverseAuditEntries(newest)
				return newest, nil
			}
		}
		if err != nil {
			continue
		}
	}
	reverseAuditEntries(newest)
	return newest, nil
}

func (l *AuditLog) logFilesNewestFirst() ([]string, error) {
	files, err := l.logFiles()
	if err != nil {
		return nil, err
	}
	sort.SliceStable(files, func(i, j int) bool {
		left, right := filepath.Base(files[i]), filepath.Base(files[j])
		leftDate, rightDate := extractDateFromFilename(left), extractDateFromFilename(right)
		if leftDate != rightDate {
			return leftDate > rightDate
		}
		return auditLogFileSeq(left) > auditLogFileSeq(right)
	})
	return files, nil
}

func auditLogFileSeq(name string) int {
	name = strings.TrimPrefix(name, "audit-")
	name = strings.TrimSuffix(name, ".jsonl")
	if len(name) <= 10 {
		return 0
	}
	rest := strings.TrimPrefix(name[10:], ".")
	n := 0
	for _, r := range rest {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func reverseAuditEntries(entries []security.AuditEntry) {
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
}

func (l *AuditLog) collectNewestFromFile(path string, match func(security.AuditEntry) bool, need int) ([]security.AuditEntry, error) {
	if need <= 0 {
		return nil, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	chunk := auditNewestReadChunk
	if chunk <= 0 || info.Size() <= int64(chunk) {
		entries, err := l.readFile(path)
		if err != nil {
			return nil, err
		}
		hits := make([]security.AuditEntry, 0, need)
		for i := len(entries) - 1; i >= 0 && len(hits) < need; i-- {
			if match(entries[i]) {
				hits = append(hits, entries[i])
			}
		}
		return hits, nil
	}
	return l.collectNewestFromFileTail(path, info.Size(), match, need)
}

func (l *AuditLog) collectNewestFromFileTail(path string, size int64, match func(security.AuditEntry) bool, need int) ([]security.AuditEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	hits := make([]security.AuditEntry, 0, need)
	leftover := []byte(nil)
	offset := size
	buf := make([]byte, auditNewestReadChunk)
	for offset > 0 && len(hits) < need {
		readSize := int64(auditNewestReadChunk)
		if offset < readSize {
			readSize = offset
		}
		readAt := offset - readSize
		n, err := f.ReadAt(buf[:readSize], readAt)
		if n == 0 {
			if err == io.EOF {
				break
			}
			if err != nil {
				return hits, err
			}
			break
		}
		offset = readAt
		chunk := append([]byte{}, buf[:n]...)
		if n == int(readSize) {
			if len(leftover) > 0 {
				chunk = append(chunk, leftover...)
			}
		} else {
			hits = appendAuditMatch(hits, leftover, match, need)
		}
		lines := bytes.Split(chunk, []byte{'\n'})
		if offset > 0 {
			leftover = lines[0]
			if len(leftover) > auditNewestLineLimit {
				leftover = nil
			}
			lines = lines[1:]
		} else {
			leftover = nil
		}
		for i := len(lines) - 1; i >= 0 && len(hits) < need; i-- {
			hits = appendAuditMatch(hits, lines[i], match, need)
		}
	}
	return hits, nil
}

func appendAuditMatch(hits []security.AuditEntry, line []byte, match func(security.AuditEntry) bool, need int) []security.AuditEntry {
	if len(hits) >= need {
		return hits
	}
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return hits
	}
	var entry security.AuditEntry
	if json.Unmarshal(line, &entry) != nil {
		return hits
	}
	if match(entry) {
		return append(hits, entry)
	}
	return hits
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
func (l *AuditLog) fileInRange(path string, filter security.AuditFilter) bool {
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
func (l *AuditLog) readFile(path string) ([]security.AuditEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []security.AuditEntry
	scanner := bufio.NewScanner(f)
	// Increase buffer for potentially large JSON lines.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry security.AuditEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue // skip malformed lines
		}
		entries = append(entries, entry)
	}
	return entries, scanner.Err()
}

// matchesFilter checks whether an entry matches the given filter criteria.
func matchesFilter(entry security.AuditEntry, filter security.AuditFilter) bool {
	if filter.StartTime != nil && entry.Timestamp.Before(*filter.StartTime) {
		return false
	}
	if filter.EndTime != nil && entry.Timestamp.After(*filter.EndTime) {
		return false
	}
	if filter.Action != "" && entry.Action != filter.Action {
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
