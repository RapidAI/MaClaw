package center

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
	"log"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/brand"
	"github.com/RapidAI/CodeClaw/hub/internal/config"
	"github.com/RapidAI/CodeClaw/hub/internal/device"
	"github.com/RapidAI/CodeClaw/hub/internal/diagnostics"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const (
	systemKeyCenterBaseURL            = "center_base_url"
	systemKeyCenterRegistration       = "center_registration"
	systemKeyAdminEmail               = "admin_email"
	systemKeyInstallationID           = "hub_installation_id"
	systemKeyHubVisibility            = "hub_visibility"
	systemKeyHubEnrollmentMode        = "hub_enrollment_mode"
	systemKeyHubCorporateEmailDomain  = "hub_corporate_email_domain"
	systemKeyHubCorporateEmailDomains = "hub_corporate_email_domains"
	systemKeyHubAcceptPublicSignup    = "hub_accept_public_signup"
	systemKeyPublicBaseURL            = "server_public_base_url"
	systemKeyInvitationCodeRequired   = "invitation_code_required"
)

var tenantEmailDomainPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$`)

type SystemSettingsRepository interface {
	Set(ctx context.Context, key, valueJSON string) error
	Get(ctx context.Context, key string) (string, error)
}

type RegistrationState struct {
	Enabled                       bool                                             `json:"enabled"`
	BaseURL                       string                                           `json:"base_url"`
	BaseURLs                      []string                                         `json:"base_urls"`
	PublicBaseURL                 string                                           `json:"public_base_url"`
	Visibility                    string                                           `json:"visibility"`
	EnrollmentMode                string                                           `json:"enrollment_mode"`
	CorporateEmailDomain          string                                           `json:"corporate_email_domain"`
	CorporateEmailDomains         []string                                         `json:"corporate_email_domains,omitempty"`
	AcceptPublicSignup            bool                                             `json:"accept_public_signup"`
	AdvertisedBaseURL             string                                           `json:"advertised_base_url,omitempty"`
	Host                          string                                           `json:"host,omitempty"`
	Port                          int                                              `json:"port,omitempty"`
	RegisterOnStartup             bool                                             `json:"register_on_startup"`
	AdminEmailPresent             bool                                             `json:"admin_email_present"`
	Registered                    bool                                             `json:"registered"`
	PendingConfirmation           bool                                             `json:"pending_confirmation"`
	Disabled                      bool                                             `json:"disabled"`
	HubID                         string                                           `json:"hub_id,omitempty"`
	DisabledReason                string                                           `json:"disabled_reason,omitempty"`
	LastError                     string                                           `json:"last_error,omitempty"`
	ActiveBaseURL                 string                                           `json:"active_base_url,omitempty"`
	LastRegisteredAt              int64                                            `json:"last_registered_at,omitempty"`
	DigitalEmployeeAuthorization  *corelib.DigitalEmployeeAuthorization            `json:"digital_employee_authorization,omitempty"`
	DigitalEmployeeAuthorizations map[string]*corelib.DigitalEmployeeAuthorization `json:"digital_employee_authorizations,omitempty"`
}

type registrationRecord struct {
	Registered                    bool                                             `json:"registered"`
	PendingConfirmation           bool                                             `json:"pending_confirmation"`
	Disabled                      bool                                             `json:"disabled"`
	HubID                         string                                           `json:"hub_id,omitempty"`
	HubSecret                     string                                           `json:"hub_secret,omitempty"`
	DisabledReason                string                                           `json:"disabled_reason,omitempty"`
	LastError                     string                                           `json:"last_error,omitempty"`
	LastBaseURL                   string                                           `json:"last_base_url,omitempty"`
	LastRegisteredAt              int64                                            `json:"last_registered_at,omitempty"`
	DigitalEmployeeAuthorization  *corelib.DigitalEmployeeAuthorization            `json:"digital_employee_authorization,omitempty"`
	DigitalEmployeeAuthorizations map[string]*corelib.DigitalEmployeeAuthorization `json:"digital_employee_authorizations,omitempty"`
}

type centerQualityProbe struct {
	Routable      bool   `json:"routable"`
	QualityScore  int    `json:"quality_score"`
	ServiceStatus string `json:"service_status"`
}

type centerErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type registerHubRequest struct {
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
	AcceptPublicSignup    bool           `json:"accept_public_signup"`
	Capabilities          map[string]any `json:"capabilities"`
}

type registerHubResponse struct {
	HubID               string `json:"hub_id"`
	HubSecret           string `json:"hub_secret"`
	PendingConfirmation bool   `json:"pending_confirmation"`
	Message             string `json:"message"`
}

type syncUserLinkRequest struct {
	HubSecret string `json:"hub_secret"`
	TenantID  string `json:"tenant_id,omitempty"`
	Email     string `json:"email"`
	IsDefault bool   `json:"is_default"`
}

type entryResolveRequest struct {
	Email    string `json:"email,omitempty"`
	Domain   string `json:"domain,omitempty"`
	TenantID string `json:"tenant_id,omitempty"`
}

type entryResolveResponse struct {
	DefaultHubID string `json:"default_hub_id"`
}

type UserCounter interface {
	ListUsers(ctx context.Context) ([]*store.User, error)
}

type MachineCounter interface {
	ListAllMachines(ctx context.Context) ([]device.MachineRuntimeInfo, error)
}

type TenantLister interface {
	List(ctx context.Context) ([]*store.Tenant, error)
}

type Service struct {
	cfg      *config.Config
	settings SystemSettingsRepository
	client   *http.Client
	users    UserCounter
	machines MachineCounter
	tenants  TenantLister

	mu               sync.Mutex
	heartbeatStarted bool
	heartbeatCancel  context.CancelFunc
	recorder         *diagnostics.FailureEventRecorder
}

func NewService(cfg *config.Config, settings SystemSettingsRepository) *Service {
	return &Service{
		cfg:      cfg,
		settings: settings,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (s *Service) SetFailureEventRecorder(recorder *diagnostics.FailureEventRecorder) {
	s.recorder = recorder
}

func (s *Service) VerifyHubSecretHash(ctx context.Context, secretHash string) bool {
	secretHash = strings.TrimSpace(secretHash)
	if s == nil || secretHash == "" {
		return false
	}
	record, err := s.loadRegistration(ctx)
	if err != nil || strings.TrimSpace(record.HubSecret) == "" {
		return false
	}
	return hashHubSecret(record.HubSecret) == secretHash
}
func (s *Service) SetStatsProviders(users UserCounter, machines MachineCounter) {
	s.users = users
	s.machines = machines
}

func (s *Service) SetTenantRepository(tenants TenantLister) {
	s.tenants = tenants
}

func (s *Service) recordFailure(ctx context.Context, category, eventCode, message, entityID, email string, details map[string]any) {
	if s == nil || s.recorder == nil {
		return
	}
	s.recorder.Record(ctx, diagnostics.FailureEventInput{
		TenantID:  store.DefaultTenantID,
		Category:  category,
		EventCode: eventCode,
		Message:   message,
		EntityID:  entityID,
		Email:     email,
		Details:   details,
	})
}

func (s *Service) RefreshStatus(ctx context.Context) (*RegistrationState, error) {
	record, err := s.loadRegistration(ctx)
	if err != nil {
		return nil, err
	}
	if (record.Registered || record.PendingConfirmation || record.Disabled) && record.HubID != "" && record.HubSecret != "" {
		_ = s.sendHeartbeat(ctx)
	}
	return s.Status(ctx)
}

func (s *Service) Status(ctx context.Context) (*RegistrationState, error) {
	baseURLs, err := s.centerBaseURLs(ctx)
	if err != nil {
		return nil, err
	}
	publicBaseURL, err := s.publicBaseURL(ctx)
	if err != nil {
		return nil, err
	}
	advertisedBaseURL, advertisedHost, advertisedPort, err := s.advertisedEndpoint()
	if err != nil {
		return nil, err
	}

	record, err := s.loadRegistration(ctx)
	if err != nil {
		return nil, err
	}
	visibility, err := s.visibility(ctx)
	if err != nil {
		return nil, err
	}
	enrollmentMode, err := s.enrollmentMode(ctx)
	if err != nil {
		return nil, err
	}
	corporateEmailDomain, err := s.corporateEmailDomain(ctx)
	if err != nil {
		return nil, err
	}
	corporateEmailDomains, err := s.corporateEmailDomains(ctx)
	if err != nil {
		return nil, err
	}
	acceptPublicSignup, err := s.acceptPublicSignup(ctx)
	if err != nil {
		return nil, err
	}
	adminEmail, err := s.adminEmail(ctx)
	if err != nil {
		return nil, err
	}

	baseURL := ""
	if record.LastBaseURL != "" {
		baseURL = record.LastBaseURL
	} else if len(baseURLs) > 0 {
		baseURL = baseURLs[0]
	}

	return &RegistrationState{
		Enabled:                       s.cfg.Center.Enabled,
		BaseURL:                       baseURL,
		BaseURLs:                      baseURLs,
		PublicBaseURL:                 publicBaseURL,
		Visibility:                    visibility,
		EnrollmentMode:                enrollmentMode,
		CorporateEmailDomain:          corporateEmailDomain,
		CorporateEmailDomains:         corporateEmailDomains,
		AcceptPublicSignup:            acceptPublicSignup,
		AdvertisedBaseURL:             advertisedBaseURL,
		Host:                          advertisedHost,
		Port:                          advertisedPort,
		RegisterOnStartup:             s.cfg.Center.RegisterOnStartup,
		AdminEmailPresent:             adminEmail != "",
		Registered:                    record.Registered,
		PendingConfirmation:           record.PendingConfirmation,
		Disabled:                      record.Disabled,
		HubID:                         record.HubID,
		DisabledReason:                record.DisabledReason,
		LastError:                     record.LastError,
		ActiveBaseURL:                 record.LastBaseURL,
		LastRegisteredAt:              record.LastRegisteredAt,
		DigitalEmployeeAuthorization:  registrationDigitalEmployeeAuthorizationForStatus(record),
		DigitalEmployeeAuthorizations: registrationDigitalEmployeeAuthorizationsForStatus(record),
	}, nil
}
func (s *Service) SetBaseURL(ctx context.Context, baseURL string) (*RegistrationState, error) {
	baseURL = normalizeBaseURL(baseURL)
	if baseURL == "" {
		return nil, fmt.Errorf("hub center base url is required")
	}

	if err := s.settings.Set(ctx, systemKeyCenterBaseURL, mustJSON(map[string]string{"value": baseURL})); err != nil {
		return nil, err
	}

	record, err := s.loadRegistration(ctx)
	if err != nil {
		return nil, err
	}
	record.LastError = ""
	if err := s.saveRegistration(ctx, record); err != nil {
		return nil, err
	}

	return s.Status(ctx)
}

func (s *Service) SetPublicBaseURL(ctx context.Context, publicBaseURL string) (*RegistrationState, error) {
	publicBaseURL = normalizeBaseURL(publicBaseURL)
	if publicBaseURL == "" {
		return nil, fmt.Errorf("hub public base url is required")
	}
	if err := s.settings.Set(ctx, systemKeyPublicBaseURL, mustJSON(map[string]string{"value": publicBaseURL})); err != nil {
		return nil, err
	}
	return s.Status(ctx)
}

// GetPublicBaseURL returns the effective public base URL, preferring the
// database value over the config file fallback.
func (s *Service) GetPublicBaseURL(ctx context.Context) string {
	url, err := s.publicBaseURL(ctx)
	if err != nil {
		return normalizeBaseURL(s.cfg.Server.PublicBaseURL)
	}
	return url
}

func (s *Service) SetVisibility(ctx context.Context, visibility string) (*RegistrationState, error) {
	normalized := normalizeVisibility(visibility)
	if err := s.settings.Set(ctx, systemKeyHubVisibility, mustJSON(map[string]string{"value": normalized})); err != nil {
		return nil, err
	}
	return s.Status(ctx)
}

func (s *Service) SetEnrollmentMode(ctx context.Context, mode string) (*RegistrationState, error) {
	normalized := normalizeEnrollmentMode(mode)
	if err := s.settings.Set(ctx, systemKeyHubEnrollmentMode, mustJSON(map[string]string{"value": normalized})); err != nil {
		return nil, err
	}
	return s.Status(ctx)
}

func (s *Service) SetCorporateEmailDomain(ctx context.Context, domain string) (*RegistrationState, error) {
	normalized := normalizeCorporateEmailDomain(domain)
	if err := s.settings.Set(ctx, systemKeyHubCorporateEmailDomain, mustJSON(map[string]string{"value": normalized})); err != nil {
		return nil, err
	}
	if _, err := s.SetCorporateEmailDomains(ctx, []string{normalized}); err != nil {
		return nil, err
	}
	return s.Status(ctx)
}

func (s *Service) SetCorporateEmailDomains(ctx context.Context, domains []string) (*RegistrationState, error) {
	normalized := normalizeCorporateEmailDomains(domains)
	if err := s.settings.Set(ctx, systemKeyHubCorporateEmailDomains, mustJSON(map[string][]string{"values": normalized})); err != nil {
		return nil, err
	}
	primary := ""
	if len(normalized) > 0 {
		primary = normalized[0]
	}
	if err := s.settings.Set(ctx, systemKeyHubCorporateEmailDomain, mustJSON(map[string]string{"value": primary})); err != nil {
		return nil, err
	}
	return s.Status(ctx)
}

func (s *Service) SetAcceptPublicSignup(ctx context.Context, enabled bool) (*RegistrationState, error) {
	if err := s.settings.Set(ctx, systemKeyHubAcceptPublicSignup, mustJSON(map[string]bool{"value": enabled})); err != nil {
		return nil, err
	}
	return s.Status(ctx)
}

func (s *Service) Register(ctx context.Context, ownerEmail string) (*RegistrationState, error) {
	record, err := s.loadRegistration(ctx)
	if err != nil {
		return nil, err
	}
	if record.Disabled && record.HubID != "" && record.HubSecret != "" {
		if record.LastError == "" {
			record.LastError = "hub has been disabled by Hub Center"
			_ = s.saveRegistration(ctx, record)
		}
		return nil, fmt.Errorf("hub has been disabled by Hub Center")
	}

	baseURLs, err := s.orderedCenterBaseURLs(ctx, record.LastBaseURL)
	if err != nil {
		return nil, err
	}
	if len(baseURLs) == 0 {
		return nil, fmt.Errorf("hub center base url is required")
	}
	advertisedBaseURL, advertisedHost, advertisedPort, err := s.advertisedEndpoint()
	if err != nil {
		return nil, err
	}
	ownerEmail = normalizeEmail(ownerEmail)
	if ownerEmail == "" {
		storedAdminEmail, err := s.adminEmail(ctx)
		if err != nil {
			return nil, err
		}
		ownerEmail = normalizeEmail(storedAdminEmail)
	}
	if ownerEmail == "" {
		return nil, fmt.Errorf("admin email is required for hub registration")
	}
	installationID, err := s.installationID(ctx)
	if err != nil {
		return nil, err
	}
	visibility, err := s.visibility(ctx)
	if err != nil {
		return nil, err
	}
	enrollmentMode, err := s.enrollmentMode(ctx)
	if err != nil {
		return nil, err
	}
	corporateEmailDomain, err := s.corporateEmailDomain(ctx)
	if err != nil {
		return nil, err
	}
	corporateEmailDomains, err := s.corporateEmailDomains(ctx)
	if err != nil {
		return nil, err
	}
	acceptPublicSignup, err := s.acceptPublicSignup(ctx)
	if err != nil {
		return nil, err
	}

	reqBody := registerHubRequest{
		InstallationID:        installationID,
		OwnerEmail:            ownerEmail,
		Name:                  s.cfg.Hub.Name,
		Description:           s.cfg.Hub.Description,
		BaseURL:               advertisedBaseURL,
		Host:                  advertisedHost,
		Port:                  advertisedPort,
		Visibility:            visibility,
		EnrollmentMode:        enrollmentMode,
		CorporateEmailDomain:  corporateEmailDomain,
		CorporateEmailDomains: corporateEmailDomains,
		AcceptPublicSignup:    acceptPublicSignup,
		Capabilities:          s.registrationCapabilities(ctx),
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	var lastErr error
	for _, baseURL := range baseURLs {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/hubs/register", bytes.NewReader(payload))
		if err != nil {
			lastErr = err
			continue
		}
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err := s.client.Do(httpReq)
		if err != nil {
			lastErr = err
			continue
		}

		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if resp.StatusCode != http.StatusOK {
			var apiErr centerErrorPayload
			_ = json.Unmarshal(body, &apiErr)
			message := strings.TrimSpace(apiErr.Message)
			if message == "" {
				message = fmt.Sprintf("hub center register failed with status %d", resp.StatusCode)
			}
			if resp.StatusCode == http.StatusLocked {
				record.Disabled = true
				record.Registered = false
				record.PendingConfirmation = false
				record.DisabledReason = message
				record.LastError = message
				record.LastBaseURL = baseURL
				_ = s.saveRegistration(ctx, record)
				return nil, errors.New(message)
			}
			lastErr = errors.New(message)
			continue
		}

		var registerResp registerHubResponse
		if err := json.Unmarshal(body, &registerResp); err != nil {
			lastErr = err
			continue
		}
		if registerResp.HubID == "" || registerResp.HubSecret == "" {
			lastErr = fmt.Errorf("hub center register returned incomplete credentials")
			s.recordFailure(ctx, "registration", "incomplete_credentials", lastErr.Error(), "", ownerEmail, map[string]any{"base_url": baseURL})
			continue
		}

		record = registrationRecord{
			Registered:          !registerResp.PendingConfirmation,
			PendingConfirmation: registerResp.PendingConfirmation,
			Disabled:            false,
			HubID:               registerResp.HubID,
			HubSecret:           registerResp.HubSecret,
			DisabledReason:      "",
			LastError:           registerResp.Message,
			LastBaseURL:         baseURL,
			LastRegisteredAt:    time.Now().Unix(),
		}
		if err := s.saveRegistration(ctx, record); err != nil {
			return nil, err
		}
		s.startHeartbeatLoop()
		return s.Status(ctx)
	}

	if lastErr != nil {
		_ = s.updateRegistrationError(ctx, lastErr.Error())
		return nil, lastErr
	}
	msg := "hub center register failed"
	s.recordFailure(ctx, "registration", "register_failed", msg, record.HubID, ownerEmail, nil)
	return nil, errors.New(msg)
}
func (s *Service) StartBackgroundSync() {
	if !s.cfg.Center.Enabled {
		return
	}

	ctx := context.Background()
	record, err := s.loadRegistration(ctx)
	if err != nil {
		return
	}
	if !record.Registered && !record.PendingConfirmation && !record.Disabled && s.cfg.Center.RegisterOnStartup {
		if adminEmail, err := s.adminEmail(ctx); err == nil && adminEmail != "" {
			if _, err := s.Register(ctx, adminEmail); err == nil {
				return
			}
		}
	}
	if (record.Registered || record.PendingConfirmation || record.Disabled) && record.HubID != "" && record.HubSecret != "" {
		s.startHeartbeatLoop()
	}
}

func (s *Service) baseURL(ctx context.Context) (string, error) {
	baseURLs, err := s.centerBaseURLs(ctx)
	if err != nil {
		return "", err
	}
	if len(baseURLs) == 0 {
		return "", nil
	}
	record, err := s.loadRegistration(ctx)
	if err == nil && strings.TrimSpace(record.LastBaseURL) != "" {
		return normalizeBaseURL(record.LastBaseURL), nil
	}
	return baseURLs[0], nil
}

func (s *Service) centerBaseURLs(ctx context.Context) ([]string, error) {
	raw, err := s.settings.Get(ctx, systemKeyCenterBaseURL)
	if err != nil {
		return nil, err
	}
	values := make([]string, 0, len(s.cfg.Center.BaseURLs)+1)
	if raw != "" {
		var payload struct {
			Value string `json:"value"`
		}
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			return nil, err
		}
		if payload.Value != "" {
			values = append(values, payload.Value)
		}
	}
	values = append(values, s.cfg.Center.BaseURLs...)
	values = append(values, s.cfg.Center.BaseURL)
	return normalizeBaseURLs(values), nil
}

func (s *Service) orderedCenterBaseURLs(ctx context.Context, preferred string) ([]string, error) {
	baseURLs, err := s.centerBaseURLs(ctx)
	if err != nil || len(baseURLs) <= 1 {
		return baseURLs, err
	}
	preferred = normalizeBaseURL(preferred)
	type candidate struct {
		BaseURL   string
		Reachable bool
		Routable  bool
		Score     int
		Preferred bool
	}
	items := make([]candidate, 0, len(baseURLs))
	for _, baseURL := range baseURLs {
		probe := s.probeCenterQuality(ctx, baseURL)
		items = append(items, candidate{BaseURL: baseURL, Reachable: probe.Reachable, Routable: probe.Routable, Score: probe.QualityScore, Preferred: baseURL == preferred})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Reachable != items[j].Reachable {
			return items[i].Reachable
		}
		if items[i].Routable != items[j].Routable {
			return items[i].Routable
		}
		leftScore := items[i].Score
		rightScore := items[j].Score
		if items[i].Preferred {
			leftScore += 5
		}
		if items[j].Preferred {
			rightScore += 5
		}
		if leftScore != rightScore {
			return leftScore > rightScore
		}
		return items[i].BaseURL < items[j].BaseURL
	})
	ordered := make([]string, 0, len(items))
	for _, item := range items {
		ordered = append(ordered, item.BaseURL)
	}
	return ordered, nil
}

func (s *Service) probeCenterQuality(ctx context.Context, baseURL string) struct {
	Reachable, Routable bool
	QualityScore        int
} {
	result := struct {
		Reachable, Routable bool
		QualityScore        int
	}{}
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/api/client/quality", nil)
	if err != nil {
		return result
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return result
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return result
	}
	var payload centerQualityProbe
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return result
	}
	result.Reachable = true
	result.Routable = payload.Routable
	result.QualityScore = payload.QualityScore
	return result
}
func (s *Service) publicBaseURL(ctx context.Context) (string, error) {
	raw, err := s.settings.Get(ctx, systemKeyPublicBaseURL)
	if err != nil {
		return "", err
	}
	if raw == "" {
		return normalizeBaseURL(s.cfg.Server.PublicBaseURL), nil
	}

	var payload struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return "", err
	}
	if payload.Value == "" {
		return normalizeBaseURL(s.cfg.Server.PublicBaseURL), nil
	}
	return normalizeBaseURL(payload.Value), nil
}

func (s *Service) adminEmail(ctx context.Context) (string, error) {
	raw, err := s.settings.Get(ctx, systemKeyAdminEmail)
	if err != nil {
		return "", err
	}
	if raw == "" {
		return "", nil
	}

	var payload struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return "", err
	}
	return normalizeEmail(payload.Value), nil
}

func (s *Service) installationID(ctx context.Context) (string, error) {
	raw, err := s.settings.Get(ctx, systemKeyInstallationID)
	if err != nil {
		return "", err
	}
	if raw != "" {
		var payload struct {
			Value string `json:"value"`
		}
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			return "", err
		}
		if strings.TrimSpace(payload.Value) != "" {
			return strings.TrimSpace(payload.Value), nil
		}
	}

	id, err := randomInstallationID()
	if err != nil {
		return "", err
	}
	if err := s.settings.Set(ctx, systemKeyInstallationID, mustJSON(map[string]string{"value": id})); err != nil {
		return "", err
	}
	return id, nil
}

func (s *Service) visibility(ctx context.Context) (string, error) {
	raw, err := s.settings.Get(ctx, systemKeyHubVisibility)
	if err != nil {
		return "", err
	}
	if raw == "" {
		return normalizeVisibility(s.cfg.Hub.Visibility), nil
	}

	var payload struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return "", err
	}
	return normalizeVisibility(payload.Value), nil
}

func (s *Service) enrollmentMode(ctx context.Context) (string, error) {
	raw, err := s.settings.Get(ctx, systemKeyHubEnrollmentMode)
	if err != nil {
		return "", err
	}
	if raw == "" {
		return normalizeEnrollmentMode(s.cfg.Identity.EnrollmentMode), nil
	}

	var payload struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return "", err
	}
	return normalizeEnrollmentMode(payload.Value), nil
}

func (s *Service) corporateEmailDomain(ctx context.Context) (string, error) {
	domains, err := s.corporateEmailDomains(ctx)
	if err == nil && len(domains) > 0 {
		return domains[0], nil
	}
	raw, err := s.settings.Get(ctx, systemKeyHubCorporateEmailDomain)
	if err != nil {
		return "", err
	}
	if raw == "" {
		return normalizeCorporateEmailDomain(s.cfg.Hub.CorporateEmailDomain), nil
	}

	var payload struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return "", err
	}
	return normalizeCorporateEmailDomain(payload.Value), nil
}

func (s *Service) corporateEmailDomains(ctx context.Context) ([]string, error) {
	raw, err := s.settings.Get(ctx, systemKeyHubCorporateEmailDomains)
	if err != nil {
		return nil, err
	}
	if raw != "" {
		var payload struct {
			Values []string `json:"values"`
		}
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			return nil, err
		}
		return normalizeCorporateEmailDomains(payload.Values), nil
	}
	configured := normalizeCorporateEmailDomains(s.cfg.Hub.CorporateEmailDomains)
	legacySettingRaw, err := s.settings.Get(ctx, systemKeyHubCorporateEmailDomain)
	if err != nil {
		return nil, err
	}
	legacy := ""
	if legacySettingRaw != "" {
		var payload struct {
			Value string `json:"value"`
		}
		if err := json.Unmarshal([]byte(legacySettingRaw), &payload); err != nil {
			return nil, err
		}
		legacy = normalizeCorporateEmailDomain(payload.Value)
	} else {
		legacy = normalizeCorporateEmailDomain(s.cfg.Hub.CorporateEmailDomain)
	}
	if legacy != "" {
		configured = normalizeCorporateEmailDomains(append(configured, legacy))
	}
	return configured, nil
}

func (s *Service) acceptPublicSignup(ctx context.Context) (bool, error) {
	raw, err := s.settings.Get(ctx, systemKeyHubAcceptPublicSignup)
	if err != nil {
		return false, err
	}
	if raw != "" {
		var payload struct {
			Value bool `json:"value"`
		}
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			return false, err
		}
		return payload.Value, nil
	}
	domains, err := s.corporateEmailDomains(ctx)
	if err != nil {
		return false, err
	}
	visibility, err := s.visibility(ctx)
	if err != nil {
		return false, err
	}
	return len(domains) == 0 && isPublicSignupVisibility(visibility), nil
}

func (s *Service) invitationCodeRequired(ctx context.Context) (bool, error) {
	raw, err := s.settings.Get(ctx, systemKeyInvitationCodeRequired)
	if err != nil {
		return false, err
	}
	if raw == "" {
		return false, nil
	}

	var payload struct {
		Value bool `json:"value"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return false, err
	}
	return payload.Value, nil
}

func LoadDigitalEmployeeAuthorization(ctx context.Context, settings SystemSettingsRepository) *corelib.DigitalEmployeeAuthorization {
	tenantID := ""
	if scoped, ok := settings.(interface{ TenantID() string }); ok {
		tenantID = scoped.TenantID()
	}
	return LoadDigitalEmployeeAuthorizationForTenant(ctx, settings, tenantID)
}

func LoadDigitalEmployeeAuthorizationForTenant(ctx context.Context, settings SystemSettingsRepository, tenantID string) *corelib.DigitalEmployeeAuthorization {
	if settings == nil {
		return nil
	}
	raw, err := settings.Get(ctx, systemKeyCenterRegistration)
	if err != nil || strings.TrimSpace(raw) == "" {
		return nil
	}
	var record registrationRecord
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		return nil
	}
	if record.Disabled {
		return disabledDigitalEmployeeAuthorizationFrom(recordDigitalEmployeeAuthorizationForTenant(record, tenantID))
	}
	if !record.Registered || record.PendingConfirmation {
		// Hub is not in a registered state — return an explicit "no authorization"
		// object so the client can distinguish this from "Hub hasn't synced yet" (nil).
		return &corelib.DigitalEmployeeAuthorization{Active: false, Reason: "not_registered"}
	}
	auth := recordDigitalEmployeeAuthorizationForTenant(record, tenantID)
	if auth == nil {
		// Hub is registered but hasn't received authorization from HubCenter yet
		// (first heartbeat hasn't completed). Return nil so the client preserves
		// any previously cached authorization rather than clearing it.
		return nil
	}
	normalized := corelib.NormalizeDigitalEmployeeAuthorization(*auth, time.Now().UTC())
	return &normalized
}

func disabledDigitalEmployeeAuthorization() *corelib.DigitalEmployeeAuthorization {
	return disabledDigitalEmployeeAuthorizationFrom(nil)
}

func disabledDigitalEmployeeAuthorizationFrom(existing *corelib.DigitalEmployeeAuthorization) *corelib.DigitalEmployeeAuthorization {
	auth := corelib.DigitalEmployeeAuthorization{Enabled: false, Reason: "disabled"}
	if existing != nil {
		auth = *existing
		auth.Enabled = false
		auth.Active = false
		auth.Reason = "disabled"
	}
	normalized := corelib.NormalizeDigitalEmployeeAuthorization(auth, time.Now().UTC())
	return &normalized
}

func registrationDigitalEmployeeAuthorizationForStatus(record registrationRecord) *corelib.DigitalEmployeeAuthorization {
	if record.Disabled {
		return disabledDigitalEmployeeAuthorizationFrom(record.DigitalEmployeeAuthorization)
	}
	return normalizeDeAuthForStatus(record.DigitalEmployeeAuthorization)
}

func registrationDigitalEmployeeAuthorizationsForStatus(record registrationRecord) map[string]*corelib.DigitalEmployeeAuthorization {
	if len(record.DigitalEmployeeAuthorizations) == 0 {
		return nil
	}
	out := make(map[string]*corelib.DigitalEmployeeAuthorization, len(record.DigitalEmployeeAuthorizations))
	for tenantID, auth := range record.DigitalEmployeeAuthorizations {
		tenantID = strings.TrimSpace(tenantID)
		if tenantID == "" {
			continue
		}
		if record.Disabled {
			out[tenantID] = disabledDigitalEmployeeAuthorizationFrom(auth)
			continue
		}
		out[tenantID] = normalizeDeAuthForStatus(auth)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func recordDigitalEmployeeAuthorizationForTenant(record registrationRecord, tenantID string) *corelib.DigitalEmployeeAuthorization {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID != "" {
		if record.DigitalEmployeeAuthorizations != nil {
			if auth := record.DigitalEmployeeAuthorizations[tenantID]; auth != nil {
				return auth
			}
		}
		if tenantID != store.DefaultTenantID {
			return nil
		}
	}
	return record.DigitalEmployeeAuthorization
}

func mergeTenantDigitalEmployeeAuthorizations(record *registrationRecord, incoming map[string]*corelib.DigitalEmployeeAuthorization) {
	if record == nil || len(incoming) == 0 {
		return
	}
	if record.DigitalEmployeeAuthorizations == nil {
		record.DigitalEmployeeAuthorizations = map[string]*corelib.DigitalEmployeeAuthorization{}
	}
	for tenantID, incomingAuth := range incoming {
		tenantID = strings.TrimSpace(tenantID)
		if tenantID == "" || incomingAuth == nil {
			continue
		}
		auth := corelib.NormalizeDigitalEmployeeAuthorization(*incomingAuth, time.Now().UTC())
		local := record.DigitalEmployeeAuthorizations[tenantID]
		if shouldAcceptAuthorizationUpdate(local, &auth) {
			record.DigitalEmployeeAuthorizations[tenantID] = &auth
		}
	}
}

func disabledDigitalEmployeeAuthorizationsFrom(existing map[string]*corelib.DigitalEmployeeAuthorization) map[string]*corelib.DigitalEmployeeAuthorization {
	if len(existing) == 0 {
		return existing
	}
	out := make(map[string]*corelib.DigitalEmployeeAuthorization, len(existing))
	for tenantID, auth := range existing {
		tenantID = strings.TrimSpace(tenantID)
		if tenantID == "" {
			continue
		}
		out[tenantID] = disabledDigitalEmployeeAuthorizationFrom(auth)
	}
	return out
}

// normalizeDeAuthForStatus re-normalizes the stored authorization against the
// current time so that the Active/Reason fields reflect real-time expiry state,
// not the state at the time of the last heartbeat.
func normalizeDeAuthForStatus(auth *corelib.DigitalEmployeeAuthorization) *corelib.DigitalEmployeeAuthorization {
	if auth == nil {
		return nil
	}
	normalized := corelib.NormalizeDigitalEmployeeAuthorization(*auth, time.Now().UTC())
	return &normalized
}

func (s *Service) loadRegistration(ctx context.Context) (registrationRecord, error) {
	raw, err := s.settings.Get(ctx, systemKeyCenterRegistration)
	if err != nil {
		return registrationRecord{}, err
	}
	if raw == "" {
		return registrationRecord{}, nil
	}

	var record registrationRecord
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		return registrationRecord{}, err
	}
	return record, nil
}

func (s *Service) saveRegistration(ctx context.Context, record registrationRecord) error {
	return s.settings.Set(ctx, systemKeyCenterRegistration, mustJSON(record))
}

func (s *Service) updateRegistrationError(ctx context.Context, message string) error {
	record, err := s.loadRegistration(ctx)
	if err != nil {
		return err
	}
	record.LastError = strings.TrimSpace(message)
	return s.saveRegistration(ctx, record)
}

func (s *Service) startHeartbeatLoop() {
	if !s.cfg.Center.Enabled {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.heartbeatStarted {
		return
	}

	interval := time.Duration(s.cfg.Center.HeartbeatIntervalSec) * time.Second
	if interval <= 0 {
		interval = 30 * time.Second
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.heartbeatStarted = true
	s.heartbeatCancel = cancel

	go func() {
		// Send an immediate heartbeat on startup so that
		// digital_employee_authorization is available as soon as possible,
		// rather than waiting for the first ticker interval (30s).
		_ = s.sendHeartbeat(ctx)

		// Start the ticker after the immediate heartbeat completes, so the
		// first tick is a full interval after the initial sync attempt.
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		defer func() {
			s.mu.Lock()
			s.heartbeatStarted = false
			s.heartbeatCancel = nil
			s.mu.Unlock()
		}()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = s.sendHeartbeat(ctx)
			}
		}
	}()
}

func (s *Service) SyncUserRoute(ctx context.Context, email string, tenantIDOpt ...string) error {
	email = normalizeEmail(email)
	if email == "" {
		return nil
	}
	tenantID := store.DefaultTenantID
	if len(tenantIDOpt) > 0 && strings.TrimSpace(tenantIDOpt[0]) != "" {
		tenantID = strings.TrimSpace(tenantIDOpt[0])
	}
	record, err := s.loadRegistration(ctx)
	if err != nil {
		return err
	}
	if (!record.Registered && !record.PendingConfirmation && !record.Disabled) || record.HubID == "" || record.HubSecret == "" {
		return nil
	}
	baseURLs, err := s.orderedCenterBaseURLs(ctx, record.LastBaseURL)
	if err != nil {
		return err
	}
	if len(baseURLs) == 0 {
		return fmt.Errorf("hub center base url is required")
	}
	payload, err := json.Marshal(syncUserLinkRequest{HubSecret: record.HubSecret, TenantID: tenantID, Email: email, IsDefault: tenantID == store.DefaultTenantID})
	if err != nil {
		return err
	}

	var lastErr error
	for _, baseURL := range baseURLs {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/hubs/"+url.PathEscape(record.HubID)+"/user-links/sync", bytes.NewReader(payload))
		if err != nil {
			lastErr = err
			continue
		}
		httpReq.Header.Set("Content-Type", "application/json")
		resp, err := s.client.Do(httpReq)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if resp.StatusCode == http.StatusOK {
			if record.LastBaseURL != baseURL {
				record.LastBaseURL = baseURL
				_ = s.saveRegistration(context.Background(), record)
			}
			return nil
		}
		var apiErr centerErrorPayload
		_ = json.Unmarshal(body, &apiErr)
		message := strings.TrimSpace(apiErr.Message)
		if message == "" {
			message = fmt.Sprintf("hub center user-route sync failed with status %d", resp.StatusCode)
		}
		lastErr = errors.New(message)
	}
	if lastErr != nil {
		s.recordFailure(ctx, "sync", "user_route_sync_failed", lastErr.Error(), record.HubID, email, nil)
	}
	return lastErr
}

// SyncInvitationCodesToCenter registers newly generated invitation codes with
// HubCenter so that it can route clients providing these codes to this Hub.
func (s *Service) SyncInvitationCodesToCenter(ctx context.Context, codes []string, tenantID string) error {
	if s == nil || len(codes) == 0 {
		return nil
	}
	record, err := s.loadRegistration(ctx)
	if err != nil {
		return err
	}
	if (!record.Registered && !record.PendingConfirmation && !record.Disabled) || record.HubID == "" || record.HubSecret == "" {
		return nil
	}
	baseURLs, err := s.orderedCenterBaseURLs(ctx, record.LastBaseURL)
	if err != nil {
		return err
	}
	if len(baseURLs) == 0 {
		return nil // no hub center configured, skip silently
	}

	payload, err := json.Marshal(map[string]any{
		"hub_secret": record.HubSecret,
		"codes":      codes,
		"tenant_id":  tenantID,
	})
	if err != nil {
		return err
	}

	var lastErr error
	for _, baseURL := range baseURLs {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/hubs/"+url.PathEscape(record.HubID)+"/invitation-codes/sync", bytes.NewReader(payload))
		if err != nil {
			lastErr = err
			continue
		}
		httpReq.Header.Set("Content-Type", "application/json")
		resp, err := s.client.Do(httpReq)
		if err != nil {
			lastErr = err
			continue
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return nil
		}
		lastErr = fmt.Errorf("hub center invitation-codes sync failed with status %d", resp.StatusCode)
	}
	return lastErr
}

// DeleteInvitationCodesFromCenter removes consumed/deleted invitation codes
// from HubCenter's routing table.
func (s *Service) DeleteInvitationCodesFromCenter(ctx context.Context, codes []string) error {
	if s == nil || len(codes) == 0 {
		return nil
	}
	record, err := s.loadRegistration(ctx)
	if err != nil {
		return err
	}
	if (!record.Registered && !record.PendingConfirmation && !record.Disabled) || record.HubID == "" || record.HubSecret == "" {
		return nil
	}
	baseURLs, err := s.orderedCenterBaseURLs(ctx, record.LastBaseURL)
	if err != nil {
		return err
	}
	if len(baseURLs) == 0 {
		return nil
	}

	payload, err := json.Marshal(map[string]any{
		"hub_secret": record.HubSecret,
		"codes":      codes,
	})
	if err != nil {
		return err
	}

	var lastErr error
	for _, baseURL := range baseURLs {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, baseURL+"/api/hubs/"+url.PathEscape(record.HubID)+"/invitation-codes/sync", bytes.NewReader(payload))
		if err != nil {
			lastErr = err
			continue
		}
		httpReq.Header.Set("Content-Type", "application/json")
		resp, err := s.client.Do(httpReq)
		if err != nil {
			lastErr = err
			continue
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return nil
		}
		lastErr = fmt.Errorf("hub center invitation-codes delete failed with status %d", resp.StatusCode)
	}
	return lastErr
}

func (s *Service) AllowsUserRoute(ctx context.Context, email string, tenantIDOpt ...string) (bool, string, error) {
	if s == nil {
		return true, "", nil
	}
	email = normalizeEmail(email)
	if email == "" {
		return true, "", nil
	}
	record, err := s.loadRegistration(ctx)
	if err != nil {
		return false, "", err
	}
	if (!record.Registered && !record.PendingConfirmation && !record.Disabled) || record.HubID == "" || record.HubSecret == "" {
		return true, "", nil
	}
	baseURLs, err := s.orderedCenterBaseURLs(ctx, record.LastBaseURL)
	if err != nil {
		return false, "", err
	}
	if len(baseURLs) == 0 {
		return false, "", fmt.Errorf("hub center base url is required")
	}
	tenantID := store.DefaultTenantID
	if len(tenantIDOpt) > 0 && strings.TrimSpace(tenantIDOpt[0]) != "" {
		tenantID = store.NormalizeTenantID(tenantIDOpt[0])
	}
	payload, err := json.Marshal(entryResolveRequest{Email: email, Domain: extractEmailDomain(email), TenantID: tenantID})
	if err != nil {
		return false, "", err
	}

	var lastErr error
	for _, baseURL := range baseURLs {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/entry/resolve-domain", bytes.NewReader(payload))
		if err != nil {
			lastErr = err
			continue
		}
		httpReq.Header.Set("Content-Type", "application/json")
		resp, err := s.client.Do(httpReq)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("hub center entry resolve failed with status %d", resp.StatusCode)
			continue
		}
		var resolved entryResolveResponse
		if err := json.Unmarshal(body, &resolved); err != nil {
			lastErr = err
			continue
		}
		if record.LastBaseURL != baseURL {
			record.LastBaseURL = baseURL
			_ = s.saveRegistration(context.Background(), record)
		}
		targetHubID := strings.TrimSpace(resolved.DefaultHubID)
		if targetHubID == "" || targetHubID == strings.TrimSpace(record.HubID) {
			return true, targetHubID, nil
		}
		return false, targetHubID, nil
	}
	return false, "", lastErr
}

func (s *Service) DeleteUserRoute(ctx context.Context, email string, tenantIDOpt ...string) error {
	if s == nil {
		return nil
	}
	email = strings.ToLower(strings.TrimSpace(email))
	tenantID := store.DefaultTenantID
	if len(tenantIDOpt) > 0 && strings.TrimSpace(tenantIDOpt[0]) != "" {
		tenantID = store.NormalizeTenantID(tenantIDOpt[0])
	}
	if email == "" {
		return nil
	}
	record, err := s.loadRegistration(ctx)
	if err != nil {
		return err
	}
	if (!record.Registered && !record.PendingConfirmation && !record.Disabled) || record.HubID == "" || record.HubSecret == "" {
		return nil
	}
	baseURLs, err := s.orderedCenterBaseURLs(ctx, record.LastBaseURL)
	if err != nil {
		return err
	}
	if len(baseURLs) == 0 {
		return fmt.Errorf("hub center base url is required")
	}
	payload, err := json.Marshal(syncUserLinkRequest{HubSecret: record.HubSecret, TenantID: tenantID, Email: email, IsDefault: tenantID == store.DefaultTenantID})
	if err != nil {
		return err
	}

	var lastErr error
	for _, baseURL := range baseURLs {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, baseURL+"/api/hubs/"+url.PathEscape(record.HubID)+"/user-links/sync", bytes.NewReader(payload))
		if err != nil {
			lastErr = err
			continue
		}
		httpReq.Header.Set("Content-Type", "application/json")
		resp, err := s.client.Do(httpReq)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if resp.StatusCode == http.StatusOK {
			if record.LastBaseURL != baseURL {
				record.LastBaseURL = baseURL
				_ = s.saveRegistration(context.Background(), record)
			}
			return nil
		}
		var apiErr centerErrorPayload
		_ = json.Unmarshal(body, &apiErr)
		message := strings.TrimSpace(apiErr.Message)
		if message == "" {
			message = fmt.Sprintf("hub center user-route delete failed with status %d", resp.StatusCode)
		}
		lastErr = errors.New(message)
	}
	if lastErr != nil {
		s.recordFailure(ctx, "sync", "user_route_delete_failed", lastErr.Error(), record.HubID, email, nil)
	}
	return lastErr
}

func (s *Service) registrationCapabilities(ctx context.Context) map[string]any {
	caps := map[string]any{
		"supports_remote_control":      true,
		"supports_pwa":                 true,
		"supports_tools":               brandTools(),
		"supports_user_data_migration": true,
		"brand":                        brand.Current().DisplayName,
	}
	if s != nil && s.users != nil {
		if users, err := s.users.ListUsers(ctx); err == nil {
			seen := map[string]struct{}{}
			tenantSeen := map[string]map[string]struct{}{}
			for _, user := range users {
				if user == nil || strings.TrimSpace(user.Email) == "" {
					continue
				}
				email := strings.ToLower(strings.TrimSpace(user.Email))
				if email == "" {
					continue
				}
				seen[email] = struct{}{}
				tenantID := strings.TrimSpace(user.TenantID)
				if tenantID == "" {
					tenantID = store.DefaultTenantID
				}
				if tenantSeen[tenantID] == nil {
					tenantSeen[tenantID] = map[string]struct{}{}
				}
				tenantSeen[tenantID][email] = struct{}{}
			}
			emails := make([]string, 0, len(seen))
			for email := range seen {
				emails = append(emails, email)
			}
			sort.Strings(emails)
			tenantEmails := map[string][]string{}
			tenantCounts := map[string]int{}
			for tenantID, values := range tenantSeen {
				items := make([]string, 0, len(values))
				for email := range values {
					items = append(items, email)
				}
				sort.Strings(items)
				tenantEmails[tenantID] = items
				tenantCounts[tenantID] = len(items)
			}
			caps["user_count"] = len(seen)
			caps["user_emails"] = emails
			caps["tenant_user_counts"] = tenantCounts
			caps["tenant_user_emails"] = tenantEmails
		}
	}
	if s != nil && s.tenants != nil {
		if tenants, err := s.tenants.List(ctx); err == nil {
			tenantDomains := tenantDomainsCapability(caps)
			tenantNames := map[string]string{}
			for _, tenant := range tenants {
				if tenant == nil || tenant.DeletedAt != nil || !strings.EqualFold(strings.TrimSpace(tenant.Status), "active") {
					continue
				}
				tenantID := strings.TrimSpace(tenant.ID)
				if tenantID == "" {
					tenantID = store.DefaultTenantID
				}
				if name := strings.TrimSpace(tenant.Name); name != "" {
					tenantNames[tenantID] = name
				}
				for _, domain := range configuredTenantDomains(tenant) {
					if domain == "" {
						continue
					}
					tenantDomains[tenantID] = appendUniqueSorted(tenantDomains[tenantID], domain)
				}
			}
			if len(tenantDomains) > 0 {
				caps["tenant_domains"] = tenantDomains
				caps["tenant_domain_source"] = "configured"
			}
			if len(tenantNames) > 0 {
				caps["tenant_names"] = tenantNames
			}
		}
	}
	if s != nil && s.machines != nil {
		if machines, err := s.machines.ListAllMachines(ctx); err == nil {
			tenantMachineCounts := map[string]int{}
			for _, machine := range machines {
				tenantID := strings.TrimSpace(machine.TenantID)
				if tenantID == "" {
					tenantID = store.DefaultTenantID
				}
				tenantMachineCounts[tenantID]++
			}
			caps["machine_count"] = len(machines)
			caps["tenant_machine_counts"] = tenantMachineCounts
		}
	}
	return caps
}

func (s *Service) sendHeartbeat(ctx context.Context) error {
	record, err := s.loadRegistration(ctx)
	if err != nil {
		return err
	}
	if (!record.Registered && !record.PendingConfirmation && !record.Disabled) || record.HubID == "" || record.HubSecret == "" {
		return nil
	}
	baseURLs, err := s.orderedCenterBaseURLs(ctx, record.LastBaseURL)
	if err != nil {
		return err
	}
	if len(baseURLs) == 0 {
		return fmt.Errorf("hub center base url is required")
	}

	invCodeRequired, _ := s.invitationCodeRequired(ctx)
	visibility, _ := s.visibility(ctx)
	enrollmentMode, _ := s.enrollmentMode(ctx)
	corporateEmailDomain, _ := s.corporateEmailDomain(ctx)
	corporateEmailDomains, _ := s.corporateEmailDomains(ctx)
	acceptPublicSignup, _ := s.acceptPublicSignup(ctx)
	advertisedBaseURL, advertisedHost, advertisedPort, advErr := s.advertisedEndpoint()
	if advErr != nil {
		advertisedBaseURL = ""
		advertisedHost = ""
		advertisedPort = 0
	}

	payload, err := json.Marshal(map[string]any{
		"hub_secret":               record.HubSecret,
		"invitation_code_required": invCodeRequired,
		"base_url":                 advertisedBaseURL,
		"host":                     advertisedHost,
		"port":                     advertisedPort,
		"visibility":               visibility,
		"enrollment_mode":          enrollmentMode,
		"corporate_email_domain":   corporateEmailDomain,
		"corporate_email_domains":  corporateEmailDomains,
		"accept_public_signup":     acceptPublicSignup,
		"capabilities":             s.registrationCapabilities(ctx),
	})
	if err != nil {
		return err
	}

	var lastErr error
	attempts := 0
	unregisteredCount := 0
	notReadyCount := 0
	for _, baseURL := range baseURLs {
		attempts++
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/hubs/"+record.HubID+"/heartbeat", bytes.NewReader(payload))
		if err != nil {
			lastErr = err
			continue
		}
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err := s.client.Do(httpReq)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}

		if resp.StatusCode == http.StatusOK {
			var okResp struct {
				DigitalEmployeeAuthorization  *corelib.DigitalEmployeeAuthorization            `json:"digital_employee_authorization"`
				DigitalEmployeeAuthorizations map[string]*corelib.DigitalEmployeeAuthorization `json:"digital_employee_authorizations"`
			}
			_ = json.Unmarshal(body, &okResp)
			if okResp.DigitalEmployeeAuthorization != nil {
				auth := corelib.NormalizeDigitalEmployeeAuthorization(*okResp.DigitalEmployeeAuthorization, time.Now().UTC())
				if shouldAcceptAuthorizationUpdate(record.DigitalEmployeeAuthorization, &auth) {
					record.DigitalEmployeeAuthorization = &auth
				} else {
					log.Printf("[center] heartbeat: rejected ve authorization downgrade from %s (incoming quota=%d active=%v, local quota=%d active=%v)",
						baseURL, auth.Quota, auth.Active, record.DigitalEmployeeAuthorization.Quota, record.DigitalEmployeeAuthorization.Active)
				}
			}
			mergeTenantDigitalEmployeeAuthorizations(&record, okResp.DigitalEmployeeAuthorizations)
			record.Registered = true
			record.PendingConfirmation = false
			record.Disabled = false
			record.DisabledReason = ""
			record.LastError = ""
			record.LastBaseURL = baseURL
			record.LastRegisteredAt = time.Now().Unix()

			return s.saveRegistration(context.Background(), record)
		}

		var apiErr centerErrorPayload
		_ = json.Unmarshal(body, &apiErr)
		message := strings.TrimSpace(apiErr.Message)
		if message == "" {
			message = fmt.Sprintf("hub center heartbeat failed with status %d", resp.StatusCode)
		}
		switch resp.StatusCode {
		case http.StatusUnauthorized, http.StatusNotFound:
			if strings.EqualFold(strings.TrimSpace(apiErr.Code), "HUB_UNREGISTERED") || apiErr.Code == "" {
				unregisteredCount++
				lastErr = errors.New(message)
				continue
			}
		case http.StatusConflict:
			if strings.EqualFold(strings.TrimSpace(apiErr.Code), "HUB_NOT_READY_ON_NODE") {
				notReadyCount++
				lastErr = errors.New(message)
				continue
			}
			record.Registered = false
			record.PendingConfirmation = true
			record.Disabled = false
			record.DisabledReason = ""
			record.LastError = message
			record.LastBaseURL = baseURL
			record.LastRegisteredAt = time.Now().Unix()
			return s.saveRegistration(context.Background(), record)
		case http.StatusLocked:
			record.Registered = false
			record.PendingConfirmation = false
			record.Disabled = true
			record.DisabledReason = message
			record.DigitalEmployeeAuthorization = disabledDigitalEmployeeAuthorization()
			record.DigitalEmployeeAuthorizations = disabledDigitalEmployeeAuthorizationsFrom(record.DigitalEmployeeAuthorizations)
			record.LastError = message
			record.LastBaseURL = baseURL
			record.LastRegisteredAt = time.Now().Unix()
			return s.saveRegistration(context.Background(), record)
		}
		lastErr = errors.New(message)
	}

	if attempts > 0 && unregisteredCount == attempts {
		record.Registered = false
		record.PendingConfirmation = false
		record.Disabled = false
		record.HubID = ""
		record.HubSecret = ""
		record.DisabledReason = ""
		record.DigitalEmployeeAuthorization = nil
		record.DigitalEmployeeAuthorizations = nil
		msg := "hub registration was removed by Hub Center"
		s.recordFailure(ctx, "heartbeat", "hub_unregistered", msg, record.HubID, "", nil)
		record.LastError = msg
		record.LastBaseURL = ""
		record.LastRegisteredAt = 0
		return s.saveRegistration(context.Background(), record)
	}
	if notReadyCount > 0 {
		msg := "hub metadata is not available on this hubcenter node yet"
		s.recordFailure(ctx, "heartbeat", "hub_not_ready_on_node", msg, record.HubID, "", nil)
		return s.updateRegistrationError(context.Background(), msg)
	}
	if lastErr != nil {
		return s.updateRegistrationError(context.Background(), lastErr.Error())
	}
	return nil
}
func normalizeEmail(email string) string {
	return strings.TrimSpace(strings.ToLower(email))
}

// shouldAcceptAuthorizationUpdate decides whether a new authorization from
// HubCenter should replace the locally persisted one.
//
// The guard prevents HA replication lag or HubCenter redeployment from
// overwriting a known-good authorization with a "not configured" default.
func shouldAcceptAuthorizationUpdate(local, incoming *corelib.DigitalEmployeeAuthorization) bool {
	if incoming == nil {
		return false
	}
	if incoming.Active {
		return true
	}
	if local == nil || !local.Active || local.Quota <= 0 {
		return true
	}
	// Local is active with quota>0. Incoming is inactive.
	// Accept only if incoming has a real quota (explicit admin disable/expiry).
	// Reject quota=0 responses that indicate "never configured on this node".
	return incoming.Quota > 0
}

func normalizeBaseURL(baseURL string) string {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return ""
	}
	if !strings.Contains(baseURL, "://") {
		baseURL = "https://" + baseURL
	}
	return strings.TrimRight(baseURL, "/")
}

func normalizeBaseURLs(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		normalized := normalizeBaseURL(value)
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

func normalizeCorporateEmailDomain(v string) string {
	v = strings.TrimSpace(strings.ToLower(v))
	v = strings.TrimPrefix(v, "@")
	v = strings.TrimPrefix(v, ".")
	return strings.TrimSpace(v)
}

func extractEmailDomain(email string) string {
	email = strings.TrimSpace(strings.ToLower(email))
	at := strings.LastIndex(email, "@")
	if at < 0 || at == len(email)-1 {
		return ""
	}
	return normalizeCorporateEmailDomain(email[at+1:])
}

func normalizeCorporateEmailDomains(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		normalized := normalizeCorporateEmailDomain(value)
		if normalized == "" || !tenantEmailDomainPattern.MatchString(normalized) {
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

func configuredTenantDomains(tenant *store.Tenant) []string {
	if tenant == nil {
		return nil
	}
	values := []string{tenant.PrimaryDomain}
	settings := map[string]any{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(tenant.SettingsJSON)), &settings); err == nil {
		for _, key := range []string{"email_domains", "domains"} {
			if raw, ok := settings[key].([]any); ok {
				for _, item := range raw {
					if value, ok := item.(string); ok {
						values = append(values, value)
					}
				}
			}
		}
	}
	return normalizeCorporateEmailDomains(values)
}

func tenantDomainsCapability(caps map[string]any) map[string][]string {
	if caps == nil {
		return map[string][]string{}
	}
	if existing, ok := caps["tenant_domains"].(map[string][]string); ok && existing != nil {
		return existing
	}
	return map[string][]string{}
}

func appendUniqueSorted(values []string, value string) []string {
	value = normalizeCorporateEmailDomain(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	values = append(values, value)
	sort.Strings(values)
	return values
}

func isPublicSignupVisibility(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "shared", "public":
		return true
	default:
		return false
	}
}

func mustJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return `{}`
	}
	return string(data)
}

func hashHubSecret(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
func randomInstallationID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "inst_" + hex.EncodeToString(buf), nil
}

func (s *Service) advertisedEndpoint() (string, string, int, error) {
	rawBaseURL, err := s.publicBaseURL(context.Background())
	if err != nil {
		return "", "", 0, err
	}
	if rawBaseURL != "" {
		parsed, err := url.Parse(rawBaseURL)
		if err != nil {
			return "", "", 0, fmt.Errorf("invalid public base url: %w", err)
		}

		host := parsed.Hostname()
		if host == "" {
			return "", "", 0, fmt.Errorf("public base url is missing host")
		}

		port := s.cfg.Server.ListenPort
		if parsed.Port() != "" {
			parsedPort, err := strconv.Atoi(parsed.Port())
			if err != nil {
				return "", "", 0, fmt.Errorf("invalid public base url port: %w", err)
			}
			port = parsedPort
		} else if parsed.Scheme == "https" {
			port = 443
		} else if parsed.Scheme == "http" {
			port = 80
		}

		return strings.TrimRight(rawBaseURL, "/"), host, port, nil
	}

	ip, err := detectAdvertisedIPv4()
	if err != nil {
		return "", "", 0, err
	}
	port := s.cfg.Server.ListenPort
	if port == 0 {
		port = 9399
	}

	return fmt.Sprintf("http://%s:%d", ip, port), ip, port, nil
}

func detectAdvertisedIPv4() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", fmt.Errorf("list network interfaces: %w", err)
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil {
				continue
			}
			ip = ip.To4()
			if ip == nil || ip.IsLoopback() {
				continue
			}
			return ip.String(), nil
		}
	}

	return "", fmt.Errorf("unable to detect non-loopback IPv4 address")
}

// brandTools returns the list of supported tool names, including any OEM extra tools.
func brandTools() []string {
	tools := []string{"claude"}
	for _, t := range brand.Current().ExtraTools {
		tools = append(tools, t.Name)
	}
	return tools
}
