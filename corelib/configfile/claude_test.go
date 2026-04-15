package configfile

import (
	"encoding/json"
	"os"
	"testing"
)

func TestClearClaudeThirdPartySettings_RemovesOnlyThirdPartyEnvKeys(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	// Seed settings.json with third-party env keys and a user-defined env key.
	existing := map[string]interface{}{
		"env": map[string]interface{}{
			"ANTHROPIC_AUTH_TOKEN":                     "tok-abc",
			"ANTHROPIC_BASE_URL":                       "https://example.com/api",
			"ANTHROPIC_MODEL":                          "my-model",
			"ANTHROPIC_DEFAULT_HAIKU_MODEL":            "my-haiku",
			"ANTHROPIC_DEFAULT_SONNET_MODEL":           "my-sonnet",
			"ANTHROPIC_DEFAULT_OPUS_MODEL":             "my-opus",
			"ANTHROPIC_SMALL_FAST_MODEL":               "my-small",
			"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
			"API_TIMEOUT_MS":                           "600000",
			"MY_CUSTOM_VAR":                            "keep-me",
		},
	}
	if err := AtomicWriteJSON(ClaudeSettingsPath(), existing); err != nil {
		t.Fatalf("seed settings: %v", err)
	}

	if err := ClearClaudeThirdPartySettings(); err != nil {
		t.Fatalf("ClearClaudeThirdPartySettings: %v", err)
	}

	result, err := ReadClaudeSettings()
	if err != nil {
		t.Fatalf("ReadClaudeSettings: %v", err)
	}

	env, _ := result["env"].(map[string]interface{})
	if env == nil {
		t.Fatal("env map should exist because MY_CUSTOM_VAR should be preserved")
	}

	// All third-party keys should be removed.
	for _, key := range thirdPartyEnvKeys {
		if _, ok := env[key]; ok {
			t.Errorf("third-party key %q should have been removed", key)
		}
	}

	// User-defined key should be preserved.
	if got, _ := env["MY_CUSTOM_VAR"].(string); got != "keep-me" {
		t.Errorf("MY_CUSTOM_VAR = %q, want %q", got, "keep-me")
	}
}

func TestClearClaudeThirdPartySettings_PreservesNonEnvFields(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	// Seed settings.json with non-env fields alongside third-party env keys.
	existing := map[string]interface{}{
		"permissions": []interface{}{"read", "write", "execute"},
		"theme":       "dark",
		"mcpServers": map[string]interface{}{
			"my-server": map[string]interface{}{
				"command": "/usr/bin/mcp-server",
				"args":    []interface{}{"--port", "8080"},
			},
		},
		"hooks": map[string]interface{}{
			"PreToolUse": []interface{}{"hook1"},
		},
		"env": map[string]interface{}{
			"ANTHROPIC_AUTH_TOKEN": "tok-xyz",
			"ANTHROPIC_BASE_URL":  "https://example.com",
		},
	}
	if err := AtomicWriteJSON(ClaudeSettingsPath(), existing); err != nil {
		t.Fatalf("seed settings: %v", err)
	}

	if err := ClearClaudeThirdPartySettings(); err != nil {
		t.Fatalf("ClearClaudeThirdPartySettings: %v", err)
	}

	result, err := ReadClaudeSettings()
	if err != nil {
		t.Fatalf("ReadClaudeSettings: %v", err)
	}

	// Verify non-env fields are preserved.
	perms, _ := result["permissions"].([]interface{})
	if len(perms) != 3 {
		t.Errorf("permissions count = %d, want 3", len(perms))
	}

	if got, _ := result["theme"].(string); got != "dark" {
		t.Errorf("theme = %q, want %q", got, "dark")
	}

	mcpServers, _ := result["mcpServers"].(map[string]interface{})
	if mcpServers == nil {
		t.Fatal("mcpServers should be preserved")
	}
	if _, ok := mcpServers["my-server"]; !ok {
		t.Error("mcpServers[my-server] should be preserved")
	}

	hooks, _ := result["hooks"].(map[string]interface{})
	if hooks == nil {
		t.Fatal("hooks should be preserved")
	}
	if _, ok := hooks["PreToolUse"]; !ok {
		t.Error("hooks[PreToolUse] should be preserved")
	}
}

func TestClearClaudeThirdPartySettings_PreservesUserDefinedEnvFields(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	// Seed settings.json with only user-defined env vars (no third-party keys).
	existing := map[string]interface{}{
		"env": map[string]interface{}{
			"MY_API_KEY":     "secret-123",
			"CUSTOM_SETTING": "enabled",
			"DEBUG_MODE":     "true",
		},
	}
	if err := AtomicWriteJSON(ClaudeSettingsPath(), existing); err != nil {
		t.Fatalf("seed settings: %v", err)
	}

	if err := ClearClaudeThirdPartySettings(); err != nil {
		t.Fatalf("ClearClaudeThirdPartySettings: %v", err)
	}

	result, err := ReadClaudeSettings()
	if err != nil {
		t.Fatalf("ReadClaudeSettings: %v", err)
	}

	env, _ := result["env"].(map[string]interface{})
	if env == nil {
		t.Fatal("env map should be preserved when it contains user-defined keys")
	}

	if got, _ := env["MY_API_KEY"].(string); got != "secret-123" {
		t.Errorf("MY_API_KEY = %q, want %q", got, "secret-123")
	}
	if got, _ := env["CUSTOM_SETTING"].(string); got != "enabled" {
		t.Errorf("CUSTOM_SETTING = %q, want %q", got, "enabled")
	}
	if got, _ := env["DEBUG_MODE"].(string); got != "true" {
		t.Errorf("DEBUG_MODE = %q, want %q", got, "true")
	}
}

func TestClearClaudeThirdPartySettings_NoOpWhenSettingsFileDoesNotExist(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	// Don't create any settings file — ClaudeSettingsPath() points to a non-existent file.
	err := ClearClaudeThirdPartySettings()
	if err != nil {
		t.Fatalf("expected no error for missing settings file, got: %v", err)
	}

	// Verify the file was NOT created.
	if _, statErr := os.Stat(ClaudeSettingsPath()); !os.IsNotExist(statErr) {
		t.Error("settings file should not have been created")
	}
}

func TestClearClaudeThirdPartySettings_NoOpWhenEnvMapIsEmpty(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	existing := map[string]interface{}{
		"theme": "light",
		"env":   map[string]interface{}{},
	}
	if err := AtomicWriteJSON(ClaudeSettingsPath(), existing); err != nil {
		t.Fatalf("seed settings: %v", err)
	}

	if err := ClearClaudeThirdPartySettings(); err != nil {
		t.Fatalf("ClearClaudeThirdPartySettings: %v", err)
	}

	result, err := ReadClaudeSettings()
	if err != nil {
		t.Fatalf("ReadClaudeSettings: %v", err)
	}

	if got, _ := result["theme"].(string); got != "light" {
		t.Errorf("theme = %q, want %q", got, "light")
	}
}

func TestClearClaudeThirdPartySettings_NoOpWhenEnvMapIsMissing(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	// Settings file exists but has no "env" field at all.
	existing := map[string]interface{}{
		"permissions": []interface{}{"read"},
		"theme":       "solarized",
	}
	if err := AtomicWriteJSON(ClaudeSettingsPath(), existing); err != nil {
		t.Fatalf("seed settings: %v", err)
	}

	if err := ClearClaudeThirdPartySettings(); err != nil {
		t.Fatalf("ClearClaudeThirdPartySettings: %v", err)
	}

	result, err := ReadClaudeSettings()
	if err != nil {
		t.Fatalf("ReadClaudeSettings: %v", err)
	}

	if got, _ := result["theme"].(string); got != "solarized" {
		t.Errorf("theme = %q, want %q", got, "solarized")
	}

	// env field should still not exist.
	if _, ok := result["env"]; ok {
		t.Error("env field should not have been created")
	}
}

func TestClearClaudeThirdPartySettings_RemovesEnvFieldWhenAllKeysAreThirdParty(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	// Seed settings.json with only third-party env keys.
	existing := map[string]interface{}{
		"theme": "monokai",
		"env": map[string]interface{}{
			"ANTHROPIC_AUTH_TOKEN": "tok-123",
			"ANTHROPIC_BASE_URL":  "https://example.com",
			"ANTHROPIC_MODEL":     "model-1",
		},
	}
	if err := AtomicWriteJSON(ClaudeSettingsPath(), existing); err != nil {
		t.Fatalf("seed settings: %v", err)
	}

	if err := ClearClaudeThirdPartySettings(); err != nil {
		t.Fatalf("ClearClaudeThirdPartySettings: %v", err)
	}

	// Read raw JSON to verify env field is completely removed.
	data, err := os.ReadFile(ClaudeSettingsPath())
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if _, ok := raw["env"]; ok {
		t.Error("env field should be removed entirely when all keys are third-party")
	}

	if got, _ := raw["theme"].(string); got != "monokai" {
		t.Errorf("theme = %q, want %q", got, "monokai")
	}
}
