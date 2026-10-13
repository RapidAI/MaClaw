package embedding

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/embedding/gguf"
)

// DefaultEmbeddingDim is the production embedding width. EmbeddingGemma is
// MRL-trained, but routing quality measurements (weather/pdf queries against
// the full 164-tool surface, 2026-08-24) showed the 256-dim truncation
// collapsing CJK discrimination to the point that git_status outranked
// web_search for a weather query; the full 768 width restores the correct
// ordering. Inference cost is identical — MRL truncation happens after
// pooling — only the stored vectors grow.
const DefaultEmbeddingDim = 768

// EmbedModelEnv selects the local embedding model family for
// NewDefaultEmbedder (and therefore SharedEmbedder) and LoadPreferredEmbedder:
//
//	any value                  Qwen3-Embedding-0.6B (Q8_0 GGUF, the single
//	                            shipped model), located by ResolveQwen3ModelPath;
//	                            the retired "gemma" pin is ignored
//
// Switching changes ModelID, so stored vectors and intent anchor caches are
// re-embedded rather than mixed.
const EmbedModelEnv = "MACLAW_EMBED_MODEL"

// EmbedModelPathEnv overrides the model file used when EmbedModelEnv=qwen3.
const EmbedModelPathEnv = "MACLAW_EMBED_MODEL_PATH"

// SelectedEmbedModel returns the local embedding model family: always "qwen3"
// (MACLAW_EMBED_MODEL is ignored).
func SelectedEmbedModel() string {
	return "qwen3"
}

// resolveModelPath maps the caller's (Gemma) model path onto the file of the
// selected model family.
func resolveModelPath(modelPath string) string {
	if p := strings.TrimSpace(os.Getenv(EmbedModelPathEnv)); p != "" {
		return p
	}
	if modelPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(modelPath), Qwen3ModelFilename)
}

// NewEmbedderFromFile loads a GGUF embedding model, dispatching on
// general.architecture. Only qwen3 is supported (Qwen3Embedder at full width;
// dim is ignored); any other architecture is an error.
func NewEmbedderFromFile(modelPath string, dim int) (Embedder, error) {
	arch, err := gguf.PeekArchitecture(modelPath)
	if err != nil {
		return nil, err
	}
	if arch == "qwen3" {
		return NewQwen3Embedder(modelPath, 0)
	}
	return nil, fmt.Errorf("embedding: unsupported GGUF architecture %q in %s (want qwen3)", arch, modelPath)
}

// NewDefaultEmbedder loads the local embedder (Qwen3) from the model slot
// through LoadPreferredEmbedder (path resolution, low-memory guard, failure
// backoff, panic-safe load). If initialization fails (model not found, etc.),
// it silently returns NoopEmbedder.
func NewDefaultEmbedder(modelPath string) Embedder {
	res, _ := LoadPreferredEmbedder(modelPath, PreferredOptions{})
	return res.Embedder
}
