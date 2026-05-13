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
func (h *IMMessageHandler) augmentToolsFromInjection(injectionText string, currentTools, baseTools []map[string]interface{}, gateActive bool) ([]map[string]interface{}, int) {
	if injectionText == "" || h.toolRouter == nil {
		return currentTools, estimateToolsTokens(currentTools)
	}

	// Strip injection prefix (e.g. "[用户补充] ", "[用户补充需求——请在当前任务中纳入] ")
	// before routing. The prefix is an LLM instruction marker, not part of
	// the user's actual message. Passing it to Route/UIC would pollute intent
	// classification (embedding similarity, keyword matching, etc.).
	routeText := stripInjectionPrefix(injectionText)
	if routeText == "" {
		return currentTools, estimateToolsTokens(currentTools)
	}

	// Route with the cleaned injection text to see what tools it would activate.
	allTools := h.getTools()
	injectionRouted := h.routeTools(routeText, allTools)

	// Build a set of tool names currently available.
	currentNames := make(map[string]bool, len(currentTools))
	for _, t := range currentTools {
		if name, ok := t["name"].(string); ok {
			currentNames[name] = true
		}
	}

	// Find tools in the injection routing result that are missing from current.
	var added []string
	for _, t := range injectionRouted {
		name, ok := t["name"].(string)
		if !ok || name == "" {
			continue
		}
		if currentNames[name] {
			continue
		}
		// Respect the coding gate: don't augment blocklisted tools when gate
		// is active. This prevents injection from bypassing the three-phase
		// workflow enforcement.
		if gateActive && codingToolBlocklist[name] {
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

	return currentTools, estimateToolsTokens(currentTools)
}

// findToolDef finds a tool definition by name in a tool list.
func findToolDef(tools []map[string]interface{}, name string) map[string]interface{} {
	for _, t := range tools {
		if n, ok := t["name"].(string); ok && n == name {
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
	// All injection prefixes follow the pattern: "[...] " (brackets + space).
	// Find the first "] " and strip everything up to and including it.
	if len(text) > 0 && text[0] == '[' {
		idx := strings.Index(text, "] ")
		if idx >= 0 && idx < 60 { // sanity: prefix shouldn't be longer than 60 bytes
			return strings.TrimSpace(text[idx+2:])
		}
	}
	return text
}
