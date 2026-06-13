package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// Shared LLM types for both stream and non-stream responses

type Response struct {
	Choices       []Choice `json:"choices"`
	Usage         *Usage   `json:"usage,omitempty"`
	LocalCacheHit bool     `json:"-"`
}

type Choice struct {
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`

	// TruncatedToolNames lists tool names whose JSON arguments were
	// incomplete (output token limit hit) and were removed by
	// filterTruncatedToolCalls. Non-nil means the agent loop should
	// treat this as a recoverable error: inject a system message with
	// the truncation hint and continue the loop so the LLM can retry
	// with shorter arguments.
	// This is NOT serialized — it is an in-process signal only.
	TruncatedToolNames []string `json:"-"`
}

type Message struct {
	Role             string      `json:"role"`
	Content          string      `json:"content"`
	ReasoningContent string      `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall  `json:"tool_calls,omitempty"`
	RawContent       interface{} `json:"-"`
}

type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
}

type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`     // OpenAI style
	CompletionTokens int `json:"completion_tokens"` // OpenAI style
	TotalTokens      int `json:"total_tokens"`      // OpenAI style

	InputTokens  int `json:"input_tokens,omitempty"`  // Anthropic style
	OutputTokens int `json:"output_tokens,omitempty"` // Anthropic style

	CachedInputTokens int `json:"cached_input_tokens,omitempty"`
	CacheWriteTokens  int `json:"cache_write_tokens,omitempty"`
}

func (u *Usage) UnmarshalJSON(data []byte) error {
	type usageAlias Usage
	var raw struct {
		usageAlias
		CacheReadInputTokens  int            `json:"cache_read_input_tokens"`
		CacheWriteInputTokens int            `json:"cache_write_input_tokens"`
		CacheCreationTokens   int            `json:"cache_creation_input_tokens"`
		PromptTokensDetails   map[string]int `json:"prompt_tokens_details"`
		InputTokensDetails    map[string]int `json:"input_tokens_details"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*u = Usage(raw.usageAlias)
	if u.InputTokens == 0 && u.PromptTokens > 0 {
		u.InputTokens = u.PromptTokens
	}
	if u.PromptTokens == 0 && u.InputTokens > 0 {
		u.PromptTokens = u.InputTokens
	}
	if u.OutputTokens == 0 && u.CompletionTokens > 0 {
		u.OutputTokens = u.CompletionTokens
	}
	if u.CompletionTokens == 0 && u.OutputTokens > 0 {
		u.CompletionTokens = u.OutputTokens
	}
	if u.TotalTokens == 0 {
		u.TotalTokens = u.InputTokens + u.OutputTokens
	}
	if u.CachedInputTokens == 0 {
		u.CachedInputTokens = firstPositiveUsageValue(
			raw.CacheReadInputTokens,
			raw.PromptTokensDetails["cached_tokens"],
			raw.InputTokensDetails["cached_tokens"],
		)
	}
	if u.CacheWriteTokens == 0 {
		u.CacheWriteTokens = firstPositiveUsageValue(
			raw.CacheWriteInputTokens,
			raw.CacheCreationTokens,
			raw.PromptTokensDetails["cache_write_tokens"],
			raw.PromptTokensDetails["cache_creation_input_tokens"],
			raw.InputTokensDetails["cache_write_tokens"],
			raw.InputTokensDetails["cache_creation_input_tokens"],
		)
	}
	return nil
}

func firstPositiveUsageValue(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

type openAIWireResponse struct {
	ID                string             `json:"id,omitempty"`
	Object            string             `json:"object,omitempty"`
	Created           int64              `json:"created,omitempty"`
	Model             string             `json:"model,omitempty"`
	SystemFingerprint string             `json:"system_fingerprint,omitempty"`
	Choices           []openAIWireChoice `json:"choices"`
	Usage             *Usage             `json:"usage,omitempty"`
}

type openAIWireChoice struct {
	Index        int               `json:"index,omitempty"`
	Message      openAIWireMessage `json:"message"`
	FinishReason string            `json:"finish_reason"`
}

type openAIWireMessage struct {
	Role             string      `json:"role"`
	Content          interface{} `json:"content"`
	ReasoningContent string      `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall  `json:"tool_calls,omitempty"`
	FunctionCall     *struct {
		Name      string          `json:"name,omitempty"`
		Arguments json.RawMessage `json:"arguments,omitempty"`
	} `json:"function_call,omitempty"`
}

// TokenCallback is called with each text delta from the LLM streaming response.
type TokenCallback func(delta string)

// Anthropic specific structures

type AnthropicContentBlock struct {
	Type  string      `json:"type"`
	Text  string      `json:"text,omitempty"`
	ID    string      `json:"id,omitempty"`
	Name  string      `json:"name,omitempty"`
	Input interface{} `json:"input,omitempty"`
}

// ParseNonStreamAnthropicResponse handles the fallback case where the provider
// returned a normal JSON response instead of SSE for Anthropic protocol.
func ParseNonStreamAnthropicResponse(resp *http.Response) (*Response, error) {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("llm error: status=%d body_len=%d", resp.StatusCode, len(body))
	}

	var anthropicResp struct {
		Content    []AnthropicContentBlock `json:"content"`
		StopReason string                  `json:"stop_reason"`
		Usage      *Usage                  `json:"usage,omitempty"`
	}
	if err := json.Unmarshal(body, &anthropicResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	msg := Message{Role: "assistant"}
	var textParts []string
	for _, block := range anthropicResp.Content {
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

	msg.Content = StripAllExtra(strings.Join(textParts, "\n"))

	finishReason := "stop"
	if anthropicResp.StopReason == "tool_use" {
		finishReason = "tool_calls"
	} else if anthropicResp.StopReason == "max_tokens" {
		finishReason = "length"
	}
	if len(msg.ToolCalls) == 0 {
		rawContent := strings.Join(textParts, "\n")
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
		Choices: []Choice{{Message: msg, FinishReason: finishReason}},
		Usage:   anthropicResp.Usage,
	}, nil
}

func normalizeOpenAIMessageContent(raw interface{}) (string, interface{}) {
	switch v := raw.(type) {
	case nil:
		return "", nil
	case string:
		return v, v
	default:
		items := toInterfaceSlice(v)
		if len(items) == 0 {
			b, err := json.Marshal(v)
			if err != nil {
				return fmt.Sprintf("%v", v), v
			}
			return string(b), v
		}
		parts := make([]string, 0, len(items))
		for _, item := range items {
			m := toStringInterfaceMap(item)
			if m == nil {
				continue
			}
			typ := stringField(m, "type")
			switch typ {
			case "text", "input_text", "output_text":
				if text := stringField(m, "text"); text != "" {
					parts = append(parts, text)
					continue
				}
				if text := stringField(m, "content"); text != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n"), v
	}
}

func projectOpenAIWireResponse(wire openAIWireResponse) *Response {
	result := &Response{Usage: wire.Usage}
	if len(wire.Choices) == 0 {
		return result
	}
	result.Choices = make([]Choice, 0, len(wire.Choices))
	for _, choice := range wire.Choices {
		content, rawContent := normalizeOpenAIMessageContent(choice.Message.Content)
		msg := Message{
			Role:             choice.Message.Role,
			Content:          StripAllExtra(content),
			ReasoningContent: choice.Message.ReasoningContent,
			ToolCalls:        choice.Message.ToolCalls,
			RawContent:       rawContent,
		}
		finishReason := choice.FinishReason
		if finishReason == "function_call" {
			finishReason = "tool_calls"
		}
		if len(msg.ToolCalls) == 0 && choice.Message.FunctionCall != nil {
			if call, ok := normalizePlainContentToolCallWithID("", "function", choice.Message.FunctionCall.Name, choice.Message.FunctionCall.Arguments); ok {
				msg.ToolCalls = append(msg.ToolCalls, call)
				msg.Content = ""
				finishReason = "tool_calls"
			}
		}
		if len(msg.ToolCalls) == 0 {
			if contentCalls, malformed := ParseContentToolCallsDetailed(content); len(contentCalls) > 0 {
				msg.ToolCalls = append(msg.ToolCalls, contentCalls...)
				msg.Content = ""
				finishReason = "tool_calls"
			} else if malformed {
				msg.Content = MalformedContentToolCallErrorMsg
				finishReason = "stop"
			}
		}
		finishReason, truncatedTools := filterStreamTruncatedToolCalls(&msg, finishReason)
		result.Choices = append(result.Choices, Choice{
			Message:            msg,
			FinishReason:       finishReason,
			TruncatedToolNames: truncatedTools,
		})
	}
	return result
}

func ParseNonStreamOpenAIResponseBody(body []byte) (*Response, error) {
	trimmed := bytes.TrimLeft(body, " \t\r\n")
	if bytes.HasPrefix(trimmed, []byte("data:")) {
		return ParseSSEToResponse(body)
	}

	var wire openAIWireResponse
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return projectOpenAIWireResponse(wire), nil
}

func ParseNonStreamOpenAIResponse(resp *http.Response) (*Response, error) {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("llm error: status=%d body_len=%d", resp.StatusCode, len(body))
	}
	return ParseNonStreamOpenAIResponseBody(body)
}

// htmlStripRe matches HTML tags for sanitization.
var htmlStripRe = regexp.MustCompile(`<[^>]*>`)

// sanitizeHTMLBody strips HTML tags from body if it looks like HTML, then truncates.
func sanitizeHTMLBody(body []byte, maxLen int) string {
	s := string(body)
	lower := strings.ToLower(s)
	if strings.Contains(lower, "<html") || strings.Contains(lower, "<!doctype") ||
		strings.Contains(lower, "<center>") || strings.Contains(lower, "<head>") {
		s = htmlStripRe.ReplaceAllString(s, " ")
		s = strings.Join(strings.Fields(s), " ")
		s = strings.TrimSpace(s)
	}
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}
