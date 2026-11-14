package agentservice

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/tooldef"
)

func TestBotScheduleStaysOnALightDesktopTurn(t *testing.T) {
	cb := &coreAgentCallbacks{
		botID:              "bot_1",
		botScheduleHandler: func(map[string]interface{}) string { return "" },
	}
	tools := cb.finishToolSurface(nil)
	if !botScheduleListed(tools) {
		t.Fatal("desktop bot turn has no bot_schedule")
	}
	if !cb.IsToolAllowedForPromptProfile("bot_schedule", agent.PromptProfileLight) {
		t.Fatal("a light turn hid bot_schedule")
	}
	bare := &coreAgentCallbacks{botID: "bot_1"}
	if botScheduleListed(bare.finishToolSurface(nil)) {
		t.Fatal("a turn without the handler advertised bot_schedule")
	}
	if bare.IsToolAllowedForPromptProfile("bot_schedule", agent.PromptProfileLight) {
		t.Fatal("a turn without the handler allowed bot_schedule")
	}
}

func botScheduleListed(tools []map[string]interface{}) bool {
	for _, tool := range tools {
		if tooldef.Name(tool) == "bot_schedule" {
			return true
		}
	}
	return false
}
