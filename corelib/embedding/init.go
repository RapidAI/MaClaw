package embedding

import "os"

// DefaultEmbeddingDim is the production embedding width. EmbeddingGemma is
// MRL-trained, but routing quality measurements (weather/pdf queries against
// the full 164-tool surface, 2026-08-24) showed the 256-dim truncation
// collapsing CJK discrimination to the point that git_status outranked
// web_search for a weather query; the full 768 width restores the correct
// ordering. Inference cost is identical — MRL truncation happens after
// pooling — only the stored vectors grow.
const DefaultEmbeddingDim = 768

// NewDefaultEmbedder attempts to create a GemmaEmbedder from modelPath.
// If initialization fails (model not found, etc.), it silently falls back to NoopEmbedder.
func NewDefaultEmbedder(modelPath string) Embedder {
	if _, err := os.Stat(modelPath); os.IsNotExist(err) {
		return NoopEmbedder{}
	}
	emb, err := NewGemmaEmbedder(modelPath, DefaultEmbeddingDim)
	if err != nil {
		return NoopEmbedder{}
	}
	return emb
}
