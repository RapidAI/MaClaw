package main

import (
	"log"
	"strings"
)

// augmentToolsFromInjection replaces the legacy surface with one fresh route
// result. It must never merge the old list with the injection route: doing so
// makes an earlier task's tool names authorization for a later task and lets
// a repeated injection steadily exhaust the tool budget. Managed semantic
// turns remain closed until their planner publishes a replacement surface.
func (h *IMMessageHandler) augmentToolsFromInjection(ctx *LoopContext, userID, injectionText string, currentTools, baseTools []map[string]interface{}, gateActive bool) ([]map[string]interface{}, int) {
	if injectionText == "" {
		return currentTools, estimateToolsTokens(currentTools)
	}
	if loopContextBlocksLegacyToolRouter(ctx) {
		log.Printf("[injection-tool-augment] skip name-router augment on managed semantic turn user=%q", userID)
		return currentTools, estimateToolsTokens(currentTools)
	}
	if h.toolRouter == nil {
		return h.finalizeInjectionAugmentedTools(ctx, userID, currentTools)
	}

	// Strip injection prefix (e.g. "[用户补充] ", "[用户补充需求——请在当前任务中纳入] ")
	// before routing. The prefix is an LLM instruction marker, not part of
	// the user's actual message. Passing it to Route/UIC would pollute intent
	// classification (embedding similarity, keyword matching, etc.).
	routeText := stripInjectionPrefix(injectionText)
	if routeText == "" {
		return h.finalizeInjectionAugmentedTools(ctx, userID, currentTools)
	}

	// Route the current task direction into a complete replacement candidate.
	allTools := h.getTools()
	// Route augmentation happens while an Agent loop is active. Keep this on
	// the same non-blocking BM25/L2-only path as the initial tool set.
	replacement := h.routeToolsForUser("", routeText, allTools, true)
	log.Printf("[injection-tool-augment] replaced legacy surface with %d routed tools", len(replacement))
	return h.finalizeInjectionAugmentedTools(ctx, userID, replacement)
}

func (h *IMMessageHandler) finalizeInjectionAugmentedTools(ctx *LoopContext, userID string, tools []map[string]interface{}) ([]map[string]interface{}, int) {
	if policyOwnerID, applyFilter := h.workflowToolFilterOwnerAndDecision(userID, ctx); applyFilter {
		tools = h.applyWorkflowToolFilterWithCatalog(policyOwnerID, tools, h.getTools())
	}
	// Re-apply the expert allow-list as well — injection re-routing must not
	// pull tools outside the expert's whitelist back into the loop.
	tools = h.filterToolsForExpertUser(userID, tools)
	// Steering can re-route tools after the initial list was built. Re-apply
	// the group boundary so an injected request cannot restore local tools.
	if ctx != nil && ctx.LansengerGroupPermissions != nil {
		tools = h.ensureLansengerGroupMemoryRecallTool(userID, tools)
		if ctx.LansengerGroupPermissions.allowsKnowledge() {
			tools = h.ensureLansengerGroupKnowledgeSearchTool(userID, tools)
		}
		tools = filterToolsForLansengerGroupPermissions(tools, *ctx.LansengerGroupPermissions)
	}
	tools = filterComputerUseToolsForLocalFileWork(ctx, "", tools)
	tools = applyRoutingMissLeftoverTools(tools, leftoverToolCatalog(h, ctx, nil), ctx)
	tools = h.pinClassifierTimeoutWebLookup(userID, ctx, tools, h.filterPolicyRejectedSurfaceTools(h.getTools()))
	tools = stripExecutionContractMetadataForLLM(tools)
	// Injection is a new direction, so it must receive a complete replacement
	// surface. Do not return the post-filter raw definitions on a planner error:
	// that would make the old compatibility path an authorization fallback.
	rendered, _, planBacked, err := h.renderClosedLegacyReplacementSurface(injectionReplacementPolicyText(ctx, tools), ctx, tools, nil)
	if err != nil || !planBacked {
		if err != nil {
			log.Printf("[legacy-adapter] injection replacement rejected user=%q reason=%v", userID, err)
		}
		return nil, 0
	}
	return rendered, estimateToolsTokens(rendered)
}

func injectionReplacementPolicyText(ctx *LoopContext, tools []map[string]interface{}) string {
	// The exact schemas are bound by LegacyAdapterPlan. This digest input only
	// identifies the host policy decision, therefore it may use the current
	// request/turn routing text without accepting a historical surface.
	if ctx != nil && strings.TrimSpace(ctx.ComputerUseRoutingText) != "" {
		return ctx.ComputerUseRoutingText
	}
	return strings.Join(agentLoopToolNamesForLog(tools), ",")
}

// findToolDef finds a tool definition by name in a tool list.
// Supports both flat format ({"name": "x"}) and OpenAI nested format
// ({"type": "function", "function": {"name": "x"}}).
func findToolDef(tools []map[string]interface{}, name string) map[string]interface{} {
	for _, t := range tools {
		if n := extractToolName(t); n == name {
			return t
		}
	}
	return nil
}

// stripInjectionPrefix removes the system-added injection prefix from text
// before passing it to tool routing. The prefix is one of:
//   - "[用户要求修改——必须立即执行] "
//   - "[用户补充需求——请在当前任务中纳入] "
//   - "[用户补充] "
//
// These prefixes are added by classifyMergeInjection and InjectSupplementary
// for LLM instruction-following purposes. They must be stripped before intent
// classification to avoid polluting the signal.
func stripInjectionPrefix(text string) string {
	text = stripGuideLaunchReferenceWrappers(text)

	// All legacy injection prefixes follow the pattern: "[...] " (brackets + space).
	// Find the first "] " and strip everything up to and including it.
	if len(text) > 0 && text[0] == '[' {
		idx := strings.Index(text, "] ")
		if idx >= 0 && idx < 60 { // sanity: prefix shouldn't be longer than 60 bytes
			return strings.TrimSpace(text[idx+2:])
		}
	}
	return text
}

func stripGuideLaunchReferenceWrappers(text string) string {
	if !strings.Contains(text, guideLaunchReferenceMarker) {
		return text
	}
	lines := strings.Split(text, "\n")
	kept := make([]string, 0, len(lines))
	for i := 0; i < len(lines); i++ {
		if isGuideLaunchReferenceHeader(lines, i) {
			i++
			continue
		}
		kept = append(kept, lines[i])
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

// augmentToolsFromSessionPins is intentionally a no-op. Session pins used to
// union successful legacy tool names into the current model surface, which
// made the surface history-dependent and could silently exhaust the budget.
// A discover/injection that changes the task must request a fresh plan; it may
// not make a newly found name executable in the same loop iteration.
func (h *IMMessageHandler) augmentToolsFromSessionPins(ctx *LoopContext, userID string, currentTools []map[string]interface{}, currentBudget int) ([]map[string]interface{}, int) {
	_ = h
	_ = ctx
	_ = userID
	return currentTools, currentBudget
}
