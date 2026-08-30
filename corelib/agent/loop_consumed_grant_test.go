package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

// consumedGrantSurfaceCallbacks renders the tool set only on the first model
// request, simulating a semantic host whose one-shot grant is consumed by the
// first successful call (production 2026-08-25: send_file succeeded, the next
// request surface dropped it, the model's retry was denied, and the assistant
// wrongly told the user the PDF could not be delivered).
type consumedGrantSurfaceCallbacks struct {
	mockCallbacks
	renders int
}

func (m *consumedGrantSurfaceCallbacks) BuildTools(string) []map[string]interface{} {
	m.renders++
	if m.renders == 1 {
		return m.tools
	}
	return nil
}

func TestRunLoopConsumedGrantDenialPreservesEarlierSuccess(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var resp map[string]interface{}
		toolCall := map[string]interface{}{
			"message": map[string]interface{}{
				"role":    "assistant",
				"content": "",
				"tool_calls": []map[string]interface{}{
					{
						"id":   "call_1",
						"type": "function",
						"function": map[string]interface{}{
							"name":      "send_file",
							"arguments": `{}`,
						},
					},
				},
			},
			"finish_reason": "tool_calls",
		}
		switch callCount {
		case 1, 2:
			resp = map[string]interface{}{"choices": []map[string]interface{}{toolCall}}
		default:
			resp = map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message":       map[string]interface{}{"role": "assistant", "content": "报告已生成并投递。"},
						"finish_reason": "stop",
					},
				},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cb := &consumedGrantSurfaceCallbacks{mockCallbacks: mockCallbacks{
		config: corelib.MaclawLLMConfig{
			URL:   server.URL,
			Model: "test",
			Key:   "test-key",
		},
		maxIter:    10,
		sysPrompt:  "You are a helpful assistant.",
		toolResult: "Artifact prepared for delivery to the current channel",
		tools: []map[string]interface{}{
			{
				"type": "function",
				"function": map[string]interface{}{
					"name":       "send_file",
					"parameters": map[string]interface{}{"type": "object"},
				},
			},
		},
	}}

	result := RunLoop(cb, "生成并发送报告", nil, nil)
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	// The grant executed exactly once; the second call must be denied before
	// reaching the executor.
	if got := len(cb.toolCalls); got != 1 {
		t.Fatalf("tool executed %d times, want exactly one (the live grant)", got)
	}
	// The denial for the consumed grant must tell the model the earlier success
	// still stands, not a bare "not available" that reads as delivery failure.
	var denial string
	for _, entry := range result.HistoryDelta {
		text, _ := entry.Content.(string)
		if entry.Role == "tool" && strings.Contains(text, "not") {
			denial = text
		}
	}
	if denial == "" {
		t.Fatalf("no denial recorded in history delta: %+v", result.HistoryDelta)
	}
	if !strings.Contains(denial, "already ran successfully") || strings.Contains(denial, "was not available") {
		t.Fatalf("consumed-grant denial must preserve the earlier success, got: %q", denial)
	}
	if !strings.Contains(result.Text, "报告已生成并投递") {
		t.Fatalf("unexpected final text: %q", result.Text)
	}
}

// A name that never succeeded keeps the generic unrendered denial.
func TestRunLoopUnrenderedDenialWithoutPriorSuccessStaysGeneric(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var resp map[string]interface{}
		if callCount == 1 {
			resp = map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]interface{}{
							"role":    "assistant",
							"content": "",
							"tool_calls": []map[string]interface{}{
								{
									"id":   "call_1",
									"type": "function",
									"function": map[string]interface{}{
										"name":      "ghost_tool",
										"arguments": `{}`,
									},
								},
							},
						},
						"finish_reason": "tool_calls",
					},
				},
			}
		} else {
			resp = map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message":       map[string]interface{}{"role": "assistant", "content": "done"},
						"finish_reason": "stop",
					},
				},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cb := &mockCallbacks{
		config:  corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter: 5, sysPrompt: "test",
	}
	result := RunLoop(cb, "call a ghost tool", nil, nil)
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	var denial string
	for _, entry := range result.HistoryDelta {
		text, _ := entry.Content.(string)
		if entry.Role == "tool" {
			denial = text
		}
	}
	if !strings.Contains(denial, "was not available in this request's rendered tool surface") {
		t.Fatalf("never-executed tool must keep the generic denial, got: %q", denial)
	}
	if strings.Contains(denial, "already ran successfully") {
		t.Fatalf("never-executed tool must not claim an earlier success: %q", denial)
	}
}
