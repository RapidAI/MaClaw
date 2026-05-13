// Package skill provides skill source access control for maclawsrv.
//
// This module manages which skill search/download sources (skillhub, clawhub,
// github) are allowed at three levels:
//   - Global: applies to all users unless overridden
//   - Tenant: applies to all users in a tenant (security group)
//   - User: per-user override
//
// Resolution priority (highest wins): User > Tenant > Global > Default (all allowed).
//
// The resolved result is merged into the heartbeat payload's EffectivePolicy
// (via SkillSourcesProvider interface) so that GUI/TUI clients receive a single
// unified allowed-sources list without needing to know about the two control planes.
package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// AllSources is the complete list of valid skill source identifiers.
var AllSources = []string{"skillhub", "clawhub", "github"}

const (
	settingsKeyGlobal = "skill_source_control_global"
	settingsKeyTenant = "skill_source_control_tenant_" // + tenantID
	settingsKeyUser   = "skill_source_control_user_"   // + userEmail
)

// SourceControlConfig represents the allowed sources at one level.
type SourceControlConfig struct {
	// AllowedSources lists permitted sources.
	// When Enabled=true and AllowedSources is empty ([]), all sources are blocked.
	// When Enabled=true and AllowedSources is non-empty, only listed sources are allowed.
	AllowedSources []string `json:"allowed_sources"`
	// Enabled controls whether this level's config is active.
	// When false, this level is skipped and the parent level applies.
	Enabled bool `json:"enabled"`
}

// SkillSourcesProvider is the interface consumed by SecurityService.GetHeartbeatPolicy
// to merge skill source control into the heartbeat payload.
type SkillSourcesProvider interface {
	// ResolveForUser returns the allowed sources for a user.
	// Returns nil when all sources are allowed (no restriction).
	ResolveForUser(ctx context.Context, email, tenantID string) []string
}

// SourceControlService manages skill source access control.
type SourceControlService struct {
	system store.SystemSettingsRepository
	mu     sync.RWMutex
	// In-memory cache for fast resolution. Populated on first access and
	// updated on writes. Cache misses fall through to DB.
	globalCache *SourceControlConfig
	globalReady bool
	tenantCache map[string]*SourceControlConfig
	userCache   map[string]*SourceControlConfig
}

// NewSourceControlService creates a new service.
func NewSourceControlService(system store.SystemSettingsRepository) *SourceControlService {
	return &SourceControlService{
		system:      system,
		tenantCache: make(map[string]*SourceControlConfig),
		userCache:   make(map[string]*SourceControlConfig),
	}
}

// --- Public API ---

// GetGlobal returns the global skill source control config.
func (s *SourceControlService) GetGlobal(ctx context.Context) (*SourceControlConfig, error) {
	s.mu.RLock()
	if s.globalReady {
		cfg := s.globalCache
		s.mu.RUnlock()
		return cfg, nil
	}
	s.mu.RUnlock()

	cfg, err := s.loadConfig(ctx, settingsKeyGlobal)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.globalCache = cfg
	s.globalReady = true
	s.mu.Unlock()
	return cfg, nil
}

// SetGlobal sets the global skill source control config.
func (s *SourceControlService) SetGlobal(ctx context.Context, cfg *SourceControlConfig) error {
	if err := validateSources(cfg.AllowedSources); err != nil {
		return err
	}
	if err := s.saveConfig(ctx, settingsKeyGlobal, cfg); err != nil {
		return err
	}
	s.mu.Lock()
	s.globalCache = cfg
	s.globalReady = true
	s.mu.Unlock()
	return nil
}

// GetTenant returns the skill source control config for a tenant (security group).
func (s *SourceControlService) GetTenant(ctx context.Context, tenantID string) (*SourceControlConfig, error) {
	s.mu.RLock()
	if cfg, ok := s.tenantCache[tenantID]; ok {
		s.mu.RUnlock()
		return cfg, nil
	}
	s.mu.RUnlock()

	cfg, err := s.loadConfig(ctx, settingsKeyTenant+tenantID)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.tenantCache[tenantID] = cfg // nil is a valid cache entry (means "not configured")
	s.mu.Unlock()
	return cfg, nil
}

// SetTenant sets the skill source control config for a tenant.
func (s *SourceControlService) SetTenant(ctx context.Context, tenantID string, cfg *SourceControlConfig) error {
	if err := validateSources(cfg.AllowedSources); err != nil {
		return err
	}
	if err := s.saveConfig(ctx, settingsKeyTenant+tenantID, cfg); err != nil {
		return err
	}
	s.mu.Lock()
	s.tenantCache[tenantID] = cfg
	s.mu.Unlock()
	return nil
}

// DeleteTenant removes the tenant-level config (reverts to global).
func (s *SourceControlService) DeleteTenant(ctx context.Context, tenantID string) error {
	if err := s.system.Set(ctx, settingsKeyTenant+tenantID, ""); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.tenantCache, tenantID)
	s.mu.Unlock()
	return nil
}

// GetUser returns the skill source control config for a specific user.
func (s *SourceControlService) GetUser(ctx context.Context, email string) (*SourceControlConfig, error) {
	s.mu.RLock()
	if cfg, ok := s.userCache[email]; ok {
		s.mu.RUnlock()
		return cfg, nil
	}
	s.mu.RUnlock()

	cfg, err := s.loadConfig(ctx, settingsKeyUser+email)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.userCache[email] = cfg
	s.mu.Unlock()
	return cfg, nil
}

// SetUser sets the skill source control config for a specific user.
func (s *SourceControlService) SetUser(ctx context.Context, email string, cfg *SourceControlConfig) error {
	if err := validateSources(cfg.AllowedSources); err != nil {
		return err
	}
	if err := s.saveConfig(ctx, settingsKeyUser+email, cfg); err != nil {
		return err
	}
	s.mu.Lock()
	s.userCache[email] = cfg
	s.mu.Unlock()
	return nil
}

// DeleteUser removes the user-level config (reverts to tenant/global).
func (s *SourceControlService) DeleteUser(ctx context.Context, email string) error {
	if err := s.system.Set(ctx, settingsKeyUser+email, ""); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.userCache, email)
	s.mu.Unlock()
	return nil
}

// ResolveForUser implements SkillSourcesProvider.
// Computes the effective allowed sources for a user.
// Resolution priority: User > Tenant > Global > Default (all allowed).
// tenantID can be empty if the user has no tenant assignment.
// Returns nil when all sources are allowed.
func (s *SourceControlService) ResolveForUser(ctx context.Context, email, tenantID string) []string {
	// 1. Check user-level override.
	if cfg, err := s.GetUser(ctx, email); err == nil && cfg != nil && cfg.Enabled {
		return cfg.AllowedSources
	}

	// 2. Check tenant-level override.
	if tenantID != "" {
		if cfg, err := s.GetTenant(ctx, tenantID); err == nil && cfg != nil && cfg.Enabled {
			return cfg.AllowedSources
		}
	}

	// 3. Check global-level.
	if cfg, err := s.GetGlobal(ctx); err == nil && cfg != nil && cfg.Enabled {
		return cfg.AllowedSources
	}

	// 4. Default: all sources allowed.
	return nil
}

// IntersectSources computes the intersection of two allowed-sources lists.
// nil means "all allowed". The intersection of nil with X is X.
// The intersection of two non-nil lists is the set intersection.
func IntersectSources(a, b []string) []string {
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}
	set := make(map[string]bool, len(b))
	for _, s := range b {
		set[s] = true
	}
	var result []string
	for _, s := range a {
		if set[s] {
			result = append(result, s)
		}
	}
	return result
}

// --- Internal helpers ---

func (s *SourceControlService) loadConfig(ctx context.Context, key string) (*SourceControlConfig, error) {
	raw, err := s.system.Get(ctx, key)
	if err != nil || raw == "" {
		return nil, err
	}
	var cfg SourceControlConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, fmt.Errorf("parse skill source config: %w", err)
	}
	return &cfg, nil
}

func (s *SourceControlService) saveConfig(ctx context.Context, key string, cfg *SourceControlConfig) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal skill source config: %w", err)
	}
	return s.system.Set(ctx, key, string(data))
}

func validateSources(sources []string) error {
	valid := map[string]bool{"skillhub": true, "clawhub": true, "github": true}
	for _, s := range sources {
		if !valid[s] {
			return fmt.Errorf("invalid skill source: %q (valid: skillhub, clawhub, github)", s)
		}
	}
	return nil
}
