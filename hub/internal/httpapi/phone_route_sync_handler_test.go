package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestAdminSyncVerifiedPhoneRoutesHandlerScopesTenantAdminsAndReturnsCount(t *testing.T) {
	services := newAdminRouterTestContext(t)

	unauthorized := doHubAdminJSONRequest(t, services.handler, http.MethodPost, "/api/admin/routing/sync-verified-phone-routes", nil, "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d body=%s", unauthorized.Code, unauthorized.Body.String())
	}

	token := issueHubAdminToken(t, services.handler)
	tenantToken := issueTenantAdminToken(t, services.handler, token, "phone-route-sync", "phone_route_admin")
	tenantRec := doHubAdminJSONRequest(t, services.handler, http.MethodPost, "/api/admin/routing/sync-verified-phone-routes", nil, tenantToken)
	if tenantRec.Code != http.StatusOK {
		t.Fatalf("tenant admin status = %d body=%s", tenantRec.Code, tenantRec.Body.String())
	}
	var tenantBody struct {
		SyncedCount int    `json:"synced_count"`
		TenantID    string `json:"tenant_id"`
	}
	if err := json.Unmarshal(tenantRec.Body.Bytes(), &tenantBody); err != nil {
		t.Fatalf("decode tenant response: %v", err)
	}
	if tenantBody.SyncedCount != 0 {
		t.Fatalf("tenant synced_count = %d, want 0", tenantBody.SyncedCount)
	}
	if tenantBody.TenantID != "tenant_phone-route-sync" {
		t.Fatalf("tenant_id = %q, want tenant_phone-route-sync", tenantBody.TenantID)
	}

	rec := doHubAdminJSONRequest(t, services.handler, http.MethodPost, "/api/admin/routing/sync-verified-phone-routes", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		SyncedCount int    `json:"synced_count"`
		TenantID    string `json:"tenant_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.SyncedCount != 0 {
		t.Fatalf("synced_count = %d, want 0", body.SyncedCount)
	}
	if body.TenantID != "" {
		t.Fatalf("global tenant_id = %q, want empty", body.TenantID)
	}

	scoped := doHubAdminJSONRequest(t, services.handler, http.MethodPost, "/api/admin/routing/sync-verified-phone-routes?tenant_id=%20tenant_phone-route-sync%20", nil, token)
	if scoped.Code != http.StatusOK {
		t.Fatalf("scoped global status = %d body=%s", scoped.Code, scoped.Body.String())
	}
	var scopedBody struct {
		SyncedCount int    `json:"synced_count"`
		TenantID    string `json:"tenant_id"`
	}
	if err := json.Unmarshal(scoped.Body.Bytes(), &scopedBody); err != nil {
		t.Fatalf("decode scoped response: %v", err)
	}
	if scopedBody.SyncedCount != 0 {
		t.Fatalf("scoped synced_count = %d, want 0", scopedBody.SyncedCount)
	}
	if scopedBody.TenantID != "tenant_phone-route-sync" {
		t.Fatalf("scoped tenant_id = %q, want tenant_phone-route-sync", scopedBody.TenantID)
	}
}
