package main

// browser_tool_diagnostic.go - Diagnostic logging for browser tool lifecycle.
//
// Browser: prefix hallucination has recurred across several fixes. The root
// cause is hard to pinpoint because browser tool lifecycle spans multiple
// layers, and a leak at any layer can cause the hallucination. This file
// provides structured diagnostic logging at each checkpoint so the next
// occurrence can be traced to the exact layer that failed.
//
// The checkpoints, in data flow order:
//
//   CP1: Route()        - conditional keep rules + semantic confirm + session pin
//   CP2: WorkflowFilter - applyWorkflowToolFilter (doc_only whitelist)
//   CP3: CodingGate     - codingToolBlocklist filtering
//   CP4: FinalToolList  - the actual tool names sent to LLM
//   CP5: StreamFilter   - rolePrefixStreamFilter detection
//   CP6: PostProcess    - stripRolePrefixHallucination cleanup
//   CP7: FinalOutput    - what reaches the frontend
//   CP8: FileDelivery   - file delivery response source and path redaction
//
// All output goes to ~/.maclaw/logs/maclaw.log with the [browser-diag] prefix.
// The diagnostic is zero-cost when no browser tool is involved.

import (
	"log"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

var browserDiagToolNames map[string]bool

func init() {
	browserDiagToolNames = make(map[string]bool)
	for _, name := range tool.NoEagerPinToolNames() {
		browserDiagToolNames[name] = true
	}
}

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

func browserDiagBrowserPrefixIndex(text string) int {
	return browserDiagFirstPrefixIndex(text, []string{"Browser:", "Browser\uFF1A"})
}

func browserDiagHasBrowserRolePrefix(text string) bool {
	return browserDiagBrowserPrefixIndex(text) >= 0
}

func browserDiagRolePrefix(text string) (kind string, idx int, ok bool) {
	candidates := []struct {
		kind    string
		pattern string
	}{
		{"Browser", "Browser:"},
		{"Browser", "Browser\uFF1A"},
		{"Tool", "Tool:"},
		{"Tool", "Tool\uFF1A"},
	}
	firstIdx := -1
	firstKind := ""
	for _, candidate := range candidates {
		currentIdx := browserDiagFirstLineStartPatternIndex(text, candidate.pattern)
		if currentIdx >= 0 && (firstIdx < 0 || currentIdx < firstIdx) {
			firstIdx = currentIdx
			firstKind = candidate.kind
		}
	}
	if firstIdx < 0 {
		return "", -1, false
	}
	return firstKind, firstIdx, true
}

func browserDiagFirstPrefixIndex(text string, patterns []string) int {
	firstIdx := -1
	for _, pattern := range patterns {
		idx := browserDiagFirstLineStartPatternIndex(text, pattern)
		if idx >= 0 && (firstIdx < 0 || idx < firstIdx) {
			firstIdx = idx
		}
	}
	return firstIdx
}

func browserDiagFirstLineStartPatternIndex(text, pattern string) int {
	searchOffset := 0
	for {
		idx := strings.Index(text[searchOffset:], pattern)
		if idx < 0 {
			return -1
		}
		absIdx := searchOffset + idx
		if browserDiagRolePrefixAtLineStart(text, absIdx) {
			return absIdx
		}
		searchOffset = absIdx + len(pattern)
		if searchOffset >= len(text) {
			return -1
		}
	}
}

func browserDiagRolePrefixAtLineStart(text string, idx int) bool {
	if idx < 0 || idx > len(text) {
		return false
	}
	lineStart := strings.LastIndex(text[:idx], "\n") + 1
	prefix := strings.TrimSpace(text[lineStart:idx])
	if prefix == "" {
		return true
	}
	prefix = strings.Trim(prefix, ">*-")
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return true
	}
	if strings.HasSuffix(prefix, ".") {
		number := strings.TrimSpace(strings.TrimSuffix(prefix, "."))
		if number != "" {
			for _, r := range number {
				if r < '0' || r > '9' {
					return false
				}
			}
			return true
		}
	}
	return false
}

func BrowserDiagCP1_Route(userMsg string, routedTools []map[string]interface{}, sessionPinned bool) {
	found := browserDiagExtractNames(routedTools)
	if len(found) == 0 && !sessionPinned {
		return
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

func BrowserDiagCP2_WorkflowFilter(beforeCount int, afterTools []map[string]interface{}, policy string, skipped bool) {
	if beforeCount == 0 {
		return
	}
	afterFound := browserDiagExtractNames(afterTools)
	if beforeCount == len(afterFound) && !skipped {
		return
	}
	log.Printf("[browser-diag] CP2_WorkflowFilter: before=%d after=%d policy=%s skipped=%v surviving=%v",
		beforeCount, len(afterFound), policy, skipped, afterFound)
}

func BrowserDiagCP3_CodingGate(beforeCount int, afterTools []map[string]interface{}, gateActive bool, skipReason string) {
	if beforeCount == 0 {
		return
	}
	afterFound := browserDiagExtractNames(afterTools)
	if beforeCount == len(afterFound) && gateActive {
		log.Printf("[browser-diag] CP3_CodingGate: WARNING gate ACTIVE but browser tools survived: before=%d after=%d surviving=%v",
			beforeCount, len(afterFound), afterFound)
		return
	}
	if beforeCount != len(afterFound) || skipReason != "" {
		log.Printf("[browser-diag] CP3_CodingGate: before=%d after=%d gateActive=%v skipReason=%q surviving=%v",
			beforeCount, len(afterFound), gateActive, skipReason, afterFound)
	}
}

func BrowserDiagCP4_FinalToolList(tools []map[string]interface{}, iteration int, totalToolCount int) {
	found := browserDiagExtractNames(tools)
	if len(found) == 0 {
		return
	}
	log.Printf("[browser-diag] CP4_FinalToolList: WARNING browser tools PRESENT in LLM tool list: %v | iteration=%d totalTools=%d",
		found, iteration, totalToolCount)
}

func BrowserDiagCP5_StreamFilter(rpfHalted bool, rpfSuppressed int, repHalted bool, repSuppressed int, rawHasBrowser, filteredHasBrowser bool, filteredContent string) {
	if !rawHasBrowser && !filteredHasBrowser && !rpfHalted {
		return
	}

	log.Printf("[browser-diag] CP5_StreamFilter: rawHasBrowserPrefix=%v filteredHasBrowserPrefix=%v rpfHalted=%v rpfSuppressed=%d repHalted=%v repSuppressed=%d",
		rawHasBrowser, filteredHasBrowser, rpfHalted, rpfSuppressed, repHalted, repSuppressed)

	if filteredHasBrowser {
		log.Printf("[browser-diag] CP5_StreamFilter: WARNING Browser: prefix LEAKED through stream filters into filteredBuf")
		idx := browserDiagBrowserPrefixIndex(filteredContent)
		if idx >= 0 {
			log.Printf("[browser-diag] CP5_StreamFilter: filtered Browser: index=%d", idx)
		}
	}
}

func BrowserDiagCP6_PostProcess(beforeContent, afterContent string) {
	beforeHas := browserDiagHasBrowserRolePrefix(beforeContent)
	afterHas := browserDiagHasBrowserRolePrefix(afterContent)

	if !beforeHas && !afterHas {
		return
	}

	if beforeHas && !afterHas {
		log.Printf("[browser-diag] CP6_PostProcess: Browser: prefix stripped by post-processing (before=%d chars, after=%d chars)",
			len(beforeContent), len(afterContent))
	} else if beforeHas && afterHas {
		log.Printf("[browser-diag] CP6_PostProcess: WARNING Browser: prefix SURVIVED post-processing (before=%d chars, after=%d chars)",
			len(beforeContent), len(afterContent))
		idx := browserDiagBrowserPrefixIndex(afterContent)
		if idx >= 0 {
			log.Printf("[browser-diag] CP6_PostProcess: surviving Browser: index=%d", idx)
		}
	}
}

func BrowserDiagCP7_FinalOutput(text string, source string) {
	if !browserDiagHasBrowserRolePrefix(text) {
		return
	}
	log.Printf("[browser-diag] CP7_FinalOutput: WARNING Browser: prefix in FINAL output | source=%s | len=%d",
		source, len(text))
	idx := browserDiagBrowserPrefixIndex(text)
	if idx >= 0 {
		log.Printf("[browser-diag] CP7_FinalOutput: Browser: index=%d", idx)
	}
}

func BrowserDiagFileDelivery(stage, text string, fileNames, localPaths []string, responseSource string) {
	hasBrowser := browserDiagHasBrowserRolePrefix(text)
	rolePrefixKind, rolePrefixIdx, hasRolePrefix := browserDiagRolePrefix(text)
	interesting := hasRolePrefix || len(fileNames) > 0 || len(localPaths) > 0 || responseSource == "file_delivery"
	if !interesting {
		return
	}
	safeFileNames := browserDiagBaseNames(fileNames)
	safeLocalPathNames := browserDiagBaseNames(localPaths)
	log.Printf("[browser-diag] CP8_FileDelivery: stage=%s responseSource=%q textHasBrowserPrefix=%v textHasRolePrefix=%v rolePrefixKind=%q rolePrefixIndex=%d textLen=%d fileNames=%q localPathCount=%d localPathNames=%q",
		stage, responseSource, hasBrowser, hasRolePrefix, rolePrefixKind, rolePrefixIdx, len(text), safeFileNames, len(safeLocalPathNames), safeLocalPathNames)
}

func browserDiagBaseNames(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	names := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		value = strings.ReplaceAll(value, "\\", "/")
		names = append(names, filepath.Base(value))
	}
	return names
}

func truncateDiagMsg(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}
