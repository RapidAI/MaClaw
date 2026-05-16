package memory

import (
	"path/filepath"
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
