package main

import (
	"fmt"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// ---------------------------------------------------------------------------
// Coding Tool Gate: code-level enforcement of the three-phase coding workflow.
//
// When the gate intent classifier returns a confirmed new-project intent, the
// gate strips coding tool calls (create_session, bash, etc.) from all
// iterations of the LLM response loop, preserving text content and delivery
// tools (generate_pdf, send_file, etc.). Bug-fix, maintenance, continuation,
// and non-coding decisions come from the classifier result, not local keyword
// helpers.

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
	active     bool       // gate is active (intentCoding && !skip && !background && !bugfix)
	intent     taskIntent // cached intent classification result
	skipSignal bool       // user message contains a skip signal
	bugFix     bool       // true when intent is coding but task is bug-fix/debug (no three-phase needed)
	reason     string     // human-readable gate decision reason (for logging)
}

// codingToolGateResult holds the result of applying the gate to tool calls.
type codingToolGateResult struct {
	stripped  []llm.ToolCall // coding tool calls that were removed
	remaining []llm.ToolCall // preserved tool calls (delivery tools, etc.)
	applied   bool           // true if any tools were actually stripped
}

// codingToolBlocklist lists tool names subject to stripping when the gate is
// active during the three-phase workflow (requirements, design, task breakdown).
// This includes coding session tools, direct coding tools, AND browser automation
// tools; during the three-phase phases, none of these should be available.
var codingToolBlocklist = map[string]bool{
	// Coding session tools.
	"create_session":   true,
	"bash":             true,
	"write_file":       true,
	"edit_file":        true,
	"edit_lines":       true,
	"craft_tool":       true,
	"send_and_observe": true,
	"control_session":  true,
	// Browser automation tools; the unified "browser" tool replaces 22
	// individual browser_* tools. Block it plus the remaining individual tools.
	"browser":          true,
	"browser_task_run": true, "browser_task_replay": true, "browser_task_verify": true, "browser_task_status": true,
	"browser_record_start": true, "browser_record_stop": true, "browser_list_flows": true,
	"browser_ocr":      true,
	"gui_record_start": true, "gui_record_stop": true,
	"gui_observe": true, "gui_verify": true,
}

// directModeSessionBlocklist lists session management tools that should be
// stripped during the execution phase when using direct coding mode. In direct
// mode, maclaw writes code itself using bash/write_file/edit_file; external
// session tools are unnecessary and their presence confuses the LLM into
// trying to delegate instead of coding directly.
var directModeSessionBlocklist = map[string]bool{
	"create_session":     true,
	"send_and_observe":   true,
	"control_session":    true,
	"get_session_output": true,
	"get_session_events": true,
	"interrupt_session":  true,
	"kill_session":       true,
	"list_sessions":      true,
	"send_input":         true,
}

// deliveryToolAllowlist lists tool names that are never intercepted.
var deliveryToolAllowlist = map[string]bool{
	"generate_pdf":  true,
	"office":        true,
	"send_file":     true,
	"memory":        true,
	"open":          true,
	"set_nickname":  true,
	"manage_config": true,
	"ask_user":      true,
	"task":          true,
}

// isCodingTool returns true iff name is in the blocklist and not in the allowlist.
func isCodingTool(name string) bool {
	return codingToolBlocklist[name] && !deliveryToolAllowlist[name]
}

// isDirectModeBlockedTool returns true if the tool should be stripped during
// the execution phase when using direct coding mode.
func isDirectModeBlockedTool(name string) bool {
	return directModeSessionBlocklist[name]
}

// mapGateIntentToConfig maps a GateIntentResult from the semantic classifier
// to a codingToolGateConfig. This is the bridge between the five-category
// semantic classification and the gate's active/bugFix decision.
//
// Mapping rules:
//   - accepted new_project => active=true, intent=intentCoding
//   - bug_fix => active=false, bugFix=true, intent=intentCoding
//   - maintenance => active=false, intent=intentCoding
//   - non_coding => active=false, intent=intentNonCoding
//   - continuation => active=false, intent=intentUnknown
//   - unknown or low confidence => active=false, intent=intentAmbiguous
func mapGateIntentToConfig(result GateIntentResult, skip bool) codingToolGateConfig {
	cfg := codingToolGateConfig{
		skipSignal: skip,
	}

	if !shouldAcceptGateResult(result) {
		cfg.active = false
		cfg.intent = intentAmbiguous
		cfg.reason = fmt.Sprintf("gate inactive: inconclusive classifier result; ordinary agent path (intent=%s, conf=%.2f, degraded=%v)",
			result.Intent, result.Confidence, result.Degraded)
		return cfg
	}

	if skip {
		cfg.reason = fmt.Sprintf("gate inactive: continuation/skip (classifier: %s, conf=%.2f)", result.Intent, result.Confidence)
		return cfg
	}

	switch result.Intent {
	case GateIntentNewProject:
		cfg.active = true
		cfg.intent = intentCoding
		cfg.reason = fmt.Sprintf("gate active: semantic new_project (conf=%.2f, layer=%d, degraded=%v)",
			result.Confidence, result.Layer, result.Degraded)
	case GateIntentBugFix:
		cfg.bugFix = true
		cfg.intent = intentCoding
		cfg.reason = fmt.Sprintf("gate inactive: semantic bug_fix (conf=%.2f)", result.Confidence)
	case GateIntentMaintenance:
		cfg.intent = intentCoding
		cfg.reason = fmt.Sprintf("gate inactive: semantic maintenance (conf=%.2f)", result.Confidence)
	case GateIntentNonCoding:
		cfg.intent = intentNonCoding
		cfg.reason = fmt.Sprintf("gate inactive: semantic non_coding (conf=%.2f)", result.Confidence)
	case GateIntentContinuation:
		cfg.intent = intentUnknown
		cfg.reason = fmt.Sprintf("gate inactive: semantic continuation (conf=%.2f)", result.Confidence)
	default:
		cfg.active = false
		cfg.intent = intentAmbiguous
		cfg.reason = fmt.Sprintf("gate inactive: unsupported classifier result; ordinary agent path (intent=%s, conf=%.2f)",
			result.Intent, result.Confidence)
	}

	return cfg
}

// newCodingToolGateConfig computes the gate decision once before the iteration
// loop begins. The result is immutable for the duration of the loop.
func newCodingToolGateConfig(userText string, loopKind LoopKind) codingToolGateConfig {
	return newCodingToolGateConfigWithClassifier(userText, loopKind, nil, nil, "")
}

// newCodingToolGateConfigWithClassifier is like newCodingToolGateConfig but
// accepts an optional GateIntentClassifier for semantic classification and
// a userID for conversation context lookup.
//
// Classification priority:
//  1. GateIntentClassifier (semantic, delegates to UIC when available)
//  2. UIC directly (when GIC not ready but UIC is available)
//  3. Ordinary-agent fallback (classifiers unexpectedly nil)
//
// Since initEarlyClassifier creates UIC synchronously at app startup,
// path 2 (UIC direct) is always available from the first user message.
// Path 3 is a safety net for test code or edge cases where no App exists.
func newCodingToolGateConfigWithClassifier(userText string, loopKind LoopKind, gic *GateIntentClassifier, uic *intent.UnifiedIntentClassifier, userID string) codingToolGateConfig {
	// Background loop always bypasses the gate, regardless of classification.
	if loopKind == LoopKindBackground {
		return codingToolGateConfig{
			reason: "gate inactive: background loop",
		}
	}

	// Try semantic classification first when classifier is available.
	// GIC.Classify() delegates to UIC when available, so we don't need to
	// check Ready() here.
	if gic != nil {
		result := gic.Classify(userText, userID)
		skip := result.Intent == GateIntentContinuation
		cfg := mapGateIntentToConfig(result, skip)
		return cfg
	}

	// Fallback: GIC is nil (test code or edge case without App).
	// Try UIC directly via the package-level variable.
	if uic == nil {
		uic = unifiedClassifier
	}
	if uic != nil {
		uicResult := uic.Classify(intent.MessageContext{Text: userText})
		gateIntent, confidence, _, layer, reason := uicResult.ToGateIntent()
		gateResult := GateIntentResult{
			Intent:     GateIntent(gateIntent),
			Confidence: confidence,
			Layer:      layer,
			Reason:     reason,
			Degraded:   uicResult.Degraded,
		}
		skip := gateResult.Intent == GateIntentContinuation
		cfg := mapGateIntentToConfig(gateResult, skip)
		return cfg
	}

	// Neither GIC nor UIC available; should not happen in normal operation
	// because initEarlyClassifier creates UIC synchronously at app startup.
	// This path is a safety net for edge cases (e.g., test code that creates
	// an IMMessageHandler without an App).
	//
	// Do not infer workflow intent from classifier absence. Let the ordinary
	// agent path proceed; high-risk actions are governed by their own tool and
	// route checks.
	return codingToolGateConfig{
		intent: intentUnknown,
		reason: "gate inactive: classifiers unavailable; ordinary agent path",
	}
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
