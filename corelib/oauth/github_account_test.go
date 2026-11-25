package oauth

import "testing"

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
