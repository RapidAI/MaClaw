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
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/center"
	"github.com/RapidAI/CodeClaw/hub/internal/config"
	"github.com/RapidAI/CodeClaw/hub/internal/device"
	"github.com/RapidAI/CodeClaw/hub/internal/invitation"
	"github.com/RapidAI/CodeClaw/hub/internal/llmcache"
	"github.com/RapidAI/CodeClaw/hub/internal/mail"
	securitypkg "github.com/RapidAI/CodeClaw/hub/internal/security"
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
	invitationSvc := invitation.NewService(st.InvitationCodes, st.System)
	identity := auth.NewIdentityService(st.Users, st.Enrollments, st.EmailBlocks, st.Machines, st.ViewerTokens, st.LoginTokens, st.System, invitationSvc, "open", true, nil, "http://127.0.0.1:8080")
	testCfg := config.Default()
	testCfg.Database.DSN = dbPath
	testCfg.Center.BaseURL = ""
	testCfg.Center.BaseURLs = nil
	centerSvc := center.NewService(testCfg, st.System)
	deviceSvc := device.NewService(st.Machines, device.NewRuntime())
	sessionSvc := session.NewService(session.NewCache(), st.Sessions)
	securityStore := securitypkg.NewSecurityStore(provider.Write)
	if err := securityStore.InitSchema(context.Background()); err != nil {
		t.Fatalf("init security schema: %v", err)
	}
	if err := securityStore.InitRootGroup(context.Background()); err != nil {
		t.Fatalf("init security root group: %v", err)
	}
	securitySvc := securitypkg.NewSecurityService(securityStore, st.System, st.AdminAudit)
	gateway := &ws.Gateway{Identity: identity, Devices: deviceSvc, Sessions: sessionSvc}
	router := NewRouter(
		admins,
		identity,
		centerSvc,
		nil,
		gateway,
		deviceSvc,
		sessionSvc,
		invitationSvc,
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
		securitySvc,
		testCfg,
		"",
		nil,
		"",
		"/app",
		"",
		nil,
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

func issueTenantAdminToken(t *testing.T, handler http.Handler, globalToken, tenantSlug, username string) string {
	t.Helper()

	tenantID := "tenant_" + tenantSlug
	createResp := doHubAdminJSONRequest(t, handler, http.MethodPost, "/api/admin/tenants", map[string]any{
		"slug":                   tenantSlug,
		"name":                   tenantSlug + " tenant",
		"initial_admin_username": username,
		"initial_admin_password": "StrongPassword123!",
		"initial_admin_email":    username + "@example.com",
	}, globalToken)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create tenant %s status = %d body=%s", tenantSlug, createResp.Code, createResp.Body.String())
	}

	loginResp := doHubAdminJSONRequest(t, handler, http.MethodPost, "/api/admin/login", map[string]any{
		"username": username,
		"password": "StrongPassword123!",
		"tenant":   tenantID,
	}, "")
	if loginResp.Code != http.StatusOK {
		t.Fatalf("tenant admin login status = %d body=%s", loginResp.Code, loginResp.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(loginResp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode tenant login response: %v", err)
	}
	token, _ := payload["access_token"].(string)
	if token == "" {
		t.Fatalf("expected tenant access token, got %v", payload)
	}
	return token
}

func TestTenantBridgeConfigDoesNotRewriteSharedConfigFile(t *testing.T) {
	ctx := newAdminRouterTestContext(t)
	bridgeDir := t.TempDir()
	globalAdmin := &store.AdminUser{ID: "global-admin", Scope: "global"}
	tenantAdmin := &store.AdminUser{ID: "tenant-admin", Scope: "tenant", TenantID: "tenant_acme"}
	postChannel := SaveBridgeChannelHandler(ctx.store.System, bridgeDir)
	postIM := UpdateOpenclawIMConfigHandler(ctx.store.System, bridgeDir)

	send := func(handler http.HandlerFunc, body any, admin *store.AdminUser) *httptest.ResponseRecorder {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/admin/test", bytes.NewReader(data))
		req = req.WithContext(context.WithValue(req.Context(), adminUserContextKey, admin))
		rec := httptest.NewRecorder()
		handler(rec, req)
		return rec
	}

	globalResp := send(postChannel, map[string]any{"id": "telegram", "enabled": false, "fields": map[string]string{"botToken": "global-token"}}, globalAdmin)
	if globalResp.Code != http.StatusOK {
		t.Fatalf("global channel save status=%d body=%s", globalResp.Code, globalResp.Body.String())
	}
	configPath := filepath.Join(bridgeDir, "config.json")
	globalConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read global bridge config: %v", err)
	}
	if !bytes.Contains(globalConfig, []byte("global-token")) {
		t.Fatalf("expected global bridge config to contain global channel: %s", string(globalConfig))
	}

	tenantResp := send(postChannel, map[string]any{"id": "telegram", "enabled": false, "fields": map[string]string{"botToken": "tenant-token"}}, tenantAdmin)
	if tenantResp.Code != http.StatusOK {
		t.Fatalf("tenant channel save status=%d body=%s", tenantResp.Code, tenantResp.Body.String())
	}
	afterTenantChannel, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read bridge config after tenant channel save: %v", err)
	}
	if !bytes.Equal(afterTenantChannel, globalConfig) || bytes.Contains(afterTenantChannel, []byte("tenant-token")) {
		t.Fatalf("tenant channel save rewrote shared bridge config: %s", string(afterTenantChannel))
	}

	tenantIMResp := send(postIM, OpenclawIMConfigState{Enabled: true, WebhookURL: "http://127.0.0.1:3210/outbound", Secret: "tenant-secret"}, tenantAdmin)
	if tenantIMResp.Code != http.StatusOK {
		t.Fatalf("tenant im save status=%d body=%s", tenantIMResp.Code, tenantIMResp.Body.String())
	}
	afterTenantIM, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read bridge config after tenant im save: %v", err)
	}
	if !bytes.Equal(afterTenantIM, globalConfig) || bytes.Contains(afterTenantIM, []byte("tenant-secret")) {
		t.Fatalf("tenant im save rewrote shared bridge config: %s", string(afterTenantIM))
	}
}
func TestAdminDebugHandlersRequireToken(t *testing.T) {
	router, _ := newAdminRouterTestServices(t)

	resp := doHubAdminJSONRequest(t, router, http.MethodGet, "/api/admin/debug/machines", nil, "")
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestBridgeChannelsReturnReadableChineseLabels(t *testing.T) {
	ctx := newAdminRouterTestContext(t)
	tenantAdmin := &store.AdminUser{ID: "tenant-admin", Scope: "tenant", TenantID: "tenant_acme"}
	req := httptest.NewRequest(http.MethodGet, "/api/admin/bridge/channels", nil)
	req = req.WithContext(context.WithValue(req.Context(), adminUserContextKey, tenantAdmin))
	rec := httptest.NewRecorder()
	GetBridgeChannelsHandler(ctx.store.System, "")(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bridge channels status=%d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Channels []struct {
			ID     string `json:"id"`
			NameZH string `json:"name_zh"`
			DescZH string `json:"desc_zh"`
			Fields []struct {
				Key     string `json:"key"`
				LabelZH string `json:"label_zh"`
			} `json:"fields"`
		} `json:"channels"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode bridge channels: %v body=%s", err, rec.Body.String())
	}
	var foundWeChat, foundDingTalk bool
	for _, channel := range payload.Channels {
		switch channel.ID {
		case "wechatwork":
			foundWeChat = true
			if channel.NameZH != "\u4f01\u4e1a\u5fae\u4fe1" || channel.DescZH != "\u8fde\u63a5\u4f01\u4e1a\u5fae\u4fe1\u673a\u5668\u4eba\u3002" {
				t.Fatalf("wechatwork zh labels are not readable: %#v", channel)
			}
		case "dingtalk":
			foundDingTalk = true
			if channel.NameZH != "\u9489\u9489" || channel.DescZH != "\u8fde\u63a5\u9489\u9489\u673a\u5668\u4eba\u3002" {
				t.Fatalf("dingtalk zh labels are not readable: %#v", channel)
			}
		}
	}
	if !foundWeChat || !foundDingTalk {
		t.Fatalf("expected wechatwork and dingtalk channels, got %#v", payload.Channels)
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

func TestAdminLoginTenantsExposeOnlyActiveTenantsAndGlobalLoginScope(t *testing.T) {
	ctx := newAdminRouterTestContext(t)
	token := issueHubAdminToken(t, ctx.handler)

	now := time.Now().UTC()
	for _, tenant := range []*store.Tenant{
		{ID: "tenant_active", Slug: "active", Name: "Active Tenant", Status: "active", CreatedAt: now, UpdatedAt: now},
		{ID: "tenant_inactive", Slug: "inactive", Name: "Inactive Tenant", Status: "inactive", CreatedAt: now, UpdatedAt: now},
		{ID: "tenant_deleted", Slug: "deleted", Name: "Deleted Tenant", Status: "active", CreatedAt: now, UpdatedAt: now},
	} {
		if err := ctx.store.Tenants.Create(context.Background(), tenant); err != nil {
			t.Fatalf("seed tenant %s: %v", tenant.ID, err)
		}
	}
	if err := ctx.store.Tenants.DeleteByID(context.Background(), "tenant_deleted"); err != nil {
		t.Fatalf("delete tenant: %v", err)
	}

	resp := doHubAdminJSONRequest(t, ctx.handler, http.MethodGet, "/api/admin/login/tenants", nil, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("login tenants status=%d body=%s", resp.Code, resp.Body.String())
	}
	if !bytes.Contains(resp.Body.Bytes(), []byte("tenant_active")) {
		t.Fatalf("expected active tenant in login choices, body=%s", resp.Body.String())
	}
	if bytes.Contains(resp.Body.Bytes(), []byte("tenant_inactive")) || bytes.Contains(resp.Body.Bytes(), []byte("tenant_deleted")) {
		t.Fatalf("inactive or deleted tenant leaked into login choices, body=%s", resp.Body.String())
	}
	if !bytes.Contains(resp.Body.Bytes(), []byte(store.DefaultTenantID)) {
		t.Fatalf("expected default tenant in login choices, body=%s", resp.Body.String())
	}
	listResp := doHubAdminJSONRequest(t, ctx.handler, http.MethodGet, "/api/admin/tenants", nil, token)
	if listResp.Code != http.StatusOK {
		t.Fatalf("tenant list status=%d body=%s", listResp.Code, listResp.Body.String())
	}
	if !bytes.Contains(listResp.Body.Bytes(), []byte(store.DefaultTenantID)) {
		t.Fatalf("expected default tenant in admin tenant list, body=%s", listResp.Body.String())
	}

	loginResp := doHubAdminJSONRequest(t, ctx.handler, http.MethodPost, "/api/admin/login", map[string]any{
		"username": "admin",
		"password": "StrongPassword123!",
		"tenant":   auth.ExplicitGlobalAdminTenantScope,
	}, "")
	if loginResp.Code != http.StatusOK {
		t.Fatalf("global login scope status=%d body=%s", loginResp.Code, loginResp.Body.String())
	}
	defaultLoginResp := doHubAdminJSONRequest(t, ctx.handler, http.MethodPost, "/api/admin/login", map[string]any{
		"username": "admin",
		"password": "StrongPassword123!",
		"tenant":   store.DefaultTenantID,
	}, "")
	if defaultLoginResp.Code != http.StatusOK {
		t.Fatalf("default tenant login scope status=%d body=%s", defaultLoginResp.Code, defaultLoginResp.Body.String())
	}
	var defaultLoginPayload struct {
		AccessToken string `json:"access_token"`
		Admin       struct {
			TenantID   string `json:"tenant_id"`
			TenantName string `json:"tenant_name"`
		} `json:"admin"`
	}
	if err := json.Unmarshal(defaultLoginResp.Body.Bytes(), &defaultLoginPayload); err != nil {
		t.Fatalf("decode default tenant login: %v", err)
	}
	if defaultLoginPayload.Admin.TenantID != store.DefaultTenantID || defaultLoginPayload.Admin.TenantName == "" {
		t.Fatalf("default tenant login missing tenant context: %#v", defaultLoginPayload.Admin)
	}
	defaultDetailResp := doHubAdminJSONRequest(t, ctx.handler, http.MethodGet, "/api/admin/tenants/"+store.DefaultTenantID, nil, defaultLoginPayload.AccessToken)
	if defaultDetailResp.Code != http.StatusOK {
		t.Fatalf("default tenant token detail status=%d body=%s", defaultDetailResp.Code, defaultDetailResp.Body.String())
	}
	for _, target := range []string{
		"/api/admin/users",
		"/api/admin/blocklist",
		"/api/admin/invites",
		"/api/admin/enrollments/all",
		"/api/admin/llm/providers",
		"/api/admin/llm/services?include_cards=false",
		"/api/admin/failure-logs",
	} {
		refreshResp := doHubAdminJSONRequest(t, ctx.handler, http.MethodGet, target, nil, defaultLoginPayload.AccessToken)
		if refreshResp.Code == http.StatusUnauthorized {
			t.Fatalf("default tenant refresh endpoint %s unauthorized body=%s", target, refreshResp.Body.String())
		}
	}
	if !requestedTenantLoginAllowed(context.Background(), store.DefaultTenantID, ctx.store.Tenants) {
		t.Fatal("default tenant login scope should be allowed")
	}
	if !adminTenantLoginAllowed(context.Background(), &store.AdminUser{Scope: "tenant", TenantID: store.DefaultTenantID}, ctx.store.Tenants) {
		t.Fatal("default tenant admin login should be allowed")
	}
}

func TestGlobalAdminCanDeactivateReactivateAndDeleteTenant(t *testing.T) {
	ctx := newAdminRouterTestContext(t)
	token := issueHubAdminToken(t, ctx.handler)
	callbackBodies := make(chan map[string]any, 4)
	callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/hub/callback/tenant" {
			t.Fatalf("unexpected callback path %s", r.URL.Path)
		}
		if r.Header.Get("X-VE-Callback-Secret") != "secret-1" {
			t.Fatalf("unexpected callback secret %q", r.Header.Get("X-VE-Callback-Secret"))
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode callback body: %v", err)
		}
		callbackBodies <- body
		w.WriteHeader(http.StatusAccepted)
	}))
	defer callback.Close()
	provider := platformProviderEntry{PlatformID: "platform-1", CallbackBaseURL: callback.URL, CallbackSecret: "secret-1", VirtualMailDomain: "ve.example.com", RegistrationStatus: "active"}
	if err := savePlatformProviderRegistry(context.Background(), ctx.store.System, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}

	createResp := doHubAdminJSONRequest(t, ctx.handler, http.MethodPost, "/api/admin/tenants", map[string]any{
		"slug":                   "lifecycle",
		"name":                   "Lifecycle Tenant",
		"initial_admin_username": "life-admin",
		"initial_admin_password": "TenantPass123!",
		"initial_admin_email":    "life@example.com",
	}, token)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create tenant status = %d body=%s", createResp.Code, createResp.Body.String())
	}
	waitTenantCallbackStatus(t, callbackBodies, "active")

	deactivateResp := doHubAdminJSONRequest(t, ctx.handler, http.MethodPatch, "/api/admin/tenants/tenant_lifecycle/status", map[string]any{"status": "inactive"}, token)
	if deactivateResp.Code != http.StatusOK || !bytes.Contains(deactivateResp.Body.Bytes(), []byte(`"status":"inactive"`)) {
		t.Fatalf("deactivate tenant status=%d body=%s", deactivateResp.Code, deactivateResp.Body.String())
	}
	waitTenantCallbackStatus(t, callbackBodies, "disabled")
	inactiveLogin := doHubAdminJSONRequest(t, ctx.handler, http.MethodPost, "/api/admin/login", map[string]any{"username": "life-admin", "password": "TenantPass123!", "tenant": "tenant_lifecycle"}, "")
	if inactiveLogin.Code != http.StatusUnauthorized {
		t.Fatalf("inactive tenant login status = %d body=%s", inactiveLogin.Code, inactiveLogin.Body.String())
	}

	reactivateResp := doHubAdminJSONRequest(t, ctx.handler, http.MethodPatch, "/api/admin/tenants/tenant_lifecycle/status", map[string]any{"status": "active"}, token)
	if reactivateResp.Code != http.StatusOK || !bytes.Contains(reactivateResp.Body.Bytes(), []byte(`"status":"active"`)) {
		t.Fatalf("reactivate tenant status=%d body=%s", reactivateResp.Code, reactivateResp.Body.String())
	}
	waitTenantCallbackStatus(t, callbackBodies, "active")
	activeLogin := doHubAdminJSONRequest(t, ctx.handler, http.MethodPost, "/api/admin/login", map[string]any{"username": "life-admin", "password": "TenantPass123!", "tenant": "tenant_lifecycle"}, "")
	if activeLogin.Code != http.StatusOK {
		t.Fatalf("reactivated tenant login status = %d body=%s", activeLogin.Code, activeLogin.Body.String())
	}

	deleteResp := doHubAdminJSONRequest(t, ctx.handler, http.MethodDelete, "/api/admin/tenants/tenant_lifecycle", nil, token)
	if deleteResp.Code != http.StatusOK || !bytes.Contains(deleteResp.Body.Bytes(), []byte(`"deleted_at"`)) {
		t.Fatalf("delete tenant status=%d body=%s", deleteResp.Code, deleteResp.Body.String())
	}
	waitTenantCallbackStatus(t, callbackBodies, "deleted")
	deletedLogin := doHubAdminJSONRequest(t, ctx.handler, http.MethodPost, "/api/admin/login", map[string]any{"username": "life-admin", "password": "TenantPass123!", "tenant": "tenant_lifecycle"}, "")
	if deletedLogin.Code != http.StatusUnauthorized {
		t.Fatalf("deleted tenant login status = %d body=%s", deletedLogin.Code, deletedLogin.Body.String())
	}
	audits, err := ctx.store.AdminAudit.List(context.Background(), store.AdminAuditLogFilter{Query: "tenant_lifecycle", Limit: 10})
	if err != nil {
		t.Fatalf("list audit logs: %v", err)
	}
	var sawStatus, sawDelete bool
	for _, item := range audits {
		if item.Action == "tenant.status_updated" {
			sawStatus = true
		}
		if item.Action == "tenant.deleted" {
			sawDelete = true
		}
	}
	if !sawStatus || !sawDelete {
		t.Fatalf("tenant lifecycle audit logs missing status=%v delete=%v logs=%#v", sawStatus, sawDelete, audits)
	}
}

func waitTenantCallbackStatus(t *testing.T, callbackBodies <-chan map[string]any, want string) {
	t.Helper()
	select {
	case body := <-callbackBodies:
		if body["hub_tenant_id"] != "tenant_lifecycle" || body["status"] != want {
			t.Fatalf("unexpected tenant callback body: %#v want status %s", body, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("tenant callback status %s was not posted", want)
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

func TestTenantCreateRollsBackTenantWhenInitialAdminCreateFails(t *testing.T) {
	ctx := newAdminRouterTestContext(t)
	token := issueHubAdminToken(t, ctx.handler)
	if _, err := ctx.admins.CreateTenantAdmin(context.Background(), "tenant_bad-admin", "admin", "StrongPassword123!", "existing-admin@example.com", "", "tenant_admin"); err != nil {
		t.Fatalf("seed existing tenant admin: %v", err)
	}

	resp := doHubAdminJSONRequest(t, ctx.handler, http.MethodPost, "/api/admin/tenants", map[string]any{
		"slug":                   "bad-admin",
		"name":                   "Bad Admin Corp",
		"initial_admin_username": "admin",
		"initial_admin_password": "StrongPassword123!",
		"initial_admin_email":    "bad-admin@example.com",
	}, token)
	if resp.Code != http.StatusConflict {
		t.Fatalf("tenant create with duplicate admin status = %d body=%s", resp.Code, resp.Body.String())
	}

	tenant, err := ctx.store.Tenants.GetByID(context.Background(), "tenant_bad-admin")
	if err != nil {
		t.Fatalf("load rolled back tenant: %v", err)
	}
	if tenant != nil {
		t.Fatalf("tenant should be removed after initial admin create failure: %#v", tenant)
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
		"tenant":   "tenant_acme",
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

	listDenied := doHubAdminJSONRequest(t, ctx.handler, http.MethodGet, "/api/admin/tenants", nil, loginPayload.AccessToken)
	if listDenied.Code != http.StatusForbidden {
		t.Fatalf("tenant admin list tenants status = %d body=%s", listDenied.Code, listDenied.Body.String())
	}
	statusDenied := doHubAdminJSONRequest(t, ctx.handler, http.MethodPatch, "/api/admin/tenants/tenant_acme/status", map[string]any{"status": "inactive"}, loginPayload.AccessToken)
	if statusDenied.Code != http.StatusForbidden {
		t.Fatalf("tenant admin update tenant status = %d body=%s", statusDenied.Code, statusDenied.Body.String())
	}
	deleteDenied := doHubAdminJSONRequest(t, ctx.handler, http.MethodDelete, "/api/admin/tenants/tenant_acme", nil, loginPayload.AccessToken)
	if deleteDenied.Code != http.StatusForbidden {
		t.Fatalf("tenant admin delete tenant status = %d body=%s", deleteDenied.Code, deleteDenied.Body.String())
	}
	ownDetail := doHubAdminJSONRequest(t, ctx.handler, http.MethodGet, "/api/admin/tenants/tenant_acme", nil, loginPayload.AccessToken)
	if ownDetail.Code != http.StatusOK {
		t.Fatalf("tenant admin own tenant detail status = %d body=%s", ownDetail.Code, ownDetail.Body.String())
	}
	ownAdmin := doHubAdminJSONRequest(t, ctx.handler, http.MethodPost, "/api/admin/tenants/tenant_acme/admins", map[string]any{
		"username": "acme-admin",
		"password": "StrongPassword123!",
		"email":    "acme-admin@example.com",
	}, loginPayload.AccessToken)
	if ownAdmin.Code != http.StatusCreated {
		t.Fatalf("tenant admin create own admin status = %d body=%s", ownAdmin.Code, ownAdmin.Body.String())
	}
	defaultAdmin := doHubAdminJSONRequest(t, ctx.handler, http.MethodPost, "/api/admin/tenants/"+store.DefaultTenantID+"/admins", map[string]any{
		"username": "default-admin",
		"password": "StrongPassword123!",
		"email":    "default-admin@example.com",
	}, token)
	if defaultAdmin.Code != http.StatusCreated {
		t.Fatalf("global admin create default tenant admin status = %d body=%s", defaultAdmin.Code, defaultAdmin.Body.String())
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
		"tenant":   "tenant_acme",
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

func TestTenantAdminCanManageTenantScopedLLMProviders(t *testing.T) {
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
		"tenant":   "tenant_acme",
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

	saveResp := doHubAdminJSONRequest(t, ctx.handler, http.MethodPut, "/api/admin/llm/providers", map[string]any{
		"enabled":             true,
		"current_provider_id": "tenant-provider",
		"providers": []map[string]any{{
			"id":      "tenant-provider",
			"name":    "Tenant Provider",
			"api_url": "https://tenant-provider.example/v1",
			"api_key": "tenant-secret",
			"model":   "tenant-model",
		}},
	}, loginPayload.AccessToken)
	if saveResp.Code != http.StatusOK {
		t.Fatalf("tenant save provider status = %d body=%s", saveResp.Code, saveResp.Body.String())
	}

	tenantGet := doHubAdminJSONRequest(t, ctx.handler, http.MethodGet, "/api/admin/llm/providers", nil, loginPayload.AccessToken)
	if tenantGet.Code != http.StatusOK {
		t.Fatalf("tenant get provider status = %d body=%s", tenantGet.Code, tenantGet.Body.String())
	}
	var tenantPayload struct {
		CurrentProviderID string `json:"current_provider_id"`
		Providers         []struct {
			ID    string `json:"id"`
			Model string `json:"model"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(tenantGet.Body.Bytes(), &tenantPayload); err != nil {
		t.Fatalf("decode tenant provider response: %v", err)
	}
	if tenantPayload.CurrentProviderID != "tenant-provider" || len(tenantPayload.Providers) != 1 || tenantPayload.Providers[0].ID != "tenant-provider" || tenantPayload.Providers[0].Model != "tenant-model" {
		t.Fatalf("unexpected tenant provider response: %#v", tenantPayload)
	}

	globalGet := doHubAdminJSONRequest(t, ctx.handler, http.MethodGet, "/api/admin/llm/providers", nil, globalToken)
	if globalGet.Code != http.StatusForbidden {
		t.Fatalf("global get tenant provider status = %d body=%s", globalGet.Code, globalGet.Body.String())
	}
	mismatchedTenant := doHubAdminJSONRequest(t, ctx.handler, http.MethodGet, "/api/admin/llm/providers?tenant_id=tenant_other", nil, loginPayload.AccessToken)
	if mismatchedTenant.Code != http.StatusForbidden {
		t.Fatalf("tenant get mismatched provider scope status = %d body=%s", mismatchedTenant.Code, mismatchedTenant.Body.String())
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
		"tenant":   "tenant_acme",
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
		"tenant":   "tenant_acme",
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
	tenantSenderSave := doHubAdminJSONRequest(t, ctx.handler, http.MethodPost, "/api/admin/mail/sender-name", map[string]any{"from_name": "Acme Mail"}, loginPayload.AccessToken)
	if tenantSenderSave.Code != http.StatusOK {
		t.Fatalf("tenant mail sender-name save status = %d body=%s", tenantSenderSave.Code, tenantSenderSave.Body.String())
	}
	tenantSenderGet := doHubAdminJSONRequest(t, ctx.handler, http.MethodGet, "/api/admin/mail/sender-name", nil, loginPayload.AccessToken)
	if tenantSenderGet.Code != http.StatusOK || !bytes.Contains(tenantSenderGet.Body.Bytes(), []byte(`"from_name":"Acme Mail"`)) {
		t.Fatalf("tenant mail sender-name get status = %d body=%s", tenantSenderGet.Code, tenantSenderGet.Body.String())
	}
	assertTenantSettingOnly(t, ctx.store.System, "tenant_acme", mail.TenantSenderNameSettingKey, "Acme Mail")
	globalGet := doHubAdminJSONRequest(t, ctx.handler, http.MethodGet, "/api/admin/smart_route_all", nil, globalToken)
	tenantGet := doHubAdminJSONRequest(t, ctx.handler, http.MethodGet, "/api/admin/smart_route_all", nil, loginPayload.AccessToken)
	if !bytes.Contains(globalGet.Body.Bytes(), []byte(`"enabled":true`)) {
		t.Fatalf("global smart route leaked tenant update: %s", globalGet.Body.String())
	}
	if !bytes.Contains(tenantGet.Body.Bytes(), []byte(`"enabled":false`)) {
		t.Fatalf("tenant smart route = %s", tenantGet.Body.String())
	}

	assertTenantForbidden := func(method, target string, body any) {
		t.Helper()
		resp := doHubAdminJSONRequest(t, ctx.handler, method, target, body, loginPayload.AccessToken)
		if resp.Code != http.StatusForbidden {
			t.Fatalf("tenant %s %s status = %d body=%s", method, target, resp.Code, resp.Body.String())
		}
	}
	for _, endpoint := range []struct {
		method string
		target string
		body   any
	}{
		{http.MethodGet, "/api/admin/center/status", nil},
		{http.MethodGet, "/api/admin/mail/config", nil},
		{http.MethodPost, "/api/admin/mail/config", map[string]any{"enabled": true}},
		{http.MethodPost, "/api/admin/mail/test", map[string]any{"email": "ops@example.com"}},
		{http.MethodGet, "/api/admin/hub_llm_config", nil},
		{http.MethodPut, "/api/admin/hub_llm_config", map[string]any{"enabled": true, "api_url": "https://tenant.example/v1", "api_key": "tenant-key", "model": "tenant-model"}},
		{http.MethodGet, "/api/admin/hub_llm_prompt_cache_config", nil},
		{http.MethodPut, "/api/admin/hub_llm_prompt_cache_config", map[string]any{"enabled": true}},
		{http.MethodPost, "/api/admin/hub_llm_prompt_cache_clear", nil},
		{http.MethodGet, "/api/admin/hub_llm_prompt_cache_entries", nil},
		{http.MethodGet, "/api/admin/hub_llm_prompt_cache_entry?cache_key=test", nil},
		{http.MethodDelete, "/api/admin/hub_llm_prompt_cache_entry?cache_key=test", nil},
		{http.MethodPost, "/api/admin/hub_llm_test", map[string]any{}},
		{http.MethodGet, "/api/admin/hub_llm_status", nil},
	} {
		assertTenantForbidden(endpoint.method, endpoint.target, endpoint.body)
	}

	assertGlobalForbidden := func(method, target string, body any) {
		t.Helper()
		resp := doHubAdminJSONRequest(t, ctx.handler, method, target, body, globalToken)
		if resp.Code != http.StatusForbidden {
			t.Fatalf("global %s %s status = %d body=%s", method, target, resp.Code, resp.Body.String())
		}
	}
	for _, endpoint := range []struct {
		method string
		target string
		body   any
	}{
		{http.MethodGet, "/api/admin/mail/sender-name", nil},
		{http.MethodPost, "/api/admin/mail/sender-name", map[string]any{"from_name": "Global Mail"}},
		{http.MethodGet, "/api/admin/billing/customer-account", nil},
		{http.MethodGet, "/api/admin/billing/licenses", nil},
		{http.MethodGet, "/api/admin/capabilities", nil},
		{http.MethodPost, "/api/admin/capabilities", map[string]any{"capability_id": "global-cap", "display_name": "Global Cap"}},
		{http.MethodGet, "/api/admin/capability-market/policy", nil},
		{http.MethodPut, "/api/admin/capability-market/policy", map[string]any{"policy": map[string]any{"enterprise_only_search": true}}},
		{http.MethodGet, "/api/admin/capability-market/acquisition-requests", nil},
		{http.MethodGet, "/api/admin/capability-market/acquisition-requests/request-1", nil},
		{http.MethodPost, "/api/admin/capability-market/acquisition-requests/request-1/approve", map[string]any{}},
		{http.MethodPost, "/api/admin/capability-market/acquisition-requests/request-1/reject", map[string]any{}},
		{http.MethodPost, "/api/admin/capability-market/acquisition-requests/request-1/complete", map[string]any{}},
		{http.MethodGet, "/api/admin/capability-market/managed-deployments", nil},
		{http.MethodPost, "/api/admin/capability-market/managed-deployments", map[string]any{}},
		{http.MethodDelete, "/api/admin/capability-market/managed-deployments/deploy-1", nil},
		{http.MethodGet, "/api/admin/capability-market/recommendations", nil},
		{http.MethodPost, "/api/admin/capability-market/recommendations", map[string]any{}},
		{http.MethodDelete, "/api/admin/capability-market/recommendations/reco-1", nil},
		{http.MethodGet, "/api/admin/capability-market/groups/group-1/effective-policies", nil},
		{http.MethodGet, "/api/admin/capability-market/users/user@example.com/inventory", nil},
		{http.MethodGet, "/api/admin/capability-market/users/user@example.com/effective-policies", nil},
		{http.MethodGet, "/api/admin/capability-market/users/user@example.com/compliance", nil},
		{http.MethodPost, "/api/admin/capability-market/mcp", map[string]any{}},
		{http.MethodPut, "/api/admin/capability-market/mcp", map[string]any{}},
		{http.MethodPost, "/api/admin/capability-market/mcp/test", map[string]any{}},
		{http.MethodPost, "/api/admin/capability-market/mcp-secret-requirements", map[string]any{}},
		{http.MethodGet, "/api/admin/capabilities/external-search", nil},
		{http.MethodPost, "/api/admin/capabilities/mcp/validate", map[string]any{}},
		{http.MethodPost, "/api/admin/capabilities/import-intent", map[string]any{}},
		{http.MethodGet, "/api/admin/security/groups", nil},
		{http.MethodGet, "/api/admin/security/groups/root", nil},
		{http.MethodPost, "/api/admin/security/groups", map[string]any{}},
		{http.MethodPut, "/api/admin/security/groups/group-1", map[string]any{}},
		{http.MethodDelete, "/api/admin/security/groups/group-1", nil},
		{http.MethodGet, "/api/admin/security/groups/group-1/members", nil},
		{http.MethodPost, "/api/admin/security/groups/group-1/members", map[string]any{}},
		{http.MethodDelete, "/api/admin/security/groups/group-1/members/user@example.com", nil},
		{http.MethodGet, "/api/admin/security/groups/group-1/policy", nil},
		{http.MethodPut, "/api/admin/security/groups/group-1/policy", map[string]any{}},
		{http.MethodGet, "/api/admin/security/users/user@example.com/effective-policy", nil},
		{http.MethodGet, "/api/admin/security/settings", nil},
		{http.MethodPut, "/api/admin/security/settings", map[string]any{}},
		{http.MethodPut, "/api/admin/security/settings/default-group", map[string]any{}},
		{http.MethodGet, "/api/v1/admin/reviews", nil},
		{http.MethodGet, "/api/v1/admin/reviews/version-1", nil},
		{http.MethodPost, "/api/v1/admin/reviews/version-1/approve", map[string]any{}},
		{http.MethodPost, "/api/v1/admin/reviews/version-1/reject", map[string]any{}},
		{http.MethodPost, "/api/v1/admin/reviews/version-1/unpublish", map[string]any{}},
		{http.MethodGet, "/api/admin/a2a/group-discussions", nil},
		{http.MethodGet, "/api/ve/list", nil},
		{http.MethodGet, "/api/ve/ve-1/history", nil},
		{http.MethodGet, "/api/ve/history/search?q=user%40example.com", nil},
		{http.MethodGet, "/api/ve/history/discussion-1/detail", nil},
		{http.MethodGet, "/api/ve/config", nil},
		{http.MethodPut, "/api/ve/config", map[string]any{"max_group_participants": 3}},
		{http.MethodPost, "/api/ve/ve-1/approve", nil},
		{http.MethodPost, "/api/ve/ve-1/reject", nil},
		{http.MethodPost, "/api/ve/ve-1/disable", nil},
		{http.MethodPut, "/api/ve/ve-1/visibility", map[string]any{"visible_group_ids": []string{"dept-1"}}},
		{http.MethodGet, "/api/admin/feishu/bindings", nil},
		{http.MethodDelete, "/api/admin/feishu/bindings?user_id=u1", nil},
		{http.MethodGet, "/api/admin/feishu/auto-enroll", nil},
		{http.MethodPost, "/api/admin/feishu/auto-enroll", map[string]any{"enabled": true}},
		{http.MethodPost, "/api/admin/settings/openclaw_im/test", map[string]any{}},
		{http.MethodGet, "/api/admin/bridge/status", nil},
		{http.MethodGet, "/api/admin/qqbot/bindings", nil},
		{http.MethodDelete, "/api/admin/qqbot/bindings?user_id=u1", nil},
		{http.MethodGet, "/api/admin/wecom/bindings", nil},
		{http.MethodDelete, "/api/admin/wecom/bindings?user_id=u1", nil},
		{http.MethodGet, "/api/admin/dingtalk/bindings", nil},
		{http.MethodDelete, "/api/admin/dingtalk/bindings?user_id=u1", nil},
	} {
		assertGlobalForbidden(endpoint.method, endpoint.target, endpoint.body)
	}

	tenantSecurity := doHubAdminJSONRequest(t, ctx.handler, http.MethodGet, "/api/admin/security/settings", nil, loginPayload.AccessToken)
	if tenantSecurity.Code != http.StatusOK {
		t.Fatalf("tenant security settings status = %d body=%s", tenantSecurity.Code, tenantSecurity.Body.String())
	}
	tenantReviews := doHubAdminJSONRequest(t, ctx.handler, http.MethodGet, "/api/v1/admin/reviews", nil, loginPayload.AccessToken)
	if tenantReviews.Code != http.StatusOK {
		t.Fatalf("tenant workflow reviews status = %d body=%s", tenantReviews.Code, tenantReviews.Body.String())
	}
	tenantVEConfig := doHubAdminJSONRequest(t, ctx.handler, http.MethodGet, "/api/ve/config", nil, loginPayload.AccessToken)
	if tenantVEConfig.Code != http.StatusOK {
		t.Fatalf("tenant VE config status = %d body=%s", tenantVEConfig.Code, tenantVEConfig.Body.String())
	}

	tenantPolicy := doHubAdminJSONRequest(t, ctx.handler, http.MethodPut, "/api/admin/capability-market/policy", map[string]any{"policy": map[string]any{"enterprise_only_search": true, "view_mode": "enterprise_only"}}, loginPayload.AccessToken)
	if tenantPolicy.Code != http.StatusOK {
		t.Fatalf("tenant capability market policy status = %d body=%s", tenantPolicy.Code, tenantPolicy.Body.String())
	}
	tenantPolicyGet := doHubAdminJSONRequest(t, ctx.handler, http.MethodGet, "/api/admin/capability-market/policy", nil, loginPayload.AccessToken)
	if !bytes.Contains(tenantPolicyGet.Body.Bytes(), []byte(`"enterprise_only_search":true`)) || !bytes.Contains(tenantPolicyGet.Body.Bytes(), []byte(`"view_mode":"enterprise_only"`)) {
		t.Fatalf("tenant capability market policy = %s", tenantPolicyGet.Body.String())
	}
	assertTenantSettingOnly(t, ctx.store.System, "tenant_acme", capabilityMarketPolicySettingKey, `"view_mode":"enterprise_only"`)

	assertGlobalForbidden(http.MethodPut, "/api/admin/content_audit/config", map[string]any{"program_path": "global-audit", "timeout_seconds": 3, "timeout_policy": "pass"})
	tenantAudit := doHubAdminJSONRequest(t, ctx.handler, http.MethodPut, "/api/admin/content_audit/config", map[string]any{"program_path": "tenant-audit", "timeout_seconds": 5, "timeout_policy": "block"}, loginPayload.AccessToken)
	if tenantAudit.Code != http.StatusOK {
		t.Fatalf("tenant content audit status = %d body=%s", tenantAudit.Code, tenantAudit.Body.String())
	}
	assertGlobalForbidden(http.MethodGet, "/api/admin/content_audit/config", nil)
	tenantAuditGet := doHubAdminJSONRequest(t, ctx.handler, http.MethodGet, "/api/admin/content_audit/config", nil, loginPayload.AccessToken)
	if !bytes.Contains(tenantAuditGet.Body.Bytes(), []byte(`"program_path":"tenant-audit"`)) {
		t.Fatalf("tenant content audit = %s", tenantAuditGet.Body.String())
	}

	assertGlobalForbidden(http.MethodPost, "/api/admin/bridge/channels", map[string]any{"id": "telegram", "enabled": false, "fields": map[string]string{"botToken": "global-token"}})
	tenantBridge := doHubAdminJSONRequest(t, ctx.handler, http.MethodPost, "/api/admin/bridge/channels", map[string]any{"id": "telegram", "enabled": false, "fields": map[string]string{"botToken": "tenant-token"}}, loginPayload.AccessToken)
	if tenantBridge.Code != http.StatusOK {
		t.Fatalf("tenant bridge status = %d body=%s", tenantBridge.Code, tenantBridge.Body.String())
	}
	assertGlobalForbidden(http.MethodGet, "/api/admin/bridge/channels", nil)
	tenantBridgeGet := doHubAdminJSONRequest(t, ctx.handler, http.MethodGet, "/api/admin/bridge/channels", nil, loginPayload.AccessToken)
	if !bytes.Contains(tenantBridgeGet.Body.Bytes(), []byte(`"botToken":"tenant-token"`)) {
		t.Fatalf("tenant bridge channels = %s", tenantBridgeGet.Body.String())
	}

	assertGlobalForbidden(http.MethodPost, "/api/admin/feishu/config", map[string]any{"enabled": true, "app_id": "global-feishu", "app_secret": "global-secret"})
	tenantFeishu := doHubAdminJSONRequest(t, ctx.handler, http.MethodPost, "/api/admin/feishu/config", map[string]any{"enabled": true, "app_id": "tenant-feishu", "app_secret": "tenant-secret"}, loginPayload.AccessToken)
	if tenantFeishu.Code != http.StatusOK {
		t.Fatalf("tenant feishu status = %d body=%s", tenantFeishu.Code, tenantFeishu.Body.String())
	}
	assertGlobalForbidden(http.MethodGet, "/api/admin/feishu/config", nil)
	tenantFeishuGet := doHubAdminJSONRequest(t, ctx.handler, http.MethodGet, "/api/admin/feishu/config", nil, loginPayload.AccessToken)
	if !bytes.Contains(tenantFeishuGet.Body.Bytes(), []byte(`"app_id":"tenant-feishu"`)) {
		t.Fatalf("tenant feishu config = %s", tenantFeishuGet.Body.String())
	}

	assertGlobalForbidden(http.MethodPost, "/api/admin/settings/openclaw_im", map[string]any{"enabled": true, "webhook_url": "http://127.0.0.1:3210/global", "secret": "global-openclaw"})
	tenantOpenclawIM := doHubAdminJSONRequest(t, ctx.handler, http.MethodPost, "/api/admin/settings/openclaw_im", map[string]any{"enabled": true, "webhook_url": "http://127.0.0.1:3210/tenant", "secret": "tenant-openclaw"}, loginPayload.AccessToken)
	if tenantOpenclawIM.Code != http.StatusOK {
		t.Fatalf("tenant openclaw im status = %d body=%s", tenantOpenclawIM.Code, tenantOpenclawIM.Body.String())
	}
	assertGlobalForbidden(http.MethodGet, "/api/admin/settings/openclaw_im", nil)
	tenantOpenclawIMGet := doHubAdminJSONRequest(t, ctx.handler, http.MethodGet, "/api/admin/settings/openclaw_im", nil, loginPayload.AccessToken)
	if !bytes.Contains(tenantOpenclawIMGet.Body.Bytes(), []byte(`"webhook_url":"http://127.0.0.1:3210/tenant"`)) {
		t.Fatalf("tenant openclaw im config = %s", tenantOpenclawIMGet.Body.String())
	}

	assertGlobalForbidden(http.MethodPost, "/api/admin/settings/qqbot", map[string]any{"enabled": true, "app_id": "global-qq", "app_secret": "global-secret"})
	tenantQQBot := doHubAdminJSONRequest(t, ctx.handler, http.MethodPost, "/api/admin/settings/qqbot", map[string]any{"enabled": true, "app_id": "tenant-qq", "app_secret": "tenant-secret"}, loginPayload.AccessToken)
	if tenantQQBot.Code != http.StatusOK {
		t.Fatalf("tenant qqbot status = %d body=%s", tenantQQBot.Code, tenantQQBot.Body.String())
	}
	assertGlobalForbidden(http.MethodGet, "/api/admin/settings/qqbot", nil)
	tenantQQBotGet := doHubAdminJSONRequest(t, ctx.handler, http.MethodGet, "/api/admin/settings/qqbot", nil, loginPayload.AccessToken)
	if !bytes.Contains(tenantQQBotGet.Body.Bytes(), []byte(`"app_id":"tenant-qq"`)) {
		t.Fatalf("tenant qqbot config = %s", tenantQQBotGet.Body.String())
	}
	assertTenantSettingOnly(t, ctx.store.System, "tenant_acme", qqbotConfigKey, `"app_id":"tenant-qq"`)

	assertGlobalForbidden(http.MethodPost, "/api/admin/settings/wecom", map[string]any{"enabled": true, "bot_id": "global-wecom", "secret": "global-secret"})
	tenantWeCom := doHubAdminJSONRequest(t, ctx.handler, http.MethodPost, "/api/admin/settings/wecom", map[string]any{"enabled": true, "bot_id": "tenant-wecom", "secret": "tenant-secret"}, loginPayload.AccessToken)
	if tenantWeCom.Code != http.StatusOK {
		t.Fatalf("tenant wecom status = %d body=%s", tenantWeCom.Code, tenantWeCom.Body.String())
	}
	assertGlobalForbidden(http.MethodGet, "/api/admin/settings/wecom", nil)
	tenantWeComGet := doHubAdminJSONRequest(t, ctx.handler, http.MethodGet, "/api/admin/settings/wecom", nil, loginPayload.AccessToken)
	if !bytes.Contains(tenantWeComGet.Body.Bytes(), []byte(`"bot_id":"tenant-wecom"`)) {
		t.Fatalf("tenant wecom config = %s", tenantWeComGet.Body.String())
	}
	assertTenantSettingOnly(t, ctx.store.System, "tenant_acme", wecomConfigKey, `"bot_id":"tenant-wecom"`)

	assertGlobalForbidden(http.MethodPost, "/api/admin/settings/dingtalk", map[string]any{"enabled": true, "client_id": "global-dingtalk", "client_secret": "global-secret"})
	tenantDingTalk := doHubAdminJSONRequest(t, ctx.handler, http.MethodPost, "/api/admin/settings/dingtalk", map[string]any{"enabled": true, "client_id": "tenant-dingtalk", "client_secret": "tenant-secret"}, loginPayload.AccessToken)
	if tenantDingTalk.Code != http.StatusOK {
		t.Fatalf("tenant dingtalk status = %d body=%s", tenantDingTalk.Code, tenantDingTalk.Body.String())
	}
	assertGlobalForbidden(http.MethodGet, "/api/admin/settings/dingtalk", nil)
	tenantDingTalkGet := doHubAdminJSONRequest(t, ctx.handler, http.MethodGet, "/api/admin/settings/dingtalk", nil, loginPayload.AccessToken)
	if !bytes.Contains(tenantDingTalkGet.Body.Bytes(), []byte(`"client_id":"tenant-dingtalk"`)) {
		t.Fatalf("tenant dingtalk config = %s", tenantDingTalkGet.Body.String())
	}
	assertTenantSettingOnly(t, ctx.store.System, "tenant_acme", dingtalkConfigKey, `"client_id":"tenant-dingtalk"`)

	globalInviteRequired := doHubAdminJSONRequest(t, ctx.handler, http.MethodPost, "/api/admin/invitation-codes/toggle", map[string]any{"required": false}, globalToken)
	if globalInviteRequired.Code != http.StatusOK {
		t.Fatalf("global invitation toggle status = %d body=%s", globalInviteRequired.Code, globalInviteRequired.Body.String())
	}
	tenantInviteRequired := doHubAdminJSONRequest(t, ctx.handler, http.MethodPost, "/api/admin/invitation-codes/toggle", map[string]any{"required": true}, loginPayload.AccessToken)
	if tenantInviteRequired.Code != http.StatusOK {
		t.Fatalf("tenant invitation toggle status = %d body=%s", tenantInviteRequired.Code, tenantInviteRequired.Body.String())
	}
	globalInviteStatus := doHubAdminJSONRequest(t, ctx.handler, http.MethodGet, "/api/admin/invitation-codes/status", nil, globalToken)
	tenantInviteStatus := doHubAdminJSONRequest(t, ctx.handler, http.MethodGet, "/api/admin/invitation-codes/status", nil, loginPayload.AccessToken)
	if !bytes.Contains(globalInviteStatus.Body.Bytes(), []byte(`"invitation_code_required":false`)) || bytes.Contains(globalInviteStatus.Body.Bytes(), []byte(`"tenant_id":"tenant_acme"`)) {
		t.Fatalf("global invitation setting leaked tenant update: %s", globalInviteStatus.Body.String())
	}
	if !bytes.Contains(tenantInviteStatus.Body.Bytes(), []byte(`"tenant_id":"tenant_acme"`)) || !bytes.Contains(tenantInviteStatus.Body.Bytes(), []byte(`"invitation_code_required":true`)) {
		t.Fatalf("tenant invitation setting = %s", tenantInviteStatus.Body.String())
	}
}

func TestTenantAdminInvitationCodeUnbindIsTenantScoped(t *testing.T) {
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
		"tenant":   "tenant_acme",
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

	decodeGeneratedCode := func(resp *httptest.ResponseRecorder) invitationCodeResponse {
		t.Helper()
		var payload struct {
			Codes []invitationCodeResponse `json:"codes"`
		}
		if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode generated code: %v body=%s", err, resp.Body.String())
		}
		if len(payload.Codes) != 1 {
			t.Fatalf("expected one generated code, got %#v", payload.Codes)
		}
		return payload.Codes[0]
	}

	globalGen := doHubAdminJSONRequest(t, ctx.handler, http.MethodPost, "/api/admin/invitation-codes/generate", map[string]any{"count": 1, "validity_days": 7}, globalToken)
	if globalGen.Code != http.StatusOK {
		t.Fatalf("global generate status = %d body=%s", globalGen.Code, globalGen.Body.String())
	}
	globalCode := decodeGeneratedCode(globalGen)

	tenantGen := doHubAdminJSONRequest(t, ctx.handler, http.MethodPost, "/api/admin/invitation-codes/generate", map[string]any{"count": 1, "validity_days": 7}, loginPayload.AccessToken)
	if tenantGen.Code != http.StatusOK {
		t.Fatalf("tenant generate status = %d body=%s", tenantGen.Code, tenantGen.Body.String())
	}
	tenantCode := decodeGeneratedCode(tenantGen)
	if tenantCode.TenantID != "tenant_acme" {
		t.Fatalf("tenant generated code tenant_id = %q", tenantCode.TenantID)
	}

	deleteGlobalAsTenant := doHubAdminJSONRequest(t, ctx.handler, http.MethodPost, "/api/admin/invitation-codes/unbind", map[string]any{"id": globalCode.ID}, loginPayload.AccessToken)
	if deleteGlobalAsTenant.Code != http.StatusNotFound {
		t.Fatalf("tenant unbind global code status = %d body=%s", deleteGlobalAsTenant.Code, deleteGlobalAsTenant.Body.String())
	}
	if found, err := ctx.store.InvitationCodes.GetByID(context.Background(), globalCode.ID); err != nil || found == nil {
		t.Fatalf("global code should remain after tenant unbind attempt, found=%#v err=%v", found, err)
	}

	deleteTenantAsTenant := doHubAdminJSONRequest(t, ctx.handler, http.MethodPost, "/api/admin/invitation-codes/unbind", map[string]any{"id": tenantCode.ID}, loginPayload.AccessToken)
	if deleteTenantAsTenant.Code != http.StatusOK {
		t.Fatalf("tenant unbind own code status = %d body=%s", deleteTenantAsTenant.Code, deleteTenantAsTenant.Body.String())
	}
	if found, err := ctx.store.InvitationCodes.GetByID(context.Background(), tenantCode.ID); err != nil || found != nil {
		t.Fatalf("tenant code should be deleted after own unbind, found=%#v err=%v", found, err)
	}
}

func assertTenantSettingOnly(t *testing.T, system store.SystemSettingsRepository, tenantID, key, wantFragment string) {
	t.Helper()
	tenantRaw, err := system.Get(context.Background(), "tenant:"+tenantID+":"+key)
	if err != nil {
		t.Fatalf("get tenant setting %s/%s: %v", tenantID, key, err)
	}
	if !strings.Contains(tenantRaw, wantFragment) {
		t.Fatalf("tenant setting %s/%s = %s, want fragment %s", tenantID, key, tenantRaw, wantFragment)
	}
	globalRaw, err := system.Get(context.Background(), key)
	if err == nil && strings.Contains(globalRaw, wantFragment) {
		t.Fatalf("tenant setting leaked into global %s: %s", key, globalRaw)
	}
}
