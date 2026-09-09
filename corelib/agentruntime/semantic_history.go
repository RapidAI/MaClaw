package agentruntime

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// PreviousTurnSemanticToolName is the compatibility placeholder used by
// persisted history when a previous semantic grant no longer has the same
// invocation name. It is presentation-only and must never be treated as an
// execution alias.
const PreviousTurnSemanticToolName = "previous_turn_tool"

// RewriteExpiredSemanticGrantNames rewrites only the historical placeholder
// when web_search is still the live lookup grant. It returns the original
// slice when no rewrite is needed and deep-copies the mutable tool-call maps
// on rewrite, so persisted history cannot be modified through a view.
//
// Historical invoke_* names intentionally remain untouched: mapping an old
// invocation token to a new surface would grant a late model response the
// authority of a different request.
func RewriteExpiredSemanticGrantNames(history []agent.ConversationEntry, live map[string]bool) []agent.ConversationEntry {
	if len(history) == 0 || !historyHasExpiredSemanticGrantName(history, live) {
		return history
	}
	out := make([]agent.ConversationEntry, len(history))
	copy(out, history)
	for i := range out {
		if rewritten := rewriteExpiredSemanticGrantName(out[i].ToolName, live); rewritten != out[i].ToolName {
			out[i].ToolName = rewritten
		}
		if out[i].ToolCalls != nil {
			out[i].ToolCalls = rewriteExpiredSemanticGrantToolCalls(out[i].ToolCalls, live)
		}
		out[i].Content = rewriteExpiredSemanticGrantContent(out[i].Content, live)
	}
	return out
}

func historyHasExpiredSemanticGrantName(history []agent.ConversationEntry, live map[string]bool) bool {
	for _, entry := range history {
		if expiredSemanticGrantName(entry.ToolName, live) {
			return true
		}
		if historyToolCallsHaveExpiredGrantName(entry.ToolCalls, live) {
			return true
		}
		asMap, ok := entry.Content.(map[string]interface{})
		if ok {
			if name, ok := asMap["name"].(string); ok && expiredSemanticGrantName(name, live) {
				return true
			}
		}
	}
	return false
}

func expiredSemanticGrantName(name string, live map[string]bool) bool {
	name = strings.TrimSpace(name)
	if name != PreviousTurnSemanticToolName {
		return false
	}
	return live != nil && live["web_search"]
}

func historyToolCallsHaveExpiredGrantName(calls interface{}, live map[string]bool) bool {
	switch typed := calls.(type) {
	case []llm.ToolCall:
		for _, call := range typed {
			if expiredSemanticGrantName(call.Function.Name, live) {
				return true
			}
		}
	case []map[string]interface{}:
		for _, call := range typed {
			if expiredSemanticGrantName(semanticGrantNameFromCallMap(call), live) {
				return true
			}
		}
	case []interface{}:
		for _, call := range typed {
			switch typedCall := call.(type) {
			case llm.ToolCall:
				if expiredSemanticGrantName(typedCall.Function.Name, live) {
					return true
				}
			case map[string]interface{}:
				if expiredSemanticGrantName(semanticGrantNameFromCallMap(typedCall), live) {
					return true
				}
			}
		}
	}
	return false
}

func semanticGrantNameFromCallMap(call map[string]interface{}) string {
	if call == nil {
		return ""
	}
	if name := semanticMapStringField(call["function"], "name"); name != "" {
		return name
	}
	if name, ok := call["name"].(string); ok {
		return name
	}
	return ""
}

func semanticMapStringField(value interface{}, key string) string {
	switch typed := value.(type) {
	case map[string]interface{}:
		name, _ := typed[key].(string)
		return name
	case map[string]string:
		return typed[key]
	default:
		return ""
	}
}

func rewriteExpiredSemanticGrantName(name string, live map[string]bool) string {
	name = strings.TrimSpace(name)
	if name != PreviousTurnSemanticToolName {
		return name
	}
	if live != nil && live["web_search"] {
		return "web_search"
	}
	return name
}

func rewriteExpiredSemanticGrantToolCalls(calls interface{}, live map[string]bool) interface{} {
	switch typed := calls.(type) {
	case []llm.ToolCall:
		out := append([]llm.ToolCall(nil), typed...)
		for i := range out {
			out[i].Function.Name = rewriteExpiredSemanticGrantName(out[i].Function.Name, live)
		}
		return out
	case []map[string]interface{}:
		out := make([]map[string]interface{}, len(typed))
		for i, call := range typed {
			out[i] = rewriteExpiredSemanticGrantCallMap(call, live)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(typed))
		for i, call := range typed {
			switch typedCall := call.(type) {
			case map[string]interface{}:
				out[i] = rewriteExpiredSemanticGrantCallMap(typedCall, live)
			case llm.ToolCall:
				typedCall.Function.Name = rewriteExpiredSemanticGrantName(typedCall.Function.Name, live)
				out[i] = typedCall
			default:
				out[i] = call
			}
		}
		return out
	default:
		return calls
	}
}

func rewriteExpiredSemanticGrantCallMap(call map[string]interface{}, live map[string]bool) map[string]interface{} {
	if call == nil {
		return nil
	}
	out := make(map[string]interface{}, len(call)+1)
	for key, value := range call {
		out[key] = value
	}
	switch fn := out["function"].(type) {
	case map[string]interface{}:
		cloned := make(map[string]interface{}, len(fn)+1)
		for key, value := range fn {
			cloned[key] = value
		}
		if name, ok := cloned["name"].(string); ok {
			cloned["name"] = rewriteExpiredSemanticGrantName(name, live)
		}
		out["function"] = cloned
	case map[string]string:
		cloned := make(map[string]string, len(fn)+1)
		for key, value := range fn {
			cloned[key] = value
		}
		if name, ok := cloned["name"]; ok {
			cloned["name"] = rewriteExpiredSemanticGrantName(name, live)
		}
		out["function"] = cloned
	}
	if name, ok := out["name"].(string); ok {
		out["name"] = rewriteExpiredSemanticGrantName(name, live)
	}
	return out
}

func rewriteExpiredSemanticGrantContent(content interface{}, live map[string]bool) interface{} {
	asMap, ok := content.(map[string]interface{})
	if !ok || asMap == nil {
		return content
	}
	name, ok := asMap["name"].(string)
	if !ok {
		return content
	}
	rewritten := rewriteExpiredSemanticGrantName(name, live)
	if rewritten == name {
		return content
	}
	out := make(map[string]interface{}, len(asMap)+1)
	for key, value := range asMap {
		out[key] = value
	}
	out["name"] = rewritten
	return out
}
