package embedding

import (
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
// NewDefaultEmbedder (and therefore SharedGemma256) and LoadPreferredEmbedder:
//
//	unset / "qwen3" / "auto"   Qwen3-Embedding-0.6B (Q8_0 GGUF, the single
//	                            shipped model), located by ResolveQwen3ModelPath
//	"gemma"                    EmbeddingGemma-300M (explicit rollback pin)
//
// Switching changes ModelID, so stored vectors and intent anchor caches are
// re-embedded rather than mixed.
const EmbedModelEnv = "MACLAW_EMBED_MODEL"

// EmbedModelPathEnv overrides the model file used when EmbedModelEnv=qwen3.
const EmbedModelPathEnv = "MACLAW_EMBED_MODEL_PATH"

// SelectedEmbedModel returns the normalised EmbedModelEnv value ("gemma" or
// "qwen3"). Qwen3 is the default; only an explicit "gemma" selects Gemma.
func SelectedEmbedModel() string {
	if GemmaPinned() {
		return "gemma"
	}
	return "qwen3"
}

// resolveModelPath maps the caller's (Gemma) model path onto the file of the
// selected model family.
func resolveModelPath(modelPath string) string {
	if SelectedEmbedModel() != "qwen3" {
		return modelPath
	}
	if p := strings.TrimSpace(os.Getenv(EmbedModelPathEnv)); p != "" {
		return p
	}
	if modelPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(modelPath), Qwen3ModelFilename)
}

// NewEmbedderFromFile loads a GGUF embedding model, dispatching on
// general.architecture (qwen3 → Qwen3Embedder at full width, everything else
// → GemmaEmbedder at dim).
func NewEmbedderFromFile(modelPath string, dim int) (Embedder, error) {
	arch, err := gguf.PeekArchitecture(modelPath)
	if err != nil {
		return nil, err
	}
	if arch == "qwen3" {
		return NewQwen3Embedder(modelPath, 0)
	}
	return NewGemmaEmbedder(modelPath, dim)
}

// NewDefaultEmbedder creates the configured local embedder from modelPath
// (see EmbedModelEnv): Qwen3 through LoadPreferredEmbedder (path resolution,
// low-memory guard, failure backoff, panic-safe load), or Gemma when pinned.
// If initialization fails (model not found, etc.), it silently falls back to
// NoopEmbedder.
func NewDefaultEmbedder(modelPath string) Embedder {
	if SelectedEmbedModel() == "qwen3" {
		res, _ := LoadPreferredEmbedder(modelPath, PreferredOptions{})
		return res.Embedder
	}
	modelPath = resolveModelPath(modelPath)
	if _, err := os.Stat(modelPath); err != nil {
		return NoopEmbedder{}
	}
	emb, err := NewEmbedderFromFile(modelPath, DefaultEmbeddingDim)
	if err != nil {
		return NoopEmbedder{}
	}
	return emb
}
