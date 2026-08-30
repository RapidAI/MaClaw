package intent

import (
	"context"
	"strings"
	"sync/atomic"
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
		{Label: LabelNonCoding, Vecs: [][]float32{{0.87, 0.493052}}},
		{Label: LabelLiveData, Vecs: [][]float32{{0.85, 0.526783}}},
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

// A confident search hit with a material document_generate companion must not
// collapse to a plain lookup: the search pair is not locally verified, but
// treating the turn as search-only silently drops the artifact capability and
// the loop later reports the generate tool as unavailable.  Escalation keeps
// the generate half as evidence so the tree verdict can synthesize the
// composite.  This shape reproduces "搜索最新的AI新闻并生成PDF报告" on the
// production model (search 0.837, document_generate 0.769).
func TestClassifyByEmbeddingSearchPdfCompositeDoesNotCollapseToPlainLookup(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	result, confident := classifyByEmbedding(emb, []intentAnchor{
		{Label: LabelSearch, Vecs: [][]float32{{0.85, 0.526783}}},
		{Label: LabelDocumentGenerate, Vecs: [][]float32{{0.77, 0.638045}}},
		{Label: LabelNonCoding, Vecs: [][]float32{{0.60, 0.80}}},
	}, "搜索最新的AI新闻并生成PDF报告")
	if confident || result.Primary != LabelSearch || len(result.Secondary) != 1 || result.Secondary[0] != LabelDocumentGenerate {
		t.Fatalf("result=%+v confident=%v, want search+PDF escalation keeping generate evidence", result, confident)
	}
	if !strings.Contains(result.Reason, "ambiguous composite") {
		t.Fatalf("reason=%q, want ambiguous composite escalation", result.Reason)
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
		{Label: LabelNonCoding, Vecs: [][]float32{{0.87, 0.493052}}},
		{Label: LabelLiveData, Vecs: [][]float32{{0.85, 0.526783}}},
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
	// The lookup guess carries document_generate evidence, so a tree timeout
	// keeps the turn explicitly unconfirmed rather than degrading to a bare
	// lookup hint that would silently drop the requested artifact.
	if first.Primary != LabelUnknown || !first.Degraded {
		t.Fatalf("first = %+v, want unconfirmed unknown for an unverifiable composite", first)
	}
	embeds := emb.queries
	second := uic.Classify(MessageContext{Text: query})
	if emb.queries <= embeds {
		t.Fatalf("L3 timeout result must not be cached; embeds stayed at %d", emb.queries)
	}
	if second.Primary != first.Primary || !second.Degraded {
		t.Fatalf("second = %+v, want the same uncached unconfirmed family as %+v", second, first)
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
	if result.Primary != LabelUnknown || !result.Degraded {
		t.Fatalf("result = %+v, want unconfirmed unknown when the tree cannot rule on the composite", result)
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
		{Label: LabelLiveData, Vecs: [][]float32{{0.85, 0.526783}}},
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
	// A lookup guess with a declared artifact half stays explicitly
	// unconfirmed: degrading to a bare hint would silently reduce a
	// composite request to lookup-only.
	composite := lookupHintOrUnknownFromL2(ClassificationResult{
		Primary: LabelLiveData, Confidence: 0.61, Secondary: []IntentLabel{LabelDocumentGenerate},
	}, true)
	if composite.Primary != LabelUnknown || !composite.Degraded {
		t.Fatalf("composite-evidence lookup = %+v, want unconfirmed unknown", composite)
	}

	// A plain lookup guess keeps the degraded hint so routing can chat
	// without HostReject.
	lookup := lookupHintOrUnknownFromL2(ClassificationResult{
		Primary: LabelLiveData, Confidence: 0.61,
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

	// An office guess at the lookup floor keeps a governed hint: it plans
	// through the office capability surface instead of stripping document
	// tools off the turn when the tree times out.
	office := lookupHintOrUnknownFromL2(ClassificationResult{Primary: LabelOffice, Confidence: 0.75}, false)
	if !office.Degraded || office.Primary != LabelOffice || office.Confidence != 0.75 {
		t.Fatalf("office hint = %+v, want degraded office hint", office)
	}
	if len(office.Secondary) != 0 || len(office.ToolNames) != 0 || office.WorkflowType != "" {
		t.Fatalf("office hint leaked adjuncts: %+v", office)
	}

	// Sub-floor office guesses and office composites with a declared artifact
	// half still collapse to unknown.
	weakOffice := lookupHintOrUnknownFromL2(ClassificationResult{Primary: LabelOffice, Confidence: 0.65}, false)
	if !weakOffice.Degraded || weakOffice.Primary != LabelUnknown {
		t.Fatalf("sub-floor office = %+v, want unknown", weakOffice)
	}
	officeComposite := lookupHintOrUnknownFromL2(ClassificationResult{
		Primary: LabelOffice, Confidence: 0.75, Secondary: []IntentLabel{LabelDocumentGenerate},
	}, false)
	if !officeComposite.Degraded || officeComposite.Primary != LabelUnknown {
		t.Fatalf("office composite = %+v, want unknown", officeComposite)
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

func TestLateTreeVerdictCachesForRepeatedRequest(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0, 0}}
	var calls atomic.Int32
	uic := New(Config{
		Embedder: emb,
		LLMContextFunc: func(ctx context.Context, _, _ string) (string, error) {
			n := calls.Add(1)
			if n == 1 {
				// The synchronous attempt loses to the fusion deadline.
				<-ctx.Done()
				return "", ctx.Err()
			}
			// The background retry answers promptly.
			return `{"top":[{"skill":"office","score":0.92}]}`, nil
		},
		FusionTreeDeadline: 30 * time.Millisecond,
		LLMTimeout:         2 * time.Second,
	})
	uic.mu.Lock()
	uic.ready = true
	uic.anchors = []intentAnchor{
		{Label: LabelLiveData, Vecs: [][]float32{{1, 0, 0}}},
		{Label: LabelSearch, Vecs: [][]float32{{1, 0, 0}}},
	}
	uic.mu.Unlock()

	first := uic.ClassifyContext(context.Background(), MessageContext{Text: "生成庆祝生日会的PPT"})
	if !first.Degraded {
		t.Fatalf("first = %+v, want degraded hint after fusion timeout", first)
	}
	// The late verdict lands asynchronously and is cached under the same key.
	deadline := time.Now().Add(3 * time.Second)
	for {
		var second ClassificationResult
		if cached, ok := uic.cache.Load(classificationCacheKey(uic.cacheEpoch.Load(), MessageContext{Text: "生成庆祝生日会的PPT"})); ok && cached != nil {
			if verdict, ok := cached.(*ClassificationResult); ok && verdict != nil && !verdict.Degraded {
				second = *verdict
			}
		}
		if second.Primary == LabelOffice {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("late tree verdict never cached; calls=%d", calls.Load())
		}
		time.Sleep(10 * time.Millisecond)
	}
	// A repeated request is served by the warm cache without a new LLM call.
	before := calls.Load()
	again := uic.ClassifyContext(context.Background(), MessageContext{Text: "生成庆祝生日会的PPT"})
	if again.Primary != LabelOffice || again.Degraded || again.Layer != 3 {
		t.Fatalf("repeat = %+v, want cached tree office verdict", again)
	}
	if calls.Load() != before {
		t.Fatalf("repeat paid another LLM call: %d -> %d", before, calls.Load())
	}
}

// A background tree verdict that grossly contradicts the local channel must
// not be cached: the resend it was meant to rescue would otherwise be routed
// by one bad LLM sample (2026-08-25 production: "生成…ppt…网上找…照片"
// cached coding 0.95 over a local office leader).
func TestLateTreeVerdictContradictedByLocalIsNotCached(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0, 0}}
	var calls atomic.Int32
	uic := New(Config{
		Embedder: emb,
		LLMContextFunc: func(ctx context.Context, _, _ string) (string, error) {
			n := calls.Add(1)
			if n == 1 {
				// The synchronous attempt loses to the fusion deadline.
				<-ctx.Done()
				return "", ctx.Err()
			}
			if n == 2 {
				// The background retry answers promptly, but with a gross misroll.
				return `{"top":[{"skill":"coding","score":0.95,"workflow_type":"coding"}]}`, nil
			}
			// The repeat request re-classifies; the fresh tree rules correctly.
			return `{"top":[{"skill":"office","score":0.92}]}`, nil
		},
		FusionTreeDeadline: 30 * time.Millisecond,
		LLMTimeout:         2 * time.Second,
	})
	uic.mu.Lock()
	uic.ready = true
	uic.anchors = []intentAnchor{
		{Label: LabelOffice, Vecs: [][]float32{{1, 0, 0}}},
		// A second non-pair label tied with office keeps L2 ambiguous so the
		// turn escalates to the tree; either leader discards a coding verdict.
		{Label: LabelDocumentRead, Vecs: [][]float32{{1, 0, 0}}},
		{Label: LabelCoding, Vecs: [][]float32{{0, 1, 0}}},
	}
	uic.mu.Unlock()

	text := "生成庆祝生日会的PPT"
	first := uic.ClassifyContext(context.Background(), MessageContext{Text: text})
	if !first.Degraded {
		t.Fatalf("first = %+v, want degraded hint after fusion timeout", first)
	}
	// Give the background verdict time to land; it must be discarded.
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if calls.Load() < 2 {
		t.Fatalf("background late verdict never ran; calls=%d", calls.Load())
	}
	time.Sleep(100 * time.Millisecond)
	if cached, ok := uic.cache.Load(classificationCacheKey(uic.cacheEpoch.Load(), MessageContext{Text: text})); ok && cached != nil {
		if verdict, ok := cached.(*ClassificationResult); ok && verdict != nil && !verdict.Degraded && verdict.Primary == LabelCoding {
			t.Fatalf("contradicted late verdict was cached: %+v", verdict)
		}
	}
	// The repeat is re-classified instead of served the poisoned verdict, and
	// the fresh tree ruling recovers the office route.
	before := calls.Load()
	again := uic.ClassifyContext(context.Background(), MessageContext{Text: text})
	if again.Primary != LabelOffice || again.Degraded {
		t.Fatalf("repeat = %+v, want fresh office route, not a poisoned coding one", again)
	}
	if calls.Load() <= before {
		t.Fatalf("repeat was served from cache despite the discarded verdict: calls=%d", calls.Load())
	}
}

// A confidently wrong synchronous tree verdict must not route the turn: the
// 2026-08-26 production turn classified "生成…ppt…网上找…照片" as browser
// 0.90 and died at plan rejection (no feasible browser provider), refusing
// the whole request. The cross-check falls back to the L2 hint exactly like
// a tree timeout.
func TestSyncTreeVerdictContradictedByLocalFallsBackToL2Hint(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0, 0}}
	var calls atomic.Int32
	uic := New(Config{
		Embedder: emb,
		LLMContextFunc: func(ctx context.Context, _, _ string) (string, error) {
			n := calls.Add(1)
			if n == 1 {
				// The live tree ruling is confident and grossly wrong.
				return `{"top":[{"skill":"browser","score":0.90}]}`, nil
			}
			// The scheduled late verdict gets a second sample; a good one is
			// cacheable, but this test never resends, so any answer is fine.
			return `{"top":[{"skill":"office","score":0.92}]}`, nil
		},
		FusionTreeDeadline: 2 * time.Second,
		LLMTimeout:         2 * time.Second,
	})
	uic.mu.Lock()
	uic.ready = true
	uic.anchors = []intentAnchor{
		{Label: LabelOffice, Vecs: [][]float32{{1, 0, 0}}},
		{Label: LabelDocumentRead, Vecs: [][]float32{{1, 0, 0}}},
		{Label: LabelBrowser, Vecs: [][]float32{{0, 1, 0}}},
	}
	uic.mu.Unlock()

	text := "生成庆祝生日会的PPT"
	result := uic.ClassifyContext(context.Background(), MessageContext{Text: text})
	if result.Primary != LabelOffice || !result.Degraded {
		t.Fatalf("result = %+v, want degraded office hint, not the contradicted browser verdict", result)
	}
	if !strings.Contains(result.Reason, "contradicted by local leader") {
		t.Fatalf("reason = %q, want contradicted-by-local explanation", result.Reason)
	}
	if cached, ok := uic.cache.Load(classificationCacheKey(uic.cacheEpoch.Load(), MessageContext{Text: text})); ok && cached != nil {
		if verdict, ok := cached.(*ClassificationResult); ok && verdict != nil && !verdict.Degraded && verdict.Primary == LabelBrowser {
			t.Fatalf("contradicted verdict was cached: %+v", verdict)
		}
	}
}
