package llm

// anthropic_convert.go provides conversion helpers between OpenAI-style
// conversation messages/tools and Anthropic Messages API format.
//
// Migrated from gui/im_llm_client.go as part of the agent-unification plan.

import (
	"encoding/json"
	"strings"
)

// AnthropicConvertedMessages holds the result of converting OpenAI-style
// messages into Anthropic API format.
type AnthropicConvertedMessages struct {
	SystemText string
	Messages   []interface{}
}

// ConvertToAnthropicMessages converts OpenAI-style conversation messages
// into Anthropic Messages API format, separating the system prompt.
func ConvertToAnthropicMessages(messages []interface{}) AnthropicConvertedMessages {
	return convertToAnthropicMessages(messages, false)
}

// ConvertToAnthropicMessagesPassBackThinking is ConvertToAnthropicMessages
// for DeepSeek thinking mode. Every assistant message gets a content[].thinking
// block so a tool continuation is not rejected with HTTP 400.
func ConvertToAnthropicMessagesPassBackThinking(messages []interface{}) AnthropicConvertedMessages {
	return convertToAnthropicMessages(messages, true)
}

func convertToAnthropicMessages(messages []interface{}, passBackThinking bool) AnthropicConvertedMessages {
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
			contentBlocks := assistantAnthropicContentBlocks(mm, passBackThinking)
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
			// Preserve image blocks (multimodal user input). Without this the
			// text extraction below would silently drop every image — breaking
			// vision probes and image chat for Anthropic-protocol providers.
			if blocks, ok := convertUserContentToAnthropicBlocks(content); ok {
				result.Messages = append(result.Messages, map[string]interface{}{
					"role": role, "content": blocks,
				})
				continue
			}
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

// assistantAnthropicContentBlocks orders thinking, then text, then tool_use.
// passBackThinking forces a thinking block on every assistant message. DeepSeek
// accepts an empty thinking string and rejects a missing thinking field.
func assistantAnthropicContentBlocks(mm map[string]interface{}, passBackThinking bool) []interface{} {
	thinking := anthropicThinkingBlocksFromContent(mm["content"])
	reasoning, hasReasoning := mm["reasoning_content"].(string)
	if len(thinking) > 0 && anthropicThinkingBlocksText(thinking) == "" && hasReasoning && reasoning != "" {
		filled := cloneAnthropicThinkingBlock(toStringInterfaceMap(thinking[0]))
		filled["thinking"] = reasoning
		thinking = []interface{}{filled}
	}
	if len(thinking) == 0 && passBackThinking {
		text := ""
		if hasReasoning {
			text = reasoning
		}
		thinking = []interface{}{map[string]interface{}{"type": "thinking", "thinking": text}}
	}
	if passBackThinking {
		stampAnthropicThinkingSignature(thinking, mm["thought_signature"])
	}
	blocks := make([]interface{}, 0, len(thinking)+2)
	blocks = append(blocks, thinking...)
	if text := extractContentString(mm); text != "" {
		blocks = append(blocks, map[string]interface{}{"type": "text", "text": text})
	}
	for _, tc := range extractToolCalls(mm) {
		var inputObj interface{}
		_ = json.Unmarshal([]byte(tc.Function.Arguments), &inputObj)
		if inputObj == nil {
			inputObj = map[string]interface{}{}
		}
		blocks = append(blocks, map[string]interface{}{
			"type": "tool_use", "id": tc.ID,
			"name": tc.Function.Name, "input": inputObj,
		})
	}
	return blocks
}

func stampAnthropicThinkingSignature(blocks []interface{}, raw interface{}) {
	sig, _ := raw.(string)
	sig = strings.TrimSpace(sig)
	if sig == "" || len(blocks) == 0 {
		return
	}
	first := toStringInterfaceMap(blocks[0])
	if first == nil || strings.TrimSpace(stringField(first, "signature")) != "" {
		return
	}
	first["signature"] = sig
}

func anthropicThinkingBlocksFromContent(raw interface{}) []interface{} {
	items := toInterfaceSlice(raw)
	if len(items) == 0 {
		return nil
	}
	var out []interface{}
	for _, item := range items {
		block := toStringInterfaceMap(item)
		if block == nil || !strings.EqualFold(stringField(block, "type"), "thinking") {
			continue
		}
		out = append(out, cloneAnthropicThinkingBlock(block))
	}
	return out
}

func cloneAnthropicThinkingBlock(block map[string]interface{}) map[string]interface{} {
	cloned := map[string]interface{}{"type": "thinking"}
	if block == nil {
		cloned["thinking"] = ""
		return cloned
	}
	if thinking, ok := block["thinking"].(string); ok {
		cloned["thinking"] = thinking
	} else if text := stringField(block, "text"); text != "" {
		cloned["thinking"] = text
	} else {
		cloned["thinking"] = ""
	}
	if sig := strings.TrimSpace(stringField(block, "signature")); sig != "" {
		cloned["signature"] = sig
	}
	return cloned
}

func anthropicThinkingBlocksText(blocks []interface{}) string {
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		m := toStringInterfaceMap(block)
		if m == nil {
			continue
		}
		if text, ok := m["thinking"].(string); ok && text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "")
}

// convertUserContentToAnthropicBlocks converts an OpenAI-style user content
// block array to Anthropic content blocks when it contains at least one image.
// Text blocks are kept as-is; OpenAI image_url blocks become Anthropic image
// blocks with a base64 (data URL) or url source; Anthropic-native image blocks
// pass through. Returns ok=false when the array has no image block or holds an
// unrecognized block shape, so the caller keeps its legacy flattening.
func convertUserContentToAnthropicBlocks(raw interface{}) ([]interface{}, bool) {
	items := toInterfaceSlice(raw)
	if len(items) == 0 {
		return nil, false
	}
	hasImage := false
	out := make([]interface{}, 0, len(items))
	for _, item := range items {
		block := toStringInterfaceMap(item)
		if block == nil {
			return nil, false
		}
		switch stringField(block, "type") {
		case "text", "input_text":
			// Anthropic rejects empty text blocks — drop them (only reachable
			// when an image is present, so the message stays non-empty).
			if stringField(block, "text") == "" {
				continue
			}
			// Copy the block so extra keys (e.g. cache_control) survive, and
			// normalize Responses-style input_text to Anthropic's text type.
			cp := make(map[string]interface{}, len(block))
			for k, v := range block {
				cp[k] = v
			}
			cp["type"] = "text"
			out = append(out, cp)
		case "image":
			hasImage = true
			out = append(out, block)
		case "image_url":
			source := openAIImageURLToAnthropicSource(block)
			if source == nil {
				return nil, false
			}
			hasImage = true
			out = append(out, map[string]interface{}{"type": "image", "source": source})
		default:
			return nil, false
		}
	}
	if !hasImage {
		return nil, false
	}
	return out, true
}

// openAIImageURLToAnthropicSource maps an OpenAI image_url block to an
// Anthropic image source. Data URLs become base64 sources; remote URLs become
// url sources. Returns nil when no usable URL is present.
func openAIImageURLToAnthropicSource(block map[string]interface{}) map[string]interface{} {
	u := stringField(block, "url")
	switch iu := block["image_url"].(type) {
	case string:
		// OpenAI also accepts image_url as a plain URL string.
		u = iu
	default:
		if m := toStringInterfaceMap(block["image_url"]); m != nil {
			u = stringField(m, "url")
		}
	}
	if u == "" {
		return nil
	}
	if strings.HasPrefix(u, "data:") {
		rest := strings.TrimPrefix(u, "data:")
		idx := strings.Index(rest, ",")
		if idx < 0 {
			return nil
		}
		meta, data := rest[:idx], rest[idx+1:]
		// Only base64 payloads map to an Anthropic base64 source; URL-encoded
		// data URLs are not valid base64.
		if !strings.HasSuffix(strings.ToLower(meta), ";base64") {
			return nil
		}
		mime := meta[:len(meta)-len(";base64")]
		if mime == "" {
			mime = "image/png"
		}
		return map[string]interface{}{"type": "base64", "media_type": mime, "data": data}
	}
	return map[string]interface{}{"type": "url", "url": u}
}

// anthropicImageSourceToDataURL maps an Anthropic image source back to an
// OpenAI-compatible URL (data URL for base64 sources, plain URL otherwise).
// Returns "" when the source holds no usable payload.
func anthropicImageSourceToDataURL(source map[string]interface{}) string {
	switch strings.ToLower(stringField(source, "type")) {
	case "base64":
		data := stringField(source, "data")
		if data == "" {
			return ""
		}
		mime := stringField(source, "media_type")
		if mime == "" {
			mime = "image/png"
		}
		return "data:" + mime + ";base64," + data
	case "url":
		return stringField(source, "url")
	}
	return ""
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
			at["input_schema"] = cloneToolDefinitionValue(params)
		}
		anthropicTools = append(anthropicTools, at)
	}
	return anthropicTools
}
