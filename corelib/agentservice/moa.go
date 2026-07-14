package agentservice

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/llm/moa"
)

// materializeProviderByName maps a configured provider name to MaclawLLMConfig
// for MoA reference/aggregator resolution (no OAuth JWT refresh — K19 host path
// simplified for REST agentservice; uses stored key/oauth access token).
func materializeProviderByName(cfg corelib.AppConfig, name string) (corelib.MaclawLLMConfig, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return corelib.MaclawLLMConfig{}, fmt.Errorf("provider required")
	}
	for _, p := range cfg.MaclawLLMProviders {
		if !strings.EqualFold(strings.TrimSpace(p.Name), name) {
			continue
		}
		p = corelib.NormalizeCodeGenSSOProvider(p)
		key := resolveProviderSecret(p)
		protocol := strings.TrimSpace(p.Protocol)
		if protocol == "" && corelib.IsAnthropicWireAPI(p.WireAPI) {
			protocol = "anthropic"
		}
		return corelib.MaclawLLMConfig{
			URL:             strings.TrimSpace(p.URL),
			Key:             key,
			Model:           strings.TrimSpace(p.Model),
			Protocol:        protocol,
			ContextLength:   p.ContextLength,
			TimeoutSec:      p.TimeoutSec,
			MaxOutputTokens: p.MaxOutputTokens,
			SupportsVision:  p.SupportsVision,
			AgentType:       p.AgentType,
			WireAPI:         strings.TrimSpace(p.WireAPI),
			ProviderName:    strings.TrimSpace(p.Name),
			AuthType:        p.AuthType,
		}, nil
	}
	return corelib.MaclawLLMConfig{}, fmt.Errorf("provider %q not found", name)
}

// resolveMoAPresetForRequest resolves a named (or default) MoA preset for agentservice.
// ok=false when MoA cannot run (env/config/refs).
func resolveMoAPresetForRequest(appCfg corelib.AppConfig, primary corelib.MaclawLLMConfig, presetName string) (moa.ResolvedPreset, string, bool) {
	if !moa.EnvAllows() {
		return moa.ResolvedPreset{}, "MACLAW_MOA=off (kill switch)", false
	}
	moaCfg := corelib.NormalizeMoAConfig(appCfg.MoA)
	if !moa.EffectiveEnabled(moaCfg.Enabled) {
		return moa.ResolvedPreset{}, "enable moa in user/app config", false
	}
	if len(moaCfg.Presets) == 0 {
		return moa.ResolvedPreset{}, "no moa presets configured", false
	}
	if strings.TrimSpace(primary.URL) == "" || strings.TrimSpace(primary.Model) == "" {
		return moa.ResolvedPreset{}, "primary llm not configured", false
	}
	name := moa.PickPresetName(moaCfg, presetName)
	if name == "" {
		return moa.ResolvedPreset{}, "no moa presets configured", false
	}
	if _, ok := moaCfg.Presets[name]; !ok {
		return moa.ResolvedPreset{}, fmt.Sprintf("preset %q not found", name), false
	}
	router := llm.NewModelRouter(nil)
	if len(appCfg.ModelRoutes) > 0 {
		routes := make(map[string]llm.ModelRoute, len(appCfg.ModelRoutes))
		for k, v := range appCfg.ModelRoutes {
			routes[k] = llm.ModelRoute{Model: v.Model, URL: v.URL, Key: v.Key, Protocol: v.Protocol, Provider: v.Provider}
		}
		router = llm.NewModelRouter(routes)
	}
	resolved, err := moa.ResolvePreset(moa.ResolveInput{
		AppMoA:     moaCfg,
		Primary:    primary,
		Aux:        appCfg.AuxiliaryLLM,
		Router:     router,
		Lookup:     func(n string) (corelib.MaclawLLMConfig, error) { return materializeProviderByName(appCfg, n) },
		PresetName: name,
	})
	if err != nil {
		return moa.ResolvedPreset{}, err.Error(), false
	}
	if moa.CountUsableRefs(resolved.References) == 0 {
		return moa.ResolvedPreset{}, "no usable reference models", false
	}
	return resolved, "", true
}

// moaPresetFromMetadata reads request-level moa_preset from message/session metadata.
func moaPresetFromMetadata(meta ...map[string]string) string {
	for _, m := range meta {
		if m == nil {
			continue
		}
		for _, key := range []string{"moa_preset", "moaPreset", "moa"} {
			if v := strings.TrimSpace(m[key]); v != "" {
				return moa.NormalizePresetToken(v)
			}
		}
	}
	return ""
}
