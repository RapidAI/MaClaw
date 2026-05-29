package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func (a *App) cachedOpenAIRequest(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, tools []map[string]interface{}, client *http.Client) (*llm.Response, error) {
	cacheCfg, ok := a.effectiveLLMPromptCacheConfig()
	if !ok || !localLLMCacheSupportsOpenAI(cfg, cacheCfg) {
		return llm.DoOpenAIRequest(ctx, cfg, messages, tools, client)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	bodyMap, key, err := localLLMCacheOpenAIKey(cfg, messages, tools, cacheCfg)
	if err != nil {
		return llm.DoOpenAIRequest(ctx, cfg, messages, tools, client)
	}
	if decision := corelib.LLMPromptCacheable(bodyMap, cacheCfg.Options()); !decision.Cacheable {
		return llm.DoOpenAIRequest(ctx, cfg, messages, tools, client)
	}
	cache := a.ensureLocalLLMCache(cacheCfg)
	if body, ok := cache.Get(key, cacheCfg); ok {
		parsed, parseErr := localLLMCacheParseStoredResponse(body)
		if parseErr == nil {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			parsed.LocalCacheHit = true
			a.recordLocalLLMCacheRequest(cfg, true)
			return parsed, nil
		}
		cache.Delete(key)
	}
	body, shared, stored, err := cache.DoSingleflightWithSharedStore(ctx, key, cacheCfg, func() ([]byte, bool, error) {
		parsed, reqErr := llm.DoOpenAIRequest(ctx, cfg, messages, tools, client)
		if reqErr != nil {
			return nil, false, reqErr
		}
		body, marshalErr := marshalLocalLLMCacheResponse(parsed)
		if marshalErr == nil {
			return body, true, nil
		}
		body, marshalErr = json.Marshal(parsed)
		return body, false, marshalErr
	})
	if err != nil {
		return nil, err
	}
	parsed, err := localLLMCacheParseResponse(body)
	if err != nil {
		return nil, err
	}
	parsed.LocalCacheHit = shared && stored
	a.recordLocalLLMCacheRequest(cfg, shared && stored)
	return parsed, nil
}

func (a *App) cachedAnthropicRequest(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, tools []map[string]interface{}, client *http.Client) (*llm.Response, error) {
	cacheCfg, ok := a.effectiveLLMPromptCacheConfig()
	if !ok || !localLLMCacheSupportsAnthropic(cfg, cacheCfg) {
		return llm.DoAnthropicRequest(ctx, cfg, messages, tools, client)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	bodyMap, key, err := localLLMCacheAnthropicKey(cfg, messages, tools, cacheCfg, false)
	if err != nil {
		return llm.DoAnthropicRequest(ctx, cfg, messages, tools, client)
	}
	if decision := corelib.LLMPromptCacheable(bodyMap, cacheCfg.Options()); !decision.Cacheable {
		return llm.DoAnthropicRequest(ctx, cfg, messages, tools, client)
	}
	cache := a.ensureLocalLLMCache(cacheCfg)
	if body, ok := cache.Get(key, cacheCfg); ok {
		parsed, parseErr := localLLMCacheParseStoredResponse(body)
		if parseErr == nil {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			parsed.LocalCacheHit = true
			a.recordLocalLLMCacheRequest(cfg, true)
			return parsed, nil
		}
		cache.Delete(key)
	}
	body, shared, stored, err := cache.DoSingleflightWithSharedStore(ctx, key, cacheCfg, func() ([]byte, bool, error) {
		parsed, reqErr := llm.DoAnthropicRequest(ctx, cfg, messages, tools, client)
		if reqErr != nil {
			return nil, false, reqErr
		}
		body, marshalErr := marshalLocalLLMCacheResponse(parsed)
		if marshalErr == nil {
			return body, true, nil
		}
		body, marshalErr = json.Marshal(parsed)
		return body, false, marshalErr
	})
	if err != nil {
		return nil, err
	}
	parsed, err := localLLMCacheParseResponse(body)
	if err != nil {
		return nil, err
	}
	parsed.LocalCacheHit = shared && stored
	a.recordLocalLLMCacheRequest(cfg, shared && stored)
	return parsed, nil
}

func (a *App) cachedStreamHit(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, tools []map[string]interface{}, onToken llm.TokenCallback) (*llm.Response, bool) {
	select {
	case <-ctx.Done():
		return nil, false
	default:
	}
	cacheCfg, ok := a.effectiveLLMPromptCacheConfig()
	if !ok || !cacheCfg.EffectiveStreamSynthesisEnabled() || strings.TrimSpace(cfg.ProviderName) == hubServiceProviderName || cfg.IsResponsesAPI() || cfg.IsResponsesWebSocket() {
		return nil, false
	}
	var bodyMap map[string]any
	var key string
	var err error
	if strings.EqualFold(strings.TrimSpace(cfg.Protocol), "anthropic") {
		if !localLLMCacheSupportsAnthropicProtocol(cfg) {
			return nil, false
		}
		bodyMap, key, err = localLLMCacheAnthropicKey(cfg, messages, tools, cacheCfg, false)
	} else {
		if !localLLMCacheSupportsOpenAIProtocol(cfg) {
			return nil, false
		}
		bodyMap, key, err = localLLMCacheOpenAIKey(cfg, messages, tools, cacheCfg)
	}
	if err != nil || !corelib.LLMPromptCacheable(bodyMap, cacheCfg.Options()).Cacheable {
		return nil, false
	}
	body, hit := a.ensureLocalLLMCache(cacheCfg).Get(key, cacheCfg)
	if !hit {
		return nil, false
	}
	parsed, err := localLLMCacheParseStoredResponse(body)
	if err != nil {
		a.ensureLocalLLMCache(cacheCfg).Delete(key)
		return nil, false
	}
	parsed.LocalCacheHit = true
	select {
	case <-ctx.Done():
		return nil, false
	default:
	}
	a.recordLocalLLMCacheRequest(cfg, true)
	if onToken != nil && len(parsed.Choices) > 0 && parsed.Choices[0].Message.Content != "" && len(parsed.Choices[0].Message.ToolCalls) == 0 {
		onToken(parsed.Choices[0].Message.Content)
	}
	return parsed, true
}

func (a *App) storeStreamResponse(cfg corelib.MaclawLLMConfig, messages []interface{}, tools []map[string]interface{}, resp *llm.Response) {
	if resp == nil || responseHasToolCalls(resp) || strings.TrimSpace(cfg.ProviderName) == hubServiceProviderName || cfg.IsResponsesAPI() || cfg.IsResponsesWebSocket() {
		return
	}
	cacheCfg, ok := a.effectiveLLMPromptCacheConfig()
	if !ok || !cacheCfg.EffectiveStreamSynthesisEnabled() {
		return
	}
	var bodyMap map[string]any
	var key string
	var err error
	if strings.EqualFold(strings.TrimSpace(cfg.Protocol), "anthropic") {
		if !localLLMCacheSupportsAnthropicProtocol(cfg) {
			return
		}
		bodyMap, key, err = localLLMCacheAnthropicKey(cfg, messages, tools, cacheCfg, false)
	} else {
		if !localLLMCacheSupportsOpenAIProtocol(cfg) {
			return
		}
		bodyMap, key, err = localLLMCacheOpenAIKey(cfg, messages, tools, cacheCfg)
	}
	if err != nil || !corelib.LLMPromptCacheable(bodyMap, cacheCfg.Options()).Cacheable {
		return
	}
	body, err := marshalLocalLLMCacheResponse(resp)
	if err != nil {
		return
	}
	if a.ensureLocalLLMCache(cacheCfg).Set(key, body, cacheCfg) {
		a.recordLocalLLMCacheRequest(cfg, false)
	}
}

func (a *App) recordLocalLLMCacheRequest(cfg corelib.MaclawLLMConfig, hit bool) {
	provider := strings.TrimSpace(cfg.ProviderName)
	if provider == "" {
		provider = strings.TrimSpace(cfg.Model)
	}
	if provider == "" || provider == hubServiceProviderName {
		return
	}
	a.AccumulateLLMLocalCacheRequest(provider, hit)
}

func (a *App) effectiveLLMPromptCacheConfig() (corelib.LLMPromptCacheConfig, bool) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return corelib.LLMPromptCacheConfig{}, false
	}
	cacheCfg := cfg.LLMPromptCache.WithDefaults()
	return cacheCfg, cacheCfg.Enabled
}

func (a *App) ensureLocalLLMCache(cacheCfg corelib.LLMPromptCacheConfig) *corelib.LLMPromptResponseCache {
	dir := cacheCfg.EffectiveCacheDir()
	a.llmPromptCacheMu.Lock()
	defer a.llmPromptCacheMu.Unlock()
	if a.llmPromptCache == nil || a.llmPromptCacheDir != dir {
		a.llmPromptCache = corelib.NewLLMPromptResponseCache(dir)
		a.llmPromptCacheDir = dir
	}
	return a.llmPromptCache
}

func migrateLLMPromptCacheDirIfNeeded(oldCfg, newCfg corelib.LLMPromptCacheConfig) error {
	oldDir := oldCfg.WithDefaults().EffectiveCacheDir()
	newDir := newCfg.WithDefaults().EffectiveCacheDir()
	copied, err := corelib.MigrateLLMPromptResponseCacheDir(oldDir, newDir)
	if err != nil {
		return err
	}
	if copied > 0 {
		log.Printf("[llm_cache] migrated %d cache files from %q to %q", copied, oldDir, newDir)
	}
	return nil
}

func localLLMCacheSupportsOpenAI(cfg corelib.MaclawLLMConfig, cacheCfg corelib.LLMPromptCacheConfig) bool {
	return cacheCfg.EffectiveOpenAIEnabled() && localLLMCacheSupportsOpenAIProtocol(cfg)
}

func localLLMCacheSupportsOpenAIProtocol(cfg corelib.MaclawLLMConfig) bool {
	protocol := strings.ToLower(strings.TrimSpace(cfg.Protocol))
	if protocol != "" && protocol != "openai" {
		return false
	}
	if cfg.IsResponsesAPI() || cfg.IsResponsesWebSocket() {
		return false
	}
	if strings.TrimSpace(cfg.ProviderName) == hubServiceProviderName {
		return false
	}
	return true
}

func localLLMCacheSupportsAnthropic(cfg corelib.MaclawLLMConfig, cacheCfg corelib.LLMPromptCacheConfig) bool {
	return cacheCfg.EffectiveAnthropicEnabled() && localLLMCacheSupportsAnthropicProtocol(cfg)
}

func localLLMCacheSupportsAnthropicProtocol(cfg corelib.MaclawLLMConfig) bool {
	if !strings.EqualFold(strings.TrimSpace(cfg.Protocol), "anthropic") || cfg.IsResponsesAPI() || cfg.IsResponsesWebSocket() {
		return false
	}
	if strings.TrimSpace(cfg.ProviderName) == hubServiceProviderName {
		return false
	}
	return true
}

func localLLMCacheOpenAIKey(cfg corelib.MaclawLLMConfig, messages []interface{}, tools []map[string]interface{}, cacheCfg corelib.LLMPromptCacheConfig) (map[string]any, string, error) {
	_, data, err := llm.BuildOpenAIChatRequestData(cfg, messages, llm.OpenAIChatRequestOptions{Stream: false, Tools: tools})
	if err != nil {
		return nil, "", err
	}
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, "", err
	}
	scope := localLLMCacheScope("openai", cfg, cacheCfg)
	key, _, err := corelib.LLMPromptCacheKey(scope, cfg.Model, body, cacheCfg.Options())
	return body, key, err
}

func localLLMCacheAnthropicKey(cfg corelib.MaclawLLMConfig, messages []interface{}, tools []map[string]interface{}, cacheCfg corelib.LLMPromptCacheConfig, stream bool) (map[string]any, string, error) {
	_, data, err := llm.BuildAnthropicMessagesRequestData(cfg, messages, llm.AnthropicMessagesRequestOptions{Stream: stream, Tools: tools})
	if err != nil {
		return nil, "", err
	}
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, "", err
	}
	scope := localLLMCacheScope("anthropic", cfg, cacheCfg)
	key, _, err := corelib.LLMPromptCacheKey(scope, cfg.Model, body, cacheCfg.Options())
	return body, key, err
}

func localLLMCacheScope(protocol string, cfg corelib.MaclawLLMConfig, cacheCfg corelib.LLMPromptCacheConfig) string {
	urlHash := sha256.Sum256([]byte(strings.TrimRight(strings.TrimSpace(cfg.URL), "/")))
	parts := []string{strings.ToLower(strings.TrimSpace(protocol)), strings.TrimSpace(cfg.ProviderName), hex.EncodeToString(urlHash[:])}
	if !cacheCfg.IgnoreModelField {
		parts = append(parts, strings.TrimSpace(cfg.Model))
	}
	return strings.Join(parts, "|")
}

func localLLMCacheParseResponse(body []byte) (*llm.Response, error) {
	var parsed llm.Response
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	return &parsed, nil
}

func localLLMCacheParseStoredResponse(body []byte) (*llm.Response, error) {
	parsed, err := localLLMCacheParseResponse(body)
	if err != nil {
		return nil, err
	}
	if len(parsed.Choices) == 0 {
		return nil, corelib.ErrLLMPromptCacheInvalidResponse
	}
	return parsed, nil
}

func marshalLocalLLMCacheResponse(resp *llm.Response) ([]byte, error) {
	if resp == nil || len(resp.Choices) == 0 {
		return nil, corelib.ErrLLMPromptCacheInvalidResponse
	}
	return json.Marshal(resp)
}
