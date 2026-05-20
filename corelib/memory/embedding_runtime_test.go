package memory

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

type runtimeEmbedderTestFake struct{}

func (runtimeEmbedderTestFake) Embed(text string) ([]float32, error) { return []float32{1, 0}, nil }

func (runtimeEmbedderTestFake) EmbedBatch(texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{1, 0}
	}
	return out, nil
}

func (runtimeEmbedderTestFake) Dim() int { return 2 }

func (runtimeEmbedderTestFake) Close() {}

func TestRuntimeEmbedderForHostFiltersNilAndNoop(t *testing.T) {
	store, err := NewStoreWithMode(t.TempDir(), StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode: %v", err)
	}
	defer store.Stop()

	if emb, ok := store.RuntimeEmbedderForHost(); ok || emb != nil {
		t.Fatalf("expected no runtime embedder before SetEmbedder, got ok=%v emb=%T", ok, emb)
	}
	store.SetEmbedder(embedding.NoopEmbedder{})
	if emb, ok := store.RuntimeEmbedderForHost(); ok || emb != nil {
		t.Fatalf("expected noop embedder to be hidden, got ok=%v emb=%T", ok, emb)
	}
}

func TestRuntimeEmbedderForHostReturnsActiveEmbedder(t *testing.T) {
	store, err := NewStoreWithMode(t.TempDir(), StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode: %v", err)
	}
	defer store.Stop()

	store.SetEmbedder(runtimeEmbedderTestFake{})
	if emb, ok := store.RuntimeEmbedderForHost(); !ok || emb == nil {
		t.Fatalf("expected active runtime embedder, got ok=%v emb=%T", ok, emb)
	}
}
func TestRuntimeEmbedderStatusForHostUsesCorelibProjection(t *testing.T) {
	store, err := NewStoreWithMode(t.TempDir(), StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode: %v", err)
	}
	defer store.Stop()

	if status := store.RuntimeEmbedderStatusForHost(); status.Active || status.Dim != 0 || status.TotalEntries != 0 || status.EmbeddedEntries != 0 {
		t.Fatalf("unexpected empty status: %+v", status)
	}

	store.SetEmbedder(runtimeEmbedderTestFake{})
	if err := store.SaveManualMemory("remember projection status", CategoryUserFact, nil); err != nil {
		t.Fatalf("SaveManualMemory: %v", err)
	}
	status := store.RuntimeEmbedderStatusForHost()
	if !status.Active || status.Dim != 2 || status.TotalEntries != 1 || status.EmbeddedEntries != 1 {
		t.Fatalf("unexpected active status: %+v", status)
	}
}
func TestSetRuntimeEmbedderForHost(t *testing.T) {
	store, err := NewStoreWithMode(t.TempDir(), StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode: %v", err)
	}
	defer store.Stop()

	store.SetRuntimeEmbedderForHost(runtimeEmbedderTestFake{})
	if emb, ok := store.RuntimeEmbedderForHost(); !ok || emb == nil {
		t.Fatalf("expected runtime embedder after host wiring, ok=%v emb=%T", ok, emb)
	}
	store.SetRuntimeEmbedderForHost(embedding.NoopEmbedder{})
	if emb, ok := store.RuntimeEmbedderForHost(); ok || emb != nil {
		t.Fatalf("expected noop embedder hidden after host wiring, ok=%v emb=%T", ok, emb)
	}
}
