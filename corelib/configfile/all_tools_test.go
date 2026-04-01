package configfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// Feature: codegen-scan-login, Property 3: Multi-tool write fault isolation
// **Validates: Requirements 2.6**
//
// For any subset of tool config writers that fail (simulated via error injection),
// WriteAllToolConfigsWithWriters should still successfully write to all non-failing
// tools, and the returned ToolConfigResult should list exactly the failed tools in
// Failed and the succeeded tools in Succeeded.
func TestProperty_MultiToolWriteFaultIsolation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a set of tool names (at least 1, up to 8).
		n := rapid.IntRange(1, 8).Draw(t, "numTools")
		toolNames := make([]string, n)
		seen := map[string]bool{}
		for i := 0; i < n; i++ {
			for {
				name := rapid.StringMatching(`[A-Za-z][A-Za-z0-9_]{0,15}`).Draw(t, "toolName")
				if !seen[name] {
					seen[name] = true
					toolNames[i] = name
					break
				}
			}
		}

		// Generate a random subset of indices that should fail.
		failSet := map[int]bool{}
		for i := 0; i < n; i++ {
			if rapid.Bool().Draw(t, "fails") {
				failSet[i] = true
			}
		}

		// Build writers: failing tools return an error, others succeed.
		writers := make([]ToolWriter, n)
		for i, name := range toolNames {
			shouldFail := failSet[i]
			writers[i] = ToolWriter{
				Name: name,
				Fn: func() error {
					if shouldFail {
						return errors.New("injected failure")
					}
					return nil
				},
			}
		}

		result := WriteAllToolConfigsWithWriters(writers)

		// Collect expected succeeded and failed names.
		var expectSucceeded, expectFailed []string
		for i, name := range toolNames {
			if failSet[i] {
				expectFailed = append(expectFailed, name)
			} else {
				expectSucceeded = append(expectSucceeded, name)
			}
		}

		// Property 1: total count of Succeeded + Failed equals total tools.
		if len(result.Succeeded)+len(result.Failed) != n {
			t.Fatalf("Succeeded(%d) + Failed(%d) != total(%d)",
				len(result.Succeeded), len(result.Failed), n)
		}

		// Property 2: non-failing tools appear in Succeeded.
		sort.Strings(result.Succeeded)
		sort.Strings(expectSucceeded)
		if !stringSlicesEqual(result.Succeeded, expectSucceeded) {
			t.Fatalf("Succeeded mismatch: got %v, want %v", result.Succeeded, expectSucceeded)
		}

		// Property 3: failing tools appear in Failed with correct tool names.
		gotFailed := make([]string, len(result.Failed))
		for i, f := range result.Failed {
			gotFailed[i] = f.Tool
			if f.Error == nil {
				t.Fatalf("Failed entry for %q has nil Error", f.Tool)
			}
		}
		sort.Strings(gotFailed)
		sort.Strings(expectFailed)
		if !stringSlicesEqual(gotFailed, expectFailed) {
			t.Fatalf("Failed mismatch: got %v, want %v", gotFailed, expectFailed)
		}
	})
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestWriteClaudeProviderSettings_CodegenWritesBoth(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	claudeExisting := map[string]interface{}{
		"customField": "keep-me",
		"env": map[string]interface{}{
			"EXISTING": "claude",
		},
	}
	if err := AtomicWriteJSON(ClaudeSettingsPath(), claudeExisting); err != nil {
		t.Fatalf("seed claude settings: %v", err)
	}

	codegenExisting := map[string]interface{}{
		"customCodeGenField": "keep-too",
		"env": map[string]interface{}{
			"EXISTING_CODEGEN": "codegen",
		},
	}
	if err := AtomicWriteJSON(CodeGenSettingsPath(), codegenExisting); err != nil {
		t.Fatalf("seed codegen settings: %v", err)
	}

	if err := WriteClaudeProviderSettings("codegen", "tok-123", "https://codegen.example/anthropic", "claude-codegen-1"); err != nil {
		t.Fatalf("WriteClaudeProviderSettings: %v", err)
	}

	claudeSettings, err := ReadClaudeSettings()
	if err != nil {
		t.Fatalf("ReadClaudeSettings: %v", err)
	}
	codegenSettings, err := ReadCodeGenSettings()
	if err != nil {
		t.Fatalf("ReadCodeGenSettings: %v", err)
	}

	assertAnthropicEnvValue(t, claudeSettings, "ANTHROPIC_AUTH_TOKEN", "tok-123")
	assertAnthropicEnvValue(t, claudeSettings, "ANTHROPIC_BASE_URL", "https://codegen.example/anthropic")
	assertAnthropicEnvValue(t, claudeSettings, "ANTHROPIC_MODEL", "claude-codegen-1")
	assertAnthropicEnvValue(t, codegenSettings, "ANTHROPIC_AUTH_TOKEN", "tok-123")
	assertAnthropicEnvValue(t, codegenSettings, "ANTHROPIC_BASE_URL", "https://codegen.example/anthropic")
	assertAnthropicEnvValue(t, codegenSettings, "ANTHROPIC_MODEL", "claude-codegen-1")

	if got, _ := claudeSettings["customField"].(string); got != "keep-me" {
		t.Fatalf("claude customField = %q, want keep-me", got)
	}
	if got, _ := codegenSettings["customCodeGenField"].(string); got != "keep-too" {
		t.Fatalf("codegen customCodeGenField = %q, want keep-too", got)
	}
}

func assertAnthropicEnvValue(t *testing.T, settings map[string]interface{}, key, want string) {
	t.Helper()
	env, _ := settings["env"].(map[string]interface{})
	if env == nil {
		t.Fatalf("settings env is nil")
	}
	if got, _ := env[key].(string); got != want {
		t.Fatalf("env[%s] = %q, want %q", key, got, want)
	}
}

// Feature: codegen-scan-login, Property 4: Config write preserves existing user settings
// **Validates: Requirements 2.7**
//
// For any existing config file containing arbitrary user-defined fields
// (MCP servers, plugins, profiles, custom env vars), after calling the
// corresponding write function with new token/baseURL/modelID, all
// pre-existing user-defined fields should remain unchanged.
func TestProperty_ConfigWritePreservesExistingUserSettings(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	rapid.Check(t, func(rt *rapid.T) {
		// Generate random new credentials to write.
		token := rapid.StringMatching(`[a-zA-Z0-9]{8,32}`).Draw(rt, "token")
		baseURL := rapid.StringMatching(`https://[a-z]{3,10}\.[a-z]{2,5}/api/v[0-9]`).Draw(rt, "baseURL")
		modelID := rapid.StringMatching(`[a-z]+-[0-9]+(\.[0-9]+)?`).Draw(rt, "modelID")

		// --- Sub-property A: Claude settings.json preserves user fields ---
		t.Run("claude", func(_ *testing.T) {
			testClaudePreservesUserSettings(rt, t, token, baseURL, modelID)
		})

		// --- Sub-property B: Gemini .env preserves user-defined env vars ---
		t.Run("gemini_env", func(_ *testing.T) {
			testGeminiEnvPreservesUserVars(rt, t, token, baseURL, modelID)
		})

		// --- Sub-property C: Gemini settings.json preserves user fields ---
		t.Run("gemini_settings", func(_ *testing.T) {
			testGeminiSettingsPreservesUserFields(rt, t, token, baseURL, modelID)
		})

		// --- Sub-property D: OpenCode opencode.json preserves user fields ---
		t.Run("opencode", func(_ *testing.T) {
			testOpencodePreservesUserSettings(rt, t, token, baseURL, modelID)
		})
	})
}

func testClaudePreservesUserSettings(rt *rapid.T, t *testing.T, token, baseURL, modelID string) {
	settingsPath := ClaudeSettingsPath()

	// Generate random user-defined fields that Claude writers should not touch.
	numMCPServers := rapid.IntRange(0, 3).Draw(rt, "claude_numMCP")
	mcpServers := make(map[string]interface{})
	for i := 0; i < numMCPServers; i++ {
		name := rapid.StringMatching(`[a-z]{3,8}`).Draw(rt, "claude_mcpName")
		mcpServers[name] = map[string]interface{}{
			"command": rapid.StringMatching(`[a-z/]{5,20}`).Draw(rt, "claude_mcpCmd"),
			"args":    []interface{}{rapid.StringMatching(`[a-z]{2,6}`).Draw(rt, "claude_mcpArg")},
		}
	}

	numPerms := rapid.IntRange(0, 3).Draw(rt, "claude_numPerms")
	permissions := make([]interface{}, numPerms)
	for i := 0; i < numPerms; i++ {
		permissions[i] = rapid.StringMatching(`[a-z_]{4,12}`).Draw(rt, "claude_perm")
	}

	customValue := rapid.StringMatching(`[a-zA-Z0-9 ]{1,20}`).Draw(rt, "claude_customVal")

	// Also seed some existing env vars that should be preserved.
	existingEnvKey := rapid.StringMatching(`MY_CUSTOM_[A-Z]{3,8}`).Draw(rt, "claude_envKey")
	existingEnvVal := rapid.StringMatching(`[a-zA-Z0-9]{4,16}`).Draw(rt, "claude_envVal")

	// Build pre-existing settings.
	existing := map[string]interface{}{
		"mcpServers":  mcpServers,
		"permissions": permissions,
		"customField": customValue,
		"env": map[string]interface{}{
			existingEnvKey: existingEnvVal,
		},
	}

	// Write pre-existing settings to disk.
	if err := AtomicWriteJSON(settingsPath, existing); err != nil {
		t.Fatalf("seed claude settings: %v", err)
	}

	// Call the writer with new credentials.
	if err := WriteClaudeSettings(token, baseURL, modelID); err != nil {
		t.Fatalf("WriteClaudeSettings: %v", err)
	}

	// Read back and verify user fields are preserved.
	result, err := ReadClaudeSettings()
	if err != nil {
		t.Fatalf("ReadClaudeSettings: %v", err)
	}

	// Verify mcpServers preserved.
	gotMCP, _ := result["mcpServers"].(map[string]interface{})
	if len(gotMCP) != len(mcpServers) {
		t.Fatalf("claude mcpServers count: got %d, want %d", len(gotMCP), len(mcpServers))
	}

	// Verify permissions preserved.
	gotPerms, _ := result["permissions"].([]interface{})
	if len(gotPerms) != len(permissions) {
		t.Fatalf("claude permissions count: got %d, want %d", len(gotPerms), len(permissions))
	}

	// Verify custom field preserved.
	if got, _ := result["customField"].(string); got != customValue {
		t.Fatalf("claude customField: got %q, want %q", got, customValue)
	}

	// Verify existing env var preserved alongside new ones.
	env, _ := result["env"].(map[string]interface{})
	if env == nil {
		t.Fatalf("claude env is nil after write")
	}
	if got, _ := env[existingEnvKey].(string); got != existingEnvVal {
		t.Fatalf("claude env[%s]: got %q, want %q", existingEnvKey, got, existingEnvVal)
	}

	// Verify new credentials were written.
	if got, _ := env["ANTHROPIC_AUTH_TOKEN"].(string); got != token {
		t.Fatalf("claude ANTHROPIC_AUTH_TOKEN: got %q, want %q", got, token)
	}
}

func testGeminiEnvPreservesUserVars(rt *rapid.T, t *testing.T, token, baseURL, modelID string) {
	envPath := GeminiEnvPath()

	// Generate random user-defined env vars.
	numUserVars := rapid.IntRange(1, 5).Draw(rt, "gemini_numUserVars")
	userVars := make(map[string]string)
	for i := 0; i < numUserVars; i++ {
		key := rapid.StringMatching(`CUSTOM_[A-Z]{3,8}`).Draw(rt, "gemini_envKey")
		val := rapid.StringMatching(`[a-zA-Z0-9]{4,16}`).Draw(rt, "gemini_envVal")
		userVars[key] = val
	}

	// Build pre-existing .env content.
	var sb strings.Builder
	for k, v := range userVars {
		fmt.Fprintf(&sb, "%s=%s\n", k, v)
	}

	// Write pre-existing .env to disk.
	dir := filepath.Dir(envPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("create gemini dir: %v", err)
	}
	if err := AtomicWrite(envPath, []byte(sb.String())); err != nil {
		t.Fatalf("seed gemini .env: %v", err)
	}

	// Call the writer.
	if err := WriteGeminiConfig(token, baseURL, modelID); err != nil {
		t.Fatalf("WriteGeminiConfig: %v", err)
	}

	// Read back and verify user vars are preserved.
	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("read gemini .env: %v", err)
	}
	gotVars := parseEnvFile(string(data))

	for k, wantVal := range userVars {
		if gotVal, ok := gotVars[k]; !ok {
			t.Fatalf("gemini .env missing user var %s", k)
		} else if gotVal != wantVal {
			t.Fatalf("gemini .env var %s: got %q, want %q", k, gotVal, wantVal)
		}
	}

	// Verify new credentials were written.
	if gotVars["GEMINI_API_KEY"] != token {
		t.Fatalf("gemini GEMINI_API_KEY: got %q, want %q", gotVars["GEMINI_API_KEY"], token)
	}
}

func testGeminiSettingsPreservesUserFields(rt *rapid.T, t *testing.T, token, baseURL, modelID string) {
	settingsPath := GeminiSettingsPath()

	// Generate random user-defined fields.
	theme := rapid.StringMatching(`[A-Za-z ]{3,12}`).Draw(rt, "gemini_theme")
	editorPref := rapid.StringMatching(`[a-z]{3,8}`).Draw(rt, "gemini_editor")

	existing := map[string]interface{}{
		"ui": map[string]interface{}{
			"theme":  theme,
			"editor": editorPref,
		},
		"customPlugin": rapid.StringMatching(`[a-z]{4,10}`).Draw(rt, "gemini_plugin"),
	}

	if err := AtomicWriteJSON(settingsPath, existing); err != nil {
		t.Fatalf("seed gemini settings: %v", err)
	}

	// Call the writer.
	if err := WriteGeminiConfig(token, baseURL, modelID); err != nil {
		t.Fatalf("WriteGeminiConfig: %v", err)
	}

	// Read back and verify.
	result, err := ReadGeminiSettings()
	if err != nil {
		t.Fatalf("ReadGeminiSettings: %v", err)
	}

	// Verify ui.theme preserved.
	ui, _ := result["ui"].(map[string]interface{})
	if ui == nil {
		t.Fatalf("gemini settings ui is nil")
	}
	if got, _ := ui["theme"].(string); got != theme {
		t.Fatalf("gemini ui.theme: got %q, want %q", got, theme)
	}
	if got, _ := ui["editor"].(string); got != editorPref {
		t.Fatalf("gemini ui.editor: got %q, want %q", got, editorPref)
	}

	// Verify custom plugin preserved.
	if _, ok := result["customPlugin"]; !ok {
		t.Fatalf("gemini settings missing customPlugin")
	}
}

func testOpencodePreservesUserSettings(rt *rapid.T, t *testing.T, token, baseURL, modelID string) {
	configPath := OpencodeConfigPath()

	// Generate random user-defined fields.
	mcpName := rapid.StringMatching(`[a-z]{3,8}`).Draw(rt, "oc_mcpName")
	mcpCmd := rapid.StringMatching(`[a-z/]{5,15}`).Draw(rt, "oc_mcpCmd")
	pluginName := rapid.StringMatching(`[a-z]{3,8}`).Draw(rt, "oc_pluginName")

	existing := map[string]interface{}{
		"$schema": "https://opencode.ai/config.json",
		"mcp": map[string]interface{}{
			mcpName: map[string]interface{}{
				"command": mcpCmd,
			},
		},
		"plugins": []interface{}{pluginName},
	}

	if err := AtomicWriteJSON(configPath, existing); err != nil {
		t.Fatalf("seed opencode config: %v", err)
	}

	providerName := rapid.StringMatching(`[a-z]{3,8}`).Draw(rt, "oc_providerName")

	// Call the writer.
	if err := WriteOpencodeConfig(token, baseURL, modelID, providerName); err != nil {
		t.Fatalf("WriteOpencodeConfig: %v", err)
	}

	// Read back and verify.
	result, err := ReadOpencodeConfig()
	if err != nil {
		t.Fatalf("ReadOpencodeConfig: %v", err)
	}

	// Verify mcp preserved.
	gotMCP, _ := result["mcp"].(map[string]interface{})
	if gotMCP == nil {
		t.Fatalf("opencode mcp is nil after write")
	}
	if _, ok := gotMCP[mcpName]; !ok {
		t.Fatalf("opencode mcp missing %s", mcpName)
	}

	// Verify plugins preserved.
	gotPlugins, _ := result["plugins"].([]interface{})
	if len(gotPlugins) != 1 {
		t.Fatalf("opencode plugins count: got %d, want 1", len(gotPlugins))
	}
	if got, _ := gotPlugins[0].(string); got != pluginName {
		t.Fatalf("opencode plugin: got %q, want %q", got, pluginName)
	}
}
