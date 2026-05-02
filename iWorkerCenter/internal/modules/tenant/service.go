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
	"strings"
	"time"
)

var (
	ErrCompanyExists           = errors.New("company already exists")
	ErrNoTenantsYet            = errors.New("no tenants exist")
	ErrSetupAlreadyDone        = errors.New("initial setup already completed")
	ErrCloudNotConfigured      = errors.New("iWorkerCloud not configured")
	ErrTenantNotFound          = errors.New("tenant not found")
	ErrCloudCredentialsMissing = errors.New("tenant cloud credentials missing")
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

// CloudRegistrationStatus summarizes local Cloud registration state without exposing secrets.
type CloudRegistrationStatus struct {
	Configured    bool          `json:"configured"`
	Registered    bool          `json:"registered"`
	CenterID      string        `json:"center_id,omitempty"`
	Status        string        `json:"status"`
	License       *CloudLicense `json:"license,omitempty"`
	LicenseError  string        `json:"license_error,omitempty"`
	NonBlocking   bool          `json:"non_blocking"`
	ControlPlane  string        `json:"control_plane"`
	BusinessScope string        `json:"business_scope"`
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
		Configured:    s.cloudClient != nil && strings.TrimSpace(s.cloudClient.cfg.BaseURL) != "",
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
	if strings.Contains(message, "status 401") || strings.Contains(message, "status 403") || strings.Contains(message, "unauthorized") || strings.Contains(message, "forbidden") {
		return "pending"
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
	if s.cloudClient == nil || s.cloudClient.cfg.BaseURL == "" {
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
func (s *TenantService) RegisterTenantToCloud(ctx context.Context, tenantID string) (*RegisterCenterResponse, error) {
	if s.cloudClient == nil || s.cloudClient.cfg.BaseURL == "" {
		return nil, ErrCloudNotConfigured
	}

	t, err := s.tenantRepo.GetByID(ctx, strings.TrimSpace(tenantID))
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, ErrTenantNotFound
	}

	resp, err := s.cloudClient.RegisterCenter(ctx, RegisterCenterRequest{
		CompanyName:      cloudRegistrationName(s.cloudClient.cfg, t.ID),
		AdminEmail:       cloudRegistrationEmail(s.cloudClient.cfg),
		BaseURL:          strings.TrimSpace(s.cloudClient.cfg.CenterBaseURL),
		CloudControlMode: firstNonEmpty(s.cloudClient.cfg.CloudControlMode, "cloud_managed"),
	})
	if err != nil {
		return nil, err
	}

	if err := s.tenantRepo.UpdateCloudInfo(ctx, t.ID, resp.CenterID, resp.Secret); err != nil {
		return nil, err
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
