package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
)

// ─────────────────────────────────────────────────────────────────────────────
// GitHub account OAuth — Device Code Flow (RFC 8628) for the app-level GitHub
// connection. Unlike github_copilot.go (which exchanges the token for a Copilot
// API token), this flow keeps the plain GitHub OAuth token for generic API
// access: skill search, update checks, and code cloning.
// ─────────────────────────────────────────────────────────────────────────────

const (
	// GitHubAccountClientID 是仓库登录用的 OAuth App（GitHub CLI 的公开
	// client，设备码流程不需要 client secret）。不能用 Copilot 的
	// GitHubCopilotClientID：那是 GitHub App（Iv1.）。GitHub App 的设备码
	// 流程忽略 scope（只认 offline_access），令牌以 ghu_ 开头，响应里的
	// scope 和 X-OAuth-Scopes 都是空的，仓库内容和 webhook 都没有权限。
	// 用户可以在设置里粘贴自己的 OAuth App Client ID 覆盖授权页名称。
	GitHubAccountClientID = "178c6fc778ccc68e1d6a"

	// GitHubAccountScopes 是连接请求的默认 scope。read:user 读取账号信息，
	// repo 允许读取/克隆私有仓库，并包含仓库 webhook。
	GitHubAccountScopes = "read:user repo"

	// GitHubAPIBase 是 GitHub REST API 基础地址。
	GitHubAPIBase = "https://api.github.com"

	// GitHubAccountProviderID 是 credential store 中该连接的 provider id。
	GitHubAccountProviderID = "github"

	// GitHubAccountTimeout 是 device code 流程的最大等待时间。
	GitHubAccountTimeout = 300 * time.Second
)

// GitHubUser is the subset of GET /user we surface in the settings UI.
type GitHubUser struct {
	Login     string `json:"login"`
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
	HTMLURL   string `json:"html_url"`
}

// RequestGitHubDeviceCodeWith is RequestGitHubDeviceCode parameterized by
// client_id and scope, so account-level flows can reuse the same endpoint.
func RequestGitHubDeviceCodeWith(ctx context.Context, clientID, scope string) (*DeviceCodeResponse, error) {
	reqBody, err := json.Marshal(map[string]string{"client_id": clientID, "scope": scope})
	if err != nil {
		return nil, fmt.Errorf("github device code: encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", GitHubDeviceCodeURL, strings.NewReader(string(reqBody)))
	if err != nil {
		return nil, fmt.Errorf("github device code: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := DoNoFollow(req)
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

	log.Printf("[github] device code requested: user_code=%s verification_uri=%s expires_in=%d",
		result.UserCode, result.VerificationURI, result.ExpiresIn)
	return &result, nil
}

// PollGitHubDeviceCodeWith polls the token endpoint like PollGitHubDeviceCode
// but exchanges against the given client_id. Per RFC 8628 §3.5 each slow_down
// grows the poll interval by 5s. scope is the grant from the token response.
// OAuth Apps return it there (for example "read:user,repo"). GitHub Apps
// always return an empty scope; that empty string is the grant, not a
// missing field.
func PollGitHubDeviceCodeWith(ctx context.Context, clientID, deviceCode string, interval int) (token, scope string, err error) {
	if interval < 1 {
		interval = GitHubCopilotDefaultInterval
	}
	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", "", fmt.Errorf("github device code: %w", ctx.Err())
		case <-ticker.C:
			token, scope, retry, slowDown, err := tryExchangeDeviceCodeWith(ctx, clientID, deviceCode)
			if err != nil {
				return "", "", err
			}
			if slowDown {
				interval += 5
				ticker.Reset(time.Duration(interval) * time.Second)
			}
			if !retry {
				return token, scope, nil
			}
		}
	}
}

// tryExchangeDeviceCodeWith is tryExchangeDeviceCode with a caller-chosen client_id.
// The slowDown return tells the caller to grow its interval per RFC 8628.
func tryExchangeDeviceCodeWith(ctx context.Context, clientID, deviceCode string) (token, scope string, retry, slowDown bool, err error) {
	reqBody, err := json.Marshal(map[string]string{
		"client_id":   clientID,
		"device_code": deviceCode,
		"grant_type":  "urn:ietf:params:oauth:grant-type:device_code",
	})
	if err != nil {
		return "", "", false, false, fmt.Errorf("github token exchange: encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", GitHubTokenURL, strings.NewReader(string(reqBody)))
	if err != nil {
		return "", "", false, false, fmt.Errorf("github token exchange: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := DoNoFollow(req)
	if err != nil {
		return "", "", true, false, nil // 网络错误，重试
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return "", "", true, false, nil
	}
	if resp.StatusCode >= 500 {
		// A transient 5xx answers with HTML, not the device-flow JSON error
		// field — retry like a network failure rather than failing the login.
		return "", "", true, false, nil
	}
	return parseGitHubDeviceToken(body)
}

// parseGitHubDeviceToken reads one device-flow token response. scope is the
// granted OAuth scope list. Callers must keep an empty scope: a GitHub App
// token is issued with scope "" and that is not "field missing".
func parseGitHubDeviceToken(body []byte) (token, scope string, retry, slowDown bool, err error) {
	var result struct {
		AccessToken string `json:"access_token"`
		Scope       string `json:"scope"`
		Error       string `json:"error"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", "", false, false, fmt.Errorf("github token exchange: parse response: %w", err)
	}

	switch result.Error {
	case "":
		if result.AccessToken == "" {
			return "", "", false, false, fmt.Errorf("github token exchange: empty access_token")
		}
		return result.AccessToken, strings.TrimSpace(result.Scope), false, false, nil
	case "authorization_pending":
		return "", "", true, false, nil
	case "slow_down":
		return "", "", true, true, nil
	case "expired_token":
		return "", "", false, false, fmt.Errorf("github device code expired — user did not authorize in time")
	case "access_denied":
		return "", "", false, false, fmt.Errorf("github: user denied authorization")
	default:
		return "", "", false, false, fmt.Errorf("github token exchange error: %s", result.Error)
	}
}

// GitHubAppClientID reports a GitHub App client id. Those ids start with
// Iv1. (sometimes written lv1.). Device flow against them does not grant
// OAuth scopes, so a request for "repo" cannot add repository access.
func GitHubAppClientID(id string) bool {
	id = strings.ToLower(strings.TrimSpace(id))
	return strings.HasPrefix(id, "iv1.") || strings.HasPrefix(id, "lv1.")
}

// GitHubAppUserToken reports a GitHub App user-to-server token ("ghu_").
// Those tokens carry no OAuth scopes at all — their repository access would
// come from the app's configuration — so a missing-scope verdict on them
// means "re-login with an OAuth App", never "reconnect to add the scope".
func GitHubAppUserToken(token string) bool {
	return strings.HasPrefix(strings.TrimSpace(token), "ghu_")
}

// GitHubEffectiveScopes is the scope list to store. The /user response
// header is the live grant. The token response is the fallback, because a
// GitHub App leaves the header empty and an OAuth App still lists the grant
// on the token itself ("read:user,repo").
func GitHubEffectiveScopes(headerScopes, grantedScopes string) string {
	if s := strings.TrimSpace(headerScopes); s != "" {
		return s
	}
	return strings.TrimSpace(grantedScopes)
}

// GitHubScopesHaveRepo reports the exact OAuth scope "repo".
// public_repo and repo:status do not grant repository hooks.
// Commas and whitespace are separators, the same split the maintenance
// page uses on the stored grant.
func GitHubScopesHaveRepo(scopeList string) bool {
	for _, part := range strings.FieldsFunc(scopeList, func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	}) {
		if part == "repo" {
			return true
		}
	}
	return false
}

// FetchGitHubUser returns the authenticated account profile for token, plus the
// granted scopes reported by the X-OAuth-Scopes response header.
func FetchGitHubUser(ctx context.Context, token string) (*GitHubUser, string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", GitHubAPIBase+"/user", nil)
	if err != nil {
		return nil, "", fmt.Errorf("github /user: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "MaClaw/1.0")

	resp, err := DoNoFollow(req)
	if err != nil {
		return nil, "", fmt.Errorf("github /user: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return nil, "", fmt.Errorf("github /user: read response: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, "", fmt.Errorf("github /user: token is invalid or revoked")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("github /user: HTTP %d: body_len=%d", resp.StatusCode, len(body))
	}

	var user GitHubUser
	if err := json.Unmarshal(body, &user); err != nil {
		return nil, "", fmt.Errorf("github /user: parse response: %w", err)
	}
	if user.Login == "" {
		return nil, "", fmt.Errorf("github /user: missing login in response")
	}
	return &user, resp.Header.Get("X-OAuth-Scopes"), nil
}

// DoGitHubAPI performs an authenticated api.github.com request. path may be an
// absolute https://api.github.com URL or a site-relative path like
// "/search/repositories?q=...". Returns the response body.
func DoGitHubAPI(token, method, apiPath string) (string, error) {
	urlStr := apiPath
	if !strings.HasPrefix(urlStr, "https://") {
		urlStr = GitHubAPIBase + "/" + strings.TrimPrefix(urlStr, "/")
	}
	// Prefix matching is not enough: "https://api.github.com.evil.com" would
	// pass it and receive the bearer token. Parse and pin the exact host.
	parsed, err := url.Parse(urlStr)
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Host, "api.github.com") {
		return "", fmt.Errorf("github api: only https api.github.com paths are allowed")
	}
	if method == "" {
		method = "GET"
	}
	// Read-only on purpose: a bodyless POST is useless here anyway, and a
	// read-only passthrough cannot be turned into a write primitive.
	switch strings.ToUpper(method) {
	case "GET", "HEAD":
	default:
		return "", fmt.Errorf("github api: method %s is not allowed", method)
	}

	req, err := http.NewRequest(strings.ToUpper(method), urlStr, nil)
	if err != nil {
		return "", fmt.Errorf("github api: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "MaClaw/1.0")

	resp, err := DoNoFollow(req)
	if err != nil {
		return "", fmt.Errorf("github api: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return "", fmt.Errorf("github api: unexpected redirect (HTTP %d)", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
	if err != nil {
		return "", fmt.Errorf("github api: read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("github api: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body[:min(len(body), 512)])))
	}
	return string(body), nil
}
