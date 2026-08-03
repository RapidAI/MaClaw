package main

import (
	"log"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func filterDirectModeAllowedTools(tools []map[string]interface{}) []map[string]interface{} {
	var filtered []map[string]interface{}
	for _, t := range tools {
		name := tool.ExtractToolName(t)
		if !isDirectModeBlockedTool(name) {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

func (h *IMMessageHandler) restoreToolsAfterSkillRecover(userID string, ctx *LoopContext, baseTools []map[string]interface{}, phase agentLoopPhase) ([]map[string]interface{}, int, bool) {
	tools := baseTools
	directModeToolsFiltered := false

	if h.taskOrchestratorRegistry != nil {
		orchInst := h.taskOrchestratorRegistry.Get(userID)
		if orchInst != nil {
			handles := orchInst.ReadyTaskHandles(1)
			mode := TaskExecModeExternal
			ok := false
			if len(handles) > 0 {
				mode, ok = orchInst.ResolveExecutionModeForTaskRun(handles[0].Task, handles[0].RunID)
			}
			if ok && mode == TaskExecModeDirect {
				tools = filterDirectModeAllowedTools(tools)
				directModeToolsFiltered = true
			}
		}
	}

	if len(phase.TruncationBlockedTools) > 0 {
		var truncFiltered []map[string]interface{}
		for _, t := range tools {
			name := tool.ExtractToolName(t)
			if !phase.TruncationBlockedTools[name] {
				truncFiltered = append(truncFiltered, t)
			}
		}
		if len(truncFiltered) < len(tools) {
			log.Printf("[agent-loop] re-applied truncation block after baseTools reset: removed %d tools", len(tools)-len(truncFiltered))
			tools = truncFiltered
		}
	}

	if _, applyFilter := h.workflowToolFilterOwnerAndDecision(userID, nil); applyFilter {
		tools = h.applyWorkflowToolFilterWithCatalog(userID, tools, h.getTools())
	}
	// Recover rebuilds from BaseTools, which intentionally predates the normal
	// group filter. Re-apply the group boundary here so a failed skill cannot
	// reveal unsafe local tools, and restore the two group-safe retrieval
	// primitives in case routing had omitted them.
	if ctx != nil && ctx.LansengerGroupPermissions != nil {
		tools = h.ensureLansengerGroupMemoryRecallTool(userID, tools)
		if ctx.LansengerGroupPermissions.allowsKnowledge() {
			tools = h.ensureLansengerGroupKnowledgeSearchTool(userID, tools)
		}
		tools = filterToolsForLansengerGroupPermissions(tools, *ctx.LansengerGroupPermissions)
	}

	tools = stripExecutionContractMetadataForLLM(tools)
	return tools, estimateToolsTokens(tools), directModeToolsFiltered
}
