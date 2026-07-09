package corelib

import (
	"path/filepath"
	"strings"
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

func TestLLMPromptCacheableRejectsLogitBias(t *testing.T) {
	opts := LLMPromptCacheOptions{Enabled: true}
	if decision := LLMPromptCacheable(map[string]any{
		"messages":   []any{map[string]any{"role": "user", "content": "hi"}},
		"logit_bias": map[string]any{"42": 1.0},
	}, opts); decision.Cacheable || decision.Reason != "logit_bias" {
		t.Fatalf("unexpected decision: %+v", decision)
	}
}

func TestLLMPromptCacheKeyStripsEmptyFunctionsAndDefaultFunctionCall(t *testing.T) {
	opts := LLMPromptCacheOptions{Enabled: true, NormalizeDeterministicParams: true}
	bodyA := map[string]any{
		"messages":      []any{map[string]any{"role": "user", "content": "hi"}},
		"functions":     []any{},
		"function_call": "auto",
	}
	bodyB := map[string]any{
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	}
	keyA, _, err := LLMPromptCacheKey("m", "m", bodyA, opts)
	if err != nil {
		t.Fatalf("keyA: %v", err)
	}
	keyB, _, err := LLMPromptCacheKey("m", "m", bodyB, opts)
	if err != nil {
		t.Fatalf("keyB: %v", err)
	}
	if keyA != keyB {
		t.Fatalf("expected equivalent keys, got %q vs %q", keyA, keyB)
	}
}

func TestLLMPromptCacheKeyPreservesToolChoiceNoneWhenToolsPresent(t *testing.T) {
	opts := LLMPromptCacheOptions{Enabled: true}
	tools := []any{map[string]any{"type": "function", "function": map[string]any{"name": "ping"}}}
	bodyNone := map[string]any{
		"messages":    []any{map[string]any{"role": "user", "content": "hi"}},
		"tools":       tools,
		"tool_choice": "none",
	}
	bodyAuto := map[string]any{
		"messages":    []any{map[string]any{"role": "user", "content": "hi"}},
		"tools":       tools,
		"tool_choice": "auto",
	}
	keyNone, _, err := LLMPromptCacheKey("m", "m", bodyNone, opts)
	if err != nil {
		t.Fatalf("keyNone: %v", err)
	}
	keyAuto, _, err := LLMPromptCacheKey("m", "m", bodyAuto, opts)
	if err != nil {
		t.Fatalf("keyAuto: %v", err)
	}
	if keyNone == keyAuto {
		t.Fatalf("tool_choice none must not collapse to auto when tools are present")
	}
}

func TestLLMPromptCacheShouldStoreRejectsToolCallsAndBadStatus(t *testing.T) {
	okBody := []byte(`{"id":"c1","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`)
	if d := LLMPromptCacheShouldStore(okBody, 200); !d.Store {
		t.Fatalf("ok body should store: %+v", d)
	}
	toolBody := []byte(`{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"1","type":"function","function":{"name":"x","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
	if d := LLMPromptCacheShouldStore(toolBody, 200); d.Store || d.Reason != "tool_calls" {
		t.Fatalf("tool body decision = %+v", d)
	}
	fnBody := []byte(`{"output":[{"type":"function_call","name":"x"}]}`)
	if d := LLMPromptCacheShouldStore(fnBody, 200); d.Store || d.Reason != "tool_calls" {
		t.Fatalf("responses function_call decision = %+v", d)
	}
	if d := LLMPromptCacheShouldStore(okBody, 500); d.Store || d.Reason != "status" {
		t.Fatalf("status decision = %+v", d)
	}
	if d := LLMPromptCacheShouldStore([]byte("not-json"), 200); d.Store || d.Reason != "invalid_json" {
		t.Fatalf("invalid json decision = %+v", d)
	}
	if d := LLMPromptCacheShouldStoreWithLimit(okBody, 200, 4); d.Store || d.Reason != "too_large" {
		t.Fatalf("too large decision = %+v", d)
	}
}

func TestSynthesizeOpenAIChatCompletionSSE(t *testing.T) {
	body := []byte(`{"id":"chatcmpl_1","object":"chat.completion","created":123,"model":"gpt-test","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	sse, err := SynthesizeOpenAIChatCompletionSSE(body)
	if err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	text := string(sse)
	if !strings.Contains(text, `"object":"chat.completion.chunk"`) {
		t.Fatalf("missing chunk object: %s", text)
	}
	if !strings.Contains(text, `"content":"hello"`) {
		t.Fatalf("missing content: %s", text)
	}
	if !strings.Contains(text, `"finish_reason":"stop"`) {
		t.Fatalf("missing finish: %s", text)
	}
	if !strings.Contains(text, "data: [DONE]") {
		t.Fatalf("missing DONE: %s", text)
	}
}

func TestSynthesizeOpenAIChatCompletionSSEToolCalls(t *testing.T) {
	body := []byte(`{"id":"chatcmpl_tool","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"x\"}"}}]},"finish_reason":"tool_calls"}]}`)
	sse, err := SynthesizeOpenAIChatCompletionSSE(body)
	if err != nil {
		t.Fatalf("synthesize tool_calls: %v", err)
	}
	text := string(sse)
	if !strings.Contains(text, `"tool_calls"`) {
		t.Fatalf("missing tool_calls in stream: %s", text)
	}
	if !strings.Contains(text, `"name":"read_file"`) {
		t.Fatalf("missing tool name: %s", text)
	}
	if !strings.Contains(text, `"finish_reason":"tool_calls"`) {
		t.Fatalf("missing finish tool_calls: %s", text)
	}
	if !strings.Contains(text, "data: [DONE]") {
		t.Fatalf("missing DONE: %s", text)
	}
}
