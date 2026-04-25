package entry

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store/sqlite"
)

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
		ID:             "hub_shared",
		OwnerEmail:     "team@example.com",
		Name:           "Shared Hub",
		BaseURL:        "https://shared.example.com",
		Visibility:     "shared",
		EnrollmentMode: "approval",
		Status:         "online",
		HubSecretHash:  "secret-shared",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	publicHub := &store.HubInstance{
		ID:             "hub_public",
		OwnerEmail:     "public@example.com",
		Name:           "Public Hub",
		BaseURL:        "https://public.example.com",
		Visibility:     "public",
		EnrollmentMode: "open",
		Status:         "online",
		HubSecretHash:  "secret-public",
		CreatedAt:      now,
		UpdatedAt:      now,
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
