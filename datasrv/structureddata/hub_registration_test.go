package structureddata

import (
	"bytes"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHubRegistrationRegisterAndSyncTenants(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	initResult, err := svc.InitializeAdmin(t.Context(), InitializeAdminInput{Username: "admin", Password: "change-me-123"})
	if err != nil {
		t.Fatalf("InitializeAdmin: %v", err)
	}
	global := Principal{TenantID: initResult.TenantID, UserID: "admin", Role: "data_admin", AdminScope: "global"}
	tenantAdmin := Principal{TenantID: "tenant-a", UserID: "tenant-admin", Role: "data_admin", AdminScope: "tenant"}

	var publicKeyPEM string
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/platform/providers/register":
			var body map[string]any
			readSignedJSON(t, r, &body)
			publicKeyPEM, _ = body["public_key"].(string)
			if strings.TrimSpace(publicKeyPEM) == "" {
				t.Fatalf("register body missing public_key: %#v", body)
			}
			verifyHubRequestSignature(t, r, publicKeyPEM)
			if body["platform_id"] != "datasrv-test" || body["platform_name"] != "DataSrv Test" {
				t.Fatalf("unexpected register body: %#v", body)
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "registration_status": "active", "platform_id": body["platform_id"]})
		case "/api/platform/tenants/list":
			if strings.TrimSpace(publicKeyPEM) == "" {
				t.Fatal("tenant sync before registration")
			}
			verifyHubRequestSignature(t, r, publicKeyPEM)
			writeJSON(w, http.StatusOK, map[string]any{"tenants": []map[string]any{{"hub_tenant_id": "tenant-a", "id": "tenant-a", "slug": "acme", "name": "Acme", "status": "active", "primary_domain": "acme.example", "domains": []string{"acme.example", "team.acme.example"}, "virtual_mail_domain": "acme.data.example", "updated_at": time.Now().UTC().Format(time.RFC3339)}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer hub.Close()

	if _, err := svc.SaveHubRegistration(t.Context(), tenantAdmin, SaveHubRegistrationInput{HubBaseURL: hub.URL}); err == nil {
		t.Fatal("tenant admin should not save hub registration")
	}
	if _, err := svc.GetHubRegistrationStatus(t.Context(), tenantAdmin); err == nil {
		t.Fatal("tenant admin should not read hub registration settings")
	}
	saved, err := svc.SaveHubRegistration(t.Context(), global, SaveHubRegistrationInput{HubBaseURL: hub.URL, PlatformID: "datasrv-test", PlatformName: "DataSrv Test", CallbackBaseURL: "http://127.0.0.1:18180", VirtualMailDomain: "DATA.EXAMPLE"})
	if err != nil {
		t.Fatalf("SaveHubRegistration: %v", err)
	}
	if !saved.Status.Configured || saved.Status.Registered || saved.Status.VirtualMailDomain != "data.example" {
		t.Fatalf("unexpected saved status: %#v", saved.Status)
	}
	if _, err := svc.SyncTenantsFromHubPublic(t.Context()); err == nil {
		t.Fatal("public login tenant sync should require active Hub registration")
	}
	registered, err := svc.RegisterHub(t.Context(), global)
	if err != nil {
		t.Fatalf("RegisterHub: %v", err)
	}
	if !registered.Status.Registered || registered.Status.LastRegisteredAt == nil {
		t.Fatalf("unexpected registered status: %#v", registered.Status)
	}
	if _, err := svc.SyncTenantsFromHub(t.Context(), tenantAdmin); err == nil {
		t.Fatal("tenant admin should not pull hub tenants")
	}
	synced, err := svc.SyncTenantsFromHubPublic(t.Context())
	if err != nil {
		t.Fatalf("SyncTenantsFromHubPublic: %v", err)
	}
	if synced.Synced != 1 || synced.Tenants[0].ID != "tenant-a" || len(synced.Tenants[0].Domains) != 2 || synced.Tenants[0].VirtualMailDomain != "acme.data.example" {
		t.Fatalf("unexpected synced tenants: %#v", synced)
	}
	if _, err := svc.SyncTenantsFromHub(t.Context(), global); err != nil {
		t.Fatalf("SyncTenantsFromHub: %v", err)
	}
	status, err := svc.SetupStatus(t.Context())
	if err != nil {
		t.Fatalf("SetupStatus: %v", err)
	}
	if status.HubRegistration == nil || !status.HubRegistration.Registered || len(status.Tenants) < 2 {
		t.Fatalf("setup status missing hub registration or tenants: %#v", status)
	}
	if status.HubRegistration.HubBaseURL != "" || status.HubRegistration.PlatformID != "" || status.HubRegistration.VirtualMailDomain != "" || status.HubRegistration.LastError != "" {
		t.Fatalf("public setup status should not expose hub registration settings: %#v", status.HubRegistration)
	}
}

func TestHubRegistrationSaveInvalidatesActiveRegistrationOnSettingsChange(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	initResult, err := svc.InitializeAdmin(t.Context(), InitializeAdminInput{Username: "admin", Password: "change-me-123"})
	if err != nil {
		t.Fatalf("InitializeAdmin: %v", err)
	}
	global := Principal{TenantID: initResult.TenantID, UserID: "admin", Role: "data_admin", AdminScope: "global"}

	var publicKeyPEM string
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/platform/providers/register":
			var body map[string]any
			readSignedJSON(t, r, &body)
			publicKeyPEM, _ = body["public_key"].(string)
			verifyHubRequestSignature(t, r, publicKeyPEM)
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		case "/api/platform/tenants/list":
			verifyHubRequestSignature(t, r, publicKeyPEM)
			writeJSON(w, http.StatusOK, map[string]any{"tenants": []map[string]any{{"id": "tenant-a", "name": "Tenant A"}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer hub.Close()

	status, err := svc.SetupStatus(t.Context())
	if err != nil {
		t.Fatalf("SetupStatus before registration: %v", err)
	}
	if status.Mode != "local_admin" {
		t.Fatalf("unexpected setup mode before Hub config: %#v", status)
	}
	if _, err := svc.SaveHubRegistration(t.Context(), global, SaveHubRegistrationInput{HubBaseURL: hub.URL, PlatformID: "datasrv-test", PlatformName: "DataSrv Test"}); err != nil {
		t.Fatalf("SaveHubRegistration: %v", err)
	}
	if _, err := svc.RegisterHub(t.Context(), global); err != nil {
		t.Fatalf("RegisterHub: %v", err)
	}
	if _, err := svc.SyncTenantsFromHubPublic(t.Context()); err != nil {
		t.Fatalf("SyncTenantsFromHubPublic before settings change: %v", err)
	}
	status, err = svc.SetupStatus(t.Context())
	if err != nil {
		t.Fatalf("SetupStatus after registration: %v", err)
	}
	if status.Mode != "hub_tenant_admin" || status.HubRegistration == nil || !status.HubRegistration.Registered || status.HubRegistration.LastSyncedAt == nil {
		t.Fatalf("unexpected setup status after Hub registration: %#v", status)
	}
	updated, err := svc.SaveHubRegistration(t.Context(), global, SaveHubRegistrationInput{HubBaseURL: hub.URL, PlatformID: "datasrv-renamed", PlatformName: "DataSrv Test"})
	if err != nil {
		t.Fatalf("SaveHubRegistration rename: %v", err)
	}
	if updated.Status.Registered || updated.Status.LastRegisteredAt != nil || updated.Status.LastSyncedAt != nil || !strings.Contains(updated.Status.LastError, "register again") {
		t.Fatalf("expected settings change to invalidate registration: %#v", updated.Status)
	}
	if _, err := svc.SyncTenantsFromHubPublic(t.Context()); err == nil {
		t.Fatal("public tenant sync should fail after registration settings changed")
	}
}

func TestHubTenantSyncStoresLastErrorOnEmptyTenantResponse(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	initResult, err := svc.InitializeAdmin(t.Context(), InitializeAdminInput{Username: "admin", Password: "change-me-123"})
	if err != nil {
		t.Fatalf("InitializeAdmin: %v", err)
	}
	global := Principal{TenantID: initResult.TenantID, UserID: "admin", Role: "data_admin", AdminScope: "global"}

	var publicKeyPEM string
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/platform/providers/register":
			var body map[string]any
			readSignedJSON(t, r, &body)
			publicKeyPEM, _ = body["public_key"].(string)
			verifyHubRequestSignature(t, r, publicKeyPEM)
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		case "/api/platform/tenants/list":
			verifyHubRequestSignature(t, r, publicKeyPEM)
			writeJSON(w, http.StatusOK, map[string]any{"tenants": []map[string]any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer hub.Close()

	if _, err := svc.SaveHubRegistration(t.Context(), global, SaveHubRegistrationInput{HubBaseURL: hub.URL, PlatformID: "datasrv-empty-tenants"}); err != nil {
		t.Fatalf("SaveHubRegistration: %v", err)
	}
	if _, err := svc.RegisterHub(t.Context(), global); err != nil {
		t.Fatalf("RegisterHub: %v", err)
	}
	if _, err := svc.SyncTenantsFromHubPublic(t.Context()); err == nil || !strings.Contains(err.Error(), "no tenants") {
		t.Fatalf("expected no tenants sync error, got %v", err)
	}
	status, err := svc.GetHubRegistrationStatus(t.Context(), global)
	if err != nil {
		t.Fatalf("GetHubRegistrationStatus: %v", err)
	}
	if !strings.Contains(status.Status.LastError, "no tenants") {
		t.Fatalf("expected last_error to record sync failure: %#v", status.Status)
	}
}

func TestHubTenantSyncDoesNotHoldServiceLockDuringRemoteCall(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	initResult, err := svc.InitializeAdmin(t.Context(), InitializeAdminInput{Username: "admin", Password: "change-me-123"})
	if err != nil {
		t.Fatalf("InitializeAdmin: %v", err)
	}
	global := Principal{TenantID: initResult.TenantID, UserID: "admin", Role: "data_admin", AdminScope: "global"}

	var publicKeyPEM string
	syncStarted := make(chan struct{})
	releaseSync := make(chan struct{})
	var once sync.Once
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/platform/providers/register":
			var body map[string]any
			readSignedJSON(t, r, &body)
			publicKeyPEM, _ = body["public_key"].(string)
			verifyHubRequestSignature(t, r, publicKeyPEM)
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "registration_status": "active", "platform_id": body["platform_id"]})
		case "/api/platform/tenants/list":
			verifyHubRequestSignature(t, r, publicKeyPEM)
			once.Do(func() { close(syncStarted) })
			<-releaseSync
			writeJSON(w, http.StatusOK, map[string]any{"tenants": []map[string]any{{"id": "tenant-a", "name": "Tenant A"}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer hub.Close()

	if _, err := svc.SaveHubRegistration(t.Context(), global, SaveHubRegistrationInput{HubBaseURL: hub.URL, PlatformID: "datasrv-sync-lock", PlatformName: "DataSrv Sync Lock"}); err != nil {
		t.Fatalf("SaveHubRegistration: %v", err)
	}
	if _, err := svc.RegisterHub(t.Context(), global); err != nil {
		t.Fatalf("RegisterHub: %v", err)
	}

	syncErr := make(chan error, 1)
	go func() {
		_, err := svc.SyncTenantsFromHub(t.Context(), global)
		syncErr <- err
	}()
	select {
	case <-syncStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("tenant sync did not reach Hub")
	}
	saved := make(chan error, 1)
	go func() {
		_, err := svc.SaveHubRegistration(t.Context(), global, SaveHubRegistrationInput{HubBaseURL: hub.URL, PlatformID: "datasrv-renamed-during-sync", PlatformName: "DataSrv Sync Lock"})
		saved <- err
	}()
	select {
	case err := <-saved:
		if err != nil {
			t.Fatalf("SaveHubRegistration while sync is in flight: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SaveHubRegistration blocked behind remote tenant sync")
	}
	close(releaseSync)
	select {
	case err := <-syncErr:
		if err == nil || !strings.Contains(err.Error(), "changed during tenant sync") {
			t.Fatalf("expected changed registration error, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("tenant sync did not finish")
	}
	status, err := svc.GetHubRegistrationStatus(t.Context(), global)
	if err != nil {
		t.Fatalf("GetHubRegistrationStatus: %v", err)
	}
	if status.Status.PlatformID != "datasrv-renamed-during-sync" || status.Status.Registered {
		t.Fatalf("in-flight sync should not overwrite changed Hub registration: %#v", status.Status)
	}
}

func TestHubRegistrationRejectsInactiveHubResponse(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	initResult, err := svc.InitializeAdmin(t.Context(), InitializeAdminInput{Username: "admin", Password: "change-me-123"})
	if err != nil {
		t.Fatalf("InitializeAdmin: %v", err)
	}
	global := Principal{TenantID: initResult.TenantID, UserID: "admin", Role: "data_admin", AdminScope: "global"}

	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/platform/providers/register" {
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		readSignedJSON(t, r, &body)
		publicKeyPEM, _ := body["public_key"].(string)
		verifyHubRequestSignature(t, r, publicKeyPEM)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "registration_status": "pending", "platform_id": body["platform_id"]})
	}))
	defer hub.Close()

	if _, err := svc.SaveHubRegistration(t.Context(), global, SaveHubRegistrationInput{HubBaseURL: hub.URL, PlatformID: "datasrv-pending"}); err != nil {
		t.Fatalf("SaveHubRegistration: %v", err)
	}
	if _, err := svc.RegisterHub(t.Context(), global); err == nil || !strings.Contains(err.Error(), "pending") {
		t.Fatalf("expected inactive registration response error, got %v", err)
	}
	status, err := svc.GetHubRegistrationStatus(t.Context(), global)
	if err != nil {
		t.Fatalf("GetHubRegistrationStatus: %v", err)
	}
	if status.Status.Registered || !strings.Contains(status.Status.LastError, "pending") {
		t.Fatalf("inactive registration must not be marked active: %#v", status.Status)
	}
}

func TestHubRegistrationHTTPTenantAdminRestrictions(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	initResult, err := svc.InitializeAdmin(t.Context(), InitializeAdminInput{Username: "admin", Password: "change-me-123"})
	if err != nil {
		t.Fatalf("InitializeAdmin: %v", err)
	}
	global := Principal{TenantID: initResult.TenantID, UserID: "admin", Role: "data_admin", AdminScope: "global"}
	created, err := svc.CreateAdminAccount(t.Context(), global, CreateAdminAccountInput{TenantID: "tenant-a", AdminScope: "tenant", Username: "tenant-admin", Password: "tenant-password-123", Role: "data_admin"})
	if err != nil {
		t.Fatalf("CreateAdminAccount tenant admin: %v", err)
	}
	tenantLogin, err := svc.Login(t.Context(), LoginInput{TenantID: "tenant-a", Username: "tenant-admin", Password: "tenant-password-123"})
	if err != nil {
		t.Fatalf("Login tenant admin: %v", err)
	}
	if _, err := svc.SyncHubTenants(t.Context(), global, SyncHubTenantsInput{Tenants: []DataTenantInfo{{ID: "tenant-a", Name: "Tenant A"}, {ID: "tenant-b", Name: "Tenant B"}}}); err != nil {
		t.Fatalf("SyncHubTenants: %v", err)
	}
	server := NewHTTPServer(svc, "", "test")

	for _, step := range []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, "/api/v1/data/admin/hub-registration", nil},
		{http.MethodPost, "/api/v1/data/admin/tenants/sync", SyncHubTenantsInput{Tenants: []DataTenantInfo{{ID: "tenant-c", Name: "Tenant C"}}}},
		{http.MethodPost, "/api/v1/data/admin/hub-registration", SaveHubRegistrationInput{HubBaseURL: "http://127.0.0.1:18181"}},
		{http.MethodPost, "/api/v1/data/admin/hub-registration/register", map[string]any{}},
		{http.MethodPost, "/api/v1/data/admin/hub-registration/sync-tenants", map[string]any{}},
	} {
		var req *http.Request
		if step.body == nil {
			req = httptest.NewRequest(step.method, step.path, nil)
		} else {
			req = jsonRequest(step.method, step.path, step.body)
		}
		req.Header.Set("Authorization", "Bearer "+tenantLogin.Token)
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("tenant admin %s %s should be forbidden status=%d body=%s", step.method, step.path, w.Code, w.Body.String())
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/data/admin/tenants", nil)
	req.Header.Set("Authorization", "Bearer "+tenantLogin.Token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("tenant admin list tenants status=%d body=%s", w.Code, w.Body.String())
	}
	var listed struct {
		Items []DataTenantInfo `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&listed); err != nil {
		t.Fatalf("decode listed tenants: %v", err)
	}
	if len(listed.Items) != 1 || listed.Items[0].ID != created.Account.TenantID {
		t.Fatalf("tenant admin should only see own synced tenant: %#v", listed)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/data/admin/tenants", nil)
	req.Header.Set("Authorization", "Bearer "+initResult.Token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("global admin list tenants status=%d body=%s", w.Code, w.Body.String())
	}
	listed = struct {
		Items []DataTenantInfo `json:"items"`
	}{}
	if err := json.NewDecoder(w.Body).Decode(&listed); err != nil {
		t.Fatalf("decode global listed tenants: %v", err)
	}
	if len(listed.Items) < 2 {
		t.Fatalf("global admin should see all synced tenants: %#v", listed)
	}
}

func TestHubRegistrationHTTPPartialSavePreservesExistingFields(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	initResult, err := svc.InitializeAdmin(t.Context(), InitializeAdminInput{Username: "admin", Password: "change-me-123"})
	if err != nil {
		t.Fatalf("InitializeAdmin: %v", err)
	}
	server := NewHTTPServer(svc, "", "test")

	request := func(body any) HubRegistrationResult {
		req := jsonRequest(http.MethodPost, "/api/v1/data/admin/hub-registration", body)
		req.Header.Set("Authorization", "Bearer "+initResult.Token)
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("save Hub registration status=%d body=%s", w.Code, w.Body.String())
		}
		var out HubRegistrationResult
		if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
			t.Fatalf("decode Hub registration response: %v", err)
		}
		return out
	}

	first := request(SaveHubRegistrationInput{HubBaseURL: "http://127.0.0.1:18181", PlatformID: "datasrv-custom", PlatformName: "DataSrv Custom", CallbackBaseURL: "http://127.0.0.1:18180", VirtualMailDomain: "DATA.EXAMPLE"})
	if first.Status.PlatformID != "datasrv-custom" || first.Status.VirtualMailDomain != "data.example" {
		t.Fatalf("initial save did not normalize expected fields: %#v", first.Status)
	}
	patched := request(map[string]any{"hub_base_url": "http://127.0.0.1:18182"})
	if patched.Status.HubBaseURL != "http://127.0.0.1:18182" || patched.Status.PlatformID != "datasrv-custom" || patched.Status.PlatformName != "DataSrv Custom" || patched.Status.CallbackBaseURL != "http://127.0.0.1:18180" || patched.Status.VirtualMailDomain != "data.example" {
		t.Fatalf("partial save should preserve omitted fields: %#v", patched.Status)
	}
	cleared := request(map[string]any{"virtual_mail_domain": ""})
	if cleared.Status.VirtualMailDomain != "" || cleared.Status.PlatformID != "datasrv-custom" {
		t.Fatalf("explicit empty field should clear only that setting: %#v", cleared.Status)
	}
}

func TestTenantAdminCanSeeSyncedTenantByHubIDOrSlug(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	initResult, err := svc.InitializeAdmin(t.Context(), InitializeAdminInput{Username: "admin", Password: "change-me-123"})
	if err != nil {
		t.Fatalf("InitializeAdmin: %v", err)
	}
	global := Principal{TenantID: initResult.TenantID, UserID: "admin", Role: "data_admin", AdminScope: "global"}
	if _, err := svc.SyncHubTenants(t.Context(), global, SyncHubTenantsInput{Tenants: []DataTenantInfo{{ID: "tenant-local", HubTenantID: "hub-tenant-a", Slug: "tenant-a", Name: "Tenant A"}}}); err != nil {
		t.Fatalf("SyncHubTenants: %v", err)
	}
	for _, tenantID := range []string{"tenant-local", "hub-tenant-a", "tenant-a"} {
		items, err := svc.ListDataTenants(t.Context(), Principal{TenantID: tenantID, UserID: "tenant-admin", Role: "data_admin", AdminScope: "tenant"})
		if err != nil {
			t.Fatalf("ListDataTenants %s: %v", tenantID, err)
		}
		if len(items) != 1 || items[0].ID != "tenant-local" {
			t.Fatalf("tenant %s should see synced tenant by id/hub id/slug, got %#v", tenantID, items)
		}
	}
}

func TestServiceTokenAdminScopeHeaderControlsGlobalHubRegistration(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	server := NewHTTPServer(NewService(store, "sqlite"), "root-token-0123456789012345", "test")

	request := func(adminScope string) *httptest.ResponseRecorder {
		req := jsonRequest(http.MethodPost, "/api/v1/data/admin/hub-registration", SaveHubRegistrationInput{HubBaseURL: "http://127.0.0.1:18181", PlatformID: "datasrv-root"})
		req.Header.Set("Authorization", "Bearer root-token-0123456789012345")
		req.Header.Set("X-MaClaw-Tenant-ID", "default")
		req.Header.Set("X-MaClaw-User-ID", "root-service")
		req.Header.Set("X-MaClaw-Role", "data_admin")
		if adminScope != "" {
			req.Header.Set("X-MaClaw-Admin-Scope", adminScope)
		}
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		return w
	}

	if w := request(""); w.Code != http.StatusForbidden {
		t.Fatalf("service token without admin scope should be tenant-scoped status=%d body=%s", w.Code, w.Body.String())
	}
	if w := request("tenant"); w.Code != http.StatusForbidden {
		t.Fatalf("service token tenant admin scope should be forbidden status=%d body=%s", w.Code, w.Body.String())
	}
	if w := request("global"); w.Code != http.StatusOK {
		t.Fatalf("service token global admin scope should save Hub registration status=%d body=%s", w.Code, w.Body.String())
	}
}

func readSignedJSON(t *testing.T, r *http.Request, out any) []byte {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	if err := json.Unmarshal(body, out); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	return body
}

func verifyHubRequestSignature(t *testing.T, r *http.Request, publicKeyPEM string) {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read signed body: %v", err)
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	block, _ := pem.Decode([]byte(publicKeyPEM))
	if block == nil {
		t.Fatal("decode public key")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		t.Fatalf("parse public key: %v", err)
	}
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		t.Fatal("public key is not RSA")
	}
	sig, err := base64.StdEncoding.DecodeString(r.Header.Get("X-VE-Signature"))
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	digest := sha256.Sum256(hubSignaturePayload(r.Method, r.URL.RequestURI(), r.Header.Get("X-VE-Timestamp"), r.Header.Get("X-VE-Nonce"), body))
	if err := rsa.VerifyPKCS1v15(rsaPub, crypto.SHA256, digest[:], sig); err != nil {
		t.Fatalf("verify signature: %v", err)
	}
}
