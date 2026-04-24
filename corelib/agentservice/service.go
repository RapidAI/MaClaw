package agentservice

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

type Config struct {
	DataRoot         string
	TokenSecret      string
	TokenTTL         time.Duration
	CredentialPepper string
}

type Service struct {
	store            Store
	executor         Executor
	tokens           *TokenManager
	dataRoot         string
	credentialPepper string
	now              func() time.Time

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
	return &Service{store: store, executor: executor, tokens: NewTokenManager(cfg.TokenSecret, cfg.TokenTTL), dataRoot: cfg.DataRoot, credentialPepper: cfg.CredentialPepper, now: time.Now}, nil
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
	t := Tenant{ID: NewID("tenant"), Name: name, Status: TenantStatusActive, CreatedAt: now, UpdatedAt: now}
	if err := s.store.SaveTenant(t); err != nil {
		return nil, err
	}
	if err := secureMkdirAll(filepath.Join(s.dataRoot, "tenants", slugID(t.ID))); err != nil {
		return nil, err
	}
	_ = s.recordAudit(auditRecord{TenantID: t.ID, Action: "tenant.created", ResourceType: "tenant", ResourceID: t.ID, ActorType: "admin"})
	return &t, nil
}

func (s *Service) ListTenants(ctx context.Context) ([]Tenant, error) {
	_ = ctx
	return s.store.ListTenants()
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
	t.UpdatedAt = s.now()
	if err := s.store.SaveTenant(t); err != nil {
		return nil, err
	}
	_ = s.recordAudit(auditRecord{TenantID: t.ID, Action: "tenant.updated", ResourceType: "tenant", ResourceID: t.ID, ActorType: "admin", Metadata: map[string]string{"status": string(t.Status)}})
	return &t, nil
}

func (s *Service) CreateUser(ctx context.Context, in CreateUserInput) (*User, error) {
	_ = ctx
	if _, err := s.store.GetTenant(in.TenantID); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	now := s.now()
	u := User{ID: NewID("user"), TenantID: in.TenantID, Name: name, Email: strings.TrimSpace(in.Email), Status: UserStatusActive, CreatedAt: now, UpdatedAt: now}
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
	if err := writeRuntimeConfig(s.userDataRoot(in.TenantID, u.ID), defaultCfg.AppConfig); err != nil {
		return nil, err
	}
	_ = s.recordAudit(auditRecord{TenantID: u.TenantID, UserID: u.ID, Action: "user.created", ResourceType: "user", ResourceID: u.ID, ActorType: "admin"})
	return &u, nil
}

func (s *Service) ListUsers(ctx context.Context, tenantID string) ([]User, error) {
	_ = ctx
	if _, err := s.store.GetTenant(tenantID); err != nil {
		return nil, err
	}
	return s.store.ListUsers(tenantID)
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
	u.UpdatedAt = s.now()
	if err := s.store.SaveUser(u); err != nil {
		return nil, err
	}
	_ = s.recordAudit(auditRecord{TenantID: u.TenantID, UserID: u.ID, Action: "user.updated", ResourceType: "user", ResourceID: u.ID, ActorType: "admin", Metadata: map[string]string{"status": string(u.Status)}})
	return &u, nil
}

func (s *Service) CreateCredential(ctx context.Context, in CreateCredentialInput) (*Credential, error) {
	_ = ctx
	if _, err := s.store.GetTenant(in.TenantID); err != nil {
		return nil, err
	}
	if _, err := s.store.GetUser(in.TenantID, in.UserID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.APIKey) == "" || strings.TrimSpace(in.APISecret) == "" {
		return nil, fmt.Errorf("api_key and api_secret are required")
	}
	now := s.now()
	apiKey := strings.TrimSpace(in.APIKey)
	digest := HashSecretWithPepper(in.APISecret, s.credentialPepper)
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
		SecretDigest: digest,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.store.SaveCredential(stored); err != nil {
		return nil, err
	}
	_ = s.recordAudit(auditRecord{TenantID: stored.TenantID, UserID: stored.UserID, Action: "credential.created", ResourceType: "credential", ResourceID: stored.ID, ActorType: "admin"})
	response := stored
	response.APIKey = apiKey
	response.APIKeyHash = ""
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
	if err := s.ensurePrincipalActive(cred.TenantID, cred.UserID); err != nil {
		return nil, err
	}
	p := Principal{TenantID: cred.TenantID, UserID: cred.UserID, Roles: []string{"user"}}
	token, exp, err := s.tokens.IssueForCredential(p, cred.ID)
	if err != nil {
		return nil, err
	}
	_ = s.recordAudit(auditRecord{TenantID: cred.TenantID, UserID: cred.UserID, Action: "auth.token_issued", ResourceType: "credential", ResourceID: cred.ID, ActorType: "credential"})
	return &IssueTokenOutput{AccessToken: token, TokenType: "Bearer", ExpiresAt: exp, Principal: p}, nil
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
	p, _, credentialID, err := s.tokens.Parse(accessToken)
	if err != nil {
		return nil, ErrUnauthorized
	}
	if strings.TrimSpace(credentialID) != "" {
		cred, credErr := s.store.GetCredential(p.TenantID, p.UserID, credentialID)
		if credErr != nil || credentialStatus(cred) != CredentialStatusActive {
			return nil, ErrUnauthorized
		}
	}
	if err := s.ensurePrincipalActive(p.TenantID, p.UserID); err != nil {
		return nil, err
	}
	return p, nil
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
	cfg.AppConfig = SanitizeAppConfig(cfg.AppConfig)
	return &cfg, nil
}

func (s *Service) UpdateUserConfig(ctx context.Context, p Principal, next corelib.AppConfig) (*UserConfig, error) {
	_ = ctx
	if _, err := s.store.GetUser(p.TenantID, p.UserID); err != nil {
		return nil, err
	}
	current, err := s.getOrLoadUserConfig(p.TenantID, p.UserID)
	if err != nil && err != ErrUserConfigNotFound {
		return nil, err
	}
	merged := mergeSecretPreserving(current.AppConfig, next)
	cfg := UserConfig{TenantID: p.TenantID, UserID: p.UserID, AppConfig: merged, UpdatedAt: s.now()}
	if err := s.store.SaveUserConfig(cfg); err != nil {
		return nil, err
	}
	if err := saveUserConfigToFile(s.userConfigPath(p.TenantID, p.UserID), cfg); err != nil {
		return nil, err
	}
	if err := writeRuntimeConfig(s.userDataRoot(p.TenantID, p.UserID), cfg.AppConfig); err != nil {
		return nil, err
	}
	_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "config.updated", ResourceType: "user_config", ResourceID: p.UserID, ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID})
	cfg.AppConfig = SanitizeAppConfig(cfg.AppConfig)
	return &cfg, nil
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
	validation, err := s.ValidateUserConfig(ctx, p)
	if err != nil {
		return nil, err
	}
	if !validation.Valid {
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
		ConfigPath:            runtimeConfigPath(inst.DataDir),
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
	for i := range items {
		items[i] = s.withInstanceReadiness(items[i])
	}
	return items, nil
}

func (s *Service) CreateSession(ctx context.Context, p Principal, instanceID string, in CreateSessionInput) (*Session, error) {
	_ = ctx
	if _, err := s.store.GetInstance(p.TenantID, p.UserID, instanceID); err != nil {
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
	content := strings.TrimSpace(in.Content)
	if content == "" {
		return nil, nil, fmt.Errorf("content is required")
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
	res, execErr := s.executor.Execute(execCtx, ExecuteRequest{Principal: p, Tenant: tenant, User: user, Instance: inst, Session: sess, Message: execMsg, History: history, DataDir: inst.DataDir, Config: cfg.AppConfig})
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
	run.WaitingForUser = run.ResponseSource == "ask_user"
	if err := s.store.SaveRun(run); err != nil {
		return nil, nil, err
	}
	if pendingAskReq != nil {
		sess.Metadata = clearPendingAskUserMetadata(sess.Metadata)
	}
	if res != nil && res.Metadata != nil && res.Metadata[metaResponseSource] == "ask_user" {
		sess.Metadata = setPendingAskUserMetadata(sess.Metadata, res.Metadata)
	}
	sess.UpdatedAt = completed
	_ = s.store.SaveSession(sess)
	_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "run.succeeded", ResourceType: "run", ResourceID: run.ID, ActorType: "system", ActorTenantID: p.TenantID, ActorUserID: p.UserID, Metadata: map[string]string{"instance_id": instanceID, "session_id": sessionID, "assistant_message_id": assistant.ID}})
	return &run, &assistant, nil
}

func (s *Service) ListAuditEvents(ctx context.Context, in ListAuditEventsInput) ([]AuditEvent, error) {
	_ = ctx
	items, err := s.store.ListAuditEvents(strings.TrimSpace(in.TenantID), strings.TrimSpace(in.UserID))
	if err != nil {
		return nil, err
	}
	action := strings.TrimSpace(in.Action)
	resourceType := strings.TrimSpace(in.ResourceType)
	if action == "" && resourceType == "" {
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

func (s *Service) GetUsageSummary(ctx context.Context, p Principal) (*UsageSummary, error) {
	_ = ctx
	if _, err := s.store.GetUser(p.TenantID, p.UserID); err != nil {
		return nil, err
	}
	instances, err := s.store.ListInstances(p.TenantID, p.UserID)
	if err != nil {
		return nil, err
	}
	summary := UsageSummary{
		TenantID:     p.TenantID,
		UserID:       p.UserID,
		DataDir:      s.userDataRoot(p.TenantID, p.UserID),
		Instances:    len(instances),
		RunsByStatus: map[RunStatus]int{},
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

		sessions, err := s.store.ListSessions(p.TenantID, p.UserID, inst.ID)
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

		runs, err := s.store.ListRuns(p.TenantID, p.UserID, inst.ID)
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
			run.WaitingForUser = run.ResponseSource == "ask_user"
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

func (s *Service) resolveCandidateConfig(p Principal, next *corelib.AppConfig) (corelib.AppConfig, error) {
	if _, err := s.store.GetUser(p.TenantID, p.UserID); err != nil {
		return corelib.AppConfig{}, err
	}
	current, err := s.getOrLoadUserConfig(p.TenantID, p.UserID)
	if err != nil {
		if err != ErrUserConfigNotFound {
			return corelib.AppConfig{}, err
		}
		current = UserConfig{TenantID: p.TenantID, UserID: p.UserID, AppConfig: corelib.AppConfig{}}
	}
	if next == nil {
		return current.AppConfig, nil
	}
	merged := mergeSecretPreserving(current.AppConfig, *next)
	return merged, nil
}

func (s *Service) withInstanceReadiness(inst Instance) Instance {
	inst.Readiness = s.buildInstanceReadiness(inst)
	inst.Ready = inst.Readiness.Ready
	inst.ReadyReason = inst.Readiness.Reason
	return inst
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

var safePathPart = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

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

func credentialStatus(cred Credential) CredentialStatus {
	if cred.Status == "" {
		return CredentialStatusActive
	}
	return cred.Status
}

func sanitizeCredential(cred Credential) Credential {
	cred.SecretDigest = ""
	cred.APIKeyHash = ""
	cred.APIKey = maskedAPIKey(cred)
	if cred.Status == "" {
		cred.Status = CredentialStatusActive
	}
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
