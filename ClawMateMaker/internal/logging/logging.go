// Package logging provides bounded, privacy-aware job diagnostics.
package logging

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

const SchemaVersion = 1
const maxDetailBytes = 4096
const maxRawLogBytes int64 = 5 * 1024 * 1024

type Severity string

const (
	Debug Severity = "debug"
	Info  Severity = "info"
	Warn  Severity = "warning"
	Error Severity = "error"
)

type Event struct {
	Timestamp   time.Time      `json:"timestamp"`
	MonotonicMs int64          `json:"monotonicMs"`
	Sequence    uint64         `json:"sequence"`
	JobID       string         `json:"jobId"`
	AttemptID   string         `json:"attemptId"`
	Severity    Severity       `json:"severity"`
	Stage       string         `json:"stage"`
	Component   string         `json:"component"`
	Code        string         `json:"code"`
	MessageKey  string         `json:"messageKey"`
	Detail      string         `json:"detail,omitempty"`
	Fields      map[string]any `json:"fields,omitempty"`
}

type Meta struct {
	SchemaVersion int               `json:"schemaVersion"`
	JobID         string            `json:"jobId"`
	CreatedAt     time.Time         `json:"createdAt"`
	Files         map[string]string `json:"files,omitempty"`
	Dropped       uint64            `json:"dropped"`
	Truncated     bool              `json:"truncated"`
}

// Snapshot is the compact, restart-safe view of a job. It is written after
// every persisted event, so a renderer that reloads while an operation is
// still active can recover the latest state before replaying events.jsonl.
// Detailed output deliberately remains in the separately paged log stream.
type Snapshot struct {
	SchemaVersion  int       `json:"schemaVersion"`
	JobID          string    `json:"jobId"`
	AttemptID      string    `json:"attemptId"`
	Status         string    `json:"status"`
	LatestSequence uint64    `json:"latestSequence"`
	LastEvent      *Event    `json:"lastEvent,omitempty"`
	ErrorCode      string    `json:"errorCode,omitempty"`
	ErrorMessage   string    `json:"errorMessage,omitempty"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type Writer struct {
	mu           sync.Mutex
	root         string
	jobID        string
	attemptID    string
	started      time.Time
	sequence     uint64
	file         *os.File
	emit         func(Event)
	closed       bool
	rawTruncated map[string]bool
	snapshot     Snapshot
}

func DefaultRoot() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "ClawMateMaker", "logs"), nil
}

func New(root, jobID, attemptID string, emit func(Event)) (*Writer, error) {
	if jobID == "" || attemptID == "" {
		return nil, errors.New("job and attempt IDs are required")
	}
	dir := filepath.Join(root, jobID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create job log directory: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(dir, "events.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("open event log: %w", err)
	}
	started := time.Now()
	w := &Writer{root: dir, jobID: jobID, attemptID: attemptID, started: started, file: f, emit: emit, rawTruncated: make(map[string]bool), snapshot: Snapshot{SchemaVersion: SchemaVersion, JobID: jobID, AttemptID: attemptID, Status: "running", UpdatedAt: started.UTC()}}
	if err := w.writeMeta(false); err != nil {
		_ = f.Close()
		return nil, err
	}
	if err := w.writeSnapshotLocked(); err != nil {
		_ = f.Close()
		return nil, err
	}
	return w, nil
}

func (w *Writer) Event(severity Severity, stage, component, code, messageKey, detail string, fields map[string]any) Event {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.sequence++
	e := Event{Timestamp: time.Now().UTC(), MonotonicMs: time.Since(w.started).Milliseconds(), Sequence: w.sequence, JobID: w.jobID, AttemptID: w.attemptID, Severity: severity, Stage: stage, Component: component, Code: code, MessageKey: messageKey, Detail: Redact(detail), Fields: SafeFields(fields)}
	if w.closed {
		return e
	}
	b, err := json.Marshal(e)
	if err == nil {
		_, err = w.file.Write(append(b, '\n'))
	}
	if err == nil && (severity == Warn || severity == Error || code == "STAGE_STARTED" || code == "STAGE_COMPLETED" || strings.HasPrefix(code, "JOB_")) {
		err = w.file.Sync()
	}
	if err == nil {
		w.snapshot.LatestSequence = e.Sequence
		copy := e
		w.snapshot.LastEvent = &copy
		w.snapshot.UpdatedAt = e.Timestamp
		err = w.writeSnapshotLocked()
	}
	if err == nil && w.emit != nil {
		w.emit(e)
	}
	return e
}

func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	err := w.file.Sync()
	closeErr := w.file.Close()
	if metaErr := w.writeMeta(true); metaErr != nil && err == nil {
		err = metaErr
	}
	if err != nil {
		return err
	}
	return closeErr
}

func (w *Writer) WriteSummary(summary any) error {
	b, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return err
	}
	// Results can include sidecar and serial-originated errors. Apply the same
	// redaction boundary as events before any result becomes persistent.
	var value any
	if err := json.Unmarshal(b, &value); err != nil {
		return err
	}
	clean, err := json.MarshalIndent(redactSummaryValue(value), "", "  ")
	if err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(w.root, "summary.json"), append(clean, '\n')); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	// Job results have a stable status/error subset. Preserve a fail-safe
	// running snapshot if a different caller writes auxiliary summary data.
	var terminal struct {
		Status       string `json:"status"`
		ErrorCode    string `json:"errorCode"`
		ErrorMessage string `json:"errorMessage"`
	}
	if json.Unmarshal(clean, &terminal) == nil && (terminal.Status == "succeeded" || terminal.Status == "failed") {
		w.snapshot.Status = terminal.Status
		w.snapshot.ErrorCode = Redact(terminal.ErrorCode)
		w.snapshot.ErrorMessage = Redact(terminal.ErrorMessage)
		w.snapshot.UpdatedAt = time.Now().UTC()
		return w.writeSnapshotLocked()
	}
	return nil
}

func (w *Writer) writeSnapshotLocked() error {
	b, err := json.MarshalIndent(w.snapshot, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(w.root, "snapshot.json"), append(b, '\n'))
}

func atomicWrite(path string, contents []byte) error {
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, contents, 0600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func redactSummaryValue(value any) any {
	switch typed := value.(type) {
	case string:
		return Redact(typed)
	case []any:
		for index := range typed {
			typed[index] = redactSummaryValue(typed[index])
		}
		return typed
	case map[string]any:
		for key, item := range typed {
			typed[key] = redactSummaryValue(item)
		}
		return typed
	default:
		return value
	}
}

// AppendRaw stores a bounded and redacted text stream. Allowed names are fixed,
// so callers cannot turn diagnostic collection into an arbitrary file-write API.
func (w *Writer) AppendRaw(name, text string) error {
	if name != "serial.log" && name != "sidecar.log" {
		return fmt.Errorf("unsupported raw log: %s", name)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || w.rawTruncated[name] {
		return nil
	}
	filePath := filepath.Join(w.root, name)
	current := int64(0)
	if info, err := os.Stat(filePath); err == nil {
		current = info.Size()
	}
	clean := []byte(Redact(text))
	if current+int64(len(clean)) > maxRawLogBytes {
		remaining := maxRawLogBytes - current
		if remaining > 0 {
			clean = clean[:remaining]
			if err := appendPrivate(filePath, clean); err != nil {
				return err
			}
		}
		w.rawTruncated[name] = true
		w.sequence++
		e := Event{Timestamp: time.Now().UTC(), MonotonicMs: time.Since(w.started).Milliseconds(), Sequence: w.sequence, JobID: w.jobID, AttemptID: w.attemptID, Severity: Warn, Stage: "logging", Component: "logging", Code: "LOG_TRUNCATED", MessageKey: "log.truncated", Fields: map[string]any{"bytes": maxRawLogBytes}}
		b, _ := json.Marshal(e)
		_, _ = w.file.Write(append(b, '\n'))
		_ = w.file.Sync()
		if w.emit != nil {
			w.emit(e)
		}
		return nil
	}
	return appendPrivate(filePath, clean)
}

func appendPrivate(name string, bytes []byte) error {
	f, err := os.OpenFile(name, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(bytes)
	return err
}

func (w *Writer) writeMeta(final bool) error {
	meta := Meta{SchemaVersion: SchemaVersion, JobID: w.jobID, CreatedAt: w.started.UTC(), Files: map[string]string{}, Truncated: len(w.rawTruncated) != 0}
	if final {
		for _, name := range []string{"events.jsonl", "summary.json", "serial.log", "sidecar.log"} {
			path := filepath.Join(w.root, name)
			if b, err := os.ReadFile(path); err == nil {
				h := sha256.Sum256(b)
				meta.Files[name] = "sha256:" + hex.EncodeToString(h[:])
			}
		}
	}
	b, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(w.root, "log-meta.json"), append(b, '\n'))
}

var sensitive = regexp.MustCompile(`(?i)(?:password|passwd|token|authorization|api[_-]?key|ssid)\s*[:=]\s*[^\s,;]+|https?://[^\s?]+\?[^\s]+|[0-9a-f]{2}(?::[0-9a-f]{2}){5}`)

func Redact(value string) string {
	value = strings.ToValidUTF8(value, "?")
	value = sensitive.ReplaceAllString(value, "[REDACTED]")
	value = strings.Map(func(r rune) rune {
		if r < 32 && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, value)
	if len(value) > maxDetailBytes {
		return value[:maxDetailBytes] + "…[truncated]"
	}
	return value
}

func SafeFields(fields map[string]any) map[string]any {
	if len(fields) == 0 {
		return nil
	}
	allowed := map[string]bool{"port": true, "chip": true, "revision": true, "flashBytes": true, "tool": true, "toolVersion": true, "exitCode": true, "durationMs": true, "command": true, "bytes": true, "attempt": true, "os": true, "vendorId": true, "productId": true, "boardId": true, "asset": true, "release": true, "sha256": true, "cached": true, "baud": true, "fromBaud": true, "toBaud": true}
	out := make(map[string]any)
	for k, v := range fields {
		if allowed[k] {
			out[k] = v
		}
	}
	return out
}

type Page struct {
	Events []Event `json:"events"`
	Next   uint64  `json:"next"`
}

// ReadSnapshot validates and returns one job snapshot without exposing an
// arbitrary local path to the Wails caller.
func ReadSnapshot(root, jobID string) (Snapshot, error) {
	if !SafeJobID(jobID) {
		return Snapshot{}, errors.New("invalid job ID")
	}
	path := filepath.Join(root, jobID, "snapshot.json")
	info, err := os.Stat(path)
	if err != nil {
		return Snapshot{}, err
	}
	if info.IsDir() || info.Size() <= 0 || info.Size() > 512*1024 {
		return Snapshot{}, errors.New("invalid job snapshot")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Snapshot{}, err
	}
	var snapshot Snapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return Snapshot{}, err
	}
	if snapshot.SchemaVersion != SchemaVersion || snapshot.JobID != jobID || snapshot.AttemptID == "" || (snapshot.Status != "running" && snapshot.Status != "succeeded" && snapshot.Status != "failed") {
		return Snapshot{}, errors.New("invalid job snapshot")
	}
	snapshot.ErrorMessage = Redact(snapshot.ErrorMessage)
	if snapshot.LastEvent != nil {
		snapshot.LastEvent.Detail = Redact(snapshot.LastEvent.Detail)
		snapshot.LastEvent.Fields = SafeFields(snapshot.LastEvent.Fields)
	}
	return snapshot, nil
}

// ReadRecentSnapshots includes both running and terminal jobs. It is used by
// the desktop startup sequence before the UI replays detailed event pages.
func ReadRecentSnapshots(root string, limit int) ([]Snapshot, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]Snapshot, 0, limit)
	for _, entry := range entries {
		if !entry.IsDir() || !SafeJobID(entry.Name()) {
			continue
		}
		if snapshot, readErr := ReadSnapshot(root, entry.Name()); readErr == nil {
			items = append(items, snapshot)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].UpdatedAt.After(items[j].UpdatedAt) })
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

// JobSummary is the safe, stable subset of a terminal task kept on disk. It
// lets a restarted WebView restore the diagnostics entry point without making
// arbitrary job directories or verbose result payloads part of the API.
type JobSummary struct {
	JobID        string    `json:"jobId"`
	Status       string    `json:"status,omitempty"`
	ErrorCode    string    `json:"errorCode,omitempty"`
	ErrorMessage string    `json:"errorMessage,omitempty"`
	StartedAt    time.Time `json:"startedAt,omitempty"`
	FinishedAt   time.Time `json:"finishedAt,omitempty"`
}

// SafeJobID accepts only the generated job identifier grammar. It is used by
// every API that turns a job ID into a local directory path.
func SafeJobID(jobID string) bool {
	if !strings.HasPrefix(jobID, "job-") || len(jobID) != len("job-")+16 {
		return false
	}
	for _, r := range jobID[len("job-"):] {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func ReadPage(dir string, after uint64, limit int) (Page, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	f, err := os.Open(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		return Page{}, err
	}
	defer f.Close()
	var page Page
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 4096), 128*1024)
	for s.Scan() {
		var e Event
		if json.Unmarshal(s.Bytes(), &e) != nil || e.Sequence <= after {
			continue
		}
		page.Events = append(page.Events, e)
		page.Next = e.Sequence
		if len(page.Events) == limit {
			break
		}
	}
	return page, s.Err()
}

// ReadRecentSummaries scans direct job folders only and returns terminal
// summaries newest-first. Malformed or unrelated files are ignored; they
// cannot break startup or turn untrusted JSON into task state.
func ReadRecentSummaries(root string, limit int) ([]JobSummary, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]JobSummary, 0, limit)
	for _, entry := range entries {
		if !entry.IsDir() || !SafeJobID(entry.Name()) {
			continue
		}
		path := filepath.Join(root, entry.Name(), "summary.json")
		info, err := os.Stat(path)
		if err != nil || info.IsDir() || info.Size() <= 0 || info.Size() > 512*1024 {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var summary JobSummary
		if json.Unmarshal(raw, &summary) != nil || summary.JobID != entry.Name() {
			continue
		}
		summary.ErrorMessage = Redact(summary.ErrorMessage)
		items = append(items, summary)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].FinishedAt.After(items[j].FinishedAt) })
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func EnvironmentFields() map[string]any {
	return map[string]any{"os": runtime.GOOS + "/" + runtime.GOARCH}
}

func SortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
