package guiapp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/oauth"

	"github.com/pkg/browser"
)

// ─────────────────────────────────────────────────────────────────────────────
// GitHub 连接 — Wails Bindings (Device Code Flow)
// 与 GitHub Copilot LLM provider 无关：这里拿到的是通用 GitHub OAuth token，
// 存入 credentials.json 的 "github" provider，供 skill 搜索、更新检测、
// 代码克隆等任何需要访问 GitHub 的功能复用。
// ─────────────────────────────────────────────────────────────────────────────

// GitHubConnectionStatus 是设置页 GitHub tab 展示的连接状态。
type GitHubConnectionStatus struct {
	Connected      bool   `json:"connected"`
	Login          string `json:"login,omitempty"`
	Name           string `json:"name,omitempty"`
	AvatarURL      string `json:"avatar_url,omitempty"`
	HTMLURL        string `json:"html_url,omitempty"`
	Scopes         string `json:"scopes,omitempty"`
	ClientID       string `json:"client_id"`
	CustomClientID bool   `json:"custom_client_id"`
}

// GitHubDeviceInfo 由 StartGitHubOAuth 返回给前端展示设备码。
type GitHubDeviceInfo struct {
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	BrowserOpened   bool   `json:"browser_opened"`
}

// githubLoginFlow 是进行中的 device-code 登录；generation 与
// beginOAuthFlow 的全局槽位配套，新登录会顶掉旧登录。
type githubLoginFlow struct {
	ctx        context.Context
	clientID   string
	deviceCode string
	interval   int
	generation uint64
	finish     func()
	claim      func(func() error) error
}

// effectiveGitHubClientID resolves the OAuth client_id: config override first,
// then the bundled default.
func (a *App) effectiveGitHubClientID() (string, bool) {
	if cfg, err := a.LoadConfig(); err == nil {
		if id := strings.TrimSpace(cfg.GitHubClientID); id != "" {
			return id, true
		}
	}
	return oauth.GitHubAccountClientID, false
}

// githubAccessToken returns the connected account's OAuth token, or "".
// Registered as the skill package's connected-account token provider at
// startup — ResolveGitHubToken runs once per searcher instance, so it must not
// hit the filesystem every time. The value is cached in memory and kept in
// step by the write paths below (login, disconnect, revoked-token cleanup).
func (a *App) githubAccessToken() string {
	a.githubTokenOnce.Do(func() { a.githubTokenCache.Store(a.loadGitHubAccessToken()) })
	token, _ := a.githubTokenCache.Load().(string)
	return token
}

// loadGitHubAccessToken reads the token straight from the credential store.
func (a *App) loadGitHubAccessToken() string {
	if a.credentialStore == nil {
		return ""
	}
	cred, err := a.credentialStore.Read(oauth.GitHubAccountProviderID)
	if err != nil || cred == nil {
		return ""
	}
	return strings.TrimSpace(cred.AccessToken)
}

// setGitHubTokenCache updates the in-memory mirror after any write to the
// GitHub credential record so hot-path readers never touch disk.
func (a *App) setGitHubTokenCache(token string) {
	a.githubTokenOnce.Do(func() { a.githubTokenCache.Store(token) })
	a.githubTokenCache.Store(token)
}

// githubConnectionStatus builds the status view from the stored credential.
func (a *App) githubConnectionStatus() GitHubConnectionStatus {
	clientID, custom := a.effectiveGitHubClientID()
	status := GitHubConnectionStatus{ClientID: clientID, CustomClientID: custom}
	if a.credentialStore == nil {
		return status
	}
	cred, err := a.credentialStore.Read(oauth.GitHubAccountProviderID)
	if err != nil || cred == nil || strings.TrimSpace(cred.AccessToken) == "" {
		return status
	}
	status.Connected = true
	status.Login = cred.Login
	status.Name = cred.DisplayName
	status.AvatarURL = cred.AvatarURL
	status.HTMLURL = cred.HTMLURL
	status.Scopes = cred.Scopes
	return status
}

// GetGitHubConnection returns the stored connection state. When refresh is
// true and a token exists, the account profile is re-fetched from GitHub so
// renamed users / revoked tokens surface in the UI.
func (a *App) GetGitHubConnection(refresh bool) GitHubConnectionStatus {
	status := a.githubConnectionStatus()
	if !refresh || !status.Connected {
		return status
	}
	refreshCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// Read the token straight from the store here — refresh is rare, and the
	// cached mirror could miss an externally updated credential, which would
	// make a healthy token look revoked and get it deleted below.
	user, scopes, err := oauth.FetchGitHubUser(refreshCtx, a.loadGitHubAccessToken())
	if err != nil {
		log.Printf("[github] connection refresh failed: %v", err)
		if strings.Contains(err.Error(), "invalid or revoked") {
			// Token is dead — drop it so the UI shows disconnected instead of
			// a phantom account.
			_ = a.credentialStore.Delete(oauth.GitHubAccountProviderID)
			a.setGitHubTokenCache("")
			return a.githubConnectionStatus()
		}
		return status // transient error: keep the stored snapshot
	}
	_ = a.credentialStore.Modify(oauth.GitHubAccountProviderID, func(old *oauth.StoredCredential) (*oauth.StoredCredential, error) {
		if old == nil {
			return nil, nil
		}
		old.Login = user.Login
		old.DisplayName = user.Name
		old.AvatarURL = user.AvatarURL
		old.HTMLURL = user.HTMLURL
		if scopes != "" {
			old.Scopes = scopes
		}
		return old, nil
	})
	return a.githubConnectionStatus()
}

// StartGitHubOAuth begins the device-code login. The frontend shows the
// returned user_code + verification_uri, then calls WaitGitHubOAuth.
func (a *App) StartGitHubOAuth() (GitHubDeviceInfo, error) {
	if cfg, err := a.LoadConfig(); err == nil {
		oauth.ApplyProxyFromAppConfig(cfg)
	}
	clientID, _ := a.effectiveGitHubClientID()

	// beginOAuthFlow cancels any in-flight OAuth flow (any provider) and owns
	// the global slot until finish/claim.
	ctx, finish, claim := a.beginOAuthFlow(oauth.GitHubAccountTimeout)
	a.oauthMu.Lock()
	ownedGen := a.oauthGeneration
	// Own the slot marker before the device request so Cancel can abort it —
	// matching the Kimi login's ordering.
	a.githubOwnedGen = ownedGen
	a.oauthMu.Unlock()

	deviceResp, err := oauth.RequestGitHubDeviceCodeWith(ctx, clientID, oauth.GitHubAccountScopes)
	if err != nil {
		finish()
		a.clearGitHubOwnedGen(ownedGen)
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return GitHubDeviceInfo{}, fmt.Errorf("GitHub 登录已取消或超时")
		}
		return GitHubDeviceInfo{}, fmt.Errorf("GitHub 设备码请求失败: %w", err)
	}

	flow := &githubLoginFlow{
		ctx:        ctx,
		clientID:   clientID,
		deviceCode: deviceResp.DeviceCode,
		interval:   deviceResp.Interval,
		generation: ownedGen,
		finish:     finish,
		claim:      claim,
	}
	a.oauthMu.Lock()
	if ctx.Err() != nil {
		a.oauthMu.Unlock()
		finish()
		a.clearGitHubOwnedGen(ownedGen)
		return GitHubDeviceInfo{}, fmt.Errorf("GitHub 登录已取消或超时")
	}
	previous := a.githubLogin
	a.githubLogin = flow
	a.oauthMu.Unlock()
	if previous != nil && previous.finish != nil {
		previous.finish()
	}
	if ctx.Err() != nil {
		// Cancelled between storing the flow and returning the device code.
		a.oauthMu.Lock()
		if a.githubLogin == flow {
			a.githubLogin = nil
		}
		a.oauthMu.Unlock()
		flow.finish()
		a.clearGitHubOwnedGen(ownedGen)
		return GitHubDeviceInfo{}, fmt.Errorf("GitHub 登录已取消或超时")
	}

	info := GitHubDeviceInfo{
		UserCode:        deviceResp.UserCode,
		VerificationURI: deviceResp.VerificationURI,
	}
	if err := browser.OpenURL(deviceResp.VerificationURI); err != nil {
		log.Printf("[github] browser open failed: %s", strings.TrimSpace(err.Error()))
	} else {
		info.BrowserOpened = true
	}
	return info, nil
}

// WaitGitHubOAuth blocks until the user finishes the browser approval, the
// flow is cancelled, or it times out. On success the token + account profile
// are persisted and the connected status is returned.
func (a *App) WaitGitHubOAuth() (GitHubConnectionStatus, error) {
	a.oauthMu.Lock()
	flow := a.githubLogin
	a.githubLogin = nil
	a.oauthMu.Unlock()
	if flow == nil || flow.ctx == nil || flow.finish == nil || flow.claim == nil {
		return GitHubConnectionStatus{}, fmt.Errorf("没有正在进行的 GitHub 登录")
	}
	defer func() {
		flow.finish()
		a.clearGitHubOwnedGen(flow.generation)
	}()

	token, err := oauth.PollGitHubDeviceCodeWith(flow.ctx, flow.clientID, flow.deviceCode, flow.interval)
	if err != nil {
		// Distinguish the two exits: a cancelled login should stay silent in
		// the UI, a timed-out one is an actual error the user must see.
		if errors.Is(err, context.DeadlineExceeded) {
			return GitHubConnectionStatus{}, fmt.Errorf("GitHub 登录超时，请重试")
		}
		if errors.Is(err, context.Canceled) {
			return GitHubConnectionStatus{}, fmt.Errorf("GitHub 登录已取消")
		}
		return GitHubConnectionStatus{}, fmt.Errorf("GitHub 登录失败: %w", err)
	}

	user, scopes, err := oauth.FetchGitHubUser(flow.ctx, token)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return GitHubConnectionStatus{}, fmt.Errorf("GitHub 登录超时，请重试")
		}
		if errors.Is(err, context.Canceled) {
			return GitHubConnectionStatus{}, fmt.Errorf("GitHub 登录已取消")
		}
		return GitHubConnectionStatus{}, fmt.Errorf("GitHub 登录成功但获取账号信息失败: %w", err)
	}

	if err := flow.claim(func() error {
		if a.credentialStore == nil {
			return fmt.Errorf("credential store 未初始化")
		}
		return a.credentialStore.Modify(oauth.GitHubAccountProviderID, func(_ *oauth.StoredCredential) (*oauth.StoredCredential, error) {
			return &oauth.StoredCredential{
				Type:        "oauth",
				AccessToken: token,
				Login:       user.Login,
				DisplayName: user.Name,
				AvatarURL:   user.AvatarURL,
				HTMLURL:     user.HTMLURL,
				Scopes:      scopes,
			}, nil
		})
	}); err != nil {
		return GitHubConnectionStatus{}, err
	}

	a.setGitHubTokenCache(token)
	log.Printf("[github] connected account=%s scopes=%s", user.Login, scopes)
	return a.githubConnectionStatus(), nil
}

// CancelGitHubOAuth stops an in-progress device login — whether the flow is
// still waiting to be claimed or WaitGitHubOAuth is already polling it.
func (a *App) CancelGitHubOAuth() {
	a.oauthMu.Lock()
	flow := a.githubLogin
	owns := a.githubOwnedGen != 0 && a.githubOwnedGen == a.oauthGeneration
	// A stored flow from an older attempt must not hide the device request
	// that currently owns the single-flight slot.
	if flow != nil && flow.generation != a.oauthGeneration {
		flow = nil
	} else if flow != nil {
		a.githubLogin = nil
	}
	var cancel context.CancelFunc
	if owns && flow == nil && a.oauthCancel != nil {
		// Wait owns the flow, or the device request has not stored one yet.
		// Cancel that context before releasing the lock so a newer login
		// cannot be the one that gets cancelled.
		a.oauthGeneration++
		cancel = a.oauthCancel
		a.oauthCancel = nil
		a.githubOwnedGen = 0
	} else if owns && flow != nil {
		a.githubOwnedGen = 0
	}
	a.oauthMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if flow != nil && flow.finish != nil {
		// This flow's own finish cancels its context. It does not bump the
		// global generation, so a login that already replaced it stays up.
		flow.finish()
	}
}

// clearGitHubOwnedGen drops the generation marker once a flow ends, so a
// later CancelGitHubOAuth does not cancel an unrelated in-flight login.
func (a *App) clearGitHubOwnedGen(gen uint64) {
	a.oauthMu.Lock()
	if a.githubOwnedGen == gen {
		a.githubOwnedGen = 0
	}
	a.oauthMu.Unlock()
}

// DisconnectGitHub removes the stored credential (logout). An in-flight
// device login is cancelled first — otherwise it could complete after the
// disconnect and write a fresh credential back.
func (a *App) DisconnectGitHub() error {
	a.CancelGitHubOAuth()
	if a.credentialStore == nil {
		return fmt.Errorf("credential store 未初始化")
	}
	if err := a.credentialStore.Delete(oauth.GitHubAccountProviderID); err != nil {
		return err
	}
	a.setGitHubTokenCache("")
	return nil
}

// GitHubAPIRequest performs an authenticated api.github.com call on behalf of
// the connected account. Path may be "/rate_limit" or a full api.github.com
// URL. Used by update checks and other GitHub-backed features.
func (a *App) GitHubAPIRequest(method, apiPath string) (string, error) {
	// One-off UI action — read the store directly so an externally updated
	// credential is honored (the cache exists for the skill-search hot path).
	token := a.loadGitHubAccessToken()
	if token == "" {
		return "", fmt.Errorf("GitHub 未连接")
	}
	if cfg, err := a.LoadConfig(); err == nil {
		oauth.ApplyProxyFromAppConfig(cfg)
	}
	return oauth.DoGitHubAPI(token, method, apiPath)
}

// normalizeGitHubCloneURL reduces the accepted repo forms to a canonical
// https clone URL plus repo name:
//   - https://github.com/owner/repo[.git][/deeper/path] (tree/PR links work)
//   - git@github.com:owner/repo[.git] / ssh://git@github.com/owner/repo[.git]
//     (SSH forms → https; token auth needs it)
//   - github.com/owner/repo
//   - owner/repo
func normalizeGitHubCloneURL(raw string) (cloneURL, repoName string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", fmt.Errorf("仓库地址为空")
	}
	// SSH shorthand: git@github.com:owner/repo.git or ssh://git@github.com/o/r.
	// Host prefixes compare case-insensitively; the path suffix keeps its
	// original case since repo paths are case-sensitive.
	lower := strings.ToLower(raw)
	switch {
	case strings.HasPrefix(lower, "git@github.com:"):
		raw = "https://github.com/" + raw[len("git@github.com:"):]
	case strings.HasPrefix(lower, "ssh://git@github.com/"):
		raw = "https://github.com/" + raw[len("ssh://git@github.com/"):]
	case strings.HasPrefix(lower, "github.com/"):
		raw = "https://" + raw
	case !strings.Contains(raw, "://") && !strings.ContainsAny(raw, "@:"):
		// owner/repo shorthand. Strings with "@" or ":" are scp-style SSH or
		// foreign forms — never reshape them into a github.com URL.
		if parts := strings.Split(strings.Trim(raw, "/"), "/"); len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			raw = "https://github.com/" + raw
		}
	}

	parsed, perr := url.Parse(raw)
	if perr != nil || parsed == nil || !strings.EqualFold(parsed.Host, "github.com") || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return "", "", fmt.Errorf("仅支持克隆 github.com 仓库")
	}
	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(segments) < 2 || segments[0] == "" || segments[1] == "" {
		return "", "", fmt.Errorf("仓库地址缺少 owner/repo")
	}
	owner, repo := segments[0], strings.TrimSuffix(segments[1], ".git")
	return "https://github.com/" + owner + "/" + repo + ".git", repo, nil
}

// GitHubCloneResult reports where a clone landed alongside git's output so
// the UI can offer "show in folder" afterwards.
type GitHubCloneResult struct {
	Output  string `json:"output"`
	DestDir string `json:"dest_dir"`
	RepoURL string `json:"repo_url"`
}

// GitHubCloneRepository clones a github.com repo into destDir using the
// connected account's token. destDir empty → <dataDir>/github/<repo>.
// The token rides the git http.extraheader config flag so it never lands in
// the clone's .git/config remote URL.
func (a *App) GitHubCloneRepository(repoURL, destDir string) (GitHubCloneResult, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return GitHubCloneResult{}, fmt.Errorf("未找到 git，请先安装 Git")
	}
	cloneURL, repoName, err := normalizeGitHubCloneURL(repoURL)
	if err != nil {
		return GitHubCloneResult{}, err
	}

	destDir = strings.TrimSpace(destDir)
	if destDir == "" {
		destDir = filepath.Join(a.GetDataDir(), "github", repoName)
	}
	result := GitHubCloneResult{DestDir: destDir, RepoURL: cloneURL}

	token := a.loadGitHubAccessToken()
	args := []string{"-c", "core.longpaths=true", "clone", "--progress", cloneURL, destDir}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	if token != "" {
		// The token travels in the process environment rather than argv,
		// which other local processes can read (ps, /proc/<pid>/cmdline).
		cmd.Env = append(os.Environ(),
			"GIT_CONFIG_COUNT=1",
			"GIT_CONFIG_KEY_0=http.https://github.com/.extraheader",
			"GIT_CONFIG_VALUE_0=AUTHORIZATION: bearer "+token,
		)
	}
	out, err := cmd.CombinedOutput()
	result.Output = string(out)
	if token != "" {
		// Never let the token leak into logs/UI via git's stderr echo.
		result.Output = strings.ReplaceAll(result.Output, token, "***")
	}
	if err != nil {
		// Surface git's own message ("remote: Repository not found") rather
		// than a bare exit status the UI cannot act on.
		detail := strings.TrimSpace(result.Output)
		if detail == "" {
			detail = err.Error()
		}
		return result, fmt.Errorf("git clone 失败: %s", detail)
	}
	log.Printf("[github] cloned %s -> %s", cloneURL, destDir)
	return result, nil
}
