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

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func TestManualBindHandlerCreatesUser(t *testing.T) {
	router, _ := newAdminRouterTestServices(t)
	token := issueHubAdminToken(t, router)

	resp := doHubAdminJSONRequest(t, router, http.MethodPost, "/api/admin/users/manual-bind", map[string]any{
		"email": "manual@example.com",
	}, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", resp.Code, resp.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	user, ok := payload["user"].(map[string]any)
	if !ok {
		t.Fatalf("expected user payload, got %#v", payload)
	}
	if user["email"] != "manual@example.com" {
		t.Fatalf("unexpected user payload: %#v", user)
	}
	if user["sn"] == "" {
		t.Fatalf("expected generated sn, got %#v", user)
	}
}

func issueTenantAdminTokenForTest(t *testing.T, services *hubAdminRouterTestServices, slug string) string {
	t.Helper()
	globalToken := issueHubAdminToken(t, services.handler)
	createResp := doHubAdminJSONRequest(t, services.handler, http.MethodPost, "/api/admin/tenants", map[string]any{
		"slug":                   slug,
		"name":                   "Tenant " + slug,
		"initial_admin_username": slug + "-owner",
		"initial_admin_password": "StrongPassword123!",
		"initial_admin_email":    slug + "-owner@example.com",
	}, globalToken)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create tenant status = %d body=%s", createResp.Code, createResp.Body.String())
	}
	loginResp := doHubAdminJSONRequest(t, services.handler, http.MethodPost, "/api/admin/login", map[string]any{
		"username": slug + "-owner",
		"password": "StrongPassword123!",
		"tenant":   "tenant_" + slug,
	}, "")
	if loginResp.Code != http.StatusOK {
		t.Fatalf("tenant admin login status = %d body=%s", loginResp.Code, loginResp.Body.String())
	}
	var payload struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(loginResp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode tenant admin login: %v", err)
	}
	if strings.TrimSpace(payload.AccessToken) == "" {
		t.Fatal("tenant admin token is empty")
	}
	return payload.AccessToken
}

func TestLookupUserHandlerReturnsBoundUser(t *testing.T) {
	router, _ := newAdminRouterTestServices(t)
	token := issueHubAdminToken(t, router)

	resp := doHubAdminJSONRequest(t, router, http.MethodPost, "/api/admin/users/manual-bind", map[string]any{
		"email": "lookup@example.com",
	}, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", resp.Code, resp.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/users/lookup?email=lookup@example.com", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !containsAll(body, "lookup@example.com", `"id":`, `"sn":`) {
		t.Fatalf("unexpected body=%s", body)
	}
}

func TestListUsersHandlerReturnsBoundUsers(t *testing.T) {
	router, _ := newAdminRouterTestServices(t)
	token := issueHubAdminToken(t, router)

	resp := doHubAdminJSONRequest(t, router, http.MethodPost, "/api/admin/users/manual-bind", map[string]any{
		"email": "listed@example.com",
	}, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", resp.Code, resp.Body.String())
	}

	listResp := doHubAdminJSONRequest(t, router, http.MethodGet, "/api/admin/users", nil, token)
	if listResp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", listResp.Code, listResp.Body.String())
	}
	if body := listResp.Body.String(); !containsAll(body, "listed@example.com", `"sn":`, `"enrollment_status":"approved"`) {
		t.Fatalf("unexpected body=%s", body)
	}
}

func TestListUsersHandlerKeepsSameEmailTenantsSeparate(t *testing.T) {
	services := newAdminRouterTestContext(t)
	token := issueHubAdminToken(t, services.handler)
	ctx := context.Background()
	now := time.Now().UTC()

	for _, user := range []*store.User{
		{ID: "user-tenant-a", TenantID: "tenant_a", Email: "same@example.com", SN: "sn-a", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now},
		{ID: "user-tenant-b", TenantID: "tenant_b", Email: "same@example.com", SN: "sn-b", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now},
	} {
		if err := services.store.Users.Create(ctx, user); err != nil {
			t.Fatalf("create user %s: %v", user.ID, err)
		}
	}
	if err := llmservice.SaveRegistry(ctx, scopedSystemSettingsForTenant("tenant_a", services.store.System), &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic", Models: []llmservice.ModelServiceModel{{Name: "gpt-5", ProviderIDs: []string{"provider-a"}}}}},
		Grants:             []llmservice.Grant{{ID: "grant-a", Email: "same@example.com", ServiceGroupID: "coding-basic", Source: "card", StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(24 * time.Hour), CreatedAt: now.Add(-time.Hour)}},
	}); err != nil {
		t.Fatalf("save tenant A registry: %v", err)
	}
	if err := llmservice.SaveRegistry(ctx, scopedSystemSettingsForTenant("tenant_b", services.store.System), &llmservice.Registry{}); err != nil {
		t.Fatalf("save tenant B registry: %v", err)
	}

	resp := doHubAdminJSONRequest(t, services.handler, http.MethodGet, "/api/admin/users", nil, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	var payload struct {
		Users []struct {
			TenantID         string `json:"tenant_id"`
			Email            string `json:"email"`
			HasServiceAccess bool   `json:"has_service_access"`
		} `json:"users"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	seen := map[string]bool{}
	for _, user := range payload.Users {
		if user.Email != "same@example.com" {
			continue
		}
		seen[user.TenantID] = user.HasServiceAccess
	}
	if len(seen) != 2 || !seen["tenant_a"] || seen["tenant_b"] {
		t.Fatalf("expected same email to remain tenant-scoped with tenant A grant only, got %#v body=%s", seen, resp.Body.String())
	}
}
func TestDeleteBoundUserHandlerRemovesUser(t *testing.T) {
	router, _ := newAdminRouterTestServices(t)
	token := issueHubAdminToken(t, router)

	bindResp := doHubAdminJSONRequest(t, router, http.MethodPost, "/api/admin/users/manual-bind", map[string]any{
		"email": "delete-bound@example.com",
	}, token)
	if bindResp.Code != http.StatusOK {
		t.Fatalf("expected bind 200, got %d body=%s", bindResp.Code, bindResp.Body.String())
	}

	deleteResp := doHubAdminJSONRequest(t, router, http.MethodDelete, "/api/admin/users?email=delete-bound@example.com", nil, token)
	if deleteResp.Code != http.StatusOK {
		t.Fatalf("expected delete 200, got %d body=%s", deleteResp.Code, deleteResp.Body.String())
	}
	if body := deleteResp.Body.String(); !containsAll(body, `"ok":true`, `"email":"delete-bound@example.com"`) {
		t.Fatalf("unexpected delete body=%s", body)
	}

	listResp := doHubAdminJSONRequest(t, router, http.MethodGet, "/api/admin/users", nil, token)
	if listResp.Code != http.StatusOK {
		t.Fatalf("expected list 200, got %d body=%s", listResp.Code, listResp.Body.String())
	}
	if strings.Contains(listResp.Body.String(), "delete-bound@example.com") {
		t.Fatalf("expected deleted user to be absent, body=%s", listResp.Body.String())
	}
}
func TestBlockedEmailHandlersPersistEntries(t *testing.T) {
	router, _ := newAdminRouterTestServices(t)
	token := issueHubAdminToken(t, router)

	addResp := doHubAdminJSONRequest(t, router, http.MethodPost, "/api/admin/blocklist", map[string]any{
		"email":  "blocked@example.com",
		"reason": "spam",
	}, token)
	if addResp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", addResp.Code, addResp.Body.String())
	}

	listResp := doHubAdminJSONRequest(t, router, http.MethodGet, "/api/admin/blocklist", nil, token)
	if listResp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", listResp.Code, listResp.Body.String())
	}
	if body := listResp.Body.String(); body == "" || !containsAll(body, "blocked@example.com", "spam") {
		t.Fatalf("unexpected body=%s", body)
	}

	removeResp := doHubAdminJSONRequest(t, router, http.MethodDelete, "/api/admin/blocklist/blocked@example.com", nil, token)
	if removeResp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", removeResp.Code, removeResp.Body.String())
	}

	listResp = doHubAdminJSONRequest(t, router, http.MethodGet, "/api/admin/blocklist", nil, token)
	if listResp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", listResp.Code, listResp.Body.String())
	}
	if body := listResp.Body.String(); strings.Contains(body, "blocked@example.com") {
		t.Fatalf("expected blocked email to be removed, body=%s", body)
	}
}

func TestCenterConfigAndRegisterHandlers(t *testing.T) {
	var capturedRegisterBody map[string]any
	centerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/client/quality" {
			writeJSON(w, http.StatusOK, map[string]any{
				"routable":       true,
				"quality_score":  100,
				"service_status": "ok",
			})
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/api/hubs/register" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&capturedRegisterBody)
		writeJSON(w, http.StatusOK, map[string]any{
			"hub_id":     "hub_123",
			"hub_secret": "secret_123",
		})
	}))
	defer centerServer.Close()

	router, _ := newAdminRouterTestServices(t)
	token := issueHubAdminToken(t, router)

	saveResp := doHubAdminJSONRequest(t, router, http.MethodPost, "/api/admin/center/config", map[string]any{
		"base_url":                centerServer.URL,
		"public_base_url":         "https://hub.example.com",
		"enrollment_mode":         "manual",
		"corporate_email_domain":  "rapidai.tech",
		"corporate_email_domains": []string{"rapidai.tech", "subsidiary.example"},
		"accept_public_signup":    true,
	}, token)
	if saveResp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", saveResp.Code, saveResp.Body.String())
	}

	statusResp := doHubAdminJSONRequest(t, router, http.MethodGet, "/api/admin/center/status", nil, token)
	if statusResp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", statusResp.Code, statusResp.Body.String())
	}
	if body := statusResp.Body.String(); !containsAll(body, centerServer.URL, `"public_base_url":"https://hub.example.com"`, `"registered":false`, `"host":"`, `"port":`, `"register_on_startup":true`, `"admin_email_present":true`, `"enrollment_mode":"manual"`, `"corporate_email_domain":"rapidai.tech"`, `"corporate_email_domains":["rapidai.tech","subsidiary.example"]`, `"accept_public_signup":true`) {
		t.Fatalf("unexpected status body=%s", body)
	}

	registerResp := doHubAdminJSONRequest(t, router, http.MethodPost, "/api/admin/center/register", map[string]any{}, token)
	if registerResp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", registerResp.Code, registerResp.Body.String())
	}
	if body := registerResp.Body.String(); !containsAll(body, `"registered":true`, `"hub_id":"hub_123"`, `"advertised_base_url":"`) {
		t.Fatalf("unexpected register body=%s", body)
	}
	if capturedRegisterBody["host"] == "" {
		t.Fatalf("expected host to be reported, got %#v", capturedRegisterBody)
	}
	if _, ok := capturedRegisterBody["port"].(float64); !ok {
		t.Fatalf("expected numeric port to be reported, got %#v", capturedRegisterBody)
	}
	if capturedRegisterBody["base_url"] == "" {
		t.Fatalf("expected base_url to be reported, got %#v", capturedRegisterBody)
	}
	if capturedRegisterBody["base_url"] != "https://hub.example.com" {
		t.Fatalf("expected configured public base url to be reported, got %#v", capturedRegisterBody)
	}
	if capturedRegisterBody["enrollment_mode"] != "manual" {
		t.Fatalf("expected manual enrollment mode to be reported, got %#v", capturedRegisterBody)
	}
	if capturedRegisterBody["corporate_email_domain"] != "rapidai.tech" {
		t.Fatalf("expected corporate email domain to be reported, got %#v", capturedRegisterBody)
	}
	if domains, ok := capturedRegisterBody["corporate_email_domains"].([]any); !ok || len(domains) != 2 {
		t.Fatalf("expected corporate email domains to be reported, got %#v", capturedRegisterBody)
	}
	if capturedRegisterBody["accept_public_signup"] != true {
		t.Fatalf("expected accept_public_signup to be reported, got %#v", capturedRegisterBody)
	}
}

func TestListFailureLogsHandlerReturnsStoredLogs(t *testing.T) {
	services := newAdminRouterTestContext(t)
	token := issueHubAdminToken(t, services.handler)

	now := time.Date(2026, 4, 25, 8, 30, 0, 0, time.UTC)
	for _, item := range []*store.FailureEventLog{
		{
			ID:          "log_hub_register_1",
			TenantID:    "tenant_a",
			Category:    "registration",
			EventCode:   "REGISTER_FORWARD_FAILED",
			Message:     "hub register forward failed",
			EntityID:    "hub_rapidai",
			Email:       "owner@rapidai.tech",
			ClientIP:    "10.10.0.1",
			DetailsJSON: `{"phase":"register","retryable":true}`,
			CreatedAt:   now,
		},
		{
			ID:          "log_hub_heartbeat_1",
			TenantID:    "tenant_b",
			Category:    "heartbeat",
			EventCode:   "HEARTBEAT_PUSH_FAILED",
			Message:     "hub heartbeat push failed",
			EntityID:    "hub_default",
			Email:       "ops@example.com",
			ClientIP:    "10.10.0.2",
			DetailsJSON: `{"phase":"heartbeat","retryable":false}`,
			CreatedAt:   now.Add(time.Minute),
		},
	} {
		if err := services.store.FailureLogs.Create(context.Background(), item); err != nil {
			t.Fatalf("create failure log %s: %v", item.ID, err)
		}
	}

	resp := doHubAdminJSONRequest(t, services.handler, http.MethodGet, "/api/admin/failure-logs?category=registration&keyword=rapidai&limit=10&offset=0", nil, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", resp.Code, resp.Body.String())
	}

	body := resp.Body.String()
	if !containsAll(body,
		`"total":1`,
		`"category":"registration"`,
		`"event_code":"REGISTER_FORWARD_FAILED"`,
		`"message":"hub register forward failed"`,
		`"email":"owner@rapidai.tech"`,
		`"client_ip":"10.10.0.1"`,
		`"phase":"register"`,
		`"retryable":true`,
		fmt.Sprintf(`"created_at":"%s"`, now.Format(time.RFC3339)),
	) {
		t.Fatalf("unexpected body=%s", body)
	}
	if strings.Contains(body, "HEARTBEAT_PUSH_FAILED") {
		t.Fatalf("expected category filter to exclude heartbeat log, body=%s", body)
	}
}

func TestListFailureLogsHandlerFiltersTenantAdminLogs(t *testing.T) {
	services := newAdminRouterTestContext(t)
	globalToken := issueHubAdminToken(t, services.handler)

	createResp := doHubAdminJSONRequest(t, services.handler, http.MethodPost, "/api/admin/tenants", map[string]any{
		"id":                     "tenant_acme",
		"slug":                   "acme",
		"name":                   "Acme",
		"initial_admin_username": "acme-admin",
		"initial_admin_password": "TenantPass123!",
		"initial_admin_email":    "admin@acme.com",
		"initial_admin_name":     "Acme Admin",
		"primary_domain":         "acme.example.com",
	}, globalToken)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create tenant status=%d body=%s", createResp.Code, createResp.Body.String())
	}

	loginResp := doHubAdminJSONRequest(t, services.handler, http.MethodPost, "/api/admin/login", map[string]any{
		"username": "acme-admin",
		"password": "TenantPass123!",
		"tenant":   "tenant_acme",
	}, "")
	if loginResp.Code != http.StatusOK {
		t.Fatalf("tenant login status=%d body=%s", loginResp.Code, loginResp.Body.String())
	}
	var loginPayload struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(loginResp.Body.Bytes(), &loginPayload); err != nil {
		t.Fatalf("decode login: %v", err)
	}

	now := time.Date(2026, 4, 25, 8, 30, 0, 0, time.UTC)
	logs := []*store.FailureEventLog{
		{ID: "log_tenant", TenantID: "tenant_acme", Category: "registration", EventCode: "TENANT_FAILED", Message: "tenant failure", Email: "admin@acme.com", DetailsJSON: `{}`, CreatedAt: now},
		{ID: "log_other", TenantID: "tenant_other", Category: "registration", EventCode: "OTHER_FAILED", Message: "other failure", Email: "admin@other.com", DetailsJSON: `{}`, CreatedAt: now.Add(time.Minute)},
	}
	for _, item := range logs {
		if err := services.store.FailureLogs.Create(context.Background(), item); err != nil {
			t.Fatalf("create failure log %s: %v", item.ID, err)
		}
	}

	resp := doHubAdminJSONRequest(t, services.handler, http.MethodGet, "/api/admin/failure-logs?category=registration&limit=10", nil, loginPayload.AccessToken)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	if !containsAll(body, `"total":1`, `"tenant_id":"tenant_acme"`, `"event_code":"TENANT_FAILED"`) {
		t.Fatalf("unexpected tenant filtered body=%s", body)
	}
	if strings.Contains(body, "OTHER_FAILED") {
		t.Fatalf("tenant admin should not see other tenant log, body=%s", body)
	}
}

func TestListFailureLogsHandlerRequiresAdminToken(t *testing.T) {
	services := newAdminRouterTestContext(t)

	resp := doHubAdminJSONRequest(t, services.handler, http.MethodGet, "/api/admin/failure-logs", nil, "")
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestAdminChangePasswordHandler(t *testing.T) {
	router, admins := newAdminRouterTestServices(t)
	token := issueHubAdminToken(t, router)

	resp := doHubAdminJSONRequest(t, router, http.MethodPost, "/api/admin/password", map[string]any{
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

	if _, err := admins.Authenticate(context.Background(), token); err == nil {
		t.Fatalf("expected old token to be invalid after password change")
	}
	if _, err := admins.Authenticate(context.Background(), newToken); err != nil {
		t.Fatalf("expected new token to authenticate: %v", err)
	}

	loginResp := doHubAdminJSONRequest(t, router, http.MethodPost, "/api/admin/login", map[string]any{
		"username": "admin",
		"password": "NewStrongPassword123!",
	}, "")
	if loginResp.Code != http.StatusOK {
		t.Fatalf("expected new password login to succeed, got %d body=%s", loginResp.Code, loginResp.Body.String())
	}
}

func TestAdminChangePasswordRejectsBlankWhitespacePassword(t *testing.T) {
	router, _ := newAdminRouterTestServices(t)
	token := issueHubAdminToken(t, router)

	resp := doHubAdminJSONRequest(t, router, http.MethodPost, "/api/admin/password", map[string]any{
		"current_password": "StrongPassword123!",
		"new_password":     "   ",
	}, token)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", resp.Code, resp.Body.String())
	}
	if body := resp.Body.String(); !containsAll(body, `"code":"INVALID_INPUT"`) {
		t.Fatalf("expected invalid input error, got %s", body)
	}
}

func TestAdminSetupRejectsInvalidEmailAsBadRequest(t *testing.T) {
	router, _ := newAdminRouterTestServices(t)

	resp := doHubAdminJSONRequest(t, router, http.MethodPost, "/api/admin/setup", map[string]any{
		"username": "admin",
		"password": "StrongPassword123!",
		"email":    "not-an-email",
	}, "")
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", resp.Code, resp.Body.String())
	}
	if body := resp.Body.String(); !containsAll(body, `"code":"INVALID_INPUT"`, "valid admin email") {
		t.Fatalf("expected invalid input email error, got %s", body)
	}
}

func TestAdminUpdateProfileRejectsInvalidEmailAsBadRequest(t *testing.T) {
	router, _ := newAdminRouterTestServices(t)
	token := issueHubAdminToken(t, router)

	resp := doHubAdminJSONRequest(t, router, http.MethodPost, "/api/admin/profile", map[string]any{
		"email": "not-an-email",
	}, token)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", resp.Code, resp.Body.String())
	}
	if body := resp.Body.String(); !containsAll(body, `"code":"INVALID_INPUT"`, "valid admin email") {
		t.Fatalf("expected invalid input email error, got %s", body)
	}
}

func TestDefaultTenantAdminCanUpdateProfileAndPassword(t *testing.T) {
	ctx := newAdminRouterTestContext(t)
	_ = issueHubAdminToken(t, ctx.handler)

	loginResp := doHubAdminJSONRequest(t, ctx.handler, http.MethodPost, "/api/admin/login", map[string]any{
		"username": "admin",
		"password": "StrongPassword123!",
		"tenant":   store.DefaultTenantID,
	}, "")
	if loginResp.Code != http.StatusOK {
		t.Fatalf("default tenant login status=%d body=%s", loginResp.Code, loginResp.Body.String())
	}
	var loginPayload struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(loginResp.Body.Bytes(), &loginPayload); err != nil {
		t.Fatalf("decode login response: %v", err)
	}

	profileResp := doHubAdminJSONRequest(t, ctx.handler, http.MethodPost, "/api/admin/profile", map[string]any{
		"email": "default-owner@example.com",
	}, loginPayload.AccessToken)
	if profileResp.Code != http.StatusOK {
		t.Fatalf("default tenant profile status=%d body=%s", profileResp.Code, profileResp.Body.String())
	}
	if body := profileResp.Body.String(); !containsAll(body, `"scope":"tenant"`, `"tenant_id":"`+store.DefaultTenantID+`"`, `"email":"default-owner@example.com"`) {
		t.Fatalf("expected default tenant profile projection, body=%s", body)
	}
	var profilePayload struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(profileResp.Body.Bytes(), &profilePayload); err != nil {
		t.Fatalf("decode profile response: %v", err)
	}
	if _, err := ctx.admins.Authenticate(context.Background(), profilePayload.AccessToken); err != nil {
		t.Fatalf("default tenant profile token should authenticate: %v", err)
	}

	passwordResp := doHubAdminJSONRequest(t, ctx.handler, http.MethodPost, "/api/admin/password", map[string]any{
		"current_password": "StrongPassword123!",
		"new_password":     "NewStrongPassword123!",
	}, profilePayload.AccessToken)
	if passwordResp.Code != http.StatusOK {
		t.Fatalf("default tenant password status=%d body=%s", passwordResp.Code, passwordResp.Body.String())
	}
	if body := passwordResp.Body.String(); !containsAll(body, `"scope":"tenant"`, `"tenant_id":"`+store.DefaultTenantID+`"`) {
		t.Fatalf("expected default tenant password projection, body=%s", body)
	}
	loginNewResp := doHubAdminJSONRequest(t, ctx.handler, http.MethodPost, "/api/admin/login", map[string]any{
		"username": "admin",
		"password": "NewStrongPassword123!",
		"tenant":   store.DefaultTenantID,
	}, "")
	if loginNewResp.Code != http.StatusOK {
		t.Fatalf("default tenant new password login status=%d body=%s", loginNewResp.Code, loginNewResp.Body.String())
	}
}

func containsAll(body string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(body, part) {
			return false
		}
	}
	return true
}

func TestGetCenterStatusHandlerClearsStalePendingConfirmation(t *testing.T) {
	centerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/client/quality" {
			writeJSON(w, http.StatusOK, map[string]any{
				"routable":       true,
				"quality_score":  100,
				"service_status": "ok",
			})
			return
		}
		if r.URL.Path != "/api/hubs/hub_pending_removed/heartbeat" {
			http.NotFound(w, r)
			return
		}
		http.Error(w, `{"code":"HUB_UNREGISTERED","message":"Hub is not registered"}`, http.StatusUnauthorized)
	}))
	defer centerServer.Close()

	services := newAdminRouterTestContext(t)
	token := issueHubAdminToken(t, services.handler)
	if err := services.store.System.Set(context.Background(), "center_base_url", `{"value":"`+centerServer.URL+`"}`); err != nil {
		t.Fatalf("set center base url: %v", err)
	}
	if err := services.store.System.Set(context.Background(), "center_registration", `{"registered":false,"pending_confirmation":true,"disabled":false,"hub_id":"hub_pending_removed","hub_secret":"secret_pending_removed","last_base_url":"`+centerServer.URL+`","last_error":"waiting for confirmation","last_registered_at":1714032000}`); err != nil {
		t.Fatalf("set center registration: %v", err)
	}

	resp := doHubAdminJSONRequest(t, services.handler, http.MethodGet, "/api/admin/center/status", nil, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	if !containsAll(body,
		`"registered":false`,
		`"pending_confirmation":false`,
		`"last_error":"hub registration was removed by Hub Center"`,
	) {
		t.Fatalf("unexpected center status body=%s", body)
	}
	if strings.Contains(body, `"hub_id":"hub_pending_removed"`) || strings.Contains(body, `"active_base_url":"`) || strings.Contains(body, `"last_registered_at":`) {
		t.Fatalf("expected stale registration fields to be cleared, body=%s", body)
	}
}

func TestGetCenterStatusHandlerSelectsTenantAuthorizationForGlobalAdmin(t *testing.T) {
	services := newAdminRouterTestContext(t)
	token := issueHubAdminToken(t, services.handler)
	now := time.Now().UTC()
	authA := corelib.NormalizeDigitalEmployeeAuthorization(corelib.DigitalEmployeeAuthorization{Enabled: true, Quota: 2, ExpiresAt: now.AddDate(1, 0, 0).Format(time.RFC3339)}, now)
	authB := corelib.NormalizeDigitalEmployeeAuthorization(corelib.DigitalEmployeeAuthorization{Enabled: true, Quota: 7, ExpiresAt: now.AddDate(2, 0, 0).Format(time.RFC3339)}, now)
	record, err := json.Marshal(map[string]any{
		"registered": true,
		"hub_id":     "hub_tenant_auth",
		"hub_secret": "secret_tenant_auth",
		"digital_employee_authorizations": map[string]any{
			"tenant_a": authA,
			"tenant_b": authB,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := services.store.System.Set(context.Background(), "center_registration", string(record)); err != nil {
		t.Fatalf("set center registration: %v", err)
	}

	resp := doHubAdminJSONRequest(t, services.handler, http.MethodGet, "/api/admin/center/status?tenant_id=tenant_b", nil, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	var payload struct {
		DigitalEmployeeAuthorization struct {
			Quota  int  `json:"quota"`
			Active bool `json:"active"`
		} `json:"digital_employee_authorization"`
		DigitalEmployeeAuthorizations map[string]any `json:"digital_employee_authorizations"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.DigitalEmployeeAuthorization.Quota != 7 || !payload.DigitalEmployeeAuthorization.Active {
		t.Fatalf("expected selected tenant_b auth, got %#v body=%s", payload.DigitalEmployeeAuthorization, resp.Body.String())
	}
	if len(payload.DigitalEmployeeAuthorizations) != 2 {
		t.Fatalf("global admin should still see tenant authorization map, got %#v", payload.DigitalEmployeeAuthorizations)
	}
}

func TestAdminDiagnoseLLMServiceReturnsBillingRoutes(t *testing.T) {
	services := newAdminRouterTestContext(t)
	token := issueTenantAdminTokenForTest(t, services, "diag")
	ctx := context.Background()
	now := time.Now().UTC()
	system := scopedSystemSettingsForTenant("tenant_diag", services.store.System)

	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID:           "grant-group",
			Name:         "Grant Group",
			AccessPolicy: llmservice.AccessPolicyGrantRequired,
			Models: []llmservice.ModelServiceModel{{
				Name:        "auto",
				ProviderIDs: []string{"provider-a"},
			}},
		}},
		UserBindings: []llmservice.UserBinding{{
			Email:           "diag@example.com",
			ServiceGroupIDs: []string{"grant-group"},
		}},
		Grants: []llmservice.Grant{{
			ID:             "grant-1",
			Email:          "diag@example.com",
			ServiceGroupID: "grant-group",
			Source:         "card",
			CardID:         "card-1",
			StartsAt:       now.Add(-time.Hour),
			ExpiresAt:      now.Add(24 * time.Hour),
			CreditsTotal:   10,
			CreditsUsed:    4,
		}},
		Cards: []llmservice.RechargeCard{{
			ID:              "card-1",
			Label:           "April Grant",
			ServiceGroupIDs: []string{"grant-group"},
			DurationDays:    30,
			CreatedAt:       now.Add(-2 * time.Hour),
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{Providers: []im.LLMProvider{{
		ID:     "provider-a",
		Name:   "Provider A",
		Model:  "test-model",
		APIURL: "http://provider-a.local",
	}}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/services/diagnose?email=diag@example.com", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	services.handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	routes, ok := payload["billing_routes"].([]any)
	if !ok || len(routes) != 1 {
		t.Fatalf("expected 1 billing route, got %#v", payload["billing_routes"])
	}
	route, ok := routes[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected route payload: %#v", routes[0])
	}
	if route["model_name"] != "auto" || route["provider_id"] != "provider-a" {
		t.Fatalf("unexpected route identity: %#v", route)
	}
	if route["access_policy"] != llmservice.AccessPolicyGrantRequired {
		t.Fatalf("unexpected access policy: %#v", route)
	}
	if route["eligible"] != true {
		t.Fatalf("expected eligible route, got %#v", route)
	}
	if route["credits_available"] != float64(6) {
		t.Fatalf("expected credits_available=6, got %#v", route["credits_available"])
	}
}

func TestAdminGenerateLLMProviderTestKey(t *testing.T) {
	services := newAdminRouterTestContext(t)
	token := issueTenantAdminTokenForTest(t, services, "key")

	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/providers/test-key", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	services.handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["email"] != "key-owner@example.com" {
		t.Fatalf("expected key-owner@example.com, got %#v", payload["email"])
	}
	if payload["token_type"] != "Bearer" {
		t.Fatalf("expected Bearer token type, got %#v", payload["token_type"])
	}
	accessToken, _ := payload["access_token"].(string)
	if strings.TrimSpace(accessToken) == "" {
		t.Fatalf("expected access token, got %#v", payload["access_token"])
	}
	if payload["auth_header"] != "Bearer "+accessToken {
		t.Fatalf("unexpected auth header: %#v", payload["auth_header"])
	}
	if payload["expires_in_days"] != float64(30) {
		t.Fatalf("expected expires_in_days=30, got %#v", payload["expires_in_days"])
	}
}
