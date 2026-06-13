package llmservice

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

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
	HubID    string
	TenantID string
	Body     map[string]any
	RawBody  []byte
	Model    string
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
// Flow: auth → cache check → provider dispatch → forward → record usage → cache write.
func HandleProxyRequest(ctx context.Context, cfg *ProxyConfig, req *ProxyRequest) (*ProxyResponse, error) {
	if cfg == nil || cfg.Service == nil || cfg.AuthChecker == nil {
		return nil, fmt.Errorf("proxy not configured")
	}

	// 1. Extract model from request body
	model := req.Model
	if model == "" {
		if m, ok := req.Body["model"].(string); ok {
			model = m
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

	var matchedGroup *llmpool.ServiceGroup
	var dispatchModel *llmpool.DispatchModel
	for i, group := range reg.ServiceGroups {
		for _, m := range group.Models {
			if m.Name == model {
				matchedGroup = &reg.ServiceGroups[i]
				dispatchModel = buildDispatchModel(reg, &m)
				break
			}
		}
		if matchedGroup != nil {
			break
		}
	}
	if matchedGroup == nil {
		return nil, fmt.Errorf("model %q not available on this HubCenter", model)
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
		cacheKey := buildCacheKey(model, req.Body)
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
				Body:       cached.Payload,
				ProviderID: cached.ProviderID,
				CacheHit:   true,
			}, nil
		}
	}

	// 5. Order providers by capability match
	orderedProviderIDs := llmpool.OrderProviders(req.Body, dispatchModel)
	if len(orderedProviderIDs) == 0 {
		return nil, fmt.Errorf("no providers configured for model %q", model)
	}

	// 6. Try providers in order (with concurrency + resilience)
	var lastErr error
	for _, providerID := range orderedProviderIDs {
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
		resp, fwdErr := forwardToProvider(ctx, cfg.HTTPClient, provider, req.Body, model)
		release()

		if fwdErr != nil || (resp != nil && resp.StatusCode >= 500) {
			if cfg.Resilience != nil {
				cfg.Resilience.RecordFailure(providerID, provider.CircuitBreakerThreshold, provider.CircuitBreakerCooldownMS)
			}
			if fwdErr != nil {
				lastErr = fwdErr
			} else {
				lastErr = fmt.Errorf("provider %s returned HTTP %d", providerID, resp.StatusCode)
			}
			continue
		}

		// Success
		if cfg.Resilience != nil {
			cfg.Resilience.RecordSuccess(providerID)
		}

		// Parse token usage from response
		inputTokens, outputTokens := extractTokenUsage(resp.Body)

		// Calculate credits
		multiplier := proxyCreditMultiplier(dispatchModel, providerID)
		if multiplier <= 0 {
			multiplier = 1
		}
		credits := float64(inputTokens+outputTokens) * multiplier / 10000 // default: 10000 tokens per credit

		// Deduct credits
		if requiresGrant && auth != nil {
			if err := cfg.AuthChecker.DeductCredits(ctx, auth.ID, credits); err != nil {
				// Log but don't fail — tokens already consumed upstream.
				// Reconciliation can fix this from usage records.
				log.Printf("[llm-proxy] WARN: credits deduction failed auth=%s credits=%.2f: %v", auth.ID, credits, err)
			}
		}

		// Record usage
		if cfg.Usage != nil {
			_ = cfg.Usage.RecordUsage(ctx, &llmpool.UsageRecord{
				ProviderID:   providerID,
				Model:        model,
				InputTokens:  inputTokens,
				OutputTokens: outputTokens,
				Credits:      credits,
				CacheHit:     false,
				Timestamp:    time.Now().UTC(),
			})
		}

		// Write to cache
		if cfg.Cache != nil && resp.StatusCode == http.StatusOK {
			cacheKey := buildCacheKey(model, req.Body)
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
	log.Printf("[llm-proxy] all providers failed for model=%s hub=%s tenant=%s lastErr=%v", model, req.HubID, req.TenantID, lastErr)
	if lastErr != nil {
		return nil, fmt.Errorf("all providers failed, last error: %w", lastErr)
	}
	return nil, fmt.Errorf("no available providers for model %q", model)
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

func forwardToProvider(ctx context.Context, client *http.Client, provider *llmpool.ProviderConfig, body map[string]any, model string) (*providerForwardResponse, error) {
	if client == nil {
		client = &http.Client{Timeout: 120 * time.Second}
	}

	// Per-request timeout based on provider config (default 120s)
	timeout := 120 * time.Second
	if provider.UpstreamTimeoutSec > 0 {
		timeout = time.Duration(provider.UpstreamTimeoutSec) * time.Second
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Override model in body if provider has a specific model mapping
	reqBody := make(map[string]any)
	for k, v := range body {
		reqBody[k] = v
	}
	// Keep original model name — provider should handle it

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	apiURL := provider.APIURL
	if !strings.HasSuffix(apiURL, "/chat/completions") && !strings.HasSuffix(apiURL, "/v1/messages") {
		trimmed := strings.TrimRight(apiURL, "/")
		if strings.HasSuffix(trimmed, "/v1") {
			apiURL = trimmed + "/chat/completions"
		} else {
			apiURL = trimmed + "/v1/chat/completions"
		}
	}

	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, apiURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if provider.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+provider.APIKey)
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("forward to %s: %w", provider.ID, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response from %s: %w", provider.ID, err)
	}

	return &providerForwardResponse{
		StatusCode: resp.StatusCode,
		Body:       respBody,
	}, nil
}

func extractTokenUsage(respBody []byte) (inputTokens, outputTokens int64) {
	var parsed struct {
		Usage struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(respBody, &parsed); err == nil {
		return parsed.Usage.PromptTokens, parsed.Usage.CompletionTokens
	}
	return 0, 0
}

func buildCacheKey(model string, body map[string]any) string {
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
	normalized["model"] = model
	data, _ := json.Marshal(normalized)
	h := fmt.Sprintf("%x", sha256Bytes(data))
	return "llm:" + model + ":" + h[:16]
}

func sha256Bytes(data []byte) [32]byte {
	return sha256.Sum256(data)
}

func proxyCreditMultiplier(model *llmpool.DispatchModel, providerID string) float64 {
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
