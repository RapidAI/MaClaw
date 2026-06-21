package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
)

func TestGetLLMServiceAccountHandlerReturnsStatusAndUsage(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "account@example.com")
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	now := time.Now().UTC()

	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic"}},
		UserBindings:       []llmservice.UserBinding{{Email: "account@example.com", ServiceGroupIDs: []string{"coding-basic"}}},
		Grants: []llmservice.Grant{{
			ID: "grant-1", Email: "account@example.com", ServiceGroupID: "coding-basic",
			Source: "card", StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), CreatedAt: now,
			CreditsTotal: 120, CreditsUsed: 25,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	rep := &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}
	rep.addUsage(now, "account@example.com", nil, corelib.TokenUsageStat{InputTokens: 10, OutputTokens: 20, TotalTokens: 30, TotalCostRMB: 0.42, Requests: 1}, 0.003)
	if err := saveLLMUsageReports(ctx, system, rep); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/llm/service/account", nil)
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	rec := httptest.NewRecorder()
	GetLLMServiceAccountHandler(identity, system, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp llmServiceAccountResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Email != "account@example.com" || resp.TenantID == "" || resp.Status == nil || resp.Status.CreditsAvailable != 95 {
		t.Fatalf("unexpected account response: %+v", resp)
	}
	if resp.Usage.TotalTokens != 30 || resp.Usage.TotalCostRMB != 0.42 {
		t.Fatalf("unexpected usage: %+v", resp.Usage)
	}
}

func TestGetLLMServiceAccountHandlerIncludesHubCenterAuthorizationCardUsage(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken := issueViewerTokenForTenant(t, identity, "tenant_acme", "account@example.com")
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	tenantSystem := scopedSystemSettingsForTenant("tenant_acme", system)
	now := time.Now().UTC()

	if err := llmservice.SaveRegistry(ctx, tenantSystem, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "maclaw_official_group", Name: "MaClaw Official"}},
	}); err != nil {
		t.Fatal(err)
	}
	accessCtrl := llmservice.NewTenantLLMAccessControl(nil)
	accessCtrl.UpdateFromHeartbeat("tenant_acme", &llmservice.TenantAuthorizationStatus{
		HubID:    "hub-1",
		TenantID: "tenant_acme",
		Authorizations: []llmservice.AuthorizationSummary{{
			ID:               "auth-card-1",
			ServiceGroupID:   "maclaw_official_group",
			CreditsTotal:     1000,
			CreditsUsed:      123.45,
			CreditsRemaining: 876.55,
			StartsAt:         now.Add(-time.Hour).Format(time.RFC3339),
			ExpiresAt:        now.Add(24 * time.Hour).Format(time.RFC3339),
			Status:           "active",
			Active:           true,
			Source:           "hubcenter_compute",
			CardOrderID:      "HC-ORDER-1",
		}},
	})
	previous := GetMaClawModule()
	defer SetMaClawModule(previous)
	SetMaClawModule(&llmservice.MaClawModule{AccessCtrl: accessCtrl})

	req := httptest.NewRequest(http.MethodGet, "/api/llm/service/account", nil)
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	rec := httptest.NewRecorder()
	GetLLMServiceAccountHandler(identity, system, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp llmServiceAccountResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Status == nil || len(resp.Status.CreditGrants) != 1 {
		t.Fatalf("credit grants = %+v", resp.Status)
	}
	grant := resp.Status.CreditGrants[0]
	if grant.ID != "auth-card-1" || grant.CardOrderID != "HC-ORDER-1" {
		t.Fatalf("grant identity = id:%q order:%q, want auth-card-1/HC-ORDER-1", grant.ID, grant.CardOrderID)
	}
	if grant.CreditsUsed != 123.45 || grant.CreditsRemaining != 876.55 {
		t.Fatalf("grant credits = used %.2f remaining %.2f", grant.CreditsUsed, grant.CreditsRemaining)
	}
	if resp.Status.CreditsUsed != 123.45 || resp.Status.CreditsRemaining != 876.55 {
		t.Fatalf("status credits = used %.2f remaining %.2f", resp.Status.CreditsUsed, resp.Status.CreditsRemaining)
	}
}

func TestGetLLMServiceAccountHandlerKeepsPeriodLimitedGrantVisible(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "limited-account@example.com")
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	now := time.Now().UTC()
	fiveHourWindowStart := time.Unix((now.Unix()/int64((5*time.Hour)/time.Second))*int64((5*time.Hour)/time.Second), 0).UTC()

	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic"}},
		UserBindings:       []llmservice.UserBinding{{Email: "limited-account@example.com", ServiceGroupIDs: []string{"coding-basic"}}},
		Grants: []llmservice.Grant{{
			ID: "grant-limited", Email: "limited-account@example.com", ServiceGroupID: "coding-basic",
			Source: "card", StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(24 * time.Hour), CreatedAt: now,
			CreditsTotal: 100, CreditsUsed: 10,
			PeriodLimits: llmservice.CreditPeriodLimits{FiveHour: 10},
			PeriodUsage:  llmservice.CreditPeriodUsage{FiveHour: llmservice.GrantUsageWindow{WindowStart: fiveHourWindowStart, CreditsUsed: 10}},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/llm/service/account", nil)
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	rec := httptest.NewRecorder()
	GetLLMServiceAccountHandler(identity, system, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp llmServiceAccountResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Status == nil || resp.Status.Active {
		t.Fatalf("expected inactive period-limited status, got %+v", resp.Status)
	}
	if len(resp.Status.ActiveGrants) != 0 {
		t.Fatalf("period-limited grant should not be exposed as active_grants: %+v", resp.Status.ActiveGrants)
	}
	if len(resp.Status.CreditGrants) != 1 || resp.Status.CreditGrants[0].Status != "period_limited" {
		t.Fatalf("expected visible period-limited credit grant, got %+v", resp.Status.CreditGrants)
	}
	if resp.Status.CreditGrants[0].CreditsRemaining <= 0 || resp.Status.CreditGrants[0].RetryAfterSeconds <= 0 {
		t.Fatalf("expected remaining total credits and retry metadata, got %+v", resp.Status.CreditGrants[0])
	}
}

func TestGetLLMServiceAccountHandlerKeepsExpiredGrantVisible(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "expired-account@example.com")
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	now := time.Now().UTC()

	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic"}},
		UserBindings:       []llmservice.UserBinding{{Email: "expired-account@example.com", ServiceGroupIDs: []string{"coding-basic"}}},
		Grants: []llmservice.Grant{{
			ID: "grant-expired", Email: "expired-account@example.com", ServiceGroupID: "coding-basic",
			Source: "card", StartsAt: now.Add(-48 * time.Hour), ExpiresAt: now.Add(-time.Hour), CreatedAt: now.Add(-48 * time.Hour),
			CreditsTotal: 100, CreditsUsed: 10,
		}},
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/llm/service/account", nil)
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	rec := httptest.NewRecorder()
	GetLLMServiceAccountHandler(identity, system, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp llmServiceAccountResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Status == nil || resp.Status.Active {
		t.Fatalf("expected inactive expired status, got %+v", resp.Status)
	}
	if resp.Status.CreditsAvailable != 0 {
		t.Fatalf("expired grant should expose zero currently available credits, got %v", resp.Status.CreditsAvailable)
	}
	if len(resp.Status.ActiveGrants) != 0 {
		t.Fatalf("expired grant should not be exposed as active_grants: %+v", resp.Status.ActiveGrants)
	}
	if len(resp.Status.CreditGrants) != 1 || resp.Status.CreditGrants[0].Status != "expired" {
		t.Fatalf("expected visible expired credit grant, got %+v", resp.Status.CreditGrants)
	}
	if len(resp.Status.InactiveReasons) == 0 || resp.Status.InactiveReasons[0] != "grant has expired" {
		t.Fatalf("expected expired inactive reason, got %+v", resp.Status.InactiveReasons)
	}
}
