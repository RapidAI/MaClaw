package remote

import (
	"encoding/json"
	"testing"
)

func TestCodexUsageUnmarshalCacheFields(t *testing.T) {
	raw := `{"type":"turn.completed","usage":{"input_tokens":24763,"cached_input_tokens":24448,"output_tokens":122}}`
	var event CodexEvent
	if err := json.Unmarshal([]byte(raw), &event); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if event.Usage == nil {
		t.Fatal("Usage is nil")
	}
	if event.Usage.InputTokens != 24763 || event.Usage.OutputTokens != 122 || event.Usage.CachedInputTokens != 24448 {
		t.Fatalf("usage = %+v", *event.Usage)
	}
}

func TestCodexUsageUnmarshalNestedCacheFields(t *testing.T) {
	raw := `{"type":"turn.completed","usage":{"prompt_tokens":1200,"completion_tokens":80,"input_tokens_details":{"cached_tokens":768,"cache_creation_input_tokens":128}}}`
	var event CodexEvent
	if err := json.Unmarshal([]byte(raw), &event); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if event.Usage == nil {
		t.Fatal("Usage is nil")
	}
	if event.Usage.InputTokens != 1200 || event.Usage.OutputTokens != 80 {
		t.Fatalf("tokens = %d in, %d out", event.Usage.InputTokens, event.Usage.OutputTokens)
	}
	if event.Usage.CachedInputTokens != 768 || event.Usage.CacheWriteTokens != 128 {
		t.Fatalf("cache tokens = read:%d write:%d", event.Usage.CachedInputTokens, event.Usage.CacheWriteTokens)
	}
}

func TestCodexEventToTextIncludesCacheUsage(t *testing.T) {
	text := CodexEventToText(CodexEvent{
		Type: "turn.completed",
		Usage: &CodexUsage{
			InputTokens:       1000,
			OutputTokens:      200,
			CachedInputTokens: 900,
			CacheWriteTokens:  64,
		},
	})
	if text != "Turn completed (tokens: 1000 in, 200 out, 900 cache read, 64 cache write)" {
		t.Fatalf("text = %q", text)
	}
}
