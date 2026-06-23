package llmservice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

type proxyRoundTripFunc func(*http.Request) (*http.Response, error)

func (f proxyRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type recordingUsageRecorder struct {
	records  []*llmpool.UsageRecord
	contexts []usageContextData
}

func (r *recordingUsageRecorder) RecordUsage(ctx context.Context, record *llmpool.UsageRecord) error {
	hubID, tenantID := usageContextValues(ctx)
	r.records = append(r.records, record)
	r.contexts = append(r.contexts, usageContextData{HubID: hubID, TenantID: tenantID})
	return nil
}

// --- mock auth repo ---

type mockAuthRepo struct {
	auths []*TenantAuthorization
}

func (r *mockAuthRepo) Create(_ context.Context, auth *TenantAuthorization) error {
	r.auths = append(r.auths, auth)
	return nil
}
func (r *mockAuthRepo) GetByID(_ context.Context, id string) (*TenantAuthorization, error) {
	for _, a := range r.auths {
		if a.ID == id {
			return a, nil
		}
	}
	return nil, nil
}
func (r *mockAuthRepo) ListByHubTenant(_ context.Context, hubID, tenantID string) ([]*TenantAuthorization, error) {
	var result []*TenantAuthorization
	for _, a := range r.auths {
		if a.HubID == hubID && a.TenantID == tenantID {
			result = append(result, a)
		}
	}
	return result, nil
}
func (r *mockAuthRepo) ListAll(_ context.Context) ([]*TenantAuthorization, error) {
	return r.auths, nil
}
func (r *mockAuthRepo) ListByServiceGroup(_ context.Context, serviceGroupID string) ([]*TenantAuthorization, error) {
	var result []*TenantAuthorization
	for _, a := range r.auths {
		if a.ServiceGroupID == serviceGroupID {
			result = append(result, a)
		}
	}
	return result, nil
}
func (r *mockAuthRepo) Update(_ context.Context, auth *TenantAuthorization) error { return nil }
func (r *mockAuthRepo) DeductCredits(_ context.Context, id string, credits float64, _ time.Time) (float64, error) {
	for _, a := range r.auths {
		if a.ID == id {
			available := a.CreditsRemaining()
			if available <= 0 {
				return 0, nil
			}
			actual := credits
			if available < actual {
				actual = available
			}
			a.CreditsUsed += actual
			return actual, nil
		}
	}
	return 0, nil
}

// --- mock system settings ---

type mockSystemSettings struct {
	data map[string]string
}

func (s *mockSystemSettings) Set(_ context.Context, key, val string) error {
	if s.data == nil {
		s.data = map[string]string{}
	}
	s.data[key] = val
	return nil
}
func (s *mockSystemSettings) Get(_ context.Context, key string) (string, error) {
	return s.data[key], nil
}
func (s *mockSystemSettings) List(_ context.Context) ([]*store.SystemSettingEntry, error) {
	return nil, nil
}

// --- tests ---

func TestHandleProxyRequestRequiresRequestAndBody(t *testing.T) {
	cfg := &ProxyConfig{
		Service:     NewService(&mockSystemSettings{}),
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
	}
	if _, err := HandleProxyRequest(context.Background(), cfg, nil); err == nil || err.Error() != "proxy request is required" {
		t.Fatalf("nil request error = %v, want proxy request is required", err)
	}
	if _, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{}); err == nil || err.Error() != "proxy request body is required" {
		t.Fatalf("nil body error = %v, want proxy request body is required", err)
	}
}

func TestHandleProxyRequestTrimsModel(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer upstream.Close()

	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers:     []llmpool.ProviderConfig{{ID: "p1", Name: "P1", APIURL: upstream.URL}},
		ServiceGroups: []llmpool.ServiceGroup{{ID: "g1", Name: "G1", Models: []llmpool.ModelConfig{{Name: "gpt-4", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p1"}}}}}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	cfg := &ProxyConfig{Service: svc, AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}), HTTPClient: upstream.Client()}
	if _, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{Body: map[string]any{"model": "  gpt-4  "}}); err != nil {
		t.Fatalf("trimmed body model should match: %v", err)
	}
	if _, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{Model: "   ", Body: map[string]any{}}); err == nil || err.Error() != "model not specified in request" {
		t.Fatalf("blank explicit model error = %v, want model not specified in request", err)
	}
}
func TestHandleProxyRequest_NoModel(t *testing.T) {
	cfg := &ProxyConfig{
		Service:     NewService(&mockSystemSettings{}),
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
	}
	req := &ProxyRequest{HubID: "hub1", TenantID: "t1", Body: map[string]any{}}
	_, err := HandleProxyRequest(context.Background(), cfg, req)
	if err == nil || err.Error() != "model not specified in request" {
		t.Fatalf("expected model error, got: %v", err)
	}
}

func TestBuildTenantAuthorizationStatusIncludesInactiveState(t *testing.T) {
	authRepo := &mockAuthRepo{auths: []*TenantAuthorization{{
		ID: "auth-expired", HubID: "hub1", TenantID: "tenant_acme", ServiceGroupID: "g1",
		CreditsTotal: 100, CreditsUsed: 10, StartsAt: time.Now().Add(-2 * time.Hour), ExpiresAt: time.Now().Add(-time.Hour),
		Status: "expired", Source: "admin_grant",
	}}}
	status, err := BuildTenantAuthorizationStatus(context.Background(), NewAuthorizationChecker(authRepo), "hub1", "tenant_acme")
	if err != nil {
		t.Fatalf("BuildTenantAuthorizationStatus() error = %v", err)
	}
	if status.AllowExternalProviders {
		t.Fatalf("AllowExternalProviders = true, want false")
	}
	if len(status.Authorizations) != 1 {
		t.Fatalf("len(Authorizations) = %d, want 1", len(status.Authorizations))
	}
	if status.Authorizations[0].Active {
		t.Fatalf("inactive authorization reported active: %#v", status.Authorizations[0])
	}
	if status.Authorizations[0].Status != "expired" {
		t.Fatalf("Status = %q, want expired", status.Authorizations[0].Status)
	}
}

func TestBuildTenantAuthorizationStatusIncludesInactivePermissionState(t *testing.T) {
	authRepo := &mockAuthRepo{auths: []*TenantAuthorization{{
		ID: "external-expired", HubID: "hub1", TenantID: "tenant_acme", ServiceGroupID: ExternalComputePermissionServiceGroupID,
		StartsAt: time.Now().Add(-2 * time.Hour), ExpiresAt: time.Now().Add(-time.Hour),
		Status: "expired", Source: "external_provider_permission", AllowExternalProviders: false,
	}}}
	status, err := BuildTenantAuthorizationStatus(context.Background(), NewAuthorizationChecker(authRepo), "hub1", "tenant_acme")
	if err != nil {
		t.Fatalf("BuildTenantAuthorizationStatus() error = %v", err)
	}
	if status.AllowExternalProviders {
		t.Fatalf("AllowExternalProviders = true, want false")
	}
	if len(status.Authorizations) != 1 {
		t.Fatalf("len(Authorizations) = %d, want explicit inactive permission state", len(status.Authorizations))
	}
	if status.Authorizations[0].Active || status.Authorizations[0].ServiceGroupID != ExternalComputePermissionServiceGroupID {
		t.Fatalf("permission state = %#v, want inactive external permission", status.Authorizations[0])
	}
}

func TestBuildTenantAuthorizationStatusLatestRevocationOverridesOlderGrant(t *testing.T) {
	now := time.Now().UTC()
	authRepo := &mockAuthRepo{auths: []*TenantAuthorization{{
		ID: "external-active-old", HubID: "hub1", TenantID: "tenant_acme", ServiceGroupID: ExternalComputePermissionServiceGroupID,
		StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour),
		Status: "active", Source: "external_provider_permission", AllowExternalProviders: true,
		CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour),
	}, {
		ID: "auth_admin_hub1_tenant_acme___external_compute_permission__", HubID: "hub1", TenantID: "tenant_acme", ServiceGroupID: ExternalComputePermissionServiceGroupID,
		StartsAt: now.Add(-time.Minute), ExpiresAt: now.Add(-time.Minute),
		Status: "expired", Source: "external_provider_permission", AllowExternalProviders: false,
		CreatedAt: now, UpdatedAt: now,
	}}}
	checker := NewAuthorizationChecker(authRepo)
	allowed, err := checker.HasExternalProviderAccess(context.Background(), "hub1", "tenant_acme")
	if err != nil {
		t.Fatalf("HasExternalProviderAccess() error = %v", err)
	}
	if allowed {
		t.Fatalf("HasExternalProviderAccess() = true, want latest revocation to win")
	}
	status, err := BuildTenantAuthorizationStatus(context.Background(), checker, "hub1", "tenant_acme")
	if err != nil {
		t.Fatalf("BuildTenantAuthorizationStatus() error = %v", err)
	}
	if status.AllowExternalProviders {
		t.Fatalf("AllowExternalProviders = true, want latest revocation to win")
	}
	if len(status.Authorizations) != 1 || status.Authorizations[0].ID != "auth_admin_hub1_tenant_acme___external_compute_permission__" || status.Authorizations[0].Active {
		t.Fatalf("authorizations = %#v, want only inactive latest revocation", status.Authorizations)
	}
}

func TestBuildTenantAuthorizationStatusNewerGrantOverridesOlderRevocation(t *testing.T) {
	now := time.Now().UTC()
	authRepo := &mockAuthRepo{auths: []*TenantAuthorization{{
		ID: "external-revoked-old", HubID: "hub1", TenantID: "tenant_acme", ServiceGroupID: ExternalComputePermissionServiceGroupID,
		StartsAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(-2 * time.Hour),
		Status: "expired", Source: "external_provider_permission", AllowExternalProviders: false,
		CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour),
	}, {
		ID: "external-active-new", HubID: "hub1", TenantID: "tenant_acme", ServiceGroupID: ExternalComputePermissionServiceGroupID,
		StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour),
		Status: "active", Source: "external_provider_permission", AllowExternalProviders: true,
		CreatedAt: now, UpdatedAt: now,
	}}}
	checker := NewAuthorizationChecker(authRepo)
	allowed, err := checker.HasExternalProviderAccess(context.Background(), "hub1", "tenant_acme")
	if err != nil {
		t.Fatalf("HasExternalProviderAccess() error = %v", err)
	}
	if !allowed {
		t.Fatalf("HasExternalProviderAccess() = false, want newer grant to win")
	}
	status, err := BuildTenantAuthorizationStatus(context.Background(), checker, "hub1", "tenant_acme")
	if err != nil {
		t.Fatalf("BuildTenantAuthorizationStatus() error = %v", err)
	}
	if !status.AllowExternalProviders {
		t.Fatalf("AllowExternalProviders = false, want newer grant to win")
	}
}

func TestBuildTenantAuthorizationStatusTiebreaksEqualUpdateTimeByCreatedAt(t *testing.T) {
	now := time.Now().UTC()
	authRepo := &mockAuthRepo{auths: []*TenantAuthorization{{
		ID: "external-revoked-old-created", HubID: "hub1", TenantID: "tenant_acme", ServiceGroupID: ExternalComputePermissionServiceGroupID,
		StartsAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(-2 * time.Hour),
		Status: "expired", Source: "external_provider_permission", AllowExternalProviders: false,
		CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now,
	}, {
		ID: "external-active-new-created", HubID: "hub1", TenantID: "tenant_acme", ServiceGroupID: ExternalComputePermissionServiceGroupID,
		StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour),
		Status: "active", Source: "external_provider_permission", AllowExternalProviders: true,
		CreatedAt: now.Add(-time.Hour), UpdatedAt: now,
	}}}
	checker := NewAuthorizationChecker(authRepo)
	allowed, err := checker.HasExternalProviderAccess(context.Background(), "hub1", "tenant_acme")
	if err != nil {
		t.Fatalf("HasExternalProviderAccess() error = %v", err)
	}
	if !allowed {
		t.Fatalf("HasExternalProviderAccess() = false, want newer created grant to win equal updated_at")
	}
	status, err := BuildTenantAuthorizationStatus(context.Background(), checker, "hub1", "tenant_acme")
	if err != nil {
		t.Fatalf("BuildTenantAuthorizationStatus() error = %v", err)
	}
	if !status.AllowExternalProviders {
		t.Fatalf("AllowExternalProviders = false, want newer created grant to win equal updated_at")
	}
}

func TestBuildTenantAuthorizationStatusIgnoresExpiredLegacyRedeemPermissionRowsForAccessState(t *testing.T) {
	now := time.Now().UTC()
	authRepo := &mockAuthRepo{auths: []*TenantAuthorization{{
		ID: "external-active-canonical", HubID: "hub1", TenantID: "tenant_acme", ServiceGroupID: ExternalComputePermissionServiceGroupID,
		StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour),
		Status: "active", Source: "external_provider_permission", AllowExternalProviders: true,
		CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Minute),
	}, {
		ID: "external-expired-legacy-redeem", HubID: "hub1", TenantID: "tenant_acme", ServiceGroupID: "redeem",
		StartsAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(time.Hour),
		Status: "expired", Source: "external_provider_permission", AllowExternalProviders: false,
		CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now,
	}}}
	checker := NewAuthorizationChecker(authRepo)
	allowed, err := checker.HasExternalProviderAccess(context.Background(), "hub1", "tenant_acme")
	if err != nil {
		t.Fatalf("HasExternalProviderAccess() error = %v", err)
	}
	if !allowed {
		t.Fatalf("HasExternalProviderAccess() = false, want canonical grant to ignore legacy redeem row")
	}
	status, err := BuildTenantAuthorizationStatus(context.Background(), checker, "hub1", "tenant_acme")
	if err != nil {
		t.Fatalf("BuildTenantAuthorizationStatus() error = %v", err)
	}
	if !status.AllowExternalProviders {
		t.Fatalf("AllowExternalProviders = false, want canonical grant to ignore legacy redeem row")
	}
}

func TestHandleProxyRequest_ModelNotInRegistry(t *testing.T) {
	system := &mockSystemSettings{}
	svc := NewService(system)
	// Save a registry with one group but different model
	_ = svc.SaveRegistry(context.Background(), &Registry{
		Providers:     []llmpool.ProviderConfig{{ID: "p1", Name: "P1", APIURL: "http://localhost"}},
		ServiceGroups: []llmpool.ServiceGroup{{ID: "g1", Name: "G1", Models: []llmpool.ModelConfig{{Name: "gpt-4", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p1"}}}}}},
	})

	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
	}
	req := &ProxyRequest{HubID: "hub1", TenantID: "t1", Body: map[string]any{"model": "nonexistent"}}
	_, err := HandleProxyRequest(context.Background(), cfg, req)
	if err == nil {
		t.Fatal("expected error for missing model")
	}
}

func TestHandleProxyRequest_AuthDenied(t *testing.T) {
	system := &mockSystemSettings{}
	svc := NewService(system)
	_ = svc.SaveRegistry(context.Background(), &Registry{
		Providers:     []llmpool.ProviderConfig{{ID: "p1", Name: "P1", APIURL: "http://localhost"}},
		ServiceGroups: []llmpool.ServiceGroup{{ID: "g1", Name: "G1", AccessPolicy: AccessPolicyGrantRequired, Models: []llmpool.ModelConfig{{Name: "gpt-4", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p1"}}}}}},
	})

	// No authorizations: access denied
	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
	}
	req := &ProxyRequest{HubID: "hub1", TenantID: "t1", Body: map[string]any{"model": "gpt-4"}}
	_, err := HandleProxyRequest(context.Background(), cfg, req)
	if err == nil {
		t.Fatal("expected authorization denied")
	}
	if !contains(err.Error(), "authorization denied") {
		t.Fatalf("expected auth denied error, got: %v", err)
	}
}

func TestHandleProxyRequest_GrantRequiredMatchesTenantIDAlias(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`))
	}))
	defer upstream.Close()

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{{ID: "p1", Name: "P1", APIURL: upstream.URL}},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "g1",
			Name:         "G1",
			AccessPolicy: AccessPolicyGrantRequired,
			Models:       []llmpool.ModelConfig{{Name: "gpt-4", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p1"}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	authRepo := &mockAuthRepo{auths: []*TenantAuthorization{{
		ID: "auth-alias", HubID: "hub1", TenantID: "acme", ServiceGroupID: "g1",
		CreditsTotal: 1000, StartsAt: time.Now().Add(-time.Hour), ExpiresAt: time.Now().Add(time.Hour),
		Status: "active",
	}}}
	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(authRepo),
		HTTPClient:  upstream.Client(),
	}
	req := &ProxyRequest{HubID: "hub1", TenantID: "tenant_acme", Body: map[string]any{"model": "gpt-4"}}
	resp, err := HandleProxyRequest(context.Background(), cfg, req)
	if err != nil {
		t.Fatalf("alias tenant grant should authorize proxy request: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if authRepo.auths[0].CreditsUsed <= 0 {
		t.Fatalf("expected alias grant credits to be deducted")
	}
}

func TestHandleProxyRequest_GrantRequiredAppliesMinimumCreditCharge(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":60,"completion_tokens":40,"total_tokens":100}}`))
	}))
	defer upstream.Close()

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{{ID: "p1", Name: "P1", APIURL: upstream.URL}},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "g1",
			Name:         "G1",
			AccessPolicy: AccessPolicyGrantRequired,
			Models:       []llmpool.ModelConfig{{Name: "gpt-4", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p1"}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	authRepo := &mockAuthRepo{auths: []*TenantAuthorization{{
		ID: "auth-small", HubID: "hub1", TenantID: "t1", ServiceGroupID: "g1",
		CreditsTotal: 1000, StartsAt: time.Now().Add(-time.Hour), ExpiresAt: time.Now().Add(time.Hour),
		Status: "active",
	}}}
	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(authRepo),
		HTTPClient:  upstream.Client(),
		Usage:       &recordingUsageRecorder{},
	}

	resp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{
		HubID:    "hub1",
		TenantID: "t1",
		Body:     map[string]any{"model": "gpt-4", "messages": []any{map[string]any{"role": "user", "content": "hi"}}},
	})
	if err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := authRepo.auths[0].CreditsUsed; got != minimumProxyRequestCredits {
		t.Fatalf("CreditsUsed = %.3f, want %.3f minimum charge", got, minimumProxyRequestCredits)
	}
}

func TestHandleProxyRequest_GrantRequiredAppliesMinimumCreditChargeWhenUsageMissing(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer upstream.Close()

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{{ID: "p1", Name: "P1", APIURL: upstream.URL}},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "g1",
			Name:         "G1",
			AccessPolicy: AccessPolicyGrantRequired,
			Models:       []llmpool.ModelConfig{{Name: "gpt-4", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p1"}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	authRepo := &mockAuthRepo{auths: []*TenantAuthorization{{
		ID: "auth-missing-usage", HubID: "hub1", TenantID: "t1", ServiceGroupID: "g1",
		CreditsTotal: 1000, StartsAt: time.Now().Add(-time.Hour), ExpiresAt: time.Now().Add(time.Hour),
		Status: "active",
	}}}
	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(authRepo),
		HTTPClient:  upstream.Client(),
		Usage:       &recordingUsageRecorder{},
	}

	resp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{
		HubID:    "hub1",
		TenantID: "t1",
		Body:     map[string]any{"model": "gpt-4", "messages": []any{map[string]any{"role": "user", "content": "hi"}}},
	})
	if err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := authRepo.auths[0].CreditsUsed; got != minimumProxyRequestCredits {
		t.Fatalf("CreditsUsed = %.3f, want %.3f minimum charge", got, minimumProxyRequestCredits)
	}
	if !bytes.Contains(resp.Body, []byte(`"usage"`)) || !bytes.Contains(resp.Body, []byte(`"estimated":true`)) {
		t.Fatalf("response body should include estimated usage: %s", resp.Body)
	}
}

func TestHandleProxyRequest_GrantRequiredSpreadsChargeAcrossComputeCards(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":10000,"completion_tokens":5000,"total_tokens":15000}}`))
	}))
	defer upstream.Close()

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{{ID: "p1", Name: "P1", APIURL: upstream.URL}},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "redeem",
			Name:         "Redeem",
			AccessPolicy: AccessPolicyGrantRequired,
			Models:       []llmpool.ModelConfig{{Name: "gpt-4", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p1"}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	now := time.Now()
	authRepo := &mockAuthRepo{auths: []*TenantAuthorization{
		{
			ID: "auth-small", HubID: "hub1", TenantID: "t1", ServiceGroupID: "redeem",
			CreditsTotal: 1, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour),
			Status: "active", CardOrderID: "HC-SMALL", CreatedAt: now.Add(-2 * time.Hour),
		},
		{
			ID: "auth-large", HubID: "hub1", TenantID: "t1", ServiceGroupID: "redeem",
			CreditsTotal: 1000, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(2 * time.Hour),
			Status: "active", CardOrderID: "HC-LARGE", CreatedAt: now.Add(-time.Hour),
		},
	}}
	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(authRepo),
		HTTPClient:  upstream.Client(),
		Usage:       &recordingUsageRecorder{},
	}

	resp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{
		HubID:    "hub1",
		TenantID: "t1",
		Body:     map[string]any{"model": "gpt-4", "messages": []any{map[string]any{"role": "user", "content": "hi"}}},
	})
	if err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := authRepo.auths[0].CreditsUsed; got != 1 {
		t.Fatalf("small card CreditsUsed = %.3f, want 1", got)
	}
	if got := authRepo.auths[1].CreditsUsed; got != 0.5 {
		t.Fatalf("large card CreditsUsed = %.3f, want 0.5", got)
	}
	usage := cfg.Usage.(*recordingUsageRecorder)
	if len(usage.records) != 1 {
		t.Fatalf("usage records len = %d, want 1", len(usage.records))
	}
	if got := usage.records[0].AuthID; got != "auth-small,auth-large" {
		t.Fatalf("usage AuthID = %q, want charged auth IDs", got)
	}
}

func TestAuthorizationCheckerDeductCreditsForServiceGroupReportsInsufficientAggregateBalance(t *testing.T) {
	now := time.Now()
	authRepo := &mockAuthRepo{auths: []*TenantAuthorization{{
		ID: "auth-half", HubID: "hub1", TenantID: "t1", ServiceGroupID: "redeem",
		CreditsTotal: 0.5, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour),
		Status: "active", CreatedAt: now,
	}}}
	checker := NewAuthorizationChecker(authRepo)

	deductions, err := checker.DeductCreditsForServiceGroup(context.Background(), "hub1", "t1", "redeem", 1)
	if err == nil {
		t.Fatal("DeductCreditsForServiceGroup() error = nil, want insufficient credits error")
	}
	var insufficient *InsufficientCreditsError
	if !errors.As(err, &insufficient) {
		t.Fatalf("DeductCreditsForServiceGroup() error = %T %[1]v, want InsufficientCreditsError", err)
	}
	if insufficient.Requested != 1 || insufficient.Deducted != 0.5 || insufficient.Remaining != 0.5 {
		t.Fatalf("insufficient error = %#v, want requested=1 deducted=0.5 remaining=0.5", insufficient)
	}
	if len(deductions) != 1 || deductions[0].AuthID != "auth-half" || deductions[0].Credits != 0.5 {
		t.Fatalf("deductions = %#v, want partial charge against auth-half", deductions)
	}
	if got := authRepo.auths[0].CreditsUsed; got != 0.5 {
		t.Fatalf("CreditsUsed = %.3f, want 0.5", got)
	}
}

func TestHandleProxyRequest_GrantRequiredRecordsOnlyDeductedCreditsWhenBalanceRunsOut(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":5000,"completion_tokens":5000,"total_tokens":10000}}`))
	}))
	defer upstream.Close()

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{{ID: "p1", Name: "P1", APIURL: upstream.URL}},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "redeem",
			Name:         "Redeem",
			AccessPolicy: AccessPolicyGrantRequired,
			Models:       []llmpool.ModelConfig{{Name: "gpt-4", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p1"}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	now := time.Now()
	authRepo := &mockAuthRepo{auths: []*TenantAuthorization{{
		ID: "auth-half", HubID: "hub1", TenantID: "t1", ServiceGroupID: "redeem",
		CreditsTotal: 0.5, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour),
		Status: "active", CreatedAt: now,
	}}}
	usage := &recordingUsageRecorder{}
	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(authRepo),
		HTTPClient:  upstream.Client(),
		Usage:       usage,
	}

	resp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{
		HubID:    "hub1",
		TenantID: "t1",
		Body:     map[string]any{"model": "gpt-4", "messages": []any{map[string]any{"role": "user", "content": "hi"}}},
	})
	if err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := authRepo.auths[0].CreditsUsed; got != 0.5 {
		t.Fatalf("CreditsUsed = %.3f, want 0.5", got)
	}
	if len(usage.records) != 1 {
		t.Fatalf("usage records len = %d, want 1", len(usage.records))
	}
	if got := usage.records[0].Credits; got != 0.5 {
		t.Fatalf("usage Credits = %.3f, want actual deducted credits 0.5", got)
	}
	if got := usage.records[0].AuthID; got != "auth-half" {
		t.Fatalf("usage AuthID = %q, want auth-half", got)
	}
}

func TestBuildTenantAuthorizationStatusPreservesFractionalCredits(t *testing.T) {
	now := time.Now().UTC()
	checker := NewAuthorizationChecker(&mockAuthRepo{auths: []*TenantAuthorization{{
		ID:             "auth-fractional",
		HubID:          "hub1",
		TenantID:       "t1",
		ServiceGroupID: "maclaw-official",
		CreditsTotal:   10,
		CreditsUsed:    1.1,
		StartsAt:       now.Add(-time.Hour),
		ExpiresAt:      now.Add(time.Hour),
		Status:         "active",
		Source:         "card",
	}}})

	status, err := BuildTenantAuthorizationStatus(context.Background(), checker, "hub1", "t1")
	if err != nil {
		t.Fatalf("BuildTenantAuthorizationStatus() error = %v", err)
	}
	if len(status.Authorizations) != 1 {
		t.Fatalf("authorizations len = %d, want 1: %#v", len(status.Authorizations), status)
	}
	got := status.Authorizations[0]
	if got.CreditsUsed != 1.1 || got.CreditsRemaining != 8.9 {
		t.Fatalf("authorization credits = used %.17g remaining %.17g, want 1.1/8.9", got.CreditsUsed, got.CreditsRemaining)
	}
}

func TestBuildTenantAuthorizationStatusRoundsCreditDisplay(t *testing.T) {
	now := time.Now().UTC()
	checker := NewAuthorizationChecker(&mockAuthRepo{auths: []*TenantAuthorization{{
		ID:             "auth-card-display",
		HubID:          "hub1",
		TenantID:       "t1",
		ServiceGroupID: "redeem",
		CreditsTotal:   520000,
		CreditsUsed:    12102.734400000001,
		StartsAt:       now.Add(-time.Hour),
		ExpiresAt:      now.Add(time.Hour),
		Status:         "active",
		Source:         "card",
	}}})

	status, err := BuildTenantAuthorizationStatus(context.Background(), checker, "hub1", "t1")
	if err != nil {
		t.Fatalf("BuildTenantAuthorizationStatus() error = %v", err)
	}
	if len(status.Authorizations) != 1 {
		t.Fatalf("authorizations len = %d, want 1: %#v", len(status.Authorizations), status)
	}
	got := status.Authorizations[0]
	if got.CreditsUsed != 12102.7344 {
		t.Fatalf("credits_used = %.17g, want 12102.7344", got.CreditsUsed)
	}
	if got.CreditsRemaining != 507897.2656 {
		t.Fatalf("credits_remaining = %.17g, want 507897.2656", got.CreditsRemaining)
	}
}

func TestHandleProxyRequest_FreeAccessPolicySkipsAuthorization(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":100,"completion_tokens":50}}`))
	}))
	defer upstream.Close()

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{{ID: "p1", Name: "P1", APIURL: upstream.URL}},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "g1",
			Name:         "G1",
			AccessPolicy: AccessPolicyFree,
			Models:       []llmpool.ModelConfig{{Name: "gpt-4", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p1"}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	authRepo := &mockAuthRepo{}
	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(authRepo),
		HTTPClient:  upstream.Client(),
	}
	req := &ProxyRequest{HubID: "hub1", TenantID: "t1", Body: map[string]any{"model": "gpt-4"}}
	resp, err := HandleProxyRequest(context.Background(), cfg, req)
	if err != nil {
		t.Fatalf("free group should not require authorization: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if len(authRepo.auths) != 0 {
		t.Fatalf("free group should not create or deduct authorizations: %#v", authRepo.auths)
	}
}

func TestForwardToProviderUsesSharedCorelibCompatibility(t *testing.T) {
	var seen map[string]any
	client := &http.Client{Transport: proxyRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "codegen.qianxin-inc.cn" || req.URL.Path != "/api/v1/chat/completions" {
			t.Fatalf("upstream URL = %s, want CodeGen chat completions endpoint", req.URL.String())
		}
		if err := json.NewDecoder(req.Body).Decode(&seen); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(`{"id":"ok","choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)),
			Request:    req,
		}, nil
	})}
	provider := &llmpool.ProviderConfig{
		ID:       "codegen",
		Name:     "CodeGen",
		APIURL:   "https://codegen.qianxin-inc.cn/api/v1",
		APIKey:   "secret",
		Protocol: "openai",
	}
	body := map[string]any{
		"model":           "auto",
		"stream":          true,
		"stream_options":  map[string]any{"include_usage": true},
		"response_format": map[string]any{"type": "json_schema"},
		"messages":        []any{map[string]any{"role": "user", "content": "hello"}},
	}

	resp, err := forwardToProvider(context.Background(), client, provider, body, "auto", "auto")
	if err != nil {
		t.Fatalf("forwardToProvider() error = %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if seen["model"] != "qax-codegen/Auto" {
		t.Fatalf("model = %#v, want CodeGen auto model", seen["model"])
	}
	for _, key := range []string{"stream_options", "response_format"} {
		if _, ok := seen[key]; ok {
			t.Fatalf("%s leaked upstream: %#v", key, seen)
		}
	}
	if seen["stream"] != false {
		t.Fatalf("stream = %#v, want false for HubCenter non-stream proxy", seen["stream"])
	}
}

func TestHandleProxyRequestUsesProviderConfiguredUpstreamModel(t *testing.T) {
	var seen map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"ok","model":"qax-codegen/Auto","choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`))
	}))
	defer upstream.Close()

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{{ID: "official", Name: "MaClaw Official", APIURL: upstream.URL}},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "maclaw-official",
			Name:         "MaClaw Official",
			AccessPolicy: AccessPolicyFree,
			Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{
				ProviderID: "official",
				Model:      "qax-codegen/Auto",
			}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
		HTTPClient:  upstream.Client(),
	}
	resp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{
		HubID:    "hub1",
		TenantID: "t1",
		Body:     map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hello"}}},
	})
	if err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if seen["model"] != "qax-codegen/Auto" {
		t.Fatalf("upstream model = %#v, want qax-codegen/Auto", seen["model"])
	}
	var payload map[string]any
	if err := json.Unmarshal(resp.Body, &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["model"] != "auto" {
		t.Fatalf("response model = %#v, want logical model auto", payload["model"])
	}
}

func TestHandleProxyRequestUsesRequestedServiceGroupWhenModelsOverlap(t *testing.T) {
	var seen map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"ok","model":"free-upstream","choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`))
	}))
	defer upstream.Close()

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "paid", Name: "Paid", APIURL: upstream.URL},
			{ID: "free", Name: "Free", APIURL: upstream.URL},
		},
		ServiceGroups: []llmpool.ServiceGroup{
			{
				ID:           "paid-group",
				Name:         "Paid Group",
				AccessPolicy: AccessPolicyGrantRequired,
				Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{
					ProviderID: "paid",
					Model:      "paid-upstream",
				}}}},
			},
			{
				ID:           "system-free",
				Name:         "System Free",
				AccessPolicy: AccessPolicyFree,
				Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{
					ProviderID: "free",
					Model:      "free-upstream",
				}}}},
			},
		},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
		HTTPClient:  upstream.Client(),
	}
	resp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{
		HubID:          "hub1",
		TenantID:       "tenant1",
		ServiceGroupID: "system-free",
		Body:           map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hello"}}},
	})
	if err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if resp.ProviderID != "free" {
		t.Fatalf("provider = %q, want free", resp.ProviderID)
	}
	if seen["model"] != "free-upstream" {
		t.Fatalf("upstream model = %#v, want free-upstream", seen["model"])
	}
}

func TestHandleProxyRequestEstimatesAndInjectsUsageWhenUpstreamOmitsUsage(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"ok","model":"qax-codegen/Auto","choices":[{"message":{"content":"estimated answer"}}]}`))
	}))
	defer upstream.Close()

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{{ID: "official", Name: "MaClaw Official", APIURL: upstream.URL}},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "maclaw-official",
			Name:         "MaClaw Official",
			AccessPolicy: AccessPolicyFree,
			Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{
				ProviderID: "official",
				Model:      "qax-codegen/Auto",
			}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	usage := &recordingUsageRecorder{}
	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
		HTTPClient:  upstream.Client(),
		Usage:       usage,
	}
	resp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{
		HubID:    "hub1",
		TenantID: "t1",
		Body:     map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hello official usage"}}},
	})
	if err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if len(usage.records) != 1 {
		t.Fatalf("usage records = %d, want 1", len(usage.records))
	}
	if len(usage.contexts) != 1 || usage.contexts[0].HubID != "hub1" || usage.contexts[0].TenantID != "t1" {
		t.Fatalf("usage context = %+v, want hub1/t1", usage.contexts)
	}
	if usage.records[0].InputTokens <= 0 || usage.records[0].OutputTokens <= 0 {
		t.Fatalf("usage record = %+v, want estimated input/output tokens", usage.records[0])
	}
	var payload map[string]any
	if err := json.Unmarshal(resp.Body, &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	usagePayload, _ := payload["usage"].(map[string]any)
	if usagePayload == nil || usagePayload["estimated"] != true {
		t.Fatalf("response usage not injected as estimated: %#v", payload["usage"])
	}
	if usagePayload["total_tokens"].(float64) <= 0 {
		t.Fatalf("total_tokens = %#v, want positive", usagePayload["total_tokens"])
	}
}

func TestHandleProxyRequestCompletesPartialUsageFromEstimate(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"ok","model":"qax-codegen/Auto","choices":[{"message":{"content":"partial usage answer"}}],"usage":{"prompt_tokens":13}}`))
	}))
	defer upstream.Close()

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{{ID: "official", Name: "MaClaw Official", APIURL: upstream.URL}},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "maclaw-official",
			Name:         "MaClaw Official",
			AccessPolicy: AccessPolicyFree,
			Models:       []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "official", Model: "qax-codegen/Auto"}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	usage := &recordingUsageRecorder{}
	cfg := &ProxyConfig{Service: svc, AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}), HTTPClient: upstream.Client(), Usage: usage}
	resp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{
		HubID:    "hub1",
		TenantID: "t1",
		Body:     map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hello"}}},
	})
	if err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if len(usage.records) != 1 || usage.records[0].InputTokens != 13 || usage.records[0].OutputTokens <= 0 {
		t.Fatalf("usage record = %+v, want input=13 and estimated output", usage.records)
	}
	var payload map[string]any
	if err := json.Unmarshal(resp.Body, &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	usagePayload, _ := payload["usage"].(map[string]any)
	if usagePayload["prompt_tokens"].(float64) != 13 || usagePayload["completion_tokens"].(float64) <= 0 || usagePayload["estimated"] != true {
		t.Fatalf("usage payload = %#v, want completed estimated usage", usagePayload)
	}
}

func TestHandleProxyRequestPreservesInputOutputUsageShape(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"ok","model":"qax-codegen/Auto","choices":[{"message":{"content":"ok"}}],"usage":{"input_tokens":11,"output_tokens":7,"total_tokens":18}}`))
	}))
	defer upstream.Close()

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{{ID: "official", Name: "MaClaw Official", APIURL: upstream.URL}},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "maclaw-official",
			Name:         "MaClaw Official",
			AccessPolicy: AccessPolicyFree,
			Models:       []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "official", Model: "qax-codegen/Auto"}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	usage := &recordingUsageRecorder{}
	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
		HTTPClient:  upstream.Client(),
		Usage:       usage,
	}
	resp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{
		HubID:    "hub1",
		TenantID: "t1",
		Body:     map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hello"}}},
	})
	if err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if len(usage.records) != 1 {
		t.Fatalf("usage records = %d, want 1", len(usage.records))
	}
	if usage.records[0].InputTokens != 11 || usage.records[0].OutputTokens != 7 {
		t.Fatalf("usage record = %+v, want input=11 output=7", usage.records[0])
	}
	var payload map[string]any
	if err := json.Unmarshal(resp.Body, &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	usagePayload, _ := payload["usage"].(map[string]any)
	if usagePayload["estimated"] == true {
		t.Fatalf("existing input/output usage should not be marked estimated: %#v", usagePayload)
	}
	if usagePayload["input_tokens"].(float64) != 11 || usagePayload["output_tokens"].(float64) != 7 {
		t.Fatalf("usage payload = %#v, want original input/output fields", usagePayload)
	}
}

func TestProxyResponseUsageWithFallbackPatchesMissingUsageShapes(t *testing.T) {
	reqBody := map[string]any{"messages": []any{map[string]any{"role": "user", "content": "hello"}}}

	input, output, patched := proxyResponseUsageWithFallback(reqBody, []byte(`{"choices":[{"message":{"content":"fallback answer"}}]}`))
	if input <= 0 || output <= 0 || !bytes.Contains(patched, []byte(`"estimated":true`)) {
		t.Fatalf("missing usage fallback = %d/%d %s, want estimated usage", input, output, patched)
	}

	input, output, patched = proxyResponseUsageWithFallback(reqBody, []byte(`{"choices":[{"message":{"content":"fallback answer"}}],"usage":{"prompt_tokens":9}}`))
	if input != 9 || output <= 0 || !bytes.Contains(patched, []byte(`"completion_tokens"`)) {
		t.Fatalf("partial usage fallback = %d/%d %s, want completed output usage", input, output, patched)
	}

	body := []byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`)
	input, output, patched = proxyResponseUsageWithFallback(reqBody, body)
	if input != 3 || output != 2 || string(patched) != string(body) {
		t.Fatalf("complete usage fallback = %d/%d %s, want original body", input, output, patched)
	}
}
func TestExtractTokenUsageAcceptsStringNumbers(t *testing.T) {
	input, output := extractTokenUsage([]byte(`{"usage":{"prompt_tokens":"12.0","completion_tokens":"8","total_tokens":"20"}}`))
	if input != 12 || output != 8 {
		t.Fatalf("usage = %d/%d, want 12/8", input, output)
	}
}

func TestExtractTokenUsageInfersMissingSideFromTotal(t *testing.T) {
	input, output := extractTokenUsage([]byte(`{"usage":{"completion_tokens":8,"total_tokens":20}}`))
	if input != 12 || output != 8 {
		t.Fatalf("usage = %d/%d, want 12/8", input, output)
	}
	input, output = extractTokenUsage([]byte(`{"usage":{"prompt_tokens":12,"total_tokens":20}}`))
	if input != 12 || output != 8 {
		t.Fatalf("usage = %d/%d, want 12/8", input, output)
	}
}

func TestEstimateProxyResponseTokensIncludesStructuredContentAndToolCalls(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"content":[{"type":"text","text":"structured answer"}],"tool_calls":[{"function":{"name":"lookup","arguments":"{\"id\":\"T-1\"}"}}],"function_call":{"name":"legacy_lookup","arguments":"{\"id\":\"T-2\"}"}}},{"text":"legacy completion text"}]}`)
	if got := estimateProxyResponseTokens(body); got <= 0 {
		t.Fatalf("estimateProxyResponseTokens() = %d, want positive", got)
	}
}

func TestEstimateProxyTokenUsageIncludesResponsesInputAndOutput(t *testing.T) {
	input, output := estimateProxyTokenUsage(
		map[string]any{"input": "hello responses", "instructions": "be concise"},
		[]byte(`{"output":[{"type":"message","content":[{"type":"output_text","text":"responses answer"}]}]}`),
	)
	if input <= 0 || output <= 0 {
		t.Fatalf("usage = %d/%d, want positive input/output", input, output)
	}
}

func TestEstimateProxyTokenUsageCountsToolSchemas(t *testing.T) {
	input, _ := estimateProxyTokenUsage(
		map[string]any{
			"messages":        []any{map[string]any{"role": "user", "content": "use tool"}},
			"response_format": map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "ticket", "schema": map[string]any{"type": "object", "properties": map[string]any{"status": map[string]any{"type": "string"}}}}},
			"tool_choice":     map[string]any{"type": "function", "function": map[string]any{"name": "lookup_ticket"}},
			"tools": []any{map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        "lookup_ticket",
					"description": "look up a ticket by id",
					"parameters":  map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string", "description": "ticket id"}}},
				},
			}},
		},
		[]byte(`{"choices":[{"message":{"content":"ok"}}]}`),
	)
	withoutTools, _ := estimateProxyTokenUsage(
		map[string]any{"messages": []any{map[string]any{"role": "user", "content": "use tool"}}},
		[]byte(`{"choices":[{"message":{"content":"ok"}}]}`),
	)
	if input <= withoutTools {
		t.Fatalf("input with tools = %d, without tools = %d, want tool schema counted", input, withoutTools)
	}
}

func TestHandleProxyRequestFallsBackToSingleProviderModel(t *testing.T) {
	var seen map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer upstream.Close()

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{{ID: "official", Name: "MaClaw Official", APIURL: upstream.URL, Models: []string{"qax-codegen/Auto"}}},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "maclaw-official",
			Name:         "MaClaw Official",
			AccessPolicy: AccessPolicyFree,
			Models:       []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "official"}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
		HTTPClient:  upstream.Client(),
	}
	if _, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{
		HubID:    "hub1",
		TenantID: "t1",
		Body:     map[string]any{"model": "auto", "messages": []any{}},
	}); err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if seen["model"] != "qax-codegen/Auto" {
		t.Fatalf("upstream model = %#v, want provider model fallback", seen["model"])
	}
}

func TestHandleProxyRequestSupportsLegacyProviderIDs(t *testing.T) {
	var seen map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer upstream.Close()

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{{ID: "legacy", Name: "Legacy", APIURL: upstream.URL, Models: []string{"legacy-auto"}}},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "legacy-group",
			Name:         "Legacy Group",
			AccessPolicy: AccessPolicyFree,
			Models:       []llmpool.ModelConfig{{Name: "auto", ProviderIDs: []string{"legacy"}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
		HTTPClient:  upstream.Client(),
	}
	resp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{
		HubID:    "hub1",
		TenantID: "t1",
		Body:     map[string]any{"model": "auto", "messages": []any{}},
	})
	if err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if seen["model"] != "legacy-auto" {
		t.Fatalf("upstream model = %#v, want legacy provider single-model fallback", seen["model"])
	}
}

func TestHandleProxyRequestAllowsSameProviderWithDifferentModelRoutes(t *testing.T) {
	var seen map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer upstream.Close()

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{{ID: "deepseek", Name: "DeepSeek", APIURL: upstream.URL}},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "redeem",
			Name:         "Redeem",
			AccessPolicy: AccessPolicyFree,
			Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{
				{ProviderID: "deepseek", Model: "deepseek-v4-flash", Priority: 10, ResolutionTier: 1, CreditMultiplier: 1},
				{ProviderID: "deepseek", Model: "deepseek-v4-pro", Priority: 50, ResolutionTier: 2, CreditMultiplier: 2},
			}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
		HTTPClient:  upstream.Client(),
	}
	if _, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{
		HubID:    "hub1",
		TenantID: "t1",
		Body:     map[string]any{"model": "auto", "messages": []any{}},
	}); err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if seen["model"] != "deepseek-v4-pro" {
		t.Fatalf("upstream model = %#v, want priority-selected deepseek-v4-pro", seen["model"])
	}
}

func TestProxyCreditMultiplierForRouteHandlesNilModel(t *testing.T) {
	got := proxyCreditMultiplierForRoute(nil, llmpool.DispatchProviderRoute{ProviderID: "p1"})
	if got != 1 {
		t.Fatalf("multiplier = %v, want 1", got)
	}
}

func TestProxyCreditMultiplierForRouteDoesNotFallBackToProviderMapInRouteMode(t *testing.T) {
	model := &llmpool.DispatchModel{
		CreditMultiplier: 1.5,
		ProviderRoutes: []llmpool.DispatchProviderRoute{
			{ProviderID: "deepseek", Model: "deepseek-v4-flash", CreditMultiplier: 0},
			{ProviderID: "deepseek", Model: "deepseek-v4-pro", CreditMultiplier: 5},
		},
		ProviderCreditMultipliers: map[string]float64{"deepseek": 5},
	}

	got := proxyCreditMultiplierForRoute(model, model.ProviderRoutes[0])
	if got != 1.5 {
		t.Fatalf("multiplier = %v, want model fallback 1.5 without provider-map cross-talk", got)
	}
}

func TestHandleProxyRequestRetriesProviderAuthorizationFailure(t *testing.T) {
	var firstHits int
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstHits++
		http.Error(w, `{"error":{"message":"upstream key forbidden"}}`, http.StatusForbidden)
	}))
	defer first.Close()
	var secondHits int
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondHits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`))
	}))
	defer second.Close()

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "p1", Name: "P1", APIURL: first.URL},
			{ID: "p2", Name: "P2", APIURL: second.URL},
		},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "g1",
			Name:         "G1",
			AccessPolicy: AccessPolicyFree,
			Models: []llmpool.ModelConfig{{Name: "gpt-4", ProviderConfigs: []llmpool.ModelProviderConfig{
				{ProviderID: "p1"},
				{ProviderID: "p2"},
			}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
		HTTPClient:  second.Client(),
	}
	req := &ProxyRequest{HubID: "hub1", TenantID: "t1", Body: map[string]any{"model": "gpt-4"}}
	resp, err := HandleProxyRequest(context.Background(), cfg, req)
	if err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if resp.StatusCode != http.StatusOK || resp.ProviderID != "p2" {
		t.Fatalf("status/provider = %d/%q, want 200/p2", resp.StatusCode, resp.ProviderID)
	}
	if firstHits != 1 || secondHits != 1 {
		t.Fatalf("provider hits = %d/%d, want 1/1", firstHits, secondHits)
	}
}

func TestHandleProxyRequestDoesNotReturnProviderAuthorizationAsTenantDenial(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"upstream key forbidden"}}`, http.StatusForbidden)
	}))
	defer upstream.Close()

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{{ID: "p1", Name: "P1", APIURL: upstream.URL}},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "g1",
			Name:         "G1",
			AccessPolicy: AccessPolicyFree,
			Models:       []llmpool.ModelConfig{{Name: "gpt-4", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p1"}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
		HTTPClient:  upstream.Client(),
	}
	req := &ProxyRequest{HubID: "hub1", TenantID: "t1", Body: map[string]any{"model": "gpt-4"}}
	_, err := HandleProxyRequest(context.Background(), cfg, req)
	if err == nil {
		t.Fatal("HandleProxyRequest() error = nil, want provider failure")
	}
	if !contains(err.Error(), "all providers failed") || contains(err.Error(), "authorization denied") {
		t.Fatalf("error = %v, want provider failure without tenant authorization denial", err)
	}
}

func TestHandleProxyRequestProviderFailureIncludesRoutingDetails(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(550)
		_, _ = w.Write([]byte(`{"error":{"message":"upstream timeout"}}`))
	}))
	defer upstream.Close()

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{{ID: "deepseek", Name: "DeepSeek", APIURL: upstream.URL}},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "redeem",
			Name:         "Redeem",
			AccessPolicy: AccessPolicyFree,
			Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{
				ProviderID: "deepseek",
				Model:      "deepseek-v4-flash",
			}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	_, err := HandleProxyRequest(context.Background(), &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
		HTTPClient:  upstream.Client(),
	}, &ProxyRequest{
		HubID:    "hub1",
		TenantID: "tenant1",
		Body:     map[string]any{"model": "auto", "messages": []any{}},
	})
	if err == nil {
		t.Fatal("HandleProxyRequest() error = nil, want provider failure")
	}
	for _, want := range []string{"all providers failed", "deepseek", "logical model auto", "upstream model deepseek-v4-flash", "550", "upstream timeout"} {
		if !contains(err.Error(), want) {
			t.Fatalf("error = %v, want %q", err, want)
		}
	}
}

func TestHandleProxyRequest_CacheHit(t *testing.T) {
	system := &mockSystemSettings{}
	svc := NewService(system)
	_ = svc.SaveRegistry(context.Background(), &Registry{
		Providers:     []llmpool.ProviderConfig{{ID: "p1", Name: "P1", APIURL: "http://localhost"}},
		ServiceGroups: []llmpool.ServiceGroup{{ID: "g1", Name: "G1", Models: []llmpool.ModelConfig{{Name: "gpt-4", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p1"}}}}}},
	})

	authRepo := &mockAuthRepo{auths: []*TenantAuthorization{{
		ID: "auth1", HubID: "hub1", TenantID: "t1", ServiceGroupID: "g1",
		CreditsTotal: 1000, StartsAt: time.Now().Add(-time.Hour), ExpiresAt: time.Now().Add(time.Hour),
		Status: "active",
	}}}

	cache := llmpool.NewCache(nil, llmpool.CacheConfig{MemoryMaxEntries: 10})
	// Pre-populate cache
	body := map[string]any{"model": "gpt-4", "messages": []any{map[string]any{"role": "user", "content": "hello"}}}
	cacheKey := buildServiceGroupCacheKey("g1", "gpt-4", body)
	_ = cache.Put(context.Background(), &llmpool.CacheEntry{
		CacheKey:   cacheKey,
		ProviderID: "p1",
		Model:      "gpt-4",
		Payload:    []byte(`{"choices":[{"message":{"content":"cached"}}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`),
	})

	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(authRepo),
		Cache:       cache,
	}
	req := &ProxyRequest{HubID: "hub1", TenantID: "t1", Body: body}
	resp, err := HandleProxyRequest(context.Background(), cfg, req)
	if err != nil {
		t.Fatal(err)
	}
	if !resp.CacheHit {
		t.Fatal("expected cache hit")
	}
	if resp.ProviderID != "p1" {
		t.Fatalf("expected provider p1, got %s", resp.ProviderID)
	}
	if bytes.Contains(resp.Body, []byte(`"usage"`)) {
		t.Fatalf("cache hit response should not include billable usage: %s", resp.Body)
	}
}

func TestHandleProxyRequestCacheIsScopedByServiceGroup(t *testing.T) {
	var providerCalls int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"ok","model":"free-upstream","choices":[{"message":{"content":"free-live"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`))
	}))
	defer upstream.Close()

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "paid", Name: "Paid", APIURL: upstream.URL},
			{ID: "free", Name: "Free", APIURL: upstream.URL},
		},
		ServiceGroups: []llmpool.ServiceGroup{
			{
				ID:           "paid-group",
				Name:         "Paid Group",
				AccessPolicy: AccessPolicyFree,
				Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{
					ProviderID: "paid",
					Model:      "paid-upstream",
				}}}},
			},
			{
				ID:           "system-free",
				Name:         "System Free",
				AccessPolicy: AccessPolicyFree,
				Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{
					ProviderID: "free",
					Model:      "free-upstream",
				}}}},
			},
		},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	body := map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hello"}}}
	cache := llmpool.NewCache(nil, llmpool.CacheConfig{MemoryMaxEntries: 10})
	_ = cache.Put(context.Background(), &llmpool.CacheEntry{
		CacheKey:   buildServiceGroupCacheKey("paid-group", "auto", body),
		ProviderID: "paid",
		Model:      "auto",
		Payload:    []byte(`{"choices":[{"message":{"content":"paid-cached"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`),
	})

	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
		Cache:       cache,
		HTTPClient:  upstream.Client(),
	}
	resp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{
		HubID:          "hub1",
		TenantID:       "tenant1",
		ServiceGroupID: "system-free",
		Body:           body,
	})
	if err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if resp.CacheHit {
		t.Fatalf("system-free request must not hit paid-group cache")
	}
	if providerCalls != 1 {
		t.Fatalf("provider calls = %d, want 1", providerCalls)
	}
	if resp.ProviderID != "free" {
		t.Fatalf("provider = %q, want free", resp.ProviderID)
	}
	if !bytes.Contains(resp.Body, []byte("free-live")) {
		t.Fatalf("response body = %s, want live free response", resp.Body)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsSubstring(s, sub))
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
