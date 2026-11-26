package guiapp

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/oauth"
)

// CloudAgentGitHubAccount is the signed-in GitHub identity shown by the
// cloud repository picker. The token deliberately stays in the desktop
// credential store; ConnectCloudAgentGit injects it at the backend boundary.
type CloudAgentGitHubAccount struct {
	Connected bool   `json:"connected"`
	Login     string `json:"login,omitempty"`
	Name      string `json:"name,omitempty"`
	Email     string `json:"email,omitempty"`
	// RepoScope is false when the token cannot read or push private repos
	// (e.g. a GitHub App user token whose scope list comes back empty).
	RepoScope bool `json:"repo_scope"`
	// GithubAppToken is true for a "ghu_" GitHub App user token. Those carry
	// no OAuth scopes by design, so the only fix is a fresh OAuth App login —
	// the form says that instead of pointing at a scope checkbox.
	GithubAppToken bool `json:"github_app_token,omitempty"`
}

// CloudAgentGitHubAccount exposes the connected GitHub account for the cloud
// coding instance's repository picker. This is a local credential-store read:
// the picker must not fail just because a secondary /user refresh was rate
// limited or a proxy returned 403. Repository discovery validates the token
// against the API when it is actually needed.
func (a *App) CloudAgentGitHubAccount() (CloudAgentGitHubAccount, error) {
	cred := a.loadGitHubCredential()
	if cred == nil {
		return CloudAgentGitHubAccount{Connected: false}, nil
	}
	return cloudGitHubAccountFromCredential(cred), nil
}

func cloudGitHubAccountFromCredential(cred *oauth.StoredCredential) CloudAgentGitHubAccount {
	if cred == nil || strings.TrimSpace(cred.AccessToken) == "" {
		return CloudAgentGitHubAccount{Connected: false}
	}
	login := strings.TrimSpace(cred.Login)
	name := strings.TrimSpace(cred.DisplayName)
	if name == "" {
		name = login
	}
	return CloudAgentGitHubAccount{
		Connected:      true,
		Login:          login,
		Name:           name,
		Email:          cloudGitHubCommitEmail(cred),
		RepoScope:      oauth.GitHubScopesHaveRepo(cred.Scopes),
		GithubAppToken: oauth.GitHubAppUserToken(cred.AccessToken),
	}
}

// cloudGitHubCommitEmail prefers GitHub's id-based noreply address. Accounts
// connected before the id was stored still get a deterministic fallback; a
// commit email does not have to be a deliverable mailbox.
func cloudGitHubCommitEmail(cred *oauth.StoredCredential) string {
	if cred == nil {
		return ""
	}
	if email := strings.TrimSpace(cred.Email); email != "" {
		return email
	}
	login := strings.TrimSpace(cred.Login)
	if login == "" {
		return ""
	}
	if id, err := strconv.ParseInt(strings.TrimSpace(cred.UserID), 10, 64); err == nil && id > 0 {
		return fmt.Sprintf("%d+%s@users.noreply.github.com", id, login)
	}
	return login + "@users.noreply.github.com"
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
		if host := hostOfOrigin(view.Origin); !accountHTTPSRootOrigin(view.Origin) || (host != "github.com" && host != "www.github.com") {
			continue
		}
		_, err = a.postCloudAgentGit(row.ID, CloudGitConnect{
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
