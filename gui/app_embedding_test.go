package main

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
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

func TestSharedEmbeddingEmbedderSerializesConcurrentLoads(t *testing.T) {
	app := &App{}
	var loads atomic.Int32
	const callers = 8

	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make(chan embedding.Embedder, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			emb, err := app.sharedEmbeddingEmbedder("model.gguf", func(string) (embedding.Embedder, error) {
				loads.Add(1)
				return &appEmbeddingTestEmbedder{}, nil
			})
			if err != nil {
				t.Errorf("sharedEmbeddingEmbedder returned error: %v", err)
				return
			}
			results <- emb
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	if loads.Load() != 1 {
		t.Fatalf("expected one embedder load, got %d", loads.Load())
	}
	var first embedding.Embedder
	for emb := range results {
		if first == nil {
			first = emb
			continue
		}
		if !sameEmbedder(first, emb) {
			t.Fatalf("expected all callers to receive the same embedder")
		}
	}
}

func TestIntentActivationDoesNotCloseSharedEmbedder(t *testing.T) {
	app := &App{}
	emb := &appEmbeddingTestEmbedder{}
	app.intentEmbedder = emb

	app.activateIntentClassifierEmbedderAsync(emb)

	if emb.closed.Load() {
		t.Fatalf("shared embedder should not be closed during intent activation")
	}
	if app.intentEmbedder != emb {
		t.Fatalf("shared embedder should remain cached")
	}
	if !app.intentEmbeddingActive.Load() {
		t.Fatalf("intent embedding should be active")
	}
}

func TestFullActivationTreatsCachedButInactiveEmbedderAsNotReused(t *testing.T) {
	app := &App{}
	emb := &appEmbeddingTestEmbedder{}
	app.intentEmbedder = emb

	claimed, reusedIntent := app.claimEmbeddingForFullActivation(emb)

	if claimed != emb {
		t.Fatalf("expected cached embedder to be claimed")
	}
	if reusedIntent {
		t.Fatalf("cached but inactive embedder should still be wired during full activation")
	}
	if emb.closed.Load() {
		t.Fatalf("cached embedder should not be closed")
	}
}
