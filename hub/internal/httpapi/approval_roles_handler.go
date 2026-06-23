package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const approvalRolesSettingsKey = "approval_roles_v1"

type approvalRoleStore struct {
	Roles          []approvalRoleRecord    `json:"roles"`
	FunctionScopes []approvalFunctionScope `json:"functionScopes,omitempty"`
	UpdatedAt      string                  `json:"updated_at,omitempty"`
}

type approvalRoleRecord struct {
	ID            string                 `json:"id"`
	View          string                 `json:"view"`
	ScopeType     string                 `json:"scopeType"`
	ScopeID       string                 `json:"scopeId"`
	ScopeName     string                 `json:"scopeName"`
	RoleCode      string                 `json:"roleCode"`
	RoleName      string                 `json:"roleName"`
	ExecutionMode string                 `json:"executionMode"`
	Assignees     []approvalRoleAssignee `json:"assignees"`
}

type approvalFunctionScope struct {
	ScopeID   string `json:"scopeId"`
	ScopeName string `json:"scopeName"`
	Custom    bool   `json:"custom,omitempty"`
}

type approvalRoleAssignee struct {
	SubjectType string `json:"subjectType"`
	SubjectID   string `json:"subjectId"`
	DisplayName string `json:"displayName"`
}

func GetApprovalRolesHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		roles, err := loadApprovalRoles(r, system)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "APPROVAL_ROLES_LOAD_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, roles)
	}
}

func UpdateApprovalRolesHandler(system store.SystemSettingsRepository, audits ...store.AdminAuditRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req approvalRoleStore
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		next, err := normalizeApprovalRoleStore(req)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_APPROVAL_ROLES", err.Error())
			return
		}
		next.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		if err := saveApprovalRoles(r, system, next); err != nil {
			writeError(w, http.StatusInternalServerError, "APPROVAL_ROLES_SAVE_FAILED", err.Error())
			return
		}
		writeAdminAuditLog(r.Context(), firstAdminAuditRepo(audits...), adminAuditUserID(r), "security.approval_roles.update", map[string]any{"role_count": len(next.Roles)})
		writeJSON(w, http.StatusOK, next)
	}
}

func loadApprovalRoles(r *http.Request, system store.SystemSettingsRepository) (approvalRoleStore, error) {
	if system == nil {
		return approvalRoleStore{Roles: []approvalRoleRecord{}}, nil
	}
	system = scopedSystemSettingsForRequest(r, system)
	raw, err := system.Get(r.Context(), approvalRolesSettingsKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return approvalRoleStore{Roles: []approvalRoleRecord{}}, nil
	}
	var saved approvalRoleStore
	if err := json.Unmarshal([]byte(raw), &saved); err != nil {
		return approvalRoleStore{}, fmt.Errorf("unmarshal approval roles: %w", err)
	}
	return normalizeApprovalRoleStore(saved)
}

func loadApprovalRolesForTenant(r *http.Request, system store.SystemSettingsRepository, tenantID string) (approvalRoleStore, error) {
	if r == nil {
		return loadApprovalRolesForTenantContext(context.Background(), system, tenantID)
	}
	return loadApprovalRolesForTenantContext(r.Context(), system, tenantID)
}

func loadApprovalRolesForTenantContext(ctx context.Context, system store.SystemSettingsRepository, tenantID string) (approvalRoleStore, error) {
	if system == nil {
		return approvalRoleStore{Roles: []approvalRoleRecord{}}, nil
	}
	tenantSystem := scopedSystemSettingsForTenant(tenantID, system)
	raw, err := tenantSystem.Get(ctx, approvalRolesSettingsKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return approvalRoleStore{Roles: []approvalRoleRecord{}}, nil
	}
	var saved approvalRoleStore
	if err := json.Unmarshal([]byte(raw), &saved); err != nil {
		return approvalRoleStore{}, fmt.Errorf("unmarshal approval roles: %w", err)
	}
	return normalizeApprovalRoleStore(saved)
}

func saveApprovalRoles(r *http.Request, system store.SystemSettingsRepository, roles approvalRoleStore) error {
	if system == nil {
		return fmt.Errorf("system settings are not configured")
	}
	system = scopedSystemSettingsForRequest(r, system)
	data, err := json.Marshal(roles)
	if err != nil {
		return fmt.Errorf("marshal approval roles: %w", err)
	}
	return system.Set(r.Context(), approvalRolesSettingsKey, string(data))
}

func normalizeApprovalRoleStore(saved approvalRoleStore) (approvalRoleStore, error) {
	out := approvalRoleStore{Roles: []approvalRoleRecord{}, FunctionScopes: []approvalFunctionScope{}, UpdatedAt: strings.TrimSpace(saved.UpdatedAt)}
	scopeSeen := map[string]struct{}{}
	scopeNameSeen := map[string]struct{}{}
	for _, scope := range saved.FunctionScopes {
		normalized, ok := normalizeApprovalFunctionScope(scope)
		if !ok {
			continue
		}
		nameKey := strings.ToLower(strings.TrimSpace(normalized.ScopeName))
		if _, exists := scopeSeen[normalized.ScopeID]; exists {
			continue
		}
		if nameKey != "" {
			if _, exists := scopeNameSeen[nameKey]; exists {
				continue
			}
			scopeNameSeen[nameKey] = struct{}{}
		}
		scopeSeen[normalized.ScopeID] = struct{}{}
		out.FunctionScopes = append(out.FunctionScopes, normalized)
	}
	if len(out.FunctionScopes) > 80 {
		return approvalRoleStore{}, fmt.Errorf("too many approval function scopes")
	}
	seen := map[string]struct{}{}
	for _, role := range saved.Roles {
		normalized, ok := normalizeApprovalRoleRecord(role)
		if !ok {
			continue
		}
		if _, exists := seen[normalized.ID]; exists {
			continue
		}
		seen[normalized.ID] = struct{}{}
		out.Roles = append(out.Roles, normalized)
	}
	if len(out.Roles) > 200 {
		return approvalRoleStore{}, fmt.Errorf("too many approval roles")
	}
	return out, nil
}

func normalizeApprovalFunctionScope(scope approvalFunctionScope) (approvalFunctionScope, bool) {
	scopeID := strings.TrimSpace(scope.ScopeID)
	scopeName := strings.TrimSpace(scope.ScopeName)
	if scopeID == "" {
		scopeID = approvalScopeCodeFromName(scopeName)
	}
	if scopeName == "" {
		scopeName = scopeID
	}
	if scopeID == "" || scopeName == "" {
		return approvalFunctionScope{}, false
	}
	return approvalFunctionScope{
		ScopeID:   scopeID,
		ScopeName: scopeName,
		Custom:    scope.Custom,
	}, true
}

func approvalScopeCodeFromName(name string) string {
	raw := strings.TrimSpace(name)
	if raw == "" {
		return ""
	}
	name = strings.ToLower(raw)
	var b strings.Builder
	prevUnderscore := false
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevUnderscore = false
			continue
		}
		if b.Len() > 0 && !prevUnderscore {
			b.WriteByte('_')
			prevUnderscore = true
		}
	}
	code := strings.Trim(b.String(), "_")
	if code == "" {
		code = fmt.Sprintf("function_%08x", approvalScopeHash(raw))
	}
	if code[0] < 'a' || code[0] > 'z' {
		code = "function_" + code
	}
	return code
}
func approvalScopeHash(value string) uint32 {
	var hash uint32 = 2166136261
	for _, r := range strings.TrimSpace(value) {
		hash ^= uint32(r)
		hash *= 16777619
	}
	return hash
}

func normalizeApprovalRoleRecord(role approvalRoleRecord) (approvalRoleRecord, bool) {
	scopeType := strings.TrimSpace(role.ScopeType)
	if scopeType == "" {
		scopeType = "global"
	}
	scopeID := strings.TrimSpace(role.ScopeID)
	if scopeID == "" {
		scopeID = "global"
	}
	roleCode := strings.TrimSpace(role.RoleCode)
	roleName := strings.TrimSpace(role.RoleName)
	if roleName == "" {
		roleName = roleCode
	}
	if roleCode == "" || roleName == "" {
		return approvalRoleRecord{}, false
	}
	view := strings.TrimSpace(role.View)
	if view == "" {
		view = "organization"
		if scopeType == "function" {
			view = "function"
		}
	}
	executionMode := strings.TrimSpace(role.ExecutionMode)
	if executionMode == "" {
		executionMode = "manual"
	}
	scopeName := strings.TrimSpace(role.ScopeName)
	if scopeName == "" {
		scopeName = scopeID
	}
	assignees := make([]approvalRoleAssignee, 0, len(role.Assignees))
	for _, assignee := range role.Assignees {
		subjectID := strings.TrimSpace(assignee.SubjectID)
		displayName := strings.TrimSpace(assignee.DisplayName)
		if subjectID == "" {
			subjectID = displayName
		}
		if displayName == "" {
			displayName = subjectID
		}
		if subjectID == "" {
			continue
		}
		subjectType := strings.TrimSpace(assignee.SubjectType)
		if subjectType == "" {
			subjectType = "user"
		}
		assignees = append(assignees, approvalRoleAssignee{
			SubjectType: subjectType,
			SubjectID:   subjectID,
			DisplayName: displayName,
		})
	}
	return approvalRoleRecord{
		ID:            approvalRoleID(scopeType, scopeID, roleCode),
		View:          view,
		ScopeType:     scopeType,
		ScopeID:       scopeID,
		ScopeName:     scopeName,
		RoleCode:      roleCode,
		RoleName:      roleName,
		ExecutionMode: executionMode,
		Assignees:     assignees,
	}, true
}

func approvalRoleID(scopeType, scopeID, roleCode string) string {
	return "role:" + urlComponent(scopeType) + ":" + urlComponent(scopeID) + ":" + urlComponent(roleCode)
}

func urlComponent(value string) string {
	return url.PathEscape(strings.TrimSpace(value))
}
