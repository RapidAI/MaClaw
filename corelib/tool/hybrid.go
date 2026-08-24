package tool

import (
	"container/list"
	"crypto/sha256"
	"encoding/gob"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

// ConcurrentEmbedder is an optional interface that embedders can implement
// to support lock-free concurrent inference (each call allocates its own scratch).
type ConcurrentEmbedder interface {
	EmbedConcurrent(text string) ([]float32, error)
}

// ---------------------------------------------------------------------------
// CosineSimilarity
// ---------------------------------------------------------------------------

// CosineSimilarity computes the cosine similarity between two float32 vectors.
// Returns 0.0 for nil, empty, mismatched-length, or zero-magnitude vectors.
// Uses SIMD-accelerated dot product and norm for performance.
// Fast path: if both vectors are already L2-normalized (norm ≈ 1.0),
// cosine similarity equals the dot product — skips the expensive norm/sqrt.
func CosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0.0
	}
	dot := float64(tensor.Dot(a, b))
	normA := float64(tensor.Dot(a, a))
	normB := float64(tensor.Dot(b, b))
	if normA == 0 || normB == 0 {
		return 0.0
	}
	// Fast path: both vectors are unit-length (L2-normalized).
	// Our embeddings are always L2-normalized, so this is the common case.
	if normA > 0.999 && normA < 1.001 && normB > 0.999 && normB < 1.001 {
		return dot
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// ---------------------------------------------------------------------------
// ToolEmbeddingCache
// ---------------------------------------------------------------------------

// ToolEmbeddingCache caches embedding vectors for tool description texts.
// Keyed by SHA-256 hash of the description text.
// Supports disk persistence: embeddings are saved to <MaclawBaseDir>/cache/tool_embeddings.gob
// and restored on next startup if the model file hasn't changed.
type ToolEmbeddingCache struct {
	mu       sync.RWMutex
	embedder embedding.Embedder
	cache    map[string][]float32 // hash(description) → embedding
	dirty    bool                 // true when cache has new entries not yet persisted
	modelID  string               // model file modtime fingerprint for cache invalidation

	saveMu   sync.Mutex    // serializes disk writes
	saveOnce sync.Once     // ensures only one debounce goroutine is active
	saveCh   chan struct{} // reset channel for debounce
}

const maxToolEmbeddingCacheSize = 2000 // upper bound to prevent unbounded growth

// diskCacheEnvelope is the on-disk format for persisted tool embeddings.
type diskCacheEnvelope struct {
	ModelID string               // model file fingerprint (modtime + size)
	EmbDim  int                  // embedding dimension at time of caching
	Entries map[string][]float32 // sha256(text) → embedding vector
}

// toolEmbeddingCachePath returns <MaclawBaseDir>/cache/tool_embeddings.gob.
func toolEmbeddingCachePath() string {
	base := maclawBaseDirFallback()
	if base == "" {
		return ""
	}
	return filepath.Join(base, "cache", "tool_embeddings.gob")
}

// modelFingerprint returns a string combining the model file's modtime and size.
// Returns "" if the file doesn't exist or can't be stat'd.
func modelFingerprint(modelPath string) string {
	fi, err := os.Stat(modelPath)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d_%d", fi.ModTime().UnixNano(), fi.Size())
}

// NewToolEmbeddingCache creates a new ToolEmbeddingCache and attempts to
// restore previously persisted embeddings from disk.
func NewToolEmbeddingCache(emb embedding.Embedder) *ToolEmbeddingCache {
	modelPath := embedding.DefaultModelPath()
	mid := modelFingerprint(modelPath)

	c := &ToolEmbeddingCache{
		embedder: emb,
		cache:    make(map[string][]float32),
		modelID:  mid,
		saveCh:   make(chan struct{}, 1),
	}
	log.Printf("[ToolEmbeddingCache] init: modelID=%q", mid)
	c.loadFromDisk()
	return c
}

func hashText(text string) string {
	h := sha256.Sum256([]byte(text))
	return fmt.Sprintf("%x", h)
}

// WarmUpAsync pre-computes embeddings for a set of tool descriptions in the
// background. This eliminates cold-start latency on the first user query by
// ensuring all tool embeddings are cached before they're needed.
// Safe to call from app startup; does not block the caller.
func (c *ToolEmbeddingCache) WarmUpAsync(toolTexts map[string]string) {
	if len(toolTexts) == 0 {
		return
	}
	go func() {
		_, err := c.GetBatch(toolTexts)
		if err != nil {
			log.Printf("[ToolEmbeddingCache] warm-up error: %v", err)
		} else {
			log.Printf("[ToolEmbeddingCache] warm-up complete: %d tools", len(toolTexts))
		}
	}()
}

// loadFromDisk restores cached embeddings from the gob file.
// If the model fingerprint doesn't match, the disk cache is ignored.
func (c *ToolEmbeddingCache) loadFromDisk() {
	p := toolEmbeddingCachePath()
	if p == "" {
		return
	}
	f, err := os.Open(p)
	if err != nil {
		log.Printf("[ToolEmbeddingCache] load: no disk cache at %s (first run)", p)
		return // file doesn't exist yet — normal on first run
	}
	defer f.Close()

	var env diskCacheEnvelope
	if err := gob.NewDecoder(f).Decode(&env); err != nil {
		log.Printf("[ToolEmbeddingCache] disk cache decode error, ignoring: %v", err)
		return
	}

	// Validate: model fingerprint must match.
	if env.ModelID != c.modelID || c.modelID == "" {
		log.Printf("[ToolEmbeddingCache] model changed (disk=%q current=%q), discarding disk cache", env.ModelID, c.modelID)
		return
	}

	// Validate: embedding dimension must match the embedder's output dim.
	embDim := c.embedder.Dim()
	if embDim > 0 && env.EmbDim > 0 && env.EmbDim != embDim {
		log.Printf("[ToolEmbeddingCache] dim mismatch (disk=%d current=%d), discarding disk cache", env.EmbDim, embDim)
		return
	}

	c.cache = make(map[string][]float32, len(env.Entries))
	for k, v := range env.Entries {
		if len(v) > 0 {
			c.cache[k] = v
		}
	}
	log.Printf("[ToolEmbeddingCache] restored %d embeddings from disk cache", len(c.cache))
}

// SaveToDisk persists the current in-memory cache to disk.
// Safe to call from any goroutine. No-op if nothing changed since last save.
// Serialized via saveMu to prevent concurrent writes.
func (c *ToolEmbeddingCache) SaveToDisk() {
	c.saveMu.Lock()
	defer c.saveMu.Unlock()

	c.mu.RLock()
	if !c.dirty {
		c.mu.RUnlock()
		return
	}
	// Snapshot under read lock.
	entries := make(map[string][]float32, len(c.cache))
	for k, v := range c.cache {
		entries[k] = v
	}
	modelID := c.modelID
	c.mu.RUnlock()

	embDim := c.embedder.Dim()

	p := toolEmbeddingCachePath()
	if p == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		log.Printf("[ToolEmbeddingCache] mkdir error: %v", err)
		return
	}

	env := diskCacheEnvelope{
		ModelID: modelID,
		EmbDim:  embDim,
		Entries: entries,
	}

	tmp := p + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		log.Printf("[ToolEmbeddingCache] create tmp file error: %v", err)
		return
	}
	if err := gob.NewEncoder(f).Encode(&env); err != nil {
		f.Close()
		os.Remove(tmp)
		log.Printf("[ToolEmbeddingCache] encode error: %v", err)
		return
	}
	f.Close()

	if err := os.Rename(tmp, p); err != nil {
		os.Remove(tmp)
		log.Printf("[ToolEmbeddingCache] rename error: %v", err)
		return
	}

	c.mu.Lock()
	c.dirty = false
	c.mu.Unlock()

	log.Printf("[ToolEmbeddingCache] saved %d embeddings to disk cache", len(entries))
}

// scheduleSave triggers an async disk save with proper debounce.
// Only one debounce goroutine is ever active; subsequent calls reset the timer.
func (c *ToolEmbeddingCache) scheduleSave() {
	// Non-blocking signal to reset the debounce timer.
	select {
	case c.saveCh <- struct{}{}:
	default:
	}

	c.saveOnce.Do(func() {
		go func() {
			for {
				// Wait for a save signal.
				<-c.saveCh
				// Debounce: keep draining signals for 2 seconds.
				timer := time.NewTimer(2 * time.Second)
			drain:
				for {
					select {
					case <-c.saveCh:
						timer.Reset(2 * time.Second)
					case <-timer.C:
						break drain
					}
				}
				c.SaveToDisk()
			}
		}()
	})
}

// evictIfNeeded removes random entries when cache exceeds maxToolEmbeddingCacheSize.
// Must be called with c.mu held for writing.
func (c *ToolEmbeddingCache) evictIfNeeded() {
	if len(c.cache) <= maxToolEmbeddingCacheSize {
		return
	}
	// Remove oldest entries. Since map iteration is random in Go, this is
	// effectively random eviction — good enough for a warm cache.
	excess := len(c.cache) - maxToolEmbeddingCacheSize
	log.Printf("[ToolEmbeddingCache] evict: cache size %d exceeds limit %d, removing %d entries", len(c.cache), maxToolEmbeddingCacheSize, excess)
	for k := range c.cache {
		if excess <= 0 {
			break
		}
		delete(c.cache, k)
		excess--
	}
}

// Get returns the cached embedding for text, or computes and caches a new one.
// On embed error the returned vector is nil and the error is propagated.
func (c *ToolEmbeddingCache) Get(text string) ([]float32, error) {
	key := hashText(text)

	c.mu.RLock()
	if vec, ok := c.cache[key]; ok {
		c.mu.RUnlock()
		return vec, nil
	}
	c.mu.RUnlock()

	vec, err := c.embedder.Embed(text)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	// Double-check: another goroutine may have computed this while we were embedding.
	if existing, ok := c.cache[key]; ok {
		c.mu.Unlock()
		return existing, nil
	}
	c.cache[key] = vec
	c.dirty = true
	c.evictIfNeeded()
	c.mu.Unlock()

	c.scheduleSave()
	return vec, nil
}

// GetBatch returns embeddings for a batch of tool descriptions.
// When the embedder supports ConcurrentEmbedder, missing embeddings are
// computed in parallel across CPU cores, dramatically reducing cold-start time.
func (c *ToolEmbeddingCache) GetBatch(texts map[string]string) (map[string][]float32, error) {
	result := make(map[string][]float32, len(texts))

	type needEmbed struct {
		toolID string
		text   string
		key    string
	}
	var missing []needEmbed

	c.mu.RLock()
	for toolID, text := range texts {
		key := hashText(text)
		if vec, ok := c.cache[key]; ok {
			result[toolID] = vec
		} else {
			missing = append(missing, needEmbed{toolID: toolID, text: text, key: key})
		}
	}
	c.mu.RUnlock()

	if len(missing) == 0 {
		return result, nil
	}

	// Try concurrent path if embedder supports it and there are multiple items.
	ce, hasConcurrent := c.embedder.(ConcurrentEmbedder)
	log.Printf("[ToolEmbeddingCache] GetBatch: total=%d cached=%d missing=%d hasConcurrent=%v", len(texts), len(texts)-len(missing), len(missing), hasConcurrent)
	if hasConcurrent && len(missing) > 1 {
		type embedResult struct {
			idx int
			vec []float32
		}
		results := make([]embedResult, len(missing))
		errs := make([]error, len(missing))

		maxWorkers := runtime.NumCPU()
		if maxWorkers > 8 {
			maxWorkers = 8
		}
		if maxWorkers > len(missing) {
			maxWorkers = len(missing)
		}

		sem := make(chan struct{}, maxWorkers)
		var wg sync.WaitGroup

		for i, m := range missing {
			wg.Add(1)
			sem <- struct{}{}
			go func(idx int, text string) {
				defer wg.Done()
				defer func() { <-sem }()
				vec, err := ce.EmbedConcurrent(text)
				if err != nil {
					errs[idx] = err
				} else {
					results[idx] = embedResult{idx: idx, vec: vec}
				}
			}(i, m.text)
		}
		wg.Wait()

		c.mu.Lock()
		for i, m := range missing {
			if errs[i] != nil {
				result[m.toolID] = nil
				continue
			}
			c.cache[m.key] = results[i].vec
			result[m.toolID] = results[i].vec
		}
		c.dirty = true
		c.evictIfNeeded()
		c.mu.Unlock()

		c.scheduleSave()
		return result, nil
	}

	// Sequential fallback.
	computed := false
	for _, m := range missing {
		vec, err := c.embedder.Embed(m.text)
		if err != nil {
			result[m.toolID] = nil
			continue
		}
		c.mu.Lock()
		c.cache[m.key] = vec
		c.dirty = true
		c.evictIfNeeded()
		c.mu.Unlock()
		result[m.toolID] = vec
		computed = true
	}

	if computed {
		c.scheduleSave()
	}

	return result, nil
}

// ---------------------------------------------------------------------------
// QueryEmbeddingCache
// ---------------------------------------------------------------------------

// queryEntry holds a cached query embedding with its creation timestamp.
type queryEntry struct {
	vec       []float32
	createdAt time.Time
	query     string // stored for O(1) eviction
}

// QueryEmbeddingCache is an LRU cache with TTL for user query embeddings.
// Uses container/list + map for O(1) get/put/evict operations.
type QueryEmbeddingCache struct {
	mu       sync.Mutex
	embedder embedding.Embedder
	items    map[string]*list.Element // query → list element
	order    *list.List               // front = oldest, back = newest
	maxSize  int
	ttl      time.Duration
}

// NewQueryEmbeddingCache creates a new QueryEmbeddingCache.
func NewQueryEmbeddingCache(emb embedding.Embedder, maxSize int, ttl time.Duration) *QueryEmbeddingCache {
	return &QueryEmbeddingCache{
		embedder: emb,
		items:    make(map[string]*list.Element, maxSize),
		order:    list.New(),
		maxSize:  maxSize,
		ttl:      ttl,
	}
}

// Get returns the cached embedding for query, or computes and caches a new one.
// Expired entries are treated as cache misses.
func (c *QueryEmbeddingCache) Get(query string) ([]float32, error) {
	c.mu.Lock()
	now := time.Now()

	if elem, ok := c.items[query]; ok {
		entry := elem.Value.(*queryEntry)
		if now.Sub(entry.createdAt) < c.ttl {
			// Move to back (most recent) — O(1)
			c.order.MoveToBack(elem)
			vec := entry.vec
			c.mu.Unlock()
			return vec, nil
		}
		// Expired — remove and recompute.
		c.order.Remove(elem)
		delete(c.items, query)
	}
	c.mu.Unlock()

	// Compute embedding outside the lock to avoid blocking other callers.
	vec, err := c.embedder.Embed(query)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check: another goroutine may have computed this while we were embedding.
	if elem, ok := c.items[query]; ok {
		entry := elem.Value.(*queryEntry)
		c.order.MoveToBack(elem)
		return entry.vec, nil
	}

	// Evict LRU if at capacity — O(1)
	if len(c.items) >= c.maxSize {
		front := c.order.Front()
		if front != nil {
			evicted := c.order.Remove(front).(*queryEntry)
			delete(c.items, evicted.query)
		}
	}

	entry := &queryEntry{vec: vec, createdAt: time.Now(), query: query}
	elem := c.order.PushBack(entry)
	c.items[query] = elem
	return vec, nil
}

// ---------------------------------------------------------------------------
// HybridRetriever
// ---------------------------------------------------------------------------

// HybridRetriever combines BM25 sparse scores with dense vector cosine
// similarity scores using weighted linear fusion.
type HybridRetriever struct {
	embedder   embedding.Embedder
	toolCache  *ToolEmbeddingCache
	queryCache *QueryEmbeddingCache
	alpha      float64 // fusion weight: alpha*BM25 + (1-alpha)*cosine
}

// NewHybridRetriever creates a new HybridRetriever with default alpha=0.6,
// query cache maxSize=64, and TTL=30s.
func NewHybridRetriever(emb embedding.Embedder) *HybridRetriever {
	return &HybridRetriever{
		embedder:   emb,
		toolCache:  NewToolEmbeddingCache(emb),
		queryCache: NewQueryEmbeddingCache(emb, 64, 30*time.Second),
		alpha:      0.6,
	}
}

// WarmUp pre-computes tool embeddings in the background to eliminate cold-start latency.
// Call this at app startup with all known tool descriptions.
func (h *HybridRetriever) WarmUp(toolTexts map[string]string) {
	h.toolCache.WarmUpAsync(toolTexts)
}

// FuseScores combines BM25 scores with vector cosine similarity scores.
//
// Parameters:
//   - query: the user query text
//   - bm25Scores: map of tool ID → raw BM25 score
//   - toolTexts: map of tool ID → description text for embedding
//
// Returns a map of tool ID → fused score.
//
// If the embedder is a NoopEmbedder, returns bm25Scores unchanged.
// On query embed error, falls back to pure BM25 scores.
func (h *HybridRetriever) FuseScores(
	query string,
	bm25Scores map[string]float64,
	toolTexts map[string]string,
) map[string]float64 {
	if embedding.IsNoop(h.embedder) {
		return bm25Scores
	}

	// Get query embedding.
	queryVec, err := h.queryCache.Get(query)
	if err != nil || queryVec == nil {
		return bm25Scores
	}

	// Get tool embeddings in batch.
	toolVecs, err := h.toolCache.GetBatch(toolTexts)
	if err != nil {
		return bm25Scores
	}

	// Min-max normalize BM25 scores.
	normBM25 := normalizeRetrievalScores(bm25Scores)

	// Compute fused scores over ALL candidates, not just BM25 hits. The whole
	// point of the dense channel is to rescue semantically relevant tools that
	// keyword scoring misses (e.g. a Chinese query against English tool
	// descriptions). Iterating only over bm25Scores keys would silently drop
	// every tool without token overlap — the failure mode behind git_status
	// outranking web_fetch for a weather query in production.
	fused := make(map[string]float64, len(toolTexts))
	for toolID := range toolTexts {
		normScore := normBM25[toolID] // 0 when the tool had no BM25 hit
		vec := toolVecs[toolID]
		if vec == nil {
			// No embedding available — use only normalized BM25 score.
			fused[toolID] = normScore
			continue
		}
		cosSim := CosineSimilarity(queryVec, vec)
		fused[toolID] = h.alpha*normScore + (1-h.alpha)*cosSim
	}

	return fused
}

// normalizeRetrievalScores scales retrieval scores by dividing by the maximum
// value, mapping the top score to 1.0 while keeping "no signal" at 0.
//
// This deliberately avoids min-max normalization: subtracting the minimum
// zeroes the weakest genuine hit (the tool ranked last among the matches) and,
// when all scores tie at a positive value (e.g. a single BM25 hit), collapses
// the only relevant tool to 0 as well. Both failure modes make downstream
// selection gates (MinCandidateRouteScore) silently drop tools that did match.
func normalizeRetrievalScores(scores map[string]float64) map[string]float64 {
	if len(scores) == 0 {
		return scores
	}

	maxVal := math.Inf(-1)
	for _, s := range scores {
		if s > maxVal {
			maxVal = s
		}
	}

	result := make(map[string]float64, len(scores))
	if maxVal <= 0 {
		// No positive signal at all: everything normalizes to 0 so selection
		// gates treat every tool as unmatched.
		for k := range scores {
			result[k] = 0.0
		}
		return result
	}

	for k, s := range scores {
		result[k] = s / maxVal
	}
	return result
}
