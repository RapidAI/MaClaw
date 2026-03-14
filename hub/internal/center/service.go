package center

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/config"
)

const (
	systemKeyCenterBaseURL      = "center_base_url"
	systemKeyCenterRegistration = "center_registration"
	systemKeyAdminEmail         = "admin_email"
	systemKeyInstallationID     = "hub_installation_id"
	systemKeyHubVisibility      = "hub_visibility"
	systemKeyHubEnrollmentMode  = "hub_enrollment_mode"
	systemKeyPublicBaseURL      = "server_public_base_url"
)

type SystemSettingsRepository interface {
	Set(ctx context.Context, key, valueJSON string) error
	Get(ctx context.Context, key string) (string, error)
}

type RegistrationState struct {
	Enabled             bool   `json:"enabled"`
	BaseURL             string `json:"base_url"`
	PublicBaseURL       string `json:"public_base_url"`
	Visibility          string `json:"visibility"`
	EnrollmentMode      string `json:"enrollment_mode"`
	AdvertisedBaseURL   string `json:"advertised_base_url,omitempty"`
	Host                string `json:"host,omitempty"`
	Port                int    `json:"port,omitempty"`
	RegisterOnStartup   bool   `json:"register_on_startup"`
	AdminEmailPresent   bool   `json:"admin_email_present"`
	Registered          bool   `json:"registered"`
	PendingConfirmation bool   `json:"pending_confirmation"`
	Disabled            bool   `json:"disabled"`
	HubID               string `json:"hub_id,omitempty"`
	DisabledReason      string `json:"disabled_reason,omitempty"`
	LastError           string `json:"last_error,omitempty"`
	LastRegisteredAt    int64  `json:"last_registered_at,omitempty"`
}

type registrationRecord struct {
	Registered          bool   `json:"registered"`
	PendingConfirmation bool   `json:"pending_confirmation"`
	Disabled            bool   `json:"disabled"`
	HubID               string `json:"hub_id,omitempty"`
	HubSecret           string `json:"hub_secret,omitempty"`
	DisabledReason      string `json:"disabled_reason,omitempty"`
	LastError           string `json:"last_error,omitempty"`
	LastRegisteredAt    int64  `json:"last_registered_at,omitempty"`
}

type registerHubRequest struct {
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

type registerHubResponse struct {
	HubID               string `json:"hub_id"`
	HubSecret           string `json:"hub_secret"`
	PendingConfirmation bool   `json:"pending_confirmation"`
	Message             string `json:"message"`
}

type Service struct {
	cfg      *config.Config
	settings SystemSettingsRepository
	client   *http.Client

	mu               sync.Mutex
	heartbeatStarted bool
	heartbeatCancel  context.CancelFunc
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

func (s *Service) Status(ctx context.Context) (*RegistrationState, error) {
	baseURL, err := s.baseURL(ctx)
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
	adminEmail, err := s.adminEmail(ctx)
	if err != nil {
		return nil, err
	}

	return &RegistrationState{
		Enabled:             s.cfg.Center.Enabled,
		BaseURL:             baseURL,
		PublicBaseURL:       publicBaseURL,
		Visibility:          visibility,
		EnrollmentMode:      enrollmentMode,
		AdvertisedBaseURL:   advertisedBaseURL,
		Host:                advertisedHost,
		Port:                advertisedPort,
		RegisterOnStartup:   s.cfg.Center.RegisterOnStartup,
		AdminEmailPresent:   adminEmail != "",
		Registered:          record.Registered,
		PendingConfirmation: record.PendingConfirmation,
		Disabled:            record.Disabled,
		HubID:               record.HubID,
		DisabledReason:      record.DisabledReason,
		LastError:           record.LastError,
		LastRegisteredAt:    record.LastRegisteredAt,
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

	baseURL, err := s.baseURL(ctx)
	if err != nil {
		return nil, err
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

	reqBody := registerHubRequest{
		InstallationID: installationID,
		OwnerEmail:     ownerEmail,
		Name:           s.cfg.Hub.Name,
		Description:    s.cfg.Hub.Description,
		BaseURL:        advertisedBaseURL,
		Host:           advertisedHost,
		Port:           advertisedPort,
		Visibility:     visibility,
		EnrollmentMode: enrollmentMode,
		Capabilities: map[string]any{
			"supports_remote_control": true,
			"supports_pwa":            true,
			"supports_tools":          []string{"claude"},
			"brand":                   "CodeClaw",
		},
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/hubs/register", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(httpReq)
	if err != nil {
		_ = s.updateRegistrationError(ctx, err.Error())
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err = fmt.Errorf("hub center register failed with status %d", resp.StatusCode)
		_ = s.updateRegistrationError(ctx, err.Error())
		return nil, err
	}

	var registerResp registerHubResponse
	if err := json.NewDecoder(resp.Body).Decode(&registerResp); err != nil {
		_ = s.updateRegistrationError(ctx, err.Error())
		return nil, err
	}
	if registerResp.HubID == "" || registerResp.HubSecret == "" {
		err = fmt.Errorf("hub center register returned incomplete credentials")
		_ = s.updateRegistrationError(ctx, err.Error())
		return nil, err
	}

	record = registrationRecord{
		Registered:          !registerResp.PendingConfirmation,
		PendingConfirmation: registerResp.PendingConfirmation,
		Disabled:            false,
		HubID:               registerResp.HubID,
		HubSecret:           registerResp.HubSecret,
		DisabledReason:      "",
		LastError:           registerResp.Message,
		LastRegisteredAt:    time.Now().Unix(),
	}
	if err := s.saveRegistration(ctx, record); err != nil {
		return nil, err
	}
	s.startHeartbeatLoop()

	return s.Status(ctx)
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
	raw, err := s.settings.Get(ctx, systemKeyCenterBaseURL)
	if err != nil {
		return "", err
	}
	if raw == "" {
		return normalizeBaseURL(s.cfg.Center.BaseURL), nil
	}

	var payload struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return "", err
	}
	if payload.Value == "" {
		return normalizeBaseURL(s.cfg.Center.BaseURL), nil
	}
	return normalizeBaseURL(payload.Value), nil
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
	record.Registered = false
	record.PendingConfirmation = false
	record.Disabled = false
	record.DisabledReason = ""
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

func (s *Service) sendHeartbeat(ctx context.Context) error {
	baseURL, err := s.baseURL(ctx)
	if err != nil {
		return err
	}

	record, err := s.loadRegistration(ctx)
	if err != nil {
		return err
	}
	if (!record.Registered && !record.PendingConfirmation && !record.Disabled) || record.HubID == "" || record.HubSecret == "" {
		return nil
	}

	payload, err := json.Marshal(map[string]string{
		"hub_secret": record.HubSecret,
	})
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/hubs/"+record.HubID+"/heartbeat", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return s.updateRegistrationError(context.Background(), err.Error())
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusNotFound {
			record.Registered = false
			record.PendingConfirmation = false
			record.Disabled = false
			record.HubID = ""
			record.HubSecret = ""
			record.DisabledReason = ""
			record.LastError = "hub registration was removed by Hub Center"
			record.LastRegisteredAt = 0
			return s.saveRegistration(context.Background(), record)
		}
		if resp.StatusCode == http.StatusConflict {
			record.Registered = false
			record.PendingConfirmation = true
			record.Disabled = false
			record.DisabledReason = ""
			record.LastError = "hub registration is waiting for email confirmation"
			record.LastRegisteredAt = time.Now().Unix()
			return s.saveRegistration(context.Background(), record)
		}
		if resp.StatusCode == http.StatusLocked {
			record.Registered = false
			record.PendingConfirmation = false
			record.Disabled = true
			record.DisabledReason = "disabled by Hub Center"
			record.LastError = "hub has been disabled by Hub Center"
			record.LastRegisteredAt = time.Now().Unix()
			return s.saveRegistration(context.Background(), record)
		}
		return s.updateRegistrationError(context.Background(), fmt.Sprintf("hub center heartbeat failed with status %d", resp.StatusCode))
	}

	record.Registered = true
	record.PendingConfirmation = false
	record.Disabled = false
	record.DisabledReason = ""
	record.LastError = ""
	record.LastRegisteredAt = time.Now().Unix()
	return s.saveRegistration(context.Background(), record)
}

func normalizeEmail(email string) string {
	return strings.TrimSpace(strings.ToLower(email))
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

func mustJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return `{}`
	}
	return string(data)
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
