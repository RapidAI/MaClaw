package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFetchGitHubOrgs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user/orgs" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer org-token" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"login":"acme","description":"Acme org"},
			{"login":""},
			{"login":"octo"}
		]`))
	}))
	defer server.Close()

	rows, err := fetchGitHubOrgs(context.Background(), server.URL, "org-token", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Login != "acme" || rows[0].Description != "Acme org" || rows[1].Login != "octo" {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestGitHubRepoNameStatus(t *testing.T) {
	var muxPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		muxPath = r.URL.Path
		if got := r.Header.Get("Authorization"); got != "Bearer t" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/acme/app":
			_, _ = w.Write([]byte(`{"full_name":"acme/app"}`))
		case "/repos/acme/free":
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	available, reason, err := githubRepoNameStatus(context.Background(), server.URL, "t", "acme", "app")
	if err != nil || available || reason == "" {
		t.Fatalf("taken = %v %q %v", available, reason, err)
	}
	available, reason, err = githubRepoNameStatus(context.Background(), server.URL, "t", "acme", "free")
	if err != nil || !available || reason != "" {
		t.Fatalf("free = %v %q %v", available, reason, err)
	}
	if muxPath != "/repos/acme/free" {
		t.Fatalf("probe path = %q", muxPath)
	}

	// A failed probe must never read as "available": rate limits stay typed,
	// and a bad local shape is rejected without a network call.
	limited := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", fmt.Sprint(time.Now().Add(time.Minute).Unix()))
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"API rate limit exceeded"}`))
	}))
	defer limited.Close()
	_, _, err = githubRepoNameStatus(context.Background(), limited.URL, "t", "acme", "app")
	var rlErr *GitHubRateLimitError
	if !errors.As(err, &rlErr) {
		t.Fatalf("rate limit is not typed: %v", err)
	}
	// An invalid local shape is rejected without a network call: not
	// available, explanatory reason, no error.
	available, reason, err = githubRepoNameStatus(context.Background(), server.URL, "t", "acme", "bad name!")
	if err != nil || available || reason == "" {
		t.Fatalf("invalid shape = %v %q %v", available, reason, err)
	}
}

func TestCreateGitHubRepo(t *testing.T) {
	var gotPath, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("method/content-type = %s %q", r.Method, r.Header.Get("Content-Type"))
		}
		// Echo the request's own name/private back so each call asserts its
		// payload end to end instead of both calls reading a canned body.
		var echo map[string]any
		_ = json.Unmarshal(raw, &echo)
		name, _ := echo["name"].(string)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"full_name":"%s","html_url":"https://github.com/%s","clone_url":"https://github.com/%s.git","default_branch":"main","private":%v}`, name, name, name, echo["private"])))
	}))
	defer server.Close()

	created, err := createGitHubRepo(context.Background(), server.URL, "t", "", "app", true, true)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/user/repos" {
		t.Fatalf("personal create path = %q", gotPath)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(gotBody), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["name"] != "app" || payload["private"] != true || payload["auto_init"] != true {
		t.Fatalf("payload = %v", payload)
	}
	if created.FullName != "app" || created.CloneURL != "https://github.com/app.git" || created.DefaultBranch != "main" || !created.Private {
		t.Fatalf("created = %+v", created)
	}

	created, err = createGitHubRepo(context.Background(), server.URL, "t", "octo", "lib", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/orgs/octo/repos" {
		t.Fatalf("org create path = %q", gotPath)
	}
	if created.FullName != "lib" || created.CloneURL != "https://github.com/lib.git" || created.Private {
		t.Fatalf("org create parsed its own payload: %+v", created)
	}
}

func TestCreateGitHubRepoErrorsAreExplainable(t *testing.T) {
	status := http.StatusUnprocessableEntity
	body := `{"message":"Repository creation failed.","errors":["name already exists on this account"]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	_, err := createGitHubRepo(context.Background(), server.URL, "t", "", "app", true, true)
	if err == nil || !strings.Contains(err.Error(), "已被占用") {
		t.Fatalf("422 not mapped to taken: %v", err)
	}

	// Other failures keep GitHub's own explanation instead of the friendly
	// swap, so a permission problem is diagnosable.
	status = http.StatusForbidden
	body = `{"message":"Not Found"}`
	_, err = createGitHubRepo(context.Background(), server.URL, "t", "octo", "lib", false, false)
	if err == nil || !strings.Contains(err.Error(), "HTTP 403") {
		t.Fatalf("403 lost its status: %v", err)
	}
}

func TestFetchGitLabNamespaces(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/namespaces" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("PRIVATE-TOKEN"); got != "glpat" {
			t.Fatalf("private-token = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"id":42,"name":"ada","path":"ada","full_path":"ada","kind":"user"},
			{"id":7,"name":"Acme","path":"acme","full_path":"acme","kind":"group"},
			{"id":0,"name":"bad","path":"bad","full_path":"bad","kind":"group"}
		]`))
	}))
	defer server.Close()

	rows, err := FetchGitLabNamespaces(context.Background(), server.URL, "glpat", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Kind != "user" || rows[0].ID != 42 || rows[1].FullPath != "acme" {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestGitLabProjectNameStatus(t *testing.T) {
	var probeURI string
	// net/http decodes r.URL.Path, so the handler matches the decoded form;
	// the escaped owner%2Fname form is asserted on the raw request URI.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probeURI = r.URL.RequestURI()
		switch r.URL.Path {
		case "/api/v4/projects/acme/app":
			_, _ = w.Write([]byte(`{"id":1}`))
		case "/api/v4/projects/acme/free":
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	available, reason, err := GitLabProjectNameStatus(context.Background(), server.URL, "glpat", "acme/app")
	if err != nil || available || reason == "" {
		t.Fatalf("taken = %v %q %v", available, reason, err)
	}
	if probeURI != "/api/v4/projects/acme%2Fapp" {
		t.Fatalf("probe uri = %q", probeURI)
	}
	available, reason, err = GitLabProjectNameStatus(context.Background(), server.URL, "glpat", "acme/free")
	if err != nil || !available || reason != "" {
		t.Fatalf("free = %v %q %v", available, reason, err)
	}
}

func TestCreateGitLabProject(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		if r.Method != http.MethodPost || r.URL.Path != "/api/v4/projects" {
			t.Fatalf("method/path = %s %q", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"path_with_namespace":"acme/app","web_url":"https://gitlab.example.com/acme/app","http_url_to_repo":"https://gitlab.example.com/acme/app.git","default_branch":"main","visibility":"private"}`))
	}))
	defer server.Close()

	created, err := CreateGitLabProject(context.Background(), server.URL, "glpat", "app", "app", 7, true, true)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(gotBody), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["namespace_id"] != float64(7) || payload["visibility"] != "private" || payload["initialize_with_readme"] != true {
		t.Fatalf("payload = %v", payload)
	}
	if created.PathWithNamespace != "acme/app" || created.HTTPURLToRepo != "https://gitlab.example.com/acme/app.git" {
		t.Fatalf("created = %+v", created)
	}

	// A missing namespace id is a local rejection, not a network call.
	if _, err := CreateGitLabProject(context.Background(), server.URL, "glpat", "app", "app", 0, true, true); err == nil {
		t.Fatal("namespace 0 accepted")
	}
}

func TestCreateGitLabProjectTakenMapsToFriendly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":{"path":["has already been taken"]}}`))
	}))
	defer server.Close()

	_, err := CreateGitLabProject(context.Background(), server.URL, "glpat", "app", "app", 7, true, true)
	if err == nil || !strings.Contains(err.Error(), "已被占用") {
		t.Fatalf("taken not mapped: %v", err)
	}
}

func TestGitLabScopesHaveCreate(t *testing.T) {
	cases := map[string]bool{
		"api":               true,
		"api,read_user":     true,
		"write_repository":  false,
		"read_user":         false,
		"":                  true, // unknown stays optimistic, like GitLabScopesHaveRepo
		"read_api":          false,
		" write_repository": false,
	}
	for scopes, want := range cases {
		if got := GitLabScopesHaveCreate(scopes); got != want {
			t.Fatalf("GitLabScopesHaveCreate(%q) = %v want %v", scopes, got, want)
		}
	}
}
