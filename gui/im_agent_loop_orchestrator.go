package main

import "log"

type agentLoopOrchestratorStepResult struct {
	Tools                   []map[string]interface{}
	Conversation            []interface{}
	DirectModeToolsFiltered bool
}

func (h *IMMessageHandler) applyAgentLoopTaskOrchestratorStep(userID string, ctx *LoopContext, tools []map[string]interface{}, conversation []interface{}, directModeToolsFiltered bool) agentLoopOrchestratorStepResult {
	result := agentLoopOrchestratorStepResult{
		Tools:                   tools,
		Conversation:            conversation,
		DirectModeToolsFiltered: directModeToolsFiltered,
	}
	if h == nil || h.taskOrchestratorRegistry == nil {
		return result
	}

	orchInst := h.taskOrchestratorRegistry.Get(userID)
	if orchInst == nil || !orchInst.IsActive() {
		return result
	}

	if orchInst.ResolveExecutionMode() == TaskExecModeDirect && !result.DirectModeToolsFiltered {
		directFiltered := filterDirectModeAllowedTools(result.Tools)
		if len(directFiltered) < len(result.Tools) {
			log.Printf("[agent-loop] direct-mode: stripped %d session tools from tool list", len(result.Tools)-len(directFiltered))
			result.Tools = directFiltered
		}
		result.DirectModeToolsFiltered = true
	}

	if taskInjection := orchInst.BuildSystemInjection(); taskInjection != "" {
		result.Conversation = append(result.Conversation, map[string]string{
			"role":    "system",
			"content": taskInjection,
		})
		if h.traceService != nil && ctx != nil && ctx.RunID != "" {
			h.appendTraceEvent(ctx, "task_orchestrator.injection", "info",
				"Injected per-task guidance", truncateTraceText(taskInjection, 220), "", "")
		}
	}

	return result
}
