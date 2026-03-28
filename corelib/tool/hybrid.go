package tool

import (
	"crypto/sha256"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// ---------------------------------------------------------------------------
// CosineSimilarity
// ---------------------------------------------------------------------------

// CosineSimilarity computes the cosine similarity between two float32 vectors.
// Returns 0.0 for nil, empty, mismatched-length, or zero-magnitude vectors.
func CosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0.0
	}
	var dot, normA, normB float64
	for i := range a {
		ai, bi := float64(a[i]), float64(b[i])
		dot += ai * bi
		normA += ai * ai
		normB += bi * bi
	}
	if normA == 0 || normB == 0 {
		return 0.0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// ---------------------------------------------------------------------------
// ToolEmbeddingCache
// ---------------------------------------------------------------------------

// ToolEmbeddingCache caches embedding vectors for tool description texts.
// Keyed by SHA-256 hash of the description text.
type ToolEmbeddingCache struct {
	mu       sync.RWMutex
	embedder embedding.Embedder
	cache    map[string][]float32 // hash(description) → embedding
}

// NewToolEmbeddingCache creates a new ToolEmbeddingCache.
func NewToolEmbeddingCache(emb embedding.Embedder) *ToolEmbeddingCache {
	return &ToolEmbeddingCache{
		embedder: emb,
		cache:    make(map[string][]float32),
	}
}

func hashText(text string) string {
	h := sha256.Sum256([]byte(text))
	return fmt.Sprintf("%x", h)
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
	c.mu.Unlock()
	return vec, nil
}

// GetBatch returns embeddings for a batch of tool descriptions.
// Keys in texts are tool IDs, values are description texts.
// Returns a map of tool ID → embedding vector.
// On per-tool embed error the vector is nil (no error propagation).
func (c *ToolEmbeddingCache) GetBatch(texts map[string]string) (map[string][]float32, error) {
	result := make(map[string][]float32, len(texts))

	// Identify which texts need computation.
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

	// Compute missing embeddings one by one (skip on per-tool error).
	for _, m := range missing {
		vec, err := c.embedder.Embed(m.text)
		if err != nil {
			// Skip vector score for this tool — nil embedding, no error propagation.
			result[m.toolID] = nil
			continue
		}
		c.mu.Lock()
		c.cache[m.key] = vec
		c.mu.Unlock()
		result[m.toolID] = vec
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
}

// QueryEmbeddingCache is an LRU cache with TTL for user query embeddings.
type QueryEmbeddingCache struct {
	mu       sync.Mutex
	embedder embedding.Embedder
	entries  map[string]*queryEntry
	order    []string // LRU order: most recent at end
	maxSize  int
	ttl      time.Duration
}

// NewQueryEmbeddingCache creates a new QueryEmbeddingCache.
func NewQueryEmbeddingCache(emb embedding.Embedder, maxSize int, ttl time.Duration) *QueryEmbeddingCache {
	return &QueryEmbeddingCache{
		embedder: emb,
		entries:  make(map[string]*queryEntry),
		order:    nil,
		maxSize:  maxSize,
		ttl:      ttl,
	}
}

// Get returns the cached embedding for query, or computes and caches a new one.
// Expired entries are treated as cache misses.
func (c *QueryEmbeddingCache) Get(query string) ([]float32, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()

	if entry, ok := c.entries[query]; ok {
		if now.Sub(entry.createdAt) < c.ttl {
			// Move to end of LRU order.
			c.moveToEnd(query)
			return entry.vec, nil
		}
		// Expired — remove and recompute.
		c.removeLocked(query)
	}

	vec, err := c.embedder.Embed(query)
	if err != nil {
		return nil, err
	}

	// Evict LRU if at capacity.
	if len(c.entries) >= c.maxSize {
		c.evictLocked()
	}

	c.entries[query] = &queryEntry{vec: vec, createdAt: now}
	c.order = append(c.order, query)
	return vec, nil
}

// moveToEnd moves query to the end of the LRU order slice.
func (c *QueryEmbeddingCache) moveToEnd(query string) {
	for i, q := range c.order {
		if q == query {
			c.order = append(c.order[:i], c.order[i+1:]...)
			c.order = append(c.order, query)
			return
		}
	}
}

// removeLocked removes a query from both the map and order slice.
func (c *QueryEmbeddingCache) removeLocked(query string) {
	delete(c.entries, query)
	for i, q := range c.order {
		if q == query {
			c.order = append(c.order[:i], c.order[i+1:]...)
			return
		}
	}
}

// evictLocked removes the least recently used entry (first element in order).
func (c *QueryEmbeddingCache) evictLocked() {
	if len(c.order) == 0 {
		return
	}
	oldest := c.order[0]
	c.order = c.order[1:]
	delete(c.entries, oldest)
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
	normBM25 := minMaxNormalize(bm25Scores)

	// Compute fused scores.
	fused := make(map[string]float64, len(bm25Scores))
	for toolID, normScore := range normBM25 {
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

// minMaxNormalize applies min-max normalization to a score map.
// If all scores are the same (min==max), all normalized values are 0.0.
func minMaxNormalize(scores map[string]float64) map[string]float64 {
	if len(scores) == 0 {
		return scores
	}

	minVal := math.Inf(1)
	maxVal := math.Inf(-1)
	for _, s := range scores {
		if s < minVal {
			minVal = s
		}
		if s > maxVal {
			maxVal = s
		}
	}

	result := make(map[string]float64, len(scores))
	rang := maxVal - minVal
	if rang == 0 {
		for k := range scores {
			result[k] = 0.0
		}
		return result
	}

	for k, s := range scores {
		result[k] = (s - minVal) / rang
	}
	return result
}
