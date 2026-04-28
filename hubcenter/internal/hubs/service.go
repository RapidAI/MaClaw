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
	"strings"
	"time"

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

const hubConfirmationPrefix = "hub_registration_confirm:"
const systemKeyPublicBaseURL = "server_public_base_url"
const adminDomainRoutePrefix = "hdr_admin_"
const adminUserLinkPrefix = "hul_admin_"

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

type transactionalUserMigrator interface {
	MigrateEmailToHub(ctx context.Context, email, fromHubID string, link *store.HubUserLink) ([]*store.HubUserLink, *store.HubUserLink, error)
}

type transactionalUserPatternMigrator interface {
	MigrateEmailPatternToHub(ctx context.Context, pattern, fromHubID, toHubID string, now time.Time) ([]*store.HubUserLink, []*store.HubUserLink, error)
}

type transactionalDomainMigrator interface {
	MigrateDomainToHub(ctx context.Context, domain, fromHubID string, route *store.HubDomainRoute) ([]*store.HubDomainRoute, error)
}

type transactionalDomainUserMigrator interface {
	MigrateDomainAndEmailPatternToHub(ctx context.Context, domain, pattern, fromHubID, toHubID string, route *store.HubDomainRoute, now time.Time) ([]*store.HubDomainRoute, []*store.HubUserLink, []*store.HubUserLink, error)
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
	Email     string `json:"email"`
	FromHubID string `json:"from_hub_id,omitempty"`
	ToHubID   string `json:"to_hub_id"`
}

type MigrateDomainRequest struct {
	Domain    string `json:"domain"`
	FromHubID string `json:"from_hub_id,omitempty"`
	ToHubID   string `json:"to_hub_id"`
}

type MigrationResult struct {
	Mode        string   `json:"mode"`
	Email       string   `json:"email,omitempty"`
	Domain      string   `json:"domain,omitempty"`
	ToHubID     string   `json:"to_hub_id"`
	RemovedIDs  []string `json:"removed_ids,omitempty"`
	UpsertedIDs []string `json:"upserted_ids,omitempty"`
}

type remoteUserMigrationRequest struct {
	HubSecretHash string            `json:"hub_secret_hash"`
	Emails        []string          `json:"emails,omitempty"`
	Users         []json.RawMessage `json:"users,omitempty"`
}

type remoteUserMigrationExportResponse struct {
	Users []json.RawMessage `json:"users"`
}

type remoteUserDataPackage struct {
	User *struct {
		Email string `json:"email"`
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

type UserRegistrationReport struct {
	TotalUsers   int                      `json:"total_users"`
	DailyTotal   int                      `json:"daily_total"`
	MonthlyTotal int                      `json:"monthly_total"`
	Daily        []UserRegistrationBucket `json:"daily"`
	Monthly      []UserRegistrationBucket `json:"monthly"`
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
			if err := s.syncHubUserEmailInventory(ctx, existing.ID, capabilityStringList(req.Capabilities["user_emails"]), now); err != nil {
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
	if err := s.syncHubUserEmailInventory(ctx, hub.ID, capabilityStringList(req.Capabilities["user_emails"]), now); err != nil {
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
			if err := s.syncHubUserEmailInventory(ctx, hub.ID, capabilityStringList(update.Capabilities["user_emails"]), now); err != nil {
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
	userCounts := map[string]map[string]struct{}{}
	if s.links != nil {
		links, err := s.links.ListAll(ctx)
		if err != nil {
			return nil, err
		}
		for _, link := range links {
			if link == nil || strings.TrimSpace(link.HubID) == "" || strings.TrimSpace(link.Email) == "" {
				continue
			}
			if userCounts[link.HubID] == nil {
				userCounts[link.HubID] = map[string]struct{}{}
			}
			userCounts[link.HubID][normalizeEmail(link.Email)] = struct{}{}
		}
	}
	out := make([]HubUserDashboardItem, 0, len(hubItems))
	for _, hub := range hubItems {
		if hub == nil {
			continue
		}
		domains := hubCorporateDomains(hub)
		signupMode := "restricted"
		if len(domains) > 0 {
			signupMode = "corporate_domain"
		} else if hub.AcceptPublicSignup {
			signupMode = "public_signup"
		}
		out = append(out, HubUserDashboardItem{
			HubID:                 hub.ID,
			HubName:               hub.Name,
			BaseURL:               hub.BaseURL,
			Status:                hub.Status,
			IsDisabled:            hub.IsDisabled,
			UserCount:             hubUserCount(hub, len(userCounts[hub.ID])),
			MachineCount:          hubMachineCount(hub),
			CorporateEmailDomain:  hub.CorporateEmailDomain,
			CorporateEmailDomains: domains,
			AcceptPublicSignup:    hub.AcceptPublicSignup,
			SignupMode:            signupMode,
			LastSeenAt:            hub.LastSeenAt,
		})
	}
	return out, nil
}

func (s *Service) UserRegistrationReport(ctx context.Context) (UserRegistrationReport, error) {
	var report UserRegistrationReport
	if s.links == nil {
		return report, nil
	}
	links, err := s.links.ListAll(ctx)
	if err != nil {
		return report, err
	}
	firstSeen := map[string]time.Time{}
	for _, link := range links {
		if link == nil {
			continue
		}
		email := normalizeEmail(link.Email)
		if email == "" || strings.Contains(email, "*") {
			continue
		}
		createdAt := link.CreatedAt
		if createdAt.IsZero() {
			createdAt = link.UpdatedAt
		}
		if createdAt.IsZero() {
			continue
		}
		if existing, ok := firstSeen[email]; !ok || createdAt.Before(existing) {
			firstSeen[email] = createdAt
		}
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
	for i := 29; i >= 0; i-- {
		day := today.AddDate(0, 0, -i).Format("2006-01-02")
		report.Daily = append(report.Daily, UserRegistrationBucket{Period: day, Count: dailyCounts[day]})
	}
	for i := 11; i >= 0; i-- {
		month := monthStart.AddDate(0, -i, 0).Format("2006-01")
		report.Monthly = append(report.Monthly, UserRegistrationBucket{Period: month, Count: monthlyCounts[month]})
	}
	return report, nil
}

func (s *Service) MigrateUser(ctx context.Context, req MigrateUserRequest) (*MigrationResult, error) {
	email := normalizeEmailPattern(req.Email)
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
		return s.migrateUserPattern(ctx, email, fromHubID, toHubID)
	}
	sources, err := s.collectUserMigrationSources(ctx, email, fromHubID, toHubID)
	if err != nil {
		return nil, err
	}
	cleanupLocalUsers, err := s.prepareLocalUserMigration(ctx, sources, toHubID)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	link := &store.HubUserLink{ID: adminUserLinkID(email), HubID: toHubID, Email: email, IsDefault: true, CreatedAt: now, UpdatedAt: now}
	var removedLinks []*store.HubUserLink
	var upsertedLink *store.HubUserLink
	if migrator, ok := s.links.(transactionalUserMigrator); ok {
		var err error
		removedLinks, upsertedLink, err = migrator.MigrateEmailToHub(ctx, email, fromHubID, link)
		if err != nil {
			return nil, err
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
		if upsertedLink != nil {
			s.sync.AppendHubUserLink(ctx, upsertedLink)
		}
	}
	upserted := []string{}
	if upsertedLink != nil {
		upserted = append(upserted, upsertedLink.ID)
	}
	s.refreshRoutes(ctx)
	if err := cleanupLocalUsers(ctx); err != nil {
		return nil, err
	}
	return &MigrationResult{Mode: "email", Email: email, ToHubID: toHubID, RemovedIDs: removed, UpsertedIDs: upserted}, nil
}

func (s *Service) migrateUserPattern(ctx context.Context, pattern, fromHubID, toHubID string) (*MigrationResult, error) {
	sources, err := s.collectUserMigrationSources(ctx, pattern, fromHubID, toHubID)
	if err != nil {
		return nil, err
	}
	cleanupLocalUsers, err := s.prepareLocalUserMigration(ctx, sources, toHubID)
	if err != nil {
		return nil, err
	}
	migrator, ok := s.links.(transactionalUserPatternMigrator)
	if !ok {
		return nil, errors.New("transactional user pattern migration is not supported by this store")
	}
	removedLinks, upsertedLinks, err := migrator.MigrateEmailPatternToHub(ctx, pattern, fromHubID, toHubID, time.Now())
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
	return &MigrationResult{Mode: "email", Email: pattern, ToHubID: toHubID, RemovedIDs: removed, UpsertedIDs: upserted}, nil
}

func (s *Service) MigrateDomain(ctx context.Context, req MigrateDomainRequest) (*MigrationResult, error) {
	domain := normalizeCorporateEmailDomain(req.Domain)
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
	sources, err := s.collectUserMigrationSources(ctx, "*@"+domain, fromHubID, toHubID)
	if err != nil {
		return nil, err
	}
	cleanupLocalUsers, err := s.prepareLocalUserMigration(ctx, sources, toHubID)
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
		removedRoutes, removedLinks, upsertedLinks, err = migrator.MigrateDomainAndEmailPatternToHub(ctx, domain, "*@"+domain, fromHubID, toHubID, route, now)
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

func (s *Service) RefreshUserInventory(ctx context.Context) (RefreshUserInventoryResult, error) {
	var result RefreshUserInventoryResult
	if s.hubs == nil {
		return result, nil
	}
	hubItems, err := s.hubs.ListAll(ctx)
	if err != nil {
		return result, err
	}
	now := time.Now()
	for _, hub := range hubItems {
		if !hubCanAttemptDirectUserMigration(hub) {
			continue
		}
		var exported remoteUserMigrationExportResponse
		if err := s.callHubUserMigration(ctx, hub, "/api/center/user-migration/export", remoteUserMigrationRequest{HubSecretHash: hub.HubSecretHash}, &exported); err != nil {
			result.HubsFailed++
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", hub.ID, err))
			continue
		}
		emails := make([]string, 0, len(exported.Users))
		for _, raw := range exported.Users {
			var pkg remoteUserDataPackage
			if err := json.Unmarshal(raw, &pkg); err != nil || pkg.User == nil {
				continue
			}
			email := normalizeEmail(pkg.User.Email)
			if email != "" {
				emails = append(emails, email)
			}
		}
		if err := s.rebuildHubUserEmailInventory(ctx, hub.ID, emails, now); err != nil {
			result.HubsFailed++
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", hub.ID, err))
			continue
		}
		if err := s.updateHubUserEmailCapability(ctx, hub, emails, now); err != nil {
			result.HubsFailed++
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", hub.ID, err))
			continue
		}
		result.HubsRefreshed++
		result.UsersIndexed += len(emails)
	}
	s.refreshRoutes(ctx)
	return result, nil
}

func (s *Service) updateHubUserEmailCapability(ctx context.Context, hub *store.HubInstance, emails []string, now time.Time) error {
	if hub == nil {
		return nil
	}
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
	caps["user_emails"] = normalized
	caps["user_count"] = len(normalized)
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

func (s *Service) removeHubUserEmailInventory(ctx context.Context, hub *store.HubInstance, removedEmails []string, now time.Time) error {
	if hub == nil || len(removedEmails) == 0 {
		return nil
	}
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

func (s *Service) prepareLocalUserMigration(ctx context.Context, sources map[string][]string, toHubID string) (func(context.Context) error, error) {
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
	for sourceHubID, emails := range sources {
		sourceHub, err := s.hubs.GetByID(ctx, sourceHubID)
		if err != nil {
			return nil, err
		}
		if sourceHub == nil || !hubSupportsDirectUserMigration(sourceHub) {
			continue
		}
		var resp remoteUserMigrationExportResponse
		if err := s.callHubUserMigration(ctx, sourceHub, "/api/center/user-migration/export", remoteUserMigrationRequest{HubSecretHash: sourceHub.HubSecretHash, Emails: emails}, &resp); err != nil {
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
	if err := s.callHubUserMigration(ctx, toHub, "/api/center/user-migration/import", remoteUserMigrationRequest{HubSecretHash: toHub.HubSecretHash, Users: packages}, nil); err != nil {
		return nil, err
	}
	cleanup := func(cleanupCtx context.Context) error {
		for sourceHubID, emails := range sources {
			sourceHub, err := s.hubs.GetByID(cleanupCtx, sourceHubID)
			if err != nil {
				return err
			}
			if sourceHub == nil || !hubSupportsDirectUserMigration(sourceHub) {
				continue
			}
			if err := s.callHubUserMigration(cleanupCtx, sourceHub, "/api/center/user-migration/delete", remoteUserMigrationRequest{HubSecretHash: sourceHub.HubSecretHash, Emails: emails}, nil); err != nil {
				return err
			}
			if err := s.removeHubUserEmailInventory(cleanupCtx, sourceHub, emails, time.Now()); err != nil {
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
	body, _ := io.ReadAll(resp.Body)
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

func (s *Service) collectUserMigrationSources(ctx context.Context, pattern, fromHubID, toHubID string) (map[string][]string, error) {
	out := map[string][]string{}
	seen := map[string]struct{}{}
	add := func(hubID, email string) {
		hubID = strings.TrimSpace(hubID)
		email = normalizeEmail(email)
		if hubID == "" || email == "" || strings.TrimSpace(hubID) == strings.TrimSpace(toHubID) {
			return
		}
		if strings.TrimSpace(fromHubID) != "" && strings.TrimSpace(hubID) != strings.TrimSpace(fromHubID) {
			return
		}
		if !wildcardEmailMatch(pattern, email) {
			return
		}
		key := hubID + "|" + email
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out[hubID] = append(out[hubID], email)
	}

	if s.links != nil {
		links, err := s.links.ListAll(ctx)
		if err != nil {
			return nil, err
		}
		for _, link := range links {
			if link == nil || isOwnerLink(link) || isAdminUserLink(link) {
				continue
			}
			add(link.HubID, link.Email)
		}
	}

	if s.hubs != nil {
		hubs, err := s.hubs.ListAll(ctx)
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
			for _, email := range capabilityStringList(caps["user_emails"]) {
				add(hub.ID, email)
			}
		}
	}
	return out, nil
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
		if isAdminUserLink(item) {
			return nil
		}
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
	items, err := s.links.ListAll(ctx)
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
	if s.links == nil || strings.TrimSpace(hubID) == "" || len(emails) == 0 {
		return nil
	}
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

		items, err := s.links.ListByEmail(ctx, email)
		if err != nil {
			return err
		}
		adminManaged := false
		for _, item := range items {
			if isAdminUserLink(item) {
				adminManaged = true
				break
			}
		}
		if adminManaged {
			continue
		}

		link := &store.HubUserLink{ID: primaryUserLinkID(hubID, email), HubID: hubID, Email: email, IsDefault: false, CreatedAt: now, UpdatedAt: now}
		if err := s.links.Upsert(ctx, link); err != nil {
			return err
		}
		if s.sync != nil {
			s.sync.AppendHubUserLink(ctx, link)
		}
	}
	return nil
}
func (s *Service) syncDomainRoutes(ctx context.Context, hub *store.HubInstance, domains []string, now time.Time) error {
	if s.routes == nil || hub == nil {
		return nil
	}
	preservedAdminRoutes := make([]*store.HubDomainRoute, 0)
	if existing, err := s.routes.ListAll(ctx); err == nil {
		for _, route := range existing {
			if route != nil && route.HubID == hub.ID && strings.HasPrefix(strings.TrimSpace(route.ID), adminDomainRoutePrefix) {
				preservedAdminRoutes = append(preservedAdminRoutes, route)
			}
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
	return 0, false
}

func isAdminUserLink(link *store.HubUserLink) bool {
	if link == nil {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(link.ID), adminUserLinkPrefix)
}

func primaryUserLinkID(hubID, email string) string {
	return fmt.Sprintf("hul_user_%s_%s", strings.TrimSpace(hubID), hashToken(normalizeEmail(email))[:16])
}

func adminUserLinkID(email string) string {
	return adminUserLinkPrefix + hashToken(normalizeEmail(email))[:20]
}

func startOfDay(t time.Time) time.Time {
	local := t.Local()
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.Local)
}

func domainRouteID(hubID string, index int) string {
	return fmt.Sprintf("hdr_%s_%d", strings.TrimSpace(hubID), index)
}

func adminDomainRouteID(domain string) string {
	return adminDomainRoutePrefix + hashToken(normalizeCorporateEmailDomain(domain))[:20]
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
