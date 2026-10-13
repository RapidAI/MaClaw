package llmpool

import (
	"encoding/json"
	"testing"
)

func TestHeadEmbedFamilyCompat(t *testing.T) {
	legacy := EmptyHead(1, DefaultHeadTau)
	if legacy.EmbedFamily() != LegacyHeadEmbedModel {
		t.Fatalf("untagged head family = %q", legacy.EmbedFamily())
	}
	if HeadEmbedCompatible(&legacy, "qwen3") {
		t.Fatal("a legacy (gemma) head must not score qwen3 vectors")
	}
	if !HeadEmbedCompatible(&legacy, "gemma") || !HeadEmbedCompatible(&legacy, "") {
		t.Fatal("legacy head with gemma / unknown embedder must stay compatible")
	}
	q := EmptyHead(2, DefaultHeadTau)
	q.EmbedModel = "qwen3"
	if !HeadEmbedCompatible(&q, "QWEN3") || HeadEmbedCompatible(&q, "gemma") || HeadEmbedCompatible(nil, "qwen3") {
		t.Fatal("tagged head compat")
	}
	b, _ := json.Marshal(q)
	var back ClassificationHead
	json.Unmarshal(b, &back)
	if back.EmbedModel != "qwen3" || back.Clone().EmbedModel != "qwen3" {
		t.Fatalf("embed_model must survive JSON and Clone: %s", b)
	}
}

func TestSampleEmbeddingReusable(t *testing.T) {
	cases := []struct {
		model   string
		n       int
		current string
		want    bool
	}{
		{"", HeadDim, "gemma", true},
		{"", HeadDim, "qwen3", false}, // legacy Gemma vector after the switch → re-embed
		{"qwen3", 1024, "qwen3", true},
		{"qwen3", HeadDim - 1, "qwen3", false},
		{"gemma", 768, "", true},
	}
	for _, c := range cases {
		if got := SampleEmbeddingReusable(c.model, c.n, c.current); got != c.want {
			t.Errorf("SampleEmbeddingReusable(%q,%d,%q)=%v want %v", c.model, c.n, c.current, got, c.want)
		}
	}
}
