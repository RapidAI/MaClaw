package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func TestStripTrailingBrokenConversationToolGroupRemovesUnpairedToolCalls(t *testing.T) {
	call := llm.ToolCall{ID: "call_1", Type: "function", Function: llm.ToolCallFunction{Name: "bash", Arguments: `{}`}}
	conversation := []interface{}{
		map[string]interface{}{"role": "user", "content": "do it"},
		map[string]interface{}{"role": "assistant", "content": "", "tool_calls": []llm.ToolCall{call}},
	}

	stripped := stripTrailingBrokenConversationToolGroup(conversation)
	if len(stripped) != 2 {
		t.Fatalf("len = %d, want 2", len(stripped))
	}
	if msgHasToolCalls(stripped[1]) {
		t.Fatalf("assistant still has tool_calls: %#v", stripped[1])
	}
	if msgHasToolCalls(conversation[1]) == false {
		t.Fatal("original conversation was mutated")
	}
}

func TestStripTrailingBrokenConversationToolGroupHandlesStructMessage(t *testing.T) {
	call := llm.ToolCall{ID: "call_struct", Type: "function", Function: llm.ToolCallFunction{Name: "bash", Arguments: `{}`}}
	conversation := []interface{}{
		map[string]interface{}{"role": "user", "content": "do it"},
		llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{call}},
	}

	stripped := stripTrailingBrokenConversationToolGroup(conversation)
	if len(stripped) != 2 {
		t.Fatalf("len = %d, want 2", len(stripped))
	}
	if msgHasToolCalls(stripped[1]) {
		t.Fatalf("assistant still has tool_calls: %#v", stripped[1])
	}
	if !msgHasToolCalls(conversation[1]) {
		t.Fatal("original conversation was mutated")
	}
}

func TestStripTrailingBrokenConversationToolGroupPreservesCompleteGroup(t *testing.T) {
	call := llm.ToolCall{ID: "call_1", Type: "function", Function: llm.ToolCallFunction{Name: "bash", Arguments: `{}`}}
	conversation := []interface{}{
		map[string]interface{}{"role": "user", "content": "do it"},
		map[string]interface{}{"role": "assistant", "content": "", "tool_calls": []llm.ToolCall{call}},
		map[string]interface{}{"role": "tool", "tool_call_id": "call_1", "content": "ok"},
	}

	stripped := stripTrailingBrokenConversationToolGroup(conversation)
	if len(stripped) != len(conversation) {
		t.Fatalf("len = %d, want %d", len(stripped), len(conversation))
	}
	if !msgHasToolCalls(stripped[1]) {
		t.Fatalf("complete assistant tool_calls were stripped: %#v", stripped[1])
	}
}

func TestStripTrailingBrokenConversationToolGroupRemovesPartialToolResults(t *testing.T) {
	call1 := llm.ToolCall{ID: "call_1", Type: "function", Function: llm.ToolCallFunction{Name: "read_file", Arguments: `{}`}}
	call2 := llm.ToolCall{ID: "call_2", Type: "function", Function: llm.ToolCallFunction{Name: "bash", Arguments: `{}`}}
	conversation := []interface{}{
		map[string]interface{}{"role": "user", "content": "do it"},
		map[string]interface{}{"role": "assistant", "content": "", "tool_calls": []llm.ToolCall{call1, call2}},
		map[string]interface{}{"role": "tool", "tool_call_id": "call_1", "content": "partial"},
		map[string]interface{}{"role": "system", "content": "[用户补充] stop that"},
	}

	stripped := stripTrailingBrokenConversationToolGroup(conversation)
	if len(stripped) != 3 {
		t.Fatalf("len = %d, want 3: %#v", len(stripped), stripped)
	}
	if msgHasToolCalls(stripped[1]) {
		t.Fatalf("assistant still has tool_calls: %#v", stripped[1])
	}
	if msgRole(stripped[2]) != "system" {
		t.Fatalf("last role = %q, want system", msgRole(stripped[2]))
	}
}

func TestStripTrailingBrokenToolGroupDoesNotMutateInput(t *testing.T) {
	call := llm.ToolCall{ID: "call_1", Type: "function", Function: llm.ToolCallFunction{Name: "bash", Arguments: `{}`}}
	history := []agent.ConversationEntry{
		{Role: "user", Content: "do it"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{call}},
	}

	stripped := stripTrailingBrokenToolGroup(history)
	if len(stripped) != 2 {
		t.Fatalf("len = %d, want 2", len(stripped))
	}
	if stripped[1].ToolCalls != nil {
		t.Fatalf("stripped assistant still has tool calls: %#v", stripped[1].ToolCalls)
	}
	if history[1].ToolCalls == nil {
		t.Fatal("original history was mutated")
	}
}
