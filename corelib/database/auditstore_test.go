package database

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func readAuditLines(t *testing.T, path string) []map[string]interface{} {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open audit file: %v", err)
	}
	defer f.Close()
	var lines []map[string]interface{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		text := strings.TrimSpace(sc.Text())
		if text == "" {
			continue
		}
		var record map[string]interface{}
		if err := json.Unmarshal([]byte(text), &record); err != nil {
			t.Fatalf("line %q is not valid JSON: %v", text, err)
		}
		lines = append(lines, record)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan audit file: %v", err)
	}
	return lines
}

func TestFileAuditSinkWritesAndReadsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit", "database_audit.jsonl")
	sink, err := NewFileAuditSink(path)
	if err != nil {
		t.Fatalf("NewFileAuditSink: %v", err)
	}
	ts := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	sink(context.Background(), AuditEvent{
		Timestamp:      ts,
		OwnerID:        "owner-1",
		SessionID:      "sess-1",
		ProfileID:      "prof-1",
		ConnectionID:   "conn-1",
		Action:         "query",
		SQLFingerprint: "fp-abc",
		ParameterCount: 2,
		Risk:           "low",
		ResultClass:    "ok",
	})
	lines := readAuditLines(t, path)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	rec := lines[0]
	if rec["kind"] != "event" {
		t.Fatalf("kind=%v, want event", rec["kind"])
	}
	for key, want := range map[string]interface{}{
		"owner_id":        "owner-1",
		"session_id":      "sess-1",
		"profile_id":      "prof-1",
		"connection_id":   "conn-1",
		"action":          "query",
		"sql_fingerprint": "fp-abc",
		"result_class":    "ok",
	} {
		if rec[key] != want {
			t.Fatalf("%s=%v, want %v", key, rec[key], want)
		}
	}
	if rec["parameter_count"] != float64(2) {
		t.Fatalf("parameter_count=%v, want 2", rec["parameter_count"])
	}
}

func TestFileAuditSinkReceiptOnlyForOKWithReceiptID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database_audit.jsonl")
	sink, err := NewFileAuditSink(path)
	if err != nil {
		t.Fatalf("NewFileAuditSink: %v", err)
	}
	base := AuditEvent{
		Timestamp:    time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
		Action:       "execute",
		ResultClass:  "ok",
		ApprovalID:   "appr-1",
		ProfileID:    "prof-1",
		ConnectionID: "conn-1",
		AffectedRows: 3,
	}
	// ok but no receipt id: no receipt.
	sink(context.Background(), base)
	// receipt id but not ok: no receipt.
	withReceipt := base
	withReceipt.ReceiptID = "rcpt-1"
	withReceipt.ResultClass = "error"
	sink(context.Background(), withReceipt)
	// ok with receipt id: receipt expected.
	okMutation := base
	okMutation.ReceiptID = "rcpt-2"
	sink(context.Background(), okMutation)

	lines := readAuditLines(t, path)
	if len(lines) != 4 {
		t.Fatalf("expected 3 events + 1 receipt = 4 lines, got %d", len(lines))
	}
	var receipts []map[string]interface{}
	for _, rec := range lines {
		if rec["kind"] == "receipt" {
			receipts = append(receipts, rec)
		}
	}
	if len(receipts) != 1 {
		t.Fatalf("expected exactly 1 receipt, got %d", len(receipts))
	}
	receipt := receipts[0]
	if receipt["receipt_id"] != "rcpt-2" {
		t.Fatalf("receipt_id=%v, want rcpt-2", receipt["receipt_id"])
	}
	if receipt["approval_id"] != "appr-1" {
		t.Fatalf("approval_id=%v, want appr-1", receipt["approval_id"])
	}
	if receipt["affected_rows"] != float64(3) {
		t.Fatalf("affected_rows=%v, want 3", receipt["affected_rows"])
	}
	if receipt["timestamp"] != "2025-06-01T00:00:00Z" {
		t.Fatalf("timestamp=%v, want 2025-06-01T00:00:00Z", receipt["timestamp"])
	}
}

func TestFileAuditSinkCreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deep", "audit.jsonl")
	sink, err := NewFileAuditSink(path)
	if err != nil {
		t.Fatalf("NewFileAuditSink: %v", err)
	}
	sink(context.Background(), AuditEvent{Action: "query", ResultClass: "ok"})
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("audit file not created: %v", err)
	}
	info, err := os.Stat(filepath.Dir(path))
	if err != nil || !info.IsDir() {
		t.Fatalf("parent dir missing: %v", err)
	}
}

func TestFileAuditSinkConcurrentWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database_audit.jsonl")
	sink, err := NewFileAuditSink(path)
	if err != nil {
		t.Fatalf("NewFileAuditSink: %v", err)
	}
	const writers = 100
	var wg sync.WaitGroup
	wg.Add(writers)
	for i := 0; i < writers; i++ {
		go func() {
			defer wg.Done()
			sink(context.Background(), AuditEvent{
				Timestamp: time.Now().UTC(),
				Action:    "query",
				Risk:      "low",
			})
		}()
	}
	wg.Wait()
	lines := readAuditLines(t, path)
	if len(lines) != writers {
		t.Fatalf("expected %d complete JSON lines, got %d", writers, len(lines))
	}
}

func TestFileAuditSinkContainsNoSensitiveFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database_audit.jsonl")
	sink, err := NewFileAuditSink(path)
	if err != nil {
		t.Fatalf("NewFileAuditSink: %v", err)
	}
	sink(context.Background(), AuditEvent{
		Timestamp:      time.Now().UTC(),
		Action:         "execute",
		SQLFingerprint: "fp-only",
		ParameterCount: 1,
		ResultClass:    "ok",
		ReceiptID:      "rcpt-9",
	})
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit file: %v", err)
	}
	for _, banned := range []string{"token", "secret", "password", "sql_text", "parameters\"", "param_values"} {
		if strings.Contains(strings.ToLower(string(raw)), banned) {
			t.Fatalf("audit file contains banned marker %q: %s", banned, raw)
		}
	}
}
