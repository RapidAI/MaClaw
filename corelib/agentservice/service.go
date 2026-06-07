package agentservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/memory"
	"github.com/RapidAI/CodeClaw/corelib/workflow"
)

type Config struct {
	DataRoot         string
	TokenSecret      string
	TokenTTL         time.Duration
	CredentialPepper string
}

type Service struct {
	store            Store
	records          RecordStore
	executor         Executor
	tokens           *TokenManager
	dataRoot         string
	credentialPepper string
	now              func() time.Time

	// SkillSourceFilter returns the allowed skill sources for a given principal.
	// nil means all sources are allowed. Set by MaClawSrv at initialization
	// to integrate with the skill source control system.
	SkillSourceFilter func(tenantID, userID string) []string

	runMu       sync.Mutex
	runningRuns map[string]context.CancelFunc
}

type auditRecord struct {
	TenantID      string
	UserID        string
	Action        string
	ResourceType  string
	ResourceID    string
	ActorType     string
	ActorTenantID string
	ActorUserID   string
	Metadata      map[string]string
}

func NewService(cfg Config, store Store, executor Executor) (*Service, error) {
	if strings.TrimSpace(cfg.DataRoot) == "" {
		return nil, fmt.Errorf("data root is required")
	}
	useFileStores := store == nil
	if store == nil {
		fileStore, err := NewFileStore(filepath.Join(cfg.DataRoot, "state", "store.json"))
		if err != nil {
			return nil, fmt.Errorf("create file store: %w", err)
		}
		store = fileStore
	}
	if executor == nil {
		executor = EchoExecutor{}
	}
	if err := secureMkdirAll(cfg.DataRoot); err != nil {
		return nil, fmt.Errorf("create data root: %w", err)
	}
	var records RecordStore
	if useFileStores {
		sqliteRecords, err := NewSQLiteRecordStore(filepath.Join(cfg.DataRoot, "records", "records.db"))
		if err != nil {
			return nil, fmt.Errorf("create record store: %w", err)
		}
		records = sqliteRecords
	} else {
		records = NewMemoryRecordStore()
	}
	return &Service{store: store, records: records, executor: executor, tokens: NewTokenManager(cfg.TokenSecret, cfg.TokenTTL), dataRoot: cfg.DataRoot, credentialPepper: cfg.CredentialPepper, now: time.Now}, nil
}

func (s *Service) DataRoot() string { return s.dataRoot }

func (s *Service) registerRunCancel(runID string, cancel context.CancelFunc) {
	if strings.TrimSpace(runID) == "" || cancel == nil {
		return
	}
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if s.runningRuns == nil {
		s.runningRuns = map[string]context.CancelFunc{}
	}
	s.runningRuns[runID] = cancel
}

func (s *Service) takeRunCancel(runID string) (context.CancelFunc, bool) {
	if strings.TrimSpace(runID) == "" {
		return nil, false
	}
	s.runMu.Lock()
	defer s.runMu.Unlock()
	cancel, ok := s.runningRuns[runID]
	if ok {
		delete(s.runningRuns, runID)
	}
	return cancel, ok
}

func (s *Service) clearRunCancel(runID string) {
	if strings.TrimSpace(runID) == "" {
		return
	}
	s.runMu.Lock()
	defer s.runMu.Unlock()
	delete(s.runningRuns, runID)
}

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
	if err := s.store.DeleteUser(tenantID, userID); err != nil {
		return err
	}
	if err := secureRemoveAllWithin(filepath.Join(s.dataRoot, "tenants", slugID(tenantID), "users"), s.userRoot(tenantID, userID)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_ = s.recordAudit(auditRecord{TenantID: tenantID, UserID: userID, Action: "user.deleted", ResourceType: "user", ResourceID: userID, ActorType: "admin"})
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

func (s *Service) GetParameterDefinitions(ctx context.Context, p Principal) ([]ParameterDefinition, error) {
	_ = ctx
	if _, err := s.store.GetUser(p.TenantID, p.UserID); err != nil {
		return nil, err
	}
	return DefaultParameterDefinitions(), nil
}

func (s *Service) GetUserConfig(ctx context.Context, p Principal) (*UserConfig, error) {
	_ = ctx
	if _, err := s.store.GetUser(p.TenantID, p.UserID); err != nil {
		return nil, err
	}
	cfg, err := s.getOrLoadUserConfig(p.TenantID, p.UserID)
	if err != nil {
		return nil, err
	}
	cfg.AppConfig = effectiveLLMFlatConfig(cfg.AppConfig)
	cfg.AppConfig = SanitizeAppConfig(cfg.AppConfig)
	return &cfg, nil
}

func (s *Service) GetRawUserConfig(ctx context.Context, p Principal) (*UserConfig, error) {
	_ = ctx
	if _, err := s.store.GetUser(p.TenantID, p.UserID); err != nil {
		return nil, err
	}
	cfg, err := s.getOrLoadRawUserConfig(p.TenantID, p.UserID)
	if err != nil {
		return nil, err
	}
	cfg.AppConfig = effectiveLLMFlatConfig(cfg.AppConfig)
	return &cfg, nil
}

type UserMemoryListInput struct {
	Category string
	Query    string
	Limit    int
	Offset   int
}

const (
	UserMemoryMaxContentRunes = 20000
	UserMemoryMaxTags         = 32
	UserMemoryMaxTagRunes     = 80
)

type UserMemoryListResult struct {
	Items          []UserMemoryView `json:"items"`
	Total          int              `json:"total"`
	Offset         int              `json:"offset"`
	Limit          int              `json:"limit"`
	HasMore        bool             `json:"has_more"`
	NextOffset     int              `json:"next_offset"`
	CategoryCounts map[string]int   `json:"category_counts"`
}

type UserMemorySaveInput struct {
	Content  string   `json:"content"`
	Category string   `json:"category"`
	Tags     []string `json:"tags"`
}

type UserMemoryView struct {
	ID          string          `json:"id"`
	Content     string          `json:"content"`
	Title       string          `json:"title,omitempty"`
	Category    memory.Category `json:"category"`
	Tags        []string        `json:"tags"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	AccessCount int             `json:"access_count"`
	SourceURL   string          `json:"source_url,omitempty"`
	SourceType  string          `json:"source_type,omitempty"`
	Stale       bool            `json:"stale,omitempty"`
	ReadOnly    bool            `json:"read_only"`
	Protected   bool            `json:"protected"`
}

func (s *Service) ListUserMemories(ctx context.Context, p Principal, in UserMemoryListInput) (*UserMemoryListResult, error) {
	_ = ctx
	if _, err := s.store.GetUser(p.TenantID, p.UserID); err != nil {
		return nil, err
	}
	store, err := s.openUserMemoryStore(p)
	if err != nil {
		return nil, err
	}
	defer store.Stop()
	category, err := normalizeUserMemoryCategory(in.Category, true)
	if err != nil {
		return nil, err
	}
	entries := store.List("", strings.TrimSpace(in.Query))
	ownerID := memoryOwnerIDForPrincipal(p)
	owned := make([]memory.Entry, 0, len(entries))
	categoryCounts := map[string]int{}
	for _, entry := range entries {
		if entry.OwnerID != "" && entry.OwnerID != ownerID {
			continue
		}
		owned = append(owned, entry)
		categoryCounts[string(memory.MapToCanonical(entry.Category))]++
	}
	filtered := owned
	if category != "" {
		filtered = make([]memory.Entry, 0, len(owned))
		for _, entry := range owned {
			if memory.MapToCanonical(entry.Category) == category {
				filtered = append(filtered, entry)
			}
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool { return filtered[i].UpdatedAt.After(filtered[j].UpdatedAt) })
	total := len(filtered)
	limit := in.Limit
	if limit <= 0 {
		limit = 100
	} else if limit > 200 {
		limit = 200
	}
	offset := in.Offset
	if offset < 0 {
		offset = 0
	}
	if offset > len(filtered) {
		offset = len(filtered)
	}
	end := offset + limit
	if end > len(filtered) {
		end = len(filtered)
	}
	return &UserMemoryListResult{Items: userMemoryViews(filtered[offset:end], ownerID), Total: total, Offset: offset, Limit: limit, HasMore: end < total, NextOffset: end, CategoryCounts: categoryCounts}, nil
}

func (s *Service) SaveUserMemory(ctx context.Context, p Principal, in UserMemorySaveInput) (*UserMemoryView, error) {
	_ = ctx
	if _, err := s.store.GetUser(p.TenantID, p.UserID); err != nil {
		return nil, err
	}
	content := strings.TrimSpace(in.Content)
	if content == "" {
		return nil, fmt.Errorf("memory content is required")
	}
	if utf8.RuneCountInString(content) > UserMemoryMaxContentRunes {
		return nil, fmt.Errorf("memory content is too long, max %d characters", UserMemoryMaxContentRunes)
	}
	category, err := normalizeUserMemoryCategory(in.Category, false)
	if err != nil {
		return nil, err
	}
	if category.IsProtected() {
		return nil, fmt.Errorf("protected memory cannot be created here")
	}
	store, err := s.openUserMemoryStore(p)
	if err != nil {
		return nil, err
	}
	defer store.Stop()
	tags, err := cleanMemoryTags(in.Tags)
	if err != nil {
		return nil, err
	}
	ownerID := memoryOwnerIDForPrincipal(p)
	entry := memory.Entry{ID: NewID("mem"), Content: content, Category: category, Tags: tags, OwnerID: ownerID, SourceType: "manual"}
	if err := store.UpsertEntriesByID([]memory.Entry{entry}); err != nil {
		return nil, err
	}
	if saved, ok := findMutableMemoryEntry(store, entry.ID, ownerID); ok {
		out := userMemoryView(saved, ownerID)
		return &out, nil
	}
	out := userMemoryView(entry, ownerID)
	return &out, nil
}

func (s *Service) UpdateUserMemory(ctx context.Context, p Principal, id string, in UserMemorySaveInput) (*UserMemoryView, error) {
	_ = ctx
	if _, err := s.store.GetUser(p.TenantID, p.UserID); err != nil {
		return nil, err
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("memory id is required")
	}
	content := strings.TrimSpace(in.Content)
	if content == "" {
		return nil, fmt.Errorf("memory content is required")
	}
	if utf8.RuneCountInString(content) > UserMemoryMaxContentRunes {
		return nil, fmt.Errorf("memory content is too long, max %d characters", UserMemoryMaxContentRunes)
	}
	category, err := normalizeUserMemoryCategory(in.Category, false)
	if err != nil {
		return nil, err
	}
	if category.IsProtected() {
		return nil, fmt.Errorf("protected memory cannot be edited here")
	}
	store, err := s.openUserMemoryStore(p)
	if err != nil {
		return nil, err
	}
	defer store.Stop()
	ownerID := memoryOwnerIDForPrincipal(p)
	if entry, ok := findMutableMemoryEntry(store, id, ownerID); !ok {
		return nil, ErrRecordNotFound
	} else if entry.Category.IsProtected() {
		return nil, fmt.Errorf("protected memory cannot be edited here")
	}
	tags, err := cleanMemoryTags(in.Tags)
	if err != nil {
		return nil, err
	}
	if err := store.Update(id, content, category, tags); err != nil {
		return nil, err
	}
	entry, _ := findMutableMemoryEntry(store, id, ownerID)
	out := userMemoryView(entry, ownerID)
	return &out, nil
}

func (s *Service) DeleteUserMemory(ctx context.Context, p Principal, id string) error {
	_ = ctx
	if _, err := s.store.GetUser(p.TenantID, p.UserID); err != nil {
		return err
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("memory id is required")
	}
	store, err := s.openUserMemoryStore(p)
	if err != nil {
		return err
	}
	defer store.Stop()
	if entry, ok := findMutableMemoryEntry(store, id, memoryOwnerIDForPrincipal(p)); !ok {
		return ErrRecordNotFound
	} else if entry.Category.IsProtected() {
		return fmt.Errorf("protected memory cannot be deleted here")
	}
	return store.Delete(id)
}

func (s *Service) openUserMemoryStore(p Principal) (*memory.Store, error) {
	dataDir := s.userDataRoot(p.TenantID, p.UserID)
	return memory.OpenDataDirStore(dataDir, memory.StoreModeAuto, filepath.Join(dataDir, "agent_memory.json"))
}

func findMutableMemoryEntry(store *memory.Store, id, ownerID string) (memory.Entry, bool) {
	for _, entry := range store.List("", "") {
		if entry.ID == id && entry.OwnerID == ownerID {
			return entry, true
		}
	}
	return memory.Entry{}, false
}

func userMemoryViews(entries []memory.Entry, ownerID string) []UserMemoryView {
	out := make([]UserMemoryView, 0, len(entries))
	for _, entry := range entries {
		out = append(out, userMemoryView(entry, ownerID))
	}
	return out
}

func userMemoryView(entry memory.Entry, ownerID string) UserMemoryView {
	protected := entry.Category.IsProtected()
	readOnly := protected || entry.OwnerID == "" || entry.OwnerID != ownerID
	return UserMemoryView{
		ID:          entry.ID,
		Content:     entry.Content,
		Title:       entry.Title,
		Category:    memory.MapToCanonical(entry.Category),
		Tags:        append([]string(nil), entry.Tags...),
		CreatedAt:   entry.CreatedAt,
		UpdatedAt:   entry.UpdatedAt,
		AccessCount: entry.AccessCount,
		SourceURL:   entry.SourceURL,
		SourceType:  entry.SourceType,
		Stale:       entry.Stale,
		ReadOnly:    readOnly,
		Protected:   protected,
	}
}

func cleanMemoryTags(tags []string) ([]string, error) {
	out := make([]string, 0, len(tags))
	seen := map[string]struct{}{}
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if utf8.RuneCountInString(tag) > UserMemoryMaxTagRunes {
			return nil, fmt.Errorf("memory tag is too long, max %d characters", UserMemoryMaxTagRunes)
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
		if len(out) > UserMemoryMaxTags {
			return nil, fmt.Errorf("too many memory tags, max %d", UserMemoryMaxTags)
		}
	}
	return out, nil
}

func normalizeUserMemoryCategory(value string, allowEmpty bool) (memory.Category, error) {
	category := memory.MapToCanonical(memory.Category(strings.TrimSpace(value)))
	if category == "" {
		if allowEmpty {
			return "", nil
		}
		return memory.CategoryUserFact, nil
	}
	switch category {
	case memory.CategorySelfIdentity, memory.CategoryUserFact, memory.CategoryPreference, memory.CategoryProjectKnowledge, memory.CategoryInstruction, memory.CategoryConversationSummary, memory.CategorySessionCheckpoint, memory.CategoryTaskArtifact, memory.CategoryProfile:
		return category, nil
	default:
		return "", fmt.Errorf("unsupported memory category %q", value)
	}
}

func (s *Service) UpdateUserConfig(ctx context.Context, p Principal, next corelib.AppConfig) (*UserConfig, error) {
	_ = ctx
	if _, err := s.store.GetUser(p.TenantID, p.UserID); err != nil {
		return nil, err
	}
	current, err := s.getOrLoadRawUserConfig(p.TenantID, p.UserID)
	if err != nil && err != ErrUserConfigNotFound {
		return nil, err
	}
	merged := normalizeLLMConfigForSave(current.AppConfig, mergeSecretPreserving(current.AppConfig, next))
	cfg := UserConfig{TenantID: p.TenantID, UserID: p.UserID, AppConfig: merged, UpdatedAt: s.now()}
	if err := s.store.SaveUserConfig(cfg); err != nil {
		return nil, err
	}
	if err := saveUserConfigToFile(s.userConfigPath(p.TenantID, p.UserID), cfg); err != nil {
		return nil, err
	}
	if err := s.refreshUserInstanceReadinessCache(p.TenantID, p.UserID, ValidateAppConfig(cfg.AppConfig), cfg.UpdatedAt); err != nil {
		return nil, err
	}
	_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "config.updated", ResourceType: "user_config", ResourceID: p.UserID, ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID})
	cfg.AppConfig = effectiveLLMFlatConfig(cfg.AppConfig)
	cfg.AppConfig = SanitizeAppConfig(cfg.AppConfig)
	return &cfg, nil
}

func (s *Service) defaultClientConfigPath() string {
	return filepath.Join(s.dataRoot, "config", "default_client_config.json")
}

func (s *Service) GetDefaultClientConfig(ctx context.Context) (*UserConfig, error) {
	_ = ctx
	cfg, err := loadUserConfigFromFile(s.defaultClientConfigPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &UserConfig{TenantID: "", UserID: "", AppConfig: corelib.AppConfig{}, UpdatedAt: time.Time{}}, nil
		}
		return nil, err
	}
	cfg.TenantID = ""
	cfg.UserID = ""
	cfg.AppConfig = SharedClientAppConfigOnly(cfg.AppConfig)
	return &cfg, nil
}

func (s *Service) UpdateDefaultClientConfig(ctx context.Context, next corelib.AppConfig) (*UserConfig, error) {
	_ = ctx
	current, err := s.GetDefaultClientConfig(ctx)
	if err != nil {
		return nil, err
	}
	currentShared := SharedClientAppConfigOnly(current.AppConfig)
	nextShared := SharedClientAppConfigOnly(next)
	merged := mergeSecretPreserving(currentShared, nextShared)
	cfg := UserConfig{TenantID: "", UserID: "", AppConfig: merged, UpdatedAt: s.now()}
	if err := saveUserConfigToFile(s.defaultClientConfigPath(), cfg); err != nil {
		return nil, err
	}
	if err := s.refreshAllUserInstanceReadinessCache(); err != nil {
		return nil, err
	}
	_ = s.recordAudit(auditRecord{Action: "config.default_client_updated", ResourceType: "default_client_config", ResourceID: "global", ActorType: "admin"})
	cfg.AppConfig = effectiveLLMFlatConfig(cfg.AppConfig)
	cfg.AppConfig = SanitizeAppConfig(cfg.AppConfig)
	return &cfg, nil
}

func (s *Service) refreshAllUserInstanceReadinessCache() error {
	tenants, err := s.store.ListTenants()
	if err != nil {
		return err
	}
	for _, tenant := range tenants {
		users, err := s.store.ListUsers(tenant.ID)
		if err != nil {
			return err
		}
		for _, user := range users {
			validation, err := s.currentInstanceConfigValidation(tenant.ID, user.ID)
			if err != nil {
				continue
			}
			if err := s.refreshUserInstanceReadinessCache(tenant.ID, user.ID, validation, s.now()); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) refreshUserInstanceReadinessCache(tenantID, userID string, validation ConfigValidationResult, updatedAt time.Time) error {
	items, err := s.store.ListInstances(tenantID, userID)
	if err != nil {
		return err
	}
	for _, inst := range items {
		inst.ConfigValidation = validation
		inst.Readiness = s.buildInstanceReadiness(inst)
		inst.Ready = inst.Readiness.Ready
		inst.ReadyReason = inst.Readiness.Reason
		inst.UpdatedAt = updatedAt
		if err := s.store.SaveInstance(inst); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ValidateUserConfig(ctx context.Context, p Principal) (*ConfigValidationResult, error) {
	return s.ValidateConfigCandidate(ctx, p, nil)
}

func (s *Service) ValidateConfigCandidate(ctx context.Context, p Principal, next *corelib.AppConfig) (*ConfigValidationResult, error) {
	_ = ctx
	candidate, err := s.resolveCandidateConfig(p, next)
	if err != nil {
		return nil, err
	}
	res := ValidateAppConfig(candidate)
	return &res, nil
}

func (s *Service) TestUserConfig(ctx context.Context, p Principal) (*ConfigTestResult, error) {
	return s.TestConfigCandidate(ctx, p, nil)
}

func (s *Service) TestConfigCandidate(ctx context.Context, p Principal, next *corelib.AppConfig) (*ConfigTestResult, error) {
	candidate, err := s.resolveCandidateConfig(p, next)
	if err != nil {
		return nil, err
	}
	return TestLLMConfig(ctx, candidate, nil), nil
}

func (s *Service) CreateInstance(ctx context.Context, p Principal, in CreateInstanceInput) (*Instance, error) {
	_ = ctx
	if _, err := s.store.GetTenant(p.TenantID); err != nil {
		return nil, err
	}
	if _, err := s.store.GetUser(p.TenantID, p.UserID); err != nil {
		return nil, err
	}
	if err := s.enforceQuotaLimit(p.TenantID, p.UserID, quotaMetricInstances); err != nil {
		return nil, err
	}
	validation, err := s.ValidateUserConfig(ctx, p)
	if err != nil {
		return nil, err
	}
	if !validation.Valid && !in.AllowInvalidConfig {
		return nil, ErrInvalidConfig
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	now := s.now()
	id := NewID("inst")
	root := filepath.Join(s.userRoot(p.TenantID, p.UserID), "instances", slugID(id))
	workspaceDir := filepath.Join(root, "workspace")
	if err := secureMkdirAll(workspaceDir); err != nil {
		return nil, err
	}
	inst := Instance{ID: id, TenantID: p.TenantID, UserID: p.UserID, Name: name, Description: strings.TrimSpace(in.Description), Metadata: cloneMap(in.Metadata), DataDir: s.userDataRoot(p.TenantID, p.UserID), RuntimeDir: root, Workspace: workspaceDir, Status: InstanceStatusReady, ConfigValidation: *validation, CreatedAt: now, UpdatedAt: now}
	inst = s.withInstanceReadiness(inst)
	if err := s.store.SaveInstance(inst); err != nil {
		return nil, err
	}
	if spec, err := s.GetInstanceBootstrap(ctx, p, id); err == nil {
		_ = writeBootstrap(filepath.Join(root, "bootstrap.json"), *spec)
	}
	_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "instance.created", ResourceType: "instance", ResourceID: inst.ID, ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID})
	return &inst, nil
}

func (s *Service) GetInstance(ctx context.Context, p Principal, instanceID string) (*Instance, error) {
	_ = ctx
	inst, err := s.store.GetInstance(p.TenantID, p.UserID, instanceID)
	if err != nil {
		return nil, err
	}
	inst = s.withInstanceReadiness(inst)
	return &inst, nil
}

func (s *Service) UpdateInstance(ctx context.Context, p Principal, instanceID string, in UpdateInstanceInput) (*Instance, error) {
	_ = ctx
	inst, err := s.store.GetInstance(p.TenantID, p.UserID, instanceID)
	if err != nil {
		return nil, err
	}
	changed := false
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			return nil, fmt.Errorf("name is required")
		}
		inst.Name = name
		changed = true
	}
	if in.Description != nil {
		inst.Description = strings.TrimSpace(*in.Description)
		changed = true
	}
	if in.Metadata != nil {
		inst.Metadata = cloneMap(in.Metadata)
		changed = true
	}
	if !changed {
		inst = s.withInstanceReadiness(inst)
		return &inst, nil
	}
	inst.UpdatedAt = s.now()
	inst = s.withInstanceReadiness(inst)
	if err := s.store.SaveInstance(inst); err != nil {
		return nil, err
	}
	_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "instance.updated", ResourceType: "instance", ResourceID: inst.ID, ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID})
	return &inst, nil
}

func (s *Service) DeleteInstance(ctx context.Context, p Principal, instanceID string) error {
	_ = ctx
	inst, err := s.store.GetInstance(p.TenantID, p.UserID, instanceID)
	if err != nil {
		return err
	}
	busy, err := s.hasRunningRuns(p.TenantID, p.UserID, instanceID, "")
	if err != nil {
		return err
	}
	if busy {
		return ErrInstanceBusy
	}
	if err := s.store.DeleteInstance(p.TenantID, p.UserID, instanceID); err != nil {
		return err
	}
	if err := secureRemoveAllWithin(filepath.Join(s.userRoot(p.TenantID, p.UserID), "instances"), inst.RuntimeDir); err != nil {
		return err
	}
	_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "instance.deleted", ResourceType: "instance", ResourceID: instanceID, ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID})
	return nil
}

func (s *Service) StopInstance(ctx context.Context, p Principal, instanceID string) (*Instance, error) {
	_ = ctx
	inst, err := s.store.GetInstance(p.TenantID, p.UserID, instanceID)
	if err != nil {
		return nil, err
	}
	inst.Status = InstanceStatusStopped
	inst.UpdatedAt = s.now()
	inst = s.withInstanceReadiness(inst)
	if err := s.store.SaveInstance(inst); err != nil {
		return nil, err
	}
	_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "instance.stopped", ResourceType: "instance", ResourceID: inst.ID, ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID})
	return &inst, nil
}

func (s *Service) ResumeInstance(ctx context.Context, p Principal, instanceID string) (*Instance, error) {
	inst, err := s.store.GetInstance(p.TenantID, p.UserID, instanceID)
	if err != nil {
		return nil, err
	}
	validation, err := s.ValidateUserConfig(ctx, p)
	if err != nil {
		return nil, err
	}
	inst.ConfigValidation = *validation
	if !validation.Valid {
		inst.Status = InstanceStatusStopped
		inst.UpdatedAt = s.now()
		inst = s.withInstanceReadiness(inst)
		_ = s.store.SaveInstance(inst)
		return &inst, ErrInvalidConfig
	}
	inst.Status = InstanceStatusReady
	inst.UpdatedAt = s.now()
	inst = s.withInstanceReadiness(inst)
	if err := s.store.SaveInstance(inst); err != nil {
		return nil, err
	}
	_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "instance.resumed", ResourceType: "instance", ResourceID: inst.ID, ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID})
	return &inst, nil
}

func (s *Service) RefreshInstanceReadiness(ctx context.Context, p Principal, instanceID string) (*Instance, error) {
	inst, err := s.store.GetInstance(p.TenantID, p.UserID, instanceID)
	if err != nil {
		return nil, err
	}
	validation, err := s.ValidateUserConfig(ctx, p)
	if err != nil {
		return nil, err
	}
	inst.ConfigValidation = *validation
	inst.UpdatedAt = s.now()
	inst = s.withInstanceReadiness(inst)
	if err := s.store.SaveInstance(inst); err != nil {
		return nil, err
	}
	return &inst, nil
}

func (s *Service) GetInstanceBootstrap(ctx context.Context, p Principal, instanceID string) (*InstanceBootstrap, error) {
	_ = ctx
	inst, err := s.store.GetInstance(p.TenantID, p.UserID, instanceID)
	if err != nil {
		return nil, err
	}
	spec := InstanceBootstrap{
		InstanceID:            inst.ID,
		TenantID:              inst.TenantID,
		UserID:                inst.UserID,
		DataDir:               inst.DataDir,
		RuntimeDir:            inst.RuntimeDir,
		WorkspaceDir:          inst.Workspace,
		ConversationStorePath: filepath.Join(inst.RuntimeDir, "conversation_memory.json"),
		ConfirmationStorePath: filepath.Join(inst.RuntimeDir, "ai_confirmations.json"),
		Metadata:              cloneMap(inst.Metadata),
		GeneratedAt:           s.now(),
	}
	return &spec, nil
}

func (s *Service) GetInstanceCapabilities(ctx context.Context, p Principal, instanceID string) (*AgentCapabilities, error) {
	inst, err := s.store.GetInstance(p.TenantID, p.UserID, instanceID)
	if err != nil {
		return nil, err
	}
	inst = s.withInstanceReadiness(inst)
	cfg, err := s.getOrLoadUserConfig(p.TenantID, p.UserID)
	if err != nil && err != ErrUserConfigNotFound {
		return nil, err
	}
	if describer, ok := s.executor.(CapabilityDescriber); ok {
		cfg.AppConfig = effectiveLLMFlatConfig(cfg.AppConfig)
		caps, err := describer.DescribeCapabilities(ctx, ExecuteRequest{
			Principal: p,
			Instance:  inst,
			DataDir:   inst.DataDir,
			Config:    cfg.AppConfig,
		})
		if err != nil {
			return nil, err
		}
		if caps != nil {
			return caps, nil
		}
	}
	return &AgentCapabilities{Executor: "unknown", SupportsSessions: true}, nil
}

func (s *Service) ListInstances(ctx context.Context, p Principal) ([]Instance, error) {
	_ = ctx
	items, err := s.store.ListInstances(p.TenantID, p.UserID)
	if err != nil {
		return nil, err
	}
	validation, validationErr := s.currentInstanceConfigValidation(p.TenantID, p.UserID)
	for i := range items {
		if validationErr == nil {
			items[i] = s.withInstanceReadinessValidation(items[i], validation)
		} else {
			items[i] = s.withInstanceReadiness(items[i])
		}
	}
	return items, nil
}

func (s *Service) CreateSession(ctx context.Context, p Principal, instanceID string, in CreateSessionInput) (*Session, error) {
	_ = ctx
	if _, err := s.store.GetInstance(p.TenantID, p.UserID, instanceID); err != nil {
		return nil, err
	}
	if err := s.enforceQuotaLimit(p.TenantID, p.UserID, quotaMetricSessions); err != nil {
		return nil, err
	}
	now := s.now()
	agentID := strings.TrimSpace(in.AgentID)
	if agentID == "" {
		agentID = "default"
	}
	sess := Session{ID: NewID("sess"), TenantID: p.TenantID, UserID: p.UserID, InstanceID: instanceID, AgentID: agentID, Title: strings.TrimSpace(in.Title), Metadata: cloneMap(in.Metadata), CreatedAt: now, UpdatedAt: now}
	if err := s.store.SaveSession(sess); err != nil {
		return nil, err
	}
	_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "session.created", ResourceType: "session", ResourceID: sess.ID, ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID, Metadata: map[string]string{"instance_id": instanceID}})
	enriched, err := s.enrichSession(sess)
	if err != nil {
		return nil, err
	}
	return &enriched, nil
}

func (s *Service) ListSessions(ctx context.Context, p Principal, instanceID string, in ListSessionsInput) ([]Session, error) {
	_ = ctx
	if _, err := s.store.GetInstance(p.TenantID, p.UserID, instanceID); err != nil {
		return nil, err
	}
	items, err := s.store.ListSessions(p.TenantID, p.UserID, instanceID)
	if err != nil {
		return nil, err
	}
	if !in.IncludeArchived {
		filtered := make([]Session, 0, len(items))
		for _, item := range items {
			if item.Archived {
				continue
			}
			filtered = append(filtered, item)
		}
		items = filtered
	}
	return s.enrichSessions(items)
}

func (s *Service) GetSession(ctx context.Context, p Principal, instanceID, sessionID string) (*Session, error) {
	_ = ctx
	v, err := s.store.GetSession(p.TenantID, p.UserID, instanceID, sessionID)
	if err != nil {
		return nil, err
	}
	enriched, err := s.enrichSession(v)
	if err != nil {
		return nil, err
	}
	return &enriched, nil
}

func (s *Service) UpdateSession(ctx context.Context, p Principal, instanceID, sessionID string, in UpdateSessionInput) (*Session, error) {
	_ = ctx
	sess, err := s.store.GetSession(p.TenantID, p.UserID, instanceID, sessionID)
	if err != nil {
		return nil, err
	}
	changed := false
	if in.Title != nil {
		sess.Title = strings.TrimSpace(*in.Title)
		changed = true
	}
	if in.Metadata != nil {
		sess.Metadata = cloneMap(in.Metadata)
		changed = true
	}
	if !changed {
		enriched, enrichErr := s.enrichSession(sess)
		if enrichErr != nil {
			return nil, enrichErr
		}
		return &enriched, nil
	}
	sess.UpdatedAt = s.now()
	if err := s.store.SaveSession(sess); err != nil {
		return nil, err
	}
	_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "session.updated", ResourceType: "session", ResourceID: sess.ID, ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID, Metadata: map[string]string{"instance_id": instanceID}})
	enriched, err := s.enrichSession(sess)
	if err != nil {
		return nil, err
	}
	return &enriched, nil
}

func (s *Service) ArchiveSession(ctx context.Context, p Principal, instanceID, sessionID string) (*Session, error) {
	_ = ctx
	sess, err := s.store.GetSession(p.TenantID, p.UserID, instanceID, sessionID)
	if err != nil {
		return nil, err
	}
	if sess.Archived {
		enriched, enrichErr := s.enrichSession(sess)
		if enrichErr != nil {
			return nil, enrichErr
		}
		return &enriched, nil
	}
	now := s.now()
	sess.Archived = true
	sess.ArchivedAt = &now
	sess.UpdatedAt = now
	if err := s.store.SaveSession(sess); err != nil {
		return nil, err
	}
	_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "session.archived", ResourceType: "session", ResourceID: sess.ID, ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID, Metadata: map[string]string{"instance_id": instanceID}})
	enriched, err := s.enrichSession(sess)
	if err != nil {
		return nil, err
	}
	return &enriched, nil
}

func (s *Service) RestoreSession(ctx context.Context, p Principal, instanceID, sessionID string) (*Session, error) {
	_ = ctx
	sess, err := s.store.GetSession(p.TenantID, p.UserID, instanceID, sessionID)
	if err != nil {
		return nil, err
	}
	if !sess.Archived {
		enriched, enrichErr := s.enrichSession(sess)
		if enrichErr != nil {
			return nil, enrichErr
		}
		return &enriched, nil
	}
	sess.Archived = false
	sess.ArchivedAt = nil
	sess.UpdatedAt = s.now()
	if err := s.store.SaveSession(sess); err != nil {
		return nil, err
	}
	_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "session.restored", ResourceType: "session", ResourceID: sess.ID, ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID, Metadata: map[string]string{"instance_id": instanceID}})
	enriched, err := s.enrichSession(sess)
	if err != nil {
		return nil, err
	}
	return &enriched, nil
}

func (s *Service) DeleteSession(ctx context.Context, p Principal, instanceID, sessionID string) error {
	_ = ctx
	if _, err := s.store.GetSession(p.TenantID, p.UserID, instanceID, sessionID); err != nil {
		return err
	}
	busy, err := s.hasRunningRuns(p.TenantID, p.UserID, instanceID, sessionID)
	if err != nil {
		return err
	}
	if busy {
		return ErrSessionBusy
	}
	if err := s.store.DeleteSession(p.TenantID, p.UserID, instanceID, sessionID); err != nil {
		return err
	}
	_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "session.deleted", ResourceType: "session", ResourceID: sessionID, ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID, Metadata: map[string]string{"instance_id": instanceID}})
	return nil
}

func (s *Service) ListMessages(ctx context.Context, p Principal, instanceID, sessionID string, in ListMessagesInput) ([]Message, error) {
	_ = ctx
	if _, err := s.store.GetSession(p.TenantID, p.UserID, instanceID, sessionID); err != nil {
		return nil, err
	}
	items, err := s.store.ListMessages(sessionID)
	if err != nil {
		return nil, err
	}
	role := in.Role
	filtered := make([]Message, 0, len(items))
	for _, item := range items {
		if role != "" && item.Role != role {
			continue
		}
		if in.Since != nil && item.CreatedAt.Before(*in.Since) {
			continue
		}
		if in.Until != nil && item.CreatedAt.After(*in.Until) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered, nil
}

func (s *Service) ListIMAuditMessages(ctx context.Context, p Principal, in ListIMAuditMessagesInput) ([]IMAuditMessage, error) {
	_ = ctx
	platform := normalizeIMAuditPlatform(in.Platform)
	contact := strings.ToLower(strings.TrimSpace(in.Contact))
	query := strings.ToLower(strings.TrimSpace(in.Query))
	role := in.Role
	instances, err := s.store.ListInstances(p.TenantID, p.UserID)
	if err != nil {
		return nil, err
	}
	out := make([]IMAuditMessage, 0)
	for _, inst := range instances {
		sessions, err := s.store.ListSessions(p.TenantID, p.UserID, inst.ID)
		if err != nil {
			return nil, err
		}
		for _, sess := range sessions {
			messages, err := s.store.ListMessages(sess.ID)
			if err != nil {
				return nil, err
			}
			for _, msg := range messages {
				if role != "" && msg.Role != role {
					continue
				}
				if in.Since != nil && msg.CreatedAt.Before(*in.Since) {
					continue
				}
				if in.Until != nil && msg.CreatedAt.After(*in.Until) {
					continue
				}
				msgPlatform := inferIMAuditPlatform(msg.Metadata, sess.Metadata)
				if msgPlatform == "" {
					continue
				}
				if platform != "" && msgPlatform != platform {
					continue
				}
				msgContact := inferIMAuditContact(msg.Metadata, sess.Metadata)
				if contact != "" && !strings.Contains(strings.ToLower(msgContact), contact) {
					continue
				}
				if query != "" && !imAuditMessageMatchesQuery(msg, sess, inst, msgPlatform, msgContact, query) {
					continue
				}
				out = append(out, IMAuditMessage{
					Message:      msg,
					SessionID:    sess.ID,
					SessionTitle: sess.Title,
					InstanceID:   inst.ID,
					InstanceName: inst.Name,
					Platform:     msgPlatform,
					ContactID:    msgContact,
					Metadata:     cloneMap(msg.Metadata),
					CreatedAt:    msg.CreatedAt,
				})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

func (s *Service) ListIMAuditContacts(ctx context.Context, p Principal, platform string) ([]IMAuditContact, error) {
	messages, err := s.ListIMAuditMessages(ctx, p, ListIMAuditMessagesInput{Platform: platform})
	if err != nil {
		return nil, err
	}
	byKey := make(map[string]*IMAuditContact)
	for _, item := range messages {
		if item.Platform == "" || item.ContactID == "" {
			continue
		}
		key := item.Platform + "\x00" + item.ContactID
		contact := byKey[key]
		if contact == nil {
			contact = &IMAuditContact{Platform: item.Platform, ContactID: item.ContactID}
			byKey[key] = contact
		}
		contact.MessageCount++
		if item.CreatedAt.After(contact.LastMessageAt) {
			contact.LastMessageAt = item.CreatedAt
		}
		if contact.DisplayName == "" {
			contact.DisplayName = strings.TrimSpace(item.Metadata["im_user_name"])
		}
	}
	out := make([]IMAuditContact, 0, len(byKey))
	for _, item := range byKey {
		out = append(out, *item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].LastMessageAt.Equal(out[j].LastMessageAt) {
			return out[i].LastMessageAt.After(out[j].LastMessageAt)
		}
		if out[i].Platform != out[j].Platform {
			return out[i].Platform < out[j].Platform
		}
		return out[i].ContactID < out[j].ContactID
	})
	return out, nil
}

func (s *Service) GetIMAuditStats(ctx context.Context, p Principal, in ListIMAuditMessagesInput) (*IMAuditStats, error) {
	messages, err := s.ListIMAuditMessages(ctx, p, in)
	if err != nil {
		return nil, err
	}
	stats := &IMAuditStats{ByPlatform: map[string]int{}}
	contacts := map[string]struct{}{}
	for _, item := range messages {
		stats.Messages++
		if item.Platform != "" {
			stats.ByPlatform[item.Platform]++
		}
		if item.Platform != "" && item.ContactID != "" {
			contacts[item.Platform+"\x00"+item.ContactID] = struct{}{}
		}
		if stats.LastMessageAt == nil || item.CreatedAt.After(*stats.LastMessageAt) {
			ts := item.CreatedAt
			stats.LastMessageAt = &ts
		}
	}
	stats.Contacts = len(contacts)
	stats.Platforms = len(stats.ByPlatform)
	return stats, nil
}

func (s *Service) DeleteIMAuditMessagesBefore(ctx context.Context, p Principal, in ListIMAuditMessagesInput, before time.Time) (*DeleteIMAuditMessagesOutput, error) {
	_ = ctx
	if before.IsZero() {
		return nil, fmt.Errorf("before is required")
	}
	platform := normalizeIMAuditPlatform(in.Platform)
	contact := strings.ToLower(strings.TrimSpace(in.Contact))
	query := strings.ToLower(strings.TrimSpace(in.Query))
	role := in.Role
	instances, err := s.store.ListInstances(p.TenantID, p.UserID)
	if err != nil {
		return nil, err
	}
	deleted := 0
	for _, inst := range instances {
		sessions, err := s.store.ListSessions(p.TenantID, p.UserID, inst.ID)
		if err != nil {
			return nil, err
		}
		for _, sess := range sessions {
			messages, err := s.store.ListMessages(sess.ID)
			if err != nil {
				return nil, err
			}
			ids := make([]string, 0)
			for _, msg := range messages {
				if !msg.CreatedAt.Before(before) {
					continue
				}
				if role != "" && msg.Role != role {
					continue
				}
				if in.Since != nil && msg.CreatedAt.Before(*in.Since) {
					continue
				}
				if in.Until != nil && msg.CreatedAt.After(*in.Until) {
					continue
				}
				msgPlatform := inferIMAuditPlatform(msg.Metadata, sess.Metadata)
				if msgPlatform == "" {
					continue
				}
				if platform != "" && msgPlatform != platform {
					continue
				}
				msgContact := inferIMAuditContact(msg.Metadata, sess.Metadata)
				if contact != "" && !strings.Contains(strings.ToLower(msgContact), contact) {
					continue
				}
				if query != "" && !imAuditMessageMatchesQuery(msg, sess, inst, msgPlatform, msgContact, query) {
					continue
				}
				ids = append(ids, msg.ID)
			}
			n, err := s.store.DeleteMessages(sess.ID, ids)
			if err != nil {
				return nil, err
			}
			deleted += n
		}
	}
	if deleted > 0 {
		meta := map[string]string{"before": before.Format(time.RFC3339Nano), "deleted": strconv.Itoa(deleted)}
		if platform != "" {
			meta["platform"] = platform
		}
		if contact != "" {
			meta["contact"] = contact
		}
		if query != "" {
			meta["query"] = query
		}
		if role != "" {
			meta["role"] = string(role)
		}
		if in.Since != nil {
			meta["since"] = in.Since.Format(time.RFC3339Nano)
		}
		if in.Until != nil {
			meta["until"] = in.Until.Format(time.RFC3339Nano)
		}
		_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "im_audit.messages_deleted", ResourceType: "im_audit", ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID, Metadata: meta})
	}
	return &DeleteIMAuditMessagesOutput{Deleted: deleted, Before: before}, nil
}

func IMMessageMetadata(in IMMessageMetadataInput) map[string]string {
	metadata := cloneMap(in.Extra)
	if metadata == nil {
		metadata = map[string]string{}
	}
	if platform := normalizeIMAuditPlatform(in.Platform); platform != "" {
		metadata["im_platform"] = platform
	}
	if contactID := strings.TrimSpace(in.ContactID); contactID != "" {
		metadata["contact_id"] = contactID
	}
	if messageID := strings.TrimSpace(in.MessageID); messageID != "" {
		metadata["im_message_id"] = messageID
	}
	if userName := strings.TrimSpace(in.UserName); userName != "" {
		metadata["im_user_name"] = userName
	}
	return metadata
}

func normalizeIMAuditPlatform(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "qq", "qqbot", "qq_bot":
		return "qq"
	case "wechat", "weixin", "wx":
		return "weixin"
	case "telegram", "tg":
		return "telegram"
	case "thirdparty", "third_party", "thirdparty_gateway":
		return "thirdparty"
	case "lansenger", "maclaw":
		return value
	default:
		return value
	}
}

func inferIMAuditPlatform(metas ...map[string]string) string {
	keys := []string{"im_platform", "platform", "channel", "source", "source_platform", "gateway"}
	for _, meta := range metas {
		for _, key := range keys {
			if value := normalizeIMAuditPlatform(metaValue(meta, key)); value != "" {
				return value
			}
		}
	}
	return ""
}

func inferIMAuditContact(metas ...map[string]string) string {
	keys := []string{"im_user_id", "contact_id", "external_user_id", "sender_id", "chat_id", "openid", "unionid", "conversation_id"}
	for _, meta := range metas {
		for _, key := range keys {
			if value := strings.TrimSpace(metaValue(meta, key)); value != "" {
				return value
			}
		}
	}
	return ""
}

func metaValue(meta map[string]string, key string) string {
	if meta == nil {
		return ""
	}
	return strings.TrimSpace(meta[key])
}

func imAuditMessageMatchesQuery(msg Message, sess Session, inst Instance, platform, contact, query string) bool {
	values := []string{msg.Content, string(msg.Role), msg.InputType, msg.OutputType, sess.Title, inst.Name, platform, contact}
	for k, v := range msg.Metadata {
		values = append(values, k, v)
	}
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), query) {
			return true
		}
	}
	return false
}

func (s *Service) SendMessage(ctx context.Context, p Principal, instanceID string, in SendMessageInput) (*Session, *Run, *Message, error) {
	sess, err := s.resolveSendSession(ctx, p, instanceID, in)
	if err != nil {
		return nil, nil, nil, err
	}
	clientMessageID := strings.TrimSpace(in.ClientMessageID)
	if clientMessageID != "" {
		if run, msg, ok := s.findExistingClientMessage(sess.ID, p, instanceID, clientMessageID); ok {
			return sess, &run, msg, nil
		}
	}
	metadata := cloneMap(in.Metadata)
	if clientMessageID != "" {
		if metadata == nil {
			metadata = map[string]string{}
		}
		metadata["client_message_id"] = clientMessageID
	}
	run, msg, err := s.PostMessage(ctx, p, instanceID, sess.ID, PostMessageInput{Content: in.Content, InputType: in.InputType, Metadata: metadata})
	if err != nil {
		return sess, run, msg, err
	}
	updatedSess, getErr := s.GetSession(ctx, p, instanceID, sess.ID)
	if getErr == nil {
		sess = updatedSess
	}
	return sess, run, msg, nil
}

func (s *Service) resolveSendSession(ctx context.Context, p Principal, instanceID string, in SendMessageInput) (*Session, error) {
	if strings.TrimSpace(in.SessionID) != "" {
		sess, err := s.GetSession(ctx, p, instanceID, in.SessionID)
		if err != nil {
			return nil, err
		}
		if sess.Archived {
			return nil, ErrSessionArchived
		}
		return sess, nil
	}
	clientSessionKey := strings.TrimSpace(in.ClientSessionKey)
	if clientSessionKey != "" {
		if sess, ok := s.findSessionByClientKey(p, instanceID, clientSessionKey); ok {
			if sess.Archived {
				return nil, ErrSessionArchived
			}
			return &sess, nil
		}
	}
	metadata := cloneMap(in.SessionMetadata)
	if clientSessionKey != "" {
		if metadata == nil {
			metadata = map[string]string{}
		}
		metadata["client_session_key"] = clientSessionKey
	}
	return s.CreateSession(ctx, p, instanceID, CreateSessionInput{AgentID: in.AgentID, Title: in.Title, Metadata: metadata})
}

func (s *Service) findSessionByClientKey(p Principal, instanceID, clientSessionKey string) (Session, bool) {
	items, err := s.store.ListSessions(p.TenantID, p.UserID, instanceID)
	if err != nil {
		return Session{}, false
	}
	for _, sess := range items {
		if sess.Metadata != nil && sess.Metadata["client_session_key"] == clientSessionKey {
			return sess, true
		}
	}
	return Session{}, false
}

func (s *Service) findExistingClientMessage(sessionID string, p Principal, instanceID, clientMessageID string) (Run, *Message, bool) {
	messages, err := s.store.ListMessages(sessionID)
	if err != nil {
		return Run{}, nil, false
	}
	var userMessageID string
	for _, msg := range messages {
		if msg.Role == MessageRoleUser && msg.Metadata != nil && msg.Metadata["client_message_id"] == clientMessageID {
			userMessageID = msg.ID
			break
		}
	}
	if userMessageID == "" {
		return Run{}, nil, false
	}
	run, err := s.store.GetRunByUserMessageID(p.TenantID, p.UserID, instanceID, userMessageID)
	if err != nil {
		return Run{}, nil, false
	}
	var assistant *Message
	if run.AssistantMessageID != "" {
		for _, msg := range messages {
			if msg.ID == run.AssistantMessageID {
				copy := msg
				assistant = &copy
				break
			}
		}
	}
	return run, assistant, true
}
func (s *Service) PostMessage(ctx context.Context, p Principal, instanceID, sessionID string, in PostMessageInput) (*Run, *Message, error) {
	tenant, err := s.store.GetTenant(p.TenantID)
	if err != nil {
		return nil, nil, err
	}
	user, err := s.store.GetUser(p.TenantID, p.UserID)
	if err != nil {
		return nil, nil, err
	}
	inst, err := s.store.GetInstance(p.TenantID, p.UserID, instanceID)
	if err != nil {
		return nil, nil, err
	}
	inst = s.withInstanceReadiness(inst)
	if !inst.Ready {
		return nil, nil, fmt.Errorf("instance is not ready: %s", inst.ReadyReason)
	}
	sess, err := s.store.GetSession(p.TenantID, p.UserID, instanceID, sessionID)
	if err != nil {
		return nil, nil, err
	}
	if sess.Archived {
		return nil, nil, ErrSessionArchived
	}
	cfg, err := s.getOrLoadUserConfig(p.TenantID, p.UserID)
	if err != nil && err != ErrUserConfigNotFound {
		return nil, nil, err
	}
	cfg.AppConfig = effectiveLLMFlatConfig(cfg.AppConfig)
	content := strings.TrimSpace(in.Content)
	if content == "" {
		return nil, nil, fmt.Errorf("content is required")
	}
	if err := s.enforceQuotaLimit(p.TenantID, p.UserID, quotaMetricMessages); err != nil {
		return nil, nil, err
	}
	if err := s.enforceQuotaLimit(p.TenantID, p.UserID, quotaMetricRuns); err != nil {
		return nil, nil, err
	}
	effectiveContent, pendingAskReq := buildEffectiveUserContent(sess, content)
	now := s.now()
	userMsg := Message{ID: NewID("msg"), SessionID: sess.ID, TenantID: p.TenantID, UserID: p.UserID, InstanceID: instanceID, Role: MessageRoleUser, InputType: defaultString(in.InputType, "text/plain"), Content: content, Metadata: cloneMap(in.Metadata), CreatedAt: now}
	if err := s.store.SaveMessage(userMsg); err != nil {
		return nil, nil, err
	}
	_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "message.posted", ResourceType: "message", ResourceID: userMsg.ID, ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID, Metadata: map[string]string{"instance_id": instanceID, "session_id": sessionID, "role": string(userMsg.Role)}})
	run := Run{ID: NewID("run"), TenantID: p.TenantID, UserID: p.UserID, InstanceID: instanceID, SessionID: sess.ID, UserMessageID: userMsg.ID, Status: RunStatusRunning, StartedAt: now}
	if err := s.store.SaveRun(run); err != nil {
		return nil, nil, err
	}
	_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "run.started", ResourceType: "run", ResourceID: run.ID, ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID, Metadata: map[string]string{"instance_id": instanceID, "session_id": sessionID}})
	history, histErr := s.store.ListMessages(sess.ID)
	if histErr != nil {
		return nil, nil, histErr
	}
	execMsg := userMsg
	execMsg.Content = effectiveContent
	execCtx, cancelExec := context.WithCancel(ctx)
	s.registerRunCancel(run.ID, cancelExec)
	res, execErr := s.executor.Execute(execCtx, ExecuteRequest{
		Principal:  p,
		Tenant:     tenant,
		User:       user,
		Instance:   inst,
		Session:    sess,
		Message:    execMsg,
		History:    history,
		DataDir:    inst.DataDir,
		Config:     cfg.AppConfig,
		ToolPolicy: toolPolicyFromMetadata(userMsg.Metadata, sess.Metadata),
		OpsApprovedCommands: opsApprovedCommandsFromMetadata(
			userMsg.Metadata,
			sess.Metadata,
		),
	})
	s.clearRunCancel(run.ID)
	cancelExec()
	completed := s.now()
	run.CompletedAt = &completed
	run.DurationMs = completed.Sub(run.StartedAt).Milliseconds()
	if execErr != nil {
		if errors.Is(execCtx.Err(), context.Canceled) || errors.Is(execErr, context.Canceled) {
			run.Status = RunStatusCancelled
			run.Error = "run cancelled"
			_ = s.store.SaveRun(run)
			_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "run.cancelled", ResourceType: "run", ResourceID: run.ID, ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID, Metadata: map[string]string{"instance_id": instanceID, "session_id": sessionID}})
			return &run, nil, execErr
		}
		run.Status = RunStatusFailed
		run.Error = execErr.Error()
		_ = s.store.SaveRun(run)
		_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "run.failed", ResourceType: "run", ResourceID: run.ID, ActorType: "system", ActorTenantID: p.TenantID, ActorUserID: p.UserID, Metadata: map[string]string{"instance_id": instanceID, "session_id": sessionID}})
		return &run, nil, execErr
	}
	assistant := Message{ID: NewID("msg"), SessionID: sess.ID, TenantID: p.TenantID, UserID: p.UserID, InstanceID: instanceID, Role: MessageRoleAssistant, OutputType: defaultString(res.OutputType, "text/plain"), Content: res.Content, Metadata: cloneMap(res.Metadata), CreatedAt: completed}
	if err := s.store.SaveMessage(assistant); err != nil {
		return nil, nil, err
	}
	run.Status = RunStatusSucceeded
	run.AssistantMessageID = assistant.ID
	run.ResponseSource = strings.TrimSpace(assistant.Metadata[metaResponseSource])
	run.WaitingForUser = normalizeResponseSourceKind(run.ResponseSource).IsWaitingForUser()
	if err := s.store.SaveRun(run); err != nil {
		return nil, nil, err
	}
	if pendingAskReq != nil {
		sess.Metadata = clearPendingAskUserMetadata(sess.Metadata)
	}
	if res != nil && res.Metadata != nil && normalizeResponseSourceKind(res.Metadata[metaResponseSource]).IsWaitingForUser() {
		sess.Metadata = setPendingAskUserMetadata(sess.Metadata, res.Metadata)
	}
	sess.UpdatedAt = completed
	_ = s.store.SaveSession(sess)
	_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "run.succeeded", ResourceType: "run", ResourceID: run.ID, ActorType: "system", ActorTenantID: p.TenantID, ActorUserID: p.UserID, Metadata: map[string]string{"instance_id": instanceID, "session_id": sessionID, "assistant_message_id": assistant.ID}})
	return &run, &assistant, nil
}

func toolPolicyFromMetadata(messageMetadata, sessionMetadata map[string]string) workflow.ToolFilterPolicy {
	for _, metadata := range []map[string]string{messageMetadata, sessionMetadata} {
		policy := workflow.ToolFilterPolicy(strings.TrimSpace(metadata["tool_policy"]))
		switch policy {
		case workflow.ToolFilterDocOnly, workflow.ToolFilterFull, workflow.ToolFilterOpsControlled:
			return policy
		}
	}
	return workflow.ToolFilterNone
}

func opsApprovedCommandsFromMetadata(messageMetadata, sessionMetadata map[string]string) []workflow.OpsApprovedCommand {
	policyText := strings.TrimSpace(messageMetadata["ops_approved_commands"])
	approvalSources := []map[string]string{messageMetadata}
	if policyText == "" {
		policyText = strings.TrimSpace(sessionMetadata["ops_approved_commands"])
		approvalSources = []map[string]string{messageMetadata, sessionMetadata}
	}
	if policyText == "" {
		return nil
	}
	decision := workflow.ExtractOpsRiskDecision(policyText)
	if decision == workflow.OpsRiskDecisionApprovalRequired && !opsExecutionApprovalSatisfies(approvalSources, workflow.ExtractOpsApprovalRequirement(policyText)) {
		return nil
	}
	if decision == workflow.OpsRiskDecisionApprovalRequired && !opsApprovalDigestMatches(approvalSources, policyText) {
		return nil
	}
	return workflow.ExtractOpsApprovedCommands(policyText)
}

func opsExecutionApprovalSatisfies(metadataSources []map[string]string, required workflow.OpsApprovalRequirement) bool {
	actual := opsExecutionApprovalLevelFromMetadata(metadataSources)
	if required == workflow.OpsApprovalRequirementSingle {
		return actual == workflow.OpsApprovalRequirementSingle || actual == workflow.OpsApprovalRequirementDouble
	}
	if required == workflow.OpsApprovalRequirementDouble {
		return actual == workflow.OpsApprovalRequirementDouble
	}
	return false
}

func opsExecutionApprovalLevelFromMetadata(metadataSources []map[string]string) workflow.OpsApprovalRequirement {
	for _, metadata := range metadataSources {
		level := strings.ToLower(strings.TrimSpace(metadata["ops_execution_approval_level"]))
		switch level {
		case string(workflow.OpsApprovalRequirementSingle):
			return workflow.OpsApprovalRequirementSingle
		case string(workflow.OpsApprovalRequirementDouble):
			return workflow.OpsApprovalRequirementDouble
		}
		value := strings.ToLower(strings.TrimSpace(metadata["ops_execution_approved"]))
		switch value {
		case "true", "yes", "approved", "1":
			return workflow.OpsApprovalRequirementSingle
		}
	}
	return workflow.OpsApprovalRequirementUnknown
}

func opsApprovalDigestMatches(metadataSources []map[string]string, policyText string) bool {
	for _, metadata := range metadataSources {
		expected := strings.TrimSpace(metadata["ops_approval_digest"])
		if expected == "" {
			continue
		}
		if strings.EqualFold(expected, workflow.OpsApprovalDigest(policyText)) {
			return true
		}
	}
	return false
}

func (s *Service) ListAuditEvents(ctx context.Context, in ListAuditEventsInput) ([]AuditEvent, error) {
	_ = ctx
	items, err := s.store.ListAuditEvents(strings.TrimSpace(in.TenantID), strings.TrimSpace(in.UserID))
	if err != nil {
		return nil, err
	}
	action := strings.TrimSpace(in.Action)
	resourceType := strings.TrimSpace(in.ResourceType)
	resourceID := strings.TrimSpace(in.ResourceID)
	actorType := strings.TrimSpace(in.ActorType)
	actorTenant := strings.TrimSpace(in.ActorTenant)
	actorUser := strings.TrimSpace(in.ActorUser)
	if action == "" && resourceType == "" && resourceID == "" && actorType == "" && actorTenant == "" && actorUser == "" && in.Since == nil && in.Until == nil {
		return items, nil
	}
	filtered := make([]AuditEvent, 0, len(items))
	for _, item := range items {
		if action != "" && item.Action != action {
			continue
		}
		if resourceType != "" && item.ResourceType != resourceType {
			continue
		}
		if resourceID != "" && item.ResourceID != resourceID {
			continue
		}
		if actorType != "" && item.ActorType != actorType {
			continue
		}
		if actorTenant != "" && item.ActorTenant != actorTenant {
			continue
		}
		if actorUser != "" && item.ActorUser != actorUser {
			continue
		}
		if in.Since != nil && item.CreatedAt.Before(*in.Since) {
			continue
		}
		if in.Until != nil && item.CreatedAt.After(*in.Until) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered, nil
}

func (s *Service) GetRun(ctx context.Context, p Principal, instanceID, runID string) (*Run, error) {
	_ = ctx
	v, err := s.store.GetRun(p.TenantID, p.UserID, instanceID, runID)
	if err != nil {
		return nil, err
	}
	enriched, err := s.enrichRun(v)
	if err != nil {
		return nil, err
	}
	return &enriched, nil
}

func (s *Service) CancelRun(ctx context.Context, p Principal, instanceID, runID string) (*Run, error) {
	_ = ctx
	if _, err := s.store.GetInstance(p.TenantID, p.UserID, instanceID); err != nil {
		return nil, err
	}
	run, err := s.store.GetRun(p.TenantID, p.UserID, instanceID, runID)
	if err != nil {
		return nil, err
	}
	cancel, ok := s.takeRunCancel(run.ID)
	if !ok || run.Status != RunStatusRunning {
		if run.Status == RunStatusCancelled {
			enriched, enrichErr := s.enrichRun(run)
			if enrichErr != nil {
				return nil, enrichErr
			}
			return &enriched, nil
		}
		return nil, ErrRunNotRunning
	}
	cancel()
	completed := s.now()
	run.Status = RunStatusCancelled
	run.Error = "run cancelled"
	run.CompletedAt = &completed
	run.DurationMs = completed.Sub(run.StartedAt).Milliseconds()
	if err := s.store.SaveRun(run); err != nil {
		return nil, err
	}
	_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "run.cancel_requested", ResourceType: "run", ResourceID: run.ID, ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID, Metadata: map[string]string{"instance_id": instanceID, "session_id": run.SessionID}})
	enriched, err := s.enrichRun(run)
	if err != nil {
		return nil, err
	}
	return &enriched, nil
}

func (s *Service) ListRuns(ctx context.Context, p Principal, instanceID string, in ListRunsInput) ([]Run, error) {
	_ = ctx
	if _, err := s.store.GetInstance(p.TenantID, p.UserID, instanceID); err != nil {
		return nil, err
	}
	items, err := s.store.ListRuns(p.TenantID, p.UserID, instanceID)
	if err != nil {
		return nil, err
	}
	status := in.Status
	sessionID := strings.TrimSpace(in.SessionID)
	responseSource := strings.TrimSpace(in.ResponseSource)
	filtered := make([]Run, 0, len(items))
	for _, item := range items {
		if status != "" && item.Status != status {
			continue
		}
		if sessionID != "" && item.SessionID != sessionID {
			continue
		}
		if responseSource != "" && strings.TrimSpace(item.ResponseSource) != responseSource {
			continue
		}
		if in.WaitingForUser != nil && item.WaitingForUser != *in.WaitingForUser {
			continue
		}
		filtered = append(filtered, item)
	}
	return s.enrichRuns(filtered)
}

type quotaMetric string

const (
	quotaMetricInstances quotaMetric = "instances"
	quotaMetricSessions  quotaMetric = "sessions"
	quotaMetricMessages  quotaMetric = "messages"
	quotaMetricRuns      quotaMetric = "runs"
)

func applyQuotaUpdate(quota *TenantQuota, maxInstances, maxSessions, maxMessages, maxRuns *int) error {
	if quota == nil {
		return nil
	}
	if maxInstances != nil {
		if *maxInstances < 0 {
			return fmt.Errorf("max_instances must be >= 0")
		}
		quota.MaxInstances = *maxInstances
	}
	if maxSessions != nil {
		if *maxSessions < 0 {
			return fmt.Errorf("max_sessions must be >= 0")
		}
		quota.MaxSessions = *maxSessions
	}
	if maxMessages != nil {
		if *maxMessages < 0 {
			return fmt.Errorf("max_messages must be >= 0")
		}
		quota.MaxMessages = *maxMessages
	}
	if maxRuns != nil {
		if *maxRuns < 0 {
			return fmt.Errorf("max_runs must be >= 0")
		}
		quota.MaxRuns = *maxRuns
	}
	return nil
}

func effectiveQuotaLimit(tenantQuota, userQuota TenantQuota, metric quotaMetric) int {
	tenantLimit := quotaValue(tenantQuota, metric)
	userLimit := quotaValue(userQuota, metric)
	if tenantLimit == 0 {
		return userLimit
	}
	if userLimit == 0 {
		return tenantLimit
	}
	if tenantLimit < userLimit {
		return tenantLimit
	}
	return userLimit
}

func quotaValue(quota TenantQuota, metric quotaMetric) int {
	switch metric {
	case quotaMetricInstances:
		return quota.MaxInstances
	case quotaMetricSessions:
		return quota.MaxSessions
	case quotaMetricMessages:
		return quota.MaxMessages
	case quotaMetricRuns:
		return quota.MaxRuns
	default:
		return 0
	}
}

func (s *Service) enforceQuotaLimit(tenantID, userID string, metric quotaMetric) error {
	tenant, err := s.store.GetTenant(tenantID)
	if err != nil {
		return err
	}
	user, err := s.store.GetUser(tenantID, userID)
	if err != nil {
		return err
	}
	limit := effectiveQuotaLimit(tenant.Quota, user.Quota, metric)
	if limit <= 0 {
		return nil
	}
	count, err := s.currentQuotaUsage(tenantID, userID, metric)
	if err != nil {
		return err
	}
	if count >= limit {
		return fmt.Errorf("%w: %s limit reached (%d)", ErrQuotaExceeded, metric, limit)
	}
	return nil
}

func (s *Service) currentQuotaUsage(tenantID, userID string, metric quotaMetric) (int, error) {
	instances, err := s.store.ListInstances(tenantID, userID)
	if err != nil {
		return 0, err
	}
	switch metric {
	case quotaMetricInstances:
		return len(instances), nil
	case quotaMetricSessions, quotaMetricMessages, quotaMetricRuns:
		count := 0
		for _, inst := range instances {
			sessions, err := s.store.ListSessions(tenantID, userID, inst.ID)
			if err != nil {
				return 0, err
			}
			if metric == quotaMetricSessions {
				count += len(sessions)
				continue
			}
			if metric == quotaMetricRuns {
				runs, err := s.store.ListRuns(tenantID, userID, inst.ID)
				if err != nil {
					return 0, err
				}
				count += len(runs)
				continue
			}
			for _, sess := range sessions {
				messages, err := s.store.ListMessages(sess.ID)
				if err != nil {
					return 0, err
				}
				count += len(messages)
			}
		}
		return count, nil
	default:
		return 0, nil
	}
}
func mergeQuota(tenantQuota, userQuota TenantQuota) TenantQuota {
	return TenantQuota{
		MaxInstances: effectiveQuotaLimit(tenantQuota, userQuota, quotaMetricInstances),
		MaxSessions:  effectiveQuotaLimit(tenantQuota, userQuota, quotaMetricSessions),
		MaxMessages:  effectiveQuotaLimit(tenantQuota, userQuota, quotaMetricMessages),
		MaxRuns:      effectiveQuotaLimit(tenantQuota, userQuota, quotaMetricRuns),
	}
}

func buildQuotaUsageItem(limit, used int) QuotaUsageItem {
	item := QuotaUsageItem{Limit: limit, Used: used}
	if limit <= 0 {
		item.Unlimited = true
		item.Limit = 0
		return item
	}
	remaining := limit - used
	if remaining < 0 {
		remaining = 0
	}
	item.Remaining = &remaining
	return item
}

func buildQuotaUsageSnapshot(limit TenantQuota, usage *UsageSummary) QuotaUsageSnapshot {
	if usage == nil {
		usage = &UsageSummary{}
	}
	return QuotaUsageSnapshot{
		Instances: buildQuotaUsageItem(limit.MaxInstances, usage.Instances),
		Sessions:  buildQuotaUsageItem(limit.MaxSessions, usage.Sessions),
		Messages:  buildQuotaUsageItem(limit.MaxMessages, usage.Messages),
		Runs:      buildQuotaUsageItem(limit.MaxRuns, usage.Runs),
	}
}
func (s *Service) GetUsageSummary(ctx context.Context, p Principal) (*UsageSummary, error) {
	_ = ctx
	if _, err := s.store.GetUser(p.TenantID, p.UserID); err != nil {
		return nil, err
	}
	return s.buildUsageSummary(p.TenantID, p.UserID)
}

func (s *Service) GetAdminOverview(ctx context.Context) (*AdminOverview, error) {
	_ = ctx
	tenants, err := s.store.ListTenants()
	if err != nil {
		return nil, err
	}
	overview := &AdminOverview{Tenants: len(tenants), RunsByStatus: map[RunStatus]int{}}
	for _, tenant := range tenants {
		if tenant.Status == TenantStatusDisabled {
			overview.DisabledTenants++
		} else {
			overview.ActiveTenants++
		}
		users, err := s.store.ListUsers(tenant.ID)
		if err != nil {
			return nil, err
		}
		overview.Users += len(users)
		for _, user := range users {
			if user.Status == UserStatusDisabled {
				overview.DisabledUsers++
			} else {
				overview.ActiveUsers++
			}

			usage, err := s.buildUsageSummary(tenant.ID, user.ID)
			if err != nil {
				return nil, err
			}
			overview.Credentials += usage.Credentials
			overview.ActiveCredentials += usage.ActiveCredentials
			overview.SuspendedCredentials += usage.SuspendedCredentials
			overview.RevokedCredentials += usage.RevokedCredentials
			overview.ExpiredCredentials += usage.ExpiredCredentials
			overview.ExpiringCredentials += usage.ExpiringCredentials
			overview.Instances += usage.Instances
			overview.ReadyInstances += usage.ReadyInstances
			overview.StoppedInstances += usage.StoppedInstances
			overview.Sessions += usage.Sessions
			overview.Messages += usage.Messages
			overview.UserMessages += usage.UserMessages
			overview.AssistantMessages += usage.AssistantMessages
			overview.Runs += usage.Runs
			if usage.LastActivityAt != nil {
				overview.LastActivityAt = laterTime(overview.LastActivityAt, *usage.LastActivityAt)
			}
			for status, count := range usage.RunsByStatus {
				overview.RunsByStatus[status] += count
			}
		}
	}
	auditEvents, err := s.store.ListAuditEvents("", "")
	if err != nil {
		return nil, err
	}
	overview.AuditEvents = len(auditEvents)
	snapshots, snapshotBytes, err := s.snapshotStats(ctx)
	if err != nil {
		return nil, err
	}
	overview.Snapshots = snapshots
	overview.SnapshotBytes = snapshotBytes
	for _, event := range auditEvents {
		overview.LastAuditAt = laterTime(overview.LastAuditAt, event.CreatedAt)
	}
	return overview, nil
}
func (s *Service) snapshotStats(ctx context.Context) (int, int64, error) {
	snapshots, err := s.ListServiceSnapshots(ctx, ListServiceSnapshotsInput{})
	if err != nil {
		return 0, 0, err
	}
	var bytes int64
	for _, snapshot := range snapshots {
		bytes += snapshot.SizeBytes
	}
	return len(snapshots), bytes, nil
}
func buildAdminTrendPoints(now time.Time, points int, bucketSize time.Duration) []AdminTrendPoint {
	items := make([]AdminTrendPoint, 0, points)
	start := now.Truncate(bucketSize).Add(-bucketSize * time.Duration(points-1))
	for i := 0; i < points; i++ {
		items = append(items, AdminTrendPoint{BucketStart: start.Add(bucketSize * time.Duration(i)), RunsByStatus: map[RunStatus]int{}})
	}
	return items
}

func addTrendMessage(points []AdminTrendPoint, ts time.Time, bucketSize time.Duration) {
	for i := range points {
		start := points[i].BucketStart
		end := start.Add(bucketSize)
		if (ts.Equal(start) || ts.After(start)) && ts.Before(end) {
			points[i].Messages++
			return
		}
	}
}

func addTrendRun(points []AdminTrendPoint, run Run, bucketSize time.Duration) {
	for i := range points {
		start := points[i].BucketStart
		end := start.Add(bucketSize)
		if (run.StartedAt.Equal(start) || run.StartedAt.After(start)) && run.StartedAt.Before(end) {
			points[i].Runs++
			points[i].RunsByStatus[run.Status]++
			return
		}
	}
}

func addTrendAudit(points []AdminTrendPoint, event AuditEvent, bucketSize time.Duration) {
	for i := range points {
		start := points[i].BucketStart
		end := start.Add(bucketSize)
		if (event.CreatedAt.Equal(start) || event.CreatedAt.After(start)) && event.CreatedAt.Before(end) {
			points[i].AuditEvents++
			return
		}
	}
}

func (s *Service) GetAdminDashboard(ctx context.Context) (*AdminDashboard, error) {
	overview, err := s.GetAdminOverview(ctx)
	if err != nil {
		return nil, err
	}
	auditEvents, err := s.store.ListAuditEvents("", "")
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	last24 := buildAdminTrendPoints(now, 24, time.Hour)
	last7 := buildAdminTrendPoints(now, 7, 24*time.Hour)
	tenants, err := s.store.ListTenants()
	if err != nil {
		return nil, err
	}
	for _, tenant := range tenants {
		users, err := s.store.ListUsers(tenant.ID)
		if err != nil {
			return nil, err
		}
		for _, user := range users {
			instances, err := s.store.ListInstances(tenant.ID, user.ID)
			if err != nil {
				return nil, err
			}
			for _, inst := range instances {
				sessions, err := s.store.ListSessions(tenant.ID, user.ID, inst.ID)
				if err != nil {
					return nil, err
				}
				for _, sess := range sessions {
					messages, err := s.store.ListMessages(sess.ID)
					if err != nil {
						return nil, err
					}
					for _, msg := range messages {
						addTrendMessage(last24, msg.CreatedAt.UTC(), time.Hour)
						addTrendMessage(last7, msg.CreatedAt.UTC(), 24*time.Hour)
					}
				}
				runs, err := s.store.ListRuns(tenant.ID, user.ID, inst.ID)
				if err != nil {
					return nil, err
				}
				for _, run := range runs {
					addTrendRun(last24, run, time.Hour)
					addTrendRun(last7, run, 24*time.Hour)
				}
			}
		}
	}
	for _, event := range auditEvents {
		addTrendAudit(last24, event, time.Hour)
		addTrendAudit(last7, event, 24*time.Hour)
	}
	sort.Slice(auditEvents, func(i, j int) bool { return auditEvents[i].CreatedAt.After(auditEvents[j].CreatedAt) })
	recent := auditEvents
	if len(recent) > 10 {
		recent = recent[:10]
	}
	return &AdminDashboard{
		Overview:          *overview,
		RecentAuditEvents: recent,
		Last24Hours:       last24,
		Last7Days:         last7,
		GeneratedAt:       now,
	}, nil
}

func (s *Service) GetAdminInsights(ctx context.Context, in AdminInsightsInput) (*AdminInsights, error) {
	_ = ctx
	now := s.now().UTC()
	limit := in.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}
	inactiveForDays := in.InactiveForDays
	if inactiveForDays <= 0 {
		inactiveForDays = 30
	}
	cutoff := now.Add(-time.Duration(inactiveForDays) * 24 * time.Hour)
	insights := &AdminInsights{GeneratedAt: now, InactiveCutoff: cutoff}
	tenants, err := s.store.ListTenants()
	if err != nil {
		return nil, err
	}
	for _, tenant := range tenants {
		summary, err := s.GetTenantSummary(ctx, tenant.ID)
		if err != nil {
			return nil, err
		}
		insights.TopTenants = append(insights.TopTenants, AdminTenantInsight{
			TenantID:       summary.TenantID,
			Name:           summary.Name,
			Status:         summary.Status,
			Users:          summary.Users,
			ActiveUsers:    summary.ActiveUsers,
			Instances:      summary.Instances,
			Messages:       summary.Messages,
			Runs:           summary.Runs,
			ActivityScore:  summary.Messages + summary.Runs,
			LastActivityAt: summary.LastActivityAt,
		})
		appendQuotaPressureInsights(&insights.QuotaPressure, "tenant", summary.TenantID, summary.Name, "", "", summary.QuotaUsage, summary.LastActivityAt)
		for _, user := range summary.UserSummaries {
			inactiveReason, inactiveDays, inactive := classifyInactiveUser(user, cutoff, now)
			if inactive {
				insights.InactiveUsers = append(insights.InactiveUsers, AdminInactiveUserInsight{
					TenantID:       summary.TenantID,
					UserID:         user.UserID,
					Name:           user.Name,
					Email:          user.Email,
					Status:         user.Status,
					Instances:      user.Instances,
					Messages:       user.Messages,
					Runs:           user.Runs,
					LastActivityAt: user.LastActivityAt,
					InactiveDays:   inactiveDays,
					Reason:         inactiveReason,
				})
			}
			appendQuotaPressureInsights(&insights.QuotaPressure, "user", summary.TenantID, summary.Name, user.UserID, user.Name, user.QuotaUsage, user.LastActivityAt)
		}
	}
	sort.Slice(insights.TopTenants, func(i, j int) bool {
		if insights.TopTenants[i].ActivityScore == insights.TopTenants[j].ActivityScore {
			if insights.TopTenants[i].Runs == insights.TopTenants[j].Runs {
				return insights.TopTenants[i].Messages > insights.TopTenants[j].Messages
			}
			return insights.TopTenants[i].Runs > insights.TopTenants[j].Runs
		}
		return insights.TopTenants[i].ActivityScore > insights.TopTenants[j].ActivityScore
	})
	sort.Slice(insights.InactiveUsers, func(i, j int) bool {
		if insights.InactiveUsers[i].InactiveDays == insights.InactiveUsers[j].InactiveDays {
			return insights.InactiveUsers[i].UserID < insights.InactiveUsers[j].UserID
		}
		return insights.InactiveUsers[i].InactiveDays > insights.InactiveUsers[j].InactiveDays
	})
	sort.Slice(insights.QuotaPressure, func(i, j int) bool {
		if insights.QuotaPressure[i].PressureRatio == insights.QuotaPressure[j].PressureRatio {
			return insights.QuotaPressure[i].Used > insights.QuotaPressure[j].Used
		}
		return insights.QuotaPressure[i].PressureRatio > insights.QuotaPressure[j].PressureRatio
	})
	if len(insights.TopTenants) > limit {
		insights.TopTenants = insights.TopTenants[:limit]
	}
	if len(insights.InactiveUsers) > limit {
		insights.InactiveUsers = insights.InactiveUsers[:limit]
	}
	if len(insights.QuotaPressure) > limit {
		insights.QuotaPressure = insights.QuotaPressure[:limit]
	}
	return insights, nil
}

func classifyInactiveUser(user TenantUserSummary, cutoff, now time.Time) (string, int, bool) {
	if user.LastActivityAt == nil {
		return "no recorded activity", 0, true
	}
	if user.LastActivityAt.Before(cutoff) {
		inactiveDays := int(now.Sub(*user.LastActivityAt).Hours() / 24)
		if inactiveDays < 0 {
			inactiveDays = 0
		}
		return "last activity is older than the inactivity cutoff", inactiveDays, true
	}
	return "", 0, false
}

func appendQuotaPressureInsights(items *[]AdminQuotaPressureInsight, scope, tenantID, tenantName, userID, userName string, usage QuotaUsageSnapshot, lastActivityAt *time.Time) {
	appendQuotaPressureMetric(items, scope, "instances", tenantID, tenantName, userID, userName, usage.Instances, lastActivityAt)
	appendQuotaPressureMetric(items, scope, "sessions", tenantID, tenantName, userID, userName, usage.Sessions, lastActivityAt)
	appendQuotaPressureMetric(items, scope, "messages", tenantID, tenantName, userID, userName, usage.Messages, lastActivityAt)
	appendQuotaPressureMetric(items, scope, "runs", tenantID, tenantName, userID, userName, usage.Runs, lastActivityAt)
}

func appendQuotaPressureMetric(items *[]AdminQuotaPressureInsight, scope, metric, tenantID, tenantName, userID, userName string, usage QuotaUsageItem, lastActivityAt *time.Time) {
	if usage.Unlimited || usage.Limit <= 0 {
		return
	}
	ratio := 0.0
	if usage.Limit > 0 {
		ratio = float64(usage.Used) / float64(usage.Limit)
	}
	if ratio < 0.8 {
		return
	}
	status := "high"
	if ratio >= 1.0 {
		status = "exceeded"
	} else if ratio >= 0.9 {
		status = "critical"
	}
	*items = append(*items, AdminQuotaPressureInsight{
		Scope:          scope,
		Metric:         metric,
		TenantID:       tenantID,
		TenantName:     tenantName,
		UserID:         userID,
		UserName:       userName,
		Limit:          usage.Limit,
		Used:           usage.Used,
		Remaining:      usage.Remaining,
		PressureRatio:  ratio,
		Status:         status,
		LastActivityAt: lastActivityAt,
	})
}

func (s *Service) GetAdminAlerts(ctx context.Context, in AdminAlertsInput) (*AdminAlerts, error) {
	_ = ctx
	now := s.now().UTC()
	alerts := &AdminAlerts{GeneratedAt: now}
	expiryWindowDays := in.CredentialExpiryWindowDays
	if expiryWindowDays <= 0 {
		expiryWindowDays = 7
	}
	if expiryWindowDays > 365 {
		expiryWindowDays = 365
	}
	credentialExpiryCutoff := now.Add(time.Duration(expiryWindowDays) * 24 * time.Hour)
	tenants, err := s.store.ListTenants()
	if err != nil {
		return nil, err
	}
	kind := strings.TrimSpace(in.Kind)
	for _, tenant := range tenants {
		if strings.TrimSpace(in.TenantID) != "" && tenant.ID != strings.TrimSpace(in.TenantID) {
			continue
		}
		users, err := s.store.ListUsers(tenant.ID)
		if err != nil {
			return nil, err
		}
		for _, user := range users {
			if strings.TrimSpace(in.UserID) != "" && user.ID != strings.TrimSpace(in.UserID) {
				continue
			}
			credentials, err := s.store.ListCredentials(tenant.ID, user.ID)
			if err != nil {
				return nil, err
			}
			for _, cred := range credentials {
				if cred.ExpiresAt == nil {
					continue
				}
				expiresAt := cred.ExpiresAt.UTC()
				if in.Since != nil && expiresAt.Before(*in.Since) {
					continue
				}
				alertKind := ""
				severity := "medium"
				title := "Credential expiring"
				reason := "credential expires soon"
				if !expiresAt.After(now) {
					alertKind = "credential_expired"
					severity = "high"
					title = "Credential expired"
					reason = "credential has expired"
				} else if !expiresAt.After(credentialExpiryCutoff) {
					alertKind = "credential_expiring"
				} else {
					continue
				}
				if kind != "" && kind != alertKind {
					continue
				}
				sanitized := sanitizeCredential(cred)
				alerts.CredentialAlerts = append(alerts.CredentialAlerts, sanitized)
				occurredAt := expiresAt
				alerts.Items = append(alerts.Items, AdminAlertItem{
					Kind:            alertKind,
					Severity:        severity,
					Title:           title,
					SuggestedAction: "Rotate the credential or extend expires_at before clients lose access.",
					TenantID:        cred.TenantID,
					UserID:          cred.UserID,
					CredentialID:    cred.ID,
					OccurredAt:      &occurredAt,
					Reason:          reason,
				})
			}
			instances, err := s.store.ListInstances(tenant.ID, user.ID)
			if err != nil {
				return nil, err
			}
			for _, inst := range instances {
				inst = s.withInstanceReadiness(inst)
				if !inst.Ready && (kind == "" || kind == "unready_instance") {
					if in.Since == nil || !inst.UpdatedAt.Before(*in.Since) {
						alerts.UnreadyInstances = append(alerts.UnreadyInstances, inst)
						occurredAt := inst.UpdatedAt
						alerts.Items = append(alerts.Items, AdminAlertItem{
							Kind:            "unready_instance",
							Severity:        "high",
							Title:           "Instance not ready",
							SuggestedAction: "Review user config and refresh instance readiness.",
							TenantID:        inst.TenantID,
							UserID:          inst.UserID,
							InstanceID:      inst.ID,
							OccurredAt:      &occurredAt,
							Reason:          inst.ReadyReason,
						})
					}
				}
				runs, err := s.store.ListRuns(tenant.ID, user.ID, inst.ID)
				if err != nil {
					return nil, err
				}
				enrichedRuns, err := s.enrichRuns(runs)
				if err != nil {
					return nil, err
				}
				for _, run := range enrichedRuns {
					if in.Since != nil && run.StartedAt.Before(*in.Since) {
						continue
					}
					if run.WaitingForUser && (kind == "" || kind == "waiting_run") {
						alerts.WaitingRuns = append(alerts.WaitingRuns, run)
						occurredAt := run.StartedAt
						alerts.Items = append(alerts.Items, AdminAlertItem{
							Kind:            "waiting_run",
							Severity:        "medium",
							Title:           "Run waiting for user input",
							SuggestedAction: "Prompt the user to answer the pending question and resume the session.",
							TenantID:        run.TenantID,
							UserID:          run.UserID,
							InstanceID:      run.InstanceID,
							SessionID:       run.SessionID,
							RunID:           run.ID,
							OccurredAt:      &occurredAt,
							Reason:          "run is waiting for user input",
						})
					}
					if run.Status == RunStatusFailed && (kind == "" || kind == "failed_run") {
						alerts.FailedRuns = append(alerts.FailedRuns, run)
						occurredAt := run.StartedAt
						alerts.Items = append(alerts.Items, AdminAlertItem{
							Kind:            "failed_run",
							Severity:        "high",
							Title:           "Run failed",
							SuggestedAction: "Inspect the run error and retry after fixing configuration or inputs.",
							TenantID:        run.TenantID,
							UserID:          run.UserID,
							InstanceID:      run.InstanceID,
							SessionID:       run.SessionID,
							RunID:           run.ID,
							OccurredAt:      &occurredAt,
							Reason:          run.Error,
						})
					}
				}
			}
		}
	}
	sort.Slice(alerts.UnreadyInstances, func(i, j int) bool {
		return alerts.UnreadyInstances[i].UpdatedAt.After(alerts.UnreadyInstances[j].UpdatedAt)
	})
	sort.Slice(alerts.WaitingRuns, func(i, j int) bool { return alerts.WaitingRuns[i].StartedAt.After(alerts.WaitingRuns[j].StartedAt) })
	sort.Slice(alerts.FailedRuns, func(i, j int) bool { return alerts.FailedRuns[i].StartedAt.After(alerts.FailedRuns[j].StartedAt) })
	sort.Slice(alerts.CredentialAlerts, func(i, j int) bool {
		if alerts.CredentialAlerts[i].ExpiresAt == nil {
			return false
		}
		if alerts.CredentialAlerts[j].ExpiresAt == nil {
			return true
		}
		return alerts.CredentialAlerts[i].ExpiresAt.Before(*alerts.CredentialAlerts[j].ExpiresAt)
	})
	sort.Slice(alerts.Items, func(i, j int) bool {
		if alerts.Items[i].OccurredAt == nil {
			return false
		}
		if alerts.Items[j].OccurredAt == nil {
			return true
		}
		return alerts.Items[i].OccurredAt.After(*alerts.Items[j].OccurredAt)
	})
	if in.Limit > 0 && len(alerts.Items) > in.Limit {
		alerts.Items = alerts.Items[:in.Limit]
	}
	return alerts, nil
}
func (s *Service) ExportServiceState(ctx context.Context, in ExportServiceStateInput) (*ExportServiceStateOutput, error) {
	_ = ctx
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.UserID = strings.TrimSpace(in.UserID)
	if in.UserID != "" && in.TenantID == "" {
		return nil, fmt.Errorf("tenant_id is required when user_id is set")
	}
	out := &ExportServiceStateOutput{
		IncludeMessages: in.IncludeMessages,
		IncludeRuns:     in.IncludeRuns,
		IncludeAudit:    in.IncludeAudit,
		IncludeSecrets:  in.IncludeSecrets,
		ExportedAt:      s.now().UTC(),
	}
	var tenants []Tenant
	if in.TenantID != "" {
		tenant, err := s.store.GetTenant(in.TenantID)
		if err != nil {
			return nil, err
		}
		tenants = []Tenant{tenant}
		out.Scope = "tenant"
		out.TenantID = in.TenantID
		if in.UserID != "" {
			out.Scope = "user"
			out.UserID = in.UserID
		}
	} else {
		items, err := s.store.ListTenants()
		if err != nil {
			return nil, err
		}
		tenants = items
		out.Scope = "service"
	}
	out.Tenants = append([]Tenant(nil), tenants...)
	for _, tenant := range tenants {
		users, err := s.store.ListUsers(tenant.ID)
		if err != nil {
			return nil, err
		}
		for _, user := range users {
			if in.UserID != "" && user.ID != in.UserID {
				continue
			}
			exported, err := s.exportUserState(tenant.ID, user.ID, in)
			if err != nil {
				return nil, err
			}
			out.Users = append(out.Users, exported)
		}
	}
	if in.UserID != "" && len(out.Users) == 0 {
		return nil, ErrUserNotFound
	}
	if in.IncludeAudit {
		auditTenantID := in.TenantID
		auditUserID := in.UserID
		items, err := s.store.ListAuditEvents(auditTenantID, auditUserID)
		if err != nil {
			return nil, err
		}
		out.AuditEvents = items
		if !in.IncludeSecrets {
			out.AuditEvents = redactAuditEventsForExport(s.dataRoot, out.AuditEvents)
		}
	}
	return out, nil
}

func (s *Service) exportUserState(tenantID, userID string, in ExportServiceStateInput) (ExportedUser, error) {
	user, err := s.store.GetUser(tenantID, userID)
	if err != nil {
		return ExportedUser{}, err
	}
	out := ExportedUser{User: user}
	cfg, err := s.getOrLoadRawUserConfig(tenantID, userID)
	if err == nil {
		if !in.IncludeSecrets {
			cfg.AppConfig = SanitizeAppConfig(cfg.AppConfig)
		}
		out.Config = &cfg
	} else if err != ErrUserConfigNotFound {
		return ExportedUser{}, err
	}
	creds, err := s.store.ListCredentials(tenantID, userID)
	if err != nil {
		return ExportedUser{}, err
	}
	out.Credentials = make([]ExportedCredential, 0, len(creds))
	for _, cred := range creds {
		out.Credentials = append(out.Credentials, exportCredential(cred, in.IncludeSecrets))
	}
	instances, err := s.store.ListInstances(tenantID, userID)
	if err != nil {
		return ExportedUser{}, err
	}
	out.Instances = make([]ExportedInstance, 0, len(instances))
	for _, inst := range instances {
		inst = s.withInstanceReadiness(inst)
		exportedInst := ExportedInstance{Instance: inst}
		sessions, err := s.store.ListSessions(tenantID, userID, inst.ID)
		if err != nil {
			return ExportedUser{}, err
		}
		exportedInst.Sessions = make([]ExportedSession, 0, len(sessions))
		for _, sess := range sessions {
			sess, err = s.enrichSession(sess)
			if err != nil {
				return ExportedUser{}, err
			}
			exportedSession := ExportedSession{Session: sess}
			if in.IncludeMessages {
				messages, err := s.store.ListMessages(sess.ID)
				if err != nil {
					return ExportedUser{}, err
				}
				exportedSession.Messages = messages
			}
			exportedInst.Sessions = append(exportedInst.Sessions, exportedSession)
		}
		if in.IncludeRuns {
			runs, err := s.store.ListRuns(tenantID, userID, inst.ID)
			if err != nil {
				return ExportedUser{}, err
			}
			runs, err = s.enrichRuns(runs)
			if err != nil {
				return ExportedUser{}, err
			}
			exportedInst.Runs = runs
		}
		out.Instances = append(out.Instances, exportedInst)
	}
	return out, nil
}

func exportCredential(cred Credential, includeSecrets bool) ExportedCredential {
	out := ExportedCredential{
		ID:           cred.ID,
		TenantID:     cred.TenantID,
		UserID:       cred.UserID,
		Name:         cred.Name,
		APIKey:       cred.APIKey,
		APIKeyPrefix: cred.APIKeyPrefix,
		APIKeyHash:   cred.APIKeyHash,
		Status:       credentialStatus(cred),
		ExpiresAt:    cred.ExpiresAt,
		TokenVersion: credentialTokenVersion(cred),
		CreatedAt:    cred.CreatedAt,
		UpdatedAt:    cred.UpdatedAt,
	}
	if includeSecrets {
		out.SecretDigest = cred.SecretDigest
		return out
	}
	sanitized := sanitizeCredential(cred)
	out.APIKey = sanitized.APIKey
	out.APIKeyHash = ""
	out.SecretDigest = ""
	return out
}

func (s *Service) CreateServiceSnapshot(ctx context.Context, in CreateServiceSnapshotInput) (*ServiceSnapshotEnvelope, error) {
	includeMessages := in.IncludeMessages == nil || *in.IncludeMessages
	includeRuns := in.IncludeRuns == nil || *in.IncludeRuns
	includeAudit := in.IncludeAudit == nil || *in.IncludeAudit
	includeSecrets := in.IncludeSecrets != nil && *in.IncludeSecrets
	export, err := s.ExportServiceState(ctx, ExportServiceStateInput{
		TenantID:        in.TenantID,
		UserID:          in.UserID,
		IncludeMessages: includeMessages,
		IncludeRuns:     includeRuns,
		IncludeAudit:    includeAudit,
		IncludeSecrets:  includeSecrets,
	})
	if err != nil {
		return nil, err
	}
	createdAt := s.now().UTC()
	snapshot := ServiceSnapshot{
		ID:              NewID("snapshot"),
		Name:            strings.TrimSpace(in.Name),
		Scope:           export.Scope,
		TenantID:        export.TenantID,
		UserID:          export.UserID,
		IncludeMessages: export.IncludeMessages,
		IncludeRuns:     export.IncludeRuns,
		IncludeAudit:    export.IncludeAudit,
		IncludeSecrets:  export.IncludeSecrets,
		CreatedAt:       createdAt,
	}
	if snapshot.Name == "" {
		snapshot.Name = snapshot.Scope + " snapshot"
	}
	path, err := s.snapshotPath(snapshot.ID)
	if err != nil {
		return nil, err
	}
	snapshot.Path = path
	envelope := &ServiceSnapshotEnvelope{Snapshot: snapshot, Data: *export}
	data, err := marshalSnapshotEnvelope(envelope)
	if err != nil {
		return nil, err
	}
	snapshot.SizeBytes = int64(len(data))
	envelope.Snapshot = snapshot
	data, err = marshalSnapshotEnvelope(envelope)
	if err != nil {
		return nil, err
	}
	if err := secureMkdirAll(s.snapshotRoot()); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return nil, err
	}
	if stat, err := os.Stat(path); err == nil {
		envelope.Snapshot.SizeBytes = stat.Size()
	}
	_ = s.recordAudit(auditRecord{TenantID: snapshot.TenantID, UserID: snapshot.UserID, Action: "snapshot.created", ResourceType: "snapshot", ResourceID: snapshot.ID, ActorType: "admin", Metadata: map[string]string{"scope": snapshot.Scope}})
	return envelope, nil
}

func (s *Service) ListServiceSnapshots(ctx context.Context, in ListServiceSnapshotsInput) ([]ServiceSnapshot, error) {
	_ = ctx
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.UserID = strings.TrimSpace(in.UserID)
	in.Scope = strings.TrimSpace(in.Scope)
	name := strings.ToLower(strings.TrimSpace(in.Name))
	if in.UserID != "" && in.TenantID == "" {
		return nil, fmt.Errorf("tenant_id is required when user_id is set")
	}
	if in.Scope != "" && in.Scope != "service" && in.Scope != "tenant" && in.Scope != "user" {
		return nil, fmt.Errorf("scope must be service, tenant, or user")
	}
	entries, err := os.ReadDir(s.snapshotRoot())
	if errors.Is(err, os.ErrNotExist) {
		return []ServiceSnapshot{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]ServiceSnapshot, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		envelope, err := s.readServiceSnapshot(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			return nil, err
		}
		snapshot := envelope.Snapshot
		if in.TenantID != "" && snapshot.TenantID != in.TenantID {
			continue
		}
		if in.UserID != "" && snapshot.UserID != in.UserID {
			continue
		}
		if in.Scope != "" && snapshot.Scope != in.Scope {
			continue
		}
		if name != "" && !strings.Contains(strings.ToLower(snapshot.Name), name) {
			continue
		}
		if in.Since != nil && snapshot.CreatedAt.Before(*in.Since) {
			continue
		}
		if in.Until != nil && snapshot.CreatedAt.After(*in.Until) {
			continue
		}
		out = append(out, snapshot)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *Service) GetServiceSnapshot(ctx context.Context, snapshotID string) (*ServiceSnapshotEnvelope, error) {
	_ = ctx
	return s.readServiceSnapshot(snapshotID)
}

func (s *Service) PruneServiceSnapshots(ctx context.Context, in PruneServiceSnapshotsInput) (*PruneServiceSnapshotsOutput, error) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.UserID = strings.TrimSpace(in.UserID)
	if in.UserID != "" && in.TenantID == "" {
		return nil, fmt.Errorf("tenant_id is required when user_id is set")
	}
	if in.KeepLatest < 0 {
		return nil, fmt.Errorf("keep_latest must be greater than or equal to 0")
	}
	if in.OlderThan == nil && in.KeepLatest == 0 {
		return nil, fmt.Errorf("older_than or keep_latest is required")
	}
	items, err := s.ListServiceSnapshots(ctx, ListServiceSnapshotsInput{TenantID: in.TenantID, UserID: in.UserID})
	if err != nil {
		return nil, err
	}
	out := &PruneServiceSnapshotsOutput{
		TenantID:    in.TenantID,
		UserID:      in.UserID,
		OlderThan:   in.OlderThan,
		KeepLatest:  in.KeepLatest,
		DryRun:      in.DryRun,
		Matched:     len(items),
		GeneratedAt: s.now().UTC(),
	}
	protected := map[string]struct{}{}
	if in.KeepLatest > 0 {
		newest := append([]ServiceSnapshot(nil), items...)
		sort.Slice(newest, func(i, j int) bool { return newest[i].CreatedAt.After(newest[j].CreatedAt) })
		for i, item := range newest {
			if i >= in.KeepLatest {
				break
			}
			protected[item.ID] = struct{}{}
			out.KeptSnapshots = append(out.KeptSnapshots, item)
		}
	}
	for _, item := range items {
		if _, ok := protected[item.ID]; ok {
			continue
		}
		if in.OlderThan != nil && !item.CreatedAt.Before(*in.OlderThan) {
			continue
		}
		out.Snapshots = append(out.Snapshots, item)
		out.FreedBytes += item.SizeBytes
		if in.DryRun {
			continue
		}
		path, err := s.snapshotPath(item.ID)
		if err != nil {
			return nil, err
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		out.Deleted++
	}
	_ = s.recordAudit(auditRecord{TenantID: in.TenantID, UserID: in.UserID, Action: "snapshot.pruned", ResourceType: "snapshot", ActorType: "admin", Metadata: map[string]string{"dry_run": fmt.Sprintf("%v", in.DryRun), "deleted": fmt.Sprintf("%d", out.Deleted), "candidates": fmt.Sprintf("%d", len(out.Snapshots)), "keep_latest": fmt.Sprintf("%d", in.KeepLatest)}})
	return out, nil
}
func (s *Service) RestoreServiceSnapshot(ctx context.Context, snapshotID string, in RestoreServiceSnapshotInput) (*RestoreServiceSnapshotOutput, error) {
	envelope, err := s.readServiceSnapshot(snapshotID)
	if err != nil {
		return nil, err
	}
	imported, err := s.ImportServiceState(ctx, ImportServiceStateRequest{Data: envelope.Data, Overwrite: in.Overwrite, DryRun: in.DryRun})
	if err != nil {
		return nil, err
	}
	_ = s.recordAudit(auditRecord{TenantID: envelope.Snapshot.TenantID, UserID: envelope.Snapshot.UserID, Action: "snapshot.restored", ResourceType: "snapshot", ResourceID: envelope.Snapshot.ID, ActorType: "admin", Metadata: map[string]string{"dry_run": fmt.Sprintf("%v", in.DryRun), "overwrite": fmt.Sprintf("%v", in.Overwrite), "scope": envelope.Snapshot.Scope}})
	return &RestoreServiceSnapshotOutput{Snapshot: envelope.Snapshot, Import: *imported}, nil
}
func (s *Service) DeleteServiceSnapshot(ctx context.Context, snapshotID string) (*ServiceSnapshot, error) {
	_ = ctx
	envelope, err := s.readServiceSnapshot(snapshotID)
	if err != nil {
		return nil, err
	}
	path, err := s.snapshotPath(snapshotID)
	if err != nil {
		return nil, err
	}
	if err := os.Remove(path); err != nil {
		return nil, err
	}
	_ = s.recordAudit(auditRecord{TenantID: envelope.Snapshot.TenantID, UserID: envelope.Snapshot.UserID, Action: "snapshot.deleted", ResourceType: "snapshot", ResourceID: envelope.Snapshot.ID, ActorType: "admin", Metadata: map[string]string{"scope": envelope.Snapshot.Scope}})
	return &envelope.Snapshot, nil
}

func (s *Service) readServiceSnapshot(snapshotID string) (*ServiceSnapshotEnvelope, error) {
	path, err := s.snapshotPath(snapshotID)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrSnapshotNotFound
	}
	if err != nil {
		return nil, err
	}
	var envelope ServiceSnapshotEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}
	if envelope.Snapshot.ID == "" {
		return nil, ErrSnapshotNotFound
	}
	envelope.Snapshot.Path = path
	if stat, err := os.Stat(path); err == nil {
		envelope.Snapshot.SizeBytes = stat.Size()
	}
	return &envelope, nil
}

func (s *Service) snapshotRoot() string {
	return filepath.Join(s.dataRoot, "snapshots")
}

func (s *Service) snapshotPath(snapshotID string) (string, error) {
	id := strings.TrimSpace(snapshotID)
	if id == "" || strings.ContainsAny(id, `/\\`) || strings.Contains(id, "..") {
		return "", ErrSnapshotNotFound
	}
	return filepath.Join(s.snapshotRoot(), id+".json"), nil
}

func marshalSnapshotEnvelope(envelope *ServiceSnapshotEnvelope) ([]byte, error) {
	data, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
func (s *Service) ImportServiceState(ctx context.Context, in ImportServiceStateRequest) (*ImportServiceStateOutput, error) {
	_ = ctx
	data := in.Data
	out := &ImportServiceStateOutput{
		Scope:      strings.TrimSpace(data.Scope),
		TenantID:   strings.TrimSpace(data.TenantID),
		UserID:     strings.TrimSpace(data.UserID),
		Overwrite:  in.Overwrite,
		DryRun:     in.DryRun,
		ImportedAt: s.now().UTC(),
	}
	if out.Scope == "" {
		out.Scope = "service"
	}
	if err := s.assessImportState(data, out); err != nil {
		return nil, err
	}
	if in.DryRun {
		return out, nil
	}
	if len(out.Conflicts) > 0 && !in.Overwrite {
		return nil, fmt.Errorf("import conflicts detected: %w", ErrAlreadyExists)
	}
	for _, tenant := range data.Tenants {
		if err := s.store.SaveTenant(tenant); err != nil {
			return nil, err
		}
		if err := secureMkdirAll(filepath.Join(s.dataRoot, "tenants", slugID(tenant.ID))); err != nil {
			return nil, err
		}
	}
	for _, exportedUser := range data.Users {
		currentAppConfig := corelib.AppConfig{}
		if _, err := s.store.GetUser(exportedUser.User.TenantID, exportedUser.User.ID); err == nil {
			if currentConfig, cfgErr := s.getOrLoadRawUserConfig(exportedUser.User.TenantID, exportedUser.User.ID); cfgErr == nil {
				currentAppConfig = currentConfig.AppConfig
			} else if cfgErr != ErrUserConfigNotFound {
				return nil, cfgErr
			}
			if err := s.deleteUserStateForImport(exportedUser.User.TenantID, exportedUser.User.ID); err != nil {
				return nil, err
			}
		} else if err != ErrUserNotFound {
			return nil, err
		}
		if err := s.store.SaveUser(exportedUser.User); err != nil {
			return nil, err
		}
		if err := secureMkdirAll(s.userDataRoot(exportedUser.User.TenantID, exportedUser.User.ID)); err != nil {
			return nil, err
		}
		if err := secureMkdirAll(filepath.Join(s.userRoot(exportedUser.User.TenantID, exportedUser.User.ID), "instances")); err != nil {
			return nil, err
		}
		if exportedUser.Config != nil {
			cfg := *exportedUser.Config
			cfg.TenantID = exportedUser.User.TenantID
			cfg.UserID = exportedUser.User.ID
			cfg.AppConfig = normalizeLLMConfigForSave(currentAppConfig, mergeSecretPreserving(currentAppConfig, cloneAppConfig(cfg.AppConfig)))
			if err := s.store.SaveUserConfig(cfg); err != nil {
				return nil, err
			}
			if err := saveUserConfigToFile(s.userConfigPath(exportedUser.User.TenantID, exportedUser.User.ID), cfg); err != nil {
				return nil, err
			}
		}
		for _, cred := range exportedUser.Credentials {
			storedCred, err := importCredential(exportedUser.User.TenantID, exportedUser.User.ID, cred)
			if err != nil {
				return nil, err
			}
			if err := s.store.SaveCredential(storedCred); err != nil {
				return nil, err
			}
		}
		for _, exportedInst := range exportedUser.Instances {
			inst := s.remapImportedInstance(exportedUser.User.TenantID, exportedUser.User.ID, exportedInst.Instance)
			if err := secureMkdirAll(inst.Workspace); err != nil {
				return nil, err
			}
			inst = s.withInstanceReadiness(inst)
			if err := s.store.SaveInstance(inst); err != nil {
				return nil, err
			}
			for _, exportedSession := range exportedInst.Sessions {
				sess := exportedSession.Session
				sess.TenantID = exportedUser.User.TenantID
				sess.UserID = exportedUser.User.ID
				sess.InstanceID = inst.ID
				if err := s.store.SaveSession(sess); err != nil {
					return nil, err
				}
				for _, msg := range exportedSession.Messages {
					msg.TenantID = exportedUser.User.TenantID
					msg.UserID = exportedUser.User.ID
					msg.InstanceID = inst.ID
					msg.SessionID = sess.ID
					if err := s.store.SaveMessage(msg); err != nil {
						return nil, err
					}
				}
			}
			for _, run := range exportedInst.Runs {
				run.TenantID = exportedUser.User.TenantID
				run.UserID = exportedUser.User.ID
				run.InstanceID = inst.ID
				if err := s.store.SaveRun(run); err != nil {
					return nil, err
				}
			}
		}
	}
	if len(data.AuditEvents) > 0 {
		existingAudit, err := s.store.ListAuditEvents("", "")
		if err != nil {
			return nil, err
		}
		existingByID := make(map[string]struct{}, len(existingAudit))
		for _, item := range existingAudit {
			if strings.TrimSpace(item.ID) != "" {
				existingByID[item.ID] = struct{}{}
			}
		}
		for _, event := range data.AuditEvents {
			if strings.TrimSpace(event.ID) != "" {
				if _, ok := existingByID[event.ID]; ok {
					continue
				}
				existingByID[event.ID] = struct{}{}
			}
			if err := s.store.SaveAuditEvent(event); err != nil {
				return nil, err
			}
		}
	}
	_ = s.recordAudit(auditRecord{TenantID: out.TenantID, UserID: out.UserID, Action: "admin.imported", ResourceType: "service_state", ResourceID: out.Scope, ActorType: "admin", Metadata: map[string]string{"overwrite": strconv.FormatBool(in.Overwrite), "dry_run": strconv.FormatBool(in.DryRun)}})
	return out, nil
}

func (s *Service) assessImportState(data ExportServiceStateOutput, out *ImportServiceStateOutput) error {
	incomingTenants := map[string]struct{}{}
	for _, tenant := range data.Tenants {
		if strings.TrimSpace(tenant.ID) == "" {
			return fmt.Errorf("tenant id is required")
		}
		incomingTenants[tenant.ID] = struct{}{}
		if existing, err := s.store.GetTenant(tenant.ID); err == nil && existing.ID != "" {
			out.Conflicts = append(out.Conflicts, fmt.Sprintf("tenant %s already exists", tenant.ID))
			appendImportPlan(out, "tenant", tenant.ID, "overwrite", "tenant already exists and would be updated")
		} else {
			appendImportPlan(out, "tenant", tenant.ID, "create", "tenant would be imported")
		}
		out.Tenants++
	}
	for _, exportedUser := range data.Users {
		if strings.TrimSpace(exportedUser.User.TenantID) == "" || strings.TrimSpace(exportedUser.User.ID) == "" {
			return fmt.Errorf("user tenant_id and id are required")
		}
		if _, ok := incomingTenants[exportedUser.User.TenantID]; !ok {
			if _, err := s.store.GetTenant(exportedUser.User.TenantID); err != nil {
				return err
			}
		}
		if _, err := s.store.GetUser(exportedUser.User.TenantID, exportedUser.User.ID); err == nil {
			out.Conflicts = append(out.Conflicts, fmt.Sprintf("user %s/%s already exists", exportedUser.User.TenantID, exportedUser.User.ID))
			appendImportPlan(out, "user", exportedUser.User.TenantID+"/"+exportedUser.User.ID, "overwrite", "user already exists and would be replaced with imported state")
		} else if err != ErrUserNotFound {
			return err
		} else {
			appendImportPlan(out, "user", exportedUser.User.TenantID+"/"+exportedUser.User.ID, "create", "user would be imported")
		}
		out.Users++
		if exportedUser.Config != nil && appConfigContainsMaskedSecrets(exportedUser.Config.AppConfig) {
			out.Warnings = append(out.Warnings, fmt.Sprintf("user %s/%s config contains masked secrets and may need manual repair", exportedUser.User.TenantID, exportedUser.User.ID))
		}
		for _, cred := range exportedUser.Credentials {
			storedCred, err := importCredential(exportedUser.User.TenantID, exportedUser.User.ID, cred)
			if err != nil {
				return nilOrErr(out, err)
			}
			_ = storedCred
			appendImportPlan(out, "credential", exportedUser.User.TenantID+"/"+exportedUser.User.ID+"/"+cred.ID, "create", "credential would be imported")
			out.Credentials++
		}
		for _, exportedInst := range exportedUser.Instances {
			appendImportPlan(out, "instance", exportedUser.User.TenantID+"/"+exportedUser.User.ID+"/"+exportedInst.Instance.ID, "create", "instance would be imported and remapped into current data root")
			out.Instances++
			for _, exportedSession := range exportedInst.Sessions {
				appendImportPlan(out, "session", exportedUser.User.TenantID+"/"+exportedUser.User.ID+"/"+exportedSession.Session.ID, "create", "session would be imported")
				out.Sessions++
				if len(exportedSession.Messages) > 0 {
					appendImportPlan(out, "message_batch", exportedUser.User.TenantID+"/"+exportedUser.User.ID+"/"+exportedSession.Session.ID, "create", fmt.Sprintf("%d messages would be imported", len(exportedSession.Messages)))
				}
				out.Messages += len(exportedSession.Messages)
			}
			if len(exportedInst.Runs) > 0 {
				appendImportPlan(out, "run_batch", exportedUser.User.TenantID+"/"+exportedUser.User.ID+"/"+exportedInst.Instance.ID, "create", fmt.Sprintf("%d runs would be imported", len(exportedInst.Runs)))
			}
			out.Runs += len(exportedInst.Runs)
		}
	}
	if len(data.AuditEvents) > 0 {
		existingAudit, err := s.store.ListAuditEvents("", "")
		if err != nil {
			return err
		}
		existingByID := make(map[string]struct{}, len(existingAudit))
		for _, item := range existingAudit {
			if strings.TrimSpace(item.ID) != "" {
				existingByID[item.ID] = struct{}{}
			}
		}
		for _, event := range data.AuditEvents {
			if strings.TrimSpace(event.ID) != "" {
				if _, ok := existingByID[event.ID]; ok {
					out.Warnings = append(out.Warnings, fmt.Sprintf("audit event %s already exists and would be skipped", event.ID))
					continue
				}
				existingByID[event.ID] = struct{}{}
			}
			appendImportPlan(out, "audit_event", event.ID, "create", "audit event would be imported")
			out.AuditEvents++
		}
	}
	return nil
}

func appendImportPlan(out *ImportServiceStateOutput, resourceType, resourceID, action, message string) {
	out.Plan = append(out.Plan, ImportPlanItem{
		ResourceType: strings.TrimSpace(resourceType),
		ResourceID:   strings.TrimSpace(resourceID),
		Action:       strings.TrimSpace(action),
		Message:      strings.TrimSpace(message),
	})
}

func nilOrErr(out *ImportServiceStateOutput, err error) error {
	return err
}

func (s *Service) remapImportedInstance(tenantID, userID string, inst Instance) Instance {
	inst.TenantID = tenantID
	inst.UserID = userID
	inst.DataDir = s.userDataRoot(tenantID, userID)
	inst.RuntimeDir = filepath.Join(s.userRoot(tenantID, userID), "instances", slugID(inst.ID))
	inst.Workspace = filepath.Join(inst.RuntimeDir, "workspace")
	return inst
}

func importCredential(tenantID, userID string, in ExportedCredential) (Credential, error) {
	cred := Credential{
		ID:           strings.TrimSpace(in.ID),
		TenantID:     tenantID,
		UserID:       userID,
		Name:         strings.TrimSpace(in.Name),
		APIKey:       strings.TrimSpace(in.APIKey),
		APIKeyPrefix: strings.TrimSpace(in.APIKeyPrefix),
		APIKeyHash:   strings.TrimSpace(in.APIKeyHash),
		Status:       in.Status,
		ExpiresAt:    in.ExpiresAt,
		TokenVersion: in.TokenVersion,
		SecretDigest: strings.TrimSpace(in.SecretDigest),
		CreatedAt:    in.CreatedAt,
		UpdatedAt:    in.UpdatedAt,
	}
	if cred.ID == "" {
		return Credential{}, fmt.Errorf("credential id is required")
	}
	if cred.Status == "" {
		cred.Status = CredentialStatusActive
	}
	cred.TokenVersion = credentialTokenVersion(cred)
	if cred.APIKeyHash == "" && cred.APIKey != "" {
		cred.APIKeyHash = hashAPIKey(cred.APIKey)
	}
	if cred.APIKeyPrefix == "" && cred.APIKey != "" {
		cred.APIKeyPrefix = deriveAPIKeyPrefix(cred.APIKey)
	}
	if cred.APIKeyHash == "" || cred.APIKeyPrefix == "" {
		return Credential{}, fmt.Errorf("credential api key hash and prefix are required for import")
	}
	if cred.SecretDigest == "" {
		return Credential{}, fmt.Errorf("credential secret_digest is required for import; export with include_secrets=true")
	}
	return cred, nil
}

func (s *Service) deleteUserStateForImport(tenantID, userID string) error {
	instances, err := s.store.ListInstances(tenantID, userID)
	if err != nil {
		return err
	}
	for _, inst := range instances {
		busy, err := s.hasRunningRuns(tenantID, userID, inst.ID, "")
		if err != nil {
			return err
		}
		if busy {
			return ErrUserBusy
		}
	}
	if err := s.store.DeleteUser(tenantID, userID); err != nil {
		return err
	}
	if err := secureRemoveAllWithin(filepath.Join(s.dataRoot, "tenants", slugID(tenantID), "users"), s.userRoot(tenantID, userID)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s *Service) GetTenantSummary(ctx context.Context, tenantID string) (*TenantSummary, error) {
	_ = ctx
	tenant, err := s.store.GetTenant(tenantID)
	if err != nil {
		return nil, err
	}
	users, err := s.store.ListUsers(tenantID)
	if err != nil {
		return nil, err
	}
	summary := TenantSummary{
		TenantID:      tenant.ID,
		Name:          tenant.Name,
		Status:        tenant.Status,
		Quota:         tenant.Quota,
		Users:         len(users),
		RunsByStatus:  map[RunStatus]int{},
		UserSummaries: make([]TenantUserSummary, 0, len(users)),
	}
	for _, user := range users {
		if user.Status == UserStatusDisabled {
			summary.DisabledUsers++
		} else {
			summary.ActiveUsers++
		}
		usage, err := s.buildUsageSummary(tenantID, user.ID)
		if err != nil {
			return nil, err
		}
		effectiveQuota := mergeQuota(tenant.Quota, user.Quota)
		userSummary := TenantUserSummary{
			UserID:               user.ID,
			Name:                 user.Name,
			Email:                user.Email,
			Status:               user.Status,
			DataDir:              usage.DataDir,
			Quota:                user.Quota,
			EffectiveQuota:       effectiveQuota,
			QuotaUsage:           buildQuotaUsageSnapshot(effectiveQuota, usage),
			Instances:            usage.Instances,
			ReadyInstances:       usage.ReadyInstances,
			StoppedInstances:     usage.StoppedInstances,
			Sessions:             usage.Sessions,
			Messages:             usage.Messages,
			UserMessages:         usage.UserMessages,
			AssistantMessages:    usage.AssistantMessages,
			Runs:                 usage.Runs,
			RunsByStatus:         usage.RunsByStatus,
			Credentials:          usage.Credentials,
			ActiveCredentials:    usage.ActiveCredentials,
			SuspendedCredentials: usage.SuspendedCredentials,
			RevokedCredentials:   usage.RevokedCredentials,
			ExpiredCredentials:   usage.ExpiredCredentials,
			ExpiringCredentials:  usage.ExpiringCredentials,
			LastActivityAt:       usage.LastActivityAt,
		}
		summary.UserSummaries = append(summary.UserSummaries, userSummary)
		summary.Credentials += userSummary.Credentials
		summary.ActiveCredentials += userSummary.ActiveCredentials
		summary.SuspendedCredentials += userSummary.SuspendedCredentials
		summary.RevokedCredentials += userSummary.RevokedCredentials
		summary.ExpiredCredentials += userSummary.ExpiredCredentials
		summary.ExpiringCredentials += userSummary.ExpiringCredentials
		summary.Instances += usage.Instances
		summary.ReadyInstances += usage.ReadyInstances
		summary.StoppedInstances += usage.StoppedInstances
		summary.Sessions += usage.Sessions
		summary.Messages += usage.Messages
		summary.UserMessages += usage.UserMessages
		summary.AssistantMessages += usage.AssistantMessages
		summary.Runs += usage.Runs
		if usage.LastActivityAt != nil {
			summary.LastActivityAt = laterTime(summary.LastActivityAt, *usage.LastActivityAt)
		}
		for status, count := range usage.RunsByStatus {
			summary.RunsByStatus[status] += count
		}
	}
	summary.QuotaUsage = buildQuotaUsageSnapshot(tenant.Quota, &UsageSummary{Instances: summary.Instances, Sessions: summary.Sessions, Messages: summary.Messages, Runs: summary.Runs})
	return &summary, nil
}

func (s *Service) buildUsageSummary(tenantID, userID string) (*UsageSummary, error) {
	instances, err := s.store.ListInstances(tenantID, userID)
	if err != nil {
		return nil, err
	}
	tenant, err := s.store.GetTenant(tenantID)
	if err != nil {
		return nil, err
	}
	user, err := s.store.GetUser(tenantID, userID)
	if err != nil {
		return nil, err
	}
	effectiveQuota := mergeQuota(tenant.Quota, user.Quota)
	summary := UsageSummary{
		TenantID:     tenantID,
		UserID:       userID,
		DataDir:      s.userDataRoot(tenantID, userID),
		Quota:        effectiveQuota,
		Instances:    len(instances),
		RunsByStatus: map[RunStatus]int{},
	}
	now := s.now().UTC()
	credentialExpiryCutoff := now.Add(7 * 24 * time.Hour)
	credentials, err := s.store.ListCredentials(tenantID, userID)
	if err != nil {
		return nil, err
	}
	for _, cred := range credentials {
		summary.Credentials++
		switch credentialStatus(cred) {
		case CredentialStatusSuspended:
			summary.SuspendedCredentials++
		case CredentialStatusRevoked:
			summary.RevokedCredentials++
		default:
			summary.ActiveCredentials++
		}
		if cred.ExpiresAt != nil {
			expiresAt := cred.ExpiresAt.UTC()
			if !expiresAt.After(now) {
				summary.ExpiredCredentials++
			} else if !expiresAt.After(credentialExpiryCutoff) {
				summary.ExpiringCredentials++
			}
		}
	}
	for _, inst := range instances {
		inst = s.withInstanceReadiness(inst)
		if inst.Ready {
			summary.ReadyInstances++
		}
		if inst.Status == InstanceStatusStopped {
			summary.StoppedInstances++
		}
		summary.LastActivityAt = laterTime(summary.LastActivityAt, inst.UpdatedAt)

		sessions, err := s.store.ListSessions(tenantID, userID, inst.ID)
		if err != nil {
			return nil, err
		}
		summary.Sessions += len(sessions)
		for _, sess := range sessions {
			summary.LastActivityAt = laterTime(summary.LastActivityAt, sess.UpdatedAt)
			messages, err := s.store.ListMessages(sess.ID)
			if err != nil {
				return nil, err
			}
			summary.Messages += len(messages)
			for _, msg := range messages {
				summary.LastActivityAt = laterTime(summary.LastActivityAt, msg.CreatedAt)
				switch msg.Role {
				case MessageRoleUser:
					summary.UserMessages++
				case MessageRoleAssistant:
					summary.AssistantMessages++
				}
			}
		}

		runs, err := s.store.ListRuns(tenantID, userID, inst.ID)
		if err != nil {
			return nil, err
		}
		summary.Runs += len(runs)
		for _, run := range runs {
			summary.RunsByStatus[run.Status]++
			summary.LastActivityAt = laterTime(summary.LastActivityAt, run.StartedAt)
			if run.CompletedAt != nil {
				summary.LastActivityAt = laterTime(summary.LastActivityAt, *run.CompletedAt)
			}
		}
	}
	summary.QuotaUsage = buildQuotaUsageSnapshot(summary.Quota, &summary)
	return &summary, nil
}

func (s *Service) GetInstanceSummary(ctx context.Context, p Principal, instanceID string) (*InstanceSummary, error) {
	_ = ctx
	inst, err := s.store.GetInstance(p.TenantID, p.UserID, instanceID)
	if err != nil {
		return nil, err
	}
	inst = s.withInstanceReadiness(inst)
	summary := InstanceSummary{
		InstanceID:   inst.ID,
		TenantID:     inst.TenantID,
		UserID:       inst.UserID,
		Status:       inst.Status,
		Ready:        inst.Ready,
		ReadyReason:  inst.ReadyReason,
		RunsByStatus: map[RunStatus]int{},
	}
	summary.LastActivityAt = laterTime(summary.LastActivityAt, inst.UpdatedAt)

	sessions, err := s.store.ListSessions(p.TenantID, p.UserID, instanceID)
	if err != nil {
		return nil, err
	}
	summary.Sessions = len(sessions)
	for _, sess := range sessions {
		if sess.Archived {
			summary.ArchivedSessions++
		}
		if sess.Metadata != nil && sess.Metadata[sessionMetaPendingAskUser] == "true" {
			summary.WaitingSessions++
		}
		summary.LastActivityAt = laterTime(summary.LastActivityAt, sess.UpdatedAt)
		messages, err := s.store.ListMessages(sess.ID)
		if err != nil {
			return nil, err
		}
		summary.Messages += len(messages)
		for _, msg := range messages {
			summary.LastActivityAt = laterTime(summary.LastActivityAt, msg.CreatedAt)
			switch msg.Role {
			case MessageRoleUser:
				summary.UserMessages++
			case MessageRoleAssistant:
				summary.AssistantMessages++
			}
		}
	}

	runs, err := s.store.ListRuns(p.TenantID, p.UserID, instanceID)
	if err != nil {
		return nil, err
	}
	summary.Runs = len(runs)
	for _, run := range runs {
		summary.RunsByStatus[run.Status]++
		if run.WaitingForUser {
			summary.WaitingRuns++
		}
		summary.LastActivityAt = laterTime(summary.LastActivityAt, run.StartedAt)
		if run.CompletedAt != nil {
			summary.LastActivityAt = laterTime(summary.LastActivityAt, *run.CompletedAt)
		}
	}
	return &summary, nil
}

func (s *Service) hasRunningRuns(tenantID, userID, instanceID, sessionID string) (bool, error) {
	runs, err := s.store.ListRuns(tenantID, userID, instanceID)
	if err != nil {
		return false, err
	}
	for _, run := range runs {
		if sessionID != "" && run.SessionID != sessionID {
			continue
		}
		if run.Status == RunStatusRunning {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) collectRunningRunBlockers(tenantID, userID string) ([]DeleteBlocker, error) {
	instances, err := s.store.ListInstances(tenantID, userID)
	if err != nil {
		return nil, err
	}
	blockers := make([]DeleteBlocker, 0)
	for _, inst := range instances {
		runs, err := s.store.ListRuns(tenantID, userID, inst.ID)
		if err != nil {
			return nil, err
		}
		for _, run := range runs {
			if run.Status != RunStatusRunning {
				continue
			}
			blockers = append(blockers, DeleteBlocker{
				Kind:       "running_run",
				TenantID:   tenantID,
				UserID:     userID,
				InstanceID: inst.ID,
				SessionID:  run.SessionID,
				RunID:      run.ID,
				Reason:     "instance has a running run",
			})
		}
	}
	return blockers, nil
}

func (s *Service) enrichSessions(items []Session) ([]Session, error) {
	out := make([]Session, 0, len(items))
	for _, item := range items {
		enriched, err := s.enrichSession(item)
		if err != nil {
			return nil, err
		}
		out = append(out, enriched)
	}
	return out, nil
}

func (s *Service) enrichSession(sess Session) (Session, error) {
	sess.WaitingForUser = sess.Metadata != nil && sess.Metadata[sessionMetaPendingAskUser] == "true"
	if sess.WaitingForUser {
		sess.PendingAsk = &SessionPendingAsk{
			Question:  strings.TrimSpace(sess.Metadata[sessionMetaPendingAskUserQuestion]),
			InputType: strings.TrimSpace(sess.Metadata[sessionMetaPendingAskUserInputType]),
			Options:   parsePendingAskUserOptions(sess.Metadata[sessionMetaPendingAskUserOptions]),
		}
	}
	messages, err := s.store.ListMessages(sess.ID)
	if err != nil {
		return Session{}, err
	}
	if len(messages) > 0 {
		last := messages[len(messages)-1].CreatedAt
		sess.LastMessageAt = &last
	}
	return sess, nil
}

func (s *Service) enrichRuns(items []Run) ([]Run, error) {
	out := make([]Run, 0, len(items))
	for _, item := range items {
		enriched, err := s.enrichRun(item)
		if err != nil {
			return nil, err
		}
		out = append(out, enriched)
	}
	return out, nil
}

func (s *Service) enrichRun(run Run) (Run, error) {
	if run.CompletedAt != nil {
		run.DurationMs = run.CompletedAt.Sub(run.StartedAt).Milliseconds()
	}
	if strings.TrimSpace(run.AssistantMessageID) == "" {
		return run, nil
	}
	messages, err := s.store.ListMessages(run.SessionID)
	if err != nil {
		return Run{}, err
	}
	for _, msg := range messages {
		if msg.ID != run.AssistantMessageID {
			continue
		}
		if msg.Metadata != nil {
			run.ResponseSource = strings.TrimSpace(msg.Metadata[metaResponseSource])
			run.WaitingForUser = normalizeResponseSourceKind(run.ResponseSource).IsWaitingForUser()
		}
		break
	}
	return run, nil
}

func (s *Service) userRoot(tenantID, userID string) string {
	return filepath.Join(s.dataRoot, "tenants", slugID(tenantID), "users", slugID(userID))
}
func (s *Service) userDataRoot(tenantID, userID string) string {
	return filepath.Join(s.userRoot(tenantID, userID), "data")
}
func (s *Service) userConfigPath(tenantID, userID string) string {
	return filepath.Join(s.userRoot(tenantID, userID), "config", "app_config.json")
}

func (s *Service) getOrLoadUserConfig(tenantID, userID string) (UserConfig, error) {
	cfg, err := s.getOrLoadRawUserConfig(tenantID, userID)
	if err != nil {
		return UserConfig{}, err
	}
	cfg.AppConfig = s.applySharedClientAppConfig(cfg.AppConfig)
	if shared, ok, err := s.loadDefaultClientConfigIfExists(); err == nil && ok && shared.UpdatedAt.After(cfg.UpdatedAt) {
		cfg.UpdatedAt = shared.UpdatedAt
	}
	return cfg, nil
}

func (s *Service) getOrLoadRawUserConfig(tenantID, userID string) (UserConfig, error) {
	cfg, err := s.store.GetUserConfig(tenantID, userID)
	if err == nil {
		return cfg, nil
	}
	if err != ErrUserConfigNotFound {
		return UserConfig{}, err
	}
	cfg, loadErr := loadUserConfigFromFile(s.userConfigPath(tenantID, userID))
	if loadErr != nil {
		return UserConfig{}, ErrUserConfigNotFound
	}
	if cfg.TenantID == "" {
		cfg.TenantID = tenantID
	}
	if cfg.UserID == "" {
		cfg.UserID = userID
	}
	_ = s.store.SaveUserConfig(cfg)
	return cfg, nil
}

func (s *Service) loadDefaultClientConfigIfExists() (UserConfig, bool, error) {
	cfg, err := loadUserConfigFromFile(s.defaultClientConfigPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return UserConfig{}, false, nil
		}
		return UserConfig{}, false, err
	}
	cfg.TenantID = ""
	cfg.UserID = ""
	return cfg, true, nil
}

func (s *Service) applySharedClientAppConfig(userCfg corelib.AppConfig) corelib.AppConfig {
	shared, ok, err := s.loadDefaultClientConfigIfExists()
	if err != nil || !ok {
		return userCfg
	}
	return mergeSharedClientAppConfig(userCfg, shared.AppConfig)
}

func mergeSharedClientAppConfig(userCfg, shared corelib.AppConfig) corelib.AppConfig {
	out := cloneAppConfig(userCfg)
	shared = cloneAppConfig(shared)
	out.WebSearchProviders = shared.WebSearchProviders
	out.WebSearchCurrentProvider = shared.WebSearchCurrentProvider
	out.DefaultProxyEnabled = shared.DefaultProxyEnabled
	out.DefaultProxyProtocol = shared.DefaultProxyProtocol
	out.DefaultProxyHost = shared.DefaultProxyHost
	out.DefaultProxyPort = shared.DefaultProxyPort
	out.DefaultProxyUsername = shared.DefaultProxyUsername
	out.DefaultProxyPassword = shared.DefaultProxyPassword
	out.DefaultProxyBypass = shared.DefaultProxyBypass
	out.DefaultProxyScopeMaclaw = shared.DefaultProxyScopeMaclaw
	out.DefaultProxyScopeCodingTools = shared.DefaultProxyScopeCodingTools
	out.DefaultProxyScopeAgent = shared.DefaultProxyScopeAgent
	out.MCPServers = shared.MCPServers
	out.LocalMCPServers = shared.LocalMCPServers
	out.SkillHubURLs = shared.SkillHubURLs
	out.ExternalSkillDirs = append([]string(nil), shared.ExternalSkillDirs...)
	out.SecurityPolicyMode = shared.SecurityPolicyMode
	out.HubSecurityCentralized = shared.HubSecurityCentralized
	out.NetworkLevel = shared.NetworkLevel
	out.NetworkAllowlist = append([]string(nil), shared.NetworkAllowlist...)
	out.SkillSourcesAllowed = append([]string(nil), shared.SkillSourcesAllowed...)
	out.Language = shared.Language
	out.UIMode = shared.UIMode
	out.WorkingDirectory = shared.WorkingDirectory
	out.VectorSearchEnabled = shared.VectorSearchEnabled
	out.ASREnabled = shared.ASREnabled
	out.TTSEnabled = shared.TTSEnabled
	out.IMProgressNudgeEnabled = shared.IMProgressNudgeEnabled
	out.SSHHosts = shared.SSHHosts
	return out
}

func SharedClientAppConfigOnly(cfg corelib.AppConfig) corelib.AppConfig {
	cfg = cloneAppConfig(cfg)
	return corelib.AppConfig{
		WebSearchProviders:           cfg.WebSearchProviders,
		WebSearchCurrentProvider:     cfg.WebSearchCurrentProvider,
		DefaultProxyEnabled:          cfg.DefaultProxyEnabled,
		DefaultProxyProtocol:         cfg.DefaultProxyProtocol,
		DefaultProxyHost:             cfg.DefaultProxyHost,
		DefaultProxyPort:             cfg.DefaultProxyPort,
		DefaultProxyUsername:         cfg.DefaultProxyUsername,
		DefaultProxyPassword:         cfg.DefaultProxyPassword,
		DefaultProxyBypass:           cfg.DefaultProxyBypass,
		DefaultProxyScopeMaclaw:      cfg.DefaultProxyScopeMaclaw,
		DefaultProxyScopeCodingTools: cfg.DefaultProxyScopeCodingTools,
		DefaultProxyScopeAgent:       cfg.DefaultProxyScopeAgent,
		MCPServers:                   cfg.MCPServers,
		LocalMCPServers:              cfg.LocalMCPServers,
		SkillHubURLs:                 cfg.SkillHubURLs,
		ExternalSkillDirs:            cfg.ExternalSkillDirs,
		SecurityPolicyMode:           cfg.SecurityPolicyMode,
		HubSecurityCentralized:       cfg.HubSecurityCentralized,
		NetworkLevel:                 cfg.NetworkLevel,
		NetworkAllowlist:             cfg.NetworkAllowlist,
		SkillSourcesAllowed:          cfg.SkillSourcesAllowed,
		Language:                     cfg.Language,
		UIMode:                       cfg.UIMode,
		WorkingDirectory:             cfg.WorkingDirectory,
		VectorSearchEnabled:          cfg.VectorSearchEnabled,
		ASREnabled:                   cfg.ASREnabled,
		TTSEnabled:                   cfg.TTSEnabled,
		IMProgressNudgeEnabled:       cfg.IMProgressNudgeEnabled,
		SSHHosts:                     cfg.SSHHosts,
	}
}

func (s *Service) resolveCandidateConfig(p Principal, next *corelib.AppConfig) (corelib.AppConfig, error) {
	if _, err := s.store.GetUser(p.TenantID, p.UserID); err != nil {
		return corelib.AppConfig{}, err
	}
	current, err := s.getOrLoadRawUserConfig(p.TenantID, p.UserID)
	if err != nil {
		if err != ErrUserConfigNotFound {
			return corelib.AppConfig{}, err
		}
		current = UserConfig{TenantID: p.TenantID, UserID: p.UserID, AppConfig: corelib.AppConfig{}}
	}
	if next == nil {
		return effectiveLLMFlatConfig(s.applySharedClientAppConfig(current.AppConfig)), nil
	}
	merged := normalizeLLMConfigForSave(current.AppConfig, mergeSecretPreserving(current.AppConfig, *next))
	return s.applySharedClientAppConfig(merged), nil
}

func (s *Service) withInstanceReadiness(inst Instance) Instance {
	if validation, err := s.currentInstanceConfigValidation(inst.TenantID, inst.UserID); err == nil {
		return s.withInstanceReadinessValidation(inst, validation)
	}
	inst.Readiness = s.buildInstanceReadiness(inst)
	inst.Ready = inst.Readiness.Ready
	inst.ReadyReason = inst.Readiness.Reason
	return inst
}

func (s *Service) withInstanceReadinessValidation(inst Instance, validation ConfigValidationResult) Instance {
	inst.ConfigValidation = validation
	inst.Readiness = s.buildInstanceReadiness(inst)
	inst.Ready = inst.Readiness.Ready
	inst.ReadyReason = inst.Readiness.Reason
	return inst
}

func (s *Service) currentInstanceConfigValidation(tenantID, userID string) (ConfigValidationResult, error) {
	cfg, err := s.getOrLoadUserConfig(tenantID, userID)
	if err != nil {
		return ConfigValidationResult{}, err
	}
	cfg.AppConfig = effectiveLLMFlatConfig(cfg.AppConfig)
	return ValidateAppConfig(cfg.AppConfig), nil
}

func (s *Service) buildInstanceReadiness(inst Instance) InstanceReadiness {
	readiness := InstanceReadiness{
		Ready:       false,
		Reason:      "instance is not ready",
		ConfigValid: inst.ConfigValidation.Valid,
	}
	if _, err := os.Stat(inst.RuntimeDir); err != nil {
		readiness.Reason = fmt.Sprintf("runtime directory is not accessible: %v", err)
		return readiness
	}
	if _, err := os.Stat(inst.DataDir); err != nil {
		readiness.Reason = fmt.Sprintf("shared data directory is not accessible: %v", err)
		return readiness
	}
	readiness.HasLLMConfig = inst.ConfigValidation.Valid
	if inst.Status != InstanceStatusReady {
		readiness.Reason = fmt.Sprintf("instance status is %s", inst.Status)
		return readiness
	}
	if !inst.ConfigValidation.Valid {
		readiness.Reason = "user LLM configuration is incomplete"
		return readiness
	}
	readiness.Ready = true
	readiness.Reason = "instance is ready"
	return readiness
}

var (
	auditExportSecretKeyPattern    = auditExportSecretKeyRegexpPattern()
	safePathPart                   = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)
	auditExportInlineSecretPattern = regexp.MustCompile(`(?i)(` + auditExportSecretKeyPattern + `)(\s*[:=]\s*)([^\s,;]+)`)
	auditExportBearerPattern       = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`)
	auditExportJSONSecretPattern   = regexp.MustCompile(`(?i)("?(?:` + auditExportSecretKeyPattern + `)"?\s*:\s*)"[^"]*"`)
)

func slugID(v string) string { return safePathPart.ReplaceAllString(v, "_") }
func cloneMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
func defaultString(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
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

func redactAuditEventsForExport(dataRoot string, events []AuditEvent) []AuditEvent {
	if len(events) == 0 {
		return nil
	}
	out := make([]AuditEvent, len(events))
	for i, event := range events {
		event.ResourceID = redactAuditExportValue(dataRoot, event.ResourceID)
		if len(event.Metadata) > 0 {
			event.Metadata = redactAuditExportMetadata(dataRoot, event.Metadata)
		}
		out[i] = event
	}
	return out
}

func redactAuditExportMetadata(dataRoot string, metadata map[string]string) map[string]string {
	out := make(map[string]string, len(metadata))
	for key, value := range metadata {
		if auditExportSensitiveKey(key) {
			out[key] = "[redacted]"
			continue
		}
		out[key] = redactAuditExportValue(dataRoot, value)
	}
	return out
}

func auditExportSensitiveKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	for _, marker := range auditExportSecretKeyMarkers() {
		if strings.Contains(key, marker) {
			return true
		}
	}
	return auditExportAuthSecretKey(key)
}

func auditExportAuthSecretKey(key string) bool {
	key = strings.NewReplacer("-", "_", ".", "_", " ", "_").Replace(strings.ToLower(strings.TrimSpace(key)))
	switch key {
	case "auth", "authentication", "auth_header", "authheader", "authentication_header":
		return true
	default:
		return false
	}
}

func auditExportSecretKeyMarkers() []string {
	return auditExportSecretKeyMarkersList[:]
}

var auditExportSecretKeyMarkersList = [...]string{"secret", "token", "password", "passwd", "authorization", "cookie", "bearer", "private", "api_key", "api-key", "apikey", "api_secret", "api-secret", "apisecret"}

func auditExportSecretKeyRegexpPattern() string {
	parts := make([]string, 0, len(auditExportSecretKeyMarkersList)+3)
	for _, marker := range auditExportSecretKeyMarkersList {
		parts = append(parts, regexp.QuoteMeta(marker))
	}
	parts = append(parts, `api[-_\s]?key`, `api[-_\s]?secret`, `auth`)
	sort.Slice(parts, func(i, j int) bool {
		return len(parts[i]) > len(parts[j])
	})
	return strings.Join(parts, "|")
}

func redactAuditExportValue(dataRoot, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	value = redactAuditExportText(dataRoot, value)
	if filepath.IsAbs(value) {
		return filepath.Base(value)
	}
	return value
}

func redactAuditExportText(dataRoot, text string) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	if dataRoot = strings.TrimSpace(dataRoot); dataRoot != "" {
		base := auditExportPathBase(dataRoot)
		for _, variant := range auditExportPathRedactionVariants(dataRoot) {
			text = strings.ReplaceAll(text, variant, base)
		}
	}
	text = auditExportBearerPattern.ReplaceAllString(text, "Bearer [redacted]")
	text = auditExportJSONSecretPattern.ReplaceAllString(text, `${1}"[redacted]"`)
	return auditExportInlineSecretPattern.ReplaceAllString(text, `${1}${2}[redacted]`)
}

func auditExportPathBase(value string) string {
	if value = strings.TrimSpace(value); value == "" {
		return value
	}
	base := filepath.Base(value)
	if base != value {
		return base
	}
	return path.Base(strings.ReplaceAll(value, `\`, "/"))
}

func auditExportPathRedactionVariants(value string) []string {
	seen := map[string]bool{}
	variants := make([]string, 0, 6)
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			return
		}
		seen[v] = true
		variants = append(variants, v)
	}
	add(value)
	add(filepath.ToSlash(value))
	add(filepath.FromSlash(value))
	for _, v := range append([]string(nil), variants...) {
		add(strings.ReplaceAll(v, `\`, `\\`))
	}
	return variants
}
func maskAPIKeyString(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if len(v) <= 6 {
		return "******"
	}
	return v[:3] + "***" + v[len(v)-3:]
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

func (s *Service) recordAudit(in auditRecord) error {
	return s.store.SaveAuditEvent(AuditEvent{
		ID:           NewID("audit"),
		TenantID:     strings.TrimSpace(in.TenantID),
		UserID:       strings.TrimSpace(in.UserID),
		ActorType:    defaultString(strings.TrimSpace(in.ActorType), "system"),
		ActorTenant:  strings.TrimSpace(in.ActorTenantID),
		ActorUser:    strings.TrimSpace(in.ActorUserID),
		Action:       strings.TrimSpace(in.Action),
		ResourceType: strings.TrimSpace(in.ResourceType),
		ResourceID:   strings.TrimSpace(in.ResourceID),
		Metadata:     cloneMap(in.Metadata),
		CreatedAt:    s.now(),
	})
}

// RecordAuditEvent stores an externally assembled audit event after applying
// the same defaults used by internal service audit records.
func (s *Service) RecordAuditEvent(ctx context.Context, event AuditEvent) error {
	_ = ctx
	if strings.TrimSpace(event.ID) == "" {
		event.ID = NewID("audit")
	}
	event.TenantID = strings.TrimSpace(event.TenantID)
	event.UserID = strings.TrimSpace(event.UserID)
	event.ActorType = defaultString(strings.TrimSpace(event.ActorType), "system")
	event.ActorTenant = strings.TrimSpace(event.ActorTenant)
	event.ActorUser = strings.TrimSpace(event.ActorUser)
	event.Action = strings.TrimSpace(event.Action)
	event.ResourceType = strings.TrimSpace(event.ResourceType)
	event.ResourceID = strings.TrimSpace(event.ResourceID)
	if event.CreatedAt.IsZero() {
		event.CreatedAt = s.now()
	}
	return s.store.SaveAuditEvent(event)
}

func laterTime(current *time.Time, candidate time.Time) *time.Time {
	if candidate.IsZero() {
		return current
	}
	if current == nil || candidate.After(*current) {
		next := candidate
		return &next
	}
	return current
}
