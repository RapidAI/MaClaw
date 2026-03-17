package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAuditLog_LogAndQuery(t *testing.T) {
	dir := t.TempDir()
	al, err := NewAuditLog(dir)
	if err != nil {
		t.Fatalf("NewAuditLog: %v", err)
	}
	defer al.Close()

	now := time.Now()
	entries := []AuditEntry{
		{
			Timestamp:    now.Add(-2 * time.Hour),
			UserID:       "user1",
			SessionID:    "sess1",
			ToolName:     "Bash",
			Arguments:    map[string]interface{}{"command": "ls -la"},
			RiskLevel:    RiskLow,
			PolicyAction: PolicyAllow,
			Result:       "success",
		},
		{
			Timestamp:    now.Add(-1 * time.Hour),
			UserID:       "user1",
			SessionID:    "sess1",
			ToolName:     "Write",
			Arguments:    map[string]interface{}{"path": "/tmp/test.txt"},
			RiskLevel:    RiskMedium,
			PolicyAction: PolicyAudit,
			Result:       "success",
		},
		{
			Timestamp:    now,
			UserID:       "user2",
			SessionID:    "sess2",
			ToolName:     "Bash",
			Arguments:    map[string]interface{}{"command": "rm -rf /"},
			RiskLevel:    RiskCritical,
			PolicyAction: PolicyDeny,
			Result:       "denied",
		},
	}

	for _, e := range entries {
		if err := al.Log(e); err != nil {
			t.Fatalf("Log: %v", err)
		}
	}

	// Query all entries.
	all, err := al.Query(AuditFilter{})
	if err != nil {
		t.Fatalf("Query all: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("expected 3 entries, got %d", len(all))
	}

	// Query by tool name.
	bashOnly, err := al.Query(AuditFilter{ToolName: "Bash"})
	if err != nil {
		t.Fatalf("Query Bash: %v", err)
	}
	if len(bashOnly) != 2 {
		t.Errorf("expected 2 Bash entries, got %d", len(bashOnly))
	}

	// Query by risk level.
	critOnly, err := al.Query(AuditFilter{RiskLevels: []RiskLevel{RiskCritical}})
	if err != nil {
		t.Fatalf("Query critical: %v", err)
	}
	if len(critOnly) != 1 {
		t.Errorf("expected 1 critical entry, got %d", len(critOnly))
	}

	// Query by time range.
	start := now.Add(-90 * time.Minute)
	end := now.Add(-30 * time.Minute)
	ranged, err := al.Query(AuditFilter{StartTime: &start, EndTime: &end})
	if err != nil {
		t.Fatalf("Query range: %v", err)
	}
	if len(ranged) != 1 {
		t.Errorf("expected 1 entry in range, got %d", len(ranged))
	}
	if len(ranged) > 0 && ranged[0].ToolName != "Write" {
		t.Errorf("expected Write entry, got %s", ranged[0].ToolName)
	}
}

func TestAuditLog_DateSplitting(t *testing.T) {
	dir := t.TempDir()
	al, err := NewAuditLog(dir)
	if err != nil {
		t.Fatalf("NewAuditLog: %v", err)
	}
	defer al.Close()

	// Log entries on different dates (use recent dates to avoid cleanup).
	now := time.Now()
	day1 := time.Date(now.Year(), now.Month(), now.Day(), 10, 0, 0, 0, time.UTC)
	day2 := day1.AddDate(0, 0, -1)

	al.Log(AuditEntry{Timestamp: day1, ToolName: "Bash", RiskLevel: RiskLow, PolicyAction: PolicyAllow})
	al.Log(AuditEntry{Timestamp: day2, ToolName: "Write", RiskLevel: RiskMedium, PolicyAction: PolicyAudit})

	// Verify two separate files were created.
	files, err := al.logFiles()
	if err != nil {
		t.Fatalf("logFiles: %v", err)
	}
	if len(files) != 2 {
		t.Errorf("expected 2 log files, got %d", len(files))
	}

	// Verify file names contain the correct dates.
	day1Str := day1.Format("2006-01-02")
	day2Str := day2.Format("2006-01-02")
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = filepath.Base(f)
	}
	if len(names) >= 2 {
		expected1 := "audit-" + day2Str + ".jsonl"
		expected2 := "audit-" + day1Str + ".jsonl"
		if names[0] != expected1 {
			t.Errorf("expected %s, got %s", expected1, names[0])
		}
		if names[1] != expected2 {
			t.Errorf("expected %s, got %s", expected2, names[1])
		}
	}
}

func TestAuditLog_SizeRotation(t *testing.T) {
	dir := t.TempDir()
	al, err := NewAuditLog(dir)
	if err != nil {
		t.Fatalf("NewAuditLog: %v", err)
	}
	defer al.Close()

	// Use today's date to avoid cleanup removing the file.
	ts := time.Now()
	dateStr := ts.Format("2006-01-02")

	// Create a file that's already near the 50MB limit.
	path := filepath.Join(dir, "audit-"+dateStr+".jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Write just under 50MB of padding.
	padding := make([]byte, auditMaxFileSize)
	for i := range padding {
		padding[i] = 'x'
	}
	f.Write(padding)
	f.Close()

	// Now log an entry — it should go to a rotated file.
	al.Log(AuditEntry{Timestamp: ts, ToolName: "Bash", RiskLevel: RiskLow, PolicyAction: PolicyAllow})

	files, err := al.logFiles()
	if err != nil {
		t.Fatalf("logFiles: %v", err)
	}
	if len(files) < 2 {
		t.Errorf("expected at least 2 files after rotation, got %d", len(files))
	}
}

func TestAuditLog_CleanOldLogs(t *testing.T) {
	dir := t.TempDir()
	al, err := NewAuditLog(dir)
	if err != nil {
		t.Fatalf("NewAuditLog: %v", err)
	}
	defer al.Close()

	// Create a log file that's 31 days old.
	oldDate := time.Now().AddDate(0, 0, -31).Format("2006-01-02")
	oldPath := filepath.Join(dir, "audit-"+oldDate+".jsonl")
	os.WriteFile(oldPath, []byte(`{"tool_name":"old"}`+"\n"), 0o644)

	// Create a recent log file.
	recentDate := time.Now().Format("2006-01-02")
	recentPath := filepath.Join(dir, "audit-"+recentDate+".jsonl")
	os.WriteFile(recentPath, []byte(`{"tool_name":"recent"}`+"\n"), 0o644)

	err = al.CleanOldLogs()
	if err != nil {
		t.Fatalf("CleanOldLogs: %v", err)
	}

	// Old file should be removed.
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("expected old log file to be removed")
	}

	// Recent file should still exist.
	if _, err := os.Stat(recentPath); err != nil {
		t.Errorf("expected recent log file to exist: %v", err)
	}
}

func TestAuditLog_DefaultTimestamp(t *testing.T) {
	dir := t.TempDir()
	al, err := NewAuditLog(dir)
	if err != nil {
		t.Fatalf("NewAuditLog: %v", err)
	}
	defer al.Close()

	// Log an entry without setting Timestamp — it should default to now.
	before := time.Now()
	al.Log(AuditEntry{ToolName: "Bash", RiskLevel: RiskLow, PolicyAction: PolicyAllow})
	after := time.Now()

	results, err := al.Query(AuditFilter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(results))
	}
	ts := results[0].Timestamp
	if ts.Before(before) || ts.After(after) {
		t.Errorf("expected timestamp between %v and %v, got %v", before, after, ts)
	}
}
