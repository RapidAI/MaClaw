package configfile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// OpencodeConfigDir returns ~/.config/opencode
// Matches cc-switch's get_opencode_dir(): dirs::home_dir().join(".config/opencode")
func OpencodeConfigDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "opencode")
}

// OpencodeConfigPath returns ~/.config/opencode/opencode.json
func OpencodeConfigPath() string {
	return filepath.Join(OpencodeConfigDir(), "opencode.json")
}

// WriteOpencodeConfig writes ~/.config/opencode/opencode.json with provider config.
//
// OpenCode uses AI SDK provider format in opencode.json:
//   - provider.<id>.npm = "@ai-sdk/openai-compatible"
//   - provider.<id>.options.baseURL, provider.<id>.options.apiKey
//   - provider.<id>.models.<modelId>.name
//
// This preserves existing MCP servers, plugins, and other providers.
func WriteOpencodeConfig(apiKey, baseURL, modelID, providerName string) error {
	if apiKey == "" {
		return nil
	}

	configPath := OpencodeConfigPath()

	// Read existing config to preserve MCP, plugins, other providers
	existing := make(map[string]interface{})
	if data, err := os.ReadFile(configPath); err == nil {
		_ = json.Unmarshal(data, &existing)
	}

	// Ensure $schema is present
	if _, ok := existing["$schema"]; !ok {
		existing["$schema"] = "https://opencode.ai/config.json"
	}

	// Build provider entry
	providerID := sanitizeProviderID(providerName)

	// Get or create provider map
	providers, _ := existing["provider"].(map[string]interface{})
	if providers == nil {
		providers = make(map[string]interface{})
	}

	// Merge with existing provider entry to preserve user-added models
	provider := buildOpencodeProvider(apiKey, baseURL, modelID, providerName)
	if existingProvider, ok := providers[providerID].(map[string]interface{}); ok {
		// Preserve existing models, then overlay ours
		if existingModels, ok := existingProvider["models"].(map[string]interface{}); ok {
			if newModels, ok := provider["models"].(map[string]interface{}); ok {
				for k, v := range newModels {
					existingModels[k] = v
				}
				provider["models"] = existingModels
			}
		}
	}
	providers[providerID] = provider
	existing["provider"] = providers

	return AtomicWriteJSON(configPath, existing)
}

// buildOpencodeProvider creates the provider config object matching cc-switch's
// OpenCodeProviderConfig structure.
func buildOpencodeProvider(apiKey, baseURL, modelID, providerName string) map[string]interface{} {
	options := map[string]interface{}{
		"apiKey": apiKey,
	}
	if baseURL != "" {
		options["baseURL"] = baseURL
	}

	provider := map[string]interface{}{
		"npm":     "@ai-sdk/openai-compatible",
		"options": options,
	}

	if providerName != "" {
		provider["name"] = providerName
	}

	// Add model definition if modelID is provided
	if modelID != "" {
		displayName := modelID
		if providerName != "" {
			displayName = providerName + "/" + modelID
		}
		models := map[string]interface{}{
			modelID: map[string]interface{}{
				"name": displayName,
			},
		}
		provider["models"] = models
	}

	return provider
}

// sanitizeProviderID creates a safe provider ID from the provider name.
// Delegates to the shared sanitizeID helper.
func sanitizeProviderID(name string) string {
	return SanitizeID(name)
}

// SanitizeID normalizes a provider/tool name into a safe ASCII identifier.
// Used by codex (TOML key), opencode (JSON key), etc.
func SanitizeID(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	if s == "" {
		return "custom"
	}
	switch s {
	case "\u8baf\u98de\u661f\u8fb0":
		return "xfyun"
	case "\u963f\u91cc\u4e91":
		return "aliyun"
	case "\u767e\u5ea6\u5343\u5e06", "qianfan":
		return "qianfan"
	}
	var result []byte
	for _, c := range s {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '-' {
			result = append(result, byte(c))
		}
	}
	if len(result) == 0 {
		return "custom"
	}
	return string(result)
}

// ReadOpencodeConfig reads ~/.config/opencode/opencode.json for backfill.
func ReadOpencodeConfig() (map[string]interface{}, error) {
	data, err := os.ReadFile(OpencodeConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read opencode config: %w", err)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse opencode config: %w", err)
	}
	return result, nil
}
