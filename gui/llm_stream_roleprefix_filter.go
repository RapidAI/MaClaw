package main

import (
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"regexp"
	"strings"
	"unicode/utf8"
)

// rolePrefixStreamFilter detects hallucinated role-prefix lines (e.g.
// "Browser: ..." or "Tool: ...") during LLM streaming and handles them
// inline. This complements the post-hoc stripRolePrefixHallucination
// (which cleans the final response text) by preventing the hallucination
// from ever reaching the frontend via streaming tokens.
//
// The filter accumulates tokens in a line buffer. When a newline is
// encountered, the completed line is checked against the role prefix
// pattern. Two cases:
//
//  1. Prefix at the start of output (seenContent=false): strip the
//     "Browser: " prefix token and emit only the content after it.
//     This handles both single-iteration Case 1 and multi-iteration
//     scenarios where a new LLM request starts with a role prefix.
//  2. Prefix after content has been emitted (seenContent=true): halt
//     and suppress this line and all subsequent tokens. The content
//     after a mid-text role prefix is almost always a duplicate.
//
// Code blocks (``` fenced) are tracked to avoid false positives on
// lines like "Browser: connected" inside code samples.
type rolePrefixStreamFilter struct {
	downstream llm.TokenCallback

	// lineBuf accumulates the current line being built from tokens.
	lineBuf strings.Builder

	// halted is set when a role prefix is detected.
	halted bool

	// suppressedRunes counts runes suppressed after halt.
	suppressedRunes int

	// inCodeBlock tracks whether we're inside a ``` fenced block.
	inCodeBlock bool

	// seenContent tracks whether any non-whitespace content has been
	// emitted before the current line. Used to distinguish Case 1
	// (prefix at start → strip prefix) from Case 2 (mid-text → halt).
	seenContent bool
}

// rolePrefixLineRe matches a role prefix at the start of a line.
// Same pattern as rolePrefixRe in im_conversation_trim.go but without
// the (?m) flag since we check individual lines. Also matches fullwidth
// colon (：U+FF1A) which Chinese LLMs sometimes produce.
var rolePrefixLineRe = regexp.MustCompile(`^[ \t]*(Browser|Tool)(?::[ \t]?|：)`)

func newRolePrefixStreamFilter(downstream llm.TokenCallback) *rolePrefixStreamFilter {
	return &rolePrefixStreamFilter{downstream: downstream}
}

func (f *rolePrefixStreamFilter) Write(delta string) {
	if f.halted {
		f.suppressedRunes += utf8.RuneCountInString(delta)
		return
	}

	// Process the delta character by character, splitting on newlines.
	for len(delta) > 0 {
		nlIdx := strings.IndexByte(delta, '\n')
		if nlIdx < 0 {
			// No newline in remaining delta — accumulate in line buffer.
			f.lineBuf.WriteString(delta)
			break
		}

		// Complete the current line (including the newline).
		f.lineBuf.WriteString(delta[:nlIdx+1])
		line := f.lineBuf.String()
		f.lineBuf.Reset()
		delta = delta[nlIdx+1:]

		// Track code blocks.
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			f.inCodeBlock = !f.inCodeBlock
		}

		// Check for role prefix outside code blocks.
		if !f.inCodeBlock && rolePrefixLineRe.MatchString(line) {
			if f.seenContent {
				// Case 2: mid-text hallucination — halt and suppress
				// this line and everything after it.
				f.halted = true
				f.suppressedRunes += utf8.RuneCountInString(line) + utf8.RuneCountInString(delta)
				return
			}
			// Case 1: role prefix at the very start of output — strip
			// the prefix token but keep the content after it. This
			// handles the scenario where a new agent loop iteration
			// starts with "Browser: ..." — the prefix is removed
			// inline so it never reaches the frontend.
			loc := rolePrefixLineRe.FindStringIndex(line)
			if loc != nil {
				stripped := line[loc[1]:]
				if strings.TrimSpace(stripped) != "" {
					f.downstream(stripped)
					f.seenContent = true
				}
			}
			continue
		}

		// Emit the line.
		f.downstream(line)
		if strings.TrimSpace(line) != "" {
			f.seenContent = true
		}
	}
}

func (f *rolePrefixStreamFilter) Flush() {
	if f.halted {
		return
	}
	remaining := f.lineBuf.String()
	if remaining != "" {
		// Check the final incomplete line for role prefix.
		if !f.inCodeBlock && rolePrefixLineRe.MatchString(remaining) {
			if f.seenContent {
				// Case 2: mid-text — halt and suppress.
				f.halted = true
				f.suppressedRunes += utf8.RuneCountInString(remaining)
				f.lineBuf.Reset()
				return
			}
			// Case 1: prefix at start — strip prefix, keep content.
			loc := rolePrefixLineRe.FindStringIndex(remaining)
			if loc != nil {
				stripped := remaining[loc[1]:]
				if strings.TrimSpace(stripped) != "" {
					f.downstream(stripped)
				}
			}
			f.lineBuf.Reset()
			return
		}
		f.downstream(remaining)
		f.lineBuf.Reset()
	}
}

// Halted returns true if a role prefix hallucination was detected.
func (f *rolePrefixStreamFilter) Halted() bool {
	return f.halted
}

// SuppressedRunes returns the count of runes suppressed after detection.
func (f *rolePrefixStreamFilter) SuppressedRunes() int {
	return f.suppressedRunes
}
