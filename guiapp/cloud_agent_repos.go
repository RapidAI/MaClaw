package guiapp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/oauth"
)

// CloudAgentGitRepository is one selectable repository in the cloud coding
// connect dialog. It intentionally carries no credential material.
type CloudAgentGitRepository struct {
	Name          string `json:"name"`
	Description   string `json:"description,omitempty"`
	WebURL        string `json:"web_url,omitempty"`
	DefaultBranch string `json:"default_branch,omitempty"`
	UpdatedAt     string `json:"updated_at,omitempty"`
	Private       bool   `json:"private,omitempty"`
}

// The repository list is shared across every dialog open and the 重试 button.
// Each open used to cost one /user/repos call; after a quota exhaustion that
// turned every retry into another dead request. freshTTL serves repeat opens
// without touching the API; a stale entry (within staleTTL) is still returned
// when the live fetch fails on a rate limit, so the picker keeps working and
// only the freshest rows are missing. Entries are ACCOUNT-scoped: switching
// the connected account (or disconnecting, or a re-login that dropped the
// repo scope) must never serve the previous credential's repositories.
const (
	cloudAgentRepoCacheFreshTTL = 5 * time.Minute
	cloudAgentRepoCacheStaleTTL = 24 * time.Hour
)

type cloudAgentRepoCacheEntry struct {
	owner     string
	rows      []CloudAgentGitRepository
	fetchedAt time.Time
}

type cloudAgentRepoCache struct {
	mu      sync.Mutex
	entries map[string]cloudAgentRepoCacheEntry
	// now is the clock, injectable so tests can travel across the TTLs
	// without sleeping. nil means time.Now.
	now func() time.Time
}

func (c *cloudAgentRepoCache) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// get serves an entry only for the same account. owner=="" (nothing
// connected) never matches, so a disconnect clears the picker's fallback.
func (c *cloudAgentRepoCache) get(service, owner string, maxAge time.Duration) ([]CloudAgentGitRepository, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if owner == "" {
		return nil, false
	}
	entry, ok := c.entries[service]
	if !ok || entry.owner != owner || c.clock().Sub(entry.fetchedAt) > maxAge {
		return nil, false
	}
	out := make([]CloudAgentGitRepository, len(entry.rows))
	copy(out, entry.rows)
	return out, true
}

func (c *cloudAgentRepoCache) put(service, owner string, rows []CloudAgentGitRepository) {
	if owner == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]cloudAgentRepoCacheEntry)
	}
	c.entries[service] = cloudAgentRepoCacheEntry{owner: owner, rows: rows, fetchedAt: c.clock()}
}

// cloudAgentGitHubCacheOwnerKey identifies the connected GitHub account for
// the cache. Login is stable across re-logins (same account = same repo
// space); UserID covers credentials stored before the login was recorded.
// The repo scope is part of the key's validity: a re-login of the SAME
// account without the repo scope must not serve the scoped session's cached
// list. Empty for anything the loader would refuse (no token, no scope).
func cloudAgentGitHubCacheOwnerKey(cred *oauth.StoredCredential) string {
	if cred == nil || strings.TrimSpace(cred.AccessToken) == "" {
		return ""
	}
	if !oauth.GitHubScopesHaveRepo(cred.Scopes) {
		return ""
	}
	login := strings.TrimSpace(cred.Login)
	if login == "" {
		login = strings.TrimSpace(cred.UserID)
	}
	return login
}

// cloudAgentGitLabCacheOwnerKey adds the site: the same login on a
// self-hosted GitLab is a different account space.
func cloudAgentGitLabCacheOwnerKey(cred *oauth.StoredCredential) string {
	if cred == nil || strings.TrimSpace(cred.AccessToken) == "" {
		return ""
	}
	if !oauth.GitLabScopesHaveRepo(cred.Scopes) {
		return ""
	}
	login := strings.TrimSpace(cred.Login)
	origin := strings.TrimSpace(cred.BaseURL)
	if login == "" && origin == "" {
		return ""
	}
	return login + "|" + origin
}

func (a *App) cloudAgentGitHubCacheOwner() string {
	return cloudAgentGitHubCacheOwnerKey(a.loadGitHubCredential())
}

func (a *App) cloudAgentGitLabCacheOwner() string {
	return cloudAgentGitLabCacheOwnerKey(a.gitlabCredential())
}

// CloudAgentGitRepositories lists repositories visible to the account already
// connected in Settings → GitHub & GitLab. The webview receives only metadata;
// the token never leaves the backend credential store for this flow.
func (a *App) CloudAgentGitRepositories(service string) ([]CloudAgentGitRepository, error) {
	key := strings.ToLower(strings.TrimSpace(service))
	var owner string
	switch key {
	case "github":
		owner = a.cloudAgentGitHubCacheOwner()
	case "gitlab":
		owner = a.cloudAgentGitLabCacheOwner()
	default:
		return nil, fmt.Errorf("代码平台只支持 GitHub 或 GitLab")
	}
	if rows, ok := a.cloudAgentRepos.get(key, owner, cloudAgentRepoCacheFreshTTL); ok {
		return rows, nil
	}
	if cfg, err := a.LoadConfig(); err == nil {
		oauth.ApplyProxyFromAppConfig(cfg)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var (
		rows []CloudAgentGitRepository
		err  error
	)
	switch key {
	case "github":
		rows, err = a.cloudAgentGitHubRepositories(ctx)
	case "gitlab":
		rows, err = a.cloudAgentGitLabRepositories(ctx)
	}
	if err != nil {
		// A rate-limited (or otherwise failed) fetch falls back to the last
		// known list of the SAME account within staleTTL: the picker keeps
		// working and the retry does not turn into another dead API call.
		if stale, ok := a.cloudAgentRepos.get(key, owner, cloudAgentRepoCacheStaleTTL); ok {
			logCloudAgentRepoCacheFallback(key, err, len(stale))
			return stale, nil
		}
		return nil, cloudAgentRepoListError(err)
	}
	if len(rows) == 0 {
		// A genuinely empty account is not cached: the user may create (or be
		// granted access to) their first repository at any moment, and a
		// cached empty list would also turn a later failed fetch into a
		// silent "no repositories" answer.
		return rows, nil
	}
	a.cloudAgentRepos.put(key, owner, rows)
	return rows, nil
}

// logCloudAgentRepoCacheFallback records why the picker is serving a stale
// list. A zero reset time (secondary limits answer without the header) logs
// without one.
func logCloudAgentRepoCacheFallback(service string, err error, entries int) {
	var rlErr *oauth.GitHubRateLimitError
	if !errors.As(err, &rlErr) {
		log.Printf("[cloud-agent] %s repo list served from cache after fetch failure: %v", service, err)
		return
	}
	if rlErr.ResetAt.IsZero() {
		log.Printf("[cloud-agent] %s repo list served from cache after rate limit: entries=%d", service, entries)
		return
	}
	log.Printf("[cloud-agent] %s repo list served from cache after rate limit: reset=%s entries=%d",
		service, rlErr.ResetAt.Format("15:04"), entries)
}

// cloudAgentRepoListError is the error the dialog shows when no cached
// fallback exists. The rate-limit branch swaps GitHub's raw body (a
// scraping-policy paragraph plus a support request id — it reads like a ban
// instead of an hourly window) for actionable text; the typed error stays
// intact in the chain until this single conversion point.
func cloudAgentRepoListError(err error) error {
	var rlErr *oauth.GitHubRateLimitError
	if errors.As(err, &rlErr) {
		return errors.New(cloudAgentRateLimitMessage(rlErr))
	}
	return err
}

// cloudAgentRateLimitMessage converts an exhausted-quota error into dialog
// text: when the window resets and what to do meanwhile. GitHub's raw body
// (a scraping-policy paragraph plus a support request id) is deliberately
// dropped — it reads like a ban instead of an hourly window.
func cloudAgentRateLimitMessage(rlErr *oauth.GitHubRateLimitError) string {
	if rlErr == nil {
		return ""
	}
	if rlErr.ResetAt.IsZero() {
		return "GitHub 接口调用配额已用尽（认证账号每小时 5000 次），稍后会自动重置；期间可在下方手动输入 owner/repo 连接"
	}
	return fmt.Sprintf("GitHub 接口调用配额已用尽（认证账号每小时 5000 次），约 %s 自动重置；期间可在下方手动输入 owner/repo 连接",
		rlErr.ResetAt.Format("15:04"))
}

func (a *App) cloudAgentGitHubRepositories(ctx context.Context) ([]CloudAgentGitRepository, error) {
	cred := a.loadGitHubCredential()
	if cred == nil || strings.TrimSpace(cred.AccessToken) == "" {
		return nil, fmt.Errorf("请先在设置中连接 GitHub")
	}
	if !oauth.GitHubScopesHaveRepo(cred.Scopes) {
		return nil, fmt.Errorf("当前 GitHub 连接没有仓库权限，请在设置中断开 GitHub 后重新登录")
	}
	rows, err := oauth.FetchGitHubRepositories(ctx, strings.TrimSpace(cred.AccessToken), 100)
	if err != nil {
		// The typed rate-limit error stays in the chain: the caller's cache
		// fallback logs the reset time from it, and cloudAgentRepoListError
		// does the single text conversion at the boundary.
		return nil, fmt.Errorf("GitHub 仓库列表获取失败: %w", err)
	}
	out := make([]CloudAgentGitRepository, 0, len(rows))
	for _, row := range rows {
		out = append(out, CloudAgentGitRepository{
			Name:          row.FullName,
			Description:   row.Description,
			WebURL:        row.HTMLURL,
			DefaultBranch: row.DefaultBranch,
			UpdatedAt:     row.UpdatedAt,
			Private:       row.Private,
		})
	}
	return out, nil
}

func (a *App) cloudAgentGitLabRepositories(ctx context.Context) ([]CloudAgentGitRepository, error) {
	cred := a.gitlabCredential()
	if cred == nil || strings.TrimSpace(cred.AccessToken) == "" {
		return nil, fmt.Errorf("请先在设置中连接 GitLab")
	}
	if !oauth.GitLabScopesHaveRepo(cred.Scopes) {
		return nil, fmt.Errorf("当前 GitLab 令牌没有仓库写权限，请在设置中重新连接 GitLab")
	}
	origin := strings.TrimSpace(cred.BaseURL)
	if !accountHTTPSRootOrigin(origin) {
		return nil, fmt.Errorf("GitLab 连接缺少有效的 HTTPS 站点地址，请在设置中重新连接 GitLab")
	}
	rows, err := oauth.FetchGitLabProjects(ctx, origin, strings.TrimSpace(cred.AccessToken), 100)
	if err != nil {
		return nil, fmt.Errorf("GitLab 仓库列表获取失败: %w", err)
	}
	out := make([]CloudAgentGitRepository, 0, len(rows))
	for _, row := range rows {
		out = append(out, CloudAgentGitRepository{
			Name:          row.PathWithNamespace,
			Description:   row.Description,
			WebURL:        row.WebURL,
			DefaultBranch: row.DefaultBranch,
			UpdatedAt:     row.LastActivityAt,
			Private:       !strings.EqualFold(row.Visibility, "public"),
		})
	}
	return out, nil
}
