package tool

import (
	"fmt"
	"strings"
)

// SplitTextLines splits text into lines and keeps each line's own newline.
// strings.SplitAfter leaves a final empty segment after a trailing newline.
// Counting that segment makes a file whose real length is the page size look
// truncated, and the next cursor then names a line the compiler does not have.
// An empty string stays one empty segment so a 1-based cursor can still name it.
func SplitTextLines(content string) []string {
	lines := strings.SplitAfter(content, "\n")
	if len(lines) > 1 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// AppendNumberedTextLines appends lines with 1-based line-number prefixes.
// The padded pipe form is Maclaw's long-standing read_file style; colonStyle
// matches the OpenCode read contract (`1: content`) used by bridge-adapted
// reads. Each input line keeps its own trailing newline when present.
func AppendNumberedTextLines(b *strings.Builder, lines []string, startLine int, colonStyle bool) {
	format := "%4d | %s"
	if colonStyle {
		format = "%d: %s"
	}
	for i, line := range lines {
		if colonStyle {
			line = strings.ReplaceAll(line, "\r\n", "\n")
		}
		b.WriteString(fmt.Sprintf(format, startLine+i, line))
	}
}

const (
	// NumberedReadMaxBytes is the OpenCode read page byte cap.
	NumberedReadMaxBytes = 50 * 1024
	// NumberedReadMaxLineChars is the OpenCode per-line character cap.
	NumberedReadMaxLineChars = 2000
)

// FormatNumberedReadPage renders one text page in the OpenCode read style:
// a "Read file x, lines A-B" header, "<line>: <content>" rows, and an
// offset-based continuation footer. Lines keep neither their trailing newline
// nor any CR, rows are clamped to 2000 characters, and the page content is
// capped at 50KiB.
func FormatNumberedReadPage(resource string, allLines []string, startIdx, maxLines int) string {
	if len(allLines) == 1 && allLines[0] == "" {
		return "Read file " + resource + ", 0 lines"
	}
	if startIdx < 0 {
		startIdx = 0
	}
	if maxLines <= 0 {
		maxLines = 2000
	}
	// Upstream read contract: an offset past the end returns the file's tail
	// page with a note in the header instead of an error.
	pastEnd := 0
	if startIdx >= len(allLines) {
		pastEnd = startIdx + 1
		startIdx = len(allLines) - maxLines
		if startIdx < 0 {
			startIdx = 0
		}
	}
	remaining := allLines[startIdx:]
	page := remaining
	if len(page) > maxLines {
		page = page[:maxLines]
	}
	// Apply the byte cap on the LF view content. Always keep at least the
	// first line: clamped rows are at most ~2KiB, far below the page cap.
	size := 0
	cut := len(page)
	for i, line := range page {
		size += len(normalizeNewlines(line))
		if size > NumberedReadMaxBytes && i > 0 {
			cut = i
			break
		}
	}
	page = page[:cut]
	startLine := startIdx + 1
	endLine := startLine + len(page) - 1
	next := 0
	if startIdx+len(page) < len(allLines) {
		next = startLine + len(page)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Read file %s, lines %d-%d", resource, startLine, endLine)
	if pastEnd > 0 {
		total := len(allLines)
		noun := "lines"
		if total == 1 {
			noun = "line"
		}
		fmt.Fprintf(&b, " (offset %d is past the end; the file has %d %s)", pastEnd, total, noun)
	}
	for i, raw := range page {
		content := strings.TrimRight(raw, "\r\n")
		runes := []rune(content)
		if len(runes) > NumberedReadMaxLineChars {
			content = string(runes[:NumberedReadMaxLineChars]) + "... (line truncated to 2000 chars)"
		}
		fmt.Fprintf(&b, "\n%d: %s", startLine+i, content)
	}
	if next > 0 {
		fmt.Fprintf(&b, "\n[Output truncated. Continue reading with offset: %d]", next)
	}
	return b.String()
}

// FormatNumberedReadDir renders one directory listing page in the OpenCode
// read style. entries must already be sorted and carry trailing separators
// for directories. startEntry is the 1-based first entry index so an offset
// continuation pages deeper instead of looping on the first page.
func FormatNumberedReadDir(resource string, entries []string, startEntry, maxEntries int) string {
	if len(entries) == 0 {
		return "Read directory " + resource + ", 0 entries"
	}
	if maxEntries <= 0 {
		maxEntries = 2000
	}
	startIdx := startEntry - 1
	if startIdx < 0 {
		startIdx = 0
	}
	if startIdx > len(entries) {
		startIdx = len(entries)
	}
	page := entries[startIdx:]
	next := 0
	if len(page) > maxEntries {
		page = page[:maxEntries]
		next = startIdx + len(page) + 1
	}
	if len(page) == 0 {
		return fmt.Sprintf("Read directory %s, entries 0-0 (offset %d is past the end; the directory has %d entries)", resource, startEntry, len(entries))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Read directory %s, entries %d-%d", resource, startIdx+1, startIdx+len(page))
	for _, entry := range page {
		b.WriteString("\n")
		b.WriteString(entry)
	}
	if next > 0 {
		fmt.Fprintf(&b, "\n[Output truncated. Continue reading with offset: %d]", next)
	}
	return b.String()
}
