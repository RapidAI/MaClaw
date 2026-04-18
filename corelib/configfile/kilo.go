package configfile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// KiloConfigDir returns ~/.kilocode/cli
func KiloConfigDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".kilocode", "cli")
}

// KiloConfigPath returns ~/.kilocode/cli/config.json
func KiloConfigPath() string {
	return filepath.Join(KiloConfigDir(), "config.json")
}

// WriteKiloConfig writes ~/.kilocode/cli/config.json with provider config.
//
// Kilo Code reads config.json on startup for provider configuration.
// The providers array contains OpenAI-compatible provider entries:
//
//	{
//	  "providers": [{
//	    "id": "default",
//	    "provider": "openai",
//	    "openAiApiKey": "...",
//	    "openAiModelId": "...",
//	    "openAiBaseUrl": "..."
//	  }]
//	}
//
// This preserves any existing fields not managed by us (e.g. MCP servers).
func WriteKiloConfig(apiKey, baseURL, modelID string) error {
	if apiKey == "" {
		return nil
	}

	configPath := KiloConfigPath()

	// Read existing config to preserve MCP servers, plugins, etc.
	existing := make(map[string]interface{})
	if data, err := os.ReadFile(configPath); err == nil {
		_ = json.Unmarshal(data, &existing)
	}

	// Build provider object
	provider := map[string]interface{}{
		"id":            "default",
		"provider":      "openai",
		"openAiApiKey":  apiKey,
		"openAiModelId": modelID,
		"openAiBaseUrl": baseURL,
	}

	// Update providers array
	existing["providers"] = []interface{}{provider}

	return AtomicWriteJSON(configPath, existing)
}

// ReadKiloConfig reads ~/.kilocode/cli/config.json for backfill.
func ReadKiloConfig() (map[string]interface{}, error) {
	data, err := os.ReadFile(KiloConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read kilo config: %w", err)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse kilo config: %w", err)
	}
	return result, nil
}

// ClearKiloThirdPartySettings removes third-party provider configuration
// from ~/.kilocode/cli/config.json when switching back to the builtin provider.
func ClearKiloThirdPartySettings() error {
	configPath := KiloConfigPath()
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read kilo config: %w", err)
	}

	var existing map[string]interface{}
	if err := json.Unmarshal(data, &existing); err != nil {
		return os.Remove(configPath)
	}

	// Remove providers array
	delete(existing, "providers")

	if len(existing) == 0 {
		return os.Remove(configPath)
	}

	return AtomicWriteJSON(configPath, existing)
}
