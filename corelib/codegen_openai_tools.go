package corelib

func SanitizeCodeGenOpenAIChatTools(tools []map[string]interface{}) []map[string]interface{} {
	if len(tools) == 0 {
		return tools
	}
	out := make([]map[string]interface{}, 0, len(tools))
	for _, tool := range tools {
		patched, ok := sanitizeCodeGenOpenAIChatTool(tool)
		if !ok {
			out = append(out, tool)
			continue
		}
		out = append(out, patched)
	}
	return out
}

func SanitizeCodeGenOpenAICompatBody(body map[string]interface{}) {
	if body == nil {
		return
	}
	for _, key := range codeGenOpenAIUnsupportedTopLevelKeys {
		delete(body, key)
	}
	if tools, ok := body["tools"]; ok {
		body["tools"] = SanitizeCodeGenOpenAIChatToolsValue(tools)
	}
	if functions, ok := body["functions"]; ok {
		body["functions"] = SanitizeCodeGenOpenAIFunctionsValue(functions)
	}
}

var codeGenOpenAIUnsupportedTopLevelKeys = []string{
	"stream_options",
	"parallel_tool_calls",
	"store",
	"metadata",
	"response_format",
	"tool_choice",
	"function_call",
	"logprobs",
	"top_logprobs",
}

func SanitizeCodeGenOpenAIChatToolsValue(tools interface{}) interface{} {
	switch x := tools.(type) {
	case []map[string]interface{}:
		return SanitizeCodeGenOpenAIChatTools(x)
	case []interface{}:
		out := make([]interface{}, len(x))
		for i, tool := range x {
			if toolMap, ok := tool.(map[string]interface{}); ok {
				if patched, ok := sanitizeCodeGenOpenAIChatTool(toolMap); ok {
					out[i] = patched
					continue
				}
			}
			out[i] = tool
		}
		return out
	default:
		return tools
	}
}

func SanitizeCodeGenOpenAIFunctionsValue(functions interface{}) interface{} {
	switch x := functions.(type) {
	case []map[string]interface{}:
		out := make([]map[string]interface{}, len(x))
		for i, fn := range x {
			out[i] = sanitizeCodeGenOpenAIFunction(fn)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(x))
		for i, fn := range x {
			if fnMap, ok := fn.(map[string]interface{}); ok {
				out[i] = sanitizeCodeGenOpenAIFunction(fnMap)
				continue
			}
			out[i] = fn
		}
		return out
	default:
		return functions
	}
}

func sanitizeCodeGenOpenAIChatTool(tool map[string]interface{}) (map[string]interface{}, bool) {
	toolType, _ := tool["type"].(string)
	function, ok := tool["function"].(map[string]interface{})
	if !ok {
		return nil, false
	}

	patchedFunction := make(map[string]interface{}, len(function))
	for _, key := range []string{"name", "description", "parameters"} {
		if val, ok := function[key]; ok {
			if key == "parameters" {
				patchedFunction[key] = SanitizeCodeGenOpenAIToolSchemaValue(val)
			} else {
				patchedFunction[key] = val
			}
		}
	}
	if _, ok := patchedFunction["parameters"]; !ok {
		patchedFunction["parameters"] = map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}
	}
	if toolType == "" {
		toolType = "function"
	}
	return map[string]interface{}{
		"type":     toolType,
		"function": patchedFunction,
	}, true
}

func sanitizeCodeGenOpenAIFunction(function map[string]interface{}) map[string]interface{} {
	patched := make(map[string]interface{}, len(function))
	for _, key := range []string{"name", "description", "parameters"} {
		if val, ok := function[key]; ok {
			if key == "parameters" {
				patched[key] = SanitizeCodeGenOpenAIToolSchemaValue(val)
			} else {
				patched[key] = val
			}
		}
	}
	if _, ok := patched["parameters"]; !ok {
		patched["parameters"] = map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}
	}
	return patched
}

func SanitizeCodeGenOpenAIToolSchemaValue(v interface{}) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(x)+1)
		for k, val := range x {
			if codeGenOpenAIToolSchemaUnsupportedKey(k) {
				continue
			}
			out[k] = SanitizeCodeGenOpenAIToolSchemaValue(val)
		}
		if _, ok := out["properties"].(map[string]interface{}); ok {
			if _, hasType := out["type"]; !hasType {
				out["type"] = "object"
			}
		}
		if typ, _ := out["type"].(string); typ == "array" {
			if _, ok := out["items"]; !ok {
				out["items"] = map[string]interface{}{"type": "string"}
			}
		}
		if _, ok := out["additionalProperties"]; ok {
			delete(out, "additionalProperties")
		}
		return out
	case map[string]string:
		out := make(map[string]interface{}, len(x)+1)
		for k, val := range x {
			if codeGenOpenAIToolSchemaUnsupportedKey(k) {
				continue
			}
			out[k] = val
		}
		if typ := out["type"]; typ == "array" {
			if _, ok := out["items"]; !ok {
				out["items"] = map[string]interface{}{"type": "string"}
			}
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(x))
		for i, val := range x {
			out[i] = SanitizeCodeGenOpenAIToolSchemaValue(val)
		}
		return out
	case []map[string]interface{}:
		out := make([]map[string]interface{}, len(x))
		for i, val := range x {
			if patched, ok := SanitizeCodeGenOpenAIToolSchemaValue(val).(map[string]interface{}); ok {
				out[i] = patched
			} else {
				out[i] = val
			}
		}
		return out
	default:
		return v
	}
}

func codeGenOpenAIToolSchemaUnsupportedKey(key string) bool {
	switch key {
	case "$schema", "$id", "$defs", "$ref", "definitions",
		"unevaluatedProperties", "dependentSchemas", "dependentRequired",
		"patternProperties", "propertyNames", "oneOf", "anyOf", "allOf",
		"not", "const", "default", "examples", "format", "readOnly",
		"writeOnly", "nullable", "deprecated":
		return true
	default:
		return false
	}
}
