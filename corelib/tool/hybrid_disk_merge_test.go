package tool

import (
	"path/filepath"
	"testing"
)

// TestSaveToDiskMergesAcrossInstances: several retrievers share one on-disk
// cache file while embedding different tool subsets. A save from a
// small-subset instance must not drop entries a larger instance wrote —
// before the merge, the second rename clobbered the first (observed in
// production: a ~7-entry save overwrote a 126-entry cache), forcing a full
// recompute on the next process start.
//
// The test injects entries directly and calls SaveToDisk synchronously:
// going through GetBatch would arm the debounced async save, whose goroutine
// outlives the test and could touch the real user cache path after the path
// injection is reverted.
func TestSaveToDiskMergesAcrossInstances(t *testing.T) {
	dir := t.TempDir()
	oldPathFn := toolEmbeddingCachePathFn
	toolEmbeddingCachePathFn = func() string {
		return filepath.Join(dir, "cache", "tool_embeddings.gob")
	}
	defer func() { toolEmbeddingCachePathFn = oldPathFn }()

	// Give both instances the same model fingerprint so the disk validation
	// accepts each other's entries.
	fillCache := func(texts ...string) {
		c := NewToolEmbeddingCache(stubConcurrentEmbedder{})
		if c.modelID == "" {
			c.modelID = "test-model"
		}
		c.mu.Lock()
		for _, text := range texts {
			c.cache[hashText(text)] = []float32{1, 0, 0}
		}
		c.dirty = true
		c.mu.Unlock()
		c.SaveToDisk()
	}

	fillCache("alpha tool", "beta tool", "gamma tool")
	fillCache("delta tool")

	// A fresh instance must restore the UNION, not whoever saved last.
	fresh := NewToolEmbeddingCache(stubConcurrentEmbedder{})
	if fresh.modelID == "" {
		fresh.modelID = "test-model"
	}
	fresh.loadFromDisk()
	for _, key := range []string{"alpha tool", "beta tool", "gamma tool", "delta tool"} {
		if _, ok := fresh.cache[hashText(key)]; !ok {
			t.Fatalf("disk cache lost entry %q after the small instance saved", key)
		}
	}
}
