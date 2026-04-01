package llm

import (
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// Simple stateless filters
// ---------------------------------------------------------------------------

var (
	reThinkBlock    = regexp.MustCompile(`(?s)<think>.*?</think>\\s*`)
	reFuncCallBlock = regexp.MustCompile(`(?s)<\\|FunctionCallBegin\\|>.*?<\\|FunctionCallEnd\\|>\\s*`)
	reToolCallBlock = regexp.MustCompile(`(?s)<tool_call>.*?</tool_call>\\s*`)
)

func StripThinkTags(s string) string {
	return strings.TrimSpace(reThinkBlock.ReplaceAllString(s, ""))
}

func StripFunctionCalls(s string) string {
	return strings.TrimSpace(reFuncCallBlock.ReplaceAllString(s, ""))
}

func StripXMLToolCalls(s string) string {
	return strings.TrimSpace(reToolCallBlock.ReplaceAllString(s, ""))
}

func StripAllExtra(s string) string {
	return StripXMLToolCalls(StripFunctionCalls(StripThinkTags(s)))
}
