package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/cloudworkspace"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func withCloudWorkspaceTenantAdmin(req *http.Request, tenantID string) *http.Request {
	return req.WithContext(context.WithValue(req.Context(), adminUserContextKey, &store.AdminUser{
		ID: "audit-admin", Scope: "tenant", TenantID: tenantID,
	}))
}

func TestCloudWorkspaceAuditAdminExportCSVAndJSONL(t *testing.T) {
	svc, h, _ := newCloudWorkspaceUserEnv(t, cloudworkspace.ModeAllUsers, 5, nil)
	created := createCloudWorkspace(t, h, "m1", "audit-export")
	for _, eventID := range []string{"e1", "e2"} {
		resp := doCloudWorkspaceRequest(t, h, http.MethodPost, "/api/v1/cloud-workspaces/"+created+"/audit", "m1", "secret", map[string]any{
			"event_id": eventID, "operation": "push", "outcome": "ok", "files": 1,
		})
		if resp.Code != http.StatusOK {
			t.Fatalf("record %s=%d %s", eventID, resp.Code, resp.Body.String())
		}
	}
	handler := CloudWorkspaceAuditExportAdminHandler(svc)
	req := withCloudWorkspaceTenantAdmin(httptest.NewRequest(http.MethodGet,
		"/api/admin/cloud-workspaces/audit/export?format=csv&limit=1", nil), "t1")
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/csv") {
		t.Fatalf("csv status=%d headers=%v body=%s", rec.Code, rec.Header(), rec.Body.String())
	}
	if rec.Header().Get("X-Audit-Has-More") != "true" || rec.Header().Get("X-Audit-Next-After-ID") == "0" {
		t.Fatalf("csv pagination headers=%v", rec.Header())
	}
	lines := strings.Split(strings.TrimSpace(rec.Body.String()), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "id,event_id") || !strings.Contains(lines[1], "e1") {
		t.Fatalf("csv body=%q", rec.Body.String())
	}

	req = withCloudWorkspaceTenantAdmin(httptest.NewRequest(http.MethodGet,
		"/api/admin/cloud-workspaces/audit/export?format=jsonl&after_id="+rec.Header().Get("X-Audit-Next-After-ID"), nil), "t1")
	rec = httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/x-ndjson") {
		t.Fatalf("jsonl status=%d headers=%v body=%s", rec.Code, rec.Header(), rec.Body.String())
	}
	var event cloudworkspace.AuditExportEvent
	if err := json.Unmarshal([]byte(strings.TrimSpace(rec.Body.String())), &event); err != nil {
		t.Fatalf("jsonl decode: %v body=%s", err, rec.Body.String())
	}
	if event.EventID != "e2" || event.TenantID != "t1" || event.UserID != "u1" {
		t.Fatalf("jsonl event=%+v", event)
	}
}

func TestCloudWorkspaceAuditAdminExportRequiresTenantScope(t *testing.T) {
	svc, _, _ := newCloudWorkspaceUserEnv(t, cloudworkspace.ModeAllUsers, 5, nil)
	handler := CloudWorkspaceAuditExportAdminHandler(svc)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/cloud-workspaces/audit/export", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	global := req.WithContext(context.WithValue(req.Context(), adminUserContextKey, &store.AdminUser{ID: "global", Scope: "global"}))
	rec = httptest.NewRecorder()
	handler(rec, global)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("global status=%d body=%s", rec.Code, rec.Body.String())
	}
}
