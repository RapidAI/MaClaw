package main

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// ---------------------------------------------------------------------------
// Coding Tool Gate — code-level enforcement of the three-phase coding workflow.
//
// When the intent classifier returns intentCoding and the user has not sent a
// skip signal, the gate strips coding tool calls (create_session, bash, etc.)
// from ALL iterations of the LLM response loop, preserving text content and
// delivery tools (generate_pdf, send_file, etc.).
//
// The gate remains active for the entire agent loop (all iterations within a
// single user message). The user's confirmation arrives as a separate message,
// triggering a new loop where the gate is re-evaluated. On iteration 1+, if
// all coding tools are stripped and the LLM has already produced text (the
// requirements doc), the loop force-returns the response for user confirmation
// instead of continuing.
//
// This acts as a hard backstop for the system-prompt HARD GATE constraint
// that the LLM sometimes ignores.
// ---------------------------------------------------------------------------

// codingToolGateConfig holds the pre-computed gate decision for the loop.
type codingToolGateConfig struct {
	active     bool       // gate is active (intentCoding && !skip && !background)
	intent     taskIntent // cached intent classification result
	skipSignal bool       // user message contains a skip signal
	reason     string     // human-readable gate decision reason (for logging)
}

// codingToolGateResult holds the result of applying the gate to tool calls.
type codingToolGateResult struct {
	stripped  []llm.ToolCall // coding tool calls that were removed
	remaining []llm.ToolCall // preserved tool calls (delivery tools, etc.)
	applied   bool           // true if any tools were actually stripped
}

// codingToolBlocklist lists tool names subject to stripping when the gate is active.
var codingToolBlocklist = map[string]bool{
	"create_session":  true,
	"bash":            true,
	"write_file":      true,
	"edit_file":       true,
	"craft_tool":      true,
	"send_and_observe": true,
	"control_session": true,
}

// deliveryToolAllowlist lists tool names that are never intercepted.
var deliveryToolAllowlist = map[string]bool{
	"generate_pdf":  true,
	"send_file":     true,
	"memory":        true,
	"open":          true,
	"set_nickname":  true,
	"manage_config": true,
	"ask_user":      true,
	"task":          true,
}

// skipSignalsChinese contains Chinese phrases that bypass the gate.
var skipSignalsChinese = []string{
	"直接做", "直接用", "不用问了", "按你的想法来", "直接开始",
	"不用确认", "马上做", "赶紧做", "跳过文档", "不需要文档",
	// Action/continuation phrases — user wants to start working on an
	// already-discussed task, not go through the three-phase workflow again.
	"开工", "开干", "动手", "搞起来", "搞起", "干吧", "做吧",
	"开始吧", "开始做", "开始干", "开始搞",
}

// skipSignalsEnglish contains English phrases that bypass the gate.
var skipSignalsEnglish = []string{
	"just do it", "skip confirmation", "go ahead", "do it now",
	"let's go", "let's do it", "let's start", "let's begin",
}

// isCodingTool returns true iff name is in the blocklist and not in the allowlist.
func isCodingTool(name string) bool {
	return codingToolBlocklist[name] && !deliveryToolAllowlist[name]
}

// containsSkipSignal checks whether text contains any known skip signal
// (Chinese or English) using case-insensitive substring matching.
func containsSkipSignal(text string) bool {
	lower := strings.ToLower(text)
	// Chinese signals: no case variation, but ToLower on text handles mixed-language input.
	for _, sig := range skipSignalsChinese {
		if strings.Contains(lower, sig) {
			return true
		}
	}
	// English signals are already lowercase constants.
	for _, sig := range skipSignalsEnglish {
		if strings.Contains(lower, sig) {
			return true
		}
	}
	return false
}

// newCodingToolGateConfig computes the gate decision once before the iteration
// loop begins. The result is immutable for the duration of the loop.
func newCodingToolGateConfig(userText string, loopKind LoopKind) codingToolGateConfig {
	return newCodingToolGateConfigWithClassifier(userText, loopKind, nil)
}

// newCodingToolGateConfigWithClassifier is like newCodingToolGateConfig but
// accepts an optional IntentClassifier for semantic refinement.
func newCodingToolGateConfigWithClassifier(userText string, loopKind LoopKind, ic *tool.IntentClassifier) codingToolGateConfig {
	result := classifyTaskIntent(userText)
	skip := containsSkipSignal(userText)

	// Semantic refinement: when keyword-based classification says "coding" but
	// the IntentClassifier says "query" (knowledge question), override to
	// non-coding so the gate doesn't strip tools for a conceptual question.
	// Only invoke the classifier when it's ready (anchors warmed up).
	if result.Intent == intentCoding && !skip && ic != nil && ic.Ready() {
		icResult := ic.Classify(userText)
		if icResult.Intent == tool.IntentQuery {
			result.Intent = intentUnknown
		}
	}

	cfg := codingToolGateConfig{
		intent:     result.Intent,
		skipSignal: skip,
	}

	switch {
	case loopKind == LoopKindBackground:
		cfg.reason = "gate inactive: background loop"
	case result.Intent != intentCoding:
		cfg.reason = fmt.Sprintf("gate inactive: intent=%s", result.Intent)
	case skip:
		cfg.reason = "gate inactive: skip signal detected"
	default:
		cfg.active = true
		cfg.reason = "gate active: intentCoding, no skip signal, chat loop"
	}
	return cfg
}

// applyCodingToolGate partitions tool calls into stripped (coding) and
// remaining (non-coding). Order of remaining calls is preserved.
func applyCodingToolGate(calls []llm.ToolCall) codingToolGateResult {
	if len(calls) == 0 {
		return codingToolGateResult{}
	}
	stripped := make([]llm.ToolCall, 0, len(calls))
	remaining := make([]llm.ToolCall, 0, len(calls))
	for _, tc := range calls {
		if isCodingTool(tc.Function.Name) {
			stripped = append(stripped, tc)
		} else {
			remaining = append(remaining, tc)
		}
	}
	return codingToolGateResult{
		stripped:  stripped,
		remaining: remaining,
		applied:   len(stripped) > 0,
	}
}
