package guiapp

import (
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/oauth"
)

// CloudAgentGitLabAccount is the signed-in GitLab identity shown by the
// cloud repository picker. The PAT deliberately stays in the desktop
// credential store; ConnectCloudAgentGit injects it at the backend boundary.
type CloudAgentGitLabAccount struct {
	Connected bool   `json:"connected"`
	Login     string `json:"login,omitempty"`
	Name      string `json:"name,omitempty"`
	Email     string `json:"email,omitempty"`
	Origin    string `json:"origin,omitempty"`
	// RepoScope is false when the PAT definitely cannot clone or push (its
	// introspected scope list lacks api and write_repository).
	RepoScope bool `json:"repo_scope"`
}

// CloudAgentGitLabAccount exposes the connected GitLab account for the cloud
// coding instance's repository picker. Like the GitHub path this is a local
// credential-store read; repository discovery performs the live API check.
func (a *App) CloudAgentGitLabAccount() (CloudAgentGitLabAccount, error) {
	cred := a.gitlabCredential()
	if cred == nil || strings.TrimSpace(cred.AccessToken) == "" {
		return CloudAgentGitLabAccount{Connected: false}, nil
	}
	login := strings.TrimSpace(cred.Login)
	name := strings.TrimSpace(cred.DisplayName)
	if name == "" {
		name = login
	}
	return CloudAgentGitLabAccount{
		Connected: true,
		Login:     login,
		Name:      name,
		Email:     cloudGitLabCommitEmail(cred),
		Origin:    cloudGitLabDisplayOrigin(cred),
		RepoScope: oauth.GitLabScopesHaveRepo(cred.Scopes),
	}, nil
}

// cloudGitLabDisplayOrigin normalizes older credentials whose BaseURL
// predates the always-normalized connect path. This fallback is display-only;
// token injection and refresh require the stored origin itself to be valid so
// a malformed credential can never become a wildcard or be sent to a guessed host.
func cloudGitLabDisplayOrigin(cred *oauth.StoredCredential) string {
	if cred == nil {
		return ""
	}
	if origin := strings.TrimSpace(cred.BaseURL); origin != "" {
		return origin
	}
	return oauth.GitLabDefaultOrigin
}

// cloudGitLabCommitEmail uses the stored private-commit address ingredients.
// Older connections predate the numeric id, so they fall back to a stable
// noreply-shaped address instead of failing the whole repository flow.
func cloudGitLabCommitEmail(cred *oauth.StoredCredential) string {
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
	host := "gitlab.com"
	if parsed, err := url.Parse(cloudGitLabDisplayOrigin(cred)); err == nil && parsed.Hostname() != "" {
		host = parsed.Hostname()
	}
	if id, err := strconv.ParseInt(strings.TrimSpace(cred.UserID), 10, 64); err == nil && id > 0 {
		return fmt.Sprintf("%d-%s@users.noreply.%s", id, login, host)
	}
	return fmt.Sprintf("%s@users.noreply.%s", login, host)
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
	// An introspected scope list that definitively lacks api and
	// write_repository cannot give the instances a working credential — the
	// srv-side clone would fail on every one of them. (An empty list means
	// the scope could not be read and stays optimistic, like the form check.)
	if !oauth.GitLabScopesHaveRepo(cred.Scopes) {
		log.Printf("[cloud-agent] gitlab credential sync skipped: token has no repository scope")
		return
	}
	accountOrigin := strings.TrimSpace(cred.BaseURL)
	accountHost := hostOfOrigin(accountOrigin)
	if !accountHTTPSRootOrigin(accountOrigin) {
		log.Printf("[cloud-agent] gitlab credential sync skipped: stored origin is not a valid HTTPS root")
		return
	}
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
		if !accountHTTPSRootOrigin(view.Origin) || accountHost == "" || hostOfOrigin(view.Origin) != accountHost {
			continue
		}
		_, err = a.postCloudAgentGit(row.ID, CloudGitConnect{
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
