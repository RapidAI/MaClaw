package tool

import (
	"path/filepath"
	"testing"
)

type stubModelIDEmbedder struct {
	stubConcurrentEmbedder
	id string
}

func (s stubModelIDEmbedder) ModelID() string { return s.id }

// TestToolEmbeddingCacheKeysOnEmbedderModelID: the disk cache must be keyed on
// the embedder in use, not on the Gemma GGUF fingerprint. Before, a
// Qwen3-only install (no Gemma file) got modelID "" and discarded the cache on
// every start, re-embedding every tool description.
func TestToolEmbeddingCacheKeysOnEmbedderModelID(t *testing.T) {
	dir := t.TempDir()
	oldPathFn := toolEmbeddingCachePathFn
	toolEmbeddingCachePathFn = func() string { return filepath.Join(dir, "cache", "tool_embeddings.gob") }
	defer func() { toolEmbeddingCachePathFn = oldPathFn }()

	q := stubModelIDEmbedder{id: "Qwen3 Embedding 0.6b:1024:prompt-v1:qwen3-instruct-v1"}
	c := NewToolEmbeddingCache(q)
	if c.modelID != "embedder:"+q.id {
		t.Fatalf("modelID = %q, want embedder ModelID", c.modelID)
	}
	c.mu.Lock()
	c.cache[hashText("alpha tool")] = []float32{1, 0, 0}
	c.dirty = true
	c.mu.Unlock()
	c.SaveToDisk()

	if again := NewToolEmbeddingCache(q); len(again.cache) != 1 {
		t.Fatalf("same embedder: restored %d entries, want 1", len(again.cache))
	}
	if other := NewToolEmbeddingCache(stubModelIDEmbedder{id: "Embeddinggemma 300m:768:prompt-v1"}); len(other.cache) != 0 {
		t.Fatalf("different embedder restored %d entries, want 0", len(other.cache))
	}
}
