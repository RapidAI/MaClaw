package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/center"
	"github.com/RapidAI/CodeClaw/hub/internal/config"
	"github.com/RapidAI/CodeClaw/hub/internal/device"
	"github.com/RapidAI/CodeClaw/hub/internal/llmcache"
	"github.com/RapidAI/CodeClaw/hub/internal/session"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
	"github.com/RapidAI/CodeClaw/hub/internal/ws"
)

type hubAdminRouterTestServices struct {
	handler http.Handler
	admins  *auth.AdminService
	store   *store.Store
}

func TestResolveHubRuntimeDataDirPrefersSQLiteDSNDirectory(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Database.DSN = filepath.Join(dir, "hub.db")
	if got := resolveHubRuntimeDataDir(cfg, ""); got != dir {
		t.Fatalf("runtime data dir = %q, want %q", got, dir)
	}

	cfg.Database.DSN = "file:" + filepath.Join(dir, "hub-file.db") + "?cache=shared&_pragma=busy_timeout(5000)"
	if got := resolveHubRuntimeDataDir(cfg, ""); got != dir {
		t.Fatalf("file DSN runtime data dir = %q, want %q", got, dir)
	}
}

func TestResolveHubRuntimeDataDirFallsBackForMemorySQLite(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("create config dir: %v", err)
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("create data dir: %v", err)
	}
	cfg := config.Default()
	cfg.Database.DSN = ":memory:"
	configPath := filepath.Join(configDir, "hub.yaml")
	if got := resolveHubRuntimeDataDir(cfg, configPath); got != dataDir {
		t.Fatalf("memory DSN runtime data dir = %q, want %q", got, dataDir)
	}
}
func newAdminRouterTestServices(t *testing.T) (http.Handler, *auth.AdminService) {
	t.Helper()
	services := newAdminRouterTestContext(t)
	return services.handler, services.admins
}

func newAdminRouterTestContext(t *testing.T) *hubAdminRouterTestServices {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "hub-admin-router-test.db")
	provider, err := sqlite.NewProvider(sqlite.Config{
		DSN:               dbPath,
		WAL:               true,
		BusyTimeoutMS:     5000,
		MaxReadOpenConns:  4,
		MaxReadIdleConns:  2,
		MaxWriteOpenConns: 1,
		MaxWriteIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	if err := sqlite.RunMigrations(provider.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() {
		_ = provider.Close()
	})

	st := sqlite.NewStore(provider)
	admins := auth.NewAdminService(st.Admins, st.System, st.AdminAudit)
	promptCache := llmcache.New(st.LLMPromptCache, llmcache.Config{})
	identity := auth.NewIdentityService(st.Users, st.Enrollments, st.EmailBlocks, st.Machines, st.ViewerTokens, st.LoginTokens, st.System, nil, "open", true, nil, "http://127.0.0.1:8080")
	testCfg := config.Default()
	testCfg.Database.DSN = dbPath
	testCfg.Center.BaseURL = ""
	testCfg.Center.BaseURLs = nil
	centerSvc := center.NewService(testCfg, st.System)
	deviceSvc := device.NewService(st.Machines, device.NewRuntime())
	sessionSvc := session.NewService(session.NewCache(), st.Sessions)
	gateway := &ws.Gateway{Identity: identity, Devices: deviceSvc, Sessions: sessionSvc}
	router := NewRouter(
		admins,
		identity,
		centerSvc,
		nil,
		gateway,
		deviceSvc,
		sessionSvc,
		nil,
		st.EmailInvites,
		st.System,
		provider.Write,
		promptCache,
		st.AdminAudit,
		st.FailureLogs,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		testCfg,
		"",
		nil,
		"",
		"/app",
		"",
		st.Tenants,
	)
	return &hubAdminRouterTestServices{
		handler: router,
		admins:  admins,
		store:   st,
	}
}

func doHubAdminJSONRequest(t *testing.T, handler http.Handler, method, target string, body any, token string) *httptest.ResponseRecorder {
	t.Helper()

	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}

	req := httptest.NewRequest(method, target, &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func issueHubAdminToken(t *testing.T, handler http.Handler) string {
	t.Helper()

	setupResp := doHubAdminJSONRequest(t, handler, http.MethodPost, "/api/admin/setup", map[string]any{
		"username": "admin",
		"password": "StrongPassword123!",
		"email":    "admin@example.com",
	}, "")
	if setupResp.Code != http.StatusOK {
		t.Fatalf("setup status = %d body=%s", setupResp.Code, setupResp.Body.String())
	}

	loginResp := doHubAdminJSONRequest(t, handler, http.MethodPost, "/api/admin/login", map[string]any{
		"username": "admin",
		"password": "StrongPassword123!",
	}, "")
	if loginResp.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", loginResp.Code, loginResp.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(loginResp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	token, _ := payload["access_token"].(string)
	if token == "" {
		t.Fatalf("expected access token, got %v", payload)
	}
	return token
}

func TestAdminDebugHandlersRequireToken(t *testing.T) {
	router, _ := newAdminRouterTestServices(t)

	resp := doHubAdminJSONRequest(t, router, http.MethodGet, "/api/admin/debug/machines", nil, "")
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestAdminStatusHandlerReflectsInitialization(t *testing.T) {
	router, _ := newAdminRouterTestServices(t)

	resp := doHubAdminJSONRequest(t, router, http.MethodGet, "/api/admin/status", nil, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200 before setup, got %d body=%s", resp.Code, resp.Body.String())
	}
	if !bytes.Contains(resp.Body.Bytes(), []byte(`"initialized":false`)) {
		t.Fatalf("expected uninitialized response, got %s", resp.Body.String())
	}

	issueHubAdminToken(t, router)

	resp = doHubAdminJSONRequest(t, router, http.MethodGet, "/api/admin/status", nil, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200 after setup, got %d body=%s", resp.Code, resp.Body.String())
	}
	if !bytes.Contains(resp.Body.Bytes(), []byte(`"initialized":true`)) {
		t.Fatalf("expected initialized response, got %s", resp.Body.String())
	}
}

func TestAdminSetupHandlerRequiresEmail(t *testing.T) {
	router, _ := newAdminRouterTestServices(t)

	resp := doHubAdminJSONRequest(t, router, http.MethodPost, "/api/admin/setup", map[string]any{
		"username": "admin",
		"password": "StrongPassword123!",
	}, "")
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", resp.Code, resp.Body.String())
	}
	if !bytes.Contains(resp.Body.Bytes(), []byte(`"code":"INVALID_INPUT"`)) {
		t.Fatalf("expected invalid input response, got %s", resp.Body.String())
	}
}

func TestAdminDebugHandlersAcceptToken(t *testing.T) {
	router, _ := newAdminRouterTestServices(t)
	token := issueHubAdminToken(t, router)

	resp := doHubAdminJSONRequest(t, router, http.MethodGet, "/api/admin/debug/machines", nil, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestAdminPromptCacheClearHandlerAcceptsToken(t *testing.T) {
	router, _ := newAdminRouterTestServices(t)
	token := issueHubAdminToken(t, router)
	resp := doHubAdminJSONRequest(t, router, http.MethodPost, "/api/admin/hub_llm_prompt_cache_clear", nil, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestAdminPromptCacheEntriesHandlerAcceptsToken(t *testing.T) {
	router, _ := newAdminRouterTestServices(t)
	token := issueHubAdminToken(t, router)
	resp := doHubAdminJSONRequest(t, router, http.MethodGet, "/api/admin/hub_llm_prompt_cache_entries?limit=5", nil, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestAdminPromptCacheEntryDeleteHandlerAcceptsToken(t *testing.T) {
	router, _ := newAdminRouterTestServices(t)
	token := issueHubAdminToken(t, router)
	resp := doHubAdminJSONRequest(t, router, http.MethodDelete, "/api/admin/hub_llm_prompt_cache_entry?cache_key=test", nil, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestTenantAdminRoutesRequireGlobalAdminForTenantCreate(t *testing.T) {
	ctx := newAdminRouterTestContext(t)
	token := issueHubAdminToken(t, ctx.handler)

	createResp := doHubAdminJSONRequest(t, ctx.handler, http.MethodPost, "/api/admin/tenants", map[string]any{
		"slug":                   "acme",
		"name":                   "Acme Corp",
		"primary_domain":         "acme.com",
		"initial_admin_username": "acme-owner",
		"initial_admin_password": "StrongPassword123!",
		"initial_admin_email":    "owner@acme.com",
	}, token)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create tenant status = %d body=%s", createResp.Code, createResp.Body.String())
	}

	loginResp := doHubAdminJSONRequest(t, ctx.handler, http.MethodPost, "/api/admin/login", map[string]any{
		"username": "acme-owner",
		"password": "StrongPassword123!",
	}, "")
	if loginResp.Code != http.StatusOK {
		t.Fatalf("tenant admin login status = %d body=%s", loginResp.Code, loginResp.Body.String())
	}
	var loginPayload struct {
		AccessToken string `json:"access_token"`
		Admin       struct {
			Scope    string `json:"scope"`
			Role     string `json:"role"`
			TenantID string `json:"tenant_id"`
		} `json:"admin"`
	}
	if err := json.Unmarshal(loginResp.Body.Bytes(), &loginPayload); err != nil {
		t.Fatalf("decode tenant admin login: %v", err)
	}
	if loginPayload.Admin.Scope != "tenant" || loginPayload.Admin.Role != "tenant_owner" || loginPayload.Admin.TenantID != "tenant_acme" {
		t.Fatalf("unexpected tenant admin payload: %+v", loginPayload.Admin)
	}

	denied := doHubAdminJSONRequest(t, ctx.handler, http.MethodPost, "/api/admin/tenants", map[string]any{
		"slug":                   "other",
		"name":                   "Other Corp",
		"initial_admin_username": "other-owner",
		"initial_admin_password": "StrongPassword123!",
		"initial_admin_email":    "owner@other.com",
	}, loginPayload.AccessToken)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("tenant admin create tenant status = %d body=%s", denied.Code, denied.Body.String())
	}

	centerUpdate := doHubAdminJSONRequest(t, ctx.handler, http.MethodPost, "/api/admin/center/config", map[string]any{"base_url": "https://center.example.com"}, loginPayload.AccessToken)
	if centerUpdate.Code != http.StatusForbidden {
		t.Fatalf("tenant admin center config status = %d body=%s", centerUpdate.Code, centerUpdate.Body.String())
	}

	centerRegister := doHubAdminJSONRequest(t, ctx.handler, http.MethodPost, "/api/admin/center/register", map[string]any{}, loginPayload.AccessToken)
	if centerRegister.Code != http.StatusForbidden {
		t.Fatalf("tenant admin center register status = %d body=%s", centerRegister.Code, centerRegister.Body.String())
	}
}

func TestTenantAdminLLMProviderTestKeyUsesTenantScope(t *testing.T) {
	ctx := newAdminRouterTestContext(t)
	globalToken := issueHubAdminToken(t, ctx.handler)

	createResp := doHubAdminJSONRequest(t, ctx.handler, http.MethodPost, "/api/admin/tenants", map[string]any{
		"slug":                   "acme",
		"name":                   "Acme Corp",
		"initial_admin_username": "acme-owner",
		"initial_admin_password": "StrongPassword123!",
		"initial_admin_email":    "owner@acme.com",
	}, globalToken)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create tenant status = %d body=%s", createResp.Code, createResp.Body.String())
	}

	loginResp := doHubAdminJSONRequest(t, ctx.handler, http.MethodPost, "/api/admin/login", map[string]any{
		"username": "acme-owner",
		"password": "StrongPassword123!",
	}, "")
	if loginResp.Code != http.StatusOK {
		t.Fatalf("tenant admin login status = %d body=%s", loginResp.Code, loginResp.Body.String())
	}
	var loginPayload struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(loginResp.Body.Bytes(), &loginPayload); err != nil {
		t.Fatalf("decode tenant admin login: %v", err)
	}

	resp := doHubAdminJSONRequest(t, ctx.handler, http.MethodPost, "/api/admin/llm/providers/test-key", map[string]any{"email": "dev@acme.com"}, loginPayload.AccessToken)
	if resp.Code != http.StatusOK {
		t.Fatalf("provider test key status = %d body=%s", resp.Code, resp.Body.String())
	}
	var payload struct {
		AccessToken string `json:"access_token"`
		Email       string `json:"email"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode provider test key: %v", err)
	}
	if payload.AccessToken == "" || payload.Email != "dev@acme.com" {
		t.Fatalf("unexpected provider test key payload: %#v", payload)
	}
	user, err := ctx.store.Users.GetByTenantEmail(context.Background(), "tenant_acme", "dev@acme.com")
	if err != nil {
		t.Fatalf("get tenant user: %v", err)
	}
	if user == nil {
		t.Fatal("expected test key user in tenant_acme")
	}
	defaultUser, err := ctx.store.Users.GetByTenantEmail(context.Background(), store.DefaultTenantID, "dev@acme.com")
	if err != nil {
		t.Fatalf("get default user: %v", err)
	}
	if defaultUser != nil {
		t.Fatalf("test key should not create default tenant user: %#v", defaultUser)
	}
	tokenHash := sha256.Sum256([]byte(payload.AccessToken))
	principal, err := ctx.store.ViewerTokens.GetByTokenHash(context.Background(), hex.EncodeToString(tokenHash[:]))
	if err != nil {
		t.Fatalf("get viewer token: %v", err)
	}
	if principal == nil || principal.TenantID != "tenant_acme" || principal.UserID != user.ID {
		t.Fatalf("viewer token not tenant scoped: %#v user=%#v", principal, user)
	}
}

func TestTenantAdminEmailInvitesAreTenantScoped(t *testing.T) {
	ctx := newAdminRouterTestContext(t)
	globalToken := issueHubAdminToken(t, ctx.handler)

	createResp := doHubAdminJSONRequest(t, ctx.handler, http.MethodPost, "/api/admin/tenants", map[string]any{
		"slug":                   "acme",
		"name":                   "Acme Corp",
		"initial_admin_username": "acme-owner",
		"initial_admin_password": "StrongPassword123!",
		"initial_admin_email":    "owner@acme.com",
	}, globalToken)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create tenant status = %d body=%s", createResp.Code, createResp.Body.String())
	}
	loginResp := doHubAdminJSONRequest(t, ctx.handler, http.MethodPost, "/api/admin/login", map[string]any{
		"username": "acme-owner",
		"password": "StrongPassword123!",
	}, "")
	if loginResp.Code != http.StatusOK {
		t.Fatalf("tenant admin login status = %d body=%s", loginResp.Code, loginResp.Body.String())
	}
	var loginPayload struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(loginResp.Body.Bytes(), &loginPayload); err != nil {
		t.Fatalf("decode tenant admin login: %v", err)
	}

	globalInvite := doHubAdminJSONRequest(t, ctx.handler, http.MethodPost, "/api/admin/invites", map[string]any{"email": "global@example.com", "role": "viewer"}, globalToken)
	if globalInvite.Code != http.StatusOK {
		t.Fatalf("global invite status = %d body=%s", globalInvite.Code, globalInvite.Body.String())
	}
	tenantInvite := doHubAdminJSONRequest(t, ctx.handler, http.MethodPost, "/api/admin/invites", map[string]any{"email": "tenant@example.com", "role": "viewer"}, loginPayload.AccessToken)
	if tenantInvite.Code != http.StatusOK {
		t.Fatalf("tenant invite status = %d body=%s", tenantInvite.Code, tenantInvite.Body.String())
	}
	var tenantPayload struct {
		ID       string `json:"id"`
		TenantID string `json:"tenant_id"`
	}
	if err := json.Unmarshal(tenantInvite.Body.Bytes(), &tenantPayload); err != nil {
		t.Fatalf("decode tenant invite: %v", err)
	}
	if tenantPayload.TenantID != "tenant_acme" {
		t.Fatalf("tenant invite tenant_id = %q", tenantPayload.TenantID)
	}

	listResp := doHubAdminJSONRequest(t, ctx.handler, http.MethodGet, "/api/admin/invites", nil, loginPayload.AccessToken)
	if listResp.Code != http.StatusOK {
		t.Fatalf("tenant list invites status = %d body=%s", listResp.Code, listResp.Body.String())
	}
	var listPayload struct {
		Invites []struct {
			ID       string `json:"id"`
			TenantID string `json:"tenant_id"`
			Email    string `json:"email"`
		} `json:"invites"`
	}
	if err := json.Unmarshal(listResp.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode tenant list: %v", err)
	}
	if len(listPayload.Invites) != 1 || listPayload.Invites[0].Email != "tenant@example.com" || listPayload.Invites[0].TenantID != "tenant_acme" {
		t.Fatalf("unexpected tenant invites: %#v", listPayload.Invites)
	}

	var globalPayload struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(globalInvite.Body.Bytes(), &globalPayload); err != nil {
		t.Fatalf("decode global invite: %v", err)
	}
	deleteGlobal := doHubAdminJSONRequest(t, ctx.handler, http.MethodDelete, "/api/admin/invites/"+globalPayload.ID, nil, loginPayload.AccessToken)
	if deleteGlobal.Code != http.StatusNotFound {
		t.Fatalf("tenant delete global invite status = %d body=%s", deleteGlobal.Code, deleteGlobal.Body.String())
	}
	deleteOwn := doHubAdminJSONRequest(t, ctx.handler, http.MethodDelete, "/api/admin/invites/"+tenantPayload.ID, nil, loginPayload.AccessToken)
	if deleteOwn.Code != http.StatusOK {
		t.Fatalf("tenant delete own invite status = %d body=%s", deleteOwn.Code, deleteOwn.Body.String())
	}
}

func TestTenantAdminSystemSettingsAreTenantScoped(t *testing.T) {
	ctx := newAdminRouterTestContext(t)
	globalToken := issueHubAdminToken(t, ctx.handler)

	createResp := doHubAdminJSONRequest(t, ctx.handler, http.MethodPost, "/api/admin/tenants", map[string]any{
		"slug":                   "acme",
		"name":                   "Acme Corp",
		"initial_admin_username": "acme-owner",
		"initial_admin_password": "StrongPassword123!",
		"initial_admin_email":    "owner@acme.com",
	}, globalToken)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create tenant status = %d body=%s", createResp.Code, createResp.Body.String())
	}
	loginResp := doHubAdminJSONRequest(t, ctx.handler, http.MethodPost, "/api/admin/login", map[string]any{
		"username": "acme-owner",
		"password": "StrongPassword123!",
	}, "")
	if loginResp.Code != http.StatusOK {
		t.Fatalf("tenant admin login status = %d body=%s", loginResp.Code, loginResp.Body.String())
	}
	var loginPayload struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(loginResp.Body.Bytes(), &loginPayload); err != nil {
		t.Fatalf("decode tenant admin login: %v", err)
	}

	globalSmart := doHubAdminJSONRequest(t, ctx.handler, http.MethodPut, "/api/admin/smart_route_all", map[string]any{"enabled": true}, globalToken)
	if globalSmart.Code != http.StatusOK {
		t.Fatalf("global smart route status = %d body=%s", globalSmart.Code, globalSmart.Body.String())
	}
	tenantSmart := doHubAdminJSONRequest(t, ctx.handler, http.MethodPut, "/api/admin/smart_route_all", map[string]any{"enabled": false}, loginPayload.AccessToken)
	if tenantSmart.Code != http.StatusOK {
		t.Fatalf("tenant smart route status = %d body=%s", tenantSmart.Code, tenantSmart.Body.String())
	}
	globalGet := doHubAdminJSONRequest(t, ctx.handler, http.MethodGet, "/api/admin/smart_route_all", nil, globalToken)
	tenantGet := doHubAdminJSONRequest(t, ctx.handler, http.MethodGet, "/api/admin/smart_route_all", nil, loginPayload.AccessToken)
	if !bytes.Contains(globalGet.Body.Bytes(), []byte(`"enabled":true`)) {
		t.Fatalf("global smart route leaked tenant update: %s", globalGet.Body.String())
	}
	if !bytes.Contains(tenantGet.Body.Bytes(), []byte(`"enabled":false`)) {
		t.Fatalf("tenant smart route = %s", tenantGet.Body.String())
	}

	globalAudit := doHubAdminJSONRequest(t, ctx.handler, http.MethodPut, "/api/admin/content_audit/config", map[string]any{"program_path": "global-audit", "timeout_seconds": 3, "timeout_policy": "pass"}, globalToken)
	if globalAudit.Code != http.StatusOK {
		t.Fatalf("global content audit status = %d body=%s", globalAudit.Code, globalAudit.Body.String())
	}
	tenantAudit := doHubAdminJSONRequest(t, ctx.handler, http.MethodPut, "/api/admin/content_audit/config", map[string]any{"program_path": "tenant-audit", "timeout_seconds": 5, "timeout_policy": "block"}, loginPayload.AccessToken)
	if tenantAudit.Code != http.StatusOK {
		t.Fatalf("tenant content audit status = %d body=%s", tenantAudit.Code, tenantAudit.Body.String())
	}
	globalAuditGet := doHubAdminJSONRequest(t, ctx.handler, http.MethodGet, "/api/admin/content_audit/config", nil, globalToken)
	tenantAuditGet := doHubAdminJSONRequest(t, ctx.handler, http.MethodGet, "/api/admin/content_audit/config", nil, loginPayload.AccessToken)
	if !bytes.Contains(globalAuditGet.Body.Bytes(), []byte(`"program_path":"global-audit"`)) {
		t.Fatalf("global content audit leaked tenant update: %s", globalAuditGet.Body.String())
	}
	if !bytes.Contains(tenantAuditGet.Body.Bytes(), []byte(`"program_path":"tenant-audit"`)) {
		t.Fatalf("tenant content audit = %s", tenantAuditGet.Body.String())
	}
}
