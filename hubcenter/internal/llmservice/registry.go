// Package llmservice implements the HubCenter LLM service management —
// provider configuration, service group routing, tenant authorization,
// and usage statistics.
package llmservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

// RegistrySettingKey is the system-settings key for the LLM provider registry.
// HA replicas must invalidate the in-memory registry cache when this key is applied.
const RegistrySettingKey = "llm_service_registry"

const DefaultComputeAgentID = "maclaw_official"
const DefaultComputeAgentName = "MaClaw官方"

const AccessPolicyFree = "free"
const AccessPolicyGrantRequired = "grant_required"

// ErrProviderNotFound is returned when a provider id is missing from the registry.
var ErrProviderNotFound = errors.New("provider not found")

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
	Providers             []llmpool.ProviderConfig `json:"providers"`
	ServiceGroups         []llmpool.ServiceGroup   `json:"service_groups"`
	Agents                []ComputeAgent           `json:"agents,omitempty"`
	DefaultServiceGroupID string                   `json:"default_service_group_id,omitempty"`
	UpdatedAt             time.Time                `json:"updated_at,omitempty"`
}

// Service manages the HubCenter LLM configuration and dispatching.
type Service struct {
	system    store.SystemSettingsRepository
	mu        sync.RWMutex
	writeMu   sync.Mutex
	cached    *Registry
	cachedAt  time.Time
	headLocal string
	headPeers []string
}

const registryCacheTTL = 30 * time.Second

func (s *Service) lockRegistryWrite() func() {
	if s == nil {
		return func() {}
	}
	s.writeMu.Lock()
	return s.writeMu.Unlock
}

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

	raw, err := s.system.Get(ctx, RegistrySettingKey)
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
	defer s.lockRegistryWrite()()
	return s.persistRegistry(ctx, reg)
}

func (s *Service) persistRegistry(ctx context.Context, reg *Registry) error {
	if s == nil {
		return fmt.Errorf("llm service is required")
	}
	if reg == nil {
		return fmt.Errorf("registry is required")
	}
	toSave := cloneRegistry(reg)
	normalizeRegistry(toSave)
	toSave.UpdatedAt = time.Now().UTC()
	data, err := json.Marshal(toSave)
	if err != nil {
		return fmt.Errorf("marshal llm registry: %w", err)
	}
	if err := s.system.Set(ctx, RegistrySettingKey, string(data)); err != nil {
		return fmt.Errorf("save llm registry: %w", err)
	}
	s.mu.Lock()
	s.cached = toSave
	s.cachedAt = time.Now()
	s.mu.Unlock()
	return nil
}

// MutateRegistry clones the current registry, applies fn, and persists only when
// fn reports a change. The write lock is held for the whole load-modify-save so
// callers cannot clobber a concurrent pause.
func (s *Service) MutateRegistry(ctx context.Context, fn func(*Registry) (bool, error)) error {
	if s == nil {
		return fmt.Errorf("llm service is required")
	}
	if fn == nil {
		return fmt.Errorf("registry mutator is required")
	}
	defer s.lockRegistryWrite()()
	reg, err := s.LoadRegistry(ctx)
	if err != nil {
		return err
	}
	next := cloneRegistry(reg)
	changed, err := fn(next)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	return s.persistRegistry(ctx, next)
}

// Default queue wait timeout when a provider has MaxConcurrency > 0 but no
// explicit QueueTimeoutMS. MaxQueueWaiters is intentionally left alone:
// 0 means unlimited waiters in llmpool.ConcurrencyController (do not coerce).
const defaultProviderQueueTimeoutMS = 120000

func cloneRegistry(reg *Registry) *Registry {
	if reg == nil {
		return &Registry{}
	}
	next := *reg
	next.Providers = cloneProviderConfigs(reg.Providers)
	next.ServiceGroups = cloneServiceGroups(reg.ServiceGroups)
	next.Agents = append([]ComputeAgent(nil), reg.Agents...)
	return &next
}

func cloneProviderConfigs(in []llmpool.ProviderConfig) []llmpool.ProviderConfig {
	if in == nil {
		return nil
	}
	out := make([]llmpool.ProviderConfig, len(in))
	for i, p := range in {
		out[i] = p
		out[i].Models = append([]string(nil), p.Models...)
		out[i].CapabilityTags = append([]string(nil), p.CapabilityTags...)
		out[i].CreditMultiplierSchedule = cloneCreditWindows(p.CreditMultiplierSchedule)
	}
	return out
}

func cloneCreditWindows(in []llmpool.CreditMultiplierWindow) []llmpool.CreditMultiplierWindow {
	if in == nil {
		return nil
	}
	out := make([]llmpool.CreditMultiplierWindow, len(in))
	for i, w := range in {
		out[i] = w
		out[i].Days = append([]int(nil), w.Days...)
	}
	return out
}

func cloneServiceGroups(in []llmpool.ServiceGroup) []llmpool.ServiceGroup {
	if in == nil {
		return nil
	}
	out := make([]llmpool.ServiceGroup, len(in))
	for i, g := range in {
		out[i] = g
		out[i].Routes = append([]llmpool.WorkloadRoute(nil), g.Routes...)
		out[i].ExposedModels = append([]string(nil), g.ExposedModels...)
		out[i].Models = cloneModelConfigs(g.Models)
	}
	return out
}

func cloneModelConfigs(in []llmpool.ModelConfig) []llmpool.ModelConfig {
	if in == nil {
		return nil
	}
	out := make([]llmpool.ModelConfig, len(in))
	for i, m := range in {
		out[i] = m
		out[i].ProviderIDs = append([]string(nil), m.ProviderIDs...)
		out[i].CapabilityTags = append([]string(nil), m.CapabilityTags...)
		out[i].ProviderConfigs = cloneModelProviderConfigs(m.ProviderConfigs)
	}
	return out
}

func cloneModelProviderConfigs(in []llmpool.ModelProviderConfig) []llmpool.ModelProviderConfig {
	if in == nil {
		return nil
	}
	out := make([]llmpool.ModelProviderConfig, len(in))
	for i, pc := range in {
		out[i] = pc
		out[i].CapabilityTags = append([]string(nil), pc.CapabilityTags...)
	}
	return out
}

func providerIndex(reg *Registry, id string) int {
	id = strings.TrimSpace(id)
	if reg == nil || id == "" {
		return -1
	}
	for i, p := range reg.Providers {
		if strings.TrimSpace(p.ID) == id {
			return i
		}
	}
	return -1
}

func normalizeRegistry(reg *Registry) {
	if reg == nil {
		return
	}
	ensureDefaultComputeAgent(reg)
	for i := range reg.Providers {
		normalizeProviderGatewayLimits(&reg.Providers[i])
		reg.Providers[i].NormalizeBilling()
	}
	normalizeProviderSequences(reg)
	for i := range reg.ServiceGroups {
		llmpool.EnsureOfficialDynamicTemplate(&reg.ServiceGroups[i])
		normalizeServiceGroupModels(&reg.ServiceGroups[i])
		normalizeServiceGroupAgent(reg, &reg.ServiceGroups[i])
		if !llmpool.IsDynamicKind(reg.ServiceGroups[i].Kind) {
			reg.ServiceGroups[i].Kind = ""
		}
	}
	sanitizeDefaultServiceGroupID(reg)
}

func catalogServiceGroupID(reg *Registry, id string) string {
	id = strings.TrimSpace(id)
	if reg == nil || id == "" || llmpool.IsHubOfficialServiceGroup(id) || sameServiceGroupID(id, ExternalComputePermissionServiceGroupID) {
		return ""
	}
	for i := range reg.ServiceGroups {
		got := strings.TrimSpace(reg.ServiceGroups[i].ID)
		if got == "" || !strings.EqualFold(got, id) || llmpool.IsHubOfficialServiceGroup(got) {
			continue
		}
		return got
	}
	return ""
}

func sanitizeDefaultServiceGroupID(reg *Registry) {
	if reg == nil {
		return
	}
	reg.DefaultServiceGroupID = catalogServiceGroupID(reg, reg.DefaultServiceGroupID)
}

func (s *Service) SetDefaultServiceGroup(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	return s.MutateRegistry(ctx, func(reg *Registry) (bool, error) {
		if id == "" {
			if strings.TrimSpace(reg.DefaultServiceGroupID) == "" {
				return false, nil
			}
			reg.DefaultServiceGroupID = ""
			return true, nil
		}
		got := catalogServiceGroupID(reg, id)
		if got == "" {
			return false, fmt.Errorf("service group %s not found", id)
		}
		if strings.EqualFold(strings.TrimSpace(reg.DefaultServiceGroupID), got) {
			return false, nil
		}
		reg.DefaultServiceGroupID = got
		return true, nil
	})
}

func normalizeProviderGatewayLimits(provider *llmpool.ProviderConfig) {
	if provider == nil {
		return
	}
	if provider.MaxConcurrency < 0 {
		provider.MaxConcurrency = 0
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

func normalizeProviderSequences(reg *Registry) {
	if reg == nil || len(reg.Providers) == 0 {
		return
	}
	for _, provider := range reg.Providers {
		if provider.Sequence > 0 {
			return
		}
	}
	for i := range reg.Providers {
		reg.Providers[i].Sequence = i + 1
	}
}

func nextProviderSequence(reg *Registry) int {
	max := 0
	if reg != nil {
		for _, provider := range reg.Providers {
			if provider.Sequence > max {
				max = provider.Sequence
			}
		}
	}
	return max + 1
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

// RequiresGrantAccessPolicy reports whether a group bills card or grant credits.
func RequiresGrantAccessPolicy(policy string) bool {
	return normalizeServiceGroupAccessPolicy(policy) == AccessPolicyGrantRequired
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
			effective := pc.TokenPricing
			if idx := providerIndex(reg, pc.ProviderID); idx >= 0 {
				effective = llmpool.EffectiveRouteTokenPricing(pc, reg.Providers[idx])
			}
			if err := llmpool.ValidateRouteBilling(pc.BillingMode, effective); err != nil {
				return fmt.Errorf("model %q provider %q: %w", model.Name, pc.ProviderID, err)
			}
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
	defer s.lockRegistryWrite()()
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
	next := cloneRegistry(reg)
	next.Agents = append(next.Agents, agent)
	return s.persistRegistry(ctx, next)
}

// UpdateAgent updates a compute agent.
func (s *Service) UpdateAgent(ctx context.Context, agent ComputeAgent) error {
	defer s.lockRegistryWrite()()
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
			next := cloneRegistry(reg)
			next.Agents[i] = agent
			return s.persistRegistry(ctx, next)
		}
	}
	return fmt.Errorf("agent %s not found", agent.ID)
}

// DeleteAgent removes a compute agent if no service group depends on it.
func (s *Service) DeleteAgent(ctx context.Context, id string) error {
	defer s.lockRegistryWrite()()
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
	filtered := make([]ComputeAgent, 0, len(reg.Agents))
	for _, agent := range reg.Agents {
		if agent.ID != id {
			filtered = append(filtered, agent)
		}
	}
	if len(filtered) == len(reg.Agents) {
		return fmt.Errorf("agent %s not found", id)
	}
	next := cloneRegistry(reg)
	next.Agents = filtered
	return s.persistRegistry(ctx, next)
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

// validateProviderDefaultBilling checks the provider-wide default token price.
// A provider without pricing is legal (routes then price themselves); a priced
// default must still be a valid paid shape (finite, non-negative, non-zero).
func validateProviderDefaultBilling(provider llmpool.ProviderConfig) error {
	if provider.TokenPricing.HasCreditPricing() {
		return llmpool.ValidateRouteBilling(llmpool.BillingModePaid, provider.TokenPricing)
	}
	return llmpool.ValidateRouteBilling(llmpool.BillingModeFree, provider.TokenPricing)
}

// AddProvider adds a new LLM provider to the registry.
func (s *Service) AddProvider(ctx context.Context, provider llmpool.ProviderConfig) error {
	defer s.lockRegistryWrite()()
	provider.ID = strings.TrimSpace(provider.ID)
	if provider.ID == "" {
		return fmt.Errorf("provider id required")
	}
	if err := validateProviderDefaultBilling(provider); err != nil {
		return err
	}
	reg, err := s.LoadRegistry(ctx)
	if err != nil {
		return err
	}
	if providerIndex(reg, provider.ID) >= 0 {
		return fmt.Errorf("provider %s already exists", provider.ID)
	}
	provider.NormalizeBilling()
	if provider.Sequence <= 0 {
		provider.Sequence = nextProviderSequence(reg)
	}
	next := cloneRegistry(reg)
	next.Providers = append(next.Providers, provider)
	return s.persistRegistry(ctx, next)
}

func mergeUnspecifiedProviderFields(existing, incoming llmpool.ProviderConfig) llmpool.ProviderConfig {
	incoming.ID = existing.ID
	incoming.Paused = existing.Paused
	if incoming.Sequence <= 0 {
		incoming.Sequence = existing.Sequence
	}
	if strings.TrimSpace(incoming.WireAPI) == "" {
		incoming.WireAPI = existing.WireAPI
	}
	if incoming.ResolutionTier == 0 {
		incoming.ResolutionTier = existing.ResolutionTier
	}
	if incoming.MaxQueueWaiters == 0 {
		incoming.MaxQueueWaiters = existing.MaxQueueWaiters
	}
	if incoming.QueueTimeoutMS == 0 {
		incoming.QueueTimeoutMS = existing.QueueTimeoutMS
	}
	if incoming.CircuitBreakerThreshold == 0 {
		incoming.CircuitBreakerThreshold = existing.CircuitBreakerThreshold
	}
	if incoming.CircuitBreakerCooldownMS == 0 {
		incoming.CircuitBreakerCooldownMS = existing.CircuitBreakerCooldownMS
	}
	if incoming.FailureBackoffBaseMS == 0 {
		incoming.FailureBackoffBaseMS = existing.FailureBackoffBaseMS
	}
	if incoming.FailureBackoffMaxMS == 0 {
		incoming.FailureBackoffMaxMS = existing.FailureBackoffMaxMS
	}
	return incoming
}

// UpdateProvider updates an existing provider in the registry.
func (s *Service) UpdateProvider(ctx context.Context, provider llmpool.ProviderConfig) error {
	defer s.lockRegistryWrite()()
	if err := validateProviderDefaultBilling(provider); err != nil {
		return err
	}
	reg, err := s.LoadRegistry(ctx)
	if err != nil {
		return err
	}
	idx := providerIndex(reg, provider.ID)
	if idx < 0 {
		return fmt.Errorf("%w: %s", ErrProviderNotFound, provider.ID)
	}
	provider = mergeUnspecifiedProviderFields(reg.Providers[idx], provider)
	provider.NormalizeBilling()
	next := cloneRegistry(reg)
	next.Providers[idx] = provider
	return s.persistRegistry(ctx, next)
}

// SetProviderPaused pauses or resumes dispatch to a provider without changing
// its credentials or routing configuration.
func (s *Service) SetProviderPaused(ctx context.Context, id string, paused bool) error {
	id = strings.TrimSpace(id)
	if s == nil || id == "" {
		return fmt.Errorf("provider id required")
	}
	defer s.lockRegistryWrite()()
	reg, err := s.LoadRegistry(ctx)
	if err != nil {
		return err
	}
	idx := providerIndex(reg, id)
	if idx < 0 {
		return fmt.Errorf("%w: %s", ErrProviderNotFound, id)
	}
	if reg.Providers[idx].Paused == paused {
		return nil
	}
	next := cloneRegistry(reg)
	next.Providers[idx].Paused = paused
	return s.persistRegistry(ctx, next)
}

// SetProviderSequence sets the dispatch order for a provider. Smaller numbers
// are tried first; paused or failed providers are skipped.
func (s *Service) SetProviderSequence(ctx context.Context, id string, sequence int) error {
	return s.SetProviderSequences(ctx, map[string]int{strings.TrimSpace(id): sequence})
}

// SetProviderSequences applies many dispatch-order updates in one persist so a
// swap or reindex cannot land halfway.
func (s *Service) SetProviderSequences(ctx context.Context, sequences map[string]int) error {
	if len(sequences) == 0 {
		return fmt.Errorf("sequences required")
	}
	return s.MutateRegistry(ctx, func(reg *Registry) (bool, error) {
		changed := false
		for id, sequence := range sequences {
			id = strings.TrimSpace(id)
			if id == "" {
				return false, fmt.Errorf("provider id required")
			}
			if sequence < 1 {
				return false, fmt.Errorf("sequence must be >= 1")
			}
			idx := providerIndex(reg, id)
			if idx < 0 {
				return false, fmt.Errorf("%w: %s", ErrProviderNotFound, id)
			}
			if reg.Providers[idx].Sequence == sequence {
				continue
			}
			reg.Providers[idx].Sequence = sequence
			changed = true
		}
		return changed, nil
	})
}

// ListProviderBilling returns vendor time-of-use policies for configured providers.
func (s *Service) ListProviderBilling(ctx context.Context) []llmpool.ProviderBillingPolicy {
	if s == nil {
		return nil
	}
	reg, err := s.LoadRegistry(ctx)
	if err != nil || reg == nil {
		return nil
	}
	return llmpool.ProviderBillingPolicies(reg.Providers)
}

// DeleteProvider removes a provider from the registry.
func (s *Service) DeleteProvider(ctx context.Context, id string) error {
	defer s.lockRegistryWrite()()
	reg, err := s.LoadRegistry(ctx)
	if err != nil {
		return err
	}
	idx := providerIndex(reg, id)
	if idx < 0 {
		return fmt.Errorf("%w: %s", ErrProviderNotFound, strings.TrimSpace(id))
	}
	filtered := make([]llmpool.ProviderConfig, 0, len(reg.Providers)-1)
	filtered = append(filtered, reg.Providers[:idx]...)
	filtered = append(filtered, reg.Providers[idx+1:]...)
	next := cloneRegistry(reg)
	next.Providers = filtered
	return s.persistRegistry(ctx, next)
}

// GetProvider returns a provider by ID.
func (s *Service) GetProvider(ctx context.Context, id string) (*llmpool.ProviderConfig, error) {
	reg, err := s.LoadRegistry(ctx)
	if err != nil {
		return nil, err
	}
	idx := providerIndex(reg, id)
	if idx < 0 {
		return nil, nil
	}
	got := reg.Providers[idx]
	return &got, nil
}

// ---------------------------------------------------------------------------
// Service Group CRUD
// ---------------------------------------------------------------------------

// AddServiceGroup adds a new service group.
func (s *Service) AddServiceGroup(ctx context.Context, group llmpool.ServiceGroup) error {
	defer s.lockRegistryWrite()()
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
	llmpool.EnsureOfficialDynamicTemplate(&group)
	normalizeServiceGroupModels(&group)
	if err := validateServiceGroupProviderRoutes(reg, group); err != nil {
		return err
	}
	if err := llmpool.ValidateDynamicServiceGroup(&group); err != nil {
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
	next := cloneRegistry(reg)
	next.ServiceGroups = append(next.ServiceGroups, group)
	return s.persistRegistry(ctx, next)
}

// UpdateServiceGroup updates an existing service group.
func (s *Service) UpdateServiceGroup(ctx context.Context, group llmpool.ServiceGroup) error {
	defer s.lockRegistryWrite()()
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
	llmpool.EnsureOfficialDynamicTemplate(&group)
	normalizeServiceGroupModels(&group)
	if err := validateServiceGroupProviderRoutes(reg, group); err != nil {
		return err
	}
	if err := llmpool.ValidateDynamicServiceGroup(&group); err != nil {
		return err
	}
	if findComputeAgent(reg, group.AgentID) == nil {
		return fmt.Errorf("agent %s not found", group.AgentID)
	}
	normalizeServiceGroupAgent(reg, &group)
	idx := -1
	for i, g := range reg.ServiceGroups {
		if g.ID == group.ID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("service group %s not found", group.ID)
	}
	next := cloneRegistry(reg)
	next.ServiceGroups[idx] = group
	return s.persistRegistry(ctx, next)
}

// DeleteServiceGroup removes a service group.
func (s *Service) DeleteServiceGroup(ctx context.Context, id string) error {
	if llmpool.IsOfficialConventionGroupID(id) {
		return fmt.Errorf("MaClaw official group %s is system-generated and cannot be deleted", llmpool.OfficialGroupID)
	}
	defer s.lockRegistryWrite()()
	reg, err := s.LoadRegistry(ctx)
	if err != nil {
		return err
	}
	filtered := make([]llmpool.ServiceGroup, 0, len(reg.ServiceGroups))
	for _, g := range reg.ServiceGroups {
		if g.ID != id {
			filtered = append(filtered, g)
		}
	}
	if len(filtered) == len(reg.ServiceGroups) {
		return fmt.Errorf("service group %s not found", id)
	}
	if def := catalogServiceGroupID(reg, reg.DefaultServiceGroupID); def != "" && strings.EqualFold(def, strings.TrimSpace(id)) {
		return fmt.Errorf("set another default service group before deleting %s", id)
	}
	next := cloneRegistry(reg)
	next.ServiceGroups = filtered
	return s.persistRegistry(ctx, next)
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
