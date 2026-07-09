package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// GitHub Copilot OAuth — Device Code Flow（RFC 8628）
// 用户在任意设备打开 URL 并输入 8 字符 code，CLI 后台轮询直到用户完成授权。
// ─────────────────────────────────────────────────────────────────────────────

const (
	// GitHubCopilotClientID 是 VS Code Copilot 使用的 OAuth client_id。
	// 这个 client_id 允许 device flow 且生成的 token 可以用于 Copilot API。
	GitHubCopilotClientID = "Iv1.b507a08c87ecfe98"

	// GitHubDeviceCodeURL 是 GitHub 的 device code 请求端点。
	GitHubDeviceCodeURL = "https://github.com/login/device/code"

	// GitHubTokenURL 是 GitHub 的 OAuth token 端点。
	GitHubTokenURL = "https://github.com/login/oauth/access_token"

	// GitHubCopilotTokenURL 是将 GitHub token 交换为 Copilot API token 的端点。
	GitHubCopilotTokenURL = "https://api.github.com/copilot_internal/v2/token"

	// GitHubCopilotScope 是 device code 请求的 scope。
	GitHubCopilotScope = "read:user"

	// GitHubCopilotDefaultInterval 是默认轮询间隔（秒）。
	GitHubCopilotDefaultInterval = 5

	// GitHubCopilotTimeout 是 device code 流程的最大等待时间。
	GitHubCopilotTimeout = 300 * time.Second
)

// DeviceCodeResponse 是 GitHub device code 端点的响应。
type DeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// CopilotTokenResponse 是 Copilot token exchange 的响应。
type CopilotTokenResponse struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
	// 还有其他字段（endpoints 等），但我们只需要 token 和 expiry
}

// RequestGitHubDeviceCode 向 GitHub 请求 device code。
// 返回的 UserCode 需要显示给用户，让用户在 VerificationURI 输入。
func RequestGitHubDeviceCode() (*DeviceCodeResponse, error) {
	reqBody := fmt.Sprintf(`{"client_id":"%s","scope":"%s"}`, GitHubCopilotClientID, GitHubCopilotScope)

	req, err := http.NewRequest("POST", GitHubDeviceCodeURL, strings.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("github device code: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github device code: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, fmt.Errorf("github device code: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github device code: HTTP %d: body_len=%d", resp.StatusCode, len(body))
	}

	var result DeviceCodeResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("github device code: parse response: %w", err)
	}
	if result.DeviceCode == "" || result.UserCode == "" {
		return nil, fmt.Errorf("github device code: missing device_code or user_code")
	}
	if result.Interval == 0 {
		result.Interval = GitHubCopilotDefaultInterval
	}

	log.Printf("[github-copilot] device code requested: user_code=%s verification_uri=%s expires_in=%d",
		result.UserCode, result.VerificationURI, result.ExpiresIn)
	return &result, nil
}

// PollGitHubDeviceCode 轮询 GitHub token 端点直到用户完成授权或超时。
// 返回的是 GitHub access_token（还需要 exchange 为 Copilot token）。
func PollGitHubDeviceCode(ctx context.Context, deviceCode string, interval int) (string, error) {
	if interval < 1 {
		interval = GitHubCopilotDefaultInterval
	}
	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("github device code: %w", ctx.Err())
		case <-ticker.C:
			token, retry, err := tryExchangeDeviceCode(deviceCode)
			if err != nil {
				return "", err
			}
			if !retry {
				return token, nil
			}
			// retry == true: 用户还没完成授权，继续轮询
		}
	}
}

// tryExchangeDeviceCode 尝试一次 device code → token exchange。
// 返回 (token, shouldRetry, error)。
func tryExchangeDeviceCode(deviceCode string) (string, bool, error) {
	reqBody := fmt.Sprintf(`{"client_id":"%s","device_code":"%s","grant_type":"urn:ietf:params:oauth:grant-type:device_code"}`,
		GitHubCopilotClientID, deviceCode)

	req, err := http.NewRequest("POST", GitHubTokenURL, strings.NewReader(reqBody))
	if err != nil {
		return "", false, fmt.Errorf("github token exchange: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", true, nil // 网络错误，重试
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return "", true, nil
	}

	var result struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", false, fmt.Errorf("github token exchange: parse response: %w", err)
	}

	switch result.Error {
	case "":
		if result.AccessToken == "" {
			return "", false, fmt.Errorf("github token exchange: empty access_token")
		}
		return result.AccessToken, false, nil
	case "authorization_pending":
		return "", true, nil // 用户还没完成，继续轮询
	case "slow_down":
		time.Sleep(5 * time.Second) // 增加间隔
		return "", true, nil
	case "expired_token":
		return "", false, fmt.Errorf("github device code expired — user did not authorize in time")
	case "access_denied":
		return "", false, fmt.Errorf("github: user denied authorization")
	default:
		return "", false, fmt.Errorf("github token exchange error: %s", result.Error)
	}
}

// ExchangeGitHubTokenForCopilot 将 GitHub access_token 交换为 Copilot API token。
// Copilot token 有短有效期（通常 30 分钟），需要用原始 GitHub token 重新获取。
func ExchangeGitHubTokenForCopilot(githubToken string) (*CopilotTokenResponse, error) {
	req, err := http.NewRequest("GET", GitHubCopilotTokenURL, nil)
	if err != nil {
		return nil, fmt.Errorf("copilot token exchange: create request: %w", err)
	}
	req.Header.Set("Authorization", "token "+githubToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "MaClaw/1.0")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("copilot token exchange: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, fmt.Errorf("copilot token exchange: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("copilot token exchange: HTTP %d (check Copilot subscription): body_len=%d", resp.StatusCode, len(body))
	}

	var result CopilotTokenResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("copilot token exchange: parse response: %w", err)
	}
	if result.Token == "" {
		return nil, fmt.Errorf("copilot token exchange: missing token in response")
	}

	log.Printf("[github-copilot] token exchange succeeded, expires_at=%d", result.ExpiresAt)
	return &result, nil
}

// RunGitHubCopilotFlow is a placeholder — NOT IMPLEMENTED.
// Use GitHubCopilotLogin() for the full async flow, or the GUI bindings
// (StartGitHubCopilotOAuth + WaitGitHubCopilotOAuth) for Wails integration.
//
// Deprecated: This function exists only for interface symmetry and will be removed.
func RunGitHubCopilotFlow(ctx context.Context) (*DeviceCodeResponse, func() (*TokenResult, error)) {
	_ = ctx
	return nil, nil
}

// GitHubCopilotLogin 执行完整的 device code flow 并返回结果。
// 阻塞直到用户完成授权或超时。
func GitHubCopilotLogin(ctx context.Context) (*DeviceCodeResponse, <-chan GitHubCopilotLoginResult, error) {
	deviceResp, err := RequestGitHubDeviceCode()
	if err != nil {
		return nil, nil, err
	}

	resultCh := make(chan GitHubCopilotLoginResult, 1)
	go func() {
		githubToken, err := PollGitHubDeviceCode(ctx, deviceResp.DeviceCode, deviceResp.Interval)
		if err != nil {
			resultCh <- GitHubCopilotLoginResult{Error: err}
			return
		}

		// 验证 Copilot 订阅可用性
		copilotToken, err := ExchangeGitHubTokenForCopilot(githubToken)
		if err != nil {
			resultCh <- GitHubCopilotLoginResult{Error: fmt.Errorf("GitHub 认证成功但 Copilot 订阅不可用: %w", err)}
			return
		}

		resultCh <- GitHubCopilotLoginResult{
			Token: &TokenResult{
				AccessToken: githubToken,
				ExpiresIn:   int(copilotToken.ExpiresAt - time.Now().Unix()),
			},
			CopilotToken:     copilotToken.Token,
			CopilotExpiresAt: copilotToken.ExpiresAt,
		}
	}()

	return deviceResp, resultCh, nil
}

// GitHubCopilotLoginResult 是 GitHubCopilotLogin 的异步结果。
type GitHubCopilotLoginResult struct {
	Token            *TokenResult // GitHub access_token（长期有效，用于刷新 Copilot token）
	CopilotToken     string       // 短期 Copilot API token
	CopilotExpiresAt int64        // Copilot token 过期时间（Unix timestamp）
	Error            error
}
