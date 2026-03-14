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

type signedAdminTokenPayload struct {
	Username       string `json:"username"`
	IssuedAt       int64  `json:"issued_at"`
	AdminSignature string `json:"admin_signature"`
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
	if email == "" {
		return fmt.Errorf("admin email is required")
	}
	admin := &store.AdminUser{
		ID:           newID("adm"),
		Username:     strings.TrimSpace(username),
		PasswordHash: string(passwordHash),
		Email:        email,
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
			AdminUserID: admin.ID,
			Action:      "admin.setup",
			PayloadJSON: mustJSON(map[string]any{"username": admin.Username, "email": admin.Email}),
			CreatedAt:   now,
		})
	}

	return nil
}

func (s *AdminService) ResetAdminCredentials(ctx context.Context, username, password string) error {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	now := time.Now()
	admin := &store.AdminUser{
		ID:           newID("adm"),
		Username:     strings.TrimSpace(username),
		PasswordHash: string(passwordHash),
		Email:        synthesizeAdminEmail(username),
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
			AdminUserID: admin.ID,
			Action:      "admin.reset_credentials",
			PayloadJSON: mustJSON(map[string]any{"username": admin.Username, "email": admin.Email}),
			CreatedAt:   now,
		})
	}

	return nil
}

func (s *AdminService) Login(ctx context.Context, username, password string) (string, *store.AdminUser, error) {
	admin, err := s.admins.GetByUsername(ctx, strings.TrimSpace(username))
	if err != nil {
		return "", nil, err
	}
	if admin == nil {
		return "", nil, ErrInvalidAdminCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(password)); err != nil {
		return "", nil, ErrInvalidAdminCredentials
	}

	token, err := s.issueToken(ctx, admin)
	if err != nil {
		return "", nil, err
	}

	if s.audit != nil {
		_ = s.audit.Create(ctx, &store.AdminAuditLog{
			ID:          newID("aa"),
			AdminUserID: admin.ID,
			Action:      "admin.login",
			PayloadJSON: `{}`,
			CreatedAt:   time.Now(),
		})
	}

	return token, admin, nil
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
	admin, err := s.admins.GetByUsername(ctx, payload.Username)
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
	admin, err := s.admins.GetByUsername(ctx, strings.TrimSpace(username))
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
	if err := s.admins.UpdatePassword(ctx, admin.Username, string(passwordHash), now); err != nil {
		return "", nil, err
	}
	admin, err = s.admins.GetByUsername(ctx, admin.Username)
	if err != nil {
		return "", nil, err
	}
	if admin == nil {
		return "", nil, ErrInvalidAdminCredentials
	}
	if s.audit != nil {
		_ = s.audit.Create(ctx, &store.AdminAuditLog{
			ID:          newID("aa"),
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
	admin, err := s.admins.GetByUsername(ctx, strings.TrimSpace(username))
	if err != nil {
		return "", nil, err
	}
	if admin == nil || admin.Status != "active" {
		return "", nil, ErrInvalidAdminCredentials
	}

	email = normalizeEmail(email)
	if email == "" {
		return "", nil, errors.New("admin email is required")
	}

	now := time.Now()
	if err := s.admins.UpdateEmail(ctx, admin.Username, email, now); err != nil {
		return "", nil, err
	}
	if err := s.settings.Set(ctx, "admin_email", mustJSON(map[string]string{"value": email})); err != nil {
		return "", nil, err
	}

	admin, err = s.admins.GetByUsername(ctx, admin.Username)
	if err != nil {
		return "", nil, err
	}
	if admin == nil {
		return "", nil, ErrInvalidAdminCredentials
	}
	if s.audit != nil {
		_ = s.audit.Create(ctx, &store.AdminAuditLog{
			ID:          newID("aa"),
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

var adminEmailSlugPattern = regexp.MustCompile(`[^a-z0-9._-]+`)

func synthesizeAdminEmail(username string) string {
	slug := strings.ToLower(strings.TrimSpace(username))
	slug = adminEmailSlugPattern.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-.")
	if slug == "" {
		slug = "admin"
	}
	return slug + "@local.admin"
}

func newID(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
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

func adminTokenSignature(admin *store.AdminUser) string {
	sum := sha256.Sum256([]byte(admin.PasswordHash + "|" + admin.Status + "|" + normalizeEmail(admin.Email)))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func mustJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return `{}`
	}
	return string(data)
}
