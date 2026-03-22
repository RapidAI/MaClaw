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
// scope 与 Codex CLI 保持一致，包含 api.connectors.read 和 api.connectors.invoke。
func DefaultConfig() Config {
	return Config{
		ClientID:      OpenAIClientID,
		AuthEndpoint:  OpenAIAuthEndpoint,
		TokenEndpoint: OpenAITokenEndpoint,
		Scopes:        []string{"openid", "profile", "email", "offline_access", "api.connectors.read", "api.connectors.invoke"},
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
	params.Set("originator", "pi")

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
func ExchangeCode(cfg Config, code, codeVerifier, redirectURI string) (*TokenResult, error) {
	result, err := exchangeCodeInternal(cfg, code, codeVerifier, redirectURI)
	if err != nil {
		return nil, err
	}
	return &TokenResult{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		ExpiresIn:    result.ExpiresIn,
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

	return &codeExchangeResult{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		IDToken:      tok.IDToken,
		ExpiresIn:    tok.ExpiresIn,
	}, nil
}

// apiKeyExchangeResponse 是 token exchange（获取 API key）的响应结构。
type apiKeyExchangeResponse struct {
	AccessToken string `json:"access_token"`
	Error       string `json:"error,omitempty"`
	ErrorDesc   string `json:"error_description,omitempty"`
}

// ExchangeForAPIKey 使用 id_token 通过 token exchange 获取 OpenAI API key。
// 这与 Codex CLI 的 obtain_api_key 逻辑一致：
// grant_type=urn:ietf:params:oauth:grant-type:token-exchange
// subject_token_type=urn:ietf:params:oauth:token-type:id_token
// requested_token=openai-api-key
func ExchangeForAPIKey(cfg Config, idToken string) (string, error) {
	data := url.Values{}
	data.Set("grant_type", "urn:ietf:params:oauth:grant-type:token-exchange")
	data.Set("client_id", cfg.ClientID)
	data.Set("requested_token", "openai-api-key")
	data.Set("subject_token", idToken)
	data.Set("subject_token_type", "urn:ietf:params:oauth:token-type:id_token")

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
			return "", fmt.Errorf("api key exchange failed (HTTP %d): %s: %s",
				resp.StatusCode, errResp.Error, errResp.ErrorDesc)
		}
		return "", fmt.Errorf("api key exchange failed (HTTP %d): %s",
			resp.StatusCode, truncateBody(body, 512))
	}

	var result apiKeyExchangeResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("api key exchange: failed to parse response: %w", err)
	}

	if result.AccessToken == "" {
		return "", fmt.Errorf("api key exchange: response missing access_token")
	}

	return result.AccessToken, nil
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
//  6. 用授权码换取 token（含 id_token）
//  7. 用 id_token 通过 token exchange 获取 API key
//  8. 停止 CallbackServer
//  9. 返回 TokenResult（Key 为 API key）
func RunOAuthFlow(cfg Config) (*TokenResult, error) {
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

	// 3. 构建 redirect_uri 和授权 URL（使用 localhost 与 Codex CLI 保持一致）
	redirectURI := fmt.Sprintf("http://localhost:%d%s", cbServer.Port(), cfg.CallbackPath)
	state := generateState()
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

	// 6. 用授权码换取 token（含 id_token）
	exchanged, err := exchangeCodeInternal(cfg, code, verifier, redirectURI)
	if err != nil {
		return nil, fmt.Errorf("oauth flow: %w", err)
	}

	// 7. 用 id_token 通过 token exchange 获取 API key
	// 如果 id_token 存在，尝试获取 API key；否则 fallback 到 access_token
	apiKey := exchanged.AccessToken
	if exchanged.IDToken != "" {
		if key, err := ExchangeForAPIKey(cfg, exchanged.IDToken); err == nil {
			apiKey = key
		} else {
			// API key exchange 失败，fallback 到 access_token。
			// 注意：access_token 可能无法直接调用 /v1/chat/completions，
			// 如果后续 LLM 调用失败，请检查此处。
			fmt.Printf("[oauth] warning: API key exchange failed, falling back to access_token: %v\n", err)
		}
	} else {
		fmt.Println("[oauth] warning: no id_token in response, using access_token directly")
	}

	return &TokenResult{
		AccessToken:  apiKey,
		RefreshToken: exchanged.RefreshToken,
		ExpiresIn:    exchanged.ExpiresIn,
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
