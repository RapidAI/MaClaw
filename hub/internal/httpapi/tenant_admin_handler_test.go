package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type tenantAdminHandlerTestRepo struct {
	items map[string]*store.Tenant
}

func (r *tenantAdminHandlerTestRepo) Create(_ context.Context, tenant *store.Tenant) error {
	r.items[tenant.ID] = tenant
	return nil
}

func (r *tenantAdminHandlerTestRepo) GetByID(_ context.Context, id string) (*store.Tenant, error) {
	item := r.items[id]
	if item == nil {
		return nil, nil
	}
	copy := *item
	return &copy, nil
}

func (r *tenantAdminHandlerTestRepo) GetBySlug(_ context.Context, slug string) (*store.Tenant, error) {
	for _, item := range r.items {
		if item.Slug == slug {
			copy := *item
			return &copy, nil
		}
	}
	return nil, nil
}

func (r *tenantAdminHandlerTestRepo) List(context.Context) ([]*store.Tenant, error) {
	items := make([]*store.Tenant, 0, len(r.items))
	for _, item := range r.items {
		copy := *item
		items = append(items, &copy)
	}
	return items, nil
}

func (r *tenantAdminHandlerTestRepo) EnsureDefault(context.Context) (*store.Tenant, error) {
	return r.GetByID(context.Background(), store.DefaultTenantID)
}

func (r *tenantAdminHandlerTestRepo) DeleteByID(_ context.Context, id string) error {
	delete(r.items, id)
	return nil
}

func (r *tenantAdminHandlerTestRepo) UpdateStatus(_ context.Context, id string, status string) error {
	if item := r.items[id]; item != nil {
		item.Status = status
		item.UpdatedAt = time.Now().UTC()
	}
	return nil
}

func (r *tenantAdminHandlerTestRepo) UpdateDomains(_ context.Context, id string, primaryDomain string, settingsJSON string) error {
	if item := r.items[id]; item != nil {
		item.PrimaryDomain = primaryDomain
		item.SettingsJSON = settingsJSON
		item.UpdatedAt = time.Now().UTC()
	}
	return nil
}

func (r *tenantAdminHandlerTestRepo) SoftDeleteByID(_ context.Context, id string) error {
	if item := r.items[id]; item != nil {
		now := time.Now().UTC()
		item.Status = "deleted"
		item.DeletedAt = &now
		item.UpdatedAt = now
	}
	return nil
}

type tenantAdminHandlerTestStopper struct {
	tenantID string
	calls    int
}

func (s *tenantAdminHandlerTestStopper) StopTenantIMs(_ context.Context, tenantID string) {
	s.tenantID = tenantID
	s.calls++
}

func tenantAdminHandlerGlobalReq(method, target string, body any) *http.Request {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, target, &buf)
	req.SetPathValue("tenantId", "tenant_a")
	req = req.WithContext(context.WithValue(req.Context(), adminUserContextKey, &store.AdminUser{Scope: "global", ID: "admin"}))
	return req
}

func tenantAdminHandlerReqWithScope(method, target string, body any, admin *store.AdminUser, tenantID string) *http.Request {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, target, &buf)
	req.SetPathValue("tenantId", tenantID)
	req = req.WithContext(context.WithValue(req.Context(), adminUserContextKey, admin))
	return req
}

func TestTenantStatusUpdateStopsRuntimeWhenDeactivated(t *testing.T) {
	repo := &tenantAdminHandlerTestRepo{items: map[string]*store.Tenant{"tenant_a": {ID: "tenant_a", Slug: "a", Name: "Tenant A", Status: "active", CreatedAt: time.Now(), UpdatedAt: time.Now()}}}
	stopper := &tenantAdminHandlerTestStopper{}
	rec := httptest.NewRecorder()
	AdminTenantStatusUpdateHandler(repo, stopper)(rec, tenantAdminHandlerGlobalReq(http.MethodPatch, "/api/admin/tenants/tenant_a/status", map[string]any{"status": "inactive"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if stopper.calls != 1 || stopper.tenantID != "tenant_a" {
		t.Fatalf("stopper calls=%d tenant=%q", stopper.calls, stopper.tenantID)
	}
}

func TestTenantDeleteStopsRuntime(t *testing.T) {
	repo := &tenantAdminHandlerTestRepo{items: map[string]*store.Tenant{"tenant_a": {ID: "tenant_a", Slug: "a", Name: "Tenant A", Status: "active", CreatedAt: time.Now(), UpdatedAt: time.Now()}}}
	stopper := &tenantAdminHandlerTestStopper{}
	rec := httptest.NewRecorder()
	AdminTenantDeleteHandler(repo, stopper)(rec, tenantAdminHandlerGlobalReq(http.MethodDelete, "/api/admin/tenants/tenant_a", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if stopper.calls != 1 || stopper.tenantID != "tenant_a" {
		t.Fatalf("stopper calls=%d tenant=%q", stopper.calls, stopper.tenantID)
	}
	if repo.items["tenant_a"].DeletedAt == nil {
		t.Fatal("tenant was not soft deleted")
	}
}

func TestTenantStatusActivateDoesNotStopRuntime(t *testing.T) {
	repo := &tenantAdminHandlerTestRepo{items: map[string]*store.Tenant{"tenant_a": {ID: "tenant_a", Slug: "a", Name: "Tenant A", Status: "inactive", CreatedAt: time.Now(), UpdatedAt: time.Now()}}}
	stopper := &tenantAdminHandlerTestStopper{}
	rec := httptest.NewRecorder()
	AdminTenantStatusUpdateHandler(repo, stopper)(rec, tenantAdminHandlerGlobalReq(http.MethodPatch, "/api/admin/tenants/tenant_a/status", map[string]any{"status": "active"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if stopper.calls != 0 {
		t.Fatalf("activate should not stop runtimes, calls=%d", stopper.calls)
	}
}

func TestTenantLifecycleRejectsTenantAdmin(t *testing.T) {
	repo := &tenantAdminHandlerTestRepo{items: map[string]*store.Tenant{"tenant_a": {ID: "tenant_a", Slug: "a", Name: "Tenant A", Status: "active", CreatedAt: time.Now(), UpdatedAt: time.Now()}}}
	admin := &store.AdminUser{Scope: "tenant", TenantID: "tenant_a", ID: "tenant-admin"}

	statusRec := httptest.NewRecorder()
	AdminTenantStatusUpdateHandler(repo)(statusRec, tenantAdminHandlerReqWithScope(http.MethodPatch, "/api/admin/tenants/tenant_a/status", map[string]any{"status": "inactive"}, admin, "tenant_a"))
	if statusRec.Code != http.StatusForbidden {
		t.Fatalf("tenant admin status update code=%d body=%s", statusRec.Code, statusRec.Body.String())
	}

	deleteRec := httptest.NewRecorder()
	AdminTenantDeleteHandler(repo)(deleteRec, tenantAdminHandlerReqWithScope(http.MethodDelete, "/api/admin/tenants/tenant_a", nil, admin, "tenant_a"))
	if deleteRec.Code != http.StatusForbidden {
		t.Fatalf("tenant admin delete code=%d body=%s", deleteRec.Code, deleteRec.Body.String())
	}
}

func TestTenantLifecycleRejectsDefaultTenant(t *testing.T) {
	repo := &tenantAdminHandlerTestRepo{items: map[string]*store.Tenant{store.DefaultTenantID: {ID: store.DefaultTenantID, Slug: "default", Name: "Default", Status: "active", CreatedAt: time.Now(), UpdatedAt: time.Now()}}}
	admin := &store.AdminUser{Scope: "global", ID: "admin"}

	detailRec := httptest.NewRecorder()
	AdminTenantDetailHandler(repo)(detailRec, tenantAdminHandlerReqWithScope(http.MethodGet, "/api/admin/tenants/"+store.DefaultTenantID, nil, admin, store.DefaultTenantID))
	if detailRec.Code != http.StatusBadRequest {
		t.Fatalf("default tenant detail code=%d body=%s", detailRec.Code, detailRec.Body.String())
	}

	statusRec := httptest.NewRecorder()
	AdminTenantStatusUpdateHandler(repo)(statusRec, tenantAdminHandlerReqWithScope(http.MethodPatch, "/api/admin/tenants/"+store.DefaultTenantID+"/status", map[string]any{"status": "inactive"}, admin, store.DefaultTenantID))
	if statusRec.Code != http.StatusBadRequest {
		t.Fatalf("default tenant status code=%d body=%s", statusRec.Code, statusRec.Body.String())
	}

	deleteRec := httptest.NewRecorder()
	AdminTenantDeleteHandler(repo)(deleteRec, tenantAdminHandlerReqWithScope(http.MethodDelete, "/api/admin/tenants/"+store.DefaultTenantID, nil, admin, store.DefaultTenantID))
	if deleteRec.Code != http.StatusBadRequest {
		t.Fatalf("default tenant delete code=%d body=%s", deleteRec.Code, deleteRec.Body.String())
	}
}

func TestTenantAdminCreateRejectsInvalidEmailAsBadRequest(t *testing.T) {
	repo := &tenantAdminHandlerTestRepo{items: map[string]*store.Tenant{"tenant_a": {ID: "tenant_a", Slug: "a", Name: "Tenant A", Status: "active", CreatedAt: time.Now(), UpdatedAt: time.Now()}}}
	ctx := newAdminRouterTestContext(t)
	admin := &store.AdminUser{Scope: "global", ID: "admin"}

	rec := httptest.NewRecorder()
	AdminTenantAdminCreateHandler(repo, ctx.admins, nil)(rec, tenantAdminHandlerReqWithScope(http.MethodPost, "/api/admin/tenants/tenant_a/admins", map[string]any{
		"username": "tenant-admin",
		"password": "StrongPassword123!",
		"email":    "not-an-email",
	}, admin, "tenant_a"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid email create code=%d body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"code":"INVALID_TENANT_ADMIN"`)) {
		t.Fatalf("expected invalid tenant admin code, body=%s", rec.Body.String())
	}
}

func TestTenantCreateAllowsLooseTenantWithoutDomainOrInitialAdmin(t *testing.T) {
	repo := &tenantAdminHandlerTestRepo{items: map[string]*store.Tenant{}}
	rec := httptest.NewRecorder()

	AdminTenantCreateHandler(repo, nil, nil)(rec, tenantAdminHandlerGlobalReq(http.MethodPost, "/api/admin/tenants", map[string]any{
		"name": "Loose Users",
	}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("loose tenant create code=%d body=%s", rec.Code, rec.Body.String())
	}
	tenant := repo.items["tenant_loose-users"]
	if tenant == nil {
		t.Fatalf("tenant was not created: %#v", repo.items)
	}
	if tenant.Slug != "loose-users" || tenant.Name != "Loose Users" || tenant.PrimaryDomain != "" {
		t.Fatalf("unexpected tenant: %#v", tenant)
	}
	if bytes.Contains(rec.Body.Bytes(), []byte(`"admin"`)) {
		t.Fatalf("response should not include admin when initial admin is omitted: %s", rec.Body.String())
	}
}

func TestTenantCreateGeneratesInternalCodeForNonASCIITenantName(t *testing.T) {
	repo := &tenantAdminHandlerTestRepo{items: map[string]*store.Tenant{}}
	rec := httptest.NewRecorder()

	AdminTenantCreateHandler(repo, nil, nil)(rec, tenantAdminHandlerGlobalReq(http.MethodPost, "/api/admin/tenants", map[string]any{
		"name": "\u96f6\u6563\u7528\u6237",
	}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("non-ascii tenant create code=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(repo.items) != 1 {
		t.Fatalf("expected one tenant, got %d", len(repo.items))
	}
	for id, tenant := range repo.items {
		if id == "" || tenant.Slug == "" || tenant.Name != "\u96f6\u6563\u7528\u6237" {
			t.Fatalf("unexpected tenant id=%q tenant=%#v", id, tenant)
		}
	}
}

func TestTenantCreateRejectsPartialInitialAdmin(t *testing.T) {
	repo := &tenantAdminHandlerTestRepo{items: map[string]*store.Tenant{}}
	rec := httptest.NewRecorder()

	AdminTenantCreateHandler(repo, nil, nil)(rec, tenantAdminHandlerGlobalReq(http.MethodPost, "/api/admin/tenants", map[string]any{
		"name":                   "Partial Admin Corp",
		"initial_admin_username": "partial-owner",
	}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("partial initial admin create code=%d body=%s", rec.Code, rec.Body.String())
	}
	if _, ok := repo.items["tenant_partial-admin-corp"]; ok {
		t.Fatal("tenant should not be created when initial admin fields are partial")
	}
}

func TestTenantCreateAcceptsMultipleEmailDomains(t *testing.T) {
	repo := &tenantAdminHandlerTestRepo{items: map[string]*store.Tenant{}}
	rec := httptest.NewRecorder()

	AdminTenantCreateHandler(repo, nil, nil)(rec, tenantAdminHandlerGlobalReq(http.MethodPost, "/api/admin/tenants", map[string]any{
		"name":           "Multi Domain Corp",
		"primary_domain": "Acme.com",
		"domains":        []string{"acme.com", "subsidiary.example"},
	}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("multi domain tenant create code=%d body=%s", rec.Code, rec.Body.String())
	}
	tenant := repo.items["tenant_multi-domain-corp"]
	if tenant == nil || tenant.PrimaryDomain != "acme.com" || !strings.Contains(tenant.SettingsJSON, "subsidiary.example") {
		t.Fatalf("unexpected tenant domains: %#v", tenant)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"domains":["acme.com","subsidiary.example"]`)) {
		t.Fatalf("response missing domains: %s", rec.Body.String())
	}
}

func TestTenantDomainsUpdateAllowsTenantAdminForOwnTenant(t *testing.T) {
	repo := &tenantAdminHandlerTestRepo{items: map[string]*store.Tenant{"tenant_a": {ID: "tenant_a", Slug: "tenant-a", Name: "Tenant A", Status: "active", CreatedAt: time.Now(), UpdatedAt: time.Now()}}}
	admin := &store.AdminUser{Scope: "tenant", TenantID: "tenant_a", ID: "tenant-admin"}
	rec := httptest.NewRecorder()

	AdminTenantDomainsUpdateHandler(repo, nil)(rec, tenantAdminHandlerReqWithScope(http.MethodPatch, "/api/admin/tenants/tenant_a/domains", map[string]any{
		"domains": []string{"Tenant-A.EXAMPLE.com", "team.example.com", "tenant-a.example.com"},
	}, admin, "tenant_a"))
	if rec.Code != http.StatusOK {
		t.Fatalf("tenant domains update code=%d body=%s", rec.Code, rec.Body.String())
	}
	tenant := repo.items["tenant_a"]
	if tenant.PrimaryDomain != "tenant-a.example.com" || !strings.Contains(tenant.SettingsJSON, "team.example.com") {
		t.Fatalf("unexpected updated domains: %#v", tenant)
	}
}

func TestTenantDomainsUpdateRejectsInvalidDomain(t *testing.T) {
	repo := &tenantAdminHandlerTestRepo{items: map[string]*store.Tenant{"tenant_a": {ID: "tenant_a", Slug: "tenant-a", Name: "Tenant A", Status: "active", CreatedAt: time.Now(), UpdatedAt: time.Now()}}}
	admin := &store.AdminUser{Scope: "tenant", TenantID: "tenant_a", ID: "tenant-admin"}
	rec := httptest.NewRecorder()

	AdminTenantDomainsUpdateHandler(repo, nil)(rec, tenantAdminHandlerReqWithScope(http.MethodPatch, "/api/admin/tenants/tenant_a/domains", map[string]any{
		"domains": []string{"https://tenant-a.example.com"},
	}, admin, "tenant_a"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid tenant domains update code=%d body=%s", rec.Code, rec.Body.String())
	}
	if repo.items["tenant_a"].PrimaryDomain != "" {
		t.Fatalf("invalid domain should not update tenant: %#v", repo.items["tenant_a"])
	}
}

func TestTenantDomainsUpdateRejectsDomainConflict(t *testing.T) {
	repo := &tenantAdminHandlerTestRepo{items: map[string]*store.Tenant{
		"tenant_a": {ID: "tenant_a", Slug: "tenant-a", Name: "Tenant A", Status: "active", PrimaryDomain: "a.example.com", CreatedAt: time.Now(), UpdatedAt: time.Now()},
		"tenant_b": {ID: "tenant_b", Slug: "tenant-b", Name: "Tenant B", Status: "active", PrimaryDomain: "b.example.com", CreatedAt: time.Now(), UpdatedAt: time.Now()},
	}}
	admin := &store.AdminUser{Scope: "tenant", TenantID: "tenant_a", ID: "tenant-admin"}
	rec := httptest.NewRecorder()

	AdminTenantDomainsUpdateHandler(repo, nil)(rec, tenantAdminHandlerReqWithScope(http.MethodPatch, "/api/admin/tenants/tenant_a/domains", map[string]any{
		"domains": []string{"b.example.com"},
	}, admin, "tenant_a"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("conflicting tenant domains update code=%d body=%s", rec.Code, rec.Body.String())
	}
	if repo.items["tenant_a"].PrimaryDomain != "a.example.com" {
		t.Fatalf("conflict should not update tenant: %#v", repo.items["tenant_a"])
	}
}

func TestTenantCreateRejectsDomainConflict(t *testing.T) {
	repo := &tenantAdminHandlerTestRepo{items: map[string]*store.Tenant{"tenant_a": {ID: "tenant_a", Slug: "tenant-a", Name: "Tenant A", Status: "active", PrimaryDomain: "a.example.com", CreatedAt: time.Now(), UpdatedAt: time.Now()}}}
	rec := httptest.NewRecorder()

	AdminTenantCreateHandler(repo, nil, nil)(rec, tenantAdminHandlerGlobalReq(http.MethodPost, "/api/admin/tenants", map[string]any{
		"name":    "Tenant B",
		"domains": []string{"a.example.com"},
	}))
	if rec.Code != http.StatusConflict {
		t.Fatalf("conflicting tenant create code=%d body=%s", rec.Code, rec.Body.String())
	}
	if _, ok := repo.items["tenant_tenant-b"]; ok {
		t.Fatal("conflicting tenant should not be created")
	}
}

func TestTenantCreateRejectsDomainConflictFromLegacySettingsDomains(t *testing.T) {
	repo := &tenantAdminHandlerTestRepo{items: map[string]*store.Tenant{"tenant_a": {ID: "tenant_a", Slug: "tenant-a", Name: "Tenant A", Status: "active", SettingsJSON: `{"email_domains":[],"domains":["legacy.example.com"]}`, CreatedAt: time.Now(), UpdatedAt: time.Now()}}}
	rec := httptest.NewRecorder()

	AdminTenantCreateHandler(repo, nil, nil)(rec, tenantAdminHandlerGlobalReq(http.MethodPost, "/api/admin/tenants", map[string]any{
		"name":    "Tenant B",
		"domains": []string{"legacy.example.com"},
	}))
	if rec.Code != http.StatusConflict {
		t.Fatalf("legacy conflicting tenant create code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestTenantDomainsUpdatePostsPlatformCallback(t *testing.T) {
	repo := &tenantAdminHandlerTestRepo{items: map[string]*store.Tenant{"tenant_a": {ID: "tenant_a", Slug: "tenant-a", Name: "Tenant A", Status: "active", CreatedAt: time.Now(), UpdatedAt: time.Now()}}}
	settings := &testSystemSettingsRepo{}
	seen := make(chan map[string]any, 1)
	callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode callback body: %v", err)
		}
		seen <- body
		w.WriteHeader(http.StatusAccepted)
	}))
	defer callback.Close()
	provider := platformProviderEntry{PlatformID: "platform-1", CallbackBaseURL: callback.URL, CallbackSecret: "secret-1", RegistrationStatus: "active"}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	rec := httptest.NewRecorder()
	AdminTenantDomainsUpdateWithPlatformCallbackHandler(settings, repo, nil)(rec, tenantAdminHandlerGlobalReq(http.MethodPatch, "/api/admin/tenants/tenant_a/domains", map[string]any{"domains": []string{"tenant-a.example.com", "team.example.com"}}))
	if rec.Code != http.StatusOK {
		t.Fatalf("tenant domains update code=%d body=%s", rec.Code, rec.Body.String())
	}
	select {
	case body := <-seen:
		rawDomains, _ := body["domains"].([]any)
		if body["hub_tenant_id"] != "tenant_a" || body["primary_domain"] != "tenant-a.example.com" || len(rawDomains) != 2 || rawDomains[1] != "team.example.com" {
			t.Fatalf("unexpected callback body: %#v", body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("tenant domain callback was not posted")
	}
}

func TestTenantCreateRollsBackWhenInitialAdminEmailInvalid(t *testing.T) {
	repo := &tenantAdminHandlerTestRepo{items: map[string]*store.Tenant{}}
	ctx := newAdminRouterTestContext(t)
	rec := httptest.NewRecorder()

	AdminTenantCreateHandler(repo, ctx.admins, nil)(rec, tenantAdminHandlerGlobalReq(http.MethodPost, "/api/admin/tenants", map[string]any{
		"slug":                   "badmail",
		"name":                   "Bad Mail Corp",
		"initial_admin_username": "badmail-owner",
		"initial_admin_password": "StrongPassword123!",
		"initial_admin_email":    "not-an-email",
	}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid initial admin email create code=%d body=%s", rec.Code, rec.Body.String())
	}
	if _, ok := repo.items["tenant_badmail"]; ok {
		t.Fatal("tenant should be removed when initial admin email is invalid")
	}
}
