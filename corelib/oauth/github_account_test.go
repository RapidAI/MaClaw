package oauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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

// A 403/429 with X-RateLimit-Remaining: 0 is an exhausted quota. The error
// must stay typed so the caller can show the reset time instead of GitHub's
// raw scraping-policy paragraph, and must still carry GitHub's own message.
func TestFetchGitHubRepositoriesRateLimitIsTyped(t *testing.T) {
	reset := time.Now().Add(30 * time.Minute).Truncate(time.Minute)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", fmt.Sprint(reset.Unix()))
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"API rate limit exceeded for user ID 123"}`))
	}))
	defer server.Close()

	_, err := fetchGitHubRepositories(context.Background(), server.URL, "repo-token", 1)
	var rlErr *GitHubRateLimitError
	if !errors.As(err, &rlErr) {
		t.Fatalf("rate limit is not typed: %v", err)
	}
	if !rlErr.ResetAt.Equal(reset) {
		t.Fatalf("reset = %s want %s", rlErr.ResetAt, reset)
	}
	if !strings.Contains(err.Error(), "API rate limit exceeded for user ID 123") {
		t.Fatalf("error hides GitHub message: %v", err)
	}
	if !strings.Contains(err.Error(), reset.Format("15:04")) {
		t.Fatalf("error hides reset time: %v", err)
	}

	// A plain 403 without the exhausted header stays an untyped HTTP error —
	// e.g. a revoked token must not be misread as "wait for the reset".
	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-RateLimit-Remaining", "4999")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"Forbidden"}`))
	}))
	defer server2.Close()
	_, err = fetchGitHubRepositories(context.Background(), server2.URL, "repo-token", 1)
	if errors.As(err, &rlErr) {
		t.Fatalf("normal 403 misclassified as rate limit: %v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "HTTP 403") {
		t.Fatalf("plain 403 lost its HTTP status: %v", err)
	}

	// 429 with the exhausted header is the same primary-limit signature.
	server3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"message":"API rate limit exceeded"}`))
	}))
	defer server3.Close()
	_, err = fetchGitHubRepositories(context.Background(), server3.URL, "repo-token", 1)
	if !errors.As(err, &rlErr) {
		t.Fatalf("429 with remaining=0 is not typed: %v", err)
	}
}
