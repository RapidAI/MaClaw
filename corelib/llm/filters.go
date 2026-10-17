package llm

import (
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// Simple stateless filters
// ---------------------------------------------------------------------------

var (
	reThinkBlock             = regexp.MustCompile(`(?s)<think>.*?</think>\\s*`)
	reDetailsBlock           = regexp.MustCompile(`(?is)<details\b[^>]*>.*?</details>\s*`)
	reSummaryBlock           = regexp.MustCompile(`(?is)<summary\b[^>]*>.*?</summary>\s*`)
	reFuncCallBlock          = regexp.MustCompile(`(?s)<\\|FunctionCallBegin\\|>.*?<\\|FunctionCallEnd\\|>\\s*`)
	reToolCallBlock          = regexp.MustCompile(`(?is)<tool_call(?:\[\])?\b[^>]*>.*?</tool_call>\s*`)
	reToolCallOpenToEnd      = regexp.MustCompile(`(?is)<tool_call(?:\[\])?\b[^>]*>.*\z`)
	reLongcatToolCallBlock   = regexp.MustCompile(`(?is)<longcat_tool_call>.*?</longcat_tool_call>\s*`)
	reLongcatToolCallOpenEnd = regexp.MustCompile(`(?is)<longcat_tool_call>.*\z`)
	reFunctionEqBlock        = regexp.MustCompile(`(?is)<function=[A-Za-z0-9_.-]+>.*?</function>\s*`)
	reFunctionEqOpenToEnd    = regexp.MustCompile(`(?is)<function=[A-Za-z0-9_.-]+>.*\z`)
	reCodexToolCallBlock     = regexp.MustCompile(`(?s)<turn:\s*tool_call\s*>.*?</turn>\s*`)
	rePlainToolCallTail      = regexp.MustCompile(`(?is)\bTOOL_CALL\b\s*\{.*\}\s*`)
	reDSMLToolCallBlock      = regexp.MustCompile(`(?is)<\|DSML\|(?:` + dsmlBlockTags + `)\s*>.*?</\|DSML\|(?:` + dsmlBlockTags + `)>\s*`)
	reDSMLToolCallOpen       = regexp.MustCompile(`(?is)<\|DSML\|(?:` + dsmlTagTail + `).*\z`)
	reDSMLInvokeBlock        = regexp.MustCompile(`(?is)<\|DSML\|invoke\b[^>]*>.*?</\|DSML\|invoke>\s*`)
	// Some gateways extract a structured tool call and still leave the model's
	// private XML in reasoning_content. <thinking> wraps the prose. <tool_value>
	// and <tool_name> are the call payload, which is already on tool_calls.
	reToolValueBlock             = regexp.MustCompile(`(?is)<tool_value\b[^>]*>.*?</tool_value>\s*`)
	reToolValueOpenToEnd         = regexp.MustCompile(`(?is)<tool_value\b[^>]*>.*\z`)
	reToolNameBlock              = regexp.MustCompile(`(?is)<tool_name\b[^>]*>.*?</tool_name>\s*`)
	reToolNameOpenToEnd          = regexp.MustCompile(`(?is)<tool_name\b[^>]*>.*\z`)
	reThinkingBlock              = regexp.MustCompile(`(?is)<thinking\b[^>]*>(.*?)</thinking>`)
	reThinkingOpenToEnd          = regexp.MustCompile(`(?is)<thinking\b[^>]*>(.*)\z`)
	reOrphanReasoningProtocolTag = regexp.MustCompile(`(?i)</?(?:thinking|tool_value|tool_name)\b[^>]*>`)
)

func StripThinkTags(s string) string {
	return strings.TrimSpace(reThinkBlock.ReplaceAllString(s, ""))
}

func StripDetailsBlocks(s string) string {
	s = reDetailsBlock.ReplaceAllString(s, "")
	s = reSummaryBlock.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

func StripFunctionCalls(s string) string {
	return strings.TrimSpace(reFuncCallBlock.ReplaceAllString(s, ""))
}

func StripXMLToolCalls(s string) string {
	s = normalizeDSMLMarkup(s)
	// Hy3's </tool_call:SUFFIX> is not </tool_call>. The open-to-end pattern
	// below would otherwise delete every character after <tool_call:SUFFIX>.
	s = stripHy3ToolMarkup(s)
	s = reLongcatToolCallBlock.ReplaceAllString(s, "")
	s = reLongcatToolCallOpenEnd.ReplaceAllString(s, "")
	s = reToolCallBlock.ReplaceAllString(s, "")
	s = reToolCallOpenToEnd.ReplaceAllString(s, "")
	s = reFunctionEqBlock.ReplaceAllString(s, "")
	s = reFunctionEqOpenToEnd.ReplaceAllString(s, "")
	s = reCodexToolCallBlock.ReplaceAllString(s, "")
	s = rePlainToolCallTail.ReplaceAllString(s, "")
	s = reDSMLToolCallBlock.ReplaceAllString(s, "")
	s = reDSMLInvokeBlock.ReplaceAllString(s, "")
	s = reDSMLToolCallOpen.ReplaceAllString(s, "")
	// The stream drops a trailing fence that never became a tag. The stored
	// message uses this strip, so the same tail must not reappear there.
	if n := dsmlOpenSuffixLen(s); n > 0 && strings.HasPrefix(collapseDSMLFence(s[len(s)-n:]), "<|") {
		s = s[:len(s)-n]
	}
	if idx := leakedLineOrientedToolMarkerIndex(s); idx >= 0 {
		s = s[:idx]
	}
	return strings.TrimSpace(s)
}

func StripAllExtra(s string) string {
	return StripXMLToolCalls(StripFunctionCalls(StripDetailsBlocks(StripThinkTags(StripLeakedReasoningProtocol(s)))))
}

// StripLeakedReasoningProtocol removes gateway leftovers such as
// </tool_value> and </thinking> from a reasoning or content string. The
// surrounding prose stays. A <tool_value> or <tool_name> payload is dropped
// because that text is the tool call, not the thought. Text that merely
// mentions those words, without the tags, is unchanged.
func StripLeakedReasoningProtocol(s string) string {
	if !leakedReasoningProtocolPresent(s) {
		return s
	}
	next := reToolValueBlock.ReplaceAllString(s, "")
	next = reToolValueOpenToEnd.ReplaceAllString(next, "")
	next = reToolNameBlock.ReplaceAllString(next, "")
	next = reToolNameOpenToEnd.ReplaceAllString(next, "")
	next = reThinkingBlock.ReplaceAllString(next, "$1")
	next = reThinkingOpenToEnd.ReplaceAllString(next, "$1")
	next = reOrphanReasoningProtocolTag.ReplaceAllString(next, "")
	if next == s {
		return s
	}
	// Tags sit at the end of a thought. Trim that tail, but keep a leading
	// indent. A message that is only a <thinking> wrapper loses the single
	// newline the wrapper introduced, not the indent of the first line.
	next = strings.TrimRight(next, " \t\r\n")
	if strings.HasPrefix(strings.TrimLeft(s, " \t\r\n"), "<") {
		next = strings.TrimPrefix(next, "\r\n")
		next = strings.TrimPrefix(next, "\n")
		next = strings.TrimPrefix(next, "\r")
	}
	return next
}

func leakedReasoningProtocolPresent(s string) bool {
	for i := 0; i < len(s); {
		j := strings.IndexByte(s[i:], '<')
		if j < 0 {
			return false
		}
		i += j
		if reasoningProtocolTagAt(s[i:]) {
			return true
		}
		i++
	}
	return false
}

// reasoningProtocolTagAt reports whether s begins with a thinking/tool_value/
// tool_name tag, or an unfinished prefix of one. <thinkingish> is not a tag.
// Comparison is ASCII-only so a character whose lowercase form changes length
// cannot shift the cut point.
func reasoningProtocolTagAt(s string) bool {
	rest := s
	if hasASCIIPrefixFold(rest, "</") {
		rest = rest[2:]
	} else if len(rest) > 0 && rest[0] == '<' {
		rest = rest[1:]
	} else {
		return false
	}
	if rest == "" {
		return false
	}
	for _, name := range []string{"thinking", "tool_value", "tool_name"} {
		if hasASCIIPrefixFold(rest, name) {
			if len(rest) == len(name) {
				return true
			}
			switch rest[len(name)] {
			case '>', ' ', '\t', '\n', '\r', '/':
				return true
			}
			continue
		}
		if len(rest) < len(name) && hasASCIIPrefixFold(name, rest) {
			return true
		}
	}
	return false
}

func hasASCIIPrefixFold(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		if asciiFold(s[i]) != prefix[i] {
			return false
		}
	}
	return true
}

func asciiFold(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}

// reasoningProtocolMarkers are the tag beginnings a stream may still be
// spelling. Longer complete tags are matched before these prefixes.
var reasoningProtocolMarkers = []string{
	"</tool_value>",
	"<tool_value",
	"</tool_name>",
	"<tool_name",
	"</thinking>",
	"<thinking",
}

// reasoningProtocolFilter keeps </tool_value>, </thinking>, and the tool
// payload they wrap off the live thinking panel. Tags may arrive split
// across deltas, so a possible tag suffix stays buffered until the next
// delta confirms it.
type reasoningProtocolFilter struct {
	downstream TokenCallback
	pending    strings.Builder
	closer     string
}

func newReasoningProtocolStreamFilter(downstream TokenCallback) *reasoningProtocolFilter {
	if downstream == nil {
		return nil
	}
	return &reasoningProtocolFilter{downstream: downstream}
}

func (f *reasoningProtocolFilter) Write(delta string) {
	if f == nil || delta == "" {
		return
	}
	// Prose tokens never touch the tag scanner. This is the common path.
	if f.closer == "" && f.pending.Len() == 0 && strings.IndexByte(delta, '<') < 0 {
		f.emit(delta)
		return
	}
	f.pending.WriteString(delta)
	f.drain(false)
}

func (f *reasoningProtocolFilter) Flush() {
	if f != nil {
		f.drain(true)
	}
}

func (f *reasoningProtocolFilter) drain(force bool) {
	for {
		s := f.pending.String()
		if s == "" {
			return
		}
		if f.closer != "" {
			if idx := indexProtocolLiteral(s, f.closer); idx >= 0 {
				rest := s[idx+len(f.closer):]
				f.closer = ""
				f.pending.Reset()
				f.pending.WriteString(rest)
				continue
			}
			if !force {
				// Keep only a possible closer prefix. The payload itself is the
				// tool call and must not accumulate for the rest of the stream.
				n := reasoningProtocolSuffixLen(s, []string{f.closer})
				f.pending.Reset()
				if n > 0 {
					f.pending.WriteString(s[len(s)-n:])
				}
				return
			}
			f.closer = ""
			f.pending.Reset()
			return
		}

		idx := strings.IndexByte(s, '<')
		if idx < 0 {
			f.emit(s)
			f.pending.Reset()
			return
		}
		if idx > 0 {
			f.emit(s[:idx])
			f.pending.Reset()
			f.pending.WriteString(s[idx:])
			continue
		}
		kind, consume := matchReasoningProtocolHead(s)
		switch kind {
		case "drop":
			f.pending.Reset()
			f.pending.WriteString(s[consume:])
			continue
		case "value", "name":
			// <tool_value/> has no body. Waiting for a closer would swallow
			// every later token.
			opened := strings.TrimRight(strings.TrimSuffix(s[:consume], ">"), " \t\r\n")
			if strings.HasSuffix(opened, "/") {
				f.pending.Reset()
				f.pending.WriteString(s[consume:])
				continue
			}
			f.closer = "</tool_value>"
			if kind == "name" {
				f.closer = "</tool_name>"
			}
			f.pending.Reset()
			f.pending.WriteString(s[consume:])
			continue
		case "hold":
			if !force {
				return
			}
			if dropPartialReasoningProtocol(s) {
				f.pending.Reset()
				return
			}
			f.emit(s)
			f.pending.Reset()
			return
		default:
			// Not our tag. Emit through the next '<' in one slice so an HTML
			// answer is not recopied once per bracket.
			next := strings.IndexByte(s[1:], '<')
			if next < 0 {
				f.emit(s)
				f.pending.Reset()
				return
			}
			end := 1 + next
			f.emit(s[:end])
			f.pending.Reset()
			f.pending.WriteString(s[end:])
			continue
		}
	}
}

func (f *reasoningProtocolFilter) emit(s string) {
	if f != nil && f.downstream != nil && s != "" {
		f.downstream(s)
	}
}

func matchReasoningProtocolHead(s string) (kind string, consume int) {
	switch {
	case hasASCIIPrefixFold(s, "</tool_value>"):
		return "drop", len("</tool_value>")
	case hasASCIIPrefixFold(s, "</tool_name>"):
		return "drop", len("</tool_name>")
	case hasASCIIPrefixFold(s, "</thinking>"):
		return "drop", len("</thinking>")
	case hasASCIIPrefixFold(s, "<thinking>") || hasReasoningTagAttrs(s, "<thinking"):
		if n := reasoningTagConsume(s, "<thinking"); n > 0 {
			return "drop", n
		}
		return "hold", 0
	case hasReasoningTagAttrs(s, "<tool_value"):
		if n := reasoningTagConsume(s, "<tool_value"); n > 0 {
			return "value", n
		}
		return "hold", 0
	case hasReasoningTagAttrs(s, "<tool_name"):
		if n := reasoningTagConsume(s, "<tool_name"); n > 0 {
			return "name", n
		}
		return "hold", 0
	}
	if reasoningProtocolSuffixLen(s, reasoningProtocolMarkers) == len(s) {
		return "hold", 0
	}
	return "", 0
}

func hasReasoningTagAttrs(s, name string) bool {
	if !hasASCIIPrefixFold(s, name) {
		return false
	}
	if len(s) == len(name) {
		return true
	}
	switch s[len(name)] {
	case '>', ' ', '\t', '\n', '\r', '/':
		return true
	default:
		return false
	}
}

func reasoningTagConsume(s, name string) int {
	if !hasReasoningTagAttrs(s, name) || len(s) == len(name) {
		return 0
	}
	gt := strings.IndexByte(s, '>')
	if gt < 0 {
		return 0
	}
	return gt + 1
}

func indexProtocolLiteral(s, needle string) int {
	if needle == "" || len(s) < len(needle) {
		return -1
	}
	for i := 0; i+len(needle) <= len(s); {
		j := strings.IndexByte(s[i:], needle[0])
		if j < 0 {
			return -1
		}
		i += j
		if hasASCIIPrefixFold(s[i:], needle) {
			return i
		}
		i++
	}
	return -1
}

func reasoningProtocolSuffixLen(s string, markers []string) int {
	max := 0
	for _, marker := range markers {
		n := len(marker) - 1
		if len(s) < n {
			n = len(s)
		}
		for i := n; i > max; i-- {
			if hasASCIIPrefixFold(s[len(s)-i:], marker[:i]) {
				max = i
				break
			}
		}
	}
	return max
}

func dropPartialReasoningProtocol(s string) bool {
	// "<tool" is also the start of <tool_call> and of ordinary words. Only a
	// tail that has already picked tool_value, tool_name, or thinking is
	// discarded when the stream ends.
	for _, marker := range []string{"<tool_value", "</tool_value", "<tool_name", "</tool_name", "<thinking", "</thinking"} {
		if len(s) > len(marker) || !hasASCIIPrefixFold(marker, s) {
			continue
		}
		if hasASCIIPrefixFold(s, "<tool_v") || hasASCIIPrefixFold(s, "<tool_n") ||
			hasASCIIPrefixFold(s, "</tool_v") || hasASCIIPrefixFold(s, "</tool_n") ||
			hasASCIIPrefixFold(s, "<thinki") || hasASCIIPrefixFold(s, "</thinki") {
			return true
		}
	}
	return false
}
