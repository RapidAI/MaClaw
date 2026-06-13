package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/tooldef"
)

var callCounter atomic.Int64

// mockCallbacks implements LoopCallbacks for testing.
type mockCallbacks struct {
	config      corelib.MaclawLLMConfig
	maxIter     int
	sysPrompt   string
	tools       []map[string]interface{}
	toolResult  string
	toolOutcome ToolExecutionOutcome
	allowed     map[string]bool
	callAllowed map[string]bool
	callReason  string
	tokens      []string
	toolCalls   []string
	toolArgs    []string
	toolEvents  []string
	stopped     bool
}

func (m *mockCallbacks) GetLLMConfig() corelib.MaclawLLMConfig      { return m.config }
func (m *mockCallbacks) GetMaxIterations() int                      { return m.maxIter }
func (m *mockCallbacks) BuildSystemPrompt(string, bool) string      { return m.sysPrompt }
func (m *mockCallbacks) BuildTools(string) []map[string]interface{} { return m.tools }
func (m *mockCallbacks) ExecuteTool(name, args string) string {
	m.toolCalls = append(m.toolCalls, name)
	m.toolArgs = append(m.toolArgs, args)
	return m.toolResult
}
func (m *mockCallbacks) ExecuteToolStructured(name, args string) ToolExecutionResult {
	if m.toolOutcome == "" {
		return ToolExecutionResult{Result: m.ExecuteTool(name, args), Outcome: executionOutcomeFromToolOutcome(classifyToolResult(m.toolResult).kind)}
	}
	m.toolCalls = append(m.toolCalls, name)
	m.toolArgs = append(m.toolArgs, args)
	return ToolExecutionResult{Result: m.toolResult, Outcome: m.toolOutcome}
}
func (m *mockCallbacks) IsToolAllowed(name string) bool {
	if m.allowed == nil {
		return true
	}
	return m.allowed[name]
}
func (m *mockCallbacks) IsToolCallAllowed(name, argsJSON string) (bool, string) {
	_ = argsJSON
	if m.callAllowed == nil {
		return true, ""
	}
	if m.callAllowed[name] {
		return true, ""
	}
	return false, m.callReason
}
func (m *mockCallbacks) OnToken(delta string)     { m.tokens = append(m.tokens, delta) }
func (m *mockCallbacks) OnProgress(text string)   {}
func (m *mockCallbacks) OnToolCall(name string)   { m.toolEvents = append(m.toolEvents, name) }
func (m *mockCallbacks) OnToolResult(name string) {}
func (m *mockCallbacks) ShouldStop() bool         { return m.stopped }

type contextProviderCallbacks struct {
	*mockCallbacks
	started  int32
	finished int32
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func (m *contextProviderCallbacks) LLMRequestContext(iteration int) (context.Context, func(error), error) {
	if iteration != 0 {
		return nil, nil, fmt.Errorf("unexpected iteration %d", iteration)
	}
	atomic.AddInt32(&m.started, 1)
	return context.WithValue(context.Background(), "loop-test-context", "ok"), func(error) {
		atomic.AddInt32(&m.finished, 1)
	}, nil
}

func responsesInputHasType(input []interface{}, typ string) bool {
	for _, item := range input {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if m["type"] == typ {
			return true
		}
	}
	return false
}

func TestRunLoop_UsesHostLLMRequestContext(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got := r.Context().Value("loop-test-context"); got != "ok" {
			return nil, fmt.Errorf("request context marker = %#v, want ok", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"role":"assistant","content":"done"}}]}`)),
		}, nil
	})}

	cb := &contextProviderCallbacks{mockCallbacks: &mockCallbacks{
		config:    corelib.MaclawLLMConfig{URL: "https://llm.test", Model: "test", Key: "test-key"},
		maxIter:   1,
		sysPrompt: "sys",
	}}
	result := RunLoop(cb, "hi", nil, client)
	if result.Error != "" || strings.TrimSpace(result.Text) != "done" {
		t.Fatalf("RunLoop result = %+v, want done without error", result)
	}
	if atomic.LoadInt32(&cb.started) != 1 || atomic.LoadInt32(&cb.finished) != 1 {
		t.Fatalf("context lifecycle started=%d finished=%d, want 1/1", cb.started, cb.finished)
	}
}

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

func TestRunLoop_InvalidToolArgumentsAreNotExecutedAndRecover(t *testing.T) {
	var requestCount atomic.Int64
	var sawRecoveryPrompt atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := requestCount.Add(1)
		var req struct {
			Messages []map[string]interface{} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if n == 1 {
			resp := map[string]interface{}{
				"choices": []map[string]interface{}{{
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "",
						"tool_calls": []map[string]interface{}{{
							"id":   "call_bad",
							"type": "function",
							"function": map[string]interface{}{
								"name":      "write_file",
								"arguments": `{"path":"a.txt","content":"unterminated`,
							},
						}},
					},
					"finish_reason": "tool_calls",
				}},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		for _, msg := range req.Messages {
			if msg["role"] == "user" && strings.Contains(fmt.Sprint(msg["content"]), "Previous tool call arguments were incomplete") {
				sawRecoveryPrompt.Store(true)
			}
			if _, ok := msg["tool_call_id"]; ok {
				t.Fatalf("invalid JSON tool call should not create tool-result history: %#v", req.Messages)
			}
		}
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{{
				"message":       map[string]interface{}{"role": "assistant", "content": "recovered"},
				"finish_reason": "stop",
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cb := &mockCallbacks{
		config:     corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:    3,
		sysPrompt:  "You are a coding agent.",
		tools:      []map[string]interface{}{tooldef.BuildToolDef("write_file", "Write file", map[string]interface{}{"type": "object"})},
		toolResult: "should not run",
	}

	result := RunLoop(cb, "write file", nil, nil)
	if result.Error != "" || result.Text != "recovered" {
		t.Fatalf("RunLoop result = %+v, want recovered without error", result)
	}
	if len(cb.toolCalls) != 0 {
		t.Fatalf("tool was executed despite invalid JSON: %v", cb.toolCalls)
	}
	if !sawRecoveryPrompt.Load() {
		t.Fatalf("second request did not receive invalid-JSON recovery prompt")
	}
}

func TestInvalidLoopToolArgumentNamesCatchesUnmarkedBadJSON(t *testing.T) {
	calls := []llm.ToolCall{
		{
			ID:   "call_bad",
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "edit_file", Arguments: `{"new_string":"unterminated`},
		},
		{
			ID:   "call_array",
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "bash", Arguments: `[]`},
		},
		{
			ID:   "call_null",
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "write_file", Arguments: `null`},
		},
		{
			ID:   "call_ok",
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "read_file", Arguments: `{"path":"main.go"}`},
		},
	}
	got := invalidLoopToolArgumentNames(calls)
	if len(got) != 3 || got[0] != "edit_file" || got[1] != "bash" || got[2] != "write_file" {
		t.Fatalf("invalidLoopToolArgumentNames = %#v, want edit_file, bash, write_file", got)
	}
}

func TestRunLoop_TruncatedToolCallInjectsRecoveryWithoutExecuting(t *testing.T) {
	var requestCount atomic.Int64
	var sawRecoveryPrompt atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := requestCount.Add(1)
		var req struct {
			Messages []map[string]interface{} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_bad","type":"function","function":{"name":"write_file","arguments":"{\"path\":\"a.txt\",\"content\":\"unterminated"}}]},"finish_reason":null}]}`+"\n\n")
			fmt.Fprint(w, `data: {"choices":[{"delta":{},"finish_reason":"length"}]}`+"\n\n")
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		for _, msg := range req.Messages {
			if msg["role"] == "user" && strings.Contains(fmt.Sprint(msg["content"]), "Previous tool call arguments were incomplete") && strings.Contains(fmt.Sprint(msg["content"]), "write_file.content <= 1800") {
				sawRecoveryPrompt.Store(true)
			}
			if _, ok := msg["tool_call_id"]; ok {
				t.Fatalf("truncated tool call should not create tool-result history: %#v", req.Messages)
			}
		}
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"recovered after truncation"},"finish_reason":null}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	cb := &mockCallbacks{
		config:     corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:    3,
		sysPrompt:  "You are a coding agent.",
		tools:      []map[string]interface{}{tooldef.BuildToolDef("write_file", "Write file", map[string]interface{}{"type": "object"})},
		toolResult: "should not run",
	}

	result := RunLoop(cb, "write file", nil, server.Client())
	if result.Error != "" || result.Text != "recovered after truncation" {
		t.Fatalf("RunLoop result = %+v, want recovered without error", result)
	}
	if len(cb.toolCalls) != 0 {
		t.Fatalf("tool was executed despite truncation: %v", cb.toolCalls)
	}
	if !sawRecoveryPrompt.Load() {
		t.Fatalf("second request did not receive truncation recovery prompt")
	}
}

func TestRunLoop_EmptyToolArgumentsNormalizeToObject(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{{
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": "",
					"tool_calls": []map[string]interface{}{{
						"id":   "call_empty",
						"type": "function",
						"function": map[string]interface{}{
							"name":      "noop",
							"arguments": "",
						},
					}},
				},
				"finish_reason": "tool_calls",
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cb := &mockCallbacks{
		config:     corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:    1,
		sysPrompt:  "sys",
		tools:      []map[string]interface{}{tooldef.BuildToolDef("noop", "Noop", map[string]interface{}{"type": "object"})},
		toolResult: "ok",
	}

	result := RunLoop(cb, "call noop", nil, nil)
	if result.Error != "max iterations reached" {
		t.Fatalf("RunLoop error = %q, want max iterations after tool execution", result.Error)
	}
	if len(cb.toolCalls) != 1 || cb.toolCalls[0] != "noop" {
		t.Fatalf("tool calls = %#v, want noop executed", cb.toolCalls)
	}
	if len(cb.toolArgs) != 1 || cb.toolArgs[0] != "{}" {
		t.Fatalf("tool args = %#v, want normalized empty args to {}", cb.toolArgs)
	}
}

func TestRunLoop_StripsRolePrefixFromFinalTextAndReasoning(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"role":              "assistant",
						"content":           "Answer kept.\n\nBrowser: duplicated browser instruction",
						"reasoning_content": "thinking kept\nBrowser: hidden browser instruction",
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
		config:    corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:   1,
		sysPrompt: "You are a helpful assistant.",
	}

	result := RunLoop(cb, "test", nil, nil)
	if result.Text != "Answer kept." {
		t.Fatalf("Text = %q, want sanitized answer", result.Text)
	}
	if strings.Contains(result.Text, "Browser:") {
		t.Fatalf("role prefix leaked in final text: %q", result.Text)
	}
}

func TestRunLoop_StripsRolePrefixFromStreamingTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"Answer kept.\n"},"finish_reason":""}]}`+"\n\n")
		_, _ = fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"Browser: duplicated browser instruction"},"finish_reason":"stop"}]}`+"\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	cb := &mockCallbacks{
		config:    corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:   1,
		sysPrompt: "You are a helpful assistant.",
	}

	result := RunLoop(cb, "test", nil, nil)
	if result.Text != "Answer kept." {
		t.Fatalf("Text = %q, want sanitized answer", result.Text)
	}
	streamed := strings.Join(cb.tokens, "")
	if strings.Contains(streamed, "Browser:") {
		t.Fatalf("role prefix leaked in streaming tokens: %q", streamed)
	}
	if strings.TrimSpace(streamed) != "Answer kept." {
		t.Fatalf("streamed = %q, want sanitized answer", streamed)
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

func TestRunLoop_UsesResponsesWireAPI(t *testing.T) {
	var gotPath string
	var gotBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("Decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_test","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}]}`))
	}))
	defer server.Close()

	cb := &mockCallbacks{
		config: corelib.MaclawLLMConfig{
			URL:     server.URL + "/v1",
			Model:   "test",
			Key:     "test-key",
			WireAPI: "responses",
		},
		maxIter:   3,
		sysPrompt: "You are a helpful assistant.",
	}

	result := RunLoop(cb, "hi", nil, server.Client())
	if result.Error != "" || result.Text != "done" {
		t.Fatalf("RunLoop result = %+v, want done without error", result)
	}
	if gotPath != "/v1/responses" {
		t.Fatalf("path = %q, want /v1/responses", gotPath)
	}
	if _, ok := gotBody["input"]; !ok {
		t.Fatalf("request body missing Responses input: %#v", gotBody)
	}
	if _, ok := gotBody["messages"]; ok {
		t.Fatalf("request body leaked chat messages: %#v", gotBody)
	}
	if strings.Join(cb.tokens, "") != "done" {
		t.Fatalf("stream fallback tokens = %#v, want done", cb.tokens)
	}
}

func TestRunLoop_ResponsesWireAPIExecutesTools(t *testing.T) {
	callCount := 0
	var secondBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("path = %q, want /v1/responses", r.URL.Path)
		}
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		if callCount == 1 {
			_, _ = w.Write([]byte(`{"id":"resp_tool","output":[{"type":"function_call","call_id":"call_1","name":"bash","arguments":"{\"command\":\"echo hi\"}"}]}`))
			return
		}
		secondBody = body
		_, _ = w.Write([]byte(`{"id":"resp_done","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"tool done"}]}]}`))
	}))
	defer server.Close()

	cb := &mockCallbacks{
		config: corelib.MaclawLLMConfig{
			URL:     server.URL + "/v1",
			Model:   "test",
			Key:     "test-key",
			WireAPI: "responses",
		},
		maxIter:    5,
		sysPrompt:  "You are a helpful assistant.",
		toolResult: "hi\n",
		tools: []map[string]interface{}{{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "bash",
				"description": "run command",
				"parameters": map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{"command": map[string]interface{}{"type": "string"}},
				},
			},
		}},
	}

	result := RunLoop(cb, "run echo hi", nil, server.Client())
	if result.Error != "" || result.Text != "tool done" {
		t.Fatalf("RunLoop result = %+v, want tool done without error", result)
	}
	if result.ToolCalls != 1 || len(cb.toolCalls) != 1 || cb.toolCalls[0] != "bash" {
		t.Fatalf("tool calls result=%d executed=%v, want one bash", result.ToolCalls, cb.toolCalls)
	}
	input, ok := secondBody["input"].([]interface{})
	if !ok {
		t.Fatalf("second request input = %#v, want array", secondBody["input"])
	}
	if !responsesInputHasType(input, "function_call") || !responsesInputHasType(input, "function_call_output") {
		t.Fatalf("second request missing function_call/function_call_output: %#v", input)
	}
	if _, ok := secondBody["messages"]; ok {
		t.Fatalf("second request leaked chat messages: %#v", secondBody)
	}
}

func TestRunLoop_ToolAuthorizerBlocksExecution(t *testing.T) {
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
										"name":      "task",
										"arguments": `{"action":"run"}`,
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
						"message": map[string]interface{}{
							"role":    "assistant",
							"content": "blocked",
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
		maxIter:   10,
		sysPrompt: "You are a helpful assistant.",
		allowed:   map[string]bool{"ssh": true},
	}

	result := RunLoop(cb, "run a task", nil, nil)

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if len(cb.toolCalls) != 0 {
		t.Fatalf("blocked tool should not reach ExecuteTool, got calls: %v", cb.toolCalls)
	}
	if len(cb.toolEvents) != 0 {
		t.Fatalf("blocked tool should not emit OnToolCall, got events: %v", cb.toolEvents)
	}
	if result.ToolCalls != 1 {
		t.Fatalf("expected blocked tool call to be counted, got %d", result.ToolCalls)
	}
}

func TestExecuteLoopTool_ToolCallAuthorizerBlocksArguments(t *testing.T) {
	cb := &mockCallbacks{
		allowed:     map[string]bool{"bash": true},
		callAllowed: map[string]bool{"bash": false},
		callReason:  "high-risk command must be reviewed",
		toolResult:  "should not run",
	}

	result := executeLoopTool(cb, "bash", `{"command":"rm -rf /"}`)

	if result.Outcome != ToolExecutionOutcomeError {
		t.Fatalf("Outcome = %q, want %q", result.Outcome, ToolExecutionOutcomeError)
	}
	if !strings.Contains(result.Result, "high-risk command") {
		t.Fatalf("unexpected result: %q", result.Result)
	}
	if len(cb.toolCalls) != 0 {
		t.Fatalf("tool executed despite argument-level rejection: %#v", cb.toolCalls)
	}
}

func TestRunLoop_ToolAuthorizerFiltersExposedTools(t *testing.T) {
	var exposed []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Tools []map[string]interface{} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		for _, def := range req.Tools {
			exposed = append(exposed, tooldef.Name(def))
		}
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "ok",
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
		tools: []map[string]interface{}{
			ToolDef("ssh", "remote shell", nil, nil),
			ToolDef("task", "spawn task", nil, nil),
		},
		allowed: map[string]bool{"ssh": true},
	}

	result := RunLoop(cb, "inspect server", nil, nil)

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if len(exposed) != 1 || exposed[0] != "ssh" {
		t.Fatalf("expected only ssh to be exposed, got %v", exposed)
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
	// Should exit after 5 consecutive empty responses (maxConsecutiveEmpty).
	if result.Iterations > 6 {
		t.Fatalf("expected <=6 iterations for hard exit, got %d", result.Iterations)
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

func TestRunLoop_EmptyResponseAfterToolTimeout_Recovers(t *testing.T) {
	// Simulates the exact scenario from the bug: tool returns timeout error,
	// then LLM returns empty response, but the recovery prompt helps it resume.
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var resp map[string]interface{}
		switch {
		case callCount == 1:
			// First call: LLM calls bash with a long-running command.
			resp = map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]interface{}{
							"role":    "assistant",
							"content": "Let me run the command.",
							"tool_calls": []map[string]interface{}{
								{
									"id":   "call_1",
									"type": "function",
									"function": map[string]interface{}{
										"name":      "bash",
										"arguments": `{"command":"sleep 120","timeout":125}`,
									},
								},
							},
						},
						"finish_reason": "tool_calls",
					},
				},
			}
		case callCount == 2:
			// Second call: LLM returns empty response (the bug).
			resp = map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message":       map[string]interface{}{"role": "assistant", "content": ""},
						"finish_reason": "stop",
					},
				},
			}
		case callCount == 3:
			// Third call: after recovery prompt, LLM resumes with a tool call.
			resp = map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]interface{}{
							"role":    "assistant",
							"content": "The command timed out. Let me check the status.",
							"tool_calls": []map[string]interface{}{
								{
									"id":   "call_2",
									"type": "function",
									"function": map[string]interface{}{
										"name":      "bash",
										"arguments": `{"command":"ps aux | grep sleep"}`,
									},
								},
							},
						},
						"finish_reason": "tool_calls",
					},
				},
			}
		default:
			// Final call: LLM provides final answer.
			resp = map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message":       map[string]interface{}{"role": "assistant", "content": "The operation completed successfully."},
						"finish_reason": "stop",
					},
				},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	toolCallCount := 0
	cb := &timeoutToolCallbacks{
		mockCallbacks: &mockCallbacks{
			config: corelib.MaclawLLMConfig{
				URL:   server.URL,
				Model: "test",
				Key:   "test-key",
			},
			maxIter:   10,
			sysPrompt: "You are a helpful assistant.",
		},
		toolCallCount: &toolCallCount,
	}

	result := RunLoop(cb, "run a long command on the server", nil, nil)

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.HardExit {
		t.Fatal("should NOT hard exit — recovery prompt should help LLM resume")
	}
	if !strings.Contains(result.Text, "completed successfully") {
		t.Fatalf("unexpected text: %q", result.Text)
	}
	if result.ToolCalls != 2 {
		t.Fatalf("expected 2 tool calls, got %d", result.ToolCalls)
	}
}

// timeoutToolCallbacks simulates a tool that returns a timeout error on first call.
type timeoutToolCallbacks struct {
	*mockCallbacks
	toolCallCount *int
}

func (c *timeoutToolCallbacks) ExecuteTool(name, args string) string {
	*c.toolCallCount++
	c.mockCallbacks.toolCalls = append(c.mockCallbacks.toolCalls, name)
	if *c.toolCallCount == 1 {
		return "\n[错误] 命令超时（240 秒）"
	}
	return "process running"
}

func TestBuildEmptyResponseRecovery_Timeout(t *testing.T) {
	outcome := classifyToolResult("[错误] 命令超时（240 秒）")
	if outcome.kind != toolOutcomeTimeout {
		t.Fatalf("expected toolOutcomeTimeout, got %d", outcome.kind)
	}

	prompt := buildEmptyResponseRecovery(1, "bash", outcome, "backup docker containers")
	if !strings.Contains(prompt, "超时") {
		t.Fatal("recovery prompt should mention timeout")
	}
	if !strings.Contains(prompt, "bash") {
		t.Fatal("recovery prompt should mention the tool name")
	}
	if !strings.Contains(prompt, "不要放弃") {
		t.Fatal("recovery prompt should encourage continuation")
	}
}

func TestBuildEmptyResponseRecovery_Error(t *testing.T) {
	outcome := classifyToolResult("Error: connection refused")
	if outcome.kind != toolOutcomeError {
		t.Fatalf("expected toolOutcomeError, got %d", outcome.kind)
	}

	prompt := buildEmptyResponseRecovery(1, "ssh", outcome, "deploy to server")
	if !strings.Contains(prompt, "错误") || !strings.Contains(prompt, "ssh") {
		t.Fatal("recovery prompt should mention error and tool name")
	}
}

func TestBuildEmptyResponseRecovery_NoFalsePositiveError(t *testing.T) {
	// Normal output that contains "error" as a substring should NOT trigger
	// the error branch — classifyToolResult checks structured prefixes only.
	outcome := classifyToolResult("cat /var/log/error_log\nsome normal output")
	if outcome.kind != toolOutcomeOK {
		t.Fatalf("expected toolOutcomeOK for normal output, got %d", outcome.kind)
	}

	prompt := buildEmptyResponseRecovery(1, "bash", outcome, "check logs")
	if strings.Contains(prompt, "返回了错误") {
		t.Fatal("should not detect 'error' in normal output as an error condition")
	}
	if !strings.Contains(prompt, "请根据其结果继续") {
		t.Fatal("should use generic continuation prompt for normal output")
	}
}

func TestBuildEmptyResponseRecovery_Escalation(t *testing.T) {
	okOutcome := classifyToolResult("ok")
	emptyOutcome := toolOutcome{kind: toolOutcomeOK}

	// First empty: mild prompt.
	prompt1 := buildEmptyResponseRecovery(1, "", emptyOutcome, "test goal")
	if strings.Contains(prompt1, "警告") {
		t.Fatal("first empty should not contain warning")
	}

	// Third empty: escalated prompt with goal reminder.
	prompt3 := buildEmptyResponseRecovery(3, "bash", okOutcome, "test goal")
	if !strings.Contains(prompt3, "警告") {
		t.Fatal("third empty should contain warning")
	}
	if !strings.Contains(prompt3, "test goal") {
		t.Fatal("third empty should include user goal")
	}
}

func TestTruncateRunesSuffix(t *testing.T) {
	// ASCII: take last 5 chars.
	if got := truncateRunesSuffix("hello world", 5); got != "world" {
		t.Fatalf("expected 'world', got %q", got)
	}
	// Chinese: should not break multi-byte characters.
	if got := truncateRunesSuffix("你好世界测试", 3); got != "界测试" {
		t.Fatalf("expected '界测试', got %q", got)
	}
	// Short string: return as-is.
	if got := truncateRunesSuffix("hi", 10); got != "hi" {
		t.Fatalf("expected 'hi', got %q", got)
	}
}

func TestTruncateRunesPrefix(t *testing.T) {
	// ASCII: take first 5 chars + "...".
	if got := truncateRunesPrefix("hello world", 5); got != "hello..." {
		t.Fatalf("expected 'hello...', got %q", got)
	}
	// Chinese: should not break multi-byte characters.
	if got := truncateRunesPrefix("你好世界测试", 3); got != "你好世..." {
		t.Fatalf("expected '你好世...', got %q", got)
	}
	// Short string: return as-is.
	if got := truncateRunesPrefix("hi", 10); got != "hi" {
		t.Fatalf("expected 'hi', got %q", got)
	}
}

func TestClassifyToolResult(t *testing.T) {
	tests := []struct {
		name   string
		result string
		want   toolOutcomeKind
	}{
		// Timeout cases — our tools produce these exact markers.
		{"bash timeout", "\n[错误] 命令超时（240 秒）", toolOutcomeTimeout},
		{"bash timeout with output", "partial output\n[错误] 命令超时（30 秒）", toolOutcomeTimeout},

		// Error cases — structured prefixes from our tool code.
		{"bash exit code", "\n[错误] 退出码: 1", toolOutcomeError},
		{"bash start failed", "[错误] 命令启动失败: exec: not found", toolOutcomeError},
		{"unknown tool", "未知工具: foobar", toolOutcomeError},
		{"tool panic", "工具执行异常: runtime error", toolOutcomeError},
		{"parse failed", "参数解析失败: unexpected end of JSON", toolOutcomeError},
		{"chinese error prefix", "错误: something went wrong", toolOutcomeError},
		{"english error prefix", "Error: connection refused", toolOutcomeError},
		{"mid-result error", "some output\n[错误] 后台任务启动失败: no space", toolOutcomeError},

		// OK cases — normal output should not be misclassified.
		{"normal output", "hello world", toolOutcomeOK},
		{"output with error substring", "cat /var/log/error_log\nsome data", toolOutcomeOK},
		{"output with Error in middle", "the Error was handled gracefully", toolOutcomeOK},
		{"empty result", "", toolOutcomeOK},
		{"json output", `{"status":"ok","errors":0}`, toolOutcomeOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyToolResult(tt.result)
			if got.kind != tt.want {
				t.Errorf("classifyToolResult(%q) = %d, want %d", tt.result, got.kind, tt.want)
			}
		})
	}
}

func TestExecuteLoopToolUsesStructuredOutcome(t *testing.T) {
	cb := &mockCallbacks{
		toolResult:  "Error: legacy-looking text",
		toolOutcome: ToolExecutionOutcomeOK,
	}
	result := executeLoopTool(cb, "demo", "{}")
	if result.Outcome != ToolExecutionOutcomeOK {
		t.Fatalf("Outcome = %q, want %q", result.Outcome, ToolExecutionOutcomeOK)
	}
	outcome := toolOutcomeFromExecutionResult(result)
	if outcome.kind != toolOutcomeOK {
		t.Fatalf("toolOutcome kind = %d, want %d", outcome.kind, toolOutcomeOK)
	}
}

func TestExecuteLoopToolFallsBackWhenStructuredOutcomeUnset(t *testing.T) {
	cb := &mockCallbacks{
		toolResult: "Error: exit code: 1",
	}
	result := executeLoopTool(cb, "demo", "{}")
	if result.Outcome != ToolExecutionOutcomeError {
		t.Fatalf("Outcome = %q, want %q", result.Outcome, ToolExecutionOutcomeError)
	}
}
