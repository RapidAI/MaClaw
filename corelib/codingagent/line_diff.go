package codingagent

import (
	"strings"

	"github.com/pmezard/go-difflib/difflib"
)

// CountTextLineChanges returns the exact line-oriented diff size between two
// text snapshots. A replacement counts its old lines as removed and its new
// lines as added, matching unified-diff and git numstat semantics.
func CountTextLineChanges(before, after string) (added, removed int) {
	if before == after {
		return 0, 0
	}
	matcher := difflib.NewMatcher(splitTextLines(before), splitTextLines(after))
	for _, op := range matcher.GetOpCodes() {
		switch op.Tag {
		case 'r':
			removed += op.I2 - op.I1
			added += op.J2 - op.J1
		case 'd':
			removed += op.I2 - op.I1
		case 'i':
			added += op.J2 - op.J1
		}
	}
	return added, removed
}

// splitTextLines splits on LF while preserving line endings. Unlike
// difflib.SplitLines it does not invent a sentinel blank line for empty or
// newline-terminated text; that sentinel made an empty-to-one-line write look
// like +1/-1 instead of git's +1/-0.
func splitTextLines(text string) []string {
	if text == "" {
		return nil
	}
	lines := strings.SplitAfter(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
