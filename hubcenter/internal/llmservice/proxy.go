package llmservice

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

var proxyDebugSecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(authorization\s*[:=]\s*bearer\s+)[^\s,"'}]+`),
	regexp.MustCompile(`(?i)((?:"|')?[\w-]*(?:api[_-]?key|access[_-]?token|accessToken|refresh[_-]?token|refreshToken|secret|password)[\w-]*(?:"|')?\s*[:=]\s*(?:"|')?)[^"',\s}]+`),
}

const proxySystemFreeAliasServiceGroupID = "system-free"

var proxySystemFreeFallbackServiceGroupIDs = []string{"redeem", "maclaw-official"}

// ProxyConfig holds runtime dependencies for the LLM proxy.
type ProxyConfig struct {
	Service     *Service
	AuthChecker *AuthorizationChecker
	Cache       *llmpool.Cache
	Concurrency *llmpool.ConcurrencyController
	Resilience  *llmpool.ResilienceController
	Usage       llmpool.UsageRecorder
	HTTPClient  *http.Client
	NodeID      string // current HubCenter node ID for binding checks

	// CheckBinding validates that this node is the bound node for the tenant.
	// Returns (allowed, redirectNodeID). If nil, binding checks are skipped.
	CheckBinding func(ctx context.Context, hubID, tenantID string) (allowed bool, redirectNodeID string)
}

// ProxyRequest holds the parsed incoming LLM proxy request.
type ProxyRequest struct {
	HubID          string
	TenantID       string
	ServiceGroupID string
	Body           map[string]any
	RawBody        []byte
	Model          string
}

// ProxyResponse holds the result of a proxied LLM request.
type ProxyResponse struct {
	StatusCode   int
	Body         []byte
	ProviderID   string
	InputTokens  int64
	OutputTokens int64
	CacheHit     bool
}

// HandleProxyRequest processes an incoming LLM proxy request from a Hub.
// Flow: auth -> cache check -> provider dispatch -> forward -> record usage -> cache write.
func HandleProxyRequest(ctx context.Context, cfg *ProxyConfig, req *ProxyRequest) (*ProxyResponse, error) {
	if cfg == nil || cfg.Service == nil || cfg.AuthChecker == nil {
		return nil, fmt.Errorf("proxy not configured")
	}
	if req == nil {
		return nil, fmt.Errorf("proxy request is required")
	}
	if req.Body == nil {
		return nil, fmt.Errorf("proxy request body is required")
	}
	ctx = WithUsageContext(ctx, req.HubID, req.TenantID)

	// 1. Extract model from request body
	model := strings.TrimSpace(req.Model)
	if model == "" {
		if m, ok := req.Body["model"].(string); ok {
			model = strings.TrimSpace(m)
		}
	}
	if model == "" {
		return nil, fmt.Errorf("model not specified in request")
	}

	// Force non-streaming: HubCenter proxy returns complete responses.
	// Hub-side handles client-facing SSE streaming independently.
	delete(req.Body, "stream")
	delete(req.Body, "stream_options")

	// 2. Find service group + dispatch model for this model
	// Try to find which service group contains this model
	reg, err := cfg.Service.LoadRegistry(ctx)
	if err != nil {
		return nil, fmt.Errorf("load registry: %w", err)
	}

	matchedGroup, dispatchModel := matchProxyServiceGroupModel(reg, strings.TrimSpace(req.ServiceGroupID), model)
	if matchedGroup == nil {
		return nil, fmt.Errorf("model %q not available on this HubCenter", model)
	}
	if requestedGroupID := strings.TrimSpace(req.ServiceGroupID); requestedGroupID != "" && !strings.EqualFold(requestedGroupID, strings.TrimSpace(matchedGroup.ID)) {
		log.Printf("[llm-proxy] resolved service_group alias requested=%s matched=%s model=%s hub=%s tenant=%s", requestedGroupID, matchedGroup.ID, model, req.HubID, req.TenantID)
	}

	// 3. Check tenant authorization only when this service group requires a card/grant.
	var auth *TenantAuthorization
	requiresGrant := matchedGroup.AccessPolicy == AccessPolicyGrantRequired
	if requiresGrant {
		var err error
		auth, err = cfg.AuthChecker.CheckAccess(ctx, req.HubID, req.TenantID, matchedGroup.ID)
		if err != nil {
			return nil, fmt.Errorf("authorization denied: %w", err)
		}
	}

	// 3.5 Check node binding (HA anti-double-spend)
	if cfg.CheckBinding != nil {
		allowed, redirectNode := cfg.CheckBinding(ctx, req.HubID, req.TenantID)
		if !allowed {
			return nil, fmt.Errorf("tenant bound to node %s, please redirect", redirectNode)
		}
	}

	// 4. Check cache
	if cfg.Cache != nil {
		cacheKey := buildServiceGroupCacheKey(matchedGroup.ID, model, req.Body)
		if cached, _ := cfg.Cache.Get(ctx, cacheKey); cached != nil {
			// Record cache hit usage (no credits deducted)
			if cfg.Usage != nil {
				_ = cfg.Usage.RecordUsage(ctx, &llmpool.UsageRecord{
					ProviderID: cached.ProviderID,
					Model:      model,
					CacheHit:   true,
					Timestamp:  time.Now().UTC(),
				})
			}
			return &ProxyResponse{
				StatusCode: http.StatusOK,
				Body:       stripProxyResponseUsage(cached.Payload),
				ProviderID: cached.ProviderID,
				CacheHit:   true,
			}, nil
		}
	}

	// 5. Order providers by capability match
	orderedRoutes := llmpool.OrderProviderRoutes(req.Body, dispatchModel)
	if len(orderedRoutes) == 0 {
		return nil, fmt.Errorf("no providers configured for model %q", model)
	}

	// 6. Try providers in order (with concurrency + resilience)
	var lastErr error
	for _, route := range orderedRoutes {
		providerID := route.ProviderID
		provider := findProvider(reg, providerID)
		if provider == nil {
			lastErr = fmt.Errorf("provider %s referenced in model but not found in registry", providerID)
			continue
		}

		// Resilience check (circuit breaker)
		if cfg.Resilience != nil {
			if err := cfg.Resilience.BeforeAttempt(providerID, provider.CircuitBreakerThreshold, provider.CircuitBreakerCooldownMS); err != nil {
				lastErr = err
				continue
			}
		}

		// Concurrency control
		var release func()
		if cfg.Concurrency != nil {
			var acqErr error
			release, acqErr = cfg.Concurrency.Acquire(ctx, providerID, provider.MaxConcurrency, provider.MaxQueueWaiters, provider.QueueTimeoutMS)
			if acqErr != nil {
				lastErr = acqErr
				continue
			}
		} else {
			release = func() {}
		}

		// Forward request
		upstreamModel := proxyUpstreamModelForRoute(route, provider, model)
		resp, fwdErr := forwardToProvider(ctx, cfg.HTTPClient, provider, req.Body, upstreamModel, model)
		release()

		if fwdErr != nil || (resp != nil && shouldRetryProxyProviderStatus(resp.StatusCode)) {
			if cfg.Resilience != nil {
				cfg.Resilience.RecordFailure(providerID, provider.CircuitBreakerThreshold, provider.CircuitBreakerCooldownMS)
			}
			if fwdErr != nil {
				lastErr = fmt.Errorf("provider %s failed for logical model %s upstream model %s: %w", providerID, model, upstreamModel, fwdErr)
			} else {
				lastErr = fmt.Errorf("provider %s failed for logical model %s upstream model %s: HTTP %d%s", providerID, model, upstreamModel, resp.StatusCode, proxyProviderErrorSnippet(resp.Body))
			}
			continue
		}

		// Success
		if cfg.Resilience != nil {
			cfg.Resilience.RecordSuccess(providerID)
		}

		if resp.StatusCode >= http.StatusBadRequest {
			return &ProxyResponse{
				StatusCode: resp.StatusCode,
				Body:       resp.Body,
				ProviderID: providerID,
				CacheHit:   false,
			}, nil
		}

		// Parse token usage from response
		inputTokens, outputTokens, respBody := proxyResponseUsageWithFallback(req.Body, resp.Body)
		resp.Body = respBody

		// Calculate credits
		multiplier := proxyCreditMultiplierForRoute(dispatchModel, route)
		if multiplier <= 0 {
			multiplier = 1
		}
		credits := estimateProxyCreditsWithFloor(inputTokens+outputTokens, multiplier)

		// Deduct credits
		var deductions []CreditDeduction
		if requiresGrant && auth != nil {
			var err error
			deductions, err = cfg.AuthChecker.DeductCreditsForServiceGroup(ctx, req.HubID, req.TenantID, matchedGroup.ID, credits)
			if err != nil {
				// Log but don't fail: tokens already consumed upstream.
				// Reconciliation can fix this from usage records.
				log.Printf("[llm-proxy] WARN: credits deduction failed auth=%s group=%s credits=%.2f: %v", auth.ID, matchedGroup.ID, credits, err)
			}
		}
		recordCredits := credits
		if requiresGrant {
			recordCredits = totalDeductionCredits(deductions)
		}

		// Record usage
		if cfg.Usage != nil {
			_ = cfg.Usage.RecordUsage(ctx, &llmpool.UsageRecord{
				ProviderID:   providerID,
				Model:        model,
				InputTokens:  inputTokens,
				OutputTokens: outputTokens,
				Credits:      recordCredits,
				CacheHit:     false,
				AuthID:       deductionAuthIDs(deductions),
				Timestamp:    time.Now().UTC(),
			})
		}

		// Write to cache
		if cfg.Cache != nil && resp.StatusCode == http.StatusOK {
			cacheKey := buildServiceGroupCacheKey(matchedGroup.ID, model, req.Body)
			_ = cfg.Cache.Put(ctx, &llmpool.CacheEntry{
				CacheKey:   cacheKey,
				ProviderID: providerID,
				Model:      model,
				Kind:       "full",
				Payload:    resp.Body,
				CreatedAt:  time.Now().UTC(),
				AccessedAt: time.Now().UTC(),
			})
		}

		return &ProxyResponse{
			StatusCode:   resp.StatusCode,
			Body:         resp.Body,
			ProviderID:   providerID,
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
			CacheHit:     false,
		}, nil
	}

	// All providers failed
	log.Printf("[llm-proxy] all providers failed for model=%s service_group=%s hub=%s tenant=%s lastErr=%v", model, matchedGroup.ID, req.HubID, req.TenantID, lastErr)
	if lastErr != nil {
		return nil, fmt.Errorf("all providers failed, last error: %w", lastErr)
	}
	return nil, fmt.Errorf("no available providers for model %q", model)
}

func proxyProviderErrorSnippet(body []byte) string {
	text := strings.ToValidUTF8(string(body), "\ufffd")
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	text = proxyRedactDebugSecrets(text)
	if text == "" {
		return ""
	}
	if len([]rune(text)) > 500 {
		text = string([]rune(text)[:497]) + "..."
	}
	return ": " + text
}

func proxyRedactDebugSecrets(text string) string {
	for _, pattern := range proxyDebugSecretPatterns {
		text = pattern.ReplaceAllString(text, "${1}[redacted]")
	}
	return text
}

func matchProxyServiceGroupModel(reg *Registry, serviceGroupID, model string) (*llmpool.ServiceGroup, *llmpool.DispatchModel) {
	if reg == nil {
		return nil, nil
	}
	serviceGroupID = strings.TrimSpace(serviceGroupID)
	model = strings.TrimSpace(model)
	if serviceGroupID != "" {
		for i := range reg.ServiceGroups {
			group := &reg.ServiceGroups[i]
			if !strings.EqualFold(strings.TrimSpace(group.ID), serviceGroupID) {
				continue
			}
			if dispatchModel := matchProxyGroupModel(reg, group, model); dispatchModel != nil {
				return group, dispatchModel
			}
			return matchProxySystemFreeFallback(reg, serviceGroupID, model)
		}
		return matchProxySystemFreeFallback(reg, serviceGroupID, model)
	}
	for i := range reg.ServiceGroups {
		group := &reg.ServiceGroups[i]
		if dispatchModel := matchProxyGroupModel(reg, group, model); dispatchModel != nil {
			return group, dispatchModel
		}
	}
	return nil, nil
}

func matchProxyGroupModel(reg *Registry, group *llmpool.ServiceGroup, model string) *llmpool.DispatchModel {
	if group == nil {
		return nil
	}
	for j := range group.Models {
		if strings.TrimSpace(group.Models[j].Name) == model {
			return buildDispatchModel(reg, &group.Models[j])
		}
	}
	return nil
}

func matchProxySystemFreeFallback(reg *Registry, serviceGroupID, model string) (*llmpool.ServiceGroup, *llmpool.DispatchModel) {
	if !isProxySystemFreeAlias(serviceGroupID) {
		return nil, nil
	}
	if group, dispatchModel := matchProxyServiceGroupModelByAccessPolicy(reg, model, AccessPolicyFree); group != nil {
		return group, dispatchModel
	}
	for _, fallbackID := range proxySystemFreeFallbackServiceGroupIDs {
		if group, dispatchModel := matchProxyServiceGroupModelByID(reg, fallbackID, model); group != nil {
			return group, dispatchModel
		}
	}
	return matchProxyServiceGroupModelByAccessPolicy(reg, model, AccessPolicyGrantRequired)
}

func matchProxyServiceGroupModelByID(reg *Registry, serviceGroupID, model string) (*llmpool.ServiceGroup, *llmpool.DispatchModel) {
	serviceGroupID = strings.TrimSpace(serviceGroupID)
	for i := range reg.ServiceGroups {
		group := &reg.ServiceGroups[i]
		if !strings.EqualFold(strings.TrimSpace(group.ID), serviceGroupID) {
			continue
		}
		if dispatchModel := matchProxyGroupModel(reg, group, model); dispatchModel != nil {
			return group, dispatchModel
		}
		return nil, nil
	}
	return nil, nil
}

func matchProxyServiceGroupModelByAccessPolicy(reg *Registry, model, accessPolicy string) (*llmpool.ServiceGroup, *llmpool.DispatchModel) {
	for i := range reg.ServiceGroups {
		group := &reg.ServiceGroups[i]
		if normalizeServiceGroupAccessPolicy(group.AccessPolicy) != accessPolicy {
			continue
		}
		if dispatchModel := matchProxyGroupModel(reg, group, model); dispatchModel != nil {
			return group, dispatchModel
		}
	}
	return nil, nil
}

func isProxySystemFreeAlias(serviceGroupID string) bool {
	return strings.EqualFold(strings.TrimSpace(serviceGroupID), proxySystemFreeAliasServiceGroupID)
}

func shouldRetryProxyProviderStatus(statusCode int) bool {
	return statusCode >= 500 ||
		statusCode == http.StatusNotFound ||
		statusCode == http.StatusUnprocessableEntity ||
		statusCode == http.StatusUnauthorized ||
		statusCode == http.StatusForbidden ||
		statusCode == http.StatusTooManyRequests
}

// ---------------------------------------------------------------------------
// internal helpers
// ---------------------------------------------------------------------------

func findProvider(reg *Registry, id string) *llmpool.ProviderConfig {
	for i, p := range reg.Providers {
		if p.ID == id {
			return &reg.Providers[i]
		}
	}
	return nil
}

type providerForwardResponse struct {
	StatusCode int
	Body       []byte
}

func proxyUpstreamModelForRoute(route llmpool.DispatchProviderRoute, provider *llmpool.ProviderConfig, logicalModel string) string {
	if route.Model != "" {
		return route.Model
	}
	if provider != nil {
		switch len(provider.Models) {
		case 1:
			return provider.Models[0]
		default:
			for _, m := range provider.Models {
				if m == logicalModel {
					return logicalModel
				}
			}
		}
	}
	return logicalModel
}

func forwardToProvider(ctx context.Context, client *http.Client, provider *llmpool.ProviderConfig, body map[string]any, upstreamModel, responseModel string) (*providerForwardResponse, error) {
	if provider == nil {
		return nil, fmt.Errorf("provider is required")
	}
	endpointProvider := corelib.LLMEndpointProvider{
		ID:                       provider.ID,
		Name:                     provider.Name,
		APIURL:                   provider.APIURL,
		APIKey:                   provider.APIKey,
		Model:                    upstreamModel,
		Protocol:                 provider.Protocol,
		WireAPI:                  provider.WireAPI,
		UpstreamTimeoutSec:       provider.UpstreamTimeoutSec,
		MaxConcurrency:           provider.MaxConcurrency,
		MaxQueueWaiters:          provider.MaxQueueWaiters,
		QueueTimeoutMS:           provider.QueueTimeoutMS,
		CircuitBreakerThreshold:  provider.CircuitBreakerThreshold,
		CircuitBreakerCooldownMS: provider.CircuitBreakerCooldownMS,
		FailureBackoffBaseMS:     provider.FailureBackoffBaseMS,
		FailureBackoffMaxMS:      provider.FailureBackoffMaxMS,
	}
	respBody, statusCode, err := corelib.ForwardLLMEndpointProviderRequest(ctx, endpointProvider, body, client, responseModel)
	if err != nil {
		return nil, fmt.Errorf("forward to %s: %w", provider.ID, err)
	}
	return &providerForwardResponse{
		StatusCode: statusCode,
		Body:       respBody,
	}, nil
}

func extractTokenUsage(respBody []byte) (inputTokens, outputTokens int64) {
	var payload map[string]any
	if err := json.Unmarshal(respBody, &payload); err != nil {
		return 0, 0
	}
	usage, _ := payload["usage"].(map[string]any)
	if usage == nil {
		return 0, 0
	}
	return extractTokenUsageFromMap(usage)
}

func proxyResponseUsageWithFallback(reqBody map[string]any, respBody []byte) (inputTokens, outputTokens int64, patchedBody []byte) {
	patchedBody = respBody
	inputTokens, outputTokens = extractTokenUsage(respBody)
	if inputTokens > 0 && outputTokens > 0 {
		return inputTokens, outputTokens, patchedBody
	}

	estimatedInput, estimatedOutput := estimateProxyTokenUsage(reqBody, respBody)
	if inputTokens == 0 && outputTokens == 0 {
		inputTokens, outputTokens = estimatedInput, estimatedOutput
		if inputTokens > 0 || outputTokens > 0 {
			patchedBody = ensureProxyResponseUsage(respBody, inputTokens, outputTokens)
		}
		return inputTokens, outputTokens, patchedBody
	}

	if inputTokens == 0 {
		inputTokens = estimatedInput
	}
	if outputTokens == 0 {
		outputTokens = estimatedOutput
	}
	if inputTokens > 0 && outputTokens > 0 {
		patchedBody = completeProxyResponseUsage(respBody, inputTokens, outputTokens)
	}
	return inputTokens, outputTokens, patchedBody
}
func estimateProxyTokenUsage(reqBody map[string]any, respBody []byte) (inputTokens, outputTokens int64) {
	if reqBody != nil {
		for _, key := range []string{"messages", "input", "instructions", "tools", "tool_choice", "response_format"} {
			if value, ok := reqBody[key]; ok {
				inputTokens += estimateProxyRequestTokens(key, value)
			}
		}
	}
	outputTokens = estimateProxyResponseTokens(respBody)
	return inputTokens, outputTokens
}

func estimateProxyRequestTokens(key string, v any) int64 {
	switch key {
	case "tools", "tool_choice", "response_format":
		return estimateProxyJSONTokens(v)
	default:
		return estimateProxyAnyTokens(v)
	}
}

func estimateProxyAnyTokens(v any) int64 {
	if text := strings.TrimSpace(flattenProxyText(v)); text != "" {
		return int64(corelib.EstimateTextTokens(text))
	}
	return estimateProxyJSONTokens(v)
}

func estimateProxyJSONTokens(v any) int64 {
	data, err := json.Marshal(v)
	if err != nil {
		return 0
	}
	return int64(corelib.EstimateTextTokens(string(data)))
}

func estimateProxyResponseTokens(respBody []byte) int64 {
	var payload map[string]any
	if err := json.Unmarshal(respBody, &payload); err != nil {
		return int64(corelib.EstimateTextTokens(string(respBody)))
	}
	var text strings.Builder
	appendPart := func(part string) {
		part = strings.TrimSpace(part)
		if part == "" {
			return
		}
		text.WriteString(part)
		text.WriteByte('\n')
	}
	if choices, ok := payload["choices"].([]any); ok {
		for _, item := range choices {
			choice, _ := item.(map[string]any)
			message, _ := choice["message"].(map[string]any)
			appendPart(flattenProxyText(message["content"]))
			appendPart(flattenProxyText(message["tool_calls"]))
			appendPart(flattenProxyText(message["function_call"]))
			appendPart(flattenProxyText(choice["text"]))
		}
	}
	appendPart(flattenProxyText(payload["output"]))
	if text.Len() == 0 {
		if data, err := json.Marshal(payload); err == nil {
			return int64(corelib.EstimateTextTokens(string(data)))
		}
	}
	return int64(corelib.EstimateTextTokens(text.String()))
}

func flattenProxyText(v any) string {
	switch val := v.(type) {
	case nil:
		return ""
	case string:
		return val
	case []any:
		parts := make([]string, 0, len(val))
		for _, item := range val {
			parts = append(parts, flattenProxyText(item))
		}
		return strings.Join(parts, " ")
	case map[string]any:
		parts := make([]string, 0, len(val))
		for _, key := range []string{"text", "content", "input", "output", "input_text", "output_text", "arguments", "name", "description", "function", "parameters", "schema"} {
			parts = append(parts, flattenProxyText(val[key]))
		}
		return strings.Join(parts, " ")
	default:
		return ""
	}
}

func completeProxyResponseUsage(respBody []byte, inputTokens, outputTokens int64) []byte {
	var payload map[string]any
	if err := json.Unmarshal(respBody, &payload); err != nil {
		return respBody
	}
	usage, _ := payload["usage"].(map[string]any)
	if usage == nil {
		return ensureProxyResponseUsage(respBody, inputTokens, outputTokens)
	}
	if usageNumber(usage["prompt_tokens"]) == 0 && usageNumber(usage["input_tokens"]) == 0 {
		usage["prompt_tokens"] = inputTokens
	}
	if usageNumber(usage["completion_tokens"]) == 0 && usageNumber(usage["output_tokens"]) == 0 {
		usage["completion_tokens"] = outputTokens
	}
	if usageNumber(usage["total_tokens"]) == 0 {
		usage["total_tokens"] = inputTokens + outputTokens
	}
	usage["estimated"] = true
	data, err := json.Marshal(payload)
	if err != nil {
		return respBody
	}
	return data
}

func ensureProxyResponseUsage(respBody []byte, inputTokens, outputTokens int64) []byte {
	var payload map[string]any
	if err := json.Unmarshal(respBody, &payload); err != nil {
		return respBody
	}
	if usage, ok := payload["usage"].(map[string]any); ok {
		in, out := extractTokenUsageFromMap(usage)
		if in > 0 || out > 0 {
			return respBody
		}
	}
	payload["usage"] = map[string]any{
		"prompt_tokens":     inputTokens,
		"completion_tokens": outputTokens,
		"total_tokens":      inputTokens + outputTokens,
		"estimated":         true,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return respBody
	}
	return data
}

func stripProxyResponseUsage(respBody []byte) []byte {
	var payload map[string]any
	if err := json.Unmarshal(respBody, &payload); err != nil {
		return respBody
	}
	if _, ok := payload["usage"]; !ok {
		return respBody
	}
	delete(payload, "usage")
	data, err := json.Marshal(payload)
	if err != nil {
		return respBody
	}
	return data
}

func extractTokenUsageFromMap(usage map[string]any) (inputTokens, outputTokens int64) {
	inputTokens = firstPositiveInt64(usageNumber(usage["prompt_tokens"]), usageNumber(usage["input_tokens"]))
	outputTokens = firstPositiveInt64(usageNumber(usage["completion_tokens"]), usageNumber(usage["output_tokens"]))
	totalTokens := usageNumber(usage["total_tokens"])
	if totalTokens > 0 {
		switch {
		case inputTokens == 0 && outputTokens == 0:
			inputTokens = totalTokens
		case inputTokens == 0 && totalTokens > outputTokens:
			inputTokens = totalTokens - outputTokens
		case outputTokens == 0 && totalTokens > inputTokens:
			outputTokens = totalTokens - inputTokens
		}
	}
	return inputTokens, outputTokens
}

func firstPositiveInt64(values ...int64) int64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func usageNumber(v any) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int64:
		return n
	case float64:
		return int64(n)
	case json.Number:
		return usageJSONNumber(n)
	case string:
		return usageJSONNumber(json.Number(strings.TrimSpace(n)))
	default:
		return 0
	}
}

func usageJSONNumber(n json.Number) int64 {
	if i, err := n.Int64(); err == nil {
		return i
	}
	if f, err := n.Float64(); err == nil {
		return int64(f)
	}
	return 0
}

func buildCacheKey(model string, body map[string]any) string {
	return buildServiceGroupCacheKey("", model, body)
}

func buildServiceGroupCacheKey(serviceGroupID, model string, body map[string]any) string {
	// Normalize: remove non-deterministic fields, canonicalize defaults
	normalized := make(map[string]any)
	for k, v := range body {
		switch k {
		case "stream", "stream_options", "user", "n":
			// Skip non-deterministic / stream-related fields
			continue
		case "temperature":
			// Normalize: 0 and absent are semantically equivalent for caching
			if f, ok := v.(float64); ok && f == 0 {
				continue
			}
		default:
			normalized[k] = v
		}
	}
	if serviceGroupID != "" {
		normalized["service_group_id"] = serviceGroupID
	}
	normalized["model"] = model
	data, _ := json.Marshal(normalized)
	h := fmt.Sprintf("%x", sha256Bytes(data))
	if serviceGroupID != "" {
		return "llm:" + serviceGroupID + ":" + model + ":" + h[:16]
	}
	return "llm:" + model + ":" + h[:16]
}

func sha256Bytes(data []byte) [32]byte {
	return sha256.Sum256(data)
}

func proxyCreditMultiplier(model *llmpool.DispatchModel, providerID string) float64 {
	if model == nil {
		return 1
	}
	if model.ProviderCreditMultipliers != nil {
		if m, ok := model.ProviderCreditMultipliers[providerID]; ok && m > 0 {
			return m
		}
	}
	if model.CreditMultiplier > 0 {
		return model.CreditMultiplier
	}
	return 1
}

func proxyCreditMultiplierForRoute(model *llmpool.DispatchModel, route llmpool.DispatchProviderRoute) float64 {
	if route.CreditMultiplier > 0 {
		return route.CreditMultiplier
	}
	if model != nil && len(model.ProviderRoutes) > 0 {
		if model.CreditMultiplier > 0 {
			return model.CreditMultiplier
		}
		return 1
	}
	return proxyCreditMultiplier(model, route.ProviderID)
}

const (
	defaultProxyTokensPerCredit = 10000
	minimumProxyRequestCredits  = 0.1
)

func estimateProxyCreditsWithFloor(tokens int64, multiplier float64) float64 {
	credits := estimateProxyCredits(tokens, multiplier)
	if credits < minimumProxyRequestCredits {
		return minimumProxyRequestCredits
	}
	return credits
}

func estimateProxyCredits(tokens int64, multiplier float64) float64 {
	if tokens <= 0 {
		return 0
	}
	if multiplier <= 0 {
		multiplier = 1
	}
	return roundProxyCredits((float64(tokens) * multiplier) / defaultProxyTokensPerCredit)
}

func roundProxyCredits(v float64) float64 {
	return math.Round(v*1000) / 1000
}

func deductionAuthIDs(deductions []CreditDeduction) string {
	if len(deductions) == 0 {
		return ""
	}
	ids := make([]string, 0, len(deductions))
	for _, deduction := range deductions {
		id := strings.TrimSpace(deduction.AuthID)
		if id != "" {
			ids = append(ids, id)
		}
	}
	return strings.Join(ids, ",")
}

func totalDeductionCredits(deductions []CreditDeduction) float64 {
	total := 0.0
	for _, deduction := range deductions {
		if deduction.Credits > 0 {
			total += deduction.Credits
		}
	}
	return roundProxyCredits(total)
}
