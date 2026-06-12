package llm

import "encoding/json"

// ResponsesConvertedInput holds the result of converting OpenAI Chat Completions
// messages into Responses API input format.
type ResponsesConvertedInput struct {
	Instructions string
	Input        []interface{}
}

// ConvertToResponsesInput converts OpenAI Chat Completions messages to
// Responses API input array + instructions string.
//
// Conversion rules (from design §3):
//   - system messages → extracted to Instructions (not in Input)
//   - user messages   → {type:"message", role:"user", content:[{type:"input_text", text:...}]}
//   - assistant text  → {type:"message", role:"assistant", content:[{type:"output_text", text:...}]}
//   - assistant tool_calls → one {type:"function_call", call_id, name, arguments} per call
//   - tool results    → {type:"function_call_output", call_id, output}
//
// Messages can be map[string]interface{} or map[string]string. Tool calls in
// assistant messages can be []ToolCall (typed) or []interface{} (untyped maps).
// For assistant messages with both content and tool_calls, the text message
// item is emitted first, then the function_call items.
func ConvertToResponsesInput(messages []interface{}) ResponsesConvertedInput {
	var result ResponsesConvertedInput
	messages = normalizeOpenAIChatToolCallLinkage(messages)
	for _, m := range messages {
		mm := toStringInterfaceMap(m)
		if mm == nil {
			continue
		}
		role, _ := mm["role"].(string)
		switch role {
		case "system", "developer":
			if content := extractContentString(mm); content != "" {
				if result.Instructions != "" {
					result.Instructions += "\n" + content
				} else {
					result.Instructions = content
				}
			}
		case "user":
			text := extractContentString(mm)
			result.Input = append(result.Input, map[string]interface{}{
				"type": "message",
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "input_text", "text": text},
				},
			})
		case "assistant":
			text := extractContentString(mm)
			toolCalls := extractToolCalls(mm)
			// Emit text message first if present
			if text != "" {
				result.Input = append(result.Input, map[string]interface{}{
					"type": "message",
					"role": "assistant",
					"content": []interface{}{
						map[string]interface{}{"type": "output_text", "text": text},
					},
				})
			}
			// Emit one function_call per tool call
			for _, tc := range toolCalls {
				result.Input = append(result.Input, map[string]interface{}{
					"type":      "function_call",
					"call_id":   tc.ID,
					"name":      tc.Function.Name,
					"arguments": tc.Function.Arguments,
				})
			}
		case "tool":
			callID, _ := mm["tool_call_id"].(string)
			result.Input = append(result.Input, map[string]interface{}{
				"type":    "function_call_output",
				"call_id": callID,
				"output":  stringifyOpenAIToolContent(mm["content"]),
			})
		}
	}
	return result
}

// ConvertToResponsesTools flattens Chat Completions tool definitions to
// Responses API format.
//
// Chat Completions: {type:"function", function:{name, description, parameters}}
// Responses API:    {type:"function", name, description, parameters}
func ConvertToResponsesTools(tools []map[string]interface{}) []map[string]interface{} {
	if len(tools) == 0 {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(tools))
	for _, t := range tools {
		fn := toStringInterfaceMap(t["function"])
		if fn == nil {
			continue
		}
		flat := map[string]interface{}{"type": "function"}
		if name, ok := fn["name"]; ok {
			flat["name"] = name
		}
		if desc, ok := fn["description"]; ok {
			flat["description"] = desc
		}
		if params, ok := fn["parameters"]; ok {
			flat["parameters"] = params
		}
		if strict, ok := fn["strict"].(bool); ok {
			flat["strict"] = strict
		}
		out = append(out, flat)
	}
	return out
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// toStringInterfaceMap normalises a JSON-shaped value to map[string]interface{}.
// Handles plain maps, string maps, and typed message structs used by callers.
func toStringInterfaceMap(m interface{}) map[string]interface{} {
	switch v := m.(type) {
	case map[string]interface{}:
		return v
	case map[string]string:
		out := make(map[string]interface{}, len(v))
		for k, val := range v {
			out[k] = val
		}
		return out
	default:
		data, err := json.Marshal(v)
		if err != nil || len(data) == 0 || string(data) == "null" {
			return nil
		}
		var out map[string]interface{}
		if err := json.Unmarshal(data, &out); err != nil || len(out) == 0 {
			return nil
		}
		return out
	}
}

// extractContentString extracts the "content" field as a string.
func extractContentString(mm map[string]interface{}) string {
	return textFromResponsesContent(mm["content"])
}

// extractToolCalls extracts tool calls from an assistant message.
// Handles both []ToolCall (typed) and []interface{} (untyped maps).
func extractToolCalls(mm map[string]interface{}) []ToolCall {
	raw := mm["tool_calls"]
	if raw == nil {
		return nil
	}
	// Typed slice
	if tcs, ok := raw.([]ToolCall); ok {
		return tcs
	}
	items := toInterfaceSlice(raw)
	if len(items) == 0 {
		return nil
	}
	out := make([]ToolCall, 0, len(items))
	for _, item := range items {
		im := toStringInterfaceMap(item)
		if im == nil {
			continue
		}
		tc := ToolCall{
			ID:   stringField(im, "id"),
			Type: stringField(im, "type"),
		}
		if fn := toStringInterfaceMap(im["function"]); fn != nil {
			tc.Function.Name = stringField(fn, "name")
			tc.Function.Arguments = normalizeOpenAIToolArgumentsString(fn["arguments"])
		}
		if tc.Type == "" {
			tc.Type = "function"
		}
		if tc.ID == "" || tc.Function.Name == "" {
			continue
		}
		out = append(out, tc)
	}
	return out
}

func toInterfaceSlice(raw interface{}) []interface{} {
	switch v := raw.(type) {
	case []interface{}:
		return v
	case nil:
		return nil
	default:
		data, err := json.Marshal(v)
		if err != nil || len(data) == 0 || string(data) == "null" {
			return nil
		}
		var out []interface{}
		if err := json.Unmarshal(data, &out); err != nil {
			return nil
		}
		return out
	}
}

func textFromResponsesContent(raw interface{}) string {
	switch v := raw.(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		items := toInterfaceSlice(v)
		if len(items) == 0 {
			return stringValue(v)
		}
		parts := make([]string, 0, len(items))
		for _, item := range items {
			block := toStringInterfaceMap(item)
			if block == nil {
				continue
			}
			typ := stringField(block, "type")
			switch typ {
			case "text", "input_text", "output_text":
				if text := stringField(block, "text"); text != "" {
					parts = append(parts, text)
				} else if text := stringField(block, "content"); text != "" {
					parts = append(parts, text)
				}
			}
		}
		return joinNonEmptyResponsesText(parts)
	}
}

func joinNonEmptyResponsesText(parts []string) string {
	out := ""
	for _, part := range parts {
		if part == "" {
			continue
		}
		if out != "" {
			out += "\n"
		}
		out += part
	}
	return out
}

// stringField safely extracts a string from a map.
func stringField(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}
