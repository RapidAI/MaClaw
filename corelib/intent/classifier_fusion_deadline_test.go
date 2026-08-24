package intent

import (
	"context"
	"strings"
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
		LLMTimeout:         30 * time.Second,
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
		t.Fatalf("Classify took %v; should cap tree wait at FusionTreeDeadline ~40ms + margin, not LLMTimeout 30s", elapsed)
	}
	if !isDegradedLookupHint(result) {
		// Tree timed out; unconfirmed L2 stays a hint, not a confirmed capability.
		t.Fatalf("expected degraded lookup hint after tree timeout, got %+v", result)
	}
}

func TestClassifyContextCancelledDoesNotWaitOnTree(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	uic := New(Config{LLMFunc: hangLLM, LLMTimeout: 30 * time.Second})
	start := time.Now()
	result := uic.ClassifyContext(ctx, MessageContext{Text: "北京天气"})
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("cancelled ClassifyContext waited %v", elapsed)
	}
	if !result.Degraded || result.Primary != LabelUnknown || !strings.Contains(result.Reason, "cancelled") {
		t.Fatalf("result = %+v, want cancelled unknown", result)
	}
}

func TestClassifyTreeTimeoutDoesNotPromoteUnconfirmedL2(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0, 0}}
	uic := New(Config{
		Embedder:           emb,
		LLMFunc:            hangLLM,
		LLMTimeout:         30 * time.Second,
		FusionTreeDeadline: 30 * time.Millisecond,
	})
	uic.mu.Lock()
	uic.ready = true
	uic.anchors = []intentAnchor{
		{Label: LabelLiveData, Vecs: [][]float32{{1, 0, 0}}},
		{Label: LabelSearch, Vecs: [][]float32{{1, 0, 0}}},
	}
	uic.mu.Unlock()

	result := uic.Classify(MessageContext{Text: "帮我分析一下这个现象到底应该怎么理解比较好"})
	if !isDegradedLookupHint(result) {
		t.Fatalf("result = %+v, want degraded lookup hint instead of unknown or confirmed L2", result)
	}
	if !strings.Contains(result.Reason, "tree classification unavailable") {
		t.Fatalf("reason=%q, want tree-unavailable fallback", result.Reason)
	}
}

func TestClassifyShortWeakLookupCachesWithoutLLM(t *testing.T) {
	emb := &queryCountingEmbedder{staticEmbedder: staticEmbedder{vec: []float32{1, 0}}, query: "北京天所"}
	uic := New(Config{Embedder: emb})
	uic.mu.Lock()
	uic.ready = true
	uic.anchors = []intentAnchor{
		{Label: LabelLiveData, Vecs: [][]float32{{0.61, 0.792}}},
		{Label: LabelSearch, Vecs: [][]float32{{0.50, 0.866}}},
	}
	uic.mu.Unlock()

	first := uic.Classify(MessageContext{Text: "北京天所"})
	if !isDegradedLookupHint(first) || first.Primary != LabelLiveData {
		t.Fatalf("first = %+v, want cached skip-tree hint", first)
	}
	if emb.queries != 1 {
		t.Fatalf("query embeds = %d, want 1", emb.queries)
	}
	again := uic.Classify(MessageContext{Text: "北京天所"})
	if again.Reason != first.Reason || emb.queries != 1 {
		t.Fatalf("embedding-only skip-tree must cache, again=%+v embeds=%d", again, emb.queries)
	}
}

func TestClassifyShortWeakLookupDoesNotCallLLM(t *testing.T) {
	emb := &queryCountingEmbedder{staticEmbedder: staticEmbedder{vec: []float32{1, 0}}, query: "北京天所"}
	llmCalls := 0
	uic := New(Config{
		Embedder: emb,
		LLMFunc: func(_, _ string) (string, error) {
			llmCalls++
			return `{"top":[{"skill":"live_data","score":0.99}]}`, nil
		},
	})
	uic.mu.Lock()
	uic.ready = true
	uic.anchors = []intentAnchor{
		{Label: LabelLiveData, Vecs: [][]float32{{0.61, 0.792}}},
		{Label: LabelSearch, Vecs: [][]float32{{0.50, 0.866}}},
	}
	uic.mu.Unlock()

	result := uic.Classify(MessageContext{Text: "北京天所"})
	if llmCalls != 0 {
		t.Fatalf("LLM calls = %d, want 0 for a short unconfirmed lookup", llmCalls)
	}
	if !isDegradedLookupHint(result) || result.Primary != LabelLiveData {
		t.Fatalf("result = %+v, want degraded live_data hint", result)
	}
	if !strings.Contains(result.Reason, "short lookup skipped tree") {
		t.Fatalf("reason=%q, want short lookup to skip L3", result.Reason)
	}
	if emb.queries != 1 {
		t.Fatalf("query embeds = %d, want 1", emb.queries)
	}
	again := uic.Classify(MessageContext{Text: "北京天所"})
	if again.Primary != result.Primary || again.Reason != result.Reason {
		t.Fatalf("cached = %+v, want %+v", again, result)
	}
	if emb.queries != 1 {
		t.Fatalf("query embeds after cache hit = %d, want 1", emb.queries)
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

func TestClassifyByEmbeddingLiveDataLookupSkipsLayer3(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	result, confident := classifyByEmbedding(emb, []intentAnchor{
		{Label: LabelLiveData, Vecs: [][]float32{{0.73, 0.683389}}},
		{Label: LabelCoding, Vecs: [][]float32{{0.65, 0.759934}}},
	}, "重庆天气")
	if !confident || result.Primary != LabelLiveData || result.Layer != 2 {
		t.Fatalf("result=%+v confident=%v, want confident Layer 2 live_data lookup", result, confident)
	}
	if !strings.Contains(result.Reason, "embedding lookup") {
		t.Fatalf("reason=%q, want lookup shortcut", result.Reason)
	}
}

func TestClassifyByEmbeddingWeatherPdfDoesNotSkipLayer3(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	result, confident := classifyByEmbedding(emb, []intentAnchor{
		{Label: LabelLiveData, Vecs: [][]float32{{0.73, 0.683389}}},
		{Label: LabelDocumentGenerate, Vecs: [][]float32{{0.65, 0.759934}}},
	}, "查询南京天气，并生成pdf报告")
	if confident || result.Primary != LabelLiveData {
		t.Fatalf("result=%+v confident=%v, want L3 escalation for lookup+PDF", result, confident)
	}
}

func TestClassifyByEmbeddingVerifiedDocumentGenerateWithLookupCompanion(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	result, confident := classifyByEmbedding(emb, []intentAnchor{
		// These scores reproduce the production shape: document generation wins
		// clearly, a generic intent is the runner-up, and the lookup companion is
		// still semantically material but not top-2.
		{Label: LabelDocumentGenerate, Vecs: [][]float32{{0.947, 0.321234}}},
		{Label: LabelNonCoding, Vecs: [][]float32{{0.805, 0.593275}}},
		{Label: LabelLiveData, Vecs: [][]float32{{0.75, 0.661438}}},
	}, "render the requested result as a file")
	if !confident || result.Primary != LabelDocumentGenerate || len(result.Secondary) != 1 || result.Secondary[0] != LabelLiveData {
		t.Fatalf("result=%+v confident=%v, want verified document-generate + live-data evidence", result, confident)
	}
	if !strings.Contains(result.Reason, "embedding declared composite") {
		t.Fatalf("reason=%q, want verified declared composite", result.Reason)
	}
}

func TestClassifyByEmbeddingSearchAndDocumentGenerateDoesNotSkipLayer3(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	result, confident := classifyByEmbedding(emb, []intentAnchor{
		{Label: LabelDocumentGenerate, Vecs: [][]float32{{0.947, 0.321234}}},
		{Label: LabelSearch, Vecs: [][]float32{{0.710, 0.704202}}},
	}, "render externally supplied reference material as a file")
	if confident || result.Primary != LabelDocumentGenerate || len(result.Secondary) != 0 {
		t.Fatalf("result=%+v confident=%v, want L3 authority for search + document_generate", result, confident)
	}
}

func TestClassifyVerifiedEmbeddingCompositeDoesNotDependOnTree(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	llmCalls := 0
	uic := New(Config{
		Embedder: emb,
		LLMFunc: func(_, _ string) (string, error) {
			llmCalls++
			// This reproduces the malformed model output seen in production.  A
			// verified local composite must not give this unrelated control-plane
			// response authority over the turn.
			return `{"top":[{"skill":"coding","score":0.8,"workflow_type":"contract_review"}]}`, nil
		},
	})
	uic.mu.Lock()
	uic.ready = true
	uic.anchors = []intentAnchor{
		{Label: LabelDocumentGenerate, Vecs: [][]float32{{0.947, 0.321234}}},
		{Label: LabelNonCoding, Vecs: [][]float32{{0.805, 0.593275}}},
		{Label: LabelLiveData, Vecs: [][]float32{{0.75, 0.661438}}},
	}
	uic.mu.Unlock()

	result := uic.Classify(MessageContext{Text: "render the requested result as a file"})
	if llmCalls != 0 || result.Layer != 2 || result.Primary != LabelLiveData || len(result.Secondary) != 1 || result.Secondary[0] != LabelDocumentGenerate {
		t.Fatalf("result=%+v llmCalls=%d, want verified embedding lookup + document_generate", result, llmCalls)
	}
	if result.ControlPlaneFailure || result.Degraded {
		t.Fatalf("verified composite must remain executable despite a possible tree failure: %+v", result)
	}
}

func TestClassifyByEmbeddingStandaloneDocumentGenerateDoesNotSkipLayer3(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	result, confident := classifyByEmbedding(emb, []intentAnchor{
		{Label: LabelDocumentGenerate, Vecs: [][]float32{{0.947, 0.321234}}},
		{Label: LabelNonCoding, Vecs: [][]float32{{0.805, 0.593275}}},
		{Label: LabelLiveData, Vecs: [][]float32{{0.650, 0.759934}}},
	}, "render the supplied material as a file")
	if confident || result.Primary != LabelDocumentGenerate || len(result.Secondary) != 0 {
		t.Fatalf("result=%+v confident=%v, want tree escalation without a lookup companion", result, confident)
	}
}

func TestClassifyTreeKeepsStandaloneDocumentGenerate(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	uic := New(Config{
		Embedder: emb,
		LLMFunc: func(_, _ string) (string, error) {
			return `{"top":[{"skill":"document_generate","score":0.95}]}`, nil
		},
	})
	uic.mu.Lock()
	uic.ready = true
	uic.anchors = []intentAnchor{
		{Label: LabelDocumentGenerate, Vecs: [][]float32{{0.947, 0.321234}}},
		{Label: LabelNonCoding, Vecs: [][]float32{{0.805, 0.593275}}},
		{Label: LabelLiveData, Vecs: [][]float32{{0.650, 0.759934}}},
	}
	uic.mu.Unlock()

	result := uic.Classify(MessageContext{Text: "render the supplied material as a file"})
	if result.Layer != 3 || result.Primary != LabelDocumentGenerate || len(result.Secondary) != 0 {
		t.Fatalf("result=%+v, want tree-confirmed standalone document generation", result)
	}
}

func TestClassifyTreeFailureDoesNotAuthorizeDocumentGenerate(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	uic := New(Config{
		Embedder:           emb,
		LLMFunc:            hangLLM,
		LLMTimeout:         30 * time.Second,
		FusionTreeDeadline: 30 * time.Millisecond,
	})
	uic.mu.Lock()
	uic.ready = true
	uic.anchors = []intentAnchor{
		{Label: LabelDocumentGenerate, Vecs: [][]float32{{0.947, 0.321234}}},
		{Label: LabelNonCoding, Vecs: [][]float32{{0.805, 0.593275}}},
		{Label: LabelLiveData, Vecs: [][]float32{{0.650, 0.759934}}},
	}
	uic.mu.Unlock()

	result := uic.Classify(MessageContext{Text: "render the supplied material as a file"})
	if !result.Degraded || result.Primary != LabelUnknown || len(result.Secondary) != 0 || len(result.ToolNames) != 0 {
		t.Fatalf("result=%+v, want degraded unknown without document-generation authority", result)
	}
}

func TestClassifyByEmbeddingCodingStillEscalatesBelowThreshold(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	result, confident := classifyByEmbedding(emb, []intentAnchor{
		{Label: LabelCoding, Vecs: [][]float32{{0.73, 0.683389}}},
		{Label: LabelSearch, Vecs: [][]float32{{0.65, 0.759934}}},
	}, "帮我改这段代码")
	if confident || result.Primary != LabelCoding {
		t.Fatalf("result=%+v confident=%v, want ambiguous coding escalation", result, confident)
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

func TestClassifyL3TimeoutDoesNotCacheLookupHint(t *testing.T) {
	query := "查询南京天气，并生成pdf报告"
	emb := &queryCountingEmbedder{staticEmbedder: staticEmbedder{vec: []float32{1, 0}}, query: query}
	uic := New(Config{
		Embedder:           emb,
		LLMFunc:            hangLLM,
		LLMTimeout:         30 * time.Second,
		FusionTreeDeadline: 30 * time.Millisecond,
	})
	uic.mu.Lock()
	uic.ready = true
	uic.anchors = []intentAnchor{
		{Label: LabelLiveData, Vecs: [][]float32{{0.73, 0.683389}}},
		{Label: LabelDocumentGenerate, Vecs: [][]float32{{0.65, 0.759934}}},
	}
	uic.mu.Unlock()

	first := uic.Classify(MessageContext{Text: query})
	if !isDegradedLookupHint(first) || first.Primary != LabelLiveData {
		t.Fatalf("first = %+v, want live_data hint", first)
	}
	embeds := emb.queries
	second := uic.Classify(MessageContext{Text: query})
	if emb.queries <= embeds {
		t.Fatalf("L3 timeout hint must not be cached; embeds stayed at %d", emb.queries)
	}
	if !isDegradedLookupHint(second) || second.Primary != first.Primary {
		t.Fatalf("second = %+v, want the same uncached hint family as %+v", second, first)
	}
}

func TestClassifyL3TimeoutDropsUnconfirmedGenerate(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	uic := New(Config{
		Embedder:           emb,
		LLMFunc:            hangLLM,
		LLMTimeout:         30 * time.Second,
		FusionTreeDeadline: 30 * time.Millisecond,
	})
	uic.mu.Lock()
	uic.ready = true
	uic.anchors = []intentAnchor{
		{Label: LabelLiveData, Vecs: [][]float32{{0.73, 0.683389}}},
		{Label: LabelDocumentGenerate, Vecs: [][]float32{{0.65, 0.759934}}},
	}
	uic.mu.Unlock()

	result := uic.Classify(MessageContext{Text: "查询南京天气，并生成pdf报告"})
	if !isDegradedLookupHint(result) || result.Primary != LabelLiveData {
		t.Fatalf("result = %+v, want live_data hint without generate", result)
	}
	for _, label := range result.Labels() {
		if label == LabelDocumentGenerate {
			t.Fatalf("L3 timeout must drop unconfirmed generate, got %+v", result)
		}
	}
}

func TestClassifyWeatherPDFKeepsVerifiedLocalCompositeWhenTreeWouldMisclassify(t *testing.T) {
	const query = "北京天气，输出 格式化pdf报告"
	llmCalls := 0
	uic := New(Config{
		Embedder: &staticEmbedder{vec: []float32{1, 0}},
		LLMFunc: func(_, _ string) (string, error) {
			llmCalls++
			return `{"top":[{"skill":"web_fetch","score":1.0}]}`, nil
		},
	})
	uic.mu.Lock()
	uic.ready = true
	uic.anchors = []intentAnchor{
		{Label: LabelDocumentGenerate, Vecs: [][]float32{{0.80, 0.60}}},
		{Label: LabelLiveData, Vecs: [][]float32{{0.75, 0.661438}}},
	}
	uic.mu.Unlock()

	result := uic.Classify(MessageContext{Text: query})
	if llmCalls != 0 {
		t.Fatalf("verified local composite must not be overwritten by tree, LLM calls=%d", llmCalls)
	}
	if result.Primary != LabelLiveData || len(result.Secondary) != 1 || result.Secondary[0] != LabelDocumentGenerate {
		t.Fatalf("result=%+v, want live_data + document_generate", result)
	}
}

func TestClassifyTreeProtocolViolationIsNotAnUnknownUserIntent(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	uic := New(Config{
		Embedder: emb,
		LLMFunc: func(_, _ string) (string, error) {
			return "I cannot access live weather data.", nil
		},
	})
	uic.mu.Lock()
	uic.ready = true
	uic.anchors = []intentAnchor{
		{Label: LabelLiveData, Vecs: [][]float32{{0.73, 0.683389}}},
		{Label: LabelDocumentGenerate, Vecs: [][]float32{{0.65, 0.759934}}},
	}
	uic.mu.Unlock()

	result := uic.Classify(MessageContext{Text: "查询南京天气，并生成pdf报告"})
	if !result.Degraded || !result.ControlPlaneFailure || result.Primary != LabelUnknown {
		t.Fatalf("result=%+v, want control-plane protocol failure", result)
	}
	if len(result.Secondary) != 0 || len(result.ToolNames) != 0 {
		t.Fatalf("protocol failure leaked capability authority: %+v", result)
	}
}

func TestLookupHintOrUnknownFromL2KeepsOnlySearchFamilies(t *testing.T) {
	lookup := lookupHintOrUnknownFromL2(ClassificationResult{
		Primary: LabelLiveData, Confidence: 0.61, Secondary: []IntentLabel{LabelDocumentGenerate},
	}, true)
	if !isDegradedLookupHint(lookup) || lookup.Primary != LabelLiveData {
		t.Fatalf("lookup = %+v, want live_data hint", lookup)
	}
	if len(lookup.Secondary) != 0 || len(lookup.ToolNames) != 0 || lookup.WorkflowType != "" {
		t.Fatalf("hint leaked adjuncts: %+v", lookup)
	}

	other := lookupHintOrUnknownFromL2(ClassificationResult{Primary: LabelFileRead, Confidence: 0.66}, false)
	if !other.Degraded || other.Primary != LabelUnknown {
		t.Fatalf("non-lookup = %+v, want unknown", other)
	}
}

func isDegradedLookupHint(result ClassificationResult) bool {
	if !result.Degraded {
		return false
	}
	if result.Primary != LabelSearch && result.Primary != LabelLiveData {
		return false
	}
	return len(result.Secondary) == 0 && len(result.ToolNames) == 0 && result.WorkflowType == ""
}

type queryCountingEmbedder struct {
	staticEmbedder
	query   string
	queries int
}

func (q *queryCountingEmbedder) Embed(text string) ([]float32, error) {
	if text == q.query {
		q.queries++
	}
	return q.staticEmbedder.Embed(text)
}
