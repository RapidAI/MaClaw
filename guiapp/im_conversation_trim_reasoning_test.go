package guiapp

import (
	"strings"
	"testing"
)

func TestTruncateAssistantContentKeepsToolThinking(t *testing.T) {
	reasoning := strings.Repeat("完整思维链。", 80)
	visible := strings.Repeat("可见回答。", 80)
	out := truncateAssistantContent([]interface{}{
		map[string]interface{}{
			"role":              "assistant",
			"content":           "call",
			"reasoning_content": reasoning,
			"tool_calls":        []interface{}{map[string]interface{}{"id": "c1"}},
		},
		map[string]interface{}{"role": "tool", "tool_call_id": "c1", "content": "ok"},
		map[string]interface{}{"role": "assistant", "content": visible, "reasoning_content": reasoning},
	}, 1)
	for i, message := range out {
		got := message.(map[string]interface{})
		if got["role"] == "assistant" && got["reasoning_content"] != reasoning {
			t.Fatalf("message %d reasoning = %q", i, got["reasoning_content"])
		}
	}
	last := out[2].(map[string]interface{})
	if last["content"] == visible {
		t.Fatal("visible content was not shortened")
	}
}

func TestTruncateAssistantContentDropsReasoningWithoutTools(t *testing.T) {
	out := truncateAssistantContent([]interface{}{
		map[string]interface{}{"role": "assistant", "content": "hi", "reasoning_content": "hidden"},
	}, 1)
	if _, ok := out[0].(map[string]interface{})["reasoning_content"]; ok {
		t.Fatal("reasoning kept on a turn without tools")
	}
}
