// Package llmservice implements the HubCenter LLM service management —
// provider configuration, service group routing, tenant authorization,
// and usage statistics.
package llmservice

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

const registryKey = "llm_service_registry"

const DefaultComputeAgentID = "maclaw_official"
const DefaultComputeAgentName = "MaClaw官方"

const AccessPolicyFree = "free"
const AccessPolicyGrantRequired = "grant_required"

// ComputeAgent represents an upstream compute reseller/agent used for settlement.
type ComputeAgent struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Contact     string    `json:"contact,omitempty"`
	Settlement  string    `json:"settlement,omitempty"`
	Description string    `json:"description,omitempty"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"created_at,omitempty"`
	UpdatedAt   time.Time `json:"updated_at,omitempty"`
}

// Registry holds all LLM providers and service groups configured on HubCenter.
type Registry struct {
	Providers     []llmpool.ProviderConfig `json:"providers"`
	ServiceGroups []llmpool.ServiceGroup   `json:"service_groups"`
	Agents        []ComputeAgent           `json:"agents,omitempty"`
	UpdatedAt     time.Time                `json:"updated_at,omitempty"`
}

// Service manages the HubCenter LLM configuration and dispatching.
type Service struct {
	system   store.SystemSettingsRepository
	mu       sync.RWMutex
	cached   *Registry
	cachedAt time.Time
}

const registryCacheTTL = 30 * time.Second

// NewService creates a new LLM service manager.
func NewService(system store.SystemSettingsRepository) *Service {
	return &Service{system: system}
}

// LoadRegistry retrieves the current registry from the system settings store.
func (s *Service) LoadRegistry(ctx context.Context) (*Registry, error) {
	s.mu.RLock()
	if s.cached != nil && time.Since(s.cachedAt) < registryCacheTTL {
		defer s.mu.RUnlock()
		return s.cached, nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	// Double-check after acquiring write lock
	if s.cached != nil && time.Since(s.cachedAt) < registryCacheTTL {
		return s.cached, nil
	}

	raw, err := s.system.Get(ctx, registryKey)
	if err != nil {
		return nil, fmt.Errorf("load llm registry: %w", err)
	}
	reg := &Registry{}
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), reg); err != nil {
			return nil, fmt.Errorf("parse llm registry: %w", err)
		}
	}
	normalizeRegistry(reg)
	s.cached = reg
	s.cachedAt = time.Now()
	return reg, nil
}

// SaveRegistry persists the registry to the system settings store.
func (s *Service) SaveRegistry(ctx context.Context, reg *Registry) error {
	normalizeRegistry(reg)
	reg.UpdatedAt = time.Now().UTC()
	data, err := json.Marshal(reg)
	if err != nil {
		return fmt.Errorf("marshal llm registry: %w", err)
	}
	if err := s.system.Set(ctx, registryKey, string(data)); err != nil {
		return fmt.Errorf("save llm registry: %w", err)
	}
	s.mu.Lock()
	s.cached = reg
	s.cachedAt = time.Now()
	s.mu.Unlock()
	return nil
}

// Default queue wait timeout when a provider has MaxConcurrency > 0 but no
// explicit QueueTimeoutMS. MaxQueueWaiters is intentionally left alone:
// 0 means unlimited waiters in llmpool.ConcurrencyController (do not coerce).
const defaultProviderQueueTimeoutMS = 120000

func normalizeRegistry(reg *Registry) {
	if reg == nil {
		return
	}
	ensureDefaultComputeAgent(reg)
	for i := range reg.Providers {
		normalizeProviderGatewayLimits(&reg.Providers[i])
	}
	for i := range reg.ServiceGroups {
		normalizeServiceGroupModels(&reg.ServiceGroups[i])
		normalizeServiceGroupAgent(reg, &reg.ServiceGroups[i])
	}
}

func normalizeProviderGatewayLimits(provider *llmpool.ProviderConfig) {
	if provider == nil {
		return
	}
	// MaxConcurrency <= 0 remains unlimited (no queue needed).
	if provider.MaxConcurrency <= 0 {
		return
	}
	// Only fill missing queue timeout. MaxQueueWaiters=0 must stay 0 so the
	// concurrency controller keeps an unbounded waiter list.
	if provider.QueueTimeoutMS <= 0 {
		provider.QueueTimeoutMS = defaultProviderQueueTimeoutMS
	}
}

func ensureDefaultComputeAgent(reg *Registry) {
	for i := range reg.Agents {
		if reg.Agents[i].ID == DefaultComputeAgentID {
			if reg.Agents[i].Name == "" {
				reg.Agents[i].Name = DefaultComputeAgentName
			}
			reg.Agents[i].Enabled = true
			return
		}
	}
	now := time.Now().UTC()
	reg.Agents = append([]ComputeAgent{{
		ID:          DefaultComputeAgentID,
		Name:        DefaultComputeAgentName,
		Description: "MaClaw official compute settlement account",
		Enabled:     true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}}, reg.Agents...)
}

func normalizeServiceGroupAgent(reg *Registry, group *llmpool.ServiceGroup) {
	if group == nil {
		return
	}
	group.AccessPolicy = normalizeServiceGroupAccessPolicy(group.AccessPolicy)
	if group.AgentID == "" {
		group.AgentID = DefaultComputeAgentID
	}
	for _, agent := range reg.Agents {
		if agent.ID == group.AgentID {
			group.AgentName = agent.Name
			return
		}
	}
	group.AgentName = group.AgentID
}

func normalizeServiceGroupAccessPolicy(policy string) string {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case AccessPolicyGrantRequired:
		return AccessPolicyGrantRequired
	default:
		return AccessPolicyFree
	}
}

func normalizeServiceGroupModels(group *llmpool.ServiceGroup) {
	if group == nil {
		return
	}
	for i := range group.Models {
		model := &group.Models[i]
		configs := modelProviderConfigs(*model)
		normalized := make([]llmpool.ModelProviderConfig, 0, len(configs))
		providerIDs := make([]string, 0, len(configs))
		seenProviderIDs := map[string]struct{}{}
		for _, pc := range configs {
			providerID := strings.TrimSpace(pc.ProviderID)
			if providerID == "" {
				continue
			}
			pc.ProviderID = providerID
			pc.Model = strings.TrimSpace(pc.Model)
			normalized = append(normalized, pc)
			if _, ok := seenProviderIDs[providerID]; !ok {
				providerIDs = append(providerIDs, providerID)
				seenProviderIDs[providerID] = struct{}{}
			}
		}
		model.ProviderConfigs = normalized
		model.ProviderIDs = providerIDs
	}
}

func validateServiceGroupProviderRoutes(reg *Registry, group llmpool.ServiceGroup) error {
	for _, model := range group.Models {
		seen := map[string]struct{}{}
		for _, pc := range modelProviderConfigs(model) {
			providerID := strings.TrimSpace(pc.ProviderID)
			if providerID == "" {
				continue
			}
			key := providerID + "\x00" + effectiveProviderRouteModel(reg, pc)
			if _, ok := seen[key]; ok {
				modelName := strings.TrimSpace(model.Name)
				if modelName == "" {
					modelName = "auto"
				}
				upstreamModel := strings.TrimSpace(pc.Model)
				if upstreamModel == "" {
					upstreamModel = "(provider default)"
				}
				return fmt.Errorf("duplicate provider route in model %q: provider %q with upstream model %q", modelName, providerID, upstreamModel)
			}
			seen[key] = struct{}{}
		}
	}
	return nil
}

func effectiveProviderRouteModel(reg *Registry, pc llmpool.ModelProviderConfig) string {
	providerID := strings.TrimSpace(pc.ProviderID)
	model := strings.TrimSpace(pc.Model)
	if model != "" {
		return model
	}
	if reg == nil {
		return ""
	}
	for _, provider := range reg.Providers {
		if provider.ID == providerID && len(provider.Models) == 1 {
			return strings.TrimSpace(provider.Models[0])
		}
	}
	return ""
}

func modelProviderConfigs(model llmpool.ModelConfig) []llmpool.ModelProviderConfig {
	if len(model.ProviderConfigs) > 0 {
		return model.ProviderConfigs
	}
	configs := make([]llmpool.ModelProviderConfig, 0, len(model.ProviderIDs))
	for _, providerID := range model.ProviderIDs {
		providerID = strings.TrimSpace(providerID)
		if providerID == "" {
			continue
		}
		configs = append(configs, llmpool.ModelProviderConfig{ProviderID: providerID})
	}
	return configs
}

func findComputeAgent(reg *Registry, id string) *ComputeAgent {
	if id == "" {
		id = DefaultComputeAgentID
	}
	for i := range reg.Agents {
		if reg.Agents[i].ID == id {
			return &reg.Agents[i]
		}
	}
	return nil
}

func normalizeComputeAgentInput(agent *ComputeAgent) error {
	if agent == nil {
		return fmt.Errorf("agent is required")
	}
	agent.ID = strings.TrimSpace(agent.ID)
	agent.Name = strings.TrimSpace(agent.Name)
	agent.Contact = strings.TrimSpace(agent.Contact)
	agent.Settlement = strings.TrimSpace(agent.Settlement)
	agent.Description = strings.TrimSpace(agent.Description)
	if agent.ID == "" || agent.Name == "" {
		return fmt.Errorf("agent id and name are required")
	}
	for _, r := range agent.ID {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return fmt.Errorf("agent id can only contain letters, numbers, underscore, and hyphen")
	}
	return nil
}

// ListAgents returns configured upstream compute agents.
func (s *Service) ListAgents(ctx context.Context) ([]ComputeAgent, error) {
	reg, err := s.LoadRegistry(ctx)
	if err != nil {
		return nil, err
	}
	return append([]ComputeAgent(nil), reg.Agents...), nil
}

// AddAgent adds a compute agent.
func (s *Service) AddAgent(ctx context.Context, agent ComputeAgent) error {
	reg, err := s.LoadRegistry(ctx)
	if err != nil {
		return err
	}
	if err := normalizeComputeAgentInput(&agent); err != nil {
		return err
	}
	for _, existing := range reg.Agents {
		if existing.ID == agent.ID {
			return fmt.Errorf("agent %s already exists", agent.ID)
		}
	}
	now := time.Now().UTC()
	agent.Enabled = true
	agent.CreatedAt = now
	agent.UpdatedAt = now
	reg.Agents = append(reg.Agents, agent)
	return s.SaveRegistry(ctx, reg)
}

// UpdateAgent updates a compute agent.
func (s *Service) UpdateAgent(ctx context.Context, agent ComputeAgent) error {
	reg, err := s.LoadRegistry(ctx)
	if err != nil {
		return err
	}
	if err := normalizeComputeAgentInput(&agent); err != nil {
		return err
	}
	for i, existing := range reg.Agents {
		if existing.ID == agent.ID {
			agent.CreatedAt = existing.CreatedAt
			agent.UpdatedAt = time.Now().UTC()
			if agent.ID == DefaultComputeAgentID {
				agent.Enabled = true
			}
			reg.Agents[i] = agent
			return s.SaveRegistry(ctx, reg)
		}
	}
	return fmt.Errorf("agent %s not found", agent.ID)
}

// DeleteAgent removes a compute agent if no service group depends on it.
func (s *Service) DeleteAgent(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == DefaultComputeAgentID {
		return fmt.Errorf("default agent cannot be deleted")
	}
	reg, err := s.LoadRegistry(ctx)
	if err != nil {
		return err
	}
	for _, group := range reg.ServiceGroups {
		if group.AgentID == id {
			return fmt.Errorf("agent %s is used by service group %s", id, group.ID)
		}
	}
	filtered := reg.Agents[:0]
	for _, agent := range reg.Agents {
		if agent.ID != id {
			filtered = append(filtered, agent)
		}
	}
	if len(filtered) == len(reg.Agents) {
		return fmt.Errorf("agent %s not found", id)
	}
	reg.Agents = filtered
	return s.SaveRegistry(ctx, reg)
}

// InvalidateCache forces the next LoadRegistry to re-read from storage.
func (s *Service) InvalidateCache() {
	s.mu.Lock()
	s.cached = nil
	s.mu.Unlock()
}

// GetSystemSetting reads a raw value from system settings (for payment config etc).
func (s *Service) GetSystemSetting(ctx context.Context, key string) (string, error) {
	return s.system.Get(ctx, key)
}

// SetSystemSetting writes a raw value to system settings.
func (s *Service) SetSystemSetting(ctx context.Context, key, value string) error {
	return s.system.Set(ctx, key, value)
}

// ---------------------------------------------------------------------------
// Provider CRUD
// ---------------------------------------------------------------------------

// AddProvider adds a new LLM provider to the registry.
func (s *Service) AddProvider(ctx context.Context, provider llmpool.ProviderConfig) error {
	reg, err := s.LoadRegistry(ctx)
	if err != nil {
		return err
	}
	for _, p := range reg.Providers {
		if p.ID == provider.ID {
			return fmt.Errorf("provider %s already exists", provider.ID)
		}
	}
	reg.Providers = append(reg.Providers, provider)
	return s.SaveRegistry(ctx, reg)
}

// UpdateProvider updates an existing provider in the registry.
func (s *Service) UpdateProvider(ctx context.Context, provider llmpool.ProviderConfig) error {
	reg, err := s.LoadRegistry(ctx)
	if err != nil {
		return err
	}
	found := false
	for i, p := range reg.Providers {
		if p.ID == provider.ID {
			reg.Providers[i] = provider
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("provider %s not found", provider.ID)
	}
	return s.SaveRegistry(ctx, reg)
}

// DeleteProvider removes a provider from the registry.
func (s *Service) DeleteProvider(ctx context.Context, id string) error {
	reg, err := s.LoadRegistry(ctx)
	if err != nil {
		return err
	}
	filtered := reg.Providers[:0]
	for _, p := range reg.Providers {
		if p.ID != id {
			filtered = append(filtered, p)
		}
	}
	if len(filtered) == len(reg.Providers) {
		return fmt.Errorf("provider %s not found", id)
	}
	reg.Providers = filtered
	return s.SaveRegistry(ctx, reg)
}

// GetProvider returns a provider by ID.
func (s *Service) GetProvider(ctx context.Context, id string) (*llmpool.ProviderConfig, error) {
	reg, err := s.LoadRegistry(ctx)
	if err != nil {
		return nil, err
	}
	for i, p := range reg.Providers {
		if p.ID == id {
			return &reg.Providers[i], nil
		}
	}
	return nil, nil
}

// ---------------------------------------------------------------------------
// Service Group CRUD
// ---------------------------------------------------------------------------

// AddServiceGroup adds a new service group.
func (s *Service) AddServiceGroup(ctx context.Context, group llmpool.ServiceGroup) error {
	if group.ID == ExternalComputePermissionServiceGroupID {
		return fmt.Errorf("service group id %s is reserved", group.ID)
	}
	reg, err := s.LoadRegistry(ctx)
	if err != nil {
		return err
	}
	if group.AgentID == "" {
		group.AgentID = DefaultComputeAgentID
	}
	normalizeServiceGroupModels(&group)
	if err := validateServiceGroupProviderRoutes(reg, group); err != nil {
		return err
	}
	if findComputeAgent(reg, group.AgentID) == nil {
		return fmt.Errorf("agent %s not found", group.AgentID)
	}
	normalizeServiceGroupAgent(reg, &group)
	for _, g := range reg.ServiceGroups {
		if g.ID == group.ID {
			return fmt.Errorf("service group %s already exists", group.ID)
		}
	}
	reg.ServiceGroups = append(reg.ServiceGroups, group)
	return s.SaveRegistry(ctx, reg)
}

// UpdateServiceGroup updates an existing service group.
func (s *Service) UpdateServiceGroup(ctx context.Context, group llmpool.ServiceGroup) error {
	if group.ID == ExternalComputePermissionServiceGroupID {
		return fmt.Errorf("service group id %s is reserved", group.ID)
	}
	reg, err := s.LoadRegistry(ctx)
	if err != nil {
		return err
	}
	if group.AgentID == "" {
		group.AgentID = DefaultComputeAgentID
	}
	normalizeServiceGroupModels(&group)
	if err := validateServiceGroupProviderRoutes(reg, group); err != nil {
		return err
	}
	if findComputeAgent(reg, group.AgentID) == nil {
		return fmt.Errorf("agent %s not found", group.AgentID)
	}
	normalizeServiceGroupAgent(reg, &group)
	found := false
	for i, g := range reg.ServiceGroups {
		if g.ID == group.ID {
			reg.ServiceGroups[i] = group
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("service group %s not found", group.ID)
	}
	return s.SaveRegistry(ctx, reg)
}

// DeleteServiceGroup removes a service group.
func (s *Service) DeleteServiceGroup(ctx context.Context, id string) error {
	reg, err := s.LoadRegistry(ctx)
	if err != nil {
		return err
	}
	filtered := reg.ServiceGroups[:0]
	for _, g := range reg.ServiceGroups {
		if g.ID != id {
			filtered = append(filtered, g)
		}
	}
	if len(filtered) == len(reg.ServiceGroups) {
		return fmt.Errorf("service group %s not found", id)
	}
	reg.ServiceGroups = filtered
	return s.SaveRegistry(ctx, reg)
}

// FindServiceGroupForModel searches all service groups for a model and returns
// the matching group + model dispatch info for provider ordering.
func (s *Service) FindServiceGroupForModel(ctx context.Context, serviceGroupID, modelName string) (*llmpool.ServiceGroup, *llmpool.DispatchModel, error) {
	reg, err := s.LoadRegistry(ctx)
	if err != nil {
		return nil, nil, err
	}
	for i, group := range reg.ServiceGroups {
		if serviceGroupID != "" && group.ID != serviceGroupID {
			continue
		}
		for _, model := range group.Models {
			if model.Name == modelName || modelName == "" {
				dm := buildDispatchModel(reg, &model)
				return &reg.ServiceGroups[i], dm, nil
			}
		}
	}
	return nil, nil, fmt.Errorf("model %q not found in service group %q", modelName, serviceGroupID)
}

// ListAvailableModels returns all models across all service groups.
func (s *Service) ListAvailableModels(ctx context.Context) ([]string, error) {
	reg, err := s.LoadRegistry(ctx)
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	var models []string
	for _, group := range reg.ServiceGroups {
		for _, model := range group.Models {
			if _, ok := seen[model.Name]; !ok {
				seen[model.Name] = struct{}{}
				models = append(models, model.Name)
			}
		}
	}
	return models, nil
}

// ---------------------------------------------------------------------------
// internal helpers
// ---------------------------------------------------------------------------

func buildDispatchModel(reg *Registry, model *llmpool.ModelConfig) *llmpool.DispatchModel {
	dm := &llmpool.DispatchModel{
		Name:                      model.Name,
		CapabilityTags:            model.CapabilityTags,
		Priority:                  model.Priority,
		ResolutionTier:            model.ResolutionTier,
		CreditMultiplier:          model.CreditMultiplier,
		ProviderCapabilityTags:    map[string][]string{},
		ProviderPriorities:        map[string]int{},
		ProviderResolutionTiers:   map[string]int{},
		ProviderCreditMultipliers: map[string]float64{},
	}
	for _, pc := range modelProviderConfigs(*model) {
		providerID := strings.TrimSpace(pc.ProviderID)
		upstreamModel := strings.TrimSpace(pc.Model)
		if providerID == "" {
			continue
		}
		dm.ProviderIDs = append(dm.ProviderIDs, providerID)
		dm.ProviderRoutes = append(dm.ProviderRoutes, llmpool.DispatchProviderRoute{
			ProviderID:       providerID,
			Model:            upstreamModel,
			CapabilityTags:   pc.CapabilityTags,
			Priority:         pc.Priority,
			ResolutionTier:   pc.ResolutionTier,
			CreditMultiplier: pc.CreditMultiplier,
			OriginalIndex:    len(dm.ProviderRoutes),
		})
		if len(pc.CapabilityTags) > 0 {
			dm.ProviderCapabilityTags[providerID] = pc.CapabilityTags
		}
		if pc.Priority != 0 {
			dm.ProviderPriorities[providerID] = pc.Priority
		}
		if pc.ResolutionTier != 0 {
			dm.ProviderResolutionTiers[providerID] = pc.ResolutionTier
		}
		if pc.CreditMultiplier > 0 {
			dm.ProviderCreditMultipliers[providerID] = pc.CreditMultiplier
		}
	}
	return dm
}
