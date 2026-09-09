package agentruntime

import (
	"encoding/json"
	"strings"
)

// DocumentReadResultProjection removes legacy continuation hints from a
// document-read result before it is shown on the governed semantic surface.
// Those hints contain path/function syntax that is not valid on the semantic
// adapter and could otherwise steer the model back to an unauthorized call.
func DocumentReadResultProjection(result string) string {
	lines := strings.Split(result, "\n")
	filtered := lines[:0]
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# path:") || strings.HasPrefix(trimmed, "# continue:") {
			continue
		}
		filtered = append(filtered, line)
	}
	return strings.Join(filtered, "\n")
}

// DeliveryInvocationArgs removes only the known empty serialization wrapper
// emitted by older clients. Contentful arguments are returned byte-for-byte
// so the normal schema gate can reject forged artifact/path fields rather than
// silently changing the request.
func DeliveryInvocationArgs(argsJSON string) string {
	var parsed map[string]any
	if json.Unmarshal([]byte(argsJSON), &parsed) != nil || parsed == nil || len(parsed) != 1 {
		return argsJSON
	}
	raw, ok := parsed["arguments"]
	if !ok {
		return argsJSON
	}
	text, ok := raw.(string)
	if !ok {
		return argsJSON
	}
	trimmed := strings.TrimSpace(text)
	// json.Unmarshal("null", &map) succeeds with a nil map. It is not an
	// empty argument object and must remain visible to the schema validator.
	if trimmed == "" || !strings.HasPrefix(trimmed, "{") {
		return argsJSON
	}
	var inner map[string]any
	if json.Unmarshal([]byte(trimmed), &inner) != nil || len(inner) != 0 {
		return argsJSON
	}
	return "{}"
}
