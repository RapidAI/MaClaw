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
	ownerID := h.workflowPolicyOwnerID(userID, ctx)

	orchInst := h.taskOrchestratorRegistry.Get(ownerID)
	if orchInst == nil || !orchInst.IsActive() {
		return result
	}
	if allowed, reason := h.workflowAllowsSubAgentExecutionForOwner(ownerID); !allowed {
		log.Printf("[agent-loop] skipped task orchestrator injection by workflow policy user=%s owner=%s reason=%s", userID, ownerID, reason)
		h.deactivateTaskOrchestratorForWorkflowPolicyBlock(ownerID, reason)
		return result
	}

	handles := orchInst.ReadyTaskHandles(1)
	if len(handles) == 0 {
		return result
	}
	handle := handles[0]
	mode, ok := orchInst.ResolveExecutionModeForTaskRun(handle.Task, handle.RunID)
	if ok && mode == TaskExecModeDirect && !result.DirectModeToolsFiltered {
		directFiltered := filterDirectModeAllowedTools(result.Tools)
		if len(directFiltered) < len(result.Tools) {
			log.Printf("[agent-loop] direct-mode: stripped %d session tools from tool list", len(result.Tools)-len(directFiltered))
			result.Tools = directFiltered
		}
		result.DirectModeToolsFiltered = true
	}

	if taskInjection := orchInst.BuildSystemInjectionForTaskRun(handle.Task, handle.RunID); taskInjection != "" {
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
