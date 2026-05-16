package memory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestJSONFileBackend_LoadAll_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "memories.json")

	b, err := NewJSONFileBackend(path)
	if err != nil {
		t.Fatalf("NewJSONFileBackend: %v", err)
	}
	defer b.Close()

	entries, err := b.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestJSONFileBackend_LoadAll_WithData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "memories.json")

	// Write a test JSON file.
	data := `[{"id":"test-1","content":"hello","category":"user_fact","created_at":"2025-01-01T00:00:00Z","updated_at":"2025-01-01T00:00:00Z"}]`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	b, err := NewJSONFileBackend(path)
	if err != nil {
		t.Fatalf("NewJSONFileBackend: %v", err)
	}
	defer b.Close()

	entries, err := b.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].ID != "test-1" {
		t.Errorf("expected ID test-1, got %s", entries[0].ID)
	}
	if entries[0].Content != "hello" {
		t.Errorf("expected content hello, got %s", entries[0].Content)
	}
}

func TestJSONFileBackend_FlushAll(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "memories.json")

	b, err := NewJSONFileBackend(path)
	if err != nil {
		t.Fatalf("NewJSONFileBackend: %v", err)
	}
	defer b.Close()

	entries := []Entry{
		{ID: "e1", Content: "first", Category: CategoryUserFact},
		{ID: "e2", Content: "second", Category: CategoryProjectKnowledge},
	}

	if err := b.FlushAll(entries); err != nil {
		t.Fatalf("FlushAll: %v", err)
	}

	// Verify file was written.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty file")
	}

	// Reload and verify.
	b2, err := NewJSONFileBackend(path)
	if err != nil {
		t.Fatalf("NewJSONFileBackend (reload): %v", err)
	}
	defer b2.Close()

	loaded, err := b2.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll (reload): %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(loaded))
	}
}

func TestJSONFileBackend_SupportsSync(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "memories.json")

	b, err := NewJSONFileBackend(path)
	if err != nil {
		t.Fatalf("NewJSONFileBackend: %v", err)
	}
	defer b.Close()

	if b.SupportsSync() {
		t.Error("JSON backend should not support sync")
	}
}

func TestJSONFileBackend_Since_ReturnsNil(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "memories.json")

	b, err := NewJSONFileBackend(path)
	if err != nil {
		t.Fatalf("NewJSONFileBackend: %v", err)
	}
	defer b.Close()

	modified, deleted, err := b.Since(0)
	if err != nil {
		t.Fatalf("Since: %v", err)
	}
	if modified != nil || deleted != nil {
		t.Error("expected nil results from JSON backend Since()")
	}
}
