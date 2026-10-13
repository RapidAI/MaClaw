package httpapi

import (
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

// After the single-model switch (Gemma → Qwen3) a stored head trained on Gemma
// vectors must stop scoring (rules only), and retraining must re-embed the
// stored previews instead of reusing cached Gemma vectors.
func TestClassHeadEmbedderSwitchBlocksLegacyHeadAndRetrainsFromPreviews(t *testing.T) {
	embedCalls := 0
	origEmb, origFam := classHeadEmbedderFn, classHeadEmbedFamilyFn
	classHeadEmbedderFn = func(preview string) ([]float32, error) {
		embedCalls++
		vec := make([]float32, llmpool.HeadDim)
		for i, class := range llmpool.FrozenWorkloadClasses {
			if strings.HasPrefix(preview, class+" ") {
				vec[i] = 1
			}
		}
		return vec, nil
	}
	classHeadEmbedFamilyFn = func() string { return "qwen3" }
	t.Cleanup(func() { classHeadEmbedderFn, classHeadEmbedFamilyFn = origEmb, origFam })

	sys := &stubSystemSettings{data: map[string]string{}}
	samples := make([]llmClassHeadSample, 0, len(llmpool.FrozenWorkloadClasses))
	for _, class := range llmpool.FrozenWorkloadClasses {
		stale := make([]float32, llmpool.HeadDim) // cached Gemma vector (untagged)
		stale[0] = 1
		samples = append(samples, llmClassHeadSample{ID: class, At: time.Now().UTC(), Preview: class + " request",
			Embedding: stale, Gold: true, GoldClass: class, RuleClass: class})
	}
	legacy := llmpool.EmptyHead(1, llmpool.DefaultHeadTau) // trained on Gemma (untagged)
	llmClassHeadMu.Lock()
	data := loadLLMClassHeadLocked(t.Context(), sys, "")
	data.Samples = samples
	data.Current = &legacy
	data.Pipeline = llmpool.PipelineOn
	if err := saveLLMClassHeadLocked(t.Context(), sys, "", data); err != nil {
		llmClassHeadMu.Unlock()
		t.Fatal(err)
	}
	llmClassHeadMu.Unlock()

	if rt := classHeadRuntime(sys, "u1", ""); rt != nil {
		if p := rt.Predict("coding request"); p.Class != llmpool.WorkloadFallbackBalanced || embedCalls != 0 {
			t.Fatalf("legacy head must not score qwen3 vectors: %+v calls=%d", p, embedCalls)
		}
	}

	if err := trainClassHeadNow(sys, ""); err != nil {
		t.Fatal(err)
	}
	if embedCalls != len(llmpool.FrozenWorkloadClasses) {
		t.Fatalf("every cached Gemma vector must be re-embedded: calls=%d", embedCalls)
	}
	llmClassHeadMu.Lock()
	got := loadLLMClassHeadLocked(t.Context(), sys, "")
	llmClassHeadMu.Unlock()
	if got.Current == nil || got.Current.EmbedModel != "qwen3" {
		t.Fatalf("retrained head must be tagged qwen3: %#v", got.Current)
	}
	for _, s := range got.Samples {
		if s.EmbedModel != "qwen3" {
			t.Fatalf("sample %s cache not retagged: %q", s.ID, s.EmbedModel)
		}
	}
	// A second training run reuses the (now qwen3) cache.
	if err := trainClassHeadNow(sys, ""); err != nil {
		t.Fatal(err)
	}
	if embedCalls != len(llmpool.FrozenWorkloadClasses) {
		t.Fatalf("qwen3-tagged cache must be reused: calls=%d", embedCalls)
	}
}
