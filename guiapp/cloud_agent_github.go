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
	// GithubAppToken is true for a "ghu_" GitHub App user token. Those carry
	// no OAuth scopes by design, so the only fix is a fresh OAuth App login —
	// the form says that instead of pointing at a scope checkbox.
	GithubAppToken bool `json:"github_app_token,omitempty"`
}

// CloudAgentGitHubAccount exposes the connected GitHub account for the cloud
// coding instance's git form. The caller must still click connect — nothing
// is pushed to maclawsrv from here.
func (a *App) CloudAgentGitHubAccount() (CloudAgentGitHubAccount, error) {
	cred := a.loadGitHubCredential()
	if cred == nil {
		return CloudAgentGitHubAccount{Connected: false}, nil
	}
	token := strings.TrimSpace(cred.AccessToken)
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
	// The /user header is the live grant; the stored grant covers the
	// header-stripped edge (a proxy can drop X-OAuth-Scopes) so a healthy
	// token is not reported scopeless. A non-empty header still wins, so a
	// server-side revocation keeps outranking the stale stored grant.
	repoScope := oauth.GitHubScopesHaveRepo(oauth.GitHubEffectiveScopes(headerScopes, cred.Scopes))
	return CloudAgentGitHubAccount{
		Connected:      true,
		Login:          user.Login,
		Name:           name,
		Email:          fmt.Sprintf("%d+%s@users.noreply.github.com", user.ID, user.Login),
		Token:          token,
		RepoScope:      repoScope,
		GithubAppToken: oauth.GitHubAppUserToken(token),
	}, nil
}

// syncCloudAgentGitCredentials refreshes the git token on every coding
// instance whose credential was pushed from the signed-in account
// (source="account"). Called after a successful GitHub login; instances with
// a manually pasted token are left alone.
func (a *App) syncCloudAgentGitCredentials() {
	cred := a.loadGitHubCredential()
	if cred == nil {
		return
	}
	token := strings.TrimSpace(cred.AccessToken)
	// A login without the repo scope (Repositories unchecked on the
	// authorization page, or the legacy ghu_ GitHub App token) cannot give
	// the instances a working credential — the srv-side clone would fail on
	// every one of them. The settings panel already tells the user to
	// re-login; leave the account-bound instances untouched meanwhile.
	if !oauth.GitHubScopesHaveRepo(cred.Scopes) {
		log.Printf("[cloud-agent] github credential sync skipped: token has no repo scope")
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
