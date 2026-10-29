package agent

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDetachedPetSessionStaysOutOfMainTranscript(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "ai_assistant_conversation.json")
	sidePath := filepath.Join(dir, "pet_companion_conversation.json")
	cm := NewPersistentConversationMemory(mainPath)
	defer cm.Stop()
	if err := cm.UseSeparateSessionFile("desktop-pet", sidePath); err != nil {
		t.Fatal(err)
	}
	cm.Append("desktop-user", ConversationEntry{Role: "user", Content: "主窗口的话"})
	cm.Append("desktop-pet", ConversationEntry{Role: "user", Content: "宠物的话"})
	if err := cm.FlushNow(); err != nil {
		t.Fatal(err)
	}
	assertSQLiteDetachedPath(t, mainPath, "desktop-user", "")
	assertSQLiteDetachedPath(t, mainPath, "desktop-pet", sidePath)
	if got := cm.LoadAll("desktop-user"); len(got) != 1 || got[0].Content != "主窗口的话" {
		t.Fatalf("main transcript = %#v", got)
	}
	if got := cm.LoadAll("desktop-pet"); len(got) != 1 || got[0].Content != "宠物的话" {
		t.Fatalf("pet transcript = %#v", got)
	}
}

func TestDetachedCheckpointDoesNotRewriteMainStore(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "ai_assistant_conversation.json")
	sidePath := filepath.Join(dir, "session-transcripts", "weather.json")
	cm := NewPersistentConversationMemory(mainPath)
	defer cm.Stop()

	cm.Append("desktop-user", ConversationEntry{Role: "user", Content: "主会话保持不动"})
	cm.Append("desktop-user:D:/tasks/weather", ConversationEntry{Role: "user", Content: "旧任务"})
	if err := cm.FlushNow(); err != nil {
		t.Fatal(err)
	}
	before := sqliteSessionUpdatedAt(t, mainPath, "desktop-user")
	if err := cm.UseSeparateSessionFile("desktop-user:D:/tasks/weather", sidePath); err != nil {
		t.Fatal(err)
	}
	if err := cm.PersistInFlightCheckpoint(
		"desktop-user:D:/tasks/weather",
		[]ConversationEntry{{Role: "user", Content: "工具检查点"}},
		"北京天气",
		"D:/tasks/weather",
		"run-1",
		InFlightCheckpoint{Sequence: 1, LastToolName: "send_file"},
	); err != nil {
		t.Fatal(err)
	}
	if after := sqliteSessionUpdatedAt(t, mainPath, "desktop-user"); after != before {
		t.Fatal("main session was rewritten by a detached checkpoint")
	}
	assertSQLiteDetachedPath(t, mainPath, "desktop-user:D:/tasks/weather", sidePath)
	got := cm.Load("desktop-user:D:/tasks/weather")
	if len(got) != 1 || got[0].Content != "工具检查点" {
		t.Fatalf("detached checkpoint = %#v", got)
	}

	cm.Stop()
	reloaded := NewPersistentConversationMemory(mainPath)
	defer reloaded.Stop()
	if got := reloaded.Load("desktop-user:D:/tasks/weather"); len(got) != 1 || got[0].Content != "工具检查点" {
		t.Fatalf("reloaded task session = %#v", got)
	}
	if main := reloaded.Load("desktop-user"); len(main) != 1 || main[0].Content != "主会话保持不动" {
		t.Fatalf("main session = %#v", main)
	}
}

func TestOlderDetachedFileDoesNotReplaceNewerMainSession(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "ai_assistant_conversation.json")
	sidePath := filepath.Join(dir, "session-transcripts", "weather.json")
	userID := "desktop-user:D:/tasks/weather"
	cm := NewPersistentConversationMemory(mainPath)
	cm.Append(userID, ConversationEntry{Role: "user", Content: "主库里的新内容"})
	if err := cm.FlushNow(); err != nil {
		t.Fatal(err)
	}
	cm.Stop()

	legacyMain := filepath.Join(dir, "legacy.json")
	writeConversationSnapshotForTest(t, legacyMain, memorySnapshot{SchemaVersion: memorySnapshotSchemaVersion, Sessions: map[string]persistedSession{
		userID: {Entries: []ConversationEntry{{Role: "user", Content: "主库里的新内容", ID: "new", Timestamp: 1}}, LastAccess: time.Now()},
	}})
	writeConversationSnapshotForTest(t, sidePath, memorySnapshot{Sessions: map[string]persistedSession{
		userID: {Entries: []ConversationEntry{{Role: "user", Content: "旁路旧检查点"}}, LastAccess: time.Now().Add(-2 * time.Hour)},
	}})
	index, err := json.Marshal(detachedSessionIndex{Sessions: map[string]string{userID: sidePath}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyMain+".detached.json", index, 0o644); err != nil {
		t.Fatal(err)
	}

	reloaded := NewPersistentConversationMemory(legacyMain)
	defer reloaded.Stop()
	got := reloaded.Load(userID)
	if len(got) != 1 || got[0].Content != "主库里的新内容" {
		t.Fatalf("older side file replaced the newer shared session: %#v", got)
	}
}

func TestUnrelatedFlushDoesNotRewriteDetachedSideFile(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "ai_assistant_conversation.json")
	sidePath := filepath.Join(dir, "session-transcripts", "weather.json")
	userID := "desktop-user:D:/tasks/weather"
	cm := NewPersistentConversationMemory(mainPath)
	defer cm.Stop()
	cm.Append("desktop-user", ConversationEntry{Role: "user", Content: "主会话"})
	cm.Append(userID, ConversationEntry{Role: "user", Content: "任务"})
	if err := cm.FlushNow(); err != nil {
		t.Fatal(err)
	}
	if err := cm.UseSeparateSessionFile(userID, sidePath); err != nil {
		t.Fatal(err)
	}
	cm.Save(userID, []ConversationEntry{{Role: "user", Content: "工具检查点"}})
	before := sqliteSessionUpdatedAt(t, mainPath, userID)
	cm.Append("desktop-user", ConversationEntry{Role: "assistant", Content: "另一会话"})
	if err := cm.FlushNow(); err != nil {
		t.Fatal(err)
	}
	if after := sqliteSessionUpdatedAt(t, mainPath, userID); after != before {
		t.Fatal("unrelated flush rewrote the detached session")
	}
	assertSQLiteDetachedPath(t, mainPath, userID, sidePath)
}

func TestDetachedBindingUsesOpaqueLegacyPath(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "ai_assistant_conversation.json")
	sidePath := filepath.Join(dir, "blocked-side")
	if err := os.MkdirAll(sidePath, 0o755); err != nil {
		t.Fatal(err)
	}
	userID := "desktop-user:D:/tasks/weather"
	cm := NewPersistentConversationMemory(mainPath)
	defer cm.Stop()
	cm.Append(userID, ConversationEntry{Role: "user", Content: "必须留下"})
	if err := cm.UseSeparateSessionFile(userID, sidePath); err != nil {
		t.Fatalf("opaque legacy path must not be opened as a side file: %v", err)
	}
	cm.Save(userID, []ConversationEntry{{Role: "user", Content: "必须留下"}, {Role: "assistant", Content: "新内容"}})
	if got := cm.Load(userID); len(got) != 2 {
		t.Fatalf("detached session was dropped: %#v", got)
	}
	assertSQLiteDetachedPath(t, mainPath, userID, sidePath)
}

func TestClearDetachedSessionDoesNotResurrect(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "ai_assistant_conversation.json")
	sidePath := filepath.Join(dir, "session-transcripts", "weather.json")
	userID := "desktop-user:D:/tasks/weather"
	cm := NewPersistentConversationMemory(mainPath)
	cm.Append(userID, ConversationEntry{Role: "user", Content: "清空前的内容"})
	if err := cm.UseSeparateSessionFile(userID, sidePath); err != nil {
		t.Fatal(err)
	}
	cm.Save(userID, []ConversationEntry{{Role: "user", Content: "清空前的内容"}})
	cm.Clear(userID)
	if err := cm.FlushNow(); err != nil {
		t.Fatal(err)
	}
	cm.Stop()

	reloaded := NewPersistentConversationMemory(mainPath)
	defer reloaded.Stop()
	if got := reloaded.Load(userID); len(got) != 0 {
		t.Fatalf("cleared session resurrected: %#v", got)
	}
}

func TestEvictExpiredKeepsDetachedPetTranscript(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "ai_assistant_conversation.json")
	sidePath := filepath.Join(dir, "pet_companion_conversation.json")
	cm := NewPersistentConversationMemory(mainPath)
	defer cm.Stop()
	if err := cm.UseSeparateSessionFile("desktop-pet", sidePath); err != nil {
		t.Fatal(err)
	}
	cm.Append("desktop-pet", ConversationEntry{Role: "user", Content: "宠物的话"})
	if err := cm.FlushNow(); err != nil {
		t.Fatal(err)
	}
	sh := cm.metadataShard("desktop-pet")
	sh.mu.Lock()
	if session := sh.sessions["desktop-pet"]; session != nil {
		session.lastAccess = time.Now().Add(-MemoryTTL - time.Hour)
	}
	sh.mu.Unlock()

	cm.EvictExpired()
	if err := cm.FlushNow(); err != nil {
		t.Fatal(err)
	}
	if got := cm.Load("desktop-pet"); len(got) != 1 || got[0].Content != "宠物的话" {
		t.Fatalf("pet session was evicted: %#v", got)
	}
}

func assertSQLiteDetachedPath(t *testing.T, storePath, userID, want string) {
	t.Helper()
	db, err := sql.Open("sqlite", conversationSQLitePath(storePath))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var got string
	if err := db.QueryRow(`SELECT detached_path FROM sessions WHERE session_id=?`, userID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("detached path for %q = %q, want %q", userID, got, want)
	}
}

func sqliteSessionUpdatedAt(t *testing.T, storePath, userID string) int64 {
	t.Helper()
	db, err := sql.Open("sqlite", conversationSQLitePath(storePath))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var updated int64
	if err := db.QueryRow(`SELECT updated_at_ns FROM sessions WHERE session_id=?`, userID).Scan(&updated); err != nil {
		t.Fatal(err)
	}
	return updated
}
