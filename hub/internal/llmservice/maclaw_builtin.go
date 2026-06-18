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
}

// MaClawProviderClient forwards LLM requests to HubCenter's LLM proxy.
type MaClawProviderClient struct {
	Config     MaClawProviderConfig
	HTTPClient *http.Client

	mu                 sync.RWMutex
	boundURL           string // persisted bound HubCenter LLM URL
	failureCount       int
	lastFailureAt      time.Time
	refreshCredentials func() (hubID, hubSecret string) // lazy refresh after registration
}

// NewMaClawProviderClient creates a new MaClaw Official provider client.
func NewMaClawProviderClient(cfg MaClawProviderConfig) *MaClawProviderClient {
	client := &http.Client{Timeout: 120 * time.Second}
	return &MaClawProviderClient{
		Config:     cfg,
		HTTPClient: client,
		boundURL:   cfg.HubCenterURL,
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
func (c *MaClawProviderClient) Forward(ctx context.Context, body []byte, tenantID string) ([]byte, int, error) {
	c.mu.RLock()
	targetURL := c.boundURL
	c.mu.RUnlock()

	if targetURL == "" {
		return nil, 0, fmt.Errorf("maclaw official provider: no HubCenter URL configured")
	}
	hubID, token := c.ensureCredentials()
	if hubID == "" || token == "" {
		return nil, 0, fmt.Errorf("maclaw official provider: hub not registered to HubCenter yet")
	}

	endpoint := strings.TrimRight(targetURL, "/") + "/api/llm/v1/chat/completions"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("maclaw official: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Hub-ID", hubID)
	req.Header.Set("X-Tenant-ID", tenantID)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		c.recordFailure()
		return nil, 0, fmt.Errorf("maclaw official: forward failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("maclaw official: read response: %w", err)
	}

	if resp.StatusCode >= 500 {
		c.recordFailure()
	} else if resp.StatusCode == http.StatusConflict {
		// 409 = HA binding conflict, need failover to a different node
		c.recordFailure()
	} else {
		c.resetFailures()
	}

	return respBody, resp.StatusCode, nil
}

// ForwardStream sends an LLM streaming request to HubCenter. The caller must
// close the returned response body.
func (c *MaClawProviderClient) ForwardStream(ctx context.Context, body []byte, tenantID string) (*http.Response, error) {
	c.mu.RLock()
	targetURL := c.boundURL
	c.mu.RUnlock()

	if targetURL == "" {
		return nil, fmt.Errorf("maclaw official provider: no HubCenter URL configured")
	}
	hubID, token := c.ensureCredentials()
	if hubID == "" || token == "" {
		return nil, fmt.Errorf("maclaw official provider: hub not registered to HubCenter yet")
	}

	endpoint := strings.TrimRight(targetURL, "/") + "/api/llm/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("maclaw official: create stream request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Hub-ID", hubID)
	req.Header.Set("X-Tenant-ID", tenantID)

	httpClient := c.streamHTTPClient()
	resp, err := httpClient.Do(req)
	if err != nil {
		c.recordFailure()
		return nil, fmt.Errorf("maclaw official: stream forward failed: %w", err)
	}
	if resp.StatusCode >= 500 || resp.StatusCode == http.StatusConflict {
		c.recordFailure()
	} else {
		c.resetFailures()
	}
	return resp, nil
}

func (c *MaClawProviderClient) streamHTTPClient() *http.Client {
	if c == nil || c.HTTPClient == nil {
		return &http.Client{Transport: defaultStreamTransport(120 * time.Second)}
	}
	if c.HTTPClient.Timeout == 0 {
		if c.HTTPClient.Transport == nil {
			client := *c.HTTPClient
			client.Transport = defaultStreamTransport(120 * time.Second)
			return &client
		}
		return c.HTTPClient
	}
	client := *c.HTTPClient
	if client.Transport == nil {
		client.Transport = defaultStreamTransport(c.HTTPClient.Timeout)
	}
	client.Timeout = 0
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
