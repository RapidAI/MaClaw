package main

// browser_tool_diagnostic.go — Diagnostic logging for browser tool lifecycle.
//
// Browser: prefix hallucination has been fixed multiple times (#27, #34, #45,
// #47, #50, #79) but keeps recurring. The root cause is hard to pinpoint
// because the browser tool lifecycle spans 7 layers, and a leak at ANY layer
// can cause the hallucination. This file provides structured diagnostic
// logging at each checkpoint so the next occurrence can be traced to the
// exact layer that failed.
//
// The 7 checkpoints (in data flow order):
//
//   CP1: Route()           — conditional keep rules + semantic confirm + session pin
//   CP2: WorkflowFilter    — applyWorkflowToolFilter (doc_only whitelist)
//   CP3: CodingGate        — codingToolBlocklist filtering
//   CP4: FinalToolList     — the actual tool names sent to LLM
//   CP5: StreamFilter      — rolePrefixStreamFilter detection
//   CP6: PostProcess       — stripRolePrefixHallucination cleanup
//   CP7: FinalOutput       — what reaches the frontend
//
// Usage: call browserDiagCheckpoint() at each layer. All output goes to
// ~/.maclaw/logs/maclaw.log with the [browser-diag] prefix.
// The diagnostic is zero-cost when no browser tool is involved (fast-path
// string check before any formatting).

import (
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// browserDiagToolNames is the set of tool names that trigger diagnostic logging.
// Derived from noEagerPinTools (which is itself derived from conditionalKeepRules
// with noMemoryPin=true). This is the single source of truth — any rule marked
// noMemoryPin=true automatically gets diagnostic coverage.
var browserDiagToolNames map[string]bool

func init() {
	browserDiagToolNames = make(map[string]bool)
	for _, name := range tool.NoEagerPinToolNames() {
		browserDiagToolNames[name] = true
	}
}

// browserDiagExtractNames extracts tool names from a tool definition list.
// Returns only browser-related names found.
func browserDiagExtractNames(tools []map[string]interface{}) []string {
	var found []string
	for _, t := range tools {
		name := tool.ExtractToolName(t)
		if browserDiagToolNames[name] {
			found = append(found, name)
		}
	}
	return found
}

// browserDiagHasBrowserPrefix checks if text contains a Browser: role prefix.
func browserDiagHasBrowserPrefix(text string) bool {
	return strings.Contains(text, "Browser:") || strings.Contains(text, "Browser：")
}

// --- Checkpoint logging functions ---

// BrowserDiagCP1_Route logs the Route() output for browser tools.
// Called after routeTools() returns.
//
// Parameters:
//   - userMsg: the user message (truncated for logging)
//   - routedTools: the tool list returned by routeTools()
//   - sessionPinned: whether "browser" is session-pinned
//   - reason: how browser tools got into the list (keyword/semantic/session-pin/none)
func BrowserDiagCP1_Route(userMsg string, routedTools []map[string]interface{}, sessionPinned bool) {
	found := browserDiagExtractNames(routedTools)
	if len(found) == 0 && !sessionPinned {
		return // fast path: no browser involvement
	}

	msgPreview := truncateDiagMsg(userMsg, 80)
	if len(found) > 0 {
		log.Printf("[browser-diag] CP1_Route: browser tools IN routed list: %v | sessionPinned=%v | msg=%q",
			found, sessionPinned, msgPreview)
	} else {
		log.Printf("[browser-diag] CP1_Route: browser tools NOT in routed list | sessionPinned=%v | msg=%q",
			sessionPinned, msgPreview)
	}
}

// BrowserDiagCP2_WorkflowFilter logs the effect of applyWorkflowToolFilter.
// Called after workflow tool filtering.
//
// Parameters:
//   - beforeCount: browser tool count before filtering
//   - afterTools: the tool list after filtering
//   - policy: the workflow tool filter policy applied (e.g. "doc_only", "full", "none")
//   - skipped: whether filtering was skipped (SkipNeedsConfirmGate)
func BrowserDiagCP2_WorkflowFilter(beforeCount int, afterTools []map[string]interface{}, policy string, skipped bool) {
	if beforeCount == 0 {
		return // fast path: no browser tools before filtering
	}
	afterFound := browserDiagExtractNames(afterTools)
	if beforeCount == len(afterFound) && !skipped {
		return // no change, skip noise
	}
	log.Printf("[browser-diag] CP2_WorkflowFilter: before=%d after=%d policy=%s skipped=%v surviving=%v",
		beforeCount, len(afterFound), policy, skipped, afterFound)
}

// BrowserDiagCP3_CodingGate logs the effect of coding tool gate filtering.
// Called after coding gate filtering.
//
// Parameters:
//   - beforeCount: browser tool count before gate filtering
//   - afterTools: the tool list after gate filtering
//   - gateActive: whether the coding gate was active
//   - skipReason: why the gate was skipped (empty if not skipped)
func BrowserDiagCP3_CodingGate(beforeCount int, afterTools []map[string]interface{}, gateActive bool, skipReason string) {
	if beforeCount == 0 {
		return // fast path
	}
	afterFound := browserDiagExtractNames(afterTools)
	if beforeCount == len(afterFound) && gateActive {
		// Gate was active but didn't remove browser tools — this is a bug signal
		log.Printf("[browser-diag] CP3_CodingGate: ⚠️ gate ACTIVE but browser tools survived: before=%d after=%d surviving=%v",
			beforeCount, len(afterFound), afterFound)
		return
	}
	if beforeCount != len(afterFound) || skipReason != "" {
		log.Printf("[browser-diag] CP3_CodingGate: before=%d after=%d gateActive=%v skipReason=%q surviving=%v",
			beforeCount, len(afterFound), gateActive, skipReason, afterFound)
	}
}

// BrowserDiagCP4_FinalToolList logs the final tool list sent to LLM.
// This is the most critical checkpoint — if browser tools are here, the LLM
// sees them and may produce Browser: hallucinations.
// Called right before the LLM request.
//
// Parameters:
//   - tools: the final tool list
//   - iteration: the agent loop iteration number
//   - totalToolCount: total number of tools in the list
func BrowserDiagCP4_FinalToolList(tools []map[string]interface{}, iteration int, totalToolCount int) {
	found := browserDiagExtractNames(tools)
	if len(found) == 0 {
		return // fast path
	}
	// This is a WARNING — browser tools in the final LLM tool list is the
	// primary trigger for Browser: hallucinations.
	log.Printf("[browser-diag] CP4_FinalToolList: ⚠️ browser tools PRESENT in LLM tool list: %v | iteration=%d totalTools=%d",
		found, iteration, totalToolCount)
}

// BrowserDiagCP5_StreamFilter logs stream filter detection results.
// Called after the stream completes (in the stream handler).
//
// Parameters:
//   - rpfHalted: whether the role prefix filter halted
//   - rpfSuppressed: number of runes suppressed by role prefix filter
//   - repHalted: whether the repetition filter halted
//   - repSuppressed: number of runes suppressed by repetition filter
//   - rawHasBrowser: whether the raw content (contentBuf) contains Browser: prefix
//   - filteredHasBrowser: whether the filtered content (filteredBuf) contains Browser: prefix
//   - filteredContent: the filtered content string (only used for context extraction when filteredHasBrowser=true)
func BrowserDiagCP5_StreamFilter(rpfHalted bool, rpfSuppressed int, repHalted bool, repSuppressed int, rawHasBrowser, filteredHasBrowser bool, filteredContent string) {
	if !rawHasBrowser && !filteredHasBrowser && !rpfHalted {
		return // fast path: no browser prefix anywhere
	}

	log.Printf("[browser-diag] CP5_StreamFilter: rawHasBrowserPrefix=%v filteredHasBrowserPrefix=%v rpfHalted=%v rpfSuppressed=%d repHalted=%v repSuppressed=%d",
		rawHasBrowser, filteredHasBrowser, rpfHalted, rpfSuppressed, repHalted, repSuppressed)

	if filteredHasBrowser {
		log.Printf("[browser-diag] CP5_StreamFilter: ⚠️ Browser: prefix LEAKED through stream filters into filteredBuf")
		// Log context around the prefix in filtered content
		idx := strings.Index(filteredContent, "Browser:")
		if idx < 0 {
			idx = strings.Index(filteredContent, "Browser：")
		}
		if idx >= 0 {
			start := idx - 50
			if start < 0 {
				start = 0
			}
			end := idx + 80
			if end > len(filteredContent) {
				end = len(filteredContent)
			}
			log.Printf("[browser-diag] CP5_StreamFilter: filtered Browser: context: ...%q...", filteredContent[start:end])
		}
	}
}

// BrowserDiagCP6_PostProcess logs the effect of stripRolePrefixHallucination.
// Called after post-processing.
//
// Parameters:
//   - beforeContent: content before stripRolePrefixHallucination
//   - afterContent: content after stripRolePrefixHallucination
func BrowserDiagCP6_PostProcess(beforeContent, afterContent string) {
	beforeHas := browserDiagHasBrowserPrefix(beforeContent)
	afterHas := browserDiagHasBrowserPrefix(afterContent)

	if !beforeHas && !afterHas {
		return // fast path
	}

	if beforeHas && !afterHas {
		log.Printf("[browser-diag] CP6_PostProcess: Browser: prefix stripped by post-processing (before=%d chars, after=%d chars)",
			len(beforeContent), len(afterContent))
	} else if beforeHas && afterHas {
		log.Printf("[browser-diag] CP6_PostProcess: ⚠️ Browser: prefix SURVIVED post-processing (before=%d chars, after=%d chars)",
			len(beforeContent), len(afterContent))
		// Log context around the surviving prefix
		idx := strings.Index(afterContent, "Browser:")
		if idx < 0 {
			idx = strings.Index(afterContent, "Browser：")
		}
		if idx >= 0 {
			start := idx - 30
			if start < 0 {
				start = 0
			}
			end := idx + 60
			if end > len(afterContent) {
				end = len(afterContent)
			}
			log.Printf("[browser-diag] CP6_PostProcess: surviving context: ...%q...", afterContent[start:end])
		}
	}
}

// BrowserDiagCP7_FinalOutput logs the final output that reaches the frontend.
// Called when building the IMAgentResponse.
//
// Parameters:
//   - text: the final response text
//   - source: where the text came from ("msgContent", "hardExit", "cancel", etc.)
func BrowserDiagCP7_FinalOutput(text string, source string) {
	if !browserDiagHasBrowserPrefix(text) {
		return // fast path
	}
	log.Printf("[browser-diag] CP7_FinalOutput: ⚠️ Browser: prefix in FINAL output | source=%s | len=%d",
		source, len(text))
	// Log context around the prefix
	idx := strings.Index(text, "Browser:")
	if idx < 0 {
		idx = strings.Index(text, "Browser：")
	}
	if idx >= 0 {
		start := idx - 30
		if start < 0 {
			start = 0
		}
		end := idx + 80
		if end > len(text) {
			end = len(text)
		}
		log.Printf("[browser-diag] CP7_FinalOutput: context: ...%q...", text[start:end])
	}
}

// --- Helper ---

func truncateDiagMsg(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}
