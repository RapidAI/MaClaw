package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// Shared LLM types for both stream and non-stream responses

type Response struct {
	// ResponseID is the provider-issued correlation for this concrete model
	// response. Hosts may use it to bind a request-local dynamic tool surface;
	// it is never derived from tool-call arguments or a local loop ID.
	ResponseID    string   `json:"id,omitempty"`
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

	// TruncatedToolArgs maps tool name to its raw (incomplete) JSON argument
	// string for truncated tool calls. This allows the agent loop to attempt
	// best-effort partial execution (e.g. extracting path and partial content
	// from a truncated write_file call and writing the partial content to disk).
	// This is NOT serialized — it is an in-process signal only.
	TruncatedToolArgs map[string]string `json:"-"`
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

	// InputReported / OutputReported are true when the provider payload
	// contained that directional leg (including an explicit zero). Hosts must
	// not locally estimate a reported zero; that would diverge from HubCenter.
	InputReported  bool `json:"-"`
	OutputReported bool `json:"-"`
}

func (u *Usage) UnmarshalJSON(data []byte) error {
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	*u = usageFromFields(corelib.ParseLLMUsageFields(payload))
	return nil
}

func usageFromFields(fields corelib.LLMUsageFields) Usage {
	u := Usage{
		PromptTokens:      int(fields.Input),
		CompletionTokens:  int(fields.Output),
		TotalTokens:       int(fields.Total),
		InputTokens:       int(fields.Input),
		OutputTokens:      int(fields.Output),
		CachedInputTokens: int(fields.Cached),
		CacheWriteTokens:  int(fields.Written),
		InputReported:     fields.InputObserved,
		OutputReported:    fields.OutputObserved,
	}
	if u.TotalTokens <= 0 {
		u.TotalTokens = u.PromptTokens + u.CompletionTokens
	}
	return u
}

// usageFromPositiveCounts maps SDK typed counters onto the shared parser.
// Typed OpenAI structs use 0 for both "missing" and "zero", so only positive
// legs are passed through; explicit zeros must come from raw JSON instead.
func usageFromPositiveCounts(prompt, completion, total int64) *Usage {
	m := make(map[string]interface{}, 3)
	if prompt > 0 {
		m["prompt_tokens"] = prompt
	}
	if completion > 0 {
		m["completion_tokens"] = completion
	}
	if total > 0 {
		m["total_tokens"] = total
	}
	if len(m) == 0 {
		return nil
	}
	u := usageFromFields(corelib.ParseLLMUsageFields(m))
	return &u
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
	// Some OpenAI-compatible providers use these aliases instead of
	// reasoning_content. They are display-safe provider output and are kept in
	// the same lane as the canonical field.
	Reasoning    string     `json:"reasoning,omitempty"`
	Thinking     string     `json:"thinking,omitempty"`
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
	FunctionCall *struct {
		Name      string          `json:"name,omitempty"`
		Arguments json.RawMessage `json:"arguments,omitempty"`
	} `json:"function_call,omitempty"`
}

func openAIReasoningText(reasoningContent, reasoning, thinking string) string {
	if reasoningContent != "" {
		return reasoningContent
	}
	if reasoning != "" {
		return reasoning
	}
	return thinking
}

// openAIThinkTagPattern accepts the common provider convention of placing
// reasoning in <think> blocks within the normal OpenAI-compatible content
// field. The unclosed form matters when an upstream response is truncated.
var openAIThinkTagPattern = regexp.MustCompile(`(?si)<think>.*?</think>|<think>.*$`)

func splitOpenAIThinkingTags(content string) (visible string, reasoning string) {
	if !strings.Contains(strings.ToLower(content), "<think>") {
		return strings.TrimSpace(content), ""
	}

	var parts []string
	visible = openAIThinkTagPattern.ReplaceAllStringFunc(content, func(match string) string {
		body := match
		lower := strings.ToLower(body)
		if strings.HasPrefix(lower, "<think>") {
			body = body[len("<think>"):]
			lower = lower[len("<think>"):]
		}
		if end := strings.LastIndex(lower, "</think>"); end >= 0 {
			body = body[:end]
		}
		if text := strings.TrimSpace(body); text != "" {
			parts = append(parts, text)
		}
		return ""
	})
	return strings.TrimSpace(visible), strings.Join(parts, "\n")
}

func mergeOpenAIReasoningText(explicit, tagged string) string {
	if strings.TrimSpace(explicit) == "" {
		explicit = ""
	}
	if strings.TrimSpace(tagged) == "" {
		tagged = ""
	}
	switch {
	case explicit == "":
		return tagged
	case tagged == "", explicit == tagged, strings.Contains(explicit, tagged):
		return explicit
	case strings.Contains(tagged, explicit):
		return tagged
	default:
		return explicit + "\n" + tagged
	}
}

// TokenCallback is called with each text delta from the LLM streaming response.
type TokenCallback func(delta string)

// Anthropic specific structures

type AnthropicContentBlock struct {
	Type     string      `json:"type"`
	Text     string      `json:"text,omitempty"`
	Thinking string      `json:"thinking,omitempty"`
	ID       string      `json:"id,omitempty"`
	Name     string      `json:"name,omitempty"`
	Input    interface{} `json:"input,omitempty"`
}

// ParseNonStreamAnthropicResponse handles the fallback case where the provider
// returned a normal JSON response instead of SSE for Anthropic protocol.
func ParseNonStreamAnthropicResponse(resp *http.Response) (*Response, error) {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if resp.StatusCode != http.StatusOK {
		return nil, newHTTPStatusError(resp.StatusCode, body)
	}
	return parseAnthropicResponseBody(body)
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
	result := &Response{ResponseID: strings.TrimSpace(wire.ID), Usage: wire.Usage}
	if len(wire.Choices) == 0 {
		return result
	}
	result.Choices = make([]Choice, 0, len(wire.Choices))
	for _, choice := range wire.Choices {
		content, rawContent := normalizeOpenAIMessageContent(choice.Message.Content)
		visibleContent, taggedReasoning := splitOpenAIThinkingTags(content)
		msg := Message{
			Role:             choice.Message.Role,
			Content:          StripAllExtra(visibleContent),
			ReasoningContent: mergeOpenAIReasoningText(openAIReasoningText(choice.Message.ReasoningContent, choice.Message.Reasoning, choice.Message.Thinking), taggedReasoning),
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
			if contentCalls, malformed := ParseContentToolCallsDetailed(visibleContent); len(contentCalls) > 0 {
				msg.ToolCalls = append(msg.ToolCalls, contentCalls...)
				msg.Content = ""
				finishReason = "tool_calls"
			} else if malformed {
				msg.Content = MalformedContentToolCallErrorMsg
				finishReason = "stop"
			}
		}
		finishReason, truncatedTools, truncatedToolArgs := filterStreamTruncatedToolCalls(&msg, finishReason)
		result.Choices = append(result.Choices, Choice{
			Message:            msg,
			FinishReason:       finishReason,
			TruncatedToolNames: truncatedTools,
			TruncatedToolArgs:  truncatedToolArgs,
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
		return nil, newHTTPStatusError(resp.StatusCode, body)
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
