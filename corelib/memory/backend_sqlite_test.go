package memory

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestSQLiteBackend(t *testing.T) *sqliteBackend {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "memory.db")
	b, err := NewSQLiteBackend(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteBackend: %v", err)
	}
	t.Cleanup(func() { b.Close() })
	return b
}

func TestSQLiteBackend_SaveAndLoad(t *testing.T) {
	b := newTestSQLiteBackend(t)

	now := time.Now().UTC().Truncate(time.Millisecond)
	entry := Entry{
		ID:          "test-1",
		Content:     "我叫张三",
		Category:    CategoryUserFact,
		Tags:        []string{"name", "identity"},
		Entities:    []string{"entity:张三"},
		Strength:    1.0,
		AccessCount: 1,
		Scope:       ScopeGlobal,
		OwnerID:     "feishu_ou_123",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := b.SaveEntry(&entry); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}

	if entry.Version == 0 {
		t.Error("expected non-zero version after save")
	}

	// Load all and verify.
	entries, err := b.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	got := entries[0]
	if got.ID != "test-1" {
		t.Errorf("ID: got %q, want %q", got.ID, "test-1")
	}
	if got.Content != "我叫张三" {
		t.Errorf("Content: got %q, want %q", got.Content, "我叫张三")
	}
	if got.Category != CategoryUserFact {
		t.Errorf("Category: got %q, want %q", got.Category, CategoryUserFact)
	}
	if got.OwnerID != "feishu_ou_123" {
		t.Errorf("OwnerID: got %q, want %q", got.OwnerID, "feishu_ou_123")
	}
	if len(got.Tags) != 2 || got.Tags[0] != "name" {
		t.Errorf("Tags: got %v", got.Tags)
	}
	if got.Version != entry.Version {
		t.Errorf("Version: got %d, want %d", got.Version, entry.Version)
	}
}

func TestSQLiteBackend_VersionIncrement(t *testing.T) {
	b := newTestSQLiteBackend(t)
	now := time.Now().UTC()

	e1 := Entry{ID: "e1", Content: "first", Category: CategoryUserFact, CreatedAt: now, UpdatedAt: now}
	e2 := Entry{ID: "e2", Content: "second", Category: CategoryUserFact, CreatedAt: now, UpdatedAt: now}

	if err := b.SaveEntry(&e1); err != nil {
		t.Fatal(err)
	}
	if err := b.SaveEntry(&e2); err != nil {
		t.Fatal(err)
	}

	if e2.Version <= e1.Version {
		t.Errorf("version should increase: e1=%d, e2=%d", e1.Version, e2.Version)
	}

	maxV, err := b.MaxVersion()
	if err != nil {
		t.Fatal(err)
	}
	if maxV != e2.Version {
		t.Errorf("MaxVersion: got %d, want %d", maxV, e2.Version)
	}
}

func TestSQLiteBackend_Since(t *testing.T) {
	b := newTestSQLiteBackend(t)
	now := time.Now().UTC()

	e1 := Entry{ID: "e1", Content: "first", Category: CategoryUserFact, CreatedAt: now, UpdatedAt: now}
	e2 := Entry{ID: "e2", Content: "second", Category: CategoryProjectKnowledge, CreatedAt: now, UpdatedAt: now}
	e3 := Entry{ID: "e3", Content: "third", Category: CategoryUserFact, CreatedAt: now, UpdatedAt: now}

	b.SaveEntry(&e1)
	checkpoint := e1.Version
	b.SaveEntry(&e2)
	b.SaveEntry(&e3)

	// Since(checkpoint) should return e2 and e3.
	modified, deleted, err := b.Since(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 0 {
		t.Errorf("expected 0 deleted, got %d", len(deleted))
	}
	if len(modified) != 2 {
		t.Fatalf("expected 2 modified, got %d", len(modified))
	}
	if modified[0].ID != "e2" || modified[1].ID != "e3" {
		t.Errorf("unexpected order: %s, %s", modified[0].ID, modified[1].ID)
	}
}

func TestSQLiteBackend_DeleteAndSince(t *testing.T) {
	b := newTestSQLiteBackend(t)
	now := time.Now().UTC()

	e1 := Entry{ID: "e1", Content: "to delete", Category: CategoryUserFact, CreatedAt: now, UpdatedAt: now}
	b.SaveEntry(&e1)
	checkpoint := e1.Version

	if err := b.DeleteEntry("e1"); err != nil {
		t.Fatal(err)
	}

	// LoadAll should not return deleted entries.
	entries, _ := b.LoadAll()
	for _, e := range entries {
		if e.ID == "e1" {
			t.Error("deleted entry should not appear in LoadAll")
		}
	}

	// Since should report the deletion.
	modified, deleted, err := b.Since(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	if len(modified) != 0 {
		t.Errorf("expected 0 modified, got %d", len(modified))
	}
	if len(deleted) != 1 || deleted[0] != "e1" {
		t.Errorf("expected deleted=[e1], got %v", deleted)
	}
}

func TestStoreUpdateEntriesByIDSQLiteBatchPersists(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	backend, err := NewSQLiteBackend(filepath.Join(dir, "memory.db"))
	if err != nil {
		t.Fatalf("NewSQLiteBackend: %v", err)
	}
	store.SetBackend(backend, SyncConfig{Enabled: false})
	t.Cleanup(store.Stop)

	if err := store.Save(Entry{ID: "batch-a", Content: "before a", Category: CategoryProjectKnowledge, Tags: []string{"a"}}); err != nil {
		t.Fatalf("save a: %v", err)
	}
	if err := store.Save(Entry{ID: "batch-b", Content: "before b", Category: CategoryProjectKnowledge, Tags: []string{"b"}}); err != nil {
		t.Fatalf("save b: %v", err)
	}
	entries := store.List("", "")
	for i := range entries {
		switch entries[i].ID {
		case "batch-a":
			entries[i].Content = "after a"
			entries[i].Tags = append(entries[i].Tags, "updated")
		case "batch-b":
			entries[i].Content = "after b"
			entries[i].Tags = append(entries[i].Tags, "updated")
		}
	}
	if err := store.UpdateEntriesByID(entries); err != nil {
		t.Fatalf("UpdateEntriesByID: %v", err)
	}
	loaded, err := backend.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	seen := map[string]string{}
	for _, entry := range loaded {
		seen[entry.ID] = entry.Content
		if entry.ID == "batch-a" || entry.ID == "batch-b" {
			if entry.Version == 0 {
				t.Fatalf("entry %s version was not updated", entry.ID)
			}
		}
	}
	if seen["batch-a"] != "after a" || seen["batch-b"] != "after b" {
		t.Fatalf("batch update not persisted: %#v", seen)
	}
}

func TestStoreUpsertEntriesByIDSQLiteCreatesAndUpdates(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	backend, err := NewSQLiteBackend(filepath.Join(dir, "memory.db"))
	if err != nil {
		t.Fatalf("NewSQLiteBackend: %v", err)
	}
	store.SetBackend(backend, SyncConfig{Enabled: false})
	t.Cleanup(store.Stop)

	if err := store.Save(Entry{ID: "source", Content: "source before", Category: CategoryProjectKnowledge, Tags: []string{"source"}}); err != nil {
		t.Fatalf("save source: %v", err)
	}
	source := store.SearchDirectByID("source")[0]
	source.Content = "source after"
	source.Tags = append(source.Tags, "transitioned")
	review := Entry{ID: "review", Content: "new review", Category: CategoryProjectKnowledge, Tags: []string{"review"}}
	if err := store.UpsertEntriesByID([]Entry{review, source}); err != nil {
		t.Fatalf("UpsertEntriesByID: %v", err)
	}
	loaded, err := backend.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	seen := map[string]string{}
	for _, entry := range loaded {
		seen[entry.ID] = entry.Content
	}
	if seen["source"] != "source after" || seen["review"] != "new review" {
		t.Fatalf("upsert batch not persisted: %#v", seen)
	}
}

func TestStoreUpsertEntriesByIDSQLitePreservesInputVersionOrder(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	backend, err := NewSQLiteBackend(filepath.Join(dir, "memory.db"))
	if err != nil {
		t.Fatalf("NewSQLiteBackend: %v", err)
	}
	store.SetBackend(backend, SyncConfig{Enabled: false})
	t.Cleanup(store.Stop)

	source := Entry{ID: "source-order", Content: "source transition", Category: CategoryProjectKnowledge, Tags: []string{"source"}}
	review := Entry{ID: "review-order", Content: "review record", Category: CategoryProjectKnowledge, Tags: []string{"review"}}
	if err := store.UpsertEntriesByID([]Entry{review, source}); err != nil {
		t.Fatalf("UpsertEntriesByID: %v", err)
	}

	modified, deleted, err := backend.Since(0)
	if err != nil {
		t.Fatalf("Since: %v", err)
	}
	if len(deleted) != 0 {
		t.Fatalf("unexpected deleted entries: %v", deleted)
	}
	if len(modified) != 2 {
		t.Fatalf("expected two modified entries, got %d: %#v", len(modified), modified)
	}
	if modified[0].ID != "review-order" || modified[1].ID != "source-order" {
		t.Fatalf("batch version order should match input order, got %s then %s", modified[0].ID, modified[1].ID)
	}
	if modified[0].Version >= modified[1].Version {
		t.Fatalf("versions should increase in input order, got %d then %d", modified[0].Version, modified[1].Version)
	}
}

func TestStoreUpdateEntriesByIDRefreshesGraphAndPersistsRelatedEdges(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	backend, err := NewSQLiteBackend(filepath.Join(dir, "memory.db"))
	if err != nil {
		t.Fatalf("NewSQLiteBackend: %v", err)
	}
	store.SetBackend(backend, SyncConfig{Enabled: false})
	t.Cleanup(store.Stop)

	now := time.Now().UTC()
	left := Entry{ID: "graph-left", Content: "left graph evidence", Category: CategoryProjectKnowledge, CreatedAt: now, UpdatedAt: now, AccessCount: 1, Strength: 1}
	right := Entry{ID: "graph-right", Content: "right graph evidence", Category: CategoryProjectKnowledge, CreatedAt: now, UpdatedAt: now, AccessCount: 1, Strength: 1}
	if err := store.Save(left); err != nil {
		t.Fatalf("save left: %v", err)
	}
	if err := store.Save(right); err != nil {
		t.Fatalf("save right: %v", err)
	}

	entries := store.List("", "")
	for i := range entries {
		if entries[i].ID == "graph-left" {
			entries[i].RelatedIDs = []string{"graph-right"}
			entries[i].RelatedEdges = []RelatedEdge{{ID: "graph-right", Strength: 0.7, LinkType: LinkReferences, UpdatedAt: now.Add(time.Minute)}}
		}
	}
	if err := store.UpdateEntriesByID(entries); err != nil {
		t.Fatalf("UpdateEntriesByID: %v", err)
	}

	neighbors := store.graph.neighborsTypedOf("graph-left")
	if edge, ok := neighbors["graph-right"]; !ok || edge.LinkType != LinkReferences || edge.Strength != 0.7 {
		t.Fatalf("batch update did not refresh graph edges: %#v", neighbors)
	}
	loaded, err := backend.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	for _, entry := range loaded {
		if entry.ID == "graph-left" {
			if len(entry.RelatedEdges) != 1 || entry.RelatedEdges[0].ID != "graph-right" || entry.RelatedEdges[0].LinkType != LinkReferences {
				t.Fatalf("related edge was not persisted: %+v", entry.RelatedEdges)
			}
			return
		}
	}
	t.Fatal("graph-left not loaded from backend")
}

func TestStoreUpdateEntriesByIDPersistsCompactFormAndEmbedding(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	backend, err := NewSQLiteBackend(filepath.Join(dir, "memory.db"))
	if err != nil {
		t.Fatalf("NewSQLiteBackend: %v", err)
	}
	store.SetBackend(backend, SyncConfig{Enabled: false})
	t.Cleanup(store.Stop)

	if err := store.Save(Entry{ID: "maintenance-fields", Content: "long memory content for compact and embedding maintenance", Category: CategoryProjectKnowledge}); err != nil {
		t.Fatalf("save entry: %v", err)
	}
	entry := store.SearchDirectByID("maintenance-fields")[0]
	entry.CompactForm = "compact maintenance field"
	entry.Embedding = []float32{0.25, 0.5, 0.75, 1}
	if err := store.UpdateEntriesByID([]Entry{entry}); err != nil {
		t.Fatalf("UpdateEntriesByID: %v", err)
	}

	loaded, err := backend.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(loaded) != 1 || loaded[0].CompactForm != "compact maintenance field" || len(loaded[0].Embedding) != 4 || loaded[0].Embedding[2] != 0.75 {
		t.Fatalf("maintenance fields not persisted: %+v", loaded)
	}
	if scores := store.vecIndex.score([]float32{0.25, 0.5, 0.75, 1}); scores["maintenance-fields"] == 0 {
		t.Fatalf("embedding backfill should refresh vector index, scores=%v", scores)
	}
}

func TestLifecycleMutationsPersistThroughSQLiteBatch(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	backend, err := NewSQLiteBackend(filepath.Join(dir, "memory.db"))
	if err != nil {
		t.Fatalf("NewSQLiteBackend: %v", err)
	}
	store.SetBackend(backend, SyncConfig{Enabled: false})
	t.Cleanup(store.Stop)

	entry := Entry{ID: "lifecycle-batch", Content: "lifecycle transition target", Category: CategoryProjectKnowledge, Status: StatusActive}
	if err := store.Save(entry); err != nil {
		t.Fatalf("save entry: %v", err)
	}
	if err := store.PinEntry("lifecycle-batch"); err != nil {
		t.Fatalf("PinEntry: %v", err)
	}
	loaded, err := backend.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll pinned: %v", err)
	}
	if len(loaded) != 1 || !loaded[0].Pinned {
		t.Fatalf("pin state not persisted: %+v", loaded)
	}
	if _, err := store.SupersedeEntryByID("lifecycle-batch", time.Now().UTC()); err != nil {
		t.Fatalf("SupersedeEntryByID: %v", err)
	}
	loaded, err = backend.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll superseded: %v", err)
	}
	if len(loaded) != 1 || loaded[0].Status != StatusSuperseded || !loaded[0].Stale || loaded[0].InvalidAt == nil {
		t.Fatalf("supersede state not persisted: %+v", loaded)
	}
	if got := store.FindByEntity("lifecycle"); len(got) != 0 {
		t.Fatalf("superseded entry should be absent from active entity lookup, got %+v", got)
	}
}

func TestTouchAccessPersistsThroughSQLiteBatch(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	backend := newTestSQLiteBackend(t)
	store.SetBackend(backend, SyncConfig{Enabled: false})
	t.Cleanup(store.Stop)

	if err := store.Save(Entry{ID: "touch-batch", Content: "touch access target", Category: CategoryProjectKnowledge, AccessCount: 1, Strength: 1}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	store.TouchAccess([]string{"touch-batch"})
	loaded, err := backend.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(loaded) != 1 || loaded[0].AccessCount != 2 || loaded[0].Strength <= 1 {
		t.Fatalf("touch access not persisted through batch path: %+v", loaded)
	}
}

func TestSQLiteBackendUpdateEntriesRollsBackOnFailure(t *testing.T) {
	b := newTestSQLiteBackend(t)
	if _, err := b.db.Exec(`CREATE TRIGGER fail_memory_batch BEFORE INSERT ON memories WHEN NEW.id = 'fail-batch' BEGIN SELECT RAISE(FAIL, 'forced batch failure'); END;`); err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}
	now := time.Now().UTC()
	first := Entry{ID: "first-batch", Content: "first should rollback", Category: CategoryProjectKnowledge, CreatedAt: now, UpdatedAt: now, AccessCount: 1, Strength: 1}
	second := Entry{ID: "fail-batch", Content: "force rollback", Category: CategoryProjectKnowledge, CreatedAt: now, UpdatedAt: now, AccessCount: 1, Strength: 1}
	if err := b.UpdateEntries([]*Entry{&first, &second}); err == nil {
		t.Fatal("expected UpdateEntries failure")
	}
	loaded, err := b.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(loaded) != 0 {
		t.Fatalf("failed batch should not persist partial entries: %#v", loaded)
	}
	version, err := b.MaxVersion()
	if err != nil {
		t.Fatalf("MaxVersion: %v", err)
	}
	if version != 0 {
		t.Fatalf("failed batch should rollback version increments, got %d", version)
	}
}

func TestSQLiteBackendUpdateEntriesAndDeleteIDsRollsBackOnDeleteFailure(t *testing.T) {
	b := newTestSQLiteBackend(t)
	now := time.Now().UTC()
	keep := Entry{ID: "keep-batch", Content: "keep original", Category: CategoryProjectKnowledge, CreatedAt: now, UpdatedAt: now, AccessCount: 1, Strength: 1}
	remove := Entry{ID: "delete-fail", Content: "delete original", Category: CategoryProjectKnowledge, CreatedAt: now, UpdatedAt: now, AccessCount: 1, Strength: 1}
	if err := b.SaveEntry(&keep); err != nil {
		t.Fatalf("save keep: %v", err)
	}
	if err := b.SaveEntry(&remove); err != nil {
		t.Fatalf("save remove: %v", err)
	}
	versionBefore, err := b.MaxVersion()
	if err != nil {
		t.Fatalf("MaxVersion before: %v", err)
	}
	if _, err := b.db.Exec(`CREATE TRIGGER fail_update_delete_batch BEFORE UPDATE OF deleted_at ON memories WHEN NEW.id = 'delete-fail' AND NEW.deleted_at IS NOT NULL BEGIN SELECT RAISE(FAIL, 'forced delete failure'); END;`); err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}

	keep.Content = "keep updated should rollback"
	if err := b.UpdateEntriesAndDeleteIDs([]*Entry{&keep}, []string{"delete-fail"}); err == nil {
		t.Fatal("expected UpdateEntriesAndDeleteIDs failure")
	}
	loaded, err := b.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	seen := map[string]Entry{}
	for _, entry := range loaded {
		seen[entry.ID] = entry
	}
	if seen["keep-batch"].Content != "keep original" {
		t.Fatalf("kept entry update should rollback, got %+v", seen["keep-batch"])
	}
	if seen["delete-fail"].Content != "delete original" {
		t.Fatalf("deleted entry should still be active after rollback, got %+v", seen["delete-fail"])
	}
	versionAfter, err := b.MaxVersion()
	if err != nil {
		t.Fatalf("MaxVersion after: %v", err)
	}
	if versionAfter != versionBefore {
		t.Fatalf("failed update/delete batch should rollback version increments, before=%d after=%d", versionBefore, versionAfter)
	}
}

func TestSQLiteBackend_UpdateEntry(t *testing.T) {
	b := newTestSQLiteBackend(t)
	now := time.Now().UTC()

	e := Entry{ID: "e1", Content: "original", Category: CategoryUserFact, CreatedAt: now, UpdatedAt: now}
	b.SaveEntry(&e)
	v1 := e.Version

	e.Content = "updated"
	e.UpdatedAt = now.Add(time.Second)
	if err := b.UpdateEntry(&e); err != nil {
		t.Fatal(err)
	}

	if e.Version <= v1 {
		t.Errorf("version should increase on update: v1=%d, v2=%d", v1, e.Version)
	}

	entries, _ := b.LoadAll()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Content != "updated" {
		t.Errorf("content not updated: got %q", entries[0].Content)
	}
}

func TestSQLiteBackend_EmbeddingRoundTrip(t *testing.T) {
	b := newTestSQLiteBackend(t)
	now := time.Now().UTC()

	vec := make([]float32, 768)
	for i := range vec {
		vec[i] = float32(i) * 0.001
	}

	e := Entry{
		ID: "emb-1", Content: "with embedding", Category: CategoryProjectKnowledge,
		Embedding: vec, CreatedAt: now, UpdatedAt: now,
	}
	b.SaveEntry(&e)

	entries, _ := b.LoadAll()
	if len(entries) != 1 {
		t.Fatal("expected 1 entry")
	}
	if len(entries[0].Embedding) != 768 {
		t.Fatalf("embedding length: got %d, want 768", len(entries[0].Embedding))
	}
	// Check a few values.
	if entries[0].Embedding[0] != 0.0 {
		t.Errorf("embedding[0]: got %f, want 0.0", entries[0].Embedding[0])
	}
	if entries[0].Embedding[100] != 0.1 {
		t.Errorf("embedding[100]: got %f, want 0.1", entries[0].Embedding[100])
	}
}

func TestSQLiteBackend_StatusRoundTrip(t *testing.T) {
	b := newTestSQLiteBackend(t)
	now := time.Now().UTC()

	e := Entry{
		ID: "s1", Content: "dormant entry", Category: CategoryUserFact,
		Status: StatusDormant, CreatedAt: now, UpdatedAt: now,
	}
	b.SaveEntry(&e)

	entries, _ := b.LoadAll()
	if len(entries) != 1 {
		t.Fatal("expected 1 entry")
	}
	if entries[0].Status != StatusDormant {
		t.Errorf("Status: got %q, want %q", entries[0].Status, StatusDormant)
	}
}

func TestSQLiteBackend_ExtraFieldsRoundTrip(t *testing.T) {
	b := newTestSQLiteBackend(t)
	now := time.Now().UTC()

	e := Entry{
		ID: "ex1", Content: "with extras", Category: CategoryProjectKnowledge,
		RelatedIDs:   []string{"other-1", "other-2"},
		RelatedEdges: []RelatedEdge{{ID: "other-1", Strength: 0.8, LinkType: LinkReferences}},
		Versions:     []VersionSnapshot{{Content: "old content", Timestamp: now.Add(-time.Hour)}},
		CreatedAt:    now, UpdatedAt: now,
	}
	b.SaveEntry(&e)

	entries, _ := b.LoadAll()
	if len(entries) != 1 {
		t.Fatal("expected 1 entry")
	}
	got := entries[0]
	if len(got.RelatedIDs) != 2 {
		t.Errorf("RelatedIDs: got %v", got.RelatedIDs)
	}
	if len(got.RelatedEdges) != 1 || got.RelatedEdges[0].LinkType != LinkReferences {
		t.Errorf("RelatedEdges: got %v", got.RelatedEdges)
	}
	if len(got.Versions) != 1 {
		t.Errorf("Versions: got %v", got.Versions)
	}
}

func TestSQLiteBackend_SupportsSync(t *testing.T) {
	b := newTestSQLiteBackend(t)
	if !b.SupportsSync() {
		t.Error("SQLite backend should support sync")
	}
}

func TestSQLiteBackend_MultipleInstances_SharedDB(t *testing.T) {
	// Simulate two instances sharing the same DB file.
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "shared.db")

	b1, err := NewSQLiteBackend(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer b1.Close()

	b2, err := NewSQLiteBackend(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer b2.Close()

	now := time.Now().UTC()

	// Instance 1 writes.
	e := Entry{ID: "from-b1", Content: "written by instance 1", Category: CategoryUserFact, CreatedAt: now, UpdatedAt: now}
	if err := b1.SaveEntry(&e); err != nil {
		t.Fatal(err)
	}

	// Instance 2 reads via Since(0).
	modified, _, err := b2.Since(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(modified) != 1 {
		t.Fatalf("instance 2 should see 1 entry, got %d", len(modified))
	}
	if modified[0].Content != "written by instance 1" {
		t.Errorf("content mismatch: %q", modified[0].Content)
	}
}

func TestSQLiteBackend_FTSSearchCJKFallback(t *testing.T) {
	b := newTestSQLiteBackend(t)
	now := time.Now().UTC()

	entry := Entry{
		ID: "fts-cjk", Content: "证据导航面板可以打开最近产物来源并回查全文",
		Category: CategoryTaskArtifact, Tags: []string{"证据导航"}, CreatedAt: now, UpdatedAt: now,
	}
	if err := b.SaveEntry(&entry); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}

	ids, err := b.SearchTextIDs("证据导航", 10)
	if err != nil {
		t.Fatalf("SearchTextIDs: %v", err)
	}
	if len(ids) != 1 || ids[0] != "fts-cjk" {
		t.Fatalf("expected fts-cjk hit, got %v", ids)
	}

	if err := b.DeleteEntry("fts-cjk"); err != nil {
		t.Fatalf("DeleteEntry: %v", err)
	}
	ids, err = b.SearchTextIDs("证据导航", 10)
	if err != nil {
		t.Fatalf("SearchTextIDs after delete: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("deleted entry should be removed from FTS index, got %v", ids)
	}
}

func TestSQLiteBackendFTSStaysAlignedWithBatchWrites(t *testing.T) {
	b := newTestSQLiteBackend(t)
	now := time.Now().UTC()
	entry := Entry{ID: "fts-batch", Content: "crimsontext batch evidence", Category: CategoryProjectKnowledge, CreatedAt: now, UpdatedAt: now, AccessCount: 1, Strength: 1}
	if err := b.SaveEntry(&entry); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}
	var ftsCount int
	if err := b.db.QueryRow(`SELECT COUNT(*) FROM memories_fts WHERE id = ?`, "fts-batch").Scan(&ftsCount); err != nil {
		t.Fatalf("count fts rows: %v", err)
	}
	if ftsCount != 1 {
		t.Fatalf("SaveEntry should create one FTS row, got %d", ftsCount)
	}
	entry.Content = "azureword batch evidence"
	entry.UpdatedAt = now.Add(time.Minute)
	if err := b.UpdateEntries([]*Entry{&entry}); err != nil {
		t.Fatalf("UpdateEntries: %v", err)
	}
	ids, err := b.SearchTextIDs("azureword", 10)
	if err != nil {
		t.Fatalf("SearchTextIDs updated: %v", err)
	}
	if len(ids) != 1 || ids[0] != "fts-batch" {
		t.Fatalf("updated FTS row missing, got %v", ids)
	}
	ids, err = b.SearchTextIDs("crimsontext", 10)
	if err != nil {
		t.Fatalf("SearchTextIDs old: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("old FTS text should be removed after batch update, got %v", ids)
	}
	if err := b.DeleteEntry("fts-batch"); err != nil {
		t.Fatalf("DeleteEntry: %v", err)
	}
	if err := b.db.QueryRow(`SELECT COUNT(*) FROM memories_fts WHERE id = ?`, "fts-batch").Scan(&ftsCount); err != nil {
		t.Fatalf("count fts rows after delete: %v", err)
	}
	if ftsCount != 0 {
		t.Fatalf("DeleteEntry should remove FTS row, got %d", ftsCount)
	}
}

func TestSQLiteBackend_FilteredFTSSearch(t *testing.T) {
	b := newTestSQLiteBackend(t)
	now := time.Now().UTC()
	entries := []Entry{
		{ID: "filtered-keep", Content: "evidence navigation read_file source", Category: CategoryProjectKnowledge, OwnerID: "owner-a", Tags: []string{"D:/workprj/project-a"}, SourceURL: "file://D:/workprj/project-a/artifact.md", CreatedAt: now.Add(-time.Hour), UpdatedAt: now},
		{ID: "filtered-owner", Content: "evidence navigation read_file source", Category: CategoryProjectKnowledge, OwnerID: "owner-b", Tags: []string{"D:/workprj/project-a"}, SourceURL: "file://D:/workprj/project-a/artifact.md", CreatedAt: now.Add(-time.Hour), UpdatedAt: now},
		{ID: "filtered-category", Content: "evidence navigation read_file source", Category: CategoryInstruction, OwnerID: "owner-a", Tags: []string{"D:/workprj/project-a"}, SourceURL: "file://D:/workprj/project-a/artifact.md", CreatedAt: now.Add(-time.Hour), UpdatedAt: now},
		{ID: "filtered-project", Content: "evidence navigation read_file source", Category: CategoryProjectKnowledge, OwnerID: "owner-a", Tags: []string{"D:/workprj/project-b"}, SourceURL: "file://D:/workprj/project-b/artifact.md", CreatedAt: now.Add(-time.Hour), UpdatedAt: now},
		{ID: "filtered-old", Content: "evidence navigation read_file source", Category: CategoryProjectKnowledge, OwnerID: "owner-a", Tags: []string{"D:/workprj/project-a"}, SourceURL: "file://D:/workprj/project-a/artifact.md", CreatedAt: now.Add(-48 * time.Hour), UpdatedAt: now},
	}
	for i := range entries {
		if err := b.SaveEntry(&entries[i]); err != nil {
			t.Fatalf("SaveEntry %s: %v", entries[i].ID, err)
		}
	}
	ids, err := b.SearchTextIDsFiltered("evidence navigation", sqliteTextFilter{OwnerID: "owner-a", Category: CategoryProjectKnowledge, ProjectPath: "D:/workprj/project-a", Since: now.Add(-24 * time.Hour)}, 10)
	if err != nil {
		t.Fatalf("SearchTextIDsFiltered: %v", err)
	}
	if len(ids) != 1 || ids[0] != "filtered-keep" {
		t.Fatalf("expected only filtered-keep, got %v", ids)
	}
}

func TestSQLiteFTSQueryQuotesTokens(t *testing.T) {
	query := sqliteFTSQuery(`alpha "quoted" evidence navigation`)
	if !strings.Contains(query, `"alpha ""quoted"" evidence navigation"`) || !strings.Contains(query, `"navigation"`) {
		t.Fatalf("unexpected fts query: %s", query)
	}
}
