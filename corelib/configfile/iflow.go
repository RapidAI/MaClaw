package configfile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// IFlowConfigDir returns ~/.iflow
func IFlowConfigDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".iflow")
}

// IFlowConfigPath returns ~/.iflow/settings.json
func IFlowConfigPath() string {
	return filepath.Join(IFlowConfigDir(), "settings.json")
}

// WriteIFlowConfig writes ~/.iflow/settings.json with provider config.
//
// iFlow CLI reads settings.json on startup for provider configuration:
//   - selectedAuthType: "openai-compatible"
//   - apiKey, baseUrl, modelName
//
// This preserves any existing fields not managed by us.
func WriteIFlowConfig(apiKey, baseURL, modelID string) error {
	if apiKey == "" {
		return nil
	}

	configPath := IFlowConfigPath()

	// Read existing config to preserve other fields
	existing := make(map[string]interface{})
	if data, err := os.ReadFile(configPath); err == nil {
		_ = json.Unmarshal(data, &existing)
	}

	// Set provider fields
	existing["selectedAuthType"] = "openai-compatible"
	existing["apiKey"] = apiKey
	if baseURL != "" {
		existing["baseUrl"] = baseURL
	} else {
		delete(existing, "baseUrl") // clear stale value from previous provider
	}
	if modelID != "" {
		existing["modelName"] = modelID
	} else {
		delete(existing, "modelName") // clear stale value from previous provider
	}

	return AtomicWriteJSON(configPath, existing)
}

// ReadIFlowConfig reads ~/.iflow/settings.json for backfill.
func ReadIFlowConfig() (map[string]interface{}, error) {
	data, err := os.ReadFile(IFlowConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read iflow config: %w", err)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse iflow config: %w", err)
	}
	return result, nil
}

// ClearIFlowThirdPartySettings removes third-party provider configuration
// from ~/.iflow/settings.json when switching back to the builtin provider.
func ClearIFlowThirdPartySettings() error {
	configPath := IFlowConfigPath()
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read iflow settings: %w", err)
	}

	var existing map[string]interface{}
	if err := json.Unmarshal(data, &existing); err != nil {
		// Can't parse — just remove the file
		return os.Remove(configPath)
	}

	// Remove provider-specific fields
	delete(existing, "selectedAuthType")
	delete(existing, "apiKey")
	delete(existing, "baseUrl")
	delete(existing, "modelName")

	if len(existing) == 0 {
		return os.Remove(configPath)
	}

	return AtomicWriteJSON(configPath, existing)
}
