package memory

import (
	"path/filepath"
	"strings"
	"testing"
)

func newCtxTestStore(t *testing.T) *Store {
	t.Helper()
	tmpDir := t.TempDir()
	memPath := filepath.Join(tmpDir, "memories.json")
	ms, err := NewStore(memPath)
	if err != nil {
		t.Fatalf("failed to create memory store: %v", err)
	}
	t.Cleanup(func() { ms.Stop() })
	return ms
}

func TestSaveWithContext_EnrichesTagsFromContext(t *testing.T) {
	ms := newCtxTestStore(t)

	entry := Entry{
		Content:  "SSH server info: host=api.rapidai.tech, port=33, user=root, GPU=4090",
		Category: CategoryProjectKnowledge,
	}
	contextHint := "user said: connect to 4090server and check GPU usage"

	err := ms.SaveWithContext(entry, contextHint)
	if err != nil {
		t.Fatalf("SaveWithContext failed: %v", err)
	}

	entries := ms.List(CategoryProjectKnowledge, "")
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	// Tags should include entities from both content AND context.
	if len(entries[0].Tags) == 0 {
		t.Error("expected non-empty tags after context enrichment")
	}
	t.Logf("tags after context enrichment: %v", entries[0].Tags)
}

func TestSaveWithContext_EmptyContextBehavesLikeSave(t *testing.T) {
	ms := newCtxTestStore(t)

	entry := Entry{
		Content:  "user preference: uses vim editor",
		Category: CategoryPreference,
		Tags:     []string{"editor"},
	}

	err := ms.SaveWithContext(entry, "")
	if err != nil {
		t.Fatalf("SaveWithContext with empty context failed: %v", err)
	}

	entries := ms.List(CategoryPreference, "")
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	hasEditor := false
	for _, tag := range entries[0].Tags {
		if tag == "editor" {
			hasEditor = true
		}
	}
	if !hasEditor {
		t.Errorf("expected 'editor' tag to be preserved, got: %v", entries[0].Tags)
	}
}

func TestSave_DelegatesToSaveWithContext(t *testing.T) {
	ms := newCtxTestStore(t)

	entry := Entry{
		Content:  "test content for delegation",
		Category: CategoryProjectKnowledge,
	}
	err := ms.Save(entry)
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	entries := ms.List(CategoryProjectKnowledge, "")
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
}

func TestTagExactMatchBoost_ExactMatch(t *testing.T) {
	entry := Entry{
		Tags: []string{"4090server", "ssh", "api.rapidai.tech"},
	}

	boost := tagExactMatchBoost(entry, []string{"4090server"})
	if boost < 4.0 {
		t.Errorf("expected significant boost for exact tag match, got %.1f", boost)
	}
}

func TestTagExactMatchBoost_NoMatch(t *testing.T) {
	entry := Entry{
		Tags: []string{"ssh", "api.rapidai.tech"},
	}

	boost := tagExactMatchBoost(entry, []string{"4090server"})
	if boost != 0 {
		t.Errorf("expected 0 boost for no match, got %.1f", boost)
	}
}

func TestTagExactMatchBoost_CaseInsensitive(t *testing.T) {
	entry := Entry{
		Tags: []string{"SSH", "Api.RapidAI.Tech"},
	}

	boost := tagExactMatchBoost(entry, []string{"ssh"})
	if boost < 4.0 {
		t.Errorf("expected boost for case-insensitive match, got %.1f", boost)
	}
}

func TestTagExactMatchBoost_Capped(t *testing.T) {
	entry := Entry{
		Tags: []string{"tag1", "tag2", "tag3", "tag4", "tag5"},
	}

	boost := tagExactMatchBoost(entry, []string{"tag1", "tag2", "tag3", "tag4", "tag5"})
	if boost > 15.0 {
		t.Errorf("expected boost to be capped at 15.0, got %.1f", boost)
	}
}

func TestTagExactMatchBoost_EmptyInputs(t *testing.T) {
	if boost := tagExactMatchBoost(Entry{}, []string{"test"}); boost != 0 {
		t.Errorf("expected 0 for entry with no tags, got %.1f", boost)
	}
	if boost := tagExactMatchBoost(Entry{Tags: []string{"test"}}, nil); boost != 0 {
		t.Errorf("expected 0 for empty entities, got %.1f", boost)
	}
}

func TestRecallDynamic_TagExactMatchBoostsRanking(t *testing.T) {
	ms := newCtxTestStore(t)

	// Save an entry with a specific tag that won't appear in BM25 content.
	entry := Entry{
		Content:  "SSH server info: host=api.rapidai.tech, port=33, user=root",
		Category: CategoryProjectKnowledge,
		Tags:     []string{"4090server", "ssh"},
	}
	_ = ms.Save(entry)

	// Save some noise entries.
	for i := 0; i < 10; i++ {
		_ = ms.Save(Entry{
			Content:  "unrelated project knowledge about database config and deployment process number " + string(rune('A'+i)),
			Category: CategoryProjectKnowledge,
			Tags:     []string{"database", "deploy"},
		})
	}

	// Recall with the alias term.
	results := ms.RecallDynamic("4090server", "", "")
	if len(results) == 0 {
		t.Fatal("RecallDynamic returned no results")
	}

	// The SSH entry should be in the top results due to tag exact match boost.
	found := false
	topN := 3
	if topN > len(results) {
		topN = len(results)
	}
	for _, e := range results[:topN] {
		if strings.Contains(e.Content, "api.rapidai.tech") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected SSH entry to be in top 3 results due to tag exact match boost")
		for i, e := range results {
			content := e.Content
			if len(content) > 60 {
				content = content[:60]
			}
			t.Logf("  result[%d]: tags=%v content=%s", i, e.Tags, content)
		}
	}
}
