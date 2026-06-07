package memory

import "fmt"

// ---------------------------------------------------------------------------
// Tool description constants for Memory Tool.
// All hosts (GUI/TUI/maclawsrv) use these to emit consistent guidance.
// Requirements: 10.1, 10.2, 10.3, 10.4
// ---------------------------------------------------------------------------

// MemoryToolDescriptionBase is the canonical description for the memory tool,
// including pagination, exhaustive mode, and scroll-through capabilities.
const MemoryToolDescriptionBase = "Manage long-term memory with actions save, recall, candidates, derived, derived_surgery, themes, scenes, trace, list, and delete. " +
	"Saves are governed; recall defaults to auto and also supports dynamic, hybrid, lightmem, and adaptive modes.\n\n" +
	"Pagination: If initial recall returns has_more=true, use the cursor to retrieve additional pages. " +
	"Exhaustive: If the user asks to 'list all' or 'summarize everything about X', use mode=exhaustive. " +
	"Scroll session: If working through a multi-step task and need progressive context, use session=true."

// ---------------------------------------------------------------------------
// Parameter descriptions for new recall parameters.
// ---------------------------------------------------------------------------

// ParamDescCursor describes the cursor parameter for paginated recall.
const ParamDescCursor = "Opaque pagination cursor from a previous recall response. " +
	"Pass this to retrieve the next page of results. " +
	"Mutually exclusive with mode=exhaustive and session=true."

// ParamDescMode describes the mode parameter including the exhaustive option.
const ParamDescMode = "Recall mode: auto (default), dynamic, hybrid, lightmem, adaptive, exhaustive. " +
	"Use mode=exhaustive to retrieve all matching entries (up to 100 entries / 15000 tokens) " +
	"when the user asks to 'list all' or 'summarize everything about X'."

// ParamDescSession describes the session parameter for scroll-through recall.
const ParamDescSession = "Set to true to enable scroll-through recall within the current agent loop. " +
	"Each subsequent recall with session=true returns the next slice of results from a cached candidate list. " +
	"Use this for iterative deepening during multi-step tasks. " +
	"Mutually exclusive with cursor."

// ---------------------------------------------------------------------------
// Response hint templates for paginated and exhaustive results.
// These are used by format functions to provide actionable guidance to the LLM.
// ---------------------------------------------------------------------------

// HintHasMorePrefix is the prefix for the has_more=true hint.
const HintHasMorePrefix = "Hint: Use cursor='"

// HintHasMoreSuffix is the suffix for the has_more=true hint.
const HintHasMoreSuffix = "' to see more results."

// HintTruncatedTemplate is the format template for the truncated=true hint.
// Use with fmt.Sprintf(HintTruncatedTemplate, totalMatching).
const HintTruncatedTemplate = "Hint: Total matching: %d. Use mode=exhaustive with category filter for focused results."

// HintSessionExhausted is the hint when all scroll session candidates are returned.
const HintSessionExhausted = "All cached candidates have been returned. Start a new recall for fresh results."

// ---------------------------------------------------------------------------
// Helper functions for building response hints.
// ---------------------------------------------------------------------------

// BuildHasMoreHint returns the actionable hint when has_more=true.
func BuildHasMoreHint(cursor string) string {
	return HintHasMorePrefix + cursor + HintHasMoreSuffix
}

// BuildTruncatedHint returns the actionable hint when truncated=true.
func BuildTruncatedHint(totalMatching int) string {
	return fmt.Sprintf(HintTruncatedTemplate, totalMatching)
}
