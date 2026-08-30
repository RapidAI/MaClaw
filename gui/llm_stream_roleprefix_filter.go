package main

import (
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"log"
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
//  2. Prefix after content has been emitted (seenContent=true): halt
//     and suppress this line and all subsequent tokens.
//
// Additionally, the line buffer is scanned for embedded \n boundaries
// that weren't split by the delta chunking. This handles the case where
// a single delta contains "...content\nBrowser: hallucination" — the \n
// is inside the delta but the outer loop hasn't split on it yet.
//
// Code blocks (``` fenced) are tracked to avoid false positives.
type rolePrefixStreamFilter struct {
	downstream llm.TokenCallback

	lineBuf         strings.Builder
	halted          bool
	suppressedRunes int
	inCodeBlock     bool
	seenContent     bool
}

// rolePrefixLineRe matches a role prefix at the start of a line.
// Allows optional leading whitespace, Markdown block-level markers
// (>, -, *, digits), and an optional space before the colon.
// Also matches fullwidth colon (：U+FF1A).
var rolePrefixLineRe = regexp.MustCompile(`^[\s>*\-]*(?:\d+\.\s*)?(Browser|Tool)\s*(?::[ \t]?|：)`)

// rolePrefixReasoningRe matches the same role-prefix pattern anywhere in a
// multi-line buffer. It is retained for sanitizeTraceStoredText (trace-event
// storage sanitization). The reasoning display path no longer uses it:
// stripRolePrefixReasoningForDisplay only strips a prefix at the very start,
// because reasoning legitimately narrates Browser/Tool usage mid-thought.
var rolePrefixReasoningRe = regexp.MustCompile(`(?m)^[\s>*\-]*(?:\d+\.\s*)?(Browser|Tool)\s*(?::[ \t]?|：)`)

// midLineRolePrefixRe matches a role prefix that appears after a \n
// inside the line buffer. This catches the case where a single streaming
// delta contains "...content\nBrowser: hallucination" and the outer
// newline-splitting loop hasn't processed the inner \n yet.
var midLineRolePrefixRe = regexp.MustCompile(`\n[\s>*\-]*(?:\d+\.\s*)?(Browser|Tool)\s*(?::[ \t]?|：)`)

// rpfMidLineCheckThreshold: only run the mid-line regex scan when the
// line buffer exceeds this many bytes. Avoids per-token regex overhead
// during normal streaming (typical token is 1-10 bytes).
const rpfMidLineCheckThreshold = 40

func newRolePrefixStreamFilter(downstream llm.TokenCallback) *rolePrefixStreamFilter {
	return &rolePrefixStreamFilter{downstream: downstream}
}

func (f *rolePrefixStreamFilter) Write(delta string) {
	if f.halted {
		f.suppressedRunes += utf8.RuneCountInString(delta)
		return
	}

	if strings.Contains(delta, "Browser") || strings.Contains(delta, "Tool") {
		log.Printf("[stream-roleprefix] prefix keyword observed in delta: halted=%v seenContent=%v bytes=%d runes=%d",
			f.halted, f.seenContent, len(delta), utf8.RuneCountInString(delta))
	}

	for len(delta) > 0 {
		nlIdx := strings.IndexByte(delta, '\n')
		if nlIdx < 0 {
			f.lineBuf.WriteString(delta)

			// Mid-line scan: only when we have seen content and the
			// buffer is long enough to plausibly contain a prefix.
			if f.seenContent && f.lineBuf.Len() > rpfMidLineCheckThreshold {
				f.checkMidLinePrefix()
			}
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

		if strings.Contains(line, "Browser") || strings.Contains(line, "Tool") {
			log.Printf("[stream-roleprefix] prefix keyword observed in line: inCodeBlock=%v seenContent=%v regexMatch=%v bytes=%d runes=%d",
				f.inCodeBlock, f.seenContent, rolePrefixLineRe.MatchString(line), len(line), utf8.RuneCountInString(line))
		}

		if !f.inCodeBlock && rolePrefixLineRe.MatchString(line) {
			if f.seenContent {
				f.halted = true
				f.suppressedRunes += utf8.RuneCountInString(line) + utf8.RuneCountInString(delta)
				log.Printf("[stream-roleprefix] Case 2 halt: seenContent=true suppressed=%d", f.suppressedRunes)
				return
			}
			loc := rolePrefixLineRe.FindStringIndex(line)
			if loc != nil {
				stripped := line[loc[1]:]
				log.Printf("[stream-roleprefix] Case 1 strip: prefixBytes=%d strippedRunes=%d", loc[1], utf8.RuneCountInString(stripped))
				if strings.TrimSpace(stripped) != "" {
					f.downstream(stripped)
					f.seenContent = true
				}
			}
			continue
		}

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
	if remaining == "" {
		return
	}

	if strings.Contains(remaining, "Browser") || strings.Contains(remaining, "Tool") {
		log.Printf("[stream-roleprefix] prefix keyword observed at flush: inCodeBlock=%v seenContent=%v regexMatch=%v bytes=%d runes=%d",
			f.inCodeBlock, f.seenContent, rolePrefixLineRe.MatchString(remaining), len(remaining), utf8.RuneCountInString(remaining))
	}

	if !f.inCodeBlock && rolePrefixLineRe.MatchString(remaining) {
		if f.seenContent {
			f.halted = true
			f.suppressedRunes += utf8.RuneCountInString(remaining)
			log.Printf("[stream-roleprefix] Flush Case 2 halt: suppressed=%d", f.suppressedRunes)
			f.lineBuf.Reset()
			return
		}
		loc := rolePrefixLineRe.FindStringIndex(remaining)
		if loc != nil {
			stripped := remaining[loc[1]:]
			log.Printf("[stream-roleprefix] Flush Case 1 strip: prefixBytes=%d strippedRunes=%d", loc[1], utf8.RuneCountInString(stripped))
			if strings.TrimSpace(stripped) != "" {
				f.downstream(stripped)
			}
		}
		f.lineBuf.Reset()
		return
	}

	// Final mid-line check: scan remaining for embedded \n + prefix.
	if !f.inCodeBlock && f.seenContent && len(remaining) > rpfMidLineCheckThreshold {
		if !strings.Contains(remaining, "Browser") && !strings.Contains(remaining, "Tool") {
			// Fast path: no prefix keyword.
		} else if loc := midLineRolePrefixRe.FindStringIndex(remaining); loc != nil {
			before := remaining[:loc[0]]
			if strings.TrimSpace(before) != "" {
				f.downstream(before)
			}
			f.halted = true
			f.suppressedRunes += utf8.RuneCountInString(remaining[loc[0]:])
			log.Printf("[stream-roleprefix] Flush mid-line halt: prefix at offset %d", loc[0])
			f.lineBuf.Reset()
			return
		}
	}

	f.downstream(remaining)
	f.lineBuf.Reset()
}

func (f *rolePrefixStreamFilter) Halted() bool         { return f.halted }
func (f *rolePrefixStreamFilter) SuppressedRunes() int { return f.suppressedRunes }

// checkMidLinePrefix scans the line buffer for a \n followed by a role
// prefix pattern. This handles the case where a streaming delta contains
// an embedded newline that the outer split loop hasn't processed.
// If found, emit content before the \n and halt.
func (f *rolePrefixStreamFilter) checkMidLinePrefix() {
	buf := f.lineBuf.String()
	if !strings.Contains(buf, "Browser") && !strings.Contains(buf, "Tool") {
		return
	}
	loc := midLineRolePrefixRe.FindStringIndex(buf)
	if loc == nil {
		return
	}
	before := buf[:loc[0]]
	if strings.TrimSpace(before) != "" {
		f.downstream(before)
	}
	f.halted = true
	f.suppressedRunes += utf8.RuneCountInString(buf[loc[0]:])
	f.lineBuf.Reset()
	log.Printf("[stream-roleprefix] checkMidLinePrefix halt: prefix found at offset %d in lineBuf", loc[0])
}

// stripRolePrefixReasoningForDisplay removes a hallucinated role prefix only
// when it appears at the very start of the reasoning text. Unlike the content
// path, reasoning is hidden/internal and routinely narrates tool usage
// ("Browser: ...", "Tool: ...") in the middle of a thought, so a mid-text
// prefix must never truncate the remaining reasoning.
func stripRolePrefixReasoningForDisplay(s string) string {
	if s == "" {
		return s
	}
	if !strings.Contains(s, "Browser") && !strings.Contains(s, "Tool") {
		return s
	}
	// rolePrefixLineRe is anchored at the start of the string (no (?m)), so
	// only a leading prefix — optionally preceded by whitespace/Markdown
	// markers — is stripped. Everything after it is kept verbatim.
	loc := rolePrefixLineRe.FindStringIndex(s)
	if loc == nil {
		return s
	}
	return s[loc[1]:]
}
