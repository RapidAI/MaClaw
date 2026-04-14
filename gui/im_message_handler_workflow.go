package main

import (
	"fmt"
	"log"

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
		// Emit suggest_maximize early so the user sees the fullscreen
		// banner while intent understanding is in progress, rather than
		// waiting until StartWorkflow completes.
		if adapter, ok := engine.GetCallbacks().(*GUIWorkflowAdapter); ok {
			adapter.EmitSuggestMaximize(userID, "coding")
		}
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
		return &IMAgentResponse{Text: resp.Text}
	}
	// RunAgentLoop=true means we need to inject the phase prompt and continue
	// to the normal agent loop. Return nil to let the caller proceed.
	// Stash the custom PhasePrompt (e.g. modify requests include user context)
	// so the system-prompt builder can use it instead of rebuilding a generic one.
	if resp.PhasePrompt != "" {
		h.stashedPhasePrompt.Store(userID, resp.PhasePrompt)
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
		// (May have been emitted earlier when FilterNeedsUnderstanding
		// was first triggered; the frontend handles duplicates gracefully.)
		if adapter, ok := engine.GetCallbacks().(*GUIWorkflowAdapter); ok {
			adapter.EmitSuggestMaximize(userID, string(state.Type))
		}
		overview := fmt.Sprintf("🚀 工作流已启动：%s\n📋 当前阶段：%s", state.Type, state.CurrentPhase)
		if reply != "" {
			overview += "\n\n" + reply
		}
		return &IMAgentResponse{Text: overview}
	}
	return &IMAgentResponse{Text: reply}
}

// handleNeedsUnderstanding starts a new intent understanding session for a
// complex task message.
func (h *IMMessageHandler) handleNeedsUnderstanding(engine *workflow.WorkflowEngine, userID, text string) *IMAgentResponse {
	understanding := engine.GetUnderstanding()
	if understanding == nil {
		// No understanding manager — fall through to normal agent loop.
		return nil
	}

	reply, err := understanding.Start(userID, text)
	if err != nil {
		log.Printf("[WorkflowInterception] understanding Start error for user %s: %v", userID, err)
		// Fallback: let it go to normal agent loop.
		return nil
	}
	return &IMAgentResponse{Text: reply}
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
