package configfile

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestWriteCodexConfigAvoidsOpenAIReservedProviderName(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")

	if err := WriteCodexConfig("sk-test", "http://127.0.0.1:20128/v1", "codex/gpt-5.4", "openai", "responses"); err != nil {
		t.Fatalf("WriteCodexConfig: %v", err)
	}
	data, err := os.ReadFile(CodexConfigPath())
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	content := string(data)
	if strings.Contains(content, `model_provider = "openai"`) || strings.Contains(content, "[model_providers.openai]") {
		t.Fatalf("reserved openai provider key leaked into config.toml:\n%s", content)
	}
	if !strings.Contains(content, `model_provider = "openai-compatible"`) || !strings.Contains(content, "[model_providers.openai-compatible]") {
		t.Fatalf("openai provider was not normalized safely:\n%s", content)
	}
}

func TestBuildCodexConfigTomlContentKeepsGenericFallbackModel(t *testing.T) {
	content := BuildCodexConfigTomlContent("https://api.example.com/v1", "", "custom", "responses")
	if !strings.Contains(content, `model = "gpt-5.4"`) {
		t.Fatalf("generic provider fallback model changed unexpectedly:\n%s", content)
	}
	if strings.Contains(content, "gpt-5.6-luna") {
		t.Fatalf("generic provider must not inherit the OpenAI OAuth model:\n%s", content)
	}
}

func TestWriteCodexConfigAddsCodeGenClientNameHeader(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")

	if err := WriteCodexConfigWithClientName("sk-test", "https://codegen.qianxin-inc.cn/api/v1", "codegen-model", "CodeGen", "responses", "custom-agent"); err != nil {
		t.Fatalf("WriteCodexConfigWithClientName: %v", err)
	}
	data, err := os.ReadFile(CodexConfigPath())
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, `http_headers = { "X-Codegen-Client-Name" = "custom-agent" }`) {
		t.Fatalf("missing CodeGen client name header:\n%s", content)
	}
}

func TestWriteCodexConfigDefaultsOpenClawCodeGenClientNameToTigerclaw(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")

	if err := WriteCodexConfigWithClientName("sk-test", "https://codegen.qianxin-inc.cn/api/v1", "codegen-model", "CodeGen", "responses", "openclaw"); err != nil {
		t.Fatalf("WriteCodexConfigWithClientName: %v", err)
	}
	data, err := os.ReadFile(CodexConfigPath())
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	content := string(data)
	if strings.Contains(content, `"X-Codegen-Client-Name" = "openclaw"`) {
		t.Fatalf("legacy openclaw CodeGen client name leaked into config.toml:\n%s", content)
	}
	if !strings.Contains(content, `"X-Codegen-Client-Name" = "tigerclaw"`) {
		t.Fatalf("CodeGen client name did not default to tigerclaw:\n%s", content)
	}
}

func TestWriteCodexConfigMergesCodeGenClientNameHeader(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")

	codexDir := filepath.Join(tmpHome, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatalf("create codex dir: %v", err)
	}
	configContent := `model_provider = "custom"
model = "old-model"

[model_providers.custom]
name = "custom"
base_url = "https://old.example/v1"
wire_api = "responses"
http_headers = { "X-Custom" = "keep" } # preserve me
supports_websockets = false
`
	if err := AtomicWrite(CodexConfigPath(), []byte(configContent)); err != nil {
		t.Fatalf("seed config.toml: %v", err)
	}

	if err := WriteCodexConfigWithClientName("sk-test", "https://codegen.qianxin-inc.cn/api/v1", "codegen-model", "custom", "responses", "custom-agent"); err != nil {
		t.Fatalf("WriteCodexConfigWithClientName: %v", err)
	}
	data, err := os.ReadFile(CodexConfigPath())
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	content := string(data)
	for _, want := range []string{`"X-Custom" = "keep"`, `"X-Codegen-Client-Name" = "custom-agent"`, `# preserve me`} {
		if !strings.Contains(content, want) {
			t.Fatalf("config.toml missing %q:\n%s", want, content)
		}
	}
}

func TestWriteCodexConfigReplacesCaseVariantCodeGenHeader(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")

	codexDir := filepath.Join(tmpHome, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatalf("create codex dir: %v", err)
	}
	configContent := `model_provider = "custom"
model = "old-model"

[model_providers.custom]
name = "custom"
base_url = "https://old.example/v1"
wire_api = "responses"
http_headers = { "x-codegen-client-name" = "old-agent", "X-Custom" = "keep" }
supports_websockets = false
`
	if err := AtomicWrite(CodexConfigPath(), []byte(configContent)); err != nil {
		t.Fatalf("seed config.toml: %v", err)
	}

	if err := WriteCodexConfigWithClientName("sk-test", "https://codegen.qianxin-inc.cn/api/v1", "codegen-model", "custom", "responses", "custom-agent"); err != nil {
		t.Fatalf("WriteCodexConfigWithClientName: %v", err)
	}
	data, err := os.ReadFile(CodexConfigPath())
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	content := string(data)
	for _, want := range []string{`"X-Codegen-Client-Name" = "custom-agent"`, `"X-Custom" = "keep"`} {
		if !strings.Contains(content, want) {
			t.Fatalf("config.toml missing %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, "x-codegen-client-name") || strings.Contains(content, "old-agent") {
		t.Fatalf("case-variant CodeGen header was not replaced:\n%s", content)
	}
}

func TestWriteCodexConfigRemovesStaleCodeGenHeaderForNonCodeGenURL(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")

	if err := WriteCodexConfigWithClientName("sk-test", "https://codegen.qianxin-inc.cn/api/v1", "codegen-model", "custom", "responses", "custom-agent"); err != nil {
		t.Fatalf("seed WriteCodexConfigWithClientName: %v", err)
	}
	if err := WriteCodexConfig("sk-test", "https://api.example.com/v1", "gpt-test", "custom", "responses"); err != nil {
		t.Fatalf("WriteCodexConfig: %v", err)
	}
	data, err := os.ReadFile(CodexConfigPath())
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	content := string(data)
	if strings.Contains(content, "http_headers") || strings.Contains(content, "X-Codegen-Client-Name") {
		t.Fatalf("stale CodeGen header was not removed:\n%s", content)
	}
}

func TestWriteCodexConfigPreservesNonCodeGenHTTPHeaders(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")

	codexDir := filepath.Join(tmpHome, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatalf("create codex dir: %v", err)
	}
	configContent := `model_provider = "custom"
model = "old-model"

[model_providers.custom]
name = "custom"
base_url = "https://old.example/v1"
wire_api = "responses"
http_headers = { "X-Custom" = "keep" }
supports_websockets = false
`
	if err := AtomicWrite(CodexConfigPath(), []byte(configContent)); err != nil {
		t.Fatalf("seed config.toml: %v", err)
	}

	if err := WriteCodexConfig("sk-test", "https://api.example.com/v1", "gpt-test", "custom", "responses"); err != nil {
		t.Fatalf("WriteCodexConfig: %v", err)
	}
	data, err := os.ReadFile(CodexConfigPath())
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, `http_headers = { "X-Custom" = "keep" }`) {
		t.Fatalf("custom http_headers were not preserved:\n%s", content)
	}
}

func TestWriteCodexConfigRemovesOnlyCodeGenHTTPHeader(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")

	codexDir := filepath.Join(tmpHome, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatalf("create codex dir: %v", err)
	}
	configContent := `model_provider = "custom"
model = "old-model"

[model_providers.custom]
name = "custom"
base_url = "https://old.example/v1"
wire_api = "responses"
http_headers = { "X-Custom" = "keep", "X-Codegen-Client-Name" = "custom-agent" }
supports_websockets = false
`
	if err := AtomicWrite(CodexConfigPath(), []byte(configContent)); err != nil {
		t.Fatalf("seed config.toml: %v", err)
	}

	if err := WriteCodexConfig("sk-test", "https://api.example.com/v1", "gpt-test", "custom", "responses"); err != nil {
		t.Fatalf("WriteCodexConfig: %v", err)
	}
	data, err := os.ReadFile(CodexConfigPath())
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	content := string(data)
	if strings.Contains(content, "X-Codegen-Client-Name") {
		t.Fatalf("CodeGen header was not removed:\n%s", content)
	}
	if !strings.Contains(content, `http_headers = { "X-Custom" = "keep" }`) {
		t.Fatalf("custom http_headers were not preserved:\n%s", content)
	}
}

func TestWriteCodexConfigPreservesHTTPHeaderCommentWhenRemovingCodeGenHeader(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")

	codexDir := filepath.Join(tmpHome, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatalf("create codex dir: %v", err)
	}
	configContent := `model_provider = "custom"
model = "old-model"

[model_providers.custom]
name = "custom"
base_url = "https://old.example/v1"
wire_api = "responses"
http_headers = { "X-Custom" = "keep", "X-Codegen-Client-Name" = "custom-agent" } # preserve me
supports_websockets = false
`
	if err := AtomicWrite(CodexConfigPath(), []byte(configContent)); err != nil {
		t.Fatalf("seed config.toml: %v", err)
	}

	if err := WriteCodexConfig("sk-test", "https://api.example.com/v1", "gpt-test", "custom", "responses"); err != nil {
		t.Fatalf("WriteCodexConfig: %v", err)
	}
	data, err := os.ReadFile(CodexConfigPath())
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	content := string(data)
	if strings.Contains(content, "X-Codegen-Client-Name") {
		t.Fatalf("CodeGen header was not removed:\n%s", content)
	}
	if !strings.Contains(content, `http_headers = { "X-Custom" = "keep" } # preserve me`) {
		t.Fatalf("custom http_headers comment was not preserved:\n%s", content)
	}
}

func TestWriteCodexConfigHandlesBareHTTPHeaderKeys(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")

	codexDir := filepath.Join(tmpHome, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatalf("create codex dir: %v", err)
	}
	configContent := `model_provider = "custom"
model = "old-model"

[model_providers.custom]
name = "custom"
base_url = "https://old.example/v1"
wire_api = "responses"
http_headers = { X-Custom = 'keep', X-Codegen-Client-Name = "custom-agent" }
supports_websockets = false
`
	if err := AtomicWrite(CodexConfigPath(), []byte(configContent)); err != nil {
		t.Fatalf("seed config.toml: %v", err)
	}

	if err := WriteCodexConfig("sk-test", "https://api.example.com/v1", "gpt-test", "custom", "responses"); err != nil {
		t.Fatalf("WriteCodexConfig: %v", err)
	}
	data, err := os.ReadFile(CodexConfigPath())
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	content := string(data)
	if strings.Contains(content, "X-Codegen-Client-Name") {
		t.Fatalf("CodeGen header was not removed:\n%s", content)
	}
	if !strings.Contains(content, `http_headers = { "X-Custom" = "keep" }`) {
		t.Fatalf("bare custom http_headers were not preserved:\n%s", content)
	}
}

func TestWriteCodexConfigPreservesHashInsideSingleQuotedHeaderValue(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")

	codexDir := filepath.Join(tmpHome, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatalf("create codex dir: %v", err)
	}
	configContent := `model_provider = "custom"
model = "old-model"

[model_providers.custom]
name = "custom"
base_url = "https://old.example/v1"
wire_api = "responses"
http_headers = { X-Custom = 'keep#literal', X-Codegen-Client-Name = "custom-agent" } # preserve me
supports_websockets = false
`
	if err := AtomicWrite(CodexConfigPath(), []byte(configContent)); err != nil {
		t.Fatalf("seed config.toml: %v", err)
	}

	if err := WriteCodexConfig("sk-test", "https://api.example.com/v1", "gpt-test", "custom", "responses"); err != nil {
		t.Fatalf("WriteCodexConfig: %v", err)
	}
	data, err := os.ReadFile(CodexConfigPath())
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	content := string(data)
	if strings.Contains(content, "X-Codegen-Client-Name") {
		t.Fatalf("CodeGen header was not removed:\n%s", content)
	}
	if !strings.Contains(content, `http_headers = { "X-Custom" = "keep#literal" } # preserve me`) {
		t.Fatalf("single-quoted header value with # was not preserved:\n%s", content)
	}
}

func TestWriteCodexConfigPreservesBraceInsideHeaderValue(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")

	codexDir := filepath.Join(tmpHome, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatalf("create codex dir: %v", err)
	}
	configContent := `model_provider = "custom"
model = "old-model"

[model_providers.custom]
name = "custom"
base_url = "https://old.example/v1"
wire_api = "responses"
http_headers = { X-Custom = 'keep}', X-Codegen-Client-Name = "custom-agent" } # preserve me
supports_websockets = false
`
	if err := AtomicWrite(CodexConfigPath(), []byte(configContent)); err != nil {
		t.Fatalf("seed config.toml: %v", err)
	}

	if err := WriteCodexConfig("sk-test", "https://api.example.com/v1", "gpt-test", "custom", "responses"); err != nil {
		t.Fatalf("WriteCodexConfig: %v", err)
	}
	data, err := os.ReadFile(CodexConfigPath())
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	content := string(data)
	if strings.Contains(content, "X-Codegen-Client-Name") {
		t.Fatalf("CodeGen header was not removed:\n%s", content)
	}
	if !strings.Contains(content, `http_headers = { "X-Custom" = "keep}" } # preserve me`) {
		t.Fatalf("header value with } was not preserved:\n%s", content)
	}
}

func TestCodexProviderKeyNormalizesKnownProviderNames(t *testing.T) {
	cases := map[string]string{
		"openai":                   "openai-compatible",
		"\u8baf\u98de\u661f\u8fb0": "xfyun",
		"\u963f\u91cc\u4e91":       "aliyun",
		"\u767e\u5ea6\u5343\u5e06": "qianfan",
		" custom provider! ":       "customprovider",
		"\u2603\u2603":             "custom",
	}
	for input, want := range cases {
		if got := CodexProviderKey(input); got != want {
			t.Fatalf("CodexProviderKey(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestClearCodexThirdPartySettings_ClearsAuthJSON(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")

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
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")

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

	// Non-Codex-websocket features should be preserved.
	if !strings.Contains(content, "[features]") {
		t.Error("[features] section should be preserved")
	}
	if strings.Contains(content, "responses_websockets_v2 = true") {
		t.Error("responses_websockets_v2 should have been removed because configured providers do not support websockets")
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
	restore := stubCodexProcessHooks(t)
	defer restore()
	findCodexProcessesFunc = func() ([]codexProcess, error) {
		t.Fatal("process check should not run when there is no codex config to clear")
		return nil, nil
	}

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
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")

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
	if strings.Contains(content, "responses_websockets_v2 = true") {
		t.Error("responses_websockets_v2 should have been removed because configured providers do not support websockets")
	}
}

func TestRestoreCodexOpenAISettingsPreservesAuthAndUpdatesAllSessions(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")

	codexDir := filepath.Join(tmpHome, ".codex")
	if err := os.MkdirAll(filepath.Join(codexDir, "sessions"), 0755); err != nil {
		t.Fatalf("create codex sessions: %v", err)
	}
	if err := AtomicWriteJSON(filepath.Join(codexDir, "auth.json"), map[string]string{"OPENAI_API_KEY": "sk-openai"}); err != nil {
		t.Fatalf("seed auth: %v", err)
	}
	config := `model_provider = "tigerproxy"
model = "gpt-5.6-terra"

[model_providers.tigerproxy]
name = "TigerProxy"
base_url = "http://127.0.0.1:18086/v1"

[features]
js_repl = false
`
	if err := AtomicWrite(filepath.Join(codexDir, "config.toml"), []byte(config)); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	jsonl := strings.Join([]string{
		`{"type":"session_meta","payload":{"model_provider":"tigerproxy"}}`,
		`{"type":"turn_context","payload":{"model":"gpt-5.6-terra","collaboration_mode":{"settings":{"model":"gpt-5.6-terra"}}}}`,
	}, "\n") + "\n"
	jsonlPath := filepath.Join(codexDir, "sessions", "rollout-test.jsonl")
	if err := os.WriteFile(jsonlPath, []byte(jsonl), 0644); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	dbPath := filepath.Join(codexDir, "state_5.sqlite")
	createCodexStateDB(t, dbPath, "tigerproxy", "gpt-5.6-terra")
	archivedDir := filepath.Join(codexDir, "archived_sessions")
	if err := os.MkdirAll(archivedDir, 0755); err != nil {
		t.Fatalf("create archived sessions: %v", err)
	}
	if err := os.WriteFile(filepath.Join(archivedDir, "rollout-archived.jsonl"), []byte(jsonl), 0644); err != nil {
		t.Fatalf("seed archived session: %v", err)
	}
	olderDBPath := filepath.Join(codexDir, "state_4.sqlite")
	createCodexStateDB(t, olderDBPath, "tigerproxy", "gpt-5.6-terra")

	cleared, err := RestoreCodexOpenAISettingsWithProxyKey("sk-openai")
	if err != nil {
		t.Fatalf("RestoreCodexOpenAISettingsWithProxyKey: %v", err)
	}
	if !cleared {
		t.Fatal("proxy auth should have been cleared")
	}

	if _, err := os.Stat(filepath.Join(codexDir, "auth.json")); !os.IsNotExist(err) {
		t.Fatalf("proxy auth should be removed, got: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(codexDir, "config.toml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(content), "tigerproxy") || !strings.Contains(string(content), "js_repl = false") {
		t.Fatalf("unexpected restored config:\n%s", content)
	}
	data, err := os.ReadFile(jsonlPath)
	if err != nil {
		t.Fatalf("read session: %v", err)
	}
	if !strings.Contains(string(data), `"model_provider":"openai"`) || !strings.Contains(string(data), `"model":"gpt-5.6"`) {
		t.Fatalf("session was not restored:\n%s", data)
	}
	assertCodexStateRow(t, dbPath, "openai", "gpt-5.6")
	assertCodexStateRow(t, olderDBPath, "openai", "gpt-5.6")
	archivedData, err := os.ReadFile(filepath.Join(archivedDir, "rollout-archived.jsonl"))
	if err != nil {
		t.Fatalf("read archived session: %v", err)
	}
	if !strings.Contains(string(archivedData), `"model_provider":"openai"`) {
		t.Fatalf("archived session was not restored:\n%s", archivedData)
	}
}

func TestRestoreCodexOpenAISettingsWithProxyKeyPreservesDifferentOpenAIAuth(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")
	codexDir := filepath.Join(tmpHome, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatalf("create codex dir: %v", err)
	}
	if err := AtomicWriteJSON(filepath.Join(codexDir, "auth.json"), map[string]string{"OPENAI_API_KEY": "sk-official"}); err != nil {
		t.Fatalf("seed auth: %v", err)
	}
	if err := AtomicWrite(filepath.Join(codexDir, "config.toml"), []byte("model_provider = \"tigerproxy\"\n")); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	cleared, err := RestoreCodexOpenAISettingsWithProxyKey("tigerproxy-local-key")
	if err != nil {
		t.Fatalf("RestoreCodexOpenAISettingsWithProxyKey: %v", err)
	}
	if cleared {
		t.Fatal("official OpenAI auth must not be cleared")
	}
	data, err := os.ReadFile(filepath.Join(codexDir, "auth.json"))
	if err != nil || !strings.Contains(string(data), "sk-official") {
		t.Fatalf("official auth should be preserved: %q, %v", data, err)
	}
}

func TestCodexRestoreSnapshotRestoresExistingAndNewFiles(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "existing.txt")
	newFile := filepath.Join(dir, "new.txt")
	if err := os.WriteFile(existing, []byte("before"), 0644); err != nil {
		t.Fatalf("seed existing: %v", err)
	}
	snapshot := codexRestoreSnapshot{
		{path: existing, exists: true, data: []byte("before")},
		{path: newFile, exists: false},
	}
	if err := os.WriteFile(existing, []byte("after"), 0644); err != nil {
		t.Fatalf("change existing: %v", err)
	}
	if err := os.WriteFile(newFile, []byte("new"), 0644); err != nil {
		t.Fatalf("create new: %v", err)
	}

	if err := snapshot.restore(); err != nil {
		t.Fatalf("restore snapshot: %v", err)
	}
	data, err := os.ReadFile(existing)
	if err != nil || string(data) != "before" {
		t.Fatalf("existing = %q, %v; want before", data, err)
	}
	if _, err := os.Stat(newFile); !os.IsNotExist(err) {
		t.Fatalf("new file should be removed, got %v", err)
	}
}

func TestWriteCodexConfigPreservesNonTargetSectionsAndUpdatesProvider(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")

	codexDir := filepath.Join(tmpHome, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatalf("create codex dir: %v", err)
	}
	configContent := `model_provider = "old"
model = "old-model"
model_reasoning_effort = "high"

[model_providers.custom]
name = "custom"
base_url = "https://old.example/v1"
supports_websockets = true
requires_openai_auth = true

[model_providers.openai-compatible]
name = "openai-compatible"
wire_api = "responses"
supports_websockets = true
requires_openai_auth = true

[profile.default]
model = "profile-model"

[features]
responses_websockets_v2 = true

[mcp_servers.filesystem]
command = "npx"
`
	if err := AtomicWrite(CodexConfigPath(), []byte(configContent)); err != nil {
		t.Fatalf("seed config.toml: %v", err)
	}

	if err := WriteCodexConfig("sk-test", "https://new.example/v1", "gpt-5.5", "custom", "responses"); err != nil {
		t.Fatalf("WriteCodexConfig: %v", err)
	}

	data, err := os.ReadFile(CodexConfigPath())
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	content := string(data)
	for _, want := range []string{
		`model_provider = "custom"`,
		`model = "gpt-5.5"`,
		`base_url = "https://new.example/v1"`,
		`wire_api = "responses"`,
		`supports_websockets = false`,
		`[model_providers.openai-compatible]`,
		`[profile.default]`,
		`model = "profile-model"`,
		`[mcp_servers.filesystem]`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("config.toml missing %q:\n%s", want, content)
		}
	}
	for _, forbidden := range []string{
		`requires_openai_auth = true`,
		`supports_websockets = true`,
		`responses_websockets_v2 = true`,
	} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("OpenAI/websocket marker %q should have been removed:\n%s", forbidden, content)
		}
	}
}

func TestWriteTigerProxyCodexConfigAddsContextSettings(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")

	if err := WriteTigerProxyCodexConfig("sk-test", "http://127.0.0.1:18086/v1", "gpt-5.5"); err != nil {
		t.Fatalf("WriteTigerProxyCodexConfig: %v", err)
	}
	data, err := os.ReadFile(CodexConfigPath())
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	content := string(data)
	for _, want := range []string{
		`model_context_window = 199000`,
		`model_auto_compact_token_limit = 180000`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("TigerProxy config missing %q:\n%s", want, content)
		}
	}
}

func TestWriteTigerProxyCodexConfigWithContextUsesOverrides(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")

	if err := WriteTigerProxyCodexConfigWithContext("sk-test", "http://127.0.0.1:18086/v1", "gpt-5.5", 256000, 220000); err != nil {
		t.Fatalf("WriteTigerProxyCodexConfigWithContext: %v", err)
	}
	data, err := os.ReadFile(CodexConfigPath())
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	content := string(data)
	for _, want := range []string{
		`model_context_window = 256000`,
		`model_auto_compact_token_limit = 220000`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("TigerProxy config missing override %q:\n%s", want, content)
		}
	}
}

func TestWriteTigerProxyCodexConfigWithContextRejectsInvalidThreshold(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")

	err := WriteTigerProxyCodexConfigWithContext("sk-test", "http://127.0.0.1:18086/v1", "gpt-5.5", 180000, 180000)
	if err == nil || !strings.Contains(err.Error(), "must be less than the context window") {
		t.Fatalf("WriteTigerProxyCodexConfigWithContext error = %v, want invalid threshold error", err)
	}
	if _, err := os.Stat(CodexConfigPath()); !os.IsNotExist(err) {
		t.Fatalf("config.toml exists after rejected settings, stat err = %v", err)
	}
}

func TestSyncTigerProxyCodexAPIKeyIfConfiguredUpdatesOnlyTigerProxy(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	codexDir := filepath.Join(tmpHome, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWrite(filepath.Join(codexDir, "config.toml"), []byte("model_provider = 'tigerproxy' # active local provider\n")); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWriteJSON(filepath.Join(codexDir, "auth.json"), map[string]interface{}{
		"OPENAI_API_KEY": "old-proxy-key",
		"tokens":         map[string]interface{}{"access_token": "preserve-me"},
	}); err != nil {
		t.Fatal(err)
	}

	result, err := SyncTigerProxyCodexAPIKeyIfConfigured("new-proxy-key")
	if err != nil || !result.Configured || !result.Updated {
		t.Fatalf("SyncTigerProxyCodexAPIKeyIfConfigured = %+v, %v; want configured and updated, nil", result, err)
	}
	auth, err := ReadCodexAuth()
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := auth["OPENAI_API_KEY"].(string); got != "new-proxy-key" {
		t.Fatalf("OPENAI_API_KEY = %q, want updated key", got)
	}
	if _, ok := auth["tokens"]; !ok {
		t.Fatal("unrelated auth.json fields were not preserved")
	}

	result, err = SyncTigerProxyCodexAPIKeyIfConfigured("new-proxy-key")
	if err != nil || !result.Configured || result.Updated {
		t.Fatalf("second sync = %+v, %v; want configured but unchanged, nil", result, err)
	}
}

func TestSyncTigerProxyCodexAPIKeyIfConfiguredLeavesOtherProvidersUntouched(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	codexDir := filepath.Join(tmpHome, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWrite(filepath.Join(codexDir, "config.toml"), []byte("model_provider = \"openai\"\n")); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWriteJSON(filepath.Join(codexDir, "auth.json"), map[string]string{"OPENAI_API_KEY": "official-key"}); err != nil {
		t.Fatal(err)
	}

	result, err := SyncTigerProxyCodexAPIKeyIfConfigured("new-proxy-key")
	if err != nil || result.Configured || result.Updated {
		t.Fatalf("SyncTigerProxyCodexAPIKeyIfConfigured = %+v, %v; want not configured, nil", result, err)
	}
	auth, err := ReadCodexAuth()
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := auth["OPENAI_API_KEY"].(string); got != "official-key" {
		t.Fatalf("OPENAI_API_KEY = %q, want official key unchanged", got)
	}
}

func TestSyncTigerProxyCodexAPIKeyIfConfiguredRefusesMalformedAuth(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	codexDir := filepath.Join(tmpHome, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWrite(filepath.Join(codexDir, "config.toml"), []byte("model_provider = \"tigerproxy\"\n")); err != nil {
		t.Fatal(err)
	}
	malformed := []byte(`{"OPENAI_API_KEY":`)
	authPath := filepath.Join(codexDir, "auth.json")
	if err := AtomicWrite(authPath, malformed); err != nil {
		t.Fatal(err)
	}

	result, err := SyncTigerProxyCodexAPIKeyIfConfigured("new-proxy-key")
	if err == nil || !result.Configured || result.Updated {
		t.Fatalf("SyncTigerProxyCodexAPIKeyIfConfigured = %+v, %v; want configured and parse error", result, err)
	}
	after, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(malformed) {
		t.Fatalf("malformed auth was overwritten: got %q, want %q", after, malformed)
	}
}

func TestSyncTigerProxyCodexAPIKeyIfConfiguredRefusesNullAuth(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	codexDir := filepath.Join(tmpHome, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWrite(filepath.Join(codexDir, "config.toml"), []byte("model_provider = \"tigerproxy\"\n")); err != nil {
		t.Fatal(err)
	}
	authPath := filepath.Join(codexDir, "auth.json")
	if err := AtomicWrite(authPath, []byte("null\n")); err != nil {
		t.Fatal(err)
	}

	result, err := SyncTigerProxyCodexAPIKeyIfConfigured("new-proxy-key")
	if err == nil || !result.Configured || result.Updated {
		t.Fatalf("SyncTigerProxyCodexAPIKeyIfConfigured = %+v, %v; want configured and parse error", result, err)
	}
	after, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != "null\n" {
		t.Fatalf("null auth was overwritten: got %q", after)
	}
}

func TestSyncTigerProxyCodexAPIKeyIfConfiguredCreatesMissingAuth(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	codexDir := filepath.Join(tmpHome, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWrite(filepath.Join(codexDir, "config.toml"), []byte("model_provider = \"tigerproxy\"\n")); err != nil {
		t.Fatal(err)
	}

	result, err := SyncTigerProxyCodexAPIKeyIfConfigured("new-proxy-key")
	if err != nil || !result.Configured || !result.Updated {
		t.Fatalf("SyncTigerProxyCodexAPIKeyIfConfigured = %+v, %v; want configured and updated, nil", result, err)
	}
	auth, err := ReadCodexAuth()
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := auth["OPENAI_API_KEY"].(string); got != "new-proxy-key" {
		t.Fatalf("OPENAI_API_KEY = %q, want new-proxy-key", got)
	}
}

func TestWriteTigerProxyCodexConfigUpdatesExistingContextSettings(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")

	if err := os.MkdirAll(filepath.Dir(CodexConfigPath()), 0o755); err != nil {
		t.Fatalf("create Codex config directory: %v", err)
	}
	seed := "model_provider = \"tigerproxy\"\nmodel = \"old-model\"\nmodel_context_window = 128000\nmodel_auto_compact_token_limit = 115000\n"
	if err := AtomicWrite(CodexConfigPath(), []byte(seed)); err != nil {
		t.Fatalf("seed config.toml: %v", err)
	}

	if err := WriteTigerProxyCodexConfig("sk-test", "http://127.0.0.1:18086/v1", "gpt-5.5"); err != nil {
		t.Fatalf("WriteTigerProxyCodexConfig: %v", err)
	}
	data, err := os.ReadFile(CodexConfigPath())
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	content := string(data)
	if strings.Contains(content, "model_context_window = 128000") || strings.Contains(content, "model_auto_compact_token_limit = 115000") {
		t.Fatalf("TigerProxy context settings were not updated:\n%s", content)
	}
	for _, want := range []string{
		`model_context_window = 199000`,
		`model_auto_compact_token_limit = 180000`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("TigerProxy config missing %q:\n%s", want, content)
		}
	}
}

func TestWriteTigerProxyCodexConfigAddsContextSettingsToCompactToml(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")

	if err := os.MkdirAll(filepath.Dir(CodexConfigPath()), 0o755); err != nil {
		t.Fatalf("create Codex config directory: %v", err)
	}
	seed := "model_provider=\"tigerproxy\"\nmodel=\"old-model\"\n"
	if err := AtomicWrite(CodexConfigPath(), []byte(seed)); err != nil {
		t.Fatalf("seed config.toml: %v", err)
	}

	if err := WriteTigerProxyCodexConfig("sk-test", "http://127.0.0.1:18086/v1", "gpt-5.5"); err != nil {
		t.Fatalf("WriteTigerProxyCodexConfig: %v", err)
	}
	data, err := os.ReadFile(CodexConfigPath())
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	content := string(data)
	for _, want := range []string{
		`model_context_window = 199000`,
		`model_auto_compact_token_limit = 180000`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("TigerProxy config missing %q:\n%s", want, content)
		}
	}
}

func TestWriteTigerProxyCodexConfigIgnoresSimilarTopLevelKeys(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")

	if err := os.MkdirAll(filepath.Dir(CodexConfigPath()), 0o755); err != nil {
		t.Fatalf("create Codex config directory: %v", err)
	}
	seed := "model_provider_alias=\"legacy\"\n"
	if err := AtomicWrite(CodexConfigPath(), []byte(seed)); err != nil {
		t.Fatalf("seed config.toml: %v", err)
	}

	if err := WriteTigerProxyCodexConfig("sk-test", "http://127.0.0.1:18086/v1", "gpt-5.5"); err != nil {
		t.Fatalf("WriteTigerProxyCodexConfig: %v", err)
	}
	data, err := os.ReadFile(CodexConfigPath())
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "model_provider_alias=\"legacy\"") {
		t.Fatalf("similar user key was not preserved:\n%s", content)
	}
	if !strings.Contains(content, `model_provider = "tigerproxy"`) || !strings.Contains(content, `model = "gpt-5.5"`) {
		t.Fatalf("TigerProxy model settings were not inserted:\n%s", content)
	}
}

func TestWriteTigerProxyCodexConfigPreservesProviderSectionComment(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")

	if err := os.MkdirAll(filepath.Dir(CodexConfigPath()), 0o755); err != nil {
		t.Fatalf("create Codex config directory: %v", err)
	}
	seed := "model_provider = \"tigerproxy\"\nmodel = \"old-model\"\n\n[model_providers.tigerproxy] # keep this comment\nname = \"old\"\n"
	if err := AtomicWrite(CodexConfigPath(), []byte(seed)); err != nil {
		t.Fatalf("seed config.toml: %v", err)
	}

	if err := WriteTigerProxyCodexConfig("sk-test", "http://127.0.0.1:18086/v1", "gpt-5.5"); err != nil {
		t.Fatalf("WriteTigerProxyCodexConfig: %v", err)
	}
	data, err := os.ReadFile(CodexConfigPath())
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	content := string(data)
	if strings.Count(content, "[model_providers.tigerproxy]") != 1 {
		t.Fatalf("TigerProxy provider section was duplicated:\n%s", content)
	}
	if !strings.Contains(content, "[model_providers.tigerproxy] # keep this comment") {
		t.Fatalf("provider section comment was not preserved:\n%s", content)
	}
}

func TestWriteTigerProxyCodexConfigRecognizesQuotedProviderSection(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")
	if err := os.MkdirAll(filepath.Dir(CodexConfigPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	seed := "model_provider = \"tigerproxy\"\nmodel = \"old\"\n\n[model_providers.\"tigerproxy\"]\nname = \"old\"\n"
	if err := AtomicWrite(CodexConfigPath(), []byte(seed)); err != nil {
		t.Fatal(err)
	}
	if err := WriteTigerProxyCodexConfig("sk-test", "http://127.0.0.1:18086/v1", "gpt-5.5"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(CodexConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "model_providers.") != 1 {
		t.Fatalf("provider section duplicated:\n%s", data)
	}
}

func TestWriteTigerProxyCodexConfigRecognizesSingleQuotedProviderSection(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")
	if err := os.MkdirAll(filepath.Dir(CodexConfigPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	seed := "model_provider = \"tigerproxy\"\nmodel = \"old\"\n\n[model_providers.'tigerproxy']\nname = \"old\"\n"
	if err := AtomicWrite(CodexConfigPath(), []byte(seed)); err != nil {
		t.Fatal(err)
	}
	if err := WriteTigerProxyCodexConfig("sk-test", "http://127.0.0.1:18086/v1", "gpt-5.5"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(CodexConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "model_providers.") != 1 {
		t.Fatalf("provider section duplicated:\n%s", data)
	}
}

func TestWriteCodexConfigPreservesTigerProxyContextSettings(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")

	if err := os.MkdirAll(filepath.Dir(CodexConfigPath()), 0o755); err != nil {
		t.Fatalf("create Codex config directory: %v", err)
	}
	seed := "model_provider = \"custom\"\nmodel = \"old-model\"\nmodel_context_window = 199000\nmodel_auto_compact_token_limit = 180000\n"
	if err := AtomicWrite(CodexConfigPath(), []byte(seed)); err != nil {
		t.Fatalf("seed config.toml: %v", err)
	}

	if err := WriteCodexConfig("sk-test", "https://api.example.com/v1", "gpt-5.5", "custom", "responses"); err != nil {
		t.Fatalf("WriteCodexConfig: %v", err)
	}
	data, err := os.ReadFile(CodexConfigPath())
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	content := string(data)
	for _, want := range []string{
		`model_context_window = 199000`,
		`model_auto_compact_token_limit = 180000`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("generic writer unexpectedly modified %q:\n%s", want, content)
		}
	}
}

func TestWriteCodexConfigAtUsesScopedDirAndRemovesStaleAuthWhenKeyEmpty(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")

	scopedDir := filepath.Join(tmpHome, "project", ".aicoder", "codex", "instance-1")
	if err := os.MkdirAll(scopedDir, 0755); err != nil {
		t.Fatalf("create scoped dir: %v", err)
	}
	if err := AtomicWriteJSON(filepath.Join(scopedDir, "auth.json"), map[string]string{"OPENAI_API_KEY": "sk-stale"}); err != nil {
		t.Fatalf("seed scoped auth: %v", err)
	}
	configContent := `model_provider = "old"
model = "old-model"

[mcp_servers.filesystem]
command = "npx"
`
	if err := AtomicWrite(filepath.Join(scopedDir, "config.toml"), []byte(configContent)); err != nil {
		t.Fatalf("seed scoped config: %v", err)
	}

	if err := WriteCodexConfigAt(scopedDir, "", "https://new.example/v1", "gpt-5.5", "custom", "responses"); err != nil {
		t.Fatalf("WriteCodexConfigAt: %v", err)
	}

	if _, err := os.Stat(filepath.Join(scopedDir, "auth.json")); !os.IsNotExist(err) {
		t.Fatalf("stale scoped auth.json should be removed, stat err=%v", err)
	}
	data, err := os.ReadFile(filepath.Join(scopedDir, "config.toml"))
	if err != nil {
		t.Fatalf("read scoped config: %v", err)
	}
	content := string(data)
	for _, want := range []string{
		`model_provider = "custom"`,
		`model = "gpt-5.5"`,
		`base_url = "https://new.example/v1"`,
		`[mcp_servers.filesystem]`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("scoped config missing %q:\n%s", want, content)
		}
	}
}

func TestWriteCodexConfigSyncsSessionJSONLAndNewestStateDB(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")

	codexDir := filepath.Join(tmpHome, ".codex")
	sessionDir := filepath.Join(codexDir, "sessions", "2026", "05", "08")
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		t.Fatalf("create session dir: %v", err)
	}
	jsonlPath := filepath.Join(sessionDir, "rollout-test.jsonl")
	jsonl := strings.Join([]string{
		`{"type":"session_meta","payload":{"id":"s1","model_provider":"old"}}`,
		`{"type":"turn_context","payload":{"model":"old-model","collaboration_mode":{"settings":{"model":"old-model"}}}}`,
		`not-json`,
		"",
	}, "\n")
	if err := os.WriteFile(jsonlPath, []byte(jsonl), 0644); err != nil {
		t.Fatalf("seed jsonl: %v", err)
	}

	oldDB := filepath.Join(codexDir, "state_1.sqlite")
	newDB := filepath.Join(codexDir, "state_2.sqlite")
	createCodexStateDB(t, oldDB, "old", "old-model")
	createCodexStateDB(t, newDB, "old", "old-model")
	oldTime := time.Now().Add(-1 * time.Hour)
	newTime := time.Now()
	if err := os.Chtimes(oldDB, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes old db: %v", err)
	}
	if err := os.Chtimes(newDB, newTime, newTime); err != nil {
		t.Fatalf("chtimes new db: %v", err)
	}

	if err := WriteCodexConfigAt(codexDir, "sk-test", "https://new.example/v1", "gpt-5.5", "custom", "responses"); err != nil {
		t.Fatalf("WriteCodexConfigAt: %v", err)
	}

	data, err := os.ReadFile(jsonlPath)
	if err != nil {
		t.Fatalf("read jsonl: %v", err)
	}
	content := string(data)
	for _, want := range []string{
		`"model_provider":"custom"`,
		`"model":"gpt-5.5"`,
		`"settings":{"model":"gpt-5.5"}`,
		`not-json`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("jsonl missing %q:\n%s", want, content)
		}
	}

	assertCodexStateRow(t, newDB, "custom", "gpt-5.5")
	assertCodexStateRow(t, oldDB, "old", "old-model")
}

func TestSyncCodexSessionStateCanBeSkipped(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("AICODER_SKIP_CODEX_SESSION_SYNC", "1")
	codexDir := filepath.Join(tmpHome, ".codex")
	sessionDir := filepath.Join(codexDir, "sessions")
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		t.Fatalf("create session dir: %v", err)
	}
	jsonlPath := filepath.Join(sessionDir, "rollout-test.jsonl")
	original := `{"type":"session_meta","payload":{"model_provider":"old"}}` + "\n"
	if err := os.WriteFile(jsonlPath, []byte(original), 0644); err != nil {
		t.Fatalf("seed jsonl: %v", err)
	}

	if err := syncCodexSessionState(codexDir, "custom", "gpt-5.5"); err != nil {
		t.Fatalf("syncCodexSessionState: %v", err)
	}
	data, err := os.ReadFile(jsonlPath)
	if err != nil {
		t.Fatalf("read jsonl: %v", err)
	}
	if string(data) != original {
		t.Fatalf("jsonl changed despite skip env:\n%s", data)
	}
}

func TestDiscoverCodexStateDBsHandlesSpecialPathCharacters(t *testing.T) {
	codexDir := filepath.Join(t.TempDir(), "codex home #1")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatalf("create codex dir: %v", err)
	}
	dbPath := filepath.Join(codexDir, "state_5.sqlite")
	createCodexStateDB(t, dbPath, "old", "old-model")

	dbs, err := discoverCodexStateDBs(codexDir)
	if err != nil {
		t.Fatalf("discoverCodexStateDBs: %v", err)
	}
	if len(dbs) != 1 || dbs[0] != dbPath {
		t.Fatalf("dbs = %#v, want [%s]", dbs, dbPath)
	}
}

func TestParseCodexProcessLinesSkipsCurrentProcessAndDeduplicates(t *testing.T) {
	current := os.Getpid()
	input := strings.Join([]string{
		"1234\tcodex.exe\tcodex exec --json",
		"1234\tcodex.exe\tduplicate",
		"2345\tcodex.test.exe\tgo test ./corelib/configfile",
		"3456\tnotcodex.exe\tunrelated process with codex in the name",
		strconv.Itoa(current) + "\tcodex.test.exe\tcurrent test process",
		"bad\tcodex.exe\tbad pid",
	}, "\n")

	got := parseCodexProcessLines(input)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1: %#v", len(got), got)
	}
	if got[0].PID != 1234 || got[0].Name != "codex.exe" || !strings.Contains(got[0].CommandLine, "codex exec") {
		t.Fatalf("unexpected parsed process: %#v", got[0])
	}
}

func TestIsCodexProcessCandidateMatchesCLIButNotIncidentalNames(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
		want bool
	}{
		{name: "codex.exe", cmd: "codex exec --json", want: true},
		{name: "node.exe", cmd: `node C:\Users\me\AppData\Roaming\npm\node_modules\@openai\codex\bin\codex.js exec`, want: true},
		{name: "powershell.exe", cmd: `powershell -Command codex exec --json`, want: true},
		{name: "Codex.exe", cmd: `"C:\Program Files\WindowsApps\OpenAI.Codex_26.623.13972.0_x64__2p2nqsd0c76g0\app\Codex.exe"`, want: true},
		{name: "openai.exe", cmd: `openai api request`, want: false},
		{name: "codex.test.exe", cmd: "go test ./corelib/configfile", want: false},
		{name: "notcodex.exe", cmd: "unrelated", want: false},
	}
	for _, tc := range cases {
		if got := isCodexProcessCandidate(tc.name, tc.cmd); got != tc.want {
			t.Fatalf("isCodexProcessCandidate(%q, %q) = %v, want %v", tc.name, tc.cmd, got, tc.want)
		}
	}
}

func TestEnsureCodexProcessesStoppedKillsThenVerifies(t *testing.T) {
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "")
	restore := stubCodexProcessHooks(t)
	defer restore()

	calls := 0
	findCodexProcessesFunc = func() ([]codexProcess, error) {
		calls++
		if calls == 1 {
			return []codexProcess{{PID: 1234, Name: "codex.exe", CommandLine: "codex exec --json"}}, nil
		}
		return nil, nil
	}
	var killed []int
	killProcessTreeFunc = func(pid int) error {
		killed = append(killed, pid)
		return nil
	}

	if err := ensureCodexProcessesStopped(); err != nil {
		t.Fatalf("ensureCodexProcessesStopped: %v", err)
	}
	if len(killed) != 1 || killed[0] != 1234 {
		t.Fatalf("killed = %#v, want [1234]", killed)
	}
	if calls < 2 {
		t.Fatalf("find calls = %d, want initial check plus verification", calls)
	}
}

func TestEnsureCodexProcessesStoppedFailsWhenKillFails(t *testing.T) {
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "")
	restore := stubCodexProcessHooks(t)
	defer restore()

	findCodexProcessesFunc = func() ([]codexProcess, error) {
		return []codexProcess{{PID: 1234, Name: "codex.exe", CommandLine: "codex exec --json"}}, nil
	}
	killProcessTreeFunc = func(pid int) error {
		return errors.New("access denied")
	}

	err := ensureCodexProcessesStopped()
	if err == nil {
		t.Fatal("expected kill failure")
	}
	if !strings.Contains(err.Error(), "kill codex process pid=1234") || !strings.Contains(err.Error(), "access denied") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureCodexProcessesStoppedFailsWhenProcessRemains(t *testing.T) {
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "")
	restore := stubCodexProcessHooks(t)
	defer restore()

	findCodexProcessesFunc = func() ([]codexProcess, error) {
		return []codexProcess{{PID: 1234, Name: "codex.exe", CommandLine: "codex exec --json"}}, nil
	}
	killProcessTreeFunc = func(pid int) error { return nil }

	err := ensureCodexProcessesStopped()
	if err == nil {
		t.Fatal("expected remaining process failure")
	}
	if !strings.Contains(err.Error(), "codex processes still running after kill") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func stubCodexProcessHooks(t *testing.T) func() {
	t.Helper()
	oldFind := findCodexProcessesFunc
	oldKill := killProcessTreeFunc
	oldSleep := codexProcessStopSleep
	codexProcessStopSleep = func(time.Duration) {}
	return func() {
		findCodexProcessesFunc = oldFind
		killProcessTreeFunc = oldKill
		codexProcessStopSleep = oldSleep
	}
}

func createCodexStateDB(t *testing.T, path, provider, model string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open state db: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE threads (id TEXT PRIMARY KEY, model_provider TEXT, model TEXT)"); err != nil {
		t.Fatalf("create threads: %v", err)
	}
	if _, err := db.Exec("INSERT INTO threads (id, model_provider, model) VALUES ('t1', ?, ?)", provider, model); err != nil {
		t.Fatalf("insert thread: %v", err)
	}
}

func assertCodexStateRow(t *testing.T, path, wantProvider, wantModel string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open state db: %v", err)
	}
	defer db.Close()
	var gotProvider, gotModel string
	if err := db.QueryRow("SELECT model_provider, model FROM threads WHERE id = 't1'").Scan(&gotProvider, &gotModel); err != nil {
		t.Fatalf("query thread: %v", err)
	}
	if gotProvider != wantProvider || gotModel != wantModel {
		t.Fatalf("%s row = %s/%s, want %s/%s", path, gotProvider, gotModel, wantProvider, wantModel)
	}
}
