package intent

import (
	"testing"
)

func TestMergeAndScore_BothChannels(t *testing.T) {
	emb := []labelScore{
		{LabelCoding, 0.85},
		{LabelBugFix, 0.60},
		{LabelNonCoding, 0.40},
	}
	tree := []labelScore{
		{LabelCoding, 0.90},
		{LabelMaintenance, 0.30},
	}

	candidates := MergeAndScore(emb, tree, 0.15, nil)

	if len(candidates) == 0 {
		t.Fatal("expected candidates")
	}

	// coding should be top (in both channels)
	top := candidates[0]
	if top.Label != LabelCoding {
		t.Errorf("expected top=coding, got %s", top.Label)
	}
	if !top.InEmb || !top.InTree {
		t.Error("coding should be in both channels")
	}

	// Verify formula: 0.15 * 0.85 + 0.85 * 0.90 = 0.1275 + 0.765 = 0.8925
	expectedScore := 0.15*0.85 + 0.85*0.90
	if diff := top.FinalScore - expectedScore; diff > 0.001 || diff < -0.001 {
		t.Errorf("expected score %.4f, got %.4f", expectedScore, top.FinalScore)
	}
}

func TestMergeAndScore_EmbeddingOnly(t *testing.T) {
	emb := []labelScore{
		{LabelSSH, 0.80},
	}
	var tree []labelScore // empty

	candidates := MergeAndScore(emb, tree, 1.0, nil) // alpha=1.0 (embedding only)

	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
	if candidates[0].Label != LabelSSH {
		t.Errorf("expected ssh, got %s", candidates[0].Label)
	}
	if candidates[0].FinalScore != 0.80 {
		t.Errorf("expected score 0.80, got %.4f", candidates[0].FinalScore)
	}
}

func TestMergeAndScore_TreeOnly(t *testing.T) {
	var emb []labelScore // empty
	tree := []labelScore{
		{LabelBrowser, 0.75},
	}

	candidates := MergeAndScore(emb, tree, 0.0, nil) // alpha=0.0 (tree only)

	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
	if candidates[0].Label != LabelBrowser {
		t.Errorf("expected browser, got %s", candidates[0].Label)
	}
	if candidates[0].FinalScore != 0.75 {
		t.Errorf("expected score 0.75, got %.4f", candidates[0].FinalScore)
	}
}

func TestMergeAndScore_CrossChannelAgreementBoost(t *testing.T) {
	// Candidate in both channels should score higher than one in only one channel.
	emb := []labelScore{
		{LabelCoding, 0.70},
		{LabelBugFix, 0.80}, // higher emb score
	}
	tree := []labelScore{
		{LabelCoding, 0.85}, // coding also in tree
		// bug_fix NOT in tree
	}

	candidates := MergeAndScore(emb, tree, 0.15, nil)

	// coding: 0.15*0.70 + 0.85*0.85 = 0.105 + 0.7225 = 0.8275
	// bug_fix: 0.15*0.80 + 0.85*0.00 = 0.12
	// coding should win despite lower emb score
	if candidates[0].Label != LabelCoding {
		t.Errorf("expected coding to win via cross-channel agreement, got %s", candidates[0].Label)
	}
}

func TestDecide_Clear(t *testing.T) {
	candidates := []FusedCandidate{
		{Label: LabelCoding, FinalScore: 0.85},
		{Label: LabelBugFix, FinalScore: 0.30},
	}
	cfg := DefaultFusionConfig()

	result := Decide(candidates, cfg)

	if result.Verdict != VerdictClear {
		t.Errorf("expected CLEAR, got %s", result.Verdict)
	}
	if result.Top.Label != LabelCoding {
		t.Errorf("expected top=coding, got %s", result.Top.Label)
	}
	if result.RunnerUp != nil {
		t.Error("CLEAR verdict should not have runner-up")
	}
}

func TestDecide_Ambiguous(t *testing.T) {
	candidates := []FusedCandidate{
		{Label: LabelCoding, FinalScore: 0.55},
		{Label: LabelMaintenance, FinalScore: 0.50}, // gap = 0.05 < delta 0.10
	}
	cfg := DefaultFusionConfig()

	result := Decide(candidates, cfg)

	if result.Verdict != VerdictAmbiguous {
		t.Errorf("expected AMBIGUOUS, got %s", result.Verdict)
	}
	if result.RunnerUp == nil {
		t.Fatal("AMBIGUOUS verdict should have runner-up")
	}
	if result.RunnerUp.Label != LabelMaintenance {
		t.Errorf("expected runner-up=maintenance, got %s", result.RunnerUp.Label)
	}
}

func TestDecide_Low(t *testing.T) {
	candidates := []FusedCandidate{
		{Label: LabelUnknown, FinalScore: 0.05},
	}
	cfg := DefaultFusionConfig()

	result := Decide(candidates, cfg)

	if result.Verdict != VerdictLow {
		t.Errorf("expected LOW, got %s", result.Verdict)
	}
}

func TestDecide_Empty(t *testing.T) {
	result := Decide(nil, DefaultFusionConfig())
	if result.Verdict != VerdictLow {
		t.Errorf("expected LOW for empty candidates, got %s", result.Verdict)
	}
}
