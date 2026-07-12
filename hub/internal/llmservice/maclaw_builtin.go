package llmservice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Built-in MaClaw Official constants
// ---------------------------------------------------------------------------

const (
	MaClawOfficialProviderID       = "maclaw_official"
	MaClawOfficialProviderName     = "MaClaw 官方"
	MaClawOfficialServiceGroupID   = "maclaw_official_group"
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
	return strings.EqualFold(strings.TrimSpace(id), MaClawOfficialServiceGroupID)
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
	boundURL           string   // persisted bound HubCenter LLM URL
	candidateURLs      []string // ordered HubCenter failover candidates
	failureCount       int
	lastFailureAt      time.Time
	refreshCredentials func() (hubID, hubSecret string) // lazy refresh after registration
}

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

// Forward sends an LLM request to HubCenter and returns the response.
func (c *MaClawProviderClient) Forward(ctx context.Context, body []byte, tenantID string, serviceGroupIDs ...string) ([]byte, int, error) {
	c.mu.RLock()
	targetURL := c.boundURL
	httpClient := c.HTTPClient
	candidates := append([]string(nil), c.candidateURLs...)
	c.mu.RUnlock()

	if targetURL == "" {
		return nil, 0, fmt.Errorf("maclaw official provider: no HubCenter URL configured")
	}
	hubID, token := c.ensureCredentials()
	if hubID == "" || token == "" {
		return nil, 0, fmt.Errorf("maclaw official provider: hub not registered to HubCenter yet")
	}

	// A HubCenter cluster can have a live registration/heartbeat endpoint while
	// one node's LLM proxy is unavailable. Retry only retryable transport/5xx/HA
	// failures on the other configured nodes, and pin the first successful node.
	targets := orderedHubCenterURLs(targetURL, candidates)
	var lastBody []byte
	var lastStatus int
	var lastErr error
	for _, target := range targets {
		respBody, status, err := c.forwardTo(ctx, httpClient, target, body, hubID, token, tenantID, serviceGroupIDs...)
		lastBody, lastStatus, lastErr = respBody, status, err
		if !shouldFailoverHubCenter(status, respBody, err) {
			c.SetBoundURL(target)
			return respBody, status, nil
		}
		log.Printf("[maclaw-provider] LLM upstream failed hubcenter=%s status=%d err=%v; trying next candidate", target, status, err)
	}
	c.recordFailure()
	return lastBody, lastStatus, lastErr
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

func (c *MaClawProviderClient) forwardTo(ctx context.Context, httpClient *http.Client, targetURL string, body []byte, hubID, token, tenantID string, serviceGroupIDs ...string) ([]byte, int, error) {
	endpoint := strings.TrimRight(targetURL, "/") + "/api/llm/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("maclaw official: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Hub-ID", hubID)
	req.Header.Set("X-Tenant-ID", tenantID)
	if serviceGroupID := firstNonEmptyServiceGroupID(serviceGroupIDs); serviceGroupID != "" {
		req.Header.Set("X-MaClaw-Service-Group-ID", serviceGroupID)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("maclaw official: forward failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("maclaw official: read response: %w", err)
	}
	return respBody, resp.StatusCode, nil
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

// ForwardStream sends an LLM streaming request to HubCenter. The caller must
// close the returned response body.
func (c *MaClawProviderClient) ForwardStream(ctx context.Context, body []byte, tenantID string, serviceGroupIDs ...string) (*http.Response, error) {
	c.mu.RLock()
	targetURL := c.boundURL
	baseClient := c.HTTPClient
	candidates := append([]string(nil), c.candidateURLs...)
	c.mu.RUnlock()

	if targetURL == "" {
		return nil, fmt.Errorf("maclaw official provider: no HubCenter URL configured")
	}
	hubID, token := c.ensureCredentials()
	if hubID == "" || token == "" {
		return nil, fmt.Errorf("maclaw official provider: hub not registered to HubCenter yet")
	}

	httpClient := streamHTTPClientFrom(baseClient)
	var lastErr error
	for _, target := range orderedHubCenterURLs(targetURL, candidates) {
		resp, err := c.forwardStreamTo(ctx, httpClient, target, body, hubID, token, tenantID, serviceGroupIDs...)
		if err != nil {
			lastErr = err
			log.Printf("[maclaw-provider] streaming LLM upstream failed hubcenter=%s err=%v; trying next candidate", target, err)
			continue
		}
		// Retrying is safe only before any stream bytes are returned to the caller.
		// Preserve an application JSON error for the caller; it may represent a
		// request that already reached the provider and must not be replayed.
		var failureBody []byte
		if resp.StatusCode >= 500 {
			failureBody, _ = io.ReadAll(resp.Body)
			resp.Body = io.NopCloser(bytes.NewReader(failureBody))
		}
		if shouldFailoverHubCenter(resp.StatusCode, failureBody, nil) {
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("maclaw official: stream upstream HTTP %d", resp.StatusCode)
			log.Printf("[maclaw-provider] streaming LLM upstream failed hubcenter=%s status=%d; trying next candidate", target, resp.StatusCode)
			continue
		}
		c.SetBoundURL(target)
		return resp, nil
	}
	c.recordFailure()
	if lastErr == nil {
		lastErr = fmt.Errorf("maclaw official: no HubCenter candidates available")
	}
	return nil, lastErr
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
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("maclaw official: stream forward failed: %w", err)
	}
	return resp, nil
}

func firstNonEmptyServiceGroupID(ids []string) string {
	for _, id := range ids {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func (c *MaClawProviderClient) streamHTTPClient() *http.Client {
	timeout := 600 * time.Second
	if c != nil && c.Config.TimeoutSec >= 300 {
		timeout = time.Duration(c.Config.TimeoutSec) * time.Second
	}
	if c == nil || c.HTTPClient == nil {
		return &http.Client{Transport: defaultStreamTransport(timeout)}
	}
	return streamHTTPClientFrom(c.HTTPClient)
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
	q.Set("tenant_id", tenantID)
	endpoint := fmt.Sprintf("%s/api/llm/v1/authorization?%s",
		strings.TrimRight(targetURL, "/"), q.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Hub-ID", hubID)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("authorization query failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read authorization response: %w", err)
	}

	var result TenantAuthorizationStatus
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse authorization response: %w", err)
	}
	log.Printf("[maclaw-provider] QueryAuthorization OK: hub=%s tenant=%s allow_external=%v auths=%d",
		hubID, tenantID, result.AllowExternalProviders, len(result.Authorizations))
	return &result, nil
}

// SetBoundURL updates the pinned HubCenter URL (after failover).
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
	HubID                  string                 `json:"hub_id"`
	TenantID               string                 `json:"tenant_id"`
	LookupTenantIDs        []string               `json:"lookup_tenant_ids,omitempty"`
	AllowExternalProviders bool                   `json:"allow_external_providers"`
	Authorizations         []AuthorizationSummary `json:"authorizations,omitempty"`
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
