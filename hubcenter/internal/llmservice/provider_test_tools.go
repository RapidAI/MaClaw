package llmservice

import (
	"bytes"
	"encoding/json"
	"strings"
)

// ProviderToolCall is one tool call returned by a member probe.
type ProviderToolCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments,omitempty"`
}

// ParseProviderTestToolCalls reads OpenAI tool_calls, Anthropic tool_use blocks,
// and Responses API function_call items.
func ParseProviderTestToolCalls(body []byte) []ProviderToolCall {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	var envelope struct {
		Choices []struct {
			Message struct {
				ToolCalls []struct {
					Function struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Content []struct {
			Type  string          `json:"type"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
		Output []struct {
			Type      string          `json:"type"`
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		} `json:"output"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil
	}
	var out []ProviderToolCall
	if len(envelope.Choices) > 0 {
		for _, call := range envelope.Choices[0].Message.ToolCalls {
			name := strings.TrimSpace(call.Function.Name)
			if name == "" {
				continue
			}
			out = append(out, ProviderToolCall{Name: name, Arguments: toolCallArguments(call.Function.Arguments)})
		}
	}
	for _, block := range envelope.Content {
		if !strings.EqualFold(strings.TrimSpace(block.Type), "tool_use") {
			continue
		}
		name := strings.TrimSpace(block.Name)
		if name == "" {
			continue
		}
		out = append(out, ProviderToolCall{Name: name, Arguments: toolCallArguments(block.Input)})
	}
	for _, item := range envelope.Output {
		if !strings.EqualFold(strings.TrimSpace(item.Type), "function_call") {
			continue
		}
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		out = append(out, ProviderToolCall{Name: name, Arguments: toolCallArguments(item.Arguments)})
	}
	return out
}

func toolCallArguments(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	return string(raw)
}
