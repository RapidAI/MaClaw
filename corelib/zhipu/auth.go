// Package zhipu implements the Zhipu (智谱 / BigModel) online login used by the
// ZCode CLI ("zcode login bigmodel"). The flow is server-managed: the client
// creates a login flow on a ZCode endpoint, the user authorizes at
// bigmodel.cn, and the client polls the same endpoint until the approval
// lands. The resulting OAuth access token is exchanged for a normal
// coding-plan API key ("{id}.{secret}") through the BigModel business
// endpoints, so the saved credential works on the open.bigmodel.cn
// Anthropic-compatible endpoint exactly like a manually created key.
package zhipu

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/oauth"
)

// Login endpoints, protocol-identical to the open-source ZCode CLI
// (apps/zcode-cli packages/adapters/src/auth/cli-oauth.ts). The 国内 origin is
// tried first; the 国际 origin is the fallback for networks that cannot reach
// the 国内 deployment. A flow must always be polled on the origin that created
// it, so Login remembers which one answered.
var endpointOrigins = []string{
	"https://zcode.chatglm.site",
	"https://zcode.z.ai",
}

// bigModelOrigin hosts the business endpoints that turn the OAuth access token
// into a coding-plan API key. A var so tests can point it at a local server.
var bigModelOrigin = "https://bigmodel.cn"

const (
	apiBasePath     = "/api/v1"
	OAuthProviderID = "bigmodel"

	// BigModel business endpoints that turn the OAuth access token into a
	// normal coding-plan API key.
	customerInfoPath   = "/api/biz/customer/getCustomerInfo"
	apiKeyListFmt      = "/api/biz/v1/organization/%s/projects/%s/api_keys"
	codingPlanKeyName  = "zcode-api-key"
	defaultOrgName     = "默认机构"
	defaultProjectName = "默认项目"

	// pollTokenBytes matches the CLI's 32-byte hex poll token; the endpoint
	// rejects other lengths with a business error.
	pollTokenBytes = 32

	// maxPollLifetime bounds the whole approval window; the server deadline
	// (expires_at) is usually the tighter limit.
	maxPollLifetime = 5 * time.Minute
	minPollInterval = time.Second
	// pollIntervalFallback covers servers that omit poll_interval_sec.
	pollIntervalFallback = 2 * time.Second

	// initAttemptTimeout bounds one /oauth/cli/init attempt so the fallback
	// origin gets its turn quickly when the first origin is black-holed.
	initAttemptTimeout = 8 * time.Second
)

// UserAgent identifies MaClaw to the login and business endpoints.
const UserAgent = "MaClaw-ZCodeLogin/1.0"

// Login is one pending browser approval created on a ZCode endpoint.
type Login struct {
	origin       string // endpoint origin that owns this flow
	pollToken    string
	flowID       string
	authorizeURL string
	expiresAt    time.Time
	pollInterval time.Duration
}

// AuthURL is the 智谱在线登录 page the user must open in a browser.
func (l *Login) AuthURL() string {
	if l == nil {
		return ""
	}
	return l.authorizeURL
}

// Lifetime is how long the approval window still has, bounded to a sane range
// so a bogus server deadline cannot stall or truncate the login.
func (l *Login) Lifetime() time.Duration {
	if l == nil || l.expiresAt.IsZero() {
		return maxPollLifetime
	}
	remaining := time.Until(l.expiresAt)
	if remaining <= 0 {
		return minPollInterval
	}
	if remaining > maxPollLifetime {
		return maxPollLifetime
	}
	return remaining
}

// IsAuthorizeURL reports whether raw is a 智谱 login page we are willing to
// open: HTTPS on the BigModel or ZCode endpoint origins.
func IsAuthorizeURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https":
		switch strings.ToLower(parsed.Hostname()) {
		case "bigmodel.cn", "www.bigmodel.cn", "zcode.chatglm.site", "zcode.z.ai":
			return true
		default:
			return false
		}
	case "http":
		// Loopback HTTP stays open for local test servers, like Qoder's
		// IsApprovalURL.
		switch strings.ToLower(parsed.Hostname()) {
		case "localhost", "127.0.0.1", "::1":
			return true
		default:
			return false
		}
	default:
		return false
	}
}

// StartLogin creates the login flow via POST /oauth/cli/init, trying each
// endpoint origin in order until one answers. A business rejection fails
// immediately: it would fail on every origin alike. Each attempt is bounded
// by initAttemptTimeout so a black-holed 国内 origin cannot stall the login
// for the full HTTP client timeout before the fallback gets its turn.
func StartLogin(ctx context.Context) (*Login, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	pollToken, err := newPollToken()
	if err != nil {
		return nil, fmt.Errorf("智谱登录初始化失败: %w", err)
	}
	var lastErr error
	for _, origin := range endpointOrigins {
		attemptCtx, cancel := context.WithTimeout(ctx, initAttemptTimeout)
		login, err := initFlowAt(attemptCtx, origin, pollToken)
		cancel()
		if err == nil {
			return login, nil
		}
		lastErr = err
		var bizErr *businessError
		if errors.As(err, &bizErr) {
			return nil, err
		}
		if ctx.Err() != nil {
			break
		}
	}
	if lastErr == nil {
		lastErr = errors.New("登录端点不可用")
	}
	return nil, fmt.Errorf("智谱登录初始化失败: %w", lastErr)
}

func initFlowAt(ctx context.Context, origin, pollToken string) (*Login, error) {
	base := strings.TrimRight(origin, "/") + apiBasePath
	raw, err := postEnvelope(ctx, base+"/oauth/cli/init", map[string]string{"provider": OAuthProviderID}, "Bearer "+pollToken)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, errors.New("智谱登录响应数据为空")
	}
	var data struct {
		FlowID          string  `json:"flow_id"`
		AuthorizeURL    string  `json:"authorize_url"`
		ExpiresAt       unixSec `json:"expires_at"`
		PollIntervalSec float64 `json:"poll_interval_sec"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("智谱登录响应无效: %w", err)
	}
	if strings.TrimSpace(data.FlowID) == "" || !IsAuthorizeURL(data.AuthorizeURL) {
		return nil, errors.New("智谱登录响应缺少有效的授权地址")
	}
	interval := time.Duration(data.PollIntervalSec * float64(time.Second))
	if interval < minPollInterval {
		interval = pollIntervalFallback
	}
	login := &Login{
		origin:       strings.TrimRight(origin, "/"),
		pollToken:    pollToken,
		flowID:       strings.TrimSpace(data.FlowID),
		authorizeURL: strings.TrimSpace(data.AuthorizeURL),
		pollInterval: interval,
	}
	if data.ExpiresAt > 0 {
		login.expiresAt = time.Unix(int64(data.ExpiresAt), 0)
	}
	return login, nil
}

// Token is the credential set returned by a completed approval. The access
// token authorizes the BigModel business endpoints; the coding-plan API key
// resolved from it is what provider requests actually use.
type Token struct {
	AccessToken  string
	RefreshToken string
	JWT          string
	UserID       string
	UserName     string
	Email        string
}

type pollResponse struct {
	Status   string `json:"status"`
	Token    string `json:"token"`
	BigModel struct {
		AccessToken     string `json:"access_token"`
		AccessTokenAlt  string `json:"accessToken"`
		RefreshToken    string `json:"refresh_token"`
		RefreshTokenAlt string `json:"refreshToken"`
	} `json:"bigmodel"`
	User struct {
		UserID string `json:"user_id"`
		Name   string `json:"name"`
		Email  string `json:"email"`
		Avatar string `json:"avatar"`
	} `json:"user"`
}

func (r pollResponse) accessToken() string {
	if strings.TrimSpace(r.BigModel.AccessToken) != "" {
		return strings.TrimSpace(r.BigModel.AccessToken)
	}
	return strings.TrimSpace(r.BigModel.AccessTokenAlt)
}

func (r pollResponse) refreshToken() string {
	if strings.TrimSpace(r.BigModel.RefreshToken) != "" {
		return strings.TrimSpace(r.BigModel.RefreshToken)
	}
	return strings.TrimSpace(r.BigModel.RefreshTokenAlt)
}

// PollUntilReady polls the flow's origin until the approval lands, the server
// reports failure, or the window closes. Pending responses and transport blips
// keep polling so an opened approval page is never wasted.
func PollUntilReady(ctx context.Context, login *Login) (Token, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if login == nil || login.flowID == "" || login.pollToken == "" {
		return Token{}, errors.New("没有正在进行的智谱登录")
	}
	deadline := time.Now().Add(login.Lifetime())
	endpoint := login.origin + apiBasePath + "/oauth/cli/poll/" + url.PathEscape(login.flowID)
	for {
		if time.Now().After(deadline) {
			return Token{}, errors.New("智谱登录已超时，请重新登录")
		}
		token, retryable, err := pollOnce(ctx, endpoint, login.pollToken)
		if err == nil {
			return token, nil
		}
		if !retryable {
			return Token{}, err
		}
		if err := sleepWithContext(ctx, login.pollInterval); err != nil {
			return Token{}, err
		}
	}
}

// pollOnce runs one poll round. retryable marks transient outcomes (pending
// approval, network blips, throttling) that should not end the login.
func pollOnce(ctx context.Context, endpoint, pollToken string) (token Token, retryable bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Token{}, false, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+pollToken)
	req.Header.Set("User-Agent", UserAgent)
	resp, err := oauth.Do(req)
	if err != nil {
		// Transport blips (network, proxy) stay retryable until the deadline.
		return Token{}, true, fmt.Errorf("轮询智谱登录失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return Token{}, true, fmt.Errorf("读取智谱登录响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		// Match the CLI's poll tolerance exactly: only 408/429/5xx keep the
		// login alive. Anything else (404 flow-not-found above all) is a
		// definitive failure — retrying a missing flow would just burn the
		// whole approval window.
		return Token{}, isTransientStatus(resp.StatusCode), fmt.Errorf("轮询智谱登录失败 (HTTP %d): %s", resp.StatusCode, truncateForError(body))
	}
	var envelope struct {
		Code int             `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return Token{}, false, fmt.Errorf("智谱登录响应无法解析: %w", err)
	}
	if envelope.Code != 0 {
		return Token{}, false, &businessError{msg: strings.TrimSpace(envelope.Msg), code: envelope.Code}
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return Token{}, true, errors.New("智谱登录响应数据为空")
	}
	var payload pollResponse
	if err := json.Unmarshal(envelope.Data, &payload); err != nil {
		return Token{}, false, fmt.Errorf("智谱登录数据无法解析: %w", err)
	}
	switch payload.Status {
	case "pending":
		return Token{}, true, errPendingApproval
	case "failed":
		return Token{}, false, errors.New("智谱授权失败，请重新登录")
	case "ready":
	default:
		return Token{}, false, fmt.Errorf("智谱登录返回未知状态 %q", payload.Status)
	}
	token = Token{
		AccessToken:  payload.accessToken(),
		RefreshToken: payload.refreshToken(),
		JWT:          strings.TrimSpace(payload.Token),
		UserID:       strings.TrimSpace(payload.User.UserID),
		UserName:     strings.TrimSpace(payload.User.Name),
		Email:        strings.TrimSpace(payload.User.Email),
	}
	if token.AccessToken == "" || token.UserID == "" {
		return Token{}, false, errors.New("智谱登录响应缺少访问令牌")
	}
	return token, false, nil
}

// isTransientStatus marks the poll outcomes the CLI tolerates (408 request
// timeout, 429 throttling, and 5xx gateway blips) as keep-polling.
func isTransientStatus(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusTooManyRequests:
		return true
	default:
		return status >= 500
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Coding-plan API key resolution (BigModel business endpoints)
// ─────────────────────────────────────────────────────────────────────────────

type bizOrganization struct {
	OrganizationID   string       `json:"organizationId"`
	OrganizationName string       `json:"organizationName"`
	Projects         []bizProject `json:"projects"`
}

type bizProject struct {
	ProjectID   string `json:"projectId"`
	ProjectName string `json:"projectName"`
}

type bizAPIKey struct {
	APIKey string `json:"apiKey"`
	Name   string `json:"name"`
}

// ResolveCodingPlanAPIKey exchanges an OAuth access token for the account's
// coding-plan API key on bigmodel.cn, creating the "zcode-api-key" entry when
// it does not exist yet. The result is "{id}.{secret}" — the same key format
// the open platform hands out manually.
func ResolveCodingPlanAPIKey(ctx context.Context, accessToken string) (string, error) {
	accessToken = strings.TrimSpace(accessToken)
	if accessToken == "" {
		return "", errors.New("智谱访问令牌为空")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var customer struct {
		Organizations []bizOrganization `json:"organizations"`
	}
	if err := bizRequest(ctx, http.MethodGet, bigModelOrigin+customerInfoPath, nil, accessToken, &customer); err != nil {
		return "", fmt.Errorf("获取智谱账号信息失败: %w", err)
	}
	organizationID, projectID := pickOrgAndProject(customer.Organizations)
	if organizationID == "" || projectID == "" {
		return "", errors.New("未找到智谱组织或项目，请先在 bigmodel.cn 开通后重试")
	}
	listEndpoint := bigModelOrigin + fmt.Sprintf(apiKeyListFmt, url.PathEscape(organizationID), url.PathEscape(projectID))
	var keys []bizAPIKey
	if err := bizRequest(ctx, http.MethodGet, listEndpoint, nil, accessToken, &keys); err != nil {
		return "", fmt.Errorf("获取智谱 API Key 列表失败: %w", err)
	}
	apiKey := ""
	for _, key := range keys {
		if strings.TrimSpace(key.Name) == codingPlanKeyName && strings.TrimSpace(key.APIKey) != "" {
			apiKey = strings.TrimSpace(key.APIKey)
			break
		}
	}
	if apiKey == "" {
		var created bizAPIKey
		if err := bizRequest(ctx, http.MethodPost, listEndpoint, map[string]string{"name": codingPlanKeyName}, accessToken, &created); err != nil {
			return "", fmt.Errorf("创建智谱 API Key 失败: %w", err)
		}
		apiKey = strings.TrimSpace(created.APIKey)
	}
	if apiKey == "" {
		return "", errors.New("智谱 API Key 响应缺少密钥")
	}
	var secret struct {
		SecretKey string `json:"secretKey"`
	}
	if err := bizRequest(ctx, http.MethodGet, listEndpoint+"/copy/"+url.PathEscape(apiKey), nil, accessToken, &secret); err != nil {
		return "", fmt.Errorf("读取智谱 API Key 密钥失败: %w", err)
	}
	secretKey := strings.TrimSpace(secret.SecretKey)
	if secretKey == "" {
		return apiKey, nil
	}
	return apiKey + "." + secretKey, nil
}

// pickOrgAndProject prefers the 默认机构/默认项目 entries and falls back to the
// first listing, mirroring the CLI's selection.
func pickOrgAndProject(orgs []bizOrganization) (string, string) {
	if len(orgs) == 0 {
		return "", ""
	}
	chosen := orgs[0]
	for _, org := range orgs {
		if strings.Contains(org.OrganizationName, defaultOrgName) {
			chosen = org
			break
		}
	}
	if strings.TrimSpace(chosen.OrganizationID) == "" {
		return "", ""
	}
	projectID := ""
	for _, project := range chosen.Projects {
		if strings.Contains(project.ProjectName, defaultProjectName) {
			projectID = project.ProjectID
			break
		}
	}
	if projectID == "" && len(chosen.Projects) > 0 {
		projectID = chosen.Projects[0].ProjectID
	}
	return strings.TrimSpace(chosen.OrganizationID), strings.TrimSpace(projectID)
}

// ─────────────────────────────────────────────────────────────────────────────
// HTTP plumbing (proxy-aware via the shared OAuth client)
// ─────────────────────────────────────────────────────────────────────────────

// errPendingApproval marks a poll round where the user has not finished the
// browser approval yet; the poll loop treats it as keep-going.
var errPendingApproval = errors.New("智谱授权等待中")

type businessError struct {
	msg  string
	code int
}

func (e *businessError) Error() string {
	if e.msg != "" {
		return fmt.Sprintf("智谱服务错误 (%d): %s", e.code, e.msg)
	}
	return fmt.Sprintf("智谱服务错误 (%d)", e.code)
}

// unixSec accepts a JSON number or numeric string for unix-seconds deadlines.
type unixSec int64

func (t *unixSec) UnmarshalJSON(raw []byte) error {
	text := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	if text == "" || text == "null" {
		*t = 0
		return nil
	}
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		*t = 0
		return nil
	}
	*t = unixSec(value)
	return nil
}

// postEnvelope posts a JSON payload and returns the success envelope's data.
func postEnvelope(ctx context.Context, endpoint string, payload any, authorization string) (json.RawMessage, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	req.Header.Set("User-Agent", UserAgent)
	resp, err := oauth.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求智谱登录服务失败: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, fmt.Errorf("读取智谱登录响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("请求智谱登录服务失败 (HTTP %d): %s", resp.StatusCode, truncateForError(raw))
	}
	return decodeEnvelope(raw)
}

func decodeEnvelope(raw []byte) (json.RawMessage, error) {
	var envelope struct {
		Code json.RawMessage `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("智谱响应无法解析: %w", err)
	}
	if !isSuccessfulBizCode(envelope.Code) {
		msg := strings.TrimSpace(envelope.Msg)
		if msg == "" {
			msg = fmt.Sprintf("业务错误 %s", string(envelope.Code))
		}
		code := 0
		_ = json.Unmarshal(envelope.Code, &code)
		return nil, &businessError{msg: msg, code: code}
	}
	return envelope.Data, nil
}

// isSuccessfulBizCode mirrors the CLI's tolerant success check: the BigModel
// endpoints answer 0, 200, their string forms, or omit the code entirely.
func isSuccessfulBizCode(raw json.RawMessage) bool {
	text := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	switch text {
	case "", "null", "0", "200":
		return true
	default:
		return false
	}
}

// bizRequest calls one BigModel business endpoint, whose Authorization header
// carries the raw OAuth access token (no Bearer prefix), and unwraps the
// envelope into target.
func bizRequest(ctx context.Context, method, endpoint string, payload any, accessToken string, target any) error {
	var reader io.Reader
	if payload != nil {
		body, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		reader = strings.NewReader(string(body))
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", accessToken)
	req.Header.Set("User-Agent", UserAgent)
	resp, err := oauth.Do(req)
	if err != nil {
		return fmt.Errorf("请求智谱开放平台失败: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return fmt.Errorf("读取智谱开放平台响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("请求智谱开放平台失败 (HTTP %d): %s", resp.StatusCode, truncateForError(raw))
	}
	data, err := decodeEnvelope(raw)
	if err != nil {
		return err
	}
	if target == nil || len(data) == 0 || string(data) == "null" {
		return nil
	}
	return json.Unmarshal(data, target)
}

func newPollToken() (string, error) {
	raw := make([]byte, pollTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

// sleepWithContext waits for d or until ctx ends, so Cancel aborts the wait.
func sleepWithContext(ctx context.Context, d time.Duration) error {
	if ctx == nil {
		ctx = context.Background()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// truncateForError keeps error messages bounded without splitting a UTF-8
// rune: bigmodel answers carry Chinese text.
func truncateForError(body []byte) string {
	text := strings.TrimSpace(string(body))
	runes := []rune(text)
	if len(runes) > 100 {
		return string(runes[:100])
	}
	return text
}
