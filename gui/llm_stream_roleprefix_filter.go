package main

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// rolePrefixStreamFilter detects hallucinated role-prefix lines (e.g.
// "Browser: ..." or "Tool: ...") during LLM streaming and suppresses
// all subsequent output once detected. This complements the post-hoc
// stripRolePrefixHallucination (which cleans the final response text)
// by preventing the hallucination from ever reaching the frontend via
// streaming tokens.
//
// The filter accumulates tokens in a line buffer. When a newline is
// encountered, the completed line is checked against the role prefix
// pattern. If a match is found:
//   - The matched line and all subsequent tokens are suppressed.
//   - The halted flag is set so the caller can early-terminate the stream.
//
// Code blocks (``` fenced) are tracked to avoid false positives on
// lines like "Browser: connected" inside code samples.
type rolePrefixStreamFilter struct {
	downstream TokenCallback

	// lineBuf accumulates the current line being built from tokens.
	lineBuf strings.Builder

	// halted is set when a role prefix is detected.
	halted bool

	// suppressedRunes counts runes suppressed after halt.
	suppressedRunes int

	// inCodeBlock tracks whether we're inside a ``` fenced block.
	inCodeBlock bool

	// seenContent tracks whether any non-whitespace content has been
	// emitted before the current line. Role prefix at the very start
	// of the output is handled differently (Case 1 in
	// stripRolePrefixHallucination): the prefix is stripped but the
	// content after it is kept. In streaming, we can't easily do this
	// token-by-token, so we only halt on mid-text prefixes (Case 2).
	// Case 1 (prefix at start) is handled by the post-hoc
	// stripRolePrefixHallucination on the final response.
	seenContent bool
}

// rolePrefixLineRe matches a role prefix at the start of a line.
// Same pattern as rolePrefixRe in im_conversation_trim.go but without
// the (?m) flag since we check individual lines. Also matches fullwidth
// colon (：U+FF1A) which Chinese LLMs sometimes produce.
var rolePrefixLineRe = regexp.MustCompile(`^[ \t]*(Browser|Tool)(?::[ \t]?|：)`)

func newRolePrefixStreamFilter(downstream TokenCallback) *rolePrefixStreamFilter {
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

		// Check for role prefix (only outside code blocks and after
		// some content has been emitted — mid-text hallucination).
		if !f.inCodeBlock && f.seenContent && rolePrefixLineRe.MatchString(line) {
			// Halt: suppress this line and everything after it.
			f.halted = true
			f.suppressedRunes += utf8.RuneCountInString(line) + utf8.RuneCountInString(delta)
			return
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
		if !f.inCodeBlock && f.seenContent && rolePrefixLineRe.MatchString(remaining) {
			f.halted = true
			f.suppressedRunes += utf8.RuneCountInString(remaining)
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
