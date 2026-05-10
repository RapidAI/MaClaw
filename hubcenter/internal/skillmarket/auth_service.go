package skillmarket

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/mail"
	"golang.org/x/crypto/bcrypt"
)

const (
	activationTokenExpiry = 24 * time.Hour
	identityTokenExpiry   = 15 * time.Minute
	passwordResetExpiry   = 30 * time.Minute
	sessionExpiry         = 7 * 24 * time.Hour
	bcryptCost            = 10
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrAccountNotActive   = errors.New("account not activated")
	ErrTokenExpired       = errors.New("token expired or invalid")
	ErrPasswordRequired   = errors.New("password is required")
	ErrEmailRequired      = errors.New("email is required")
	ErrCurrentPassword    = errors.New("current password is incorrect")
)

// PublicBaseURLProvider resolves the hubcenter external base URL from system settings.
type PublicBaseURLProvider interface {
	PublicBaseURL(ctx context.Context) (string, error)
}

// AuthService handles registration, login, activation, and identity verification.
type AuthService struct {
	store                *Store
	mailer               *mail.Service
	baseURL              string // static fallback
	urlProvider          PublicBaseURLProvider
	sessionSigningSecret []byte
}

// NewAuthService creates an AuthService.
func NewAuthService(store *Store, mailer *mail.Service, baseURL string) *AuthService {
	return &AuthService{store: store, mailer: mailer, baseURL: strings.TrimRight(baseURL, "/")}
}

// SetPublicBaseURLProvider sets the dynamic URL provider (call after construction to avoid import cycles).
func (a *AuthService) SetPublicBaseURLProvider(p PublicBaseURLProvider) {
	a.urlProvider = p
}

// SetSessionSigningSecret enables locally verifiable session tokens for HA
// clusters. Database-backed sessions remain supported for older clients.
func (a *AuthService) SetSessionSigningSecret(secret string) {
	if a == nil {
		return
	}
	secret = strings.TrimSpace(secret)
	if secret == "" {
		a.sessionSigningSecret = nil
		return
	}
	a.sessionSigningSecret = []byte(secret)
}

// resolveBaseURL reads the configured hubcenter external URL, falling back to the static baseURL.
func (a *AuthService) resolveBaseURL(ctx context.Context) string {
	if a.urlProvider != nil {
		if u, err := a.urlProvider.PublicBaseURL(ctx); err == nil && u != "" {
			return strings.TrimRight(u, "/")
		}
	}
	return a.baseURL
}

// Register creates a new account with password and sends activation email.
// Returns the created user (status=unverified).
func (a *AuthService) Register(ctx context.Context, email, password string) (*SkillMarketUser, error) {
	email = normalizeEmail(email)
	if email == "" {
		return nil, ErrEmailRequired
	}
	if len(password) < 6 {
		return nil, ErrPasswordRequired
	}
	if len(password) > 72 {
		return nil, fmt.Errorf("password too long (max 72 characters)")
	}

	// Check if already exists
	existing, err := a.store.GetUserByEmail(ctx, email)
	if err == nil {
		// If already exists and has password, reject
		hash, _ := a.store.GetPasswordHash(ctx, existing.ID)
		if hash != "" {
			if existing.Status != "verified" {
				return existing, a.sendActivationEmail(ctx, existing.ID, existing.Email)
			}
			return nil, fmt.Errorf("account already exists")
		}
		// Existing unverified account without password — set password and send activation
		hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
		if err != nil {
			return nil, err
		}
		if err := a.store.SetPasswordHash(ctx, existing.ID, string(hashed)); err != nil {
			return nil, err
		}
		if err := a.sendActivationEmail(ctx, existing.ID, existing.Email); err != nil {
			return nil, err
		}
		return existing, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	// Create new user
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	user := &SkillMarketUser{
		ID:               generateID(),
		Email:            email,
		Status:           "unverified",
		Credits:          0,
		VoucherCount:     defaultVoucherCount,
		VoucherExpiresAt: now.Add(defaultVoucherDays * 24 * time.Hour),
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := a.store.CreateUser(ctx, user); err != nil {
		return nil, err
	}
	if err := a.store.SetPasswordHash(ctx, user.ID, string(hashed)); err != nil {
		return nil, err
	}

	if err := a.sendActivationEmail(ctx, user.ID, email); err != nil {
		return nil, err
	}
	return user, nil
}

// Activate activates an account via token from activation email.
func (a *AuthService) Activate(ctx context.Context, token string) (*Session, error) {
	at, err := a.store.GetAuthToken(ctx, token)
	if err != nil {
		return nil, ErrTokenExpired
	}
	if at.TokenType != "activation" || time.Now().After(at.ExpiresAt) {
		_ = a.store.DeleteAuthToken(ctx, token)
		return nil, ErrTokenExpired
	}

	// Activate user
	if err := a.store.UpdateUserStatus(ctx, at.UserID, "verified", "email"); err != nil {
		return nil, err
	}
	_ = a.store.DeleteAuthToken(ctx, token)

	// Create session
	user, err := a.store.GetUserByID(ctx, at.UserID)
	if err != nil {
		return nil, err
	}
	return a.createSession(ctx, user)
}

// Login authenticates with email + password, returns session.
func (a *AuthService) Login(ctx context.Context, email, password string) (*Session, error) {
	email = normalizeEmail(email)
	if email == "" {
		return nil, ErrEmailRequired
	}

	user, err := a.store.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, ErrInvalidCredentials
	}
	if user.Status != "verified" {
		return nil, ErrAccountNotActive
	}

	hash, err := a.store.GetPasswordHash(ctx, user.ID)
	if err != nil || hash == "" {
		return nil, ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	return a.createSession(ctx, user)
}

// SendIdentityVerification sends a verification email for lookup.
// - verified user → identity verification link (click to auto-login)
// - unverified user → resend activation email
// - unknown email → send a registration invitation (no account created)
func (a *AuthService) SendIdentityVerification(ctx context.Context, email string) error {
	email = normalizeEmail(email)
	if email == "" {
		return ErrEmailRequired
	}

	user, err := a.store.GetUserByEmail(ctx, email)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		// User does not exist → send registration invitation (no account created).
		regLink := fmt.Sprintf("%s/skillmarket/user/", a.resolveBaseURL(ctx))
		subject := "SkillMarket Registration Invitation"
		body := fmt.Sprintf(
			"Hello,\r\n\r\nSomeone requested to log in with this email on SkillMarket, but no account exists yet.\r\n\r\nIf this was you, please register here:\r\n%s\r\n\r\nIf you did not request this, please ignore this email.\r\n",
			regLink,
		)
		return a.mailer.Send(ctx, []string{email}, subject, body)
	}

	if user.Status != "verified" {
		// Unverified account → resend activation email.
		return a.sendActivationEmail(ctx, user.ID, email)
	}

	// Verified account → send identity verification link.
	token := generateToken()
	now := time.Now()
	at := &AuthToken{
		Token:     token,
		UserID:    user.ID,
		TokenType: "identity",
		ExpiresAt: now.Add(identityTokenExpiry),
		CreatedAt: now,
	}
	if err := a.store.CreateAuthToken(ctx, at); err != nil {
		return err
	}

	link := fmt.Sprintf("%s/skillmarket/user/?verify=%s", a.resolveBaseURL(ctx), token)
	subject := "SkillMarket Identity Verification"
	body := fmt.Sprintf(
		"Hello,\r\n\r\nYou requested to access your SkillMarket account.\r\n\r\nClick the link below to verify your identity and log in:\r\n%s\r\n\r\nThis link expires in 15 minutes.\r\n\r\nIf you did not request this, please ignore this email.\r\n",
		link,
	)
	return a.mailer.Send(ctx, []string{email}, subject, body)
}

// VerifyIdentity verifies identity token and creates session.
func (a *AuthService) VerifyIdentity(ctx context.Context, token string) (*Session, error) {
	at, err := a.store.GetAuthToken(ctx, token)
	if err != nil {
		return nil, ErrTokenExpired
	}
	if at.TokenType != "identity" || time.Now().After(at.ExpiresAt) {
		_ = a.store.DeleteAuthToken(ctx, token)
		return nil, ErrTokenExpired
	}
	_ = a.store.DeleteAuthToken(ctx, token)

	user, err := a.store.GetUserByID(ctx, at.UserID)
	if err != nil {
		return nil, err
	}
	return a.createSession(ctx, user)
}

// ValidateSession checks if a session token is valid.
func (a *AuthService) ValidateSession(ctx context.Context, token string) (*Session, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, ErrTokenExpired
	}
	if revoked, err := a.store.IsSessionRevoked(ctx, token); err != nil {
		return nil, err
	} else if revoked {
		return nil, ErrTokenExpired
	}
	sess, err := a.store.GetSession(ctx, token)
	if err == nil {
		if time.Now().After(sess.ExpiresAt) {
			_ = a.store.DeleteSession(ctx, token)
			return nil, ErrTokenExpired
		}
		return sess, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	sess, err = a.validateSignedSessionToken(token)
	if err != nil {
		return nil, ErrTokenExpired
	}
	return sess, nil
}

// Logout deletes a session.
func (a *AuthService) Logout(ctx context.Context, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}
	var expiresAt time.Time
	if sess, err := a.store.GetSession(ctx, token); err == nil && sess != nil {
		expiresAt = sess.ExpiresAt
	} else if sess, err := a.validateSignedSessionToken(token); err == nil && sess != nil {
		expiresAt = sess.ExpiresAt
	}
	return a.store.DeleteAndRevokeSession(ctx, token, expiresAt)
}

// ResendActivation resends activation email for an unverified account.
func (a *AuthService) ResendActivation(ctx context.Context, email string) error {
	email = normalizeEmail(email)
	user, err := a.store.GetUserByEmail(ctx, email)
	if err != nil {
		return ErrNotFound
	}
	if user.Status == "verified" {
		return nil // already verified
	}
	return a.sendActivationEmail(ctx, user.ID, email)
}

// SendPasswordReset sends a password reset email. Returns nil even if user not found (anti-enumeration).
func (a *AuthService) SendPasswordReset(ctx context.Context, email string) error {
	email = normalizeEmail(email)
	if email == "" {
		return ErrEmailRequired
	}
	user, err := a.store.GetUserByEmail(ctx, email)
	if err != nil {
		return nil // silent — prevent enumeration
	}
	if user.Status != "verified" {
		return nil
	}
	token := generateToken()
	now := time.Now()
	at := &AuthToken{
		Token:     token,
		UserID:    user.ID,
		TokenType: "password_reset",
		ExpiresAt: now.Add(passwordResetExpiry),
		CreatedAt: now,
	}
	if err := a.store.CreateAuthToken(ctx, at); err != nil {
		return err
	}
	link := fmt.Sprintf("%s/skillmarket/user/?reset=%s", a.resolveBaseURL(ctx), token)
	subject := "SkillMarket Password Reset"
	body := fmt.Sprintf(
		"Hello,\r\n\r\nYou requested to reset your SkillMarket password.\r\n\r\nClick the link below to set a new password:\r\n%s\r\n\r\nThis link expires in 30 minutes.\r\n\r\nIf you did not request this, please ignore this email.\r\n",
		link,
	)
	return a.mailer.Send(ctx, []string{email}, subject, body)
}

// CurrentUser returns the authenticated SkillMarket user for a session token.
func (a *AuthService) CurrentUser(ctx context.Context, sessionToken string) (*SkillMarketUser, error) {
	sess, err := a.ValidateSession(ctx, sessionToken)
	if err != nil {
		return nil, err
	}
	return a.store.GetUserByID(ctx, sess.UserID)
}

// ChangePassword verifies the current password and stores a new password.
func (a *AuthService) ChangePassword(ctx context.Context, sessionToken, currentPassword, newPassword string) error {
	sess, err := a.ValidateSession(ctx, sessionToken)
	if err != nil {
		return err
	}
	if len(newPassword) < 6 {
		return ErrPasswordRequired
	}
	if len(newPassword) > 72 {
		return fmt.Errorf("password too long (max 72 characters)")
	}
	hash, err := a.store.GetPasswordHash(ctx, sess.UserID)
	if err != nil || hash == "" {
		return ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(currentPassword)); err != nil {
		return ErrCurrentPassword
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcryptCost)
	if err != nil {
		return err
	}
	return a.store.SetPasswordHash(ctx, sess.UserID, string(hashed))
}

// ResetPassword validates the reset token and sets a new password, returning a session.
func (a *AuthService) ResetPassword(ctx context.Context, token, newPassword string) (*Session, error) {
	if len(newPassword) < 6 {
		return nil, ErrPasswordRequired
	}
	if len(newPassword) > 72 {
		return nil, fmt.Errorf("password too long (max 72 characters)")
	}
	at, err := a.store.GetAuthToken(ctx, token)
	if err != nil {
		return nil, ErrTokenExpired
	}
	if at.TokenType != "password_reset" || time.Now().After(at.ExpiresAt) {
		_ = a.store.DeleteAuthToken(ctx, token)
		return nil, ErrTokenExpired
	}
	_ = a.store.DeleteAuthToken(ctx, token)

	hashed, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcryptCost)
	if err != nil {
		return nil, err
	}
	if err := a.store.SetPasswordHash(ctx, at.UserID, string(hashed)); err != nil {
		return nil, err
	}
	user, err := a.store.GetUserByID(ctx, at.UserID)
	if err != nil {
		return nil, err
	}
	return a.createSession(ctx, user)
}

// ── internal helpers ────────────────────────────────────────────────────

func (a *AuthService) sendActivationEmail(ctx context.Context, userID, email string) error {
	token := generateToken()
	now := time.Now()
	at := &AuthToken{
		Token:     token,
		UserID:    userID,
		TokenType: "activation",
		ExpiresAt: now.Add(activationTokenExpiry),
		CreatedAt: now,
	}
	if err := a.store.CreateAuthToken(ctx, at); err != nil {
		return err
	}

	link := fmt.Sprintf("%s/skillmarket/user/?activate=%s", a.resolveBaseURL(ctx), token)
	subject := "SkillMarket Account Activation"
	body := fmt.Sprintf(
		"Hello,\r\n\r\nThank you for registering on SkillMarket.\r\n\r\nClick the link below to activate your account:\r\n%s\r\n\r\nThis link expires in 24 hours.\r\n\r\nIf you did not register, please ignore this email.\r\n",
		link,
	)
	return a.mailer.Send(ctx, []string{email}, subject, body)
}

func (a *AuthService) createSession(ctx context.Context, user *SkillMarketUser) (*Session, error) {
	now := time.Now()
	expiresAt := now.Add(sessionExpiry)
	sess := &Session{
		Token:     a.newSessionToken(user, now, expiresAt),
		UserID:    user.ID,
		Email:     user.Email,
		ExpiresAt: expiresAt,
		CreatedAt: now,
	}
	if err := a.store.CreateSession(ctx, sess); err != nil {
		return nil, err
	}
	return sess, nil
}

func generateToken() string {
	var buf [32]byte
	_, _ = rand.Read(buf[:])
	return hex.EncodeToString(buf[:])
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
