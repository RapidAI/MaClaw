package llm

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"regexp"
	"strconv"
	"strings"
)

var (
	contentXMLToolCallBlockRe     = regexp.MustCompile(`(?is)<tool_call(?:\[\])?\b[^>]*>\s*(.*?)\s*</tool_call>`)
	contentAngleToolCallOpenRe    = regexp.MustCompile(`(?is)<tool_call(?:\[\])?\b[^>]*>`)
	contentXMLToolCallOpenToEndRe = regexp.MustCompile(`(?is)<tool_call(?:\[\])?\b[^>]*>.*\z`)
	contentCodexToolCallBlockRe   = regexp.MustCompile(`(?s)<turn:\s*tool_call\s*>(.*?)</turn>`)
	contentCodexToolCallMarkerRe  = regexp.MustCompile(`(?is)<turn:\s*tool_call\b`)
	contentPlainToolCallMarkerRe  = regexp.MustCompile(`(?is)\bTOOL_CALL\b\s*`)
	contentCodexToolInvokeRe      = regexp.MustCompile(`(?s)<invoke\b([^>]*)>(.*?)</invoke>`)
	contentCodexToolParameterRe   = regexp.MustCompile(`(?s)<parameter\b([^>]*)>(.*?)</parameter>`)
	contentCodexToolAttributeRe   = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_:-]*)\s*=\s*"([^"]*)"`)
	contentFunctionEqBlockRe      = regexp.MustCompile(`(?is)<function=([A-Za-z0-9_.-]+)>(.*?)</function>`)
	contentFunctionEqOpenRe       = regexp.MustCompile(`(?is)<function=([A-Za-z0-9_.-]+)>`)
	contentGLMArgPairRe           = regexp.MustCompile(`(?is)<arg_key>\s*(.*?)\s*</arg_key>\s*<arg_value>(.*?)</arg_value>`)
	contentLongcatArgPairRe       = regexp.MustCompile(`(?is)<longcat_arg_key>\s*(.*?)\s*</longcat_arg_key>\s*<longcat_arg_value>(.*?)</longcat_arg_value>`)
	longcatJSONNumberRe           = regexp.MustCompile(`^-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?$`)
	contentQwenParamEqRe          = regexp.MustCompile(`(?is)<parameter=([^>]+)>(.*?)</parameter>`)
	contentLeadingToolNameRe      = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_.-]*)`)
	contentSpecialTokenRe         = regexp.MustCompile(`<\|[^|<>\n]+\|>`)
	// The DSML token is ｜DSML｜. A decoder that also writes the bar literally
	// produces ｜｜DSML｜｜. A fence is rewritten only when a real tag follows,
	// so "< | DSML | note" stays prose. tool_calls is listed before calls so
	// the short block name cannot steal the tail of tool_calls.
	dsmlTagFenceRe          = regexp.MustCompile(`(?i)(</?)\s*(?:` + dsmlBar + `\s*)+DSML(?:\s*` + dsmlBar + `)+\s*(/)?\s*(` + dsmlTagTail + `)`)
	dsmlFenceRe             = regexp.MustCompile(`(?i)(?:\|\s*)+DSML(?:\s*\|)+`)
	dsmlCollapsePipeSpaceRe = regexp.MustCompile(`\s*\|\s*`)
	dsmlCollapseOpenSpaceRe = regexp.MustCompile(`<\s+`)
	dsmlCollapsedTagSpaceRe = regexp.MustCompile(`^<\|dsml\|\s+`)
	dsmlParameterOpenRe     = regexp.MustCompile(`(?i)<\|DSML\|parameter`)
	dsmlParameterCloseRe    = regexp.MustCompile(`(?i)</\|DSML\|parameter>`)
	contentDSMLBlockRe      = regexp.MustCompile(`(?is)<\|DSML\|(?:` + dsmlBlockTags + `)\s*>(.*?)</\|DSML\|(?:` + dsmlBlockTags + `)>`)
	contentDSMLBlockOpenRe  = regexp.MustCompile(`(?is)<\|DSML\|(?:` + dsmlTagTail + `)`)
	contentDSMLInvokeRe     = regexp.MustCompile(`(?is)<\|DSML\|invoke\b([^>]*)>(.*?)</\|DSML\|invoke>`)
	contentDSMLInvokeOpenRe = regexp.MustCompile(`(?is)<\|DSML\|invoke(?:\s+name\s*=|\s*>)`)
	contentDSMLMarkerRe     = regexp.MustCompile(`(?i)<\s*(?:[|\x{FF5C}]\s*)+DSML(?:\s*[|\x{FF5C}])+\s*(?:` + dsmlTagTail + `)`)
)

const (
	// dsmlBlockTags are the wrappers around invoke elements. calls is the
	// block tag left when this decoder writes the fence and drops "tool_".
	dsmlBlockTags = "tool_calls|function_calls|calls"
	// invoke and parameter are tags only with an attribute or a close.
	// "parameter is a field" is a sentence. Block tags close with '>'.
	dsmlTagTail = `(?:(?:invoke|parameter)(?:\s+name\s*=|\s*>)|(?:` + dsmlBlockTags + `)\s*>)`
	dsmlBar     = `[|\x{FF5C}]`
)

var dsmlCollapsedMarkers = []string{
	"<|dsml|tool_calls",
	"<|dsml|function_calls",
	"<|dsml|invoke",
	"<|dsml|parameter",
	"<|dsml|calls",
}

const MalformedContentToolCallErrorMsg = "模型返回了无法解析的工具调用，已拦截原始工具 XML。请重试，或切换更兼容 OpenAI tool_calls 的模型。"

// ParseContentToolCallsDetailed extracts tool calls emitted in assistant
// content by OpenAI-compatible providers that fail to populate tool_calls.
func ParseContentToolCallsDetailed(content string) ([]ToolCall, bool) {
	if looksLikeDSMLContent(content) {
		if calls, malformed := parseDSMLContentToolCalls(content); len(calls) > 0 || malformed {
			return calls, malformed
		}
	}
	// LongCat writes <longcat_tool_call>, which <tool_call> does not match.
	// A tag that appears only inside an earlier <tool_call> body is file
	// text, not a second call.
	if idx := indexASCIIFold(content, "<longcat_tool_call"); idx >= 0 && !earlierStructuredToolCallMarker(content, idx) {
		calls, malformed := parseLongcatContentToolCalls(content)
		if malformed && len(calls) == 0 {
			logMalformedContentToolCall(content)
		}
		return calls, malformed
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
	// Some models emit the call as a markdown-wrapped function line
	// (`*web_search(query="...")*`) instead of tool_calls. That line is the
	// call, not an answer.
	parenCalls, parenMalformed := parseParenthesizedContentToolCalls(content)
	if len(parenCalls) > 0 {
		calls = append(calls, parenCalls...)
	}
	if parenMalformed {
		malformed = true
	}
	// Some providers emit a line-oriented call that the legacy plain parser
	// partially recognizes (typically dropping later arguments). Prefer the
	// structured line parse whenever it succeeds so tool arguments are not lost.
	lineCalls, lineMalformed := parseLineOrientedContentToolCalls(content)
	if len(lineCalls) > 0 {
		calls = lineCalls
		malformed = false
	} else if lineMalformed {
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
	if args, ok := parseLineOrientedToolCallArguments(body); ok {
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

// parseLongcatContentToolCalls reads LongCat's native tool XML.
// Thinking models emit a function name plus <longcat_arg_key> pairs.
// LongCat-Flash emits one JSON object in the same tag. Each open tag starts
// a call, so a missing close cannot swallow the next call.
func parseLongcatContentToolCalls(content string) ([]ToolCall, bool) {
	var calls []ToolCall
	malformed := false
	saw := false
	const openTag = "<longcat_tool_call>"
	const closeTag = "</longcat_tool_call>"
	for {
		rel := indexASCIIFold(content, "<longcat_tool_call")
		if rel < 0 {
			break
		}
		saw = true
		content = content[rel:]
		if !asciiHasPrefix(content, openTag) {
			malformed = true
			break
		}
		content = content[len(openTag):]
		closeRel := indexASCIIFold(content, closeTag)
		nextRel := indexASCIIFold(content, "<longcat_tool_call")
		end := closeRel
		closed := closeRel >= 0
		if nextRel >= 0 && (!closed || nextRel < closeRel) {
			end = nextRel
			closed = false
		}
		// end == 0 means the next open is already the current head. Parsing
		// that empty span and slicing by zero would spin.
		if end == 0 {
			malformed = true
			continue
		}
		body := content
		if end >= 0 {
			body = content[:end]
			if closed {
				content = content[end+len(closeTag):]
			} else {
				content = content[end:]
			}
		} else {
			content = ""
		}
		call, ok := parseLongcatToolCallBody(body)
		if ok {
			calls = append(calls, call)
		} else {
			malformed = true
		}
		if end < 0 {
			break
		}
	}
	if !saw {
		return nil, false
	}
	return calls, malformed
}

func earlierStructuredToolCallMarker(content string, before int) bool {
	if before <= 0 {
		return false
	}
	head := content[:before]
	for _, marker := range []string{"<tool_call", "<turn: tool_call", "<function="} {
		if indexASCIIFold(head, marker) >= 0 {
			return true
		}
	}
	return false
}

func parseLongcatToolCallBody(body string) (ToolCall, bool) {
	body = strings.TrimSpace(body)
	if body == "" {
		return ToolCall{}, false
	}
	if strings.HasPrefix(body, "{") {
		if call, ok := parseContentJSONToolCallPayload(body); ok {
			return call, true
		}
	}
	name, rest, ok := splitLeadingToolName(body)
	if !ok && contentLeadingToolNameRe.FindString(body) == body {
		name = body
		rest = ""
		ok = true
	}
	if !ok || name == "" {
		return ToolCall{}, false
	}
	if rest == "" {
		return normalizePlainContentToolCall(name, json.RawMessage(`{}`))
	}
	args, ok := parseLongcatArgPairs(rest)
	if !ok {
		return ToolCall{}, false
	}
	return normalizePlainContentToolCall(name, args)
}

func parseLongcatArgPairs(body string) (json.RawMessage, bool) {
	if indexASCIIFold(body, "<longcat_tool_call") >= 0 {
		return nil, false
	}
	locs := contentLongcatArgPairRe.FindAllStringSubmatchIndex(body, -1)
	if len(locs) == 0 {
		if strings.TrimSpace(body) == "" {
			return json.RawMessage(`{}`), true
		}
		return nil, false
	}
	args := make(map[string]interface{}, len(locs))
	cursor := 0
	for _, loc := range locs {
		if len(loc) < 6 || strings.TrimSpace(body[cursor:loc[0]]) != "" {
			return nil, false
		}
		key := strings.TrimSpace(html.UnescapeString(body[loc[2]:loc[3]]))
		if key == "" {
			return nil, false
		}
		if _, exists := args[key]; exists {
			return nil, false
		}
		args[key] = coerceLongcatArgValue(key, body[loc[4]:loc[5]])
		cursor = loc[1]
	}
	if strings.TrimSpace(body[cursor:]) != "" {
		return nil, false
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return nil, false
	}
	return raw, true
}

// coerceLongcatArgValue numbers only known integer keys. Every other key stays
// a string, so an id, a path, or a one-line JSON document is not dropped by a
// string-only argument reader. Readers that accept numeric strings still see
// a number for the keys listed here; local file tools accept only numbers.
func coerceLongcatArgValue(key, raw string) interface{} {
	value := strings.TrimSpace(html.UnescapeString(raw))
	if value == "" || strings.ContainsAny(value, "\r\n") || !longcatArgKeyIsNumber(key) {
		return value
	}
	if longcatJSONNumberRe.MatchString(value) {
		return json.Number(value)
	}
	return value
}

func longcatArgKeyIsNumber(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "offset", "limit", "lines", "num_lines", "line_count",
		"start", "start_line", "startline", "end", "end_line",
		"tail", "tail_lines", "max_results", "timeout", "timeout_seconds",
		"max_chars", "max_affected_rows", "count", "port",
		"duration_ms", "delta_x", "delta_y",
		"context", "before_context", "after_context",
		"width", "max_slides", "slide_offset", "max_rows",
		"x", "y",
		"wait_seconds", "token_budget", "max_turns",
		"stale_after_days", "max_actions", "min_failure_runs",
		"max_items", "max_file_bytes", "max_file_mb":
		return true
	default:
		return false
	}
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
	case strings.Contains(lower, "<longcat_tool_call"):
		kind = "longcat"
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
	case looksLikeUnparsedLeakedToolCall(content):
		kind = "line_oriented"
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
	// A narrated lookup line is held, not treated as the end of the message.
	// Text that arrives after it must still reach the chat. Flush hides the
	// line only when it parsed as a call; an unfinished line stays visible.
	if idx := parenContentToolCallIndex(s); idx >= 0 {
		if idx > 0 {
			visible = s[:idx]
		}
		if !force {
			return visible, s[idx:], false
		}
		if calls, _ := parseParenthesizedContentToolCalls(s); len(calls) > 0 {
			return visible, "", false
		}
		return s, "", false
	}
	partial := contentToolCallMarkerSuffixLen(s)
	if n := leakedLineOrientedIncompleteLineHoldLen(s); n > partial {
		partial = n
	}
	if n := parenToolCallIncompleteLineHoldLen(s); n > partial {
		partial = n
	}
	if partial <= 0 {
		return s, "", false
	}
	visible = s[:len(s)-partial]
	suffix := s[len(s)-partial:]
	if !force {
		return visible, suffix, false
	}
	if parenToolCallSuffixIsUnconfirmed(suffix) {
		// The trailing span never became a call. Show it.
		return s, "", false
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
	trimmed := strings.TrimLeft(suffix, " \t\r\n")
	for _, marker := range []string{"<longcat_tool_call", "<tool_call", "<turn: tool_call", "<function="} {
		if asciiHasPrefix(trimmed, marker) {
			return true
		}
	}
	return couldBecomeLeakedLineOrientedToolLine(suffix)
}

func leakedLineOrientedIncompleteLineHoldLen(s string) int {
	idx := strings.LastIndexByte(s, '\n')
	last := s
	if idx >= 0 {
		last = s[idx+1:]
	}
	if last == "" || !couldBecomeLeakedLineOrientedToolLine(last) {
		return 0
	}
	return len(last)
}

func couldBecomeLeakedLineOrientedToolLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	if name := lineOrientedToolName(trimmed); name != "" && leakedToolNameShouldSuppressStream(name) {
		return true
	}
	fields := strings.Fields(trimmed)
	switch len(fields) {
	case 1:
		return couldBeLeakedToolNamePrefix(fields[0])
	case 2:
		return looksLikeJunkToolCallPrefix(fields[0]) && strings.HasPrefix(strings.ToLower(fields[1]), "glob")
	default:
		return false
	}
}

func couldBeLeakedToolNamePrefix(ident string) bool {
	if contentLeadingToolNameRe.FindString(ident) != ident {
		return false
	}
	lower := strings.ToLower(ident)
	if lower == "glob" || lower == "tool_call" || strings.HasPrefix(lower, "tool_call") {
		return false
	}
	if strings.HasPrefix(lower, "glob_") {
		return true
	}
	const leaked = "glob_file_search_tool"
	return strings.HasPrefix(leaked, lower) && len(lower) >= 5
}

// contentToolCallASCIIMarkers are matched without copying a lowercased buffer.
// Indexes stay in the original string when a rune's lowercase form is longer.
var contentToolCallASCIIMarkers = []string{
	"<longcat_tool_call",
	"<tool_call",
	"<turn: tool_call",
	"<function=",
	"tool_call\n",
	"tool_call\r\n",
	"tool_call {",
}

func firstContentToolCallMarkerIndex(s string) int {
	best := -1
	for _, marker := range contentToolCallASCIIMarkers {
		if idx := indexASCIIFold(s, marker); idx >= 0 && (best < 0 || idx < best) {
			best = idx
		}
	}
	if loc := contentDSMLMarkerRe.FindStringIndex(s); loc != nil && (best < 0 || loc[0] < best) {
		best = loc[0]
	}
	if idx := leakedLineOrientedToolMarkerIndex(s); idx >= 0 && (best < 0 || idx < best) {
		best = idx
	}
	return best
}

func contentToolCallMarkerSuffixLen(s string) int {
	best := 0
	for _, marker := range contentToolCallASCIIMarkers {
		max := len(marker) - 1
		if len(s) < max {
			max = len(s)
		}
		for i := max; i > best; i-- {
			if asciiFoldEqual(s[len(s)-i:], marker[:i]) {
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
	if !hasDSMLWord(s) {
		return false
	}
	// A fence with no real tag is prose. The marker is the same tag tail the
	// rewriter accepts, so a second collapse of the whole message cannot
	// discover a call the marker missed.
	return contentDSMLMarkerRe.MatchString(s)
}

func hasDSMLWord(s string) bool {
	for i := 0; i+4 <= len(s); i++ {
		if (s[i] == 'd' || s[i] == 'D') &&
			(s[i+1] == 's' || s[i+1] == 'S') &&
			(s[i+2] == 'm' || s[i+2] == 'M') &&
			(s[i+3] == 'l' || s[i+3] == 'L') {
			return true
		}
	}
	return false
}

func collapseDSMLFence(s string) string {
	s = strings.ToLower(strings.ReplaceAll(s, "\uFF5C", "|"))
	s = dsmlCollapseOpenSpaceRe.ReplaceAllString(s, "<")
	s = dsmlCollapsePipeSpaceRe.ReplaceAllString(s, "|")
	s = dsmlFenceRe.ReplaceAllString(s, "|dsml|")
	return collapseOpenDSMLBars(s)
}

// collapseOpenDSMLBars folds a repeated bar run after '<' into one bar when
// that run is the fence prefix. '<||ds' is '<|ds'. '<||b' is left alone.
func collapseOpenDSMLBars(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] != '<' {
			b.WriteByte(s[i])
			i++
			continue
		}
		j := i + 1
		for j < len(s) && s[j] == '|' {
			j++
		}
		if j >= i+3 && (j == len(s) || s[j] == 'd') {
			b.WriteString("<|")
			i = j
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func dsmlOpenSuffixLen(s string) int {
	idx := strings.LastIndex(s, "<")
	if idx < 0 {
		return 0
	}
	tail := s[idx:]
	if !dsmlTailMightBeFence(tail) {
		return 0
	}
	if contentDSMLMarkerRe.MatchString(tail) {
		return 0
	}
	collapsed := dsmlCollapsedTagSpaceRe.ReplaceAllString(collapseDSMLFence(tail), "<|dsml|")
	for _, marker := range dsmlCollapsedMarkers {
		if strings.HasPrefix(marker, collapsed) {
			return len(s) - idx
		}
	}
	return 0
}

// dsmlTailMightBeFence reports whether a '<' tail can still grow into the
// DSML token. A '<div' or '<html' tail is not collapsed on every streamed byte.
func dsmlTailMightBeFence(tail string) bool {
	if len(tail) == 0 || tail[0] != '<' {
		return false
	}
	i := 1
	for n := 0; i < len(tail) && n < 16; n++ {
		switch tail[i] {
		case ' ', '\t', '\n', '\r':
			i++
			continue
		}
		break
	}
	if i >= len(tail) {
		return true
	}
	return tail[i] == '|' || strings.HasPrefix(tail[i:], "\uFF5C")
}

func normalizeDSMLMarkup(s string) string {
	if !hasDSMLWord(s) {
		return s
	}
	locs := dsmlTagFenceRe.FindAllStringSubmatchIndex(s, -1)
	if len(locs) == 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	prev := 0
	for _, loc := range locs {
		if len(loc) < 8 || loc[6] < 0 {
			continue
		}
		b.WriteString(s[prev:loc[0]])
		open := s[loc[2]:loc[3]]
		if loc[4] >= 0 && s[loc[4]:loc[5]] == "/" {
			open = "</"
		}
		b.WriteString(open)
		b.WriteString("|DSML|")
		b.WriteString(canonicalizeDSMLTagTail(s[loc[6]:loc[7]]))
		prev = loc[1]
	}
	b.WriteString(s[prev:])
	return b.String()
}

// canonicalizeDSMLTagTail drops whitespace the detector allowed before '>'.
// The close patterns match parameter> and invoke>, so "calls >" has to
// become "calls>" or the block is reported malformed and dropped.
func canonicalizeDSMLTagTail(tail string) string {
	i := strings.LastIndexByte(tail, '>')
	if i < 0 {
		return tail
	}
	return strings.TrimRight(tail[:i], " \t\r\n") + tail[i:]
}

// parenContentToolNames is the lookup set models narrate as a markdown-wrapped
// function line. File and shell names stay out: a code sample must not become
// an execution.
var parenContentToolNames = []string{"web_search", "web_fetch", "current_datetime"}

func parseParenthesizedContentToolCalls(content string) ([]ToolCall, bool) {
	// A wrapped lookup line is a call only when the rest of the message is a
	// short lead-in. The same line inside a real answer stays text.
	if !parenNarrationIsTheAnswer(content) {
		return nil, false
	}
	var calls []ToolCall
	for _, line := range strings.Split(content, "\n") {
		name, argsBody, ok, partial := splitParenToolCallLine(line)
		if !ok || partial {
			continue
		}
		raw, parsed := parseParenToolArgs(argsBody)
		if !parsed {
			continue
		}
		call, ok := normalizePlainContentToolCall(name, raw)
		if !ok {
			continue
		}
		calls = append(calls, call)
	}
	// An unfinished line stays text. Marking it malformed would replace the
	// reply with the tool-XML error.
	return calls, false
}

func parenNarrationIsTheAnswer(content string) bool {
	var other int
	for _, line := range strings.Split(content, "\n") {
		if _, _, ok, partial := splitParenToolCallLine(line); ok || partial {
			continue
		}
		other += len([]rune(strings.TrimSpace(line)))
	}
	return other <= 80
}

func splitParenToolCallLine(line string) (name, args string, ok, incomplete bool) {
	raw := strings.TrimSpace(line)
	if !strings.HasPrefix(raw, "*") && !strings.HasPrefix(raw, "_") && !strings.HasPrefix(raw, "`") {
		return "", "", false, false
	}
	trimmed := trimWrappingMarkdown(raw)
	open := strings.IndexByte(trimmed, '(')
	if open <= 0 {
		return "", "", false, false
	}
	name, ok = canonicalParenContentToolName(trimmed[:open])
	if !ok {
		return "", "", false, false
	}
	close := parenCallClose(trimmed, open)
	if close < 0 {
		return "", "", false, true
	}
	if strings.TrimSpace(trimmed[close+1:]) != "" {
		return "", "", false, false
	}
	return name, trimmed[open+1 : close], true, false
}

func trimWrappingMarkdown(s string) string {
	s = strings.TrimSpace(s)
	for len(s) >= 2 {
		a, b := s[0], s[len(s)-1]
		if (a == '*' || a == '_' || a == '`') && a == b {
			s = strings.TrimSpace(s[1 : len(s)-1])
			continue
		}
		break
	}
	return s
}

func canonicalParenContentToolName(name string) (string, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, known := range parenContentToolNames {
		if name == known {
			return known, true
		}
	}
	return "", false
}

func parseParenToolArgs(body string) (json.RawMessage, bool) {
	body = strings.TrimSpace(body)
	if body == "" {
		return json.RawMessage("{}"), true
	}
	args := map[string]interface{}{}
	rest := body
	for strings.TrimSpace(rest) != "" {
		rest = strings.TrimSpace(rest)
		eq := strings.IndexByte(rest, '=')
		if eq <= 0 {
			return nil, false
		}
		key := strings.TrimSpace(rest[:eq])
		if contentLeadingToolNameRe.FindString(key) != key {
			return nil, false
		}
		rest = strings.TrimSpace(rest[eq+1:])
		if rest == "" {
			return nil, false
		}
		var val string
		if rest[0] == '"' || rest[0] == '\'' {
			quote := rest[0]
			var b strings.Builder
			i := 1
			closed := false
			for i < len(rest) {
				if rest[i] == '\\' && i+1 < len(rest) {
					b.WriteByte(rest[i+1])
					i += 2
					continue
				}
				if rest[i] == quote {
					closed = true
					i++
					break
				}
				b.WriteByte(rest[i])
				i++
			}
			if !closed {
				return nil, false
			}
			val = b.String()
			rest = strings.TrimSpace(rest[i:])
		} else {
			end := strings.IndexByte(rest, ',')
			if end < 0 {
				val = strings.TrimSpace(rest)
				rest = ""
			} else {
				val = strings.TrimSpace(rest[:end])
				rest = rest[end:]
			}
			if val == "" {
				return nil, false
			}
		}
		if isNumericToolArgKey(key) {
			if n, err := strconv.Atoi(val); err == nil {
				args[key] = n
			} else {
				args[key] = val
			}
		} else {
			args[key] = val
		}
		if strings.HasPrefix(rest, ",") {
			rest = strings.TrimSpace(rest[1:])
		}
	}
	if len(args) == 0 {
		return nil, false
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return nil, false
	}
	return raw, true
}

func parenCallClose(s string, open int) int {
	inQuote := byte(0)
	for i := open + 1; i < len(s); i++ {
		c := s[i]
		if inQuote != 0 {
			if c == '\\' && i+1 < len(s) {
				i++
				continue
			}
			if c == inQuote {
				inQuote = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			inQuote = c
			continue
		}
		if c == ')' {
			return i
		}
	}
	return -1
}

func parenContentToolCallIndex(s string) int {
	if !parenNarrationIsTheAnswer(s) {
		return -1
	}
	best := -1
	for _, name := range parenContentToolNames {
		needle := name + "("
		from := 0
		for from < len(s) {
			rel := indexASCIIFold(s[from:], needle)
			if rel < 0 {
				break
			}
			abs := from + rel
			if parenCallWrappedAtLineStart(s, abs) && (best < 0 || abs < best) {
				best = parenCallMarkdownStart(s, abs)
			}
			from = abs + len(needle)
		}
	}
	return best
}

// indexASCIIFold finds needle in s without case-folding the whole string.
// strings.ToLower can change byte length, which would slice the stream at the
// wrong offset.
func indexASCIIFold(s, needle string) int {
	n := len(needle)
	if n == 0 || len(s) < n {
		return -1
	}
	first := needle[0]
	alt := first
	if first >= 'a' && first <= 'z' {
		alt = first - ('a' - 'A')
	}
	for i := 0; i+n <= len(s); {
		rest := s[i:]
		j := strings.IndexByte(rest, first)
		if alt != first {
			if k := strings.IndexByte(rest, alt); k >= 0 && (j < 0 || k < j) {
				j = k
			}
		}
		if j < 0 {
			return -1
		}
		i += j
		if i+n > len(s) {
			return -1
		}
		if asciiFoldEqual(s[i:i+n], needle) {
			return i
		}
		i++
	}
	return -1
}

func asciiFoldEqual(s, lowerNeedle string) bool {
	for i := 0; i < len(lowerNeedle); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != lowerNeedle[i] {
			return false
		}
	}
	return true
}

func asciiHasPrefix(s, lowerPrefix string) bool {
	return len(s) >= len(lowerPrefix) && asciiFoldEqual(s[:len(lowerPrefix)], lowerPrefix)
}

func parenCallWrappedAtLineStart(s string, abs int) bool {
	i := abs
	for i > 0 && (s[i-1] == ' ' || s[i-1] == '\t') {
		i--
	}
	if i == 0 {
		return false
	}
	mark := s[i-1]
	if mark != '*' && mark != '_' && mark != '`' {
		return false
	}
	for i > 0 {
		c := s[i-1]
		if c == mark || c == ' ' || c == '\t' {
			i--
			continue
		}
		return c == '\n' || c == '\r'
	}
	return true
}

func parenCallMarkdownStart(s string, abs int) int {
	for abs > 0 && (s[abs-1] == ' ' || s[abs-1] == '\t') {
		abs--
	}
	for abs > 0 {
		c := s[abs-1]
		if c != '*' && c != '_' && c != '`' {
			break
		}
		abs--
	}
	return abs
}

func parenToolCallIncompleteLineHoldLen(s string) int {
	idx := strings.LastIndexByte(s, '\n')
	last := s
	if idx >= 0 {
		last = s[idx+1:]
	}
	if last == "" || !couldBecomeParenToolCallLine(last) {
		return 0
	}
	return len(last)
}

func couldBecomeParenToolCallLine(line string) bool {
	raw := strings.TrimSpace(line)
	if raw == "" || (!strings.HasPrefix(raw, "*") && !strings.HasPrefix(raw, "_") && !strings.HasPrefix(raw, "`")) {
		return false
	}
	if _, _, ok, incomplete := splitParenToolCallLine(raw); ok || incomplete {
		return !ok
	}
	// "*web_search*" is closed emphasis. Only an unclosed span can still grow
	// into web_search(...).
	if closedParenMarkdownSpan(raw) {
		return false
	}
	inner := strings.TrimLeft(raw, "*_`")
	inner = strings.TrimSpace(inner)
	if inner == "" || strings.ContainsAny(inner, " \t()\"'") {
		return false
	}
	lower := strings.ToLower(inner)
	for _, known := range parenContentToolNames {
		if strings.HasPrefix(known, lower) && len(lower) >= 4 {
			return true
		}
	}
	return false
}

func parenToolCallSuffixIsUnconfirmed(suffix string) bool {
	idx := strings.LastIndexByte(suffix, '\n')
	last := suffix
	if idx >= 0 {
		last = suffix[idx+1:]
	}
	return couldBecomeParenToolCallLine(last)
}

func closedParenMarkdownSpan(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return false
	}
	mark := s[0]
	if (mark != '*' && mark != '_' && mark != '`') || s[len(s)-1] != mark {
		return false
	}
	return trimWrappingMarkdown(s) != s
}

func parseLineOrientedContentToolCalls(content string) ([]ToolCall, bool) {
	_, name, rest := findLineOrientedToolCall(content)
	if name == "" {
		return nil, looksLikeUnparsedLeakedToolCall(content) || leakedLineOrientedToolMarkerIndex(content) >= 0
	}
	args, ok := parseLineOrientedToolCallArguments(rest)
	if !ok {
		return nil, true
	}
	call, ok := normalizePlainContentToolCall(name, args)
	if !ok {
		return nil, true
	}
	return []ToolCall{call}, false
}

func findLineOrientedToolCall(content string) (int, string, string) {
	parts := strings.Split(content, "\n")
	offset := 0
	foundAt, foundName, foundRest := -1, "", ""
	for i, line := range parts {
		name := lineOrientedToolName(strings.TrimSpace(line))
		if name != "" {
			rest := strings.Join(parts[i+1:], "\n")
			if _, ok := parseLineOrientedToolCallArguments(rest); ok {
				foundAt, foundName, foundRest = offset, name, rest
			}
		}
		offset += len(line)
		if i < len(parts)-1 {
			offset++
		}
	}
	return foundAt, foundName, foundRest
}

func parseLineOrientedToolCallArguments(body string) (json.RawMessage, bool) {
	body = stripContentSpecialTokens(strings.TrimSpace(body))
	if body == "" {
		return nil, false
	}
	lines := strings.Split(body, "\n")
	args := map[string]interface{}{}
	for i := 0; i < len(lines); i++ {
		line := stripContentSpecialTokens(strings.TrimSpace(lines[i]))
		if line == "" {
			continue
		}
		key, inlineVal, hasInline := splitLineOrientedArgKey(line)
		if !looksLikeLineOrientedArgKey(key) {
			if lineOrientedArgsLookExecutable(args) {
				break
			}
			return nil, false
		}
		val := inlineVal
		if !hasInline {
			i++
			for i < len(lines) {
				val = stripContentSpecialTokens(strings.TrimSpace(lines[i]))
				if val != "" {
					break
				}
				i++
			}
		}
		if val == "" {
			if lineOrientedArgsLookExecutable(args) {
				break
			}
			return nil, false
		}
		if isNumericToolArgKey(key) {
			if n, err := strconv.Atoi(val); err == nil {
				args[key] = n
				continue
			}
		}
		args[key] = val
	}
	if len(args) == 0 || !lineOrientedArgsLookExecutable(args) {
		return nil, false
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return nil, false
	}
	return raw, true
}

func isNumericToolArgKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "max_results", "max_chars", "offset", "timeout", "count":
		return true
	default:
		return false
	}
}

func splitLineOrientedArgKey(line string) (key, value string, hasValue bool) {
	if idx := strings.IndexByte(line, ':'); idx > 0 {
		key = strings.TrimSpace(line[:idx])
		value = stripContentSpecialTokens(strings.TrimSpace(line[idx+1:]))
		if looksLikeLineOrientedArgKey(key) && value != "" {
			return key, value, true
		}
	}
	return strings.TrimSuffix(line, ":"), "", false
}

func looksLikeLineOrientedArgKey(key string) bool {
	key = strings.TrimSpace(strings.TrimSuffix(key, ":"))
	if key == "" || strings.ContainsAny(key, " \t\\/") {
		return false
	}
	if contentLeadingToolNameRe.FindString(key) != key {
		return false
	}
	switch strings.ToLower(key) {
	case "path", "file_path", "glob_pattern", "pattern", "glob", "query", "command",
		"old_string", "new_string", "content", "working_dir", "timeout":
		return true
	}
	return strings.Contains(key, "_") && len(key) <= 40
}

func lineOrientedArgsLookExecutable(args map[string]interface{}) bool {
	for _, key := range []string{"path", "file_path", "glob_pattern", "pattern", "glob", "command", "query"} {
		if _, ok := args[key]; ok {
			return true
		}
	}
	return false
}

func lineOrientedToolName(line string) string {
	line = stripContentSpecialTokens(strings.TrimSpace(line))
	if line == "" {
		return ""
	}
	fields := strings.Fields(line)
	switch len(fields) {
	case 1:
		if looksLikeLineOrientedToolName(fields[0]) {
			return fields[0]
		}
	case 2:
		if !looksLikeLineOrientedToolName(fields[1]) {
			return ""
		}
		if looksLikeJunkToolCallPrefix(fields[0]) || strings.HasSuffix(strings.ToLower(fields[1]), "_tool") {
			return fields[1]
		}
	}
	return ""
}

func looksLikeLineOrientedToolName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || contentLeadingToolNameRe.FindString(name) != name {
		return false
	}
	lower := strings.ToLower(name)
	if lower == "tool_call" {
		return false
	}
	if strings.HasSuffix(lower, "_tool") {
		return len(name) >= 6
	}
	switch lower {
	case "glob", "glob_file_search", "search_files", "list_directory", "read_file", "write_file", "edit_file", "ripgrep", "grep_search", "web_search", "web_fetch":
		return true
	}
	return false
}

func looksLikeJunkToolCallPrefix(s string) bool {
	if len(s) < 2 || len(s) > 12 || strings.Contains(s, "_") {
		return false
	}
	hasDigit := false
	for _, r := range s {
		if r >= '0' && r <= '9' {
			hasDigit = true
			continue
		}
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return false
		}
	}
	return hasDigit
}

func stripContentSpecialTokens(s string) string {
	return strings.TrimSpace(contentSpecialTokenRe.ReplaceAllString(s, ""))
}

func looksLikeUnparsedLeakedToolCall(content string) bool {
	if !contentSpecialTokenRe.MatchString(content) {
		return false
	}
	for _, line := range strings.Split(content, "\n") {
		if lineOrientedToolName(strings.TrimSpace(line)) != "" {
			return true
		}
	}
	return false
}

func leakedLineOrientedToolMarkerIndex(s string) int {
	parts := strings.Split(s, "\n")
	offset := 0
	for i, line := range parts {
		name := lineOrientedToolName(strings.TrimSpace(line))
		if name != "" && leakedToolNameShouldSuppressStream(name) {
			return offset
		}
		offset += len(line)
		if i < len(parts)-1 {
			offset++
		}
	}
	return -1
}

func leakedToolNameShouldSuppressStream(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	return strings.HasSuffix(lower, "_tool") || lower == "glob_file_search" || lower == "web_search" || lower == "web_fetch"
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
