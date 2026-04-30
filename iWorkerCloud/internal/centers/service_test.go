package centers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/license"
	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/store"
)

type memoryCenterRepo struct {
	items map[string]*store.Center
}

func newMemoryCenterRepo(centers ...*store.Center) *memoryCenterRepo {
	repo := &memoryCenterRepo{items: map[string]*store.Center{}}
	for _, center := range centers {
		copy := *center
		repo.items[center.ID] = &copy
	}
	return repo
}

func (m *memoryCenterRepo) Create(_ context.Context, c *store.Center) error {
	copy := *c
	m.items[c.ID] = &copy
	return nil
}

func (m *memoryCenterRepo) GetByID(_ context.Context, id string) (*store.Center, error) {
	c, ok := m.items[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	copy := *c
	return &copy, nil
}

func (m *memoryCenterRepo) List(context.Context) ([]*store.Center, error) {
	out := make([]*store.Center, 0, len(m.items))
	for _, c := range m.items {
		copy := *c
		out = append(out, &copy)
	}
	return out, nil
}

func (m *memoryCenterRepo) UpdateStatus(_ context.Context, id, status string) error {
	c, ok := m.items[id]
	if !ok {
		return fmt.Errorf("not found")
	}
	c.Status = status
	c.UpdatedAt = time.Now()
	return nil
}

func (m *memoryCenterRepo) UpdateHeartbeat(_ context.Context, id string) error {
	c, ok := m.items[id]
	if !ok {
		return fmt.Errorf("not found")
	}
	c.LastHeartbeat = time.Now()
	c.LastSyncStatus = "heartbeat_ok"
	c.UpdatedAt = c.LastHeartbeat
	return nil
}

func (m *memoryCenterRepo) UpdateIntegration(_ context.Context, c *store.Center) error {
	current, ok := m.items[c.ID]
	if !ok {
		return fmt.Errorf("not found")
	}
	current.BaseURL = c.BaseURL
	current.SupportsMultiTenant = c.SupportsMultiTenant
	current.TenantCount = c.TenantCount
	current.CloudControlMode = c.CloudControlMode
	current.LastSyncStatus = c.LastSyncStatus
	current.UpdatedAt = time.Now()
	return nil
}

func (m *memoryCenterRepo) Delete(_ context.Context, id string) error {
	delete(m.items, id)
	return nil
}

type memoryLicenseRepo struct {
	items map[string]*store.License
}

func newMemoryLicenseRepo(licenses ...*store.License) *memoryLicenseRepo {
	repo := &memoryLicenseRepo{items: map[string]*store.License{}}
	for _, lic := range licenses {
		copy := *lic
		repo.items[lic.ID] = &copy
	}
	return repo
}

func (m *memoryLicenseRepo) Create(_ context.Context, l *store.License) error {
	copy := *l
	m.items[l.ID] = &copy
	return nil
}

func (m *memoryLicenseRepo) GetByID(_ context.Context, id string) (*store.License, error) {
	lic, ok := m.items[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	copy := *lic
	return &copy, nil
}

func (m *memoryLicenseRepo) GetByCenterID(_ context.Context, centerID string) ([]*store.License, error) {
	out := []*store.License{}
	for _, lic := range m.items {
		if lic.CenterID == centerID {
			copy := *lic
			out = append(out, &copy)
		}
	}
	return out, nil
}

func (m *memoryLicenseRepo) GetActiveByCenterID(_ context.Context, centerID string) (*store.License, error) {
	now := time.Now()
	for _, lic := range m.items {
		if lic.CenterID == centerID && lic.RevokedAt == nil && (lic.IsLongTerm || lic.ExpiresAt.After(now)) {
			copy := *lic
			return &copy, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (m *memoryLicenseRepo) Revoke(_ context.Context, id string) error {
	lic, ok := m.items[id]
	if !ok {
		return fmt.Errorf("not found")
	}
	now := time.Now()
	lic.RevokedAt = &now
	return nil
}

func (m *memoryLicenseRepo) List(context.Context) ([]*store.License, error) {
	out := make([]*store.License, 0, len(m.items))
	for _, lic := range m.items {
		copy := *lic
		out = append(out, &copy)
	}
	return out, nil
}
func TestProbeVerifiesIWorkerCenterServiceIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/center/status" {
			t.Fatalf("probe path = %q, want /api/center/status", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"status":"ok","runtime_type":"service","product_kind":"iworkercenter","admin_console":"web_console","provider_count":2,"runtime_provider_mode":"cloud_sync","compute_source":"cloud","compute_permission":true,"cloud_provider_count":2,"compute_sync_status":{"status":"success","last_sync_at":"2026-04-30T00:00:00Z","provider_count":2}}`))
	}))
	defer server.Close()

	repo := newMemoryCenterRepo(&store.Center{ID: "ctr_1", BaseURL: server.URL, Status: "active", SupportsMultiTenant: true})
	svc := NewService(repo, nil)

	result, center, err := svc.Probe(context.Background(), "ctr_1")
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if !result.OK || result.RuntimeType != "service" || result.ProductKind != "iworkercenter" || result.AdminConsole != "web_console" {
		t.Fatalf("result = %+v", result)
	}
	if result.RuntimeProviderMode != "cloud_sync" || result.ComputeSource != "cloud" || !result.ComputePermission {
		t.Fatalf("compute runtime result = %+v", result)
	}
	if result.ProviderCount != 2 || result.CloudProviderCount != 2 {
		t.Fatalf("provider counts = runtime:%d cloud:%d, want 2/2", result.ProviderCount, result.CloudProviderCount)
	}
	if result.ComputeSyncStatus == nil || result.ComputeSyncStatus.Status != "success" || result.ComputeSyncStatus.ProviderCount != 2 {
		t.Fatalf("ComputeSyncStatus = %+v, want success/2", result.ComputeSyncStatus)
	}
	if center.LastSyncStatus != "probe_ok" {
		t.Fatalf("LastSyncStatus = %q, want probe_ok", center.LastSyncStatus)
	}
}

func TestProbeRejectsNonIWorkerCenterEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok","runtime_type":"desktop","product_kind":"iworker","admin_console":"desktop_gui"}`))
	}))
	defer server.Close()

	repo := newMemoryCenterRepo(&store.Center{ID: "ctr_1", BaseURL: server.URL, Status: "active", SupportsMultiTenant: true, LastSyncStatus: "configured"})
	svc := NewService(repo, nil)

	result, center, err := svc.Probe(context.Background(), "ctr_1")
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if result.OK {
		t.Fatalf("expected non-center endpoint to fail, got %+v", result)
	}
	if center.LastSyncStatus != "probe_not_iworkercenter" {
		t.Fatalf("LastSyncStatus = %q, want probe_not_iworkercenter", center.LastSyncStatus)
	}
	management := buildCenterManagement(center, nil)
	if !containsIssue(management.Issues, "probe_not_iworkercenter") {
		t.Fatalf("issues = %+v", management.Issues)
	}
	found := false
	for _, action := range management.RecommendedActions {
		if action.Code == "verify_center_service_identity" {
			found = true
		}
	}
	if !found {
		t.Fatalf("recommended_actions = %+v", management.RecommendedActions)
	}
}

func TestHeartbeatRequiresIWorkerCenterServiceIdentity(t *testing.T) {
	repo := newMemoryCenterRepo(&store.Center{ID: "ctr_1", Status: "active", SecretHash: hashSecret("secret-abc"), LastSyncStatus: "configured"})
	svc := NewService(repo, nil)

	err := svc.Heartbeat(context.Background(), "ctr_1", HeartbeatRequest{
		Secret:       "secret-abc",
		RuntimeType:  "desktop",
		ProductKind:  "iworker",
		AdminConsole: "desktop_gui",
	})
	if err != ErrInvalidServiceIdentity {
		t.Fatalf("Heartbeat() error = %v, want ErrInvalidServiceIdentity", err)
	}
	center, _ := repo.GetByID(context.Background(), "ctr_1")
	if !center.LastHeartbeat.IsZero() {
		t.Fatalf("LastHeartbeat should not be updated for invalid identity: %v", center.LastHeartbeat)
	}
	if center.LastSyncStatus != "heartbeat_not_iworkercenter" {
		t.Fatalf("LastSyncStatus = %q, want heartbeat_not_iworkercenter", center.LastSyncStatus)
	}
}

func TestHeartbeatAcceptsIWorkerCenterServiceIdentity(t *testing.T) {
	repo := newMemoryCenterRepo(&store.Center{ID: "ctr_1", Status: "active", SecretHash: hashSecret("secret-abc"), LastSyncStatus: "configured"})
	svc := NewService(repo, nil)

	err := svc.Heartbeat(context.Background(), "ctr_1", HeartbeatRequest{
		Secret:       "secret-abc",
		RuntimeType:  "service",
		ProductKind:  "iworkercenter",
		AdminConsole: "web_console",
	})
	if err != nil {
		t.Fatalf("Heartbeat() error: %v", err)
	}
	center, _ := repo.GetByID(context.Background(), "ctr_1")
	if center.LastHeartbeat.IsZero() {
		t.Fatal("LastHeartbeat was not updated")
	}
	if center.LastSyncStatus != "heartbeat_ok" {
		t.Fatalf("LastSyncStatus = %q, want heartbeat_ok", center.LastSyncStatus)
	}
}

func TestProvisionReadinessRejectsUnlicensedCenter(t *testing.T) {
	repo := newMemoryCenterRepo(&store.Center{ID: "ctr_1", Status: "active", BaseURL: "https://center.example", SupportsMultiTenant: true})
	svc := NewService(repo, license.NewService(newMemoryLicenseRepo(), nil))

	readiness, err := svc.ProvisionReadiness(context.Background(), "ctr_1")
	if err != nil {
		t.Fatalf("ProvisionReadiness() error: %v", err)
	}
	if readiness.Allowed {
		t.Fatalf("readiness.Allowed = true, want false")
	}
	if !containsIssue(readiness.Issues, "no_active_license") {
		t.Fatalf("issues = %+v, want no_active_license", readiness.Issues)
	}
	if _, err := svc.EnsureProvisionAllowed(context.Background(), "ctr_1"); !errors.Is(err, ErrProvisionNotAllowed) {
		t.Fatalf("EnsureProvisionAllowed() error = %v, want ErrProvisionNotAllowed", err)
	}
}

func TestProvisionReadinessAllowsLicensedMultiTenantCenter(t *testing.T) {
	repo := newMemoryCenterRepo(&store.Center{ID: "ctr_1", Status: "active", BaseURL: "https://center.example", SupportsMultiTenant: true, LastSyncStatus: "probe_ok"})
	licenses := newMemoryLicenseRepo(&store.License{ID: "lic_1", CenterID: "ctr_1", ExpiresAt: time.Now().Add(time.Hour), CreatedAt: time.Now()})
	svc := NewService(repo, license.NewService(licenses, nil))

	readiness, err := svc.ProvisionReadiness(context.Background(), "ctr_1")
	if err != nil {
		t.Fatalf("ProvisionReadiness() error: %v", err)
	}
	if !readiness.Allowed {
		t.Fatalf("readiness.Allowed = false, issues=%+v", readiness.Issues)
	}
	center, err := svc.EnsureProvisionAllowed(context.Background(), "ctr_1")
	if err != nil {
		t.Fatalf("EnsureProvisionAllowed() error: %v", err)
	}
	if center.ID != "ctr_1" {
		t.Fatalf("center.ID = %q, want ctr_1", center.ID)
	}
}

func TestProvisionReadinessRejectsUnverifiedServiceIdentity(t *testing.T) {
	repo := newMemoryCenterRepo(&store.Center{ID: "ctr_1", Status: "active", BaseURL: "https://center.example", SupportsMultiTenant: true, LastSyncStatus: "configured"})
	licenses := newMemoryLicenseRepo(&store.License{ID: "lic_1", CenterID: "ctr_1", ExpiresAt: time.Now().Add(time.Hour), CreatedAt: time.Now()})
	svc := NewService(repo, license.NewService(licenses, nil))

	readiness, err := svc.ProvisionReadiness(context.Background(), "ctr_1")
	if err != nil {
		t.Fatalf("ProvisionReadiness() error: %v", err)
	}
	if readiness.Allowed {
		t.Fatalf("readiness.Allowed = true, want false")
	}
	if !containsIssue(readiness.Issues, "service_identity_not_verified") {
		t.Fatalf("issues = %+v, want service_identity_not_verified", readiness.Issues)
	}
	management := buildCenterManagement(readiness.Center, readiness.ActiveLicense)
	if management.Ready {
		t.Fatalf("management.Ready = true, want false")
	}
	if management.ManagementPosture != "needs_setup" {
		t.Fatalf("ManagementPosture = %q, want needs_setup", management.ManagementPosture)
	}
}
