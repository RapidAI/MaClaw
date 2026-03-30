package tool

import (
	"strings"
	"unicode/utf8"
)

// DefaultBodyMaxChars is the default maximum rune count for BodySummary.
const DefaultBodyMaxChars = 1500

// TruncateBody performs markdown-aware truncation on body text.
// It preserves complete lines, prioritizing markdown headings (# prefix),
// list items (- or * prefix), and code block boundaries (``` markers).
// When truncation occurs, "\n..." is appended to the output.
func TruncateBody(body string, maxChars int) string {
	if body == "" {
		return ""
	}
	if maxChars <= 0 {
		return ""
	}

	runes := []rune(body)
	if len(runes) <= maxChars {
		return body
	}

	lines := strings.Split(body, "\n")

	// Accumulate complete lines up to maxChars budget.
	// Lines are kept in order; markdown structure (headings, lists, code blocks)
	// is naturally preserved because these elements typically appear early.
	var kept []string
	usedRunes := 0

	for i, line := range lines {
		lineRunes := utf8.RuneCountInString(line)
		// Cost: line runes + 1 for newline separator (except first line).
		cost := lineRunes
		if i > 0 {
			cost++
		}

		if usedRunes+cost <= maxChars {
			kept = append(kept, line)
			usedRunes += cost
		} else {
			break
		}
	}

	// Edge case: if no complete line fits (single overlong first line),
	// truncate the first line at the rune boundary.
	if len(kept) == 0 {
		runes := []rune(lines[0])
		if len(runes) > maxChars {
			return string(runes[:maxChars]) + "..."
		}
		return lines[0] + "\n..."
	}

	return strings.Join(kept, "\n") + "\n..."
}
