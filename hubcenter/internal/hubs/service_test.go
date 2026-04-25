package hubs

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
