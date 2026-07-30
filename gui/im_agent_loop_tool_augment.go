package main

import (
	"log"
	"strings"
)

// augmentToolsFromInjection performs incremental tool routing when a merge
// injection changes the task direction mid-loop.
//
// The tool list is computed once at loop start based on the original user
// message. When a merge injection arrives (e.g. "直接用ssh连上服务器"), the
// injection text may require tools (ssh, browser, etc.) that weren't in the
// original routing result. This function re-routes using the injection text
// and appends any newly activated tools to the current tool set.
//
// Mechanism: Route() with injection text → find tools in the result that are
// NOT in the current set → filter out blocklisted tools (coding gate) →
// append remaining to current tools.
//
// This maintains the invariant that "the tool list reflects the current task
// direction" while respecting the coding gate's invariant that "blocklisted
// tools don't appear in the tool list during three-phase workflow".
func (h *IMMessageHandler) augmentToolsFromInjection(ctx *LoopContext, userID, injectionText string, currentTools, baseTools []map[string]interface{}, gateActive bool) ([]map[string]interface{}, int) {
	if injectionText == "" {
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

	// Route with the cleaned injection text to see what tools it would activate.
	allTools := h.getTools()
	injectionRouted := h.routeTools(routeText, allTools)

	// Build a set of tool names currently available.
	currentNames := make(map[string]bool, len(currentTools))
	for _, t := range currentTools {
		if name := extractToolName(t); name != "" {
			currentNames[name] = true
		}
	}

	// Find tools in the injection routing result that are missing from current.
	var added []string
	for _, t := range injectionRouted {
		name := extractToolName(t)
		if name == "" {
			continue
		}
		if currentNames[name] {
			continue
		}
		// Look up the full definition from baseTools (which has all tools
		// before workflow/gate filtering). If not in baseTools, use the
		// definition from injectionRouted directly.
		def := findToolDef(baseTools, name)
		if def == nil {
			def = t
		}
		currentTools = append(currentTools, def)
		currentNames[name] = true
		added = append(added, name)
	}

	if len(added) > 0 {
		log.Printf("[injection-tool-augment] added %d tools from injection: %v", len(added), added)
	}

	return h.finalizeInjectionAugmentedTools(ctx, userID, currentTools)
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
		tools = filterToolsForLansengerGroupPermissions(tools, *ctx.LansengerGroupPermissions)
	}
	tools = stripExecutionContractMetadataForLLM(tools)
	return tools, estimateToolsTokens(tools)
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

// augmentToolsFromSessionPins checks if any session-pinned conditional tools
// are missing from the current tool list and adds their definitions.
//
// This handles the case where discover_tool session-pins a tool (e.g. ssh)
// mid-loop. The tool list was computed at loop start and doesn't include
// tools that weren't activated by the original user message. After session-
// pinning, the next Route() call would include them, but within the same
// loop we need to proactively add them so the LLM can call them immediately.
//
// Respects the workflow tool filter: if the current workflow phase restricts
// tools (e.g. doc_only), newly pinned tools that aren't in the allowed set
// won't be added. This prevents discover_tool from bypassing workflow policy.
func (h *IMMessageHandler) augmentToolsFromSessionPins(ctx *LoopContext, userID string, currentTools []map[string]interface{}, currentBudget int) ([]map[string]interface{}, int) {
	if h == nil || h.toolRouter == nil {
		return currentTools, currentBudget
	}

	// Build set of currently visible tool names.
	currentNames := make(map[string]bool, len(currentTools))
	for _, t := range currentTools {
		if name := extractToolName(t); name != "" {
			currentNames[name] = true
		}
	}

	// Check all session-pinned tools against what's currently visible.
	pinnedMissing := h.toolRouter.SessionPinnedToolsMissing(currentNames)
	if len(pinnedMissing) == 0 {
		return currentTools, currentBudget
	}

	// Fetch fresh tool definitions (cache may have been invalidated by
	// discover_tool) and find definitions for the missing pinned tools.
	allTools := h.getTools()
	var added []string
	for _, name := range pinnedMissing {
		def := findToolDef(allTools, name)
		if def == nil {
			continue
		}
		currentTools = append(currentTools, def)
		currentNames[name] = true
		added = append(added, name)
	}

	if len(added) > 0 {
		// Re-apply workflow tool filter if active — don't let discover_tool
		// bypass workflow phase policy (e.g. doc_only restrictions).
		if policyOwnerID, applyFilter := h.workflowToolFilterOwnerAndDecision(userID, ctx); applyFilter {
			currentTools = h.applyWorkflowToolFilterWithCatalog(policyOwnerID, currentTools, allTools)
		}
		// Re-apply the expert allow-list for the same reason: session-pinned
		// tools outside the expert whitelist must not re-enter the loop.
		currentTools = h.filterToolsForExpertUser(userID, currentTools)
		// discover_tool can pin a conditional tool during a group turn. Keep
		// group permissions last so discovery cannot create a bypass.
		if ctx != nil && ctx.LansengerGroupPermissions != nil {
			currentTools = filterToolsForLansengerGroupPermissions(currentTools, *ctx.LansengerGroupPermissions)
		}
		currentTools = stripExecutionContractMetadataForLLM(currentTools)
		currentBudget = estimateToolsTokens(currentTools)

		// Log which tools actually ended up in the final list (some may have
		// been removed by the workflow filter).
		var effective []string
		finalNames := make(map[string]bool, len(currentTools))
		for _, t := range currentTools {
			if n := extractToolName(t); n != "" {
				finalNames[n] = true
			}
		}
		for _, name := range added {
			if finalNames[name] {
				effective = append(effective, name)
			}
		}
		if len(effective) > 0 {
			log.Printf("[session-pin-augment] added %d session-pinned tools to LLM tool list: %v", len(effective), effective)
		} else {
			log.Printf("[session-pin-augment] %d session-pinned tools discovered but filtered by workflow policy: %v", len(added), added)
		}
	}

	return currentTools, currentBudget
}
