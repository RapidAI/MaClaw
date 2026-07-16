package lansenger

import (
	"strings"
	"unicode/utf8"
)

// MaxGroupReplyQuoteRunes caps how much of the original question is quoted in a
// group reply so the answer remains the visual focus.
const MaxGroupReplyQuoteRunes = 200

// FormatGroupReplyWithQuote prefixes a group-chat reply with a distinguishable
// quote of the original question. Group threads interleave many speakers; without
// this header it is hard to tell which question an answer belongs to.
//
// Layout (plain text, survives Markdown stripping):
//
//	{staffId}问：原始问题内容
//
//	实际回复内容
//
// senderID is the Lansenger staffId from the inbound "from" field (IncomingMessage.FromUserID).
// Private replies and empty questions are returned unchanged.
func FormatGroupReplyWithQuote(senderID, question, reply string) string {
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

	who := strings.TrimSpace(senderID)
	if who == "" {
		who = "有人"
	}

	var b strings.Builder
	b.WriteString(who)
	b.WriteString("问：")
	// Keep the first line of the question on the same line as "xx问：" for a
	// compact header; subsequent lines follow indented under it.
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

// alreadyHasGroupReplyQuote reports whether text already starts with our
// "{id}问：..." group-reply header so we do not nest quotes.
func alreadyHasGroupReplyQuote(text string) bool {
	// Match: <non-empty id>问：
	// Keep it conservative: require the first line to look like our header.
	first, _, _ := strings.Cut(text, "\n")
	first = strings.TrimSpace(first)
	idx := strings.Index(first, "问：")
	if idx <= 0 {
		return false
	}
	// ID portion must be non-empty and free of spaces (staffIds / "有人").
	id := first[:idx]
	return id != "" && !strings.ContainsAny(id, " \t")
}

// MaybeFormatGroupReplyWithQuote applies FormatGroupReplyWithQuote only for
// group conversations.
func MaybeFormatGroupReplyWithQuote(isGroup bool, senderID, question, reply string) string {
	if !isGroup {
		return strings.TrimSpace(reply)
	}
	return FormatGroupReplyWithQuote(senderID, question, reply)
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
