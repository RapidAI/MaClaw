package auth

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/invitation"
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
	allowed      bool
	targetHubID  string
	syncCalls    int
	replaceCalls int
	accounts     []string
	tenants      []string
}

type testUserRouteSyncOnly struct {
	syncCalls    int
	replaceCalls int
	accounts     []string
	tenants      []string
}

type captureIdentityMailer struct {
	to      []string
	subject string
	body    string
	err     error
}

type retryPhoneRouteSyncer struct {
	failFirstReplace bool
	replaceCalls     atomic.Int32
	synced           chan string
}

func (m *captureIdentityMailer) Send(ctx context.Context, to []string, subject string, body string) error {
	_ = ctx
	if m.err != nil {
		return m.err
	}
	m.to = append([]string(nil), to...)
	m.subject = subject
	m.body = body
	return nil
}

func (m *captureIdentityMailer) SendLoginConfirmation(ctx context.Context, to string, confirmURL string) error {
	return m.Send(ctx, []string{to}, "login", confirmURL)
}

func (m *captureIdentityMailer) SendRegistrationVerification(ctx context.Context, to string, confirmURL string, code string, lang ...string) error {
	return m.Send(ctx, []string{to}, "registration-verification", confirmURL+"\n"+code)
}

func (s *testUserRouteSyncer) SyncUserRoute(_ context.Context, account string, tenantIDOpt ...string) error {
	s.syncCalls++
	s.accounts = append(s.accounts, account)
	if len(tenantIDOpt) > 0 {
		s.tenants = append(s.tenants, tenantIDOpt[0])
	}
	return nil
}

func (s *testUserRouteSyncer) SyncUserRouteReplaceAll(_ context.Context, account string, tenantIDOpt ...string) error {
	s.replaceCalls++
	s.accounts = append(s.accounts, account)
	if len(tenantIDOpt) > 0 {
		s.tenants = append(s.tenants, tenantIDOpt[0])
	}
	return nil
}

func (s *testUserRouteSyncer) AllowsUserRoute(context.Context, string, ...string) (bool, string, error) {
	return s.allowed, s.targetHubID, nil
}

func (s *testUserRouteSyncOnly) SyncUserRoute(_ context.Context, account string, tenantIDOpt ...string) error {
	s.syncCalls++
	s.accounts = append(s.accounts, account)
	if len(tenantIDOpt) > 0 {
		s.tenants = append(s.tenants, tenantIDOpt[0])
	}
	return nil
}

func (s *testUserRouteSyncOnly) SyncUserRouteReplaceAll(_ context.Context, account string, tenantIDOpt ...string) error {
	s.replaceCalls++
	s.accounts = append(s.accounts, account)
	if len(tenantIDOpt) > 0 {
		s.tenants = append(s.tenants, tenantIDOpt[0])
	}
	return nil
}

func (s *retryPhoneRouteSyncer) SyncUserRoute(context.Context, string, ...string) error {
	return nil
}

func (s *retryPhoneRouteSyncer) SyncUserRouteReplaceAll(_ context.Context, account string, _ ...string) error {
	call := s.replaceCalls.Add(1)
	if s.failFirstReplace && call == 1 {
		return errors.New("temporary route sync failure")
	}
	if s.synced != nil {
		select {
		case s.synced <- account:
		default:
		}
	}
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

func TestIdentityServiceBindVerifiedPhoneRejectsOverlongPhone(t *testing.T) {
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
		"http://hub.local",
	)
	now := time.Now().UTC()
	user := &store.User{
		ID:               "user_phone_limit",
		TenantID:         store.DefaultTenantID,
		Email:            "buyer@example.com",
		SN:               "SN-phone-limit",
		Status:           "active",
		EnrollmentStatus: "approved",
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := deps.store.Users.Create(context.Background(), user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := svc.BindVerifiedPhoneToUser(context.Background(), user, "123456789012345678901"); !errors.Is(err, ErrInvalidEmail) {
		t.Fatalf("BindVerifiedPhoneToUser error = %v, want ErrInvalidEmail", err)
	}
	identities, err := deps.store.Users.ListIdentitiesByUser(context.Background(), store.DefaultTenantID, user.ID)
	if err != nil {
		t.Fatalf("list identities: %v", err)
	}
	for _, identity := range identities {
		if identity.Type == "phone" {
			t.Fatalf("overlong phone should not be bound: %#v", identity)
		}
	}
}

func TestIdentityServiceBindVerifiedPhoneSyncsRouteAndBackfillsLLMRegistry(t *testing.T) {
	deps := newTestStore(t)
	routeSyncer := &testUserRouteSyncOnly{}
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
		"http://hub.local",
	)
	svc.SetUserRouteSyncer(routeSyncer)
	now := time.Now().UTC()
	user := &store.User{
		ID:               "user_bind_phone",
		TenantID:         store.DefaultTenantID,
		Email:            "buyer@example.com",
		SN:               "SN-bind-phone",
		Status:           "active",
		EnrollmentStatus: "approved",
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := deps.store.Users.Create(context.Background(), user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := llmservice.SaveRegistry(context.Background(), deps.store.System, &llmservice.Registry{
		Grants: []llmservice.Grant{{ID: "grant-phone", Email: "phone:19900001111", ServiceGroupID: "coding-basic"}},
	}); err != nil {
		t.Fatalf("SaveRegistry: %v", err)
	}

	if err := svc.BindVerifiedPhoneToUser(context.Background(), user, "19900001111"); err != nil {
		t.Fatalf("BindVerifiedPhoneToUser: %v", err)
	}
	if routeSyncer.syncCalls != 0 || routeSyncer.replaceCalls != 1 || len(routeSyncer.accounts) != 1 || routeSyncer.accounts[0] != "phone:19900001111" {
		t.Fatalf("expected verified phone route replace sync, syncCalls=%d replaceCalls=%d accounts=%#v", routeSyncer.syncCalls, routeSyncer.replaceCalls, routeSyncer.accounts)
	}
	reg, err := llmservice.LoadRegistry(context.Background(), deps.store.System)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	if len(reg.Grants) != 1 || reg.Grants[0].UserID != user.ID {
		t.Fatalf("expected phone grant backfilled to user ID, grants=%#v", reg.Grants)
	}
}

func TestIdentityServiceBindVerifiedPhoneRetriesRouteSyncFailure(t *testing.T) {
	originalDelays := verifiedPhoneRouteRetryDelays
	verifiedPhoneRouteRetryDelays = []time.Duration{10 * time.Millisecond}
	defer func() { verifiedPhoneRouteRetryDelays = originalDelays }()

	deps := newTestStore(t)
	routeSyncer := &retryPhoneRouteSyncer{failFirstReplace: true, synced: make(chan string, 1)}
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
		"http://hub.local",
	)
	svc.SetUserRouteSyncer(routeSyncer)
	now := time.Now().UTC()
	user := &store.User{
		ID:               "user_bind_phone_retry",
		TenantID:         store.DefaultTenantID,
		Email:            "retry@example.com",
		SN:               "SN-bind-phone-retry",
		Status:           "active",
		EnrollmentStatus: "approved",
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := deps.store.Users.Create(context.Background(), user); err != nil {
		t.Fatalf("create user: %v", err)
	}

	if err := svc.BindVerifiedPhoneToUser(context.Background(), user, "19900002222"); err != nil {
		t.Fatalf("BindVerifiedPhoneToUser should not fail on route sync retryable error: %v", err)
	}
	select {
	case got := <-routeSyncer.synced:
		if got != "phone:19900002222" {
			t.Fatalf("retry synced account = %q, want phone:19900002222", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for phone route sync retry")
	}
	if calls := routeSyncer.replaceCalls.Load(); calls < 2 {
		t.Fatalf("replaceCalls = %d, want initial failure plus retry", calls)
	}
}

func TestIdentityServiceSyncVerifiedPhoneRoutesBackfillsExistingBindings(t *testing.T) {
	deps := newTestStore(t)
	routeSyncer := &testUserRouteSyncOnly{}
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
		"http://hub.local",
	)
	svc.SetUserRouteSyncer(routeSyncer)
	now := time.Now().UTC()
	users := []*store.User{
		{ID: "user_verified_phone", TenantID: "tenant_acme", Email: "verified@example.com", SN: "SN-verified-phone", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now},
		{ID: "user_unverified_phone", TenantID: "tenant_acme", Email: "unverified@example.com", SN: "SN-unverified-phone", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now},
	}
	for _, user := range users {
		if err := deps.store.Users.Create(context.Background(), user); err != nil {
			t.Fatalf("create user %s: %v", user.ID, err)
		}
	}
	if err := deps.store.Users.UpsertIdentity(context.Background(), &store.UserIdentity{
		ID:         "user_verified_phone_phone",
		TenantID:   "tenant_acme",
		UserID:     "user_verified_phone",
		Type:       "phone",
		Value:      "199 0000-1111",
		Verified:   true,
		VerifiedAt: &now,
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("upsert verified phone identity: %v", err)
	}
	if err := deps.store.Users.UpsertIdentity(context.Background(), &store.UserIdentity{
		ID:        "user_unverified_phone_phone",
		TenantID:  "tenant_acme",
		UserID:    "user_unverified_phone",
		Type:      "phone",
		Value:     "18800002222",
		Verified:  false,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("upsert unverified phone identity: %v", err)
	}

	count, err := svc.SyncVerifiedPhoneRoutes(context.Background())
	if err != nil {
		t.Fatalf("SyncVerifiedPhoneRoutes: %v", err)
	}
	if count != 1 || routeSyncer.replaceCalls != 1 || routeSyncer.syncCalls != 0 {
		t.Fatalf("expected one replace sync, count=%d syncCalls=%d replaceCalls=%d", count, routeSyncer.syncCalls, routeSyncer.replaceCalls)
	}
	if len(routeSyncer.accounts) != 1 || routeSyncer.accounts[0] != "phone:19900001111" {
		t.Fatalf("expected normalized verified phone route, accounts=%#v", routeSyncer.accounts)
	}
	if len(routeSyncer.tenants) != 1 || routeSyncer.tenants[0] != "tenant_acme" {
		t.Fatalf("expected tenant-scoped phone route sync, tenants=%#v", routeSyncer.tenants)
	}
}

func TestIdentityServiceSyncVerifiedPhoneRoutesForTenantScopesBackfill(t *testing.T) {
	deps := newTestStore(t)
	routeSyncer := &testUserRouteSyncOnly{}
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
		"http://hub.local",
	)
	svc.SetUserRouteSyncer(routeSyncer)
	now := time.Now().UTC()
	for _, item := range []struct {
		user  *store.User
		phone string
	}{
		{user: &store.User{ID: "user_phone_a", TenantID: "tenant_a", Email: "a@example.com", SN: "SN-a", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}, phone: "19900001111"},
		{user: &store.User{ID: "user_phone_b", TenantID: "tenant_b", Email: "b@example.com", SN: "SN-b", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}, phone: "18800002222"},
	} {
		user := item.user
		if err := deps.store.Users.Create(context.Background(), user); err != nil {
			t.Fatalf("create user %s: %v", user.ID, err)
		}
		if err := deps.store.Users.UpsertIdentity(context.Background(), &store.UserIdentity{
			ID:         user.ID + "_phone",
			TenantID:   user.TenantID,
			UserID:     user.ID,
			Type:       "phone",
			Value:      item.phone,
			Verified:   true,
			VerifiedAt: &now,
			CreatedAt:  now,
			UpdatedAt:  now,
		}); err != nil {
			t.Fatalf("upsert phone identity for %s: %v", user.ID, err)
		}
	}

	count, err := svc.SyncVerifiedPhoneRoutesForTenant(context.Background(), "tenant_b")
	if err != nil {
		t.Fatalf("SyncVerifiedPhoneRoutesForTenant: %v", err)
	}
	if count != 1 || routeSyncer.replaceCalls != 1 {
		t.Fatalf("expected one tenant-scoped replace sync, count=%d replaceCalls=%d", count, routeSyncer.replaceCalls)
	}
	if len(routeSyncer.tenants) != 1 || routeSyncer.tenants[0] != "tenant_b" {
		t.Fatalf("expected only tenant_b sync, tenants=%#v", routeSyncer.tenants)
	}
	if len(routeSyncer.accounts) != 1 || routeSyncer.accounts[0] != "phone:18800002222" {
		t.Fatalf("expected only tenant_b phone route, accounts=%#v", routeSyncer.accounts)
	}
}

func TestAuthenticateViewerFallsBackToViewerTokenTenant(t *testing.T) {
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
		"http://hub.local",
	)
	now := time.Now().UTC()
	user := &store.User{
		ID:               "user_legacy_blank_tenant",
		TenantID:         store.DefaultTenantID,
		Email:            "legacy@example.com",
		SN:               "SN-legacy-tenant",
		Status:           "active",
		EnrollmentStatus: "approved",
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := deps.store.Users.Create(context.Background(), user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := deps.provider.Write.Exec(`UPDATE users SET tenant_id = '' WHERE id = ?`, user.ID); err != nil {
		t.Fatalf("clear legacy user tenant: %v", err)
	}
	const rawViewerToken = "viewer-token-legacy-tenant"
	if err := deps.store.ViewerTokens.Create(context.Background(), &store.ViewerToken{
		ID:        "vt_legacy_tenant",
		TenantID:  "tenant-acme",
		UserID:    user.ID,
		TokenHash: hashToken(rawViewerToken),
		ExpiresAt: now.Add(time.Hour),
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("create viewer token: %v", err)
	}

	principal, err := svc.AuthenticateViewer(context.Background(), rawViewerToken)
	if err != nil {
		t.Fatalf("AuthenticateViewer: %v", err)
	}
	if principal == nil || principal.TenantID != "tenant-acme" || principal.UserID != user.ID {
		t.Fatalf("unexpected principal: %#v", principal)
	}
}

func TestIssueViewerTokenForUserFallsBackToContextTenant(t *testing.T) {
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
		"http://hub.local",
	)
	now := time.Now().UTC()
	user := &store.User{
		ID:               "user_issue_viewer_context_tenant",
		TenantID:         store.DefaultTenantID,
		Email:            "context-tenant@example.com",
		SN:               "SN-context-tenant",
		Status:           "active",
		EnrollmentStatus: "approved",
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := deps.store.Users.Create(context.Background(), user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := deps.provider.Write.Exec(`UPDATE users SET tenant_id = '' WHERE id = ?`, user.ID); err != nil {
		t.Fatalf("clear legacy user tenant: %v", err)
	}

	rawViewerToken, err := svc.IssueViewerTokenForUser(WithTenant(context.Background(), "tenant-acme"), user.ID)
	if err != nil {
		t.Fatalf("IssueViewerTokenForUser: %v", err)
	}
	principal, err := svc.AuthenticateViewer(context.Background(), rawViewerToken)
	if err != nil {
		t.Fatalf("AuthenticateViewer: %v", err)
	}
	if principal == nil || principal.TenantID != "tenant-acme" || principal.UserID != user.ID {
		t.Fatalf("unexpected principal: %#v", principal)
	}
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

func TestIdentityServiceRegistrationVerificationIsIndependentFromLogin(t *testing.T) {
	deps := newTestStore(t)
	mailer := &captureIdentityMailer{}
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
		mailer,
		"http://127.0.0.1:9399",
	)
	ctx := context.Background()

	enroll, err := svc.StartEnrollment(ctx, "verify@example.com", "office-pc", "windows", "", "")
	if err != nil {
		t.Fatalf("StartEnrollment: %v", err)
	}
	if enroll == nil || enroll.Status != "approved" {
		t.Fatalf("unexpected enrollment result: %+v", enroll)
	}
	if mailer.subject != "registration-verification" {
		t.Fatalf("expected registration verification mail, got subject=%q body=%q", mailer.subject, mailer.body)
	}
	parts := strings.Split(strings.TrimSpace(mailer.body), "\n")
	if len(parts) != 2 {
		t.Fatalf("expected confirm URL and code in mail body, got %q", mailer.body)
	}
	parsedURL, err := url.Parse(parts[0])
	if err != nil {
		t.Fatalf("parse verify URL: %v", err)
	}
	rawToken := parsedURL.Query().Get("token")
	if rawToken == "" {
		t.Fatalf("missing token in verify URL: %s", parts[0])
	}
	if len(parts[1]) != 6 {
		t.Fatalf("expected six digit verification code, got %q", parts[1])
	}

	if _, _, err := svc.ConfirmEmailLogin(ctx, rawToken); !errors.Is(err, ErrInvalidUserCredentials) {
		t.Fatalf("registration verification token must not sign in, got err=%v", err)
	}
	code, user, err := svc.ConfirmRegistrationVerification(ctx, rawToken)
	if err != nil {
		t.Fatalf("ConfirmRegistrationVerification: %v", err)
	}
	if code != parts[1] {
		t.Fatalf("verification code mismatch: mail=%q page=%q", parts[1], code)
	}
	if user == nil || user.Email != "verify@example.com" {
		t.Fatalf("unexpected verified user: %+v", user)
	}
}

func TestRequestEmailLoginForNewUserSendsRegistrationVerification(t *testing.T) {
	deps := newTestStore(t)
	mailer := &captureIdentityMailer{}
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
		mailer,
		"http://127.0.0.1:9399",
	)
	ctx := context.Background()

	result, err := svc.RequestEmailLogin(ctx, "fresh@example.com")
	if err != nil {
		t.Fatalf("RequestEmailLogin: %v", err)
	}
	if result == nil || result.Status != "pending_email_confirmation" || result.SentTo != "email" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.PollID != "" {
		t.Fatalf("registration verification should not return login poll_id, got %q", result.PollID)
	}
	if mailer.subject != "registration-verification" {
		t.Fatalf("expected registration verification mail, got subject=%q body=%q", mailer.subject, mailer.body)
	}
	parts := strings.Split(strings.TrimSpace(mailer.body), "\n")
	if len(parts) != 2 {
		t.Fatalf("expected confirm URL and code in mail body, got %q", mailer.body)
	}
	parsedURL, err := url.Parse(parts[0])
	if err != nil {
		t.Fatalf("parse verify URL: %v", err)
	}
	rawToken := parsedURL.Query().Get("token")
	if rawToken == "" {
		t.Fatalf("missing token in verify URL: %s", parts[0])
	}
	if _, _, err := svc.ConfirmEmailLogin(ctx, rawToken); !errors.Is(err, ErrInvalidUserCredentials) {
		t.Fatalf("new-user registration token must not sign in, got err=%v", err)
	}
	poll, err := svc.PollEmailLogin(ctx, "")
	if err != nil {
		t.Fatalf("PollEmailLogin empty registration poll: %v", err)
	}
	if poll == nil || poll.Status != "invalid" {
		t.Fatalf("registration verification should not be poll-loginable, got %+v", poll)
	}
}

func TestRequestEmailLoginUnverifiedExistingUserSendsRegistrationVerification(t *testing.T) {
	deps := newTestStore(t)
	mailer := &captureIdentityMailer{}
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
		mailer,
		"http://127.0.0.1:9399",
	)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := deps.store.Users.Create(ctx, &store.User{
		ID:               "user_unverified_existing",
		TenantID:         store.DefaultTenantID,
		Email:            "unverified@example.com",
		SN:               "SN-UNVERIFIED",
		Status:           "active",
		EnrollmentStatus: "approved",
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}

	result, err := svc.RequestEmailLogin(ctx, "unverified@example.com")
	if err != nil {
		t.Fatalf("RequestEmailLogin: %v", err)
	}
	if result == nil || result.PollID != "" || result.SentTo != "email" {
		t.Fatalf("expected registration verification without login poll id, got %+v", result)
	}
	if mailer.subject != "registration-verification" {
		t.Fatalf("expected registration verification mail, got subject=%q body=%q", mailer.subject, mailer.body)
	}
}

func TestRequestEmailLoginRegistrationVerificationInvalidatesOlderLinks(t *testing.T) {
	deps := newTestStore(t)
	mailer := &captureIdentityMailer{}
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
		mailer,
		"http://127.0.0.1:9399",
	)
	ctx := context.Background()

	if _, err := svc.RequestEmailLogin(ctx, "repeat-verify@example.com"); err != nil {
		t.Fatalf("first RequestEmailLogin: %v", err)
	}
	firstToken := extractRegistrationVerificationToken(t, mailer.body)
	if _, err := svc.RequestEmailLogin(ctx, "repeat-verify@example.com"); err != nil {
		t.Fatalf("second RequestEmailLogin: %v", err)
	}
	secondToken := extractRegistrationVerificationToken(t, mailer.body)
	if firstToken == secondToken {
		t.Fatal("expected refreshed registration verification token")
	}

	if _, _, err := svc.ConfirmRegistrationVerification(ctx, firstToken); !errors.Is(err, ErrInvalidUserCredentials) {
		t.Fatalf("old registration verification token should be invalid, got err=%v", err)
	}
	if _, _, err := svc.ConfirmRegistrationVerification(ctx, secondToken); err != nil {
		t.Fatalf("latest registration verification token should confirm: %v", err)
	}
}

func TestRequestEmailLoginRegistrationVerificationSendFailureConsumesToken(t *testing.T) {
	deps := newTestStore(t)
	mailer := &captureIdentityMailer{err: errors.New("smtp failed")}
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
		mailer,
		"http://127.0.0.1:9399",
	)
	ctx := context.Background()

	if _, err := svc.RequestEmailLogin(ctx, "smtp-fail@example.com"); err == nil {
		t.Fatal("expected registration verification send failure")
	}
	tokens, err := deps.store.LoginTokens.ListPendingByTenant(ctx, store.DefaultTenantID)
	if err != nil {
		t.Fatalf("list pending tokens: %v", err)
	}
	for _, token := range tokens {
		if token != nil && token.Email == "smtp-fail@example.com" && token.Purpose == loginTokenPurposeVerifyEmail {
			t.Fatalf("send failure left pending registration token: %+v", token)
		}
	}
}

func TestRequestEmailLoginRegistrationVerificationResendFailureKeepsOlderLink(t *testing.T) {
	deps := newTestStore(t)
	mailer := &captureIdentityMailer{}
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
		mailer,
		"http://127.0.0.1:9399",
	)
	ctx := context.Background()

	if _, err := svc.RequestEmailLogin(ctx, "resend-fail@example.com"); err != nil {
		t.Fatalf("first RequestEmailLogin: %v", err)
	}
	firstToken := extractRegistrationVerificationToken(t, mailer.body)
	mailer.err = errors.New("smtp failed")
	if _, err := svc.RequestEmailLogin(ctx, "resend-fail@example.com"); err == nil {
		t.Fatal("expected resend failure")
	}
	if _, _, err := svc.ConfirmRegistrationVerification(ctx, firstToken); err != nil {
		t.Fatalf("older delivered token should remain valid after resend failure: %v", err)
	}
}

func TestRequestEmailLoginVerifiedUserIgnoresStaleRegistrationVerificationToken(t *testing.T) {
	deps := newTestStore(t)
	mailer := &captureIdentityMailer{}
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
		mailer,
		"http://127.0.0.1:9399",
	)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := deps.store.Users.Create(ctx, &store.User{
		ID:               "user_verified_stale_verify",
		TenantID:         store.DefaultTenantID,
		Email:            "verified@example.com",
		SN:               "SN-VERIFIED",
		Status:           "active",
		EnrollmentStatus: "approved",
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := deps.store.Users.MarkEmailVerified(ctx, store.DefaultTenantID, "verified@example.com"); err != nil {
		t.Fatalf("mark email verified: %v", err)
	}
	if err := deps.store.LoginTokens.Create(ctx, &store.LoginToken{
		ID:            "lt_stale_registration_verify",
		TenantID:      store.DefaultTenantID,
		Email:         "verified@example.com",
		TokenHash:     hashToken("stale-registration-token"),
		PollTokenHash: hashToken("stale-registration-poll"),
		Purpose:       loginTokenPurposeVerifyEmail,
		ExpiresAt:     now.Add(time.Hour),
		CreatedAt:     now,
	}); err != nil {
		t.Fatalf("create stale token: %v", err)
	}

	result, err := svc.RequestEmailLogin(ctx, "verified@example.com")
	if err != nil {
		t.Fatalf("RequestEmailLogin: %v", err)
	}
	if result == nil || result.PollID == "" {
		t.Fatalf("expected normal login poll id, got %+v", result)
	}
	if mailer.subject != "login" {
		t.Fatalf("expected login mail, got subject=%q body=%q", mailer.subject, mailer.body)
	}
}

func TestIdentityServiceEmailLoginIncludesMobileHubContext(t *testing.T) {
	deps := newTestStore(t)
	if err := deps.store.System.Set(context.Background(), systemKeyCenterRegistration, `{"hub_id":"hub_mobile"}`); err != nil {
		t.Fatalf("seed center registration: %v", err)
	}
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
		"https://tenant-a.maclaw.top",
	)
	ctx := context.Background()
	if _, err := svc.StartEnrollment(ctx, "mobile@example.com", "phone", "ios", "", ""); err != nil {
		t.Fatalf("StartEnrollment: %v", err)
	}

	req, err := svc.RequestEmailLogin(ctx, "mobile@example.com")
	if err != nil {
		t.Fatalf("RequestEmailLogin: %v", err)
	}
	if req.HubURL != "https://tenant-a.maclaw.top" || req.Hub == nil || req.Hub.ID != "hub_mobile" {
		t.Fatalf("request mobile hub context = %+v", req)
	}
	if req.LLM == nil || req.LLM.Mode != defaultMobileOfficialLLMMode {
		t.Fatalf("request llm context = %+v", req.LLM)
	}

	rawToken := extractIdentityDevConfirmToken(t, req.Message)
	if _, _, err := svc.ConfirmEmailLogin(ctx, rawToken); err != nil {
		t.Fatalf("ConfirmEmailLogin: %v", err)
	}
	poll, err := svc.PollEmailLogin(ctx, req.PollID)
	if err != nil {
		t.Fatalf("PollEmailLogin: %v", err)
	}
	if poll.Status != "confirmed" || poll.AccessToken == "" {
		t.Fatalf("unexpected poll result: %+v", poll)
	}
	if poll.HubURL != "https://tenant-a.maclaw.top" || poll.Hub == nil || poll.Hub.ID != "hub_mobile" {
		t.Fatalf("poll mobile hub context = %+v", poll)
	}
	if poll.User == nil || poll.User.TenantID != store.DefaultTenantID || poll.User.Email != "mobile@example.com" {
		t.Fatalf("poll user context = %+v", poll.User)
	}
	if poll.LLM == nil || poll.LLM.Mode != defaultMobileOfficialLLMMode {
		t.Fatalf("poll llm context = %+v", poll.LLM)
	}
}

func extractIdentityDevConfirmToken(t *testing.T, message string) string {
	t.Helper()
	const prefix = "Use this confirm URL for development: "
	if len(message) <= len(prefix) || message[:len(prefix)] != prefix {
		t.Fatalf("unexpected confirm message: %q", message)
	}
	parsedURL, err := url.Parse(message[len(prefix):])
	if err != nil {
		t.Fatalf("parse confirm URL: %v", err)
	}
	rawToken := parsedURL.Query().Get("token")
	if rawToken == "" {
		t.Fatalf("missing token in confirm URL: %s", message)
	}
	return rawToken
}

func extractRegistrationVerificationToken(t *testing.T, body string) string {
	t.Helper()
	parts := strings.Split(strings.TrimSpace(body), "\n")
	if len(parts) < 1 {
		t.Fatalf("missing verification URL in body: %q", body)
	}
	parsedURL, err := url.Parse(parts[0])
	if err != nil {
		t.Fatalf("parse verification URL: %v", err)
	}
	rawToken := parsedURL.Query().Get("token")
	if rawToken == "" {
		t.Fatalf("missing token in verification URL: %s", parts[0])
	}
	return rawToken
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

func TestResolveTenantByEmailPrefersUnrestrictedLegacyUserOverDomainMismatchDuplicate(t *testing.T) {
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
	if err := deps.store.Tenants.Create(context.Background(), &store.Tenant{
		ID:               "tenant_qianxin",
		Slug:             "tenant-qianxin",
		Name:             "Qianxin",
		Status:           "active",
		PrimaryDomain:    "qianxin.com",
		SettingsJSON:     `{"email_domains":["qianxin.com"],"allow_user_registration":true}`,
		CreatedByAdminID: "test",
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	for _, user := range []*store.User{
		{ID: "u_legacy_default", TenantID: store.DefaultTenantID, Email: "znsoft@163.com", SN: "SN-LEGACY", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now},
		{ID: "u_wrong_qianxin", TenantID: "tenant_qianxin", Email: "znsoft@163.com", SN: "SN-WRONG", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now},
	} {
		if err := deps.store.Users.Create(context.Background(), user); err != nil {
			t.Fatalf("create user %s: %v", user.ID, err)
		}
	}

	tenantID, found, ambiguous, err := svc.ResolveTenantByEmail(context.Background(), "znsoft@163.com")
	if err != nil {
		t.Fatalf("ResolveTenantByEmail: %v", err)
	}
	if tenantID != store.DefaultTenantID || !found || ambiguous {
		t.Fatalf("expected default legacy user route, tenant=%q found=%v ambiguous=%v", tenantID, found, ambiguous)
	}
}

func TestResolveTenantByEmailKeepsAmbiguousExistingUsersWhenDomainsDoNotDisambiguate(t *testing.T) {
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
		{ID: "tenant_a", Slug: "tenant-a", Name: "Tenant A", Status: "active", SettingsJSON: `{}`, CreatedByAdminID: "test", CreatedAt: now, UpdatedAt: now},
		{ID: "tenant_b", Slug: "tenant-b", Name: "Tenant B", Status: "active", SettingsJSON: `{}`, CreatedByAdminID: "test", CreatedAt: now, UpdatedAt: now},
	} {
		if err := deps.store.Tenants.Create(context.Background(), tenant); err != nil {
			t.Fatalf("create tenant %s: %v", tenant.ID, err)
		}
	}
	for _, user := range []*store.User{
		{ID: "u_a", TenantID: "tenant_a", Email: "shared@example.com", SN: "SN-A", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now},
		{ID: "u_b", TenantID: "tenant_b", Email: "shared@example.com", SN: "SN-B", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now},
	} {
		if err := deps.store.Users.Create(context.Background(), user); err != nil {
			t.Fatalf("create user %s: %v", user.ID, err)
		}
	}

	_, found, ambiguous, err := svc.ResolveTenantByEmail(context.Background(), "shared@example.com")
	if err != nil {
		t.Fatalf("ResolveTenantByEmail: %v", err)
	}
	if !found || !ambiguous {
		t.Fatalf("expected ambiguous unrestricted duplicate, found=%v ambiguous=%v", found, ambiguous)
	}
}

func TestResolveTenantByEmailKeepsAmbiguousExistingUsersWhenMultipleDomainsMatch(t *testing.T) {
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
		{ID: "tenant_a", Slug: "tenant-a", Name: "Tenant A", Status: "active", PrimaryDomain: "example.com", SettingsJSON: `{}`, CreatedByAdminID: "test", CreatedAt: now, UpdatedAt: now},
		{ID: "tenant_b", Slug: "tenant-b", Name: "Tenant B", Status: "active", PrimaryDomain: "example.com", SettingsJSON: `{}`, CreatedByAdminID: "test", CreatedAt: now, UpdatedAt: now},
	} {
		if err := deps.store.Tenants.Create(context.Background(), tenant); err != nil {
			t.Fatalf("create tenant %s: %v", tenant.ID, err)
		}
	}
	for _, user := range []*store.User{
		{ID: "u_a", TenantID: "tenant_a", Email: "shared@example.com", SN: "SN-A", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now},
		{ID: "u_b", TenantID: "tenant_b", Email: "shared@example.com", SN: "SN-B", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now},
	} {
		if err := deps.store.Users.Create(context.Background(), user); err != nil {
			t.Fatalf("create user %s: %v", user.ID, err)
		}
	}

	_, found, ambiguous, err := svc.ResolveTenantByEmail(context.Background(), "shared@example.com")
	if err != nil {
		t.Fatalf("ResolveTenantByEmail: %v", err)
	}
	if !found || !ambiguous {
		t.Fatalf("expected ambiguous duplicate exact matches, found=%v ambiguous=%v", found, ambiguous)
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

func TestRequestEmailLoginAllowsExistingUserWhenTenantRegistrationClosed(t *testing.T) {
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

	result, err := svc.RequestEmailLogin(WithTenant(context.Background(), "tenant_closed"), "existing@example.com")
	if err != nil {
		t.Fatalf("RequestEmailLogin existing user: %v", err)
	}
	if result == nil || result.Status != "pending_email_confirmation" || result.TenantID != "tenant_closed" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestIdentityServiceRejectsNewEmailUserOutsideTenantDomains(t *testing.T) {
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
	if err := deps.store.Tenants.Create(context.Background(), &store.Tenant{
		ID:               "tenant_qianxin",
		Slug:             "tenant-qianxin",
		Name:             "Qianxin",
		Status:           "active",
		PrimaryDomain:    "qianxin.com",
		SettingsJSON:     `{"email_domains":["qianxin.com"],"allow_user_registration":true}`,
		CreatedByAdminID: "test",
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	ctx := WithTenant(context.Background(), "tenant_qianxin")

	if _, err := svc.RequestEmailLogin(ctx, "znsoft@163.com"); !errors.Is(err, ErrEmailDomainNotAllowed) {
		t.Fatalf("RequestEmailLogin error = %v, want ErrEmailDomainNotAllowed", err)
	}
	if _, err := svc.StartEnrollment(ctx, "znsoft@163.com", "office-pc", "windows", "", ""); !errors.Is(err, ErrEmailDomainNotAllowed) {
		t.Fatalf("StartEnrollment error = %v, want ErrEmailDomainNotAllowed", err)
	}
	if _, err := svc.ManualBindForTenant(ctx, "tenant_qianxin", "znsoft@163.com"); !errors.Is(err, ErrEmailDomainNotAllowed) {
		t.Fatalf("ManualBindForTenant error = %v, want ErrEmailDomainNotAllowed", err)
	}
	if user, err := deps.store.Users.GetByTenantEmail(ctx, "tenant_qianxin", "znsoft@163.com"); err != nil || user != nil {
		t.Fatalf("unexpected user after rejected registration: user=%+v err=%v", user, err)
	}
	if _, err := svc.RequestEmailLogin(ctx, "user@qianxin.com"); err != nil {
		t.Fatalf("RequestEmailLogin allowed domain: %v", err)
	}
	if _, err := svc.StartEnrollment(ctx, "phone:19900001111", "phone", "android", "", ""); err != nil {
		t.Fatalf("StartEnrollment phone identity should skip email domain check: %v", err)
	}
}

func TestIdentityServiceAllowsExistingEmailUserOutsideTenantDomains(t *testing.T) {
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
	if err := deps.store.Tenants.Create(context.Background(), &store.Tenant{
		ID:               "tenant_qianxin",
		Slug:             "tenant-qianxin",
		Name:             "Qianxin",
		Status:           "active",
		PrimaryDomain:    "qianxin.com",
		SettingsJSON:     `{"email_domains":["qianxin.com"],"allow_user_registration":true}`,
		CreatedByAdminID: "test",
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	if err := deps.store.Users.Create(context.Background(), &store.User{
		ID:               "u_legacy_163",
		TenantID:         "tenant_qianxin",
		Email:            "znsoft@163.com",
		SN:               "SN-LEGACY",
		Status:           "active",
		EnrollmentStatus: "approved",
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		t.Fatalf("create legacy user: %v", err)
	}
	ctx := WithTenant(context.Background(), "tenant_qianxin")

	if _, err := svc.RequestEmailLogin(ctx, "znsoft@163.com"); err != nil {
		t.Fatalf("RequestEmailLogin existing out-of-domain user: %v", err)
	}
	if _, err := svc.StartEnrollment(ctx, "znsoft@163.com", "office-pc", "windows", "", ""); err != nil {
		t.Fatalf("StartEnrollment existing out-of-domain user: %v", err)
	}
	if _, err := svc.ManualBindForTenant(ctx, "tenant_qianxin", "znsoft@163.com"); err != nil {
		t.Fatalf("ManualBindForTenant existing out-of-domain user: %v", err)
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

func TestStartEnrollmentGrantsInvitationLLMBenefitForNewSelfEnrollUser(t *testing.T) {
	deps := newTestStore(t)
	ctx := context.Background()
	if err := llmservice.SaveRegistry(ctx, deps.store.System, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "invite-pro", Name: "Invite Pro"}},
	}); err != nil {
		t.Fatalf("SaveRegistry: %v", err)
	}
	invitationSvc := invitation.NewService(deps.store.InvitationCodes, deps.store.System)
	svc := NewIdentityService(
		deps.store.Users,
		deps.store.Enrollments,
		deps.store.EmailBlocks,
		deps.store.Machines,
		deps.store.ViewerTokens,
		deps.store.LoginTokens,
		deps.store.System,
		invitationSvc,
		"open",
		true,
		nil,
		"http://127.0.0.1:9399",
	)
	codes, err := invitationSvc.GenerateCodesForTenantWithOptions(ctx, store.DefaultTenantID, invitation.GenerateCodeOptions{
		Count:                1,
		LLMServiceGroupID:    "invite-pro",
		LLMGrantDurationDays: 5,
		LLMGrantCredits:      88.125,
	})
	if err != nil {
		t.Fatalf("GenerateCodesForTenantWithOptions: %v", err)
	}

	result, err := svc.StartEnrollment(ctx, "new-invite@example.com", "office-pc", "windows", "", codes[0].Code)
	if err != nil {
		t.Fatalf("StartEnrollment: %v", err)
	}
	if result == nil || result.Status != "approved" {
		t.Fatalf("unexpected enrollment result: %+v", result)
	}

	reg, err := llmservice.LoadRegistry(ctx, deps.store.System)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	count := 0
	for _, grant := range reg.Grants {
		if grant.Email == "new-invite@example.com" && grant.ServiceGroupID == "invite-pro" && grant.Source == "invitation_code" {
			count++
			if grant.CardID != codes[0].ID {
				t.Fatalf("CardID = %q, want %q", grant.CardID, codes[0].ID)
			}
			if grant.CreditsTotal != 88.125 {
				t.Fatalf("CreditsTotal = %v, want 88.125", grant.CreditsTotal)
			}
			if got := grant.ExpiresAt.Sub(grant.StartsAt); got != 5*24*time.Hour {
				t.Fatalf("duration = %v, want 5 days", got)
			}
		}
	}
	if count != 1 {
		t.Fatalf("expected 1 invitation LLM grant, got %d", count)
	}
}

func TestApproveEnrollmentGrantsInvitationLLMBenefitForExistingUser(t *testing.T) {
	deps := newTestStore(t)
	ctx := context.Background()
	if err := llmservice.SaveRegistry(ctx, deps.store.System, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "invite-pro", Name: "Invite Pro"}},
	}); err != nil {
		t.Fatalf("SaveRegistry: %v", err)
	}
	invitationSvc := invitation.NewService(deps.store.InvitationCodes, deps.store.System)
	svc := NewIdentityService(
		deps.store.Users,
		deps.store.Enrollments,
		deps.store.EmailBlocks,
		deps.store.Machines,
		deps.store.ViewerTokens,
		deps.store.LoginTokens,
		deps.store.System,
		invitationSvc,
		"approval",
		true,
		nil,
		"http://127.0.0.1:9399",
	)
	now := time.Now().UTC()
	if err := deps.store.Users.Create(ctx, &store.User{
		ID:               "u_existing_invite",
		TenantID:         store.DefaultTenantID,
		Email:            "existing-invite@example.com",
		SN:               "SN-EXISTING",
		Status:           "active",
		EnrollmentStatus: "approved",
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	codes, err := invitationSvc.GenerateCodesForTenantWithOptions(ctx, store.DefaultTenantID, invitation.GenerateCodeOptions{
		Count:                1,
		LLMServiceGroupID:    "invite-pro",
		LLMGrantDurationDays: 9,
		LLMGrantCredits:      456.789,
	})
	if err != nil {
		t.Fatalf("GenerateCodesForTenantWithOptions: %v", err)
	}
	if err := invitationSvc.ValidateAndConsumeForTenant(ctx, store.DefaultTenantID, codes[0].Code, "existing-invite@example.com"); err != nil {
		t.Fatalf("ValidateAndConsumeForTenant: %v", err)
	}
	if err := deps.store.Enrollments.Create(ctx, &store.UserEnrollment{
		ID:        "enroll_existing_invite",
		TenantID:  store.DefaultTenantID,
		Email:     "existing-invite@example.com",
		Status:    "pending",
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed enrollment: %v", err)
	}

	if _, _, err := svc.ApproveEnrollment(ctx, "enroll_existing_invite"); err != nil {
		t.Fatalf("ApproveEnrollment: %v", err)
	}

	reg, err := llmservice.LoadRegistry(ctx, deps.store.System)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	count := 0
	for _, grant := range reg.Grants {
		if grant.Email == "existing-invite@example.com" && grant.ServiceGroupID == "invite-pro" && grant.Source == "invitation_code" {
			count++
			if grant.CardID != codes[0].ID {
				t.Fatalf("CardID = %q, want %q", grant.CardID, codes[0].ID)
			}
			if grant.CreditsTotal != 456.789 {
				t.Fatalf("CreditsTotal = %v, want 456.789", grant.CreditsTotal)
			}
			if got := grant.ExpiresAt.Sub(grant.StartsAt); got != 9*24*time.Hour {
				t.Fatalf("duration = %v, want 9 days", got)
			}
		}
	}
	if count != 1 {
		t.Fatalf("expected 1 invitation LLM grant, got %d", count)
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
