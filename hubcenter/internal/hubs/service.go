package hubs

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/mail"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

var ErrHubUnauthorized = errors.New("hub unauthorized")
var ErrHubPendingConfirmation = errors.New("hub pending confirmation")
var ErrHubDisabled = errors.New("hub disabled")
var ErrEmailBlocked = errors.New("email blocked")
var ErrIPBlocked = errors.New("ip blocked")
var ErrInvalidConfirmationToken = errors.New("invalid confirmation token")

const hubConfirmationPrefix = "hub_registration_confirm:"
const systemKeyPublicBaseURL = "server_public_base_url"

type confirmationTokenRecord struct {
	TokenHash string `json:"token_hash"`
	ExpiresAt int64  `json:"expires_at"`
}

type confirmationTokenState struct {
	Tokens []confirmationTokenRecord `json:"tokens"`
}

type BlockedEmailRepository interface {
	GetByEmail(ctx context.Context, email string) (*store.BlockedEmail, error)
	Create(ctx context.Context, item *store.BlockedEmail) error
	DeleteByEmail(ctx context.Context, email string) error
	List(ctx context.Context) ([]*store.BlockedEmail, error)
}

type BlockedIPRepository interface {
	GetByIP(ctx context.Context, ip string) (*store.BlockedIP, error)
	Create(ctx context.Context, item *store.BlockedIP) error
	DeleteByIP(ctx context.Context, ip string) error
	List(ctx context.Context) ([]*store.BlockedIP, error)
}

type RegisterHubRequest struct {
	InstallationID string         `json:"installation_id"`
	OwnerEmail     string         `json:"owner_email"`
	Name           string         `json:"name"`
	Description    string         `json:"description"`
	BaseURL        string         `json:"base_url"`
	Host           string         `json:"host"`
	Port           int            `json:"port"`
	Visibility     string         `json:"visibility"`
	EnrollmentMode string         `json:"enrollment_mode"`
	Capabilities   map[string]any `json:"capabilities"`
}

type RegisterHubResult struct {
	HubID               string `json:"hub_id"`
	HubSecret           string `json:"hub_secret"`
	PendingConfirmation bool   `json:"pending_confirmation"`
	Message             string `json:"message,omitempty"`
}

type Service struct {
	hubs          store.HubRepository
	links         store.HubUserLinkRepository
	blockedEmails BlockedEmailRepository
	blockedIPs    BlockedIPRepository
	settings      store.SystemSettingsRepository
	mailer        mail.Mailer
	publicBaseURL string
}

func NewService(hubs store.HubRepository, links store.HubUserLinkRepository, blockedEmails BlockedEmailRepository, blockedIPs BlockedIPRepository, settings store.SystemSettingsRepository, mailer mail.Mailer, publicBaseURL string) *Service {
	return &Service{
		hubs:          hubs,
		links:         links,
		blockedEmails: blockedEmails,
		blockedIPs:    blockedIPs,
		settings:      settings,
		mailer:        mailer,
		publicBaseURL: strings.TrimRight(strings.TrimSpace(publicBaseURL), "/"),
	}
}

func (s *Service) RegisterHub(ctx context.Context, req RegisterHubRequest) (*RegisterHubResult, error) {
	return s.RegisterHubFromIP(ctx, req, "")
}

func (s *Service) RegisterHubFromIP(ctx context.Context, req RegisterHubRequest, clientIP string) (*RegisterHubResult, error) {
	ownerEmail := normalizeEmail(req.OwnerEmail)
	if err := s.checkEmailAllowed(ctx, ownerEmail); err != nil {
		return nil, err
	}
	if err := s.checkIPAllowed(ctx, clientIP); err != nil {
		return nil, err
	}
	if s.mailer == nil || s.settings == nil {
		return nil, fmt.Errorf("mail delivery is not configured")
	}

	now := time.Now()
	rawSecret, err := randomToken()
	if err != nil {
		return nil, err
	}
	capJSON, err := json.Marshal(req.Capabilities)
	if err != nil {
		return nil, err
	}

	installationID := strings.TrimSpace(req.InstallationID)
	if installationID != "" {
		existing, err := s.hubs.GetByInstallationID(ctx, installationID)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			if existing.IsDisabled {
				return nil, ErrHubDisabled
			}
			existing.OwnerEmail = ownerEmail
			existing.Name = strings.TrimSpace(req.Name)
			existing.Description = strings.TrimSpace(req.Description)
			existing.BaseURL = strings.TrimRight(strings.TrimSpace(req.BaseURL), "/")
			existing.Host = strings.TrimSpace(req.Host)
			existing.Port = req.Port
			existing.Visibility = normalizeVisibility(req.Visibility)
			existing.EnrollmentMode = normalizeEnrollmentMode(req.EnrollmentMode)
			existing.CapabilitiesJSON = string(capJSON)
			existing.HubSecretHash = hashToken(rawSecret)
			existing.LastSeenAt = &now
			existing.UpdatedAt = now
			if existing.IsDisabled {
				existing.Status = "disabled"
			} else {
				existing.Status = "pending_confirmation"
			}

			if err := s.hubs.UpdateRegistration(ctx, existing); err != nil {
				return nil, err
			}
			if err := s.syncOwnerLink(ctx, existing.ID, existing.OwnerEmail, now); err != nil {
				return nil, err
			}
			if err := s.sendConfirmation(ctx, existing.ID, existing.OwnerEmail, existing.Name); err != nil {
				return nil, err
			}
			return &RegisterHubResult{
				HubID:               existing.ID,
				HubSecret:           rawSecret,
				PendingConfirmation: true,
				Message:             "Hub registration confirmation sent",
			}, nil
		}
	}

	hub := &store.HubInstance{
		ID:               newID("hub"),
		InstallationID:   installationID,
		OwnerEmail:       ownerEmail,
		Name:             strings.TrimSpace(req.Name),
		Description:      strings.TrimSpace(req.Description),
		BaseURL:          strings.TrimRight(strings.TrimSpace(req.BaseURL), "/"),
		Host:             strings.TrimSpace(req.Host),
		Port:             req.Port,
		Visibility:       normalizeVisibility(req.Visibility),
		EnrollmentMode:   normalizeEnrollmentMode(req.EnrollmentMode),
		Status:           "pending_confirmation",
		IsDisabled:       false,
		DisabledReason:   "",
		CapabilitiesJSON: string(capJSON),
		HubSecretHash:    hashToken(rawSecret),
		LastSeenAt:       &now,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if err := s.hubs.Create(ctx, hub); err != nil {
		return nil, err
	}
	if err := s.syncOwnerLink(ctx, hub.ID, hub.OwnerEmail, now); err != nil {
		return nil, err
	}
	if err := s.sendConfirmation(ctx, hub.ID, hub.OwnerEmail, hub.Name); err != nil {
		return nil, err
	}

	return &RegisterHubResult{
		HubID:               hub.ID,
		HubSecret:           rawSecret,
		PendingConfirmation: true,
		Message:             "Hub registration confirmation sent",
	}, nil
}

func (s *Service) HeartbeatHub(ctx context.Context, hubID string) error {
	return s.HeartbeatHubWithSecret(ctx, hubID, "")
}

func (s *Service) HeartbeatHubWithSecret(ctx context.Context, hubID, rawSecret string) error {
	hub, err := s.hubs.GetByID(ctx, hubID)
	if err != nil {
		return err
	}
	if hub == nil {
		return ErrHubUnauthorized
	}
	if rawSecret != "" && hub.HubSecretHash != hashToken(rawSecret) {
		return ErrHubUnauthorized
	}
	if hub.Status == "pending_confirmation" {
		return ErrHubPendingConfirmation
	}
	if err := s.hubs.UpdateHeartbeat(ctx, hubID, time.Now()); err != nil {
		return err
	}
	if hub.IsDisabled || hub.Status == "disabled" {
		return ErrHubDisabled
	}
	return nil
}

func (s *Service) ConfirmRegistration(ctx context.Context, token string) error {
	hubID, secret, ok := strings.Cut(strings.TrimSpace(token), ".")
	if !ok || strings.TrimSpace(hubID) == "" || strings.TrimSpace(secret) == "" {
		return ErrInvalidConfirmationToken
	}

	hub, err := s.hubs.GetByID(ctx, hubID)
	if err != nil {
		return err
	}
	if hub == nil {
		return ErrInvalidConfirmationToken
	}

	raw, err := s.settings.Get(ctx, hubConfirmationPrefix+hubID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(raw) == "" {
		return ErrInvalidConfirmationToken
	}

	var payload confirmationTokenState
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return err
	}
	if len(payload.Tokens) == 0 {
		return ErrInvalidConfirmationToken
	}

	nowUnix := time.Now().Unix()
	matched := false
	activeTokens := make([]confirmationTokenRecord, 0, len(payload.Tokens))
	secretHash := hashToken(secret)
	for _, candidate := range payload.Tokens {
		if candidate.TokenHash == "" || candidate.ExpiresAt <= nowUnix {
			continue
		}
		activeTokens = append(activeTokens, candidate)
		if candidate.TokenHash == secretHash {
			matched = true
		}
	}
	if !matched {
		return ErrInvalidConfirmationToken
	}

	return s.confirmHubRegistration(ctx, hub, hubID)
}

func (s *Service) ConfirmHubRegistrationByAdmin(ctx context.Context, hubID string) error {
	hubID = strings.TrimSpace(hubID)
	if hubID == "" {
		return errors.New("hub id is required")
	}
	hub, err := s.hubs.GetByID(ctx, hubID)
	if err != nil {
		return err
	}
	if hub == nil {
		return ErrInvalidConfirmationToken
	}
	return s.confirmHubRegistration(ctx, hub, hubID)
}

func (s *Service) confirmHubRegistration(ctx context.Context, hub *store.HubInstance, hubID string) error {
	if hub.IsDisabled {
		hub.Status = "disabled"
	} else {
		hub.Status = "online"
	}
	hub.UpdatedAt = time.Now()
	if err := s.hubs.UpdateRegistration(ctx, hub); err != nil {
		return err
	}
	if s.settings != nil {
		if err := s.settings.Set(ctx, hubConfirmationPrefix+hubID, mustJSON(confirmationTokenState{Tokens: []confirmationTokenRecord{}})); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ListHubs(ctx context.Context) ([]*store.HubInstance, error) {
	return s.hubs.ListAll(ctx)
}

func (s *Service) UpdateVisibility(ctx context.Context, hubID, visibility string) error {
	if strings.TrimSpace(hubID) == "" {
		return errors.New("hub id is required")
	}
	return s.hubs.UpdateVisibility(ctx, strings.TrimSpace(hubID), normalizeVisibility(visibility), time.Now())
}

func (s *Service) DisableHub(ctx context.Context, hubID, reason string) error {
	return s.hubs.SetDisabled(ctx, hubID, true, strings.TrimSpace(reason), time.Now())
}

func (s *Service) EnableHub(ctx context.Context, hubID string) error {
	return s.hubs.SetDisabled(ctx, hubID, false, "", time.Now())
}

func (s *Service) DeleteHub(ctx context.Context, hubID string) error {
	if s.links != nil {
		if err := s.links.DeleteByHubID(ctx, hubID); err != nil {
			return err
		}
	}
	return s.hubs.DeleteByID(ctx, hubID)
}

func (s *Service) AddBlockedEmail(ctx context.Context, email, reason string) error {
	if s.blockedEmails == nil {
		return nil
	}
	now := time.Now()
	return s.blockedEmails.Create(ctx, &store.BlockedEmail{
		ID:        newID("be"),
		Email:     normalizeEmail(email),
		Reason:    strings.TrimSpace(reason),
		CreatedAt: now,
		UpdatedAt: now,
	})
}

func (s *Service) ListBlockedEmails(ctx context.Context) ([]*store.BlockedEmail, error) {
	if s.blockedEmails == nil {
		return nil, nil
	}
	return s.blockedEmails.List(ctx)
}

func (s *Service) RemoveBlockedEmail(ctx context.Context, email string) error {
	if s.blockedEmails == nil {
		return nil
	}
	return s.blockedEmails.DeleteByEmail(ctx, normalizeEmail(email))
}

func (s *Service) AddBlockedIP(ctx context.Context, ip, reason string) error {
	if s.blockedIPs == nil {
		return nil
	}
	now := time.Now()
	return s.blockedIPs.Create(ctx, &store.BlockedIP{
		ID:        newID("bi"),
		IP:        strings.TrimSpace(ip),
		Reason:    strings.TrimSpace(reason),
		CreatedAt: now,
		UpdatedAt: now,
	})
}

func (s *Service) ListBlockedIPs(ctx context.Context) ([]*store.BlockedIP, error) {
	if s.blockedIPs == nil {
		return nil, nil
	}
	return s.blockedIPs.List(ctx)
}

func (s *Service) RemoveBlockedIP(ctx context.Context, ip string) error {
	if s.blockedIPs == nil {
		return nil
	}
	return s.blockedIPs.DeleteByIP(ctx, strings.TrimSpace(ip))
}

func (s *Service) sendConfirmation(ctx context.Context, hubID, ownerEmail, hubName string) error {
	confirmURL, err := s.prepareConfirmation(ctx, hubID)
	if err != nil {
		return err
	}
	return s.mailer.SendHubRegistrationConfirmation(ctx, ownerEmail, confirmURL, hubName)
}

func (s *Service) prepareConfirmation(ctx context.Context, hubID string) (string, error) {
	tokenSecret, err := randomToken()
	if err != nil {
		return "", err
	}
	expiresAt := time.Now().Add(24 * time.Hour).Unix()
	state := confirmationTokenState{
		Tokens: []confirmationTokenRecord{{
			TokenHash: hashToken(tokenSecret),
			ExpiresAt: expiresAt,
		}},
	}

	if s.settings != nil {
		raw, err := s.settings.Get(ctx, hubConfirmationPrefix+hubID)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(raw) != "" {
			var existing confirmationTokenState
			if err := json.Unmarshal([]byte(raw), &existing); err == nil {
				nowUnix := time.Now().Unix()
				for _, token := range existing.Tokens {
					if token.TokenHash == "" || token.ExpiresAt <= nowUnix {
						continue
					}
					state.Tokens = append(state.Tokens, token)
				}
			}
		}
	}

	if len(state.Tokens) > 5 {
		state.Tokens = state.Tokens[:5]
	}
	if err := s.settings.Set(ctx, hubConfirmationPrefix+hubID, mustJSON(state)); err != nil {
		return "", err
	}
	baseURL, err := s.PublicBaseURL(ctx)
	if err != nil {
		return "", err
	}
	if baseURL == "" {
		baseURL = strings.TrimRight(strings.TrimSpace(s.publicBaseURL), "/")
	}
	if baseURL == "" {
		baseURL = "http://127.0.0.1:9388"
	}
	return baseURL + "/hub-registration/confirm?token=" + hubID + "." + tokenSecret, nil
}

func (s *Service) PublicBaseURL(ctx context.Context) (string, error) {
	if s.settings == nil {
		return strings.TrimRight(strings.TrimSpace(s.publicBaseURL), "/"), nil
	}
	raw, err := s.settings.Get(ctx, systemKeyPublicBaseURL)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(raw) == "" {
		return strings.TrimRight(strings.TrimSpace(s.publicBaseURL), "/"), nil
	}
	var payload struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return "", err
	}
	if strings.TrimSpace(payload.Value) == "" {
		return strings.TrimRight(strings.TrimSpace(s.publicBaseURL), "/"), nil
	}
	return strings.TrimRight(strings.TrimSpace(payload.Value), "/"), nil
}

func (s *Service) SetPublicBaseURL(ctx context.Context, publicBaseURL string) (string, error) {
	publicBaseURL = strings.TrimRight(strings.TrimSpace(publicBaseURL), "/")
	if publicBaseURL == "" {
		return "", fmt.Errorf("hub center public base url is required")
	}
	if s.settings == nil {
		s.publicBaseURL = publicBaseURL
		return publicBaseURL, nil
	}
	if err := s.settings.Set(ctx, systemKeyPublicBaseURL, mustJSON(map[string]string{"value": publicBaseURL})); err != nil {
		return "", err
	}
	return publicBaseURL, nil
}

func (s *Service) syncOwnerLink(ctx context.Context, hubID, ownerEmail string, now time.Time) error {
	if s.links == nil || ownerEmail == "" {
		return nil
	}
	if err := s.links.DeleteByHubID(ctx, hubID); err != nil {
		return err
	}
	return s.links.Create(ctx, &store.HubUserLink{
		ID:        newID("hul"),
		HubID:     hubID,
		Email:     ownerEmail,
		IsDefault: true,
		CreatedAt: now,
		UpdatedAt: now,
	})
}

func (s *Service) checkEmailAllowed(ctx context.Context, email string) error {
	if s.blockedEmails == nil || email == "" {
		return nil
	}

	blocked, err := s.blockedEmails.GetByEmail(ctx, email)
	if err != nil {
		return err
	}
	if blocked != nil {
		return ErrEmailBlocked
	}
	return nil
}

func (s *Service) checkIPAllowed(ctx context.Context, ip string) error {
	if s.blockedIPs == nil {
		return nil
	}

	ip = strings.TrimSpace(ip)
	if ip == "" {
		return nil
	}

	blocked, err := s.blockedIPs.GetByIP(ctx, ip)
	if err != nil {
		return err
	}
	if blocked != nil {
		return ErrIPBlocked
	}
	return nil
}

func newID(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
}

func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func mustJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return `{}`
	}
	return string(data)
}

func normalizeEmail(email string) string {
	return strings.TrimSpace(strings.ToLower(email))
}

func defaultIfEmpty(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
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

func normalizeVisibility(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "shared":
		return "shared"
	case "public":
		return "public"
	default:
		return "private"
	}
}
