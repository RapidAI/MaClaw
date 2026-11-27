package oauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
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

// GitHubRepository is one repository visible to the connected account.
type GitHubRepository struct {
	FullName      string `json:"full_name"`
	Description   string `json:"description"`
	Private       bool   `json:"private"`
	HTMLURL       string `json:"html_url"`
	DefaultBranch string `json:"default_branch"`
	UpdatedAt     string `json:"updated_at"`
}

// FetchGitHubRepositories lists the repositories the token can use, ordered
// by recent activity. The GitHub API caps a page at 100 rows.
func FetchGitHubRepositories(ctx context.Context, token string, limit int) ([]GitHubRepository, error) {
	return fetchGitHubRepositories(ctx, GitHubAPIBase, token, limit)
}

func fetchGitHubRepositories(ctx context.Context, apiBase, token string, limit int) ([]GitHubRepository, error) {
	if limit < 1 || limit > 100 {
		limit = 100
	}
	endpoint := strings.TrimRight(apiBase, "/") + "/user/repos?per_page=" + fmt.Sprint(limit) + "&sort=updated&affiliation=owner,collaborator,organization_member"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("github /user/repos: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "MaClaw/1.0")

	resp, err := DoNoFollow(req)
	if err != nil {
		return nil, fmt.Errorf("github /user/repos: request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
	if err != nil {
		return nil, fmt.Errorf("github /user/repos: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, gitHubResponseError("github /user/repos", resp, body)
	}
	var rows []GitHubRepository
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("github /user/repos: parse response: %w", err)
	}
	out := rows[:0]
	for _, row := range rows {
		if strings.TrimSpace(row.FullName) == "" {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

// gitHubHTTPError keeps GitHub's own explanation. A bare status/body length
// hides the difference between a rate limit, a revoked grant, and a missing
// permission, which made the repository dialog look randomly broken.
func gitHubHTTPError(endpoint string, status int, body []byte) error {
	var payload struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &payload) == nil {
		if msg := strings.TrimSpace(payload.Message); msg != "" {
			return fmt.Errorf("%s: HTTP %d: %s", endpoint, status, msg)
		}
	}
	return fmt.Errorf("%s: HTTP %d", endpoint, status)
}

// GitHubRateLimitError is an exhausted GitHub API quota (HTTP 403/429 with
// X-RateLimit-Remaining: 0). Callers use errors.As to tell "wait for the
// reset" apart from a revoked token or a missing permission, and to show the
// reset time instead of GitHub's raw scraping-policy paragraph.
type GitHubRateLimitError struct {
	Endpoint string
	Message  string
	// ResetAt is when the hourly window rolls over, from X-RateLimit-Reset
	// (unix seconds). Zero when GitHub did not answer with the header.
	ResetAt time.Time
}

func (e *GitHubRateLimitError) Error() string {
	msg := e.Message
	if msg == "" {
		msg = "API rate limit exceeded"
	}
	base := fmt.Sprintf("%s: quota exhausted: %s", e.Endpoint, msg)
	if e.ResetAt.IsZero() {
		return base
	}
	return base + fmt.Sprintf(" (resets at %s)", e.ResetAt.Format("15:04"))
}

// gitHubRateLimitError converts a rate-limited response into a
// GitHubRateLimitError. remaining==0 plus the 403/429 status is the primary
// rate limit signature; the body alone ("rate limit exceeded") also appears
// for secondary limits without the header, so the status check stays the
// authority.
func gitHubRateLimitError(endpoint string, resp *http.Response, body []byte) *GitHubRateLimitError {
	err := &GitHubRateLimitError{Endpoint: endpoint}
	if resp != nil {
		if v := strings.TrimSpace(resp.Header.Get("X-RateLimit-Reset")); v != "" {
			if sec, parseErr := strconv.ParseInt(v, 10, 64); parseErr == nil && sec > 0 {
				err.ResetAt = time.Unix(sec, 0)
			}
		}
	}
	var payload struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &payload) == nil {
		err.Message = strings.TrimSpace(payload.Message)
	}
	return err
}

// gitHubResponseError maps a non-200 GitHub response: an exhausted quota
// becomes a typed GitHubRateLimitError, everything else keeps GitHub's own
// explanation.
func gitHubResponseError(endpoint string, resp *http.Response, body []byte) error {
	if resp != nil && (resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests) && strings.TrimSpace(resp.Header.Get("X-RateLimit-Remaining")) == "0" {
		return gitHubRateLimitError(endpoint, resp, body)
	}
	return gitHubHTTPError(endpoint, resp.StatusCode, body)
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
		return nil, "", gitHubResponseError("github /user", resp, body)
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

// ─────────────────────────────────────────────────────────────────────────────
// Repository creation — org listing, name-availability probing, and create.
// These are the write-capable counterparts of FetchGitHubRepositories, used by
// the cloud-agent / virtual-repository "create and bind" flows. The apiBase
// parameterization follows the fetchGitHubRepositories test pattern.
// ─────────────────────────────────────────────────────────────────────────────

// GitHubOrg is one organization the connected account belongs to
// (GET /user/orgs). Creating a repository inside one additionally requires
// the member's create permission; the create call surfaces the provider's
// refusal when it is missing.
type GitHubOrg struct {
	Login       string `json:"login"`
	Description string `json:"description"`
}

// FetchGitHubOrgs lists the organizations the token's account belongs to.
func FetchGitHubOrgs(ctx context.Context, token string, limit int) ([]GitHubOrg, error) {
	return fetchGitHubOrgs(ctx, GitHubAPIBase, token, limit)
}

func fetchGitHubOrgs(ctx context.Context, apiBase, token string, limit int) ([]GitHubOrg, error) {
	if limit < 1 || limit > 100 {
		limit = 100
	}
	endpoint := strings.TrimRight(apiBase, "/") + "/user/orgs?per_page=" + fmt.Sprint(limit)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("github /user/orgs: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "MaClaw/1.0")

	resp, err := DoNoFollow(req)
	if err != nil {
		return nil, fmt.Errorf("github /user/orgs: request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
	if err != nil {
		return nil, fmt.Errorf("github /user/orgs: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, gitHubResponseError("github /user/orgs", resp, body)
	}
	var rows []GitHubOrg
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("github /user/orgs: parse response: %w", err)
	}
	out := rows[:0]
	for _, row := range rows {
		if strings.TrimSpace(row.Login) == "" {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

// GitHubRepoNameShape reports whether one owner/name path segment is
// plausibly a GitHub repository path segment: letters, digits, dot,
// underscore, hyphen; 1-200 chars; not "." or "..". GitHub itself normalizes
// and validates names on create; this pre-check only catches obvious typos
// before a network round-trip. Callers outside the package (the guiapp
// create flows) reuse it so the client-side rule cannot drift from the
// probe's.
func GitHubRepoNameShape(segment string) bool {
	segment = strings.TrimSpace(segment)
	if segment == "" || len(segment) > 200 || segment == "." || segment == ".." {
		return false
	}
	for _, r := range segment {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
		default:
			return false
		}
	}
	return true
}

// GitHubRepoNameStatus probes whether owner/name can still be created:
// HTTP 200 means the repository already exists (taken), 404 means the path
// is free. Anything else — exhausted quota, revoked token — is an error, so
// a failed probe never reads as "available".
func GitHubRepoNameStatus(ctx context.Context, token, owner, name string) (available bool, reason string, err error) {
	return githubRepoNameStatus(ctx, GitHubAPIBase, token, owner, name)
}

func githubRepoNameStatus(ctx context.Context, apiBase, token, owner, name string) (bool, string, error) {
	owner = strings.TrimSpace(owner)
	name = strings.TrimSpace(name)
	if !GitHubRepoNameShape(owner) || !GitHubRepoNameShape(name) {
		return false, "归属或仓库名格式无效", nil
	}
	endpoint := strings.TrimRight(apiBase, "/") + "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false, "", fmt.Errorf("github /repos/%s/%s: create request: %w", owner, name, err)
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "MaClaw/1.0")

	resp, err := DoNoFollow(req)
	if err != nil {
		return false, "", fmt.Errorf("github /repos/%s/%s: request failed: %w", owner, name, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return false, "", fmt.Errorf("github /repos/%s/%s: read response: %w", owner, name, err)
	}
	switch resp.StatusCode {
	case http.StatusOK:
		return false, "该仓库已存在", nil
	case http.StatusNotFound:
		return true, "", nil
	default:
		return false, "", gitHubResponseError(fmt.Sprintf("github /repos/%s/%s", owner, name), resp, body)
	}
}

// GitHubCreatedRepo is the subset of the create-repository response the
// callers surface after a successful POST.
type GitHubCreatedRepo struct {
	FullName      string `json:"full_name"`
	HTMLURL       string `json:"html_url"`
	CloneURL      string `json:"clone_url"`
	DefaultBranch string `json:"default_branch"`
	Private       bool   `json:"private"`
}

// CreateGitHubRepo creates a repository under the token's own account
// (owner empty) or under an organization the account belongs to. autoInit
// seeds an initial README commit so the fresh repository has a real default
// branch for the first clone and push.
func CreateGitHubRepo(ctx context.Context, token, owner, name string, private, autoInit bool) (*GitHubCreatedRepo, error) {
	return createGitHubRepo(ctx, GitHubAPIBase, token, owner, name, private, autoInit)
}

func createGitHubRepo(ctx context.Context, apiBase, token, owner, name string, private, autoInit bool) (*GitHubCreatedRepo, error) {
	owner = strings.TrimSpace(owner)
	name = strings.TrimSpace(name)
	if name == "" || !GitHubRepoNameShape(name) {
		return nil, fmt.Errorf("github create repo: 仓库名格式无效")
	}
	if owner != "" && !GitHubRepoNameShape(owner) {
		return nil, fmt.Errorf("github create repo: 组织名格式无效")
	}
	endpoint := strings.TrimRight(apiBase, "/") + "/user/repos"
	if owner != "" {
		endpoint = strings.TrimRight(apiBase, "/") + "/orgs/" + url.PathEscape(owner) + "/repos"
	}
	payload, err := json.Marshal(map[string]any{
		"name":      name,
		"private":   private,
		"auto_init": autoInit,
	})
	if err != nil {
		return nil, fmt.Errorf("github create repo: encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("github create repo: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "MaClaw/1.0")

	resp, err := DoNoFollow(req)
	if err != nil {
		return nil, fmt.Errorf("github create repo: request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("github create repo: read response: %w", err)
	}
	if resp.StatusCode == http.StatusCreated {
		var created GitHubCreatedRepo
		if err := json.Unmarshal(body, &created); err != nil {
			return nil, fmt.Errorf("github create repo: parse response: %w", err)
		}
		if created.FullName == "" {
			created.FullName = name
		}
		return &created, nil
	}
	return nil, gitHubCreateRepoError(resp, body)
}

// gitHubCreateRepoError maps the create-specific failures to readable text:
// 422 name-already-exists and 403 organization permission are the two the
// create dialog must explain; everything else keeps GitHub's own message.
func gitHubCreateRepoError(resp *http.Response, body []byte) error {
	endpoint := "github create repo"
	if resp != nil && (resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests) && strings.TrimSpace(resp.Header.Get("X-RateLimit-Remaining")) == "0" {
		return gitHubRateLimitError(endpoint, resp, body)
	}
	var payload struct {
		Message string   `json:"message"`
		Errors  []string `json:"errors"`
	}
	message := ""
	if json.Unmarshal(body, &payload) == nil {
		message = strings.TrimSpace(payload.Message)
		for _, item := range payload.Errors {
			message += " " + strings.TrimSpace(item)
		}
	}
	if resp != nil && resp.StatusCode == http.StatusUnprocessableEntity && strings.Contains(strings.ToLower(message), "already exists") {
		return fmt.Errorf("%s: 仓库名已被占用", endpoint)
	}
	if message != "" {
		return fmt.Errorf("%s: HTTP %d: %s", endpoint, resp.StatusCode, message)
	}
	return fmt.Errorf("%s: HTTP %d", endpoint, resp.StatusCode)
}
