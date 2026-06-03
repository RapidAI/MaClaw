package agentservice

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

func DefaultParameterDefinitions() []ParameterDefinition {
	return []ParameterDefinition{
		{Key: "maclaw_llm_url", Title: "LLM URL", Description: "Legacy flat LLM endpoint URL.", Required: false, Type: "string", Example: "https://api.openai.com/v1"},
		{Key: "maclaw_llm_key", Title: "LLM API Key", Description: "Legacy flat API key or bearer token.", Required: false, Secret: true, Type: "string", Example: "sk-***"},
		{Key: "maclaw_llm_model", Title: "LLM Model", Description: "Legacy flat default model.", Required: false, Type: "string", Example: "gpt-5.4"},
		{Key: "maclaw_llm_current_provider", Title: "Current Provider", Description: "Selected provider name from maclaw_llm_providers.", Required: false, Type: "string", Example: "openai-prod"},
		{Key: "maclaw_llm_providers", Title: "LLM Providers", Description: "Provider list. When configured, MaClawSrv prefers the selected provider over legacy flat fields.", Required: false, Type: "array", Example: `[{"name":"openai-prod","url":"https://api.openai.com/v1","key":"sk-***","model":"gpt-5.4","wire_api":"responses"}]`},
	}
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
	return strings.TrimSpace(value) == "******"
}

func ValidateAppConfig(cfg corelib.AppConfig) ConfigValidationResult {
	issues := validateLLMConfig(cfg)
	return ConfigValidationResult{Valid: len(issues) == 0, Issues: issues}
}

func ResolveLLMConfig(cfg corelib.AppConfig) (corelib.MaclawLLMConfig, error) {
	if provider, err := resolveSelectedProvider(cfg); err == nil {
		key := resolveProviderSecret(provider)
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
			WireAPI:        effectiveProviderWireAPI(provider, protocol),
			ProviderName:   strings.TrimSpace(provider.Name),
		}, nil
	}

	url := strings.TrimSpace(cfg.MaclawLLMUrl)
	key := strings.TrimSpace(cfg.MaclawLLMKey)
	model := strings.TrimSpace(cfg.MaclawLLMModel)
	if url == "" || key == "" || model == "" {
		return corelib.MaclawLLMConfig{}, fmt.Errorf("llm config is incomplete")
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

func effectiveProviderWireAPI(provider corelib.MaclawLLMProvider, protocol string) string {
	wireAPI := strings.TrimSpace(provider.WireAPI)
	if strings.EqualFold(strings.TrimSpace(protocol), "anthropic") {
		return wireAPI
	}
	model := strings.ToLower(strings.TrimSpace(provider.Model))
	normalizedWire := strings.ToLower(wireAPI)
	if strings.HasPrefix(model, "gpt-5") {
		switch normalizedWire {
		case "", "chat", "chat_completions", "openai":
			return "responses"
		}
	}
	return wireAPI
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
		}
		if strings.TrimSpace(provider.Model) == "" {
			issues = append(issues, ConfigValidationIssue{Key: "maclaw_llm_providers.model", Message: "Selected provider model is required."})
		}
		if strings.TrimSpace(resolveProviderSecret(provider)) == "" {
			issues = append(issues, ConfigValidationIssue{Key: "maclaw_llm_providers.key", Message: "Selected provider credential is required."})
		}
		return issues
	}
	if strings.TrimSpace(cfg.MaclawLLMUrl) == "" {
		issues = append(issues, ConfigValidationIssue{Key: "maclaw_llm_url", Message: "LLM URL is required."})
	}
	if strings.TrimSpace(cfg.MaclawLLMKey) == "" {
		issues = append(issues, ConfigValidationIssue{Key: "maclaw_llm_key", Message: "LLM API key is required."})
	}
	if strings.TrimSpace(cfg.MaclawLLMModel) == "" {
		issues = append(issues, ConfigValidationIssue{Key: "maclaw_llm_model", Message: "LLM model is required."})
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
	preserveRuntimeIntegrationConfig(current, &next)
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

func preserveRuntimeIntegrationConfig(current corelib.AppConfig, next *corelib.AppConfig) {
	if next == nil {
		return
	}
	clonedCurrent := cloneAppConfig(current)
	if strings.TrimSpace(next.RemoteHubURL) == "" {
		next.RemoteHubURL = clonedCurrent.RemoteHubURL
	}
	if len(next.SkillSourcesAllowed) == 0 && len(clonedCurrent.SkillSourcesAllowed) > 0 {
		next.SkillSourcesAllowed = clonedCurrent.SkillSourcesAllowed
	}
	if len(next.MCPServers) == 0 && len(clonedCurrent.MCPServers) > 0 {
		next.MCPServers = clonedCurrent.MCPServers
	}
	if len(next.LocalMCPServers) == 0 && len(clonedCurrent.LocalMCPServers) > 0 {
		next.LocalMCPServers = clonedCurrent.LocalMCPServers
	}
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
		currentByKey[indexedSecretLookupKey(provider.Name, i)] = provider
	}
	for i := range next {
		currentProvider := corelib.WebSearchProvider{}
		if found, ok := currentByKey[indexedSecretLookupKey(next[i].Name, i)]; ok {
			currentProvider = found
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
