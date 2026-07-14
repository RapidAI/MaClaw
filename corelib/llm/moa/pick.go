package moa

import (
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// NormalizePresetToken strips optional moa: prefix then applies config key rules
// (corelib.NormalizeMoAPresetName). Use for slash @name, sticky, metadata.
func NormalizePresetToken(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	name = strings.TrimPrefix(name, "moa:")
	return corelib.NormalizeMoAPresetName(name)
}

// PickPresetName chooses a preset key from config.
// Prefer requested (after NormalizePresetToken), then DefaultPreset, then sorted first key.
// Returns "" when no presets exist. If requested is non-empty but missing, returns the
// normalized token so callers can emit "not found".
func PickPresetName(cfg corelib.MoAConfig, requested string) string {
	cfg = corelib.NormalizeMoAConfig(cfg)
	if len(cfg.Presets) == 0 {
		return ""
	}
	// Explicit request must not fall through to default (invalid tokens, typos).
	if req := strings.TrimSpace(requested); req != "" {
		name := NormalizePresetToken(req)
		if name == "" {
			// Non-empty request that normalizes to nothing (e.g. "!!!") — force miss.
			return "!"
		}
		return name
	}
	if cfg.DefaultPreset != "" {
		if _, ok := cfg.Presets[cfg.DefaultPreset]; ok {
			return cfg.DefaultPreset
		}
	}
	ids := make([]string, 0, len(cfg.Presets))
	for k := range cfg.Presets {
		ids = append(ids, k)
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

// CountUsableRefs counts references that have URL+model and are not error placeholders.
func CountUsableRefs(refs []ResolvedRef) int {
	n := 0
	for _, r := range refs {
		if strings.TrimSpace(r.Config.URL) == "" || strings.TrimSpace(r.Config.Model) == "" {
			continue
		}
		if strings.HasPrefix(r.Config.ProviderName, "error:") {
			continue
		}
		n++
	}
	return n
}
