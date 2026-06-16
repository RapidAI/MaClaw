package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
)

func TestMaClawComputeStatusIncludesRegisteredHubContextForTenantAdmin(t *testing.T) {
	services := newAdminRouterTestContext(t)
	globalToken := issueHubAdminToken(t, services.handler)
	tenantToken := issueTenantAdminToken(t, services.handler, globalToken, "acme", "acme-admin")

	if err := services.store.System.Set(context.Background(), "center_registration", `{"registered":true,"hub_id":"hub_acme","hub_secret":"secret","last_base_url":"https://hubs.example.com"}`); err != nil {
		t.Fatalf("set center registration: %v", err)
	}

	resp := doHubAdminJSONRequest(t, services.handler, http.MethodGet, "/api/admin/llm/maclaw-compute-status", nil, tenantToken)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", resp.Code, resp.Body.String())
	}

	var payload struct {
		HubID         string `json:"hub_id"`
		CenterBaseURL string `json:"center_base_url"`
		TenantID      string `json:"tenant_id"`
		AdminEmail    string `json:"admin_email"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.HubID != "hub_acme" {
		t.Fatalf("hub_id = %q, want hub_acme", payload.HubID)
	}
	if payload.CenterBaseURL != "https://hubs.example.com" {
		t.Fatalf("center_base_url = %q", payload.CenterBaseURL)
	}
	if payload.TenantID != "tenant_acme" {
		t.Fatalf("tenant_id = %q, want tenant_acme", payload.TenantID)
	}
	if payload.AdminEmail != "acme-admin@example.com" {
		t.Fatalf("admin_email = %q, want acme-admin@example.com", payload.AdminEmail)
	}
}

func TestMaClawComputeStatusFallsBackToConfiguredCenterBaseURL(t *testing.T) {
	services := newAdminRouterTestContext(t)
	globalToken := issueHubAdminToken(t, services.handler)
	tenantToken := issueTenantAdminToken(t, services.handler, globalToken, "beta", "beta-admin")

	if err := services.store.System.Set(context.Background(), "center_base_url", `{"value":"https://hubs.example.com"}`); err != nil {
		t.Fatalf("set center base url: %v", err)
	}
	if err := services.store.System.Set(context.Background(), "center_registration", `{"registered":true,"hub_id":"hub_beta","hub_secret":"secret"}`); err != nil {
		t.Fatalf("set center registration: %v", err)
	}

	resp := doHubAdminJSONRequest(t, services.handler, http.MethodGet, "/api/admin/llm/maclaw-compute-status", nil, tenantToken)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", resp.Code, resp.Body.String())
	}

	var payload struct {
		HubID         string `json:"hub_id"`
		CenterBaseURL string `json:"center_base_url"`
		TenantID      string `json:"tenant_id"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.HubID != "hub_beta" {
		t.Fatalf("hub_id = %q, want hub_beta", payload.HubID)
	}
	if payload.CenterBaseURL != "https://hubs.example.com" {
		t.Fatalf("center_base_url = %q", payload.CenterBaseURL)
	}
	if payload.TenantID != "tenant_beta" {
		t.Fatalf("tenant_id = %q, want tenant_beta", payload.TenantID)
	}
}

func TestMaClawComputeStatusUsesCurrentAccessControlWhenHandlerCapturedNil(t *testing.T) {
	previous := GetMaClawModule()
	defer SetMaClawModule(previous)

	client := llmservice.NewMaClawProviderClient(llmservice.MaClawProviderConfig{
		HubCenterURL: "https://hubcenter.example.com",
		HubID:        "hub_dynamic",
		MachineToken: "secret_dynamic",
	})
	accessCtrl := llmservice.NewTenantLLMAccessControl(client)
	accessCtrl.UpdateFromHeartbeat("tenant_acme", &llmservice.TenantAuthorizationStatus{
		HubID:                  "hub_dynamic",
		TenantID:               "tenant_acme",
		AllowExternalProviders: true,
		Authorizations: []llmservice.AuthorizationSummary{{
			ID:               "auth_active",
			ServiceGroupID:   "maclaw_official_group",
			CreditsTotal:     100,
			CreditsUsed:      25,
			CreditsRemaining: 75,
			ExpiresAt:        "2099-01-01T00:00:00Z",
			Status:           "active",
			Active:           true,
		}},
	})
	SetMaClawModule(&llmservice.MaClawModule{
		Client:     client,
		AccessCtrl: accessCtrl,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/maclaw-compute-status?tenant_id=tenant_acme", nil)
	rec := httptest.NewRecorder()
	MaClawComputeStatusHandler(nil, nil)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		AllowExternalProviders bool `json:"allow_external_providers"`
		Authorizations         []struct {
			ID               string  `json:"id"`
			CreditsRemaining float64 `json:"credits_remaining"`
			Active           bool    `json:"active"`
		} `json:"authorizations"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.AllowExternalProviders {
		t.Fatalf("allow_external_providers = false, want true")
	}
	if len(payload.Authorizations) != 1 || payload.Authorizations[0].ID != "auth_active" || payload.Authorizations[0].CreditsRemaining != 75 || !payload.Authorizations[0].Active {
		t.Fatalf("authorizations = %#v, want active auth summary", payload.Authorizations)
	}
}
