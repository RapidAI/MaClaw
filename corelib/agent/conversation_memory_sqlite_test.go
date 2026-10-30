package agent

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestConversationSQLiteMigrationPreservesEveryBranchAndActivePath(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "ai_assistant_conversation.json")
	sidePath := filepath.Join(dir, "session-transcripts", "detached.json")
	extraPath := filepath.Join(dir, "session-transcripts", "unindexed.json")
	now := time.Now().UTC().Truncate(time.Microsecond)
	shared := memorySnapshot{SchemaVersion: memorySnapshotSchemaVersion, Sessions: map[string]persistedSession{
		"desktop-user": {
			LastAccess: now,
			Entries: []ConversationEntry{
				{Role: "user", Content: "root", ID: "root", Timestamp: 1},
				{Role: "assistant", Content: "old branch", ID: "old", ParentID: "root", Timestamp: 2},
				{Role: "assistant", Content: "active branch", ID: "active", ParentID: "root", Timestamp: 3, ToolCalls: []interface{}{map[string]interface{}{"id": "call-1"}}},
			},
			ActiveBranchTipID: "active",
			UnfinishedSlot:    &UnfinishedTaskSlot{SlotID: "slot", UserID: "desktop-user", Status: UnfinishedTaskSlotStatusResumed, CreatedAt: now, UpdatedAt: now},
		},
		"desktop-user:D:/detached": {LastAccess: now.Add(-time.Hour), Entries: []ConversationEntry{{Role: "user", Content: "stale shared", ID: "stale", Timestamp: 1}}},
	}}
	writeConversationSnapshotForTest(t, mainPath, shared)
	writeConversationSnapshotForTest(t, sidePath, memorySnapshot{SchemaVersion: memorySnapshotSchemaVersion, Sessions: map[string]persistedSession{
		"desktop-user:D:/detached": {
			LastAccess: now,
			Entries: []ConversationEntry{
				{Role: "user", Content: "detached root", ID: "d-root", Timestamp: 4},
				{Role: "assistant", Content: "detached child", ID: "d-child", ParentID: "d-root", Timestamp: 5},
			},
			ActiveBranchTipID: "d-child",
		},
	}})
	writeConversationSnapshotForTest(t, extraPath, memorySnapshot{SchemaVersion: memorySnapshotSchemaVersion, Sessions: map[string]persistedSession{
		"desktop-user:D:/unindexed": {LastAccess: now, Entries: []ConversationEntry{{Role: "user", Content: "must import", ID: "u-root", Timestamp: 6}}},
	}})
	index, err := json.Marshal(detachedSessionIndex{Sessions: map[string]string{"desktop-user:D:/detached": sidePath}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mainPath+".detached.json", index, 0o644); err != nil {
		t.Fatal(err)
	}
	migration, err := readLegacyConversationMigration(mainPath)
	if err != nil {
		t.Fatal(err)
	}

	cm := NewPersistentConversationMemory(mainPath)
	defer cm.Stop()
	if cm.sqlite == nil || cm.legacyJSON {
		t.Fatal("legacy snapshot did not migrate to SQLite")
	}
	for userID, want := range migration.sessions {
		got := cm.LoadAll(userID)
		if !reflect.DeepEqual(got, want.Entries) {
			t.Fatalf("session %q entries differ after migration\ngot:  %#v\nwant: %#v", userID, got, want.Entries)
		}
		wantActive := NewConversationTreeWithTip(want.Entries, want.ActiveBranchTipID).ActiveBranch()
		if gotActive := cm.LoadActiveBranch(userID); !reflect.DeepEqual(gotActive, wantActive) {
			t.Fatalf("session %q active branch differs\ngot:  %#v\nwant: %#v", userID, gotActive, wantActive)
		}
	}
	for _, path := range []string{mainPath, sidePath, extraPath, mainPath + ".detached.json"} {
		if _, err := os.Stat(path + ".migrated.bak"); err != nil {
			t.Fatalf("migration backup %q: %v", path, err)
		}
	}
	cm.Save("desktop-user", append(cm.LoadActiveBranch("desktop-user"), ConversationEntry{Role: "user", Content: "after migration"}))
	if err := cm.FlushNow(); err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(mainPath + ".migrated.bak")
	if err != nil {
		t.Fatal(err)
	}
	var untouched memorySnapshot
	if err := json.Unmarshal(backup, &untouched); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(untouched.Sessions, shared.Sessions) {
		t.Fatal("new SQLite save rewrote the migrated JSON backup")
	}
}

func TestConversationSQLiteInterruptedMigrationRetriesImport(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "ai_assistant_conversation.json")
	writeConversationSnapshotForTest(t, mainPath, memorySnapshot{SchemaVersion: memorySnapshotSchemaVersion, Sessions: map[string]persistedSession{
		"desktop-user": {LastAccess: time.Now().UTC(), Entries: []ConversationEntry{{Role: "user", Content: "survived the crash", ID: "root", Timestamp: 1}}},
	}})
	// Simulate a kill between database creation and the import commit: the file
	// exists (schema applied, no sessions) but the legacy JSON is not imported
	// and not renamed. The next open must retry the import instead of treating
	// the database as already migrated.
	dbPath := conversationSQLitePath(mainPath)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(conversationSQLiteSchema); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(mainPath + ".migrated.bak"); !os.IsNotExist(err) {
		t.Fatalf("legacy source renamed before any import: %v", err)
	}

	cm := NewPersistentConversationMemory(mainPath)
	defer cm.Stop()
	if cm.sqlite == nil || cm.legacyJSON {
		t.Fatal("interrupted migration did not retry the SQLite import")
	}
	got := cm.Load("desktop-user")
	if len(got) != 1 || got[0].Content != "survived the crash" {
		t.Fatalf("entries after interrupted migration = %#v", got)
	}
	if _, err := os.Stat(mainPath + ".migrated.bak"); err != nil {
		t.Fatalf("import retry did not back up the legacy source: %v", err)
	}

	// A second open must not re-import or touch the backup.
	cm.Stop()
	reopened := NewPersistentConversationMemory(mainPath)
	defer reopened.Stop()
	if reopened.legacyJSON || reopened.sqlite == nil {
		t.Fatal("reopen after completed migration lost the SQLite backend")
	}
	if got := reopened.Load("desktop-user"); len(got) != 1 || got[0].Content != "survived the crash" {
		t.Fatalf("entries after reopen = %#v", got)
	}
	backup, err := os.ReadFile(mainPath + ".migrated.bak")
	if err != nil {
		t.Fatal(err)
	}
	var snapshot memorySnapshot
	if err := json.Unmarshal(backup, &snapshot); err != nil {
		t.Fatal(err)
	}
	if _, ok := snapshot.Sessions["desktop-user"]; !ok {
		t.Fatal("reopen rewrote the migrated backup")
	}
}

func TestConversationSQLiteActiveBranchDoesNotLoadWholeSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conversation.json")
	cm := NewPersistentConversationMemory(path)
	cm.Save("desktop-user", []ConversationEntry{
		{Role: "user", Content: "root"},
		{Role: "assistant", Content: "child"},
	})
	want := cm.LoadActiveBranch("desktop-user")
	cm.Stop()

	reopened := NewPersistentConversationMemory(path)
	defer reopened.Stop()
	sh := reopened.metadataShard("desktop-user")
	sh.mu.RLock()
	loadedBefore := sh.sessions["desktop-user"].entriesLoaded
	sh.mu.RUnlock()
	if loadedBefore {
		t.Fatal("startup eagerly loaded conversation entries")
	}
	got := reopened.LoadActiveBranch("desktop-user")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("active branch = %#v, want %#v", got, want)
	}
	sh.mu.RLock()
	loadedAfter := sh.sessions["desktop-user"].entriesLoaded
	sh.mu.RUnlock()
	if loadedAfter {
		t.Fatal("active branch query loaded the whole session")
	}
}

func TestConversationSQLiteSearchUsesEntryColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conversation.json")
	cm := NewPersistentConversationMemory(path)
	defer cm.Stop()
	cm.Save("desktop-user", []ConversationEntry{
		{Role: "user", Content: "weather in lanzhou tomorrow"},
		{Role: "assistant", Content: "rain is likely"},
	})
	if err := cm.FlushNow(); err != nil {
		t.Fatal(err)
	}
	hits, err := cm.SearchEntries("desktop-user", "weather", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Role != "user" {
		t.Fatalf("hits = %#v", hits)
	}
	short, err := cm.SearchEntries("desktop-user", "ai", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(short) != 1 || short[0].Role != "assistant" {
		t.Fatalf("short query hits = %#v", short)
	}
	db, err := sql.Open("sqlite", conversationSQLitePath(path)+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var role, text string
	if err := db.QueryRow(`SELECT role, content_text FROM conversation_entries WHERE session_id='desktop-user' AND role='user'`).Scan(&role, &text); err != nil {
		t.Fatal(err)
	}
	if role != "user" || !strings.Contains(text, "weather") {
		t.Fatalf("columns role=%q text=%q", role, text)
	}
}

func TestConversationSQLiteSearchFoldsLetterCase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conversation.json")
	cm := NewPersistentConversationMemory(path)
	cm.Save("desktop-user", []ConversationEntry{
		{Role: "user", Content: "Weather forecast tonight"},
		{Role: "assistant", Content: "weather forecast is clear"},
	})
	if err := cm.FlushNow(); err != nil {
		t.Fatal(err)
	}
	hits, err := cm.SearchEntries("desktop-user", "Weather", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("write-path hits = %#v", hits)
	}
	cm.Stop()

	db, err := sql.Open("sqlite", conversationSQLitePath(path)+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM conversation_entries_fts`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO conversation_entries_fts(session_id, node_id, role, tool_name, content_text)
		SELECT session_id, node_id, role, tool_name, content_text FROM conversation_entries`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM conversation_meta WHERE key='fts_lower_v1'`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := NewPersistentConversationMemory(path)
	defer reopened.Stop()
	folded, err := reopened.SearchEntries("desktop-user", "WEATHER", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(folded) != 2 {
		t.Fatalf("rebuilt hits = %#v", folded)
	}
	check, err := sql.Open("sqlite", conversationSQLitePath(path)+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer check.Close()
	var stored string
	if err := check.QueryRow(`SELECT content_text FROM conversation_entries WHERE content_text LIKE '%Weather forecast tonight%'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stored, "Weather forecast tonight") {
		t.Fatalf("stored text = %q", stored)
	}
}

func TestConversationSQLiteIncrementalFlushUpdatesOnlyChangedNodes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conversation.json")
	cm := NewPersistentConversationMemory(path)
	defer cm.Stop()
	cm.Save("desktop-user", []ConversationEntry{{Role: "user", Content: "root"}})
	cm.Save("other", []ConversationEntry{{Role: "user", Content: "untouched"}})
	if err := cm.FlushNow(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", conversationSQLitePath(path))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rootID := cm.LoadActiveBranch("desktop-user")[0].ID
	var rootUpdated, otherSessionUpdated int64
	if err := db.QueryRow(`SELECT updated_at_ns FROM conversation_entries WHERE session_id='desktop-user' AND node_id=?`, rootID).Scan(&rootUpdated); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT updated_at_ns FROM sessions WHERE session_id='other'`).Scan(&otherSessionUpdated); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	cm.Append("desktop-user", ConversationEntry{Role: "assistant", Content: "new child"})
	if err := cm.FlushNow(); err != nil {
		t.Fatal(err)
	}
	var rootAfter, otherAfter int64
	if err := db.QueryRow(`SELECT updated_at_ns FROM conversation_entries WHERE session_id='desktop-user' AND node_id=?`, rootID).Scan(&rootAfter); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT updated_at_ns FROM sessions WHERE session_id='other'`).Scan(&otherAfter); err != nil {
		t.Fatal(err)
	}
	if rootAfter != rootUpdated {
		t.Fatal("unchanged node was rewritten")
	}
	if otherAfter != otherSessionUpdated {
		t.Fatal("unrelated session was rewritten")
	}
	want := cm.LoadAll("desktop-user")
	cm.Stop()
	reopened := NewPersistentConversationMemory(path)
	defer reopened.Stop()
	if got := reopened.LoadAll("desktop-user"); !reflect.DeepEqual(got, want) {
		t.Fatalf("incremental reopen differs\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestConversationSQLiteOpenFailureFallsBackToLegacyJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conversation.json")
	writeConversationSnapshotForTest(t, path, memorySnapshot{Sessions: map[string]persistedSession{
		"desktop-user": {LastAccess: time.Now(), Entries: []ConversationEntry{{Role: "user", Content: "legacy fallback"}}},
	}})
	if err := os.Mkdir(conversationSQLitePath(path), 0o755); err != nil {
		t.Fatal(err)
	}
	cm := NewPersistentConversationMemory(path)
	defer cm.Stop()
	if !cm.legacyJSON {
		t.Fatal("SQLite open failure did not select legacy JSON mode")
	}
	if got := cm.Load("desktop-user"); len(got) != 1 || got[0].Content != "legacy fallback" {
		t.Fatalf("legacy fallback entries = %#v", got)
	}
	cm.Append("desktop-user", ConversationEntry{Role: "assistant", Content: "legacy write"})
	if err := cm.FlushNow(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("legacy store is not usable after fallback: %v", err)
	}
	if _, err := os.Stat(path + ".migrated.bak"); !os.IsNotExist(err) {
		t.Fatal("failed migration renamed the legacy source")
	}
}

func TestConversationSQLiteSchemaAndSeparateReadCompatibility(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ai_assistant_conversation.json")
	side := filepath.Join(dir, "session-transcripts", "owner.json")
	cm := NewPersistentConversationMemory(path)
	if err := cm.UseSeparateSessionFile("desktop-user:D:/project", side); err != nil {
		t.Fatal(err)
	}
	cm.Save("desktop-user:D:/project", []ConversationEntry{{Role: "user", Content: "detached sqlite"}})
	cm.Stop()
	entries, err := LoadSeparateSessionEntries(side, "desktop-user:D:/project")
	if err != nil || len(entries) != 1 || entries[0].Content != "detached sqlite" {
		t.Fatalf("LoadSeparateSessionEntries = %#v, %v", entries, err)
	}
	db, err := sql.Open("sqlite", conversationSQLitePath(path))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, index := range []string{"conversation_entries_parent", "conversation_entries_updated"} {
		var count int
		if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='index' AND name=?`, index).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("missing index %q", index)
		}
	}
}

func writeConversationSnapshotForTest(t *testing.T, path string, snapshot memorySnapshot) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readConversationStoreForTest(t *testing.T, path string) ([]byte, error) {
	t.Helper()
	if data, err := os.ReadFile(path); err == nil {
		return data, nil
	}
	db, err := sql.Open("sqlite", conversationSQLitePath(path))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := &conversationSQLiteStore{db: db, path: conversationSQLitePath(path)}
	rows, err := db.Query(`SELECT session_id, metadata_json FROM sessions`)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := memorySnapshot{SchemaVersion: memorySnapshotSchemaVersion, Sessions: map[string]persistedSession{}}
	for rows.Next() {
		var userID, metadata string
		if err := rows.Scan(&userID, &metadata); err != nil {
			t.Fatal(err)
		}
		var session persistedSession
		if err := json.Unmarshal([]byte(metadata), &session); err != nil {
			t.Fatal(err)
		}
		session.Entries, _, err = store.loadEntries(userID)
		if err != nil {
			t.Fatal(err)
		}
		snapshot.Sessions[userID] = session
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return data, nil
}

func TestConversationSQLiteSeparateReadMatchesStoredTreeOrder(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "conversations.json")
	side := filepath.Join(dir, "sessions", "chat.json")
	cm := NewPersistentConversationMemory(main)
	if err := cm.UseSeparateSessionFile("chat", side); err != nil {
		t.Fatal(err)
	}
	cm.Save("chat", []ConversationEntry{{Role: "user", Content: "old"}, {Role: "assistant", Content: "old answer"}})
	cm.Save("chat", []ConversationEntry{{Role: "user", Content: "synced"}, {Role: "assistant", Content: "synced answer"}})
	cm.Save("chat", []ConversationEntry{{Role: "user", Content: "next"}, {Role: "assistant", Content: "next answer"}})
	active := cm.Load("chat")
	cm.Save("chat", append(active, ConversationEntry{Role: "user", Content: "question"}, ConversationEntry{Role: "assistant", Content: "answer"}))
	want := cm.LoadAll("chat")
	if err := cm.FlushNow(); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSeparateSessionEntries(side, "chat")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("separate read order differs\ngot:  %#v\nwant: %#v", got, want)
	}
	cm.Stop()
}

func TestConversationSQLiteDetachedSaveDoesNotFlushOtherDirtySession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conversation.json")
	cm := NewPersistentConversationMemory(path)
	defer cm.Stop()
	cm.Save("desktop-user", []ConversationEntry{{Role: "user", Content: "durable"}})
	cm.Save("desktop-user:D:/project", []ConversationEntry{{Role: "user", Content: "initial"}})
	if err := cm.FlushNow(); err != nil {
		t.Fatal(err)
	}
	cm.Append("desktop-user", ConversationEntry{Role: "assistant", Content: "still pending"})
	if err := cm.UseSeparateSessionFile("desktop-user:D:/project", filepath.Join(filepath.Dir(path), "session-transcripts", "project.json")); err != nil {
		t.Fatal(err)
	}
	cm.Save("desktop-user:D:/project", []ConversationEntry{{Role: "user", Content: "detached synchronous"}})
	db, err := sql.Open("sqlite", conversationSQLitePath(path))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var otherEntries int
	if err := db.QueryRow(`SELECT count(*) FROM conversation_entries WHERE session_id='desktop-user'`).Scan(&otherEntries); err != nil {
		t.Fatal(err)
	}
	if otherEntries != 1 {
		t.Fatalf("detached save flushed an unrelated dirty session: entries=%d", otherEntries)
	}
	if got := cm.Load("desktop-user:D:/project"); len(got) != 1 || got[0].Content != "detached synchronous" {
		t.Fatalf("detached synchronous save = %#v", got)
	}
	if err := cm.FlushNow(); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM conversation_entries WHERE session_id='desktop-user'`).Scan(&otherEntries); err != nil {
		t.Fatal(err)
	}
	if otherEntries != 2 {
		t.Fatalf("FlushNow did not commit the other dirty session: entries=%d", otherEntries)
	}
}

func TestConversationSQLiteBranchWalkUsesCoveringIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ai_assistant_conversation.json")
	cm := NewPersistentConversationMemory(path)
	defer cm.Stop()
	cm.Save("desktop-user", []ConversationEntry{
		{Role: "user", Content: strings.Repeat("x", 4000), ID: "root", Timestamp: 1},
		{Role: "assistant", Content: strings.Repeat("y", 4000), ID: "child", ParentID: "root", Timestamp: 2},
	})
	if err := cm.FlushNow(); err != nil {
		t.Fatal(err)
	}
	rows, err := cm.sqlite.db.Query(`EXPLAIN QUERY PLAN `+conversationBranchFromTipSQL, "desktop-user", "child", "desktop-user", "desktop-user")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan += detail + "\n"
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan, "USING COVERING INDEX conversation_entries_walk") {
		t.Fatalf("branch walk plan = %s", plan)
	}
	orderRows, err := cm.sqlite.db.Query(`EXPLAIN QUERY PLAN SELECT node_id, entry_order FROM conversation_entries INDEXED BY conversation_entries_order WHERE session_id=?`, "desktop-user")
	if err != nil {
		t.Fatal(err)
	}
	defer orderRows.Close()
	plan = ""
	for orderRows.Next() {
		var id, parent, unused int
		var detail string
		if err := orderRows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan += detail + "\n"
	}
	if err := orderRows.Err(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan, "USING COVERING INDEX conversation_entries_order") {
		t.Fatalf("entry order plan = %s", plan)
	}
}
