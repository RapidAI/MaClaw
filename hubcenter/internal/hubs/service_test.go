package hubs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/entry"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/mail"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store/sqlite"
)

type testMailer struct {
	lastTo         string
	lastConfirmURL string
}

func tokenFromURL(url string) string {
	parts := strings.SplitN(url, "token=", 2)
	if len(parts) != 2 {
		return ""
	}
	return parts[1]
}

func (m *testMailer) Send(ctx context.Context, to []string, subject string, body string) error {
	return nil
}

func (m *testMailer) SendHubRegistrationConfirmation(ctx context.Context, to string, confirmURL string, hubName string) error {
	m.lastTo = to
	m.lastConfirmURL = confirmURL
	return nil
}

var _ mail.Mailer = (*testMailer)(nil)

type fakeSyncRecorder struct {
	deletedHubInstances []string
	deletedHubLinks     []string
	deletedHubRoutes    []string
}

func (f *fakeSyncRecorder) SyncHubHeartbeat(context.Context, string)                {}
func (f *fakeSyncRecorder) AppendBlockedEmail(context.Context, *store.BlockedEmail) {}
func (f *fakeSyncRecorder) DeleteBlockedEmail(context.Context, string)              {}
func (f *fakeSyncRecorder) AppendBlockedIP(context.Context, *store.BlockedIP)       {}
func (f *fakeSyncRecorder) DeleteBlockedIP(context.Context, string)                 {}
func (f *fakeSyncRecorder) AppendHubInstance(context.Context, *store.HubInstance)   {}
func (f *fakeSyncRecorder) DeleteHubInstance(_ context.Context, hubID string) {
	f.deletedHubInstances = append(f.deletedHubInstances, hubID)
}
func (f *fakeSyncRecorder) AppendHubDomainRoute(context.Context, *store.HubDomainRoute) {}
func (f *fakeSyncRecorder) DeleteHubDomainRoute(_ context.Context, routeID string) {
	f.deletedHubRoutes = append(f.deletedHubRoutes, routeID)
}
func (f *fakeSyncRecorder) AppendHubUserLink(context.Context, *store.HubUserLink) {}
func (f *fakeSyncRecorder) DeleteHubUserLink(_ context.Context, linkID string) {
	f.deletedHubLinks = append(f.deletedHubLinks, linkID)
}

func newTestStore(t *testing.T) *sqlite.Provider {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "hubcenter-test.db")
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

	return provider
}

func TestSyncHubUserLinkReplacesPreviousUserBinding(t *testing.T) {
	provider := newTestStore(t)
	st := sqlite.NewStore(provider)
	mailer := &testMailer{}
	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs, st.System, mailer, "http://127.0.0.1:9388")
	ctx := context.Background()
	now := time.Now()

	hubA := &store.HubInstance{ID: "hub_a", OwnerEmail: "owner-a@example.com", Name: "Hub A", BaseURL: "https://a.example.com", Status: "online", HubSecretHash: hashToken("secret-a"), CreatedAt: now, UpdatedAt: now}
	hubB := &store.HubInstance{ID: "hub_b", OwnerEmail: "owner-b@example.com", Name: "Hub B", BaseURL: "https://b.example.com", Status: "online", HubSecretHash: hashToken("secret-b"), CreatedAt: now, UpdatedAt: now}
	for _, hub := range []*store.HubInstance{hubA, hubB} {
		if err := st.Hubs.Create(ctx, hub); err != nil {
			t.Fatalf("create hub %s: %v", hub.ID, err)
		}
	}
	if err := st.HubUserLinks.Upsert(ctx, &store.HubUserLink{ID: primaryUserLinkID(hubA.ID, "user@example.com"), HubID: hubA.ID, Email: "user@example.com", IsDefault: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("seed user link: %v", err)
	}

	if err := svc.SyncHubUserLink(ctx, hubB.ID, "secret-b", "user@example.com", true); err != nil {
		t.Fatalf("SyncHubUserLink: %v", err)
	}
	items, err := st.HubUserLinks.ListByEmail(ctx, "user@example.com")
	if err != nil {
		t.Fatalf("ListByEmail: %v", err)
	}
	if len(items) != 1 || items[0].HubID != hubB.ID {
		t.Fatalf("expected only hub_b binding, got %+v", items)
	}
}

func TestUserRegistrationReportGroupsByHubWithRecentWindows(t *testing.T) {
	provider := newTestStore(t)
	st := sqlite.NewStore(provider)
	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs, st.System, &testMailer{}, "http://127.0.0.1:9388")
	ctx := context.Background()
	now := time.Now()

	hubA := &store.HubInstance{ID: "hub_a", OwnerEmail: "owner-a@example.com", Name: "Hub A", BaseURL: "https://a.example.com", Status: "online", HubSecretHash: hashToken("secret-a"), CreatedAt: now, UpdatedAt: now}
	hubB := &store.HubInstance{ID: "hub_b", OwnerEmail: "owner-b@example.com", Name: "Hub B", BaseURL: "https://b.example.com", Status: "online", HubSecretHash: hashToken("secret-b"), CreatedAt: now, UpdatedAt: now}
	for _, hub := range []*store.HubInstance{hubA, hubB} {
		if err := st.Hubs.Create(ctx, hub); err != nil {
			t.Fatalf("create hub %s: %v", hub.ID, err)
		}
	}
	seedLink := func(hubID, email string, createdAt time.Time) {
		t.Helper()
		link := &store.HubUserLink{ID: primaryUserLinkID(hubID, email), HubID: hubID, Email: email, CreatedAt: createdAt, UpdatedAt: createdAt}
		if err := st.HubUserLinks.Create(ctx, link); err != nil {
			t.Fatalf("seed link %s: %v", email, err)
		}
	}
	today := startOfDay(now)
	seedLink(hubA.ID, "today@example.com", today.Add(2*time.Hour))
	seedLink(hubA.ID, "yesterday@example.com", today.AddDate(0, 0, -1).Add(3*time.Hour))
	seedLink(hubA.ID, "old@example.com", today.AddDate(0, 0, -8).Add(4*time.Hour))
	seedLink(hubB.ID, "other@example.com", today.AddDate(0, 0, -2).Add(5*time.Hour))

	report, err := svc.UserRegistrationReport(ctx)
	if err != nil {
		t.Fatalf("UserRegistrationReport: %v", err)
	}
	if len(report.Daily) != 7 {
		t.Fatalf("daily buckets=%d, want 7", len(report.Daily))
	}
	if len(report.Monthly) != 6 {
		t.Fatalf("monthly buckets=%d, want 6", len(report.Monthly))
	}
	if len(report.Hubs) != 2 {
		t.Fatalf("hub reports=%d, want 2", len(report.Hubs))
	}
	byHub := map[string]UserRegistrationHubReport{}
	for _, item := range report.Hubs {
		byHub[item.HubID] = item
		if len(item.Daily) != 7 || len(item.Monthly) != 6 {
			t.Fatalf("hub %s windows: daily=%d monthly=%d", item.HubID, len(item.Daily), len(item.Monthly))
		}
	}
	if byHub[hubA.ID].TotalUsers != 3 || byHub[hubB.ID].TotalUsers != 1 {
		t.Fatalf("unexpected hub totals: %+v", byHub)
	}
	if byHub[hubA.ID].Daily[len(byHub[hubA.ID].Daily)-1].Count != 1 {
		t.Fatalf("expected hub A today count 1, got %+v", byHub[hubA.ID].Daily)
	}
	if byHub[hubA.ID].Daily[0].Count != 0 {
		t.Fatalf("expected 8-day-old user outside daily window, got %+v", byHub[hubA.ID].Daily)
	}
}

func TestMigrateUserMakesTargetHubDefault(t *testing.T) {
	provider := newTestStore(t)
	st := sqlite.NewStore(provider)
	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs, st.System, &testMailer{}, "http://127.0.0.1:9388")
	entrySvc := entry.NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs)
	svc.SetRouteSnapshotRefresher(entrySvc)
	ctx := context.Background()
	now := time.Now()

	for _, hub := range []*store.HubInstance{
		{ID: "hub_a", OwnerEmail: "owner-a@example.com", Name: "Hub A", BaseURL: "https://a.example.com", Status: "online", CreatedAt: now, UpdatedAt: now},
		{ID: "hub_b", OwnerEmail: "owner-b@example.com", Name: "Hub B", BaseURL: "https://b.example.com", Status: "online", CreatedAt: now, UpdatedAt: now},
	} {
		if err := st.Hubs.Create(ctx, hub); err != nil {
			t.Fatalf("create hub %s: %v", hub.ID, err)
		}
	}
	if err := st.HubUserLinks.Upsert(ctx, &store.HubUserLink{ID: primaryUserLinkID("hub_a", "user@example.com"), HubID: "hub_a", Email: "user@example.com", IsDefault: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("seed user link: %v", err)
	}
	if err := st.HubUserLinks.Upsert(ctx, &store.HubUserLink{ID: "target-existing-user-link", HubID: "hub_b", Email: "user@example.com", IsDefault: false, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("seed target user link: %v", err)
	}

	result, err := svc.MigrateUser(ctx, MigrateUserRequest{Email: "USER@example.com", ToHubID: "hub_b"})
	if err != nil {
		t.Fatalf("MigrateUser: %v", err)
	}
	if result.Email != "user@example.com" || result.ToHubID != "hub_b" || len(result.UpsertedIDs) != 1 {
		t.Fatalf("unexpected migration result: %+v", result)
	}
	resolved, err := entrySvc.ResolveByEmail(ctx, "user@example.com")
	if err != nil {
		t.Fatalf("ResolveByEmail: %v", err)
	}
	if resolved.DefaultHubID != "hub_b" {
		t.Fatalf("expected hub_b default after migration, got %+v", resolved)
	}
	links, err := st.HubUserLinks.ListByEmail(ctx, "user@example.com")
	if err != nil {
		t.Fatalf("ListByEmail: %v", err)
	}
	foundExistingTarget := false
	for _, link := range links {
		if link.HubID == "hub_a" {
			t.Fatalf("expected source hub link to be removed, got %+v", links)
		}
		if link.ID == "target-existing-user-link" && link.HubID == "hub_b" {
			foundExistingTarget = true
		}
	}
	if !foundExistingTarget {
		t.Fatalf("expected existing target hub link to be preserved, got %+v", links)
	}
}

func TestMigrateUserSurvivesSourceHubResync(t *testing.T) {
	provider := newTestStore(t)
	st := sqlite.NewStore(provider)
	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs, st.System, &testMailer{}, "http://127.0.0.1:9388")
	entrySvc := entry.NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs)
	svc.SetRouteSnapshotRefresher(entrySvc)
	ctx := context.Background()
	now := time.Now()

	for _, hub := range []*store.HubInstance{
		{ID: "hub_a", OwnerEmail: "owner-a@example.com", Name: "Hub A", BaseURL: "https://a.example.com", Status: "online", HubSecretHash: hashToken("secret-a"), CreatedAt: now, UpdatedAt: now},
		{ID: "hub_b", OwnerEmail: "owner-b@example.com", Name: "Hub B", BaseURL: "https://b.example.com", Status: "online", HubSecretHash: hashToken("secret-b"), CreatedAt: now, UpdatedAt: now},
	} {
		if err := st.Hubs.Create(ctx, hub); err != nil {
			t.Fatalf("create hub %s: %v", hub.ID, err)
		}
	}
	if err := st.HubUserLinks.Upsert(ctx, &store.HubUserLink{ID: primaryUserLinkID("hub_a", "user@example.com"), HubID: "hub_a", Email: "user@example.com", IsDefault: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("seed source user link: %v", err)
	}
	if err := st.HubUserLinks.Upsert(ctx, &store.HubUserLink{ID: "target-existing-user-link", HubID: "hub_b", Email: "user@example.com", IsDefault: false, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("seed target user link: %v", err)
	}

	if _, err := svc.MigrateUser(ctx, MigrateUserRequest{Email: "user@example.com", ToHubID: "hub_b"}); err != nil {
		t.Fatalf("MigrateUser: %v", err)
	}
	if err := svc.SyncHubUserLink(ctx, "hub_a", "secret-a", "user@example.com", true); err != nil {
		t.Fatalf("source SyncHubUserLink: %v", err)
	}
	links, err := st.HubUserLinks.ListByEmail(ctx, "user@example.com")
	if err != nil {
		t.Fatalf("ListByEmail: %v", err)
	}
	foundTargetExisting := false
	for _, link := range links {
		if link.HubID == "hub_a" {
			t.Fatalf("expected source resync to be ignored after admin migration, got %+v", links)
		}
		if link.ID == "target-existing-user-link" && link.HubID == "hub_b" {
			foundTargetExisting = true
		}
	}
	if !foundTargetExisting {
		t.Fatalf("expected target hub existing link to be preserved, got %+v", links)
	}
	if err := entrySvc.Rebuild(ctx); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	resolved, err := entrySvc.ResolveByEmail(ctx, "user@example.com")
	if err != nil {
		t.Fatalf("ResolveByEmail: %v", err)
	}
	if resolved.DefaultHubID != "hub_b" || len(resolved.Hubs) != 1 || resolved.Hubs[0].HubID != "hub_b" {
		t.Fatalf("expected admin migration to keep hub_b after source resync, got %+v", resolved)
	}
}
func TestMigrateUserPatternMovesMatchingUsers(t *testing.T) {
	provider := newTestStore(t)
	st := sqlite.NewStore(provider)
	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs, st.System, &testMailer{}, "http://127.0.0.1:9388")
	ctx := context.Background()
	now := time.Now()

	for _, hub := range []*store.HubInstance{
		{ID: "hub_a", OwnerEmail: "owner-a@example.com", Name: "Hub A", BaseURL: "https://a.example.com", Status: "online", CreatedAt: now, UpdatedAt: now},
		{ID: "hub_b", OwnerEmail: "owner-b@example.com", Name: "Hub B", BaseURL: "https://b.example.com", Status: "online", CreatedAt: now, UpdatedAt: now},
		{ID: "hub_target", OwnerEmail: "owner-target@example.com", Name: "Hub Target", BaseURL: "https://target.example.com", Status: "online", CreatedAt: now, UpdatedAt: now},
	} {
		if err := st.Hubs.Create(ctx, hub); err != nil {
			t.Fatalf("create hub %s: %v", hub.ID, err)
		}
	}
	for _, item := range []struct{ hubID, email string }{
		{"hub_a", "mark@qianxin.com"},
		{"hub_b", "mary@qianxin.com"},
		{"hub_target", "max@qianxin.com"},
		{"hub_a", "tom@qianxin.com"},
		{"hub_b", "mary@other.com"},
	} {
		isDefault := item.email != "max@qianxin.com"
		if err := st.HubUserLinks.Upsert(ctx, &store.HubUserLink{ID: primaryUserLinkID(item.hubID, item.email), HubID: item.hubID, Email: item.email, IsDefault: isDefault, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatalf("seed user link: %v", err)
		}
	}

	result, err := svc.MigrateUser(ctx, MigrateUserRequest{Email: "\uff4d\uff41\uff0a\uff20\uff51\uff49\uff41\uff4e\uff58\uff49\uff4e\uff0e\uff43\uff4f\uff4d", ToHubID: "hub_target"})
	if err != nil {
		t.Fatalf("MigrateUser pattern: %v", err)
	}
	if result.Email != "ma*@qianxin.com" || len(result.UpsertedIDs) != 2 || len(result.RemovedIDs) != 2 {
		t.Fatalf("unexpected pattern migration result: %+v", result)
	}
	for _, email := range []string{"mark@qianxin.com", "mary@qianxin.com"} {
		links, err := st.HubUserLinks.ListByEmail(ctx, email)
		if err != nil {
			t.Fatalf("ListByEmail %s: %v", email, err)
		}
		for _, link := range links {
			if link.HubID != "hub_target" {
				t.Fatalf("expected %s only on target hub, got %+v", email, links)
			}
		}
	}
	links, err := st.HubUserLinks.ListByEmail(ctx, "tom@qianxin.com")
	if err != nil {
		t.Fatalf("ListByEmail tom: %v", err)
	}
	if len(links) != 1 || links[0].HubID != "hub_a" {
		t.Fatalf("expected non-matching user untouched, got %+v", links)
	}
	links, err = st.HubUserLinks.ListByEmail(ctx, "max@qianxin.com")
	if err != nil {
		t.Fatalf("ListByEmail max: %v", err)
	}
	if len(links) != 1 || links[0].HubID != "hub_target" || links[0].IsDefault {
		t.Fatalf("expected existing target user untouched, got %+v", links)
	}
}

func TestMigrateUserPatternWithSourceHubOnlyMovesThatSource(t *testing.T) {
	provider := newTestStore(t)
	st := sqlite.NewStore(provider)
	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs, st.System, &testMailer{}, "http://127.0.0.1:9388")
	ctx := context.Background()
	now := time.Now()

	for _, hub := range []*store.HubInstance{
		{ID: "hub_a", OwnerEmail: "owner-a@example.com", Name: "Hub A", BaseURL: "https://a.example.com", Status: "online", CreatedAt: now, UpdatedAt: now},
		{ID: "hub_b", OwnerEmail: "owner-b@example.com", Name: "Hub B", BaseURL: "https://b.example.com", Status: "online", CreatedAt: now, UpdatedAt: now},
		{ID: "hub_target", OwnerEmail: "owner-target@example.com", Name: "Hub Target", BaseURL: "https://target.example.com", Status: "online", CreatedAt: now, UpdatedAt: now},
	} {
		if err := st.Hubs.Create(ctx, hub); err != nil {
			t.Fatalf("create hub %s: %v", hub.ID, err)
		}
	}
	for _, item := range []struct{ hubID, email string }{
		{"hub_a", "mark@qianxin.com"},
		{"hub_b", "mary@qianxin.com"},
	} {
		if err := st.HubUserLinks.Upsert(ctx, &store.HubUserLink{ID: primaryUserLinkID(item.hubID, item.email), HubID: item.hubID, Email: item.email, IsDefault: true, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatalf("seed user link: %v", err)
		}
	}

	result, err := svc.MigrateUser(ctx, MigrateUserRequest{Email: "*@qianxin.com", FromHubID: "hub_a", ToHubID: "hub_target"})
	if err != nil {
		t.Fatalf("MigrateUser pattern with source: %v", err)
	}
	if len(result.UpsertedIDs) != 1 || len(result.RemovedIDs) != 1 {
		t.Fatalf("unexpected source-scoped migration result: %+v", result)
	}
	links, err := st.HubUserLinks.ListByEmail(ctx, "mary@qianxin.com")
	if err != nil {
		t.Fatalf("ListByEmail mary: %v", err)
	}
	if len(links) != 1 || links[0].HubID != "hub_b" {
		t.Fatalf("expected non-source matched user untouched, got %+v", links)
	}
}

func TestMigrateDomainMakesTargetHubDefault(t *testing.T) {
	provider := newTestStore(t)
	st := sqlite.NewStore(provider)
	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs, st.System, &testMailer{}, "http://127.0.0.1:9388")
	entrySvc := entry.NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs)
	svc.SetRouteSnapshotRefresher(entrySvc)
	ctx := context.Background()
	now := time.Now()

	for _, hub := range []*store.HubInstance{
		{ID: "hub_a", OwnerEmail: "owner-a@example.com", Name: "Hub A", BaseURL: "https://a.example.com", Status: "online", CorporateEmailDomain: "example.com", CreatedAt: now, UpdatedAt: now},
		{ID: "hub_b", OwnerEmail: "owner-b@example.com", Name: "Hub B", BaseURL: "https://b.example.com", Status: "online", CreatedAt: now, UpdatedAt: now},
	} {
		if err := st.Hubs.Create(ctx, hub); err != nil {
			t.Fatalf("create hub %s: %v", hub.ID, err)
		}
	}
	if err := st.HubDomainRoutes.Upsert(ctx, &store.HubDomainRoute{ID: domainRouteID("hub_a", 0), HubID: "hub_a", Domain: "example.com", Enabled: true, Priority: 100, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("seed domain route: %v", err)
	}
	if err := st.HubDomainRoutes.Upsert(ctx, &store.HubDomainRoute{ID: "target-existing-domain-route", HubID: "hub_b", Domain: "example.com", Enabled: true, Priority: 50, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("seed target domain route: %v", err)
	}
	if err := st.HubUserLinks.Upsert(ctx, &store.HubUserLink{ID: primaryUserLinkID("hub_a", "user@example.com"), HubID: "hub_a", Email: "user@example.com", IsDefault: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("seed source user link: %v", err)
	}

	result, err := svc.MigrateDomain(ctx, MigrateDomainRequest{Domain: "@EXAMPLE.com", ToHubID: "hub_b"})
	if err != nil {
		t.Fatalf("MigrateDomain: %v", err)
	}
	if result.Domain != "example.com" || result.ToHubID != "hub_b" || len(result.UpsertedIDs) < 1 {
		t.Fatalf("unexpected migration result: %+v", result)
	}
	resolved, err := entrySvc.ResolveByEmail(ctx, "new-user@example.com")
	if err != nil {
		t.Fatalf("ResolveByEmail: %v", err)
	}
	if resolved.DefaultHubID != "hub_b" {
		t.Fatalf("expected hub_b default after domain migration, got %+v", resolved)
	}
	if len(resolved.Hubs) != 1 || resolved.Hubs[0].HubID != "hub_b" {
		t.Fatalf("expected domain migration to return only target hub, got %+v", resolved)
	}
	routes, err := st.HubDomainRoutes.ListAll(ctx)
	if err != nil {
		t.Fatalf("ListAll routes: %v", err)
	}
	foundExistingTarget := false
	for _, route := range routes {
		if route.Domain != "example.com" {
			continue
		}
		if route.HubID == "hub_a" {
			t.Fatalf("expected source hub route to be removed, got %+v", routes)
		}
		if route.ID == "target-existing-domain-route" && route.HubID == "hub_b" {
			foundExistingTarget = true
		}
	}
	if !foundExistingTarget {
		t.Fatalf("expected existing target hub route to be preserved, got %+v", routes)
	}
	links, err := st.HubUserLinks.ListByEmail(ctx, "user@example.com")
	if err != nil {
		t.Fatalf("ListByEmail migrated user: %v", err)
	}
	for _, link := range links {
		if link != nil && link.HubID == "hub_a" {
			t.Fatalf("expected domain migration to remove source user link after target override is written, got %+v", links)
		}
	}
	if len(links) == 0 {
		t.Fatalf("expected migrated user link to remain discoverable on target hub")
	}
}

func TestRegisterHubKeepsExistingUserLinksOnReRegister(t *testing.T) {
	provider := newTestStore(t)
	st := sqlite.NewStore(provider)
	mailer := &testMailer{}
	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs, st.System, mailer, "http://127.0.0.1:9388")
	ctx := context.Background()
	now := time.Now()

	hub := &store.HubInstance{ID: "hub_keep_links", InstallationID: "inst_keep_links", OwnerEmail: "owner@example.com", Name: "Hub", BaseURL: "https://hub.example.com", Status: "online", HubSecretHash: hashToken("old-secret"), CreatedAt: now, UpdatedAt: now}
	if err := st.Hubs.Create(ctx, hub); err != nil {
		t.Fatalf("create hub: %v", err)
	}
	if err := st.HubUserLinks.Upsert(ctx, &store.HubUserLink{ID: primaryUserLinkID(hub.ID, "user@example.com"), HubID: hub.ID, Email: "user@example.com", IsDefault: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("seed user link: %v", err)
	}

	if _, err := svc.RegisterHub(ctx, RegisterHubRequest{InstallationID: hub.InstallationID, OwnerEmail: "owner@example.com", Name: "Hub", BaseURL: "https://hub.example.com"}); err != nil {
		t.Fatalf("RegisterHub: %v", err)
	}
	items, err := st.HubUserLinks.ListByEmail(ctx, "user@example.com")
	if err != nil {
		t.Fatalf("ListByEmail: %v", err)
	}
	if len(items) != 1 || items[0].HubID != hub.ID {
		t.Fatalf("expected user link preserved, got %+v", items)
	}
	ownerLink, err := st.HubUserLinks.GetDefaultByEmail(ctx, "owner@example.com")
	if err != nil {
		t.Fatalf("GetDefaultByEmail owner: %v", err)
	}
	if ownerLink == nil || ownerLink.HubID != hub.ID {
		t.Fatalf("expected owner link for hub, got %+v", ownerLink)
	}
}
func TestRegisterAndHeartbeatHub(t *testing.T) {
	provider := newTestStore(t)
	st := sqlite.NewStore(provider)
	mailer := &testMailer{}
	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs, st.System, mailer, "http://127.0.0.1:9388")
	ctx := context.Background()

	result, err := svc.RegisterHub(ctx, RegisterHubRequest{
		OwnerEmail:     "owner@example.com",
		Name:           "MaClaw Team Hub",
		Description:    "Team remote coding hub",
		BaseURL:        "https://teamhub.example.com",
		Host:           "teamhub.example.com",
		Port:           9399,
		Visibility:     "shared",
		EnrollmentMode: "approval",
		Capabilities: map[string]any{
			"supports_remote_control": true,
		},
	})
	if err != nil {
		t.Fatalf("RegisterHub: %v", err)
	}
	if result == nil || result.HubID == "" || result.HubSecret == "" {
		t.Fatalf("unexpected register result: %+v", result)
	}

	hub, err := st.Hubs.GetByID(ctx, result.HubID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if hub == nil || hub.OwnerEmail != "owner@example.com" || hub.Status != "pending_confirmation" {
		t.Fatalf("unexpected hub row: %+v", hub)
	}
	if hub.Host != "teamhub.example.com" || hub.Port != 9399 {
		t.Fatalf("expected host/port to be stored, got %+v", hub)
	}
	if hub.BaseURL != "https://teamhub.example.com" {
		t.Fatalf("expected base url to be preserved, got %+v", hub)
	}

	link, err := st.HubUserLinks.GetDefaultByEmail(ctx, "owner@example.com")
	if err != nil {
		t.Fatalf("GetDefaultByEmail: %v", err)
	}
	if link == nil || link.HubID != result.HubID {
		t.Fatalf("expected default hub link for owner, got %+v", link)
	}

	token := tokenFromURL(mailer.lastConfirmURL)
	if err := svc.ConfirmRegistration(ctx, token); err != nil {
		t.Fatalf("ConfirmRegistration: %v", err)
	}

	if err := svc.HeartbeatHubWithSecret(ctx, result.HubID, result.HubSecret, nil, nil); err != nil {
		t.Fatalf("HeartbeatHubWithSecret: %v", err)
	}

	if err := svc.HeartbeatHubWithSecret(ctx, result.HubID, "wrong-secret", nil, nil); err != ErrHubUnauthorized {
		t.Fatalf("expected ErrHubUnauthorized, got %v", err)
	}
}

func TestRegisterHubUsesConfiguredPublicBaseURLForConfirmation(t *testing.T) {
	provider := newTestStore(t)
	st := sqlite.NewStore(provider)
	mailer := &testMailer{}
	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs, st.System, mailer, "http://127.0.0.1:9388")
	ctx := context.Background()

	if _, err := svc.SetPublicBaseURL(ctx, "https://center.example.com"); err != nil {
		t.Fatalf("SetPublicBaseURL: %v", err)
	}
	if _, err := svc.RegisterHub(ctx, RegisterHubRequest{
		OwnerEmail: "owner@example.com",
		Name:       "MaClaw Team Hub",
		BaseURL:    "https://teamhub.example.com",
	}); err != nil {
		t.Fatalf("RegisterHub: %v", err)
	}
	if len(mailer.lastConfirmURL) == 0 || mailer.lastConfirmURL[:len("https://center.example.com")] != "https://center.example.com" {
		t.Fatalf("expected confirm url to use configured public base url, got %s", mailer.lastConfirmURL)
	}
}

func TestRegisterHubNormalizesCorporateEmailDomain(t *testing.T) {
	provider := newTestStore(t)
	st := sqlite.NewStore(provider)
	mailer := &testMailer{}
	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs, st.System, mailer, "http://127.0.0.1:9388")
	ctx := context.Background()

	result, err := svc.RegisterHub(ctx, RegisterHubRequest{
		OwnerEmail:           "owner@example.com",
		Name:                 "Corporate Hub",
		BaseURL:              "https://corp.example.com",
		CorporateEmailDomain: "@RAPIDAI.TECH",
	})
	if err != nil {
		t.Fatalf("RegisterHub: %v", err)
	}

	hub, err := st.Hubs.GetByID(ctx, result.HubID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if hub == nil {
		t.Fatal("expected hub to exist")
	}
	if hub.CorporateEmailDomain != "rapidai.tech" {
		t.Fatalf("expected normalized corporate domain, got %+v", hub)
	}
}

func TestRegisterHubStoresMultipleCorporateEmailDomains(t *testing.T) {
	provider := newTestStore(t)
	st := sqlite.NewStore(provider)
	mailer := &testMailer{}
	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs, st.System, mailer, "http://127.0.0.1:9388")
	ctx := context.Background()

	result, err := svc.RegisterHub(ctx, RegisterHubRequest{
		OwnerEmail:            "owner@example.com",
		Name:                  "Corporate Hub",
		BaseURL:               "https://corp.example.com",
		CorporateEmailDomains: []string{"@RAPIDAI.TECH", "subsidiary.example", "rapidai.tech"},
		AcceptPublicSignup:    boolPtr(true),
	})
	if err != nil {
		t.Fatalf("RegisterHub: %v", err)
	}

	hub, err := st.Hubs.GetByID(ctx, result.HubID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if hub == nil || hub.CorporateEmailDomain != "rapidai.tech" || !hub.AcceptPublicSignup {
		t.Fatalf("unexpected hub: %+v", hub)
	}
	routes, err := st.HubDomainRoutes.ListAll(ctx)
	if err != nil {
		t.Fatalf("ListAll routes: %v", err)
	}
	if len(routes) != 2 || routes[0].Domain != "rapidai.tech" || routes[1].Domain != "subsidiary.example" {
		t.Fatalf("unexpected routes: %+v", routes)
	}
}

func TestConfirmHubRegistrationByAdmin(t *testing.T) {
	provider := newTestStore(t)
	st := sqlite.NewStore(provider)
	mailer := &testMailer{}
	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs, st.System, mailer, "http://127.0.0.1:9388")
	ctx := context.Background()

	result, err := svc.RegisterHub(ctx, RegisterHubRequest{
		OwnerEmail: "owner@example.com",
		Name:       "Pending Hub",
		BaseURL:    "https://teamhub.example.com",
		Host:       "teamhub.example.com",
		Port:       9399,
	})
	if err != nil {
		t.Fatalf("RegisterHub: %v", err)
	}

	if err := svc.ConfirmHubRegistrationByAdmin(ctx, result.HubID); err != nil {
		t.Fatalf("ConfirmHubRegistrationByAdmin: %v", err)
	}

	hub, err := st.Hubs.GetByID(ctx, result.HubID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if hub == nil || hub.Status != "online" {
		t.Fatalf("expected hub to be online after manual confirm, got %+v", hub)
	}
}

func TestRegisterHubRejectsBlockedEmailAndIP(t *testing.T) {
	provider := newTestStore(t)
	st := sqlite.NewStore(provider)
	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs, st.System, &testMailer{}, "http://127.0.0.1:9388")
	ctx := context.Background()

	now := time.Now()
	if err := st.BlockedEmails.Create(ctx, &store.BlockedEmail{
		ID:        "be_1",
		Email:     "owner@example.com",
		Reason:    "abuse",
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create blocked email: %v", err)
	}

	if _, err := svc.RegisterHub(ctx, RegisterHubRequest{
		OwnerEmail:     "owner@example.com",
		Name:           "Blocked Hub",
		BaseURL:        "https://blocked.example.com",
		Visibility:     "private",
		EnrollmentMode: "open",
	}); err != ErrEmailBlocked {
		t.Fatalf("expected ErrEmailBlocked, got %v", err)
	}

	if err := st.BlockedEmails.DeleteByEmail(ctx, "owner@example.com"); err != nil {
		t.Fatalf("delete blocked email: %v", err)
	}

	if err := st.BlockedIPs.Create(ctx, &store.BlockedIP{
		ID:        "bi_1",
		IP:        "10.0.0.7",
		Reason:    "scanner",
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create blocked ip: %v", err)
	}

	if _, err := svc.RegisterHubFromIP(ctx, RegisterHubRequest{
		OwnerEmail:     "owner@example.com",
		Name:           "Blocked Hub",
		BaseURL:        "https://blocked.example.com",
		Visibility:     "private",
		EnrollmentMode: "open",
	}, "10.0.0.7"); err != ErrIPBlocked {
		t.Fatalf("expected ErrIPBlocked, got %v", err)
	}
}

func TestRebuildHubUserEmailInventoryRemovesStaleOrdinaryLinks(t *testing.T) {
	provider := newTestStore(t)
	st := sqlite.NewStore(provider)
	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs, st.System, &testMailer{}, "http://127.0.0.1:9388")
	ctx := context.Background()
	now := time.Now()

	for _, link := range []*store.HubUserLink{
		{ID: primaryUserLinkID("hub_a", "stale@example.com"), HubID: "hub_a", Email: "stale@example.com", IsDefault: false, CreatedAt: now, UpdatedAt: now},
		{ID: primaryUserLinkID("hub_a", "keep@example.com"), HubID: "hub_a", Email: "keep@example.com", IsDefault: false, CreatedAt: now, UpdatedAt: now},
		{ID: adminUserLinkID("stale@example.com"), HubID: "hub_target", Email: "stale@example.com", IsDefault: true, CreatedAt: now, UpdatedAt: now},
	} {
		if err := st.HubUserLinks.Upsert(ctx, link); err != nil {
			t.Fatalf("seed link %s: %v", link.ID, err)
		}
	}

	if err := svc.rebuildHubUserEmailInventory(ctx, "hub_a", []string{"keep@example.com"}, now); err != nil {
		t.Fatalf("rebuildHubUserEmailInventory: %v", err)
	}
	stale, err := st.HubUserLinks.ListByEmail(ctx, "stale@example.com")
	if err != nil {
		t.Fatalf("ListByEmail stale: %v", err)
	}
	if len(stale) != 1 || stale[0].ID != adminUserLinkID("stale@example.com") {
		t.Fatalf("expected stale ordinary link removed while admin override remains, got %+v", stale)
	}
	keep, err := st.HubUserLinks.ListByEmail(ctx, "keep@example.com")
	if err != nil {
		t.Fatalf("ListByEmail keep: %v", err)
	}
	if len(keep) != 1 || keep[0].HubID != "hub_a" {
		t.Fatalf("expected current inventory link preserved, got %+v", keep)
	}
}
func TestRefreshUserInventoryAttemptsHubWithoutCapabilityFlag(t *testing.T) {
	provider := newTestStore(t)
	st := sqlite.NewStore(provider)
	ctx := context.Background()
	now := time.Now()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/center/user-migration/export" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"users": []any{
				map[string]any{"user": map[string]any{"email": "xx@qianxin.com"}},
				map[string]any{"user": map[string]any{"email": "ma@qianxin.com"}},
			},
		})
	}))
	defer server.Close()

	hub := &store.HubInstance{
		ID:            "hub_mypapers",
		OwnerEmail:    "owner@example.com",
		Name:          "Papers",
		BaseURL:       server.URL,
		Status:        "online",
		HubSecretHash: hashToken("secret"),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := st.Hubs.Create(ctx, hub); err != nil {
		t.Fatalf("create hub: %v", err)
	}

	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs, st.System, &testMailer{}, "http://127.0.0.1:9388")
	result, err := svc.RefreshUserInventory(ctx)
	if err != nil {
		t.Fatalf("RefreshUserInventory: %v", err)
	}
	if result.HubsRefreshed != 1 || result.UsersIndexed != 2 || result.HubsFailed != 0 {
		t.Fatalf("unexpected refresh result: %+v", result)
	}
	links, err := st.HubUserLinks.ListByEmail(ctx, "xx@qianxin.com")
	if err != nil {
		t.Fatalf("ListByEmail: %v", err)
	}
	if len(links) != 1 || links[0].HubID != hub.ID {
		t.Fatalf("expected refreshed inventory link on source hub, got %+v", links)
	}
	stored, err := st.Hubs.GetByID(ctx, hub.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if !hubSupportsDirectUserMigration(stored) {
		t.Fatalf("expected refresh to mark hub as supporting user data migration, caps=%s", stored.CapabilitiesJSON)
	}
}

func TestLocalUserMigrationCleanupRemovesSourceInventory(t *testing.T) {
	provider := newTestStore(t)
	st := sqlite.NewStore(provider)
	ctx := context.Background()
	now := time.Now()

	var sourceDeleted []string
	sourceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/center/user-migration/export":
			_ = json.NewEncoder(w).Encode(map[string]any{"users": []any{map[string]any{"user": map[string]any{"email": "xx@qianxin.com"}}}})
		case "/api/center/user-migration/delete":
			var req remoteUserMigrationRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			sourceDeleted = append(sourceDeleted, req.Emails...)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer sourceServer.Close()
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/center/user-migration/import" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer targetServer.Close()

	sourceHub := &store.HubInstance{ID: "hub_source", OwnerEmail: "owner-a@example.com", Name: "Source", BaseURL: sourceServer.URL, Status: "online", CapabilitiesJSON: `{"user_emails":["xx@qianxin.com","keep@qianxin.com"],"supports_user_data_migration":true}`, HubSecretHash: hashToken("secret-a"), CreatedAt: now, UpdatedAt: now}
	targetHub := &store.HubInstance{ID: "hub_target", OwnerEmail: "owner-b@example.com", Name: "Target", BaseURL: targetServer.URL, Status: "online", CapabilitiesJSON: `{"user_emails":[],"supports_user_data_migration":true}`, HubSecretHash: hashToken("secret-b"), CreatedAt: now, UpdatedAt: now}
	for _, hub := range []*store.HubInstance{sourceHub, targetHub} {
		if err := st.Hubs.Create(ctx, hub); err != nil {
			t.Fatalf("create hub %s: %v", hub.ID, err)
		}
	}
	if err := st.HubUserLinks.Upsert(ctx, &store.HubUserLink{ID: primaryUserLinkID(sourceHub.ID, "xx@qianxin.com"), HubID: sourceHub.ID, Email: "xx@qianxin.com", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("seed source inventory: %v", err)
	}

	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs, st.System, &testMailer{}, "http://127.0.0.1:9388")
	cleanup, err := svc.prepareLocalUserMigration(ctx, map[string][]string{sourceHub.ID: {"xx@qianxin.com"}}, targetHub.ID)
	if err != nil {
		t.Fatalf("prepareLocalUserMigration: %v", err)
	}
	if err := cleanup(ctx); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if len(sourceDeleted) != 1 || sourceDeleted[0] != "xx@qianxin.com" {
		t.Fatalf("expected source delete call for migrated user, got %+v", sourceDeleted)
	}
	links, err := st.HubUserLinks.ListByEmail(ctx, "xx@qianxin.com")
	if err != nil {
		t.Fatalf("ListByEmail: %v", err)
	}
	for _, link := range links {
		if link.HubID == sourceHub.ID {
			t.Fatalf("expected source inventory removed after cleanup, got %+v", links)
		}
	}
	stored, err := st.Hubs.GetByID(ctx, sourceHub.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if strings.Contains(stored.CapabilitiesJSON, "xx@qianxin.com") || !strings.Contains(stored.CapabilitiesJSON, "keep@qianxin.com") {
		t.Fatalf("expected source capability inventory to remove only migrated user, caps=%s", stored.CapabilitiesJSON)
	}
}
func TestCollectUserMigrationSourcesFallsBackToHeartbeatInventory(t *testing.T) {
	provider := newTestStore(t)
	st := sqlite.NewStore(provider)
	svc := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs, st.System, &testMailer{}, "http://127.0.0.1:9388")
	ctx := context.Background()
	now := time.Now()

	for _, hub := range []*store.HubInstance{
		{ID: "hub_source", OwnerEmail: "owner-a@example.com", Name: "Source", BaseURL: "https://source.example.com", Status: "online", CapabilitiesJSON: `{"user_emails":["old@qianxin.com"],"supports_user_data_migration":true}`, HubSecretHash: hashToken("secret-a"), CreatedAt: now, UpdatedAt: now},
		{ID: "hub_target", OwnerEmail: "owner-b@example.com", Name: "Target", BaseURL: "https://target.example.com", Status: "online", CapabilitiesJSON: `{"user_emails":[],"supports_user_data_migration":true}`, HubSecretHash: hashToken("secret-b"), CreatedAt: now, UpdatedAt: now},
	} {
		if err := st.Hubs.Create(ctx, hub); err != nil {
			t.Fatalf("create hub %s: %v", hub.ID, err)
		}
	}
	if err := st.HubUserLinks.Upsert(ctx, &store.HubUserLink{ID: adminUserLinkID("old@qianxin.com"), HubID: "hub_target", Email: "old@qianxin.com", IsDefault: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("seed admin override: %v", err)
	}

	sources, err := svc.collectUserMigrationSources(ctx, "*@qianxin.com", "", "hub_target")
	if err != nil {
		t.Fatalf("collectUserMigrationSources: %v", err)
	}
	got := sources["hub_source"]
	if len(got) != 1 || got[0] != "old@qianxin.com" {
		t.Fatalf("expected heartbeat inventory source for historical migration repair, got %+v", sources)
	}
}
func TestHeartbeatUserEmailInventoryMakesScatteredUsersDiscoverable(t *testing.T) {
	provider := newTestStore(t)
	st := sqlite.NewStore(provider)
	ctx := context.Background()
	mailer := &testMailer{}
	hubService := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs, st.System, mailer, "http://127.0.0.1:9388")

	register := func(owner, name, baseURL string) *RegisterHubResult {
		result, err := hubService.RegisterHub(ctx, RegisterHubRequest{
			OwnerEmail:     owner,
			Name:           name,
			BaseURL:        baseURL,
			Visibility:     "private",
			EnrollmentMode: "open",
		})
		if err != nil {
			t.Fatalf("RegisterHub %s: %v", name, err)
		}
		if err := hubService.ConfirmRegistration(ctx, tokenFromURL(mailer.lastConfirmURL)); err != nil {
			t.Fatalf("ConfirmRegistration %s: %v", name, err)
		}
		return result
	}

	hubA := register("owner-a@example.com", "Papers", "https://hub.mypapers.top")
	hubB := register("owner-b@example.com", "Maclaw", "https://hub.maclaw.top")

	if err := hubService.HeartbeatHubWithSecret(ctx, hubA.HubID, hubA.HubSecret, nil, &HeartbeatHubUpdate{
		BaseURL:        "https://hub.mypapers.top",
		Visibility:     "private",
		EnrollmentMode: "open",
		Capabilities:   map[string]any{"user_emails": []any{"alice@qianxin.com", "bob@qianxin.com"}},
	}); err != nil {
		t.Fatalf("HeartbeatHubWithSecret hubA: %v", err)
	}
	if err := hubService.HeartbeatHubWithSecret(ctx, hubB.HubID, hubB.HubSecret, nil, &HeartbeatHubUpdate{
		BaseURL:        "https://hub.maclaw.top",
		Visibility:     "private",
		EnrollmentMode: "open",
		Capabilities:   map[string]any{"user_emails": []any{"alice@qianxin.com"}},
	}); err != nil {
		t.Fatalf("HeartbeatHubWithSecret hubB: %v", err)
	}

	inventoryLinks, err := st.HubUserLinks.ListByEmail(ctx, "alice@qianxin.com")
	if err != nil {
		t.Fatalf("ListByEmail alice inventory: %v", err)
	}
	for _, link := range inventoryLinks {
		if link != nil && link.IsDefault {
			t.Fatalf("inventory links should not claim default ownership, got %+v", inventoryLinks)
		}
	}

	entryService := entry.NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs)
	result, err := entryService.ResolveAdminByEmailPattern(ctx, "*@qianxin.com")
	if err != nil {
		t.Fatalf("ResolveAdminByEmailPattern: %v", err)
	}
	if result.Mode != "multiple" || !resultHasHub(result, hubA.HubID) || !resultHasHub(result, hubB.HubID) {
		t.Fatalf("expected scattered qianxin users on both hubs, got %+v", result)
	}

	if _, err := hubService.MigrateUser(ctx, MigrateUserRequest{Email: "*@qianxin.com", ToHubID: hubB.HubID}); err != nil {
		t.Fatalf("MigrateUser pattern: %v", err)
	}
	if err := hubService.HeartbeatHubWithSecret(ctx, hubA.HubID, hubA.HubSecret, nil, &HeartbeatHubUpdate{
		BaseURL:        "https://hub.mypapers.top",
		Visibility:     "private",
		EnrollmentMode: "open",
		Capabilities:   map[string]any{"user_emails": []any{"alice@qianxin.com", "bob@qianxin.com"}},
	}); err != nil {
		t.Fatalf("HeartbeatHubWithSecret source after migration: %v", err)
	}
	links, err := st.HubUserLinks.ListByEmail(ctx, "bob@qianxin.com")
	if err != nil {
		t.Fatalf("ListByEmail bob: %v", err)
	}
	for _, link := range links {
		if link != nil && link.HubID == hubA.HubID && !isOwnerLink(link) {
			t.Fatalf("expected migrated user to leave source hub only after target override is written, got %+v", links)
		}
	}
}

func resultHasHub(result *entry.ResolveResult, hubID string) bool {
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
func TestDisabledHubStaysDisabledAfterHeartbeat(t *testing.T) {
	provider := newTestStore(t)
	st := sqlite.NewStore(provider)
	mailer := &testMailer{}
	hubService := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs, st.System, mailer, "http://127.0.0.1:9388")
	ctx := context.Background()

	result, err := hubService.RegisterHub(ctx, RegisterHubRequest{
		OwnerEmail:     "owner@example.com",
		Name:           "Disable Me",
		BaseURL:        "https://disabled.example.com",
		Host:           "disabled.example.com",
		Port:           9399,
		Visibility:     "private",
		EnrollmentMode: "open",
	})
	if err != nil {
		t.Fatalf("RegisterHub: %v", err)
	}
	token := tokenFromURL(mailer.lastConfirmURL)
	if err := hubService.ConfirmRegistration(ctx, token); err != nil {
		t.Fatalf("ConfirmRegistration: %v", err)
	}

	if err := hubService.DisableHub(ctx, result.HubID, "maintenance"); err != nil {
		t.Fatalf("DisableHub: %v", err)
	}

	if err := hubService.HeartbeatHubWithSecret(ctx, result.HubID, result.HubSecret, nil, nil); err != ErrHubDisabled {
		t.Fatalf("expected ErrHubDisabled, got %v", err)
	}

	hub, err := st.Hubs.GetByID(ctx, result.HubID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if hub == nil {
		t.Fatal("expected hub to exist")
	}
	if !hub.IsDisabled {
		t.Fatalf("expected hub to remain disabled, got %+v", hub)
	}
	if hub.Status != "disabled" {
		t.Fatalf("expected disabled status after heartbeat, got %+v", hub)
	}
}

func TestDisabledHubCannotReregister(t *testing.T) {
	provider := newTestStore(t)
	st := sqlite.NewStore(provider)
	mailer := &testMailer{}
	hubService := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs, st.System, mailer, "http://127.0.0.1:9388")
	ctx := context.Background()

	result, err := hubService.RegisterHub(ctx, RegisterHubRequest{
		InstallationID: "inst_disabled_again",
		OwnerEmail:     "owner@example.com",
		Name:           "Disable Again",
		BaseURL:        "https://disabled.example.com",
		Host:           "disabled.example.com",
		Port:           9399,
		Visibility:     "private",
		EnrollmentMode: "open",
	})
	if err != nil {
		t.Fatalf("RegisterHub: %v", err)
	}

	token := tokenFromURL(mailer.lastConfirmURL)
	if err := hubService.ConfirmRegistration(ctx, token); err != nil {
		t.Fatalf("ConfirmRegistration: %v", err)
	}
	if err := hubService.DisableHub(ctx, result.HubID, "maintenance"); err != nil {
		t.Fatalf("DisableHub: %v", err)
	}

	_, err = hubService.RegisterHub(ctx, RegisterHubRequest{
		InstallationID: "inst_disabled_again",
		OwnerEmail:     "owner@example.com",
		Name:           "Disable Again",
		BaseURL:        "https://disabled.example.com",
		Host:           "disabled.example.com",
		Port:           9399,
		Visibility:     "private",
		EnrollmentMode: "open",
	})
	if err != ErrHubDisabled {
		t.Fatalf("expected ErrHubDisabled, got %v", err)
	}
}

func TestDeleteHubRemovesRegistrationAndLinks(t *testing.T) {
	provider := newTestStore(t)
	st := sqlite.NewStore(provider)
	hubService := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs, st.System, &testMailer{}, "http://127.0.0.1:9388")
	ctx := context.Background()

	result, err := hubService.RegisterHub(ctx, RegisterHubRequest{
		OwnerEmail:     "owner@example.com",
		Name:           "Delete Me",
		BaseURL:        "https://delete.example.com",
		Host:           "delete.example.com",
		Port:           9399,
		Visibility:     "private",
		EnrollmentMode: "open",
	})
	if err != nil {
		t.Fatalf("RegisterHub: %v", err)
	}

	if err := hubService.DeleteHub(ctx, result.HubID); err != nil {
		t.Fatalf("DeleteHub: %v", err)
	}

	hub, err := st.Hubs.GetByID(ctx, result.HubID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if hub != nil {
		t.Fatalf("expected hub to be deleted, got %+v", hub)
	}

	link, err := st.HubUserLinks.GetDefaultByEmail(ctx, "owner@example.com")
	if err != nil {
		t.Fatalf("GetDefaultByEmail: %v", err)
	}
	if link != nil {
		t.Fatalf("expected default link to be removed, got %+v", link)
	}

	if err := hubService.HeartbeatHubWithSecret(ctx, result.HubID, result.HubSecret, nil, nil); err != ErrHubUnauthorized {
		t.Fatalf("expected deleted hub heartbeat to be unauthorized, got %v", err)
	}
}

func TestRegisterHubReusesExistingInstallationID(t *testing.T) {
	provider := newTestStore(t)
	st := sqlite.NewStore(provider)
	hubService := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs, st.System, &testMailer{}, "http://127.0.0.1:9388")
	ctx := context.Background()

	first, err := hubService.RegisterHub(ctx, RegisterHubRequest{
		InstallationID: "inst_same_machine",
		OwnerEmail:     "owner@example.com",
		Name:           "Original Hub",
		BaseURL:        "https://first.example.com",
		Host:           "first.example.com",
		Port:           9399,
		Visibility:     "private",
		EnrollmentMode: "open",
	})
	if err != nil {
		t.Fatalf("first RegisterHub: %v", err)
	}

	second, err := hubService.RegisterHub(ctx, RegisterHubRequest{
		InstallationID: "inst_same_machine",
		OwnerEmail:     "owner@example.com",
		Name:           "Renamed Hub",
		BaseURL:        "https://second.example.com",
		Host:           "second.example.com",
		Port:           9494,
		Visibility:     "shared",
		EnrollmentMode: "approval",
	})
	if err != nil {
		t.Fatalf("second RegisterHub: %v", err)
	}

	if first.HubID != second.HubID {
		t.Fatalf("expected duplicate registration to reuse hub id, got %q and %q", first.HubID, second.HubID)
	}
	if first.HubSecret == second.HubSecret {
		t.Fatalf("expected registration secret to rotate on re-register")
	}

	hubs, err := st.Hubs.ListAll(ctx)
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(hubs) != 1 {
		t.Fatalf("expected a single hub row after duplicate registration, got %d", len(hubs))
	}
	hub := hubs[0]
	if hub.Name != "Renamed Hub" || hub.BaseURL != "https://second.example.com" || hub.Host != "second.example.com" || hub.Port != 9494 {
		t.Fatalf("expected latest registration to update hub metadata, got %+v", hub)
	}
	if hub.InstallationID != "inst_same_machine" {
		t.Fatalf("expected installation id to persist, got %+v", hub)
	}
}

func TestRegisterHubKeepsRecentConfirmationLinksValid(t *testing.T) {
	provider := newTestStore(t)
	st := sqlite.NewStore(provider)
	mailer := &testMailer{}
	hubService := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs, st.System, mailer, "http://127.0.0.1:9388")
	ctx := context.Background()

	first, err := hubService.RegisterHub(ctx, RegisterHubRequest{
		InstallationID: "inst_retry_confirmation",
		OwnerEmail:     "owner@example.com",
		Name:           "Retry Hub",
		BaseURL:        "https://retry.example.com",
		Host:           "retry.example.com",
		Port:           9399,
		Visibility:     "private",
		EnrollmentMode: "open",
	})
	if err != nil {
		t.Fatalf("first RegisterHub: %v", err)
	}
	firstToken := tokenFromURL(mailer.lastConfirmURL)

	second, err := hubService.RegisterHub(ctx, RegisterHubRequest{
		InstallationID: "inst_retry_confirmation",
		OwnerEmail:     "owner@example.com",
		Name:           "Retry Hub",
		BaseURL:        "https://retry.example.com",
		Host:           "retry.example.com",
		Port:           9399,
		Visibility:     "private",
		EnrollmentMode: "open",
	})
	if err != nil {
		t.Fatalf("second RegisterHub: %v", err)
	}
	if first.HubID != second.HubID {
		t.Fatalf("expected same hub id, got %q and %q", first.HubID, second.HubID)
	}

	if err := hubService.ConfirmRegistration(ctx, firstToken); err != nil {
		t.Fatalf("ConfirmRegistration with earlier token: %v", err)
	}

	hub, err := st.Hubs.GetByID(ctx, first.HubID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if hub == nil || hub.Status != "online" {
		t.Fatalf("expected hub to be online after confirming earlier token, got %+v", hub)
	}
}

func boolPtr(v bool) *bool { return &v }

func TestDeleteHubSyncsHubUserLinkDeletes(t *testing.T) {
	provider := newTestStore(t)
	st := sqlite.NewStore(provider)
	sync := &fakeSyncRecorder{}
	hubService := NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs, st.System, &testMailer{}, "http://127.0.0.1:9388")
	hubService.SetSyncRecorder(sync)
	ctx := context.Background()

	result, err := hubService.RegisterHub(ctx, RegisterHubRequest{
		OwnerEmail:     "owner@example.com",
		Name:           "Delete Sync",
		BaseURL:        "https://delete-sync.example.com",
		Host:           "delete-sync.example.com",
		Port:           9399,
		Visibility:     "private",
		EnrollmentMode: "open",
	})
	if err != nil {
		t.Fatalf("RegisterHub: %v", err)
	}

	if err := hubService.DeleteHub(ctx, result.HubID); err != nil {
		t.Fatalf("DeleteHub: %v", err)
	}

	if len(sync.deletedHubLinks) != 1 || sync.deletedHubLinks[0] != primaryOwnerLinkID(result.HubID) {
		t.Fatalf("unexpected deleted hub links: %+v", sync.deletedHubLinks)
	}
	if len(sync.deletedHubInstances) != 1 || sync.deletedHubInstances[0] != result.HubID {
		t.Fatalf("unexpected deleted hub instances: %+v", sync.deletedHubInstances)
	}
}
