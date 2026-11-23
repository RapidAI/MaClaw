package guiapp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestSharedUsageDoesNotOverwriteEstimateWithMissingPromptTokens(t *testing.T) {
	var events []AIAssistantStreamEvent
	app := &App{ctx: context.Background()}
	app.testEmit = func(name string, data ...interface{}) {
		if name != "ai-assistant-context-usage" || len(data) == 0 {
			return
		}
		payload, _ := data[0].(string)
		var event AIAssistantStreamEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			t.Errorf("decode context event: %v", err)
			return
		}
		events = append(events, event)
	}
	h := &IMMessageHandler{app: app}
	cb := &sharedAgentLoopCallbacks{
		handler: h,
		userID:  "desktop-user:E:/workprj/MaClawRust",
		llmCfg:  corelib.MaclawLLMConfig{ContextLength: 400000},
	}
	cb.ReportContextWindowUsage(12345, 320000, true)
	cb.OnLLMUsage("test-model", 0, 80)
	if len(events) != 1 {
		t.Fatalf("output-only usage replaced the prompt estimate: %#v", events)
	}
	if events[0].ContextUsedTokens != 12345 || !events[0].ContextUsageEstimated {
		t.Fatalf("estimate changed after output-only usage: %#v", events[0])
	}

	cb.OnLLMUsage("test-model", 15000, 90)
	if len(events) != 2 || events[1].ContextUsedTokens != 15000 || events[1].ContextUsageEstimated {
		t.Fatalf("provider prompt usage was not published: %#v", events)
	}
}
