package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func TestEntryProbeHandlerReturnsBoundUser(t *testing.T) {
	services := newAdminRouterTestContext(t)
	router := services.handler
	token := issueHubAdminToken(t, router)

	bindResp := doHubAdminJSONRequest(t, router, http.MethodPost, "/api/admin/users/manual-bind", map[string]any{
		"email": "bound@example.com",
	}, token)
	if bindResp.Code != http.StatusOK {
		t.Fatalf("manual bind status = %d body=%s", bindResp.Code, bindResp.Body.String())
	}
	user, err := services.store.Users.GetByTenantEmail(context.Background(), store.DefaultTenantID, "bound@example.com")
	if err != nil {
		t.Fatalf("GetByTenantEmail: %v", err)
	}
	if user == nil {
		t.Fatal("bound user not found")
	}
	now := time.Now().UTC()
	if err := services.store.Users.UpsertIdentity(context.Background(), &store.UserIdentity{
		ID:         user.ID + "_phone",
		TenantID:   user.TenantID,
		UserID:     user.ID,
		Type:       "phone",
		Value:      "17090134628",
		Verified:   true,
		VerifiedAt: &now,
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("UpsertIdentity phone: %v", err)
	}

	resp := doHubAdminJSONRequest(t, router, http.MethodPost, "/api/entry/probe", map[string]any{
		"email": "bound@example.com",
	}, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("probe status = %d body=%s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	if !containsAll(body, "\"status\":\"bound\"", "\"phone_number\":\"17090134628\"", "\"bound\":true", "\"can_login\":true", "\"pwa_url\":\"http://127.0.0.1:8080/app?email=bound%40example.com", "entry=app", "autologin=1") {
		t.Fatalf("unexpected body=%s", body)
	}
}

func TestEntryProbeHandlerReturnsNotFoundForUnknownEmail(t *testing.T) {
	router, _ := newAdminRouterTestServices(t)

	resp := doHubAdminJSONRequest(t, router, http.MethodPost, "/api/entry/probe", map[string]any{
		"email": "missing@example.com",
	}, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("probe status = %d body=%s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	if !containsAll(body, "\"status\":\"not_found\"", "\"bound\":false", "\"can_login\":false", "\"enrollment_mode\":\"open\"") {
		t.Fatalf("unexpected body=%s", body)
	}
}

func TestEntryProbeHandlerRoutesPhoneNumberIdentity(t *testing.T) {
	router, _ := newAdminRouterTestServices(t)
	token := issueHubAdminToken(t, router)

	bindResp := doHubAdminJSONRequest(t, router, http.MethodPost, "/api/admin/users/manual-bind", map[string]any{
		"email": "phone:17000000000",
	}, token)
	if bindResp.Code != http.StatusOK {
		t.Fatalf("manual bind status = %d body=%s", bindResp.Code, bindResp.Body.String())
	}

	resp := doHubAdminJSONRequest(t, router, http.MethodPost, "/api/entry/probe", map[string]any{
		"phone_number": "170 0000 0000",
	}, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("probe status = %d body=%s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	if !containsAll(body, "\"email\":\"phone:17000000000\"", "\"status\":\"bound\"", "\"bound\":true", "\"can_login\":true", "email=phone%3A17000000000") {
		t.Fatalf("unexpected body=%s", body)
	}
}

func TestEntryProbeHandlerReturnsBlockedForBlockedEmail(t *testing.T) {
	router, _ := newAdminRouterTestServices(t)
	token := issueHubAdminToken(t, router)

	addResp := doHubAdminJSONRequest(t, router, http.MethodPost, "/api/admin/blocklist", map[string]any{
		"email":  "blocked@example.com",
		"reason": "spam",
	}, token)
	if addResp.Code != http.StatusOK {
		t.Fatalf("blocklist status = %d body=%s", addResp.Code, addResp.Body.String())
	}

	resp := doHubAdminJSONRequest(t, router, http.MethodPost, "/api/entry/probe", map[string]any{
		"email": "blocked@example.com",
	}, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("probe status = %d body=%s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	if !containsAll(body, "\"status\":\"blocked\"", "\"message\":\"Account is blocked\"") || strings.Contains(body, "\"pwa_url\"") {
		t.Fatalf("unexpected body=%s", body)
	}
}
