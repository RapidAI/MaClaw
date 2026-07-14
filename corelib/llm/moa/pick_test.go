package moa

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestPickPresetName(t *testing.T) {
	cfg := corelib.MoAConfig{
		DefaultPreset: "review",
		Presets: map[string]corelib.MoAPresetConfig{
			"review": {Enabled: true},
			"alpha":  {Enabled: true},
		},
	}
	if got := PickPresetName(cfg, ""); got != "review" {
		t.Fatalf("default: %q", got)
	}
	if got := PickPresetName(cfg, "MOA:Alpha"); got != "alpha" {
		t.Fatalf("requested: %q", got)
	}
	if got := PickPresetName(cfg, "missing"); got != "missing" {
		t.Fatalf("missing returns requested for caller error: %q", got)
	}
	// Explicit garbage must NOT silently pick default.
	if got := PickPresetName(cfg, "!!!"); got == "review" || got == "" {
		t.Fatalf("invalid explicit must miss, got %q", got)
	}
	empty := corelib.MoAConfig{}
	if got := PickPresetName(empty, ""); got != "" {
		t.Fatalf("empty: %q", got)
	}
	// No default → sorted first key
	cfg2 := corelib.MoAConfig{
		Presets: map[string]corelib.MoAPresetConfig{
			"zeta":  {Enabled: true},
			"alpha": {Enabled: true},
		},
	}
	if got := PickPresetName(cfg2, ""); got != "alpha" {
		t.Fatalf("sorted: %q", got)
	}
	// Spaces / punctuation must match NormalizeMoAConfig keys.
	cfg3 := corelib.NormalizeMoAConfig(corelib.MoAConfig{
		Presets: map[string]corelib.MoAPresetConfig{
			"My Review!": {Enabled: true},
		},
	})
	if _, ok := cfg3.Presets["my-review"]; !ok {
		t.Fatalf("normalize keys: %#v", cfg3.Presets)
	}
	if got := PickPresetName(cfg3, "My Review!"); got != "my-review" {
		t.Fatalf("space name: %q", got)
	}
	if got := NormalizePresetToken("moa:My Review!"); got != "my-review" {
		t.Fatalf("token: %q", got)
	}
}

func TestCountUsableRefs(t *testing.T) {
	refs := []ResolvedRef{
		{Config: corelib.MaclawLLMConfig{URL: "http://a", Model: "m"}},
		{Config: corelib.MaclawLLMConfig{URL: "http://b", Model: "m", ProviderName: "error:x"}},
		{Config: corelib.MaclawLLMConfig{URL: "", Model: "m"}},
		{Config: corelib.MaclawLLMConfig{URL: "http://c", Model: "n"}},
	}
	if n := CountUsableRefs(refs); n != 2 {
		t.Fatalf("usable=%d", n)
	}
}
