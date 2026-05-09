package tenant

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
		_ = json.NewEncoder(w).Encode(RegisterCenterResponse{CenterID: "center-1", Secret: "secret-1", Status: "active", Reused: true})
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
	if resp.CenterID != "center-1" || resp.Secret != "secret-1" || !resp.Reused {
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

func TestRegisterCenterRejectsOversizedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"center_id":"center-oversized","secret":"secret","status":"pending","message":"`))
		_, _ = io.WriteString(w, strings.Repeat("x", 1<<20))
		_, _ = w.Write([]byte(`"}`))
	}))
	defer srv.Close()

	client := NewCloudClient(CloudConfig{BaseURL: srv.URL})
	_, err := client.RegisterCenter(context.Background(), RegisterCenterRequest{
		CompanyName: "Oversized Inc",
		AdminEmail:  "admin@example.com",
	})
	if err == nil || !strings.Contains(err.Error(), "decode register response") {
		t.Fatalf("RegisterCenter() err = %v, want limited decode error", err)
	}
}

func TestRegisterCenterNormalizesReturnedCredentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(RegisterCenterResponse{
			CenterID: " center-normalized ",
			Secret:   " secret-normalized ",
			Status:   " pending ",
			Message:  " registered ",
		})
	}))
	defer srv.Close()

	client := NewCloudClient(CloudConfig{BaseURL: srv.URL})
	resp, err := client.RegisterCenter(context.Background(), RegisterCenterRequest{
		CompanyName: "Normalized Inc",
		AdminEmail:  "admin@example.com",
	})
	if err != nil {
		t.Fatalf("RegisterCenter() error: %v", err)
	}
	if resp.CenterID != "center-normalized" || resp.Secret != "secret-normalized" || resp.Status != "pending" || resp.Message != "registered" {
		t.Fatalf("response was not normalized: %+v", resp)
	}
}

func TestRegisterCenterRejectsMissingReturnedCredentials(t *testing.T) {
	cases := []struct {
		name       string
		response   RegisterCenterResponse
		wantErr    string
		company    string
		adminEmail string
	}{
		{
			name:       "missing center id",
			response:   RegisterCenterResponse{Secret: "secret-1", Status: "pending"},
			wantErr:    "center_id",
			company:    "Missing Center Inc",
			adminEmail: "center@example.com",
		},
		{
			name:       "missing secret",
			response:   RegisterCenterResponse{CenterID: "center-1", Status: "pending"},
			wantErr:    "secret",
			company:    "Missing Secret Inc",
			adminEmail: "secret@example.com",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(tc.response)
			}))
			defer srv.Close()

			client := NewCloudClient(CloudConfig{BaseURL: srv.URL})
			_, err := client.RegisterCenter(context.Background(), RegisterCenterRequest{
				CompanyName: tc.company,
				AdminEmail:  tc.adminEmail,
			})
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("RegisterCenter() err = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestRegisterTenantToCloudPersistsReturnedCredentials(t *testing.T) {
	seen := make(chan RegisterCenterRequest, 1)
	rawBody := make(chan string, 1)
	heartbeatSeen := make(chan CenterHeartbeatRequest, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/centers/register":
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
		case "/api/centers/center-service-1/heartbeat":
			var req CenterHeartbeatRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode heartbeat: %v", err)
			}
			if req.Secret != "secret-service-1" || req.RuntimeType != "service" || req.ProductKind != "iworkercenter" || req.AdminConsole != "web_console" {
				t.Fatalf("heartbeat identity = %+v", req)
			}
			heartbeatSeen <- req
			writeJSON(w, http.StatusOK, map[string]any{"status": "heartbeat_ok"})
		default:
			t.Fatalf("path = %q", r.URL.Path)
		}
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
	if req.CompanyName != "Acme Inc" || req.AdminEmail != "admin@example.com" || req.BaseURL != "https://center.example.com" || req.CloudControlMode != "cloud_managed" {
		t.Fatalf("metadata = %+v", req)
	}
	if req.LegalPerson != "Jane Doe" || req.Address != "1 Center Road" {
		t.Fatalf("company review fields missing from cloud registration: %+v", req)
	}

	updated, err := svc.tenantRepo.GetByID(context.Background(), tenant.ID)
	if err != nil {
		t.Fatalf("get tenant: %v", err)
	}
	body := <-rawBody
	for _, required := range []string{"Acme Inc", "Jane Doe", "1 Center Road", "admin@example.com", "tenant_count", "legal_person"} {
		if !strings.Contains(body, required) {
			t.Fatalf("cloud registration missing %q: %s", required, body)
		}
	}

	if updated.CloudCenterID != "center-service-1" || updated.CloudSecret != "secret-service-1" {
		t.Fatalf("cloud credentials were not persisted: %+v", updated)
	}
	heartbeat := <-heartbeatSeen
	if heartbeat.CloudHeartbeat == nil || !heartbeat.CloudHeartbeat.NonBlocking || heartbeat.CloudHeartbeat.BusinessImpact != "none_local_center_and_iworker_continue" {
		t.Fatalf("registration heartbeat continuity = %+v", heartbeat.CloudHeartbeat)
	}
}

func TestAdminCloudRegisterRouteRegistersTenant(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/centers/register":
			_ = json.NewEncoder(w).Encode(RegisterCenterResponse{CenterID: "center-admin-1", Secret: "secret-admin-1", Status: "active", Message: "registered", Reused: true})
		case "/api/centers/center-admin-1/heartbeat":
			writeJSON(w, http.StatusOK, map[string]any{"status": "heartbeat_ok"})
		default:
			t.Fatalf("path = %q", r.URL.Path)
		}
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
	if resp["center_id"] != "center-admin-1" || resp["status"] != "active" || resp["reused"] != true {
		t.Fatalf("response = %+v", resp)
	}
	if resp["heartbeat_sent"] != true {
		t.Fatalf("heartbeat_sent = %+v, response = %+v", resp["heartbeat_sent"], resp)
	}
}

func TestAdminCloudRegisterRouteReportsHeartbeatFailureWithoutLosingCredentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/centers/register":
			_ = json.NewEncoder(w).Encode(RegisterCenterResponse{CenterID: "center-heartbeat-fail", Secret: "secret-heartbeat-fail", Status: "pending", Message: "registered"})
		case "/api/centers/center-heartbeat-fail/heartbeat":
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "heartbeat unavailable"})
		default:
			t.Fatalf("path = %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	svc, _ := newTestService(t)
	svc.cloudClient = NewCloudClient(CloudConfig{BaseURL: srv.URL, CenterBaseURL: "https://center.example.com"})
	tenant, err := svc.SetupFirstTenant(context.Background(), CreateTenantRequest{
		CompanyName:   "Heartbeat Failure Inc",
		Email:         "heartbeat-failure@example.com",
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
	if resp["center_id"] != "center-heartbeat-fail" || resp["heartbeat_sent"] != false {
		t.Fatalf("response = %+v", resp)
	}
	if got, _ := resp["heartbeat_error"].(string); !strings.Contains(got, "heartbeat unavailable") {
		t.Fatalf("heartbeat_error = %q, response = %+v", got, resp)
	}

	updated, err := svc.tenantRepo.GetByID(context.Background(), tenant.ID)
	if err != nil {
		t.Fatalf("get tenant: %v", err)
	}
	if updated.CloudCenterID != "center-heartbeat-fail" || updated.CloudSecret != "secret-heartbeat-fail" {
		t.Fatalf("cloud credentials were not persisted despite registration success: %+v", updated)
	}
}

func TestAdminCloudRegisterRouteUsesSubmittedRegistrationInfo(t *testing.T) {
	seen := make(chan RegisterCenterRequest, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/centers/register":
			var req RegisterCenterRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode cloud registration: %v", err)
			}
			seen <- req
			_ = json.NewEncoder(w).Encode(RegisterCenterResponse{CenterID: "center-admin-override", Secret: "secret-admin-override", Status: "pending", Message: "registered"})
		case "/api/centers/center-admin-override/heartbeat":
			writeJSON(w, http.StatusOK, map[string]any{"status": "heartbeat_ok"})
		default:
			t.Fatalf("path = %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	svc, _ := newTestService(t)
	svc.cloudClient = NewCloudClient(CloudConfig{BaseURL: srv.URL, CenterBaseURL: "https://center.example.com"})
	tenant, err := svc.SetupFirstTenant(context.Background(), CreateTenantRequest{
		CompanyName:   "Tenant Default Inc",
		LegalPerson:   "Tenant Legal",
		Email:         "tenant-default@example.com",
		Address:       "Tenant default address",
		AdminUsername: "admin",
		AdminPassword: "pass1234",
	})
	if err != nil {
		t.Fatalf("setup tenant: %v", err)
	}

	mux := http.NewServeMux()
	NewHandler(svc).RegisterAdminRoutes(mux)
	body := strings.NewReader(`{"company_name":"Submitted Inc","legal_person":"Submitted Legal","admin_phone":"+86 13800000000","admin_email":"submitted@example.com","address":"Submitted address"}`)
	req := httptest.NewRequest(http.MethodPost, "/admin/cloud/register?tenant_id="+tenant.ID, body)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	got := <-seen
	if got.CompanyName != "Submitted Inc" || got.LegalPerson != "Submitted Legal" || got.AdminPhone != "+86 13800000000" || got.AdminEmail != "submitted@example.com" || got.Address != "Submitted address" {
		t.Fatalf("submitted registration fields were not used: %+v", got)
	}
	if got.CompanyID != tenant.ID || got.MachineID == "" || got.BaseURL != "https://center.example.com" {
		t.Fatalf("system registration identity was not preserved: %+v", got)
	}
}

func TestAdminCloudRegisterRouteRejectsInvalidJSON(t *testing.T) {
	svc, _ := newTestService(t)
	svc.cloudClient = NewCloudClient(CloudConfig{BaseURL: "https://cloud.example.com", CenterBaseURL: "https://center.example.com"})
	tenant, err := svc.SetupFirstTenant(context.Background(), CreateTenantRequest{
		CompanyName:   "Invalid JSON Inc",
		Email:         "invalid-json@example.com",
		AdminUsername: "admin",
		AdminPassword: "pass1234",
	})
	if err != nil {
		t.Fatalf("setup tenant: %v", err)
	}

	mux := http.NewServeMux()
	NewHandler(svc).RegisterAdminRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, "/admin/cloud/register?tenant_id="+tenant.ID, strings.NewReader(`{"company_name":`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	updated, err := svc.tenantRepo.GetByID(context.Background(), tenant.ID)
	if err != nil {
		t.Fatalf("get tenant: %v", err)
	}
	if updated.CloudCenterID != "" || updated.CloudSecret != "" {
		t.Fatalf("cloud credentials should not be written after invalid JSON: %+v", updated)
	}
}

func TestAdminCloudRegisterRouteRejectsTrailingJSON(t *testing.T) {
	cloudCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cloudCalled = true
		t.Fatalf("cloud should not be called for invalid admin JSON")
	}))
	defer srv.Close()

	svc, _ := newTestService(t)
	svc.cloudClient = NewCloudClient(CloudConfig{BaseURL: srv.URL, CenterBaseURL: "https://center.example.com"})
	tenant, err := svc.SetupFirstTenant(context.Background(), CreateTenantRequest{
		CompanyName:   "Trailing JSON Inc",
		Email:         "trailing-json@example.com",
		AdminUsername: "admin",
		AdminPassword: "pass1234",
	})
	if err != nil {
		t.Fatalf("setup tenant: %v", err)
	}

	mux := http.NewServeMux()
	NewHandler(svc).RegisterAdminRoutes(mux)
	body := strings.NewReader(`{"company_name":"Submitted Inc","admin_email":"submitted@example.com"} {"company_name":"Injected Inc"}`)
	req := httptest.NewRequest(http.MethodPost, "/admin/cloud/register?tenant_id="+tenant.ID, body)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if cloudCalled {
		t.Fatal("cloud was called for trailing JSON payload")
	}
	updated, err := svc.tenantRepo.GetByID(context.Background(), tenant.ID)
	if err != nil {
		t.Fatalf("get tenant: %v", err)
	}
	if updated.CloudCenterID != "" || updated.CloudSecret != "" {
		t.Fatalf("cloud credentials should not be written after trailing JSON: %+v", updated)
	}
}

func TestTenantHandlerDoesNotExposeCloudTenantManagementRoutes(t *testing.T) {
	svc, _ := newTestService(t)
	mux := http.NewServeMux()
	handler := NewHandler(svc)
	handler.RegisterRoutes(mux)
	handler.RegisterAdminRoutes(mux)

	cases := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/cloud/tenants", ""},
		{http.MethodPost, "/api/cloud/tenants", `{"company_name":"Acme","email":"admin@example.com"}`},
		{http.MethodPut, "/api/cloud/tenants/tnt_1", `{"company_name":"Acme"}`},
		{http.MethodDelete, "/api/cloud/tenants/tnt_1", ""},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound && w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s %s status = %d body=%s, want 404/405 for removed Cloud tenant management route", tc.method, tc.path, w.Code, w.Body.String())
		}
	}
}

func TestAdminCloudStatusRouteReturnsLocalRegistrationState(t *testing.T) {
	svc, _ := newTestService(t)
	tenant, err := svc.SetupFirstTenant(context.Background(), CreateTenantRequest{
		CompanyName:   "Status Route Inc",
		Email:         "status@example.com",
		AdminUsername: "admin",
		AdminPassword: "pass1234",
	})
	if err != nil {
		t.Fatalf("setup tenant: %v", err)
	}

	mux := http.NewServeMux()
	NewHandler(svc).RegisterAdminRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "/admin/cloud/status?tenant_id="+tenant.ID, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["configured"] != false || resp["registered"] != false || resp["status"] != "not_configured" || resp["non_blocking"] != true || resp["company_id"] != tenant.ID {
		t.Fatalf("response = %+v", resp)
	}
	if machineID, ok := resp["machine_id"].(string); !ok || !strings.HasPrefix(machineID, "iwm_") {
		t.Fatalf("response = %+v", resp)
	}
}

func TestAdminCloudStatusRouteKeepsPendingRegistrationVisible(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/centers/center-pending-1/license" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("X-Center-Secret"); got != "secret-pending-1" {
			t.Fatalf("center secret header = %q", got)
		}
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "no active license"})
	}))
	defer srv.Close()

	svc, _ := newTestService(t)
	svc.cloudClient = NewCloudClient(CloudConfig{BaseURL: srv.URL})
	tenant, err := svc.SetupFirstTenant(context.Background(), CreateTenantRequest{
		CompanyName:   "Pending Route Inc",
		Email:         "pending@example.com",
		AdminUsername: "admin",
		AdminPassword: "pass1234",
	})
	if err != nil {
		t.Fatalf("setup tenant: %v", err)
	}
	if err := svc.tenantRepo.UpdateCloudInfo(context.Background(), tenant.ID, "center-pending-1", "secret-pending-1"); err != nil {
		t.Fatalf("update cloud info: %v", err)
	}

	mux := http.NewServeMux()
	NewHandler(svc).RegisterAdminRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "/admin/cloud/status?tenant_id="+tenant.ID, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if strings.Contains(body, "secret-pending-1") {
		t.Fatalf("status leaked cloud secret: %s", body)
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["configured"] != true || resp["registered"] != true || resp["center_id"] != "center-pending-1" || resp["status"] != "pending" || resp["non_blocking"] != true || resp["company_id"] != tenant.ID {
		t.Fatalf("response = %+v", resp)
	}
	if resp["license_error"] != "fetch center license: status 404: no active license" {
		t.Fatalf("license_error = %q", resp["license_error"])
	}
	if machineID, ok := resp["machine_id"].(string); !ok || !strings.HasPrefix(machineID, "iwm_") {
		t.Fatalf("response = %+v", resp)
	}
}

func TestAdminCloudStatusRouteTreatsCloudOutageAsNonBlocking(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("cloud should be offline for this test")
	}))
	cloudURL := srv.URL
	srv.Close()

	svc, _ := newTestService(t)
	svc.cloudClient = NewCloudClient(CloudConfig{BaseURL: cloudURL})
	tenant, err := svc.SetupFirstTenant(context.Background(), CreateTenantRequest{
		CompanyName:   "Offline Route Inc",
		Email:         "offline@example.com",
		AdminUsername: "admin",
		AdminPassword: "pass1234",
	})
	if err != nil {
		t.Fatalf("setup tenant: %v", err)
	}
	if err := svc.tenantRepo.UpdateCloudInfo(context.Background(), tenant.ID, "center-offline-1", "secret-offline-1"); err != nil {
		t.Fatalf("update cloud info: %v", err)
	}

	mux := http.NewServeMux()
	NewHandler(svc).RegisterAdminRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "/admin/cloud/status?tenant_id="+tenant.ID, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if strings.Contains(body, "secret-offline-1") {
		t.Fatalf("status leaked cloud secret: %s", body)
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["configured"] != true || resp["registered"] != true || resp["center_id"] != "center-offline-1" || resp["status"] != "offline" || resp["non_blocking"] != true {
		t.Fatalf("response = %+v", resp)
	}
	if resp["license_error"] == "" {
		t.Fatalf("expected diagnostic license_error, response = %+v", resp)
	}
}

func TestAdminCloudStatusRouteUsesCachedLicenseAndComputeDuringCloudOutage(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/centers/center-cached-1/license":
			if got := r.Header.Get("X-Center-Secret"); got != "secret-cached-1" {
				t.Fatalf("center secret header = %q", got)
			}
			writeJSON(w, http.StatusOK, CloudLicense{
				ID:          "lic-cached-1",
				CenterID:    "center-cached-1",
				Modules:     `["compute","skill_market"]`,
				Type:        "annual",
				ExpiresAt:   now.Add(365 * 24 * time.Hour),
				IsLongTerm:  false,
				Certificate: "signed-license",
				CreatedAt:   now,
			})
		case "/api/centers/center-cached-1/compute-providers":
			writeJSON(w, http.StatusOK, CenterComputeProvidersResponse{
				ComputePermission: true,
				ForceSync:         true,
				Providers: []CloudComputeProvider{{
					ID:          "provider-1",
					Name:        "Cloud Compute",
					BaseURL:     "https://llm.example.com",
					Protocol:    "openai",
					ComputeType: "llm",
					Model:       "model-a",
					Enabled:     true,
				}},
			})
		default:
			t.Fatalf("path = %q", r.URL.Path)
		}
	}))

	svc, _ := newTestService(t)
	svc.cloudClient = NewCloudClient(CloudConfig{BaseURL: srv.URL})
	tenant, err := svc.SetupFirstTenant(context.Background(), CreateTenantRequest{
		CompanyName:   "Cached License Inc",
		Email:         "cached@example.com",
		AdminUsername: "admin",
		AdminPassword: "pass1234",
	})
	if err != nil {
		t.Fatalf("setup tenant: %v", err)
	}
	if err := svc.tenantRepo.UpdateCloudInfo(context.Background(), tenant.ID, "center-cached-1", "secret-cached-1"); err != nil {
		t.Fatalf("update cloud info: %v", err)
	}

	mux := http.NewServeMux()
	NewHandler(svc).RegisterAdminRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "/admin/cloud/status?tenant_id="+tenant.ID, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first status = %d, body = %s", w.Code, w.Body.String())
	}
	var first struct {
		Status  string              `json:"status"`
		License *CloudLicense       `json:"license"`
		Compute *CloudComputeStatus `json:"compute"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &first); err != nil {
		t.Fatalf("decode first response: %v", err)
	}
	if first.Status != "licensed" || first.License == nil || first.License.ID != "lic-cached-1" || first.Compute == nil || first.Compute.ProviderCount != 1 {
		t.Fatalf("first response = %+v", first)
	}

	cloudURL := srv.URL
	srv.Close()
	svc.cloudClient = NewCloudClient(CloudConfig{BaseURL: cloudURL})

	req = httptest.NewRequest(http.MethodGet, "/admin/cloud/status?tenant_id="+tenant.ID, nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("offline status = %d, body = %s", w.Code, w.Body.String())
	}
	var offline struct {
		Status        string              `json:"status"`
		License       *CloudLicense       `json:"license"`
		Compute       *CloudComputeStatus `json:"compute"`
		LicenseCached bool                `json:"license_cached"`
		ComputeCached bool                `json:"compute_cached"`
		NonBlocking   bool                `json:"non_blocking"`
		LicenseError  string              `json:"license_error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &offline); err != nil {
		t.Fatalf("decode offline response: %v", err)
	}
	if offline.Status != "offline" || !offline.NonBlocking || offline.License == nil || offline.License.ID != "lic-cached-1" || !offline.LicenseCached {
		t.Fatalf("offline license cache response = %+v", offline)
	}
	if offline.Compute == nil || offline.Compute.ProviderCount != 1 || !offline.Compute.ComputePermission || !offline.ComputeCached {
		t.Fatalf("offline compute cache response = %+v", offline)
	}
	if offline.LicenseError == "" {
		t.Fatalf("expected outage diagnostic error, response = %+v", offline)
	}
}

func TestAdminCloudStatusRouteDoesNotUseCachedLicenseForCredentialMismatch(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	rejectCredentials := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/centers/center-credential-1/license":
			if rejectCredentials {
				writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid center credentials"})
				return
			}
			writeJSON(w, http.StatusOK, CloudLicense{
				ID:          "lic-credential-1",
				CenterID:    "center-credential-1",
				Modules:     `["compute"]`,
				Type:        "annual",
				ExpiresAt:   now.Add(365 * 24 * time.Hour),
				Certificate: "signed-license",
				CreatedAt:   now,
			})
		case "/api/centers/center-credential-1/compute-providers":
			writeJSON(w, http.StatusOK, CenterComputeProvidersResponse{ComputePermission: true})
		default:
			t.Fatalf("path = %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	svc, _ := newTestService(t)
	svc.cloudClient = NewCloudClient(CloudConfig{BaseURL: srv.URL})
	tenant, err := svc.SetupFirstTenant(context.Background(), CreateTenantRequest{
		CompanyName:   "Credential Cache Inc",
		Email:         "credential-cache@example.com",
		AdminUsername: "admin",
		AdminPassword: "pass1234",
	})
	if err != nil {
		t.Fatalf("setup tenant: %v", err)
	}
	if err := svc.tenantRepo.UpdateCloudInfo(context.Background(), tenant.ID, "center-credential-1", "secret-credential-1"); err != nil {
		t.Fatalf("update cloud info: %v", err)
	}

	first, err := svc.CloudStatus(context.Background(), tenant.ID)
	if err != nil {
		t.Fatalf("first cloud status: %v", err)
	}
	if first.Status != "licensed" || first.License == nil {
		t.Fatalf("first status = %+v", first)
	}

	rejectCredentials = true
	second, err := svc.CloudStatus(context.Background(), tenant.ID)
	if err != nil {
		t.Fatalf("second cloud status: %v", err)
	}
	if second.Status != "credential_mismatch" || second.License != nil || second.LicenseCached {
		t.Fatalf("credential mismatch should not use cached license: %+v", second)
	}
}

func TestCloudStatusReportsCorruptCacheDuringCloudOutage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "cloud offline", http.StatusServiceUnavailable)
	}))
	cloudURL := srv.URL
	srv.Close()

	svc, p := newTestService(t)
	svc.cloudClient = NewCloudClient(CloudConfig{BaseURL: cloudURL})
	tenant, err := svc.SetupFirstTenant(context.Background(), CreateTenantRequest{
		CompanyName:   "Corrupt Cache Inc",
		Email:         "corrupt-cache@example.com",
		AdminUsername: "admin",
		AdminPassword: "pass1234",
	})
	if err != nil {
		t.Fatalf("setup tenant: %v", err)
	}
	if err := svc.tenantRepo.UpdateCloudInfo(context.Background(), tenant.ID, "center-corrupt-cache", "secret-corrupt-cache"); err != nil {
		t.Fatalf("update cloud info: %v", err)
	}
	if _, err := p.Write.Exec(`INSERT INTO system_settings (key, value_json, updated_at) VALUES (?, ?, datetime('now'))`, tenantCloudStatusCacheKey(tenant.ID), `{bad cache json`); err != nil {
		t.Fatalf("insert corrupt cache: %v", err)
	}

	status, err := svc.CloudStatus(context.Background(), tenant.ID)
	if err != nil {
		t.Fatalf("cloud status: %v", err)
	}
	if status.Status != "offline" || !status.NonBlocking {
		t.Fatalf("status = %+v, want non-blocking offline", status)
	}
	if status.CacheError == "" {
		t.Fatalf("expected cache error to be visible: %+v", status)
	}
	if status.LicenseCached || status.ComputeCached || status.License != nil || status.Compute != nil {
		t.Fatalf("corrupt cache should not be used: %+v", status)
	}
}
