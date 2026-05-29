package corelib

import (
	"path/filepath"
	"testing"
)

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

func TestLLMPromptCacheableRejectsStoreSideEffect(t *testing.T) {
	opts := LLMPromptCacheOptions{Enabled: true}
	if decision := LLMPromptCacheable(map[string]any{"messages": []any{}, "store": true}, opts); decision.Cacheable || decision.Reason != "store" {
		t.Fatalf("store=true decision = %+v, want uncacheable store", decision)
	}
	if decision := LLMPromptCacheable(map[string]any{"messages": []any{}, "store": false}, opts); !decision.Cacheable {
		t.Fatalf("store=false should remain cacheable: %+v", decision)
	}
}

func TestLLMPromptCacheKeyPreservesScopeAndModelCase(t *testing.T) {
	body := map[string]any{"messages": []any{map[string]any{"role": "user", "content": "repeat this"}}}
	opts := LLMPromptCacheOptions{Enabled: true, IgnoreModelField: false}
	keyA, _, err := LLMPromptCacheKey("openai|Local|abc", "Model-A", body, opts)
	if err != nil {
		t.Fatalf("keyA error: %v", err)
	}
	keyB, _, err := LLMPromptCacheKey("openai|local|abc", "model-a", body, opts)
	if err != nil {
		t.Fatalf("keyB error: %v", err)
	}
	if keyA == keyB {
		t.Fatalf("case-distinct scope/model should not share cache key: %q", keyA)
	}
}

func TestLLMPromptCacheConfigDefaultsCacheDirUnderMaclaw(t *testing.T) {
	cfg := LLMPromptCacheConfig{}.WithDefaults()
	if cfg.CacheDir != DefaultLLMPromptCacheDir() {
		t.Fatalf("cache dir = %q, want %q", cfg.CacheDir, DefaultLLMPromptCacheDir())
	}
	if filepath.Base(cfg.CacheDir) != "llm_prompt_cache" {
		t.Fatalf("cache dir base = %q, want llm_prompt_cache", filepath.Base(cfg.CacheDir))
	}
}

func TestExpandLLMPromptCacheDirExpandsTilde(t *testing.T) {
	got := ExpandLLMPromptCacheDir("~/custom_llm_cache")
	if filepath.Base(got) != "custom_llm_cache" || got == "~/custom_llm_cache" {
		t.Fatalf("expanded dir = %q", got)
	}
}
