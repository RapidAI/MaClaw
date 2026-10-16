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
	// Revoke before persisting the new config. This intentionally favors safe
	// unavailability if a later config write fails: a changed endpoint or
	// credential must never retain an old trusted routing declaration.
	if err := s.dynamicCapabilities.ClearPrincipal(p); err != nil {
		return nil, fmt.Errorf("revoke dynamic capability contracts: %w", err)
	}
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
	// If the user already has their own LLM configuration (set by VE platform
	// or manually), do NOT overwrite it with the global shared config.
	// Only users without any LLM config inherit from the global settings.
	if !userHasOwnLLMConfig(userCfg) {
		out.MaclawLLMUrl = shared.MaclawLLMUrl
		out.MaclawLLMKey = shared.MaclawLLMKey
		out.MaclawLLMModel = shared.MaclawLLMModel
		out.MaclawLLMProtocol = shared.MaclawLLMProtocol
		out.MaclawLLMContextLength = shared.MaclawLLMContextLength
		out.MaclawLLMTimeoutSec = shared.MaclawLLMTimeoutSec
		out.MaclawLLMProviders = shared.MaclawLLMProviders
		out.MaclawLLMCurrentProvider = shared.MaclawLLMCurrentProvider
	}
	out.AgentResponseTimeoutSec = shared.AgentResponseTimeoutSec
	out.SkillRunnerTimeoutSec = shared.SkillRunnerTimeoutSec
	out.LLMPromptCache = shared.LLMPromptCache
	out.MaclawAgentMaxIterations = shared.MaclawAgentMaxIterations
	out.SubAgentConcurrency = shared.SubAgentConcurrency
	out.WebSearchProviders = shared.WebSearchProviders
	out.WebSearchCurrentProvider = shared.WebSearchCurrentProvider
	out.WebSearchStrategy = shared.WebSearchStrategy
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
	out.TTSVoiceID = shared.TTSVoiceID
	out.TTSAutoVoiceSummary = shared.TTSAutoVoiceSummary
	out.IMProgressNudgeEnabled = shared.IMProgressNudgeEnabled
	out.SSHHosts = shared.SSHHosts
	out.KnowledgeVisionLLM = shared.KnowledgeVisionLLM
	out.KnowledgeIncludeImages = shared.KnowledgeIncludeImages
	out.AuxiliaryLLM = shared.AuxiliaryLLM
	out.ModelRoutes = shared.ModelRoutes
	out.DailyLLMBudgetUSD = shared.DailyLLMBudgetUSD
	return out
}

// userHasOwnLLMConfig returns true when the user's config already has LLM
// settings — either via MaclawLLMProviders (set by VE platform) or via the
// flat fields (url+key+model). Such users should NOT have their LLM config
// overwritten by the global shared/default client config.
func userHasOwnLLMConfig(cfg corelib.AppConfig) bool {
	if len(cfg.MaclawLLMProviders) > 0 {
		return true
	}
	if strings.TrimSpace(cfg.MaclawLLMUrl) != "" && strings.TrimSpace(cfg.MaclawLLMKey) != "" && strings.TrimSpace(cfg.MaclawLLMModel) != "" {
		return true
	}
	return false
}

func SharedClientAppConfigOnly(cfg corelib.AppConfig) corelib.AppConfig {
	cfg = cloneAppConfig(cfg)
	return corelib.AppConfig{
		MaclawLLMUrl:                 cfg.MaclawLLMUrl,
		MaclawLLMKey:                 cfg.MaclawLLMKey,
		MaclawLLMModel:               cfg.MaclawLLMModel,
		MaclawLLMProtocol:            cfg.MaclawLLMProtocol,
		MaclawLLMContextLength:       cfg.MaclawLLMContextLength,
		MaclawLLMTimeoutSec:          cfg.MaclawLLMTimeoutSec,
		AgentResponseTimeoutSec:      cfg.AgentResponseTimeoutSec,
		SkillRunnerTimeoutSec:        cfg.SkillRunnerTimeoutSec,
		MaclawLLMProviders:           cfg.MaclawLLMProviders,
		MaclawLLMCurrentProvider:     cfg.MaclawLLMCurrentProvider,
		LLMPromptCache:               cfg.LLMPromptCache,
		MaclawAgentMaxIterations:     cfg.MaclawAgentMaxIterations,
		SubAgentConcurrency:          cfg.SubAgentConcurrency,
		WebSearchProviders:           cfg.WebSearchProviders,
		WebSearchCurrentProvider:     cfg.WebSearchCurrentProvider,
		WebSearchStrategy:            cfg.WebSearchStrategy,
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
		TTSVoiceID:                   cfg.TTSVoiceID,
		TTSEnabled:                   cfg.TTSEnabled,
		TTSAutoVoiceSummary:          cfg.TTSAutoVoiceSummary,
		IMProgressNudgeEnabled:       cfg.IMProgressNudgeEnabled,
		SSHHosts:                     cfg.SSHHosts,
		KnowledgeVisionLLM:           cfg.KnowledgeVisionLLM,
		KnowledgeIncludeImages:       cfg.KnowledgeIncludeImages,
		AuxiliaryLLM:                 cfg.AuxiliaryLLM,
		ModelRoutes:                  cfg.ModelRoutes,
		DailyLLMBudgetUSD:            cfg.DailyLLMBudgetUSD,
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
