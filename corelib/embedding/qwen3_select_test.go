package embedding

import (
	"path/filepath"
	"testing"
)

func TestResolveModelPathSelectsFamily(t *testing.T) {
	gemma := filepath.Join("/m", LegacyGemmaModelFilename)
	t.Setenv(EmbedModelEnv, "")
	t.Setenv(EmbedModelPathEnv, "")
	if got := resolveModelPath(gemma); got != filepath.Join("/m", Qwen3ModelFilename) {
		t.Fatalf("default family is Qwen3 (single shipped model), got %q", got)
	}
	t.Setenv(EmbedModelEnv, "gemma")
	if got := resolveModelPath(gemma); got != filepath.Join("/m", Qwen3ModelFilename) {
		t.Fatalf("retired gemma pin must be ignored (Qwen3 path), got %q", got)
	}
	t.Setenv(EmbedModelEnv, "qwen3")
	if got := resolveModelPath(gemma); got != filepath.Join("/m", Qwen3ModelFilename) {
		t.Fatalf("qwen3 family path = %q", got)
	}
	t.Setenv(EmbedModelPathEnv, "/x/q.gguf")
	if got := resolveModelPath(gemma); got != "/x/q.gguf" {
		t.Fatalf("explicit qwen3 path = %q", got)
	}
}

func TestNewEmbedderFromFileDispatchesQwen3(t *testing.T) {
	p := qwen3TestModel(t)
	e, err := NewEmbedderFromFile(p, DefaultEmbeddingDim)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	q, ok := e.(*Qwen3Embedder)
	if !ok {
		t.Fatalf("got %T, want *Qwen3Embedder", e)
	}
	if q.Dim() != 1024 {
		t.Fatalf("qwen3 must load at full width, Dim=%d", q.Dim())
	}
	var _ RoleEmbedder = q
}
