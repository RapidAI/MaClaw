package intent

import (
	"testing"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// fixedVecEmbedder returns one vector for every text. Scores tie, so Layer 2
// escalates and the tree result is a new struct. That is the path that used
// to drop QueryVec.
type fixedVecEmbedder struct {
	vec []float32
}

func (e fixedVecEmbedder) Embed(string) ([]float32, error) {
	out := make([]float32, len(e.vec))
	copy(out, e.vec)
	return out, nil
}

func (e fixedVecEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i], _ = e.Embed("")
	}
	return out, nil
}

func (e fixedVecEmbedder) Dim() int { return len(e.vec) }
func (fixedVecEmbedder) Close()     {}

func TestTreeVerdictKeepsLayer2QueryVector(t *testing.T) {
	uic := New(Config{
		Embedder: fixedVecEmbedder{vec: []float32{1, 0, 0, 0}},
		LLMFunc: func(systemPrompt, userText string) (string, error) {
			return `{"top":[{"skill":"coding","score":0.91,"reason":"code"}]}`, nil
		},
	})
	deadline := time.Now().Add(5 * time.Second)
	for !uic.Ready() {
		if time.Now().After(deadline) {
			t.Fatal("anchor warmup timed out")
		}
		time.Sleep(10 * time.Millisecond)
	}
	msg := MessageContext{Text: "QUERYMARK organize this into a file and mail it"}
	live := uic.Classify(msg)
	if live.Primary != LabelCoding {
		t.Fatalf("primary = %s, want coding (reason %q)", live.Primary, live.Reason)
	}
	if live.Layer != 3 {
		t.Fatalf("layer = %d, want the tree path, reason %q", live.Layer, live.Reason)
	}
	if len(live.QueryVec) != 4 || live.QueryVec[0] != 1 {
		t.Fatalf("live query vec = %v, want the layer-2 vector", live.QueryVec)
	}
	cached := uic.Classify(msg)
	if len(cached.QueryVec) != 0 {
		t.Fatalf("cached query vec len = %d, want the cache to drop it", len(cached.QueryVec))
	}
}

func TestCarryLayer2QueryEvidenceCopiesOnce(t *testing.T) {
	dst := ClassificationResult{Primary: LabelDocumentGenerate}
	src := ClassificationResult{
		QueryVec:     []float32{0.25, 0.5},
		AnchorMaxCos: map[string]float64{"mailbox_send": 0.4},
	}
	carryLayer2QueryEvidence(&dst, src)
	src.QueryVec[0] = 9
	src.AnchorMaxCos["mailbox_send"] = 1
	if dst.QueryVec[0] != 0.25 || dst.AnchorMaxCos["mailbox_send"] != 0.4 {
		t.Fatalf("evidence aliased the source: %+v", dst)
	}
	dst.QueryVec = []float32{1}
	carryLayer2QueryEvidence(&dst, src)
	if len(dst.QueryVec) != 1 || dst.QueryVec[0] != 1 {
		t.Fatalf("existing vector was replaced: %v", dst.QueryVec)
	}
}

func installQueryAnchors(uic *UnifiedIntentClassifier, anchors []intentAnchor) {
	uic.mu.Lock()
	uic.ready = true
	uic.anchors = anchors
	uic.mu.Unlock()
}

func TestEmbeddingOnlyHintKeepsLayer2QueryVector(t *testing.T) {
	uic := New(Config{Embedder: &staticEmbedder{vec: []float32{1, 0, 0}}})
	installQueryAnchors(uic, []intentAnchor{
		{Label: LabelLiveData, Vecs: [][]float32{{1, 0, 0}}},
		{Label: LabelSearch, Vecs: [][]float32{{1, 0, 0}}},
	})
	live := uic.Classify(MessageContext{Text: "QUERYMARK weather without a tree"})
	if live.Primary != LabelLiveData && live.Primary != LabelSearch {
		t.Fatalf("primary = %s, want a lookup hint (%s)", live.Primary, live.Reason)
	}
	if !live.Degraded || len(live.QueryVec) != 3 || live.QueryVec[0] != 1 {
		t.Fatalf("live = %+v, want the layer-2 vector on the hint", live)
	}
}

func TestShortLookupCacheDropsQueryVector(t *testing.T) {
	uic := New(Config{Embedder: &staticEmbedder{vec: []float32{1, 0}}})
	// Cosine 0.5 stays under the lookup floor, so a two-rune search guess
	// skips the tree and is the degraded hint the cache is allowed to keep.
	installQueryAnchors(uic, []intentAnchor{
		{Label: LabelSearch, Vecs: [][]float32{{0.5, 0.8660254}}},
	})
	msg := MessageContext{Text: "天气"}
	live := uic.Classify(msg)
	if live.Primary != LabelSearch || !live.Degraded {
		t.Fatalf("live = %+v, want a degraded search hint", live)
	}
	if len(live.QueryVec) != 2 || live.QueryVec[0] != 1 {
		t.Fatalf("live query vec = %v", live.QueryVec)
	}
	cached := uic.Classify(msg)
	if cached.Primary != LabelSearch || len(cached.QueryVec) != 0 {
		t.Fatalf("cached = primary %s vec %v, want the hint without the vector", cached.Primary, cached.QueryVec)
	}
}

func TestVetoedHintKeepsLayer2QueryVector(t *testing.T) {
	t.Setenv(HeadHintFixEnv, "1")
	uic := ambiguousLookupUIC(headUncertain("document_generate"))
	live := uic.Classify(MessageContext{Text: hintText})
	if live.Primary != LabelUnknown || live.RunnerUp == "" {
		t.Fatalf("live = %+v, want a vetoed unknown", live)
	}
	if len(live.QueryVec) != 3 || live.QueryVec[0] != 1 {
		t.Fatalf("vetoed query vec = %v, want the layer-2 vector", live.QueryVec)
	}
}

func TestContradictedTreeKeepsLayer2QueryVector(t *testing.T) {
	uic := New(Config{
		Embedder: &staticEmbedder{vec: []float32{1, 0, 0}},
		LLMFunc: func(string, string) (string, error) {
			return `{"top":[{"skill":"coding","score":0.95,"reason":"code"}]}`, nil
		},
	})
	installQueryAnchors(uic, []intentAnchor{
		{Label: LabelLiveData, Vecs: [][]float32{{1, 0, 0}}},
		{Label: LabelSearch, Vecs: [][]float32{{1, 0, 0}}},
	})
	live := uic.Classify(MessageContext{Text: "QUERYMARK this lookup must not become coding"})
	if live.Primary != LabelLiveData && live.Primary != LabelSearch {
		t.Fatalf("primary = %s, want the lookup hint (%s)", live.Primary, live.Reason)
	}
	if len(live.QueryVec) != 3 || live.QueryVec[0] != 1 {
		t.Fatalf("contradicted query vec = %v", live.QueryVec)
	}
}

func TestStoreRecoveredClassificationDropsQueryVector(t *testing.T) {
	uic := New(Config{Embedder: embedding.NoopEmbedder{}})
	msg := MessageContext{Text: "recover query vec"}
	live := ClassificationResult{
		Primary: LabelCoding, Confidence: 0.95, Layer: 3,
		QueryVec: []float32{1, 2, 3, 4},
	}
	uic.StoreRecoveredClassification(msg, live)
	if len(live.QueryVec) != 4 || live.QueryVec[0] != 1 {
		t.Fatal("store mutated the caller vector")
	}
	got, ok := uic.ClassifyCached(msg)
	if !ok || got.Primary != LabelCoding || len(got.QueryVec) != 0 {
		t.Fatalf("cached primary=%s vec=%v ok=%v", got.Primary, got.QueryVec, ok)
	}
}

func TestEmptyClassificationDropsQueryVector(t *testing.T) {
	uic := New(Config{Embedder: &staticEmbedder{vec: []float32{1, 0, 0}}})
	installQueryAnchors(uic, []intentAnchor{
		{Label: LabelSearch, Vecs: [][]float32{{1, 0, 0}}},
	})
	msg := MessageContext{Text: ""}
	live := uic.Classify(msg)
	if len(live.QueryVec) != 0 || len(live.AnchorMaxCos) != 0 {
		t.Fatalf("empty text kept query evidence: vec=%v anchors=%v", live.QueryVec, live.AnchorMaxCos)
	}
	cached, ok := uic.cache.Load(classificationCacheKey(uic.cacheEpoch.Load(), msg))
	if !ok {
		t.Fatal("empty text hint was not cached")
	}
	stored := cached.(*ClassificationResult)
	if len(stored.QueryVec) != 0 {
		t.Fatalf("cached empty text vec = %v", stored.QueryVec)
	}
}

func TestShortLookupCacheStoresSkillRelease(t *testing.T) {
	text := "用skill"
	if utf8.RuneCountInString(text) > shortAmbiguousLookupMaxRunes {
		t.Fatalf("%q is %d runes, past the short-lookup cap", text, utf8.RuneCountInString(text))
	}
	uic := New(Config{Embedder: &staticEmbedder{vec: []float32{1, 0}}})
	installQueryAnchors(uic, []intentAnchor{
		{Label: LabelSearch, Vecs: [][]float32{{0.5, 0.8660254}}},
	})
	msg := MessageContext{Text: text}
	live := uic.Classify(msg)
	if live.Primary != "" {
		t.Fatalf("live primary = %s, want the named-skill release (%s)", live.Primary, live.Reason)
	}
	if len(live.QueryVec) != 2 || live.QueryVec[0] != 1 {
		t.Fatalf("live query vec = %v", live.QueryVec)
	}
	cached, ok := uic.cache.Load(classificationCacheKey(uic.cacheEpoch.Load(), msg))
	if !ok {
		t.Fatal("short lookup hint was not cached")
	}
	stored := cached.(*ClassificationResult)
	if stored.Primary != "" || len(stored.QueryVec) != 0 {
		t.Fatalf("cached primary=%s vec=%v, want the released hint without the vector", stored.Primary, stored.QueryVec)
	}
}

func TestOverrideCompositeOwnsQueryVector(t *testing.T) {
	l2 := ClassificationResult{
		Primary:      LabelLiveData,
		Secondary:    []IntentLabel{LabelDocumentGenerate},
		Confidence:   0.9,
		QueryVec:     []float32{0.25, 0.5},
		AnchorMaxCos: map[string]float64{"live_data": 0.9},
	}
	got, ok := overrideCompositeByHead(l2, string(LabelDocumentGenerate), 0.99, 0.96)
	if !ok || got.Primary != LabelDocumentGenerate {
		t.Fatalf("override = %+v ok=%v", got, ok)
	}
	l2.QueryVec[0] = 9
	l2.AnchorMaxCos["live_data"] = 0
	if len(got.QueryVec) != 2 || got.QueryVec[0] != 0.25 || got.AnchorMaxCos["live_data"] != 0.9 {
		t.Fatalf("override aliased layer-2 evidence: vec=%v anchors=%v", got.QueryVec, got.AnchorMaxCos)
	}
}
