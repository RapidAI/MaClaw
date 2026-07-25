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

	"github.com/RapidAI/CodeClaw/corelib"
)

// RefreshXAIToken refreshes an xAI OAuth token using the token endpoint
// currently advertised by xAI's OIDC discovery document.
func RefreshXAIToken(ctx context.Context, refreshToken string) (*TokenResult, error) {
	discovery, err := DiscoverOIDCEndpoints(ctx, XAIOAuthIssuer)
	if err != nil {
		return nil, err
	}
	cfg := XAIConfig()
	cfg.TokenEndpoint = discovery.TokenEndpoint
	return RefreshAccessTokenCtx(ctx, cfg, refreshToken)
}

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

// RefreshAccessToken 使用 refresh_token 获取新的 access_token。
// 直接返回 access_token 用于 Responses API，不做 token exchange。
func RefreshAccessToken(cfg Config, refreshToken string) (*TokenResult, error) {
	return RefreshAccessTokenCtx(context.Background(), cfg, refreshToken)
}

// RefreshAccessTokenCtx refreshes an OAuth credential while preserving caller
// cancellation and the existing short network timeout.
func RefreshAccessTokenCtx(ctx context.Context, cfg Config, refreshToken string) (*TokenResult, error) {
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", refreshToken)
	data.Set("client_id", cfg.ClientID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.TokenEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("token refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
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

	// Codex subscription endpoints authenticate with the refreshed OAuth JWT.
	// A token exchange yields a platform sk- key, which those endpoints reject.
	return &TokenResult{
		AccessToken:    tok.AccessToken,
		RawAccessToken: tok.AccessToken,
		RefreshToken:   tok.RefreshToken,
		ExpiresIn:      tok.ExpiresIn,
	}, nil
}

// ApplyTokenResult 将 TokenResult 应用到 provider 并返回更新后的副本。
// Key 和 OAuthAccessToken 都设为 access_token（Codex 订阅模式不区分）。
// RefreshToken 仅在非空时更新（保留旧值），TokenExpiresAt 设为 now + ExpiresIn。
func ApplyTokenResult(provider corelib.MaclawLLMProvider, result *TokenResult) corelib.MaclawLLMProvider {
	provider.Key = result.AccessToken
	if result.RawAccessToken != "" {
		provider.OAuthAccessToken = result.RawAccessToken
	}
	if result.RefreshToken != "" {
		provider.RefreshToken = result.RefreshToken
	}
	provider.TokenExpiresAt = time.Now().Unix() + int64(result.ExpiresIn)

	keyPrefix := result.AccessToken
	if len(keyPrefix) > 15 {
		keyPrefix = keyPrefix[:15] + "..."
	}
	log.Printf("[oauth] ApplyTokenResult: token_prefix=%q expires_in=%d",
		keyPrefix, result.ExpiresIn)

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
