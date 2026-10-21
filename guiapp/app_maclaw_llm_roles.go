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
// A local pin (inherit_hub false plus a model) wins and stays on the Hub
// provider when one exists. Otherwise the interactive assistant (foreground)
// uses the assistant profile whenever that profile is not MaClaw official, and
// uses an explicit official model the user selected over the Hub band.
// Background and computer-use keep the Hub role snapshot. An official
// assistant left on auto still takes that snapshot. Anything else uses the
// assistant profile.
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
	state := a.maclawLLMProvidersFromConfig(&cfg)
	return a.resolveMaclawLLMRoleFromConfig(role, cfg, state.Providers, state.Current)
}

// resolveMaclawLLMRoleFromConfig resolves one role from the config and provider
// rows the caller already loaded. The settings panel uses this so the
// effective model line cannot observe a later snapshot than the draft.
func (a *App) resolveMaclawLLMRoleFromConfig(role string, cfg corelib.AppConfig, providers []corelib.MaclawLLMProvider, current string) (corelib.MaclawLLMConfig, error) {
	profiles, profilesErr := effectiveMaclawLLMProfiles(cfg, providers, current)
	assignment := corelib.MaclawLLMRoleAssignment{}
	if profiles != nil {
		assignment = maclawLLMRoleAssignment(*profiles, role)
	}
	if !assignment.InheritsHub() {
		model := normalizeDesktopRoleModel(assignment.Model)
		if model == "" {
			return corelib.MaclawLLMConfig{}, fmt.Errorf("LLM role %q has no model", role)
		}
		resolved, pinErr := a.materializePinnedRole(role, model, providers, profiles)
		if pinErr != nil {
			return corelib.MaclawLLMConfig{}, pinErr
		}
		return resolved, nil
	}
	snapshot := normalizeDesktopRoleModel(hubRoleModel(cfg.HubRoleModels, role))
	if profiles != nil {
		provider, model, selErr := effectiveMaclawLLMProfileSelection(maclawLLMProfileAssistant, cfg, providers, current, profiles)
		if selErr == nil && roleUsesAssistantSelection(role, provider, model, snapshot) {
			// Official auto borrows vision. A third-party auto model keeps the
			// probe stored on its provider.
			assistant := ensureHubAutoRoleVision(a.materializeMaclawLLMSelection(role, provider, model), providerIsHubService(provider))
			// Computer use needs a model that sees the screenshots; a
			// text-only assistant leaves that role on the Hub snapshot.
			if role != maclawLLMRoleComputerUse || assistant.SupportsVision || snapshot == "" {
				return assistant, nil
			}
		}
	}
	if snapshot != "" {
		if provider, ok := findHubServiceProvider(providers); ok {
			return a.materializeResolvedRole(role, provider, snapshot), nil
		}
	}
	if profilesErr != nil {
		return corelib.MaclawLLMConfig{}, profilesErr
	}
	assistant, provider, err := a.materializeEffectiveMaclawLLMProfile(maclawLLMProfileAssistant, cfg, providers, current, profiles)
	if err != nil {
		return corelib.MaclawLLMConfig{}, err
	}
	assistant.Profile = role
	// Lost entitlement clears the Hub snapshot, so every role falls back here.
	// Only the official provider borrows vision for auto.
	return ensureHubAutoRoleVision(assistant, providerIsHubService(provider)), nil
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

// providerIsHubService reports the MaClaw official catalog row. A third-party
// URL that merely contains /api/llm/v1 is not official: that path shape is
// also how IsHubManagedLLMEndpoint recognizes Hub routing.
func providerIsHubService(provider corelib.MaclawLLMProvider) bool {
	return provider.IsHubService || isHubServiceProviderName(provider.Name)
}

// roleUsesAssistantSelection: an empty role follows the person's current
// (assistant) model, and the Hub snapshot only when that model is MaClaw
// official on auto. Foreground always follows a custom assistant. Background
// follows it too (before 2026-10-21 it kept the Hub snapshot, so a
// custom-model user's worker loops ran on Hub auto). Computer use follows it
// only when that model can see images (checked by the caller after
// materializing); a text-only assistant keeps the Hub snapshot there.
func roleUsesAssistantSelection(role string, provider corelib.MaclawLLMProvider, model, snapshot string) bool {
	if !foregroundUsesAssistantSelection(provider, model, snapshot) {
		return false
	}
	return role == maclawLLMRoleForeground || role == maclawLLMRoleBackground || role == maclawLLMRoleComputerUse
}

// ResolveHubLLMRole is the Hub's shared model for one role, ignoring the
// person's own settings. Callers use it only as the last attempt after the
// person's own model failed.
func (a *App) ResolveHubLLMRole(role string) (corelib.MaclawLLMConfig, error) {
	if a == nil {
		return corelib.MaclawLLMConfig{}, fmt.Errorf("app is nil")
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return corelib.MaclawLLMConfig{}, err
	}
	state := a.maclawLLMProvidersFromConfig(&cfg)
	provider, ok := findHubServiceProvider(state.Providers)
	if !ok {
		return corelib.MaclawLLMConfig{}, fmt.Errorf("no Hub service provider")
	}
	model := normalizeDesktopRoleModel(hubRoleModel(cfg.HubRoleModels, role))
	if model == "" {
		model = hubServiceAutoModel
	}
	return a.materializeResolvedRole(role, provider, model), nil
}

// foregroundUsesAssistantSelection reports that interactive chat should call
// the assistant assignment. Any non-official provider wins. On the official
// provider, an explicit model other than auto wins over the Hub band. Auto
// keeps the band so an untouched assistant profile still follows Hub.
// mid and official-mid are the same band: the assignment UI stores the short
// name, while Hub's snapshot stores the official-* id.
func foregroundUsesAssistantSelection(provider corelib.MaclawLLMProvider, model, snapshot string) bool {
	if !providerIsHubService(provider) {
		return true
	}
	chosen := normalizeDesktopRoleModel(model)
	if chosen == "" || chosen == hubServiceAutoModel {
		return false
	}
	return !sameDesktopRoleModel(chosen, snapshot)
}

func sameDesktopRoleModel(a, b string) bool {
	left := normalizeDesktopRoleModel(a)
	right := normalizeDesktopRoleModel(b)
	if strings.EqualFold(left, right) {
		return true
	}
	leftBand := canonicalOfficialBand(left)
	rightBand := canonicalOfficialBand(right)
	return leftBand != "" && leftBand == rightBand
}

func canonicalOfficialBand(model string) string {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "low", "official-low":
		return "official-low"
	case "mid", "official-mid":
		return "official-mid"
	case "high", "official-high":
		return "official-high"
	case "auto", "default":
		return hubServiceAutoModel
	default:
		return ""
	}
}

func findHubServiceProvider(providers []corelib.MaclawLLMProvider) (corelib.MaclawLLMProvider, bool) {
	for _, provider := range providers {
		if providerIsHubService(provider) {
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
	model = constrainOpenCodeModel(provider, corelib.MigrateZhipuCodingModel(provider.Name, normalizeDesktopRoleModel(model)))
	return ensureHubAutoRoleVision(a.materializeMaclawLLMSelection(role, provider, model), providerIsHubService(provider))
}

// ensureHubAutoRoleVision attaches images when the MaClaw official provider
// will choose the model. Picture-PDF pages and screenshots are sent only after
// SupportsVision is set; Hub's auto route then sees the image parts and can
// stay on a vision model. A third-party auto model keeps its probed flag,
// including one whose URL contains /api/llm/v1. A pinned text model does too.
func ensureHubAutoRoleVision(cfg corelib.MaclawLLMConfig, official bool) corelib.MaclawLLMConfig {
	if cfg.SupportsVision || !official {
		return cfg
	}
	model := strings.TrimSpace(cfg.Model)
	if !strings.EqualFold(model, "auto") && !strings.EqualFold(model, "default") {
		return cfg
	}
	cfg.SupportsVision = true
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

func (a *App) maclawLLMRoleSummary(role string, assignment corelib.MaclawLLMRoleAssignment, cfg corelib.AppConfig, providers []corelib.MaclawLLMProvider, current string) MaclawLLMRoleSummary {
	summary := MaclawLLMRoleSummary{Role: role, InheritHub: assignment.InheritsHub()}
	resolved, err := a.resolveMaclawLLMRoleFromConfig(role, cfg, providers, current)
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
