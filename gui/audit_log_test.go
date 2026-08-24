package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/security"
)

func TestAuditLog_LogAndQuery(t *testing.T) {
	dir := t.TempDir()
	al, err := NewAuditLog(dir)
	if err != nil {
		t.Fatalf("NewAuditLog: %v", err)
	}
	defer al.Close()

	now := time.Now()
	entries := []security.AuditEntry{
		{
			Timestamp:    now.Add(-2 * time.Hour),
			UserID:       "user1",
			SessionID:    "sess1",
			ToolName:     "Bash",
			Arguments:    map[string]interface{}{"command": "ls -la"},
			RiskLevel:    security.RiskLow,
			PolicyAction: security.PolicyAllow,
			Result:       "success",
		},
		{
			Timestamp:    now.Add(-1 * time.Hour),
			UserID:       "user1",
			SessionID:    "sess1",
			ToolName:     "Write",
			Arguments:    map[string]interface{}{"path": "/tmp/test.txt"},
			RiskLevel:    security.RiskMedium,
			PolicyAction: security.PolicyAudit,
			Result:       "success",
		},
		{
			Timestamp:    now,
			UserID:       "user2",
			SessionID:    "sess2",
			ToolName:     "Bash",
			Arguments:    map[string]interface{}{"command": "rm -rf /"},
			RiskLevel:    security.RiskCritical,
			PolicyAction: security.PolicyDeny,
			Result:       "denied",
		},
	}

	for _, e := range entries {
		if err := al.Log(e); err != nil {
			t.Fatalf("Log: %v", err)
		}
	}

	// Query all entries.
	all, err := al.Query(security.AuditFilter{})
	if err != nil {
		t.Fatalf("Query all: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("expected 3 entries, got %d", len(all))
	}

	// Query by tool name.
	bashOnly, err := al.Query(security.AuditFilter{ToolName: "Bash"})
	if err != nil {
		t.Fatalf("Query Bash: %v", err)
	}
	if len(bashOnly) != 2 {
		t.Errorf("expected 2 Bash entries, got %d", len(bashOnly))
	}

	// Query by risk level.
	critOnly, err := al.Query(security.AuditFilter{RiskLevels: []security.RiskLevel{security.RiskCritical}})
	if err != nil {
		t.Fatalf("Query critical: %v", err)
	}
	if len(critOnly) != 1 {
		t.Errorf("expected 1 critical entry, got %d", len(critOnly))
	}

	// Query by time range.
	start := now.Add(-90 * time.Minute)
	end := now.Add(-30 * time.Minute)
	ranged, err := al.Query(security.AuditFilter{StartTime: &start, EndTime: &end})
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

	al.Log(security.AuditEntry{Timestamp: day1, ToolName: "Bash", RiskLevel: security.RiskLow, PolicyAction: security.PolicyAllow})
	al.Log(security.AuditEntry{Timestamp: day2, ToolName: "Write", RiskLevel: security.RiskMedium, PolicyAction: security.PolicyAudit})

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
	al.Log(security.AuditEntry{Timestamp: ts, ToolName: "Bash", RiskLevel: security.RiskLow, PolicyAction: security.PolicyAllow})

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
	al.Log(security.AuditEntry{ToolName: "Bash", RiskLevel: security.RiskLow, PolicyAction: security.PolicyAllow})
	after := time.Now()

	results, err := al.Query(security.AuditFilter{})
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

func TestAuditLog_ActionFieldAndFilter(t *testing.T) {
	dir := t.TempDir()
	al, err := NewAuditLog(dir)
	if err != nil {
		t.Fatalf("NewAuditLog: %v", err)
	}
	defer al.Close()

	now := time.Now()
	entries := []security.AuditEntry{
		{
			Timestamp:    now.Add(-2 * time.Minute),
			Action:       security.AuditActionHubSkillInstall,
			ToolName:     "hub_skill_install",
			RiskLevel:    security.RiskLow,
			PolicyAction: security.PolicyAllow,
			Result:       "installed and executed skill deploy-helper from https://hub.example.com, trust_level=official, risk=low: success",
		},
		{
			Timestamp:    now.Add(-1 * time.Minute),
			Action:       security.AuditActionHubSkillReject,
			ToolName:     "hub_skill_install",
			RiskLevel:    security.RiskCritical,
			PolicyAction: security.PolicyDeny,
			Result:       "rejected skill dangerous-tool from https://hub.example.com: critical risk, trust_level=unknown",
		},
		{
			Timestamp:    now,
			ToolName:     "Bash",
			RiskLevel:    security.RiskLow,
			PolicyAction: security.PolicyAllow,
			Result:       "success",
		},
	}

	for _, e := range entries {
		if err := al.Log(e); err != nil {
			t.Fatalf("Log: %v", err)
		}
	}

	// Query by Action — should return only install entries.
	installOnly, err := al.Query(security.AuditFilter{Action: security.AuditActionHubSkillInstall})
	if err != nil {
		t.Fatalf("Query install: %v", err)
	}
	if len(installOnly) != 1 {
		t.Errorf("expected 1 install entry, got %d", len(installOnly))
	}
	if len(installOnly) > 0 && installOnly[0].Action != security.AuditActionHubSkillInstall {
		t.Errorf("expected action %s, got %s", security.AuditActionHubSkillInstall, installOnly[0].Action)
	}

	// Query by Action — should return only reject entries.
	rejectOnly, err := al.Query(security.AuditFilter{Action: security.AuditActionHubSkillReject})
	if err != nil {
		t.Fatalf("Query reject: %v", err)
	}
	if len(rejectOnly) != 1 {
		t.Errorf("expected 1 reject entry, got %d", len(rejectOnly))
	}

	// Query without Action filter — should return all 3.
	all, err := al.Query(security.AuditFilter{})
	if err != nil {
		t.Fatalf("Query all: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("expected 3 entries, got %d", len(all))
	}

	// Verify the Action field round-trips through JSON serialization.
	if len(installOnly) > 0 {
		entry := installOnly[0]
		if entry.Action != security.AuditActionHubSkillInstall {
			t.Errorf("expected action %q, got %q", security.AuditActionHubSkillInstall, entry.Action)
		}
		if entry.RiskLevel != security.RiskLow {
			t.Errorf("expected risk level %q, got %q", security.RiskLow, entry.RiskLevel)
		}
	}
}

func TestAuditLog_RedactsSensitiveArgumentsBeforePersisting(t *testing.T) {
	dir := t.TempDir()
	al, err := NewAuditLog(dir)
	if err != nil {
		t.Fatalf("NewAuditLog: %v", err)
	}
	defer al.Close()

	entry := security.AuditEntry{
		ToolName: "ssh",
		Arguments: map[string]interface{}{
			"password": "pw-value",
			"command":  "docker run -e JWT_SECRET='jwt-value' -e API_KEY_SECRET=api-value --token token-value --api-key=flag-api-value image",
			"headers":  []string{"Authorization: Bearer bearer-value", "Cookie: session=cookie-value"},
			"nested": map[string]interface{}{
				"storage_encryption_key": "encryption-value",
			},
		},
		RiskLevel:    security.RiskLow,
		PolicyAction: security.PolicyAllow,
		Result:       "used API_KEY_SECRET=api-value with Authorization: Bearer result-token",
	}
	if err := al.Log(entry); err != nil {
		t.Fatalf("Log: %v", err)
	}

	files, err := al.logFiles()
	if err != nil || len(files) != 1 {
		t.Fatalf("logFiles = %v, %v", files, err)
	}
	raw, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	text := string(raw)
	for _, secret := range []string{"pw-value", "jwt-value", "api-value", "token-value", "flag-api-value", "bearer-value", "cookie-value", "encryption-value", "result-token"} {
		if strings.Contains(text, secret) {
			t.Fatalf("audit log leaked %q: %s", secret, text)
		}
	}
	if !strings.Contains(text, "[REDACTED]") || !strings.Contains(text, "sensitive_detected") {
		t.Fatalf("audit log did not record redaction metadata: %s", text)
	}
}

func TestAuditLog_QueryNewestMatchingStopsAtLimit(t *testing.T) {
	dir := t.TempDir()
	al, err := NewAuditLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = al.Close() })
	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		if err := al.Log(security.AuditEntry{
			Timestamp: now.Add(time.Duration(i) * time.Minute), UserID: "user-1", ToolName: "bash",
			RiskLevel: security.RiskLow, PolicyAction: security.PolicyAllow, Result: "ok",
		}); err != nil {
			t.Fatal(err)
		}
		if err := al.Log(security.AuditEntry{
			Timestamp: now.Add(time.Duration(i)*time.Minute + time.Second), UserID: "user-2", ToolName: "secret_tool",
			RiskLevel: security.RiskHigh, PolicyAction: security.PolicyDeny, Result: "denied",
		}); err != nil {
			t.Fatal(err)
		}
	}

	got, err := al.QueryNewestMatching(func(entry security.AuditEntry) bool {
		return entry.UserID == "user-1"
	}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("newest matching=%#v", got)
	}
	if got[0].Timestamp.After(got[1].Timestamp) {
		t.Fatalf("expected chronological newest-N: %#v", got)
	}
	if got[0].UserID != "user-1" || got[1].UserID != "user-1" {
		t.Fatalf("foreign entry leaked: %#v", got)
	}
	if got[1].Timestamp.Unix() != now.Add(4*time.Minute).Unix() {
		t.Fatalf("expected the two newest user-1 events, last=%v want=%v", got[1].Timestamp, now.Add(4*time.Minute))
	}
}

func TestAuditLog_QueryNewestMatchingReadsTailWithoutLoadingWholeFile(t *testing.T) {
	dir := t.TempDir()
	al, err := NewAuditLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = al.Close() })
	oldChunk := auditNewestReadChunk
	auditNewestReadChunk = 80
	t.Cleanup(func() { auditNewestReadChunk = oldChunk })

	now := time.Now().UTC()
	for i := 0; i < 8; i++ {
		if err := al.Log(security.AuditEntry{
			Timestamp: now.Add(time.Duration(i) * time.Minute), UserID: "user-1", ToolName: "bash",
			RiskLevel: security.RiskLow, PolicyAction: security.PolicyAllow, Result: "ok",
		}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := al.QueryNewestMatching(func(entry security.AuditEntry) bool {
		return entry.UserID == "user-1"
	}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Timestamp.Unix() != now.Add(6*time.Minute).Unix() || got[1].Timestamp.Unix() != now.Add(7*time.Minute).Unix() {
		t.Fatalf("tail newest=%#v", got)
	}

	all, err := al.QueryNewestMatching(func(entry security.AuditEntry) bool {
		return entry.UserID == "user-1"
	}, 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 8 || all[0].Timestamp.Unix() != now.Unix() || all[7].Timestamp.Unix() != now.Add(7*time.Minute).Unix() {
		t.Fatalf("tail must reconstruct the first file line, got=%#v", all)
	}
}

func TestAuditLog_QueryNewestMatchingZeroChunkFallsBack(t *testing.T) {
	dir := t.TempDir()
	al, err := NewAuditLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = al.Close() })
	oldChunk := auditNewestReadChunk
	auditNewestReadChunk = 0
	t.Cleanup(func() { auditNewestReadChunk = oldChunk })
	now := time.Now().UTC()
	if err := al.Log(security.AuditEntry{
		Timestamp: now, UserID: "user-1", ToolName: "bash",
		RiskLevel: security.RiskLow, PolicyAction: security.PolicyAllow, Result: "ok",
	}); err != nil {
		t.Fatal(err)
	}
	got, err := al.QueryNewestMatching(func(entry security.AuditEntry) bool {
		return entry.UserID == "user-1"
	}, 1)
	if err != nil || len(got) != 1 || got[0].UserID != "user-1" {
		t.Fatalf("zero chunk fallback=%#v err=%v", got, err)
	}
}

func TestAuditAction_Constants(t *testing.T) {
	// Verify the security.AuditAction constants have the expected string values.
	if security.AuditActionHubSkillInstall != "hub_skill_install" {
		t.Errorf("expected %q, got %q", "hub_skill_install", security.AuditActionHubSkillInstall)
	}
	if security.AuditActionHubSkillUpdate != "hub_skill_update" {
		t.Errorf("expected %q, got %q", "hub_skill_update", security.AuditActionHubSkillUpdate)
	}
	if security.AuditActionHubSkillReject != "hub_skill_reject" {
		t.Errorf("expected %q, got %q", "hub_skill_reject", security.AuditActionHubSkillReject)
	}
}
