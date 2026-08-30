package llm

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ---------------------------------------------------------------------------
// Responses API response wire types (unexported)
// ---------------------------------------------------------------------------

type responsesAPIResponse struct {
	ID     string                   `json:"id"`
	Output []responsesAPIOutputItem `json:"output"`
	Usage  *Usage                   `json:"usage,omitempty"`
}

type responsesAPIOutputItem struct {
	Type      string                    `json:"type"` // "message" or "function_call"
	Role      string                    `json:"role,omitempty"`
	Content   []responsesAPIContentPart `json:"content,omitempty"`
	CallID    string                    `json:"call_id,omitempty"`
	Name      string                    `json:"name,omitempty"`
	Arguments string                    `json:"arguments,omitempty"`
	// Summary is the user-displayable reasoning summary returned by Responses
	// reasoning items. It is intentionally distinct from private chain of thought.
	Summary []responsesAPIContentPart `json:"summary,omitempty"`
}

type responsesAPIReasoningOutputItem struct {
	Type    string                    `json:"type"`
	Summary []responsesAPIContentPart `json:"summary,omitempty"`
	Content []responsesAPIContentPart `json:"content,omitempty"`
}

// DisplaySummary returns the display-safe summary included by a Responses API
// provider. It never attempts to derive or request private chain-of-thought.
func (item responsesAPIReasoningOutputItem) DisplaySummary() string {
	switch strings.ToLower(strings.TrimSpace(item.Type)) {
	case "reasoning", "analysis", "reasoning_summary":
	default:
		return ""
	}
	parts := item.Summary
	if len(parts) == 0 {
		parts = item.Content
	}
	var out strings.Builder
	for _, part := range parts {
		switch part.Type {
		case "summary_text", "reasoning_text", "reasoning_content", "text", "output_text", "thinking":
			text := part.Text
			if text == "" {
				text = part.Content
			}
			out.WriteString(text)
		}
	}
	return out.String()
}

type responsesAPIContentPart struct {
	Type string `json:"type"` // "output_text" or provider-compatible text part
	Text string `json:"text"`
	// Some OpenAI-compatible Responses endpoints emit text under content rather
	// than text. Accept both so a valid vision reply cannot be misclassified as
	// an empty response.
	Content string `json:"content"`
}

type responsesAPIUsageEvent struct {
	Usage    *Usage `json:"usage,omitempty"`
	Response struct {
		Usage *Usage `json:"usage,omitempty"`
	} `json:"response,omitempty"`
}

// ExtractResponsesAPIUsageFromEventPayload extracts usage from Responses API
// streaming event payloads. Current providers usually place usage under
// response.usage on response.completed, while some proxies expose it at the
// top level.
func ExtractResponsesAPIUsageFromEventPayload(payload []byte) *Usage {
	var event responsesAPIUsageEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return nil
	}
	if event.Response.Usage != nil {
		return event.Response.Usage
	}
	return event.Usage
}

// ---------------------------------------------------------------------------
// Parsers
// ---------------------------------------------------------------------------

// ParseNonStreamResponsesAPIBody parses a Responses API JSON body into the
// internal Response type.
func ParseNonStreamResponsesAPIBody(body []byte) (*Response, error) {
	var wire responsesAPIResponse
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	msg := Message{Role: "assistant"}
	var textParts []string

	for _, item := range wire.Output {
		switch item.Type {
		case "message":
			for _, part := range item.Content {
				switch part.Type {
				case "output_text", "text", "input_text":
					if text := strings.TrimSpace(part.Text); text != "" {
						textParts = append(textParts, text)
					} else if content := strings.TrimSpace(part.Content); content != "" {
						textParts = append(textParts, content)
					}
				}
			}
		case "function_call":
			msg.ToolCalls = append(msg.ToolCalls, ToolCall{
				ID:   item.CallID,
				Type: "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{
					Name:      item.Name,
					Arguments: item.Arguments,
				},
			})
		case "reasoning", "analysis", "reasoning_summary":
			reasoningItem := responsesAPIReasoningOutputItem{
				Type:    item.Type,
				Summary: item.Summary,
				Content: item.Content,
			}
			if summary := reasoningItem.DisplaySummary(); summary != "" {
				// Separate summaries from distinct reasoning items, mirroring
				// appendResponsesStreamReasoning's item separation.
				if msg.ReasoningContent != "" {
					msg.ReasoningContent += "\n"
				}
				msg.ReasoningContent += summary
			}
		}
	}

	msg.Content = StripAllExtra(strings.Join(textParts, ""))

	finishReason := "stop"
	if len(msg.ToolCalls) > 0 {
		finishReason = "tool_calls"
	}
	finishReason, truncatedTools, truncatedToolArgs := filterStreamTruncatedToolCalls(&msg, finishReason)

	return &Response{
		ResponseID: strings.TrimSpace(wire.ID),
		Choices:    []Choice{{Message: msg, FinishReason: finishReason, TruncatedToolNames: truncatedTools, TruncatedToolArgs: truncatedToolArgs}},
		Usage:      wire.Usage,
	}, nil
}

// ParseNonStreamResponsesAPIResponse reads and parses a non-streaming
// Responses API HTTP response into the internal Response type.
func ParseNonStreamResponsesAPIResponse(resp *http.Response) (*Response, error) {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if resp.StatusCode != http.StatusOK {
		return nil, newHTTPStatusError(resp.StatusCode, body)
	}
	return ParseNonStreamResponsesAPIBody(body)
}
