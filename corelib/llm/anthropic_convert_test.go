package llm

import "testing"

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
