package tenant

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHandlerTenantStatus_Empty(t *testing.T) {
	svc, _ := newTestService(t)
	h := NewHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/auth/tenant-status", nil)
	w := httptest.NewRecorder()
	h.handleTenantStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["needs_setup"] != true {
		t.Errorf("expected needs_setup=true, got %v", resp["needs_setup"])
	}
	if resp["count"] != float64(0) {
		t.Errorf("expected count=0, got %v", resp["count"])
	}
}

func TestHandlerTenantStatus_AfterSetup(t *testing.T) {
	svc, _ := newTestService(t)
	h := NewHandler(svc)

	// Setup first tenant
	svc.SetupFirstTenant(context.Background(), CreateTenantRequest{
		CompanyName: "公司", Email: "a@t.com",
		AdminUsername: "admin", AdminPassword: "pass",
	})

	req := httptest.NewRequest(http.MethodGet, "/auth/tenant-status", nil)
	w := httptest.NewRecorder()
	h.handleTenantStatus(w, req)

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["needs_setup"] != false {
		t.Errorf("expected needs_setup=false after setup")
	}
	if resp["count"] != float64(1) {
		t.Errorf("expected count=1, got %v", resp["count"])
	}
}

func TestHandlerListTenants(t *testing.T) {
	svc, _ := newTestService(t)
	h := NewHandler(svc)

	svc.SetupFirstTenant(context.Background(), CreateTenantRequest{
		CompanyName: "测试公司", Email: "a@t.com",
		AdminUsername: "admin", AdminPassword: "pass",
	})

	req := httptest.NewRequest(http.MethodGet, "/auth/tenants", nil)
	w := httptest.NewRecorder()
	h.handleListTenants(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Tenants []struct {
			ID          string `json:"id"`
			CompanyName string `json:"company_name"`
		} `json:"tenants"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Tenants) != 1 {
		t.Fatalf("expected 1 tenant, got %d", len(resp.Tenants))
	}
	if resp.Tenants[0].CompanyName != "测试公司" {
		t.Errorf("expected '测试公司', got %q", resp.Tenants[0].CompanyName)
	}
}

func TestHandlerSetupTenant_Success(t *testing.T) {
	svc, _ := newTestService(t)
	h := NewHandler(svc)

	body, _ := json.Marshal(CreateTenantRequest{
		CompanyName: "新公司", Email: "new@t.com",
		AdminUsername: "admin", AdminPassword: "pass123",
	})
	req := httptest.NewRequest(http.MethodPost, "/auth/setup-tenant", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.handleSetupTenant(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["tenant_id"] == nil || resp["tenant_id"] == "" {
		t.Error("expected tenant_id in response")
	}
}

func TestHandlerSetupTenant_AlreadyDone(t *testing.T) {
	svc, _ := newTestService(t)
	h := NewHandler(svc)

	// First setup
	body, _ := json.Marshal(CreateTenantRequest{
		CompanyName: "公司A", Email: "a@t.com",
		AdminUsername: "admin", AdminPassword: "pass",
	})
	req := httptest.NewRequest(http.MethodPost, "/auth/setup-tenant", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.handleSetupTenant(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first setup failed: %d", w.Code)
	}

	// Second setup should return 409
	body2, _ := json.Marshal(CreateTenantRequest{
		CompanyName: "公司B", Email: "b@t.com",
		AdminUsername: "admin2", AdminPassword: "pass",
	})
	req2 := httptest.NewRequest(http.MethodPost, "/auth/setup-tenant", bytes.NewReader(body2))
	w2 := httptest.NewRecorder()
	h.handleSetupTenant(w2, req2)
	if w2.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestHandlerProvision_NoCloudConfig(t *testing.T) {
	svc, _ := newTestService(t)
	h := NewHandler(svc)

	body, _ := json.Marshal(ProvisionRequest{
		CompanyName:   "远程公司",
		Email:         "remote@t.com",
		AdminUsername: "admin",
		AdminPassword: "pass",
		Timestamp:     time.Now().Unix(),
		Nonce:         "nonce-1",
		Signature:     "invalid",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/tenants/provision", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.handleProvision(w, req)

	// Should fail because no cloud config (pubKeyCache is nil)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
}
