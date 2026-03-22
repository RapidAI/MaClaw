package oauth

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// UsageInfo 包含 OpenAI 账户用量信息。
type UsageInfo struct {
	TotalGranted   float64 `json:"total_granted"`
	TotalUsed      float64 `json:"total_used"`
	TotalAvailable float64 `json:"total_available"`
}

// DefaultCostsEndpoint 是 OpenAI 的 Costs API 地址。
const DefaultCostsEndpoint = "https://api.openai.com/v1/organization/costs"

// costsResponse 是 OpenAI Costs API 的 JSON 响应结构。
type costsResponse struct {
	Object string `json:"object"`
	Data   []struct {
		Object string `json:"object"`
		Amount struct {
			Value    float64 `json:"value"`
			Currency string  `json:"currency"`
		} `json:"amount"`
		LineItem *string `json:"line_item"`
	} `json:"data"`
	HasMore  bool   `json:"has_more"`
	NextPage string `json:"next_page"`
}

// QueryUsage 使用 access_token 查询 OpenAI 账户当月花费。
// 使用新的 /v1/organization/costs API。
func QueryUsage(accessToken string) (*UsageInfo, error) {
	return QueryUsageFrom(DefaultCostsEndpoint, accessToken)
}

// QueryUsageFrom 从指定 endpoint 查询用量（便于测试注入 mock 地址）。
func QueryUsageFrom(endpoint, accessToken string) (*UsageInfo, error) {
	// 计算当月第一天的 Unix 时间戳
	now := time.Now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	var totalUsed float64
	afterParam := "" // 分页游标

	for {
		u, err := url.Parse(endpoint)
		if err != nil {
			return nil, fmt.Errorf("parse costs endpoint: %w", err)
		}
		q := u.Query()
		q.Set("start_time", strconv.FormatInt(monthStart.Unix(), 10))
		q.Set("limit", "30")
		if afterParam != "" {
			q.Set("after", afterParam)
		}
		u.RawQuery = q.Encode()

		req, err := http.NewRequest(http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, fmt.Errorf("create costs request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("User-Agent", "OpenClaw/1.0")

		client := &http.Client{Timeout: 15 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("costs request failed: %w", err)
		}

		body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("read costs response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			msg := string(body)
			if len(msg) > 256 {
				msg = msg[:256] + "..."
			}
			return nil, fmt.Errorf("costs API error (HTTP %d): %s", resp.StatusCode, msg)
		}

		var costsResp costsResponse
		if err := json.Unmarshal(body, &costsResp); err != nil {
			return nil, fmt.Errorf("parse costs response: %w", err)
		}

		// 汇总所有 line items 的花费
		for _, item := range costsResp.Data {
			totalUsed += item.Amount.Value
		}

		if !costsResp.HasMore || len(costsResp.Data) == 0 {
			break
		}
		// 使用最后一条记录的 start_time 作为分页游标（Costs API 用 after 参数）
		afterParam = costsResp.NextPage
		if afterParam == "" {
			break
		}
	}

	// Costs API 不返回 granted/available，只返回花费。
	// 将 TotalUsed 设为当月花费，TotalGranted 和 TotalAvailable 设为 0 表示未知。
	return &UsageInfo{
		TotalGranted:   0,
		TotalUsed:      totalUsed,
		TotalAvailable: 0,
	}, nil
}
