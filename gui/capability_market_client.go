package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

type HubCapabilitySummary struct {
	External          bool   `json:"external,omitempty"`
	ID                string `json:"id"`
	CapabilityType    string `json:"capability_type"`
	CapabilityID      string `json:"capability_id"`
	DisplayName       string `json:"display_name"`
	Description       string `json:"description,omitempty"`
	Source            string `json:"source"`
	Status            string `json:"status"`
	GlobalKey         string `json:"global_key"`
	CurrentVersionKey string `json:"current_version_key,omitempty"`
	MetadataJSON      string `json:"metadata_json,omitempty"`
}

type HubCapabilityDeployment struct {
	ID                   string `json:"id"`
	CapabilityRef        string `json:"capability_ref"`
	CapabilityVersionKey string `json:"capability_version_key,omitempty"`
	ScopeJSON            string `json:"scope_json"`
	DeploymentPolicy     string `json:"deployment_policy"`
	ReinstallIfRemoved   bool   `json:"reinstall_if_removed"`
	RetryIntervalMinutes int    `json:"retry_interval_minutes"`
}

type HubCapabilityRecommendation struct {
	ID                   string `json:"id"`
	CapabilityRef        string `json:"capability_ref"`
	CapabilityVersionKey string `json:"capability_version_key,omitempty"`
	ScopeJSON            string `json:"scope_json"`
	Reason               string `json:"recommendation_reason,omitempty"`
	AllowUserDismiss     bool   `json:"allow_user_dismiss"`
}

type HubCapabilityInventoryItem struct {
	CapabilityRef        string         `json:"capability_ref"`
	CapabilityVersionKey string         `json:"capability_version_key,omitempty"`
	CapabilityType       string         `json:"capability_type,omitempty"`
	InstallStatus        string         `json:"install_status,omitempty"`
	Installed            bool           `json:"installed"`
	Metadata             map[string]any `json:"metadata,omitempty"`
	LastSeenAt           string         `json:"last_seen_at,omitempty"`
}

type HubCapabilityInventoryReport struct {
	Items        []HubCapabilityInventoryItem `json:"items,omitempty"`
	FullSnapshot bool                         `json:"full_snapshot,omitempty"`
}

type HubCapabilityInstallIntent struct {
	CapabilityID   string         `json:"capability_id"`
	CapabilityType string         `json:"capability_type"`
	Version        string         `json:"version,omitempty"`
	Source         string         `json:"source"`
	Pricing        string         `json:"pricing,omitempty"`
	Price          map[string]any `json:"price,omitempty"`
	License        map[string]any `json:"license,omitempty"`
	UserReason     string         `json:"user_reason,omitempty"`
}

type HubCapabilityInstallIntentResult struct {
	Action     string                `json:"action"`
	Reason     string                `json:"reason,omitempty"`
	RequestID  string                `json:"request_id,omitempty"`
	Capability *HubCapabilitySummary `json:"capability,omitempty"`
}

type HubMCPSecretRequirement struct {
	Name          string `json:"name"`
	Label         string `json:"label,omitempty"`
	Scope         string `json:"scope"`
	StoragePolicy string `json:"storage_policy"`
	Required      bool   `json:"required"`
	HelpURL       string `json:"help_url,omitempty"`
}

type HubMCPHubSecret struct {
	ID              string `json:"id"`
	UserID          string `json:"user_id"`
	MCPServerID     string `json:"mcp_server_id"`
	RequirementName string `json:"requirement_name"`
	SecretDigest    string `json:"secret_digest"`
	MetadataJSON    string `json:"metadata_json"`
	UpdatedAt       string `json:"updated_at"`
}

type HubMCPHubSecretInput struct {
	MCPServerID     string         `json:"mcp_server_id"`
	RequirementName string         `json:"requirement_name"`
	SecretValue     string         `json:"secret_value"`
	Metadata        map[string]any `json:"metadata,omitempty"`
}

type HubMCPSecretBinding struct {
	MCPServerID     string `json:"mcp_server_id"`
	RequirementName string `json:"requirement_name"`
	Storage         string `json:"storage"`
	HubSecretRef    string `json:"hub_secret_ref,omitempty"`
	LocalSecretRef  string `json:"local_secret_ref,omitempty"`
	Status          string `json:"status"`
}

type capabilityMarketClient struct {
	baseURL string
	token   string
	http    *http.Client
}

func newCapabilityMarketClient(cfg corelib.AppConfig) (*capabilityMarketClient, error) {
	baseURL := capabilityMarketBaseURL(cfg)
	token := capabilityMarketAuthToken(cfg)
	if baseURL == "" || token == "" {
		return nil, fmt.Errorf("hub marketplace client is not configured")
	}
	return &capabilityMarketClient{baseURL: baseURL, token: token, http: &http.Client{Timeout: 20 * time.Second}}, nil
}

func capabilityMarketBaseURL(cfg corelib.AppConfig) string {
	return strings.TrimRight(strings.TrimSpace(firstNonEmpty(cfg.RemoteHubURL, cfg.RemoteHubCenterURL)), "/")
}

func capabilityMarketAuthToken(cfg corelib.AppConfig) string {
	return strings.TrimSpace(firstNonEmpty(cfg.RemoteViewerToken, cfg.SkillMarketSessionToken, cfg.RemoteMachineToken))
}

func (c *capabilityMarketClient) listCapabilities(ctx context.Context, capabilityType, query string) ([]HubCapabilitySummary, error) {
	path := "/api/capabilities"
	values := url.Values{}
	if strings.TrimSpace(capabilityType) != "" {
		values.Set("type", strings.TrimSpace(capabilityType))
	}
	if encoded := values.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var resp struct {
		Items []HubCapabilitySummary `json:"items"`
	}
	if err := c.getJSON(ctx, path, &resp); err != nil {
		return nil, err
	}
	needle := strings.TrimSpace(strings.ToLower(query))
	if needle == "" {
		return resp.Items, nil
	}
	filtered := make([]HubCapabilitySummary, 0, len(resp.Items))
	for _, item := range resp.Items {
		haystack := strings.ToLower(strings.Join([]string{item.ID, item.CapabilityID, item.DisplayName, item.Description, item.Source, item.GlobalKey}, " "))
		if strings.Contains(haystack, needle) {
			filtered = append(filtered, item)
		}
	}
	return filtered, nil
}
func listHubCenterMCPCapabilities(ctx context.Context, httpClient *http.Client, baseURL string, query string) ([]HubCapabilitySummary, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, nil
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	endpoint := baseURL + "/api/capability-market/mcp"
	if strings.TrimSpace(query) != "" {
		endpoint += "?q=" + url.QueryEscape(strings.TrimSpace(query))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("hubcenter MCP marketplace request failed: status=%s", resp.Status)
	}
	var payload struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	items := make([]HubCapabilitySummary, 0, len(payload.Items))
	for _, raw := range payload.Items {
		id := firstCapabilityNonEmpty(anyString(raw["id"]), anyString(raw["capability_id"]), anyString(raw["name"]), anyString(raw["display_name"]))
		if id == "" {
			continue
		}
		metadata := map[string]any{"external": true}
		for _, key := range []string{"pricing", "license", "mcp", "secret_requirements", "version", "version_key"} {
			if value, ok := raw[key]; ok && value != nil {
				metadata[key] = value
			}
		}
		items = append(items, HubCapabilitySummary{
			External:          true,
			ID:                id,
			CapabilityType:    corelib.CapabilityTypeMCP,
			CapabilityID:      firstCapabilityNonEmpty(anyString(raw["capability_id"]), id),
			DisplayName:       firstCapabilityNonEmpty(anyString(raw["display_name"]), anyString(raw["name"]), id),
			Description:       anyString(raw["description"]),
			Source:            corelib.CapabilitySourceHubCenter,
			Status:            "external",
			GlobalKey:         anyString(raw["global_key"]),
			CurrentVersionKey: firstCapabilityNonEmpty(anyString(raw["version_key"]), anyString(raw["version"])),
			MetadataJSON:      mustCapabilityMetadataJSON(metadata),
		})
	}
	return items, nil
}

func anyString(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	default:
		return ""
	}
}

func mustCapabilityMetadataJSON(value map[string]any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(data)
}
func (c *capabilityMarketClient) listManagedDeployments(ctx context.Context) ([]HubCapabilityDeployment, error) {
	var resp struct {
		Items []HubCapabilityDeployment `json:"items"`
	}
	if err := c.getJSON(ctx, "/api/capabilities/managed-deployments", &resp); err != nil {
		return nil, err
	}
	return resp.Items, nil
}

func (c *capabilityMarketClient) listRecommendations(ctx context.Context) ([]HubCapabilityRecommendation, error) {
	var resp struct {
		Items []HubCapabilityRecommendation `json:"items"`
	}
	if err := c.getJSON(ctx, "/api/capabilities/recommended", &resp); err != nil {
		return nil, err
	}
	return resp.Items, nil
}

func (c *capabilityMarketClient) reportInventory(ctx context.Context, report HubCapabilityInventoryReport) error {
	return c.putJSON(ctx, "/api/capabilities/inventory", report, nil)
}

func (c *capabilityMarketClient) getCapability(ctx context.Context, id string) (*HubCapabilitySummary, error) {
	var item HubCapabilitySummary
	if err := c.getJSON(ctx, "/api/capabilities/"+url.PathEscape(id), &item); err != nil {
		return nil, err
	}
	return &item, nil
}

func (c *capabilityMarketClient) listMCPSecretRequirements(ctx context.Context, id, versionKey string) ([]HubMCPSecretRequirement, error) {
	path := "/api/capabilities/" + url.PathEscape(id) + "/mcp-secret-requirements"
	if strings.TrimSpace(versionKey) != "" {
		path += "?version_key=" + url.QueryEscape(versionKey)
	}
	var resp struct {
		Items []HubMCPSecretRequirement `json:"items"`
	}
	if err := c.getJSON(ctx, path, &resp); err != nil {
		return nil, err
	}
	return resp.Items, nil
}

func (c *capabilityMarketClient) createInstallIntent(ctx context.Context, intent HubCapabilityInstallIntent) (*HubCapabilityInstallIntentResult, error) {
	capabilityID := strings.TrimSpace(intent.CapabilityID)
	if capabilityID == "" {
		return nil, fmt.Errorf("capability_id is required")
	}
	var resp HubCapabilityInstallIntentResult
	if err := c.postJSON(ctx, "/api/capabilities/"+url.PathEscape(capabilityID)+"/install-intent", intent, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *capabilityMarketClient) listMCPSecretBindings(ctx context.Context, mcpServerID string) ([]HubMCPSecretBinding, error) {
	path := "/api/capabilities/mcp-secret-bindings"
	if strings.TrimSpace(mcpServerID) != "" {
		path += "?mcp_server_id=" + url.QueryEscape(mcpServerID)
	}
	var resp struct {
		Items []HubMCPSecretBinding `json:"items"`
	}
	if err := c.getJSON(ctx, path, &resp); err != nil {
		return nil, err
	}
	return resp.Items, nil
}
func (c *capabilityMarketClient) saveMCPSecretBinding(ctx context.Context, binding HubMCPSecretBinding) error {
	return c.putJSON(ctx, "/api/capabilities/mcp-secret-bindings", binding, nil)
}

func (c *capabilityMarketClient) listMCPHubSecrets(ctx context.Context, mcpServerID string) ([]HubMCPHubSecret, error) {
	path := "/api/capabilities/mcp-hub-secrets"
	if strings.TrimSpace(mcpServerID) != "" {
		path += "?mcp_server_id=" + url.QueryEscape(mcpServerID)
	}
	var resp struct {
		Items []HubMCPHubSecret `json:"items"`
	}
	if err := c.getJSON(ctx, path, &resp); err != nil {
		return nil, err
	}
	return resp.Items, nil
}

func (c *capabilityMarketClient) saveMCPHubSecret(ctx context.Context, secret HubMCPHubSecretInput) (*HubMCPHubSecret, error) {
	var resp HubMCPHubSecret
	if err := c.putJSON(ctx, "/api/capabilities/mcp-hub-secrets", secret, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *capabilityMarketClient) getJSON(ctx context.Context, path string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	return c.doJSON(req, dest)
}

func (c *capabilityMarketClient) postJSON(ctx context.Context, path string, body any, dest any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.doJSON(req, dest)
}

func (c *capabilityMarketClient) putJSON(ctx context.Context, path string, body any, dest any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.doJSON(req, dest)
}

func (c *capabilityMarketClient) doJSON(req *http.Request, dest any) error {
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var body map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&body)
		return fmt.Errorf("hub marketplace request failed: status=%d body_fields=%d", resp.StatusCode, len(body))
	}
	if dest == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(dest)
}
