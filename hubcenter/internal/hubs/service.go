package hubs

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/diagnostics"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/mail"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
	"golang.org/x/text/width"
)

var ErrHubUnauthorized = errors.New("hub unauthorized")
var ErrHubNotReadyOnNode = errors.New("hub not ready on node")
var ErrHubPendingConfirmation = errors.New("hub pending confirmation")
var ErrHubDisabled = errors.New("hub disabled")
var ErrEmailBlocked = errors.New("email blocked")
var ErrIPBlocked = errors.New("ip blocked")
var ErrInvalidConfirmationToken = errors.New("invalid confirmation token")
var ErrHubNotFound = errors.New("hub not found")
var ErrDigitalEmployeeQuotaDecrease = errors.New("digital employee quota cannot decrease")
var ErrDigitalEmployeeQuotaRequired = errors.New("digital employee quota must be greater than zero when enabled")
var ErrDigitalEmployeeYearsRequired = errors.New("digital employee authorization years must be at least one when enabled")

const hubConfirmationPrefix = "hub_registration_confirm:"
const (
	systemKeyPublicBaseURL                       = "server_public_base_url"
	systemKeyTenantDigitalEmployeeAuthorizations = "tenant_digital_employee_authorizations"
)
const adminDomainRoutePrefix = "hdr_admin_"
const adminUserLinkPrefix = "hul_admin_"
const hubUserMigrationResponseBodyLimit = 128 << 20

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

type hubUserLinkScopedDeleter interface {
	DeleteByHubTenantEmail(ctx context.Context, hubID, tenantID, email string) ([]*store.HubUserLink, error)
}

type routeSnapshotRefresher interface {
	Rebuild(ctx context.Context) error
}

type transactionalUserMigrator interface {
	MigrateEmailToHub(ctx context.Context, email, fromHubID, sourceTenantID string, link *store.HubUserLink) ([]*store.HubUserLink, *store.HubUserLink, error)
}

type transactionalUserPatternMigrator interface {
	MigrateEmailPatternToHub(ctx context.Context, pattern, fromHubID, sourceTenantID, toHubID, targetTenantID string, now time.Time) ([]*store.HubUserLink, []*store.HubUserLink, error)
}

type transactionalDomainMigrator interface {
	MigrateDomainToHub(ctx context.Context, domain, fromHubID, sourceTenantID string, route *store.HubDomainRoute) ([]*store.HubDomainRoute, error)
}

type transactionalDomainUserMigrator interface {
	MigrateDomainAndEmailPatternToHub(ctx context.Context, domain, pattern, fromHubID, sourceTenantID, toHubID, targetTenantID string, route *store.HubDomainRoute, now time.Time) ([]*store.HubDomainRoute, []*store.HubUserLink, []*store.HubUserLink, error)
}

type hubUserLinkByHubLister interface {
	ListByHubID(ctx context.Context, hubID string) ([]*store.HubUserLink, error)
}

type hubDomainRouteByHubLister interface {
	ListByHubID(ctx context.Context, hubID string) ([]*store.HubDomainRoute, error)
}

type hubDomainRouteByDomainLister interface {
	ListEnabledByDomain(ctx context.Context, domain string) ([]*store.HubDomainRoute, error)
}

type hubUserInventoryRefreshCandidateLister interface {
	ListUserInventoryRefreshCandidates(ctx context.Context) ([]*store.HubInstance, error)
}

type hubUserCountByTenantLister interface {
	ListUserCountsByHubTenant(ctx context.Context) ([]store.HubTenantUserCount, error)
}

type hubUserFirstSeenLister interface {
	ListUserFirstSeen(ctx context.Context) ([]store.HubUserFirstSeen, error)
}

type hubUserMigrationSourceLinkLister interface {
	ListMigrationSourceLinks(ctx context.Context, pattern, fromHubID, sourceTenantID, excludeHubID string) ([]*store.HubUserLink, error)
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
	Capabilities          map[string]any
}

type MigrateUserRequest struct {
	Email          string `json:"email"`
	TenantID       string `json:"tenant_id,omitempty"`
	SourceTenantID string `json:"source_tenant_id,omitempty"`
	TargetTenantID string `json:"target_tenant_id,omitempty"`
	FromHubID      string `json:"from_hub_id,omitempty"`
	ToHubID        string `json:"to_hub_id"`
}

type MigrateDomainRequest struct {
	Domain         string `json:"domain"`
	TenantID       string `json:"tenant_id,omitempty"`
	SourceTenantID string `json:"source_tenant_id,omitempty"`
	TargetTenantID string `json:"target_tenant_id,omitempty"`
	FromHubID      string `json:"from_hub_id,omitempty"`
	ToHubID        string `json:"to_hub_id"`
}

type MigrationResult struct {
	Mode           string   `json:"mode"`
	Email          string   `json:"email,omitempty"`
	Domain         string   `json:"domain,omitempty"`
	ToHubID        string   `json:"to_hub_id"`
	SourceTenantID string   `json:"source_tenant_id,omitempty"`
	TargetTenantID string   `json:"target_tenant_id,omitempty"`
	RemovedIDs     []string `json:"removed_ids,omitempty"`
	UpsertedIDs    []string `json:"upserted_ids,omitempty"`
}

type userMigrationSource struct {
	HubID    string
	TenantID string
	Emails   []string
}

type migrationTenantPair struct {
	SourceTenantID string
	TargetTenantID string
}

type remoteUserMigrationRequest struct {
	HubSecretHash string            `json:"hub_secret_hash"`
	TenantID      string            `json:"tenant_id,omitempty"`
	Emails        []string          `json:"emails,omitempty"`
	Users         []json.RawMessage `json:"users,omitempty"`
}

type remoteUserMigrationExportResponse struct {
	TenantID string            `json:"tenant_id,omitempty"`
	Users    []json.RawMessage `json:"users"`
}

type remoteUserDataPackage struct {
	TenantID string `json:"tenant_id,omitempty"`
	User     *struct {
		TenantID string `json:"tenant_id,omitempty"`
		Email    string `json:"email"`
	} `json:"user,omitempty"`
}

type RefreshUserInventoryResult struct {
	HubsRefreshed int      `json:"hubs_refreshed"`
	UsersIndexed  int      `json:"users_indexed"`
	HubsFailed    int      `json:"hubs_failed"`
	Errors        []string `json:"errors,omitempty"`
}
type HubUserDashboardItem struct {
	HubID                 string     `json:"hub_id"`
	TenantID              string     `json:"tenant_id,omitempty"`
	TenantName            string     `json:"tenant_name,omitempty"`
	HubName               string     `json:"hub_name"`
	BaseURL               string     `json:"base_url"`
	Status                string     `json:"status"`
	IsDisabled            bool       `json:"is_disabled"`
	UserCount             int        `json:"user_count"`
	MachineCount          int        `json:"machine_count"`
	CorporateEmailDomain  string     `json:"corporate_email_domain"`
	CorporateEmailDomains []string   `json:"corporate_email_domains,omitempty"`
	AcceptPublicSignup    bool       `json:"accept_public_signup"`
	SignupMode            string     `json:"signup_mode"`
	LastSeenAt            *time.Time `json:"last_seen_at,omitempty"`
}

type UserRegistrationBucket struct {
	Period string `json:"period"`
	Count  int    `json:"count"`
}

type UserRegistrationHubReport struct {
	HubID        string                   `json:"hub_id"`
	TenantID     string                   `json:"tenant_id,omitempty"`
	TenantName   string                   `json:"tenant_name,omitempty"`
	HubName      string                   `json:"hub_name"`
	BaseURL      string                   `json:"base_url"`
	TotalUsers   int                      `json:"total_users"`
	DailyTotal   int                      `json:"daily_total"`
	MonthlyTotal int                      `json:"monthly_total"`
	Daily        []UserRegistrationBucket `json:"daily"`
	Monthly      []UserRegistrationBucket `json:"monthly"`
}

type UserRegistrationReport struct {
	TotalUsers   int                         `json:"total_users"`
	DailyTotal   int                         `json:"daily_total"`
	MonthlyTotal int                         `json:"monthly_total"`
	Daily        []UserRegistrationBucket    `json:"daily"`
	Monthly      []UserRegistrationBucket    `json:"monthly"`
	Hubs         []UserRegistrationHubReport `json:"hubs"`
}

type Service struct {
	registrationMu sync.Mutex

	hubs          store.HubRepository
	links         store.HubUserLinkRepository
	routes        store.HubDomainRouteRepository
	blockedEmails BlockedEmailRepository
	blockedIPs    BlockedIPRepository
	settings      store.SystemSettingsRepository
	mailer        mail.Mailer
	publicBaseURL string
	client        *http.Client
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
		client:        &http.Client{Timeout: 30 * time.Second},
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
	tenantID := ""
	if raw, ok := details["tenant_id"]; ok {
		tenantID = strings.TrimSpace(fmt.Sprint(raw))
	}
	s.recorder.Record(ctx, diagnostics.FailureEventInput{
		TenantID:  tenantID,
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
	locked := false
	unlockRegistration := func() {
		if locked {
			locked = false
			s.registrationMu.Unlock()
		}
	}
	s.registrationMu.Lock()
	locked = true
	defer unlockRegistration()

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
	acceptPublicSignup := explicitPublicSignupOrDefault(req.AcceptPublicSignup)

	completeExistingRegistration := func(existing *store.HubInstance) (*RegisterHubResult, error) {
		result, err := s.updateRegisteredHub(ctx, existing, req, ownerEmail, rawSecret, string(capJSON), corporateEmailDomains, corporateEmailDomain, visibility, acceptPublicSignup, now)
		if err != nil {
			return nil, err
		}
		if !result.PendingConfirmation {
			unlockRegistration()
			return result, nil
		}
		confirmURL, err := s.prepareConfirmation(ctx, existing.ID)
		if err != nil {
			return nil, err
		}
		unlockRegistration()
		if err := s.mailer.SendHubRegistrationConfirmation(ctx, existing.OwnerEmail, confirmURL, existing.Name); err != nil {
			return nil, err
		}
		return result, nil
	}

	if installationID != "" {
		existing, err := s.hubs.GetByInstallationID(ctx, installationID)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			return completeExistingRegistration(existing)
		}
	}
	if existing, err := s.findHubByRegistrationEndpoint(ctx, req); err != nil {
		return nil, err
	} else if existing != nil {
		return completeExistingRegistration(existing)
	}

	hub := &store.HubInstance{
		ID:                     newID("hub"),
		InstallationID:         installationID,
		HubOrigin:              "self_hosted",
		DefaultSignupScope:     "domain_restricted",
		OwnerEmail:             ownerEmail,
		Name:                   strings.TrimSpace(req.Name),
		Description:            strings.TrimSpace(req.Description),
		BaseURL:                normalizeHubBaseURL(req.BaseURL),
		Host:                   normalizeHubHost(req.Host),
		Port:                   req.Port,
		Visibility:             visibility,
		EnrollmentMode:         normalizeEnrollmentMode(req.EnrollmentMode),
		CorporateEmailDomain:   corporateEmailDomain,
		AcceptPublicSignup:     acceptPublicSignup,
		Status:                 "pending_confirmation",
		IsDisabled:             false,
		DisabledReason:         "",
		CapabilitiesJSON:       string(capJSON),
		RegistrationPolicyJSON: "{}",
		HubSecretHash:          hashToken(rawSecret),
		LastSeenAt:             &now,
		CreatedAt:              now,
		UpdatedAt:              now,
	}

	if err := s.hubs.Create(ctx, hub); err != nil {
		if isUniqueConstraintError(err) {
			if existing, lookupErr := s.findHubByRegistrationIdentity(ctx, installationID, req); lookupErr != nil {
				return nil, lookupErr
			} else if existing != nil {
				return completeExistingRegistration(existing)
			}
		}
		return nil, err
	}
	s.recordHubInstance(ctx, hub)
	if err := s.syncOwnerLink(ctx, hub.ID, hub.OwnerEmail, now); err != nil {
		return nil, err
	}
	if err := s.syncDomainRoutes(ctx, hub, corporateEmailDomains, now); err != nil {
		return nil, err
	}
	if err := s.syncHubUserEmailInventoryFromCapabilities(ctx, hub.ID, req.Capabilities, now); err != nil {
		return nil, err
	}
	if err := s.ensureDefaultHubRegistrationPolicy(ctx, hub.ID); err != nil {
		return nil, err
	}
	s.refreshRoutes(ctx)
	confirmURL, err := s.prepareConfirmation(ctx, hub.ID)
	if err != nil {
		return nil, err
	}
	unlockRegistration()
	if err := s.mailer.SendHubRegistrationConfirmation(ctx, hub.OwnerEmail, confirmURL, hub.Name); err != nil {
		return nil, err
	}

	return &RegisterHubResult{HubID: hub.ID, HubSecret: rawSecret, PendingConfirmation: true, Message: "Hub registration confirmation sent"}, nil
}

func (s *Service) findHubByRegistrationIdentity(ctx context.Context, installationID string, req RegisterHubRequest) (*store.HubInstance, error) {

	if installationID != "" {
		existing, err := s.hubs.GetByInstallationID(ctx, installationID)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			return existing, nil
		}
	}
	return s.findHubByRegistrationEndpoint(ctx, req)
}

func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint failed")
}

func (s *Service) updateRegisteredHub(ctx context.Context, existing *store.HubInstance, req RegisterHubRequest, ownerEmail, rawSecret, capabilitiesJSON string, corporateEmailDomains []string, corporateEmailDomain, visibility string, acceptPublicSignup bool, now time.Time) (*RegisterHubResult, error) {
	if existing.IsDisabled {
		return nil, ErrHubDisabled
	}

	alreadyConfirmed := existing.Status == "online"
	installationID := strings.TrimSpace(req.InstallationID)

	if installationID != "" {
		existing.InstallationID = installationID
	}
	existing.OwnerEmail = ownerEmail
	existing.Name = strings.TrimSpace(req.Name)
	existing.Description = strings.TrimSpace(req.Description)
	existing.BaseURL = normalizeHubBaseURL(req.BaseURL)
	existing.Host = normalizeHubHost(req.Host)
	existing.Port = req.Port
	existing.Visibility = visibility
	existing.EnrollmentMode = normalizeEnrollmentMode(req.EnrollmentMode)
	existing.CorporateEmailDomain = corporateEmailDomain
	if req.AcceptPublicSignup != nil {
		existing.AcceptPublicSignup = acceptPublicSignup
	}
	existing.CapabilitiesJSON = capabilitiesJSON
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
	if err := s.syncHubUserEmailInventoryFromCapabilities(ctx, existing.ID, req.Capabilities, now); err != nil {
		return nil, err
	}
	if err := s.ensureDefaultHubRegistrationPolicy(ctx, existing.ID); err != nil {
		return nil, err
	}
	s.refreshRoutes(ctx)

	if alreadyConfirmed {
		return &RegisterHubResult{HubID: existing.ID, HubSecret: rawSecret, PendingConfirmation: false, Message: "Hub re-registered successfully, already confirmed"}, nil
	}
	return &RegisterHubResult{HubID: existing.ID, HubSecret: rawSecret, PendingConfirmation: true, Message: "Hub registration confirmation sent"}, nil
}

func (s *Service) findHubByRegistrationEndpoint(ctx context.Context, req RegisterHubRequest) (*store.HubInstance, error) {
	host := normalizeHubHost(req.Host)
	baseURL := normalizeHubBaseURL(req.BaseURL)
	if host == "" && baseURL == "" {
		return nil, nil
	}
	return s.hubs.GetByEndpoint(ctx, host, req.Port, baseURL)
}

func normalizeHubBaseURL(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return ""
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return baseURL
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	return parsed.String()
}

func normalizeHubHost(host string) string {
	return strings.ToLower(strings.TrimSpace(host))
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
		acceptPublicSignup := hub.AcceptPublicSignup
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
		if update.Capabilities != nil {
			capJSON, err := json.Marshal(update.Capabilities)
			if err != nil {
				return err
			}
			hub.CapabilitiesJSON = string(capJSON)
		}
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
		if update.Capabilities != nil {
			if err := s.syncHubUserEmailInventoryFromCapabilities(ctx, hub.ID, update.Capabilities, now); err != nil {
				return err
			}
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
	if err := s.ensureDefaultHubRegistrationPolicy(ctx, hubID); err != nil {
		return err
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

func (s *Service) ListUserDashboard(ctx context.Context) ([]HubUserDashboardItem, error) {
	hubItems, err := s.hubs.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	userCounts := map[string]map[string]int{}
	if s.links != nil {
		userCounts, err = s.dashboardUserCounts(ctx)
		if err != nil {
			return nil, err
		}
	}
	out := make([]HubUserDashboardItem, 0, len(hubItems))
	for _, hub := range hubItems {
		if hub == nil {
			continue
		}
		caps := hubCapabilities(hub)
		domains := hubCorporateDomains(hub)
		signupMode := "restricted"
		if len(domains) > 0 {
			signupMode = "corporate_domain"
		} else if hub.AcceptPublicSignup {
			signupMode = "public_signup"
		}
		userCount := hubUserCountFromCapabilities(caps, hubUserCountFallback(userCounts, hub.ID, ""))
		machineCount := hubMachineCountFromCapabilities(caps)
		out = append(out, HubUserDashboardItem{
			HubID:                 hub.ID,
			HubName:               hub.Name,
			BaseURL:               hub.BaseURL,
			Status:                hub.Status,
			IsDisabled:            hub.IsDisabled,
			UserCount:             userCount,
			MachineCount:          machineCount,
			CorporateEmailDomain:  hub.CorporateEmailDomain,
			CorporateEmailDomains: domains,
			AcceptPublicSignup:    hub.AcceptPublicSignup,
			SignupMode:            signupMode,
			LastSeenAt:            hub.LastSeenAt,
		})
		for _, tenantID := range dashboardTenantIDs(caps, userCounts[hub.ID]) {
			if tenantID == "" {
				continue
			}
			tenantName := tenantDashboardName(caps, tenantID)
			tenantDomains := tenantDashboardDomains(caps, tenantID)
			tenantSignupMode := "restricted"
			if len(tenantDomains) > 0 {
				tenantSignupMode = "corporate_domain"
			} else if hub.AcceptPublicSignup {
				tenantSignupMode = "public_signup"
			}
			out = append(out, HubUserDashboardItem{
				HubID:                 hub.ID,
				TenantID:              tenantID,
				TenantName:            tenantName,
				HubName:               hub.Name,
				BaseURL:               hub.BaseURL,
				Status:                hub.Status,
				IsDisabled:            hub.IsDisabled,
				UserCount:             tenantUserCountFromCapabilities(caps, tenantID, hubUserCountFallback(userCounts, hub.ID, tenantID)),
				MachineCount:          tenantMachineCountFromCapabilities(caps, tenantID),
				CorporateEmailDomain:  firstString(tenantDomains),
				CorporateEmailDomains: tenantDomains,
				AcceptPublicSignup:    hub.AcceptPublicSignup,
				SignupMode:            tenantSignupMode,
				LastSeenAt:            hub.LastSeenAt,
			})
		}
	}
	return out, nil
}

func (s *Service) dashboardUserCounts(ctx context.Context) (map[string]map[string]int, error) {
	if s == nil || s.links == nil {
		return map[string]map[string]int{}, nil
	}
	if lister, ok := s.links.(hubUserCountByTenantLister); ok {
		rows, err := lister.ListUserCountsByHubTenant(ctx)
		if err != nil {
			return nil, err
		}
		counts := map[string]map[string]int{}
		for _, row := range rows {
			hubID := strings.TrimSpace(row.HubID)
			if hubID == "" || row.Count <= 0 {
				continue
			}
			tenantID := normalizeHubSyncTenantID(row.TenantID)
			if row.AllTenants {
				tenantID = ""
			}
			if counts[hubID] == nil {
				counts[hubID] = map[string]int{}
			}
			counts[hubID][tenantID] = row.Count
		}
		return counts, nil
	}
	links, err := s.links.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	seen := map[string]map[string]struct{}{}
	for _, link := range links {
		if link == nil || strings.TrimSpace(link.HubID) == "" || strings.TrimSpace(link.Email) == "" {
			continue
		}
		hubID := strings.TrimSpace(link.HubID)
		tenantID := normalizeHubSyncTenantID(link.TenantID)
		if seen[hubID] == nil {
			seen[hubID] = map[string]struct{}{}
		}
		seen[hubID][tenantEmailCountKey(tenantID, normalizeEmail(link.Email))] = struct{}{}
	}
	counts := map[string]map[string]int{}
	for hubID, items := range seen {
		counts[hubID] = map[string]int{}
		uniqueAll := map[string]struct{}{}
		for key := range items {
			tenantID, email := splitTenantEmailCountKey(key)
			if email == "" {
				continue
			}
			uniqueAll[email] = struct{}{}
			counts[hubID][tenantID]++
		}
		counts[hubID][""] = len(uniqueAll)
	}
	return counts, nil
}

func (s *Service) UserRegistrationReport(ctx context.Context) (UserRegistrationReport, error) {
	var report UserRegistrationReport
	hubItems, err := s.hubs.ListAll(ctx)
	if err != nil {
		return report, err
	}
	hubsByID := make(map[string]*store.HubInstance, len(hubItems))
	for _, hub := range hubItems {
		if hub == nil || strings.TrimSpace(hub.ID) == "" {
			continue
		}
		hubsByID[hub.ID] = hub
	}
	if s.links == nil {
		report.Hubs = buildUserRegistrationHubReports(hubItems, nil, nil, time.Now())
		return report, nil
	}
	firstSeen, hubFirstSeen, hubTenantFirstSeen, err := s.userRegistrationFirstSeen(ctx)
	if err != nil {
		return report, err
	}
	report.TotalUsers = len(firstSeen)
	now := time.Now()
	today := startOfDay(now)
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)
	dailyCounts := map[string]int{}
	monthlyCounts := map[string]int{}
	for _, createdAt := range firstSeen {
		local := createdAt.Local()
		dayKey := local.Format("2006-01-02")
		monthKey := local.Format("2006-01")
		dailyCounts[dayKey]++
		monthlyCounts[monthKey]++
		if !local.Before(today) {
			report.DailyTotal++
		}
		if !local.Before(monthStart) {
			report.MonthlyTotal++
		}
	}
	for i := 6; i >= 0; i-- {
		day := today.AddDate(0, 0, -i).Format("2006-01-02")
		report.Daily = append(report.Daily, UserRegistrationBucket{Period: day, Count: dailyCounts[day]})
	}
	for i := 5; i >= 0; i-- {
		month := monthStart.AddDate(0, -i, 0).Format("2006-01")
		report.Monthly = append(report.Monthly, UserRegistrationBucket{Period: month, Count: monthlyCounts[month]})
	}
	report.Hubs = buildUserRegistrationHubReports(hubItems, hubFirstSeen, hubTenantFirstSeen, now)
	for hubID := range hubFirstSeen {
		if _, ok := hubsByID[hubID]; ok {
			continue
		}
		hub := &store.HubInstance{ID: hubID}
		report.Hubs = append(report.Hubs, buildUserRegistrationHubReport(hub, "", hubFirstSeen[hubID], now))
		report.Hubs = append(report.Hubs, buildTenantUserRegistrationHubReports(hub, hubTenantFirstSeen[hubID], now)...)
	}
	return report, nil
}

func (s *Service) userRegistrationFirstSeen(ctx context.Context) (map[string]time.Time, map[string]map[string]time.Time, map[string]map[string]map[string]time.Time, error) {
	firstSeen := map[string]time.Time{}
	hubFirstSeen := map[string]map[string]time.Time{}
	hubTenantFirstSeen := map[string]map[string]map[string]time.Time{}
	if lister, ok := s.links.(hubUserFirstSeenLister); ok {
		rows, err := lister.ListUserFirstSeen(ctx)
		if err != nil {
			return nil, nil, nil, err
		}
		for _, row := range rows {
			addUserRegistrationFirstSeen(firstSeen, hubFirstSeen, hubTenantFirstSeen, row.HubID, row.TenantID, row.Email, row.FirstSeen)
		}
		return firstSeen, hubFirstSeen, hubTenantFirstSeen, nil
	}
	links, err := s.links.ListAll(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	for _, link := range links {
		if link == nil {
			continue
		}
		createdAt := link.CreatedAt
		if createdAt.IsZero() {
			createdAt = link.UpdatedAt
		}
		addUserRegistrationFirstSeen(firstSeen, hubFirstSeen, hubTenantFirstSeen, link.HubID, link.TenantID, link.Email, createdAt)
	}
	return firstSeen, hubFirstSeen, hubTenantFirstSeen, nil
}

func addUserRegistrationFirstSeen(firstSeen map[string]time.Time, hubFirstSeen map[string]map[string]time.Time, hubTenantFirstSeen map[string]map[string]map[string]time.Time, hubID, tenantID, email string, createdAt time.Time) {
	hubID = strings.TrimSpace(hubID)
	tenantID = normalizeHubSyncTenantID(tenantID)
	email = normalizeEmail(email)
	if hubID == "" || email == "" || strings.Contains(email, "*") || createdAt.IsZero() {
		return
	}
	userKey := tenantEmailCountKey(tenantID, email)
	if existing, ok := firstSeen[userKey]; !ok || createdAt.Before(existing) {
		firstSeen[userKey] = createdAt
	}
	if hubFirstSeen[hubID] == nil {
		hubFirstSeen[hubID] = map[string]time.Time{}
	}
	if existing, ok := hubFirstSeen[hubID][userKey]; !ok || createdAt.Before(existing) {
		hubFirstSeen[hubID][userKey] = createdAt
	}
	if tenantID == "" {
		return
	}
	if hubTenantFirstSeen[hubID] == nil {
		hubTenantFirstSeen[hubID] = map[string]map[string]time.Time{}
	}
	if hubTenantFirstSeen[hubID][tenantID] == nil {
		hubTenantFirstSeen[hubID][tenantID] = map[string]time.Time{}
	}
	if existing, ok := hubTenantFirstSeen[hubID][tenantID][email]; !ok || createdAt.Before(existing) {
		hubTenantFirstSeen[hubID][tenantID][email] = createdAt
	}
}

func buildUserRegistrationHubReports(hubs []*store.HubInstance, hubFirstSeen map[string]map[string]time.Time, hubTenantFirstSeen map[string]map[string]map[string]time.Time, now time.Time) []UserRegistrationHubReport {
	out := make([]UserRegistrationHubReport, 0, len(hubs))
	for _, hub := range hubs {
		if hub == nil || strings.TrimSpace(hub.ID) == "" {
			continue
		}
		out = append(out, buildUserRegistrationHubReport(hub, "", hubFirstSeen[hub.ID], now))
		out = append(out, buildTenantUserRegistrationHubReports(hub, hubTenantFirstSeen[hub.ID], now)...)
	}
	return out
}

func buildTenantUserRegistrationHubReports(hub *store.HubInstance, tenants map[string]map[string]time.Time, now time.Time) []UserRegistrationHubReport {
	seen := map[string]struct{}{}
	for tenantID := range tenants {
		if tenantID = strings.TrimSpace(tenantID); tenantID != "" {
			seen[tenantID] = struct{}{}
		}
	}
	for _, tenantID := range dashboardTenantIDs(hubCapabilities(hub), nil) {
		if tenantID = strings.TrimSpace(tenantID); tenantID != "" {
			seen[tenantID] = struct{}{}
		}
	}
	tenantIDs := make([]string, 0, len(seen))
	for tenantID := range seen {
		tenantIDs = append(tenantIDs, tenantID)
	}
	sort.Strings(tenantIDs)
	out := make([]UserRegistrationHubReport, 0, len(tenantIDs))
	for _, tenantID := range tenantIDs {
		out = append(out, buildUserRegistrationHubReport(hub, tenantID, tenants[tenantID], now))
	}
	return out
}

func buildUserRegistrationHubReport(hub *store.HubInstance, tenantID string, firstSeen map[string]time.Time, now time.Time) UserRegistrationHubReport {
	report := UserRegistrationHubReport{}
	if hub != nil {
		report.HubID = hub.ID
		report.TenantID = strings.TrimSpace(tenantID)
		report.TenantName = tenantDashboardName(hubCapabilities(hub), report.TenantID)
		report.HubName = hub.Name
		report.BaseURL = hub.BaseURL
	}
	today := startOfDay(now)
	monthStart := time.Date(now.Local().Year(), now.Local().Month(), 1, 0, 0, 0, 0, time.Local)
	dailyCounts := map[string]int{}
	monthlyCounts := map[string]int{}
	for _, createdAt := range firstSeen {
		local := createdAt.Local()
		dailyCounts[local.Format("2006-01-02")]++
		monthlyCounts[local.Format("2006-01")]++
		if !local.Before(today) {
			report.DailyTotal++
		}
		if !local.Before(monthStart) {
			report.MonthlyTotal++
		}
	}
	report.TotalUsers = len(firstSeen)
	for i := 6; i >= 0; i-- {
		day := today.AddDate(0, 0, -i).Format("2006-01-02")
		report.Daily = append(report.Daily, UserRegistrationBucket{Period: day, Count: dailyCounts[day]})
	}
	for i := 5; i >= 0; i-- {
		month := monthStart.AddDate(0, -i, 0).Format("2006-01")
		report.Monthly = append(report.Monthly, UserRegistrationBucket{Period: month, Count: monthlyCounts[month]})
	}
	return report
}

func (s *Service) MigrateUser(ctx context.Context, req MigrateUserRequest) (*MigrationResult, error) {
	email := normalizeEmailPattern(req.Email)
	sourceTenantID, tenantID := resolveMigrationTenants(req.TenantID, req.SourceTenantID, req.TargetTenantID)
	toHubID := strings.TrimSpace(req.ToHubID)
	fromHubID := strings.TrimSpace(req.FromHubID)
	if email == "" || toHubID == "" {
		return nil, errors.New("email and target hub id are required")
	}
	if !isValidEmailMigrationPattern(email) {
		return nil, errors.New("email must be an address, @domain, or wildcard pattern like ma*@qianxin.com")
	}
	if err := s.ensureTargetHub(ctx, toHubID); err != nil {
		return nil, err
	}

	if s.links == nil {
		return &MigrationResult{Mode: "email", Email: email, ToHubID: toHubID}, nil
	}
	if isEmailMigrationPattern(email) {
		return s.migrateUserPattern(ctx, email, fromHubID, toHubID, sourceTenantID, tenantID)
	}
	sources, err := s.collectUserMigrationSources(ctx, email, fromHubID, toHubID, sourceTenantID)
	if err != nil {
		return nil, err
	}
	cleanupLocalUsers, err := s.prepareLocalUserMigration(ctx, sources, toHubID, tenantID)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	var removedLinks []*store.HubUserLink
	var upsertedLinks []*store.HubUserLink
	if migrator, ok := s.links.(transactionalUserMigrator); ok {
		for _, pair := range migrationTenantPairs(sources, sourceTenantID, tenantID) {
			targetTenantID := pair.TargetTenantID
			link := &store.HubUserLink{ID: adminUserLinkIDForTenant(targetTenantID, email), HubID: toHubID, TenantID: targetTenantID, Email: email, IsDefault: targetTenantID == "", CreatedAt: now, UpdatedAt: now}
			removed, upserted, err := migrator.MigrateEmailToHub(ctx, email, fromHubID, pair.SourceTenantID, link)
			if err != nil {
				return nil, err
			}
			removedLinks = append(removedLinks, removed...)
			if upserted != nil {
				upsertedLinks = append(upsertedLinks, upserted)
			}
		}
	} else {
		return nil, errors.New("transactional user migration is not supported by this store")
	}
	removed := make([]string, 0, len(removedLinks))
	for _, item := range removedLinks {
		if item != nil {
			removed = append(removed, item.ID)
		}
	}
	if s.sync != nil {
		for _, item := range removedLinks {
			if item != nil {
				s.sync.DeleteHubUserLink(ctx, item.ID)
			}
		}
		for _, item := range upsertedLinks {
			if item != nil {
				s.sync.AppendHubUserLink(ctx, item)
			}
		}
	}
	upserted := []string{}
	for _, item := range upsertedLinks {
		if item != nil {
			upserted = append(upserted, item.ID)
		}
	}
	s.refreshRoutes(ctx)
	if err := cleanupLocalUsers(ctx); err != nil {
		return nil, err
	}
	return &MigrationResult{Mode: "email", Email: email, ToHubID: toHubID, SourceTenantID: sourceTenantID, TargetTenantID: tenantID, RemovedIDs: removed, UpsertedIDs: upserted}, nil
}

func (s *Service) migrateUserPattern(ctx context.Context, pattern, fromHubID, toHubID, sourceTenantID, targetTenantID string) (*MigrationResult, error) {
	sources, err := s.collectUserMigrationSources(ctx, pattern, fromHubID, toHubID, sourceTenantID)
	if err != nil {
		return nil, err
	}
	cleanupLocalUsers, err := s.prepareLocalUserMigration(ctx, sources, toHubID, targetTenantID)
	if err != nil {
		return nil, err
	}
	migrator, ok := s.links.(transactionalUserPatternMigrator)
	if !ok {
		return nil, errors.New("transactional user pattern migration is not supported by this store")
	}
	removedLinks, upsertedLinks, err := migrator.MigrateEmailPatternToHub(ctx, pattern, fromHubID, sourceTenantID, toHubID, targetTenantID, time.Now())
	if err != nil {
		return nil, err
	}
	removed := make([]string, 0, len(removedLinks))
	for _, item := range removedLinks {
		if item != nil {
			removed = append(removed, item.ID)
		}
	}
	upserted := make([]string, 0, len(upsertedLinks))
	for _, item := range upsertedLinks {
		if item != nil {
			upserted = append(upserted, item.ID)
		}
	}
	if s.sync != nil {
		for _, item := range removedLinks {
			if item != nil {
				s.sync.DeleteHubUserLink(ctx, item.ID)
			}
		}
		for _, item := range upsertedLinks {
			if item != nil {
				s.sync.AppendHubUserLink(ctx, item)
			}
		}
	}
	s.refreshRoutes(ctx)
	if err := cleanupLocalUsers(ctx); err != nil {
		return nil, err
	}
	return &MigrationResult{Mode: "email", Email: pattern, ToHubID: toHubID, SourceTenantID: sourceTenantID, TargetTenantID: targetTenantID, RemovedIDs: removed, UpsertedIDs: upserted}, nil
}

func (s *Service) MigrateDomain(ctx context.Context, req MigrateDomainRequest) (*MigrationResult, error) {
	domain := normalizeCorporateEmailDomain(req.Domain)
	sourceTenantID, tenantID := resolveMigrationTenants(req.TenantID, req.SourceTenantID, req.TargetTenantID)
	if tenantID == "" && sourceTenantID != "" {
		tenantID = sourceTenantID
	}
	toHubID := strings.TrimSpace(req.ToHubID)
	fromHubID := strings.TrimSpace(req.FromHubID)
	if domain == "" || toHubID == "" {
		return nil, errors.New("domain and target hub id are required")
	}
	if err := s.ensureTargetHub(ctx, toHubID); err != nil {
		return nil, err
	}
	if s.routes == nil {
		return &MigrationResult{Mode: "domain", Domain: domain, ToHubID: toHubID}, nil
	}
	if tenantID != "" {
		sources, err := s.collectUserMigrationSources(ctx, "*@"+domain, fromHubID, toHubID, sourceTenantID)
		if err != nil {
			return nil, err
		}
		cleanupLocalUsers, err := s.prepareLocalUserMigration(ctx, sources, toHubID, tenantID)
		if err != nil {
			return nil, err
		}
		result, err := s.migrateTenantDomain(ctx, domain, sourceTenantID, tenantID, fromHubID, toHubID)
		if err != nil {
			return nil, err
		}
		if err := cleanupLocalUsers(ctx); err != nil {
			return nil, err
		}
		result.SourceTenantID = sourceTenantID
		result.TargetTenantID = tenantID
		return result, nil
	}
	sources, err := s.collectUserMigrationSources(ctx, "*@"+domain, fromHubID, toHubID, sourceTenantID)
	if err != nil {
		return nil, err
	}
	cleanupLocalUsers, err := s.prepareLocalUserMigration(ctx, sources, toHubID, "")
	if err != nil {
		return nil, err
	}

	adminRouteID := adminDomainRouteID(domain)
	now := time.Now()
	route := &store.HubDomainRoute{ID: adminRouteID, HubID: toHubID, Domain: domain, Enabled: true, Priority: 0, CreatedAt: now, UpdatedAt: now}

	var removedRoutes []*store.HubDomainRoute
	var removedLinks []*store.HubUserLink
	var upsertedLinks []*store.HubUserLink
	if migrator, ok := s.routes.(transactionalDomainUserMigrator); ok {
		var err error
		removedRoutes, removedLinks, upsertedLinks, err = migrator.MigrateDomainAndEmailPatternToHub(ctx, domain, "*@"+domain, fromHubID, sourceTenantID, toHubID, "", route, now)
		if err != nil {
			return nil, err
		}
	} else {
		return nil, errors.New("transactional domain user migration is not supported by this store")
	}

	removed := make([]string, 0, len(removedRoutes)+len(removedLinks))
	for _, item := range removedRoutes {
		if item != nil {
			removed = append(removed, item.ID)
		}
	}
	for _, item := range removedLinks {
		if item != nil {
			removed = append(removed, item.ID)
		}
	}
	upserted := []string{route.ID}
	for _, item := range upsertedLinks {
		if item != nil {
			upserted = append(upserted, item.ID)
		}
	}
	if s.sync != nil {
		for _, item := range removedRoutes {
			if item != nil {
				s.sync.DeleteHubDomainRoute(ctx, item.ID)
			}
		}
		s.sync.AppendHubDomainRoute(ctx, route)
		for _, item := range removedLinks {
			if item != nil {
				s.sync.DeleteHubUserLink(ctx, item.ID)
			}
		}
		for _, item := range upsertedLinks {
			if item != nil {
				s.sync.AppendHubUserLink(ctx, item)
			}
		}
	}
	s.refreshRoutes(ctx)
	if err := cleanupLocalUsers(ctx); err != nil {
		return nil, err
	}
	return &MigrationResult{Mode: "domain", Domain: domain, ToHubID: toHubID, RemovedIDs: removed, UpsertedIDs: upserted}, nil
}

func (s *Service) migrateTenantDomain(ctx context.Context, domain, sourceTenantID, tenantID, fromHubID, toHubID string) (*MigrationResult, error) {
	now := time.Now()
	route := &store.HubDomainRoute{ID: adminTenantDomainRouteID(tenantID, domain), HubID: toHubID, TenantID: tenantID, Domain: domain, Enabled: true, Priority: 0, CreatedAt: now, UpdatedAt: now}
	removedRoutes := []*store.HubDomainRoute{}
	removedLinks := []*store.HubUserLink{}
	upsertedLinks := []*store.HubUserLink{}
	if migrator, ok := s.routes.(transactionalDomainUserMigrator); ok {
		var err error
		removedRoutes, removedLinks, upsertedLinks, err = migrator.MigrateDomainAndEmailPatternToHub(ctx, domain, "*@"+domain, fromHubID, sourceTenantID, toHubID, tenantID, route, now)
		if err != nil {
			return nil, err
		}
	} else if migrator, ok := s.routes.(transactionalDomainMigrator); ok {
		var err error
		removedRoutes, err = migrator.MigrateDomainToHub(ctx, domain, fromHubID, sourceTenantID, route)
		if err != nil {
			return nil, err
		}
	} else {
		return nil, errors.New("transactional domain migration is not supported by this store")
	}
	removed := make([]string, 0, len(removedRoutes)+len(removedLinks))
	for _, item := range removedRoutes {
		if item != nil {
			removed = append(removed, item.ID)
		}
	}
	for _, item := range removedLinks {
		if item != nil {
			removed = append(removed, item.ID)
		}
	}
	upserted := []string{route.ID}
	for _, item := range upsertedLinks {
		if item != nil {
			upserted = append(upserted, item.ID)
		}
	}
	if s.sync != nil {
		for _, item := range removedRoutes {
			if item != nil {
				s.sync.DeleteHubDomainRoute(ctx, item.ID)
			}
		}
		s.sync.AppendHubDomainRoute(ctx, route)
		for _, item := range removedLinks {
			if item != nil {
				s.sync.DeleteHubUserLink(ctx, item.ID)
			}
		}
		for _, item := range upsertedLinks {
			if item != nil {
				s.sync.AppendHubUserLink(ctx, item)
			}
		}
	}
	s.refreshRoutes(ctx)
	return &MigrationResult{Mode: "domain", Domain: domain, ToHubID: toHubID, RemovedIDs: removed, UpsertedIDs: upserted}, nil
}

func (s *Service) RefreshUserInventory(ctx context.Context) (RefreshUserInventoryResult, error) {
	var result RefreshUserInventoryResult
	if s.hubs == nil {
		return result, nil
	}
	hubItems, err := s.listUserInventoryRefreshCandidates(ctx)
	if err != nil {
		return result, err
	}
	now := time.Now()
	for _, hub := range hubItems {
		if !hubCanAttemptDirectUserMigration(hub) {
			continue
		}
		tenantEmails, err := s.exportHubUserInventoryByTenant(ctx, hub)
		if err != nil {
			result.HubsFailed++
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", hub.ID, err))
			continue
		}
		if err := s.syncHubTenantUserEmailInventory(ctx, hub.ID, tenantEmails, now); err != nil {
			result.HubsFailed++
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", hub.ID, err))
			continue
		}
		if err := s.updateHubUserEmailCapabilities(ctx, hub, tenantEmails, now); err != nil {
			result.HubsFailed++
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", hub.ID, err))
			continue
		}
		result.HubsRefreshed++
		for _, emails := range tenantEmails {
			result.UsersIndexed += len(emails)
		}
	}
	s.refreshRoutes(ctx)
	return result, nil
}

func (s *Service) listUserInventoryRefreshCandidates(ctx context.Context) ([]*store.HubInstance, error) {
	if s == nil || s.hubs == nil {
		return nil, nil
	}
	if lister, ok := s.hubs.(hubUserInventoryRefreshCandidateLister); ok {
		return lister.ListUserInventoryRefreshCandidates(ctx)
	}
	hubItems, err := s.hubs.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*store.HubInstance, 0, len(hubItems))
	for _, hub := range hubItems {
		if hubCanAttemptDirectUserMigration(hub) {
			out = append(out, hub)
		}
	}
	return out, nil
}

func (s *Service) exportHubUserInventoryByTenant(ctx context.Context, hub *store.HubInstance) (map[string][]string, error) {
	tenantIDs := refreshInventoryTenantIDs(hubCapabilities(hub))
	out := map[string][]string{}
	for _, tenantID := range tenantIDs {
		var exported remoteUserMigrationExportResponse
		if err := s.callHubUserMigration(ctx, hub, "/api/center/user-migration/export", remoteUserMigrationRequest{HubSecretHash: hub.HubSecretHash, TenantID: tenantID}, &exported); err != nil {
			return nil, err
		}
		responseTenantID := normalizeHubSyncTenantID(exported.TenantID)
		if responseTenantID == "" {
			responseTenantID = tenantID
		}
		for _, raw := range exported.Users {
			var pkg remoteUserDataPackage
			if err := json.Unmarshal(raw, &pkg); err != nil || pkg.User == nil {
				continue
			}
			email := normalizeEmail(pkg.User.Email)
			if email == "" {
				continue
			}
			pkgTenantID := normalizeHubSyncTenantID(pkg.TenantID)
			if pkgTenantID == "" {
				pkgTenantID = normalizeHubSyncTenantID(pkg.User.TenantID)
			}
			if pkgTenantID == "" {
				pkgTenantID = responseTenantID
			}
			out[pkgTenantID] = append(out[pkgTenantID], email)
		}
		if _, ok := out[responseTenantID]; !ok {
			out[responseTenantID] = nil
		}
	}
	for tenantID, emails := range out {
		out[tenantID] = uniqueSortedEmails(emails)
	}
	return out, nil
}

func refreshInventoryTenantIDs(caps map[string]any) []string {
	seen := map[string]struct{}{}
	if len(tenantStringListCapabilityMap(caps["tenant_user_emails"], nil, true)) > 0 {
		for tenantID := range tenantUserEmailCapabilityMap(caps) {
			seen[normalizeHubSyncTenantID(tenantID)] = struct{}{}
		}
	}
	if len(capabilityStringList(caps["user_emails"])) > 0 {
		seen[""] = struct{}{}
	}
	for _, tenantID := range dashboardTenantIDs(caps, nil) {
		seen[normalizeHubSyncTenantID(tenantID)] = struct{}{}
	}
	if len(seen) == 0 {
		return []string{""}
	}
	out := make([]string, 0, len(seen))
	for tenantID := range seen {
		out = append(out, tenantID)
	}
	sort.Strings(out)
	return out
}

func uniqueSortedEmails(emails []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(emails))
	for _, rawEmail := range emails {
		email := normalizeEmail(rawEmail)
		if email == "" {
			continue
		}
		if _, ok := seen[email]; ok {
			continue
		}
		seen[email] = struct{}{}
		out = append(out, email)
	}
	sort.Strings(out)
	return out
}

func (s *Service) updateHubUserEmailCapabilities(ctx context.Context, hub *store.HubInstance, tenantEmails map[string][]string, now time.Time) error {
	if hub == nil {
		return nil
	}
	caps := map[string]any{}
	if strings.TrimSpace(hub.CapabilitiesJSON) != "" {
		_ = json.Unmarshal([]byte(hub.CapabilitiesJSON), &caps)
	}
	if len(tenantEmails) == 1 {
		if emails, ok := tenantEmails[""]; ok && len(tenantStringListCapabilityMap(caps["tenant_user_emails"], nil, true)) == 0 {
			return s.updateHubUserEmailCapability(ctx, hub, emails, now)
		}
	}
	byTenant := tenantUserEmailCapabilityMap(caps)
	for tenantID, emails := range tenantEmails {
		byTenant[strings.TrimSpace(tenantID)] = uniqueSortedEmails(emails)
	}
	counts := map[string]int{}
	flatEmails := make([]string, 0)
	for tenantID, values := range byTenant {
		emails := uniqueSortedEmails(values)
		byTenant[tenantID] = emails
		counts[tenantID] = len(emails)
		flatEmails = append(flatEmails, emails...)
	}
	caps["tenant_user_emails"] = byTenant
	caps["tenant_user_counts"] = counts
	caps["user_emails"] = uniqueSortedEmails(flatEmails)
	caps["user_count"] = len(caps["user_emails"].([]string))
	caps["supports_user_data_migration"] = true
	data, err := json.Marshal(caps)
	if err != nil {
		return err
	}
	copy := *hub
	copy.CapabilitiesJSON = string(data)
	copy.UpdatedAt = now
	if err := s.hubs.UpdateRegistration(ctx, &copy); err != nil {
		return err
	}
	s.recordHubInstance(ctx, &copy)
	return nil
}

func (s *Service) updateHubUserEmailCapability(ctx context.Context, hub *store.HubInstance, emails []string, now time.Time) error {
	return s.updateHubTenantUserEmailCapability(ctx, hub, "", emails, now)
}

func (s *Service) updateHubTenantUserEmailCapability(ctx context.Context, hub *store.HubInstance, tenantID string, emails []string, now time.Time) error {
	if hub == nil {
		return nil
	}
	tenantID = strings.TrimSpace(tenantID)
	caps := map[string]any{}
	if strings.TrimSpace(hub.CapabilitiesJSON) != "" {
		_ = json.Unmarshal([]byte(hub.CapabilitiesJSON), &caps)
	}
	normalized := make([]string, 0, len(emails))
	seen := map[string]struct{}{}
	for _, rawEmail := range emails {
		email := normalizeEmail(rawEmail)
		if email == "" {
			continue
		}
		if _, ok := seen[email]; ok {
			continue
		}
		seen[email] = struct{}{}
		normalized = append(normalized, email)
	}
	if len(tenantStringListCapabilityMap(caps["tenant_user_emails"], nil, true)) > 0 {
		byTenant := tenantUserEmailCapabilityMap(caps)
		byTenant[tenantID] = normalized
		caps["tenant_user_emails"] = byTenant
		counts := map[string]int{}
		for key, values := range byTenant {
			counts[key] = len(values)
		}
		caps["tenant_user_counts"] = counts
	} else {
		caps["user_emails"] = normalized
		caps["user_count"] = len(normalized)
	}
	caps["supports_user_data_migration"] = true
	data, err := json.Marshal(caps)
	if err != nil {
		return err
	}
	copy := *hub
	copy.CapabilitiesJSON = string(data)
	copy.UpdatedAt = now
	if err := s.hubs.UpdateRegistration(ctx, &copy); err != nil {
		return err
	}
	s.recordHubInstance(ctx, &copy)
	return nil
}

func (s *Service) removeHubUserEmailInventory(ctx context.Context, hub *store.HubInstance, tenantID string, removedEmails []string, now time.Time) error {
	if hub == nil || len(removedEmails) == 0 {
		return nil
	}
	tenantID = strings.TrimSpace(tenantID)
	removed := map[string]struct{}{}
	for _, rawEmail := range removedEmails {
		if email := normalizeEmail(rawEmail); email != "" {
			removed[email] = struct{}{}
		}
	}
	if len(removed) == 0 {
		return nil
	}
	caps := map[string]any{}
	if strings.TrimSpace(hub.CapabilitiesJSON) != "" {
		_ = json.Unmarshal([]byte(hub.CapabilitiesJSON), &caps)
	}
	if len(tenantStringListCapabilityMap(caps["tenant_user_emails"], nil, true)) > 0 {
		byTenant := tenantUserEmailCapabilityMap(caps)
		remaining := make([]string, 0)
		for _, rawEmail := range byTenant[tenantID] {
			email := normalizeEmail(rawEmail)
			if email == "" {
				continue
			}
			if _, ok := removed[email]; ok {
				continue
			}
			remaining = append(remaining, email)
		}
		if err := s.updateHubTenantUserEmailCapability(ctx, hub, tenantID, remaining, now); err != nil {
			return err
		}
		return s.syncHubTenantUserEmailInventory(ctx, hub.ID, map[string][]string{tenantID: remaining}, now)
	}
	remaining := make([]string, 0)
	for _, rawEmail := range capabilityStringList(caps["user_emails"]) {
		email := normalizeEmail(rawEmail)
		if email == "" {
			continue
		}
		if _, ok := removed[email]; ok {
			continue
		}
		remaining = append(remaining, email)
	}
	if err := s.updateHubUserEmailCapability(ctx, hub, remaining, now); err != nil {
		return err
	}
	return s.rebuildHubUserEmailInventory(ctx, hub.ID, remaining, now)
}

func (s *Service) prepareLocalUserMigration(ctx context.Context, sources []userMigrationSource, toHubID, targetTenantID string) (func(context.Context) error, error) {
	if len(sources) == 0 {
		return func(context.Context) error { return nil }, nil
	}
	toHub, err := s.hubs.GetByID(ctx, toHubID)
	if err != nil {
		return nil, err
	}
	if toHub == nil {
		return nil, ErrHubNotFound
	}
	packages := make([]json.RawMessage, 0)
	for _, source := range sources {
		sourceHub, err := s.hubs.GetByID(ctx, source.HubID)
		if err != nil {
			return nil, err
		}
		if sourceHub == nil || !hubSupportsDirectUserMigration(sourceHub) {
			continue
		}
		var resp remoteUserMigrationExportResponse
		if err := s.callHubUserMigration(ctx, sourceHub, "/api/center/user-migration/export", remoteUserMigrationRequest{HubSecretHash: sourceHub.HubSecretHash, TenantID: source.TenantID, Emails: source.Emails}, &resp); err != nil {
			return nil, err
		}
		packages = append(packages, resp.Users...)
	}
	if len(packages) == 0 {
		return func(context.Context) error { return nil }, nil
	}
	if !hubSupportsDirectUserMigration(toHub) {
		return nil, errors.New("target hub does not have migration endpoint credentials")
	}
	if err := s.callHubUserMigration(ctx, toHub, "/api/center/user-migration/import", remoteUserMigrationRequest{HubSecretHash: toHub.HubSecretHash, TenantID: targetTenantID, Users: packages}, nil); err != nil {
		return nil, err
	}
	cleanup := func(cleanupCtx context.Context) error {
		for _, source := range sources {
			sourceHub, err := s.hubs.GetByID(cleanupCtx, source.HubID)
			if err != nil {
				return err
			}
			if sourceHub == nil || !hubSupportsDirectUserMigration(sourceHub) {
				continue
			}
			if err := s.callHubUserMigration(cleanupCtx, sourceHub, "/api/center/user-migration/delete", remoteUserMigrationRequest{HubSecretHash: sourceHub.HubSecretHash, TenantID: source.TenantID, Emails: source.Emails}, nil); err != nil {
				return err
			}
			if err := s.removeHubUserEmailInventory(cleanupCtx, sourceHub, source.TenantID, source.Emails, time.Now()); err != nil {
				return err
			}
		}
		return nil
	}
	return cleanup, nil
}

func hubCanAttemptDirectUserMigration(hub *store.HubInstance) bool {
	if hub == nil || strings.TrimSpace(hub.BaseURL) == "" || strings.TrimSpace(hub.HubSecretHash) == "" {
		return false
	}
	baseURL := strings.ToLower(strings.TrimSpace(hub.BaseURL))
	return strings.HasPrefix(baseURL, "http://") || strings.HasPrefix(baseURL, "https://")
}

func hubSupportsDirectUserMigration(hub *store.HubInstance) bool {
	if !hubCanAttemptDirectUserMigration(hub) {
		return false
	}
	var caps map[string]any
	if strings.TrimSpace(hub.CapabilitiesJSON) == "" || json.Unmarshal([]byte(hub.CapabilitiesJSON), &caps) != nil {
		return false
	}
	for _, key := range []string{"supports_user_data_migration", "supportsUserDataMigration"} {
		if value, ok := caps[key].(bool); ok && value {
			return true
		}
	}
	return false
}

func (s *Service) callHubUserMigration(ctx context.Context, hub *store.HubInstance, path string, payload remoteUserMigrationRequest, out any) error {
	if s.client == nil {
		s.client = &http.Client{Timeout: 30 * time.Second}
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	url := strings.TrimRight(strings.TrimSpace(hub.BaseURL), "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := readLimitedHubResponse(resp.Body, hubUserMigrationResponseBodyLimit)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("hub user migration call %s failed with status %d: %s", url, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return err
		}
	}
	return nil
}

func readLimitedHubResponse(r io.Reader, limit int64) ([]byte, error) {
	if limit <= 0 {
		limit = hubUserMigrationResponseBodyLimit
	}
	body, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("hub response body exceeds %d bytes", limit)
	}
	return body, nil
}

func (s *Service) collectUserMigrationSources(ctx context.Context, pattern, fromHubID, toHubID, sourceTenantID string) ([]userMigrationSource, error) {
	out := map[string]*userMigrationSource{}
	order := []string{}
	seen := map[string]struct{}{}
	add := func(hubID, tenantID, email string) {
		hubID = strings.TrimSpace(hubID)
		tenantID = normalizeHubSyncTenantID(tenantID)
		email = normalizeEmail(email)
		if hubID == "" || email == "" || strings.TrimSpace(hubID) == strings.TrimSpace(toHubID) {
			return
		}
		if strings.TrimSpace(fromHubID) != "" && strings.TrimSpace(hubID) != strings.TrimSpace(fromHubID) {
			return
		}
		if normalizeHubSyncTenantID(sourceTenantID) != "" && tenantID != normalizeHubSyncTenantID(sourceTenantID) {
			return
		}
		if !wildcardEmailMatch(pattern, email) {
			return
		}
		key := hubID + "|" + tenantID + "|" + email
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		groupKey := hubID + "|" + tenantID
		group := out[groupKey]
		if group == nil {
			group = &userMigrationSource{HubID: hubID, TenantID: tenantID}
			out[groupKey] = group
			order = append(order, groupKey)
		}
		group.Emails = append(group.Emails, email)
	}

	if s.links != nil {
		links, err := listUserMigrationSourceLinksForPattern(ctx, s.links, pattern, fromHubID, sourceTenantID, toHubID)
		if err != nil {
			return nil, err
		}
		for _, link := range links {
			if link == nil || isOwnerLink(link) || isAdminUserLink(link) {
				continue
			}
			add(link.HubID, link.TenantID, link.Email)
		}
	}

	if s.hubs != nil {
		hubs, err := listUserMigrationSourceHubs(ctx, s.hubs, fromHubID)
		if err != nil {
			return nil, err
		}
		for _, hub := range hubs {
			if hub == nil || strings.TrimSpace(hub.CapabilitiesJSON) == "" {
				continue
			}
			var caps map[string]any
			if err := json.Unmarshal([]byte(hub.CapabilitiesJSON), &caps); err != nil {
				continue
			}
			for tenantID, emails := range tenantUserEmailCapabilityMap(caps) {
				for _, email := range emails {
					add(hub.ID, tenantID, email)
				}
			}
		}
	}
	items := make([]userMigrationSource, 0, len(order))
	for _, key := range order {
		if source := out[key]; source != nil && len(source.Emails) > 0 {
			items = append(items, *source)
		}
	}
	return items, nil
}

func listUserMigrationSourceLinks(ctx context.Context, repo store.HubUserLinkRepository, fromHubID string) ([]*store.HubUserLink, error) {
	return listUserMigrationSourceLinksForPattern(ctx, repo, "*", fromHubID, "", "")
}

func listUserMigrationSourceLinksForPattern(ctx context.Context, repo store.HubUserLinkRepository, pattern, fromHubID, sourceTenantID, excludeHubID string) ([]*store.HubUserLink, error) {
	if lister, ok := repo.(hubUserMigrationSourceLinkLister); ok {
		return lister.ListMigrationSourceLinks(ctx, pattern, fromHubID, sourceTenantID, excludeHubID)
	}
	fromHubID = strings.TrimSpace(fromHubID)
	if fromHubID != "" {
		return listHubUserLinksByHub(ctx, repo, fromHubID)
	}
	return repo.ListAll(ctx)
}

func listUserMigrationSourceHubs(ctx context.Context, repo store.HubRepository, fromHubID string) ([]*store.HubInstance, error) {
	fromHubID = strings.TrimSpace(fromHubID)
	if fromHubID != "" {
		hub, err := repo.GetByID(ctx, fromHubID)
		if err != nil || hub == nil {
			return nil, err
		}
		return []*store.HubInstance{hub}, nil
	}
	return repo.ListAll(ctx)
}

func resolveMigrationTenants(legacyTenantID, sourceTenantID, targetTenantID string) (string, string) {
	legacyTenantID = normalizeHubSyncTenantID(legacyTenantID)
	sourceTenantID = normalizeHubSyncTenantID(sourceTenantID)
	targetTenantID = normalizeHubSyncTenantID(targetTenantID)
	if targetTenantID == "" {
		targetTenantID = legacyTenantID
	}
	if sourceTenantID == "" && legacyTenantID != "" {
		sourceTenantID = legacyTenantID
	}
	return sourceTenantID, targetTenantID
}

func migrationTenantPairs(sources []userMigrationSource, requestedSourceTenantID, requestedTargetTenantID string) []migrationTenantPair {
	requestedSourceTenantID = normalizeHubSyncTenantID(requestedSourceTenantID)
	requestedTargetTenantID = normalizeHubSyncTenantID(requestedTargetTenantID)
	if requestedTargetTenantID != "" {
		sourceTenantIDs := migrationSourceTenantIDs(sources)
		if len(sourceTenantIDs) == 0 {
			sourceTenantIDs = []string{requestedSourceTenantID}
		}
		out := make([]migrationTenantPair, 0, len(sourceTenantIDs))
		for _, sourceTenantID := range sourceTenantIDs {
			out = append(out, migrationTenantPair{SourceTenantID: sourceTenantID, TargetTenantID: requestedTargetTenantID})
		}
		return out
	}
	sourceTenantIDs := migrationSourceTenantIDs(sources)
	if len(sourceTenantIDs) == 0 {
		sourceTenantIDs = []string{requestedSourceTenantID}
	}
	out := make([]migrationTenantPair, 0, len(sourceTenantIDs))
	for _, sourceTenantID := range sourceTenantIDs {
		out = append(out, migrationTenantPair{SourceTenantID: sourceTenantID, TargetTenantID: sourceTenantID})
	}
	return out
}

func migrationSourceTenantIDs(sources []userMigrationSource) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0)
	for _, source := range sources {
		tenantID := normalizeHubSyncTenantID(source.TenantID)
		if _, ok := seen[tenantID]; ok {
			continue
		}
		seen[tenantID] = struct{}{}
		out = append(out, tenantID)
	}
	return out
}

func (s *Service) ensureTargetHub(ctx context.Context, hubID string) error {
	hub, err := s.hubs.GetByID(ctx, strings.TrimSpace(hubID))
	if err != nil {
		return err
	}
	if hub == nil {
		return ErrHubNotFound
	}
	if hub.IsDisabled || hub.Status == "disabled" {
		return ErrHubDisabled
	}
	return nil
}

type DigitalEmployeeAuthorizationUpdate struct {
	TenantID  string
	Quota     int
	Years     int
	Enabled   *bool
	StartDate string // optional ISO date (YYYY-MM-DD); if set, overrides the default base date for expiry calculation
}

func (s *Service) HubDigitalEmployeeAuthorization(ctx context.Context, hubID string) (*corelib.DigitalEmployeeAuthorization, error) {
	auths, err := s.HubDigitalEmployeeAuthorizations(ctx, hubID)
	if err != nil {
		return nil, err
	}
	if auth := auths[""]; auth != nil {
		return auth, nil
	}
	return nil, nil
}

func (s *Service) HubDigitalEmployeeAuthorizations(ctx context.Context, hubID string) (map[string]*corelib.DigitalEmployeeAuthorization, error) {
	hub, err := s.hubs.GetByID(ctx, strings.TrimSpace(hubID))
	if err != nil {
		return nil, err
	}
	if hub == nil {
		return nil, ErrHubNotFound
	}
	out := map[string]*corelib.DigitalEmployeeAuthorization{}
	// If authorization was never configured on this node (all fields at default),
	// return nil so the Hub preserves any previously received valid authorization.
	// This prevents HA replication lag from overwriting a correct quota with zero.
	if hub.DigitalEmployeeQuota != 0 || hub.DigitalEmployeeAuthorizationEnabled || hub.DigitalEmployeeAuthorizationExpiresAt != nil {
		auth := corelib.DigitalEmployeeAuthorization{
			Quota:   hub.DigitalEmployeeQuota,
			Enabled: hub.DigitalEmployeeAuthorizationEnabled,
		}
		if hub.DigitalEmployeeAuthorizationExpiresAt != nil {
			auth.ExpiresAt = hub.DigitalEmployeeAuthorizationExpiresAt.UTC().Format(time.RFC3339)
		}
		out[""] = ptrDigitalEmployeeAuthorization(corelib.NormalizeDigitalEmployeeAuthorization(auth, time.Now().UTC()))
	}
	tenantAuths, err := s.loadTenantDigitalEmployeeAuthorizations(ctx, hubID)
	if err != nil {
		return nil, err
	}
	for tenantID, auth := range tenantAuths {
		if strings.TrimSpace(tenantID) == "" || auth == nil {
			continue
		}
		normalized := corelib.NormalizeDigitalEmployeeAuthorization(*auth, time.Now().UTC())
		out[strings.TrimSpace(tenantID)] = &normalized
	}
	return out, nil
}

func (s *Service) UpdateDigitalEmployeeAuthorization(ctx context.Context, hubID string, req DigitalEmployeeAuthorizationUpdate) (*corelib.DigitalEmployeeAuthorization, error) {
	hubID = strings.TrimSpace(hubID)
	tenantID := strings.TrimSpace(req.TenantID)
	if hubID == "" {
		return nil, errors.New("hub id is required")
	}
	hub, err := s.hubs.GetByID(ctx, hubID)
	if err != nil {
		return nil, err
	}
	if hub == nil {
		return nil, ErrHubNotFound
	}
	if tenantID != "" {
		return s.updateTenantDigitalEmployeeAuthorization(ctx, hubID, tenantID, req)
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	requestedQuota := req.Quota
	if requestedQuota == 0 && hub.DigitalEmployeeQuota > 0 {
		requestedQuota = hub.DigitalEmployeeQuota
	}
	if requestedQuota < hub.DigitalEmployeeQuota {
		return nil, ErrDigitalEmployeeQuotaDecrease
	}
	if enabled && requestedQuota <= 0 {
		return nil, ErrDigitalEmployeeQuotaRequired
	}
	years := req.Years
	if enabled && years < 1 {
		return nil, ErrDigitalEmployeeYearsRequired
	}
	var expiresAt *time.Time
	if enabled {
		base := time.Now().UTC()
		// If a custom start date is provided, use it as the base for expiry calculation.
		if req.StartDate != "" {
			parsed, parseErr := time.Parse("2006-01-02", req.StartDate)
			if parseErr != nil {
				return nil, fmt.Errorf("invalid start_date format (expected YYYY-MM-DD): %w", parseErr)
			}
			base = parsed.UTC()
		} else if hub.DigitalEmployeeAuthorizationExpiresAt != nil && hub.DigitalEmployeeAuthorizationExpiresAt.After(base) {
			base = hub.DigitalEmployeeAuthorizationExpiresAt.UTC()
		}
		next := base.AddDate(years, 0, 0)
		if hub.DigitalEmployeeAuthorizationExpiresAt != nil && hub.DigitalEmployeeAuthorizationExpiresAt.After(next) {
			next = hub.DigitalEmployeeAuthorizationExpiresAt.UTC()
		}
		expiresAt = &next
	} else {
		expiresAt = hub.DigitalEmployeeAuthorizationExpiresAt
	}
	quota := requestedQuota
	if quota < 0 {
		quota = 0
	}
	if err := s.hubs.UpdateDigitalEmployeeAuthorization(ctx, hubID, quota, enabled, expiresAt, time.Now()); err != nil {
		return nil, err
	}
	// Propagate the change to all HA nodes so that Hub heartbeats hitting
	// any node will receive the updated quota.
	if err := s.recordHubByID(ctx, hubID); err != nil {
		return nil, err
	}
	return s.HubDigitalEmployeeAuthorization(ctx, hubID)
}

func ptrDigitalEmployeeAuthorization(auth corelib.DigitalEmployeeAuthorization) *corelib.DigitalEmployeeAuthorization {
	return &auth
}

func (s *Service) updateTenantDigitalEmployeeAuthorization(ctx context.Context, hubID, tenantID string, req DigitalEmployeeAuthorizationUpdate) (*corelib.DigitalEmployeeAuthorization, error) {
	items, err := s.loadTenantDigitalEmployeeAuthorizations(ctx, hubID)
	if err != nil {
		return nil, err
	}
	current := items[tenantID]
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	currentQuota := 0
	var currentExpiresAt *time.Time
	if current != nil {
		currentQuota = current.Quota
		if strings.TrimSpace(current.ExpiresAt) != "" {
			if parsed, parseErr := time.Parse(time.RFC3339, strings.TrimSpace(current.ExpiresAt)); parseErr == nil {
				currentExpiresAt = &parsed
			}
		}
	}
	requestedQuota := req.Quota
	if requestedQuota == 0 && currentQuota > 0 {
		requestedQuota = currentQuota
	}
	if requestedQuota < currentQuota {
		return nil, ErrDigitalEmployeeQuotaDecrease
	}
	if enabled && requestedQuota <= 0 {
		return nil, ErrDigitalEmployeeQuotaRequired
	}
	years := req.Years
	if enabled && years < 1 {
		return nil, ErrDigitalEmployeeYearsRequired
	}
	var expiresAt *time.Time
	if enabled {
		base := time.Now().UTC()
		if req.StartDate != "" {
			parsed, parseErr := time.Parse("2006-01-02", req.StartDate)
			if parseErr != nil {
				return nil, fmt.Errorf("invalid start_date format (expected YYYY-MM-DD): %w", parseErr)
			}
			base = parsed.UTC()
		} else if currentExpiresAt != nil && currentExpiresAt.After(base) {
			base = currentExpiresAt.UTC()
		}
		next := base.AddDate(years, 0, 0)
		if currentExpiresAt != nil && currentExpiresAt.After(next) {
			next = currentExpiresAt.UTC()
		}
		expiresAt = &next
	} else {
		expiresAt = currentExpiresAt
	}
	auth := corelib.DigitalEmployeeAuthorization{Quota: requestedQuota, Enabled: enabled}
	if expiresAt != nil {
		auth.ExpiresAt = expiresAt.UTC().Format(time.RFC3339)
	}
	normalized := corelib.NormalizeDigitalEmployeeAuthorization(auth, time.Now().UTC())
	items[tenantID] = &normalized
	if err := s.saveTenantDigitalEmployeeAuthorizations(ctx, hubID, items); err != nil {
		return nil, err
	}
	if err := s.recordHubByID(ctx, hubID); err != nil {
		return nil, err
	}
	return &normalized, nil
}

func (s *Service) loadTenantDigitalEmployeeAuthorizations(ctx context.Context, hubID string) (map[string]*corelib.DigitalEmployeeAuthorization, error) {
	items := map[string]*corelib.DigitalEmployeeAuthorization{}
	if s.settings == nil {
		return items, nil
	}
	raw, err := s.settings.Get(ctx, tenantDigitalEmployeeAuthorizationsKey(hubID))
	if err != nil || strings.TrimSpace(raw) == "" {
		return items, nil
	}
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Service) saveTenantDigitalEmployeeAuthorizations(ctx context.Context, hubID string, items map[string]*corelib.DigitalEmployeeAuthorization) error {
	if s.settings == nil {
		return nil
	}
	data, err := json.Marshal(items)
	if err != nil {
		return err
	}
	return s.settings.Set(ctx, tenantDigitalEmployeeAuthorizationsKey(hubID), string(data))
}

func tenantDigitalEmployeeAuthorizationsKey(hubID string) string {
	return systemKeyTenantDigitalEmployeeAuthorizations + ":" + strings.TrimSpace(hubID)
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
	hubID = strings.TrimSpace(hubID)
	if s.links != nil {
		items, err := listHubUserLinksByHub(ctx, s.links, hubID)
		if err != nil {
			return err
		}
		if err := s.links.DeleteByHubID(ctx, hubID); err != nil {
			return err
		}
		if s.sync != nil {
			for _, item := range items {
				if item == nil || strings.TrimSpace(item.HubID) != hubID {
					continue
				}
				s.sync.DeleteHubUserLink(ctx, item.ID)
			}
		}
	}
	var routes []*store.HubDomainRoute
	var routeListErr error
	if s.routes != nil {
		routes, routeListErr = listHubDomainRoutesByHub(ctx, s.routes, hubID)
		if routeListErr != nil {
			return routeListErr
		}
		if err := s.routes.DeleteByHubID(ctx, hubID); err != nil {
			return err
		}
	}
	if err := s.hubs.DeleteByID(ctx, hubID); err != nil {
		return err
	}
	if s.sync != nil {
		for _, route := range routes {
			if route == nil || strings.TrimSpace(route.HubID) != hubID {
				continue
			}
			s.sync.DeleteHubDomainRoute(ctx, route.ID)
		}
		s.sync.DeleteHubInstance(ctx, hubID)
	}
	s.refreshRoutes(ctx)
	return nil
}

func listHubUserLinksByHub(ctx context.Context, repo store.HubUserLinkRepository, hubID string) ([]*store.HubUserLink, error) {
	if lister, ok := repo.(hubUserLinkByHubLister); ok {
		return lister.ListByHubID(ctx, hubID)
	}
	items, err := repo.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*store.HubUserLink, 0)
	for _, item := range items {
		if item != nil && strings.TrimSpace(item.HubID) == strings.TrimSpace(hubID) {
			out = append(out, item)
		}
	}
	return out, nil
}

func listHubDomainRoutesByHub(ctx context.Context, repo store.HubDomainRouteRepository, hubID string) ([]*store.HubDomainRoute, error) {
	if lister, ok := repo.(hubDomainRouteByHubLister); ok {
		return lister.ListByHubID(ctx, hubID)
	}
	items, err := repo.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*store.HubDomainRoute, 0)
	for _, item := range items {
		if item != nil && strings.TrimSpace(item.HubID) == strings.TrimSpace(hubID) {
			out = append(out, item)
		}
	}
	return out, nil
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

func (s *Service) SyncHubUserLink(ctx context.Context, hubID, rawSecret, email string, isDefault bool, tenantIDOpt ...string) error {
	hubID = strings.TrimSpace(hubID)
	email = normalizeEmail(email)
	tenantID := ""
	if len(tenantIDOpt) > 0 {
		tenantID = normalizeHubSyncTenantID(tenantIDOpt[0])
	}
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
	allowed, err := s.userRouteAllowedByDomain(ctx, hubID, tenantID, email)
	if err != nil {
		return err
	}
	if !allowed {
		return nil
	}
	items, err := s.links.ListByEmail(ctx, email)
	if err != nil {
		return err
	}
	for _, item := range items {
		if item != nil && isAdminUserLink(item) && normalizeHubSyncTenantID(item.TenantID) == tenantID {
			return nil
		}
	}
	for _, item := range items {
		if item == nil || isOwnerLink(item) {
			continue
		}
		if normalizeHubSyncTenantID(item.TenantID) != tenantID {
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
	link := &store.HubUserLink{ID: primaryUserLinkIDForTenant(hubID, tenantID, email), HubID: hubID, TenantID: tenantID, Email: email, IsDefault: isDefault && tenantID == "", CreatedAt: now, UpdatedAt: now}
	if err := s.links.Upsert(ctx, link); err != nil {
		return err
	}
	if s.sync != nil {
		s.sync.AppendHubUserLink(ctx, link)
	}
	s.refreshRoutes(ctx)
	return nil
}

func (s *Service) DeleteHubUserLink(ctx context.Context, hubID, rawSecret, email string, tenantIDOpt ...string) error {
	hubID = strings.TrimSpace(hubID)
	email = normalizeEmail(email)
	tenantID := ""
	if len(tenantIDOpt) > 0 {
		tenantID = normalizeHubSyncTenantID(tenantIDOpt[0])
	}
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
	removed, err := s.deleteHubUserLinksByScope(ctx, hubID, tenantID, email)
	if err != nil {
		return err
	}
	if s.sync != nil {
		for _, item := range removed {
			if item != nil {
				s.sync.DeleteHubUserLink(ctx, item.ID)
			}
		}
	}
	s.refreshRoutes(ctx)
	return nil
}

func (s *Service) deleteHubUserLinksByScope(ctx context.Context, hubID, tenantID, email string) ([]*store.HubUserLink, error) {
	if deleter, ok := s.links.(hubUserLinkScopedDeleter); ok {
		return deleter.DeleteByHubTenantEmail(ctx, hubID, tenantID, email)
	}
	items, err := s.links.ListByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	removed := make([]*store.HubUserLink, 0)
	for _, item := range items {
		if item == nil || isOwnerLink(item) || isAdminUserLink(item) {
			continue
		}
		if strings.TrimSpace(item.HubID) != strings.TrimSpace(hubID) || normalizeHubSyncTenantID(item.TenantID) != normalizeHubSyncTenantID(tenantID) {
			continue
		}
		if err := s.links.DeleteByID(ctx, item.ID); err != nil {
			return nil, err
		}
		removed = append(removed, item)
	}
	return removed, nil
}

func normalizeHubSyncTenantID(tenantID string) string {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "tenant_default" {
		return ""
	}
	return tenantID
}

func (s *Service) rebuildHubUserEmailInventory(ctx context.Context, hubID string, emails []string, now time.Time) error {
	if s.links == nil || strings.TrimSpace(hubID) == "" {
		return nil
	}
	current := map[string]struct{}{}
	for _, rawEmail := range emails {
		email := normalizeEmail(rawEmail)
		if email != "" {
			current[email] = struct{}{}
		}
	}
	items, err := listHubUserLinksByHub(ctx, s.links, hubID)
	if err != nil {
		return err
	}
	for _, item := range items {
		if item == nil || strings.TrimSpace(item.HubID) != strings.TrimSpace(hubID) || isOwnerLink(item) || isAdminUserLink(item) {
			continue
		}
		if !strings.HasPrefix(strings.TrimSpace(item.ID), "hul_user_"+strings.TrimSpace(hubID)+"_") {
			continue
		}
		email := normalizeEmail(item.Email)
		if _, ok := current[email]; ok {
			continue
		}
		if err := s.links.DeleteByID(ctx, item.ID); err != nil {
			return err
		}
		if s.sync != nil {
			s.sync.DeleteHubUserLink(ctx, item.ID)
		}
	}
	return s.syncHubUserEmailInventory(ctx, hubID, emails, now)
}

func (s *Service) syncHubUserEmailInventory(ctx context.Context, hubID string, emails []string, now time.Time) error {
	return s.syncHubTenantUserEmailInventory(ctx, hubID, map[string][]string{"": emails}, now)
}

func (s *Service) syncHubUserEmailInventoryFromCapabilities(ctx context.Context, hubID string, caps map[string]any, now time.Time) error {
	return s.syncHubTenantUserEmailInventory(ctx, hubID, tenantUserEmailCapabilityMap(caps), now)
}

func (s *Service) syncHubTenantUserEmailInventory(ctx context.Context, hubID string, tenantEmails map[string][]string, now time.Time) error {
	if s.links == nil || strings.TrimSpace(hubID) == "" || len(tenantEmails) == 0 {
		return nil
	}
	routeChecker, err := s.newUserRouteDomainChecker(ctx)
	if err != nil {
		return err
	}
	seen := map[string]struct{}{}
	seenByTenant := map[string]map[string]struct{}{}
	for tenantID, emails := range tenantEmails {
		tenantID = normalizeHubSyncTenantID(tenantID)
		if seenByTenant[tenantID] == nil {
			seenByTenant[tenantID] = map[string]struct{}{}
		}
		for _, rawEmail := range emails {
			email := normalizeEmail(rawEmail)
			if email == "" {
				continue
			}
			allowed := routeChecker.allowed(hubID, tenantID, email)
			if !allowed {
				if removed, err := s.deleteHubUserLinksByScope(ctx, hubID, tenantID, email); err != nil {
					return err
				} else if s.sync != nil {
					for _, item := range removed {
						if item != nil {
							s.sync.DeleteHubUserLink(ctx, item.ID)
						}
					}
				}
				continue
			}
			seenKey := tenantID + "\x00" + email
			if _, ok := seen[seenKey]; ok {
				continue
			}
			seen[seenKey] = struct{}{}
			seenByTenant[tenantID][email] = struct{}{}

			items, err := s.links.ListByEmail(ctx, email)
			if err != nil {
				return err
			}
			adminManaged := false
			for _, item := range items {
				if item != nil && isAdminUserLink(item) && normalizeHubSyncTenantID(item.TenantID) == tenantID {
					adminManaged = true
					break
				}
			}
			if adminManaged {
				continue
			}

			link := &store.HubUserLink{ID: primaryUserLinkIDForTenant(hubID, tenantID, email), HubID: hubID, TenantID: tenantID, Email: email, IsDefault: false, CreatedAt: now, UpdatedAt: now}
			if err := s.links.Upsert(ctx, link); err != nil {
				return err
			}
			if s.sync != nil {
				s.sync.AppendHubUserLink(ctx, link)
			}
		}
	}

	items, err := listHubUserLinksByHub(ctx, s.links, hubID)
	if err != nil {
		return err
	}
	for _, item := range items {
		if item == nil || strings.TrimSpace(item.HubID) != strings.TrimSpace(hubID) || isOwnerLink(item) || isAdminUserLink(item) {
			continue
		}
		if !strings.HasPrefix(strings.TrimSpace(item.ID), "hul_user_"+strings.TrimSpace(hubID)+"_") {
			continue
		}
		tenantID := normalizeHubSyncTenantID(item.TenantID)
		seenEmails, ok := seenByTenant[tenantID]
		if !ok {
			continue
		}
		if _, ok := seenEmails[normalizeEmail(item.Email)]; ok {
			continue
		}
		if err := s.links.DeleteByID(ctx, item.ID); err != nil {
			return err
		}
		if s.sync != nil {
			s.sync.DeleteHubUserLink(ctx, item.ID)
		}
	}
	return nil
}

type userRouteDomainChecker struct {
	byDomain map[string][]userRouteDomainCandidate
}

type userRouteDomainCandidate struct {
	route *store.HubDomainRoute
	hub   *store.HubInstance
}

func (s *Service) newUserRouteDomainChecker(ctx context.Context) (*userRouteDomainChecker, error) {
	checker := &userRouteDomainChecker{byDomain: map[string][]userRouteDomainCandidate{}}
	if s == nil || s.routes == nil {
		return checker, nil
	}
	routes, err := s.routes.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	hubsByID := map[string]*store.HubInstance{}
	for _, route := range routes {
		if route == nil || !route.Enabled {
			continue
		}
		domain := normalizeCorporateEmailDomain(route.Domain)
		if domain == "" {
			continue
		}
		hubID := strings.TrimSpace(route.HubID)
		hub, ok := hubsByID[hubID]
		if !ok && s.hubs != nil {
			hub, err = s.hubs.GetByID(ctx, hubID)
			if err != nil {
				return nil, err
			}
			hubsByID[hubID] = hub
		}
		if !hubDomainRouteActive(hub) {
			continue
		}
		checker.byDomain[domain] = append(checker.byDomain[domain], userRouteDomainCandidate{route: route, hub: hub})
	}
	return checker, nil
}

func (c *userRouteDomainChecker) allowed(hubID, tenantID, email string) bool {
	if c == nil || len(c.byDomain) == 0 {
		return true
	}
	domain := normalizeCorporateEmailDomain(extractEmailDomain(email))
	if domain == "" {
		return true
	}
	tenantID = normalizeHubSyncTenantID(tenantID)
	hubID = strings.TrimSpace(hubID)
	var bestRoute *store.HubDomainRoute
	var bestHub *store.HubInstance
	preferExactTenant := false
	for _, candidate := range c.byDomain[domain] {
		route := candidate.route
		if route == nil {
			continue
		}
		routeTenantID := normalizeHubSyncTenantID(route.TenantID)
		if routeTenantID != "" && routeTenantID != tenantID {
			continue
		}
		if preferExactTenant && routeTenantID == "" {
			continue
		}
		if routeTenantID != "" && !preferExactTenant {
			preferExactTenant = true
			bestRoute = nil
			bestHub = nil
		}
		if bestRoute == nil || routePriorityLess(route, candidate.hub, bestRoute, bestHub) {
			bestRoute = route
			bestHub = candidate.hub
		}
	}
	if bestRoute == nil {
		return true
	}
	return strings.TrimSpace(bestRoute.HubID) == hubID
}

func (s *Service) userRouteAllowedByDomain(ctx context.Context, hubID, tenantID, email string) (bool, error) {
	if s == nil || s.routes == nil {
		return true, nil
	}
	domain := normalizeCorporateEmailDomain(extractEmailDomain(email))
	if domain == "" {
		return true, nil
	}
	tenantID = normalizeHubSyncTenantID(tenantID)
	hubID = strings.TrimSpace(hubID)
	routes, err := listEnabledDomainRoutesForDomain(ctx, s.routes, domain)
	if err != nil {
		return false, err
	}
	var bestRoute *store.HubDomainRoute
	var bestHub *store.HubInstance
	preferExactTenant := false
	for _, route := range routes {
		if route == nil || !route.Enabled || normalizeCorporateEmailDomain(route.Domain) != domain {
			continue
		}
		routeTenantID := normalizeHubSyncTenantID(route.TenantID)
		if routeTenantID != "" && routeTenantID != tenantID {
			continue
		}
		if preferExactTenant && routeTenantID == "" {
			continue
		}
		hub, err := s.hubs.GetByID(ctx, strings.TrimSpace(route.HubID))
		if err != nil {
			return false, err
		}
		if !hubDomainRouteActive(hub) {
			continue
		}
		if routeTenantID != "" && !preferExactTenant {
			preferExactTenant = true
			bestRoute = nil
			bestHub = nil
		}
		if bestRoute == nil || routePriorityLess(route, hub, bestRoute, bestHub) {
			bestRoute = route
			bestHub = hub
		}
	}
	if bestRoute == nil {
		return true, nil
	}
	return strings.TrimSpace(bestRoute.HubID) == hubID, nil
}

func listEnabledDomainRoutesForDomain(ctx context.Context, repo store.HubDomainRouteRepository, domain string) ([]*store.HubDomainRoute, error) {
	domain = normalizeCorporateEmailDomain(domain)
	if lister, ok := repo.(hubDomainRouteByDomainLister); ok {
		return lister.ListEnabledByDomain(ctx, domain)
	}
	items, err := repo.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*store.HubDomainRoute, 0)
	for _, item := range items {
		if item != nil && item.Enabled && normalizeCorporateEmailDomain(item.Domain) == domain {
			out = append(out, item)
		}
	}
	return out, nil
}

func hubDomainRouteActive(hub *store.HubInstance) bool {
	return hub != nil && !hub.IsDisabled && strings.EqualFold(strings.TrimSpace(hub.Status), "online")
}

func routePriorityLess(a *store.HubDomainRoute, aHub *store.HubInstance, b *store.HubDomainRoute, bHub *store.HubInstance) bool {
	if b == nil {
		return true
	}
	if a.Priority != b.Priority {
		return a.Priority < b.Priority
	}
	av := hubRouteVisibilityPriority(aHub)
	bv := hubRouteVisibilityPriority(bHub)
	if av != bv {
		return av < bv
	}
	aName := hubRouteName(aHub)
	bName := hubRouteName(bHub)
	if aName != bName {
		return aName < bName
	}
	if normalizeHubSyncTenantID(a.TenantID) != normalizeHubSyncTenantID(b.TenantID) {
		return normalizeHubSyncTenantID(a.TenantID) > normalizeHubSyncTenantID(b.TenantID)
	}
	if strings.TrimSpace(a.HubID) != strings.TrimSpace(b.HubID) {
		return strings.TrimSpace(a.HubID) < strings.TrimSpace(b.HubID)
	}
	return strings.TrimSpace(a.ID) < strings.TrimSpace(b.ID)
}

func hubRouteName(hub *store.HubInstance) string {
	if hub == nil {
		return ""
	}
	return strings.TrimSpace(hub.Name)
}

func hubRouteVisibilityPriority(hub *store.HubInstance) int {
	if hub == nil {
		return 3
	}
	switch strings.ToLower(strings.TrimSpace(hub.Visibility)) {
	case "shared":
		return 0
	case "public":
		return 1
	default:
		return 2
	}
}

func tenantUserEmailCapabilityMap(caps map[string]any) map[string][]string {
	if caps != nil {
		byTenant := tenantStringListCapabilityMap(caps["tenant_user_emails"], nil, true)
		if len(byTenant) > 0 {
			return byTenant
		}
	}
	byTenant := map[string][]string{}
	if caps != nil {
		byTenant[""] = capabilityStringList(caps["user_emails"])
	}
	return byTenant
}

func tenantDomainCapabilityMap(caps map[string]any) map[string][]string {
	if caps == nil {
		return map[string][]string{}
	}
	return tenantStringListCapabilityMap(caps["tenant_domains"], normalizeCorporateEmailDomains, true)
}

func tenantStringListCapabilityMap(value any, normalize func([]string) []string, includeEmptyTenant bool) map[string][]string {
	byTenant := map[string][]string{}
	rv := reflect.ValueOf(value)
	if !rv.IsValid() || rv.Kind() != reflect.Map || rv.Type().Key().Kind() != reflect.String {
		return byTenant
	}
	for _, key := range rv.MapKeys() {
		tenantID := normalizeHubSyncTenantID(key.String())
		if tenantID == "" && !includeEmptyTenant {
			continue
		}
		items := capabilityStringList(rv.MapIndex(key).Interface())
		if normalize != nil {
			items = normalize(items)
		}
		if len(items) > 0 {
			byTenant[tenantID] = items
		}
	}
	return byTenant
}

func hubCapabilities(hub *store.HubInstance) map[string]any {
	caps := map[string]any{}
	if hub == nil || strings.TrimSpace(hub.CapabilitiesJSON) == "" {
		return caps
	}
	_ = json.Unmarshal([]byte(hub.CapabilitiesJSON), &caps)
	return caps
}

func dashboardTenantIDs(caps map[string]any, fallback map[string]int) []string {
	seen := map[string]struct{}{}
	for tenantID := range tenantUserEmailCapabilityMap(caps) {
		if tenantID = strings.TrimSpace(tenantID); tenantID != "" {
			seen[tenantID] = struct{}{}
		}
	}
	collectTenantIDsFromNumericMap(seen, caps["tenant_user_counts"])
	collectTenantIDsFromNumericMap(seen, caps["tenant_machine_counts"])
	collectTenantIDsFromMapKeys(seen, caps["tenant_domains"])
	collectTenantIDsFromMapKeys(seen, caps["tenant_names"])
	for tenantID := range fallback {
		if tenantID = strings.TrimSpace(tenantID); tenantID != "" {
			seen[tenantID] = struct{}{}
		}
	}
	items := make([]string, 0, len(seen))
	for tenantID := range seen {
		items = append(items, tenantID)
	}
	sort.Strings(items)
	return items
}

func collectTenantIDsFromNumericMap(seen map[string]struct{}, value any) {
	collectTenantIDsFromMapKeys(seen, value)
}

func collectTenantIDsFromMapKeys(seen map[string]struct{}, value any) {
	rv := reflect.ValueOf(value)
	if !rv.IsValid() || rv.Kind() != reflect.Map || rv.Type().Key().Kind() != reflect.String {
		return
	}
	for _, key := range rv.MapKeys() {
		tenantID := normalizeHubSyncTenantID(key.String())
		if tenantID != "" {
			seen[tenantID] = struct{}{}
		}
	}
}

func tenantDashboardDomains(caps map[string]any, tenantID string) []string {
	items := tenantDomainCapabilityMap(caps)[normalizeHubSyncTenantID(tenantID)]
	return normalizeCorporateEmailDomains(items)
}

func tenantDashboardName(caps map[string]any, tenantID string) string {
	tenantID = normalizeHubSyncTenantID(tenantID)
	switch raw := caps["tenant_names"].(type) {
	case map[string]any:
		name, _ := raw[tenantID].(string)
		if name == "" && tenantID == "" {
			name, _ = raw["tenant_default"].(string)
		}
		return strings.TrimSpace(name)
	case map[string]string:
		name := raw[tenantID]
		if name == "" && tenantID == "" {
			name = raw["tenant_default"]
		}
		return strings.TrimSpace(name)
	default:
		return ""
	}
}

func tenantUserCountFromCapabilities(caps map[string]any, tenantID string, fallback int) int {
	if n, ok := tenantCountFromCapability(caps["tenant_user_counts"], tenantID); ok {
		return n
	}
	if emails := tenantUserEmailCapabilityMap(caps)[normalizeHubSyncTenantID(tenantID)]; len(emails) > 0 {
		return len(emails)
	}
	return fallback
}

func tenantMachineCountFromCapabilities(caps map[string]any, tenantID string) int {
	if n, ok := tenantCountFromCapability(caps["tenant_machine_counts"], tenantID); ok {
		return n
	}
	return 0
}

func tenantCountFromCapability(value any, tenantID string) (int, bool) {
	tenantID = normalizeHubSyncTenantID(tenantID)
	rv := reflect.ValueOf(value)
	if !rv.IsValid() || rv.Kind() != reflect.Map || rv.Type().Key().Kind() != reflect.String {
		return 0, false
	}
	item := rv.MapIndex(reflect.ValueOf(tenantID))
	if !item.IsValid() && tenantID == "" {
		item = rv.MapIndex(reflect.ValueOf("tenant_default"))
	}
	if !item.IsValid() {
		return 0, false
	}
	return numericCapability(item.Interface())
}

func hubUserCountFallback(counts map[string]map[string]int, hubID, tenantID string) int {
	items := counts[strings.TrimSpace(hubID)]
	if len(items) == 0 {
		return 0
	}
	tenantID = strings.TrimSpace(tenantID)
	return items[tenantID]
}

func tenantEmailCountKey(tenantID, email string) string {
	tenantID = strings.TrimSpace(tenantID)
	email = normalizeEmail(email)
	if email == "" {
		return ""
	}
	return tenantID + "\x00" + email
}

func splitTenantEmailCountKey(key string) (string, string) {
	parts := strings.SplitN(key, "\x00", 2)
	if len(parts) != 2 {
		return "", normalizeEmail(key)
	}
	return strings.TrimSpace(parts[0]), normalizeEmail(parts[1])
}

func firstString(items []string) string {
	if len(items) == 0 {
		return ""
	}
	return items[0]
}

func (s *Service) syncDomainRoutes(ctx context.Context, hub *store.HubInstance, domains []string, now time.Time) error {
	if s.routes == nil || hub == nil {
		return nil
	}
	tenantDomains := tenantDomainCapabilityMap(hubCapabilities(hub))
	preservedAdminRoutes := make([]*store.HubDomainRoute, 0)
	removedRouteIDs := make([]string, 0)
	existingRoutesListed := false
	if existing, err := listHubDomainRoutesByHub(ctx, s.routes, hub.ID); err == nil {
		existingRoutesListed = true
		for _, route := range existing {
			if route == nil || strings.TrimSpace(route.HubID) != strings.TrimSpace(hub.ID) {
				continue
			}
			if strings.HasPrefix(strings.TrimSpace(route.ID), adminDomainRoutePrefix) {
				preservedAdminRoutes = append(preservedAdminRoutes, route)
				continue
			}
			removedRouteIDs = append(removedRouteIDs, route.ID)
		}
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
		if existingRoutesListed {
			for _, routeID := range removedRouteIDs {
				s.sync.DeleteHubDomainRoute(ctx, routeID)
			}
		} else {
			for idx := 0; idx < 16; idx++ {
				s.sync.DeleteHubDomainRoute(ctx, domainRouteID(hub.ID, idx))
			}
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
	for tenantID, values := range tenantDomains {
		tenantID = strings.TrimSpace(tenantID)
		if tenantID == "" || len(values) == 0 {
			continue
		}
		for idx, domain := range values {
			route := &store.HubDomainRoute{ID: tenantDomainRouteID(hub.ID, tenantID, idx), HubID: hub.ID, TenantID: tenantID, Domain: domain, Enabled: true, Priority: 200 + idx, CreatedAt: now, UpdatedAt: now}
			if err := s.routes.Upsert(ctx, route); err != nil {
				return err
			}
			if s.sync != nil {
				s.sync.AppendHubDomainRoute(ctx, route)
			}
		}
	}
	for _, route := range preservedAdminRoutes {
		route.UpdatedAt = now
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
	return strings.TrimSpace(strings.ToLower(width.Narrow.String(email)))
}

func normalizeEmailPattern(pattern string) string {
	pattern = normalizeEmail(pattern)
	pattern = strings.ReplaceAll(pattern, "\uff0a", "*")
	if strings.HasPrefix(pattern, "@") {
		pattern = "*" + pattern
	}
	return pattern
}

func wildcardEmailMatch(pattern, email string) bool {
	pattern = normalizeEmailPattern(pattern)
	email = normalizeEmail(email)
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == email
	}
	if parts[0] != "" && !strings.HasPrefix(email, parts[0]) {
		return false
	}
	pos := len(parts[0])
	for _, part := range parts[1 : len(parts)-1] {
		if part == "" {
			continue
		}
		idx := strings.Index(email[pos:], part)
		if idx < 0 {
			return false
		}
		pos += idx + len(part)
	}
	last := parts[len(parts)-1]
	return last == "" || strings.HasSuffix(email[pos:], last)
}
func isEmailMigrationPattern(email string) bool {
	return strings.Contains(email, "*")
}

func isValidEmailMigrationPattern(pattern string) bool {
	local, domain, ok := strings.Cut(normalizeEmailPattern(pattern), "@")
	if !ok || local == "" || domain == "" || strings.Contains(domain, "*") || !strings.Contains(domain, ".") {
		return false
	}
	return !strings.ContainsAny(local, " \t\r\n@") && !strings.ContainsAny(domain, " \t\r\n@")
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

func extractEmailDomain(email string) string {
	_, domain, ok := strings.Cut(normalizeEmail(email), "@")
	if !ok {
		return ""
	}
	return normalizeCorporateEmailDomain(domain)
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

func hubCorporateDomains(hub *store.HubInstance) []string {
	if hub == nil {
		return nil
	}
	if strings.TrimSpace(hub.CorporateEmailDomain) == "" {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, 2)
	add := func(value string) {
		value = normalizeCorporateEmailDomain(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	add(hub.CorporateEmailDomain)
	var caps map[string]any
	if strings.TrimSpace(hub.CapabilitiesJSON) != "" && json.Unmarshal([]byte(hub.CapabilitiesJSON), &caps) == nil {
		if values, ok := caps["corporate_email_domains"].([]any); ok {
			for _, value := range values {
				add(fmt.Sprint(value))
			}
		}
		if value, ok := caps["corporate_email_domain"]; ok {
			add(fmt.Sprint(value))
		}
	}
	return out
}

func hubMachineCount(hub *store.HubInstance) int {
	if hub == nil || strings.TrimSpace(hub.CapabilitiesJSON) == "" {
		return 0
	}
	var caps map[string]any
	if err := json.Unmarshal([]byte(hub.CapabilitiesJSON), &caps); err != nil {
		return 0
	}
	return hubMachineCountFromCapabilities(caps)
}

func hubMachineCountFromCapabilities(caps map[string]any) int {
	for _, key := range []string{"machine_count", "machines_count", "machineCount", "machinesCount", "machine_total", "machines"} {
		if n, ok := numericCapability(caps[key]); ok {
			return n
		}
	}
	return 0
}

func hubUserCount(hub *store.HubInstance, fallback int) int {
	if hub == nil || strings.TrimSpace(hub.CapabilitiesJSON) == "" {
		return fallback
	}
	var caps map[string]any
	if err := json.Unmarshal([]byte(hub.CapabilitiesJSON), &caps); err != nil {
		return fallback
	}
	return hubUserCountFromCapabilities(caps, fallback)
}

func hubUserCountFromCapabilities(caps map[string]any, fallback int) int {
	for _, key := range []string{"user_count", "users_count", "userCount", "usersCount", "user_total", "users"} {
		if n, ok := numericCapability(caps[key]); ok {
			return n
		}
	}
	return fallback
}

func capabilityStringList(value any) []string {
	switch v := value.(type) {
	case []string:
		return append([]string(nil), v...)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if item == nil {
				continue
			}
			out = append(out, fmt.Sprint(item))
		}
		return out
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		parts := strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t' || r == ' ' })
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			if strings.TrimSpace(part) != "" {
				out = append(out, part)
			}
		}
		return out
	default:
		return nil
	}
}

func numericCapability(value any) (int, bool) {
	switch v := value.(type) {
	case float64:
		if v >= 0 {
			return int(v), true
		}
	case int:
		if v >= 0 {
			return v, true
		}
	case []any:
		return len(v), true
	case string:
		var n int
		if _, err := fmt.Sscanf(strings.TrimSpace(v), "%d", &n); err == nil && n >= 0 {
			return n, true
		}
	}
	rv := reflect.ValueOf(value)
	if rv.IsValid() {
		switch rv.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			if n := rv.Int(); n >= 0 {
				return int(n), true
			}
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
			return int(rv.Uint()), true
		case reflect.Float32:
			if n := rv.Float(); n >= 0 {
				return int(n), true
			}
		}
	}
	return 0, false
}

func isAdminUserLink(link *store.HubUserLink) bool {
	if link == nil {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(link.ID), adminUserLinkPrefix)
}

func primaryUserLinkID(hubID, email string) string {
	return primaryUserLinkIDForTenant(hubID, "", email)
}

func primaryUserLinkIDForTenant(hubID, tenantID, email string) string {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return fmt.Sprintf("hul_user_%s_%s", strings.TrimSpace(hubID), hashToken(normalizeEmail(email))[:16])
	}
	return fmt.Sprintf("hul_user_%s_%s_%s", strings.TrimSpace(hubID), hashToken(tenantID)[:8], hashToken(normalizeEmail(email))[:16])
}

func adminUserLinkID(email string) string {
	return adminUserLinkIDForTenant("", email)
}

func adminUserLinkIDForTenant(tenantID, email string) string {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return adminUserLinkPrefix + hashToken(normalizeEmail(email))[:20]
	}
	return adminUserLinkPrefix + hashToken(tenantID)[:8] + "_" + hashToken(normalizeEmail(email))[:16]
}

func startOfDay(t time.Time) time.Time {
	local := t.Local()
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.Local)
}

func domainRouteID(hubID string, index int) string {
	return fmt.Sprintf("hdr_%s_%d", strings.TrimSpace(hubID), index)
}

func tenantDomainRouteID(hubID, tenantID string, index int) string {
	return fmt.Sprintf("hdr_tenant_%s_%s_%d", strings.TrimSpace(hubID), hashToken(strings.TrimSpace(tenantID))[:8], index)
}

func adminDomainRouteID(domain string) string {
	return adminDomainRoutePrefix + hashToken(normalizeCorporateEmailDomain(domain))[:20]
}

func adminTenantDomainRouteID(tenantID, domain string) string {
	return adminDomainRoutePrefix + hashToken(strings.TrimSpace(tenantID))[:8] + "_" + hashToken(normalizeCorporateEmailDomain(domain))[:20]
}

func explicitPublicSignupOrDefault(value *bool) bool {
	if value == nil {
		return false
	}
	return *value
}

func (s *Service) refreshRoutes(ctx context.Context) {
	if s.refresher == nil {
		return
	}
	_ = s.refresher.Rebuild(ctx)
}
