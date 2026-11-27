package guiapp

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/oauth"
)

// fresh hits never re-fetch, stale entries fall back only within staleTTL,
// and entries are scoped to one connected account. The clock is injected, so
// the test travels across the TTL boundaries without sleeping.
func TestCloudAgentRepoCacheTTLs(t *testing.T) {
	current := time.Now()
	var cache cloudAgentRepoCache
	cache.now = func() time.Time { return current }

	rows := []CloudAgentGitRepository{{Name: "acme/app"}}
	cache.put("github", "znsoftm", rows)
	cache.put("gitlab", "znsoft2|https://gitlab.com", []CloudAgentGitRepository{{Name: "gitlab-org/gitlab"}})

	got, ok := cache.get("github", "znsoftm", cloudAgentRepoCacheFreshTTL)
	if !ok || len(got) != 1 || got[0].Name != "acme/app" {
		t.Fatalf("fresh github = %v %v", got, ok)
	}
	// The stored slice is copied: mutating the result must not corrupt the cache.
	got[0].Name = "mutated"
	again, _ := cache.get("github", "znsoftm", cloudAgentRepoCacheFreshTTL)
	if again[0].Name != "acme/app" {
		t.Fatalf("cache leaked a mutable slice: %v", again[0].Name)
	}
	if other, ok := cache.get("gitlab", "znsoft2|https://gitlab.com", cloudAgentRepoCacheFreshTTL); !ok || other[0].Name != "gitlab-org/gitlab" {
		t.Fatalf("service keys share one entry: %v %v", other, ok)
	}

	// A different account on the same service must never see this list —
	// switching accounts silently served the previous owner's repositories.
	if _, ok := cache.get("github", "someone-else", cloudAgentRepoCacheFreshTTL); ok {
		t.Fatal("another account hit the cached entry")
	}
	// A disconnected account (empty owner) never matches anything.
	if _, ok := cache.get("github", "", cloudAgentRepoCacheStaleTTL); ok {
		t.Fatal("empty owner hit the cached entry")
	}
	// Storing without an owner is a no-op: a disconnected read must not seed
	// the fallback for whoever connects next.
	cache.put("gitlab", "", []CloudAgentGitRepository{{Name: "ghost/repo"}})
	if _, ok := cache.get("gitlab", "anyone", cloudAgentRepoCacheStaleTTL); ok {
		t.Fatal("ownerless put created a servable entry")
	}

	// past freshTTL the fresh read misses, but the stale fallback still serves
	// the last known list (the rate-limit path).
	current = current.Add(cloudAgentRepoCacheFreshTTL + time.Second)
	if _, ok := cache.get("github", "znsoftm", cloudAgentRepoCacheFreshTTL); ok {
		t.Fatal("expired entry still fresh")
	}
	stale, ok := cache.get("github", "znsoftm", cloudAgentRepoCacheStaleTTL)
	if !ok || len(stale) != 1 {
		t.Fatalf("stale fallback lost the entry: %v %v", stale, ok)
	}

	// past staleTTL nothing is served — a week-old list must not mask a
	// permanently broken fetch.
	current = current.Add(cloudAgentRepoCacheStaleTTL)
	if _, ok := cache.get("github", "znsoftm", cloudAgentRepoCacheStaleTTL); ok {
		t.Fatal("week-old entry served as stale fallback")
	}

	// An empty cache never fabricates a fallback.
	var empty cloudAgentRepoCache
	if _, ok := empty.get("github", "znsoftm", cloudAgentRepoCacheStaleTTL); ok {
		t.Fatal("empty cache returned an entry")
	}
}

// The friendly quota message carries the reset time and stays actionable —
// it must not leak GitHub's raw scraping-policy paragraph to the dialog.
func TestCloudAgentRepoRateLimitMessage(t *testing.T) {
	reset := time.Now().Add(25 * time.Minute)
	msg := cloudAgentRateLimitMessage(&oauth.GitHubRateLimitError{
		Endpoint: "github /user/repos",
		Message:  "API rate limit exceeded for user ID 123 (scraping, Terms of Service)",
		ResetAt:  reset,
	})
	if !strings.Contains(msg, "配额已用尽") || !strings.Contains(msg, reset.Format("15:04")) {
		t.Fatalf("message missing quota guidance: %q", msg)
	}
	if strings.Contains(msg, "scraping") || strings.Contains(msg, "Terms of Service") {
		t.Fatalf("message leaks the raw GitHub paragraph: %q", msg)
	}
	// A missing reset header still produces usable guidance.
	noReset := cloudAgentRateLimitMessage(&oauth.GitHubRateLimitError{Endpoint: "github /user/repos"})
	if !strings.Contains(noReset, "配额已用尽") {
		t.Fatalf("no-reset message unusable: %q", noReset)
	}
	// nil is a no-op so the caller's errors.As guard stays simple.
	if cloudAgentRateLimitMessage(nil) != "" {
		t.Fatal("nil error produced a message")
	}
}

// cloudAgentRepoListError finds the typed error through the loader's wrapping
// and swaps only that branch's text; every other error surfaces unchanged.
func TestCloudAgentRepoListErrorConversion(t *testing.T) {
	reset := time.Now().Add(20 * time.Minute)
	wrapped := fmt.Errorf("GitHub 仓库列表获取失败: %w", &oauth.GitHubRateLimitError{
		Endpoint: "github /user/repos",
		ResetAt:  reset,
	})
	converted := cloudAgentRepoListError(wrapped)
	msg := converted.Error()
	if strings.Contains(msg, "GitHub 仓库列表获取失败") || strings.Contains(msg, "HTTP") {
		t.Fatalf("conversion kept the raw wrapper: %q", msg)
	}
	if !strings.Contains(msg, reset.Format("15:04")) {
		t.Fatalf("conversion lost the reset time: %q", msg)
	}

	// Other failures keep their full context.
	plain := errors.New("GitHub 仓库列表获取失败: HTTP 401: token is invalid or revoked")
	if cloudAgentRepoListError(plain) != plain {
		t.Fatal("plain error was replaced")
	}
}

// The cache owner key only admits credentials the loader would serve: a
// token, and the repo scope. A re-login of the same account without the
// scope must not read the scoped session's cached list.
func TestCloudAgentRepoCacheOwnerKeys(t *testing.T) {
	scoped := &oauth.StoredCredential{AccessToken: " gho_token ", Login: " znsoftm ", UserID: "15354249", Scopes: "read:user,repo"}
	if got := cloudAgentGitHubCacheOwnerKey(scoped); got != "znsoftm" {
		t.Fatalf("scoped github key = %q", got)
	}
	// Login missing (legacy credential) falls back to the user id.
	noLogin := &oauth.StoredCredential{AccessToken: "gho_token", UserID: "15354249", Scopes: "repo"}
	if got := cloudAgentGitHubCacheOwnerKey(noLogin); got != "15354249" {
		t.Fatalf("no-login github key = %q", got)
	}
	// Same account re-logged-in without the repo scope: no cache.
	narrowed := &oauth.StoredCredential{AccessToken: "gho_token", Login: "znsoftm", Scopes: "read:user"}
	if got := cloudAgentGitHubCacheOwnerKey(narrowed); got != "" {
		t.Fatalf("scope-narrowed github key = %q", got)
	}
	if got := cloudAgentGitHubCacheOwnerKey(nil); got != "" {
		t.Fatalf("nil github key = %q", got)
	}
	if got := cloudAgentGitHubCacheOwnerKey(&oauth.StoredCredential{Login: "znsoftm", Scopes: "repo"}); got != "" {
		t.Fatalf("empty-token github key = %q", got)
	}

	gitlab := &oauth.StoredCredential{AccessToken: "glpat_x", Login: "znsoft2", BaseURL: "https://gitlab.com", Scopes: "api"}
	if got := cloudAgentGitLabCacheOwnerKey(gitlab); got != "znsoft2|https://gitlab.com" {
		t.Fatalf("gitlab key = %q", got)
	}
	// The site is part of the identity: same login on a self-hosted GitLab
	// is a different repository space.
	selfHosted := &oauth.StoredCredential{AccessToken: "glpat_x", Login: "znsoft2", BaseURL: "https://git.example.com", Scopes: "api"}
	if cloudAgentGitLabCacheOwnerKey(gitlab) == cloudAgentGitLabCacheOwnerKey(selfHosted) {
		t.Fatal("self-hosted site shares the gitlab.com cache key")
	}
	// GitLab treats a BLANK scope list as a legacy credential the loader
	// admits (GitLabScopesHaveRepo is lenient on empty), so the cache key
	// admits it too; explicit non-repo scopes do not pass.
	legacy := &oauth.StoredCredential{AccessToken: "glpat_x", Login: "znsoft2", BaseURL: "https://gitlab.com"}
	if got := cloudAgentGitLabCacheOwnerKey(legacy); got != "znsoft2|https://gitlab.com" {
		t.Fatalf("blank-scope gitlab key = %q", got)
	}
	if got := cloudAgentGitLabCacheOwnerKey(&oauth.StoredCredential{AccessToken: "glpat_x", Login: "znsoft2", Scopes: "read_user"}); got != "" {
		t.Fatalf("read_user-only gitlab key = %q", got)
	}
}
