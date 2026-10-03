package guiapp

import (
	"log"
	"strings"

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
	directModeToolsFiltered := h.mainLoopInDirectMode(userID, ctx)

	catalog := h.unmanagedLegacyHostCatalog()
	if ownerID, applyFilter := h.workflowToolFilterOwnerAndDecision(userID, ctx); applyFilter {
		tools = h.applyWorkflowToolFilterWithCatalog(ownerID, tools, catalog)
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
	tools = filterComputerUseToolsForLocalFileWork(ctx, "", tools)
	tools = applyRoutingMissLeftoverTools(tools, leftoverToolCatalog(h, ctx, nil), h.routingMissFloorDefinitions(), ctx)
	lookupCatalog := h.filterPolicyRejectedSurfaceTools(catalog)
	tools = h.pinClassifierTimeoutWebLookup(userID, ctx, tools, lookupCatalog)
	tools = h.pinClassifierTimeoutExecutionFloor(userID, ctx, tools, lookupCatalog)
	// Workflow ensure and the timeout floor pin both run above and can put
	// bash back. Re-apply the filters that must stick. The timeout path
	// seals with the host catalog because render follows. An ordinary
	// recover must not run that ensure a second time.
	if loopContextHasClassifierTimeoutLookup(ctx) && executionSurfaceIsFull(executionProfileFromLoop(ctx)) {
		tools = h.sealClassifierTimeoutExecutionFloor(userID, ctx, tools, phase, directModeToolsFiltered, nil)
	} else {
		tools = h.filterToolsForExpertUser(userID, tools)
		if directModeToolsFiltered {
			tools = filterDirectModeAllowedTools(tools)
		}
		beforeTruncation := len(tools)
		tools = dropTruncationBlockedTools(tools, phase.TruncationBlockedTools)
		if len(tools) < beforeTruncation {
			log.Printf("[agent-loop] re-applied truncation block after baseTools reset: removed %d tools", beforeTruncation-len(tools))
		}
		tools = h.filterPolicyRejectedSurfaceTools(tools)
	}

	tools = stripExecutionContractMetadataForLLM(tools)
	// Recovery is a fresh model request, not permission to restore the raw
	// BaseTools snapshot. Re-render it as a closed replacement surface so an
	// old candidate or a policy filter cannot bypass the reviewed catalog.
	rendered, _, planBacked, err := h.renderClosedLegacyReplacementSurface(strings.Join(agentLoopToolNamesForLog(tools), ","), ctx, tools, nil)
	if err != nil || !planBacked {
		if err != nil {
			log.Printf("[legacy-adapter] recovery replacement rejected user=%q reason=%v", userID, err)
		}
		return nil, 0, directModeToolsFiltered
	}
	return rendered, estimateToolsTokens(rendered), directModeToolsFiltered
}
