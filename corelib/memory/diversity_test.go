package memory

import (
	"path/filepath"
	"testing"
)

func TestThemeAwareDiversityRerankPromotesDistinctThemes(t *testing.T) {
	candidates := []recallScored{
		{entry: Entry{ID: "a1"}, score: 10},
		{entry: Entry{ID: "a2"}, score: 9},
		{entry: Entry{ID: "a3"}, score: 8},
		{entry: Entry{ID: "b1"}, score: 7},
		{entry: Entry{ID: "b2"}, score: 6},
	}
	themes := []ThemeNode{
		{ID: "theme-a", EntryIDs: []string{"a1", "a2", "a3"}},
		{ID: "theme-b", EntryIDs: []string{"b1", "b2"}},
	}

	got := themeAwareDiversityRerank(candidates, themes, 2)
	if len(got) != len(candidates) {
		t.Fatalf("result length changed: %d", len(got))
	}
	if got[0].entry.ID != "a1" || got[1].entry.ID != "b1" {
		t.Fatalf("expected top representatives a1,b1, got %s,%s", got[0].entry.ID, got[1].entry.ID)
	}
}

func TestRecallDynamicHybridUsesThemeDiversity(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	entries := []Entry{
		{Content: "Go concurrency worker pool retry pattern alpha", Category: CategoryProjectKnowledge, Tags: []string{"go"}, Embedding: []float32{1, 0}, Status: StatusActive},
		{Content: "Go concurrency goroutine scheduler retry pattern beta", Category: CategoryProjectKnowledge, Tags: []string{"go"}, Embedding: []float32{0.99, 0.01}, Status: StatusActive},
		{Content: "Go concurrency channel backpressure retry pattern gamma", Category: CategoryProjectKnowledge, Tags: []string{"go"}, Embedding: []float32{0.98, 0.02}, Status: StatusActive},
		{Content: "PostgreSQL backup restore incident timeline", Category: CategoryProjectKnowledge, Tags: []string{"postgresql"}, Embedding: []float32{0, 1}, Status: StatusActive},
	}
	for _, entry := range entries {
		if err := store.Save(entry); err != nil {
			t.Fatal(err)
		}
	}

	store.mu.RLock()
	snapshot := make([]Entry, len(store.entries))
	copy(snapshot, store.entries)
	store.mu.RUnlock()
	store.ThemeManager().Rebuild(snapshot, nil)

	results := store.RecallDynamic("why compare go concurrency and postgresql backup over time", "", "")
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}
	if results[0].Tags[0] == results[1].Tags[0] {
		t.Fatalf("expected distinct themes near the top, got %q then %q", results[0].Content, results[1].Content)
	}
}
