package httpapi

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type fakePlatformTenantRepo struct {
	items []*store.Tenant
}

func (f fakePlatformTenantRepo) Create(ctx context.Context, tenant *store.Tenant) error {
	_ = ctx
	_ = tenant
	return nil
}

func (f fakePlatformTenantRepo) DeleteByID(ctx context.Context, id string) error {
	_ = ctx
	_ = id
	return nil
}
func (f fakePlatformTenantRepo) GetByID(ctx context.Context, id string) (*store.Tenant, error) {
	_ = ctx
	for _, item := range f.items {
		if item != nil && item.ID == id {
			return item, nil
		}
	}
	return nil, nil
}

func (f fakePlatformTenantRepo) GetBySlug(ctx context.Context, slug string) (*store.Tenant, error) {
	_ = ctx
	for _, item := range f.items {
		if item != nil && item.Slug == slug {
			return item, nil
		}
	}
	return nil, nil
}

func (f fakePlatformTenantRepo) List(ctx context.Context) ([]*store.Tenant, error) {
	_ = ctx
	return f.items, nil
}

func (f fakePlatformTenantRepo) EnsureDefault(ctx context.Context) (*store.Tenant, error) {
	_ = ctx
	if len(f.items) > 0 {
		return f.items[0], nil
	}
	return &store.Tenant{ID: store.DefaultTenantID, Slug: "default", Name: "Default", Status: "active"}, nil
}

type failingPlatformSettingsRepo struct {
	raw string
}

func (f failingPlatformSettingsRepo) Get(ctx context.Context, key string) (string, error) {
	_ = ctx
	_ = key
	return f.raw, nil
}

func (f failingPlatformSettingsRepo) Set(ctx context.Context, key, valueJSON string) error {
	_ = ctx
	_ = key
	_ = valueJSON
	return errors.New("set failed")
}

type fakePlatformMachineSender struct {
	calls int
	err   error
}

func (f *fakePlatformMachineSender) SendToMachine(machineID string, msg any) error {
	_ = machineID
	_ = msg
	f.calls++
	return f.err
}

func TestPlatformAwareMachineSenderPrefersPlatformCallback(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	callbackCalls := 0
	callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/a2a/employees/platform-employee-1/messages" {
			t.Fatalf("unexpected callback path %s", r.URL.Path)
		}
		if got := r.Header.Get("X-VE-Callback-Secret"); got != "secret-1" {
			t.Fatalf("unexpected callback secret %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode callback body: %v", err)
		}
		if body["payload"] == nil {
			t.Fatalf("callback body missing payload: %#v", body)
		}
		callbackCalls++
		w.WriteHeader(http.StatusAccepted)
	}))
	defer callback.Close()

	provider := platformProviderEntry{PlatformID: "platform-1", CallbackBaseURL: callback.URL, CallbackSecret: "secret-1", RegistrationStatus: "active"}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "ve_employee_1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", Status: veStatusActive, OnlineStatus: "platform", RegisteredAt: time.Now().UTC().Format(time.RFC3339)}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}

	fallback := &fakePlatformMachineSender{err: nil}
	sender := platformAwareMachineSender{fallback: fallback, system: settings, tenants: fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}}
	if err := sender.SendToMachine("ve_employee_1", map[string]any{"type": "discussion.message"}); err != nil {
		t.Fatalf("SendToMachine returned error: %v", err)
	}
	if callbackCalls != 1 {
		t.Fatalf("expected one platform callback, got %d", callbackCalls)
	}
	if fallback.calls != 0 {
		t.Fatalf("fallback should not be used for platform employees when callback succeeds, got %d calls", fallback.calls)
	}
}

func TestPlatformAwareMachineSenderFallsBackWhenCallbackFails(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer callback.Close()

	provider := platformProviderEntry{PlatformID: "platform-1", CallbackBaseURL: callback.URL, RegistrationStatus: "active"}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "ve_employee_1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", Status: veStatusActive}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}

	fallback := &fakePlatformMachineSender{err: nil}
	sender := platformAwareMachineSender{fallback: fallback, system: settings, tenants: fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}}
	if err := sender.SendToMachine("ve_employee_1", map[string]any{"type": "discussion.message"}); err != nil {
		t.Fatalf("SendToMachine returned error: %v", err)
	}
	if fallback.calls != 1 {
		t.Fatalf("expected fallback after callback failure, got %d calls", fallback.calls)
	}
}

func TestPlatformAwareMachineSenderReturnsCallbackErrorWithoutFallback(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	provider := platformProviderEntry{PlatformID: "platform-1", CallbackBaseURL: "http://127.0.0.1:1", RegistrationStatus: "active"}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "ve_employee_1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", Status: veStatusActive}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}

	sender := platformAwareMachineSender{system: settings, tenants: fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}}
	if err := sender.SendToMachine("ve_employee_1", map[string]any{"type": "discussion.message"}); err == nil {
		t.Fatal("expected callback error without fallback")
	} else if errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUpdatePlatformEmployeeStatusDisablesTenantScopedRegistryEntry(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	provider := platformProviderEntry{PlatformID: "platform-1", RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "ve_employee_1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", Status: veStatusActive, OnlineStatus: "platform"}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}

	tenantID, updated, err := updatePlatformEmployeeStatus(context.Background(), settings, fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}, "platform-1", "platform-employee-1", veStatusDisabled)
	if err != nil {
		t.Fatalf("update status: %v", err)
	}
	if !updated || tenantID != "tenant-a" {
		t.Fatalf("unexpected update result tenant=%q updated=%v", tenantID, updated)
	}
	updatedRegistry := loadVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings))
	if len(updatedRegistry.Employees) != 1 {
		t.Fatalf("unexpected registry: %#v", updatedRegistry)
	}
	got := updatedRegistry.Employees[0]
	if got.Status != veStatusDisabled || got.OnlineStatus != veOnlineStatusOffline || got.DisabledAt == "" {
		t.Fatalf("employee was not disabled correctly: %#v", got)
	}
}

type fakePlatformUserRepo struct {
	items []*store.User
}

func (f fakePlatformUserRepo) Create(ctx context.Context, user *store.User) error {
	_ = ctx
	_ = user
	return nil
}

func (f fakePlatformUserRepo) GetByID(ctx context.Context, id string) (*store.User, error) {
	_ = ctx
	for _, item := range f.items {
		if item != nil && item.ID == id {
			return item, nil
		}
	}
	return nil, nil
}

func (f fakePlatformUserRepo) GetByEmail(ctx context.Context, email string) (*store.User, error) {
	_ = ctx
	for _, item := range f.items {
		if item != nil && strings.EqualFold(item.Email, email) {
			return item, nil
		}
	}
	return nil, nil
}

func (f fakePlatformUserRepo) GetByTenantEmail(ctx context.Context, tenantID, email string) (*store.User, error) {
	_ = ctx
	for _, item := range f.items {
		if item != nil && item.TenantID == tenantID && strings.EqualFold(item.Email, email) {
			return item, nil
		}
	}
	return nil, nil
}

func (f fakePlatformUserRepo) List(ctx context.Context) ([]*store.User, error) {
	_ = ctx
	return f.items, nil
}

func (f fakePlatformUserRepo) ListByTenant(ctx context.Context, tenantID string) ([]*store.User, error) {
	_ = ctx
	out := make([]*store.User, 0, len(f.items))
	for _, item := range f.items {
		if item != nil && item.TenantID == tenantID {
			out = append(out, item)
		}
	}
	return out, nil
}

func (f fakePlatformUserRepo) DeleteByEmail(ctx context.Context, email string) error {
	_ = ctx
	_ = email
	return nil
}

func (f fakePlatformUserRepo) DeleteByTenantEmail(ctx context.Context, tenantID, email string) error {
	_ = ctx
	_ = tenantID
	_ = email
	return nil
}

func (f fakePlatformUserRepo) UpdateSmartRoute(ctx context.Context, userID string, enabled bool) error {
	_ = ctx
	_ = userID
	_ = enabled
	return nil
}

func TestPlatformSourceUsersForTenantExcludesPlatformEmployees(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	now := time.Now().UTC()
	users := fakePlatformUserRepo{items: []*store.User{
		{ID: "real-1", TenantID: "tenant-a", Email: "real@example.com", Status: "active", UpdatedAt: now},
		{ID: "ve-account-1", TenantID: "tenant-a", Email: "worker@tenant.ve.test", Status: "active", UpdatedAt: now},
		{ID: "real-other", TenantID: "tenant-b", Email: "other@example.com", Status: "active", UpdatedAt: now},
	}}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "ve_employee_1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", OwnerUserID: "ve-account-1", OwnerEmail: "worker@tenant.ve.test", Status: veStatusActive}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}

	items, err := platformSourceUsersForTenant(context.Background(), settings, users, "tenant-a")
	if err != nil {
		t.Fatalf("source users: %v", err)
	}
	if len(items) != 1 || items[0]["id"] != "real-1" {
		t.Fatalf("expected only real user, got %#v", items)
	}
}

func TestPlatformEmployeeRegisterRequiresUserRepoAndEmployeeID(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active"}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	tenants := fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}

	missingRepoReq := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/employees", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-a", "employee_id": "platform-employee-1", "virtual_email": "worker@tenant.test", "name": "Worker"})
	missingRepoRec := httptest.NewRecorder()
	PlatformEmployeeRegisterHandler(settings, tenants, nil).ServeHTTP(missingRepoRec, missingRepoReq)
	if missingRepoRec.Code != http.StatusServiceUnavailable || !bytes.Contains(missingRepoRec.Body.Bytes(), []byte("USER_REPOSITORY_UNAVAILABLE")) {
		t.Fatalf("missing repo status=%d body=%s", missingRepoRec.Code, missingRepoRec.Body.String())
	}

	missingEmployeeReq := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/employees", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-a", "virtual_email": "worker@tenant.test", "name": "Worker"})
	missingEmployeeRec := httptest.NewRecorder()
	PlatformEmployeeRegisterHandler(settings, tenants, fakePlatformUserRepo{}).ServeHTTP(missingEmployeeRec, missingEmployeeReq)
	if missingEmployeeRec.Code != http.StatusBadRequest || !bytes.Contains(missingEmployeeRec.Body.Bytes(), []byte("EMPLOYEE_ID_REQUIRED")) {
		t.Fatalf("missing employee id status=%d body=%s", missingEmployeeRec.Code, missingEmployeeRec.Body.String())
	}

	platformOnlyReq := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/employees", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-a", "platform_employee_id": "platform-employee-2", "virtual_email": "worker2@tenant.test", "name": "Worker 2"})
	platformOnlyRec := httptest.NewRecorder()
	PlatformEmployeeRegisterHandler(settings, tenants, fakePlatformUserRepo{}).ServeHTTP(platformOnlyRec, platformOnlyReq)
	if platformOnlyRec.Code != http.StatusOK {
		t.Fatalf("platform employee id register status=%d body=%s", platformOnlyRec.Code, platformOnlyRec.Body.String())
	}
	registry := loadVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings))
	if len(registry.Employees) != 1 || registry.Employees[0].PlatformEmployeeID != "platform-employee-2" || registry.Employees[0].MachineID != "ve_platform-employee-2" {
		t.Fatalf("unexpected registered employee: %#v", registry.Employees)
	}
}
func TestPlatformEmployeeExistsInTenantMatchesPlatformEmployeeID(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "ve_employee_1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", Status: veStatusActive}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}
	if !platformEmployeeExistsInTenant(context.Background(), settings, "tenant-a", "platform-1", "platform-employee-1") {
		t.Fatal("expected platform employee to be found in tenant registry")
	}
	if platformEmployeeExistsInTenant(context.Background(), settings, "tenant-b", "platform-1", "platform-employee-1") {
		t.Fatal("employee should not be visible across tenants")
	}
	if platformEmployeeExistsInTenant(context.Background(), settings, "tenant-a", "other-platform", "platform-employee-1") {
		t.Fatal("employee should not match a different platform")
	}
}

func TestPlatformKnowledgeImportValidatesHubTenantAndEmployee(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active"}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "ve_employee_1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", Status: veStatusActive}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}

	handler := PlatformKnowledgeImportHandler(settings, fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}, {ID: "tenant-b", Slug: "tenant-b", Name: "Tenant B", Status: "active"}}})

	goodReq := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/knowledge/imports", "platform-1", privateKey, map[string]any{"import_id": "kimp-1", "hub_tenant_id": "tenant-a", "platform_employee_id": "platform-employee-1", "title": "Case Pack"})
	goodRec := httptest.NewRecorder()
	handler.ServeHTTP(goodRec, goodReq)
	if goodRec.Code != http.StatusAccepted {
		t.Fatalf("expected accepted knowledge import, got status %d body %s", goodRec.Code, goodRec.Body.String())
	}

	crossTenantReq := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/knowledge/imports", "platform-1", privateKey, map[string]any{"import_id": "kimp-2", "hub_tenant_id": "tenant-b", "platform_employee_id": "platform-employee-1", "title": "Case Pack"})
	crossTenantRec := httptest.NewRecorder()
	handler.ServeHTTP(crossTenantRec, crossTenantReq)
	if crossTenantRec.Code != http.StatusNotFound {
		t.Fatalf("expected cross-tenant employee lookup to be rejected, got status %d body %s", crossTenantRec.Code, crossTenantRec.Body.String())
	}
}

func TestPlatformEmployeeStatusAcceptsPlatformEmployeeID(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "ve_employee_1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", Status: veStatusActive, OnlineStatus: "platform"}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}

	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/employees/ve_employee_1/status", "platform-1", privateKey, map[string]any{"platform_employee_id": "platform-employee-1", "service_status": "disabled"})
	rec := httptest.NewRecorder()
	PlatformEmployeeStatusHandler(settings, fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status update status=%d body=%s", rec.Code, rec.Body.String())
	}
	updated := loadVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings))
	if len(updated.Employees) != 1 || updated.Employees[0].Status != veStatusDisabled {
		t.Fatalf("employee status was not updated: %#v", updated.Employees)
	}
}
func TestPlatformTenantDomainsReturnsSaveFailure(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	rawRegistry, err := json.Marshal(platformProviderRegistry{Providers: []platformProviderEntry{provider}})
	if err != nil {
		t.Fatalf("marshal provider registry: %v", err)
	}
	settings := failingPlatformSettingsRepo{raw: string(rawRegistry)}
	tenants := fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}

	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/providers/tenant-domains", "platform-1", privateKey, map[string]any{"tenant_domains": []map[string]any{{"hub_tenant_id": "tenant-a"}}})
	rec := httptest.NewRecorder()
	PlatformTenantDomainsHandler(settings, tenants).ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError || !bytes.Contains(rec.Body.Bytes(), []byte("TENANT_DOMAINS_SAVE_FAILED")) {
		t.Fatalf("tenant domain save failure status=%d body=%s", rec.Code, rec.Body.String())
	}
}
func TestPlatformTenantEndpointsRequireActiveTenants(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-inactive"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	deletedAt := time.Now().UTC()
	tenants := fakePlatformTenantRepo{items: []*store.Tenant{
		{ID: "tenant-active", Slug: "active", Name: "Active", Status: "active"},
		{ID: "tenant-inactive", Slug: "inactive", Name: "Inactive", Status: "inactive"},
		{ID: "tenant-deleted", Slug: "deleted", Name: "Deleted", Status: "active", DeletedAt: &deletedAt},
	}}

	listReq := newSignedPlatformJSONRequest(t, http.MethodGet, "/api/platform/tenants", "platform-1", privateKey, map[string]any{})
	listRec := httptest.NewRecorder()
	PlatformTenantsListHandler(settings, tenants).ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("tenant list status=%d body=%s", listRec.Code, listRec.Body.String())
	}
	if !bytes.Contains(listRec.Body.Bytes(), []byte("tenant-active")) || bytes.Contains(listRec.Body.Bytes(), []byte("tenant-inactive")) || bytes.Contains(listRec.Body.Bytes(), []byte("tenant-deleted")) {
		t.Fatalf("tenant list should include only active tenants: %s", listRec.Body.String())
	}

	tenantDomainsReq := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/providers/tenant-domains", "platform-1", privateKey, map[string]any{"tenant_domains": []map[string]any{{"hub_tenant_id": "tenant-active", "tenant_id": "source-active"}, {"hub_tenant_id": "tenant-inactive", "tenant_id": "source-inactive"}}})
	tenantDomainsRec := httptest.NewRecorder()
	PlatformTenantDomainsHandler(settings, tenants).ServeHTTP(tenantDomainsRec, tenantDomainsReq)
	if tenantDomainsRec.Code != http.StatusOK || !bytes.Contains(tenantDomainsRec.Body.Bytes(), []byte(`"tenant_domain_count":1`)) {
		t.Fatalf("tenant domain update status=%d body=%s", tenantDomainsRec.Code, tenantDomainsRec.Body.String())
	}
	updatedProviders := loadPlatformProviderRegistry(context.Background(), settings)
	if len(updatedProviders.Providers) != 1 || len(updatedProviders.Providers[0].TenantDomains) != 1 || updatedProviders.Providers[0].TenantDomains[0].HubTenantID != "tenant-active" {
		t.Fatalf("tenant domain update should retain only active tenants: %#v", updatedProviders)
	}

	invalidTenantDomainsReq := newSignedPlatformRawRequest(t, http.MethodPost, "/api/platform/providers/tenant-domains", "platform-1", privateKey, []byte(`{"tenant_domains"`))
	invalidTenantDomainsRec := httptest.NewRecorder()
	PlatformTenantDomainsHandler(settings, tenants).ServeHTTP(invalidTenantDomainsRec, invalidTenantDomainsReq)
	if invalidTenantDomainsRec.Code != http.StatusBadRequest {
		t.Fatalf("invalid tenant domain json status=%d body=%s", invalidTenantDomainsRec.Code, invalidTenantDomainsRec.Body.String())
	}

	migrationReq := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/migrations", "platform-1", privateKey, map[string]any{"migration_id": "mig-1", "hub_tenant_id": "tenant-inactive"})
	migrationRec := httptest.NewRecorder()
	PlatformMigrationSubmitHandler(settings, tenants).ServeHTTP(migrationRec, migrationReq)
	if migrationRec.Code != http.StatusNotFound {
		t.Fatalf("inactive tenant migration status=%d body=%s", migrationRec.Code, migrationRec.Body.String())
	}

	sourceUsersReq := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/source-users/sync", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-inactive"})
	sourceUsersRec := httptest.NewRecorder()
	PlatformSourceUsersSyncHandler(settings, fakePlatformUserRepo{}, tenants).ServeHTTP(sourceUsersRec, sourceUsersReq)
	if sourceUsersRec.Code != http.StatusNotFound {
		t.Fatalf("inactive tenant source users status=%d body=%s", sourceUsersRec.Code, sourceUsersRec.Body.String())
	}

	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "ve_employee_1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", Status: veStatusActive}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-inactive", settings), registry); err != nil {
		t.Fatalf("save inactive tenant ve registry: %v", err)
	}
	knowledgeReq := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/knowledge/imports", "platform-1", privateKey, map[string]any{"import_id": "kimp-inactive", "hub_tenant_id": "tenant-inactive", "platform_employee_id": "platform-employee-1"})
	knowledgeRec := httptest.NewRecorder()
	PlatformKnowledgeImportHandler(settings, tenants).ServeHTTP(knowledgeRec, knowledgeReq)
	if knowledgeRec.Code != http.StatusNotFound {
		t.Fatalf("inactive tenant knowledge import status=%d body=%s", knowledgeRec.Code, knowledgeRec.Body.String())
	}

	updatedTenant, updated, err := updatePlatformEmployeeStatus(context.Background(), settings, tenants, "platform-1", "platform-employee-1", veStatusDisabled)
	if err != nil {
		t.Fatalf("update inactive tenant employee status: %v", err)
	}
	if updated || updatedTenant != "" {
		t.Fatalf("inactive tenant employee should not be updated, tenant=%q updated=%v", updatedTenant, updated)
	}
}
func newSignedPlatformJSONRequest(t *testing.T, method, target, platformID string, privateKey *rsa.PrivateKey, payload any) *http.Request {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return newSignedPlatformRawRequest(t, method, target, platformID, privateKey, body)
}

func newSignedPlatformRawRequest(t *testing.T, method, target, platformID string, privateKey *rsa.PrivateKey, body []byte) *http.Request {
	t.Helper()
	digest := sha256.Sum256(body)
	sig, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("sign payload: %v", err)
	}
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-VE-Platform-ID", platformID)
	req.Header.Set("X-VE-Signature", base64.StdEncoding.EncodeToString(sig))
	return req
}

func testPlatformPublicKeyPEM(t *testing.T, privateKey *rsa.PrivateKey) string {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}
