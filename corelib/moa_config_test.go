package corelib

import "testing"

func TestNormalizeAndValidateMoAConfig(t *testing.T) {
	trueVal := true
	cfg := MoAConfig{
		Enabled:       true,
		DefaultPreset: "Review",
		Presets: map[string]MoAPresetConfig{
			"Review": {
				Enabled:     true,
				DisplayName: "方案评审",
				Aggregator:  MoAModelRef{UsePrimary: true},
				ReferenceModels: []MoAModelRef{
					{Provider: "OpenAI", Model: "gpt-4.1"},
					{TaskRoute: "reasoning"},
					{}, // dropped
				},
				ReferenceMaxTokens: 600,
			},
		},
		OnlyBeforeFirstTool: &trueVal,
	}
	n := NormalizeMoAConfig(cfg)
	if _, ok := n.Presets["review"]; !ok {
		t.Fatalf("expected normalized preset key review, got %#v", n.Presets)
	}
	if n.DefaultPreset != "review" {
		t.Fatalf("default_preset=%q", n.DefaultPreset)
	}
	if len(n.Presets["review"].ReferenceModels) != 2 {
		t.Fatalf("refs=%d", len(n.Presets["review"].ReferenceModels))
	}
	known := map[string]struct{}{"OpenAI": {}}
	if err := ValidateMoAConfig(n, known); err != nil {
		t.Fatalf("ValidateMoAConfig: %v", err)
	}
}

func TestValidateMoAConfigRejectsNestingAndMissingAgg(t *testing.T) {
	cfg := MoAConfig{
		Enabled: true,
		Presets: map[string]MoAPresetConfig{
			"bad": {
				Enabled:    true,
				Aggregator: MoAModelRef{Model: "moa:other"},
			},
		},
	}
	if err := ValidateMoAConfig(cfg, nil); err == nil {
		t.Fatal("expected nest rejection")
	}
	cfg2 := MoAConfig{
		Enabled: true,
		Presets: map[string]MoAPresetConfig{
			"empty": {Enabled: true},
		},
	}
	if err := ValidateMoAConfig(cfg2, nil); err == nil {
		t.Fatal("expected empty aggregator rejection")
	}
}

func TestMoAEffectiveDefaults(t *testing.T) {
	var c MoAConfig
	if c.EffectiveMaxReferences() != 3 || c.EffectiveFanoutMaxIterations() != 1 || !c.EffectiveOnlyBeforeFirstTool() {
		t.Fatalf("defaults: maxRefs=%d fanout=%d onlyBefore=%v", c.EffectiveMaxReferences(), c.EffectiveFanoutMaxIterations(), c.EffectiveOnlyBeforeFirstTool())
	}
}
