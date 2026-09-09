package agentruntime

import "strings"

// AppendUniqueStrings appends non-empty values in stable first-seen order.
// It is used for transport-neutral artifact/path projections where duplicate
// entries would otherwise cause repeated delivery or inconsistent snapshots.
// Existing destination order is preserved and values are compared after
// trimming surrounding whitespace.
func AppendUniqueStrings(dst []string, values ...string) []string {
	seen := make(map[string]struct{}, len(dst)+len(values))
	for _, value := range dst {
		if value = strings.TrimSpace(value); value != "" {
			seen[value] = struct{}{}
		}
	}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		dst = append(dst, value)
	}
	return dst
}

// FilterToolDefinitionsByName removes OpenAI function-tool definitions whose
// nested function name matches name. The operation preserves order and reuses
// the destination slice, matching the low-allocation projection used by host
// adapters while keeping tool-surface filtering transport neutral.
func FilterToolDefinitionsByName(definitions []map[string]any, name string) []map[string]any {
	name = strings.TrimSpace(name)
	filtered := definitions[:0]
	for _, definition := range definitions {
		if ToolDefinitionName(definition) != name {
			filtered = append(filtered, definition)
		}
	}
	return filtered
}

// ToolDefinitionName extracts the canonical nested OpenAI function name.
// Malformed definitions return an empty name so filtering cannot accidentally
// remove an unrelated entry.
func ToolDefinitionName(definition map[string]any) string {
	function, ok := definition["function"].(map[string]any)
	if !ok {
		return ""
	}
	name, _ := function["name"].(string)
	return strings.TrimSpace(name)
}
