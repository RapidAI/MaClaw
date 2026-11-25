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
	// GithubAppToken marks a "ghu_" legacy GitHub App user token: the scope
	// list is empty by design and only a fresh OAuth App login can fix it.
	GithubAppToken bool `json:"github_app_token,omitempty"`
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

// loadGitHubCredential reads the signed-in account credential straight from
// the store — token plus the recorded scope grant. nil when not logged in.
func (a *App) loadGitHubCredential() *oauth.StoredCredential {
	if a.credentialStore == nil {
		return nil
	}
	cred, err := a.credentialStore.Read(oauth.GitHubAccountProviderID)
	if err != nil || cred == nil || strings.TrimSpace(cred.AccessToken) == "" {
		return nil
	}
	return cred
}

// loadGitHubAccessToken reads the token straight from the credential store.
func (a *App) loadGitHubAccessToken() string {
	cred := a.loadGitHubCredential()
	if cred == nil {
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
	status.GithubAppToken = oauth.GitHubAppUserToken(cred.AccessToken)
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
	if oauth.GitHubAppClientID(clientID) {
		// A GitHub App device flow ignores the requested scope, so the stored
		// grant stays empty and "reconnect to add repo" can never succeed.
		return GitHubDeviceInfo{}, fmt.Errorf("这个 Client ID 是 GitHub App，设备码登录发不出 repo 权限。请改用 OAuth App 的 Client ID，或留空使用内置登录")
	}

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

	token, grantedScopes, err := oauth.PollGitHubDeviceCodeWith(flow.ctx, flow.clientID, flow.deviceCode, flow.interval)
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

	user, headerScopes, err := oauth.FetchGitHubUser(flow.ctx, token)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return GitHubConnectionStatus{}, fmt.Errorf("GitHub 登录超时，请重试")
		}
		if errors.Is(err, context.Canceled) {
			return GitHubConnectionStatus{}, fmt.Errorf("GitHub 登录已取消")
		}
		return GitHubConnectionStatus{}, fmt.Errorf("GitHub 登录成功但获取账号信息失败: %w", err)
	}

	scopes := oauth.GitHubEffectiveScopes(headerScopes, grantedScopes)
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
	a.resyncRepoMaintAfterLogin()
	go a.syncCloudAgentGitCredentials()
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

// Forge services the clone card accepts.
const (
	cloneServiceGitHub = "github"
	cloneServiceGitLab = "gitlab"
)

// cloneHost is one accepted clone origin: host[:port] plus its https base.
type cloneHost struct {
	host string
	base string
}

// savedGitLabCloneHost reduces the connected GitLab account's origin to its
// https host[:port]. nil when no usable origin is saved — a token is never
// sent to a host the user did not connect.
func savedGitLabCloneHost(gitlabOrigin string) *cloneHost {
	raw := strings.TrimSpace(gitlabOrigin)
	if raw == "" {
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed == nil || parsed.Host == "" || !strings.EqualFold(parsed.Scheme, "https") {
		return nil
	}
	host := strings.ToLower(parsed.Hostname())
	if port := parsed.Port(); port != "" {
		host += ":" + port
	}
	return &cloneHost{host: host, base: "https://" + host}
}

// normalizeForgeCloneURL reduces the accepted repo forms to a canonical
// https clone URL plus repo name and forge service:
//   - https://github.com/owner/repo[.git][/deeper/path] (tree/PR links work)
//   - https://gitlab.com/group[/sub]/repo[.git][/deeper/path]
//   - https://<saved GitLab origin>/group/repo… (self-hosted, exact host match)
//   - git@host:… / ssh://git@host/… SSH forms → https (token auth needs it)
//   - github.com/owner/repo, gitlab.com/group/repo, <saved-host>/group/repo
//   - owner/repo (defaults to GitHub)
//
// gitlabOrigin is the connected GitLab account's origin; a clone on any other
// host is refused so the token is never sent somewhere the user did not
// connect (same rule as corelib/gitforge.parseURL).
func normalizeForgeCloneURL(raw, gitlabOrigin string) (cloneURL, repoName, service string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", "", fmt.Errorf("仓库地址为空")
	}
	saved := savedGitLabCloneHost(gitlabOrigin)

	// SSH shorthand: git@host:o/r.git or ssh://git@host/o/r. Host prefixes
	// compare case-insensitively; the path suffix keeps its original case
	// since repo paths are case-sensitive.
	lower := strings.ToLower(raw)
	switch {
	case strings.HasPrefix(lower, "git@github.com:"):
		raw = "https://github.com/" + raw[len("git@github.com:"):]
	case strings.HasPrefix(lower, "ssh://git@github.com/"):
		raw = "https://github.com/" + raw[len("ssh://git@github.com/"):]
	case strings.HasPrefix(lower, "git@gitlab.com:"):
		raw = "https://gitlab.com/" + raw[len("git@gitlab.com:"):]
	case strings.HasPrefix(lower, "ssh://git@gitlab.com/"):
		raw = "https://gitlab.com/" + raw[len("ssh://git@gitlab.com/"):]
	case saved != nil && strings.HasPrefix(lower, "git@"+saved.host+":"):
		raw = saved.base + "/" + raw[len("git@"+saved.host+":"):]
	case saved != nil && strings.HasPrefix(lower, "ssh://git@"+saved.host+"/"):
		raw = saved.base + "/" + raw[len("ssh://git@"+saved.host+"/"):]
	case strings.HasPrefix(lower, "github.com/"), strings.HasPrefix(lower, "www.github.com/"):
		raw = "https://" + raw
	case strings.HasPrefix(lower, "gitlab.com/"), strings.HasPrefix(lower, "www.gitlab.com/"):
		raw = "https://" + raw
	case saved != nil && strings.HasPrefix(lower, saved.host+"/"):
		raw = saved.base + "/" + raw[len(saved.host+"/"):]
	case !strings.Contains(raw, "://") && !strings.ContainsAny(raw, "@:"):
		// owner/repo shorthand. Strings with "@" or ":" are scp-style SSH or
		// foreign forms — never reshape them into a github.com URL. GitHub
		// owner names never contain a dot, so "github.com.evil.com/o/r" is
		// a lookalike host, not a valid owner, and is refused by falling
		// through to the URL parse below.
		if parts := strings.Split(strings.Trim(raw, "/"), "/"); len(parts) == 2 && parts[0] != "" && parts[1] != "" && !strings.Contains(parts[0], ".") {
			raw = "https://github.com/" + raw
		}
	}

	parsed, perr := url.Parse(raw)
	if perr != nil || parsed == nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return "", "", "", fmt.Errorf("仓库地址只接受 http(s) 链接，例如 https://github.com/owner/repo")
	}
	host := strings.ToLower(parsed.Hostname())
	if port := parsed.Port(); port != "" {
		host += ":" + port
	}
	// GitLab routes file views under "/-/" (tree/blob/commit/issues): cut the
	// suffix so a pasted file link still lands on the repo itself. "-" is a
	// reserved name and can never be a real group or project segment.
	path := strings.Trim(parsed.EscapedPath(), "/")
	if host != "github.com" && host != "www.github.com" {
		if idx := strings.Index(path, "/-/"); idx >= 0 {
			path = path[:idx]
		}
	}
	segments := strings.Split(path, "/")
	for _, seg := range segments {
		if seg == "" || seg == "." || seg == ".." {
			return "", "", "", fmt.Errorf("仓库地址缺少仓库路径")
		}
	}
	switch {
	case host == "github.com" || host == "www.github.com":
		if len(segments) < 2 {
			return "", "", "", fmt.Errorf("仓库地址缺少 owner/repo")
		}
		repo := strings.TrimSuffix(segments[1], ".git")
		cloneURL = "https://github.com/" + segments[0] + "/" + repo + ".git"
		repoName = repo
		service = cloneServiceGitHub
	case host == "gitlab.com" || host == "www.gitlab.com":
		if len(segments) < 2 {
			return "", "", "", fmt.Errorf("GitLab 仓库地址缺少 group/repo")
		}
		cloneURL, repoName, service = gitlabCloneOn("https://gitlab.com", segments)
	case saved != nil && host == saved.host:
		if len(segments) < 2 {
			return "", "", "", fmt.Errorf("GitLab 仓库地址缺少 group/repo")
		}
		cloneURL, repoName, service = gitlabCloneOn(saved.base, segments)
	default:
		return "", "", "", fmt.Errorf("仅支持克隆 github.com、gitlab.com 或已连接的自建 GitLab 仓库")
	}
	return cloneURL, repoName, service, nil
}

// gitlabCloneOn builds the canonical clone URL for a GitLab repo path.
// GitLab repos nest under groups, so any path of >= 2 segments is accepted;
// only the repo name carries the .git suffix.
func gitlabCloneOn(base string, segments []string) (cloneURL, repoName, service string) {
	segments[len(segments)-1] = strings.TrimSuffix(segments[len(segments)-1], ".git")
	path := strings.Join(segments, "/")
	return base + "/" + path + ".git", segments[len(segments)-1], cloneServiceGitLab
}

// GitHubCloneResult reports where a clone landed alongside git's output so
// the UI can offer "show in folder" afterwards.
type GitHubCloneResult struct {
	Output  string `json:"output"`
	DestDir string `json:"dest_dir"`
	RepoURL string `json:"repo_url"`
}

// GitHubCloneRepository clones a github.com / gitlab.com / self-hosted
// GitLab repo into destDir using the matching connected account's token.
// destDir empty → <dataDir>/<service>/<repo>. The token is given to a
// one-shot credential helper through the environment, so it never lands in
// the clone's .git/config or in argv.
func (a *App) GitHubCloneRepository(repoURL, destDir string) (GitHubCloneResult, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return GitHubCloneResult{}, fmt.Errorf("未找到 git，请先安装 Git")
	}
	// One credential read feeds both the origin match and the token choice.
	gitlabCred := a.gitlabCredential()
	savedOrigin := ""
	if gitlabCred != nil {
		savedOrigin = strings.TrimSpace(gitlabCred.BaseURL)
	}
	cloneURL, repoName, service, err := normalizeForgeCloneURL(repoURL, savedOrigin)
	if err != nil {
		return GitHubCloneResult{}, err
	}

	destDir = strings.TrimSpace(destDir)
	if destDir == "" {
		destDir = filepath.Join(a.GetDataDir(), service, repoName)
	}
	result := GitHubCloneResult{DestDir: destDir, RepoURL: cloneURL}

	token, tokenEnv := a.cloneCredentials(service, cloneURL, gitlabCred)
	if err := forgeCloneTokenOK(token); err != nil {
		return result, err
	}
	args := append(forgeCloneAuthArgs(service, cloneURL, token), "clone", "--progress", cloneURL, destDir)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = forgeCloneEnv(os.Environ(), tokenEnv, token)
	out, err := cmd.CombinedOutput()
	result.Output = redactCloneOutput(string(out), token)
	if err != nil {
		// Surface git's own message ("remote: Repository not found") rather
		// than a bare exit status the UI cannot act on.
		detail := strings.TrimSpace(result.Output)
		if detail == "" {
			detail = err.Error()
		}
		return result, fmt.Errorf("git clone 失败: %s", detail)
	}
	log.Printf("[%s] cloned %s -> %s", service, cloneURL, destDir)
	return result, nil
}

// cloneCredentials picks the token and its env var for one clone. GitHub
// uses the connected account for github.com. GitLab uses the connected
// account's PAT only when the clone targets the same instance the account
// was connected to — a token is never sent to a host the user did not
// connect; anything else clones anonymously (public repos only).
func (a *App) cloneCredentials(service, cloneURL string, cred *oauth.StoredCredential) (token, tokenEnv string) {
	if service == cloneServiceGitHub {
		return a.loadGitHubAccessToken(), githubCloneTokenEnv
	}
	tokenEnv = gitlabCloneTokenEnv
	if cred == nil {
		return "", tokenEnv
	}
	saved := savedGitLabCloneHost(strings.TrimSpace(cred.BaseURL))
	parsed, err := url.Parse(cloneURL)
	if err != nil || parsed == nil || saved == nil || !strings.EqualFold(parsed.Host, saved.host) {
		return "", tokenEnv
	}
	return strings.TrimSpace(cred.AccessToken), tokenEnv
}

// githubCloneTokenEnv / gitlabCloneTokenEnv are the variables the one-shot
// credential helper reads. They are not GITHUB_TOKEN / GITLAB_TOKEN: other
// tools on the machine treat those names as their own.
const (
	githubCloneTokenEnv = "MACLAW_GITHUB_TOKEN"
	gitlabCloneTokenEnv = "MACLAW_GITLAB_TOKEN"
)

// forgeCloneAuthArgs is the git -c prefix for one clone. Empty
// credential.helper and http.extraheader values are reset sentinels, and
// they have to be arguments: on Windows an empty environment variable is
// unset, so git reports "missing config value" and never starts the clone.
// The helper is added after those resets. Command-line config is applied
// after files and after GIT_CONFIG_*, so this also drops a stale
// Authorization header and Git Credential Manager for this command only.
func forgeCloneAuthArgs(service, cloneURL, token string) []string {
	args := []string{"-c", "core.longpaths=true"}
	if token == "" {
		return args
	}
	origin := cloneOriginForConfig(cloneURL)
	return append(args,
		"-c", "credential.helper=",
		"-c", "credential.https://"+origin+".helper=",
		"-c", "http.extraheader=",
		"-c", "http.https://"+origin+"/.extraheader=",
		"-c", "http.proactiveAuth=basic",
		"-c", cloneCredentialHelper(service),
	)
}

// cloneOriginForConfig reduces a clone URL to the host git config keys
// address: scheme dropped, host[:port] only.
func cloneOriginForConfig(cloneURL string) string {
	parsed, err := url.Parse(cloneURL)
	if err != nil || parsed == nil || parsed.Host == "" {
		return "github.com"
	}
	return strings.ToLower(parsed.Host)
}

// cloneCredentialHelper is a shell snippet git runs only for this command.
// GitHub's git HTTPS endpoint rejects Authorization: Bearer with the body
// "invalid credentials" (that is the maintenance failure). It accepts Basic
// auth with username x-access-token; GitLab takes username oauth2. The
// snippet prints that pair and reads the token from the environment, so the
// secret is not in argv or .git/config.
// f is invoked with "$@" because git runs `sh -c <snippet> credential-helper get`
// and the operation is a positional parameter of that shell, not of f.
// Git for Windows runs that snippet with sh.exe. A desktop process often has
// Git\cmd on PATH and not Git\usr\bin; cmd\git.exe is a wrapper and puts
// usr\bin on the helper's PATH itself. The snippet must stay inline: an
// absolute backslash path is handed to sh, which treats `\` as an escape,
// and the helper never starts.
func cloneCredentialHelper(service string) string {
	username, tokenEnv := "x-access-token", githubCloneTokenEnv
	if service == cloneServiceGitLab {
		username, tokenEnv = "oauth2", gitlabCloneTokenEnv
	}
	return `credential.helper=!f() { case "$1" in get) printf '%s\n' username=` + username + `; printf '%s\n' "password=$` + tokenEnv + `";; esac; }; f "$@"`
}

// forgeCloneTokenOK accepts an empty token (anonymous clone) and the
// characters a GitHub OAuth token or GitLab PAT actually uses. Anything
// else is refused so it cannot break out of the helper's quoted password.
func forgeCloneTokenOK(token string) error {
	if token == "" {
		return nil
	}
	for _, r := range token {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-', r == '.':
		default:
			return fmt.Errorf("仓库令牌无法用于 git 认证")
		}
	}
	return nil
}

// forgeCloneEnv builds the child environment for one clone.
// The secret stays in the environment, where other local processes can
// still read it, and out of argv. When a token is set, inherited GIT_CONFIG_*
// pairs are dropped so a parent process cannot add a second Authorization
// header. An empty token clones anonymously and leaves the user's credential
// helper and git config alone.
func forgeCloneEnv(base []string, tokenEnv, token string) []string {
	env := make([]string, 0, len(base)+4)
	for _, entry := range base {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if token != "" && cloneEnvDropped(key) {
			continue
		}
		env = append(env, entry)
	}
	if token == "" {
		return env
	}
	// An unattended clone must not sit on a credential-manager dialog
	// after the forge rejects the request.
	return append(env,
		"GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE=Never",
		tokenEnv+"="+token,
	)
}

func cloneEnvDropped(key string) bool {
	switch strings.ToUpper(key) {
	case "GIT_CONFIG_COUNT", "GIT_CONFIG_PARAMETERS", "GIT_TERMINAL_PROMPT", "GCM_INTERACTIVE",
		githubCloneTokenEnv, gitlabCloneTokenEnv:
		return true
	}
	return strings.HasPrefix(strings.ToUpper(key), "GIT_CONFIG_KEY_") || strings.HasPrefix(strings.ToUpper(key), "GIT_CONFIG_VALUE_")
}

func redactCloneOutput(output, token string) string {
	if token == "" {
		return output
	}
	return strings.ReplaceAll(output, token, "***")
}
