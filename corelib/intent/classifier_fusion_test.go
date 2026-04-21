package intent

import (
	"testing"
)

// TestFusionToClassification_Clear verifies that a CLEAR fusion verdict
// maps correctly to a ClassificationResult.
func TestFusionToClassification_Clear(t *testing.T) {
	u := &UnifiedIntentClassifier{
		affinity: NewToolAffinityRegistry(),
	}

	fr := FusionResult{
		Verdict: VerdictClear,
		Top:     FusedCandidate{Label: LabelSSH, FinalScore: 0.82, InEmb: true, InTree: true},
		Candidates: []FusedCandidate{
			{Label: LabelSSH, FinalScore: 0.82},
			{Label: LabelCoding, FinalScore: 0.20},
		},
		ActiveChannels: []string{"embedding", "tree"},
	}
	l1 := ClassificationResult{Primary: LabelUnknown, Confidence: 0}

	result := u.fusionToClassification(fr, l1)

	if result.Primary != LabelSSH {
		t.Errorf("expected primary=ssh, got %s", result.Primary)
	}
	if result.Confidence != 0.82 {
		t.Errorf("expected confidence=0.82, got %.2f", result.Confidence)
	}
	if result.Layer != 23 {
		t.Errorf("expected layer=23 (fusion), got %d", result.Layer)
	}
}

// TestFusionToClassification_Ambiguous verifies that an AMBIGUOUS verdict
// populates the Secondary field with the runner-up.
func TestFusionToClassification_Ambiguous(t *testing.T) {
	u := &UnifiedIntentClassifier{
		affinity: NewToolAffinityRegistry(),
	}

	runnerUp := FusedCandidate{Label: LabelMaintenance, FinalScore: 0.48}
	fr := FusionResult{
		Verdict:  VerdictAmbiguous,
		Top:      FusedCandidate{Label: LabelCoding, FinalScore: 0.52},
		RunnerUp: &runnerUp,
		Candidates: []FusedCandidate{
			{Label: LabelCoding, FinalScore: 0.52},
			{Label: LabelMaintenance, FinalScore: 0.48},
		},
		ActiveChannels: []string{"embedding", "tree"},
	}
	l1 := ClassificationResult{Primary: LabelUnknown, Confidence: 0}

	result := u.fusionToClassification(fr, l1)

	if result.Primary != LabelCoding {
		t.Errorf("expected primary=coding, got %s", result.Primary)
	}
	if len(result.Secondary) != 1 || result.Secondary[0] != LabelMaintenance {
		t.Errorf("expected secondary=[maintenance], got %v", result.Secondary)
	}
}

// TestFusionToClassification_Low_FallsBackToL1 verifies that a LOW verdict
// with a better L1 result falls back to L1.
func TestFusionToClassification_Low_FallsBackToL1(t *testing.T) {
	u := &UnifiedIntentClassifier{
		affinity: NewToolAffinityRegistry(),
	}

	fr := FusionResult{
		Verdict:        VerdictLow,
		Top:            FusedCandidate{Label: LabelUnknown, FinalScore: 0.05},
		ActiveChannels: []string{"embedding", "tree"},
	}
	l1 := ClassificationResult{Primary: LabelContinuation, Confidence: 0.85, Layer: 1}

	result := u.fusionToClassification(fr, l1)

	// L1 had better signal, should fall back.
	if result.Primary != LabelContinuation {
		t.Errorf("expected fallback to L1 continuation, got %s", result.Primary)
	}
	if result.Layer != 1 {
		t.Errorf("expected layer=1 (L1 fallback), got %d", result.Layer)
	}
}

// TestFusionToClassification_BothChannelsFailed verifies that when both
// channels fail, the L1 fallback is used.
func TestFusionToClassification_BothChannelsFailed(t *testing.T) {
	u := &UnifiedIntentClassifier{
		affinity: NewToolAffinityRegistry(),
	}

	fr := FusionResult{
		Verdict:        VerdictLow,
		ActiveChannels: []string{}, // both failed
		Degraded:       true,
	}
	l1 := ClassificationResult{Primary: LabelSSH, Confidence: 0.92, Layer: 1}

	result := u.fusionToClassification(fr, l1)

	if result.Primary != LabelSSH {
		t.Errorf("expected L1 fallback to ssh, got %s", result.Primary)
	}
}

// TestEmbeddingTopK_ReturnsTopK verifies that embeddingTopK returns at most
// topK results sorted by score descending.
func TestEmbeddingTopK_SortedDescending(t *testing.T) {
	// This test uses the fusion MergeAndScore to verify sorting behavior
	// since embeddingTopK requires a real embedder.
	emb := []labelScore{
		LabelScore(LabelCoding, 0.90),
		LabelScore(LabelSSH, 0.70),
		LabelScore(LabelBugFix, 0.80),
	}

	candidates := MergeAndScore(emb, nil, 1.0)

	if len(candidates) != 3 {
		t.Fatalf("expected 3 candidates, got %d", len(candidates))
	}
	// Should be sorted: coding(0.90) > bugfix(0.80) > ssh(0.70)
	if candidates[0].Label != LabelCoding {
		t.Errorf("expected first=coding, got %s", candidates[0].Label)
	}
	if candidates[1].Label != LabelBugFix {
		t.Errorf("expected second=bug_fix, got %s", candidates[1].Label)
	}
	if candidates[2].Label != LabelSSH {
		t.Errorf("expected third=ssh, got %s", candidates[2].Label)
	}
}

// TestFusionConfig_Defaults verifies default fusion parameters.
func TestFusionConfig_Defaults(t *testing.T) {
	cfg := DefaultFusionConfig()

	if cfg.Alpha != 0.15 {
		t.Errorf("expected alpha=0.15, got %.2f", cfg.Alpha)
	}
	if cfg.Delta != 0.10 {
		t.Errorf("expected delta=0.10, got %.2f", cfg.Delta)
	}
	if cfg.LowThreshold != 0.15 {
		t.Errorf("expected lowThreshold=0.15, got %.2f", cfg.LowThreshold)
	}
}

// TestSetFusionConfig verifies runtime config update.
func TestSetFusionConfig(t *testing.T) {
	u := &UnifiedIntentClassifier{
		fusionCfg: DefaultFusionConfig(),
	}

	newCfg := FusionConfig{Alpha: 0.30, Delta: 0.20, LowThreshold: 0.25}
	u.SetFusionConfig(newCfg)

	got := u.GetFusionConfig()
	if got.Alpha != 0.30 || got.Delta != 0.20 || got.LowThreshold != 0.25 {
		t.Errorf("config not updated: %+v", got)
	}
}
