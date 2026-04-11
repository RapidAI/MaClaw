package remote

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z~^$]|\x1b\].*?(?:\x1b\\|\x07)|\x1b[()#][A-Z0-9]?|\x1b[a-zA-Z]`)
var controlPattern = regexp.MustCompile(`[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]`)
var multiSpacePattern = regexp.MustCompile(`\s{2,}`)

// boxDrawingOnly matches lines composed entirely of box-drawing, block-element,
// and common ASCII separator characters.
var boxDrawingOnly = regexp.MustCompile(`^[\s\x{2500}-\x{259F}\x{2550}-\x{256C}\-=_*+|]+$`)

// NormalizeChunkLines splits a raw PTY chunk into cleaned, non-empty lines
// with ANSI stripping, noise filtering, and length truncation applied.
func NormalizeChunkLines(chunk []byte) []string {
	text := string(chunk)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	rawLines := strings.Split(text, "\n")
	out := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		line = strings.TrimSpace(StripANSI(line))
		if line == "" || IsNoiseLine(line) {
			continue
		}
		if len(line) > 300 {
			line = line[:300] + "..."
		}
		out = append(out, line)
	}
	return out
}

// StripANSI removes ANSI escape sequences and control characters from a string.
func StripANSI(s string) string {
	s = ansiPattern.ReplaceAllString(s, "")
	s = controlPattern.ReplaceAllString(s, "")
	return multiSpacePattern.ReplaceAllString(s, " ")
}

// IsNoiseLine returns true if the line is visual noise (empty, dots, box-drawing).
func IsNoiseLine(s string) bool {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" || trimmed == ".." || trimmed == "..." {
		return true
	}
	return boxDrawingOnly.MatchString(trimmed)
}

// RawChunkResult holds the parsed lines from a PTY chunk along with a
// flag indicating whether the chunk contained a screen-clear sequence.
type RawChunkResult struct {
	Lines           []string
	IsScreenRefresh bool
}

// screenClearPattern matches common ANSI sequences that clear the screen.
var screenClearPattern = regexp.MustCompile(`\x1b\[2J|\x1b\[H|\x1b\[1;1H|\x1b\[\?1049[hl]`)

// RawChunkLines splits a PTY output chunk into lines with only ANSI
// stripping applied. No noise filtering, no length truncation.
func RawChunkLines(chunk []byte) RawChunkResult {
	raw := string(chunk)
	// Sanitize invalid UTF-8 sequences that ConPTY on Windows may produce
	// (e.g. emoji bytes truncated by GBK code page).
	if !utf8.ValidString(raw) {
		raw = strings.ToValidUTF8(raw, "")
	}
	isRefresh := screenClearPattern.MatchString(raw)

	text := strings.ReplaceAll(raw, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	rawLines := strings.Split(text, "\n")
	out := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		cleaned := strings.TrimRight(StripANSI(line), " \t")
		out = append(out, cleaned)
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return RawChunkResult{Lines: out, IsScreenRefresh: isRefresh}
}
