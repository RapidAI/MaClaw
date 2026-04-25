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

	"github.com/RapidAI/CodeClaw/hubcenter/internal/diagnostics"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/mail"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

var ErrHubUnauthorized = errors.New("hub unauthorized")
var ErrHubNotReadyOnNode = errors.New("hub not ready on node")
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

type syncRecorder interface {
	SyncHubHeartbeat(ctx context.Context, hubID string)
	AppendBlockedEmail(ctx context.Context, item *store.BlockedEmail)
	DeleteBlockedEmail(ctx context.Context, email string)
	AppendBlockedIP(ctx context.Context, item *store.BlockedIP)
	DeleteBlockedIP(ctx context.Context, ip string)
	AppendHubInstance(ctx context.Context, item *store.HubInstance)
	DeleteHubInstance(ctx context.Context, hubID string)
	AppendHubDomainRoute(ctx context.Context, item *store.HubDomainRoute)
	DeleteHubDomainRoute(ctx context.Context, routeID string)
	AppendHubUserLink(ctx context.Context, item *store.HubUserLink)
	DeleteHubUserLink(ctx context.Context, linkID string)
}

type routeSnapshotRefresher interface {
	Rebuild(ctx context.Context) error
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
	InstallationID        string         `json:"installation_id"`
	OwnerEmail            string         `json:"owner_email"`
	Name                  string         `json:"name"`
	Description           string         `json:"description"`
	BaseURL               string         `json:"base_url"`
	Host                  string         `json:"host"`
	Port                  int            `json:"port"`
	Visibility            string         `json:"visibility"`
	EnrollmentMode        string         `json:"enrollment_mode"`
	CorporateEmailDomain  string         `json:"corporate_email_domain,omitempty"`
	CorporateEmailDomains []string       `json:"corporate_email_domains,omitempty"`
	AcceptPublicSignup    *bool          `json:"accept_public_signup,omitempty"`
	Capabilities          map[string]any `json:"capabilities"`
}

type RegisterHubResult struct {
	HubID               string `json:"hub_id"`
	HubSecret           string `json:"hub_secret"`
	PendingConfirmation bool   `json:"pending_confirmation"`
	Message             string `json:"message,omitempty"`
}

type HeartbeatHubUpdate struct {
	BaseURL               string
	Host                  string
	Port                  int
	Visibility            string
	EnrollmentMode        string
	CorporateEmailDomain  string
	CorporateEmailDomains []string
	AcceptPublicSignup    *bool
}

type Service struct {
	hubs          store.HubRepository
	links         store.HubUserLinkRepository
	routes        store.HubDomainRouteRepository
	blockedEmails BlockedEmailRepository
	blockedIPs    BlockedIPRepository
	settings      store.SystemSettingsRepository
	mailer        mail.Mailer
	publicBaseURL string
	sync          syncRecorder
	refresher     routeSnapshotRefresher
	recorder      *diagnostics.FailureEventRecorder
}

func NewService(hubs store.HubRepository, links store.HubUserLinkRepository, routes store.HubDomainRouteRepository, blockedEmails BlockedEmailRepository, blockedIPs BlockedIPRepository, settings store.SystemSettingsRepository, mailer mail.Mailer, publicBaseURL string) *Service {
	return &Service{
		hubs:          hubs,
		links:         links,
		routes:        routes,
		blockedEmails: blockedEmails,
		blockedIPs:    blockedIPs,
		settings:      settings,
		mailer:        mailer,
		publicBaseURL: strings.TrimRight(strings.TrimSpace(publicBaseURL), "/"),
	}
}

func (s *Service) SetSyncRecorder(recorder syncRecorder) {
	s.sync = recorder
}

func (s *Service) SetRouteSnapshotRefresher(refresher routeSnapshotRefresher) {
	s.refresher = refresher
}

func (s *Service) SetFailureEventRecorder(recorder *diagnostics.FailureEventRecorder) {
	s.recorder = recorder
}

func (s *Service) recordFailure(ctx context.Context, category, eventCode, message, entityID, email, clientIP string, details map[string]any) {
	if s == nil || s.recorder == nil {
		return
	}
	s.recorder.Record(ctx, diagnostics.FailureEventInput{
		Category:  category,
		EventCode: eventCode,
		Message:   message,
		EntityID:  entityID,
		Email:     email,
		ClientIP:  clientIP,
		Details:   details,
	})
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
	corporateEmailDomains := normalizeCorporateEmailDomains(req.CorporateEmailDomains)
	corporateEmailDomain := normalizeCorporateEmailDomain(req.CorporateEmailDomain)
	if len(corporateEmailDomains) == 0 && corporateEmailDomain != "" {
		corporateEmailDomains = []string{corporateEmailDomain}
	}
	if len(corporateEmailDomains) > 0 {
		corporateEmailDomain = corporateEmailDomains[0]
	}
	visibility := normalizeVisibility(req.Visibility)
	acceptPublicSignup := len(corporateEmailDomains) == 0 && isPublicSignupVisibility(visibility)
	if req.AcceptPublicSignup != nil {
		acceptPublicSignup = *req.AcceptPublicSignup
	}
	if installationID != "" {
		existing, err := s.hubs.GetByInstallationID(ctx, installationID)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			if existing.IsDisabled {
				return nil, ErrHubDisabled
			}

			alreadyConfirmed := existing.Status == "online"
			existing.OwnerEmail = ownerEmail
			existing.Name = strings.TrimSpace(req.Name)
			existing.Description = strings.TrimSpace(req.Description)
			existing.BaseURL = strings.TrimRight(strings.TrimSpace(req.BaseURL), "/")
			existing.Host = strings.TrimSpace(req.Host)
			existing.Port = req.Port
			existing.Visibility = visibility
			existing.EnrollmentMode = normalizeEnrollmentMode(req.EnrollmentMode)
			existing.CorporateEmailDomain = corporateEmailDomain
			existing.AcceptPublicSignup = acceptPublicSignup
			existing.CapabilitiesJSON = string(capJSON)
			existing.HubSecretHash = hashToken(rawSecret)
			existing.LastSeenAt = &now
			existing.UpdatedAt = now
			if !alreadyConfirmed {
				existing.Status = "pending_confirmation"
			}

			if err := s.hubs.UpdateRegistration(ctx, existing); err != nil {
				return nil, err
			}
			s.recordHubInstance(ctx, existing)
			if err := s.syncOwnerLink(ctx, existing.ID, existing.OwnerEmail, now); err != nil {
				return nil, err
			}
			if err := s.syncDomainRoutes(ctx, existing, corporateEmailDomains, now); err != nil {
				return nil, err
			}
			s.refreshRoutes(ctx)

			if alreadyConfirmed {
				return &RegisterHubResult{HubID: existing.ID, HubSecret: rawSecret, PendingConfirmation: false, Message: "Hub re-registered successfully, already confirmed"}, nil
			}
			if err := s.sendConfirmation(ctx, existing.ID, existing.OwnerEmail, existing.Name); err != nil {
				return nil, err
			}
			return &RegisterHubResult{HubID: existing.ID, HubSecret: rawSecret, PendingConfirmation: true, Message: "Hub registration confirmation sent"}, nil
		}
	}

	hub := &store.HubInstance{
		ID:                   newID("hub"),
		InstallationID:       installationID,
		OwnerEmail:           ownerEmail,
		Name:                 strings.TrimSpace(req.Name),
		Description:          strings.TrimSpace(req.Description),
		BaseURL:              strings.TrimRight(strings.TrimSpace(req.BaseURL), "/"),
		Host:                 strings.TrimSpace(req.Host),
		Port:                 req.Port,
		Visibility:           visibility,
		EnrollmentMode:       normalizeEnrollmentMode(req.EnrollmentMode),
		CorporateEmailDomain: corporateEmailDomain,
		AcceptPublicSignup:   acceptPublicSignup,
		Status:               "pending_confirmation",
		IsDisabled:           false,
		DisabledReason:       "",
		CapabilitiesJSON:     string(capJSON),
		HubSecretHash:        hashToken(rawSecret),
		LastSeenAt:           &now,
		CreatedAt:            now,
		UpdatedAt:            now,
	}

	if err := s.hubs.Create(ctx, hub); err != nil {
		return nil, err
	}
	s.recordHubInstance(ctx, hub)
	if err := s.syncOwnerLink(ctx, hub.ID, hub.OwnerEmail, now); err != nil {
		return nil, err
	}
	if err := s.syncDomainRoutes(ctx, hub, corporateEmailDomains, now); err != nil {
		return nil, err
	}
	s.refreshRoutes(ctx)
	if err := s.sendConfirmation(ctx, hub.ID, hub.OwnerEmail, hub.Name); err != nil {
		return nil, err
	}

	return &RegisterHubResult{HubID: hub.ID, HubSecret: rawSecret, PendingConfirmation: true, Message: "Hub registration confirmation sent"}, nil
}

func (s *Service) HeartbeatHub(ctx context.Context, hubID string) error {
	return s.HeartbeatHubWithSecret(ctx, hubID, "", nil, nil)
}

func (s *Service) HeartbeatHubWithSecret(ctx context.Context, hubID, rawSecret string, invitationCodeRequired *bool, update *HeartbeatHubUpdate) error {
	hub, err := s.hubs.GetByID(ctx, hubID)
	if err != nil {
		return err
	}
	if hub == nil {
		if s.sync != nil {
			return ErrHubNotReadyOnNode
		}
		return ErrHubUnauthorized
	}
	if rawSecret != "" && hub.HubSecretHash != hashToken(rawSecret) {
		return ErrHubUnauthorized
	}
	if hub.Status == "pending_confirmation" {
		return ErrHubPendingConfirmation
	}

	now := time.Now()
	if update != nil {
		corporateEmailDomains := normalizeCorporateEmailDomains(update.CorporateEmailDomains)
		corporateEmailDomain := normalizeCorporateEmailDomain(update.CorporateEmailDomain)
		if len(corporateEmailDomains) == 0 && corporateEmailDomain != "" {
			corporateEmailDomains = []string{corporateEmailDomain}
		}
		if len(corporateEmailDomains) > 0 {
			corporateEmailDomain = corporateEmailDomains[0]
		}
		visibility := normalizeVisibility(update.Visibility)
		acceptPublicSignup := len(corporateEmailDomains) == 0 && isPublicSignupVisibility(visibility)
		if update.AcceptPublicSignup != nil {
			acceptPublicSignup = *update.AcceptPublicSignup
		}
		hub.BaseURL = strings.TrimRight(strings.TrimSpace(update.BaseURL), "/")
		hub.Host = strings.TrimSpace(update.Host)
		hub.Port = update.Port
		hub.Visibility = visibility
		hub.EnrollmentMode = normalizeEnrollmentMode(update.EnrollmentMode)
		hub.CorporateEmailDomain = corporateEmailDomain
		hub.AcceptPublicSignup = acceptPublicSignup
		hub.LastSeenAt = &now
		hub.UpdatedAt = now
		if hub.IsDisabled || hub.Status == "disabled" {
			hub.Status = "disabled"
		} else {
			hub.Status = "online"
		}
		if err := s.hubs.UpdateRegistration(ctx, hub); err != nil {
			return err
		}
		s.recordHubInstance(ctx, hub)
		if err := s.syncDomainRoutes(ctx, hub, corporateEmailDomains, now); err != nil {
			return err
		}
	}
	if err := s.hubs.UpdateHeartbeat(ctx, hubID, now); err != nil {
		return err
	}
	if invitationCodeRequired != nil {
		if err := s.hubs.UpdateInvitationCodeRequired(ctx, hubID, *invitationCodeRequired, now); err != nil {
			return err
		}
	}
	if hub.IsDisabled || hub.Status == "disabled" {
		return ErrHubDisabled
	}
	if s.sync != nil {
		s.sync.SyncHubHeartbeat(ctx, hubID)
	}
	s.refreshRoutes(ctx)
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
	secretHash := hashToken(secret)
	for _, candidate := range payload.Tokens {
		if candidate.TokenHash == "" || candidate.ExpiresAt <= nowUnix {
			continue
		}
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
	s.recordHubInstance(ctx, hub)
	s.refreshRoutes(ctx)
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
	if err := s.hubs.UpdateVisibility(ctx, strings.TrimSpace(hubID), normalizeVisibility(visibility), time.Now()); err != nil {
		return err
	}
	if err := s.recordHubByID(ctx, hubID); err != nil {
		return err
	}
	s.refreshRoutes(ctx)
	return nil
}

func (s *Service) DisableHub(ctx context.Context, hubID, reason string) error {
	if err := s.hubs.SetDisabled(ctx, hubID, true, strings.TrimSpace(reason), time.Now()); err != nil {
		return err
	}
	if err := s.recordHubByID(ctx, hubID); err != nil {
		return err
	}
	s.refreshRoutes(ctx)
	return nil
}

func (s *Service) EnableHub(ctx context.Context, hubID string) error {
	if err := s.hubs.SetDisabled(ctx, hubID, false, "", time.Now()); err != nil {
		return err
	}
	if err := s.recordHubByID(ctx, hubID); err != nil {
		return err
	}
	s.refreshRoutes(ctx)
	return nil
}

func (s *Service) DeleteHub(ctx context.Context, hubID string) error {
	if s.links != nil {
		items, err := s.links.ListAll(ctx)
		if err != nil {
			return err
		}
		if err := s.links.DeleteByHubID(ctx, hubID); err != nil {
			return err
		}
		if s.sync != nil {
			for _, item := range items {
				if item == nil || strings.TrimSpace(item.HubID) != strings.TrimSpace(hubID) {
					continue
				}
				s.sync.DeleteHubUserLink(ctx, item.ID)
			}
		}
	}
	if s.routes != nil {
		if err := s.routes.DeleteByHubID(ctx, hubID); err != nil {
			return err
		}
	}
	if err := s.hubs.DeleteByID(ctx, hubID); err != nil {
		return err
	}
	if s.sync != nil {
		for idx := 0; idx < 16; idx++ {
			s.sync.DeleteHubDomainRoute(ctx, domainRouteID(hubID, idx))
		}
		s.sync.DeleteHubInstance(ctx, hubID)
	}
	s.refreshRoutes(ctx)
	return nil
}

func (s *Service) AddBlockedEmail(ctx context.Context, email, reason string) error {
	if s.blockedEmails == nil {
		return nil
	}
	now := time.Now()
	item := &store.BlockedEmail{ID: newID("be"), Email: normalizeEmail(email), Reason: strings.TrimSpace(reason), CreatedAt: now, UpdatedAt: now}
	if err := s.blockedEmails.Create(ctx, item); err != nil {
		return err
	}
	if s.sync != nil {
		s.sync.AppendBlockedEmail(ctx, item)
	}
	s.refreshRoutes(ctx)
	return nil
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
	normalized := normalizeEmail(email)
	if err := s.blockedEmails.DeleteByEmail(ctx, normalized); err != nil {
		return err
	}
	if s.sync != nil {
		s.sync.DeleteBlockedEmail(ctx, normalized)
	}
	s.refreshRoutes(ctx)
	return nil
}

func (s *Service) AddBlockedIP(ctx context.Context, ip, reason string) error {
	if s.blockedIPs == nil {
		return nil
	}
	now := time.Now()
	item := &store.BlockedIP{ID: newID("bi"), IP: strings.TrimSpace(ip), Reason: strings.TrimSpace(reason), CreatedAt: now, UpdatedAt: now}
	if err := s.blockedIPs.Create(ctx, item); err != nil {
		return err
	}
	if s.sync != nil {
		s.sync.AppendBlockedIP(ctx, item)
	}
	s.refreshRoutes(ctx)
	return nil
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
	normalized := strings.TrimSpace(ip)
	if err := s.blockedIPs.DeleteByIP(ctx, normalized); err != nil {
		return err
	}
	if s.sync != nil {
		s.sync.DeleteBlockedIP(ctx, normalized)
	}
	s.refreshRoutes(ctx)
	return nil
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
	state := confirmationTokenState{Tokens: []confirmationTokenRecord{{TokenHash: hashToken(tokenSecret), ExpiresAt: expiresAt}}}

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
	link := &store.HubUserLink{ID: primaryOwnerLinkID(hubID), HubID: hubID, Email: ownerEmail, IsDefault: true, CreatedAt: now, UpdatedAt: now}
	if err := s.links.Upsert(ctx, link); err != nil {
		return err
	}
	if s.sync != nil {
		s.sync.AppendHubUserLink(ctx, link)
	}
	return nil
}

func (s *Service) SyncHubUserLink(ctx context.Context, hubID, rawSecret, email string, isDefault bool) error {
	hubID = strings.TrimSpace(hubID)
	email = normalizeEmail(email)
	if hubID == "" || email == "" {
		return errors.New("hub id and email are required")
	}
	hub, err := s.hubs.GetByID(ctx, hubID)
	if err != nil {
		return err
	}
	if hub == nil {
		return ErrHubUnauthorized
	}
	if rawSecret == "" || hub.HubSecretHash != hashToken(rawSecret) {
		return ErrHubUnauthorized
	}
	if hub.Status == "pending_confirmation" {
		return ErrHubPendingConfirmation
	}
	if hub.IsDisabled || hub.Status == "disabled" {
		return ErrHubDisabled
	}
	if s.links == nil {
		return nil
	}
	items, err := s.links.ListByEmail(ctx, email)
	if err != nil {
		return err
	}
	for _, item := range items {
		if item == nil || isOwnerLink(item) {
			continue
		}
		if err := s.links.DeleteByID(ctx, item.ID); err != nil {
			return err
		}
		if s.sync != nil {
			s.sync.DeleteHubUserLink(ctx, item.ID)
		}
	}
	now := time.Now()
	link := &store.HubUserLink{ID: primaryUserLinkID(hubID, email), HubID: hubID, Email: email, IsDefault: isDefault, CreatedAt: now, UpdatedAt: now}
	if err := s.links.Upsert(ctx, link); err != nil {
		return err
	}
	if s.sync != nil {
		s.sync.AppendHubUserLink(ctx, link)
	}
	s.refreshRoutes(ctx)
	return nil
}

func (s *Service) syncDomainRoutes(ctx context.Context, hub *store.HubInstance, domains []string, now time.Time) error {
	if s.routes == nil || hub == nil {
		return nil
	}
	domains = normalizeCorporateEmailDomains(domains)
	if len(domains) == 0 {
		legacy := normalizeCorporateEmailDomain(hub.CorporateEmailDomain)
		if legacy != "" {
			domains = []string{legacy}
		}
	}
	if err := s.routes.DeleteByHubID(ctx, hub.ID); err != nil {
		return err
	}
	if s.sync != nil {
		for idx := 0; idx < 16; idx++ {
			s.sync.DeleteHubDomainRoute(ctx, domainRouteID(hub.ID, idx))
		}
	}
	for idx, domain := range domains {
		route := &store.HubDomainRoute{ID: domainRouteID(hub.ID, idx), HubID: hub.ID, Domain: domain, Enabled: true, Priority: 100 + idx, CreatedAt: now, UpdatedAt: now}
		if err := s.routes.Upsert(ctx, route); err != nil {
			return err
		}
		if s.sync != nil {
			s.sync.AppendHubDomainRoute(ctx, route)
		}
	}
	return nil
}

func (s *Service) recordHubByID(ctx context.Context, hubID string) error {
	hub, err := s.hubs.GetByID(ctx, hubID)
	if err != nil {
		return err
	}
	s.recordHubInstance(ctx, hub)
	return nil
}

func (s *Service) recordHubInstance(ctx context.Context, hub *store.HubInstance) {
	if s.sync == nil || hub == nil {
		return
	}
	s.sync.AppendHubInstance(ctx, hub)
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

func normalizeCorporateEmailDomain(v string) string {
	v = strings.TrimSpace(strings.ToLower(v))
	v = strings.TrimPrefix(v, "@")
	v = strings.TrimPrefix(v, ".")
	return strings.TrimSpace(v)
}

func normalizeCorporateEmailDomains(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		normalized := normalizeCorporateEmailDomain(value)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

func primaryOwnerLinkID(hubID string) string {
	return "hul_owner_" + strings.TrimSpace(hubID)
}

func isOwnerLink(link *store.HubUserLink) bool {
	if link == nil {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(link.ID), "hul_owner_")
}

func primaryUserLinkID(hubID, email string) string {
	return fmt.Sprintf("hul_user_%s_%s", strings.TrimSpace(hubID), hashToken(normalizeEmail(email))[:16])
}

func domainRouteID(hubID string, index int) string {
	return fmt.Sprintf("hdr_%s_%d", strings.TrimSpace(hubID), index)
}

func isPublicSignupVisibility(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "shared", "public":
		return true
	default:
		return false
	}
}

func (s *Service) refreshRoutes(ctx context.Context) {
	if s.refresher == nil {
		return
	}
	_ = s.refresher.Rebuild(ctx)
}
