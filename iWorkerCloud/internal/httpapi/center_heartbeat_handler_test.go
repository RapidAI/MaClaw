package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/centers"
	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/license"
	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/store"
)

type heartbeatCenterRepo struct {
	items map[string]*store.Center
}

func newHeartbeatCenterRepo(items ...*store.Center) *heartbeatCenterRepo {
	repo := &heartbeatCenterRepo{items: map[string]*store.Center{}}
	for _, item := range items {
		copy := *item
		repo.items[item.ID] = &copy
	}
	return repo
}

func (r *heartbeatCenterRepo) Create(_ context.Context, c *store.Center) error {
	copy := *c
	r.items[c.ID] = &copy
	return nil
}

func (r *heartbeatCenterRepo) GetByID(_ context.Context, id string) (*store.Center, error) {
	item, ok := r.items[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	copy := *item
	return &copy, nil
}

func (r *heartbeatCenterRepo) GetByRegistrationKey(_ context.Context, machineID, companyID string) (*store.Center, error) {
	for _, item := range r.items {
		if item.MachineID == machineID && item.CompanyID == companyID {
			copy := *item
			return &copy, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (r *heartbeatCenterRepo) List(context.Context) ([]*store.Center, error) {
	out := make([]*store.Center, 0, len(r.items))
	for _, item := range r.items {
		copy := *item
		out = append(out, &copy)
	}
	return out, nil
}

func (r *heartbeatCenterRepo) UpdateStatus(_ context.Context, id, status string) error {
	item, ok := r.items[id]
	if !ok {
		return fmt.Errorf("not found")
	}
	item.Status = status
	return nil
}

func (r *heartbeatCenterRepo) UpdateHeartbeat(_ context.Context, c *store.Center) error {
	item, ok := r.items[c.ID]
	if !ok {
		return fmt.Errorf("not found")
	}
	item.LastHeartbeat = time.Now()
	item.LastSyncStatus = c.LastSyncStatus
	item.IWorkerReady = c.IWorkerReady
	item.IWorkerReadinessStatus = c.IWorkerReadinessStatus
	item.IWorkerTenantCount = c.IWorkerTenantCount
	item.IWorkerRoleCount = c.IWorkerRoleCount
	item.IWorkerColleagueCount = c.IWorkerColleagueCount
	item.IWorkerLocalAccountCount = c.IWorkerLocalAccountCount
	item.IWorkerAgentInstanceCount = c.IWorkerAgentInstanceCount
	item.IWorkerReadinessJSON = c.IWorkerReadinessJSON
	return nil
}

func (r *heartbeatCenterRepo) UpdateIntegration(_ context.Context, c *store.Center) error {
	item, ok := r.items[c.ID]
	if !ok {
		return fmt.Errorf("not found")
	}
	item.BaseURL = c.BaseURL
	item.SupportsMultiTenant = c.SupportsMultiTenant
	item.TenantCount = c.TenantCount
	item.CloudControlMode = c.CloudControlMode
	item.LastSyncStatus = c.LastSyncStatus
	item.IWorkerReady = c.IWorkerReady
	item.IWorkerReadinessStatus = c.IWorkerReadinessStatus
	item.IWorkerTenantCount = c.IWorkerTenantCount
	item.IWorkerRoleCount = c.IWorkerRoleCount
	item.IWorkerColleagueCount = c.IWorkerColleagueCount
	item.IWorkerLocalAccountCount = c.IWorkerLocalAccountCount
	item.IWorkerAgentInstanceCount = c.IWorkerAgentInstanceCount
	item.IWorkerReadinessJSON = c.IWorkerReadinessJSON
	return nil
}

func (r *heartbeatCenterRepo) UpdateRegistration(_ context.Context, c *store.Center) error {
	item, ok := r.items[c.ID]
	if !ok {
		return fmt.Errorf("not found")
	}
	item.MachineID = c.MachineID
	item.CompanyID = c.CompanyID
	item.CompanyName = c.CompanyName
	item.AdminEmail = c.AdminEmail
	item.AdminPhone = c.AdminPhone
	item.Address = c.Address
	item.LegalPerson = c.LegalPerson
	item.BaseURL = c.BaseURL
	item.SupportsMultiTenant = c.SupportsMultiTenant
	item.TenantCount = c.TenantCount
	item.CloudControlMode = c.CloudControlMode
	item.LastSyncStatus = c.LastSyncStatus
	return nil
}

func (r *heartbeatCenterRepo) Delete(_ context.Context, id string) error {
	delete(r.items, id)
	return nil
}

func TestHeartbeatHandlerAcceptsServiceIdentity(t *testing.T) {
	repo := newHeartbeatCenterRepo(&store.Center{ID: "ctr_1", Status: "active", SecretHash: hashTestSecret("secret-abc")})
	svc := centers.NewService(repo, nil)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/centers/{id}/heartbeat", HeartbeatHandler(svc))

	req := httptest.NewRequest(http.MethodPost, "/api/centers/ctr_1/heartbeat", strings.NewReader(`{"secret":"secret-abc","runtime_type":"service","product_kind":"iworkercenter","admin_console":"web_console"}`))
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", res.Code, res.Body.String())
	}
	center, _ := repo.GetByID(context.Background(), "ctr_1")
	if center.LastSyncStatus != "heartbeat_ok" {
		t.Fatalf("LastSyncStatus = %q, want heartbeat_ok", center.LastSyncStatus)
	}
}

func TestHeartbeatHandlerStoresIWorkerReadiness(t *testing.T) {
	repo := newHeartbeatCenterRepo(&store.Center{ID: "ctr_1", Status: "active", SecretHash: hashTestSecret("secret-abc")})
	svc := centers.NewService(repo, nil)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/centers/{id}/heartbeat", HeartbeatHandler(svc))

	body := `{"secret":"secret-abc","runtime_type":"service","product_kind":"iworkercenter","admin_console":"web_console","iworker_readiness":{"ready":true,"status":"ready","tenant_count":1,"role_count":2,"colleague_count":3,"local_account_count":4,"agent_instance_count":5}}`
	req := httptest.NewRequest(http.MethodPost, "/api/centers/ctr_1/heartbeat", strings.NewReader(body))
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", res.Code, res.Body.String())
	}
	center, _ := repo.GetByID(context.Background(), "ctr_1")
	if !center.IWorkerReady || center.IWorkerReadinessStatus != "ready" || center.IWorkerColleagueCount != 0 || center.IWorkerAgentInstanceCount != 5 {
		t.Fatalf("stored sanitized readiness = %+v", center)
	}
}

func TestHeartbeatHandlerRejectsInvalidServiceIdentityWithBadRequest(t *testing.T) {
	repo := newHeartbeatCenterRepo(&store.Center{ID: "ctr_1", Status: "active", SecretHash: hashTestSecret("secret-abc")})
	svc := centers.NewService(repo, nil)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/centers/{id}/heartbeat", HeartbeatHandler(svc))

	req := httptest.NewRequest(http.MethodPost, "/api/centers/ctr_1/heartbeat", strings.NewReader(`{"secret":"secret-abc","runtime_type":"desktop","product_kind":"iworker","admin_console":"desktop_gui"}`))
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", res.Code, res.Body.String())
	}
	var body map[string]string
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "HEARTBEAT_IDENTITY_FAILED" {
		t.Fatalf("error = %q, want HEARTBEAT_IDENTITY_FAILED", body["error"])
	}
}

func TestHeartbeatHandlerRejectsDisabledCenterWithForbidden(t *testing.T) {
	repo := newHeartbeatCenterRepo(&store.Center{ID: "ctr_1", Status: "disabled", SecretHash: hashTestSecret("secret-abc")})
	svc := centers.NewService(repo, nil)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/centers/{id}/heartbeat", HeartbeatHandler(svc))

	req := httptest.NewRequest(http.MethodPost, "/api/centers/ctr_1/heartbeat", strings.NewReader(`{"secret":"secret-abc","runtime_type":"service","product_kind":"iworkercenter","admin_console":"web_console"}`))
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s", res.Code, res.Body.String())
	}
}

type heartbeatLicenseRepo struct{}

func (r heartbeatLicenseRepo) Create(context.Context, *store.License) error { return nil }
func (r heartbeatLicenseRepo) GetByID(context.Context, string) (*store.License, error) {
	return nil, fmt.Errorf("not found")
}
func (r heartbeatLicenseRepo) GetByCenterID(context.Context, string) ([]*store.License, error) {
	return nil, nil
}
func (r heartbeatLicenseRepo) GetActiveByCenterID(context.Context, string) (*store.License, error) {
	return nil, fmt.Errorf("not found")
}
func (r heartbeatLicenseRepo) Revoke(context.Context, string) error           { return nil }
func (r heartbeatLicenseRepo) List(context.Context) ([]*store.License, error) { return nil, nil }

func TestRuntimeSnapshotHandlerReturnsNotFound(t *testing.T) {
	repo := newHeartbeatCenterRepo()
	svc := centers.NewService(repo, nil)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/admin/centers/{id}/runtime-snapshot", RuntimeSnapshotHandler(svc))

	req := httptest.NewRequest(http.MethodGet, "/api/admin/centers/missing/runtime-snapshot", nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s, want 404", res.Code, res.Body.String())
	}
	var body map[string]string
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "CENTER_NOT_FOUND" {
		t.Fatalf("error = %q, want CENTER_NOT_FOUND", body["error"])
	}
}

func TestServiceReadinessHandlerReturnsBlockingIssues(t *testing.T) {
	repo := newHeartbeatCenterRepo(&store.Center{ID: "ctr_1", Status: "active", BaseURL: "https://center.example", SupportsMultiTenant: true})
	svc := centers.NewService(repo, license.NewService(heartbeatLicenseRepo{}, nil))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/admin/centers/{id}/service-readiness", ServiceReadinessHandler(svc))

	req := httptest.NewRequest(http.MethodGet, "/api/admin/centers/ctr_1/service-readiness", nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", res.Code, res.Body.String())
	}
	var body struct {
		Allowed bool     `json:"allowed"`
		Issues  []string `json:"issues"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Allowed {
		t.Fatalf("allowed = true, want false")
	}
	found := false
	for _, issue := range body.Issues {
		if issue == "no_active_license" {
			found = true
		}
	}
	if !found {
		t.Fatalf("issues = %+v, want no_active_license", body.Issues)
	}
}

func TestProvisionTenantRouteIsNotRegistered(t *testing.T) {
	repo := newHeartbeatCenterRepo(&store.Center{ID: "ctr_1", Status: "active", BaseURL: "https://center.example"})
	svc := centers.NewService(repo, license.NewService(heartbeatLicenseRepo{}, nil))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/admin/centers/{id}/service-readiness", ServiceReadinessHandler(svc))

	req := httptest.NewRequest(http.MethodPost, "/api/admin/centers/ctr_1/provision-tenant", strings.NewReader(`{"company_name":"Acme","admin_password":"secret"}`))
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s, want 404 for removed cloud-side tenant provisioning route", res.Code, res.Body.String())
	}
}

func TestCenterManagementOmitsBusinessTopologyAndTaskDetails(t *testing.T) {
	repo := newHeartbeatCenterRepo(&store.Center{
		ID:                        "ctr_1",
		CompanyName:               "Legacy Acme Tenant",
		AdminEmail:                "tenant-admin@acme.example",
		AdminPhone:                "+1-555-0100",
		Address:                   "1 Customer Road",
		LegalPerson:               "Jane Customer",
		Status:                    "active",
		BaseURL:                   "https://center.example",
		SupportsMultiTenant:       true,
		TenantCount:               7,
		IWorkerTenantCount:        3,
		IWorkerRoleCount:          4,
		IWorkerColleagueCount:     5,
		IWorkerLocalAccountCount:  6,
		IWorkerAgentInstanceCount: 2,
		IWorkerReadinessStatus:    "ready",
		IWorkerReadinessJSON:      "{\"ready\":true,\"status\":\"ready\",\"tenant_count\":3,\"role_count\":4,\"colleague_count\":5,\"local_account_count\":6,\"agent_instance_count\":2,\"current_task\":\"Quarter-close approval\",\"current_detail\":\"Customer Acme revenue plan\",\"workload_summary\":{\"agent_instance_count\":2,\"active_count\":1,\"completed_count\":8,\"review_count\":1,\"blocked_count\":0}}",
	})
	svc := centers.NewService(repo, license.NewService(heartbeatLicenseRepo{}, nil))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/admin/centers/management", CenterManagementHandler(svc))

	req := httptest.NewRequest(http.MethodGet, "/api/admin/centers/management", nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", res.Code, res.Body.String())
	}
	body := res.Body.String()
	for _, forbidden := range []string{"tenant_count", "supports_multi_tenant", "role_count", "colleague_count", "local_account_count", "current_task", "current_detail", "Quarter-close", "Customer Acme"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("management response leaked %q: %s", forbidden, body)
		}
	}
	for _, expected := range []string{"tenant-admin@acme.example", "+1-555-0100", "1 Customer Road", "Jane Customer", "admin_email", "admin_phone", "legal_person", "address"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("management response missing registration review field %q: %s", expected, body)
		}
	}
	for _, expected := range []string{"workload_agent_instances", "workload_active_tasks", "workload_completed_tasks", "workload_review_tasks", "workload_blocked_tasks"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("management response missing aggregate %q: %s", expected, body)
		}
	}
}

func TestUpdateIntegrationIgnoresBusinessTopologyFields(t *testing.T) {
	repo := newHeartbeatCenterRepo(&store.Center{ID: "ctr_1", Status: "active", BaseURL: "https://old.example", SupportsMultiTenant: true, TenantCount: 7, LastSyncStatus: "configured"})
	svc := centers.NewService(repo, nil)
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/admin/centers/{id}/integration", UpdateCenterIntegrationHandler(svc))

	req := httptest.NewRequest(http.MethodPut, "/api/admin/centers/ctr_1/integration", strings.NewReader(`{"base_url":"https://center.example","supports_multi_tenant":false,"tenant_count":0,"cloud_control_mode":"cloud_managed","last_sync_status":"configured"}`))
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", res.Code, res.Body.String())
	}
	center, _ := repo.GetByID(context.Background(), "ctr_1")
	if center.TenantCount != 7 {
		t.Fatalf("TenantCount = %d, want preserved 7 when Cloud integration payload includes ignored business tenant count", center.TenantCount)
	}
	if center.BaseURL != "https://center.example" {
		t.Fatalf("center integration not updated as expected: %+v", center)
	}
	if !center.SupportsMultiTenant {
		t.Fatalf("SupportsMultiTenant = false, want legacy value preserved because Cloud no longer manages tenant topology")
	}
}

func stringSliceContains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
