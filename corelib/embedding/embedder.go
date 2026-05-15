// Package embedding provides a text embedding interface and implementations.
package embedding

import (
	"os"
	"path/filepath"
)

// DefaultModelFilename is the standard filename for the Gemma embedding model.
const DefaultModelFilename = "embeddinggemma-300M-Q8_0.gguf"

// DefaultModelDownloadURL is the primary download source for the embedding model (GitHub Releases).
// Used by GUI and maclawsrv for background model preloading.
const DefaultModelDownloadURL = "https://github.com/RapidAI/MaClaw/releases/download/Model_Release/embeddinggemma-300M-Q8_0.gguf"

// DefaultModelPath returns the default model path: ~/.maclaw/models/embeddinggemma-300M-Q8_0.gguf.
// Returns an empty string if the home directory cannot be determined.
func DefaultModelPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".maclaw", "models", DefaultModelFilename)
}

// Embedder produces dense vector representations of text.
type Embedder interface {
	// Embed returns the embedding vector for a single text.
	// Returns nil, nil when the embedder is not available (noop).
	Embed(text string) ([]float32, error)

	// EmbedBatch returns embeddings for multiple texts.
	EmbedBatch(texts []string) ([][]float32, error)

	// Dim returns the embedding dimension (e.g. 256, 768).
	Dim() int

	// Close releases resources held by the embedder.
	Close()
}
