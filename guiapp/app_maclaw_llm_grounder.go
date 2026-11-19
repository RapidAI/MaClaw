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
// roles it never follows Hub or the assistant: unset means not used. A cached
// bot choice replaces a local pin. The settings summary uses the same decision.
func (a *App) ResolveMaclawLLMGrounder() (corelib.MaclawLLMConfig, error) {
	if a == nil {
		return corelib.MaclawLLMConfig{}, errGrounderNotConfigured
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return corelib.MaclawLLMConfig{}, err
	}
	state := a.maclawLLMProvidersFromConfig(&cfg)
	return a.resolveGrounderRole(cfg, state.Providers, state.Current)
}

// resolveGrounderRole is the decision the click path and the settings summary
// share. The bot view comes from the config already loaded. A cold read does
// not load config again, so the provider rows stay the ones beside that view.
func (a *App) resolveGrounderRole(cfg corelib.AppConfig, providers []corelib.MaclawLLMProvider, current string) (corelib.MaclawLLMConfig, error) {
	if choice, ok := a.desktopBotLLMChoiceFromConfig(strings.TrimSpace(cfg.RemoteUserID), cfg); ok {
		return a.resolveGrounderFromChoice(choice, providers)
	}
	profiles, err := effectiveMaclawLLMProfiles(cfg, providers, current)
	if err != nil || profiles == nil {
		return corelib.MaclawLLMConfig{}, errGrounderNotConfigured
	}
	return a.resolveMaclawLLMGrounderFrom(*profiles, providers)
}

func (a *App) resolveGrounderFromChoice(choice DesktopBotLLM, providers []corelib.MaclawLLMProvider) (corelib.MaclawLLMConfig, error) {
	mode := strings.TrimSpace(choice.Choice)
	if mode == "" {
		mode = strings.TrimSpace(choice.Mode)
	}
	try := func(spec DesktopBotLLMRole, ok bool) (corelib.MaclawLLMConfig, bool) {
		if !ok || spec.Unused || spec.Inherit || strings.TrimSpace(spec.Model) == "" {
			return corelib.MaclawLLMConfig{}, false
		}
		resolved, matErr := a.materializeDesktopHubRole(maclawLLMRoleGrounder, spec, providers)
		if matErr != nil {
			return corelib.MaclawLLMConfig{}, false
		}
		resolved.SupportsVision = true
		return resolved, true
	}
	if mode == "custom" {
		if spec, explicit := explicitDesktopBotRole(choice, maclawLLMRoleGrounder); explicit {
			resolved, matErr := a.materializeDesktopHubRole(maclawLLMRoleGrounder, spec, providers)
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

func (a *App) maclawLLMGrounderSummary(cfg corelib.AppConfig, profiles corelib.MaclawLLMProfiles, providers []corelib.MaclawLLMProvider, current string) MaclawLLMRoleSummary {
	summary := MaclawLLMRoleSummary{Role: maclawLLMRoleGrounder, InheritHub: profiles.Grounder.InheritsHub()}
	resolved, err := a.resolveGrounderRole(cfg, providers, current)
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
