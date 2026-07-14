package moa

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// ProviderLookup materializes a named provider to a full MaclawLLMConfig.
// Host (GUI) must inject OAuth/CredentialStore-aware implementation.
type ProviderLookup func(providerName string) (corelib.MaclawLLMConfig, error)

// ResolveInput is the pure resolve input (no CredentialStore).
type ResolveInput struct {
	AppMoA     corelib.MoAConfig
	Primary    corelib.MaclawLLMConfig
	Aux        corelib.AuxiliaryLLMConfig
	Router     *llm.ModelRouter
	Lookup     ProviderLookup
	PresetName string
}

// ResolvedRef is one advisor endpoint.
type ResolvedRef struct {
	Label  string
	Config corelib.MaclawLLMConfig
}

// ResolvedPreset is ready for Runner / loop.
type ResolvedPreset struct {
	Name                string
	DisplayName         string
	Enabled             bool
	References          []ResolvedRef
	Aggregator          corelib.MaclawLLMConfig
	AggregatorUsePrimary bool
	ReferenceTimeoutSec int
	FanoutMaxIterations int
	OnlyBeforeFirstTool bool
	Raw                 corelib.MoAPresetConfig
}

// ResolvePreset resolves a named preset. Returns error if invalid.
func ResolvePreset(in ResolveInput) (ResolvedPreset, error) {
	cfg := corelib.NormalizeMoAConfig(in.AppMoA)
	name := NormalizePresetToken(in.PresetName)
	if name == "" {
		name = NormalizePresetToken(cfg.DefaultPreset)
	}
	if name == "" {
		return ResolvedPreset{}, fmt.Errorf("moa: no preset name")
	}
	preset, ok := cfg.Presets[name]
	if !ok {
		return ResolvedPreset{}, fmt.Errorf("moa: preset %q not found", name)
	}

	maxRefs := cfg.EffectiveMaxReferences()
	refsIn := preset.ReferenceModels
	if len(refsIn) > maxRefs {
		refsIn = refsIn[:maxRefs]
	}

	out := ResolvedPreset{
		Name:                name,
		DisplayName:         strings.TrimSpace(preset.DisplayName),
		Enabled:             preset.Enabled,
		ReferenceTimeoutSec: cfg.EffectiveReferenceTimeoutSec(),
		FanoutMaxIterations: cfg.EffectiveFanoutMaxIterations(),
		OnlyBeforeFirstTool: cfg.EffectiveOnlyBeforeFirstTool(),
		Raw:                 preset,
	}
	if preset.FanoutMaxIterations > 0 {
		out.FanoutMaxIterations = preset.FanoutMaxIterations
	}
	if preset.OnlyBeforeFirstTool != nil {
		out.OnlyBeforeFirstTool = *preset.OnlyBeforeFirstTool
	}

	agg, usePrimary, err := resolveModelRef(in, preset.Aggregator, "agg")
	if err != nil {
		return ResolvedPreset{}, fmt.Errorf("moa preset %q aggregator: %w", name, err)
	}
	if preset.AggregatorMaxTokens > 0 {
		agg.MaxOutputTokens = preset.AggregatorMaxTokens
	}
	out.Aggregator = agg
	out.AggregatorUsePrimary = usePrimary

	out.References = make([]ResolvedRef, 0, len(refsIn))
	for i, r := range refsIn {
		label := refLabel(r, i)
		rcfg, _, err := resolveModelRef(in, r, "ref")
		if err != nil {
			// Soft-fail: keep a placeholder so partial council still runs (K8).
			out.References = append(out.References, ResolvedRef{
				Label: label,
				Config: corelib.MaclawLLMConfig{
					ProviderName: "error:" + sanitizeError(err.Error()),
				},
			})
			continue
		}
		if preset.ReferenceMaxTokens > 0 {
			rcfg.MaxOutputTokens = preset.ReferenceMaxTokens
		}
		// Reject responses-ws for references (K: Responses-WS as ref).
		if rcfg.IsResponsesWebSocket() {
			out.References = append(out.References, ResolvedRef{
				Label: label,
				Config: corelib.MaclawLLMConfig{
					ProviderName: "error:responses-ws not supported for MoA reference",
				},
			})
			continue
		}
		out.References = append(out.References, ResolvedRef{
			Label:  label,
			Config: rcfg,
		})
	}
	return out, nil
}

func refLabel(r corelib.MoAModelRef, i int) string {
	if r.Provider != "" {
		if r.Model != "" {
			return r.Provider + "/" + r.Model
		}
		return r.Provider
	}
	if r.TaskRoute != "" {
		return "route:" + r.TaskRoute
	}
	if r.UseAux {
		return "aux"
	}
	if r.UsePrimary {
		return "primary"
	}
	return fmt.Sprintf("advisor-%d", i+1)
}

func resolveModelRef(in ResolveInput, r corelib.MoAModelRef, role string) (corelib.MaclawLLMConfig, bool, error) {
	r.Provider = strings.TrimSpace(r.Provider)
	r.TaskRoute = strings.TrimSpace(strings.ToLower(r.TaskRoute))
	r.Model = strings.TrimSpace(r.Model)
	r.URL = strings.TrimSpace(r.URL)
	r.Key = strings.TrimSpace(r.Key)
	r.Protocol = strings.TrimSpace(strings.ToLower(r.Protocol))
	r.WireAPI = strings.TrimSpace(strings.ToLower(r.WireAPI))

	if r.UsePrimary && r.UseAux {
		return corelib.MaclawLLMConfig{}, false, fmt.Errorf("use_primary and use_aux are mutually exclusive")
	}
	if r.UsePrimary && (r.Provider != "" || r.TaskRoute != "") {
		return corelib.MaclawLLMConfig{}, false, fmt.Errorf("use_primary cannot combine with provider/task_route")
	}
	if strings.HasPrefix(strings.ToLower(r.Model), "moa:") {
		return corelib.MaclawLLMConfig{}, false, fmt.Errorf("cannot nest moa: models")
	}

	var base corelib.MaclawLLMConfig
	usePrimary := false
	switch {
	case r.UsePrimary:
		base = in.Primary
		usePrimary = true
	case r.UseAux:
		if !in.Aux.IsConfigured() {
			return corelib.MaclawLLMConfig{}, false, fmt.Errorf("auxiliary LLM not configured")
		}
		base = corelib.MaclawLLMConfig{
			URL:      in.Aux.URL,
			Key:      in.Aux.Key,
			Model:    in.Aux.Model,
			Protocol: in.Aux.Protocol,
		}
	case r.Provider != "":
		if in.Lookup == nil {
			return corelib.MaclawLLMConfig{}, false, fmt.Errorf("provider_lookup_required")
		}
		cfg, err := in.Lookup(r.Provider)
		if err != nil {
			return corelib.MaclawLLMConfig{}, false, err
		}
		base = cfg
	default:
		base = in.Primary
	}

	if r.TaskRoute != "" && in.Router != nil {
		base = in.Router.Route(llm.TaskType(r.TaskRoute), base)
	}
	if r.Model != "" {
		base.Model = r.Model
	}
	if r.URL != "" {
		base.URL = r.URL
	}
	if r.Key != "" {
		base.Key = r.Key
	}
	if r.Protocol != "" {
		base.Protocol = r.Protocol
	}
	if r.WireAPI != "" {
		base.WireAPI = r.WireAPI
	}

	if role == "agg" {
		if strings.TrimSpace(base.URL) == "" || strings.TrimSpace(base.Model) == "" {
			return corelib.MaclawLLMConfig{}, false, fmt.Errorf("aggregator missing url/model")
		}
	}
	return base, usePrimary, nil
}
