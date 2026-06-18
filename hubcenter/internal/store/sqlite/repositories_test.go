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

func timePtr(t time.Time) *time.Time {
	return &t
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

func TestReplaceConflictingHubInstanceMergesEndpointDuplicate(t *testing.T) {
	st := newCenterTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	local := &store.HubInstance{
		ID:               "hub-local",
		InstallationID:   "",
		OwnerEmail:       "old@example.com",
		Name:             "Old Hub",
		BaseURL:          "https://hub.example.com/",
		Host:             "hub.example.com",
		Port:             443,
		Visibility:       "public",
		EnrollmentMode:   "open",
		Status:           "online",
		CapabilitiesJSON: "{}",
		HubSecretHash:    "old-secret",
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := st.Hubs.Create(ctx, local); err != nil {
		t.Fatalf("create local hub: %v", err)
	}
	if err := st.HubUserLinks.Upsert(ctx, &store.HubUserLink{ID: "link-local", HubID: local.ID, Email: "user@example.com", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create link: %v", err)
	}
	if err := st.HubDomainRoutes.Upsert(ctx, &store.HubDomainRoute{ID: "route-local", HubID: local.ID, Domain: "example.com", Enabled: true, Priority: 100, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create route: %v", err)
	}

	replacer, ok := st.Hubs.(interface {
		ReplaceConflictingHubInstance(context.Context, *store.HubInstance) error
	})
	if !ok {
		t.Fatal("hub repository does not support ReplaceConflictingHubInstance")
	}
	remote := &store.HubInstance{
		ID:                                    "hub-remote",
		InstallationID:                        "inst-remote",
		OwnerEmail:                            "new@example.com",
		Name:                                  "Remote Hub",
		BaseURL:                               "https://hub.example.com",
		Host:                                  "hub.example.com",
		Port:                                  443,
		Visibility:                            "shared",
		EnrollmentMode:                        "approval",
		Status:                                "online",
		CapabilitiesJSON:                      "{}",
		HubSecretHash:                         "new-secret",
		InvitationCodeRequired:                true,
		DigitalEmployeeQuota:                  9,
		DigitalEmployeeAuthorizationEnabled:   true,
		DigitalEmployeeAuthorizationExpiresAt: timePtr(now.AddDate(1, 0, 0)),
		AllowExternalProviders:                true,
		CreatedAt:                             now.Add(time.Minute),
		UpdatedAt:                             now.Add(time.Minute),
	}
	if err := replacer.ReplaceConflictingHubInstance(ctx, remote); err != nil {
		t.Fatalf("replace conflicting hub: %v", err)
	}
	if err := replacer.ReplaceConflictingHubInstance(ctx, &store.HubInstance{}); err == nil {
		t.Fatal("replace conflicting hub succeeded with missing id")
	}

	if got, err := st.Hubs.GetByID(ctx, local.ID); err != nil {
		t.Fatalf("get old hub: %v", err)
	} else if got != nil {
		t.Fatalf("old hub still exists: %#v", got)
	}
	got, err := st.Hubs.GetByID(ctx, remote.ID)
	if err != nil {
		t.Fatalf("get remote hub: %v", err)
	}
	if got == nil || got.BaseURL != "https://hub.example.com" || got.OwnerEmail != "new@example.com" {
		t.Fatalf("unexpected remote hub: %#v", got)
	}
	if !got.InvitationCodeRequired || got.DigitalEmployeeQuota != 9 || !got.DigitalEmployeeAuthorizationEnabled || got.DigitalEmployeeAuthorizationExpiresAt == nil || !got.AllowExternalProviders {
		t.Fatalf("authorization fields not merged into remote hub: %#v", got)
	}
	links, err := st.HubUserLinks.ListAll(ctx)
	if err != nil {
		t.Fatalf("list links: %v", err)
	}
	if len(links) != 1 || links[0].HubID != remote.ID {
		t.Fatalf("links not merged: %#v", links)
	}
	routes, err := st.HubDomainRoutes.ListAll(ctx)
	if err != nil {
		t.Fatalf("list routes: %v", err)
	}
	if len(routes) != 1 || routes[0].HubID != remote.ID {
		t.Fatalf("routes not merged: %#v", routes)
	}
	all, err := st.Hubs.ListAll(ctx)
	if err != nil {
		t.Fatalf("list hubs: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("hub count = %d, want 1: %#v", len(all), all)
	}
}

func TestHubRepositoriesRoundTrip(t *testing.T) {
	st := newCenterTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	hub := &store.HubInstance{
		ID:                     "hub_1",
		HubOrigin:              "official",
		DefaultSignupScope:     "public",
		OwnerEmail:             "owner@example.com",
		Name:                   "MaClaw Hub",
		Description:            "Primary hub",
		BaseURL:                "https://hub.example.com",
		Visibility:             "private",
		EnrollmentMode:         "open",
		CorporateEmailDomain:   "rapidai.tech",
		AcceptPublicSignup:     true,
		Status:                 "offline",
		IsDisabled:             false,
		DisabledReason:         "",
		CapabilitiesJSON:       `{"supports_pwa":true}`,
		RegistrationPolicyJSON: `{"tenants":{"tenant_default":{"signup_scope":"public"}}}`,
		HubSecretHash:          "secret-hash",
		CreatedAt:              now,
		UpdatedAt:              now,
	}
	if err := st.Hubs.Create(ctx, hub); err != nil {
		t.Fatalf("create hub: %v", err)
	}

	gotHub, err := st.Hubs.GetByID(ctx, hub.ID)
	if err != nil {
		t.Fatalf("get hub: %v", err)
	}
	if gotHub == nil || gotHub.BaseURL != hub.BaseURL || gotHub.CorporateEmailDomain != hub.CorporateEmailDomain || gotHub.AcceptPublicSignup != hub.AcceptPublicSignup || gotHub.HubOrigin != hub.HubOrigin || gotHub.DefaultSignupScope != hub.DefaultSignupScope || gotHub.RegistrationPolicyJSON != hub.RegistrationPolicyJSON {
		t.Fatalf("unexpected hub: %#v", gotHub)
	}
	gotHub.HubOrigin = "self_hosted"
	gotHub.DefaultSignupScope = "domain_restricted"
	gotHub.RegistrationPolicyJSON = `{"tenants":{}}`
	gotHub.InvitationCodeRequired = true
	gotHub.DigitalEmployeeQuota = 7
	gotHub.DigitalEmployeeAuthorizationEnabled = true
	gotHub.DigitalEmployeeAuthorizationExpiresAt = timePtr(now.AddDate(1, 0, 0))
	gotHub.AllowExternalProviders = true
	gotHub.UpdatedAt = now.Add(30 * time.Second)
	if err := st.Hubs.UpdateRegistration(ctx, gotHub); err != nil {
		t.Fatalf("update hub registration policy fields: %v", err)
	}
	updatedHub, err := st.Hubs.GetByID(ctx, hub.ID)
	if err != nil {
		t.Fatalf("get updated hub: %v", err)
	}
	if updatedHub == nil || updatedHub.HubOrigin != "self_hosted" || updatedHub.DefaultSignupScope != "domain_restricted" || updatedHub.RegistrationPolicyJSON != `{"tenants":{}}` {
		t.Fatalf("registration policy fields not persisted: %#v", updatedHub)
	}
	if !updatedHub.InvitationCodeRequired || updatedHub.DigitalEmployeeQuota != 7 || !updatedHub.DigitalEmployeeAuthorizationEnabled || updatedHub.DigitalEmployeeAuthorizationExpiresAt == nil || !updatedHub.AllowExternalProviders {
		t.Fatalf("authorization fields not persisted by UpdateRegistration: %#v", updatedHub)
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
	gotDefault, err = st.HubUserLinks.GetDefaultByEmail(ctx, "MEMBER@example.com")
	if err != nil || gotDefault == nil || gotDefault.HubID != hub.ID {
		t.Fatalf("case-insensitive default link = %#v err=%v", gotDefault, err)
	}
	links, err := st.HubUserLinks.ListByEmail(ctx, "MEMBER@example.com")
	if err != nil || len(links) != 1 || links[0].HubID != hub.ID {
		t.Fatalf("case-insensitive user links = %#v err=%v", links, err)
	}

	listByEmail, err := st.Hubs.ListByEmail(ctx, link.Email)
	if err != nil {
		t.Fatalf("list hubs by email: %v", err)
	}
	if len(listByEmail) != 1 || listByEmail[0].ID != hub.ID {
		t.Fatalf("unexpected hubs by email: %#v", listByEmail)
	}

	route := &store.HubDomainRoute{
		ID:        "hdr_primary_hub_1",
		HubID:     hub.ID,
		Domain:    "rapidai.tech",
		Enabled:   true,
		Priority:  100,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := st.HubDomainRoutes.Upsert(ctx, route); err != nil {
		t.Fatalf("upsert hub domain route: %v", err)
	}
	routes, err := st.HubDomainRoutes.ListAll(ctx)
	if err != nil {
		t.Fatalf("list hub domain routes: %v", err)
	}
	if len(routes) != 1 || routes[0].HubID != hub.ID || routes[0].Domain != route.Domain || !routes[0].Enabled {
		t.Fatalf("unexpected hub domain routes: %#v", routes)
	}
}

func TestHubUserLinkAggregateIndexExists(t *testing.T) {
	provider, err := NewProvider(Config{
		DSN:               filepath.Join(t.TempDir(), "hubcenter-index-test.db"),
		WAL:               true,
		BusyTimeoutMS:     5000,
		MaxReadOpenConns:  1,
		MaxReadIdleConns:  1,
		MaxWriteOpenConns: 1,
		MaxWriteIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	rows, err := provider.Write.Query(`PRAGMA index_list('hub_user_links')`)
	if err != nil {
		t.Fatalf("pragma index_list: %v", err)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var seq int
		var name string
		var unique int
		var origin string
		var partial int
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			t.Fatalf("scan index row: %v", err)
		}
		if name == "idx_hub_user_links_hub_tenant_email" {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("index rows: %v", err)
	}
	if !found {
		t.Fatal("idx_hub_user_links_hub_tenant_email not found")
	}
}

func TestHubRepoGetByEndpointPrefersHostPortOverBaseURL(t *testing.T) {
	st := newCenterTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	old := now.Add(-time.Hour)

	if err := st.Hubs.Create(ctx, &store.HubInstance{ID: "hub_host", OwnerEmail: "owner@example.com", Name: "Host Hub", BaseURL: "https://host.example.com", Host: "10.0.0.7", Port: 9399, Visibility: "private", EnrollmentMode: "open", Status: "online", CapabilitiesJSON: `{}`, HubSecretHash: "secret", CreatedAt: old, UpdatedAt: old}); err != nil {
		t.Fatalf("create host hub: %v", err)
	}
	if err := st.Hubs.Create(ctx, &store.HubInstance{ID: "hub_url", OwnerEmail: "owner@example.com", Name: "URL Hub", BaseURL: "https://url.example.com", Host: "10.0.0.8", Port: 9399, Visibility: "private", EnrollmentMode: "open", Status: "online", CapabilitiesJSON: `{}`, HubSecretHash: "secret", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create url hub: %v", err)
	}

	got, err := st.Hubs.GetByEndpoint(ctx, "10.0.0.7", 9399, "https://url.example.com")
	if err != nil {
		t.Fatalf("GetByEndpoint: %v", err)
	}
	if got == nil || got.ID != "hub_host" {
		t.Fatalf("expected host+port match to win over base_url match, got %+v", got)
	}
}

func TestHubRepoGetByEndpointNormalizesLookupInput(t *testing.T) {
	st := newCenterTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	if err := st.Hubs.Create(ctx, &store.HubInstance{ID: "hub_lookup_norm", OwnerEmail: "owner@example.com", Name: "Lookup Norm", BaseURL: "https://hub.example.com:9399", Host: "hub.example.com", Port: 9399, Visibility: "private", EnrollmentMode: "open", Status: "online", CapabilitiesJSON: `{}`, HubSecretHash: "secret", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create hub: %v", err)
	}

	got, err := st.Hubs.GetByEndpoint(ctx, "Hub.Example.COM", 9399, "HTTPS://Hub.Example.COM:9399/")
	if err != nil {
		t.Fatalf("GetByEndpoint: %v", err)
	}
	if got == nil || got.ID != "hub_lookup_norm" {
		t.Fatalf("expected normalized lookup to find hub, got %+v", got)
	}
}

func TestHubRepoNormalizesEndpointOnCreateAndUpdate(t *testing.T) {
	st := newCenterTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	hub := &store.HubInstance{ID: "hub_norm", InstallationID: " inst_norm ", OwnerEmail: "owner@example.com", Name: "Norm Hub", BaseURL: "HTTP://Hub.Example.COM:9399/", Host: "Hub.Example.COM", Port: 9399, Visibility: "private", EnrollmentMode: "open", Status: "online", CapabilitiesJSON: `{}`, HubSecretHash: "secret", CreatedAt: now, UpdatedAt: now}
	if err := st.Hubs.Create(ctx, hub); err != nil {
		t.Fatalf("create hub: %v", err)
	}
	got, err := st.Hubs.GetByID(ctx, hub.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got == nil || got.InstallationID != "inst_norm" || got.Host != "hub.example.com" || got.BaseURL != "http://hub.example.com:9399" {
		t.Fatalf("expected normalized endpoint after create, got %+v", got)
	}
	byInstallation, err := st.Hubs.GetByInstallationID(ctx, " inst_norm ")
	if err != nil {
		t.Fatalf("GetByInstallationID: %v", err)
	}
	if byInstallation == nil || byInstallation.ID != hub.ID {
		t.Fatalf("expected normalized installation lookup to find hub, got %+v", byInstallation)
	}
	got.InstallationID = " inst_updated "
	got.Host = "Other.Example.COM"
	got.BaseURL = "HTTPS://Other.Example.COM/"
	got.UpdatedAt = now.Add(time.Minute)
	if err := st.Hubs.UpdateRegistration(ctx, got); err != nil {
		t.Fatalf("UpdateRegistration: %v", err)
	}
	updated, err := st.Hubs.GetByID(ctx, hub.ID)
	if err != nil {
		t.Fatalf("GetByID updated: %v", err)
	}
	if updated == nil || updated.InstallationID != "inst_updated" || updated.Host != "other.example.com" || updated.BaseURL != "https://other.example.com" {
		t.Fatalf("expected normalized endpoint after update, got %+v", updated)
	}
}

func TestHubUserLinkDeleteByHubTenantEmailKeepsAdminAndOtherScopes(t *testing.T) {
	st := newCenterTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	repo, ok := st.HubUserLinks.(*hubUserLinkRepo)
	if !ok {
		t.Fatal("hub user link repo has unexpected type")
	}
	deleter, ok := st.HubUserLinks.(interface {
		DeleteByHubTenantEmail(context.Context, string, string, string) ([]*store.HubUserLink, error)
	})
	if !ok {
		t.Fatal("hub user link repo does not support scoped delete")
	}
	adminID := adminStoreUserLinkIDForTenant("tenant-a", "member@example.com")
	links := []*store.HubUserLink{
		{ID: "hul_user_hub-a_tenant-a_member", HubID: "hub-a", TenantID: "tenant-a", Email: "member@example.com", CreatedAt: now, UpdatedAt: now},
		{ID: adminID, HubID: "hub-a", TenantID: "tenant-a", Email: "member@example.com", CreatedAt: now, UpdatedAt: now},
		{ID: "hul_user_hub-a_tenant-b_member", HubID: "hub-a", TenantID: "tenant-b", Email: "member@example.com", CreatedAt: now, UpdatedAt: now},
		{ID: "hul_user_hub-b_tenant-a_member", HubID: "hub-b", TenantID: "tenant-a", Email: "member@example.com", CreatedAt: now, UpdatedAt: now},
	}
	for _, link := range links {
		if err := st.HubUserLinks.Upsert(ctx, link); err != nil {
			t.Fatalf("seed link %s: %v", link.ID, err)
		}
	}

	removed, err := deleter.DeleteByHubTenantEmail(ctx, "hub-a", "tenant-a", "MEMBER@example.com")
	if err != nil {
		t.Fatalf("DeleteByHubTenantEmail: %v", err)
	}
	if len(removed) != 1 || removed[0].ID != "hul_user_hub-a_tenant-a_member" {
		t.Fatalf("removed links = %+v", removed)
	}
	remaining, err := st.HubUserLinks.ListByEmail(ctx, "member@example.com")
	if err != nil {
		t.Fatalf("ListByEmail: %v", err)
	}
	seen := map[string]bool{}
	for _, link := range remaining {
		seen[link.ID] = true
	}
	if seen["hul_user_hub-a_tenant-a_member"] || !seen[adminID] || !seen["hul_user_hub-a_tenant-b_member"] || !seen["hul_user_hub-b_tenant-a_member"] {
		t.Fatalf("unexpected remaining scoped links: %+v", remaining)
	}

	if _, err := repo.db.ExecContext(ctx, `
		INSERT INTO hub_user_links (id, hub_id, tenant_id, email, is_default, created_at, updated_at)
		VALUES (?, ?, ?, ?, 0, ?, ?)
	`, "hul_user_hub-a_legacy-default", "hub-a", "tenant_default", "legacy-default@example.com", now.Format(time.RFC3339), now.Format(time.RFC3339)); err != nil {
		t.Fatalf("seed legacy default tenant link: %v", err)
	}
	removed, err = deleter.DeleteByHubTenantEmail(ctx, "hub-a", "tenant_default", "legacy-default@example.com")
	if err != nil {
		t.Fatalf("DeleteByHubTenantEmail legacy default: %v", err)
	}
	if len(removed) != 1 || removed[0].ID != "hul_user_hub-a_legacy-default" {
		t.Fatalf("legacy default removed links = %+v", removed)
	}
}

func TestHubUserLinkMigrateEmailToHubMatchesEmailCaseInsensitively(t *testing.T) {
	st := newCenterTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	migrator, ok := st.HubUserLinks.(interface {
		MigrateEmailToHub(context.Context, string, string, string, *store.HubUserLink) ([]*store.HubUserLink, *store.HubUserLink, error)
	})
	if !ok {
		t.Fatal("hub user link repo does not support email migration")
	}
	if err := st.HubUserLinks.Upsert(ctx, &store.HubUserLink{ID: "hul_user_hub-a_member", HubID: "hub-a", TenantID: "tenant-a", Email: "Member@Example.com", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("seed mixed-case link: %v", err)
	}

	removed, upserted, err := migrator.MigrateEmailToHub(ctx, "member@example.com", "hub-a", "tenant-a", &store.HubUserLink{ID: adminStoreUserLinkIDForTenant("tenant-b", "member@example.com"), HubID: "hub-b", TenantID: "tenant-b", Email: "member@example.com", CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatalf("MigrateEmailToHub: %v", err)
	}
	if len(removed) != 1 || removed[0].ID != "hul_user_hub-a_member" || upserted == nil || upserted.HubID != "hub-b" || upserted.TenantID != "tenant-b" {
		t.Fatalf("unexpected migration result removed=%+v upserted=%+v", removed, upserted)
	}
	links, err := st.HubUserLinks.ListByEmail(ctx, "MEMBER@example.com")
	if err != nil {
		t.Fatalf("ListByEmail: %v", err)
	}
	if len(links) != 1 || links[0].HubID != "hub-b" || links[0].TenantID != "tenant-b" {
		t.Fatalf("expected only migrated target link, got %+v", links)
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

func TestGossipRepositoryCountSnapshotRecords(t *testing.T) {
	st := newCenterTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	if err := st.Gossip.CreatePost(ctx, &store.GossipPost{ID: "post-1", MachineID: "machine-1", UserEmail: "user@example.com", Nickname: "User", Content: "hello", Category: "general", CreatedAt: now}); err != nil {
		t.Fatalf("CreatePost() error = %v", err)
	}
	if err := st.Gossip.CreateComment(ctx, &store.GossipComment{ID: "comment-1", PostID: "post-1", MachineID: "machine-2", UserEmail: "reply@example.com", Nickname: "Reply", Content: "world", Rating: 1, CreatedAt: now}); err != nil {
		t.Fatalf("CreateComment() error = %v", err)
	}
	counter, ok := st.Gossip.(interface {
		CountSnapshotRecords(context.Context) (int64, error)
	})
	if !ok {
		t.Fatal("gossip repository does not expose CountSnapshotRecords")
	}
	got, err := counter.CountSnapshotRecords(ctx)
	if err != nil {
		t.Fatalf("CountSnapshotRecords() error = %v", err)
	}
	if got != 2 {
		t.Fatalf("CountSnapshotRecords() = %d, want 2", got)
	}
}

func TestFailureEventLogTenantDefaultFilterMatchesLegacyRows(t *testing.T) {
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
	t.Cleanup(func() { _ = provider.Close() })
	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	st := NewStore(provider)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	if err := st.FailureLogs.Create(ctx, &store.FailureEventLog{ID: "default_normalized", TenantID: "tenant_default", Category: "registration", EventCode: "DEFAULT_NORMALIZED", CreatedAt: now}); err != nil {
		t.Fatalf("create normalized log: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `
		INSERT INTO failure_event_logs (id, tenant_id, category, event_code, message, entity_id, email, client_ip, details_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "default_legacy", "tenant_default", "registration", "DEFAULT_LEGACY", "", "", "", "", "{}", now.Add(time.Second).Format(time.RFC3339)); err != nil {
		t.Fatalf("insert legacy log: %v", err)
	}
	if err := st.FailureLogs.Create(ctx, &store.FailureEventLog{ID: "tenant_a", TenantID: "tenant_a", Category: "registration", EventCode: "TENANT_A", CreatedAt: now.Add(2 * time.Second)}); err != nil {
		t.Fatalf("create tenant log: %v", err)
	}

	items, total, err := st.FailureLogs.List(ctx, store.FailureEventLogFilter{TenantID: "tenant_default", TenantIDSet: true, Limit: 10})
	if err != nil {
		t.Fatalf("list logs: %v", err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("default tenant filter returned total=%d len=%d items=%+v", total, len(items), items)
	}
	seen := map[string]bool{}
	for _, item := range items {
		seen[item.EventCode] = true
		if item.EventCode == "TENANT_A" {
			t.Fatalf("default tenant filter leaked tenant_a: %+v", items)
		}
	}
	if !seen["DEFAULT_NORMALIZED"] || !seen["DEFAULT_LEGACY"] {
		t.Fatalf("default tenant filter missed normalized or legacy rows: %+v", items)
	}
}
