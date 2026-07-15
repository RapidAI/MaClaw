package httpapi

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/workflow"
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

type fakeDeptLookup struct {
	byEmail map[string]string
}

func (f fakeDeptLookup) GetUserGroupID(_ context.Context, email string) (string, error) {
	if f.byEmail == nil {
		return "", nil
	}
	return f.byEmail[email], nil
}

func TestWorkflowApprovalRoleResolverDynamicApplicantDepartment(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	tenantID := "tenant-dynamic-dept"
	ctx := store.WithTenant(context.Background(), tenantID)
	tenantSettings := scopedSystemSettingsForTenant(tenantID, settings)
	// Department-scoped managers for sales + finance; dynamic template has empty assignees.
	payload := `{"roles":[` +
		`{"scopeType":"dynamic","scopeId":"applicant_department","roleCode":"department_manager","roleName":"Department Manager","executionMode":"manual","assignees":[]},` +
		`{"scopeType":"department","scopeId":"dept-sales","roleCode":"department_manager","roleName":"Sales Manager","executionMode":"manual","assignees":[{"subjectType":"digital_employee","subjectId":"machine-sales-mgr","displayName":"Sales Mgr"}]},` +
		`{"scopeType":"department","scopeId":"dept-finance","roleCode":"department_manager","roleName":"Finance Manager","executionMode":"manual","assignees":[{"subjectType":"digital_employee","subjectId":"machine-fin-mgr","displayName":"Fin Mgr"}]}` +
		`]}`
	if err := tenantSettings.Set(ctx, approvalRolesSettingsKey, payload); err != nil {
		t.Fatalf("save approval roles: %v", err)
	}

	resolver := newWorkflowApprovalRoleResolver(settings, nil, nil, fakeDeptLookup{
		byEmail: map[string]string{"alice@example.com": "dept-sales"},
	})
	ctx = workflow.WithApprovalResolveContext(ctx, &workflow.ApprovalResolveContext{
		ApplicantID: "alice@example.com",
	})
	got, err := resolver.ResolveApproverIDs(ctx, []string{"role:dynamic:applicant_department:department_manager"})
	if err != nil {
		t.Fatalf("ResolveApproverIDs: %v", err)
	}
	if len(got) != 1 || got[0] != "machine-sales-mgr" {
		t.Fatalf("got %#v, want machine-sales-mgr", got)
	}

	// Explicit department id in resolve context wins over email lookup.
	ctx2 := workflow.WithApprovalResolveContext(store.WithTenant(context.Background(), tenantID), &workflow.ApprovalResolveContext{
		ApplicantID:  "alice@example.com",
		DepartmentID: "dept-finance",
	})
	got2, err := resolver.ResolveApproverIDs(ctx2, []string{"role:dynamic:applicant_department:department_manager"})
	if err != nil {
		t.Fatalf("ResolveApproverIDs: %v", err)
	}
	if len(got2) != 1 || got2[0] != "machine-fin-mgr" {
		t.Fatalf("got %#v, want machine-fin-mgr", got2)
	}
}

func TestExpandApprovalRoleAssigneesExecutionMode(t *testing.T) {
	userMachine := func(email string) []string {
		if email == "boss@example.com" {
			return []string{"machine-boss"}
		}
		return nil
	}
	role := approvalRoleRecord{
		ExecutionMode: "digital_suggest",
		Assignees: []approvalRoleAssignee{
			{SubjectType: "user", SubjectID: "boss@example.com"},
			{SubjectType: "digital_employee", SubjectID: "machine-bot"},
		},
	}
	got := expandApprovalRoleAssignees(role, userMachine)
	if len(got) != 2 || got[0] != "machine-bot" || got[1] != "machine-boss" {
		t.Fatalf("digital_suggest order = %#v, want [bot, boss]", got)
	}

	role.ExecutionMode = "manual"
	got = expandApprovalRoleAssignees(role, userMachine)
	if len(got) != 2 || got[0] != "machine-boss" || got[1] != "machine-bot" {
		t.Fatalf("manual order = %#v, want [boss, bot]", got)
	}

	role.ExecutionMode = "auto"
	got = expandApprovalRoleAssignees(role, userMachine)
	if len(got) != 1 || got[0] != "machine-bot" {
		t.Fatalf("auto = %#v, want [bot]", got)
	}
}

func TestApprovalResolveContextFromInstanceData(t *testing.T) {
	rc := workflow.ApprovalResolveContextFromInstanceData(map[string]interface{}{
		"requester_id": "alice@example.com",
		"details": map[string]interface{}{
			"department_id": "dept-1",
		},
	})
	if rc.ApplicantID != "alice@example.com" || rc.DepartmentID != "dept-1" {
		t.Fatalf("resolve context = %#v", rc)
	}
}
