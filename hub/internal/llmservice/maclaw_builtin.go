package llmservice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	corellm "github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

// ---------------------------------------------------------------------------
// Built-in MaClaw Official constants
// ---------------------------------------------------------------------------

const (
	MaClawOfficialProviderID       = "maclaw_official"
	MaClawOfficialProviderName     = "MaClaw 官方"
	MaClawOfficialServiceGroupID   = llmpool.HubOfficialServiceGroupID
	MaClawOfficialServiceGroupName = "MaClaw 官方服务组"
)

// IsBuiltinProvider returns true for the MaClaw Official provider that routes
// through HubCenter instead of the local provider registry.
func IsBuiltinProvider(id string) bool {
	return strings.EqualFold(strings.TrimSpace(id), MaClawOfficialProviderID)
}

// HasBuiltinProviderRoute returns true if the registry contains at least one
// model service group with a model routed through a built-in provider.
// This is the single authoritative check for "built-in LLM service is configured
// in this registry" — used by both the admin API (service_available field) and
// any future readiness/health checks.
func HasBuiltinProviderRoute(reg *Registry) bool {
	if reg == nil {
		return false
	}
	for i := range reg.ModelServiceGroups {
		for j := range reg.ModelServiceGroups[i].Models {
			for _, pid := range reg.ModelServiceGroups[i].Models[j].ProviderIDs {
				if IsBuiltinProvider(pid) {
					return true
				}
			}
		}
	}
	return false
}

// IsBuiltinServiceGroup returns true for the default MaClaw Official service
// group. The group still follows normal service-group billing/access rules.
func IsBuiltinServiceGroup(id string) bool {
	return llmpool.IsHubOfficialServiceGroup(id)
}

// ---------------------------------------------------------------------------
// MaClaw Official Provider — forwards LLM requests to HubCenter
// ---------------------------------------------------------------------------

// MaClawProviderConfig holds the connection details for the MaClaw Official provider.
type MaClawProviderConfig struct {
	HubCenterURL string // bound HubCenter node URL
	HubID        string // this Hub's ID
	TenantID     string // current tenant
	MachineToken string // Hub's machine token for auth
	TimeoutSec   int    // upstream request timeout in seconds (default 600, min 300)
}

// MaClawProviderClient forwards LLM requests to HubCenter's LLM proxy.
type MaClawProviderClient struct {
	Config     MaClawProviderConfig
	HTTPClient *http.Client

	mu                 sync.RWMutex
	boundURL           string               // preferred HubCenter URL for unpinned tenants
	candidateURLs      []string             // ordered HubCenter failover candidates
	tenantBound        map[string]tenantPin // tenantID -> owner HubCenter URL
	nodeURLs           map[string]string    // nodeID -> HubCenter URL
	ownerCooldown      map[string]time.Time // owner URL -> last unreachable time
	failureCount       int
	lastFailureAt      time.Time
	refreshCredentials func() (hubID, hubSecret string) // lazy refresh after registration
}

type tenantPin struct {
	URL      string
	PinnedAt time.Time
}

const (
	officialTenantBoundCode    = "TENANT_BOUND_TO_NODE"
	officialRedirectNodeHeader = "X-MaClaw-Redirect-Node"
	officialRedirectURLHeader  = "X-MaClaw-Redirect-URL"
)

// officialTenantPinTTL matches HubCenter's LLM node binding lease.
var officialTenantPinTTL = 10 * time.Minute

// officialOwnerCooldown skips a just-failed owner so the next request
// does not wait out the full upstream timeout on a dead node.
var officialOwnerCooldown = 30 * time.Second

// officialOwnerProbeTimeout bounds the cheap reachability check before a
// 409 redirect hops to the bound owner. The real LLM POST still uses the
// full official timeout so healthy thinking responses are not cut short.
var officialOwnerProbeTimeout = 3 * time.Second

// officialAdminRequestTimeout bounds HubCenter GETs that must not inherit
// the 600s official LLM timeout (authorization and class-head pulls).
var officialAdminRequestTimeout = 10 * time.Second

const maxOfficialHubCenterAttempts = 8

// NewMaClawProviderClient creates a new MaClaw Official provider client.
func NewMaClawProviderClient(cfg MaClawProviderConfig) *MaClawProviderClient {
	// Default upstream timeout: 600s. DeepSeek V4 Flash thinking mode can spend
	// 60-180s in reasoning before producing any output; complex tasks need more.
	// Minimum recommended: 300s. Configurable via MaClawProviderConfig.TimeoutSec.
	timeout := 600
	if cfg.TimeoutSec > 0 {
		timeout = cfg.TimeoutSec
		if timeout < 300 {
			timeout = 300 // enforce minimum
		}
	}
	client := &http.Client{Timeout: time.Duration(timeout) * time.Second}
	return &MaClawProviderClient{
		Config:        cfg,
		HTTPClient:    client,
		boundURL:      cfg.HubCenterURL,
		candidateURLs: normalizeHubCenterURLs([]string{cfg.HubCenterURL}),
		tenantBound:   map[string]tenantPin{},
		nodeURLs:      map[string]string{},
		ownerCooldown: map[string]time.Time{},
	}
}

// ConfigSnapshot returns a copy of the provider config under the client lock.
func (c *MaClawProviderClient) ConfigSnapshot() MaClawProviderConfig {
	if c == nil {
		return MaClawProviderConfig{}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Config
}

// UpdateTimeout updates the HTTP client timeout at runtime (e.g. when admin saves settings).
// Replaces the HTTP client instance to avoid data races on the Timeout field.
func (c *MaClawProviderClient) UpdateTimeout(timeoutSec int) {
	if c == nil {
		return
	}
	if timeoutSec < 300 {
		timeoutSec = 300
	}
	c.mu.Lock()
	c.Config.TimeoutSec = timeoutSec
	c.HTTPClient = &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	c.mu.Unlock()
}

func (c *MaClawProviderClient) ensureCredentials() (hubID, token string) {
	c.mu.RLock()
	hubID = c.Config.HubID
	token = c.Config.MachineToken
	refresh := c.refreshCredentials
	c.mu.RUnlock()

	if hubID != "" && token != "" {
		return hubID, token
	}
	if refresh == nil {
		return hubID, token
	}

	newID, newToken := refresh()
	if newID == "" || newToken == "" {
		return hubID, token
	}

	c.mu.Lock()
	c.Config.HubID = newID
	c.Config.MachineToken = newToken
	c.mu.Unlock()
	return newID, newToken
}

// OfficialForwardResult is a HubCenter proxy response plus billing metadata.
type OfficialForwardResult struct {
	Body             []byte
	StatusCode       int
	Header           http.Header
	CreditMultiplier float64
	ProviderID       string
	// NoUpstreamDispatch is true only when this client can prove it did not
	// send the quoted request to HubCenter. Hub uses it to release a sent
	// admission reservation without weakening protection for network-ambiguous
	// failures after http.Client.Do has begun.
	NoUpstreamDispatch bool
}

// OfficialPricingQuote is HubCenter's short-lived commitment to one concrete
// provider route and one resolved directional price. Token is transport-only:
// callers must never store it in a billing ledger or expose it to users.
type OfficialPricingQuote struct {
	Token              string                       `json:"token"`
	ProviderID         string                       `json:"provider_id"`
	UpstreamModel      string                       `json:"upstream_model"`
	Pricing            llmpool.ResolvedTokenPricing `json:"pricing"`
	ProviderMultiplier float64                      `json:"provider_multiplier"`
	ExpiresAt          time.Time                    `json:"expires_at"`
	targetURL          string
}

// OfficialBillingAttempt is HubCenter's authenticated reconciliation response.
// It contains only the final metering snapshot required to settle a previously
// sent Hub reservation; it intentionally excludes request/response content and
// the opaque quote token.
type OfficialBillingAttempt struct {
	StatusCode      int                          `json:"status_code"`
	ProviderID      string                       `json:"provider_id"`
	PricingSnapshot llmpool.TokenPricingSnapshot `json:"pricing_snapshot"`
	CompletedAt     time.Time                    `json:"completed_at"`
}

// Forward sends an LLM request to HubCenter and returns the response.
func (c *MaClawProviderClient) Forward(ctx context.Context, body []byte, tenantID string, serviceGroupIDs ...string) ([]byte, int, error) {
	result, err := c.ForwardDetailed(ctx, body, tenantID, serviceGroupIDs...)
	return result.Body, result.StatusCode, err
}

// ForwardDetailed is Forward plus HubCenter billing headers.
func (c *MaClawProviderClient) ForwardDetailed(ctx context.Context, body []byte, tenantID string, serviceGroupIDs ...string) (OfficialForwardResult, error) {
	httpClient := c.httpClient()
	targets := c.orderedTargets(tenantID)
	if len(targets) == 0 {
		return OfficialForwardResult{}, fmt.Errorf("maclaw official provider: no HubCenter URL configured")
	}
	hubID, token := c.ensureCredentials()
	if hubID == "" || token == "" {
		return OfficialForwardResult{}, fmt.Errorf("maclaw official provider: hub not registered to HubCenter yet")
	}

	tried := make(map[string]struct{}, len(targets))
	var last OfficialForwardResult
	var lastErr error
	var requiredOwner, requiredNodeID string
	for attempts := 0; len(targets) > 0 && attempts < maxOfficialHubCenterAttempts; {
		target := targets[0]
		targets = targets[1:]
		key := normalizeHubCenterURLOne(target)
		if key == "" {
			continue
		}
		if _, ok := tried[key]; ok {
			continue
		}
		tried[key] = struct{}{}
		attempts++
		result, err := c.forwardTo(ctx, httpClient, target, body, hubID, token, tenantID, serviceGroupIDs...)
		last, lastErr = result, err
		if err == nil {
			if redirect, ok := parseHubCenterBindingRedirect(result.StatusCode, result.Body, result.Header); ok {
				next, owner, stop := c.applyBindingRedirect(ctx, tenantID, redirect, tried, targets)
				if stop != nil {
					return OfficialForwardResult{}, c.failRequiredOwnerUnlessCanceled(ctx, redirect.NodeID, owner, stop)
				}
				if owner != "" {
					requiredOwner = owner
					requiredNodeID = firstNonEmptyString(redirect.NodeID, requiredNodeID)
				}
				targets = next
				continue
			}
		}
		if !shouldFailoverHubCenter(result.StatusCode, result.Body, err) {
			c.rememberSuccessfulTarget(tenantID, target)
			return result, nil
		}
		if requiredOwner != "" && sameHubCenterURL(key, requiredOwner) {
			return OfficialForwardResult{}, c.failRequiredOwnerUnlessCanceled(ctx, requiredNodeID, requiredOwner, err)
		}
		log.Printf("[maclaw-provider] LLM upstream failed hubcenter=%s status=%d err=%v; trying next candidate", target, result.StatusCode, err)
	}
	c.recordFailure()
	if last.StatusCode == http.StatusConflict {
		if redirect, ok := parseHubCenterBindingRedirect(last.StatusCode, last.Body, last.Header); ok {
			return OfficialForwardResult{}, bindingLoopFailure(redirect.NodeID, redirect.RedirectURL, lastErr)
		}
		if lastErr == nil {
			lastErr = fmt.Errorf("maclaw official: hubcenter HTTP %d", last.StatusCode)
		}
		return OfficialForwardResult{}, lastErr
	}
	return last, lastErr
}

// Quote requests a HubCenter route/price commitment. A successful quote must
// be consumed by ForwardDetailedWithQuote rather than the ordinary failover
// path, otherwise the final provider price could differ from Hub's admission
// check.
func (c *MaClawProviderClient) Quote(ctx context.Context, body []byte, tenantID string, serviceGroupIDs ...string) (OfficialPricingQuote, error) {
	httpClient := c.httpClient()
	targets := c.orderedTargets(tenantID)
	if len(targets) == 0 {
		return OfficialPricingQuote{}, fmt.Errorf("maclaw official provider: no HubCenter URL configured")
	}
	hubID, token := c.ensureCredentials()
	if hubID == "" || token == "" {
		return OfficialPricingQuote{}, fmt.Errorf("maclaw official provider: hub not registered to HubCenter yet")
	}
	var lastErr error
	for _, target := range targets {
		quote, status, err := c.quoteTo(ctx, httpClient, target, body, hubID, token, tenantID, serviceGroupIDs...)
		if err == nil && status >= 200 && status < 300 {
			quote.targetURL = target
			return quote, nil
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("maclaw official: quote HTTP %d", status)
		}
		// Application errors must remain visible to Hub; only a transport error
		// can safely move the never-consumed quote request to another node.
		if err == nil {
			break
		}
	}
	return OfficialPricingQuote{}, lastErr
}

// BillingAttempt retrieves a final official billing snapshot after the original
// response became unavailable to Hub. It does not retry or replay upstream work.
func (c *MaClawProviderClient) BillingAttempt(ctx context.Context, tenantID, requestID string) (OfficialBillingAttempt, int, error) {
	httpClient := c.httpClient()
	targets := c.orderedTargets(tenantID)
	if len(targets) == 0 {
		return OfficialBillingAttempt{}, 0, fmt.Errorf("maclaw official provider: no HubCenter URL configured")
	}
	hubID, token := c.ensureCredentials()
	if hubID == "" || token == "" {
		return OfficialBillingAttempt{}, 0, fmt.Errorf("maclaw official provider: hub not registered to HubCenter yet")
	}
	var lastErr error
	for _, target := range targets {
		endpoint := strings.TrimRight(target, "/") + "/api/llm/v1/billing-attempts/" + url.PathEscape(strings.TrimSpace(requestID))
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return OfficialBillingAttempt{}, 0, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Hub-ID", hubID)
		req.Header.Set("X-Tenant-ID", tenantID)
		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		var payload struct {
			Attempt OfficialBillingAttempt `json:"attempt"`
		}
		decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&payload)
		_ = resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 && decodeErr == nil {
			return payload.Attempt, resp.StatusCode, nil
		}
		if decodeErr != nil {
			lastErr = fmt.Errorf("maclaw official: decode billing attempt: %w", decodeErr)
		} else {
			lastErr = fmt.Errorf("maclaw official: billing attempt HTTP %d", resp.StatusCode)
		}
		if resp.StatusCode < 500 {
			return OfficialBillingAttempt{}, resp.StatusCode, lastErr
		}
	}
	return OfficialBillingAttempt{}, 0, lastErr
}

func (c *MaClawProviderClient) ForwardDetailedWithQuote(ctx context.Context, quote OfficialPricingQuote, body []byte, tenantID string, serviceGroupIDs ...string) (OfficialForwardResult, error) {
	if strings.TrimSpace(quote.Token) == "" || strings.TrimSpace(quote.targetURL) == "" || !quote.ExpiresAt.After(time.Now().UTC()) {
		return OfficialForwardResult{NoUpstreamDispatch: true}, fmt.Errorf("maclaw official provider: invalid or expired pricing quote")
	}
	hubID, token := c.ensureCredentials()
	if hubID == "" || token == "" {
		return OfficialForwardResult{NoUpstreamDispatch: true}, fmt.Errorf("maclaw official provider: hub not registered to HubCenter yet")
	}
	return c.forwardToWithQuote(ctx, c.httpClient(), quote.targetURL, body, hubID, token, tenantID, quote.Token, serviceGroupIDs...)
}

// shouldFailoverHubCenter only treats failures before a HubCenter application
// response as node failures. A JSON 5xx is a real HubCenter/provider error and
// is returned unchanged: replaying it on another node can duplicate LLM usage
// or tool side effects. Reverse-proxy HTML/text 5xx responses are safe to try
// on another configured HubCenter node.
func shouldFailoverHubCenter(status int, responseBody []byte, err error) bool {
	if err != nil || status == http.StatusConflict {
		return true
	}
	if status < 500 {
		return false
	}
	return !json.Valid(bytes.TrimSpace(responseBody))
}

func (c *MaClawProviderClient) forwardTo(ctx context.Context, httpClient *http.Client, targetURL string, body []byte, hubID, token, tenantID string, serviceGroupIDs ...string) (OfficialForwardResult, error) {
	endpoint := strings.TrimRight(targetURL, "/") + "/api/llm/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return OfficialForwardResult{}, fmt.Errorf("maclaw official: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Hub-ID", hubID)
	req.Header.Set("X-Tenant-ID", tenantID)
	if serviceGroupID := firstNonEmptyServiceGroupID(serviceGroupIDs); serviceGroupID != "" {
		req.Header.Set("X-MaClaw-Service-Group-ID", serviceGroupID)
	}
	applyOfficialForwardMeta(req, ctx)
	resp, err := httpClient.Do(req)
	if err != nil {
		return OfficialForwardResult{}, fmt.Errorf("maclaw official: forward failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	multiplier, providerID := officialForwardBilling(resp)
	if err != nil {
		return OfficialForwardResult{Body: respBody, StatusCode: resp.StatusCode, CreditMultiplier: multiplier, ProviderID: providerID}, fmt.Errorf("maclaw official: read response: %w", err)
	}
	return OfficialForwardResult{
		Body:             respBody,
		StatusCode:       resp.StatusCode,
		Header:           resp.Header.Clone(),
		CreditMultiplier: multiplier,
		ProviderID:       providerID,
	}, nil
}

func (c *MaClawProviderClient) forwardToWithQuote(ctx context.Context, httpClient *http.Client, targetURL string, body []byte, hubID, token, tenantID, quoteToken string, serviceGroupIDs ...string) (OfficialForwardResult, error) {
	endpoint := strings.TrimRight(targetURL, "/") + "/api/llm/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return OfficialForwardResult{NoUpstreamDispatch: true}, fmt.Errorf("maclaw official: create quoted request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Hub-ID", hubID)
	req.Header.Set("X-Tenant-ID", tenantID)
	req.Header.Set(llmpool.PricingQuoteHeader, quoteToken)
	if serviceGroupID := firstNonEmptyServiceGroupID(serviceGroupIDs); serviceGroupID != "" {
		req.Header.Set("X-MaClaw-Service-Group-ID", serviceGroupID)
	}
	applyOfficialForwardMeta(req, ctx)
	resp, err := httpClient.Do(req)
	if err != nil {
		return OfficialForwardResult{}, fmt.Errorf("maclaw official: quoted forward failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, readErr := io.ReadAll(resp.Body)
	multiplier, providerID := officialForwardBilling(resp)
	result := OfficialForwardResult{Body: respBody, StatusCode: resp.StatusCode, Header: resp.Header.Clone(), CreditMultiplier: multiplier, ProviderID: providerID}
	if readErr != nil {
		return result, fmt.Errorf("maclaw official: read quoted response: %w", readErr)
	}
	return result, nil
}

func (c *MaClawProviderClient) quoteTo(ctx context.Context, httpClient *http.Client, targetURL string, body []byte, hubID, token, tenantID string, serviceGroupIDs ...string) (OfficialPricingQuote, int, error) {
	endpoint := strings.TrimRight(targetURL, "/") + "/api/llm/v1/quotes"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return OfficialPricingQuote{}, 0, fmt.Errorf("maclaw official: create quote request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Hub-ID", hubID)
	req.Header.Set("X-Tenant-ID", tenantID)
	if serviceGroupID := firstNonEmptyServiceGroupID(serviceGroupIDs); serviceGroupID != "" {
		req.Header.Set("X-MaClaw-Service-Group-ID", serviceGroupID)
	}
	applyOfficialForwardMeta(req, ctx)
	resp, err := httpClient.Do(req)
	if err != nil {
		return OfficialPricingQuote{}, 0, fmt.Errorf("maclaw official: quote request failed: %w", err)
	}
	defer resp.Body.Close()
	var payload struct {
		Quote struct {
			ProviderID         string                       `json:"provider_id"`
			UpstreamModel      string                       `json:"upstream_model"`
			Pricing            llmpool.ResolvedTokenPricing `json:"pricing"`
			ProviderMultiplier float64                      `json:"provider_multiplier"`
			ExpiresAt          time.Time                    `json:"expires_at"`
		} `json:"quote"`
		Token string `json:"token"`
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return OfficialPricingQuote{}, resp.StatusCode, nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&payload); err != nil {
		return OfficialPricingQuote{}, resp.StatusCode, fmt.Errorf("maclaw official: decode quote response: %w", err)
	}
	if strings.TrimSpace(payload.Token) == "" || strings.TrimSpace(payload.Quote.ProviderID) == "" || payload.Quote.ExpiresAt.IsZero() {
		return OfficialPricingQuote{}, resp.StatusCode, fmt.Errorf("maclaw official: malformed quote response")
	}
	return OfficialPricingQuote{Token: payload.Token, ProviderID: payload.Quote.ProviderID, UpstreamModel: payload.Quote.UpstreamModel, Pricing: payload.Quote.Pricing, ProviderMultiplier: llmpool.NormalizeCreditMultiplier(payload.Quote.ProviderMultiplier), ExpiresAt: payload.Quote.ExpiresAt}, resp.StatusCode, nil
}

func officialForwardBilling(resp *http.Response) (float64, string) {
	if resp == nil {
		return 0, ""
	}
	multiplier := creditMultiplierFromHeader(resp.Header)
	providerID := strings.TrimSpace(resp.Header.Get(llmpool.ProviderIDHeader))
	if v := creditMultiplierFromHeader(resp.Trailer); v > 0 {
		multiplier = v
	}
	if id := strings.TrimSpace(resp.Trailer.Get(llmpool.ProviderIDHeader)); id != "" {
		providerID = id
	}
	return multiplier, providerID
}

func creditMultiplierFromHeader(header http.Header) float64 {
	if header == nil {
		return 0
	}
	return llmpool.ParseCreditMultiplierHeader(header.Get(llmpool.CreditMultiplierHeader))
}

// SetHubCenterCandidates configures the cluster members used after a retryable
// upstream failure. The current bound URL is always tried first.
func (c *MaClawProviderClient) SetHubCenterCandidates(urls []string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.candidateURLs = normalizeHubCenterURLs(urls)
	c.mu.Unlock()
}

func normalizeHubCenterURLs(urls []string) []string {
	seen := make(map[string]struct{}, len(urls))
	out := make([]string, 0, len(urls))
	for _, raw := range urls {
		u := strings.TrimRight(strings.TrimSpace(raw), "/")
		if u == "" {
			continue
		}
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}
	return out
}

func orderedHubCenterURLs(current string, candidates []string) []string {
	return normalizeHubCenterURLs(append([]string{current}, candidates...))
}

func normalizeHubCenterURLOne(raw string) string {
	urls := normalizeHubCenterURLs([]string{raw})
	if len(urls) == 0 {
		return ""
	}
	return urls[0]
}

func (c *MaClawProviderClient) orderedTargets(tenantID string) []string {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	start := c.boundURL
	if pinned := c.liveTenantPinLocked(strings.TrimSpace(tenantID), now); pinned != "" && !c.coolingDownLocked(pinned, now) {
		start = pinned
	}
	ordered := orderedHubCenterURLs(start, append([]string(nil), c.candidateURLs...))
	filtered := make([]string, 0, len(ordered))
	for _, raw := range ordered {
		if !c.coolingDownLocked(raw, now) {
			filtered = append(filtered, raw)
		}
	}
	if len(filtered) == 0 {
		return ordered
	}
	return filtered
}

func (c *MaClawProviderClient) rememberSuccessfulTarget(tenantID, rawURL string) {
	tenantID = strings.TrimSpace(tenantID)
	url := normalizeHubCenterURLOne(rawURL)
	if c == nil || url == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.tenantBound == nil {
		c.tenantBound = map[string]tenantPin{}
	}
	hadPin := c.liveTenantPinLocked(tenantID, time.Now()) != ""
	if tenantID != "" {
		c.tenantBound[tenantID] = tenantPin{URL: url, PinnedAt: time.Now()}
	}
	if c.ownerCooldown != nil {
		delete(c.ownerCooldown, url)
	}
	if !hadPin {
		c.boundURL = url
		c.failureCount = 0
	}
}

func tenantPinLive(pin tenantPin, now time.Time) bool {
	if pin.URL == "" || pin.PinnedAt.IsZero() {
		return false
	}
	if officialTenantPinTTL <= 0 {
		return true
	}
	return now.Sub(pin.PinnedAt) < officialTenantPinTTL
}

func (c *MaClawProviderClient) liveTenantPinLocked(tenantID string, now time.Time) string {
	if c == nil || c.tenantBound == nil || tenantID == "" {
		return ""
	}
	pin := c.tenantBound[tenantID]
	if !tenantPinLive(pin, now) {
		if pin.URL != "" {
			delete(c.tenantBound, tenantID)
		}
		return ""
	}
	return pin.URL
}

func (c *MaClawProviderClient) coolingDownLocked(rawURL string, now time.Time) bool {
	if c == nil || c.ownerCooldown == nil || officialOwnerCooldown <= 0 {
		return false
	}
	key := normalizeHubCenterURLOne(rawURL)
	if key == "" {
		return false
	}
	failedAt, ok := c.ownerCooldown[key]
	if !ok || failedAt.IsZero() {
		return false
	}
	if now.Sub(failedAt) >= officialOwnerCooldown {
		delete(c.ownerCooldown, key)
		return false
	}
	return true
}

func (c *MaClawProviderClient) markOwnerUnreachable(rawURL string) {
	url := normalizeHubCenterURLOne(rawURL)
	if c == nil || url == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ownerCooldown == nil {
		c.ownerCooldown = map[string]time.Time{}
	}
	now := time.Now()
	c.ownerCooldown[url] = now
	for id, pin := range c.tenantBound {
		if sameHubCenterURL(pin.URL, url) {
			delete(c.tenantBound, id)
		}
	}
	if sameHubCenterURL(c.boundURL, url) {
		for _, cand := range c.candidateURLs {
			if cand == "" || sameHubCenterURL(cand, url) || c.coolingDownLocked(cand, now) {
				continue
			}
			c.boundURL = cand
			break
		}
	}
}

func requestCanceled(ctx context.Context, err error) bool {
	if ctx != nil && ctx.Err() != nil {
		return true
	}
	return errors.Is(err, context.Canceled)
}

func (c *MaClawProviderClient) failRequiredOwnerUnlessCanceled(ctx context.Context, nodeID, owner string, cause error) error {
	if requestCanceled(ctx, cause) {
		if ctx != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		return cause
	}
	return c.failRequiredOwner(nodeID, owner, cause)
}

func (c *MaClawProviderClient) failRequiredOwner(nodeID, owner string, cause error) error {
	c.markOwnerUnreachable(owner)
	c.recordFailure()
	if errors.Is(cause, corellm.ErrOfficialOwnerUnreachable) {
		return cause
	}
	return ownerUnreachableError(nodeID, owner, cause)
}

// TenantHubCenterURL returns the HubCenter URL pinned for this tenant, if any.
func (c *MaClawProviderClient) TenantHubCenterURL(tenantID string) string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.liveTenantPinLocked(strings.TrimSpace(tenantID), time.Now())
}

// RememberNodeURL records a HubCenter node ID to base URL mapping.
func (c *MaClawProviderClient) RememberNodeURL(nodeID, rawURL string) {
	if c == nil {
		return
	}
	nodeID = strings.TrimSpace(nodeID)
	url := httpHubCenterURL(rawURL)
	if nodeID == "" || url == "" {
		return
	}
	c.mu.Lock()
	if c.nodeURLs == nil {
		c.nodeURLs = map[string]string{}
	}
	c.nodeURLs[nodeID] = url
	c.mu.Unlock()
}

type hubCenterBindingRedirect struct {
	NodeID      string
	RedirectURL string
}

func parseHubCenterBindingRedirect(status int, body []byte, header http.Header) (hubCenterBindingRedirect, bool) {
	if status != http.StatusConflict {
		return hubCenterBindingRedirect{}, false
	}
	out := hubCenterBindingRedirect{}
	code, message := "", ""
	var payload map[string]any
	if json.Unmarshal(body, &payload) == nil {
		out.NodeID = jsonMapString(payload, "node_id")
		out.RedirectURL = httpHubCenterURL(jsonMapString(payload, "redirect_url"))
		code = jsonMapString(payload, "code")
		message = firstNonEmptyString(jsonMapString(payload, "message"), jsonMapString(payload, "msg"))
		if errObj, ok := payload["error"].(map[string]any); ok {
			if out.NodeID == "" {
				out.NodeID = jsonMapString(errObj, "node_id")
			}
			if out.RedirectURL == "" {
				out.RedirectURL = httpHubCenterURL(jsonMapString(errObj, "redirect_url"))
			}
			if code == "" {
				code = jsonMapString(errObj, "code")
			}
			if message == "" {
				message = jsonMapString(errObj, "message")
			}
		}
	}
	if out.NodeID == "" {
		out.NodeID = parseBoundNodeID(message)
	}
	if out.NodeID == "" {
		out.NodeID = parseBoundNodeID(string(body))
	}
	bound := strings.EqualFold(code, officialTenantBoundCode) ||
		out.NodeID != "" ||
		strings.Contains(strings.ToLower(message), "bound to node") ||
		strings.Contains(strings.ToLower(string(body)), "bound to node")
	if !bound {
		return hubCenterBindingRedirect{}, false
	}
	if header != nil {
		if out.NodeID == "" {
			out.NodeID = strings.TrimSpace(header.Get(officialRedirectNodeHeader))
		}
		if out.RedirectURL == "" {
			out.RedirectURL = httpHubCenterURL(header.Get(officialRedirectURLHeader))
		}
		if out.RedirectURL == "" {
			out.RedirectURL = httpHubCenterURL(bindingRedirectBaseURL(header.Get("Location")))
		}
	}
	return out, true
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func sameHubCenterURL(a, b string) bool {
	left := normalizeHubCenterURLOne(a)
	right := normalizeHubCenterURLOne(b)
	return left != "" && left == right
}

func httpHubCenterURL(raw string) string {
	u := normalizeHubCenterURLOne(raw)
	parsed, err := url.Parse(u)
	if err != nil || parsed.Host == "" {
		return ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	if parsed.User != nil {
		return ""
	}
	return u
}

func bindingRedirectBaseURL(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimSuffix(raw, "/api/llm/v1/chat/completions")
	return normalizeHubCenterURLOne(raw)
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

func jsonMapString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	s, _ := m[key].(string)
	return strings.TrimSpace(s)
}

func (c *MaClawProviderClient) resolveBindingRedirect(redirect hubCenterBindingRedirect) string {
	if url := httpHubCenterURL(redirect.RedirectURL); url != "" {
		c.RememberNodeURL(redirect.NodeID, url)
		return url
	}
	if strings.TrimSpace(redirect.NodeID) == "" {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return httpHubCenterURL(c.nodeURLs[strings.TrimSpace(redirect.NodeID)])
}

func (c *MaClawProviderClient) applyBindingRedirect(ctx context.Context, tenantID string, redirect hubCenterBindingRedirect, tried map[string]struct{}, targets []string) ([]string, string, error) {
	owner := c.resolveBindingRedirect(redirect)
	if owner == "" {
		log.Printf("[maclaw-provider] tenant %s bound to %s but owner URL is unknown", tenantID, redirect.NodeID)
		return targets, "", nil
	}
	ownerKey := normalizeHubCenterURLOne(owner)
	c.mu.Lock()
	cooling := c.coolingDownLocked(ownerKey, time.Now())
	c.mu.Unlock()
	if _, seen := tried[ownerKey]; seen || cooling {
		return targets, owner, ownerUnreachableError(redirect.NodeID, owner, nil)
	}
	if err := c.probeHubCenterReachable(ctx, owner); err != nil {
		log.Printf("[maclaw-provider] tenant %s bound to %s but owner %s failed probe: %v", tenantID, redirect.NodeID, owner, err)
		if requestCanceled(ctx, err) {
			if ctx != nil && ctx.Err() != nil {
				return targets, owner, ctx.Err()
			}
			return targets, owner, err
		}
		return targets, owner, ownerUnreachableError(redirect.NodeID, owner, err)
	}
	log.Printf("[maclaw-provider] tenant %s bound to %s; redirecting official LLM to %s", tenantID, redirect.NodeID, owner)
	return append([]string{owner}, targets...), owner, nil
}

func (c *MaClawProviderClient) probeHubCenterReachable(ctx context.Context, baseURL string) error {
	baseURL = normalizeHubCenterURLOne(baseURL)
	if baseURL == "" {
		return fmt.Errorf("empty hubcenter url")
	}
	timeout := officialOwnerProbeTimeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	if ctx == nil {
		ctx = context.Background()
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, baseURL+"/api/client/quality", nil)
	if err != nil {
		return err
	}
	base := c.httpClient()
	probeClient := &http.Client{Timeout: timeout}
	if base != nil {
		probeClient.Transport = base.Transport
	}
	resp, err := probeClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	// Any HTTP response means the process accepted the connection. A 5xx on
	// /api/client/quality must not block the official LLM hop.
	return nil
}

func ownerUnreachableError(nodeID, owner string, err error) error {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		nodeID = "unknown"
	}
	if strings.TrimSpace(owner) != "" || err != nil {
		log.Printf("[maclaw-provider] tenant bound to node %s owner=%s unreachable: %v", nodeID, owner, err)
	}
	return fmt.Errorf("%w: tenant bound to node %s but owner is unreachable", corellm.ErrOfficialOwnerUnreachable, nodeID)
}

func bindingLoopFailure(nodeID, owner string, lastErr error) error {
	if errors.Is(lastErr, corellm.ErrOfficialOwnerUnreachable) {
		return lastErr
	}
	return ownerUnreachableError(nodeID, owner, lastErr)
}

// ForwardStream sends an LLM streaming request to HubCenter. The caller must
// close the returned response body.
func (c *MaClawProviderClient) ForwardStream(ctx context.Context, body []byte, tenantID string, serviceGroupIDs ...string) (*http.Response, error) {
	baseClient := c.httpClient()
	targets := c.orderedTargets(tenantID)
	if len(targets) == 0 {
		return nil, fmt.Errorf("maclaw official provider: no HubCenter URL configured")
	}
	hubID, token := c.ensureCredentials()
	if hubID == "" || token == "" {
		return nil, fmt.Errorf("maclaw official provider: hub not registered to HubCenter yet")
	}

	httpClient := streamHTTPClientFrom(baseClient)
	tried := make(map[string]struct{}, len(targets))
	var lastErr error
	var requiredOwner, requiredNodeID string
	var sawBindingRedirect bool
	for attempts := 0; len(targets) > 0 && attempts < maxOfficialHubCenterAttempts; {
		target := targets[0]
		targets = targets[1:]
		key := normalizeHubCenterURLOne(target)
		if key == "" {
			continue
		}
		if _, ok := tried[key]; ok {
			continue
		}
		tried[key] = struct{}{}
		attempts++
		resp, err := c.forwardStreamTo(ctx, httpClient, target, body, hubID, token, tenantID, serviceGroupIDs...)
		if err != nil {
			if requiredOwner != "" && sameHubCenterURL(key, requiredOwner) {
				return nil, c.failRequiredOwnerUnlessCanceled(ctx, requiredNodeID, requiredOwner, err)
			}
			lastErr = err
			log.Printf("[maclaw-provider] streaming LLM upstream failed hubcenter=%s err=%v; trying next candidate", target, err)
			continue
		}
		// Retrying is safe only before any stream bytes are returned to the caller.
		// Preserve an application JSON error for the caller; it may represent a
		// request that already reached the provider and must not be replayed.
		var failureBody []byte
		if resp.StatusCode == http.StatusConflict || resp.StatusCode >= 500 {
			failureBody, _ = io.ReadAll(resp.Body)
			resp.Body = io.NopCloser(bytes.NewReader(failureBody))
		}
		if redirect, ok := parseHubCenterBindingRedirect(resp.StatusCode, failureBody, resp.Header); ok {
			_ = resp.Body.Close()
			sawBindingRedirect = true
			next, owner, stop := c.applyBindingRedirect(ctx, tenantID, redirect, tried, targets)
			if stop != nil {
				return nil, c.failRequiredOwnerUnlessCanceled(ctx, redirect.NodeID, owner, stop)
			}
			if owner != "" {
				requiredOwner = owner
				requiredNodeID = firstNonEmptyString(redirect.NodeID, requiredNodeID)
			}
			targets = next
			lastErr = fmt.Errorf("maclaw official: stream upstream HTTP %d", resp.StatusCode)
			continue
		}
		if shouldFailoverHubCenter(resp.StatusCode, failureBody, nil) {
			_ = resp.Body.Close()
			if requiredOwner != "" && sameHubCenterURL(key, requiredOwner) {
				return nil, c.failRequiredOwnerUnlessCanceled(ctx, requiredNodeID, requiredOwner, fmt.Errorf("HTTP %d", resp.StatusCode))
			}
			lastErr = fmt.Errorf("maclaw official: stream upstream HTTP %d", resp.StatusCode)
			log.Printf("[maclaw-provider] streaming LLM upstream failed hubcenter=%s status=%d; trying next candidate", target, resp.StatusCode)
			continue
		}
		c.rememberSuccessfulTarget(tenantID, target)
		return resp, nil
	}
	c.recordFailure()
	if lastErr == nil {
		lastErr = fmt.Errorf("maclaw official: no HubCenter candidates available")
	}
	if sawBindingRedirect {
		return nil, bindingLoopFailure(requiredNodeID, requiredOwner, lastErr)
	}
	return nil, lastErr
}

// ForwardStreamWithQuote sends a pinned stream to the exact HubCenter node
// which issued the quote. Unlike the legacy streaming path it deliberately
// never retries another HubCenter node: a retry would no longer be guaranteed
// to use the quoted provider route and time-of-use price.
func (c *MaClawProviderClient) ForwardStreamWithQuote(ctx context.Context, quote OfficialPricingQuote, body []byte, tenantID string, serviceGroupIDs ...string) (*http.Response, error) {
	if strings.TrimSpace(quote.Token) == "" || strings.TrimSpace(quote.targetURL) == "" || !quote.ExpiresAt.After(time.Now().UTC()) {
		return nil, fmt.Errorf("maclaw official provider: invalid or expired pricing quote")
	}
	hubID, token := c.ensureCredentials()
	if hubID == "" || token == "" {
		return nil, fmt.Errorf("maclaw official provider: hub not registered to HubCenter yet")
	}
	return c.forwardStreamToWithQuote(ctx, streamHTTPClientFrom(c.httpClient()), quote.targetURL, body, hubID, token, tenantID, quote.Token, serviceGroupIDs...)
}

func (c *MaClawProviderClient) forwardStreamTo(ctx context.Context, httpClient *http.Client, targetURL string, body []byte, hubID, token, tenantID string, serviceGroupIDs ...string) (*http.Response, error) {
	endpoint := strings.TrimRight(targetURL, "/") + "/api/llm/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("maclaw official: create stream request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Hub-ID", hubID)
	req.Header.Set("X-Tenant-ID", tenantID)
	if serviceGroupID := firstNonEmptyServiceGroupID(serviceGroupIDs); serviceGroupID != "" {
		req.Header.Set("X-MaClaw-Service-Group-ID", serviceGroupID)
	}
	applyOfficialForwardMeta(req, ctx)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("maclaw official: stream forward failed: %w", err)
	}
	return resp, nil
}

func (c *MaClawProviderClient) forwardStreamToWithQuote(ctx context.Context, httpClient *http.Client, targetURL string, body []byte, hubID, token, tenantID, quoteToken string, serviceGroupIDs ...string) (*http.Response, error) {
	endpoint := strings.TrimRight(targetURL, "/") + "/api/llm/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("maclaw official: create quoted stream request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Hub-ID", hubID)
	req.Header.Set("X-Tenant-ID", tenantID)
	req.Header.Set(llmpool.PricingQuoteHeader, quoteToken)
	if serviceGroupID := firstNonEmptyServiceGroupID(serviceGroupIDs); serviceGroupID != "" {
		req.Header.Set("X-MaClaw-Service-Group-ID", serviceGroupID)
	}
	applyOfficialForwardMeta(req, ctx)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("maclaw official: quoted stream forward failed: %w", err)
	}
	return resp, nil
}

func applyOfficialForwardMeta(req *http.Request, ctx context.Context) {
	if req == nil {
		return
	}
	meta := OfficialForwardMetaFrom(ctx)
	if requestID := strings.TrimSpace(meta.RequestID); requestID != "" {
		req.Header.Set("X-MaClaw-Request-ID", requestID)
	}
	if class := llmpool.NormalizeWorkloadClass(meta.WorkloadClass); llmpool.IsWorkloadClass(class) {
		req.Header.Set(llmpool.WorkloadClassHeader, class)
	}
	if resolved := strings.TrimSpace(meta.ResolvedModel); resolved != "" {
		req.Header.Set(llmpool.ResolvedModelHeader, resolved)
	}
	if workflow := strings.TrimSpace(meta.WorkflowType); workflow != "" {
		req.Header.Set(llmpool.WorkflowTypeHeader, workflow)
	}
	if phase := strings.TrimSpace(meta.PhaseKind); phase != "" {
		req.Header.Set(llmpool.PhaseKindHeader, phase)
	}
	if task := strings.TrimSpace(meta.TaskType); task != "" {
		req.Header.Set(llmpool.TaskTypeHeader, task)
	}
}

func firstNonEmptyServiceGroupID(ids []string) string {
	for _, id := range ids {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func (c *MaClawProviderClient) httpClient() *http.Client {
	if c == nil {
		return &http.Client{Timeout: 600 * time.Second}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 600 * time.Second}
}

func (c *MaClawProviderClient) adminHTTPClient() *http.Client {
	base := c.httpClient()
	timeout := officialAdminRequestTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	client := &http.Client{Timeout: timeout}
	if base != nil {
		client.Transport = base.Transport
	}
	return client
}

func (c *MaClawProviderClient) streamHTTPClient() *http.Client {
	timeout := 600 * time.Second
	if c != nil {
		c.mu.RLock()
		if c.Config.TimeoutSec >= 300 {
			timeout = time.Duration(c.Config.TimeoutSec) * time.Second
		}
		c.mu.RUnlock()
	}
	base := c.httpClient()
	if c == nil || base == nil {
		return &http.Client{Transport: defaultStreamTransport(timeout)}
	}
	return streamHTTPClientFrom(base)
}

func streamHTTPClientFrom(base *http.Client) *http.Client {
	// Streaming must never use a full-request Timeout (it would kill long SSE
	// responses). Always bound ResponseHeaderTimeout so hung upstreams fail fast.
	const defaultHeaderTimeout = 120 * time.Second
	if base == nil {
		return &http.Client{Transport: defaultStreamTransport(defaultHeaderTimeout)}
	}
	client := *base
	client.Timeout = 0
	if client.Transport == nil {
		// Unbounded base client (Timeout==0, Transport==nil): apply a safe header wait.
		client.Transport = defaultStreamTransport(defaultHeaderTimeout)
		return &client
	}
	// Preserve a custom transport when present, but still enforce a header timeout.
	if transport, ok := client.Transport.(*http.Transport); ok {
		clone := transport.Clone()
		if clone.ResponseHeaderTimeout <= 0 {
			clone.ResponseHeaderTimeout = defaultHeaderTimeout
		}
		client.Transport = clone
	}
	return &client
}

func defaultStreamTransport(responseHeaderTimeout time.Duration) http.RoundTripper {
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return http.DefaultTransport
	}
	clone := transport.Clone()
	clone.ResponseHeaderTimeout = responseHeaderTimeout
	return clone
}

// QueryAuthorization queries the tenant's LLM authorization status from HubCenter.
func (c *MaClawProviderClient) QueryAuthorization(ctx context.Context, tenantID string) (*TenantAuthorizationStatus, error) {
	targets := c.orderedTargets(tenantID)
	if len(targets) == 0 {
		return nil, fmt.Errorf("no HubCenter URL configured")
	}
	hubID, token := c.ensureCredentials()
	if hubID == "" || token == "" {
		return nil, fmt.Errorf("hub not registered to HubCenter yet")
	}
	httpClient := c.adminHTTPClient()
	var lastErr error
	tried := make(map[string]struct{}, len(targets))
	for _, targetURL := range targets {
		key := normalizeHubCenterURLOne(targetURL)
		if key == "" {
			continue
		}
		if _, ok := tried[key]; ok {
			continue
		}
		tried[key] = struct{}{}
		result, err := c.queryAuthorizationAt(ctx, httpClient, targetURL, hubID, token, tenantID)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if !shouldFailoverAuthorization(err) {
			return nil, err
		}
		log.Printf("[maclaw-provider] QueryAuthorization failed hubcenter=%s err=%v; trying next candidate", targetURL, err)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no HubCenter URL configured")
	}
	return nil, lastErr
}

func (c *MaClawProviderClient) queryAuthorizationAt(ctx context.Context, httpClient *http.Client, targetURL, hubID, token, tenantID string) (*TenantAuthorizationStatus, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	q := url.Values{}
	q.Set("hub_id", hubID)
	q.Set("tenant_id", tenantID)
	endpoint := fmt.Sprintf("%s/api/llm/v1/authorization?%s",
		strings.TrimRight(targetURL, "/"), q.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Hub-ID", hubID)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read authorization response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("authorization query failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var result TenantAuthorizationStatus
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse authorization response: %w", err)
	}
	log.Printf("[maclaw-provider] QueryAuthorization OK: hub=%s tenant=%s allow_external=%v auths=%d",
		hubID, tenantID, result.AllowExternalProviders, len(result.Authorizations))
	return &result, nil
}

func shouldFailoverAuthorization(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, code := range []string{"HTTP 400", "HTTP 401", "HTTP 403"} {
		if strings.Contains(msg, code) {
			return false
		}
	}
	return true
}

type PublishedOfficialHead struct {
	Published bool                        `json:"published"`
	Pipeline  string                      `json:"pipeline"`
	Head      *llmpool.ClassificationHead `json:"head,omitempty"`
}

func (c *MaClawProviderClient) PullPublishedOfficialHead(ctx context.Context) (*llmpool.ClassificationHead, error) {
	if c == nil {
		return nil, fmt.Errorf("maclaw client is nil")
	}
	c.mu.RLock()
	targetURL := c.boundURL
	c.mu.RUnlock()
	if targetURL == "" {
		return nil, fmt.Errorf("no HubCenter URL configured")
	}
	hubID, token := c.ensureCredentials()
	if hubID == "" || token == "" {
		return nil, fmt.Errorf("hub not registered to HubCenter yet")
	}
	q := url.Values{}
	q.Set("hub_id", hubID)
	endpoint := fmt.Sprintf("%s/api/llm/v1/official-class-head?%s", strings.TrimRight(targetURL, "/"), q.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Hub-ID", hubID)
	resp, err := c.adminHTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("official class head pull failed (HTTP %d): %s", resp.StatusCode, string(body))
	}
	var published PublishedOfficialHead
	if err := json.Unmarshal(body, &published); err != nil {
		return nil, err
	}
	if !published.Published || published.Head == nil || !published.Head.Ready() {
		return nil, fmt.Errorf("official head is not published")
	}
	return published.Head, nil
}

// SetBoundURL updates the preferred HubCenter URL for unpinned tenants.
func (c *MaClawProviderClient) SetBoundURL(url string) {
	c.mu.Lock()
	c.boundURL = url
	c.failureCount = 0
	c.mu.Unlock()
}

// CurrentHubCenterURL returns the HubCenter URL currently used for forwarding.
func (c *MaClawProviderClient) CurrentHubCenterURL() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if strings.TrimSpace(c.boundURL) != "" {
		return c.boundURL
	}
	return c.Config.HubCenterURL
}

// SetRefreshCredentials sets the callback used to lazy-load hub credentials
// after registration completes (hub_id and hub_secret may be empty at init time).
func (c *MaClawProviderClient) SetRefreshCredentials(fn func() (string, string)) {
	c.mu.Lock()
	c.refreshCredentials = fn
	c.mu.Unlock()
}

// NeedsFailover returns true if the provider has accumulated enough failures
// to warrant switching to a different HubCenter node.
func (c *MaClawProviderClient) NeedsFailover() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.failureCount >= 3
}

func (c *MaClawProviderClient) recordFailure() {
	c.mu.Lock()
	c.failureCount++
	c.lastFailureAt = time.Now()
	c.mu.Unlock()
}

func (c *MaClawProviderClient) resetFailures() {
	c.mu.Lock()
	if c.failureCount > 0 {
		c.failureCount = 0
	}
	c.mu.Unlock()
}

// ---------------------------------------------------------------------------
// Authorization status (returned by HubCenter)
// ---------------------------------------------------------------------------

// TenantAuthorizationStatus is the response from HubCenter's authorization query.
type TenantAuthorizationStatus struct {
	HubID                  string                          `json:"hub_id"`
	TenantID               string                          `json:"tenant_id"`
	LookupTenantIDs        []string                        `json:"lookup_tenant_ids,omitempty"`
	AllowExternalProviders bool                            `json:"allow_external_providers"`
	Authorizations         []AuthorizationSummary          `json:"authorizations,omitempty"`
	ProviderBilling        []llmpool.ProviderBillingPolicy `json:"provider_billing,omitempty"`
}

// AuthorizationSummary mirrors HubCenter's tenant authorization summary.
type AuthorizationSummary struct {
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

// ---------------------------------------------------------------------------
// Registry injection helpers
// ---------------------------------------------------------------------------

// ComputeStoreURL builds the HubCenter compute store URL with tenant context
// for the "购买算力" button on the Hub admin panel.
func ComputeStoreURL(hubCenterURL, hubID, tenantID, adminEmail string) string {
	base := strings.TrimRight(hubCenterURL, "/")
	v := url.Values{}
	v.Set("hub_id", hubID)
	v.Set("tenant_id", tenantID)
	v.Set("email", adminEmail)
	return base + "/compute-store?" + v.Encode()
}

// EnsureBuiltinProvider ensures the MaClaw Official provider exists in the registry.
// Returns true if it was added (registry was modified).
func EnsureBuiltinProvider(reg *Registry) bool {
	for i := range reg.ModelServiceGroups {
		if strings.EqualFold(strings.TrimSpace(reg.ModelServiceGroups[i].ID), MaClawOfficialServiceGroupID) {
			return ensureBuiltinProviderServiceGroupPolicy(&reg.ModelServiceGroups[i])
		}
	}
	// Add the builtin service group
	reg.ModelServiceGroups = append([]ModelServiceGroup{{
		ID:           MaClawOfficialServiceGroupID,
		Name:         MaClawOfficialServiceGroupName,
		Description:  "MaClaw 官方 LLM 服务，通过 HubCenter 提供算力",
		AccessPolicy: AccessPolicyGrantRequired,
		Models: []ModelServiceModel{
			{
				Name:        "auto",
				Description: "自动选择最佳模型",
				ProviderIDs: []string{MaClawOfficialProviderID},
			},
		},
	}}, reg.ModelServiceGroups...)
	return true
}

func ensureBuiltinProviderServiceGroupPolicy(group *ModelServiceGroup) bool {
	if group == nil {
		return false
	}
	if !isDefaultMaClawOfficialServiceGroup(*group) {
		return false
	}
	if NormalizeAccessPolicy(group.AccessPolicy) != AccessPolicyFree {
		return false
	}
	group.AccessPolicy = AccessPolicyGrantRequired
	return true
}

func isDefaultMaClawOfficialServiceGroup(group ModelServiceGroup) bool {
	if !strings.EqualFold(strings.TrimSpace(group.ID), MaClawOfficialServiceGroupID) {
		return false
	}
	if strings.TrimSpace(group.Description) != "MaClaw 官方 LLM 服务，通过 HubCenter 提供算力" {
		return false
	}
	if len(group.Models) != 1 {
		return false
	}
	model := group.Models[0]
	if strings.TrimSpace(model.Name) != "auto" {
		return false
	}
	if len(model.ProviderIDs) != 1 || !IsBuiltinProvider(model.ProviderIDs[0]) {
		return false
	}
	return true
}
