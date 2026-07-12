package textutil

import (
	"strings"
	"unicode/utf8"
)

// isPictographBase reports decorative pictograph / dingbat bases used as UI chrome.
// Ranges only — no emoji literals in source.
// Note: regional indicators (U+1F1E6–1F1FF) sit inside U+1F300–1FAFF.
// U+2B50 (star) is outside 2600–27BF and is listed explicitly for rating marks.
func isPictographBase(r rune) bool {
	switch {
	case r >= 0x1F300 && r <= 0x1FAFF:
		return true
	case r >= 0x2600 && r <= 0x27BF:
		return true
	case r >= 0x2300 && r <= 0x23FF:
		return true
	case r == 0x2B50:
		return true
	default:
		return false
	}
}

// isPreservedInlineMark reports semantic marks kept for product UI (status / star).
// Aligns with gui remoteStreamMarks + star; decorative "AI flavor" marks are stripped.
func isPreservedInlineMark(r rune) bool {
	switch r {
	case 0x23FA, // ⏺ record / tool
		0x23F3, // ⏳ hourglass / pending
		0x26A1, // ⚡ bolt
		0x2713, // ✓ check
		0x2705, // ✅ check
		0x2717, // ✗ cross
		0x26A0, // ⚠ warn
		0x274C, // ❌ cross
		0x2B50: // ⭐ star
		return true
	default:
		return false
	}
}

// StripLeadingEmojiCluster removes leading decorative pictograph clusters
// (including ZWJ sequences) and the spaces that follow each cluster unit.
// Semantic status/star marks are also stripped when line-leading via the older
// API used for tool progress prefixes; chat-body display uses stripLineDecorativePictographs.
func StripLeadingEmojiCluster(s string) string {
	i := 0
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			break
		}
		if !isPictographBase(r) {
			break
		}
		i += size
		// optional VS16
		if i < len(s) {
			r2, size2 := utf8.DecodeRuneInString(s[i:])
			if r2 == 0xFE0F {
				i += size2
			}
		}
		// ZWJ-joined units
		for i < len(s) {
			r2, size2 := utf8.DecodeRuneInString(s[i:])
			if r2 != 0x200D {
				break
			}
			if i+size2 >= len(s) {
				break
			}
			r3, size3 := utf8.DecodeRuneInString(s[i+size2:])
			if !isPictographBase(r3) {
				break
			}
			i += size2 + size3
			if i < len(s) {
				r4, size4 := utf8.DecodeRuneInString(s[i:])
				if r4 == 0xFE0F {
					i += size4
				}
			}
		}
		// trailing spaces/tabs after this cluster unit
		for i < len(s) {
			r2, size2 := utf8.DecodeRuneInString(s[i:])
			if r2 == ' ' || r2 == '\t' {
				i += size2
				continue
			}
			break
		}
	}
	return s[i:]
}

// takeCluster advances past one pictograph cluster starting at s[i:], returning
// the base rune (first pictograph base), whether it was a multi-base ZWJ sequence,
// and the byte end index of the cluster (not including trailing spaces).
func takeCluster(s string, i int) (base rune, multiBase bool, end int, ok bool) {
	if i >= len(s) {
		return 0, false, i, false
	}
	r, size := utf8.DecodeRuneInString(s[i:])
	if r == utf8.RuneError && size == 1 {
		return 0, false, i, false
	}
	if !isPictographBase(r) {
		return 0, false, i, false
	}
	base = r
	end = i + size
	if end < len(s) {
		r2, size2 := utf8.DecodeRuneInString(s[end:])
		if r2 == 0xFE0F {
			end += size2
		}
	}
	bases := 1
	for end < len(s) {
		r2, size2 := utf8.DecodeRuneInString(s[end:])
		if r2 != 0x200D {
			break
		}
		if end+size2 >= len(s) {
			break
		}
		r3, size3 := utf8.DecodeRuneInString(s[end+size2:])
		if !isPictographBase(r3) {
			break
		}
		bases++
		end += size2 + size3
		if end < len(s) {
			r4, size4 := utf8.DecodeRuneInString(s[end:])
			if r4 == 0xFE0F {
				end += size4
			}
		}
	}
	return base, bases > 1, end, true
}

// stripLineDecorativePictographs removes chatbot-style decorative pictographs
// anywhere on the line (after optional markdown structural prefix), while
// keeping semantic status/star marks for product UI substitution.
func stripLineDecorativePictographs(line string) string {
	if line == "" || !hasPictographBase(line) {
		return line
	}
	wsEnd := 0
	for wsEnd < len(line) && (line[wsEnd] == ' ' || line[wsEnd] == '\t') {
		wsEnd++
	}
	rest := line[wsEnd:]
	mdPrefix, restAfterMD := takeMarkdownLinePrefix(rest)

	var b strings.Builder
	b.Grow(len(restAfterMD))
	changed := false
	i := 0
	for i < len(restAfterMD) {
		base, multi, end, ok := takeCluster(restAfterMD, i)
		if !ok {
			// Copy one rune.
			r, size := utf8.DecodeRuneInString(restAfterMD[i:])
			if r == utf8.RuneError && size == 1 {
				b.WriteByte(restAfterMD[i])
				i++
				continue
			}
			b.WriteRune(r)
			i += size
			continue
		}
		// Keep single semantic status/star marks; drop decorative / ZWJ clusters.
		if !multi && isPreservedInlineMark(base) {
			b.WriteString(restAfterMD[i:end])
			i = end
			continue
		}
		changed = true
		i = end
		// Skip spaces/tabs that trailed the decorative cluster (collapse AI flavor padding).
		for i < len(restAfterMD) {
			r2, size2 := utf8.DecodeRuneInString(restAfterMD[i:])
			if r2 == ' ' || r2 == '\t' {
				i += size2
				continue
			}
			break
		}
	}
	if !changed {
		return line
	}
	// Collapse residual double spaces introduced by mid-line removals; keep structure.
	cleaned := collapseHorizontalSpaceRuns(b.String())
	cleaned = strings.TrimRight(cleaned, " \t")
	return line[:wsEnd] + mdPrefix + cleaned
}

func collapseHorizontalSpaceRuns(s string) string {
	if !strings.Contains(s, "  ") && !strings.Contains(s, "\t\t") && !strings.Contains(s, " \t") && !strings.Contains(s, "\t ") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		if r == ' ' || r == '\t' {
			if prevSpace {
				continue
			}
			b.WriteByte(' ')
			prevSpace = true
			continue
		}
		prevSpace = false
		b.WriteRune(r)
	}
	return b.String()
}

// stripLineLeadingEmoji is kept as an alias for callers/tests that used the
// older line-leading-only name; policy now strips decorative marks mid-line too.
func stripLineLeadingEmoji(line string) string {
	return stripLineDecorativePictographs(line)
}

// takeMarkdownLinePrefix returns a heading/list/blockquote prefix if present.
func takeMarkdownLinePrefix(s string) (prefix string, rest string) {
	if s == "" {
		return "", s
	}
	// Headings: #{1,6} + whitespace
	if s[0] == '#' {
		n := 0
		for n < len(s) && n < 6 && s[n] == '#' {
			n++
		}
		if n > 0 && n < len(s) && (s[n] == ' ' || s[n] == '\t') {
			m := n
			for m < len(s) && (s[m] == ' ' || s[m] == '\t') {
				m++
			}
			return s[:m], s[m:]
		}
	}
	// Unordered list: - * + + whitespace
	if (s[0] == '-' || s[0] == '*' || s[0] == '+') && len(s) > 1 && (s[1] == ' ' || s[1] == '\t') {
		m := 1
		for m < len(s) && (s[m] == ' ' || s[m] == '\t') {
			m++
		}
		return s[:m], s[m:]
	}
	// Ordered list: digits + . + whitespace
	if s[0] >= '0' && s[0] <= '9' {
		n := 0
		for n < len(s) && s[n] >= '0' && s[n] <= '9' {
			n++
		}
		if n > 0 && n < len(s) && s[n] == '.' {
			m := n + 1
			if m < len(s) && (s[m] == ' ' || s[m] == '\t') {
				for m < len(s) && (s[m] == ' ' || s[m] == '\t') {
					m++
				}
				return s[:m], s[m:]
			}
		}
	}
	// Blockquote: > + whitespace
	if s[0] == '>' && len(s) > 1 && (s[1] == ' ' || s[1] == '\t') {
		m := 1
		for m < len(s) && (s[m] == ' ' || s[m] == '\t') {
			m++
		}
		return s[:m], s[m:]
	}
	return "", s
}

func isFenceLine(line string) bool {
	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	if i >= len(line) {
		return false
	}
	if line[i] == '`' {
		n := 0
		for i+n < len(line) && line[i+n] == '`' {
			n++
		}
		return n >= 3
	}
	if line[i] == '~' {
		n := 0
		for i+n < len(line) && line[i+n] == '~' {
			n++
		}
		return n >= 3
	}
	return false
}

// hasPictographBase reports whether s contains any decorative pictograph base.
// Used as a fast path so clean messages skip line splitting entirely.
func hasPictographBase(s string) bool {
	for _, r := range s {
		if isPictographBase(r) {
			return true
		}
	}
	return false
}

// PrepareChatBodyForDisplay strips decorative pictographs (line-leading and
// mid-sentence "AI flavor") outside fenced code blocks. Semantic status marks
// (check / warn / cross / …) and stars are kept so product UIs can render SVG
// glyphs. Display-only — does not mutate stored message content.
func PrepareChatBodyForDisplay(text string) string {
	if text == "" || !hasPictographBase(text) {
		return text
	}
	lines := strings.Split(text, "\n")
	inFence := false
	changed := false
	for i, line := range lines {
		if isFenceLine(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		cleaned := stripLineDecorativePictographs(line)
		if cleaned != line {
			lines[i] = cleaned
			changed = true
		}
	}
	if !changed {
		return text
	}
	return strings.Join(lines, "\n")
}
