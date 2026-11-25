package guiapp

import (
	"context"
	"fmt"
	"strings"
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

// CloudAgentGitRepositories lists repositories visible to the account already
// connected in Settings → GitHub & GitLab. The webview receives only metadata;
// the token never leaves the backend credential store for this flow.
func (a *App) CloudAgentGitRepositories(service string) ([]CloudAgentGitRepository, error) {
	if cfg, err := a.LoadConfig(); err == nil {
		oauth.ApplyProxyFromAppConfig(cfg)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	switch strings.ToLower(strings.TrimSpace(service)) {
	case "github":
		return a.cloudAgentGitHubRepositories(ctx)
	case "gitlab":
		return a.cloudAgentGitLabRepositories(ctx)
	default:
		return nil, fmt.Errorf("代码平台只支持 GitHub 或 GitLab")
	}
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
