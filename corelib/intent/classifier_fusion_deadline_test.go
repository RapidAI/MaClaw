package intent

import (
	"context"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// hangLLM never returns useful content within the test window — used to prove
// fusion does not wait for the full 30s LLM timeout when L2 is available.
func hangLLM(_ string, _ string) (string, error) {
	time.Sleep(2 * time.Second)
	return `[{"label":"coding","score":0.9,"workflow_type":"coding"}]`, nil
}

func TestDefaultFusionTreeDeadlinePreservesLLMBudgetForAmbiguousRequests(t *testing.T) {
	uic := New(Config{Embedder: embedding.NoopEmbedder{}})
	if uic.FusionTreeDeadline() != DefaultFusionTreeDeadline {
		t.Fatalf("FusionTreeDeadline = %s, want %s", uic.FusionTreeDeadline(), DefaultFusionTreeDeadline)
	}
	if uic.llmTimeout != DefaultLLMTimeout {
		t.Fatalf("llmTimeout = %s, want %s", uic.llmTimeout, DefaultLLMTimeout)
	}
}

func TestFusionTreeDeadlineCapsDualChannelWait(t *testing.T) {
	// Dual-channel: noop embedder is still "present" but returns no useful scores
	// when not ready; use a real-ish path with LLM hang + short fusion deadline.
	// With Embedder=Noop, canEmb is false so Classify uses tree-only with LLMTimeout.
	// Force fusion path by providing a non-noop embedder that is not ready...
	// Actually Classify with Noop skips fusion. We call classifyWithFusion after
	// wiring a fake ready path via Config with Embedder that is non-noop.
	//
	// Use LLM-only classifyWithFusion indirectly: construct UIC with LLM +
	// FusionTreeDeadline short, and a non-noop embedder that fails Embed so L2
	// returns empty → still fusion with emb fail. Better: use embedding.Noop
	// and call Classify — that is tree-only.
	//
	// Instead invoke classifyWithFusion through a UIC that has both channels:
	// embedder Noop is IsNoop true. We need a custom embedder.
	emb := &staticEmbedder{vec: []float32{1, 0, 0}}
	uic := New(Config{
		Embedder:           emb,
		LLMFunc:            hangLLM,
		LLMTimeout:         40 * time.Millisecond,
		FusionTreeDeadline: 40 * time.Millisecond,
	})
	// Mark ready so dual-channel fusion runs (anchors may be empty until warmup).
	uic.mu.Lock()
	uic.ready = true
	// Equal top scores keep L2 ambiguous, so this test exercises the optional
	// fusion helper rather than the embedding-first fast path.
	uic.anchors = []intentAnchor{
		{Label: LabelLiveData, Vecs: [][]float32{{1, 0, 0}}},
		{Label: LabelSearch, Vecs: [][]float32{{1, 0, 0}}},
	}
	uic.mu.Unlock()

	start := time.Now()
	result := uic.Classify(MessageContext{Text: "兰州今天天气怎么样"})
	elapsed := time.Since(start)
	if elapsed > 400*time.Millisecond {
		t.Fatalf("fusion Classify took %v; should cap tree wait at ~40ms + margin", elapsed)
	}
	if !result.Degraded {
		// Tree timed out; fusion should degrade to embedding-only.
		t.Fatalf("expected degraded after tree timeout, got %+v", result)
	}
}

func TestFusionTreeDeadlineCancelsContextAwareLLM(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0, 0}}
	canceled := make(chan struct{})
	uic := New(Config{
		Embedder: emb,
		LLMContextFunc: func(ctx context.Context, _, _ string) (string, error) {
			<-ctx.Done()
			close(canceled)
			return "", ctx.Err()
		},
		FusionTreeDeadline: 20 * time.Millisecond,
	})
	uic.mu.Lock()
	uic.ready = true
	uic.anchors = []intentAnchor{
		{Label: LabelLiveData, Vecs: [][]float32{{1, 0, 0}}},
		{Label: LabelSearch, Vecs: [][]float32{{1, 0, 0}}},
	}
	uic.mu.Unlock()

	result := uic.classifyWithFusion("weather")
	if !result.Degraded {
		t.Fatalf("expected embedding-only degraded result, got %+v", result)
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("fusion deadline did not cancel the context-aware LLM request")
	}
}

func TestClassifyUsesEmbeddingBeforeLLM(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0, 0}}
	llmCalls := 0
	uic := New(Config{
		Embedder: emb,
		LLMFunc: func(_, _ string) (string, error) {
			llmCalls++
			return `{"top":[{"skill":"search","score":0.99}]}`, nil
		},
	})
	uic.mu.Lock()
	uic.ready = true
	uic.anchors = []intentAnchor{
		{Label: LabelLiveData, Vecs: [][]float32{{1, 0, 0}}},
		{Label: LabelSearch, Vecs: [][]float32{{0, 1, 0}}},
	}
	uic.mu.Unlock()

	result := uic.Classify(MessageContext{Text: "weather"})
	if result.Layer != 2 || result.Primary != LabelLiveData {
		t.Fatalf("result = %+v, want confident Layer 2 live_data result", result)
	}
	if llmCalls != 0 {
		t.Fatalf("LLM calls = %d, want 0 for a confident embedding result", llmCalls)
	}
}

func TestClassifyEscalatesAmbiguousEmbeddingToLLM(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0, 0}}
	llmCalls := 0
	uic := New(Config{
		Embedder: emb,
		LLMFunc: func(_, _ string) (string, error) {
			llmCalls++
			return `{"top":[{"skill":"search","score":0.99}]}`, nil
		},
	})
	uic.mu.Lock()
	uic.ready = true
	uic.anchors = []intentAnchor{
		{Label: LabelLiveData, Vecs: [][]float32{{1, 0, 0}}},
		{Label: LabelSearch, Vecs: [][]float32{{1, 0, 0}}},
	}
	uic.mu.Unlock()

	result := uic.Classify(MessageContext{Text: "weather"})
	if result.Layer != 3 || result.Primary != LabelSearch {
		t.Fatalf("result = %+v, want Layer 3 search result after ambiguous embedding", result)
	}
	if llmCalls != 1 {
		t.Fatalf("LLM calls = %d, want 1 for an ambiguous embedding result", llmCalls)
	}
}

func TestSetFusionTreeDeadlineRespectsLLMTimeoutCap(t *testing.T) {
	uic := New(Config{
		LLMTimeout:         100 * time.Millisecond,
		FusionTreeDeadline: 5 * time.Second,
	})
	if uic.FusionTreeDeadline() != 100*time.Millisecond {
		t.Fatalf("FusionTreeDeadline = %s, want capped to LLMTimeout 100ms", uic.FusionTreeDeadline())
	}
	uic.SetFusionTreeDeadline(2 * time.Second)
	if uic.FusionTreeDeadline() != 100*time.Millisecond {
		t.Fatalf("after Set, FusionTreeDeadline = %s, want still capped to 100ms", uic.FusionTreeDeadline())
	}
}

// staticEmbedder is a minimal embedder for fusion deadline tests.
type staticEmbedder struct {
	vec []float32
}

func (s *staticEmbedder) Embed(text string) ([]float32, error) {
	out := make([]float32, len(s.vec))
	copy(out, s.vec)
	return out, nil
}

func (s *staticEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		v, _ := s.Embed(texts[i])
		out[i] = v
	}
	return out, nil
}

func (s *staticEmbedder) Close() {}

func (s *staticEmbedder) Dim() int { return len(s.vec) }
