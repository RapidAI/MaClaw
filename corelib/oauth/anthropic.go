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
)

// ─────────────────────────────────────────────────────────────────────────────
// Anthropic OAuth — Claude Pro/Max 订阅登录（PKCE flow）
// 基于 Claude Code 使用的 OAuth 端点和 client_id。
// ─────────────────────────────────────────────────────────────────────────────

const (
	// AnthropicClientID 是 Claude Code 注册的 OAuth client_id。
	AnthropicClientID = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"

	// AnthropicAuthEndpoint 是 Anthropic 的 OAuth 授权端点（claude.ai）。
	AnthropicAuthEndpoint = "https://claude.ai/oauth/authorize"

	// AnthropicTokenEndpoint 是 Anthropic 的 OAuth token 端点。
	AnthropicTokenEndpoint = "https://console.anthropic.com/v1/oauth/token"

	// AnthropicRedirectURI 是 Anthropic 的固定回调 URI。
	// Claude Code 使用 Anthropic 托管的回调页面，不是 localhost。
	// 用户授权后浏览器跳转到此 URL 并显示 code，用户复制回 CLI。
	AnthropicRedirectURI = "https://console.anthropic.com/oauth/code/callback"

	// AnthropicAPIBaseURL 是使用 OAuth token 调用 API 的基础 URL。
	AnthropicAPIBaseURL = "https://api.anthropic.com"
)

// AnthropicOAuthConfig 返回 Anthropic OAuth 的配置。
func AnthropicOAuthConfig() Config {
	return Config{
		ClientID:      AnthropicClientID,
		AuthEndpoint:  AnthropicAuthEndpoint,
		TokenEndpoint: AnthropicTokenEndpoint,
		Scopes:        []string{"org:create_api_key", "user:profile", "user:inference"},
		CallbackPath:  "", // 不使用本地回调——Anthropic 使用远程托管的回调页面
		Timeout:       300 * time.Second,
	}
}

// AnthropicOAuthParams 包含 Anthropic OAuth 流程的参数。
type AnthropicOAuthParams struct {
	AuthURL  string // 用户在浏览器中打开的授权 URL
	Verifier string // PKCE code_verifier（用于 token exchange）
	State    string // state 参数（与 verifier 相同值）
}

// PrepareAnthropicOAuth 生成 Anthropic OAuth 流程的参数。
// 返回的 AuthURL 供用户在浏览器中打开。用户授权后，Anthropic 的回调页面
// 会显示 authorization code，用户复制后粘贴回来完成登录。
func PrepareAnthropicOAuth() (*AnthropicOAuthParams, error) {
	verifier, err := GenerateCodeVerifier()
	if err != nil {
		return nil, fmt.Errorf("anthropic oauth: generate verifier: %w", err)
	}
	challenge := GenerateCodeChallenge(verifier)

	// Anthropic 使用 verifier 本身作为 state（与 Claude Code 行为一致）
	state := verifier

	params := url.Values{}
	params.Set("code", "true")
	params.Set("response_type", "code")
	params.Set("client_id", AnthropicClientID)
	params.Set("redirect_uri", AnthropicRedirectURI)
	params.Set("scope", strings.Join(AnthropicOAuthConfig().Scopes, " "))
	params.Set("code_challenge", challenge)
	params.Set("code_challenge_method", "S256")
	params.Set("state", state)

	authURL := AnthropicAuthEndpoint + "?" + params.Encode()

	return &AnthropicOAuthParams{
		AuthURL:  authURL,
		Verifier: verifier,
		State:    state,
	}, nil
}

// CompleteAnthropicOAuth 用用户提供的 authorization code 完成 token exchange。
func CompleteAnthropicOAuth(params *AnthropicOAuthParams, code string) (*TokenResult, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, fmt.Errorf("anthropic oauth: authorization code is empty")
	}

	// Token exchange — POST JSON（Anthropic 要求 JSON，不是 form-encoded）
	reqBody := map[string]string{
		"grant_type":    "authorization_code",
		"code":          code,
		"code_verifier": params.Verifier,
		"client_id":     AnthropicClientID,
		"redirect_uri":  AnthropicRedirectURI,
	}
	if params.State != "" {
		reqBody["state"] = params.State
	}

	bodyBytes, _ := json.Marshal(reqBody)
	req, err := http.NewRequest("POST", AnthropicTokenEndpoint, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return nil, fmt.Errorf("anthropic oauth: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "anthropic")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic oauth: token request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, fmt.Errorf("anthropic oauth: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic oauth: token exchange failed (HTTP %d): body_len=%d", resp.StatusCode, len(body))
	}

	var tok struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tok); err != nil {
		return nil, fmt.Errorf("anthropic oauth: parse token response: %w", err)
	}
	if tok.AccessToken == "" {
		return nil, fmt.Errorf("anthropic oauth: response missing access_token")
	}

	log.Printf("[anthropic-oauth] token exchange succeeded, expires_in=%d", tok.ExpiresIn)
	return &TokenResult{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		ExpiresIn:    tok.ExpiresIn,
	}, nil
}

// RefreshAnthropicToken 使用 refresh_token 获取新的 access_token。
func RefreshAnthropicToken(refreshToken string) (*TokenResult, error) {
	reqBody := map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": refreshToken,
		"client_id":     AnthropicClientID,
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req, err := http.NewRequest("POST", AnthropicTokenEndpoint, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return nil, fmt.Errorf("anthropic token refresh: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "anthropic")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic token refresh: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, fmt.Errorf("anthropic token refresh: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic token refresh failed (HTTP %d): body_len=%d", resp.StatusCode, len(body))
	}

	var tok struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tok); err != nil {
		return nil, fmt.Errorf("anthropic token refresh: parse response: %w", err)
	}
	if tok.AccessToken == "" {
		return nil, fmt.Errorf("anthropic token refresh: response missing access_token")
	}

	log.Printf("[anthropic-oauth] refresh succeeded, expires_in=%d", tok.ExpiresIn)
	return &TokenResult{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		ExpiresIn:    tok.ExpiresIn,
	}, nil
}

// RunAnthropicOAuthFlow is not directly usable — Anthropic requires a remote
// callback (console.anthropic.com) that cannot be intercepted by a localhost
// server. Use the two-step flow instead:
//   1. PrepareAnthropicOAuth() → get auth URL
//   2. User opens URL, authorizes, copies code from Anthropic's callback page
//   3. CompleteAnthropicOAuth(params, code) → get tokens
func RunAnthropicOAuthFlow(ctx context.Context) (*TokenResult, error) {
	_ = ctx
	return nil, fmt.Errorf("anthropic oauth: use PrepareAnthropicOAuth + CompleteAnthropicOAuth (remote callback requires user to paste code)")
}
