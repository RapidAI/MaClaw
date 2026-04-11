package compute

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// GetAdapter — Anthropic
// ---------------------------------------------------------------------------

func TestGetAdapter_Anthropic(t *testing.T) {
	adapter, err := GetAdapter(ProtocolAnthropic)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := adapter.(*AnthropicAdapter); !ok {
		t.Fatalf("expected *AnthropicAdapter, got %T", adapter)
	}
}

// ---------------------------------------------------------------------------
// AnthropicAdapter.ConvertRequest
// ---------------------------------------------------------------------------

func TestAnthropicAdapter_ConvertRequest_Basic(t *testing.T) {
	adapter := &AnthropicAdapter{}
	maxTokens := 512
	req := &OpenAIChatRequest{
		Model: "claude-3-haiku-20240307",
		Messages: []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
		MaxTokens: &maxTokens,
	}
	provider := &ComputeProvider{
		BaseURL:   "https://api.anthropic.com",
		APIKey:    "sk-ant-test",
		UserAgent: "openclaw",
	}

	httpReq, err := adapter.ConvertRequest(req, provider)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// URL should be base_url/messages (not /v1/messages)
	if httpReq.URL.String() != "https://api.anthropic.com/messages" {
		t.Errorf("unexpected URL: %s", httpReq.URL.String())
	}
	if httpReq.Method != http.MethodPost {
		t.Errorf("expected POST, got %s", httpReq.Method)
	}

	// Check required headers
	if got := httpReq.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got %q", got)
	}
	if got := httpReq.Header.Get("anthropic-version"); got != "2023-06-01" {
		t.Errorf("expected anthropic-version '2023-06-01', got %q", got)
	}
	if got := httpReq.Header.Get("x-api-key"); got != "sk-ant-test" {
		t.Errorf("expected x-api-key 'sk-ant-test', got %q", got)
	}
	if got := httpReq.Header.Get("Authorization"); got != "Bearer sk-ant-test" {
		t.Errorf("expected Authorization 'Bearer sk-ant-test', got %q", got)
	}
	if got := httpReq.Header.Get("User-Agent"); got != "openclaw" {
		t.Errorf("expected User-Agent 'openclaw', got %q", got)
	}

	// Check body
	body, _ := io.ReadAll(httpReq.Body)
	var parsed anthropicRequest
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("failed to parse body: %v", err)
	}
	if parsed.Model != "claude-3-haiku-20240307" {
		t.Errorf("expected model 'claude-3-haiku-20240307', got %q", parsed.Model)
	}
	if parsed.MaxTokens != 512 {
		t.Errorf("expected max_tokens 512, got %d", parsed.MaxTokens)
	}
	if parsed.System != "" {
		t.Errorf("expected empty system, got %q", parsed.System)
	}
	if len(parsed.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(parsed.Messages))
	}
}

func TestAnthropicAdapter_ConvertRequest_SystemExtraction(t *testing.T) {
	adapter := &AnthropicAdapter{}
	req := &OpenAIChatRequest{
		Model: "claude-3-haiku-20240307",
		Messages: []map[string]interface{}{
			{"role": "system", "content": "You are helpful"},
			{"role": "user", "content": "Hello"},
		},
	}
	provider := &ComputeProvider{
		BaseURL: "https://api.anthropic.com",
		APIKey:  "sk-ant-test",
	}

	httpReq, err := adapter.ConvertRequest(req, provider)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body, _ := io.ReadAll(httpReq.Body)
	var parsed anthropicRequest
	json.Unmarshal(body, &parsed)

	if parsed.System != "You are helpful" {
		t.Errorf("expected system 'You are helpful', got %q", parsed.System)
	}
	// System messages should be removed from messages array
	if len(parsed.Messages) != 1 {
		t.Fatalf("expected 1 message (system removed), got %d", len(parsed.Messages))
	}
	role, _ := parsed.Messages[0]["role"].(string)
	if role != "user" {
		t.Errorf("expected remaining message role 'user', got %q", role)
	}
}

func TestAnthropicAdapter_ConvertRequest_MultipleSystemMessages(t *testing.T) {
	adapter := &AnthropicAdapter{}
	req := &OpenAIChatRequest{
		Model: "claude-3-haiku-20240307",
		Messages: []map[string]interface{}{
			{"role": "system", "content": "You are helpful"},
			{"role": "system", "content": "Be concise"},
			{"role": "user", "content": "Hello"},
		},
	}
	provider := &ComputeProvider{
		BaseURL: "https://api.anthropic.com",
		APIKey:  "key",
	}

	httpReq, err := adapter.ConvertRequest(req, provider)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body, _ := io.ReadAll(httpReq.Body)
	var parsed anthropicRequest
	json.Unmarshal(body, &parsed)

	if parsed.System != "You are helpful\nBe concise" {
		t.Errorf("expected concatenated system, got %q", parsed.System)
	}
	if len(parsed.Messages) != 1 {
		t.Errorf("expected 1 message, got %d", len(parsed.Messages))
	}
}

func TestAnthropicAdapter_ConvertRequest_NoSystemMessages(t *testing.T) {
	adapter := &AnthropicAdapter{}
	req := &OpenAIChatRequest{
		Model: "claude-3-haiku-20240307",
		Messages: []map[string]interface{}{
			{"role": "user", "content": "Hello"},
			{"role": "assistant", "content": "Hi"},
			{"role": "user", "content": "How are you?"},
		},
	}
	provider := &ComputeProvider{
		BaseURL: "https://api.anthropic.com",
		APIKey:  "key",
	}

	httpReq, err := adapter.ConvertRequest(req, provider)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body, _ := io.ReadAll(httpReq.Body)
	var parsed anthropicRequest
	json.Unmarshal(body, &parsed)

	if parsed.System != "" {
		t.Errorf("expected empty system, got %q", parsed.System)
	}
	if len(parsed.Messages) != 3 {
		t.Errorf("expected 3 messages, got %d", len(parsed.Messages))
	}
}

func TestAnthropicAdapter_ConvertRequest_DefaultMaxTokens(t *testing.T) {
	adapter := &AnthropicAdapter{}
	req := &OpenAIChatRequest{
		Model:    "claude-3-haiku-20240307",
		Messages: []map[string]interface{}{{"role": "user", "content": "Hi"}},
	}
	provider := &ComputeProvider{
		BaseURL: "https://api.anthropic.com",
		APIKey:  "key",
	}

	httpReq, err := adapter.ConvertRequest(req, provider)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body, _ := io.ReadAll(httpReq.Body)
	var parsed anthropicRequest
	json.Unmarshal(body, &parsed)

	if parsed.MaxTokens != 1024 {
		t.Errorf("expected default max_tokens 1024, got %d", parsed.MaxTokens)
	}
}

func TestAnthropicAdapter_ConvertRequest_TrailingSlash(t *testing.T) {
	adapter := &AnthropicAdapter{}
	req := &OpenAIChatRequest{
		Model:    "claude-3-haiku-20240307",
		Messages: []map[string]interface{}{{"role": "user", "content": "Hi"}},
	}
	provider := &ComputeProvider{
		BaseURL: "https://api.anthropic.com/v1/",
		APIKey:  "key",
	}

	httpReq, err := adapter.ConvertRequest(req, provider)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if httpReq.URL.String() != "https://api.anthropic.com/v1/messages" {
		t.Errorf("trailing slash not trimmed: %s", httpReq.URL.String())
	}
}

func TestAnthropicAdapter_ConvertRequest_NilRequest(t *testing.T) {
	adapter := &AnthropicAdapter{}
	_, err := adapter.ConvertRequest(nil, &ComputeProvider{BaseURL: "https://x.com"})
	if err == nil {
		t.Fatal("expected error for nil request")
	}
}

func TestAnthropicAdapter_ConvertRequest_NilProvider(t *testing.T) {
	adapter := &AnthropicAdapter{}
	_, err := adapter.ConvertRequest(&OpenAIChatRequest{}, nil)
	if err == nil {
		t.Fatal("expected error for nil provider")
	}
}

// ---------------------------------------------------------------------------
// AnthropicAdapter.ConvertResponse
// ---------------------------------------------------------------------------

func TestAnthropicAdapter_ConvertResponse_Success(t *testing.T) {
	antResp := anthropicResponse{
		ID:    "msg_xxx",
		Model: "claude-3-haiku-20240307",
		Content: []anthropicContent{
			{Type: "text", Text: "Hello!"},
		},
		StopReason: "end_turn",
		Usage: &anthropicUsage{
			InputTokens:  10,
			OutputTokens: 5,
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(antResp)
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("failed to get response: %v", err)
	}

	adapter := &AnthropicAdapter{}
	result, err := adapter.ConvertResponse(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ID != "msg_xxx" {
		t.Errorf("expected ID 'msg_xxx', got %q", result.ID)
	}
	if result.Object != "chat.completion" {
		t.Errorf("expected object 'chat.completion', got %q", result.Object)
	}
	if result.Model != "claude-3-haiku-20240307" {
		t.Errorf("expected model 'claude-3-haiku-20240307', got %q", result.Model)
	}
	if len(result.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(result.Choices))
	}
	if result.Choices[0].Message.Role != "assistant" {
		t.Errorf("expected role 'assistant', got %q", result.Choices[0].Message.Role)
	}
	if result.Choices[0].Message.Content != "Hello!" {
		t.Errorf("expected content 'Hello!', got %q", result.Choices[0].Message.Content)
	}
	if result.Choices[0].FinishReason != "stop" {
		t.Errorf("expected finish_reason 'stop', got %q", result.Choices[0].FinishReason)
	}

	// Usage
	if result.Usage == nil {
		t.Fatal("expected non-nil usage")
	}
	if result.Usage.InputTokens != 10 {
		t.Errorf("expected 10 input tokens, got %d", result.Usage.InputTokens)
	}
	if result.Usage.OutputTokens != 5 {
		t.Errorf("expected 5 output tokens, got %d", result.Usage.OutputTokens)
	}
	if result.Usage.TotalTokens != 15 {
		t.Errorf("expected 15 total tokens, got %d", result.Usage.TotalTokens)
	}
}

func TestAnthropicAdapter_ConvertResponse_MaxTokensStop(t *testing.T) {
	antResp := anthropicResponse{
		ID:    "msg_yyy",
		Model: "claude-3-haiku-20240307",
		Content: []anthropicContent{
			{Type: "text", Text: "Partial response..."},
		},
		StopReason: "max_tokens",
		Usage: &anthropicUsage{
			InputTokens:  50,
			OutputTokens: 100,
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(antResp)
	}))
	defer srv.Close()

	resp, _ := http.Get(srv.URL)
	adapter := &AnthropicAdapter{}
	result, err := adapter.ConvertResponse(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Choices[0].FinishReason != "length" {
		t.Errorf("expected finish_reason 'length' for max_tokens, got %q", result.Choices[0].FinishReason)
	}
}

func TestAnthropicAdapter_ConvertResponse_NoUsage(t *testing.T) {
	antResp := anthropicResponse{
		ID:    "msg_zzz",
		Model: "claude-3-haiku-20240307",
		Content: []anthropicContent{
			{Type: "text", Text: "Hello"},
		},
		StopReason: "end_turn",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(antResp)
	}))
	defer srv.Close()

	resp, _ := http.Get(srv.URL)
	adapter := &AnthropicAdapter{}
	result, err := adapter.ConvertResponse(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Usage != nil {
		t.Errorf("expected nil usage, got %+v", result.Usage)
	}
}

func TestAnthropicAdapter_ConvertResponse_NilResponse(t *testing.T) {
	adapter := &AnthropicAdapter{}
	_, err := adapter.ConvertResponse(nil)
	if err == nil {
		t.Fatal("expected error for nil response")
	}
}

func TestAnthropicAdapter_ConvertResponse_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	resp, _ := http.Get(srv.URL)
	adapter := &AnthropicAdapter{}
	_, err := adapter.ConvertResponse(resp)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// ---------------------------------------------------------------------------
// AnthropicAdapter.ExtractUsage
// ---------------------------------------------------------------------------

func TestAnthropicAdapter_ExtractUsage_WithUsage(t *testing.T) {
	adapter := &AnthropicAdapter{}
	resp := &OpenAIChatResponse{
		Usage: &TokenUsage{
			InputTokens:  100,
			OutputTokens: 50,
			TotalTokens:  150,
		},
	}

	usage := adapter.ExtractUsage(resp)
	if usage == nil {
		t.Fatal("expected non-nil usage")
	}
	if usage.InputTokens != 100 || usage.OutputTokens != 50 || usage.TotalTokens != 150 {
		t.Errorf("unexpected usage: %+v", usage)
	}
}

func TestAnthropicAdapter_ExtractUsage_NilResponse(t *testing.T) {
	adapter := &AnthropicAdapter{}
	if usage := adapter.ExtractUsage(nil); usage != nil {
		t.Errorf("expected nil usage, got %+v", usage)
	}
}

func TestAnthropicAdapter_ExtractUsage_NoUsage(t *testing.T) {
	adapter := &AnthropicAdapter{}
	resp := &OpenAIChatResponse{}
	if usage := adapter.ExtractUsage(resp); usage != nil {
		t.Errorf("expected nil usage, got %+v", usage)
	}
}

// ---------------------------------------------------------------------------
// End-to-end: ConvertRequest → mock Anthropic server → ConvertResponse
// ---------------------------------------------------------------------------

func TestAnthropicAdapter_EndToEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/messages") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
			t.Errorf("expected anthropic-version '2023-06-01', got %q", got)
		}
		if got := r.Header.Get("x-api-key"); got != "sk-ant-e2e" {
			t.Errorf("expected x-api-key 'sk-ant-e2e', got %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-ant-e2e" {
			t.Errorf("expected Authorization 'Bearer sk-ant-e2e', got %q", got)
		}
		if got := r.Header.Get("User-Agent"); got != "claude-code/2.0.0" {
			t.Errorf("expected User-Agent 'claude-code/2.0.0', got %q", got)
		}

		// Verify body has system extracted
		var reqBody anthropicRequest
		json.NewDecoder(r.Body).Decode(&reqBody)
		if reqBody.System != "You are helpful" {
			t.Errorf("expected system 'You are helpful', got %q", reqBody.System)
		}
		if reqBody.Model != "claude-3-haiku-20240307" {
			t.Errorf("expected model 'claude-3-haiku-20240307', got %q", reqBody.Model)
		}
		if len(reqBody.Messages) != 1 {
			t.Errorf("expected 1 message, got %d", len(reqBody.Messages))
		}

		// Return Anthropic response
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(anthropicResponse{
			ID:    "msg_e2e",
			Model: "claude-3-haiku-20240307",
			Content: []anthropicContent{
				{Type: "text", Text: "Hi there!"},
			},
			StopReason: "end_turn",
			Usage: &anthropicUsage{
				InputTokens:  20,
				OutputTokens: 8,
			},
		})
	}))
	defer srv.Close()

	adapter := &AnthropicAdapter{}
	provider := &ComputeProvider{
		BaseURL:   srv.URL,
		APIKey:    "sk-ant-e2e",
		UserAgent: "claude-code/2.0.0",
	}
	chatReq := &OpenAIChatRequest{
		Model: "claude-3-haiku-20240307",
		Messages: []map[string]interface{}{
			{"role": "system", "content": "You are helpful"},
			{"role": "user", "content": "Hello"},
		},
	}

	// ConvertRequest
	httpReq, err := adapter.ConvertRequest(chatReq, provider)
	if err != nil {
		t.Fatalf("ConvertRequest failed: %v", err)
	}

	// Execute
	client := srv.Client()
	httpResp, err := client.Do(httpReq)
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}

	// ConvertResponse
	result, err := adapter.ConvertResponse(httpResp)
	if err != nil {
		t.Fatalf("ConvertResponse failed: %v", err)
	}

	if result.ID != "msg_e2e" {
		t.Errorf("expected ID 'msg_e2e', got %q", result.ID)
	}
	if len(result.Choices) != 1 || result.Choices[0].Message.Content != "Hi there!" {
		t.Errorf("unexpected choices: %+v", result.Choices)
	}
	if result.Choices[0].FinishReason != "stop" {
		t.Errorf("expected finish_reason 'stop', got %q", result.Choices[0].FinishReason)
	}

	// ExtractUsage
	usage := adapter.ExtractUsage(result)
	if usage == nil {
		t.Fatal("expected non-nil usage")
	}
	if usage.InputTokens != 20 || usage.OutputTokens != 8 || usage.TotalTokens != 28 {
		t.Errorf("unexpected usage: %+v", usage)
	}
}
