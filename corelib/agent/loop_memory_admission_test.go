package agent

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
	"github.com/RapidAI/CodeClaw/corelib/tooldef"
)

// A memory admission ends the turn as a typed failure, with the sentence
// both as the text the person reads and as the error the executor records.
func TestRunLoopMemoryAdmissionFailsTheTurn(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{
					"role": "assistant",
					"tool_calls": []map[string]any{{
						"id": "call_1", "type": "function",
						"function": map[string]any{"name": "desktop", "arguments": `{}`},
					}},
				},
				"finish_reason": "tool_calls",
			}},
		})
	}))
	defer server.Close()
	cb := &memoryAdmissionCallbacks{mockCallbacks: &mockCallbacks{
		config:    corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:   3,
		sysPrompt: "test",
		tools: []map[string]any{
			tooldef.BuildToolDef("desktop", "desktop", map[string]any{"type": "object"}),
		},
	}}
	result := RunLoop(cb, "open the report", nil, nil)
	if result.Text != desktop.MemoryBusyOutcome || result.Error != desktop.MemoryBusyOutcome {
		t.Fatalf("text=%q error=%q", result.Text, result.Error)
	}
	if !errors.Is(result.Err, desktop.ErrMemoryAdmission) {
		t.Fatalf("err = %v", result.Err)
	}
	if len(cb.toolCalls) != 1 {
		t.Fatalf("executed %v", cb.toolCalls)
	}
}

// Three calls in one batch: the first admission failure skips the other two.
func TestRunLoopMemoryAdmissionSkipsTheRestOfTheBatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls := make([]map[string]any, 3)
		for i := 0; i < 3; i++ {
			calls[i] = map[string]any{
				"id": "call_" + string(rune('1'+i)), "type": "function",
				"function": map[string]any{"name": "desktop", "arguments": `{}`},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message":       map[string]any{"role": "assistant", "tool_calls": calls},
				"finish_reason": "tool_calls",
			}},
		})
	}))
	defer server.Close()
	cb := &memoryAdmissionCallbacks{mockCallbacks: &mockCallbacks{
		config:    corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:   3,
		sysPrompt: "test",
		tools: []map[string]any{
			tooldef.BuildToolDef("desktop", "desktop", map[string]any{"type": "object"}),
		},
	}}
	result := RunLoop(cb, "open the report", nil, nil)
	if len(cb.toolCalls) != 1 || result.ToolCalls != 3 {
		t.Fatalf("executed %v tool calls counted %d", cb.toolCalls, result.ToolCalls)
	}
	var kinds []string
	for _, entry := range result.HistoryDelta {
		if entry.Role != "tool" {
			continue
		}
		kinds = append(kinds, entry.FailureKind)
	}
	if len(kinds) != 3 || kinds[0] != ToolFailureAdmissionMemory || kinds[1] != ToolFailureAdmissionSkipped || kinds[2] != ToolFailureAdmissionSkipped {
		t.Fatalf("kinds %v", kinds)
	}
}

type memoryAdmissionCallbacks struct {
	*mockCallbacks
}

func (m *memoryAdmissionCallbacks) ExecuteToolCall(name, args, callID string) ToolExecutionResult {
	m.toolCalls = append(m.toolCalls, name)
	return ToolExecutionResult{
		Result:      desktop.MemoryBusyOutcome,
		Outcome:     ToolExecutionOutcomeError,
		FailureKind: ToolFailureAdmissionMemory,
	}
}
