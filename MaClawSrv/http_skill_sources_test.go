package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

func TestAdminSkillSourcePolicyWritesAudit(t *testing.T) {
	ctx := context.Background()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(ctx, agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(ctx, agentservice.CreateUserInput{TenantID: tenant.ID, Name: "Runtime User", Email: "runtime@example.test"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/skill-sources/available", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("available sources status = %d", w.Code)
	}
	var available struct {
		Sources     []string          `json:"sources"`
		Description map[string]string `json:"description"`
	}
	if err := json.NewDecoder(w.Body).Decode(&available); err != nil {
		t.Fatalf("decode available sources: %v", err)
	}
	if !stringSliceContains(available.Sources, "local") || strings.TrimSpace(available.Description["local"]) == "" {
		t.Fatalf("available sources should expose local source and description: %#v", available)
	}

	do := func(method, path, body string) int {
		t.Helper()
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		return w.Code
	}

	if code := do(http.MethodPut, "/api/v1/admin/skill-sources/global", `{"enabled":true,"allowed_sources":["github"]}`); code != http.StatusOK {
		t.Fatalf("set global status = %d", code)
	}
	if code := do(http.MethodPut, "/api/v1/admin/skill-sources/tenant/tenant-a", `{"enabled":true,"allowed_sources":["skillhub"]}`); code != http.StatusOK {
		t.Fatalf("set tenant status = %d", code)
	}
	if code := do(http.MethodDelete, "/api/v1/admin/skill-sources/tenant/tenant-a", ""); code != http.StatusOK {
		t.Fatalf("delete tenant status = %d", code)
	}
	tenantUserPath := "/api/v1/admin/skill-sources/tenants/" + tenant.ID + "/users/" + user.ID
	if code := do(http.MethodPut, tenantUserPath, `{"enabled":true,"allowed_sources":["local"]}`); code != http.StatusOK {
		t.Fatalf("set tenant user status = %d", code)
	}
	if code := do(http.MethodDelete, tenantUserPath, ""); code != http.StatusOK {
		t.Fatalf("delete tenant user status = %d", code)
	}
	checks := map[string]string{
		"admin.skill_sources_global_updated":      "global",
		"admin.skill_sources_tenant_updated":      "tenant-a",
		"admin.skill_sources_tenant_deleted":      "tenant-a",
		"admin.skill_sources_tenant_user_updated": tenant.ID + ":" + user.ID,
		"admin.skill_sources_tenant_user_deleted": tenant.ID + ":" + user.ID,
	}
	for action, resourceID := range checks {
		events, err := svc.ListAuditEvents(ctx, agentservice.ListAuditEventsInput{Action: action})
		if err != nil {
			t.Fatalf("ListAuditEvents %s: %v", action, err)
		}
		if len(events) != 1 || events[0].ResourceID != resourceID || events[0].Metadata["remote_ip"] == "" {
			t.Fatalf("unexpected audit events for %s: %#v", action, events)
		}
	}
}

func TestSkillSourceTenantUserPolicyUsesRuntimeUserID(t *testing.T) {
	ctx := context.Background()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(ctx, agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(ctx, agentservice.CreateUserInput{TenantID: tenant.ID, Name: "Runtime User", Email: "runtime@example.test"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	sourceSvc := cskill.NewSourceControlService(newFileKVStore(filepath.Join(t.TempDir(), "skill_sources.json")))
	if err := sourceSvc.SetGlobal(ctx, &cskill.SourceControlConfig{Enabled: true, AllowedSources: []string{"skillhub"}}); err != nil {
		t.Fatalf("SetGlobal: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil, sourceSvc)
	path := "/api/v1/admin/skill-sources/tenants/" + tenant.ID + "/users/" + user.ID
	req := httptest.NewRequest(http.MethodPut, path, bytes.NewBufferString(`{"enabled":true,"allowed_sources":["local"]}`))
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("set tenant user status = %d body = %s", w.Code, w.Body.String())
	}

	got := svc.SkillSourceFilter(tenant.ID, user.ID)
	if len(got) != 1 || got[0] != "local" {
		t.Fatalf("SkillSourceFilter() = %#v, want tenant user local policy", got)
	}

	req = httptest.NewRequest(http.MethodGet, path+"/resolve", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"local"`) {
		t.Fatalf("resolve tenant user status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestSkillSourceTenantUserPolicyRequiresExistingUser(t *testing.T) {
	ctx := context.Background()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(ctx, agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil, cskill.NewSourceControlService(newFileKVStore(filepath.Join(t.TempDir(), "skill_sources.json"))))
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/skill-sources/tenants/"+tenant.ID+"/users/missing-user", bytes.NewBufferString(`{"enabled":true,"allowed_sources":["github"]}`))
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing user status = %d body = %s", w.Code, w.Body.String())
	}

	if got := svc.SkillSourceFilter(tenant.ID, "missing-user"); got != nil {
		t.Fatalf("missing user policy should not be persisted, got %#v", got)
	}
}

func stringSliceContains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
