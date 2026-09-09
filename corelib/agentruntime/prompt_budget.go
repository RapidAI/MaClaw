package agentruntime

import "strings"

// truncNotice is the single stable truncation marker shape.
const truncNotice = "\n[truncated]"

// TruncateToTokenBudget truncates content to fit within the given token budget,
// cutting at a smart boundary (paragraph break "\n\n", or sentence-ending
// punctuation followed by whitespace/newline). Appends "[truncated]" notice.
// Uses rune-safe operations to avoid splitting multi-byte UTF-8 characters.
// Uses conservative rune budget (1.5 runes/token) to ensure the truncated
// result never exceeds the token budget under CJK-aware estimation.
func TruncateToTokenBudget(content string, tokenBudget int) string {
	// Convert to runes for safe truncation of multi-byte characters.
	runes := []rune(content)
	// Conservative: assume worst case (all CJK, ~1.5 chars/token).
	maxRunes := tokenBudget * 3 / 2
	if maxRunes <= 0 {
		return truncNotice
	}
	if len(runes) <= maxRunes {
		return content
	}

	// Reserve space for the truncation notice.
	truncNoticeRunes := len([]rune(truncNotice))
	cutoff := maxRunes - truncNoticeRunes
	if cutoff <= 0 {
		return truncNotice
	}
	if cutoff > len(runes) {
		return content
	}

	snippet := string(runes[:cutoff])

	// Try to find a smart boundary working backwards from the cutoff point.
	halfLen := len(snippet) / 2

	// Priority 1: paragraph break ("\n\n")
	if idx := strings.LastIndex(snippet, "\n\n"); idx > halfLen {
		return snippet[:idx] + truncNotice
	}

	// Priority 2: sentence-ending punctuation (., 。, !, ?, ！, ？) followed
	// by whitespace or newline, or at end of snippet.
	bestSentEnd := -1
	for i := len(snippet) - 1; i > halfLen; i-- {
		ch := snippet[i]
		if ch == '.' || ch == '!' || ch == '?' {
			// Check that the next char (if any) is whitespace/newline or end of snippet.
			if i+1 >= len(snippet) || snippet[i+1] == ' ' || snippet[i+1] == '\n' || snippet[i+1] == '\r' || snippet[i+1] == '\t' {
				bestSentEnd = i + 1
				break
			}
		}
		// Handle multi-byte sentence-ending punctuation (。！？).
		// These are 3-byte UTF-8 sequences.
		if i >= 2 {
			triple := snippet[i-2 : i+1]
			if triple == "。" || triple == "！" || triple == "？" {
				bestSentEnd = i + 1
				break
			}
		}
	}
	if bestSentEnd > 0 {
		return snippet[:bestSentEnd] + truncNotice
	}

	// Priority 3: newline break
	if idx := strings.LastIndex(snippet, "\n"); idx > halfLen {
		return snippet[:idx] + truncNotice
	}

	// Fallback: hard cut (already rune-safe from the runes[:cutoff] above).
	return snippet + truncNotice
}
