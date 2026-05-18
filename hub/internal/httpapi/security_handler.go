package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/security"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func securityRequestContext(r *http.Request) context.Context {
	return security.WithTenant(r.Context(), RequestTenantID(r))
}

// SecurityGroupsRootHandler returns only the root node for lazy tree loading.
// GET /api/admin/security/groups/root
func SecurityGroupsRootHandler(svc *security.SecurityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := securityRequestContext(r)
		node, err := svc.GetRootGroupNode(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "TREE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"root": node})
	}
}

// SecurityGroupsHandler returns the complete group tree.
// GET /api/admin/security/groups
func SecurityGroupsHandler(svc *security.SecurityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := securityRequestContext(r)
		tree, err := svc.GetGroupTree(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "TREE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"tree": tree})
	}
}

// CreateSecurityGroupHandler creates a child group.
// POST /api/admin/security/groups
func CreateSecurityGroupHandler(svc *security.SecurityService, audits ...store.AdminAuditRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := securityRequestContext(r)
		audit := firstAdminAuditRepo(audits...)
		var req struct {
			Name     string `json:"name"`
			ParentID string `json:"parent_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if req.Name == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "name is required")
			return
		}
		if req.ParentID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "parent_id is required")
			return
		}

		group, err := svc.CreateGroup(ctx, req.Name, req.ParentID)
		if err != nil {
			if strings.Contains(err.Error(), "parent group not found") {
				writeError(w, http.StatusNotFound, "NOT_FOUND", "parent group not found")
				return
			}
			if strings.Contains(err.Error(), "depth exceeds") {
				writeError(w, http.StatusBadRequest, "DEPTH_EXCEEDED", err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "CREATE_FAILED", err.Error())
			return
		}
		writeAdminAuditLog(r.Context(), audit, adminAuditUserID(r), "security.group.create", map[string]any{"group_id": group.ID, "name": req.Name, "parent_id": req.ParentID})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "group": group})
	}
}

// UpdateSecurityGroupHandler renames a group.
// PUT /api/admin/security/groups/{id}
func UpdateSecurityGroupHandler(svc *security.SecurityService, audits ...store.AdminAuditRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := securityRequestContext(r)
		audit := firstAdminAuditRepo(audits...)
		id := r.PathValue("id")
		if id == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "id is required")
			return
		}
		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if req.Name == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "name is required")
			return
		}

		if err := svc.RenameGroup(ctx, id, req.Name); err != nil {
			writeError(w, http.StatusInternalServerError, "RENAME_FAILED", err.Error())
			return
		}
		writeAdminAuditLog(r.Context(), audit, adminAuditUserID(r), "security.group.rename", map[string]any{"group_id": id, "name": req.Name})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// DeleteSecurityGroupHandler deletes a group.
// DELETE /api/admin/security/groups/{id}
func DeleteSecurityGroupHandler(svc *security.SecurityService, audits ...store.AdminAuditRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := securityRequestContext(r)
		audit := firstAdminAuditRepo(audits...)
		id := r.PathValue("id")
		if id == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "id is required")
			return
		}

		if err := svc.DeleteGroup(ctx, id); err != nil {
			if strings.Contains(err.Error(), "cannot delete root group") {
				writeError(w, http.StatusForbidden, "FORBIDDEN", "cannot delete root group")
				return
			}
			writeError(w, http.StatusInternalServerError, "DELETE_FAILED", err.Error())
			return
		}
		writeAdminAuditLog(r.Context(), audit, adminAuditUserID(r), "security.group.delete", map[string]any{"group_id": id})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// ListGroupMembersHandler returns the member emails and direct child groups for a group.
// GET /api/admin/security/groups/{id}/members
func ListGroupMembersHandler(svc *security.SecurityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := securityRequestContext(r)
		id := r.PathValue("id")
		if id == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "id is required")
			return
		}

		members, err := svc.ListGroupMembers(ctx, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_MEMBERS_FAILED", err.Error())
			return
		}
		children, err := svc.GetGroupChildren(ctx, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_CHILDREN_FAILED", err.Error())
			return
		}
		if members == nil {
			members = []string{}
		}
		if children == nil {
			children = []*security.GroupTreeNode{}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"members":  members,
			"children": children,
		})
	}
}

// AddGroupMemberHandler assigns a user to a group.
// POST /api/admin/security/groups/{id}/members
func AddGroupMemberHandler(svc *security.SecurityService, audits ...store.AdminAuditRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := securityRequestContext(r)
		audit := firstAdminAuditRepo(audits...)
		id := r.PathValue("id")
		if id == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "id is required")
			return
		}
		var req struct {
			Email string `json:"email"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if req.Email == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "email is required")
			return
		}

		if err := svc.AssignUser(ctx, req.Email, id); err != nil {
			writeError(w, http.StatusInternalServerError, "ASSIGN_FAILED", err.Error())
			return
		}
		writeAdminAuditLog(r.Context(), audit, adminAuditUserID(r), "security.group.member.add", map[string]any{"group_id": id, "email": req.Email})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// RemoveGroupMemberHandler removes a user from a group (back to Root_Group).
// DELETE /api/admin/security/groups/{id}/members/{email}
func RemoveGroupMemberHandler(svc *security.SecurityService, audits ...store.AdminAuditRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := securityRequestContext(r)
		audit := firstAdminAuditRepo(audits...)
		id := r.PathValue("id")
		email := r.PathValue("email")
		if id == "" || email == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "id and email are required")
			return
		}

		if err := svc.RemoveUser(ctx, id, email); err != nil {
			writeError(w, http.StatusInternalServerError, "REMOVE_FAILED", err.Error())
			return
		}
		writeAdminAuditLog(r.Context(), audit, adminAuditUserID(r), "security.group.member.remove", map[string]any{"group_id": id, "email": email})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// GetGroupPolicyHandler returns the policy view for a group (with inheritance info).
// GET /api/admin/security/groups/{id}/policy
func GetGroupPolicyHandler(svc *security.SecurityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := securityRequestContext(r)
		id := r.PathValue("id")
		if id == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "id is required")
			return
		}

		view, err := svc.GetGroupPolicy(ctx, id)
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				writeError(w, http.StatusNotFound, "NOT_FOUND", "group not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "POLICY_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, view)
	}
}

// UpdateGroupPolicyHandler updates the sparse policy for a group.
// PUT /api/admin/security/groups/{id}/policy
func UpdateGroupPolicyHandler(svc *security.SecurityService, audits ...store.AdminAuditRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := securityRequestContext(r)
		audit := firstAdminAuditRepo(audits...)
		id := r.PathValue("id")
		if id == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "id is required")
			return
		}
		var req struct {
			Policy map[string]interface{} `json:"policy"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if req.Policy == nil {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "policy is required")
			return
		}

		if err := svc.UpdateGroupPolicy(ctx, id, req.Policy); err != nil {
			writeError(w, http.StatusInternalServerError, "UPDATE_POLICY_FAILED", err.Error())
			return
		}
		writeAdminAuditLog(r.Context(), audit, adminAuditUserID(r), "security.group.policy.update", map[string]any{"group_id": id, "policy": req.Policy})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// GetUserEffectivePolicyHandler returns the effective policy for a user.
// GET /api/admin/security/users/{email}/effective-policy
func GetUserEffectivePolicyHandler(svc *security.SecurityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := securityRequestContext(r)
		email := r.PathValue("email")
		if email == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "email is required")
			return
		}

		groupID, groupPath, policy, groupPolicy, err := svc.GetUserPolicyView(ctx, email)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "POLICY_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"policy": policy, "group_id": groupID, "group_path": groupPath, "group_policy": groupPolicy})
	}
}

// GetSecuritySettingsHandler returns the system security settings.
// GET /api/admin/security/settings
func GetSecuritySettingsHandler(svc *security.SecurityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := securityRequestContext(r)
		settings, err := svc.GetSettings(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "SETTINGS_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, settings)
	}
}

// UpdateSecuritySettingsHandler updates the system security settings.
// PUT /api/admin/security/settings
func UpdateSecuritySettingsHandler(svc *security.SecurityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := securityRequestContext(r)
		var req security.SecuritySettings
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}

		adminUserID := "admin"
		if admin := AdminFromContext(r.Context()); admin != nil {
			adminUserID = admin.ID
		}

		if err := svc.UpdateSettings(ctx, &req, adminUserID); err != nil {
			writeError(w, http.StatusInternalServerError, "UPDATE_SETTINGS_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// SetDefaultGroupHandler sets the default group for new users.
// PUT /api/admin/security/settings/default-group
func SetDefaultGroupHandler(svc *security.SecurityService, audits ...store.AdminAuditRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := securityRequestContext(r)
		audit := firstAdminAuditRepo(audits...)
		var req struct {
			GroupID string `json:"group_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if req.GroupID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "group_id is required")
			return
		}

		if err := svc.SetDefaultGroup(ctx, req.GroupID); err != nil {
			if strings.Contains(err.Error(), "default group not found") {
				writeError(w, http.StatusBadRequest, "NOT_FOUND", "default group not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "SET_DEFAULT_FAILED", err.Error())
			return
		}
		writeAdminAuditLog(r.Context(), audit, adminAuditUserID(r), "security.default_group.update", map[string]any{"group_id": req.GroupID})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// EnrollGroupTreeHandler returns the group tree for enrollment (public endpoint).
// GET /api/enroll/group-tree
// When org_structure_enabled is false, returns empty children array.
func EnrollGroupTreeHandler(svc *security.SecurityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		settings, err := svc.GetSettings(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "SETTINGS_FAILED", err.Error())
			return
		}

		if !settings.OrgStructureEnabled {
			writeJSON(w, http.StatusOK, map[string]any{
				"org_structure_enabled": false,
				"groups":                []any{},
			})
			return
		}

		tree, err := svc.GetGroupTree(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "TREE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"org_structure_enabled": true,
			"tree":                  tree,
		})
	}
}
