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
