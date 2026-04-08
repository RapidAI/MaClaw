package freeproxy

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// ToolCall represents an OpenAI-compatible tool call extracted from model output.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

// ToolFunction holds the function name and arguments of a tool call.
type ToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

var toolCallBlockRe = regexp.MustCompile("(?s)```tool_call\\s*\\n?(.*?)\\n?```")

// ParseToolCalls extracts tool calls from model output that uses the
// ```tool_call ... ``` code block convention (same as dangbei-api-deployment).
func ParseToolCalls(content string) []ToolCall {
	matches := toolCallBlockRe.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return nil
	}
	var calls []ToolCall
	for i, m := range matches {
		if len(m) < 2 {
			continue
		}
		if call, ok := parseToolCallPayload(strings.TrimSpace(m[1]), i); ok {
			calls = append(calls, call)
		}
	}
	return calls
}

// RemoveToolCallBlocks strips ```tool_call ... ``` blocks from content.
func RemoveToolCallBlocks(content string) string {
	return strings.TrimSpace(toolCallBlockRe.ReplaceAllString(content, ""))
}

// HasToolCalls checks whether content contains tool call blocks.
func HasToolCalls(content string) bool {
	return strings.Contains(content, "```tool_call")
}

// xmlToolCallBlockRe matches <tool_call>...</tool_call> blocks used by small
// models (e.g. xiaomi/mimo-v2-pro) that emit tool calls as XML tags in the
// content field instead of structured delta.tool_calls.
var xmlToolCallBlockRe = regexp.MustCompile(`(?s)<tool_call>\s*(.*?)\s*</tool_call>`)

// ParseXMLToolCalls extracts tool calls from model output that uses the
// <tool_call>JSON</tool_call> XML tag convention.
func ParseXMLToolCalls(content string) []ToolCall {
	matches := xmlToolCallBlockRe.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return nil
	}
	var calls []ToolCall
	for i, m := range matches {
		if len(m) < 2 {
			continue
		}
		if call, ok := parseToolCallPayload(strings.TrimSpace(m[1]), i); ok {
			calls = append(calls, call)
		}
	}
	return calls
}

// RemoveXMLToolCallBlocks strips <tool_call>...</tool_call> blocks from content.
func RemoveXMLToolCallBlocks(content string) string {
	return strings.TrimSpace(xmlToolCallBlockRe.ReplaceAllString(content, ""))
}

// HasXMLToolCalls checks whether content contains XML tool call blocks.
func HasXMLToolCalls(content string) bool {
	return strings.Contains(content, "<tool_call>")
}

func parseToolCallPayload(raw string, index int) (ToolCall, bool) {
	var parsed struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return ToolCall{}, false
	}
	if strings.TrimSpace(parsed.Name) == "" {
		return ToolCall{}, false
	}
	args := strings.TrimSpace(string(parsed.Arguments))
	if args == "" {
		args = "{}"
	}
	return ToolCall{
		ID:   generateToolCallID(index),
		Type: "function",
		Function: ToolFunction{
			Name:      parsed.Name,
			Arguments: args,
		},
	}, true
}

func generateToolCallID(_ int) string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return fmt.Sprintf("call_%x", b)
}
