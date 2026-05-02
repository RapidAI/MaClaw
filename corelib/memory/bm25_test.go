package memory

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestBM25_BasicScoring(t *testing.T) {
	idx := newBM25Index()
	entries := []Entry{
		{ID: "1", Content: "deploy_all.cmd deployment script failed; check path config"},
		{ID: "2", Content: "user prefers VS Code as editor"},
		{ID: "3", Content: "project uses Go and tokenizer library"},
		{ID: "4", Content: "previous deploy script permission issue was fixed"},
		{ID: "5", Content: "database connection pool size is 20"},
	}
	idx.rebuild(entries)

	scores := idx.score("deployment script problem")
	if scores == nil {
		t.Fatal("expected non-nil scores")
	}
	if scores["1"] <= 0 {
		t.Errorf("entry 1 (deploy failure) should have positive score, got %f", scores["1"])
	}
	if scores["4"] <= 0 {
		t.Errorf("entry 4 (deploy permission) should have positive score, got %f", scores["4"])
	}
	if scores["5"] > 0 {
		t.Errorf("entry 5 (database) should not match deploy query, got %f", scores["5"])
	}
}

func TestBM25_EmptyQuery(t *testing.T) {
	idx := newBM25Index()
	idx.rebuild([]Entry{{ID: "1", Content: "hello world"}})
	scores := idx.score("")
	if scores != nil {
		t.Errorf("empty query should return nil scores, got %v", scores)
	}
}

func TestBM25_EmptyIndex(t *testing.T) {
	idx := newBM25Index()
	scores := idx.score("hello")
	if scores != nil {
		t.Errorf("empty index should return nil scores, got %v", scores)
	}
}

func TestBM25_AddRemoveUpdate(t *testing.T) {
	idx := newBM25Index()
	e1 := Entry{ID: "1", Content: "Go programming"}
	e2 := Entry{ID: "2", Content: "Python data analysis"}

	idx.addEntry(e1)
	idx.addEntry(e2)

	scores := idx.score("Go programming")
	if scores["1"] <= 0 {
		t.Errorf("entry 1 should match Go programming")
	}

	idx.removeEntry("1")
	scores = idx.score("Go programming")
	if scores["1"] > 0 {
		t.Errorf("entry 1 should be removed")
	}

	e2.Content = "Go and Python mixed programming"
	idx.updateEntry(e2)
	scores = idx.score("Go programming")
	if scores["2"] <= 0 {
		t.Errorf("updated entry 2 should match Go programming")
	}
}

func TestBM25_TagsIndexed(t *testing.T) {
	idx := newBM25Index()
	e := Entry{ID: "1", Content: "configuration file notes", Tags: []string{"deployment", "config"}}
	idx.addEntry(e)

	scores := idx.score("deployment")
	if scores["1"] <= 0 {
		t.Errorf("tags should be indexed, entry should match deployment")
	}
}

func TestStore_RecallWithBM25(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "memory.json")

	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	entries := []Entry{
		{Content: "deploy_all.cmd deployment script failed", Category: CategoryProjectKnowledge, Tags: []string{"deploy"}},
		{Content: "user prefers dark theme", Category: CategoryPreference},
		{Content: "Go project uses tokenizer library", Category: CategoryProjectKnowledge, Tags: []string{"nlp"}},
		{Content: "previous deploy permission issue was fixed", Category: CategoryProjectKnowledge, Tags: []string{"deploy"}},
		{Content: "database connection pool size is 20", Category: CategoryProjectKnowledge},
	}
	for _, e := range entries {
		if err := store.Save(e); err != nil {
			t.Fatal(err)
		}
	}

	results := store.Recall("deployment script problem")
	if len(results) == 0 {
		t.Fatal("expected recall results")
	}

	found := false
	for _, r := range results[:min(3, len(results))] {
		if strings.Contains(r.Content, "deploy") || strings.Contains(r.Content, "deployment") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("top recall results should include deploy-related entries, got: %v", results)
	}
}

func TestBM25_IndexesCanonicalEntityTokens(t *testing.T) {
	idx := newBM25Index()
	idx.addEntry(Entry{
		ID:       "dirty-entity",
		Content:  "configuration note",
		Entities: []string{" Entity: Alpha Host ", " Relation: HAS-PORT ", " Entity: Port 2222 "},
	})

	scores := idx.score("alpha host")
	if scores["dirty-entity"] <= 0 {
		t.Fatalf("expected dirty entity token to be indexed for BM25, got scores=%v", scores)
	}
}
