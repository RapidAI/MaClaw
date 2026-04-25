package memory

import (
	"path/filepath"
	"strings"
	"testing"
)

func newDedupTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test_memory.json")
	s, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Stop() })
	return s
}

func TestSave_SubstringDedup_ContainedContent(t *testing.T) {
	s := newDedupTestStore(t)

	// Save a detailed entry first.
	_ = s.Save(Entry{
		Content:  "The project uses PostgreSQL 16 with pgvector extension for vector search and BM25 indexing",
		Category: CategoryProjectKnowledge,
		Tags:     []string{"postgresql", "pgvector"},
	})

	// Save a shorter entry that is a substring of the first.
	_ = s.Save(Entry{
		Content:  "The project uses PostgreSQL 16 with pgvector extension",
		Category: CategoryProjectKnowledge,
		Tags:     []string{"database"},
	})

	entries := s.List(CategoryProjectKnowledge, "")
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry after substring dedup, got %d", len(entries))
	}

	// Tags should be merged.
	hasPgvector := false
	hasDatabase := false
	for _, tag := range entries[0].Tags {
		if tag == "pgvector" {
			hasPgvector = true
		}
		if tag == "database" {
			hasDatabase = true
		}
	}
	if !hasPgvector || !hasDatabase {
		t.Errorf("expected merged tags, got: %v", entries[0].Tags)
	}
}

func TestSave_SubstringDedup_ContainingContent(t *testing.T) {
	s := newDedupTestStore(t)

	// Save a shorter entry first.
	_ = s.Save(Entry{
		Content:  "User prefers dark mode in all editors and terminals",
		Category: CategoryPreference,
	})

	// Save a longer entry that contains the first.
	_ = s.Save(Entry{
		Content:  "User prefers dark mode in all editors and terminals, especially VS Code and iTerm2",
		Category: CategoryPreference,
	})

	entries := s.List(CategoryPreference, "")
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry after substring dedup, got %d", len(entries))
	}
}

func TestSave_SubstringDedup_ShortContentNotDeduped(t *testing.T) {
	s := newDedupTestStore(t)

	// Short content (< minSubstringLen=20) should not trigger substring dedup.
	_ = s.Save(Entry{Content: "short content A", Category: CategoryProjectKnowledge})
	_ = s.Save(Entry{Content: "short content B", Category: CategoryProjectKnowledge})

	entries := s.List(CategoryProjectKnowledge, "")
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries for short content, got %d", len(entries))
	}
}

func TestSave_SubstringDedup_DifferentContentNotDeduped(t *testing.T) {
	s := newDedupTestStore(t)

	_ = s.Save(Entry{
		Content:  "The project uses PostgreSQL 16 for the main database backend",
		Category: CategoryProjectKnowledge,
	})
	_ = s.Save(Entry{
		Content:  "The deployment pipeline uses Docker and Kubernetes for orchestration",
		Category: CategoryProjectKnowledge,
	})

	entries := s.List(CategoryProjectKnowledge, "")
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries for different content, got %d", len(entries))
	}
}

func TestFindSubstringDuplicate_OnlyScansRecent50(t *testing.T) {
	s := newDedupTestStore(t)

	// Save 60 entries.
	for i := 0; i < 60; i++ {
		_ = s.Save(Entry{
			Content:  "unique entry number " + strings.Repeat("x", 30) + string(rune('A'+i%26)) + string(rune('a'+i/26)),
			Category: CategoryProjectKnowledge,
		})
	}

	// The first 10 entries are outside the recent-50 window.
	// A substring match against entry #0 should NOT be found.
	s.mu.Lock()
	idx := s.findSubstringDuplicate(s.entries[0].Content, "") // empty ownerID for single-user mode
	s.mu.Unlock()

	// It might still match because entry[0] is still in the slice,
	// but findSubstringDuplicate starts from len-50, so entry[0] at index 0
	// is outside the scan window (60-50=10, so scan starts at index 10).
	// Pass empty ownerID for single-user mode (backward compatible).
	if idx == 0 {
		t.Error("findSubstringDuplicate should not scan entries outside the recent-50 window")
	}
}
