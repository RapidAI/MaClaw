package agent

// tool_definitions.go provides the ToolDef helper for building OpenAI-compatible
// tool definitions. Used by CoreToolRegistry.BuildDefinitions() and by GUI's
// tool_registry_builtin.go.
//
// Tool definitions are NOT maintained here as a standalone list. They are
// registered together with their handlers in tool_register_core.go via
// RegisterCoreTools(). This eliminates the "two independent lists" problem.

// ToolDef builds a single OpenAI-compatible tool definition.
func ToolDef(name, desc string, props map[string]interface{}, required []string) map[string]interface{} {
	params := map[string]interface{}{
		"type": "object",
	}
	if props != nil {
		params["properties"] = props
	} else {
		params["properties"] = map[string]interface{}{}
	}
	if len(required) > 0 {
		params["required"] = required
	}
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        name,
			"description": desc,
			"parameters":  params,
		},
	}
}
