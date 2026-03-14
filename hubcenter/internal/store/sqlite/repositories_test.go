package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

func newCenterTestStore(t *testing.T) *store.Store {
	t.Helper()

	provider, err := NewProvider(Config{
		DSN:               filepath.Join(t.TempDir(), "hubcenter-test.db"),
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
	t.Cleanup(func() {
		_ = provider.Close()
	})

	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	return NewStore(provider)
}

func TestAdminAndSystemRepositoriesRoundTrip(t *testing.T) {
	st := newCenterTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	admin := &store.AdminUser{
		ID:           "adm_1",
		Username:     "admin",
		PasswordHash: "hash",
		Email:        "admin@example.com",
		Status:       "active",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := st.Admins.Create(ctx, admin); err != nil {
		t.Fatalf("create admin: %v", err)
	}

	gotAdmin, err := st.Admins.GetByUsername(ctx, admin.Username)
	if err != nil {
		t.Fatalf("get admin: %v", err)
	}
	if gotAdmin == nil || gotAdmin.Email != admin.Email {
		t.Fatalf("unexpected admin: %#v", gotAdmin)
	}

	count, err := st.Admins.Count(ctx)
	if err != nil {
		t.Fatalf("count admins: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}

	if err := st.Admins.DeleteAll(ctx); err != nil {
		t.Fatalf("delete all admins: %v", err)
	}

	count, err = st.Admins.Count(ctx)
	if err != nil {
		t.Fatalf("count after delete: %v", err)
	}
	if count != 0 {
		t.Fatalf("count after delete = %d, want 0", count)
	}

	if err := st.System.Set(ctx, "admin_initialized", `{"value":true}`); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	gotSetting, err := st.System.Get(ctx, "admin_initialized")
	if err != nil {
		t.Fatalf("get setting: %v", err)
	}
	if gotSetting != `{"value":true}` {
		t.Fatalf("setting = %q", gotSetting)
	}
}

func TestHubRepositoriesRoundTrip(t *testing.T) {
	st := newCenterTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	hub := &store.HubInstance{
		ID:               "hub_1",
		OwnerEmail:       "owner@example.com",
		Name:             "MaClaw Hub",
		Description:      "Primary hub",
		BaseURL:          "https://hub.example.com",
		Visibility:       "private",
		EnrollmentMode:   "open",
		Status:           "offline",
		IsDisabled:       false,
		DisabledReason:   "",
		CapabilitiesJSON: `{"supports_pwa":true}`,
		HubSecretHash:    "secret-hash",
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := st.Hubs.Create(ctx, hub); err != nil {
		t.Fatalf("create hub: %v", err)
	}

	gotHub, err := st.Hubs.GetByID(ctx, hub.ID)
	if err != nil {
		t.Fatalf("get hub: %v", err)
	}
	if gotHub == nil || gotHub.BaseURL != hub.BaseURL {
		t.Fatalf("unexpected hub: %#v", gotHub)
	}

	if err := st.Hubs.UpdateHeartbeat(ctx, hub.ID, now.Add(time.Minute)); err != nil {
		t.Fatalf("update heartbeat: %v", err)
	}
	if err := st.Hubs.SetDisabled(ctx, hub.ID, true, "maintenance", now.Add(2*time.Minute)); err != nil {
		t.Fatalf("disable hub: %v", err)
	}

	allHubs, err := st.Hubs.ListAll(ctx)
	if err != nil {
		t.Fatalf("list all hubs: %v", err)
	}
	if len(allHubs) != 1 || !allHubs[0].IsDisabled {
		t.Fatalf("unexpected hubs: %#v", allHubs)
	}

	link := &store.HubUserLink{
		ID:        "link_1",
		HubID:     hub.ID,
		Email:     "member@example.com",
		IsDefault: true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := st.HubUserLinks.Create(ctx, link); err != nil {
		t.Fatalf("create hub user link: %v", err)
	}

	gotDefault, err := st.HubUserLinks.GetDefaultByEmail(ctx, link.Email)
	if err != nil {
		t.Fatalf("get default link: %v", err)
	}
	if gotDefault == nil || gotDefault.HubID != hub.ID {
		t.Fatalf("unexpected default link: %#v", gotDefault)
	}

	listByEmail, err := st.Hubs.ListByEmail(ctx, link.Email)
	if err != nil {
		t.Fatalf("list hubs by email: %v", err)
	}
	if len(listByEmail) != 1 || listByEmail[0].ID != hub.ID {
		t.Fatalf("unexpected hubs by email: %#v", listByEmail)
	}
}

func TestBlockedRepositoriesRoundTrip(t *testing.T) {
	st := newCenterTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	blockedEmail := &store.BlockedEmail{
		ID:        "be_1",
		Email:     "blocked@example.com",
		Reason:    "abuse",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := st.BlockedEmails.Create(ctx, blockedEmail); err != nil {
		t.Fatalf("create blocked email: %v", err)
	}

	gotEmail, err := st.BlockedEmails.GetByEmail(ctx, blockedEmail.Email)
	if err != nil {
		t.Fatalf("get blocked email: %v", err)
	}
	if gotEmail == nil || gotEmail.Reason != blockedEmail.Reason {
		t.Fatalf("unexpected blocked email: %#v", gotEmail)
	}

	blockedEmails, err := st.BlockedEmails.List(ctx)
	if err != nil {
		t.Fatalf("list blocked emails: %v", err)
	}
	if len(blockedEmails) != 1 {
		t.Fatalf("blocked emails len = %d, want 1", len(blockedEmails))
	}

	blockedIP := &store.BlockedIP{
		ID:        "bi_1",
		IP:        "127.0.0.1",
		Reason:    "rate-limit",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := st.BlockedIPs.Create(ctx, blockedIP); err != nil {
		t.Fatalf("create blocked ip: %v", err)
	}

	gotIP, err := st.BlockedIPs.GetByIP(ctx, blockedIP.IP)
	if err != nil {
		t.Fatalf("get blocked ip: %v", err)
	}
	if gotIP == nil || gotIP.Reason != blockedIP.Reason {
		t.Fatalf("unexpected blocked ip: %#v", gotIP)
	}

	blockedIPs, err := st.BlockedIPs.List(ctx)
	if err != nil {
		t.Fatalf("list blocked ips: %v", err)
	}
	if len(blockedIPs) != 1 {
		t.Fatalf("blocked ips len = %d, want 1", len(blockedIPs))
	}
}
