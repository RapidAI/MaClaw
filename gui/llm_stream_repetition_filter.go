package main

import (
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"strings"
	"unicode/utf8"
)

// repetitionFilter detects and suppresses repetitive text in LLM streaming
// output. When the LLM degenerates into repeating the same sentence or
// block of sentences, this filter stops forwarding the repeated content
// to the downstream callback (typically the frontend onToken handler).
//
// Detection strategy - two layers:
//
// Layer 1 (sentence-based):
//   - Split pending buffer on sentence boundaries (Chinese and ASCII punctuation).
//   - Track normalized sentences in a sliding window.
//   - Detect repeating patterns of 1-4 sentences.
//
// Layer 2 (paragraph-based):
//   - Split pending buffer on paragraph boundaries (\n\n).
//   - Track normalized paragraphs in a sliding window.
//   - Detect repeating patterns of 1-3 paragraphs.
//   - This catches structured content (Markdown tables, lists) that
//     doesn't use sentence-ending punctuation but repeats as blocks.
//
// Both layers share the same pending buffer. On each Write(), the buffer
// is scanned for the earliest boundary (sentence or paragraph). Content
// up to that boundary is emitted and tracked by the corresponding layer.
//
// If either layer detects repetition reaching repMaxConsecutive times,
// suppress further output and set a "halted" flag.

const (
	repWindowMaxSentences     = 20
	repMinSentenceRunes       = 15
	repMaxConsecutive         = 2
	repMaxPatternLen          = 4
	repWindowMaxRunes         = 2000
	repWindowMaxParagraphs    = 12
	repMinParagraphRunes      = 30
	repMaxParagraphPatternLen = 3
)

func sentenceBoundary(r rune) bool {
	switch r {
	case '\u3002', '\uff01', '\uff1f', '!', '?':
		return true
	}
	return false
}

type repetitionFilter struct {
	downstream llm.TokenCallback

	// pending accumulates tokens until a boundary is found.
	pending strings.Builder

	// Layer 1: sentence-level tracking.
	recentSentences []string

	// Layer 2: paragraph-level tracking.
	recentParagraphs []string

	halted          bool
	suppressedRunes int
}

func newRepetitionFilter(downstream llm.TokenCallback) *repetitionFilter {
	return &repetitionFilter{downstream: downstream}
}

func (f *repetitionFilter) Write(delta string) {
	if f.halted {
		f.suppressedRunes += utf8.RuneCountInString(delta)
		return
	}

	f.pending.WriteString(delta)

	// Drain the pending buffer by splitting on the earliest boundary.
	for {
		text := f.pending.String()
		if text == "" {
			break
		}

		sentIdx := f.findSentenceBoundary(text)
		paraIdx := findParagraphBreak(text)

		if sentIdx < 0 && paraIdx < 0 {
			// No boundary. Fallback for very long content without any boundary.
			if utf8.RuneCountInString(text) > repWindowMaxRunes/2 {
				f.checkLongPendingRepetition(text)
			}
			break
		}

		// Use whichever boundary comes first.
		if sentIdx >= 0 && (paraIdx < 0 || sentIdx <= paraIdx) {
			f.drainSentence(text, sentIdx)
		} else {
			f.drainParagraph(text, paraIdx)
		}

		if f.halted {
			// Suppress whatever remains in pending.
			rem := f.pending.String()
			if rem != "" {
				f.suppressedRunes += utf8.RuneCountInString(rem)
				f.pending.Reset()
			}
			return
		}
	}
}

// findSentenceBoundary returns the byte index just past the first sentence
// boundary rune, or -1 if none found.
func (f *repetitionFilter) findSentenceBoundary(text string) int {
	for i, r := range text {
		if sentenceBoundary(r) {
			return i + utf8.RuneLen(r)
		}
	}
	return -1
}

// drainSentence emits text up to sentEnd, tracks it in Layer 1, and
// updates pending.
func (f *repetitionFilter) drainSentence(text string, sentEnd int) {
	sentence := text[:sentEnd]
	remainder := text[sentEnd:]
	f.pending.Reset()
	f.pending.WriteString(remainder)

	// Emit first, detect after (same as original design).
	f.downstream(sentence)

	normalized := normalizeSentence(sentence)
	if utf8.RuneCountInString(normalized) < repMinSentenceRunes {
		return
	}

	f.recentSentences = append(f.recentSentences, normalized)
	if len(f.recentSentences) > repWindowMaxSentences {
		f.recentSentences = f.recentSentences[len(f.recentSentences)-repWindowMaxSentences:]
	}

	if detectRepetition(f.recentSentences, repMaxPatternLen) {
		f.halted = true
	}
}

// drainParagraph emits text up to (and including) the paragraph break,
// tracks the paragraph content in Layer 2, and updates pending.
func (f *repetitionFilter) drainParagraph(text string, breakStart int) {
	// Find the end of the break: skip all \n, \r, spaces, tabs.
	breakEnd := breakStart
	for breakEnd < len(text) {
		r, sz := utf8.DecodeRuneInString(text[breakEnd:])
		if r == '\n' || r == '\r' || r == ' ' || r == '\t' {
			breakEnd += sz
		} else {
			break
		}
	}

	emitPart := text[:breakEnd]
	remainder := text[breakEnd:]
	f.pending.Reset()
	f.pending.WriteString(remainder)

	// Emit the content including the break whitespace.
	f.downstream(emitPart)

	// Track the paragraph (content before the break, not the break itself).
	paraText := text[:breakStart]
	normalized := normalizeSentence(paraText)
	if utf8.RuneCountInString(normalized) < repMinParagraphRunes {
		return
	}

	f.recentParagraphs = append(f.recentParagraphs, normalized)
	if len(f.recentParagraphs) > repWindowMaxParagraphs {
		f.recentParagraphs = f.recentParagraphs[len(f.recentParagraphs)-repWindowMaxParagraphs:]
	}

	if detectRepetition(f.recentParagraphs, repMaxParagraphPatternLen) {
		f.halted = true
	}
}

func (f *repetitionFilter) Flush() {
	if f.halted {
		return
	}
	remaining := f.pending.String()
	if remaining == "" {
		return
	}
	f.downstream(remaining)
	f.pending.Reset()

	// Treat end-of-stream as a paragraph boundary: track the final chunk.
	normalized := normalizeSentence(remaining)
	if utf8.RuneCountInString(normalized) >= repMinParagraphRunes {
		f.recentParagraphs = append(f.recentParagraphs, normalized)
		if detectRepetition(f.recentParagraphs, repMaxParagraphPatternLen) {
			f.halted = true
		}
	}
}

func (f *repetitionFilter) Halted() bool         { return f.halted }
func (f *repetitionFilter) SuppressedRunes() int { return f.suppressedRunes }

// detectRepetition checks if the tail of `window` contains a repeating
// pattern of length 1..maxPatLen. Shared by both Layer 1 and Layer 2.
// Pure function - does not access filter state.
func detectRepetition(window []string, maxPatLen int) bool {
	n := len(window)
	for patLen := 1; patLen <= maxPatLen; patLen++ {
		needed := patLen * repMaxConsecutive
		if n < needed {
			continue
		}
		tail := window[n-needed:]
		pattern := tail[:patLen]
		allMatch := true
		for block := 1; block < repMaxConsecutive; block++ {
			off := block * patLen
			for i := 0; i < patLen; i++ {
				if tail[off+i] != pattern[i] {
					allMatch = false
					break
				}
			}
			if !allMatch {
				break
			}
		}
		if allMatch {
			return true
		}
	}
	return false
}

// checkLongPendingRepetition handles the case where the LLM repeats a
// long phrase without any boundary (no sentence punctuation, no \n\n).
func (f *repetitionFilter) checkLongPendingRepetition(text string) {
	runes := []rune(text)
	n := len(runes)
	for patLen := n / 4; patLen <= n/2; patLen++ {
		pattern := string(runes[:patLen])
		normalized := normalizeSentence(pattern)
		if utf8.RuneCountInString(normalized) < repMinSentenceRunes {
			continue
		}
		count := 0
		for offset := 0; offset+patLen <= n; offset += patLen {
			chunk := string(runes[offset : offset+patLen])
			if normalizeSentence(chunk) == normalized {
				count++
			} else {
				break
			}
		}
		if count >= repMaxConsecutive {
			f.downstream(pattern)
			f.halted = true
			f.suppressedRunes += n - patLen
			f.pending.Reset()
			return
		}
	}
}

// findParagraphBreak finds the byte index of the first \n\n (or \n
// followed by optional horizontal whitespace and another \n).
// Returns -1 if not found.
func findParagraphBreak(text string) int {
	// Fast path: no newline at all.
	firstNL := strings.IndexByte(text, '\n')
	if firstNL < 0 || firstNL >= len(text)-1 {
		return -1
	}
	for i := firstNL; i < len(text)-1; {
		r, sz := utf8.DecodeRuneInString(text[i:])
		if r == '\n' {
			j := i + sz
			for j < len(text) {
				r2, sz2 := utf8.DecodeRuneInString(text[j:])
				if r2 == ' ' || r2 == '\t' || r2 == '\r' {
					j += sz2
					continue
				}
				if r2 == '\n' {
					return i
				}
				break
			}
		}
		i += sz
	}
	return -1
}

// normalizeSentence strips leading/trailing whitespace and collapses
// internal whitespace for comparison purposes.
func normalizeSentence(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	prevSpace := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\r' || r == '\n' {
			if !prevSpace {
				b.WriteRune(' ')
				prevSpace = true
			}
		} else {
			b.WriteRune(r)
			prevSpace = false
		}
	}
	return b.String()
}
