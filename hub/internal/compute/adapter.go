package compute

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// OpenAIChatRequest represents an OpenAI-compatible chat completion request.
type OpenAIChatRequest struct {
	Model       string                   `json:"model"`
	Messages    []map[string]interface{} `json:"messages"`
	MaxTokens   *int                     `json:"max_tokens,omitempty"`
	Temperature *float64                 `json:"temperature,omitempty"`
	TopP        *float64                 `json:"top_p,omitempty"`
	Stream      bool                     `json:"stream,omitempty"`
	Stop        interface{}              `json:"stop,omitempty"`
}

// OpenAIChatResponse represents an OpenAI-compatible chat completion response.
type OpenAIChatResponse struct {
	ID      string              `json:"id"`
	Object  string              `json:"object"`
	Model   string              `json:"model"`
	Choices []OpenAIChoice      `json:"choices"`
	Usage   *TokenUsage         `json:"usage,omitempty"`
	Error   *OpenAIErrorPayload `json:"error,omitempty"`
}

// OpenAIChoice represents a single choice in the chat completion response.
type OpenAIChoice struct {
	Index        int           `json:"index"`
	Message      OpenAIMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

// OpenAIMessage represents a message in the chat completion response.
type OpenAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// OpenAIErrorPayload represents an error in the OpenAI error format.
type OpenAIErrorPayload struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code,omitempty"`
}

// TokenUsage holds token counts from an LLM response.
type TokenUsage struct {
	InputTokens  int64 `json:"prompt_tokens"`
	OutputTokens int64 `json:"completion_tokens"`
	TotalTokens  int64 `json:"total_tokens"`
}

// ProtocolAdapter converts requests and responses between OpenAI format
// and a provider's native protocol.
type ProtocolAdapter interface {
	// ConvertRequest builds an http.Request for the target provider from an OpenAI-format request.
	ConvertRequest(req *OpenAIChatRequest, provider *ComputeProvider) (*http.Request, error)
	// ConvertResponse parses the provider's HTTP response into an OpenAI-format response.
	ConvertResponse(resp *http.Response) (*OpenAIChatResponse, error)
	// ExtractUsage returns token usage from the parsed response.
	ExtractUsage(resp *OpenAIChatResponse) *TokenUsage
}

// GetAdapter returns the ProtocolAdapter for the given protocol string.
// Returns an error for unsupported protocols.
func GetAdapter(protocol string) (ProtocolAdapter, error) {
	switch protocol {
	case ProtocolOpenAI:
		return &OpenAIAdapter{}, nil
	case ProtocolAnthropic:
		return &AnthropicAdapter{}, nil
	case ProtocolGemini:
		return &GeminiAdapter{}, nil
	default:
		return nil, fmt.Errorf("unsupported protocol: %s", protocol)
	}
}

// ---------------------------------------------------------------------------
// OpenAI Adapter (passthrough)
// ---------------------------------------------------------------------------

// OpenAIAdapter is a passthrough adapter for OpenAI-compatible providers.
type OpenAIAdapter struct{}

// ConvertRequest builds a POST request to base_url/chat/completions with
// Authorization Bearer header and the provider's User-Agent.
func (a *OpenAIAdapter) ConvertRequest(req *OpenAIChatRequest, provider *ComputeProvider) (*http.Request, error) {
	if req == nil {
		return nil, fmt.Errorf("request is nil")
	}
	if provider == nil {
		return nil, fmt.Errorf("provider is nil")
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := strings.TrimRight(provider.BaseURL, "/") + "/chat/completions"
	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+provider.APIKey)
	if provider.UserAgent != "" {
		httpReq.Header.Set("User-Agent", provider.UserAgent)
	}

	return httpReq, nil
}

// ConvertResponse decodes the HTTP response body directly as an OpenAIChatResponse.
func (a *OpenAIAdapter) ConvertResponse(resp *http.Response) (*OpenAIChatResponse, error) {
	if resp == nil {
		return nil, fmt.Errorf("response is nil")
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	var result OpenAIChatResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &result, nil
}

// ExtractUsage returns the Usage field from the response directly.
func (a *OpenAIAdapter) ExtractUsage(resp *OpenAIChatResponse) *TokenUsage {
	if resp == nil {
		return nil
	}
	return resp.Usage
}
