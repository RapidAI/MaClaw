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

func TestWorkflowApprovalRoleResolverPreservesDirectApproversAndDedupes(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	tenantID := "tenant-role-runtime-mixed"
	ctx := store.WithTenant(context.Background(), tenantID)
	tenantSettings := scopedSystemSettingsForTenant(tenantID, settings)
	payload := `{"roles":[` +
		`{"scopeType":"function","scopeId":"finance","roleCode":"finance_approver","roleName":"Finance Approver","assignees":[{"subjectType":"digital_employee","subjectId":"machine-finance-bot","displayName":"Finance Bot"},{"subjectType":"digital_employee","subjectId":"machine-shared","displayName":"Shared Bot"}]},` +
		`{"scopeType":"function","scopeId":"legal","roleCode":"contract approver","roleName":"Contract Approver","assignees":[{"subjectType":"digital_employee","subjectId":"machine-shared","displayName":"Shared Bot"},{"subjectType":"digital_employee","subjectId":"machine-legal-bot","displayName":"Legal Bot"}]}` +
		`]}`
	if err := tenantSettings.Set(ctx, approvalRolesSettingsKey, payload); err != nil {
		t.Fatalf("save approval roles: %v", err)
	}

	resolver := newWorkflowApprovalRoleResolver(settings, nil, nil)
	got, err := resolver.ResolveApproverIDs(ctx, []string{
		"direct-machine",
		"role:function:finance:finance_approver",
		"direct-machine",
		"role:function:legal:contract%20approver",
		"role:function:missing:unknown",
	})
	if err != nil {
		t.Fatalf("ResolveApproverIDs returned error: %v", err)
	}
	want := []string{"direct-machine", "machine-finance-bot", "machine-shared", "machine-legal-bot"}
	if len(got) != len(want) {
		t.Fatalf("resolved approvers = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("resolved approvers = %#v, want %#v", got, want)
		}
	}
}
