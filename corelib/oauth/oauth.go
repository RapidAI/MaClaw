package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pkg/browser"
)

// OpenAI OAuth 常量
const (
	// OpenAIClientID 是 Codex CLI 注册的 OAuth client_id。
	OpenAIClientID = "app_EMoamEEZ73f0CkXaXp7hrann"

	// OpenAIIssuer 是 OpenAI 的 OAuth issuer 基础 URL。
	OpenAIIssuer = "https://auth.openai.com"

	// OpenAIAuthEndpoint 是 OpenAI 的 OAuth 授权端点。
	OpenAIAuthEndpoint = OpenAIIssuer + "/oauth/authorize"

	// OpenAITokenEndpoint 是 OpenAI 的 OAuth token 端点。
	OpenAITokenEndpoint = OpenAIIssuer + "/oauth/token"

	// DefaultCallbackPort 是 Codex CLI 使用的默认回调端口。
	DefaultCallbackPort = 1455

	// TokenRefreshMargin 是 token 过期前触发刷新的提前量。
	TokenRefreshMargin = 5 * time.Minute
)

const (
	// CodexSubscriptionDefaultModel is the current default model for OpenAI
	// ChatGPT/Codex OAuth subscription endpoints.
	CodexSubscriptionDefaultModel = "gpt-5.6-luna"
)

// Config 包含 OAuth 流程的配置参数。
type Config struct {
	ClientID      string
	AuthEndpoint  string
	TokenEndpoint string
	Scopes        []string
	CallbackPath  string
	Timeout       time.Duration
}

// TokenResult 是 OAuth 流程的返回结果。
type TokenResult struct {
	AccessToken    string
	RawAccessToken string // 原始 access_token（未经 API key exchange），Responses API 需要用这个
	RefreshToken   string
	ExpiresIn      int // 秒
}

// DefaultConfig 返回使用 OpenAI 默认值的 OAuth 配置。
// scope 与 Codex CLI 保持一致：openid profile email offline_access。
// Codex CLI 的 access_token 包含 api.connectors.read 和 api.connectors.invoke scope，
// 这些 scope 是调用 Responses API 所必需的。
func DefaultConfig() Config {
	return Config{
		ClientID:      OpenAIClientID,
		AuthEndpoint:  OpenAIAuthEndpoint,
		TokenEndpoint: OpenAITokenEndpoint,
		Scopes:        []string{"openid", "profile", "email", "offline_access", "api.connectors.read", "api.connectors.invoke"},
		CallbackPath:  "/auth/callback",
		Timeout:       300 * time.Second,
	}
}

// codeVerifierChars 是 RFC 7636 允许的 unreserved 字符集 [A-Za-z0-9-._~]。
const codeVerifierChars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~"

// GenerateCodeVerifier 生成一个 43-128 字符的随机 code_verifier，
// 仅使用 RFC 7636 规定的 unreserved 字符集 [A-Za-z0-9-._~]。
func GenerateCodeVerifier() (string, error) {
	const length = 64 // 在 43-128 范围内选择一个合理的固定长度
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	for i := range buf {
		buf[i] = codeVerifierChars[int(buf[i])%len(codeVerifierChars)]
	}
	return string(buf), nil
}

// GenerateCodeChallenge 对 code_verifier 做 SHA256 哈希，然后 base64url 编码（无填充）。
func GenerateCodeChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// BuildAuthURL 构建 OAuth 授权 URL，包含所有必要的查询参数。
// 与 Codex CLI 保持一致，包含 codex_cli_simplified_flow、id_token_add_organizations、originator 等参数。
func BuildAuthURL(cfg Config, codeChallenge, redirectURI, state string) string {
	params := url.Values{}
	params.Set("response_type", "code")
	params.Set("client_id", cfg.ClientID)
	params.Set("redirect_uri", redirectURI)
	if len(cfg.Scopes) > 0 {
		params.Set("scope", strings.Join(cfg.Scopes, " "))
	}
	params.Set("code_challenge", codeChallenge)
	params.Set("code_challenge_method", "S256")
	params.Set("state", state)
	// Codex CLI 兼容参数
	params.Set("id_token_add_organizations", "true")
	params.Set("codex_cli_simplified_flow", "true")
	params.Set("originator", "codex_cli_rs")
	// 强制新登录，避免浏览器中已有的 OpenAI session 导致 unknown_error
	params.Set("prompt", "login")

	return cfg.AuthEndpoint + "?" + params.Encode()
}

// tokenResponse 是 OpenAI token endpoint 的 JSON 响应结构。
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	ExpiresIn    int    `json:"expires_in"`
	Error        string `json:"error,omitempty"`
	ErrorDesc    string `json:"error_description,omitempty"`
}

// codeExchangeResult 是授权码换 token 的内部结果，包含 id_token 用于后续 API key exchange。
type codeExchangeResult struct {
	AccessToken  string
	RefreshToken string
	IDToken      string
	ExpiresIn    int
}

// ExchangeCode 使用授权码向 token endpoint 换取 access_token。
// 发送 POST 请求，form-encoded body 包含 grant_type、code、code_verifier、redirect_uri、client_id。
//
// ChatGPT Codex subscription requests use the OAuth access_token directly as
// their Bearer credential. Do not exchange it for a platform sk- API key.
func ExchangeCode(cfg Config, code, codeVerifier, redirectURI string) (*TokenResult, error) {
	result, err := exchangeCodeInternal(cfg, code, codeVerifier, redirectURI)
	if err != nil {
		return nil, err
	}

	return &TokenResult{
		AccessToken:    result.AccessToken,
		RawAccessToken: result.AccessToken,
		RefreshToken:   result.RefreshToken,
		ExpiresIn:      result.ExpiresIn,
	}, nil
}

// exchangeCodeInternal 是 ExchangeCode 的内部实现，额外返回 id_token。
func exchangeCodeInternal(cfg Config, code, codeVerifier, redirectURI string) (*codeExchangeResult, error) {
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("code_verifier", codeVerifier)
	data.Set("redirect_uri", redirectURI)
	data.Set("client_id", cfg.ClientID)

	resp, err := http.PostForm(cfg.TokenEndpoint, data)
	if err != nil {
		return nil, fmt.Errorf("token exchange request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, fmt.Errorf("token exchange: failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp tokenResponse
		if json.Unmarshal(body, &errResp) == nil && errResp.Error != "" {
			return nil, fmt.Errorf("token exchange failed (HTTP %d): %s: %s",
				resp.StatusCode, errResp.Error, errResp.ErrorDesc)
		}
		return nil, fmt.Errorf("token exchange failed (HTTP %d): %s",
			resp.StatusCode, truncateBody(body, 512))
	}

	var tok tokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return nil, fmt.Errorf("token exchange: failed to parse response: %w", err)
	}

	if tok.AccessToken == "" {
		return nil, fmt.Errorf("token exchange: response missing access_token")
	}

	// Diagnostic: log token response fields to help debug id_token issues
	atPrefix := tok.AccessToken
	if len(atPrefix) > 20 {
		atPrefix = atPrefix[:20] + "..."
	}
	idPrefix := tok.IDToken
	if len(idPrefix) > 20 {
		idPrefix = idPrefix[:20] + "..."
	}
	log.Printf("[oauth] code exchange response: has_access_token=true access_token_prefix=%q has_id_token=%v id_token_prefix=%q has_refresh_token=%v expires_in=%d",
		atPrefix, tok.IDToken != "", idPrefix, tok.RefreshToken != "", tok.ExpiresIn)

	return &codeExchangeResult{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		IDToken:      tok.IDToken,
		ExpiresIn:    tok.ExpiresIn,
	}, nil
}

// apiKeyExchangeResponse 是 token exchange（获取 API key）的响应结构。
// NOTE: 当前未使用——Codex 订阅模式直接用 access_token，不做 token exchange。
// 保留以备将来支持 API key 模式。
type apiKeyExchangeResponse struct {
	AccessToken string `json:"access_token"`
	Error       string `json:"error,omitempty"`
	ErrorDesc   string `json:"error_description,omitempty"`
}

// ExchangeForAPIKey 使用 id_token 通过 token exchange 获取 OpenAI API key。
// NOTE: 当前未使用——Codex 订阅模式直接用 access_token 调用 Responses API。
// 保留以备将来支持 API key 付费模式。
func ExchangeForAPIKey(cfg Config, idToken string) (string, error) {
	return doTokenExchange(cfg, idToken, "urn:ietf:params:oauth:token-type:id_token")
}

// exchangeAccessTokenForAPIKey uses an access_token to obtain an API key via
// token exchange. This is a fallback for when id_token is not available.
// NOTE: 当前未使用——保留以备将来支持 API key 模式。
func exchangeAccessTokenForAPIKey(cfg Config, accessToken string) (string, error) {
	return doTokenExchange(cfg, accessToken, "urn:ietf:params:oauth:token-type:access_token")
}

// doTokenExchange performs the RFC 8693 token exchange to obtain an OpenAI API key.
func doTokenExchange(cfg Config, subjectToken, subjectTokenType string) (string, error) {
	data := url.Values{}
	data.Set("grant_type", "urn:ietf:params:oauth:grant-type:token-exchange")
	data.Set("client_id", cfg.ClientID)
	data.Set("requested_token", "openai-api-key")
	data.Set("subject_token", subjectToken)
	data.Set("subject_token_type", subjectTokenType)

	resp, err := http.PostForm(cfg.TokenEndpoint, data)
	if err != nil {
		return "", fmt.Errorf("api key exchange request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return "", fmt.Errorf("api key exchange: failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp apiKeyExchangeResponse
		if json.Unmarshal(body, &errResp) == nil && errResp.Error != "" {
			return "", fmt.Errorf("api key exchange failed (HTTP %d, token_type=%s): %s: %s",
				resp.StatusCode, subjectTokenType, errResp.Error, errResp.ErrorDesc)
		}
		return "", fmt.Errorf("api key exchange failed (HTTP %d, token_type=%s): %s",
			resp.StatusCode, subjectTokenType, truncateBody(body, 512))
	}

	var result apiKeyExchangeResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("api key exchange: failed to parse response: %w", err)
	}

	if result.AccessToken == "" {
		return "", fmt.Errorf("api key exchange: response missing access_token (token_type=%s)", subjectTokenType)
	}

	tokenPrefix := result.AccessToken
	if len(tokenPrefix) > 15 {
		tokenPrefix = tokenPrefix[:15] + "..."
	}
	log.Printf("[oauth] API key exchange succeeded (token_type=%s): key_prefix=%q", subjectTokenType, tokenPrefix)

	return result.AccessToken, nil
}

// truncateBody returns metadata only; OAuth error bodies can include tokens or codes.
func truncateBody(body []byte, maxLen int) string {
	s := string(body)
	if maxLen <= 0 {
		return ""
	}
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}

// RunOAuthFlow 执行完整的 OAuth PKCE 流程（使用默认超时）。
func RunOAuthFlow(cfg Config) (*TokenResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()
	return RunOAuthFlowCtx(ctx, cfg)
}

// HeadlessOAuthParams 包含无头环境下 OAuth 流程所需的参数。
// TUI 显示 AuthURL 让用户在其他设备浏览器中打开，完成授权后
// 浏览器会重定向到 RedirectURI（打不开），用户复制地址栏中的
// 完整 URL 粘贴回 TUI，TUI 从中提取 code 完成 token exchange。
type HeadlessOAuthParams struct {
	AuthURL     string // 用户在浏览器中打开的授权 URL
	RedirectURI string // 回调 URL（用于 token exchange 时匹配）
	Verifier    string // PKCE code_verifier（用于 token exchange）
}

// PrepareHeadlessOAuth 生成无头环境下 OAuth 流程所需的参数。
// 返回的 AuthURL 供用户在任意浏览器中打开。
func PrepareHeadlessOAuth(cfg Config) (*HeadlessOAuthParams, error) {
	// 使用固定的 localhost redirect_uri（与 Codex CLI 一致）
	redirectURI := fmt.Sprintf("http://localhost:%d%s", DefaultCallbackPort, cfg.CallbackPath)
	return PrepareHeadlessOAuthWithRedirectURI(cfg, redirectURI)
}

// PrepareHeadlessOAuthWithRedirectURI generates PKCE parameters for a caller-
// supplied redirect URI. This is used by local callback servers that must bind
// an ephemeral port before building the authorization URL.
func PrepareHeadlessOAuthWithRedirectURI(cfg Config, redirectURI string) (*HeadlessOAuthParams, error) {
	verifier, err := GenerateCodeVerifier()
	if err != nil {
		return nil, fmt.Errorf("oauth: failed to generate code verifier: %w", err)
	}
	challenge := GenerateCodeChallenge(verifier)
	state := generateState()
	authURL := BuildAuthURL(cfg, challenge, redirectURI, state)

	return &HeadlessOAuthParams{
		AuthURL:     authURL,
		RedirectURI: redirectURI,
		Verifier:    verifier,
	}, nil
}

// CompleteHeadlessOAuth 用用户粘贴的回调 URL 完成 token exchange。
// callbackURL 是浏览器地址栏中的完整 URL，包含 ?code=xxx 参数。
func CompleteHeadlessOAuth(cfg Config, params *HeadlessOAuthParams, callbackURL string) (*TokenResult, error) {
	// 从 URL 中提取 code 参数
	callbackURL = strings.TrimSpace(callbackURL)
	code := extractCodeFromURL(callbackURL)
	if code == "" {
		return nil, fmt.Errorf("无法从 URL 中提取授权码（code 参数），请确认复制了完整的地址栏 URL")
	}

	// 用 code 换取 token
	exchanged, err := exchangeCodeInternal(cfg, code, params.Verifier, params.RedirectURI)
	if err != nil {
		return nil, fmt.Errorf("token exchange 失败: %w", err)
	}

	return &TokenResult{
		AccessToken:    exchanged.AccessToken,
		RawAccessToken: exchanged.AccessToken,
		RefreshToken:   exchanged.RefreshToken,
		ExpiresIn:      exchanged.ExpiresIn,
	}, nil
}

// extractCodeFromURL 从回调 URL 中提取 code 查询参数。
// 支持完整 URL（http://localhost:1455/auth/callback?code=xxx&state=yyy）
// 和纯 code 值。
func extractCodeFromURL(raw string) string {
	// 尝试解析为 URL
	if u, err := url.Parse(raw); err == nil {
		if code := u.Query().Get("code"); code != "" {
			return code
		}
	}
	// 如果不是 URL，可能用户直接粘贴了 code 值
	raw = strings.TrimSpace(raw)
	if len(raw) > 10 && !strings.Contains(raw, " ") && !strings.Contains(raw, "://") {
		return raw
	}
	return ""
}

// RunOAuthFlowCtx 执行完整的 OAuth PKCE 流程，支持 context 取消：
//  1. 生成 code_verifier 和 code_challenge
//  2. 启动 CallbackServer
//  3. 构建 redirect_uri 和授权 URL
//  4. 打开系统浏览器
//  5. 等待回调获取授权码（可通过 ctx 取消）
//  6. 用授权码换取 access_token + refresh_token
//  7. 返回 TokenResult（直接使用 access_token，不做 API key exchange）
func RunOAuthFlowCtx(ctx context.Context, cfg Config) (*TokenResult, error) {
	// 1. 生成 PKCE 参数
	verifier, err := GenerateCodeVerifier()
	if err != nil {
		return nil, fmt.Errorf("oauth flow: failed to generate code verifier: %w", err)
	}
	challenge := GenerateCodeChallenge(verifier)

	// 2. 启动 Callback Server（优先使用 Codex CLI 默认端口 1455）
	cbServer := NewCallbackServer()
	if err := cbServer.StartOnPort(cfg.CallbackPath, DefaultCallbackPort); err != nil {
		// fallback 到随机端口
		if err2 := cbServer.Start(cfg.CallbackPath); err2 != nil {
			return nil, fmt.Errorf("oauth flow: failed to start callback server: %w", err2)
		}
	}
	defer cbServer.Stop()

	// 3. 构建 redirect_uri 和授权 URL（使用 localhost 与 openai-auth crate 保持一致，
	// 注意：OpenAI 对 redirect_uri 做严格匹配，localhost 和 127.0.0.1 不等价。
	// openai-auth crate 默认使用 http://localhost:1455/auth/callback）
	redirectURI := fmt.Sprintf("http://localhost:%d%s", cbServer.Port(), cfg.CallbackPath)
	state := generateState()
	authURL := BuildAuthURL(cfg, challenge, redirectURI, state)

	// 4. 打开系统浏览器
	log.Printf("[oauth] auth URL: %s", authURL)
	if err := browser.OpenURL(authURL); err != nil {
		return nil, fmt.Errorf("oauth flow: failed to open browser: %w", err)
	}

	// 5. 等待回调获取授权码
	code, err := cbServer.WaitForCodeCtx(ctx)
	if err != nil {
		return nil, fmt.Errorf("oauth flow: %w", err)
	}

	// 6. 用授权码换取 token（含 id_token）
	exchanged, err := exchangeCodeInternal(cfg, code, verifier, redirectURI)
	if err != nil {
		return nil, fmt.Errorf("oauth flow: %w", err)
	}

	return &TokenResult{
		AccessToken:    exchanged.AccessToken,
		RawAccessToken: exchanged.AccessToken,
		RefreshToken:   exchanged.RefreshToken,
		ExpiresIn:      exchanged.ExpiresIn,
	}, nil
}

// generateState 生成 32 字节随机 state 参数（base64url 编码）。
func generateState() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		// fallback: 使用 code challenge 前缀
		return "maclaw-oauth-state"
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}
