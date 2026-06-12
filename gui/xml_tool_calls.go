package main

import (
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// parseXMLContentToolCalls extracts tool calls emitted as XML tags in the
// content field by models that do not stream structured delta.tool_calls.
func parseXMLContentToolCalls(content string) []llm.ToolCall {
	calls, _ := parseXMLContentToolCallsDetailed(content)
	return calls
}

func parseXMLContentToolCallsDetailed(content string) ([]llm.ToolCall, bool) {
	return llm.ParseContentToolCallsDetailed(content)
}
