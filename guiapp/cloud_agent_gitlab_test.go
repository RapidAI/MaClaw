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

func TestSyncCloudAgentGitLabCredentialsRefreshesOnlyAccountBound(t *testing.T) {
	var pushed []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/cloud-agents":
			_, _ = w.Write([]byte(`{"instances":[
				{"id":"cag_bound","kind":"coding","status":"idle"},
				{"id":"cag_other_host","kind":"coding","status":"idle"},
				{"id":"cag_manual","kind":"coding","status":"idle"},
				{"id":"cag_github","kind":"coding","status":"idle"}
			]}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/git"):
			switch {
			case strings.HasSuffix(r.URL.Path, "/cag_bound/git"):
				_, _ = w.Write([]byte(`{"connected":true,"service":"gitlab","origin":"https://gitlab.example.com","repo":"acme/demo","author_name":"Tux","author_email":"tux@x.com","source":"account"}`))
			case strings.HasSuffix(r.URL.Path, "/cag_other_host/git"):
				_, _ = w.Write([]byte(`{"connected":true,"service":"gitlab","origin":"https://gitlab.other.io","repo":"acme/demo","author_name":"Tux","author_email":"tux@x.com","source":"account"}`))
			case strings.HasSuffix(r.URL.Path, "/cag_manual/git"):
				_, _ = w.Write([]byte(`{"connected":true,"service":"gitlab","origin":"https://gitlab.example.com","repo":"acme/demo","author_name":"Tux","author_email":"tux@x.com"}`))
			case strings.HasSuffix(r.URL.Path, "/cag_github/git"):
				_, _ = w.Write([]byte(`{"connected":true,"service":"github","origin":"https://github.com","repo":"acme/demo","author_name":"Tux","author_email":"tux@x.com","source":"account"}`))
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
	if err := store.Modify(oauth.GitLabAccountProviderID, func(_ *oauth.StoredCredential) (*oauth.StoredCredential, error) {
		return &oauth.StoredCredential{Type: "token", AccessToken: "glpat-new", BaseURL: "https://gitlab.example.com", Login: "tux"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	app.credentialStore = store

	app.syncCloudAgentGitLabCredentials()

	if len(pushed) != 1 {
		t.Fatalf("pushed %d git connects, want 1: %v", len(pushed), pushed)
	}
	if !strings.Contains(pushed[0]["_instance"].(string), "cag_bound") {
		t.Fatalf("refreshed wrong instance: %v", pushed[0])
	}
	if pushed[0]["token"] != "glpat-new" || pushed[0]["source"] != "account" || pushed[0]["service"] != "gitlab" {
		t.Fatalf("body = %v", pushed[0])
	}
	if pushed[0]["repo"] != "acme/demo" || pushed[0]["author_name"] != "Tux" {
		t.Fatalf("repo/author lost on refresh: %v", pushed[0])
	}
}

func TestSyncCloudAgentGitLabCredentialsSkipsWithoutToken(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	// No credential store and no token — must not even list instances.
	app.syncCloudAgentGitLabCredentials()
}

func TestSyncCloudAgentGitLabCredentialsSkipsInsufficientScopes(t *testing.T) {
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
	// A definitively insufficient introspected grant (read-only) cannot push
	// — the instances keep whatever token they already have.
	if err := store.Modify(oauth.GitLabAccountProviderID, func(_ *oauth.StoredCredential) (*oauth.StoredCredential, error) {
		return &oauth.StoredCredential{Type: "token", AccessToken: "glpat-readonly", BaseURL: "https://gitlab.example.com", Login: "tux", Scopes: "read_user,read_repository"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	app.credentialStore = store

	app.syncCloudAgentGitLabCredentials()

	if hit {
		t.Fatal("read-only token must not reach the hub — instances keep their working credential")
	}
}
