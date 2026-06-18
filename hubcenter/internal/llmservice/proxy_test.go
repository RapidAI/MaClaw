package llmservice

import (
	"bytes"
	"context"
	"encoding/json"
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
func (r *mockAuthRepo) DeductCredits(_ context.Context, id string, credits float64, _ time.Time) error {
	for _, a := range r.auths {
		if a.ID == id {
			a.CreditsUsed += credits
		}
	}
	return nil
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

	// No authorizations → access denied
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

	resp, err := forwardToProvider(context.Background(), client, provider, body, "auto")
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
	cacheKey := buildCacheKey("gpt-4", body)
	_ = cache.Put(context.Background(), &llmpool.CacheEntry{
		CacheKey:   cacheKey,
		ProviderID: "p1",
		Model:      "gpt-4",
		Payload:    []byte(`{"choices":[{"message":{"content":"cached"}}]}`),
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
