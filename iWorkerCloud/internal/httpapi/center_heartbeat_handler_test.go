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

func (r *heartbeatCenterRepo) UpdateHeartbeat(_ context.Context, id string) error {
	item, ok := r.items[id]
	if !ok {
		return fmt.Errorf("not found")
	}
	item.LastHeartbeat = time.Now()
	item.LastSyncStatus = "heartbeat_ok"
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

func TestProvisionTenantHandlerRejectsCenterWithoutActiveLicense(t *testing.T) {
	repo := newHeartbeatCenterRepo(&store.Center{ID: "ctr_1", Status: "active", BaseURL: "https://center.example", SupportsMultiTenant: true})
	svc := centers.NewService(repo, license.NewService(heartbeatLicenseRepo{}, nil))
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/admin/centers/{id}/provision-tenant", ProvisionTenantHandler(svc))

	req := httptest.NewRequest(http.MethodPost, "/api/admin/centers/ctr_1/provision-tenant", strings.NewReader(`{"company_name":"Acme","email":"admin@example.com"}`))
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusConflict {
		t.Fatalf("status = %d body=%s", res.Code, res.Body.String())
	}
	var body struct {
		Error     string `json:"error"`
		Readiness struct {
			Allowed bool     `json:"allowed"`
			Issues  []string `json:"issues"`
		} `json:"readiness"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error != "CENTER_NOT_READY" {
		t.Fatalf("error = %q, want CENTER_NOT_READY", body.Error)
	}
	if body.Readiness.Allowed {
		t.Fatalf("readiness.allowed = true, want false")
	}
	if !stringSliceContains(body.Readiness.Issues, "no_active_license") {
		t.Fatalf("readiness.issues = %+v, want no_active_license", body.Readiness.Issues)
	}
}

func TestProvisionReadinessHandlerReturnsBlockingIssues(t *testing.T) {
	repo := newHeartbeatCenterRepo(&store.Center{ID: "ctr_1", Status: "active", BaseURL: "https://center.example", SupportsMultiTenant: true})
	svc := centers.NewService(repo, license.NewService(heartbeatLicenseRepo{}, nil))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/admin/centers/{id}/provision-readiness", ProvisionReadinessHandler(svc))

	req := httptest.NewRequest(http.MethodGet, "/api/admin/centers/ctr_1/provision-readiness", nil)
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

func stringSliceContains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
