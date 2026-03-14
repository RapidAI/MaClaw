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

	svc := NewService(st.Hubs, st.HubUserLinks, st.BlockedEmails, st.BlockedIPs)
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

	svc := NewService(st.Hubs, st.HubUserLinks, st.BlockedEmails, st.BlockedIPs)
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

	svc := NewService(st.Hubs, st.HubUserLinks, st.BlockedEmails, st.BlockedIPs)
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

	svc := NewService(st.Hubs, st.HubUserLinks, st.BlockedEmails, st.BlockedIPs)
	result, err := svc.ResolveByEmail(ctx, "user@example.com")
	if err != nil {
		t.Fatalf("ResolveByEmail: %v", err)
	}
	if result == nil || result.Mode != "multiple" {
		t.Fatalf("unexpected resolve result: %+v", result)
	}
	if len(result.Hubs) != 2 {
		t.Fatalf("expected 2 visible hubs, got %d", len(result.Hubs))
	}
	if result.Hubs[0].HubID != "hub_shared" {
		t.Fatalf("expected shared hub first, got %q", result.Hubs[0].HubID)
	}
	if result.Hubs[1].HubID != "hub_public" {
		t.Fatalf("expected public hub second, got %q", result.Hubs[1].HubID)
	}
	if result.DefaultHubID != "hub_shared" {
		t.Fatalf("expected default shared hub, got %q", result.DefaultHubID)
	}
}
