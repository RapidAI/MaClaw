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
}

type responsesAPIContentPart struct {
	Type string `json:"type"` // "output_text"
	Text string `json:"text"`
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
				if part.Type == "output_text" {
					textParts = append(textParts, part.Text)
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
		}
	}

	msg.Content = StripAllExtra(strings.Join(textParts, ""))

	finishReason := "stop"
	if len(msg.ToolCalls) > 0 {
		finishReason = "tool_calls"
	}

	return &Response{
		Choices: []Choice{{Message: msg, FinishReason: finishReason}},
		Usage:   wire.Usage,
	}, nil
}

// ParseNonStreamResponsesAPIResponse reads and parses a non-streaming
// Responses API HTTP response into the internal Response type.
func ParseNonStreamResponsesAPIResponse(resp *http.Response) (*Response, error) {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("llm error: status=%d body=%s", resp.StatusCode, string(body))
	}
	return ParseNonStreamResponsesAPIBody(body)
}
