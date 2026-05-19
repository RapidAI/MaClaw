package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewStoreWithMode_SQLite_Fresh(t *testing.T) {
	dir := t.TempDir()

	store, err := NewStoreWithMode(dir, StoreModeSQLite)
	if err != nil {
		t.Fatalf("NewStoreWithMode: %v", err)
	}
	defer store.Stop()

	if store.backend == nil {
		t.Fatal("expected non-nil backend")
	}
	if !store.backend.SupportsSync() {
		t.Error("expected SQLite backend to support sync")
	}

	// Verify DB file was created.
	dbPath := filepath.Join(dir, "memory.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("memory.db not created")
	}
}

func TestNewStoreWithMode_JSON_Fresh(t *testing.T) {
	dir := t.TempDir()

	store, err := NewStoreWithMode(dir, StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode: %v", err)
	}
	defer store.Stop()

	// JSON mode should not create memory.db.
	dbPath := filepath.Join(dir, "memory.db")
	if _, err := os.Stat(dbPath); err == nil {
		t.Error("memory.db should not exist in JSON mode")
	}
}

func TestNewStoreWithMode_SQLite_MigratesLegacyJSON(t *testing.T) {
	dir := t.TempDir()

	// Create a legacy JSON file with entries.
	now := time.Now().UTC()
	entries := []Entry{
		{ID: "legacy-1", Content: "from json", Category: CategoryUserFact, CreatedAt: now, UpdatedAt: now, Strength: 1, AccessCount: 1},
		{ID: "legacy-2", Content: "also json", Category: CategoryProjectKnowledge, CreatedAt: now, UpdatedAt: now, Strength: 1, AccessCount: 1},
	}
	data, _ := json.MarshalIndent(entries, "", "  ")
	jsonPath := filepath.Join(dir, "memories.json")
	if err := os.WriteFile(jsonPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Create store in SQLite mode — should migrate.
	store, err := NewStoreWithMode(dir, StoreModeSQLite)
	if err != nil {
		t.Fatalf("NewStoreWithMode: %v", err)
	}
	defer store.Stop()

	// Verify entries are in memory.
	store.mu.RLock()
	count := len(store.entries)
	store.mu.RUnlock()
	if count != 2 {
		t.Errorf("expected 2 entries after migration, got %d", count)
	}

	// Verify legacy file was renamed.
	if _, err := os.Stat(jsonPath); err == nil {
		t.Error("legacy JSON file should have been renamed to .migrated")
	}
	if _, err := os.Stat(jsonPath + ".migrated"); os.IsNotExist(err) {
		t.Error("expected .migrated file")
	}

	// Verify entries are in SQLite.
	modified, _, _ := store.backend.Since(0)
	if len(modified) != 2 {
		t.Errorf("expected 2 entries in SQLite, got %d", len(modified))
	}
}

func TestNewStoreWithModeAndLegacyJSONSeedsCanonicalStore(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(t.TempDir(), "agent_memory.json")
	now := time.Now().UTC()
	entries := []Entry{{ID: "legacy-agent-1", Content: "MaClawSrv legacy agent memory", Category: CategoryUserFact, CreatedAt: now, UpdatedAt: now, Strength: 1}}
	data, _ := json.MarshalIndent(entries, "", "  ")
	if err := os.WriteFile(legacyPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := NewStoreWithModeAndLegacyJSON(dir, StoreModeJSON, legacyPath)
	if err != nil {
		t.Fatalf("NewStoreWithModeAndLegacyJSON: %v", err)
	}
	defer store.Stop()

	got := store.List(CategoryUserFact, "legacy agent")
	if len(got) != 1 || got[0].ID != "legacy-agent-1" {
		t.Fatalf("expected seeded legacy memory, got %+v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "memories.json")); err != nil {
		t.Fatalf("expected canonical memories.json to be seeded: %v", err)
	}
	if _, err := os.Stat(legacyPath); err != nil {
		t.Fatalf("legacy source should be left in place for safe migration: %v", err)
	}
}

func TestOpenDataDirStoreSeedsLegacyRootJSON(t *testing.T) {
	dataDir := t.TempDir()
	now := time.Now().UTC()
	entries := []Entry{{ID: "legacy-root-json", Content: "legacy root json memory", Category: CategoryUserFact, CreatedAt: now, UpdatedAt: now, Strength: 1}}
	data, _ := json.MarshalIndent(entries, "", "  ")
	if err := os.WriteFile(filepath.Join(dataDir, "memories.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := OpenDataDirStore(dataDir, StoreModeJSON)
	if err != nil {
		t.Fatalf("OpenDataDirStore: %v", err)
	}
	defer store.Stop()

	got := store.List(CategoryUserFact, "legacy root")
	if len(got) != 1 || got[0].ID != "legacy-root-json" {
		t.Fatalf("expected seeded legacy root memory, got %+v", got)
	}
	if _, err := os.Stat(filepath.Join(DataDirStoreDir(dataDir), "memories.json")); err != nil {
		t.Fatalf("expected canonical data-dir memories.json: %v", err)
	}
}

func TestOpenDataDirStoreCopiesLegacyRootSQLite(t *testing.T) {
	dataDir := t.TempDir()
	legacy, err := NewStoreWithMode(dataDir, StoreModeSQLite)
	if err != nil {
		t.Fatal(err)
	}
	if err := legacy.Save(Entry{ID: "legacy-root-sqlite", Content: "legacy root sqlite memory", Category: CategoryProjectKnowledge, Tags: []string{"sqlite"}}); err != nil {
		t.Fatalf("Save legacy: %v", err)
	}
	legacy.Stop()

	store, err := OpenDataDirStore(dataDir, StoreModeSQLite)
	if err != nil {
		t.Fatalf("OpenDataDirStore: %v", err)
	}
	defer store.Stop()

	got := store.List(CategoryProjectKnowledge, "legacy root sqlite")
	if len(got) != 1 || got[0].ID != "legacy-root-sqlite" {
		t.Fatalf("expected copied legacy sqlite memory, got %+v", got)
	}
	if _, err := os.Stat(filepath.Join(DataDirStoreDir(dataDir), "memory.db")); err != nil {
		t.Fatalf("expected canonical data-dir memory.db: %v", err)
	}
}

func TestNewStoreWithMode_Auto_DetectsExistingDB(t *testing.T) {
	dir := t.TempDir()

	// Create an empty DB file to trigger auto-detection.
	dbPath := filepath.Join(dir, "memory.db")
	// Create a real SQLite DB.
	b, err := NewSQLiteBackend(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	b.Close()

	// Auto mode should detect the DB and use SQLite.
	store, err := NewStoreWithMode(dir, StoreModeAuto)
	if err != nil {
		t.Fatalf("NewStoreWithMode: %v", err)
	}
	defer store.Stop()

	if store.backend == nil || !store.backend.SupportsSync() {
		t.Error("auto mode should have selected SQLite backend")
	}
}

func TestNewStoreWithMode_SQLite_CrossInstanceSync(t *testing.T) {
	dir := t.TempDir()

	// Create two stores sharing the same directory (same DB).
	store1, err := NewStoreWithMode(dir, StoreModeSQLite)
	if err != nil {
		t.Fatal(err)
	}
	defer store1.Stop()

	store2, err := NewStoreWithMode(dir, StoreModeSQLite)
	if err != nil {
		t.Fatal(err)
	}
	defer store2.Stop()

	// Store 1 writes via backend.
	now := time.Now().UTC()
	entry := Entry{
		ID: "cross-1", Content: "我叫张三", Category: CategoryUserFact,
		Strength: 1, AccessCount: 1, CreatedAt: now, UpdatedAt: now,
		OwnerID: "user-A",
	}
	if err := store1.backend.SaveEntry(&entry); err != nil {
		t.Fatal(err)
	}
	// Also add to store1's memory (simulating a normal Save flow).
	store1.mu.Lock()
	store1.entries = append(store1.entries, entry)
	store1.addToIndicesLocked(entry)
	store1.mu.Unlock()

	// Trigger store2's sync.
	store2.syncOnce()

	// Verify store2 has the entry.
	store2.mu.RLock()
	found := false
	for _, e := range store2.entries {
		if e.ID == "cross-1" && e.Content == "我叫张三" {
			found = true
			break
		}
	}
	store2.mu.RUnlock()

	if !found {
		t.Error("store2 did not sync entry from store1 — cross-instance sync failed")
	}

	// Verify BM25 search works on store2.
	store2.mu.RLock()
	scores := store2.bm25.score("张三")
	store2.mu.RUnlock()
	if scores["cross-1"] <= 0 {
		t.Error("BM25 search on store2 should find synced entry")
	}
}

func TestNewStoreWithMode_SQLite_SaveUpdateDeletePersist(t *testing.T) {
	dir := t.TempDir()

	store, err := NewStoreWithMode(dir, StoreModeSQLite)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(Entry{ID: "sqlite-store-1", Content: "SQLite store saves normal memory writes", Category: CategoryProjectKnowledge, Tags: []string{"sqlite"}}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := store.Update("sqlite-store-1", "SQLite store persists updates", CategoryProjectKnowledge, []string{"sqlite", "updated"}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	store.Stop()

	reloaded, err := NewStoreWithMode(dir, StoreModeSQLite)
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.Stop()

	entries := reloaded.List(CategoryProjectKnowledge, "updates")
	if len(entries) != 1 || entries[0].Content != "SQLite store persists updates" {
		t.Fatalf("expected updated entry after reload, got %#v", entries)
	}

	if err := reloaded.Delete("sqlite-store-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	reloaded.Stop()

	afterDelete, err := NewStoreWithMode(dir, StoreModeSQLite)
	if err != nil {
		t.Fatal(err)
	}
	defer afterDelete.Stop()
	if got := afterDelete.List(CategoryProjectKnowledge, "SQLite store"); len(got) != 0 {
		t.Fatalf("expected deleted entry to stay deleted after reload, got %#v", got)
	}
}

func TestNewStoreWithMode_SQLite_DerivedMetadataPersists(t *testing.T) {
	dir := t.TempDir()

	store, err := NewStoreWithMode(dir, StoreModeSQLite)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 5, 19, 10, 0, 0, 0, time.UTC)
	until := now.Add(time.Hour)
	entry := Entry{
		ID:          "schema-sqlite-1",
		Content:     "SQLite should persist derived memory metadata",
		Category:    CategoryProjectKnowledge,
		Tags:        []string{"sqlite", "derived"},
		Status:      StatusActive,
		CreatedAt:   now,
		UpdatedAt:   now,
		Strength:    1,
		EvidenceIDs: []string{"raw-1", "raw-2"},
		RelatedIDs:  []string{"raw-1", "raw-2"},
		DerivedKind: "schema:recurring",
		Boundary: &MemoryBoundary{
			OwnerID:     "owner-a",
			ProjectPath: `D:\workprj\alpha`,
			SourceScope: "conversation",
			Since:       &now,
			Until:       &until,
		},
	}
	if err := store.Save(entry); err != nil {
		t.Fatalf("Save: %v", err)
	}
	store.Stop()

	reloaded, err := NewStoreWithMode(dir, StoreModeSQLite)
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.Stop()

	entries := reloaded.SearchByMode("schema-sqlite-1", SearchDirect, CategoryProjectKnowledge, `D:\workprj\alpha`, 1, "owner-a")
	if len(entries) != 1 {
		t.Fatalf("expected derived entry after reload, got %+v", entries)
	}
	got := entries[0]
	if got.DerivedKind != "schema:recurring" {
		t.Fatalf("DerivedKind = %q", got.DerivedKind)
	}
	if len(got.EvidenceIDs) != 2 || got.EvidenceIDs[0] != "raw-1" || got.EvidenceIDs[1] != "raw-2" {
		t.Fatalf("EvidenceIDs not persisted: %+v", got.EvidenceIDs)
	}
	if len(got.RelatedIDs) != 2 || got.RelatedIDs[0] != "raw-1" || got.RelatedIDs[1] != "raw-2" {
		t.Fatalf("RelatedIDs not persisted: %+v", got.RelatedIDs)
	}
	if got.Boundary == nil || got.Boundary.OwnerID != "owner-a" || got.Boundary.ProjectPath != `D:\workprj\alpha` || got.Boundary.SourceScope != "conversation" {
		t.Fatalf("Boundary not persisted: %+v", got.Boundary)
	}
	if got.Boundary.Since == nil || !got.Boundary.Since.Equal(now) || got.Boundary.Until == nil || !got.Boundary.Until.Equal(until) {
		t.Fatalf("Boundary time window not persisted: %+v", got.Boundary)
	}
}
