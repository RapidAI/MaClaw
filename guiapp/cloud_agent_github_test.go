package guiapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/oauth"
)

func TestSyncCloudAgentGitCredentialsRefreshesOnlyAccountBound(t *testing.T) {
	var pushed []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/cloud-agents":
			_, _ = w.Write([]byte(`{"instances":[
				{"id":"cag_bound","kind":"coding","status":"idle"},
				{"id":"cag_manual","kind":"coding","status":"idle"},
				{"id":"cag_insecure","kind":"coding","status":"idle"},
				{"id":"cag_session","kind":"session","status":"idle"}
			]}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/git"):
			switch {
			case strings.HasSuffix(r.URL.Path, "/cag_bound/git"):
				_, _ = w.Write([]byte(`{"connected":true,"service":"github","origin":"https://github.com","repo":"acme/demo","author_name":"Ada","author_email":"ada@x.com","source":"account"}`))
			case strings.HasSuffix(r.URL.Path, "/cag_manual/git"):
				_, _ = w.Write([]byte(`{"connected":true,"service":"github","repo":"acme/demo","author_name":"Ada","author_email":"ada@x.com"}`))
			case strings.HasSuffix(r.URL.Path, "/cag_insecure/git"):
				_, _ = w.Write([]byte(`{"connected":true,"service":"github","origin":"http://github.com","repo":"acme/demo","author_name":"Ada","author_email":"ada@x.com","source":"account"}`))
			default:
				_, _ = w.Write([]byte(`{}`))
			}
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git"):
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			body["_instance"] = r.URL.Path
			pushed = append(pushed, body)
			_, _ = w.Write([]byte(`{"connected":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token", RemoteMachineToken: "machine-token", RemoteMachineID: "machine-test",
	}); err != nil {
		t.Fatal(err)
	}
	store := oauth.NewFileCredentialStore(filepath.Join(app.testHomeDir, "credentials.json"))
	if err := store.Modify(oauth.GitHubAccountProviderID, func(_ *oauth.StoredCredential) (*oauth.StoredCredential, error) {
		return &oauth.StoredCredential{Type: "oauth", AccessToken: "new-oauth-token", Login: "ada", Scopes: "repo"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	app.credentialStore = store

	app.syncCloudAgentGitCredentials()

	if len(pushed) != 1 {
		t.Fatalf("pushed %d git connects, want 1: %v", len(pushed), pushed)
	}
	if !strings.Contains(pushed[0]["_instance"].(string), "cag_bound") {
		t.Fatalf("refreshed wrong instance: %v", pushed[0])
	}
	if pushed[0]["token"] != "new-oauth-token" || pushed[0]["source"] != "account" {
		t.Fatalf("body = %v", pushed[0])
	}
	if pushed[0]["repo"] != "acme/demo" || pushed[0]["author_name"] != "Ada" {
		t.Fatalf("repo/author lost on refresh: %v", pushed[0])
	}
}

func TestSyncCloudAgentGitCredentialsSkipsWithoutToken(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	// No credential store and no token — must not even list instances.
	app.syncCloudAgentGitCredentials()
}

func TestSyncCloudAgentGitCredentialsSkipsScopelessToken(t *testing.T) {
	hit := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token", RemoteMachineToken: "machine-token", RemoteMachineID: "machine-test",
	}); err != nil {
		t.Fatal(err)
	}
	store := oauth.NewFileCredentialStore(filepath.Join(app.testHomeDir, "credentials.json"))
	if err := store.Modify(oauth.GitHubAccountProviderID, func(_ *oauth.StoredCredential) (*oauth.StoredCredential, error) {
		return &oauth.StoredCredential{Type: "oauth", AccessToken: "ghu_legacy_or_scopeless", Login: "ada"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	app.credentialStore = store

	app.syncCloudAgentGitCredentials()

	if hit {
		t.Fatal("scopeless token must not reach the hub — instances keep their working credential")
	}
}

func TestConnectCloudAgentGitInjectsStoredGitHubAccount(t *testing.T) {
	var pushed map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/cag_1/git") {
			if err := json.NewDecoder(r.Body).Decode(&pushed); err != nil {
				t.Fatalf("decode: %v", err)
			}
			_, _ = w.Write([]byte(`{"connected":true,"repo":"acme/demo"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token", RemoteMachineToken: "machine-token", RemoteMachineID: "machine-test",
	}); err != nil {
		t.Fatal(err)
	}
	store := oauth.NewFileCredentialStore(filepath.Join(app.testHomeDir, "credentials.json"))
	if err := store.Modify(oauth.GitHubAccountProviderID, func(_ *oauth.StoredCredential) (*oauth.StoredCredential, error) {
		return &oauth.StoredCredential{Type: "oauth", AccessToken: "stored-github-token", Login: "ada", DisplayName: "Ada Lovelace", UserID: "42", Scopes: "repo"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	app.credentialStore = store

	if _, err := app.ConnectCloudAgentGit("cag_1", CloudGitConnect{Service: "github", Repo: "acme/demo", Source: "account"}); err != nil {
		t.Fatal(err)
	}
	if pushed["token"] != "stored-github-token" || pushed["source"] != "account" {
		t.Fatalf("token/source = %v", pushed)
	}
	if pushed["author_name"] != "Ada Lovelace" || pushed["author_email"] != "42+ada@users.noreply.github.com" {
		t.Fatalf("author = %v", pushed)
	}
}

func TestConnectCloudAgentGitDoesNotSendGitHubAccountTokenToAnotherHost(t *testing.T) {
	hit := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token", RemoteMachineToken: "machine-token", RemoteMachineID: "machine-test",
	}); err != nil {
		t.Fatal(err)
	}
	store := oauth.NewFileCredentialStore(filepath.Join(app.testHomeDir, "credentials.json"))
	if err := store.Modify(oauth.GitHubAccountProviderID, func(_ *oauth.StoredCredential) (*oauth.StoredCredential, error) {
		return &oauth.StoredCredential{Type: "oauth", AccessToken: "stored-github-token", Login: "ada", Scopes: "repo"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	app.credentialStore = store

	_, err := app.ConnectCloudAgentGit("cag_1", CloudGitConnect{Service: "github", Origin: "https://github.example.com", Repo: "acme/demo", Source: "account"})
	if err == nil {
		t.Fatal("foreign origin accepted the account token")
	}
	if hit {
		t.Fatal("foreign origin reached the hub with an account-bound connect")
	}
	for _, origin := range []string{"http://github.com", "https://user@github.com", "https://github.com/acme"} {
		_, err = app.ConnectCloudAgentGit("cag_1", CloudGitConnect{Service: "github", Origin: origin, Repo: "acme/demo", Source: "account"})
		if err == nil {
			t.Fatalf("unsafe origin accepted the account token: %s", origin)
		}
		if hit {
			t.Fatalf("unsafe origin reached the hub with an account-bound connect: %s", origin)
		}
	}
}

func TestCloudAgentGitHubAccountIsLocalAndDoesNotExposeToken(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	store := oauth.NewFileCredentialStore(filepath.Join(app.testHomeDir, "credentials.json"))
	if err := store.Modify(oauth.GitHubAccountProviderID, func(_ *oauth.StoredCredential) (*oauth.StoredCredential, error) {
		return &oauth.StoredCredential{Type: "oauth", AccessToken: "stored-github-token", Login: "ada", DisplayName: "Ada Lovelace", UserID: "42", Scopes: "read:user,repo"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	app.credentialStore = store

	view, err := app.CloudAgentGitHubAccount()
	if err != nil {
		t.Fatal(err)
	}
	if !view.Connected || view.Login != "ada" || view.Name != "Ada Lovelace" || !view.RepoScope {
		t.Fatalf("view = %+v", view)
	}
	if view.Email != "42+ada@users.noreply.github.com" {
		t.Fatalf("email = %q", view.Email)
	}
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "stored-github-token") || strings.Contains(string(raw), "\"token\":") {
		t.Fatalf("account view exposes token material: %s", raw)
	}
}

func TestConnectCloudAgentGitRejectsManualTokenMarkedAsAccount(t *testing.T) {
	app := &App{}
	_, err := app.ConnectCloudAgentGit("cag_1", CloudGitConnect{Service: "github", Repo: "acme/demo", Token: "manual-token", Source: "account"})
	if err == nil || !strings.Contains(err.Error(), "不能同时提交手动令牌") {
		t.Fatalf("mixed account/manual credential accepted: %v", err)
	}
}

func TestConnectCloudAgentGitRejectsUnknownAccountService(t *testing.T) {
	app := &App{}
	_, err := app.ConnectCloudAgentGit("cag_1", CloudGitConnect{Service: "bitbucket", Repo: "acme/demo", Source: "account"})
	if err == nil || !strings.Contains(err.Error(), "只支持 GitHub 或 GitLab") {
		t.Fatalf("unknown account service accepted: %v", err)
	}
}
