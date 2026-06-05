package structureddata

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const defaultAdminSessionTTL = 12 * time.Hour
const defaultAdminPasswordMinLength = 8
const defaultAdminLoginLockoutMinutes = 15

type adminUserRecord struct {
	ID                string
	TenantID          string
	Username          string
	DisplayName       string
	Role              string
	AdminScope        string
	Enabled           bool
	LastLoginAt       *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
	PasswordHash      string
	LoginFailureCount int
	LoginLockedUntil  time.Time
}

type adminSessionRecord struct {
	ID         string
	TenantID   string
	UserID     string
	Username   string
	Role       string
	AdminScope string
	TokenHash  string
	ExpiresAt  time.Time
	CreatedAt  time.Time
}

func (s *Service) SetupStatus(ctx context.Context) (*SetupStatus, error) {
	initialized, err := s.store.AdminInitialized(ctx)
	if err != nil {
		return nil, err
	}
	tenants, err := s.store.ListDataTenants(ctx)
	if err != nil {
		return nil, err
	}
	hubRegistration, err := s.store.GetHubRegistration(ctx)
	if err != nil {
		return nil, err
	}
	mode := "local_admin"
	if hubRegistration != nil && strings.TrimSpace(hubRegistration.HubBaseURL) != "" {
		mode = "hub_tenant_admin"
	}
	return &SetupStatus{
		Initialized:     initialized,
		TenantID:        "default",
		Mode:            mode,
		AdminScopes:     []string{"global", "tenant"},
		Tenants:         tenants,
		HubRegistration: ptrHubRegistrationStatus(hubRegistration),
		PasswordPolicy:  s.adminPasswordPolicy(),
	}, nil
}

func ptrHubRegistrationStatus(record *hubRegistrationRecord) *HubRegistrationStatus {
	status := publicHubRegistrationStatusFromRecord(record)
	return &status
}

func (s *Service) InitializeAdmin(ctx context.Context, in InitializeAdminInput) (*InitializeAdminResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	initialized, err := s.store.AdminInitialized(ctx)
	if err != nil {
		return nil, err
	}
	if initialized {
		return nil, ErrAlreadyExists
	}
	tenantID := normalizedAdminTenant(in.TenantID)
	username, err := normalizedAdminUsername(in.Username)
	if err != nil {
		return nil, err
	}
	password := strings.TrimSpace(in.Password)
	if err := s.validateAdminPassword(password); err != nil {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	user := adminUserRecord{
		ID:           newID("admin"),
		TenantID:     tenantID,
		Username:     username,
		DisplayName:  trimForStorage(in.DisplayName, 120),
		Role:         "data_admin",
		AdminScope:   "global",
		Enabled:      true,
		PasswordHash: string(hash),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if _, err := s.store.CreateAdminUser(ctx, user); err != nil {
		return nil, err
	}
	login, err := s.createAdminSession(ctx, user, tenantID, in.ExpiresHours)
	if err != nil {
		return nil, err
	}
	s.audit(ctx, Principal{TenantID: tenantID, UserID: user.ID, Role: user.Role, AdminScope: user.AdminScope}, "admin.setup_initialize", "", "admin_user", user.ID, "Initialized first global administrator "+username, map[string]any{"username": username, "role": user.Role, "admin_scope": user.AdminScope})
	return &InitializeAdminResult{
		Initialized: true,
		TenantID:    tenantID,
		Username:    username,
		DisplayName: user.DisplayName,
		Role:        user.Role,
		AdminScope:  user.AdminScope,
		Token:       login.Token,
		ExpiresAt:   login.ExpiresAt,
	}, nil
}

func (s *Service) Login(ctx context.Context, in LoginInput) (*LoginResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tenantID := normalizedAdminTenant(in.TenantID)
	username, err := normalizedAdminUsername(in.Username)
	if err != nil {
		return nil, ErrUnauthorized
	}
	now := s.now().UTC()
	users, err := s.findAdminUsersForLogin(ctx, tenantID, username)
	if err != nil {
		return nil, ErrUnauthorized
	}
	password := strings.TrimSpace(in.Password)
	for _, user := range users {
		if user == nil || s.adminLoginLocked(*user, now) || !user.Enabled {
			continue
		}
		if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
			continue
		}
		_ = s.store.TouchAdminLogin(ctx, user.TenantID, user.ID, now)
		if err := s.store.ClearAdminLoginFailure(ctx, user.TenantID, username, now); err != nil {
			return nil, err
		}
		login, err := s.createAdminSession(ctx, *user, tenantID, in.ExpiresHours)
		if err != nil {
			return nil, err
		}
		s.audit(ctx, Principal{TenantID: tenantID, UserID: user.ID, Role: user.Role, AdminScope: user.AdminScope}, "admin.login", "", "admin_user", user.ID, "Administrator login "+user.Username, map[string]any{"username": user.Username, "role": user.Role, "admin_scope": user.AdminScope, "expires_at": login.ExpiresAt})
		return login, nil
	}
	return nil, s.recordAdminLoginFailures(ctx, users, username, now)
}

func (s *Service) CreateAdminAccount(ctx context.Context, p Principal, in CreateAdminAccountInput) (*AdminAccountResult, error) {
	if !principalCanAdmin(p) {
		return nil, ErrForbidden
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !principalIsGlobalAdmin(p) && strings.TrimSpace(in.TenantID) == "" {
		in.TenantID = p.TenantID
	}
	adminScope, tenantID, err := normalizeAdminScopeTenant(in.AdminScope, in.TenantID)
	if err != nil {
		return nil, err
	}
	if adminScope == "global" && !principalIsGlobalAdmin(p) {
		return nil, ErrForbidden
	}
	if err := requireAdminTenantAccess(p, tenantID); err != nil {
		return nil, err
	}
	username, err := normalizedAdminUsername(in.Username)
	if err != nil {
		return nil, err
	}
	if adminScope == "global" {
		if err := s.ensureGlobalAdminUsernameAvailable(ctx, username, ""); err != nil {
			return nil, err
		}
	} else {
		if err := s.ensureTenantAdminUsernameDoesNotShadowGlobal(ctx, username); err != nil {
			return nil, err
		}
	}
	password := strings.TrimSpace(in.Password)
	if err := s.validateAdminPassword(password); err != nil {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	role, err := normalizeAdminRole(in.Role)
	if err != nil {
		return nil, err
	}
	user := adminUserRecord{
		ID:           newID("admin"),
		TenantID:     tenantID,
		Username:     username,
		DisplayName:  trimForStorage(in.DisplayName, 120),
		Role:         role,
		AdminScope:   adminScope,
		Enabled:      true,
		PasswordHash: string(hash),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	out, err := s.store.CreateAdminUser(ctx, user)
	if err != nil {
		return nil, err
	}
	s.audit(ctx, p, "admin.account_create", "", "admin_user", out.ID, "Created administrator account "+out.Username, map[string]any{"username": out.Username, "role": out.Role, "admin_scope": out.AdminScope})
	return &AdminAccountResult{Account: adminAccountInfoFromRecord(*out)}, nil
}

func (s *Service) ListAdminAccounts(ctx context.Context, tenantID string) (*ListAdminAccountsResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	users, err := s.store.ListAdminUsers(ctx, normalizedAdminTenant(tenantID))
	if err != nil {
		return nil, err
	}
	out := make([]AdminAccountInfo, 0, len(users))
	for _, user := range users {
		out = append(out, adminAccountInfoFromRecord(user))
	}
	return &ListAdminAccountsResult{Items: out}, nil
}

func (s *Service) ListAdminAccountsForPrincipal(ctx context.Context, p Principal, tenantID string) (*ListAdminAccountsResult, error) {
	if !principalCanAdmin(p) {
		return nil, ErrForbidden
	}
	if strings.TrimSpace(tenantID) == "" {
		if principalIsGlobalAdmin(p) {
			tenantID = "all"
		} else {
			tenantID = p.TenantID
		}
	}
	if err := requireAdminTenantAccess(p, normalizedAdminTenant(tenantID)); err != nil {
		return nil, err
	}
	return s.ListAdminAccounts(ctx, tenantID)
}

func (s *Service) UpdateAdminAccount(ctx context.Context, p Principal, tenantID, username string, in UpdateAdminAccountInput) (*AdminAccountResult, error) {
	if !principalCanAdmin(p) {
		return nil, ErrForbidden
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !principalIsGlobalAdmin(p) && strings.TrimSpace(tenantID) == "" {
		tenantID = p.TenantID
	}
	tenantID = normalizedAdminTenant(tenantID)
	if err := requireAdminTenantAccess(p, tenantID); err != nil {
		return nil, err
	}
	username, err := normalizedAdminUsername(username)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Role) != "" {
		role, err := normalizeAdminRole(in.Role)
		if err != nil {
			return nil, err
		}
		in.Role = role
	}
	if strings.TrimSpace(in.AdminScope) != "" {
		adminScope, scopeTenant, err := normalizeAdminScopeTenant(in.AdminScope, tenantID)
		if err != nil {
			return nil, err
		}
		if adminScope == "global" && !principalIsGlobalAdmin(p) {
			return nil, ErrForbidden
		}
		if adminScope == "global" {
			if err := s.ensureGlobalAdminUsernameAvailable(ctx, username, tenantID); err != nil {
				return nil, err
			}
		}
		in.AdminScope = adminScope
		tenantID = scopeTenant
	}
	existing, err := s.store.FindAdminUser(ctx, tenantID, username)
	if err != nil {
		return nil, err
	}
	nextEnabled := existing.Enabled
	if in.Enabled != nil {
		nextEnabled = *in.Enabled
	}
	nextRole := existing.Role
	if strings.TrimSpace(in.Role) != "" {
		nextRole = in.Role
	}
	nextAdminScope := normalizedAdminScope(existing.AdminScope)
	if strings.TrimSpace(in.AdminScope) != "" {
		nextAdminScope = normalizedAdminScope(in.AdminScope)
	}
	if existing.Enabled && strings.EqualFold(existing.Role, "data_admin") && normalizedAdminScope(existing.AdminScope) == "global" && (!nextEnabled || !strings.EqualFold(nextRole, "data_admin") || nextAdminScope != "global") {
		users, err := s.store.ListAdminUsers(ctx, "all")
		if err != nil {
			return nil, err
		}
		enabledGlobalAdminCount := 0
		for _, user := range users {
			if user.Enabled && strings.EqualFold(user.Role, "data_admin") && normalizedAdminScope(user.AdminScope) == "global" {
				enabledGlobalAdminCount++
			}
		}
		if enabledGlobalAdminCount <= 1 {
			return nil, fmt.Errorf("%w: cannot remove the last enabled global data_admin administrator", ErrInvalidInput)
		}
	}
	out, err := s.store.UpdateAdminUser(ctx, tenantID, username, in, s.now().UTC())
	if err != nil {
		return nil, err
	}
	if adminAccountSessionInvalidated(*existing, *out) {
		sessionTenantID := out.TenantID
		if normalizedAdminScope(existing.AdminScope) == "global" {
			sessionTenantID = "all"
		}
		if err := s.store.DeleteAdminSessionsForUser(ctx, sessionTenantID, out.ID); err != nil {
			return nil, err
		}
	}
	s.audit(ctx, p, "admin.account_update", "", "admin_user", out.ID, "Updated administrator account "+out.Username, map[string]any{"username": out.Username, "role": out.Role, "enabled": out.Enabled})
	return &AdminAccountResult{Account: adminAccountInfoFromRecord(*out)}, nil
}

func (s *Service) ResetAdminPassword(ctx context.Context, in ResetAdminPasswordInput) (*ResetAdminPasswordResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tenantID := normalizedAdminTenant(in.TenantID)
	username, err := normalizedAdminUsername(in.Username)
	if err != nil {
		return nil, err
	}
	password := strings.TrimSpace(in.Password)
	if err := s.validateAdminPassword(password); err != nil {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	user, err := s.store.UpdateAdminPassword(ctx, tenantID, username, string(hash), now)
	if err != nil {
		return nil, err
	}
	sessionTenantID := user.TenantID
	if normalizedAdminScope(user.AdminScope) == "global" {
		sessionTenantID = "all"
	}
	if err := s.store.DeleteAdminSessionsForUser(ctx, sessionTenantID, user.ID); err != nil {
		return nil, err
	}
	s.audit(ctx, Principal{TenantID: user.TenantID, UserID: "offline_admin_recovery", Role: "data_admin"}, "admin.password_reset", "", "admin_user", user.ID, "Reset administrator password "+user.Username, map[string]any{"username": user.Username})
	return &ResetAdminPasswordResult{TenantID: user.TenantID, Username: user.Username, UpdatedAt: user.UpdatedAt}, nil
}

func (s *Service) FindAdminSessionBySecret(ctx context.Context, token string) (*Principal, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, ErrUnauthorized
	}
	session, err := s.store.FindAdminSessionByHash(ctx, apiKeyHash(token), s.now().UTC())
	if err != nil {
		return nil, ErrUnauthorized
	}
	return &Principal{
		TenantID:   session.TenantID,
		UserID:     session.UserID,
		Role:       session.Role,
		AdminScope: normalizedAdminScope(session.AdminScope),
		Policy: &APIKeyPolicy{
			ID:             session.ID,
			TenantID:       session.TenantID,
			UserID:         session.UserID,
			Role:           session.Role,
			AllowRawData:   true,
			AllowSensitive: true,
			AllowAdmin:     true,
		},
	}, nil
}

func (s *Service) ListAdminSessionsForPrincipal(ctx context.Context, p Principal, tenantID string) (*ListAdminSessionsResult, error) {
	if !principalCanAdmin(p) {
		return nil, ErrForbidden
	}
	if strings.TrimSpace(tenantID) == "" {
		if principalIsGlobalAdmin(p) {
			tenantID = "all"
		} else {
			tenantID = p.TenantID
		}
	}
	if err := requireAdminTenantAccess(p, normalizedAdminTenant(tenantID)); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	sessions, err := s.store.ListAdminSessions(ctx, normalizedAdminTenant(tenantID), s.now().UTC())
	if err != nil {
		return nil, err
	}
	currentID := ""
	if p.Policy != nil {
		currentID = strings.TrimSpace(p.Policy.ID)
	}
	out := make([]AdminSessionInfo, 0, len(sessions))
	for _, session := range sessions {
		out = append(out, adminSessionInfoFromRecord(session, currentID))
	}
	return &ListAdminSessionsResult{Items: out}, nil
}

func (s *Service) UpdateAdminSession(ctx context.Context, p Principal, tenantID, sessionID string, in UpdateAdminSessionInput) (*AdminSessionResult, error) {
	if !principalCanAdmin(p) {
		return nil, ErrForbidden
	}
	if !principalIsGlobalAdmin(p) && strings.TrimSpace(tenantID) == "" {
		tenantID = p.TenantID
	}
	tenantID = normalizedAdminTenant(tenantID)
	if err := requireAdminTenantAccess(p, tenantID); err != nil {
		return nil, err
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	now := s.now().UTC()
	expiresAt, err := adminSessionExpiresAtFromInput(now, in)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if principalIsGlobalAdmin(p) && adminTenantSelectorIsAll(tenantID) {
		resolvedTenantID, err := s.resolveAdminSessionTenant(ctx, sessionID, now)
		if err != nil {
			return nil, err
		}
		tenantID = resolvedTenantID
	}
	session, err := s.store.UpdateAdminSessionExpiresAt(ctx, tenantID, sessionID, expiresAt, now)
	if err != nil {
		return nil, err
	}
	currentID := ""
	if p.Policy != nil {
		currentID = strings.TrimSpace(p.Policy.ID)
	}
	s.audit(ctx, p, "admin.session_update", "", "admin_session", sessionID, "Updated administrator session expiry "+sessionID, map[string]any{"expires_at": session.ExpiresAt})
	return &AdminSessionResult{Session: adminSessionInfoFromRecord(*session, currentID)}, nil
}

func (s *Service) RevokeAdminSession(ctx context.Context, p Principal, tenantID, sessionID string) (*RevokeAdminSessionResult, error) {
	if !principalCanAdmin(p) {
		return nil, ErrForbidden
	}
	if !principalIsGlobalAdmin(p) && strings.TrimSpace(tenantID) == "" {
		tenantID = p.TenantID
	}
	tenantID = normalizedAdminTenant(tenantID)
	if err := requireAdminTenantAccess(p, tenantID); err != nil {
		return nil, err
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if principalIsGlobalAdmin(p) && adminTenantSelectorIsAll(tenantID) {
		resolvedTenantID, err := s.resolveAdminSessionTenant(ctx, sessionID, s.now().UTC())
		if err != nil {
			return nil, err
		}
		tenantID = resolvedTenantID
	}
	if err := s.store.DeleteAdminSession(ctx, tenantID, sessionID); err != nil {
		return nil, err
	}
	s.audit(ctx, p, "admin.session_revoke", "", "admin_session", sessionID, "Revoked administrator session "+sessionID, nil)
	return &RevokeAdminSessionResult{SessionID: sessionID, Revoked: true}, nil
}

func adminAccountInfoFromRecord(user adminUserRecord) AdminAccountInfo {
	return AdminAccountInfo{
		ID:          user.ID,
		TenantID:    user.TenantID,
		Username:    user.Username,
		DisplayName: user.DisplayName,
		Role:        user.Role,
		AdminScope:  normalizedAdminScope(user.AdminScope),
		Enabled:     user.Enabled,
		LastLoginAt: user.LastLoginAt,
		CreatedAt:   user.CreatedAt,
		UpdatedAt:   user.UpdatedAt,
	}
}

func adminSessionInfoFromRecord(session adminSessionRecord, currentID string) AdminSessionInfo {
	return AdminSessionInfo{
		ID:         session.ID,
		TenantID:   session.TenantID,
		UserID:     session.UserID,
		Username:   session.Username,
		Role:       session.Role,
		AdminScope: normalizedAdminScope(session.AdminScope),
		Current:    currentID != "" && session.ID == currentID,
		ExpiresAt:  session.ExpiresAt,
		CreatedAt:  session.CreatedAt,
	}
}

func adminAccountSessionInvalidated(before adminUserRecord, after adminUserRecord) bool {
	return before.Enabled != after.Enabled ||
		!strings.EqualFold(before.Role, after.Role) ||
		normalizedAdminScope(before.AdminScope) != normalizedAdminScope(after.AdminScope)
}

func (s *Service) createAdminSession(ctx context.Context, user adminUserRecord, requestedTenantID string, expiresHours int) (*LoginResult, error) {
	ttl := defaultAdminSessionTTL
	if expiresHours > 0 && expiresHours <= 168 {
		ttl = time.Duration(expiresHours) * time.Hour
	}
	now := s.now().UTC()
	token := generateAPIKeySecret()
	sessionTenantID := user.TenantID
	if normalizedAdminScope(user.AdminScope) == "global" {
		sessionTenantID = normalizedAdminTenant(requestedTenantID)
	}
	if err := s.store.DeleteExpiredAdminSessions(ctx, now); err != nil {
		return nil, err
	}
	session := adminSessionRecord{
		ID:         newID("sess"),
		TenantID:   sessionTenantID,
		UserID:     user.ID,
		Username:   user.Username,
		Role:       user.Role,
		AdminScope: normalizedAdminScope(user.AdminScope),
		TokenHash:  apiKeyHash(token),
		ExpiresAt:  now.Add(ttl),
		CreatedAt:  now,
	}
	if _, err := s.store.CreateAdminSession(ctx, session); err != nil {
		return nil, err
	}
	return &LoginResult{TenantID: sessionTenantID, Username: user.Username, Role: user.Role, AdminScope: normalizedAdminScope(user.AdminScope), Token: token, ExpiresAt: session.ExpiresAt}, nil
}

func (s *Service) findAdminUsersForLogin(ctx context.Context, tenantID, username string) ([]*adminUserRecord, error) {
	users := make([]*adminUserRecord, 0, 2)
	user, err := s.store.FindAdminUser(ctx, tenantID, username)
	if err == nil {
		users = append(users, user)
	} else if !errors.Is(err, ErrAdminNotFound) {
		return nil, err
	}
	globalUser, err := s.findGlobalAdminUserByUsername(ctx, username)
	if err == nil && !adminUserAlreadyIncluded(users, globalUser) {
		users = append(users, globalUser)
	} else if err != nil && !errors.Is(err, ErrAdminNotFound) {
		return nil, err
	}
	if len(users) == 0 {
		return nil, ErrAdminNotFound
	}
	return users, nil
}

func adminUserAlreadyIncluded(users []*adminUserRecord, candidate *adminUserRecord) bool {
	if candidate == nil {
		return true
	}
	for _, user := range users {
		if user != nil && strings.EqualFold(user.ID, candidate.ID) {
			return true
		}
	}
	return false
}

func (s *Service) resolveAdminSessionTenant(ctx context.Context, sessionID string, now time.Time) (string, error) {
	sessions, err := s.store.ListAdminSessions(ctx, "all", now)
	if err != nil {
		return "", err
	}
	for _, session := range sessions {
		if strings.EqualFold(session.ID, sessionID) {
			return session.TenantID, nil
		}
	}
	return "", ErrSessionNotFound
}

func adminTenantSelectorIsAll(tenantID string) bool {
	tenantID = strings.TrimSpace(tenantID)
	return strings.EqualFold(tenantID, "all") || tenantID == "*"
}

func (s *Service) findGlobalAdminUserByUsername(ctx context.Context, username string) (*adminUserRecord, error) {
	users, err := s.store.ListAdminUsers(ctx, "all")
	if err != nil {
		return nil, err
	}
	for _, user := range users {
		if strings.EqualFold(user.Username, username) && normalizedAdminScope(user.AdminScope) == "global" {
			return &user, nil
		}
	}
	return nil, ErrAdminNotFound
}

func (s *Service) ensureGlobalAdminUsernameAvailable(ctx context.Context, username, currentTenantID string) error {
	users, err := s.store.ListAdminUsers(ctx, "all")
	if err != nil {
		return err
	}
	currentTenantID = strings.TrimSpace(currentTenantID)
	for _, user := range users {
		if !strings.EqualFold(user.Username, username) {
			continue
		}
		if currentTenantID != "" && strings.EqualFold(normalizedAdminTenant(user.TenantID), normalizedAdminTenant(currentTenantID)) {
			continue
		}
		return fmt.Errorf("%w: global administrator username must be unique across tenants", ErrAlreadyExists)
	}
	return nil
}

func (s *Service) ensureTenantAdminUsernameDoesNotShadowGlobal(ctx context.Context, username string) error {
	users, err := s.store.ListAdminUsers(ctx, "all")
	if err != nil {
		return err
	}
	for _, user := range users {
		if strings.EqualFold(user.Username, username) && normalizedAdminScope(user.AdminScope) == "global" {
			return fmt.Errorf("%w: tenant administrator username must not shadow a global administrator", ErrAlreadyExists)
		}
	}
	return nil
}

func normalizedAdminScope(scope string) string {
	if strings.EqualFold(strings.TrimSpace(scope), "tenant") {
		return "tenant"
	}
	return "global"
}

func normalizeAdminScopeTenant(scope, tenantID string) (string, string, error) {
	scope = strings.ToLower(strings.TrimSpace(scope))
	if scope == "" {
		scope = "tenant"
	}
	switch scope {
	case "global":
		return "global", normalizedAdminTenant(tenantID), nil
	case "tenant":
		return "tenant", normalizedAdminTenant(tenantID), nil
	default:
		return "", "", fmt.Errorf("%w: admin_scope must be global or tenant", ErrInvalidInput)
	}
}

func principalAdminScope(p Principal) string {
	if p.AdminScope != "" {
		return normalizedAdminScope(p.AdminScope)
	}
	return "tenant"
}

func principalIsGlobalAdmin(p Principal) bool {
	return principalCanAdmin(p) && principalAdminScope(p) == "global"
}

func requireAdminTenantAccess(p Principal, tenantID string) error {
	if principalIsGlobalAdmin(p) {
		return nil
	}
	if strings.EqualFold(normalizedAdminTenant(p.TenantID), normalizedAdminTenant(tenantID)) {
		return nil
	}
	return ErrForbidden
}

func normalizedAdminTenant(value string) string {
	value = strings.TrimSpace(value)
	if strings.EqualFold(value, "__global__") {
		return ""
	}
	if value == "" {
		return "default"
	}
	return value
}

func normalizedAdminUsername(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) < 3 || len(value) > 64 {
		return "", fmt.Errorf("%w: username must be 3-64 characters", ErrInvalidInput)
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			continue
		}
		return "", fmt.Errorf("%w: username contains unsupported characters", ErrInvalidInput)
	}
	return value, nil
}

func normalizeAdminRole(role string) (string, error) {
	role = strings.ToLower(strings.TrimSpace(role))
	switch role {
	case "", "data_admin":
		return "data_admin", nil
	case "data_auditor", "data_user":
		return role, nil
	default:
		return "", fmt.Errorf("%w: role must be data_admin, data_auditor, or data_user", ErrInvalidInput)
	}
}

func (s *Service) validateAdminPassword(password string) error {
	minLength := s.adminPasswordMinLength
	if minLength < defaultAdminPasswordMinLength {
		minLength = defaultAdminPasswordMinLength
	}
	if len(password) < minLength {
		return fmt.Errorf("%w: password must be at least %d characters", ErrInvalidInput, minLength)
	}
	return nil
}

func (s *Service) adminPasswordPolicy() *AdminPasswordPolicy {
	lockoutMinutes := 0
	if s.adminLoginMaxFailures > 0 {
		lockoutMinutes = int(s.adminLoginLockout / time.Minute)
	}
	return &AdminPasswordPolicy{
		MinLength:             effectiveAdminPasswordMinLength(s.adminPasswordMinLength),
		RotationDays:          0,
		LockoutEnabled:        s.adminLoginMaxFailures > 0,
		LoginMaxFailures:      s.adminLoginMaxFailures,
		LoginLockoutMinutes:   lockoutMinutes,
		OfflineResetAvailable: true,
	}
}

func effectiveAdminPasswordMinLength(value int) int {
	if value < defaultAdminPasswordMinLength {
		return defaultAdminPasswordMinLength
	}
	if value > 128 {
		return 128
	}
	return value
}

func adminPasswordMinLengthFromEnv() int {
	raw := strings.TrimSpace(os.Getenv("MACLAW_DATA_ADMIN_PASSWORD_MIN_LENGTH"))
	if raw == "" {
		return defaultAdminPasswordMinLength
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return defaultAdminPasswordMinLength
	}
	return effectiveAdminPasswordMinLength(value)
}

func adminLoginMaxFailuresFromEnv() int {
	value := intFromEnv("MACLAW_DATA_ADMIN_LOGIN_MAX_FAILURES", 0)
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func adminLoginLockoutFromEnv() time.Duration {
	minutes := intFromEnv("MACLAW_DATA_ADMIN_LOGIN_LOCKOUT_MINUTES", defaultAdminLoginLockoutMinutes)
	if minutes < 1 {
		minutes = defaultAdminLoginLockoutMinutes
	}
	if minutes > 1440 {
		minutes = 1440
	}
	return time.Duration(minutes) * time.Minute
}

func intFromEnv(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func (s *Service) adminLoginLocked(user adminUserRecord, now time.Time) bool {
	if s.adminLoginMaxFailures <= 0 {
		return false
	}
	return !user.LoginLockedUntil.IsZero() && user.LoginLockedUntil.After(now)
}

func (s *Service) recordAdminLoginFailure(ctx context.Context, tenantID, username string, now time.Time) error {
	if s.adminLoginMaxFailures <= 0 {
		return nil
	}
	_, err := s.store.RecordAdminLoginFailure(ctx, tenantID, username, now, s.adminLoginMaxFailures, s.adminLoginLockout)
	return err
}

func (s *Service) recordAdminLoginFailures(ctx context.Context, users []*adminUserRecord, username string, now time.Time) error {
	if s.adminLoginMaxFailures <= 0 {
		return ErrUnauthorized
	}
	for _, user := range users {
		if user == nil {
			continue
		}
		if err := s.recordAdminLoginFailure(ctx, user.TenantID, username, now); err != nil {
			return err
		}
	}
	return ErrUnauthorized
}

func adminSessionExpiresAtFromInput(now time.Time, in UpdateAdminSessionInput) (time.Time, error) {
	if in.ExpiresAt != nil && in.ExpiresHours > 0 {
		return time.Time{}, fmt.Errorf("%w: use either expires_at or expires_hours", ErrInvalidInput)
	}
	var expiresAt time.Time
	switch {
	case in.ExpiresAt != nil:
		expiresAt = in.ExpiresAt.UTC()
	case in.ExpiresHours > 0:
		expiresAt = now.Add(time.Duration(in.ExpiresHours) * time.Hour)
	default:
		return time.Time{}, fmt.Errorf("%w: expires_at or expires_hours is required", ErrInvalidInput)
	}
	if !expiresAt.After(now) {
		return time.Time{}, fmt.Errorf("%w: session expiry must be in the future", ErrInvalidInput)
	}
	if expiresAt.After(now.Add(168 * time.Hour)) {
		return time.Time{}, fmt.Errorf("%w: session expiry cannot exceed 168 hours", ErrInvalidInput)
	}
	return expiresAt, nil
}
