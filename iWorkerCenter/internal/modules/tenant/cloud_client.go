package tenant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// CloudConfig holds iWorkerCloud connection settings.
type CloudConfig struct {
	BaseURL           string `yaml:"base_url"`
	CenterBaseURL     string `yaml:"center_base_url"`
	RegistrationName  string `yaml:"registration_name"`
	RegistrationEmail string `yaml:"registration_email"`
	CloudControlMode  string `yaml:"cloud_control_mode"`
}

// CloudClient communicates with iWorkerCloud.
type CloudClient struct {
	mu     sync.RWMutex
	cfg    CloudConfig
	client *http.Client
}

func NewCloudClient(cfg CloudConfig) *CloudClient {
	c := &CloudClient{client: &http.Client{Timeout: 15 * time.Second}}
	c.SetConfig(cfg)
	return c
}

// Config returns a sanitized copy of the current iWorkerCloud settings.
func (c *CloudClient) Config() CloudConfig {
	if c == nil {
		return CloudConfig{CloudControlMode: "cloud_managed"}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cfg
}

// SetConfig updates the iWorkerCloud settings used by subsequent requests.
func (c *CloudClient) SetConfig(cfg CloudConfig) {
	if c == nil {
		return
	}
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	cfg.CenterBaseURL = strings.TrimRight(strings.TrimSpace(cfg.CenterBaseURL), "/")
	cfg.RegistrationName = strings.TrimSpace(cfg.RegistrationName)
	cfg.RegistrationEmail = strings.TrimSpace(cfg.RegistrationEmail)
	cfg.CloudControlMode = strings.TrimSpace(cfg.CloudControlMode)
	if cfg.CloudControlMode == "" {
		cfg.CloudControlMode = "cloud_managed"
	}
	c.mu.Lock()
	c.cfg = cfg
	c.mu.Unlock()
}

// FetchPublicKey retrieves the iWorkerCloud RSA public key (PEM).
func (c *CloudClient) FetchPublicKey(ctx context.Context) ([]byte, error) {
	cfg := c.Config()
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("cloud base_url not configured")
	}
	url := strings.TrimRight(cfg.BaseURL, "/") + "/api/public-key"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch public key: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch public key: status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return nil, fmt.Errorf("read public key body: %w", err)
	}
	return data, nil
}

// RegisterCenterRequest is sent to iWorkerCloud to register this center.
type RegisterCenterRequest struct {
	CompanyName         string `json:"company_name"`
	AdminEmail          string `json:"admin_email"`
	AdminPhone          string `json:"admin_phone,omitempty"`
	Address             string `json:"address,omitempty"`
	LegalPerson         string `json:"legal_person,omitempty"`
	BaseURL             string `json:"base_url,omitempty"`
	CloudControlMode    string `json:"cloud_control_mode,omitempty"`
	SupportsMultiTenant bool   `json:"supports_multi_tenant,omitempty"`
	TenantCount         int    `json:"tenant_count,omitempty"`
}

// RegisterCenterResponse is returned by iWorkerCloud.
type RegisterCenterResponse struct {
	CenterID string `json:"center_id"`
	Secret   string `json:"secret"`
	Status   string `json:"status"`
	Message  string `json:"message"`
}

// CloudLicense is the active license record returned by iWorkerCloud.
type CloudLicense struct {
	ID          string     `json:"id"`
	CenterID    string     `json:"center_id"`
	Modules     string     `json:"modules"`
	Type        string     `json:"type"`
	ExpiresAt   time.Time  `json:"expires_at"`
	IsLongTerm  bool       `json:"is_long_term"`
	Certificate string     `json:"certificate"`
	CreatedAt   time.Time  `json:"created_at"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
}

// RegisterCenter calls POST /api/centers/register on iWorkerCloud.
func (c *CloudClient) RegisterCenter(ctx context.Context, req RegisterCenterRequest) (*RegisterCenterResponse, error) {
	cfg := c.Config()
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("cloud base_url not configured")
	}
	url := strings.TrimRight(cfg.BaseURL, "/") + "/api/centers/register"
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("register center: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("register center: status %d, body: %s", resp.StatusCode, string(respBody))
	}
	var result RegisterCenterResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode register response: %w", err)
	}
	return &result, nil
}

// FetchCenterLicense retrieves the active license for a registered Center.
func (c *CloudClient) FetchCenterLicense(ctx context.Context, centerID, centerSecret string) (*CloudLicense, error) {
	cfg := c.Config()
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("cloud base_url not configured")
	}
	centerID = strings.TrimSpace(centerID)
	if centerID == "" {
		return nil, fmt.Errorf("center_id is required")
	}
	if centerSecret == "" {
		return nil, fmt.Errorf("center_secret is required")
	}

	url := fmt.Sprintf("%s/api/centers/%s/license", strings.TrimRight(cfg.BaseURL, "/"), centerID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Center-Secret", centerSecret)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch center license: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("fetch center license: status %d, body: %s", resp.StatusCode, string(respBody))
	}

	var result CloudLicense
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode center license response: %w", err)
	}
	return &result, nil
}

// CloudComputeProvider is a compute provider assignment returned by iWorkerCloud.
type CloudComputeProvider struct {
	ID                   string  `json:"id"`
	Name                 string  `json:"name"`
	BaseURL              string  `json:"base_url"`
	APIKey               string  `json:"api_key,omitempty"`
	Protocol             string  `json:"protocol"`
	UserAgent            string  `json:"user_agent"`
	ComputeType          string  `json:"compute_type"`
	Model                string  `json:"model"`
	Enabled              bool    `json:"enabled"`
	Priority             int     `json:"priority"`
	Description          string  `json:"description"`
	InputPricePerMToken  float64 `json:"input_price_per_mtoken"`
	OutputPricePerMToken float64 `json:"output_price_per_mtoken"`
}

// CenterComputeProvidersResponse is returned by iWorkerCloud for a registered Center.
type CenterComputeProvidersResponse struct {
	Providers         []CloudComputeProvider `json:"providers"`
	ComputePermission bool                   `json:"compute_permission"`
	ForceSync         bool                   `json:"force_sync"`
}

// FetchCenterComputeProviders retrieves compute providers assigned to this Center.
func (c *CloudClient) FetchCenterComputeProviders(ctx context.Context, centerID, centerSecret string) (*CenterComputeProvidersResponse, error) {
	cfg := c.Config()
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("cloud base_url not configured")
	}
	centerID = strings.TrimSpace(centerID)
	if centerID == "" {
		return nil, fmt.Errorf("center_id is required")
	}
	if centerSecret == "" {
		return nil, fmt.Errorf("center_secret is required")
	}

	url := fmt.Sprintf("%s/api/centers/%s/compute-providers", strings.TrimRight(cfg.BaseURL, "/"), centerID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Center-Secret", centerSecret)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch center compute providers: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("fetch center compute providers: status %d, body: %s", resp.StatusCode, string(respBody))
	}

	var result CenterComputeProvidersResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode center compute providers response: %w", err)
	}
	if result.Providers == nil {
		result.Providers = []CloudComputeProvider{}
	}
	return &result, nil
}

// CenterHeartbeatRequest identifies this runtime as an iWorkerCenter service.
type CenterHeartbeatRequest struct {
	Secret              string                  `json:"secret"`
	RuntimeType         string                  `json:"runtime_type"`
	ProductKind         string                  `json:"product_kind"`
	AdminConsole        string                  `json:"admin_console"`
	ProviderCount       int                     `json:"provider_count,omitempty"`
	RuntimeProviderMode string                  `json:"runtime_provider_mode,omitempty"`
	ComputeSource       string                  `json:"compute_source,omitempty"`
	ComputePermission   bool                    `json:"compute_permission,omitempty"`
	CloudProviderCount  int                     `json:"cloud_provider_count,omitempty"`
	ComputeSyncStatus   *CloudComputeSyncStatus `json:"compute_sync_status,omitempty"`
	IWorkerReadiness    *CloudIWorkerReadiness  `json:"iworker_readiness,omitempty"`
}

type CloudComputeSyncStatus struct {
	LastSyncAt    string `json:"last_sync_at"`
	Status        string `json:"status"`
	Error         string `json:"error,omitempty"`
	ProviderCount int    `json:"provider_count"`
	NonBlocking   bool   `json:"non_blocking,omitempty"`
	RuntimeImpact string `json:"runtime_impact,omitempty"`
}

type CloudCenterRuntime struct {
	ProviderCount       int                     `json:"provider_count,omitempty"`
	RuntimeProviderMode string                  `json:"runtime_provider_mode,omitempty"`
	ComputeSource       string                  `json:"compute_source,omitempty"`
	ComputePermission   bool                    `json:"compute_permission,omitempty"`
	CloudProviderCount  int                     `json:"cloud_provider_count,omitempty"`
	ComputeSyncStatus   *CloudComputeSyncStatus `json:"compute_sync_status,omitempty"`
}

// CloudIWorkerReadiness is the customer-side iWorker operating readiness sent to Cloud.
type CloudIWorkerReadiness struct {
	Ready              bool                  `json:"ready"`
	Status             string                `json:"status"`
	AgentInstanceCount int                   `json:"agent_instance_count"`
	AgentRuntimeReady  bool                  `json:"agent_runtime_ready"`
	GoalWatchReady     bool                  `json:"goalwatch_ready"`
	WorkloadSummary    *CloudWorkloadSummary `json:"workload_summary,omitempty"`
}

type CloudWorkloadSummary struct {
	AgentInstanceCount int    `json:"agent_instance_count"`
	ActiveCount        int    `json:"active_count"`
	CompletedCount     int    `json:"completed_count"`
	ReviewCount        int    `json:"review_count"`
	BlockedCount       int    `json:"blocked_count"`
	UpdatedAt          string `json:"updated_at,omitempty"`
}

func NewCenterHeartbeatRequest(centerSecret string) CenterHeartbeatRequest {
	return CenterHeartbeatRequest{Secret: centerSecret, RuntimeType: "service", ProductKind: "iworkercenter", AdminConsole: "web_console"}
}

func NewCenterHeartbeatRequestWithReadiness(centerSecret string, readiness *CloudIWorkerReadiness, runtime *CloudCenterRuntime) CenterHeartbeatRequest {
	req := NewCenterHeartbeatRequest(centerSecret)
	req.IWorkerReadiness = readiness
	if runtime != nil {
		req.ProviderCount = runtime.ProviderCount
		req.RuntimeProviderMode = runtime.RuntimeProviderMode
		req.ComputeSource = runtime.ComputeSource
		req.ComputePermission = runtime.ComputePermission
		req.CloudProviderCount = runtime.CloudProviderCount
		req.ComputeSyncStatus = runtime.ComputeSyncStatus
	}
	return req
}

// SendCenterHeartbeat reports that this iWorkerCenter service instance is alive.
func (c *CloudClient) SendCenterHeartbeat(ctx context.Context, centerID, centerSecret string, readiness *CloudIWorkerReadiness, runtime *CloudCenterRuntime) error {
	cfg := c.Config()
	if cfg.BaseURL == "" {
		return fmt.Errorf("cloud base_url not configured")
	}
	centerID = strings.TrimSpace(centerID)
	if centerID == "" {
		return fmt.Errorf("center_id is required")
	}
	if centerSecret == "" {
		return fmt.Errorf("center_secret is required")
	}

	url := fmt.Sprintf("%s/api/centers/%s/heartbeat", strings.TrimRight(cfg.BaseURL, "/"), centerID)
	body, err := json.Marshal(NewCenterHeartbeatRequestWithReadiness(centerSecret, readiness, runtime))
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send center heartbeat: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("send center heartbeat: status %d, body: %s", resp.StatusCode, string(respBody))
	}
	return nil
}
