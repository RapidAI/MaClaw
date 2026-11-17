package textutil

import "strings"

// answerSliceOverlapMin is the shortest shared run that is one clause.
// A single punctuation mark is not the same sentence.
const answerSliceOverlapMin = 16

// ProjectAnswerSlices joins the streamed opening and the settled ending of one
// digital-employee answer. A shared run is kept once. A longer slice that
// already contains the other replaces it. A short fragment is the next piece
// of the same sentence. Anything else stays in order with a gap between them.
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
	openingRunes := []rune(opening)
	endingRunes := []rune(ending)
	if strings.HasPrefix(ending, opening) || strings.HasSuffix(ending, opening) {
		if len(endingRunes) >= len(openingRunes) {
			return ending
		}
	}
	if strings.HasPrefix(opening, ending) || strings.HasSuffix(opening, ending) {
		return opening
	}
	if n := sharedAnswerEdge(openingRunes, endingRunes); n > 0 {
		return opening + string(endingRunes[n:])
	}
	if len(endingRunes) < answerSliceOverlapMin || len(openingRunes) < answerSliceOverlapMin {
		return opening + ending
	}
	return opening + "\n\n…\n\n" + ending
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

// sharedAnswerEdge is the longest prefix of tail that is a suffix of prefix.
func sharedAnswerEdge(prefix, tail []rune) int {
	if len(prefix) == 0 || len(tail) == 0 {
		return 0
	}
	s := make([]rune, 0, len(tail)+1+len(prefix))
	s = append(s, tail...)
	s = append(s, 0)
	s = append(s, prefix...)
	pi := make([]int, len(s))
	k := 0
	for i := 1; i < len(s); i++ {
		for k > 0 && s[i] != s[k] {
			k = pi[k-1]
		}
		if s[i] == s[k] {
			k++
		}
		pi[i] = k
	}
	n := pi[len(s)-1]
	if n >= answerSliceOverlapMin {
		return n
	}
	return 0
}
