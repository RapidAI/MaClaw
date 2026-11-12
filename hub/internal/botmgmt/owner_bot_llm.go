package botmgmt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// ownerBotLLM is the one model choice for one owner's bots.
// Mode default leaves every role to the service (the Hub route, or the
// provider and role pins an administrator configured). Mode custom stores
// the four role rows and is the only source MaClawSrv consults for that user.
type ownerBotLLM struct {
	Mode        string            `json:"mode"`
	Foreground  *ownerLLMOverride `json:"foreground,omitempty"`
	Background  *ownerLLMOverride `json:"background,omitempty"`
	ComputerUse *ownerLLMOverride `json:"computer_use,omitempty"`
	Grounder    *ownerLLMOverride `json:"grounder,omitempty"`
}

// persistedOwnerBotLLM returns the stored choice, migrating a legacy
// single-provider override the first time this owner is read.
func (s *Service) persistedOwnerBotLLM(ctx context.Context, tenantID, hubUserID string) (ownerBotLLM, bool, error) {
	if s == nil || s.System == nil {
		return ownerBotLLM{}, false, ErrSettingsUnavailable
	}
	hubUserID = strings.TrimSpace(hubUserID)
	if hubUserID == "" {
		return ownerBotLLM{}, false, fmt.Errorf("%w: hub user id is required", ErrInvalidInput)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return ownerBotLLM{}, false, err
	}
	if migrateOwnerBotLLM(&rec, hubUserID) {
		if err := s.save(ctx, tenantID, rec); err != nil {
			return ownerBotLLM{}, false, err
		}
	}
	_, stored := ownerBotLLMEntry(rec, hubUserID)
	return ownerBotLLMOf(rec, hubUserID), stored, nil
}

func ownerBotLLMEntry(rec record, hubUserID string) (*ownerBotLLM, bool) {
	if rec.OwnerBotLLMs == nil {
		return nil, false
	}
	choice := rec.OwnerBotLLMs[strings.TrimSpace(hubUserID)]
	return choice, choice != nil
}

// migrateOwnerBotLLM copies a legacy single-provider override into the role
// rows when this owner has no role choice yet. Foreground, background and
// computer use take that provider and model; the grounder stays unused.
// An owner who already has role rows keeps them, and the leftover override
// is dropped so the two cannot diverge.
// changed reports that the record should be saved.
func migrateOwnerBotLLM(rec *record, hubUserID string) bool {
	if rec == nil {
		return false
	}
	hubUserID = strings.TrimSpace(hubUserID)
	if hubUserID == "" {
		return false
	}
	if rec.OwnerBotLLMs != nil {
		if _, ok := rec.OwnerBotLLMs[hubUserID]; ok {
			return dropOwnerLLMOverride(rec, hubUserID)
		}
	}
	override := effectiveOwnerLLMOverride(*rec, hubUserID)
	if override == nil {
		// Nothing was stored. Leave the map empty so a later first save is
		// the thing that creates the row, and drop an incomplete override
		// that would otherwise keep looking like a choice.
		return dropOwnerLLMOverride(rec, hubUserID)
	}
	choice := ownerBotLLM{
		Mode:        corelib.MaclawBotLLMModeCustom,
		Foreground:  cloneOwnerLLMOverride(override),
		Background:  cloneOwnerLLMOverride(override),
		ComputerUse: cloneOwnerLLMOverride(override),
	}
	storeOwnerBotLLM(rec, hubUserID, choice)
	return true
}

func dropOwnerLLMOverride(rec *record, hubUserID string) bool {
	if rec.OwnerLLMOverrides == nil {
		return false
	}
	if _, ok := rec.OwnerLLMOverrides[hubUserID]; !ok {
		return false
	}
	delete(rec.OwnerLLMOverrides, hubUserID)
	if len(rec.OwnerLLMOverrides) == 0 {
		rec.OwnerLLMOverrides = nil
	}
	return true
}

func storeOwnerBotLLM(rec *record, hubUserID string, choice ownerBotLLM) {
	if rec.OwnerBotLLMs == nil {
		rec.OwnerBotLLMs = map[string]*ownerBotLLM{}
	}
	stored := cloneOwnerBotLLM(choice)
	rec.OwnerBotLLMs[hubUserID] = &stored
	dropOwnerLLMOverride(rec, hubUserID)
}

func ownerBotLLMOf(rec record, hubUserID string) ownerBotLLM {
	hubUserID = strings.TrimSpace(hubUserID)
	if rec.OwnerBotLLMs != nil {
		if choice := rec.OwnerBotLLMs[hubUserID]; choice != nil {
			return cloneOwnerBotLLM(*choice)
		}
	}
	if override := effectiveOwnerLLMOverride(rec, hubUserID); override != nil {
		return ownerBotLLM{
			Mode:        corelib.MaclawBotLLMModeCustom,
			Foreground:  cloneOwnerLLMOverride(override),
			Background:  cloneOwnerLLMOverride(override),
			ComputerUse: cloneOwnerLLMOverride(override),
		}
	}
	return ownerBotLLM{Mode: corelib.MaclawBotLLMModeDefault}
}

func ownerBotLLMIsCustom(rec record, hubUserID string) bool {
	return ownerBotLLMOf(rec, hubUserID).Mode == corelib.MaclawBotLLMModeCustom
}

func cloneOwnerBotLLM(in ownerBotLLM) ownerBotLLM {
	mode := strings.ToLower(strings.TrimSpace(in.Mode))
	if mode != corelib.MaclawBotLLMModeCustom {
		return ownerBotLLM{Mode: corelib.MaclawBotLLMModeDefault}
	}
	return ownerBotLLM{
		Mode:        corelib.MaclawBotLLMModeCustom,
		Foreground:  cloneOwnerLLMOverride(in.Foreground),
		Background:  cloneOwnerLLMOverride(in.Background),
		ComputerUse: cloneOwnerLLMOverride(in.ComputerUse),
		Grounder:    cloneOwnerLLMOverride(in.Grounder),
	}
}

func cloneOwnerLLMOverride(in *ownerLLMOverride) *ownerLLMOverride {
	if in == nil {
		return nil
	}
	cp := *in
	return &cp
}

func (choice ownerBotLLM) view(stored bool) PrincipalLLMView {
	if choice.Mode != corelib.MaclawBotLLMModeCustom {
		return PrincipalLLMView{Mode: corelib.MaclawBotLLMModeDefault, Choice: corelib.MaclawBotLLMModeDefault, Stored: stored}
	}
	roles := &PrincipalLLMRolesView{
		Foreground:  roleView(choice.Foreground, false),
		Background:  roleView(choice.Background, false),
		ComputerUse: roleView(choice.ComputerUse, false),
		Grounder:    roleView(choice.Grounder, true),
	}
	view := PrincipalLLMView{
		Mode: corelib.MaclawBotLLMModeCustom, Choice: corelib.MaclawBotLLMModeCustom,
		Roles: roles, Stored: stored,
	}
	// Older GUIs read one provider. A concrete provider is selectable. An
	// official foreground is the Hub route, which that dialog cannot pick, so
	// the legacy fields say default while Choice keeps the real mode.
	if concrete := firstConcreteRole(choice); concrete != nil && (choice.Foreground == nil || !choice.Foreground.Official) {
		view.Name = concrete.Name
		view.URL = concrete.URL
		view.Model = concrete.Model
		view.KeySet = strings.TrimSpace(concrete.Key) != ""
		return view
	}
	view.Mode = corelib.MaclawBotLLMModeDefault
	return view
}

// effectiveServiceRoles is the model each role uses when the owner does not
// pin it. Names and models only; the provider key stays in the settings record.
func effectiveServiceRoles(settings llmSettings) *PrincipalLLMRolesView {
	roles := llmRolesView(settings.Roles)
	return &PrincipalLLMRolesView{
		Foreground:  effectiveServiceRole(settings, roles.Foreground, false),
		Background:  effectiveServiceRole(settings, roles.Background, false),
		ComputerUse: effectiveServiceRole(settings, roles.ComputerUse, false),
		Grounder:    effectiveServiceRole(settings, roles.Grounder, true),
	}
}

func effectiveServiceRole(settings llmSettings, role LLMRoleAssignment, grounder bool) PrincipalLLMRoleView {
	mode := strings.ToLower(strings.TrimSpace(role.Mode))
	if grounder && (mode == "" || mode == corelib.MaclawHubBotRoleUnused) {
		return PrincipalLLMRoleView{Unused: true}
	}
	if mode == corelib.MaclawHubBotRolePin {
		return serviceProviderView(settings, role.Provider)
	}
	if selected, ok := selectedBotLLMProvider(settings); ok {
		return PrincipalLLMRoleView{Name: selected.Name, Model: selected.Model}
	}
	return PrincipalLLMRoleView{Official: true, Model: botLLMModel}
}

func serviceProviderView(settings llmSettings, id string) PrincipalLLMRoleView {
	name := roleProviderName(settings, id)
	if name == botLLMProviderName || isBotDefaultServiceGroup(id) {
		return PrincipalLLMRoleView{Official: true, Model: botLLMModel}
	}
	for _, item := range settings.Providers {
		if item.ID == strings.TrimSpace(id) || corelib.MaclawLLMProviderNameEqual(item.Name, name) {
			return PrincipalLLMRoleView{Name: item.Name, Model: item.Model}
		}
	}
	return PrincipalLLMRoleView{Name: name}
}

func firstConcreteRole(choice ownerBotLLM) *ownerLLMOverride {
	for _, role := range []*ownerLLMOverride{choice.Foreground, choice.Background, choice.ComputerUse, choice.Grounder} {
		if role == nil || role.Official || strings.TrimSpace(role.Name) == "" {
			continue
		}
		return role
	}
	return nil
}

func roleView(role *ownerLLMOverride, grounder bool) PrincipalLLMRoleView {
	if role == nil || (role.Official && strings.TrimSpace(role.Model) == "") || (!role.Official && strings.TrimSpace(role.Name) == "") {
		if grounder {
			return PrincipalLLMRoleView{Unused: true}
		}
		return PrincipalLLMRoleView{Inherit: true}
	}
	if role.Official {
		return PrincipalLLMRoleView{Official: true, Model: strings.TrimSpace(role.Model), KeySet: true}
	}
	return PrincipalLLMRoleView{
		Name:     role.Name,
		URL:      role.URL,
		Model:    role.Model,
		Protocol: role.Protocol,
		WireAPI:  role.WireAPI,
		KeySet:   strings.TrimSpace(role.Key) != "",
	}
}

// normalizeOwnerBotLLM turns one save into the stored choice.
// A body without Roles is the older single-provider endpoint.
func normalizeOwnerBotLLM(in PrincipalLLMInput, prev ownerBotLLM, providers []llmProvider) (ownerBotLLM, error) {
	mode := strings.ToLower(strings.TrimSpace(in.Mode))
	switch mode {
	case "", "default", "system":
		return ownerBotLLM{Mode: corelib.MaclawBotLLMModeDefault}, nil
	case "custom", "provider":
	default:
		return ownerBotLLM{}, fmt.Errorf("%w: llm mode must be default or custom", ErrInvalidInput)
	}
	if in.Roles == nil {
		one, err := normalizeCustomProvider(in, prev, prev.Foreground, providers)
		if err != nil {
			return ownerBotLLM{}, err
		}
		return ownerBotLLM{
			Mode:        corelib.MaclawBotLLMModeCustom,
			Foreground:  cloneOwnerLLMOverride(one),
			Background:  cloneOwnerLLMOverride(one),
			ComputerUse: cloneOwnerLLMOverride(one),
		}, nil
	}
	fg, err := normalizeOwnerLLMRole("foreground", in.Roles.Foreground, prev, providers, false)
	if err != nil {
		return ownerBotLLM{}, err
	}
	bg, err := normalizeOwnerLLMRole("background", in.Roles.Background, prev, providers, false)
	if err != nil {
		return ownerBotLLM{}, err
	}
	cu, err := normalizeOwnerLLMRole("computer_use", in.Roles.ComputerUse, prev, providers, false)
	if err != nil {
		return ownerBotLLM{}, err
	}
	gr, err := normalizeOwnerLLMRole("grounder", in.Roles.Grounder, prev, providers, true)
	if err != nil {
		return ownerBotLLM{}, err
	}
	if err := consistentRoleProviders(fg, bg, cu, gr); err != nil {
		return ownerBotLLM{}, err
	}
	return ownerBotLLM{
		Mode:        corelib.MaclawBotLLMModeCustom,
		Foreground:  fg,
		Background:  bg,
		ComputerUse: cu,
		Grounder:    gr,
	}, nil
}

func normalizeOwnerLLMRole(role string, in PrincipalLLMRoleInput, prev ownerBotLLM, providers []llmProvider, grounder bool) (*ownerLLMOverride, error) {
	if in.Inherit || in.Unused || (grounder && !in.Official && strings.TrimSpace(in.Name) == "" && strings.TrimSpace(in.Model) == "") {
		if !grounder && in.Unused {
			return nil, fmt.Errorf("%w: only the grounding model can be unused", ErrInvalidInput)
		}
		return nil, nil
	}
	if !grounder && strings.TrimSpace(in.Name) == "" && !in.Official && strings.TrimSpace(in.Model) == "" {
		return nil, nil
	}
	if in.Official {
		model := strings.TrimSpace(in.Model)
		if model == "" || len(model) > 128 || strings.ContainsAny(model, "\r\n") {
			return nil, fmt.Errorf("%w: llm provider model is required", ErrInvalidInput)
		}
		return &ownerLLMOverride{Official: true, Model: model}, nil
	}
	one, err := normalizeCustomProvider(PrincipalLLMInput{
		Mode: "custom", Name: in.Name, URL: in.URL, Key: in.Key, Model: in.Model,
		Protocol: in.Protocol, WireAPI: in.WireAPI,
	}, prev, roleOverride(prev, role), providers)
	if err != nil {
		return nil, err
	}
	return one, nil
}

func roleOverride(choice ownerBotLLM, role string) *ownerLLMOverride {
	switch role {
	case "background":
		return choice.Background
	case "computer_use":
		return choice.ComputerUse
	case "grounder":
		return choice.Grounder
	default:
		return choice.Foreground
	}
}

// storedProviderKey is a key Hub already holds for this provider name on any
// role. A later save can move that provider between roles without sending the
// key again.
func storedProviderKey(choice ownerBotLLM, name string) string {
	for _, role := range []*ownerLLMOverride{choice.Foreground, choice.Background, choice.ComputerUse, choice.Grounder} {
		if role == nil || role.Official || !corelib.MaclawLLMProviderNameEqual(role.Name, name) {
			continue
		}
		if key := strings.TrimSpace(role.Key); key != "" {
			return key
		}
	}
	return ""
}

func consistentRoleProviders(roles ...*ownerLLMOverride) error {
	seen := map[string]*ownerLLMOverride{}
	for _, role := range roles {
		if role == nil || role.Official {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(role.Name))
		prev, ok := seen[key]
		if !ok {
			seen[key] = role
			continue
		}
		if prev.URL != role.URL || prev.Key != role.Key || prev.Protocol != role.Protocol || prev.WireAPI != role.WireAPI {
			return fmt.Errorf("%w: llm provider %s is inconsistent across roles", ErrInvalidInput, role.Name)
		}
	}
	return nil
}

func ownerBotLLMStamp(choice ownerBotLLM) string {
	if choice.Mode != corelib.MaclawBotLLMModeCustom {
		return ""
	}
	hash := sha256.New()
	for _, role := range []*ownerLLMOverride{choice.Foreground, choice.Background, choice.ComputerUse, choice.Grounder} {
		_, _ = io.WriteString(hash, principalLLMStamp(role)+"\n")
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// roleProfilePin is the MaClawSrv maclaw_llm_profiles entry for one custom role.
// The id is the legacy name id so it matches the provider row this push writes
// even when that row has no separate durable id.
func roleProfilePin(role *ownerLLMOverride) map[string]any {
	name := botLLMProviderName
	model := ""
	if role != nil {
		model = strings.TrimSpace(role.Model)
		if !role.Official && strings.TrimSpace(role.Name) != "" {
			name = strings.TrimSpace(role.Name)
		}
	}
	return map[string]any{
		"inherit_hub": false,
		"provider_id": corelib.MaclawLLMLegacyProviderID(name),
		"model":       model,
	}
}

func customRoleProfiles(existing any, choice ownerBotLLM) map[string]any {
	profiles := map[string]any{}
	if raw, ok := existing.(map[string]any); ok {
		for key, value := range raw {
			profiles[key] = value
		}
	}
	setRolePin(profiles, "foreground", choice.Foreground)
	setRolePin(profiles, "background", choice.Background)
	setRolePin(profiles, "computer_use", choice.ComputerUse)
	setRolePin(profiles, "grounder", choice.Grounder)
	return profiles
}

func setRolePin(profiles map[string]any, key string, role *ownerLLMOverride) {
	if role == nil {
		delete(profiles, key)
		return
	}
	profiles[key] = roleProfilePin(role)
}

func clearRoleProfiles(app map[string]any) {
	raw, ok := app["maclaw_llm_profiles"].(map[string]any)
	if !ok || raw == nil {
		return
	}
	next := map[string]any{}
	for key, value := range raw {
		next[key] = value
	}
	delete(next, "foreground")
	delete(next, "background")
	delete(next, "computer_use")
	delete(next, "grounder")
	if len(next) == 0 {
		delete(app, "maclaw_llm_profiles")
		return
	}
	app["maclaw_llm_profiles"] = next
}

func appendCustomRoleProviders(providers []map[string]any, choice ownerBotLLM) []map[string]any {
	seen := map[string]struct{}{}
	for _, entry := range providers {
		if name, _ := entry["name"].(string); strings.TrimSpace(name) != "" {
			seen[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
		}
	}
	for _, role := range []*ownerLLMOverride{choice.Foreground, choice.Background, choice.ComputerUse, choice.Grounder} {
		if role == nil || role.Official {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(role.Name))
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		entry := map[string]any{
			"id":       corelib.MaclawLLMLegacyProviderID(role.Name),
			"name":     role.Name,
			"url":      role.URL,
			"key":      role.Key,
			"model":    role.Model,
			"protocol": role.Protocol,
		}
		if strings.TrimSpace(role.WireAPI) != "" {
			entry["wire_api"] = role.WireAPI
		}
		providers = append(providers, entry)
	}
	return providers
}

func applyServiceBotLLMFlat(app map[string]any, settings llmSettings, endpoint, viewerToken string) {
	selected, custom := selectedBotLLMProvider(settings)
	if custom {
		app["maclaw_llm_url"] = selected.URL
		app["maclaw_llm_key"] = selected.Key
		app["maclaw_llm_model"] = selected.Model
		app["maclaw_llm_protocol"] = selected.Protocol
		app["maclaw_llm_current_provider"] = selected.Name
		return
	}
	app["maclaw_llm_url"] = endpoint
	app["maclaw_llm_key"] = viewerToken
	app["maclaw_llm_model"] = botLLMModel
	delete(app, "maclaw_llm_protocol")
	app["maclaw_llm_current_provider"] = botLLMProviderName
}

func applyCustomFlat(app map[string]any, choice ownerBotLLM, endpoint, viewerToken string) {
	fg := choice.Foreground
	if fg == nil || fg.Official {
		model := botLLMModel
		if fg != nil && strings.TrimSpace(fg.Model) != "" {
			model = strings.TrimSpace(fg.Model)
		}
		app["maclaw_llm_url"] = endpoint
		app["maclaw_llm_key"] = viewerToken
		app["maclaw_llm_model"] = model
		delete(app, "maclaw_llm_protocol")
		app["maclaw_llm_current_provider"] = botLLMProviderName
		return
	}
	app["maclaw_llm_url"] = fg.URL
	app["maclaw_llm_key"] = fg.Key
	app["maclaw_llm_model"] = fg.Model
	app["maclaw_llm_protocol"] = fg.Protocol
	app["maclaw_llm_current_provider"] = fg.Name
}
