package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/auth"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/entry"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/hubs"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/mail"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store/sqlite"
)

type hubCenterHTTPTestServices struct {
	store   *store.Store
	admins  *auth.AdminService
	hubs    *hubs.Service
	entry   *entry.Service
	handler http.Handler
	mailer  *httpTestMailer
}

type httpTestMailer struct {
	lastConfirmURL string
}

func (m *httpTestMailer) Send(ctx context.Context, to []string, subject string, body string) error {
	return nil
}

func (m *httpTestMailer) SendHubRegistrationConfirmation(ctx context.Context, to string, confirmURL string, hubName string) error {
	m.lastConfirmURL = confirmURL
	return nil
}

var _ mail.Mailer = (*httpTestMailer)(nil)

func newHubCenterHTTPTestServices(t *testing.T) *hubCenterHTTPTestServices {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "hubcenter-http-test.db")
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
	adminService := auth.NewAdminService(st.Admins, st.System, st.AdminAudit)
	mailer := &httpTestMailer{}
	hubService := hubs.NewService(st.Hubs, st.HubUserLinks, st.BlockedEmails, st.BlockedIPs, st.System, mailer, "http://127.0.0.1:9388")
	entryService := entry.NewService(st.Hubs, st.HubUserLinks, st.BlockedEmails, st.BlockedIPs)

	return &hubCenterHTTPTestServices{
		store:   st,
		admins:  adminService,
		hubs:    hubService,
		entry:   entryService,
		handler: NewRouter(adminService, hubService, entryService, nil),
		mailer:  mailer,
	}
}

func doJSONRequest(t *testing.T, handler http.Handler, method, target string, body any, token string) *httptest.ResponseRecorder {
	t.Helper()

	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(data)
	}

	req := httptest.NewRequest(method, target, reader)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func issueAdminToken(t *testing.T, svc *hubCenterHTTPTestServices) string {
	t.Helper()

	setupResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/setup", map[string]any{
		"username": "admin",
		"password": "StrongPassword123!",
		"email":    "admin@example.com",
	}, "")
	if setupResp.Code != http.StatusOK {
		t.Fatalf("setup status = %d, body = %s", setupResp.Code, setupResp.Body.String())
	}

	loginResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/login", map[string]any{
		"username": "admin",
		"password": "StrongPassword123!",
	}, "")
	if loginResp.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", loginResp.Code, loginResp.Body.String())
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

func TestAdminSetupAndLoginHandlers(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	_ = issueAdminToken(t, svc)
}

func TestAdminSetupRequiresEmail(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)

	resp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/setup", map[string]any{
		"username": "admin",
		"password": "StrongPassword123!",
	}, "")
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestAdminChangePasswordHandler(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	token := issueAdminToken(t, svc)

	resp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/password", map[string]any{
		"current_password": "StrongPassword123!",
		"new_password":     "NewStrongPassword123!",
	}, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", resp.Code, resp.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	newToken, _ := payload["access_token"].(string)
	if newToken == "" {
		t.Fatalf("expected refreshed token, got %v", payload)
	}

	if _, err := svc.admins.Authenticate(context.Background(), token); err == nil {
		t.Fatalf("expected old token to be invalid after password change")
	}
	if _, err := svc.admins.Authenticate(context.Background(), newToken); err != nil {
		t.Fatalf("expected new token to authenticate: %v", err)
	}

	loginResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/login", map[string]any{
		"username": "admin",
		"password": "NewStrongPassword123!",
	}, "")
	if loginResp.Code != http.StatusOK {
		t.Fatalf("expected new password login to succeed, got %d body=%s", loginResp.Code, loginResp.Body.String())
	}
}

func TestAdminStatusHandlerReflectsInitialization(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)

	resp := doJSONRequest(t, svc.handler, http.MethodGet, "/api/admin/status", nil, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200 before setup, got %d body=%s", resp.Code, resp.Body.String())
	}
	if !bytes.Contains(resp.Body.Bytes(), []byte(`"initialized":false`)) {
		t.Fatalf("expected uninitialized response, got %s", resp.Body.String())
	}

	_ = issueAdminToken(t, svc)

	resp = doJSONRequest(t, svc.handler, http.MethodGet, "/api/admin/status", nil, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200 after setup, got %d body=%s", resp.Code, resp.Body.String())
	}
	if !bytes.Contains(resp.Body.Bytes(), []byte(`"initialized":true`)) {
		t.Fatalf("expected initialized response, got %s", resp.Body.String())
	}
}

func TestRegisterHeartbeatAndResolveHandlers(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)

	registerResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/hubs/register", map[string]any{
		"owner_email":     "owner@example.com",
		"name":            "CodeClaw Team Hub",
		"description":     "Team remote coding hub",
		"base_url":        "https://teamhub.example.com",
		"visibility":      "shared",
		"enrollment_mode": "approval",
		"capabilities": map[string]any{
			"supports_remote_control": true,
		},
	}, "")
	if registerResp.Code != http.StatusOK {
		t.Fatalf("register status = %d, body = %s", registerResp.Code, registerResp.Body.String())
	}

	var registerResult map[string]any
	if err := json.Unmarshal(registerResp.Body.Bytes(), &registerResult); err != nil {
		t.Fatalf("decode register response: %v", err)
	}
	hubID, _ := registerResult["hub_id"].(string)
	hubSecret, _ := registerResult["hub_secret"].(string)
	if hubID == "" || hubSecret == "" {
		t.Fatalf("unexpected register result: %+v", registerResult)
	}

	token := svc.mailer.lastConfirmURL[len("http://127.0.0.1:9388/hub-registration/confirm?token="):]
	confirmReq := httptest.NewRequest(http.MethodGet, "/hub-registration/confirm?token="+token, nil)
	confirmResp := httptest.NewRecorder()
	svc.handler.ServeHTTP(confirmResp, confirmReq)
	if confirmResp.Code != http.StatusOK {
		t.Fatalf("confirm status = %d, body = %s", confirmResp.Code, confirmResp.Body.String())
	}

	heartbeatResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/hubs/"+hubID+"/heartbeat", map[string]any{
		"hub_secret": hubSecret,
	}, "")
	if heartbeatResp.Code != http.StatusOK {
		t.Fatalf("heartbeat status = %d, body = %s", heartbeatResp.Code, heartbeatResp.Body.String())
	}

	resolveResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/entry/resolve", map[string]any{
		"email": "owner@example.com",
	}, "")
	if resolveResp.Code != http.StatusOK {
		t.Fatalf("resolve status = %d, body = %s", resolveResp.Code, resolveResp.Body.String())
	}

	var resolveResult entry.ResolveResult
	if err := json.Unmarshal(resolveResp.Body.Bytes(), &resolveResult); err != nil {
		t.Fatalf("decode resolve response: %v", err)
	}
	if resolveResult.Mode != "single" {
		t.Fatalf("expected single mode, got %+v", resolveResult)
	}
	if resolveResult.DefaultPWA == "" {
		t.Fatalf("expected default pwa url, got %+v", resolveResult)
	}
}

func TestAdminServerConfigUpdatesConfirmationBaseURL(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	token := issueAdminToken(t, svc)

	resp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/server/config", map[string]any{
		"public_base_url": "https://center.example.com",
	}, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", resp.Code, resp.Body.String())
	}

	registerResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/hubs/register", map[string]any{
		"owner_email":     "owner@example.com",
		"name":            "CodeClaw Team Hub",
		"base_url":        "https://teamhub.example.com",
		"visibility":      "shared",
		"enrollment_mode": "approval",
	}, "")
	if registerResp.Code != http.StatusOK {
		t.Fatalf("register status = %d, body = %s", registerResp.Code, registerResp.Body.String())
	}

	if !strings.HasPrefix(svc.mailer.lastConfirmURL, "https://center.example.com/hub-registration/confirm?token=") {
		t.Fatalf("expected confirm url to use configured public base url, got %s", svc.mailer.lastConfirmURL)
	}
}

func TestConfirmHubHandlerManuallyActivatesPendingHub(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	token := issueAdminToken(t, svc)

	registerResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/hubs/register", map[string]any{
		"owner_email":     "owner@example.com",
		"name":            "Pending Hub",
		"base_url":        "https://teamhub.example.com",
		"host":            "teamhub.example.com",
		"port":            9399,
		"visibility":      "shared",
		"enrollment_mode": "approval",
	}, "")
	if registerResp.Code != http.StatusOK {
		t.Fatalf("register status = %d, body = %s", registerResp.Code, registerResp.Body.String())
	}

	var registerResult map[string]any
	if err := json.Unmarshal(registerResp.Body.Bytes(), &registerResult); err != nil {
		t.Fatalf("decode register response: %v", err)
	}
	hubID, _ := registerResult["hub_id"].(string)
	if hubID == "" {
		t.Fatalf("expected hub id, got %+v", registerResult)
	}

	resp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/hubs/"+hubID+"/confirm", map[string]any{}, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", resp.Code, resp.Body.String())
	}

	hub, err := svc.store.Hubs.GetByID(context.Background(), hubID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if hub == nil || hub.Status != "online" {
		t.Fatalf("expected hub to be online after manual confirm, got %+v", hub)
	}
}

func TestRegisterHubHandlerRejectsBlockedOwnerEmail(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	token := issueAdminToken(t, svc)

	blockResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/blocked-emails", map[string]any{
		"email":  "owner@example.com",
		"reason": "abuse",
	}, token)
	if blockResp.Code != http.StatusOK {
		t.Fatalf("block email status = %d, body = %s", blockResp.Code, blockResp.Body.String())
	}

	resp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/hubs/register", map[string]any{
		"owner_email":     "owner@example.com",
		"name":            "Blocked Hub",
		"base_url":        "https://blocked.example.com",
		"visibility":      "private",
		"enrollment_mode": "open",
	}, "")
	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for blocked owner email, got %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestRegisterHubHandlerRejectsBlockedIP(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	token := issueAdminToken(t, svc)

	blockResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/blocked-ips", map[string]any{
		"ip":     "10.0.0.7",
		"reason": "scanner",
	}, token)
	if blockResp.Code != http.StatusOK {
		t.Fatalf("block ip status = %d, body = %s", blockResp.Code, blockResp.Body.String())
	}

	reqBody := map[string]any{
		"owner_email":     "owner@example.com",
		"name":            "Blocked IP Hub",
		"base_url":        "https://blocked-ip.example.com",
		"visibility":      "private",
		"enrollment_mode": "open",
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/hubs/register", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "10.0.0.7")
	rr := httptest.NewRecorder()
	svc.handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for blocked ip, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestEntryResolveHandlerRejectsBlockedIP(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	token := issueAdminToken(t, svc)

	blockResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/blocked-ips", map[string]any{
		"ip":     "10.0.0.8",
		"reason": "scanner",
	}, token)
	if blockResp.Code != http.StatusOK {
		t.Fatalf("block ip status = %d, body = %s", blockResp.Code, blockResp.Body.String())
	}

	reqBody := map[string]any{"email": "user@example.com"}
	data, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/entry/resolve", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Real-IP", "10.0.0.8")
	rr := httptest.NewRecorder()
	svc.handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for blocked ip, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestManagementHandlersRequireAdminToken(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)

	resp := doJSONRequest(t, svc.handler, http.MethodGet, "/api/admin/hubs", nil, "")
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without admin token, got %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestManagementHandlersBlockEmailAndDisableHub(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	token := issueAdminToken(t, svc)

	registerResult, err := svc.hubs.RegisterHub(context.Background(), hubs.RegisterHubRequest{
		OwnerEmail:     "owner@example.com",
		Name:           "Personal Hub",
		Description:    "Personal remote hub",
		BaseURL:        "https://personal.example.com",
		Visibility:     "private",
		EnrollmentMode: "open",
	})
	if err != nil {
		t.Fatalf("register hub: %v", err)
	}

	blockResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/blocked-emails", map[string]any{
		"email":  "blocked@example.com",
		"reason": "abuse",
	}, token)
	if blockResp.Code != http.StatusOK {
		t.Fatalf("block email status = %d, body = %s", blockResp.Code, blockResp.Body.String())
	}

	listBlockedResp := doJSONRequest(t, svc.handler, http.MethodGet, "/api/admin/blocked-emails", nil, token)
	if listBlockedResp.Code != http.StatusOK {
		t.Fatalf("list blocked emails status = %d, body = %s", listBlockedResp.Code, listBlockedResp.Body.String())
	}
	if !bytes.Contains(listBlockedResp.Body.Bytes(), []byte("blocked@example.com")) {
		t.Fatalf("expected blocked email in list, body=%s", listBlockedResp.Body.String())
	}

	removeBlockedResp := doJSONRequest(t, svc.handler, http.MethodDelete, "/api/admin/blocked-emails/blocked@example.com", nil, token)
	if removeBlockedResp.Code != http.StatusOK {
		t.Fatalf("remove blocked email status = %d, body = %s", removeBlockedResp.Code, removeBlockedResp.Body.String())
	}

	listBlockedResp = doJSONRequest(t, svc.handler, http.MethodGet, "/api/admin/blocked-emails", nil, token)
	if listBlockedResp.Code != http.StatusOK {
		t.Fatalf("list blocked emails status = %d, body = %s", listBlockedResp.Code, listBlockedResp.Body.String())
	}
	if bytes.Contains(listBlockedResp.Body.Bytes(), []byte("blocked@example.com")) {
		t.Fatalf("expected blocked email to be removed, body=%s", listBlockedResp.Body.String())
	}

	blockIPResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/blocked-ips", map[string]any{
		"ip":     "10.0.0.7",
		"reason": "scanner",
	}, token)
	if blockIPResp.Code != http.StatusOK {
		t.Fatalf("block ip status = %d, body = %s", blockIPResp.Code, blockIPResp.Body.String())
	}

	removeBlockedIPResp := doJSONRequest(t, svc.handler, http.MethodDelete, "/api/admin/blocked-ips/10.0.0.7", nil, token)
	if removeBlockedIPResp.Code != http.StatusOK {
		t.Fatalf("remove blocked ip status = %d, body = %s", removeBlockedIPResp.Code, removeBlockedIPResp.Body.String())
	}

	disableResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/hubs/"+registerResult.HubID+"/disable", map[string]any{
		"reason": "maintenance",
	}, token)
	if disableResp.Code != http.StatusOK {
		t.Fatalf("disable hub status = %d, body = %s", disableResp.Code, disableResp.Body.String())
	}

	resolveResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/entry/resolve", map[string]any{
		"email": "owner@example.com",
	}, "")
	if resolveResp.Code != http.StatusOK {
		t.Fatalf("resolve status = %d, body = %s", resolveResp.Code, resolveResp.Body.String())
	}

	var resolveResult entry.ResolveResult
	if err := json.Unmarshal(resolveResp.Body.Bytes(), &resolveResult); err != nil {
		t.Fatalf("decode resolve response: %v", err)
	}
	if resolveResult.Mode != "none" {
		t.Fatalf("expected disabled hub to be filtered, got %+v", resolveResult)
	}

	heartbeatResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/hubs/"+registerResult.HubID+"/heartbeat", map[string]any{
		"hub_secret": registerResult.HubSecret,
	}, "")
	if heartbeatResp.Code != http.StatusLocked {
		t.Fatalf("expected disabled hub heartbeat to be locked, got %d body=%s", heartbeatResp.Code, heartbeatResp.Body.String())
	}
	if !bytes.Contains(heartbeatResp.Body.Bytes(), []byte(`"code":"HUB_DISABLED"`)) {
		t.Fatalf("expected HUB_DISABLED, body=%s", heartbeatResp.Body.String())
	}
}

func TestDeleteHubHandlerRemovesHubAndHeartbeatBecomesUnregistered(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	token := issueAdminToken(t, svc)

	registerResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/hubs/register", map[string]any{
		"owner_email":     "owner@example.com",
		"name":            "Delete Hub",
		"description":     "Delete target",
		"base_url":        "https://delete.example.com",
		"host":            "delete.example.com",
		"port":            9399,
		"visibility":      "private",
		"enrollment_mode": "open",
	}, "")
	if registerResp.Code != http.StatusOK {
		t.Fatalf("register status = %d, body = %s", registerResp.Code, registerResp.Body.String())
	}

	var registerResult map[string]any
	if err := json.Unmarshal(registerResp.Body.Bytes(), &registerResult); err != nil {
		t.Fatalf("decode register response: %v", err)
	}
	hubID, _ := registerResult["hub_id"].(string)
	hubSecret, _ := registerResult["hub_secret"].(string)
	if hubID == "" || hubSecret == "" {
		t.Fatalf("unexpected register result: %+v", registerResult)
	}

	deleteResp := doJSONRequest(t, svc.handler, http.MethodDelete, "/api/admin/hubs/"+hubID, nil, token)
	if deleteResp.Code != http.StatusOK {
		t.Fatalf("delete hub status = %d, body = %s", deleteResp.Code, deleteResp.Body.String())
	}

	listResp := doJSONRequest(t, svc.handler, http.MethodGet, "/api/admin/hubs", nil, token)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list hubs status = %d, body = %s", listResp.Code, listResp.Body.String())
	}
	if bytes.Contains(listResp.Body.Bytes(), []byte(hubID)) {
		t.Fatalf("expected deleted hub to disappear from list, body=%s", listResp.Body.String())
	}

	heartbeatResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/hubs/"+hubID+"/heartbeat", map[string]any{
		"hub_secret": hubSecret,
	}, "")
	if heartbeatResp.Code != http.StatusUnauthorized {
		t.Fatalf("expected deleted hub heartbeat to be unauthorized, got %d body=%s", heartbeatResp.Code, heartbeatResp.Body.String())
	}
	if !bytes.Contains(heartbeatResp.Body.Bytes(), []byte(`"code":"HUB_UNREGISTERED"`)) {
		t.Fatalf("expected HUB_UNREGISTERED, body=%s", heartbeatResp.Body.String())
	}
}

func TestUpdateHubVisibilityHandler(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	token := issueAdminToken(t, svc)

	registerResult, err := svc.hubs.RegisterHub(context.Background(), hubs.RegisterHubRequest{
		OwnerEmail:     "owner@example.com",
		Name:           "Visibility Hub",
		BaseURL:        "https://visibility.example.com",
		Visibility:     "private",
		EnrollmentMode: "open",
	})
	if err != nil {
		t.Fatalf("register hub: %v", err)
	}

	resp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/hubs/"+registerResult.HubID+"/visibility", map[string]any{
		"visibility": "shared",
	}, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", resp.Code, resp.Body.String())
	}

	hub, err := svc.store.Hubs.GetByID(context.Background(), registerResult.HubID)
	if err != nil {
		t.Fatalf("get hub: %v", err)
	}
	if hub == nil || hub.Visibility != "shared" {
		t.Fatalf("expected shared visibility, got %+v", hub)
	}
}

func TestListHubsHandlerUsesSnakeCaseFields(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	token := issueAdminToken(t, svc)

	_, err := svc.hubs.RegisterHub(context.Background(), hubs.RegisterHubRequest{
		OwnerEmail:     "owner@example.com",
		Name:           "Personal Hub",
		Description:    "Personal remote hub",
		BaseURL:        "https://personal.example.com",
		Host:           "personal.example.com",
		Port:           9399,
		Visibility:     "private",
		EnrollmentMode: "open",
	})
	if err != nil {
		t.Fatalf("register hub: %v", err)
	}

	resp := doJSONRequest(t, svc.handler, http.MethodGet, "/api/admin/hubs", nil, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("list hubs status = %d, body = %s", resp.Code, resp.Body.String())
	}

	body := resp.Body.String()
	if !bytes.Contains(resp.Body.Bytes(), []byte(`"owner_email":"owner@example.com"`)) {
		t.Fatalf("expected owner_email in response, body=%s", body)
	}
	if !bytes.Contains(resp.Body.Bytes(), []byte(`"base_url":"https://personal.example.com"`)) {
		t.Fatalf("expected base_url in response, body=%s", body)
	}
	if !bytes.Contains(resp.Body.Bytes(), []byte(`"last_seen_at"`)) {
		t.Fatalf("expected last_seen_at in response, body=%s", body)
	}
	if bytes.Contains(resp.Body.Bytes(), []byte(`"OwnerEmail"`)) || bytes.Contains(resp.Body.Bytes(), []byte(`"BaseURL"`)) {
		t.Fatalf("expected snake_case response fields, body=%s", body)
	}
}

func TestAdminStaticRouteServesIndexAndAssets(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("center-admin"), 0644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "admin.js"), []byte("console.log('center');"), 0644); err != nil {
		t.Fatalf("write asset: %v", err)
	}

	mux := http.NewServeMux()
	registerAdminStaticRoutes(mux, dir, "/admin")

	indexReq := httptest.NewRequest(http.MethodGet, "/admin", nil)
	indexRec := httptest.NewRecorder()
	mux.ServeHTTP(indexRec, indexReq)
	if indexRec.Code != http.StatusOK || indexRec.Body.String() != "center-admin" {
		t.Fatalf("unexpected admin index response: %d %q", indexRec.Code, indexRec.Body.String())
	}

	assetReq := httptest.NewRequest(http.MethodGet, "/admin/admin.js", nil)
	assetRec := httptest.NewRecorder()
	mux.ServeHTTP(assetRec, assetReq)
	if assetRec.Code != http.StatusOK || assetRec.Body.String() != "console.log('center');" {
		t.Fatalf("unexpected admin asset response: %d %q", assetRec.Code, assetRec.Body.String())
	}
}

func TestHealthRoute(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	svc.handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}
