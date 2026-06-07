package httpapi

import (
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/device"
	"github.com/RapidAI/CodeClaw/hub/internal/security"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// WorkflowApproverDirectoryHandler returns tenant-scoped organization data for
// the workflow designer approver picker. It is machine-authenticated by the
// workflow API middleware, not admin-authenticated, so workflow authors can pick
// approvers without opening the Hub admin console.
func WorkflowApproverDirectoryHandler(securitySvc *security.SecurityService, identity *auth.IdentityService, devices *device.Service, system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if securitySvc == nil || identity == nil || devices == nil || system == nil {
			writeError(w, http.StatusServiceUnavailable, "DIRECTORY_UNAVAILABLE", "approver directory is not configured")
			return
		}

		tenantID, ok := store.TenantIDFromContextIfPresent(r.Context())
		if !ok {
			tenantID = RequestTenantID(r)
		}

		ctx := security.WithTenant(r.Context(), tenantID)
		tree, err := securitySvc.GetGroupTree(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "TREE_FAILED", err.Error())
			return
		}

		membersByGroup := map[string][]string{}
		for _, groupID := range workflowDirectoryGroupIDs(tree) {
			members, err := securitySvc.ListGroupMembers(ctx, groupID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "LIST_MEMBERS_FAILED", err.Error())
				return
			}
			if members == nil {
				members = []string{}
			}
			membersByGroup[groupID] = members
		}

		users, err := identity.ListUsersForTenant(r.Context(), tenantID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_USERS_FAILED", err.Error())
			return
		}
		userViews := make([]map[string]any, 0, len(users))
		for _, user := range users {
			if user == nil || strings.TrimSpace(user.Email) == "" {
				continue
			}
			userViews = append(userViews, map[string]any{
				"id":                user.ID,
				"tenant_id":         store.NormalizeTenantID(user.TenantID),
				"email":             user.Email,
				"sn":                user.SN,
				"status":            user.Status,
				"enrollment_status": user.EnrollmentStatus,
			})
		}

		machines, err := devices.ListMachinesByTenant(r.Context(), tenantID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_MACHINES_FAILED", err.Error())
			return
		}

		tenantSystem := scopedSystemSettingsForTenant(tenantID, system)
		registry := loadVERegistry(r.Context(), tenantSystem)
		enrichVERegistryOwners(r.Context(), &registry, identity.UsersRepo())
		enrichVERegistryEmployeeTypes(&registry)
		runtimePresence := emptyMacLawSrvRuntimePresence()
		if veRegistryHasMacLawSrvRuntimeEmployees(registry, true) {
			runtimePresence = loadMacLawSrvRuntimePresence(r.Context(), globalSystemSettings(system), tenantID)
		}
		employees := make([]digitalEmployeeEntry, 0, len(registry.Employees))
		for _, entry := range registry.Employees {
			entry = applyVEDiscoverablePresence(r.Context(), entry, devices, runtimePresence)
			if strings.EqualFold(strings.TrimSpace(entry.Status), veStatusActive) {
				employees = append(employees, entry)
			}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"tenant_id":        tenantID,
			"tree":             tree,
			"members_by_group": membersByGroup,
			"users":            userViews,
			"machines":         enrichMachineList(r.Context(), machines, identity.UsersRepo()),
			"employees":        employees,
		})
	}
}

func workflowDirectoryGroupIDs(root *security.GroupTreeNode) []string {
	ids := []string{}
	var walk func(*security.GroupTreeNode)
	walk = func(node *security.GroupTreeNode) {
		if node == nil || strings.TrimSpace(node.ID) == "" {
			return
		}
		ids = append(ids, node.ID)
		for _, child := range node.Children {
			walk(child)
		}
	}
	walk(root)
	return ids
}
