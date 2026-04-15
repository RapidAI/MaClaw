package configfile

import (
	"encoding/json"
	"os"
	"testing"
)

func TestClearGeminiThirdPartySettings_RemovesManagedKeysFromEnvPreservingCustomVars(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	// Seed .env with managed keys and a user-defined custom var.
	envContent := "GEMINI_API_KEY=test-key\nGOOGLE_API_KEY=test-key\nGOOGLE_GEMINI_BASE_URL=https://example.com\nGEMINI_MODEL=my-model\nMY_CUSTOM_VAR=keep-me\n"
	dir := GeminiDirPath()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("create dir: %v", err)
	}
	if err := os.WriteFile(GeminiEnvPath(), []byte(envContent), 0644); err != nil {
		t.Fatalf("seed .env: %v", err)
	}

	// Seed settings.json with api-key auth type.
	settings := map[string]interface{}{
		"security": map[string]interface{}{
			"auth": map[string]interface{}{
				"selectedType": "api-key",
			},
		},
	}
	if err := AtomicWriteJSON(GeminiSettingsPath(), settings); err != nil {
		t.Fatalf("seed settings: %v", err)
	}

	if err := ClearGeminiThirdPartySettings(); err != nil {
		t.Fatalf("ClearGeminiThirdPartySettings: %v", err)
	}

	// Verify .env: managed keys removed, custom var preserved.
	envData, err := os.ReadFile(GeminiEnvPath())
	if err != nil {
		t.Fatalf("read .env: %v", err)
	}
	vars := parseEnvFile(string(envData))

	for _, key := range geminiManagedEnvKeys {
		if _, ok := vars[key]; ok {
			t.Errorf("managed key %q should have been removed from .env", key)
		}
	}
	if got, ok := vars["MY_CUSTOM_VAR"]; !ok || got != "keep-me" {
		t.Errorf("MY_CUSTOM_VAR = %q, want %q", got, "keep-me")
	}
}

func TestClearGeminiThirdPartySettings_ResetsAuthTypePreservingOtherFields(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	dir := GeminiDirPath()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("create dir: %v", err)
	}

	// Seed settings.json with api-key auth type and other fields.
	settings := map[string]interface{}{
		"ui": map[string]interface{}{
			"theme":             "Default Dark",
			"autoThemeSwitching": false,
		},
		"security": map[string]interface{}{
			"auth": map[string]interface{}{
				"selectedType": "api-key",
				"otherField":   "preserved",
			},
			"sandbox": true,
		},
		"selectedAuthType": "gemini-api-key",
	}
	if err := AtomicWriteJSON(GeminiSettingsPath(), settings); err != nil {
		t.Fatalf("seed settings: %v", err)
	}

	if err := ClearGeminiThirdPartySettings(); err != nil {
		t.Fatalf("ClearGeminiThirdPartySettings: %v", err)
	}

	data, err := os.ReadFile(GeminiSettingsPath())
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Auth type should be reset to oauth-personal.
	security, _ := result["security"].(map[string]interface{})
	if security == nil {
		t.Fatal("security section should be preserved")
	}
	auth, _ := security["auth"].(map[string]interface{})
	if auth == nil {
		t.Fatal("auth section should be preserved")
	}
	if got, _ := auth["selectedType"].(string); got != "oauth-personal" {
		t.Errorf("selectedType = %q, want %q", got, "oauth-personal")
	}
	// Other auth fields preserved.
	if got, _ := auth["otherField"].(string); got != "preserved" {
		t.Errorf("otherField = %q, want %q", got, "preserved")
	}
	// Other security fields preserved.
	if got, _ := security["sandbox"].(bool); !got {
		t.Error("sandbox should be preserved as true")
	}

	// UI fields preserved.
	ui, _ := result["ui"].(map[string]interface{})
	if ui == nil {
		t.Fatal("ui section should be preserved")
	}
	if got, _ := ui["theme"].(string); got != "Default Dark" {
		t.Errorf("theme = %q, want %q", got, "Default Dark")
	}

	// Top-level fields preserved.
	if got, _ := result["selectedAuthType"].(string); got != "gemini-api-key" {
		t.Errorf("selectedAuthType = %q, want %q", got, "gemini-api-key")
	}
}

func TestClearGeminiThirdPartySettings_NoOpWhenFilesDoNotExist(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	// Don't create any files — GeminiEnvPath() and GeminiSettingsPath() point to non-existent files.
	err := ClearGeminiThirdPartySettings()
	if err != nil {
		t.Fatalf("expected no error for missing files, got: %v", err)
	}

	// Verify neither file was created.
	if _, statErr := os.Stat(GeminiEnvPath()); !os.IsNotExist(statErr) {
		t.Error(".env file should not have been created")
	}
	if _, statErr := os.Stat(GeminiSettingsPath()); !os.IsNotExist(statErr) {
		t.Error("settings.json file should not have been created")
	}
}

func TestClearGeminiThirdPartySettings_HandlesEmptyEnvFileGracefully(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	dir := GeminiDirPath()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("create dir: %v", err)
	}

	// Write an empty .env file.
	if err := os.WriteFile(GeminiEnvPath(), []byte(""), 0644); err != nil {
		t.Fatalf("seed empty .env: %v", err)
	}

	err := ClearGeminiThirdPartySettings()
	if err != nil {
		t.Fatalf("expected no error for empty .env, got: %v", err)
	}

	// .env should still exist (empty content is fine).
	if _, statErr := os.Stat(GeminiEnvPath()); os.IsNotExist(statErr) {
		t.Error(".env file should still exist after clearing empty file")
	}
}
