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
// Detection strategy:
//   - Accumulate recent output in a sliding window of completed sentences.
//   - After each sentence boundary (。！？!?\n), check whether the
//     recent sentence sequence contains a repeating pattern.
//   - A "pattern" is 1-4 consecutive sentences that repeat as a block.
//     For example: [A, B, A, B] is a pattern of length 2 repeating twice.
//   - If a pattern repeats repMaxConsecutive times, suppress further
//     output and set a "halted" flag.
//   - The halted flag can be read by the caller (e.g. the SSE loop)
//     to early-terminate the stream if desired.
//
// The filter is intentionally conservative: it only triggers on exact
// block repetition (after whitespace normalization), avoiding false
// positives on legitimate repeated phrases like list items or code.

const (
	// repWindowMaxSentences is the maximum number of sentences to keep
	// in the sliding window for pattern detection.
	repWindowMaxSentences = 20

	// repMinSentenceRunes is the minimum sentence length (in runes) to
	// consider for repetition detection. Very short sentences like "好的"
	// are common and legitimate.
	repMinSentenceRunes = 15

	// repMaxConsecutive is the number of times a pattern must repeat
	// before the filter starts suppressing. 2 means: the pattern appears
	// twice (original + one repetition) → halt on the third occurrence.
	repMaxConsecutive = 2

	// repMaxPatternLen is the maximum number of sentences in a repeating
	// pattern. Covers cases like [A,B,C,D, A,B,C,D, ...].
	repMaxPatternLen = 4

	// repWindowMaxRunes caps the pending buffer for the long-text
	// fallback check (no sentence boundaries).
	repWindowMaxRunes = 2000
)

// sentenceBoundary returns true if the rune is a sentence-ending punctuation.
// Newlines are intentionally excluded: code blocks and Markdown lists contain
// many newlines that would cause false-positive repetition detection.
func sentenceBoundary(r rune) bool {
	switch r {
	case '。', '！', '？', '!', '?':
		return true
	}
	return false
}

type repetitionFilter struct {
	downstream llm.TokenCallback

	// pending accumulates tokens until a sentence boundary is found.
	pending strings.Builder

	// recentSentences stores the normalized text of recently completed
	// sentences for pattern detection.
	recentSentences []string

	// halted is set to true when repetition is detected and suppression
	// has begun. Once halted, all further tokens are silently dropped.
	halted bool

	// suppressedRunes counts how many runes were suppressed.
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

	// Process completed sentences from the pending buffer.
	for {
		text := f.pending.String()
		if text == "" {
			break
		}

		// Find the first sentence boundary.
		boundaryIdx := -1
		for i, r := range text {
			if sentenceBoundary(r) {
				boundaryIdx = i + utf8.RuneLen(r)
				break
			}
		}

		if boundaryIdx < 0 {
			// No complete sentence yet. Check if pending is suspiciously
			// long without boundaries (e.g. the LLM is repeating a phrase
			// without punctuation).
			if utf8.RuneCountInString(text) > repWindowMaxRunes/2 {
				f.checkLongPendingRepetition(text)
			}
			break
		}

		sentence := text[:boundaryIdx]
		remainder := text[boundaryIdx:]

		f.pending.Reset()
		f.pending.WriteString(remainder)

		normalized := normalizeSentence(sentence)

		// Always emit the sentence first (we detect repetition after
		// accumulating enough sentences, and halt on the next write).
		f.downstream(sentence)

		// Only track sentences long enough to be meaningful.
		if utf8.RuneCountInString(normalized) < repMinSentenceRunes {
			continue
		}

		f.recentSentences = append(f.recentSentences, normalized)

		// Trim window.
		if len(f.recentSentences) > repWindowMaxSentences {
			excess := len(f.recentSentences) - repWindowMaxSentences
			f.recentSentences = f.recentSentences[excess:]
		}

		// Check for repeating patterns.
		if f.detectRepetition() {
			f.halted = true
			f.suppressedRunes += utf8.RuneCountInString(remainder)
			f.pending.Reset()
			return
		}
	}
}

func (f *repetitionFilter) Flush() {
	if f.halted {
		return
	}
	remaining := f.pending.String()
	if remaining != "" {
		f.downstream(remaining)
		f.pending.Reset()
	}
}

// Halted returns true if the filter detected repetition and stopped output.
func (f *repetitionFilter) Halted() bool {
	return f.halted
}

// SuppressedRunes returns the number of runes that were suppressed.
func (f *repetitionFilter) SuppressedRunes() int {
	return f.suppressedRunes
}

// detectRepetition checks if the recent sentence window contains a
// repeating pattern of length 1..repMaxPatternLen.
//
// For pattern length P, we need at least P * repMaxConsecutive sentences.
// We check if the last P*repMaxConsecutive sentences form repMaxConsecutive
// identical blocks of P sentences each.
func (f *repetitionFilter) detectRepetition() bool {
	n := len(f.recentSentences)
	for patLen := 1; patLen <= repMaxPatternLen; patLen++ {
		needed := patLen * repMaxConsecutive
		if n < needed {
			continue
		}
		// Extract the last `needed` sentences.
		window := f.recentSentences[n-needed:]
		// The pattern is the first `patLen` sentences.
		pattern := window[:patLen]
		// Check if all subsequent blocks match.
		allMatch := true
		for block := 1; block < repMaxConsecutive; block++ {
			offset := block * patLen
			for i := 0; i < patLen; i++ {
				if window[offset+i] != pattern[i] {
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
// long phrase without sentence-ending punctuation. It checks if the
// pending buffer contains the same substring repeated.
func (f *repetitionFilter) checkLongPendingRepetition(text string) {
	runes := []rune(text)
	n := len(runes)
	// Try pattern lengths from n/4 to n/2.
	for patLen := n / 4; patLen <= n/2; patLen++ {
		pattern := string(runes[:patLen])
		normalized := normalizeSentence(pattern)
		if utf8.RuneCountInString(normalized) < repMinSentenceRunes {
			continue
		}
		// Check if the pattern repeats at least twice.
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
			// Emit only the first occurrence, halt the rest.
			f.downstream(pattern)
			f.halted = true
			f.suppressedRunes += n - patLen
			f.pending.Reset()
			return
		}
	}
}

// normalizeSentence strips leading/trailing whitespace and collapses
// internal whitespace for comparison purposes.
func normalizeSentence(s string) string {
	s = strings.TrimSpace(s)
	// Collapse all whitespace sequences to a single space.
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
