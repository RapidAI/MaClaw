package corelib

import "testing"

func TestLLMPromptCacheKeyNormalizesTransportAndDefaultNoise(t *testing.T) {
	opts := LLMPromptCacheOptions{
		Enabled:                      true,
		NormalizeDeterministicParams: true,
		IgnoreModelField:             true,
		IgnoreUserField:              true,
		IgnoreMetadataField:          true,
	}
	bodyA := map[string]any{
		"model":                 "codex/gpt-5.4",
		"messages":              []any{map[string]any{"role": "user", "content": "repeat this"}},
		"stream":                true,
		"stream_options":        map[string]any{"include_usage": true},
		"temperature":           0,
		"top_p":                 1,
		"n":                     1,
		"tools":                 []any{},
		"tool_choice":           "none",
		"parallel_tool_calls":   true,
		"logprobs":              false,
		"top_logprobs":          0,
		"response_format":       map[string]any{"type": "text"},
		"modalities":            []any{"text"},
		"max_tokens":            0,
		"max_completion_tokens": 0,
		"metadata":              map[string]any{"trace_id": "abc"},
		"user":                  "u1",
	}
	bodyB := map[string]any{
		"messages": []any{map[string]any{"role": "user", "content": "repeat this"}},
	}
	keyA, hashA, err := LLMPromptCacheKey("auto", "codex/gpt-5.4", bodyA, opts)
	if err != nil {
		t.Fatalf("keyA error: %v", err)
	}
	keyB, hashB, err := LLMPromptCacheKey("auto", "auto", bodyB, opts)
	if err != nil {
		t.Fatalf("keyB error: %v", err)
	}
	if keyA != keyB || hashA != hashB {
		t.Fatalf("expected equivalent requests to share cache key: %q/%q vs %q/%q", keyA, hashA, keyB, hashB)
	}
}

func TestLLMPromptCacheableRejectsSampling(t *testing.T) {
	opts := LLMPromptCacheOptions{Enabled: true}
	if decision := LLMPromptCacheable(map[string]any{"messages": []any{}, "temperature": 0.3}, opts); decision.Cacheable || decision.Reason != "temperature" {
		t.Fatalf("unexpected decision: %+v", decision)
	}
	if decision := LLMPromptCacheable(map[string]any{"messages": []any{}, "stream": true, "temperature": 0}, opts); !decision.Cacheable {
		t.Fatalf("streaming transport preference should remain cacheable: %+v", decision)
	}
}
