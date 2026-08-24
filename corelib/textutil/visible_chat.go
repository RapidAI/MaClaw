package textutil

import "strings"

// SanitizeVisibleChatText removes stream markers and non-displayable runes that
// IM / digital-employee UIs render as empty squares (tofu).
//
// The shared agent loop prefixes private reasoning deltas with U+0001. When
// those deltas are concatenated into a visible reply, every token starts with
// a square. Chinese reasoning models also emit Private Use Area special tokens
// (commonly U+E000–U+F8FF) that have no glyph in client fonts.
func SanitizeVisibleChatText(s string) string {
	if s == "" || !hasDroppedVisibleChatRune(s) {
		return s
	}
	return strings.Map(dropNonDisplayChatRune, s)
}

// VisibleChatStreamDelta prepares one agent-loop token for a digital-employee
// UI that has no reasoning panel. Reasoning-lane deltas (leading U+0001) are
// dropped entirely; remaining text is tofu-sanitized.
func VisibleChatStreamDelta(s string) string {
	if s == "" || strings.HasPrefix(s, "\x01") {
		return ""
	}
	return SanitizeVisibleChatText(s)
}

// FirstVisibleChatText returns the first candidate that still has visible text
// after tofu sanitizing. Use it for assembled replies (SSE done vs aggregated
// chunks, JSON fallbacks) so a sentinel-only payload cannot hide a real answer.
func FirstVisibleChatText(candidates ...string) string {
	for _, s := range candidates {
		if s == "" {
			continue
		}
		cleaned := SanitizeVisibleChatText(s)
		if strings.TrimSpace(cleaned) != "" {
			return cleaned
		}
	}
	return ""
}

func hasDroppedVisibleChatRune(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool {
		return dropNonDisplayChatRune(r) < 0
	})
}

func dropNonDisplayChatRune(r rune) rune {
	switch r {
	case '\n', '\r', '\t':
		return r
	case '\x01':
		// Reasoning-lane sentinel. Chromium and most IM clients draw it as □.
		return -1
	}
	if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
		return -1
	}
	if isPrivateUseRune(r) || isSpecialsRune(r) || isSurrogateRune(r) {
		return -1
	}
	return r
}

func isPrivateUseRune(r rune) bool {
	switch {
	case r >= 0xE000 && r <= 0xF8FF:
		return true
	case r >= 0xF0000 && r <= 0xFFFFD:
		return true
	case r >= 0x100000 && r <= 0x10FFFD:
		return true
	default:
		return false
	}
}

func isSpecialsRune(r rune) bool {
	return r >= 0xFFF0 && r <= 0xFFFF
}

func isSurrogateRune(r rune) bool {
	return r >= 0xD800 && r <= 0xDFFF
}
