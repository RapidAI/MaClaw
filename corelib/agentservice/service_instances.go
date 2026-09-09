package agentservice

import (
	"context"
	"errors"
	"fmt"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"path/filepath"
	"strconv"
	"strings"
)

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
	snapshot, err := s.DescribeRuntimeCapabilities(ctx, p, instanceID)
	if err != nil {
		return nil, err
	}
	return LegacyCapabilitiesFromRuntime(snapshot), nil
}

// LegacyCapabilitiesFromRuntime maps the shared Runtime snapshot to the
// historical DTO kept for API compatibility. New callers should consume the
// snapshot directly; keeping this adapter here prevents the legacy endpoint
// from becoming a second capability source.
func LegacyCapabilitiesFromRuntime(snapshot agentruntime.CapabilitySnapshot) *AgentCapabilities {
	capabilities := &AgentCapabilities{
		Executor:          snapshot.Profile.Name,
		SupportsSessions:  snapshot.Profile.Capabilities["sessions"],
		SupportsAskUser:   snapshot.Profile.Capabilities["ask_user"],
		SupportsSSH:       snapshot.Profile.Capabilities["ssh"],
		SupportsLocalBash: snapshot.Profile.Capabilities["local_bash"],
		Metadata:          cloneMap(snapshot.Metadata),
		SurfaceDigest:     snapshot.SurfaceDigest,
	}
	if capabilities.Executor == "" {
		capabilities.Executor = "unknown"
	}
	// The historical contract always advertised sessions for a working
	// service, including executors that do not implement a custom describer.
	if _, ok := snapshot.Profile.Capabilities["sessions"]; !ok {
		capabilities.SupportsSessions = true
	}
	for _, tool := range snapshot.Tools {
		capabilities.Tools = append(capabilities.Tools, AgentToolCapability{
			Name: tool.Name, Description: tool.Description, Enabled: tool.Enabled,
			DisabledReason: tool.DisabledReason, Parameters: tool.Parameters,
		})
	}
	return capabilities
}

// DescribeRuntimeCapabilities exposes the transport-neutral capability
// snapshot produced by the shared Runtime facade. Unlike the legacy
// AgentCapabilities DTO, this contract carries module descriptors and the
// host profile used for the current authenticated instance.
func (s *Service) DescribeRuntimeCapabilities(ctx context.Context, p Principal, instanceID string) (agentruntime.CapabilitySnapshot, error) {
	if err := s.beginRequest(); err != nil {
		return agentruntime.CapabilitySnapshot{}, err
	}
	defer s.activeRequests.Done()
	if err := ctx.Err(); err != nil {
		return agentruntime.CapabilitySnapshot{}, err
	}
	if s == nil || s.store == nil {
		return agentruntime.CapabilitySnapshot{}, errors.New("service is unavailable")
	}
	inst, err := s.store.GetInstance(p.TenantID, p.UserID, instanceID)
	if err != nil {
		return agentruntime.CapabilitySnapshot{}, err
	}
	inst = s.withInstanceReadiness(inst)
	cfg, err := s.getOrLoadUserConfig(p.TenantID, p.UserID)
	if err != nil && err != ErrUserConfigNotFound {
		return agentruntime.CapabilitySnapshot{}, err
	}
	cfg.AppConfig = effectiveLLMFlatConfig(cfg.AppConfig)
	runtime := s.Runtime()
	if runtime == nil {
		return agentruntime.CapabilitySnapshot{}, errors.New("agent runtime is unavailable")
	}
	host := s.runtimeHostCapabilities()
	if host == nil {
		host = agentruntime.HeadlessHostCapabilities{}
	}
	snapshot, err := runtime.DescribeCapabilities(ctx, agentruntime.CapabilityRequest{
		Scope: agentruntime.Scope{TenantID: p.TenantID, UserID: p.UserID, InstanceID: instanceID},
		Host:  host,
		Input: ExecuteRequest{Principal: p, Instance: inst, DataDir: inst.DataDir, Config: cfg.AppConfig, Host: host},
	})
	if err != nil {
		return agentruntime.CapabilitySnapshot{}, err
	}
	if snapshot.ContractVersion == "" {
		snapshot.ContractVersion = agentruntime.ContractVersion
	}
	if snapshot.Profile.Name == "" {
		snapshot.Profile = host.Profile()
	}
	// Surface repository guarantees beside the Runtime module/tool contract.
	// These values are derived from the concrete Store at the composition root,
	// not from transport-specific code, so GUI and headless clients can make the
	// same decision about retries, replay and acknowledgement semantics.
	persistence := DescribeLifecyclePersistence(s.store)
	if snapshot.Metadata == nil {
		snapshot.Metadata = make(map[string]string, 8)
	}
	snapshot.Metadata[agentruntime.CapabilityMetadataLifecyclePersistenceMode] = persistence.Mode
	snapshot.Metadata[agentruntime.CapabilityMetadataDurableOutbox] = strconv.FormatBool(persistence.DurableOutbox)
	snapshot.Metadata[agentruntime.CapabilityMetadataAdmissionAtomic] = strconv.FormatBool(persistence.AdmissionAtomic)
	snapshot.Metadata[agentruntime.CapabilityMetadataAdmissionOutboxAtomic] = strconv.FormatBool(persistence.AdmissionOutboxAtomic)
	snapshot.Metadata[agentruntime.CapabilityMetadataCompletionAtomic] = strconv.FormatBool(persistence.CompletionAtomic)
	snapshot.Metadata[agentruntime.CapabilityMetadataCompletionOutboxAtomic] = strconv.FormatBool(persistence.CompletionOutboxAtomic)
	snapshot.Metadata[agentruntime.CapabilityMetadataTerminalOutboxAtomic] = strconv.FormatBool(persistence.TerminalOutboxAtomic)
	ratePolicy := s.RuntimeRateLimiter().Config()
	snapshot.Metadata[agentruntime.CapabilityMetadataRateLimitEnabled] = strconv.FormatBool(ratePolicy.Enabled)
	snapshot.Metadata[agentruntime.CapabilityMetadataRateLimitRate] = strconv.FormatFloat(ratePolicy.Rate, 'f', -1, 64)
	snapshot.Metadata[agentruntime.CapabilityMetadataRateLimitBurst] = strconv.Itoa(ratePolicy.Burst)
	snapshot.Metadata[agentruntime.CapabilityMetadataRateLimitTenantLimit] = strconv.Itoa(ratePolicy.TenantLimit)
	return snapshot, nil
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
