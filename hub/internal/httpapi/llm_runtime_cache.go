package httpapi

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const llmRuntimeCacheTTL = 3 * time.Second

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

func llmRuntimeCacheKey(system store.SystemSettingsRepository) string {
	rv := reflect.ValueOf(system)
	if !rv.IsValid() {
		return "<nil>"
	}
	switch rv.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		return fmt.Sprintf("%T:%x", system, rv.Pointer())
	default:
		return fmt.Sprintf("%T:%v", system, system)
	}
}

func cloneLLMProviderRegistry(reg *im.LLMProviderRegistry) *im.LLMProviderRegistry {
	if reg == nil {
		return nil
	}
	clone := *reg
	clone.Providers = append([]im.LLMProvider(nil), reg.Providers...)
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
