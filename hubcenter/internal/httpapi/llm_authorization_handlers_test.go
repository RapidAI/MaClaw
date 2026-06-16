package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

func mustParseTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parse time %q: %v", value, err)
	}
	return parsed
}

func testHashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func TestAdminCreateLLMAuthorizationAllowsExternalComputeWithoutServiceGroup(t *testing.T) {
	repo := &llmDeleteAuthRepo{}
	checker := llmservice.NewAuthorizationChecker(repo)
	body := bytes.NewReader([]byte(`{
		"id":"auth_external_hub1_tenant1",
		"hub_id":"hub1",
		"tenant_id":"tenant1",
		"allow_external_providers":true
	}`))
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/authorizations", body)
	rr := httptest.NewRecorder()

	adminCreateLLMAuthorization(checker).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if len(repo.auths) != 1 {
		t.Fatalf("auth count = %d", len(repo.auths))
	}
	if repo.auths[0].ServiceGroupID != llmservice.ExternalComputePermissionServiceGroupID {
		t.Fatalf("service_group_id = %q", repo.auths[0].ServiceGroupID)
	}
	if repo.auths[0].CreditsTotal <= 0 {
		t.Fatalf("credits_total = %v, want compatibility default", repo.auths[0].CreditsTotal)
	}
	if repo.auths[0].StartsAt.IsZero() || repo.auths[0].ExpiresAt.IsZero() {
		t.Fatalf("starts/expires should be defaulted: %#v", repo.auths[0])
	}
	if repo.auths[0].Status != "active" || repo.auths[0].Source != "external_provider_permission" {
		t.Fatalf("status/source = %q/%q", repo.auths[0].Status, repo.auths[0].Source)
	}
	ok, err := checker.HasExternalProviderAccess(context.Background(), "hub1", "tenant1")
	if err != nil || !ok {
		t.Fatalf("external provider access = %v err=%v", ok, err)
	}
}

func TestAdminCreateLLMAuthorizationStillRequiresServiceGroupForCreditGrant(t *testing.T) {
	checker := llmservice.NewAuthorizationChecker(&llmDeleteAuthRepo{})
	payload, _ := json.Marshal(map[string]any{
		"hub_id":        "hub1",
		"tenant_id":     "tenant1",
		"credits_total": 100,
		"starts_at":     "2026-01-01T00:00:00Z",
		"expires_at":    "2099-12-31T23:59:59Z",
		"status":        "active",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/authorizations", bytes.NewReader(payload))
	rr := httptest.NewRecorder()

	adminCreateLLMAuthorization(checker).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", rr.Code, rr.Body.String())
	}
}

func TestAdminCreateLLMAuthorizationDisablesPreviousExternalComputeGrant(t *testing.T) {
	repo := &llmDeleteAuthRepo{auths: []*llmservice.TenantAuthorization{{
		ID:                     "old_external",
		HubID:                  "hub1",
		TenantID:               "tenant1",
		ServiceGroupID:         llmservice.ExternalComputePermissionServiceGroupID,
		AllowExternalProviders: true,
		CreditsTotal:           1000000000000,
		StartsAt:               mustParseTime(t, "2026-01-01T00:00:00Z"),
		ExpiresAt:              mustParseTime(t, "2099-12-31T23:59:59Z"),
		Status:                 "active",
		Source:                 "external_provider_permission",
	}}}
	checker := llmservice.NewAuthorizationChecker(repo)
	body := bytes.NewReader([]byte(`{
		"id":"new_external",
		"hub_id":"hub1",
		"tenant_id":"tenant1",
		"allow_external_providers":false,
		"credits_total":1000000000000,
		"starts_at":"2026-01-01T00:00:00Z",
		"expires_at":"2099-12-31T23:59:59Z",
		"status":"active",
		"source":"external_provider_permission"
	}`))
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/authorizations", body)
	rr := httptest.NewRecorder()

	adminCreateLLMAuthorization(checker).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if repo.auths[0].AllowExternalProviders {
		t.Fatalf("old external grant still allows external providers")
	}
	if repo.auths[0].Status != "expired" {
		t.Fatalf("old external grant status = %q, want expired", repo.auths[0].Status)
	}
	ok, err := checker.HasExternalProviderAccess(context.Background(), "hub1", "tenant1")
	if err != nil {
		t.Fatalf("external provider access err=%v", err)
	}
	if ok {
		t.Fatalf("external provider access still enabled")
	}
}

func TestExternalComputeCardGrantAllowsLegacyRowsWithoutFlag(t *testing.T) {
	now := time.Now().UTC()
	checker := llmservice.NewAuthorizationChecker(&llmDeleteAuthRepo{auths: []*llmservice.TenantAuthorization{{
		ID:             "legacy_card_external",
		HubID:          "hub1",
		TenantID:       "tenant1",
		ServiceGroupID: llmservice.ExternalComputePermissionServiceGroupID,
		CreditsTotal:   1000000000000,
		StartsAt:       now.Add(-time.Hour),
		ExpiresAt:      now.Add(time.Hour),
		Status:         "active",
		Source:         "card",
	}}})

	ok, err := checker.HasExternalProviderAccess(context.Background(), "hub1", "tenant1")
	if err != nil {
		t.Fatalf("external provider access err=%v", err)
	}
	if !ok {
		t.Fatalf("legacy card external grant should allow external providers")
	}
}

func TestExternalComputeAdminGrantCanExplicitlyDisableAccess(t *testing.T) {
	now := time.Now().UTC()
	checker := llmservice.NewAuthorizationChecker(&llmDeleteAuthRepo{auths: []*llmservice.TenantAuthorization{{
		ID:             "admin_external_disabled",
		HubID:          "hub1",
		TenantID:       "tenant1",
		ServiceGroupID: llmservice.ExternalComputePermissionServiceGroupID,
		CreditsTotal:   1000000000000,
		StartsAt:       now.Add(-time.Hour),
		ExpiresAt:      now.Add(time.Hour),
		Status:         "active",
		Source:         "external_provider_permission",
	}}})

	ok, err := checker.HasExternalProviderAccess(context.Background(), "hub1", "tenant1")
	if err != nil {
		t.Fatalf("external provider access err=%v", err)
	}
	if ok {
		t.Fatalf("explicit disabled admin grant should not allow external providers")
	}
}

func TestAdminCreateLLMAuthorizationKeepsPreviousGrantWhenCreateFails(t *testing.T) {
	createErr := errors.New("create failed")
	repo := &llmDeleteAuthRepo{
		createErr: createErr,
		auths: []*llmservice.TenantAuthorization{{
			ID:                     "old_external",
			HubID:                  "hub1",
			TenantID:               "tenant1",
			ServiceGroupID:         llmservice.ExternalComputePermissionServiceGroupID,
			AllowExternalProviders: true,
			CreditsTotal:           1000000000000,
			StartsAt:               mustParseTime(t, "2026-01-01T00:00:00Z"),
			ExpiresAt:              mustParseTime(t, "2099-12-31T23:59:59Z"),
			Status:                 "active",
			Source:                 "external_provider_permission",
		}},
	}
	checker := llmservice.NewAuthorizationChecker(repo)
	body := bytes.NewReader([]byte(`{
		"id":"new_external",
		"hub_id":"hub1",
		"tenant_id":"tenant1",
		"allow_external_providers":false,
		"credits_total":1000000000000,
		"starts_at":"2026-01-01T00:00:00Z",
		"expires_at":"2099-12-31T23:59:59Z",
		"status":"active",
		"source":"external_provider_permission"
	}`))
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/authorizations", body)
	rr := httptest.NewRecorder()

	adminCreateLLMAuthorization(checker).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", rr.Code, rr.Body.String())
	}
	if !repo.auths[0].AllowExternalProviders {
		t.Fatalf("old external grant was disabled after create failure")
	}
	if repo.auths[0].Status != "active" {
		t.Fatalf("old external grant status = %q, want active", repo.auths[0].Status)
	}
}

func TestLLMAuthorizationQueryRequiresHubMachineAuth(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	ctx := context.Background()
	secret := "hub-secret"
	now := time.Now().UTC()
	if err := svc.store.Hubs.Create(ctx, &store.HubInstance{
		ID:            "hub_secure",
		OwnerEmail:    "owner@example.com",
		Name:          "Secure Hub",
		BaseURL:       "https://hub.example.com",
		Status:        "online",
		HubSecretHash: testHashToken(secret),
		CreatedAt:     now,
		UpdatedAt:     now,
	}); err != nil {
		t.Fatalf("create hub: %v", err)
	}

	checker := llmservice.NewAuthorizationChecker(&llmDeleteAuthRepo{auths: []*llmservice.TenantAuthorization{{
		ID:                     "external",
		HubID:                  "hub_secure",
		TenantID:               "tenant_default",
		ServiceGroupID:         llmservice.ExternalComputePermissionServiceGroupID,
		CreditsTotal:           1000000000000,
		StartsAt:               now.Add(-time.Hour),
		ExpiresAt:              now.Add(time.Hour),
		Status:                 "active",
		AllowExternalProviders: false,
		Source:                 "external_provider_permission",
	}}})
	mux := http.NewServeMux()
	RegisterLLMRoutes(mux, nil, svc.hubs, llmservice.NewService(&llmDeleteTestSettings{}), nil, checker, nil, nil)

	noAuth := httptest.NewRequest(http.MethodGet, "/api/llm/v1/authorization?hub_id=hub_secure&tenant_id=tenant_default", nil)
	noAuthResp := httptest.NewRecorder()
	mux.ServeHTTP(noAuthResp, noAuth)
	if noAuthResp.Code != http.StatusUnauthorized {
		t.Fatalf("missing auth status = %d body=%s, want 401", noAuthResp.Code, noAuthResp.Body.String())
	}

	wrongAuth := httptest.NewRequest(http.MethodGet, "/api/llm/v1/authorization?hub_id=hub_secure&tenant_id=tenant_default", nil)
	wrongAuth.Header.Set("Authorization", "Bearer wrong")
	wrongAuth.Header.Set("X-Hub-ID", "hub_secure")
	wrongAuthResp := httptest.NewRecorder()
	mux.ServeHTTP(wrongAuthResp, wrongAuth)
	if wrongAuthResp.Code != http.StatusUnauthorized {
		t.Fatalf("wrong auth status = %d body=%s, want 401", wrongAuthResp.Code, wrongAuthResp.Body.String())
	}

	okReq := httptest.NewRequest(http.MethodGet, "/api/llm/v1/authorization?hub_id=hub_secure&tenant_id=tenant_default", nil)
	okReq.Header.Set("Authorization", "Bearer "+secret)
	okReq.Header.Set("X-Hub-ID", "hub_secure")
	okResp := httptest.NewRecorder()
	mux.ServeHTTP(okResp, okReq)
	if okResp.Code != http.StatusOK {
		t.Fatalf("authorized status = %d body=%s, want 200", okResp.Code, okResp.Body.String())
	}
	var payload struct {
		AllowExternalProviders bool `json:"allow_external_providers"`
	}
	if err := json.Unmarshal(okResp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode authorization response: %v", err)
	}
	if !payload.AllowExternalProviders {
		t.Fatalf("allow_external_providers = false, want true")
	}
}

func TestLLMAuthorizationBatchRequiresHubMachineAuth(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	ctx := context.Background()
	secret := "hub-batch-secret"
	now := time.Now().UTC()
	if err := svc.store.Hubs.Create(ctx, &store.HubInstance{
		ID:            "hub_batch",
		OwnerEmail:    "owner@example.com",
		Name:          "Batch Hub",
		BaseURL:       "https://hub.example.com",
		Status:        "online",
		HubSecretHash: testHashToken(secret),
		CreatedAt:     now,
		UpdatedAt:     now,
	}); err != nil {
		t.Fatalf("create hub: %v", err)
	}

	checker := llmservice.NewAuthorizationChecker(&llmDeleteAuthRepo{auths: []*llmservice.TenantAuthorization{{
		ID:                     "external_batch",
		HubID:                  "hub_batch",
		TenantID:               "tenant_a",
		ServiceGroupID:         llmservice.ExternalComputePermissionServiceGroupID,
		CreditsTotal:           1000000000000,
		StartsAt:               now.Add(-time.Hour),
		ExpiresAt:              now.Add(time.Hour),
		Status:                 "active",
		AllowExternalProviders: true,
		Source:                 "external_provider_permission",
	}}})
	mux := http.NewServeMux()
	RegisterLLMRoutes(mux, nil, svc.hubs, llmservice.NewService(&llmDeleteTestSettings{}), nil, checker, nil, nil)

	body := []byte(`{"tenant_ids":["tenant_a","tenant_a","","tenant_b"]}`)
	noAuth := httptest.NewRequest(http.MethodPost, "/api/llm/v1/authorization/batch", bytes.NewReader(body))
	noAuth.Header.Set("X-Hub-ID", "hub_batch")
	noAuthResp := httptest.NewRecorder()
	mux.ServeHTTP(noAuthResp, noAuth)
	if noAuthResp.Code != http.StatusUnauthorized {
		t.Fatalf("missing auth status = %d body=%s, want 401", noAuthResp.Code, noAuthResp.Body.String())
	}

	okReq := httptest.NewRequest(http.MethodPost, "/api/llm/v1/authorization/batch", bytes.NewReader(body))
	okReq.Header.Set("Authorization", "Bearer "+secret)
	okReq.Header.Set("X-Hub-ID", "hub_batch")
	okResp := httptest.NewRecorder()
	mux.ServeHTTP(okResp, okReq)
	if okResp.Code != http.StatusOK {
		t.Fatalf("authorized status = %d body=%s, want 200", okResp.Code, okResp.Body.String())
	}
	var payload struct {
		Tenants map[string]struct {
			HubID                  string `json:"hub_id"`
			TenantID               string `json:"tenant_id"`
			AllowExternalProviders bool   `json:"allow_external_providers"`
		} `json:"tenants"`
	}
	if err := json.Unmarshal(okResp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode batch authorization response: %v", err)
	}
	if len(payload.Tenants) != 2 {
		t.Fatalf("tenant count = %d payload=%#v, want 2", len(payload.Tenants), payload.Tenants)
	}
	if got := payload.Tenants["tenant_a"]; got.HubID != "hub_batch" || got.TenantID != "tenant_a" || !got.AllowExternalProviders {
		t.Fatalf("tenant_a status = %#v, want granted hub_batch/tenant_a", got)
	}
	if got := payload.Tenants["tenant_b"]; got.HubID != "hub_batch" || got.TenantID != "tenant_b" || got.AllowExternalProviders {
		t.Fatalf("tenant_b status = %#v, want ungranted hub_batch/tenant_b", got)
	}
}
