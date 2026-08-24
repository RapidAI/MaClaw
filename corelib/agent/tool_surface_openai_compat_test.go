package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

type retryingOpenAICompatCallbacks struct {
	cfg   corelib.MaclawLLMConfig
	tools []map[string]interface{}
}

func (c *retryingOpenAICompatCallbacks) GetLLMConfig() corelib.MaclawLLMConfig { return c.cfg }
func (c *retryingOpenAICompatCallbacks) GetMaxIterations() int                 { return 1 }
func (c *retryingOpenAICompatCallbacks) BuildSystemPrompt(string, bool) string {
	return "You are a test assistant."
}
func (c *retryingOpenAICompatCallbacks) BuildTools(string) []map[string]interface{} {
	return c.tools
}
func (c *retryingOpenAICompatCallbacks) ExecuteTool(string, string) string { return "" }
func (c *retryingOpenAICompatCallbacks) OnToken(string)                    {}
func (c *retryingOpenAICompatCallbacks) OnProgress(string)                 {}
func (c *retryingOpenAICompatCallbacks) OnToolCall(string)                 {}
func (c *retryingOpenAICompatCallbacks) OnToolResult(string)               {}
func (c *retryingOpenAICompatCallbacks) ShouldStop() bool                  { return false }

func hubCompatReceiptToolDefinition() []map[string]interface{} {
	return []map[string]interface{}{{
		"type": "function",
		"function": map[string]interface{}{
			"name":        "web_fetch",
			"description": "Fetch one approved web resource.",
			"parameters": map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"url": map[string]interface{}{"type": "string"},
				},
				"required": []interface{}{"url"},
			},
		},
	}}
}

// The official Hub uses the conservative OpenAI-compatible schema projection.
// Its request surface must be frozen after that projection, not before it.
func TestOpenAICompatReceiptAcceptsProviderReducedToolSchema(t *testing.T) {
	rendered := hubCompatReceiptToolDefinition()
	cfg := corelib.MaclawLLMConfig{URL: "https://hub.mypapers.top/api/llm/v1", Model: "auto"}
	prepared := llm.PrepareOpenAIChatToolsForWire(cfg, rendered)
	params := prepared[0]["function"].(map[string]interface{})["parameters"].(map[string]interface{})
	if _, exists := params["additionalProperties"]; exists {
		t.Fatalf("compat projection retained unsupported schema keyword: %#v", params)
	}

	called := false
	client, err := NewToolSurfaceReceiptHTTPClientWithInvocationPolicy(
		&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			called = true
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(nil))}, nil
		})},
		prepared,
		DefaultToolSurfaceInvocationPolicy(ToolSurfaceEnvelopeOpenAIChat),
		nil,
	)
	if err != nil {
		t.Fatalf("create receipt client: %v", err)
	}
	_, body, err := llm.BuildOpenAIChatRequestData(cfg, []interface{}{map[string]interface{}{"role": "user", "content": "hi"}}, llm.OpenAIChatRequestOptions{
		Stream: true, Tools: prepared, ExplicitToolReplacement: true,
	})
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.test/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("provider-compatible wire request was rejected: %v", err)
	}
	_ = response.Body.Close()
	if !called {
		t.Fatal("transport was not reached")
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode wire body: %v", err)
	}
	if _, exists := payload["tools"]; !exists {
		t.Fatal("wire body omitted tools")
	}
}

func TestRunLoopOpenAICompatReceiptSurvivesStreamFallback(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requests++
		var payload map[string]interface{}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		tools := payload["tools"].([]interface{})
		params := tools[0].(map[string]interface{})["function"].(map[string]interface{})["parameters"].(map[string]interface{})
		if _, exists := params["additionalProperties"]; exists {
			t.Fatalf("request %d retained incompatible schema: %#v", requests, params)
		}
		if requests == 1 {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`not-json`)) // Invalid stream body forces the owning loop to construct a fallback surface.
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	result := RunLoop(&retryingOpenAICompatCallbacks{
		// IsMaclawOfficialHubLLMURL selects the conservative schema adapter.
		// Route the hostname to the local server with a test HTTP client so this
		// covers precisely the deployed Hub compatibility branch.
		cfg:   corelib.MaclawLLMConfig{URL: "https://hub.mypapers.top/api/llm/v1", Model: "auto"},
		tools: hubCompatReceiptToolDefinition(),
	}, "hi", nil, &http.Client{Transport: rewriteTransport{target: srv.URL}})
	if result.Error != "" {
		t.Fatalf("RunLoop error: %s", result.Error)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want streaming attempt plus fallback", requests)
	}
	if result.Text != "ok" {
		t.Fatalf("result text = %q, want ok", result.Text)
	}
}

func TestRunLoopOpenAICompatReceiptSurvivesOuterRetry(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requests++
		var payload map[string]interface{}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		tools := payload["tools"].([]interface{})
		params := tools[0].(map[string]interface{})["function"].(map[string]interface{})["parameters"].(map[string]interface{})
		if _, exists := params["additionalProperties"]; exists {
			t.Fatalf("request %d retained incompatible schema: %#v", requests, params)
		}
		if requests == 1 {
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	result := RunLoop(&retryingOpenAICompatCallbacks{
		cfg:   corelib.MaclawLLMConfig{URL: "https://hub.mypapers.top/api/llm/v1", Model: "auto"},
		tools: hubCompatReceiptToolDefinition(),
	}, "hi", nil, &http.Client{Transport: rewriteTransport{target: srv.URL}})
	if result.Error != "" {
		t.Fatalf("RunLoop error: %s", result.Error)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want initial attempt plus outer retry", requests)
	}
	if result.Text != "ok" {
		t.Fatalf("result text = %q, want ok", result.Text)
	}
}

func TestPrepareToolsForToolSurfaceReceiptKeepsAnthropicDefinitions(t *testing.T) {
	rendered := hubCompatReceiptToolDefinition()
	for _, cfg := range []corelib.MaclawLLMConfig{{URL: "https://hub.mypapers.top/api/llm/v1", Model: "auto", Protocol: "anthropic"}} {
		prepared := prepareToolsForToolSurfaceReceipt(cfg, rendered)
		params := prepared[0]["function"].(map[string]interface{})["parameters"].(map[string]interface{})
		if _, exists := params["additionalProperties"]; !exists {
			t.Fatalf("anthropic envelope was projected as OpenAI chat: %#v", params)
		}
	}
}

func TestAnthropicReceiptAcceptsConvertedToolSchema(t *testing.T) {
	rendered := hubCompatReceiptToolDefinition()
	cfg := corelib.MaclawLLMConfig{URL: "https://hub.mypapers.top/api/llm/v1", Model: "auto", Protocol: "anthropic"}
	prepared := prepareToolsForToolSurfaceReceipt(cfg, rendered)
	called := false
	client, err := NewToolSurfaceReceiptHTTPClientWithInvocationPolicy(
		&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			called = true
			var payload map[string]interface{}
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Fatalf("decode Anthropic wire body: %v", err)
			}
			tools := payload["tools"].([]interface{})
			inputSchema := tools[0].(map[string]interface{})["input_schema"].(map[string]interface{})
			if _, exists := inputSchema["additionalProperties"]; !exists {
				t.Fatalf("Anthropic wire schema was unexpectedly reduced: %#v", inputSchema)
			}
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(nil))}, nil
		})},
		prepared,
		DefaultToolSurfaceInvocationPolicy(ToolSurfaceEnvelopeAnthropic),
		nil,
	)
	if err != nil {
		t.Fatalf("create receipt client: %v", err)
	}
	_, body, err := llm.BuildAnthropicMessagesRequestData(cfg, []interface{}{map[string]interface{}{"role": "user", "content": "hi"}}, llm.AnthropicMessagesRequestOptions{
		Tools: prepared, ExplicitToolReplacement: true,
	})
	if err != nil {
		t.Fatalf("build Anthropic request: %v", err)
	}
	request, err := http.NewRequest(http.MethodPost, "https://example.test/v1/messages", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("converted Anthropic wire request was rejected: %v", err)
	}
	_ = response.Body.Close()
	if !called {
		t.Fatal("transport was not reached")
	}
}

func TestResponsesCompatReceiptAcceptsProviderReducedToolSchema(t *testing.T) {
	rendered := hubCompatReceiptToolDefinition()
	cfg := corelib.MaclawLLMConfig{URL: "https://hub.mypapers.top/api/llm/v1", Model: "auto", WireAPI: "responses"}
	prepared := prepareToolsForToolSurfaceReceipt(cfg, rendered)
	client, err := NewToolSurfaceReceiptHTTPClientWithInvocationPolicy(
		&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(nil))}, nil
		})},
		prepared,
		DefaultToolSurfaceInvocationPolicy(ToolSurfaceEnvelopeResponses),
		nil,
	)
	if err != nil {
		t.Fatalf("create receipt client: %v", err)
	}
	_, body, err := llm.BuildResponsesAPIRequestData(cfg, []interface{}{map[string]interface{}{"role": "user", "content": "hi"}}, llm.ResponsesAPIRequestOptions{
		Tools: prepared, ExplicitToolReplacement: true,
	})
	if err != nil {
		t.Fatalf("build Responses request: %v", err)
	}
	request, err := http.NewRequest(http.MethodPost, "https://example.test/v1/responses", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("provider-compatible Responses wire request was rejected: %v", err)
	}
	_ = response.Body.Close()
}

func TestResponsesReceiptPrefersWireAPIOverAnthropicProtocol(t *testing.T) {
	rendered := hubCompatReceiptToolDefinition()
	// Responses takes precedence in RunLoop dispatch even when an imported
	// provider record retains a stale anthropic Protocol label.
	cfg := corelib.MaclawLLMConfig{URL: "https://hub.mypapers.top/api/llm/v1", Model: "auto", Protocol: "anthropic", WireAPI: "responses"}
	prepared := prepareToolsForToolSurfaceReceipt(cfg, rendered)
	params := prepared[0]["function"].(map[string]interface{})["parameters"].(map[string]interface{})
	if _, exists := params["additionalProperties"]; exists {
		t.Fatalf("Responses wire API retained incompatible schema due to Protocol: %#v", params)
	}

	client, err := NewToolSurfaceReceiptHTTPClientWithInvocationPolicy(
		&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(nil))}, nil
		})},
		prepared,
		DefaultToolSurfaceInvocationPolicy(ToolSurfaceEnvelopeResponses),
		nil,
	)
	if err != nil {
		t.Fatalf("create receipt client: %v", err)
	}
	_, body, err := llm.BuildResponsesAPIRequestData(cfg, []interface{}{map[string]interface{}{"role": "user", "content": "hi"}}, llm.ResponsesAPIRequestOptions{
		Tools: prepared, ExplicitToolReplacement: true,
	})
	if err != nil {
		t.Fatalf("build Responses request: %v", err)
	}
	request, err := http.NewRequest(http.MethodPost, "https://example.test/v1/responses", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("Responses wire request was rejected: %v", err)
	}
	_ = response.Body.Close()
}

func TestRunLoopResponsesCompatReceiptSurvivesStreamFallback(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requests++
		var payload map[string]interface{}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		tools := payload["tools"].([]interface{})
		params := tools[0].(map[string]interface{})["parameters"].(map[string]interface{})
		if _, exists := params["additionalProperties"]; exists {
			t.Fatalf("request %d retained incompatible schema: %#v", requests, params)
		}
		w.Header().Set("Content-Type", "application/json")
		if requests == 1 {
			_, _ = w.Write([]byte(`not-json`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"resp_test","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}]}`))
	}))
	defer srv.Close()

	result := RunLoop(&retryingOpenAICompatCallbacks{
		cfg:   corelib.MaclawLLMConfig{URL: "https://hub.mypapers.top/api/llm/v1", Model: "auto", WireAPI: "responses"},
		tools: hubCompatReceiptToolDefinition(),
	}, "hi", nil, &http.Client{Transport: rewriteTransport{target: srv.URL}})
	if result.Error != "" {
		t.Fatalf("RunLoop error: %s", result.Error)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want streaming attempt plus fallback", requests)
	}
	if result.Text != "ok" {
		t.Fatalf("result text = %q, want ok", result.Text)
	}
}

var _ LoopCallbacks = (*retryingOpenAICompatCallbacks)(nil)

type rewriteTransport struct {
	target string
}

func (t rewriteTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	target, err := http.NewRequestWithContext(request.Context(), request.Method, t.target+request.URL.Path, request.Body)
	if err != nil {
		return nil, err
	}
	target.Header = request.Header.Clone()
	target.ContentLength = request.ContentLength
	return http.DefaultTransport.RoundTrip(target)
}
