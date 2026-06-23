package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/device"
	securitypkg "github.com/RapidAI/CodeClaw/hub/internal/security"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
)

func TestWorkflowApproverDirectoryHandlerReturnsTenantScopedApprovers(t *testing.T) {
	ctx := context.Background()
	tenantID := "tenant_workflow_directory"
	now := time.Now().UTC()

	provider, err := sqlite.NewProvider(sqlite.Config{
		DSN:               filepath.Join(t.TempDir(), "workflow-directory.db"),
		WAL:               true,
		BusyTimeoutMS:     5000,
		MaxReadOpenConns:  4,
		MaxReadIdleConns:  2,
		MaxWriteOpenConns: 1,
		MaxWriteIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	if err := sqlite.RunMigrations(provider.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })

	st := sqlite.NewStore(provider)
	identity := auth.NewIdentityService(st.Users, st.Enrollments, st.EmailBlocks, st.Machines, st.ViewerTokens, st.LoginTokens, st.System, nil, "open", true, nil, "")
	deviceSvc := device.NewService(st.Machines, device.NewRuntime())
	securityStore := securitypkg.NewSecurityStore(provider.Write)
	if err := securityStore.InitSchema(ctx); err != nil {
		t.Fatalf("init security schema: %v", err)
	}
	securityCtx := securitypkg.WithTenant(ctx, tenantID)
	requestCtx := store.WithTenant(ctx, tenantID)
	if err := securityStore.InitRootGroup(securityCtx); err != nil {
		t.Fatalf("init tenant root group: %v", err)
	}
	securitySvc := securitypkg.NewSecurityService(securityStore, st.System, st.AdminAudit, st.Users)

	if err := st.Users.Create(ctx, &store.User{ID: "user-approver", TenantID: tenantID, Email: "approver@example.com", SN: "SN-1", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := st.Users.Create(ctx, &store.User{ID: "user-finance", TenantID: tenantID, Email: "finance@example.com", SN: "SN-FIN", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create finance user: %v", err)
	}
	if err := st.Users.Create(ctx, &store.User{ID: "user-other", TenantID: "tenant_other", Email: "other@example.com", SN: "SN-2", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create other user: %v", err)
	}

	root, err := securitySvc.GetGroupTree(securityCtx)
	if err != nil {
		t.Fatalf("get group tree: %v", err)
	}
	if root == nil || root.ID == "" {
		t.Fatal("expected tenant root group")
	}
	financeGroup, err := securitySvc.CreateGroup(securityCtx, "Finance", root.ID)
	if err != nil {
		t.Fatalf("create finance group: %v", err)
	}
	if err := securitySvc.AssignUser(securityCtx, "approver@example.com", root.ID); err != nil {
		t.Fatalf("assign user: %v", err)
	}
	if err := securitySvc.AssignUser(securityCtx, "finance@example.com", financeGroup.ID); err != nil {
		t.Fatalf("assign finance user: %v", err)
	}

	if err := st.Machines.Create(ctx, &store.Machine{ID: "machine-approver", TenantID: tenantID, UserID: "user-approver", ClientID: "client-1", Name: "Office PC", Platform: "windows", Status: "online", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create machine: %v", err)
	}
	if err := st.Machines.Create(ctx, &store.Machine{ID: "machine-finance", TenantID: tenantID, UserID: "user-finance", ClientID: "client-fin", Name: "Finance PC", Platform: "windows", Status: "online", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create finance machine: %v", err)
	}
	if err := st.Machines.Create(ctx, &store.Machine{ID: "machine-other", TenantID: "tenant_other", UserID: "user-other", ClientID: "client-2", Name: "Other PC", Platform: "windows", Status: "online", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create other machine: %v", err)
	}

	tenantSystem := scopedSystemSettingsForTenant(tenantID, st.System)
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/platform/runtime/report" {
			t.Fatalf("unexpected runtime path: %s", r.URL.Path)
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "users": []map[string]any{}})
	}))
	defer runtime.Close()
	if err := saveMacLawSrvRuntimeRegistry(ctx, st.System, macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{{RuntimeID: maclawSrvRuntimePlatformID, BaseURL: runtime.URL, TenantIDs: []string{tenantID}}}}); err != nil {
		t.Fatalf("save runtime registry: %v", err)
	}
	if err := saveVERegistry(ctx, tenantSystem, digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{
		{ID: "ve-active", MachineID: "ve-machine-active", Name: "Runtime Worker", Status: veStatusActive, OwnerEmail: "owner@example.com"},
		{ID: "ve-finance-twin", MachineID: "ve-machine-finance-twin", Name: "Finance Twin", Status: veStatusActive, OwnerEmail: "finance@example.com"},
		{ID: "ve-finance-bot", MachineID: "ve-machine-finance-bot", Name: "Finance Bot", Status: veStatusActive, VisibleGroupIDs: []string{financeGroup.ID}},
		{ID: "ve-ghost", MachineID: "ve-ghost", PlatformID: maclawSrvRuntimePlatformID, PlatformEmployeeID: "deleted-employee", Name: "Deleted Runtime Worker", Status: veStatusActive, OwnerEmail: "owner@example.com"},
		{ID: "ve-disabled", MachineID: "ve-machine-disabled", Name: "Disabled Worker", Status: veStatusDisabled, OwnerEmail: "owner@example.com"},
	}}); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}
	if err := tenantSystem.Set(ctx, approvalRolesSettingsKey, `{"functionScopes":[{"scopeId":"finance","scopeName":"Finance"},{"scopeId":"hr","scopeName":"HR","custom":true}],"roles":[{"scopeType":"function","scopeId":"finance","scopeName":"Finance","roleCode":"finance_approver","roleName":"Finance Approver","executionMode":"digital_review","assignees":[{"subjectType":"user","subjectId":"approver@example.com","displayName":"Approver"}]},{"scopeType":"department","scopeId":"`+financeGroup.ID+`","scopeName":"Finance","roleCode":"department_manager","roleName":"Department Manager","executionMode":"manual","assignees":[{"subjectType":"digital_twin","subjectId":"ve-machine-finance-twin","displayName":"Finance Twin"},{"subjectType":"digital_employee","subjectId":"ve-machine-finance-bot","displayName":"Finance Bot"}]}]}`); err != nil {
		t.Fatalf("save approval roles: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workflow-directory/approvers", nil).WithContext(requestCtx)
	rr := httptest.NewRecorder()
	WorkflowApproverDirectoryHandler(securitySvc, identity, deviceSvc, st.System).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		TenantID       string                     `json:"tenant_id"`
		Tree           *securitypkg.GroupTreeNode `json:"tree"`
		MembersByGroup map[string][]string        `json:"members_by_group"`
		Users          []map[string]any           `json:"users"`
		Machines       []map[string]any           `json:"machines"`
		Employees      []digitalEmployeeEntry     `json:"employees"`
		ApprovalRoles  []approvalRoleRecord       `json:"approval_roles"`
		FunctionScopes []approvalFunctionScope    `json:"function_scopes"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.TenantID != tenantID {
		t.Fatalf("tenant_id = %q, want %q", body.TenantID, tenantID)
	}
	if body.Tree == nil || body.Tree.ID != root.ID || body.Tree.TenantID != tenantID {
		t.Fatalf("unexpected tree: %#v", body.Tree)
	}
	if len(body.Tree.Children) != 1 || body.Tree.Children[0].ID != financeGroup.ID {
		t.Fatalf("expected finance group in tree, got %#v", body.Tree.Children)
	}
	if got := body.MembersByGroup[root.ID]; len(got) != 1 || got[0] != "approver@example.com" {
		t.Fatalf("root members = %#v", got)
	}
	if got := body.MembersByGroup[financeGroup.ID]; len(got) != 1 || got[0] != "finance@example.com" {
		t.Fatalf("finance members = %#v", got)
	}
	if !jsonListContains(body.Users, "email", "approver@example.com") || !jsonListContains(body.Users, "email", "finance@example.com") || jsonListContains(body.Users, "email", "other@example.com") {
		t.Fatalf("unexpected users: %#v", body.Users)
	}
	if !jsonListContains(body.Machines, "machine_id", "machine-approver") || !jsonListContains(body.Machines, "machine_id", "machine-finance") || jsonListContains(body.Machines, "machine_id", "machine-other") {
		t.Fatalf("unexpected machines: %#v", body.Machines)
	}
	if len(body.Employees) != 3 || !employeeListContains(body.Employees, "ve-machine-active") || !employeeListContains(body.Employees, "ve-machine-finance-twin") || !employeeListContains(body.Employees, "ve-machine-finance-bot") {
		t.Fatalf("unexpected employees: %#v", body.Employees)
	}
	if len(body.ApprovalRoles) != 2 {
		t.Fatalf("unexpected approval roles: %#v", body.ApprovalRoles)
	}
	if len(body.FunctionScopes) != 2 || body.FunctionScopes[0].ScopeID != "finance" || body.FunctionScopes[1].ScopeID != "hr" || !body.FunctionScopes[1].Custom {
		t.Fatalf("unexpected function scopes: %#v", body.FunctionScopes)
	}
	rolesByID := map[string]approvalRoleRecord{}
	for _, role := range body.ApprovalRoles {
		rolesByID[role.ID] = role
	}
	if role := rolesByID["role:function:finance:finance_approver"]; role.ID == "" || role.Assignees[0].SubjectID != "approver@example.com" {
		t.Fatalf("unexpected function approval role: %#v", role)
	}
	departmentRoleID := "role:department:" + financeGroup.ID + ":department_manager"
	if role := rolesByID[departmentRoleID]; role.ID == "" || role.Assignees[0].SubjectID != "ve-machine-finance-twin" || role.Assignees[1].SubjectID != "ve-machine-finance-bot" {
		t.Fatalf("unexpected department approval role: %#v", role)
	}
}

func TestWorkflowApproverDirectoryHandlerMissingDeps(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workflow-directory/approvers", nil)
	rr := httptest.NewRecorder()
	WorkflowApproverDirectoryHandler(nil, nil, nil, nil).ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
}

func jsonListContains(items []map[string]any, key, value string) bool {
	for _, item := range items {
		if got, _ := item[key].(string); got == value {
			return true
		}
	}
	return false
}

func employeeListContains(items []digitalEmployeeEntry, machineID string) bool {
	for _, item := range items {
		if item.MachineID == machineID {
			return true
		}
	}
	return false
}
