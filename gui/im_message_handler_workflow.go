package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/workflow"
)

// handleWorkflowInterception checks if the message should be handled by the
// workflow engine (corelib/workflow). Returns an IMAgentResponse if the message
// was fully handled, or nil if it should proceed to the normal agent loop.
//
// Called from handleIMMessageWithLoop after slash commands and LLM config check,
// before the main agent loop logic.
func (h *IMMessageHandler) handleWorkflowInterception(userID, text string) *IMAgentResponse {
	engine := h.app.workflowEngine
	if engine == nil {
		return nil
	}

	filter := engine.GetFilter()
	if filter == nil {
		return nil
	}

	classification := filter.Classify(userID, text)

	switch classification {
	case workflow.FilterActiveWorkflow:
		return h.handleActiveWorkflow(engine, userID, text)

	case workflow.FilterActiveUnderstanding:
		return h.handleActiveUnderstanding(engine, userID, text)

	case workflow.FilterNeedsUnderstanding:
		return h.handleNeedsUnderstanding(engine, userID, text)

	case workflow.FilterSmallTalk:
		// Already handled by isShortChitChatMessage above in the caller.
		// If it reaches here (e.g., longer small talk), let it pass through
		// to the normal agent loop.
		return nil

	case workflow.FilterSimpleDirective:
		// Pass through to normal agent loop — no workflow interception needed.
		return nil
	}

	return nil
}

// handleActiveWorkflow processes input for a user with an active workflow.
func (h *IMMessageHandler) handleActiveWorkflow(engine *workflow.WorkflowEngine, userID, text string) *IMAgentResponse {
	resp, err := engine.HandleInput(userID, text)
	if err != nil {
		log.Printf("[WorkflowInterception] HandleInput error for user %s: %v", userID, err)
		return &IMAgentResponse{Error: fmt.Sprintf("工作流处理出错: %v", err)}
	}
	if resp == nil {
		return nil
	}
	if !resp.RunAgentLoop {
		// If the workflow just completed, reset the suggest_maximize dedup
		// flag so the next workflow can trigger the banner again.
		if resp.Complete {
			if adapter, ok := engine.GetCallbacks().(*GUIWorkflowAdapter); ok {
				adapter.ResetSuggestMaximize(userID)
			}
		}
		return &IMAgentResponse{Text: resp.Text}
	}
	// RunAgentLoop=true — the workflow engine wants the agent loop to
	// generate phase output. The fullscreen banner was already emitted
	// when the workflow was first started (StartWorkflow). Do NOT emit
	// it again here — this path runs for every message while the workflow
	// is active, including non-workflow messages like weather queries.
	//
	// Only set the workflowAgentLoopMarker when the engine matched a
	// specific action (confirm/skip/modify). When DefaultInput is true,
	// the message didn't match any workflow keyword — it may be unrelated
	// (e.g. "查询天气" while a coding workflow is active). In that case,
	// let the agent loop run normally without doc capture, phase prompt
	// injection, or preview panel updates.
	if !resp.DefaultInput {
		if resp.PhasePrompt != "" {
			h.stashedPhasePrompt.Store(userID, resp.PhasePrompt)
		}
		h.workflowAgentLoopMarker.Store(userID, true)
	}
	// Send phase transition text (e.g. "✅ 进入阶段 2/5：技术设计") to the user
	// before the agent loop runs, so they see the transition message.
	if resp.Advance && resp.Text != "" {
		if cb := engine.GetCallbacks(); cb != nil {
			_ = cb.SendTextToUser(userID, resp.Text)
		}
	}
	return nil
}

// handleActiveUnderstanding processes input for a user with an active
// intent understanding session.
func (h *IMMessageHandler) handleActiveUnderstanding(engine *workflow.WorkflowEngine, userID, text string) *IMAgentResponse {
	understanding := engine.GetUnderstanding()
	if understanding == nil {
		return nil
	}

	reply, ready, cancelled, intent, err := understanding.HandleInput(userID, text)
	if err != nil {
		log.Printf("[WorkflowInterception] understanding HandleInput error for user %s: %v", userID, err)
		// On LLM timeout or transient error, clean up the understanding session
		// and fall through to the normal agent loop instead of showing a raw error.
		if understanding.HasActiveSession(userID) {
			_, _, _, _, _ = understanding.HandleInput(userID, "取消")
		}
		return nil // fall through to normal agent loop
	}
	if cancelled {
		return &IMAgentResponse{Text: "已取消。有什么其他需要帮助的吗？"}
	}
	if ready && intent != nil {
		// Intent understanding is complete — start the workflow.
		state, err := engine.StartWorkflow(userID, *intent)
		if err != nil {
			log.Printf("[WorkflowInterception] StartWorkflow error for user %s: %v", userID, err)
			return &IMAgentResponse{Error: fmt.Sprintf("启动工作流失败: %v", err)}
		}
		// Suggest maximizing the AI panel for workflow experience.
		if adapter, ok := engine.GetCallbacks().(*GUIWorkflowAdapter); ok {
			adapter.EmitSuggestMaximize(userID, string(state.Type))
		}
		overview := fmt.Sprintf("🚀 工作流已启动：%s\n📋 当前阶段：%s", state.Type, state.CurrentPhase)
		if reply != "" {
			overview += "\n\n" + reply
		}
		// For document-required workflows, append the upload prompt to the
		// startup message so the user knows to provide the input document.
		if req := engine.GetInputRequirement(userID); req != nil {
			overview += "\n\n📎 " + req.Description
			if len(req.FileTypes) > 0 {
				overview += fmt.Sprintf("（支持格式：%s）", strings.Join(req.FileTypes, "、"))
			}
			if req.AcceptText {
				overview += "\n也可以直接将文档内容粘贴到对话框中，或提供网址由系统自动抓取。"
			}
		}
		return &IMAgentResponse{Text: overview}
	}
	return &IMAgentResponse{Text: reply}
}

// handleNeedsUnderstanding starts a new intent understanding session for a
// message that needs LLM classification. The LLM will decide:
//   - Is this a workflow task? (if not, returns nil → normal agent loop)
//   - Which workflow template?
//   - Structured intent extraction
func (h *IMMessageHandler) handleNeedsUnderstanding(engine *workflow.WorkflowEngine, userID, text string) *IMAgentResponse {
	understanding := engine.GetUnderstanding()
	if understanding == nil {
		// No understanding manager — fall through to normal agent loop.
		return nil
	}

	result, err := understanding.Start(userID, text)
	if err != nil {
		log.Printf("[WorkflowInterception] understanding Start error for user %s: %v", userID, err)
		// LLM call failed (timeout, network error, etc.). Before falling
		// through to the normal agent loop, try keyword-based template
		// matching as a fallback. This catches obvious workflow tasks like
		// "生成PRD文档" that contain strong template keywords (PRD, SWOT, etc.)
		// even when the LLM is unavailable.
		return h.tryKeywordWorkflowFallback(engine, userID, text, false)
	}

	// LLM determined this is NOT a workflow task — fall through to normal
	// agent loop. No session was created.
	if result.Rejected {
		log.Printf("[WorkflowInterception] understanding rejected task for user %s: %q", userID, truncateRunes(text, 80))
		// Only override an explicit LLM rejection when there's a STRONG
		// keyword match (uppercase abbreviation like PRD, or long Chinese
		// phrase like 商业计划). Weak matches (e.g., "产品"+"需求") are not
		// enough to override the LLM — "翻译这个产品需求文档" should stay
		// rejected as a simple translation task.
		return h.tryKeywordWorkflowFallback(engine, userID, text, true)
	}

	// LLM says it IS a workflow task — an understanding session has been
	// created. The user will go through multi-round clarification before
	// the workflow actually starts. Do NOT emit suggest_maximize here;
	// it will be emitted when StartWorkflow succeeds (in handleActiveUnderstanding
	// or tryKeywordWorkflowFallback).

	return &IMAgentResponse{Text: result.Reply}
}

// tryKeywordWorkflowFallback attempts to start a workflow using keyword-based
// template matching when the LLM intent understanding call fails or rejects
// the task. This is a safety net for obvious workflow tasks (containing strong
// keywords like PRD, SWOT, 商业计划 etc.) that should not fall through to the
// normal agent loop just because the LLM is slow or misconfigured.
//
// When strongOnly is true (LLM explicitly rejected), only strong keyword
// matches (uppercase abbreviations, ≥3 Chinese char phrases) can override.
// When strongOnly is false (LLM error/timeout), both strong and weak (≥2 hits)
// matches are accepted.
//
// Returns nil if no template matches (caller should fall through).
func (h *IMMessageHandler) tryKeywordWorkflowFallback(engine *workflow.WorkflowEngine, userID, text string, strongOnly bool) *IMAgentResponse {
	registry := engine.GetRegistry()
	if registry == nil {
		return nil
	}

	var matchedType workflow.WorkflowType
	var matched bool
	if strongOnly {
		matchedType, matched = registry.MatchTemplateByStrongKeyword(text)
	} else {
		matchedType, matched = registry.MatchTemplateByKeywords(text)
	}
	if !matched {
		return nil
	}
	log.Printf("[WorkflowInterception] keyword fallback matched template %q (strongOnly=%v) for user %s: %q", matchedType, strongOnly, userID, truncateRunes(text, 80))

	// Build a minimal StructuredIntent and start the workflow directly,
	// bypassing the multi-round understanding session.
	intent := workflow.StructuredIntent{
		Category:   matchedType,
		Summary:    text,
		Goals:      []string{text},
		Confidence: 0.7, // keyword match is less confident than LLM
	}
	state, err := engine.StartWorkflow(userID, intent)
	if err != nil {
		log.Printf("[WorkflowInterception] keyword fallback StartWorkflow error for user %s: %v", userID, err)
		return nil
	}
	if adapter, ok := engine.GetCallbacks().(*GUIWorkflowAdapter); ok {
		adapter.EmitSuggestMaximize(userID, string(state.Type))
	}
	overview := fmt.Sprintf("🚀 工作流已启动：%s\n📋 当前阶段：%s", state.Type, state.CurrentPhase)

	// Append input requirement prompt for document-required workflows
	// (bid_response, contract_review, etc.).
	if req := engine.GetInputRequirement(userID); req != nil {
		overview += "\n\n📎 " + req.Description
		if len(req.FileTypes) > 0 {
			overview += fmt.Sprintf("（支持格式：%s）", strings.Join(req.FileTypes, "、"))
		}
		if req.AcceptText {
			overview += "\n也可以直接将文档内容粘贴到对话框中，或提供网址由系统自动抓取。"
		}
	}

	return &IMAgentResponse{Text: overview}
}

// cancelWorkflowForUser cancels any active workflow and understanding session
// for the given user. Called from /new, /reset, /clear, and /cancel handlers.
func (h *IMMessageHandler) cancelWorkflowForUser(userID string) {
	if h.app == nil || h.app.workflowEngine == nil {
		return
	}
	engine := h.app.workflowEngine
	// Cancel active workflow (ignore error if none active).
	_ = engine.CancelWorkflow(userID)
	// Cancel active understanding session.
	if understanding := engine.GetUnderstanding(); understanding != nil {
		if understanding.HasActiveSession(userID) {
			// HandleInput with cancel word to clean up, or just let it expire.
			// Direct cleanup: call HandleInput with a cancel message.
			_, _, _, _, _ = understanding.HandleInput(userID, "取消")
		}
	}
	// Reset the suggest_maximize dedup flag so the next workflow can
	// trigger the fullscreen banner again.
	if adapter, ok := engine.GetCallbacks().(*GUIWorkflowAdapter); ok {
		adapter.ResetSuggestMaximize(userID)
	}
	// Clear any pending ask_user state.
	h.pendingAskUser.Delete(userID)
}

// docOnlyAllowedTools is the set of tool names allowed during doc_only phases.
// These are documentation/communication tools that don't execute code or
// modify the project.
var docOnlyAllowedTools = map[string]bool{
	"write_file":    true,
	"read_file":     true,
	"edit_file":     true,
	"memory":        true,
	"generate_pdf":  true,
	"send_file":     true,
	"web_search":    true,
	"web_fetch":     true,
	"open":          true,
	"set_nickname":  true,
}

// filterToolsForDocOnly filters the tool list to only include documentation
// tools. Used during workflow phases with ToolFilterDocOnly policy.
func filterToolsForDocOnly(tools []map[string]interface{}) []map[string]interface{} {
	if len(tools) == 0 {
		return tools
	}
	filtered := make([]map[string]interface{}, 0, len(tools))
	for _, def := range tools {
		name := extractToolName(def)
		if docOnlyAllowedTools[name] {
			filtered = append(filtered, def)
		}
	}
	if len(filtered) == 0 {
		return tools // safety fallback: don't strip all tools
	}
	return filtered
}

// applyWorkflowToolFilter restricts the tool list based on the current
// workflow phase's ToolFilterPolicy.
func (h *IMMessageHandler) applyWorkflowToolFilter(userID string, tools []map[string]interface{}) []map[string]interface{} {
	engine := h.app.workflowEngine
	if engine == nil {
		return tools
	}
	policy := engine.GetPhaseToolFilter(userID)
	if policy == workflow.ToolFilterDocOnly {
		return filterToolsForDocOnly(tools)
	}
	return tools
}
