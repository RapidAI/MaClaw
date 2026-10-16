package intent

import (
	"strings"
	"testing"
)

// Regression for the 2026-08-25 production miss on
// "生成庆祝我家布偶宝宝5岁生日的ppt，没有照片，网上随便找一下布偶照片。":
// L2 shipped a confident plain-office verdict (0.857), the semantic plan
// carried only document.write.office, and the model's image-search calls
// failed with "not available in this request's rendered tool surface".
//
// Measured on the installed 768-dim model the same day, office-only negatives
// ("把数据整理成Excel表格" search 0.737) outscore the genuine find-images
// phrasings (search 0.652), so no local floor may settle the pair: a confident
// office leader with a material lookup companion must escalate with the half
// attached as evidence, and the tree merge must keep office as primary.
func TestClassifyEscalatesConfidentOfficeWithLookupCompanion(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	llmCalls := 0
	uic := New(Config{
		Embedder: emb,
		LLMFunc: func(_, _ string) (string, error) {
			llmCalls++
			// The tree confirms the deck only; the lookup half arrives as L2
			// escalation evidence and must be synthesized, not dropped.
			return `{"top":[{"skill":"office","score":0.92}]}`, nil
		},
	})
	uic.mu.Lock()
	uic.ready = true
	uic.anchors = []intentAnchor{
		// Confident office leader (cos 1.0) with a material search companion
		// (cos 0.65 >= EmbeddingLookupCompositeFloor).
		{Label: LabelOffice, Vecs: [][]float32{{1, 0}}},
		{Label: LabelSearch, Vecs: [][]float32{{0.65, 0.7599342086785331}}},
	}
	uic.mu.Unlock()

	l2, confident := classifyByEmbedding(emb, uic.anchors, "find ragdoll photos online and make a birthday ppt")
	if confident {
		t.Fatalf("confident office + lookup companion must not ship locally: %+v", l2)
	}
	if l2.Primary != LabelOffice || len(l2.Secondary) != 1 || l2.Secondary[0] != LabelSearch {
		t.Fatalf("L2 escalation must carry the lookup half as evidence: %+v", l2)
	}

	result := uic.Classify(MessageContext{Text: "find ragdoll photos online and make a birthday ppt"})
	if llmCalls != 1 {
		t.Fatalf("llmCalls=%d, want exactly one tree escalation", llmCalls)
	}
	if result.Primary != LabelOffice || len(result.Secondary) != 1 || result.Secondary[0] != LabelSearch {
		t.Fatalf("result=%+v, want office primary with the search half preserved", result)
	}
	if !strings.Contains(result.Reason, "synthesized composite") {
		t.Fatalf("reason=%q, want synthesized composite evidence", result.Reason)
	}
	if result.Degraded || result.ControlPlaneFailure {
		t.Fatalf("synthesized office composite must stay executable: %+v", result)
	}
}

// The synthesis used to hardcode document_generate as the artifact half: a
// live_data_visual (or office) half paired with a lookup was mislabeled into
// the PDF chain, dropping the visual capability. The artifact half must keep
// its own label.
func TestClassifyTreeSynthesisKeepsNonDocumentArtifactHalf(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	uic := New(Config{
		Embedder: emb,
		LLMFunc: func(_, _ string) (string, error) {
			return `{"top":[{"skill":"live_data_visual","score":0.9}]}`, nil
		},
	})
	uic.mu.Lock()
	uic.ready = true
	uic.anchors = []intentAnchor{
		// Ambiguous visual leader (0.72 < confident floor) with a search
		// runner-up (0.66) that forms a declared pair.
		{Label: LabelLiveDataVisual, Vecs: [][]float32{{0.72, 0.6939698487904158}}},
		{Label: LabelSearch, Vecs: [][]float32{{0.66, 0.751331649344144}}},
	}
	uic.mu.Unlock()

	result := uic.Classify(MessageContext{Text: "search current numbers and render the live chart"})
	if result.Primary != LabelLiveDataVisual || len(result.Secondary) != 1 || result.Secondary[0] != LabelSearch {
		t.Fatalf("result=%+v, want live_data_visual primary with search half, never a document_generate mislabel", result)
	}
	for _, label := range result.Labels() {
		if label == LabelDocumentGenerate {
			t.Fatalf("result=%+v, document_generate must not appear for a visual composite", result)
		}
	}
}

// The reverse direction keeps its canonical form: a lookup leader with an
// office half still synthesizes, and office stays out of the lookup-primary
// normalization that only governs the document chain.
func TestClassifyTreeSynthesisKeepsOfficeArtifactForLookupLeader(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	uic := New(Config{
		Embedder: emb,
		LLMFunc: func(_, _ string) (string, error) {
			return `{"top":[{"skill":"search","score":0.9}]}`, nil
		},
	})
	uic.mu.Lock()
	uic.ready = true
	uic.anchors = []intentAnchor{
		{Label: LabelSearch, Vecs: [][]float32{{0.72, 0.6939698487904158}}},
		{Label: LabelOffice, Vecs: [][]float32{{0.66, 0.751331649344144}}},
	}
	uic.mu.Unlock()

	result := uic.Classify(MessageContext{Text: "search photos online and build the celebration deck"})
	if result.Primary != LabelOffice || len(result.Secondary) != 1 || result.Secondary[0] != LabelSearch {
		t.Fatalf("result=%+v, want office artifact primary with the search prerequisite preserved", result)
	}
}

// A staged chart turned into a fillable sheet embeds as office above the
// confident floor, with document_read a few hundredths behind and a lookup
// cosine high enough to escalate. The tree then answering "read the picture"
// used to replace the office leader. Image demotion treats that read as
// needing no tool, so the model is called with an empty tool list, narrates
// the spreadsheet, and the turn stops as success.
func TestClassifyRetainsSettledOfficeLeaderOverReadVerdict(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	uic := New(Config{
		Embedder: emb,
		LLMFunc: func(_, _ string) (string, error) {
			return `{"top":[{"skill":"document_read","score":0.80}]}`, nil
		},
	})
	uic.mu.Lock()
	uic.ready = true
	uic.anchors = []intentAnchor{
		{Label: LabelOffice, Vecs: [][]float32{{0.849, 0.5283937921666851}}},
		{Label: LabelDocumentRead, Vecs: [][]float32{{0.810, 0.5864290226784044}}},
		{Label: LabelLiveData, Vecs: [][]float32{{0.738, 0.6747992294005091}}},
	}
	uic.mu.Unlock()

	l2, confident := classifyByEmbedding(emb, uic.anchors, "将图表内容总结成可填写的表格，包括评分及总评")
	if confident {
		t.Fatalf("narrow office/read gap must escalate: %+v", l2)
	}
	if l2.Primary != LabelOffice || l2.Confidence < EmbeddingConfidentMinScore {
		t.Fatalf("L2 leader = %+v, want office at or above the confident floor", l2)
	}

	result := uic.Classify(MessageContext{Text: "将图表内容总结成可填写的表格，包括评分及总评"})
	if result.Primary != LabelOffice || len(result.Secondary) != 0 || !result.Degraded || result.Layer != 2 {
		t.Fatalf("result=%+v, want a degraded office hint with the unconfirmed lookup half dropped", result)
	}
	if result.RunnerUp != LabelDocumentRead {
		t.Fatalf("runner-up=%s, want the read verdict kept as evidence", result.RunnerUp)
	}
	if !strings.Contains(result.Reason, "office leader") {
		t.Fatalf("reason=%q, want the retention reason", result.Reason)
	}
}

func TestRetainSettledOfficeLeaderLabelMatrix(t *testing.T) {
	l2 := ClassificationResult{Primary: LabelOffice, Confidence: 0.849, Secondary: []IntentLabel{LabelLiveData}}
	read, ok := retainSettledOfficeLeader(l2, ClassificationResult{Primary: LabelDocumentRead, Confidence: 0.80})
	if !ok || read.Primary != LabelOffice || !read.Degraded || read.Layer != 2 || len(read.Secondary) != 0 || read.RunnerUp != LabelDocumentRead {
		t.Fatalf("document_read = %+v ok=%v", read, ok)
	}
	below := ClassificationResult{Primary: LabelOffice, Confidence: EmbeddingConfidentMinScore - 0.01}
	if _, ok := retainSettledOfficeLeader(below, ClassificationResult{Primary: LabelDocumentRead, Confidence: 0.8}); ok {
		t.Fatal("office below the confident floor must stay with the tree")
	}
	for _, tree := range []ClassificationResult{
		{Primary: LabelCoding, Confidence: 0.90},
		{Primary: LabelDocumentGenerate, Confidence: 0.88},
		{Primary: LabelFileWrite, Confidence: 0.90},
		{Primary: LabelWorkflowTask, Confidence: 0.80},
		{Primary: LabelScreenshot, Confidence: 0.90},
		{Primary: LabelScreenshot, Confidence: 0.75, Layer: 3},
		{Primary: LabelScreenshot, Secondary: []IntentLabel{LabelDocumentRead}, Confidence: 0.90, Layer: 3},
		{Primary: LabelScreenshot, Confidence: 0.60, Layer: 3, WorkflowType: "capture"},
		{Primary: LabelSearch, Confidence: 0.90},
		{Primary: LabelUnknown, Confidence: 0.30},
		{Primary: LabelNonCoding, Confidence: 0.80},
		{Primary: LabelContinuation, Confidence: 0.80},
		{},
		{Primary: LabelDocumentRead, Secondary: []IntentLabel{LabelFileWrite}, Confidence: 0.80},
		{Primary: LabelDocumentRead, Secondary: []IntentLabel{LabelFileRead}, Confidence: 0.80},
		{Primary: LabelDocumentRead, Secondary: []IntentLabel{LabelKnowledgeRead}, Confidence: 0.80},
		{Primary: LabelDocumentRead, Confidence: 0.80, WorkflowType: "presentation_design"},
		{Primary: LabelFileRead, Confidence: 0.80, WorkflowType: "coding"},
		{Primary: LabelLiveData, Secondary: []IntentLabel{LabelDocumentGenerate}, Confidence: 0.88},
	} {
		if _, ok := retainSettledOfficeLeader(l2, tree); ok {
			t.Fatalf("tree ruling %+v must stand", tree)
		}
	}
	for _, tree := range []ClassificationResult{
		{Primary: LabelFileRead, Confidence: 0.80},
		{Primary: LabelKnowledgeRead, Confidence: 0.80},
		{Primary: LabelDocumentRead, Secondary: []IntentLabel{LabelScreenshot}, Confidence: 0.80},
		{Primary: LabelFileRead, Secondary: []IntentLabel{LabelUnknown}, Confidence: 0.80},
		{Primary: LabelScreenshot, Confidence: 0.60, Layer: 3},
		{Primary: LabelScreenshot, Confidence: 0.90, Degraded: true},
		{Primary: LabelScreenshot, Secondary: []IntentLabel{LabelDocumentRead}, Confidence: 0.60, Layer: 3},
	} {
		got, ok := retainSettledOfficeLeader(l2, tree)
		if !ok || got.Primary != LabelOffice || !got.Degraded || len(got.Secondary) != 0 || got.RunnerUp != tree.Primary {
			t.Fatalf("read verdict %+v retained as %+v ok=%v", tree, got, ok)
		}
	}
}

// The tree is still the disambiguator between two file capabilities. A PDF
// ruling must not be pulled back to office just because office led the cosine.
func TestClassifyTreeDocumentGenerateStillReplacesOfficeLeader(t *testing.T) {
	emb := &staticEmbedder{vec: []float32{1, 0}}
	uic := New(Config{
		Embedder: emb,
		LLMFunc: func(_, _ string) (string, error) {
			return `{"top":[{"skill":"document_generate","score":0.88}]}`, nil
		},
	})
	uic.mu.Lock()
	uic.ready = true
	uic.anchors = []intentAnchor{
		{Label: LabelOffice, Vecs: [][]float32{{0.849, 0.5283937921666851}}},
		{Label: LabelDocumentGenerate, Vecs: [][]float32{{0.816, 0.5780467119878857}}},
		{Label: LabelLiveData, Vecs: [][]float32{{0.738, 0.6747992294005091}}},
	}
	uic.mu.Unlock()

	result := uic.Classify(MessageContext{Text: "把这些内容生成PDF"})
	if result.Primary == LabelOffice || !result.HasLabel(LabelDocumentGenerate) {
		t.Fatalf("result=%+v, want the tree PDF ruling kept as the artifact half", result)
	}
}
