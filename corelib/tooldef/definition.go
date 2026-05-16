package tooldef

// Name extracts a tool/function name from an OpenAI-compatible tool
// definition. It also accepts the legacy flat {"name": "..."} shape used by a
// few tests and adapters.
func Name(def map[string]interface{}) string {
	if def == nil {
		return ""
	}
	if fn, ok := def["function"].(map[string]interface{}); ok {
		if name, ok := fn["name"].(string); ok {
			return name
		}
	}
	if name, ok := def["name"].(string); ok {
		return name
	}
	return ""
}

// BuildToolDef constructs an OpenAI-compatible tool definition map from the
// given name, description, and JSON Schema parameters object.
func BuildToolDef(name, description string, parameters map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        name,
			"description": description,
			"parameters":  parameters,
		},
	}
}
