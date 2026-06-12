package main

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// substantialWorkflowDoc generates a test document long enough to pass
// isSubstantivePhaseDocument (>= 200 runes).
func substantialWorkflowDoc(phaseLabel string) string {
	return "# " + phaseLabel + " Document\n\n" + strings.Repeat("This is a substantial workflow document for testing purposes. ", 10)
}

// toolCallNamed creates an llm.ToolCall with the given function name for testing.
func toolCallNamed(name string) llm.ToolCall {
	return llm.ToolCall{
		ID:       "call_test_" + name,
		Type:     "function",
		Function: llm.ToolCallFunction{Name: name, Arguments: "{}"},
	}
}
