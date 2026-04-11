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
// GetAdapter
// ---------------------------------------------------------------------------

func TestGetAdapter_OpenAI(t *testing.T) {
	adapter, err := GetAdapter(ProtocolOpenAI)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := adapter.(*OpenAIAdapter); !ok {
		t.Fatalf("expected *OpenAIAdapter, got %T", adapter)
	}
}

func TestGetAdapter_Unsupported(t *testing.T) {
	_, err := GetAdapter("unknown")
	if err == nil {
		t.Fatal("expected error for unsupported protocol")
	}
	if !strings.Contains(err.Error(), "unsupported protocol") {
		t.Errorf("expected 'unsupported protocol' in error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// OpenAIAdapter.ConvertRequest
// ---------------------------------------------------------------------------

func TestOpenAIAdapter_ConvertRequest_Basic(t *testing.T) {
	adapter := &OpenAIAdapter{}
	req := &OpenAIChatRequest{
		Model: "gpt-4",
		Messages: []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
	}
	provider := &ComputeProvider{
		BaseURL:   "https://api.openai.com/v1",
		APIKey:    "sk-test-key",
		UserAgent: "openclaw",
	}

	httpReq, err := adapter.ConvertRequest(req, provider)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check URL
	if httpReq.URL.String() != "https://api.openai.com/v1/chat/completions" {
		t.Errorf("unexpected URL: %s", httpReq.URL.String())
	}

	// Check method
	if httpReq.Method != http.MethodPost {
		t.Errorf("expected POST, got %s", httpReq.Method)
	}

	// Check headers
	if got := httpReq.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got %q", got)
	}
	if got := httpReq.Header.Get("Authorization"); got != "Bearer sk-test-key" {
		t.Errorf("expected Authorization 'Bearer sk-test-key', got %q", got)
	}
	if got := httpReq.Header.Get("User-Agent"); got != "openclaw" {
		t.Errorf("expected User-Agent 'openclaw', got %q", got)
	}

	// Check body
	body, _ := io.ReadAll(httpReq.Body)
	var parsed OpenAIChatRequest
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	if parsed.Model != "gpt-4" {
		t.Errorf("expected model 'gpt-4', got %q", parsed.Model)
	}
}

func TestOpenAIAdapter_ConvertRequest_TrailingSlash(t *testing.T) {
	adapter := &OpenAIAdapter{}
	req := &OpenAIChatRequest{Model: "gpt-4", Messages: []map[string]interface{}{}}
	provider := &ComputeProvider{
		BaseURL: "https://api.openai.com/v1/",
		APIKey:  "key",
	}

	httpReq, err := adapter.ConvertRequest(req, provider)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if httpReq.URL.String() != "https://api.openai.com/v1/chat/completions" {
		t.Errorf("trailing slash not trimmed: %s", httpReq.URL.String())
	}
}

func TestOpenAIAdapter_ConvertRequest_NoUserAgent(t *testing.T) {
	adapter := &OpenAIAdapter{}
	req := &OpenAIChatRequest{Model: "gpt-4", Messages: []map[string]interface{}{}}
	provider := &ComputeProvider{
		BaseURL:   "https://api.openai.com/v1",
		APIKey:    "key",
		UserAgent: "",
	}

	httpReq, err := adapter.ConvertRequest(req, provider)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// When UserAgent is empty, we don't set it (Go default will be used)
	if got := httpReq.Header.Get("User-Agent"); got != "" {
		t.Errorf("expected empty User-Agent header, got %q", got)
	}
}

func TestOpenAIAdapter_ConvertRequest_NilRequest(t *testing.T) {
	adapter := &OpenAIAdapter{}
	_, err := adapter.ConvertRequest(nil, &ComputeProvider{BaseURL: "https://x.com"})
	if err == nil {
		t.Fatal("expected error for nil request")
	}
}

func TestOpenAIAdapter_ConvertRequest_NilProvider(t *testing.T) {
	adapter := &OpenAIAdapter{}
	_, err := adapter.ConvertRequest(&OpenAIChatRequest{}, nil)
	if err == nil {
		t.Fatal("expected error for nil provider")
	}
}

// ---------------------------------------------------------------------------
// OpenAIAdapter.ConvertResponse — mock HTTP server
// ---------------------------------------------------------------------------

func TestOpenAIAdapter_ConvertResponse_Success(t *testing.T) {
	respBody := OpenAIChatResponse{
		ID:    "chatcmpl-123",
		Model: "gpt-4",
		Choices: []OpenAIChoice{
			{
				Index:        0,
				Message:      OpenAIMessage{Role: "assistant", Content: "Hello!"},
				FinishReason: "stop",
			},
		},
		Usage: &TokenUsage{
			InputTokens:  10,
			OutputTokens: 5,
			TotalTokens:  15,
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(respBody)
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("failed to get response: %v", err)
	}

	adapter := &OpenAIAdapter{}
	result, err := adapter.ConvertResponse(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ID != "chatcmpl-123" {
		t.Errorf("expected ID 'chatcmpl-123', got %q", result.ID)
	}
	if result.Model != "gpt-4" {
		t.Errorf("expected model 'gpt-4', got %q", result.Model)
	}
	if len(result.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(result.Choices))
	}
	if result.Choices[0].Message.Content != "Hello!" {
		t.Errorf("expected content 'Hello!', got %q", result.Choices[0].Message.Content)
	}
	if result.Choices[0].FinishReason != "stop" {
		t.Errorf("expected finish_reason 'stop', got %q", result.Choices[0].FinishReason)
	}
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

func TestOpenAIAdapter_ConvertResponse_NilResponse(t *testing.T) {
	adapter := &OpenAIAdapter{}
	_, err := adapter.ConvertResponse(nil)
	if err == nil {
		t.Fatal("expected error for nil response")
	}
}

func TestOpenAIAdapter_ConvertResponse_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("failed to get response: %v", err)
	}

	adapter := &OpenAIAdapter{}
	_, err = adapter.ConvertResponse(resp)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// ---------------------------------------------------------------------------
// OpenAIAdapter.ExtractUsage
// ---------------------------------------------------------------------------

func TestOpenAIAdapter_ExtractUsage_WithUsage(t *testing.T) {
	adapter := &OpenAIAdapter{}
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
	if usage.InputTokens != 100 {
		t.Errorf("expected 100 input tokens, got %d", usage.InputTokens)
	}
	if usage.OutputTokens != 50 {
		t.Errorf("expected 50 output tokens, got %d", usage.OutputTokens)
	}
	if usage.TotalTokens != 150 {
		t.Errorf("expected 150 total tokens, got %d", usage.TotalTokens)
	}
}

func TestOpenAIAdapter_ExtractUsage_NoUsage(t *testing.T) {
	adapter := &OpenAIAdapter{}
	resp := &OpenAIChatResponse{}

	usage := adapter.ExtractUsage(resp)
	if usage != nil {
		t.Errorf("expected nil usage, got %+v", usage)
	}
}

func TestOpenAIAdapter_ExtractUsage_NilResponse(t *testing.T) {
	adapter := &OpenAIAdapter{}
	usage := adapter.ExtractUsage(nil)
	if usage != nil {
		t.Errorf("expected nil usage for nil response, got %+v", usage)
	}
}

// ---------------------------------------------------------------------------
// End-to-end: ConvertRequest → mock server → ConvertResponse → ExtractUsage
// ---------------------------------------------------------------------------

func TestOpenAIAdapter_EndToEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key-e2e" {
			t.Errorf("expected 'Bearer test-key-e2e', got %q", got)
		}
		if got := r.Header.Get("User-Agent"); got != "claude-code/2.0.0" {
			t.Errorf("expected User-Agent 'claude-code/2.0.0', got %q", got)
		}

		var reqBody OpenAIChatRequest
		json.NewDecoder(r.Body).Decode(&reqBody)
		if reqBody.Model != "gpt-4o" {
			t.Errorf("expected model 'gpt-4o', got %q", reqBody.Model)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(OpenAIChatResponse{
			ID:    "chatcmpl-e2e",
			Model: "gpt-4o-2024-05-13",
			Choices: []OpenAIChoice{
				{
					Index:        0,
					Message:      OpenAIMessage{Role: "assistant", Content: "Hi there!"},
					FinishReason: "stop",
				},
			},
			Usage: &TokenUsage{
				InputTokens:  20,
				OutputTokens: 8,
				TotalTokens:  28,
			},
		})
	}))
	defer srv.Close()

	adapter := &OpenAIAdapter{}
	provider := &ComputeProvider{
		BaseURL:   srv.URL,
		APIKey:    "test-key-e2e",
		UserAgent: "claude-code/2.0.0",
	}
	chatReq := &OpenAIChatRequest{
		Model: "gpt-4o",
		Messages: []map[string]interface{}{
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

	if result.ID != "chatcmpl-e2e" {
		t.Errorf("expected ID 'chatcmpl-e2e', got %q", result.ID)
	}
	if len(result.Choices) != 1 || result.Choices[0].Message.Content != "Hi there!" {
		t.Errorf("unexpected choices: %+v", result.Choices)
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
