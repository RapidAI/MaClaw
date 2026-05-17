package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestAdminSkillSourcePolicyWritesAudit(t *testing.T) {
	ctx := context.Background()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

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
	userPath := "/api/v1/admin/skill-sources/user/" + url.PathEscape("ops@example.com")
	if code := do(http.MethodPut, userPath, `{"enabled":true,"allowed_sources":["clawhub","github"]}`); code != http.StatusOK {
		t.Fatalf("set user status = %d", code)
	}
	if code := do(http.MethodDelete, userPath, ""); code != http.StatusOK {
		t.Fatalf("delete user status = %d", code)
	}

	checks := map[string]string{
		"admin.skill_sources_global_updated": "global",
		"admin.skill_sources_tenant_updated": "tenant-a",
		"admin.skill_sources_tenant_deleted": "tenant-a",
		"admin.skill_sources_user_updated":   "ops@example.com",
		"admin.skill_sources_user_deleted":   "ops@example.com",
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
