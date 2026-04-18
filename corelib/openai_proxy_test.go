package corelib

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
)

func TestNeedsOpenAIProxy(t *testing.T) {
	tests := []struct {
		name        string
		requiredEnv []string
		extraEnv    map[string]string
		want        bool
	}{
		{
			name:        "required_env contains OPENAI_API_KEY, no extra_env",
			requiredEnv: []string{"OPENAI_API_KEY"},
			extraEnv:    map[string]string{},
			want:        true,
		},
		{
			name:        "required_env contains OPENAI_API_KEY among others, no extra_env",
			requiredEnv: []string{"OTHER_VAR", "OPENAI_API_KEY", "ANOTHER_VAR"},
			extraEnv:    map[string]string{},
			want:        true,
		},
		{
			name:        "required_env does not contain OPENAI_API_KEY",
			requiredEnv: []string{"OTHER_VAR", "SOME_KEY"},
			extraEnv:    map[string]string{},
			want:        false,
		},
		{
			name:        "empty required_env",
			requiredEnv: []string{},
			extraEnv:    map[string]string{},
			want:        false,
		},
		{
			name:        "nil required_env",
			requiredEnv: nil,
			extraEnv:    map[string]string{},
			want:        false,
		},
		{
			name:        "user provides OPENAI_API_KEY in extra_env",
			requiredEnv: []string{"OPENAI_API_KEY"},
			extraEnv:    map[string]string{"OPENAI_API_KEY": "sk-user-key"},
			want:        false,
		},
		{
			name:        "user provides OPENAI_BASE_URL in extra_env",
			requiredEnv: []string{"OPENAI_API_KEY"},
			extraEnv:    map[string]string{"OPENAI_BASE_URL": "https://api.example.com/v1"},
			want:        false,
		},
		{
			name:        "user provides both OPENAI_API_KEY and OPENAI_BASE_URL",
			requiredEnv: []string{"OPENAI_API_KEY"},
			extraEnv:    map[string]string{"OPENAI_API_KEY": "sk-user", "OPENAI_BASE_URL": "https://api.example.com"},
			want:        false,
		},
		{
			name:        "extra_env has OPENAI_API_KEY with empty value",
			requiredEnv: []string{"OPENAI_API_KEY"},
			extraEnv:    map[string]string{"OPENAI_API_KEY": ""},
			want:        true,
		},
		{
			name:        "extra_env has OPENAI_BASE_URL with empty value",
			requiredEnv: []string{"OPENAI_API_KEY"},
			extraEnv:    map[string]string{"OPENAI_BASE_URL": ""},
			want:        true,
		},
		{
			name:        "nil extra_env map",
			requiredEnv: []string{"OPENAI_API_KEY"},
			extraEnv:    nil,
			want:        true,
		},
		{
			name:        "case sensitive - lowercase openai_api_key not matched",
			requiredEnv: []string{"openai_api_key"},
			extraEnv:    map[string]string{},
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NeedsOpenAIProxy(tt.requiredEnv, tt.extraEnv)
			if got != tt.want {
				t.Errorf("NeedsOpenAIProxy(%v, %v) = %v, want %v",
					tt.requiredEnv, tt.extraEnv, got, tt.want)
			}
		})
	}
}

func TestRouteProtocol(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		wireAPI  string
		want     string
	}{
		{"empty config defaults to openai", "", "", "openai"},
		{"protocol openai", "openai", "", "openai"},
		{"protocol OPENAI case insensitive", "OPENAI", "", "openai"},
		{"protocol anthropic", "anthropic", "", "anthropic"},
		{"protocol Anthropic case insensitive", "Anthropic", "", "anthropic"},
		{"protocol ANTHROPIC uppercase", "ANTHROPIC", "", "anthropic"},
		{"wireAPI responses", "", "responses", "responses"},
		{"wireAPI responses-ws", "", "responses-ws", "responses"},
		{"wireAPI Responses with spaces", "", "  Responses  ", "responses"},
		{"wireAPI Responses-WS uppercase", "", "Responses-WS", "responses"},
		{"wireAPI chat defaults to openai", "", "chat", "openai"},
		{"anthropic protocol takes precedence over wireAPI", "anthropic", "responses", "anthropic"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewOpenAIProxy(OpenAIProxyConfig{
				Protocol: tt.protocol,
				WireAPI:  tt.wireAPI,
			})
			got := p.routeProtocol()
			if got != tt.want {
				t.Errorf("routeProtocol() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHandleChatCompletions_PathValidation(t *testing.T) {
	p := NewOpenAIProxy(OpenAIProxyConfig{})
	port, err := p.Start()
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer p.Stop()

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	// Test 404 for wrong path
	resp, err := http.Post(baseURL+"/v1/models", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 404 {
		t.Errorf("wrong path: got status %d, want 404", resp.StatusCode)
	}

	var errBody map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&errBody)
	errObj, _ := errBody["error"].(map[string]interface{})
	if errObj["message"] != "Not Found" {
		t.Errorf("wrong error message: %v", errObj["message"])
	}
	if errObj["type"] != "invalid_request_error" {
		t.Errorf("wrong error type: %v", errObj["type"])
	}
}

func TestHandleChatCompletions_MethodValidation(t *testing.T) {
	p := NewOpenAIProxy(OpenAIProxyConfig{})
	port, err := p.Start()
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer p.Stop()

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	// Test 405 for GET method
	resp, err := http.Get(baseURL + "/v1/chat/completions")
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 405 {
		t.Errorf("wrong method: got status %d, want 405", resp.StatusCode)
	}

	var errBody map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&errBody)
	errObj, _ := errBody["error"].(map[string]interface{})
	if errObj["message"] != "Method Not Allowed" {
		t.Errorf("wrong error message: %v", errObj["message"])
	}
	if errObj["type"] != "invalid_request_error" {
		t.Errorf("wrong error type: %v", errObj["type"])
	}
}

func TestHandleChatCompletions_InvalidJSON(t *testing.T) {
	p := NewOpenAIProxy(OpenAIProxyConfig{})
	port, err := p.Start()
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer p.Stop()

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	// Test 400 for invalid JSON
	resp, err := http.Post(baseURL+"/v1/chat/completions", "application/json", strings.NewReader(`{not valid json`))
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("invalid JSON: got status %d, want 400", resp.StatusCode)
	}

	var errBody map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&errBody)
	errObj, _ := errBody["error"].(map[string]interface{})
	msg, _ := errObj["message"].(string)
	if !strings.HasPrefix(msg, "invalid JSON:") {
		t.Errorf("error message should start with 'invalid JSON:', got %q", msg)
	}
	if errObj["type"] != "invalid_request_error" {
		t.Errorf("wrong error type: %v", errObj["type"])
	}
}

func TestHandleChatCompletions_RoutesToForward(t *testing.T) {
	// Test that valid request routes to forwardOpenAI and returns 502 when upstream is unreachable
	p := NewOpenAIProxy(OpenAIProxyConfig{
		URL:   "http://localhost:9999",
		Model: "test-model",
	})
	port, err := p.Start()
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer p.Stop()

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	body := `{"model": "gpt-4", "messages": [{"role": "user", "content": "hello"}]}`
	resp, err := http.Post(baseURL+"/v1/chat/completions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	defer resp.Body.Close()

	// forwardOpenAI returns error when upstream is unreachable, handler wraps in 502
	if resp.StatusCode != 502 {
		t.Errorf("unreachable upstream: got status %d, want 502", resp.StatusCode)
	}

	var respBody map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&respBody)
	errObj, _ := respBody["error"].(map[string]interface{})
	msg, _ := errObj["message"].(string)
	if !strings.HasPrefix(msg, "upstream provider unreachable:") {
		t.Errorf("error message should start with 'upstream provider unreachable:', got %q", msg)
	}
	if errObj["type"] != "server_error" {
		t.Errorf("wrong error type: %v", errObj["type"])
	}
}

func TestForwardOpenAI_Success(t *testing.T) {
	// Create a mock upstream server
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request method and path
		if r.Method != "POST" {
			t.Errorf("upstream got method %s, want POST", r.Method)
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("upstream got path %s, want /v1/chat/completions", r.URL.Path)
		}

		// Verify headers
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("upstream got Content-Type %q, want application/json", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("Authorization") != "Bearer test-key-123" {
			t.Errorf("upstream got Authorization %q, want 'Bearer test-key-123'", r.Header.Get("Authorization"))
		}

		// Verify body: model should be replaced, stream should be false
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "configured-model" {
			t.Errorf("upstream got model %v, want 'configured-model'", body["model"])
		}
		if body["stream"] != false {
			t.Errorf("upstream got stream %v, want false", body["stream"])
		}
		// messages should be preserved
		msgs, _ := body["messages"].([]interface{})
		if len(msgs) != 1 {
			t.Errorf("upstream got %d messages, want 1", len(msgs))
		}

		// Return a mock response
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":     "chatcmpl-123",
			"object": "chat.completion",
			"model":  "configured-model",
			"choices": []map[string]interface{}{
				{
					"index":         0,
					"message":       map[string]interface{}{"role": "assistant", "content": "Hello!"},
					"finish_reason": "stop",
				},
			},
		})
	})

	mockServer := &http.Server{Handler: upstream}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen error: %v", err)
	}
	go mockServer.Serve(listener)
	defer mockServer.Close()

	mockPort := listener.Addr().(*net.TCPAddr).Port
	mockURL := fmt.Sprintf("http://127.0.0.1:%d", mockPort)

	// Create proxy pointing to mock upstream
	p := NewOpenAIProxy(OpenAIProxyConfig{
		URL:   mockURL,
		Key:   "test-key-123",
		Model: "configured-model",
	})

	// Call forwardOpenAI directly
	body := map[string]interface{}{
		"model":    "gpt-4",
		"messages": []interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		"stream":   true,
	}

	respBody, statusCode, err := p.forwardOpenAI(body)
	if err != nil {
		t.Fatalf("forwardOpenAI error: %v", err)
	}
	if statusCode != 200 {
		t.Errorf("got status %d, want 200", statusCode)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp["id"] != "chatcmpl-123" {
		t.Errorf("got id %v, want chatcmpl-123", resp["id"])
	}
}

func TestForwardOpenAI_UpstreamError(t *testing.T) {
	// Mock upstream that returns 429
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": "Rate limit exceeded",
				"type":    "rate_limit_error",
			},
		})
	})

	mockServer := &http.Server{Handler: upstream}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen error: %v", err)
	}
	go mockServer.Serve(listener)
	defer mockServer.Close()

	mockPort := listener.Addr().(*net.TCPAddr).Port
	mockURL := fmt.Sprintf("http://127.0.0.1:%d", mockPort)

	p := NewOpenAIProxy(OpenAIProxyConfig{
		URL:   mockURL,
		Key:   "test-key",
		Model: "test-model",
	})

	body := map[string]interface{}{
		"model":    "gpt-4",
		"messages": []interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
	}

	respBody, statusCode, err := p.forwardOpenAI(body)
	if err != nil {
		t.Fatalf("forwardOpenAI should not return error for 4xx/5xx, got: %v", err)
	}
	// Should forward 429 status code as-is (Req 3.5)
	if statusCode != 429 {
		t.Errorf("got status %d, want 429", statusCode)
	}

	var resp map[string]interface{}
	json.Unmarshal(respBody, &resp)
	errObj, _ := resp["error"].(map[string]interface{})
	if errObj["message"] != "Rate limit exceeded" {
		t.Errorf("got error message %v, want 'Rate limit exceeded'", errObj["message"])
	}
}

func TestForwardOpenAI_URLConstruction(t *testing.T) {
	// Test that trailing slash in config URL is handled correctly
	tests := []struct {
		name    string
		baseURL string
		wantURL string
	}{
		{"no trailing slash", "https://api.example.com", "https://api.example.com/v1/chat/completions"},
		{"with trailing slash", "https://api.example.com/", "https://api.example.com/v1/chat/completions"},
		{"with path", "https://api.example.com/api", "https://api.example.com/api/v1/chat/completions"},
		{"with path and trailing slash", "https://api.example.com/api/", "https://api.example.com/api/v1/chat/completions"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotURL string
			upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotURL = "http://" + r.Host + r.URL.Path
				w.WriteHeader(200)
				w.Write([]byte(`{"ok":true}`))
			})

			mockServer := &http.Server{Handler: upstream}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatalf("listen error: %v", err)
			}
			go mockServer.Serve(listener)
			defer mockServer.Close()

			mockPort := listener.Addr().(*net.TCPAddr).Port
			// Override the base URL to point to our mock but keep the path structure
			mockBaseURL := fmt.Sprintf("http://127.0.0.1:%d", mockPort)

			p := NewOpenAIProxy(OpenAIProxyConfig{
				URL:   mockBaseURL + "/",
				Key:   "key",
				Model: "model",
			})

			body := map[string]interface{}{"model": "x", "messages": []interface{}{}}
			_, _, err = p.forwardOpenAI(body)
			if err != nil {
				t.Fatalf("forwardOpenAI error: %v", err)
			}

			expectedPath := "/v1/chat/completions"
			if !strings.HasSuffix(gotURL, expectedPath) {
				t.Errorf("got URL %q, want suffix %q", gotURL, expectedPath)
			}
		})
	}
}

func TestOpenaiToResponses(t *testing.T) {
	tests := []struct {
		name     string
		body     map[string]interface{}
		model    string
		wantKeys []string
	}{
		{
			name: "basic conversion with messages",
			body: map[string]interface{}{
				"model": "gpt-4",
				"messages": []interface{}{
					map[string]interface{}{"role": "system", "content": "You are helpful."},
					map[string]interface{}{"role": "user", "content": "Hello"},
				},
				"stream": true,
			},
			model:    "gpt-5.4",
			wantKeys: []string{"model", "input", "stream"},
		},
		{
			name:     "nil messages",
			body:     map[string]interface{}{"model": "gpt-4"},
			model:    "test-model",
			wantKeys: []string{"model", "input", "stream"},
		},
		{
			name: "empty messages array",
			body: map[string]interface{}{
				"model":    "gpt-4",
				"messages": []interface{}{},
			},
			model:    "test-model",
			wantKeys: []string{"model", "input", "stream"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := openaiToResponses(tt.body, tt.model)

			// Verify model is set from parameter
			if result["model"] != tt.model {
				t.Errorf("model = %v, want %v", result["model"], tt.model)
			}

			// Verify stream is false
			if result["stream"] != false {
				t.Errorf("stream = %v, want false", result["stream"])
			}

			// Verify input field exists
			input, ok := result["input"]
			if !ok {
				t.Fatal("input field missing")
			}

			// Verify messages are mapped to input
			if tt.body["messages"] != nil {
				msgs := tt.body["messages"].([]interface{})
				inputArr := input.([]interface{})
				if len(inputArr) != len(msgs) {
					t.Errorf("input length = %d, want %d", len(inputArr), len(msgs))
				}
			} else {
				inputArr := input.([]interface{})
				if len(inputArr) != 0 {
					t.Errorf("input length = %d, want 0 for nil messages", len(inputArr))
				}
			}
		})
	}
}

func TestResponsesToOpenAI(t *testing.T) {
	tests := []struct {
		name        string
		resp        map[string]interface{}
		model       string
		wantContent string
		wantID      string
		wantPrompt  float64
		wantCompl   float64
	}{
		{
			name: "single message with single output_text",
			resp: map[string]interface{}{
				"id": "resp_123",
				"output": []interface{}{
					map[string]interface{}{
						"type": "message",
						"role": "assistant",
						"content": []interface{}{
							map[string]interface{}{
								"type": "output_text",
								"text": "Hello! How can I help?",
							},
						},
					},
				},
				"usage": map[string]interface{}{
					"input_tokens":  float64(25),
					"output_tokens": float64(15),
				},
			},
			model:       "gpt-5.4",
			wantContent: "Hello! How can I help?",
			wantID:      "resp_123",
			wantPrompt:  25,
			wantCompl:   15,
		},
		{
			name: "multiple output_text blocks concatenated",
			resp: map[string]interface{}{
				"id": "resp_456",
				"output": []interface{}{
					map[string]interface{}{
						"type": "message",
						"role": "assistant",
						"content": []interface{}{
							map[string]interface{}{
								"type": "output_text",
								"text": "Part 1. ",
							},
							map[string]interface{}{
								"type": "output_text",
								"text": "Part 2.",
							},
						},
					},
				},
				"usage": map[string]interface{}{
					"input_tokens":  float64(10),
					"output_tokens": float64(20),
				},
			},
			model:       "model-x",
			wantContent: "Part 1. Part 2.",
			wantID:      "resp_456",
			wantPrompt:  10,
			wantCompl:   20,
		},
		{
			name: "multiple message items concatenated (Req 8.4)",
			resp: map[string]interface{}{
				"id": "resp_789",
				"output": []interface{}{
					map[string]interface{}{
						"type": "message",
						"role": "assistant",
						"content": []interface{}{
							map[string]interface{}{
								"type": "output_text",
								"text": "First message. ",
							},
						},
					},
					map[string]interface{}{
						"type": "message",
						"role": "assistant",
						"content": []interface{}{
							map[string]interface{}{
								"type": "output_text",
								"text": "Second message.",
							},
						},
					},
				},
				"usage": map[string]interface{}{
					"input_tokens":  float64(5),
					"output_tokens": float64(30),
				},
			},
			model:       "model-y",
			wantContent: "First message. Second message.",
			wantID:      "resp_789",
			wantPrompt:  5,
			wantCompl:   30,
		},
		{
			name: "empty output array",
			resp: map[string]interface{}{
				"id":     "resp_empty",
				"output": []interface{}{},
			},
			model:       "model-z",
			wantContent: "",
			wantID:      "resp_empty",
			wantPrompt:  0,
			wantCompl:   0,
		},
		{
			name: "nil usage",
			resp: map[string]interface{}{
				"id": "resp_no_usage",
				"output": []interface{}{
					map[string]interface{}{
						"type": "message",
						"content": []interface{}{
							map[string]interface{}{
								"type": "output_text",
								"text": "text",
							},
						},
					},
				},
			},
			model:       "model-a",
			wantContent: "text",
			wantID:      "resp_no_usage",
			wantPrompt:  0,
			wantCompl:   0,
		},
		{
			name:        "empty id defaults to chatcmpl-proxy",
			resp:        map[string]interface{}{},
			model:       "model-b",
			wantContent: "",
			wantID:      "chatcmpl-proxy",
			wantPrompt:  0,
			wantCompl:   0,
		},
		{
			name: "non-message output items are skipped",
			resp: map[string]interface{}{
				"id": "resp_mixed",
				"output": []interface{}{
					map[string]interface{}{
						"type": "function_call",
						"name": "get_weather",
					},
					map[string]interface{}{
						"type": "message",
						"content": []interface{}{
							map[string]interface{}{
								"type": "output_text",
								"text": "Only this.",
							},
						},
					},
				},
			},
			model:       "model-c",
			wantContent: "Only this.",
			wantID:      "resp_mixed",
			wantPrompt:  0,
			wantCompl:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := responsesToOpenAI(tt.resp, tt.model)

			// Verify structural fields
			if result["id"] != tt.wantID {
				t.Errorf("id = %v, want %v", result["id"], tt.wantID)
			}
			if result["object"] != "chat.completion" {
				t.Errorf("object = %v, want chat.completion", result["object"])
			}
			if result["model"] != tt.model {
				t.Errorf("model = %v, want %v", result["model"], tt.model)
			}

			// Verify choices
			choices, ok := result["choices"].([]interface{})
			if !ok || len(choices) != 1 {
				t.Fatalf("choices should have 1 item, got %v", result["choices"])
			}
			choice := choices[0].(map[string]interface{})
			if choice["index"] != 0 {
				t.Errorf("choice index = %v, want 0", choice["index"])
			}
			if choice["finish_reason"] != "stop" {
				t.Errorf("finish_reason = %v, want stop", choice["finish_reason"])
			}
			msg := choice["message"].(map[string]interface{})
			if msg["role"] != "assistant" {
				t.Errorf("message role = %v, want assistant", msg["role"])
			}
			if msg["content"] != tt.wantContent {
				t.Errorf("message content = %q, want %q", msg["content"], tt.wantContent)
			}

			// Verify usage
			usage := result["usage"].(map[string]interface{})
			if usage["prompt_tokens"] != tt.wantPrompt {
				t.Errorf("prompt_tokens = %v, want %v", usage["prompt_tokens"], tt.wantPrompt)
			}
			if usage["completion_tokens"] != tt.wantCompl {
				t.Errorf("completion_tokens = %v, want %v", usage["completion_tokens"], tt.wantCompl)
			}
			expectedTotal := tt.wantPrompt + tt.wantCompl
			if usage["total_tokens"] != expectedTotal {
				t.Errorf("total_tokens = %v, want %v", usage["total_tokens"], expectedTotal)
			}
		})
	}
}

func TestForwardResponses_Success(t *testing.T) {
	// Create a mock upstream Responses API server
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request method and path
		if r.Method != "POST" {
			t.Errorf("upstream got method %s, want POST", r.Method)
		}
		if r.URL.Path != "/v1/responses" {
			t.Errorf("upstream got path %s, want /v1/responses", r.URL.Path)
		}

		// Verify headers
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("upstream got Content-Type %q, want application/json", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("Authorization") != "Bearer resp-key-456" {
			t.Errorf("upstream got Authorization %q, want 'Bearer resp-key-456'", r.Header.Get("Authorization"))
		}

		// Verify body
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "resp-model" {
			t.Errorf("upstream got model %v, want 'resp-model'", body["model"])
		}
		if body["stream"] != false {
			t.Errorf("upstream got stream %v, want false", body["stream"])
		}
		// input should contain the messages
		input, _ := body["input"].([]interface{})
		if len(input) != 1 {
			t.Errorf("upstream got %d input items, want 1", len(input))
		}

		// Return a mock Responses API response
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "resp_abc",
			"output": []interface{}{
				map[string]interface{}{
					"type": "message",
					"role": "assistant",
					"content": []interface{}{
						map[string]interface{}{
							"type": "output_text",
							"text": "Hello from Responses API!",
						},
					},
				},
			},
			"usage": map[string]interface{}{
				"input_tokens":  float64(10),
				"output_tokens": float64(8),
			},
		})
	})

	mockServer := &http.Server{Handler: upstream}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen error: %v", err)
	}
	go mockServer.Serve(listener)
	defer mockServer.Close()

	mockPort := listener.Addr().(*net.TCPAddr).Port
	mockURL := fmt.Sprintf("http://127.0.0.1:%d", mockPort)

	p := NewOpenAIProxy(OpenAIProxyConfig{
		URL:     mockURL,
		Key:     "resp-key-456",
		Model:   "resp-model",
		WireAPI: "responses",
	})

	body := map[string]interface{}{
		"model":    "gpt-4",
		"messages": []interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		"stream":   true,
	}

	respBody, statusCode, err := p.forwardResponses(body)
	if err != nil {
		t.Fatalf("forwardResponses error: %v", err)
	}
	if statusCode != 200 {
		t.Errorf("got status %d, want 200", statusCode)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	// Verify converted OpenAI response
	if resp["id"] != "resp_abc" {
		t.Errorf("id = %v, want resp_abc", resp["id"])
	}
	if resp["object"] != "chat.completion" {
		t.Errorf("object = %v, want chat.completion", resp["object"])
	}
	if resp["model"] != "resp-model" {
		t.Errorf("model = %v, want resp-model", resp["model"])
	}

	choices := resp["choices"].([]interface{})
	choice := choices[0].(map[string]interface{})
	msg := choice["message"].(map[string]interface{})
	if msg["content"] != "Hello from Responses API!" {
		t.Errorf("content = %v, want 'Hello from Responses API!'", msg["content"])
	}

	usage := resp["usage"].(map[string]interface{})
	if usage["prompt_tokens"] != float64(10) {
		t.Errorf("prompt_tokens = %v, want 10", usage["prompt_tokens"])
	}
	if usage["completion_tokens"] != float64(8) {
		t.Errorf("completion_tokens = %v, want 8", usage["completion_tokens"])
	}
	if usage["total_tokens"] != float64(18) {
		t.Errorf("total_tokens = %v, want 18", usage["total_tokens"])
	}
}

func TestForwardResponses_UpstreamError(t *testing.T) {
	// Mock upstream that returns 500
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte(`{"error": "internal server error"}`))
	})

	mockServer := &http.Server{Handler: upstream}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen error: %v", err)
	}
	go mockServer.Serve(listener)
	defer mockServer.Close()

	mockPort := listener.Addr().(*net.TCPAddr).Port
	mockURL := fmt.Sprintf("http://127.0.0.1:%d", mockPort)

	p := NewOpenAIProxy(OpenAIProxyConfig{
		URL:     mockURL,
		Key:     "key",
		Model:   "model",
		WireAPI: "responses",
	})

	body := map[string]interface{}{
		"model":    "gpt-4",
		"messages": []interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
	}

	respBody, statusCode, err := p.forwardResponses(body)
	if err != nil {
		t.Fatalf("forwardResponses should not return error for upstream 5xx, got: %v", err)
	}
	if statusCode != 500 {
		t.Errorf("got status %d, want 500", statusCode)
	}

	var resp map[string]interface{}
	json.Unmarshal(respBody, &resp)
	errObj, _ := resp["error"].(map[string]interface{})
	msg, _ := errObj["message"].(string)
	if !strings.HasPrefix(msg, "upstream error (HTTP 500):") {
		t.Errorf("error message should start with 'upstream error (HTTP 500):', got %q", msg)
	}
	if errObj["type"] != "server_error" {
		t.Errorf("error type = %v, want server_error", errObj["type"])
	}
}

func TestForwardResponses_Unreachable(t *testing.T) {
	p := NewOpenAIProxy(OpenAIProxyConfig{
		URL:     "http://127.0.0.1:1", // port 1 should be unreachable
		Key:     "key",
		Model:   "model",
		WireAPI: "responses",
	})

	body := map[string]interface{}{
		"model":    "gpt-4",
		"messages": []interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
	}

	_, _, err := p.forwardResponses(body)
	if err == nil {
		t.Fatal("forwardResponses should return error when upstream is unreachable")
	}
}

func TestForwardResponses_URLConstruction(t *testing.T) {
	var gotPath string
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":     "resp_url",
			"output": []interface{}{},
		})
	})

	mockServer := &http.Server{Handler: upstream}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen error: %v", err)
	}
	go mockServer.Serve(listener)
	defer mockServer.Close()

	mockPort := listener.Addr().(*net.TCPAddr).Port

	// Test with trailing slash
	p := NewOpenAIProxy(OpenAIProxyConfig{
		URL:     fmt.Sprintf("http://127.0.0.1:%d/", mockPort),
		Key:     "key",
		Model:   "model",
		WireAPI: "responses",
	})

	body := map[string]interface{}{"model": "x", "messages": []interface{}{}}
	_, _, err = p.forwardResponses(body)
	if err != nil {
		t.Fatalf("forwardResponses error: %v", err)
	}

	if gotPath != "/v1/responses" {
		t.Errorf("got path %q, want /v1/responses", gotPath)
	}
}
