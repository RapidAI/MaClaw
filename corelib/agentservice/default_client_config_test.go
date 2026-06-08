package agentservice

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestSharedClientConfigAppliesToAllUsers(t *testing.T) {
	ctx := context.Background()
	svc, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, NewMemoryStore(), EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(ctx, CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	existingUser, err := svc.CreateUser(ctx, CreateUserInput{TenantID: tenant.ID, Name: "Existing User"})
	if err != nil {
		t.Fatalf("CreateUser existing: %v", err)
	}
	existingPrincipal := Principal{TenantID: tenant.ID, UserID: existingUser.ID}
	if _, err := svc.UpdateUserConfig(ctx, existingPrincipal, corelib.AppConfig{
		WebSearchCurrentProvider: "user-search",
		DefaultProxyHost:         "user.proxy",
		NetworkLevel:             "full",
		ExternalSkillDirs:        []string{"/user/skills"},
		MaclawLLMUrl:             "https://llm.example/v1",
		MaclawLLMKey:             "user-llm-key",
		MaclawLLMModel:           "user-model",
		VectorSearchEnabled:      false,
		ASREnabled:               false,
		TTSEnabled:               false,
		TTSVoiceID:               "user-voice",
	}); err != nil {
		t.Fatalf("UpdateUserConfig existing: %v", err)
	}

	sharedCfg := corelib.AppConfig{
		WebSearchProviders: []corelib.WebSearchProvider{{
			Name: "corp-search",
			Type: "serpapi",
		}},
		WebSearchCurrentProvider: "serpapi",
		DefaultProxyEnabled:      true,
		DefaultProxyProtocol:     "socks5",
		DefaultProxyHost:         "proxy.internal",
		DefaultProxyPort:         "1080",
		DefaultProxyPassword:     "proxy-secret",
		DefaultProxyScopeAgent:   true,
		NetworkLevel:             "allowlist",
		NetworkAllowlist:         []string{"api.example.com", "*.corp.local"},
		ExternalSkillDirs:        []string{"/opt/maclaw/skills"},
		MaclawLLMUrl:             "https://shared-llm.example/v1",
		MaclawLLMKey:             "shared-llm-key",
		MaclawLLMModel:           "shared-model",
		MaclawLLMProtocol:        "openai",
		MaclawLLMContextLength:   32000,
		MaclawLLMTimeoutSec:      90,
		AgentResponseTimeoutSec:  120,
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:     "shared-main",
			URL:      "https://shared-llm.example/v1",
			Key:      "shared-provider-key",
			Model:    "shared-model",
			Protocol: "openai",
		}},
		MaclawLLMCurrentProvider: "shared-main",
		LLMPromptCache: corelib.LLMPromptCacheConfig{
			Enabled:                   true,
			TTLSeconds:                600,
			SingleflightWaitTimeoutMS: 7000,
		},
		MaclawAgentMaxIterations: 7,
		SubAgentConcurrency:      3,
		VectorSearchEnabled:      true,
		ASREnabled:               true,
		TTSEnabled:               true,
		TTSVoiceID:               "zf_xiaoyi",
		KnowledgeIncludeImages:   true,
		KnowledgeVisionLLM: corelib.KnowledgeVisionLLMConfig{
			Enabled:    true,
			BaseURL:    "https://vision.example/v1",
			APIKey:     "vision-key",
			Model:      "vision-model",
			MaxTokens:  700,
			TimeoutSec: 25,
			Verified:   true,
		},
		AuxiliaryLLM: corelib.AuxiliaryLLMConfig{
			URL:      "https://aux.example/v1",
			Key:      "aux-key",
			Model:    "aux-model",
			Protocol: "openai",
		},
		ModelRoutes: map[string]corelib.ModelRouteConfig{
			"intent": {Model: "intent-model", Provider: "shared-main"},
		},
		DailyLLMBudgetUSD: 6.5,
	}
	if _, err := svc.UpdateDefaultClientConfig(ctx, sharedCfg); err != nil {
		t.Fatalf("UpdateDefaultClientConfig: %v", err)
	}

	gotExisting, err := svc.getOrLoadUserConfig(tenant.ID, existingUser.ID)
	if err != nil {
		t.Fatalf("getOrLoadUserConfig existing: %v", err)
	}
	assertSharedClientConfigApplied(t, gotExisting.AppConfig)

	rawExisting, err := svc.GetRawUserConfig(ctx, existingPrincipal)
	if err != nil {
		t.Fatalf("GetRawUserConfig existing: %v", err)
	}
	if rawExisting.AppConfig.WebSearchCurrentProvider != "user-search" || rawExisting.AppConfig.DefaultProxyHost != "user.proxy" || rawExisting.AppConfig.NetworkLevel != "full" {
		t.Fatalf("raw user config should remain user-specific: %#v", rawExisting.AppConfig)
	}
	if rawExisting.AppConfig.MaclawLLMUrl != "https://llm.example/v1" || rawExisting.AppConfig.MaclawLLMKey != "user-llm-key" || rawExisting.AppConfig.MaclawLLMModel != "user-model" || rawExisting.AppConfig.TTSVoiceID != "user-voice" {
		t.Fatalf("raw user AI config should remain user-specific on disk: %#v", rawExisting.AppConfig)
	}

	newUser, err := svc.CreateUser(ctx, CreateUserInput{TenantID: tenant.ID, Name: "New User"})
	if err != nil {
		t.Fatalf("CreateUser new: %v", err)
	}
	gotNew, err := svc.getOrLoadUserConfig(tenant.ID, newUser.ID)
	if err != nil {
		t.Fatalf("getOrLoadUserConfig new: %v", err)
	}
	assertSharedClientConfigApplied(t, gotNew.AppConfig)
}

func TestDefaultClientConfigPersistsOnlySharedFields(t *testing.T) {
	ctx := context.Background()
	svc, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, NewMemoryStore(), EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if _, err := svc.UpdateDefaultClientConfig(ctx, corelib.AppConfig{
		WebSearchProviders:       []corelib.WebSearchProvider{{Name: "admin-search", Type: "serpapi", Key: "search-secret"}},
		WebSearchCurrentProvider: "admin-search",
		DefaultProxyPassword:     "proxy-secret",
		MaclawLLMUrl:             "https://private-llm.example/v1",
		MaclawLLMKey:             "private-key",
		MaclawLLMModel:           "private-model",
		MaclawLLMCurrentProvider: "shared-main",
		MaclawLLMProviders:       []corelib.MaclawLLMProvider{{Name: "shared-main", URL: "https://private-llm.example/v1", Key: "provider-key", Model: "private-model"}},
		LLMPromptCache:           corelib.LLMPromptCacheConfig{Enabled: true, TTLSeconds: 900},
		TTSVoiceID:               "zf_xiaoyi",
		AuxiliaryLLM:             corelib.AuxiliaryLLMConfig{URL: "https://aux.example/v1", Key: "aux-key", Model: "aux-model"},
		ModelRoutes:              map[string]corelib.ModelRouteConfig{"fast": {Model: "fast-model", Key: "route-key"}},
		KnowledgeVisionLLM:       corelib.KnowledgeVisionLLMConfig{Enabled: true, BaseURL: "https://vision.example/v1", APIKey: "vision-key", Model: "vision-model"},
		MemoryMaxBackups:         99,
	}); err != nil {
		t.Fatalf("UpdateDefaultClientConfig: %v", err)
	}
	raw, ok, err := svc.loadDefaultClientConfigIfExists()
	if err != nil || !ok {
		t.Fatalf("loadDefaultClientConfigIfExists: ok=%v err=%v", ok, err)
	}
	if raw.AppConfig.MaclawLLMUrl != "https://private-llm.example/v1" || raw.AppConfig.MaclawLLMKey != "private-key" || raw.AppConfig.MaclawLLMModel != "private-model" || raw.AppConfig.MemoryMaxBackups != 0 {
		t.Fatalf("default client config should persist shared AI fields but not private user-only fields: %#v", raw.AppConfig)
	}
	if raw.AppConfig.DefaultProxyPassword != "proxy-secret" || len(raw.AppConfig.WebSearchProviders) != 1 || raw.AppConfig.WebSearchProviders[0].Key != "search-secret" || len(raw.AppConfig.MaclawLLMProviders) != 1 || raw.AppConfig.MaclawLLMProviders[0].Key != "provider-key" || raw.AppConfig.AuxiliaryLLM.Key != "aux-key" || raw.AppConfig.ModelRoutes["fast"].Key != "route-key" {
		t.Fatalf("default client config should keep shared secrets before API sanitization: %#v", raw.AppConfig)
	}
	got, err := svc.GetDefaultClientConfig(ctx)
	if err != nil {
		t.Fatalf("GetDefaultClientConfig: %v", err)
	}
	if got.AppConfig.MaclawLLMUrl != "https://private-llm.example/v1" || got.AppConfig.MaclawLLMKey != "private-key" || got.AppConfig.MemoryMaxBackups != 0 {
		t.Fatalf("default client config response should expose shared AI fields: %#v", got.AppConfig)
	}
	if len(got.AppConfig.MaclawLLMProviders) != 1 || got.AppConfig.MaclawLLMProviders[0].Key != "provider-key" || got.AppConfig.AuxiliaryLLM.Key != "aux-key" || got.AppConfig.ModelRoutes["fast"].Key != "route-key" {
		t.Fatalf("default client config response should preserve stored shared AI secrets before form re-submit masking: %#v", got.AppConfig)
	}
}

func TestDefaultClientConfigPreservesMaskedSearchKeyWhenProviderRenamed(t *testing.T) {
	ctx := context.Background()
	svc, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, NewMemoryStore(), EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if _, err := svc.UpdateDefaultClientConfig(ctx, corelib.AppConfig{
		WebSearchProviders: []corelib.WebSearchProvider{{
			Name:    "Corp Search",
			Type:    "serpapi",
			BaseURL: "https://search.example/api",
			Key:     "search-secret",
		}},
		WebSearchCurrentProvider: "Corp Search",
	}); err != nil {
		t.Fatalf("UpdateDefaultClientConfig seed: %v", err)
	}
	if _, err := svc.UpdateDefaultClientConfig(ctx, corelib.AppConfig{
		WebSearchProviders: []corelib.WebSearchProvider{{
			Name:    "Corporate Search",
			Type:    "serpapi",
			BaseURL: "https://search.example/api",
			Key:     "******",
		}},
		WebSearchCurrentProvider: "Corporate Search",
	}); err != nil {
		t.Fatalf("UpdateDefaultClientConfig rename: %v", err)
	}
	raw, ok, err := svc.loadDefaultClientConfigIfExists()
	if err != nil || !ok {
		t.Fatalf("loadDefaultClientConfigIfExists: ok=%v err=%v", ok, err)
	}
	if len(raw.AppConfig.WebSearchProviders) != 1 || raw.AppConfig.WebSearchProviders[0].Key != "search-secret" {
		t.Fatalf("masked search key should survive provider rename: %#v", raw.AppConfig.WebSearchProviders)
	}
}

func assertSharedClientConfigApplied(t *testing.T, got corelib.AppConfig) {
	t.Helper()
	if got.WebSearchCurrentProvider != "serpapi" || len(got.WebSearchProviders) != 1 || got.WebSearchProviders[0].Name != "corp-search" {
		t.Fatalf("web search defaults not applied: %#v", got.WebSearchProviders)
	}
	if !got.DefaultProxyEnabled || got.DefaultProxyProtocol != "socks5" || got.DefaultProxyHost != "proxy.internal" || got.DefaultProxyPassword != "proxy-secret" || !got.DefaultProxyScopeAgent {
		t.Fatalf("proxy defaults not applied: %#v", got)
	}
	if got.NetworkLevel != "allowlist" || len(got.NetworkAllowlist) != 2 || got.NetworkAllowlist[1] != "*.corp.local" {
		t.Fatalf("network defaults not applied: %#v", got.NetworkAllowlist)
	}
	if len(got.ExternalSkillDirs) != 1 || got.ExternalSkillDirs[0] != "/opt/maclaw/skills" {
		t.Fatalf("skill dir defaults not applied: %#v", got.ExternalSkillDirs)
	}
	if got.MaclawLLMUrl != "https://shared-llm.example/v1" || got.MaclawLLMKey != "shared-llm-key" || got.MaclawLLMModel != "shared-model" || got.MaclawLLMCurrentProvider != "shared-main" {
		t.Fatalf("shared primary LLM defaults not applied: %#v", got)
	}
	if got.MaclawLLMProtocol != "openai" || got.MaclawLLMContextLength != 32000 || got.MaclawLLMTimeoutSec != 90 || got.AgentResponseTimeoutSec != 120 {
		t.Fatalf("shared LLM runtime defaults not applied: %#v", got)
	}
	if len(got.MaclawLLMProviders) != 1 || got.MaclawLLMProviders[0].Name != "shared-main" || got.MaclawAgentMaxIterations != 7 || got.SubAgentConcurrency != 3 {
		t.Fatalf("shared provider or iteration defaults not applied: %#v", got)
	}
	if !got.VectorSearchEnabled || !got.ASREnabled || !got.TTSEnabled || got.TTSVoiceID != "zf_xiaoyi" {
		t.Fatalf("shared local AI toggles not applied: %#v", got)
	}
	if !got.LLMPromptCache.Enabled || got.LLMPromptCache.TTLSeconds != 600 || got.LLMPromptCache.SingleflightWaitTimeoutMS != 7000 {
		t.Fatalf("shared prompt cache defaults not applied: %#v", got.LLMPromptCache)
	}
	if !got.KnowledgeIncludeImages || !got.KnowledgeVisionLLM.Enabled || got.KnowledgeVisionLLM.Model != "vision-model" {
		t.Fatalf("shared knowledge AI defaults not applied: %#v %#v", got.KnowledgeIncludeImages, got.KnowledgeVisionLLM)
	}
	if got.AuxiliaryLLM.Model != "aux-model" || got.ModelRoutes["intent"].Model != "intent-model" || got.DailyLLMBudgetUSD != 6.5 {
		t.Fatalf("shared advanced AI defaults not applied: %#v %#v %v", got.AuxiliaryLLM, got.ModelRoutes, got.DailyLLMBudgetUSD)
	}
}
