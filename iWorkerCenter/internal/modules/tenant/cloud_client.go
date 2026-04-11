package tenant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// CloudConfig holds iWorkerCloud connection settings.
type CloudConfig struct {
	BaseURL            string `yaml:"base_url"`
	PublicKeyCacheHours int    `yaml:"public_key_cache_hours"`
}

// CloudClient communicates with iWorkerCloud.
type CloudClient struct {
	cfg    CloudConfig
	client *http.Client
}

func NewCloudClient(cfg CloudConfig) *CloudClient {
	if cfg.PublicKeyCacheHours <= 0 {
		cfg.PublicKeyCacheHours = 24
	}
	return &CloudClient{
		cfg: cfg,
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

// FetchPublicKey retrieves the iWorkerCloud RSA public key (PEM).
func (c *CloudClient) FetchPublicKey(ctx context.Context) ([]byte, error) {
	if c.cfg.BaseURL == "" {
		return nil, fmt.Errorf("cloud base_url not configured")
	}
	url := strings.TrimRight(c.cfg.BaseURL, "/") + "/api/public-key"
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
	CompanyName string `json:"company_name"`
	AdminEmail  string `json:"admin_email"`
	AdminPhone  string `json:"admin_phone"`
	Address     string `json:"address"`
	LegalPerson string `json:"legal_person"`
}

// RegisterCenterResponse is returned by iWorkerCloud.
type RegisterCenterResponse struct {
	CenterID string `json:"center_id"`
	Secret   string `json:"secret"`
	Status   string `json:"status"`
	Message  string `json:"message"`
}

// RegisterCenter calls POST /api/centers/register on iWorkerCloud.
func (c *CloudClient) RegisterCenter(ctx context.Context, req RegisterCenterRequest) (*RegisterCenterResponse, error) {
	if c.cfg.BaseURL == "" {
		return nil, fmt.Errorf("cloud base_url not configured")
	}
	url := strings.TrimRight(c.cfg.BaseURL, "/") + "/api/centers/register"
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
