package configfile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// GeminiDirPath returns ~/.gemini
func GeminiDirPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".gemini")
}

// GeminiEnvPath returns ~/.gemini/.env
func GeminiEnvPath() string {
	return filepath.Join(GeminiDirPath(), ".env")
}

// GeminiSettingsPath returns ~/.gemini/settings.json
func GeminiSettingsPath() string {
	return filepath.Join(GeminiDirPath(), "settings.json")
}

// WriteGeminiConfig writes both ~/.gemini/.env and ~/.gemini/settings.json.
//
// Key improvements learned from cc-switch:
// 1. Writes .env file so Gemini CLI picks up API key on startup and subprocess restarts
// 2. Updates settings.json with security.auth.selectedType for API key vs OAuth mode
// 3. Sets file permissions (600) on Unix for security
// 4. Preserves existing settings.json fields (theme, editor prefs, etc.)
func WriteGeminiConfig(apiKey, baseURL, modelID string) error {
	if apiKey == "" {
		return nil // builtin provider, skip
	}

	dir := GeminiDirPath()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create gemini dir: %w", err)
	}
	// Set directory permissions on Unix
	if runtime.GOOS != "windows" {
		_ = os.Chmod(dir, 0700)
	}

	// Step 1: Write .env file
	if err := writeGeminiEnvFile(apiKey, baseURL, modelID); err != nil {
		return fmt.Errorf("write gemini .env: %w", err)
	}

	// Step 2: Update settings.json
	if err := writeGeminiSettings(apiKey); err != nil {
		return fmt.Errorf("write gemini settings: %w", err)
	}

	return nil
}

// writeGeminiEnvFile writes ~/.gemini/.env with incremental editing.
// Preserves existing env vars that we don't manage.
func writeGeminiEnvFile(apiKey, baseURL, modelID string) error {
	envPath := GeminiEnvPath()

	// Read existing .env to preserve user's custom vars
	existing := make(map[string]string)
	if data, err := os.ReadFile(envPath); err == nil {
		existing = parseEnvFile(string(data))
	}

	// Set our managed keys
	existing["GEMINI_API_KEY"] = apiKey
	existing["GOOGLE_API_KEY"] = apiKey
	if baseURL != "" {
		existing["GOOGLE_GEMINI_BASE_URL"] = baseURL
	} else {
		delete(existing, "GOOGLE_GEMINI_BASE_URL")
	}
	if modelID != "" {
		existing["GEMINI_MODEL"] = modelID
	} else {
		delete(existing, "GEMINI_MODEL")
	}

	// Build .env content
	content := buildEnvFileContent(existing)

	if err := AtomicWrite(envPath, []byte(content)); err != nil {
		return err
	}

	// Tighten file permissions on Unix (600 = owner read/write only).
	// Note: AtomicWrite creates with 0644; we chmod after rename.
	// The temp file window is brief but acceptable for local config.
	if runtime.GOOS != "windows" {
		_ = os.Chmod(envPath, 0600)
	}
	return nil
}

// writeGeminiSettings updates ~/.gemini/settings.json, preserving existing fields.
// Sets security.auth.selectedType to "api-key" when using API key auth.
func writeGeminiSettings(apiKey string) error {
	settingsPath := GeminiSettingsPath()

	// Read existing settings
	existing := make(map[string]interface{})
	if data, err := os.ReadFile(settingsPath); err == nil {
		_ = json.Unmarshal(data, &existing)
	}

	// Set security.auth.selectedType
	security, _ := existing["security"].(map[string]interface{})
	if security == nil {
		security = make(map[string]interface{})
	}
	auth, _ := security["auth"].(map[string]interface{})
	if auth == nil {
		auth = make(map[string]interface{})
	}

	if apiKey != "" {
		auth["selectedType"] = "api-key"
	}

	security["auth"] = auth
	existing["security"] = security

	return AtomicWriteJSON(settingsPath, existing)
}

// parseEnvFile parses a .env file into key-value pairs.
// Handles KEY=VALUE, KEY="VALUE", and comments (#).
func parseEnvFile(content string) map[string]string {
	result := make(map[string]string)
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		// Strip surrounding quotes
		if len(val) >= 2 && ((val[0] == '"' && val[len(val)-1] == '"') ||
			(val[0] == '\'' && val[len(val)-1] == '\'')) {
			val = val[1 : len(val)-1]
		}
		result[key] = val
	}
	return result
}

// buildEnvFileContent builds .env file content from key-value pairs.
// Managed keys are written first in a fixed order, then remaining user vars sorted.
func buildEnvFileContent(vars map[string]string) string {
	// Group our managed keys first, then others
	managed := []string{"GEMINI_API_KEY", "GOOGLE_API_KEY", "GOOGLE_GEMINI_BASE_URL", "GEMINI_MODEL"}
	var sb strings.Builder

	written := make(map[string]bool)
	for _, key := range managed {
		if val, ok := vars[key]; ok {
			fmt.Fprintf(&sb, "%s=%s\n", key, val)
			written[key] = true
		}
	}

	// Collect and sort remaining user-defined vars for deterministic output
	var others []string
	for key := range vars {
		if !written[key] {
			others = append(others, key)
		}
	}
	sort.Strings(others)
	for _, key := range others {
		fmt.Fprintf(&sb, "%s=%s\n", key, vars[key])
	}

	return sb.String()
}

// ReadGeminiEnv reads ~/.gemini/.env for backfill.
func ReadGeminiEnv() (map[string]string, error) {
	data, err := os.ReadFile(GeminiEnvPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return parseEnvFile(string(data)), nil
}

// ReadGeminiSettings reads ~/.gemini/settings.json for backfill.
func ReadGeminiSettings() (map[string]interface{}, error) {
	data, err := os.ReadFile(GeminiSettingsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// geminiManagedEnvKeys lists the env keys that are managed by maclaw for
// Gemini third-party provider configuration. ClearGeminiThirdPartySettings
// removes exactly these keys when switching back to the builtin provider.
var geminiManagedEnvKeys = []string{
	"GEMINI_API_KEY",
	"GOOGLE_API_KEY",
	"GOOGLE_GEMINI_BASE_URL",
	"GEMINI_MODEL",
}

// ClearGeminiThirdPartySettings surgically removes third-party provider
// configuration from ~/.gemini/.env and ~/.gemini/settings.json, preserving
// all other user state (custom env vars, UI preferences, etc.).
//
// For .env: removes only the managed keys (GEMINI_API_KEY, GOOGLE_API_KEY,
// GOOGLE_GEMINI_BASE_URL, GEMINI_MODEL), preserving any user-defined vars.
//
// For settings.json: resets security.auth.selectedType to "oauth-personal"
// (the default auth mode), preserving all other fields.
//
// If either file doesn't exist, that part is a no-op. Both operations are
// independent — a failure in one does not prevent the other from executing.
func ClearGeminiThirdPartySettings() error {
	var firstErr error
	if err := clearGeminiEnvThirdPartyKeys(); err != nil {
		firstErr = fmt.Errorf("clear gemini .env: %w", err)
	}
	if err := clearGeminiSettingsAuthType(); err != nil {
		if firstErr != nil {
			return firstErr // return the first error
		}
		return fmt.Errorf("clear gemini settings: %w", err)
	}
	return firstErr
}

// clearGeminiEnvThirdPartyKeys reads ~/.gemini/.env, removes managed keys,
// and writes back preserving user's custom vars. No-op if file doesn't exist.
func clearGeminiEnvThirdPartyKeys() error {
	envPath := GeminiEnvPath()

	data, err := os.ReadFile(envPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read .env: %w", err)
	}

	vars := parseEnvFile(string(data))

	// Remove only the managed keys; leave user-defined vars intact.
	for _, key := range geminiManagedEnvKeys {
		delete(vars, key)
	}

	// If no vars remain, write an empty file rather than deleting it.
	content := buildEnvFileContent(vars)

	// Skip write if content hasn't changed.
	if string(data) == content {
		return nil
	}

	if err := AtomicWrite(envPath, []byte(content)); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(envPath, 0600)
	}
	return nil
}

// clearGeminiSettingsAuthType reads ~/.gemini/settings.json, resets
// security.auth.selectedType to "oauth-personal", and writes back
// preserving all other fields. No-op if file doesn't exist.
func clearGeminiSettingsAuthType() error {
	settingsPath := GeminiSettingsPath()

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read settings: %w", err)
	}

	var existing map[string]interface{}
	if err := json.Unmarshal(data, &existing); err != nil {
		return fmt.Errorf("parse settings: %w", err)
	}

	// Navigate to security.auth.selectedType and reset it.
	security, _ := existing["security"].(map[string]interface{})
	if security == nil {
		return nil // no security section, nothing to reset
	}
	auth, _ := security["auth"].(map[string]interface{})
	if auth == nil {
		return nil // no auth section, nothing to reset
	}

	// Only write if the value actually needs changing.
	if auth["selectedType"] == "oauth-personal" {
		return nil
	}

	auth["selectedType"] = "oauth-personal"
	security["auth"] = auth
	existing["security"] = security

	return AtomicWriteJSON(settingsPath, existing)
}

// EnsureGeminiOnboarding ensures that Gemini CLI's user-level settings file
// (~/.gemini/settings.json) contains a theme setting and other flags so
// first-run interactive prompts (theme picker, auth selection, etc.) are skipped.
//
// The function is idempotent — it only adds missing keys and never removes
// existing user preferences.
//
// logFn is an optional callback for diagnostic messages; pass nil to suppress.
func EnsureGeminiOnboarding(logFn func(string)) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home directory: %w", err)
	}

	dir := filepath.Join(home, ".gemini")
	configPath := filepath.Join(dir, "settings.json")

	existing := map[string]any{}
	data, err := os.ReadFile(configPath)
	if err == nil {
		if err := json.Unmarshal(data, &existing); err != nil {
			backupPath := configPath + ".bak"
			_ = os.Rename(configPath, backupPath)
			if logFn != nil {
				logFn(fmt.Sprintf("[gemini-onboarding] backed up corrupt %s to %s", configPath, backupPath))
			}
			existing = map[string]any{}
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", configPath, err)
	}

	changed := false

	// Ensure ui.theme is set to skip the theme selection prompt.
	ui, _ := existing["ui"].(map[string]any)
	if ui == nil {
		ui = map[string]any{}
		existing["ui"] = ui
	}
	if ui["theme"] == nil || strings.TrimSpace(fmt.Sprint(ui["theme"])) == "" {
		ui["theme"] = "Default Dark"
		changed = true
	}

	if ui["autoThemeSwitching"] == nil {
		ui["autoThemeSwitching"] = false
		changed = true
	}
	if ui["hideTips"] == nil {
		ui["hideTips"] = true
		changed = true
	}
	if ui["showShortcutsHint"] == nil {
		ui["showShortcutsHint"] = false
		changed = true
	}
	if ui["dynamicWindowTitle"] == nil {
		ui["dynamicWindowTitle"] = false
		changed = true
	}
	if ui["showStatusInTitle"] == nil {
		ui["showStatusInTitle"] = false
		changed = true
	}
	if ui["hideWindowTitle"] == nil {
		ui["hideWindowTitle"] = true
		changed = true
	}
	if ui["showCompatibilityWarnings"] == nil {
		ui["showCompatibilityWarnings"] = false
		changed = true
	}
	if ui["showHomeDirectoryWarning"] == nil {
		ui["showHomeDirectoryWarning"] = false
		changed = true
	}

	// Pre-select auth type to prevent the interactive auth selection prompt.
	if existing["selectedAuthType"] == nil || strings.TrimSpace(fmt.Sprint(existing["selectedAuthType"])) == "" {
		if os.Getenv("GEMINI_API_KEY") != "" {
			existing["selectedAuthType"] = "gemini-api-key"
		} else {
			existing["selectedAuthType"] = "oauth-personal"
		}
		changed = true
	}

	if !changed {
		if logFn != nil {
			logFn("[gemini-onboarding] settings already complete, no changes needed")
		}
		return nil
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	out, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := AtomicWrite(configPath, out); err != nil {
		return fmt.Errorf("write %s: %w", configPath, err)
	}

	if logFn != nil {
		logFn(fmt.Sprintf("[gemini-onboarding] updated %s with onboarding flags", configPath))
	}
	return nil
}
