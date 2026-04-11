package compute

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Anthropic request/response types for the Messages API.

type anthropicRequest struct {
	Model       string                   `json:"model"`
	System      string                   `json:"system,omitempty"`
	Messages    []map[string]interface{} `json:"messages"`
	MaxTokens   int                      `json:"max_tokens"`
	Temperature *float64                 `json:"temperature,omitempty"`
	TopP        *float64                 `json:"top_p,omitempty"`
	Stream      bool                     `json:"stream,omitempty"`
	Stop        interface{}              `json:"stop_sequences,omitempty"`
}

type anthropicResponse struct {
	ID         string             `json:"id"`
	Model      string             `json:"model"`
	Content    []anthropicContent `json:"content"`
	StopReason string             `json:"stop_reason"`
	Usage      *anthropicUsage    `json:"usage,omitempty"`
}

type anthropicContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type anthropicUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

// AnthropicAdapter converts between OpenAI format and Anthropic Messages API.
type AnthropicAdapter struct{}

// ConvertRequest builds an HTTP request for the Anthropic Messages API from
// an OpenAI-format chat request.
func (a *AnthropicAdapter) ConvertRequest(req *OpenAIChatRequest, provider *ComputeProvider) (*http.Request, error) {
	if req == nil {
		return nil, fmt.Errorf("request is nil")
	}
	if provider == nil {
		return nil, fmt.Errorf("provider is nil")
	}

	// Extract system messages and separate non-system messages.
	var systemParts []string
	var messages []map[string]interface{}
	for _, msg := range req.Messages {
		role, _ := msg["role"].(string)
		if role == "system" {
			content, _ := msg["content"].(string)
			if content != "" {
				systemParts = append(systemParts, content)
			}
		} else {
			messages = append(messages, msg)
		}
	}

	maxTokens := 1024
	if req.MaxTokens != nil {
		maxTokens = *req.MaxTokens
	}

	antReq := anthropicRequest{
		Model:       req.Model,
		Messages:    messages,
		MaxTokens:   maxTokens,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stream:      req.Stream,
		Stop:        req.Stop,
	}
	if len(systemParts) > 0 {
		antReq.System = strings.Join(systemParts, "\n")
	}

	body, err := json.Marshal(antReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := strings.TrimRight(provider.BaseURL, "/") + "/messages"
	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	httpReq.Header.Set("x-api-key", provider.APIKey)
	httpReq.Header.Set("Authorization", "Bearer "+provider.APIKey)
	if provider.UserAgent != "" {
		httpReq.Header.Set("User-Agent", provider.UserAgent)
	}

	return httpReq, nil
}

// ConvertResponse parses an Anthropic Messages API response into OpenAI format.
func (a *AnthropicAdapter) ConvertResponse(resp *http.Response) (*OpenAIChatResponse, error) {
	if resp == nil {
		return nil, fmt.Errorf("response is nil")
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	var antResp anthropicResponse
	if err := json.Unmarshal(body, &antResp); err != nil {
		return nil, fmt.Errorf("decode anthropic response: %w", err)
	}

	// Build content from the first text block.
	var content string
	for _, c := range antResp.Content {
		if c.Type == "text" {
			content = c.Text
			break
		}
	}

	// Map Anthropic stop_reason to OpenAI finish_reason.
	finishReason := mapAnthropicStopReason(antResp.StopReason)

	result := &OpenAIChatResponse{
		ID:    antResp.ID,
		Object: "chat.completion",
		Model: antResp.Model,
		Choices: []OpenAIChoice{
			{
				Index:        0,
				Message:      OpenAIMessage{Role: "assistant", Content: content},
				FinishReason: finishReason,
			},
		},
	}

	if antResp.Usage != nil {
		result.Usage = &TokenUsage{
			InputTokens:  antResp.Usage.InputTokens,
			OutputTokens: antResp.Usage.OutputTokens,
			TotalTokens:  antResp.Usage.InputTokens + antResp.Usage.OutputTokens,
		}
	}

	return result, nil
}

// ExtractUsage returns token usage from the converted OpenAI response.
func (a *AnthropicAdapter) ExtractUsage(resp *OpenAIChatResponse) *TokenUsage {
	if resp == nil {
		return nil
	}
	return resp.Usage
}

// mapAnthropicStopReason converts Anthropic stop reasons to OpenAI finish reasons.
func mapAnthropicStopReason(reason string) string {
	switch reason {
	case "end_turn":
		return "stop"
	case "max_tokens":
		return "length"
	case "stop_sequence":
		return "stop"
	default:
		if reason == "" {
			return "stop"
		}
		return reason
	}
}
