package guiapp

import (
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/longhorizon"
)

// horizonRoleTimeout is the per-role completion timeout for Manager/Auditor
// LLM calls (P5-c). It replaces the previously hardcoded 2-minute budget so
// the two control roles stay shorter than any executor episode.
func horizonRoleTimeout(role string) time.Duration {
	return time.Duration(longhorizon.RoleTimeoutS(role)) * time.Second
}

// horizonEpisodeTimeout resolves the executor episode deadline: an explicit
// EpisodeBudget.MaxDurationS wins, otherwise the role default applies.
func horizonEpisodeTimeout(ep longhorizon.EpisodeContext) time.Duration {
	if ep.Budget.MaxDurationS > 0 {
		return time.Duration(ep.Budget.MaxDurationS) * time.Second
	}
	return horizonRoleTimeout(ep.Role)
}

// horizonProfileForRole maps a horizon role onto its optional model override
// in the persisted model assignment (P5-b). Manager and the three auditors
// are meant for a strong model, executors for a cheaper one. Unassigned or
// partially assigned overrides are ignored.
func horizonProfileForRole(profiles *corelib.MaclawLLMProfiles, role string) (corelib.MaclawLLMProfile, bool) {
	if profiles == nil {
		return corelib.MaclawLLMProfile{}, false
	}
	var profile corelib.MaclawLLMProfile
	switch {
	case role == longhorizon.RoleManager:
		profile = profiles.Horizon.Manager
	case longhorizon.IsAuditorRole(role):
		profile = profiles.Horizon.Auditor
	default:
		profile = profiles.Horizon.Executor
	}
	if !maclawLLMProfileAssigned(profile) {
		return corelib.MaclawLLMProfile{}, false
	}
	return profile, true
}

// horizonModelForRole resolves the effective LLM config for one horizon role
// (P5-b). Without a configured override, or when the override cannot be
// resolved against the current providers, the base coding config wins so the
// supervisor never fails closed on an incomplete assignment.
func (h *IMMessageHandler) horizonModelForRole(role string, baseCfg corelib.MaclawLLMConfig) corelib.MaclawLLMConfig {
	if h == nil || h.app == nil {
		return baseCfg
	}
	cfg, err := h.app.LoadConfig()
	if err != nil || cfg.MaclawLLMProfiles == nil {
		return baseCfg
	}
	profile, ok := horizonProfileForRole(cfg.MaclawLLMProfiles, role)
	if !ok {
		return baseCfg
	}
	state := h.app.GetMaclawLLMProviders()
	provider, ok := resolveMaclawLLMProviderByID(state.Providers, profile.ProviderID)
	if !ok {
		return baseCfg
	}
	model := constrainOpenCodeModel(provider, corelib.MigrateZhipuCodingModel(provider.Name, profile.Model))
	if model == "" {
		return baseCfg
	}
	resolved := h.app.materializeMaclawLLMProvider(provider)
	resolved.Model = model
	resolved.SupportsVision = providerSupportsVisionForModel(provider, model)
	// Keep token usage attributed to the base execution profile; RouteSource
	// records that a horizon role override picked the endpoint.
	resolved.Profile = baseCfg.Profile
	resolved.RouteSource = "horizon:" + strings.TrimSpace(role)
	return resolved
}
