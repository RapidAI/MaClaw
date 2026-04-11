package memory

import (
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

// vectorIndex stores embedding vectors keyed by entry ID and computes
// cosine similarity scores against a query vector.
// Vectors are stored L2-normalized, so cosine similarity = dot product.
type vectorIndex struct {
	mu         sync.RWMutex
	embeddings map[string][]float32
	dim        int
}

func newVectorIndex() *vectorIndex {
	return &vectorIndex{embeddings: make(map[string][]float32)}
}

func (v *vectorIndex) add(id string, emb []float32) {
	if len(emb) == 0 {
		return
	}
	v.mu.Lock()
	v.embeddings[id] = emb
	if v.dim == 0 {
		v.dim = len(emb)
	}
	v.mu.Unlock()
}

func (v *vectorIndex) remove(id string) {
	v.mu.Lock()
	delete(v.embeddings, id)
	v.mu.Unlock()
}

func (v *vectorIndex) update(id string, emb []float32) {
	if len(emb) == 0 {
		return
	}
	v.mu.Lock()
	v.embeddings[id] = emb
	v.mu.Unlock()
}

func (v *vectorIndex) rebuild(entries []Entry) {
	v.mu.Lock()
	v.embeddings = make(map[string][]float32, len(entries))
	for _, e := range entries {
		if len(e.Embedding) > 0 {
			v.embeddings[e.ID] = e.Embedding
			if v.dim == 0 {
				v.dim = len(e.Embedding)
			}
		}
	}
	v.mu.Unlock()
}

// score computes cosine similarity between queryEmb and all stored embeddings.
// If embeddings are L2-normalized (as produced by GemmaEmbedder), this reduces
// to a dot product. Uses SIMD-accelerated tensor.Dot for performance.
func (v *vectorIndex) score(queryEmb []float32) map[string]float64 {
	if len(queryEmb) == 0 {
		return nil
	}
	v.mu.RLock()
	defer v.mu.RUnlock()

	if len(v.embeddings) == 0 {
		return nil
	}

	scores := make(map[string]float64, len(v.embeddings))
	for id, emb := range v.embeddings {
		if len(emb) != len(queryEmb) {
			continue
		}
		sim := float64(tensor.Dot(queryEmb, emb))
		if sim > 0 {
			scores[id] = sim
		}
	}
	return scores
}
