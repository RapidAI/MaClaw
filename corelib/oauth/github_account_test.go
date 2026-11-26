package oauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGitHubAccountClientHonorsRepoScope(t *testing.T) {
	if GitHubAppClientID(GitHubAccountClientID) {
		t.Fatal("account login uses a GitHub App, so the repo scope is never granted")
	}
	if !GitHubAppClientID(GitHubCopilotClientID) {
		t.Fatal("copilot client should stay classified as a GitHub App")
	}
	if !GitHubAppClientID("  LV1.abc ") {
		t.Fatal("lv1 prefix is a GitHub App client id")
	}
	if GitHubAppClientID("178c6fc778ccc68e1d6a") {
		t.Fatal("oauth app client id was classified as a GitHub App")
	}
}

func TestParseGitHubDeviceTokenKeepsGrantedScope(t *testing.T) {
	token, scope, retry, slow, err := parseGitHubDeviceToken([]byte(`{"access_token":"gho_test","scope":"read:user,repo","token_type":"bearer"}`))
	if err != nil || retry || slow || token != "gho_test" || scope != "read:user,repo" {
		t.Fatalf("oauth grant = %q %q retry=%v slow=%v err=%v", token, scope, retry, slow, err)
	}

	token, scope, retry, slow, err = parseGitHubDeviceToken([]byte(`{"access_token":"ghu_test","scope":"","token_type":"bearer"}`))
	if err != nil || retry || slow || token != "ghu_test" || scope != "" {
		t.Fatalf("github app grant = %q %q retry=%v slow=%v err=%v", token, scope, retry, slow, err)
	}

	_, _, retry, slow, err = parseGitHubDeviceToken([]byte(`{"error":"authorization_pending"}`))
	if err != nil || !retry || slow {
		t.Fatalf("pending = retry=%v slow=%v err=%v", retry, slow, err)
	}
}

func TestGitHubEffectiveScopesFallsBackToTokenGrant(t *testing.T) {
	if got := GitHubEffectiveScopes("", "read:user,repo"); got != "read:user,repo" {
		t.Fatalf("empty header = %q", got)
	}
	if got := GitHubEffectiveScopes("repo, read:user", "read:user"); got != "repo, read:user" {
		t.Fatalf("header = %q", got)
	}
	if got := GitHubEffectiveScopes("  ", ""); got != "" {
		t.Fatalf("blank = %q", got)
	}
}

func TestGitHubScopesHaveRepo(t *testing.T) {
	for _, scopes := range []string{"repo", "read:user repo", "read:user,repo", "repo, read:user", " read:user, repo "} {
		if !GitHubScopesHaveRepo(scopes) {
			t.Fatalf("%q should include repo", scopes)
		}
	}
	for _, scopes := range []string{"", "read:user", "public_repo", "repo:status", "repo:invite", "read:repo_hook"} {
		if GitHubScopesHaveRepo(scopes) {
			t.Fatalf("%q must not count as repo", scopes)
		}
	}
}

func TestGitHubAppUserToken(t *testing.T) {
	for _, token := range []string{"ghu_abc123", " ghu_abc123 ", "ghu_"} {
		if !GitHubAppUserToken(token) {
			t.Fatalf("%q should be a GitHub App user token", token)
		}
	}
	for _, token := range []string{"", "gho_abc123", "ghp_abc123", "github_pat_abc123", "6c1Fabcdefgh"} {
		if GitHubAppUserToken(token) {
			t.Fatalf("%q must not count as a GitHub App user token", token)
		}
	}
}

func TestFetchGitHubRepositories(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user/repos" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer repo-token" {
			t.Fatalf("authorization = %q", got)
		}
		if got := r.URL.Query().Get("affiliation"); got != "owner,collaborator,organization_member" {
			t.Fatalf("affiliation = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"full_name":"acme/app","description":"App","private":true,"html_url":"https://github.com/acme/app","default_branch":"main","updated_at":"2026-01-02T03:04:05Z"},
			{"full_name":""},
			{"full_name":"octo/lib","private":false}
		]`))
	}))
	defer server.Close()

	rows, err := fetchGitHubRepositories(context.Background(), server.URL, "repo-token", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].FullName != "acme/app" || !rows[0].Private || rows[1].FullName != "octo/lib" {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestGitHubHTTPErrorKeepsAPIMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"API rate limit exceeded"}`))
	}))
	defer server.Close()

	_, err := fetchGitHubRepositories(context.Background(), server.URL, "repo-token", 1)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "API rate limit exceeded") {
		t.Fatalf("error hides GitHub message: %v", err)
	}
	if strings.Contains(err.Error(), "body_len=") {
		t.Fatalf("error still uses opaque body length: %v", err)
	}
}
