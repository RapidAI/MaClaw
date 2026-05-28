package entry

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store/sqlite"
)

type countingHubRepo struct {
	store.HubRepository
	mu           sync.Mutex
	listAllCalls int
}

func (r *countingHubRepo) ListAll(ctx context.Context) ([]*store.HubInstance, error) {
	r.mu.Lock()
	r.listAllCalls++
	r.mu.Unlock()
	return r.HubRepository.ListAll(ctx)
}

func (r *countingHubRepo) ListAllCalls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.listAllCalls
}

type countingHubDomainRouteRepo struct {
	store.HubDomainRouteRepository
	mu           sync.Mutex
	listAllCalls int
}

func (r *countingHubDomainRouteRepo) ListAll(ctx context.Context) ([]*store.HubDomainRoute, error) {
	r.mu.Lock()
	r.listAllCalls++
	r.mu.Unlock()
	return r.HubDomainRouteRepository.ListAll(ctx)
}

func (r *countingHubDomainRouteRepo) ListAllCalls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.listAllCalls
}

type pagedCountingHubRepo struct {
	store.HubRepository
	mu            sync.Mutex
	listAllCalls  int
	listPageCalls int
}

func (r *pagedCountingHubRepo) ListAll(ctx context.Context) ([]*store.HubInstance, error) {
	r.mu.Lock()
	r.listAllCalls++
	r.mu.Unlock()
	return r.HubRepository.ListAll(ctx)
}

func (r *pagedCountingHubRepo) ListPage(ctx context.Context, offset, limit int) ([]*store.HubInstance, error) {
	r.mu.Lock()
	r.listPageCalls++
	r.mu.Unlock()
	return r.HubRepository.(hubPageLister).ListPage(ctx, offset, limit)
}

func (r *pagedCountingHubRepo) Calls() (int, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.listAllCalls, r.listPageCalls
}

type pagedCountingHubUserLinkRepo struct {
	store.HubUserLinkRepository
	mu            sync.Mutex
	listAllCalls  int
	listPageCalls int
}

func (r *pagedCountingHubUserLinkRepo) ListAll(ctx context.Context) ([]*store.HubUserLink, error) {
	r.mu.Lock()
	r.listAllCalls++
	r.mu.Unlock()
	return r.HubUserLinkRepository.ListAll(ctx)
}

func (r *pagedCountingHubUserLinkRepo) ListPage(ctx context.Context, offset, limit int) ([]*store.HubUserLink, error) {
	r.mu.Lock()
	r.listPageCalls++
	r.mu.Unlock()
	return r.HubUserLinkRepository.(hubUserLinkPageLister).ListPage(ctx, offset, limit)
}

func (r *pagedCountingHubUserLinkRepo) Calls() (int, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.listAllCalls, r.listPageCalls
}

type pagedCountingHubDomainRouteRepo struct {
	store.HubDomainRouteRepository
	mu            sync.Mutex
	listAllCalls  int
	listPageCalls int
}

func (r *pagedCountingHubDomainRouteRepo) ListAll(ctx context.Context) ([]*store.HubDomainRoute, error) {
	r.mu.Lock()
	r.listAllCalls++
	r.mu.Unlock()
	return r.HubDomainRouteRepository.ListAll(ctx)
}

func (r *pagedCountingHubDomainRouteRepo) ListPage(ctx context.Context, offset, limit int) ([]*store.HubDomainRoute, error) {
	r.mu.Lock()
	r.listPageCalls++
	r.mu.Unlock()
	return r.HubDomainRouteRepository.(hubDomainRoutePageLister).ListPage(ctx, offset, limit)
}

func (r *pagedCountingHubDomainRouteRepo) Calls() (int, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.listAllCalls, r.listPageCalls
}

type pagedCountingBlockedEmailRepo struct {
	store.BlockedEmailRepository
	mu            sync.Mutex
	listCalls     int
	listPageCalls int
}

func (r *pagedCountingBlockedEmailRepo) List(ctx context.Context) ([]*store.BlockedEmail, error) {
	r.mu.Lock()
	r.listCalls++
	r.mu.Unlock()
	return r.BlockedEmailRepository.List(ctx)
}

func (r *pagedCountingBlockedEmailRepo) ListPage(ctx context.Context, offset, limit int) ([]*store.BlockedEmail, error) {
	r.mu.Lock()
	r.listPageCalls++
	r.mu.Unlock()
	return r.BlockedEmailRepository.(blockedEmailPageLister).ListPage(ctx, offset, limit)
}

func (r *pagedCountingBlockedEmailRepo) Calls() (int, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.listCalls, r.listPageCalls
}

type pagedCountingBlockedIPRepo struct {
	store.BlockedIPRepository
	mu            sync.Mutex
	listCalls     int
	listPageCalls int
}

func (r *pagedCountingBlockedIPRepo) List(ctx context.Context) ([]*store.BlockedIP, error) {
	r.mu.Lock()
	r.listCalls++
	r.mu.Unlock()
	return r.BlockedIPRepository.List(ctx)
}

func (r *pagedCountingBlockedIPRepo) ListPage(ctx context.Context, offset, limit int) ([]*store.BlockedIP, error) {
	r.mu.Lock()
	r.listPageCalls++
	r.mu.Unlock()
	return r.BlockedIPRepository.(blockedIPPageLister).ListPage(ctx, offset, limit)
}

func (r *pagedCountingBlockedIPRepo) Calls() (int, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.listCalls, r.listPageCalls
}

func newTestStore(t *testing.T) *store.Store {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "hubcenter-entry-test.db")
	provider, err := sqlite.NewProvider(sqlite.Config{
		DSN:               dbPath,
		WAL:               true,
		BusyTimeoutMS:     5000,
		MaxReadOpenConns:  4,
		MaxReadIdleConns:  2,
		MaxWriteOpenConns: 1,
		MaxWriteIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	if err := sqlite.RunMigrations(provider.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	t.Cleanup(func() {
		_ = provider.Close()
	})

	return sqlite.NewStore(provider)
}

func TestResolveByEmailUsesOfficialTenantPublicFallbackPolicy(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	hub := &store.HubInstance{ID: "hub_official_public", OwnerEmail: "owner@example.com", Name: "Official Hub", BaseURL: "https://official.example.com", Visibility: "private", EnrollmentMode: "open", Status: "online", HubSecretHash: "secret", CreatedAt: now, UpdatedAt: now}
	if err := st.Hubs.Create(ctx, hub); err != nil {
		t.Fatalf("create hub: %v", err)
	}
	if err := st.System.Set(ctx, systemKeyHubRegistrationPolicies, `{"hubs":{"hub_official_public":{"hub_origin":"official","default_signup_scope":"domain_restricted","tenants":{"public":{"tenant_id":"public","signup_scope":"public","is_public_fallback":true,"invite_enabled":true,"max_active_invites":100,"monthly_invite_quota":500,"per_invite_max_uses_default":1,"per_invite_max_uses_max":20,"status":"active"}}}}}`); err != nil {
		t.Fatalf("set policy: %v", err)
	}

	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs, st.System)
	result, err := svc.ResolveByEmail(ctx, "scattered@example.net")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if result.Mode != "single" || len(result.Hubs) != 1 {
		t.Fatalf("expected one public fallback hub, got mode=%s hubs=%d", result.Mode, len(result.Hubs))
	}
	if result.Hubs[0].HubID != hub.ID || result.Hubs[0].TenantID != "public" {
		t.Fatalf("unexpected fallback route: %+v", result.Hubs[0])
	}
}

func TestResolveByEmailNormalizesStoredRegistrationPolicy(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	hub := &store.HubInstance{ID: "hub_official_policy_case", OwnerEmail: "owner@example.com", Name: "Official Case Hub", BaseURL: "https://official-case.example.com", Visibility: "private", EnrollmentMode: "open", Status: "online", HubSecretHash: "secret", CreatedAt: now, UpdatedAt: now}
	if err := st.Hubs.Create(ctx, hub); err != nil {
		t.Fatalf("create hub: %v", err)
	}
	if err := st.System.Set(ctx, systemKeyHubRegistrationPolicies, `{"hubs":{"hub_official_policy_case":{"hub_origin":"OFFICIAL","default_signup_scope":"PUBLIC","tenants":{"tenant_default":{"signup_scope":"INHERIT","is_public_fallback":true,"status":"ACTIVE"}}}}}`); err != nil {
		t.Fatalf("set policy: %v", err)
	}

	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs, st.System)
	result, err := svc.ResolveByEmail(ctx, "case@example.net")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if result.Mode != "single" || len(result.Hubs) != 1 || result.Hubs[0].HubID != hub.ID || result.Hubs[0].TenantID != "" {
		t.Fatalf("unexpected normalized fallback route: %+v", result)
	}
}

func TestResolveByEmailUsesOfficialDefaultTenantPublicFallbackPolicy(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	hub := &store.HubInstance{ID: "hub_official_default_public", OwnerEmail: "owner@example.com", Name: "Official Default Hub", BaseURL: "https://official-default.example.com", Visibility: "private", EnrollmentMode: "open", Status: "online", HubSecretHash: "secret", CreatedAt: now, UpdatedAt: now}
	if err := st.Hubs.Create(ctx, hub); err != nil {
		t.Fatalf("create hub: %v", err)
	}
	if err := st.System.Set(ctx, systemKeyHubRegistrationPolicies, `{"hubs":{"hub_official_default_public":{"hub_origin":"official","default_signup_scope":"public","tenants":{"":{"tenant_id":"","signup_scope":"inherit","is_public_fallback":true,"status":"active"}}}}}`); err != nil {
		t.Fatalf("set policy: %v", err)
	}

	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs, st.System)
	result, err := svc.ResolveByEmail(ctx, "scattered@example.net")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if result.Mode != "single" || len(result.Hubs) != 1 || result.Hubs[0].HubID != hub.ID || result.Hubs[0].TenantID != "" {
		t.Fatalf("unexpected fallback route: %+v", result)
	}
}

func TestResolveByEmailUsesHubRowRegistrationPolicyFallback(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	hub := &store.HubInstance{ID: "hub_official_row_public", HubOrigin: "official", DefaultSignupScope: "public", RegistrationPolicyJSON: `{"tenants":{"public":{"tenant_id":"public","signup_scope":"public","is_public_fallback":true,"status":"active"}}}`, OwnerEmail: "owner@example.com", Name: "Official Row Hub", BaseURL: "https://official-row.example.com", Visibility: "private", EnrollmentMode: "open", Status: "online", HubSecretHash: "secret", CreatedAt: now, UpdatedAt: now}
	if err := st.Hubs.Create(ctx, hub); err != nil {
		t.Fatalf("create hub: %v", err)
	}

	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs, st.System)
	result, err := svc.ResolveByEmail(ctx, "scattered@example.net")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if result.Mode != "single" || len(result.Hubs) != 1 || result.Hubs[0].HubID != hub.ID || result.Hubs[0].TenantID != "public" {
		t.Fatalf("unexpected row fallback route: %+v", result)
	}
}

func TestResolveByEmailConfiguredSelfHostedHubDoesNotBecomePublicFallback(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	hub := &store.HubInstance{ID: "hub_enterprise_no_domain", OwnerEmail: "owner@example.com", Name: "Enterprise Hub", BaseURL: "https://enterprise.example.com", Visibility: "shared", EnrollmentMode: "open", Status: "online", HubSecretHash: "secret", CreatedAt: now, UpdatedAt: now}
	if err := st.Hubs.Create(ctx, hub); err != nil {
		t.Fatalf("create hub: %v", err)
	}
	if err := st.System.Set(ctx, systemKeyHubRegistrationPolicies, `{"hubs":{"hub_enterprise_no_domain":{"hub_origin":"self_hosted","default_signup_scope":"domain_restricted","tenants":{}}}}`); err != nil {
		t.Fatalf("set policy: %v", err)
	}

	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs, st.System)
	result, err := svc.ResolveByEmail(ctx, "guest@example.net")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if result.Mode != "none" || len(result.Hubs) != 0 {
		t.Fatalf("expected no fallback route, got mode=%s hubs=%d", result.Mode, len(result.Hubs))
	}
}

func TestResolveByEmailSharedHubWithoutPublicSignupDoesNotFallback(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	hub := &store.HubInstance{ID: "hub_shared_no_public_signup", OwnerEmail: "owner@example.com", Name: "Shared Enterprise Hub", BaseURL: "https://shared-enterprise.example.com", Visibility: "shared", EnrollmentMode: "open", Status: "online", HubSecretHash: "secret", CreatedAt: now, UpdatedAt: now}
	if err := st.Hubs.Create(ctx, hub); err != nil {
		t.Fatalf("create hub: %v", err)
	}

	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs, st.System)
	result, err := svc.ResolveByEmail(ctx, "guest@example.net")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if result.Mode != "none" || len(result.Hubs) != 0 {
		t.Fatalf("expected shared hub without accept_public_signup to stay out of fallback, got %+v", result)
	}
}

func TestResolveByEmailPublicFallbackRequiresEffectivePublicScope(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	hub := &store.HubInstance{ID: "hub_official_inherit_restricted", OwnerEmail: "owner@example.com", Name: "Official Restricted", BaseURL: "https://official-restricted.example.com", Visibility: "private", EnrollmentMode: "open", Status: "online", HubSecretHash: "secret", CreatedAt: now, UpdatedAt: now}
	if err := st.Hubs.Create(ctx, hub); err != nil {
		t.Fatalf("create hub: %v", err)
	}
	if err := st.System.Set(ctx, systemKeyHubRegistrationPolicies, `{"hubs":{"hub_official_inherit_restricted":{"hub_origin":"official","default_signup_scope":"domain_restricted","tenants":{"public":{"tenant_id":"public","signup_scope":"inherit","is_public_fallback":true,"status":"active"}}}}}`); err != nil {
		t.Fatalf("set policy: %v", err)
	}

	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs, st.System)
	result, err := svc.ResolveByEmail(ctx, "guest@example.net")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if result.Mode != "none" || len(result.Hubs) != 0 {
		t.Fatalf("expected inherit/domain_restricted fallback to be ignored, got %+v", result)
	}
}

func TestResolveByEmailPrefersDefaultLinkedHub(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	hubA := &store.HubInstance{
		ID:             "hub_a",
		OwnerEmail:     "owner@example.com",
		Name:           "Hub A",
		BaseURL:        "https://hub-a.example.com",
		Visibility:     "shared",
		EnrollmentMode: "open",
		Status:         "online",
		HubSecretHash:  "secret-a",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	hubB := &store.HubInstance{
		ID:             "hub_b",
		OwnerEmail:     "owner@example.com",
		Name:           "Hub B",
		BaseURL:        "https://hub-b.example.com",
		Visibility:     "shared",
		EnrollmentMode: "approval",
		Status:         "online",
		HubSecretHash:  "secret-b",
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := st.Hubs.Create(ctx, hubA); err != nil {
		t.Fatalf("create hubA: %v", err)
	}
	if err := st.Hubs.Create(ctx, hubB); err != nil {
		t.Fatalf("create hubB: %v", err)
	}
	if err := st.HubUserLinks.Create(ctx, &store.HubUserLink{
		ID:        "link_a",
		HubID:     hubA.ID,
		Email:     "user@example.com",
		IsDefault: false,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create link_a: %v", err)
	}
	if err := st.HubUserLinks.Create(ctx, &store.HubUserLink{
		ID:        "link_b",
		HubID:     hubB.ID,
		Email:     "user@example.com",
		IsDefault: true,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create link_b: %v", err)
	}

	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs)
	result, err := svc.ResolveByEmail(ctx, "user@example.com")
	if err != nil {
		t.Fatalf("ResolveByEmail: %v", err)
	}
	if result == nil || result.Mode != "multiple" {
		t.Fatalf("unexpected resolve result: %+v", result)
	}
	if result.DefaultHubID != "hub_b" {
		t.Fatalf("expected default hub_b, got %q", result.DefaultHubID)
	}
	if result.DefaultPWA != "https://hub-b.example.com/app?email=user%40example.com&entry=app&autologin=1" {
		t.Fatalf("unexpected default pwa: %q", result.DefaultPWA)
	}
	if len(result.Hubs) != 2 {
		t.Fatalf("expected 2 hubs, got %d", len(result.Hubs))
	}
}

func TestResolveByEmailIgnoresOwnerHubLinksForEntryRouting(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	corpHub := &store.HubInstance{
		ID:                   "hub_corp",
		OwnerEmail:           "znsoft@163.com",
		Name:                 "Corporate Hub",
		BaseURL:              "https://corp.example.com",
		Visibility:           "shared",
		EnrollmentMode:       "open",
		CorporateEmailDomain: "rapidai.tech",
		AcceptPublicSignup:   false,
		Status:               "online",
		HubSecretHash:        "secret-corp",
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	publicHub := &store.HubInstance{
		ID:                 "hub_public",
		OwnerEmail:         "owner@example.com",
		Name:               "Public Hub",
		BaseURL:            "https://public.example.com",
		Visibility:         "shared",
		EnrollmentMode:     "open",
		AcceptPublicSignup: true,
		Status:             "online",
		HubSecretHash:      "secret-public",
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	for _, hub := range []*store.HubInstance{corpHub, publicHub} {
		if err := st.Hubs.Create(ctx, hub); err != nil {
			t.Fatalf("create hub %s: %v", hub.ID, err)
		}
	}
	if err := st.HubUserLinks.Create(ctx, &store.HubUserLink{
		ID:        "hul_owner_" + corpHub.ID,
		HubID:     corpHub.ID,
		Email:     "znsoft@163.com",
		IsDefault: true,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create owner link: %v", err)
	}

	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs)
	result, err := svc.ResolveByEmail(ctx, "znsoft@163.com")
	if err != nil {
		t.Fatalf("ResolveByEmail: %v", err)
	}
	if result == nil || result.Mode != "single" {
		t.Fatalf("unexpected resolve result: %+v", result)
	}
	if result.DefaultHubID != publicHub.ID {
		t.Fatalf("expected public hub only, got %q", result.DefaultHubID)
	}
	if len(result.Hubs) != 1 || result.Hubs[0].HubID != publicHub.ID {
		t.Fatalf("expected only public hub, got %+v", result.Hubs)
	}
}

func TestResolveAdminByEmailIgnoresOwnerOnlyLinks(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	corpHub := &store.HubInstance{
		ID:                   "hub_corp_admin",
		OwnerEmail:           "znsoft@163.com",
		Name:                 "Corporate Hub",
		BaseURL:              "https://corp.example.com",
		Visibility:           "shared",
		EnrollmentMode:       "open",
		CorporateEmailDomain: "rapidai.tech",
		AcceptPublicSignup:   false,
		Status:               "online",
		HubSecretHash:        "secret-corp-admin",
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	publicHub := &store.HubInstance{
		ID:                 "hub_public_admin",
		OwnerEmail:         "owner@example.com",
		Name:               "Public Hub",
		BaseURL:            "https://public.example.com",
		Visibility:         "shared",
		EnrollmentMode:     "open",
		AcceptPublicSignup: true,
		Status:             "online",
		HubSecretHash:      "secret-public-admin",
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	for _, hub := range []*store.HubInstance{corpHub, publicHub} {
		if err := st.Hubs.Create(ctx, hub); err != nil {
			t.Fatalf("create hub %s: %v", hub.ID, err)
		}
	}
	if err := st.HubUserLinks.Create(ctx, &store.HubUserLink{
		ID:        "hul_owner_" + corpHub.ID,
		HubID:     corpHub.ID,
		Email:     "znsoft@163.com",
		IsDefault: true,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create owner link: %v", err)
	}

	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs)
	result, err := svc.ResolveAdminByEmail(ctx, "znsoft@163.com")
	if err != nil {
		t.Fatalf("ResolveAdminByEmail: %v", err)
	}
	if result == nil || result.Mode != "none" {
		t.Fatalf("unexpected resolve result: %+v", result)
	}
	if result.DefaultHubID != "" {
		t.Fatalf("expected no default hub, got %q", result.DefaultHubID)
	}
	if len(result.Hubs) != 0 {
		t.Fatalf("expected no hubs when only owner links exist, got %+v", result.Hubs)
	}
}

func TestResolveAdminByEmailPrefersDirectUserLinkOverOwnerLinks(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	directHub := &store.HubInstance{
		ID:                 "hub_direct_admin",
		OwnerEmail:         "owner@example.com",
		Name:               "Direct Hub",
		BaseURL:            "https://direct.example.com",
		Visibility:         "shared",
		EnrollmentMode:     "open",
		AcceptPublicSignup: true,
		Status:             "online",
		HubSecretHash:      "secret-direct-admin",
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	ownerHub := &store.HubInstance{
		ID:                 "hub_owner_admin",
		OwnerEmail:         "znsoft@163.com",
		Name:               "Owner Hub",
		BaseURL:            "https://owner.example.com",
		Visibility:         "shared",
		EnrollmentMode:     "open",
		AcceptPublicSignup: true,
		Status:             "online",
		HubSecretHash:      "secret-owner-admin",
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	for _, hub := range []*store.HubInstance{directHub, ownerHub} {
		if err := st.Hubs.Create(ctx, hub); err != nil {
			t.Fatalf("create hub %s: %v", hub.ID, err)
		}
	}
	if err := st.HubUserLinks.Create(ctx, &store.HubUserLink{
		ID:        "link_direct_admin",
		HubID:     directHub.ID,
		Email:     "znsoft@163.com",
		IsDefault: true,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create direct link: %v", err)
	}
	if err := st.HubUserLinks.Create(ctx, &store.HubUserLink{
		ID:        "hul_owner_" + ownerHub.ID,
		HubID:     ownerHub.ID,
		Email:     "znsoft@163.com",
		IsDefault: true,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create owner link: %v", err)
	}

	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs)
	result, err := svc.ResolveAdminByEmail(ctx, "znsoft@163.com")
	if err != nil {
		t.Fatalf("ResolveAdminByEmail: %v", err)
	}
	if result == nil || result.Mode != "single" {
		t.Fatalf("unexpected resolve result: %+v", result)
	}
	if result.DefaultHubID != directHub.ID {
		t.Fatalf("expected direct hub first, got %q", result.DefaultHubID)
	}
	if len(result.Hubs) != 1 || result.Hubs[0].HubID != directHub.ID {
		t.Fatalf("expected only direct hub, got %+v", result.Hubs)
	}
}

func TestResolveAdminByEmailPatternIncludesInventoryHiddenByAdminOverride(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	sourceHub := &store.HubInstance{
		ID:               "hub_mypapers",
		OwnerEmail:       "owner-papers@example.com",
		Name:             "Papers",
		BaseURL:          "https://hub.mypapers.top",
		Visibility:       "private",
		EnrollmentMode:   "open",
		Status:           "online",
		HubSecretHash:    "secret-papers",
		CapabilitiesJSON: `{"user_emails":["xx@qianxin.com"],"supports_user_data_migration":true}`,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	targetHub := &store.HubInstance{
		ID:             "hub_maclaw",
		OwnerEmail:     "owner-maclaw@example.com",
		Name:           "Maclaw",
		BaseURL:        "https://hub.maclaw.top",
		Visibility:     "private",
		EnrollmentMode: "open",
		Status:         "online",
		HubSecretHash:  "secret-maclaw",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	for _, hub := range []*store.HubInstance{sourceHub, targetHub} {
		if err := st.Hubs.Create(ctx, hub); err != nil {
			t.Fatalf("create hub %s: %v", hub.ID, err)
		}
	}
	if err := st.HubUserLinks.Create(ctx, &store.HubUserLink{
		ID:        "hul_admin_xx_qianxin",
		HubID:     targetHub.ID,
		Email:     "xx@qianxin.com",
		IsDefault: true,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create admin migration link: %v", err)
	}

	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs)
	adminResult, err := svc.ResolveAdminByEmailPattern(ctx, "*@qianxin.com")
	if err != nil {
		t.Fatalf("ResolveAdminByEmailPattern: %v", err)
	}
	if adminResult == nil || adminResult.Mode != "multiple" || !resultHasHub(adminResult, sourceHub.ID) || !resultHasHub(adminResult, targetHub.ID) {
		t.Fatalf("expected admin inventory search to show source and target hubs, got %+v", adminResult)
	}

	entryResult, err := svc.ResolveByEmail(ctx, "xx@qianxin.com")
	if err != nil {
		t.Fatalf("ResolveByEmail: %v", err)
	}
	if entryResult == nil || entryResult.DefaultHubID != targetHub.ID || len(entryResult.Hubs) != 1 {
		t.Fatalf("expected normal entry routing to stay on migrated target only, got %+v", entryResult)
	}
}

func TestResolveAdminByEmailPatternNormalizesDefaultTenantInventory(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	hub := &store.HubInstance{
		ID:               "hub_default_inventory",
		OwnerEmail:       "owner@example.com",
		Name:             "Default Inventory",
		BaseURL:          "https://hub.example.com",
		Visibility:       "private",
		EnrollmentMode:   "open",
		Status:           "online",
		HubSecretHash:    "secret",
		CapabilitiesJSON: `{"user_emails":["user@example.com"],"tenant_user_emails":{"tenant_default":["user@example.com"]},"supports_user_data_migration":true}`,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := st.Hubs.Create(ctx, hub); err != nil {
		t.Fatalf("create hub: %v", err)
	}

	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs)
	result, err := svc.ResolveAdminByEmailPattern(ctx, "user@example.com")
	if err != nil {
		t.Fatalf("ResolveAdminByEmailPattern: %v", err)
	}
	if result == nil || len(result.Hubs) != 1 || result.Hubs[0].TenantID != "" {
		t.Fatalf("expected tenant_default inventory to route as default tenant, got %+v", result)
	}
}

func TestResolveAdminByEmailPatternReadsTenantInventoryWithoutFlatUserEmails(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	hub := &store.HubInstance{
		ID:               "hub_tenant_only_inventory",
		OwnerEmail:       "owner@example.com",
		Name:             "Tenant Only Inventory",
		BaseURL:          "https://hub.example.com",
		Visibility:       "private",
		EnrollmentMode:   "open",
		Status:           "online",
		HubSecretHash:    "secret",
		CapabilitiesJSON: `{"tenant_user_emails":{"tenant_a":["alice@example.com"]},"supports_user_data_migration":true}`,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := st.Hubs.Create(ctx, hub); err != nil {
		t.Fatalf("create hub: %v", err)
	}

	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs)
	result, err := svc.ResolveAdminByEmailPattern(ctx, "alice@example.com")
	if err != nil {
		t.Fatalf("ResolveAdminByEmailPattern: %v", err)
	}
	if result == nil || len(result.Hubs) != 1 || result.Hubs[0].TenantID != "tenant_a" {
		t.Fatalf("expected tenant inventory without flat user_emails to be visible, got %+v", result)
	}
}

func TestResolveByEmailBlocked(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	if err := st.BlockedEmails.Create(ctx, &store.BlockedEmail{
		ID:        "blocked_1",
		Email:     "blocked@example.com",
		Reason:    "abuse",
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create blocked email: %v", err)
	}

	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs)
	result, err := svc.ResolveByEmail(ctx, "blocked@example.com")
	if err != nil {
		t.Fatalf("ResolveByEmail blocked: %v", err)
	}
	if result == nil || result.Mode != "none" || result.Message != "Email is blocked" {
		t.Fatalf("unexpected blocked resolve result: %+v", result)
	}
}

func TestResolveByEmailRejectsBlockedIP(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	if err := st.BlockedIPs.Create(ctx, &store.BlockedIP{
		ID:        "blocked_ip_1",
		IP:        "10.0.0.8",
		Reason:    "scanner",
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create blocked ip: %v", err)
	}

	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs)
	_, err := svc.ResolveByEmailFromIP(ctx, "user@example.com", "10.0.0.8")
	if err != ErrIPBlocked {
		t.Fatalf("expected ErrIPBlocked, got %v", err)
	}
}

func TestResolveByEmailIncludesOnlinePublicAndSharedHubs(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	privateHub := &store.HubInstance{
		ID:             "hub_private",
		OwnerEmail:     "owner@example.com",
		Name:           "Private Hub",
		BaseURL:        "https://private.example.com",
		Visibility:     "private",
		EnrollmentMode: "manual",
		Status:         "online",
		HubSecretHash:  "secret-private",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	sharedHub := &store.HubInstance{
		ID:                 "hub_shared",
		OwnerEmail:         "team@example.com",
		Name:               "Shared Hub",
		BaseURL:            "https://shared.example.com",
		Visibility:         "shared",
		EnrollmentMode:     "approval",
		AcceptPublicSignup: true,
		Status:             "online",
		HubSecretHash:      "secret-shared",
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	publicHub := &store.HubInstance{
		ID:                 "hub_public",
		OwnerEmail:         "public@example.com",
		Name:               "Public Hub",
		BaseURL:            "https://public.example.com",
		Visibility:         "public",
		EnrollmentMode:     "open",
		AcceptPublicSignup: true,
		Status:             "online",
		HubSecretHash:      "secret-public",
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	offlinePublicHub := &store.HubInstance{
		ID:             "hub_public_offline",
		OwnerEmail:     "public@example.com",
		Name:           "Offline Public Hub",
		BaseURL:        "https://offline.example.com",
		Visibility:     "public",
		EnrollmentMode: "open",
		Status:         "offline",
		HubSecretHash:  "secret-offline",
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	for _, hub := range []*store.HubInstance{privateHub, sharedHub, publicHub, offlinePublicHub} {
		if err := st.Hubs.Create(ctx, hub); err != nil {
			t.Fatalf("create hub %s: %v", hub.ID, err)
		}
	}
	if err := st.HubUserLinks.Create(ctx, &store.HubUserLink{
		ID:        "link_private",
		HubID:     privateHub.ID,
		Email:     "user@example.com",
		IsDefault: false,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create private link: %v", err)
	}

	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs)
	result, err := svc.ResolveByEmail(ctx, "user@example.com")
	if err != nil {
		t.Fatalf("ResolveByEmail: %v", err)
	}
	if result == nil || result.Mode != "multiple" {
		t.Fatalf("unexpected resolve result: %+v", result)
	}
	if len(result.Hubs) != 3 {
		t.Fatalf("expected 3 visible hubs (1 bound private + 1 shared + 1 public), got %d", len(result.Hubs))
	}
	// Bound private hub has no special priority (not default), so it sorts after shared/public.
	// Order: shared (priority 1), public (priority 2), private (priority 3).
	if result.Hubs[0].HubID != "hub_shared" {
		t.Fatalf("expected shared hub first, got %q", result.Hubs[0].HubID)
	}
	if result.Hubs[1].HubID != "hub_public" {
		t.Fatalf("expected public hub second, got %q", result.Hubs[1].HubID)
	}
	if result.Hubs[2].HubID != "hub_private" {
		t.Fatalf("expected bound private hub third, got %q", result.Hubs[2].HubID)
	}
	if result.DefaultHubID != "hub_shared" {
		t.Fatalf("expected default shared hub, got %q", result.DefaultHubID)
	}
}

func TestResolveByEmailPrefersCorporateDomainMatch(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	corpHub := &store.HubInstance{
		ID:                   "hub_qax",
		OwnerEmail:           "owner@rapidai.tech",
		Name:                 "QAX Hub",
		BaseURL:              "https://qax.example.com",
		Visibility:           "private",
		EnrollmentMode:       "open",
		CorporateEmailDomain: "rapidai.tech",
		Status:               "online",
		HubSecretHash:        "secret-qax",
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	defaultHub := &store.HubInstance{
		ID:                 "hub_default",
		OwnerEmail:         "owner@example.com",
		Name:               "Default Hub",
		BaseURL:            "https://default.example.com",
		Visibility:         "shared",
		EnrollmentMode:     "approval",
		AcceptPublicSignup: true,
		Status:             "online",
		HubSecretHash:      "secret-default",
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	for _, hub := range []*store.HubInstance{corpHub, defaultHub} {
		if err := st.Hubs.Create(ctx, hub); err != nil {
			t.Fatalf("create hub %s: %v", hub.ID, err)
		}
	}

	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs)
	result, err := svc.ResolveByEmail(ctx, "alice@rapidai.tech")
	if err != nil {
		t.Fatalf("ResolveByEmail: %v", err)
	}
	if result == nil || result.Mode != "single" {
		t.Fatalf("unexpected resolve result: %+v", result)
	}
	if result.DefaultHubID != "hub_qax" {
		t.Fatalf("expected corporate hub, got %q", result.DefaultHubID)
	}
	if len(result.Hubs) != 1 || result.Hubs[0].HubID != "hub_qax" {
		t.Fatalf("expected only corporate hub, got %+v", result.Hubs)
	}
}

func TestResolveByEmailFallsBackToDefaultCorporateHub(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	corpHub := &store.HubInstance{
		ID:                   "hub_qax",
		OwnerEmail:           "owner@rapidai.tech",
		Name:                 "QAX Hub",
		BaseURL:              "https://qax.example.com",
		Visibility:           "shared",
		EnrollmentMode:       "open",
		CorporateEmailDomain: "rapidai.tech",
		Status:               "online",
		HubSecretHash:        "secret-qax",
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	defaultHub := &store.HubInstance{
		ID:                 "hub_default",
		OwnerEmail:         "owner@example.com",
		Name:               "Default Hub",
		BaseURL:            "https://default.example.com",
		Visibility:         "shared",
		EnrollmentMode:     "approval",
		AcceptPublicSignup: true,
		Status:             "online",
		HubSecretHash:      "secret-default",
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	for _, hub := range []*store.HubInstance{corpHub, defaultHub} {
		if err := st.Hubs.Create(ctx, hub); err != nil {
			t.Fatalf("create hub %s: %v", hub.ID, err)
		}
	}

	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs)
	result, err := svc.ResolveByEmail(ctx, "bob@other.com")
	if err != nil {
		t.Fatalf("ResolveByEmail: %v", err)
	}
	if result == nil || result.Mode != "single" {
		t.Fatalf("unexpected resolve result: %+v", result)
	}
	if result.DefaultHubID != "hub_default" {
		t.Fatalf("expected default hub, got %q", result.DefaultHubID)
	}
	if len(result.Hubs) != 1 || result.Hubs[0].HubID != "hub_default" {
		t.Fatalf("expected only default hub, got %+v", result.Hubs)
	}
}

func TestResolveByEmailNormalizesCorporateDomainMatching(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	corpHub := &store.HubInstance{
		ID:                   "hub_qax",
		OwnerEmail:           "owner@rapidai.tech",
		Name:                 "QAX Hub",
		BaseURL:              "https://qax.example.com",
		Visibility:           "shared",
		EnrollmentMode:       "approval",
		CorporateEmailDomain: "@RAPIDAI.TECH",
		Status:               "online",
		HubSecretHash:        "secret-qax",
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	defaultHub := &store.HubInstance{
		ID:             "hub_default",
		OwnerEmail:     "owner@example.com",
		Name:           "Default Hub",
		BaseURL:        "https://default.example.com",
		Visibility:     "shared",
		EnrollmentMode: "approval",
		Status:         "online",
		HubSecretHash:  "secret-default",
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	for _, hub := range []*store.HubInstance{corpHub, defaultHub} {
		if err := st.Hubs.Create(ctx, hub); err != nil {
			t.Fatalf("create hub %s: %v", hub.ID, err)
		}
	}

	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs)
	result, err := svc.ResolveByEmail(ctx, "Alice@RapidAI.Tech")
	if err != nil {
		t.Fatalf("ResolveByEmail: %v", err)
	}
	if result == nil || result.Mode != "single" {
		t.Fatalf("unexpected resolve result: %+v", result)
	}
	if result.DefaultHubID != "hub_qax" {
		t.Fatalf("expected normalized corporate hub, got %q", result.DefaultHubID)
	}
	if len(result.Hubs) != 1 || result.Hubs[0].CorporateEmailDomain != "rapidai.tech" {
		t.Fatalf("expected normalized corporate domain in response, got %+v", result.Hubs)
	}
}

func TestResolveByEmailMatchesAdditionalDomainRoutes(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	hub := &store.HubInstance{
		ID:                   "hub_multi_domain",
		OwnerEmail:           "owner@rapidai.tech",
		Name:                 "Multi Domain Hub",
		BaseURL:              "https://multi.example.com",
		Visibility:           "shared",
		EnrollmentMode:       "approval",
		CorporateEmailDomain: "rapidai.tech",
		Status:               "online",
		HubSecretHash:        "secret-multi",
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	if err := st.Hubs.Create(ctx, hub); err != nil {
		t.Fatalf("create hub: %v", err)
	}
	if err := st.HubDomainRoutes.Upsert(ctx, &store.HubDomainRoute{
		ID:        "hdr_extra_hub_multi_domain",
		HubID:     hub.ID,
		Domain:    "subsidiary.example",
		Enabled:   true,
		Priority:  50,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("upsert domain route: %v", err)
	}

	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs)
	result, err := svc.ResolveByEmail(ctx, "user@subsidiary.example")
	if err != nil {
		t.Fatalf("ResolveByEmail: %v", err)
	}
	if result == nil || result.Mode != "single" {
		t.Fatalf("unexpected resolve result: %+v", result)
	}
	if result.DefaultHubID != hub.ID {
		t.Fatalf("expected multi-domain hub, got %q", result.DefaultHubID)
	}
	if len(result.Hubs) != 1 || result.Hubs[0].CorporateEmailDomain != "subsidiary.example" {
		t.Fatalf("expected additional domain route in response, got %+v", result.Hubs)
	}
}

func TestRoutingDiagnosticsReportsLegacyBackfillPending(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	hub := &store.HubInstance{
		ID:                   "hub_legacy_pending",
		OwnerEmail:           "owner@rapidai.tech",
		Name:                 "Legacy Pending Hub",
		BaseURL:              "https://legacy.example.com",
		Visibility:           "private",
		EnrollmentMode:       "approval",
		CorporateEmailDomain: "rapidai.tech",
		Status:               "online",
		HubSecretHash:        "secret-legacy",
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	publicHub := &store.HubInstance{
		ID:                 "hub_public_signup",
		OwnerEmail:         "owner@example.com",
		Name:               "Public Signup Hub",
		BaseURL:            "https://public.example.com",
		Visibility:         "shared",
		EnrollmentMode:     "open",
		AcceptPublicSignup: true,
		Status:             "online",
		HubSecretHash:      "secret-public",
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	for _, item := range []*store.HubInstance{hub, publicHub} {
		if err := st.Hubs.Create(ctx, item); err != nil {
			t.Fatalf("create hub %s: %v", item.ID, err)
		}
	}

	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs)
	if err := svc.Rebuild(ctx); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	diagnostics, err := svc.RoutingDiagnostics(ctx)
	if err != nil {
		t.Fatalf("RoutingDiagnostics: %v", err)
	}
	if diagnostics.Snapshot.DomainRoutes != 1 {
		t.Fatalf("Snapshot.DomainRoutes = %d, want 1", diagnostics.Snapshot.DomainRoutes)
	}
	if diagnostics.Snapshot.PublicHubs != 1 {
		t.Fatalf("Snapshot.PublicHubs = %d, want 1", diagnostics.Snapshot.PublicHubs)
	}
	if diagnostics.Hubs.LegacyDomainHubs != 1 {
		t.Fatalf("LegacyDomainHubs = %d, want 1", diagnostics.Hubs.LegacyDomainHubs)
	}
	if diagnostics.Hubs.EnabledDomainRoutes != 0 {
		t.Fatalf("EnabledDomainRoutes = %d, want 0", diagnostics.Hubs.EnabledDomainRoutes)
	}
	if diagnostics.Migration.LegacyDomainBackfillPending != 1 {
		t.Fatalf("LegacyDomainBackfillPending = %d, want 1", diagnostics.Migration.LegacyDomainBackfillPending)
	}
}

func TestRoutingDiagnosticsTenantDomainDoesNotSatisfyGlobalBackfill(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	hub := &store.HubInstance{
		ID:                   "hub_tenant_route_only",
		OwnerEmail:           "owner@rapidai.tech",
		Name:                 "Tenant Route Only Hub",
		BaseURL:              "https://tenant-route.example.com",
		CorporateEmailDomain: "rapidai.tech",
		Status:               "online",
		HubSecretHash:        "secret",
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	if err := st.Hubs.Create(ctx, hub); err != nil {
		t.Fatalf("create hub: %v", err)
	}
	if err := st.HubDomainRoutes.Upsert(ctx, &store.HubDomainRoute{
		ID:        "hdr_tenant_route_only",
		HubID:     hub.ID,
		TenantID:  "tenant_a",
		Domain:    "rapidai.tech",
		Enabled:   true,
		Priority:  200,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("upsert tenant route: %v", err)
	}

	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs)
	diagnostics, err := svc.RoutingDiagnostics(ctx)
	if err != nil {
		t.Fatalf("RoutingDiagnostics: %v", err)
	}
	if diagnostics.Migration.LegacyDomainBackfillPending != 1 {
		t.Fatalf("LegacyDomainBackfillPending = %d, want 1", diagnostics.Migration.LegacyDomainBackfillPending)
	}
}

func TestRoutingDiagnosticsReusesSnapshotInputs(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	hub := &store.HubInstance{
		ID:                   "hub_diag_scans",
		OwnerEmail:           "owner@rapidai.tech",
		Name:                 "Diagnostics Scan Hub",
		BaseURL:              "https://diag.example.com",
		CorporateEmailDomain: "rapidai.tech",
		Status:               "online",
		HubSecretHash:        "secret",
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	if err := st.Hubs.Create(ctx, hub); err != nil {
		t.Fatalf("create hub: %v", err)
	}
	if err := st.HubDomainRoutes.Upsert(ctx, &store.HubDomainRoute{ID: "route_diag_scans", HubID: hub.ID, Domain: "rapidai.tech", Enabled: true, Priority: 100, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("upsert route: %v", err)
	}
	hubs := &countingHubRepo{HubRepository: st.Hubs}
	routes := &countingHubDomainRouteRepo{HubDomainRouteRepository: st.HubDomainRoutes}
	svc := NewService(hubs, st.HubUserLinks, routes, st.BlockedEmails, st.BlockedIPs)

	diagnostics, err := svc.RoutingDiagnostics(ctx)
	if err != nil {
		t.Fatalf("RoutingDiagnostics: %v", err)
	}
	if diagnostics.Snapshot.DomainRoutes != 1 || diagnostics.Hubs.EnabledDomainRoutes != 1 {
		t.Fatalf("unexpected diagnostics: %+v", diagnostics)
	}
	if got := hubs.ListAllCalls(); got != 1 {
		t.Fatalf("hub ListAll calls = %d, want 1", got)
	}
	if got := routes.ListAllCalls(); got != 1 {
		t.Fatalf("route ListAll calls = %d, want 1", got)
	}
}

func TestRoutingDiagnosticsUsesPagedSnapshotInputs(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	hub := &store.HubInstance{ID: "hub_diag_paged", OwnerEmail: "owner@rapidai.tech", Name: "Diagnostics Paged Hub", BaseURL: "https://paged.example.com", CorporateEmailDomain: "rapidai.tech", Status: "online", HubSecretHash: "secret", CreatedAt: now, UpdatedAt: now}
	if err := st.Hubs.Create(ctx, hub); err != nil {
		t.Fatalf("create hub: %v", err)
	}
	if err := st.HubUserLinks.Upsert(ctx, &store.HubUserLink{ID: "link_diag_paged", HubID: hub.ID, Email: "user@rapidai.tech", IsDefault: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("upsert link: %v", err)
	}
	if err := st.HubDomainRoutes.Upsert(ctx, &store.HubDomainRoute{ID: "route_diag_paged", HubID: hub.ID, Domain: "rapidai.tech", Enabled: true, Priority: 100, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("upsert route: %v", err)
	}
	if err := st.BlockedEmails.Create(ctx, &store.BlockedEmail{ID: "blocked_email_paged", Email: "blocked@rapidai.tech", Reason: "test", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create blocked email: %v", err)
	}
	if err := st.BlockedIPs.Create(ctx, &store.BlockedIP{ID: "blocked_ip_paged", IP: "10.1.2.3", Reason: "test", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create blocked ip: %v", err)
	}

	hubs := &pagedCountingHubRepo{HubRepository: st.Hubs}
	links := &pagedCountingHubUserLinkRepo{HubUserLinkRepository: st.HubUserLinks}
	routes := &pagedCountingHubDomainRouteRepo{HubDomainRouteRepository: st.HubDomainRoutes}
	blockedEmails := &pagedCountingBlockedEmailRepo{BlockedEmailRepository: st.BlockedEmails}
	blockedIPs := &pagedCountingBlockedIPRepo{BlockedIPRepository: st.BlockedIPs}
	svc := NewService(hubs, links, routes, blockedEmails, blockedIPs)

	diagnostics, err := svc.RoutingDiagnostics(ctx)
	if err != nil {
		t.Fatalf("RoutingDiagnostics: %v", err)
	}
	if diagnostics.Snapshot.EmailRoutes != 1 || diagnostics.Snapshot.DomainRoutes != 1 {
		t.Fatalf("unexpected diagnostics: %+v", diagnostics)
	}
	if diagnostics.Snapshot.BlockedEmails != 1 || diagnostics.Snapshot.BlockedIPs != 1 {
		t.Fatalf("unexpected blocked snapshot counts: %+v", diagnostics.Snapshot)
	}
	if listAll, listPage := hubs.Calls(); listAll != 0 || listPage == 0 {
		t.Fatalf("hub calls = ListAll:%d ListPage:%d, want 0/>0", listAll, listPage)
	}
	if listAll, listPage := links.Calls(); listAll != 0 || listPage == 0 {
		t.Fatalf("link calls = ListAll:%d ListPage:%d, want 0/>0", listAll, listPage)
	}
	if listAll, listPage := routes.Calls(); listAll != 0 || listPage == 0 {
		t.Fatalf("route calls = ListAll:%d ListPage:%d, want 0/>0", listAll, listPage)
	}
	if list, listPage := blockedEmails.Calls(); list != 0 || listPage == 0 {
		t.Fatalf("blocked email calls = List:%d ListPage:%d, want 0/>0", list, listPage)
	}
	if list, listPage := blockedIPs.Calls(); list != 0 || listPage == 0 {
		t.Fatalf("blocked ip calls = List:%d ListPage:%d, want 0/>0", list, listPage)
	}
}

func TestResolveByDomainReturnsOnlyExactDomainMatches(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	domainHub := &store.HubInstance{
		ID:                   "hub_qianxin",
		OwnerEmail:           "owner@qianxin.com",
		Name:                 "Qianxin Hub",
		BaseURL:              "https://qianxin.example.com",
		Visibility:           "shared",
		EnrollmentMode:       "approval",
		CorporateEmailDomain: "qianxin.com",
		Status:               "online",
		HubSecretHash:        "secret-qianxin",
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	publicHub := &store.HubInstance{
		ID:                 "hub_public",
		OwnerEmail:         "owner@example.com",
		Name:               "Public Hub",
		BaseURL:            "https://public.example.com",
		Visibility:         "shared",
		EnrollmentMode:     "open",
		AcceptPublicSignup: true,
		Status:             "online",
		HubSecretHash:      "secret-public",
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	for _, hub := range []*store.HubInstance{domainHub, publicHub} {
		if err := st.Hubs.Create(ctx, hub); err != nil {
			t.Fatalf("create hub %s: %v", hub.ID, err)
		}
	}

	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs)
	result, err := svc.ResolveByDomain(ctx, "qianxin.com")
	if err != nil {
		t.Fatalf("ResolveByDomain: %v", err)
	}
	if result == nil || result.Mode != "single" {
		t.Fatalf("unexpected resolve result: %+v", result)
	}
	if result.DefaultHubID != "hub_qianxin" {
		t.Fatalf("expected domain hub, got %q", result.DefaultHubID)
	}
	if len(result.Hubs) != 1 || result.Hubs[0].HubID != "hub_qianxin" {
		t.Fatalf("expected only exact domain hub, got %+v", result.Hubs)
	}
	if result.Hubs[0].PWAURL != "" {
		t.Fatalf("expected empty pwa url for domain query, got %q", result.Hubs[0].PWAURL)
	}
}

func TestResolveByDomainReturnsNoneWhenNoExactDomainRoute(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	publicHub := &store.HubInstance{
		ID:                 "hub_public_only",
		OwnerEmail:         "owner@example.com",
		Name:               "Public Hub",
		BaseURL:            "https://public.example.com",
		Visibility:         "shared",
		EnrollmentMode:     "open",
		AcceptPublicSignup: true,
		Status:             "online",
		HubSecretHash:      "secret-public-only",
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := st.Hubs.Create(ctx, publicHub); err != nil {
		t.Fatalf("create public hub: %v", err)
	}

	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs)
	result, err := svc.ResolveByDomain(ctx, "qianxin.com")
	if err != nil {
		t.Fatalf("ResolveByDomain: %v", err)
	}
	if result == nil || result.Mode != "none" {
		t.Fatalf("expected none result, got %+v", result)
	}
	if len(result.Hubs) != 0 {
		t.Fatalf("expected no hubs, got %+v", result.Hubs)
	}
}

func TestResolveByEmailKeepsSameHubTenantCandidates(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	hub := &store.HubInstance{
		ID:             "hub_multi_tenant",
		OwnerEmail:     "owner@example.com",
		Name:           "Hub Multi Tenant",
		BaseURL:        "https://hub.example.com",
		Visibility:     "shared",
		EnrollmentMode: "open",
		Status:         "online",
		HubSecretHash:  "secret",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := st.Hubs.Create(ctx, hub); err != nil {
		t.Fatalf("create hub: %v", err)
	}
	for _, link := range []*store.HubUserLink{
		{ID: "link_tenant_a", HubID: hub.ID, TenantID: "tenant_a", Email: "same@example.com", CreatedAt: now, UpdatedAt: now},
		{ID: "link_tenant_b", HubID: hub.ID, TenantID: "tenant_b", Email: "same@example.com", CreatedAt: now, UpdatedAt: now},
	} {
		if err := st.HubUserLinks.Create(ctx, link); err != nil {
			t.Fatalf("create link %s: %v", link.ID, err)
		}
	}

	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs)
	result, err := svc.ResolveByEmail(ctx, "same@example.com")
	if err != nil {
		t.Fatalf("ResolveByEmail: %v", err)
	}
	if result == nil || result.Mode != "multiple" || len(result.Hubs) < 2 {
		t.Fatalf("expected multiple virtual hub candidates, got %+v", result)
	}
	seen := map[string]bool{}
	for _, item := range result.Hubs {
		if item.HubID != hub.ID {
			t.Fatalf("unexpected hub id: %+v", item)
		}
		if item.TenantID == "" {
			continue
		}
		seen[item.TenantID] = true
		if item.PWAURL == "" || !strings.Contains(item.PWAURL, "tenant_id=") {
			t.Fatalf("expected tenant pwa url, got %+v", item)
		}
	}
	if !seen["tenant_a"] || !seen["tenant_b"] {
		t.Fatalf("missing tenant candidates: %+v", result.Hubs)
	}
}

func TestAdminTenantRouteDoesNotHideOtherTenantSameEmail(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	hubA := &store.HubInstance{ID: "hub_tenant_a", OwnerEmail: "owner-a@example.com", Name: "Hub A", BaseURL: "https://a.example.com", Visibility: "shared", EnrollmentMode: "open", Status: "online", HubSecretHash: "secret-a", CreatedAt: now, UpdatedAt: now}
	hubB := &store.HubInstance{ID: "hub_tenant_b", OwnerEmail: "owner-b@example.com", Name: "Hub B", BaseURL: "https://b.example.com", Visibility: "shared", EnrollmentMode: "open", Status: "online", HubSecretHash: "secret-b", CreatedAt: now, UpdatedAt: now}
	for _, hub := range []*store.HubInstance{hubA, hubB} {
		if err := st.Hubs.Create(ctx, hub); err != nil {
			t.Fatalf("create hub %s: %v", hub.ID, err)
		}
	}
	if err := st.HubUserLinks.Create(ctx, &store.HubUserLink{ID: "hul_admin_tenant_a", HubID: hubA.ID, TenantID: "tenant_a", Email: "same@example.com", IsDefault: false, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create admin tenant link: %v", err)
	}
	if err := st.HubUserLinks.Create(ctx, &store.HubUserLink{ID: "link_tenant_b", HubID: hubB.ID, TenantID: "tenant_b", Email: "same@example.com", IsDefault: false, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create tenant b link: %v", err)
	}

	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs)
	result, err := svc.ResolveByEmail(ctx, "same@example.com")
	if err != nil {
		t.Fatalf("ResolveByEmail: %v", err)
	}
	seen := map[string]string{}
	for _, item := range result.Hubs {
		seen[item.TenantID] = item.HubID
	}
	if seen["tenant_a"] != hubA.ID || seen["tenant_b"] != hubB.ID {
		t.Fatalf("expected admin route to hide only its tenant, got %+v", result.Hubs)
	}
}

func TestResolveByDomainReturnsTenantVirtualHubRoutes(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now()
	hub := &store.HubInstance{ID: "hub_multi_tenant_domain", OwnerEmail: "owner@example.com", Name: "Multi Tenant Domain", BaseURL: "https://hub.example.com", Visibility: "shared", EnrollmentMode: "open", Status: "online", HubSecretHash: "secret", CreatedAt: now, UpdatedAt: now}
	if err := st.Hubs.Create(ctx, hub); err != nil {
		t.Fatalf("create hub: %v", err)
	}
	for _, route := range []*store.HubDomainRoute{
		{ID: "route_tenant_a", HubID: hub.ID, TenantID: "tenant_a", Domain: "acme.example", Enabled: true, Priority: 100, CreatedAt: now, UpdatedAt: now},
		{ID: "route_tenant_b", HubID: hub.ID, TenantID: "tenant_b", Domain: "acme.example", Enabled: true, Priority: 101, CreatedAt: now, UpdatedAt: now},
	} {
		if err := st.HubDomainRoutes.Upsert(ctx, route); err != nil {
			t.Fatalf("upsert route %s: %v", route.ID, err)
		}
	}

	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs)
	result, err := svc.ResolveByDomain(ctx, "acme.example")
	if err != nil {
		t.Fatalf("ResolveByDomain: %v", err)
	}
	if result == nil || result.Mode != "multiple" || len(result.Hubs) != 2 {
		t.Fatalf("expected two tenant virtual hub routes, got %+v", result)
	}
	seen := map[string]bool{}
	for _, item := range result.Hubs {
		seen[item.TenantID] = true
		if item.HubID != hub.ID || item.PWAURL == "" || !strings.Contains(item.PWAURL, "tenant_id=") {
			t.Fatalf("unexpected tenant domain candidate: %+v", item)
		}
	}
	if !seen["tenant_a"] || !seen["tenant_b"] {
		t.Fatalf("missing tenant domain routes: %+v", result.Hubs)
	}
}

func TestAdminTenantDomainRouteDoesNotHideGlobalDomainRoute(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	hub := &store.HubInstance{
		ID:                   "hub_tenant_admin_domain",
		OwnerEmail:           "owner@example.com",
		Name:                 "Tenant Admin Domain",
		BaseURL:              "https://hub.example.com",
		Visibility:           "shared",
		EnrollmentMode:       "open",
		Status:               "online",
		CorporateEmailDomain: "acme.example",
		HubSecretHash:        "secret",
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	if err := st.Hubs.Create(ctx, hub); err != nil {
		t.Fatalf("create hub: %v", err)
	}
	if err := st.HubDomainRoutes.Upsert(ctx, &store.HubDomainRoute{
		ID:        "hdr_admin_tenant_a_acme",
		HubID:     hub.ID,
		TenantID:  "tenant_a",
		Domain:    "acme.example",
		Enabled:   true,
		Priority:  0,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("upsert tenant admin domain route: %v", err)
	}

	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs)
	result, err := svc.ResolveByDomain(ctx, "acme.example")
	if err != nil {
		t.Fatalf("ResolveByDomain: %v", err)
	}
	if result == nil || result.Mode != "multiple" || len(result.Hubs) != 2 {
		t.Fatalf("expected tenant and global domain routes, got %+v", result)
	}
	seen := map[string]bool{}
	for _, item := range result.Hubs {
		if item.HubID != hub.ID {
			t.Fatalf("unexpected hub id: %+v", item)
		}
		seen[item.TenantID] = true
	}
	if !seen[""] || !seen["tenant_a"] {
		t.Fatalf("expected global and tenant domain routes, got %+v", result.Hubs)
	}
}

func resultHasHub(result *ResolveResult, hubID string) bool {
	if result == nil {
		return false
	}
	for _, hub := range result.Hubs {
		if hub.HubID == hubID {
			return true
		}
	}
	return false
}
