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
// GetAdapter — Gemini
// ---------------------------------------------------------------------------

func TestGetAdapter_Gemini(t *testing.T) {
	adapter, err := GetAdapter(ProtocolGemini)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := adapter.(*GeminiAdapter); !ok {
		t.Fatalf("expected *GeminiAdapter, got %T", adapter)
	}
}

// ---------------------------------------------------------------------------
// GeminiAdapter.ConvertRequest
// ---------------------------------------------------------------------------

func TestGeminiAdapter_ConvertRequest_Basic(t *testing.T) {
	adapter := &GeminiAdapter{}
	maxTokens := 1024
	req := &OpenAIChatRequest{
		Model: "gemini-pro",
		Messages: []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
		MaxTokens: &maxTokens,
	}
	provider := &ComputeProvider{
		BaseURL:   "https://generativelanguage.googleapis.com/v1beta",
		APIKey:    "AIzaSy-test-key",
		UserAgent: "openclaw",
	}

	httpReq, err := adapter.ConvertRequest(req, provider)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// URL should be base_url/models/{model}:generateContent?key=xxx
	expectedURL := "https://generativelanguage.googleapis.com/v1beta/models/gemini-pro:generateContent?key=AIzaSy-test-key"
	if httpReq.URL.String() != expectedURL {
		t.Errorf("unexpected URL:\n  got:  %s\n  want: %s", httpReq.URL.String(), expectedURL)
	}
	if httpReq.Method != http.MethodPost {
		t.Errorf("expected POST, got %s", httpReq.Method)
	}

	// Check headers
	if got := httpReq.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got %q", got)
	}
	if got := httpReq.Header.Get("User-Agent"); got != "openclaw" {
		t.Errorf("expected User-Agent 'openclaw', got %q", got)
	}
	// API key should NOT be in Authorization header
	if got := httpReq.Header.Get("Authorization"); got != "" {
		t.Errorf("expected no Authorization header, got %q", got)
	}

	// Check body
	body, _ := io.ReadAll(httpReq.Body)
	var parsed geminiRequest
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("failed to parse body: %v", err)
	}
	if parsed.SystemInstruction != nil {
		t.Errorf("expected nil systemInstruction, got %+v", parsed.SystemInstruction)
	}
	if len(parsed.Contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(parsed.Contents))
	}
	if parsed.Contents[0].Role != "user" {
		t.Errorf("expected role 'user', got %q", parsed.Contents[0].Role)
	}
	if parsed.Contents[0].Parts[0].Text != "Hello" {
		t.Errorf("expected text 'Hello', got %q", parsed.Contents[0].Parts[0].Text)
	}
	if parsed.GenerationConfig == nil {
		t.Fatal("expected non-nil generationConfig")
	}
	if parsed.GenerationConfig.MaxOutputTokens == nil || *parsed.GenerationConfig.MaxOutputTokens != 1024 {
		t.Errorf("expected maxOutputTokens 1024, got %v", parsed.GenerationConfig.MaxOutputTokens)
	}
}

func TestGeminiAdapter_ConvertRequest_SystemExtraction(t *testing.T) {
	adapter := &GeminiAdapter{}
	req := &OpenAIChatRequest{
		Model: "gemini-pro",
		Messages: []map[string]interface{}{
			{"role": "system", "content": "You are helpful"},
			{"role": "user", "content": "Hello"},
		},
	}
	provider := &ComputeProvider{
		BaseURL: "https://generativelanguage.googleapis.com/v1beta",
		APIKey:  "test-key",
	}

	httpReq, err := adapter.ConvertRequest(req, provider)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body, _ := io.ReadAll(httpReq.Body)
	var parsed geminiRequest
	json.Unmarshal(body, &parsed)

	if parsed.SystemInstruction == nil {
		t.Fatal("expected non-nil systemInstruction")
	}
	if len(parsed.SystemInstruction.Parts) != 1 {
		t.Fatalf("expected 1 system part, got %d", len(parsed.SystemInstruction.Parts))
	}
	if parsed.SystemInstruction.Parts[0].Text != "You are helpful" {
		t.Errorf("expected system text 'You are helpful', got %q", parsed.SystemInstruction.Parts[0].Text)
	}
	// System messages should be removed from contents
	if len(parsed.Contents) != 1 {
		t.Fatalf("expected 1 content (system removed), got %d", len(parsed.Contents))
	}
	if parsed.Contents[0].Role != "user" {
		t.Errorf("expected role 'user', got %q", parsed.Contents[0].Role)
	}
}

func TestGeminiAdapter_ConvertRequest_MultipleSystemMessages(t *testing.T) {
	adapter := &GeminiAdapter{}
	req := &OpenAIChatRequest{
		Model: "gemini-pro",
		Messages: []map[string]interface{}{
			{"role": "system", "content": "You are helpful"},
			{"role": "system", "content": "Be concise"},
			{"role": "user", "content": "Hello"},
		},
	}
	provider := &ComputeProvider{
		BaseURL: "https://generativelanguage.googleapis.com/v1beta",
		APIKey:  "key",
	}

	httpReq, err := adapter.ConvertRequest(req, provider)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body, _ := io.ReadAll(httpReq.Body)
	var parsed geminiRequest
	json.Unmarshal(body, &parsed)

	if parsed.SystemInstruction == nil {
		t.Fatal("expected non-nil systemInstruction")
	}
	if len(parsed.SystemInstruction.Parts) != 2 {
		t.Fatalf("expected 2 system parts, got %d", len(parsed.SystemInstruction.Parts))
	}
	if parsed.SystemInstruction.Parts[0].Text != "You are helpful" {
		t.Errorf("expected first part 'You are helpful', got %q", parsed.SystemInstruction.Parts[0].Text)
	}
	if parsed.SystemInstruction.Parts[1].Text != "Be concise" {
		t.Errorf("expected second part 'Be concise', got %q", parsed.SystemInstruction.Parts[1].Text)
	}
	if len(parsed.Contents) != 1 {
		t.Errorf("expected 1 content, got %d", len(parsed.Contents))
	}
}

func TestGeminiAdapter_ConvertRequest_AssistantToModel(t *testing.T) {
	adapter := &GeminiAdapter{}
	req := &OpenAIChatRequest{
		Model: "gemini-pro",
		Messages: []map[string]interface{}{
			{"role": "user", "content": "Hello"},
			{"role": "assistant", "content": "Hi there"},
			{"role": "user", "content": "How are you?"},
		},
	}
	provider := &ComputeProvider{
		BaseURL: "https://generativelanguage.googleapis.com/v1beta",
		APIKey:  "key",
	}

	httpReq, err := adapter.ConvertRequest(req, provider)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body, _ := io.ReadAll(httpReq.Body)
	var parsed geminiRequest
	json.Unmarshal(body, &parsed)

	if len(parsed.Contents) != 3 {
		t.Fatalf("expected 3 contents, got %d", len(parsed.Contents))
	}
	// "assistant" should be mapped to "model"
	if parsed.Contents[1].Role != "model" {
		t.Errorf("expected role 'model' for assistant message, got %q", parsed.Contents[1].Role)
	}
	if parsed.Contents[1].Parts[0].Text != "Hi there" {
		t.Errorf("expected text 'Hi there', got %q", parsed.Contents[1].Parts[0].Text)
	}
}

func TestGeminiAdapter_ConvertRequest_APIKeyAsQueryParam(t *testing.T) {
	adapter := &GeminiAdapter{}
	req := &OpenAIChatRequest{
		Model:    "gemini-pro",
		Messages: []map[string]interface{}{{"role": "user", "content": "Hi"}},
	}
	provider := &ComputeProvider{
		BaseURL: "https://generativelanguage.googleapis.com/v1beta",
		APIKey:  "my-secret-key",
	}

	httpReq, err := adapter.ConvertRequest(req, provider)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// API key should be in query parameter
	if got := httpReq.URL.Query().Get("key"); got != "my-secret-key" {
		t.Errorf("expected query param key='my-secret-key', got %q", got)
	}
	// Should NOT be in headers
	if got := httpReq.Header.Get("Authorization"); got != "" {
		t.Errorf("expected no Authorization header, got %q", got)
	}
}

func TestGeminiAdapter_ConvertRequest_TrailingSlash(t *testing.T) {
	adapter := &GeminiAdapter{}
	req := &OpenAIChatRequest{
		Model:    "gemini-pro",
		Messages: []map[string]interface{}{{"role": "user", "content": "Hi"}},
	}
	provider := &ComputeProvider{
		BaseURL: "https://generativelanguage.googleapis.com/v1beta/",
		APIKey:  "key",
	}

	httpReq, err := adapter.ConvertRequest(req, provider)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(httpReq.URL.Path, "/models/gemini-pro:generateContent") {
		t.Errorf("trailing slash not trimmed properly: %s", httpReq.URL.String())
	}
}

func TestGeminiAdapter_ConvertRequest_NilRequest(t *testing.T) {
	adapter := &GeminiAdapter{}
	_, err := adapter.ConvertRequest(nil, &ComputeProvider{BaseURL: "https://x.com"})
	if err == nil {
		t.Fatal("expected error for nil request")
	}
}

func TestGeminiAdapter_ConvertRequest_NilProvider(t *testing.T) {
	adapter := &GeminiAdapter{}
	_, err := adapter.ConvertRequest(&OpenAIChatRequest{}, nil)
	if err == nil {
		t.Fatal("expected error for nil provider")
	}
}

func TestGeminiAdapter_ConvertRequest_FallbackModel(t *testing.T) {
	adapter := &GeminiAdapter{}
	req := &OpenAIChatRequest{
		Model:    "", // empty model in request
		Messages: []map[string]interface{}{{"role": "user", "content": "Hi"}},
	}
	provider := &ComputeProvider{
		BaseURL: "https://generativelanguage.googleapis.com/v1beta",
		APIKey:  "key",
		Model:   "gemini-1.5-flash",
	}

	httpReq, err := adapter.ConvertRequest(req, provider)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(httpReq.URL.Path, "/models/gemini-1.5-flash:generateContent") {
		t.Errorf("expected fallback to provider model, got URL: %s", httpReq.URL.String())
	}
}

// ---------------------------------------------------------------------------
// GeminiAdapter.ConvertResponse
// ---------------------------------------------------------------------------

func TestGeminiAdapter_ConvertResponse_Success(t *testing.T) {
	gemResp := geminiResponse{
		Candidates: []geminiCandidate{
			{
				Content: geminiContent{
					Role:  "model",
					Parts: []geminiPart{{Text: "Hello!"}},
				},
				FinishReason: "STOP",
			},
		},
		UsageMetadata: &geminiUsageMeta{
			PromptTokenCount:     10,
			CandidatesTokenCount: 5,
			TotalTokenCount:      15,
		},
		ModelVersion: "gemini-pro-001",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(gemResp)
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("failed to get response: %v", err)
	}

	adapter := &GeminiAdapter{}
	result, err := adapter.ConvertResponse(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Object != "chat.completion" {
		t.Errorf("expected object 'chat.completion', got %q", result.Object)
	}
	if result.Model != "gemini-pro-001" {
		t.Errorf("expected model 'gemini-pro-001', got %q", result.Model)
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

func TestGeminiAdapter_ConvertResponse_MaxTokensStop(t *testing.T) {
	gemResp := geminiResponse{
		Candidates: []geminiCandidate{
			{
				Content: geminiContent{
					Role:  "model",
					Parts: []geminiPart{{Text: "Partial..."}},
				},
				FinishReason: "MAX_TOKENS",
			},
		},
		UsageMetadata: &geminiUsageMeta{
			PromptTokenCount:     50,
			CandidatesTokenCount: 100,
			TotalTokenCount:      150,
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(gemResp)
	}))
	defer srv.Close()

	resp, _ := http.Get(srv.URL)
	adapter := &GeminiAdapter{}
	result, err := adapter.ConvertResponse(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Choices[0].FinishReason != "length" {
		t.Errorf("expected finish_reason 'length' for MAX_TOKENS, got %q", result.Choices[0].FinishReason)
	}
}

func TestGeminiAdapter_ConvertResponse_SafetyStop(t *testing.T) {
	gemResp := geminiResponse{
		Candidates: []geminiCandidate{
			{
				Content:      geminiContent{Role: "model", Parts: []geminiPart{{Text: ""}}},
				FinishReason: "SAFETY",
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(gemResp)
	}))
	defer srv.Close()

	resp, _ := http.Get(srv.URL)
	adapter := &GeminiAdapter{}
	result, err := adapter.ConvertResponse(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Choices[0].FinishReason != "content_filter" {
		t.Errorf("expected finish_reason 'content_filter' for SAFETY, got %q", result.Choices[0].FinishReason)
	}
}

func TestGeminiAdapter_ConvertResponse_NoUsage(t *testing.T) {
	gemResp := geminiResponse{
		Candidates: []geminiCandidate{
			{
				Content:      geminiContent{Role: "model", Parts: []geminiPart{{Text: "Hello"}}},
				FinishReason: "STOP",
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(gemResp)
	}))
	defer srv.Close()

	resp, _ := http.Get(srv.URL)
	adapter := &GeminiAdapter{}
	result, err := adapter.ConvertResponse(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Usage != nil {
		t.Errorf("expected nil usage, got %+v", result.Usage)
	}
}

func TestGeminiAdapter_ConvertResponse_NilResponse(t *testing.T) {
	adapter := &GeminiAdapter{}
	_, err := adapter.ConvertResponse(nil)
	if err == nil {
		t.Fatal("expected error for nil response")
	}
}

func TestGeminiAdapter_ConvertResponse_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	resp, _ := http.Get(srv.URL)
	adapter := &GeminiAdapter{}
	_, err := adapter.ConvertResponse(resp)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// ---------------------------------------------------------------------------
// GeminiAdapter.ExtractUsage
// ---------------------------------------------------------------------------

func TestGeminiAdapter_ExtractUsage_WithUsage(t *testing.T) {
	adapter := &GeminiAdapter{}
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

func TestGeminiAdapter_ExtractUsage_NilResponse(t *testing.T) {
	adapter := &GeminiAdapter{}
	if usage := adapter.ExtractUsage(nil); usage != nil {
		t.Errorf("expected nil usage, got %+v", usage)
	}
}

func TestGeminiAdapter_ExtractUsage_NoUsage(t *testing.T) {
	adapter := &GeminiAdapter{}
	resp := &OpenAIChatResponse{}
	if usage := adapter.ExtractUsage(resp); usage != nil {
		t.Errorf("expected nil usage, got %+v", usage)
	}
}

// ---------------------------------------------------------------------------
// End-to-end: ConvertRequest → mock Gemini server → ConvertResponse
// ---------------------------------------------------------------------------

func TestGeminiAdapter_EndToEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/models/gemini-pro:generateContent") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		// API key should be in query param
		if got := r.URL.Query().Get("key"); got != "AIzaSy-e2e-key" {
			t.Errorf("expected query key='AIzaSy-e2e-key', got %q", got)
		}
		if got := r.Header.Get("User-Agent"); got != "claude-code/2.0.0" {
			t.Errorf("expected User-Agent 'claude-code/2.0.0', got %q", got)
		}
		// No Authorization header for Gemini
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("expected no Authorization header, got %q", got)
		}

		// Verify body has system extracted to systemInstruction
		var reqBody geminiRequest
		json.NewDecoder(r.Body).Decode(&reqBody)
		if reqBody.SystemInstruction == nil {
			t.Fatal("expected non-nil systemInstruction")
		}
		if len(reqBody.SystemInstruction.Parts) != 1 || reqBody.SystemInstruction.Parts[0].Text != "You are helpful" {
			t.Errorf("unexpected systemInstruction: %+v", reqBody.SystemInstruction)
		}
		if len(reqBody.Contents) != 1 {
			t.Errorf("expected 1 content, got %d", len(reqBody.Contents))
		}
		if reqBody.Contents[0].Role != "user" {
			t.Errorf("expected role 'user', got %q", reqBody.Contents[0].Role)
		}

		// Return Gemini response
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(geminiResponse{
			Candidates: []geminiCandidate{
				{
					Content: geminiContent{
						Role:  "model",
						Parts: []geminiPart{{Text: "Hi there!"}},
					},
					FinishReason: "STOP",
				},
			},
			UsageMetadata: &geminiUsageMeta{
				PromptTokenCount:     20,
				CandidatesTokenCount: 8,
				TotalTokenCount:      28,
			},
			ModelVersion: "gemini-pro-001",
		})
	}))
	defer srv.Close()

	adapter := &GeminiAdapter{}
	provider := &ComputeProvider{
		BaseURL:   srv.URL,
		APIKey:    "AIzaSy-e2e-key",
		UserAgent: "claude-code/2.0.0",
	}
	chatReq := &OpenAIChatRequest{
		Model: "gemini-pro",
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

	if result.Model != "gemini-pro-001" {
		t.Errorf("expected model 'gemini-pro-001', got %q", result.Model)
	}
	if len(result.Choices) != 1 || result.Choices[0].Message.Content != "Hi there!" {
		t.Errorf("unexpected choices: %+v", result.Choices)
	}
	if result.Choices[0].Message.Role != "assistant" {
		t.Errorf("expected role 'assistant', got %q", result.Choices[0].Message.Role)
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
