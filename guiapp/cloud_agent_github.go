package guiapp

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/oauth"
)

// CloudAgentGitHubAccount is the signed-in GitHub credential view the cloud
// git form needs to prefill itself. Token is included so the form can submit
// it straight to maclawsrv; it never crosses the webview boundary otherwise.
type CloudAgentGitHubAccount struct {
	Connected bool   `json:"connected"`
	Login     string `json:"login,omitempty"`
	Name      string `json:"name,omitempty"`
	Email     string `json:"email,omitempty"`
	Token     string `json:"token,omitempty"`
	// RepoScope is false when the token cannot read or push private repos
	// (e.g. a GitHub App user token whose scope list comes back empty).
	RepoScope bool `json:"repo_scope"`
}

// CloudAgentGitHubAccount exposes the connected GitHub account for the cloud
// coding instance's git form. The caller must still click connect — nothing
// is pushed to maclawsrv from here.
func (a *App) CloudAgentGitHubAccount() (CloudAgentGitHubAccount, error) {
	token := a.loadGitHubAccessToken()
	if token == "" {
		return CloudAgentGitHubAccount{Connected: false}, nil
	}
	if cfg, err := a.LoadConfig(); err == nil {
		oauth.ApplyProxyFromAppConfig(cfg)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	user, headerScopes, err := oauth.FetchGitHubUser(ctx, token)
	if err != nil {
		return CloudAgentGitHubAccount{}, fmt.Errorf("GitHub 账号信息获取失败: %w", err)
	}
	name := strings.TrimSpace(user.Name)
	if name == "" {
		name = user.Login
	}
	return CloudAgentGitHubAccount{
		Connected: true,
		Login:     user.Login,
		Name:      name,
		Email:     fmt.Sprintf("%d+%s@users.noreply.github.com", user.ID, user.Login),
		Token:     token,
		RepoScope: oauth.GitHubScopesHaveRepo(headerScopes),
	}, nil
}

// syncCloudAgentGitCredentials refreshes the git token on every coding
// instance whose credential was pushed from the signed-in account
// (source="account"). Called after a successful GitHub login; instances with
// a manually pasted token are left alone.
func (a *App) syncCloudAgentGitCredentials() {
	token := a.loadGitHubAccessToken()
	if token == "" {
		return
	}
	list, err := a.ListCloudAgents()
	if err != nil {
		log.Printf("[cloud-agent] github credential sync list failed: %v", err)
		return
	}
	refreshed := 0
	for _, row := range list.Instances {
		if row.Kind != "coding" {
			continue
		}
		view, err := a.GetCloudAgentGit(row.ID)
		if err != nil {
			log.Printf("[cloud-agent] github credential sync read %s: %v", row.ID, err)
			continue
		}
		if !view.Connected || view.Source != "account" || view.Service != "github" {
			continue
		}
		_, err = a.ConnectCloudAgentGit(row.ID, CloudGitConnect{
			Service:     "github",
			Origin:      view.Origin,
			Repo:        view.Repo,
			Ref:         view.Ref,
			Token:       token,
			AuthorName:  view.AuthorName,
			AuthorEmail: view.AuthorEmail,
			Source:      "account",
		})
		if err != nil {
			log.Printf("[cloud-agent] github credential sync %s: %v", row.ID, err)
			continue
		}
		refreshed++
	}
	if refreshed > 0 {
		log.Printf("[cloud-agent] github credential sync refreshed %d instance(s)", refreshed)
	}
}
