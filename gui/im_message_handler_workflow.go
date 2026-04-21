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
	// the system's last prompt to the user + the last assistant message.
	// The last assistant message is critical — it often contains instructions
	// like "确定了就告诉我'开工'" which the LLM needs to understand that
	// "开工" is a direct response to that prompt, not an unrelated message.
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

			phaseContext += "系统提示用户：请查看文档并确认，或提出修改意见。\n"

			// Inject the last assistant message from conversation history.
			// This provides the immediate conversational context — e.g. if
			// the assistant said "确定了就告诉我'开工'", the classifier can
			// understand that "开工" is a direct response to that prompt.
			if lastAssistant := h.getLastAssistantSnippet(userID, 300); lastAssistant != "" {
				phaseContext += fmt.Sprintf("助手最后一条消息：%s\n", lastAssistant)
			}
		}
	}

	userMessage := text
	if phaseContext != "" {
		userMessage = fmt.Sprintf("[上下文]\n%s\n\n[用户回复]\n%s", phaseContext, text)
	}

	classifyResult, err := h.LLMClassify(ctx, LLMClassifyRequest{
		SystemPrompt: `You are a user intent classifier for a document review workflow.

The user was shown a document/plan and asked to confirm or request changes. You will receive:
- The workflow context: phase name, document snippet, the system prompt shown to the user
- The assistant's last message (if available): this is what the user is directly responding to
- The user's response

IMPORTANT: Pay close attention to the assistant's last message. If the assistant asked the user to say a specific word/phrase to proceed (e.g. "确定了就告诉我'开工'"), and the user replies with exactly that word/phrase, it is a confirmation — not an unrelated message.

Classify the user's response into exactly one category. Reply with ONLY the category word, nothing else:
- "confirm" — user approves and wants to proceed. This includes any form of agreement, acceptance, or readiness signal in the context of the ongoing review. A short or vague reply after being shown a document almost always means approval.
- "modify" — user provides specific feedback, corrections, additions, or requests changes to the DOCUMENT CONTENT itself (e.g. "加一个登录功能", "把技术栈改成React", "需求里漏了XX").
- "other" — user is asking something clearly unrelated to the document or workflow. This includes:
  - Server/infrastructure operations: "更新omniroute", "登录服务器", "重启服务", "npm install", "部署"
  - Information queries: weather, search, off-topic questions
  - File operations: "打开XX文件", "截图"
  - Any request that involves EXECUTING an action on a system/server/tool, rather than editing the document

CRITICAL: "更新" (update) can mean either "update the document" or "update software on a server". If the object of "更新" is a software package, service, server component, or tool name (not a section/content of the document), classify as "other".

When in doubt between "confirm" and "other", prefer "confirm" — the conversational context is a document review, so the user's response is most likely directed at the document.`,
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
		// User wants to modify the phase document — fall through to the
		// normal agent loop (NOT the workflow-specific agent loop).
		//
		// Previously this branch set workflowAgentLoopMarker=true and
		// injected a phasePrompt, which caused the agent loop to run in
		// workflow mode with doc_only tool filtering. This was wrong:
		// when the LLM misclassifies an operation request (e.g. "更新
		// 上面的omniroute") as "modify", the workflow agent loop strips
		// ssh/bash tools and the LLM can't execute the actual request.
		//
		// By falling through to the normal agent loop (same as "other"),
		// the LLM gets the full tool list and conversation history. It
		// can determine on its own whether the user wants to edit the
		// document or perform a server operation. The workflow phase
		// context is already in the conversation history, so the LLM
		// has enough information to update the document if that's truly
		// what the user wants.
		//
		// Mark as "other" so the agent loop skips NeedsConfirm gate and
		// doc_only tool filtering — same treatment as unrelated messages.
		h.workflowPendingConfirmOther.Store(userID, true)
		return nil // fall through to normal agent loop

	default:
		// "other" or unrecognized — let the message fall through to normal
		// agent loop handling (e.g. weather query during active workflow).
		// Mark this so the agent loop skips the NeedsConfirm gate and does
		// not capture the unrelated LLM output as a phase document.
		h.workflowPendingConfirmOther.Store(userID, true)
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
		// These workflows wait for user input before generating phase content.
		if req := engine.GetInputRequirement(userID); req != nil {
			overview += "\n\n📎 " + req.Description
			if len(req.FileTypes) > 0 {
				overview += fmt.Sprintf("（支持格式：%s）", strings.Join(req.FileTypes, "、"))
			}
			if req.AcceptText {
				overview += "\n也可以直接将文档内容粘贴到对话框中，或提供网址由系统自动抓取。"
			}
			return &IMAgentResponse{Text: overview}
		}

		// Non-input-driven workflows: automatically trigger the agent loop
		// to generate the first phase document, instead of requiring the
		// user to send a second message (e.g. "开工").
		//
		// Send the overview as an intermediate message, then set up the
		// agent loop markers so the caller runs the phase generation.
		// This mirrors the pattern in handleActiveWorkflow() when
		// engine.HandleInput() returns RunAgentLoop=true.
		log.Printf("[WorkflowInterception] auto-triggering agent loop for first phase: user=%s type=%s phase=%s",
			userID, state.Type, state.CurrentPhase)
		if cb := engine.GetCallbacks(); cb != nil {
			_ = cb.SendTextToUser(userID, overview)
		}
		// Build the phase prompt for the first phase.
		tmpl := engine.GetRegistry().Match(state.Type)
		if tmpl != nil && len(tmpl.Phases) > 0 {
			firstPhase := &tmpl.Phases[0]
			phasePrompt := workflow.BuildPhaseSystemPrompt(state, firstPhase, engine.GetRegistry())
			if phasePrompt != "" {
				h.stashedPhasePrompt.Store(userID, phasePrompt)
			}
		}
		h.workflowAgentLoopMarker.Store(userID, true)
		return nil // fall through to agent loop
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

// filterToolsForDocOnly filters the tool list to only include tools permitted
// during doc_only workflow phases, as defined by workflow.DocOnlyAllowedTools.
func filterToolsForDocOnly(tools []map[string]interface{}) []map[string]interface{} {
	if len(tools) == 0 {
		return tools
	}
	filtered := make([]map[string]interface{}, 0, len(tools))
	for _, def := range tools {
		name := extractToolName(def)
		if workflow.DocOnlyAllowedTools[name] {
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

// getLastAssistantSnippet returns the tail of the last assistant message from
// conversation history, truncated to maxRunes from the END. We take the tail
// because assistant messages are often long documents followed by a short
// confirmation prompt at the end (e.g. "确定了就告诉我'开工'"). The tail
// captures this prompt, which is the most relevant context for classifying
// the user's response.
func (h *IMMessageHandler) getLastAssistantSnippet(userID string, maxRunes int) string {
	if h.memory == nil {
		return ""
	}
	entries := h.memory.load(userID)
	// Walk backwards to find the last assistant message.
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Role != "assistant" {
			continue
		}
		content, ok := entries[i].Content.(string)
		if !ok || content == "" {
			continue
		}
		runes := []rune(content)
		if len(runes) > maxRunes {
			// Take the tail — the confirmation prompt is at the end.
			return "..." + string(runes[len(runes)-maxRunes:])
		}
		return content
	}
	return ""
}
