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
		logLocalLLMCacheSkip(cfg, "openai", "unsupported_or_disabled")
		return llm.DoOpenAIRequest(ctx, cfg, messages, tools, client)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	bodyMap, key, err := localLLMCacheOpenAIKey(cfg, messages, tools, cacheCfg)
	if err != nil {
		logLocalLLMCacheSkip(cfg, "openai", "key_error:"+err.Error())
		return llm.DoOpenAIRequest(ctx, cfg, messages, tools, client)
	}
	if decision := corelib.LLMPromptCacheable(bodyMap, cacheCfg.Options()); !decision.Cacheable {
		logLocalLLMCacheSkip(cfg, "openai", "uncacheable:"+decision.Reason)
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
			logLocalLLMCacheDecision(cfg, "openai", key, "hit")
			return parsed, nil
		}
		cache.Delete(key)
		logLocalLLMCacheDecision(cfg, "openai", key, "invalid_hit_deleted")
	}
	logLocalLLMCacheDecision(cfg, "openai", key, "miss")
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
	if shared && stored {
		logLocalLLMCacheDecision(cfg, "openai", key, "shared_hit")
	} else if stored {
		logLocalLLMCacheDecision(cfg, "openai", key, "stored")
	} else {
		logLocalLLMCacheDecision(cfg, "openai", key, "store_skipped")
	}
	return parsed, nil
}

func (a *App) cachedAnthropicRequest(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, tools []map[string]interface{}, client *http.Client) (*llm.Response, error) {
	cacheCfg, ok := a.effectiveLLMPromptCacheConfig()
	if !ok || !localLLMCacheSupportsAnthropic(cfg, cacheCfg) {
		logLocalLLMCacheSkip(cfg, "anthropic", "unsupported_or_disabled")
		return llm.DoAnthropicRequest(ctx, cfg, messages, tools, client)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	bodyMap, key, err := localLLMCacheAnthropicKey(cfg, messages, tools, cacheCfg, false)
	if err != nil {
		logLocalLLMCacheSkip(cfg, "anthropic", "key_error:"+err.Error())
		return llm.DoAnthropicRequest(ctx, cfg, messages, tools, client)
	}
	if decision := corelib.LLMPromptCacheable(bodyMap, cacheCfg.Options()); !decision.Cacheable {
		logLocalLLMCacheSkip(cfg, "anthropic", "uncacheable:"+decision.Reason)
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
			logLocalLLMCacheDecision(cfg, "anthropic", key, "hit")
			return parsed, nil
		}
		cache.Delete(key)
		logLocalLLMCacheDecision(cfg, "anthropic", key, "invalid_hit_deleted")
	}
	logLocalLLMCacheDecision(cfg, "anthropic", key, "miss")
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
	if shared && stored {
		logLocalLLMCacheDecision(cfg, "anthropic", key, "shared_hit")
	} else if stored {
		logLocalLLMCacheDecision(cfg, "anthropic", key, "stored")
	} else {
		logLocalLLMCacheDecision(cfg, "anthropic", key, "store_skipped")
	}
	return parsed, nil
}

func (a *App) cachedStreamHit(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, tools []map[string]interface{}, onToken llm.TokenCallback) (*llm.Response, bool) {
	select {
	case <-ctx.Done():
		return nil, false
	default:
	}
	cacheCfg, ok := a.effectiveLLMPromptCacheConfig()
	if !ok || !cacheCfg.EffectiveStreamSynthesisEnabled() || isHubServiceProviderName(cfg.ProviderName) || cfg.IsResponsesAPI() || cfg.IsResponsesWebSocket() {
		logLocalLLMCacheSkip(cfg, "stream", localLLMCacheStreamDisabledReason(cfg, ok, cacheCfg))
		return nil, false
	}
	var bodyMap map[string]any
	var key string
	var err error
	if strings.EqualFold(strings.TrimSpace(cfg.Protocol), "anthropic") {
		if !localLLMCacheSupportsAnthropicProtocol(cfg) {
			logLocalLLMCacheSkip(cfg, "stream", "unsupported_anthropic")
			return nil, false
		}
		bodyMap, key, err = localLLMCacheAnthropicKey(cfg, messages, tools, cacheCfg, false)
	} else {
		if !localLLMCacheSupportsOpenAIProtocol(cfg) {
			logLocalLLMCacheSkip(cfg, "stream", "unsupported_openai")
			return nil, false
		}
		bodyMap, key, err = localLLMCacheOpenAIKey(cfg, messages, tools, cacheCfg)
	}
	if err != nil {
		logLocalLLMCacheSkip(cfg, "stream", "key_error:"+err.Error())
		return nil, false
	}
	if decision := corelib.LLMPromptCacheable(bodyMap, cacheCfg.Options()); !decision.Cacheable {
		logLocalLLMCacheSkip(cfg, "stream", "uncacheable:"+decision.Reason)
		return nil, false
	}
	body, hit := a.ensureLocalLLMCache(cacheCfg).Get(key, cacheCfg)
	if !hit {
		logLocalLLMCacheDecision(cfg, "stream", key, "miss")
		return nil, false
	}
	parsed, err := localLLMCacheParseStoredResponse(body)
	if err != nil {
		a.ensureLocalLLMCache(cacheCfg).Delete(key)
		logLocalLLMCacheDecision(cfg, "stream", key, "invalid_hit_deleted")
		return nil, false
	}
	parsed.LocalCacheHit = true
	select {
	case <-ctx.Done():
		return nil, false
	default:
	}
	a.recordLocalLLMCacheRequest(cfg, true)
	logLocalLLMCacheDecision(cfg, "stream", key, "hit")
	if onToken != nil && len(parsed.Choices) > 0 && parsed.Choices[0].Message.Content != "" && len(parsed.Choices[0].Message.ToolCalls) == 0 {
		onToken(parsed.Choices[0].Message.Content)
	}
	return parsed, true
}

func (a *App) storeStreamResponse(cfg corelib.MaclawLLMConfig, messages []interface{}, tools []map[string]interface{}, resp *llm.Response) {
	if resp == nil || responseHasToolCalls(resp) || isHubServiceProviderName(cfg.ProviderName) || cfg.IsResponsesAPI() || cfg.IsResponsesWebSocket() {
		logLocalLLMCacheSkip(cfg, "stream", localLLMCacheStoreIneligibleReason(cfg, resp))
		return
	}
	cacheCfg, ok := a.effectiveLLMPromptCacheConfig()
	if !ok || !cacheCfg.EffectiveStreamSynthesisEnabled() {
		logLocalLLMCacheSkip(cfg, "stream", "store_disabled")
		return
	}
	var bodyMap map[string]any
	var key string
	var err error
	if strings.EqualFold(strings.TrimSpace(cfg.Protocol), "anthropic") {
		if !localLLMCacheSupportsAnthropicProtocol(cfg) {
			logLocalLLMCacheSkip(cfg, "stream", "store_unsupported_anthropic")
			return
		}
		bodyMap, key, err = localLLMCacheAnthropicKey(cfg, messages, tools, cacheCfg, false)
	} else {
		if !localLLMCacheSupportsOpenAIProtocol(cfg) {
			logLocalLLMCacheSkip(cfg, "stream", "store_unsupported_openai")
			return
		}
		bodyMap, key, err = localLLMCacheOpenAIKey(cfg, messages, tools, cacheCfg)
	}
	if err != nil {
		logLocalLLMCacheSkip(cfg, "stream", "store_key_error:"+err.Error())
		return
	}
	if decision := corelib.LLMPromptCacheable(bodyMap, cacheCfg.Options()); !decision.Cacheable {
		logLocalLLMCacheSkip(cfg, "stream", "store_uncacheable:"+decision.Reason)
		return
	}
	body, err := marshalLocalLLMCacheResponse(resp)
	if err != nil {
		logLocalLLMCacheSkip(cfg, "stream", "store_invalid_response:"+err.Error())
		return
	}
	if a.ensureLocalLLMCache(cacheCfg).Set(key, body, cacheCfg) {
		a.recordLocalLLMCacheRequest(cfg, false)
		logLocalLLMCacheDecision(cfg, "stream", key, "stored")
	} else {
		logLocalLLMCacheDecision(cfg, "stream", key, "store_failed")
	}
}

func logLocalLLMCacheDecision(cfg corelib.MaclawLLMConfig, protocol string, key string, event string) {
	log.Printf("[llm_cache] event=%s protocol=%s configured_protocol=%q wire_api=%q provider=%q model=%q base_url_hash=%s key=%s", event, protocol, strings.TrimSpace(cfg.Protocol), strings.TrimSpace(cfg.WireAPI), strings.TrimSpace(cfg.ProviderName), strings.TrimSpace(cfg.Model), shortLocalLLMCacheURLHash(cfg.URL), shortLocalLLMCacheKey(key))
}

func logLocalLLMCacheSkip(cfg corelib.MaclawLLMConfig, protocol string, reason string) {
	log.Printf("[llm_cache] event=skip protocol=%s configured_protocol=%q wire_api=%q provider=%q model=%q base_url_hash=%s responses_api=%t responses_ws=%t reason=%s", protocol, strings.TrimSpace(cfg.Protocol), strings.TrimSpace(cfg.WireAPI), strings.TrimSpace(cfg.ProviderName), strings.TrimSpace(cfg.Model), shortLocalLLMCacheURLHash(cfg.URL), cfg.IsResponsesAPI(), cfg.IsResponsesWebSocket(), sanitizeLocalLLMCacheLogValue(reason))
}

func localLLMCacheStreamDisabledReason(cfg corelib.MaclawLLMConfig, cfgLoaded bool, cacheCfg corelib.LLMPromptCacheConfig) string {
	switch {
	case !cfgLoaded:
		return "config_disabled_or_unavailable"
	case !cacheCfg.EffectiveStreamSynthesisEnabled():
		return "stream_synthesis_disabled"
	case isHubServiceProviderName(cfg.ProviderName):
		return "official_hub_bypass"
	case cfg.IsResponsesAPI():
		return "responses_api_unsupported"
	case cfg.IsResponsesWebSocket():
		return "responses_ws_unsupported"
	default:
		return "unsupported_or_disabled"
	}
}

func localLLMCacheStoreIneligibleReason(cfg corelib.MaclawLLMConfig, resp *llm.Response) string {
	switch {
	case resp == nil:
		return "nil_response"
	case responseHasToolCalls(resp):
		return "tool_calls_not_cached"
	case isHubServiceProviderName(cfg.ProviderName):
		return "official_hub_bypass"
	case cfg.IsResponsesAPI():
		return "responses_api_unsupported"
	case cfg.IsResponsesWebSocket():
		return "responses_ws_unsupported"
	default:
		return "store_response_ineligible"
	}
}

func shortLocalLLMCacheURLHash(rawURL string) string {
	rawURL = strings.TrimRight(strings.TrimSpace(rawURL), "/")
	if rawURL == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(rawURL))
	return hex.EncodeToString(sum[:])[:12]
}

func shortLocalLLMCacheKey(key string) string {
	key = strings.TrimSpace(key)
	if len(key) <= 21 {
		return key
	}
	if strings.HasPrefix(key, "llm_resp_") {
		return key[:21]
	}
	return key[:12]
}

func sanitizeLocalLLMCacheLogValue(value string) string {
	value = strings.NewReplacer("\r", " ", "\n", " ", "\t", " ").Replace(strings.TrimSpace(value))
	if len(value) > 160 {
		return value[:160]
	}
	return value
}

func (a *App) recordLocalLLMCacheRequest(cfg corelib.MaclawLLMConfig, hit bool) {
	provider := strings.TrimSpace(cfg.ProviderName)
	if provider == "" {
		provider = strings.TrimSpace(cfg.Model)
	}
	if provider == "" || isHubServiceProviderName(provider) {
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
	if isHubServiceProviderName(cfg.ProviderName) {
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
	if isHubServiceProviderName(cfg.ProviderName) {
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
