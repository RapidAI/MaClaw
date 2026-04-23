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
	return cfg
}

func maskSecret(v string) string {
	if strings.TrimSpace(v) == "" {
		return ""
	}
	return "******"
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
	return corelib.MaclawLLMConfig{
		URL:           url,
		Key:           key,
		Model:         model,
		Protocol:      strings.TrimSpace(cfg.MaclawLLMProtocol),
		ContextLength: cfg.MaclawLLMContextLength,
		TimeoutSec:    cfg.MaclawLLMTimeoutSec,
	}, nil
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
	if strings.Contains(strings.ToLower(strings.TrimSpace(provider.URL)), "chatgpt.com") && oauthToken != "" {
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

func mergeSecretPreserving(current, next corelib.AppConfig) corelib.AppConfig {
	if strings.TrimSpace(next.MaclawLLMKey) == "" || next.MaclawLLMKey == "******" {
		next.MaclawLLMKey = current.MaclawLLMKey
	}
	if len(next.MaclawLLMProviders) == 0 {
		return next
	}

	currentByName := make(map[string]corelib.MaclawLLMProvider, len(current.MaclawLLMProviders))
	for i, provider := range current.MaclawLLMProviders {
		key := provider.Name
		if strings.TrimSpace(key) == "" {
			key = fmt.Sprintf("#%d", i)
		}
		currentByName[key] = provider
	}

	providers := make([]corelib.MaclawLLMProvider, len(next.MaclawLLMProviders))
	copy(providers, next.MaclawLLMProviders)
	for i := range providers {
		lookupKey := providers[i].Name
		if strings.TrimSpace(lookupKey) == "" {
			lookupKey = fmt.Sprintf("#%d", i)
		}
		currentProvider, ok := currentByName[lookupKey]
		if !ok {
			continue
		}
		if strings.TrimSpace(providers[i].Key) == "" || providers[i].Key == "******" {
			providers[i].Key = currentProvider.Key
		}
		if strings.TrimSpace(providers[i].OAuthAccessToken) == "" || providers[i].OAuthAccessToken == "******" {
			providers[i].OAuthAccessToken = currentProvider.OAuthAccessToken
		}
		if strings.TrimSpace(providers[i].RefreshToken) == "" || providers[i].RefreshToken == "******" {
			providers[i].RefreshToken = currentProvider.RefreshToken
		}
	}
	next.MaclawLLMProviders = providers
	return next
}
