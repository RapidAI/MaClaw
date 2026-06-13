package llm

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strings"
)

var (
	contentXMLToolCallBlockRe        = regexp.MustCompile(`(?is)<tool_call(?:\[\])?\b[^>]*>\s*(.*?)\s*</tool_call>`)
	contentAngleToolCallOpenRe       = regexp.MustCompile(`(?is)<tool_call(?:\[\])?\b[^>]*>`)
	contentCodexToolCallBlockRe      = regexp.MustCompile(`(?s)<turn:\s*tool_call\s*>(.*?)</turn>`)
	contentCodexToolCallMarkerRe     = regexp.MustCompile(`(?is)<turn:\s*tool_call\b`)
	contentPlainToolCallMarkerRe     = regexp.MustCompile(`(?is)\bTOOL_CALL\b\s*`)
	contentCodexToolInvokeRe         = regexp.MustCompile(`(?s)<invoke\b([^>]*)>(.*?)</invoke>`)
	contentCodexToolParameterRe      = regexp.MustCompile(`(?s)<parameter\b([^>]*)>(.*?)</parameter>`)
	contentCodexToolAttributeRe      = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_:-]*)\s*=\s*"([^"]*)"`)
	MalformedContentToolCallErrorMsg = "模型返回了无法解析的工具调用，已拦截原始工具 XML。请重试，或切换更兼容 OpenAI tool_calls 的模型。"
)

// ParseContentToolCallsDetailed extracts tool calls emitted in assistant
// content by OpenAI-compatible providers that fail to populate tool_calls.
func ParseContentToolCallsDetailed(content string) ([]ToolCall, bool) {
	matches := contentXMLToolCallBlockRe.FindAllStringSubmatch(content, -1)
	var calls []ToolCall
	malformed := false
	for _, m := range matches {
		if len(m) < 2 {
			malformed = true
			continue
		}
		call, ok := parseContentJSONToolCallPayload(strings.TrimSpace(m[1]))
		if ok {
			calls = append(calls, call)
		} else {
			malformed = true
		}
	}
	codexCalls, codexMalformed := parseCodexContentToolCalls(content)
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
		raw, ok := extractJSONObjectAfter(rest)
		if !ok {
			malformed = true
			continue
		}
		call, ok := parseContentJSONToolCallPayload(raw)
		if ok {
			calls = append(calls, call)
		} else {
			malformed = true
		}
	}
	return calls, malformed
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
	lower := strings.ToLower(s)
	if idx := firstContentToolCallMarkerIndex(lower); idx >= 0 {
		if idx > 0 {
			f.downstream(s[:idx])
		}
		f.suppressed = true
		f.pending.Reset()
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
	if partial := contentToolCallMarkerSuffixLen(lower); partial > 0 && !force {
		if len(s) > partial {
			f.downstream(s[:len(s)-partial])
			f.pending.Reset()
			f.pending.WriteString(s[len(s)-partial:])
		}
		return
	}
	f.downstream(s)
	f.pending.Reset()
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

func firstContentToolCallMarkerIndex(lower string) int {
	best := -1
	for _, marker := range []string{"<tool_call", "<turn: tool_call", "tool_call\n", "tool_call\r\n", "tool_call {"} {
		if idx := strings.Index(lower, marker); idx >= 0 && (best < 0 || idx < best) {
			best = idx
		}
	}
	return best
}

func contentToolCallMarkerSuffixLen(lower string) int {
	best := 0
	for _, marker := range []string{"<tool_call", "<turn: tool_call", "tool_call\n", "tool_call\r\n", "tool_call {"} {
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
	return best
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
		invokes := contentCodexToolInvokeRe.FindAllStringSubmatch(block[1], -1)
		if len(invokes) == 0 {
			malformed = true
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
