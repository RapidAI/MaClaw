package views

// markdown.go provides lightweight terminal Markdown rendering using lipgloss.
// Handles: headings, code blocks, inline code, bold, italic, bullet/numbered
// lists, horizontal rules, blockquotes, and tables. No external dependencies
// beyond lipgloss (already in the project).

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
)

// Styles for Markdown elements.
var (
	mdH1Style = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("212")). // pink
			MarginBottom(0)
	mdH2Style = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("117")). // blue
			MarginBottom(0)
	mdH3Style = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("156")). // green
			MarginBottom(0)
	mdBoldStyle = lipgloss.NewStyle().
			Bold(true)
	mdItalicStyle = lipgloss.NewStyle().
			Italic(true).
			Foreground(lipgloss.Color("250"))
	mdCodeInlineStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("222")). // yellow
				Background(lipgloss.Color("236"))  // dark gray bg
	mdCodeBlockStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("222")).
				Background(lipgloss.Color("235"))
	mdCodeFenceStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("240"))
	mdBlockquoteStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("245")).
				Italic(true)
	mdHRStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))
	mdBulletStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")) // orange bullet
	mdLinkStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("39")).
			Underline(true)
	mdTableHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("117")) // blue, matches H2
	mdTableCellStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("252")) // light gray
	mdTableSepStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("238")) // subtle dark gray
)

// RenderMarkdown converts a Markdown string into styled terminal lines.
// Each returned string is a single rendered line (may contain ANSI codes).
//
// Streaming-aware: handles incomplete markdown at the end of content
// (e.g., unclosed **bold** or partial table rows) by cleaning orphaned
// delimiters before rendering. This prevents raw markdown markers from
// appearing during streaming when content arrives incrementally.
func RenderMarkdown(text string, maxWidth int) []string {
	if maxWidth < 8 {
		maxWidth = 8
	}

	// Clean orphaned delimiters at the end of streaming content.
	text = cleanOrphanedDelimiters(text)

	rawLines := strings.Split(text, "\n")
	var result []string
	inCodeBlock := false
	codeLang := ""

	for i := 0; i < len(rawLines); i++ {
		line := rawLines[i]

		// Code fence toggle.
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			if !inCodeBlock {
				inCodeBlock = true
				codeLang = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "```"))
				if codeLang != "" {
					// Show language label as a subtle tag.
					result = append(result, mdCodeFenceStyle.Render("  ╭─ "+codeLang))
				}
			} else {
				inCodeBlock = false
				codeLang = ""
			}
			continue
		}

		// Inside code block — render as-is with code style.
		if inCodeBlock {
			// Pad to maxWidth for consistent background.
			// Use displayWidth (not rune count) so CJK chars are measured correctly.
			rendered := line
			dw := displayWidth(rendered)
			if dw < maxWidth-2 {
				rendered = rendered + strings.Repeat(" ", maxWidth-2-dw)
			}
			result = append(result, mdCodeBlockStyle.Render("  "+rendered))
			continue
		}

		// Horizontal rule.
		trimmed := strings.TrimSpace(line)
		if isHorizontalRule(trimmed) {
			hr := strings.Repeat("─", max(1, maxWidth-4))
			result = append(result, mdHRStyle.Render("  "+hr))
			continue
		}

		// Headings.
		if strings.HasPrefix(trimmed, "### ") {
			content := strings.TrimPrefix(trimmed, "### ")
			content = renderInlineMarkdown(content)
			result = append(result, mdH3Style.Render("  "+content))
			continue
		}
		if strings.HasPrefix(trimmed, "## ") {
			content := strings.TrimPrefix(trimmed, "## ")
			content = renderInlineMarkdown(content)
			result = append(result, mdH2Style.Render("  "+content))
			continue
		}
		if strings.HasPrefix(trimmed, "# ") {
			content := strings.TrimPrefix(trimmed, "# ")
			content = renderInlineMarkdown(content)
			result = append(result, mdH1Style.Render("  "+content))
			continue
		}

		// Blockquote.
		if strings.HasPrefix(trimmed, "> ") {
			content := strings.TrimPrefix(trimmed, "> ")
			content = renderInlineMarkdown(content)
			result = append(result, mdBlockquoteStyle.Render("  │ "+content))
			continue
		}

		// Bullet list.
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			indent := countLeadingSpaces(line)
			content := trimmed[2:] // skip "- " or "* "
			content = renderInlineMarkdown(content)
			pad := strings.Repeat(" ", indent)
			result = append(result, "  "+pad+mdBulletStyle.Render("•")+" "+content)
			continue
		}

		// Numbered list.
		if idx := numberedListPrefix(trimmed); idx != "" {
			content := trimmed[len(idx):]
			content = renderInlineMarkdown(content)
			result = append(result, "  "+lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render(idx)+content)
			continue
		}

		// Table: collect consecutive lines that look like table rows.
		// Require at least 2 consecutive table lines to avoid false positives
		// on paragraphs that happen to contain a single pipe character.
		if isTableLine(trimmed) && i+1 < len(rawLines) && isTableLine(strings.TrimSpace(rawLines[i+1])) {
			var tableLines []string
			for i < len(rawLines) && isTableLine(strings.TrimSpace(rawLines[i])) {
				tableLines = append(tableLines, strings.TrimSpace(rawLines[i]))
				i++
			}
			i-- // back up one since the for loop will i++
			result = append(result, renderTable(tableLines, maxWidth)...)
			continue
		}

		// Orphaned table line (streaming): a single line that looks like a
		// table row but has no following table line. Strip pipe delimiters
		// and render as plain text so the user doesn't see raw | characters.
		// Skip separator lines (|---|---|) — they're meaningless without a table.
		if isTableLine(trimmed) {
			if isTableSeparator(trimmed) {
				// Separator without a table — skip entirely.
				continue
			}
			cells := parseTableCells(trimmed)
			cleaned := strings.Join(cells, "  ")
			cleaned = renderInlineMarkdown(cleaned)
			result = append(result, "  "+cleaned)
			continue
		}

		// Empty line.
		if trimmed == "" {
			result = append(result, "")
			continue
		}

		// Normal paragraph — apply inline formatting and wrap to width.
		rendered := renderInlineMarkdown(line)
		wrapped := wrapToWidth(rendered, max(1, maxWidth-2))
		for _, wl := range wrapped {
			result = append(result, "  "+wl)
		}
	}

	return result
}

// renderInlineMarkdown handles bold, italic, inline code, and links within a line.
func renderInlineMarkdown(line string) string {
	// Process inline code first (to avoid bold/italic inside code).
	line = processInlineCode(line)
	// Links: [text](url) → styled text.
	line = processLinks(line)
	// Bold: **text** or __text__
	line = processInlinePattern(line, "**", "**", mdBoldStyle)
	line = processInlinePattern(line, "__", "__", mdBoldStyle)
	// Italic: *text* (single). Skip if preceded/followed by * to avoid
	// matching inside already-processed ** sequences or ANSI codes.
	line = processItalic(line)
	return line
}

// cleanOrphanedDelimiters strips incomplete markdown delimiters at the end
// of content. During streaming, content arrives incrementally — a bold marker
// ** may arrive before its closing ** in the next chunk. Without cleanup,
// RenderMarkdown would display raw ** markers to the user.
//
// This function scans the LAST LINE of the text (where streaming truncation
// happens) and removes orphaned delimiters. It does NOT modify content inside
// code blocks or lines that are code fences themselves.
func cleanOrphanedDelimiters(text string) string {
	// Find the last line (streaming truncation point).
	lastNL := strings.LastIndex(text, "\n")
	var prefix, lastLine string
	if lastNL >= 0 {
		prefix = text[:lastNL+1]
		lastLine = text[lastNL+1:]
	} else {
		prefix = ""
		lastLine = text
	}

	if lastLine == "" {
		return text
	}

	trimmedLast := strings.TrimSpace(lastLine)

	// Don't touch code fence lines themselves (``` or ```python).
	if strings.HasPrefix(trimmedLast, "```") {
		return text
	}

	// Don't touch content inside code blocks.
	// Count code fence lines (lines starting with ```) in prefix to determine
	// if the last line is inside a code block. This is more robust than
	// counting ``` substrings, which would miscount `````, inline ```, etc.
	fenceCount := 0
	for _, pline := range strings.Split(prefix, "\n") {
		if strings.HasPrefix(strings.TrimSpace(pline), "```") {
			fenceCount++
		}
	}
	if fenceCount%2 != 0 {
		// Inside a code block — don't strip anything.
		return text
	}

	// Strip orphaned bold markers: odd number of ** means unclosed bold.
	// Use non-overlapping count that handles *** correctly.
	lastLine = cleanOrphanedBoldMarker(lastLine)

	// Strip orphaned underline bold markers.
	lastLine = cleanOrphanedPairMarker(lastLine, "__")

	// Strip orphaned inline code backtick (single `, not triple ```).
	// Only count isolated backticks, not triple-backtick fences.
	singleBackticks := countSingleBackticks(lastLine)
	if singleBackticks%2 != 0 {
		// Remove the last single backtick (the unclosed one).
		idx := lastIndexSingleBacktick(lastLine)
		if idx >= 0 {
			lastLine = lastLine[:idx] + lastLine[idx+1:]
		}
	}

	// Strip orphaned link syntax: [text]( without closing )
	// Pattern: [...](... at end without )
	if idx := strings.LastIndex(lastLine, "]("); idx >= 0 {
		afterParen := lastLine[idx+2:]
		if !strings.Contains(afterParen, ")") {
			// Unclosed link — show just the link text.
			bracketStart := strings.LastIndex(lastLine[:idx], "[")
			if bracketStart >= 0 {
				linkText := lastLine[bracketStart+1 : idx]
				lastLine = lastLine[:bracketStart] + linkText
			}
		}
	} else if idx := strings.LastIndex(lastLine, "["); idx >= 0 {
		// Opening [ without ]( — might be mid-link.
		afterBracket := lastLine[idx+1:]
		if !strings.Contains(afterBracket, "]") {
			// Unclosed bracket — remove the [
			lastLine = lastLine[:idx] + lastLine[idx+1:]
		}
	}

	return prefix + lastLine
}

// cleanOrphanedBoldMarker handles ** specifically, avoiding corruption of
// *** (bold+italic) sequences. Scans for ** boundaries that are not part
// of *** and removes the last unpaired one.
func cleanOrphanedBoldMarker(line string) string {
	// Count non-overlapping ** occurrences, skipping *** sequences.
	// Walk through the string tracking * runs.
	count := 0
	lastIdx := -1
	i := 0
	runes := []rune(line)
	for i < len(runes) {
		if runes[i] == '*' {
			// Count consecutive *
			start := i
			for i < len(runes) && runes[i] == '*' {
				i++
			}
			runLen := i - start
			// A run of exactly 2 is one ** marker.
			// A run of 4 is two ** markers. Etc.
			pairs := runLen / 2
			count += pairs
			if pairs > 0 {
				// Last ** in this run starts at start + (runLen - runLen%2 - 2)
				// if runLen is even, or start + (runLen - 1 - 2) if odd.
				// Simplified: the last ** starts at start + 2*(pairs-1) + (runLen%2)
				lastIdx = start + runLen - 2 // byte position of last ** in run
			}
			continue
		}
		i++
	}
	if count%2 != 0 && lastIdx >= 0 {
		// Remove the last ** (2 runes at lastIdx).
		result := string(runes[:lastIdx]) + string(runes[lastIdx+2:])
		return result
	}
	return line
}

// cleanOrphanedPairMarker removes the last occurrence of a pair marker
// (like __) if the count is odd (meaning one is unclosed).
func cleanOrphanedPairMarker(line, marker string) string {
	count := strings.Count(line, marker)
	if count%2 != 0 {
		// Odd count — remove the last occurrence (the unclosed one).
		idx := strings.LastIndex(line, marker)
		line = line[:idx] + line[idx+len(marker):]
	}
	return line
}

// countSingleBackticks counts backticks that are NOT part of triple-backtick
// (```) sequences.
func countSingleBackticks(s string) int {
	count := 0
	runes := []rune(s)
	i := 0
	for i < len(runes) {
		if runes[i] == '`' {
			start := i
			for i < len(runes) && runes[i] == '`' {
				i++
			}
			runLen := i - start
			if runLen < 3 {
				count += runLen
			}
			// Runs of 3+ are code fences, not inline code — skip them.
			continue
		}
		i++
	}
	return count
}

// lastIndexSingleBacktick returns the index of the last backtick that is
// NOT part of a triple-backtick sequence, or -1 if none.
func lastIndexSingleBacktick(s string) int {
	runes := []rune(s)
	lastIdx := -1
	i := 0
	for i < len(runes) {
		if runes[i] == '`' {
			start := i
			for i < len(runes) && runes[i] == '`' {
				i++
			}
			runLen := i - start
			if runLen < 3 {
				// Last backtick in this short run.
				lastIdx = i - 1
			}
			continue
		}
		i++
	}
	return lastIdx
}

// wrapToWidth wraps a rendered line (possibly containing ANSI codes) to fit
// within the target display width. Uses display-width-aware measurement that
// correctly handles CJK characters (width 2) and skips ANSI escape sequences.
//
// Returns a slice of wrapped lines. If the input fits within maxWidth, returns
// a single-element slice.
func wrapToWidth(s string, maxWidth int) []string {
	if maxWidth <= 0 {
		return []string{s}
	}
	if displayWidthVisible(s) <= maxWidth {
		return []string{s}
	}

	runes := []rune(s)
	var lines []string
	lineStart := 0
	w := 0
	hasStyle := false // true if any non-reset ANSI sequence has been seen

	i := 0
	for i < len(runes) {
		// Skip ANSI escape sequences (don't count as width).
		if runes[i] == '\x1b' && i+1 < len(runes) && runes[i+1] == '[' {
			seqStart := i
			i += 2
			for i < len(runes) && (runes[i] < 0x40 || runes[i] > 0x7E) {
				i++
			}
			if i < len(runes) {
				i++ // skip final byte
			}
			// Track whether we're inside a styled region.
			seq := string(runes[seqStart:i])
			if seq == "\x1b[0m" {
				hasStyle = false
			} else {
				hasStyle = true
			}
			continue
		}

		rw := 1
		if runes[i] >= 0x1100 && isCJKOrFullwidth(runes[i]) {
			rw = 2
		} else if runes[i] == 0xFE0F || runes[i] == 0xFE0E || runes[i] == 0x200D {
			rw = 0
		}

		if w+rw > maxWidth {
			// Emit current line with style reset if needed.
			lineContent := string(runes[lineStart:i])
			if hasStyle {
				lineContent += "\x1b[0m"
			}
			lines = append(lines, lineContent)
			lineStart = i
			w = 0
			// Note: hasStyle carries over — the next line segment will
			// inherit whatever ANSI state was active. The terminal resets
			// at the line boundary due to the \x1b[0m we appended, and
			// lipgloss re-applies styles per-render in the caller.
		}

		w += rw
		i++
	}

	// Emit remaining content.
	if lineStart < len(runes) {
		lines = append(lines, string(runes[lineStart:]))
	}

	if len(lines) == 0 {
		return []string{s}
	}
	return lines
}

// processLinks replaces [text](url) with styled link text.
func processLinks(line string) string {
	for {
		start := strings.Index(line, "[")
		if start < 0 {
			break
		}
		rest := line[start+1:]
		closeBracket := strings.Index(rest, "](")
		if closeBracket < 0 {
			break
		}
		text := rest[:closeBracket]
		afterParen := rest[closeBracket+2:]
		closeParen := strings.Index(afterParen, ")")
		if closeParen < 0 {
			break
		}
		// Render link text with underline style; URL is hidden in terminal.
		styled := mdLinkStyle.Render(text)
		line = line[:start] + styled + afterParen[closeParen+1:]
	}
	return line
}

// processItalic handles *text* italic without corrupting ANSI escape codes
// injected by prior inline passes (bold, code).
func processItalic(line string) string {
	runes := []rune(line)
	var result strings.Builder
	i := 0
	for i < len(runes) {
		// Skip ANSI escape sequences entirely.
		if runes[i] == '\x1b' && i+1 < len(runes) && runes[i+1] == '[' {
			result.WriteRune(runes[i])
			i++
			for i < len(runes) {
				result.WriteRune(runes[i])
				if runes[i] >= 0x40 && runes[i] <= 0x7E {
					i++
					break
				}
				i++
			}
			continue
		}
		// Match *text* where * is not adjacent to another *.
		if runes[i] == '*' {
			// Don't match if preceded by * (would be **).
			if i > 0 && runes[i-1] == '*' {
				result.WriteRune(runes[i])
				i++
				continue
			}
			// Don't match if followed by * (would be **).
			if i+1 < len(runes) && runes[i+1] == '*' {
				result.WriteRune(runes[i])
				i++
				continue
			}
			// Find closing *.
			j := i + 1
			for j < len(runes) && runes[j] != '*' {
				// Skip ANSI inside italic span.
				if runes[j] == '\x1b' && j+1 < len(runes) && runes[j+1] == '[' {
					j += 2
					for j < len(runes) && (runes[j] < 0x40 || runes[j] > 0x7E) {
						j++
					}
					if j < len(runes) {
						j++
					}
					continue
				}
				j++
			}
			if j < len(runes) && j > i+1 {
				inner := string(runes[i+1 : j])
				result.WriteString(mdItalicStyle.Render(inner))
				i = j + 1
				continue
			}
		}
		result.WriteRune(runes[i])
		i++
	}
	return result.String()
}

// processInlineCode replaces `code` with styled code.
func processInlineCode(line string) string {
	var result strings.Builder
	runes := []rune(line)
	i := 0
	for i < len(runes) {
		if runes[i] == '`' {
			// Find closing backtick.
			j := i + 1
			for j < len(runes) && runes[j] != '`' {
				j++
			}
			if j < len(runes) {
				code := string(runes[i+1 : j])
				result.WriteString(mdCodeInlineStyle.Render(code))
				i = j + 1
				continue
			}
		}
		result.WriteRune(runes[i])
		i++
	}
	return result.String()
}

// processInlinePattern replaces open...close delimited text with styled text.
func processInlinePattern(line, open, close string, style lipgloss.Style) string {
	for {
		start := strings.Index(line, open)
		if start < 0 {
			break
		}
		rest := line[start+len(open):]
		end := strings.Index(rest, close)
		if end < 0 {
			break
		}
		inner := rest[:end]
		if inner == "" {
			break
		}
		styled := style.Render(inner)
		line = line[:start] + styled + rest[end+len(close):]
	}
	return line
}

// isHorizontalRule checks if a line is a Markdown horizontal rule.
func isHorizontalRule(s string) bool {
	s = strings.ReplaceAll(s, " ", "")
	if len(s) < 3 {
		return false
	}
	allDash := true
	allStar := true
	allUnder := true
	for _, r := range s {
		if r != '-' {
			allDash = false
		}
		if r != '*' {
			allStar = false
		}
		if r != '_' {
			allUnder = false
		}
	}
	return allDash || allStar || allUnder
}

// numberedListPrefix returns the "1. " prefix if the line starts with a number list.
func numberedListPrefix(s string) string {
	for i, r := range s {
		if r >= '0' && r <= '9' {
			continue
		}
		if r == '.' && i > 0 && i < len(s)-1 && s[i+1] == ' ' {
			return s[:i+2]
		}
		break
	}
	return ""
}

// countLeadingSpaces counts leading spaces in a string.
func countLeadingSpaces(s string) int {
	count := 0
	for _, r := range s {
		if r == ' ' {
			count++
		} else if r == '\t' {
			count += 4
		} else {
			break
		}
	}
	return count
}

// --- Table rendering ---

// isTableLine returns true if the line looks like a markdown table row.
// Requires the line to start or end with | to avoid false positives on
// prose that happens to contain a pipe character.
func isTableLine(trimmed string) bool {
	if trimmed == "" {
		return false
	}
	if !strings.Contains(trimmed, "|") {
		return false
	}
	// Standard markdown table: starts or ends with |
	if strings.HasPrefix(trimmed, "|") || strings.HasSuffix(trimmed, "|") {
		return true
	}
	// Separator line: only dashes, pipes, colons, spaces.
	if isTableSeparator(trimmed) {
		return true
	}
	return false
}

// isTableSeparator checks if a line is a table separator (|---|---|).
func isTableSeparator(s string) bool {
	for _, r := range s {
		switch r {
		case '-', '|', ':', ' ', '\t':
			continue
		default:
			return false
		}
	}
	return strings.Contains(s, "-")
}

// parseTableCells splits a table row into trimmed cell strings.
func parseTableCells(line string) []string {
	// Strip leading/trailing pipes.
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "|") {
		line = line[1:]
	}
	if strings.HasSuffix(line, "|") {
		line = line[:len(line)-1]
	}
	parts := strings.Split(line, "|")
	cells := make([]string, len(parts))
	for i, p := range parts {
		cells[i] = strings.TrimSpace(p)
	}
	return cells
}

// displayWidth returns the display width of a string, counting CJK characters
// as width 2 and ASCII as width 1. This is needed for correct column alignment
// when mixing Chinese and ASCII text.
func displayWidth(s string) int {
	w := 0
	for len(s) > 0 {
		r, size := utf8.DecodeRuneInString(s)
		s = s[size:]
		// Variation selectors and zero-width joiners are invisible modifiers.
		if r == 0xFE0F || r == 0xFE0E || r == 0x200D {
			continue
		}
		if r >= 0x1100 && isCJKOrFullwidth(r) {
			w += 2
		} else {
			w++
		}
	}
	return w
}

// isCJKOrFullwidth returns true for characters that occupy 2 terminal cells:
// CJK Unified Ideographs, Hangul, fullwidth forms, and emoji.
func isCJKOrFullwidth(r rune) bool {
	return (r >= 0x1100 && r <= 0x115F) || // Hangul Jamo
		(r >= 0x2E80 && r <= 0x9FFF) || // CJK radicals, ideographs
		(r >= 0xAC00 && r <= 0xD7AF) || // Hangul syllables
		(r >= 0xF900 && r <= 0xFAFF) || // CJK compat ideographs
		(r >= 0xFE30 && r <= 0xFE4F) || // CJK compat forms
		(r >= 0xFF01 && r <= 0xFF60) || // Fullwidth forms
		(r >= 0x20000 && r <= 0x2FA1F) || // CJK ext B-F
		// Emoji and symbols that render as width 2 in terminals:
		(r >= 0x2600 && r <= 0x27BF) || // Misc symbols, Dingbats
		(r >= 0x2B50 && r <= 0x2B55) || // Stars, circles
		(r >= 0x1F000 && r <= 0x1FAFF) || // Mahjong, Dominos, Playing Cards, Emoji
		(r >= 0x1FC00 && r <= 0x1FFFF) // Supplemental symbols
}

// padToWidth pads a string with spaces to reach the target display width.
func padToWidth(s string, target int) string {
	cur := displayWidth(s)
	if cur >= target {
		return s
	}
	return s + strings.Repeat(" ", target-cur)
}

// truncateToWidth truncates a string to fit within the target display width,
// cutting at rune boundaries to avoid corrupting multi-byte UTF-8 characters.
func truncateToWidth(s string, target int) string {
	if target <= 0 {
		return ""
	}
	w := 0
	for i, r := range s {
		if r == 0xFE0F || r == 0xFE0E || r == 0x200D {
			continue
		}
		rw := 1
		if isCJKOrFullwidth(r) {
			rw = 2
		}
		if w+rw > target {
			return s[:i]
		}
		w += rw
	}
	return s
}

// contentDisplayWidth returns the display width of text after stripping
// inline markdown markers (**bold**, __bold__, *italic*, `code`).
// Used for table column width calculation so columns are sized for the
// rendered content, not the raw markdown source.
func contentDisplayWidth(s string) int {
	// Strip bold markers: ** and __
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "__", "")
	// Strip inline code backticks
	s = strings.ReplaceAll(s, "`", "")
	// Note: single * (italic) is not stripped here because it's ambiguous
	// with multiplication/bullet usage. The width difference is only 2 chars
	// which is acceptable for column sizing.
	return displayWidth(s)
}

// displayWidthVisible returns the display width of a string, skipping ANSI
// escape sequences (CSI sequences: ESC [ ... final_byte). This is needed
// after renderInlineMarkdown injects lipgloss ANSI codes.
func displayWidthVisible(s string) int {
	w := 0
	i := 0
	runes := []rune(s)
	for i < len(runes) {
		if runes[i] == '\x1b' && i+1 < len(runes) && runes[i+1] == '[' {
			// Skip CSI sequence: ESC [ ... (until 0x40-0x7E final byte)
			i += 2
			for i < len(runes) && (runes[i] < 0x40 || runes[i] > 0x7E) {
				i++
			}
			if i < len(runes) {
				i++ // skip final byte
			}
			continue
		}
		if runes[i] >= 0x1100 && isCJKOrFullwidth(runes[i]) {
			w += 2
		} else if runes[i] == 0xFE0F || runes[i] == 0xFE0E || runes[i] == 0x200D {
			// Variation selectors and zero-width joiners are invisible.
		} else {
			w++
		}
		i++
	}
	return w
}

// truncateToWidthVisible truncates a string (possibly containing ANSI codes)
// to fit within the target visible display width. ANSI escape sequences are
// preserved (not counted as width) and a reset sequence is appended if the
// string was truncated mid-style.
func truncateToWidthVisible(s string, target int) string {
	if target <= 0 {
		return ""
	}
	w := 0
	runes := []rune(s)
	i := 0
	inANSI := false
	for i < len(runes) {
		if runes[i] == '\x1b' && i+1 < len(runes) && runes[i+1] == '[' {
			// Skip entire CSI sequence — always include in output.
			i += 2
			for i < len(runes) && (runes[i] < 0x40 || runes[i] > 0x7E) {
				i++
			}
			if i < len(runes) {
				i++ // skip final byte
			}
			inANSI = true
			continue
		}
		rw := 1
		if runes[i] >= 0x1100 && isCJKOrFullwidth(runes[i]) {
			rw = 2
		} else if runes[i] == 0xFE0F || runes[i] == 0xFE0E || runes[i] == 0x200D {
			rw = 0
		}
		if w+rw > target {
			result := string(runes[:i])
			if inANSI {
				result += "\x1b[0m" // reset to avoid style leaking
			}
			return result
		}
		w += rw
		i++
	}
	return s
}

// renderTable renders a group of markdown table lines as a clean borderless
// table with aligned columns. Header row is styled differently, separator
// line becomes a subtle thin line.
func renderTable(lines []string, maxWidth int) []string {
	if len(lines) == 0 {
		return nil
	}

	// Parse all rows, skip separator lines but remember if row 0 has a
	// separator after it (meaning row 0 is the header).
	type row struct {
		cells    []string
		isHeader bool
	}
	var rows []row
	for i, line := range lines {
		if isTableSeparator(line) {
			// If separator is right after the first data row, mark it as header.
			if i == 1 && len(rows) == 1 {
				rows[0].isHeader = true
			}
			continue
		}
		rows = append(rows, row{cells: parseTableCells(line)})
	}
	if len(rows) == 0 {
		return nil
	}

	// Determine column count and widths.
	numCols := 0
	for _, r := range rows {
		if len(r.cells) > numCols {
			numCols = len(r.cells)
		}
	}
	if numCols == 0 {
		return nil
	}

	colWidths := make([]int, numCols)
	for _, r := range rows {
		for j := 0; j < len(r.cells) && j < numCols; j++ {
			w := contentDisplayWidth(r.cells[j])
			if w > colWidths[j] {
				colWidths[j] = w
			}
		}
	}

	// Cap column widths so total fits in maxWidth.
	// Reserve: 2 (left indent) + 2*numCols (padding between columns).
	totalW := 0
	for _, w := range colWidths {
		totalW += w
	}
	gap := 2 // spaces between columns
	overhead := 2 + gap*(numCols-1)
	if totalW+overhead > maxWidth && maxWidth > overhead+numCols {
		// Proportionally shrink columns.
		avail := maxWidth - overhead
		for j := range colWidths {
			colWidths[j] = colWidths[j] * avail / totalW
			if colWidths[j] < 2 {
				colWidths[j] = 2
			}
		}
	}

	// Render rows.
	var result []string
	spacer := strings.Repeat(" ", gap)
	for ri, r := range rows {
		var b strings.Builder
		b.WriteString("  ") // left indent
		for j := 0; j < numCols; j++ {
			if j > 0 {
				b.WriteString(spacer)
			}
			cell := ""
			if j < len(r.cells) {
				cell = r.cells[j]
			}
			// Render inline markdown (strips ** markers, adds ANSI bold/italic).
			cell = renderInlineMarkdown(cell)
			// Truncate if cell exceeds column width (can happen after
			// proportional shrinking). Uses visible width to skip ANSI codes.
			if displayWidthVisible(cell) > colWidths[j] {
				cell = truncateToWidthVisible(cell, colWidths[j])
			}
			// Style cell content only, not padding spaces.
			// Colored foreground on padding spaces appears as solid
			// colored blocks on some terminals (especially Windows Terminal).
			cellStyle := mdTableCellStyle
			if r.isHeader {
				cellStyle = mdTableHeaderStyle
			}
			cellW := displayWidthVisible(cell)
			styled := cellStyle.Render(cell)
			if cellW < colWidths[j] {
				styled += strings.Repeat(" ", colWidths[j]-cellW)
			}
			b.WriteString(styled)
		}
		result = append(result, b.String())

		// After header row, add a subtle separator line.
		if r.isHeader && ri == 0 {
			var sep strings.Builder
			sep.WriteString("  ")
			for j := 0; j < numCols; j++ {
				if j > 0 {
					sep.WriteString(spacer)
				}
				sep.WriteString(mdTableSepStyle.Render(strings.Repeat("─", colWidths[j])))
			}
			result = append(result, sep.String())
		}
	}
	return result
}
