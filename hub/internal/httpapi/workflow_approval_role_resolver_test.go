package httpapi

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func TestWorkflowApprovalRoleResolverExpandsRoleAssignees(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	tenantID := "tenant-role-runtime"
	ctx := store.WithTenant(context.Background(), tenantID)
	tenantSettings := scopedSystemSettingsForTenant(tenantID, settings)
	if err := tenantSettings.Set(ctx, approvalRolesSettingsKey, `{"roles":[{"scopeType":"function","scopeId":"finance","roleCode":"finance_approver","roleName":"Finance Approver","assignees":[{"subjectType":"digital_employee","subjectId":"machine-finance-bot","displayName":"Finance Bot"}]}]}`); err != nil {
		t.Fatalf("save approval roles: %v", err)
	}

	resolver := newWorkflowApprovalRoleResolver(settings, nil, nil)
	got, err := resolver.ResolveApproverIDs(ctx, []string{"role:function:finance:finance_approver"})
	if err != nil {
		t.Fatalf("ResolveApproverIDs returned error: %v", err)
	}
	if len(got) != 1 || got[0] != "machine-finance-bot" {
		t.Fatalf("resolved approvers = %#v, want machine-finance-bot", got)
	}
}
