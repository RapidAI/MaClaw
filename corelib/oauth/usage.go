package oauth

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// UsageInfo 包含 OpenAI 账户用量信息。
type UsageInfo struct {
	TotalGranted   float64 `json:"total_granted"`
	TotalUsed      float64 `json:"total_used"`
	TotalAvailable float64 `json:"total_available"`
}

// DefaultBillingEndpoint 是 OpenAI 的 billing API 地址。
const DefaultBillingEndpoint = "https://api.openai.com/dashboard/billing/credit_grants"

// QueryUsage 使用 access_token 查询 OpenAI 账户用量。
func QueryUsage(accessToken string) (*UsageInfo, error) {
	return QueryUsageFrom(DefaultBillingEndpoint, accessToken)
}

// QueryUsageFrom 从指定 endpoint 查询用量（便于测试注入 mock 地址）。
func QueryUsageFrom(endpoint, accessToken string) (*UsageInfo, error) {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create usage request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("User-Agent", "OpenClaw/1.0")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("usage request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, fmt.Errorf("read usage response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		msg := string(body)
		if len(msg) > 256 {
			msg = msg[:256] + "..."
		}
		return nil, fmt.Errorf("usage API error (HTTP %d): %s", resp.StatusCode, msg)
	}

	var info UsageInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("parse usage response: %w", err)
	}

	return &info, nil
}
