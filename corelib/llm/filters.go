package llm

import (
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// Simple stateless filters
// ---------------------------------------------------------------------------

var (
	reThinkBlock         = regexp.MustCompile(`(?s)<think>.*?</think>\\s*`)
	reDetailsBlock       = regexp.MustCompile(`(?is)<details\b[^>]*>.*?</details>\s*`)
	reSummaryBlock       = regexp.MustCompile(`(?is)<summary\b[^>]*>.*?</summary>\s*`)
	reFuncCallBlock      = regexp.MustCompile(`(?s)<\\|FunctionCallBegin\\|>.*?<\\|FunctionCallEnd\\|>\\s*`)
	reToolCallBlock      = regexp.MustCompile(`(?is)<tool_call(?:\[\])?\b[^>]*>.*?</tool_call>\s*`)
	reToolCallOpenToEnd  = regexp.MustCompile(`(?is)<tool_call(?:\[\])?\b[^>]*>.*\z`)
	reCodexToolCallBlock = regexp.MustCompile(`(?s)<turn:\s*tool_call\s*>.*?</turn>\s*`)
	rePlainToolCallTail  = regexp.MustCompile(`(?is)\bTOOL_CALL\b\s*\{.*\}\s*`)
)

func StripThinkTags(s string) string {
	return strings.TrimSpace(reThinkBlock.ReplaceAllString(s, ""))
}

func StripDetailsBlocks(s string) string {
	s = reDetailsBlock.ReplaceAllString(s, "")
	s = reSummaryBlock.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

func StripFunctionCalls(s string) string {
	return strings.TrimSpace(reFuncCallBlock.ReplaceAllString(s, ""))
}

func StripXMLToolCalls(s string) string {
	s = reToolCallBlock.ReplaceAllString(s, "")
	s = reToolCallOpenToEnd.ReplaceAllString(s, "")
	s = reCodexToolCallBlock.ReplaceAllString(s, "")
	s = rePlainToolCallTail.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

func StripAllExtra(s string) string {
	return StripXMLToolCalls(StripFunctionCalls(StripDetailsBlocks(StripThinkTags(s))))
}
