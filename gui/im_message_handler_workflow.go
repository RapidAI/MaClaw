package main

import (
	"context"
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

	// --- PendingConfirm: lightweight LLM intent classification ---
	// The engine detected that the user is responding to a NeedsConfirm phase
	// that already has output. Instead of running the full agent loop (~55K
	// tokens with tools + history), we make a single lightweight LLM call
	// (~200 tokens) to classify the intent as confirm/modify/other.
	if resp.PendingConfirm {
		return h.handlePendingConfirm(engine, userID, text)
	}

	if !resp.RunAgentLoop {
		if resp.Complete {
			if adapter, ok := engine.GetCallbacks().(*GUIWorkflowAdapter); ok {
				adapter.ResetSuggestMaximize(userID)
			}
		}
		return &IMAgentResponse{Text: resp.Text}
	}
	// RunAgentLoop=true — the workflow engine wants the agent loop to
	// generate phase output.
	if !resp.DefaultInput {
		if resp.PhasePrompt != "" {
			h.stashedPhasePrompt.Store(userID, resp.PhasePrompt)
		}
		h.workflowAgentLoopMarker.Store(userID, true)
	}
	if resp.Advance && resp.Text != "" {
		if cb := engine.GetCallbacks(); cb != nil {
			_ = cb.SendTextToUser(userID, resp.Text)
		}
	}
	return nil
}

// handlePendingConfirm uses a lightweight LLM call to classify the user's
// intent after viewing a phase document: confirm, modify, or other.
//
// Token budget: ~300-500 input + ~10 output vs ~55000 input for full agent loop.
// Latency: 1-3s (lightweight) vs 5-15s (full loop).
func (h *IMMessageHandler) handlePendingConfirm(engine *workflow.WorkflowEngine, userID, text string) *IMAgentResponse {
	ctx := context.Background()

	// Build compact context: phase name + first ~200 chars of the document +
	// the system's last prompt to the user. This gives the LLM enough context
	// to distinguish "好的" (confirming the requirements doc) from "好的"
	// (random agreement), without sending the full 5000+ char document.
	phaseContext := ""
	ws := engine.GetActiveWorkflow(userID)
	if ws != nil {
		tmpl := engine.GetRegistry().Match(ws.Type)
		if tmpl != nil && ws.PhaseIndex < len(tmpl.Phases) {
			phase := &tmpl.Phases[ws.PhaseIndex]
			phaseContext = fmt.Sprintf("当前工作流阶段：%s（%s）\n", phase.Name, phase.Description)

			// Add a snippet of the phase output (first ~200 runes).
			if output, ok := ws.PhaseOutputs[ws.CurrentPhase]; ok && len(output) > 0 {
				snippet := output
				runes := []rune(snippet)
				if len(runes) > 200 {
					snippet = string(runes[:200]) + "..."
				}
				phaseContext += fmt.Sprintf("文档摘要：%s\n", snippet)
			}

			phaseContext += "系统提示用户：请查看文档并确认，或提出修改意见。"
		}
	}

	userMessage := text
	if phaseContext != "" {
		userMessage = fmt.Sprintf("[上下文]\n%s\n\n[用户回复]\n%s", phaseContext, text)
	}

	classifyResult, err := h.LLMClassify(ctx, LLMClassifyRequest{
		SystemPrompt: `You are a user intent classifier for a document review workflow.

The user was shown a document and asked to confirm or request changes. You will receive the workflow context (phase name, document snippet, system prompt) and the user's response.

Classify the user's response into exactly one category. Reply with ONLY the category word, nothing else:
- "confirm" — user approves the document and wants to proceed to the next phase
- "modify" — user wants to change or update the document
- "other" — user is asking something unrelated to the document review`,
		UserMessage: userMessage,
		TimeoutSec:  10,
		Tag:         "workflow-confirm",
	})

	if err != nil {
		// LLM call failed — fall back to treating as confirm (most common intent).
		// This is safe because modify requests will be caught on the next round
		// when the user sees the wrong phase and corrects.
		log.Printf("[workflow-confirm] LLM classify failed, falling back to confirm: %v", err)
		return h.advanceAndRespond(engine, userID)
	}

	intent := strings.ToLower(strings.TrimSpace(classifyResult.Text))
	log.Printf("[workflow-confirm] user=%s text=%q → intent=%q (input=%d output=%d latency=%.1fs)",
		userID, truncateForLogGUI(text, 30), intent, classifyResult.InputTokens, classifyResult.OutputTokens, classifyResult.Latency.Seconds())

	switch {
	case strings.Contains(intent, "confirm"):
		return h.advanceAndRespond(engine, userID)

	case strings.Contains(intent, "modify"):
		// User wants to modify — run the agent loop with a modify prompt.
		if ws == nil {
			return nil // fall through to normal agent loop
		}
		tmpl := engine.GetRegistry().Match(ws.Type)
		if tmpl == nil || ws.PhaseIndex >= len(tmpl.Phases) {
			return nil
		}
		phase := &tmpl.Phases[ws.PhaseIndex]
		phasePrompt := workflow.BuildPhaseSystemPrompt(ws, phase, engine.GetRegistry())
		modifyPrompt := fmt.Sprintf("%s\n\n## 用户修改请求\n\n用户要求修改当前阶段产出物：%s\n请根据修改意见更新产出物。", phasePrompt, text)
		h.stashedPhasePrompt.Store(userID, modifyPrompt)
		h.workflowAgentLoopMarker.Store(userID, true)
		return nil // fall through to agent loop with modify prompt

	default:
		// "other" or unrecognized — let the message fall through to normal
		// agent loop handling (e.g. weather query during active workflow).
		return nil
	}
}

// advanceAndRespond advances the workflow to the next phase and returns
// the transition message as an IMAgentResponse.
func (h *IMMessageHandler) advanceAndRespond(engine *workflow.WorkflowEngine, userID string) *IMAgentResponse {
	advResp, advErr := engine.AdvancePhase(userID)
	if advErr != nil {
		log.Printf("[workflow-confirm] AdvancePhase error: user=%s err=%v", userID, advErr)
		return nil // fall through to normal agent loop
	}
	if advResp == nil {
		return nil
	}

	// If the next phase needs the agent loop to generate content,
	// stash the prompt and let the agent loop handle it.
	if advResp.RunAgentLoop {
		if advResp.PhasePrompt != "" {
			h.stashedPhasePrompt.Store(userID, advResp.PhasePrompt)
		}
		h.workflowAgentLoopMarker.Store(userID, true)
		// Send the transition text before the agent loop runs.
		if advResp.Text != "" {
			if cb := engine.GetCallbacks(); cb != nil {
				_ = cb.SendTextToUser(userID, advResp.Text)
			}
		}
		return nil // fall through to agent loop for next phase
	}

	// Non-RunAgentLoop response (e.g. workflow completed).
	if advResp.Complete {
		if adapter, ok := engine.GetCallbacks().(*GUIWorkflowAdapter); ok {
			adapter.ResetSuggestMaximize(userID)
		}
	}
	return &IMAgentResponse{Text: advResp.Text}
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
		// Guard: if the LLM returned category="none" or empty with ready=true,
		// it means the task is NOT a workflow task (e.g., content processing).
		// Do NOT call StartWorkflow — fall through to the normal agent loop.
		// The understanding session has already been cleaned up by HandleInput
		// when isReady=true, so no additional cleanup is needed.
		if intent.Category == workflow.WorkflowNone || intent.Category == "" {
			log.Printf("[WorkflowInterception] understanding returned ready=true with category=%q for user %s, falling through to agent loop", intent.Category, userID)
			return nil
		}

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
		// LLM call failed (timeout, network error, etc.). Use keyword-based
		// template matching as a degraded fallback. This catches obvious
		// workflow tasks like "生成PRD文档" even when the LLM is unavailable.
		return h.tryKeywordWorkflowFallback(engine, userID, text)
	}

	// LLM determined this is NOT a workflow task — trust the LLM's judgment
	// and fall through to the normal agent loop. No session was created.
	//
	// Previously this path called tryKeywordWorkflowFallback(strongOnly=true)
	// to override the LLM with keyword matching. This caused false positives:
	// "打开桌面上的PPT文件并截图" was correctly rejected by the LLM but then
	// overridden by the "PPT" strong keyword match into presentation_design.
	//
	// The LLM has full semantic understanding of the user's intent. Keyword
	// matching cannot distinguish "打开PPT文件" (file operation) from
	// "设计一个PPT" (creation task). When the LLM explicitly says "not a
	// workflow", we trust it.
	if result.Rejected {
		log.Printf("[WorkflowInterception] understanding rejected task for user %s: %q — trusting LLM, no keyword override", userID, truncateRunes(text, 80))
		return nil
	}

	// LLM says it IS a workflow task — an understanding session has been
	// created. The user will go through multi-round clarification before
	// the workflow actually starts. Do NOT emit suggest_maximize here;
	// it will be emitted when StartWorkflow succeeds (in handleActiveUnderstanding
	// or tryKeywordWorkflowFallback).

	return &IMAgentResponse{Text: result.Reply}
}

// tryKeywordWorkflowFallback attempts to start a workflow using keyword-based
// template matching when the LLM intent understanding call FAILS (timeout,
// network error, etc.). This is a degraded fallback for when the LLM is
// unavailable — it catches obvious workflow tasks like "生成PRD文档" that
// contain strong template keywords (PRD, SWOT, 商业计划 etc.).
//
// This function should ONLY be called when the LLM call fails. It must NOT
// be used to override an explicit LLM rejection — the LLM has full semantic
// understanding and can distinguish "打开PPT文件" (file operation) from
// "设计一个PPT" (creation task), which keyword matching cannot.
//
// Returns nil if no template matches (caller should fall through).
func (h *IMMessageHandler) tryKeywordWorkflowFallback(engine *workflow.WorkflowEngine, userID, text string) *IMAgentResponse {
	registry := engine.GetRegistry()
	if registry == nil {
		return nil
	}

	matchedType, matched := registry.MatchTemplateByKeywords(text)
	if !matched {
		return nil
	}
	log.Printf("[WorkflowInterception] keyword fallback matched template %q for user %s: %q", matchedType, userID, truncateRunes(text, 80))

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
