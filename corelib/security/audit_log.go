package security

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

// AuditLog manages audit log files with date-based splitting, size-based
// rotation, and 30-day retention.
type AuditLog struct {
	mu      sync.Mutex
	dir     string
	current *os.File
	curDate string
	curSize int64
}

const (
	auditMaxFileSize   = 50 * 1024 * 1024 // 50 MB
	auditRetentionDays = 30
)

// NewAuditLog creates an AuditLog that writes to the given directory.
func NewAuditLog(dir string) (*AuditLog, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("audit log: create dir: %w", err)
	}
	return &AuditLog{dir: dir}, nil
}

// Log writes an audit entry as a single JSON line.
func (l *AuditLog) Log(entry AuditEntry) error {
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}
	categories := RedactedAuditCategories(entry)
	entry = SanitizeAuditEntry(entry)
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

// Query returns audit entries matching the given filter.
func (l *AuditLog) Query(filter AuditFilter) ([]AuditEntry, error) {
	l.mu.Lock()
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
		if !l.fileInRange(f, filter) {
			continue
		}
		entries, err := l.readFile(f)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if matchesFilter(e, filter) {
				results = append(results, e)
			}
		}
	}

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

func (l *AuditLog) rotateLocked(dateStr string) error {
	if l.current != nil {
		_ = l.current.Close()
		l.current = nil
	}

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
	_ = l.cleanOldLogsLocked()
	return nil
}

func (l *AuditLog) filePathForDate(dateStr string, seq int) string {
	if seq == 0 {
		return filepath.Join(l.dir, fmt.Sprintf("audit-%s.jsonl", dateStr))
	}
	return filepath.Join(l.dir, fmt.Sprintf("audit-%s.%d.jsonl", dateStr, seq))
}

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

func (l *AuditLog) fileInRange(path string, filter AuditFilter) bool {
	dateStr := extractDateFromFilename(filepath.Base(path))
	if dateStr == "" {
		return true
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

func extractDateFromFilename(name string) string {
	name = strings.TrimPrefix(name, "audit-")
	if len(name) >= 10 {
		return name[:10]
	}
	return ""
}

func (l *AuditLog) readFile(path string) ([]AuditEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []AuditEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry AuditEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, scanner.Err()
}

func matchesFilter(entry AuditEntry, filter AuditFilter) bool {
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
