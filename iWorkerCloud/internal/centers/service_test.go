package centers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/license"
	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/store"
)

type memoryCenterRepo struct {
	items                map[string]*store.Center
	updateIntegrationErr error
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
		return nil, sql.ErrNoRows
	}
	copy := *c
	return &copy, nil
}

func (m *memoryCenterRepo) GetByRegistrationKey(_ context.Context, machineID, companyID string) (*store.Center, error) {
	for _, c := range m.items {
		if c.MachineID == machineID && c.CompanyID == companyID {
			copy := *c
			return &copy, nil
		}
	}
	return nil, sql.ErrNoRows
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

func (m *memoryCenterRepo) UpdateHeartbeat(_ context.Context, c *store.Center) error {
	current, ok := m.items[c.ID]
	if !ok {
		return fmt.Errorf("not found")
	}
	current.LastHeartbeat = time.Now()
	current.LastSyncStatus = c.LastSyncStatus
	current.IWorkerReady = c.IWorkerReady
	current.IWorkerReadinessStatus = c.IWorkerReadinessStatus
	current.IWorkerTenantCount = c.IWorkerTenantCount
	current.IWorkerRoleCount = c.IWorkerRoleCount
	current.IWorkerColleagueCount = c.IWorkerColleagueCount
	current.IWorkerLocalAccountCount = c.IWorkerLocalAccountCount
	current.IWorkerAgentInstanceCount = c.IWorkerAgentInstanceCount
	current.IWorkerReadinessJSON = c.IWorkerReadinessJSON
	current.RuntimeStatusJSON = c.RuntimeStatusJSON
	current.UpdatedAt = current.LastHeartbeat
	return nil
}

func (m *memoryCenterRepo) UpdateIntegration(_ context.Context, c *store.Center) error {
	if m.updateIntegrationErr != nil {
		return m.updateIntegrationErr
	}
	current, ok := m.items[c.ID]
	if !ok {
		return fmt.Errorf("not found")
	}
	current.BaseURL = c.BaseURL
	current.SupportsMultiTenant = c.SupportsMultiTenant
	current.TenantCount = c.TenantCount
	current.CloudControlMode = c.CloudControlMode
	current.LastSyncStatus = c.LastSyncStatus
	current.IWorkerReady = c.IWorkerReady
	current.IWorkerReadinessStatus = c.IWorkerReadinessStatus
	current.IWorkerTenantCount = c.IWorkerTenantCount
	current.IWorkerRoleCount = c.IWorkerRoleCount
	current.IWorkerColleagueCount = c.IWorkerColleagueCount
	current.IWorkerLocalAccountCount = c.IWorkerLocalAccountCount
	current.IWorkerAgentInstanceCount = c.IWorkerAgentInstanceCount
	current.IWorkerReadinessJSON = c.IWorkerReadinessJSON
	current.RuntimeStatusJSON = c.RuntimeStatusJSON
	current.UpdatedAt = time.Now()
	return nil
}

func (m *memoryCenterRepo) UpdateRegistration(_ context.Context, c *store.Center) error {
	current, ok := m.items[c.ID]
	if !ok {
		return fmt.Errorf("not found")
	}
	current.MachineID = c.MachineID
	current.CompanyID = c.CompanyID
	current.CompanyName = c.CompanyName
	current.AdminEmail = c.AdminEmail
	current.AdminPhone = c.AdminPhone
	current.Address = c.Address
	current.LegalPerson = c.LegalPerson
	current.BaseURL = c.BaseURL
	current.SupportsMultiTenant = c.SupportsMultiTenant
	current.TenantCount = c.TenantCount
	current.CloudControlMode = c.CloudControlMode
	current.LastSyncStatus = c.LastSyncStatus
	current.SecretHash = c.SecretHash
	current.ManagementSecret = c.ManagementSecret
	current.UpdatedAt = time.Now()
	return nil
}

func (m *memoryCenterRepo) Delete(_ context.Context, id string) error {
	delete(m.items, id)
	return nil
}

type memoryLicenseRepo struct {
	items     map[string]*store.License
	activeErr error
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
	if m.activeErr != nil {
		return nil, m.activeErr
	}
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
		_, _ = w.Write([]byte(`{"status":"ok","runtime_type":"service","product_kind":"iworkercenter","admin_console":"web_console","provider_count":2,"runtime_provider_mode":"cloud_sync","compute_source":"cloud","compute_permission":true,"cloud_provider_count":2,"compute_sync_status":{"status":"success","last_sync_at":"2026-04-30T00:00:00Z","provider_count":2},"iworker_readiness":{"ready":true,"status":"ready","tenant_count":1,"role_count":2,"colleague_count":3,"local_account_count":4,"agent_instance_count":5}}`))
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
	if !center.IWorkerReady || center.IWorkerReadinessStatus != "ready" || center.IWorkerColleagueCount != 0 || center.IWorkerLocalAccountCount != 0 || center.IWorkerAgentInstanceCount != 5 {
		t.Fatalf("stored iWorker readiness = %+v", center)
	}
	if result.IWorkerReadiness == nil || result.IWorkerReadiness.AgentInstanceCount != 5 {
		t.Fatalf("probe iWorker readiness = %+v", result.IWorkerReadiness)
	}
	rawResult, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal probe result: %v", err)
	}
	for _, forbidden := range []string{"tenant_count", "role_count", "colleague_count", "local_account_count", "required_client_paths", "checks", "auth_methods"} {
		if strings.Contains(string(rawResult), forbidden) {
			t.Fatalf("probe result leaked %q: %s", forbidden, rawResult)
		}
	}
}

func TestProbeCapturesNonBlockingLocalFallbackStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/center/status" {
			t.Fatalf("probe path = %q, want /api/center/status", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"status":"ok","runtime_type":"service","product_kind":"iworkercenter","admin_console":"web_console","provider_count":1,"runtime_provider_mode":"local_settings_fallback","compute_source":"cloud","compute_permission":false,"cloud_provider_count":0,"compute_sync_status":{"status":"failure","error":"cloud unavailable","provider_count":0,"non_blocking":true,"runtime_impact":"local_settings_fallback"},"cloud_heartbeat":{"configured":true,"status":"degraded","consecutive_failures":1,"non_blocking":true,"business_impact":"none_local_center_and_iworker_continue"},"iworker_readiness":{"ready":true,"status":"ready","agent_instance_count":2,"workload_summary":{"agent_instance_count":2,"active_count":1,"completed_count":3,"review_count":1,"blocked_count":0}}}`))
	}))
	defer server.Close()

	repo := newMemoryCenterRepo(&store.Center{ID: "ctr_1", BaseURL: server.URL, Status: "active", SupportsMultiTenant: true})
	svc := NewService(repo, nil)

	result, center, err := svc.Probe(context.Background(), "ctr_1")
	if err != nil {
		t.Fatalf("Probe() error: %v", err)
	}
	if !result.OK || center.LastSyncStatus != "probe_ok" {
		t.Fatalf("probe result=%+v center=%+v", result, center)
	}
	if result.RuntimeProviderMode != "local_settings_fallback" || result.CloudProviderCount != 0 {
		t.Fatalf("runtime fallback not captured: %+v", result)
	}
	if result.ComputeSyncStatus == nil || !result.ComputeSyncStatus.NonBlocking || result.ComputeSyncStatus.RuntimeImpact != "local_settings_fallback" {
		t.Fatalf("ComputeSyncStatus = %+v, want non-blocking local fallback", result.ComputeSyncStatus)
	}
	if result.CloudHeartbeat == nil || !result.CloudHeartbeat.NonBlocking || result.CloudHeartbeat.BusinessImpact != "none_local_center_and_iworker_continue" {
		t.Fatalf("CloudHeartbeat = %+v, want non-blocking local continuity", result.CloudHeartbeat)
	}
	if center.RuntimeStatusJSON == "" {
		t.Fatal("probe did not persist runtime status")
	}
	management := buildCenterManagement(center, &store.License{ID: "lic_1", CenterID: "ctr_1", ExpiresAt: time.Now().Add(time.Hour), CreatedAt: time.Now()})
	if management.RuntimeStatus == nil || management.RuntimeStatus.RuntimeProviderMode != "local_settings_fallback" || management.RuntimeStatus.ComputeSyncStatus == nil || !management.RuntimeStatus.ComputeSyncStatus.NonBlocking {
		t.Fatalf("management runtime status = %+v", management.RuntimeStatus)
	}
	if management.RuntimeStatus.CloudHeartbeat == nil || !management.RuntimeStatus.CloudHeartbeat.NonBlocking {
		t.Fatalf("management cloud heartbeat = %+v, want non-blocking", management.RuntimeStatus.CloudHeartbeat)
	}
	rawResult, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal probe result: %v", err)
	}
	for _, forbidden := range []string{"current_task", "current_detail", "task_title", "business_payload"} {
		if strings.Contains(string(rawResult), forbidden) {
			t.Fatalf("fallback probe leaked business field %q: %s", forbidden, rawResult)
		}
	}
}

func TestRuntimeSnapshotDoesNotMutateCenterStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok","runtime_type":"service","product_kind":"iworkercenter","admin_console":"web_console","provider_count":1,"runtime_provider_mode":"cloud_sync","compute_source":"cloud","cloud_provider_count":1,"compute_sync_status":{"status":"success","provider_count":1}}`))
	}))
	defer server.Close()

	repo := newMemoryCenterRepo(&store.Center{ID: "ctr_1", BaseURL: server.URL, Status: "active", SupportsMultiTenant: true, LastSyncStatus: "configured"})
	svc := NewService(repo, nil)

	result, err := svc.RuntimeSnapshot(context.Background(), "ctr_1")
	if err != nil {
		t.Fatalf("RuntimeSnapshot() error: %v", err)
	}
	if !result.OK || result.RuntimeProviderMode != "cloud_sync" || result.ProviderCount != 1 {
		t.Fatalf("result = %+v, want cloud runtime snapshot", result)
	}
	center, _ := repo.GetByID(context.Background(), "ctr_1")
	if center.LastSyncStatus != "configured" {
		t.Fatalf("LastSyncStatus = %q, want unchanged configured", center.LastSyncStatus)
	}
}

func TestProbeFailsWhenResultCannotBePersisted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok","runtime_type":"service","product_kind":"iworkercenter","admin_console":"web_console"}`))
	}))
	defer server.Close()

	repo := newMemoryCenterRepo(&store.Center{ID: "ctr_1", BaseURL: server.URL, Status: "active", LastSyncStatus: "configured"})
	repo.updateIntegrationErr = errors.New("database locked")
	svc := NewService(repo, nil)

	result, center, err := svc.Probe(context.Background(), "ctr_1")
	if err == nil || !strings.Contains(err.Error(), "record probe result") {
		t.Fatalf("Probe() error = %v, want persistence error", err)
	}
	if result != nil || center != nil {
		t.Fatalf("Probe() returned result=%+v center=%+v despite persistence failure", result, center)
	}
	stored, _ := repo.GetByID(context.Background(), "ctr_1")
	if stored.LastSyncStatus != "configured" {
		t.Fatalf("LastSyncStatus = %q, want unchanged configured", stored.LastSyncStatus)
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
	if !errors.Is(err, ErrInvalidServiceIdentity) {
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

func TestHeartbeatInvalidIdentityFailsWhenStatusCannotBeRecorded(t *testing.T) {
	repo := newMemoryCenterRepo(&store.Center{ID: "ctr_1", Status: "active", SecretHash: hashSecret("secret-abc"), LastSyncStatus: "configured"})
	repo.updateIntegrationErr = errors.New("disk full")
	svc := NewService(repo, nil)

	err := svc.Heartbeat(context.Background(), "ctr_1", HeartbeatRequest{
		Secret:       "secret-abc",
		RuntimeType:  "desktop",
		ProductKind:  "iworker",
		AdminConsole: "desktop_gui",
	})
	if !errors.Is(err, ErrInvalidServiceIdentity) || !strings.Contains(err.Error(), "failed to record service identity failure") {
		t.Fatalf("Heartbeat() error = %v, want wrapped identity persistence error", err)
	}
	center, _ := repo.GetByID(context.Background(), "ctr_1")
	if center.LastSyncStatus != "configured" {
		t.Fatalf("LastSyncStatus = %q, want unchanged configured", center.LastSyncStatus)
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

func TestHeartbeatStoresRuntimeContinuityWithoutBusinessData(t *testing.T) {
	repo := newMemoryCenterRepo(&store.Center{ID: "ctr_1", Status: "active", SecretHash: hashSecret("secret-abc"), LastSyncStatus: "configured"})
	svc := NewService(repo, nil)

	err := svc.Heartbeat(context.Background(), "ctr_1", HeartbeatRequest{
		Secret:              "secret-abc",
		RuntimeType:         "service",
		ProductKind:         "iworkercenter",
		AdminConsole:        "web_console",
		ProviderCount:       1,
		RuntimeProviderMode: "local_settings_fallback",
		ComputeSource:       "cloud",
		CloudProviderCount:  0,
		ComputeSyncStatus:   &centerComputeSyncStatus{Status: "failure", Error: "cloud unavailable", ProviderCount: 0, NonBlocking: true, RuntimeImpact: "local_settings_fallback"},
		CloudHeartbeat:      &centerCloudHeartbeat{Enabled: true, Status: "degraded", FailureCount: 2, NonBlocking: true, BusinessImpact: "none_local_center_and_iworker_continue"},
	})
	if err != nil {
		t.Fatalf("Heartbeat() error: %v", err)
	}
	center, _ := repo.GetByID(context.Background(), "ctr_1")
	if center.RuntimeStatusJSON == "" {
		t.Fatal("RuntimeStatusJSON was not stored")
	}
	management := buildCenterManagement(center, &store.License{ID: "lic_1", CenterID: "ctr_1", ExpiresAt: time.Now().Add(time.Hour), CreatedAt: time.Now()})
	if management.RuntimeStatus == nil || management.RuntimeStatus.RuntimeProviderMode != "local_settings_fallback" || management.RuntimeStatus.ComputeSyncStatus == nil || !management.RuntimeStatus.ComputeSyncStatus.NonBlocking {
		t.Fatalf("management runtime status = %+v", management.RuntimeStatus)
	}
	if management.RuntimeStatus.CloudHeartbeat == nil || !management.RuntimeStatus.CloudHeartbeat.NonBlocking || management.RuntimeStatus.CloudHeartbeat.BusinessImpact != "none_local_center_and_iworker_continue" {
		t.Fatalf("cloud heartbeat status = %+v, want non-blocking local continuity", management.RuntimeStatus.CloudHeartbeat)
	}
	for _, forbidden := range []string{"current_task", "current_detail", "tenant_count", "role_count", "business_payload"} {
		if strings.Contains(center.RuntimeStatusJSON, forbidden) {
			t.Fatalf("runtime heartbeat leaked %q: %s", forbidden, center.RuntimeStatusJSON)
		}
	}
}

func TestHeartbeatStoresIWorkerReadiness(t *testing.T) {
	repo := newMemoryCenterRepo(&store.Center{ID: "ctr_1", Status: "active", SecretHash: hashSecret("secret-abc"), LastSyncStatus: "configured"})
	svc := NewService(repo, nil)

	err := svc.Heartbeat(context.Background(), "ctr_1", HeartbeatRequest{
		Secret:       "secret-abc",
		RuntimeType:  "service",
		ProductKind:  "iworkercenter",
		AdminConsole: "web_console",
		IWorkerReadiness: &IWorkerReadinessReport{
			Ready:               false,
			Status:              "needs_bootstrap",
			AgentInstanceCount:  4,
			RequiredClientPaths: []string{"/client/iworker/instances"},
			Checks:              []ReadinessItem{{Name: "tenant", Ready: true, Status: "ready", Count: 3}, {Name: "agent_runtime", Ready: true, Status: "ready"}},
			AuthMethods:         []AuthItem{{Method: "local", Label: "Local account", Ready: true, Implemented: true, Status: "ready"}},
		},
	})
	if err != nil {
		t.Fatalf("Heartbeat() error: %v", err)
	}
	center, _ := repo.GetByID(context.Background(), "ctr_1")
	if center.IWorkerReady || center.IWorkerReadinessStatus != "needs_bootstrap" || center.IWorkerRoleCount != 0 || center.IWorkerLocalAccountCount != 0 || center.IWorkerAgentInstanceCount != 4 {
		t.Fatalf("stored sanitized readiness = %+v", center)
	}
	if center.IWorkerReadinessJSON == "" {
		t.Fatal("IWorkerReadinessJSON was not stored")
	}
	for _, forbidden := range []string{"required_client_paths", "checks", "auth_methods", "/client/iworker/instances", "tenant", "local"} {
		if strings.Contains(center.IWorkerReadinessJSON, forbidden) {
			t.Fatalf("stored readiness leaked %q: %s", forbidden, center.IWorkerReadinessJSON)
		}
	}
	management := buildCenterManagement(center, &store.License{ID: "lic_1", CenterID: "ctr_1", ExpiresAt: time.Now().Add(time.Hour), CreatedAt: time.Now()})
	if containsIssue(management.Issues, "iworker_readiness_incomplete") {
		t.Fatalf("iWorker readiness should be observed but not block Cloud service readiness: %+v", management.Issues)
	}
	if management.IWorkerOperationalReady {
		t.Fatalf("IWorkerOperationalReady = true, want false")
	}
	if management.IWorkerReadiness == nil || management.IWorkerReadiness.Status != "needs_bootstrap" {
		t.Fatalf("management.IWorkerReadiness = %+v", management.IWorkerReadiness)
	}
}

func TestManagementDoesNotRequireIWorkerBusinessReadinessAfterServiceIdentityVerified(t *testing.T) {
	center := &store.Center{ID: "ctr_1", Status: "active", BaseURL: "https://center.example", SupportsMultiTenant: false, LastSyncStatus: "heartbeat_ok"}
	management := buildCenterManagement(center, &store.License{ID: "lic_1", CenterID: "ctr_1", ExpiresAt: time.Now().Add(time.Hour), CreatedAt: time.Now()})
	if !management.Ready {
		t.Fatalf("management.Ready = false, issues=%+v; Cloud service readiness must stay isolated from Center business setup", management.Issues)
	}
	if containsIssue(management.Issues, "iworker_readiness_not_reported") || containsIssue(management.Issues, "multi_tenant_not_confirmed") {
		t.Fatalf("business readiness leaked into Cloud management issues: %+v", management.Issues)
	}
}

func TestManagementSummaryCountsRuntimeFallbackWithoutBusinessLeak(t *testing.T) {
	runtimeJSON := `{"provider_count":1,"runtime_provider_mode":"local_settings_fallback","compute_source":"cloud","cloud_provider_count":0,"compute_sync_status":{"status":"failure","provider_count":0,"non_blocking":true,"runtime_impact":"local_settings_fallback"},"cloud_heartbeat":{"configured":true,"status":"degraded","consecutive_failures":2,"non_blocking":true,"business_impact":"none_local_center_and_iworker_continue"}}`
	repo := newMemoryCenterRepo(&store.Center{
		ID:                "ctr_1",
		Status:            "active",
		BaseURL:           "https://center.example",
		LastSyncStatus:    "heartbeat_ok",
		RuntimeStatusJSON: runtimeJSON,
	})
	licenses := newMemoryLicenseRepo(&store.License{ID: "lic_1", CenterID: "ctr_1", ExpiresAt: time.Now().Add(time.Hour), CreatedAt: time.Now()})
	svc := NewService(repo, license.NewService(licenses, nil))

	report, err := svc.Management(context.Background())
	if err != nil {
		t.Fatalf("Management() error: %v", err)
	}
	if report.Summary.RuntimeFallbackCenters != 1 || report.Summary.RuntimeNonBlockingIssues != 1 || report.Summary.RuntimeBlockingIssues != 0 {
		t.Fatalf("runtime summary = %+v", report.Summary)
	}
	if report.Summary.HeartbeatDegradedCenters != 1 || report.Summary.HeartbeatBlockingIssues != 0 {
		t.Fatalf("heartbeat summary = %+v", report.Summary)
	}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	for _, forbidden := range []string{"current_task", "current_detail", "tenant_count", "role_count", "business_payload"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("management report leaked %q: %s", forbidden, raw)
		}
	}
}

func TestServiceReadinessRejectsUnlicensedCenter(t *testing.T) {
	repo := newMemoryCenterRepo(&store.Center{ID: "ctr_1", Status: "active", BaseURL: "https://center.example", SupportsMultiTenant: true})
	svc := NewService(repo, license.NewService(newMemoryLicenseRepo(), nil))

	readiness, err := svc.ServiceReadiness(context.Background(), "ctr_1")
	if err != nil {
		t.Fatalf("ServiceReadiness() error: %v", err)
	}
	if readiness.Allowed {
		t.Fatalf("readiness.Allowed = true, want false")
	}
	if !containsIssue(readiness.Issues, "no_active_license") {
		t.Fatalf("issues = %+v, want no_active_license", readiness.Issues)
	}
	if _, err := svc.EnsureServiceManagementAllowed(context.Background(), "ctr_1"); !errors.Is(err, ErrServiceManagementNotAllowed) {
		t.Fatalf("EnsureServiceManagementAllowed() error = %v, want ErrServiceManagementNotAllowed", err)
	}
}

func TestServiceReadinessReturnsLicenseStoreErrors(t *testing.T) {
	repo := newMemoryCenterRepo(&store.Center{ID: "ctr_1", Status: "active", BaseURL: "https://center.example", SupportsMultiTenant: true, LastSyncStatus: "probe_ok"})
	licenses := newMemoryLicenseRepo()
	licenses.activeErr = errors.New("database locked")
	svc := NewService(repo, license.NewService(licenses, nil))

	readiness, err := svc.ServiceReadiness(context.Background(), "ctr_1")
	if err == nil || !strings.Contains(err.Error(), "load active license") {
		t.Fatalf("ServiceReadiness() error = %v, want license store error", err)
	}
	if readiness != nil {
		t.Fatalf("readiness = %+v, want nil on license store error", readiness)
	}
}

func TestServiceReadinessAllowsLicensedCenter(t *testing.T) {
	repo := newMemoryCenterRepo(&store.Center{ID: "ctr_1", Status: "active", BaseURL: "https://center.example", SupportsMultiTenant: true, LastSyncStatus: "probe_ok"})
	licenses := newMemoryLicenseRepo(&store.License{ID: "lic_1", CenterID: "ctr_1", ExpiresAt: time.Now().Add(time.Hour), CreatedAt: time.Now()})
	svc := NewService(repo, license.NewService(licenses, nil))

	readiness, err := svc.ServiceReadiness(context.Background(), "ctr_1")
	if err != nil {
		t.Fatalf("ServiceReadiness() error: %v", err)
	}
	if !readiness.Allowed {
		t.Fatalf("readiness.Allowed = false, issues=%+v", readiness.Issues)
	}
	center, err := svc.EnsureServiceManagementAllowed(context.Background(), "ctr_1")
	if err != nil {
		t.Fatalf("EnsureServiceManagementAllowed() error: %v", err)
	}
	if center.ID != "ctr_1" {
		t.Fatalf("center.ID = %q, want ctr_1", center.ID)
	}
}

func TestServiceReadinessRejectsUnverifiedServiceIdentity(t *testing.T) {
	repo := newMemoryCenterRepo(&store.Center{ID: "ctr_1", Status: "active", BaseURL: "https://center.example", SupportsMultiTenant: true, LastSyncStatus: "configured"})
	licenses := newMemoryLicenseRepo(&store.License{ID: "lic_1", CenterID: "ctr_1", ExpiresAt: time.Now().Add(time.Hour), CreatedAt: time.Now()})
	svc := NewService(repo, license.NewService(licenses, nil))

	readiness, err := svc.ServiceReadiness(context.Background(), "ctr_1")
	if err != nil {
		t.Fatalf("ServiceReadiness() error: %v", err)
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

func TestRegisterReusesExistingCenterByMachineAndCompany(t *testing.T) {
	repo := newMemoryCenterRepo()
	svc := NewService(repo, nil)
	ctx := context.Background()

	first, err := svc.Register(ctx, RegisterRequest{MachineID: "machine-1", CompanyID: "company-1", CompanyName: "Acme", AdminEmail: "admin@example.com"})
	if err != nil {
		t.Fatalf("first register: %v", err)
	}
	second, err := svc.Register(ctx, RegisterRequest{MachineID: "machine-1", CompanyID: "company-1", CompanyName: "Acme Updated", AdminEmail: "new@example.com", BaseURL: "https://center.example.com"})
	if err != nil {
		t.Fatalf("second register: %v", err)
	}
	if !second.Reused || second.CenterID != first.CenterID || second.Secret != first.Secret {
		t.Fatalf("registration was not reused: first=%+v second=%+v", first, second)
	}
	center, err := repo.GetByID(ctx, first.CenterID)
	if err != nil {
		t.Fatalf("get center: %v", err)
	}
	if center.CompanyName != "Acme Updated" || center.AdminEmail != "new@example.com" || center.BaseURL != "https://center.example.com" {
		t.Fatalf("existing center metadata not refreshed: %+v", center)
	}
}

func TestRegisterRequiresMachineAndCompanyIdentity(t *testing.T) {
	repo := newMemoryCenterRepo()
	svc := NewService(repo, nil)
	ctx := context.Background()

	cases := []RegisterRequest{
		{CompanyID: "company-1", CompanyName: "Acme", AdminEmail: "admin@example.com"},
		{MachineID: "machine-1", CompanyName: "Acme", AdminEmail: "admin@example.com"},
	}
	for _, req := range cases {
		if _, err := svc.Register(ctx, req); err == nil || !strings.Contains(err.Error(), "machine_id and company_id") {
			t.Fatalf("Register(%+v) error = %v, want machine/company identity error", req, err)
		}
	}
	if len(repo.items) != 0 {
		t.Fatalf("unexpected centers created: %+v", repo.items)
	}
}

func TestRegisterRepairsMissingManagementSecretWhenReusingRegistration(t *testing.T) {
	repo := newMemoryCenterRepo(&store.Center{
		ID:          "ctr_existing",
		MachineID:   "machine-1",
		CompanyID:   "company-1",
		CompanyName: "Legacy Acme",
		AdminEmail:  "old@example.com",
		Status:      "pending",
	})
	svc := NewService(repo, nil)
	ctx := context.Background()

	result, err := svc.Register(ctx, RegisterRequest{MachineID: "machine-1", CompanyID: "company-1", CompanyName: "Acme", AdminEmail: "admin@example.com"})
	if err != nil {
		t.Fatalf("Register() error: %v", err)
	}
	if !result.Reused || result.CenterID != "ctr_existing" || strings.TrimSpace(result.Secret) == "" {
		t.Fatalf("result = %+v, want reused existing registration with repaired secret", result)
	}
	center, err := repo.GetByID(ctx, "ctr_existing")
	if err != nil {
		t.Fatalf("GetByID() error: %v", err)
	}
	if center.ManagementSecret != result.Secret || center.SecretHash != hashSecret(result.Secret) {
		t.Fatalf("secret not repaired on existing center: center=%+v result=%+v", center, result)
	}
}

func TestDeleteReturnsNotFoundForMissingCenter(t *testing.T) {
	repo := newMemoryCenterRepo()
	svc := NewService(repo, nil)

	err := svc.Delete(context.Background(), "missing-center")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete() error = %v, want ErrNotFound", err)
	}
}
