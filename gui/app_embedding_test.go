package main

import (
	"sync/atomic"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

type appEmbeddingTestEmbedder struct {
	closed atomic.Bool
}

func (e *appEmbeddingTestEmbedder) Embed(string) ([]float32, error) {
	return []float32{1, 0, 0}, nil
}

func (e *appEmbeddingTestEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{1, 0, 0}
	}
	return out, nil
}

func (e *appEmbeddingTestEmbedder) Dim() int { return 3 }

func (e *appEmbeddingTestEmbedder) Close() { e.closed.Store(true) }

func TestFullEmbeddingActivationReusesIntentEmbedder(t *testing.T) {
	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache:      corelib.AppConfig{VectorSearchEnabled: true},
	}

	intentEmb := &appEmbeddingTestEmbedder{}
	app.activateIntentClassifierEmbedderAsync(intentEmb)
	if app.intentEmbedder != intentEmb {
		t.Fatalf("intent embedder was not retained")
	}
	if !app.intentEmbeddingActive.Load() {
		t.Fatalf("intent embedding should be active")
	}

	fullEmb := &appEmbeddingTestEmbedder{}
	app.activateEmbedderAsync(fullEmb)
	if app.intentEmbedder != intentEmb {
		t.Fatalf("full activation should reuse intent embedder")
	}
	if !fullEmb.closed.Load() {
		t.Fatalf("redundant full embedder should be closed")
	}
	if !app.embeddingActivated.Load() {
		t.Fatalf("full embedding activation flag should be set")
	}
}
