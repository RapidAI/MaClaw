package embedding

import "os"

// NewDefaultEmbedder attempts to create a GemmaEmbedder from modelPath.
// If initialization fails (model not found, etc.), it silently falls back to NoopEmbedder.
func NewDefaultEmbedder(modelPath string) Embedder {
	if _, err := os.Stat(modelPath); os.IsNotExist(err) {
		return NoopEmbedder{}
	}
	emb, err := NewGemmaEmbedder(modelPath, 256)
	if err != nil {
		return NoopEmbedder{}
	}
	return emb
}
