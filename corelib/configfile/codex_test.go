package configfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClearCodexThirdPartySettings_ClearsAuthJSON(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	// Create .codex directory and seed auth.json with an API key.
	codexDir := filepath.Join(tmpHome, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatalf("create codex dir: %v", err)
	}
	auth := map[string]string{"OPENAI_API_KEY": "sk-test-key-12345"}
	if err := AtomicWriteJSON(CodexAuthPath(), auth); err != nil {
		t.Fatalf("seed auth.json: %v", err)
	}

	// Verify auth.json exists before clearing.
	if _, err := os.Stat(CodexAuthPath()); os.IsNotExist(err) {
		t.Fatal("auth.json should exist before clearing")
	}

	if err := ClearCodexThirdPartySettings(); err != nil {
		t.Fatalf("ClearCodexThirdPartySettings: %v", err)
	}

	// auth.json should be removed.
	if _, err := os.Stat(CodexAuthPath()); !os.IsNotExist(err) {
		t.Error("auth.json should have been removed")
	}
}

func TestClearCodexThirdPartySettings_ResetsProviderPreservingMCPServersAndProfiles(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	codexDir := filepath.Join(tmpHome, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatalf("create codex dir: %v", err)
	}

	// Seed auth.json.
	auth := map[string]string{"OPENAI_API_KEY": "sk-test-key"}
	if err := AtomicWriteJSON(CodexAuthPath(), auth); err != nil {
		t.Fatalf("seed auth.json: %v", err)
	}

	// Seed config.toml with provider config, MCP servers, profiles, and features.
	configContent := `model_provider = "custom"
model = "gpt-5.4"
model_reasoning_effort = "xhigh"
disable_response_storage = true

[model_providers.custom]
name = "custom"
base_url = "https://example.com/api"
wire_api = "responses"
supports_websockets = true
requires_openai_auth = true

[mcp_servers.my-server]
command = "/usr/bin/mcp-server"
args = ["--port", "8080"]

[mcp_servers.another-server]
command = "npx"
args = ["-y", "@modelcontextprotocol/server-filesystem"]

[profile.default]
model = "gpt-5.4"

[features]
responses_websockets_v2 = true
`
	if err := AtomicWrite(CodexConfigPath(), []byte(configContent)); err != nil {
		t.Fatalf("seed config.toml: %v", err)
	}

	if err := ClearCodexThirdPartySettings(); err != nil {
		t.Fatalf("ClearCodexThirdPartySettings: %v", err)
	}

	// auth.json should be removed.
	if _, err := os.Stat(CodexAuthPath()); !os.IsNotExist(err) {
		t.Error("auth.json should have been removed")
	}

	// config.toml should still exist with MCP servers, profiles, and features preserved.
	data, err := os.ReadFile(CodexConfigPath())
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	content := string(data)

	// Provider-related fields should be removed.
	if strings.Contains(content, `model_provider = "custom"`) {
		t.Error("model_provider should have been removed")
	}
	if strings.Contains(content, "[model_providers.custom]") {
		t.Error("[model_providers.custom] section should have been removed")
	}
	if strings.Contains(content, `base_url = "https://example.com/api"`) {
		t.Error("base_url in provider section should have been removed")
	}
	if strings.Contains(content, `wire_api = "responses"`) {
		t.Error("wire_api in provider section should have been removed")
	}

	// MCP servers should be preserved.
	if !strings.Contains(content, "[mcp_servers.my-server]") {
		t.Error("[mcp_servers.my-server] should be preserved")
	}
	if !strings.Contains(content, `command = "/usr/bin/mcp-server"`) {
		t.Error("mcp-server command should be preserved")
	}
	if !strings.Contains(content, "[mcp_servers.another-server]") {
		t.Error("[mcp_servers.another-server] should be preserved")
	}

	// Profiles should be preserved.
	if !strings.Contains(content, "[profile.default]") {
		t.Error("[profile.default] should be preserved")
	}

	// Features should be preserved.
	if !strings.Contains(content, "[features]") {
		t.Error("[features] section should be preserved")
	}
	if !strings.Contains(content, "responses_websockets_v2 = true") {
		t.Error("responses_websockets_v2 feature should be preserved")
	}

	// Non-provider top-level fields should be preserved.
	if !strings.Contains(content, `model_reasoning_effort = "xhigh"`) {
		t.Error("model_reasoning_effort should be preserved")
	}
	if !strings.Contains(content, "disable_response_storage = true") {
		t.Error("disable_response_storage should be preserved")
	}
}

func TestClearCodexThirdPartySettings_NoOpWhenFilesDoNotExist(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	// Don't create any files — CodexAuthPath() and CodexConfigPath() point to non-existent files.
	err := ClearCodexThirdPartySettings()
	if err != nil {
		t.Fatalf("expected no error for missing files, got: %v", err)
	}

	// Verify neither file was created.
	if _, statErr := os.Stat(CodexAuthPath()); !os.IsNotExist(statErr) {
		t.Error("auth.json should not have been created")
	}
	if _, statErr := os.Stat(CodexConfigPath()); !os.IsNotExist(statErr) {
		t.Error("config.toml should not have been created")
	}
}

func TestClearCodexThirdPartySettings_HandlesConfigTomlWithOnlyUserContent(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	codexDir := filepath.Join(tmpHome, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatalf("create codex dir: %v", err)
	}

	// config.toml with only user content — no provider section, no model_provider field.
	configContent := `model_reasoning_effort = "xhigh"
disable_response_storage = true

[mcp_servers.filesystem]
command = "npx"
args = ["-y", "@modelcontextprotocol/server-filesystem"]

[features]
responses_websockets_v2 = true
`
	if err := AtomicWrite(CodexConfigPath(), []byte(configContent)); err != nil {
		t.Fatalf("seed config.toml: %v", err)
	}

	if err := ClearCodexThirdPartySettings(); err != nil {
		t.Fatalf("ClearCodexThirdPartySettings: %v", err)
	}

	// config.toml should be unchanged since there's nothing to remove.
	data, err := os.ReadFile(CodexConfigPath())
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	content := string(data)

	// All user content should be preserved.
	if !strings.Contains(content, `model_reasoning_effort = "xhigh"`) {
		t.Error("model_reasoning_effort should be preserved")
	}
	if !strings.Contains(content, "disable_response_storage = true") {
		t.Error("disable_response_storage should be preserved")
	}
	if !strings.Contains(content, "[mcp_servers.filesystem]") {
		t.Error("[mcp_servers.filesystem] should be preserved")
	}
	if !strings.Contains(content, "[features]") {
		t.Error("[features] section should be preserved")
	}
	if !strings.Contains(content, "responses_websockets_v2 = true") {
		t.Error("responses_websockets_v2 should be preserved")
	}
}
