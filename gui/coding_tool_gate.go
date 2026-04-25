package main

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// ---------------------------------------------------------------------------
// Coding Tool Gate — code-level enforcement of the three-phase coding workflow.
//
// When the intent classifier returns intentCoding and the user has not sent a
// skip signal AND the task is not a bug-fix/debug task, the gate strips coding
// tool calls (create_session, bash, etc.) from ALL iterations of the LLM
// response loop, preserving text content and delivery tools (generate_pdf,
// send_file, etc.).
//
// Bug-fix/debug tasks (e.g. "有bug，一直显示加载中", "修复加载错误") are
// detected by isBugFixOnly() and bypass the gate entirely, because they should
// be executed directly without the three-phase workflow (requirements → design
// → task breakdown).
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
// active during the three-phase workflow (requirements → design → task breakdown).
// This includes coding session tools, direct coding tools, AND browser automation
// tools — during the three-phase phases, none of these should be available.
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
	// Browser automation tools — stripped during coding workflow phases to
	// prevent LLM confusion from 25+ browser tool definitions in context.
	"browser_session_start": true, "browser_session_stop": true, "browser_observe": true,
	"browser_navigate": true, "browser_click": true, "browser_type": true,
	"browser_wait": true, "browser_back": true, "browser_refresh": true, "browser_extract": true,
	"browser_connect": true, "browser_screenshot": true, "browser_get_text": true,
	"browser_get_html": true, "browser_eval": true, "browser_scroll": true,
	"browser_select": true, "browser_list_pages": true, "browser_switch_page": true,
	"browser_close": true, "browser_click_at": true, "browser_set_files": true,
	"browser_info": true, "browser_ocr": true,
	"browser_task_run": true, "browser_task_replay": true, "browser_task_verify": true, "browser_task_status": true,
	"browser_record_start": true, "browser_record_stop": true, "browser_list_flows": true,
	"gui_record_start": true, "gui_record_stop": true,
	"gui_observe": true, "gui_verify": true,
}

// directModeSessionBlocklist lists session management tools that should be
// stripped during the execution phase when using direct coding mode. In direct
// mode, maclaw writes code itself using bash/write_file/edit_file — external
// session tools are unnecessary and their presence confuses the LLM into
// trying to delegate instead of coding directly.
var directModeSessionBlocklist = map[string]bool{
	"create_session":    true,
	"send_and_observe":  true,
	"control_session":   true,
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

// skipSignalsChinese contains Chinese phrases that bypass the gate.
var skipSignalsChinese = []string{
	"直接做", "直接用", "不用问了", "按你的想法来", "直接开始",
	"不用确认", "马上做", "赶紧做", "跳过文档", "不需要文档",
	// Action/continuation phrases — user wants to start working on an
	// already-discussed task, not go through the three-phase workflow again.
	// NOTE: These are degraded-mode fallbacks only. When GIC/UIC is available,
	// the classifier's GateIntentContinuation handles these phrases semantically.
	"开工", "开干", "动手", "搞起来", "搞起", "干吧", "做吧",
	"开始吧", "开始做", "开始干", "开始搞",
}

// skipSignalsEnglish contains English phrases that bypass the gate.
var skipSignalsEnglish = []string{
	"just do it", "skip confirmation", "go ahead", "do it now",
	"let's go", "let's do it", "let's start", "let's begin",
}

// bugFixKeywords are phrases that indicate a bug-fix, debugging, or
// maintenance task rather than a new-project creation task. When the user
// message ONLY matches these keywords (and none of the "creation" keywords
// like "开发一个", "游戏", "前端" etc.), the gate is NOT activated because
// bug fixes should be executed directly without the three-phase workflow.
var bugFixKeywords = map[string]bool{
	"修bug": true, "修 bug": true, "修复bug": true, "修复 bug": true,
	"bug": true, "fix": true, "修复": true, "修正": true,
	"调试": true, "debug": true, "排查": true, "排错": true,
	"报错": true, "出错": true, "错误": true, "异常": true,
	"加载中": true, "卡住": true, "崩溃": true, "crash": true,
	"白屏": true, "闪退": true, "不工作": true, "不生效": true,
	"失败": true, "不显示": true, "显示异常": true,
}

// creationCodingKeywords are phrases that indicate a new project/feature
// creation task that SHOULD go through the three-phase workflow. When any
// of these appear alongside bugFixKeywords, the gate remains active.
var creationCodingKeywords = []string{
	"开发", "开发一个", "开发个", "实现一个", "实现个",
	"创建一个", "创建个", "写代码", "编程",
	"添加功能", "新增功能", "写脚本", "写一个脚本", "写个脚本",
	"写函数", "写方法", "写接口", "写api", "写 api",
	"游戏", "game", "前端", "后端", "frontend", "backend",
}

// isBugFixOnly returns true when the user message matches bug-fix keywords
// but does NOT match any creation-oriented coding keywords. This means the
// task is a direct fix/debug that should skip the three-phase workflow.
func isBugFixOnly(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	// Check if any bug-fix keyword matches.
	hasBugFix := false
	for kw := range bugFixKeywords {
		if strings.Contains(lower, kw) {
			hasBugFix = true
			break
		}
	}
	if !hasBugFix {
		return false
	}
	// If any creation keyword also matches, this is a new project that
	// happens to mention bugs — not a pure bug-fix task.
	for _, kw := range creationCodingKeywords {
		if strings.Contains(lower, kw) {
			return false
		}
	}
	return true
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

// mapGateIntentToConfig maps a GateIntentResult from the semantic classifier
// to a codingToolGateConfig. This is the bridge between the five-category
// semantic classification and the gate's active/bugFix decision.
//
// Mapping rules:
//   - new_project + confidence ≥ 0.70 → active=true, intent=intentCoding
//   - bug_fix → active=false, bugFix=true, intent=intentCoding
//   - maintenance → active=false, intent=intentCoding
//   - non_coding → active=false, intent=intentNonCoding
//   - continuation → active=false, intent=intentUnknown
//   - unknown or low confidence → active=false, intent=intentUnknown
func mapGateIntentToConfig(result GateIntentResult, skip bool) codingToolGateConfig {
	cfg := codingToolGateConfig{
		skipSignal: skip,
	}

	if skip {
		cfg.reason = fmt.Sprintf("gate inactive: continuation/skip (classifier: %s, conf=%.2f)", result.Intent, result.Confidence)
		return cfg
	}

	switch result.Intent {
	case GateIntentNewProject:
		if result.Confidence >= 0.70 {
			cfg.active = true
			cfg.intent = intentCoding
			cfg.reason = fmt.Sprintf("gate active: semantic new_project (conf=%.2f, layer=%d)", result.Confidence, result.Layer)
		} else {
			cfg.intent = intentUnknown
			cfg.reason = fmt.Sprintf("gate inactive: new_project but low confidence (%.2f)", result.Confidence)
		}
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
		cfg.intent = intentUnknown
		cfg.reason = fmt.Sprintf("gate inactive: semantic unknown/low confidence (conf=%.2f)", result.Confidence)
	}

	return cfg
}

// newCodingToolGateConfig computes the gate decision once before the iteration
// loop begins. The result is immutable for the duration of the loop.
func newCodingToolGateConfig(userText string, loopKind LoopKind) codingToolGateConfig {
	return newCodingToolGateConfigWithClassifier(userText, loopKind, nil, "")
}

// newCodingToolGateConfigWithClassifier is like newCodingToolGateConfig but
// accepts an optional GateIntentClassifier for semantic classification and
// a userID for conversation context lookup.
//
// Classification priority:
//  1. GateIntentClassifier (semantic, delegates to UIC when available)
//  2. classifyTaskIntent (delegates to UIC when available, keyword fallback only when UIC is nil)
//
// isBugFixOnly() keyword detection is only used when classifyTaskIntent falls
// back to keyword rules (UIC unavailable). When UIC is available, bug-fix
// detection is handled by the UIC's LabelBugFix classification.
func newCodingToolGateConfigWithClassifier(userText string, loopKind LoopKind, gic *GateIntentClassifier, userID string) codingToolGateConfig {
	// Background loop always bypasses the gate, regardless of classification.
	if loopKind == LoopKindBackground {
		return codingToolGateConfig{
			reason: "gate inactive: background loop",
		}
	}

	// Try semantic classification first when classifier is available and ready.
	// When a classifier is available, the skip signal is derived from the
	// classifier's result (GateIntentContinuation), NOT from keyword matching.
	// containsSkipSignal is only used in degraded mode (no classifier).
	if gic != nil && gic.Ready() {
		result := gic.Classify(userText, userID)
		skip := result.Intent == GateIntentContinuation
		cfg := mapGateIntentToConfig(result, skip)
		return cfg
	}

	// Fallback: GIC unavailable. Try UIC directly, then keyword fallback.
	// This path is reached when GIC hasn't been initialized yet (early startup)
	// or when embedding is unavailable (GIC.Ready() returns false).
	if uic := unifiedClassifier; uic != nil {
		// UIC available — use it directly for classification.
		uicResult := uic.Classify(intent.MessageContext{Text: userText})
		gateIntent, confidence, _, layer, reason := uicResult.ToGateIntent()
		gateResult := GateIntentResult{
			Intent:     GateIntent(gateIntent),
			Confidence: confidence,
			Layer:      layer,
			Reason:     reason,
		}
		skip := gateResult.Intent == GateIntentContinuation
		cfg := mapGateIntentToConfig(gateResult, skip)
		return cfg
	}

	// Neither GIC nor UIC available — degraded mode.
	// Use keyword-based skip signal detection as last resort.
	skip := containsSkipSignal(userText)
	bugfix := isBugFixOnly(userText)
	cfg := codingToolGateConfig{
		intent:     intentAmbiguous,
		skipSignal: skip,
		bugFix:     bugfix,
	}
	if bugfix {
		cfg.reason = "gate inactive: bug-fix detected (keyword fallback, UIC unavailable)"
	} else if skip {
		cfg.reason = "gate inactive: skip signal detected (keyword fallback, UIC unavailable)"
	} else {
		cfg.reason = "gate inactive: intent=ambiguous (degraded mode, UIC unavailable)"
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
