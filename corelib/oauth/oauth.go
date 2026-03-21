package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pkg/browser"
)

// OpenAI OAuth 常量
const (
	// OpenAIClientID 是 Codex CLI 的 OAuth client_id（占位符，需替换为实际值）。
	OpenAIClientID = "codex-cli"

	// OpenAIAuthEndpoint 是 OpenAI 的 OAuth 授权端点。
	OpenAIAuthEndpoint = "https://auth.openai.com/oauth/authorize"

	// OpenAITokenEndpoint 是 OpenAI 的 OAuth token 端点。
	OpenAITokenEndpoint = "https://auth.openai.com/oauth/token"

	// TokenRefreshMargin 是 token 过期前触发刷新的提前量。
	TokenRefreshMargin = 5 * time.Minute
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
	AccessToken  string
	RefreshToken string
	ExpiresIn    int // 秒
}

// DefaultConfig 返回使用 OpenAI 默认值的 OAuth 配置。
func DefaultConfig() Config {
	return Config{
		ClientID:      OpenAIClientID,
		AuthEndpoint:  OpenAIAuthEndpoint,
		TokenEndpoint: OpenAITokenEndpoint,
		Scopes:        nil,
		CallbackPath:  "/auth/callback",
		Timeout:       120 * time.Second,
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
func BuildAuthURL(cfg Config, codeChallenge, redirectURI, state string) string {
	params := url.Values{}
	params.Set("client_id", cfg.ClientID)
	params.Set("redirect_uri", redirectURI)
	params.Set("response_type", "code")
	params.Set("code_challenge", codeChallenge)
	params.Set("code_challenge_method", "S256")
	if len(cfg.Scopes) > 0 {
		params.Set("scope", strings.Join(cfg.Scopes, " "))
	}
	params.Set("state", state)

	return cfg.AuthEndpoint + "?" + params.Encode()
}

// tokenResponse 是 OpenAI token endpoint 的 JSON 响应结构。
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Error        string `json:"error,omitempty"`
	ErrorDesc    string `json:"error_description,omitempty"`
}

// ExchangeCode 使用授权码向 token endpoint 换取 access_token。
// 发送 POST 请求，form-encoded body 包含 grant_type、code、code_verifier、redirect_uri、client_id。
func ExchangeCode(cfg Config, code, codeVerifier, redirectURI string) (*TokenResult, error) {
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

	return &TokenResult{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		ExpiresIn:    tok.ExpiresIn,
	}, nil
}

// truncateBody returns the body as a string, truncated to maxLen with "..." suffix.
func truncateBody(body []byte, maxLen int) string {
	s := string(body)
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}

// RunOAuthFlow 执行完整的 OAuth PKCE 流程：
//  1. 生成 code_verifier 和 code_challenge
//  2. 启动 CallbackServer
//  3. 构建 redirect_uri 和授权 URL
//  4. 打开系统浏览器
//  5. 等待回调获取授权码
//  6. 用授权码换取 token
//  7. 停止 CallbackServer
//  8. 返回 TokenResult
func RunOAuthFlow(cfg Config) (*TokenResult, error) {
	// 1. 生成 PKCE 参数
	verifier, err := GenerateCodeVerifier()
	if err != nil {
		return nil, fmt.Errorf("oauth flow: failed to generate code verifier: %w", err)
	}
	challenge := GenerateCodeChallenge(verifier)

	// 2. 启动 Callback Server
	cbServer := NewCallbackServer()
	if err := cbServer.Start(cfg.CallbackPath); err != nil {
		return nil, fmt.Errorf("oauth flow: failed to start callback server: %w", err)
	}
	defer cbServer.Stop()

	// 3. 构建 redirect_uri 和授权 URL
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d%s", cbServer.Port(), cfg.CallbackPath)
	state := challenge[:16] // 使用 challenge 前缀作为简单 state 参数
	authURL := BuildAuthURL(cfg, challenge, redirectURI, state)

	// 4. 打开系统浏览器
	if err := browser.OpenURL(authURL); err != nil {
		return nil, fmt.Errorf("oauth flow: failed to open browser: %w", err)
	}

	// 5. 等待回调获取授权码
	code, err := cbServer.WaitForCode(cfg.Timeout)
	if err != nil {
		return nil, fmt.Errorf("oauth flow: %w", err)
	}

	// 6. 用授权码换取 token
	token, err := ExchangeCode(cfg, code, verifier, redirectURI)
	if err != nil {
		return nil, fmt.Errorf("oauth flow: %w", err)
	}

	return token, nil
}
