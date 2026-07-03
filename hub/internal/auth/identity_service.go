package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"
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
	ErrRegistrationDisabled   = errors.New("new user registration is disabled for this tenant")
	ErrRoutedToAnotherHub     = errors.New("email is routed to another hub")
	identitySNCounter         atomic.Uint64
	tenantEmailDomainPattern  = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$`)
)

// InvitationCodeValidator abstracts the invitation code service to avoid circular imports.
type InvitationCodeValidator interface {
	IsRequired(ctx context.Context) (bool, error)
	IsRequiredForTenant(ctx context.Context, tenantID string) (bool, error)
	ValidateAndConsume(ctx context.Context, code string, email string) error
	ValidateAndConsumeForTenant(ctx context.Context, tenantID string, code string, email string) error
	CheckExpiry(ctx context.Context, email string) (bool, *time.Time, error)
	CheckExpiryForTenant(ctx context.Context, tenantID string, email string) (bool, *time.Time, error)
}

type invitationCodeGrantProvider interface {
	GetCodeByTenantEmail(ctx context.Context, tenantID, email string) (*store.InvitationCode, error)
}

// LoginNotifier sends login confirmation links to bound IM channels.
type LoginNotifier interface {
	BroadcastLoginLink(ctx context.Context, email, confirmURL string) []string
	BroadcastLoginLinkForTenant(ctx context.Context, tenantID, email, confirmURL string) []string
}

// UserRouteSyncer pushes confirmed user-to-hub bindings to Hub Center when available.
type UserRouteSyncer interface {
	SyncUserRoute(ctx context.Context, email string, tenantIDOpt ...string) error
	// SyncUserRouteReplaceAll is like SyncUserRoute but removes ALL existing routes
	// for this email on HubCenter before creating the new one. Used after invitation-
	// code enrollment to fully migrate the user to the new Hub.
	SyncUserRouteReplaceAll(ctx context.Context, email string, tenantIDOpt ...string) error
}

type UserRouteValidator interface {
	AllowsUserRoute(ctx context.Context, email string, tenantIDOpt ...string) (bool, string, error)
}

const (
	systemKeyEnrollmentMode      = "identity_enrollment_mode"
	systemKeyPublicBaseURL       = "server_public_base_url"
	systemKeyCenterRegistration  = "center_registration"
	defaultMobileOfficialLLMMode = "maclaw_official"
)

type EnrollmentResult struct {
	Status       string `json:"status"`
	TenantID     string `json:"tenant_id,omitempty"`
	TenantName   string `json:"tenant_name,omitempty"`
	Message      string `json:"message,omitempty"`
	UserID       string `json:"user_id,omitempty"`
	Email        string `json:"email,omitempty"`
	SN           string `json:"sn,omitempty"`
	MachineID    string `json:"machine_id,omitempty"`
	MachineToken string `json:"machine_token,omitempty"`
	ViewerToken  string `json:"viewer_token,omitempty"`
	ExpiresAt    string `json:"expires_at,omitempty"`
}

// EnrollOption allows passing optional parameters to StartEnrollment.
type EnrollOption func(*enrollOptions)

type enrollOptions struct {
	Language      string
	PhoneVerified bool
}

// WithLanguage sets the UI language for registration emails.
func WithLanguage(lang string) EnrollOption {
	return func(o *enrollOptions) { o.Language = lang }
}

func WithPhoneVerifiedRegistration() EnrollOption {
	return func(o *enrollOptions) { o.PhoneVerified = true }
}

func (s *IdentityService) TenantDisplayName(ctx context.Context, tenantID string) string {
	tenantID = normalizeTenantIDValue(tenantID)
	if s == nil || s.tenants == nil {
		return tenantID
	}
	tenant, err := s.tenants.GetByID(ctx, tenantID)
	if err != nil || tenant == nil {
		return tenantID
	}
	if name := strings.TrimSpace(tenant.Name); name != "" {
		return name
	}
	if slug := strings.TrimSpace(tenant.Slug); slug != "" {
		return slug
	}
	return tenantID
}

type EmailLoginRequestResult struct {
	Status       string         `json:"status"`
	TenantID     string         `json:"tenant_id,omitempty"`
	Message      string         `json:"message,omitempty"`
	PollID       string         `json:"poll_id,omitempty"`
	SentTo       string         `json:"sent_to,omitempty"`
	HubURL       string         `json:"hub_url,omitempty"`
	HubID        string         `json:"hub_id,omitempty"`
	HubCenterURL string         `json:"hubcenter_url,omitempty"`
	Hub          *EmailLoginHub `json:"hub,omitempty"`
	LLM          *EmailLoginLLM `json:"llm,omitempty"`
}

type EmailPollResult struct {
	Status       string          `json:"status"`
	TenantID     string          `json:"tenant_id,omitempty"`
	AccessToken  string          `json:"access_token,omitempty"`
	ExpiresIn    int             `json:"expires_in,omitempty"`
	Email        string          `json:"email,omitempty"`
	SN           string          `json:"sn,omitempty"`
	HubURL       string          `json:"hub_url,omitempty"`
	HubID        string          `json:"hub_id,omitempty"`
	HubCenterURL string          `json:"hubcenter_url,omitempty"`
	Hub          *EmailLoginHub  `json:"hub,omitempty"`
	User         *EmailLoginUser `json:"user,omitempty"`
	LLM          *EmailLoginLLM  `json:"llm,omitempty"`
}

type EmailLoginHub struct {
	ID      string `json:"id,omitempty"`
	BaseURL string `json:"base_url,omitempty"`
	URL     string `json:"url,omitempty"`
}

type EmailLoginUser struct {
	TenantID string `json:"tenant_id,omitempty"`
	Email    string `json:"email,omitempty"`
	SN       string `json:"sn,omitempty"`
}

type EmailLoginLLM struct {
	Mode            string `json:"mode"`
	AuthorizationID string `json:"authorization_id,omitempty"`
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
	users              store.UserRepository
	enrollments        store.EnrollmentRepository
	blocks             store.EmailBlocklistRepository
	machines           store.MachineRepository
	viewerTok          store.ViewerTokenRepository
	loginTok           store.LoginTokenRepository
	tenants            store.TenantRepository
	settings           SystemSettingsRepository
	invitationSvc      InvitationCodeValidator
	enrollmentMode     string
	allowSelfEnroll    bool
	mailer             mail.Mailer
	publicBaseURL      string
	loginNotifier      LoginNotifier
	userRouteSyncer    UserRouteSyncer
	userRouteValidator UserRouteValidator
}

func (s *IdentityService) UsersRepo() store.UserRepository {
	if s == nil {
		return nil
	}
	return s.users
}

func (s *IdentityService) MachinesRepo() store.MachineRepository {
	if s == nil {
		return nil
	}
	return s.machines
}

func (s *IdentityService) EnrollmentsRepo() store.EnrollmentRepository {
	if s == nil {
		return nil
	}
	return s.enrollments
}

func (s *IdentityService) ViewerTokensRepo() store.ViewerTokenRepository {
	if s == nil {
		return nil
	}
	return s.viewerTok
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

func (s *IdentityService) SetTenantRepository(tenants store.TenantRepository) {
	if s == nil {
		return
	}
	s.tenants = tenants
}

// SetLoginNotifier wires the cross-IM login link broadcaster.
// Called from bootstrap after the IM Adapter is fully assembled.
func (s *IdentityService) SetLoginNotifier(n LoginNotifier) {
	s.loginNotifier = n
}

func (s *IdentityService) SetUserRouteSyncer(syncer UserRouteSyncer) {
	s.userRouteSyncer = syncer
	s.userRouteValidator = nil
	if validator, ok := syncer.(UserRouteValidator); ok {
		s.userRouteValidator = validator
	}
}

func (s *IdentityService) ensureUserRouteAllowed(ctx context.Context, email string) error {
	if s == nil || s.userRouteValidator == nil || strings.TrimSpace(email) == "" {
		return nil
	}
	allowed, targetHubID, err := s.userRouteValidator.AllowsUserRoute(ctx, email, tenantIDFromContext(ctx))
	if err != nil || allowed {
		return err
	}
	if strings.TrimSpace(targetHubID) != "" {
		return fmt.Errorf("%w: %s", ErrRoutedToAnotherHub, targetHubID)
	}
	return ErrRoutedToAnotherHub
}

func (s *IdentityService) CanRegisterUserRoute(ctx context.Context, email string) error {
	return s.ensureUserRouteAllowed(ctx, email)
}

func (s *IdentityService) syncUserRoute(ctx context.Context, email string) {
	if s == nil || s.userRouteSyncer == nil || strings.TrimSpace(email) == "" {
		return
	}
	if s.userRouteValidator != nil {
		allowed, _, err := s.userRouteValidator.AllowsUserRoute(ctx, email, tenantIDFromContext(ctx))
		if err != nil || !allowed {
			return
		}
	}
	_ = s.userRouteSyncer.SyncUserRoute(ctx, email, tenantIDFromContext(ctx))
}

func (s *IdentityService) StartEnrollment(ctx context.Context, email, machineName, platform, clientID, invitationCode string, opts ...EnrollOption) (*EnrollmentResult, error) {
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
	invitationRebind := false
	if user != nil && s.invitationSvc != nil {
		expired, expiresAt, _ := s.invitationSvc.CheckExpiryForTenant(ctx, tenantID, email)
		if expired {
			if strings.TrimSpace(invitationCode) != "" {
				// Expired user provided a new invitation code - rebind
				if err := s.invitationSvc.ValidateAndConsumeForTenant(ctx, tenantID, invitationCode, email); err != nil {
					return nil, ErrInvalidInvitationCode
				}
				if err := s.grantInvitationCodeLLMServiceForUser(ctx, tenantID, user.ID, email); err != nil {
					return nil, err
				}
				invitationRebind = true
				// Continue normal enrollment flow
			} else {
				// Expired and no new code provided
				result := &EnrollmentResult{
					Status:   "invitation_expired",
					TenantID: tenantID,
					Message:  "invitation code has expired",
					Email:    email,
				}
				if expiresAt != nil {
					result.ExpiresAt = expiresAt.Format(time.RFC3339)
				}
				return result, ErrInvitationExpired
			}
		}
	}

	// Invitation code validation - only required for new users
	invitationAccepted := false
	if user == nil && s.invitationSvc != nil {
		required, err := s.invitationSvc.IsRequiredForTenant(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		code := strings.TrimSpace(invitationCode)
		if code != "" {
			if err := s.invitationSvc.ValidateAndConsumeForTenant(ctx, tenantID, code, email); err != nil {
				return nil, ErrInvalidInvitationCode
			}
			invitationAccepted = true
		} else if required {
			return nil, ErrInvitationCodeRequired
		}
	}

	if user == nil {
		if err := s.ensureUserRouteAllowed(ctx, email); err != nil {
			return nil, err
		}
		if !invitationAccepted {
			allowed, err := s.tenantAllowsNewUserRegistration(ctx, tenantID)
			if err != nil {
				return nil, err
			}
			if !allowed {
				return nil, ErrRegistrationDisabled
			}
		}
		mode, err := s.enrollmentModeValue(ctx)
		if err != nil {
			return nil, err
		}
		switch mode {
		case "manual":
			return &EnrollmentResult{
				Status:   "manual_binding_required",
				TenantID: tenantID,
				Message:  "This hub requires manual binding before a machine can be enrolled",
				Email:    email,
			}, nil
		case "approval":
			return s.ensurePendingApprovalForTenant(ctx, tenantID, email, "Awaiting administrator approval before machine enrollment")
		default:
			if !s.allowSelfEnroll {
				return &EnrollmentResult{
					Status:   "manual_binding_required",
					TenantID: tenantID,
					Message:  "Self enrollment is disabled. Ask an administrator to generate an SN binding first",
					Email:    email,
				}, nil
			}
			user, err = s.createApprovedUserForTenant(ctx, tenantID, email)
			if err != nil {
				return nil, err
			}
			// Send dedicated registration verification email (explains 70% bonus).
			// If sending fails, grant the benefit directly — don't penalize the user
			// for server-side email delivery issues.
			var eopts enrollOptions
			for _, opt := range opts {
				opt(&eopts)
			}
			if eopts.PhoneVerified {
				_ = s.grantPhoneVerifiedBenefitForUser(ctx, user.ID, email)
			} else if _, notifyErr := s.sendRegistrationVerification(ctx, email, eopts.Language); notifyErr != nil {
				_ = s.grantEmailConfirmedBenefitForUser(ctx, user.ID, email)
				_ = s.users.MarkEmailVerified(ctx, tenantID, email)
			}
		}
	}

	// When a user was enrolled via invitation code, replace all existing routes
	// on HubCenter to point to this Hub. This ensures re-invitation to a new Hub
	// correctly overrides stale routes from the old Hub.
	if (invitationAccepted || invitationRebind) && s.userRouteSyncer != nil {
		if err := s.userRouteSyncer.SyncUserRouteReplaceAll(ctx, email, tenantIDFromContext(ctx)); err != nil {
			log.Printf("[enrollment] SyncUserRouteReplaceAll failed for %s: %v (route may still point to old hub)", email, err)
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
		if err := s.ensureUserRouteAllowed(ctx, email); err != nil {
			return nil, err
		}
		allowed, err := s.tenantAllowsNewUserRegistration(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		if !allowed {
			return nil, ErrRegistrationDisabled
		}
		mode, err := s.enrollmentModeValue(ctx)
		if err != nil {
			return nil, err
		}
		switch mode {
		case "manual":
			return &EmailLoginRequestResult{
				Status:   "manual_binding_required",
				TenantID: tenantID,
				Message:  "This hub requires manual binding before email sign-in can be used",
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
			return s.withMobileLoginRequestContext(ctx, &EmailLoginRequestResult{
				Status:   result.Status,
				TenantID: tenantID,
				Message:  result.Message,
				PollID:   pollResult.PollID,
			}), nil
		default:
			if !s.allowSelfEnroll {
				return &EmailLoginRequestResult{
					Status:   "manual_binding_required",
					TenantID: tenantID,
					Message:  "Self enrollment is disabled. Ask an administrator to generate an SN binding first",
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
		return s.withMobileLoginRequestContext(ctx, &EmailLoginRequestResult{
			Status:   "pending_email_confirmation",
			TenantID: tenantID,
			Message:  "Email delivery failed, but your account is approved. Please wait a moment.",
			PollID:   pollResult.PollID,
		}), nil
	}
	if result != nil {
		result = s.withMobileLoginRequestContext(ctx, result)
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
		if err := s.mailer.SendLoginConfirmation(store.WithTenant(ctx, tenantID), email, confirmURL); err != nil {
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
		return s.withMobileLoginRequestContext(ctx, &EmailLoginRequestResult{
			Status:   "pending_email_confirmation",
			TenantID: tenantID,
			Message:  fmt.Sprintf("Use this confirm URL for development: %s", confirmURL),
			PollID:   rawPollToken,
		}), nil
	}

	sentTo := channels[0]
	for _, ch := range channels[1:] {
		sentTo += " + " + ch
	}
	return s.withMobileLoginRequestContext(ctx, &EmailLoginRequestResult{
		Status:   "pending_email_confirmation",
		TenantID: tenantID,
		Message:  fmt.Sprintf("Verification link sent to: %s", sentTo),
		PollID:   rawPollToken,
		SentTo:   sentTo,
	}), nil
}

// sendRegistrationVerification creates a login token and sends a dedicated
// registration verification email (with language-appropriate content explaining
// the 70% bonus credits). This is distinct from SendLoginConfirmation which is
// a generic "click to sign in" email.
func (s *IdentityService) sendRegistrationVerification(ctx context.Context, email, language string) (*EmailLoginRequestResult, error) {
	tenantID := tenantIDFromContext(ctx)
	rawToken, err := randomToken(32)
	if err != nil {
		return nil, err
	}
	rawPollToken, err := randomToken(32)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	if err := s.loginTok.Create(ctx, &store.LoginToken{
		ID:            newID("lt"),
		TenantID:      tenantID,
		Email:         email,
		TokenHash:     hashToken(rawToken),
		PollTokenHash: hashToken(rawPollToken),
		Purpose:       "login",
		ExpiresAt:     now.Add(72 * time.Hour), // 3 days for registration verification (vs 15min for login)
		CreatedAt:     now,
	}); err != nil {
		return nil, err
	}

	// Use a direct GET endpoint for email verification — user clicks the link
	// and gets a success page immediately, no PWA frontend required.
	confirmURL := s.buildVerifyEmailURL(rawToken)

	if s.mailer == nil {
		return nil, fmt.Errorf("mail delivery is not configured")
	}
	if err := s.mailer.SendRegistrationVerification(store.WithTenant(ctx, tenantID), email, confirmURL, language); err != nil {
		return nil, err
	}

	return &EmailLoginRequestResult{
		Status:   "pending_email_confirmation",
		TenantID: tenantID,
		Message:  "Registration verification email sent",
		PollID:   rawPollToken,
		SentTo:   "email",
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

	return s.withMobileLoginRequestContext(ctx, &EmailLoginRequestResult{
		Status:   "pending_approval",
		TenantID: tenantID,
		PollID:   rawPollToken,
	}), nil
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
	if err := s.grantEmailConfirmedBenefitForUser(ctx, user.ID, user.Email); err != nil {
		return "", nil, err
	}
	// Mark user as email-verified in the user record.
	_ = s.users.MarkEmailVerified(ctx, user.TenantID, user.Email)

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

	return s.withMobileLoginPollContext(ctx, user, &EmailPollResult{
		Status:      "confirmed",
		TenantID:    user.TenantID,
		AccessToken: rawViewerToken,
		ExpiresIn:   30 * 86400,
		Email:       user.Email,
		SN:          user.SN,
	}), nil
}

func (s *IdentityService) ManualBind(ctx context.Context, email string) (*store.User, error) {
	return s.ManualBindForTenant(ctx, store.DefaultTenantID, email)
}

func (s *IdentityService) withMobileLoginRequestContext(ctx context.Context, result *EmailLoginRequestResult) *EmailLoginRequestResult {
	if result == nil {
		return nil
	}
	hub := s.currentLoginHubPayload()
	result.Hub = hub
	if hub != nil {
		result.HubURL = hub.BaseURL
		result.HubID = hub.ID
	}
	if result.LLM == nil {
		result.LLM = &EmailLoginLLM{Mode: defaultMobileOfficialLLMMode}
	}
	return result
}

func (s *IdentityService) withMobileLoginPollContext(ctx context.Context, user *store.User, result *EmailPollResult) *EmailPollResult {
	if result == nil {
		return nil
	}
	hub := s.currentLoginHubPayload()
	result.Hub = hub
	if hub != nil {
		result.HubURL = hub.BaseURL
		result.HubID = hub.ID
	}
	if user != nil {
		result.User = &EmailLoginUser{
			TenantID: user.TenantID,
			Email:    user.Email,
			SN:       user.SN,
		}
	}
	if result.LLM == nil {
		result.LLM = &EmailLoginLLM{Mode: defaultMobileOfficialLLMMode}
	}
	return result
}

func (s *IdentityService) currentLoginHubPayload() *EmailLoginHub {
	hubURL := strings.TrimRight(strings.TrimSpace(s.resolvePublicBaseURL()), "/")
	if hubURL == "" {
		hubURL = "http://127.0.0.1:9399"
	}
	return &EmailLoginHub{
		ID:      s.currentHubID(),
		BaseURL: hubURL,
		URL:     hubURL,
	}
}

func (s *IdentityService) currentHubID() string {
	if s == nil || s.settings == nil {
		return ""
	}
	raw, err := s.settings.Get(context.Background(), systemKeyCenterRegistration)
	if err != nil || strings.TrimSpace(raw) == "" {
		return ""
	}
	var payload struct {
		HubID string `json:"hub_id"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.HubID)
}

func (s *IdentityService) tenantAllowsNewUserRegistration(ctx context.Context, tenantID string) (bool, error) {
	if s == nil || s.tenants == nil {
		return true, nil
	}
	tenant, err := s.tenants.GetByID(ctx, normalizeTenantIDValue(tenantID))
	if err != nil {
		return false, err
	}
	if tenant == nil {
		return true, nil
	}
	return tenantAllowsNewUserRegistration(tenant), nil
}

func tenantAllowsNewUserRegistration(tenant *store.Tenant) bool {
	if tenant == nil || strings.TrimSpace(tenant.SettingsJSON) == "" {
		return true
	}
	settings := map[string]any{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(tenant.SettingsJSON)), &settings); err != nil {
		return true
	}
	for _, key := range []string{"allow_user_registration", "registration_enabled"} {
		if value, ok := settings[key].(bool); ok {
			return value
		}
	}
	return true
}

func (s *IdentityService) ManualBindForTenant(ctx context.Context, tenantID, email string) (*store.User, error) {
	tenantID = normalizeTenantIDValue(tenantID)
	ctx = WithTenant(ctx, tenantID)
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
	if err := s.ensureUserRouteAllowed(ctx, email); err != nil {
		return nil, err
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

func (s *IdentityService) LookupUserByPhone(ctx context.Context, phoneNumber string) (*store.User, error) {
	phoneNumber = normalizePhoneIdentityValue(phoneNumber)
	if phoneNumber == "" {
		return nil, ErrInvalidEmail
	}
	if s == nil || s.users == nil {
		return nil, nil
	}
	return s.users.GetByTenantIdentity(ctx, tenantIDFromContext(ctx), "phone", phoneNumber)
}

func (s *IdentityService) BindVerifiedPhoneToUser(ctx context.Context, user *store.User, phoneNumber string) error {
	if s == nil || s.users == nil || user == nil {
		return nil
	}
	phoneNumber = normalizePhoneIdentityValue(phoneNumber)
	if phoneNumber == "" || len(phoneNumber) > 20 {
		return ErrInvalidEmail
	}
	now := time.Now().UTC()
	if err := s.users.UpsertIdentity(ctx, &store.UserIdentity{
		ID:         user.ID + "_phone",
		TenantID:   user.TenantID,
		UserID:     user.ID,
		Type:       "phone",
		Value:      phoneNumber,
		Verified:   true,
		VerifiedAt: &now,
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		return err
	}
	phoneIdentity := "phone:" + phoneNumber
	s.syncVerifiedPhoneRoute(WithTenant(ctx, user.TenantID), phoneIdentity)
	if changed, err := llmservice.BackfillRegistryUserIDs(WithTenant(ctx, user.TenantID), s.settings, s.users, user.TenantID); err != nil {
		log.Printf("[identity] backfill LLM registry user IDs after phone bind failed for %s (%s): %v", user.Email, user.ID, err)
	} else if changed {
		log.Printf("[identity] backfilled LLM registry user IDs after phone bind for %s (%s)", user.Email, user.ID)
	}
	return nil
}

func (s *IdentityService) syncVerifiedPhoneRoute(ctx context.Context, phoneIdentity string) {
	if s == nil || s.userRouteSyncer == nil || strings.TrimSpace(phoneIdentity) == "" {
		return
	}
	if err := s.userRouteSyncer.SyncUserRouteReplaceAll(ctx, phoneIdentity, tenantIDFromContext(ctx)); err != nil {
		log.Printf("[identity] sync verified phone route failed for %s: %v", phoneIdentity, err)
	}
}

func normalizePhoneIdentityValue(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.TrimPrefix(value, "phone:")
	var b strings.Builder
	for _, r := range value {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
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
		return s.resolveTenantByConfiguredDomain(ctx, email)
	}
	if len(seen) > 1 {
		return "", true, true, nil
	}
	for id := range seen {
		return id, true, false, nil
	}
	return "", false, false, nil
}

func (s *IdentityService) resolveTenantByConfiguredDomain(ctx context.Context, email string) (tenantID string, found bool, ambiguous bool, err error) {
	if s == nil || s.tenants == nil {
		return "", false, false, nil
	}
	domain := emailDomain(email)
	if domain == "" {
		return "", false, false, nil
	}
	items, err := s.tenants.List(ctx)
	if err != nil {
		return "", false, false, err
	}
	seen := map[string]struct{}{}
	for _, tenant := range items {
		if tenant == nil || tenant.DeletedAt != nil || !strings.EqualFold(strings.TrimSpace(tenant.Status), "active") {
			continue
		}
		for _, candidate := range tenantConfiguredEmailDomains(tenant) {
			if strings.EqualFold(candidate, domain) {
				seen[normalizeTenantIDValue(tenant.ID)] = struct{}{}
			}
		}
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

func tenantConfiguredEmailDomains(tenant *store.Tenant) []string {
	if tenant == nil {
		return nil
	}
	var raw []string
	if strings.TrimSpace(tenant.PrimaryDomain) != "" {
		raw = append(raw, tenant.PrimaryDomain)
	}
	settings := map[string]any{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(tenant.SettingsJSON)), &settings); err == nil {
		for _, key := range []string{"email_domains", "domains"} {
			if values, ok := settings[key].([]any); ok {
				for _, item := range values {
					if value, ok := item.(string); ok {
						raw = append(raw, value)
					}
				}
			}
		}
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(raw))
	for _, value := range raw {
		for _, part := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t' || r == ' ' }) {
			domain := strings.ToLower(strings.Trim(strings.TrimSpace(part), "."))
			if domain == "" || !tenantEmailDomainPattern.MatchString(domain) {
				continue
			}
			if _, ok := seen[domain]; ok {
				continue
			}
			seen[domain] = struct{}{}
			out = append(out, domain)
		}
	}
	return out
}

func emailDomain(email string) string {
	email = normalizeEmail(email)
	at := strings.LastIndex(email, "@")
	if at < 0 || at == len(email)-1 {
		return ""
	}
	return strings.ToLower(strings.Trim(strings.TrimSpace(email[at+1:]), "."))
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
	if user, err := s.LookupUserByPhone(ctx, mobile); err != nil || user != nil {
		return user, err
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
	tenantID := strings.TrimSpace(tenantIDFromContext(ctx))
	if user != nil && strings.TrimSpace(user.TenantID) != "" {
		tenantID = strings.TrimSpace(user.TenantID)
	}
	if tenantID == "" {
		tenantID = store.DefaultTenantID
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

	tenantID := strings.TrimSpace(user.TenantID)
	if tenantID == "" {
		tenantID = strings.TrimSpace(viewerToken.TenantID)
	}
	if tenantID == "" {
		tenantID = store.DefaultTenantID
	}
	return &ViewerPrincipal{TenantID: tenantID, UserID: user.ID, Email: user.Email}, nil
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
	if err := s.ensureDefaultLLMServiceForUser(ctx, user.ID, email); err != nil {
		return nil, err
	}
	if err := s.grantInvitationCodeLLMServiceForUser(ctx, tenantID, user.ID, email); err != nil {
		return nil, err
	}
	s.syncUserRoute(ctx, email)
	return user, nil
}

func (s *IdentityService) ensureDefaultLLMServiceForUser(ctx context.Context, userID, email string) error {
	if s == nil || s.settings == nil {
		return nil
	}
	return llmservice.GrantDefaultServiceForNewUserID(ctx, s.settings, userID, email)
}

func (s *IdentityService) grantEmailConfirmedBenefitForUser(ctx context.Context, userID, email string) error {
	if s == nil || s.settings == nil {
		return nil
	}
	return llmservice.GrantEmailConfirmedBenefitForUserID(ctx, s.settings, userID, email)
}

func (s *IdentityService) grantPhoneVerifiedBenefitForUser(ctx context.Context, userID, email string) error {
	if s == nil || s.settings == nil {
		return nil
	}
	return llmservice.GrantPhoneVerifiedBenefitForUserID(ctx, s.settings, userID, email)
}

func (s *IdentityService) grantInvitationCodeLLMServiceForUser(ctx context.Context, tenantID, userID, email string) error {
	if s == nil || s.settings == nil || s.invitationSvc == nil {
		return nil
	}
	provider, ok := s.invitationSvc.(invitationCodeGrantProvider)
	if !ok {
		return nil
	}
	code, err := provider.GetCodeByTenantEmail(ctx, tenantID, email)
	if err != nil || code == nil {
		return err
	}
	return llmservice.GrantInvitationCodeBenefitForUserID(ctx, s.settings, userID, email, code.ID, code.LLMServiceGroupID, code.LLMGrantDurationDays, code.LLMGrantCredits)
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
	if err := s.ensureUserRouteAllowed(ctx, target.Email); err != nil {
		return nil, nil, err
	}
	if err := s.enrollments.Approve(ctx, id, time.Now()); err != nil {
		return nil, nil, err
	}
	// Check if user already exists (e.g. re-approval)
	existing, _ := s.users.GetByTenantEmail(ctx, tenantID, target.Email)
	if existing != nil {
		existing.EnrollmentStatus = "approved"
		existing.Status = "active"
		if err := s.ensureDefaultLLMServiceForUser(ctx, existing.ID, target.Email); err != nil {
			return nil, nil, err
		}
		if err := s.grantInvitationCodeLLMServiceForUser(ctx, tenantID, existing.ID, target.Email); err != nil {
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
		Status:   "pending_approval",
		TenantID: tenantID,
		Message:  message,
		Email:    email,
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
		TenantID:     user.TenantID,
		TenantName:   s.TenantDisplayName(ctx, user.TenantID),
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
	return fmt.Sprintf("SN-%d-%d", time.Now().UnixNano(), identitySNCounter.Add(1))
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

func (s *IdentityService) buildVerifyEmailURL(rawToken string) string {
	base := s.resolvePublicBaseURL()
	if base == "" {
		base = "http://127.0.0.1:9399"
	}
	return fmt.Sprintf("%s/api/auth/verify-email?token=%s", base, url.QueryEscape(rawToken))
}

// resolvePublicBaseURL reads the dynamic public base URL from settings,
// falling back to the static config value passed at construction time.
func (s *IdentityService) resolvePublicBaseURL() string {
	if s == nil || s.settings == nil {
		if s == nil {
			return ""
		}
		return s.publicBaseURL
	}
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
