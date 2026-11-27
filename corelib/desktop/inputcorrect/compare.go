package inputcorrect

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Text comparison for a field the tool read back. It is exact after two
// folds only:
//
//   - Unicode NFC, so a composed character and its decomposed form match
//   - newline unification: CR LF and CR become LF
//
// Spaces, case, and markdown are not folded. A leading prefix is not a
// match. The model is not asked whether the text matches.
const (
	Match         = "match"
	Mismatch      = "mismatch"
	Truncated     = "truncated"
	Duplicate     = "duplicate"
	Newline       = "newline"
	EmptyExpected = "empty"
)

// FoldExact applies the two folds above. It does not trim.
func FoldExact(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return norm.NFC.String(s)
}

// CompareExact reports whether observed is the expected field text.
func CompareExact(expected, observed string) (bool, string) {
	if strings.TrimSpace(expected) == "" || strings.ContainsRune(expected, '\x00') {
		return false, EmptyExpected
	}
	e, o := FoldExact(expected), FoldExact(observed)
	if e == o {
		return true, Match
	}
	if stripNewlines(e) == stripNewlines(o) {
		return false, Newline
	}
	if e != "" && strings.HasPrefix(e, o) && o != "" {
		return false, Truncated
	}
	if e != "" && (o == e+e || strings.Contains(o, e+"\n"+e) || strings.Count(o, e) >= 2) {
		return false, Duplicate
	}
	return false, Mismatch
}

func stripNewlines(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' {
			return -1
		}
		return r
	}, s)
}

// HasEdgeSpace reports leading or trailing Unicode space. A reader that
// strips edges cannot prove an exact match for such text.
func HasEdgeSpace(s string) bool {
	if s == "" {
		return false
	}
	r := []rune(s)
	return unicode.IsSpace(r[0]) || unicode.IsSpace(r[len(r)-1])
}
