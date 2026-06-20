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

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func TestAdminCreateLLMAuthorizationAllowsExternalComputeWithoutServiceGroup(t *testing.T) {
	repo := &llmDeleteAuthRepo{}
	checker := llmservice.NewAuthorizationChecker(repo)
	body := bytes.NewReader([]byte(`{
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
	// Pure permission grant — no virtual credits needed.
	if repo.auths[0].CreditsTotal != 0 {
		t.Fatalf("credits_total = %v, want 0 (pure permission grant)", repo.auths[0].CreditsTotal)
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

func TestAdminListLLMAuthorizationsMarksExternalComputeAccess(t *testing.T) {
	repo := &llmDeleteAuthRepo{auths: []*llmservice.TenantAuthorization{{
		ID:                     "external_disabled",
		HubID:                  "hub1",
		TenantID:               "tenant1",
		ServiceGroupID:         llmservice.ExternalComputePermissionServiceGroupID,
		AllowExternalProviders: false,
		Status:                 "expired",
		Source:                 "external_provider_permission",
	}, {
		ID:             "credit_grant",
		HubID:          "hub1",
		TenantID:       "tenant1",
		ServiceGroupID: "group1",
		CreditsTotal:   100,
		CreditsUsed:    1.1,
		Status:         "active",
		Source:         "admin_grant",
	}}}
	checker := llmservice.NewAuthorizationChecker(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/authorizations", nil)
	rr := httptest.NewRecorder()

	adminListLLMAuthorizations(checker).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var payload struct {
		Authorizations []struct {
			ID                      string  `json:"id"`
			CreditsUsed             float64 `json:"credits_used"`
			IsExternalComputeAccess bool    `json:"is_external_compute_access"`
		} `json:"authorizations"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Authorizations) != 2 {
		t.Fatalf("authorization count = %d body=%s", len(payload.Authorizations), rr.Body.String())
	}
	if !payload.Authorizations[0].IsExternalComputeAccess {
		t.Fatalf("external compute authorization not marked: %+v", payload.Authorizations[0])
	}
	if payload.Authorizations[1].IsExternalComputeAccess {
		t.Fatalf("credit grant marked as external compute: %+v", payload.Authorizations[1])
	}
	if payload.Authorizations[1].CreditsUsed != 1.1 {
		t.Fatalf("credits_used = %.17g, want 1.1", payload.Authorizations[1].CreditsUsed)
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
		"hub_id":"hub1",
		"tenant_id":"tenant1",
		"allow_external_providers":false
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

func TestAdminCreateLLMAuthorizationRevocationCreatesInactiveStateWhenMissingGrant(t *testing.T) {
	repo := &llmDeleteAuthRepo{}
	checker := llmservice.NewAuthorizationChecker(repo)
	body := bytes.NewReader([]byte(`{
		"hub_id":"hub1",
		"tenant_id":"tenant1",
		"allow_external_providers":false
	}`))
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/authorizations", body)
	rr := httptest.NewRecorder()

	adminCreateLLMAuthorization(checker).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if len(repo.auths) != 1 {
		t.Fatalf("auth count = %d, want revocation tombstone", len(repo.auths))
	}
	got := repo.auths[0]
	if got.AllowExternalProviders || got.Status != "expired" || got.ServiceGroupID != llmservice.ExternalComputePermissionServiceGroupID {
		t.Fatalf("revocation tombstone = %#v", got)
	}
	status, err := llmservice.BuildTenantAuthorizationStatus(context.Background(), checker, "hub1", "tenant1")
	if err != nil {
		t.Fatalf("BuildTenantAuthorizationStatus: %v", err)
	}
	if status.AllowExternalProviders || len(status.Authorizations) != 1 || status.Authorizations[0].Active {
		t.Fatalf("authorization status = %#v, want explicit inactive state", status)
	}
}

func TestAdminCreateLLMAuthorizationDisablesPreviousExternalComputeGrantTenantAlias(t *testing.T) {
	repo := &llmDeleteAuthRepo{auths: []*llmservice.TenantAuthorization{{
		ID:                     "old_external_alias",
		HubID:                  "hub1",
		TenantID:               "acme",
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
		"hub_id":"hub1",
		"tenant_id":"tenant_acme",
		"allow_external_providers":true
	}`))
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/authorizations", body)
	rr := httptest.NewRecorder()

	adminCreateLLMAuthorization(checker).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if len(repo.auths) != 2 {
		t.Fatalf("auth count = %d, want old plus new", len(repo.auths))
	}
	if repo.auths[0].AllowExternalProviders || repo.auths[0].Status != "expired" {
		t.Fatalf("old alias grant not expired: %#v", repo.auths[0])
	}
	if repo.auths[1].TenantID != "tenant_acme" || !repo.auths[1].AllowExternalProviders || repo.auths[1].Status != "active" {
		t.Fatalf("new tenant grant = %#v", repo.auths[1])
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
	// Sending allow_external_providers:false is now a revocation request.
	// It should expire the old grant and return 200 (revoked), regardless of createErr.
	body := bytes.NewReader([]byte(`{
		"hub_id":"hub1",
		"tenant_id":"tenant1",
		"allow_external_providers":false
	}`))
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/authorizations", body)
	rr := httptest.NewRecorder()

	adminCreateLLMAuthorization(checker).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200 (revocation)", rr.Code, rr.Body.String())
	}
	if repo.auths[0].AllowExternalProviders {
		t.Fatalf("old external grant should have AllowExternalProviders=false after revocation")
	}
	if repo.auths[0].Status != "expired" {
		t.Fatalf("old external grant status = %q, want expired", repo.auths[0].Status)
	}
	if !repo.auths[0].ExpiresAt.Before(mustParseTime(t, "2099-12-31T23:59:59Z")) {
		t.Fatalf("old external grant expires_at = %s, want revoked timestamp before original far-future expiry", repo.auths[0].ExpiresAt)
	}
}

func TestAdminCreateLLMAuthorizationUpdatesExistingExternalComputeGrant(t *testing.T) {
	id := "auth_admin_hub1_tenant1_" + llmservice.ExternalComputePermissionServiceGroupID
	createdAt := mustParseTime(t, "2026-01-01T00:00:00Z")
	repo := &llmDeleteAuthRepo{auths: []*llmservice.TenantAuthorization{{
		ID:                     id,
		HubID:                  "hub1",
		TenantID:               "tenant1",
		ServiceGroupID:         llmservice.ExternalComputePermissionServiceGroupID,
		AllowExternalProviders: false,
		CreditsTotal:           1,
		CreditsUsed:            1,
		StartsAt:               createdAt,
		ExpiresAt:              createdAt.Add(time.Hour),
		Status:                 "expired",
		Source:                 "external_provider_permission",
		CreatedAt:              createdAt,
		UpdatedAt:              createdAt,
	}}}
	checker := llmservice.NewAuthorizationChecker(repo)
	body := bytes.NewReader([]byte(`{
		"hub_id":" hub1 ",
		"tenant_id":" tenant1 ",
		"allow_external_providers":true
	}`))
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/authorizations", body)
	rr := httptest.NewRecorder()

	adminCreateLLMAuthorization(checker).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if len(repo.auths) != 1 {
		t.Fatalf("auth count = %d, want 1 updated row", len(repo.auths))
	}
	got := repo.auths[0]
	if !got.AllowExternalProviders || got.Status != "active" {
		t.Fatalf("updated auth = %#v, want active external compute grant", got)
	}
	if !got.CreatedAt.Equal(createdAt) {
		t.Fatalf("created_at = %s, want preserved %s", got.CreatedAt, createdAt)
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
	if payload.AllowExternalProviders {
		t.Fatalf("allow_external_providers = true, want false for disabled external compute grant")
	}
}

func TestLLMAuthorizationQueryAllowsActiveExternalComputeGrant(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	ctx := context.Background()
	secret := "hub-active-secret"
	now := time.Now().UTC()
	if err := svc.store.Hubs.Create(ctx, &store.HubInstance{
		ID:            "hub_active",
		OwnerEmail:    "owner@example.com",
		Name:          "Active Hub",
		BaseURL:       "https://hub.example.com",
		Status:        "online",
		HubSecretHash: testHashToken(secret),
		CreatedAt:     now,
		UpdatedAt:     now,
	}); err != nil {
		t.Fatalf("create hub: %v", err)
	}

	checker := llmservice.NewAuthorizationChecker(&llmDeleteAuthRepo{auths: []*llmservice.TenantAuthorization{{
		ID:                     "external_active",
		HubID:                  "hub_active",
		TenantID:               "tenant_active",
		ServiceGroupID:         llmservice.ExternalComputePermissionServiceGroupID,
		CreditsTotal:           0,
		StartsAt:               now.Add(-time.Hour),
		ExpiresAt:              now.Add(time.Hour),
		Status:                 "active",
		AllowExternalProviders: true,
		Source:                 "external_provider_permission",
	}}})
	mux := http.NewServeMux()
	RegisterLLMRoutes(mux, nil, svc.hubs, llmservice.NewService(&llmDeleteTestSettings{}), nil, checker, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/llm/v1/authorization?hub_id=hub_active&tenant_id=tenant_active", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("X-Hub-ID", "hub_active")
	resp := httptest.NewRecorder()
	mux.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", resp.Code, resp.Body.String())
	}
	var payload struct {
		HubID                  string `json:"hub_id"`
		TenantID               string `json:"tenant_id"`
		AllowExternalProviders bool   `json:"allow_external_providers"`
		Authorizations         []struct {
			ID               string  `json:"id"`
			CreditsRemaining float64 `json:"credits_remaining"`
			Active           bool    `json:"active"`
		} `json:"authorizations"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode authorization response: %v", err)
	}
	if payload.HubID != "hub_active" || payload.TenantID != "tenant_active" || !payload.AllowExternalProviders {
		t.Fatalf("authorization = %#v, want active external compute grant", payload)
	}
	// Pure permission records (external_provider_permission) should NOT appear
	// in the authorizations list — they are not credit-based quotas.
	if len(payload.Authorizations) != 0 {
		t.Fatalf("authorizations len = %d body=%s, want 0 (permission-only grant)", len(payload.Authorizations), resp.Body.String())
	}
}

func TestLLMAuthorizationQueryMatchesTenantIDAlias(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	ctx := context.Background()
	secret := "hub-alias-secret"
	now := time.Now().UTC()
	if err := svc.store.Hubs.Create(ctx, &store.HubInstance{
		ID:            "hub_alias",
		OwnerEmail:    "owner@example.com",
		Name:          "Alias Hub",
		BaseURL:       "https://hub.example.com",
		Status:        "online",
		HubSecretHash: testHashToken(secret),
		CreatedAt:     now,
		UpdatedAt:     now,
	}); err != nil {
		t.Fatalf("create hub: %v", err)
	}

	checker := llmservice.NewAuthorizationChecker(&llmDeleteAuthRepo{auths: []*llmservice.TenantAuthorization{{
		ID:                     "external_alias",
		HubID:                  "hub_alias",
		TenantID:               "acme",
		ServiceGroupID:         llmservice.ExternalComputePermissionServiceGroupID,
		CreditsTotal:           0,
		StartsAt:               now.Add(-time.Hour),
		ExpiresAt:              now.Add(time.Hour),
		Status:                 "active",
		AllowExternalProviders: true,
		Source:                 "external_provider_permission",
	}}})
	mux := http.NewServeMux()
	RegisterLLMRoutes(mux, nil, svc.hubs, llmservice.NewService(&llmDeleteTestSettings{}), nil, checker, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/llm/v1/authorization?hub_id=hub_alias&tenant_id=tenant_acme", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("X-Hub-ID", "hub_alias")
	resp := httptest.NewRecorder()
	mux.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", resp.Code, resp.Body.String())
	}
	var payload struct {
		TenantID               string `json:"tenant_id"`
		AllowExternalProviders bool   `json:"allow_external_providers"`
		Authorizations         []struct {
			ID string `json:"id"`
		} `json:"authorizations"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode authorization response: %v", err)
	}
	if payload.TenantID != "tenant_acme" || !payload.AllowExternalProviders || len(payload.Authorizations) != 0 {
		t.Fatalf("authorization alias response = %#v, want request tenant with active alias grant (no credit authorizations)", payload)
	}
}

func TestLLMAuthorizationQueryMatchesDefaultTenantStorageAlias(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	ctx := context.Background()
	secret := "hub-default-secret"
	now := time.Now().UTC()
	if err := svc.store.Hubs.Create(ctx, &store.HubInstance{
		ID:            "hub_default_alias",
		OwnerEmail:    "owner@example.com",
		Name:          "Default Alias Hub",
		BaseURL:       "https://hub.example.com",
		Status:        "online",
		HubSecretHash: testHashToken(secret),
		CreatedAt:     now,
		UpdatedAt:     now,
	}); err != nil {
		t.Fatalf("create hub: %v", err)
	}

	checker := llmservice.NewAuthorizationChecker(&llmDeleteAuthRepo{auths: []*llmservice.TenantAuthorization{{
		ID:                     "external_default_alias",
		HubID:                  "hub_default_alias",
		TenantID:               "",
		ServiceGroupID:         llmservice.ExternalComputePermissionServiceGroupID,
		CreditsTotal:           0,
		StartsAt:               now.Add(-time.Hour),
		ExpiresAt:              now.Add(time.Hour),
		Status:                 "active",
		AllowExternalProviders: true,
		Source:                 "external_provider_permission",
	}}})
	mux := http.NewServeMux()
	RegisterLLMRoutes(mux, nil, svc.hubs, llmservice.NewService(&llmDeleteTestSettings{}), nil, checker, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/llm/v1/authorization?hub_id=hub_default_alias&tenant_id=tenant_default", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("X-Hub-ID", "hub_default_alias")
	resp := httptest.NewRecorder()
	mux.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", resp.Code, resp.Body.String())
	}
	var payload struct {
		TenantID               string   `json:"tenant_id"`
		LookupTenantIDs        []string `json:"lookup_tenant_ids"`
		AllowExternalProviders bool     `json:"allow_external_providers"`
		Authorizations         []struct {
			ID       string `json:"id"`
			TenantID string `json:"tenant_id"`
		} `json:"authorizations"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode authorization response: %v", err)
	}
	if payload.TenantID != "tenant_default" || !payload.AllowExternalProviders || len(payload.Authorizations) != 0 {
		t.Fatalf("default tenant alias response = %#v, want active default alias grant (no credit authorizations)", payload)
	}
	if !containsString(payload.LookupTenantIDs, "") {
		t.Fatalf("lookup_tenant_ids = %#v, want default storage alias", payload.LookupTenantIDs)
	}
}

func TestHubHeartbeatIncludesGenericLLMComputeAuthorizationPayload(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	ctx := context.Background()
	secret := "hub-heartbeat-llm-secret"
	now := time.Now().UTC()
	if err := svc.store.Hubs.Create(ctx, &store.HubInstance{
		ID:            "hub_heartbeat_llm",
		OwnerEmail:    "owner@example.com",
		Name:          "Heartbeat LLM Hub",
		BaseURL:       "https://hub.example.com",
		Status:        "online",
		HubSecretHash: testHashToken(secret),
		CreatedAt:     now,
		UpdatedAt:     now,
	}); err != nil {
		t.Fatalf("create hub: %v", err)
	}

	checker := llmservice.NewAuthorizationChecker(&llmDeleteAuthRepo{auths: []*llmservice.TenantAuthorization{{
		ID:                     "external_heartbeat",
		HubID:                  "hub_heartbeat_llm",
		TenantID:               "acme",
		ServiceGroupID:         llmservice.ExternalComputePermissionServiceGroupID,
		CreditsTotal:           1000000000000,
		StartsAt:               now.Add(-time.Hour),
		ExpiresAt:              now.Add(time.Hour),
		Status:                 "active",
		AllowExternalProviders: true,
		Source:                 "external_provider_permission",
	}}})
	previous := currentLLMAuthorizationSyncChecker()
	SetLLMAuthorizationSyncChecker(checker)
	defer SetLLMAuthorizationSyncChecker(previous)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/hubs/{id}/heartbeat", HubHeartbeatHandler(svc.hubs))
	req := httptest.NewRequest(http.MethodPost, "/api/hubs/hub_heartbeat_llm/heartbeat", bytes.NewReader([]byte(`{"hub_secret":"`+secret+`"}`)))
	resp := httptest.NewRecorder()
	mux.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("heartbeat status = %d body=%s, want 200", resp.Code, resp.Body.String())
	}
	var payload struct {
		AllowExternalProviders bool `json:"allow_external_providers"`
		Authorizations         map[string]struct {
			Tenants map[string]struct {
				AllowExternalProviders bool `json:"allow_external_providers"`
				Authorizations         []struct {
					ID string `json:"id"`
				} `json:"authorizations"`
			} `json:"tenants"`
		} `json:"authorizations"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode heartbeat response: %v", err)
	}
	if !payload.AllowExternalProviders {
		t.Fatalf("top-level allow_external_providers = false, want true for legacy Hub/UI compatibility")
	}
	compute := payload.Authorizations["llm_compute"].Tenants["tenant_acme"]
	if !compute.AllowExternalProviders {
		t.Fatalf("llm compute heartbeat authorization = %#v body=%s, want allow_external_providers=true", compute, resp.Body.String())
	}
	// Pure permission grants don't appear in the authorizations list
	if len(compute.Authorizations) != 0 {
		t.Fatalf("llm compute heartbeat authorizations len = %d, want 0 (permission-only)", len(compute.Authorizations))
	}
}

func TestHubHeartbeatIncludesInactiveLLMComputeRevocationPayload(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	ctx := context.Background()
	secret := "hub-heartbeat-llm-revoked-secret"
	now := time.Now().UTC()
	if err := svc.store.Hubs.Create(ctx, &store.HubInstance{
		ID:                     "hub_heartbeat_llm_revoked",
		OwnerEmail:             "owner@example.com",
		Name:                   "Heartbeat Revoked LLM Hub",
		BaseURL:                "https://hub.example.com",
		Status:                 "online",
		HubSecretHash:          testHashToken(secret),
		AllowExternalProviders: true,
		CreatedAt:              now,
		UpdatedAt:              now,
	}); err != nil {
		t.Fatalf("create hub: %v", err)
	}

	checker := llmservice.NewAuthorizationChecker(&llmDeleteAuthRepo{auths: []*llmservice.TenantAuthorization{{
		ID:                     "external_revoked_heartbeat",
		HubID:                  "hub_heartbeat_llm_revoked",
		TenantID:               "tenant_acme",
		ServiceGroupID:         llmservice.ExternalComputePermissionServiceGroupID,
		StartsAt:               now.Add(-time.Hour),
		ExpiresAt:              now.Add(-time.Hour),
		Status:                 "expired",
		AllowExternalProviders: false,
		Source:                 "external_provider_permission",
	}}})
	previous := currentLLMAuthorizationSyncChecker()
	SetLLMAuthorizationSyncChecker(checker)
	defer SetLLMAuthorizationSyncChecker(previous)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/hubs/{id}/heartbeat", HubHeartbeatHandler(svc.hubs))
	req := httptest.NewRequest(http.MethodPost, "/api/hubs/hub_heartbeat_llm_revoked/heartbeat", bytes.NewReader([]byte(`{"hub_secret":"`+secret+`"}`)))
	resp := httptest.NewRecorder()
	mux.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("heartbeat status = %d body=%s, want 200", resp.Code, resp.Body.String())
	}
	var payload struct {
		AllowExternalProviders bool `json:"allow_external_providers"`
		Authorizations         map[string]struct {
			Tenants map[string]struct {
				AllowExternalProviders bool `json:"allow_external_providers"`
				Authorizations         []struct {
					ID     string `json:"id"`
					Active bool   `json:"active"`
					Status string `json:"status"`
				} `json:"authorizations"`
			} `json:"tenants"`
		} `json:"authorizations"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode heartbeat response: %v", err)
	}
	if payload.AllowExternalProviders {
		t.Fatalf("top-level allow_external_providers = true, want false for revoked compute authorization")
	}
	compute := payload.Authorizations["llm_compute"].Tenants["tenant_acme"]
	if compute.AllowExternalProviders || len(compute.Authorizations) != 1 || compute.Authorizations[0].Active || compute.Authorizations[0].Status != "expired" {
		t.Fatalf("llm compute revoked heartbeat authorization = %#v body=%s", compute, resp.Body.String())
	}
}

func TestHubHeartbeatIncludesEmptyLLMComputeAuthorizationPayloadWhenNoGrant(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	ctx := context.Background()
	secret := "hub-heartbeat-empty-llm-secret"
	now := time.Now().UTC()
	if err := svc.store.Hubs.Create(ctx, &store.HubInstance{
		ID:            "hub_heartbeat_empty_llm",
		OwnerEmail:    "owner@example.com",
		Name:          "Heartbeat Empty LLM Hub",
		BaseURL:       "https://hub.example.com",
		Status:        "online",
		HubSecretHash: testHashToken(secret),
		CreatedAt:     now,
		UpdatedAt:     now,
	}); err != nil {
		t.Fatalf("create hub: %v", err)
	}

	checker := llmservice.NewAuthorizationChecker(&llmDeleteAuthRepo{})
	previous := currentLLMAuthorizationSyncChecker()
	SetLLMAuthorizationSyncChecker(checker)
	defer SetLLMAuthorizationSyncChecker(previous)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/hubs/{id}/heartbeat", HubHeartbeatHandler(svc.hubs))
	req := httptest.NewRequest(http.MethodPost, "/api/hubs/hub_heartbeat_empty_llm/heartbeat", bytes.NewReader([]byte(`{"hub_secret":"`+secret+`"}`)))
	resp := httptest.NewRecorder()
	mux.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("heartbeat status = %d body=%s, want 200", resp.Code, resp.Body.String())
	}
	var payload struct {
		Authorizations map[string]struct {
			Tenants map[string]any `json:"tenants"`
		} `json:"authorizations"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode heartbeat response: %v", err)
	}
	compute, ok := payload.Authorizations["llm_compute"]
	if !ok || len(compute.Tenants) != 0 {
		t.Fatalf("llm compute empty heartbeat authorization = %#v body=%s", compute, resp.Body.String())
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
