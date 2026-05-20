package llm

import "testing"

func TestParseNonStreamOpenAIResponseBodyCapturesPromptCacheUsage(t *testing.T) {
	body := []byte(`{
		"choices": [{"message": {"role": "assistant", "content": "ok"}, "finish_reason": "stop"}],
		"usage": {
			"prompt_tokens": 120,
			"completion_tokens": 8,
			"total_tokens": 128,
			"prompt_tokens_details": {"cached_tokens": 96, "cache_write_tokens": 12}
		}
	}`)
	resp, err := ParseNonStreamOpenAIResponseBody(body)
	if err != nil {
		t.Fatalf("ParseNonStreamOpenAIResponseBody: %v", err)
	}
	if resp.Usage == nil {
		t.Fatal("expected usage")
	}
	if resp.Usage.CachedInputTokens != 96 || resp.Usage.CacheWriteTokens != 12 {
		t.Fatalf("cache usage = cached:%d write:%d", resp.Usage.CachedInputTokens, resp.Usage.CacheWriteTokens)
	}
	if resp.Usage.InputTokens != 120 || resp.Usage.OutputTokens != 8 {
		t.Fatalf("normalized usage = input:%d output:%d", resp.Usage.InputTokens, resp.Usage.OutputTokens)
	}
}
