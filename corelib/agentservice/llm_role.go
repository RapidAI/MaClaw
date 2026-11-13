package agentservice

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// LLM loop roles used by bot turns and desktop computer use.
const (
	LLMRoleForeground  = "foreground"
	LLMRoleBackground  = "background"
	LLMRoleComputerUse = "computer_use"
	LLMRoleGrounder    = "grounder"
)

// OfficialHubLLMProviderName is the MaClaw official row Hub writes into
// maclaw_llm_providers. Role pins that choose the official group resolve to it.
const OfficialHubLLMProviderName = "hub-llm"

// ErrGrounderUnused means no grounding model is configured for this user.
var ErrGrounderUnused = fmt.Errorf("no grounding model is configured")

// ResolveLLMRole returns the model for one bot role.
//
// MaclawBotLLMMode picks the one source Hub wrote for this user:
//   - custom: an explicit maclaw_llm_profiles role pin. A role with no pin
//     follows the service, so an administrator pin still applies there.
//   - default: the service. A Hub Bot-management pin (maclaw_hub_bot_roles)
//     selects that provider; otherwise the current provider (ResolveLLMConfig).
//     Profile pins are ignored.
//   - empty: a config Hub has not rewritten. The older combined precedence
//     still applies (profile pin, then the service) so those bots do not
//     change until the next push.
//
// Grounder is optional. Unused or an empty assignment means it is not
// configured. Inherit, on the service path, follows the current provider.
func ResolveLLMRole(cfg corelib.AppConfig, role string) (corelib.MaclawLLMConfig, error) {
	role = strings.ToLower(strings.TrimSpace(role))
	switch role {
	case LLMRoleForeground, LLMRoleBackground, LLMRoleComputerUse, LLMRoleGrounder, "":
	default:
		return corelib.MaclawLLMConfig{}, fmt.Errorf("unknown LLM role %q", role)
	}
	if role == "" {
		role = LLMRoleForeground
	}
	switch strings.ToLower(strings.TrimSpace(cfg.MaclawBotLLMMode)) {
	case corelib.MaclawBotLLMModeCustom:
		return resolveCustomBotRole(cfg, role)
	case corelib.MaclawBotLLMModeDefault:
		return resolveServiceBotRole(cfg, role)
	default:
		if pinned, ok, err := resolveProfileRolePin(cfg, role); ok {
			if err != nil {
				return corelib.MaclawLLMConfig{}, err
			}
			return roleVisionOverride(cfg, role, pinned), nil
		}
		return resolveServiceBotRole(cfg, role)
	}
}

// resolveCustomBotRole uses an explicit role pin when one is stored. A role
// left unset follows the service (administrator pin or current provider), so
// an admin pin still applies to the rows the owner did not set.
func resolveCustomBotRole(cfg corelib.AppConfig, role string) (corelib.MaclawLLMConfig, error) {
	pinned, ok, err := resolveProfileRolePin(cfg, role)
	if ok {
		if err != nil {
			return corelib.MaclawLLMConfig{}, err
		}
		return roleVisionOverride(cfg, role, pinned), nil
	}
	return resolveServiceBotRole(cfg, role)
}

// resolveServiceBotRole is the administrator / Hub route. It does not consult
// maclaw_llm_profiles.
func resolveServiceBotRole(cfg corelib.AppConfig, role string) (corelib.MaclawLLMConfig, error) {
	assignment := hubBotRole(cfg.MaclawHubBotRoles, role)
	mode := strings.ToLower(strings.TrimSpace(assignment.Mode))
	if role == LLMRoleGrounder && (mode == "" || mode == corelib.MaclawHubBotRoleUnused) {
		return corelib.MaclawLLMConfig{}, ErrGrounderUnused
	}
	if mode == corelib.MaclawHubBotRolePin {
		resolved, err := resolveNamedProvider(cfg, assignment.Provider)
		if err == nil {
			return roleVisionOverride(cfg, role, applyHubPinModel(cfg, assignment, resolved)), nil
		}
		// A stale pin must not fail the turn. Grounder stays unused rather
		// than silently borrowing the chat model.
		if role == LLMRoleGrounder {
			return corelib.MaclawLLMConfig{}, ErrGrounderUnused
		}
	}
	if role == LLMRoleGrounder && mode != corelib.MaclawHubBotRoleInherit {
		return corelib.MaclawLLMConfig{}, ErrGrounderUnused
	}
	resolved, err := ResolveLLMConfig(cfg)
	if err != nil {
		return resolved, err
	}
	return roleVisionOverride(cfg, role, resolved), nil
}

// roleVisionOverride finalizes SupportsVision the way the GUI role path does:
// the grounder always receives the screenshot, and every other role borrows
// vision on the official provider's auto/default band because Hub picks a
// vision-capable model server-side.
func roleVisionOverride(cfg corelib.AppConfig, role string, resolved corelib.MaclawLLMConfig) corelib.MaclawLLMConfig {
	if role == LLMRoleGrounder {
		resolved.SupportsVision = true
		return resolved
	}
	if provider, ok := findMaclawProviderByName(cfg.MaclawLLMProviders, resolved.ProviderName); ok {
		resolved.SupportsVision = rolePinSupportsVision(provider, resolved.Model)
	}
	return resolved
}

func hubBotRole(roles corelib.MaclawHubBotRoles, role string) corelib.MaclawHubBotRole {
	switch role {
	case LLMRoleBackground:
		return roles.Background
	case LLMRoleComputerUse:
		return roles.ComputerUse
	case LLMRoleGrounder:
		return roles.Grounder
	default:
		return roles.Foreground
	}
}

func profileRoleAssignment(profiles *corelib.MaclawLLMProfiles, role string) corelib.MaclawLLMRoleAssignment {
	if profiles == nil {
		return corelib.MaclawLLMRoleAssignment{}
	}
	switch role {
	case LLMRoleBackground:
		return profiles.Background
	case LLMRoleComputerUse:
		return profiles.ComputerUse
	case LLMRoleGrounder:
		return profiles.Grounder
	default:
		return profiles.Foreground
	}
}

// resolveProfileRolePin reports a GUI-style local pin when one is present.
// ok is false when the role still inherits (or the grounder is unset).
func resolveProfileRolePin(cfg corelib.AppConfig, role string) (corelib.MaclawLLMConfig, bool, error) {
	if cfg.MaclawLLMProfiles == nil {
		return corelib.MaclawLLMConfig{}, false, nil
	}
	assignment := profileRoleAssignment(cfg.MaclawLLMProfiles, role)
	model := strings.TrimSpace(assignment.Model)
	if assignment.InheritsHub() || model == "" {
		return corelib.MaclawLLMConfig{}, false, nil
	}
	resolve := func(provider corelib.MaclawLLMProvider) (corelib.MaclawLLMConfig, error) {
		resolved, err := ResolveLLMConfig(configWithCurrentProvider(cfg, provider.Name))
		if err != nil {
			return corelib.MaclawLLMConfig{}, err
		}
		return applyRolePinModel(resolved, provider, model), nil
	}
	// A pin that names a provider runs on it, like the GUI's
	// materializePinnedRole. A missing provider fails the role rather than
	// silently rerouting it.
	if providerID := strings.TrimSpace(assignment.ProviderID); providerID != "" {
		provider, ok := findMaclawProviderByID(cfg.MaclawLLMProviders, providerID)
		if !ok {
			return corelib.MaclawLLMConfig{}, true, fmt.Errorf("LLM role %q references an unavailable provider", role)
		}
		resolved, err := resolve(provider)
		return resolved, true, err
	}
	// Same as the GUI pin: stay on the official provider when one is listed,
	// otherwise the assistant profile's provider. The default current
	// provider is the last resort so a stale assistant pin cannot fail a turn.
	if provider, ok := findOfficialHubProvider(cfg.MaclawLLMProviders); ok {
		if resolved, rerr := resolve(provider); rerr == nil {
			return resolved, true, nil
		}
	}
	if providerID := strings.TrimSpace(cfg.MaclawLLMProfiles.Assistant.ProviderID); providerID != "" {
		if provider, ok := findMaclawProviderByID(cfg.MaclawLLMProviders, providerID); ok {
			if resolved, rerr := resolve(provider); rerr == nil {
				return resolved, true, nil
			}
		}
	}
	base, err := ResolveLLMConfig(cfg)
	if err != nil {
		return corelib.MaclawLLMConfig{}, true, err
	}
	if provider, ok := findMaclawProviderByName(cfg.MaclawLLMProviders, base.ProviderName); ok {
		return applyRolePinModel(base, provider, model), true, nil
	}
	base.Model = corelib.MigrateZhipuCodingModel(base.ProviderName, model)
	return base, true, nil
}

// applyHubPinModel uses the model stored on a Bot-management pin. The
// official group ignores it: that route schedules its own model.
func applyHubPinModel(cfg corelib.AppConfig, assignment corelib.MaclawHubBotRole, resolved corelib.MaclawLLMConfig) corelib.MaclawLLMConfig {
	model := strings.TrimSpace(assignment.Model)
	if model == "" || hubPinIgnoresModel(cfg, resolved) {
		return resolved
	}
	if provider, ok := findMaclawProviderByName(cfg.MaclawLLMProviders, resolved.ProviderName); ok {
		return applyRolePinModel(resolved, provider, model)
	}
	resolved.Model = model
	return resolved
}

func hubPinIgnoresModel(cfg corelib.AppConfig, resolved corelib.MaclawLLMConfig) bool {
	if provider, ok := findMaclawProviderByName(cfg.MaclawLLMProviders, resolved.ProviderName); ok {
		if provider.IsHubService || strings.EqualFold(strings.TrimSpace(provider.Name), OfficialHubLLMProviderName) {
			return true
		}
	}
	return strings.EqualFold(strings.TrimSpace(resolved.ProviderName), OfficialHubLLMProviderName)
}

// applyRolePinModel mirrors the GUI's materializeResolvedRole: the pinned
// model replaces the provider default and image capability is recomputed for
// that model rather than carried over from the provider's default.
func applyRolePinModel(resolved corelib.MaclawLLMConfig, provider corelib.MaclawLLMProvider, model string) corelib.MaclawLLMConfig {
	resolved.Model = corelib.MigrateZhipuCodingModel(provider.Name, model)
	resolved.SupportsVision = rolePinSupportsVision(provider, resolved.Model)
	return resolved
}

// rolePinSupportsVision recomputes the probed flag for the pinned model and,
// like the GUI's ensureHubAutoRoleVision, keeps vision on the official
// provider's auto/default band because Hub picks a vision-capable model
// server-side.
func rolePinSupportsVision(provider corelib.MaclawLLMProvider, model string) bool {
	sees, known, _ := corelib.ProviderVisionDecision(provider, model)
	if known {
		if sees {
			return true
		}
		// An explicit off wins, including on the official auto band.
		if corelib.ModelVisionOverride(provider, model) == "off" {
			return false
		}
		if !provider.IsHubService && !strings.EqualFold(strings.TrimSpace(provider.Name), OfficialHubLLMProviderName) {
			return false
		}
	} else if !provider.IsHubService && !strings.EqualFold(strings.TrimSpace(provider.Name), OfficialHubLLMProviderName) {
		return false
	}
	model = strings.TrimSpace(model)
	return strings.EqualFold(model, "auto") || strings.EqualFold(model, "default")
}

func findMaclawProviderByID(providers []corelib.MaclawLLMProvider, providerID string) (corelib.MaclawLLMProvider, bool) {
	for _, provider := range providers {
		if strings.TrimSpace(provider.ID) == providerID || corelib.MaclawLLMLegacyProviderID(provider.Name) == providerID {
			return provider, true
		}
	}
	return corelib.MaclawLLMProvider{}, false
}

func findMaclawProviderByName(providers []corelib.MaclawLLMProvider, name string) (corelib.MaclawLLMProvider, bool) {
	for _, provider := range providers {
		if corelib.MaclawLLMProviderNameEqual(provider.Name, name) {
			return provider, true
		}
	}
	return corelib.MaclawLLMProvider{}, false
}

func findOfficialHubProvider(providers []corelib.MaclawLLMProvider) (corelib.MaclawLLMProvider, bool) {
	for _, provider := range providers {
		if provider.IsHubService || strings.EqualFold(strings.TrimSpace(provider.Name), OfficialHubLLMProviderName) {
			return provider, true
		}
	}
	return corelib.MaclawLLMProvider{}, false
}

func resolveNamedProvider(cfg corelib.AppConfig, name string) (corelib.MaclawLLMConfig, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return corelib.MaclawLLMConfig{}, fmt.Errorf("role provider is empty")
	}
	if strings.EqualFold(name, OfficialHubLLMProviderName) || strings.EqualFold(name, "maclaw_official_group") || strings.EqualFold(name, "system-free") {
		if provider, ok := findOfficialHubProvider(cfg.MaclawLLMProviders); ok {
			name = provider.Name
		} else {
			name = OfficialHubLLMProviderName
		}
	}
	return ResolveLLMConfig(configWithCurrentProvider(cfg, name))
}

func configWithCurrentProvider(cfg corelib.AppConfig, name string) corelib.AppConfig {
	next := cfg
	next.MaclawLLMCurrentProvider = name
	return next
}

// ResolveTurnLLMRole picks the loop role for one MaClawSrv turn: a long-job
// watcher uses background, everything else uses foreground.
func ResolveTurnLLMRole(cfg corelib.AppConfig, messageMeta, instanceMeta map[string]string) (corelib.MaclawLLMConfig, error) {
	role := LLMRoleForeground
	if metaValue(messageMeta, "long_job_watch") != "" || metaValue(instanceMeta, "long_job_watch") != "" {
		role = LLMRoleBackground
	}
	return ResolveLLMRole(cfg, role)
}
