package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
	if body := rec.Body.String(); !containsAll(body, "lookup@example.com", "\"id\":", "\"sn\":") {
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
	if body := listResp.Body.String(); !containsAll(body, "listed@example.com", "\"sn\":", "\"enrollment_status\":\"approved\"") {
		t.Fatalf("unexpected body=%s", body)
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

func TestInviteHandlersPersistEntries(t *testing.T) {
	router, _ := newAdminRouterTestServices(t)
	token := issueHubAdminToken(t, router)

	addResp := doHubAdminJSONRequest(t, router, http.MethodPost, "/api/admin/invites", map[string]any{
		"email": "invite@example.com",
		"role":  "member",
	}, token)
	if addResp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", addResp.Code, addResp.Body.String())
	}

	listResp := doHubAdminJSONRequest(t, router, http.MethodGet, "/api/admin/invites", nil, token)
	if listResp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", listResp.Code, listResp.Body.String())
	}
	if body := listResp.Body.String(); body == "" || !containsAll(body, "invite@example.com", "member") {
		t.Fatalf("unexpected body=%s", body)
	}
}

func TestCenterConfigAndRegisterHandlers(t *testing.T) {
	var capturedRegisterBody map[string]any
	centerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		"base_url":        centerServer.URL,
		"public_base_url": "https://hub.example.com",
		"enrollment_mode": "manual",
	}, token)
	if saveResp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", saveResp.Code, saveResp.Body.String())
	}

	statusResp := doHubAdminJSONRequest(t, router, http.MethodGet, "/api/admin/center/status", nil, token)
	if statusResp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", statusResp.Code, statusResp.Body.String())
	}
	if body := statusResp.Body.String(); !containsAll(body, centerServer.URL, `"public_base_url":"https://hub.example.com"`, `"registered":false`, `"host":"`, `"port":`, `"register_on_startup":true`, `"admin_email_present":true`, `"enrollment_mode":"manual"`) {
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

func containsAll(body string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(body, part) {
			return false
		}
	}
	return true
}
