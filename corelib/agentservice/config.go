package agentservice

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

func DefaultParameterDefinitions() []ParameterDefinition {
	defs := []ParameterDefinition{
		{Key: "maclaw_llm_url", Title: "LLM URL", Description: "Legacy flat LLM endpoint URL.", Required: false, Type: "string", Example: "https://api.openai.com/v1"},
		{Key: "maclaw_llm_key", Title: "LLM API Key", Description: "Legacy flat API key or bearer token.", Required: false, Secret: true, Type: "string", Example: "sk-***"},
		{Key: "maclaw_llm_model", Title: "LLM Model", Description: "Legacy flat default model.", Required: false, Type: "string", Example: "gpt-5.4"},
		{Key: "maclaw_llm_current_provider", Title: "Current Provider", Description: "Selected provider name from maclaw_llm_providers.", Required: false, Type: "string", Example: "openai-prod"},
		{Key: "maclaw_llm_providers", Title: "LLM Providers", Description: "Provider list. When configured, MaClawSrv prefers the selected provider over legacy flat fields.", Required: false, Type: "array", Example: `[{"name":"openai-prod","url":"https://api.openai.com/v1","key":"sk-***","model":"gpt-5.4","wire_api":"responses"}]`},
		{Key: "mcp_servers", Title: "Remote MCP Servers", Description: "Remote MCP server registry shared by all user assistant instances.", Required: false, Type: "array", Example: `[{"id":"docs","name":"Docs","endpoint_url":"https://mcp.example/sse","auth_type":"bearer","auth_secret":"token"}]`},
		{Key: "local_mcp_servers", Title: "Local MCP Servers", Description: "Local MCP stdio server registry shared by all user assistant instances.", Required: false, Type: "array", Example: `[{"id":"local-tools","name":"Local Tools","command":"node","args":["server.js"],"env":{"TOKEN":"***"}}]`},
		{Key: "ssh_hosts", Title: "SSH Hosts", Description: "Preconfigured SSH host labels available to user assistant instances. Label-based hosts enable the SSH tool without allowing arbitrary direct SSH targets.", Required: false, Type: "array", Example: `[{"label":"prod-web","host":"10.0.0.10","port":22,"user":"deploy","auth_method":"agent"}]`},
		{Key: "skill_hub_urls", Title: "Skill Hubs", Description: "Skill discovery sources for this user.", Required: false, Type: "array", Example: `[{"name":"default","url":"https://hub.example"}]`},
		{Key: "external_skill_dirs", Title: "External Skill Directories", Description: "Additional user skill directories.", Required: false, Type: "array", Example: `["D:/skills"]`},
		{Key: "skill_sources_allowed", Title: "Allowed Skill Sources", Description: "Optional allow-list for skill sources. Empty allows all configured sources.", Required: false, Type: "array", Example: `["skillhub","clawhub","github","local"]`},
		{Key: "memory_auto_compress", Title: "Memory Auto Compress", Description: "Enable automatic conversation and memory compression.", Required: false, Type: "bool", Example: "true"},
		{Key: "memory_max_backups", Title: "Memory Max Backups", Description: "Maximum memory backup count. Zero uses service default.", Required: false, Type: "integer", Example: "20"},
		{Key: "knowledge_skill_token_budget", Title: "Knowledge Skill Token Budget", Description: "Token budget for knowledge skill context packs. Zero uses service default.", Required: false, Type: "integer", Example: "12000"},
		{Key: "web_search_providers", Title: "Web Search Providers", Description: "Search provider configuration shared by user assistant instances.", Required: false, Type: "array", Example: `[{"name":"serpapi","type":"serpapi","key":"***"}]`},
		{Key: "web_search_current_provider", Title: "Current Web Search Provider", Description: "Selected provider name from web_search_providers.", Required: false, Type: "string", Example: "serpapi"},
		{Key: "security_policy_mode", Title: "Security Policy Mode", Description: "User-level security policy mode for tool and agent execution.", Required: false, Type: "string", Example: "standard"},
		{Key: "sandbox_mode", Title: "Sandbox Mode", Description: "Execution sandbox preference for this user.", Required: false, Type: "string", Example: "os"},
		{Key: "network_level", Title: "Network Level", Description: "Network access level for user tools and agents.", Required: false, Type: "string", Example: "intranet"},
		{Key: "yolo_mode_allowed", Title: "YOLO Mode Allowed", Description: "Allow this user to enable broad tool execution mode.", Required: false, Type: "bool", Example: "false"},
		{Key: "skill_runner_timeout_sec", Title: "Skill Runner Timeout", Description: "Default SkillRunner job and bash step timeout in seconds. Range: 240-14400; default: 600. Per-skill global_timeout overrides this value.", Required: false, Type: "integer", Example: "3600"},
	}
	return appendMissingAppConfigDefinitions(defs)
}

func SharedClientParameterDefinitions() []ParameterDefinition {
	defs := DefaultParameterDefinitions()
	seen := make(map[string]bool, len(defs))
	out := make([]ParameterDefinition, 0, len(defs))
	for _, def := range defs {
		seen[def.Key] = true
		if sharedClientConfigKeys[def.Key] {
			out = append(out, def)
		}
	}
	t := reflect.TypeOf(corelib.AppConfig{})
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		key := jsonFieldName(field)
		if key == "" || seen[key] || !sharedClientConfigKeys[key] {
			continue
		}
		out = append(out, ParameterDefinition{Key: key, Title: titleFromConfigKey(key), Description: "Shared client AppConfig field " + key + ".", Required: false, Secret: configKeyLooksSecret(key), Type: appConfigFieldType(field.Type)})
		seen[key] = true
	}
	return out
}

var sharedClientConfigKeys = map[string]bool{
	"maclaw_llm_url":                   true,
	"maclaw_llm_key":                   true,
	"maclaw_llm_model":                 true,
	"maclaw_llm_protocol":              true,
	"maclaw_llm_context_length":        true,
	"maclaw_llm_timeout_sec":           true,
	"agent_response_timeout_sec":       true,
	"skill_runner_timeout_sec":         true,
	"maclaw_llm_providers":             true,
	"maclaw_llm_current_provider":      true,
	"llm_prompt_cache":                 true,
	"maclaw_agent_max_iterations":      true,
	"subagent_concurrency":             true,
	"web_search_providers":             true,
	"web_search_current_provider":      true,
	"default_proxy_enabled":            true,
	"default_proxy_protocol":           true,
	"default_proxy_host":               true,
	"default_proxy_port":               true,
	"default_proxy_username":           true,
	"default_proxy_password":           true,
	"default_proxy_bypass":             true,
	"default_proxy_scope_maclaw":       true,
	"default_proxy_scope_coding_tools": true,
	"default_proxy_scope_agent":        true,
	"mcp_servers":                      true,
	"local_mcp_servers":                true,
	"ssh_hosts":                        true,
	"skill_hub_urls":                   true,
	"external_skill_dirs":              true,
	"skill_sources_allowed":            true,
	"security_policy_mode":             true,
	"hub_security_centralized":         true,
	"network_level":                    true,
	"network_allowlist":                true,
	"language":                         true,
	"ui_mode":                          true,
	"working_directory":                true,
	"vector_search_enabled":            true,
	"asr_enabled":                      true,
	"tts_voice_id":                     true,
	"tts_enabled":                      true,
	"im_progress_nudge_enabled":        true,
	"knowledge_vision_llm":             true,
	"knowledge_include_images":         true,
	"auxiliary_llm":                    true,
	"model_routes":                     true,
	"daily_llm_budget_usd":             true,
	"moa":                              true,
}

func appendMissingAppConfigDefinitions(defs []ParameterDefinition) []ParameterDefinition {
	seen := make(map[string]bool, len(defs))
	for _, def := range defs {
		seen[def.Key] = true
	}
	t := reflect.TypeOf(corelib.AppConfig{})
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		key := jsonFieldName(field)
		if key == "" || seen[key] || !appConfigFieldAvailableInMaClawSrv(key) {
			continue
		}
		defs = append(defs, ParameterDefinition{Key: key, Title: titleFromConfigKey(key), Description: "AppConfig field " + key + ".", Required: false, Secret: configKeyLooksSecret(key), Type: appConfigFieldType(field.Type)})
		seen[key] = true
	}
	return defs
}

func appConfigFieldAvailableInMaClawSrv(key string) bool {
	if _, ok := maclawSrvHiddenAppConfigKeys[key]; ok {
		return false
	}
	return true
}

var maclawSrvHiddenAppConfigKeys = map[string]struct{}{
	"claude":                           {},
	"codex":                            {},
	"opencode":                         {},
	"codebuddy":                        {},
	"iflow":                            {},
	"kilo":                             {},
	"projects":                         {},
	"current_project":                  {},
	"active_tool":                      {},
	"default_tool":                     {},
	"default_tool_provider":            {},
	"show_codex":                       {},
	"show_opencode":                    {},
	"show_codebuddy":                   {},
	"show_iflow":                       {},
	"show_kilo":                        {},
	"extra_tool_configs":               {},
	"default_proxy_scope_coding_tools": {},
	"use_windows_terminal":             {},
	"nl_skills":                        {},
}

func jsonFieldName(field reflect.StructField) string {
	if field.PkgPath != "" {
		return ""
	}
	tag := field.Tag.Get("json")
	if tag == "-" {
		return ""
	}
	name := strings.Split(tag, ",")[0]
	if name != "" {
		return name
	}
	return field.Name
}

func appConfigFieldType(t reflect.Type) string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Bool:
		return "bool"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Slice, reflect.Array:
		return "array"
	case reflect.Map, reflect.Struct:
		return "object"
	default:
		return "string"
	}
}

func titleFromConfigKey(key string) string {
	parts := strings.Split(key, "_")
	for i, part := range parts {
		if part == "" {
			continue
		}
		upper := strings.ToUpper(part)
		switch upper {
		case "LLM", "MCP", "MIS", "QQ", "ASR", "TTS", "UI", "URL", "ID", "API", "CDN", "WSS", "YOLO", "IM", "VAD":
			parts[i] = upper
		default:
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return strings.Join(parts, " ")
}

func configKeyLooksSecret(key string) bool {
	key = strings.ToLower(key)
	if strings.Contains(key, "token_usage") || strings.Contains(key, "token_budget") {
		return false
	}
	for _, marker := range []string{"key", "secret", "token", "password", "credential"} {
		if strings.Contains(key, marker) {
			return true
		}
	}
	return false
}

func SanitizeAppConfig(cfg corelib.AppConfig) corelib.AppConfig {
	cfg.DefaultProxyPassword = maskSecret(cfg.DefaultProxyPassword)
	cfg.RemoteMachineToken = maskSecret(cfg.RemoteMachineToken)
	cfg.RemoteViewerToken = maskSecret(cfg.RemoteViewerToken)
	cfg.SkillMarketSessionToken = maskSecret(cfg.SkillMarketSessionToken)
	cfg.MISData.Token = maskSecret(cfg.MISData.Token)
	cfg.MaclawLLMKey = maskSecret(cfg.MaclawLLMKey)
	if len(cfg.MaclawLLMProviders) > 0 {
		providers := make([]corelib.MaclawLLMProvider, len(cfg.MaclawLLMProviders))
		copy(providers, cfg.MaclawLLMProviders)
		for i := range providers {
			providers[i].Key = maskSecret(providers[i].Key)
			providers[i].OAuthAccessToken = maskSecret(providers[i].OAuthAccessToken)
			providers[i].RefreshToken = maskSecret(providers[i].RefreshToken)
		}
		cfg.MaclawLLMProviders = providers
	}
	if len(cfg.WebSearchProviders) > 0 {
		providers := make([]corelib.WebSearchProvider, len(cfg.WebSearchProviders))
		copy(providers, cfg.WebSearchProviders)
		for i := range providers {
			providers[i].Key = maskSecret(providers[i].Key)
		}
		cfg.WebSearchProviders = providers
	}
	if len(cfg.MCPServers) > 0 {
		servers := make([]corelib.MCPServerEntry, len(cfg.MCPServers))
		copy(servers, cfg.MCPServers)
		for i := range servers {
			servers[i].AuthSecret = maskSecret(servers[i].AuthSecret)
			servers[i].Headers = maskStringMapValues(servers[i].Headers)
		}
		cfg.MCPServers = servers
	}
	if len(cfg.LocalMCPServers) > 0 {
		servers := make([]corelib.LocalMCPServerEntry, len(cfg.LocalMCPServers))
		copy(servers, cfg.LocalMCPServers)
		for i := range servers {
			servers[i].Env = maskStringMapValues(servers[i].Env)
		}
		cfg.LocalMCPServers = servers
	}
	cfg.QQBotAppSecret = maskSecret(cfg.QQBotAppSecret)
	cfg.TelegramBotToken = maskSecret(cfg.TelegramBotToken)
	cfg.WeixinToken = maskSecret(cfg.WeixinToken)
	cfg.LansengerAppSecret = maskSecret(cfg.LansengerAppSecret)
	cfg.ThirdPartyGatewayToken = maskSecret(cfg.ThirdPartyGatewayToken)
	cfg.AuxiliaryLLM.Key = maskSecret(cfg.AuxiliaryLLM.Key)
	if len(cfg.ModelRoutes) > 0 {
		routes := make(map[string]corelib.ModelRouteConfig, len(cfg.ModelRoutes))
		for key, route := range cfg.ModelRoutes {
			route.Key = maskSecret(route.Key)
			routes[key] = route
		}
		cfg.ModelRoutes = routes
	}
	if len(cfg.ExtraToolConfigs) > 0 {
		configs := make(map[string]corelib.ToolConfig, len(cfg.ExtraToolConfigs))
		for key, toolCfg := range cfg.ExtraToolConfigs {
			models := make([]corelib.ModelConfig, len(toolCfg.Models))
			copy(models, toolCfg.Models)
			for i := range models {
				models[i].ApiKey = maskSecret(models[i].ApiKey)
			}
			toolCfg.Models = models
			configs[key] = toolCfg
		}
		cfg.ExtraToolConfigs = configs
	}
	if len(cfg.SSHHosts) > 0 {
		hosts := make([]corelib.SSHHostEntry, len(cfg.SSHHosts))
		copy(hosts, cfg.SSHHosts)
		for i := range hosts {
			hosts[i].Password = maskSecret(hosts[i].Password)
			hosts[i].Passphrase = maskSecret(hosts[i].Passphrase)
		}
		cfg.SSHHosts = hosts
	}
	return cfg
}

func maskSecret(v string) string {
	if strings.TrimSpace(v) == "" {
		return ""
	}
	return "******"
}

func maskStringMapValues(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = maskSecret(value)
	}
	return out
}

func appConfigContainsMaskedSecrets(cfg corelib.AppConfig) bool {
	if maskedSecretPlaceholder(cfg.DefaultProxyPassword) || maskedSecretPlaceholder(cfg.RemoteMachineToken) || maskedSecretPlaceholder(cfg.RemoteViewerToken) || maskedSecretPlaceholder(cfg.SkillMarketSessionToken) || maskedSecretPlaceholder(cfg.MISData.Token) || maskedSecretPlaceholder(cfg.MaclawLLMKey) {
		return true
	}
	for _, provider := range cfg.MaclawLLMProviders {
		if maskedSecretPlaceholder(provider.Key) || maskedSecretPlaceholder(provider.OAuthAccessToken) || maskedSecretPlaceholder(provider.RefreshToken) {
			return true
		}
	}
	for _, provider := range cfg.WebSearchProviders {
		if maskedSecretPlaceholder(provider.Key) {
			return true
		}
	}
	for _, server := range cfg.MCPServers {
		if maskedSecretPlaceholder(server.AuthSecret) || stringMapContainsMaskedSecrets(server.Headers) {
			return true
		}
	}
	for _, server := range cfg.LocalMCPServers {
		if stringMapContainsMaskedSecrets(server.Env) {
			return true
		}
	}
	if maskedSecretPlaceholder(cfg.QQBotAppSecret) || maskedSecretPlaceholder(cfg.TelegramBotToken) || maskedSecretPlaceholder(cfg.WeixinToken) || maskedSecretPlaceholder(cfg.LansengerAppSecret) || maskedSecretPlaceholder(cfg.ThirdPartyGatewayToken) || maskedSecretPlaceholder(cfg.AuxiliaryLLM.Key) {
		return true
	}
	for _, route := range cfg.ModelRoutes {
		if maskedSecretPlaceholder(route.Key) {
			return true
		}
	}
	for _, toolCfg := range cfg.ExtraToolConfigs {
		for _, model := range toolCfg.Models {
			if maskedSecretPlaceholder(model.ApiKey) {
				return true
			}
		}
	}
	return false
}

func stringMapContainsMaskedSecrets(values map[string]string) bool {
	for _, value := range values {
		if maskedSecretPlaceholder(value) {
			return true
		}
	}
	return false
}

func maskedSecretPlaceholder(value string) bool {
	value = strings.TrimSpace(value)
	return value == "******" || value == "********" || strings.EqualFold(value, "__masked__")
}

func IsMaskedSecretPlaceholder(value string) bool {
	return maskedSecretPlaceholder(value)
}

func ValidateAppConfig(cfg corelib.AppConfig) ConfigValidationResult {
	issues := validateLLMConfig(cfg)
	issues = append(issues, validateSSHHostConfig(cfg)...)
	return ConfigValidationResult{Valid: len(issues) == 0, Issues: issues}
}

func ResolveLLMConfig(cfg corelib.AppConfig) (corelib.MaclawLLMConfig, error) {
	if provider, err := resolveSelectedProvider(cfg); err == nil {
		provider = corelib.NormalizeCodeGenSSOProvider(provider)
		key := resolveProviderSecret(provider)
		if isManagedByHubPlaceholder(strings.TrimSpace(provider.URL), key, strings.TrimSpace(provider.Model)) {
			return corelib.MaclawLLMConfig{}, fmt.Errorf("llm config still uses unresolved VE Platform managed-by-hub placeholder")
		}
		protocol := strings.TrimSpace(provider.Protocol)
		if protocol == "" && corelib.IsAnthropicWireAPI(provider.WireAPI) {
			protocol = "anthropic"
		}
		return corelib.MaclawLLMConfig{
			URL:            strings.TrimSpace(provider.URL),
			Key:            key,
			Model:          strings.TrimSpace(provider.Model),
			Protocol:       protocol,
			ContextLength:  provider.ContextLength,
			TimeoutSec:     provider.TimeoutSec,
			SupportsVision: provider.SupportsVision,
			AgentType:      provider.AgentType,
			WireAPI:        strings.TrimSpace(provider.WireAPI),
			ProviderName:   strings.TrimSpace(provider.Name),
		}, nil
	}

	url := strings.TrimSpace(cfg.MaclawLLMUrl)
	key := strings.TrimSpace(cfg.MaclawLLMKey)
	model := strings.TrimSpace(cfg.MaclawLLMModel)
	if url == "" || key == "" || model == "" {
		return corelib.MaclawLLMConfig{}, fmt.Errorf("llm config is incomplete")
	}
	if isManagedByHubPlaceholder(url, key, model) {
		return corelib.MaclawLLMConfig{}, fmt.Errorf("llm config still uses unresolved VE Platform managed-by-hub placeholder")
	}
	return corelib.MaclawLLMConfig{
		URL:           url,
		Key:           key,
		Model:         model,
		Protocol:      strings.TrimSpace(cfg.MaclawLLMProtocol),
		ContextLength: cfg.MaclawLLMContextLength,
		TimeoutSec:    cfg.MaclawLLMTimeoutSec,
	}, nil
}

func isManagedByHubPlaceholder(url, key, model string) bool {
	url = strings.ToLower(strings.TrimSpace(url))
	key = strings.ToLower(strings.TrimSpace(key))
	model = strings.ToLower(strings.TrimSpace(model))
	return strings.Contains(url, "managed-by-hub") || key == "managed-by-hub" || model == "managed-by-hub"
}

func resolveSelectedProvider(cfg corelib.AppConfig) (corelib.MaclawLLMProvider, error) {
	if len(cfg.MaclawLLMProviders) == 0 {
		return corelib.MaclawLLMProvider{}, fmt.Errorf("no llm providers configured")
	}
	selected := strings.TrimSpace(cfg.MaclawLLMCurrentProvider)
	if selected == "" {
		if len(cfg.MaclawLLMProviders) == 1 {
			return cfg.MaclawLLMProviders[0], nil
		}
		return corelib.MaclawLLMProvider{}, fmt.Errorf("maclaw_llm_current_provider is required when multiple providers are configured")
	}
	for _, provider := range cfg.MaclawLLMProviders {
		if provider.Name == selected || strings.EqualFold(provider.Name, selected) {
			return provider, nil
		}
	}
	return corelib.MaclawLLMProvider{}, fmt.Errorf("selected provider %q was not found", selected)
}

func resolveProviderSecret(provider corelib.MaclawLLMProvider) string {
	oauthToken := strings.TrimSpace(provider.OAuthAccessToken)
	key := strings.TrimSpace(provider.Key)
	if classifyProviderEndpointKind(provider.URL).PrefersOAuthToken() && oauthToken != "" {
		return oauthToken
	}
	switch strings.ToLower(strings.TrimSpace(provider.AuthType)) {
	case "oauth", "bearer", "sso":
		if oauthToken != "" {
			return oauthToken
		}
	}
	if key != "" {
		return key
	}
	return oauthToken
}

func normalizeLLMFlatConfig(cfg corelib.AppConfig) corelib.AppConfig {
	return projectSelectedProviderToFlat(cfg, false)
}

func effectiveLLMFlatConfig(cfg corelib.AppConfig) corelib.AppConfig {
	return projectSelectedProviderToFlat(cfg, true)
}

func projectSelectedProviderToFlat(cfg corelib.AppConfig, overwrite bool) corelib.AppConfig {
	provider, ok := resolveSelectedProviderForFlatConfig(cfg)
	if !ok {
		return cfg
	}
	provider = corelib.NormalizeCodeGenSSOProvider(provider)
	if overwrite || strings.TrimSpace(cfg.MaclawLLMUrl) == "" {
		cfg.MaclawLLMUrl = strings.TrimSpace(provider.URL)
	}
	if overwrite || secretPlaceholderOrEmpty(cfg.MaclawLLMKey) {
		cfg.MaclawLLMKey = resolveProviderSecret(provider)
	}
	if overwrite || strings.TrimSpace(cfg.MaclawLLMModel) == "" {
		cfg.MaclawLLMModel = strings.TrimSpace(provider.Model)
	}
	return cfg
}

func normalizeLLMConfigForSave(current, next corelib.AppConfig) corelib.AppConfig {
	for i := range next.MaclawLLMProviders {
		next.MaclawLLMProviders[i] = corelib.NormalizeCodeGenSSOProvider(next.MaclawLLMProviders[i])
	}
	provider, providerIndex, ok := resolveSelectedProviderForFlatConfigWithIndex(next)
	if !ok {
		return normalizeLLMFlatConfig(next)
	}
	if _, currentProviderOK := resolveSelectedProviderForFlatConfig(current); !currentProviderOK {
		return normalizeLLMFlatConfig(next)
	}
	currentEffective := effectiveLLMFlatConfig(current)
	flatURLChanged := llmFlatStringFieldShouldSync(current.MaclawLLMUrl, currentEffective.MaclawLLMUrl, next.MaclawLLMUrl)
	flatKeyChanged := llmFlatSecretFieldShouldSync(current.MaclawLLMKey, currentEffective.MaclawLLMKey, next.MaclawLLMKey)
	flatModelChanged := llmFlatStringFieldShouldSync(current.MaclawLLMModel, currentEffective.MaclawLLMModel, next.MaclawLLMModel)
	flatChanged := flatURLChanged || flatKeyChanged || flatModelChanged
	providerChanged := selectedLLMProviderChanged(current, next) || selectedLLMProviderConfigChanged(current, provider)
	if providerChanged {
		return projectSelectedProviderToFlat(next, true)
	}
	if flatChanged {
		if value := strings.TrimSpace(next.MaclawLLMUrl); flatURLChanged {
			provider.URL = value
		}
		if value := strings.TrimSpace(next.MaclawLLMKey); flatKeyChanged {
			provider.Key = value
		}
		if value := strings.TrimSpace(next.MaclawLLMModel); flatModelChanged {
			provider.Model = value
		}
		next.MaclawLLMProviders[providerIndex] = provider
		return projectSelectedProviderToFlat(next, true)
	}
	return normalizeLLMFlatConfig(next)
}

func selectedLLMProviderChanged(current, next corelib.AppConfig) bool {
	return strings.TrimSpace(current.MaclawLLMCurrentProvider) != strings.TrimSpace(next.MaclawLLMCurrentProvider)
}

func selectedLLMProviderConfigChanged(current corelib.AppConfig, nextProvider corelib.MaclawLLMProvider) bool {
	currentProvider, ok := resolveSelectedProviderForFlatConfig(current)
	if !ok {
		return false
	}
	return strings.TrimSpace(currentProvider.URL) != strings.TrimSpace(nextProvider.URL) ||
		resolveProviderSecret(currentProvider) != resolveProviderSecret(nextProvider) ||
		strings.TrimSpace(currentProvider.Model) != strings.TrimSpace(nextProvider.Model)
}

func llmFlatStringFieldShouldSync(currentRaw, currentEffective, next string) bool {
	value := strings.TrimSpace(next)
	return value != "" && value != strings.TrimSpace(currentEffective) && value != strings.TrimSpace(currentRaw)
}

func llmFlatSecretFieldShouldSync(currentRaw, currentEffective, next string) bool {
	value := strings.TrimSpace(next)
	return value != "" && !maskedSecretPlaceholder(value) && value != strings.TrimSpace(currentEffective) && value != strings.TrimSpace(currentRaw)
}

func resolveSelectedProviderForFlatConfig(cfg corelib.AppConfig) (corelib.MaclawLLMProvider, bool) {
	provider, _, ok := resolveSelectedProviderForFlatConfigWithIndex(cfg)
	return provider, ok
}

func resolveSelectedProviderForFlatConfigWithIndex(cfg corelib.AppConfig) (corelib.MaclawLLMProvider, int, bool) {
	if len(cfg.MaclawLLMProviders) == 0 {
		return corelib.MaclawLLMProvider{}, -1, false
	}
	selected := strings.TrimSpace(cfg.MaclawLLMCurrentProvider)
	if selected == "" && len(cfg.MaclawLLMProviders) == 1 {
		return cfg.MaclawLLMProviders[0], 0, true
	}
	for i, provider := range cfg.MaclawLLMProviders {
		if strings.EqualFold(strings.TrimSpace(provider.Name), selected) {
			return provider, i, true
		}
	}
	return corelib.MaclawLLMProvider{}, -1, false
}

func validateLLMConfig(cfg corelib.AppConfig) []ConfigValidationIssue {
	issues := make([]ConfigValidationIssue, 0)
	if len(cfg.MaclawLLMProviders) > 0 {
		provider, err := resolveSelectedProvider(cfg)
		if err != nil {
			issues = append(issues, ConfigValidationIssue{Key: "maclaw_llm_current_provider", Message: err.Error()})
			return issues
		}
		if strings.TrimSpace(provider.URL) == "" {
			issues = append(issues, ConfigValidationIssue{Key: "maclaw_llm_providers.url", Message: "Selected provider URL is required."})
		} else if isManagedByHubPlaceholder(provider.URL, "", "") {
			issues = append(issues, ConfigValidationIssue{Key: "maclaw_llm_providers.url", Message: "Selected provider URL still uses unresolved VE Platform managed-by-hub placeholder."})
		}
		if strings.TrimSpace(provider.Model) == "" {
			issues = append(issues, ConfigValidationIssue{Key: "maclaw_llm_providers.model", Message: "Selected provider model is required."})
		} else if isManagedByHubPlaceholder("", "", provider.Model) {
			issues = append(issues, ConfigValidationIssue{Key: "maclaw_llm_providers.model", Message: "Selected provider model still uses unresolved VE Platform managed-by-hub placeholder."})
		}
		providerSecret := resolveProviderSecret(provider)
		if strings.TrimSpace(providerSecret) == "" {
			issues = append(issues, ConfigValidationIssue{Key: "maclaw_llm_providers.key", Message: "Selected provider credential is required."})
		} else if isManagedByHubPlaceholder("", providerSecret, "") {
			issues = append(issues, ConfigValidationIssue{Key: "maclaw_llm_providers.key", Message: "Selected provider credential still uses unresolved VE Platform managed-by-hub placeholder."})
		}
		return issues
	}
	if strings.TrimSpace(cfg.MaclawLLMUrl) == "" {
		issues = append(issues, ConfigValidationIssue{Key: "maclaw_llm_url", Message: "LLM URL is required."})
	} else if isManagedByHubPlaceholder(cfg.MaclawLLMUrl, "", "") {
		issues = append(issues, ConfigValidationIssue{Key: "maclaw_llm_url", Message: "LLM URL still uses unresolved VE Platform managed-by-hub placeholder."})
	}
	if strings.TrimSpace(cfg.MaclawLLMKey) == "" {
		issues = append(issues, ConfigValidationIssue{Key: "maclaw_llm_key", Message: "LLM API key is required."})
	} else if isManagedByHubPlaceholder("", cfg.MaclawLLMKey, "") {
		issues = append(issues, ConfigValidationIssue{Key: "maclaw_llm_key", Message: "LLM API key still uses unresolved VE Platform managed-by-hub placeholder."})
	}
	if strings.TrimSpace(cfg.MaclawLLMModel) == "" {
		issues = append(issues, ConfigValidationIssue{Key: "maclaw_llm_model", Message: "LLM model is required."})
	} else if isManagedByHubPlaceholder("", "", cfg.MaclawLLMModel) {
		issues = append(issues, ConfigValidationIssue{Key: "maclaw_llm_model", Message: "LLM model still uses unresolved VE Platform managed-by-hub placeholder."})
	}
	return issues
}

func validateSSHHostConfig(cfg corelib.AppConfig) []ConfigValidationIssue {
	issues := make([]ConfigValidationIssue, 0)
	seen := make(map[string]int, len(cfg.SSHHosts))
	for i, host := range cfg.SSHHosts {
		prefix := fmt.Sprintf("ssh_hosts[%d]", i)
		label := strings.TrimSpace(host.Label)
		addr := strings.TrimSpace(host.Host)
		user := strings.TrimSpace(host.User)
		if label == "" {
			issues = append(issues, ConfigValidationIssue{Key: prefix + ".label", Message: "SSH host label is required."})
		} else {
			key := strings.ToLower(label)
			if first, ok := seen[key]; ok {
				issues = append(issues, ConfigValidationIssue{Key: prefix + ".label", Message: fmt.Sprintf("SSH host label duplicates ssh_hosts[%d].label.", first)})
			} else {
				seen[key] = i
			}
		}
		if addr == "" {
			issues = append(issues, ConfigValidationIssue{Key: prefix + ".host", Message: "SSH host address is required."})
		}
		if user == "" {
			issues = append(issues, ConfigValidationIssue{Key: prefix + ".user", Message: "SSH username is required."})
		}
		if host.Port < 0 || host.Port > 65535 {
			issues = append(issues, ConfigValidationIssue{Key: prefix + ".port", Message: "SSH port must be between 1 and 65535, or 0 to use default 22."})
		}
		switch strings.ToLower(strings.TrimSpace(host.AuthMethod)) {
		case "", "password", "key", "agent":
		default:
			issues = append(issues, ConfigValidationIssue{Key: prefix + ".auth_method", Message: "SSH auth_method must be password, key, or agent."})
		}
	}
	return issues
}

func loadUserConfigFromFile(path string) (UserConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return UserConfig{}, err
	}
	var cfg UserConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return UserConfig{}, err
	}
	return cfg, nil
}

func saveUserConfigToFile(path string, cfg UserConfig) error {
	if err := secureMkdirAll(filepath.Dir(path)); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func cloneAppConfig(cfg corelib.AppConfig) corelib.AppConfig {
	data, err := json.Marshal(cfg)
	if err != nil {
		return cfg
	}
	var out corelib.AppConfig
	if err := json.Unmarshal(data, &out); err != nil {
		return cfg
	}
	return out
}
func mergeSecretPreserving(current, next corelib.AppConfig) corelib.AppConfig {
	preserveMaskedSecretString(&next.DefaultProxyPassword, current.DefaultProxyPassword)
	preserveMaskedSecretString(&next.RemoteMachineToken, current.RemoteMachineToken)
	preserveMaskedSecretString(&next.RemoteViewerToken, current.RemoteViewerToken)
	preserveMaskedSecretString(&next.SkillMarketSessionToken, current.SkillMarketSessionToken)
	preserveMaskedSecretString(&next.MISData.Token, current.MISData.Token)
	preserveSecretString(&next.MaclawLLMKey, current.MaclawLLMKey)
	preserveMaclawLLMProviderSecrets(current.MaclawLLMProviders, next.MaclawLLMProviders)
	preserveWebSearchProviderSecrets(current.WebSearchProviders, next.WebSearchProviders)
	preserveMCPServerSecrets(current.MCPServers, next.MCPServers)
	preserveLocalMCPServerSecrets(current.LocalMCPServers, next.LocalMCPServers)
	preserveMaskedSecretString(&next.QQBotAppSecret, current.QQBotAppSecret)
	preserveMaskedSecretString(&next.TelegramBotToken, current.TelegramBotToken)
	preserveMaskedSecretString(&next.WeixinToken, current.WeixinToken)
	preserveMaskedSecretString(&next.LansengerAppSecret, current.LansengerAppSecret)
	preserveMaskedSecretString(&next.ThirdPartyGatewayToken, current.ThirdPartyGatewayToken)
	preserveMaskedSecretString(&next.AuxiliaryLLM.Key, current.AuxiliaryLLM.Key)
	preserveModelRouteSecrets(current.ModelRoutes, next.ModelRoutes)
	preserveExtraToolConfigSecrets(current.ExtraToolConfigs, next.ExtraToolConfigs)
	return next
}

func preserveSecretString(next *string, current string) {
	if next == nil {
		return
	}
	if secretPlaceholderOrEmpty(*next) {
		*next = current
	}
}

func preserveMaskedSecretString(next *string, current string) {
	if next == nil {
		return
	}
	if maskedSecretPlaceholder(*next) {
		*next = current
	}
}

func secretPlaceholderOrEmpty(value string) bool {
	return strings.TrimSpace(value) == "" || maskedSecretPlaceholder(value)
}

func preserveMaclawLLMProviderSecrets(current, next []corelib.MaclawLLMProvider) {
	if len(next) == 0 {
		return
	}
	currentByKey := make(map[string]corelib.MaclawLLMProvider, len(current))
	for i, provider := range current {
		currentByKey[indexedSecretLookupKey(provider.Name, i)] = provider
	}
	for i := range next {
		currentProvider := corelib.MaclawLLMProvider{}
		if found, ok := currentByKey[indexedSecretLookupKey(next[i].Name, i)]; ok {
			currentProvider = found
		}
		preserveSecretString(&next[i].Key, currentProvider.Key)
		preserveMaskedSecretString(&next[i].OAuthAccessToken, currentProvider.OAuthAccessToken)
		preserveMaskedSecretString(&next[i].RefreshToken, currentProvider.RefreshToken)
	}
}

func preserveWebSearchProviderSecrets(current, next []corelib.WebSearchProvider) {
	if len(next) == 0 {
		return
	}
	currentByKey := make(map[string]corelib.WebSearchProvider, len(current))
	for i, provider := range current {
		for _, key := range webSearchProviderSecretLookupKeys(provider, i) {
			if _, exists := currentByKey[key]; !exists {
				currentByKey[key] = provider
			}
		}
	}
	for i := range next {
		currentProvider := corelib.WebSearchProvider{}
		for _, key := range webSearchProviderSecretLookupKeys(next[i], i) {
			if found, ok := currentByKey[key]; ok {
				currentProvider = found
				break
			}
		}
		preserveMaskedSecretString(&next[i].Key, currentProvider.Key)
	}
}

func preserveMCPServerSecrets(current, next []corelib.MCPServerEntry) {
	if len(next) == 0 {
		return
	}
	currentByKey := make(map[string]corelib.MCPServerEntry, len(current))
	for i, server := range current {
		currentByKey[mcpSecretLookupKey(server.ID, server.Name, i)] = server
	}
	for i := range next {
		currentServer := corelib.MCPServerEntry{}
		if found, ok := currentByKey[mcpSecretLookupKey(next[i].ID, next[i].Name, i)]; ok {
			currentServer = found
		}
		preserveMaskedSecretString(&next[i].AuthSecret, currentServer.AuthSecret)
		next[i].Headers = preserveStringMapSecretValues(currentServer.Headers, next[i].Headers)
	}
}

func preserveLocalMCPServerSecrets(current, next []corelib.LocalMCPServerEntry) {
	if len(next) == 0 {
		return
	}
	currentByKey := make(map[string]corelib.LocalMCPServerEntry, len(current))
	for i, server := range current {
		currentByKey[mcpSecretLookupKey(server.ID, server.Name, i)] = server
	}
	for i := range next {
		currentServer := corelib.LocalMCPServerEntry{}
		if found, ok := currentByKey[mcpSecretLookupKey(next[i].ID, next[i].Name, i)]; ok {
			currentServer = found
		}
		next[i].Env = preserveStringMapSecretValues(currentServer.Env, next[i].Env)
	}
}

func preserveModelRouteSecrets(current, next map[string]corelib.ModelRouteConfig) {
	if len(next) == 0 {
		return
	}
	for key, route := range next {
		currentRoute := corelib.ModelRouteConfig{}
		if found, ok := current[key]; ok {
			currentRoute = found
		}
		preserveMaskedSecretString(&route.Key, currentRoute.Key)
		next[key] = route
	}
}

func preserveExtraToolConfigSecrets(current, next map[string]corelib.ToolConfig) {
	if len(next) == 0 {
		return
	}
	for key, toolCfg := range next {
		currentCfg := corelib.ToolConfig{}
		if found, ok := current[key]; ok {
			currentCfg = found
		}
		currentByKey := make(map[string]corelib.ModelConfig, len(currentCfg.Models))
		for i, model := range currentCfg.Models {
			currentByKey[modelSecretLookupKey(model, i)] = model
		}
		for i := range toolCfg.Models {
			currentModel := corelib.ModelConfig{}
			if found, ok := currentByKey[modelSecretLookupKey(toolCfg.Models[i], i)]; ok {
				currentModel = found
			}
			preserveMaskedSecretString(&toolCfg.Models[i].ApiKey, currentModel.ApiKey)
		}
		next[key] = toolCfg
	}
}

func preserveStringMapSecretValues(current, next map[string]string) map[string]string {
	if len(next) == 0 {
		return next
	}
	out := make(map[string]string, len(next))
	for key, value := range next {
		if maskedSecretPlaceholder(value) {
			if currentValue, ok := lookupStringMapValue(current, key); ok {
				out[key] = currentValue
				continue
			}
			out[key] = ""
			continue
		}
		out[key] = value
	}
	return out
}

func lookupStringMapValue(values map[string]string, key string) (string, bool) {
	if value, ok := values[key]; ok {
		return value, true
	}
	for currentKey, value := range values {
		if strings.EqualFold(currentKey, key) {
			return value, true
		}
	}
	return "", false
}

func indexedSecretLookupKey(name string, index int) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Sprintf("#%d", index)
	}
	return strings.ToLower(name)
}

func webSearchProviderSecretLookupKeys(provider corelib.WebSearchProvider, index int) []string {
	keys := []string{"name:" + indexedSecretLookupKey(provider.Name, index)}
	if provider.Type = strings.TrimSpace(provider.Type); provider.Type != "" {
		keys = append(keys, "type:"+strings.ToLower(provider.Type)+fmt.Sprintf("#%d", index))
		if baseURL := strings.TrimSpace(provider.BaseURL); baseURL != "" {
			keys = append(keys, "type-base:"+strings.ToLower(provider.Type)+"|"+strings.ToLower(baseURL))
		}
	}
	return keys
}

func mcpSecretLookupKey(id, name string, index int) string {
	if id = strings.TrimSpace(id); id != "" {
		return "id:" + strings.ToLower(id)
	}
	return "name:" + indexedSecretLookupKey(name, index)
}

func modelSecretLookupKey(model corelib.ModelConfig, index int) string {
	if id := strings.TrimSpace(model.ModelId); id != "" {
		return "id:" + strings.ToLower(id)
	}
	return "name:" + indexedSecretLookupKey(model.ModelName, index)
}
