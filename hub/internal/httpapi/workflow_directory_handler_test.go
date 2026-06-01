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
	if err := securitySvc.AssignUser(securityCtx, "approver@example.com", root.ID); err != nil {
		t.Fatalf("assign user: %v", err)
	}

	if err := st.Machines.Create(ctx, &store.Machine{ID: "machine-approver", TenantID: tenantID, UserID: "user-approver", ClientID: "client-1", Name: "Office PC", Platform: "windows", Status: "online", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create machine: %v", err)
	}
	if err := st.Machines.Create(ctx, &store.Machine{ID: "machine-other", TenantID: "tenant_other", UserID: "user-other", ClientID: "client-2", Name: "Other PC", Platform: "windows", Status: "online", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create other machine: %v", err)
	}

	tenantSystem := scopedSystemSettingsForTenant(tenantID, st.System)
	if err := saveVERegistry(ctx, tenantSystem, digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{
		{ID: "ve-active", MachineID: "ve-machine-active", Name: "Runtime Worker", Status: veStatusActive, OwnerEmail: "owner@example.com"},
		{ID: "ve-disabled", MachineID: "ve-machine-disabled", Name: "Disabled Worker", Status: veStatusDisabled, OwnerEmail: "owner@example.com"},
	}}); err != nil {
		t.Fatalf("save ve registry: %v", err)
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
	if got := body.MembersByGroup[root.ID]; len(got) != 1 || got[0] != "approver@example.com" {
		t.Fatalf("members = %#v", got)
	}
	if !jsonListContains(body.Users, "email", "approver@example.com") || jsonListContains(body.Users, "email", "other@example.com") {
		t.Fatalf("unexpected users: %#v", body.Users)
	}
	if !jsonListContains(body.Machines, "machine_id", "machine-approver") || jsonListContains(body.Machines, "machine_id", "machine-other") {
		t.Fatalf("unexpected machines: %#v", body.Machines)
	}
	if len(body.Employees) != 1 || body.Employees[0].Name != "Runtime Worker" || body.Employees[0].MachineID != "ve-machine-active" {
		t.Fatalf("unexpected employees: %#v", body.Employees)
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
