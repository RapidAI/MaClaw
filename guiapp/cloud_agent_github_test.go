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
				{"id":"cag_session","kind":"session","status":"idle"}
			]}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/git"):
			switch {
			case strings.HasSuffix(r.URL.Path, "/cag_bound/git"):
				_, _ = w.Write([]byte(`{"connected":true,"service":"github","origin":"https://github.com","repo":"acme/demo","author_name":"Ada","author_email":"ada@x.com","source":"account"}`))
			case strings.HasSuffix(r.URL.Path, "/cag_manual/git"):
				_, _ = w.Write([]byte(`{"connected":true,"service":"github","repo":"acme/demo","author_name":"Ada","author_email":"ada@x.com"}`))
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
		return &oauth.StoredCredential{Type: "oauth", AccessToken: "new-oauth-token", Login: "ada"}, nil
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
