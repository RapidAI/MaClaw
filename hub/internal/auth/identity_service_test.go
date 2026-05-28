package auth

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type closedDefaultTenantRepo struct {
	store.TenantRepository
}

type testInvitationCodeValidator struct {
	required bool
	code     string
	consumed int
}

type testUserRouteSyncer struct {
	allowed     bool
	targetHubID string
	syncCalls   int
}

type testUserRouteSyncOnly struct {
	syncCalls int
}

func (s *testUserRouteSyncer) SyncUserRoute(context.Context, string, ...string) error {
	s.syncCalls++
	return nil
}

func (s *testUserRouteSyncer) AllowsUserRoute(context.Context, string, ...string) (bool, string, error) {
	return s.allowed, s.targetHubID, nil
}

func (s *testUserRouteSyncOnly) SyncUserRoute(context.Context, string, ...string) error {
	s.syncCalls++
	return nil
}

func (v *testInvitationCodeValidator) IsRequired(context.Context) (bool, error) {
	return v.required, nil
}
func (v *testInvitationCodeValidator) IsRequiredForTenant(context.Context, string) (bool, error) {
	return v.required, nil
}
func (v *testInvitationCodeValidator) ValidateAndConsume(_ context.Context, code string, _ string) error {
	return v.ValidateAndConsumeForTenant(context.Background(), store.DefaultTenantID, code, "")
}
func (v *testInvitationCodeValidator) ValidateAndConsumeForTenant(_ context.Context, _ string, code string, _ string) error {
	if code != v.code {
		return ErrInvalidInvitationCode
	}
	v.consumed++
	return nil
}
func (v *testInvitationCodeValidator) CheckExpiry(context.Context, string) (bool, *time.Time, error) {
	return false, nil, nil
}
func (v *testInvitationCodeValidator) CheckExpiryForTenant(context.Context, string, string) (bool, *time.Time, error) {
	return false, nil, nil
}

func (r closedDefaultTenantRepo) GetByID(ctx context.Context, id string) (*store.Tenant, error) {
	if id == store.DefaultTenantID {
		return &store.Tenant{ID: store.DefaultTenantID, Slug: "default", Name: "Default Tenant", Status: "active", SettingsJSON: `{"allow_user_registration":false}`}, nil
	}
	return r.TenantRepository.GetByID(ctx, id)
}

func (r closedDefaultTenantRepo) EnsureDefault(ctx context.Context) (*store.Tenant, error) {
	return r.GetByID(ctx, store.DefaultTenantID)
}

func TestIdentityServiceEnrollmentAndEmailLogin(t *testing.T) {
	deps := newTestStore(t)
	svc := NewIdentityService(
		deps.store.Users,
		deps.store.Enrollments,
		deps.store.EmailBlocks,
		deps.store.Machines,
		deps.store.ViewerTokens,
		deps.store.LoginTokens,
		deps.store.System,
		nil,
		"open",
		true,
		nil,
		"http://127.0.0.1:9399",
	)
	ctx := context.Background()

	enroll, err := svc.StartEnrollment(ctx, "user@example.com", "office-pc", "windows", "", "")
	if err != nil {
		t.Fatalf("StartEnrollment: %v", err)
	}
	if enroll == nil || enroll.Status != "approved" {
		t.Fatalf("unexpected enrollment result: %+v", enroll)
	}
	if enroll.Email != "user@example.com" || enroll.SN == "" || enroll.MachineID == "" || enroll.MachineToken == "" {
		t.Fatalf("enrollment missing identity fields: %+v", enroll)
	}

	principal, err := svc.AuthenticateMachine(ctx, enroll.MachineID, enroll.MachineToken)
	if err != nil {
		t.Fatalf("AuthenticateMachine: %v", err)
	}
	if principal == nil || principal.UserID == "" || principal.MachineID != enroll.MachineID {
		t.Fatalf("unexpected machine principal: %+v", principal)
	}

	req, err := svc.RequestEmailLogin(ctx, "user@example.com")
	if err != nil {
		t.Fatalf("RequestEmailLogin: %v", err)
	}
	if req == nil || req.Status != "pending_email_confirmation" {
		t.Fatalf("unexpected login request result: %+v", req)
	}
	if req.Message == "" {
		t.Fatal("expected dev confirm URL message when mailer is nil")
	}

	prefix := "Use this confirm URL for development: "
	if len(req.Message) <= len(prefix) || req.Message[:len(prefix)] != prefix {
		t.Fatalf("unexpected confirm message: %q", req.Message)
	}

	confirmURL := req.Message[len(prefix):]
	if parsedBase, err := url.Parse(confirmURL); err == nil {
		if parsedBase.Scheme != "http" || parsedBase.Host != "127.0.0.1:9399" {
			t.Fatalf("unexpected confirm URL host: %s", confirmURL)
		}
	}
	parsedURL, err := url.Parse(confirmURL)
	if err != nil {
		t.Fatalf("parse confirm URL: %v", err)
	}
	rawToken := parsedURL.Query().Get("token")
	if rawToken == "" {
		t.Fatalf("missing token in confirm URL: %s", confirmURL)
	}
	viewerToken, user, err := svc.ConfirmEmailLogin(ctx, rawToken)
	if err != nil {
		t.Fatalf("ConfirmEmailLogin: %v", err)
	}
	if viewerToken == "" {
		t.Fatal("expected viewer token")
	}
	if user == nil || user.Email != "user@example.com" || user.SN != enroll.SN {
		t.Fatalf("unexpected user after confirm: %+v", user)
	}
}

func TestIdentityServiceRejectsNewUserWhenCenterRoutesEmailElsewhere(t *testing.T) {
	deps := newTestStore(t)
	svc := NewIdentityService(
		deps.store.Users,
		deps.store.Enrollments,
		deps.store.EmailBlocks,
		deps.store.Machines,
		deps.store.ViewerTokens,
		deps.store.LoginTokens,
		deps.store.System,
		nil,
		"open",
		true,
		nil,
		"http://127.0.0.1:9399",
	)
	svc.SetUserRouteSyncer(&testUserRouteSyncer{allowed: false, targetHubID: "hub_maclaw"})

	if _, err := svc.RequestEmailLogin(context.Background(), "user@qianxin.com"); err == nil || !errors.Is(err, ErrRoutedToAnotherHub) {
		t.Fatalf("RequestEmailLogin error = %v, want ErrRoutedToAnotherHub", err)
	}
	user, err := deps.store.Users.GetByTenantEmail(context.Background(), store.DefaultTenantID, "user@qianxin.com")
	if err != nil || user != nil {
		t.Fatalf("routed-away login should not create local user, user=%+v err=%v", user, err)
	}

	if _, err := svc.StartEnrollment(context.Background(), "user@qianxin.com", "pc", "windows", "client", ""); err == nil || !errors.Is(err, ErrRoutedToAnotherHub) {
		t.Fatalf("StartEnrollment error = %v, want ErrRoutedToAnotherHub", err)
	}
	if _, err := svc.ManualBindForTenant(context.Background(), store.DefaultTenantID, "user@qianxin.com"); err == nil || !errors.Is(err, ErrRoutedToAnotherHub) {
		t.Fatalf("ManualBindForTenant error = %v, want ErrRoutedToAnotherHub", err)
	}
}

func TestIdentityServiceDoesNotResyncExistingUserWhenCenterRoutesEmailElsewhere(t *testing.T) {
	deps := newTestStore(t)
	svc := NewIdentityService(
		deps.store.Users,
		deps.store.Enrollments,
		deps.store.EmailBlocks,
		deps.store.Machines,
		deps.store.ViewerTokens,
		deps.store.LoginTokens,
		deps.store.System,
		nil,
		"open",
		true,
		nil,
		"http://127.0.0.1:9399",
	)
	now := time.Now()
	if err := deps.store.Users.Create(context.Background(), &store.User{ID: "u_existing", TenantID: store.DefaultTenantID, Email: "user@qianxin.com", SN: "SN-EXISTING", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	syncer := &testUserRouteSyncer{allowed: false, targetHubID: "hub_maclaw"}
	svc.SetUserRouteSyncer(syncer)

	if _, err := svc.AdminConfirmLoginByEmail(context.Background(), "user@qianxin.com"); err != nil {
		t.Fatalf("AdminConfirmLoginByEmail existing user: %v", err)
	}
	if syncer.syncCalls != 0 {
		t.Fatalf("existing routed-away user should not resync route, syncCalls=%d", syncer.syncCalls)
	}
}

func TestIdentityServiceSetUserRouteSyncerClearsStaleValidator(t *testing.T) {
	deps := newTestStore(t)
	svc := NewIdentityService(
		deps.store.Users,
		deps.store.Enrollments,
		deps.store.EmailBlocks,
		deps.store.Machines,
		deps.store.ViewerTokens,
		deps.store.LoginTokens,
		deps.store.System,
		nil,
		"open",
		true,
		nil,
		"http://127.0.0.1:9399",
	)
	svc.SetUserRouteSyncer(&testUserRouteSyncer{allowed: false, targetHubID: "hub_maclaw"})
	syncOnly := &testUserRouteSyncOnly{}
	svc.SetUserRouteSyncer(syncOnly)

	if _, err := svc.ManualBindForTenant(context.Background(), store.DefaultTenantID, "user@qianxin.com"); err != nil {
		t.Fatalf("ManualBindForTenant should not use stale route validator: %v", err)
	}
	if syncOnly.syncCalls != 1 {
		t.Fatalf("expected replacement syncer to sync once, got %d", syncOnly.syncCalls)
	}
}

func TestManualBindForTenantAllowsSameEmailAcrossTenants(t *testing.T) {
	deps := newTestStore(t)
	svc := NewIdentityService(
		deps.store.Users,
		deps.store.Enrollments,
		deps.store.EmailBlocks,
		deps.store.Machines,
		deps.store.ViewerTokens,
		deps.store.LoginTokens,
		deps.store.System,
		nil,
		"open",
		true,
		nil,
		"http://127.0.0.1:9399",
	)
	ctx := context.Background()

	userA, err := svc.ManualBindForTenant(ctx, "tenant_a", "shared@example.com")
	if err != nil {
		t.Fatalf("ManualBindForTenant tenant_a: %v", err)
	}
	userB, err := svc.ManualBindForTenant(ctx, "tenant_b", "shared@example.com")
	if err != nil {
		t.Fatalf("ManualBindForTenant tenant_b: %v", err)
	}
	if userA == nil || userB == nil || userA.ID == userB.ID || userA.SN == userB.SN {
		t.Fatalf("expected separate tenant users with unique SNs, got A=%+v B=%+v", userA, userB)
	}
	if userA.TenantID != "tenant_a" || userB.TenantID != "tenant_b" {
		t.Fatalf("unexpected tenant ids: A=%+v B=%+v", userA, userB)
	}
}

func TestResolveTenantByEmailUsesConfiguredTenantDomains(t *testing.T) {
	deps := newTestStore(t)
	svc := NewIdentityService(
		deps.store.Users,
		deps.store.Enrollments,
		deps.store.EmailBlocks,
		deps.store.Machines,
		deps.store.ViewerTokens,
		deps.store.LoginTokens,
		deps.store.System,
		nil,
		"open",
		true,
		nil,
		"http://127.0.0.1:9399",
	)
	svc.SetTenantRepository(deps.store.Tenants)
	now := time.Now().UTC()
	if err := deps.store.Tenants.Create(context.Background(), &store.Tenant{ID: "tenant_acme", Slug: "tenant-acme", Name: "Acme", Status: "active", PrimaryDomain: "acme.com", SettingsJSON: `{"email_domains":["acme.com","team.acme.com"]}`, CreatedByAdminID: "test", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	tenantID, found, ambiguous, err := svc.ResolveTenantByEmail(context.Background(), "new-user@team.acme.com")
	if err != nil {
		t.Fatalf("ResolveTenantByEmail: %v", err)
	}
	if tenantID != "tenant_acme" || !found || ambiguous {
		t.Fatalf("unexpected tenant route tenant=%q found=%v ambiguous=%v", tenantID, found, ambiguous)
	}
}

func TestResolveTenantByEmailIgnoresInvalidConfiguredTenantDomains(t *testing.T) {
	deps := newTestStore(t)
	svc := NewIdentityService(
		deps.store.Users,
		deps.store.Enrollments,
		deps.store.EmailBlocks,
		deps.store.Machines,
		deps.store.ViewerTokens,
		deps.store.LoginTokens,
		deps.store.System,
		nil,
		"open",
		true,
		nil,
		"http://127.0.0.1:9399",
	)
	svc.SetTenantRepository(deps.store.Tenants)
	now := time.Now().UTC()
	if err := deps.store.Tenants.Create(context.Background(), &store.Tenant{ID: "tenant_bad", Slug: "tenant-bad", Name: "Bad", Status: "active", PrimaryDomain: "https://bad.example.com", SettingsJSON: `{"email_domains":["https://bad.example.com","bad..example.com"]}`, CreatedByAdminID: "test", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	tenantID, found, ambiguous, err := svc.ResolveTenantByEmail(context.Background(), "new-user@bad.example.com")
	if err != nil {
		t.Fatalf("ResolveTenantByEmail: %v", err)
	}
	if tenantID != "" || found || ambiguous {
		t.Fatalf("expected invalid configured domains to be ignored, tenant=%q found=%v ambiguous=%v", tenantID, found, ambiguous)
	}
}

func TestResolveTenantByEmailConfiguredDomainAmbiguous(t *testing.T) {
	deps := newTestStore(t)
	svc := NewIdentityService(
		deps.store.Users,
		deps.store.Enrollments,
		deps.store.EmailBlocks,
		deps.store.Machines,
		deps.store.ViewerTokens,
		deps.store.LoginTokens,
		deps.store.System,
		nil,
		"open",
		true,
		nil,
		"http://127.0.0.1:9399",
	)
	svc.SetTenantRepository(deps.store.Tenants)
	now := time.Now().UTC()
	for _, tenant := range []*store.Tenant{
		{ID: "tenant_a", Slug: "tenant-a", Name: "Tenant A", Status: "active", PrimaryDomain: "shared.com", SettingsJSON: `{}`, CreatedByAdminID: "test", CreatedAt: now, UpdatedAt: now},
		{ID: "tenant_b", Slug: "tenant-b", Name: "Tenant B", Status: "active", PrimaryDomain: "shared.com", SettingsJSON: `{}`, CreatedByAdminID: "test", CreatedAt: now, UpdatedAt: now},
	} {
		if err := deps.store.Tenants.Create(context.Background(), tenant); err != nil {
			t.Fatalf("create tenant %s: %v", tenant.ID, err)
		}
	}

	_, found, ambiguous, err := svc.ResolveTenantByEmail(context.Background(), "new-user@shared.com")
	if err != nil {
		t.Fatalf("ResolveTenantByEmail: %v", err)
	}
	if !found || !ambiguous {
		t.Fatalf("expected ambiguous domain route, found=%v ambiguous=%v", found, ambiguous)
	}
}

func TestStartEnrollmentRejectsNewUserWhenTenantRegistrationClosed(t *testing.T) {
	deps := newTestStore(t)
	svc := NewIdentityService(
		deps.store.Users,
		deps.store.Enrollments,
		deps.store.EmailBlocks,
		deps.store.Machines,
		deps.store.ViewerTokens,
		deps.store.LoginTokens,
		deps.store.System,
		nil,
		"open",
		true,
		nil,
		"http://127.0.0.1:9399",
	)
	svc.SetTenantRepository(deps.store.Tenants)
	now := time.Now().UTC()
	if err := deps.store.Tenants.Create(context.Background(), &store.Tenant{ID: "tenant_closed", Slug: "tenant-closed", Name: "Closed", Status: "active", SettingsJSON: `{"allow_user_registration":false}`, CreatedByAdminID: "test", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	_, err := svc.StartEnrollment(WithTenant(context.Background(), "tenant_closed"), "new@example.com", "office-pc", "windows", "", "")
	if err == nil || err != ErrRegistrationDisabled {
		t.Fatalf("expected ErrRegistrationDisabled, got %v", err)
	}
}

func TestStartEnrollmentAllowsInvitedUserWhenTenantRegistrationClosed(t *testing.T) {
	deps := newTestStore(t)
	invites := &testInvitationCodeValidator{code: "INVITE-1"}
	svc := NewIdentityService(
		deps.store.Users,
		deps.store.Enrollments,
		deps.store.EmailBlocks,
		deps.store.Machines,
		deps.store.ViewerTokens,
		deps.store.LoginTokens,
		deps.store.System,
		invites,
		"open",
		true,
		nil,
		"http://127.0.0.1:9399",
	)
	svc.SetTenantRepository(deps.store.Tenants)
	now := time.Now().UTC()
	if err := deps.store.Tenants.Create(context.Background(), &store.Tenant{ID: "tenant_closed", Slug: "tenant-closed", Name: "Closed", Status: "active", SettingsJSON: `{"allow_user_registration":false}`, CreatedByAdminID: "test", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	result, err := svc.StartEnrollment(WithTenant(context.Background(), "tenant_closed"), "invited@example.com", "office-pc", "windows", "", "INVITE-1")
	if err != nil {
		t.Fatalf("StartEnrollment invited user: %v", err)
	}
	if result == nil || result.Status != "approved" || invites.consumed != 1 {
		t.Fatalf("unexpected result=%+v consumed=%d", result, invites.consumed)
	}
}

func TestClosedTenantAllowsExistingUserEnrollment(t *testing.T) {
	deps := newTestStore(t)
	svc := NewIdentityService(
		deps.store.Users,
		deps.store.Enrollments,
		deps.store.EmailBlocks,
		deps.store.Machines,
		deps.store.ViewerTokens,
		deps.store.LoginTokens,
		deps.store.System,
		nil,
		"open",
		true,
		nil,
		"http://127.0.0.1:9399",
	)
	svc.SetTenantRepository(deps.store.Tenants)
	now := time.Now().UTC()
	if err := deps.store.Tenants.Create(context.Background(), &store.Tenant{ID: "tenant_closed", Slug: "tenant-closed", Name: "Closed", Status: "active", SettingsJSON: `{"allow_user_registration":false}`, CreatedByAdminID: "test", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	if _, err := svc.ManualBindForTenant(WithTenant(context.Background(), "tenant_closed"), "tenant_closed", "existing@example.com"); err != nil {
		t.Fatalf("manual bind: %v", err)
	}

	result, err := svc.StartEnrollment(WithTenant(context.Background(), "tenant_closed"), "existing@example.com", "office-pc", "windows", "", "")
	if err != nil {
		t.Fatalf("StartEnrollment existing user: %v", err)
	}
	if result == nil || result.Status != "approved" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestRequestEmailLoginRejectsNewUserWhenDefaultTenantRegistrationClosed(t *testing.T) {
	deps := newTestStore(t)
	svc := NewIdentityService(
		deps.store.Users,
		deps.store.Enrollments,
		deps.store.EmailBlocks,
		deps.store.Machines,
		deps.store.ViewerTokens,
		deps.store.LoginTokens,
		deps.store.System,
		nil,
		"open",
		true,
		nil,
		"http://127.0.0.1:9399",
	)
	svc.SetTenantRepository(closedDefaultTenantRepo{TenantRepository: deps.store.Tenants})

	_, err := svc.RequestEmailLogin(context.Background(), "loose@example.com")
	if err == nil || err != ErrRegistrationDisabled {
		t.Fatalf("expected ErrRegistrationDisabled, got %v", err)
	}
}

func TestIdentityServiceEnrollmentAndEmailLoginCreatesDefaultLLMServiceGrant(t *testing.T) {
	deps := newTestStore(t)
	svc := NewIdentityService(
		deps.store.Users,
		deps.store.Enrollments,
		deps.store.EmailBlocks,
		deps.store.Machines,
		deps.store.ViewerTokens,
		deps.store.LoginTokens,
		deps.store.System,
		nil,
		"open",
		true,
		nil,
		"http://127.0.0.1:9399",
	)
	ctx := context.Background()
	if err := llmservice.SaveRegistry(ctx, deps.store.System, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID:   "coding-basic",
			Name: "Coding Basic",
			Models: []llmservice.ModelServiceModel{{
				Name:        "auto",
				ProviderIDs: []string{"provider-a"},
			}},
		}},
		DefaultNewUserServiceGroups: []string{"coding-basic"},
		DefaultNewUserDurationDays:  7,
	}); err != nil {
		t.Fatalf("SaveRegistry: %v", err)
	}

	if _, err := svc.RequestEmailLogin(ctx, "grantme@example.com"); err != nil {
		t.Fatalf("RequestEmailLogin: %v", err)
	}

	reg, err := llmservice.LoadRegistry(ctx, deps.store.System)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	count := 0
	for _, grant := range reg.Grants {
		if grant.Email == "grantme@example.com" && grant.ServiceGroupID == "coding-basic" && grant.Source == "new_user_default" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected 1 default LLM service grant, got %d", count)
	}
}

func TestIdentityServiceApprovalModeCreatesPendingEnrollment(t *testing.T) {
	deps := newTestStore(t)
	svc := NewIdentityService(
		deps.store.Users,
		deps.store.Enrollments,
		deps.store.EmailBlocks,
		deps.store.Machines,
		deps.store.ViewerTokens,
		deps.store.LoginTokens,
		deps.store.System,
		nil,
		"approval",
		true,
		nil,
		"http://127.0.0.1:9399",
	)

	result, err := svc.StartEnrollment(context.Background(), "pending@example.com", "office-pc", "windows", "", "")
	if err != nil {
		t.Fatalf("StartEnrollment: %v", err)
	}
	if result == nil || result.Status != "pending_approval" {
		t.Fatalf("unexpected result: %+v", result)
	}

	pending, err := deps.store.Enrollments.GetPendingByEmail(context.Background(), "pending@example.com")
	if err != nil {
		t.Fatalf("GetPendingByEmail: %v", err)
	}
	if pending == nil {
		t.Fatal("expected pending enrollment record")
	}
}

func TestIdentityServiceManualModeRequiresExistingBinding(t *testing.T) {
	deps := newTestStore(t)
	svc := NewIdentityService(
		deps.store.Users,
		deps.store.Enrollments,
		deps.store.EmailBlocks,
		deps.store.Machines,
		deps.store.ViewerTokens,
		deps.store.LoginTokens,
		deps.store.System,
		nil,
		"manual",
		true,
		nil,
		"http://127.0.0.1:9399",
	)

	result, err := svc.StartEnrollment(context.Background(), "manual-only@example.com", "office-pc", "windows", "", "")
	if err != nil {
		t.Fatalf("StartEnrollment: %v", err)
	}
	if result == nil || result.Status != "manual_binding_required" {
		t.Fatalf("unexpected result: %+v", result)
	}
}
