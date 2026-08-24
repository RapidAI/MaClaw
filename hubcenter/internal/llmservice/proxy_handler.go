package llmservice

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

// ProxyHandler returns an HTTP handler for the LLM proxy endpoint.
// POST /api/llm/v1/chat/completions
//
// Headers:
//   - Authorization: Bearer <hub_machine_token>  (validated upstream by hub auth middleware)
//   - X-Hub-ID: hub instance ID
//   - X-Tenant-ID: tenant ID on the hub
func ProxyHandler(cfg *ProxyConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		hubID := strings.TrimSpace(r.Header.Get("X-Hub-ID"))
		tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
		if hubID == "" || tenantID == "" {
			writeJSONError(w, http.StatusBadRequest, "X-Hub-ID and X-Tenant-ID headers are required")
			return
		}

		// Read body
		body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20)) // 10MB limit
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "failed to read request body")
			return
		}
		defer r.Body.Close()

		var parsed map[string]any
		if err := json.Unmarshal(body, &parsed); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}

		proxyReq := &ProxyRequest{
			HubID:          hubID,
			TenantID:       tenantID,
			RequestID:      strings.TrimSpace(r.Header.Get("X-MaClaw-Request-ID")),
			ServiceGroupID: strings.TrimSpace(r.Header.Get("X-MaClaw-Service-Group-ID")),
			Header:         r.Header.Clone(),
			Body:           parsed,
			RawBody:        body,
			StartedAt:      time.Now(),
		}
		if rawQuote := strings.TrimSpace(r.Header.Get(llmpool.PricingQuoteHeader)); rawQuote != "" {
			quote, ok := proxyQuoteFromRequest(cfg, rawQuote, proxyReq)
			if !ok {
				writeJSONError(w, http.StatusConflict, "pricing quote is invalid, expired, already used, or does not match this request")
				return
			}
			proxyReq.Quote = &quote
		}

		if proxyRequestWantsStream(parsed) {
			streamProxyRequest(w, r, cfg, proxyReq)
			return
		}

		resp, err := HandleProxyRequest(r.Context(), cfg, proxyReq)
		if err != nil {
			writeProxyRequestError(w, err)
			return
		}

		// Forward the upstream response as-is
		w.Header().Set("Content-Type", "application/json")
		if resp.CacheHit {
			w.Header().Set("X-Cache", "HIT")
		}
		if class := strings.TrimSpace(proxyReq.WorkloadClass); class != "" && class != llmpool.WorkloadUnclassified {
			w.Header().Set(llmpool.WorkloadClassHeader, class)
		}
		if resolved := strings.TrimSpace(proxyReq.Model); resolved != "" {
			w.Header().Set(llmpool.ResolvedModelHeader, resolved)
		}
		w.Header().Set(llmpool.ProviderIDHeader, resp.ProviderID)
		if resp.PricingSnapshot != nil {
			if encoded, ok := llmpool.EncodeTokenPricingSnapshot(*resp.PricingSnapshot); ok {
				w.Header().Set(llmpool.TokenPricingSnapshotHeader, encoded)
			}
		}
		if resp.CreditMultiplier > 0 {
			w.Header().Set(llmpool.CreditMultiplierHeader, llmpool.FormatCreditMultiplierHeader(resp.CreditMultiplier))
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(resp.Body)
	}
}

// ProxyQuoteHandler selects exactly one currently usable official route and
// freezes its resolved directional price. Hub must present the returned opaque
// token to /chat/completions; a quote never authorizes a different request.
func ProxyQuoteHandler(cfg *ProxyConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if cfg == nil || cfg.Quotes == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "pricing quotes are not configured")
			return
		}
		hubID := strings.TrimSpace(r.Header.Get("X-Hub-ID"))
		tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
		requestID := strings.TrimSpace(r.Header.Get("X-MaClaw-Request-ID"))
		if hubID == "" || tenantID == "" || requestID == "" {
			writeJSONError(w, http.StatusBadRequest, "X-Hub-ID, X-Tenant-ID, and X-MaClaw-Request-ID headers are required")
			return
		}
		bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "failed to read request body")
			return
		}
		var body map[string]any
		if err := json.Unmarshal(bodyBytes, &body); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		req := &ProxyRequest{
			HubID:          hubID,
			TenantID:       tenantID,
			RequestID:      requestID,
			ServiceGroupID: strings.TrimSpace(r.Header.Get("X-MaClaw-Service-Group-ID")),
			Header:         r.Header.Clone(),
			Body:           body,
			RawBody:        bodyBytes,
			StartedAt:      time.Now(),
		}
		// A quote commits Hub to one concrete route. For a streaming request it
		// must therefore be selected from the stream-capable routes now, rather
		// than discovering after Hub has reserved Credits that the quoted route
		// only supports a non-stream wire protocol.
		var dispatch *proxyDispatch
		if proxyRequestWantsStream(body) {
			dispatches, err := prepareProxyStreamDispatches(r.Context(), cfg, req)
			if err != nil {
				writeProxyRequestError(w, err)
				return
			}
			if len(dispatches) > 0 {
				dispatch = dispatches[0]
			}
		} else {
			dispatch, err = prepareProxyDispatch(r.Context(), cfg, req)
			if err != nil {
				writeProxyRequestError(w, err)
				return
			}
		}
		if dispatch == nil || dispatch.provider == nil || dispatch.matchedGroup == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "no provider available for pricing quote")
			return
		}
		upstreamModel := proxyUpstreamModelForRoute(dispatch.route, dispatch.provider, dispatch.model)
		pricing := proxyTokenPricingSnapshot(dispatch.matchedGroup, dispatch.provider, dispatch.provider.ID, upstreamModel, 0, 0, proxyRequestStartedAt(req))
		if pricing == nil {
			writeJSONError(w, http.StatusUnprocessableEntity, "quoted provider route has no directional Credits price")
			return
		}
		quote, err := cfg.Quotes.Put(ProxyQuote{
			RequestDigest:  proxyRequestDigest(bodyBytes),
			HubID:          hubID,
			TenantID:       tenantID,
			RequestID:      requestID,
			ServiceGroupID: dispatch.matchedGroup.ID,
			LogicalModel:   dispatch.model,
			ProviderID:     dispatch.provider.ID,
			UpstreamModel:  upstreamModel,
			Pricing:        pricing.Pricing,
			ExpiresAt:      time.Now().Add(proxyQuoteTTL),
		})
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Quote ProxyQuote `json:"quote"`
			Token string     `json:"token"`
		}{Quote: quote, Token: quote.Token})
	}
}

// ProxyBillingAttemptHandler lets the authenticated owning Hub recover the
// immutable final pricing/usage fact after a proxy response or SSE trailer was
// lost. The lookup is strictly scoped to Hub, tenant, and request ID.
func ProxyBillingAttemptHandler(cfg *ProxyConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if cfg == nil || cfg.Attempts == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "billing attempt reconciliation is not configured")
			return
		}
		hubID := strings.TrimSpace(r.Header.Get("X-Hub-ID"))
		tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
		requestID := strings.TrimSpace(r.PathValue("request_id"))
		if hubID == "" || tenantID == "" || requestID == "" {
			writeJSONError(w, http.StatusBadRequest, "X-Hub-ID, X-Tenant-ID, and request_id are required")
			return
		}
		attempt, ok, err := cfg.Attempts.GetContext(r.Context(), hubID, tenantID, requestID)
		if err != nil {
			writeJSONError(w, http.StatusServiceUnavailable, "billing attempt reconciliation is temporarily unavailable")
			return
		}
		if !ok {
			writeJSONError(w, http.StatusNotFound, "billing attempt not found")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"attempt": attempt})
	}
}

type proxyStreamResult struct {
	err      error
	dispatch *proxyDispatch
}

var proxyStreamHeartbeatInterval = 15 * time.Second

type proxyHTTPStreamWriter struct {
	mu      sync.Mutex
	w       http.ResponseWriter
	flusher http.Flusher
}

func (w *proxyHTTPStreamWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w.Write(p)
}

func (w *proxyHTTPStreamWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.flusher != nil {
		w.flusher.Flush()
	}
}

func proxyRequestWantsStream(body map[string]any) bool {
	v, ok := body["stream"]
	if !ok {
		return false
	}
	switch value := v.(type) {
	case bool:
		return value
	case string:
		return strings.EqualFold(strings.TrimSpace(value), "true")
	default:
		return false
	}
}

func streamProxyRequest(w http.ResponseWriter, r *http.Request, cfg *ProxyConfig, proxyReq *ProxyRequest) {
	ctx, cancel := context.WithCancel(WithUsageContext(r.Context(), proxyReq.HubID, proxyReq.TenantID))
	defer cancel()
	dispatches, err := prepareProxyStreamDispatches(ctx, cfg, proxyReq)
	if err != nil {
		writeProxyRequestError(w, err)
		return
	}

	flusher, _ := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Trailer", llmpool.CreditMultiplierHeader+", "+llmpool.ProviderIDHeader+", "+llmpool.TokenPricingSnapshotHeader)
	if class := strings.TrimSpace(proxyReq.WorkloadClass); class != "" && class != llmpool.WorkloadUnclassified {
		w.Header().Set(llmpool.WorkloadClassHeader, class)
	}
	if resolved := strings.TrimSpace(proxyReq.Model); resolved != "" {
		w.Header().Set(llmpool.ResolvedModelHeader, resolved)
	}
	if multiplier, ok := proxySharedCreditMultiplier(dispatches, proxyRequestStartedAt(proxyReq)); ok {
		w.Header().Set(llmpool.CreditMultiplierHeader, llmpool.FormatCreditMultiplierHeader(multiplier))
	}
	if len(dispatches) == 1 && dispatches[0] != nil && dispatches[0].provider != nil {
		w.Header().Set(llmpool.ProviderIDHeader, dispatches[0].provider.ID)
	}
	w.WriteHeader(http.StatusOK)
	if flusher != nil {
		flusher.Flush()
	}

	streamWriter := &proxyHTTPStreamWriter{w: w, flusher: flusher}
	resultCh := make(chan proxyStreamResult, 1)
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				select {
				case resultCh <- proxyStreamResult{err: fmt.Errorf("stream dispatch panic: %v", rec)}:
				default:
				}
			}
		}()
		dispatch, err := handleProxyStreamDispatches(ctx, cfg, proxyReq, streamWriter, dispatches)
		resultCh <- proxyStreamResult{err: err, dispatch: dispatch}
	}()

	finish := func(result proxyStreamResult, writeErrorPayload bool) {
		if writeErrorPayload && result.err != nil {
			writeProxyStreamError(streamWriter, http.StatusServiceUnavailable, result.err.Error())
		}
		writeProxyStreamBillingTrailers(w, proxyReq, result.dispatch)
		streamWriter.Flush()
	}

	ticker := time.NewTicker(proxyStreamHeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			cancel()
			finish(<-resultCh, false)
			return
		case <-ticker.C:
			if _, err := io.WriteString(streamWriter, ": ping\n\n"); err != nil {
				cancel()
				finish(<-resultCh, false)
				return
			}
			streamWriter.Flush()
		case result := <-resultCh:
			finish(result, true)
			return
		}
	}
}

func writeProxyStreamBillingTrailers(w http.ResponseWriter, req *ProxyRequest, dispatch *proxyDispatch) {
	if w == nil || dispatch == nil || dispatch.provider == nil {
		return
	}
	// Directional pricing exposes only the service-group route markup; legacy
	// routes retain their combined provider × route multiplier.
	multiplier := proxyDispatchCreditMultiplier(dispatch, proxyRequestStartedAt(req))
	w.Header().Set(llmpool.CreditMultiplierHeader, llmpool.FormatCreditMultiplierHeader(multiplier))
	w.Header().Set(llmpool.ProviderIDHeader, dispatch.provider.ID)
	upstreamModel := proxyUpstreamModelForRoute(dispatch.route, dispatch.provider, dispatch.model)
	if snapshot := proxyDispatchTokenPricingSnapshot(req, dispatch, upstreamModel); snapshot != nil {
		if encoded, ok := llmpool.EncodeTokenPricingSnapshot(*snapshot); ok {
			w.Header().Set(llmpool.TokenPricingSnapshotHeader, encoded)
		}
	}
}

func proxyDispatchTokenPricingSnapshot(req *ProxyRequest, dispatch *proxyDispatch, upstreamModel string) *llmpool.TokenPricingSnapshot {
	if dispatch == nil || dispatch.provider == nil {
		return nil
	}
	if dispatch.pricing != nil {
		return &llmpool.TokenPricingSnapshot{
			ProviderID:    dispatch.provider.ID,
			UpstreamModel: strings.TrimSpace(upstreamModel),
			Pricing:       *dispatch.pricing,
			InputTokens:   dispatch.billingInputTokens,
			OutputTokens:  dispatch.billingOutputTokens,
		}
	}
	return proxyRequestTokenPricingSnapshot(req, dispatch.matchedGroup, dispatch.provider, dispatch.provider.ID, upstreamModel, dispatch.billingInputTokens, dispatch.billingOutputTokens, proxyRequestStartedAt(req))
}

func proxyQuoteFromRequest(cfg *ProxyConfig, token string, req *ProxyRequest) (ProxyQuote, bool) {
	if cfg == nil || cfg.Quotes == nil || req == nil {
		return ProxyQuote{}, false
	}
	return cfg.Quotes.Claim(token, req.HubID, req.TenantID, req.RequestID, proxyRequestDigest(req.RawBody))
}

func writeProxyRequestError(w http.ResponseWriter, err error) {
	if bound := asTenantBoundError(err); bound != nil {
		writeBindingRedirectError(w, bound)
		return
	}
	errMsg := err.Error()
	if strings.Contains(errMsg, "authorization denied") {
		writeJSONError(w, http.StatusForbidden, errMsg)
		return
	}
	if strings.Contains(errMsg, "bound to node") {
		writeBindingRedirectError(w, &TenantBoundError{NodeID: parseBoundNodeID(errMsg)})
		return
	}
	if strings.Contains(errMsg, "not available") || strings.Contains(errMsg, "not specified") || strings.Contains(errMsg, "no stream-capable providers") {
		writeJSONError(w, http.StatusBadRequest, errMsg)
		return
	}
	if strings.Contains(errMsg, "all providers failed") {
		writeJSONError(w, http.StatusServiceUnavailable, errMsg)
		return
	}
	writeJSONError(w, http.StatusInternalServerError, errMsg)
}

func writeProxyStreamError(w io.Writer, code int, message string) {
	if strings.TrimSpace(message) == "" {
		message = http.StatusText(code)
	}
	payload := map[string]any{
		"error": map[string]any{
			"message": message,
			"code":    code,
		},
	}
	writeProxySSEData(w, payload)
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
}

func writeProxySSEData(w io.Writer, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_, _ = io.WriteString(w, "data: ")
	_, _ = w.Write(data)
	_, _ = io.WriteString(w, "\n\n")
}

type TenantAuthorizationStatus struct {
	HubID                  string                          `json:"hub_id"`
	TenantID               string                          `json:"tenant_id"`
	LookupTenantIDs        []string                        `json:"lookup_tenant_ids,omitempty"`
	AllowExternalProviders bool                            `json:"allow_external_providers"`
	Authorizations         []TenantAuthorizationSummary    `json:"authorizations,omitempty"`
	ProviderBilling        []llmpool.ProviderBillingPolicy `json:"provider_billing,omitempty"`
}

type TenantAuthorizationSummary struct {
	ID                     string  `json:"id"`
	HubID                  string  `json:"hub_id,omitempty"`
	TenantID               string  `json:"tenant_id,omitempty"`
	ServiceGroupID         string  `json:"service_group_id"`
	CreditsTotal           float64 `json:"credits_total"`
	CreditsUsed            float64 `json:"credits_used"`
	CreditsRemaining       float64 `json:"credits_remaining"`
	StartsAt               string  `json:"starts_at"`
	ExpiresAt              string  `json:"expires_at"`
	Status                 string  `json:"status"`
	Active                 bool    `json:"active"`
	AllowExternalProviders bool    `json:"allow_external_providers"`
	Source                 string  `json:"source"`
	CardOrderID            string  `json:"card_order_id,omitempty"`
}

var (
	providerBillingCatalogMu sync.RWMutex
	providerBillingCatalog   func(context.Context) []llmpool.ProviderBillingPolicy
)

// SetProviderBillingCatalog installs the HubCenter provider billing publisher
// used when syncing official MaClaw rates to Hub.
func SetProviderBillingCatalog(fn func(context.Context) []llmpool.ProviderBillingPolicy) {
	providerBillingCatalogMu.Lock()
	providerBillingCatalog = fn
	providerBillingCatalogMu.Unlock()
}

func CurrentProviderBillingCatalog(ctx context.Context) []llmpool.ProviderBillingPolicy {
	return currentProviderBillingCatalog(ctx)
}

func currentProviderBillingCatalog(ctx context.Context) []llmpool.ProviderBillingPolicy {
	providerBillingCatalogMu.RLock()
	fn := providerBillingCatalog
	providerBillingCatalogMu.RUnlock()
	if fn == nil {
		return nil
	}
	return fn(ctx)
}

func BuildTenantAuthorizationStatus(ctx context.Context, checker *AuthorizationChecker, hubID, tenantID string) (*TenantAuthorizationStatus, error) {
	auths, err := checker.ListByHubTenantAliases(ctx, hubID, tenantID)
	if err != nil {
		return nil, err
	}

	current := now()
	result := &TenantAuthorizationStatus{
		HubID:           hubID,
		TenantID:        tenantID,
		LookupTenantIDs: tenantAuthorizationLookupIDs(tenantID),
	}
	if _, allowed := latestExternalProviderAuthorizationState(auths, current); allowed {
		result.AllowExternalProviders = true
	}
	if policies := currentProviderBillingCatalog(ctx); len(policies) > 0 {
		result.ProviderBilling = policies
	}
	for _, a := range auths {
		active := a.IsActive(current)
		// External compute permission records are pure permission grants,
		// not credit-based quotas. They remain active as long as status is
		// "active" and the time window is valid, regardless of credits.
		if !active && isExternalComputePermissionRecord(a) && isTimeWindowActive(a, current) {
			active = true
		}
		if active && isExternalComputePermissionRecord(a) {
			continue
		}
		result.Authorizations = append(result.Authorizations, TenantAuthorizationSummary{
			ID:                     a.ID,
			HubID:                  a.HubID,
			TenantID:               a.TenantID,
			ServiceGroupID:         a.ServiceGroupID,
			CreditsTotal:           roundTenantAuthorizationStatusCredits(a.CreditsTotal),
			CreditsUsed:            roundTenantAuthorizationStatusCredits(a.CreditsUsed),
			CreditsRemaining:       roundTenantAuthorizationStatusCredits(a.CreditsRemaining()),
			StartsAt:               a.StartsAt.Format(time.RFC3339),
			ExpiresAt:              a.ExpiresAt.Format(time.RFC3339),
			Status:                 a.Status,
			Active:                 active,
			AllowExternalProviders: a.AllowExternalProviders,
			Source:                 a.Source,
			CardOrderID:            a.CardOrderID,
		})
	}
	return result, nil
}

func roundTenantAuthorizationStatusCredits(v float64) float64 {
	return math.Round(v*10000) / 10000
}

// isExternalComputePermissionRecord returns true for records that represent
// a permission grant (not a credit-based quota). These records should not be
// gated by CreditsRemaining > 0.
func isExternalComputePermissionRecord(a *TenantAuthorization) bool {
	if a == nil {
		return false
	}
	return sameServiceGroupID(a.ServiceGroupID, ExternalComputePermissionServiceGroupID) ||
		a.Source == "external_provider_permission"
}

// isTimeWindowActive checks if the record is within its time validity window
// and not explicitly expired/exhausted. Does NOT check credits.
func isTimeWindowActive(a *TenantAuthorization, now time.Time) bool {
	if a == nil {
		return false
	}
	if a.Status == "expired" || a.Status == "exhausted" {
		return false
	}
	// Treat zero StartsAt as "always started".
	if !a.StartsAt.IsZero() && now.Before(a.StartsAt) {
		return false
	}
	// Treat zero ExpiresAt as "never expires".
	if !a.ExpiresAt.IsZero() && now.After(a.ExpiresAt) {
		return false
	}
	return true
}

// AuthorizationQueryHandler returns an HTTP handler for querying tenant authorization status.
// GET /api/llm/v1/authorization?hub_id=X&tenant_id=Y
func AuthorizationQueryHandler(checker *AuthorizationChecker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hubID := strings.TrimSpace(r.URL.Query().Get("hub_id"))
		tenantID := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
		if hubID == "" || tenantID == "" {
			writeJSONError(w, http.StatusBadRequest, "hub_id and tenant_id query params are required")
			return
		}

		result, err := BuildTenantAuthorizationStatus(r.Context(), checker, hubID, tenantID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

// AuthorizationBatchQueryHandler returns authorization status for multiple tenants.
// POST /api/llm/v1/authorization/batch
func AuthorizationBatchQueryHandler(checker *AuthorizationChecker) http.HandlerFunc {
	type batchRequest struct {
		TenantIDs []string `json:"tenant_ids"`
	}
	type batchResponse struct {
		Tenants map[string]*TenantAuthorizationStatus `json:"tenants,omitempty"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		hubID := strings.TrimSpace(r.Header.Get("X-Hub-ID"))
		if hubID == "" {
			writeJSONError(w, http.StatusBadRequest, "X-Hub-ID header is required")
			return
		}

		var req batchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}

		resp := batchResponse{Tenants: map[string]*TenantAuthorizationStatus{}}
		seen := map[string]struct{}{}
		for _, rawTenantID := range req.TenantIDs {
			tenantID := strings.TrimSpace(rawTenantID)
			if tenantID == "" {
				continue
			}
			if _, ok := seen[tenantID]; ok {
				continue
			}
			seen[tenantID] = struct{}{}
			status, err := BuildTenantAuthorizationStatus(r.Context(), checker, hubID, tenantID)
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, err.Error())
				return
			}
			resp.Tenants[tenantID] = status
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func writeJSONError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"message": message,
			"code":    code,
		},
	})
}

func writeBindingRedirectError(w http.ResponseWriter, bound *TenantBoundError) {
	if bound == nil {
		writeJSONError(w, http.StatusConflict, "tenant bound to another node, please redirect")
		return
	}
	nodeID := strings.TrimSpace(bound.NodeID)
	redirectURL := clientFacingRedirectURL(bound.RedirectURL)
	if nodeID != "" {
		w.Header().Set(RedirectNodeHeader, nodeID)
	}
	if redirectURL != "" {
		w.Header().Set(RedirectURLHeader, redirectURL)
		w.Header().Set("Location", redirectURL)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusConflict)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"code":         TenantBoundErrorCode,
		"node_id":      nodeID,
		"redirect_url": redirectURL,
		"error": map[string]any{
			"message":      bound.Error(),
			"code":         TenantBoundErrorCode,
			"node_id":      nodeID,
			"redirect_url": redirectURL,
		},
	})
}

func parseBoundNodeID(msg string) string {
	const marker = "tenant bound to node "
	lower := strings.ToLower(msg)
	idx := strings.Index(lower, marker)
	if idx < 0 {
		return ""
	}
	rest := strings.TrimSpace(msg[idx+len(marker):])
	if rest == "" {
		return ""
	}
	end := len(rest)
	for i, r := range rest {
		if r == ',' || r == ' ' || r == ';' || r == '"' {
			end = i
			break
		}
	}
	return strings.TrimSpace(rest[:end])
}

func now() time.Time {
	return time.Now().UTC()
}
