package llm

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"regexp"
	"strings"
)

var (
	contentXMLToolCallBlockRe        = regexp.MustCompile(`(?is)<tool_call(?:\[\])?\b[^>]*>\s*(.*?)\s*</tool_call>`)
	contentAngleToolCallOpenRe       = regexp.MustCompile(`(?is)<tool_call(?:\[\])?\b[^>]*>`)
	contentXMLToolCallOpenToEndRe    = regexp.MustCompile(`(?is)<tool_call(?:\[\])?\b[^>]*>.*\z`)
	contentCodexToolCallBlockRe      = regexp.MustCompile(`(?s)<turn:\s*tool_call\s*>(.*?)</turn>`)
	contentCodexToolCallMarkerRe     = regexp.MustCompile(`(?is)<turn:\s*tool_call\b`)
	contentPlainToolCallMarkerRe     = regexp.MustCompile(`(?is)\bTOOL_CALL\b\s*`)
	contentCodexToolInvokeRe         = regexp.MustCompile(`(?s)<invoke\b([^>]*)>(.*?)</invoke>`)
	contentCodexToolParameterRe      = regexp.MustCompile(`(?s)<parameter\b([^>]*)>(.*?)</parameter>`)
	contentCodexToolAttributeRe      = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_:-]*)\s*=\s*"([^"]*)"`)
	contentFunctionEqBlockRe         = regexp.MustCompile(`(?is)<function=([A-Za-z0-9_.-]+)>(.*?)</function>`)
	contentFunctionEqOpenRe          = regexp.MustCompile(`(?is)<function=([A-Za-z0-9_.-]+)>`)
	contentGLMArgPairRe              = regexp.MustCompile(`(?is)<arg_key>\s*(.*?)\s*</arg_key>\s*<arg_value>(.*?)</arg_value>`)
	contentQwenParamEqRe             = regexp.MustCompile(`(?is)<parameter=([^>]+)>(.*?)</parameter>`)
	contentLeadingToolNameRe         = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_.-]*)`)
	dsmlSpacedFenceRe                = regexp.MustCompile(`(?i)\|\s*DSML\s*\|`)
	dsmlLooseOpenRe                  = regexp.MustCompile(`(?i)<\s*\|DSML\|`)
	dsmlLooseCloseRe                 = regexp.MustCompile(`(?i)</\s*\|DSML\|`)
	dsmlTagSpaceRe                   = regexp.MustCompile(`(?i)(</?)\|DSML\|\s+`)
	dsmlAltCloseRe                   = regexp.MustCompile(`(?i)<\|DSML\|/([A-Za-z_]+)>`)
	dsmlCollapsePipeSpaceRe          = regexp.MustCompile(`\s*\|\s*`)
	dsmlCollapseOpenSpaceRe          = regexp.MustCompile(`<\s+`)
	dsmlParameterOpenRe              = regexp.MustCompile(`(?i)<\|DSML\|parameter`)
	dsmlParameterCloseRe             = regexp.MustCompile(`(?i)</\|DSML\|parameter>`)
	contentDSMLBlockRe               = regexp.MustCompile(`(?is)<\|DSML\|(?:tool_calls|function_calls)\s*>(.*?)</\|DSML\|(?:tool_calls|function_calls)>`)
	contentDSMLBlockOpenRe           = regexp.MustCompile(`(?is)<\|DSML\|(?:tool_calls|function_calls)\b`)
	contentDSMLInvokeRe              = regexp.MustCompile(`(?is)<\|DSML\|invoke\b([^>]*)>(.*?)</\|DSML\|invoke>`)
	contentDSMLInvokeOpenRe          = regexp.MustCompile(`(?is)<\|DSML\|invoke\b`)
	contentDSMLMarkerRe              = regexp.MustCompile(`(?i)<\s*[|\x{FF5C}]\s*DSML\s*[|\x{FF5C}]\s*(?:tool_calls|function_calls|invoke|parameter)\b`)
	MalformedContentToolCallErrorMsg = "模型返回了无法解析的工具调用，已拦截原始工具 XML。请重试，或切换更兼容 OpenAI tool_calls 的模型。"
)

// ParseContentToolCallsDetailed extracts tool calls emitted in assistant
// content by OpenAI-compatible providers that fail to populate tool_calls.
func ParseContentToolCallsDetailed(content string) ([]ToolCall, bool) {
	if looksLikeDSMLContent(content) {
		if calls, malformed := parseDSMLContentToolCalls(content); len(calls) > 0 || malformed {
			return calls, malformed
		}
	}
	matches := contentXMLToolCallBlockRe.FindAllStringSubmatch(content, -1)
	var calls []ToolCall
	malformed := false
	for _, m := range matches {
		if len(m) < 2 {
			malformed = true
			continue
		}
		body := strings.TrimSpace(m[1])
		parsed, fragMalformed := parseContentToolCallFragments(m[0], body)
		if len(parsed) > 0 {
			calls = append(calls, parsed...)
		}
		if fragMalformed && len(parsed) == 0 {
			malformed = true
		}
	}
	fnCalls, fnMalformed := parseFunctionEqContentToolCalls(contentResidualAfterXMLToolCalls(content))
	if len(fnCalls) > 0 {
		calls = append(calls, fnCalls...)
	}
	if fnMalformed {
		malformed = true
	}
	codexCalls, codexMalformed := parseCodexContentToolCalls(contentXMLToolCallBlockRe.ReplaceAllString(content, ""))
	if len(codexCalls) > 0 {
		calls = append(calls, codexCalls...)
	}
	if codexMalformed {
		malformed = true
	}
	angleCalls, angleMalformed := parseUnclosedAngleContentToolCalls(content)
	if len(angleCalls) > 0 {
		calls = append(calls, angleCalls...)
	}
	if angleMalformed {
		malformed = true
	}
	jsonCalls, jsonMalformed := parseBareJSONContentToolCalls(content)
	if len(jsonCalls) > 0 {
		calls = append(calls, jsonCalls...)
	}
	if jsonMalformed {
		malformed = true
	}
	plainCalls, plainMalformed := parsePlainContentToolCalls(content)
	if len(plainCalls) > 0 {
		calls = append(calls, plainCalls...)
	}
	if plainMalformed {
		malformed = true
	}
	if malformed && len(calls) == 0 {
		logMalformedContentToolCall(content)
	}
	return calls, malformed
}

func parseUnclosedAngleContentToolCalls(content string) ([]ToolCall, bool) {
	matches := contentAngleToolCallOpenRe.FindAllStringIndex(content, -1)
	if len(matches) == 0 {
		return nil, false
	}
	var calls []ToolCall
	malformed := false
	for _, match := range matches {
		rest := content[match[1]:]
		if strings.Contains(strings.ToLower(rest), "</tool_call>") {
			continue
		}
		open := content[match[0]:match[1]]
		parsed, fragMalformed := parseContentToolCallFragments(open, strings.TrimSpace(rest))
		if len(parsed) > 0 {
			calls = append(calls, parsed...)
			continue
		}
		if fragMalformed {
			malformed = true
		}
	}
	return calls, malformed
}

func parseContentToolCallFragments(open, body string) ([]ToolCall, bool) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, true
	}
	if call, ok := parseContentJSONToolCallPayload(body); ok {
		return []ToolCall{call}, false
	}
	if looksLikeFunctionEqToolBody(body) {
		if fnCalls, fnMalformed := parseFunctionEqContentToolCalls(body); len(fnCalls) > 0 {
			return fnCalls, fnMalformed
		} else if fnMalformed {
			return nil, true
		}
	}
	if call, ok := parseMarkupContentToolCall(open, body); ok {
		return []ToolCall{call}, false
	}
	if raw, ok := extractJSONObjectAfter(body); ok {
		if call, parsed := parseContentJSONToolCallPayload(raw); parsed {
			return []ToolCall{call}, false
		}
	}
	return nil, true
}

func looksLikeFunctionEqToolBody(body string) bool {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return false
	}
	if idx := strings.IndexByte(trimmed, '<'); idx > 0 {
		trimmed = strings.TrimSpace(trimmed[idx:])
	}
	return strings.HasPrefix(strings.ToLower(trimmed), "<function=")
}

func parseFunctionEqContentToolCalls(content string) ([]ToolCall, bool) {
	matches := contentFunctionEqBlockRe.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return nil, contentFunctionEqOpenRe.MatchString(content)
	}
	var calls []ToolCall
	malformed := false
	for _, m := range matches {
		if len(m) < 3 {
			malformed = true
			continue
		}
		call, ok := parseNamedMarkupToolCall(strings.TrimSpace(m[1]), strings.TrimSpace(m[2]))
		if ok {
			calls = append(calls, call)
		} else {
			malformed = true
		}
	}
	return calls, malformed
}

func parseMarkupContentToolCall(full, body string) (ToolCall, bool) {
	open := full
	if end := strings.IndexByte(full, '>'); end >= 0 {
		open = full[:end+1]
	}
	attrs := parseCodexContentAttrs(open)
	if call, ok := parseNamedMarkupToolCall(attrs["name"], body); ok {
		return call, true
	}
	if loc := contentCodexToolInvokeRe.FindStringSubmatch(body); len(loc) >= 3 {
		if call, ok := parseCodexContentInvoke(loc); ok {
			return call, true
		}
	}
	if loc := contentFunctionEqBlockRe.FindStringSubmatch(body); len(loc) >= 3 {
		if call, ok := parseNamedMarkupToolCall(loc[1], loc[2]); ok {
			return call, true
		}
	}
	if name, rest, ok := splitLeadingToolName(body); ok {
		if call, parsed := parseNamedMarkupToolCall(name, rest); parsed {
			return call, true
		}
	}
	return ToolCall{}, false
}

func parseNamedMarkupToolCall(name, body string) (ToolCall, bool) {
	name = strings.TrimSpace(html.UnescapeString(name))
	body = strings.TrimSpace(body)
	if name == "" && body != "" {
		if next, rest, ok := splitLeadingToolName(body); ok {
			name = next
			body = rest
		}
	}
	if name == "" {
		return ToolCall{}, false
	}
	if args, ok := parseMarkupToolCallArguments(body); ok {
		return normalizePlainContentToolCall(name, args)
	}
	if call, ok := parseContentJSONToolCallPayload(body); ok {
		if strings.TrimSpace(call.Function.Name) == "" {
			call.Function.Name = name
		}
		return call, true
	}
	if args, ok := extractJSONObjectAfter(body); ok {
		return normalizePlainContentToolCall(name, json.RawMessage(args))
	}
	return ToolCall{}, false
}

func contentResidualAfterXMLToolCalls(content string) string {
	residual := contentXMLToolCallBlockRe.ReplaceAllString(content, "")
	residual = contentCodexToolCallBlockRe.ReplaceAllString(residual, "")
	residual = contentXMLToolCallOpenToEndRe.ReplaceAllString(residual, "")
	return strings.TrimSpace(residual)
}

func parseMarkupToolCallArguments(body string) (json.RawMessage, bool) {
	if pairs := contentGLMArgPairRe.FindAllStringSubmatch(body, -1); len(pairs) > 0 {
		return marshalMarkupArgPairs(pairs)
	}
	if pairs := contentQwenParamEqRe.FindAllStringSubmatch(body, -1); len(pairs) > 0 {
		return marshalMarkupArgPairs(pairs)
	}
	if params := contentCodexToolParameterRe.FindAllStringSubmatch(body, -1); len(params) > 0 {
		args := make(map[string]interface{}, len(params))
		for _, p := range params {
			if len(p) < 3 {
				return nil, false
			}
			paramName := strings.TrimSpace(parseCodexContentAttrs(p[1])["name"])
			if paramName == "" {
				return nil, false
			}
			args[paramName] = strings.TrimSpace(html.UnescapeString(p[2]))
		}
		raw, err := json.Marshal(args)
		if err != nil {
			return nil, false
		}
		return raw, true
	}
	return nil, false
}

func marshalMarkupArgPairs(pairs [][]string) (json.RawMessage, bool) {
	args := make(map[string]interface{}, len(pairs))
	for _, p := range pairs {
		if len(p) < 3 {
			return nil, false
		}
		key := strings.TrimSpace(html.UnescapeString(p[1]))
		if key == "" {
			return nil, false
		}
		args[key] = strings.TrimSpace(html.UnescapeString(p[2]))
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return nil, false
	}
	return raw, true
}

func splitLeadingToolName(body string) (string, string, bool) {
	m := contentLeadingToolNameRe.FindStringSubmatch(body)
	if len(m) < 2 {
		return "", body, false
	}
	name := strings.TrimSpace(m[1])
	if name == "" {
		return "", body, false
	}
	rest := strings.TrimSpace(body[len(m[0]):])
	if rest == "" || rest[0] == '<' || rest[0] == '{' {
		return name, rest, true
	}
	return "", body, false
}

func logMalformedContentToolCall(content string) {
	kind := "unknown"
	lower := strings.ToLower(content)
	switch {
	case strings.Contains(lower, "<function="):
		kind = "function"
	case strings.Contains(lower, "<arg_key>"):
		kind = "glm_arg_key"
	case strings.Contains(lower, "<turn: tool_call"):
		kind = "codex"
	case strings.Contains(lower, "<tool_call"):
		kind = "tool_call"
	case strings.Contains(lower, "tool_call"):
		kind = "plain"
	}
	log.Printf("[LLM] intercepted unparseable content tool markup kind=%s bytes=%d", kind, len(content))
}

func parseBareJSONContentToolCalls(content string) ([]ToolCall, bool) {
	raw := strings.TrimSpace(contentToolCallJSONCandidate(content))
	if raw == "" || (raw[0] != '{' && raw[0] != '[') {
		return nil, false
	}
	if raw[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal([]byte(raw), &items); err != nil {
			return nil, false
		}
		var calls []ToolCall
		malformed := false
		for _, item := range items {
			var obj map[string]json.RawMessage
			if err := json.Unmarshal(item, &obj); err != nil || !bareJSONLooksLikeToolCall(obj) {
				malformed = true
				continue
			}
			call, ok := parsePlainJSONToolCallPayload(string(item))
			if ok {
				calls = append(calls, call)
			} else {
				malformed = true
			}
		}
		if len(calls) == 0 {
			return nil, false
		}
		return calls, malformed
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return nil, false
	}
	if toolCallsRaw, ok := obj["tool_calls"]; ok {
		var items []json.RawMessage
		if err := json.Unmarshal(toolCallsRaw, &items); err != nil {
			return nil, true
		}
		var calls []ToolCall
		malformed := false
		for _, item := range items {
			var obj map[string]json.RawMessage
			if err := json.Unmarshal(item, &obj); err != nil || !bareJSONLooksLikeToolCall(obj) {
				malformed = true
				continue
			}
			call, ok := parsePlainJSONToolCallPayload(string(item))
			if ok {
				calls = append(calls, call)
			} else {
				malformed = true
			}
		}
		return calls, malformed || len(calls) == 0
	}
	if !bareJSONLooksLikeToolCall(obj) {
		return nil, false
	}
	call, ok := parsePlainJSONToolCallPayload(raw)
	if !ok {
		return nil, true
	}
	return []ToolCall{call}, false
}

func contentToolCallJSONCandidate(content string) string {
	trimmed := strings.TrimSpace(content)
	if strings.HasPrefix(trimmed, "```") {
		lines := strings.Split(trimmed, "\n")
		if len(lines) >= 2 {
			lines = lines[1:]
			if last := len(lines) - 1; last >= 0 && strings.TrimSpace(lines[last]) == "```" {
				lines = lines[:last]
			}
			return strings.Join(lines, "\n")
		}
	}
	return trimmed
}

func bareJSONLooksLikeToolCall(obj map[string]json.RawMessage) bool {
	if _, ok := obj["function"]; ok {
		return true
	}
	if _, ok := obj["function_call"]; ok {
		return true
	}
	hasName := false
	for _, key := range []string{"name", "tool", "tool_name"} {
		if _, ok := obj[key]; ok {
			hasName = true
			break
		}
	}
	if !hasName {
		return false
	}
	for _, key := range []string{"arguments", "args", "parameters", "input"} {
		if _, ok := obj[key]; ok {
			return true
		}
	}
	return false
}

type contentToolCallDeltaFilter struct {
	downstream      TokenCallback
	downstreamFlush func()
	pending         strings.Builder
	suppressed      bool
}

func newContentToolCallDeltaFilter(downstream TokenCallback) *contentToolCallDeltaFilter {
	if downstream != nil {
		details := &detailsFilter{downstream: downstream}
		downstream = details.Write
		return &contentToolCallDeltaFilter{downstream: downstream, downstreamFlush: details.Flush}
	}
	return &contentToolCallDeltaFilter{downstream: downstream}
}

func (f *contentToolCallDeltaFilter) Write(delta string) {
	if f == nil || f.downstream == nil || f.suppressed {
		return
	}
	f.pending.WriteString(delta)
	f.drain(false)
}

func (f *contentToolCallDeltaFilter) Flush() {
	if f == nil || f.downstream == nil {
		return
	}
	f.drain(true)
	if f.downstreamFlush != nil {
		f.downstreamFlush()
	}
}

func (f *contentToolCallDeltaFilter) drain(force bool) {
	if f.suppressed {
		f.pending.Reset()
		return
	}
	s := f.pending.String()
	if s == "" {
		return
	}
	if looksLikeBareJSONToolCallStreamPrefix(s) {
		if !force {
			return
		}
		if calls, malformed := parseBareJSONContentToolCalls(s); len(calls) > 0 || malformed {
			f.suppressed = true
			f.pending.Reset()
			return
		}
	}
	visible, hold, suppress := HoldContentToolCallStream(s, force)
	if visible != "" {
		f.downstream(visible)
	}
	if suppress {
		f.suppressed = true
		f.pending.Reset()
		return
	}
	f.pending.Reset()
	if hold != "" {
		f.pending.WriteString(hold)
	}
}

func looksLikeBareJSONToolCallStreamPrefix(content string) bool {
	trimmed := strings.TrimLeft(content, " \t\r\n")
	if trimmed == "" {
		return true
	}
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix("```json", lower) || strings.HasPrefix(lower, "```json") {
		return true
	}
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return true
	}
	return false
}

// FirstContentToolCallMarkerIndex is the first byte index of a content-emitted
// tool-call marker (XML, Codex, DSML, or plain TOOL_CALL). Stream filters use
// this so DeepSeek DSML is hidden the same way as <tool_call>.
func FirstContentToolCallMarkerIndex(s string) int {
	return firstContentToolCallMarkerIndex(s)
}

// ContentToolCallMarkerSuffixLen is the trailing byte count that may still
// grow into a content tool-call marker. Stream filters must hold that suffix.
func ContentToolCallMarkerSuffixLen(s string) int {
	return contentToolCallMarkerSuffixLen(s)
}

// HoldContentToolCallStream splits buffered stream text into a safe visible
// prefix and an unconfirmed suffix. suppress means the rest of the stream is
// a tool-call body and must not reach the chat. On flush, a partial DSML
// fence is dropped instead of leaked.
func HoldContentToolCallStream(s string, force bool) (visible, hold string, suppress bool) {
	if idx := firstContentToolCallMarkerIndex(s); idx >= 0 {
		if idx > 0 {
			visible = s[:idx]
		}
		return visible, "", true
	}
	partial := contentToolCallMarkerSuffixLen(s)
	if partial <= 0 {
		return s, "", false
	}
	visible = s[:len(s)-partial]
	suffix := s[len(s)-partial:]
	if !force {
		return visible, suffix, false
	}
	if dropPartialContentToolCallOnFlush(suffix) {
		return visible, "", true
	}
	return s, "", false
}

func dropPartialContentToolCallOnFlush(suffix string) bool {
	collapsed := collapseDSMLFence(suffix)
	if strings.HasPrefix(collapsed, "<|") {
		return true
	}
	lower := strings.ToLower(strings.TrimLeft(suffix, " \t\r\n"))
	return strings.HasPrefix(lower, "<tool_call") || strings.HasPrefix(lower, "<turn: tool_call") || strings.HasPrefix(lower, "<function=")
}

func firstContentToolCallMarkerIndex(s string) int {
	lower := strings.ToLower(s)
	best := -1
	for _, marker := range []string{"<tool_call", "<turn: tool_call", "<function=", "tool_call\n", "tool_call\r\n", "tool_call {"} {
		if idx := strings.Index(lower, marker); idx >= 0 && (best < 0 || idx < best) {
			best = idx
		}
	}
	if loc := contentDSMLMarkerRe.FindStringIndex(s); loc != nil && (best < 0 || loc[0] < best) {
		best = loc[0]
	}
	return best
}

func contentToolCallMarkerSuffixLen(s string) int {
	lower := strings.ToLower(s)
	best := 0
	for _, marker := range []string{"<tool_call", "<turn: tool_call", "<function=", "tool_call\n", "tool_call\r\n", "tool_call {"} {
		max := len(marker) - 1
		if len(lower) < max {
			max = len(lower)
		}
		for i := max; i > best; i-- {
			if strings.HasSuffix(lower, marker[:i]) {
				best = i
				break
			}
		}
	}
	if n := dsmlOpenSuffixLen(s); n > best {
		best = n
	}
	return best
}

func looksLikeDSMLContent(s string) bool {
	if contentDSMLMarkerRe.MatchString(s) {
		return true
	}
	return strings.Contains(collapseDSMLFence(s), "<|dsml|")
}

func collapseDSMLFence(s string) string {
	s = strings.ToLower(strings.ReplaceAll(s, "\uFF5C", "|"))
	s = dsmlCollapseOpenSpaceRe.ReplaceAllString(s, "<")
	return dsmlCollapsePipeSpaceRe.ReplaceAllString(s, "|")
}

func dsmlOpenSuffixLen(s string) int {
	idx := strings.LastIndex(s, "<")
	if idx < 0 {
		return 0
	}
	tail := s[idx:]
	if contentDSMLMarkerRe.MatchString(tail) {
		return 0
	}
	collapsed := collapseDSMLFence(tail)
	for _, marker := range []string{"<|dsml|tool_calls", "<|dsml|function_calls", "<|dsml|invoke", "<|dsml|parameter"} {
		if len(collapsed) < len(marker) && strings.HasPrefix(marker, collapsed) {
			return len(s) - idx
		}
	}
	return 0
}

func normalizeDSMLMarkup(s string) string {
	s = strings.ReplaceAll(s, "\uFF5C", "|")
	s = dsmlSpacedFenceRe.ReplaceAllString(s, "|DSML|")
	s = dsmlLooseOpenRe.ReplaceAllString(s, "<|DSML|")
	s = dsmlLooseCloseRe.ReplaceAllString(s, "</|DSML|")
	s = strings.ReplaceAll(s, "|DSML| /", "|DSML|/")
	s = dsmlTagSpaceRe.ReplaceAllString(s, "${1}|DSML|")
	return dsmlAltCloseRe.ReplaceAllString(s, "</|DSML|$1>")
}

func parsePlainContentToolCalls(content string) ([]ToolCall, bool) {
	matches := contentPlainToolCallMarkerRe.FindAllStringIndex(content, -1)
	if len(matches) == 0 {
		return nil, false
	}
	var calls []ToolCall
	malformed := false
	for _, match := range matches {
		if match[0] > 0 {
			prev := content[match[0]-1]
			if prev == '<' || prev == '/' {
				continue
			}
		}
		prefixStart := match[0] - len("<turn: ")
		if prefixStart < 0 {
			prefixStart = 0
		}
		if strings.Contains(strings.ToLower(content[prefixStart:match[0]]), "<turn:") {
			continue
		}
		raw, ok := extractJSONObjectAfter(content[match[1]:])
		if !ok {
			malformed = true
			continue
		}
		call, ok := parsePlainJSONToolCallPayload(raw)
		if ok {
			calls = append(calls, call)
		} else {
			malformed = true
		}
	}
	return calls, malformed
}

func extractJSONObjectAfter(s string) (string, bool) {
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return "", false
	}
	inString := false
	escaped := false
	depth := 0
	for i := start; i < len(s); i++ {
		ch := s[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1], true
			}
		}
	}
	return "", false
}

func parsePlainJSONToolCallPayload(raw string) (ToolCall, bool) {
	var parsed struct {
		ID         string          `json:"id"`
		CallID     string          `json:"call_id"`
		Type       string          `json:"type"`
		Name       string          `json:"name"`
		Tool       string          `json:"tool"`
		ToolName   string          `json:"tool_name"`
		Function   json.RawMessage `json:"function"`
		FuncCall   json.RawMessage `json:"function_call"`
		Arguments  json.RawMessage `json:"arguments"`
		Args       json.RawMessage `json:"args"`
		Parameters json.RawMessage `json:"parameters"`
		Input      json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return ToolCall{}, false
	}
	name := strings.TrimSpace(parsed.Name)
	if name == "" {
		name = strings.TrimSpace(parsed.Tool)
	}
	if name == "" {
		name = strings.TrimSpace(parsed.ToolName)
	}
	if name == "" && len(parsed.Function) > 0 {
		var fnName string
		if err := json.Unmarshal(parsed.Function, &fnName); err == nil {
			name = strings.TrimSpace(fnName)
		} else {
			var fnObj struct {
				Name       string          `json:"name"`
				Arguments  json.RawMessage `json:"arguments"`
				Args       json.RawMessage `json:"args"`
				Parameters json.RawMessage `json:"parameters"`
				Input      json.RawMessage `json:"input"`
			}
			if err := json.Unmarshal(parsed.Function, &fnObj); err == nil {
				name = strings.TrimSpace(fnObj.Name)
				if len(parsed.Arguments) == 0 && len(parsed.Args) == 0 && len(parsed.Parameters) == 0 && len(parsed.Input) == 0 {
					parsed.Arguments = fnObj.Arguments
					parsed.Args = fnObj.Args
					parsed.Parameters = fnObj.Parameters
					parsed.Input = fnObj.Input
				}
			}
		}
	}
	if name == "" && len(parsed.FuncCall) > 0 {
		var fnObj struct {
			Name       string          `json:"name"`
			Arguments  json.RawMessage `json:"arguments"`
			Args       json.RawMessage `json:"args"`
			Parameters json.RawMessage `json:"parameters"`
			Input      json.RawMessage `json:"input"`
		}
		if err := json.Unmarshal(parsed.FuncCall, &fnObj); err == nil {
			name = strings.TrimSpace(fnObj.Name)
			if len(parsed.Arguments) == 0 && len(parsed.Args) == 0 && len(parsed.Parameters) == 0 && len(parsed.Input) == 0 {
				parsed.Arguments = fnObj.Arguments
				parsed.Args = fnObj.Args
				parsed.Parameters = fnObj.Parameters
				parsed.Input = fnObj.Input
			}
		}
	}
	if name == "" {
		return ToolCall{}, false
	}
	args := parsed.Arguments
	if len(args) == 0 {
		args = parsed.Args
	}
	if len(args) == 0 {
		args = parsed.Parameters
	}
	if len(args) == 0 {
		args = parsed.Input
	}
	id := strings.TrimSpace(parsed.ID)
	if id == "" {
		id = strings.TrimSpace(parsed.CallID)
	}
	return normalizePlainContentToolCallWithID(id, parsed.Type, name, args)
}

func normalizePlainContentToolCall(name string, args json.RawMessage) (ToolCall, bool) {
	return normalizePlainContentToolCallWithID("", "", name, args)
}

func normalizePlainContentToolCallWithID(id, callType, name string, args json.RawMessage) (ToolCall, bool) {
	argsString, ok := normalizeContentToolCallArguments(args)
	if !ok {
		return ToolCall{}, false
	}
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "ssh_execute_command":
		var mm map[string]interface{}
		if err := json.Unmarshal([]byte(argsString), &mm); err != nil {
			return ToolCall{}, false
		}
		if username, ok := mm["username"]; ok {
			mm["user"] = username
			delete(mm, "username")
		}
		if command, ok := mm["command"]; ok {
			mm["initial_command"] = command
		}
		mm["action"] = "connect"
		normalized, err := json.Marshal(mm)
		if err != nil {
			return ToolCall{}, false
		}
		return makeContentToolCallWithID(id, callType, "ssh", string(normalized)), true
	default:
		if !json.Valid([]byte(argsString)) {
			return ToolCall{}, false
		}
		return makeContentToolCallWithID(id, callType, name, argsString), true
	}
}

func normalizeContentToolCallArguments(args json.RawMessage) (string, bool) {
	argsString := strings.TrimSpace(string(args))
	if argsString == "" || argsString == "null" {
		return "{}", true
	}
	var encoded string
	if err := json.Unmarshal([]byte(argsString), &encoded); err == nil {
		argsString = strings.TrimSpace(encoded)
		if argsString == "" {
			return "{}", true
		}
	}
	if !json.Valid([]byte(argsString)) {
		return "", false
	}
	return argsString, true
}

func parseContentJSONToolCallPayload(raw string) (ToolCall, bool) {
	return parsePlainJSONToolCallPayload(raw)
}

func parseCodexContentToolCalls(content string) ([]ToolCall, bool) {
	blocks := contentCodexToolCallBlockRe.FindAllStringSubmatch(content, -1)
	if len(blocks) == 0 {
		return nil, contentCodexToolCallMarkerRe.MatchString(content)
	}
	var calls []ToolCall
	malformed := false
	for _, block := range blocks {
		if len(block) < 2 {
			malformed = true
			continue
		}
		body := strings.TrimSpace(block[1])
		if body == "" {
			continue
		}
		invokes := contentCodexToolInvokeRe.FindAllStringSubmatch(body, -1)
		if len(invokes) == 0 {
			parsed, fragMalformed := parseContentToolCallFragments(block[0], body)
			if len(parsed) > 0 {
				calls = append(calls, parsed...)
				continue
			}
			if fragMalformed {
				malformed = true
			}
			continue
		}
		for _, inv := range invokes {
			call, ok := parseCodexContentInvoke(inv)
			if ok {
				calls = append(calls, call)
			} else {
				malformed = true
			}
		}
	}
	return calls, malformed
}

func parseDSMLContentToolCalls(content string) ([]ToolCall, bool) {
	content = normalizeDSMLMarkup(content)
	blocks := contentDSMLBlockRe.FindAllStringSubmatch(content, -1)
	if len(blocks) == 0 {
		invokes := contentDSMLInvokeRe.FindAllStringSubmatch(content, -1)
		if len(invokes) == 0 {
			return nil, contentDSMLBlockOpenRe.MatchString(content) || contentDSMLInvokeOpenRe.MatchString(content)
		}
		return parseDSMLInvokes(invokes)
	}
	var calls []ToolCall
	malformed := false
	for _, block := range blocks {
		if len(block) < 2 {
			malformed = true
			continue
		}
		invokes := contentDSMLInvokeRe.FindAllStringSubmatch(block[1], -1)
		if len(invokes) == 0 {
			malformed = true
			continue
		}
		parsed, invMalformed := parseDSMLInvokes(invokes)
		calls = append(calls, parsed...)
		if invMalformed {
			malformed = true
		}
	}
	residual := strings.TrimSpace(contentDSMLBlockRe.ReplaceAllString(content, ""))
	if residual != "" {
		if extra := contentDSMLInvokeRe.FindAllStringSubmatch(residual, -1); len(extra) > 0 {
			parsed, extraMalformed := parseDSMLInvokes(extra)
			calls = append(calls, parsed...)
			if extraMalformed {
				malformed = true
			}
			residual = strings.TrimSpace(contentDSMLInvokeRe.ReplaceAllString(residual, ""))
		}
		if contentDSMLBlockOpenRe.MatchString(residual) || contentDSMLInvokeOpenRe.MatchString(residual) {
			malformed = true
		}
	}
	return calls, malformed
}

func parseDSMLInvokes(invokes [][]string) ([]ToolCall, bool) {
	var calls []ToolCall
	malformed := false
	for _, inv := range invokes {
		if len(inv) < 3 {
			malformed = true
			continue
		}
		body := dsmlParameterOpenRe.ReplaceAllString(inv[2], "<parameter")
		body = dsmlParameterCloseRe.ReplaceAllString(body, "</parameter>")
		call, ok := parseCodexContentInvoke([]string{inv[0], inv[1], body})
		if ok {
			calls = append(calls, call)
		} else {
			malformed = true
		}
	}
	return calls, malformed
}

func parseCodexContentInvoke(inv []string) (ToolCall, bool) {
	if len(inv) < 3 {
		return ToolCall{}, false
	}
	attrs := parseCodexContentAttrs(inv[1])
	name := strings.TrimSpace(attrs["name"])
	if name == "" {
		return ToolCall{}, false
	}
	params := contentCodexToolParameterRe.FindAllStringSubmatch(inv[2], -1)
	args := make(map[string]interface{}, len(params))
	for _, p := range params {
		if len(p) < 3 {
			return ToolCall{}, false
		}
		paramAttrs := parseCodexContentAttrs(p[1])
		paramName := strings.TrimSpace(paramAttrs["name"])
		if paramName == "" {
			return ToolCall{}, false
		}
		rawValue := strings.TrimSpace(html.UnescapeString(p[2]))
		if strings.EqualFold(paramAttrs["string"], "false") {
			var decoded interface{}
			if err := json.Unmarshal([]byte(rawValue), &decoded); err == nil {
				args[paramName] = decoded
				continue
			}
		}
		args[paramName] = rawValue
	}
	argBytes, err := json.Marshal(args)
	if err != nil {
		return ToolCall{}, false
	}
	return makeContentToolCall(html.UnescapeString(name), string(argBytes)), true
}

func parseCodexContentAttrs(raw string) map[string]string {
	attrs := map[string]string{}
	for _, m := range contentCodexToolAttributeRe.FindAllStringSubmatch(raw, -1) {
		if len(m) == 3 {
			attrs[strings.TrimSpace(m[1])] = html.UnescapeString(m[2])
		}
	}
	return attrs
}

func makeContentToolCall(name, args string) ToolCall {
	return makeContentToolCallWithID("", "", name, args)
}

func makeContentToolCallWithID(id, callType, name, args string) ToolCall {
	id = strings.TrimSpace(id)
	if id == "" {
		id = randomContentToolCallID()
	}
	args = sanitizeContentToolCallArguments(name, args)
	return ToolCall{
		ID:   id,
		Type: normalizeToolCallType(callType),
		Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{
			Name:      name,
			Arguments: args,
		},
	}
}

// sanitizeContentToolCallArguments keeps content-emitted web_search payloads
// executable. DeepSeek DSML (and similar XML) often includes count/max_results
// or a destination hint. The host schema is query-only with
// additionalProperties=false; leftover fields consume the one-shot grant and
// never unlock generate_pdf. Structured OpenAI tool_calls are not rewritten
// here — that path still fail-closes on forged extras.
func sanitizeContentToolCallArguments(name, args string) string {
	if !strings.EqualFold(strings.TrimSpace(name), "web_search") {
		return args
	}
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(args), &obj); err != nil || obj == nil {
		return args
	}
	query, _ := obj["query"].(string)
	query = strings.TrimSpace(query)
	if query == "" {
		return args
	}
	if len(obj) == 1 {
		return args
	}
	out, err := json.Marshal(map[string]string{"query": query})
	if err != nil {
		return args
	}
	return string(out)
}

func normalizeToolCallType(callType string) string {
	callType = strings.TrimSpace(callType)
	if callType == "" {
		return "function"
	}
	return callType
}

// NormalizeToolCallsForConversation fills OpenAI-required tool_call fields
// that compatible providers sometimes omit in responses.
func NormalizeToolCallsForConversation(calls []ToolCall) []ToolCall {
	if len(calls) == 0 {
		return calls
	}
	out := make([]ToolCall, len(calls))
	for i, call := range calls {
		out[i] = call
		out[i].ID = strings.TrimSpace(out[i].ID)
		if out[i].ID == "" {
			out[i].ID = randomContentToolCallID()
		}
		out[i].Type = normalizeToolCallType(out[i].Type)
	}
	return out
}

func randomContentToolCallID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return fmt.Sprintf("call_%x", b)
}
