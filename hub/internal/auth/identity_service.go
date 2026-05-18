package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/mail"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

var (
	ErrInvalidEmail           = errors.New("invalid email")
	ErrEmailBlocked           = errors.New("email blocked")
	ErrInvalidUserCredentials = errors.New("invalid user credentials")
	ErrMachineUnauthorized    = errors.New("machine unauthorized")
	ErrInvitationCodeRequired = errors.New("invitation code is required")
	ErrInvalidInvitationCode  = errors.New("invalid or used invitation code")
	ErrInvitationExpired      = errors.New("invitation code has expired")
)

// InvitationCodeValidator abstracts the invitation code service to avoid circular imports.
type InvitationCodeValidator interface {
	IsRequired(ctx context.Context) (bool, error)
	ValidateAndConsume(ctx context.Context, code string, email string) error
	ValidateAndConsumeForTenant(ctx context.Context, tenantID string, code string, email string) error
	CheckExpiry(ctx context.Context, email string) (bool, *time.Time, error)
	CheckExpiryForTenant(ctx context.Context, tenantID string, email string) (bool, *time.Time, error)
}

// LoginNotifier sends login confirmation links to bound IM channels.
type LoginNotifier interface {
	BroadcastLoginLink(ctx context.Context, email, confirmURL string) []string
	BroadcastLoginLinkForTenant(ctx context.Context, tenantID, email, confirmURL string) []string
}

// UserRouteSyncer pushes confirmed user-to-hub bindings to Hub Center when available.
type UserRouteSyncer interface {
	SyncUserRoute(ctx context.Context, email string, tenantIDOpt ...string) error
}

const (
	systemKeyEnrollmentMode = "identity_enrollment_mode"
	systemKeyPublicBaseURL  = "server_public_base_url"
)

type EnrollmentResult struct {
	Status       string `json:"status"`
	Message      string `json:"message,omitempty"`
	UserID       string `json:"user_id,omitempty"`
	Email        string `json:"email,omitempty"`
	SN           string `json:"sn,omitempty"`
	MachineID    string `json:"machine_id,omitempty"`
	MachineToken string `json:"machine_token,omitempty"`
	ViewerToken  string `json:"viewer_token,omitempty"`
	ExpiresAt    string `json:"expires_at,omitempty"`
}

type EmailLoginRequestResult struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	PollID  string `json:"poll_id,omitempty"`
	SentTo  string `json:"sent_to,omitempty"`
}

type EmailPollResult struct {
	Status      string `json:"status"`
	AccessToken string `json:"access_token,omitempty"`
	ExpiresIn   int    `json:"expires_in,omitempty"`
	Email       string `json:"email,omitempty"`
	SN          string `json:"sn,omitempty"`
}

type SystemSettingsRepository interface {
	Set(ctx context.Context, key, valueJSON string) error
	Get(ctx context.Context, key string) (string, error)
}

type tenantContextKey struct{}

func WithTenant(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantContextKey{}, normalizeTenantIDValue(tenantID))
}

func TenantIDFromContext(ctx context.Context) string {
	return tenantIDFromContext(ctx)
}

func tenantIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return store.DefaultTenantID
	}
	if tenantID, ok := ctx.Value(tenantContextKey{}).(string); ok {
		return normalizeTenantIDValue(tenantID)
	}
	return store.DefaultTenantID
}

func normalizeTenantIDValue(tenantID string) string {
	return store.NormalizeTenantID(tenantID)
}

type MachinePrincipal struct {
	TenantID  string
	UserID    string
	MachineID string
}

type MachineMetadata struct {
	Name                 string
	Platform             string
	Hostname             string
	Arch                 string
	AppVersion           string
	HeartbeatIntervalSec int
}

type ViewerPrincipal struct {
	TenantID string
	UserID   string
	Email    string
}

type IdentityService struct {
	users           store.UserRepository
	enrollments     store.EnrollmentRepository
	blocks          store.EmailBlocklistRepository
	machines        store.MachineRepository
	viewerTok       store.ViewerTokenRepository
	loginTok        store.LoginTokenRepository
	settings        SystemSettingsRepository
	invitationSvc   InvitationCodeValidator
	enrollmentMode  string
	allowSelfEnroll bool
	mailer          mail.Mailer
	publicBaseURL   string
	loginNotifier   LoginNotifier
	userRouteSyncer UserRouteSyncer
}

func (s *IdentityService) UsersRepo() store.UserRepository {
	if s == nil {
		return nil
	}
	return s.users
}

func (s *IdentityService) UpdateMachineMetadata(ctx context.Context, machineID string, metadata MachineMetadata) error {
	if s == nil || s.machines == nil || strings.TrimSpace(machineID) == "" {
		return nil
	}
	return s.machines.UpdateMetadata(ctx, machineID, store.MachineMetadata{
		Name:                 defaultIfEmpty(metadata.Name, "MaClaw Desktop"),
		Platform:             defaultIfEmpty(metadata.Platform, "unknown"),
		Hostname:             strings.TrimSpace(metadata.Hostname),
		Arch:                 strings.TrimSpace(metadata.Arch),
		AppVersion:           strings.TrimSpace(metadata.AppVersion),
		HeartbeatIntervalSec: metadata.HeartbeatIntervalSec,
	})
}

func NewIdentityService(
	users store.UserRepository,
	enrollments store.EnrollmentRepository,
	blocks store.EmailBlocklistRepository,
	machines store.MachineRepository,
	viewerTok store.ViewerTokenRepository,
	loginTok store.LoginTokenRepository,
	settings SystemSettingsRepository,
	invitationSvc InvitationCodeValidator,
	enrollmentMode string,
	allowSelfEnroll bool,
	mailer mail.Mailer,
	publicBaseURL string,
) *IdentityService {
	return &IdentityService{
		users:           users,
		enrollments:     enrollments,
		blocks:          blocks,
		machines:        machines,
		viewerTok:       viewerTok,
		loginTok:        loginTok,
		settings:        settings,
		invitationSvc:   invitationSvc,
		enrollmentMode:  normalizeEnrollmentMode(enrollmentMode),
		allowSelfEnroll: allowSelfEnroll,
		mailer:          mailer,
		publicBaseURL:   strings.TrimRight(publicBaseURL, "/"),
	}
}

// SetLoginNotifier wires the cross-IM login link broadcaster.
// Called from bootstrap after the IM Adapter is fully assembled.
func (s *IdentityService) SetLoginNotifier(n LoginNotifier) {
	s.loginNotifier = n
}

func (s *IdentityService) SetUserRouteSyncer(syncer UserRouteSyncer) {
	s.userRouteSyncer = syncer
}

func (s *IdentityService) syncUserRoute(ctx context.Context, email string) {
	if s == nil || s.userRouteSyncer == nil || strings.TrimSpace(email) == "" {
		return
	}
	_ = s.userRouteSyncer.SyncUserRoute(ctx, email, tenantIDFromContext(ctx))
}

func (s *IdentityService) StartEnrollment(ctx context.Context, email, machineName, platform, clientID, invitationCode string) (*EnrollmentResult, error) {
	email = normalizeEmail(email)
	tenantID := tenantIDFromContext(ctx)
	if email == "" {
		return nil, ErrInvalidEmail
	}
	if err := s.ensureEmailAllowed(ctx, email); err != nil {
		return nil, err
	}

	user, err := s.users.GetByTenantEmail(ctx, tenantID, email)
	if err != nil {
		return nil, err
	}

	// Expiry check for existing users
	if user != nil && s.invitationSvc != nil {
		expired, expiresAt, _ := s.invitationSvc.CheckExpiryForTenant(ctx, tenantID, email)
		if expired {
			if strings.TrimSpace(invitationCode) != "" {
				// Expired user provided a new invitation code - rebind
				if err := s.invitationSvc.ValidateAndConsumeForTenant(ctx, tenantID, invitationCode, email); err != nil {
					return nil, ErrInvalidInvitationCode
				}
				// Continue normal enrollment flow
			} else {
				// Expired and no new code provided
				result := &EnrollmentResult{
					Status:  "invitation_expired",
					Message: "invitation code has expired",
					Email:   email,
				}
				if expiresAt != nil {
					result.ExpiresAt = expiresAt.Format(time.RFC3339)
				}
				return result, ErrInvitationExpired
			}
		}
	}

	// Invitation code validation - only required for new users
	if user == nil && s.invitationSvc != nil {
		required, err := s.invitationSvc.IsRequired(ctx)
		if err != nil {
			return nil, err
		}
		if required {
			if strings.TrimSpace(invitationCode) == "" {
				return nil, ErrInvitationCodeRequired
			}
			if err := s.invitationSvc.ValidateAndConsumeForTenant(ctx, tenantID, invitationCode, email); err != nil {
				return nil, ErrInvalidInvitationCode
			}
		}
	}

	if user == nil {
		mode, err := s.enrollmentModeValue(ctx)
		if err != nil {
			return nil, err
		}
		switch mode {
		case "manual":
			return &EnrollmentResult{
				Status:  "manual_binding_required",
				Message: "This hub requires manual binding before a machine can be enrolled",
				Email:   email,
			}, nil
		case "approval":
			return s.ensurePendingApprovalForTenant(ctx, tenantID, email, "Awaiting administrator approval before machine enrollment")
		default:
			if !s.allowSelfEnroll {
				return &EnrollmentResult{
					Status:  "manual_binding_required",
					Message: "Self enrollment is disabled. Ask an administrator to generate an SN binding first",
					Email:   email,
				}, nil
			}
			user, err = s.createApprovedUserForTenant(ctx, tenantID, email)
			if err != nil {
				return nil, err
			}
			_, _ = s.createLoginTokenAndNotify(ctx, email)
		}
	}

	return s.issueMachineForUser(ctx, user, machineName, platform, clientID)
}

func (s *IdentityService) RequestEmailLogin(ctx context.Context, email string) (*EmailLoginRequestResult, error) {
	email = normalizeEmail(email)
	tenantID := tenantIDFromContext(ctx)
	if email == "" {
		return nil, ErrInvalidEmail
	}
	if err := s.ensureEmailAllowed(ctx, email); err != nil {
		return nil, err
	}

	user, err := s.users.GetByTenantEmail(ctx, tenantID, email)
	if err != nil {
		return nil, err
	}
	if user == nil {
		mode, err := s.enrollmentModeValue(ctx)
		if err != nil {
			return nil, err
		}
		switch mode {
		case "manual":
			return &EmailLoginRequestResult{
				Status:  "manual_binding_required",
				Message: "This hub requires manual binding before email sign-in can be used",
			}, nil
		case "approval":
			result, err := s.ensurePendingApprovalForTenant(ctx, tenantID, email, "Awaiting administrator approval before email sign-in")
			if err != nil {
				return nil, err
			}
			// Create a login token with a long expiry so the PWA can poll for approval.
			// When the admin approves the enrollment, the token will be consumed
			// and the poll will return "confirmed".
			pollResult, err := s.createLoginTokenForPoll(ctx, email, 24*time.Hour)
			if err != nil {
				return nil, err
			}
			return &EmailLoginRequestResult{
				Status:  result.Status,
				Message: result.Message,
				PollID:  pollResult.PollID,
			}, nil
		default:
			if !s.allowSelfEnroll {
				return &EmailLoginRequestResult{
					Status:  "manual_binding_required",
					Message: "Self enrollment is disabled. Ask an administrator to generate an SN binding first",
				}, nil
			}
			user, err = s.createApprovedUserForTenant(ctx, tenantID, email)
			if err != nil {
				return nil, err
			}
		}
	}

	// User exists and is active. Try to send a login email, but if the mailer
	// fails (network issues, misconfiguration, etc.) fall back to creating a
	// pre-consumed poll token so the PWA can still auto-login via polling.
	result, err := s.createLoginTokenAndNotify(ctx, email)
	if err != nil && user.Status == "active" {
		// Email delivery failed, but the user is already approved.
		// Create a poll token and immediately consume it so the PWA
		// poll returns "confirmed" right away.
		pollResult, pollErr := s.createLoginTokenForPoll(ctx, email, 15*time.Minute)
		if pollErr != nil {
			return nil, err // return original mailer error
		}
		s.consumePendingLoginToken(ctx, email)
		return &EmailLoginRequestResult{
			Status:  "pending_email_confirmation",
			Message: "Email delivery failed, but your account is approved. Please wait a moment.",
			PollID:  pollResult.PollID,
		}, nil
	}
	return result, err
}

// createLoginTokenAndNotify creates (or refreshes) a login token for the given
// email, sends the confirmation email if a mailer is configured, and returns
// the result with a poll_id so the client can poll for confirmation.
func (s *IdentityService) createLoginTokenAndNotify(ctx context.Context, email string) (*EmailLoginRequestResult, error) {
	tenantID := tenantIDFromContext(ctx)
	rawToken, err := randomToken(32)
	if err != nil {
		return nil, err
	}
	rawPollToken, err := randomToken(32)
	if err != nil {
		return nil, err
	}

	// Reuse existing pending login token if one exists (avoid creating duplicates).
	existing, err := s.loginTok.GetPendingByTenantEmail(ctx, tenantID, email)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		// Refresh the token hashes so we can send a new confirm URL and return a new poll_id.
		if err := s.loginTok.RefreshToken(ctx, existing.ID, hashToken(rawToken), hashToken(rawPollToken)); err != nil {
			return nil, err
		}
	} else {
		now := time.Now()
		if err := s.loginTok.Create(ctx, &store.LoginToken{
			ID:            newID("lt"),
			TenantID:      tenantID,
			Email:         email,
			TokenHash:     hashToken(rawToken),
			PollTokenHash: hashToken(rawPollToken),
			Purpose:       "login",
			ExpiresAt:     now.Add(15 * time.Minute),
			CreatedAt:     now,
		}); err != nil {
			return nil, err
		}
	}

	confirmURL := s.buildConfirmURL(rawToken)
	var channels []string
	var emailErr error

	// 1. Send via email (best-effort - don't block IM delivery on email failure)
	if s.mailer != nil {
		if err := s.mailer.SendLoginConfirmation(ctx, email, confirmURL); err != nil {
			emailErr = err
		} else {
			channels = append(channels, "email")
		}
	}

	// 2. Send to bound IM channels
	if s.loginNotifier != nil {
		imChannels := s.loginNotifier.BroadcastLoginLinkForTenant(ctx, tenantID, email, confirmURL)
		channels = append(channels, imChannels...)
	}

	// If no channel succeeded, return the email error (or a generic one).
	if len(channels) == 0 {
		if emailErr != nil {
			return nil, emailErr
		}
		// No mailer and no IM channels - dev mode fallback
		return &EmailLoginRequestResult{
			Status:  "pending_email_confirmation",
			Message: fmt.Sprintf("Use this confirm URL for development: %s", confirmURL),
			PollID:  rawPollToken,
		}, nil
	}

	sentTo := channels[0]
	for _, ch := range channels[1:] {
		sentTo += " + " + ch
	}
	return &EmailLoginRequestResult{
		Status:  "pending_email_confirmation",
		Message: fmt.Sprintf("Verification link sent to: %s", sentTo),
		PollID:  rawPollToken,
		SentTo:  sentTo,
	}, nil
}

// createLoginTokenForPoll creates (or refreshes) a login token for the given
// email with a custom expiry, without sending a confirmation email. This is
// used for approval-mode enrollments where the PWA needs to poll until the
// admin approves.
func (s *IdentityService) createLoginTokenForPoll(ctx context.Context, email string, expiry time.Duration) (*EmailLoginRequestResult, error) {
	tenantID := tenantIDFromContext(ctx)
	rawPollToken, err := randomToken(32)
	if err != nil {
		return nil, err
	}
	rawToken, err := randomToken(32)
	if err != nil {
		return nil, err
	}

	existing, err := s.loginTok.GetPendingByTenantEmail(ctx, tenantID, email)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		if err := s.loginTok.RefreshToken(ctx, existing.ID, hashToken(rawToken), hashToken(rawPollToken)); err != nil {
			return nil, err
		}
	} else {
		now := time.Now()
		if err := s.loginTok.Create(ctx, &store.LoginToken{
			ID:            newID("lt"),
			TenantID:      tenantID,
			Email:         email,
			TokenHash:     hashToken(rawToken),
			PollTokenHash: hashToken(rawPollToken),
			Purpose:       "login",
			ExpiresAt:     now.Add(expiry),
			CreatedAt:     now,
		}); err != nil {
			return nil, err
		}
	}

	return &EmailLoginRequestResult{
		Status: "pending_approval",
		PollID: rawPollToken,
	}, nil
}

func (s *IdentityService) ConfirmEmailLogin(ctx context.Context, rawToken string) (string, *store.User, error) {
	loginToken, err := s.loginTok.GetByTokenHash(ctx, hashToken(rawToken))
	if err != nil {
		return "", nil, err
	}
	if loginToken == nil || loginToken.ConsumedAt != nil || time.Now().After(loginToken.ExpiresAt) {
		return "", nil, ErrInvalidUserCredentials
	}

	ctx = WithTenant(ctx, loginToken.TenantID)
	user, err := s.users.GetByTenantEmail(ctx, loginToken.TenantID, loginToken.Email)
	if err != nil {
		return "", nil, err
	}
	if user == nil || user.Status != "active" {
		return "", nil, ErrInvalidUserCredentials
	}

	rawViewerToken, err := randomToken(32)
	if err != nil {
		return "", nil, err
	}
	now := time.Now()
	if err := s.viewerTok.Create(ctx, &store.ViewerToken{
		ID:        newID("vt"),
		TenantID:  user.TenantID,
		UserID:    user.ID,
		TokenHash: hashToken(rawViewerToken),
		ExpiresAt: now.Add(30 * 24 * time.Hour),
		CreatedAt: now,
	}); err != nil {
		return "", nil, err
	}
	if err := s.loginTok.Consume(ctx, loginToken.ID, now); err != nil {
		return "", nil, err
	}
	if err := s.grantEmailConfirmedBenefitForUser(ctx, user.Email); err != nil {
		return "", nil, err
	}

	return rawViewerToken, user, nil
}

// PollEmailLogin checks if the login token identified by rawPollToken has been
// consumed (i.e. the user clicked the email confirmation link). If consumed,
// it creates a new viewer token and returns it so the original PWA tab can
// automatically sign in.
func (s *IdentityService) PollEmailLogin(ctx context.Context, rawPollToken string) (*EmailPollResult, error) {
	loginToken, err := s.loginTok.GetByPollTokenHash(ctx, hashToken(rawPollToken))
	if err != nil {
		return nil, err
	}
	if loginToken == nil {
		return &EmailPollResult{Status: "invalid"}, nil
	}
	if time.Now().After(loginToken.ExpiresAt) {
		return &EmailPollResult{Status: "expired"}, nil
	}
	if loginToken.ConsumedAt == nil {
		return &EmailPollResult{Status: "pending"}, nil
	}

	// Token was consumed - the user confirmed via email link.
	// Issue a viewer token for this polling client too.
	ctx = WithTenant(ctx, loginToken.TenantID)
	user, err := s.users.GetByTenantEmail(ctx, loginToken.TenantID, loginToken.Email)
	if err != nil {
		return nil, err
	}
	if user == nil || user.Status != "active" {
		return &EmailPollResult{Status: "confirmed_but_inactive"}, nil
	}

	rawViewerToken, err := randomToken(32)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	if err := s.viewerTok.Create(ctx, &store.ViewerToken{
		ID:        newID("vt"),
		TenantID:  user.TenantID,
		UserID:    user.ID,
		TokenHash: hashToken(rawViewerToken),
		ExpiresAt: now.Add(30 * 24 * time.Hour),
		CreatedAt: now,
	}); err != nil {
		return nil, err
	}

	return &EmailPollResult{
		Status:      "confirmed",
		AccessToken: rawViewerToken,
		ExpiresIn:   30 * 86400,
		Email:       user.Email,
		SN:          user.SN,
	}, nil
}

func (s *IdentityService) ManualBind(ctx context.Context, email string) (*store.User, error) {
	return s.ManualBindForTenant(ctx, store.DefaultTenantID, email)
}

func (s *IdentityService) ManualBindForTenant(ctx context.Context, tenantID, email string) (*store.User, error) {
	email = normalizeEmail(email)
	if email == "" {
		return nil, ErrInvalidEmail
	}
	if err := s.ensureEmailAllowed(ctx, email); err != nil {
		return nil, err
	}
	user, err := s.users.GetByTenantEmail(ctx, tenantID, email)
	if err != nil {
		return nil, err
	}
	if user != nil {
		return user, nil
	}
	return s.createApprovedUserForTenant(ctx, tenantID, email)
}

func (s *IdentityService) LookupUserByEmail(ctx context.Context, email string) (*store.User, error) {
	email = normalizeEmail(email)
	if email == "" {
		return nil, ErrInvalidEmail
	}
	return s.users.GetByTenantEmail(ctx, tenantIDFromContext(ctx), email)
}

func (s *IdentityService) ResolveTenantByEmail(ctx context.Context, email string) (tenantID string, found bool, ambiguous bool, err error) {
	email = normalizeEmail(email)
	if email == "" {
		return "", false, false, ErrInvalidEmail
	}
	items, err := s.users.List(ctx)
	if err != nil {
		return "", false, false, err
	}
	seen := map[string]struct{}{}
	for _, user := range items {
		if user == nil || !strings.EqualFold(normalizeEmail(user.Email), email) {
			continue
		}
		id := normalizeTenantIDValue(user.TenantID)
		seen[id] = struct{}{}
	}
	if len(seen) == 0 {
		return "", false, false, nil
	}
	if len(seen) > 1 {
		return "", true, true, nil
	}
	for id := range seen {
		return id, true, false, nil
	}
	return "", false, false, nil
}

// LookupUserByMobile finds a user by mobile number. It first looks up the
// enrollment record to get the email, then resolves the user from the users table.
// It tries exact match first, then with +86 prefix for Chinese numbers.
func (s *IdentityService) LookupUserByMobile(ctx context.Context, mobile string) (*store.User, error) {
	mobile = strings.TrimSpace(mobile)
	mobile = strings.ReplaceAll(mobile, " ", "")
	mobile = strings.ReplaceAll(mobile, "-", "")
	if mobile == "" {
		return nil, fmt.Errorf("mobile is required")
	}

	variants := []string{mobile}
	if len(mobile) == 11 && mobile[0] == '1' {
		variants = append(variants, "+86"+mobile)
	}
	if strings.HasPrefix(mobile, "+86") && len(mobile) == 14 {
		variants = append(variants, mobile[3:])
	}

	items, err := s.enrollments.ListAllByTenant(ctx, tenantIDFromContext(ctx))
	if err != nil {
		return nil, err
	}
	var enrollment *store.UserEnrollment
	for _, item := range items {
		if item == nil {
			continue
		}
		itemMobile := strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(item.Mobile), " ", ""), "-", "")
		for _, variant := range variants {
			if itemMobile == variant {
				enrollment = item
				break
			}
		}
		if enrollment != nil {
			break
		}
	}

	if enrollment == nil {
		return nil, nil
	}
	return s.users.GetByTenantEmail(ctx, enrollment.TenantID, enrollment.Email)
}

func (s *IdentityService) IsEmailBlocked(ctx context.Context, email string) (bool, error) {
	email = normalizeEmail(email)
	if email == "" {
		return false, ErrInvalidEmail
	}
	if s.blocks == nil {
		return false, nil
	}
	item, err := s.blocks.GetByTenantEmail(ctx, tenantIDFromContext(ctx), email)
	if err != nil {
		return false, err
	}
	return item != nil, nil
}

func (s *IdentityService) BuildPWAEntryURL(email string) string {
	base := s.publicBaseURL
	if base == "" {
		base = "http://127.0.0.1:9399"
	}
	return fmt.Sprintf(
		"%s/app?email=%s&entry=app&autologin=1",
		base,
		url.QueryEscape(normalizeEmail(email)),
	)
}

func (s *IdentityService) ListUsers(ctx context.Context) ([]*store.User, error) {
	return s.users.List(ctx)
}

func (s *IdentityService) ListUsersForTenant(ctx context.Context, tenantID string) ([]*store.User, error) {
	return s.users.ListByTenant(ctx, tenantID)
}

func (s *IdentityService) EnrollmentMode(ctx context.Context) (string, error) {
	return s.enrollmentModeValue(ctx)
}

func (s *IdentityService) SetEnrollmentMode(ctx context.Context, mode string) error {
	normalized := normalizeEnrollmentMode(mode)
	if s.settings == nil {
		s.enrollmentMode = normalized
		return nil
	}
	if err := s.settings.Set(ctx, systemKeyEnrollmentMode, settingsJSON(map[string]string{"value": normalized})); err != nil {
		return err
	}
	s.enrollmentMode = normalized
	return nil
}

func (s *IdentityService) AddBlockedEmail(ctx context.Context, email, reason string) error {
	email = normalizeEmail(email)
	if email == "" {
		return ErrInvalidEmail
	}
	if s.blocks == nil {
		return nil
	}
	now := time.Now()
	return s.blocks.Create(ctx, &store.EmailBlockItem{
		ID:        newID("blk"),
		TenantID:  tenantIDFromContext(ctx),
		Email:     email,
		Reason:    strings.TrimSpace(reason),
		CreatedAt: now,
		UpdatedAt: now,
	})
}

func (s *IdentityService) ListBlockedEmails(ctx context.Context) ([]*store.EmailBlockItem, error) {
	if s.blocks == nil {
		return []*store.EmailBlockItem{}, nil
	}
	return s.blocks.ListByTenant(ctx, tenantIDFromContext(ctx))
}

func (s *IdentityService) RemoveBlockedEmail(ctx context.Context, email string) error {
	email = normalizeEmail(email)
	if email == "" {
		return ErrInvalidEmail
	}
	if s.blocks == nil {
		return nil
	}
	return s.blocks.DeleteByTenantEmail(ctx, tenantIDFromContext(ctx), email)
}

func (s *IdentityService) AuthenticateMachine(ctx context.Context, machineID, rawToken string) (*MachinePrincipal, error) {
	machine, err := s.machines.GetByID(ctx, machineID)
	if err != nil {
		return nil, err
	}
	if machine == nil {
		return nil, ErrMachineUnauthorized
	}
	if machine.MachineTokenHash != hashToken(rawToken) {
		return nil, ErrMachineUnauthorized
	}
	return &MachinePrincipal{TenantID: machine.TenantID, UserID: machine.UserID, MachineID: machine.ID}, nil
}

// IssueViewerTokenForUser creates a new viewer token for the given user.
// This is called during WebSocket machine auth so that existing clients
// (which only have a machine_token) can obtain a viewer_token without
// re-enrolling.
func (s *IdentityService) IssueViewerTokenForUser(ctx context.Context, userID string) (string, error) {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return "", err
	}
	tenantID := store.DefaultTenantID
	if user != nil && strings.TrimSpace(user.TenantID) != "" {
		tenantID = user.TenantID
	}
	raw, err := randomToken(32)
	if err != nil {
		return "", err
	}
	now := time.Now()
	if err := s.viewerTok.Create(ctx, &store.ViewerToken{
		ID:        newID("vt"),
		TenantID:  tenantID,
		UserID:    userID,
		TokenHash: hashToken(raw),
		ExpiresAt: now.Add(30 * 24 * time.Hour),
		CreatedAt: now,
	}); err != nil {
		return "", err
	}
	return raw, nil
}

func (s *IdentityService) AuthenticateViewer(ctx context.Context, rawToken string) (*ViewerPrincipal, error) {
	if strings.TrimSpace(rawToken) == "" {
		return nil, ErrInvalidUserCredentials
	}

	viewerToken, err := s.viewerTok.GetByTokenHash(ctx, hashToken(rawToken))
	if err != nil {
		return nil, err
	}
	if viewerToken == nil || viewerToken.RevokedAt != nil || time.Now().After(viewerToken.ExpiresAt) {
		return nil, ErrInvalidUserCredentials
	}

	user, err := s.users.GetByID(ctx, viewerToken.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil || user.Status != "active" {
		return nil, ErrInvalidUserCredentials
	}

	// Sliding expiration: if less than half the lifetime remains, extend to 30 days from now.
	remaining := time.Until(viewerToken.ExpiresAt)
	if remaining < 15*24*time.Hour {
		_ = s.viewerTok.ExtendExpiry(ctx, viewerToken.ID, time.Now().Add(30*24*time.Hour))
	}

	return &ViewerPrincipal{TenantID: user.TenantID, UserID: user.ID, Email: user.Email}, nil
}

func (s *IdentityService) createApprovedUser(ctx context.Context, email string) (*store.User, error) {
	return s.createApprovedUserForTenant(ctx, tenantIDFromContext(ctx), email)
}

func (s *IdentityService) createApprovedUserForTenant(ctx context.Context, tenantID, email string) (*store.User, error) {
	now := time.Now()
	user := &store.User{
		ID:               newID("u"),
		TenantID:         tenantID,
		Email:            email,
		SN:               generateSN(),
		Status:           "active",
		EnrollmentStatus: "approved",
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := s.users.Create(ctx, user); err != nil {
		return nil, err
	}
	ctx = WithTenant(ctx, tenantID)
	if err := s.ensureDefaultLLMServiceForUser(ctx, email); err != nil {
		return nil, err
	}
	s.syncUserRoute(ctx, email)
	return user, nil
}

func (s *IdentityService) ensureDefaultLLMServiceForUser(ctx context.Context, email string) error {
	if s == nil || s.settings == nil {
		return nil
	}
	return llmservice.GrantDefaultServiceForNewUser(ctx, s.settings, email)
}

func (s *IdentityService) grantEmailConfirmedBenefitForUser(ctx context.Context, email string) error {
	if s == nil || s.settings == nil {
		return nil
	}
	return llmservice.GrantEmailConfirmedBenefitForUser(ctx, s.settings, email)
}

// ListPendingEnrollments returns all enrollment requests with status "pending".
func (s *IdentityService) ListPendingEnrollments(ctx context.Context) ([]*store.UserEnrollment, error) {
	return s.enrollments.ListPendingByTenant(ctx, tenantIDFromContext(ctx))
}

// ListAllEnrollments returns all enrollment requests regardless of status.
func (s *IdentityService) ListAllEnrollments(ctx context.Context) ([]*store.UserEnrollment, error) {
	return s.enrollments.ListAllByTenant(ctx, tenantIDFromContext(ctx))
}

// ApproveEnrollment approves a pending enrollment and creates an active user.
func (s *IdentityService) ApproveEnrollment(ctx context.Context, id string) (*store.User, *store.UserEnrollment, error) {
	// We need to find the enrollment to get the email - list all pending and find by ID
	tenantID := tenantIDFromContext(ctx)
	pending, err := s.enrollments.ListPendingByTenant(ctx, tenantID)
	if err != nil {
		return nil, nil, err
	}
	var target *store.UserEnrollment
	for _, p := range pending {
		if p.ID == id {
			target = p
			break
		}
	}
	if target == nil {
		return nil, nil, fmt.Errorf("enrollment not found or not pending: %s", id)
	}
	if strings.TrimSpace(target.TenantID) != "" {
		tenantID = target.TenantID
		ctx = WithTenant(ctx, tenantID)
	}
	if err := s.enrollments.Approve(ctx, id, time.Now()); err != nil {
		return nil, nil, err
	}
	// Check if user already exists (e.g. re-approval)
	existing, _ := s.users.GetByTenantEmail(ctx, tenantID, target.Email)
	if existing != nil {
		existing.EnrollmentStatus = "approved"
		existing.Status = "active"
		if err := s.ensureDefaultLLMServiceForUser(ctx, target.Email); err != nil {
			return nil, nil, err
		}
		// Consume any pending login token so the PWA poll returns "confirmed".
		s.consumePendingLoginToken(ctx, target.Email)
		s.syncUserRoute(ctx, target.Email)
		return existing, target, nil
	}
	user, err := s.createApprovedUserForTenant(ctx, tenantID, target.Email)
	if err != nil {
		return nil, nil, err
	}
	// Consume any pending login token so the PWA poll returns "confirmed".
	s.consumePendingLoginToken(ctx, target.Email)
	s.syncUserRoute(ctx, target.Email)
	return user, target, nil
}

// RejectEnrollment rejects a pending enrollment request.
func (s *IdentityService) RejectEnrollment(ctx context.Context, id string) error {
	tenantID := tenantIDFromContext(ctx)
	pending, err := s.enrollments.ListPendingByTenant(ctx, tenantID)
	if err != nil {
		return err
	}
	found := false
	for _, item := range pending {
		if item != nil && item.ID == id {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("enrollment not found or not pending: %s", id)
	}
	return s.enrollments.Reject(ctx, id, time.Now())
}

// ListPendingLoginTokens returns all unconsumed, non-expired login tokens.
func (s *IdentityService) ListPendingLoginTokens(ctx context.Context) ([]*store.LoginToken, error) {
	return s.loginTok.ListPendingByTenant(ctx, tenantIDFromContext(ctx))
}

// AdminConfirmLoginByEmail consumes the pending login token for the given email
// so that the PWA poll will see it as confirmed. If the user does not exist yet,
// it creates an approved user first.
func (s *IdentityService) AdminConfirmLoginByEmail(ctx context.Context, email string) (*store.User, error) {
	email = normalizeEmail(email)
	if email == "" {
		return nil, ErrInvalidEmail
	}

	// Ensure the user exists (create if needed).
	tenantID := tenantIDFromContext(ctx)
	user, err := s.users.GetByTenantEmail(ctx, tenantID, email)
	if err != nil {
		return nil, err
	}
	if user == nil {
		user, err = s.createApprovedUser(ctx, email)
		if err != nil {
			return nil, err
		}
	}

	// Also approve any pending enrollment for this email.
	if pendingEnr, _ := s.enrollments.GetPendingByTenantEmail(ctx, tenantID, email); pendingEnr != nil {
		_ = s.enrollments.Approve(ctx, pendingEnr.ID, time.Now())
	}

	// Consume the pending login token so the PWA poll returns "confirmed".
	s.consumePendingLoginToken(ctx, email)
	s.syncUserRoute(ctx, email)

	return user, nil
}

// consumePendingLoginToken consumes the pending login token for the given email
// (best-effort, errors are ignored). This allows the PWA poll to see the token
// as consumed and return "confirmed" with an access token.
func (s *IdentityService) consumePendingLoginToken(ctx context.Context, email string) {
	pending, err := s.loginTok.GetPendingByTenantEmail(ctx, tenantIDFromContext(ctx), email)
	if err != nil || pending == nil {
		return
	}
	_ = s.loginTok.Consume(ctx, pending.ID, time.Now())
}

func (s *IdentityService) ensurePendingApproval(ctx context.Context, email, message string) (*EnrollmentResult, error) {
	return s.ensurePendingApprovalForTenant(ctx, tenantIDFromContext(ctx), email, message)
}

func (s *IdentityService) ensurePendingApprovalForTenant(ctx context.Context, tenantID, email, message string) (*EnrollmentResult, error) {
	pending, err := s.enrollments.GetPendingByTenantEmail(ctx, tenantID, email)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	if pending == nil {
		if err := s.enrollments.Create(ctx, &store.UserEnrollment{
			ID:        newID("enr"),
			TenantID:  tenantID,
			Email:     email,
			Status:    "pending",
			Note:      message,
			CreatedAt: now,
			UpdatedAt: now,
		}); err != nil {
			return nil, err
		}
	}
	return &EnrollmentResult{
		Status:  "pending_approval",
		Message: message,
		Email:   email,
	}, nil
}

func (s *IdentityService) issueMachineForUser(ctx context.Context, user *store.User, machineName, platform, clientID string) (*EnrollmentResult, error) {
	// Derive a deterministic machine ID from user_id + client_id so the same
	// physical machine always maps to the same record regardless of re-enrollment.
	machineID := deriveMachineID(user.ID, clientID)

	rawToken, err := randomToken(32)
	if err != nil {
		return nil, err
	}

	now := time.Now()

	existing, err := s.machines.GetByID(ctx, machineID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		// Reissue a new token for the existing machine
		if err := s.machines.UpdateTokenHash(ctx, machineID, hashToken(rawToken)); err != nil {
			return nil, err
		}
	} else {
		machine := &store.Machine{
			ID:               machineID,
			TenantID:         user.TenantID,
			UserID:           user.ID,
			ClientID:         clientID,
			Name:             defaultIfEmpty(machineName, "MaClaw Desktop"),
			Platform:         defaultIfEmpty(platform, "unknown"),
			MachineTokenHash: hashToken(rawToken),
			Status:           "offline",
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		if err := s.machines.Create(ctx, machine); err != nil {
			return nil, err
		}
	}

	// Issue a viewer token after the machine operation succeeds, so we don't
	// leave orphan tokens in the DB if machine creation/update fails.
	rawViewerToken, err := randomToken(32)
	if err != nil {
		return nil, err
	}
	if err := s.viewerTok.Create(ctx, &store.ViewerToken{
		ID:        newID("vt"),
		TenantID:  user.TenantID,
		UserID:    user.ID,
		TokenHash: hashToken(rawViewerToken),
		ExpiresAt: now.Add(30 * 24 * time.Hour),
		CreatedAt: now,
	}); err != nil {
		return nil, err
	}

	return &EnrollmentResult{
		Status:       "approved",
		UserID:       user.ID,
		Email:        user.Email,
		SN:           user.SN,
		MachineID:    machineID,
		MachineToken: rawToken,
		ViewerToken:  rawViewerToken,
	}, nil
}

// deriveMachineID produces a stable, deterministic machine ID from user and client identifiers.
func deriveMachineID(userID, clientID string) string {
	h := sha256.New()
	h.Write([]byte(userID))
	h.Write([]byte(":"))
	h.Write([]byte(clientID))
	return "m_" + hex.EncodeToString(h.Sum(nil))[:16]
}

func (s *IdentityService) ensureEmailAllowed(ctx context.Context, email string) error {
	if s.blocks == nil {
		return nil
	}
	item, err := s.blocks.GetByTenantEmail(ctx, tenantIDFromContext(ctx), email)
	if err != nil {
		return err
	}
	if item != nil {
		return ErrEmailBlocked
	}
	return nil
}

func generateSN() string {
	return fmt.Sprintf("SN-%d", time.Now().UnixNano())
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func defaultIfEmpty(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func normalizeEnrollmentMode(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "approval":
		return "approval"
	case "manual":
		return "manual"
	default:
		return "open"
	}
}

func (s *IdentityService) enrollmentModeValue(ctx context.Context) (string, error) {
	if s.settings == nil {
		return normalizeEnrollmentMode(s.enrollmentMode), nil
	}
	raw, err := s.settings.Get(ctx, systemKeyEnrollmentMode)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(raw) == "" {
		return normalizeEnrollmentMode(s.enrollmentMode), nil
	}
	var payload struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return "", err
	}
	return normalizeEnrollmentMode(payload.Value), nil
}

func settingsJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return `{}`
	}
	return string(data)
}

func (s *IdentityService) buildConfirmURL(rawToken string) string {
	base := s.resolvePublicBaseURL()
	if base == "" {
		base = "http://127.0.0.1:9399"
	}
	return fmt.Sprintf("%s/app/auth/confirm?token=%s", base, url.QueryEscape(rawToken))
}

// resolvePublicBaseURL reads the dynamic public base URL from settings,
// falling back to the static config value passed at construction time.
func (s *IdentityService) resolvePublicBaseURL() string {
	raw, err := s.settings.Get(context.Background(), systemKeyPublicBaseURL)
	if err != nil || raw == "" {
		return s.publicBaseURL
	}
	var payload struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil || payload.Value == "" {
		return s.publicBaseURL
	}
	return strings.TrimRight(payload.Value, "/")
}
