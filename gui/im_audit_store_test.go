package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
		{Timestamp: now, UserID: "hub-user", Platform: "thirdparty", Role: "user", Content: "hello through hub"},
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
	if result.Total != 3 || len(result.Messages) != 3 {
		t.Fatalf("thirdparty query returned total=%d len=%d, want 3", result.Total, len(result.Messages))
	}
	for _, msg := range result.Messages {
		if msg.Platform != "thirdparty" && !strings.HasPrefix(msg.Platform, "thirdparty:") {
			t.Fatalf("query included non-thirdparty platform %q", msg.Platform)
		}
	}

	users, err := store.ListUsers("thirdparty")
	if err != nil {
		t.Fatalf("ListUsers thirdparty: %v", err)
	}
	if len(users) != 3 {
		t.Fatalf("thirdparty users = %#v, want 3", users)
	}

	stats, err := store.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.ThirdParty != 3 || stats.QQ != 1 || stats.Total != 4 {
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
	if strings.Contains(text, "qq message") || !strings.Contains(text, "hello from a") || !strings.Contains(text, "reply from b") || !strings.Contains(text, "hello through hub") {
		t.Fatalf("export did not filter thirdparty rows correctly: %q", text)
	}
	if !strings.Contains(text, "附件名称") || !strings.Contains(text, "本地路径") {
		t.Fatalf("export omitted attachment columns: %q", text)
	}
}

func TestNormalizeIMAuditPlatformPreservesThirdPartyProvenance(t *testing.T) {
	tests := map[string]string{
		"thirdparty":          "thirdparty",
		"thirdparty:pet-01":   "thirdparty:pet-01",
		" ThirdParty:PET-02 ": "thirdparty:pet-02",
		"weixin_local":        "weixin",
		" CustomTransport ":   "CustomTransport",
	}
	for input, want := range tests {
		if got := normalizeIMAuditPlatform(input); got != want {
			t.Errorf("normalizeIMAuditPlatform(%q)=%q, want %q", input, got, want)
		}
	}
}

func TestThirdPartyAuditFilterUsesSQLiteIndexablePrefix(t *testing.T) {
	where, args := buildIMAuditWhere("thirdparty", "", "")
	if !strings.Contains(where, "platform GLOB ?") {
		t.Fatalf("third-party filter=%q, want indexable GLOB prefix", where)
	}
	if len(args) != 2 || args[1] != "thirdparty:*" {
		t.Fatalf("third-party args=%#v", args)
	}
}

func TestIMAuditStoreConfiguresSQLiteForSerializedReliableWrites(t *testing.T) {
	store, err := NewIMAuditStore(filepath.Join(t.TempDir(), "im_audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if got := store.db.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("max open connections=%d, want 1", got)
	}
	var busyTimeout int
	if err := store.db.QueryRow(`PRAGMA busy_timeout`).Scan(&busyTimeout); err != nil {
		t.Fatal(err)
	}
	if busyTimeout != 5000 {
		t.Fatalf("busy timeout=%d, want 5000", busyTimeout)
	}
}

func TestIMAuditStoreWriteThenQueryThirdParty(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "im_audit.db")
	store, err := NewIMAuditStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if !store.WriteCritical(IMAuditMessage{
		UserID: "thirdparty:esp32-pet:default", Platform: "ThirdParty:ESP32-PET", Role: "user", Content: "real write path",
	}) {
		t.Fatal("critical audit write was not accepted")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := NewIMAuditStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	result, err := reopened.Query("thirdparty", "", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || len(result.Messages) != 1 {
		t.Fatalf("query result=%#v, want one persisted third-party row", result)
	}
	if got := result.Messages[0].Platform; got != "thirdparty:esp32-pet" {
		t.Fatalf("stored platform=%q, want client provenance", got)
	}
}

func TestIMAuditQueryFlushesAcceptedWrites(t *testing.T) {
	store, err := NewIMAuditStore(filepath.Join(t.TempDir(), "im_audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for i := 0; i < 100; i++ {
		store.Write(IMAuditMessage{
			UserID: "thirdparty:pet:default", Platform: "thirdparty:pet", Role: "user", Content: fmt.Sprintf("message-%d", i),
		})
	}
	result, err := store.Query("thirdparty", "", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 100 {
		t.Fatalf("total=%d immediately after writes, want 100", result.Total)
	}
}

func TestIMAuditBatchFailureStillReleasesFlushWaiters(t *testing.T) {
	for _, test := range []struct {
		name    string
		closeDB bool
	}{
		{name: "begin failure", closeDB: true},
		{name: "prepare failure"},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "im_audit.db"))
			if err != nil {
				t.Fatal(err)
			}
			if test.closeDB {
				if err := db.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				defer db.Close()
			}

			store := &IMAuditStore{db: db, accepted: 2}
			store.stateCond = sync.NewCond(&store.stateMu)
			store.processBatch([]IMAuditMessage{{Content: "one"}, {Content: "two"}})

			done := make(chan struct{})
			go func() {
				store.flush()
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("flush remained blocked after the failed batch was consumed")
			}
			if store.processed != 2 {
				t.Fatalf("processed=%d, want 2", store.processed)
			}
		})
	}
}

func TestDeleteBeforeFlushesQueuedOldMessages(t *testing.T) {
	store, err := NewIMAuditStore(filepath.Join(t.TempDir(), "im_audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	old := time.Now().AddDate(0, 0, -60).Format(time.RFC3339)
	for i := 0; i < 100; i++ {
		store.Write(IMAuditMessage{Timestamp: old, UserID: "old", Platform: "thirdparty:esp32", Role: "user", Content: fmt.Sprintf("old-%d", i)})
	}
	deleted, _, err := store.DeleteBeforeWithAttachmentPaths(30)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 100 {
		t.Fatalf("deleted=%d, want 100 queued old messages", deleted)
	}
	result, err := store.Query("thirdparty", "", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 0 {
		t.Fatalf("old messages reappeared after cleanup: %#v", result.Messages)
	}
}

func TestIMAuditStoreMigratesAndQueriesAttachmentColumns(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "im_audit.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE im_audit_messages (id INTEGER PRIMARY KEY AUTOINCREMENT, timestamp DATETIME NOT NULL, user_id TEXT NOT NULL, platform TEXT NOT NULL, role TEXT NOT NULL, content TEXT NOT NULL)`)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	store, err := NewIMAuditStore(dbPath)
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	defer store.Close()
	_, err = store.db.Exec(`INSERT INTO im_audit_messages (timestamp,user_id,platform,role,content,attachment_path,attachment_name,attachment_media_type,attachment_size) VALUES (?,?,?,?,?,?,?,?,?)`,
		time.Now().Format(time.RFC3339), "g:u", "lansenger", "user", "file", `C:\audit\report.pdf`, "report.pdf", "file", 1234)
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.Query("lansenger", "", "", 1)
	if err != nil || len(result.Messages) != 1 {
		t.Fatalf("query=%#v err=%v", result, err)
	}
	got := result.Messages[0]
	if got.AttachmentName != "report.pdf" || got.AttachmentSize != 1234 || got.AttachmentPath == "" {
		t.Fatalf("attachment=%#v", got)
	}
}

func TestValidateIMAuditAttachmentPathRejectsEscape(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	root := app.imAuditAttachmentRoot()
	inside := filepath.Join(root, "lansenger", "g", "file.txt")
	if err := os.MkdirAll(filepath.Dir(inside), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inside, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := app.validateIMAuditAttachmentPath(inside); err != nil {
		t.Fatalf("inside rejected: %v", err)
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("no"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := app.validateIMAuditAttachmentPath(outside); err == nil {
		t.Fatal("outside path accepted")
	}
}

func TestCleanupOrphanIMAuditAttachmentsRetainsReferencedFile(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	store, err := NewIMAuditStore(filepath.Join(t.TempDir(), "im_audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	referenced, err := app.saveIMAuditAttachment("lansenger", "g1", "m1", "kept.txt", []byte("kept"))
	if err != nil {
		t.Fatal(err)
	}
	orphan, err := app.saveIMAuditAttachment("lansenger", "g1", "m2", "orphan.txt", []byte("orphan"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`INSERT INTO im_audit_messages (timestamp,user_id,platform,role,content,attachment_path,attachment_name) VALUES (?,?,?,?,?,?,?)`,
		time.Now().Format(time.RFC3339), "g1:u1", "lansenger", "user", "file", referenced, "kept.txt"); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-imAuditOrphanGracePeriod - time.Minute)
	if err := os.Chtimes(orphan, old, old); err != nil {
		t.Fatal(err)
	}

	app.cleanupOrphanIMAuditAttachments(store)
	if _, err := os.Stat(referenced); err != nil {
		t.Fatalf("referenced attachment removed: %v", err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("orphan attachment still exists: %v", err)
	}
}

func TestCleanupOrphanIMAuditAttachmentsDoesNotRaceNewFile(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	store, err := NewIMAuditStore(filepath.Join(t.TempDir(), "im_audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	recent, err := app.saveIMAuditAttachment("lansenger", "g1", "m1", "pending.txt", []byte("pending"))
	if err != nil {
		t.Fatal(err)
	}
	app.cleanupOrphanIMAuditAttachments(store)
	if _, err := os.Stat(recent); err != nil {
		t.Fatalf("recent attachment removed before its row could persist: %v", err)
	}
}

func TestSafeIMAuditCSVCellNeutralizesFormulas(t *testing.T) {
	for _, input := range []string{"=cmd|' /C calc'!A0", "+SUM(1,2)", " -1+2", "\t@evil"} {
		if got := safeIMAuditCSVCell(input); !strings.HasPrefix(got, "'") {
			t.Errorf("safeIMAuditCSVCell(%q) = %q, want apostrophe prefix", input, got)
		}
	}
	if got := safeIMAuditCSVCell("ordinary text"); got != "ordinary text" {
		t.Fatalf("ordinary text changed to %q", got)
	}
}

func TestIMAuditStoreCloseIsSafeWithConcurrentWriters(t *testing.T) {
	store, err := NewIMAuditStore(filepath.Join(t.TempDir(), "im_audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	var writers sync.WaitGroup
	for i := 0; i < 16; i++ {
		writers.Add(1)
		go func() {
			defer writers.Done()
			for j := 0; j < 100; j++ {
				store.Write(IMAuditMessage{UserID: "u", Platform: "lansenger", Role: "user", Content: "message"})
			}
		}()
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	writers.Wait()
	if store.WriteCritical(IMAuditMessage{UserID: "late", Platform: "lansenger", Role: "user", Content: "late"}) {
		t.Fatal("write after close was accepted")
	}
}

func TestCreateIMAuditCSVFileStaysInOutputDirectoryAndAvoidsOverwrite(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	f1, path1, err := createIMAuditCSVFile(dir, `../../outside`, now)
	if err != nil {
		t.Fatal(err)
	}
	_ = f1.Close()
	f2, path2, err := createIMAuditCSVFile(dir, `../../outside`, now)
	if err != nil {
		t.Fatal(err)
	}
	_ = f2.Close()
	if filepath.Dir(path1) != dir || filepath.Dir(path2) != dir {
		t.Fatalf("export escaped output directory: %q %q", path1, path2)
	}
	if path1 == path2 {
		t.Fatal("second export overwrote the first")
	}
}

func TestDeleteBeforeRejectsNonPositiveRetention(t *testing.T) {
	store, err := NewIMAuditStore(filepath.Join(t.TempDir(), "im_audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, days := range []int{0, -1} {
		if _, _, err := store.DeleteBeforeWithAttachmentPaths(days); err == nil {
			t.Fatalf("days=%d was accepted", days)
		}
	}
}
