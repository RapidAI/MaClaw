package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/llm"
)

var xmlToolCallBlockRe = regexp.MustCompile(`(?s)<tool_call>\s*(.*?)\s*</tool_call>`)

// parseXMLContentToolCalls extracts tool calls emitted as XML tags in the
// content field by models that do not stream structured delta.tool_calls.
func parseXMLContentToolCalls(content string) []llm.ToolCall {
	matches := xmlToolCallBlockRe.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return nil
	}

	var calls []llm.ToolCall
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		call, ok := parseXMLToolCallPayload(strings.TrimSpace(m[1]))
		if ok {
			calls = append(calls, call)
		}
	}
	return calls
}

func parseXMLToolCallPayload(raw string) (llm.ToolCall, bool) {
	var parsed struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return llm.ToolCall{}, false
	}
	if strings.TrimSpace(parsed.Name) == "" {
		return llm.ToolCall{}, false
	}

	args := strings.TrimSpace(string(parsed.Arguments))
	if args == "" {
		args = "{}"
	}

	return llm.ToolCall{
		ID:   randomXMLToolCallID(),
		Type: "function",
		Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{
			Name:      parsed.Name,
			Arguments: args,
		},
	}, true
}

func randomXMLToolCallID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return fmt.Sprintf("call_%x", b)
}
