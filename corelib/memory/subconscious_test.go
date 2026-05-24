package memory

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSubconsciousMarkVolatilePersistsThroughSQLiteBatch(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	backend := newTestSQLiteBackend(t)
	store.SetBackend(backend, SyncConfig{Enabled: false})
	entry := Entry{ID: "volatile-sqlite", Content: "volatile memory", Category: CategoryProjectKnowledge, AccessCount: 1, Strength: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := store.UpsertEntriesByID([]Entry{entry}); err != nil {
		t.Fatalf("UpsertEntriesByID: %v", err)
	}

	store.MarkVolatile("volatile-sqlite")
	loaded, err := backend.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected one entry, got %+v", loaded)
	}
	if !loaded[0].Stale || loaded[0].Stability == nil || loaded[0].Stability.Level != StabilityVolatile || loaded[0].Stability.ContradictCount != 1 {
		t.Fatalf("volatile metadata not persisted: %+v", loaded[0])
	}
}

func TestSubconsciousRemovePersistsSQLiteTombstone(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	backend := newTestSQLiteBackend(t)
	store.SetBackend(backend, SyncConfig{Enabled: false})
	entry := Entry{ID: "remove-sqlite", Content: "remove memory", Category: CategoryProjectKnowledge, AccessCount: 1, Strength: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := store.UpsertEntriesByID([]Entry{entry}); err != nil {
		t.Fatalf("UpsertEntriesByID: %v", err)
	}

	store.Remove("remove-sqlite")
	loaded, err := backend.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(loaded) != 0 {
		t.Fatalf("removed entry should be absent from active backend rows: %+v", loaded)
	}
	_, deleted, err := backend.Since(0)
	if err != nil {
		t.Fatalf("Since: %v", err)
	}
	if len(deleted) != 1 || deleted[0] != "remove-sqlite" {
		t.Fatalf("removed entry should appear as sync tombstone, got %v", deleted)
	}
}
