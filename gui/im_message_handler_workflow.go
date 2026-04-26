package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/workflow"
)

// getWorkflowEngine returns the workflow engine if available, or nil.
// Checks the direct field first (set at construction or by TUI), then
// falls back to h.app for GUI late-init compatibility.
func (h *IMMessageHandler) getWorkflowEngine() *workflow.WorkflowEngine {
	if h.workflowEngine != nil {
		return h.workflowEngine
	}
	if h.app == nil {
		return nil
	}
	return h.app.workflowEngine
}

// getTaskOrchestrator returns the per-user task execution orchestrator.
// Creates one on demand if the registry exists but no orchestrator for
// this user yet. Returns nil if the registry is not initialized.
func (h *IMMessageHandler) getTaskOrchestrator(userID string) *TaskExecutionOrchestrator {
	if h.taskOrchestratorRegistry == nil {
		return nil
	}
	return h.taskOrchestratorRegistry.GetOrCreate(userID)
}

// getTaskOrchestratorReadOnly returns the per-user orchestrator if it exists,
// without creating one. Used for read-only checks (IsActive, ShouldUseSubAgent)
// to avoid polluting the registry with empty orchestrator entries.
func (h *IMMessageHandler) getTaskOrchestratorReadOnly(userID string) *TaskExecutionOrchestrator {
	if h.taskOrchestratorRegistry == nil {
		return nil
	}
	return h.taskOrchestratorRegistry.Get(userID)
}

// extractOriginalRequest returns the best available representation of the
// user's original task request from the workflow state. Returns "" if the
// current text already IS the original request (no substitution needed).
//
// Priority:
//  1. Goals[0] — UIC direct path stores the raw user text here
//  2. Summary — IUM LLM generates a structured summary of the intent
//  3. "" — text is already the original request (UIC direct, no IUM)
func extractOriginalRequest(state *workflow.WorkflowState, currentText string) string {
	if state == nil {
		return ""
	}
	// Try Goals[0] first — it's the raw user text in the UIC direct path.
	if len(state.Intent.Goals) > 0 && state.Intent.Goals[0] != "" {
		candidate := state.Intent.Goals[0]
		if candidate != currentText {
			return candidate
		}
	}
	// Try Summary — IUM LLM generates a structured summary.
	if state.Intent.Summary != "" && state.Intent.Summary != currentText {
		// Skip UIC-generated summaries like "UIC fusion: non_coding (conf=0.92)"
		// — these are internal labels, not user-facing task descriptions.
		if !strings.HasPrefix(state.Intent.Summary, "UIC fusion:") {
			return state.Intent.Summary
		}
	}
	// currentText is already the original request.
	return ""
}

// handlePostStartWorkflow is the single post-StartWorkflow handler for all
// GUI-side workflow start paths (UIC, IUM, keyword fallback). It:
//  1. Emits suggest_maximize for the desktop panel
//  2. Builds the overview text ("🚀 工作流已启动")
//  3. For input-required workflows: returns the overview + upload prompt
//  4. For other workflows: sends overview via callback, then re-routes to
//     handleActiveWorkflow which triggers the first phase's agent loop
//
// extraText is appended to the overview (e.g. IUM's reply text). Pass "" if none.
func (h *IMMessageHandler) handlePostStartWorkflow(
	engine *workflow.WorkflowEngine,
	userID, text string,
	state *workflow.WorkflowState,
	extraText string,
) *IMAgentResponse {
	// Suggest maximizing the AI panel for workflow experience.
	if adapter, ok := engine.GetCallbacks().(*GUIWorkflowAdapter); ok {
		adapter.EmitSuggestMaximize(userID, string(state.Type))
	}

	overview := fmt.Sprintf("🚀 工作流已启动：%s\n📋 当前阶段：%s", state.Type, state.CurrentPhase)
	if extraText != "" {
		overview += "\n\n" + extraText
	}

	// Input-required workflows must wait for user to provide documents.
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

	// Non-input workflows: send overview, then re-route to handleActiveWorkflow
	// which calls HandleInput → PhasePrompt + RunAgentLoop → agent loop runs.
	if cb := engine.GetCallbacks(); cb != nil {
		_ = cb.SendTextToUser(userID, overview)
	}

	// ── Stash the original task request ──
	//
	// When a workflow starts via multi-round IUM, `text` is the IUM
	// completion message (e.g. "没有其它信息了"), not the original task
	// request (e.g. "根据 readme.md 做 PPT"). The original request is
	// preserved in state.Intent — either in Goals[0] (UIC direct path
	// stores the raw user text) or in Summary (IUM LLM generates a
	// structured summary of the user's intent).
	//
	// The agent loop's userText must carry task semantics so the LLM
	// knows what to produce. We stash the best available source here;
	// runAgentLoop consumes it via LoadAndDelete when workflowAgentLoop=true.
	if originalRequest := extractOriginalRequest(state, text); originalRequest != "" {
		h.workflowOriginalRequest.Store(userID, originalRequest)
		log.Printf("[WorkflowInterception] stashed original request for user=%s: %q (current text=%q)",
			userID, truncateRunes(originalRequest, 80), truncateRunes(text, 30))
	}

	log.Printf("[WorkflowInterception] StartWorkflow succeeded, re-routing to handleActiveWorkflow for user=%s type=%s phase=%s",
		userID, state.Type, state.CurrentPhase)
	return h.handleActiveWorkflow(engine, userID, text)
}

// handleWorkflowInterception checks if the message should be handled by the
// workflow engine (corelib/workflow). Returns an IMAgentResponse if the message
// was fully handled, or nil if it should proceed to the normal agent loop.
//
// Called from handleIMMessageWithLoop after slash commands and LLM config check,
// before the main agent loop logic.
func (h *IMMessageHandler) handleWorkflowInterception(userID, text string) *IMAgentResponse {
	engine := h.getWorkflowEngine()
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
	// ── Cross-type task detection via UIC ──
	//
	// Before delegating to the engine, check if the message is a completely
	// different type of workflow task. The UIC (UnifiedIntentClassifier)
	// maps messages to workflow types via L1 keywords + L2 embedding + L3
	// tree reasoning — the same mechanism handleNeedsUnderstanding uses to
	// decide which workflow to start.
	//
	// This MUST run before HandleInput because HandleInput has multiple
	// branches (PendingConfirm, WaitingForInput, default) and cross-type
	// detection needs to work regardless of which branch would fire.
	//
	// When the UIC determines a workflow type that DIFFERS from the active
	// workflow with sufficient confidence, the user has moved on. Cancel
	// the current workflow and re-route through the full pipeline.
	if uic := h.getUnifiedClassifier(); uic != nil {
		ws := engine.GetActiveWorkflow(userID)
		if ws != nil {
			uicResult := uic.Classify(intent.MessageContext{
				Text:   text,
				UserID: userID,
			})
			if uicResult.WorkflowType != "" &&
				uicResult.WorkflowType != "none" &&
				workflow.WorkflowType(uicResult.WorkflowType) != ws.Type &&
				uicResult.Confidence >= 0.70 {
				log.Printf("[WorkflowInterception] cross-type replacement: user=%s "+
					"active=%s uic_workflow=%s intent=%s conf=%.2f — "+
					"cancelling active workflow and re-routing",
					userID, ws.Type, uicResult.WorkflowType,
					uicResult.Primary, uicResult.Confidence)
				_ = engine.CancelWorkflow(userID)
				return h.handleWorkflowInterception(userID, text)
			}
		}
	}

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

	// When the engine signals an execution phase, attempt to activate the
	// task orchestrator. The engine provides the preceding phase's output
	// as TaskBreakdownText. If it parses as a task list → orchestrator +
	// SubAgent. If not (e.g. PPT's slide_scripting output) → fall through
	// to the normal agent loop which handles execution via tools directly.
	if resp.ActivateOrchestrator {
		taskOrch := h.getTaskOrchestrator(userID)
		if taskOrch != nil && !taskOrch.IsActive() && resp.TaskBreakdownText != "" {
			tasks := ParseTaskListFromText(resp.TaskBreakdownText)
			if len(tasks) > 0 {
				projectPath := h.traceProjectPath()
				if projectPath == "" {
					home, _ := os.UserHomeDir()
					projectPath = home
				}
				taskOrch.Activate(tasks, resp.RequirementsContext, resp.DesignContext, projectPath, "")
				log.Printf("[WorkflowInterception] orchestrator activated by engine: "+
					"%d tasks for user=%s project=%s", len(tasks), userID, projectPath)
			} else {
				log.Printf("[WorkflowInterception] execution phase entered but "+
					"preceding output is not a task list — using normal agent loop "+
					"for user=%s", userID)
			}
		}
	}

	// RunAgentLoop=true — the workflow engine wants the agent loop to
	// generate phase output.
	//
	// Phase prompt injection and doc capture marker are ALWAYS set when
	// the engine provides a PhasePrompt. The phase prompt is the engine's
	// instruction to the LLM ("you are in the audience_goal phase,
	// generate..."). Without it, the LLM has no idea what to produce.
	// The marker enables doc capture (SavePhaseOutput) so the generated
	// document is saved and the workflow can advance.
	//
	// DefaultInput=true means the user's message reached the engine's
	// default branch (no confirm/skip match). This happens both for
	// legitimate phase triggers ("开工") and unrelated messages ("check
	// server status"). In both cases, the phase prompt guides the LLM
	// to produce the phase deliverable. If the LLM instead answers the
	// unrelated question, the NeedsConfirm gate will force-return the
	// output for user review — the user can then re-trigger the phase.
	//
	// Previously, DefaultInput=true suppressed both phase prompt injection
	// and doc capture, causing the workflow to stall completely — the LLM
	// received no phase instructions and produced no captured output.
	if resp.PhasePrompt != "" {
		h.stashedPhasePrompt.Store(userID, resp.PhasePrompt)
	}
	h.workflowAgentLoopMarker.Store(userID, true)
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

	// Cross-type detection is handled by handleActiveWorkflow BEFORE
	// HandleInput is called. By the time we reach here, the message is
	// confirmed to be directed at the current workflow (same type or
	// non-workflow). We only need to classify confirm/modify/cancel/other.

	ws := engine.GetActiveWorkflow(userID)

	// Build compact context: phase name + first ~200 chars of the document +
	// the system's last prompt to the user + the last assistant message.
	// The last assistant message is critical — it often contains instructions
	// like "确定了就告诉我'开工'" which the LLM needs to understand that
	// "开工" is a direct response to that prompt, not an unrelated message.
	phaseContext := ""
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
- "cancel" — user wants to abandon, cancel, or discard the current workflow entirely. This includes:
  - Explicit cancellation: "放弃", "取消", "不做了", "算了", "不要了", "cancel", "abort"
  - NOTE: Starting a completely different task type (e.g. coding task during a PPT workflow) is handled separately by the engine's cross-type detection. You do NOT need to detect that here.
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

	case strings.Contains(intent, "cancel"):
		// User explicitly wants to abandon the workflow.
		_ = engine.CancelWorkflow(userID)
		log.Printf("[workflow-confirm] user=%s cancelled workflow via LLM classification", userID)
		return &IMAgentResponse{Text: "已取消当前工作流。有什么其他需要帮助的吗？"}

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
	// ── Pre-advance validation ──
	//
	// Before advancing, verify that the current phase has a valid output.
	// This catches the case where the LLM produced unrelated content that
	// was rejected by SavePhaseOutput's minimum quality gate (or was never
	// captured at all), but the user still said "confirm".
	//
	// Instead of advancing to an empty next phase, re-trigger the current
	// phase's agent loop to generate the deliverable.
	ws := engine.GetActiveWorkflow(userID)
	if ws != nil {
		output := ws.PhaseOutputs[ws.CurrentPhase]
		if len([]rune(output)) < 100 {
			// The phase has no valid output — don't advance.
			tmpl := engine.GetRegistry().Match(ws.Type)
			if tmpl != nil && ws.PhaseIndex < len(tmpl.Phases) {
				phase := &tmpl.Phases[ws.PhaseIndex]
				phasePrompt := workflow.BuildPhaseSystemPrompt(ws, phase, engine.GetRegistry())
				h.stashedPhasePrompt.Store(userID, phasePrompt)
				h.workflowAgentLoopMarker.Store(userID, true)

				// Re-stash the original task request so the agent loop uses
				// it as userText instead of the confirm message ("开工").
				if orig := extractOriginalRequest(ws, ""); orig != "" {
					h.workflowOriginalRequest.Store(userID, orig)
				}

				log.Printf("[workflow-confirm] phase %s has no valid output (len=%d), re-triggering agent loop for user=%s",
					ws.CurrentPhase, len([]rune(output)), userID)
				if cb := engine.GetCallbacks(); cb != nil {
					_ = cb.SendTextToUser(userID, fmt.Sprintf("📋 当前阶段（%s）的文档尚未生成，正在重新生成...", phase.Name))
				}
				return nil // fall through to agent loop with phase prompt
			}
		}
	}

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

		// Stash the original task request so the next phase's agent loop
		// uses it as userText instead of the confirm message ("开工"/"确认").
		if ws := engine.GetActiveWorkflow(userID); ws != nil {
			if orig := extractOriginalRequest(ws, ""); orig != "" {
				h.workflowOriginalRequest.Store(userID, orig)
			}
		}

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
		return h.handlePostStartWorkflow(engine, userID, text, state, reply)
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

	// ── Unified classification: UIC fusion produces both IntentLabel and WorkflowType ──
	//
	// The UIC's L3 tree reasoning channel now outputs workflow_type alongside
	// the intent label in a single LLM call. This eliminates the need for a
	// separate IUM LLM call (10-30s) to determine workflow type.
	//
	// Decision flow:
	//   1. UIC.Classify() → ClassificationResult with Primary + WorkflowType
	//   2. WorkflowType non-empty → directly start workflow (skip IUM LLM call)
	//   3. WorkflowType empty → not a workflow task → return nil (normal agent loop)
	//
	// L1 keywords no longer short-circuit the fusion pipeline. Even if L1
	// misclassifies "宣传ppt" as non_coding, L2+L3 fusion corrects it to
	// office with workflow_type="presentation_design".
	if uic := h.getUnifiedClassifier(); uic != nil {
		uicResult := uic.Classify(intent.MessageContext{
			Text:   text,
			UserID: userID,
		})

		// UIC determined a specific workflow type via L3 tree reasoning.
		if uicResult.WorkflowType != "" && uicResult.WorkflowType != "none" {
			log.Printf("[WorkflowInterception] UIC fusion determined workflow for user %s: "+
				"intent=%s conf=%.2f layer=%d workflow_type=%s text=%q",
				userID, uicResult.Primary, uicResult.Confidence, uicResult.Layer,
				uicResult.WorkflowType, truncateRunes(text, 80))

			wfType := workflow.WorkflowType(uicResult.WorkflowType)
			registry := engine.GetRegistry()
			if registry != nil && registry.Match(wfType) != nil {
				// Valid workflow type — start workflow directly, skip IUM LLM call.
				wfIntent := workflow.StructuredIntent{
					Category:   wfType,
					Summary:    fmt.Sprintf("UIC fusion: %s (conf=%.2f)", uicResult.Primary, uicResult.Confidence),
					Goals:      []string{text},
					Confidence: uicResult.Confidence,
				}
				state, err := engine.StartWorkflow(userID, wfIntent)
				if err != nil {
					log.Printf("[WorkflowInterception] UIC-driven StartWorkflow error for user %s: %v", userID, err)
					// Fall through to IUM for deeper analysis rather than
					// dropping to normal agent loop — the user clearly wants
					// a workflow task.
				} else {
					return h.handlePostStartWorkflow(engine, userID, text, state, "")
				}
			}
			// Unknown workflow type — fall through to IUM for clarification.
			log.Printf("[WorkflowInterception] UIC workflow_type %q not found in registry, falling through to IUM", uicResult.WorkflowType)
		}

		// UIC says no workflow — check if it's a confident non-workflow signal.
		// Only reject if the classification came from fusion (layer >= 2),
		// not from L1 keywords alone (layer == 1 in degraded mode).
		if uicResult.WorkflowType == "" && uicResult.Layer >= 2 {
			threshold := uic.GetWorkflowRejectThreshold()
			if uicResult.Confidence >= threshold && !uic.IsWorkflowCandidate(uicResult.Primary) {
				log.Printf("[WorkflowInterception] UIC fusion rejected workflow for user %s: "+
					"intent=%s conf=%.2f layer=%d threshold=%.2f — text=%q",
					userID, uicResult.Primary, uicResult.Confidence, uicResult.Layer, threshold,
					truncateRunes(text, 80))
				return nil
			}
		}

		// UIC not confident enough or ambiguous — fall through to IUM for deeper analysis.
		log.Printf("[WorkflowInterception] UIC fusion: intent=%s conf=%.2f layer=%d wf=%s — "+
			"not decisive, proceeding to IUM",
			uicResult.Primary, uicResult.Confidence, uicResult.Layer, uicResult.WorkflowType)
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
	if result.Rejected {
		log.Printf("[WorkflowInterception] understanding rejected task for user %s: %q — trusting LLM, no keyword override", userID, truncateRunes(text, 80))
		return nil
	}

	// LLM says it IS a workflow task — an understanding session has been
	// created. The user will go through multi-round clarification before
	// the workflow actually starts.
	return &IMAgentResponse{Text: result.Reply}
}

// getUnifiedClassifier returns the UIC instance if available, or nil.
// Checks the direct field first (set at construction or by TUI), then
// falls back to h.app for GUI late-init compatibility.
func (h *IMMessageHandler) getUnifiedClassifier() *intent.UnifiedIntentClassifier {
	if h.unifiedClassifier != nil {
		return h.unifiedClassifier
	}
	if h.app == nil {
		return nil
	}
	return h.app.unifiedClassifier
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
	return h.handlePostStartWorkflow(engine, userID, text, state, "")
}

// cancelWorkflowForUser cancels any active workflow and understanding session
// for the given user. Called from /new, /reset, /clear, and /cancel handlers.
func (h *IMMessageHandler) cancelWorkflowForUser(userID string) {
	engine := h.getWorkflowEngine()
	if engine == nil {
		return
	}
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
	engine := h.getWorkflowEngine()
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
	entries := h.memory.Load(userID)
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
