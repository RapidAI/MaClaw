package llm

import (
	"strings"
	"testing"
)

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

func TestParseNonStreamOpenAIResponseBodyCapturesTopLevelPromptCacheUsage(t *testing.T) {
	body := []byte(`{
		"choices": [{"message": {"role": "assistant", "content": "ok"}, "finish_reason": "stop"}],
		"usage": {
			"input_tokens": 200,
			"output_tokens": 10,
			"cache_read_input_tokens": 144,
			"cache_creation_input_tokens": 32
		}
	}`)
	resp, err := ParseNonStreamOpenAIResponseBody(body)
	if err != nil {
		t.Fatalf("ParseNonStreamOpenAIResponseBody: %v", err)
	}
	if resp.Usage == nil {
		t.Fatal("expected usage")
	}
	if resp.Usage.CachedInputTokens != 144 || resp.Usage.CacheWriteTokens != 32 {
		t.Fatalf("cache usage = cached:%d write:%d", resp.Usage.CachedInputTokens, resp.Usage.CacheWriteTokens)
	}
	if resp.Usage.TotalTokens != 210 {
		t.Fatalf("total tokens = %d", resp.Usage.TotalTokens)
	}
}

func TestParseSSEToResponseCapturesUsageOnlyPromptCacheChunk(t *testing.T) {
	body := []byte(`data: {"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}
data: {"choices":[],"usage":{"prompt_tokens":180,"completion_tokens":9,"total_tokens":189,"prompt_tokens_details":{"cached_tokens":120,"cache_write_tokens":24}}}
data: [DONE]
`)
	resp, err := ParseSSEToResponse(body)
	if err != nil {
		t.Fatalf("ParseSSEToResponse: %v", err)
	}
	if resp.Usage == nil {
		t.Fatal("expected usage from usage-only SSE chunk")
	}
	if resp.Usage.CachedInputTokens != 120 || resp.Usage.CacheWriteTokens != 24 {
		t.Fatalf("cache usage = cached:%d write:%d", resp.Usage.CachedInputTokens, resp.Usage.CacheWriteTokens)
	}
	if got := resp.Choices[0].Message.Content; got != "ok" {
		t.Fatalf("content = %q", got)
	}
}

func TestParseSSEStreamCapturesPromptCacheUsageOnChoiceChunk(t *testing.T) {
	body := strings.NewReader(`data: {"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":180,"completion_tokens":9,"total_tokens":189,"prompt_tokens_details":{"cached_tokens":120,"cache_write_tokens":24}}}
data: [DONE]
`)
	resp, err := parseSSEStream(body, nil)
	if err != nil {
		t.Fatalf("parseSSEStream: %v", err)
	}
	if resp.Usage == nil {
		t.Fatal("expected usage from choice SSE chunk")
	}
	if resp.Usage.CachedInputTokens != 120 || resp.Usage.CacheWriteTokens != 24 {
		t.Fatalf("cache usage = cached:%d write:%d", resp.Usage.CachedInputTokens, resp.Usage.CacheWriteTokens)
	}
}

func TestParseAnthropicResponseBodyCapturesPromptCacheUsage(t *testing.T) {
	body := []byte(`{
		"content": [{"type": "text", "text": "ok"}],
		"stop_reason": "end_turn",
		"usage": {
			"input_tokens": 300,
			"output_tokens": 12,
			"cache_read_input_tokens": 180,
			"cache_creation_input_tokens": 64
		}
	}`)
	resp, err := parseAnthropicResponseBody(body)
	if err != nil {
		t.Fatalf("parseAnthropicResponseBody: %v", err)
	}
	if resp.Usage == nil {
		t.Fatal("expected usage")
	}
	if resp.Usage.CachedInputTokens != 180 || resp.Usage.CacheWriteTokens != 64 {
		t.Fatalf("cache usage = cached:%d write:%d", resp.Usage.CachedInputTokens, resp.Usage.CacheWriteTokens)
	}
}

func TestParseAnthropicSSEStreamCapturesPromptCacheUsage(t *testing.T) {
	body := strings.NewReader(strings.Join([]string{
		`data: {"type":"message_start","message":{"usage":{"input_tokens":300,"cache_read_input_tokens":180,"cache_creation_input_tokens":64}}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":12}}`,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n"))
	resp, err := parseAnthropicSSEStream(body, nil)
	if err != nil {
		t.Fatalf("parseAnthropicSSEStream: %v", err)
	}
	if resp.Usage == nil {
		t.Fatal("expected usage")
	}
	if resp.Usage.CachedInputTokens != 180 || resp.Usage.CacheWriteTokens != 64 {
		t.Fatalf("cache usage = cached:%d write:%d", resp.Usage.CachedInputTokens, resp.Usage.CacheWriteTokens)
	}
	if resp.Usage.OutputTokens != 12 || resp.Usage.TotalTokens != 312 {
		t.Fatalf("usage = input:%d output:%d total:%d", resp.Usage.InputTokens, resp.Usage.OutputTokens, resp.Usage.TotalTokens)
	}
}
