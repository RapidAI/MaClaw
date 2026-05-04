package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIMAuditThirdPartyPlatformFilters(t *testing.T) {
	store, err := NewIMAuditStore(filepath.Join(t.TempDir(), "im_audit.db"))
	if err != nil {
		t.Fatalf("NewIMAuditStore: %v", err)
	}
	defer store.Close()

	now := time.Now().Format(time.RFC3339)
	rows := []IMAuditMessage{
		{Timestamp: now, UserID: "client-a:room-1", Platform: "thirdparty:client-a", Role: "user", Content: "hello from a"},
		{Timestamp: now, UserID: "client-b:room-2", Platform: "thirdparty:client-b", Role: "assistant", Content: "reply from b"},
		{Timestamp: now, UserID: "qq-user", Platform: "qq", Role: "user", Content: "qq message"},
	}
	for _, row := range rows {
		if _, err := store.db.Exec(
			`INSERT INTO im_audit_messages (timestamp, user_id, platform, role, content) VALUES (?, ?, ?, ?, ?)`,
			row.Timestamp, row.UserID, row.Platform, row.Role, row.Content,
		); err != nil {
			t.Fatalf("insert audit row: %v", err)
		}
	}

	result, err := store.Query("thirdparty", "", "", 1)
	if err != nil {
		t.Fatalf("Query thirdparty: %v", err)
	}
	if result.Total != 2 || len(result.Messages) != 2 {
		t.Fatalf("thirdparty query returned total=%d len=%d, want 2", result.Total, len(result.Messages))
	}
	for _, msg := range result.Messages {
		if !strings.HasPrefix(msg.Platform, "thirdparty:") {
			t.Fatalf("query included non-thirdparty platform %q", msg.Platform)
		}
	}

	users, err := store.ListUsers("thirdparty")
	if err != nil {
		t.Fatalf("ListUsers thirdparty: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("thirdparty users = %#v, want 2", users)
	}

	stats, err := store.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.ThirdParty != 2 || stats.QQ != 1 || stats.Total != 3 {
		t.Fatalf("unexpected stats: %#v", stats)
	}

	exportPath, err := store.ExportCSV("thirdparty", "", "", t.TempDir())
	if err != nil {
		t.Fatalf("ExportCSV thirdparty: %v", err)
	}
	data, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "时间,用户ID,平台,角色,内容") {
		t.Fatalf("export header is not localized correctly: %q", text)
	}
	if strings.Contains(text, "qq message") || !strings.Contains(text, "hello from a") || !strings.Contains(text, "reply from b") {
		t.Fatalf("export did not filter thirdparty rows correctly: %q", text)
	}
}
