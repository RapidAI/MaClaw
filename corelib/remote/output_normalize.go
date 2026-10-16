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
	s = StripANSIKeepSpacing(s)
	return multiSpacePattern.ReplaceAllString(s, " ")
}

// StripANSIKeepSpacing removes ANSI and other control characters without
// collapsing runs of spaces. Column-aligned command output keeps its
// separators; callers trim trailing PTY padding on their own.
func StripANSIKeepSpacing(s string) string {
	s = ansiPattern.ReplaceAllString(s, "")
	return controlPattern.ReplaceAllString(s, "")
}

// CompactPtyOutput strips terminal controls and trailing padding from one
// captured command. Internal spacing is kept so df/free columns stay readable.
func CompactPtyOutput(s string) string {
	s = StripANSIKeepSpacing(s)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// StripLeadingCommandEcho removes the PTY echo of command from the front of
// output. Wrapped echoes span lines at the terminal width. A line that mixes
// the tail of the echo with real output is left in place.
func StripLeadingCommandEcho(output, command string) string {
	target := strings.Join(strings.Fields(command), "")
	if target == "" || strings.TrimSpace(output) == "" {
		return output
	}
	lines := strings.Split(output, "\n")
	acc := ""
	drop := 0
	for i, line := range lines {
		piece := line
		if i == 0 {
			piece = lineWithoutShellPrompt(line)
		}
		next := acc + strings.Join(strings.Fields(piece), "")
		if next == target {
			drop = i + 1
			acc = next
			break
		}
		if next != "" && strings.HasPrefix(target, next) {
			drop = i + 1
			acc = next
			continue
		}
		break
	}
	if acc != target || drop == 0 || drop >= len(lines) {
		return output
	}
	rest := strings.TrimSpace(strings.Join(lines[drop:], "\n"))
	if rest == "" {
		return output
	}
	return rest
}

func lineWithoutShellPrompt(line string) string {
	for _, prompt := range []string{"# ", "$ ", "% "} {
		idx := strings.LastIndex(line, prompt)
		if idx < 0 {
			continue
		}
		if strings.Contains(line[:idx], "@") {
			return line[idx+len(prompt):]
		}
	}
	return line
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
