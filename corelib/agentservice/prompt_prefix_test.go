package agentservice

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestDesktopBotPinsToolsAndSystemAcrossTurns(t *testing.T) {
	agent.ResetPromptPrefixPins()
	t.Cleanup(agent.ResetPromptPrefixPins)
	cb := &coreAgentCallbacks{
		botID:    "bot-1",
		session:  Session{ID: "conv-pin"},
		llmCfg:   corelib.MaclawLLMConfig{ProviderName: "openai", Model: "case"},
		userText: "打开首页",
	}
	first := cb.pinDesktopSystemPrompt("系统规则")
	second := cb.pinDesktopSystemPrompt("系统规则\n中途改过的 AGENTS")
	if first != "系统规则" || second != first {
		t.Fatalf("system pin = %q then %q", first, second)
	}
	if agent.PromptPrefixUnexpectedTotal(agent.PromptSegmentSystem, "openai", "case") != 1 {
		t.Fatal("system change was not counted")
	}

	on := []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "desktop", "description": "mcp on"}},
	}
	off := []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "desktop", "description": "mcp off"}},
	}
	sentOn := cb.PinModelTools(on)
	sentOff := cb.PinModelTools(off)
	if agent.CanonicalToolJSON(sentOn) != agent.CanonicalToolJSON(sentOff) {
		t.Fatalf("tools moved from %s to %s", agent.CanonicalToolJSON(sentOn), agent.CanonicalToolJSON(sentOff))
	}
	if agent.CanonicalToolJSON(sentOff) != agent.CanonicalToolJSON(on) {
		t.Fatalf("sent tools = %s", agent.CanonicalToolJSON(sentOff))
	}
	if agent.PromptPrefixUnexpectedTotal(agent.PromptSegmentTools, "openai", "case") != 1 {
		t.Fatal("tool change was not counted")
	}
}
