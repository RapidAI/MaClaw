package agentruntime

// This file contains the transport-neutral half of tool-call handling.  A
// model can emit the same call through Wails, IM, HTTP, or TUI; argument
// cleanup and compatibility aliases therefore must not live in one host
// package.  Side effects and policy checks remain host/module concerns.

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// NormalizeToolArgumentsJSON applies the shared LLM argument sanitizer and
// gives empty argument payloads one canonical representation.  It deliberately
// does not parse the result: callers that need validation should use
// ParseToolArgumentsObject so malformed payloads can be reported at the
// appropriate execution boundary.
func NormalizeToolArgumentsJSON(argsJSON string) string {
	cleaned := SanitizeToolArguments(strings.TrimSpace(argsJSON))
	if cleaned == "" {
		return "{}"
	}
	return cleaned
}

// SanitizeToolArguments removes common wrappers emitted by smaller LLMs
// (code fences, single-quote wrappers, and over-escaped quotes).  The
// transport-neutral implementation lives here so GUI, srv, TUI, and the
// historical corelib/tool helper all share one sanitizer.
func SanitizeToolArguments(args string) string {
	args = strings.TrimSpace(args)
	if args == "" {
		return args
	}
	for _, fence := range []string{"```json", "```JSON", "```"} {
		if strings.HasPrefix(args, fence) && strings.HasSuffix(strings.TrimRight(args, "\n\r\t "), "```") {
			args = strings.TrimPrefix(args, fence)
			args = strings.TrimLeft(args, "\n\r\t ")
			if idx := strings.LastIndex(args, "```"); idx >= 0 {
				args = args[:idx]
			}
			args = strings.TrimRight(args, "\n\r\t ")
			break
		}
	}
	if len(args) >= 2 && args[0] == '\'' && args[len(args)-1] == '\'' {
		inner := args[1 : len(args)-1]
		trimmed := strings.TrimSpace(inner)
		if (strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}")) ||
			(strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]")) {
			args = inner
		}
	}
	// Only unwrap escaped structural quotes when the payload itself starts
	// with an escaped JSON object/array. A normal object begins with `{"` and
	// must not be rewritten: replacing every `\"` inside a valid string would
	// corrupt legitimate escaped quotes.
	if strings.HasPrefix(args, `{\"`) || strings.HasPrefix(args, `[\"`) {
		args = strings.ReplaceAll(args, `\"`, `"`)
	}
	args = strings.ReplaceAll(args, `\'`, `'`)
	return strings.TrimSpace(args)
}

// ParseToolArgumentsObject parses a model tool payload and requires a JSON
// object.  Arrays, scalars and null are rejected consistently by GUI and
// headless hosts instead of being silently converted into an empty object.
func ParseToolArgumentsObject(argsJSON string) (map[string]any, error) {
	cleaned := NormalizeToolArgumentsJSON(argsJSON)
	var args map[string]any
	if err := json.Unmarshal([]byte(cleaned), &args); err != nil {
		return nil, err
	}
	if args == nil {
		return nil, fmt.Errorf("arguments must be a JSON object")
	}
	return args, nil
}

// CanonicalizeBrowserToolCall converts legacy browser_<action> names into the
// merged browser(action=<action>) contract.  It only rewrites the call shape;
// session ownership, action policy, and side effects stay in the host adapter.
func CanonicalizeBrowserToolCall(name string, args map[string]any) (string, map[string]any, bool) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "browser" || !strings.HasPrefix(trimmed, "browser_") {
		return trimmed, args, false
	}
	action := strings.TrimPrefix(trimmed, "browser_")
	if action == "" {
		return trimmed, args, false
	}
	out := make(map[string]any, len(args)+1)
	for key, value := range args {
		out[key] = value
	}
	out["action"] = action
	return "browser", out, true
}

// CanonicalizeBrowserToolCallJSON is the JSON adapter used by streaming
// loops.  A malformed argument payload still gets a canonical tool name so
// the normal argument-error path can give the model a useful retry; it is not
// swallowed or replaced with an empty object here.
func CanonicalizeBrowserToolCallJSON(name, argsJSON string) (string, string, bool) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "browser" || !strings.HasPrefix(trimmed, "browser_") {
		return trimmed, argsJSON, false
	}
	var args map[string]any
	cleaned := strings.TrimSpace(argsJSON)
	if cleaned != "" {
		parsed, err := ParseToolArgumentsObject(cleaned)
		if err != nil {
			canonical, _, rewritten := CanonicalizeBrowserToolCall(trimmed, nil)
			return canonical, argsJSON, rewritten
		}
		args = parsed
	}
	canonical, rewrittenArgs, rewritten := CanonicalizeBrowserToolCall(trimmed, args)
	if !rewritten {
		return trimmed, argsJSON, false
	}
	encoded, err := json.Marshal(rewrittenArgs)
	if err != nil {
		return canonical, argsJSON, true
	}
	return canonical, string(encoded), true
}

// CanonicalizeToolCallJSON applies every transport-neutral compatibility alias
// in the same order used by Runtime hosts.  The operation is idempotent, so a
// GUI loop may call it at the streaming boundary and a headless executor may
// call it again at the execution boundary without changing the payload.
func CanonicalizeToolCallJSON(name, argsJSON string) (string, string, bool) {
	canonicalName, canonicalArgs, browserChanged := CanonicalizeBrowserToolCallJSON(name, argsJSON)
	canonicalName, canonicalArgs, localChanged := CanonicalizeLocalFileSearchToolCallJSON(canonicalName, canonicalArgs)
	return canonicalName, canonicalArgs, browserChanged || localChanged
}

// UnsupportedBrowserAction returns the normalized action for browser
// operations intentionally excluded from the stable merged browser path.
// An empty return means the action is supported or was not a browser call.
// Keeping this list in Runtime prevents GUI and headless adapters from
// accepting different legacy actions.
func UnsupportedBrowserAction(name string, args map[string]any) string {
	trimmed := strings.ToLower(strings.TrimSpace(name))
	if trimmed == "" {
		return ""
	}
	action := strings.TrimPrefix(trimmed, "browser_")
	if trimmed == "browser" {
		if value, ok := args["action"].(string); ok {
			action = strings.ToLower(strings.TrimSpace(value))
		}
	}
	switch action {
	case "eval", "click_at", "get_text", "get_html", "screenshot", "ocr", "task_replay", "record_start", "record_stop":
		return action
	default:
		return ""
	}
}

// CanonicalizeLocalFileSearchToolCall maps historical aliases to the shared
// Glob tool and promotes glob_pattern/glob into the canonical pattern field.
// The returned map is copied before mutation, so callers can safely reuse the
// decoded model payload for audit or retry diagnostics.
func CanonicalizeLocalFileSearchToolCall(name string, args map[string]any) (string, map[string]any, bool) {
	trimmed := strings.TrimSpace(name)
	canonical := canonicalLocalFileSearchToolName(trimmed)
	nameChanged := canonical != trimmed
	if canonical != "Glob" {
		if nameChanged {
			return canonical, args, true
		}
		return trimmed, args, false
	}
	if args == nil {
		if nameChanged {
			return canonical, nil, true
		}
		return trimmed, args, false
	}
	pattern := stringValue(args["pattern"])
	if pattern == "" {
		pattern = stringValue(args["glob_pattern"])
		if pattern == "" {
			pattern = stringValue(args["glob"])
		}
	}
	argsChanged := false
	if pattern != "" && stringValue(args["pattern"]) != pattern {
		out := make(map[string]any, len(args)+1)
		for key, value := range args {
			out[key] = value
		}
		out["pattern"] = pattern
		args = out
		argsChanged = true
	}
	if nameChanged || argsChanged {
		return canonical, args, true
	}
	return trimmed, args, false
}

// CanonicalizeLocalFileSearchToolCallJSON is the JSON adapter for the alias
// contract.  As with browser aliases, an invalid payload may still have its
// tool name canonicalized while the original payload is preserved for the
// shared parse error path.
func CanonicalizeLocalFileSearchToolCallJSON(name, argsJSON string) (string, string, bool) {
	trimmedArgs := strings.TrimSpace(argsJSON)
	var args map[string]any
	if trimmedArgs != "" {
		parsed, err := ParseToolArgumentsObject(trimmedArgs)
		if err != nil {
			canonical := canonicalLocalFileSearchToolName(name)
			if canonical != strings.TrimSpace(name) {
				return canonical, argsJSON, true
			}
			return name, argsJSON, false
		}
		args = parsed
	}
	canonical, rewrittenArgs, rewritten := CanonicalizeLocalFileSearchToolCall(name, args)
	if !rewritten {
		return name, argsJSON, false
	}
	if rewrittenArgs == nil {
		return canonical, argsJSON, true
	}
	encoded, err := json.Marshal(rewrittenArgs)
	if err != nil {
		return canonical, argsJSON, true
	}
	return canonical, string(encoded), true
}

func canonicalLocalFileSearchToolName(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "glob", "glob_file_search", "glob_file_search_tool", "search_files", "search_file":
		return "Glob"
	default:
		return strings.TrimSpace(name)
	}
}

func stringValue(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

// NonEmptyStringFromAny renders a loosely-typed model payload field as a
// trimmed string, treating nil and the "<nil>" formatting artifact as empty.
// Hosts share this so routing fields extracted from either side of the MCP
// call envelope use identical emptiness semantics.
func NonEmptyStringFromAny(value any) string {
	if value == nil {
		return ""
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" || text == "<nil>" {
		return ""
	}
	return text
}

// ExtractMCPToolRequiredArgs extracts the "required" field from an MCP tool's
// InputSchema. Returns nil if the schema or required field is absent.
func ExtractMCPToolRequiredArgs(schema map[string]any) []string {
	if schema == nil {
		return nil
	}
	raw, ok := schema["required"]
	if !ok {
		return nil
	}
	arr, ok := raw.([]any)
	if !ok {
		// Try []string directly (some implementations).
		if strArr, ok := raw.([]string); ok {
			return normalizeMCPRequiredArgs(strArr)
		}
		return nil
	}
	result := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok {
			result = append(result, s)
		}
	}
	return normalizeMCPRequiredArgs(result)
}

// normalizeMCPRequiredArgs trims, drops empties, and case-insensitively
// deduplicates required argument names while preserving first-seen order.
func normalizeMCPRequiredArgs(args []string) []string {
	if len(args) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(args))
	result := make([]string, 0, len(args))
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue
		}
		key := strings.ToLower(arg)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, arg)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// MCPToolArgumentsFromAny normalizes the call_mcp_tool "arguments" payload.
// Object payloads are shallow-copied so later promotion never mutates the
// decoded model map; string payloads go through the shared JSON object parser
// so malformed payloads fail identically in GUI and headless hosts.
func MCPToolArgumentsFromAny(raw any) (map[string]any, error) {
	switch v := raw.(type) {
	case nil:
		return map[string]any{}, nil
	case map[string]any:
		cloned := make(map[string]any, len(v))
		for key, value := range v {
			cloned[key] = value
		}
		return cloned, nil
	case string:
		return ParseToolArgumentsObject(v)
	default:
		return nil, fmt.Errorf("arguments must be an object or JSON object string, got %T", raw)
	}
}

// NormalizeMCPToolCallArgsForAgentLoop returns a copy of the call_mcp_tool
// envelope with routing fields promoted out of the nested arguments object.
func NormalizeMCPToolCallArgsForAgentLoop(args map[string]any) (map[string]any, error) {
	if args == nil {
		return map[string]any{}, nil
	}
	toolArgs, err := MCPToolArgumentsFromAny(args["arguments"])
	if err != nil {
		return nil, err
	}
	normalized := make(map[string]any, len(args))
	for key, value := range args {
		normalized[key] = value
	}
	PromoteMCPRoutingFields(normalized, toolArgs)
	normalized["arguments"] = toolArgs
	return normalized, nil
}

// PromoteMCPRoutingFields lifts server_id/tool_name out of the nested
// arguments object when the envelope itself does not already carry them, and
// always removes them from the nested object so the MCP schema validator only
// sees real tool arguments.
func PromoteMCPRoutingFields(args map[string]any, toolArgs map[string]any) {
	if args == nil || toolArgs == nil {
		return
	}
	for _, key := range []string{"server_id", "tool_name"} {
		if strings.TrimSpace(NonEmptyStringFromAny(args[key])) == "" {
			if value := strings.TrimSpace(NonEmptyStringFromAny(toolArgs[key])); value != "" {
				args[key] = value
			}
		}
		delete(toolArgs, key)
	}
}

// IsMCPCallEnvelopeKey reports whether a flattened call_mcp_tool field belongs
// to the envelope itself (or is a host-private "_" key) rather than to the
// nested tool arguments.
func IsMCPCallEnvelopeKey(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || strings.HasPrefix(name, "_") {
		return true
	}
	switch name {
	case "server_id", "tool_name", "arguments", "resolved_id":
		return true
	default:
		return false
	}
}

// MCPSchemaArgumentNames lists the argument names declared by an MCP input
// schema, excluding envelope keys. Property names are sorted for determinism
// (Go map iteration order is random); required args follow in first-seen order.
func MCPSchemaArgumentNames(schema map[string]any) []string {
	seen := make(map[string]struct{})
	var names []string
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || IsMCPCallEnvelopeKey(name) {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	if props, ok := schema["properties"].(map[string]any); ok {
		names := make([]string, 0, len(props))
		for name := range props {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			add(name)
		}
	}
	for _, name := range ExtractMCPToolRequiredArgs(schema) {
		add(name)
	}
	return names
}

// PromoteMCPNestedArgsFromCallEnvelope copies flattened call_mcp_tool fields
// (for example query next to server_id) into arguments when the nested object
// omitted them. Non-blank nested values win; blank nested strings do not.
func PromoteMCPNestedArgsFromCallEnvelope(callArgs, toolArgs map[string]any, inputSchema map[string]any) map[string]any {
	if toolArgs == nil {
		toolArgs = map[string]any{}
	}
	if callArgs == nil {
		return toolArgs
	}
	names := MCPSchemaArgumentNames(inputSchema)
	if len(names) == 0 {
		for name := range callArgs {
			if !IsMCPCallEnvelopeKey(name) {
				names = append(names, name)
			}
		}
	}
	for _, name := range names {
		if existing, exists := toolArgs[name]; exists && !mcpBlankString(existing) {
			continue
		}
		if value, ok := callArgs[name]; ok && !mcpBlankString(value) {
			toolArgs[name] = value
		}
	}
	return toolArgs
}

func mcpBlankString(value any) bool {
	s, ok := value.(string)
	return ok && strings.TrimSpace(s) == ""
}
