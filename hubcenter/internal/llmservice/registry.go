// Package llmservice implements the HubCenter LLM service management —
// provider configuration, service group routing, tenant authorization,
// and usage statistics.
package llmservice

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

const registryKey = "llm_service_registry"

// Registry holds all LLM providers and service groups configured on HubCenter.
type Registry struct {
	Providers     []llmpool.ProviderConfig `json:"providers"`
	ServiceGroups []llmpool.ServiceGroup   `json:"service_groups"`
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
	s.cached = reg
	s.cachedAt = time.Now()
	return reg, nil
}

// SaveRegistry persists the registry to the system settings store.
func (s *Service) SaveRegistry(ctx context.Context, reg *Registry) error {
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
	reg, err := s.LoadRegistry(ctx)
	if err != nil {
		return err
	}
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
	reg, err := s.LoadRegistry(ctx)
	if err != nil {
		return err
	}
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
	for _, pc := range model.ProviderConfigs {
		dm.ProviderIDs = append(dm.ProviderIDs, pc.ProviderID)
		if len(pc.CapabilityTags) > 0 {
			dm.ProviderCapabilityTags[pc.ProviderID] = pc.CapabilityTags
		}
		if pc.Priority != 0 {
			dm.ProviderPriorities[pc.ProviderID] = pc.Priority
		}
		if pc.ResolutionTier != 0 {
			dm.ProviderResolutionTiers[pc.ProviderID] = pc.ResolutionTier
		}
		if pc.CreditMultiplier > 0 {
			dm.ProviderCreditMultipliers[pc.ProviderID] = pc.CreditMultiplier
		}
	}
	return dm
}
