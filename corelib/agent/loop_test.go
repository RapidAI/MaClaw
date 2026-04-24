package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

var callCounter atomic.Int64

// mockCallbacks implements LoopCallbacks for testing.
type mockCallbacks struct {
	config     corelib.MaclawLLMConfig
	maxIter    int
	sysPrompt  string
	tools      []map[string]interface{}
	toolResult string
	tokens     []string
	toolCalls  []string
	stopped    bool
}

func (m *mockCallbacks) GetLLMConfig() corelib.MaclawLLMConfig      { return m.config }
func (m *mockCallbacks) GetMaxIterations() int                      { return m.maxIter }
func (m *mockCallbacks) BuildSystemPrompt(string, bool) string      { return m.sysPrompt }
func (m *mockCallbacks) BuildTools(string) []map[string]interface{} { return m.tools }
func (m *mockCallbacks) ExecuteTool(name, args string) string {
	m.toolCalls = append(m.toolCalls, name)
	return m.toolResult
}
func (m *mockCallbacks) OnToken(delta string)     { m.tokens = append(m.tokens, delta) }
func (m *mockCallbacks) OnProgress(text string)   {}
func (m *mockCallbacks) OnToolCall(name string)   {}
func (m *mockCallbacks) OnToolResult(name string) {}
func (m *mockCallbacks) ShouldStop() bool         { return m.stopped }

func TestRunLoop_NoToolCalls_ReturnsFinalText(t *testing.T) {
	// Mock LLM server that returns a simple text response (no tool calls).
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "Hello! How can I help?",
					},
					"finish_reason": "stop",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cb := &mockCallbacks{
		config: corelib.MaclawLLMConfig{
			URL:   server.URL,
			Model: "test",
			Key:   "test-key",
		},
		maxIter:   10,
		sysPrompt: "You are a helpful assistant.",
	}

	result := RunLoop(cb, "hi", nil, nil)

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.Text != "Hello! How can I help?" {
		t.Fatalf("unexpected text: %q", result.Text)
	}
	if result.Iterations != 1 {
		t.Fatalf("expected 1 iteration, got %d", result.Iterations)
	}
	if len(cb.tokens) != 1 {
		t.Fatalf("OnToken should be called once with full text via streaming, got: %v", cb.tokens)
	}
	if cb.tokens[0] != "Hello! How can I help?" {
		t.Fatalf("OnToken delta mismatch: %q", cb.tokens[0])
	}
}

func TestRunLoop_WithToolCall_ExecutesAndContinues(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var resp map[string]interface{}
		if callCount == 1 {
			// First call: return a tool call.
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
										"name":      "bash",
										"arguments": `{"command":"echo hello"}`,
									},
								},
							},
						},
						"finish_reason": "tool_calls",
					},
				},
			}
		} else {
			// Second call: return final text.
			resp = map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]interface{}{
							"role":    "assistant",
							"content": "Done! The command output: hello",
						},
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
		config: corelib.MaclawLLMConfig{
			URL:   server.URL,
			Model: "test",
			Key:   "test-key",
		},
		maxIter:    10,
		sysPrompt:  "You are a helpful assistant.",
		toolResult: "hello\n",
	}

	result := RunLoop(cb, "run echo hello", nil, nil)

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Text, "Done!") {
		t.Fatalf("unexpected text: %q", result.Text)
	}
	if result.Iterations != 2 {
		t.Fatalf("expected 2 iterations, got %d", result.Iterations)
	}
	if result.ToolCalls != 1 {
		t.Fatalf("expected 1 tool call, got %d", result.ToolCalls)
	}
	if len(cb.toolCalls) != 1 || cb.toolCalls[0] != "bash" {
		t.Fatalf("unexpected tool calls: %v", cb.toolCalls)
	}
}

func TestRunLoop_AskUserReturnsEarly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "",
						"tool_calls": []map[string]interface{}{
							{
								"id":   "call_ask_1",
								"type": "function",
								"function": map[string]interface{}{
									"name":      "ask_user",
									"arguments": `{"question":"Choose one","options":["A","B"],"input_type":"choice"}`,
								},
							},
						},
					},
					"finish_reason": "tool_calls",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cb := &mockCallbacks{
		config:     corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:    10,
		sysPrompt:  "You are a helpful assistant.",
		toolResult: `__ASK_USER__{"question":"Choose one","options":["A","B"],"input_type":"choice"}`,
	}

	result := RunLoop(cb, "need help", nil, nil)
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.AskUser == nil || result.AskUser.Question != "Choose one" {
		t.Fatalf("unexpected ask_user result: %#v", result.AskUser)
	}
	if !strings.Contains(result.Text, "Choose one") {
		t.Fatalf("unexpected text: %q", result.Text)
	}
	if result.ToolCalls != 1 {
		t.Fatalf("expected 1 tool call, got %d", result.ToolCalls)
	}
}

func TestRunLoop_LLMNotConfigured_ReturnsError(t *testing.T) {
	cb := &mockCallbacks{
		config: corelib.MaclawLLMConfig{}, // empty
	}
	result := RunLoop(cb, "hi", nil, nil)
	if result.Error == "" {
		t.Fatal("expected error for unconfigured LLM")
	}
}

func TestRunLoop_Cancelled_ReturnsError(t *testing.T) {
	cb := &mockCallbacks{
		config: corelib.MaclawLLMConfig{
			URL:   "http://localhost:1",
			Model: "test",
			Key:   "test-key",
		},
		maxIter: 10,
		stopped: true, // immediately cancelled
	}
	result := RunLoop(cb, "hi", nil, nil)
	if result.Error != "cancelled" {
		t.Fatalf("expected 'cancelled' error, got %q", result.Error)
	}
}

func TestRunLoop_MaxIterations_ReturnsError(t *testing.T) {
	// Server always returns tool calls, never a final answer.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "",
						"tool_calls": []map[string]interface{}{
							{
								"id":   fmt.Sprintf("call_%d", callCounter.Add(1)),
								"type": "function",
								"function": map[string]interface{}{
									"name":      "bash",
									"arguments": `{"command":"echo loop"}`,
								},
							},
						},
					},
					"finish_reason": "tool_calls",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cb := &mockCallbacks{
		config: corelib.MaclawLLMConfig{
			URL:   server.URL,
			Model: "test",
			Key:   "test-key",
		},
		maxIter:    3,
		sysPrompt:  "test",
		toolResult: "ok",
	}

	result := RunLoop(cb, "loop forever", nil, nil)
	if !strings.Contains(result.Error, "max iterations") {
		t.Fatalf("expected max iterations error, got %q", result.Error)
	}
	if result.Iterations != 3 {
		t.Fatalf("expected 3 iterations, got %d", result.Iterations)
	}
	if result.ToolCalls != 3 {
		t.Fatalf("expected 3 tool calls, got %d", result.ToolCalls)
	}
}

func TestRunLoop_ConsecutiveEmptyResponses_HardExit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always return empty content, no tool calls.
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message":       map[string]interface{}{"role": "assistant", "content": ""},
					"finish_reason": "stop",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cb := &mockCallbacks{
		config: corelib.MaclawLLMConfig{
			URL:   server.URL,
			Model: "test",
			Key:   "test-key",
		},
		maxIter:   10,
		sysPrompt: "test",
	}

	result := RunLoop(cb, "hi", nil, nil)

	if !result.HardExit {
		t.Fatal("expected HardExit=true for consecutive empty responses")
	}
	// Should exit after 3 consecutive empty responses (maxConsecutiveEmpty).
	if result.Iterations > 4 {
		t.Fatalf("expected <=4 iterations for hard exit, got %d", result.Iterations)
	}
}

func TestRunLoop_DriftDetection_SameToolSameResult(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var respData map[string]interface{}
		if callCount <= 8 {
			// Keep returning the same tool call.
			respData = map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]interface{}{
							"role":    "assistant",
							"content": "",
							"tool_calls": []map[string]interface{}{
								{
									"id":   fmt.Sprintf("call_%d", callCount),
									"type": "function",
									"function": map[string]interface{}{
										"name":      "bash",
										"arguments": `{"command":"echo test"}`,
									},
								},
							},
						},
						"finish_reason": "tool_calls",
					},
				},
			}
		} else {
			// After drift injection, return final answer.
			respData = map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message":       map[string]interface{}{"role": "assistant", "content": "I detected a loop and stopped."},
						"finish_reason": "stop",
					},
				},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(respData)
	}))
	defer server.Close()

	cb := &mockCallbacks{
		config: corelib.MaclawLLMConfig{
			URL:   server.URL,
			Model: "test",
			Key:   "test-key",
		},
		maxIter:    20,
		sysPrompt:  "test",
		toolResult: "same output every time", // same result = drift
	}

	result := RunLoop(cb, "do something", nil, nil)

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	// The drift detection should have injected a system message that caused
	// the LLM to stop. The loop should complete before maxIter.
	if result.Iterations >= 20 {
		t.Fatalf("expected drift detection to stop loop early, got %d iterations", result.Iterations)
	}
	if result.ToolCalls < 4 {
		t.Fatalf("expected at least 4 tool calls before drift detection, got %d", result.ToolCalls)
	}
}

func TestRunLoop_NoDrift_WhenResultsChange(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var respData map[string]interface{}
		if callCount <= 5 {
			respData = map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]interface{}{
							"role":    "assistant",
							"content": "",
							"tool_calls": []map[string]interface{}{
								{
									"id":   fmt.Sprintf("call_%d", callCount),
									"type": "function",
									"function": map[string]interface{}{
										"name":      "bash",
										"arguments": `{"command":"check_status"}`,
									},
								},
							},
						},
						"finish_reason": "tool_calls",
					},
				},
			}
		} else {
			respData = map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message":       map[string]interface{}{"role": "assistant", "content": "Task completed."},
						"finish_reason": "stop",
					},
				},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(respData)
	}))
	defer server.Close()

	// Tool results change each time — this is polling, not drift.
	pollCount := 0
	cb := &mockCallbacks{
		config: corelib.MaclawLLMConfig{
			URL:   server.URL,
			Model: "test",
			Key:   "test-key",
		},
		maxIter:   20,
		sysPrompt: "test",
	}
	// Override ExecuteTool to return changing results.
	origExecute := cb.ExecuteTool
	_ = origExecute
	changingCb := &changingResultCallbacks{mockCallbacks: cb}
	changingCb.pollCount = &pollCount

	result := RunLoop(changingCb, "poll status", nil, nil)

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	// Should complete normally without drift detection interfering.
	if result.Text != "Task completed." {
		t.Fatalf("unexpected text: %q", result.Text)
	}
}

// changingResultCallbacks wraps mockCallbacks but returns different results each time.
type changingResultCallbacks struct {
	*mockCallbacks
	pollCount *int
}

func (c *changingResultCallbacks) ExecuteTool(name, args string) string {
	*c.pollCount++
	return fmt.Sprintf("status: running (%d seconds)", *c.pollCount*5)
}
