package guiapp

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// maclawLLMRoleGrounder is the optional Computer Use grounding model (see
// corelib.MaclawLLMProfiles.Grounder and corelib/grounding).
const maclawLLMRoleGrounder = "grounder"

// errGrounderNotConfigured means the user has not pinned a grounding model;
// computer_click then keeps the single-model behaviour.
var errGrounderNotConfigured = fmt.Errorf("no grounding model is configured")

// ResolveMaclawLLMGrounder returns the pinned grounding model. Unlike the loop
// roles it never follows Hub or the assistant: unset means not used.
func (a *App) ResolveMaclawLLMGrounder() (corelib.MaclawLLMConfig, error) {
	if a == nil {
		return corelib.MaclawLLMConfig{}, errGrounderNotConfigured
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return corelib.MaclawLLMConfig{}, err
	}
	state := a.maclawLLMProvidersFromConfig(&cfg)
	profiles, err := effectiveMaclawLLMProfiles(cfg, state.Providers, state.Current)
	if err != nil || profiles == nil {
		return corelib.MaclawLLMConfig{}, errGrounderNotConfigured
	}
	return a.resolveMaclawLLMGrounderFrom(*profiles, state.Providers)
}

func (a *App) resolveMaclawLLMGrounderFrom(profiles corelib.MaclawLLMProfiles, providers []corelib.MaclawLLMProvider) (corelib.MaclawLLMConfig, error) {
	model := normalizeDesktopRoleModel(profiles.Grounder.Model)
	if profiles.Grounder.InheritsHub() || model == "" {
		return corelib.MaclawLLMConfig{}, errGrounderNotConfigured
	}
	resolved, err := a.materializePinnedRole(maclawLLMRoleGrounder, model, providers, &profiles)
	if err != nil {
		return corelib.MaclawLLMConfig{}, err
	}
	// The grounder always receives the screenshot.
	resolved.SupportsVision = true
	return resolved, nil
}

func (a *App) maclawLLMGrounderSummary(profiles corelib.MaclawLLMProfiles, providers []corelib.MaclawLLMProvider) MaclawLLMRoleSummary {
	summary := MaclawLLMRoleSummary{Role: maclawLLMRoleGrounder, InheritHub: profiles.Grounder.InheritsHub()}
	resolved, err := a.resolveMaclawLLMGrounderFrom(profiles, providers)
	if err != nil || strings.TrimSpace(resolved.Model) == "" {
		return summary
	}
	summary.Model = resolved.Model
	summary.ProviderID = resolved.ProviderID
	summary.ProviderName = resolved.ProviderName
	summary.SupportsVision = true
	summary.Configured = true
	return summary
}
