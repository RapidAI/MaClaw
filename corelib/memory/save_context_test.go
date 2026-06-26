package memory

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestSaveWithContext_EmbeddingTimeoutStillPersistsEntry(t *testing.T) {
	ms := newCtxTestStore(t)

	prevBudget := saveEmbeddingBudget
	saveEmbeddingBudget = 30 * time.Millisecond
	t.Cleanup(func() { saveEmbeddingBudget = prevBudget })

	emb := &blockingQueryEmbedder{
		started: make(chan struct{}),
		release: make(chan struct{}),
		vec:     []float32{1, 2, 3, 4},
	}
	ms.SetEmbedder(emb)

	done := make(chan error, 1)
	start := time.Now()
	go func() {
		done <- ms.SaveWithContext(Entry{
			Content:  "remember that the server uses tmux for long jobs",
			Category: CategoryProjectKnowledge,
		}, "ssh maintenance habits")
	}()

	<-emb.started
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("SaveWithContext failed: %v", err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("SaveWithContext blocked on embedding timeout")
	}
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("SaveWithContext timeout fallback took too long: %v", elapsed)
	}

	entries := ms.List(CategoryProjectKnowledge, "")
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry after timeout fallback, got %d", len(entries))
	}
	if len(entries[0].Embedding) != 0 {
		t.Fatalf("expected entry to persist without embedding on timeout, got %v", entries[0].Embedding)
	}

	close(emb.release)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		entries = ms.List(CategoryProjectKnowledge, "")
		if len(entries) == 1 && len(entries[0].Embedding) == 4 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed-out save embedding was not applied after background completion")
}

func TestSaveWithContext_AsyncEmbeddingDoesNotOverwriteLaterUpdate(t *testing.T) {
	ms := newCtxTestStore(t)

	prevBudget := saveEmbeddingBudget
	saveEmbeddingBudget = 30 * time.Millisecond
	t.Cleanup(func() { saveEmbeddingBudget = prevBudget })

	emb := &blockingQueryEmbedder{
		started: make(chan struct{}),
		release: make(chan struct{}),
		vec:     []float32{9, 8, 7, 6},
	}
	ms.SetEmbedder(emb)

	done := make(chan error, 1)
	go func() {
		done <- ms.SaveWithContext(Entry{
			Content:  "original ssh maintenance note",
			Category: CategoryProjectKnowledge,
		}, "")
	}()

	<-emb.started
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("SaveWithContext failed: %v", err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("SaveWithContext blocked on embedding timeout")
	}

	entries := ms.List(CategoryProjectKnowledge, "")
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry after timeout fallback, got %d", len(entries))
	}
	updated := entries[0]
	updated.Content = "updated ssh maintenance note"
	if err := ms.UpdateEntriesByID([]Entry{updated}); err != nil {
		t.Fatalf("UpdateEntriesByID failed: %v", err)
	}

	close(emb.release)
	time.Sleep(100 * time.Millisecond)

	entries = ms.List(CategoryProjectKnowledge, "")
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry after async embedding completion, got %d", len(entries))
	}
	if entries[0].Content != "updated ssh maintenance note" {
		t.Fatalf("async embedding overwrote later content update: %q", entries[0].Content)
	}
	if len(entries[0].Embedding) != 0 {
		t.Fatalf("async embedding should not apply after content hash changed, got %v", entries[0].Embedding)
	}
}

func TestSaveWithContext_SkipsEmbeddingWhenConcurrencySaturated(t *testing.T) {
	ms := newCtxTestStore(t)
	emb := &countingQueryEmbedder{dim: 4}
	ms.SetEmbedder(emb)
	ms.saveEmbeddingSem = make(chan struct{}, 1)
	ms.saveEmbeddingSem <- struct{}{}

	start := time.Now()
	if err := ms.SaveWithContext(Entry{
		Content:  "save should not wait for a saturated embedding queue",
		Category: CategoryProjectKnowledge,
	}, ""); err != nil {
		t.Fatalf("SaveWithContext failed: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("SaveWithContext took too long with saturated embedding queue: %v", elapsed)
	}
	if emb.calls != 0 {
		t.Fatalf("expected saturated save embedding queue to skip embed call, got %d", emb.calls)
	}
	entries := ms.List(CategoryProjectKnowledge, "")
	if len(entries) != 1 {
		t.Fatalf("expected saved entry, got %d", len(entries))
	}
	if len(entries[0].Embedding) != 0 {
		t.Fatalf("expected entry to save without embedding, got %v", entries[0].Embedding)
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
