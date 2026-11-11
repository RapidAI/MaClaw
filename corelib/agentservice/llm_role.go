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
// Precedence (same idea as the GUI's ResolveMaclawLLMRole, applied on the
// MaClawSrv user config Hub writes):
//  1. A local maclaw_llm_profiles pin (inherit_hub=false plus a model) wins.
//  2. A Hub Bot-management pin (maclaw_hub_bot_roles) selects that provider.
//  3. Otherwise the default current provider (ResolveLLMConfig).
//
// Grounder is optional. Unused or an empty assignment means it is not
// configured (the GUI default). Inherit follows the default current provider
// (Hub Bot management 跟随默认). A pin selects that provider.
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

	if pinned, ok, err := resolveProfileRolePin(cfg, role); ok {
		if err != nil {
			return corelib.MaclawLLMConfig{}, err
		}
		return pinned, nil
	}

	assignment := hubBotRole(cfg.MaclawHubBotRoles, role)
	mode := strings.ToLower(strings.TrimSpace(assignment.Mode))
	if role == LLMRoleGrounder && (mode == "" || mode == corelib.MaclawHubBotRoleUnused) {
		return corelib.MaclawLLMConfig{}, ErrGrounderUnused
	}
	if mode == corelib.MaclawHubBotRolePin {
		resolved, err := resolveNamedProvider(cfg, assignment.Provider)
		if err == nil {
			return resolved, nil
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
	return ResolveLLMConfig(cfg)
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
	base, err := ResolveLLMConfig(cfg)
	if err != nil {
		return corelib.MaclawLLMConfig{}, true, err
	}
	// Same as the GUI pin: stay on the official provider when one is listed,
	// otherwise keep the default current provider and only override the model.
	if provider, ok := findOfficialHubProvider(cfg.MaclawLLMProviders); ok {
		if resolved, rerr := ResolveLLMConfig(configWithCurrentProvider(cfg, provider.Name)); rerr == nil {
			resolved.Model = model
			return resolved, true, nil
		}
	}
	base.Model = model
	return base, true, nil
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
