package oauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// UsageInfo 包含 OpenAI 账户用量信息。
type UsageInfo struct {
	TotalGranted   float64 `json:"total_granted"`
	TotalUsed      float64 `json:"total_used"`
	TotalAvailable float64 `json:"total_available"`
}

// DefaultCostsEndpoint 是 OpenAI 的 Costs API 地址。
const DefaultCostsEndpoint = "https://api.openai.com/v1/organization/costs"

const maxOrganizationCostPages = 40

// costsResponse matches the official organization costs page, including the
// nested per-bucket results[] used by GET /v1/organization/costs.
type costsResponse struct {
	Object   string        `json:"object"`
	Data     []costsBucket `json:"data"`
	HasMore  bool          `json:"has_more"`
	NextPage *string       `json:"next_page"`
}

type costsBucket struct {
	Object   string          `json:"object"`
	Amount   costAmount      `json:"amount"`
	Results  []costsLineItem `json:"results"`
	LineItem *string         `json:"line_item"`
}

type costsLineItem struct {
	Amount costAmount `json:"amount"`
}

type costAmount struct {
	Value    json.Number `json:"value"`
	Currency string      `json:"currency"`
}

func (a costAmount) float() float64 {
	if strings.TrimSpace(a.Value.String()) == "" {
		return 0
	}
	f, err := a.Value.Float64()
	if err != nil {
		return 0
	}
	return f
}

func (bucket costsBucket) costTotal() float64 {
	if len(bucket.Results) > 0 {
		var total float64
		for _, result := range bucket.Results {
			total += result.Amount.float()
		}
		return total
	}
	return bucket.Amount.float()
}

// LooksLikeOpenAIAdminKey reports whether key is an OpenAI Admin API key.
// Organization costs require sk-admin-..., not ChatGPT/Codex OAuth tokens
// and not regular project API keys.
func LooksLikeOpenAIAdminKey(key string) bool {
	return strings.HasPrefix(strings.TrimSpace(key), "sk-admin-")
}

// OrganizationCostsUnsupportedReason explains why provider cannot query
// OpenAI organization costs. An empty string means the query may proceed.
func OrganizationCostsUnsupportedReason(provider corelib.MaclawLLMProvider) string {
	if provider.IsCodexSubscriptionOAuthProvider() || strings.Contains(strings.ToLower(provider.URL), "chatgpt.com") {
		return "ChatGPT/Codex 登录无法查询 OpenAI 组织账单，请查看 Token 用量统计"
	}
	if strings.EqualFold(strings.TrimSpace(provider.AuthType), "oauth") {
		return "OAuth 凭证无法查询 OpenAI 组织账单（需要 Admin API Key sk-admin-...）"
	}
	if !LooksLikeOpenAIAdminKey(provider.Key) {
		if isOpenAINamedProvider(provider.Name) || isOpenAIOrganizationCostsHost(provider.URL) {
			return "查询 OpenAI 组织账单需要 Admin API Key（sk-admin-...），普通 API Key 不可用"
		}
		return "当前服务商不支持 OpenAI 组织账单查询"
	}
	if !isOpenAINamedProvider(provider.Name) && !isOpenAIOrganizationCostsHost(provider.URL) {
		return "当前服务商不支持 OpenAI 组织账单查询"
	}
	return ""
}

// QueryOrganizationCosts queries OpenAI organization costs when the provider
// actually has an Admin API key. It does not send OAuth tokens to OpenAI.
func QueryOrganizationCosts(provider corelib.MaclawLLMProvider) (*UsageInfo, error) {
	if reason := OrganizationCostsUnsupportedReason(provider); reason != "" {
		return nil, errors.New(reason)
	}
	return QueryUsage(strings.TrimSpace(provider.Key))
}

func isOpenAINamedProvider(name string) bool {
	name = strings.TrimSpace(name)
	return strings.EqualFold(name, "OpenAI") || strings.EqualFold(name, "OpenAI Official")
}

func isOpenAIOrganizationCostsHost(rawURL string) bool {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return false
	}
	if !strings.Contains(rawURL, "://") {
		rawURL = "https://" + rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return strings.Contains(strings.ToLower(rawURL), "api.openai.com")
	}
	return strings.EqualFold(u.Hostname(), "api.openai.com")
}

// QueryUsage 使用 Admin API key 查询 OpenAI 账户当月花费。
func QueryUsage(accessToken string) (*UsageInfo, error) {
	return QueryUsageFrom(DefaultCostsEndpoint, accessToken)
}

// QueryUsageFrom 从指定 endpoint 查询用量（便于测试注入 mock 地址）。
func QueryUsageFrom(endpoint, accessToken string) (*UsageInfo, error) {
	now := time.Now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	var totalUsed float64
	pageCursor := ""

	for page := 0; page < maxOrganizationCostPages; page++ {
		u, err := url.Parse(endpoint)
		if err != nil {
			return nil, fmt.Errorf("parse costs endpoint: %w", err)
		}
		q := u.Query()
		q.Set("start_time", strconv.FormatInt(monthStart.Unix(), 10))
		// Daily buckets; 32 covers any calendar month in one request (API max is 180).
		q.Set("limit", "32")
		if pageCursor != "" {
			q.Set("page", pageCursor)
		}
		u.RawQuery = q.Encode()

		req, err := http.NewRequest(http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, fmt.Errorf("create costs request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("User-Agent", "MaClaw")

		resp, err := httpClient().Do(req)
		if err != nil {
			return nil, annotateOAuthNetworkError("costs request failed", err)
		}

		body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("read costs response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, costsAPIError(resp.StatusCode, body)
		}

		var direct UsageInfo
		if err := json.Unmarshal(body, &direct); err == nil {
			if direct.TotalGranted != 0 || direct.TotalUsed != 0 || direct.TotalAvailable != 0 {
				return &direct, nil
			}
		}

		var costsResp costsResponse
		if err := json.Unmarshal(body, &costsResp); err != nil {
			return nil, fmt.Errorf("parse costs response: %w", err)
		}

		for _, item := range costsResp.Data {
			totalUsed += item.costTotal()
		}

		if !costsResp.HasMore || len(costsResp.Data) == 0 || costsResp.NextPage == nil {
			break
		}
		next := strings.TrimSpace(*costsResp.NextPage)
		if next == "" || next == pageCursor {
			break
		}
		pageCursor = next
	}

	// Costs API 不返回 granted/available，只返回花费。
	return &UsageInfo{
		TotalGranted:   0,
		TotalUsed:      totalUsed,
		TotalAvailable: 0,
	}, nil
}

func costsAPIError(status int, body []byte) error {
	var wrapped struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &wrapped) == nil {
		if msg := strings.TrimSpace(wrapped.Error.Message); msg != "" {
			return fmt.Errorf("costs API error (HTTP %d): %s", status, msg)
		}
	}
	snippet := strings.TrimSpace(truncateBody(body, 240))
	if snippet == "" {
		return fmt.Errorf("costs API error (HTTP %d)", status)
	}
	return fmt.Errorf("costs API error (HTTP %d): %s", status, snippet)
}
