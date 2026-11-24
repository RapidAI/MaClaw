package guiapp

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/oauth"
)

// CloudAgentGitLabAccount is the signed-in GitLab credential view the cloud
// git form needs to prefill itself. Token is included so the form can submit
// it straight to maclawsrv; it never crosses the webview boundary otherwise.
type CloudAgentGitLabAccount struct {
	Connected bool   `json:"connected"`
	Login     string `json:"login,omitempty"`
	Name      string `json:"name,omitempty"`
	Email     string `json:"email,omitempty"`
	Origin    string `json:"origin,omitempty"`
	Token     string `json:"token,omitempty"`
	// RepoScope is false when the PAT definitely cannot clone or push (its
	// introspected scope list lacks api and write_repository).
	RepoScope bool `json:"repo_scope"`
}

// CloudAgentGitLabAccount exposes the connected GitLab account for the cloud
// coding instance's git form. The caller must still click connect — nothing
// is pushed to maclawsrv from here.
func (a *App) CloudAgentGitLabAccount() (CloudAgentGitLabAccount, error) {
	cred := a.gitlabCredential()
	if cred == nil || strings.TrimSpace(cred.AccessToken) == "" {
		return CloudAgentGitLabAccount{Connected: false}, nil
	}
	if cfg, err := a.LoadConfig(); err == nil {
		oauth.ApplyProxyFromAppConfig(cfg)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	user, err := oauth.FetchGitLabUser(ctx, cred.BaseURL, strings.TrimSpace(cred.AccessToken))
	if err != nil {
		return CloudAgentGitLabAccount{}, fmt.Errorf("GitLab 账号信息获取失败: %w", err)
	}
	name := strings.TrimSpace(user.Name)
	if name == "" {
		name = user.Username
	}
	email := strings.TrimSpace(user.Email)
	if email == "" {
		// GitLab's private commit address is <id>-<username>@noreply.<host>.
		host := "gitlab.com"
		if parsed, perr := url.Parse(cred.BaseURL); perr == nil && parsed.Host != "" {
			host = parsed.Host
		}
		email = fmt.Sprintf("%d-%s@noreply.%s", user.ID, user.Username, host)
	}
	return CloudAgentGitLabAccount{
		Connected: true,
		Login:     user.Username,
		Name:      name,
		Email:     email,
		Origin:    cred.BaseURL,
		Token:     strings.TrimSpace(cred.AccessToken),
		RepoScope: oauth.GitLabScopesHaveRepo(cred.Scopes),
	}, nil
}

// syncCloudAgentGitLabCredentials refreshes the git token on every coding
// instance whose credential was pushed from the signed-in GitLab account
// (source="account"). Called after a successful GitLab connect; instances
// with a manually pasted token — or a repo on a different GitLab host than
// the account — are left alone.
func (a *App) syncCloudAgentGitLabCredentials() {
	cred := a.gitlabCredential()
	if cred == nil || strings.TrimSpace(cred.AccessToken) == "" {
		return
	}
	token := strings.TrimSpace(cred.AccessToken)
	accountHost := hostOfOrigin(cred.BaseURL)
	list, err := a.ListCloudAgents()
	if err != nil {
		log.Printf("[cloud-agent] gitlab credential sync list failed: %v", err)
		return
	}
	refreshed := 0
	for _, row := range list.Instances {
		if row.Kind != "coding" {
			continue
		}
		view, err := a.GetCloudAgentGit(row.ID)
		if err != nil {
			log.Printf("[cloud-agent] gitlab credential sync read %s: %v", row.ID, err)
			continue
		}
		if !view.Connected || view.Source != "account" || view.Service != "gitlab" {
			continue
		}
		// A PAT only works on the host it was minted on — skip instances
		// bound to a different GitLab origin than the signed-in account.
		if accountHost != "" && hostOfOrigin(view.Origin) != accountHost {
			continue
		}
		_, err = a.ConnectCloudAgentGit(row.ID, CloudGitConnect{
			Service:     "gitlab",
			Origin:      view.Origin,
			Repo:        view.Repo,
			Ref:         view.Ref,
			Token:       token,
			AuthorName:  view.AuthorName,
			AuthorEmail: view.AuthorEmail,
			Source:      "account",
		})
		if err != nil {
			log.Printf("[cloud-agent] gitlab credential sync %s: %v", row.ID, err)
			continue
		}
		refreshed++
	}
	if refreshed > 0 {
		log.Printf("[cloud-agent] gitlab credential sync refreshed %d instance(s)", refreshed)
	}
}

// hostOfOrigin reduces an origin URL to its host for the same-account-host
// comparison; an unparseable input returns "".
func hostOfOrigin(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Host)
}
