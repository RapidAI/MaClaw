package guiapp

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

const (
	maclawLLMRoleForeground  = "foreground"
	maclawLLMRoleBackground  = "background"
	maclawLLMRoleComputerUse = "computer_use"
)

// MaclawLLMRoleSummary is the effective loop role shown in model settings.
// It carries no URL or credential.
type MaclawLLMRoleSummary struct {
	Role           string `json:"role"`
	Model          string `json:"model,omitempty"`
	InheritHub     bool   `json:"inherit_hub"`
	ProviderID     string `json:"provider_id,omitempty"`
	ProviderName   string `json:"provider_name,omitempty"`
	SupportsVision bool   `json:"supports_vision"`
	Configured     bool   `json:"configured"`
}

// ResolveMaclawLLMRole resolves one loop role.
// A local pin (inherit_hub false plus a model) wins. Otherwise a non-empty
// Hub role snapshot is used on the Hub provider, even when assistant points
// at a third-party provider. Anything else uses the assistant profile.
func (a *App) ResolveMaclawLLMRole(role string) (corelib.MaclawLLMConfig, error) {
	role = strings.ToLower(strings.TrimSpace(role))
	switch role {
	case maclawLLMRoleForeground, maclawLLMRoleBackground, maclawLLMRoleComputerUse:
	default:
		return corelib.MaclawLLMConfig{}, fmt.Errorf("unknown LLM role %q", role)
	}
	if a == nil {
		return corelib.MaclawLLMConfig{}, fmt.Errorf("app is nil")
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return corelib.MaclawLLMConfig{}, err
	}
	state := a.GetMaclawLLMProviders()
	profiles, profilesErr := effectiveMaclawLLMProfiles(cfg, state.Providers, state.Current)
	assignment := corelib.MaclawLLMRoleAssignment{}
	if profiles != nil {
		assignment = maclawLLMRoleAssignment(*profiles, role)
	}
	if !assignment.InheritsHub() {
		model := normalizeDesktopRoleModel(assignment.Model)
		if model == "" {
			return corelib.MaclawLLMConfig{}, fmt.Errorf("LLM role %q has no model", role)
		}
		resolved, pinErr := a.materializePinnedRole(role, model, state.Providers, profiles)
		if pinErr != nil {
			return corelib.MaclawLLMConfig{}, pinErr
		}
		return resolved, nil
	}
	if hubModel := normalizeDesktopRoleModel(hubRoleModel(cfg.HubRoleModels, role)); hubModel != "" {
		if provider, ok := findHubServiceProvider(state.Providers); ok {
			return a.materializeResolvedRole(role, provider, hubModel), nil
		}
	}
	if profilesErr != nil {
		return corelib.MaclawLLMConfig{}, profilesErr
	}
	assistant, err := a.ResolveMaclawLLMProfile(maclawLLMProfileAssistant)
	if err != nil {
		return corelib.MaclawLLMConfig{}, err
	}
	assistant.Profile = role
	return ensureComputerUseRoleVision(assistant), nil
}

func maclawLLMRoleAssignment(profiles corelib.MaclawLLMProfiles, role string) corelib.MaclawLLMRoleAssignment {
	switch role {
	case maclawLLMRoleBackground:
		return profiles.Background
	case maclawLLMRoleComputerUse:
		return profiles.ComputerUse
	default:
		return profiles.Foreground
	}
}

func hubRoleModel(roles corelib.MaclawLLMHubRoleModels, role string) string {
	switch role {
	case maclawLLMRoleBackground:
		return roles.Background
	case maclawLLMRoleComputerUse:
		return roles.ComputerUse
	default:
		return roles.Foreground
	}
}

func normalizeDesktopRoleModel(model string) string {
	model = strings.TrimSpace(model)
	if strings.EqualFold(model, "default") || strings.EqualFold(model, hubServiceAutoModel) {
		return hubServiceAutoModel
	}
	return model
}

func findHubServiceProvider(providers []corelib.MaclawLLMProvider) (corelib.MaclawLLMProvider, bool) {
	for _, provider := range providers {
		if provider.IsHubService || isHubServiceProviderName(provider.Name) {
			return provider, true
		}
	}
	return corelib.MaclawLLMProvider{}, false
}

func (a *App) materializePinnedRole(role, model string, providers []corelib.MaclawLLMProvider, profiles *corelib.MaclawLLMProfiles) (corelib.MaclawLLMConfig, error) {
	if provider, ok := findHubServiceProvider(providers); ok {
		return a.materializeResolvedRole(role, provider, model), nil
	}
	providerID := ""
	if profiles != nil {
		providerID = profiles.Assistant.ProviderID
	}
	provider, ok := resolveMaclawLLMProviderByID(providers, providerID)
	if !ok {
		return corelib.MaclawLLMConfig{}, fmt.Errorf("LLM role %q has no provider", role)
	}
	return a.materializeResolvedRole(role, provider, model), nil
}

func (a *App) materializeResolvedRole(role string, provider corelib.MaclawLLMProvider, model string) corelib.MaclawLLMConfig {
	model = normalizeDesktopRoleModel(model)
	resolved := a.materializeMaclawLLMProvider(provider)
	resolved.Model = constrainOpenCodeModel(provider, corelib.MigrateZhipuCodingModel(provider.Name, model))
	resolved.SupportsVision = providerSupportsVisionForModel(provider, resolved.Model)
	resolved.Profile = role
	resolved.RouteSource = "base"
	return ensureComputerUseRoleVision(resolved)
}

// ensureComputerUseRoleVision sends screenshots when the computer-use role is
// vision-capable, and also when Hub will choose the model (auto/default).
// A pinned non-vision model stays text-primary.
func ensureComputerUseRoleVision(cfg corelib.MaclawLLMConfig) corelib.MaclawLLMConfig {
	if !strings.EqualFold(strings.TrimSpace(cfg.Profile), maclawLLMRoleComputerUse) || cfg.SupportsVision {
		return cfg
	}
	model := strings.TrimSpace(cfg.Model)
	if !strings.EqualFold(model, "auto") && !strings.EqualFold(model, "default") {
		return cfg
	}
	if isHubServiceProviderName(cfg.ProviderName) || corelib.IsHubManagedLLMEndpoint(cfg.URL, model) {
		cfg.SupportsVision = true
	}
	return cfg
}

func normalizeMaclawLLMRoleAssignment(role *corelib.MaclawLLMRoleAssignment) {
	if role == nil {
		return
	}
	role.Model = normalizeDesktopRoleModel(role.Model)
	if role.Model == "" {
		return
	}
	if strings.EqualFold(role.Model, "auto") {
		role.Model = hubServiceAutoModel
	}
}

func validateMaclawLLMRoleAssignment(name string, role corelib.MaclawLLMRoleAssignment) error {
	if role.InheritsHub() {
		return nil
	}
	if strings.TrimSpace(role.Model) == "" {
		return fmt.Errorf("%s model is required when it does not follow Hub", name)
	}
	return nil
}

func (a *App) maclawLLMRoleSummary(role string, assignment corelib.MaclawLLMRoleAssignment) MaclawLLMRoleSummary {
	summary := MaclawLLMRoleSummary{Role: role, InheritHub: assignment.InheritsHub()}
	resolved, err := a.ResolveMaclawLLMRole(role)
	if err != nil || strings.TrimSpace(resolved.Model) == "" {
		return summary
	}
	summary.Model = resolved.Model
	summary.ProviderID = resolved.ProviderID
	summary.ProviderName = resolved.ProviderName
	summary.SupportsVision = resolved.SupportsVision
	summary.Configured = true
	return summary
}

// syncHubRoleModels stores the Hub role snapshot. A lost entitlement clears it
// so inherit falls back to the assistant profile. Local inherit_hub:false pins
// are left on the profile.
func syncHubRoleModels(cfg *corelib.AppConfig, status HubLLMServiceStatus, keep bool) bool {
	if cfg == nil {
		return false
	}
	next := corelib.MaclawLLMHubRoleModels{}
	if keep {
		next = corelib.MaclawLLMHubRoleModels{
			Foreground:  strings.TrimSpace(status.RoleModels.Foreground),
			Background:  strings.TrimSpace(status.RoleModels.Background),
			ComputerUse: strings.TrimSpace(status.RoleModels.ComputerUse),
		}
	}
	if cfg.HubRoleModels == next {
		return false
	}
	cfg.HubRoleModels = next
	return true
}
