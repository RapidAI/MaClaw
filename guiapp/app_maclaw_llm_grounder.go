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
	if choice, ok := a.desktopBotLLMChoice(a.configuredRemoteUserID()); ok {
		return a.resolveGrounderFromChoice(choice)
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

func (a *App) resolveGrounderFromChoice(choice DesktopBotLLM) (corelib.MaclawLLMConfig, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return corelib.MaclawLLMConfig{}, err
	}
	state := a.maclawLLMProvidersFromConfig(&cfg)
	mode := strings.TrimSpace(choice.Choice)
	if mode == "" {
		mode = strings.TrimSpace(choice.Mode)
	}
	try := func(spec DesktopBotLLMRole, ok bool) (corelib.MaclawLLMConfig, bool) {
		if !ok || spec.Unused || spec.Inherit || strings.TrimSpace(spec.Model) == "" {
			return corelib.MaclawLLMConfig{}, false
		}
		resolved, matErr := a.materializeDesktopHubRole(maclawLLMRoleGrounder, spec, state.Providers)
		if matErr != nil {
			return corelib.MaclawLLMConfig{}, false
		}
		resolved.SupportsVision = true
		return resolved, true
	}
	if mode == "custom" {
		if spec, explicit := explicitDesktopBotRole(choice, maclawLLMRoleGrounder); explicit {
			resolved, matErr := a.materializeDesktopHubRole(maclawLLMRoleGrounder, spec, state.Providers)
			if matErr != nil {
				return corelib.MaclawLLMConfig{}, matErr
			}
			resolved.SupportsVision = true
			return resolved, nil
		}
	}
	if resolved, ok := try(effectiveDesktopBotRole(choice, maclawLLMRoleGrounder)); ok {
		return resolved, nil
	}
	return corelib.MaclawLLMConfig{}, errGrounderNotConfigured
}

func (a *App) resolveMaclawLLMGrounderFrom(profiles corelib.MaclawLLMProfiles, providers []corelib.MaclawLLMProvider) (corelib.MaclawLLMConfig, error) {
	model := normalizeDesktopRoleModel(profiles.Grounder.Model)
	if profiles.Grounder.InheritsHub() || model == "" {
		return corelib.MaclawLLMConfig{}, errGrounderNotConfigured
	}
	resolved, err := a.materializePinnedRole(maclawLLMRoleGrounder, profiles.Grounder, providers, &profiles)
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
