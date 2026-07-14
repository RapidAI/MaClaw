package corelib

import (
	"fmt"
	"strings"
)

// MoAConfig is Mixture-of-Agents (multi-model council) configuration.
// See docs/moa-mixture-of-agents-design-zh.md.
type MoAConfig struct {
	Enabled             bool                      `json:"enabled"`
	DefaultPreset       string                    `json:"default_preset,omitempty"`
	AllowAuto           bool                      `json:"allow_auto,omitempty"`
	MaxReferences       int                       `json:"max_references,omitempty"`
	ReferenceTimeoutSec int                       `json:"reference_timeout_sec,omitempty"`
	FanoutMaxIterations int                       `json:"fanout_max_iterations,omitempty"`
	OnlyBeforeFirstTool *bool                     `json:"only_before_first_tool,omitempty"`
	Presets             map[string]MoAPresetConfig `json:"presets,omitempty"`
}

// MoAPresetConfig is one named council preset (references + aggregator).
type MoAPresetConfig struct {
	Enabled             bool          `json:"enabled"`
	ReferenceModels     []MoAModelRef `json:"reference_models,omitempty"`
	Aggregator          MoAModelRef   `json:"aggregator"`
	ReferenceMaxTokens  int           `json:"reference_max_tokens,omitempty"`
	AggregatorMaxTokens int           `json:"max_tokens,omitempty"`
	FanoutMaxIterations int           `json:"fanout_max_iterations,omitempty"`
	OnlyBeforeFirstTool *bool         `json:"only_before_first_tool,omitempty"`
	DisplayName         string        `json:"display_name,omitempty"`
}

// MoAModelRef resolves to a MaclawLLMConfig via provider name / task route / primary / aux.
// Prefer Provider, TaskRoute, UsePrimary, or UseAux. Inline Key is discouraged.
type MoAModelRef struct {
	Provider   string `json:"provider,omitempty"`
	TaskRoute  string `json:"task_route,omitempty"`
	Model      string `json:"model,omitempty"`
	URL        string `json:"url,omitempty"`
	Key        string `json:"key,omitempty"`
	Protocol   string `json:"protocol,omitempty"`
	WireAPI    string `json:"wire_api,omitempty"`
	UsePrimary bool   `json:"use_primary,omitempty"`
	UseAux     bool   `json:"use_aux,omitempty"`
}

// EffectiveMaxReferences returns the global hard cap (default 3).
func (c MoAConfig) EffectiveMaxReferences() int {
	if c.MaxReferences > 0 {
		return c.MaxReferences
	}
	return 3
}

// EffectiveReferenceTimeoutSec returns per-reference timeout (default 45).
func (c MoAConfig) EffectiveReferenceTimeoutSec() int {
	if c.ReferenceTimeoutSec > 0 {
		return c.ReferenceTimeoutSec
	}
	return 45
}

// EffectiveFanoutMaxIterations returns how many main-loop iterations may fan out (default 1).
func (c MoAConfig) EffectiveFanoutMaxIterations() int {
	if c.FanoutMaxIterations > 0 {
		return c.FanoutMaxIterations
	}
	return 1
}

// EffectiveOnlyBeforeFirstTool defaults to true when unset.
func (c MoAConfig) EffectiveOnlyBeforeFirstTool() bool {
	if c.OnlyBeforeFirstTool == nil {
		return true
	}
	return *c.OnlyBeforeFirstTool
}

// NormalizeMoAConfig cleans names, caps, and drops empty presets. Does not validate providers exist.
func NormalizeMoAConfig(in MoAConfig) MoAConfig {
	out := in
	out.DefaultPreset = normalizeMoAPresetName(out.DefaultPreset)
	if out.MaxReferences < 0 {
		out.MaxReferences = 0
	}
	if out.MaxReferences > 8 {
		out.MaxReferences = 8
	}
	if out.ReferenceTimeoutSec < 0 {
		out.ReferenceTimeoutSec = 0
	}
	if out.ReferenceTimeoutSec > 300 {
		out.ReferenceTimeoutSec = 300
	}
	if out.FanoutMaxIterations < 0 {
		out.FanoutMaxIterations = 0
	}
	if out.FanoutMaxIterations > 20 {
		out.FanoutMaxIterations = 20
	}
	if len(in.Presets) == 0 {
		out.Presets = nil
		return out
	}
	presets := make(map[string]MoAPresetConfig, len(in.Presets))
	for name, p := range in.Presets {
		key := normalizeMoAPresetName(name)
		if key == "" {
			continue
		}
		p.DisplayName = strings.TrimSpace(p.DisplayName)
		p.Aggregator = normalizeMoAModelRef(p.Aggregator)
		refs := make([]MoAModelRef, 0, len(p.ReferenceModels))
		for _, r := range p.ReferenceModels {
			r = normalizeMoAModelRef(r)
			if moaModelRefEmpty(r) {
				continue
			}
			refs = append(refs, r)
		}
		p.ReferenceModels = refs
		if p.ReferenceMaxTokens < 0 {
			p.ReferenceMaxTokens = 0
		}
		if p.AggregatorMaxTokens < 0 {
			p.AggregatorMaxTokens = 0
		}
		if p.FanoutMaxIterations < 0 {
			p.FanoutMaxIterations = 0
		}
		presets[key] = p
	}
	out.Presets = presets
	if out.DefaultPreset != "" {
		if _, ok := out.Presets[out.DefaultPreset]; !ok {
			out.DefaultPreset = ""
		}
	}
	return out
}

// ValidateMoAConfig returns a user-facing error if the config is inconsistent.
// knownProviders is the set of MaclawLLMProviders names (optional; empty skips existence checks).
func ValidateMoAConfig(cfg MoAConfig, knownProviders map[string]struct{}) error {
	cfg = NormalizeMoAConfig(cfg)
	if !cfg.Enabled && len(cfg.Presets) == 0 {
		return nil
	}
	if cfg.Enabled && len(cfg.Presets) == 0 {
		return fmt.Errorf("moa: enabled but no presets configured")
	}
	if cfg.DefaultPreset != "" {
		if _, ok := cfg.Presets[cfg.DefaultPreset]; !ok {
			return fmt.Errorf("moa: default_preset %q not found", cfg.DefaultPreset)
		}
	}
	for name, p := range cfg.Presets {
		if err := validateMoAModelRef("aggregator", p.Aggregator, knownProviders); err != nil {
			return fmt.Errorf("moa preset %q: %w", name, err)
		}
		if moaModelRefEmpty(p.Aggregator) {
			return fmt.Errorf("moa preset %q: aggregator is required (use current primary or a provider)", name)
		}
		if p.Aggregator.UsePrimary && p.Aggregator.UseAux {
			return fmt.Errorf("moa preset %q: aggregator cannot use both primary and aux", name)
		}
		if p.Aggregator.UsePrimary && (p.Aggregator.Provider != "" || p.Aggregator.TaskRoute != "") {
			return fmt.Errorf("moa preset %q: aggregator use_primary cannot combine with provider/task_route", name)
		}
		maxRefs := cfg.EffectiveMaxReferences()
		if len(p.ReferenceModels) > maxRefs {
			return fmt.Errorf("moa preset %q: too many reference models (%d > max %d)", name, len(p.ReferenceModels), maxRefs)
		}
		for i, r := range p.ReferenceModels {
			if r.UsePrimary && r.UseAux {
				return fmt.Errorf("moa preset %q reference[%d]: cannot use both primary and aux", name, i)
			}
			if r.UsePrimary && (r.Provider != "" || r.TaskRoute != "") {
				return fmt.Errorf("moa preset %q reference[%d]: use_primary cannot combine with provider/task_route", name, i)
			}
			if err := validateMoAModelRef(fmt.Sprintf("reference[%d]", i), r, knownProviders); err != nil {
				return fmt.Errorf("moa preset %q: %w", name, err)
			}
			// Recursive MoA blocked (virtual ids)
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(r.Model)), "moa:") {
				return fmt.Errorf("moa preset %q reference[%d]: cannot nest moa: models", name, i)
			}
		}
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(p.Aggregator.Model)), "moa:") {
			return fmt.Errorf("moa preset %q: aggregator cannot be another moa preset", name)
		}
	}
	return nil
}

// NormalizeMoAPresetName canonicalizes a preset id the same way config keys are stored
// (lower, spaces→-, strip non [a-z0-9_-]). Callers that accept user input (slash @name,
// metadata) must use this before map lookup.
func NormalizeMoAPresetName(name string) string {
	return normalizeMoAPresetName(name)
}

func normalizeMoAPresetName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	name = strings.ReplaceAll(name, " ", "-")
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func normalizeMoAModelRef(r MoAModelRef) MoAModelRef {
	r.Provider = strings.TrimSpace(r.Provider)
	r.TaskRoute = strings.TrimSpace(strings.ToLower(r.TaskRoute))
	r.Model = strings.TrimSpace(r.Model)
	r.URL = strings.TrimSpace(r.URL)
	r.Key = strings.TrimSpace(r.Key)
	r.Protocol = strings.TrimSpace(strings.ToLower(r.Protocol))
	r.WireAPI = strings.TrimSpace(strings.ToLower(r.WireAPI))
	return r
}

func moaModelRefEmpty(r MoAModelRef) bool {
	return !r.UsePrimary && !r.UseAux && r.Provider == "" && r.TaskRoute == "" && r.Model == "" && r.URL == ""
}

func validateMoAModelRef(role string, r MoAModelRef, knownProviders map[string]struct{}) error {
	r = normalizeMoAModelRef(r)
	if moaModelRefEmpty(r) {
		return nil
	}
	bases := 0
	if r.UsePrimary {
		bases++
	}
	if r.UseAux {
		bases++
	}
	if r.Provider != "" {
		bases++
	}
	if r.TaskRoute != "" && !r.UsePrimary && !r.UseAux && r.Provider == "" {
		// task_route alone is a valid base (overlays primary)
		bases++
	}
	if bases == 0 && (r.Model != "" || r.URL != "") {
		// bare model/url overlay without base — allowed as primary overlay
		return nil
	}
	if r.Provider != "" && knownProviders != nil && len(knownProviders) > 0 {
		if _, ok := knownProviders[r.Provider]; !ok {
			// case-insensitive match
			found := false
			for name := range knownProviders {
				if strings.EqualFold(name, r.Provider) {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("%s: unknown provider %q", role, r.Provider)
			}
		}
	}
	if r.Key != "" {
		// Soft policy: allow but callers may warn (K18). Not a hard error.
	}
	return nil
}
