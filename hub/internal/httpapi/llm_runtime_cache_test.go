package httpapi

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
)

func TestLLMRuntimeCacheAvoidsRepeatedSettingsReads(t *testing.T) {
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	invalidateLLMRuntimeCaches(system)
	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "provider-a", Model: "gpt-test"}}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "group-a", Name: "Group A", Models: []llmservice.ModelServiceModel{{Name: "auto", ProviderIDs: []string{"provider-a"}}}}}, GlobalServiceGroupIDs: []string{"group-a"}}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}
	if _, err := SaveHubLLMPromptCacheConfig(ctx, system, HubLLMPromptCacheConfig{Enabled: true, TTLSeconds: 99, MemoryMaxEntries: 8, MemoryMaxBytes: 1024, DiskMaxBytes: 2048}); err != nil {
		t.Fatalf("save prompt cache config: %v", err)
	}

	system.ResetGetCounts()
	invalidateLLMRuntimeCaches(system)

	for i := 0; i < 2; i++ {
		if _, err := loadCachedLLMProviderRegistry(ctx, system); err != nil {
			t.Fatalf("load provider registry #%d: %v", i+1, err)
		}
		if _, err := loadCachedLLMServiceRegistry(ctx, system); err != nil {
			t.Fatalf("load service registry #%d: %v", i+1, err)
		}
		cfg := loadCachedHubLLMPromptCacheConfig(ctx, system)
		if cfg.TTLSeconds != 99 {
			t.Fatalf("prompt cache ttl = %d, want 99", cfg.TTLSeconds)
		}
	}

	if got := system.GetCount(im.LLMProviderRegistryKey); got != 1 {
		t.Fatalf("provider registry Get count = %d, want 1", got)
	}
	if got := system.GetCount(llmservice.RegistryKey); got != 1 {
		t.Fatalf("service registry Get count = %d, want 1", got)
	}
	if got := system.GetCount(hubLLMPromptCacheConfigKey); got != 1 {
		t.Fatalf("prompt cache config Get count = %d, want 1", got)
	}
}

func TestLLMRuntimeCacheReturnsClones(t *testing.T) {
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	invalidateLLMRuntimeCaches(system)
	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "provider-a", Model: "gpt-test"}}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "group-a", Name: "Group A", Models: []llmservice.ModelServiceModel{{Name: "auto", ProviderIDs: []string{"provider-a"}}}}}, GlobalServiceGroupIDs: []string{"group-a"}}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}

	system.ResetGetCounts()
	invalidateLLMRuntimeCaches(system)

	providerReg1, err := loadCachedLLMProviderRegistry(ctx, system)
	if err != nil {
		t.Fatalf("load provider registry: %v", err)
	}
	providerReg1.Providers[0].Model = "mutated"
	providerReg2, err := loadCachedLLMProviderRegistry(ctx, system)
	if err != nil {
		t.Fatalf("reload provider registry: %v", err)
	}
	if providerReg2.Providers[0].Model != "gpt-test" {
		t.Fatalf("provider cache returned shared mutable state: %#v", providerReg2.Providers[0])
	}
	if got := system.GetCount(im.LLMProviderRegistryKey); got != 1 {
		t.Fatalf("provider registry Get count = %d, want 1", got)
	}

	serviceReg1, err := loadCachedLLMServiceRegistry(ctx, system)
	if err != nil {
		t.Fatalf("load service registry: %v", err)
	}
	group1 := serviceReg1.FindModelServiceGroup("group-a")
	if group1 == nil || len(group1.Models) == 0 || len(group1.Models[0].ProviderIDs) == 0 {
		t.Fatalf("service registry missing expected model group: %#v", serviceReg1.ModelServiceGroups)
	}
	group1.Models[0].ProviderIDs[0] = "mutated"
	serviceReg1.GlobalServiceGroupIDs[0] = "mutated-global"
	serviceReg2, err := loadCachedLLMServiceRegistry(ctx, system)
	if err != nil {
		t.Fatalf("reload service registry: %v", err)
	}
	group2 := serviceReg2.FindModelServiceGroup("group-a")
	if group2 == nil || len(group2.Models) == 0 || len(group2.Models[0].ProviderIDs) == 0 {
		t.Fatalf("reloaded service registry missing expected model group: %#v", serviceReg2.ModelServiceGroups)
	}
	if group2.Models[0].ProviderIDs[0] != "provider-a" {
		t.Fatalf("service cache returned shared mutable state: %#v", group2.Models[0].ProviderIDs)
	}
	if len(serviceReg2.GlobalServiceGroupIDs) != 1 || serviceReg2.GlobalServiceGroupIDs[0] != "group-a" {
		t.Fatalf("service cache returned shared global groups: %#v", serviceReg2.GlobalServiceGroupIDs)
	}
	if got := system.GetCount(llmservice.RegistryKey); got != 1 {
		t.Fatalf("service registry Get count = %d, want 1", got)
	}
}

func TestSaveHubLLMPromptCacheConfigInvalidatesRuntimeCache(t *testing.T) {
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	invalidateLLMRuntimeCaches(system)
	if _, err := SaveHubLLMPromptCacheConfig(ctx, system, HubLLMPromptCacheConfig{Enabled: true, TTLSeconds: 30, MemoryMaxEntries: 4, MemoryMaxBytes: 512, DiskMaxBytes: 1024}); err != nil {
		t.Fatalf("save initial prompt cache config: %v", err)
	}

	system.ResetGetCounts()
	invalidateLLMRuntimeCaches(system)

	cfg := loadCachedHubLLMPromptCacheConfig(ctx, system)
	if cfg.TTLSeconds != 30 {
		t.Fatalf("initial ttl = %d, want 30", cfg.TTLSeconds)
	}
	if got := system.GetCount(hubLLMPromptCacheConfigKey); got != 1 {
		t.Fatalf("prompt cache config Get count after first load = %d, want 1", got)
	}

	if _, err := SaveHubLLMPromptCacheConfig(ctx, system, HubLLMPromptCacheConfig{Enabled: true, TTLSeconds: 45, MemoryMaxEntries: 4, MemoryMaxBytes: 512, DiskMaxBytes: 1024}); err != nil {
		t.Fatalf("save updated prompt cache config: %v", err)
	}
	cfg = loadCachedHubLLMPromptCacheConfig(ctx, system)
	if cfg.TTLSeconds != 45 {
		t.Fatalf("updated ttl = %d, want 45", cfg.TTLSeconds)
	}
	if got := system.GetCount(hubLLMPromptCacheConfigKey); got != 2 {
		t.Fatalf("prompt cache config Get count after invalidated reload = %d, want 2", got)
	}
}
