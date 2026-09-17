package httpapi

import (
	"context"
	"fmt"
	"log"
	"reflect"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// llmRuntimeCacheTTL bounds how long a cached registry survives without a
// invalidation signal. Registries are multi-MB JSON documents (tens of MB in
// production), so re-reading and re-unmarshalling on a short TTL dominates hub
// CPU when many machines poll service-account endpoints. Every mutation path
// (card store, provider config, credit billing, system-free, …) calls
// invalidateLLMRuntimeCaches, so a long TTL only delays convergence for
// changes written through paths that missed invalidation wiring.
const llmRuntimeCacheTTL = 60 * time.Second

type llmRuntimeCacheState struct {
	mu           sync.RWMutex
	providers    map[string]cachedLLMProviderRegistry
	services     map[string]cachedLLMServiceRegistry
	promptConfig map[string]cachedPromptCacheConfig
}

type cachedLLMProviderRegistry struct {
	loadedAt time.Time
	value    *im.LLMProviderRegistry
	err      error
}

type cachedLLMServiceRegistry struct {
	loadedAt time.Time
	value    *llmservice.Registry
	err      error
}

type cachedPromptCacheConfig struct {
	loadedAt time.Time
	value    HubLLMPromptCacheConfig
}

var globalLLMRuntimeCache = &llmRuntimeCacheState{
	providers:    map[string]cachedLLMProviderRegistry{},
	services:     map[string]cachedLLMServiceRegistry{},
	promptConfig: map[string]cachedPromptCacheConfig{},
}

func loadCachedLLMProviderRegistry(ctx context.Context, system store.SystemSettingsRepository) (*im.LLMProviderRegistry, error) {
	if system == nil {
		return im.LoadLLMProviderRegistry(ctx, system)
	}
	key := llmRuntimeCacheKey(system)
	now := time.Now()
	globalLLMRuntimeCache.mu.RLock()
	entry, ok := globalLLMRuntimeCache.providers[key]
	globalLLMRuntimeCache.mu.RUnlock()
	if ok && now.Sub(entry.loadedAt) < llmRuntimeCacheTTL {
		return cloneLLMProviderRegistry(entry.value), entry.err
	}
	reg, err := im.LoadLLMProviderRegistry(ctx, system)
	globalLLMRuntimeCache.mu.Lock()
	globalLLMRuntimeCache.providers[key] = cachedLLMProviderRegistry{loadedAt: now, value: cloneLLMProviderRegistry(reg), err: err}
	globalLLMRuntimeCache.mu.Unlock()
	return cloneLLMProviderRegistry(reg), err
}

func loadCachedLLMServiceRegistryForViewer(ctx context.Context, system store.SystemSettingsRepository, userID, email string) (*llmservice.Registry, error) {
	reg, err := loadCachedLLMServiceRegistry(ctx, system)
	if err != nil || reg == nil {
		return reg, err
	}
	if !llmservice.NeedsNewUserLimitCardBackfill(reg, userID, email) {
		return reg, nil
	}
	issued, ensureErr := llmservice.EnsureNewUserLimitCardForUserID(ctx, system, userID, email)
	if ensureErr == nil && issued {
		invalidateLLMRuntimeCaches(system)
		if fresh, loadErr := loadCachedLLMServiceRegistry(ctx, system); loadErr == nil && fresh != nil {
			reg = fresh
		}
	}
	if !llmservice.HasLiveNewUserLimitCard(reg, userID, email) {
		_ = llmservice.IssueNewUserLimitCards(reg, []llmservice.VoucherUser{{ID: userID, Email: email}}, time.Now().UTC())
	}
	return reg, nil
}

func loadCachedLLMServiceRegistry(ctx context.Context, system store.SystemSettingsRepository) (*llmservice.Registry, error) {
	if system == nil {
		return llmservice.LoadRegistry(ctx, system)
	}
	key := llmRuntimeCacheKey(system)
	now := time.Now()
	globalLLMRuntimeCache.mu.RLock()
	entry, ok := globalLLMRuntimeCache.services[key]
	globalLLMRuntimeCache.mu.RUnlock()
	if ok && now.Sub(entry.loadedAt) < llmRuntimeCacheTTL {
		return cloneLLMServiceRegistry(entry.value), entry.err
	}
	reg, err := llmservice.LoadRegistry(ctx, system)
	if err == nil && reg != nil {
		ledgerBefore := len(reg.BillingLedger)
		llmservice.TrimBillingLedgerForRegistry(reg)
		// Persist the shrink so the multi-MB registry row converges instead of
		// re-parsing the full append-only ledger on every reload.
		if len(reg.BillingLedger) != ledgerBefore {
			if saveErr := llmservice.SaveRegistry(ctx, system, reg); saveErr != nil {
				log.Printf("[llm-runtime-cache] trim billing ledger persist failed: %v", saveErr)
			}
		}
		// One-shot repair for historical metered grants that were queued behind
		// an active grant under the old redeem policy. After promote+save, later
		// loads see StartsAt <= now and skip this path.
		if n := llmservice.PromoteQueuedMeteredGrants(reg, time.Now().UTC()); n > 0 {
			if saveErr := llmservice.SaveRegistry(ctx, system, reg); saveErr != nil {
				// Keep the in-memory promotion for this response even if persist fails.
				_ = saveErr
			}
		}
	}
	globalLLMRuntimeCache.mu.Lock()
	globalLLMRuntimeCache.services[key] = cachedLLMServiceRegistry{loadedAt: now, value: cloneLLMServiceRegistry(reg), err: err}
	globalLLMRuntimeCache.mu.Unlock()
	return cloneLLMServiceRegistry(reg), err
}

func loadCachedHubLLMPromptCacheConfig(ctx context.Context, system store.SystemSettingsRepository) HubLLMPromptCacheConfig {
	if system == nil {
		return LoadHubLLMPromptCacheConfig(ctx, system)
	}
	key := llmRuntimeCacheKey(system)
	now := time.Now()
	globalLLMRuntimeCache.mu.RLock()
	entry, ok := globalLLMRuntimeCache.promptConfig[key]
	globalLLMRuntimeCache.mu.RUnlock()
	if ok && now.Sub(entry.loadedAt) < llmRuntimeCacheTTL {
		return entry.value
	}
	cfg := LoadHubLLMPromptCacheConfig(ctx, system)
	globalLLMRuntimeCache.mu.Lock()
	globalLLMRuntimeCache.promptConfig[key] = cachedPromptCacheConfig{loadedAt: now, value: cfg}
	globalLLMRuntimeCache.mu.Unlock()
	return cfg
}

func invalidateLLMRuntimeCaches(system store.SystemSettingsRepository) {
	if system == nil {
		return
	}
	key := llmRuntimeCacheKey(system)
	globalLLMRuntimeCache.mu.Lock()
	delete(globalLLMRuntimeCache.providers, key)
	delete(globalLLMRuntimeCache.services, key)
	delete(globalLLMRuntimeCache.promptConfig, key)
	globalLLMRuntimeCache.mu.Unlock()
}

func invalidateLLMEntitlementCaches(system store.SystemSettingsRepository) {
	if system == nil {
		return
	}
	key := llmRuntimeCacheKey(system)
	globalLLMRuntimeCache.mu.Lock()
	delete(globalLLMRuntimeCache.providers, key)
	delete(globalLLMRuntimeCache.services, key)
	globalLLMRuntimeCache.mu.Unlock()
}

// llmRuntimeCacheKey returns a stable identity for a settings repository.
// Handlers wrap the root repository in per-request decorators
// (tenantScopedSystemSettings, userReferralMetricSystemSettings), so keying on
// the wrapper itself — or formatting a wrapper struct with %v — produces a new
// key per request and silently disables the cache. Unwrap to the root
// repository and key on its pointer plus the tenant scope.
func llmRuntimeCacheKey(system store.SystemSettingsRepository) string {
	if system == nil {
		return "<nil>"
	}
	tenantID := ""
	root := system
	for root != nil {
		switch v := root.(type) {
		case tenantScopedSystemSettings:
			if tenantID == "" {
				tenantID = v.tenantID
			}
			root = v.base
		case *tenantScopedSystemSettings:
			if tenantID == "" {
				tenantID = v.tenantID
			}
			root = v.base
		case userReferralMetricSystemSettings:
			root = v.SystemSettingsRepository
		case *userReferralMetricSystemSettings:
			root = v.SystemSettingsRepository
		default:
			goto unwrapped
		}
	}
unwrapped:
	if root == nil {
		if tenantID == "" {
			return "<nil>"
		}
		return tenantID + "|<nil>"
	}
	rv := reflect.ValueOf(root)
	var ptr uintptr
	switch rv.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		ptr = rv.Pointer()
	}
	return fmt.Sprintf("%s|%T:%x", tenantID, root, ptr)
}

func cloneLLMProviderRegistry(reg *im.LLMProviderRegistry) *im.LLMProviderRegistry {
	if reg == nil {
		return nil
	}
	clone := *reg
	clone.Providers = append([]im.LLMProvider(nil), reg.Providers...)
	// Deep-copy each provider's pricing (presence-aware cache prices and any
	// time-window overrides) so a configuration reload can never rewrite
	// memory a frozen quote still references.
	for i := range clone.Providers {
		clone.Providers[i].TokenPricing = clone.Providers[i].TokenPricing.Clone()
	}
	clone.TokenUsage = corelib.FilterRemoteCodingToolTokenUsage(reg.TokenUsage)
	return &clone
}

func cloneLLMServiceRegistry(reg *llmservice.Registry) *llmservice.Registry {
	if reg == nil {
		return nil
	}
	clone := *reg
	clone.ModelServiceGroups = make([]llmservice.ModelServiceGroup, len(reg.ModelServiceGroups))
	for i, group := range reg.ModelServiceGroups {
		clone.ModelServiceGroups[i] = group
		clone.ModelServiceGroups[i].Routes = append([]llmpool.WorkloadRoute(nil), group.Routes...)
		clone.ModelServiceGroups[i].ExposedModels = append([]string(nil), group.ExposedModels...)
		clone.ModelServiceGroups[i].Models = make([]llmservice.ModelServiceModel, len(group.Models))
		for j, model := range group.Models {
			clone.ModelServiceGroups[i].Models[j] = model
			clone.ModelServiceGroups[i].Models[j].ProviderIDs = append([]string(nil), model.ProviderIDs...)
			clone.ModelServiceGroups[i].Models[j].CapabilityTags = append([]string(nil), model.CapabilityTags...)
			clone.ModelServiceGroups[i].Models[j].ProviderConfigs = make([]llmservice.ModelServiceProviderConfig, len(model.ProviderConfigs))
			for k, cfg := range model.ProviderConfigs {
				clone.ModelServiceGroups[i].Models[j].ProviderConfigs[k] = cfg
				clone.ModelServiceGroups[i].Models[j].ProviderConfigs[k].CapabilityTags = append([]string(nil), cfg.CapabilityTags...)
			}
		}
	}
	clone.GlobalServiceGroupIDs = append([]string(nil), reg.GlobalServiceGroupIDs...)
	clone.GroupBindings = make([]llmservice.GroupBinding, len(reg.GroupBindings))
	for i, binding := range reg.GroupBindings {
		clone.GroupBindings[i] = binding
		clone.GroupBindings[i].ServiceGroupIDs = append([]string(nil), binding.ServiceGroupIDs...)
	}
	clone.UserBindings = make([]llmservice.UserBinding, len(reg.UserBindings))
	for i, binding := range reg.UserBindings {
		clone.UserBindings[i] = binding
		clone.UserBindings[i].ServiceGroupIDs = append([]string(nil), binding.ServiceGroupIDs...)
	}
	clone.Cards = append([]llmservice.RechargeCard(nil), reg.Cards...)
	clone.Grants = append([]llmservice.Grant(nil), reg.Grants...)
	clone.DefaultNewUserServiceGroups = append([]string(nil), reg.DefaultNewUserServiceGroups...)
	return &clone
}
