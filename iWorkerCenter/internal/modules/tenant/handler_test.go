package tenant

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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

func TestRegisterRoutesDoesNotExposeCloudProvision(t *testing.T) {
	svc, _ := newTestService(t)
	h := NewHandler(svc)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/tenants/provision", bytes.NewReader([]byte(`{"company_name":"Acme","admin_password":"secret"}`)))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s, want 404 for disabled cloud tenant provisioning route", w.Code, w.Body.String())
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

func TestAdminCloudConfigRouteUpdatesRuntimeConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	svc, _ := newTestService(t)
	h := NewHandler(svc)
	mux := http.NewServeMux()
	h.RegisterAdminRoutes(mux)

	body := bytes.NewReader([]byte(`{"base_url":"http://127.0.0.1:9366/","center_base_url":"http://127.0.0.1:9377/","registration_name":"Local Center","registration_email":"admin@example.com","cloud_control_mode":"hybrid"}`))
	req := httptest.NewRequest(http.MethodPut, "/admin/cloud/config", body)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp CloudConfig
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.BaseURL != "http://127.0.0.1:9366" || resp.CenterBaseURL != "http://127.0.0.1:9377" || resp.CloudControlMode != "hybrid" {
		t.Fatalf("config = %+v", resp)
	}
	if got := svc.CloudConfig(context.Background()); got.BaseURL != "http://127.0.0.1:9366" {
		t.Fatalf("runtime config not updated: %+v", got)
	}
}

func TestAdminCloudConfigRouteReturnsSnakeCaseJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	svc, _ := newTestService(t)
	svc.cloudClient = NewCloudClient(CloudConfig{BaseURL: "http://127.0.0.1:9366", CenterBaseURL: "http://127.0.0.1:9377", RegistrationName: "Local Center", RegistrationEmail: "admin@example.com", CloudControlMode: "hybrid"})
	mux := http.NewServeMux()
	NewHandler(svc).RegisterAdminRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/admin/cloud/config", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "\"base_url\"") || strings.Contains(body, "\"BaseURL\"") {
		t.Fatalf("cloud config should use snake_case JSON, body = %s", body)
	}
}
