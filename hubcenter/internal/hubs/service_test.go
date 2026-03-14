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

func TestRegisterAndHeartbeatHub(t *testing.T) {
	provider := newTestStore(t)
	st := sqlite.NewStore(provider)
	mailer := &testMailer{}
	svc := NewService(st.Hubs, st.HubUserLinks, st.BlockedEmails, st.BlockedIPs, st.System, mailer, "http://127.0.0.1:9388")
	ctx := context.Background()

	result, err := svc.RegisterHub(ctx, RegisterHubRequest{
		OwnerEmail:     "owner@example.com",
		Name:           "CodeClaw Team Hub",
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

	if err := svc.HeartbeatHubWithSecret(ctx, result.HubID, result.HubSecret, nil); err != nil {
		t.Fatalf("HeartbeatHubWithSecret: %v", err)
	}

	if err := svc.HeartbeatHubWithSecret(ctx, result.HubID, "wrong-secret", nil); err != ErrHubUnauthorized {
		t.Fatalf("expected ErrHubUnauthorized, got %v", err)
	}
}

func TestRegisterHubUsesConfiguredPublicBaseURLForConfirmation(t *testing.T) {
	provider := newTestStore(t)
	st := sqlite.NewStore(provider)
	mailer := &testMailer{}
	svc := NewService(st.Hubs, st.HubUserLinks, st.BlockedEmails, st.BlockedIPs, st.System, mailer, "http://127.0.0.1:9388")
	ctx := context.Background()

	if _, err := svc.SetPublicBaseURL(ctx, "https://center.example.com"); err != nil {
		t.Fatalf("SetPublicBaseURL: %v", err)
	}
	if _, err := svc.RegisterHub(ctx, RegisterHubRequest{
		OwnerEmail: "owner@example.com",
		Name:       "CodeClaw Team Hub",
		BaseURL:    "https://teamhub.example.com",
	}); err != nil {
		t.Fatalf("RegisterHub: %v", err)
	}
	if len(mailer.lastConfirmURL) == 0 || mailer.lastConfirmURL[:len("https://center.example.com")] != "https://center.example.com" {
		t.Fatalf("expected confirm url to use configured public base url, got %s", mailer.lastConfirmURL)
	}
}

func TestConfirmHubRegistrationByAdmin(t *testing.T) {
	provider := newTestStore(t)
	st := sqlite.NewStore(provider)
	mailer := &testMailer{}
	svc := NewService(st.Hubs, st.HubUserLinks, st.BlockedEmails, st.BlockedIPs, st.System, mailer, "http://127.0.0.1:9388")
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
	svc := NewService(st.Hubs, st.HubUserLinks, st.BlockedEmails, st.BlockedIPs, st.System, &testMailer{}, "http://127.0.0.1:9388")
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
	hubService := NewService(st.Hubs, st.HubUserLinks, st.BlockedEmails, st.BlockedIPs, st.System, mailer, "http://127.0.0.1:9388")
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

	if err := hubService.HeartbeatHubWithSecret(ctx, result.HubID, result.HubSecret, nil); err != ErrHubDisabled {
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
	hubService := NewService(st.Hubs, st.HubUserLinks, st.BlockedEmails, st.BlockedIPs, st.System, mailer, "http://127.0.0.1:9388")
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
	hubService := NewService(st.Hubs, st.HubUserLinks, st.BlockedEmails, st.BlockedIPs, st.System, &testMailer{}, "http://127.0.0.1:9388")
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

	if err := hubService.HeartbeatHubWithSecret(ctx, result.HubID, result.HubSecret, nil); err != ErrHubUnauthorized {
		t.Fatalf("expected deleted hub heartbeat to be unauthorized, got %v", err)
	}
}

func TestRegisterHubReusesExistingInstallationID(t *testing.T) {
	provider := newTestStore(t)
	st := sqlite.NewStore(provider)
	hubService := NewService(st.Hubs, st.HubUserLinks, st.BlockedEmails, st.BlockedIPs, st.System, &testMailer{}, "http://127.0.0.1:9388")
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
	hubService := NewService(st.Hubs, st.HubUserLinks, st.BlockedEmails, st.BlockedIPs, st.System, mailer, "http://127.0.0.1:9388")
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
