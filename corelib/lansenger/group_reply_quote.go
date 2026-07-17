package lansenger

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxGroupReplyQuoteRunes caps how much of the original question is quoted in a
// group reply so the answer remains the visual focus.
const MaxGroupReplyQuoteRunes = 200

// MaxGroupReplySenderRunes caps the "xx问：" label so a malformed display name
// cannot dominate the reply, and so free-form prose containing "问：" is less
// likely to look like our header.
const MaxGroupReplySenderRunes = 32

// maxGroupReplyHeaderRunes is an upper bound on the whole quote block before
// the blank line that separates it from the answer body.
// label + "问：" + question + a few newlines inside a multi-line question.
const maxGroupReplyHeaderRunes = MaxGroupReplySenderRunes + 2 + MaxGroupReplyQuoteRunes + 16

// groupReplyNameAndID returns (displayName, staffId) after normalize.
// displayName is empty when missing or when it is only an echo of staffId.
func groupReplyNameAndID(msg IncomingMessage) (name, id string) {
	name = normalizeGroupReplySenderLabel(msg.SenderName)
	id = normalizeGroupReplySenderLabel(msg.FromUserID)
	// Some payloads echo staffId into senderName.
	if name != "" && id != "" && name == id {
		name = ""
	}
	return name, id
}

// GroupReplyDisplayName returns the platform display name when it is usable as
// a quote-header label (non-empty after normalize, and not just an echo of
// staffId). Empty means callers should fall back to staffId / "有人".
func GroupReplyDisplayName(msg IncomingMessage) string {
	name, _ := groupReplyNameAndID(msg)
	return name
}

// GroupReplySenderLabel picks a human-readable label for group reply headers
// ("xx问：..."). Prefers display name; falls back to staffId.
func GroupReplySenderLabel(msg IncomingMessage) string {
	name, id := groupReplyNameAndID(msg)
	if name != "" {
		return name
	}
	return id
}

// FormatGroupReplyWithQuote prefixes a group-chat reply with a distinguishable
// quote of the original question. Group threads interleave many speakers; without
// this header it is hard to tell which question an answer belongs to.
//
// Layout (plain text, survives Markdown stripping):
//
//	{显示名}问：原始问题内容
//
//	实际回复内容
//
// senderLabel should be the human-readable display name when available
// (see GroupReplySenderLabel); staffId is an acceptable fallback.
// Private replies and empty questions are returned unchanged.
func FormatGroupReplyWithQuote(senderLabel, question, reply string) string {
	reply = strings.TrimSpace(reply)
	if reply == "" {
		return ""
	}
	// Avoid double-prefix if a multi-hop path already quoted this body
	// (e.g. hub + local fallback both trying to annotate the same text).
	if alreadyHasGroupReplyQuote(reply) {
		return reply
	}
	question = normalizeGroupQuoteQuestion(question)
	if question == "" {
		return reply
	}

	who := normalizeGroupReplySenderLabel(senderLabel)
	if who == "" {
		who = "有人"
	}

	// Pre-size in bytes (Grow wants capacity, not rune count).
	var b strings.Builder
	b.Grow(len(who) + len("问：") + len(question) + 2 + len(reply))
	b.WriteString(who)
	b.WriteString("问：")
	// Keep the first line of the question on the same line as "xx问：" for a
	// compact header; subsequent lines follow under it.
	lines := strings.Split(question, "\n")
	b.WriteString(lines[0])
	for _, line := range lines[1:] {
		b.WriteByte('\n')
		b.WriteString(line)
	}
	// Blank line separates the quoted question from the answer body.
	b.WriteString("\n\n")
	b.WriteString(reply)
	return b.String()
}

// FormatGroupReplyWithQuoteFromMessage is FormatGroupReplyWithQuote using
// GroupReplySenderLabel(msg) so callers cannot forget the display-name preference.
func FormatGroupReplyWithQuoteFromMessage(msg IncomingMessage, question, reply string) string {
	return FormatGroupReplyWithQuote(GroupReplySenderLabel(msg), question, reply)
}

// alreadyHasGroupReplyQuote reports whether text already starts with our
// "{label}问：...\n\n..." group-reply header so we do not nest quotes.
func alreadyHasGroupReplyQuote(text string) bool {
	text = strings.TrimSpace(text)
	// Our formatter always inserts a blank line before the answer body.
	sep := strings.Index(text, "\n\n")
	if sep <= 0 {
		return false
	}
	// Header block must be short enough to be our quote (not free-form prose
	// that happens to contain "问：" and a later blank line).
	if utf8.RuneCountInString(text[:sep]) > maxGroupReplyHeaderRunes {
		return false
	}
	first, _, _ := strings.Cut(text[:sep], "\n")
	first = strings.TrimSpace(first)
	idx := strings.Index(first, "问：")
	if idx <= 0 {
		return false
	}
	label := strings.TrimSpace(first[:idx])
	if label == "" {
		return false
	}
	// Cap matches GroupReplySenderLabel so long prose is not treated as a header.
	if utf8.RuneCountInString(label) > MaxGroupReplySenderRunes {
		return false
	}
	// Exact "请" only — rejects free-form "请问：…\n\n…", but keeps rare names
	// that end with 请 (e.g. "赵请").
	if label == "请" {
		return false
	}
	return true
}

// MaybeFormatGroupReplyWithQuote applies FormatGroupReplyWithQuote only for
// group conversations.
func MaybeFormatGroupReplyWithQuote(isGroup bool, senderLabel, question, reply string) string {
	if !isGroup {
		return strings.TrimSpace(reply)
	}
	return FormatGroupReplyWithQuote(senderLabel, question, reply)
}

// MaybeFormatGroupReplyWithQuoteFromMessage applies the quote header using the
// message's preferred display label (name > staffId).
func MaybeFormatGroupReplyWithQuoteFromMessage(isGroup bool, msg IncomingMessage, question, reply string) string {
	if !isGroup {
		return strings.TrimSpace(reply)
	}
	return FormatGroupReplyWithQuoteFromMessage(msg, question, reply)
}

func normalizeGroupReplySenderLabel(senderLabel string) string {
	who := strings.TrimSpace(senderLabel)
	if i := strings.IndexAny(who, "\r\n"); i >= 0 {
		who = strings.TrimSpace(who[:i])
	}
	// Drop control / zero-width noise that can sneak in from platform payloads.
	if strings.IndexFunc(who, isGroupReplyLabelNoise) >= 0 {
		var b strings.Builder
		b.Grow(len(who))
		for _, r := range who {
			if isGroupReplyLabelNoise(r) {
				continue
			}
			b.WriteRune(r)
		}
		who = b.String()
	}
	// Collapse internal whitespace so multi-token names stay compact.
	who = strings.Join(strings.Fields(who), " ")
	// "问：" inside a label would make "{label}问：" ambiguous / break
	// alreadyHasGroupReplyQuote. Strip the marker sequence if it appears.
	if strings.Contains(who, "问：") {
		who = strings.ReplaceAll(who, "问：", "")
		who = strings.Join(strings.Fields(who), " ")
	}
	return truncateRunes(who, MaxGroupReplySenderRunes)
}

func isGroupReplyLabelNoise(r rune) bool {
	return r < 0x20 || r == 0x7f || unicode.Is(unicode.Cf, r)
}

func normalizeGroupQuoteQuestion(question string) string {
	question = strings.TrimSpace(question)
	if question == "" {
		return ""
	}
	// Collapse excessive blank lines so the quote block stays compact.
	lines := strings.Split(question, "\n")
	out := make([]string, 0, len(lines))
	prevBlank := false
	for _, line := range lines {
		trimmed := strings.TrimRight(line, " \t\r")
		blank := strings.TrimSpace(trimmed) == ""
		if blank {
			if prevBlank || len(out) == 0 {
				continue
			}
			prevBlank = true
			out = append(out, "")
			continue
		}
		prevBlank = false
		out = append(out, trimmed)
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	question = strings.Join(out, "\n")
	return truncateRunes(question, MaxGroupReplyQuoteRunes)
}

func truncateRunes(s string, max int) string {
	if max <= 0 || s == "" {
		return s
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	if max == 1 {
		return "…"
	}
	return string(runes[:max-1]) + "…"
}
