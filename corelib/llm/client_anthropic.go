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
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

type AnthropicMessagesRequestOptions struct {
	Stream                  bool
	Tools                   []map[string]interface{}
	MaxTokens               int
	ExplicitToolReplacement bool
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
	if len(opts.Tools) > 0 || opts.ExplicitToolReplacement {
		if at := ConvertToAnthropicTools(opts.Tools); len(at) > 0 {
			reqBody["tools"] = at
		} else if opts.ExplicitToolReplacement {
			// An owner that is replacing a previously executable surface must
			// communicate an empty surface explicitly. Omitting tools leaves
			// stateful Anthropic-compatible providers free to retain history.
			reqBody["tools"] = []map[string]interface{}{}
		}
	}
	corelib.ApplyReasoningControls(cfg, reqBody, corelib.ReasoningAPIAnthropic)
	ensureAnthropicThinkingFitsOutputLimit(reqBody)
	return reqBody
}

// Anthropic requires an extended-thinking budget to be smaller than the
// request's maximum output budget. Bound the budget to the configured output
// limit instead of silently enlarging a user-selected limit: max_tokens is a
// cost and latency guard, so changing it would violate that configuration.
func ensureAnthropicThinkingFitsOutputLimit(reqBody map[string]interface{}) {
	thinking, _ := reqBody["thinking"].(map[string]interface{})
	if thinking == nil || !strings.EqualFold(strings.TrimSpace(fmt.Sprint(thinking["type"])), "enabled") {
		return
	}
	budget, ok := thinking["budget_tokens"].(int)
	if !ok || budget < 1024 {
		return
	}
	maxTokens, _ := reqBody["max_tokens"].(int)
	if maxTokens < 2 {
		return
	}
	maxBudget := maxTokens - 1
	if budget > maxBudget {
		thinking["budget_tokens"] = maxBudget
	}
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
		ID         string                  `json:"id,omitempty"`
		Content    []AnthropicContentBlock `json:"content"`
		StopReason string                  `json:"stop_reason"`
		Usage      *Usage                  `json:"usage,omitempty"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse anthropic response: %w", err)
	}

	msg := Message{Role: "assistant"}
	var textParts []string
	var reasoningParts []string
	for _, block := range raw.Content {
		switch block.Type {
		case "text":
			textParts = append(textParts, block.Text)
		case "thinking":
			if text := anthropicThinkingBlockText(block.Type, block.Thinking, block.Text); text != "" {
				reasoningParts = append(reasoningParts, text)
			}
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
	// Match the stream path: thinking deltas are concatenated, not joined with extra newlines.
	msg.ReasoningContent = strings.Join(reasoningParts, "")

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
		ResponseID: strings.TrimSpace(raw.ID),
		Choices: []Choice{{
			Message:      msg,
			FinishReason: finishReason,
		}},
		Usage: raw.Usage,
	}, nil
}

func anthropicThinkingBlockText(blockType, thinking, text string) string {
	switch strings.TrimSpace(blockType) {
	case "thinking":
		if thinking != "" {
			return thinking
		}
		return text
	default:
		return ""
	}
}

func anthropicDeltaThinkingText(deltaType, thinking, text string) string {
	switch strings.TrimSpace(deltaType) {
	case "thinking_delta", "reasoning_delta":
		if thinking != "" {
			return thinking
		}
		return text
	default:
		return ""
	}
}

func appendAnthropicThinking(buf *strings.Builder, onReasoning TokenCallback, text string) {
	if text == "" {
		return
	}
	if buf != nil {
		buf.WriteString(text)
	}
	if onReasoning != nil {
		onReasoning(text)
	}
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
