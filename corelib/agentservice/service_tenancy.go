package agentservice

import (
	"context"
	"errors"
	"fmt"
	"github.com/RapidAI/CodeClaw/corelib"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (s *Service) CreateTenant(ctx context.Context, in CreateTenantInput) (*Tenant, error) {
	_ = ctx
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	now := s.now()
	t := Tenant{
		ID:                     NewID("tenant"),
		Name:                   name,
		Status:                 TenantStatusActive,
		DeleteProtected:        in.DeleteProtected,
		DeleteProtectionReason: strings.TrimSpace(in.DeleteProtectionReason),
		CreatedAt:              now,
		UpdatedAt:              now,
	}
	if err := s.store.SaveTenant(t); err != nil {
		return nil, err
	}
	if err := secureMkdirAll(filepath.Join(s.dataRoot, "tenants", slugID(t.ID))); err != nil {
		return nil, err
	}
	_ = s.recordAudit(auditRecord{TenantID: t.ID, Action: "tenant.created", ResourceType: "tenant", ResourceID: t.ID, ActorType: "admin"})
	return &t, nil
}

func (s *Service) ListTenants(ctx context.Context, in ListTenantsInput) ([]Tenant, error) {
	_ = ctx
	items, err := s.store.ListTenants()
	if err != nil {
		return nil, err
	}
	status := strings.TrimSpace(string(in.Status))
	name := strings.ToLower(strings.TrimSpace(in.Name))
	if status == "" && name == "" {
		return items, nil
	}
	filtered := make([]Tenant, 0, len(items))
	for _, item := range items {
		if status != "" && string(item.Status) != status {
			continue
		}
		if name != "" && !strings.Contains(strings.ToLower(item.Name), name) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered, nil
}

func (s *Service) GetTenant(ctx context.Context, tenantID string) (*Tenant, error) {
	_ = ctx
	t, err := s.store.GetTenant(tenantID)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *Service) UpdateTenant(ctx context.Context, tenantID string, in UpdateTenantInput) (*Tenant, error) {
	_ = ctx
	t, err := s.store.GetTenant(tenantID)
	if err != nil {
		return nil, err
	}
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			return nil, fmt.Errorf("name is required")
		}
		t.Name = name
	}
	if in.Status != nil {
		if !validTenantStatus(*in.Status) {
			return nil, fmt.Errorf("invalid tenant status")
		}
		t.Status = *in.Status
	}
	if in.DeleteProtected != nil {
		t.DeleteProtected = *in.DeleteProtected
		if !t.DeleteProtected {
			t.DeleteProtectionReason = ""
		}
	}
	if in.DeleteProtectionReason != nil {
		t.DeleteProtectionReason = strings.TrimSpace(*in.DeleteProtectionReason)
	}
	if err := applyQuotaUpdate(&t.Quota, in.MaxInstances, in.MaxSessions, in.MaxMessages, in.MaxRuns); err != nil {
		return nil, err
	}
	t.UpdatedAt = s.now()
	if err := s.store.SaveTenant(t); err != nil {
		return nil, err
	}
	_ = s.recordAudit(auditRecord{TenantID: t.ID, Action: "tenant.updated", ResourceType: "tenant", ResourceID: t.ID, ActorType: "admin", Metadata: map[string]string{"status": string(t.Status)}})
	return &t, nil
}

func (s *Service) DeleteTenant(ctx context.Context, tenantID string) error {
	check, err := s.GetTenantDeleteCheck(ctx, tenantID)
	if err != nil {
		return err
	}
	if !check.CanDelete {
		if check.DeleteProtected || hasDeleteProtectionBlocker(check.Blockers) {
			return ErrDeleteProtected
		}
		return ErrTenantBusy
	}
	// Tenant deletion bypasses DeleteUser, so clear each scoped declaration
	// explicitly before removing the tenant. This preserves the same
	// fail-closed re-creation invariant as the user lifecycle path.
	users, err := s.store.ListUsers(tenantID)
	if err != nil {
		return err
	}
	for _, user := range users {
		if err := s.dynamicCapabilities.ClearPrincipal(Principal{TenantID: tenantID, UserID: user.ID}); err != nil {
			return fmt.Errorf("revoke dynamic capability contracts: %w", err)
		}
	}
	if err := s.store.DeleteTenant(tenantID); err != nil {
		return err
	}
	if err := secureRemoveAllWithin(filepath.Join(s.dataRoot, "tenants"), filepath.Join(s.dataRoot, "tenants", slugID(tenantID))); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_ = s.recordAudit(auditRecord{TenantID: tenantID, Action: "tenant.deleted", ResourceType: "tenant", ResourceID: tenantID, ActorType: "admin"})
	return nil
}

func (s *Service) GetTenantDeleteCheck(ctx context.Context, tenantID string) (*TenantDeleteCheck, error) {
	_ = ctx
	if _, err := s.store.GetTenant(tenantID); err != nil {
		return nil, err
	}
	users, err := s.store.ListUsers(tenantID)
	if err != nil {
		return nil, err
	}
	check := &TenantDeleteCheck{
		TenantID:               tenantID,
		CanDelete:              true,
		DeleteProtected:        false,
		DeleteProtectionReason: "",
		GeneratedAt:            s.now().UTC(),
	}
	tenant, err := s.store.GetTenant(tenantID)
	if err != nil {
		return nil, err
	}
	if tenant.DeleteProtected {
		check.CanDelete = false
		check.DeleteProtected = true
		check.DeleteProtectionReason = tenant.DeleteProtectionReason
		check.Blockers = append(check.Blockers, DeleteBlocker{Kind: "delete_protected", TenantID: tenantID, Reason: deleteProtectionReason("tenant", tenant.DeleteProtectionReason)})
	}
	for _, user := range users {
		check.Users++
		credentials, err := s.store.ListCredentials(tenantID, user.ID)
		if err != nil {
			return nil, err
		}
		check.Credentials += len(credentials)
		usage, err := s.buildUsageSummary(tenantID, user.ID)
		if err != nil {
			return nil, err
		}
		check.Instances += usage.Instances
		check.Sessions += usage.Sessions
		check.Messages += usage.Messages
		check.Runs += usage.Runs
		if user.DeleteProtected {
			check.CanDelete = false
			check.Blockers = append(check.Blockers, DeleteBlocker{Kind: "delete_protected", TenantID: tenantID, UserID: user.ID, Reason: deleteProtectionReason("user", user.DeleteProtectionReason)})
		}
		blockers, err := s.collectRunningRunBlockers(tenantID, user.ID)
		if err != nil {
			return nil, err
		}
		if len(blockers) > 0 {
			check.CanDelete = false
			check.Blockers = append(check.Blockers, blockers...)
		}
	}
	return check, nil
}

// EnsurePrincipal creates tenant/user rows (and empty user config) when missing.
// IDs are taken from p (stable Hub/viewer identity), not auto-generated.
// Idempotent and safe for concurrent first-touch mobile/agent sessions.
func (s *Service) EnsurePrincipal(ctx context.Context, p Principal, email, displayName string) error {
	_ = ctx
	tenantID := strings.TrimSpace(p.TenantID)
	userID := strings.TrimSpace(p.UserID)
	if tenantID == "" || userID == "" {
		return fmt.Errorf("tenant_id and user_id are required")
	}
	now := s.now()
	if _, err := s.store.GetTenant(tenantID); err != nil {
		t := Tenant{
			ID:        tenantID,
			Name:      firstNonEmptyString(displayName, tenantID),
			Status:    TenantStatusActive,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := s.store.SaveTenant(t); err != nil {
			return err
		}
		_ = secureMkdirAll(filepath.Join(s.dataRoot, "tenants", slugID(t.ID)))
	}
	if _, err := s.store.GetUser(tenantID, userID); err != nil {
		name := strings.TrimSpace(displayName)
		if name == "" {
			name = firstNonEmptyString(strings.TrimSpace(email), userID)
		}
		u := User{
			ID:        userID,
			TenantID:  tenantID,
			Name:      name,
			Email:     strings.TrimSpace(email),
			Status:    UserStatusActive,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := s.store.SaveUser(u); err != nil {
			return err
		}
		if err := secureMkdirAll(s.userDataRoot(tenantID, userID)); err != nil {
			return err
		}
		if err := secureMkdirAll(filepath.Join(s.userRoot(tenantID, userID), "instances")); err != nil {
			return err
		}
		defaultCfg := UserConfig{TenantID: tenantID, UserID: userID, AppConfig: corelib.AppConfig{}, UpdatedAt: now}
		if err := s.store.SaveUserConfig(defaultCfg); err != nil {
			return err
		}
		if err := saveUserConfigToFile(s.userConfigPath(tenantID, userID), defaultCfg); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) CreateUser(ctx context.Context, in CreateUserInput) (*User, error) {
	if _, err := s.store.GetTenant(in.TenantID); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	now := s.now()
	u := User{
		ID:                     NewID("user"),
		TenantID:               in.TenantID,
		Name:                   name,
		Email:                  strings.TrimSpace(in.Email),
		Status:                 UserStatusActive,
		DeleteProtected:        in.DeleteProtected,
		DeleteProtectionReason: strings.TrimSpace(in.DeleteProtectionReason),
		CreatedAt:              now,
		UpdatedAt:              now,
	}
	if err := s.store.SaveUser(u); err != nil {
		return nil, err
	}
	if err := secureMkdirAll(s.userDataRoot(in.TenantID, u.ID)); err != nil {
		return nil, err
	}
	if err := secureMkdirAll(filepath.Join(s.userRoot(in.TenantID, u.ID), "instances")); err != nil {
		return nil, err
	}
	defaultCfg := UserConfig{TenantID: in.TenantID, UserID: u.ID, AppConfig: corelib.AppConfig{}, UpdatedAt: s.now()}
	if err := s.store.SaveUserConfig(defaultCfg); err != nil {
		return nil, err
	}
	if err := saveUserConfigToFile(s.userConfigPath(in.TenantID, u.ID), defaultCfg); err != nil {
		return nil, err
	}
	_ = s.recordAudit(auditRecord{TenantID: u.TenantID, UserID: u.ID, Action: "user.created", ResourceType: "user", ResourceID: u.ID, ActorType: "admin"})
	return &u, nil
}

func (s *Service) ListUsers(ctx context.Context, tenantID string, in ListUsersAdminInput) ([]User, error) {
	_ = ctx
	if _, err := s.store.GetTenant(tenantID); err != nil {
		return nil, err
	}
	items, err := s.store.ListUsers(tenantID)
	if err != nil {
		return nil, err
	}
	status := strings.TrimSpace(string(in.Status))
	name := strings.ToLower(strings.TrimSpace(in.Name))
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if status == "" && name == "" && email == "" {
		return items, nil
	}
	filtered := make([]User, 0, len(items))
	for _, item := range items {
		if status != "" && string(item.Status) != status {
			continue
		}
		if name != "" && !strings.Contains(strings.ToLower(item.Name), name) {
			continue
		}
		if email != "" && !strings.Contains(strings.ToLower(item.Email), email) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered, nil
}

func (s *Service) ListAllUsers(ctx context.Context, in ListAllUsersAdminInput) ([]User, error) {
	_ = ctx
	tenantID := strings.TrimSpace(in.TenantID)
	if tenantID != "" {
		return s.ListUsers(ctx, tenantID, ListUsersAdminInput{Status: in.Status, Name: in.Name, Email: in.Email})
	}
	tenants, err := s.store.ListTenants()
	if err != nil {
		return nil, err
	}
	all := make([]User, 0)
	for _, tenant := range tenants {
		users, err := s.store.ListUsers(tenant.ID)
		if err != nil {
			return nil, err
		}
		all = append(all, users...)
	}
	status := strings.TrimSpace(string(in.Status))
	name := strings.ToLower(strings.TrimSpace(in.Name))
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if status == "" && name == "" && email == "" {
		return all, nil
	}
	filtered := make([]User, 0, len(all))
	for _, item := range all {
		if status != "" && string(item.Status) != status {
			continue
		}
		if name != "" && !strings.Contains(strings.ToLower(item.Name), name) {
			continue
		}
		if email != "" && !strings.Contains(strings.ToLower(item.Email), email) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered, nil
}

func (s *Service) GetUser(ctx context.Context, tenantID, userID string) (*User, error) {
	_ = ctx
	u, err := s.store.GetUser(tenantID, userID)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *Service) UpdateUser(ctx context.Context, tenantID, userID string, in UpdateUserInput) (*User, error) {
	_ = ctx
	if _, err := s.store.GetTenant(tenantID); err != nil {
		return nil, err
	}
	u, err := s.store.GetUser(tenantID, userID)
	if err != nil {
		return nil, err
	}
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			return nil, fmt.Errorf("name is required")
		}
		u.Name = name
	}
	if in.Email != nil {
		u.Email = strings.TrimSpace(*in.Email)
	}
	if in.Status != nil {
		if !validUserStatus(*in.Status) {
			return nil, fmt.Errorf("invalid user status")
		}
		u.Status = *in.Status
	}
	if in.DeleteProtected != nil {
		u.DeleteProtected = *in.DeleteProtected
		if !u.DeleteProtected {
			u.DeleteProtectionReason = ""
		}
	}
	if in.DeleteProtectionReason != nil {
		u.DeleteProtectionReason = strings.TrimSpace(*in.DeleteProtectionReason)
	}
	if err := applyQuotaUpdate(&u.Quota, in.MaxInstances, in.MaxSessions, in.MaxMessages, in.MaxRuns); err != nil {
		return nil, err
	}
	u.UpdatedAt = s.now()
	if err := s.store.SaveUser(u); err != nil {
		return nil, err
	}
	_ = s.recordAudit(auditRecord{TenantID: u.TenantID, UserID: u.ID, Action: "user.updated", ResourceType: "user", ResourceID: u.ID, ActorType: "admin", Metadata: map[string]string{"status": string(u.Status)}})
	return &u, nil
}

func (s *Service) DeleteUser(ctx context.Context, tenantID, userID string) error {
	check, err := s.GetUserDeleteCheck(ctx, tenantID, userID)
	if err != nil {
		return err
	}
	if !check.CanDelete {
		if check.DeleteProtected || hasDeleteProtectionBlocker(check.Blockers) {
			return ErrDeleteProtected
		}
		return ErrUserBusy
	}
	// Remove durable control-plane declarations before the principal record and
	// its filesystem scope disappear. A later recreated user with the same IDs
	// must never inherit the deleted principal's dynamic authority.
	if err := s.dynamicCapabilities.ClearPrincipal(Principal{TenantID: tenantID, UserID: userID}); err != nil {
		return fmt.Errorf("revoke dynamic capability contracts: %w", err)
	}
	if err := s.store.DeleteUser(tenantID, userID); err != nil {
		return err
	}
	if err := secureRemoveAllWithin(filepath.Join(s.dataRoot, "tenants", slugID(tenantID), "users"), s.userRoot(tenantID, userID)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_ = s.recordAudit(auditRecord{TenantID: tenantID, UserID: userID, Action: "user.deleted", ResourceType: "user", ResourceID: userID, ActorType: "admin"})
	return nil
}

// PurgeUserData force-removes all user-owned agent runtime data. Unlike
// DeleteUser, it is deliberately idempotent and does not retain runtime audit
// events: it is called by a host account-unbind flow, not by normal agent
// administration. The host's immutable approval audit remains outside this
// service and is therefore unaffected.
func (s *Service) PurgeUserData(ctx context.Context, tenantID, userID string) error {
	_ = ctx
	tenantID = strings.TrimSpace(tenantID)
	userID = strings.TrimSpace(userID)
	if tenantID == "" || userID == "" {
		return fmt.Errorf("tenant_id and user_id are required")
	}
	if err := s.dynamicCapabilities.ClearPrincipal(Principal{TenantID: tenantID, UserID: userID}); err != nil {
		return fmt.Errorf("revoke dynamic capability contracts: %w", err)
	}

	instances, err := s.store.ListInstances(tenantID, userID)
	if err != nil && !errors.Is(err, ErrUserNotFound) {
		return err
	}
	for _, instance := range instances {
		runs, runErr := s.store.ListRuns(tenantID, userID, instance.ID)
		if runErr != nil {
			return runErr
		}
		for _, run := range runs {
			if cancel, ok := s.takeRunCancel(run.ID); ok {
				cancel()
			}
		}
	}
	if err := s.records.DeleteStructuredRecordsForUser(tenantID, userID); err != nil {
		return err
	}
	if _, err := s.store.DeleteAuditEvents(tenantID, userID); err != nil {
		return err
	}
	if err := s.store.DeleteUser(tenantID, userID); err != nil && !errors.Is(err, ErrUserNotFound) {
		return err
	}
	if err := secureRemoveAllWithin(filepath.Join(s.dataRoot, "tenants", slugID(tenantID), "users"), s.userRoot(tenantID, userID)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s *Service) GetUserDeleteCheck(ctx context.Context, tenantID, userID string) (*UserDeleteCheck, error) {
	_ = ctx
	if _, err := s.store.GetTenant(tenantID); err != nil {
		return nil, err
	}
	if _, err := s.store.GetUser(tenantID, userID); err != nil {
		return nil, err
	}
	credentials, err := s.store.ListCredentials(tenantID, userID)
	if err != nil {
		return nil, err
	}
	usage, err := s.buildUsageSummary(tenantID, userID)
	if err != nil {
		return nil, err
	}
	blockers, err := s.collectRunningRunBlockers(tenantID, userID)
	if err != nil {
		return nil, err
	}
	user, err := s.store.GetUser(tenantID, userID)
	if err != nil {
		return nil, err
	}
	if user.DeleteProtected {
		blockers = append([]DeleteBlocker{{Kind: "delete_protected", TenantID: tenantID, UserID: userID, Reason: deleteProtectionReason("user", user.DeleteProtectionReason)}}, blockers...)
	}
	check := &UserDeleteCheck{
		TenantID:               tenantID,
		UserID:                 userID,
		CanDelete:              len(blockers) == 0,
		DeleteProtected:        user.DeleteProtected,
		DeleteProtectionReason: user.DeleteProtectionReason,
		Credentials:            len(credentials),
		Instances:              usage.Instances,
		Sessions:               usage.Sessions,
		Messages:               usage.Messages,
		Runs:                   usage.Runs,
		Blockers:               blockers,
		GeneratedAt:            s.now().UTC(),
	}
	return check, nil
}

func (s *Service) GetTenantRetirePlan(ctx context.Context, tenantID string, in ExportServiceStateInput) (*TenantRetirePlan, error) {
	check, err := s.GetTenantDeleteCheck(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	in.TenantID = tenantID
	in.UserID = ""
	exported, err := s.ExportServiceState(ctx, in)
	if err != nil {
		return nil, err
	}
	return &TenantRetirePlan{DeleteCheck: *check, Export: *exported, GeneratedAt: s.now().UTC()}, nil
}

func hasDeleteProtectionBlocker(blockers []DeleteBlocker) bool {
	for _, blocker := range blockers {
		if blocker.Kind == "delete_protected" {
			return true
		}
	}
	return false
}

func deleteProtectionReason(scope, reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return scope + " is delete-protected"
	}
	return scope + " is delete-protected: " + reason
}

func (s *Service) GetUserRetirePlan(ctx context.Context, tenantID, userID string, in ExportServiceStateInput) (*UserRetirePlan, error) {
	check, err := s.GetUserDeleteCheck(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	in.TenantID = tenantID
	in.UserID = userID
	exported, err := s.ExportServiceState(ctx, in)
	if err != nil {
		return nil, err
	}
	return &UserRetirePlan{DeleteCheck: *check, Export: *exported, GeneratedAt: s.now().UTC()}, nil
}

func (s *Service) CreateCredential(ctx context.Context, in CreateCredentialInput) (*Credential, error) {
	_ = ctx
	if _, err := s.store.GetTenant(in.TenantID); err != nil {
		return nil, err
	}
	if _, err := s.store.GetUser(in.TenantID, in.UserID); err != nil {
		return nil, err
	}
	now := s.now()
	apiKey := strings.TrimSpace(in.APIKey)
	generatedKey := false
	if apiKey == "" {
		var err error
		apiKey, err = s.generateUniqueCredentialAPIKey()
		if err != nil {
			return nil, err
		}
		generatedKey = true
	} else if err := s.ensureCredentialAPIKeyAvailable(apiKey, ""); err != nil {
		return nil, err
	}
	apiSecret := strings.TrimSpace(in.APISecret)
	generatedSecret := false
	if apiSecret == "" {
		var err error
		apiSecret, err = generateCredentialAPISecret()
		if err != nil {
			return nil, fmt.Errorf("generate credential secret: %w", err)
		}
		generatedSecret = true
	}
	digest := HashSecretWithPepper(apiSecret, s.credentialPepper)
	if digest == "" {
		return nil, fmt.Errorf("failed to derive credential secret")
	}
	stored := Credential{
		ID:           NewID("cred"),
		TenantID:     in.TenantID,
		UserID:       in.UserID,
		Name:         strings.TrimSpace(in.Name),
		APIKeyPrefix: deriveAPIKeyPrefix(apiKey),
		APIKeyHash:   hashAPIKey(apiKey),
		Status:       CredentialStatusActive,
		TokenVersion: 1,
		SecretDigest: digest,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if in.ExpiresAt != nil {
		expiresAt := in.ExpiresAt.UTC()
		stored.ExpiresAt = &expiresAt
	}
	if err := s.store.SaveCredential(stored); err != nil {
		return nil, err
	}
	_ = s.recordAudit(auditRecord{TenantID: stored.TenantID, UserID: stored.UserID, Action: "credential.created", ResourceType: "credential", ResourceID: stored.ID, ActorType: "admin"})
	response := sanitizeCredential(stored)
	if generatedKey {
		response.APIKey = apiKey
	}
	if generatedSecret {
		response.APISecret = apiSecret
	}
	return &response, nil
}

func (s *Service) ListCredentials(ctx context.Context, tenantID, userID string) ([]Credential, error) {
	_ = ctx
	if _, err := s.store.GetTenant(tenantID); err != nil {
		return nil, err
	}
	if _, err := s.store.GetUser(tenantID, userID); err != nil {
		return nil, err
	}
	items, err := s.store.ListCredentials(tenantID, userID)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i] = sanitizeCredential(items[i])
	}
	return items, nil
}

func (s *Service) GetCredential(ctx context.Context, tenantID, userID, credentialID string) (*Credential, error) {
	_ = ctx
	if _, err := s.store.GetTenant(tenantID); err != nil {
		return nil, err
	}
	if _, err := s.store.GetUser(tenantID, userID); err != nil {
		return nil, err
	}
	cred, err := s.store.GetCredential(tenantID, userID, credentialID)
	if err != nil {
		return nil, err
	}
	cred = sanitizeCredential(cred)
	return &cred, nil
}

func (s *Service) UpdateCredential(ctx context.Context, tenantID, userID, credentialID string, in UpdateCredentialInput) (*Credential, error) {
	_ = ctx
	if _, err := s.store.GetTenant(tenantID); err != nil {
		return nil, err
	}
	if _, err := s.store.GetUser(tenantID, userID); err != nil {
		return nil, err
	}
	cred, err := s.store.GetCredential(tenantID, userID, credentialID)
	if err != nil {
		return nil, err
	}
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			return nil, fmt.Errorf("name is required")
		}
		cred.Name = name
	}
	if in.Status != nil {
		if !validCredentialStatus(*in.Status) {
			return nil, fmt.Errorf("invalid credential status")
		}
		if credentialStatus(cred) != *in.Status {
			cred.TokenVersion = credentialTokenVersion(cred) + 1
		}
		cred.Status = *in.Status
	}
	if in.ClearExpiresAt {
		cred.ExpiresAt = nil
	} else if in.ExpiresAt != nil {
		expiresAt := in.ExpiresAt.UTC()
		cred.ExpiresAt = &expiresAt
	}
	cred.UpdatedAt = s.now()
	if err := s.store.SaveCredential(cred); err != nil {
		return nil, err
	}
	_ = s.recordAudit(auditRecord{TenantID: cred.TenantID, UserID: cred.UserID, Action: "credential.updated", ResourceType: "credential", ResourceID: cred.ID, ActorType: "admin", Metadata: map[string]string{"status": string(credentialStatus(cred))}})
	cred = sanitizeCredential(cred)
	return &cred, nil
}

func (s *Service) RotateCredentialSecret(ctx context.Context, tenantID, userID, credentialID string, in RotateCredentialSecretInput) (*Credential, error) {
	_ = ctx
	if _, err := s.store.GetTenant(tenantID); err != nil {
		return nil, err
	}
	if _, err := s.store.GetUser(tenantID, userID); err != nil {
		return nil, err
	}
	apiSecret := strings.TrimSpace(in.APISecret)
	generatedSecret := false
	if apiSecret == "" {
		var err error
		apiSecret, err = generateCredentialAPISecret()
		if err != nil {
			return nil, fmt.Errorf("generate credential secret: %w", err)
		}
		generatedSecret = true
	}
	cred, err := s.store.GetCredential(tenantID, userID, credentialID)
	if err != nil {
		return nil, err
	}
	digest := HashSecretWithPepper(apiSecret, s.credentialPepper)
	if digest == "" {
		return nil, fmt.Errorf("failed to derive credential secret")
	}
	cred.SecretDigest = digest
	cred.TokenVersion = credentialTokenVersion(cred) + 1
	cred.UpdatedAt = s.now()
	if err := s.store.SaveCredential(cred); err != nil {
		return nil, err
	}
	_ = s.recordAudit(auditRecord{TenantID: cred.TenantID, UserID: cred.UserID, Action: "credential.secret_rotated", ResourceType: "credential", ResourceID: cred.ID, ActorType: "admin"})
	response := sanitizeCredential(cred)
	if generatedSecret {
		response.APISecret = apiSecret
	}
	return &response, nil
}

func (s *Service) RotateCredentialAPIKey(ctx context.Context, tenantID, userID, credentialID string, in RotateCredentialKeyInput) (*Credential, error) {
	_ = ctx
	if _, err := s.store.GetTenant(tenantID); err != nil {
		return nil, err
	}
	if _, err := s.store.GetUser(tenantID, userID); err != nil {
		return nil, err
	}
	apiKey := strings.TrimSpace(in.APIKey)
	generatedKey := false
	if apiKey == "" {
		var err error
		apiKey, err = s.generateUniqueCredentialAPIKey()
		if err != nil {
			return nil, err
		}
		generatedKey = true
	}
	cred, err := s.store.GetCredential(tenantID, userID, credentialID)
	if err != nil {
		return nil, err
	}
	if err := s.ensureCredentialAPIKeyAvailable(apiKey, cred.ID); err != nil {
		return nil, err
	}
	cred.APIKey = ""
	cred.APIKeyPrefix = deriveAPIKeyPrefix(apiKey)
	cred.APIKeyHash = hashAPIKey(apiKey)
	cred.TokenVersion = credentialTokenVersion(cred) + 1
	cred.UpdatedAt = s.now()
	if err := s.store.SaveCredential(cred); err != nil {
		return nil, err
	}
	_ = s.recordAudit(auditRecord{TenantID: cred.TenantID, UserID: cred.UserID, Action: "credential.key_rotated", ResourceType: "credential", ResourceID: cred.ID, ActorType: "admin"})
	response := sanitizeCredential(cred)
	if generatedKey {
		response.APIKey = apiKey
	}
	return &response, nil
}

func (s *Service) RevokeCredential(ctx context.Context, tenantID, userID, credentialID string) (*Credential, error) {
	_ = ctx
	if _, err := s.store.GetTenant(tenantID); err != nil {
		return nil, err
	}
	if _, err := s.store.GetUser(tenantID, userID); err != nil {
		return nil, err
	}
	cred, err := s.store.GetCredential(tenantID, userID, credentialID)
	if err != nil {
		return nil, err
	}
	cred.Status = CredentialStatusRevoked
	cred.TokenVersion = credentialTokenVersion(cred) + 1
	cred.UpdatedAt = s.now()
	if err := s.store.SaveCredential(cred); err != nil {
		return nil, err
	}
	_ = s.recordAudit(auditRecord{TenantID: cred.TenantID, UserID: cred.UserID, Action: "credential.revoked", ResourceType: "credential", ResourceID: cred.ID, ActorType: "admin"})
	cred = sanitizeCredential(cred)
	return &cred, nil
}

func (s *Service) IssueToken(ctx context.Context, in IssueTokenInput) (*IssueTokenOutput, error) {
	_ = ctx
	cred, err := s.store.GetCredentialByAPIKey(strings.TrimSpace(in.APIKey))
	if err != nil || !VerifySecretWithPepper(in.APISecret, cred.SecretDigest, s.credentialPepper) {
		return nil, ErrUnauthorized
	}
	if credentialStatus(cred) != CredentialStatusActive {
		return nil, ErrUnauthorized
	}
	if credentialExpired(cred, s.now()) {
		return nil, ErrUnauthorized
	}
	if err := s.ensurePrincipalActive(cred.TenantID, cred.UserID); err != nil {
		return nil, err
	}
	p := Principal{TenantID: cred.TenantID, UserID: cred.UserID, Roles: []string{"user"}}
	token, exp, err := s.tokens.IssueForCredential(p, cred.ID, credentialTokenVersion(cred))
	if err != nil {
		return nil, err
	}
	_ = s.recordAudit(auditRecord{TenantID: cred.TenantID, UserID: cred.UserID, Action: "auth.token_issued", ResourceType: "credential", ResourceID: cred.ID, ActorType: "credential"})
	return &IssueTokenOutput{AccessToken: token, TokenType: "Bearer", ExpiresAt: exp, Principal: p}, nil
}

func (s *Service) generateUniqueCredentialAPIKey() (string, error) {
	for i := 0; i < 8; i++ {
		apiKey, err := generateCredentialAPIKey()
		if err != nil {
			return "", fmt.Errorf("generate credential api key: %w", err)
		}
		err = s.ensureCredentialAPIKeyAvailable(apiKey, "")
		if err == nil {
			return apiKey, nil
		}
		if !errors.Is(err, ErrAlreadyExists) {
			return "", err
		}
	}
	return "", fmt.Errorf("generate credential api key: %w", ErrAlreadyExists)
}

func (s *Service) ensureCredentialAPIKeyAvailable(apiKey, currentCredentialID string) error {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return fmt.Errorf("api_key is required")
	}
	cred, err := s.store.GetCredentialByAPIKey(apiKey)
	if err == nil {
		if strings.TrimSpace(currentCredentialID) != "" && cred.ID == currentCredentialID {
			return nil
		}
		return fmt.Errorf("api_key already exists")
	}
	if !errors.Is(err, ErrCredentialNotFound) {
		return err
	}
	return nil
}

func (s *Service) RecordTokenAuthFailure(ctx context.Context, apiKey, remoteIP, reason string) error {
	_ = ctx
	return s.recordTokenAuthEvent(strings.TrimSpace(apiKey), strings.TrimSpace(remoteIP), strings.TrimSpace(reason), "auth.token_failed")
}

func (s *Service) RecordTokenRateLimit(ctx context.Context, apiKey, remoteIP string) error {
	_ = ctx
	return s.recordTokenAuthEvent(strings.TrimSpace(apiKey), strings.TrimSpace(remoteIP), "rate_limited", "auth.token_rate_limited")
}

func (s *Service) recordTokenAuthEvent(apiKey, remoteIP, reason, action string) error {
	rec := auditRecord{
		Action:       action,
		ResourceType: "credential",
		ActorType:    "anonymous",
		Metadata: map[string]string{
			"api_key_prefix": deriveAPIKeyPrefix(apiKey),
			"remote_ip":      remoteIP,
			"reason":         reason,
		},
	}
	if cred, err := s.store.GetCredentialByAPIKey(apiKey); err == nil {
		rec.TenantID = cred.TenantID
		rec.UserID = cred.UserID
		rec.ResourceID = cred.ID
		rec.ActorType = "credential"
	}
	return s.recordAudit(rec)
}

func (s *Service) Authenticate(accessToken string) (*Principal, error) {
	p, _, credentialID, credentialVersion, err := s.tokens.Parse(accessToken)
	if err != nil {
		return nil, ErrUnauthorized
	}
	if strings.TrimSpace(credentialID) != "" {
		cred, credErr := s.store.GetCredential(p.TenantID, p.UserID, credentialID)
		if credErr != nil || credentialStatus(cred) != CredentialStatusActive {
			return nil, ErrUnauthorized
		}
		if credentialExpired(cred, s.now()) {
			return nil, ErrUnauthorized
		}
		if credentialVersion > 0 && credentialTokenVersion(cred) != credentialVersion {
			return nil, ErrUnauthorized
		}
	}
	if err := s.ensurePrincipalActive(p.TenantID, p.UserID); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Service) RefreshToken(ctx context.Context, accessToken string, credentialSlidingTTL time.Duration) (*IssueTokenOutput, error) {
	_ = ctx
	p, _, credentialID, credentialVersion, err := s.tokens.Parse(strings.TrimSpace(accessToken))
	if err != nil {
		return nil, ErrUnauthorized
	}
	if strings.TrimSpace(credentialID) != "" {
		cred, credErr := s.store.GetCredential(p.TenantID, p.UserID, credentialID)
		if credErr != nil || credentialStatus(cred) != CredentialStatusActive {
			return nil, ErrUnauthorized
		}
		if credentialExpired(cred, s.now()) {
			return nil, ErrUnauthorized
		}
		if credentialVersion > 0 && credentialTokenVersion(cred) != credentialVersion {
			return nil, ErrUnauthorized
		}
		if err := s.ensurePrincipalActive(p.TenantID, p.UserID); err != nil {
			return nil, err
		}
		if credentialSlidingTTL > 0 && cred.ExpiresAt != nil {
			nextExpiry := s.now().Add(credentialSlidingTTL).UTC()
			if nextExpiry.After(cred.ExpiresAt.UTC()) {
				cred.ExpiresAt = &nextExpiry
				cred.UpdatedAt = s.now()
				if saveErr := s.store.SaveCredential(cred); saveErr != nil {
					return nil, saveErr
				}
			}
		}
		token, exp, issueErr := s.tokens.IssueForCredential(*p, cred.ID, credentialTokenVersion(cred))
		if issueErr != nil {
			return nil, issueErr
		}
		return &IssueTokenOutput{AccessToken: token, TokenType: "Bearer", ExpiresAt: exp, Principal: *p}, nil
	}
	if err := s.ensurePrincipalActive(p.TenantID, p.UserID); err != nil {
		return nil, err
	}
	token, exp, err := s.tokens.Issue(*p)
	if err != nil {
		return nil, err
	}
	return &IssueTokenOutput{AccessToken: token, TokenType: "Bearer", ExpiresAt: exp, Principal: *p}, nil
}

func (s *Service) GetMe(ctx context.Context, p Principal) (*User, error) {
	_ = ctx
	u, err := s.store.GetUser(p.TenantID, p.UserID)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func validTenantStatus(status TenantStatus) bool {
	return status == TenantStatusActive || status == TenantStatusDisabled
}

func validUserStatus(status UserStatus) bool {
	return status == UserStatusActive || status == UserStatusDisabled
}

func validCredentialStatus(status CredentialStatus) bool {
	return status == CredentialStatusActive || status == CredentialStatusSuspended || status == CredentialStatusRevoked
}

func credentialStatus(cred Credential) CredentialStatus {
	if cred.Status == "" {
		return CredentialStatusActive
	}
	return cred.Status
}

func credentialExpired(cred Credential, now time.Time) bool {
	if cred.ExpiresAt == nil {
		return false
	}
	return !cred.ExpiresAt.After(now)
}

func sanitizeCredential(cred Credential) Credential {
	cred.SecretDigest = ""
	cred.APISecret = ""
	cred.APIKeyHash = ""
	cred.APIKey = maskedAPIKey(cred)
	if cred.Status == "" {
		cred.Status = CredentialStatusActive
	}
	cred.TokenVersion = credentialTokenVersion(cred)
	return cred
}

func maskedAPIKey(cred Credential) string {
	v := strings.TrimSpace(cred.APIKey)
	if v != "" {
		return maskAPIKeyString(v)
	}
	prefix := strings.TrimSpace(cred.APIKeyPrefix)
	if prefix == "" {
		return ""
	}
	if len(prefix) <= 3 {
		return prefix + "***"
	}
	return prefix[:3] + "***"
}

func (s *Service) ensurePrincipalActive(tenantID, userID string) error {
	tenant, err := s.store.GetTenant(tenantID)
	if err != nil {
		return ErrUnauthorized
	}
	if tenant.Status != TenantStatusActive {
		return ErrForbidden
	}
	user, err := s.store.GetUser(tenantID, userID)
	if err != nil {
		return ErrUnauthorized
	}
	if user.Status != UserStatusActive {
		return ErrForbidden
	}
	return nil
}
