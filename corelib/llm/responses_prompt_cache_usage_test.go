package llm

import "testing"

func TestParseNonStreamResponsesAPIBodyCapturesPromptCacheUsage(t *testing.T) {
	body := []byte(`{
		"id": "resp_1",
		"output": [{"type": "message", "role": "assistant", "content": [{"type": "output_text", "text": "ok"}]}],
		"usage": {
			"input_tokens": 240,
			"output_tokens": 16,
			"total_tokens": 256,
			"input_tokens_details": {"cached_tokens": 128, "cache_creation_input_tokens": 48}
		}
	}`)
	resp, err := ParseNonStreamResponsesAPIBody(body)
	if err != nil {
		t.Fatalf("ParseNonStreamResponsesAPIBody: %v", err)
	}
	if resp.Usage == nil {
		t.Fatal("expected usage")
	}
	if resp.Usage.CachedInputTokens != 128 || resp.Usage.CacheWriteTokens != 48 {
		t.Fatalf("cache usage = cached:%d write:%d", resp.Usage.CachedInputTokens, resp.Usage.CacheWriteTokens)
	}
	if resp.Usage.PromptTokens != 240 || resp.Usage.CompletionTokens != 16 {
		t.Fatalf("normalized usage = prompt:%d completion:%d", resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
	}
}
func TestExtractResponsesAPIUsageFromEventPayloadCapturesNestedAndTopLevelCacheUsage(t *testing.T) {
	nested := ExtractResponsesAPIUsageFromEventPayload([]byte(`{
		"type": "response.completed",
		"response": {
			"usage": {
				"input_tokens": 320,
				"output_tokens": 24,
				"input_tokens_details": {"cached_tokens": 256, "cache_creation_input_tokens": 32}
			}
		}
	}`))
	if nested == nil {
		t.Fatal("expected nested usage")
	}
	if nested.PromptTokens != 320 || nested.CompletionTokens != 24 || nested.CachedInputTokens != 256 || nested.CacheWriteTokens != 32 {
		t.Fatalf("nested usage = %+v", nested)
	}

	topLevel := ExtractResponsesAPIUsageFromEventPayload([]byte(`{
		"type": "response.completed",
		"usage": {
			"input_tokens": 400,
			"output_tokens": 12,
			"cache_read_input_tokens": 300,
			"cache_write_input_tokens": 40
		}
	}`))
	if topLevel == nil {
		t.Fatal("expected top-level usage")
	}
	if topLevel.PromptTokens != 400 || topLevel.CompletionTokens != 12 || topLevel.CachedInputTokens != 300 || topLevel.CacheWriteTokens != 40 {
		t.Fatalf("top-level usage = %+v", topLevel)
	}
}
