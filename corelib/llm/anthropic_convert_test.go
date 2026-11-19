package llm

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestConvertToAnthropicMessages_PreservesImageBlocks(t *testing.T) {
	messages := []interface{}{
		map[string]interface{}{
			"role": "user",
			"content": []interface{}{
				map[string]interface{}{"type": "text", "text": "read this"},
				map[string]interface{}{
					"type":      "image_url",
					"image_url": map[string]interface{}{"url": "data:image/png;base64,aGVsbG8="},
				},
			},
		},
	}
	converted := ConvertToAnthropicMessages(messages)
	if len(converted.Messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(converted.Messages))
	}
	msg := converted.Messages[0].(map[string]interface{})
	blocks, ok := msg["content"].([]interface{})
	if !ok {
		t.Fatalf("content type = %T, want []interface{} (image must not be flattened away)", msg["content"])
	}
	if len(blocks) != 2 {
		t.Fatalf("blocks = %d, want 2", len(blocks))
	}
	text := blocks[0].(map[string]interface{})
	if text["type"] != "text" || text["text"] != "read this" {
		t.Fatalf("text block = %+v", text)
	}
	image := blocks[1].(map[string]interface{})
	if image["type"] != "image" {
		t.Fatalf("image block type = %v, want image", image["type"])
	}
	source := image["source"].(map[string]interface{})
	if source["type"] != "base64" || source["media_type"] != "image/png" || source["data"] != "aGVsbG8=" {
		t.Fatalf("image source = %+v", source)
	}
}

func TestConvertToAnthropicMessages_PassesThroughAnthropicImageBlocks(t *testing.T) {
	imageBlock := map[string]interface{}{
		"type":   "image",
		"source": map[string]interface{}{"type": "base64", "media_type": "image/jpeg", "data": "LzlqLzRBQVE="},
	}
	messages := []interface{}{
		map[string]interface{}{
			"role":    "user",
			"content": []interface{}{map[string]interface{}{"type": "text", "text": "hi"}, imageBlock},
		},
	}
	converted := ConvertToAnthropicMessages(messages)
	msg := converted.Messages[0].(map[string]interface{})
	blocks, ok := msg["content"].([]interface{})
	if !ok || len(blocks) != 2 {
		t.Fatalf("content = %#v, want 2 blocks", msg["content"])
	}
	got := blocks[1].(map[string]interface{})
	if got["type"] != "image" {
		t.Fatalf("block type = %v, want image passthrough", got["type"])
	}
}

func TestConvertToAnthropicMessagesPassBackThinking(t *testing.T) {
	messages := []interface{}{
		map[string]interface{}{"role": "user", "content": "continue"},
		map[string]interface{}{
			"role":              "assistant",
			"content":           "working",
			"reasoning_content": "The release folder is still v33.",
			"thought_signature": "sig-9",
			"tool_calls": []interface{}{map[string]interface{}{
				"id":   "call_9",
				"type": "function",
				"function": map[string]interface{}{
					"name":      "read_file",
					"arguments": `{"path":"README.md"}`,
				},
			}},
		},
		map[string]interface{}{
			"role":              "assistant",
			"content":           "ok",
			"reasoning_content": "",
		},
	}
	converted := ConvertToAnthropicMessagesPassBackThinking(messages)
	if len(converted.Messages) != 3 {
		t.Fatalf("messages = %d, want 3", len(converted.Messages))
	}
	toolTurn := converted.Messages[1].(map[string]interface{})["content"].([]interface{})
	thinking := toolTurn[0].(map[string]interface{})
	if thinking["type"] != "thinking" || thinking["thinking"] != "The release folder is still v33." || thinking["signature"] != "sig-9" {
		t.Fatalf("thinking = %#v", thinking)
	}
	if toolTurn[1].(map[string]interface{})["type"] != "text" || toolTurn[2].(map[string]interface{})["type"] != "tool_use" {
		t.Fatalf("tool turn = %#v", toolTurn)
	}
	plain := converted.Messages[2].(map[string]interface{})["content"].([]interface{})
	if plain[0].(map[string]interface{})["thinking"] != "" {
		t.Fatalf("empty thinking = %#v", plain[0])
	}

	without := ConvertToAnthropicMessages(messages)
	plainBlocks := without.Messages[1].(map[string]interface{})["content"].([]interface{})
	if plainBlocks[0].(map[string]interface{})["type"] == "thinking" {
		t.Fatalf("default convert invented thinking: %#v", plainBlocks)
	}
}

func TestBuildAnthropicMessagesRequestBodyPassesDeepSeekThinkingBack(t *testing.T) {
	req := BuildAnthropicMessagesRequestBody(
		corelib.MaclawLLMConfig{URL: "https://api.deepseek.com/anthropic", Model: "deepseek-v4-flash", Protocol: "anthropic"},
		[]interface{}{
			map[string]interface{}{"role": "user", "content": "go on"},
			map[string]interface{}{
				"role":              "assistant",
				"content":           "",
				"reasoning_content": "Count the tables before editing the release.",
				"thought_signature": "sig-2",
				"tool_calls": []map[string]interface{}{{
					"id":   "call_2",
					"type": "function",
					"function": map[string]interface{}{
						"name":      "grep",
						"arguments": `{"pattern":"34"}`,
					},
				}},
			},
			map[string]interface{}{"role": "tool", "tool_call_id": "call_2", "content": "34 tables"},
		},
		AnthropicMessagesRequestOptions{Tools: []map[string]interface{}{{
			"type": "function",
			"function": map[string]interface{}{
				"name":       "grep",
				"parameters": map[string]interface{}{"type": "object"},
			},
		}}},
	)
	messages := req["messages"].([]interface{})
	if len(messages) != 3 {
		t.Fatalf("messages = %d, want user, assistant, tool result", len(messages))
	}
	assistant := messages[1].(map[string]interface{})
	if _, ok := assistant["reasoning_content"]; ok {
		t.Fatal("anthropic message kept reasoning_content")
	}
	blocks := assistant["content"].([]interface{})
	got := blocks[0].(map[string]interface{})
	if got["type"] != "thinking" || got["thinking"] != "Count the tables before editing the release." || got["signature"] != "sig-2" {
		t.Fatalf("thinking block = %#v", got)
	}
	toolUse := blocks[len(blocks)-1].(map[string]interface{})
	if toolUse["type"] != "tool_use" || toolUse["id"] != "call_2" || toolUse["name"] != "grep" {
		t.Fatalf("tool_use = %#v", toolUse)
	}
	result := messages[2].(map[string]interface{})
	resultBlocks := result["content"].([]interface{})
	if result["role"] != "user" || resultBlocks[0].(map[string]interface{})["type"] != "tool_result" {
		t.Fatalf("tool result = %#v", result)
	}
}

func TestBuildAnthropicMessagesRequestBodyKeepsContentThinking(t *testing.T) {
	req := BuildAnthropicMessagesRequestBody(
		corelib.MaclawLLMConfig{URL: "https://api.deepseek.com/anthropic", Model: "deepseek-v4-flash", Protocol: "anthropic"},
		[]interface{}{
			map[string]interface{}{"role": "user", "content": "go on"},
			map[string]interface{}{
				"role": "assistant",
				"content": []interface{}{
					map[string]interface{}{"type": "thinking", "thinking": "Count from the block.", "signature": "sig-block"},
					map[string]interface{}{"type": "text", "text": "noted"},
				},
			},
		},
		AnthropicMessagesRequestOptions{},
	)
	blocks := req["messages"].([]interface{})[1].(map[string]interface{})["content"].([]interface{})
	thinking := blocks[0].(map[string]interface{})
	if thinking["type"] != "thinking" || thinking["thinking"] != "Count from the block." || thinking["signature"] != "sig-block" {
		t.Fatalf("thinking = %#v", thinking)
	}
	if blocks[1].(map[string]interface{})["text"] != "noted" {
		t.Fatalf("blocks = %#v", blocks)
	}
}

func TestConvertToAnthropicMessages_TextOnlyStillFlattens(t *testing.T) {
	messages := []interface{}{
		map[string]interface{}{
			"role":    "user",
			"content": []interface{}{map[string]interface{}{"type": "text", "text": "plain"}},
		},
	}
	converted := ConvertToAnthropicMessages(messages)
	msg := converted.Messages[0].(map[string]interface{})
	if content, ok := msg["content"].(string); !ok || content != "plain" {
		t.Fatalf("content = %#v, want flattened string %q", msg["content"], "plain")
	}
}

func TestOpenAIImageURLToAnthropicSource_RemoteURL(t *testing.T) {
	source := openAIImageURLToAnthropicSource(map[string]interface{}{
		"type":      "image_url",
		"image_url": map[string]interface{}{"url": "https://example.com/x.png"},
	})
	if source == nil || source["type"] != "url" || source["url"] != "https://example.com/x.png" {
		t.Fatalf("source = %+v", source)
	}
}

func TestOpenAIImageURLToAnthropicSource_StringForm(t *testing.T) {
	source := openAIImageURLToAnthropicSource(map[string]interface{}{
		"type":      "image_url",
		"image_url": "data:image/png;base64,aGVsbG8=",
	})
	if source == nil || source["type"] != "base64" || source["data"] != "aGVsbG8=" {
		t.Fatalf("source = %+v", source)
	}
}

func TestOpenAIImageURLToAnthropicSource_UppercaseBase64Marker(t *testing.T) {
	source := openAIImageURLToAnthropicSource(map[string]interface{}{
		"type":      "image_url",
		"image_url": map[string]interface{}{"url": "data:image/png;BASE64,aGVsbG8="},
	})
	if source == nil || source["media_type"] != "image/png" {
		t.Fatalf("source = %+v, want clean media_type", source)
	}
}

func TestOpenAIImageURLToAnthropicSource_RejectsNonBase64DataURL(t *testing.T) {
	if source := openAIImageURLToAnthropicSource(map[string]interface{}{
		"type":      "image_url",
		"image_url": map[string]interface{}{"url": "data:image/svg+xml,%3Csvg%3E"},
	}); source != nil {
		t.Fatalf("source = %+v, want nil for URL-encoded data URL", source)
	}
}

func TestConvertToAnthropicMessages_DropsEmptyTextBlockWithImage(t *testing.T) {
	messages := []interface{}{
		map[string]interface{}{
			"role": "user",
			"content": []interface{}{
				map[string]interface{}{"type": "text", "text": ""},
				map[string]interface{}{
					"type":      "image_url",
					"image_url": map[string]interface{}{"url": "data:image/png;base64,aGVsbG8="},
				},
			},
		},
	}
	converted := ConvertToAnthropicMessages(messages)
	msg := converted.Messages[0].(map[string]interface{})
	blocks := msg["content"].([]interface{})
	if len(blocks) != 1 || blocks[0].(map[string]interface{})["type"] != "image" {
		t.Fatalf("blocks = %+v, want only the image block", blocks)
	}
}

func TestSanitizeOpenAIContentBlocks_ImageBecomesImageURL(t *testing.T) {
	out := sanitizeOpenAIContentBlocks([]interface{}{
		map[string]interface{}{
			"type":   "image",
			"source": map[string]interface{}{"type": "base64", "media_type": "image/png", "data": "aGVsbG8="},
		},
	})
	if len(out) != 1 {
		t.Fatalf("blocks = %d, want 1", len(out))
	}
	block := out[0].(map[string]interface{})
	if block["type"] != "image_url" {
		t.Fatalf("block type = %v, want image_url", block["type"])
	}
	iu := block["image_url"].(map[string]interface{})
	if iu["url"] != "data:image/png;base64,aGVsbG8=" {
		t.Fatalf("url = %v", iu["url"])
	}
}

func TestConvertToAnthropicMessages_InputTextNormalized(t *testing.T) {
	messages := []interface{}{
		map[string]interface{}{
			"role": "user",
			"content": []interface{}{
				map[string]interface{}{"type": "input_text", "text": "look"},
				map[string]interface{}{
					"type":      "image_url",
					"image_url": map[string]interface{}{"url": "data:image/png;base64,aGVsbG8="},
				},
			},
		},
	}
	converted := ConvertToAnthropicMessages(messages)
	msg := converted.Messages[0].(map[string]interface{})
	blocks := msg["content"].([]interface{})
	if got := blocks[0].(map[string]interface{})["type"]; got != "text" {
		t.Fatalf("block type = %v, want normalized text", got)
	}
}

func TestConvertToAnthropicTools_ClonesNestedParameters(t *testing.T) {
	parameters := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{"type": "string"},
		},
	}
	tools := []map[string]interface{}{{
		"type": "function",
		"function": map[string]interface{}{
			"name": "search", "parameters": parameters,
		},
	}}

	converted := ConvertToAnthropicTools(tools)
	convertedParameters := converted[0]["input_schema"].(map[string]interface{})
	convertedParameters["type"] = "array"
	convertedParameters["properties"].(map[string]interface{})["query"].(map[string]interface{})["type"] = "integer"

	if parameters["type"] != "object" {
		t.Fatalf("source parameter type mutated: %#v", parameters)
	}
	sourceQuery := parameters["properties"].(map[string]interface{})["query"].(map[string]interface{})
	if sourceQuery["type"] != "string" {
		t.Fatalf("source nested parameter mutated: %#v", parameters)
	}
}

func TestConvertToAnthropicTools_ClonesNamedJSONCollectionTypes(t *testing.T) {
	type namedSchema map[string]interface{}
	type namedEnum []string
	parameters := namedSchema{
		"type": "object",
		"properties": namedSchema{
			"format": namedSchema{"type": "string", "enum": namedEnum{"json", "text"}},
		},
	}
	tools := []map[string]interface{}{{
		"type":     "function",
		"function": map[string]interface{}{"name": "named", "parameters": parameters},
	}}

	converted := ConvertToAnthropicTools(tools)
	convertedParams := converted[0]["input_schema"].(namedSchema)
	convertedParams["properties"].(namedSchema)["format"].(namedSchema)["enum"].(namedEnum)[0] = "rewritten"

	if got := parameters["properties"].(namedSchema)["format"].(namedSchema)["enum"].(namedEnum)[0]; got != "json" {
		t.Fatalf("source named schema mutated: %#v", parameters)
	}
}
