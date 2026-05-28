package main

import "regexp"

// Compiled regexes for isSubstantivePhaseDocument, package-level for performance.
var (
	substantiveHeadingRe    = regexp.MustCompile(`(?m)^#{1,6}\s+\S`)
	substantiveNumberedRe   = regexp.MustCompile(`(?m)^(?:\d+[\.\x{3001}\)]\s*)\S`)
	substantiveBulletLineRe = regexp.MustCompile(`(?m)^[-*]\s+\S`)
)

// isSubstantivePhaseDocument checks whether the LLM output is a real phase
// document instead of a transient preamble. It is intentionally structural:
// no localized phrase lists or promise/stall keyword routing are used here.
func isSubstantivePhaseDocument(text string) bool {
	if len([]rune(text)) >= 200 {
		return true
	}
	if substantiveHeadingRe.MatchString(text) {
		return true
	}
	if substantiveNumberedRe.MatchString(text) {
		return true
	}
	if len(substantiveBulletLineRe.FindAllStringIndex(text, 3)) >= 3 {
		return true
	}
	return false
}
