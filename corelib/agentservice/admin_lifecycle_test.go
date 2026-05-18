package agentservice

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestDeleteUserRemovesStateAndDirectories(t *testing.T) {
	svc := newStatusTestService(t)
	tenant, user := createStatusTestUser(t, svc)
	principal := Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testDeleteLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	if _, err := svc.CreateSession(context.Background(), principal, inst.ID, CreateSessionInput{AgentID: "default"}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := svc.CreateCredential(context.Background(), CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "API", APIKey: "key-delete-user", APISecret: "secret-delete-user"}); err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}

	userRoot := svc.userRoot(tenant.ID, user.ID)
	if err := svc.DeleteUser(context.Background(), tenant.ID, user.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if _, err := svc.store.GetUser(tenant.ID, user.ID); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("GetUser error = %v, want ErrUserNotFound", err)
	}
	if _, err := os.Stat(userRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("user root should be removed, stat err = %v", err)
	}
}

func TestExportServiceStateRedactsCompactAuditSecretMarkers(t *testing.T) {
	svc := newStatusTestService(t)
	dataRoot := svc.DataRoot()
	if err := svc.RecordAuditEvent(context.Background(), AuditEvent{ActorType: "admin", Action: "secret.audit", ResourceType: "test", ResourceID: dataRoot, Metadata: map[string]string{"apikey": "metadata-compact-key", "apisecret": "metadata-compact-secret", "auth_header": "metadata-auth-token", "author": "visible-author", "message": "apikey=compact-key apisecret:compact-secret API Key = display-key API Secret: display-secret {\"apikey\":\"json-compact-key\",\"apisecret\":\"json-compact-secret\"} path=" + dataRoot}}); err != nil {
		t.Fatalf("RecordAuditEvent: %v", err)
	}
	out, err := svc.ExportServiceState(context.Background(), ExportServiceStateInput{IncludeAudit: true})
	if err != nil {
		t.Fatalf("ExportServiceState: %v", err)
	}
	data, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal export: %v", err)
	}
	text := string(data)
	for _, secret := range []string{"metadata-compact-key", "metadata-compact-secret", "metadata-auth-token", "compact-key", "compact-secret", "display-key", "display-secret", "json-compact-key", "json-compact-secret", dataRoot, filepath.ToSlash(dataRoot)} {
		if strings.Contains(text, secret) {
			t.Fatalf("expected redacted export value %q, got %s", secret, text)
		}
	}
	if !strings.Contains(text, "visible-author") {
		t.Fatalf("expected non-secret author metadata to remain visible, got %s", text)
	}
}

func TestExportServiceStateRedactsUserConfigSecrets(t *testing.T) {
	svc := newStatusTestService(t)
	tenant, user := createStatusTestUser(t, svc)
	principal := Principal{TenantID: tenant.ID, UserID: user.ID}
	cfg := testDeleteLLMConfig()
	cfg.DefaultProxyPassword = "proxy-password-secret"
	cfg.RemoteMachineToken = "remote-machine-secret"
	cfg.RemoteViewerToken = "remote-viewer-secret"
	cfg.SkillMarketSessionToken = "market-session-secret"
	cfg.MISData = corelib.MISDataConfig{Enabled: true, Endpoint: "https://mis.example", Token: "mis-token-secret"}
	cfg.MaclawLLMProviders = []corelib.MaclawLLMProvider{{Name: "primary", URL: "https://llm.example/v1", Key: "provider-key-secret", Model: "test-model", OAuthAccessToken: "provider-access-secret", RefreshToken: "provider-refresh-secret"}}
	cfg.WebSearchProviders = []corelib.WebSearchProvider{{Name: "search", Type: "api", Key: "web-search-secret", BaseURL: "https://search.example"}}
	cfg.MCPServers = []corelib.MCPServerEntry{{ID: "mcp-remote", Name: "Remote MCP", EndpointURL: "https://mcp.example", AuthType: "bearer", AuthSecret: "mcp-auth-secret", Headers: map[string]string{"Authorization": "Bearer mcp-header-secret", "X-Trace": "trace-value"}}}
	cfg.LocalMCPServers = []corelib.LocalMCPServerEntry{{ID: "mcp-local", Name: "Local MCP", Command: "node", Env: map[string]string{"LOCAL_MCP_TOKEN": "local-mcp-secret", "PLAIN_SETTING": "plain-setting"}}}
	cfg.QQBotAppSecret = "qq-secret"
	cfg.TelegramBotToken = "telegram-secret"
	cfg.WeixinToken = "weixin-secret"
	cfg.LansengerAppSecret = "lansenger-secret"
	cfg.ThirdPartyGatewayToken = "gateway-secret"
	cfg.AuxiliaryLLM = corelib.AuxiliaryLLMConfig{URL: "https://aux.example/v1", Key: "aux-secret", Model: "aux-model"}
	cfg.ModelRoutes = map[string]corelib.ModelRouteConfig{"intent": {Model: "intent-model", Key: "route-secret"}}
	cfg.ExtraToolConfigs = map[string]corelib.ToolConfig{"coder": {CurrentModel: "tool-model", Models: []corelib.ModelConfig{{ModelName: "Tool", ModelId: "tool-model", ApiKey: "tool-secret"}}}}

	if _, err := svc.UpdateUserConfig(context.Background(), principal, cfg); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	out, err := svc.ExportServiceState(context.Background(), ExportServiceStateInput{TenantID: tenant.ID, UserID: user.ID})
	if err != nil {
		t.Fatalf("ExportServiceState: %v", err)
	}
	data, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal export: %v", err)
	}
	text := string(data)
	for _, secret := range []string{"test-key", "proxy-password-secret", "remote-machine-secret", "remote-viewer-secret", "market-session-secret", "mis-token-secret", "provider-key-secret", "provider-access-secret", "provider-refresh-secret", "web-search-secret", "mcp-auth-secret", "mcp-header-secret", "trace-value", "local-mcp-secret", "plain-setting", "qq-secret", "telegram-secret", "weixin-secret", "lansenger-secret", "gateway-secret", "aux-secret", "route-secret", "tool-secret"} {
		if strings.Contains(text, secret) {
			t.Fatalf("expected redacted config secret %q, got %s", secret, text)
		}
	}
	if !strings.Contains(text, "LOCAL_MCP_TOKEN") || !strings.Contains(text, "Authorization") || !strings.Contains(text, "******") {
		t.Fatalf("expected config keys and mask placeholders to remain visible, got %s", text)
	}
	if !appConfigContainsMaskedSecrets(out.Users[0].Config.AppConfig) {
		t.Fatalf("expected masked secrets to be detected in exported config: %#v", out.Users[0].Config.AppConfig)
	}
	dryRun, err := svc.ImportServiceState(context.Background(), ImportServiceStateRequest{Data: *out, DryRun: true})
	if err != nil {
		t.Fatalf("ImportServiceState dry run: %v", err)
	}
	if len(dryRun.Warnings) == 0 || !strings.Contains(dryRun.Warnings[0], "masked secrets") {
		t.Fatalf("expected masked secret import warning, got %#v", dryRun.Warnings)
	}

	if out.Users[0].Config == nil {
		t.Fatalf("expected sanitized config in export")
	}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, out.Users[0].Config.AppConfig); err != nil {
		t.Fatalf("UpdateUserConfig with masked values: %v", err)
	}

	withSecrets, err := svc.ExportServiceState(context.Background(), ExportServiceStateInput{TenantID: tenant.ID, UserID: user.ID, IncludeSecrets: true})
	if err != nil {
		t.Fatalf("ExportServiceState with secrets: %v", err)
	}
	full, err := json.Marshal(withSecrets)
	if err != nil {
		t.Fatalf("marshal export with secrets: %v", err)
	}
	for _, secret := range []string{"test-key", "proxy-password-secret", "remote-machine-secret", "remote-viewer-secret", "market-session-secret", "mis-token-secret", "provider-key-secret", "provider-access-secret", "provider-refresh-secret", "web-search-secret", "mcp-auth-secret", "mcp-header-secret", "trace-value", "local-mcp-secret", "plain-setting", "qq-secret", "telegram-secret", "weixin-secret", "lansenger-secret", "gateway-secret", "aux-secret", "route-secret", "tool-secret"} {
		if !strings.Contains(string(full), secret) {
			t.Fatalf("expected include_secrets export to preserve %q, got %s", secret, full)
		}
	}
}
func TestImportServiceStateMaskedConfigDoesNotPersistPlaceholders(t *testing.T) {
	ctx := context.Background()
	source := newStatusTestService(t)
	tenant, user := createStatusTestUser(t, source)
	principal := Principal{TenantID: tenant.ID, UserID: user.ID}
	cfg := testDeleteLLMConfig()
	cfg.MCPServers = []corelib.MCPServerEntry{{ID: "mcp-remote", Name: "Remote MCP", AuthSecret: "mcp-auth-secret", Headers: map[string]string{"Authorization": "mcp-header-secret"}}}
	cfg.LocalMCPServers = []corelib.LocalMCPServerEntry{{ID: "mcp-local", Name: "Local MCP", Env: map[string]string{"LOCAL_MCP_TOKEN": "local-mcp-secret"}}}
	if _, err := source.UpdateUserConfig(ctx, principal, cfg); err != nil {
		t.Fatalf("UpdateUserConfig source: %v", err)
	}
	exported, err := source.ExportServiceState(ctx, ExportServiceStateInput{TenantID: tenant.ID, UserID: user.ID})
	if err != nil {
		t.Fatalf("ExportServiceState: %v", err)
	}
	if exported.Users[0].Config == nil || !appConfigContainsMaskedSecrets(exported.Users[0].Config.AppConfig) {
		t.Fatalf("expected masked config export: %#v", exported.Users[0].Config)
	}

	target := newStatusTestService(t)
	if _, err := target.ImportServiceState(ctx, ImportServiceStateRequest{Data: *exported}); err != nil {
		t.Fatalf("ImportServiceState new target: %v", err)
	}
	imported, err := target.store.GetUserConfig(tenant.ID, user.ID)
	if err != nil {
		t.Fatalf("GetUserConfig imported: %v", err)
	}
	if appConfigContainsMaskedSecrets(imported.AppConfig) || imported.AppConfig.MaclawLLMKey == "******" || imported.AppConfig.MCPServers[0].AuthSecret == "******" || imported.AppConfig.LocalMCPServers[0].Env["LOCAL_MCP_TOKEN"] == "******" {
		t.Fatalf("masked placeholders should be cleared on import without existing secrets: %#v", imported.AppConfig)
	}
	if imported.AppConfig.MaclawLLMKey != "" || imported.AppConfig.MCPServers[0].AuthSecret != "" || imported.AppConfig.LocalMCPServers[0].Env["LOCAL_MCP_TOKEN"] != "" {
		t.Fatalf("expected missing imported secrets to be empty for manual repair: %#v", imported.AppConfig)
	}

	existing := newStatusTestService(t)
	if err := existing.store.SaveTenant(*tenant); err != nil {
		t.Fatalf("SaveTenant existing: %v", err)
	}
	if err := existing.store.SaveUser(*user); err != nil {
		t.Fatalf("SaveUser existing: %v", err)
	}
	existingCfg := cfg
	existingCfg.MaclawLLMKey = "existing-key"
	existingCfg.MCPServers[0].AuthSecret = "existing-mcp-secret"
	existingCfg.MCPServers[0].Headers["Authorization"] = "existing-header-secret"
	existingCfg.LocalMCPServers[0].Env["LOCAL_MCP_TOKEN"] = "existing-local-secret"
	if _, err := existing.UpdateUserConfig(ctx, principal, existingCfg); err != nil {
		t.Fatalf("UpdateUserConfig existing: %v", err)
	}
	if _, err := existing.ImportServiceState(ctx, ImportServiceStateRequest{Data: *exported, Overwrite: true}); err != nil {
		t.Fatalf("ImportServiceState overwrite: %v", err)
	}
	preserved, err := existing.store.GetUserConfig(tenant.ID, user.ID)
	if err != nil {
		t.Fatalf("GetUserConfig preserved: %v", err)
	}
	if preserved.AppConfig.MaclawLLMKey != "existing-key" || preserved.AppConfig.MCPServers[0].AuthSecret != "existing-mcp-secret" || preserved.AppConfig.MCPServers[0].Headers["Authorization"] != "existing-header-secret" || preserved.AppConfig.LocalMCPServers[0].Env["LOCAL_MCP_TOKEN"] != "existing-local-secret" {
		t.Fatalf("expected masked import to preserve existing secrets on overwrite: %#v", preserved.AppConfig)
	}
}
func TestMergeSecretPreservingAllowsClearingOptionalSecrets(t *testing.T) {
	current := testDeleteLLMConfig()
	current.DefaultProxyPassword = "proxy-password-secret"
	current.RemoteMachineToken = "remote-machine-secret"
	current.MCPServers = []corelib.MCPServerEntry{{ID: "mcp-remote", Name: "Remote MCP", AuthSecret: "mcp-auth-secret", Headers: map[string]string{"Authorization": "mcp-header-secret"}}}
	current.LocalMCPServers = []corelib.LocalMCPServerEntry{{ID: "mcp-local", Name: "Local MCP", Env: map[string]string{"LOCAL_MCP_TOKEN": "local-mcp-secret"}}}
	current.WebSearchProviders = []corelib.WebSearchProvider{{Name: "search", Key: "web-search-secret"}}
	current.MaclawLLMProviders = []corelib.MaclawLLMProvider{{Name: "primary", URL: "https://llm.example/v1", Key: "provider-key-secret", Model: "test-model", OAuthAccessToken: "provider-access-secret", RefreshToken: "provider-refresh-secret"}}
	current.MaclawLLMCurrentProvider = "primary"
	current.ModelRoutes = map[string]corelib.ModelRouteConfig{"intent": {Model: "intent-model", Key: "route-secret"}}
	current.ExtraToolConfigs = map[string]corelib.ToolConfig{"coder": {Models: []corelib.ModelConfig{{ModelId: "tool-model", ApiKey: "tool-secret"}}}}

	next := testDeleteLLMConfig()
	next.MaclawLLMKey = "******"
	next.MaclawLLMProviders = []corelib.MaclawLLMProvider{{Name: "primary", URL: "https://llm.example/v1", Key: "******", Model: "test-model", OAuthAccessToken: "", RefreshToken: ""}}
	next.MaclawLLMCurrentProvider = "primary"
	next.MCPServers = []corelib.MCPServerEntry{{ID: "mcp-remote", Name: "Remote MCP", Headers: map[string]string{"Authorization": ""}}}
	next.LocalMCPServers = []corelib.LocalMCPServerEntry{{ID: "mcp-local", Name: "Local MCP", Env: map[string]string{"LOCAL_MCP_TOKEN": ""}}}
	next.WebSearchProviders = []corelib.WebSearchProvider{{Name: "search", Key: ""}}
	next.ModelRoutes = map[string]corelib.ModelRouteConfig{"intent": {Model: "intent-model", Key: ""}}
	next.ExtraToolConfigs = map[string]corelib.ToolConfig{"coder": {Models: []corelib.ModelConfig{{ModelId: "tool-model", ApiKey: ""}}}}

	merged := mergeSecretPreserving(current, next)
	if merged.MaclawLLMKey != "test-key" {
		t.Fatalf("expected required LLM key placeholder to preserve current key, got %q", merged.MaclawLLMKey)
	}
	if merged.MaclawLLMProviders[0].Key != "provider-key-secret" {
		t.Fatalf("expected required provider key placeholder to preserve current key, got %#v", merged.MaclawLLMProviders[0])
	}
	if merged.DefaultProxyPassword != "" || merged.RemoteMachineToken != "" || merged.MCPServers[0].AuthSecret != "" || merged.MCPServers[0].Headers["Authorization"] != "" || merged.LocalMCPServers[0].Env["LOCAL_MCP_TOKEN"] != "" || merged.WebSearchProviders[0].Key != "" || merged.MaclawLLMProviders[0].OAuthAccessToken != "" || merged.MaclawLLMProviders[0].RefreshToken != "" || merged.ModelRoutes["intent"].Key != "" || merged.ExtraToolConfigs["coder"].Models[0].ApiKey != "" {
		t.Fatalf("expected optional secrets to be clearable with empty values, got %#v", merged)
	}

	next.MaclawLLMProviders[0].OAuthAccessToken = "******"
	next.MaclawLLMProviders[0].RefreshToken = "******"
	next.MCPServers[0].AuthSecret = "******"
	next.MCPServers[0].Headers["Authorization"] = "******"
	next.LocalMCPServers[0].Env["LOCAL_MCP_TOKEN"] = "******"
	merged = mergeSecretPreserving(current, next)
	if merged.MaclawLLMProviders[0].OAuthAccessToken != "provider-access-secret" || merged.MaclawLLMProviders[0].RefreshToken != "provider-refresh-secret" || merged.MCPServers[0].AuthSecret != "mcp-auth-secret" || merged.MCPServers[0].Headers["Authorization"] != "mcp-header-secret" || merged.LocalMCPServers[0].Env["LOCAL_MCP_TOKEN"] != "local-mcp-secret" {
		t.Fatalf("expected masked optional secrets to preserve current values, got %#v", merged)
	}
}
func TestDeleteTenantRemovesStateAndDirectories(t *testing.T) {
	svc := newStatusTestService(t)
	tenant, user := createStatusTestUser(t, svc)
	principal := Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testDeleteLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	if _, err := svc.CreateInstance(context.Background(), principal, CreateInstanceInput{Name: "Instance"}); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}

	tenantRoot := filepath.Join(svc.dataRoot, "tenants", slugID(tenant.ID))
	if err := svc.DeleteTenant(context.Background(), tenant.ID); err != nil {
		t.Fatalf("DeleteTenant: %v", err)
	}
	if _, err := svc.store.GetTenant(tenant.ID); !errors.Is(err, ErrTenantNotFound) {
		t.Fatalf("GetTenant error = %v, want ErrTenantNotFound", err)
	}
	if _, err := os.Stat(tenantRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("tenant root should be removed, stat err = %v", err)
	}
}

func TestCredentialDetailUpdateAndRotate(t *testing.T) {
	svc := newStatusTestService(t)
	tenant, user := createStatusTestUser(t, svc)
	created, err := svc.CreateCredential(context.Background(), CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "Original", APIKey: "key-rotate", APISecret: "secret-old"})
	if err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}
	fetched, err := svc.GetCredential(context.Background(), tenant.ID, user.ID, created.ID)
	if err != nil {
		t.Fatalf("GetCredential: %v", err)
	}
	if fetched.APIKey == "" || fetched.APIKey == "key-rotate" || fetched.SecretDigest != "" {
		t.Fatalf("unexpected fetched credential: %#v", fetched)
	}
	updatedName := "Renamed"
	updated, err := svc.UpdateCredential(context.Background(), tenant.ID, user.ID, created.ID, UpdateCredentialInput{Name: &updatedName})
	if err != nil {
		t.Fatalf("UpdateCredential: %v", err)
	}
	if updated.Name != updatedName {
		t.Fatalf("updated name = %q, want %q", updated.Name, updatedName)
	}
	if _, err := svc.IssueToken(context.Background(), IssueTokenInput{APIKey: "key-rotate", APISecret: "secret-old"}); err != nil {
		t.Fatalf("IssueToken before rotate: %v", err)
	}
	rotated, err := svc.RotateCredentialSecret(context.Background(), tenant.ID, user.ID, created.ID, RotateCredentialSecretInput{APISecret: "secret-new"})
	if err != nil {
		t.Fatalf("RotateCredentialSecret: %v", err)
	}
	if rotated.SecretDigest != "" {
		t.Fatalf("rotated credential should be sanitized: %#v", rotated)
	}
	if _, err := svc.IssueToken(context.Background(), IssueTokenInput{APIKey: "key-rotate", APISecret: "secret-old"}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("old secret error = %v, want ErrUnauthorized", err)
	}
	if _, err := svc.IssueToken(context.Background(), IssueTokenInput{APIKey: "key-rotate", APISecret: "secret-new"}); err != nil {
		t.Fatalf("IssueToken with new secret: %v", err)
	}
	rotatedKey, err := svc.RotateCredentialAPIKey(context.Background(), tenant.ID, user.ID, created.ID, RotateCredentialKeyInput{APIKey: "key-rotated"})
	if err != nil {
		t.Fatalf("RotateCredentialAPIKey: %v", err)
	}
	if rotatedKey.APIKey == "" || rotatedKey.APIKey == "key-rotated" || rotatedKey.SecretDigest != "" {
		t.Fatalf("rotated key credential should be sanitized: %#v", rotatedKey)
	}
	if _, err := svc.IssueToken(context.Background(), IssueTokenInput{APIKey: "key-rotate", APISecret: "secret-new"}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("old key error = %v, want ErrUnauthorized", err)
	}
	if _, err := svc.IssueToken(context.Background(), IssueTokenInput{APIKey: "key-rotated", APISecret: "secret-new"}); err != nil {
		t.Fatalf("IssueToken with rotated key: %v", err)
	}
}

func testDeleteLLMConfig() corelib.AppConfig {
	return corelib.AppConfig{
		MaclawLLMUrl:   "https://llm.example/v1",
		MaclawLLMKey:   "test-key",
		MaclawLLMModel: "test-model",
	}
}
