// source_control.go provides skill source access control with four-level
// resolution: Tenant User > User > Tenant > Global > Default (all allowed).
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
	"strings"
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
var AllSkillSources = []string{"skillhub", "clawhub", "github", "enterprise_hub", "local"}

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

// SourceControlService manages skill source access control by global, tenant,
// global user, and tenant-scoped runtime user overrides.
// Thread-safe. Uses in-memory cache with write-through to KVStore.
type SourceControlService struct {
	kv KVStore
	mu sync.RWMutex
	// In-memory cache. Populated on first access, updated on writes.
	globalCache     *SourceControlConfig
	globalReady     bool
	tenantCache     map[string]*SourceControlConfig
	userCache       map[string]*SourceControlConfig
	tenantUserCache map[string]*SourceControlConfig
}

const (
	srcCtlKeyGlobal     = "skill_source_control_global"
	srcCtlKeyTenant     = "skill_source_control_tenant_"      // + tenantID
	srcCtlKeyUser       = "skill_source_control_user_"        // + userID (legacy global user override)
	srcCtlKeyTenantUser = "skill_source_control_tenant_user_" // + tenantID + ":" + userID
)

// NewSourceControlService creates a new service backed by the given KVStore.
func NewSourceControlService(kv KVStore) *SourceControlService {
	return &SourceControlService{
		kv:              kv,
		tenantCache:     make(map[string]*SourceControlConfig),
		userCache:       make(map[string]*SourceControlConfig),
		tenantUserCache: make(map[string]*SourceControlConfig),
	}
}

// --- Public API ---

func (s *SourceControlService) GetGlobal(ctx context.Context) (*SourceControlConfig, error) {
	s.mu.RLock()
	if s.globalReady {
		cfg := cloneSourceControlConfig(s.globalCache)
		s.mu.RUnlock()
		return cfg, nil
	}
	s.mu.RUnlock()
	cfg, err := s.load(ctx, srcCtlKeyGlobal)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.globalCache = cloneSourceControlConfig(cfg)
	s.globalReady = true
	s.mu.Unlock()
	return cloneSourceControlConfig(cfg), nil
}

func (s *SourceControlService) SetGlobal(ctx context.Context, cfg *SourceControlConfig) error {
	if err := validateSourceControlConfig(cfg); err != nil {
		return err
	}
	stored := cloneSourceControlConfig(cfg)
	if err := s.save(ctx, srcCtlKeyGlobal, stored); err != nil {
		return err
	}
	s.mu.Lock()
	s.globalCache = cloneSourceControlConfig(stored)
	s.globalReady = true
	s.mu.Unlock()
	return nil
}

func (s *SourceControlService) GetTenant(ctx context.Context, tenantID string) (*SourceControlConfig, error) {
	tenantID = strings.TrimSpace(tenantID)
	s.mu.RLock()
	if cfg, ok := s.tenantCache[tenantID]; ok {
		s.mu.RUnlock()
		return cloneSourceControlConfig(cfg), nil
	}
	s.mu.RUnlock()
	cfg, err := s.load(ctx, srcCtlKeyTenant+tenantID)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.tenantCache[tenantID] = cloneSourceControlConfig(cfg)
	s.mu.Unlock()
	return cloneSourceControlConfig(cfg), nil
}

func (s *SourceControlService) SetTenant(ctx context.Context, tenantID string, cfg *SourceControlConfig) error {
	tenantID = strings.TrimSpace(tenantID)
	if err := validateSourceControlConfig(cfg); err != nil {
		return err
	}
	stored := cloneSourceControlConfig(cfg)
	if err := s.save(ctx, srcCtlKeyTenant+tenantID, stored); err != nil {
		return err
	}
	s.mu.Lock()
	s.tenantCache[tenantID] = cloneSourceControlConfig(stored)
	s.mu.Unlock()
	return nil
}

func (s *SourceControlService) DeleteTenant(ctx context.Context, tenantID string) error {
	tenantID = strings.TrimSpace(tenantID)
	if err := s.kv.Set(ctx, srcCtlKeyTenant+tenantID, ""); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.tenantCache, tenantID)
	s.mu.Unlock()
	return nil
}

func (s *SourceControlService) GetUser(ctx context.Context, userID string) (*SourceControlConfig, error) {
	userID = strings.TrimSpace(userID)
	s.mu.RLock()
	if cfg, ok := s.userCache[userID]; ok {
		s.mu.RUnlock()
		return cloneSourceControlConfig(cfg), nil
	}
	s.mu.RUnlock()
	cfg, err := s.load(ctx, srcCtlKeyUser+userID)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.userCache[userID] = cloneSourceControlConfig(cfg)
	s.mu.Unlock()
	return cloneSourceControlConfig(cfg), nil
}

func (s *SourceControlService) GetTenantUser(ctx context.Context, tenantID, userID string) (*SourceControlConfig, error) {
	tenantID = strings.TrimSpace(tenantID)
	userID = strings.TrimSpace(userID)
	cacheKey := tenantUserCacheKey(tenantID, userID)
	s.mu.RLock()
	if cfg, ok := s.tenantUserCache[cacheKey]; ok {
		s.mu.RUnlock()
		return cloneSourceControlConfig(cfg), nil
	}
	s.mu.RUnlock()
	cfg, err := s.load(ctx, srcCtlKeyTenantUser+cacheKey)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.tenantUserCache[cacheKey] = cloneSourceControlConfig(cfg)
	s.mu.Unlock()
	return cloneSourceControlConfig(cfg), nil
}

func (s *SourceControlService) SetUser(ctx context.Context, userID string, cfg *SourceControlConfig) error {
	userID = strings.TrimSpace(userID)
	if err := validateSourceControlConfig(cfg); err != nil {
		return err
	}
	stored := cloneSourceControlConfig(cfg)
	if err := s.save(ctx, srcCtlKeyUser+userID, stored); err != nil {
		return err
	}
	s.mu.Lock()
	s.userCache[userID] = cloneSourceControlConfig(stored)
	s.mu.Unlock()
	return nil
}

func (s *SourceControlService) SetTenantUser(ctx context.Context, tenantID, userID string, cfg *SourceControlConfig) error {
	tenantID = strings.TrimSpace(tenantID)
	userID = strings.TrimSpace(userID)
	if err := validateSourceControlConfig(cfg); err != nil {
		return err
	}
	cacheKey := tenantUserCacheKey(tenantID, userID)
	stored := cloneSourceControlConfig(cfg)
	if err := s.save(ctx, srcCtlKeyTenantUser+cacheKey, stored); err != nil {
		return err
	}
	s.mu.Lock()
	s.tenantUserCache[cacheKey] = cloneSourceControlConfig(stored)
	s.mu.Unlock()
	return nil
}

func (s *SourceControlService) DeleteUser(ctx context.Context, userID string) error {
	userID = strings.TrimSpace(userID)
	if err := s.kv.Set(ctx, srcCtlKeyUser+userID, ""); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.userCache, userID)
	s.mu.Unlock()
	return nil
}

func (s *SourceControlService) DeleteTenantUser(ctx context.Context, tenantID, userID string) error {
	tenantID = strings.TrimSpace(tenantID)
	userID = strings.TrimSpace(userID)
	cacheKey := tenantUserCacheKey(tenantID, userID)
	if err := s.kv.Set(ctx, srcCtlKeyTenantUser+cacheKey, ""); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.tenantUserCache, cacheKey)
	s.mu.Unlock()
	return nil
}

// ResolveForUser computes the effective allowed sources for a user.
// Resolution priority: Tenant User > User > Tenant > Global > Default (all allowed).
// Returns nil when all sources are allowed.
func (s *SourceControlService) ResolveForUser(ctx context.Context, userID, tenantID string) []string {
	userID = strings.TrimSpace(userID)
	tenantID = strings.TrimSpace(tenantID)
	if tenantID != "" {
		if cfg, err := s.GetTenantUser(ctx, tenantID, userID); err == nil && cfg != nil && cfg.Enabled {
			return effectiveAllowedSources(cfg)
		}
	}
	if cfg, err := s.GetUser(ctx, userID); err == nil && cfg != nil && cfg.Enabled {
		return effectiveAllowedSources(cfg)
	}
	if tenantID != "" {
		if cfg, err := s.GetTenant(ctx, tenantID); err == nil && cfg != nil && cfg.Enabled {
			return effectiveAllowedSources(cfg)
		}
	}
	if cfg, err := s.GetGlobal(ctx); err == nil && cfg != nil && cfg.Enabled {
		return effectiveAllowedSources(cfg)
	}
	return nil
}

func effectiveAllowedSources(cfg *SourceControlConfig) []string {
	if cfg == nil || !cfg.Enabled {
		return nil
	}
	if len(cfg.AllowedSources) == 0 {
		return []string{}
	}
	return canonicalSourceList(cfg.AllowedSources)
}

func cloneSourceControlConfig(cfg *SourceControlConfig) *SourceControlConfig {
	if cfg == nil {
		return nil
	}
	out := *cfg
	if cfg.AllowedSources != nil {
		out.AllowedSources = append([]string(nil), cfg.AllowedSources...)
	}
	return &out
}

func tenantUserCacheKey(tenantID, userID string) string {
	return tenantID + ":" + userID
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
			return fmt.Errorf("invalid skill source: %q (valid: skillhub, clawhub, github, enterprise_hub, local)", s)
		}
	}
	return nil
}

func validateSourceControlConfig(cfg *SourceControlConfig) error {
	if cfg == nil {
		return fmt.Errorf("skill source config is required")
	}
	return ValidateSourceNames(cfg.AllowedSources)
}

// FormatSourcePolicyDenied returns the localized user-facing denial shown when
// a skill install source is outside the organization allow-list.
func FormatSourcePolicyDenied(source string, allowedSources []string) string {
	source = normalizeHubSearchSource(source)
	if source == "" {
		source = "skillhub"
	}
	allowed := "none"
	if len(allowedSources) > 0 {
		var normalized []string
		seen := map[string]bool{}
		for _, item := range allowedSources {
			v := normalizeHubSearchSource(item)
			if v == "" || seen[v] {
				continue
			}
			seen[v] = true
			normalized = append(normalized, v)
		}
		if len(normalized) > 0 {
			allowed = strings.Join(normalized, ", ")
		}
	}
	return fmt.Sprintf("当前企业策略不允许从该能力市场安装此 Skill（skill source: %s，允许来源：%s）。Your organization policy does not allow installing this skill from this capability marketplace (skill source: %s, allowed sources: %s).", source, allowed, source, allowed)
}

// IntersectSources computes the intersection of two allowed-sources lists.
// nil/empty means "all allowed". The intersection of nil with X is X.
// The intersection of two non-nil lists is the set intersection.
func IntersectSources(a, b []string) []string {
	if len(a) == 0 {
		return canonicalSourceList(b)
	}
	if len(b) == 0 {
		return canonicalSourceList(a)
	}
	set := make(map[string]bool, len(b))
	for _, s := range b {
		set[normalizeHubSearchSource(s)] = true
	}
	seen := make(map[string]bool, len(a))
	var result []string
	for _, s := range a {
		canon := normalizeHubSearchSource(s)
		if canon != "" && set[canon] && !seen[canon] {
			seen[canon] = true
			result = append(result, canon)
		}
	}
	return result
}

func validSourceName(s string) bool {
	switch normalizeHubSearchSource(s) {
	case "skillhub", "clawhub", "github", "enterprise_hub", "local":
		return true
	}
	return false
}

func canonicalSourceList(sources []string) []string {
	if len(sources) == 0 {
		return sources
	}
	result := make([]string, 0, len(sources))
	seen := make(map[string]bool, len(sources))
	for _, source := range sources {
		canon := normalizeHubSearchSource(source)
		if canon == "" || seen[canon] {
			continue
		}
		seen[canon] = true
		result = append(result, canon)
	}
	return result
}
