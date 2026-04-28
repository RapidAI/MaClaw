package tenant

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
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
	ErrSignatureInvalid        = errors.New("invalid signature")
	ErrTimestampExpired        = errors.New("timestamp expired")
	ErrNonceReplay             = errors.New("nonce already used")
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

// ProvisionRequest is the signed request from iWorkerCloud.
type ProvisionRequest struct {
	CompanyName   string `json:"company_name"`
	LegalPerson   string `json:"legal_person"`
	Email         string `json:"email"`
	Address       string `json:"address"`
	AdminUsername string `json:"admin_username"`
	AdminPassword string `json:"admin_password"`
	Timestamp     int64  `json:"timestamp"`
	Nonce         string `json:"nonce"`
	Signature     string `json:"signature"` // base64-encoded RSA signature
}

// TenantService handles multi-tenancy business logic.
type TenantService struct {
	tenantRepo  *TenantRepo
	nonceRepo   *NonceRepo
	adminDB     *sql.DB // write DB for creating admin_users
	cloudClient *CloudClient
	pubKeyCache *PublicKeyCache
	secGroupDB  *sql.DB // write DB for security_groups init
}

func NewTenantService(
	tenantRepo *TenantRepo,
	nonceRepo *NonceRepo,
	adminDB *sql.DB,
	secGroupDB *sql.DB,
	cloudClient *CloudClient,
	pubKeyCache *PublicKeyCache,
) *TenantService {
	return &TenantService{
		tenantRepo:  tenantRepo,
		nonceRepo:   nonceRepo,
		adminDB:     adminDB,
		secGroupDB:  secGroupDB,
		cloudClient: cloudClient,
		pubKeyCache: pubKeyCache,
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

// ProvisionFromCloud handles a signed provision request from iWorkerCloud.
func (s *TenantService) ProvisionFromCloud(ctx context.Context, req ProvisionRequest, rawBodyWithoutSig []byte) (*Tenant, error) {
	// 1. Verify timestamp (5 min window)
	now := time.Now().Unix()
	if abs(now-req.Timestamp) > 300 {
		return nil, ErrTimestampExpired
	}

	// 2. Verify nonce
	expiresAt := time.Now().Add(10 * time.Minute)
	ok, err := s.nonceRepo.Consume(ctx, req.Nonce, expiresAt)
	if err != nil {
		return nil, fmt.Errorf("nonce check: %w", err)
	}
	if !ok {
		return nil, ErrNonceReplay
	}

	// 3. Verify signature
	if s.pubKeyCache == nil {
		return nil, ErrCloudNotConfigured
	}
	pubKey, err := s.pubKeyCache.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("get public key: %w", err)
	}

	bodyHash := sha256.Sum256(rawBodyWithoutSig)
	bodyHashHex := hex.EncodeToString(bodyHash[:])

	sigBytes, err := base64.StdEncoding.DecodeString(req.Signature)
	if err != nil {
		return nil, ErrSignatureInvalid
	}

	if err := VerifyProvisionSignature(pubKey, req.Timestamp, req.Nonce, bodyHashHex, sigBytes); err != nil {
		return nil, ErrSignatureInvalid
	}

	// 4. Create tenant
	return s.createTenantInternal(ctx, CreateTenantRequest{
		CompanyName:   req.CompanyName,
		LegalPerson:   req.LegalPerson,
		Email:         req.Email,
		Address:       req.Address,
		AdminUsername: req.AdminUsername,
		AdminPassword: req.AdminPassword,
	})
}

// ListActiveTenants returns all active tenants (for login page).
func (s *TenantService) ListActiveTenants(ctx context.Context) ([]*Tenant, error) {
	return s.tenantRepo.ListActive(ctx)
}

// TenantCount returns the total number of tenants.
func (s *TenantService) TenantCount(ctx context.Context) (int, error) {
	return s.tenantRepo.Count(ctx)
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

// RegisterToCloud registers a tenant with iWorkerCloud (async-safe).
func (s *TenantService) RegisterToCloud(ctx context.Context, tenantID string) {
	if s.cloudClient == nil || s.cloudClient.cfg.BaseURL == "" {
		log.Printf("[tenant] cloud not configured, skipping registration for %s", tenantID)
		return
	}

	t, err := s.tenantRepo.GetByID(ctx, tenantID)
	if err != nil || t == nil {
		log.Printf("[tenant] cannot find tenant %s for cloud registration: %v", tenantID, err)
		return
	}

	resp, err := s.cloudClient.RegisterCenter(ctx, RegisterCenterRequest{
		CompanyName: t.CompanyName,
		AdminEmail:  t.Email,
		LegalPerson: t.LegalPerson,
		Address:     t.Address,
	})
	if err != nil {
		log.Printf("[tenant] cloud registration failed for %s: %v", tenantID, err)
		return
	}

	if err := s.tenantRepo.UpdateCloudInfo(ctx, tenantID, resp.CenterID, resp.Secret); err != nil {
		log.Printf("[tenant] failed to save cloud info for %s: %v", tenantID, err)
	}
	log.Printf("[tenant] registered %s with cloud, center_id=%s", tenantID, resp.CenterID)
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

func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}
