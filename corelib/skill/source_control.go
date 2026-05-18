// source_control.go provides skill source access control with three-level
// resolution: User > Tenant > Global > Default (all allowed).
//
// This lives in corelib/skill so it can be consumed by both:
//   - hub (maclawsrv via hub/internal/httpapi)
//   - MaClawSrv (standalone deployment)
//   - GUI/TUI (for local-only mode)
//
// The only external dependency is KVStore — a minimal key-value interface
// that any storage backend can implement (SQLite, file-based, in-memory).

package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// KVStore is the minimal key-value storage interface required by SourceControlService.
// Implementations: hub's SystemSettingsRepository, MaClawSrv's file store, in-memory (tests).
type KVStore interface {
	Set(ctx context.Context, key, value string) error
	Get(ctx context.Context, key string) (string, error)
}

// AllSkillSources is the canonical list of valid skill source identifiers.
// This is the single source of truth — all other packages reference this.
var AllSkillSources = []string{"skillhub", "clawhub", "github"}

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

// SourceControlService manages skill source access control at three levels.
// Thread-safe. Uses in-memory cache with write-through to KVStore.
type SourceControlService struct {
	kv KVStore
	mu sync.RWMutex
	// In-memory cache. Populated on first access, updated on writes.
	globalCache *SourceControlConfig
	globalReady bool
	tenantCache map[string]*SourceControlConfig
	userCache   map[string]*SourceControlConfig
}

const (
	srcCtlKeyGlobal     = "skill_source_control_global"
	srcCtlKeyTenant     = "skill_source_control_tenant_"      // + tenantID
	srcCtlKeyUser       = "skill_source_control_user_"        // + userEmail (legacy global user override)
	srcCtlKeyTenantUser = "skill_source_control_tenant_user_" // + tenantID + ":" + userEmail
)

// NewSourceControlService creates a new service backed by the given KVStore.
func NewSourceControlService(kv KVStore) *SourceControlService {
	return &SourceControlService{
		kv:          kv,
		tenantCache: make(map[string]*SourceControlConfig),
		userCache:   make(map[string]*SourceControlConfig),
	}
}

// --- Public API ---

func (s *SourceControlService) GetGlobal(ctx context.Context) (*SourceControlConfig, error) {
	s.mu.RLock()
	if s.globalReady {
		cfg := s.globalCache
		s.mu.RUnlock()
		return cfg, nil
	}
	s.mu.RUnlock()
	cfg, err := s.load(ctx, srcCtlKeyGlobal)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.globalCache = cfg
	s.globalReady = true
	s.mu.Unlock()
	return cfg, nil
}

func (s *SourceControlService) SetGlobal(ctx context.Context, cfg *SourceControlConfig) error {
	if err := ValidateSourceNames(cfg.AllowedSources); err != nil {
		return err
	}
	if err := s.save(ctx, srcCtlKeyGlobal, cfg); err != nil {
		return err
	}
	s.mu.Lock()
	s.globalCache = cfg
	s.globalReady = true
	s.mu.Unlock()
	return nil
}

func (s *SourceControlService) GetTenant(ctx context.Context, tenantID string) (*SourceControlConfig, error) {
	s.mu.RLock()
	if cfg, ok := s.tenantCache[tenantID]; ok {
		s.mu.RUnlock()
		return cfg, nil
	}
	s.mu.RUnlock()
	cfg, err := s.load(ctx, srcCtlKeyTenant+tenantID)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.tenantCache[tenantID] = cfg
	s.mu.Unlock()
	return cfg, nil
}

func (s *SourceControlService) SetTenant(ctx context.Context, tenantID string, cfg *SourceControlConfig) error {
	if err := ValidateSourceNames(cfg.AllowedSources); err != nil {
		return err
	}
	if err := s.save(ctx, srcCtlKeyTenant+tenantID, cfg); err != nil {
		return err
	}
	s.mu.Lock()
	s.tenantCache[tenantID] = cfg
	s.mu.Unlock()
	return nil
}

func (s *SourceControlService) DeleteTenant(ctx context.Context, tenantID string) error {
	if err := s.kv.Set(ctx, srcCtlKeyTenant+tenantID, ""); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.tenantCache, tenantID)
	s.mu.Unlock()
	return nil
}

func (s *SourceControlService) GetUser(ctx context.Context, email string) (*SourceControlConfig, error) {
	s.mu.RLock()
	if cfg, ok := s.userCache[email]; ok {
		s.mu.RUnlock()
		return cfg, nil
	}
	s.mu.RUnlock()
	cfg, err := s.load(ctx, srcCtlKeyUser+email)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.userCache[email] = cfg
	s.mu.Unlock()
	return cfg, nil
}

func (s *SourceControlService) GetTenantUser(ctx context.Context, tenantID, email string) (*SourceControlConfig, error) {
	cacheKey := tenantUserCacheKey(tenantID, email)
	s.mu.RLock()
	if cfg, ok := s.userCache[cacheKey]; ok {
		s.mu.RUnlock()
		return cfg, nil
	}
	s.mu.RUnlock()
	cfg, err := s.load(ctx, srcCtlKeyTenantUser+cacheKey)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.userCache[cacheKey] = cfg
	s.mu.Unlock()
	return cfg, nil
}

func (s *SourceControlService) SetUser(ctx context.Context, email string, cfg *SourceControlConfig) error {
	if err := ValidateSourceNames(cfg.AllowedSources); err != nil {
		return err
	}
	if err := s.save(ctx, srcCtlKeyUser+email, cfg); err != nil {
		return err
	}
	s.mu.Lock()
	s.userCache[email] = cfg
	s.mu.Unlock()
	return nil
}

func (s *SourceControlService) SetTenantUser(ctx context.Context, tenantID, email string, cfg *SourceControlConfig) error {
	if err := ValidateSourceNames(cfg.AllowedSources); err != nil {
		return err
	}
	cacheKey := tenantUserCacheKey(tenantID, email)
	if err := s.save(ctx, srcCtlKeyTenantUser+cacheKey, cfg); err != nil {
		return err
	}
	s.mu.Lock()
	s.userCache[cacheKey] = cfg
	s.mu.Unlock()
	return nil
}

func (s *SourceControlService) DeleteUser(ctx context.Context, email string) error {
	if err := s.kv.Set(ctx, srcCtlKeyUser+email, ""); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.userCache, email)
	s.mu.Unlock()
	return nil
}

func (s *SourceControlService) DeleteTenantUser(ctx context.Context, tenantID, email string) error {
	cacheKey := tenantUserCacheKey(tenantID, email)
	if err := s.kv.Set(ctx, srcCtlKeyTenantUser+cacheKey, ""); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.userCache, cacheKey)
	s.mu.Unlock()
	return nil
}

// ResolveForUser computes the effective allowed sources for a user.
// Resolution priority: Tenant User > User > Tenant > Global > Default (all allowed).
// Returns nil when all sources are allowed.
func (s *SourceControlService) ResolveForUser(ctx context.Context, email, tenantID string) []string {
	if tenantID != "" {
		if cfg, err := s.GetTenantUser(ctx, tenantID, email); err == nil && cfg != nil && cfg.Enabled {
			return cfg.AllowedSources
		}
	}
	if cfg, err := s.GetUser(ctx, email); err == nil && cfg != nil && cfg.Enabled {
		return cfg.AllowedSources
	}
	if tenantID != "" {
		if cfg, err := s.GetTenant(ctx, tenantID); err == nil && cfg != nil && cfg.Enabled {
			return cfg.AllowedSources
		}
	}
	if cfg, err := s.GetGlobal(ctx); err == nil && cfg != nil && cfg.Enabled {
		return cfg.AllowedSources
	}
	return nil
}

func tenantUserCacheKey(tenantID, email string) string {
	return tenantID + ":" + email
}

// --- Helpers ---

func (s *SourceControlService) load(ctx context.Context, key string) (*SourceControlConfig, error) {
	raw, err := s.kv.Get(ctx, key)
	if err != nil || raw == "" {
		return nil, err
	}
	var cfg SourceControlConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, fmt.Errorf("parse skill source config %q: %w", key, err)
	}
	return &cfg, nil
}

func (s *SourceControlService) save(ctx context.Context, key string, cfg *SourceControlConfig) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal skill source config: %w", err)
	}
	return s.kv.Set(ctx, key, string(data))
}

// ValidateSourceNames checks that all source names are valid.
func ValidateSourceNames(sources []string) error {
	for _, s := range sources {
		if !validSourceName(s) {
			return fmt.Errorf("invalid skill source: %q (valid: skillhub, clawhub, github)", s)
		}
	}
	return nil
}

// IntersectSources computes the intersection of two allowed-sources lists.
// nil/empty means "all allowed". The intersection of nil with X is X.
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

func validSourceName(s string) bool {
	switch s {
	case "skillhub", "clawhub", "github":
		return true
	}
	return false
}
