package oauth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/pkg/browser"
)

// CodeGen SSO 配置常量。
// 这些值由企业 IT 提供，指向奇安信内部 codegen SSO 服务。
const (
	// CodeGenSSOLoginURL 是 CodeGen SSO 扫码登录页面地址（旧版，轮询模式）。
	CodeGenSSOLoginURL = "https://codegen.qianxin-inc.cn/api/v1/auth/sso/get-token"

	// CodeGenSSOLoginBaseURL 是 CodeGen SSO 登录入口（支持 ref 回调参数）。
	// 用法：CodeGenSSOLoginBaseURL + "?ref=" + url.QueryEscape(callbackURL)
	// SSO 完成后浏览器会重定向到 ref 指定的 URL，并附带 token 查询参数。
	CodeGenSSOLoginBaseURL = "https://codegen.qianxin-inc.cn/api/v1/auth/sso/login"

	// CodeGenModelsEndpoint 是 CodeGen 模型列表 API 端点。
	CodeGenModelsEndpoint = "https://codegen.qianxin-inc.cn/api/v1/models"

	// CodeGenBaseURL 是 CodeGen API 的基础地址。
	CodeGenBaseURL = "https://codegen.qianxin-inc.cn/api/v1"

	// CodeGenTimeout 是 SSO 扫码流程的超时时间。
	CodeGenTimeout = 5 * time.Minute

	// CodeGenPollInterval 是轮询 token 的间隔时间。
	CodeGenPollInterval = 2 * time.Second

	// CodeGen SSO OAuth 2.0 (PKCE) 配置
	CodeGenClientID      = "codegen-maclaw-client"
	CodeGenAuthEndpoint  = "https://codegen.qianxin-inc.cn/api/v1/auth/sso/authorize"
	CodeGenTokenEndpoint = "https://codegen.qianxin-inc.cn/api/v1/auth/sso/token"
)

// CodeGenOAuthConfig 返回 CodeGen SSO 的 OAuth 配置。
func CodeGenOAuthConfig() Config {
	return Config{
		ClientID:      CodeGenClientID,
		AuthEndpoint:  CodeGenAuthEndpoint,
		TokenEndpoint: CodeGenTokenEndpoint,
		Scopes:        []string{"openid", "profile", "email", "offline_access"},
		CallbackPath:  "/auth/codegen/callback",
		Timeout:       CodeGenTimeout,
	}
}

// CodeGenSSOResult 保存从 CodeGen SSO 回调中解析出的认证信息。
type CodeGenSSOResult struct {
	// AccessToken 是用于调用 CodeGen 模型 API 的凭证。
	AccessToken string

	// BaseURL 是 CodeGen 模型服务的 API 基础地址（如 https://codegen.qianxin-inc.cn/api/v1）。
	BaseURL string

	// ModelID 是默认使用的模型标识（如 claude-3-5-sonnet-20241022）。
	ModelID string

	// ContextLength 是模型支持的最大上下文 token 数（可选）。
	ContextLength int

	// Email 是从 id_token JWT payload 中解析出的用户邮件地址。
	// 需要 SSO scope 包含 "openid profile email"。
	Email string
}

// codeGenScanResponse 是扫码页面返回的 token 响应结构。
type codeGenScanResponse struct {
	Token   string `json:"token"`
	Email   string `json:"email"`
	Status  string `json:"status"`  // "pending", "success", "expired"
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

// codeGenModelsResponse 是 /api/v1/models 端点的响应结构。
type codeGenModelsResponse struct {
	// 兼容两种响应格式：旧版使用 "models"，新版使用 "data"
	Models []struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		Provider      string `json:"provider"`
		ContextWindow int    `json:"context_window"`
	} `json:"models"`
	Data []struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		Provider      string `json:"provider"`
		ContextWindow int    `json:"context_window"`
	} `json:"data"`
	BaseURL string `json:"base_url,omitempty"`
	Success bool   `json:"success,omitempty"`
	Error   string `json:"error,omitempty"`
}

// GetModels 返回模型列表，兼容 "models" 和 "data" 两种响应字段。
func (r *codeGenModelsResponse) GetModels() []struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Provider      string `json:"provider"`
	ContextWindow int    `json:"context_window"`
} {
	if len(r.Models) > 0 {
		return r.Models
	}
	return r.Data
}

// jwtEmailPayload 是 JWT payload 中用于提取 email 的结构体。
type jwtEmailPayload struct {
	Email string `json:"email"`
}

// ExtractEmailFromJWT 从 JWT token 的 payload 中提取 email 字段。
// JWT 格式为 header.payload.signature，按 "." 分割取第二段 payload，
// 使用 base64 RawURLEncoding 解码（处理无 padding 的 base64url），
// 然后 JSON 解析提取 email 字段。
// 校验 email 非空且包含 "@"。
// 解析失败返回 ("", error)，不 panic。
func ExtractEmailFromJWT(token string) (string, error) {
	parts := strings.SplitN(token, ".", 4)
	if len(parts) < 3 {
		return "", fmt.Errorf("ExtractEmailFromJWT: invalid JWT format, expected 3 parts, got %d", len(parts))
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("ExtractEmailFromJWT: base64 decode failed: %w", err)
	}

	var claims jwtEmailPayload
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", fmt.Errorf("ExtractEmailFromJWT: JSON parse failed: %w", err)
	}

	email := strings.TrimSpace(claims.Email)
	if email == "" {
		return "", fmt.Errorf("ExtractEmailFromJWT: email field is empty")
	}

	if !strings.Contains(email, "@") {
		return "", fmt.Errorf("ExtractEmailFromJWT: invalid email format, missing @")
	}

	return email, nil
}

// AllowedSSODomains 是 QR_Extractor 允许请求的域名白名单。
// 仅允许重定向到这些域名，防止 SSRF。
var AllowedSSODomains = []string{
	"codegen.qianxin-inc.cn",
	"zerotrust.qianxin-inc.cn",
}

// codeGenHTTPClient 是 CodeGen SSO 请求的共享 HTTP 客户端。
var codeGenHTTPClient = &http.Client{Timeout: 30 * time.Second}

// NeedsRefreshCodeGen 检查 CodeGen SSO token 是否即将过期。
// 仅对 AuthType == "sso" 的 provider 生效。
// TokenExpiresAt == 0 表示无过期信息（旧数据），返回 false。
// 提前 5 分钟刷新。
func NeedsRefreshCodeGen(provider corelib.MaclawLLMProvider) bool {
	if provider.AuthType != "sso" {
		return false
	}
	if provider.TokenExpiresAt == 0 {
		return false
	}
	return time.Now().Unix()+300 >= provider.TokenExpiresAt
}

// ValidateCodeGenToken 用当前 token 调用 /models 端点验证有效性。
// 返回 true 表示 token 仍然有效。
func ValidateCodeGenToken(token string) bool {
	if token == "" {
		return false
	}
	req, err := http.NewRequest("GET", CodeGenModelsEndpoint, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.ReadAll(io.LimitReader(resp.Body, 1024))

	// 2xx = 有效，401/403 = 过期/无效
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// codeGenRefreshRequest 是 /api/v1/auth/refresh 的请求体。
type codeGenRefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// codeGenRefreshResponse 是 /api/v1/auth/refresh 的响应体。
type codeGenRefreshResponse struct {
	Token        string `json:"token"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Error        string `json:"error,omitempty"`
}

// RefreshCodeGenToken 使用 refresh_token 刷新 CodeGen SSO token。
// 返回新的 token、过期时间和（可选的）新 refresh_token。
func RefreshCodeGenToken(refreshToken string) (*TokenResult, error) {
	if refreshToken == "" {
		return nil, fmt.Errorf("codegen refresh: refresh_token 为空")
	}

	// 构造请求体
	reqBody, err := json.Marshal(codeGenRefreshRequest{RefreshToken: refreshToken})
	if err != nil {
		return nil, fmt.Errorf("codegen refresh: 序列化请求失败: %w", err)
	}

	// POST /api/v1/auth/refresh
	endpoint := CodeGenBaseURL + "/auth/refresh"
	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("codegen refresh: 创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := codeGenHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("codegen refresh: 请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, fmt.Errorf("codegen refresh: 读取响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("codegen refresh: HTTP %d: %s", resp.StatusCode, truncateBody(body, 512))
	}

	var refreshResp codeGenRefreshResponse
	if err := json.Unmarshal(body, &refreshResp); err != nil {
		return nil, fmt.Errorf("codegen refresh: 解析响应失败: %w", err)
	}

	if refreshResp.Error != "" {
		return nil, fmt.Errorf("codegen refresh: %s", refreshResp.Error)
	}

	if refreshResp.Token == "" {
		return nil, fmt.Errorf("codegen refresh: 响应中缺少 token")
	}

	return &TokenResult{
		AccessToken:  refreshResp.Token,
		RefreshToken: refreshResp.RefreshToken,
		ExpiresIn:    refreshResp.ExpiresIn,
	}, nil
}

// RunCodeGenSSOFlow 执行 CodeGen SSO 扫码登录流程：
//  1. 打开浏览器访问扫码登录页面
//  2. 轮询页面等待用户扫码完成
//  3. 获取 token 后调用 /api/v1/models 获取模型列表
//  4. 返回 token、模型信息和用户 email
func RunCodeGenSSOFlow() (CodeGenSSOResult, error) {
	// 1. 打开浏览器访问扫码登录页
	if err := browser.OpenURL(CodeGenSSOLoginURL); err != nil {
		return CodeGenSSOResult{}, fmt.Errorf("codegen sso: 无法打开浏览器: %w", err)
	}

	// 2. 轮询等待用户扫码（该页面会在用户扫码后返回 token）
	// 注意：这里假设扫码页面会通过某种方式（如 WebSocket 或轮询端点）返回 token
	// 实际实现需要根据 CodeGen SSO 服务的具体机制调整
	token, email, err := waitForCodeGenToken(CodeGenTimeout)
	if err != nil {
		return CodeGenSSOResult{}, fmt.Errorf("codegen sso: %w", err)
	}

	// 3. 用 token 获取模型列表
	models, baseURL, err := fetchCodeGenModels(token)
	if err != nil {
		return CodeGenSSOResult{}, fmt.Errorf("codegen sso: 获取模型列表失败: %w", err)
	}

	// 4. 选择默认模型（取第一个）
	defaultModel := ""
	contextLength := 0
	if len(models) > 0 {
		defaultModel = models[0].ID
		contextLength = models[0].ContextWindow
	}

	return CodeGenSSOResult{
		AccessToken:   token,
		BaseURL:       baseURL,
		ModelID:       defaultModel,
		ContextLength: contextLength,
		Email:         email,
	}, nil
}

// RunCodeGenSSOFlowWithCallback 执行 CodeGen SSO 扫码登录流程（回调模式）。
// 支持通过 context 取消。
//
// 流程：
//  1. 在本地启动临时 HTTP 服务器监听回调
//  2. 打开浏览器访问 SSO 登录页面，ref 参数指向本地回调地址
//  3. 用户在浏览器中扫码，SSO 完成后浏览器重定向到本地回调，携带 token
//  4. 本地服务器接收 token，关闭服务器
//  5. 用 token 获取模型列表
func RunCodeGenSSOFlowWithCallback(ctx context.Context) (CodeGenSSOResult, error) {
	// 1. 启动本地回调服务器
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return CodeGenSSOResult{}, fmt.Errorf("codegen sso: 启动回调服务器失败: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	callbackURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	type callbackResult struct {
		token string
		email string
	}
	resultCh := make(chan callbackResult, 1)

	mux := http.NewServeMux()
	// SSO 完成后浏览器重定向到 http://127.0.0.1:{port}/?{JWT_TOKEN}
	// token 是整个 query string（不是 key=value 格式）
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// 优先尝试 ?token=xxx 格式
		token := r.URL.Query().Get("token")
		if token == "" {
			// 回退：整个 RawQuery 就是 JWT token
			token = r.URL.RawQuery
		}
		if token != "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `<!DOCTYPE html><html><head><meta charset="utf-8"><title>登录成功</title></head><body style="font-family:sans-serif;text-align:center;padding:60px"><h2>✅ 登录成功</h2><p>已自动返回应用，可以关闭此页面。</p><script>setTimeout(function(){window.close()},2000)</script></body></html>`)
			resultCh <- callbackResult{token: token, email: r.URL.Query().Get("email")}
		} else {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `<!DOCTYPE html><html><head><meta charset="utf-8"><title>登录失败</title></head><body style="font-family:sans-serif;text-align:center;padding:60px"><h2>❌ 登录失败</h2><p>未收到 token，请重试。</p></body></html>`)
		}
	})

	server := &http.Server{Handler: mux}
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("[CodeGen SSO] 回调服务器错误: %v", err)
		}
	}()
	defer server.Close()

	// 2. 打开浏览器访问 SSO 登录页面
	loginURL := CodeGenSSOLoginBaseURL + "?ref=" + url.QueryEscape(callbackURL)
	log.Printf("[CodeGen SSO] 打开浏览器: %s", loginURL)
	if err := browser.OpenURL(loginURL); err != nil {
		return CodeGenSSOResult{}, fmt.Errorf("codegen sso: 无法打开浏览器: %w", err)
	}

	// 3. 等待回调或超时
	var token, email string
	select {
	case result := <-resultCh:
		token = result.token
		log.Printf("[CodeGen SSO] 收到回调 token (len=%d)", len(token))

		// 优先从 JWT payload 提取 email
		jwtEmail, jwtErr := ExtractEmailFromJWT(token)
		if jwtErr == nil {
			email = jwtEmail
			log.Printf("[CodeGen SSO] 从 JWT 提取到 email=%q", email)
		} else {
			log.Printf("[CodeGen SSO] JWT 提取 email 失败: %v，回退到回调参数", jwtErr)
			email = result.email
		}
	case <-ctx.Done():
		return CodeGenSSOResult{}, context.Canceled
	case <-time.After(CodeGenTimeout):
		return CodeGenSSOResult{}, fmt.Errorf("codegen sso: 扫码登录超时（%v）", CodeGenTimeout)
	}

	// 4. 用 token 获取模型列表
	models, baseURL, err := fetchCodeGenModels(token)
	if err != nil {
		return CodeGenSSOResult{}, fmt.Errorf("codegen sso: 获取模型列表失败: %w", err)
	}

	defaultModel := ""
	contextLength := 0
	if len(models) > 0 {
		defaultModel = models[0].ID
		contextLength = models[0].ContextWindow
	}

	return CodeGenSSOResult{
		AccessToken:   token,
		BaseURL:       baseURL,
		ModelID:       defaultModel,
		ContextLength: contextLength,
		Email:         email,
	}, nil
}

// waitForCodeGenToken 轮询等待用户在浏览器中扫码完成。
// 返回 access_token 和用户 email。
// 内部委托给 waitForCodeGenTokenWithContext，使用 context.Background() 和共享 HTTP 客户端。
func waitForCodeGenToken(timeout time.Duration) (token string, email string, err error) {
	return waitForCodeGenTokenWithContext(context.Background(), timeout, codeGenHTTPClient)
}

// waitForCodeGenTokenWithContext 是 waitForCodeGenToken 的 context 感知版本。
// 支持通过 context 取消轮询。当 ctx 被取消时返回 context.Canceled 错误。
//
// pollClient 是用于轮询的 HTTP 客户端。内嵌扫码场景下必须使用
// ExtractSSOQRCodeURL 返回的带 cookie jar 的客户端，以维持 SSO 会话状态。
// 浏览器扫码场景下使用共享的 codeGenHTTPClient。
func waitForCodeGenTokenWithContext(ctx context.Context, timeout time.Duration, pollClient *http.Client) (token string, email string, err error) {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(CodeGenPollInterval)
	defer ticker.Stop()

	// 创建不跟踪重定向的客户端。
	// get-token 端点有两种重定向行为：
	//   1. 扫码前：302 → zerotrust SSO 页面（继续等待）
	//   2. 扫码后：302 → /get-token/page?token=xxx（提取 token）
	// 不跟踪重定向可以从 Location 头区分这两种情况。
	noRedirectClient := &http.Client{
		Timeout: pollClient.Timeout,
		Jar:     pollClient.Jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	for {
		if time.Now().After(deadline) {
			return "", "", fmt.Errorf("扫码登录超时（%v）", timeout)
		}

		req, err := http.NewRequestWithContext(ctx, "GET", CodeGenSSOLoginURL, nil)
		if err != nil {
			return "", "", fmt.Errorf("创建请求失败: %w", err)
		}

		resp, err := noRedirectClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return "", "", context.Canceled
			}
			select {
			case <-ticker.C:
				continue
			case <-ctx.Done():
				return "", "", context.Canceled
			}
		}

		body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		resp.Body.Close()

		// 处理重定向响应
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			location := resp.Header.Get("Location")
			if location != "" {
				locURL, parseErr := url.Parse(location)
				if parseErr == nil {
					// 检查重定向 URL 中是否有 token 参数（扫码成功）
					if t := locURL.Query().Get("token"); t != "" {
						log.Printf("[CodeGen SSO Poll] 从重定向 URL 提取到 token (len=%d)", len(t))
						return t, locURL.Query().Get("email"), nil
					}
					// 重定向到 codegen 域名但没有 token（可能是中间跳转）
					if locURL.Hostname() == "codegen.qianxin-inc.cn" {
						log.Printf("[CodeGen SSO Poll] 重定向回 codegen: %s", location)
					}
				}
			}
			// 重定向到 zerotrust 或其他（扫码尚未完成），继续等待
			select {
			case <-ticker.C:
				continue
			case <-ctx.Done():
				return "", "", context.Canceled
			}
		}

		if err != nil {
			select {
			case <-ticker.C:
				continue
			case <-ctx.Done():
				return "", "", context.Canceled
			}
		}

		// 200 响应：尝试解析 JSON（兼容直接返回 JSON 的情况）
		var scanResp codeGenScanResponse
		if err := json.Unmarshal(body, &scanResp); err != nil {
			// 非 JSON（HTML 等），继续重试
			select {
			case <-ticker.C:
				continue
			case <-ctx.Done():
				return "", "", context.Canceled
			}
		}

		switch scanResp.Status {
		case "success":
			if scanResp.Token == "" {
				return "", "", fmt.Errorf("扫码成功但未返回 token")
			}
			return scanResp.Token, scanResp.Email, nil
		case "expired":
			return "", "", fmt.Errorf("二维码已过期，请重试")
		case "pending", "":
			select {
			case <-ticker.C:
				continue
			case <-ctx.Done():
				return "", "", context.Canceled
			}
		default:
			if scanResp.Error != "" {
				return "", "", fmt.Errorf("扫码失败: %s", scanResp.Error)
			}
			select {
			case <-ticker.C:
				continue
			case <-ctx.Done():
				return "", "", context.Canceled
			}
		}
	}
}

// ewmImgRegexp 匹配含 class="ewm-img" 的 <img> 标签并捕获 src 属性值。
// 使用两个备选分支支持 class 和 src 属性的任意顺序。
var ewmImgRegexp = regexp.MustCompile(
	`(?i)<img\s[^>]*class\s*=\s*["']ewm-img["'][^>]*src\s*=\s*["']([^"']+)["'][^>]*/?>` +
		`|` +
		`(?i)<img\s[^>]*src\s*=\s*["']([^"']+)["'][^>]*class\s*=\s*["']ewm-img["'][^>]*/?>`,
)
// extractQRCodeFromHTML 从 HTML 中提取 class="ewm-img" 的 img 标签的 src 属性，
// 解析其中 /qrcode.php?v=... 的 v 查询参数，URL decode 后返回二维码内容 URL。
func extractQRCodeFromHTML(html string) (string, error) {
	matches := ewmImgRegexp.FindStringSubmatch(html)
	if matches == nil {
		return "", fmt.Errorf("extractQRCode: 未找到 class=\"ewm-img\" 的 img 标签")
	}

	// src 值在 group 1（class 在前）或 group 2（src 在前）
	src := matches[1]
	if src == "" {
		src = matches[2]
	}

	// 解析 src 中的查询参数，提取 v 的值。
	// src 可能是相对路径如 /qrcode.php?v=..., 需要补全为绝对 URL 以便 url.Parse 正确解析。
	parsedURL, err := url.Parse(src)
	if err != nil {
		return "", fmt.Errorf("extractQRCode: 解析 src URL 失败: %w", err)
	}

	// url.Parse().Query().Get() 已经对查询参数值做了 URL decode，
	// 无需再调用 url.QueryUnescape()（否则会导致双重解码，
	// 例如原始 URL 中的 %25 会被错误地解码为 %）。
	v := parsedURL.Query().Get("v")
	if v == "" {
		return "", fmt.Errorf("extractQRCode: src 中缺少 v 查询参数")
	}

	return v, nil
}

// isDomainAllowed 检查给定域名是否在 AllowedSSODomains 白名单中。
func isDomainAllowed(host string) bool {
	for _, d := range AllowedSSODomains {
		if host == d {
			return true
		}
	}
	return false
}

// ExtractSSOQRCodeURL 请求 CodeGen SSO 登录页面，跟踪 302 重定向到 zerotrust SSO 页面，
// 解析 HTML 提取 <img class="ewm-img" src="/qrcode.php?v=..."> 的 v 参数值，
// URL decode 后返回二维码内容 URL。
//
// 同时返回带 cookie jar 的 *http.Client 和 zerotrust 页面的最终 URL。
// 后续轮询必须使用该 client 以维持 SSO 会话状态。
// ssoPageURL 是 zerotrust SSO 页面的完整 URL（含 OIDC 参数），
// 用于后续轮询扫码状态。
func ExtractSSOQRCodeURL() (qrURL string, sessionClient *http.Client, ssoPageURL string, err error) {
	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Timeout: 30 * time.Second,
		Jar:     jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			host := req.URL.Hostname()
			if !isDomainAllowed(host) {
				return fmt.Errorf("redirect to disallowed domain: %s", host)
			}
			return nil
		},
	}

	resp, err := client.Get(CodeGenSSOLoginURL)
	if err != nil {
		return "", nil, "", fmt.Errorf("ExtractSSOQRCodeURL: 请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 记录最终 URL（跟踪重定向后的 zerotrust SSO 页面地址）
	finalURL := resp.Request.URL.String()

	if resp.StatusCode != http.StatusOK {
		return "", nil, "", fmt.Errorf("ExtractSSOQRCodeURL: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", nil, "", fmt.Errorf("ExtractSSOQRCodeURL: 读取响应失败: %w", err)
	}

	qrURL, err = extractQRCodeFromHTML(string(body))
	if err != nil {
		return "", nil, "", err
	}

	return qrURL, client, finalURL, nil
}

// fetchCodeGenModels 使用 access_token 调用 /api/v1/models 获取模型列表。
func fetchCodeGenModels(token string) (models []struct{ ID string; ContextWindow int }, baseURL string, err error) {
	req, err := http.NewRequest("GET", CodeGenModelsEndpoint, nil)
	if err != nil {
		return nil, "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := codeGenHTTPClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return nil, "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncateBody(body, 512))
	}

	var modelsResp codeGenModelsResponse
	if err := json.Unmarshal(body, &modelsResp); err != nil {
		return nil, "", fmt.Errorf("parse response: %w", err)
	}

	if modelsResp.Error != "" {
		return nil, "", fmt.Errorf("api error: %s", modelsResp.Error)
	}

	// 转换为简化结构
	modelList := modelsResp.GetModels()
	result := make([]struct{ ID string; ContextWindow int }, len(modelList))
	for i, m := range modelList {
		result[i].ID = m.ID
		result[i].ContextWindow = m.ContextWindow
	}

	// 使用响应中的 base_url，如果没有则使用默认值
	if modelsResp.BaseURL != "" {
		baseURL = modelsResp.BaseURL
	} else {
		baseURL = CodeGenBaseURL
	}

	return result, baseURL, nil
}

// RunCodeGenSSOFlowWithPKCE 启动 CodeGen SSO 的 PKCE 授权流程，并返回解析后的结果。
func RunCodeGenSSOFlowWithPKCE(cfg Config) (CodeGenSSOResult, error) {
	tokenResult, err := RunOAuthFlow(cfg)
	if err != nil {
		return CodeGenSSOResult{}, err
	}

	// 获取模型列表以确定默认 modelID 和 baseURL
	models, baseURL, err := fetchCodeGenModels(tokenResult.AccessToken)
	if err != nil {
		// 如果获取模型失败，仍返回 token，但使用默认值
		return CodeGenSSOResult{
			AccessToken: tokenResult.AccessToken,
			BaseURL:     CodeGenBaseURL,
		}, nil
	}

	var modelID string
	var contextLength int
	if len(models) > 0 {
		// 默认使用第一个模型
		modelID = models[0].ID
		contextLength = models[0].ContextWindow
	}

	return CodeGenSSOResult{
		AccessToken:   tokenResult.AccessToken,
		BaseURL:       baseURL,
		ModelID:       modelID,
		ContextLength: contextLength,
	}, nil
}
