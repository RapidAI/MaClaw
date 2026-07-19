package llm

// client_anthropic.go provides a non-streaming Anthropic Messages API request
// function, parallel to DoOpenAIRequest in client.go.
//
// Used by corelib/agent.RunLoop when cfg.Protocol == "anthropic".

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/RapidAI/CodeClaw/corelib"
)

type AnthropicMessagesRequestOptions struct {
	Stream    bool
	Tools     []map[string]interface{}
	MaxTokens int
}

func BuildAnthropicMessagesRequestBody(
	cfg corelib.MaclawLLMConfig,
	messages []interface{},
	opts AnthropicMessagesRequestOptions,
) map[string]interface{} {
	messages = SanitizeOpenAICompatRequestMessages(messages, true)
	converted := ConvertToAnthropicMessages(messages)
	maxTokens := opts.MaxTokens
	if maxTokens <= 0 {
		// Use config-aware default: user setting > model-specific default > hardcoded fallback
		maxTokens = cfg.EffectiveMaxOutputTokens()
	}
	reqBody := map[string]interface{}{
		"model":      cfg.UpstreamModel(),
		"messages":   converted.Messages,
		"max_tokens": maxTokens,
		"stream":     opts.Stream,
	}
	if converted.SystemText != "" {
		if cfg.EnablePromptCache {
			// Anthropic cache_control: mark system prompt for provider-side KV caching.
			// The "ephemeral" type indicates this content is stable within the session
			// and should be cached for subsequent requests. Saves ~90% of system prompt
			// input token cost on iterations 2-80 of SubAgent tasks.
			reqBody["system"] = []map[string]interface{}{
				{
					"type":          "text",
					"text":          converted.SystemText,
					"cache_control": map[string]string{"type": "ephemeral"},
				},
			}
		} else {
			reqBody["system"] = converted.SystemText
		}
	}
	if len(opts.Tools) > 0 {
		if at := ConvertToAnthropicTools(opts.Tools); len(at) > 0 {
			reqBody["tools"] = at
		}
	}
	return reqBody
}

func BuildAnthropicMessagesRequestData(
	cfg corelib.MaclawLLMConfig,
	messages []interface{},
	opts AnthropicMessagesRequestOptions,
) (endpoint string, body []byte, err error) {
	endpoint = corelib.AnthropicMessagesEndpoint(cfg.URL)
	body, err = json.Marshal(BuildAnthropicMessagesRequestBody(cfg, messages, opts))
	return endpoint, body, err
}

// DoAnthropicRequest sends a non-streaming Anthropic Messages API request.
// It converts OpenAI-style messages/tools to Anthropic format, sends the
// request, and converts the response back to the unified *Response type.
func DoAnthropicRequest(
	ctx context.Context,
	cfg corelib.MaclawLLMConfig,
	messages []interface{},
	tools []map[string]interface{},
	client *http.Client,
) (*Response, error) {
	return DoAnthropicRequestWithOptions(ctx, cfg, messages, tools, client, AnthropicMessagesRequestOptions{Stream: false, Tools: tools})
}

func DoAnthropicRequestWithOptions(
	ctx context.Context,
	cfg corelib.MaclawLLMConfig,
	messages []interface{},
	tools []map[string]interface{},
	client *http.Client,
	opts AnthropicMessagesRequestOptions,
) (*Response, error) {
	opts.Stream = false
	opts.Tools = tools
	endpoint, data, err := BuildAnthropicMessagesRequestData(cfg, messages, opts)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	resp, status, body, err := anthropicSDKMessage(ctx, cfg, data, client)
	// status==0 means no HTTP response (network/cancel); keep the original error.
	if se := newHTTPStatusError(status, body); se != nil {
		return nil, se
	}
	if err != nil {
		return nil, fmt.Errorf("[%s] %w", endpoint, err)
	}
	return resp, nil
}

// parseAnthropicResponseBody parses a non-streaming Anthropic Messages API response.
func parseAnthropicResponseBody(body []byte) (*Response, error) {
	var raw struct {
		Content    []AnthropicContentBlock `json:"content"`
		StopReason string                  `json:"stop_reason"`
		Usage      *Usage                  `json:"usage,omitempty"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse anthropic response: %w", err)
	}

	msg := Message{Role: "assistant"}
	var textParts []string
	for _, block := range raw.Content {
		switch block.Type {
		case "text":
			textParts = append(textParts, block.Text)
		case "tool_use":
			argsJSON, _ := json.Marshal(block.Input)
			msg.ToolCalls = append(msg.ToolCalls, ToolCall{
				ID:   block.ID,
				Type: "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{
					Name:      block.Name,
					Arguments: string(argsJSON),
				},
			})
		}
	}
	msg.Content = StripAllExtra(joinStrings(textParts))

	finishReason := "stop"
	if raw.StopReason == "tool_use" {
		finishReason = "tool_calls"
	} else if raw.StopReason == "max_tokens" {
		finishReason = "length"
	}
	if len(msg.ToolCalls) == 0 {
		rawContent := joinStrings(textParts)
		if contentCalls, malformed := ParseContentToolCallsDetailed(rawContent); len(contentCalls) > 0 {
			msg.ToolCalls = append(msg.ToolCalls, contentCalls...)
			msg.Content = ""
			finishReason = "tool_calls"
		} else if malformed {
			msg.Content = MalformedContentToolCallErrorMsg
			finishReason = "stop"
		}
	}

	return &Response{
		Choices: []Choice{{
			Message:      msg,
			FinishReason: finishReason,
		}},
		Usage: raw.Usage,
	}, nil
}

func joinStrings(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		return parts[0]
	}
	result := parts[0]
	for _, p := range parts[1:] {
		result += "\n" + p
	}
	return result
}
