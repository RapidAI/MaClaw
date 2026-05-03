package memory

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// fakeEmbedder returns a fixed vector for any input text.
type fakeEmbedder struct {
	dim    int
	called int
}

func (f *fakeEmbedder) Embed(text string) ([]float32, error) {
	f.called++
	v := make([]float32, f.dim)
	for i := range v {
		v[i] = 0.5
	}
	return v, nil
}

func (f *fakeEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v, _ := f.Embed(t)
		out[i] = v
	}
	return out, nil
}

func (f *fakeEmbedder) Dim() int { return f.dim }
func (f *fakeEmbedder) Close()   {}

func TestSetEmbedder_BackfillMissingEmbeddings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mem.json")

	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	// Save entries without embeddings.
	for _, content := range []string{"hello world", "foo bar", "test entry"} {
		if err := store.Save(Entry{Content: content, Category: CategoryUserFact}); err != nil {
			t.Fatal(err)
		}
	}

	// Verify entries have no embeddings yet.
	store.mu.RLock()
	for _, e := range store.entries {
		if len(e.Embedding) != 0 {
			t.Fatalf("expected no embedding before SetEmbedder, got %d dims", len(e.Embedding))
		}
	}
	store.mu.RUnlock()

	// Wire a real embedder — should trigger background backfill.
	emb := &fakeEmbedder{dim: 4}
	store.SetEmbedder(emb)

	// Wait for backfill to complete (it runs in a goroutine).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		store.mu.RLock()
		allDone := true
		for _, e := range store.entries {
			if len(e.Embedding) == 0 {
				allDone = false
				break
			}
		}
		store.mu.RUnlock()
		if allDone {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Verify all entries now have embeddings.
	store.mu.RLock()
	for _, e := range store.entries {
		if len(e.Embedding) != 4 {
			t.Errorf("entry %q: expected 4-dim embedding, got %d", e.ID, len(e.Embedding))
		}
	}
	store.mu.RUnlock()

	if emb.called != 3 {
		t.Errorf("expected embedder called 3 times, got %d", emb.called)
	}
}

func TestSetEmbedder_NoopSkipsBackfill(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mem.json")

	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	if err := store.Save(Entry{Content: "test", Category: CategoryUserFact}); err != nil {
		t.Fatal(err)
	}

	// SetEmbedder with NoopEmbedder should not launch backfill.
	store.SetEmbedder(embedding.NoopEmbedder{})

	time.Sleep(100 * time.Millisecond)

	store.mu.RLock()
	for _, e := range store.entries {
		if len(e.Embedding) != 0 {
			t.Errorf("noop embedder should not produce embeddings, got %d dims", len(e.Embedding))
		}
	}
	store.mu.RUnlock()
}

func TestSetEmbedder_NilSkipsBackfill(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mem.json")

	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	if err := store.Save(Entry{Content: "test", Category: CategoryUserFact}); err != nil {
		t.Fatal(err)
	}

	// SetEmbedder(nil) should not panic or launch backfill.
	store.SetEmbedder(nil)

	time.Sleep(100 * time.Millisecond)

	store.mu.RLock()
	for _, e := range store.entries {
		if len(e.Embedding) != 0 {
			t.Errorf("nil embedder should not produce embeddings, got %d dims", len(e.Embedding))
		}
	}
	store.mu.RUnlock()
}

func TestSetEmbedder_SkipsEntriesWithExistingEmbedding(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mem.json")

	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	// Save one entry with embedding already set.
	if err := store.Save(Entry{
		Content:   "already embedded",
		Category:  CategoryUserFact,
		Embedding: []float32{1, 2, 3, 4},
	}); err != nil {
		t.Fatal(err)
	}

	// Save one entry without embedding.
	if err := store.Save(Entry{Content: "needs embedding", Category: CategoryUserFact}); err != nil {
		t.Fatal(err)
	}

	emb := &fakeEmbedder{dim: 4}
	store.SetEmbedder(emb)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		store.mu.RLock()
		allDone := true
		for _, e := range store.entries {
			if len(e.Embedding) == 0 {
				allDone = false
				break
			}
		}
		store.mu.RUnlock()
		if allDone {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Only the entry without embedding should have been processed.
	if emb.called != 1 {
		t.Errorf("expected embedder called 1 time (skip existing), got %d", emb.called)
	}

	// Verify the pre-existing embedding was NOT overwritten.
	store.mu.RLock()
	for _, e := range store.entries {
		if e.Content == "already embedded" {
			if e.Embedding[0] != 1 || e.Embedding[1] != 2 {
				t.Errorf("pre-existing embedding was overwritten: %v", e.Embedding)
			}
		}
	}
	store.mu.RUnlock()
}

func TestQueryEmbeddingCached_WithEmbedder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mem.json")

	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	// Without embedder, should return nil.
	if got := store.queryEmbeddingCached("hello"); got != nil {
		t.Errorf("expected nil without embedder, got %v", got)
	}

	// With noop embedder, should return nil.
	store.SetEmbedder(embedding.NoopEmbedder{})
	if got := store.queryEmbeddingCached("hello"); got != nil {
		t.Errorf("expected nil with noop embedder, got %v", got)
	}

	// With real embedder, should return a vector.
	store.SetEmbedder(&fakeEmbedder{dim: 4})
	got := store.queryEmbeddingCached("hello")
	if len(got) != 4 {
		t.Errorf("expected 4-dim vector, got %d", len(got))
	}
}

func TestBackfill_RespectsStopChannel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mem.json")

	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}

	// Add many entries.
	for i := 0; i < 20; i++ {
		_ = store.Save(Entry{Content: "entry " + string(rune('A'+i)), Category: CategoryUserFact})
	}

	// Stop the store before setting embedder — backfill should exit early.
	store.Stop()

	emb := &fakeEmbedder{dim: 4}
	store.SetEmbedder(emb)

	time.Sleep(200 * time.Millisecond)

	// The embedder may have been called 0 or a few times, but not all 20.
	// The key thing is it doesn't hang or panic.
	if emb.called > 20 {
		t.Errorf("backfill should have stopped early, but called %d times", emb.called)
	}
}

func TestBackfill_PersistsAfterReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mem.json")

	// Create store, add entries, backfill, flush.
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}

	_ = store.Save(Entry{Content: "persist test", Category: CategoryUserFact})

	emb := &fakeEmbedder{dim: 4}
	store.SetEmbedder(emb)

	// Wait for backfill.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		store.mu.RLock()
		done := len(store.entries[0].Embedding) > 0
		store.mu.RUnlock()
		if done {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Flush to disk.
	if err := store.Flush(); err != nil {
		t.Fatal(err)
	}
	store.Stop()

	// Verify the file exists and has content.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty memory file")
	}

	// Reload and verify embeddings survived.
	store2, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store2.Stop()

	store2.mu.RLock()
	if len(store2.entries) != 1 {
		t.Fatalf("expected 1 entry after reload, got %d", len(store2.entries))
	}
	if len(store2.entries[0].Embedding) != 4 {
		t.Errorf("expected 4-dim embedding after reload, got %d", len(store2.entries[0].Embedding))
	}
	store2.mu.RUnlock()
}

func TestSetEmbedderCancelsStaleBackfill(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	if err := store.Save(Entry{Content: "needs embedding", Category: CategoryProjectKnowledge}); err != nil {
		t.Fatal(err)
	}

	first := &blockingQueryEmbedder{started: make(chan struct{}), release: make(chan struct{}), vec: []float32{1, 0, 0, 0}}
	store.SetEmbedder(first)
	<-first.started

	second := &fakeEmbedder{dim: 4}
	store.SetEmbedder(second)
	close(first.release)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		store.mu.RLock()
		var got []float32
		if len(store.entries) > 0 {
			got = append([]float32(nil), store.entries[0].Embedding...)
		}
		store.mu.RUnlock()
		if len(got) > 0 {
			if got[0] == 1 {
				t.Fatalf("stale backfill wrote old embedder vector: %v", got)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for replacement embedder backfill")
}
