package tool

// TokenJuice: unified token compression layer for tool results.
// Inspired by OpenHuman's tokenjuice module — classifies content type
// and applies type-specific compression before size truncation.
//
// Flow: tool returns raw result → CompressToolResult() → size truncation → conversation
//
// This module sits BEFORE the existing truncateToolResultForTool() logic,
// reducing content size through intelligent compression so that more
// unique information survives the subsequent budget truncation.

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

// ContentType classifies tool output for type-specific compression.
type ContentType int

const (
	ContentPlain    ContentType = iota // plain text, markdown
	ContentHTML                        // HTML markup
	ContentJSON                        // JSON data
	ContentTerminal                    // terminal/shell output (ANSI, repeated lines)
)

// PerToolResultCap defines per-tool token budget caps.
// After compression, results exceeding this cap are hard-truncated.
var PerToolResultCap = map[string]int{
	"web_fetch":          4000,
	"web_search":         2500,
	"bash":               4000,
	"read_file":          5000,
	"read_tool_result":   6000,
	"ssh":                3500,
	"list_directory":     1500,
	"screenshot":         600,
	"manage_skill":       2500,
	"get_session_output": 4000,
	"memory":             1500,
	"browser":            3000,
}

// DefaultResultCap is used when a tool has no specific cap defined.
const DefaultResultCap = 4000

// GetResultCap returns the token cap for a given tool.
func GetResultCap(toolName string) int {
	if cap, ok := PerToolResultCap[toolName]; ok {
		return cap
	}
	return DefaultResultCap
}

// CompressToolResult applies type-aware compression to a tool result.
// It classifies the content, applies appropriate compression rules,
// then enforces the per-tool token cap.
// Returns the compressed result string.
func CompressToolResult(toolName, result string) string {
	if result == "" {
		return ""
	}
	// Skip compression for short results (< 200 chars).
	if utf8.RuneCountInString(result) < 200 {
		return result
	}

	contentType := ClassifyContent(result)
	var compressed string
	switch contentType {
	case ContentHTML:
		compressed = compressHTML(result)
	case ContentJSON:
		compressed = compressJSON(result)
	case ContentTerminal:
		compressed = compressTerminal(result)
	default:
		compressed = compressPlain(result)
	}

	return compressed
}

// ClassifyContent determines the content type of a tool result.
func ClassifyContent(s string) ContentType {
	trimmed := strings.TrimSpace(s)
	if len(trimmed) == 0 {
		return ContentPlain
	}

	// Check for HTML
	lower := strings.ToLower(trimmed[:min(500, len(trimmed))])
	if strings.Contains(lower, "<html") || strings.Contains(lower, "<!doctype") ||
		strings.Contains(lower, "<head>") || strings.Contains(lower, "<body") ||
		(strings.Count(lower, "<") > 5 && strings.Count(lower, ">") > 5) {
		return ContentHTML
	}

	// Check for JSON — only validate if it starts with { or [
	first := trimmed[0]
	if first == '{' || first == '[' {
		// Short strings: just validate directly (cheap)
		if len(trimmed) <= 200 {
			if json.Valid([]byte(trimmed)) {
				return ContentJSON
			}
		} else {
			// Longer strings: heuristic check first to avoid expensive json.Valid
			peek := trimmed[:200]
			jsonChars := 0
			for _, c := range []byte(peek) {
				if c == ':' || c == '"' || c == '{' || c == '}' || c == '[' || c == ']' {
					jsonChars++
				}
			}
			if jsonChars > len(peek)/7 {
				checkLen := len(trimmed)
				if checkLen > 65536 {
					checkLen = 65536
				}
				if json.Valid([]byte(trimmed[:checkLen])) {
					return ContentJSON
				}
			}
		}
	}

	// Check for terminal output (ANSI codes, common shell patterns)
	if hasTerminalPatterns(trimmed) {
		return ContentTerminal
	}

	return ContentPlain
}

// --- HTML Compression ---

var (
	htmlTagRe      = regexp.MustCompile(`<[^>]+>`)
	htmlStyleRe    = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	htmlScriptRe   = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	htmlCommentRe  = regexp.MustCompile(`<!--[\s\S]*?-->`)
	htmlNavRe      = regexp.MustCompile(`(?is)<nav[^>]*>.*?</nav>`)
	htmlFooterRe   = regexp.MustCompile(`(?is)<footer[^>]*>.*?</footer>`)
	htmlHeaderRe   = regexp.MustCompile(`(?is)<header[^>]*>.*?</header>`)
	multiSpaceRe   = regexp.MustCompile(`\s{2,}`)
	multiNewlineRe = regexp.MustCompile(`\n{3,}`)
)

func compressHTML(s string) string {
	// Remove non-content elements
	s = htmlScriptRe.ReplaceAllString(s, "")
	s = htmlStyleRe.ReplaceAllString(s, "")
	s = htmlCommentRe.ReplaceAllString(s, "")
	s = htmlNavRe.ReplaceAllString(s, "")
	s = htmlFooterRe.ReplaceAllString(s, "")
	s = htmlHeaderRe.ReplaceAllString(s, "")

	// Strip remaining tags
	s = htmlTagRe.ReplaceAllString(s, " ")

	// Collapse whitespace
	s = multiSpaceRe.ReplaceAllString(s, " ")
	s = multiNewlineRe.ReplaceAllString(s, "\n\n")
	s = strings.TrimSpace(s)

	// Shorten URLs in the text
	s = shortenURLsInText(s)

	return s
}

// --- JSON Compression ---

func compressJSON(s string) string {
	// Skip JSON compression for very large payloads — the cost of parsing
	// outweighs the benefit. Let the downstream size truncation handle it.
	if len(s) > 65536 {
		return s
	}
	var data interface{}
	if err := json.Unmarshal([]byte(s), &data); err != nil {
		return s // not valid JSON after all, return as-is
	}
	compressed := compressJSONValue(data, 0)
	out, err := json.Marshal(compressed)
	if err != nil {
		return s
	}
	return string(out)
}

func compressJSONValue(v interface{}, depth int) interface{} {
	if depth > 6 {
		return "[nested...]"
	}
	switch val := v.(type) {
	case map[string]interface{}:
		result := make(map[string]interface{}, len(val))
		for k, v2 := range val {
			result[k] = compressJSONValue(v2, depth+1)
		}
		return result
	case []interface{}:
		if len(val) <= 5 {
			out := make([]interface{}, len(val))
			for i, item := range val {
				out[i] = compressJSONValue(item, depth+1)
			}
			return out
		}
		// Keep first 3 + last 2, insert summary
		out := make([]interface{}, 0, 6)
		for i := 0; i < 3; i++ {
			out = append(out, compressJSONValue(val[i], depth+1))
		}
		out = append(out, fmt.Sprintf("[...省略 %d 项...]", len(val)-5))
		for i := len(val) - 2; i < len(val); i++ {
			out = append(out, compressJSONValue(val[i], depth+1))
		}
		return out
	case string:
		// Shorten long strings
		if len(val) > 500 {
			runes := []rune(val)
			if len(runes) > 200 {
				return string(runes[:200]) + "..."
			}
		}
		// Shorten URLs
		if isURL(val) {
			return shortenURL(val)
		}
		// Detect and replace base64
		if len(val) > 100 && looksLikeBase64(val) {
			return fmt.Sprintf("[base64 data, %d bytes]", len(val))
		}
		return val
	default:
		return val
	}
}

// --- Terminal Output Compression ---

var ansiEscapeRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func compressTerminal(s string) string {
	// Strip ANSI escape codes
	s = ansiEscapeRe.ReplaceAllString(s, "")

	// Remove carriage returns (progress bars)
	s = strings.ReplaceAll(s, "\r", "\n")

	lines := strings.Split(s, "\n")
	var result []string
	i := 0
	for i < len(lines) {
		line := strings.TrimRight(lines[i], " \t")

		// Skip empty lines in sequences of >2
		if line == "" {
			if len(result) == 0 || result[len(result)-1] != "" {
				result = append(result, "")
			}
			i++
			continue
		}

		// Collapse consecutive identical lines
		j := i + 1
		for j < len(lines) && strings.TrimRight(lines[j], " \t") == line {
			j++
		}
		if j-i >= 3 {
			result = append(result, line)
			result = append(result, fmt.Sprintf("[...重复 %d 行...]", j-i-1))
			i = j
			continue
		}

		// Collapse progress-bar-like lines (contain %, ████, or spinner chars)
		if isProgressLine(line) {
			// Skip all consecutive progress lines, keep only the last
			k := i + 1
			for k < len(lines) && isProgressLine(strings.TrimRight(lines[k], " \t")) {
				k++
			}
			if k-i > 2 {
				result = append(result, strings.TrimRight(lines[k-1], " \t"))
				i = k
				continue
			}
		}

		result = append(result, line)
		i++
	}

	return strings.Join(result, "\n")
}

// --- Plain Text Compression ---

func compressPlain(s string) string {
	// Shorten URLs
	s = shortenURLsInText(s)

	// Collapse multiple blank lines
	s = multiNewlineRe.ReplaceAllString(s, "\n\n")

	// Detect and replace base64 blocks
	s = replaceBase64Blocks(s)

	return s
}

// --- Helper Functions ---

var urlInTextRe = regexp.MustCompile(`https?://[^\s<>"')\]]+`)

func shortenURLsInText(s string) string {
	return urlInTextRe.ReplaceAllStringFunc(s, func(u string) string {
		return shortenURL(u)
	})
}

func shortenURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return rawURL
	}
	// Keep short URLs as-is
	if len(rawURL) <= 80 {
		return rawURL
	}
	path := parsed.Path
	if len(path) > 40 {
		parts := strings.Split(strings.Trim(path, "/"), "/")
		if len(parts) > 2 {
			path = "/" + parts[0] + "/.../" + parts[len(parts)-1]
		}
	}
	short := parsed.Scheme + "://" + parsed.Host + path
	if len(short) > 100 {
		short = short[:100] + "..."
	}
	return short
}

func isURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

var base64Re = regexp.MustCompile(`[A-Za-z0-9+/]{100,}={0,2}`)

func looksLikeBase64(s string) bool {
	if len(s) < 100 {
		return false
	}
	// Check if >80% of chars are base64 alphabet
	b64Chars := 0
	total := min(500, len(s))
	for i := 0; i < total; i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '+' || c == '/' || c == '=' {
			b64Chars++
		}
	}
	return float64(b64Chars)/float64(total) > 0.8
}

func replaceBase64Blocks(s string) string {
	return base64Re.ReplaceAllStringFunc(s, func(match string) string {
		if len(match) > 200 {
			return fmt.Sprintf("[base64 data, %d bytes]", len(match))
		}
		return match
	})
}

func hasTerminalPatterns(s string) bool {
	// ANSI escape codes — definitive terminal signal
	prefix := s
	if len(prefix) > 500 {
		prefix = prefix[:500]
	}
	if ansiEscapeRe.MatchString(prefix) {
		return true
	}
	// Common shell prompt patterns (require multiple signals to avoid
	// false positives with markdown headings or quoted text)
	lines := strings.SplitN(s, "\n", 10)
	shellPatterns := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "$ ") || // shell prompt (with space to avoid $variable)
			strings.Contains(trimmed, "exit code") ||
			strings.HasPrefix(trimmed, "npm ") || strings.HasPrefix(trimmed, "go ") ||
			strings.Contains(trimmed, "warning:") || strings.Contains(trimmed, "error:") ||
			strings.HasPrefix(trimmed, "PASS ") || strings.HasPrefix(trimmed, "FAIL ") {
			shellPatterns++
		}
	}
	return shellPatterns >= 3
}

func isProgressLine(line string) bool {
	return strings.Contains(line, "█") || strings.Contains(line, "▓") ||
		strings.Contains(line, "░") || strings.Contains(line, "━") ||
		(strings.Contains(line, "%") && (strings.Contains(line, "[") || strings.Contains(line, "("))) ||
		strings.Contains(line, "⠋") || strings.Contains(line, "⠙") ||
		strings.Contains(line, "⠹") || strings.Contains(line, "⠸")
}
