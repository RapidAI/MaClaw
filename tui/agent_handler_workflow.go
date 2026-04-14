package main

import (
	"fmt"
	"log"

	"github.com/RapidAI/CodeClaw/corelib/workflow"
)

// handleTUIWorkflowInterception checks if the message should be handled by the
// workflow engine (corelib/workflow). Returns an *AgentResponse if the message
// was fully handled, or nil if it should proceed to the normal agent loop.
//
// Called from RunAgentLoop after LLM config check, before building the system
// prompt and entering the LLM call loop.
func (h *TUIAgentHandler) handleTUIWorkflowInterception(text string) *AgentResponse {
	engine := h.workflowEngine
	if engine == nil {
		return nil
	}
	filter := engine.GetFilter()
	if filter == nil {
		return nil
	}

	userID := "tui-user" // TUI is single-user
	classification := filter.Classify(userID, text)

	switch classification {
	case workflow.FilterActiveWorkflow:
		resp, err := engine.HandleInput(userID, text)
		if err != nil {
			log.Printf("[TUIWorkflow] HandleInput error: %v", err)
			return &AgentResponse{Error: fmt.Sprintf("工作流处理出错: %v", err)}
		}
		if resp != nil && !resp.RunAgentLoop {
			return &AgentResponse{Text: resp.Text}
		}
		// Stash the custom PhasePrompt (e.g. modify requests) so the
		// system-prompt builder can use it instead of rebuilding a generic one.
		if resp != nil && resp.PhasePrompt != "" {
			h.stashedPhasePrompt = resp.PhasePrompt
		}
		// Print phase transition text before the agent loop runs.
		if resp != nil && resp.Advance && resp.Text != "" {
			fmt.Println(resp.Text)
		}
		return nil // proceed to normal agent loop

	case workflow.FilterActiveUnderstanding:
		understanding := engine.GetUnderstanding()
		if understanding == nil {
			return nil
		}
		reply, ready, cancelled, intent, err := understanding.HandleInput(userID, text)
		if err != nil {
			log.Printf("[TUIWorkflow] understanding HandleInput error: %v", err)
			return &AgentResponse{Error: fmt.Sprintf("意图理解出错: %v", err)}
		}
		if cancelled {
			return &AgentResponse{Text: "已取消。"}
		}
		if ready && intent != nil {
			state, err := engine.StartWorkflow(userID, *intent)
			if err != nil {
				log.Printf("[TUIWorkflow] StartWorkflow error: %v", err)
				return &AgentResponse{Error: fmt.Sprintf("启动工作流失败: %v", err)}
			}
			overview := fmt.Sprintf("🚀 工作流已启动：%s\n当前阶段：%s", state.Type, state.CurrentPhase)
			if reply != "" {
				overview += "\n\n" + reply
			}
			return &AgentResponse{Text: overview}
		}
		return &AgentResponse{Text: reply}

	case workflow.FilterNeedsUnderstanding:
		understanding := engine.GetUnderstanding()
		if understanding == nil {
			return nil
		}
		reply, err := understanding.Start(userID, text)
		if err != nil {
			log.Printf("[TUIWorkflow] understanding Start error: %v", err)
			return nil // fallback to normal agent loop
		}
		return &AgentResponse{Text: reply}

	case workflow.FilterSmallTalk, workflow.FilterSimpleDirective:
		return nil // pass through to normal agent loop
	}

	return nil
}

// cancelTUIWorkflow cancels any active workflow for the TUI user.
func (h *TUIAgentHandler) cancelTUIWorkflow() {
	if h.workflowEngine == nil {
		return
	}
	userID := "tui-user"
	_ = h.workflowEngine.CancelWorkflow(userID)
	if understanding := h.workflowEngine.GetUnderstanding(); understanding != nil {
		if understanding.HasActiveSession(userID) {
			_, _, _, _, _ = understanding.HandleInput(userID, "取消")
		}
	}
}

// captureWorkflowOutput stores the agent loop output in the workflow's
// PhaseOutputs if there's an active workflow. TUI has no split-pane UI
// but the output is persisted for potential future use.
func (h *TUIAgentHandler) captureWorkflowOutput(text string) {
	if h.workflowEngine == nil || text == "" {
		return
	}
	h.workflowEngine.SavePhaseOutput("tui-user", text)
}

// tuiDocOnlyAllowedTools is the set of tool names allowed during doc_only phases.
var tuiDocOnlyAllowedTools = map[string]bool{
	"bash":       true,
	"write_file": true,
	"read_file":  true,
	"edit_file":  true,
	"memory":     true,
	"web_search": true,
	"web_fetch":  true,
	"open":       true,
}

// applyTUIWorkflowToolFilter restricts the tool list based on the current
// workflow phase's ToolFilterPolicy.
func (h *TUIAgentHandler) applyTUIWorkflowToolFilter(tools []map[string]interface{}) []map[string]interface{} {
	if h.workflowEngine == nil {
		return tools
	}
	policy := h.workflowEngine.GetPhaseToolFilter("tui-user")
	if policy != workflow.ToolFilterDocOnly {
		return tools
	}
	filtered := make([]map[string]interface{}, 0, len(tools))
	for _, def := range tools {
		name := ""
		if fn, ok := def["function"].(map[string]interface{}); ok {
			name, _ = fn["name"].(string)
		}
		if tuiDocOnlyAllowedTools[name] {
			filtered = append(filtered, def)
		}
	}
	if len(filtered) == 0 {
		return tools
	}
	return filtered
}
