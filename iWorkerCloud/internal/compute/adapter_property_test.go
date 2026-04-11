// Feature: compute-power-management, Property 8-13
package compute

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"testing/quick"
)

// TestPropertyOpenAIPassthrough verifies that OpenAI adapter round-trips
// request content correctly (Property 8).
func TestPropertyOpenAIPassthrough(t *testing.T) {
	adapter := &OpenAIAdapter{}

	f := func(model, content string) bool {
		if model == "" {
			model = "gpt-4"
		}
		req := &OpenAIChatRequest{
			Model: model,
			Messages: []map[string]interface{}{
				{"role": "user", "content": content},
			},
		}
		provider := &ComputeProvider{
			BaseURL:   "https://api.example.com",
			APIKey:    "test-key",
			UserAgent: "test-agent",
		}

		httpReq, err := adapter.ConvertRequest(req, provider)
		if err != nil {
			t.Logf("convert request error: %v", err)
			return false
		}

		// Verify the request body can be decoded back
		body, _ := io.ReadAll(httpReq.Body)
		var decoded OpenAIChatRequest
		if err := json.Unmarshal(body, &decoded); err != nil {
			t.Logf("decode error: %v", err)
			return false
		}

		if decoded.Model != model {
			return false
		}
		if httpReq.Header.Get("User-Agent") != "test-agent" {
			return false
		}
		if httpReq.Header.Get("Authorization") != "Bearer test-key" {
			return false
		}

		return true
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Errorf("Property 8 failed: %v", err)
	}
}

// TestPropertyAnthropicSystemExtraction verifies that system messages are
// extracted into the Anthropic system parameter (Property 9).
func TestPropertyAnthropicSystemExtraction(t *testing.T) {
	adapter := &AnthropicAdapter{}

	f := func(systemMsg, userMsg string) bool {
		if systemMsg == "" {
			return true // skip empty system messages
		}
		req := &OpenAIChatRequest{
			Model: "claude-3",
			Messages: []map[string]interface{}{
				{"role": "system", "content": systemMsg},
				{"role": "user", "content": userMsg},
			},
		}
		provider := &ComputeProvider{
			BaseURL: "https://api.anthropic.com",
			APIKey:  "test-key",
		}

		httpReq, err := adapter.ConvertRequest(req, provider)
		if err != nil {
			t.Logf("convert error: %v", err)
			return false
		}

		// Verify anthropic-version header
		if httpReq.Header.Get("anthropic-version") != "2023-06-01" {
			t.Log("missing anthropic-version header")
			return false
		}

		// Verify both auth headers
		if httpReq.Header.Get("x-api-key") != "test-key" {
			t.Log("missing x-api-key header")
			return false
		}
		if httpReq.Header.Get("Authorization") != "Bearer test-key" {
			t.Log("missing Authorization header")
			return false
		}

		// Verify system message is extracted into the system field
		body, _ := io.ReadAll(httpReq.Body)
		var antReq anthropicRequest
		if err := json.Unmarshal(body, &antReq); err != nil {
			t.Logf("decode error: %v", err)
			return false
		}

		if antReq.System == "" {
			t.Log("system field should not be empty")
			return false
		}

		// System messages should not appear in messages array
		for _, msg := range antReq.Messages {
			role, _ := msg["role"].(string)
			if role == "system" {
				t.Log("system message should not be in messages array")
				return false
			}
		}

		return true
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Errorf("Property 9 failed: %v", err)
	}
}

// TestPropertyGeminiFormatConversion verifies that OpenAI messages are
// converted to Gemini contents format (Property 10).
func TestPropertyGeminiFormatConversion(t *testing.T) {
	adapter := &GeminiAdapter{}

	f := func(systemMsg, userMsg string) bool {
		messages := []map[string]interface{}{
			{"role": "user", "content": userMsg},
		}
		if systemMsg != "" {
			messages = append([]map[string]interface{}{
				{"role": "system", "content": systemMsg},
			}, messages...)
		}

		req := &OpenAIChatRequest{
			Model:    "gemini-pro",
			Messages: messages,
		}
		provider := &ComputeProvider{
			BaseURL: "https://generativelanguage.googleapis.com/v1",
			APIKey:  "test-key",
			Model:   "gemini-pro",
		}

		httpReq, err := adapter.ConvertRequest(req, provider)
		if err != nil {
			t.Logf("convert error: %v", err)
			return false
		}

		// Verify API key is in query parameter
		if httpReq.URL.Query().Get("key") != "test-key" {
			t.Log("API key should be in query parameter")
			return false
		}

		body, _ := io.ReadAll(httpReq.Body)
		var gemReq geminiRequest
		if err := json.Unmarshal(body, &gemReq); err != nil {
			t.Logf("decode error: %v", err)
			return false
		}

		// Verify contents array exists
		if len(gemReq.Contents) == 0 {
			t.Log("contents should not be empty")
			return false
		}

		// If system message provided, verify systemInstruction is set
		if systemMsg != "" {
			if gemReq.SystemInstruction == nil {
				t.Log("systemInstruction should be set when system message exists")
				return false
			}
		}

		return true
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Errorf("Property 10 failed: %v", err)
	}
}

// TestPropertyErrorResponsePreservesStatus verifies that error conversion
// preserves the HTTP status code (Property 11).
func TestPropertyErrorResponsePreservesStatus(t *testing.T) {
	f := func(statusCode uint16, errMsg string) bool {
		code := int(statusCode%500) + 400 // 400-899 range
		if code == 200 {
			code = 400
		}

		resp := &http.Response{
			StatusCode: code,
			Body:       io.NopCloser(bytes.NewBufferString(errMsg)),
		}

		result, returnedCode := ConvertErrorResponse(resp, "openai")

		if returnedCode != code {
			t.Logf("status code mismatch: got %d, want %d", returnedCode, code)
			return false
		}
		if result.Error == nil {
			t.Log("error payload should not be nil")
			return false
		}
		if result.Error.Type == "" {
			t.Log("error type should not be empty")
			return false
		}

		return true
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Errorf("Property 11 failed: %v", err)
	}
}


// TestPropertyTokenUsageExtraction verifies that token usage extraction from
// all protocol responses produces correct total_tokens (Property 12).
func TestPropertyTokenUsageExtraction(t *testing.T) {
	f := func(inputTokens, outputTokens uint32) bool {
		in := int64(inputTokens)
		out := int64(outputTokens)

		resp := &OpenAIChatResponse{
			Usage: &TokenUsage{
				InputTokens:  in,
				OutputTokens: out,
				TotalTokens:  in + out,
			},
		}

		// Test OpenAI adapter extraction
		openaiAdapter := &OpenAIAdapter{}
		usage := openaiAdapter.ExtractUsage(resp)
		if usage == nil {
			t.Log("OpenAI usage should not be nil")
			return false
		}
		if usage.TotalTokens != in+out {
			return false
		}

		// Test Anthropic adapter extraction
		anthropicAdapter := &AnthropicAdapter{}
		usage = anthropicAdapter.ExtractUsage(resp)
		if usage == nil {
			t.Log("Anthropic usage should not be nil")
			return false
		}
		if usage.TotalTokens != in+out {
			return false
		}

		// Test Gemini adapter extraction
		geminiAdapter := &GeminiAdapter{}
		usage = geminiAdapter.ExtractUsage(resp)
		if usage == nil {
			t.Log("Gemini usage should not be nil")
			return false
		}
		if usage.TotalTokens != in+out {
			return false
		}

		return true
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Errorf("Property 12 failed: %v", err)
	}
}

// TestPropertyTokenEstimationFallback verifies that token estimation produces
// positive values for non-empty text (Property 13).
func TestPropertyTokenEstimationFallback(t *testing.T) {
	f := func(inputText, outputText string) bool {
		usage := EstimateTokenUsage(inputText, outputText)
		if usage == nil {
			t.Log("estimated usage should not be nil")
			return false
		}

		// For non-empty input, tokens should be positive
		if inputText != "" && usage.InputTokens <= 0 {
			t.Logf("input tokens should be positive for non-empty text, got %d", usage.InputTokens)
			return false
		}
		if outputText != "" && usage.OutputTokens <= 0 {
			t.Logf("output tokens should be positive for non-empty text, got %d", usage.OutputTokens)
			return false
		}

		// For empty input, tokens should be zero
		if inputText == "" && usage.InputTokens != 0 {
			return false
		}
		if outputText == "" && usage.OutputTokens != 0 {
			return false
		}

		// Total should always equal sum
		if usage.TotalTokens != usage.InputTokens+usage.OutputTokens {
			return false
		}

		return true
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Errorf("Property 13 failed: %v", err)
	}
}
