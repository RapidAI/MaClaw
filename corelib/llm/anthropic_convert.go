package llm

// anthropic_convert.go provides conversion helpers between OpenAI-style
// conversation messages/tools and Anthropic Messages API format.
//
// Migrated from gui/im_llm_client.go as part of the agent-unification plan.

import "encoding/json"

// AnthropicConvertedMessages holds the result of converting OpenAI-style
// messages into Anthropic API format.
type AnthropicConvertedMessages struct {
	SystemText string
	Messages   []interface{}
}

// ConvertToAnthropicMessages converts OpenAI-style conversation messages
// into Anthropic Messages API format, separating the system prompt.
func ConvertToAnthropicMessages(messages []interface{}) AnthropicConvertedMessages {
	var result AnthropicConvertedMessages
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
				if result.SystemText != "" {
					result.SystemText += "\n" + content
				} else {
					result.SystemText = content
				}
			}
		case "assistant":
			var contentBlocks []interface{}
			if text := extractContentString(mm); text != "" {
				contentBlocks = append(contentBlocks, map[string]interface{}{
					"type": "text", "text": text,
				})
			}
			for _, tc := range extractToolCalls(mm) {
				var inputObj interface{}
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &inputObj)
				if inputObj == nil {
					inputObj = map[string]interface{}{}
				}
				contentBlocks = append(contentBlocks, map[string]interface{}{
					"type": "tool_use", "id": tc.ID,
					"name": tc.Function.Name, "input": inputObj,
				})
			}
			if len(contentBlocks) > 0 {
				result.Messages = append(result.Messages, map[string]interface{}{
					"role": "assistant", "content": contentBlocks,
				})
			}
		case "tool":
			toolCallID, _ := mm["tool_call_id"].(string)
			content := stringifyOpenAIToolContent(mm["content"])
			toolResultBlock := map[string]interface{}{
				"type": "tool_result", "id": "toolrslt_" + toolCallID, "tool_use_id": toolCallID, "content": content,
			}
			merged := false
			if len(result.Messages) > 0 {
				if lastMsg, ok := result.Messages[len(result.Messages)-1].(map[string]interface{}); ok {
					if lastRole, _ := lastMsg["role"].(string); lastRole == "user" {
						if blocks, ok := lastMsg["content"].([]interface{}); ok && len(blocks) > 0 {
							if firstBlock, ok := blocks[0].(map[string]interface{}); ok {
								if firstBlock["type"] == "tool_result" {
									lastMsg["content"] = append(blocks, toolResultBlock)
									merged = true
								}
							}
						}
					}
				}
			}
			if !merged {
				result.Messages = append(result.Messages, map[string]interface{}{
					"role": "user", "content": []interface{}{toolResultBlock},
				})
			}
		default:
			content := mm["content"]
			if text := extractContentString(mm); text != "" {
				content = text
			}
			result.Messages = append(result.Messages, map[string]interface{}{
				"role": role, "content": content,
			})
		}
	}
	return result
}

// ConvertToAnthropicTools converts OpenAI-style tool definitions to Anthropic format.
func ConvertToAnthropicTools(tools []map[string]interface{}) []map[string]interface{} {
	var anthropicTools []map[string]interface{}
	for _, t := range tools {
		fn := toStringInterfaceMap(t["function"])
		if fn == nil {
			continue
		}
		at := map[string]interface{}{"name": fn["name"]}
		if desc, ok := fn["description"]; ok {
			at["description"] = desc
		}
		if params, ok := fn["parameters"]; ok {
			at["input_schema"] = params
		}
		anthropicTools = append(anthropicTools, at)
	}
	return anthropicTools
}
