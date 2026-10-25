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

// DeliveryInvocationArgs reduces a current-channel delivery call to the empty
// object. File, image, and voice adapters publish an empty schema: the plan
// and the broker already name the artifact, and a path, a destination, or a
// forged artifact id cannot select a different one. Leaving those fields for
// the schema to reject counts as a progress miss. Five misses stop the turn
// as no_progress, and the host then refuses to attach the file it already
// published. Any JSON object is that same call. A non-object stays unchanged
// so the schema still rejects it. json.Unmarshal("null") succeeds with a nil
// map; that is not an object.
func DeliveryInvocationArgs(argsJSON string) string {
	var parsed map[string]any
	if json.Unmarshal([]byte(argsJSON), &parsed) != nil || parsed == nil {
		return argsJSON
	}
	return "{}"
}
