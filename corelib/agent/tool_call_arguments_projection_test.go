package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/tooldef"
)

type toolArgumentProjectingCallbacks struct {
	*mockCallbacks
}

func (c *toolArgumentProjectingCallbacks) ProjectToolCallArguments(conversation []interface{}, history []ConversationEntry, call llm.ToolCall) ([]interface{}, []ConversationEntry) {
	if len(call.Function.Arguments) < 100 {
		return conversation, history
	}
	const summary = `{"_context_compressed":true,"preview":"recoverable"}`
	for _, raw := range conversation {
		msg, ok := raw.(map[string]interface{})
		if !ok || msg["role"] != "assistant" {
			continue
		}
		calls, ok := msg["tool_calls"].([]llm.ToolCall)
		if !ok {
			continue
		}
		calls = append([]llm.ToolCall(nil), calls...)
		for i := range calls {
			if calls[i].ID == call.ID {
				calls[i].Function.Arguments = summary
			}
		}
		msg["tool_calls"] = calls
	}
	for i := range history {
		calls, ok := history[i].ToolCalls.([]llm.ToolCall)
		if !ok {
			continue
		}
		calls = append([]llm.ToolCall(nil), calls...)
		for j := range calls {
			if calls[j].ID == call.ID {
				calls[j].Function.Arguments = summary
			}
		}
		history[i].ToolCalls = calls
	}
	return conversation, history
}

func TestRunLoopProjectsArgumentsWithoutChangingExecutionInput(t *testing.T) {
	original := `{"command":"` + strings.Repeat("x", 5000) + `"}`
	const summary = `{"_context_compressed":true,"preview":"recoverable"}`
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		if requests == 1 {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"choices": []interface{}{map[string]interface{}{
					"message": map[string]interface{}{
						"role": "assistant", "content": "",
						"tool_calls": []interface{}{map[string]interface{}{
							"id": "call-large", "type": "function",
							"function": map[string]interface{}{"name": "bash", "arguments": original},
						}},
					},
					"finish_reason": "tool_calls",
				}},
			})
			return
		}
		var payload struct {
			Messages []map[string]interface{} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			return
		}
		found := false
		for _, msg := range payload.Messages {
			if msg["role"] != "assistant" {
				continue
			}
			calls, _ := msg["tool_calls"].([]interface{})
			for _, raw := range calls {
				call, _ := raw.(map[string]interface{})
				fn, _ := call["function"].(map[string]interface{})
				if call["id"] == "call-large" {
					found = true
					if got := fmt.Sprint(fn["arguments"]); got != summary {
						t.Errorf("next outbound arguments = %q, want compact preview", got)
					}
				}
			}
		}
		if !found {
			t.Error("next outbound request omitted the projected assistant tool call")
		}
		_ = json.NewEncoder(w).Encode(textResponse("done"))
	}))
	defer server.Close()

	cb := &toolArgumentProjectingCallbacks{mockCallbacks: &mockCallbacks{
		config:     corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:    2,
		sysPrompt:  "sys",
		tools:      []map[string]interface{}{tooldef.BuildToolDef("bash", "Run", map[string]interface{}{"type": "object"})},
		toolResult: "ok",
	}}
	result := RunLoop(cb, "run", nil, server.Client())
	if result.Error != "" || result.Text != "done" {
		t.Fatalf("result=%+v", result)
	}
	if len(cb.toolArgs) != 1 || cb.toolArgs[0] != original {
		t.Fatalf("execution input changed: %#v", cb.toolArgs)
	}
	found := false
	for _, entry := range result.HistoryDelta {
		calls, ok := entry.ToolCalls.([]llm.ToolCall)
		if !ok {
			continue
		}
		for _, call := range calls {
			if call.ID == "call-large" {
				found = true
				if call.Function.Arguments != summary {
					t.Fatalf("durable history kept original arguments: %d bytes", len(call.Function.Arguments))
				}
			}
		}
	}
	if !found {
		t.Fatal("history delta omitted the projected tool call")
	}
}
