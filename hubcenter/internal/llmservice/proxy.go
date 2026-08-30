package llmservice

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	corellm "github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

var proxyDebugSecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(authorization\s*[:=]\s*bearer\s+)[^\s,"'}]+`),
	regexp.MustCompile(`(?i)((?:"|')?[\w-]*(?:api[_-]?key|access[_-]?token|accessToken|refresh[_-]?token|refreshToken|secret|password)[\w-]*(?:"|')?\s*[:=]\s*(?:"|')?)[^"',\s}]+`),
}

const proxySystemFreeAliasServiceGroupID = "system-free"

var proxyComputeFallbackServiceGroupIDs = []string{"redeem", "maclaw-official"}

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

	// LookupNodeURL maps a HubCenter node ID to its client-facing base URL.
	// Used to populate 409 redirect_url so Hub can jump to the tenant owner.
	LookupNodeURL func(nodeID string) string

	// Quotes pins one Hub-issued official request to a single HubCenter route
	// and the provider's resolved time-of-use price. Nil keeps compatibility for
	// direct/internal callers, while the public HTTP endpoint fails closed when
	// a supplied quote is invalid.
	Quotes *ProxyQuoteStore
	// Attempts retains the final, non-sensitive billing fact for quoted official
	// requests. Hub uses it only to settle an already-sent reservation after a
	// response/trailer was lost in transit.
	Attempts *ProxyBillingAttemptStore
}

const proxyQuoteTTL = 2 * time.Minute

const proxyBillingAttemptTTL = 30 * time.Minute

// A completed upstream call must remain reconcilable even if the client has
// already disconnected and canceled its HTTP request context.
const proxyBillingAttemptPersistenceTimeout = 5 * time.Second

// ProxyBillingAttempt is the authenticated reconciliation fact for one
// completed official attempt. It deliberately excludes quote tokens, prompts,
// completions and provider credentials.
type ProxyBillingAttempt struct {
	HubID           string                       `json:"hub_id"`
	TenantID        string                       `json:"tenant_id"`
	RequestID       string                       `json:"request_id"`
	StatusCode      int                          `json:"status_code"`
	ProviderID      string                       `json:"provider_id"`
	PricingSnapshot llmpool.TokenPricingSnapshot `json:"pricing_snapshot"`
	CompletedAt     time.Time                    `json:"completed_at"`
}

// ProxyBillingAttemptRepository keeps reconciliation facts available across a
// HubCenter restart. Records are deliberately first-write-wins: the Hub's sent
// reservation can only be settled from the first completed upstream attempt.
// Implementations must not apply a time-based retention policy unless Hub has
// acknowledged that the corresponding reservation was settled.
type ProxyBillingAttemptRepository interface {
	RecordProxyBillingAttempt(ctx context.Context, attempt ProxyBillingAttempt) (inserted bool, err error)
	GetProxyBillingAttempt(ctx context.Context, hubID, tenantID, requestID string) (ProxyBillingAttempt, bool, error)
}

type ProxyBillingAttemptStore struct {
	mu         sync.Mutex
	items      map[string]ProxyBillingAttempt
	repository ProxyBillingAttemptRepository
}

func NewProxyBillingAttemptStore(repository ...ProxyBillingAttemptRepository) *ProxyBillingAttemptStore {
	var durable ProxyBillingAttemptRepository
	if len(repository) > 0 {
		durable = repository[0]
	}
	return &ProxyBillingAttemptStore{items: make(map[string]ProxyBillingAttempt), repository: durable}
}

func proxyBillingAttemptKey(hubID, tenantID, requestID string) string {
	return strings.ToLower(strings.TrimSpace(hubID)) + "\x00" + strings.ToLower(strings.TrimSpace(tenantID)) + "\x00" + strings.TrimSpace(requestID)
}

func validProxyBillingAttempt(attempt ProxyBillingAttempt) bool {
	return strings.TrimSpace(attempt.HubID) != "" && strings.TrimSpace(attempt.TenantID) != "" &&
		strings.TrimSpace(attempt.RequestID) != "" && strings.TrimSpace(attempt.ProviderID) != "" && !attempt.CompletedAt.IsZero()
}

// RecordContext durably records the immutable attempt before retaining the
// short-lived in-process cache. Callers must surface an error: swallowing it
// after the upstream request completed would make a lost response impossible
// for Hub to reconcile after this process restarts.
func (s *ProxyBillingAttemptStore) RecordContext(ctx context.Context, attempt ProxyBillingAttempt) error {
	if s == nil || strings.TrimSpace(attempt.HubID) == "" || strings.TrimSpace(attempt.TenantID) == "" || strings.TrimSpace(attempt.RequestID) == "" || strings.TrimSpace(attempt.ProviderID) == "" || attempt.CompletedAt.IsZero() {
		return nil
	}
	if s.repository != nil {
		inserted, err := s.repository.RecordProxyBillingAttempt(ctx, attempt)
		if err != nil {
			return err
		}
		if !inserted {
			// A prior process (or a duplicate request) already owns the immutable
			// fact. Cache that canonical value rather than the new candidate.
			stored, ok, err := s.repository.GetProxyBillingAttempt(ctx, attempt.HubID, attempt.TenantID, attempt.RequestID)
			if err != nil {
				return err
			}
			if ok {
				attempt = stored
			}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	for key, item := range s.items {
		if !item.CompletedAt.Add(proxyBillingAttemptTTL).After(now) {
			delete(s.items, key)
		}
	}
	// A billing attempt is the final, HubCenter-authenticated reconciliation
	// fact for one Hub request ID. Keep the first completed attempt so a retry
	// or an accidental duplicate execution can never rewrite the amount that
	// Hub uses to settle its already-sent reservation.
	key := proxyBillingAttemptKey(attempt.HubID, attempt.TenantID, attempt.RequestID)
	if _, exists := s.items[key]; exists {
		return nil
	}
	s.items[key] = attempt
	return nil
}

// Record is retained for direct and test callers which do not have a request
// context. Production proxy paths use RecordContext and log persistence errors.
func (s *ProxyBillingAttemptStore) Record(attempt ProxyBillingAttempt) {
	_ = s.RecordContext(context.Background(), attempt)
}

func (s *ProxyBillingAttemptStore) Get(hubID, tenantID, requestID string) (ProxyBillingAttempt, bool) {
	attempt, ok, _ := s.GetContext(context.Background(), hubID, tenantID, requestID)
	return attempt, ok
}

func (s *ProxyBillingAttemptStore) GetContext(ctx context.Context, hubID, tenantID, requestID string) (ProxyBillingAttempt, bool, error) {
	if s == nil {
		return ProxyBillingAttempt{}, false, nil
	}
	s.mu.Lock()
	key := proxyBillingAttemptKey(hubID, tenantID, requestID)
	attempt, ok := s.items[key]
	if ok && attempt.CompletedAt.Add(proxyBillingAttemptTTL).After(time.Now().UTC()) {
		s.mu.Unlock()
		return attempt, true, nil
	}
	if ok {
		delete(s.items, key)
	}
	s.mu.Unlock()
	if s.repository == nil {
		return ProxyBillingAttempt{}, false, nil
	}
	attempt, ok, err := s.repository.GetProxyBillingAttempt(ctx, hubID, tenantID, requestID)
	if err != nil || !ok {
		return ProxyBillingAttempt{}, ok, err
	}
	if !validProxyBillingAttempt(attempt) {
		return ProxyBillingAttempt{}, false, fmt.Errorf("durable billing attempt is invalid")
	}
	s.mu.Lock()
	s.items[key] = attempt
	s.mu.Unlock()
	return attempt, true, nil
}

// ProxyQuote is a HubCenter-owned, short-lived dispatch commitment. The token
// authorizing it is opaque and lives only in ProxyQuoteStore; this public
// snapshot is safe to return to Hub for admission control and audit.
type ProxyQuote struct {
	Token              string                       `json:"-"`
	RequestDigest      string                       `json:"-"`
	Claimed            bool                         `json:"-"`
	HubID              string                       `json:"hub_id"`
	TenantID           string                       `json:"tenant_id"`
	RequestID          string                       `json:"request_id"`
	ServiceGroupID     string                       `json:"service_group_id"`
	LogicalModel       string                       `json:"logical_model"`
	ProviderID         string                       `json:"provider_id"`
	UpstreamModel      string                       `json:"upstream_model"`
	Pricing            llmpool.ResolvedTokenPricing `json:"pricing"`
	ProviderMultiplier float64                      `json:"provider_multiplier"`
	ExpiresAt          time.Time                    `json:"expires_at"`
}

// ProxyQuoteStore is intentionally process-local: HubCenter node binding
// already routes a tenant to one owner. A quote is never regenerated from a
// token; missing, expired, mismatched, or already-claimed quotes are rejected.
type ProxyQuoteStore struct {
	mu    sync.Mutex
	items map[string]ProxyQuote
}

func NewProxyQuoteStore() *ProxyQuoteStore {
	return &ProxyQuoteStore{items: make(map[string]ProxyQuote)}
}

func (s *ProxyQuoteStore) Put(quote ProxyQuote) (ProxyQuote, error) {
	if s == nil {
		return ProxyQuote{}, fmt.Errorf("pricing quotes are not configured")
	}
	if strings.TrimSpace(quote.HubID) == "" || strings.TrimSpace(quote.TenantID) == "" ||
		strings.TrimSpace(quote.RequestID) == "" || strings.TrimSpace(quote.ProviderID) == "" ||
		strings.TrimSpace(quote.LogicalModel) == "" || strings.TrimSpace(quote.RequestDigest) == "" || quote.ExpiresAt.IsZero() {
		return ProxyQuote{}, fmt.Errorf("invalid pricing quote")
	}
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return ProxyQuote{}, fmt.Errorf("generate pricing quote token: %w", err)
	}
	quote.Token = fmt.Sprintf("q_%x", buf)
	quote.ExpiresAt = quote.ExpiresAt.UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	for token, item := range s.items {
		if !item.ExpiresAt.After(now) {
			delete(s.items, token)
		}
	}
	s.items[quote.Token] = quote
	return quote, nil
}

// Claim atomically validates and consumes a quote before an upstream attempt
// can start. Consumption is intentionally irreversible: after a connection
// failure HubCenter cannot safely tell whether the provider received work, so
// allowing the same token again could duplicate a billable or side-effecting
// upstream request. Hub reconciles a lost response through the billing-attempt
// endpoint instead.
func (s *ProxyQuoteStore) Claim(token, hubID, tenantID, requestID, requestDigest string) (ProxyQuote, bool) {
	if s == nil || strings.TrimSpace(token) == "" {
		return ProxyQuote{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := strings.TrimSpace(token)
	quote, ok := s.items[key]
	if !ok || !quote.ExpiresAt.After(time.Now().UTC()) {
		delete(s.items, key)
		return ProxyQuote{}, false
	}
	if !strings.EqualFold(strings.TrimSpace(quote.HubID), strings.TrimSpace(hubID)) ||
		!strings.EqualFold(strings.TrimSpace(quote.TenantID), strings.TrimSpace(tenantID)) ||
		!strings.EqualFold(strings.TrimSpace(quote.RequestID), strings.TrimSpace(requestID)) ||
		!strings.EqualFold(strings.TrimSpace(quote.RequestDigest), strings.TrimSpace(requestDigest)) {
		return ProxyQuote{}, false
	}
	delete(s.items, key)
	quote.Claimed = true
	return quote, true
}

// proxyRequestDigest binds an opaque quote to the exact JSON payload that was
// priced. It is held only in HubCenter memory and is never returned to Hub or
// written to the billing-attempt record.
func proxyRequestDigest(body []byte) string {
	sum := sha256.Sum256(body)
	return fmt.Sprintf("%x", sum[:])
}

const TenantBoundErrorCode = "TENANT_BOUND_TO_NODE"

const (
	RedirectNodeHeader = "X-MaClaw-Redirect-Node"
	RedirectURLHeader  = "X-MaClaw-Redirect-URL"
)

// TenantBoundError tells Hub this tenant's official LLM lease lives on another node.
type TenantBoundError struct {
	NodeID      string
	RedirectURL string
}

func (e *TenantBoundError) Error() string {
	if e == nil {
		return "tenant bound to another node, please redirect"
	}
	nodeID := strings.TrimSpace(e.NodeID)
	if nodeID == "" {
		nodeID = "unknown"
	}
	if url := strings.TrimSpace(e.RedirectURL); url != "" {
		return fmt.Sprintf("tenant bound to node %s, please redirect to %s", nodeID, url)
	}
	return fmt.Sprintf("tenant bound to node %s, please redirect", nodeID)
}

func newTenantBoundError(cfg *ProxyConfig, nodeID string) error {
	nodeID = strings.TrimSpace(nodeID)
	redirectURL := ""
	if cfg != nil && cfg.LookupNodeURL != nil && nodeID != "" {
		redirectURL = clientFacingRedirectURL(cfg.LookupNodeURL(nodeID))
	}
	return &TenantBoundError{NodeID: nodeID, RedirectURL: redirectURL}
}

func clientFacingRedirectURL(raw string) string {
	u := strings.TrimRight(strings.TrimSpace(raw), "/")
	parsed, err := url.Parse(u)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	return u
}

func rejectIfTenantBound(ctx context.Context, cfg *ProxyConfig, hubID, tenantID string) error {
	if cfg == nil || cfg.CheckBinding == nil {
		return nil
	}
	allowed, redirectNode := cfg.CheckBinding(ctx, hubID, tenantID)
	if allowed {
		return nil
	}
	return newTenantBoundError(cfg, redirectNode)
}

func asTenantBoundError(err error) *TenantBoundError {
	var bound *TenantBoundError
	if errors.As(err, &bound) {
		return bound
	}
	return nil
}

// ProxyRequest holds the parsed incoming LLM proxy request.
type ProxyRequest struct {
	HubID           string
	TenantID        string
	RequestID       string
	ServiceGroupID  string
	Header          http.Header
	Body            map[string]any
	RawBody         []byte
	Model           string
	StartedAt       time.Time
	WorkloadClass   string
	ClassSource     string
	RuleClass       string
	RuleSource      string
	HeadClass       string
	HeadMaxP        float64
	HeadPassthrough bool
	// Quote is populated after an opaque HubCenter quote token is verified. Its
	// route and time-of-use price stay fixed for the entire upstream attempt.
	Quote *ProxyQuote
}

// ProxyResponse holds the result of a proxied LLM request.
type ProxyResponse struct {
	StatusCode       int
	Body             []byte
	ProviderID       string
	InputTokens      int64
	OutputTokens     int64
	CacheHit         bool
	CreditMultiplier float64
	PricingSnapshot  *llmpool.TokenPricingSnapshot
}

type ProxyStreamWriter interface {
	io.Writer
	Flush()
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
	startedAt := proxyRequestStartedAt(req)
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
	requestedModel := model

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
	model, matchedGroup, dispatchModel = applyProxyWorkloadRouting(req, cfg, reg, matchedGroup, dispatchModel, model)
	if matchedGroup == nil || dispatchModel == nil {
		return nil, fmt.Errorf("model %q not available on this HubCenter", model)
	}
	req.Model = model
	if req.Body != nil && strings.TrimSpace(model) != "" {
		req.Body["model"] = model
	}
	if requestedGroupID := strings.TrimSpace(req.ServiceGroupID); requestedGroupID != "" && !strings.EqualFold(requestedGroupID, strings.TrimSpace(matchedGroup.ID)) {
		log.Printf("[llm-proxy] resolved service_group alias requested=%s matched=%s model=%s hub=%s tenant=%s", requestedGroupID, matchedGroup.ID, model, req.HubID, req.TenantID)
	}

	// 3. Check tenant authorization only when this service group requires a card/grant.
	var auth *TenantAuthorization
	requiresGrant := serviceGroupRequiresGrant(matchedGroup)
	if requiresGrant {
		var err error
		auth, err = cfg.AuthChecker.CheckAccess(ctx, req.HubID, req.TenantID, matchedGroup.ID)
		if err != nil {
			return nil, fmt.Errorf("authorization denied: %w", err)
		}
	}

	// 3.5 Check node binding (HA anti-double-spend)
	if err := rejectIfTenantBound(ctx, cfg, req.HubID, req.TenantID); err != nil {
		return nil, err
	}

	// 4. Check cache unless it belongs to a missing or paused provider.
	if cfg.Cache != nil && req.Quote == nil {
		cacheKey := buildServiceGroupCacheKey(matchedGroup.ID, model, req.Body)
		if cached, _ := cfg.Cache.Get(ctx, cacheKey); cached != nil {
			if provider := findProvider(reg, cached.ProviderID); provider != nil && !provider.Paused {
				// Record cache hit usage (no credits deducted)
				if cfg.Usage != nil {
					_ = cfg.Usage.RecordUsage(ctx, &llmpool.UsageRecord{
						ProviderID:     cached.ProviderID,
						Model:          model,
						ServiceGroupID: matchedGroup.ID,
						WorkloadClass:  req.WorkloadClass,
						ClassSource:    req.ClassSource,
						Preview:        llmpool.RequestTextPreview(req.Body, 200),
						CacheHit:       true,
						Timestamp:      time.Now().UTC(),
					})
				}
				recordProxyClassHeadSample(cfg, req, matchedGroup.ID)
				return &ProxyResponse{
					StatusCode:       http.StatusOK,
					Body:             stripProxyResponseUsage(cached.Payload),
					ProviderID:       cached.ProviderID,
					CacheHit:         true,
					CreditMultiplier: proxyCacheHitCreditMultiplier(reg, dispatchModel, cached.ProviderID, startedAt),
				}, nil
			}
		}
	}

	// 5. Same-multiplier providers share an equal-weight WRR pool. Other
	// multipliers and score bands stay failover-only. Unnamed extras stay
	// after primaries and WRR among themselves by vendor multiplier.
	orderedRoutes, quoteErr := proxyOrderedRoutesForRequest(cfg, reg, req, matchedGroup, dispatchModel, model, acceptLiveProvider, startedAt, false)
	if quoteErr != nil {
		return nil, quoteErr
	}
	if len(orderedRoutes) == 0 {
		return nil, fmt.Errorf("no providers configured for model %q", model)
	}

	// 6. Try providers in order (with concurrency + resilience)
	var lastErr error
	gate := newProviderAttemptGate()
	for i, route := range orderedRoutes {
		providerID := route.ProviderID
		provider := findProvider(reg, providerID)
		if provider == nil {
			lastErr = fmt.Errorf("provider %s referenced in model but not found in registry", providerID)
			continue
		}
		if provider.Paused {
			lastErr = fmt.Errorf("provider %s is paused", providerID)
			continue
		}

		// Runtime health: cooldown after consecutive failures, then one probe
		// covering every remaining route for this provider in the request.
		fresh, err := gate.before(cfg, provider)
		if err != nil {
			lastErr = err
			continue
		}

		// Concurrency control: 0 = unlimited. A busy provider is skipped so
		// the next sequenced backend can serve the request.
		release, acqErr := acquireProxyConcurrency(cfg, provider)
		if acqErr != nil {
			if shouldAbortResilienceProbe(fresh, hasLaterRouteForProvider(orderedRoutes, i, providerID)) {
				proxyAbortResilienceProbe(cfg, providerID)
			}
			lastErr = fmt.Errorf("provider %s is at concurrency limit %d", providerID, provider.MaxConcurrency)
			continue
		}

		// Forward request
		upstreamModel := proxyUpstreamModelForRoute(route, provider, model)
		resp, fwdErr := func() (*providerForwardResponse, error) {
			defer release()
			return forwardToProvider(ctx, cfg.HTTPClient, provider, req.Body, upstreamModel, requestedModel)
		}()
		if proxyCanceledWithoutUpstreamSuccess(ctx, fwdErr, resp) {
			proxyAbortResilienceProbe(cfg, providerID)
			return nil, ctx.Err()
		}

		if fwdErr != nil || resp == nil || shouldRetryProxyProviderStatus(resp.StatusCode) {
			if !hasLaterRouteForProvider(orderedRoutes, i, providerID) {
				proxyRecordResilienceFailure(cfg, provider)
			}
			if fwdErr != nil {
				lastErr = fmt.Errorf("provider %s failed for logical model %s upstream model %s: %w", providerID, model, upstreamModel, fwdErr)
			} else if resp == nil {
				lastErr = fmt.Errorf("provider %s failed for logical model %s upstream model %s: empty response", providerID, model, upstreamModel)
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

		// Directional provider pricing is frozen at request start and marked up
		// once by the service-group route.  The legacy vendor multiplier path is
		// retained only for routes which have not configured token pricing.
		credits, multiplier, pricingSnapshot := proxyRequestBillingCredits(req, matchedGroup, provider, dispatchModel, route, providerID, upstreamModel, inputTokens, outputTokens, nil)

		// Deduct credits
		var deductions []CreditDeduction
		if credits > 0 && requiresGrant && auth != nil {
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
				ProviderID:     providerID,
				Model:          model,
				ServiceGroupID: matchedGroup.ID,
				WorkloadClass:  req.WorkloadClass,
				ClassSource:    req.ClassSource,
				Preview:        llmpool.RequestTextPreview(req.Body, 200),
				InputTokens:    inputTokens,
				OutputTokens:   outputTokens,
				Credits:        recordCredits,
				CacheHit:       false,
				AuthID:         deductionAuthIDs(deductions),
				Timestamp:      time.Now().UTC(),
			})
		}
		recordProxyClassHeadSample(cfg, req, matchedGroup.ID)

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

		response := &ProxyResponse{
			StatusCode:       resp.StatusCode,
			Body:             resp.Body,
			ProviderID:       providerID,
			InputTokens:      inputTokens,
			OutputTokens:     outputTokens,
			CacheHit:         false,
			CreditMultiplier: multiplier,
			PricingSnapshot:  pricingSnapshot,
		}
		if err := recordProxyBillingAttempt(cfg, req, response); err != nil {
			// The provider has already consumed the request. Returning an error
			// before Hub receives the response would unnecessarily turn a complete
			// request into an ambiguous transport outcome, so retain availability
			// while making the durability failure explicit in logs/metrics.
			log.Printf("[llm-proxy] ERROR: persist billing reconciliation attempt hub=%s tenant=%s request=%s: %v", req.HubID, req.TenantID, req.RequestID, err)
		}
		return response, nil
	}

	// All providers failed
	log.Printf("[llm-proxy] all providers failed for model=%s service_group=%s hub=%s tenant=%s lastErr=%v", model, matchedGroup.ID, req.HubID, req.TenantID, lastErr)
	if lastErr != nil {
		return nil, fmt.Errorf("all providers failed, last error: %w", lastErr)
	}
	return nil, fmt.Errorf("no available providers for model %q", model)
}

func persistProxyBillingAttempt(cfg *ProxyConfig, attempt ProxyBillingAttempt) error {
	if cfg == nil || cfg.Attempts == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), proxyBillingAttemptPersistenceTimeout)
	defer cancel()
	return cfg.Attempts.RecordContext(ctx, attempt)
}

func recordProxyBillingAttempt(cfg *ProxyConfig, req *ProxyRequest, response *ProxyResponse) error {
	if cfg == nil || cfg.Attempts == nil || req == nil || response == nil || response.PricingSnapshot == nil || response.StatusCode >= http.StatusBadRequest {
		return nil
	}
	return persistProxyBillingAttempt(cfg, ProxyBillingAttempt{
		HubID:           req.HubID,
		TenantID:        req.TenantID,
		RequestID:       req.RequestID,
		StatusCode:      response.StatusCode,
		ProviderID:      response.ProviderID,
		PricingSnapshot: *response.PricingSnapshot,
		CompletedAt:     time.Now().UTC(),
	})
}

func proxyTokenPricingSnapshot(group *llmpool.ServiceGroup, provider *llmpool.ProviderConfig, providerID, upstreamModel string, inputTokens, outputTokens int64, startedAt time.Time) *llmpool.TokenPricingSnapshot {
	_, route, ok := findGroupProviderConfig(group, providerID, upstreamModel)
	if !ok {
		return nil
	}
	if llmpool.NormalizeBillingMode(route.BillingMode) == llmpool.BillingModeFree {
		return nil
	}
	effective := route.TokenPricing
	if provider != nil {
		effective = llmpool.EffectiveRouteTokenPricing(route, *provider)
	}
	pricing, ok := llmpool.ResolveTokenPricing(effective, startedAt)
	if !ok {
		return nil
	}
	providerMultiplier := 1.0
	if provider != nil {
		providerMultiplier = llmpool.ResolveCreditMultiplier(provider.BillingPolicy(), startedAt)
	}
	return &llmpool.TokenPricingSnapshot{
		ProviderID:         strings.TrimSpace(providerID),
		UpstreamModel:      strings.TrimSpace(upstreamModel),
		Pricing:            pricing,
		ProviderMultiplier: providerMultiplier,
		InputTokens:        inputTokens,
		OutputTokens:       outputTokens,
	}
}

func proxyRequestTokenPricingSnapshot(req *ProxyRequest, group *llmpool.ServiceGroup, provider *llmpool.ProviderConfig, providerID, upstreamModel string, inputTokens, outputTokens int64, startedAt time.Time) *llmpool.TokenPricingSnapshot {
	if pricing := proxyResolvedRequestTokenPricing(req, group, provider, providerID, upstreamModel, startedAt); pricing != nil {
		return &llmpool.TokenPricingSnapshot{
			ProviderID:         strings.TrimSpace(providerID),
			UpstreamModel:      strings.TrimSpace(upstreamModel),
			Pricing:            *pricing,
			ProviderMultiplier: proxyProviderMultiplierForRequest(req, provider, providerID, startedAt),
			InputTokens:        inputTokens,
			OutputTokens:       outputTokens,
		}
	}
	return nil
}

func proxyResolvedRequestTokenPricing(req *ProxyRequest, group *llmpool.ServiceGroup, provider *llmpool.ProviderConfig, providerID, upstreamModel string, startedAt time.Time) *llmpool.ResolvedTokenPricing {
	if pricing := proxyQuotePricingForRequest(req, providerID); pricing != nil {
		return pricing
	}
	snapshot := proxyTokenPricingSnapshot(group, provider, providerID, upstreamModel, 0, 0, startedAt)
	if snapshot == nil {
		return nil
	}
	pricing := snapshot.Pricing
	return &pricing
}

// proxyRequestBillingCredits is the single direct-HubCenter billing path. A
// configured directional price belongs to the provider; the route multiplier
// belongs to the service group and is applied exactly once. Provider legacy
// multipliers remain available only for legacy routes without directional
// pricing, so migrating a provider cannot accidentally double-charge users.
func proxyRequestBillingCredits(req *ProxyRequest, group *llmpool.ServiceGroup, provider *llmpool.ProviderConfig, model *llmpool.DispatchModel, route llmpool.DispatchProviderRoute, providerID, upstreamModel string, inputTokens, outputTokens int64, frozenPricing *llmpool.ResolvedTokenPricing) (credits, displayMultiplier float64, snapshot *llmpool.TokenPricingSnapshot) {
	startedAt := proxyRequestStartedAt(req)
	// An explicit free route is terminal. It must never fall through to the
	// legacy tokens × multiplier path merely because it intentionally has no
	// directional price configured.
	if proxyRouteBillingMode(group, providerID, upstreamModel) == llmpool.BillingModeFree {
		return 0, 1, nil
	}
	if frozenPricing != nil {
		snapshot = &llmpool.TokenPricingSnapshot{
			ProviderID:         strings.TrimSpace(providerID),
			UpstreamModel:      strings.TrimSpace(upstreamModel),
			Pricing:            *frozenPricing,
			ProviderMultiplier: proxyProviderMultiplierForRequest(req, provider, providerID, startedAt),
			InputTokens:        inputTokens,
			OutputTokens:       outputTokens,
		}
	} else {
		snapshot = proxyRequestTokenPricingSnapshot(req, group, provider, providerID, upstreamModel, inputTokens, outputTokens, startedAt)
	}
	if snapshot != nil {
		// Directional pricing is the provider's base price. Its time-of-use
		// multiplier and the route markup are both part of the final debit.
		displayMultiplier = llmpool.CombineCreditMultipliers(snapshot.ProviderMultiplier, proxyCreditMultiplierForRoute(model, route))
		if microcredits, ok := llmpool.EstimateTokenPricingMicrocredits(inputTokens, outputTokens, snapshot.Pricing, displayMultiplier); ok {
			return llmpool.MicrocreditsToCredits(microcredits), displayMultiplier, snapshot
		}
		// A snapshot is only produced from validated pricing. Keep a defensive
		// fallback so a malformed in-memory configuration cannot skip billing.
		input := float64(maxProxyTokenCount(inputTokens)) * snapshot.Pricing.InputCreditsPer10K * displayMultiplier / defaultProxyTokensPerCredit
		output := float64(maxProxyTokenCount(outputTokens)) * snapshot.Pricing.OutputCreditsPer10K * displayMultiplier / defaultProxyTokensPerCredit
		credits = input + output
		if minimum := snapshot.Pricing.MinimumRequestCredits * displayMultiplier; credits < minimum {
			credits = minimum
		}
		return roundProxyCredits(credits), displayMultiplier, snapshot
	}

	displayMultiplier = proxyEffectiveCreditMultiplier(provider, model, route, startedAt)
	return estimateProxyCreditsWithFloor(inputTokens+outputTokens, displayMultiplier), displayMultiplier, nil
}

// proxyProviderMultiplierForRequest freezes a quoted provider's resolved
// HubCenter multiplier for the entire request. Direct requests resolve it at
// their request start, which is the same ownership boundary as token pricing.
func proxyProviderMultiplierForRequest(req *ProxyRequest, provider *llmpool.ProviderConfig, providerID string, startedAt time.Time) float64 {
	if req != nil && req.Quote != nil && strings.EqualFold(strings.TrimSpace(req.Quote.ProviderID), strings.TrimSpace(providerID)) {
		return llmpool.NormalizeCreditMultiplier(req.Quote.ProviderMultiplier)
	}
	if provider != nil {
		return llmpool.ResolveCreditMultiplier(provider.BillingPolicy(), startedAt)
	}
	return 1
}

func proxyRouteBillingMode(group *llmpool.ServiceGroup, providerID, upstreamModel string) string {
	_, route, ok := findGroupProviderConfig(group, providerID, upstreamModel)
	if !ok {
		return ""
	}
	return llmpool.NormalizeBillingMode(route.BillingMode)
}

func maxProxyTokenCount(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}

func proxyQuotePricingForRequest(req *ProxyRequest, providerID string) *llmpool.ResolvedTokenPricing {
	if req == nil || req.Quote == nil || !strings.EqualFold(strings.TrimSpace(req.Quote.ProviderID), strings.TrimSpace(providerID)) {
		return nil
	}
	pricing := req.Quote.Pricing
	return &pricing
}

// proxyOrderedRoutesForRequest permits no fallback when Hub supplied a valid
// quote. That is essential: retrying a different provider after Hub admitted
// a fixed price would make the final charge non-deterministic.
func proxyOrderedRoutesForRequest(cfg *ProxyConfig, reg *Registry, req *ProxyRequest, group *llmpool.ServiceGroup, model *llmpool.DispatchModel, logicalModel string, accept func(*llmpool.ProviderConfig) bool, startedAt time.Time, stream bool) ([]llmpool.DispatchProviderRoute, error) {
	if req == nil || req.Quote == nil {
		return orderProxyDispatchRoutes(cfg, reg, group, logicalModel, llmpool.OrderScoredProviderRoutes(req.Body, model), accept, startedAt, stream), nil
	}
	quote := req.Quote
	// Claim validates expiry before consuming the opaque token. Once claimed,
	// the route and price are immutable for that one in-flight request, so do
	// not reject it merely because the short admission TTL passes while HubCenter
	// is parsing or preparing the dispatch. Direct/internal callers that did not
	// claim a quote retain the expiry validation below.
	if (!quote.Claimed && !quote.ExpiresAt.After(time.Now().UTC())) || !strings.EqualFold(quote.HubID, req.HubID) || !strings.EqualFold(quote.TenantID, req.TenantID) ||
		!strings.EqualFold(quote.RequestID, req.RequestID) || !strings.EqualFold(quote.LogicalModel, logicalModel) ||
		!strings.EqualFold(quote.ServiceGroupID, group.ID) {
		return nil, fmt.Errorf("pricing quote does not match this request")
	}
	for _, route := range llmpool.OrderScoredProviderRoutes(req.Body, model) {
		if strings.EqualFold(route.Route.ProviderID, quote.ProviderID) && strings.EqualFold(proxyUpstreamModelForRoute(route.Route, findProvider(reg, route.Route.ProviderID), logicalModel), quote.UpstreamModel) {
			provider := findProvider(reg, route.Route.ProviderID)
			if provider == nil || provider.Paused || (accept != nil && !accept(provider)) {
				return nil, fmt.Errorf("quoted provider %s is not available", quote.ProviderID)
			}
			return []llmpool.DispatchProviderRoute{route.Route}, nil
		}
	}
	return nil, fmt.Errorf("quoted provider route is no longer configured")
}

func HandleProxyStreamRequest(ctx context.Context, cfg *ProxyConfig, req *ProxyRequest, dst ProxyStreamWriter) error {
	if dst == nil {
		return fmt.Errorf("stream writer is required")
	}
	ctx = WithUsageContext(ctx, req.HubID, req.TenantID)
	dispatches, err := prepareProxyStreamDispatches(ctx, cfg, req)
	if err != nil {
		return err
	}
	_, err = handleProxyStreamDispatches(ctx, cfg, req, dst, dispatches)
	return err
}

func handleProxyStreamDispatches(ctx context.Context, cfg *ProxyConfig, req *ProxyRequest, dst ProxyStreamWriter, dispatches []*proxyDispatch) (*proxyDispatch, error) {
	var lastErr error
	gate := newProviderAttemptGate()
	for i, dispatch := range dispatches {
		if dispatch == nil || dispatch.provider == nil {
			continue
		}
		providerID := dispatch.provider.ID
		fresh, err := gate.before(cfg, dispatch.provider)
		if err != nil {
			lastErr = err
			continue
		}

		release, acqErr := acquireProxyConcurrency(cfg, dispatch.provider)
		if acqErr != nil {
			if shouldAbortResilienceProbe(fresh, hasLaterDispatchForProvider(dispatches, i, providerID)) {
				proxyAbortResilienceProbe(cfg, providerID)
			}
			lastErr = fmt.Errorf("provider %s is at concurrency limit %d", providerID, dispatch.provider.MaxConcurrency)
			continue
		}

		upstreamModel := proxyUpstreamModelForRoute(dispatch.route, dispatch.provider, dispatch.model)
		responseModel := strings.TrimSpace(dispatch.responseModel)
		if responseModel == "" {
			responseModel = dispatch.model
		}
		result, err := func() (*providerStreamResult, error) {
			defer release()
			return streamProviderToWriter(ctx, cfg.HTTPClient, dispatch.provider, req.Body, upstreamModel, responseModel, dst)
		}()
		if err != nil {
			if proxyCanceledWithoutStreamSuccess(ctx, result) {
				proxyAbortResilienceProbe(cfg, providerID)
				return nil, ctx.Err()
			}
			if !hasLaterDispatchForProvider(dispatches, i, providerID) {
				proxyRecordResilienceFailure(cfg, dispatch.provider)
			}
			lastErr = fmt.Errorf("stream provider %s failed for logical model %s upstream model %s: %w", providerID, dispatch.model, upstreamModel, err)
			if result != nil && result.wroteBusinessStream {
				recordProxyStreamUsage(ctx, cfg, req, dispatch, providerID, result)
				return dispatch, lastErr
			}
			continue
		}
		if result == nil {
			lastErr = fmt.Errorf("stream provider %s failed for logical model %s upstream model %s: empty response", providerID, dispatch.model, upstreamModel)
			if !hasLaterDispatchForProvider(dispatches, i, providerID) {
				proxyRecordResilienceFailure(cfg, dispatch.provider)
			}
			continue
		}
		if result.statusCode >= http.StatusBadRequest {
			lastErr = fmt.Errorf("stream provider %s failed for logical model %s upstream model %s: HTTP %d%s", providerID, dispatch.model, upstreamModel, result.statusCode, proxyProviderErrorSnippet(result.errorBody))
			if shouldRetryProxyProviderStatus(result.statusCode) {
				if proxyCanceledWithoutStreamSuccess(ctx, result) {
					proxyAbortResilienceProbe(cfg, providerID)
					return nil, ctx.Err()
				}
				if !hasLaterDispatchForProvider(dispatches, i, providerID) {
					proxyRecordResilienceFailure(cfg, dispatch.provider)
				}
				continue
			}
			if cfg.Resilience != nil {
				cfg.Resilience.RecordSuccess(providerID)
			}
			return nil, lastErr
		}
		if cfg.Resilience != nil {
			cfg.Resilience.RecordSuccess(providerID)
		}

		recordProxyStreamUsage(ctx, cfg, req, dispatch, providerID, result)
		return dispatch, nil
	}
	if lastErr != nil {
		return nil, fmt.Errorf("all stream providers failed, last error: %w", lastErr)
	}
	return nil, fmt.Errorf("no stream-capable providers available")
}

func recordProxyStreamUsage(ctx context.Context, cfg *ProxyConfig, req *ProxyRequest, dispatch *proxyDispatch, providerID string, result *providerStreamResult) {
	if cfg == nil || req == nil || dispatch == nil || result == nil {
		return
	}
	inputTokens, outputTokens := result.inputTokens, result.outputTokens
	if !result.inputTokensObserved || !result.outputTokensObserved {
		estimatedInput, estimatedOutput := estimateProxyTokenUsage(req.Body, []byte(result.outputText))
		if !result.inputTokensObserved {
			inputTokens = estimatedInput
		}
		if !result.outputTokensObserved {
			outputTokens = estimatedOutput
		}
	}
	// The HTTP handler emits the pricing snapshot only after this function
	// returns, so persist the final measured usage on the winning dispatch for
	// its response trailer. It is request-local and never reused.
	dispatch.billingInputTokens = inputTokens
	dispatch.billingOutputTokens = outputTokens
	upstreamModel := proxyUpstreamModelForRoute(dispatch.route, dispatch.provider, dispatch.model)
	credits, _, _ := proxyRequestBillingCredits(req, dispatch.matchedGroup, dispatch.provider, dispatch.dispatchModel, dispatch.route, providerID, upstreamModel, inputTokens, outputTokens, dispatch.pricing)

	var deductions []CreditDeduction
	if credits > 0 && dispatch.requiresGrant && dispatch.auth != nil && cfg.AuthChecker != nil {
		var deductErr error
		deductions, deductErr = cfg.AuthChecker.DeductCreditsForServiceGroup(ctx, req.HubID, req.TenantID, dispatch.matchedGroup.ID, credits)
		if deductErr != nil {
			log.Printf("[llm-proxy] WARN: stream credits deduction failed auth=%s group=%s credits=%.2f: %v", dispatch.auth.ID, dispatch.matchedGroup.ID, credits, deductErr)
		}
	}
	recordCredits := credits
	if dispatch.requiresGrant {
		recordCredits = totalDeductionCredits(deductions)
	}
	groupID := ""
	if dispatch.matchedGroup != nil {
		groupID = dispatch.matchedGroup.ID
	}
	if cfg.Usage != nil {
		_ = cfg.Usage.RecordUsage(ctx, &llmpool.UsageRecord{
			ProviderID:     providerID,
			Model:          dispatch.model,
			ServiceGroupID: groupID,
			WorkloadClass:  req.WorkloadClass,
			ClassSource:    req.ClassSource,
			Preview:        llmpool.RequestTextPreview(req.Body, 200),
			InputTokens:    inputTokens,
			OutputTokens:   outputTokens,
			Credits:        recordCredits,
			CacheHit:       false,
			AuthID:         deductionAuthIDs(deductions),
			Timestamp:      time.Now().UTC(),
		})
	}
	recordProxyClassHeadSample(cfg, req, groupID)
	if cfg.Attempts != nil {
		if snapshot := proxyDispatchTokenPricingSnapshot(req, dispatch, upstreamModel); snapshot != nil {
			if err := persistProxyBillingAttempt(cfg, ProxyBillingAttempt{HubID: req.HubID, TenantID: req.TenantID, RequestID: req.RequestID, StatusCode: http.StatusOK, ProviderID: providerID, PricingSnapshot: *snapshot, CompletedAt: time.Now().UTC()}); err != nil {
				// A streamed response may already have exposed business events, so do
				// not manufacture a second response. Log the durable-fact failure for
				// the operator instead.
				log.Printf("[llm-proxy] ERROR: persist stream billing reconciliation attempt hub=%s tenant=%s request=%s: %v", req.HubID, req.TenantID, req.RequestID, err)
			}
		}
	}
}

func prepareProxyStreamDispatches(ctx context.Context, cfg *ProxyConfig, req *ProxyRequest) ([]*proxyDispatch, error) {
	dispatches, err := prepareProxyDispatches(ctx, cfg, req, acceptLiveStreamProvider, true)
	if err != nil {
		if strings.Contains(err.Error(), "no available providers") && !strings.Contains(err.Error(), "paused") {
			return nil, fmt.Errorf("no stream-capable providers available")
		}
		return nil, err
	}
	return dispatches, nil
}

type proxyDispatch struct {
	model               string
	responseModel       string
	matchedGroup        *llmpool.ServiceGroup
	dispatchModel       *llmpool.DispatchModel
	route               llmpool.DispatchProviderRoute
	provider            *llmpool.ProviderConfig
	auth                *TenantAuthorization
	requiresGrant       bool
	billingInputTokens  int64
	billingOutputTokens int64
	pricing             *llmpool.ResolvedTokenPricing
}

func prepareProxyDispatch(ctx context.Context, cfg *ProxyConfig, req *ProxyRequest) (*proxyDispatch, error) {
	dispatches, err := prepareProxyDispatches(ctx, cfg, req, acceptLiveProvider, false)
	if err != nil {
		return nil, err
	}
	if len(dispatches) == 0 {
		return nil, fmt.Errorf("no available providers for model %q", strings.TrimSpace(req.Model))
	}
	return dispatches[0], nil
}

func prepareProxyDispatches(ctx context.Context, cfg *ProxyConfig, req *ProxyRequest, accept func(*llmpool.ProviderConfig) bool, stream bool) ([]*proxyDispatch, error) {
	if cfg == nil || cfg.Service == nil || cfg.AuthChecker == nil {
		return nil, fmt.Errorf("proxy not configured")
	}
	if req == nil {
		return nil, fmt.Errorf("proxy request is required")
	}
	if req.Body == nil {
		return nil, fmt.Errorf("proxy request body is required")
	}
	if accept == nil {
		accept = acceptLiveProvider
	}
	ctx = WithUsageContext(ctx, req.HubID, req.TenantID)

	model := strings.TrimSpace(req.Model)
	if model == "" {
		if m, ok := req.Body["model"].(string); ok {
			model = strings.TrimSpace(m)
		}
	}
	if model == "" {
		return nil, fmt.Errorf("model not specified in request")
	}
	requestedModel := model

	reg, err := cfg.Service.LoadRegistry(ctx)
	if err != nil {
		return nil, fmt.Errorf("load registry: %w", err)
	}
	matchedGroup, dispatchModel := matchProxyServiceGroupModel(reg, strings.TrimSpace(req.ServiceGroupID), model)
	if matchedGroup == nil {
		return nil, fmt.Errorf("model %q not available on this HubCenter", model)
	}
	model, matchedGroup, dispatchModel = applyProxyWorkloadRouting(req, cfg, reg, matchedGroup, dispatchModel, model)
	if matchedGroup == nil || dispatchModel == nil {
		return nil, fmt.Errorf("model %q not available on this HubCenter", model)
	}
	req.Model = model
	if req.Body != nil && strings.TrimSpace(model) != "" {
		req.Body["model"] = model
	}
	if requestedGroupID := strings.TrimSpace(req.ServiceGroupID); requestedGroupID != "" && !strings.EqualFold(requestedGroupID, strings.TrimSpace(matchedGroup.ID)) {
		log.Printf("[llm-proxy] resolved service_group alias requested=%s matched=%s model=%s hub=%s tenant=%s", requestedGroupID, matchedGroup.ID, model, req.HubID, req.TenantID)
	}

	var auth *TenantAuthorization
	requiresGrant := serviceGroupRequiresGrant(matchedGroup)
	if requiresGrant {
		auth, err = cfg.AuthChecker.CheckAccess(ctx, req.HubID, req.TenantID, matchedGroup.ID)
		if err != nil {
			return nil, fmt.Errorf("authorization denied: %w", err)
		}
	}
	if err := rejectIfTenantBound(ctx, cfg, req.HubID, req.TenantID); err != nil {
		return nil, err
	}

	orderedRoutes, quoteErr := proxyOrderedRoutesForRequest(cfg, reg, req, matchedGroup, dispatchModel, model, accept, proxyRequestStartedAt(req), stream)
	if quoteErr != nil {
		return nil, quoteErr
	}
	if len(orderedRoutes) == 0 {
		return nil, fmt.Errorf("no providers configured for model %q", model)
	}
	dispatches := make([]*proxyDispatch, 0, len(orderedRoutes))
	sawPaused := false
	for _, route := range orderedRoutes {
		provider := findProvider(reg, route.ProviderID)
		if provider == nil {
			continue
		}
		if provider.Paused {
			sawPaused = true
			continue
		}
		if !accept(provider) {
			continue
		}
		responseModel := requestedModel
		if responseModel == "" {
			responseModel = model
		}
		upstreamModel := proxyUpstreamModelForRoute(route, provider, model)
		pricing := proxyResolvedRequestTokenPricing(req, matchedGroup, provider, provider.ID, upstreamModel, proxyRequestStartedAt(req))
		dispatches = append(dispatches, &proxyDispatch{
			model:         model,
			responseModel: responseModel,
			matchedGroup:  matchedGroup,
			dispatchModel: dispatchModel,
			route:         route,
			provider:      provider,
			auth:          auth,
			requiresGrant: requiresGrant,
			pricing:       pricing,
		})
	}
	if len(dispatches) == 0 {
		if sawPaused {
			return nil, fmt.Errorf("no available providers for model %q: paused", model)
		}
		return nil, fmt.Errorf("no available providers for model %q", model)
	}
	return dispatches, nil
}

type providerStreamResult struct {
	statusCode           int
	errorBody            []byte
	inputTokens          int64
	outputTokens         int64
	inputTokensObserved  bool
	outputTokensObserved bool
	outputText           string
	wroteStream          bool
	wroteBusinessStream  bool
}

func streamProviderToWriter(ctx context.Context, client *http.Client, provider *llmpool.ProviderConfig, body map[string]any, upstreamModel, responseModel string, dst ProxyStreamWriter) (*providerStreamResult, error) {
	if provider == nil {
		return nil, fmt.Errorf("provider is required")
	}
	if !providerSupportsProxyStreaming(provider) {
		return nil, fmt.Errorf("provider %s does not support direct stream proxy", provider.ID)
	}
	reqBody := make(map[string]any, len(body)+2)
	for k, v := range body {
		reqBody[k] = v
	}
	reqBody["model"] = upstreamModel
	reqBody["stream"] = true
	delete(reqBody, "provider")
	delete(reqBody, "model_provider")
	sanitizeProxyStreamOptions(reqBody)
	// Ensure max_tokens is present for the upstream provider. The client may
	// have set it, but if not (or if it was stripped), enforce a generous default
	// so the backend model doesn't use a low internal default that truncates
	// tool call arguments. This is the authoritative "last chance" enforcement
	// before the request hits the actual LLM API.
	if _, hasMax := reqBody["max_tokens"]; !hasMax {
		if _, hasMaxComp := reqBody["max_completion_tokens"]; !hasMaxComp {
			reqBody["max_tokens"] = 65536
		}
	}
	// Client-stated reasoning intent must reach the upstream in its own
	// spelling: Agnes only honors reasoning_effort and silently ignores the
	// DeepSeek-style thinking object. Auto (no control in the body) stays
	// untouched. (Mirrors the non-stream path in
	// corelib/openai_compat_forward.go sanitizeOpenAICompatForwardBody.)
	corelib.RetargetReasoningControlsForUpstream(corelib.MaclawLLMConfig{
		URL:   provider.APIURL,
		Model: upstreamModel,
	}, reqBody, corelib.ReasoningAPIChat)
	// DeepSeek V4+ thinking mode: ensure thinking is enabled and budget is capped
	// when tools are present. The maclaw client normally sets these, but older
	// clients or third-party integrations may omit them. This is the authoritative
	// "last chance" enforcement on the stream path (mirrors the non-stream path
	// in corelib/openai_compat_forward.go sanitizeOpenAICompatForwardBody).
	if corelib.IsDeepSeekThinkingModeModel(corelib.MaclawLLMConfig{Model: upstreamModel}) {
		if _, hasThinking := reqBody["thinking"]; !hasThinking {
			reqBody["thinking"] = map[string]any{"type": "enabled"}
		}
		if hasToolsInStreamBody(reqBody) {
			if thinking, ok := reqBody["thinking"].(map[string]any); ok {
				if _, hasBudget := thinking["budget_tokens"]; !hasBudget {
					// Conservative budget (4096) — see corelib/llm/client.go comment.
					thinking["budget_tokens"] = 4096
				}
			}
		}
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal stream request: %w", err)
	}
	cfg := corelib.LLMEndpointProvider{
		APIURL:             provider.APIURL,
		APIKey:             provider.APIKey,
		Model:              upstreamModel,
		Protocol:           provider.Protocol,
		WireAPI:            provider.WireAPI,
		UpstreamTimeoutSec: provider.UpstreamTimeoutSec,
	}.MaclawLLMConfig()
	client = proxyStreamingHTTPClient(client, cfg)
	endpoint := corellm.BuildOpenAIChatCompletionsEndpoint(corelib.NormalizeGLMCodingPlanOpenAIBaseURL(provider.APIURL, cfg.UserAgent()))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("build stream request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if strings.TrimSpace(provider.APIKey) != "" {
		httpReq.Header.Set("Authorization", "Bearer "+strings.TrimSpace(provider.APIKey))
	}
	if ua := cfg.UserAgent(); strings.TrimSpace(ua) != "" {
		httpReq.Header.Set("User-Agent", ua)
	}
	corelib.SetCodeGenClientNameHeaderIfNeededWithName(httpReq, cfg.UserAgent())

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	result := &providerStreamResult{statusCode: resp.StatusCode}
	if resp.StatusCode >= http.StatusBadRequest {
		result.errorBody, _ = io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return result, nil
	}
	if err := proxyProviderSSE(resp.Body, dst, responseModel, result); err != nil {
		return result, err
	}
	if !result.wroteBusinessStream {
		return result, fmt.Errorf("upstream stream ended before business data")
	}
	return result, nil
}

func proxyStreamingHTTPClient(base *http.Client, cfg corelib.MaclawLLMConfig) *http.Client {
	if base == nil {
		base = corelib.NewLLMEndpointHTTPClient(cfg)
	}
	streamClient := *base
	streamClient.Timeout = 0
	headerTimeout := time.Duration(cfg.EffectiveTimeoutSec()) * time.Second
	if headerTimeout <= 0 {
		headerTimeout = time.Duration(corelib.DefaultLLMTimeoutSec) * time.Second
	}
	if transport, ok := base.Transport.(*http.Transport); ok && transport != nil {
		clone := transport.Clone()
		if clone.ResponseHeaderTimeout <= 0 || clone.ResponseHeaderTimeout > headerTimeout {
			clone.ResponseHeaderTimeout = headerTimeout
		}
		streamClient.Transport = clone
	} else if base.Transport == nil {
		if transport, ok := http.DefaultTransport.(*http.Transport); ok && transport != nil {
			clone := transport.Clone()
			clone.ResponseHeaderTimeout = headerTimeout
			streamClient.Transport = clone
		}
	}
	return &streamClient
}

func providerSupportsProxyStreaming(provider *llmpool.ProviderConfig) bool {
	if provider == nil {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(provider.Protocol), "") && !strings.EqualFold(strings.TrimSpace(provider.Protocol), "openai") {
		return false
	}
	wire := corelib.NormalizeLLMProviderWireAPI(provider.WireAPI)
	return wire == "chat"
}

func sanitizeProxyStreamOptions(body map[string]any) {
	if body == nil {
		return
	}
	if raw, ok := body["stream_options"]; ok {
		if options, ok := raw.(map[string]any); ok {
			options["include_usage"] = true
			return
		}
	}
	body["stream_options"] = map[string]any{"include_usage": true}
}

// hasToolsInStreamBody checks if the request body contains a non-empty tools array.
// Same logic as corelib's hasToolsInBody but local to avoid export dependency.
func hasToolsInStreamBody(body map[string]any) bool {
	tools, ok := body["tools"]
	if !ok || tools == nil {
		return false
	}
	switch t := tools.(type) {
	case []any:
		return len(t) > 0
	case []map[string]any:
		return len(t) > 0
	default:
		return false
	}
}

func proxyProviderSSE(src io.Reader, dst ProxyStreamWriter, responseModel string, result *providerStreamResult) error {
	scanner := bufio.NewScanner(src)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	event := make([]string, 0, 4)
	flushEvent := func() error {
		if len(event) == 0 {
			return nil
		}
		if !proxySSEEventHasNonEmptyData(event) {
			event = event[:0]
			return nil
		}
		eventType := proxySSEEventType(event)
		dataLines := proxySSEDataLines(event)
		combinedData := proxySSECombinedData(dataLines)
		if combinedData == "[DONE]" {
			if !result.wroteBusinessStream {
				event = event[:0]
				return nil
			}
			if _, err := io.WriteString(dst, "data: [DONE]\n\n"); err != nil {
				return err
			}
			result.wroteStream = true
			dst.Flush()
			event = event[:0]
			return nil
		}
		if streamErr := proxyStreamErrorFromData(eventType, []byte(combinedData)); streamErr != nil {
			event = event[:0]
			return streamErr
		}
		if len(dataLines) > 1 {
			for _, line := range event {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "data:") || strings.HasPrefix(trimmed, ":") {
					continue
				}
				if _, err := io.WriteString(dst, line+"\n"); err != nil {
					return err
				}
			}
			forwardData := combinedData
			if patched, err := proxyStreamPatchAndMeasureData([]byte(combinedData), responseModel, result); err == nil && patched != nil {
				forwardData = string(patched)
			}
			if _, err := io.WriteString(dst, "data: "+forwardData+"\n\n"); err != nil {
				return err
			}
			result.wroteStream = true
			result.wroteBusinessStream = true
			dst.Flush()
			event = event[:0]
			return nil
		}
		for _, line := range event {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "data:") {
				data := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
				if data == "[DONE]" {
					if !result.wroteBusinessStream {
						continue
					}
					if _, err := io.WriteString(dst, line+"\n"); err != nil {
						return err
					}
					result.wroteStream = true
					continue
				}
				streamErr := proxyStreamErrorFromData(eventType, []byte(data))
				if streamErr != nil {
					event = event[:0]
					return streamErr
				}
				forwardLine := line
				if data != "" {
					patched, err := proxyStreamPatchAndMeasureData([]byte(data), responseModel, result)
					if err == nil && patched != nil {
						forwardLine = "data: " + string(patched)
					}
				}
				if _, err := io.WriteString(dst, forwardLine+"\n"); err != nil {
					return err
				}
				result.wroteStream = true
				result.wroteBusinessStream = true
				continue
			}
			if strings.HasPrefix(trimmed, ":") {
				continue
			}
			if _, err := io.WriteString(dst, line+"\n"); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(dst, "\n"); err != nil {
			return err
		}
		dst.Flush()
		event = event[:0]
		return nil
	}
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if err := flushEvent(); err != nil {
				return err
			}
			continue
		}
		event = append(event, line)
	}
	if err := flushEvent(); err != nil {
		return err
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	dst.Flush()
	return nil
}

func proxySSEEventHasNonEmptyData(event []string) bool {
	for _, line := range event {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "data:") && strings.TrimSpace(strings.TrimPrefix(trimmed, "data:")) != "" {
			return true
		}
	}
	return false
}

func proxySSEEventType(event []string) string {
	for _, line := range event {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "event:") {
			return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(trimmed, "event:")))
		}
	}
	return ""
}

func proxySSEDataLines(event []string) []string {
	dataLines := make([]string, 0, len(event))
	for _, line := range event {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(trimmed, "data:")))
		}
	}
	return dataLines
}

func proxySSECombinedData(dataLines []string) string {
	if len(dataLines) == 0 {
		return ""
	}
	if len(dataLines) == 1 {
		return dataLines[0]
	}
	return strings.Join(dataLines, "\n")
}

func proxyStreamErrorFromData(eventType string, data []byte) error {
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		if strings.EqualFold(eventType, "error") && strings.TrimSpace(string(data)) != "" {
			return fmt.Errorf("upstream stream error: %s", strings.TrimSpace(string(data)))
		}
		return nil
	}
	rawErr, ok := payload["error"]
	if (!ok || rawErr == nil) && proxyStreamPayloadLooksLikeTopLevelError(payload) {
		return fmt.Errorf("upstream stream error: %s", strings.TrimSpace(payload["message"].(string)))
	}
	if (!ok || rawErr == nil) && !strings.EqualFold(eventType, "error") {
		return nil
	}
	if message, ok := rawErr.(string); ok && strings.TrimSpace(message) != "" {
		return fmt.Errorf("upstream stream error: %s", strings.TrimSpace(message))
	}
	if errObj, ok := rawErr.(map[string]any); ok {
		if message, ok := errObj["message"].(string); ok && strings.TrimSpace(message) != "" {
			return fmt.Errorf("upstream stream error: %s", strings.TrimSpace(message))
		}
	}
	if message, ok := payload["message"].(string); ok && strings.TrimSpace(message) != "" {
		if strings.EqualFold(eventType, "error") || proxyStreamPayloadLooksLikeTopLevelError(payload) {
			return fmt.Errorf("upstream stream error: %s", strings.TrimSpace(message))
		}
	}
	return fmt.Errorf("upstream stream error")
}

func proxyStreamPayloadLooksLikeTopLevelError(payload map[string]any) bool {
	if payload == nil {
		return false
	}
	if _, ok := payload["choices"]; ok {
		return false
	}
	if _, ok := payload["object"]; ok {
		return false
	}
	code, hasCode := payload["code"].(string)
	if !hasCode || strings.TrimSpace(code) == "" {
		return false
	}
	message, hasMessage := payload["message"].(string)
	return hasMessage && strings.TrimSpace(message) != ""
}

func proxyStreamPatchAndMeasureData(data []byte, responseModel string, result *providerStreamResult) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	if strings.TrimSpace(responseModel) != "" {
		payload["model"] = responseModel
	}
	if usage, _ := payload["usage"].(map[string]any); usage != nil {
		input, output, inputObserved, outputObserved := extractTokenUsageFromMapWithPresence(usage)
		if inputObserved {
			result.inputTokens = input
			result.inputTokensObserved = true
		}
		if outputObserved {
			result.outputTokens = output
			result.outputTokensObserved = true
		}
	}
	result.outputText += proxyStreamChunkText(payload)
	patched, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return patched, nil
}

func proxyStreamChunkText(payload map[string]any) string {
	var text strings.Builder
	choices, _ := payload["choices"].([]any)
	for _, item := range choices {
		choice, _ := item.(map[string]any)
		if choice == nil {
			continue
		}
		delta, _ := choice["delta"].(map[string]any)
		if delta != nil {
			text.WriteString(flattenProxyText(delta["content"]))
			text.WriteString(flattenProxyText(delta["reasoning_content"]))
			text.WriteString(flattenProxyText(delta["tool_calls"]))
			text.WriteString(flattenProxyText(delta["function_call"]))
		}
		text.WriteString(flattenProxyText(choice["text"]))
	}
	return text.String()
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

func acceptLiveProvider(provider *llmpool.ProviderConfig) bool {
	return provider != nil && !provider.Paused
}

func acceptLiveStreamProvider(provider *llmpool.ProviderConfig) bool {
	return acceptLiveProvider(provider) && providerSupportsProxyStreaming(provider)
}

func acquireProxyConcurrency(cfg *ProxyConfig, provider *llmpool.ProviderConfig) (func(), error) {
	if cfg == nil || cfg.Concurrency == nil || provider == nil {
		return func() {}, nil
	}
	release, err := cfg.Concurrency.TryAcquire(provider.ID, provider.MaxConcurrency)
	if err != nil {
		return nil, err
	}
	if release == nil {
		return func() {}, nil
	}
	return release, nil
}

type providerAttemptGate struct {
	admitted map[string]struct{}
	blocked  map[string]error
}

func newProviderAttemptGate() *providerAttemptGate {
	return &providerAttemptGate{
		admitted: map[string]struct{}{},
		blocked:  map[string]error{},
	}
}

func (g *providerAttemptGate) before(cfg *ProxyConfig, provider *llmpool.ProviderConfig) (fresh bool, err error) {
	if g == nil {
		return false, proxyBeforeAttempt(cfg, provider)
	}
	if provider == nil {
		return false, fmt.Errorf("provider is required")
	}
	id := providerIDKey(provider.ID)
	if id == "" {
		return false, fmt.Errorf("provider id required")
	}
	if blocked := g.blocked[id]; blocked != nil {
		return false, blocked
	}
	if _, ok := g.admitted[id]; ok {
		return false, nil
	}
	if err := proxyBeforeAttempt(cfg, provider); err != nil {
		g.blocked[id] = err
		return false, err
	}
	g.admitted[id] = struct{}{}
	return true, nil
}

func shouldAbortResilienceProbe(fresh, hasLater bool) bool {
	return fresh || !hasLater
}

func proxyCanceledWithoutUpstreamSuccess(ctx context.Context, fwdErr error, resp *providerForwardResponse) bool {
	if ctx == nil || ctx.Err() == nil {
		return false
	}
	if fwdErr != nil || resp == nil {
		return true
	}
	return shouldRetryProxyProviderStatus(resp.StatusCode)
}

func proxyCanceledWithoutStreamSuccess(ctx context.Context, result *providerStreamResult) bool {
	if ctx == nil || ctx.Err() == nil {
		return false
	}
	if result != nil && result.wroteBusinessStream {
		return false
	}
	return true
}

const (
	defaultProxyCircuitThreshold = 2
	defaultProxyCircuitBaseMS    = 10_000
	defaultProxyCircuitMaxMS     = 300_000
)

func proxyBeforeAttempt(cfg *ProxyConfig, provider *llmpool.ProviderConfig) error {
	if cfg == nil || cfg.Resilience == nil || provider == nil {
		return nil
	}
	return cfg.Resilience.BeforeAttempt(provider.ID, proxyCircuitThreshold(provider), proxyCircuitBaseMS(provider))
}

func proxyRecordResilienceFailure(cfg *ProxyConfig, provider *llmpool.ProviderConfig) {
	if cfg == nil || cfg.Resilience == nil || provider == nil {
		return
	}
	cfg.Resilience.RecordFailureBackoff(provider.ID, proxyCircuitThreshold(provider), proxyCircuitBaseMS(provider), proxyCircuitMaxMS(provider))
}

func proxyAbortResilienceProbe(cfg *ProxyConfig, providerID string) {
	if cfg == nil || cfg.Resilience == nil {
		return
	}
	cfg.Resilience.AbortProbe(providerID)
}

func proxyCircuitThreshold(provider *llmpool.ProviderConfig) int {
	if provider == nil || provider.CircuitBreakerThreshold <= 0 {
		return defaultProxyCircuitThreshold
	}
	return provider.CircuitBreakerThreshold
}

func proxyCircuitBaseMS(provider *llmpool.ProviderConfig) int {
	if provider == nil {
		return defaultProxyCircuitBaseMS
	}
	if provider.CircuitBreakerCooldownMS > 0 {
		return provider.CircuitBreakerCooldownMS
	}
	if provider.FailureBackoffBaseMS > 0 {
		return provider.FailureBackoffBaseMS
	}
	return defaultProxyCircuitBaseMS
}

func proxyCircuitMaxMS(provider *llmpool.ProviderConfig) int {
	if provider == nil || provider.FailureBackoffMaxMS <= 0 {
		return defaultProxyCircuitMaxMS
	}
	return provider.FailureBackoffMaxMS
}

var proxyDispatchWRR = llmpool.NewWRRScheduler()

func orderProxyDispatchRoutes(cfg *ProxyConfig, reg *Registry, group *llmpool.ServiceGroup, logicalModel string, scored []llmpool.ScoredProviderRoute, accept func(*llmpool.ProviderConfig) bool, startedAt time.Time, stream bool) []llmpool.DispatchProviderRoute {
	if accept == nil {
		accept = acceptLiveProvider
	}
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	primary := balanceProxyScoredRoutes(cfg, reg, scored, accept, startedAt, proxyDispatchPool(group, logicalModel, stream))
	extras := extraLiveServiceGroupFailoverRoutes(reg, group, logicalModel, primary, accept)
	if len(extras) == 0 {
		return primary
	}
	// Extras stay after every primary band. Same vendor-multiplier extras
	// WRR in their own pool; borrowed score/tier/route markup from other
	// models must not split that equal-weight group.
	balancedExtras := balanceProxyExtraRoutes(cfg, reg, extras, accept, startedAt, proxyExtraDispatchPool(group, logicalModel, stream))
	return append(append([]llmpool.DispatchProviderRoute(nil), primary...), balancedExtras...)
}

func balanceProxyScoredRoutes(cfg *ProxyConfig, reg *Registry, scored []llmpool.ScoredProviderRoute, accept func(*llmpool.ProviderConfig) bool, startedAt time.Time, pool string) []llmpool.DispatchProviderRoute {
	if len(scored) == 0 {
		return nil
	}
	candidates := make([]llmpool.BalanceCandidate, 0, len(scored))
	for _, item := range scored {
		meta, provider := proxyDispatchMeta(reg, item.Route.ProviderID)
		candidates = append(candidates, llmpool.BalanceCandidate{
			Route:               item.Route,
			Score:               item.Score,
			ResolutionTier:      item.ResolutionTier,
			EffectiveMultiplier: llmpool.EffectiveRouteMultiplier(meta, item.Route, startedAt),
			Sequence:            meta.Sequence,
			MaxConcurrency:      meta.MaxConcurrency,
			SkipWRR:             proxyRouteSkipWRR(cfg, provider, accept),
		})
	}
	return balancedRoutes(proxyDispatchWRR, pool, candidates)
}

func balanceProxyExtraRoutes(cfg *ProxyConfig, reg *Registry, extras []llmpool.DispatchProviderRoute, accept func(*llmpool.ProviderConfig) bool, startedAt time.Time, pool string) []llmpool.DispatchProviderRoute {
	if len(extras) == 0 {
		return nil
	}
	candidates := make([]llmpool.BalanceCandidate, 0, len(extras))
	for _, route := range extras {
		meta, provider := proxyDispatchMeta(reg, route.ProviderID)
		candidates = append(candidates, llmpool.BalanceCandidate{
			Route:               route,
			EffectiveMultiplier: llmpool.ResolveCreditMultiplier(meta.Billing, startedAt),
			Sequence:            meta.Sequence,
			MaxConcurrency:      meta.MaxConcurrency,
			SkipWRR:             proxyRouteSkipWRR(cfg, provider, accept),
		})
	}
	return balancedRoutes(proxyDispatchWRR, pool, candidates)
}

func proxyDispatchMeta(reg *Registry, providerID string) (llmpool.ProviderDispatchMeta, *llmpool.ProviderConfig) {
	provider := findProvider(reg, providerID)
	if provider == nil {
		return llmpool.ProviderDispatchMeta{}, nil
	}
	return llmpool.MetaFromProvider(*provider), provider
}

func proxyRouteSkipWRR(cfg *ProxyConfig, provider *llmpool.ProviderConfig, accept func(*llmpool.ProviderConfig) bool) bool {
	if accept == nil {
		accept = acceptLiveProvider
	}
	return provider == nil || !accept(provider) || providerCircuitOpen(cfg, provider)
}

func balancedRoutes(sched *llmpool.WRRScheduler, pool string, candidates []llmpool.BalanceCandidate) []llmpool.DispatchProviderRoute {
	balanced := llmpool.BalanceProviderRoutes(sched, pool, candidates)
	out := make([]llmpool.DispatchProviderRoute, 0, len(balanced))
	for _, item := range balanced {
		out = append(out, item.Route)
	}
	return out
}

func proxyDispatchPool(group *llmpool.ServiceGroup, logicalModel string, stream bool) string {
	pool := strings.TrimSpace(logicalModel)
	if group != nil {
		if id := strings.TrimSpace(group.ID); id != "" {
			if pool == "" {
				pool = id
			} else {
				pool = id + "\x1e" + pool
			}
		}
	}
	// Stream accept excludes non-SSE providers from WRR. Keep that membership
	// in its own pool so mixed stream/non-stream traffic does not reset LB.
	if stream {
		if pool == "" {
			return "stream"
		}
		return pool + "\x1estream"
	}
	return pool
}

func proxyExtraDispatchPool(group *llmpool.ServiceGroup, logicalModel string, stream bool) string {
	pool := proxyDispatchPool(group, logicalModel, stream)
	if pool == "" {
		return "extra"
	}
	return pool + "\x1eextra"
}

func providerCircuitOpen(cfg *ProxyConfig, provider *llmpool.ProviderConfig) bool {
	if cfg == nil || cfg.Resilience == nil || provider == nil {
		return false
	}
	return cfg.Resilience.Snapshot(provider.ID, proxyCircuitThreshold(provider)).State == "open"
}

func extraLiveServiceGroupFailoverRoutes(reg *Registry, group *llmpool.ServiceGroup, logicalModel string, routes []llmpool.DispatchProviderRoute, accept func(*llmpool.ProviderConfig) bool) []llmpool.DispatchProviderRoute {
	if accept == nil {
		accept = acceptLiveProvider
	}
	seen := map[string]struct{}{}
	for _, route := range routes {
		if key := providerIDKey(route.ProviderID); key != "" {
			seen[key] = struct{}{}
		}
	}
	var extras []llmpool.DispatchProviderRoute
	for _, providerID := range liveFailoverProviderIDs(reg, group, accept) {
		key := providerIDKey(providerID)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		provider := findProvider(reg, providerID)
		if !accept(provider) {
			continue
		}
		seen[key] = struct{}{}
		failoverRoutes := buildServiceGroupFailoverRoutes(reg, group, provider, logicalModel)
		if len(failoverRoutes) == 0 {
			continue
		}
		for _, route := range failoverRoutes {
			route.OriginalIndex = len(routes) + len(extras)
			extras = append(extras, route)
		}
	}
	return extras
}

func liveFailoverProviderIDs(reg *Registry, group *llmpool.ServiceGroup, accept func(*llmpool.ProviderConfig) bool) []string {
	if accept == nil {
		accept = acceptLiveProvider
	}
	var ids []string
	seen := map[string]struct{}{}
	add := func(providerID string) {
		provider := findProvider(reg, providerID)
		if !accept(provider) {
			return
		}
		key := providerIDKey(provider.ID)
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		ids = append(ids, strings.TrimSpace(provider.ID))
	}
	addFromGroup := func(g *llmpool.ServiceGroup) {
		if g == nil {
			return
		}
		for _, model := range g.Models {
			for _, pc := range modelProviderConfigs(model) {
				add(pc.ProviderID)
			}
		}
	}
	addFromGroup(group)
	agentID := ""
	if group != nil {
		agentID = strings.TrimSpace(group.AgentID)
	}
	if reg != nil && agentID != "" {
		for i := range reg.ServiceGroups {
			g := &reg.ServiceGroups[i]
			if !strings.EqualFold(strings.TrimSpace(g.AgentID), agentID) {
				continue
			}
			addFromGroup(g)
		}
	}
	if reg != nil {
		for i := range reg.Providers {
			add(reg.Providers[i].ID)
		}
	}
	return ids
}

func buildServiceGroupFailoverRoutes(reg *Registry, group *llmpool.ServiceGroup, provider *llmpool.ProviderConfig, logicalModel string) []llmpool.DispatchProviderRoute {
	id := ""
	if provider != nil {
		id = strings.TrimSpace(provider.ID)
	}
	logicalModel = strings.TrimSpace(logicalModel)
	var out []llmpool.DispatchProviderRoute
	seen := map[string]struct{}{}
	add := func(route llmpool.DispatchProviderRoute) {
		route.ProviderID = id
		upstream := strings.ToLower(strings.TrimSpace(route.Model))
		if upstream == "" {
			upstream = strings.ToLower(logicalModel)
			route.Model = logicalModel
		}
		if upstream == "" || id == "" {
			return
		}
		if _, ok := seen[upstream]; ok {
			return
		}
		seen[upstream] = struct{}{}
		out = append(out, route)
	}
	addFromGroup := func(g *llmpool.ServiceGroup) {
		if g == nil {
			return
		}
		for _, model := range g.Models {
			for _, pc := range modelProviderConfigs(model) {
				if !strings.EqualFold(strings.TrimSpace(pc.ProviderID), id) {
					continue
				}
				route := llmpool.DispatchProviderRoute{ProviderID: id, Model: strings.TrimSpace(pc.Model)}
				copyFailoverRoutePolicy(&route, pc)
				route.Model = proxyUpstreamModelForRoute(route, provider, strings.TrimSpace(model.Name))
				add(route)
			}
		}
	}

	knownModels := providerModelSet(provider)
	if logicalModel != "" && (len(knownModels) == 0 || hasFoldedKey(knownModels, logicalModel)) {
		route := llmpool.DispatchProviderRoute{ProviderID: id, Model: logicalModel}
		if _, pc, ok := findGroupProviderConfig(group, id, logicalModel); ok {
			copyFailoverRoutePolicy(&route, pc)
			if model := strings.TrimSpace(pc.Model); model != "" {
				route.Model = model
			} else {
				route.Model = logicalModel
			}
		}
		add(route)
	}
	addFromGroup(group)
	if reg != nil {
		for i := range reg.ServiceGroups {
			g := &reg.ServiceGroups[i]
			if group != nil && strings.EqualFold(strings.TrimSpace(g.ID), strings.TrimSpace(group.ID)) {
				continue
			}
			addFromGroup(g)
		}
	}
	if len(out) == 0 {
		route := llmpool.DispatchProviderRoute{ProviderID: id}
		route.Model = proxyUpstreamModelForRoute(route, provider, logicalModel)
		add(route)
	}
	return out
}

func providerModelSet(provider *llmpool.ProviderConfig) map[string]struct{} {
	if provider == nil || len(provider.Models) == 0 {
		return nil
	}
	out := map[string]struct{}{}
	for _, model := range provider.Models {
		model = strings.ToLower(strings.TrimSpace(model))
		if model == "" {
			continue
		}
		out[model] = struct{}{}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func hasFoldedKey(set map[string]struct{}, name string) bool {
	_, ok := set[strings.ToLower(strings.TrimSpace(name))]
	return ok
}

func providerIDKey(id string) string {
	return strings.ToLower(strings.TrimSpace(id))
}

func hasLaterRouteForProvider(routes []llmpool.DispatchProviderRoute, index int, providerID string) bool {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" || index+1 >= len(routes) {
		return false
	}
	for _, route := range routes[index+1:] {
		if strings.EqualFold(strings.TrimSpace(route.ProviderID), providerID) {
			return true
		}
	}
	return false
}

func hasLaterDispatchForProvider(dispatches []*proxyDispatch, index int, providerID string) bool {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" || index+1 >= len(dispatches) {
		return false
	}
	for _, dispatch := range dispatches[index+1:] {
		if dispatch != nil && dispatch.provider != nil && strings.EqualFold(strings.TrimSpace(dispatch.provider.ID), providerID) {
			return true
		}
	}
	return false
}

func findGroupProviderConfig(group *llmpool.ServiceGroup, providerID, preferModel string) (string, llmpool.ModelProviderConfig, bool) {
	providerID = strings.TrimSpace(providerID)
	preferModel = strings.TrimSpace(preferModel)
	if group == nil || providerID == "" {
		return "", llmpool.ModelProviderConfig{}, false
	}
	var fallbackName string
	var fallback llmpool.ModelProviderConfig
	hasFallback := false
	for _, model := range group.Models {
		for _, pc := range modelProviderConfigs(model) {
			if !strings.EqualFold(strings.TrimSpace(pc.ProviderID), providerID) {
				continue
			}
			name := strings.TrimSpace(model.Name)
			if preferModel != "" {
				if strings.EqualFold(name, preferModel) || strings.EqualFold(strings.TrimSpace(pc.Model), preferModel) {
					return name, pc, true
				}
				continue
			}
			if !hasFallback {
				fallbackName = name
				fallback = pc
				hasFallback = true
			}
		}
	}
	if preferModel != "" {
		return "", llmpool.ModelProviderConfig{}, false
	}
	return fallbackName, fallback, hasFallback
}

func copyFailoverRoutePolicy(route *llmpool.DispatchProviderRoute, pc llmpool.ModelProviderConfig) {
	if route == nil {
		return
	}
	route.CapabilityTags = append([]string(nil), pc.CapabilityTags...)
	route.Priority = pc.Priority
	route.ResolutionTier = pc.ResolutionTier
	route.CreditMultiplier = pc.CreditMultiplier
}

func recordProxyClassHeadSample(cfg *ProxyConfig, req *ProxyRequest, groupID string) {
	if cfg == nil || cfg.Service == nil || req == nil || req.HeadPassthrough {
		return
	}
	ruleClass := strings.TrimSpace(req.RuleClass)
	if ruleClass == "" {
		ruleClass = req.WorkloadClass
	}
	ruleSource := strings.TrimSpace(req.RuleSource)
	if ruleSource == "" {
		ruleSource = req.ClassSource
	}
	cfg.Service.RecordOfficialClassHeadSample(llmpool.RequestTextPreview(req.Body, 400), ruleClass, ruleSource, req.HeadClass, req.HeadMaxP, groupID, req.HeadPassthrough)
}

func applyProxyWorkloadRouting(req *ProxyRequest, cfg *ProxyConfig, reg *Registry, group *llmpool.ServiceGroup, dispatchModel *llmpool.DispatchModel, model string) (string, *llmpool.ServiceGroup, *llmpool.DispatchModel) {
	if req == nil || group == nil {
		return model, group, dispatchModel
	}
	var runtime *llmpool.HeadRuntime
	if cfg != nil && cfg.Service != nil {
		runtime = cfg.Service.HeadRuntimeForGroup(strings.TrimSpace(group.ID), strings.TrimSpace(req.TenantID))
	}
	header := req.Header
	dec := llmpool.ClassifyAndRouteWithHead(header, req.Body, group, model, runtime)
	req.WorkloadClass = dec.Class
	req.ClassSource = dec.Source
	req.RuleClass = dec.RuleClass
	req.RuleSource = dec.RuleSource
	req.HeadClass = dec.HeadClass
	req.HeadMaxP = dec.HeadMaxP
	req.HeadPassthrough = dec.Passthrough
	if dec.Passthrough || strings.TrimSpace(dec.ResolvedModel) == "" || strings.EqualFold(dec.ResolvedModel, model) {
		if req.WorkloadClass == "" {
			req.WorkloadClass = llmpool.WorkloadUnclassified
		}
		return model, group, dispatchModel
	}
	resolved := strings.TrimSpace(dec.ResolvedModel)
	matched, next := matchProxyServiceGroupModel(reg, strings.TrimSpace(group.ID), resolved)
	if matched == nil {
		return model, group, dispatchModel
	}
	return resolved, matched, next
}

func matchProxyServiceGroupModel(reg *Registry, serviceGroupID, model string) (*llmpool.ServiceGroup, *llmpool.DispatchModel) {
	if reg == nil {
		return nil, nil
	}
	serviceGroupID = strings.TrimSpace(serviceGroupID)
	model = strings.TrimSpace(model)
	if isProxyOfficialHubEntry(serviceGroupID) {
		return matchProxyOfficialComputeFallback(reg, model)
	}
	if serviceGroupID != "" {
		for i := range reg.ServiceGroups {
			group := &reg.ServiceGroups[i]
			if !isProxyCatalogServiceGroup(group) {
				continue
			}
			if !strings.EqualFold(strings.TrimSpace(group.ID), serviceGroupID) {
				continue
			}
			if dispatchModel := matchProxyGroupModel(reg, group, model); dispatchModel != nil {
				return group, dispatchModel
			}
			if fallback, dispatchModel := matchProxyConfiguredDefault(reg, model); fallback != nil {
				return fallback, dispatchModel
			}
			return matchProxyHubEntryFallback(reg, serviceGroupID, model)
		}
		if fallback, dispatchModel := matchProxyConfiguredDefault(reg, model); fallback != nil {
			return fallback, dispatchModel
		}
		return matchProxyHubEntryFallback(reg, serviceGroupID, model)
	}
	for i := range reg.ServiceGroups {
		group := &reg.ServiceGroups[i]
		if !isProxyCatalogServiceGroup(group) {
			continue
		}
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
		if strings.EqualFold(strings.TrimSpace(group.Models[j].Name), strings.TrimSpace(model)) {
			return buildDispatchModel(reg, &group.Models[j])
		}
	}
	if llmpool.IsAutoModel(model) && llmpool.IsDynamicKind(group.Kind) {
		for j := range group.Models {
			if llmpool.IsAutoModel(group.Models[j].Name) {
				return buildDispatchModel(reg, &group.Models[j])
			}
		}
		if len(group.Models) > 0 {
			return buildDispatchModel(reg, &group.Models[0])
		}
	}
	return nil
}

func matchProxyHubEntryFallback(reg *Registry, serviceGroupID, model string) (*llmpool.ServiceGroup, *llmpool.DispatchModel) {
	if !isProxySystemFreeAlias(serviceGroupID) {
		return nil, nil
	}
	if group, dispatchModel := matchProxyServiceGroupModelByAccessPolicy(reg, model, AccessPolicyFree); group != nil {
		return group, dispatchModel
	}
	return matchProxyOfficialComputeFallback(reg, model)
}

func matchProxyConfiguredDefault(reg *Registry, model string) (*llmpool.ServiceGroup, *llmpool.DispatchModel) {
	if reg == nil {
		return nil, nil
	}
	id := catalogServiceGroupID(reg, reg.DefaultServiceGroupID)
	if id == "" {
		return nil, nil
	}
	return matchProxyServiceGroupModelByID(reg, id, model)
}

func matchProxyOfficialComputeFallback(reg *Registry, model string) (*llmpool.ServiceGroup, *llmpool.DispatchModel) {
	// Official Hub traffic bills a compute card. Skip a misconfigured free
	// redeem / maclaw-official group instead of treating it as paid compute.
	if group, dispatchModel := matchProxyConfiguredDefault(reg, model); serviceGroupRequiresGrant(group) {
		return group, dispatchModel
	}
	for _, fallbackID := range proxyComputeFallbackServiceGroupIDs {
		group, dispatchModel := matchProxyServiceGroupModelByID(reg, fallbackID, model)
		if !serviceGroupRequiresGrant(group) {
			continue
		}
		return group, dispatchModel
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
		if !isProxyCatalogServiceGroup(group) {
			continue
		}
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

func isProxyOfficialHubEntry(serviceGroupID string) bool {
	return llmpool.IsHubOfficialServiceGroup(serviceGroupID)
}

func isProxyCatalogServiceGroup(group *llmpool.ServiceGroup) bool {
	return group != nil && !isProxyOfficialHubEntry(group.ID)
}

func serviceGroupRequiresGrant(group *llmpool.ServiceGroup) bool {
	return group != nil && normalizeServiceGroupAccessPolicy(group.AccessPolicy) == AccessPolicyGrantRequired
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
	if reg == nil {
		return nil
	}
	idx := providerIndex(reg, id)
	if idx < 0 {
		return nil
	}
	got := reg.Providers[idx]
	return &got
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
	var payload map[string]any
	if err := json.Unmarshal(respBody, &payload); err == nil {
		if usage, _ := payload["usage"].(map[string]any); usage != nil {
			inputTokens, outputTokens, inputObserved, outputObserved := extractTokenUsageFromMapWithPresence(usage)
			// An upstream's explicit zero is a measured fact, not an absent
			// field. Never replace it with a local estimate.
			if inputObserved && outputObserved {
				return inputTokens, outputTokens, patchedBody
			}
			estimatedInput, estimatedOutput := estimateProxyTokenUsage(reqBody, respBody)
			if !inputObserved {
				inputTokens = estimatedInput
			}
			if !outputObserved {
				outputTokens = estimatedOutput
			}
			return inputTokens, outputTokens, completeProxyResponseUsage(respBody, inputTokens, outputTokens)
		}
	}

	inputTokens, outputTokens = extractTokenUsage(respBody)
	if inputTokens > 0 || outputTokens > 0 {
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
	_, _, inputObserved, outputObserved := extractTokenUsageFromMapWithPresence(usage)
	if !inputObserved {
		usage["prompt_tokens"] = inputTokens
	}
	if !outputObserved {
		usage["completion_tokens"] = outputTokens
	}
	if _, totalObserved := usageNumberPresent(usage["total_tokens"]); !totalObserved {
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
		_, _, inputObserved, outputObserved := extractTokenUsageFromMapWithPresence(usage)
		if inputObserved && outputObserved {
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
	inputTokens, outputTokens, _, _ = extractTokenUsageFromMapWithPresence(usage)
	return inputTokens, outputTokens
}

// extractTokenUsageFromMapWithPresence retains the distinction between an
// upstream's explicit zero and a missing directional value. Explicit zero is
// authoritative and must not be replaced by a local token estimate.
func extractTokenUsageFromMapWithPresence(usage map[string]any) (inputTokens, outputTokens int64, inputObserved, outputObserved bool) {
	if usage == nil {
		return 0, 0, false, false
	}
	inputTokens, inputObserved = firstTokenUsageValue(usage, "prompt_tokens", "input_tokens")
	outputTokens, outputObserved = firstTokenUsageValue(usage, "completion_tokens", "output_tokens")
	totalTokens, totalObserved := usageNumberPresent(usage["total_tokens"])
	if totalTokens > 0 {
		switch {
		case !inputObserved && !outputObserved:
			// A total-only usage is a complete measurement. Record it as input
			// and do not later fill the missing side with a local estimate,
			// which would inflate the billed token total.
			inputTokens = totalTokens
			inputObserved = true
			outputObserved = true
		case !inputObserved && totalTokens > outputTokens:
			inputTokens = totalTokens - outputTokens
			inputObserved = true
		case !outputObserved && totalTokens > inputTokens:
			outputTokens = totalTokens - inputTokens
			outputObserved = true
		}
	}
	if totalObserved && totalTokens == 0 {
		// A total of zero is an explicit upstream declaration. When one
		// direction is explicitly zero and the other is absent, the absent
		// direction is also known to be zero; do not turn it into an estimate.
		switch {
		case !inputObserved && !outputObserved:
			return 0, 0, true, true
		case !inputObserved && outputObserved && outputTokens == 0:
			return 0, 0, true, true
		case inputObserved && inputTokens == 0 && !outputObserved:
			return 0, 0, true, true
		}
	}
	if reasoning := reasoningTokensFromUsage(usage); reasoning > 0 {
		accounted := inputTokens + outputTokens
		switch {
		case totalObserved && totalTokens == accounted+reasoning:
			outputTokens += reasoning
			outputObserved = true
		case totalObserved && totalTokens == accounted:
			// reasoning is already inside completion/output tokens
		case totalObserved && totalTokens > accounted && totalTokens-accounted <= reasoning:
			outputTokens += totalTokens - accounted
			outputObserved = true
		case !outputObserved:
			outputTokens += reasoning
			outputObserved = true
		}
	}
	return inputTokens, outputTokens, inputObserved, outputObserved
}

func reasoningTokensFromUsage(usage map[string]any) int64 {
	if usage == nil {
		return 0
	}
	if value, ok := usageNumberPresent(usage["reasoning_tokens"]); ok && value > 0 {
		return value
	}
	for _, key := range []string{"completion_tokens_details", "output_tokens_details"} {
		details, _ := usage[key].(map[string]any)
		if details == nil {
			continue
		}
		if value, ok := usageNumberPresent(details["reasoning_tokens"]); ok && value > 0 {
			return value
		}
	}
	return 0
}

func firstTokenUsageValue(usage map[string]any, keys ...string) (int64, bool) {
	for _, key := range keys {
		if value, ok := usageNumberPresent(usage[key]); ok {
			return value, true
		}
	}
	return 0, false
}

func usageNumber(v any) int64 {
	value, _ := usageNumberPresent(v)
	return value
}

func usageNumberPresent(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int64:
		return n, true
	case float64:
		return int64(n), true
	case json.Number:
		return usageJSONNumber(n), true
	case string:
		if strings.TrimSpace(n) == "" {
			return 0, false
		}
		return usageJSONNumber(json.Number(strings.TrimSpace(n))), true
	default:
		return 0, false
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

func proxyRequestStartedAt(req *ProxyRequest) time.Time {
	if req != nil && !req.StartedAt.IsZero() {
		return req.StartedAt
	}
	return time.Now()
}

func proxyEffectiveCreditMultiplier(provider *llmpool.ProviderConfig, model *llmpool.DispatchModel, route llmpool.DispatchProviderRoute, startedAt time.Time) float64 {
	vendor := 1.0
	if provider != nil {
		vendor = llmpool.ResolveCreditMultiplier(provider.BillingPolicy(), startedAt)
	}
	return llmpool.CombineCreditMultipliers(vendor, proxyCreditMultiplierForRoute(model, route))
}

// proxyDispatchCreditMultiplier returns the combined provider time-of-use and
// service-group route multiplier that applies to a completed dispatch. A
// quoted request must use the value frozen in its quote, rather than resolve a
// provider schedule again while producing the response trailer.
func proxyDispatchCreditMultiplier(req *ProxyRequest, dispatch *proxyDispatch, startedAt time.Time) float64 {
	if dispatch == nil {
		return 0
	}
	providerID := ""
	if dispatch.provider != nil {
		providerID = dispatch.provider.ID
	}
	if dispatch.matchedGroup != nil && proxyRouteBillingMode(dispatch.matchedGroup, providerID, proxyUpstreamModelForRoute(dispatch.route, dispatch.provider, dispatch.model)) == llmpool.BillingModeFree {
		return 1
	}
	providerMultiplier := proxyProviderMultiplierForRequest(req, dispatch.provider, providerID, startedAt)
	return llmpool.CombineCreditMultipliers(providerMultiplier, proxyCreditMultiplierForRoute(dispatch.dispatchModel, dispatch.route))
}

func proxyCacheHitCreditMultiplier(reg *Registry, model *llmpool.DispatchModel, providerID string, startedAt time.Time) float64 {
	provider := findProvider(reg, providerID)
	if provider == nil {
		return 0
	}
	return proxyEffectiveCreditMultiplier(provider, model, proxyRouteForProvider(model, providerID), startedAt)
}

func proxyRouteForProvider(model *llmpool.DispatchModel, providerID string) llmpool.DispatchProviderRoute {
	providerID = strings.TrimSpace(providerID)
	if model != nil {
		for _, route := range model.ProviderRoutes {
			if strings.EqualFold(strings.TrimSpace(route.ProviderID), providerID) {
				return route
			}
		}
	}
	return llmpool.DispatchProviderRoute{ProviderID: providerID}
}

func proxySharedCreditMultiplier(req *ProxyRequest, dispatches []*proxyDispatch, startedAt time.Time) (float64, bool) {
	var shared float64
	found := false
	for _, dispatch := range dispatches {
		if dispatch == nil || dispatch.provider == nil {
			continue
		}
		multiplier := proxyDispatchCreditMultiplier(req, dispatch, startedAt)
		if !found {
			shared, found = multiplier, true
			continue
		}
		if multiplier != shared {
			return 0, false
		}
	}
	return shared, found
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
	multiplier = llmpool.NormalizeCreditMultiplier(multiplier)
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
