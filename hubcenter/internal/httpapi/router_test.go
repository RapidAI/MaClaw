package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/auth"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/entry"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/hubs"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/mail"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/skillmarket"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store/sqlite"
	_ "modernc.org/sqlite"
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
	hubService := hubs.NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs, st.System, mailer, "http://127.0.0.1:9388")
	entryService := entry.NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs)
	hubService.SetInvitationCodeRoutes(st.InvitationCodeRoutes)
	entryService.SetInvitationCodeRoutes(st.InvitationCodeRoutes)

	return &hubCenterHTTPTestServices{
		store:   st,
		admins:  adminService,
		hubs:    hubService,
		entry:   entryService,
		handler: NewRouter(adminService, hubService, entryService, nil, nil, st.FailureLogs, nil, nil, nil, st.System, st.News, nil),
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

func TestRouterDoesNotServePetPackHelp(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	rec := httptest.NewRecorder()
	svc.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/pet-pack-help?lang=zh", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("pet-pack-help status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func responseErrorCode(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	var payload struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error response: %v body=%s", err, rr.Body.String())
	}
	return payload.Code
}

func doJSONRequestWithHost(t *testing.T, handler http.Handler, method, target string, body any, token string, requestURL string) *httptest.ResponseRecorder {
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

	req := httptest.NewRequest(method, requestURL, reader)
	req.URL.Path = target
	req.RequestURI = target
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func TestRouterWriteHandlersRejectOversizedJSON(t *testing.T) {
	largeBody := `{"value":"` + strings.Repeat("x", defaultJSONBodyLimit) + `"}`
	tests := []struct {
		name    string
		target  string
		handler http.HandlerFunc
		pathID  string
	}{
		{name: "register", target: "/api/hubs/register", handler: RegisterHubHandler(nil)},
		{name: "heartbeat", target: "/api/hubs/hub-1/heartbeat", handler: HubHeartbeatHandler(nil), pathID: "hub-1"},
		{name: "sync link", target: "/api/hubs/hub-1/users/sync", handler: HubUserLinkSyncHandler(nil), pathID: "hub-1"},
		{name: "delete link", target: "/api/hubs/hub-1/users/delete", handler: HubUserLinkDeleteHandler(nil), pathID: "hub-1"},
		{name: "entry resolve", target: "/api/entry/resolve", handler: EntryResolveHandler(nil)},
		{name: "entry domain", target: "/api/entry/resolve-domain", handler: EntryResolveDomainHandler(nil)},
		{name: "admin route", target: "/api/admin/routes/query", handler: AdminRouteQueryHandler(nil)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tt.target, strings.NewReader(largeBody))
			if tt.pathID != "" {
				req.SetPathValue("id", tt.pathID)
			}
			rr := httptest.NewRecorder()

			tt.handler(rr, req)

			if rr.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusRequestEntityTooLarge, rr.Body.String())
			}
			if !strings.Contains(rr.Body.String(), "REQUEST_TOO_LARGE") {
				t.Fatalf("body = %s, want REQUEST_TOO_LARGE", rr.Body.String())
			}
		})
	}
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

func registerConfirmAndHeartbeatHub(t *testing.T, svc *hubCenterHTTPTestServices, body map[string]any) map[string]any {
	t.Helper()

	registerResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/hubs/register", body, "")
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

	token := strings.TrimPrefix(svc.mailer.lastConfirmURL, "http://127.0.0.1:9388/hub-registration/confirm?token=")
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

	return registerResult
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

	registerResult := registerConfirmAndHeartbeatHub(t, svc, map[string]any{
		"owner_email":          "owner@example.com",
		"name":                 "MaClaw Team Hub",
		"description":          "Team remote coding hub",
		"base_url":             "https://teamhub.example.com",
		"visibility":           "shared",
		"enrollment_mode":      "approval",
		"accept_public_signup": true,
		"capabilities": map[string]any{
			"supports_remote_control": true,
		},
	})

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
	if resolveResult.DefaultHubID != registerResult["hub_id"] {
		t.Fatalf("expected default hub %v, got %+v", registerResult["hub_id"], resolveResult)
	}
}

func TestEntryResolveHandlerRoutesPhoneNumberFromUserLink(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)

	registerResult := registerConfirmAndHeartbeatHub(t, svc, map[string]any{
		"owner_email":     "owner@example.com",
		"name":            "Phone Hub",
		"base_url":        "https://phonehub.example.com",
		"visibility":      "shared",
		"enrollment_mode": "approval",
	})
	hubID, _ := registerResult["hub_id"].(string)
	hubSecret, _ := registerResult["hub_secret"].(string)
	linkResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/hubs/"+hubID+"/user-links/sync", map[string]any{
		"hub_secret": hubSecret,
		"email":      "phone:19900001111",
		"is_default": true,
	}, "")
	if linkResp.Code != http.StatusOK {
		t.Fatalf("sync phone link status = %d body=%s", linkResp.Code, linkResp.Body.String())
	}

	resolveResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/entry/resolve", map[string]any{
		"phone_number": "199 0000 1111",
	}, "")
	if resolveResp.Code != http.StatusOK {
		t.Fatalf("resolve phone status = %d body = %s", resolveResp.Code, resolveResp.Body.String())
	}
	var resolveResult entry.ResolveResult
	if err := json.Unmarshal(resolveResp.Body.Bytes(), &resolveResult); err != nil {
		t.Fatalf("decode resolve response: %v", err)
	}
	if resolveResult.Mode != "single" || len(resolveResult.Hubs) != 1 || resolveResult.Hubs[0].HubID != hubID || resolveResult.Email != "phone:19900001111" {
		t.Fatalf("phone user link must select its historical registration target: %+v", resolveResult)
	}

	barePhoneResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/entry/resolve", map[string]any{
		"email": "19900001111",
	}, "")
	if barePhoneResp.Code != http.StatusOK {
		t.Fatalf("resolve bare phone status = %d body = %s", barePhoneResp.Code, barePhoneResp.Body.String())
	}
	var barePhoneResult entry.ResolveResult
	if err := json.Unmarshal(barePhoneResp.Body.Bytes(), &barePhoneResult); err != nil {
		t.Fatalf("decode bare phone resolve response: %v", err)
	}
	if barePhoneResult.Mode != "single" || len(barePhoneResult.Hubs) != 1 || barePhoneResult.Hubs[0].HubID != hubID || barePhoneResult.Email != "phone:19900001111" {
		t.Fatalf("bare phone user link must select its historical registration target: %+v", barePhoneResult)
	}
}

func TestEntryResolveHandlerDoesNotPublicFallbackUnknownPhoneNumber(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)

	registerConfirmAndHeartbeatHub(t, svc, map[string]any{
		"owner_email":          "owner@example.com",
		"name":                 "Public Hub",
		"base_url":             "https://public.example.com",
		"visibility":           "shared",
		"enrollment_mode":      "approval",
		"accept_public_signup": true,
	})

	resolveResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/entry/resolve", map[string]any{
		"phone_number": "19900002222",
	}, "")
	if resolveResp.Code != http.StatusOK {
		t.Fatalf("resolve phone status = %d body = %s", resolveResp.Code, resolveResp.Body.String())
	}
	var resolveResult entry.ResolveResult
	if err := json.Unmarshal(resolveResp.Body.Bytes(), &resolveResult); err != nil {
		t.Fatalf("decode resolve response: %v", err)
	}
	if resolveResult.Mode != "none" || resolveResult.DefaultHubID != "" || resolveResult.Email != "phone:19900002222" {
		t.Fatalf("expected unknown phone to avoid public fallback, got %+v", resolveResult)
	}
}

func TestEntryResolveHandlerIgnoresInvalidPhoneNumberWhenEmailProvided(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)

	registerResult := registerConfirmAndHeartbeatHub(t, svc, map[string]any{
		"owner_email":     "owner@example.com",
		"name":            "Email Hub",
		"base_url":        "https://emailhub.example.com",
		"visibility":      "shared",
		"enrollment_mode": "approval",
	})
	hubID, _ := registerResult["hub_id"].(string)
	hubSecret, _ := registerResult["hub_secret"].(string)
	linkResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/hubs/"+hubID+"/user-links/sync", map[string]any{
		"hub_secret": hubSecret,
		"email":      "buyer@example.com",
		"is_default": true,
	}, "")
	if linkResp.Code != http.StatusOK {
		t.Fatalf("sync email link status = %d body=%s", linkResp.Code, linkResp.Body.String())
	}

	resolveResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/entry/resolve", map[string]any{
		"phone_number": "12-3",
		"email":        "buyer@example.com",
	}, "")
	if resolveResp.Code != http.StatusOK {
		t.Fatalf("resolve email status = %d body = %s", resolveResp.Code, resolveResp.Body.String())
	}
	var resolveResult entry.ResolveResult
	if err := json.Unmarshal(resolveResp.Body.Bytes(), &resolveResult); err != nil {
		t.Fatalf("decode resolve response: %v", err)
	}
	if resolveResult.Mode != "single" || resolveResult.DefaultHubID != hubID || resolveResult.Email != "buyer@example.com" {
		t.Fatalf("expected invalid phone to fall back to email, got %+v", resolveResult)
	}
}

func TestEntryResolveHandlerRejectsInvalidPhoneNumberWithoutEmail(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)

	resolveResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/entry/resolve", map[string]any{
		"phone_number": "abc123456",
	}, "")
	if resolveResp.Code != http.StatusBadRequest {
		t.Fatalf("resolve invalid phone status = %d body = %s", resolveResp.Code, resolveResp.Body.String())
	}
	if !strings.Contains(resolveResp.Body.String(), "INVALID_PHONE_NUMBER") {
		t.Fatalf("expected INVALID_PHONE_NUMBER, got %s", resolveResp.Body.String())
	}
}

func TestEntryResolveHandlerDoesNotTreatNumericEmailAsPhone(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)

	registerResult := registerConfirmAndHeartbeatHub(t, svc, map[string]any{
		"owner_email":     "owner@example.com",
		"name":            "Numeric Email Hub",
		"base_url":        "https://numeric-email.example.com",
		"visibility":      "shared",
		"enrollment_mode": "approval",
	})
	hubID, _ := registerResult["hub_id"].(string)
	hubSecret, _ := registerResult["hub_secret"].(string)
	linkResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/hubs/"+hubID+"/user-links/sync", map[string]any{
		"hub_secret": hubSecret,
		"email":      "buyer123456@example.com",
		"is_default": true,
	}, "")
	if linkResp.Code != http.StatusOK {
		t.Fatalf("sync numeric email link status = %d body=%s", linkResp.Code, linkResp.Body.String())
	}

	resolveResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/entry/resolve", map[string]any{
		"email": "buyer123456@example.com",
	}, "")
	if resolveResp.Code != http.StatusOK {
		t.Fatalf("resolve numeric email status = %d body = %s", resolveResp.Code, resolveResp.Body.String())
	}
	var resolveResult entry.ResolveResult
	if err := json.Unmarshal(resolveResp.Body.Bytes(), &resolveResult); err != nil {
		t.Fatalf("decode numeric email resolve response: %v", err)
	}
	if resolveResult.Mode != "single" || resolveResult.DefaultHubID != hubID || resolveResult.Email != "buyer123456@example.com" {
		t.Fatalf("expected numeric email to remain email route, got %+v", resolveResult)
	}
}

func TestNormalizeEntryResolvePhoneIdentityRejectsAlphanumericUserID(t *testing.T) {
	if got := normalizeEntryResolvePhoneIdentity("abc123456"); got != "" {
		t.Fatalf("normalizeEntryResolvePhoneIdentity(alphanumeric) = %q, want empty", got)
	}
	if got := normalizeEntryResolvePhoneIdentity("199 0000-1111"); got != "phone:19900001111" {
		t.Fatalf("normalizeEntryResolvePhoneIdentity(phone) = %q, want phone:19900001111", got)
	}
	if got := normalizeEntryResolvePhoneIdentity("phone:19900001111"); got != "phone:19900001111" {
		t.Fatalf("normalizeEntryResolvePhoneIdentity(phone identity) = %q, want phone:19900001111", got)
	}
}

func TestMobileServiceRedemptionRouteIsNotExposed(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	resp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/mobile/service-redemptions", map[string]any{
		"code": "legacy-mobile-code",
	}, "")
	if resp.Code != http.StatusNotFound {
		t.Fatalf("legacy mobile redemption route status = %d body=%s, want 404", resp.Code, resp.Body.String())
	}
}
func TestMobileDesktopQRSessionRouteIsNotExposed(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	resp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/mobile/llm/desktop-qr-sessions", map[string]any{
		"qr_payload": `{"v":2,"type":"maclaw_mobile_llm_authorization","session_id":"mlqr_test","hub_url":"https://tenant-a.maclaw.top"}`,
	}, "")
	if resp.Code != http.StatusNotFound {
		t.Fatalf("desktop QR mobile-login route status = %d body=%s, want 404", resp.Code, resp.Body.String())
	}
}
func TestSameURLOriginHandlesDefaultPorts(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want bool
	}{
		{
			name: "https default port",
			a:    "https://hub.example.com",
			b:    "https://hub.example.com:443/path",
			want: true,
		},
		{
			name: "http default port",
			a:    "http://127.0.0.1",
			b:    "http://127.0.0.1:80/path",
			want: true,
		},
		{
			name: "different explicit port",
			a:    "https://hub.example.com",
			b:    "https://hub.example.com:8443",
			want: false,
		},
		{
			name: "different scheme",
			a:    "https://hub.example.com",
			b:    "http://hub.example.com:443",
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sameURLOrigin(tt.a, tt.b); got != tt.want {
				t.Fatalf("sameURLOrigin(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestResolveHandlersRouteByCorporateEmailDomain(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)

	exactHub := registerConfirmAndHeartbeatHub(t, svc, map[string]any{
		"owner_email":             "owner-qx@example.com",
		"name":                    "Qianxin Hub",
		"base_url":                "https://qianxin.example.com",
		"visibility":              "shared",
		"enrollment_mode":         "approval",
		"corporate_email_domain":  "rapidai.tech",
		"corporate_email_domains": []string{"rapidai.tech", "subsidiary.example"},
	})

	defaultHub := registerConfirmAndHeartbeatHub(t, svc, map[string]any{
		"owner_email":          "owner-default@example.com",
		"name":                 "Default Hub",
		"base_url":             "https://default.example.com",
		"visibility":           "shared",
		"enrollment_mode":      "approval",
		"accept_public_signup": true,
	})

	resolveExactResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/entry/resolve", map[string]any{
		"email": "user@rapidai.tech",
	}, "")
	if resolveExactResp.Code != http.StatusOK {
		t.Fatalf("resolve exact status = %d, body = %s", resolveExactResp.Code, resolveExactResp.Body.String())
	}

	var exactResult entry.ResolveResult
	if err := json.Unmarshal(resolveExactResp.Body.Bytes(), &exactResult); err != nil {
		t.Fatalf("decode exact resolve response: %v", err)
	}
	if exactResult.DefaultHubID != exactHub["hub_id"] {
		t.Fatalf("expected qianxin hub %v, got %+v", exactHub["hub_id"], exactResult)
	}
	if len(exactResult.Hubs) != 1 || exactResult.Hubs[0].CorporateEmailDomain != "rapidai.tech" {
		t.Fatalf("expected exact corporate route, got %+v", exactResult.Hubs)
	}

	resolveDomainResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/entry/resolve-domain", map[string]any{
		"email": "stale-user@rapidai.tech",
	}, "")
	if resolveDomainResp.Code != http.StatusOK {
		t.Fatalf("resolve domain status = %d, body = %s", resolveDomainResp.Code, resolveDomainResp.Body.String())
	}
	var domainResult entry.ResolveResult
	if err := json.Unmarshal(resolveDomainResp.Body.Bytes(), &domainResult); err != nil {
		t.Fatalf("decode domain resolve response: %v", err)
	}
	if domainResult.DefaultHubID != exactHub["hub_id"] {
		t.Fatalf("expected domain owner hub %v, got %+v", exactHub["hub_id"], domainResult)
	}

	resolveOtherTenantDomainResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/entry/resolve-domain", map[string]any{
		"domain":    "rapidai.tech",
		"tenant_id": "tenant_other",
	}, "")
	if resolveOtherTenantDomainResp.Code != http.StatusOK {
		t.Fatalf("resolve other tenant domain status = %d, body = %s", resolveOtherTenantDomainResp.Code, resolveOtherTenantDomainResp.Body.String())
	}
	domainResult = entry.ResolveResult{}
	if err := json.Unmarshal(resolveOtherTenantDomainResp.Body.Bytes(), &domainResult); err != nil {
		t.Fatalf("decode other tenant domain resolve response: %v", err)
	}
	if domainResult.DefaultHubID != exactHub["hub_id"] {
		t.Fatalf("expected global domain route visible for other tenant, got %+v", domainResult)
	}

	resolveExtraResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/entry/resolve", map[string]any{
		"email": "user@subsidiary.example",
	}, "")
	if resolveExtraResp.Code != http.StatusOK {
		t.Fatalf("resolve extra status = %d, body = %s", resolveExtraResp.Code, resolveExtraResp.Body.String())
	}
	var extraResult entry.ResolveResult
	if err := json.Unmarshal(resolveExtraResp.Body.Bytes(), &extraResult); err != nil {
		t.Fatalf("decode extra resolve response: %v", err)
	}
	if extraResult.DefaultHubID != exactHub["hub_id"] || len(extraResult.Hubs) != 1 || extraResult.Hubs[0].CorporateEmailDomain != "subsidiary.example" {
		t.Fatalf("expected extra corporate route, got %+v", extraResult)
	}

	resolveDefaultResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/entry/resolve", map[string]any{
		"email": "user@other.com",
	}, "")
	if resolveDefaultResp.Code != http.StatusOK {
		t.Fatalf("resolve default status = %d, body = %s", resolveDefaultResp.Code, resolveDefaultResp.Body.String())
	}

	var defaultResult entry.ResolveResult
	if err := json.Unmarshal(resolveDefaultResp.Body.Bytes(), &defaultResult); err != nil {
		t.Fatalf("decode default resolve response: %v", err)
	}
	if defaultResult.DefaultHubID != defaultHub["hub_id"] {
		t.Fatalf("expected catch-all hub %v, got %+v", defaultHub["hub_id"], defaultResult)
	}
	if len(defaultResult.Hubs) != 1 || defaultResult.Hubs[0].CorporateEmailDomain != "" {
		t.Fatalf("expected catch-all route without corporate domain, got %+v", defaultResult.Hubs)
	}
}

func TestResolveDomainPrefersTenantRouteOverGlobalRoute(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	ctx := context.Background()
	now := time.Now()

	globalHub := &store.HubInstance{ID: "hub_global", OwnerEmail: "global@example.com", Name: "A Global", BaseURL: "https://global.example.com", Visibility: "shared", Status: "online", CreatedAt: now, UpdatedAt: now}
	tenantHub := &store.HubInstance{ID: "hub_tenant", OwnerEmail: "tenant@example.com", Name: "Z Tenant", BaseURL: "https://tenant.example.com", Visibility: "shared", Status: "online", CreatedAt: now, UpdatedAt: now}
	for _, hub := range []*store.HubInstance{globalHub, tenantHub} {
		if err := svc.store.Hubs.Create(ctx, hub); err != nil {
			t.Fatalf("create hub %s: %v", hub.ID, err)
		}
	}
	if err := svc.store.HubDomainRoutes.Upsert(ctx, &store.HubDomainRoute{ID: "global-qianxin", HubID: globalHub.ID, Domain: "qianxin.com", Enabled: true, Priority: 0, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("seed global route: %v", err)
	}
	if err := svc.store.HubDomainRoutes.Upsert(ctx, &store.HubDomainRoute{ID: "tenant-qianxin", HubID: tenantHub.ID, TenantID: "tenant_qianxin", Domain: "qianxin.com", Enabled: true, Priority: 100, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("seed tenant route: %v", err)
	}

	resolveTenantResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/entry/resolve-domain", map[string]any{
		"domain":    "qianxin.com",
		"tenant_id": "tenant_qianxin",
	}, "")
	if resolveTenantResp.Code != http.StatusOK {
		t.Fatalf("resolve tenant domain status = %d, body = %s", resolveTenantResp.Code, resolveTenantResp.Body.String())
	}
	var result entry.ResolveResult
	if err := json.Unmarshal(resolveTenantResp.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode tenant domain resolve response: %v", err)
	}
	if result.DefaultHubID != tenantHub.ID || len(result.Hubs) != 1 {
		t.Fatalf("expected tenant route to override global route, got %+v", result)
	}

	resolveOtherResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/entry/resolve-domain", map[string]any{
		"domain":    "qianxin.com",
		"tenant_id": "tenant_other",
	}, "")
	if resolveOtherResp.Code != http.StatusOK {
		t.Fatalf("resolve other domain status = %d, body = %s", resolveOtherResp.Code, resolveOtherResp.Body.String())
	}
	result = entry.ResolveResult{}
	if err := json.Unmarshal(resolveOtherResp.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode other domain resolve response: %v", err)
	}
	if result.DefaultHubID != globalHub.ID || len(result.Hubs) != 1 {
		t.Fatalf("expected other tenant to use global route, got %+v", result)
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
		"name":            "MaClaw Team Hub",
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

func TestDigitalEmployeeAuthorizationAdminRouteAndHeartbeat(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	token := issueAdminToken(t, svc)

	registered := registerConfirmAndHeartbeatHub(t, svc, map[string]any{
		"owner_email":     "owner-ve@example.com",
		"name":            "Digital Employee Hub",
		"base_url":        "https://ve.example.com",
		"visibility":      "shared",
		"enrollment_mode": "approval",
	})
	hubID := registered["hub_id"].(string)
	hubSecret := registered["hub_secret"].(string)

	policyTenantResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/hubs/"+hubID+"/registration-policy", map[string]any{
		"tenant": map[string]any{
			"tenant_id":    "tenant_policy",
			"tenant_name":  "Policy Tenant",
			"signup_scope": "invite_only",
		},
	}, token)
	if policyTenantResp.Code != http.StatusOK {
		t.Fatalf("policy tenant update status=%d body=%s", policyTenantResp.Code, policyTenantResp.Body.String())
	}
	defaultPolicyResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/hubs/"+hubID+"/registration-policy", map[string]any{
		"tenant": map[string]any{
			"tenant_id":    "tenant_default",
			"signup_scope": "inherit",
		},
	}, token)
	if defaultPolicyResp.Code != http.StatusOK {
		t.Fatalf("default policy update status=%d body=%s", defaultPolicyResp.Code, defaultPolicyResp.Body.String())
	}
	if !bytes.Contains(defaultPolicyResp.Body.Bytes(), []byte(`"tenant_default"`)) || bytes.Contains(defaultPolicyResp.Body.Bytes(), []byte(`"tenants":{""`)) {
		t.Fatalf("default policy response should expose tenant_default, body=%s", defaultPolicyResp.Body.String())
	}
	policyListResp := doJSONRequest(t, svc.handler, http.MethodGet, "/api/admin/hubs", nil, token)
	if policyListResp.Code != http.StatusOK {
		t.Fatalf("hub list should expose policy-only tenant before authorization, status=%d body=%s", policyListResp.Code, policyListResp.Body.String())
	}
	var policyListBody struct {
		Hubs []struct {
			Tenants []struct {
				TenantID   string `json:"tenant_id"`
				TenantName string `json:"tenant_name"`
			} `json:"tenants"`
		} `json:"hubs"`
	}
	if err := json.Unmarshal(policyListResp.Body.Bytes(), &policyListBody); err != nil {
		t.Fatalf("decode policy-only tenant list: %v body=%s", err, policyListResp.Body.String())
	}
	foundDefaultTenant := false
	foundPolicyTenant := false
	for _, hub := range policyListBody.Hubs {
		for _, tenant := range hub.Tenants {
			if tenant.TenantID == "tenant_default" {
				foundDefaultTenant = true
			}
			if tenant.TenantID == "tenant_policy" && tenant.TenantName == "Policy Tenant" {
				foundPolicyTenant = true
			}
		}
	}
	if !foundDefaultTenant {
		t.Fatalf("hub list should expose default tenant as a manageable tenant, decoded=%+v body=%s", policyListBody, policyListResp.Body.String())
	}
	if !foundPolicyTenant {
		t.Fatalf("hub list should expose policy-only tenant before authorization, decoded=%+v body=%s", policyListBody, policyListResp.Body.String())
	}

	missingTenantResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/hubs/"+hubID+"/digital-employee-authorization", map[string]any{
		"quota":   1,
		"years":   1,
		"enabled": true,
	}, token)
	if missingTenantResp.Code != http.StatusBadRequest || responseErrorCode(t, missingTenantResp) != "TENANT_ID_REQUIRED" {
		t.Fatalf("expected missing tenant rejection, status=%d body=%s", missingTenantResp.Code, missingTenantResp.Body.String())
	}

	defaultTenantResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/hubs/"+hubID+"/digital-employee-authorization", map[string]any{
		"tenant_id": "tenant_default",
		"quota":     1,
		"years":     1,
		"enabled":   true,
	}, token)
	if defaultTenantResp.Code != http.StatusOK {
		t.Fatalf("default tenant authorization update status=%d body=%s", defaultTenantResp.Code, defaultTenantResp.Body.String())
	}

	noSettingsHubService := hubs.NewService(svc.store.Hubs, svc.store.HubUserLinks, svc.store.HubDomainRoutes, svc.store.BlockedEmails, svc.store.BlockedIPs, nil, svc.mailer, "http://127.0.0.1:9388")
	noSettingsHandler := NewRouter(svc.admins, noSettingsHubService, svc.entry, nil, nil, svc.store.FailureLogs, nil, nil, nil, svc.store.System, svc.store.News, nil)
	storeUnavailableResp := doJSONRequest(t, noSettingsHandler, http.MethodPost, "/api/admin/hubs/"+hubID+"/digital-employee-authorization", map[string]any{
		"tenant_id": "tenant_a",
		"quota":     1,
		"years":     1,
		"enabled":   true,
	}, token)
	if storeUnavailableResp.Code != http.StatusServiceUnavailable || responseErrorCode(t, storeUnavailableResp) != "DIGITAL_EMPLOYEE_AUTHORIZATION_STORE_UNAVAILABLE" {
		t.Fatalf("expected unavailable authorization store rejection, status=%d body=%s", storeUnavailableResp.Code, storeUnavailableResp.Body.String())
	}

	zeroQuotaResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/hubs/"+hubID+"/digital-employee-authorization", map[string]any{
		"tenant_id": "tenant_a",
		"quota":     0,
		"years":     1,
		"enabled":   true,
	}, token)
	if zeroQuotaResp.Code != http.StatusBadRequest || responseErrorCode(t, zeroQuotaResp) != "DIGITAL_EMPLOYEE_QUOTA_REQUIRED" {
		t.Fatalf("expected enabled zero quota rejection, status=%d body=%s", zeroQuotaResp.Code, zeroQuotaResp.Body.String())
	}

	missingYearsResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/hubs/"+hubID+"/digital-employee-authorization", map[string]any{
		"tenant_id": "tenant_a",
		"quota":     4,
	}, token)
	if missingYearsResp.Code != http.StatusBadRequest || responseErrorCode(t, missingYearsResp) != "INVALID_YEARS" {
		t.Fatalf("expected implicit enabled yearly authorization rejection, status=%d body=%s", missingYearsResp.Code, missingYearsResp.Body.String())
	}

	resp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/hubs/"+hubID+"/digital-employee-authorization", map[string]any{
		"tenant_id": "tenant_a",
		"quota":     4,
		"years":     1,
		"enabled":   true,
	}, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("authorization update status=%d body=%s", resp.Code, resp.Body.String())
	}
	var updateBody struct {
		TenantID      string `json:"tenant_id"`
		Authorization struct {
			Quota  int  `json:"quota"`
			Active bool `json:"active"`
		} `json:"digital_employee_authorization"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &updateBody); err != nil {
		t.Fatalf("decode authorization update response: %v body=%s", err, resp.Body.String())
	}
	if updateBody.TenantID != "tenant_a" || updateBody.Authorization.Quota != 4 || !updateBody.Authorization.Active {
		t.Fatalf("expected active tenant_a quota response, decoded=%+v body=%s", updateBody, resp.Body.String())
	}

	decreaseResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/hubs/"+hubID+"/digital-employee-authorization", map[string]any{
		"tenant_id": "tenant_a",
		"quota":     3,
		"years":     1,
		"enabled":   true,
	}, token)
	if decreaseResp.Code != http.StatusBadRequest || responseErrorCode(t, decreaseResp) != "DIGITAL_EMPLOYEE_QUOTA_DECREASE" {
		t.Fatalf("expected quota decrease rejection, status=%d body=%s", decreaseResp.Code, decreaseResp.Body.String())
	}

	invalidYearsResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/hubs/"+hubID+"/digital-employee-authorization", map[string]any{
		"tenant_id": "tenant_a",
		"quota":     4,
		"years":     0,
		"enabled":   true,
	}, token)
	if invalidYearsResp.Code != http.StatusBadRequest || responseErrorCode(t, invalidYearsResp) != "INVALID_YEARS" {
		t.Fatalf("expected enabled yearly authorization rejection, status=%d body=%s", invalidYearsResp.Code, invalidYearsResp.Body.String())
	}

	renewResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/hubs/"+hubID+"/digital-employee-authorization", map[string]any{
		"tenant_id": "tenant_a",
		"years":     1,
		"enabled":   true,
	}, token)
	if renewResp.Code != http.StatusOK {
		t.Fatalf("authorization renew without quota status=%d body=%s", renewResp.Code, renewResp.Body.String())
	}
	var renewBody struct {
		Authorization struct {
			Quota  int  `json:"quota"`
			Active bool `json:"active"`
		} `json:"digital_employee_authorization"`
	}
	if err := json.Unmarshal(renewResp.Body.Bytes(), &renewBody); err != nil {
		t.Fatalf("decode authorization renew response: %v body=%s", err, renewResp.Body.String())
	}
	if renewBody.Authorization.Quota != 4 || !renewBody.Authorization.Active {
		t.Fatalf("renew without quota should preserve active quota, decoded=%+v body=%s", renewBody, renewResp.Body.String())
	}

	heartbeatResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/hubs/"+hubID+"/heartbeat", map[string]any{
		"hub_secret": hubSecret,
	}, "")
	if heartbeatResp.Code != http.StatusOK {
		t.Fatalf("heartbeat status=%d body=%s", heartbeatResp.Code, heartbeatResp.Body.String())
	}
	var heartbeatBody struct {
		LegacyAuthorization  map[string]any            `json:"digital_employee_authorization"`
		TenantAuthorizations map[string]map[string]any `json:"digital_employee_authorizations"`
	}
	if err := json.Unmarshal(heartbeatResp.Body.Bytes(), &heartbeatBody); err != nil {
		t.Fatalf("decode heartbeat response: %v body=%s", err, heartbeatResp.Body.String())
	}
	if heartbeatBody.LegacyAuthorization["quota"] != float64(1) || heartbeatBody.TenantAuthorizations["tenant_a"]["quota"] != float64(4) {
		t.Fatalf("heartbeat should push default and tenant digital employee authorizations, decoded=%+v body=%s", heartbeatBody, heartbeatResp.Body.String())
	}

	tenantResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/hubs/"+hubID+"/digital-employee-authorization", map[string]any{
		"tenant_id": "tenant_b",
		"quota":     2,
		"years":     1,
		"enabled":   true,
	}, token)
	if tenantResp.Code != http.StatusOK {
		t.Fatalf("tenant authorization update status=%d body=%s", tenantResp.Code, tenantResp.Body.String())
	}
	listResp := doJSONRequest(t, svc.handler, http.MethodGet, "/api/admin/hubs", nil, token)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list hubs with tenant authorization status=%d body=%s", listResp.Code, listResp.Body.String())
	}
	var listBody struct {
		Hubs []struct {
			Tenants                       []map[string]any          `json:"tenants"`
			DigitalEmployeeAuthorizations map[string]map[string]any `json:"digital_employee_authorizations"`
		} `json:"hubs"`
	}
	if err := json.Unmarshal(listResp.Body.Bytes(), &listBody); err != nil {
		t.Fatalf("decode hub list response: %v body=%s", err, listResp.Body.String())
	}
	if len(listBody.Hubs) != 1 || listBody.Hubs[0].DigitalEmployeeAuthorizations["tenant_default"] == nil || listBody.Hubs[0].DigitalEmployeeAuthorizations["tenant_b"] == nil || listBody.Hubs[0].DigitalEmployeeAuthorizations[""] != nil {
		t.Fatalf("hub list should expose default and tenant auths without legacy empty key, decoded=%+v body=%s", listBody, listResp.Body.String())
	}
	foundTenantB := false
	foundDefaultTenant = false
	for _, tenant := range listBody.Hubs[0].Tenants {
		if tenant["tenant_id"] == "tenant_default" {
			foundDefaultTenant = true
		}
		if tenant["tenant_id"] == "tenant_b" {
			foundTenantB = true
		}
	}
	if !foundDefaultTenant || !foundTenantB {
		t.Fatalf("hub list should expose default tenant and tenant_b in tenants, decoded=%+v body=%s", listBody, listResp.Body.String())
	}
	tenantHeartbeat := doJSONRequest(t, svc.handler, http.MethodPost, "/api/hubs/"+hubID+"/heartbeat", map[string]any{
		"hub_secret": hubSecret,
	}, "")
	if tenantHeartbeat.Code != http.StatusOK {
		t.Fatalf("tenant heartbeat status=%d body=%s", tenantHeartbeat.Code, tenantHeartbeat.Body.String())
	}
	var tenantHeartbeatBody struct {
		TenantAuthorizations map[string]map[string]any `json:"digital_employee_authorizations"`
	}
	if err := json.Unmarshal(tenantHeartbeat.Body.Bytes(), &tenantHeartbeatBody); err != nil {
		t.Fatalf("decode tenant heartbeat response: %v body=%s", err, tenantHeartbeat.Body.String())
	}
	if tenantHeartbeatBody.TenantAuthorizations["tenant_a"] == nil || tenantHeartbeatBody.TenantAuthorizations["tenant_b"]["quota"] != float64(2) {
		t.Fatalf("heartbeat should push tenant digital employee authorizations, decoded=%+v body=%s", tenantHeartbeatBody, tenantHeartbeat.Body.String())
	}

	disableResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/hubs/"+hubID+"/digital-employee-authorization", map[string]any{
		"tenant_id": "tenant_a",
		"enabled":   false,
	}, token)
	if disableResp.Code != http.StatusOK {
		t.Fatalf("authorization disable status=%d body=%s", disableResp.Code, disableResp.Body.String())
	}
	var disableBody struct {
		Authorization struct {
			Quota  int    `json:"quota"`
			Active bool   `json:"active"`
			Reason string `json:"reason"`
		} `json:"digital_employee_authorization"`
	}
	if err := json.Unmarshal(disableResp.Body.Bytes(), &disableBody); err != nil {
		t.Fatalf("decode disable response: %v body=%s", err, disableResp.Body.String())
	}
	if disableBody.Authorization.Quota != 4 || disableBody.Authorization.Active || disableBody.Authorization.Reason != "disabled" {
		t.Fatalf("disable should preserve quota but mark inactive, decoded=%+v body=%s", disableBody, disableResp.Body.String())
	}

	disabledHeartbeat := doJSONRequest(t, svc.handler, http.MethodPost, "/api/hubs/"+hubID+"/heartbeat", map[string]any{
		"hub_secret": hubSecret,
	}, "")
	if disabledHeartbeat.Code != http.StatusOK {
		t.Fatalf("disabled heartbeat status=%d body=%s", disabledHeartbeat.Code, disabledHeartbeat.Body.String())
	}
	var disabledHeartbeatBody struct {
		TenantAuthorizations map[string]struct {
			Quota  int    `json:"quota"`
			Active bool   `json:"active"`
			Reason string `json:"reason"`
		} `json:"digital_employee_authorizations"`
	}
	if err := json.Unmarshal(disabledHeartbeat.Body.Bytes(), &disabledHeartbeatBody); err != nil {
		t.Fatalf("decode disabled heartbeat response: %v body=%s", err, disabledHeartbeat.Body.String())
	}
	tenantAAuth := disabledHeartbeatBody.TenantAuthorizations["tenant_a"]
	if tenantAAuth.Quota != 4 || tenantAAuth.Active || tenantAAuth.Reason != "disabled" {
		t.Fatalf("heartbeat should push disabled digital employee authorization, decoded=%+v body=%s", disabledHeartbeatBody, disabledHeartbeat.Body.String())
	}
}

func TestManagementHandlersRequireAdminToken(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)

	resp := doJSONRequest(t, svc.handler, http.MethodGet, "/api/admin/hubs", nil, "")
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without admin token, got %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestExpertMarketAdminRoutesRequireAdminToken(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	smStore, err := skillmarket.NewStore(db, db)
	if err != nil {
		t.Fatal(err)
	}
	users := skillmarket.NewUserService(smStore, nil)
	authSvc := skillmarket.NewAuthService(smStore, nil, "")
	handlers := NewSkillMarketHandlers(SkillMarketConfig{Store: smStore, UserSvc: users, AuthSvc: authSvc, CreditsSvc: skillmarket.NewCreditsService(smStore), DataDir: t.TempDir()})
	// Use the complete test service for admin authentication and replace only the
	// optional Skill Market handler passed into its otherwise production router.
	svc := newHubCenterHTTPTestServices(t)
	svc.handler = NewRouter(svc.admins, svc.hubs, svc.entry, nil, nil, svc.store.FailureLogs, nil, nil, handlers, svc.store.System, svc.store.News, nil)
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/admin/expert-market/experts"},
		{http.MethodGet, "/api/v1/admin/expert-market/experts/example/events"},
		{http.MethodPost, "/api/v1/admin/expert-market/experts/example/approve"},
		{http.MethodPost, "/api/v1/admin/expert-market/experts/example/reject"},
		{http.MethodPost, "/api/v1/admin/expert-market/experts/example/unlist"},
		{http.MethodDelete, "/api/v1/admin/expert-market/experts/example"},
		{http.MethodDelete, "/api/v1/admin/expert-market/experts/example/purge"},
	} {
		resp := doJSONRequest(t, svc.handler, route.method, route.path, nil, "")
		if resp.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s: expected 401, got %d body=%s", route.method, route.path, resp.Code, resp.Body.String())
		}
	}
	retired := doJSONRequest(t, svc.handler, http.MethodPost, "/api/v1/admin/expert-market/experts/example/list", nil, "")
	if retired.Code != http.StatusNotFound {
		t.Fatalf("retired list route: expected 404, got %d body=%s", retired.Code, retired.Body.String())
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

func TestListFailureLogsHandlerReturnsFilteredLogs(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	token := issueAdminToken(t, svc)

	now := time.Date(2026, 4, 25, 9, 0, 0, 0, time.UTC)
	for _, item := range []*store.FailureEventLog{
		{
			ID:          "center_fail_default_1",
			TenantID:    "tenant_default",
			Category:    "registration",
			EventCode:   "DEFAULT_TENANT_FAILED",
			Message:     "default tenant registration failed",
			EntityID:    "hub_default",
			Email:       "owner@example.com",
			ClientIP:    "172.16.0.9",
			DetailsJSON: `{"field":"tenant_id"}`,
			CreatedAt:   now.Add(-time.Minute),
		},
		{
			ID:          "center_fail_default_legacy_1",
			TenantID:    "",
			Category:    "registration",
			EventCode:   "DEFAULT_TENANT_LEGACY_FAILED",
			Message:     "legacy default tenant registration failed",
			EntityID:    "hub_default_legacy",
			Email:       "owner-legacy@example.com",
			ClientIP:    "172.16.0.8",
			DetailsJSON: `{"field":"tenant_id"}`,
			CreatedAt:   now.Add(-2 * time.Minute),
		},
		{
			ID:          "center_fail_register_1",
			TenantID:    "tenant_a",
			Category:    "registration",
			EventCode:   "HUB_REGISTER_VALIDATE_FAILED",
			Message:     "hub registration validation failed",
			EntityID:    "hub_rapidai",
			Email:       "owner@rapidai.tech",
			ClientIP:    "172.16.0.10",
			DetailsJSON: `{"field":"base_url","reason":"missing"}`,
			CreatedAt:   now,
		},
		{
			ID:          "center_fail_ha_1",
			TenantID:    "tenant_b",
			Category:    "ha",
			EventCode:   "HA_APPLY_FAILED",
			Message:     "ha apply failed",
			EntityID:    "peer_1",
			ClientIP:    "172.16.0.11",
			DetailsJSON: `{"peer":"node-b"}`,
			CreatedAt:   now.Add(time.Minute),
		},
	} {
		if err := svc.store.FailureLogs.Create(context.Background(), item); err != nil {
			t.Fatalf("create failure log %s: %v", item.ID, err)
		}
	}

	resp := doJSONRequest(t, svc.handler, http.MethodGet, "/api/admin/failure-logs?category=registration&keyword=rapidai&limit=5&offset=0", nil, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", resp.Code, resp.Body.String())
	}

	body := resp.Body.String()
	for _, want := range []string{
		`"total":1`,
		`"event_code":"HUB_REGISTER_VALIDATE_FAILED"`,
		`"email":"owner@rapidai.tech"`,
		`"field":"base_url"`,
		`"reason":"missing"`,
		`"created_at":"2026-04-25T09:00:00Z"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected body to contain %s, body=%s", want, body)
		}
	}
	if strings.Contains(body, "HA_APPLY_FAILED") {
		t.Fatalf("expected filtered response to exclude ha log, body=%s", body)
	}
	if strings.Contains(body, `"details_json"`) {
		t.Fatalf("expected decoded details payload, body=%s", body)
	}

	tenantResp := doJSONRequest(t, svc.handler, http.MethodGet, "/api/admin/failure-logs?tenant_id=tenant_a&limit=5", nil, token)
	if tenantResp.Code != http.StatusOK {
		t.Fatalf("expected tenant filtered 200, got %d body=%s", tenantResp.Code, tenantResp.Body.String())
	}
	tenantBody := tenantResp.Body.String()
	if !strings.Contains(tenantBody, `"tenant_id":"tenant_a"`) || !strings.Contains(tenantBody, `"total":1`) {
		t.Fatalf("expected tenant_a failure log, body=%s", tenantBody)
	}
	if strings.Contains(tenantBody, "HA_APPLY_FAILED") || strings.Contains(tenantBody, `"tenant_id":"tenant_b"`) {
		t.Fatalf("tenant filter leaked other tenant log, body=%s", tenantBody)
	}

	defaultTenantResp := doJSONRequest(t, svc.handler, http.MethodGet, "/api/admin/failure-logs?tenant_id=tenant_default&limit=5", nil, token)
	if defaultTenantResp.Code != http.StatusOK {
		t.Fatalf("expected default tenant filtered 200, got %d body=%s", defaultTenantResp.Code, defaultTenantResp.Body.String())
	}
	defaultTenantBody := defaultTenantResp.Body.String()
	if !strings.Contains(defaultTenantBody, `"tenant_id":"tenant_default"`) || !strings.Contains(defaultTenantBody, `"total":2`) || !strings.Contains(defaultTenantBody, "DEFAULT_TENANT_FAILED") || !strings.Contains(defaultTenantBody, "DEFAULT_TENANT_LEGACY_FAILED") {
		t.Fatalf("expected default tenant failure log, body=%s", defaultTenantBody)
	}
	if strings.Contains(defaultTenantBody, `"tenant_id":""`) || strings.Contains(defaultTenantBody, "HUB_REGISTER_VALIDATE_FAILED") || strings.Contains(defaultTenantBody, "HA_APPLY_FAILED") {
		t.Fatalf("default tenant filter leaked internal or other tenant log, body=%s", defaultTenantBody)
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

	registerResult, err := svc.hubs.RegisterHub(context.Background(), hubs.RegisterHubRequest{
		InstallationID:       "inst_personal_hub_secret_source",
		OwnerEmail:           "owner@example.com",
		Name:                 "Personal Hub",
		Description:          "Personal remote hub",
		BaseURL:              "https://personal.example.com",
		Host:                 "personal.example.com",
		Port:                 9399,
		Visibility:           "private",
		EnrollmentMode:       "open",
		CorporateEmailDomain: "personal.example.com",
		CorporateEmailDomains: []string{
			"personal.example.com",
			"team.personal.example.com",
		},
	})
	if err != nil {
		t.Fatalf("register hub: %v", err)
	}
	hubItems, err := svc.hubs.ListHubs(context.Background())
	if err != nil {
		t.Fatalf("list internal hubs: %v", err)
	}
	if len(hubItems) != 1 {
		t.Fatalf("expected 1 internal hub, got %d", len(hubItems))
	}
	internalJSON, err := json.Marshal(hubItems[0])
	if err != nil {
		t.Fatalf("marshal internal hub: %v", err)
	}
	if !bytes.Contains(internalJSON, []byte(`"installation_id":"inst_personal_hub_secret_source"`)) {
		t.Fatalf("expected internal hub JSON to retain installation_id for HA sync, json=%s", string(internalJSON))
	}
	now := time.Now().UTC()
	if err := svc.store.HubUserLinks.Upsert(context.Background(), &store.HubUserLink{ID: "personal-enterprise", HubID: registerResult.HubID, Email: "user@personal.example.com", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("seed enterprise link: %v", err)
	}
	if err := svc.store.HubUserLinks.Upsert(context.Background(), &store.HubUserLink{ID: "personal-guest", HubID: registerResult.HubID, Email: "user@external.example", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("seed guest link: %v", err)
	}
	if err := svc.store.HubDomainRoutes.Upsert(context.Background(), &store.HubDomainRoute{ID: "personal-route-extra", HubID: registerResult.HubID, Domain: "route-only.personal.example.com", Enabled: true, Priority: 10, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("seed route domain: %v", err)
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
	if !bytes.Contains(resp.Body.Bytes(), []byte(`"guest_domains":["external.example"]`)) {
		t.Fatalf("expected filtered guest_domains in response, body=%s", body)
	}
	if !bytes.Contains(resp.Body.Bytes(), []byte(`"corporate_email_domains":["personal.example.com","team.personal.example.com","route-only.personal.example.com"]`)) {
		t.Fatalf("expected configured default tenant domains in response, body=%s", body)
	}
	if bytes.Contains(resp.Body.Bytes(), []byte(`"guest_domains":["example.com"`)) {
		t.Fatalf("owner email domain should not be listed as scattered guest domain, body=%s", body)
	}
	if bytes.Contains(resp.Body.Bytes(), []byte(`"OwnerEmail"`)) || bytes.Contains(resp.Body.Bytes(), []byte(`"BaseURL"`)) {
		t.Fatalf("expected snake_case response fields, body=%s", body)
	}
	if bytes.Contains(resp.Body.Bytes(), []byte(`"installation_id"`)) || bytes.Contains(resp.Body.Bytes(), []byte(`"InstallationID"`)) {
		t.Fatalf("expected installation id to be omitted from response, body=%s", body)
	}

	domainResp := doJSONRequest(t, svc.handler, http.MethodGet, "/api/admin/routing/enterprise-mail-domains", nil, token)
	if domainResp.Code != http.StatusOK {
		t.Fatalf("enterprise domains status = %d, body = %s", domainResp.Code, domainResp.Body.String())
	}
	domainBody := domainResp.Body.String()
	if !bytes.Contains(domainResp.Body.Bytes(), []byte(`"enterprise_domain":"personal.example.com"`)) {
		t.Fatalf("expected enterprise domain in response, body=%s", domainBody)
	}
	if !bytes.Contains(domainResp.Body.Bytes(), []byte(`"tenant_id":"tenant_default"`)) || bytes.Contains(domainResp.Body.Bytes(), []byte(`"tenant_id":""`)) {
		t.Fatalf("enterprise domain response should expose default tenant with admin id, body=%s", domainBody)
	}
	if !bytes.Contains(domainResp.Body.Bytes(), []byte(`"guest_domains":["external.example"]`)) {
		t.Fatalf("expected filtered guest domains in enterprise response, body=%s", domainBody)
	}
	var domainList struct {
		Items []struct {
			TenantID          string   `json:"tenant_id"`
			EnterpriseDomain  string   `json:"enterprise_domain"`
			EnterpriseDomains []string `json:"enterprise_domains"`
			GuestDomains      []string `json:"guest_domains"`
		} `json:"items"`
	}
	if err := json.Unmarshal(domainResp.Body.Bytes(), &domainList); err != nil {
		t.Fatalf("decode enterprise domains response: %v body=%s", err, domainBody)
	}
	if len(domainList.Items) != 1 || domainList.Items[0].TenantID != "tenant_default" || !reflect.DeepEqual(domainList.Items[0].EnterpriseDomains, []string{"personal.example.com", "team.personal.example.com", "route-only.personal.example.com"}) || !reflect.DeepEqual(domainList.Items[0].GuestDomains, []string{"external.example"}) {
		t.Fatalf("enterprise domains should be grouped once per tenant, decoded=%+v body=%s", domainList, domainBody)
	}

	dashboardResp := doJSONRequest(t, svc.handler, http.MethodGet, "/api/admin/users/dashboard", nil, token)
	if dashboardResp.Code != http.StatusOK {
		t.Fatalf("user dashboard status = %d, body = %s", dashboardResp.Code, dashboardResp.Body.String())
	}
	dashboardBody := dashboardResp.Body.String()
	if !bytes.Contains(dashboardResp.Body.Bytes(), []byte(`"tenant_id":"tenant_default"`)) || bytes.Contains(dashboardResp.Body.Bytes(), []byte(`"tenant_id":""`)) {
		t.Fatalf("user dashboard should expose default tenant with admin id, body=%s", dashboardBody)
	}
}

func TestUpdateHubNameHandlerUpdatesAdminDisplayName(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	token := issueAdminToken(t, svc)

	registerResult, err := svc.hubs.RegisterHub(context.Background(), hubs.RegisterHubRequest{
		InstallationID: "inst_rename_secret_source",
		OwnerEmail:     "owner-rename@example.com",
		Name:           "Original Hub Name",
		BaseURL:        "https://rename.example.com",
		Visibility:     "shared",
		EnrollmentMode: "open",
	})
	if err != nil {
		t.Fatalf("register hub: %v", err)
	}

	if _, err := svc.hubs.RegisterHub(context.Background(), hubs.RegisterHubRequest{
		InstallationID: "inst_rename_secret_source",
		OwnerEmail:     "owner-rename@example.com",
		Name:           "Self Reported Before Override",
		BaseURL:        "https://rename.example.com",
		Visibility:     "shared",
		EnrollmentMode: "open",
	}); err != nil {
		t.Fatalf("re-register hub before admin override: %v", err)
	}
	beforeOverrideResp := doJSONRequest(t, svc.handler, http.MethodGet, "/api/admin/hubs", nil, token)
	if beforeOverrideResp.Code != http.StatusOK {
		t.Fatalf("list hubs before admin override status = %d, body = %s", beforeOverrideResp.Code, beforeOverrideResp.Body.String())
	}
	if !bytes.Contains(beforeOverrideResp.Body.Bytes(), []byte(`"name":"Self Reported Before Override"`)) {
		t.Fatalf("self-reported name should update before admin override, body=%s", beforeOverrideResp.Body.String())
	}

	resp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/hubs/name", map[string]any{
		"hub_id": registerResult.HubID,
		"name":   "Desk A Production",
	}, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("update hub name status = %d, body = %s", resp.Code, resp.Body.String())
	}
	if !bytes.Contains(resp.Body.Bytes(), []byte(`"name":"Desk A Production"`)) {
		t.Fatalf("expected updated name in response, body=%s", resp.Body.String())
	}
	if bytes.Contains(resp.Body.Bytes(), []byte(`"installation_id"`)) {
		t.Fatalf("admin update response should not expose installation id, body=%s", resp.Body.String())
	}

	listResp := doJSONRequest(t, svc.handler, http.MethodGet, "/api/admin/hubs", nil, token)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list hubs status = %d, body = %s", listResp.Code, listResp.Body.String())
	}
	if !bytes.Contains(listResp.Body.Bytes(), []byte(`"name":"Desk A Production"`)) {
		t.Fatalf("expected updated name in list response, body=%s", listResp.Body.String())
	}
	if bytes.Contains(listResp.Body.Bytes(), []byte(`"installation_id"`)) {
		t.Fatalf("admin list response should not expose installation id, body=%s", listResp.Body.String())
	}

	legacyResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/hubs/"+registerResult.HubID+"/name", map[string]any{
		"name": "Desk B Backup",
	}, token)
	if legacyResp.Code != http.StatusOK {
		t.Fatalf("legacy update hub name status = %d, body = %s", legacyResp.Code, legacyResp.Body.String())
	}
	if !bytes.Contains(legacyResp.Body.Bytes(), []byte(`"name":"Desk B Backup"`)) {
		t.Fatalf("expected legacy updated name in response, body=%s", legacyResp.Body.String())
	}

	if _, err := svc.hubs.RegisterHub(context.Background(), hubs.RegisterHubRequest{
		InstallationID: "inst_rename_secret_source",
		OwnerEmail:     "owner-rename@example.com",
		Name:           "Self Reported Name",
		BaseURL:        "https://rename.example.com",
		Visibility:     "shared",
		EnrollmentMode: "open",
	}); err != nil {
		t.Fatalf("re-register hub: %v", err)
	}
	afterReregisterResp := doJSONRequest(t, svc.handler, http.MethodGet, "/api/admin/hubs", nil, token)
	if afterReregisterResp.Code != http.StatusOK {
		t.Fatalf("list hubs after re-register status = %d, body = %s", afterReregisterResp.Code, afterReregisterResp.Body.String())
	}
	if !bytes.Contains(afterReregisterResp.Body.Bytes(), []byte(`"name":"Desk B Backup"`)) || bytes.Contains(afterReregisterResp.Body.Bytes(), []byte(`"name":"Self Reported Name"`)) {
		t.Fatalf("admin display name should survive hub re-registration, body=%s", afterReregisterResp.Body.String())
	}
}

func TestAdminHubDefaultTenantIsManageableWithoutInventory(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	token := issueAdminToken(t, svc)

	registerResult, err := svc.hubs.RegisterHub(context.Background(), hubs.RegisterHubRequest{
		OwnerEmail:           "owner-default-tenant@example.com",
		Name:                 "Default Tenant Hub",
		BaseURL:              "https://default-tenant.example.com",
		Visibility:           "shared",
		EnrollmentMode:       "open",
		CorporateEmailDomain: "default-tenant.example.com",
	})
	if err != nil {
		t.Fatalf("register hub: %v", err)
	}

	listResp := doJSONRequest(t, svc.handler, http.MethodGet, "/api/admin/hubs", nil, token)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list hubs status = %d, body = %s", listResp.Code, listResp.Body.String())
	}
	if !bytes.Contains(listResp.Body.Bytes(), []byte(`"tenant_id":"tenant_default"`)) {
		t.Fatalf("admin hub list should always expose default tenant, body=%s", listResp.Body.String())
	}
	if !bytes.Contains(listResp.Body.Bytes(), []byte(`"corporate_email_domain":"default-tenant.example.com"`)) {
		t.Fatalf("admin default tenant should inherit hub mail domain before inventory sync, body=%s", listResp.Body.String())
	}

	policyResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/hubs/"+registerResult.HubID+"/registration-policy", map[string]any{
		"hub_origin":           "self_hosted",
		"default_signup_scope": "domain_restricted",
		"tenant": map[string]any{
			"tenant_id":      "tenant_default",
			"signup_scope":   "inherit",
			"invite_enabled": true,
		},
	}, token)
	if policyResp.Code != http.StatusOK {
		t.Fatalf("default tenant registration policy status = %d, body = %s", policyResp.Code, policyResp.Body.String())
	}
	if !bytes.Contains(policyResp.Body.Bytes(), []byte(`"tenant_default"`)) || bytes.Contains(policyResp.Body.Bytes(), []byte(`"tenants":{""`)) {
		t.Fatalf("default tenant policy response should use admin tenant id, body=%s", policyResp.Body.String())
	}
}

func TestAdminRoutingDiagnosticsHandlerReturnsSnapshotAndMigrationState(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	token := issueAdminToken(t, svc)

	registerConfirmAndHeartbeatHub(t, svc, map[string]any{
		"owner_email":            "owner-routing@example.com",
		"name":                   "RapidAI Hub",
		"base_url":               "https://rapidai.example.com",
		"visibility":             "shared",
		"enrollment_mode":        "approval",
		"corporate_email_domain": "rapidai.tech",
	})
	registerConfirmAndHeartbeatHub(t, svc, map[string]any{
		"owner_email":          "owner-default@example.com",
		"name":                 "Default Hub",
		"base_url":             "https://default.example.com",
		"visibility":           "shared",
		"enrollment_mode":      "approval",
		"accept_public_signup": true,
	})
	if err := svc.entry.Rebuild(context.Background()); err != nil {
		t.Fatalf("rebuild snapshot: %v", err)
	}

	resp := doJSONRequest(t, svc.handler, http.MethodGet, "/api/admin/routing/diagnostics", nil, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", resp.Code, resp.Body.String())
	}

	body := resp.Body.String()
	for _, want := range []string{
		`"domain_routes":1`,
		`"public_hubs":1`,
		`"total":2`,
		`"online":2`,
		`"legacy_domain_hubs":1`,
		`"enabled_domain_routes":1`,
		`"legacy_domain_backfill_pending":0`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected body to contain %s, body=%s", want, body)
		}
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

func TestHubCenterAdminPageIncludesFailureLogsUI(t *testing.T) {
	content := readAdminPageBundle(t)
	for _, want := range []string{
		`data-tab="failurelogs"`,
		`id="tab-failurelogs"`,
		`data-tab="usermgmt"`,
		`section.id='tab-usermgmt'`,
		`/api/admin/users/migrate`,
		`loadFailureLogs`,
		`/api/admin/failure-logs`,
		`routingDiagnosticsTitle`,
		`id="routingDiagnosticsGrid"`,
		`loadRoutingDiagnostics`,
		`/api/admin/routing/diagnostics`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("admin index missing %s", want)
		}
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

func TestAdminRouteQueryByDomainReturnsOnlyExactMatches(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	token := issueAdminToken(t, svc)

	exactHub := registerConfirmAndHeartbeatHub(t, svc, map[string]any{
		"owner_email":            "owner-qx@example.com",
		"name":                   "Qianxin Hub",
		"base_url":               "https://qianxin.example.com",
		"visibility":             "shared",
		"enrollment_mode":        "approval",
		"corporate_email_domain": "qianxin.com",
	})
	registerConfirmAndHeartbeatHub(t, svc, map[string]any{
		"owner_email":     "owner-default@example.com",
		"name":            "Default Hub",
		"base_url":        "https://default.example.com",
		"visibility":      "shared",
		"enrollment_mode": "approval",
	})

	resp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/routing/query", map[string]any{
		"query":      "qianxin.com",
		"query_type": "domain",
	}, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("query status = %d, body = %s", resp.Code, resp.Body.String())
	}

	var result entry.ResolveResult
	if err := json.Unmarshal(resp.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode query response: %v", err)
	}
	if result.DefaultHubID != exactHub["hub_id"] {
		t.Fatalf("expected exact hub %v, got %+v", exactHub["hub_id"], result)
	}
	if len(result.Hubs) != 1 || result.Hubs[0].CorporateEmailDomain != "qianxin.com" || result.Hubs[0].TenantID != "tenant_default" {
		t.Fatalf("expected exact domain route only, got %+v", result.Hubs)
	}
}

func TestAdminRouteQuerySupportsPhoneNumber(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	token := issueAdminToken(t, svc)

	registerResult := registerConfirmAndHeartbeatHub(t, svc, map[string]any{
		"owner_email":     "owner-phone@example.com",
		"name":            "Phone Admin Hub",
		"base_url":        "https://phone-admin.example.com",
		"visibility":      "shared",
		"enrollment_mode": "approval",
	})
	hubID, _ := registerResult["hub_id"].(string)
	hubSecret, _ := registerResult["hub_secret"].(string)
	linkResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/hubs/"+hubID+"/user-links/sync", map[string]any{
		"hub_secret": hubSecret,
		"email":      "phone:19900001111",
		"is_default": true,
	}, "")
	if linkResp.Code != http.StatusOK {
		t.Fatalf("sync phone link status = %d body=%s", linkResp.Code, linkResp.Body.String())
	}

	for _, seed := range []struct {
		name        string
		query       string
		queryType   string
		phoneNumber string
	}{
		{name: "bare phone", query: "199 0000 1111"},
		{name: "phone identity", query: "phone:19900001111", queryType: "phone"},
		{name: "explicit phone number", query: "ignored@example.com", queryType: "email", phoneNumber: "phone:19900001111"},
	} {
		t.Run(seed.name, func(t *testing.T) {
			body := map[string]any{"query": seed.query}
			if seed.queryType != "" {
				body["query_type"] = seed.queryType
			}
			if seed.phoneNumber != "" {
				body["phone_number"] = seed.phoneNumber
			}
			resp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/routing/query", body, token)
			if resp.Code != http.StatusOK {
				t.Fatalf("query status = %d, body = %s", resp.Code, resp.Body.String())
			}
			var result entry.ResolveResult
			if err := json.Unmarshal(resp.Body.Bytes(), &result); err != nil {
				t.Fatalf("decode query response: %v", err)
			}
			if result.Mode != "single" || result.DefaultHubID != hubID || result.Email != "phone:19900001111" {
				t.Fatalf("unexpected phone route query result: %+v", result)
			}
		})
	}
}

func TestAdminRouteQueryRejectsInvalidPhoneNumber(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	token := issueAdminToken(t, svc)

	for _, body := range []map[string]any{
		{"query": "abc123", "query_type": "phone"},
		{"query": "ignored@example.com", "phone_number": "phone:abc123"},
	} {
		resp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/routing/query", body, token)
		if resp.Code != http.StatusBadRequest {
			t.Fatalf("query status = %d, want 400 body=%s", resp.Code, resp.Body.String())
		}
	}
}

func TestAdminRouteQueryDoesNotTreatNumericEmailAsPhone(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	token := issueAdminToken(t, svc)

	phoneHub := registerConfirmAndHeartbeatHub(t, svc, map[string]any{
		"owner_email":     "owner-phone@example.com",
		"name":            "Phone Hub",
		"base_url":        "https://phone.example.com",
		"visibility":      "shared",
		"enrollment_mode": "approval",
	})
	emailHub := registerConfirmAndHeartbeatHub(t, svc, map[string]any{
		"owner_email":     "owner-email@example.com",
		"name":            "Numeric Email Hub",
		"base_url":        "https://numeric-email.example.com",
		"visibility":      "shared",
		"enrollment_mode": "approval",
	})
	for _, seed := range []struct {
		hub   map[string]any
		email string
	}{
		{phoneHub, "phone:19900001111"},
		{emailHub, "19900001111@example.com"},
	} {
		hubID, _ := seed.hub["hub_id"].(string)
		hubSecret, _ := seed.hub["hub_secret"].(string)
		resp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/hubs/"+hubID+"/user-links/sync", map[string]any{
			"hub_secret": hubSecret,
			"email":      seed.email,
			"is_default": true,
		}, "")
		if resp.Code != http.StatusOK {
			t.Fatalf("sync %s status = %d body=%s", seed.email, resp.Code, resp.Body.String())
		}
	}

	resp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/routing/query", map[string]any{
		"query": "19900001111@example.com",
	}, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("query status = %d, body = %s", resp.Code, resp.Body.String())
	}
	var result entry.ResolveResult
	if err := json.Unmarshal(resp.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode query response: %v", err)
	}
	if result.Mode != "single" || result.DefaultHubID != emailHub["hub_id"] || result.Email != "19900001111@example.com" {
		t.Fatalf("numeric email should resolve as email route, got %+v", result)
	}
}

func TestAdminRouteMaintenanceNormalizesPhoneNumber(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	token := issueAdminToken(t, svc)

	var probedEmail string
	hubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/center/user-exists" {
			t.Fatalf("unexpected hub probe path: %s", r.URL.Path)
		}
		var req struct {
			Email string `json:"email"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode hub probe: %v", err)
		}
		probedEmail = req.Email
		writeJSON(w, http.StatusOK, map[string]any{"exists": true})
	}))
	t.Cleanup(hubServer.Close)

	registerResult := registerConfirmAndHeartbeatHub(t, svc, map[string]any{
		"owner_email":     "owner-phone-maintenance@example.com",
		"name":            "Phone Maintenance Hub",
		"base_url":        hubServer.URL,
		"visibility":      "shared",
		"enrollment_mode": "approval",
	})
	hubID, _ := registerResult["hub_id"].(string)

	restoreResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/routing/restore-email-route", map[string]any{
		"email":      "199 0000 1111",
		"hub_id":     hubID,
		"tenant_id":  "tenant_default",
		"is_default": true,
	}, token)
	if restoreResp.Code != http.StatusOK {
		t.Fatalf("restore phone route status = %d body=%s", restoreResp.Code, restoreResp.Body.String())
	}
	var restoreBody map[string]any
	if err := json.Unmarshal(restoreResp.Body.Bytes(), &restoreBody); err != nil {
		t.Fatalf("decode restore response: %v", err)
	}
	if restoreBody["email"] != "phone:19900001111" {
		t.Fatalf("restore response email = %v, want phone:19900001111", restoreBody["email"])
	}

	queryResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/routing/query", map[string]any{
		"query": "19900001111",
	}, token)
	if queryResp.Code != http.StatusOK {
		t.Fatalf("query phone status = %d body=%s", queryResp.Code, queryResp.Body.String())
	}
	var queryResult entry.ResolveResult
	if err := json.Unmarshal(queryResp.Body.Bytes(), &queryResult); err != nil {
		t.Fatalf("decode query response: %v", err)
	}
	if queryResult.Mode != "single" || queryResult.DefaultHubID != hubID || queryResult.Email != "phone:19900001111" {
		t.Fatalf("unexpected restored phone route query result: %+v", queryResult)
	}

	verifyResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/routing/verify-email-route", map[string]any{
		"email": "19900001111",
	}, token)
	if verifyResp.Code != http.StatusOK {
		t.Fatalf("verify phone route status = %d body=%s", verifyResp.Code, verifyResp.Body.String())
	}
	var verifyBody hubs.AdminRouteVerificationResult
	if err := json.Unmarshal(verifyResp.Body.Bytes(), &verifyBody); err != nil {
		t.Fatalf("decode verify response: %v", err)
	}
	if verifyBody.Email != "phone:19900001111" || len(verifyBody.Routes) != 1 || verifyBody.Routes[0].UserExists == nil || !*verifyBody.Routes[0].UserExists {
		t.Fatalf("unexpected verify response: %+v", verifyBody)
	}
	if probedEmail != "phone:19900001111" {
		t.Fatalf("hub probe email = %q, want phone:19900001111", probedEmail)
	}

	deleteResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/routing/delete-email-route", map[string]any{
		"email": "19900001111",
	}, token)
	if deleteResp.Code != http.StatusOK {
		t.Fatalf("delete phone route status = %d body=%s", deleteResp.Code, deleteResp.Body.String())
	}
	var deleteBody map[string]any
	if err := json.Unmarshal(deleteResp.Body.Bytes(), &deleteBody); err != nil {
		t.Fatalf("decode delete response: %v", err)
	}
	if deleteBody["email"] != "phone:19900001111" || deleteBody["deleted_count"] != float64(1) {
		t.Fatalf("unexpected delete response: %+v", deleteBody)
	}
}

func TestAdminRouteQueryShowsTenantName(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	token := issueAdminToken(t, svc)

	namedHub := registerConfirmAndHeartbeatHub(t, svc, map[string]any{
		"owner_email":     "owner-tenant@example.com",
		"name":            "Tenant Hub",
		"base_url":        "https://tenant.example.com",
		"visibility":      "shared",
		"enrollment_mode": "approval",
		"capabilities": map[string]any{
			"tenant_domains":       map[string]any{"tenant_a": []any{"acme.example"}},
			"tenant_domain_source": "configured",
			"tenant_names":         map[string]any{"tenant_a": "研发部"},
		},
	})
	missingNameHub := registerConfirmAndHeartbeatHub(t, svc, map[string]any{
		"owner_email":     "owner-missing-tenant@example.com",
		"name":            "Missing Tenant Name Hub",
		"base_url":        "https://missing-tenant.example.com",
		"visibility":      "shared",
		"enrollment_mode": "approval",
		"capabilities": map[string]any{
			"tenant_domains":       map[string]any{"tenant_b": []any{"beta.example"}},
			"tenant_domain_source": "configured",
			"tenant_names":         map[string]any{"tenant_a": "研发部"},
		},
	})

	resp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/routing/query", map[string]any{
		"query":      "acme.example",
		"query_type": "domain",
	}, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("query status = %d, body = %s", resp.Code, resp.Body.String())
	}
	var result entry.ResolveResult
	if err := json.Unmarshal(resp.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode query response: %v", err)
	}
	if len(result.Hubs) != 1 || result.Hubs[0].HubID != namedHub["hub_id"] || result.Hubs[0].TenantID != "tenant_a" || result.Hubs[0].TenantName != "研发部" {
		t.Fatalf("expected tenant route with tenant name, got %+v", result.Hubs)
	}

	resp = doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/routing/query", map[string]any{
		"query":      "beta.example",
		"query_type": "domain",
	}, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("query missing name status = %d, body = %s", resp.Code, resp.Body.String())
	}
	result = entry.ResolveResult{}
	if err := json.Unmarshal(resp.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode missing name query response: %v", err)
	}
	if len(result.Hubs) != 1 || result.Hubs[0].HubID != missingNameHub["hub_id"] || result.Hubs[0].TenantID != "tenant_b" || result.Hubs[0].TenantName != "" {
		t.Fatalf("expected tenant route without synthetic tenant name, got %+v", result.Hubs)
	}
}

func TestAdminUserMigrationHandlerMovesEmailRoute(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	token := issueAdminToken(t, svc)

	hubA := registerConfirmAndHeartbeatHub(t, svc, map[string]any{
		"owner_email":     "owner-a@example.com",
		"name":            "Hub A",
		"base_url":        "https://a.example.com",
		"visibility":      "shared",
		"enrollment_mode": "approval",
	})
	hubB := registerConfirmAndHeartbeatHub(t, svc, map[string]any{
		"owner_email":     "owner-b@example.com",
		"name":            "Hub B",
		"base_url":        "https://b.example.com",
		"visibility":      "shared",
		"enrollment_mode": "approval",
	})

	linkResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/hubs/"+hubA["hub_id"].(string)+"/user-links/sync", map[string]any{
		"hub_secret": hubA["hub_secret"],
		"email":      "moved@example.com",
		"is_default": true,
	}, "")
	if linkResp.Code != http.StatusOK {
		t.Fatalf("sync link status=%d body=%s", linkResp.Code, linkResp.Body.String())
	}

	migrateResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/users/migrate", map[string]any{
		"mode":        "email",
		"email":       "moved@example.com",
		"from_hub_id": hubA["hub_id"],
		"to_hub_id":   hubB["hub_id"],
	}, token)
	if migrateResp.Code != http.StatusOK {
		t.Fatalf("migrate status=%d body=%s", migrateResp.Code, migrateResp.Body.String())
	}
	var migrateBody struct {
		Migration struct {
			SourceTenantID string `json:"source_tenant_id"`
			TargetTenantID string `json:"target_tenant_id"`
		} `json:"migration"`
	}
	if err := json.Unmarshal(migrateResp.Body.Bytes(), &migrateBody); err != nil {
		t.Fatalf("decode migrate response: %v body=%s", err, migrateResp.Body.String())
	}
	if migrateBody.Migration.SourceTenantID != "tenant_default" || migrateBody.Migration.TargetTenantID != "tenant_default" {
		t.Fatalf("migration response should expose default tenant with admin id, decoded=%+v body=%s", migrateBody, migrateResp.Body.String())
	}

	resolveResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/entry/resolve", map[string]any{"email": "moved@example.com"}, "")
	if resolveResp.Code != http.StatusOK {
		t.Fatalf("resolve status=%d body=%s", resolveResp.Code, resolveResp.Body.String())
	}
	var resolved entry.ResolveResult
	if err := json.Unmarshal(resolveResp.Body.Bytes(), &resolved); err != nil {
		t.Fatalf("decode resolve: %v", err)
	}
	if resolved.DefaultHubID != hubB["hub_id"] {
		t.Fatalf("expected target hub %v, got %+v", hubB["hub_id"], resolved)
	}
}

func TestHubUserLinkDeleteHandlerRemovesTenantScopedRoute(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)

	hub := registerConfirmAndHeartbeatHub(t, svc, map[string]any{
		"owner_email":     "owner-delete@example.com",
		"name":            "Hub Delete",
		"base_url":        "https://delete.example.com",
		"visibility":      "shared",
		"enrollment_mode": "approval",
	})
	hubID := hub["hub_id"].(string)
	hubSecret := hub["hub_secret"]
	for _, tenantID := range []string{"tenant_a", "tenant_b"} {
		resp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/hubs/"+hubID+"/user-links/sync", map[string]any{
			"hub_secret": hubSecret,
			"tenant_id":  tenantID,
			"email":      "scoped-delete@example.com",
		}, "")
		if resp.Code != http.StatusOK {
			t.Fatalf("sync tenant %s status=%d body=%s", tenantID, resp.Code, resp.Body.String())
		}
	}

	deleteResp := doJSONRequest(t, svc.handler, http.MethodDelete, "/api/hubs/"+hubID+"/user-links/sync", map[string]any{
		"hub_secret": hubSecret,
		"tenant_id":  "tenant_a",
		"email":      "SCOPED-DELETE@example.com",
	}, "")
	if deleteResp.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", deleteResp.Code, deleteResp.Body.String())
	}
	links, err := svc.store.HubUserLinks.ListByEmail(context.Background(), "scoped-delete@example.com")
	if err != nil {
		t.Fatalf("ListByEmail: %v", err)
	}
	if len(links) != 1 || links[0].TenantID != "tenant_b" || links[0].HubID != hubID {
		t.Fatalf("expected only tenant_b route left, got %+v", links)
	}
}

func TestAdminUserMigrationHandlerMovesScatteredEmailRoutesWithoutSourceHub(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	token := issueAdminToken(t, svc)

	hubA := registerConfirmAndHeartbeatHub(t, svc, map[string]any{"owner_email": "owner-a@example.com", "name": "Hub A", "base_url": "https://a.example.com", "visibility": "shared", "enrollment_mode": "approval"})
	hubB := registerConfirmAndHeartbeatHub(t, svc, map[string]any{"owner_email": "owner-b@example.com", "name": "Hub B", "base_url": "https://b.example.com", "visibility": "shared", "enrollment_mode": "approval"})
	hubC := registerConfirmAndHeartbeatHub(t, svc, map[string]any{"owner_email": "owner-c@example.com", "name": "Hub C", "base_url": "https://c.example.com", "visibility": "shared", "enrollment_mode": "approval"})

	for i, hub := range []map[string]any{hubA, hubB} {
		resp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/hubs/"+hub["hub_id"].(string)+"/user-links/sync", map[string]any{
			"hub_secret": hub["hub_secret"],
			"email":      "scattered@example.com",
			"is_default": i == 0,
		}, "")
		if resp.Code != http.StatusOK {
			t.Fatalf("sync link status=%d body=%s", resp.Code, resp.Body.String())
		}
	}

	migrateResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/users/migrate", map[string]any{
		"mode":      "email",
		"email":     "scattered@example.com",
		"to_hub_id": hubC["hub_id"],
	}, token)
	if migrateResp.Code != http.StatusOK {
		t.Fatalf("migrate status=%d body=%s", migrateResp.Code, migrateResp.Body.String())
	}

	links, err := svc.store.HubUserLinks.ListByEmail(context.Background(), "scattered@example.com")
	if err != nil {
		t.Fatalf("ListByEmail: %v", err)
	}
	for _, link := range links {
		if link.HubID == hubA["hub_id"] || link.HubID == hubB["hub_id"] {
			t.Fatalf("expected scattered source links removed, got %+v", links)
		}
	}
}

func TestAdminRouteQuerySupportsWildcardEmailPattern(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	token := issueAdminToken(t, svc)

	hubA := registerConfirmAndHeartbeatHub(t, svc, map[string]any{"owner_email": "owner-a@example.com", "name": "Hub A", "base_url": "https://a.example.com", "visibility": "shared", "enrollment_mode": "approval"})
	hubB := registerConfirmAndHeartbeatHub(t, svc, map[string]any{"owner_email": "owner-b@example.com", "name": "Hub B", "base_url": "https://b.example.com", "visibility": "shared", "enrollment_mode": "approval"})

	for i, seed := range []struct {
		hub   map[string]any
		email string
	}{{hubA, "mark@qianxin.com"}, {hubB, "mary@qianxin.com"}, {hubB, "tom@qianxin.com"}} {
		resp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/hubs/"+seed.hub["hub_id"].(string)+"/user-links/sync", map[string]any{
			"hub_secret": seed.hub["hub_secret"],
			"email":      seed.email,
			"is_default": i == 0,
		}, "")
		if resp.Code != http.StatusOK {
			t.Fatalf("sync link status=%d body=%s", resp.Code, resp.Body.String())
		}
	}

	resp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/routing/query", map[string]any{
		"query":      "ｍａ＊＠ｑｉａｎｘｉｎ．ｃｏｍ",
		"query_type": "email",
	}, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("query status=%d body=%s", resp.Code, resp.Body.String())
	}
	var result entry.ResolveResult
	if err := json.Unmarshal(resp.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode query: %v", err)
	}
	if len(result.Hubs) != 2 {
		t.Fatalf("expected two matched hubs for wildcard query, got %+v", result)
	}
}
