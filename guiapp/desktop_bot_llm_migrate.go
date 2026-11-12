package guiapp

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// migrateDesktopBotLLMRoles copies local bot role pins into the Hub choice
// once. Pins win over a legacy single provider that Hub already expanded into
// the three main roles. Roles still following Hub keep that expanded provider
// when one exists, and otherwise use the official Hub model. The grounder
// stays unused unless it was itself pinned.
//
// A failed upload leaves the marker unset so the next read tries again. When
// there is nothing to copy, or a pin names a provider this computer no longer
// has, the marker is set and the Hub choice is left as it is.
func (a *App) migrateDesktopBotLLMRoles(current DesktopBotLLM) (*DesktopBotLLM, error) {
	if a == nil {
		return &current, nil
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return &current, nil
	}
	if cfg.DesktopBotLLMRolesMigrated {
		return &current, nil
	}
	input, apply := planLocalBotRoleMigration(cfg, current)
	next := current
	if apply {
		saved, saveErr := a.SaveDesktopBotLLM(input)
		if saveErr != nil {
			return &current, nil
		}
		if saved != nil {
			next = *saved
		}
	}
	cfg.DesktopBotLLMRolesMigrated = true
	if err := a.SaveConfig(cfg); err != nil {
		return &next, nil
	}
	return &next, nil
}

// planLocalBotRoleMigration reports the custom save that preserves local role
// pins. apply is false when no role was pinned, or when a pin cannot be
// resolved to a provider on this computer.
func planLocalBotRoleMigration(cfg corelib.AppConfig, server DesktopBotLLM) (DesktopBotLLMInput, bool) {
	if !localBotRoleRowsSet(cfg.MaclawLLMProfiles) {
		return DesktopBotLLMInput{}, false
	}
	profiles := cfg.MaclawLLMProfiles
	fg, ok := migratedMainRole(maclawLLMRoleForeground, profiles.Foreground, cfg, server)
	if !ok {
		return DesktopBotLLMInput{}, false
	}
	bg, ok := migratedMainRole(maclawLLMRoleBackground, profiles.Background, cfg, server)
	if !ok {
		return DesktopBotLLMInput{}, false
	}
	cu, ok := migratedMainRole(maclawLLMRoleComputerUse, profiles.ComputerUse, cfg, server)
	if !ok {
		return DesktopBotLLMInput{}, false
	}
	gr, ok := migratedGrounderRole(profiles.Grounder, cfg.MaclawLLMProviders)
	if !ok {
		return DesktopBotLLMInput{}, false
	}
	input := DesktopBotLLMInput{
		Mode: "custom",
		Roles: &DesktopBotLLMRolesInput{
			Foreground: fg, Background: bg, ComputerUse: cu, Grounder: gr,
		},
	}
	// Older hubs ignore Roles and read the single provider. Foreground is that
	// provider when it is not the official route.
	if !fg.Official {
		input.Name = fg.Name
		input.URL = fg.URL
		input.Key = fg.Key
		input.Model = fg.Model
		input.Protocol = fg.Protocol
		input.WireAPI = fg.WireAPI
	}
	return input, true
}

func localBotRoleRowsSet(profiles *corelib.MaclawLLMProfiles) bool {
	if profiles == nil {
		return false
	}
	for _, assignment := range []corelib.MaclawLLMRoleAssignment{
		profiles.Foreground, profiles.Background, profiles.ComputerUse, profiles.Grounder,
	} {
		if roleAssignmentPinned(assignment) {
			return true
		}
	}
	return false
}

func roleAssignmentPinned(assignment corelib.MaclawLLMRoleAssignment) bool {
	return !assignment.InheritsHub() && strings.TrimSpace(assignment.Model) != ""
}

func migratedMainRole(role string, assignment corelib.MaclawLLMRoleAssignment, cfg corelib.AppConfig, server DesktopBotLLM) (DesktopBotLLMRoleInput, bool) {
	if roleAssignmentPinned(assignment) {
		return roleInputFromPin(assignment, cfg.MaclawLLMProviders)
	}
	if server.Mode == "custom" && server.Roles != nil {
		stored := server.Roles.Foreground
		switch role {
		case maclawLLMRoleBackground:
			stored = server.Roles.Background
		case maclawLLMRoleComputerUse:
			stored = server.Roles.ComputerUse
		}
		if in, ok := roleInputFromServer(stored); ok {
			return in, true
		}
	}
	if server.Mode == "custom" && server.Roles == nil {
		if in, ok := roleInputFromLegacyServer(server); ok {
			return in, true
		}
	}
	model := normalizeDesktopRoleModel(hubRoleModel(cfg.HubRoleModels, role))
	if model == "" {
		model = hubServiceAutoModel
	}
	return DesktopBotLLMRoleInput{Official: true, Model: model}, true
}

func migratedGrounderRole(assignment corelib.MaclawLLMRoleAssignment, providers []corelib.MaclawLLMProvider) (DesktopBotLLMRoleInput, bool) {
	if !roleAssignmentPinned(assignment) {
		return DesktopBotLLMRoleInput{Unused: true}, true
	}
	return roleInputFromPin(assignment, providers)
}

func roleInputFromPin(assignment corelib.MaclawLLMRoleAssignment, providers []corelib.MaclawLLMProvider) (DesktopBotLLMRoleInput, bool) {
	provider, ok := providerForRolePin(providers, assignment)
	if !ok {
		return DesktopBotLLMRoleInput{}, false
	}
	model := normalizeDesktopRoleModel(assignment.Model)
	if provider.IsHubService || isHubServiceProviderName(provider.Name) {
		return DesktopBotLLMRoleInput{Official: true, Model: model}, true
	}
	if strings.TrimSpace(provider.Key) == "" || strings.TrimSpace(provider.URL) == "" || strings.TrimSpace(provider.Name) == "" {
		return DesktopBotLLMRoleInput{}, false
	}
	return DesktopBotLLMRoleInput{
		Name: provider.Name, URL: provider.URL, Key: provider.Key, Model: model,
		Protocol: provider.Protocol, WireAPI: provider.WireAPI,
	}, true
}

func providerForRolePin(providers []corelib.MaclawLLMProvider, assignment corelib.MaclawLLMRoleAssignment) (corelib.MaclawLLMProvider, bool) {
	id := strings.TrimSpace(assignment.ProviderID)
	if id == "" {
		for _, provider := range providers {
			if provider.IsHubService || isHubServiceProviderName(provider.Name) {
				return provider, true
			}
		}
		return corelib.MaclawLLMProvider{}, false
	}
	for _, provider := range providers {
		if strings.TrimSpace(provider.ID) == id || corelib.MaclawLLMLegacyProviderID(provider.Name) == id {
			return provider, true
		}
	}
	return corelib.MaclawLLMProvider{}, false
}

func roleInputFromServer(role DesktopBotLLMRole) (DesktopBotLLMRoleInput, bool) {
	if role.Unused {
		return DesktopBotLLMRoleInput{}, false
	}
	if role.Official && strings.TrimSpace(role.Model) != "" {
		return DesktopBotLLMRoleInput{Official: true, Model: strings.TrimSpace(role.Model)}, true
	}
	if strings.TrimSpace(role.Name) == "" || strings.TrimSpace(role.Model) == "" {
		return DesktopBotLLMRoleInput{}, false
	}
	return DesktopBotLLMRoleInput{
		Name: role.Name, URL: role.URL, Model: role.Model,
		Protocol: role.Protocol, WireAPI: role.WireAPI,
	}, true
}

func roleInputFromLegacyServer(server DesktopBotLLM) (DesktopBotLLMRoleInput, bool) {
	name := strings.TrimSpace(server.Name)
	model := strings.TrimSpace(server.Model)
	if name == "" || model == "" {
		return DesktopBotLLMRoleInput{}, false
	}
	if strings.EqualFold(name, "hub-llm") {
		return DesktopBotLLMRoleInput{Official: true, Model: model}, true
	}
	return DesktopBotLLMRoleInput{Name: name, URL: server.URL, Model: model}, true
}
