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
	}
	if _, err := svc.UpdateDefaultClientConfig(ctx, sharedCfg); err != nil {
		t.Fatalf("UpdateDefaultClientConfig: %v", err)
	}

	gotExisting, err := svc.getOrLoadUserConfig(tenant.ID, existingUser.ID)
	if err != nil {
		t.Fatalf("getOrLoadUserConfig existing: %v", err)
	}
	assertSharedClientConfigApplied(t, gotExisting.AppConfig)
	if gotExisting.AppConfig.MaclawLLMUrl != "https://llm.example/v1" || gotExisting.AppConfig.MaclawLLMKey != "user-llm-key" || gotExisting.AppConfig.MaclawLLMModel != "user-model" {
		t.Fatalf("private LLM config should remain user-specific: %#v", gotExisting.AppConfig)
	}

	rawExisting, err := svc.GetRawUserConfig(ctx, existingPrincipal)
	if err != nil {
		t.Fatalf("GetRawUserConfig existing: %v", err)
	}
	if rawExisting.AppConfig.WebSearchCurrentProvider != "user-search" || rawExisting.AppConfig.DefaultProxyHost != "user.proxy" || rawExisting.AppConfig.NetworkLevel != "full" {
		t.Fatalf("raw user config should remain user-specific: %#v", rawExisting.AppConfig)
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
		MemoryMaxBackups:         99,
	}); err != nil {
		t.Fatalf("UpdateDefaultClientConfig: %v", err)
	}
	raw, ok, err := svc.loadDefaultClientConfigIfExists()
	if err != nil || !ok {
		t.Fatalf("loadDefaultClientConfigIfExists: ok=%v err=%v", ok, err)
	}
	if raw.AppConfig.MaclawLLMUrl != "" || raw.AppConfig.MaclawLLMKey != "" || raw.AppConfig.MaclawLLMModel != "" || raw.AppConfig.MemoryMaxBackups != 0 {
		t.Fatalf("default client config should not persist private user fields: %#v", raw.AppConfig)
	}
	if raw.AppConfig.DefaultProxyPassword != "proxy-secret" || len(raw.AppConfig.WebSearchProviders) != 1 || raw.AppConfig.WebSearchProviders[0].Key != "search-secret" {
		t.Fatalf("default client config should keep shared secrets before API sanitization: %#v", raw.AppConfig)
	}
	got, err := svc.GetDefaultClientConfig(ctx)
	if err != nil {
		t.Fatalf("GetDefaultClientConfig: %v", err)
	}
	if got.AppConfig.MaclawLLMUrl != "" || got.AppConfig.MemoryMaxBackups != 0 {
		t.Fatalf("default client config response should expose only shared fields: %#v", got.AppConfig)
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
}
