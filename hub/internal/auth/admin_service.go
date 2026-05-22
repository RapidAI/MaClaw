package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrAdminAlreadyInitialized = errors.New("admin already initialized")
	ErrInvalidAdminCredentials = errors.New("invalid admin credentials")
	ErrInvalidAdminPassword    = errors.New("invalid admin password")
)

type AdminService struct {
	admins   store.AdminUserRepository
	settings store.SystemSettingsRepository
	audit    store.AdminAuditRepository
}

type tenantAdminLister interface {
	ListByScopeTenant(ctx context.Context, scope, tenantID string) ([]*store.AdminUser, error)
}

func NewAdminService(
	admins store.AdminUserRepository,
	settings store.SystemSettingsRepository,
	audit store.AdminAuditRepository,
) *AdminService {
	return &AdminService{
		admins:   admins,
		settings: settings,
		audit:    audit,
	}
}

const adminTokenSecretKey = "admin_token_secret"

const ExplicitGlobalAdminTenantScope = "__global__"

type signedAdminTokenPayload struct {
	Username       string `json:"username"`
	IssuedAt       int64  `json:"issued_at"`
	AdminSignature string `json:"admin_signature"`
	Scope          string `json:"scope"`
	Role           string `json:"role"`
	TenantID       string `json:"tenant_id,omitempty"`
	Nonce          string `json:"nonce"`
}

func (s *AdminService) IsInitialized(ctx context.Context) (bool, error) {
	count, err := s.admins.Count(ctx)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *AdminService) SetupInitialAdmin(ctx context.Context, username, password, email string) error {
	username = strings.TrimSpace(username)
	if username == "" || strings.TrimSpace(password) == "" {
		return fmt.Errorf("username and password are required")
	}
	initialized, err := s.IsInitialized(ctx)
	if err != nil {
		return err
	}
	if initialized {
		return ErrAdminAlreadyInitialized
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	now := time.Now()
	email = normalizeEmail(email)
	if !isValidAdminEmail(email) {
		return fmt.Errorf("valid admin email is required")
	}
	admin := &store.AdminUser{
		ID:           newID("adm"),
		Username:     username,
		PasswordHash: string(passwordHash),
		Email:        email,
		Scope:        "global",
		Role:         "global_owner",
		Status:       "active",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.admins.Create(ctx, admin); err != nil {
		return err
	}

	if err := s.settings.Set(ctx, "admin_initialized", `{"value":true}`); err != nil {
		return err
	}
	if err := s.settings.Set(ctx, "admin_email", mustJSON(map[string]string{"value": admin.Email})); err != nil {
		return err
	}

	if s.audit != nil {
		_ = s.audit.Create(ctx, &store.AdminAuditLog{
			ID:          newID("aa"),
			TenantID:    admin.TenantID,
			AdminUserID: admin.ID,
			Action:      "admin.setup",
			PayloadJSON: mustJSON(map[string]any{"username": admin.Username, "email": admin.Email}),
			CreatedAt:   now,
		})
	}

	return nil
}

func (s *AdminService) CreateTenantAdmin(ctx context.Context, tenantID, username, password, email, displayName, role string) (*store.AdminUser, error) {
	tenantID = normalizeTenantIDValue(tenantID)
	if tenantID == "" || strings.EqualFold(tenantID, store.DefaultTenantID) || strings.EqualFold(tenantID, ExplicitGlobalAdminTenantScope) {
		return nil, fmt.Errorf("tenant id is required")
	}
	username = strings.TrimSpace(username)
	if username == "" || strings.TrimSpace(password) == "" {
		return nil, fmt.Errorf("username and password are required")
	}
	email = normalizeEmail(email)
	if !isValidAdminEmail(email) {
		return nil, fmt.Errorf("valid admin email is required")
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	admin := &store.AdminUser{
		ID:           newID("adm"),
		Username:     username,
		PasswordHash: string(passwordHash),
		Email:        email,
		Scope:        "tenant",
		Role:         normalizedTenantAdminRole(role),
		TenantID:     tenantID,
		DisplayName:  strings.TrimSpace(displayName),
		Status:       "active",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.admins.Create(ctx, admin); err != nil {
		return nil, err
	}
	if s.audit != nil {
		_ = s.audit.Create(ctx, &store.AdminAuditLog{
			ID:          newID("aa"),
			TenantID:    admin.TenantID,
			AdminUserID: admin.ID,
			Action:      "tenant_admin.created",
			PayloadJSON: mustJSON(map[string]any{"username": admin.Username, "email": admin.Email, "tenant_id": admin.TenantID, "role": admin.Role}),
			CreatedAt:   now,
		})
	}
	return admin, nil
}

func (s *AdminService) ListTenantAdmins(ctx context.Context, tenantID string) ([]*store.AdminUser, error) {
	lister, ok := s.admins.(tenantAdminLister)
	if !ok {
		return nil, fmt.Errorf("tenant admin listing is not supported")
	}
	return lister.ListByScopeTenant(ctx, "tenant", normalizeTenantIDValue(tenantID))
}

func (s *AdminService) ResetAdminCredentials(ctx context.Context, username, password string) error {
	username = strings.TrimSpace(username)
	if username == "" || strings.TrimSpace(password) == "" {
		return fmt.Errorf("username and password are required")
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	now := time.Now()
	admin := &store.AdminUser{
		ID:           newID("adm"),
		Username:     username,
		PasswordHash: string(passwordHash),
		Email:        synthesizeAdminEmail(username),
		Scope:        "global",
		Role:         "global_owner",
		Status:       "active",
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := s.admins.DeleteAll(ctx); err != nil {
		return err
	}
	if err := s.admins.Create(ctx, admin); err != nil {
		return err
	}
	if err := s.settings.Set(ctx, "admin_initialized", `{"value":true}`); err != nil {
		return err
	}
	if err := s.settings.Set(ctx, "admin_email", mustJSON(map[string]string{"value": admin.Email})); err != nil {
		return err
	}

	if s.audit != nil {
		_ = s.audit.Create(ctx, &store.AdminAuditLog{
			ID:          newID("aa"),
			TenantID:    admin.TenantID,
			AdminUserID: admin.ID,
			Action:      "admin.reset_credentials",
			PayloadJSON: mustJSON(map[string]any{"username": admin.Username, "email": admin.Email}),
			CreatedAt:   now,
		})
	}

	return nil
}

func (s *AdminService) Login(ctx context.Context, username, password string) (string, *store.AdminUser, error) {
	return s.LoginScoped(ctx, username, password, ExplicitGlobalAdminTenantScope)
}

func (s *AdminService) LoginScoped(ctx context.Context, username, password, tenantID string) (string, *store.AdminUser, error) {
	admin, err := s.VerifyScopedCredentials(ctx, username, password, tenantID)
	if err != nil {
		return "", nil, err
	}
	token, err := s.issueToken(ctx, admin)
	if err != nil {
		return "", nil, err
	}

	if s.audit != nil {
		_ = s.audit.Create(ctx, &store.AdminAuditLog{
			ID:          newID("aa"),
			TenantID:    admin.TenantID,
			AdminUserID: admin.ID,
			Action:      "admin.login",
			PayloadJSON: mustJSON(map[string]any{"tenant_id": admin.TenantID, "scope": normalizedAdminScope(admin)}),
			CreatedAt:   time.Now(),
		})
	}

	return token, admin, nil
}

func (s *AdminService) VerifyScopedCredentials(ctx context.Context, username, password, tenantID string) (*store.AdminUser, error) {
	username = strings.TrimSpace(username)
	tenantID = strings.TrimSpace(tenantID)
	var (
		admin *store.AdminUser
		err   error
	)
	if strings.EqualFold(tenantID, ExplicitGlobalAdminTenantScope) {
		admin, err = s.admins.GetByUsernameScoped(ctx, username, "global", "")
		tenantID = ""
	} else if tenantID != "" {
		tenantID = normalizeTenantIDValue(tenantID)
		if strings.EqualFold(tenantID, store.DefaultTenantID) {
			return nil, ErrInvalidAdminCredentials
		}
		admin, err = s.admins.GetByUsernameScoped(ctx, username, "tenant", tenantID)
	} else {
		return nil, ErrInvalidAdminCredentials
	}
	if err != nil {
		return nil, err
	}
	if admin == nil || admin.Status != "active" {
		return nil, ErrInvalidAdminCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(password)); err != nil {
		return nil, ErrInvalidAdminCredentials
	}
	if tenantID == "" && normalizedAdminScope(admin) == "tenant" {
		return nil, ErrInvalidAdminCredentials
	}
	if tenantID != "" && (normalizedAdminScope(admin) != "tenant" || normalizeTenantIDValue(admin.TenantID) != tenantID) {
		return nil, ErrInvalidAdminCredentials
	}
	return admin, nil
}

func (s *AdminService) Authenticate(ctx context.Context, token string) (*store.AdminUser, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, ErrInvalidAdminCredentials
	}

	payload, err := s.parseToken(ctx, token)
	if err != nil {
		return nil, ErrInvalidAdminCredentials
	}
	admin, err := s.admins.GetByUsernameScoped(ctx, payload.Username, payload.Scope, payload.TenantID)
	if err != nil {
		return nil, err
	}
	if admin == nil || admin.Status != "active" {
		return nil, ErrInvalidAdminCredentials
	}
	if adminTokenSignature(admin) != payload.AdminSignature {
		return nil, ErrInvalidAdminCredentials
	}

	return admin, nil
}

func (s *AdminService) ChangePassword(ctx context.Context, username, currentPassword, newPassword string) (string, *store.AdminUser, error) {
	return s.ChangePasswordScoped(ctx, username, currentPassword, newPassword, "global", "")
}

func (s *AdminService) ChangePasswordScoped(ctx context.Context, username, currentPassword, newPassword, scope, tenantID string) (string, *store.AdminUser, error) {
	if strings.TrimSpace(newPassword) == "" {
		return "", nil, fmt.Errorf("new password is required")
	}
	admin, err := s.admins.GetByUsernameScoped(ctx, strings.TrimSpace(username), scope, strings.TrimSpace(tenantID))
	if err != nil {
		return "", nil, err
	}
	if admin == nil || admin.Status != "active" {
		return "", nil, ErrInvalidAdminCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(currentPassword)); err != nil {
		return "", nil, ErrInvalidAdminPassword
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return "", nil, err
	}
	now := time.Now()
	if err := s.admins.UpdatePasswordScoped(ctx, admin.Username, admin.Scope, admin.TenantID, string(passwordHash), now); err != nil {
		return "", nil, err
	}
	admin, err = s.admins.GetByUsernameScoped(ctx, admin.Username, admin.Scope, admin.TenantID)
	if err != nil {
		return "", nil, err
	}
	if admin == nil {
		return "", nil, ErrInvalidAdminCredentials
	}
	if s.audit != nil {
		_ = s.audit.Create(ctx, &store.AdminAuditLog{
			ID:          newID("aa"),
			TenantID:    admin.TenantID,
			AdminUserID: admin.ID,
			Action:      "admin.change_password",
			PayloadJSON: mustJSON(map[string]any{"username": admin.Username}),
			CreatedAt:   now,
		})
	}
	token, err := s.issueToken(ctx, admin)
	if err != nil {
		return "", nil, err
	}
	return token, admin, nil
}

func (s *AdminService) UpdateEmail(ctx context.Context, username, email string) (string, *store.AdminUser, error) {
	return s.UpdateEmailScoped(ctx, username, email, "global", "")
}

func (s *AdminService) UpdateEmailScoped(ctx context.Context, username, email, scope, tenantID string) (string, *store.AdminUser, error) {
	admin, err := s.admins.GetByUsernameScoped(ctx, strings.TrimSpace(username), scope, strings.TrimSpace(tenantID))
	if err != nil {
		return "", nil, err
	}
	if admin == nil || admin.Status != "active" {
		return "", nil, ErrInvalidAdminCredentials
	}

	email = normalizeEmail(email)
	if !isValidAdminEmail(email) {
		return "", nil, errors.New("valid admin email is required")
	}

	now := time.Now()
	if err := s.admins.UpdateEmailScoped(ctx, admin.Username, admin.Scope, admin.TenantID, email, now); err != nil {
		return "", nil, err
	}
	if normalizedAdminScope(admin) == "global" {
		if err := s.settings.Set(ctx, "admin_email", mustJSON(map[string]string{"value": email})); err != nil {
			return "", nil, err
		}
	}

	admin, err = s.admins.GetByUsernameScoped(ctx, admin.Username, admin.Scope, admin.TenantID)
	if err != nil {
		return "", nil, err
	}
	if admin == nil {
		return "", nil, ErrInvalidAdminCredentials
	}
	if s.audit != nil {
		_ = s.audit.Create(ctx, &store.AdminAuditLog{
			ID:          newID("aa"),
			TenantID:    admin.TenantID,
			AdminUserID: admin.ID,
			Action:      "admin.update_email",
			PayloadJSON: mustJSON(map[string]any{"username": admin.Username, "email": admin.Email}),
			CreatedAt:   now,
		})
	}

	token, err := s.issueToken(ctx, admin)
	if err != nil {
		return "", nil, err
	}
	return token, admin, nil
}

func normalizeEmail(email string) string {
	return strings.TrimSpace(strings.ToLower(email))
}

var adminEmailPattern = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)
var adminEmailSlugPattern = regexp.MustCompile(`[^a-z0-9._-]+`)

func isValidAdminEmail(email string) bool {
	return adminEmailPattern.MatchString(normalizeEmail(email))
}

func synthesizeAdminEmail(username string) string {
	slug := strings.ToLower(strings.TrimSpace(username))
	slug = adminEmailSlugPattern.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-.")
	if slug == "" {
		slug = "admin"
	}
	return slug + "@local.admin"
}

// idCounter ensures newID returns unique values even when called multiple
// times within the same nanosecond (e.g. enrollment issuing both a machine
// token and a viewer token in quick succession).
var idCounter atomic.Int64

func newID(prefix string) string {
	seq := idCounter.Add(1)
	return fmt.Sprintf("%s_%d_%d", prefix, time.Now().UnixNano(), seq)
}

func randomToken(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func (s *AdminService) issueToken(ctx context.Context, admin *store.AdminUser) (string, error) {
	secret, err := s.tokenSecret(ctx)
	if err != nil {
		return "", err
	}
	nonce, err := randomToken(16)
	if err != nil {
		return "", err
	}
	payload := signedAdminTokenPayload{
		Username:       admin.Username,
		IssuedAt:       time.Now().Unix(),
		AdminSignature: adminTokenSignature(admin),
		Scope:          normalizedAdminScope(admin),
		Role:           normalizedAdminRole(admin),
		TenantID:       strings.TrimSpace(admin.TenantID),
		Nonce:          nonce,
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(rawPayload)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(encodedPayload))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encodedPayload + "." + signature, nil
}

func (s *AdminService) parseToken(ctx context.Context, token string) (*signedAdminTokenPayload, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, ErrInvalidAdminCredentials
	}
	secret, err := s.tokenSecret(ctx)
	if err != nil {
		return nil, err
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(parts[0]))
	expectedSig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrInvalidAdminCredentials
	}
	if !hmac.Equal(mac.Sum(nil), expectedSig) {
		return nil, ErrInvalidAdminCredentials
	}
	rawPayload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, ErrInvalidAdminCredentials
	}
	var payload signedAdminTokenPayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return nil, ErrInvalidAdminCredentials
	}
	if strings.TrimSpace(payload.Username) == "" || strings.TrimSpace(payload.AdminSignature) == "" {
		return nil, ErrInvalidAdminCredentials
	}
	return &payload, nil
}

func (s *AdminService) tokenSecret(ctx context.Context) (string, error) {
	raw, err := s.settings.Get(ctx, adminTokenSecretKey)
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
	secret, err := randomToken(32)
	if err != nil {
		return "", err
	}
	if err := s.settings.Set(ctx, adminTokenSecretKey, mustJSON(map[string]string{"value": secret})); err != nil {
		return "", err
	}
	return secret, nil
}

func normalizedAdminScope(admin *store.AdminUser) string {
	if admin != nil && strings.EqualFold(strings.TrimSpace(admin.Scope), "tenant") {
		return "tenant"
	}
	return "global"
}

func normalizedAdminRole(admin *store.AdminUser) string {
	if admin != nil && strings.TrimSpace(admin.Role) != "" {
		return strings.TrimSpace(admin.Role)
	}
	if normalizedAdminScope(admin) == "tenant" {
		return "tenant_owner"
	}
	return "global_owner"
}

func normalizedTenantAdminRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "tenant_admin", "tenant_owner":
		return strings.ToLower(strings.TrimSpace(role))
	default:
		return "tenant_owner"
	}
}

func adminTokenSignature(admin *store.AdminUser) string {
	sum := sha256.Sum256([]byte(admin.PasswordHash + "|" + admin.Status + "|" + normalizeEmail(admin.Email) + "|" + normalizedAdminScope(admin) + "|" + normalizedAdminRole(admin) + "|" + strings.TrimSpace(admin.TenantID)))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func mustJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return `{}`
	}
	return string(data)
}
