package guiapp

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

var errBotLLMApp = fmt.Errorf("app is nil")

// SyncDesktopBotLLM reads the Hub bot-model choice and, once per Hub account,
// reconciles local role pins with it. A choice Hub already stored wins: local
// pins are cleared and not uploaded, except pins still waiting for a save
// because they need a key. Pins are copied up only when Hub has never stored
// a choice. API keys are not uploaded here; a pin that needs one stays on
// this computer until the owner saves the Model tab.
func (a *App) SyncDesktopBotLLM() (*DesktopBotLLM, error) {
	if a == nil {
		return nil, errBotLLMApp
	}
	a.botLLMSyncMu.Lock()
	defer a.botLLMSyncMu.Unlock()
	view, err := a.fetchDesktopBotLLM()
	if err != nil {
		if cached, ok := a.desktopBotLLMChoice(a.configuredRemoteUserID()); ok {
			return &cached, nil
		}
		return nil, err
	}
	return a.reconcileDesktopBotLLM(view)
}

func (a *App) reconcileDesktopBotLLM(view DesktopBotLLM) (*DesktopBotLLM, error) {
	owner := a.configuredRemoteUserID()
	cfg, err := a.LoadConfig()
	if err != nil {
		a.rememberDesktopBotLLM(owner, view)
		return &view, nil
	}
	account := desktopBotLLMAccount(cfg, owner)
	if owner != "" && account.Migrated {
		a.rememberDesktopBotLLM(owner, view)
		return &view, nil
	}
	plan := planLocalBotRoleMigration(cfg, view, account.Pending)
	next := view
	if plan.Upload {
		savedView, saveErr := a.putDesktopBotLLM(plan.Input)
		saved := &savedView
		if saveErr != nil {
			log.Printf("[bot-llm] could not store role rows: %s", saveErr.Error())
			display := overlayPendingRoles(view, cfg, plan.Pending)
			display.NeedsRoleSave = true
			a.rememberDesktopBotLLM(owner, view)
			return &display, nil
		}
		if saved != nil {
			next = *saved
		}
		if next.Roles == nil {
			log.Printf("[bot-llm] hub did not return role rows; local pins were left in place")
			a.rememberDesktopBotLLM(owner, view)
			return &view, nil
		}
		view = next
	}
	for _, reason := range plan.Reasons {
		log.Printf("[bot-llm] %s", reason)
	}
	mark := plan.Mark && len(plan.Pending) == 0 && (plan.Upload == false || next.Roles != nil)
	if plan.Upload && next.Roles == nil {
		mark = false
	}
	if err := a.patchDesktopBotLLMAccount(owner, mark, plan.Clear, plan.Pending, view); err != nil {
		log.Printf("[bot-llm] could not record the bot model view: %s", err.Error())
	} else {
		a.rememberDesktopBotLLM(owner, view)
	}
	display := next
	if len(plan.Pending) > 0 {
		display = overlayPendingRoles(view, cfg, plan.Pending)
		display.NeedsRoleSave = true
	}
	return &display, nil
}

type localBotRoleMigration struct {
	Upload  bool
	Input   DesktopBotLLMInput
	Clear   []string
	Pending []string
	Reasons []string
	Mark    bool
}

func planLocalBotRoleMigration(cfg corelib.AppConfig, server DesktopBotLLM, keptPending []string) localBotRoleMigration {
	profiles := cfg.MaclawLLMProfiles
	pinned := pinnedRoleNames(profiles)
	if server.Stored {
		if len(keptPending) == 0 {
			return localBotRoleMigration{Mark: true, Clear: pinned}
		}
		var clear, still []string
		for _, name := range pinned {
			if containsRole(keptPending, name) {
				still = append(still, name)
				continue
			}
			clear = append(clear, name)
		}
		return localBotRoleMigration{Clear: clear, Pending: still, Mark: len(still) == 0}
	}
	if len(pinned) == 0 {
		return localBotRoleMigration{Mark: true}
	}
	roles := DesktopBotLLMRolesInput{
		Foreground:  DesktopBotLLMRoleInput{Inherit: true},
		Background:  DesktopBotLLMRoleInput{Inherit: true},
		ComputerUse: DesktopBotLLMRoleInput{Inherit: true},
		Grounder:    DesktopBotLLMRoleInput{Unused: true},
	}
	var clear, pending, reasons []string
	consider := func(name string, assignment corelib.MaclawLLMRoleAssignment, grounder bool) {
		if !roleAssignmentPinned(assignment) {
			return
		}
		uploaded, reason := classifyLocalPin(name, assignment, cfg.MaclawLLMProviders)
		if reason != "" {
			reasons = append(reasons, reason)
			pending = append(pending, name)
			return
		}
		input := uploaded
		switch name {
		case maclawLLMRoleForeground:
			roles.Foreground = input
		case maclawLLMRoleBackground:
			roles.Background = input
		case maclawLLMRoleComputerUse:
			roles.ComputerUse = input
		case maclawLLMRoleGrounder:
			roles.Grounder = input
		}
		clear = append(clear, name)
		_ = grounder
	}
	if profiles != nil {
		consider(maclawLLMRoleForeground, profiles.Foreground, false)
		consider(maclawLLMRoleBackground, profiles.Background, false)
		consider(maclawLLMRoleComputerUse, profiles.ComputerUse, false)
		consider(maclawLLMRoleGrounder, profiles.Grounder, true)
	}
	if len(clear) == 0 {
		return localBotRoleMigration{Pending: pending, Reasons: reasons}
	}
	return localBotRoleMigration{
		Upload: true,
		Input: DesktopBotLLMInput{
			Mode:  "custom",
			Roles: &roles,
		},
		Clear:   clear,
		Pending: pending,
		Reasons: reasons,
		Mark:    len(pending) == 0,
	}
}

// classifyLocalPin reports a key-free role Hub can store. A provider that
// needs an API key is left pending: automatic migration does not upload keys.
func classifyLocalPin(role string, assignment corelib.MaclawLLMRoleAssignment, providers []corelib.MaclawLLMProvider) (DesktopBotLLMRoleInput, string) {
	provider, ok := providerForRolePin(providers, assignment)
	model := normalizeDesktopRoleModel(assignment.Model)
	if !ok {
		return DesktopBotLLMRoleInput{}, "role " + role + " kept locally: provider is not on this computer"
	}
	if provider.IsHubService || isHubServiceProviderName(provider.Name) {
		return DesktopBotLLMRoleInput{Official: true, Model: model}, ""
	}
	if strings.TrimSpace(provider.Key) == "" {
		return DesktopBotLLMRoleInput{}, "role " + role + " kept locally: provider " + strings.TrimSpace(provider.Name) + " has no API key until you save"
	}
	return DesktopBotLLMRoleInput{}, "role " + role + " kept locally: provider " + strings.TrimSpace(provider.Name) + " key is sent only when you save"
}

func containsRole(names []string, name string) bool {
	for _, item := range names {
		if item == name {
			return true
		}
	}
	return false
}

func pinnedRoleNames(profiles *corelib.MaclawLLMProfiles) []string {
	if profiles == nil {
		return nil
	}
	var names []string
	if roleAssignmentPinned(profiles.Foreground) {
		names = append(names, maclawLLMRoleForeground)
	}
	if roleAssignmentPinned(profiles.Background) {
		names = append(names, maclawLLMRoleBackground)
	}
	if roleAssignmentPinned(profiles.ComputerUse) {
		names = append(names, maclawLLMRoleComputerUse)
	}
	if roleAssignmentPinned(profiles.Grounder) {
		names = append(names, maclawLLMRoleGrounder)
	}
	return names
}

func roleAssignmentPinned(assignment corelib.MaclawLLMRoleAssignment) bool {
	return !assignment.InheritsHub() && strings.TrimSpace(assignment.Model) != ""
}

func overlayPendingRoles(view DesktopBotLLM, cfg corelib.AppConfig, pending []string) DesktopBotLLM {
	if cfg.MaclawLLMProfiles == nil || len(pending) == 0 {
		view.NeedsRoleSave = len(pending) > 0
		return view
	}
	if view.Roles == nil {
		view.Roles = &DesktopBotLLMRoles{}
	}
	view.Choice = "custom"
	for _, name := range pending {
		assignment := roleAssignmentByName(*cfg.MaclawLLMProfiles, name)
		provider, ok := providerForRolePin(cfg.MaclawLLMProviders, assignment)
		role := DesktopBotLLMRole{Model: normalizeDesktopRoleModel(assignment.Model)}
		if ok {
			role.Name = provider.Name
			role.URL = provider.URL
			role.Protocol = provider.Protocol
			role.WireAPI = provider.WireAPI
			if provider.IsHubService || isHubServiceProviderName(provider.Name) {
				role.Official = true
			}
		}
		switch name {
		case maclawLLMRoleForeground:
			view.Roles.Foreground = role
		case maclawLLMRoleBackground:
			view.Roles.Background = role
		case maclawLLMRoleComputerUse:
			view.Roles.ComputerUse = role
		case maclawLLMRoleGrounder:
			view.Roles.Grounder = role
		}
	}
	view.NeedsRoleSave = true
	return view
}

func roleAssignmentByName(profiles corelib.MaclawLLMProfiles, name string) corelib.MaclawLLMRoleAssignment {
	switch name {
	case maclawLLMRoleBackground:
		return profiles.Background
	case maclawLLMRoleComputerUse:
		return profiles.ComputerUse
	case maclawLLMRoleGrounder:
		return profiles.Grounder
	default:
		return profiles.Foreground
	}
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

func (a *App) patchDesktopBotLLMAccount(owner string, migrated bool, clear []string, pending []string, view DesktopBotLLM) error {
	if a == nil || owner == "" && len(clear) == 0 {
		return nil
	}
	return a.mutateConfig(func(cfg *corelib.AppConfig) {
		if cfg.MaclawLLMProfiles != nil && len(clear) > 0 {
			profiles := *cfg.MaclawLLMProfiles
			for _, name := range clear {
				switch name {
				case maclawLLMRoleForeground:
					profiles.Foreground = corelib.MaclawLLMRoleAssignment{}
				case maclawLLMRoleBackground:
					profiles.Background = corelib.MaclawLLMRoleAssignment{}
				case maclawLLMRoleComputerUse:
					profiles.ComputerUse = corelib.MaclawLLMRoleAssignment{}
				case maclawLLMRoleGrounder:
					profiles.Grounder = corelib.MaclawLLMRoleAssignment{}
				}
			}
			cfg.MaclawLLMProfiles = &profiles
		}
		if owner == "" {
			return
		}
		if cfg.DesktopBotLLMAccounts == nil {
			cfg.DesktopBotLLMAccounts = map[string]corelib.DesktopBotLLMAccount{}
		}
		account := cfg.DesktopBotLLMAccounts[owner]
		if migrated {
			account.Migrated = true
			account.Pending = nil
		} else {
			account.Pending = append([]string(nil), pending...)
		}
		raw, err := json.Marshal(viewWithoutHint(view))
		if err == nil {
			account.ChoiceJSON = string(raw)
		}
		cfg.DesktopBotLLMAccounts[owner] = account
	}, configMutateOpts{caller: "desktop-bot-llm"})
}

func viewWithoutHint(view DesktopBotLLM) DesktopBotLLM {
	view.NeedsRoleSave = false
	view.Warning = ""
	return view
}

func desktopBotLLMAccount(cfg corelib.AppConfig, owner string) corelib.DesktopBotLLMAccount {
	if cfg.DesktopBotLLMAccounts == nil {
		return corelib.DesktopBotLLMAccount{}
	}
	return cfg.DesktopBotLLMAccounts[owner]
}

func (a *App) rememberDesktopBotLLM(owner string, view DesktopBotLLM) {
	if a == nil {
		return
	}
	a.botLLMCacheMu.Lock()
	if a.botLLMChoice == nil {
		a.botLLMChoice = map[string]DesktopBotLLM{}
	}
	a.botLLMChoice[owner] = viewWithoutHint(view)
	a.botLLMCacheMu.Unlock()
	if owner == "" {
		return
	}
	raw, err := json.Marshal(viewWithoutHint(view))
	if err != nil {
		return
	}
	_, _ = a.mutateConfigMaybe(func(cfg *corelib.AppConfig) bool {
		if cfg.DesktopBotLLMAccounts == nil {
			cfg.DesktopBotLLMAccounts = map[string]corelib.DesktopBotLLMAccount{}
		}
		account := cfg.DesktopBotLLMAccounts[owner]
		if account.ChoiceJSON == string(raw) {
			return false
		}
		account.ChoiceJSON = string(raw)
		cfg.DesktopBotLLMAccounts[owner] = account
		return true
	}, configMutateOpts{caller: "desktop-bot-llm-cache"})
}

func (a *App) desktopBotLLMChoice(owner string) (DesktopBotLLM, bool) {
	if a == nil {
		return DesktopBotLLM{}, false
	}
	a.botLLMCacheMu.RLock()
	var view DesktopBotLLM
	ok := false
	if a.botLLMChoice != nil {
		view, ok = a.botLLMChoice[owner]
		ok = ok && (view.Choice != "" || view.Mode != "" || view.Stored || view.Effective != nil)
	}
	a.botLLMCacheMu.RUnlock()
	if ok {
		return view, true
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return DesktopBotLLM{}, false
	}
	account := desktopBotLLMAccount(cfg, owner)
	if account.ChoiceJSON == "" {
		return DesktopBotLLM{}, false
	}
	var cached DesktopBotLLM
	if err := json.Unmarshal([]byte(account.ChoiceJSON), &cached); err != nil {
		return DesktopBotLLM{}, false
	}
	a.rememberDesktopBotLLM(owner, cached)
	return cached, true
}
