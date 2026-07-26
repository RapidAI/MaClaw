package moa

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestShouldFanOutDefaults(t *testing.T) {
	cfg := corelib.MoAConfig{} // fanout default 1, only_before_first_tool true
	preset := corelib.MoAPresetConfig{Enabled: true}

	d := ShouldFanOut(cfg, preset, 0, 0, false)
	if !d.Allow {
		t.Fatalf("first fan-out should allow: %v", d)
	}
	d = ShouldFanOut(cfg, preset, 1, 1, false)
	if d.Allow {
		t.Fatalf("budget exhausted should deny: %v", d)
	}
	d = ShouldFanOut(cfg, preset, 0, 0, true)
	if d.Allow {
		t.Fatalf("tools seen should deny: %v", d)
	}
	falseV := false
	cfg.OnlyBeforeFirstTool = &falseV
	d = ShouldFanOut(cfg, preset, 0, 0, true)
	if !d.Allow {
		t.Fatalf("only_before=false should allow with tools: %v", d)
	}
}

func TestResolvePresetUsePrimaryAndProvider(t *testing.T) {
	primary := corelib.MaclawLLMConfig{URL: "http://p", Key: "pk", Model: "big"}
	lookup := func(name string) (corelib.MaclawLLMConfig, error) {
		if name == "OpenAI" {
			return corelib.MaclawLLMConfig{URL: "http://o", Key: "ok", Model: "gpt", WireAPI: "chat"}, nil
		}
		return corelib.MaclawLLMConfig{}, errNotFound(name)
	}
	in := ResolveInput{
		AppMoA: corelib.MoAConfig{
			Enabled:       true,
			DefaultPreset: "review",
			Presets: map[string]corelib.MoAPresetConfig{
				"review": {
					Enabled:    true,
					Aggregator: corelib.MoAModelRef{UsePrimary: true},
					ReferenceModels: []corelib.MoAModelRef{
						{Provider: "OpenAI"},
					},
					ReferenceMaxTokens: 600,
				},
			},
		},
		Primary:    primary,
		Lookup:     lookup,
		PresetName: "review",
	}
	got, err := ResolvePreset(in)
	if err != nil {
		t.Fatal(err)
	}
	if !got.AggregatorUsePrimary || got.Aggregator.Model != "big" {
		t.Fatalf("agg %#v", got.Aggregator)
	}
	if len(got.References) != 1 || got.References[0].Config.Model != "gpt" {
		t.Fatalf("refs %#v", got.References)
	}
	if got.References[0].Config.MaxOutputTokens != 600 {
		t.Fatalf("max tokens %d", got.References[0].Config.MaxOutputTokens)
	}
}

func TestResolvePresetUseAuxPreservesGlobalReasoningControls(t *testing.T) {
	in := ResolveInput{
		AppMoA: corelib.MoAConfig{
			DefaultPreset: "review",
			Presets: map[string]corelib.MoAPresetConfig{
				"review": {
					Enabled:         true,
					Aggregator:      corelib.MoAModelRef{UsePrimary: true},
					ReferenceModels: []corelib.MoAModelRef{{UseAux: true}},
				},
			},
		},
		Primary: corelib.MaclawLLMConfig{URL: "http://primary", Key: "pk", Model: "primary", ThinkingMode: "disabled", ReasoningEffort: "minimal"},
		Aux:     corelib.AuxiliaryLLMConfig{URL: "http://aux", Key: "ak", Model: "aux"},
	}
	got, err := ResolvePreset(in)
	if err != nil {
		t.Fatal(err)
	}
	ref := got.References[0].Config
	if ref.ThinkingMode != "disabled" || ref.ReasoningEffort != "minimal" {
		t.Fatalf("auxiliary MoA config lost reasoning controls: %+v", ref)
	}
}

func errNotFound(name string) error {
	return &resolveError{name: name}
}

type resolveError struct{ name string }

func (e *resolveError) Error() string { return "not found: " + e.name }
