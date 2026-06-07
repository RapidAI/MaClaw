package memory

import (
	"crypto/rand"
	"path/filepath"
	"testing"
	"time"
)

func TestRecallExhaustive_EmptyStore(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "memories.json")
	store, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Stop()

	result := store.RecallExhaustive("test query", "", "")
	if result == nil {
		t.Fatal("expected non-nil ExhaustiveResult")
	}
	if len(result.Entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(result.Entries))
	}
	if result.Truncated {
		t.Error("expected Truncated=false for empty store")
	}
	if result.TotalMatching != 0 {
		t.Errorf("expected TotalMatching=0, got %d", result.TotalMatching)
	}
}

func TestRecallExhaustive_ReturnsMatchingEntries(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "memories.json")
	store, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Stop()

	// Save a few entries with known content.
	for i := 0; i < 5; i++ {
		entry := Entry{
			Content:   "golang concurrency patterns and goroutines",
			Category:  CategoryProjectKnowledge,
			Tags:      []string{"golang", "concurrency"},
			CreatedAt: time.Now(),
		}
		if err := store.Save(entry); err != nil && i == 0 {
			// Only first save should succeed; rest are deduplicated.
			t.Fatalf("Save: %v", err)
		}
	}

	// Save a distinct entry.
	if err := store.Save(Entry{
		Content:   "rust memory safety without garbage collection",
		Category:  CategoryProjectKnowledge,
		Tags:      []string{"rust", "memory-safety"},
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("Save distinct: %v", err)
	}

	result := store.RecallExhaustive("golang concurrency", CategoryProjectKnowledge, "")
	if result == nil {
		t.Fatal("expected non-nil ExhaustiveResult")
	}
	// At least the golang entry should match.
	if len(result.Entries) == 0 {
		t.Error("expected at least 1 matching entry")
	}
	if result.Truncated {
		t.Error("expected Truncated=false for small result set")
	}
}

func TestRecallExhaustive_RespectsOwnerIDIsolation(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "memories.json")
	store, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Stop()

	// Save entry with specific owner.
	if err := store.SaveForUser(Entry{
		Content:   "secret project alpha details for user A",
		Category:  CategoryProjectKnowledge,
		Tags:      []string{"alpha", "secret"},
		CreatedAt: time.Now(),
	}, "user-a"); err != nil {
		t.Fatalf("Save for user-a: %v", err)
	}

	// Save entry with different owner.
	if err := store.SaveForUser(Entry{
		Content:   "secret project beta details for user B",
		Category:  CategoryProjectKnowledge,
		Tags:      []string{"beta", "secret"},
		CreatedAt: time.Now(),
	}, "user-b"); err != nil {
		t.Fatalf("Save for user-b: %v", err)
	}

	// Recall as user-a should not see user-b's entries.
	result := store.RecallExhaustive("secret project", CategoryProjectKnowledge, "", "user-a")
	if result == nil {
		t.Fatal("expected non-nil ExhaustiveResult")
	}
	for _, e := range result.Entries {
		if e.OwnerID == "user-b" {
			t.Error("user-a should not see user-b's entries")
		}
	}
}

func TestRecallExhaustive_RespectsCategoryFilter(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "memories.json")
	store, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Stop()

	// Save entries in different categories.
	if err := store.Save(Entry{
		Content:   "kubernetes deployment strategies and rolling updates",
		Category:  CategoryProjectKnowledge,
		Tags:      []string{"kubernetes", "deployment"},
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("Save project: %v", err)
	}

	if err := store.Save(Entry{
		Content:   "user prefers dark mode and vim keybindings kubernetes",
		Category:  "preference",
		Tags:      []string{"preference", "editor"},
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("Save preference: %v", err)
	}

	// Recall only project_knowledge category.
	result := store.RecallExhaustive("kubernetes", CategoryProjectKnowledge, "")
	if result == nil {
		t.Fatal("expected non-nil ExhaustiveResult")
	}
	for _, e := range result.Entries {
		if e.Category != CategoryProjectKnowledge {
			t.Errorf("expected only project_knowledge entries, got category=%s", e.Category)
		}
	}
}

func TestRecallExhaustive_EntryCap(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "memories.json")
	store, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Stop()

	// Create more than 100 entries that will match the query.
	for i := 0; i < 120; i++ {
		entry := Entry{
			Content:   "golang microservice architecture pattern " + randomSuffix(),
			Category:  CategoryProjectKnowledge,
			Tags:      []string{"golang", "microservice"},
			CreatedAt: time.Now(),
		}
		_ = store.Save(entry)
	}

	result := store.RecallExhaustive("golang microservice", CategoryProjectKnowledge, "")
	if result == nil {
		t.Fatal("expected non-nil ExhaustiveResult")
	}
	if len(result.Entries) > exhaustiveMaxEntries {
		t.Errorf("expected at most %d entries, got %d", exhaustiveMaxEntries, len(result.Entries))
	}
}

// randomSuffix generates a short unique suffix to avoid content dedup.
func randomSuffix() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b) // best-effort random
	return time.Now().Format("150405.000000000")
}
