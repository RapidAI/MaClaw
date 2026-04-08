package freeproxy

import (
	"encoding/json"
	"testing"
)

func TestToStreamToolCallDeltas_PreservesIDsAndAddsIndexes(t *testing.T) {
	calls := []ToolCall{
		{ID: "call_1", Type: "function", Function: ToolFunction{Name: "search", Arguments: `{"q":"go"}`}},
		{ID: "call_2", Type: "function", Function: ToolFunction{Name: "lookup", Arguments: `{"id":2}`}},
	}
	deltas := toStreamToolCallDeltas(calls)
	if len(deltas) != 2 {
		t.Fatalf("expected 2 deltas, got %d", len(deltas))
	}
	if deltas[0].Index != 0 || deltas[1].Index != 1 {
		t.Fatalf("unexpected indexes: %#v", deltas)
	}
	if deltas[0].ID != "call_1" || deltas[1].ID != "call_2" {
		t.Fatalf("unexpected ids: %#v", deltas)
	}
	if deltas[0].Function.Name != "search" || deltas[1].Function.Name != "lookup" {
		t.Fatalf("unexpected function names: %#v", deltas)
	}
}

func TestStreamChatDelta_JSONIncludesIndexedToolCalls(t *testing.T) {
	chunk := chatResponse{
		ID: "fp-1",
		Object: "chat.completion.chunk",
		Created: 1,
		Model: "m",
		Choices: []chatChoice{{
			Index: 0,
			Delta: streamChatDelta{ToolCalls: toStreamToolCallDeltas([]ToolCall{{
				ID: "call_1", Type: "function", Function: ToolFunction{Name: "search", Arguments: `{"q":"go"}`},
			}})},
		}},
	}
	data, err := json.Marshal(chunk)
	if err != nil {
		t.Fatalf("marshal chunk: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal chunk: %v", err)
	}
	choices := decoded["choices"].([]interface{})
	delta := choices[0].(map[string]interface{})["delta"].(map[string]interface{})
	toolCalls := delta["tool_calls"].([]interface{})
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(toolCalls))
	}
	first := toolCalls[0].(map[string]interface{})
	if first["index"].(float64) != 0 {
		t.Fatalf("expected index 0, got %#v", first["index"])
	}
	if first["id"].(string) != "call_1" {
		t.Fatalf("expected id call_1, got %#v", first["id"])
	}
}
