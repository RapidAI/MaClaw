package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Sanitizing every entry on load cost 8 seconds on a real 38 MB / 16k-entry
// store, and it changed nothing: the file had been written sanitized by the same
// version. The schema version marks such a file so the loader can trust it.
func TestSnapshotEntriesNeedSanitize(t *testing.T) {
	if !snapshotEntriesNeedSanitize(0) {
		t.Fatal("unversioned stores must still be sanitized on load")
	}
	if snapshotEntriesNeedSanitize(memorySnapshotSchemaVersion) {
		t.Fatal("current-version stores must not be re-sanitized")
	}
	if snapshotEntriesNeedSanitize(memorySnapshotSchemaVersion + 1) {
		t.Fatal("a newer store must be trusted, not re-sanitized")
	}
}

// A save must stamp the version, otherwise the next load pays the full pass
// again forever.
func TestWriteMemorySnapshotFileStampsSchemaVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.json")
	err := writeMemorySnapshotFile(path, memorySnapshot{
		Sessions: map[string]persistedSession{
			"desktop-user": {Entries: []ConversationEntry{{Role: "user", Content: "hi"}}},
		},
	})
	if err != nil {
		t.Fatalf("writeMemorySnapshotFile: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got memorySnapshot
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.SchemaVersion != memorySnapshotSchemaVersion {
		t.Fatalf("schema_version = %d, want %d", got.SchemaVersion, memorySnapshotSchemaVersion)
	}
	if len(got.Sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(got.Sessions))
	}
}

// A store written by this version must load without being marked dirty, which is
// what keeps repeated starts from re-paying for (and re-writing) the pass.
func TestVersionedStoreLoadsWithoutRewrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "store.json")

	writer := NewConversationMemory()
	writer.storePath = path
	writer.Save("desktop-user", []ConversationEntry{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "world"},
	})
	if err := writer.FlushNow(); err != nil {
		t.Fatalf("FlushNow: %v", err)
	}
	writer.Stop()

	loaded := NewConversationMemory()
	loaded.storePath = path
	started := time.Now()
	if err := loaded.loadFromDisk(); err != nil {
		t.Fatalf("loadFromDisk: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("versioned load took %v; the sanitize pass is probably still running", elapsed)
	}
	if loaded.dirty {
		t.Fatal("a versioned store that changed nothing must not be marked dirty for rewrite")
	}
	if entries := loaded.Load("desktop-user"); len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	loaded.Stop()
}

// A legacy (unversioned) store must be marked for rewrite once, so the next
// start can take the fast path. This is the migration that makes the skip safe.
func TestLegacyStoreIsSanitizedAndRewritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.json")

	legacy := memorySnapshot{
		Sessions: map[string]persistedSession{
			"desktop-user": {
				Entries: []ConversationEntry{
					{Role: "user", Content: "hello"},
					{Role: "assistant", Content: "world"},
				},
				LastAccess: time.Now(),
			},
		},
	}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	loaded := NewConversationMemory()
	loaded.storePath = path
	if err := loaded.loadFromDisk(); err != nil {
		t.Fatalf("loadFromDisk: %v", err)
	}
	if entries := loaded.Load("desktop-user"); len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	if !loaded.dirty {
		t.Fatal("legacy store must be marked dirty so the sanitized form is persisted with a version")
	}

	// Flushing must stamp the version, turning the next load into the fast path.
	if err := loaded.FlushNow(); err != nil {
		t.Fatalf("FlushNow: %v", err)
	}
	loaded.Stop()

	rewritten, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var migrated memorySnapshot
	if err := json.Unmarshal(rewritten, &migrated); err != nil {
		t.Fatal(err)
	}
	if migrated.SchemaVersion != memorySnapshotSchemaVersion {
		t.Fatalf("after migration schema_version = %d, want %d", migrated.SchemaVersion, memorySnapshotSchemaVersion)
	}
}

// A side (detached) transcript used to bypass the sanitize pass entirely:
// loadFromDisk only walks the shared snapshot, so a legacy or hand-edited side
// file entered memory unredacted even when the shared store was clean.
func TestLegacySideSessionIsSanitizedOnLoad(t *testing.T) {
	dir := t.TempDir()
	sidePath := filepath.Join(dir, "side.json")

	legacy := []byte(`{"sessions":{"desktop-user:proj":{"entries":[` +
		`{"role":"assistant","content":"calling","tool_calls":[{"id":"call-1","type":"function","function":{"name":"ssh","arguments":"{\"password\":\"legacy-password\"}"}}]},` +
		`{"role":"tool","content":"Cookie: legacy-cookie","tool_call_id":"call-1"}],` +
		`"last_access":"2026-01-01T00:00:00Z"}}}`)
	if err := os.WriteFile(sidePath, legacy, 0o644); err != nil {
		t.Fatalf("write side file: %v", err)
	}

	cm := NewConversationMemory()
	cm.storePath = filepath.Join(dir, "store.json")
	if err := cm.UseSeparateSessionFile("desktop-user:proj", sidePath); err != nil {
		t.Fatalf("UseSeparateSessionFile: %v", err)
	}
	defer cm.Stop()

	entries := cm.Load("desktop-user:proj")
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	if dump := fmt.Sprintf("%v", entries[0].ToolCalls); strings.Contains(dump, "legacy-password") {
		t.Fatalf("legacy side-file tool arguments were not redacted: %s", dump)
	}
	if dump := fmt.Sprintf("%v", entries[1].Content); strings.Contains(dump, "legacy-cookie") {
		t.Fatalf("legacy side-file tool output was not redacted: %q", dump)
	}
}

// A side file written by the current version must load unchanged, so the hot
// path stays free of the sanitize pass.
func TestVersionedSideSessionLoadsUnchanged(t *testing.T) {
	dir := t.TempDir()
	sidePath := filepath.Join(dir, "side.json")

	current := memorySnapshot{
		SchemaVersion: memorySnapshotSchemaVersion,
		Sessions: map[string]persistedSession{
			"desktop-user:proj": {
				Entries: []ConversationEntry{
					{Role: "user", Content: "keep me"},
					{Role: "assistant", Content: "and me"},
				},
				LastAccess: time.Now(),
			},
		},
	}
	data, err := json.Marshal(current)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sidePath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	cm := NewConversationMemory()
	cm.storePath = filepath.Join(dir, "store.json")
	if err := cm.UseSeparateSessionFile("desktop-user:proj", sidePath); err != nil {
		t.Fatalf("UseSeparateSessionFile: %v", err)
	}
	defer cm.Stop()

	entries := cm.Load("desktop-user:proj")
	if len(entries) != 2 ||
		fmt.Sprintf("%v", entries[0].Content) != "keep me" ||
		fmt.Sprintf("%v", entries[1].Content) != "and me" {
		t.Fatalf("versioned side-file entries changed: %#v", entries)
	}
}
