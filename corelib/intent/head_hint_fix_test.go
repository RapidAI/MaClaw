package intent

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type familyEmbedder struct {
	staticEmbedder
	id string
}

func (f *familyEmbedder) ModelID() string { return f.id }

func TestHeadHintFixFlagDefaultsOnForQwen3Only(t *testing.T) {
	q := &familyEmbedder{staticEmbedder{vec: []float32{1, 0}}, "Qwen3 Embedding 0.6b:1024:prompt-v1"}
	g := &familyEmbedder{staticEmbedder{vec: []float32{1, 0}}, "embeddinggemma-300m:256"}
	t.Setenv(HeadHintFixEnv, "")
	if !headHintFixEnabled(q) {
		t.Fatal("unset flag must default ON for the Qwen3 family")
	}
	if headHintFixEnabled(g) {
		t.Fatal("unset flag must default OFF for Gemma")
	}
	t.Setenv(HeadHintFixEnv, "0")
	if headHintFixEnabled(q) {
		t.Fatal("MACLAW_INTENT_HEAD_HINT_FIX=0 must disable for Qwen3")
	}
	t.Setenv(HeadHintFixEnv, "on")
	if !headHintFixEnabled(g) {
		t.Fatal("MACLAW_INTENT_HEAD_HINT_FIX=on must force-enable")
	}
}

// ambiguousLookupUIC reproduces the production degraded-hint shape: L2 ties
// live_data/search (unconfirmed), and Layer 3 is the given callback.
func ambiguousLookupUIC(l3 LLMClassifyContextFunc) *UnifiedIntentClassifier {
	uic := New(Config{Embedder: &staticEmbedder{vec: []float32{1, 0, 0}}, LLMContextFunc: l3})
	uic.mu.Lock()
	uic.ready = true
	uic.anchors = []intentAnchor{
		{Label: LabelLiveData, Vecs: [][]float32{{1, 0, 0}}},
		{Label: LabelSearch, Vecs: [][]float32{{1, 0, 0}}},
	}
	uic.mu.Unlock()
	return uic
}

const hintText = "帮我分析一下这个现象到底应该怎么理解比较好"

func headUncertain(label string) LLMClassifyContextFunc {
	return func(context.Context, context.Context, string, string) (string, error) {
		return "", NewLocalHeadUncertainError(label, 0.61, 0.87)
	}
}

func TestHeadHintFixVetoesHintTheHeadDisagreesWith(t *testing.T) {
	t.Setenv(HeadHintFixEnv, "1")
	r := ambiguousLookupUIC(headUncertain("document_generate")).Classify(MessageContext{Text: hintText})
	if r.Primary != LabelUnknown || !r.Degraded || r.Layer != 2 {
		t.Fatalf("result=%+v, want degraded unknown (vetoed hint)", r)
	}
	if r.RunnerUp != LabelLiveData && r.RunnerUp != LabelSearch {
		t.Fatalf("RunnerUp=%q, want the vetoed lookup hint kept as evidence", r.RunnerUp)
	}
	if !strings.Contains(r.Reason, "local head disagrees") {
		t.Fatalf("reason=%q", r.Reason)
	}
}

func TestHeadHintFixOffKeepsLegacyHint(t *testing.T) {
	t.Setenv(HeadHintFixEnv, "0")
	r := ambiguousLookupUIC(headUncertain("document_generate")).Classify(MessageContext{Text: hintText})
	if !isDegradedLookupHint(r) {
		t.Fatalf("flag off: result=%+v, want legacy degraded lookup hint", r)
	}
}

func TestHeadHintFixKeepsHintTheHeadAgreesWith(t *testing.T) {
	t.Setenv(HeadHintFixEnv, "0")
	legacy := ambiguousLookupUIC(headUncertain("document_generate")).Classify(MessageContext{Text: hintText})
	t.Setenv(HeadHintFixEnv, "1")
	r := ambiguousLookupUIC(headUncertain(string(legacy.Primary))).Classify(MessageContext{Text: hintText})
	if !isDegradedLookupHint(r) || r.Primary != legacy.Primary {
		t.Fatalf("head agrees with hint %s: result=%+v, want the hint kept", legacy.Primary, r)
	}
}

func TestHeadHintFixIgnoresRemoteTreeFailure(t *testing.T) {
	t.Setenv(HeadHintFixEnv, "1")
	remoteFail := func(context.Context, context.Context, string, string) (string, error) {
		return "", errors.New("remote tree: bad gateway body")
	}
	r := ambiguousLookupUIC(remoteFail).Classify(MessageContext{Text: hintText})
	if !isDegradedLookupHint(r) {
		t.Fatalf("remote failure carries no head verdict: result=%+v, want legacy hint", r)
	}
}

// compositeUIC reproduces a confident L2 declared composite
// (document_generate + live_data evidence) with a fixed head probe.
func compositeUIC(probe LocalHeadProbeFunc) *UnifiedIntentClassifier {
	uic := New(Config{Embedder: &staticEmbedder{vec: []float32{1, 0}}, LocalHeadProbe: probe})
	uic.mu.Lock()
	uic.ready = true
	uic.anchors = []intentAnchor{
		{Label: LabelDocumentGenerate, Vecs: [][]float32{{0.947, 0.321234}}},
		{Label: LabelNonCoding, Vecs: [][]float32{{0.87, 0.493052}}},
		{Label: LabelLiveData, Vecs: [][]float32{{0.85, 0.526783}}},
	}
	uic.mu.Unlock()
	return uic
}

func fixedProbe(label string, prob float64) LocalHeadProbeFunc {
	return func(context.Context, string) (string, float64, float64, bool) { return label, prob, 0.96, true }
}

const compositeText = "render the requested result as a file"

func TestHeadHintFixConfidentHeadOverridesComposite(t *testing.T) {
	t.Setenv(HeadHintFixEnv, "0")
	base := compositeUIC(fixedProbe("search", 0.99)).Classify(MessageContext{Text: compositeText})
	if !isDeclaredCompositeResult(base) {
		t.Fatalf("fixture must be a declared composite, got %+v", base)
	}
	// Pick a head label that differs from the composite's primary.
	headLabel := string(LabelDocumentGenerate)
	if base.Primary == LabelDocumentGenerate {
		headLabel = string(LabelLiveData)
	}
	t.Setenv(HeadHintFixEnv, "1")
	r := compositeUIC(fixedProbe(headLabel, 0.99)).Classify(MessageContext{Text: compositeText})
	if string(r.Primary) != headLabel || len(r.Secondary) != 0 || r.Layer != 3 {
		t.Fatalf("result=%+v, want head label %s alone at layer 3", r, headLabel)
	}
	if !strings.Contains(r.Reason, "local head overrides L2 composite") {
		t.Fatalf("reason=%q", r.Reason)
	}
}

func TestHeadHintFixWeakOrAgreeingHeadKeepsComposite(t *testing.T) {
	t.Setenv(HeadHintFixEnv, "1")
	base := compositeUIC(nil).Classify(MessageContext{Text: compositeText})
	other := string(LabelDocumentGenerate)
	if base.Primary == LabelDocumentGenerate {
		other = string(LabelLiveData)
	}
	for name, p := range map[string]LocalHeadProbeFunc{
		"below strong threshold": fixedProbe(other, 0.95),
		"agrees with primary":    fixedProbe(string(base.Primary), 0.999),
		"head says unknown":      fixedProbe("unknown", 0.999),
		"probe unavailable":      func(context.Context, string) (string, float64, float64, bool) { return "", 0, 0, false },
	} {
		r := compositeUIC(p).Classify(MessageContext{Text: compositeText})
		if r.Primary != base.Primary || !isDeclaredCompositeResult(r) {
			t.Fatalf("%s: result=%+v, want composite %+v kept", name, r, base)
		}
	}
	t.Setenv(HeadHintFixEnv, "0")
	r := compositeUIC(fixedProbe(other, 0.999)).Classify(MessageContext{Text: compositeText})
	if r.Primary != base.Primary || !isDeclaredCompositeResult(r) {
		t.Fatalf("flag off: result=%+v, want composite kept", r)
	}
}

func TestLocalTreeFuncUncertainCarriesHeadTop(t *testing.T) {
	h, err := ParseLocalHead(tinyHeadJSON(t, []string{"search", "ssh", "unknown"}, 4, 3, 0.999, 0.999, []string{"ssh"}))
	if err != nil {
		t.Fatal(err)
	}
	emb := stubEmbedder{dim: h.Dim()}
	_, err = LocalTreeFunc(emb, h, nil)(context.Background(), context.Background(), "", "hello")
	label, prob, ok := LocalHeadUncertainTop(err)
	if !ok || label == "" || prob <= 0 || prob >= 0.999 {
		t.Fatalf("err=%v label=%q prob=%v ok=%v, want the head's sub-threshold top-1", err, label, prob, ok)
	}
	if _, _, ok := LocalHeadUncertainTop(errors.New("local head uncertain: x")); ok {
		t.Fatal("plain error must not carry a head verdict")
	}
	pl, pp, strong, ok := LocalHeadProbe(emb, h)(context.Background(), "hello")
	if !ok || pl != label || pp != prob || strong != 0.999 {
		t.Fatalf("probe=(%q,%v,%v,%v), want (%q,%v,0.999,true)", pl, pp, strong, ok, label, prob)
	}
	if LocalHeadProbe(emb, nil) != nil {
		t.Fatal("nil head must give a nil probe")
	}
}
