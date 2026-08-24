package agentservice

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func TestCoreAgentRouteTurn_HubManagedPersistsHints(t *testing.T) {
	cb := &coreAgentCallbacks{
		llmCfg: corelib.MaclawLLMConfig{URL: "https://hub.example.com/api/llm/v1", Model: "auto"},
	}
	cfg, d, ok := cb.RouteTurn("你好")
	if !ok || cfg.Model != "auto" || cfg.TaskTypeHint == "" {
		t.Fatalf("hub hints: ok=%v cfg=%+v decision=%+v", ok, cfg, d)
	}
	if cb.GetLLMConfig().TaskTypeHint != cfg.TaskTypeHint {
		t.Fatalf("GetLLMConfig dropped hints: %+v", cb.GetLLMConfig())
	}
	if d.TaskType == string(llm.TaskFast) && cfg.TaskTypeHint != string(llm.TaskFast) {
		t.Fatalf("task hint=%q decision=%q", cfg.TaskTypeHint, d.TaskType)
	}
}

func TestCoreAgentRouteTurn_ThirdPartyHasNoHints(t *testing.T) {
	cb := &coreAgentCallbacks{
		llmCfg: corelib.MaclawLLMConfig{URL: "https://api.openai.com/v1", Model: "gpt-4o"},
	}
	cfg, _, ok := cb.RouteTurn("你好")
	if !ok || cfg.HubManaged || cfg.TaskTypeHint != "" {
		t.Fatalf("third-party must not carry Hub hints: %+v", cfg)
	}
}
