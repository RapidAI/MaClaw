package guiapp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/oauth"
	"github.com/zalando/go-keyring"
)

// ─────────────────────────────────────────────────────────────────────────────
// Git platform repository creation, shared by the cloud-agent connect dialog
// and the virtual-repository mapping dialog. Everything runs on the DESKTOP
// against the provider REST APIs with a credential the user picked; the token
// never crosses the webview boundary — bindings reference credentials by id
// and resolve the secret from the settings account or the virtual-repository
// credential store at this boundary.
// ─────────────────────────────────────────────────────────────────────────────

// gitPlatformAPI bundles the provider REST calls behind injectable function
// vars. Production points at the pinned oauth helpers; tests point them at
// httptest servers (the oauth package keeps its GitHub base host-pinned and
// reachable for tests only through its own private apiBase variants).
type gitPlatformAPIBundle struct {
	githubUser       func(ctx context.Context, token string) (string, error)
	githubOrgs       func(ctx context.Context, token string, limit int) ([]oauth.GitHubOrg, error)
	githubNameStatus func(ctx context.Context, token, owner, name string) (bool, string, error)
	githubCreate     func(ctx context.Context, token, owner, name string, private, autoInit bool) (*oauth.GitHubCreatedRepo, error)
	gitlabUser       func(ctx context.Context, origin, token string) (string, error)
	gitlabNamespace  func(ctx context.Context, origin, token string, limit int) ([]oauth.GitLabNamespace, error)
	gitlabNameStatus func(ctx context.Context, origin, token, fullPath string) (bool, string, error)
	gitlabCreate     func(ctx context.Context, origin, token, name, path string, namespaceID int64, private, initReadme bool) (*oauth.GitLabCreatedProject, error)
}

var gitPlatformAPI = gitPlatformAPIBundle{
	githubUser: func(ctx context.Context, token string) (string, error) {
		user, _, err := oauth.FetchGitHubUser(ctx, token)
		if err != nil {
			return "", err
		}
		return user.Login, nil
	},
	githubOrgs:       oauth.FetchGitHubOrgs,
	githubNameStatus: oauth.GitHubRepoNameStatus,
	githubCreate:     oauth.CreateGitHubRepo,
	gitlabUser: func(ctx context.Context, origin, token string) (string, error) {
		user, err := oauth.FetchGitLabUser(ctx, origin, token)
		if err != nil {
			return "", err
		}
		return user.Username, nil
	},
	gitlabNamespace:  oauth.FetchGitLabNamespaces,
	gitlabNameStatus: oauth.GitLabProjectNameStatus,
	gitlabCreate:     oauth.CreateGitLabProject,
}

// gitPlatformProbeLogin validates a provider-tagged secret against the
// provider's own /user endpoint and returns the login it belongs to. It runs
// on every provider-tagged credential save, so a broken token fails at save
// time with a readable message instead of inside a later create call.
func (a *App) gitPlatformProbeLogin(provider, origin, secret string) (string, error) {
	ctx, cancel := a.gitPlatformContext()
	defer cancel()
	switch provider {
	case "github":
		login, err := gitPlatformAPI.githubUser(ctx, secret)
		if err != nil {
			return "", fmt.Errorf("GitHub 令牌校验失败: %w", err)
		}
		return login, nil
	case "gitlab":
		login, err := gitPlatformAPI.gitlabUser(ctx, origin, secret)
		if err != nil {
			return "", fmt.Errorf("GitLab 令牌校验失败: %w", err)
		}
		return login, nil
	default:
		return "", fmt.Errorf("unknown git platform %q", provider)
	}
}

// gitPlatformContext is the shared per-call API budget plus the proxy wiring
// the other provider-backed flows (repository listing) apply as well.
func (a *App) gitPlatformContext() (context.Context, context.CancelFunc) {
	if cfg, err := a.LoadConfig(); err == nil {
		oauth.ApplyProxyFromAppConfig(cfg)
	}
	return context.WithTimeout(context.Background(), 20*time.Second)
}

// GitPlatformCredentialView is one credential the creation flow may use. The
// webview receives labels only — the ID is the only handle that travels back.
type GitPlatformCredentialView struct {
	// ID is "" for the settings account; a vcred_ id for a virtual-repository
	// credential. The empty id is the default pick.
	ID string `json:"id"`
	// Kind is "account" for the settings account and "credential" for a
	// virtual-repository row. Provider mirrors the credential's platform tag
	// ("" = generic) so the picker can label legacy rows honestly.
	Kind     string `json:"kind"`
	Provider string `json:"provider,omitempty"`
	Name     string `json:"name,omitempty"`
	Login    string `json:"login,omitempty"`
	Origin   string `json:"origin,omitempty"` // gitlab self-hosted site root
}

// GitPlatformNamespaceView is one namespace the chosen credential can target.
type GitPlatformNamespaceView struct {
	// Key is the owner used in repository paths: the login (GitHub) or the
	// namespace full path (GitLab).
	Key         string `json:"key"`
	Label       string `json:"label"`
	Kind        string `json:"kind"` // "personal" | "org" | "group"
	NamespaceID int64  `json:"namespace_id,omitempty"`
}

// GitPlatformNamespaceList is the GitPlatformNamespaces answer: usable rows
// plus an optional warning explaining a partial GitHub list. The personal
// namespace stays usable when the org enumeration fails, but the picker must
// say WHY orgs are missing instead of letting silence read as "no
// organizations".
type GitPlatformNamespaceList struct {
	Rows []GitPlatformNamespaceView `json:"rows"`
	// OrgWarning carries the failed GitHub org listing's error text. Empty
	// when the list is complete (always empty for GitLab, whose namespace
	// fetch fails hard instead of degrading).
	OrgWarning string `json:"org_warning,omitempty"`
}

// GitPlatformNameStatus is the availability-check answer. A taken or invalid
// name is a valid answer, not an error; API failures return a Go error so a
// broken probe never reads as "available".
type GitPlatformNameStatus struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// GitPlatformCreateRepoInput selects where and what to create.
type GitPlatformCreateRepoInput struct {
	Service      string `json:"service"`
	CredentialID string `json:"credential_id,omitempty"`
	// Owner is the namespace Key from GitPlatformNamespaces.
	Owner string `json:"owner"`
	// NamespaceID is required for GitLab (from the namespace view).
	NamespaceID int64 `json:"namespace_id,omitempty"`
	// NamespaceKind is "personal" for the account's own namespace and
	// "org"/"group" otherwise; GitHub switches between /user/repos and
	// /orgs/{org}/repos on it.
	NamespaceKind string `json:"namespace_kind,omitempty"`
	Name          string `json:"name"`
	Private       bool   `json:"private"`
	// InitReadme seeds an initial commit so the fresh repository has a real
	// default branch for the first clone and push.
	InitReadme bool `json:"init_readme"`
}

// GitPlatformCreatedRepo is the created repository surface for the UI.
type GitPlatformCreatedRepo struct {
	FullName      string `json:"full_name"`
	WebURL        string `json:"web_url,omitempty"`
	CloneURL      string `json:"clone_url"`
	DefaultBranch string `json:"default_branch,omitempty"`
	Private       bool   `json:"private"`
}

// gitPlatformResolved is the credential the create/status calls run with.
type gitPlatformResolved struct {
	service  string
	token    string
	origin   string // github: https://github.com; gitlab: the site root
	username string // provider login or the credential's username
}

// gitPlatformResolveCredential resolves the credential reference used by the
// creation flows. credentialID "" selects the settings account (GitHub needs
// the repo scope; GitLab needs the api scope — write_repository cannot create
// projects); a vcred_ id selects a git-kind virtual-repository credential
// whose provider (and GitLab origin) matches the service. GitHub also accepts
// a legacy provider-agnostic credential because its API host is fixed; GitLab
// cannot — the API origin must come from the credential itself.
func (a *App) gitPlatformResolveCredential(service, credentialID string) (*gitPlatformResolved, error) {
	key := strings.ToLower(strings.TrimSpace(service))
	if key != "github" && key != "gitlab" {
		return nil, fmt.Errorf("代码平台只支持 GitHub 或 GitLab")
	}
	credentialID = strings.TrimSpace(credentialID)
	if credentialID == "" {
		switch key {
		case "github":
			cred := a.loadGitHubCredential()
			if cred == nil || strings.TrimSpace(cred.AccessToken) == "" {
				return nil, fmt.Errorf("请先在设置中连接 GitHub")
			}
			if !oauth.GitHubScopesHaveRepo(cred.Scopes) {
				return nil, fmt.Errorf("当前 GitHub 连接没有仓库权限，请在设置中断开 GitHub 后重新登录")
			}
			return &gitPlatformResolved{
				service:  "github",
				token:    strings.TrimSpace(cred.AccessToken),
				origin:   "https://github.com",
				username: strings.TrimSpace(cred.Login),
			}, nil
		case "gitlab":
			cred := a.gitlabCredential()
			if cred == nil || strings.TrimSpace(cred.AccessToken) == "" {
				return nil, fmt.Errorf("请先在设置中连接 GitLab")
			}
			if !oauth.GitLabScopesHaveCreate(cred.Scopes) {
				return nil, fmt.Errorf("当前 GitLab 令牌没有 api 权限，无法创建仓库；请在设置中改用含 api 权限的令牌")
			}
			origin := strings.TrimSpace(cred.BaseURL)
			if !accountHTTPSRootOrigin(origin) {
				return nil, fmt.Errorf("GitLab 连接缺少有效的 HTTPS 站点地址，请在设置中重新连接 GitLab")
			}
			return &gitPlatformResolved{
				service:  "gitlab",
				token:    strings.TrimSpace(cred.AccessToken),
				origin:   origin,
				username: strings.TrimSpace(cred.Login),
			}, nil
		}
	}
	file, err := a.loadRepositoryCredentials()
	if err != nil {
		return nil, err
	}
	for _, item := range file.Items {
		if item.ID != credentialID {
			continue
		}
		if item.Kind != "git" {
			return nil, fmt.Errorf("所选凭据不是 Git 凭据")
		}
		provider := strings.ToLower(strings.TrimSpace(item.Provider))
		if key == "github" && provider != "" && provider != "github" {
			return nil, fmt.Errorf("所选凭据不是 GitHub 凭据")
		}
		if key == "gitlab" && provider != "gitlab" {
			return nil, fmt.Errorf("所选凭据不是 GitLab 凭据")
		}
		origin := "https://github.com"
		if key == "gitlab" {
			origin, err = oauth.NormalizeGitLabOrigin(item.Origin)
			if err != nil {
				return nil, fmt.Errorf("所选凭据缺少有效的 GitLab 站点地址: %w", err)
			}
		}
		token, err := keyring.Get(virtualRepositoryKeyringService, item.ID)
		if err != nil {
			return nil, fmt.Errorf("读取凭据密文失败，请在凭据管理中重新输入该令牌: %w", err)
		}
		if strings.TrimSpace(token) == "" {
			return nil, fmt.Errorf("所选凭据的令牌为空，请在凭据管理中重新输入")
		}
		return &gitPlatformResolved{
			service:  key,
			token:    token,
			origin:   origin,
			username: strings.TrimSpace(item.Username),
		}, nil
	}
	return nil, fmt.Errorf("所选凭据不存在")
}

// normalizeGitPlatformService validates the service selector shared by the
// creation bindings.
func normalizeGitPlatformService(service string) (string, error) {
	key := strings.ToLower(strings.TrimSpace(service))
	if key != "github" && key != "gitlab" {
		return "", fmt.Errorf("代码平台只支持 GitHub 或 GitLab")
	}
	return key, nil
}

// GitPlatformCreateCredentials lists the credentials the creation flow may
// use for service. The settings account comes first (it is the default pick);
// provider-tagged virtual-repository credentials follow. Legacy
// provider-agnostic git credentials are offered to GitHub only — its API host
// is fixed, so the token's platform surfaces on first use.
func (a *App) GitPlatformCreateCredentials(service string) ([]GitPlatformCredentialView, error) {
	key, err := normalizeGitPlatformService(service)
	if err != nil {
		return nil, err
	}
	out := []GitPlatformCredentialView{}
	switch key {
	case "github":
		if cred := a.loadGitHubCredential(); cred != nil && strings.TrimSpace(cred.AccessToken) != "" && oauth.GitHubScopesHaveRepo(cred.Scopes) {
			account := cloudGitHubAccountFromCredential(cred)
			out = append(out, GitPlatformCredentialView{Kind: "account", Name: account.Name, Login: account.Login})
		}
	case "gitlab":
		if cred := a.gitlabCredential(); cred != nil && strings.TrimSpace(cred.AccessToken) != "" && oauth.GitLabScopesHaveCreate(cred.Scopes) {
			origin := strings.TrimSpace(cred.BaseURL)
			if accountHTTPSRootOrigin(origin) {
				name := strings.TrimSpace(cred.DisplayName)
				if name == "" {
					name = strings.TrimSpace(cred.Login)
				}
				out = append(out, GitPlatformCredentialView{Kind: "account", Name: name, Login: strings.TrimSpace(cred.Login), Origin: origin})
			}
		}
	}
	file, err := a.loadRepositoryCredentials()
	if err != nil {
		return nil, err
	}
	for _, item := range file.Items {
		if item.Kind != "git" {
			continue
		}
		provider := strings.ToLower(strings.TrimSpace(item.Provider))
		switch key {
		case "github":
			if provider != "" && provider != "github" {
				continue
			}
		case "gitlab":
			if provider != "gitlab" {
				continue
			}
		}
		out = append(out, GitPlatformCredentialView{
			ID:       item.ID,
			Kind:     "credential",
			Provider: provider,
			Name:     item.Name,
			Login:    item.Username,
			Origin:   item.Origin,
		})
	}
	return out, nil
}

// GitPlatformNamespaces lists the namespaces (personal + orgs/groups) the
// chosen credential can target. The GitHub /user probe doubles as the token
// freshness check — a revoked token fails here with a readable message
// instead of at create time.
func (a *App) GitPlatformNamespaces(service, credentialID string) (GitPlatformNamespaceList, error) {
	key, err := normalizeGitPlatformService(service)
	if err != nil {
		return GitPlatformNamespaceList{}, err
	}
	resolved, err := a.gitPlatformResolveCredential(key, credentialID)
	if err != nil {
		return GitPlatformNamespaceList{}, err
	}
	ctx, cancel := a.gitPlatformContext()
	defer cancel()
	out := GitPlatformNamespaceList{Rows: []GitPlatformNamespaceView{}}
	switch key {
	case "github":
		// The /user probe ALWAYS runs: it authoritatively resolves the login
		// (a stored username can be stale or user-typed) and doubles as the
		// token freshness check — swallowing a revoked-token failure here
		// would leave a personal-only list with the root cause invisible
		// until create time.
		login, err := gitPlatformAPI.githubUser(ctx, resolved.token)
		if err != nil {
			return GitPlatformNamespaceList{}, fmt.Errorf("GitHub 账号读取失败: %w", err)
		}
		out.Rows = append(out.Rows, GitPlatformNamespaceView{Key: login, Label: login, Kind: "personal"})
		orgs, err := gitPlatformAPI.githubOrgs(ctx, resolved.token, 100)
		if err != nil {
			// The personal namespace stays usable even when the org listing
			// fails (missing membership, quota hiccup); the answer carries
			// the failure text so the picker explains the partial list
			// instead of logging it away.
			log.Printf("[git-platform] %s org list failed: %v", key, err)
			out.OrgWarning = err.Error()
			return out, nil
		}
		for _, org := range orgs {
			out.Rows = append(out.Rows, GitPlatformNamespaceView{Key: org.Login, Label: org.Login, Kind: "org"})
		}
	case "gitlab":
		namespaces, err := gitPlatformAPI.gitlabNamespace(ctx, resolved.origin, resolved.token, 100)
		if err != nil {
			return GitPlatformNamespaceList{}, fmt.Errorf("GitLab 归属空间读取失败: %w", err)
		}
		for _, ns := range namespaces {
			kind := "group"
			if strings.EqualFold(ns.Kind, "user") {
				kind = "personal"
			}
			out.Rows = append(out.Rows, GitPlatformNamespaceView{Key: ns.FullPath, Label: ns.Name, Kind: kind, NamespaceID: ns.ID})
		}
	}
	return out, nil
}

// GitPlatformRepoNameStatus probes whether owner/name can still be created
// with the chosen credential. Local shape errors return Available=false with
// a reason and no error; remote "taken" is likewise a valid answer. Only an
// API failure (quota, revoked token, network) returns an error.
func (a *App) GitPlatformRepoNameStatus(service, credentialID, owner, name string) (GitPlatformNameStatus, error) {
	key, err := normalizeGitPlatformService(service)
	if err != nil {
		return GitPlatformNameStatus{}, err
	}
	owner = strings.TrimSpace(owner)
	name = strings.TrimSpace(name)
	if !oauth.GitHubRepoNameShape(owner) || !oauth.GitHubRepoNameShape(name) {
		return GitPlatformNameStatus{Available: false, Reason: "归属或仓库名格式无效"}, nil
	}
	resolved, err := a.gitPlatformResolveCredential(key, credentialID)
	if err != nil {
		return GitPlatformNameStatus{}, err
	}
	ctx, cancel := a.gitPlatformContext()
	defer cancel()
	switch key {
	case "github":
		available, reason, err := gitPlatformAPI.githubNameStatus(ctx, resolved.token, owner, name)
		if err != nil {
			return GitPlatformNameStatus{}, fmt.Errorf("GitHub 仓库名检查失败: %w", err)
		}
		return GitPlatformNameStatus{Available: available, Reason: reason}, nil
	case "gitlab":
		available, reason, err := gitPlatformAPI.gitlabNameStatus(ctx, resolved.origin, resolved.token, owner+"/"+name)
		if err != nil {
			return GitPlatformNameStatus{}, fmt.Errorf("GitLab 仓库名检查失败: %w", err)
		}
		return GitPlatformNameStatus{Available: available, Reason: reason}, nil
	}
	return GitPlatformNameStatus{}, fmt.Errorf("代码平台只支持 GitHub 或 GitLab")
}

// GitPlatformCreateRepo creates the repository under the chosen namespace,
// then busts the connect-dialog repository cache so the new repository is
// immediately pickable. Binding is a separate, explicit step per surface.
func (a *App) GitPlatformCreateRepo(input GitPlatformCreateRepoInput) (GitPlatformCreatedRepo, error) {
	key, err := normalizeGitPlatformService(input.Service)
	if err != nil {
		return GitPlatformCreatedRepo{}, err
	}
	name := strings.TrimSpace(input.Name)
	if !oauth.GitHubRepoNameShape(name) {
		return GitPlatformCreatedRepo{}, fmt.Errorf("仓库名格式无效")
	}
	resolved, err := a.gitPlatformResolveCredential(key, input.CredentialID)
	if err != nil {
		return GitPlatformCreatedRepo{}, err
	}
	ctx, cancel := a.gitPlatformContext()
	defer cancel()
	var out GitPlatformCreatedRepo
	switch key {
	case "github":
		owner := strings.TrimSpace(input.Owner)
		// Personal targets POST /user/repos; anything else POSTs
		// /orgs/{org}/repos, which 404s when the owner is a plain user. The
		// namespace kind normally comes from GitPlatformNamespaces, but an
		// empty kind (or an owner equal to the account's own login) falls
		// back to the personal endpoint.
		if strings.EqualFold(input.NamespaceKind, "personal") || (input.NamespaceKind == "" && strings.EqualFold(owner, resolved.username)) {
			owner = ""
		}
		created, err := gitPlatformAPI.githubCreate(ctx, resolved.token, owner, name, input.Private, input.InitReadme)
		if err != nil {
			return GitPlatformCreatedRepo{}, fmt.Errorf("GitHub 仓库创建失败: %w", err)
		}
		out = GitPlatformCreatedRepo{
			FullName:      created.FullName,
			WebURL:        created.HTMLURL,
			CloneURL:      created.CloneURL,
			DefaultBranch: created.DefaultBranch,
			Private:       created.Private,
		}
		if out.CloneURL == "" && out.FullName != "" {
			out.CloneURL = "https://github.com/" + out.FullName + ".git"
		}
	case "gitlab":
		owner := strings.TrimSpace(input.Owner)
		if !oauth.GitHubRepoNameShape(owner) || input.NamespaceID <= 0 {
			return GitPlatformCreatedRepo{}, fmt.Errorf("缺少有效的归属空间")
		}
		created, err := gitPlatformAPI.gitlabCreate(ctx, resolved.origin, resolved.token, name, name, input.NamespaceID, input.Private, input.InitReadme)
		if err != nil {
			return GitPlatformCreatedRepo{}, fmt.Errorf("GitLab 仓库创建失败: %w", err)
		}
		out = GitPlatformCreatedRepo{
			FullName:      created.PathWithNamespace,
			WebURL:        created.WebURL,
			CloneURL:      created.HTTPURLToRepo,
			DefaultBranch: created.DefaultBranch,
			Private:       !strings.EqualFold(created.Visibility, "public"),
		}
		if out.CloneURL == "" && out.FullName != "" {
			out.CloneURL = resolved.origin + "/" + out.FullName + ".git"
		}
	default:
		return GitPlatformCreatedRepo{}, fmt.Errorf("代码平台只支持 GitHub 或 GitLab")
	}
	a.cloudAgentRepos.invalidate(key)
	return out, nil
}

// ImportGitPlatformAccountCredential snapshots the settings account token
// into the virtual-repository credential store, so a repository created with
// the settings account can still be bound to a virtual-repository mapping
// (whose bindings resolve secrets from that store only). GitHub device-flow
// tokens and GitLab PATs do not rotate automatically, so the snapshot stays
// valid until the user revokes or re-mints it. An equivalent credential is
// reused instead of duplicated.
func (a *App) ImportGitPlatformAccountCredential(service string) (RepositoryCredentialMetadata, error) {
	key, err := normalizeGitPlatformService(service)
	if err != nil {
		return RepositoryCredentialMetadata{}, err
	}
	resolved, err := a.gitPlatformResolveCredential(key, "")
	if err != nil {
		return RepositoryCredentialMetadata{}, err
	}
	file, err := a.loadRepositoryCredentials()
	if err != nil {
		return RepositoryCredentialMetadata{}, err
	}
	for _, item := range file.Items {
		if item.Kind != "git" || strings.ToLower(strings.TrimSpace(item.Provider)) != key || !strings.EqualFold(strings.TrimSpace(item.Username), resolved.username) {
			continue
		}
		if key == "gitlab" && hostOfOrigin(item.Origin) != hostOfOrigin(resolved.origin) {
			continue
		}
		return item, nil
	}
	name := "GitHub 账号 @" + resolved.username
	origin := ""
	if key == "gitlab" {
		name = "GitLab 账号 @" + resolved.username
		origin = resolved.origin
	}
	input, err := json.Marshal(saveRepositoryCredentialInput{
		Name:     name,
		Kind:     "git",
		Username: resolved.username,
		Provider: key,
		Origin:   origin,
		Secret:   resolved.token,
	})
	if err != nil {
		return RepositoryCredentialMetadata{}, err
	}
	saved, err := a.SaveRepositoryCredential(string(input))
	if err != nil {
		return RepositoryCredentialMetadata{}, err
	}
	var item RepositoryCredentialMetadata
	if err := json.Unmarshal([]byte(saved), &item); err != nil {
		return RepositoryCredentialMetadata{}, err
	}
	log.Printf("[git-platform] imported settings %s account as repository credential %s", key, item.ID)
	return item, nil
}
