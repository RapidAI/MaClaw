package intent

import (
	"context"
	"math"
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
		LLMContextFunc: func(ctx context.Context, _ context.Context, _, _ string) (string, error) {
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

func TestMatchingAnchorIdentitySkipsShortCues(t *testing.T) {
	anchors := []intentAnchor{{Label: LabelContinuation, Texts: []string{"继续", "开工", "continue"}}}
	if _, ok := matchingAnchorIdentity(anchors, "继续"); ok {
		t.Fatal("short continuation cue was locked to its exemplar")
	}
	if _, ok := matchingAnchorIdentity(anchors, "continue"); ok {
		t.Fatal("eight-letter continuation cue was locked to its exemplar")
	}
	if _, ok := matchingAnchorIdentity(anchors, "截图"); ok {
		t.Fatal("short screenshot cue was locked to its exemplar")
	}
}

func TestClassifyByEmbeddingAnchorIdentityBeatsRemoteCosine(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	result, confident := classifyByEmbedding(emb, []intentAnchor{
		{Label: LabelSSH, Texts: []string{"登录服务器查看日志"}, Vecs: [][]float32{{0.95, 0.3122}}},
		{Label: LabelFileDelete, Texts: []string{"删除刚才的markdown文件"}, Vecs: [][]float32{{0.20, 0.9798}}},
	}, "删除刚才的 markdown 文件")
	if !confident || result.Primary != LabelFileDelete {
		t.Fatalf("spaced result=%+v confident=%v, want the file_delete exemplar", result, confident)
	}
	result, confident = classifyByEmbedding(emb, []intentAnchor{
		{Label: LabelSSH, Texts: []string{"登录服务器查看日志"}, Vecs: [][]float32{{0.95, 0.3122}}},
		{Label: LabelFileDelete, Texts: []string{"删除刚才的markdown文件"}, Vecs: [][]float32{{0.20, 0.9798}}},
	}, "删除刚才的markdown文件。")
	if !confident || result.Primary != LabelFileDelete {
		t.Fatalf("punctuated result=%+v confident=%v, want the file_delete exemplar", result, confident)
	}
}

func TestClassifyByEmbeddingRemoteHostEscalatesBesideLocalDelete(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	result, confident := classifyByEmbedding(emb, []intentAnchor{
		{Label: LabelSSH, Vecs: [][]float32{{0.85, 0.526782}}},
		{Label: LabelFileDelete, Vecs: [][]float32{{0.74, 0.672606}}},
	}, "把刚才生成的说明删掉")
	if confident || result.Primary != LabelSSH || len(result.Secondary) != 1 || result.Secondary[0] != LabelFileDelete {
		t.Fatalf("result=%+v confident=%v, want tree escalation with file_delete", result, confident)
	}
}

func TestClassifyByEmbeddingRemoteHostStaysWithoutLocalNeighbor(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	result, confident := classifyByEmbedding(emb, []intentAnchor{
		{Label: LabelSSH, Vecs: [][]float32{{0.90, 0.435890}}},
		{Label: LabelCoding, Vecs: [][]float32{{0.40, 0.916515}}},
	}, "登录服务器查看日志")
	if !confident || result.Primary != LabelSSH {
		t.Fatalf("result=%+v confident=%v, want confident ssh", result, confident)
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

func TestClassifyByEmbeddingCompetingVisualEscalatesWithoutPDFToken(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	anchors := []intentAnchor{
		{Label: LabelLiveData, Vecs: [][]float32{{0.90, 0.4358898943540673}}},
		{Label: LabelDocumentGenerate, Vecs: [][]float32{{0.74, 0.6726065725724457}}},
		{Label: LabelLiveDataVisual, Vecs: [][]float32{{0.86, 0.5102938362773704}}},
	}
	// Same competing scores. Chart words, paraphrases, and unrelated wording
	// all escalate; none of them is a local chart grant.
	for _, text := range []string{
		"画出近一月股价趋势图",
		"画一张股价柱状图",
		"画个天气图",
		"看看这个柱状物",
		"最近情况如何",
	} {
		result, confident := classifyByEmbedding(emb, anchors, text)
		if confident || result.Primary != LabelLiveData || len(result.Secondary) != 0 {
			t.Fatalf("%q result=%+v confident=%v, want escalation without a local artifact", text, result, confident)
		}
		if result.RunnerUp == LabelLiveDataVisual || result.RunnerUp == LabelDocumentGenerate {
			t.Fatalf("%q runner-up %s must not reattach the cosine winner", text, result.RunnerUp)
		}
		if !strings.Contains(result.Reason, "ambiguous composite") {
			t.Fatalf("%q reason=%q, want ambiguous composite escalation", text, result.Reason)
		}
	}
}

func TestClassifyByEmbeddingNamedPDFSurvivesStrongerVisualScore(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	// Same scores as the chart replacement above. The utterance names a PDF
	// and does not name a chart, so the visual margin must not drop generate.
	result, confident := classifyByEmbedding(emb, []intentAnchor{
		{Label: LabelLiveData, Vecs: [][]float32{{0.90, 0.4358898943540673}}},
		{Label: LabelDocumentGenerate, Vecs: [][]float32{{0.74, 0.6726065725724457}}},
		{Label: LabelLiveDataVisual, Vecs: [][]float32{{0.86, 0.5102938362773704}}},
	}, "崇州天气，生成格式化pdf")
	if !confident || result.Layer != 2 || result.Primary != LabelLiveData || len(result.Secondary) != 1 || result.Secondary[0] != LabelDocumentGenerate {
		t.Fatalf("result=%+v confident=%v, want the verified PDF composite", result, confident)
	}
}

func TestClassifyByEmbeddingChartAndPDFKeepsPDFComposite(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	// Both artifacts are named. The visual score still outranks PDF, but a
	// tree round-trip is not required to keep the file the user named, and a
	// timeout must not be able to drop it.
	result, confident := classifyByEmbedding(emb, []intentAnchor{
		{Label: LabelLiveData, Vecs: [][]float32{{0.90, 0.4358898943540673}}},
		{Label: LabelDocumentGenerate, Vecs: [][]float32{{0.74, 0.6726065725724457}}},
		{Label: LabelLiveDataVisual, Vecs: [][]float32{{0.86, 0.5102938362773704}}},
	}, "画出股价趋势图并生成pdf")
	if !confident || result.Primary != LabelLiveData || len(result.Secondary) != 1 || result.Secondary[0] != LabelDocumentGenerate {
		t.Fatalf("result=%+v confident=%v, want the verified PDF composite", result, confident)
	}
}

func TestClassifyByEmbeddingVisualAboveFloorButBelowPDFEscalates(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	// Visual clears 0.73 but loses the cosine comparison. That margin used
	// to keep the PDF pair and miss a chart the user did not spell with the
	// chart word list.
	result, confident := classifyByEmbedding(emb, []intentAnchor{
		{Label: LabelLiveData, Vecs: [][]float32{{0.90, 0.4358898943540673}}},
		{Label: LabelDocumentGenerate, Vecs: [][]float32{{0.85, 0.5267826876426364}}},
		{Label: LabelLiveDataVisual, Vecs: [][]float32{{0.74, 0.6726065725724457}}},
	}, "画个天气图")
	if confident || result.Primary != LabelLiveData || len(result.Secondary) != 0 {
		t.Fatalf("result=%+v confident=%v, want escalation", result, confident)
	}
}

func TestClassifyByEmbeddingVisualLeaderStaysVisual(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	result, confident := classifyByEmbedding(emb, []intentAnchor{
		{Label: LabelLiveDataVisual, Vecs: [][]float32{{0.92, 0.39191835884530846}}},
		{Label: LabelLiveData, Vecs: [][]float32{{0.80, 0.6}}},
		{Label: LabelDocumentGenerate, Vecs: [][]float32{{0.75, 0.6614378277661477}}},
	}, "画出近一月股价趋势图")
	if !confident || result.Primary != LabelLiveDataVisual || len(result.Secondary) != 0 {
		t.Fatalf("result=%+v confident=%v, want a plain live_data_visual grant", result, confident)
	}
}

func TestClassifyByEmbeddingVisualLeaderYieldsToNamedPDF(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	result, confident := classifyByEmbedding(emb, []intentAnchor{
		{Label: LabelLiveDataVisual, Vecs: [][]float32{{0.92, 0.39191835884530846}}},
		{Label: LabelLiveData, Vecs: [][]float32{{0.85, 0.5267826876426364}}},
		{Label: LabelSearch, Vecs: [][]float32{{0.90, 0.4358898943540673}}},
		{Label: LabelDocumentGenerate, Vecs: [][]float32{{0.75, 0.6614378277661477}}},
	}, "画出趋势并生成pdf")
	if !confident || result.Primary != LabelLiveData || len(result.Secondary) != 1 || result.Secondary[0] != LabelDocumentGenerate {
		t.Fatalf("result=%+v confident=%v, want live_data + PDF, not the higher search or chart score", result, confident)
	}
}

func TestClassifyByEmbeddingVisualLeaderNamedPDFFallsBackToSearchNotWebFetch(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	// live_data is below the hint floor. web_fetch scores higher than search,
	// but a tree timeout keeps search and collapses web_fetch to unknown.
	result, confident := classifyByEmbedding(emb, []intentAnchor{
		{Label: LabelLiveDataVisual, Vecs: [][]float32{{0.92, 0.39191835884530846}}},
		{Label: LabelLiveData, Vecs: [][]float32{{0.50, 0.8660254}}},
		{Label: LabelWebFetch, Vecs: [][]float32{{0.90, 0.4358898943540673}}},
		{Label: LabelSearch, Vecs: [][]float32{{0.75, 0.6614378277661477}}},
		{Label: LabelDocumentGenerate, Vecs: [][]float32{{0.50, 0.8660254}}},
	}, "画出趋势并生成pdf")
	if confident || result.Primary != LabelSearch || len(result.Secondary) != 0 {
		t.Fatalf("result=%+v confident=%v, want a search hint, not web_fetch", result, confident)
	}
}

func TestClassifyByEmbeddingVisualLeaderNamedPDFBelowLiveDataFloorEscalates(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	// live_data 0.80 sits under the 0.82 companion floor. A chart leader must
	// not mint the PDF pair from that score.
	result, confident := classifyByEmbedding(emb, []intentAnchor{
		{Label: LabelLiveDataVisual, Vecs: [][]float32{{0.92, 0.39191835884530846}}},
		{Label: LabelLiveData, Vecs: [][]float32{{0.80, 0.6}}},
		{Label: LabelDocumentGenerate, Vecs: [][]float32{{0.75, 0.6614378277661477}}},
	}, "画出趋势并生成pdf")
	if confident || result.Primary != LabelLiveData || len(result.Secondary) != 0 {
		t.Fatalf("result=%+v confident=%v, want a lookup escalation", result, confident)
	}
}

func TestClassifyByEmbeddingVisualLeaderNamedPDFWithoutSupportEscalates(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	result, confident := classifyByEmbedding(emb, []intentAnchor{
		{Label: LabelLiveDataVisual, Vecs: [][]float32{{0.92, 0.39191835884530846}}},
		{Label: LabelLiveData, Vecs: [][]float32{{0.80, 0.6}}},
		{Label: LabelDocumentGenerate, Vecs: [][]float32{{0.50, 0.8660254}}},
	}, "画出趋势并生成pdf")
	if confident || result.Primary != LabelLiveData || len(result.Secondary) != 0 {
		t.Fatalf("result=%+v confident=%v, want a lookup escalation, not a chart grant", result, confident)
	}
}

func TestUtteranceNamesPDFRequiresTokenBoundary(t *testing.T) {
	if !utteranceNamesPDF("崇州天气，生成格式化pdf") || !utteranceNamesPDF("生成PDF报告") || !utteranceNamesPDF("生成pdf，图在 a.pdf") || !utteranceNamesPDF("生成ＰＤＦ报告") || !utteranceNamesPDF("查完了。pdf") || !utteranceNamesPDF("不要pdf，还是生成pdf") {
		t.Fatal("pdf format token must match")
	}
	if utteranceNamesPDF("用 pdfium 渲染") || utteranceNamesPDF("画出近一月股价趋势图") || utteranceNamesPDF("画出趋势图，参见 a.pdf") || utteranceNamesPDF(`C:\data\report.PDF`) || utteranceNamesPDF("参见报告．pdf") || utteranceNamesPDF("不要pdf，画出股价趋势图") || utteranceNamesPDF("不要　pdf") || utteranceNamesPDF("不要生成pdf") || utteranceNamesPDF("不要  生成  pdf") || utteranceNamesPDF("不要ＰＤＦ") || utteranceNamesPDF("no pdf, draw a chart") {
		t.Fatal("pdf inside another word, a file extension, or a negation is not a generate request")
	}
}

func TestClassifyByEmbeddingNegatedPDFDoesNotOverrideVisualLeader(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	result, confident := classifyByEmbedding(emb, []intentAnchor{
		{Label: LabelLiveDataVisual, Vecs: [][]float32{{0.92, 0.39191835884530846}}},
		{Label: LabelLiveData, Vecs: [][]float32{{0.80, 0.6}}},
		{Label: LabelDocumentGenerate, Vecs: [][]float32{{0.75, 0.6614378277661477}}},
	}, "不要pdf，画出近一月股价趋势图")
	if !confident || result.Primary != LabelLiveDataVisual || result.HasLabel(LabelDocumentGenerate) {
		t.Fatalf("result=%+v confident=%v, want the chart leader; a negated pdf is not a generate request", result, confident)
	}
}

func TestClassifyByEmbeddingFileExtensionDoesNotOverrideVisualLeader(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	result, confident := classifyByEmbedding(emb, []intentAnchor{
		{Label: LabelLiveDataVisual, Vecs: [][]float32{{0.92, 0.39191835884530846}}},
		{Label: LabelLiveData, Vecs: [][]float32{{0.80, 0.6}}},
		{Label: LabelDocumentGenerate, Vecs: [][]float32{{0.75, 0.6614378277661477}}},
	}, "画出近一月股价趋势图，参见 a.pdf")
	if !confident || result.Primary != LabelLiveDataVisual || len(result.Secondary) != 0 {
		t.Fatalf("result=%+v confident=%v, want the chart leader; a .pdf path is not a generate request", result, confident)
	}
}

func TestClassifyByEmbeddingWeakerVisualDoesNotDisturbPDFComposite(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	result, confident := classifyByEmbedding(emb, []intentAnchor{
		{Label: LabelLiveData, Vecs: [][]float32{{0.90, 0.4358898943540673}}},
		{Label: LabelDocumentGenerate, Vecs: [][]float32{{0.85, 0.5267826876426364}}},
		{Label: LabelLiveDataVisual, Vecs: [][]float32{{0.60, 0.8}}},
	}, "北京天气，输出格式化pdf报告")
	if !confident || result.Primary != LabelLiveData || len(result.Secondary) != 1 || result.Secondary[0] != LabelDocumentGenerate {
		t.Fatalf("result=%+v confident=%v, want the verified PDF composite", result, confident)
	}
}

func competingVisualAnchors() []intentAnchor {
	return []intentAnchor{
		{Label: LabelLiveData, Vecs: [][]float32{{0.90, 0.4358898943540673}}},
		{Label: LabelDocumentGenerate, Vecs: [][]float32{{0.74, 0.6726065725724457}}},
		{Label: LabelLiveDataVisual, Vecs: [][]float32{{0.86, 0.5102938362773704}}},
	}
}

func TestClassifyTreeReadsCompetingVisualParaphrase(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	uic := New(Config{
		Embedder: emb,
		LLMFunc: func(_, _ string) (string, error) {
			return `{"top":[{"skill":"live_data_visual","score":0.91}]}`, nil
		},
	})
	uic.mu.Lock()
	uic.ready = true
	uic.anchors = competingVisualAnchors()
	uic.mu.Unlock()

	result := uic.Classify(MessageContext{Text: "画个天气图"})
	if result.Layer != 3 || result.Primary != LabelLiveDataVisual || len(result.Secondary) != 1 || result.Secondary[0] != LabelLiveData {
		t.Fatalf("result=%+v, want the tree's chart reading with the lookup half", result)
	}
}

func TestClassifyTreeReadsCompetingPDFParaphrase(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	uic := New(Config{
		Embedder: emb,
		LLMFunc: func(_, _ string) (string, error) {
			return `{"top":[{"skill":"document_generate","score":0.91}]}`, nil
		},
	})
	uic.mu.Lock()
	uic.ready = true
	uic.anchors = competingVisualAnchors()
	uic.mu.Unlock()

	result := uic.Classify(MessageContext{Text: "把天气整理成一份报告文件"})
	if result.Layer != 3 || result.Primary != LabelLiveData || len(result.Secondary) != 1 || result.Secondary[0] != LabelDocumentGenerate {
		t.Fatalf("result=%+v, want the tree's PDF reading", result)
	}
}

func anchorVec(cos float64) []float32 {
	return []float32{float32(cos), float32(math.Sqrt(1 - cos*cos))}
}

// 2026-09-28 WeChat 「北京天气」: live_data 0.73 beside live_data_visual 0.646
// left the lookup shortcut, the tree said live_data 0.95, and synthesis
// crowned the chart. The reply then attached a placeholder bar image.
func TestClassifyPlainWeatherDoesNotBecomeVisualCard(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	anchors := []intentAnchor{
		{Label: LabelLiveData, Vecs: [][]float32{anchorVec(0.73)}},
		{Label: LabelLiveDataVisual, Vecs: [][]float32{anchorVec(0.646)}},
	}
	l2, confident := classifyByEmbedding(emb, anchors, "北京天气")
	if !confident || l2.Primary != LabelLiveData || l2.HasLabel(LabelLiveDataVisual) || l2.RunnerUp == LabelLiveDataVisual {
		t.Fatalf("l2=%+v confident=%v, a sub-floor chart resemblance must stay a plain lookup", l2, confident)
	}

	llmCalls := 0
	uic := New(Config{
		Embedder: emb,
		LLMFunc: func(_, _ string) (string, error) {
			llmCalls++
			return `{"top":[{"skill":"live_data_visual","score":0.95}]}`, nil
		},
	})
	uic.mu.Lock()
	uic.ready = true
	uic.anchors = anchors
	uic.mu.Unlock()

	result := uic.Classify(MessageContext{Text: "北京天气"})
	if llmCalls != 0 {
		t.Fatalf("llmCalls=%d, plain weather must not escalate", llmCalls)
	}
	if result.Primary != LabelLiveData || result.HasLabel(LabelLiveDataVisual) {
		t.Fatalf("result=%+v, want live_data without a visual card", result)
	}
}

func TestClassifyTreeLookupDoesNotCrownStrongVisualResemblance(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	anchors := []intentAnchor{
		{Label: LabelLiveData, Vecs: [][]float32{anchorVec(0.92)}},
		{Label: LabelLiveDataVisual, Vecs: [][]float32{anchorVec(0.80)}},
	}
	l2, confident := classifyByEmbedding(emb, anchors, "北京天气")
	if confident || l2.Primary != LabelLiveData || len(l2.Secondary) != 1 || l2.Secondary[0] != LabelLiveDataVisual {
		t.Fatalf("l2=%+v confident=%v, a reviewed chart companion must escalate", l2, confident)
	}

	uic := New(Config{
		Embedder: emb,
		LLMFunc: func(_, _ string) (string, error) {
			return `{"top":[{"skill":"live_data","score":0.95}]}`, nil
		},
	})
	uic.mu.Lock()
	uic.ready = true
	uic.anchors = anchors
	uic.mu.Unlock()

	result := uic.Classify(MessageContext{Text: "北京天气"})
	if result.Primary != LabelLiveData || result.HasLabel(LabelLiveDataVisual) {
		t.Fatalf("result=%+v, tree lookup must not be rewritten into a visual card", result)
	}
}

func TestSecondaryTreeLabelsUsesChartFloor(t *testing.T) {
	weak := secondaryTreeLabels([]TreeCandidate{
		{Label: LabelLiveData, Score: 0.95},
		{Label: LabelLiveDataVisual, Score: 0.60},
	})
	if len(weak) != 0 {
		t.Fatalf("secondary=%v, a sub-floor chart guess must not ride on a lookup", weak)
	}
	kept := secondaryTreeLabels([]TreeCandidate{
		{Label: LabelLiveData, Score: 0.95},
		{Label: LabelLiveDataVisual, Score: 0.80},
	})
	if len(kept) != 1 || kept[0] != LabelLiveDataVisual {
		t.Fatalf("secondary=%v, a reviewed chart half must stay", kept)
	}
	lookup := secondaryTreeLabels([]TreeCandidate{
		{Label: LabelLiveDataVisual, Score: 0.91},
		{Label: LabelLiveData, Score: 0.60},
	})
	if len(lookup) != 1 || lookup[0] != LabelLiveData {
		t.Fatalf("secondary=%v, the lookup half of a chart verdict must stay", lookup)
	}
	pdf := secondaryTreeLabels([]TreeCandidate{
		{Label: LabelLiveData, Score: 0.95},
		{Label: LabelDocumentGenerate, Score: 0.55},
	})
	if len(pdf) != 1 || pdf[0] != LabelDocumentGenerate {
		t.Fatalf("secondary=%v, lookup+PDF must keep the 0.50 composite floor", pdf)
	}
}

func TestClassifyWeakTreeVisualDoesNotAttachWeatherCard(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	uic := New(Config{
		Embedder: emb,
		LLMFunc: func(_, _ string) (string, error) {
			return `{"top":[{"skill":"live_data","score":0.95},{"skill":"live_data_visual","score":0.60}]}`, nil
		},
	})
	uic.mu.Lock()
	uic.ready = true
	// Gap under the lookup shortcut, chart under the companion floor, so
	// this turn still asks the tree. The weak visual candidate must not
	// come back as a card.
	uic.anchors = []intentAnchor{
		{Label: LabelLiveData, Vecs: [][]float32{anchorVec(0.72)}},
		{Label: LabelLiveDataVisual, Vecs: [][]float32{anchorVec(0.70)}},
	}
	uic.mu.Unlock()

	result := uic.Classify(MessageContext{Text: "北京天气"})
	if result.Primary != LabelLiveData || result.HasLabel(LabelLiveDataVisual) {
		t.Fatalf("result=%+v, weak tree visual must not attach a card", result)
	}
}

func TestClassifyWeakTreeRetainsLookupWithoutChart(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	uic := New(Config{
		Embedder: emb,
		LLMFunc: func(_, _ string) (string, error) {
			return `{"top":[{"skill":"live_data_visual","score":0.40}]}`, nil
		},
	})
	uic.mu.Lock()
	uic.ready = true
	uic.anchors = []intentAnchor{
		{Label: LabelLiveData, Vecs: [][]float32{anchorVec(0.92)}},
		{Label: LabelLiveDataVisual, Vecs: [][]float32{anchorVec(0.80)}},
	}
	uic.mu.Unlock()

	result := uic.Classify(MessageContext{Text: "北京天气"})
	if result.Primary != LabelLiveData || result.HasLabel(LabelLiveDataVisual) {
		t.Fatalf("result=%+v, a weak tree must not keep the chart half that only escalated the lookup", result)
	}
}

func TestClassifyChartCompanionTimeoutStaysWeatherLookup(t *testing.T) {
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
		{Label: LabelLiveData, Vecs: [][]float32{anchorVec(0.92)}},
		{Label: LabelLiveDataVisual, Vecs: [][]float32{anchorVec(0.80)}},
	}
	uic.mu.Unlock()

	result := uic.Classify(MessageContext{Text: "北京天气"})
	if !result.Degraded || result.Primary != LabelLiveData || result.HasLabel(LabelLiveDataVisual) {
		t.Fatalf("result=%+v, a chart companion must not fail the lookup or grant a card when the tree times out", result)
	}
}

func TestClassifySubFloorVisualDoesNotReplaceWeakerLookup(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	uic := New(Config{
		Embedder: emb,
		LLMFunc: func(_, _ string) (string, error) {
			return `{"top":[{"skill":"live_data_visual","score":0.60}]}`, nil
		},
	})
	uic.mu.Lock()
	uic.ready = true
	uic.anchors = []intentAnchor{
		{Label: LabelLiveData, Vecs: [][]float32{anchorVec(0.66)}},
		{Label: LabelLiveDataVisual, Vecs: [][]float32{anchorVec(0.64)}},
	}
	uic.mu.Unlock()

	result := uic.Classify(MessageContext{Text: "今天北京天气怎么样"})
	if result.Primary != LabelLiveData || result.HasLabel(LabelLiveDataVisual) {
		t.Fatalf("result=%+v, a sub-floor chart verdict must not replace a weaker lookup", result)
	}
}

func TestClassifyWeakTreeVisualPrimaryStaysLookup(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	uic := New(Config{
		Embedder: emb,
		LLMFunc: func(_, _ string) (string, error) {
			return `{"top":[{"skill":"live_data_visual","score":0.60}]}`, nil
		},
	})
	uic.mu.Lock()
	uic.ready = true
	uic.anchors = []intentAnchor{
		{Label: LabelLiveData, Vecs: [][]float32{anchorVec(0.72)}},
		{Label: LabelLiveDataVisual, Vecs: [][]float32{anchorVec(0.70)}},
	}
	uic.mu.Unlock()

	result := uic.Classify(MessageContext{Text: "北京天气"})
	if result.Primary != LabelLiveData || result.HasLabel(LabelLiveDataVisual) {
		t.Fatalf("result=%+v, an uncertain chart verdict must not replace a lookup", result)
	}
}

func TestClassifyTreeConfirmedVisualKeepsTheChart(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	anchors := []intentAnchor{
		{Label: LabelLiveData, Vecs: [][]float32{anchorVec(0.92)}},
		{Label: LabelLiveDataVisual, Vecs: [][]float32{anchorVec(0.80)}},
	}
	uic := New(Config{
		Embedder: emb,
		LLMFunc: func(_, _ string) (string, error) {
			return `{"top":[{"skill":"live_data_visual","score":0.91}]}`, nil
		},
	})
	uic.mu.Lock()
	uic.ready = true
	uic.anchors = anchors
	uic.mu.Unlock()

	result := uic.Classify(MessageContext{Text: "画一张北京天气实况图"})
	if result.Primary != LabelLiveDataVisual || len(result.Secondary) != 1 || result.Secondary[0] != LabelLiveData {
		t.Fatalf("result=%+v, a tree-confirmed chart must keep the lookup half", result)
	}
}

func TestClassifyTreePlainLookupDoesNotReattachCompetingArtifact(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	uic := New(Config{
		Embedder: emb,
		LLMFunc: func(_, _ string) (string, error) {
			return `{"top":[{"skill":"live_data","score":0.91}]}`, nil
		},
	})
	uic.mu.Lock()
	uic.ready = true
	uic.anchors = competingVisualAnchors()
	uic.mu.Unlock()

	result := uic.Classify(MessageContext{Text: "画个天气图"})
	if result.Primary != LabelLiveData || len(result.Secondary) != 0 || result.HasLabel(LabelLiveDataVisual) || result.HasLabel(LabelDocumentGenerate) {
		t.Fatalf("result=%+v, cosine winner must not survive a plain lookup verdict", result)
	}
}

func TestClassifyCompetingVisualTimeoutStaysLookup(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	uic := New(Config{
		Embedder:           emb,
		LLMFunc:            hangLLM,
		LLMTimeout:         30 * time.Second,
		FusionTreeDeadline: 30 * time.Millisecond,
	})
	uic.mu.Lock()
	uic.ready = true
	uic.anchors = competingVisualAnchors()
	uic.mu.Unlock()

	result := uic.Classify(MessageContext{Text: "画个天气图"})
	if !result.Degraded || result.Primary != LabelLiveData || len(result.Secondary) != 0 {
		t.Fatalf("result=%+v, timeout must stay a lookup hint and must not pick an artifact", result)
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

	// 2026-09-18: the collapsed result must preserve the L2 primary as
	// RunnerUp escalation evidence. A tree-timeout collapse used to destroy
	// non-lookup signals entirely (production: ssh at 0.83 collapsed to bare
	// unknown and every ssh availability layer stayed dark). RunnerUp is not
	// an authorized intent — surfaces that honor it apply their own policy.
	ssh := lookupHintOrUnknownFromL2(ClassificationResult{Primary: LabelSSH, Confidence: 0.83}, false)
	if ssh.RunnerUp != LabelSSH || ssh.RunnerUpScore != 0.83 {
		t.Fatalf("collapsed ssh must survive as RunnerUp evidence: %+v", ssh)
	}
	if ssh.HasLabel(LabelSSH) {
		t.Fatalf("RunnerUp must not leak into declared labels: %+v", ssh.Labels())
	}
	if other.RunnerUp != LabelFileRead || other.RunnerUpScore != 0.66 {
		t.Fatalf("file_read collapse must also preserve evidence: %+v", other)
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
		LLMContextFunc: func(ctx context.Context, _ context.Context, _, _ string) (string, error) {
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
		LLMContextFunc: func(ctx context.Context, _ context.Context, _, _ string) (string, error) {
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
		LLMContextFunc: func(ctx context.Context, _ context.Context, _, _ string) (string, error) {
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

// A budget-fired tree timeout must not make every other ambiguous turn in the
// burst pay the full fusion deadline again: escalations for a DIFFERENT scope
// fail fast into the L2 fallback while the failure is still fresh, a repeat of
// the SAME scope still escalates (its detached read may be adoptable — the
// resend-recovery path), and a successful late verdict lifts the suppression
// for everything that follows.
func TestBudgetFiredTreeTimeoutSkipsOtherScopesOnly(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0, 0}}
	var calls atomic.Int32
	releaseLate := make(chan struct{})
	uic := New(Config{
		Embedder: emb,
		LLMContextFunc: func(ctx context.Context, _ context.Context, _, _ string) (string, error) {
			n := calls.Add(1)
			if n == 1 {
				// The first turn's tree call loses to the fusion deadline.
				<-ctx.Done()
				return "", ctx.Err()
			}
			if n == 2 {
				// The scheduled late verdict: gate it so the suppression
				// window is observed while the failure is still unresolved.
				<-releaseLate
			}
			return `{"top":[{"skill":"office","score":0.92}]}`, nil
		},
		FusionTreeDeadline: 30 * time.Millisecond,
		LLMTimeout:         5 * time.Second,
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

	first := uic.ClassifyContext(context.Background(), MessageContext{Text: "生成庆祝生日会的PPT"})
	if !first.Degraded {
		t.Fatalf("first = %+v, want degraded hint after fusion timeout", first)
	}
	// The timeout schedules a background late verdict; wait until it has
	// claimed the scope and is blocked on the gate, so its pending success
	// cannot leak into the suppression assertions below.
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if calls.Load() < 2 {
		t.Fatalf("background late verdict never started; calls=%d", calls.Load())
	}
	before := calls.Load()

	// A different ambiguous scope while the failure is still fresh must not
	// pay another tree call: it lands straight in the L2 fallback.
	other := uic.ClassifyContext(context.Background(), MessageContext{Text: "发邮件给团队更新项目进度"})
	if !other.Degraded {
		t.Fatalf("other = %+v, want degraded L2 fallback without a tree call", other)
	}
	if calls.Load() != before {
		t.Fatalf("other scope paid a tree call despite the fresh endpoint failure: calls=%d", calls.Load())
	}

	// Let the late verdict land: it recovers the failed scope (cache-warmed)
	// and lifts the suppression for everything after.
	close(releaseLate)
	deadline = time.Now().Add(3 * time.Second)
	for calls.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	again := uic.ClassifyContext(context.Background(), MessageContext{Text: "生成庆祝生日会的PPT"})
	if again.Primary != LabelOffice || again.Degraded {
		t.Fatalf("repeat = %+v, want office route for the failed scope", again)
	}
	recovered := uic.ClassifyContext(context.Background(), MessageContext{Text: "整理会议纪要并群发"})
	if recovered.Primary != LabelOffice || recovered.Degraded {
		t.Fatalf("post-recovery scope = %+v, want probing to resume after the late verdict lifted suppression", recovered)
	}
}

// A 5xx from the tree endpoint (e.g. an HTTP 502 upstream failure) is the same
// health evidence as a budget-fired timeout: escalations for OTHER scopes fail
// fast into the L2 fallback while the failure is still fresh, and the failed
// scope itself still escalates on repeat.
func TestTree5xxFailureSkipsOtherScopesOnly(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0, 0}}
	var calls atomic.Int32
	releaseLate := make(chan struct{})
	uic := New(Config{
		Embedder: emb,
		LLMContextFunc: func(ctx context.Context, _ context.Context, _, _ string) (string, error) {
			n := calls.Add(1)
			if n == 1 {
				return "", &httpStatusError{status: 502}
			}
			if n == 2 {
				<-releaseLate
			}
			return `{"top":[{"skill":"office","score":0.92}]}`, nil
		},
		FusionTreeDeadline: 30 * time.Millisecond,
		LLMTimeout:         5 * time.Second,
	})
	uic.mu.Lock()
	uic.ready = true
	uic.anchors = []intentAnchor{
		{Label: LabelOffice, Vecs: [][]float32{{1, 0, 0}}},
		{Label: LabelDocumentRead, Vecs: [][]float32{{1, 0, 0}}},
		{Label: LabelCoding, Vecs: [][]float32{{0, 1, 0}}},
	}
	uic.mu.Unlock()

	first := uic.ClassifyContext(context.Background(), MessageContext{Text: "生成庆祝生日会的PPT"})
	if !first.Degraded {
		t.Fatalf("first = %+v, want degraded hint after 5xx", first)
	}
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if calls.Load() < 2 {
		t.Fatalf("background late verdict never started; calls=%d", calls.Load())
	}
	before := calls.Load()

	other := uic.ClassifyContext(context.Background(), MessageContext{Text: "发邮件给团队更新项目进度"})
	if !other.Degraded {
		t.Fatalf("other = %+v, want degraded L2 fallback without a tree call", other)
	}
	if calls.Load() != before {
		t.Fatalf("other scope paid a tree call despite the fresh 5xx: calls=%d", calls.Load())
	}

	close(releaseLate)
	deadline = time.Now().Add(3 * time.Second)
	for calls.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	again := uic.ClassifyContext(context.Background(), MessageContext{Text: "生成庆祝生日会的PPT"})
	if again.Primary != LabelOffice || again.Degraded {
		t.Fatalf("repeat = %+v, want office route for the failed scope", again)
	}
}

// httpStatusError mimics corelib/llm.HTTPStatusError for tests in this
// package (which cannot import llm without an import cycle).
type httpStatusError struct{ status int }

func (e *httpStatusError) Error() string       { return "HTTP 502: body_len=434" }
func (e *httpStatusError) HTTPStatusCode() int { return e.status }
