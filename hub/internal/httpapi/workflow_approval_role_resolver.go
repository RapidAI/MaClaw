package httpapi

import (
	"context"
	"net/url"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/device"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type workflowApprovalRoleResolver struct {
	system   store.SystemSettingsRepository
	identity *auth.IdentityService
	devices  *device.Service
}

func newWorkflowApprovalRoleResolver(system store.SystemSettingsRepository, identity *auth.IdentityService, devices *device.Service) *workflowApprovalRoleResolver {
	return &workflowApprovalRoleResolver{system: system, identity: identity, devices: devices}
}

func (r *workflowApprovalRoleResolver) ResolveApproverIDs(ctx context.Context, approverIDs []string) ([]string, error) {
	if r == nil {
		return normalizeWorkflowApproverIDs(approverIDs), nil
	}
	tenantID, ok := store.TenantIDFromContextIfPresent(ctx)
	if !ok {
		tenantID = store.DefaultTenantID
	}
	roles, err := loadApprovalRolesForTenantContext(ctx, r.system, tenantID)
	if err != nil {
		return nil, err
	}
	roleByID := make(map[string]approvalRoleRecord, len(roles.Roles))
	for _, role := range roles.Roles {
		roleByID[role.ID] = role
	}
	userMachineID := r.userMachineResolver(ctx, tenantID)
	out := make([]string, 0, len(approverIDs))
	seen := map[string]struct{}{}
	for _, id := range approverIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if !strings.HasPrefix(id, "role:") {
			appendUniqueWorkflowApprover(&out, seen, id)
			continue
		}
		role, ok := roleByID[id]
		if !ok {
			if decoded, derr := decodeApprovalRoleID(id); derr == nil {
				role, ok = roleByID[decoded]
			}
		}
		if !ok {
			continue
		}
		for _, assignee := range role.Assignees {
			for _, resolved := range resolveApprovalRoleAssignee(assignee, userMachineID) {
				appendUniqueWorkflowApprover(&out, seen, resolved)
			}
		}
	}
	return out, nil
}

func (r *workflowApprovalRoleResolver) userMachineResolver(ctx context.Context, tenantID string) func(string) []string {
	if r == nil || r.identity == nil || r.devices == nil {
		return func(_ string) []string { return nil }
	}
	users, err := r.identity.ListUsersForTenant(ctx, tenantID)
	if err != nil {
		return func(_ string) []string { return nil }
	}
	emailToUserID := map[string]string{}
	for _, user := range users {
		if user == nil {
			continue
		}
		email := strings.TrimSpace(strings.ToLower(user.Email))
		if email == "" {
			continue
		}
		emailToUserID[email] = strings.TrimSpace(user.ID)
	}
	return func(subjectID string) []string {
		key := strings.TrimSpace(strings.ToLower(subjectID))
		userID := emailToUserID[key]
		if userID == "" {
			return nil
		}
		machines, err := r.devices.ListMachines(ctx, userID)
		if err != nil || len(machines) == 0 {
			return nil
		}
		for _, machine := range machines {
			if machine.Online && strings.TrimSpace(machine.MachineID) != "" {
				return []string{strings.TrimSpace(machine.MachineID)}
			}
		}
		for _, machine := range machines {
			if strings.TrimSpace(machine.MachineID) != "" {
				return []string{strings.TrimSpace(machine.MachineID)}
			}
		}
		return nil
	}
}

func resolveApprovalRoleAssignee(assignee approvalRoleAssignee, userMachineID func(string) []string) []string {
	subjectID := strings.TrimSpace(assignee.SubjectID)
	if subjectID == "" {
		return nil
	}
	subjectType := strings.TrimSpace(strings.ToLower(assignee.SubjectType))
	if subjectType == "user" || strings.Contains(subjectID, "@") {
		if resolved := userMachineID(subjectID); len(resolved) > 0 {
			return resolved
		}
		if subjectType == "user" {
			return nil
		}
	}
	return []string{subjectID}
}

func normalizeWorkflowApproverIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := map[string]struct{}{}
	for _, id := range ids {
		appendUniqueWorkflowApprover(&out, seen, id)
	}
	return out
}

func appendUniqueWorkflowApprover(out *[]string, seen map[string]struct{}, id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	if _, ok := seen[id]; ok {
		return
	}
	seen[id] = struct{}{}
	*out = append(*out, id)
}

func decodeApprovalRoleID(id string) (string, error) {
	parts := strings.Split(id, ":")
	for i := range parts {
		decoded, err := url.PathUnescape(parts[i])
		if err != nil {
			return "", err
		}
		parts[i] = decoded
	}
	return strings.Join(parts, ":"), nil
}
