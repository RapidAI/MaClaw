package memory

import (
	"path/filepath"
	"strings"
	"sync"
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

// recordingCompactFormGenerator implements CompactFormGenerator for tests.
type recordingCompactFormGenerator struct {
	mu      sync.Mutex
	calls   []string
	compact string
	err     error
}

func (g *recordingCompactFormGenerator) Generate(content string, _ Category) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls = append(g.calls, content)
	return g.compact, g.err
}

func (g *recordingCompactFormGenerator) callCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.calls)
}

func waitForCompactForm(t *testing.T, ms *Store, cat Category) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		entries := ms.List(cat, "")
		if len(entries) == 1 && entries[0].CompactForm != "" {
			return entries[0].CompactForm
		}
		time.Sleep(10 * time.Millisecond)
	}
	return ""
}

func TestSaveWithContext_AsyncCompactFormBackfill(t *testing.T) {
	ms := newCtxTestStore(t)
	gen := &recordingCompactFormGenerator{compact: "compact: server runs tmux"}
	ms.SetCompactFormGenerator(gen)

	longContent := "remember that the server uses tmux for long jobs " + strings.Repeat("with detailed session notes ", 20)
	err := ms.SaveWithContext(Entry{
		Content:  longContent,
		Category: CategoryProjectKnowledge,
	}, "")
	if err != nil {
		t.Fatalf("SaveWithContext failed: %v", err)
	}

	// Save must return before the generator is consulted (async, non-blocking).
	if got := waitForCompactForm(t, ms, CategoryProjectKnowledge); got != "compact: server runs tmux" {
		t.Fatalf("expected async compact form to be applied, got %q", got)
	}
	if gen.callCount() != 1 {
		t.Fatalf("expected exactly 1 generator call, got %d", gen.callCount())
	}
}

func TestSaveWithContext_AsyncCompactFormSkipsShortContent(t *testing.T) {
	ms := newCtxTestStore(t)
	gen := &recordingCompactFormGenerator{compact: "compact"}
	ms.SetCompactFormGenerator(gen)

	err := ms.SaveWithContext(Entry{
		Content:  "short note, no compact form needed",
		Category: CategoryProjectKnowledge,
	}, "")
	if err != nil {
		t.Fatalf("SaveWithContext failed: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if gen.callCount() != 0 {
		t.Fatalf("expected no generator call for short content, got %d", gen.callCount())
	}
}

func TestSaveWithContext_AsyncCompactFormSkipsExistingCompactForm(t *testing.T) {
	ms := newCtxTestStore(t)
	gen := &recordingCompactFormGenerator{compact: "compact"}
	ms.SetCompactFormGenerator(gen)

	err := ms.SaveWithContext(Entry{
		Content:     "long entry that already carries a compact form " + strings.Repeat("extra detail ", 30),
		Category:    CategoryProjectKnowledge,
		CompactForm: "precomputed compact form",
	}, "")
	if err != nil {
		t.Fatalf("SaveWithContext failed: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if gen.callCount() != 0 {
		t.Fatalf("expected no generator call when CompactForm is preset, got %d", gen.callCount())
	}
}

func TestSaveWithContext_AsyncCompactFormRejectsNonShorterResult(t *testing.T) {
	ms := newCtxTestStore(t)
	longContent := "remember that the server uses tmux for long jobs " + strings.Repeat("with detailed session notes ", 20)
	gen := &recordingCompactFormGenerator{compact: longContent + " even longer"}
	ms.SetCompactFormGenerator(gen)

	err := ms.SaveWithContext(Entry{
		Content:  longContent,
		Category: CategoryProjectKnowledge,
	}, "")
	if err != nil {
		t.Fatalf("SaveWithContext failed: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && gen.callCount() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if gen.callCount() != 1 {
		t.Fatalf("expected 1 generator call, got %d", gen.callCount())
	}
	time.Sleep(50 * time.Millisecond)
	entries := ms.List(CategoryProjectKnowledge, "")
	if len(entries) != 1 || entries[0].CompactForm != "" {
		t.Fatalf("non-shorter compact form must not be applied, got %+v", entries)
	}
}

func TestNewLLMCompactFormGenerator(t *testing.T) {
	if got := NewLLMCompactFormGenerator(nil); got != nil {
		t.Fatalf("expected nil generator for nil LLM, got %T", got)
	}
	llm := &mockLLMForExtraction{extractResponse: "compact fact"}
	gen := NewLLMCompactFormGenerator(llm)
	if gen == nil {
		t.Fatal("expected non-nil generator")
	}
	compact, err := gen.Generate("some long memory content about a server", CategoryProjectKnowledge)
	if err != nil || compact != "compact fact" {
		t.Fatalf("Generate = %q, %v", compact, err)
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
