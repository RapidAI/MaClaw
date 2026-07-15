package llmservice

import (
	"strings"
)

// System-free is the reserved, free (no recharge) service group used by all
// server-side LLM callers (Hub agents, MaClawSrv agents, workflow draft, IM
// system LLM, config assistant, etc.).
const (
	SystemFreeServiceGroupID   = "system-free"
	SystemFreeServiceGroupName = "System Free"
	SystemFreeServiceGroupDesc = "System free LLM service group for Hub/MaClawSrv server-side agents. Binding is enough; no recharge required."
)

// IsSystemFreeServiceGroup reports whether id is the reserved system-free group.
func IsSystemFreeServiceGroup(id string) bool {
	return strings.EqualFold(strings.TrimSpace(id), SystemFreeServiceGroupID)
}

// IsProtectedServiceGroupID is true for groups that cannot be deleted by admins.
// "default" remains read-only legacy; system-free is protected but provider-editable.
func IsProtectedServiceGroupID(id string) bool {
	id = strings.TrimSpace(id)
	return IsBuiltinModelServiceGroupID(id) || IsSystemFreeServiceGroup(id) || IsBuiltinServiceGroup(id)
}

// SystemFreeTemplate returns the canonical system-free group (free policy,
// default model auto → maclaw_official).
func SystemFreeTemplate() ModelServiceGroup {
	return ModelServiceGroup{
		ID:           SystemFreeServiceGroupID,
		Name:         SystemFreeServiceGroupName,
		Description:  SystemFreeServiceGroupDesc,
		AccessPolicy: AccessPolicyFree,
		Models: []ModelServiceModel{{
			Name:        "auto",
			Description: "System default model route",
			ProviderIDs: []string{MaClawOfficialProviderID},
		}},
	}
}

// EnsureSystemFreeServiceGroup guarantees system-free exists, is free, has at
// least one model route, and is pinned as SystemDefaultServiceGroupID.
// Returns true when the registry was modified.
func EnsureSystemFreeServiceGroup(reg *Registry) bool {
	if reg == nil {
		return false
	}
	changed := false
	idx := -1
	for i := range reg.ModelServiceGroups {
		if IsSystemFreeServiceGroup(reg.ModelServiceGroups[i].ID) {
			idx = i
			break
		}
	}
	if idx < 0 {
		// Prepend so it is easy to find in admin UIs.
		reg.ModelServiceGroups = append([]ModelServiceGroup{SystemFreeTemplate()}, reg.ModelServiceGroups...)
		changed = true
		idx = 0
	} else {
		g := &reg.ModelServiceGroups[idx]
		// Force reserved id casing.
		if strings.TrimSpace(g.ID) != SystemFreeServiceGroupID {
			g.ID = SystemFreeServiceGroupID
			changed = true
		}
		if strings.TrimSpace(g.Name) == "" {
			g.Name = SystemFreeServiceGroupName
			changed = true
		}
		if NormalizeAccessPolicy(g.AccessPolicy) != AccessPolicyFree {
			g.AccessPolicy = AccessPolicyFree
			changed = true
		}
		if !systemFreeHasUsableModel(g) {
			if len(g.Models) == 0 {
				g.Models = SystemFreeTemplate().Models
				changed = true
			} else {
				// Repair first model with empty providers.
				m := &g.Models[0]
				if strings.TrimSpace(m.Name) == "" {
					m.Name = "auto"
					changed = true
				}
				if len(normalizeProviderIDs(m)) == 0 {
					m.ProviderIDs = []string{MaClawOfficialProviderID}
					m.ProviderConfigs = nil
					changed = true
				}
			}
		}
	}
	if !strings.EqualFold(strings.TrimSpace(reg.SystemDefaultServiceGroupID), SystemFreeServiceGroupID) {
		reg.SystemDefaultServiceGroupID = SystemFreeServiceGroupID
		changed = true
	}
	return changed
}

func systemFreeHasUsableModel(g *ModelServiceGroup) bool {
	if g == nil {
		return false
	}
	for i := range g.Models {
		if strings.TrimSpace(g.Models[i].Name) == "" {
			continue
		}
		if len(normalizeProviderIDs(&g.Models[i])) > 0 {
			return true
		}
	}
	return false
}

func normalizeProviderIDs(m *ModelServiceModel) []string {
	if m == nil {
		return nil
	}
	out := make([]string, 0, len(m.ProviderIDs)+len(m.ProviderConfigs))
	seen := map[string]struct{}{}
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		key := strings.ToLower(id)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, id)
	}
	for _, id := range m.ProviderIDs {
		add(id)
	}
	for _, cfg := range m.ProviderConfigs {
		add(cfg.ProviderID)
	}
	return out
}

// ProtectSystemFreeOnSave merges system-free invariants into next after an admin
// PUT. Prefer next's editable fields (name/desc/models) when present; never allow
// deletion or non-free policy; always pin SystemDefaultServiceGroupID.
func ProtectSystemFreeOnSave(next, old *Registry) {
	if next == nil {
		return
	}
	var incoming *ModelServiceGroup
	for i := range next.ModelServiceGroups {
		if IsSystemFreeServiceGroup(next.ModelServiceGroups[i].ID) {
			incoming = &next.ModelServiceGroups[i]
			break
		}
	}
	var previous *ModelServiceGroup
	if old != nil {
		previous = old.FindModelServiceGroup(SystemFreeServiceGroupID)
	}

	base := SystemFreeTemplate()
	if previous != nil {
		base = *previous
	}
	if incoming != nil {
		// Keep operator edits for name/description/models.
		if strings.TrimSpace(incoming.Name) != "" {
			base.Name = strings.TrimSpace(incoming.Name)
		}
		if strings.TrimSpace(incoming.Description) != "" {
			base.Description = strings.TrimSpace(incoming.Description)
		}
		if len(incoming.Models) > 0 {
			base.Models = append([]ModelServiceModel(nil), incoming.Models...)
		}
	}
	base.ID = SystemFreeServiceGroupID
	base.AccessPolicy = AccessPolicyFree
	if !systemFreeHasUsableModel(&base) {
		base.Models = SystemFreeTemplate().Models
	}

	// Replace or insert the protected group in next.
	found := false
	for i := range next.ModelServiceGroups {
		if IsSystemFreeServiceGroup(next.ModelServiceGroups[i].ID) {
			next.ModelServiceGroups[i] = base
			found = true
			break
		}
	}
	if !found {
		next.ModelServiceGroups = append([]ModelServiceGroup{base}, next.ModelServiceGroups...)
	}
	next.SystemDefaultServiceGroupID = SystemFreeServiceGroupID
}

// SystemFreeStatus is a readiness snapshot for admin UI / login gate.
type SystemFreeStatus struct {
	ServiceGroupID string              `json:"service_group_id"`
	Present        bool                `json:"present"`
	AccessPolicy   string              `json:"access_policy,omitempty"`
	Name           string              `json:"name,omitempty"`
	Description    string              `json:"description,omitempty"`
	Models         []ModelServiceModel `json:"models,omitempty"`
	ProviderIDs    []string            `json:"provider_ids,omitempty"`
	Ready          bool                `json:"ready"`
	Reasons        []string            `json:"reasons,omitempty"`
	IsDefault      bool                `json:"is_system_default"`
}

// EvaluateSystemFreeStatus checks structural readiness of system-free.
// configuredProviderIDs should include local provider ids; maclaw_official is
// always considered configured as a builtin route target.
func EvaluateSystemFreeStatus(reg *Registry, configuredProviderIDs map[string]struct{}) SystemFreeStatus {
	st := SystemFreeStatus{
		ServiceGroupID: SystemFreeServiceGroupID,
		IsDefault:      reg != nil && IsSystemFreeServiceGroup(reg.SystemDefaultServiceGroupID),
	}
	if reg == nil {
		st.Reasons = []string{"llm_service_registry_missing"}
		return st
	}
	g := reg.FindModelServiceGroup(SystemFreeServiceGroupID)
	if g == nil {
		st.Reasons = []string{"system_free_missing"}
		return st
	}
	st.Present = true
	st.AccessPolicy = NormalizeAccessPolicy(g.AccessPolicy)
	st.Name = g.Name
	st.Description = g.Description
	st.Models = append([]ModelServiceModel(nil), g.Models...)

	if st.AccessPolicy != AccessPolicyFree {
		st.Reasons = append(st.Reasons, "access_policy_not_free")
	}
	if !st.IsDefault {
		st.Reasons = append(st.Reasons, "not_system_default")
	}

	providerIDs := make([]string, 0)
	seen := map[string]struct{}{}
	hasRoutable := false
	for i := range g.Models {
		if strings.TrimSpace(g.Models[i].Name) == "" {
			continue
		}
		ids := normalizeProviderIDs(&g.Models[i])
		if len(ids) == 0 {
			continue
		}
		modelOK := false
		for _, pid := range ids {
			key := strings.ToLower(strings.TrimSpace(pid))
			if key == "" {
				continue
			}
			if _, ok := seen[key]; !ok {
				seen[key] = struct{}{}
				providerIDs = append(providerIDs, pid)
			}
			if IsBuiltinProvider(pid) {
				modelOK = true
				continue
			}
			if configuredProviderIDs != nil {
				if _, ok := configuredProviderIDs[key]; ok {
					modelOK = true
				}
			}
		}
		if modelOK {
			hasRoutable = true
		}
	}
	st.ProviderIDs = providerIDs
	if len(providerIDs) == 0 {
		st.Reasons = append(st.Reasons, "no_providers")
	} else if !hasRoutable {
		st.Reasons = append(st.Reasons, "providers_not_configured")
	}
	st.Ready = st.Present &&
		st.AccessPolicy == AccessPolicyFree &&
		st.IsDefault &&
		hasRoutable &&
		len(st.Reasons) == 0
	return st
}
