package configfile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestExternalAgentTypeMatchesEachSource(t *testing.T) {
	if got := ExternalAgentType(ExternalAgentSourceCodex); got != ExternalAgentTypeCodex {
		t.Fatalf("codex = %q", got)
	}
	if got := ExternalAgentType(ExternalAgentSourceClaudeCode); got != ExternalAgentTypeClaudeCode {
		t.Fatalf("claude = %q", got)
	}
	if got := ExternalAgentType(ExternalAgentSourceOpenCode); got != ExternalAgentTypeOpenCode {
		t.Fatalf("opencode = %q", got)
	}
}

func TestToProviderFillsAgentTypeFromSource(t *testing.T) {
	p := ExternalAgentCandidate{Source: ExternalAgentSourceOpenCode, Name: ExternalAgentProviderOpenCode, Model: "big-pickle"}.ToProvider(nil)
	if p.AgentType != ExternalAgentTypeOpenCode {
		t.Fatalf("AgentType = %q", p.AgentType)
	}
	if p.ContextLength != 128000 {
		t.Fatalf("ContextLength = %d", p.ContextLength)
	}
}

func TestParseCodexTomlReadsProviderAndModel(t *testing.T) {
	model, providerID, providers := parseCodexToml(`
model_provider = "glm"
model = "glm-5.3"

[model_providers.myomni]
base_url = "https://example.invalid/v1"

[model_providers.glm]
name = "glm"
base_url = "https://open.bigmodel.cn/api/coding/paas/v4"
wire_api = "responses"
`)
	if model != "glm-5.3" || providerID != "glm" {
		t.Fatalf("model=%q provider=%q", model, providerID)
	}
	if providers["glm"]["base_url"] != "https://open.bigmodel.cn/api/coding/paas/v4" {
		t.Fatalf("glm base_url = %#v", providers["glm"])
	}
	if providers["glm"]["wire_api"] != "responses" {
		t.Fatalf("wire_api = %q", providers["glm"]["wire_api"])
	}
}

func TestScanCodexCandidateFromThirdPartyProvider(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	auth := map[string]string{"OPENAI_API_KEY": "glm-test-key"}
	raw, _ := json.Marshal(auth)
	if err := os.WriteFile(filepath.Join(codexDir, "auth.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	config := `model_provider = "glm" # active
model = "glm-5.3" # current
[model_providers.glm]
base_url = "https://open.bigmodel.cn/api/coding/paas/v4" # coding plan
wire_api = "responses"
`
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	c, ok := scanCodexCandidate()
	if !ok {
		t.Fatal("expected Codex candidate")
	}
	if c.Name != ExternalAgentProviderCodex || c.Source != ExternalAgentSourceCodex {
		t.Fatalf("identity = %+v", c)
	}
	if c.URL != "https://open.bigmodel.cn/api/coding/paas/v4" {
		t.Fatalf("URL = %q", c.URL)
	}
	if c.Key != "glm-test-key" || c.Model != "glm-5.3" || c.WireAPI != "responses" {
		t.Fatalf("fields = %+v", c)
	}
	if c.AgentType != ExternalAgentTypeCodex {
		t.Fatalf("Codex AgentType = %q, want %q", c.AgentType, ExternalAgentTypeCodex)
	}
}

func TestScanCodexCandidatePrefersAPIKeyOverLeftoverOAuth(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	auth := map[string]interface{}{
		"OPENAI_API_KEY": "sk-third-party",
		"tokens":         map[string]string{"access_token": "chatgpt-oauth"},
	}
	raw, _ := json.Marshal(auth)
	if err := os.WriteFile(filepath.Join(codexDir, "auth.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	c, ok := scanCodexCandidate()
	if !ok {
		t.Fatal("expected Codex candidate")
	}
	if c.URL != "https://api.openai.com/v1" {
		t.Fatalf("URL = %q, leftover OAuth must not force ChatGPT subscription", c.URL)
	}
	if c.Key != "sk-third-party" || c.AuthType == "oauth" {
		t.Fatalf("fields = %+v", c)
	}
}

func TestScanCodexCandidateMissingAuthSkipped(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if _, ok := scanCodexCandidate(); ok {
		t.Fatal("empty home must not yield a Codex candidate")
	}
}

func TestScanClaudeCodeCandidate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	settings := map[string]interface{}{
		"env": map[string]string{
			"ANTHROPIC_AUTH_TOKEN":         "claude-key",
			"ANTHROPIC_BASE_URL":           "https://open.bigmodel.cn/api/anthropic",
			"ANTHROPIC_MODEL":              "glm-5.3",
			"ANTHROPIC_DEFAULT_SONNET_MODEL": "glm-5.3",
		},
	}
	raw, _ := json.Marshal(settings)
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	c, ok := scanClaudeCodeCandidate()
	if !ok {
		t.Fatal("expected Claude Code candidate")
	}
	if c.URL != "https://open.bigmodel.cn/api/anthropic" || c.Key != "claude-key" || c.Model != "glm-5.3" {
		t.Fatalf("candidate = %+v", c)
	}
	if c.Protocol != "anthropic" || c.AgentType != ExternalAgentTypeClaudeCode {
		t.Fatalf("protocol/agent = %+v", c)
	}
}

func TestScanOpenCodeZenCandidateKeepsPreferredPaidModel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("OPENCODE_API_KEY", "")
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "xdg"))
	authDir := filepath.Join(home, "xdg", "opencode")
	if err := os.MkdirAll(authDir, 0o755); err != nil {
		t.Fatal(err)
	}
	auth := map[string]interface{}{
		"opencode": map[string]string{"type": "api", "key": "zen-key"},
		"anthropic": map[string]string{"key": "should-not-use"},
	}
	raw, _ := json.Marshal(auth)
	if err := os.WriteFile(filepath.Join(authDir, "auth.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	cfgDir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "opencode.json"), []byte(`{"model":"opencode/gpt-5.5"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	c, ok := scanOpenCodeZenCandidate()
	if !ok {
		t.Fatal("expected OpenCode candidate")
	}
	if c.URL != OpenCodeZenBaseURL || c.Key != "zen-key" {
		t.Fatalf("candidate = %+v", c)
	}
	if c.AgentType != ExternalAgentTypeOpenCode {
		t.Fatalf("OpenCode AgentType = %q, want %q", c.AgentType, ExternalAgentTypeOpenCode)
	}
	if c.Model != "gpt-5.5" {
		t.Fatalf("preferred paid model was discarded: %q", c.Model)
	}
}

func TestScanExternalAgentsSkipsIncomplete(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("OPENCODE_API_KEY", "")
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "xdg-empty"))
	if got := ScanExternalAgents(); len(got) != 0 {
		t.Fatalf("got %#v, want empty", got)
	}
}
