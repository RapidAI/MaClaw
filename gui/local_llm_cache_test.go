package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func localLLMCacheTestConfig(t *testing.T) corelib.LLMPromptCacheConfig {
	t.Helper()
	cfg := corelib.DefaultLLMPromptCacheConfig()
	cfg.CacheDir = t.TempDir()
	return cfg
}

func TestLocalLLMCacheCachesOpenAICompatibleRequest(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":"hit-%d"}}]}`, calls.Load())
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	cfg := localLLMCacheTestConfig(t)
	cfg.Enabled = true
	if err := app.SaveConfig(corelib.AppConfig{LLMPromptCache: cfg}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	llmCfg := corelib.MaclawLLMConfig{URL: server.URL, Key: "test", Model: "test-model", Protocol: "openai", ProviderName: "local"}
	messages := []interface{}{map[string]interface{}{"role": "user", "content": "hello"}}

	first, err := app.cachedOpenAIRequest(context.Background(), llmCfg, messages, nil, server.Client())
	if err != nil {
		t.Fatalf("first cachedOpenAIRequest: %v", err)
	}
	second, err := app.cachedOpenAIRequest(context.Background(), llmCfg, messages, nil, server.Client())
	if err != nil {
		t.Fatalf("second cachedOpenAIRequest: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("server calls = %d, want 1", calls.Load())
	}
	if first.Choices[0].Message.Content != "hit-1" || second.Choices[0].Message.Content != "hit-1" {
		t.Fatalf("contents = %q/%q, want cached hit-1", first.Choices[0].Message.Content, second.Choices[0].Message.Content)
	}
	usage := app.GetLLMTokenUsage("local")
	if usage.LocalCacheRequests != 2 || usage.LocalCacheHits != 1 {
		t.Fatalf("local cache usage requests=%d hits=%d, want 2/1", usage.LocalCacheRequests, usage.LocalCacheHits)
	}
}

func TestLocalLLMCacheBypassesUnsupportedProtocolsAndHub(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	cfg := localLLMCacheTestConfig(t)
	cfg.Enabled = true
	if err := app.SaveConfig(corelib.AppConfig{LLMPromptCache: cfg}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	cases := []struct {
		name string
		cfg  corelib.MaclawLLMConfig
	}{
		{name: "responses-ws", cfg: corelib.MaclawLLMConfig{Protocol: "openai", WireAPI: "responses-ws"}},
		{name: "hub", cfg: corelib.MaclawLLMConfig{Protocol: "openai", ProviderName: hubServiceProviderName}},
		{name: "hub_anthropic", cfg: corelib.MaclawLLMConfig{Protocol: "anthropic", ProviderName: hubServiceProviderName}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if localLLMCacheSupportsOpenAI(tc.cfg, cfg) || localLLMCacheSupportsAnthropic(tc.cfg, cfg) {
				t.Fatalf("localLLMCacheSupports(%+v) = true, want false", tc.cfg)
			}
		})
	}
}

func TestLocalLLMCacheCachesAnthropicRequest(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"content":[{"type":"text","text":"anthropic-%d"}],"stop_reason":"end_turn"}`, calls.Load())
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	cfg := localLLMCacheTestConfig(t)
	cfg.Enabled = true
	if err := app.SaveConfig(corelib.AppConfig{LLMPromptCache: cfg}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	llmCfg := corelib.MaclawLLMConfig{URL: server.URL, Key: "test", Model: "claude-test", Protocol: "anthropic", ProviderName: "local"}
	messages := []interface{}{map[string]interface{}{"role": "user", "content": "hello"}}

	first, err := app.cachedAnthropicRequest(context.Background(), llmCfg, messages, nil, server.Client())
	if err != nil {
		t.Fatalf("first cachedAnthropicRequest: %v", err)
	}
	second, err := app.cachedAnthropicRequest(context.Background(), llmCfg, messages, nil, server.Client())
	if err != nil {
		t.Fatalf("second cachedAnthropicRequest: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("server calls = %d, want 1", calls.Load())
	}
	if first.Choices[0].Message.Content != "anthropic-1" || second.Choices[0].Message.Content != "anthropic-1" {
		t.Fatalf("contents = %q/%q, want cached anthropic-1", first.Choices[0].Message.Content, second.Choices[0].Message.Content)
	}
	usage := app.GetLLMTokenUsage("local")
	if usage.LocalCacheRequests != 2 || usage.LocalCacheHits != 1 {
		t.Fatalf("local cache usage requests=%d hits=%d, want 2/1", usage.LocalCacheRequests, usage.LocalCacheHits)
	}
}

func TestLocalLLMCacheSynthesizesStreamHit(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	cfg := localLLMCacheTestConfig(t)
	cfg.Enabled = true
	if err := app.SaveConfig(corelib.AppConfig{LLMPromptCache: cfg}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	llmCfg := corelib.MaclawLLMConfig{URL: "http://127.0.0.1:1", Key: "test", Model: "test-model", Protocol: "openai", ProviderName: "local"}
	messages := []interface{}{map[string]interface{}{"role": "user", "content": "hello"}}
	app.storeStreamResponse(llmCfg, messages, nil, &llm.Response{Choices: []llm.Choice{{Message: llm.Message{Role: "assistant", Content: "cached stream"}, FinishReason: "stop"}}})

	var streamed string
	resp, ok := app.cachedStreamHit(context.Background(), llmCfg, messages, nil, func(delta string) { streamed += delta })
	if !ok {
		t.Fatal("cachedStreamHit ok = false, want true")
	}
	if streamed != "cached stream" || resp.Choices[0].Message.Content != "cached stream" {
		t.Fatalf("streamed/resp = %q/%q", streamed, resp.Choices[0].Message.Content)
	}
	usage := app.GetLLMTokenUsage("local")
	if usage.LocalCacheRequests != 2 || usage.LocalCacheHits != 1 {
		t.Fatalf("local cache usage requests=%d hits=%d, want 2/1", usage.LocalCacheRequests, usage.LocalCacheHits)
	}
}

func TestLocalLLMCacheStreamHitRespectsCanceledContext(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	cfg := localLLMCacheTestConfig(t)
	cfg.Enabled = true
	if err := app.SaveConfig(corelib.AppConfig{LLMPromptCache: cfg}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	llmCfg := corelib.MaclawLLMConfig{URL: "http://127.0.0.1:1", Key: "test", Model: "test-model", Protocol: "openai", ProviderName: "local"}
	messages := []interface{}{map[string]interface{}{"role": "user", "content": "hello"}}
	app.storeStreamResponse(llmCfg, messages, nil, &llm.Response{Choices: []llm.Choice{{Message: llm.Message{Role: "assistant", Content: "cached stream"}, FinishReason: "stop"}}})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var streamed string
	if resp, ok := app.cachedStreamHit(ctx, llmCfg, messages, nil, func(delta string) { streamed += delta }); ok || resp != nil {
		t.Fatalf("cachedStreamHit canceled = %+v/%v, want nil/false", resp, ok)
	}
	if streamed != "" {
		t.Fatalf("streamed after cancel = %q, want empty", streamed)
	}
}

func TestLocalLLMCacheStreamSynthesisSwitchDisablesStreamStore(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	streamOff := false
	cfg := localLLMCacheTestConfig(t)
	cfg.Enabled = true
	cfg.StreamSynthesisEnabled = &streamOff
	if err := app.SaveConfig(corelib.AppConfig{LLMPromptCache: cfg}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	llmCfg := corelib.MaclawLLMConfig{URL: "http://127.0.0.1:1", Key: "test", Model: "test-model", Protocol: "openai", ProviderName: "local"}
	messages := []interface{}{map[string]interface{}{"role": "user", "content": "hello"}}
	app.storeStreamResponse(llmCfg, messages, nil, &llm.Response{Choices: []llm.Choice{{Message: llm.Message{Role: "assistant", Content: "cached stream"}, FinishReason: "stop"}}})

	if _, ok := app.cachedStreamHit(context.Background(), llmCfg, messages, nil, nil); ok {
		t.Fatal("cachedStreamHit ok = true, want false when stream synthesis is disabled")
	}
	usage := app.GetLLMTokenUsage("local")
	if usage.LocalCacheRequests != 0 || usage.LocalCacheHits != 0 {
		t.Fatalf("local cache usage requests=%d hits=%d, want 0/0", usage.LocalCacheRequests, usage.LocalCacheHits)
	}
}

func TestLocalLLMCacheStreamSynthesisIndependentFromNonStreamProtocolSwitches(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	off := false
	on := true
	cfg := localLLMCacheTestConfig(t)
	cfg.Enabled = true
	cfg.OpenAIEnabled = &off
	cfg.AnthropicEnabled = &off
	cfg.StreamSynthesisEnabled = &on
	if err := app.SaveConfig(corelib.AppConfig{LLMPromptCache: cfg}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	llmCfg := corelib.MaclawLLMConfig{URL: "http://127.0.0.1:1", Key: "test", Model: "test-model", Protocol: "openai", ProviderName: "local"}
	messages := []interface{}{map[string]interface{}{"role": "user", "content": "hello"}}
	app.storeStreamResponse(llmCfg, messages, nil, &llm.Response{Choices: []llm.Choice{{Message: llm.Message{Role: "assistant", Content: "cached stream"}, FinishReason: "stop"}}})

	var streamed string
	resp, ok := app.cachedStreamHit(context.Background(), llmCfg, messages, nil, func(delta string) { streamed += delta })
	if !ok {
		t.Fatal("cachedStreamHit ok = false, want true when stream synthesis is enabled")
	}
	if streamed != "cached stream" || resp.Choices[0].Message.Content != "cached stream" {
		t.Fatalf("streamed/resp = %q/%q", streamed, resp.Choices[0].Message.Content)
	}
}

func TestLocalLLMCacheDeletesUnparseableHitAndRefetches(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"fresh"}}]}`)
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	cfg := localLLMCacheTestConfig(t)
	cfg.Enabled = true
	if err := app.SaveConfig(corelib.AppConfig{LLMPromptCache: cfg}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	llmCfg := corelib.MaclawLLMConfig{URL: server.URL, Key: "test", Model: "test-model", Protocol: "openai", ProviderName: "local"}
	messages := []interface{}{map[string]interface{}{"role": "user", "content": "hello"}}
	_, key, err := localLLMCacheOpenAIKey(llmCfg, messages, nil, cfg)
	if err != nil {
		t.Fatalf("localLLMCacheOpenAIKey: %v", err)
	}
	app.ensureLocalLLMCache(cfg).Set(key, []byte(`not-json`), cfg)

	resp, err := app.cachedOpenAIRequest(context.Background(), llmCfg, messages, nil, server.Client())
	if err != nil {
		t.Fatalf("cachedOpenAIRequest: %v", err)
	}
	if calls.Load() != 1 || resp.Choices[0].Message.Content != "fresh" {
		t.Fatalf("calls/content = %d/%q, want 1/fresh", calls.Load(), resp.Choices[0].Message.Content)
	}
}

func TestLocalLLMCacheNonStreamHitRespectsCanceledContext(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"cached"}}]}`)
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	cfg := localLLMCacheTestConfig(t)
	cfg.Enabled = true
	if err := app.SaveConfig(corelib.AppConfig{LLMPromptCache: cfg}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	llmCfg := corelib.MaclawLLMConfig{URL: server.URL, Key: "test", Model: "test-model", Protocol: "openai", ProviderName: "local"}
	messages := []interface{}{map[string]interface{}{"role": "user", "content": "hello"}}
	if _, err := app.cachedOpenAIRequest(context.Background(), llmCfg, messages, nil, server.Client()); err != nil {
		t.Fatalf("prime cache: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	resp, err := app.cachedOpenAIRequest(ctx, llmCfg, messages, nil, server.Client())
	if err == nil || resp != nil {
		t.Fatalf("canceled cache hit = %+v/%v, want nil/error", resp, err)
	}
	if calls.Load() != 1 {
		t.Fatalf("server calls = %d, want 1", calls.Load())
	}
	usage := app.GetLLMTokenUsage("local")
	if usage.LocalCacheRequests != 1 || usage.LocalCacheHits != 0 {
		t.Fatalf("local cache usage requests=%d hits=%d, want 1/0", usage.LocalCacheRequests, usage.LocalCacheHits)
	}
}

func TestLocalLLMCacheRejectsUsageOnlyCachedResponse(t *testing.T) {
	if _, err := localLLMCacheParseStoredResponse([]byte(`{"usage":{"input_tokens":1}}`)); err == nil {
		t.Fatal("localLLMCacheParseStoredResponse usage-only response error = nil, want invalid response error")
	}
}

func TestLocalLLMCacheDoesNotFailOrStoreUsageOnlyNonStreamResponse(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"usage":{"input_tokens":1}}`)
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	cfg := localLLMCacheTestConfig(t)
	cfg.Enabled = true
	if err := app.SaveConfig(corelib.AppConfig{LLMPromptCache: cfg}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	llmCfg := corelib.MaclawLLMConfig{URL: server.URL, Key: "test", Model: "test-model", Protocol: "openai", ProviderName: "local"}
	messages := []interface{}{map[string]interface{}{"role": "user", "content": "hello"}}

	first, err := app.cachedOpenAIRequest(context.Background(), llmCfg, messages, nil, server.Client())
	if err != nil {
		t.Fatalf("first cachedOpenAIRequest: %v", err)
	}
	second, err := app.cachedOpenAIRequest(context.Background(), llmCfg, messages, nil, server.Client())
	if err != nil {
		t.Fatalf("second cachedOpenAIRequest: %v", err)
	}
	if len(first.Choices) != 0 || len(second.Choices) != 0 {
		t.Fatalf("choices = %d/%d, want 0/0", len(first.Choices), len(second.Choices))
	}
	if calls.Load() != 2 {
		t.Fatalf("server calls = %d, want 2", calls.Load())
	}
	usage := app.GetLLMTokenUsage("local")
	if usage.LocalCacheRequests != 2 || usage.LocalCacheHits != 0 {
		t.Fatalf("local cache usage requests=%d hits=%d, want 2/0", usage.LocalCacheRequests, usage.LocalCacheHits)
	}
}

func TestLocalLLMCacheDoesNotStoreUsageOnlyStreamResponse(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	cfg := localLLMCacheTestConfig(t)
	cfg.Enabled = true
	if err := app.SaveConfig(corelib.AppConfig{LLMPromptCache: cfg}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	llmCfg := corelib.MaclawLLMConfig{URL: "http://127.0.0.1:1", Key: "test", Model: "test-model", Protocol: "openai", ProviderName: "local"}
	messages := []interface{}{map[string]interface{}{"role": "user", "content": "hello"}}
	app.storeStreamResponse(llmCfg, messages, nil, &llm.Response{Usage: &llm.Usage{InputTokens: 1}})

	if _, ok := app.cachedStreamHit(context.Background(), llmCfg, messages, nil, nil); ok {
		t.Fatal("cachedStreamHit ok = true, want false for usage-only response")
	}
	usage := app.GetLLMTokenUsage("local")
	if usage.LocalCacheRequests != 0 || usage.LocalCacheHits != 0 {
		t.Fatalf("local cache usage requests=%d hits=%d, want 0/0", usage.LocalCacheRequests, usage.LocalCacheHits)
	}
}

func TestLocalLLMCacheDoesNotRecordUnstoredStreamResponse(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	cfg := localLLMCacheTestConfig(t)
	cfg.Enabled = true
	cfg.MemoryMaxBytes = 1
	cfg.DiskMaxBytes = 1
	if err := app.SaveConfig(corelib.AppConfig{LLMPromptCache: cfg}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	llmCfg := corelib.MaclawLLMConfig{URL: "http://127.0.0.1:1", Key: "test", Model: "test-model", Protocol: "openai", ProviderName: "local"}
	messages := []interface{}{map[string]interface{}{"role": "user", "content": "hello"}}
	app.storeStreamResponse(llmCfg, messages, nil, &llm.Response{Choices: []llm.Choice{{Message: llm.Message{Role: "assistant", Content: "cached stream"}, FinishReason: "stop"}}})

	if _, ok := app.cachedStreamHit(context.Background(), llmCfg, messages, nil, nil); ok {
		t.Fatal("cachedStreamHit ok = true, want false for response over cache limits")
	}
	usage := app.GetLLMTokenUsage("local")
	if usage.LocalCacheRequests != 0 || usage.LocalCacheHits != 0 {
		t.Fatalf("local cache usage requests=%d hits=%d, want 0/0", usage.LocalCacheRequests, usage.LocalCacheHits)
	}
}

func TestLocalLLMCacheIgnoreModelFieldAffectsScope(t *testing.T) {
	cfg := localLLMCacheTestConfig(t).WithDefaults()
	cfg.IgnoreModelField = true
	base := corelib.MaclawLLMConfig{URL: "http://local.test/v1", Key: "test", Model: "model-a", Protocol: "openai", ProviderName: "local"}
	messages := []interface{}{map[string]interface{}{"role": "user", "content": "hello"}}
	_, keyA, err := localLLMCacheOpenAIKey(base, messages, nil, cfg)
	if err != nil {
		t.Fatalf("keyA: %v", err)
	}
	base.Model = "model-b"
	_, keyB, err := localLLMCacheOpenAIKey(base, messages, nil, cfg)
	if err != nil {
		t.Fatalf("keyB: %v", err)
	}
	if keyA != keyB {
		t.Fatalf("keys with ignored model differ: %q vs %q", keyA, keyB)
	}

	cfg.IgnoreModelField = false
	base.Model = "model-a"
	_, keyA, err = localLLMCacheOpenAIKey(base, messages, nil, cfg)
	if err != nil {
		t.Fatalf("strict keyA: %v", err)
	}
	base.Model = "model-b"
	_, keyB, err = localLLMCacheOpenAIKey(base, messages, nil, cfg)
	if err != nil {
		t.Fatalf("strict keyB: %v", err)
	}
	if keyA == keyB {
		t.Fatalf("keys with model included should differ: %q", keyA)
	}
}

func TestLocalLLMCacheScopeKeepsURLPathCaseDistinct(t *testing.T) {
	cfg := localLLMCacheTestConfig(t).WithDefaults()
	base := corelib.MaclawLLMConfig{URL: "http://local.test/v1/CasePath", Key: "test", Model: "model-a", Protocol: "openai", ProviderName: "local"}
	messages := []interface{}{map[string]interface{}{"role": "user", "content": "hello"}}
	_, keyA, err := localLLMCacheOpenAIKey(base, messages, nil, cfg)
	if err != nil {
		t.Fatalf("keyA: %v", err)
	}
	base.URL = "http://local.test/v1/casepath"
	_, keyB, err := localLLMCacheOpenAIKey(base, messages, nil, cfg)
	if err != nil {
		t.Fatalf("keyB: %v", err)
	}
	if keyA == keyB {
		t.Fatalf("keys for case-distinct URL paths should differ: %q", keyA)
	}
}

func TestLocalLLMCacheAllProtocolSwitchesOffDisablesGlobal(t *testing.T) {
	off := false
	cfg := corelib.LLMPromptCacheConfig{Enabled: true, OpenAIEnabled: &off, AnthropicEnabled: &off, StreamSynthesisEnabled: &off}.WithDefaults()
	if cfg.Enabled {
		t.Fatalf("Enabled = true, want false when all protocol switches are off")
	}
}

func TestLocalLLMCacheUsesConfiguredCacheDir(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	cacheDir := t.TempDir()
	cfg := localLLMCacheTestConfig(t)
	cfg.Enabled = true
	cfg.CacheDir = cacheDir
	if err := app.SaveConfig(corelib.AppConfig{LLMPromptCache: cfg}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	llmCfg := corelib.MaclawLLMConfig{URL: "http://127.0.0.1:1", Key: "test", Model: "test-model", Protocol: "openai", ProviderName: "local"}
	messages := []interface{}{map[string]interface{}{"role": "user", "content": "hello"}}
	app.storeStreamResponse(llmCfg, messages, nil, &llm.Response{Choices: []llm.Choice{{Message: llm.Message{Role: "assistant", Content: "cached"}, FinishReason: "stop"}}})
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		t.Fatalf("ReadDir(%q): %v", cacheDir, err)
	}
	if len(entries) == 0 {
		t.Fatalf("configured cache dir %q is empty", cacheDir)
	}
}

func TestSaveConfigMigratesLLMCacheDirOnChange(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	oldDir := t.TempDir()
	newDir := t.TempDir()
	cfg := localLLMCacheTestConfig(t)
	cfg.CacheDir = oldDir
	if err := app.SaveConfig(corelib.AppConfig{LLMPromptCache: cfg}); err != nil {
		t.Fatalf("SaveConfig old: %v", err)
	}
	corelib.NewLLMPromptResponseCache(oldDir).Set("llm_resp_existing", []byte("cached"), cfg)
	cfg.CacheDir = newDir
	if err := app.SaveConfig(corelib.AppConfig{LLMPromptCache: cfg}); err != nil {
		t.Fatalf("SaveConfig new: %v", err)
	}
	got, ok := corelib.NewLLMPromptResponseCache(newDir).Get("llm_resp_existing", cfg)
	if !ok || string(got) != "cached" {
		t.Fatalf("migrated cache = %q/%v, want cached/true", got, ok)
	}
}

func TestLocalLLMCacheDisabledByDefault(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	cacheCfg, ok := app.effectiveLLMPromptCacheConfig()
	if ok {
		t.Fatalf("effectiveLLMPromptCacheConfig enabled = true, want false")
	}
	if cacheCfg.TTLSeconds != 1800 || cacheCfg.MemoryMaxEntries != 256 {
		t.Fatalf("defaults = %+v", cacheCfg)
	}
}
