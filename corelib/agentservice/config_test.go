package agentservice

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestResolveLLMConfigPrefersResponsesForGPT5ChatProviders(t *testing.T) {
	cfg := corelib.AppConfig{
		MaclawLLMCurrentProvider: "openai-prod",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:    "openai-prod",
			URL:     "http://localhost:48760/v1",
			Key:     "sk-test",
			Model:   "gpt-5.5",
			WireAPI: "chat_completions",
		}},
	}

	out, err := ResolveLLMConfig(cfg)
	if err != nil {
		t.Fatalf("ResolveLLMConfig: %v", err)
	}
	if out.WireAPI != "responses" {
		t.Fatalf("WireAPI = %q, want responses", out.WireAPI)
	}
}

func TestResolveLLMConfigKeepsExplicitNonGPT5WireAPI(t *testing.T) {
	cfg := corelib.AppConfig{
		MaclawLLMCurrentProvider: "custom",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:    "custom",
			URL:     "http://localhost:48760/v1",
			Key:     "sk-test",
			Model:   "custom-model",
			WireAPI: "chat_completions",
		}},
	}

	out, err := ResolveLLMConfig(cfg)
	if err != nil {
		t.Fatalf("ResolveLLMConfig: %v", err)
	}
	if out.WireAPI != "chat_completions" {
		t.Fatalf("WireAPI = %q, want chat_completions", out.WireAPI)
	}
}

func TestMergeSecretPreservingKeepsRuntimeIntegrationsWhenUpdatingModelOnly(t *testing.T) {
	current := corelib.AppConfig{
		RemoteHubURL:             "https://hub.example.test",
		SkillSourcesAllowed:      []string{"skillhub"},
		MCPServers:               []corelib.MCPServerEntry{{ID: "mcp_redteam", Name: "evaluating-platform-redteam-tools", EndpointURL: "https://platform.internal/redteam-mcp", AuthType: "bearer", AuthSecret: "bridge-secret"}},
		LocalMCPServers:          []corelib.LocalMCPServerEntry{{ID: "local_1", Name: "local helper", Command: "node", Env: map[string]string{"LOCAL_TOKEN": "secret"}}},
		MaclawLLMProviders:       []corelib.MaclawLLMProvider{{Name: "old", URL: "https://old.example/v1", Key: "old-key", Model: "old-model"}},
		MaclawLLMCurrentProvider: "old",
	}
	next := corelib.AppConfig{
		MaclawLLMProviders:       []corelib.MaclawLLMProvider{{Name: "new", URL: "https://new.example/v1", Key: "new-key", Model: "new-model"}},
		MaclawLLMCurrentProvider: "new",
	}

	merged := mergeSecretPreserving(current, next)

	if len(merged.MCPServers) != 1 || merged.MCPServers[0].ID != "mcp_redteam" || merged.MCPServers[0].AuthSecret != "bridge-secret" {
		t.Fatalf("MCP servers not preserved: %#v", merged.MCPServers)
	}
	if len(merged.LocalMCPServers) != 1 || merged.LocalMCPServers[0].ID != "local_1" || merged.LocalMCPServers[0].Env["LOCAL_TOKEN"] != "secret" {
		t.Fatalf("local MCP servers not preserved: %#v", merged.LocalMCPServers)
	}
	if merged.RemoteHubURL != "https://hub.example.test" {
		t.Fatalf("remote hub URL = %q", merged.RemoteHubURL)
	}
	if len(merged.SkillSourcesAllowed) != 1 || merged.SkillSourcesAllowed[0] != "skillhub" {
		t.Fatalf("skill source policy = %#v", merged.SkillSourcesAllowed)
	}
	if merged.MaclawLLMCurrentProvider != "new" || len(merged.MaclawLLMProviders) != 1 || merged.MaclawLLMProviders[0].Name != "new" {
		t.Fatalf("model update was not applied: %#v", merged.MaclawLLMProviders)
	}
}
