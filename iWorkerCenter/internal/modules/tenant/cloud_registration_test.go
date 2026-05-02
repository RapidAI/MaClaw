package tenant

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegisterCenterSendsManagementMetadata(t *testing.T) {
	seen := make(chan RegisterCenterRequest, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/centers/register" {
			t.Fatalf("path = %q, want /api/centers/register", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %q, want POST", r.Method)
		}
		var req RegisterCenterRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		seen <- req
		_ = json.NewEncoder(w).Encode(RegisterCenterResponse{CenterID: "center-1", Secret: "secret-1", Status: "active"})
	}))
	defer srv.Close()

	client := NewCloudClient(CloudConfig{BaseURL: srv.URL + "/"})
	resp, err := client.RegisterCenter(context.Background(), RegisterCenterRequest{
		CompanyName:      "Acme Inc",
		AdminEmail:       "admin@example.com",
		LegalPerson:      "Jane Doe",
		Address:          "1 Center Road",
		BaseURL:          "https://center.example.com",
		CloudControlMode: "cloud_managed",
	})
	if err != nil {
		t.Fatalf("RegisterCenter() error: %v", err)
	}
	if resp.CenterID != "center-1" || resp.Secret != "secret-1" {
		t.Fatalf("response = %+v", resp)
	}

	req := <-seen
	if req.CompanyName != "Acme Inc" || req.AdminEmail != "admin@example.com" {
		t.Fatalf("registration identity = %+v", req)
	}
	if req.BaseURL != "https://center.example.com" || req.CloudControlMode != "cloud_managed" {
		t.Fatalf("management service metadata = %+v", req)
	}
}

func TestRegisterTenantToCloudPersistsReturnedCredentials(t *testing.T) {
	seen := make(chan RegisterCenterRequest, 1)
	rawBody := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request: %v", err)
		}
		rawBody <- string(body)
		var req RegisterCenterRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		seen <- req
		_ = json.NewEncoder(w).Encode(RegisterCenterResponse{CenterID: "center-service-1", Secret: "secret-service-1", Status: "active", Message: "registered"})
	}))
	defer srv.Close()

	svc, _ := newTestService(t)
	svc.cloudClient = NewCloudClient(CloudConfig{
		BaseURL:           srv.URL,
		CenterBaseURL:     " https://center.example.com ",
		RegistrationName:  "HQ iWorkerCenter",
		RegistrationEmail: "center-admin@example.net",
		CloudControlMode:  "cloud_managed",
	})
	tenant, err := svc.SetupFirstTenant(context.Background(), CreateTenantRequest{
		CompanyName:   "Acme Inc",
		LegalPerson:   "Jane Doe",
		Email:         "admin@example.com",
		Address:       "1 Center Road",
		AdminUsername: "admin",
		AdminPassword: "pass1234",
	})
	if err != nil {
		t.Fatalf("setup tenant: %v", err)
	}

	resp, err := svc.RegisterTenantToCloud(context.Background(), tenant.ID)
	if err != nil {
		t.Fatalf("RegisterTenantToCloud() error: %v", err)
	}
	if resp.CenterID != "center-service-1" || resp.Secret != "secret-service-1" {
		t.Fatalf("response = %+v", resp)
	}

	req := <-seen
	if req.CompanyName != "HQ iWorkerCenter" || req.AdminEmail != "center-admin@example.net" || req.BaseURL != "https://center.example.com" || req.CloudControlMode != "cloud_managed" {
		t.Fatalf("metadata = %+v", req)
	}
	if req.LegalPerson != "" || req.Address != "" {
		t.Fatalf("tenant business fields leaked into cloud registration: %+v", req)
	}

	updated, err := svc.tenantRepo.GetByID(context.Background(), tenant.ID)
	if err != nil {
		t.Fatalf("get tenant: %v", err)
	}
	body := <-rawBody
	for _, forbidden := range []string{"Acme Inc", "Jane Doe", "1 Center Road", "admin@example.com", "tenant_count", "legal_person"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("cloud registration leaked %q: %s", forbidden, body)
		}
	}

	if updated.CloudCenterID != "center-service-1" || updated.CloudSecret != "secret-service-1" {
		t.Fatalf("cloud credentials were not persisted: %+v", updated)
	}
}

func TestAdminCloudRegisterRouteRegistersTenant(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/centers/register" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(RegisterCenterResponse{CenterID: "center-admin-1", Secret: "secret-admin-1", Status: "active", Message: "registered"})
	}))
	defer srv.Close()

	svc, _ := newTestService(t)
	svc.cloudClient = NewCloudClient(CloudConfig{BaseURL: srv.URL, CenterBaseURL: "https://center.example.com"})
	tenant, err := svc.SetupFirstTenant(context.Background(), CreateTenantRequest{
		CompanyName:   "Admin Route Inc",
		Email:         "admin-route@example.com",
		AdminUsername: "admin",
		AdminPassword: "pass1234",
	})
	if err != nil {
		t.Fatalf("setup tenant: %v", err)
	}

	mux := http.NewServeMux()
	NewHandler(svc).RegisterAdminRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, "/admin/cloud/register?tenant_id="+tenant.ID, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["center_id"] != "center-admin-1" || resp["status"] != "active" {
		t.Fatalf("response = %+v", resp)
	}
}
