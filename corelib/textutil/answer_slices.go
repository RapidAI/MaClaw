package textutil

import (
	"strings"
	"unicode/utf16"
)

// answerSliceOverlapMin is the shortest shared run that is one clause, counted
// in UTF-16 code units — the same unit the desktop viewer uses for string
// length. A single punctuation mark is not the same sentence.
const answerSliceOverlapMin = 16

// ProjectAnswerSlices joins the streamed opening and the settled ending of one
// digital-employee answer.
//
// The agent loop trims the settled answer, while the token stream keeps the
// newline that trim removed. Matching ignores that edge so one sentence is not
// shown twice. A shared run of at least one clause is kept once. A longer
// slice that already contains the other replaces it. A short ending is the
// next piece of the same sentence (a saved stream fragment). A short opening
// is not glued onto a different long answer. Anything else stays in order
// with a gap between them.
func ProjectAnswerSlices(opening, ending string) string {
	if opening == "" {
		return ending
	}
	if ending == "" {
		return opening
	}
	if strings.HasPrefix(strings.TrimSpace(ending), "[系统提示]") {
		return opening + "\n\n" + ending
	}
	openCmp := trimAnswerEdgeRight(opening)
	endCmp := trimAnswerEdgeLeft(ending)
	if openCmp == "" {
		return ending
	}
	if endCmp == "" {
		return opening
	}
	openUnits := utf16Units(openCmp)
	endUnits := utf16Units(endCmp)
	if (strings.HasPrefix(endCmp, openCmp) || strings.HasSuffix(endCmp, openCmp)) && len(endUnits) >= len(openUnits) {
		return ending
	}
	if strings.HasPrefix(openCmp, endCmp) || strings.HasSuffix(openCmp, endCmp) {
		return opening
	}
	if n := sharedAnswerEdge(openUnits, endUnits); n > 0 {
		return openCmp + dropUTF16Prefix(endCmp, n)
	}
	// The ending is the piece that used to be appended. A short opening is a
	// different sentence from a long settled answer, so it keeps the gap.
	if utf16Len(ending) < answerSliceOverlapMin {
		return opening + ending
	}
	return opening + "\n\n…\n\n" + ending
}

// ClipKeepTail bounds text to maxRunes and retains the end. A folded answer
// carries the settled ending there; keeping the head drops it. The marker
// sits outside the budget, the same way the head clip marks a cut request.
// maxRunes <= 0 leaves the text unchanged.
func ClipKeepTail(value string, maxRunes int) string {
	value = strings.TrimSpace(value)
	if value == "" || maxRunes <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return "..." + string(runes[len(runes)-maxRunes:])
}

// StreamEndContent is the settled slice a viewer still has to fold onto the
// opening it already showed. Empty when that opening is already the answer,
// including a turn that never streamed and sent the answer as one chunk.
func StreamEndContent(streamed, final string) string {
	if streamed == "" {
		return ""
	}
	if ProjectAnswerSlices(streamed, final) == streamed {
		return ""
	}
	return final
}

func trimAnswerEdgeRight(s string) string {
	return strings.TrimRight(s, " \t\r\n")
}

func trimAnswerEdgeLeft(s string) string {
	return strings.TrimLeft(s, " \t\r\n")
}

func utf16Units(s string) []uint16 {
	return utf16.Encode([]rune(s))
}

func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n++
		if r > 0xFFFF {
			n++
		}
	}
	return n
}

// dropUTF16Prefix drops the first n UTF-16 code units. A match produced by
// sharedAnswerEdge ends on a code-point boundary for well-formed text.
func dropUTF16Prefix(s string, units int) string {
	if units <= 0 {
		return s
	}
	n := 0
	for i, r := range s {
		if n >= units {
			return s[i:]
		}
		n++
		if r > 0xFFFF {
			n++
		}
	}
	return ""
}

// sharedAnswerEdge is the longest prefix of tail that is a suffix of prefix.
// The walk stays inside tail, so a NUL in the text cannot form a false edge.
func sharedAnswerEdge(prefix, tail []uint16) int {
	if len(prefix) == 0 || len(tail) == 0 {
		return 0
	}
	pi := make([]int, len(tail))
	k := 0
	for i := 1; i < len(tail); i++ {
		for k > 0 && tail[i] != tail[k] {
			k = pi[k-1]
		}
		if tail[i] == tail[k] {
			k++
		}
		pi[i] = k
	}
	k = 0
	for _, c := range prefix {
		for k > 0 && (k == len(tail) || tail[k] != c) {
			k = pi[k-1]
		}
		if k < len(tail) && tail[k] == c {
			k++
		}
	}
	if k >= answerSliceOverlapMin {
		return k
	}
	return 0
}
