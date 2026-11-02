package agent

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// openCodeFreeAgentRequest is a chat turn whose upstream body was replaced
// with the official OpenCode agent tools. The model then calls those names
// (shell, webfetch, websearch) even though this loop rendered Maclaw tools.
func openCodeFreeAgentRequest(cfg corelib.MaclawLLMConfig) bool {
	wire := corelib.OpenCodeWireModel(cfg.URL, cfg.Key, cfg.UpstreamModel())
	if !corelib.OpenCodeZenAgentModel(wire) {
		return false
	}
	// Direct OpenCode, Hub, and an OpenCode provider pointed at CodexProxy
	// all return the same rewritten tool calls. Another host that happens
	// to use a -free model id keeps its own tool policy.
	if corelib.IsOpenCodeURL(cfg.URL) || cfg.IsFirstPartyHubLLM() {
		return true
	}
	if strings.Contains(strings.ToLower(cfg.URL), "/api/llm/v1") {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(cfg.ProviderName), "OpenCode") || strings.EqualFold(strings.TrimSpace(cfg.AgentType), "OpenCode")
}

// OpenCodeFreeHostTool reports a Maclaw executor that a free OpenCode turn
// may run even when the semantic planner did not render it. The free client
// replaced the tool list the model saw, so the planner's names never reached it.
func OpenCodeFreeHostTool(cfg corelib.MaclawLLMConfig, name string) bool {
	if !openCodeFreeAgentRequest(cfg) {
		return false
	}
	switch strings.TrimSpace(name) {
	case "bash", "read_file", "write_file", "web_fetch", "web_search":
		return true
	default:
		return false
	}
}

func rewriteOpenCodeAgentToolCalls(calls []llm.ToolCall) []llm.ToolCall {
	return llm.AdaptOpenCodeFreeToolCalls(nil, calls)
}

func rewriteOpenCodeAgentToolCall(name, argsJSON string) (string, string, bool) {
	got := llm.AdaptOpenCodeFreeToolCalls(nil, []llm.ToolCall{{
		Function: llm.ToolCallFunction{Name: name, Arguments: argsJSON},
	}})
	if len(got) != 1 {
		return name, argsJSON, false
	}
	if got[0].Function.Name == name && got[0].Function.Arguments == argsJSON {
		return name, argsJSON, false
	}
	return got[0].Function.Name, got[0].Function.Arguments, true
}
