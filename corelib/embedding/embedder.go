// Package embedding provides a text embedding interface and implementations.
package embedding

import (
	"path/filepath"
	"sync/atomic"
)

// DefaultModelFilename is the file name of the single shipped local
// embedding model (Qwen3-Embedding-0.6B Q8_0 GGUF).
const DefaultModelFilename = Qwen3ModelFilename

// DefaultModelDownloadURL is the primary download source for the embedding
// model (GitHub release Model_Release). Hosts append the hub mirror
// (<hub>/api/v1/models/<file>) as the fallback.
const DefaultModelDownloadURL = ReleaseBaseURL + "/" + Qwen3ModelFilename

// LegacyGemmaModelFilename is the EmbeddingGemma-300M file earlier releases
// downloaded into the models dir. It is no longer loaded or downloaded; hosts
// only use the name to remove the stale cache copy once Qwen3 is installed.
const LegacyGemmaModelFilename = "embeddinggemma-300M-Q8_0.gguf"

// BaseDirFunc optionally supplies the effective Maclaw base directory.
// corelib wires this at init time without creating an import cycle.
var BaseDirFunc atomic.Value // stores func() string

// DefaultModelsDir returns the default local AI model directory.
func DefaultModelsDir() string {
	if fn, ok := BaseDirFunc.Load().(func() string); ok && fn != nil {
		if base := fn(); base != "" {
			return filepath.Join(base, "models")
		}
	}
	return ""
}

// DefaultModelPath returns the default embedding model path under the effective
// Maclaw base directory.
// Returns an empty string if the home directory cannot be determined.
func DefaultModelPath() string {
	dir := DefaultModelsDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, DefaultModelFilename)
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
