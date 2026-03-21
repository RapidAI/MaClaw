package oauth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// NeedsRefresh 检查 provider 的 access_token 是否即将过期。
// 如果 AuthType 为空或不是 "oauth"，返回 false（向后兼容）。
// 如果 TokenExpiresAt 为 0，返回 false（无过期信息）。
// 当前时间 + TokenRefreshMargin (5 min) >= TokenExpiresAt 时返回 true。
func NeedsRefresh(provider corelib.MaclawLLMProvider) bool {
	if provider.AuthType != "oauth" {
		return false
	}
	if provider.TokenExpiresAt == 0 {
		return false
	}
	return time.Now().Unix()+int64(TokenRefreshMargin.Seconds()) >= provider.TokenExpiresAt
}

// refreshRequest 是发送给 OpenAI token endpoint 的 JSON 刷新请求。
// 与 Codex CLI 保持一致，使用 JSON 格式而非 form-encoded。
type refreshRequest struct {
	ClientID     string `json:"client_id"`
	GrantType    string `json:"grant_type"`
	RefreshToken string `json:"refresh_token"`
}

// RefreshAccessToken 使用 refresh_token 获取新的 access_token。
// 使用 JSON POST 请求（与 Codex CLI 保持一致）。
// 如果响应包含 id_token，会自动通过 token exchange 获取 API key。
func RefreshAccessToken(cfg Config, refreshToken string) (*TokenResult, error) {
	reqBody := refreshRequest{
		ClientID:     cfg.ClientID,
		GrantType:    "refresh_token",
		RefreshToken: refreshToken,
	}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("token refresh: failed to marshal request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, cfg.TokenEndpoint, bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("token refresh: failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, fmt.Errorf("token refresh: failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp tokenResponse
		if json.Unmarshal(body, &errResp) == nil && errResp.Error != "" {
			return nil, fmt.Errorf("token refresh failed (HTTP %d): %s: %s",
				resp.StatusCode, errResp.Error, errResp.ErrorDesc)
		}
		return nil, fmt.Errorf("token refresh failed (HTTP %d): %s",
			resp.StatusCode, truncateBody(body, 512))
	}

	var tok tokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return nil, fmt.Errorf("token refresh: failed to parse response: %w", err)
	}

	if tok.AccessToken == "" {
		return nil, fmt.Errorf("token refresh: response missing access_token")
	}

	// 如果响应包含 id_token，尝试通过 token exchange 获取 API key
	apiKey := tok.AccessToken
	if tok.IDToken != "" {
		if key, err := ExchangeForAPIKey(cfg, tok.IDToken); err == nil {
			apiKey = key
		}
	}

	return &TokenResult{
		AccessToken:  apiKey,
		RefreshToken: tok.RefreshToken,
		ExpiresIn:    tok.ExpiresIn,
	}, nil
}

// ApplyTokenResult 将 TokenResult 应用到 provider 并返回更新后的副本。
// Key 设为 AccessToken，RefreshToken 仅在非空时更新（保留旧值），
// TokenExpiresAt 设为 now + ExpiresIn。
func ApplyTokenResult(provider corelib.MaclawLLMProvider, result *TokenResult) corelib.MaclawLLMProvider {
	provider.Key = result.AccessToken
	if result.RefreshToken != "" {
		provider.RefreshToken = result.RefreshToken
	}
	provider.TokenExpiresAt = time.Now().Unix() + int64(result.ExpiresIn)
	return provider
}

// EnsureValidToken 检查并在需要时刷新 token，返回更新后的 provider。
// 如果 AuthType 不是 "oauth"，直接返回原 provider。
// 如果 token 不需要刷新，直接返回原 provider。
// 如果 refresh_token 为空，返回错误提示重新登录。
// 刷新成功后调用 saveFn 持久化。
func EnsureValidToken(provider corelib.MaclawLLMProvider, cfg Config, saveFn func(corelib.MaclawLLMProvider) error) (corelib.MaclawLLMProvider, error) {
	if provider.AuthType != "oauth" {
		return provider, nil
	}
	if !NeedsRefresh(provider) {
		return provider, nil
	}
	if provider.RefreshToken == "" {
		return provider, fmt.Errorf("refresh_token is empty, please re-login")
	}

	result, err := RefreshAccessToken(cfg, provider.RefreshToken)
	if err != nil {
		return provider, fmt.Errorf("token refresh failed: %w", err)
	}

	provider = ApplyTokenResult(provider, result)

	if err := saveFn(provider); err != nil {
		return provider, fmt.Errorf("failed to persist refreshed token: %w", err)
	}

	return provider, nil
}
