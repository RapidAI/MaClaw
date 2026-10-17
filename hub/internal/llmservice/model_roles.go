package llmservice

import "strings"

const (
	ModelRoleForeground  = "foreground"
	ModelRoleBackground  = "background"
	ModelRoleComputerUse = "computer_use"
	ModelRoleAuto        = "auto"
)

// ModelRoles names the model each desktop loop should use.
// An empty field is not a pin. "default" is stored as auto.
type ModelRoles struct {
	Foreground  string `json:"foreground,omitempty"`
	Background  string `json:"background,omitempty"`
	ComputerUse string `json:"computer_use,omitempty"`
}

func (m ModelRoles) normalized() ModelRoles {
	return ModelRoles{
		Foreground:  normalizeRoleModel(m.Foreground),
		Background:  normalizeRoleModel(m.Background),
		ComputerUse: normalizeRoleModel(m.ComputerUse),
	}
}

func (m ModelRoles) empty() bool {
	return m.Foreground == "" && m.Background == "" && m.ComputerUse == ""
}

func normalizeRoleModel(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return ""
	}
	if strings.EqualFold(model, ModelRoleAuto) || strings.EqualFold(model, "default") {
		return ModelRoleAuto
	}
	return model
}

// mergeModelRoles lets a later non-empty pin replace an earlier one.
func mergeModelRoles(base, extra ModelRoles) ModelRoles {
	if extra.Foreground != "" {
		base.Foreground = extra.Foreground
	}
	if extra.Background != "" {
		base.Background = extra.Background
	}
	if extra.ComputerUse != "" {
		base.ComputerUse = extra.ComputerUse
	}
	return base
}

// ResolveEffectiveModelRoles fills the three loop roles.
// A user pin wins, then a tenant pin. An empty role uses the single configured
// model when the catalog has exactly one, and auto otherwise. auto is the
// server-side workload route; it is not a fixed model id.
func ResolveEffectiveModelRoles(defaults, user ModelRoles, defaultModel string, authorizedModelCount int) ModelRoles {
	defaults = defaults.normalized()
	user = user.normalized()
	fallback := roleModelFallback(defaultModel, authorizedModelCount)
	pick := func(userVal, defaultVal string) string {
		if userVal != "" {
			return userVal
		}
		if defaultVal != "" {
			return defaultVal
		}
		return fallback
	}
	return ModelRoles{
		Foreground:  pick(user.Foreground, defaults.Foreground),
		Background:  pick(user.Background, defaults.Background),
		ComputerUse: pick(user.ComputerUse, defaults.ComputerUse),
	}
}

func roleModelFallback(defaultModel string, authorizedModelCount int) string {
	model := normalizeRoleModel(defaultModel)
	if model == "" || model == ModelRoleAuto {
		return ModelRoleAuto
	}
	// One configured model is the historical assignment. More than one model
	// must stay on auto until an admin or user pins a role.
	if authorizedModelCount <= 1 {
		return model
	}
	return ModelRoleAuto
}

func modelRolesForOwner(reg *Registry, owner userAccountRef) ModelRoles {
	if reg == nil || owner.empty() {
		return ModelRoles{}
	}
	var roles ModelRoles
	for _, binding := range reg.UserBindings {
		if bindingMatchesUser(binding, owner) {
			roles = mergeModelRoles(roles, binding.ModelRoles.normalized())
		}
	}
	return roles
}

// FindUserBinding returns the merged binding for this account, if one exists.
func (r *Registry) FindUserBinding(userID, email string) (UserBinding, bool) {
	if r == nil {
		return UserBinding{}, false
	}
	owner := newUserAccountRef(userID, email)
	if owner.empty() {
		return UserBinding{}, false
	}
	var found UserBinding
	ok := false
	for _, binding := range r.UserBindings {
		if !bindingMatchesUser(binding, owner) {
			continue
		}
		if !ok {
			found = binding
			ok = true
			continue
		}
		found.ServiceGroupIDs = normalizeStringSlice(append(append([]string{}, found.ServiceGroupIDs...), binding.ServiceGroupIDs...))
		found.ModelRoles = mergeModelRoles(found.ModelRoles, binding.ModelRoles.normalized())
		if found.Email == "" {
			found.Email = binding.Email
		}
		if found.UserID == "" {
			found.UserID = binding.UserID
		}
	}
	return found, ok
}

// PreserveOmittedDefaultModelRoles keeps the tenant role pins when an older
// admin client saves a registry that does not know the field.
func PreserveOmittedDefaultModelRoles(req, old *Registry, provided bool) {
	if provided || req == nil || old == nil {
		return
	}
	req.DefaultModelRoles = old.DefaultModelRoles
}

// PreserveOmittedUserModelRoles copies stored role pins onto incoming bindings
// whose JSON object omitted model_roles. modelRolesProvided is parallel to
// req.UserBindings; a nil or mismatched slice leaves the request unchanged.
func PreserveOmittedUserModelRoles(req, old *Registry, modelRolesProvided []bool) {
	if req == nil || old == nil || len(modelRolesProvided) != len(req.UserBindings) {
		return
	}
	for i := range req.UserBindings {
		if modelRolesProvided[i] {
			continue
		}
		prev, ok := old.FindUserBinding(req.UserBindings[i].UserID, req.UserBindings[i].Email)
		if !ok {
			continue
		}
		req.UserBindings[i].ModelRoles = prev.ModelRoles
	}
}
