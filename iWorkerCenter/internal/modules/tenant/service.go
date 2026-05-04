package tenant

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var (
	ErrCompanyExists           = errors.New("company already exists")
	ErrNoTenantsYet            = errors.New("no tenants exist")
	ErrSetupAlreadyDone        = errors.New("initial setup already completed")
	ErrCloudNotConfigured      = errors.New("iWorkerCloud not configured")
	ErrTenantNotFound          = errors.New("tenant not found")
	ErrCloudCredentialsMissing = errors.New("tenant cloud credentials missing")
	ErrMultiTenantDisabled     = errors.New("multi-tenant mode is disabled")
)

// CreateTenantRequest is used for both setup-tenant and provision.
type CreateTenantRequest struct {
	CompanyName   string `json:"company_name"`
	LegalPerson   string `json:"legal_person"`
	Email         string `json:"email"`
	Address       string `json:"address"`
	AdminUsername string `json:"admin_username"`
	AdminPassword string `json:"admin_password"`
}

// TenantService handles multi-tenancy business logic.
type UpdateTenantRequest struct {
	CompanyName string `json:"company_name"`
	LegalPerson string `json:"legal_person"`
	Email       string `json:"email"`
	Address     string `json:"address"`
	Status      string `json:"status"`
}

type MultiTenantSettings struct {
	Mode        string `json:"mode"`
	MultiTenant bool   `json:"multi_tenant"`
}
type TenantService struct {
	tenantRepo  *TenantRepo
	adminDB     *sql.DB // write DB for creating admin_users
	cloudClient *CloudClient
	secGroupDB  *sql.DB // write DB for security_groups init
}

func NewTenantService(
	tenantRepo *TenantRepo,
	adminDB *sql.DB,
	secGroupDB *sql.DB,
	cloudClient *CloudClient,
) *TenantService {
	return &TenantService{
		tenantRepo:  tenantRepo,
		adminDB:     adminDB,
		secGroupDB:  secGroupDB,
		cloudClient: cloudClient,
	}
}

// SetupFirstTenant creates the first tenant (initial setup flow).
func (s *TenantService) SetupFirstTenant(ctx context.Context, req CreateTenantRequest) (*Tenant, error) {
	count, err := s.tenantRepo.Count(ctx)
	if err != nil {
		return nil, err
	}
	if count > 0 {
		return nil, ErrSetupAlreadyDone
	}
	return s.createTenantInternal(ctx, req)
}

// ListActiveTenants returns all active tenants (for login page).
func (s *TenantService) ListActiveTenants(ctx context.Context) ([]*Tenant, error) {
	return s.tenantRepo.ListActive(ctx)
}

// TenantCount returns the total number of tenants.
func (s *TenantService) TenantCount(ctx context.Context) (int, error) {
	return s.tenantRepo.Count(ctx)
}

func (s *TenantService) ListTenants(ctx context.Context) ([]*Tenant, error) {
	return s.tenantRepo.List(ctx)
}

func (s *TenantService) CreateTenant(ctx context.Context, req CreateTenantRequest) (*Tenant, error) {
	return s.createTenantInternal(ctx, req)
}

func (s *TenantService) UpdateTenant(ctx context.Context, tenantID string, req UpdateTenantRequest) (*Tenant, error) {
	tenantID = strings.TrimSpace(tenantID)
	t, err := s.tenantRepo.GetByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, ErrTenantNotFound
	}
	if value := strings.TrimSpace(req.CompanyName); value != "" && value != t.CompanyName {
		existing, err := s.tenantRepo.GetByCompanyName(ctx, value)
		if err != nil {
			return nil, err
		}
		if existing != nil && existing.ID != tenantID {
			return nil, ErrCompanyExists
		}
		t.CompanyName = value
	}
	if value := strings.TrimSpace(req.Email); value != "" {
		t.Email = value
	}
	t.LegalPerson = strings.TrimSpace(req.LegalPerson)
	t.Address = strings.TrimSpace(req.Address)
	if value := strings.TrimSpace(req.Status); value != "" {
		if value != "active" && value != "disabled" {
			return nil, fmt.Errorf("status must be active or disabled")
		}
		t.Status = value
	}
	if strings.TrimSpace(t.CompanyName) == "" || strings.TrimSpace(t.Email) == "" {
		return nil, fmt.Errorf("company_name and email are required")
	}
	if err := s.tenantRepo.Update(ctx, t); err != nil {
		return nil, err
	}
	return s.tenantRepo.GetByID(ctx, tenantID)
}

func (s *TenantService) DeleteTenant(ctx context.Context, tenantID string) error {
	t, err := s.tenantRepo.GetByID(ctx, strings.TrimSpace(tenantID))
	if err != nil {
		return err
	}
	if t == nil {
		return ErrTenantNotFound
	}
	return s.tenantRepo.Delete(ctx, t.ID)
}

func (s *TenantService) MultiTenantSettings(ctx context.Context) (MultiTenantSettings, error) {
	_ = ctx
	mode := "dedicated"
	if s != nil && s.adminDB != nil {
		var raw string
		if err := s.adminDB.QueryRow(`SELECT value_json FROM system_settings WHERE key='tenant_mode'`).Scan(&raw); err == nil && strings.TrimSpace(raw) != "" {
			var parsed struct {
				Mode string `json:"mode"`
			}
			if yaml.Unmarshal([]byte(raw), &parsed) == nil && strings.TrimSpace(parsed.Mode) != "" {
				mode = strings.TrimSpace(parsed.Mode)
			}
		}
	}
	if mode != "multi_tenant" {
		mode = "dedicated"
	}
	return MultiTenantSettings{Mode: mode, MultiTenant: mode == "multi_tenant"}, nil
}

func (s *TenantService) UpdateMultiTenantSettings(ctx context.Context, mode string) (MultiTenantSettings, error) {
	_ = ctx
	mode = strings.TrimSpace(mode)
	if mode == "" {
		mode = "dedicated"
	}
	if mode != "dedicated" && mode != "multi_tenant" {
		return MultiTenantSettings{}, fmt.Errorf("mode must be dedicated or multi_tenant")
	}
	data, _ := yaml.Marshal(map[string]string{"mode": mode})
	_, err := s.adminDB.Exec(`INSERT INTO system_settings (key, value_json, updated_at) VALUES ('tenant_mode', ?, datetime('now')) ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json, updated_at=datetime('now')`, string(data))
	if err != nil {
		return MultiTenantSettings{}, err
	}
	return s.MultiTenantSettings(ctx)
}

func (s *TenantService) AuthenticateCloudCenter(ctx context.Context, centerID, secret string) error {
	centerID = strings.TrimSpace(centerID)
	secret = strings.TrimSpace(secret)
	if centerID == "" || secret == "" {
		return ErrCloudCredentialsMissing
	}
	t, err := s.tenantRepo.GetByCloudCenterID(ctx, centerID)
	if err != nil {
		return err
	}
	if t == nil || strings.TrimSpace(t.CloudSecret) != secret {
		return ErrCloudCredentialsMissing
	}
	return nil
}
func (s *TenantService) RequireMultiTenantEnabled(ctx context.Context) error {
	settings, err := s.MultiTenantSettings(ctx)
	if err != nil {
		return err
	}
	if !settings.MultiTenant {
		return ErrMultiTenantDisabled
	}
	return nil
}

// CloudRegistrationStatus summarizes local Cloud registration state without exposing secrets.
type CloudComputeStatus struct {
	ProviderCount     int    `json:"provider_count"`
	ComputePermission bool   `json:"compute_permission"`
	ForceSync         bool   `json:"force_sync"`
	Error             string `json:"error,omitempty"`
}

type CloudRegistrationStatus struct {
	Configured    bool                `json:"configured"`
	Registered    bool                `json:"registered"`
	CenterID      string              `json:"center_id,omitempty"`
	Status        string              `json:"status"`
	License       *CloudLicense       `json:"license,omitempty"`
	Compute       *CloudComputeStatus `json:"compute,omitempty"`
	LicenseError  string              `json:"license_error,omitempty"`
	NonBlocking   bool                `json:"non_blocking"`
	ControlPlane  string              `json:"control_plane"`
	BusinessScope string              `json:"business_scope"`
}

// UpdateCloudConfigRequest updates the iWorkerCloud connection settings.
type UpdateCloudConfigRequest struct {
	BaseURL           string `json:"base_url"`
	CenterBaseURL     string `json:"center_base_url"`
	RegistrationName  string `json:"registration_name"`
	RegistrationEmail string `json:"registration_email"`
	CloudControlMode  string `json:"cloud_control_mode"`
}

// CloudConfig returns the current iWorkerCloud connection settings.
func (s *TenantService) CloudConfig(ctx context.Context) CloudConfig {
	_ = ctx
	if s.cloudClient == nil {
		return CloudConfig{CloudControlMode: "cloud_managed"}
	}
	return s.cloudClient.Config()
}

// UpdateCloudConfig validates, persists, and activates iWorkerCloud settings.
func (s *TenantService) UpdateCloudConfig(ctx context.Context, req UpdateCloudConfigRequest) (CloudConfig, error) {
	_ = ctx
	cfg := CloudConfig{
		BaseURL:           strings.TrimSpace(req.BaseURL),
		CenterBaseURL:     strings.TrimSpace(req.CenterBaseURL),
		RegistrationName:  strings.TrimSpace(req.RegistrationName),
		RegistrationEmail: strings.TrimSpace(req.RegistrationEmail),
		CloudControlMode:  firstNonEmpty(req.CloudControlMode, "cloud_managed"),
	}
	if err := validateHTTPURL("base_url", cfg.BaseURL, true); err != nil {
		return CloudConfig{}, err
	}
	if err := validateHTTPURL("center_base_url", cfg.CenterBaseURL, false); err != nil {
		return CloudConfig{}, err
	}
	if s.cloudClient == nil {
		s.cloudClient = NewCloudClient(cfg)
	} else {
		s.cloudClient.SetConfig(cfg)
	}
	cfg = s.cloudClient.Config()
	if err := persistCloudConfig(cfg); err != nil {
		return CloudConfig{}, err
	}
	return cfg, nil
}

func validateHTTPURL(field, value string, required bool) error {
	value = strings.TrimSpace(value)
	if value == "" {
		if required {
			return fmt.Errorf("%s is required", field)
		}
		return nil
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("%s must be an http(s) URL", field)
	}
	return nil
}

func cloudConfigDir() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("IWORKERCENTER_HOME")); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	return filepath.Join(home, ".iworkercenter"), nil
}

func ensureMachineID() (string, error) {
	dir, err := cloudConfigDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("create config dir: %w", err)
	}
	path := filepath.Join(dir, "machine_id")
	if data, err := os.ReadFile(path); err == nil {
		if id := strings.TrimSpace(string(data)); id != "" {
			return id, nil
		}
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate machine id: %w", err)
	}
	id := "iwm_" + hex.EncodeToString(b)
	if err := os.WriteFile(path, []byte(id+"\n"), 0600); err != nil {
		return "", fmt.Errorf("persist machine id: %w", err)
	}
	return id, nil
}

func persistCloudConfig(cfg CloudConfig) error {
	dir, err := cloudConfigDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	path := filepath.Join(dir, "config.yaml")
	configFile := map[string]any{}
	if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
		_ = yaml.Unmarshal(data, &configFile)
	}
	configFile["cloud"] = cfg
	data, err := yaml.Marshal(configFile)
	if err != nil {
		return fmt.Errorf("marshal cloud config: %w", err)
	}
	return os.WriteFile(path, data, 0600)
}

// CloudStatus returns local Cloud registration state and best-effort license status.
func (s *TenantService) CloudStatus(ctx context.Context, tenantID string) (*CloudRegistrationStatus, error) {
	t, err := s.tenantRepo.GetByID(ctx, strings.TrimSpace(tenantID))
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, ErrTenantNotFound
	}

	status := &CloudRegistrationStatus{
		Configured:    strings.TrimSpace(s.CloudConfig(ctx).BaseURL) != "",
		Registered:    strings.TrimSpace(t.CloudCenterID) != "" && strings.TrimSpace(t.CloudSecret) != "",
		CenterID:      strings.TrimSpace(t.CloudCenterID),
		Status:        "unregistered",
		NonBlocking:   true,
		ControlPlane:  "registration_authorization_compute_distribution",
		BusinessScope: "isolated_in_iworkercenter_and_iworker",
	}
	if !status.Configured {
		status.Status = "not_configured"
		return status, nil
	}
	if !status.Registered {
		return status, nil
	}

	license, err := s.cloudClient.FetchCenterLicense(ctx, t.CloudCenterID, t.CloudSecret)
	if err != nil {
		status.Status = classifyCloudLicenseError(err)
		status.LicenseError = err.Error()
		return status, nil
	}
	status.Status = "licensed"
	status.License = license
	if computeStatus, err := s.cloudClient.FetchCenterComputeProviders(ctx, t.CloudCenterID, t.CloudSecret); err == nil && computeStatus != nil {
		status.Compute = &CloudComputeStatus{ProviderCount: len(computeStatus.Providers), ComputePermission: computeStatus.ComputePermission, ForceSync: computeStatus.ForceSync}
	} else if err != nil {
		status.Compute = &CloudComputeStatus{Error: err.Error()}
	}
	return status, nil
}

func classifyCloudLicenseError(err error) string {
	if err == nil {
		return "licensed"
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "status 404") || strings.Contains(message, "no active license") || strings.Contains(message, "not found") {
		return "pending"
	}
	if strings.Contains(message, "status 401") || strings.Contains(message, "status 403") || strings.Contains(message, "unauthorized") || strings.Contains(message, "forbidden") || strings.Contains(message, "auth_failed") || strings.Contains(message, "invalid center credentials") {
		return "credential_mismatch"
	}
	if strings.Contains(message, "connection") || strings.Contains(message, "timeout") || strings.Contains(message, "no such host") || strings.Contains(message, "refused") {
		return "offline"
	}
	return "error"
}

// CloudCredentials returns the cloud registration credentials for a tenant.
func (s *TenantService) CloudCredentials(ctx context.Context, tenantID string) (string, string, error) {
	t, err := s.tenantRepo.GetByID(ctx, strings.TrimSpace(tenantID))
	if err != nil {
		return "", "", err
	}
	if t == nil {
		return "", "", ErrTenantNotFound
	}
	if t.CloudCenterID == "" || t.CloudSecret == "" {
		return "", "", ErrCloudCredentialsMissing
	}
	return t.CloudCenterID, t.CloudSecret, nil
}

// FetchCloudLicense retrieves this tenant's active iWorkerCloud license.
func (s *TenantService) FetchCloudLicense(ctx context.Context, tenantID string) (*CloudLicense, error) {
	if s.cloudClient == nil || s.cloudClient.Config().BaseURL == "" {
		return nil, ErrCloudNotConfigured
	}

	t, err := s.tenantRepo.GetByID(ctx, strings.TrimSpace(tenantID))
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, ErrTenantNotFound
	}
	if t.CloudCenterID == "" || t.CloudSecret == "" {
		return nil, ErrCloudCredentialsMissing
	}

	return s.cloudClient.FetchCenterLicense(ctx, t.CloudCenterID, t.CloudSecret)
}

// RegisterTenantToCloud registers a tenant with iWorkerCloud and persists the returned Center credentials.
func (s *TenantService) RegisterTenantToCloud(ctx context.Context, tenantID string, registration ...RegisterCenterRequest) (*RegisterCenterResponse, error) {
	if s.cloudClient == nil || s.cloudClient.Config().BaseURL == "" {
		return nil, ErrCloudNotConfigured
	}

	t, err := s.tenantRepo.GetByID(ctx, strings.TrimSpace(tenantID))
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, ErrTenantNotFound
	}

	cfg := s.cloudClient.Config()
	tenantMode, _ := s.MultiTenantSettings(ctx)
	tenantCount, _ := s.TenantCount(ctx)
	machineID, err := ensureMachineID()
	if err != nil {
		return nil, err
	}

	req := RegisterCenterRequest{
		MachineID:           machineID,
		CompanyID:           strings.TrimSpace(t.ID),
		CompanyName:         firstNonEmpty(t.CompanyName, cloudRegistrationName(cfg, t.ID)),
		AdminEmail:          firstNonEmpty(t.Email, cloudRegistrationEmail(cfg)),
		Address:             strings.TrimSpace(t.Address),
		LegalPerson:         strings.TrimSpace(t.LegalPerson),
		BaseURL:             strings.TrimSpace(cfg.CenterBaseURL),
		CloudControlMode:    firstNonEmpty(cfg.CloudControlMode, "cloud_managed"),
		SupportsMultiTenant: tenantMode.MultiTenant,
		TenantCount:         tenantCount,
	}
	if len(registration) > 0 {
		req = mergeRegisterCenterRequest(req, registration[0])
	}
	if strings.TrimSpace(req.CompanyName) == "" || strings.TrimSpace(req.AdminEmail) == "" {
		return nil, fmt.Errorf("company_name and admin_email are required")
	}

	resp, err := s.cloudClient.RegisterCenter(ctx, req)
	if err != nil {
		return nil, err
	}

	if err := s.tenantRepo.UpdateCloudInfo(ctx, t.ID, resp.CenterID, resp.Secret); err != nil {
		return nil, err
	}
	if err := s.cloudClient.SendCenterHeartbeat(ctx, resp.CenterID, resp.Secret, nil, nil); err != nil {
		log.Printf("[tenant] cloud heartbeat after registration failed for %s: %v", t.ID, err)
	}
	return resp, nil
}

// RegisterToCloud registers a tenant with iWorkerCloud (async-safe).
func (s *TenantService) RegisterToCloud(ctx context.Context, tenantID string) {
	resp, err := s.RegisterTenantToCloud(ctx, tenantID)
	if err != nil {
		log.Printf("[tenant] cloud registration skipped/failed for %s: %v", tenantID, err)
		return
	}
	log.Printf("[tenant] registered %s with cloud, center_id=%s", tenantID, resp.CenterID)
}

func mergeRegisterCenterRequest(base RegisterCenterRequest, override RegisterCenterRequest) RegisterCenterRequest {
	if value := strings.TrimSpace(override.CompanyName); value != "" {
		base.CompanyName = value
	}
	if value := strings.TrimSpace(override.AdminEmail); value != "" {
		base.AdminEmail = value
	}
	if value := strings.TrimSpace(override.AdminPhone); value != "" {
		base.AdminPhone = value
	}
	if value := strings.TrimSpace(override.Address); value != "" {
		base.Address = value
	}
	if value := strings.TrimSpace(override.LegalPerson); value != "" {
		base.LegalPerson = value
	}
	if value := strings.TrimSpace(override.BaseURL); value != "" {
		base.BaseURL = value
	}
	if value := strings.TrimSpace(override.CloudControlMode); value != "" {
		base.CloudControlMode = value
	}
	return base
}
func cloudRegistrationName(cfg CloudConfig, fallbackID string) string {
	if name := strings.TrimSpace(cfg.RegistrationName); name != "" {
		return name
	}
	fallbackID = strings.TrimSpace(fallbackID)
	if fallbackID == "" {
		return "iWorkerCenter service"
	}
	return "iWorkerCenter service " + fallbackID
}

func cloudRegistrationEmail(cfg CloudConfig) string {
	if email := strings.TrimSpace(cfg.RegistrationEmail); email != "" {
		return email
	}
	return "iworkercenter@local.invalid"
}

func (s *TenantService) createTenantInternal(ctx context.Context, req CreateTenantRequest) (*Tenant, error) {
	req.CompanyName = strings.TrimSpace(req.CompanyName)
	req.Email = strings.TrimSpace(req.Email)
	req.AdminUsername = strings.TrimSpace(req.AdminUsername)

	if req.CompanyName == "" || req.Email == "" || req.AdminUsername == "" || req.AdminPassword == "" {
		return nil, fmt.Errorf("company_name, email, admin_username, admin_password are required")
	}

	// Check duplicate
	existing, err := s.tenantRepo.GetByCompanyName(ctx, req.CompanyName)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, ErrCompanyExists
	}

	now := time.Now()
	tenantID := fmt.Sprintf("tnt_%d", now.UnixNano())

	t := &Tenant{
		ID:          tenantID,
		CompanyName: req.CompanyName,
		LegalPerson: strings.TrimSpace(req.LegalPerson),
		Email:       req.Email,
		Address:     strings.TrimSpace(req.Address),
		Status:      "active",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := s.tenantRepo.Create(ctx, t); err != nil {
		return nil, fmt.Errorf("create tenant: %w", err)
	}

	// Create admin user
	if err := s.createAdminUser(ctx, tenantID, req.AdminUsername, req.AdminPassword); err != nil {
		return nil, fmt.Errorf("create admin user: %w", err)
	}

	// Init root security group
	if err := s.initRootSecurityGroup(ctx, tenantID); err != nil {
		log.Printf("[tenant] init root security group for %s: %v", tenantID, err)
	}

	return t, nil
}

func (s *TenantService) createAdminUser(ctx context.Context, tenantID, username, password string) error {
	salt := generateSalt()
	hash := hashPassword(password, salt)
	id := fmt.Sprintf("adm_%d", time.Now().UnixNano())
	_, err := s.adminDB.ExecContext(ctx,
		`INSERT INTO admin_users (id, username, password_hash, salt, tenant_id, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, datetime('now'), datetime('now'))`,
		id, username, hash, salt, tenantID)
	return err
}

func (s *TenantService) initRootSecurityGroup(ctx context.Context, tenantID string) error {
	id := fmt.Sprintf("sg_root_%s", tenantID)
	_, err := s.secGroupDB.ExecContext(ctx,
		"INSERT OR IGNORE INTO security_groups (id, name, parent_id, tenant_id, created_at, updated_at) VALUES (?, '\u5168\u5458', '', ?, datetime('now'), datetime('now'))",
		id, tenantID)
	return err
}

func generateSalt() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func hashPassword(password, salt string) string {
	h := sha256.Sum256([]byte(salt + ":" + password))
	return hex.EncodeToString(h[:])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
