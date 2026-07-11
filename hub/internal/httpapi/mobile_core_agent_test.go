package httpapi

import (
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

// initMobileCoreAgentForTest resets prior process state, sets the data root,
// and registers cleanup so SQLite handles are closed before TempDir removal.
// Embedding is disabled by default so handler tests stay fast and offline.
func initMobileCoreAgentForTest(t testing.TB, runtimeDataDir string) {
	t.Helper()
	resetMobileCoreAgentForTest()
	t.Setenv("MACLAW_EMBEDDING_DISABLED", "1")
	InitMobileCoreAgent(runtimeDataDir)
	t.Cleanup(resetMobileCoreAgentForTest)
}

func TestInitMobileCoreAgentSetsDataRoot(t *testing.T) {
	dir := t.TempDir()
	initMobileCoreAgentForTest(t, dir)
	got := mobileCoreAgentDataRoot()
	want := filepath.Join(dir, "mobile-agent")
	if got != want {
		t.Fatalf("mobileCoreAgentDataRoot() = %q, want %q", got, want)
	}
}

func TestMobileMergeUserAgentConfigKeepsMCPForcesHubLLM(t *testing.T) {
	user := corelib.AppConfig{
		MaclawLLMUrl:   "http://user-provider.example/v1",
		MaclawLLMKey:   "user-key",
		MaclawLLMModel: "user-model",
		MCPServers: []corelib.MCPServerEntry{
			{ID: "srv1", Name: "demo", EndpointURL: "http://127.0.0.1:9"},
		},
	}
	hub := mobileCoreAgentAppConfig("system-hint", "https://hub.example.test")
	merged := mobileMergeUserAgentConfig(user, hub)
	if merged.MaclawLLMUrl != hub.MaclawLLMUrl || merged.MaclawLLMKey != hub.MaclawLLMKey || merged.MaclawLLMModel != hub.MaclawLLMModel {
		t.Fatalf("LLM proxy fields not forced onto user config: url=%q key=%q model=%q", merged.MaclawLLMUrl, merged.MaclawLLMKey, merged.MaclawLLMModel)
	}
	if len(merged.MCPServers) != 1 || merged.MCPServers[0].ID != "srv1" {
		t.Fatalf("MCP servers not preserved: %#v", merged.MCPServers)
	}
}
