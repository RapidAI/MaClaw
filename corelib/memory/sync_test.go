package memory

import (
	"path/filepath"
	"testing"
	"time"
)

// newSyncTestStores creates two Store instances sharing the same SQLite DB,
// simulating two maclawsrv instances on the same server.
func newSyncTestStores(t *testing.T) (store1, store2 *Store, dbPath string) {
	t.Helper()
	dir := t.TempDir()
	dbPath = filepath.Join(dir, "memory.db")

	// Create the shared SQLite backend for store 1.
	b1, err := NewSQLiteBackend(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteBackend (store1): %v", err)
	}

	// Store 1: create with JSON path (for legacy compat), then override backend.
	jsonPath1 := filepath.Join(dir, "s1_memories.json")
	store1, err = NewStore(jsonPath1)
	if err != nil {
		t.Fatalf("NewStore (store1): %v", err)
	}
	store1.SetBackend(b1, SyncConfig{Enabled: false, InstanceID: "instance-1"})

	// Create the shared SQLite backend for store 2 (same DB file).
	b2, err := NewSQLiteBackend(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteBackend (store2): %v", err)
	}

	jsonPath2 := filepath.Join(dir, "s2_memories.json")
	store2, err = NewStore(jsonPath2)
	if err != nil {
		t.Fatalf("NewStore (store2): %v", err)
	}
	// Store 2 has sync enabled with a very short interval for testing.
	store2.SetBackend(b2, SyncConfig{
		Enabled:    true,
		Interval:   100 * time.Millisecond, // fast polling for tests
		InstanceID: "instance-2",
	})

	t.Cleanup(func() {
		store1.Stop()
		store2.Stop()
		b1.Close()
		b2.Close()
	})

	return store1, store2, dbPath
}

func TestSyncLoop_BasicSync(t *testing.T) {
	store1, store2, _ := newSyncTestStores(t)

	now := time.Now().UTC()

	// Store 1 writes an entry directly to the backend.
	entry := Entry{
		ID:          "sync-test-1",
		Content:     "我叫张三",
		Category:    CategoryUserFact,
		Tags:        []string{"name"},
		OwnerID:     "user-A",
		Strength:    1.0,
		AccessCount: 1,
		Scope:       ScopeGlobal,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store1.backend.SaveEntry(&entry); err != nil {
		t.Fatalf("store1 SaveEntry: %v", err)
	}

	// Wait for store2's sync loop to pick it up.
	time.Sleep(300 * time.Millisecond)

	// Manually trigger sync (in case timing is tight).
	store2.syncOnce()

	// Verify store2 has the entry in memory.
	store2.mu.RLock()
	found := false
	for _, e := range store2.entries {
		if e.ID == "sync-test-1" && e.Content == "我叫张三" {
			found = true
			break
		}
	}
	store2.mu.RUnlock()

	if !found {
		t.Error("store2 did not sync entry from store1")
	}
}

func TestSyncLoop_DeleteSync(t *testing.T) {
	store1, store2, _ := newSyncTestStores(t)

	now := time.Now().UTC()

	// Write and sync an entry.
	entry := Entry{
		ID: "del-test-1", Content: "to be deleted", Category: CategoryUserFact,
		Strength: 1.0, AccessCount: 1, CreatedAt: now, UpdatedAt: now,
	}
	store1.backend.SaveEntry(&entry)

	// Sync to store2.
	store2.syncOnce()

	// Verify it's there.
	store2.mu.RLock()
	idx := store2.findEntryIndexByIDLocked("del-test-1")
	store2.mu.RUnlock()
	if idx < 0 {
		t.Fatal("entry not synced to store2")
	}

	// Delete from store1's backend.
	if err := store1.backend.DeleteEntry("del-test-1"); err != nil {
		t.Fatal(err)
	}

	// Sync again.
	store2.syncOnce()

	// Verify it's gone from store2.
	store2.mu.RLock()
	idx = store2.findEntryIndexByIDLocked("del-test-1")
	store2.mu.RUnlock()
	if idx >= 0 {
		t.Error("deleted entry still present in store2 after sync")
	}
}

func TestSyncLoop_DeleteRebuildsDerivedProjectIndex(t *testing.T) {
	store1, store2, _ := newSyncTestStores(t)

	now := time.Now().UTC()
	projectPath := filepath.Join(t.TempDir(), "project-alpha")
	entry := Entry{
		ID:          "sync-project-delete",
		Content:     "Project Alpha shipped a workflow output",
		Category:    CategoryTaskArtifact,
		Tags:        []string{projectPath, "tangible_output"},
		SourceType:  "task_artifact",
		Strength:    1.0,
		AccessCount: 1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store1.Save(entry); err != nil {
		t.Fatalf("store1 Save: %v", err)
	}

	store2.syncOnce()
	if _, ok := store2.ProjectRecordForHost(projectPath); !ok {
		t.Fatal("project record not synced to store2")
	}

	if err := store1.Delete("sync-project-delete"); err != nil {
		t.Fatalf("store1 Delete: %v", err)
	}
	store2.syncOnce()

	if _, ok := store2.ProjectRecordForHost(projectPath); ok {
		t.Fatal("deleted remote project entry left stale project index record")
	}
}

func TestSyncLoop_DeleteCleansInMemoryGraphLinks(t *testing.T) {
	store1, store2, _ := newSyncTestStores(t)

	now := time.Now().UTC()
	keep := Entry{
		ID:           "sync-graph-keep",
		Content:      "sync graph keep",
		Category:     CategoryProjectKnowledge,
		RelatedIDs:   []string{"sync-graph-delete"},
		RelatedEdges: []RelatedEdge{{ID: "sync-graph-delete", Strength: 0.9}},
		Strength:     1,
		AccessCount:  1,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	remove := Entry{
		ID:          "sync-graph-delete",
		Content:     "sync graph delete",
		Category:    CategoryProjectKnowledge,
		Strength:    1,
		AccessCount: 1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store1.backend.SaveEntry(&keep); err != nil {
		t.Fatalf("save keep: %v", err)
	}
	if err := store1.backend.SaveEntry(&remove); err != nil {
		t.Fatalf("save remove: %v", err)
	}

	store2.syncOnce()
	store2.mu.RLock()
	idx := store2.findEntryIndexByIDLocked("sync-graph-keep")
	if idx < 0 || len(store2.entries[idx].RelatedIDs) == 0 || len(store2.entries[idx].RelatedEdges) == 0 {
		store2.mu.RUnlock()
		t.Fatalf("expected graph link before remote delete, idx=%d", idx)
	}
	store2.mu.RUnlock()

	if err := store1.backend.DeleteEntry("sync-graph-delete"); err != nil {
		t.Fatalf("delete remove: %v", err)
	}
	store2.syncOnce()

	store2.mu.RLock()
	idx = store2.findEntryIndexByIDLocked("sync-graph-keep")
	if idx < 0 {
		store2.mu.RUnlock()
		t.Fatal("keep entry missing after remote delete sync")
	}
	if len(store2.entries[idx].RelatedIDs) != 0 || len(store2.entries[idx].RelatedEdges) != 0 {
		links := append([]string(nil), store2.entries[idx].RelatedIDs...)
		edges := append([]RelatedEdge(nil), store2.entries[idx].RelatedEdges...)
		store2.mu.RUnlock()
		t.Fatalf("remote delete left stale in-memory graph links: ids=%v edges=%+v", links, edges)
	}
	store2.mu.RUnlock()
}

func TestSyncLoop_MixedWindowAdvancesWatermarkThroughDelete(t *testing.T) {
	store1, store2, _ := newSyncTestStores(t)

	now := time.Now().UTC()
	keep := Entry{
		ID:          "sync-mixed-keep",
		Content:     "mixed window keep",
		Category:    CategoryUserFact,
		Strength:    1.0,
		AccessCount: 1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	remove := Entry{
		ID:          "sync-mixed-delete",
		Content:     "mixed window delete",
		Category:    CategoryUserFact,
		Strength:    1.0,
		AccessCount: 1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store1.backend.SaveEntry(&keep); err != nil {
		t.Fatalf("save keep: %v", err)
	}
	if err := store1.backend.SaveEntry(&remove); err != nil {
		t.Fatalf("save remove: %v", err)
	}
	if err := store1.backend.DeleteEntry("sync-mixed-delete"); err != nil {
		t.Fatalf("delete remove: %v", err)
	}
	maxVersion, err := store1.backend.MaxVersion()
	if err != nil {
		t.Fatalf("MaxVersion: %v", err)
	}
	if maxVersion <= keep.Version {
		t.Fatalf("delete should own highest version, keep=%d max=%d", keep.Version, maxVersion)
	}

	store2.syncOnce()

	store2.mu.RLock()
	keepIdx := store2.findEntryIndexByIDLocked("sync-mixed-keep")
	deleteIdx := store2.findEntryIndexByIDLocked("sync-mixed-delete")
	lastVersion := store2.sync.lastVersion
	store2.mu.RUnlock()
	if keepIdx < 0 {
		t.Fatal("modified entry from mixed sync window was not merged")
	}
	if deleteIdx >= 0 {
		t.Fatal("deleted entry from mixed sync window was left in memory")
	}
	if lastVersion != maxVersion {
		t.Fatalf("sync watermark should advance through highest delete version, got %d want %d", lastVersion, maxVersion)
	}
}

func TestSyncLoop_UpdateSync(t *testing.T) {
	store1, store2, _ := newSyncTestStores(t)

	now := time.Now().UTC()

	// Write initial entry.
	entry := Entry{
		ID: "upd-test-1", Content: "original", Category: CategoryUserFact,
		Strength: 1.0, AccessCount: 1, CreatedAt: now, UpdatedAt: now,
	}
	store1.backend.SaveEntry(&entry)
	store2.syncOnce()

	// Update the entry.
	entry.Content = "updated content"
	entry.UpdatedAt = now.Add(time.Second)
	store1.backend.UpdateEntry(&entry)

	// Sync.
	store2.syncOnce()

	// Verify update propagated.
	store2.mu.RLock()
	idx := store2.findEntryIndexByIDLocked("upd-test-1")
	var content string
	if idx >= 0 {
		content = store2.entries[idx].Content
	}
	store2.mu.RUnlock()

	if content != "updated content" {
		t.Errorf("expected 'updated content', got %q", content)
	}
}

func TestSyncLoop_BM25IndexUpdated(t *testing.T) {
	store1, store2, _ := newSyncTestStores(t)

	now := time.Now().UTC()

	// Write an entry with searchable content.
	entry := Entry{
		ID: "bm25-test", Content: "PostgreSQL database backup strategy",
		Category: CategoryProjectKnowledge, Tags: []string{"postgres", "backup"},
		Strength: 1.0, AccessCount: 1, CreatedAt: now, UpdatedAt: now,
	}
	store1.backend.SaveEntry(&entry)

	// Sync.
	store2.syncOnce()

	// Verify BM25 index was updated — search should find it.
	store2.mu.RLock()
	scores := store2.bm25.score("PostgreSQL backup")
	store2.mu.RUnlock()

	if scores["bm25-test"] <= 0 {
		t.Error("BM25 index not updated after sync — entry not searchable")
	}
}

func TestSyncLoop_NoSyncWhenDisabled(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "memory.db")

	b, err := NewSQLiteBackend(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	jsonPath := filepath.Join(dir, "memories.json")
	store, err := NewStore(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	// Set backend with sync disabled.
	store.SetBackend(b, SyncConfig{Enabled: false})

	if store.sync != nil {
		t.Error("sync state should be nil when disabled")
	}
}

func TestSyncLoop_MultipleEntries(t *testing.T) {
	store1, store2, _ := newSyncTestStores(t)

	now := time.Now().UTC()

	// Write 10 entries from store1.
	for i := 0; i < 10; i++ {
		e := Entry{
			ID:          generateID(),
			Content:     "entry content",
			Category:    CategoryUserFact,
			Strength:    1.0,
			AccessCount: 1,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		store1.backend.SaveEntry(&e)
	}

	// Sync.
	store2.syncOnce()

	// Verify all 10 are in store2.
	store2.mu.RLock()
	count := len(store2.entries)
	store2.mu.RUnlock()

	if count != 10 {
		t.Errorf("expected 10 entries in store2, got %d", count)
	}
}
