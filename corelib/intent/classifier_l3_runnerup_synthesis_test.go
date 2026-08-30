package intent

import (
	"strings"
	"testing"
)

// TestClassifyTreeSynthesizesCompositeFromEmbeddingRunnerUp reproduces the
// 2026-08-24 production miss for "全网搜索张惠妹歌曲列表，生成详细pdf版本清单":
// L2 scored search 0.690 / document_generate 0.683 (ambiguous, escalated),
// the tree answered a bare lookup (web_fetch 0.95), and the turn shipped
// without the document-generation capability ("PDF 生成工具不可用"). The
// runner-up half must survive escalation as evidence so the synthesis can
// still recover the declared lookup+generate composite.
func TestClassifyTreeSynthesizesCompositeFromEmbeddingRunnerUp(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	llmCalls := 0
	uic := New(Config{
		Embedder: emb,
		LLMFunc: func(_, _ string) (string, error) {
			llmCalls++
			return `{"top":[{"skill":"web_fetch","score":0.95}]}`, nil
		},
	})
	uic.mu.Lock()
	uic.ready = true
	uic.anchors = []intentAnchor{
		// Production score shape: the lookup half leads narrowly, the document
		// half is a close runner-up; neither clears the confident threshold.
		{Label: LabelSearch, Vecs: [][]float32{{0.69, 0.723809}}},
		{Label: LabelDocumentGenerate, Vecs: [][]float32{{0.6825, 0.730891}}},
	}
	uic.mu.Unlock()

	result := uic.Classify(MessageContext{Text: "search the web for the full song list and render a detailed pdf checklist"})
	if llmCalls != 1 {
		t.Fatalf("llmCalls=%d, want exactly one tree escalation", llmCalls)
	}
	if result.Primary != LabelWebFetch || len(result.Secondary) != 1 || result.Secondary[0] != LabelDocumentGenerate {
		t.Fatalf("result=%+v, want synthesized web_fetch -> document_generate composite", result)
	}
	if !strings.Contains(result.Reason, "synthesized composite") {
		t.Fatalf("reason=%q, want synthesized composite evidence", result.Reason)
	}
	// Composite confidence keeps the tree's verdict score: the tree is the
	// route authority at L3, and a min() with the weaker half only dragged the
	// result under the downstream floors, stripping capability management.
	if d := result.Confidence - 0.95; d < -1e-3 || d > 1e-3 {
		t.Fatalf("confidence=%.4f, want the tree verdict score ~0.95", result.Confidence)
	}
	if result.Degraded || result.ControlPlaneFailure {
		t.Fatalf("synthesized composite must stay executable: %+v", result)
	}
}

// TestClassifyTreeDoesNotInventCompositeFromUnrelatedRunnerUp guards the
// other direction: a runner-up that forms no declared pair with the tree's
// verdict must remain evidence, never a synthesized capability.
func TestClassifyTreeDoesNotInventCompositeFromUnrelatedRunnerUp(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	uic := New(Config{
		Embedder: emb,
		LLMFunc: func(_, _ string) (string, error) {
			return `{"top":[{"skill":"ssh","score":0.95}]}`, nil
		},
	})
	uic.mu.Lock()
	uic.ready = true
	uic.anchors = []intentAnchor{
		{Label: LabelSearch, Vecs: [][]float32{{0.69, 0.723809}}},
		{Label: LabelDocumentGenerate, Vecs: [][]float32{{0.6825, 0.730891}}},
	}
	uic.mu.Unlock()

	result := uic.Classify(MessageContext{Text: "check the remote server load"})
	if result.Primary != LabelSSH || len(result.Secondary) != 0 {
		t.Fatalf("result=%+v, want the bare tree verdict without an invented document half", result)
	}
}

// TestClassifyTreeSynthesizesCompositeDespiteExtraTreeCandidates reproduces
// the 2026-08-24 production rerun of the 张惠妹 turn: the tree answered with
// multiple candidates (web_fetch 0.95 + search 0.80).  The extra candidate
// survives the secondaryTreeLabels filter (≥0.70, within 0.20 of the top),
// populated Secondary, and used to suppress the runner-up synthesis
// entirely, so the turn shipped without document_generate again and the
// loop hallucinated "PDF报告已发布".
func TestClassifyTreeSynthesizesCompositeDespiteExtraTreeCandidates(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	uic := New(Config{
		Embedder: emb,
		LLMFunc: func(_, _ string) (string, error) {
			return `{"top":[{"skill":"web_fetch","score":0.95,"workflow_type":""},{"skill":"search","score":0.80,"workflow_type":""}]}`, nil
		},
	})
	uic.mu.Lock()
	uic.ready = true
	uic.anchors = []intentAnchor{
		{Label: LabelSearch, Vecs: [][]float32{{0.69, 0.723809}}},
		{Label: LabelDocumentGenerate, Vecs: [][]float32{{0.6825, 0.730891}}},
	}
	uic.mu.Unlock()

	result := uic.Classify(MessageContext{Text: "search the web for the full song list and render a detailed pdf checklist"})
	if result.Primary != LabelWebFetch || len(result.Secondary) != 1 || result.Secondary[0] != LabelDocumentGenerate {
		t.Fatalf("result=%+v, want synthesized web_fetch -> document_generate composite despite extra tree candidates", result)
	}
	if !strings.Contains(result.Reason, "synthesized composite") {
		t.Fatalf("reason=%q, want synthesized composite evidence", result.Reason)
	}
}

// TestClassifyTreeSynthesizesCompositeFromSecondaryEvidence covers the shape
// where the L2 generic-confident guard escalated a strong lookup with
// document_generate attached as Secondary evidence, but an unrelated label
// (non_coding) holds the runner-up slot.  The tree answering a bare lookup
// must still synthesize the composite from the Secondary evidence; without
// it the turn ships lookup-only and the artifact capability is lost.
func TestClassifyTreeSynthesizesCompositeFromSecondaryEvidence(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	uic := New(Config{
		Embedder: emb,
		LLMFunc: func(_, _ string) (string, error) {
			return `{"top":[{"skill":"live_data","score":0.90}]}`, nil
		},
	})
	uic.mu.Lock()
	uic.ready = true
	uic.anchors = []intentAnchor{
		{Label: LabelLiveData, Vecs: [][]float32{{0.85, 0.526783}}},
		{Label: LabelNonCoding, Vecs: [][]float32{{0.74, 0.672607}}},
		{Label: LabelDocumentGenerate, Vecs: [][]float32{{0.72, 0.693974}}},
	}
	uic.mu.Unlock()

	result := uic.Classify(MessageContext{Text: "杭州天气，生成pdf报告"})
	if result.Primary != LabelLiveData || len(result.Secondary) != 1 || result.Secondary[0] != LabelDocumentGenerate {
		t.Fatalf("result=%+v, want synthesized live_data -> document_generate composite from secondary evidence", result)
	}
	if !strings.Contains(result.Reason, "synthesized composite") {
		t.Fatalf("reason=%q, want synthesized composite evidence", result.Reason)
	}
}

// TestClassifyWeakTreeVerdictDoesNotOverrideStrongEmbeddingLeader reproduces
// the 2026-08-24 "pdf在哪？" turn: L2 held document_generate at 0.87, the
// tree guessed session_manage at 0.41 (its own "uncertain" band), and the
// weak verdict pushed the turn into the light tool profile, stripping the
// PDF/delivery capabilities the user was asking about.  A sub-0.50 tree
// verdict must not override a locally confident leader.
func TestClassifyWeakTreeVerdictDoesNotOverrideStrongEmbeddingLeader(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	llmCalls := 0
	uic := New(Config{
		Embedder: emb,
		LLMFunc: func(_, _ string) (string, error) {
			llmCalls++
			return `{"top":[{"skill":"session_manage","score":0.41}]}`, nil
		},
	})
	uic.mu.Lock()
	uic.ready = true
	uic.anchors = []intentAnchor{
		{Label: LabelDocumentGenerate, Vecs: [][]float32{{0.87, 0.493052}}},
		{Label: LabelNonCoding, Vecs: [][]float32{{0.60, 0.80}}},
	}
	uic.mu.Unlock()

	result := uic.Classify(MessageContext{Text: "pdf在哪？"})
	if llmCalls != 1 {
		t.Fatalf("llmCalls=%d, want exactly one tree escalation", llmCalls)
	}
	if result.Primary != LabelDocumentGenerate || result.Layer != 2 {
		t.Fatalf("result=%+v, want the strong L2 leader retained over the weak tree verdict", result)
	}
	if !strings.Contains(result.Reason, "weak tree verdict") {
		t.Fatalf("reason=%q, want the distrust rationale recorded", result.Reason)
	}
	if result.Degraded || result.ControlPlaneFailure {
		t.Fatalf("retained L2 leader must stay executable: %+v", result)
	}
}

// TestClassifyWeakTreeVerdictDoesNotOverrideSubFloorEmbeddingLeader is the
// 2026-08-29 fusion incident: L2 held workflow_task at 0.74 (below the 0.78
// confident bar, so it correctly escalated) and the tree guessed task_track
// at 0.38. Requiring EmbeddingConfidentMinScore on the distrust guard let
// that guess win, and because task_track is a managed family the turn was
// locked onto a task-tracking surface.
func TestClassifyWeakTreeVerdictDoesNotOverrideSubFloorEmbeddingLeader(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	llmCalls := 0
	uic := New(Config{
		Embedder: emb,
		LLMFunc: func(_, _ string) (string, error) {
			llmCalls++
			return `{"top":[{"skill":"task_track","score":0.38}]}`, nil
		},
	})
	uic.mu.Lock()
	uic.ready = true
	uic.anchors = []intentAnchor{
		{Label: LabelWorkflowTask, Vecs: [][]float32{{0.74, 0.672607}}},
		{Label: LabelTaskTrack, Vecs: [][]float32{{0.40, 0.916515}}},
	}
	uic.mu.Unlock()

	result := uic.Classify(MessageContext{Text: "帮我写一份商业计划书"})
	if llmCalls != 1 {
		t.Fatalf("llmCalls=%d, want exactly one tree escalation", llmCalls)
	}
	if result.Primary != LabelWorkflowTask || result.Layer != 2 {
		t.Fatalf("result=%+v, want L2 workflow_task retained over weak tree task_track", result)
	}
	if !strings.Contains(result.Reason, "weak tree verdict") {
		t.Fatalf("reason=%q, want the distrust rationale recorded", result.Reason)
	}
	if result.Degraded || result.ControlPlaneFailure {
		t.Fatalf("retained L2 leader must stay executable: %+v", result)
	}
}

func TestRetainEmbeddingOverWeakTree(t *testing.T) {
	if !retainEmbeddingOverWeakTree(ClassificationResult{Primary: LabelWorkflowTask, Confidence: 0.74}, 0.38) {
		t.Fatal("usable sub-0.78 L2 must beat a guessing tree")
	}
	if !retainEmbeddingOverWeakTree(ClassificationResult{Primary: LabelDocumentGenerate, Confidence: 0.87}, 0.41) {
		t.Fatal("confident L2 must still beat a guessing tree")
	}
	if retainEmbeddingOverWeakTree(ClassificationResult{Primary: LabelWorkflowTask, Confidence: 0.74}, 0.59) {
		t.Fatal("mid-band tree (0.59) must still be allowed to synthesize")
	}
	if retainEmbeddingOverWeakTree(ClassificationResult{Primary: LabelUnknown, Confidence: 0.80}, 0.30) {
		t.Fatal("unknown L2 must not be retained")
	}
	if retainEmbeddingOverWeakTree(ClassificationResult{Primary: LabelTaskTrack, Confidence: 0.30}, 0.38) {
		t.Fatal("a weaker L2 must not beat the tree")
	}
	if retainEmbeddingOverWeakTree(ClassificationResult{Primary: LabelWorkflowTask, Confidence: 0.51}, 0.38) {
		t.Fatal("L2 below the usable-hint floor must not be retained")
	}
	if retainEmbeddingOverWeakTree(ClassificationResult{Primary: LabelWorkflowTask, Confidence: 0.74}, TreeVerdictDistrustMaxScore) {
		t.Fatal("tree at the 0.50 distrust bound must still be allowed to stand")
	}
	if !retainEmbeddingOverWeakTree(ClassificationResult{Primary: LabelWorkflowTask, Confidence: EmbeddingLookupMinScore}, 0.38) {
		t.Fatal("L2 at the usable-hint floor must beat a guessing tree")
	}
}

// TestClassifyTreeSynthesisKeepsStrongerLocalHalfEvidence is the mirror of
// the runner-up incident: the tree returned a weak lookup verdict (web_fetch
// 0.599) while the local office half was strong (0.855). Keeping only the
// tree's score dragged the synthesized office composite to 0.60 — under the
// 0.70 tree floor — so the identical PPT request fell out of capability
// management into the 24-tool legacy surface (2026-08-26 production). Either
// authority's strong evidence must keep the turn managed.
func TestClassifyTreeSynthesisKeepsStrongerLocalHalfEvidence(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	uic := New(Config{
		Embedder: emb,
		LLMFunc: func(_, _ string) (string, error) {
			return `{"top":[{"skill":"web_fetch","score":0.599}]}`, nil
		},
	})
	uic.mu.Lock()
	uic.ready = true
	uic.anchors = []intentAnchor{
		// The local office half is strong but ambiguous (web_fetch rides as the
		// declared lookup companion), so L2 escalates to the tree.
		{Label: LabelOffice, Vecs: [][]float32{{0.855, 0.723809}}},
		{Label: LabelWebFetch, Vecs: [][]float32{{0.71, 0.730891}}},
	}
	uic.mu.Unlock()

	result := uic.Classify(MessageContext{Text: "生成庆祝我家布偶宝宝5岁生日的ppt，没有照片，网上随便找一下布偶照片。"})
	if result.Primary != LabelOffice || len(result.Secondary) != 1 || result.Secondary[0] != LabelWebFetch {
		t.Fatalf("result=%+v, want synthesized office -> web_fetch composite", result)
	}
	if !strings.Contains(result.Reason, "synthesized composite") {
		t.Fatalf("reason=%q, want synthesized composite evidence", result.Reason)
	}
	// The stronger local half's evidence lifts the composite above the tree
	// floor (0.70); the weak tree verdict (0.599) must not drag it out of
	// capability management.
	if result.Confidence <= 0.70 {
		t.Fatalf("confidence=%.4f, want the stronger half's evidence to lift it above the 0.70 tree floor", result.Confidence)
	}
	if result.Degraded || result.ControlPlaneFailure {
		t.Fatalf("synthesized composite must stay executable: %+v", result)
	}
}
