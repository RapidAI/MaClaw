package httpapi

import (
	"context"
	"encoding/json"
	"math"
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
	// The service-account summary is sourced from the same audited usage report
	// as the admin page. Mark this fixture as a frozen pricing snapshot so the
	// RMB reference amount is intentionally available to the account holder.
	rep.addUsageWithCreditBreakdown(now, "account@example.com", nil, corelib.TokenUsageStat{InputTokens: 10, OutputTokens: 20, TotalTokens: 30, InputCostRMB: 0.14, OutputCostRMB: 0.28, TotalCostRMB: 0.42, Requests: 1}, 0.003, &llmUsageCreditBreakdown{RMBPricingRecorded: true})
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
	if resp.Usage.TotalTokens != 30 || math.Abs(resp.Usage.TotalCostRMB-0.42) > 1e-12 {
		t.Fatalf("unexpected usage: %+v", resp.Usage)
	}
}

func TestGetLLMServiceAccountHandlerExcludesHubCenterAuthorizationCardUsage(t *testing.T) {
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
	if resp.Status == nil {
		t.Fatalf("credit grants = %+v", resp.Status)
	}
	if len(resp.Status.CreditGrants) != 0 || len(resp.Status.ActiveGrants) != 0 {
		t.Fatalf("HubCenter tenant grants should not appear in personal service account status: %+v", resp.Status)
	}
	if resp.Status.CreditsUsed != 0 || resp.Status.CreditsRemaining != 0 || resp.Status.CreditsAvailable != 0 {
		t.Fatalf("HubCenter tenant credits should not be counted as personal credits: %+v", resp.Status)
	}
	if resp.Status.Active || resp.Status.SkipLLMConfig {
		t.Fatalf("HubCenter tenant cards should not activate personal account status: %+v", resp.Status)
	}
}

func TestGetLLMServiceStatusHandlerExcludesHubCenterAuthorizationCardUsage(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken := issueViewerTokenForTenant(t, identity, "tenant_acme", "status@example.com")
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

	req := httptest.NewRequest(http.MethodGet, "/api/llm/service/status", nil)
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	rec := httptest.NewRecorder()
	GetLLMServiceStatusHandler(identity, system, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var status llmservice.ServiceStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if len(status.CreditGrants) != 0 || len(status.ActiveGrants) != 0 {
		t.Fatalf("HubCenter tenant grants should not appear in personal service status: %+v", status)
	}
	if status.CreditsUsed != 0 || status.CreditsRemaining != 0 || status.CreditsAvailable != 0 {
		t.Fatalf("HubCenter tenant credits should not be counted as personal status credits: %+v", status)
	}
	if status.Active || status.SkipLLMConfig {
		t.Fatalf("HubCenter tenant cards should not activate personal service status: %+v", status)
	}
}

func TestGetLLMServiceAccountHandlerExcludesQueuedHubCenterCard(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken := issueViewerTokenForTenant(t, identity, "tenant_acme", "queued@example.com")
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
			ID:               "auth-future",
			ServiceGroupID:   "maclaw_official_group",
			CreditsTotal:     1000,
			CreditsUsed:      0,
			CreditsRemaining: 1000,
			StartsAt:         now.Add(24 * time.Hour).Format(time.RFC3339),
			ExpiresAt:        now.Add(48 * time.Hour).Format(time.RFC3339),
			Status:           "active",
			Active:           false,
			Source:           "hubcenter_compute",
			CardOrderID:      "HC-FUTURE",
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
	if resp.Status == nil {
		t.Fatalf("credit grants = %+v", resp.Status)
	}
	if len(resp.Status.CreditGrants) != 0 || len(resp.Status.ActiveGrants) != 0 {
		t.Fatalf("queued HubCenter tenant card should not appear in personal status: %+v", resp.Status)
	}
	if resp.Status.Active || resp.Status.CreditsRemaining != 0 || resp.Status.CreditsAvailable != 0 {
		t.Fatalf("queued HubCenter tenant card should not activate current service or balance: %+v", resp.Status)
	}
}

func TestGetLLMServiceAccountHandlerExcludesPeriodLimitedHubCenterCard(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken := issueViewerTokenForTenant(t, identity, "tenant_acme", "limited@example.com")
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
			ID:               "auth-period-limited",
			ServiceGroupID:   "maclaw_official_group",
			CreditsTotal:     1000,
			CreditsUsed:      100,
			CreditsRemaining: 900,
			StartsAt:         now.Add(-time.Hour).Format(time.RFC3339),
			ExpiresAt:        now.Add(24 * time.Hour).Format(time.RFC3339),
			Status:           "period_limited",
			Active:           false,
			Source:           "hubcenter_compute",
			CardOrderID:      "HC-LIMITED",
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
	if resp.Status == nil {
		t.Fatalf("credit grants = %+v", resp.Status)
	}
	if len(resp.Status.CreditGrants) != 0 || len(resp.Status.ActiveGrants) != 0 {
		t.Fatalf("period-limited HubCenter tenant card should not appear in personal status: %+v", resp.Status)
	}
	if resp.Status.Active || resp.Status.CreditsRemaining != 0 || resp.Status.CreditsAvailable != 0 {
		t.Fatalf("period-limited HubCenter tenant card should not affect personal balance: %+v", resp.Status)
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

func TestGetLLMServiceAccountHandlerBackfillsNewUserLimitCard(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "recharge@example.com")
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	now := time.Now().UTC()
	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups:        []llmservice.ModelServiceGroup{{ID: "redeem", Name: "充值服务组", AccessPolicy: llmservice.AccessPolicyGrantRequired}},
		DefaultNewUserBenefitMode: llmservice.NewUserBenefitModeLimitCard,
		DefaultNewUserLimitCard:   llmservice.NewUserLimitCard{ServiceGroupIDs: []string{"redeem"}, PeriodLimits: llmservice.CreditPeriodLimits{FiveHour: 100, Daily: 200}},
		Grants: []llmservice.Grant{{
			ID: "paid-1", Email: "recharge@example.com", ServiceGroupID: "redeem", Source: "card",
			StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(24 * time.Hour), CreditsTotal: 50000, CreditsUsed: 1000,
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
	var welcome *llmservice.ActiveGrant
	for i := range resp.Status.CreditGrants {
		if resp.Status.CreditGrants[i].Source == "new_user_limit_card" {
			welcome = &resp.Status.CreditGrants[i]
			break
		}
	}
	if welcome == nil {
		t.Fatalf("expected auto-backfilled welcome card beside recharge grant, got %+v", resp.Status.CreditGrants)
	}
	if welcome.PeriodLimits == nil || welcome.PeriodLimits.FiveHour != 100 || welcome.PeriodLimits.Daily != 200 {
		t.Fatalf("welcome period limits = %#v", welcome.PeriodLimits)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/llm/service/account", nil)
	req2.Header.Set("Authorization", "Bearer "+viewerToken)
	rec = httptest.NewRecorder()
	GetLLMServiceAccountHandler(identity, system, nil).ServeHTTP(rec, req2)
	if rec.Code != http.StatusOK {
		t.Fatalf("second status = %d body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	welcomeCount := 0
	for _, grant := range resp.Status.CreditGrants {
		if grant.Source == "new_user_limit_card" {
			welcomeCount++
		}
	}
	if welcomeCount != 1 {
		t.Fatalf("second poll welcome cards = %d, want 1: %+v", welcomeCount, resp.Status.CreditGrants)
	}
}
