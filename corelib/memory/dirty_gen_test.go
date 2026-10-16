package memory

// dirty_gen_test.go guards the DirtyGen contract that generation-keyed
// derived-index caches (e.g. guiapp's scene index cache) rely on: the
// generation must increase on EVERY in-memory entries mutation, including the
// backend-backed paths that never call markDirtyLocked.

import (
	"path/filepath"
	"testing"
	"time"
)

func TestDirtyGenBumpsOnBackendBackedMutations(t *testing.T) {
	dir := t.TempDir()
	backend, err := NewSQLiteBackend(filepath.Join(dir, "memory.db"))
	if err != nil {
		t.Fatalf("NewSQLiteBackend: %v", err)
	}
	defer backend.Close()
	store, err := NewStore(filepath.Join(dir, "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	store.SetBackend(backend, SyncConfig{Enabled: true, Interval: time.Hour, InstanceID: "test-instance"})
	defer store.Stop()

	bump := func(name string, fn func()) {
		t.Helper()
		before := store.DirtyGen()
		fn()
		if got := store.DirtyGen(); got <= before {
			t.Fatalf("%s: DirtyGen did not increase (before=%d after=%d)", name, before, got)
		}
	}

	entryID := ""
	bump("Save", func() {
		entryID = "dg-entry-1"
		if err := store.Save(Entry{ID: entryID, Content: "用户喜欢咖啡", Category: CategoryUserFact, Tags: []string{"t"}}); err != nil {
			t.Fatalf("Save: %v", err)
		}
	})
	bump("Update", func() {
		if err := store.Update(entryID, "用户喜欢喝茶", CategoryUserFact, []string{"t"}); err != nil {
			t.Fatalf("Update: %v", err)
		}
	})
	bump("Delete", func() {
		if err := store.Delete(entryID); err != nil {
			t.Fatalf("Delete: %v", err)
		}
	})
	bump("RestoreEntriesSnapshot", func() {
		if err := store.RestoreEntriesSnapshot(nil); err != nil {
			t.Fatalf("RestoreEntriesSnapshot: %v", err)
		}
	})
	bump("applyRemoteSyncBatchLocked", func() {
		store.mu.Lock()
		// Version must exceed the sync high-water mark (the snapshot restore
		// above advances it to the backend's max version) or the entry is skipped.
		store.applyRemoteSyncBatchLocked([]Entry{{ID: "dg-sync-1", Content: "远程同步条目", Category: CategoryUserFact, Version: 1 << 40, UpdatedAt: time.Now()}}, nil)
		store.mu.Unlock()
	})
}
