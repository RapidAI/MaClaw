package agent

import (
	"fmt"
	"strings"
)

// LightTurnToolAllowlist is the shared allowlist for adaptive light turns
// (TUI / agentservice / hosts without GUI execution contracts).
// Matches the light system prompt: prefer live lookup tools, avoid coding/shell/files.
var LightTurnToolAllowlist = map[string]bool{
	"web_search":             true,
	"web_fetch":              true,
	"download_file":          true,
	"current_datetime":       true,
	"memory":                 true, // hosts should still prefer recall; write is discouraged by light prompt
	"tts":                    true,
	"read_tool_result":       true,
	"knowledge_search":       true,
	"knowledge_context_pack": true,
}

// IsLightTurnToolAllowed reports whether name may run on a light prompt profile.
// Empty name is not allowed.
func IsLightTurnToolAllowed(name string) bool {
	n := strings.TrimSpace(name)
	if n == "" {
		return false
	}
	return LightTurnToolAllowlist[n]
}

// LightToolDenyMessage is returned when a non-allowlisted tool is requested
// during a light adaptive-prompt turn (misroute signal for operators + model).
func LightToolDenyMessage(name string) string {
	n := strings.TrimSpace(name)
	if n == "" {
		n = "(unknown)"
	}
	return fmt.Sprintf(
		"Error: tool %q is unavailable on the light prompt profile (adaptive cost path). "+
			"Rephrase as a coding/ops task, or set %s=full for full tools.",
		n, PromptProfileEnvKey,
	)
}

// FilterToolDefsForLightTurn keeps only light-safe tool definitions.
// Tool defs follow OpenAI shape: {"type":"function","function":{"name":"..."}}.
// If filtering would remove everything, the original list is returned (fail-open).
func FilterToolDefsForLightTurn(defs []map[string]interface{}) []map[string]interface{} {
	if len(defs) == 0 {
		return defs
	}
	out := make([]map[string]interface{}, 0, len(defs))
	for _, def := range defs {
		name := toolDefName(def)
		if name == "" {
			continue
		}
		if LightTurnToolAllowlist[name] {
			out = append(out, def)
		}
	}
	if len(out) == 0 {
		return defs
	}
	return out
}

// StrippedLightToolNames returns tool names removed by the light allowlist.
// Empty when nothing would be stripped (or fail-open would apply).
func StrippedLightToolNames(defs []map[string]interface{}) []string {
	if len(defs) == 0 {
		return nil
	}
	var stripped []string
	kept := 0
	for _, def := range defs {
		name := toolDefName(def)
		if name == "" {
			continue
		}
		if LightTurnToolAllowlist[name] {
			kept++
		} else {
			stripped = append(stripped, name)
		}
	}
	if kept == 0 {
		// fail-open path — nothing effectively stripped
		return nil
	}
	return stripped
}

func toolDefName(def map[string]interface{}) string {
	if def == nil {
		return ""
	}
	if fn, ok := def["function"].(map[string]interface{}); ok {
		if name, _ := fn["name"].(string); strings.TrimSpace(name) != "" {
			return strings.TrimSpace(name)
		}
	}
	if name, _ := def["name"].(string); strings.TrimSpace(name) != "" {
		return strings.TrimSpace(name)
	}
	return ""
}
