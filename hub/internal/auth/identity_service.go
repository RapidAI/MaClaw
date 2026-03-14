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

	"github.com/RapidAI/CodeClaw/hub/internal/mail"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

var (
	ErrInvalidEmail           = errors.New("invalid email")
	ErrEmailBlocked           = errors.New("email blocked")
	ErrInvalidUserCredentials = errors.New("invalid user credentials")
	ErrMachineUnauthorized    = errors.New("machine unauthorized")
)

const (
	systemKeyEnrollmentMode = "identity_enrollment_mode"
)

type EnrollmentResult struct {
	Status       string `json:"status"`
	Message      string `json:"message,omitempty"`
	UserID       string `json:"user_id,omitempty"`
	Email        string `json:"email,omitempty"`
	SN           string `json:"sn,omitempty"`
	MachineID    string `json:"machine_id,omitempty"`
	MachineToken string `json:"machine_token,omitempty"`
}

type EmailLoginRequestResult struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type SystemSettingsRepository interface {
	Set(ctx context.Context, key, valueJSON string) error
	Get(ctx context.Context, key string) (string, error)
}

type MachinePrincipal struct {
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
	UserID string
	Email  string
}

type IdentityService struct {
	users           store.UserRepository
	enrollments     store.EnrollmentRepository
	blocks          store.EmailBlocklistRepository
	invites         store.EmailInviteRepository
	machines        store.MachineRepository
	viewerTok       store.ViewerTokenRepository
	loginTok        store.LoginTokenRepository
	settings        SystemSettingsRepository
	enrollmentMode  string
	allowSelfEnroll bool
	mailer          mail.Mailer
	publicBaseURL   string
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
		Name:                 defaultIfEmpty(metadata.Name, "CodeClaw Desktop"),
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
	invites store.EmailInviteRepository,
	machines store.MachineRepository,
	viewerTok store.ViewerTokenRepository,
	loginTok store.LoginTokenRepository,
	settings SystemSettingsRepository,
	enrollmentMode string,
	allowSelfEnroll bool,
	mailer mail.Mailer,
	publicBaseURL string,
) *IdentityService {
	return &IdentityService{
		users:           users,
		enrollments:     enrollments,
		blocks:          blocks,
		invites:         invites,
		machines:        machines,
		viewerTok:       viewerTok,
		loginTok:        loginTok,
		settings:        settings,
		enrollmentMode:  normalizeEnrollmentMode(enrollmentMode),
		allowSelfEnroll: allowSelfEnroll,
		mailer:          mailer,
		publicBaseURL:   strings.TrimRight(publicBaseURL, "/"),
	}
}

func (s *IdentityService) StartEnrollment(ctx context.Context, email, machineName, platform string) (*EnrollmentResult, error) {
	email = normalizeEmail(email)
	if email == "" {
		return nil, ErrInvalidEmail
	}
	if err := s.ensureEmailAllowed(ctx, email); err != nil {
		return nil, err
	}

	user, err := s.users.GetByEmail(ctx, email)
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
			return &EnrollmentResult{
				Status:  "manual_binding_required",
				Message: "This hub requires manual binding before a machine can be enrolled",
				Email:   email,
			}, nil
		case "approval":
			return s.ensurePendingApproval(ctx, email, "Awaiting administrator approval before machine enrollment")
		default:
			if !s.allowSelfEnroll {
				return &EnrollmentResult{
					Status:  "manual_binding_required",
					Message: "Self enrollment is disabled. Ask an administrator to generate an SN binding first",
					Email:   email,
				}, nil
			}
			user, err = s.createApprovedUser(ctx, email)
			if err != nil {
				return nil, err
			}
		}
	}

	return s.issueMachineForUser(ctx, user, machineName, platform)
}

func (s *IdentityService) RequestEmailLogin(ctx context.Context, email string) (*EmailLoginRequestResult, error) {
	email = normalizeEmail(email)
	if email == "" {
		return nil, ErrInvalidEmail
	}
	if err := s.ensureEmailAllowed(ctx, email); err != nil {
		return nil, err
	}

	user, err := s.users.GetByEmail(ctx, email)
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
			result, err := s.ensurePendingApproval(ctx, email, "Awaiting administrator approval before email sign-in")
			if err != nil {
				return nil, err
			}
			return &EmailLoginRequestResult{
				Status:  result.Status,
				Message: result.Message,
			}, nil
		default:
			if !s.allowSelfEnroll {
				return &EmailLoginRequestResult{
					Status:  "manual_binding_required",
					Message: "Self enrollment is disabled. Ask an administrator to generate an SN binding first",
				}, nil
			}
			user, err = s.createApprovedUser(ctx, email)
			if err != nil {
				return nil, err
			}
		}
	}

	rawToken, err := randomToken(32)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	if err := s.loginTok.Create(ctx, &store.LoginToken{
		ID:        newID("lt"),
		Email:     email,
		TokenHash: hashToken(rawToken),
		Purpose:   "login",
		ExpiresAt: now.Add(15 * time.Minute),
		CreatedAt: now,
	}); err != nil {
		return nil, err
	}

	confirmURL := s.buildConfirmURL(rawToken)
	if s.mailer != nil {
		if err := s.mailer.SendLoginConfirmation(ctx, email, confirmURL); err != nil {
			return nil, err
		}
	}

	message := "A sign-in email has been sent"
	if s.mailer == nil {
		message = fmt.Sprintf("Use this confirm URL for development: %s", confirmURL)
	}
	return &EmailLoginRequestResult{
		Status:  "pending_email_confirmation",
		Message: message,
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

	user, err := s.users.GetByEmail(ctx, loginToken.Email)
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
		UserID:    user.ID,
		TokenHash: hashToken(rawViewerToken),
		ExpiresAt: now.Add(24 * time.Hour),
		CreatedAt: now,
	}); err != nil {
		return "", nil, err
	}
	if err := s.loginTok.Consume(ctx, loginToken.ID, now); err != nil {
		return "", nil, err
	}

	return rawViewerToken, user, nil
}

func (s *IdentityService) ManualBind(ctx context.Context, email string) (*store.User, error) {
	email = normalizeEmail(email)
	if email == "" {
		return nil, ErrInvalidEmail
	}
	if err := s.ensureEmailAllowed(ctx, email); err != nil {
		return nil, err
	}
	user, err := s.users.GetByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	if user != nil {
		return user, nil
	}
	return s.createApprovedUser(ctx, email)
}

func (s *IdentityService) LookupUserByEmail(ctx context.Context, email string) (*store.User, error) {
	email = normalizeEmail(email)
	if email == "" {
		return nil, ErrInvalidEmail
	}
	return s.users.GetByEmail(ctx, email)
}

func (s *IdentityService) IsEmailBlocked(ctx context.Context, email string) (bool, error) {
	email = normalizeEmail(email)
	if email == "" {
		return false, ErrInvalidEmail
	}
	if s.blocks == nil {
		return false, nil
	}
	item, err := s.blocks.GetByEmail(ctx, email)
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
	return s.blocks.List(ctx)
}

func (s *IdentityService) RemoveBlockedEmail(ctx context.Context, email string) error {
	email = normalizeEmail(email)
	if email == "" {
		return ErrInvalidEmail
	}
	if s.blocks == nil {
		return nil
	}
	return s.blocks.DeleteByEmail(ctx, email)
}

func (s *IdentityService) AddInvite(ctx context.Context, email, role string) error {
	email = normalizeEmail(email)
	if email == "" {
		return ErrInvalidEmail
	}
	if role == "" {
		role = "viewer"
	}
	if s.invites == nil {
		return nil
	}
	now := time.Now()
	return s.invites.Create(ctx, &store.EmailInvite{
		ID:        newID("inv"),
		Email:     email,
		Role:      role,
		Status:    "active",
		CreatedAt: now,
		UpdatedAt: now,
	})
}

func (s *IdentityService) ListInvites(ctx context.Context) ([]*store.EmailInvite, error) {
	if s.invites == nil {
		return []*store.EmailInvite{}, nil
	}
	return s.invites.List(ctx)
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
	return &MachinePrincipal{UserID: machine.UserID, MachineID: machine.ID}, nil
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

	return &ViewerPrincipal{UserID: user.ID, Email: user.Email}, nil
}

func (s *IdentityService) createApprovedUser(ctx context.Context, email string) (*store.User, error) {
	now := time.Now()
	user := &store.User{
		ID:               newID("u"),
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
	return user, nil
}

func (s *IdentityService) ensurePendingApproval(ctx context.Context, email string, message string) (*EnrollmentResult, error) {
	pending, err := s.enrollments.GetPendingByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	if pending == nil {
		if err := s.enrollments.Create(ctx, &store.UserEnrollment{
			ID:        newID("enr"),
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

func (s *IdentityService) issueMachineForUser(ctx context.Context, user *store.User, machineName, platform string) (*EnrollmentResult, error) {
	rawToken, err := randomToken(32)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	machine := &store.Machine{
		ID:               newID("m"),
		UserID:           user.ID,
		Name:             defaultIfEmpty(machineName, "CodeClaw Desktop"),
		Platform:         defaultIfEmpty(platform, "unknown"),
		MachineTokenHash: hashToken(rawToken),
		Status:           "offline",
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := s.machines.Create(ctx, machine); err != nil {
		return nil, err
	}
	return &EnrollmentResult{
		Status:       "approved",
		UserID:       user.ID,
		Email:        user.Email,
		SN:           user.SN,
		MachineID:    machine.ID,
		MachineToken: rawToken,
	}, nil
}

func (s *IdentityService) ensureEmailAllowed(ctx context.Context, email string) error {
	if s.blocks == nil {
		return nil
	}
	item, err := s.blocks.GetByEmail(ctx, email)
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
	base := s.publicBaseURL
	if base == "" {
		base = "http://127.0.0.1:9399"
	}
	return fmt.Sprintf("%s/app/auth/confirm?token=%s", base, url.QueryEscape(rawToken))
}
