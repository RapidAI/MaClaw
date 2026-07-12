package corelib

import "testing"

func TestIsKnowledgeAutoRecallEnabledDefaultTrue(t *testing.T) {
	t.Parallel()
	var cfg AppConfig
	if !cfg.IsKnowledgeAutoRecallEnabled() {
		t.Fatal("nil KnowledgeAutoRecallEnabled should default to true")
	}
	off := false
	cfg.KnowledgeAutoRecallEnabled = &off
	if cfg.IsKnowledgeAutoRecallEnabled() {
		t.Fatal("explicit false should disable auto-recall")
	}
	on := true
	cfg.KnowledgeAutoRecallEnabled = &on
	if !cfg.IsKnowledgeAutoRecallEnabled() {
		t.Fatal("explicit true should enable auto-recall")
	}
}

func TestEffectiveKnowledgeAutoRecallMinScore(t *testing.T) {
	t.Parallel()
	var cfg AppConfig
	if got := cfg.EffectiveKnowledgeAutoRecallMinScore(); got != DefaultKnowledgeAutoRecallMinScore {
		t.Fatalf("default min score = %v, want %v", got, DefaultKnowledgeAutoRecallMinScore)
	}
	cfg.KnowledgeAutoRecallMinScore = 1.25
	if got := cfg.EffectiveKnowledgeAutoRecallMinScore(); got != 1.25 {
		t.Fatalf("custom min score = %v, want 1.25", got)
	}
	cfg.KnowledgeAutoRecallMinScore = -1
	if got := cfg.EffectiveKnowledgeAutoRecallMinScore(); got != DefaultKnowledgeAutoRecallMinScore {
		t.Fatalf("non-positive min score should fall back, got %v", got)
	}
}
