package guiapp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/oauth"
	"github.com/zalando/go-keyring"
)

// withGitPlatformAPITest replaces the injected provider bundle for the test's
// lifetime. Fields left nil keep the production (or previously overridden)
// implementation, so each test stubs only the calls it exercises.
func withGitPlatformAPITest(t *testing.T, override gitPlatformAPIBundle) {
	t.Helper()
	prev := gitPlatformAPI
	merged := prev
	if override.githubUser != nil {
		merged.githubUser = override.githubUser
	}
	if override.githubOrgs != nil {
		merged.githubOrgs = override.githubOrgs
	}
	if override.githubNameStatus != nil {
		merged.githubNameStatus = override.githubNameStatus
	}
	if override.githubCreate != nil {
		merged.githubCreate = override.githubCreate
	}
	if override.gitlabUser != nil {
		merged.gitlabUser = override.gitlabUser
	}
	if override.gitlabNamespace != nil {
		merged.gitlabNamespace = override.gitlabNamespace
	}
	if override.gitlabNameStatus != nil {
		merged.gitlabNameStatus = override.gitlabNameStatus
	}
	if override.gitlabCreate != nil {
		merged.gitlabCreate = override.gitlabCreate
	}
	gitPlatformAPI = merged
	t.Cleanup(func() { gitPlatformAPI = prev })
}

func newGitPlatformTestApp(t *testing.T, githubScopes, gitlabScopes string) *App {
	t.Helper()
	keyring.MockInit()
	app := &App{testHomeDir: t.TempDir()}
	store := oauth.NewFileCredentialStore(filepath.Join(app.testHomeDir, "credentials.json"))
	app.credentialStore = store
	if githubScopes != "-" {
		if err := store.Modify(oauth.GitHubAccountProviderID, func(_ *oauth.StoredCredential) (*oauth.StoredCredential, error) {
			return &oauth.StoredCredential{Type: "oauth", AccessToken: "gho_settings", Login: "ada", DisplayName: "Ada", Email: "ada@example.com", Scopes: githubScopes}, nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if gitlabScopes != "-" {
		if err := store.Modify(oauth.GitLabAccountProviderID, func(_ *oauth.StoredCredential) (*oauth.StoredCredential, error) {
			return &oauth.StoredCredential{Type: "pat", AccessToken: "glpat_settings", Login: "ada", BaseURL: "https://gitlab.example.com", Scopes: gitlabScopes}, nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	return app
}

func mustSaveVRepoCredential(t *testing.T, app *App, input string) RepositoryCredentialMetadata {
	t.Helper()
	raw, err := app.SaveRepositoryCredential(input)
	if err != nil {
		t.Fatalf("save credential: %v", err)
	}
	var item RepositoryCredentialMetadata
	if err := json.Unmarshal([]byte(raw), &item); err != nil {
		t.Fatal(err)
	}
	return item
}

func TestGitPlatformCreateCredentialsFiltersByProvider(t *testing.T) {
	app := newGitPlatformTestApp(t, "repo", "api")
	// Install the provider stubs BEFORE saving provider-tagged credentials:
	// the save path probes the token against the provider, and the probe
	// must never reach the real API from a test.
	withGitPlatformAPITest(t, gitPlatformAPIBundle{
		githubUser: func(_ context.Context, _ string) (string, error) { return "ada", nil },
		gitlabUser: func(_ context.Context, _, _ string) (string, error) { return "ada", nil },
	})
	// A provider-tagged gitlab credential plus a legacy generic one; an
	// svn-kind and a github-tagged one that must not reach the gitlab list.
	mustSaveVRepoCredential(t, app, `{"name":"Work GitLab","kind":"git","provider":"gitlab","origin":"https://gitlab.example.com","secret":"glpat-work"}`)
	mustSaveVRepoCredential(t, app, `{"name":"Generic","kind":"git","username":"generic","secret":"generic"}`)
	mustSaveVRepoCredential(t, app, `{"name":"GH PAT","kind":"git","provider":"github","secret":"ghp_pat"}`)

	githubList, err := app.GitPlatformCreateCredentials("github")
	if err != nil {
		t.Fatal(err)
	}
	if len(githubList) != 3 {
		t.Fatalf("github list = %+v", githubList)
	}
	if githubList[0].Kind != "account" || githubList[0].ID != "" || githubList[0].Login != "ada" {
		t.Fatalf("settings account first: %+v", githubList[0])
	}
	if githubList[0].Name != "Ada" || strings.Contains(githubList[0].Name, "secret") {
		t.Fatalf("account view carries unexpected material: %+v", githubList[0])
	}
	ids := map[string]bool{}
	for _, view := range githubList {
		if view.ID != "" {
			ids[view.ID] = true
		}
	}
	if len(ids) != 2 {
		t.Fatalf("expected generic + github credentials, got %+v", githubList)
	}

	gitlabList, err := app.GitPlatformCreateCredentials("gitlab")
	if err != nil {
		t.Fatal(err)
	}
	if len(gitlabList) != 2 {
		t.Fatalf("gitlab list = %+v", gitlabList)
	}
	if gitlabList[0].Kind != "account" || gitlabList[0].Origin != "https://gitlab.example.com" {
		t.Fatalf("gitlab account view = %+v", gitlabList[0])
	}
	if gitlabList[1].Kind != "credential" || gitlabList[1].Name != "Work GitLab" {
		t.Fatalf("gitlab credential view = %+v", gitlabList[1])
	}

	// A service the flow does not support is a local rejection.
	if _, err := app.GitPlatformCreateCredentials("gitea"); err == nil {
		t.Fatal("gitea accepted")
	}
}

func TestGitPlatformCreateCredentialsScopeGates(t *testing.T) {
	// GitHub settings account without the repo scope is excluded from the
	// create list even though it stays connected.
	app := newGitPlatformTestApp(t, "read:user", "-")
	list, err := app.GitPlatformCreateCredentials("github")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("scopeless account offered: %+v", list)
	}

	// GitLab's write_repository can clone and push but NOT create projects;
	// only the api scope (or an unreadable scope list) makes the settings
	// account a create candidate.
	app = newGitPlatformTestApp(t, "-", "write_repository,read_user")
	list, err = app.GitPlatformCreateCredentials("gitlab")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("write_repository account offered for create: %+v", list)
	}
	app = newGitPlatformTestApp(t, "-", "api")
	list, err = app.GitPlatformCreateCredentials("gitlab")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Kind != "account" {
		t.Fatalf("api account missing: %+v", list)
	}
}

func TestGitPlatformResolveCredential(t *testing.T) {
	app := newGitPlatformTestApp(t, "repo", "api")
	withGitPlatformAPITest(t, gitPlatformAPIBundle{
		githubUser: func(_ context.Context, _ string) (string, error) { return "ada", nil },
		gitlabUser: func(_ context.Context, _, _ string) (string, error) { return "ada", nil },
	})
	cred := mustSaveVRepoCredential(t, app, `{"name":"GH PAT","kind":"git","provider":"github","secret":"ghp_resolved"}`)

	// The settings account path (empty id).
	resolved, err := app.gitPlatformResolveCredential("github", "")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.token != "gho_settings" || resolved.origin != "https://github.com" || resolved.username != "ada" {
		t.Fatalf("account resolve = %+v", resolved)
	}

	// The credential path reads the secret from the keyring only. The
	// provider-tagged save probed the token, so its username was auto-filled
	// with the login the secret belongs to.
	resolved, err = app.gitPlatformResolveCredential("github", cred.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.token != "ghp_resolved" || resolved.username != "ada" {
		t.Fatalf("credential resolve = %+v", resolved)
	}

	// A provider mismatch is refused before any secret is read.
	gitlabCred := mustSaveVRepoCredential(t, app, `{"name":"Work GitLab","kind":"git","provider":"gitlab","origin":"https://gitlab.example.com","secret":"glpat-work"}`)
	if _, err := app.gitPlatformResolveCredential("github", gitlabCred.ID); err == nil {
		t.Fatal("gitlab credential accepted for github")
	}
	if _, err := app.gitPlatformResolveCredential("gitlab", cred.ID); err == nil {
		t.Fatal("github credential accepted for gitlab")
	}
	// Unknown ids fail closed.
	if _, err := app.gitPlatformResolveCredential("github", "vcred_missing"); err == nil {
		t.Fatal("missing credential accepted")
	}
}

func TestGitPlatformNamespacesGitHub(t *testing.T) {
	app := newGitPlatformTestApp(t, "repo", "-")
	var probedToken string
	withGitPlatformAPITest(t, gitPlatformAPIBundle{
		githubUser: func(_ context.Context, token string) (string, error) {
			probedToken = token
			return "ada", nil
		},
		githubOrgs: func(_ context.Context, _ string, _ int) ([]oauth.GitHubOrg, error) {
			return []oauth.GitHubOrg{{Login: "acme"}, {Login: "octo"}}, nil
		},
	})

	// The /user probe runs even though the account carries a cached login:
	// it authoritatively resolves the owner and doubles as the token
	// freshness check, so a revoked token fails here with a readable
	// message instead of behind a swallowed org error.
	list, err := app.GitPlatformNamespaces("github", "")
	if err != nil {
		t.Fatal(err)
	}
	if probedToken != "gho_settings" {
		t.Fatalf("probe used token %q", probedToken)
	}
	if len(list.Rows) != 3 || list.Rows[0].Kind != "personal" || list.Rows[0].Key != "ada" || list.Rows[1].Kind != "org" || list.Rows[1].Key != "acme" {
		t.Fatalf("rows = %+v", list.Rows)
	}
	if list.OrgWarning != "" {
		t.Fatalf("complete list carried a warning: %q", list.OrgWarning)
	}

	// A failed org listing keeps the personal namespace usable AND explains
	// the partial list: creation in the account's own namespace must not die
	// with the org enumeration, and silence must not read as "no orgs".
	withGitPlatformAPITest(t, gitPlatformAPIBundle{
		githubOrgs: func(_ context.Context, _ string, _ int) ([]oauth.GitHubOrg, error) {
			return nil, context.DeadlineExceeded
		},
	})
	list, err = app.GitPlatformNamespaces("github", "")
	if err != nil || len(list.Rows) != 1 || list.Rows[0].Kind != "personal" {
		t.Fatalf("org failure killed personal namespace: %v %+v", err, list.Rows)
	}
	if list.OrgWarning == "" {
		t.Fatal("partial list carried no org warning")
	}

	// A revoked token fails the namespace fetch outright — the probe verdict
	// is authoritative even when a stale cached login exists.
	withGitPlatformAPITest(t, gitPlatformAPIBundle{
		githubUser: func(_ context.Context, _ string) (string, error) {
			return "", context.DeadlineExceeded
		},
	})
	if _, err = app.GitPlatformNamespaces("github", ""); err == nil || !strings.Contains(err.Error(), "GitHub 账号读取失败") {
		t.Fatalf("revoked token swallowed: %v", err)
	}
}

func TestGitPlatformNamespacesGitLab(t *testing.T) {
	app := newGitPlatformTestApp(t, "-", "api")
	withGitPlatformAPITest(t, gitPlatformAPIBundle{
		gitlabUser: func(_ context.Context, _, _ string) (string, error) { return "work-ada", nil },
	})
	cred := mustSaveVRepoCredential(t, app, `{"name":"Work GitLab","kind":"git","provider":"gitlab","origin":"https://gitlab.example.com","secret":"glpat-work","username":"work-ada"}`)
	var gotOrigin string
	withGitPlatformAPITest(t, gitPlatformAPIBundle{
		gitlabNamespace: func(_ context.Context, origin, _ string, _ int) ([]oauth.GitLabNamespace, error) {
			gotOrigin = origin
			return []oauth.GitLabNamespace{
				{ID: 42, Name: "ada", Path: "ada", FullPath: "ada", Kind: "user"},
				{ID: 7, Name: "Acme", Path: "acme", FullPath: "acme", Kind: "group"},
			}, nil
		},
	})

	list, err := app.GitPlatformNamespaces("gitlab", cred.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotOrigin != "https://gitlab.example.com" {
		t.Fatalf("origin = %q", gotOrigin)
	}
	if len(list.Rows) != 2 || list.Rows[0].Kind != "personal" || list.Rows[0].NamespaceID != 42 || list.Rows[1].Kind != "group" {
		t.Fatalf("rows = %+v", list.Rows)
	}
	if list.OrgWarning != "" {
		t.Fatalf("gitlab list carried a warning: %q", list.OrgWarning)
	}
}

func TestGitPlatformRepoNameStatus(t *testing.T) {
	app := newGitPlatformTestApp(t, "repo", "api")
	calls := 0
	withGitPlatformAPITest(t, gitPlatformAPIBundle{
		githubNameStatus: func(_ context.Context, _, owner, name string) (bool, string, error) {
			calls++
			if name == "taken" {
				return false, "该仓库已存在", nil
			}
			return true, "", nil
		},
		gitlabNameStatus: func(_ context.Context, _, _, fullPath string) (bool, string, error) {
			if fullPath != "acme/lab" {
				t.Fatalf("gitlab probe path = %q", fullPath)
			}
			return false, "该仓库已存在", nil
		},
	})

	// A bad local shape never reaches the API.
	status, err := app.GitPlatformRepoNameStatus("github", "", "acme", "bad name!")
	if err != nil || status.Available || status.Reason == "" {
		t.Fatalf("shape = %+v %v", status, err)
	}
	if calls != 0 {
		t.Fatalf("shape check hit the API %d times", calls)
	}

	status, err = app.GitPlatformRepoNameStatus("github", "", "acme", "free")
	if err != nil || !status.Available || status.Reason != "" {
		t.Fatalf("free = %+v %v", status, err)
	}
	status, err = app.GitPlatformRepoNameStatus("github", "", "acme", "taken")
	if err != nil || status.Available || status.Reason == "" {
		t.Fatalf("taken = %+v %v", status, err)
	}
	if _, err = app.GitPlatformRepoNameStatus("gitlab", "", "acme", "lab"); err != nil {
		t.Fatalf("gitlab status: %v", err)
	}
}

func TestGitPlatformCreateRepoGitHub(t *testing.T) {
	app := newGitPlatformTestApp(t, "repo", "-")
	var gotOwner string
	var gotFlags [][2]bool
	withGitPlatformAPITest(t, gitPlatformAPIBundle{
		githubCreate: func(_ context.Context, _, owner, name string, private, autoInit bool) (*oauth.GitHubCreatedRepo, error) {
			gotOwner = owner
			gotFlags = append(gotFlags, [2]bool{private, autoInit})
			return &oauth.GitHubCreatedRepo{FullName: "ada/app", CloneURL: "https://github.com/ada/app.git", DefaultBranch: "main", Private: true}, nil
		},
	})

	created, err := app.GitPlatformCreateRepo(GitPlatformCreateRepoInput{
		Service: "github", NamespaceKind: "personal", Owner: "ada", Name: " app ", Private: true, InitReadme: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotOwner != "" {
		t.Fatalf("personal create sent owner %q", gotOwner)
	}
	if !gotFlags[0][0] || !gotFlags[0][1] {
		t.Fatalf("personal create flags = %v", gotFlags[0])
	}
	if created.FullName != "ada/app" || created.CloneURL != "https://github.com/ada/app.git" {
		t.Fatalf("created = %+v", created)
	}

	created, err = app.GitPlatformCreateRepo(GitPlatformCreateRepoInput{
		Service: "github", NamespaceKind: "org", Owner: "octo", Name: "lib",
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotOwner != "octo" {
		t.Fatalf("org create sent owner %q", gotOwner)
	}
	if created.FullName != "ada/app" {
		t.Fatalf("created = %+v", created)
	}

	// An empty namespace kind with the account's own login falls back to the
	// personal endpoint instead of POSTing /orgs/{user}/repos (404).
	if _, err = app.GitPlatformCreateRepo(GitPlatformCreateRepoInput{
		Service: "github", Owner: "ada", Name: "fallback",
	}); err != nil {
		t.Fatal(err)
	}
	if gotOwner != "" {
		t.Fatalf("kindless personal fallback sent owner %q", gotOwner)
	}

	// A missing name shape and an unknown service fail locally.
	if _, err := app.GitPlatformCreateRepo(GitPlatformCreateRepoInput{Service: "github", Name: "bad name!"}); err == nil {
		t.Fatal("invalid name accepted")
	}
	if _, err := app.GitPlatformCreateRepo(GitPlatformCreateRepoInput{Service: "gitea", Name: "ok"}); err == nil {
		t.Fatal("gitea accepted")
	}
}

func TestGitPlatformCreateRepoGitLabAndCacheBust(t *testing.T) {
	app := newGitPlatformTestApp(t, "repo", "api")
	// Seed the gitlab picker cache; the gitlab creation must bust the SAME
	// service's entry so the new repository is pickable immediately.
	cacheKey := app.cloudAgentGitLabCacheOwner()
	if cacheKey == "" {
		t.Fatal("cache owner key missing")
	}
	app.cloudAgentRepos.put("gitlab", cacheKey, []CloudAgentGitRepository{{Name: "old/one"}})
	withGitPlatformAPITest(t, gitPlatformAPIBundle{
		gitlabCreate: func(_ context.Context, origin, _, name, path string, namespaceID int64, private, initReadme bool) (*oauth.GitLabCreatedProject, error) {
			if origin != "https://gitlab.example.com" || namespaceID != 7 || name != "lab" || path != "lab" || !private || !initReadme {
				t.Fatalf("create args = %s %d %s %s %v %v", origin, namespaceID, name, path, private, initReadme)
			}
			return &oauth.GitLabCreatedProject{PathWithNamespace: "acme/lab", HTTPURLToRepo: "https://gitlab.example.com/acme/lab.git", Visibility: "private"}, nil
		},
	})

	created, err := app.GitPlatformCreateRepo(GitPlatformCreateRepoInput{
		Service: "gitlab", Owner: "acme", NamespaceID: 7, NamespaceKind: "group", Name: "lab", Private: true, InitReadme: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.FullName != "acme/lab" || !created.Private {
		t.Fatalf("created = %+v", created)
	}

	// The creation busts the connect-dialog cache so the new repository is
	// pickable immediately instead of after freshTTL.
	if _, ok := app.cloudAgentRepos.get("gitlab", cacheKey, cloudAgentRepoCacheFreshTTL); ok {
		t.Fatal("create did not bust the repository cache")
	}

	// GitLab without a namespace id is a local rejection.
	if _, err := app.GitPlatformCreateRepo(GitPlatformCreateRepoInput{Service: "gitlab", Owner: "acme", Name: "lab"}); err == nil {
		t.Fatal("missing namespace accepted")
	}
}

func TestImportGitPlatformAccountCredential(t *testing.T) {
	app := newGitPlatformTestApp(t, "repo", "api")
	withGitPlatformAPITest(t, gitPlatformAPIBundle{
		githubUser: func(_ context.Context, token string) (string, error) {
			if token != "gho_settings" {
				t.Fatalf("probe token = %q", token)
			}
			return "ada", nil
		},
		gitlabUser: func(_ context.Context, origin, token string) (string, error) {
			if token != "glpat_settings" || origin != "https://gitlab.example.com" {
				t.Fatalf("probe = %s %q", origin, token)
			}
			return "ada", nil
		},
	})

	imported, err := app.ImportGitPlatformAccountCredential("github")
	if err != nil {
		t.Fatal(err)
	}
	if imported.Provider != "github" || imported.Username != "ada" || imported.Scope != "github.com" {
		t.Fatalf("imported = %+v", imported)
	}
	stored, err := keyring.Get(virtualRepositoryKeyringService, imported.ID)
	if err != nil || stored != "gho_settings" {
		t.Fatalf("keyring secret=%q err=%v", stored, err)
	}

	// A second import of the same account reuses the credential instead of
	// duplicating it.
	again, err := app.ImportGitPlatformAccountCredential("github")
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != imported.ID {
		t.Fatalf("duplicate import created %s vs %s", again.ID, imported.ID)
	}

	gitlabImported, err := app.ImportGitPlatformAccountCredential("gitlab")
	if err != nil {
		t.Fatal(err)
	}
	if gitlabImported.Provider != "gitlab" || gitlabImported.Origin != "https://gitlab.example.com" || gitlabImported.Scope != "gitlab.example.com" {
		t.Fatalf("gitlab import = %+v", gitlabImported)
	}
}

func TestSaveRepositoryCredentialProviderValidation(t *testing.T) {
	app := newGitPlatformTestApp(t, "repo", "api")
	probeCalls := 0
	withGitPlatformAPITest(t, gitPlatformAPIBundle{
		githubUser: func(_ context.Context, token string) (string, error) {
			probeCalls++
			if token == "ghp_bad" {
				return "", context.DeadlineExceeded
			}
			return "ada", nil
		},
	})

	// A provider-tagged secret is validated against the provider before
	// anything is stored; a broken token fails the save.
	if _, err := app.SaveRepositoryCredential(`{"name":"Broken","kind":"git","provider":"github","secret":"ghp_bad"}`); err == nil {
		t.Fatal("broken token accepted")
	}
	// The empty username is filled from the validated login and the scope
	// defaults to the provider host.
	raw, err := app.SaveRepositoryCredential(`{"name":"GH PAT","kind":"git","provider":"github","secret":"ghp_ok"}`)
	if err != nil {
		t.Fatal(err)
	}
	var item RepositoryCredentialMetadata
	if err := json.Unmarshal([]byte(raw), &item); err != nil {
		t.Fatal(err)
	}
	if item.Username != "ada" || item.Scope != "github.com" || item.Provider != "github" {
		t.Fatalf("item = %+v", item)
	}
	// One probe for the failed save plus one for the successful one.
	if probeCalls != 2 {
		t.Fatalf("probe ran %d times", probeCalls)
	}

	// Metadata-only edits (no secret change) skip the probe.
	if _, err := app.SaveRepositoryCredential(`{"id":"` + item.ID + `","name":"GH PAT renamed","kind":"git","provider":"github","username":"ada","secret":""}`); err != nil {
		t.Fatal(err)
	}
	if probeCalls != 1 {
		t.Fatalf("metadata edit re-probed: %d", probeCalls)
	}

	// GitLab credentials require the site origin; github ones must not carry
	// one; unknown providers are rejected.
	cases := []struct{ input, want string }{
		{`{"name":"L","kind":"git","provider":"gitlab","secret":"gl"}`, "origin"},
		{`{"name":"G","kind":"git","provider":"github","origin":"https://github.com","secret":"gh"}`, "origin"},
		{`{"name":"X","kind":"git","provider":"gitea","secret":"s"}`, "provider"},
		{`{"name":"B","kind":"git","provider":"gitlab","origin":"https://gitlab.example.com/sub","secret":"gl"}`, "站点地址"},
	}
	for _, tc := range cases {
		if _, err := app.SaveRepositoryCredential(tc.input); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("input %s: err=%v want %q", tc.input, err, tc.want)
		}
	}

	// The legacy provider-agnostic credential keeps requiring a username and
	// must never be probed.
	if _, err := app.SaveRepositoryCredential(`{"name":"Legacy","kind":"git","secret":"s"}`); err == nil || !strings.Contains(err.Error(), "username") {
		t.Fatalf("legacy username requirement dropped: %v", err)
	}
	if probeCalls != 1 {
		t.Fatalf("legacy save probed: %d", probeCalls)
	}
}

func TestPrepareCloudGitCredentialToken(t *testing.T) {
	app := newGitPlatformTestApp(t, "repo", "api")
	withGitPlatformAPITest(t, gitPlatformAPIBundle{
		githubUser: func(_ context.Context, _ string) (string, error) { return "ada", nil },
	})
	cred := mustSaveVRepoCredential(t, app, `{"name":"GH PAT","kind":"git","provider":"github","secret":"ghp_bind"}`)

	// The credential source injects the keyring secret backend-side; the
	// webview never carries a token. Origin and author identity fall out of
	// the credential.
	body := CloudGitConnect{Service: "github", Repo: "ada/app", Source: "credential", CredentialID: cred.ID}
	if err := app.prepareCloudGitAccountConnect(&body); err != nil {
		t.Fatal(err)
	}
	if body.Token != "ghp_bind" || body.Source != "credential" || body.Origin != "https://github.com" {
		t.Fatalf("body = %+v", body)
	}
	if body.AuthorName != "ada" || body.AuthorEmail != "ada@users.noreply.github.com" {
		t.Fatalf("author = %s %s", body.AuthorName, body.AuthorEmail)
	}

	// A manual token submitted alongside the credential source is refused:
	// the injected secret must win, never merge.
	conflict := CloudGitConnect{Service: "github", Repo: "ada/app", Source: "credential", CredentialID: cred.ID, Token: "manual"}
	if err := app.prepareCloudGitAccountConnect(&conflict); err == nil {
		t.Fatal("manual token accepted with credential source")
	}
	// A missing credential reference fails closed.
	missing := CloudGitConnect{Service: "github", Repo: "ada/app", Source: "credential"}
	if err := app.prepareCloudGitAccountConnect(&missing); err == nil {
		t.Fatal("missing credential accepted")
	}
	// Unknown ids fail closed.
	unknown := CloudGitConnect{Service: "github", Repo: "ada/app", Source: "credential", CredentialID: "vcred_missing"}
	if err := app.prepareCloudGitAccountConnect(&unknown); err == nil {
		t.Fatal("unknown credential accepted")
	}

	// A gitlab credential pins the connect origin to the credential's site
	// and rejects a different host.
	withGitPlatformAPITest(t, gitPlatformAPIBundle{
		gitlabUser: func(_ context.Context, _, _ string) (string, error) { return "ada", nil },
	})
	gitlabCred := mustSaveVRepoCredential(t, app, `{"name":"Work GitLab","kind":"git","provider":"gitlab","origin":"https://gitlab.example.com","secret":"glpat-bind"}`)
	gitlabBody := CloudGitConnect{Service: "gitlab", Repo: "acme/app", Origin: "https://other.example.com", Source: "credential", CredentialID: gitlabCred.ID}
	if err := app.prepareCloudGitAccountConnect(&gitlabBody); err == nil {
		t.Fatal("foreign origin accepted")
	}
	gitlabBody = CloudGitConnect{Service: "gitlab", Repo: "acme/app", Source: "credential", CredentialID: gitlabCred.ID}
	if err := app.prepareCloudGitAccountConnect(&gitlabBody); err != nil {
		t.Fatal(err)
	}
	if gitlabBody.Token != "glpat-bind" || gitlabBody.Origin != "https://gitlab.example.com" {
		t.Fatalf("gitlab body = %+v", gitlabBody)
	}
	if gitlabBody.AuthorEmail != "ada@users.noreply.gitlab.example.com" {
		t.Fatalf("gitlab author email = %q", gitlabBody.AuthorEmail)
	}

	// Unknown source values keep passing through untouched (the historical
	// behavior the manual-token path relies on).
	manual := CloudGitConnect{Service: "github", Repo: "ada/app", Token: "manual", Source: ""}
	if err := app.prepareCloudGitAccountConnect(&manual); err != nil || manual.Token != "manual" {
		t.Fatalf("manual path changed: %+v %v", manual, err)
	}
}

// TestSyncCloudAgentGitCredentialsSkipsCredentialSource mirrors the
// cloud_agent_github_test.go hub mock: a coding instance bound with
// source="credential" must survive the settings-account sync untouched — its
// token is user-managed in the credential store, not a mirror of the
// signed-in account.
func TestSyncCloudAgentGitCredentialsSkipsCredentialSource(t *testing.T) {
	var pushed []map[string]any
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/cloud-agents":
			_, _ = w.Write([]byte(`{"instances":[{"id":"cag_cred","kind":"coding","status":"idle"}]}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/cag_cred/git"):
			_, _ = w.Write([]byte(`{"connected":true,"service":"github","origin":"https://github.com","repo":"ada/app","source":"credential"}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git"):
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			pushed = append(pushed, body)
			_, _ = w.Write([]byte(`{"connected":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer hub.Close()

	app := newGitPlatformTestApp(t, "repo", "api")
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL: hub.URL, RemoteViewerToken: "viewer-token", RemoteMachineToken: "machine-token", RemoteMachineID: "machine-test",
	}); err != nil {
		t.Fatal(err)
	}

	app.syncCloudAgentGitCredentials()

	if len(pushed) != 0 {
		t.Fatalf("credential-sourced binding was refreshed: %+v", pushed)
	}
}
