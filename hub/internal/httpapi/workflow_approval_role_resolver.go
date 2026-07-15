package httpapi

import (
	"context"
	"net/url"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/device"
	"github.com/RapidAI/CodeClaw/hub/internal/security"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/workflow"
)

// userDepartmentLookup resolves an applicant email to a security group id.
// *security.SecurityService satisfies this via GetUserGroupID.
type userDepartmentLookup interface {
	GetUserGroupID(ctx context.Context, email string) (string, error)
}

type workflowApprovalRoleResolver struct {
	system   store.SystemSettingsRepository
	identity *auth.IdentityService
	devices  *device.Service
	security userDepartmentLookup
}

func newWorkflowApprovalRoleResolver(system store.SystemSettingsRepository, identity *auth.IdentityService, devices *device.Service, securitySvc ...userDepartmentLookup) *workflowApprovalRoleResolver {
	var sec userDepartmentLookup
	if len(securitySvc) > 0 {
		sec = securitySvc[0]
	}
	return &workflowApprovalRoleResolver{system: system, identity: identity, devices: devices, security: sec}
}

// ResolveExecutionMode returns the most cautious execution mode among role refs
// in approverIDs. Used when the approval node config does not set execution_mode.
// Order of caution: digital_suggest > digital_review > auto > manual.
func (r *workflowApprovalRoleResolver) ResolveExecutionMode(ctx context.Context, approverIDs []string) string {
	if r == nil {
		return ""
	}
	tenantID, ok := store.TenantIDFromContextIfPresent(ctx)
	if !ok {
		tenantID = store.DefaultTenantID
	}
	roles, err := loadApprovalRolesForTenantContext(ctx, r.system, tenantID)
	if err != nil {
		return ""
	}
	roleByID := make(map[string]approvalRoleRecord, len(roles.Roles))
	for _, role := range roles.Roles {
		roleByID[role.ID] = role
	}
	rank := func(mode string) int {
		switch normalizeWorkflowExecutionMode(mode) {
		case "digital_suggest":
			return 4
		case "digital_review":
			return 3
		case "auto":
			return 2
		case "manual":
			return 1
		default:
			return 0
		}
	}
	best := ""
	bestRank := 0
	for _, id := range approverIDs {
		id = strings.TrimSpace(id)
		if !strings.HasPrefix(id, "role:") {
			continue
		}
		role, ok := r.lookupRole(id, roleByID)
		if !ok {
			continue
		}
		if isDynamicApplicantDepartmentRole(role) {
			// Keep dynamic template mode (department-mapped role inherits its own mode later).
		}
		mode := normalizeWorkflowExecutionMode(role.ExecutionMode)
		if rnk := rank(mode); rnk > bestRank {
			bestRank = rnk
			best = mode
		}
	}
	return best
}

func normalizeWorkflowExecutionMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "manual", "human":
		return "manual"
	case "digital_suggest", "digital-suggest", "suggest":
		return "digital_suggest"
	case "digital_review", "digital-review", "review":
		return "digital_review"
	case "auto", "automatic", "auto_approve":
		return "auto"
	default:
		return strings.ToLower(strings.TrimSpace(mode))
	}
}

// ResolveApproverIDs expands role:… references and direct machine/user ids into
// concrete runtime approver identities (prefer online machine ids for users).
//
// Dynamic roles:
//
//	role:dynamic:applicant_department:<roleCode>
//
// resolve the applicant's department group, then look up
//
//	role:department:<deptID>:<roleCode>
//
// (falling back to the dynamic role's own assignees when present).
//
// executionMode influences assignee ordering / filtering:
//
//	manual          → humans first (users / personal twins), then digital employees
//	digital_suggest → digital first, then humans (sequential nodes hit digital first)
//	digital_review  → same as digital_suggest
//	auto            → digital assignees only when present; otherwise humans
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
	// Secondary index: scopeType|scopeId|roleCode → role (for dynamic remapping).
	roleByScopeCode := make(map[string]approvalRoleRecord, len(roles.Roles))
	for _, role := range roles.Roles {
		key := approvalRoleScopeKey(role.ScopeType, role.ScopeID, role.RoleCode)
		roleByScopeCode[key] = role
	}

	userMachineID := r.userMachineResolver(ctx, tenantID)
	resolveCtx := workflow.ApprovalResolveContextFrom(ctx)
	applicantDeptID := r.resolveApplicantDepartmentID(ctx, resolveCtx)

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
		role, ok := r.lookupRole(id, roleByID)
		if !ok {
			continue
		}
		// Dynamic applicant_department → concrete department role.
		if isDynamicApplicantDepartmentRole(role) {
			if mapped, mok := r.mapDynamicDepartmentRole(role, applicantDeptID, roleByScopeCode); mok {
				role = mapped
			}
		}
		resolved := expandApprovalRoleAssignees(role, userMachineID)
		for _, item := range resolved {
			appendUniqueWorkflowApprover(&out, seen, item)
		}
	}
	return out, nil
}

func (r *workflowApprovalRoleResolver) lookupRole(id string, roleByID map[string]approvalRoleRecord) (approvalRoleRecord, bool) {
	if role, ok := roleByID[id]; ok {
		return role, true
	}
	if decoded, err := decodeApprovalRoleID(id); err == nil {
		if role, ok := roleByID[decoded]; ok {
			return role, true
		}
	}
	// Synthesize a minimal role from the id for dynamic templates that may not be saved yet.
	if role, ok := parseApprovalRoleID(id); ok {
		return role, true
	}
	return approvalRoleRecord{}, false
}

func (r *workflowApprovalRoleResolver) resolveApplicantDepartmentID(ctx context.Context, resolveCtx *workflow.ApprovalResolveContext) string {
	if resolveCtx != nil {
		if dept := strings.TrimSpace(resolveCtx.DepartmentID); dept != "" {
			return dept
		}
	}
	if r == nil || r.security == nil || resolveCtx == nil {
		return ""
	}
	email := strings.TrimSpace(resolveCtx.ApplicantID)
	if email == "" || !strings.Contains(email, "@") {
		// Try map email-like fields only.
		return ""
	}
	// Ensure tenant is on context for security store.
	if _, ok := store.TenantIDFromContextIfPresent(ctx); !ok {
		// leave as-is; security store will use default tenant
	}
	dept, err := r.security.GetUserGroupID(ctx, email)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(dept)
}

func isDynamicApplicantDepartmentRole(role approvalRoleRecord) bool {
	return strings.EqualFold(strings.TrimSpace(role.ScopeType), "dynamic") &&
		strings.EqualFold(strings.TrimSpace(role.ScopeID), "applicant_department")
}

func (r *workflowApprovalRoleResolver) mapDynamicDepartmentRole(dynamic approvalRoleRecord, deptID string, roleByScopeCode map[string]approvalRoleRecord) (approvalRoleRecord, bool) {
	deptID = strings.TrimSpace(deptID)
	if deptID == "" {
		// No department known: keep dynamic role assignees (may be empty).
		return dynamic, len(dynamic.Assignees) > 0
	}
	key := approvalRoleScopeKey("department", deptID, dynamic.RoleCode)
	if role, ok := roleByScopeCode[key]; ok {
		return role, true
	}
	// Fall back to dynamic role's own assignees if configured as a template with people.
	if len(dynamic.Assignees) > 0 {
		return dynamic, true
	}
	return dynamic, false
}

func approvalRoleScopeKey(scopeType, scopeID, roleCode string) string {
	return strings.ToLower(strings.TrimSpace(scopeType)) + "|" +
		strings.ToLower(strings.TrimSpace(scopeID)) + "|" +
		strings.ToLower(strings.TrimSpace(roleCode))
}

func parseApprovalRoleID(id string) (approvalRoleRecord, bool) {
	// role:scopeType:scopeId:roleCode (each segment may be path-escaped)
	decoded, err := decodeApprovalRoleID(id)
	if err != nil {
		return approvalRoleRecord{}, false
	}
	parts := strings.Split(decoded, ":")
	if len(parts) < 4 || parts[0] != "role" {
		return approvalRoleRecord{}, false
	}
	scopeType := parts[1]
	scopeID := parts[2]
	roleCode := strings.Join(parts[3:], ":")
	if scopeType == "" || roleCode == "" {
		return approvalRoleRecord{}, false
	}
	return approvalRoleRecord{
		ID:            id,
		ScopeType:     scopeType,
		ScopeID:       scopeID,
		RoleCode:      roleCode,
		RoleName:      roleCode,
		ExecutionMode: "manual",
	}, true
}

func expandApprovalRoleAssignees(role approvalRoleRecord, userMachineID func(string) []string) []string {
	if len(role.Assignees) == 0 {
		return nil
	}
	mode := strings.ToLower(strings.TrimSpace(role.ExecutionMode))
	if mode == "" {
		mode = "manual"
	}

	type resolvedAssignee struct {
		id      string
		digital bool
	}
	var items []resolvedAssignee
	for _, assignee := range role.Assignees {
		for _, id := range resolveApprovalRoleAssignee(assignee, userMachineID) {
			items = append(items, resolvedAssignee{
				id:      id,
				digital: isDigitalApprovalSubject(assignee.SubjectType),
			})
		}
	}
	if len(items) == 0 {
		return nil
	}

	var digital, human []string
	for _, item := range items {
		if item.digital {
			digital = append(digital, item.id)
		} else {
			human = append(human, item.id)
		}
	}

	switch mode {
	case "auto", "automatic", "auto_approve":
		// Prefer digital-only when available (VE auto path); otherwise humans.
		if len(digital) > 0 {
			return digital
		}
		return human
	case "digital_suggest", "digital_review", "digital_first":
		// Digital first so sequential/single modes hit VE rules before humans.
		return append(append([]string{}, digital...), human...)
	default: // manual
		// Humans first; include digital twins only as fallback so manual nodes
		// do not auto-route exclusively to bots when people are configured.
		if len(human) > 0 {
			return append(append([]string{}, human...), digital...)
		}
		return digital
	}
}

func isDigitalApprovalSubject(subjectType string) bool {
	switch strings.ToLower(strings.TrimSpace(subjectType)) {
	case "digital_employee", "digital_twin", "ve", "bot", "machine":
		return true
	default:
		return false
	}
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

// Ensure security.SecurityService implements userDepartmentLookup at compile time when present.
var _ userDepartmentLookup = (*security.SecurityService)(nil)
