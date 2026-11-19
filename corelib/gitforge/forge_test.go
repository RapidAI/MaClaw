package gitforge

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseRejectsATokenAndAcceptsBothForges(t *testing.T) {
	if _, err := Parse("https://x-access-token:ghp_secret@github.com/acme/app", "", ""); err == nil {
		t.Fatal("token URL was accepted")
	}
	if _, err := Parse("https://evil.example/acme/app", "", ""); err == nil {
		t.Fatal("unknown host was accepted")
	}
	gh, err := Parse("https://github.com/acme/app.git", "", "")
	if err != nil || gh.Service != GitHub || gh.Clone != "https://github.com/acme/app.git" || gh.API != "https://api.github.com" {
		t.Fatalf("github: %+v %v", gh, err)
	}
	gl, err := Parse("group/sub/app", GitLab, "https://gitlab.example.com")
	if err != nil || gl.Clone != "https://gitlab.example.com/group/sub/app.git" || gl.API != "https://gitlab.example.com/api/v4" {
		t.Fatalf("gitlab: %+v %v", gl, err)
	}
	if _, err := Parse("group/app", GitLab, "http://gitlab.example.com"); err == nil {
		t.Fatal("plain http GITLAB_HOST was accepted")
	}
}

func TestScriptKeepsTheTokenOutOfTheCommand(t *testing.T) {
	repo, err := Parse("acme/app", GitHub, "")
	if err != nil {
		t.Fatal(err)
	}
	script, err := Script("clone", repo, "/home/desktop/work/bot/run/app", "main", "", "GITHUB_TOKEN")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(script, "ghp_") || !strings.Contains(script, "https://github.com/acme/app.git") || !strings.Contains(script, `$GITHUB_TOKEN`) || !strings.Contains(script, "x-access-token") {
		t.Fatalf("clone script: %s", script)
	}
	gl, err := Parse("acme/app", GitLab, "")
	if err != nil {
		t.Fatal(err)
	}
	push, err := Script("push", gl, "/home/desktop/work/bot/run/app", "fix-login", "", "GITLAB_TOKEN")
	if err != nil || !strings.Contains(push, "oauth2") || strings.Contains(push, "glpat-") || !strings.Contains(push, "push -u origin 'fix-login'") {
		t.Fatalf("push script: %s %v", push, err)
	}
	if _, err := Script("push", gl, "/home/desktop/work/bot/run/app", "", "", "GITLAB_TOKEN"); err == nil {
		t.Fatal("push without a branch was accepted")
	}
	if _, err := Script("commit", repo, "/tmp/app", "", "hello\nthere", "GITHUB_TOKEN"); err == nil {
		t.Fatal("multiline message was accepted")
	}
}

func TestCommitScriptRefusesSecretFiles(t *testing.T) {
	script, err := Script("commit", Repo{}, "/home/desktop/work/bot/app", "", "fix login", "")
	if err != nil || !strings.Contains(script, "refusing to commit secret files") || !strings.Contains(script, ".env.example") || !strings.Contains(script, "reset -q") {
		t.Fatalf("commit script: %v\n%s", err, script)
	}
	bash := testBash(t)
	dir := filepath.ToSlash(t.TempDir())
	runGit(t, dir, "init")
	writeRepoFile(t, dir, "README", "hello\n")
	runGit(t, dir, "-c", "user.email=t@example.com", "-c", "user.name=Test", "add", "README")
	runGit(t, dir, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "-m", "init")
	writeRepoFile(t, dir, ".env", "TOKEN=secret\n")
	writeRepoFile(t, dir, ".env.example", "TOKEN=\n")
	writeRepoFile(t, dir, "src/config.pem", "-----BEGIN-----\n")
	writeRepoFile(t, dir, "main.go", "package main\n")
	script, err = Script("commit", Repo{}, dir, "", "fix login", "")
	if err != nil {
		t.Fatal(err)
	}
	out, runErr := runBash(t, bash, dir, script)
	refused := refusedSecretFiles(out)
	if runErr == nil || !containsAll(refused, ".env", "src/config.pem") || containsAny(refused, ".env.example") {
		t.Fatalf("guard refused=%v out=%s err=%v", refused, out, runErr)
	}
	staged := splitLines(runGit(t, dir, "diff", "--cached", "--name-only"))
	if containsAny(staged, ".env", "src/config.pem") || !containsAll(staged, "main.go", ".env.example") {
		t.Fatalf("index after refusal: %v", staged)
	}
	clean := filepath.ToSlash(t.TempDir())
	runGit(t, clean, "init")
	writeRepoFile(t, clean, "README", "hello\n")
	runGit(t, clean, "-c", "user.email=t@example.com", "-c", "user.name=Test", "add", "README")
	runGit(t, clean, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "-m", "init")
	writeRepoFile(t, clean, ".env.example", "TOKEN=\n")
	writeRepoFile(t, clean, "main.go", "package main\n")
	script, err = Script("commit", Repo{}, clean, "", "fix login", "")
	if err != nil {
		t.Fatal(err)
	}
	out, runErr = runBash(t, bash, clean, script)
	if runErr != nil {
		t.Fatalf("example env was refused: %s %v", out, runErr)
	}
	names := runGit(t, clean, "show", "--name-only", "--pretty=", "HEAD")
	if !strings.Contains(names, ".env.example") || !strings.Contains(names, "main.go") {
		t.Fatalf("commit files: %s", names)
	}
}

func splitLines(out string) []string {
	var names []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			names = append(names, line)
		}
	}
	return names
}

func refusedSecretFiles(out string) []string {
	const marker = "refusing to commit secret files:"
	i := strings.Index(out, marker)
	if i < 0 {
		return nil
	}
	var names []string
	for _, line := range strings.Split(out[i+len(marker):], "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			names = append(names, line)
		}
	}
	return names
}

func containsAll(names []string, want ...string) bool {
	for _, item := range want {
		found := false
		for _, name := range names {
			if name == item {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func containsAny(names []string, want ...string) bool {
	for _, item := range want {
		for _, name := range names {
			if name == item {
				return true
			}
		}
	}
	return false
}

func testBash(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	if path, err := exec.LookPath("bash"); err == nil {
		return path
	}
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("bash not installed")
	}
	for _, candidate := range []string{
		filepath.Join(filepath.Dir(git), "bash.exe"),
		filepath.Join(filepath.Dir(filepath.Dir(git)), "bin", "bash.exe"),
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	t.Skip("bash not installed")
	return ""
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %s %v", strings.Join(args, " "), out, err)
	}
	return string(out)
}

func writeRepoFile(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(filepath.FromSlash(dir), filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runBash(t *testing.T, bash, dir, script string) (string, error) {
	t.Helper()
	path := filepath.Join(filepath.FromSlash(dir), "commit.sh")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bash, path)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestIssuesAndReleaseStayOnTheAPI(t *testing.T) {
	var sawAuth, sawRelease bool
	const token = "ghp_testtokenvalue1234567890"
	client := &Client{Token: token, HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Errorf("auth %q", r.Header.Get("Authorization"))
		}
		sawAuth = true
		switch {
		case r.URL.Path == "/repos/acme/app/issues":
			return jsonResponse(`[{"number":7,"title":"crash on empty","html_url":"https://github.com/acme/app/issues/7","updated_at":"2026-11-15T00:00:00Z"},{"number":8,"title":"pr","pull_request":{}}]`), nil
		case r.URL.Path == "/repos/acme/app/actions/workflows/ci.yml/dispatches" && r.Method == http.MethodPost:
			return &http.Response{StatusCode: 204, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
		case r.URL.Path == "/repos/acme/app/releases" && r.Method == http.MethodPost:
			sawRelease = true
			raw, _ := io.ReadAll(r.Body)
			if strings.Contains(string(raw), token) {
				t.Fatal("token was in the release body")
			}
			return jsonResponse(`{"html_url":"https://github.com/acme/app/releases/tag/v1.2.0","tag_name":"v1.2.0"}`), nil
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.String())
			return jsonResponse(`{"message":"no"}`), nil
		}
	})}}
	repo, err := Parse("acme/app", GitHub, "")
	if err != nil {
		t.Fatal(err)
	}
	// Point the client at the transport by rewriting API through a custom host
	// the transport ignores: call() uses repo.API. Swap it to a local root
	// the RoundTrip still sees as that host.
	repo.API = "https://api.github.com"
	text, err := client.Issues(context.Background(), repo, "open", 10)
	if err != nil || !strings.Contains(text, "#7 crash on empty") || strings.Contains(text, "pr") || strings.Contains(text, token) {
		t.Fatalf("issues: %s %v", text, err)
	}
	got, err := client.Dispatch(context.Background(), repo, "ci.yml", "main")
	if err != nil || !strings.Contains(got, "ci.yml") {
		t.Fatalf("dispatch: %s %v", got, err)
	}
	rel, err := client.Release(context.Background(), repo, "v1.2.0", "v1.2.0", "fixes #7", "main")
	if err != nil || !strings.Contains(rel, "releases/tag/v1.2.0") || !sawRelease || !sawAuth {
		t.Fatalf("release: %s %v", rel, err)
	}
	denied := &Client{Token: token, HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader(`{"message":"bad ` + token + `"}`)), Header: make(http.Header)}, nil
	})}}
	_, err = denied.WhoAmI(context.Background(), repo)
	if err == nil || !strings.Contains(err.Error(), "HTTP 401") || strings.Contains(err.Error(), token) {
		t.Fatalf("401 leaked or hid the status: %v", err)
	}
}

func TestSyncFetchesTheBaseWithoutPuttingTheTokenInTheCommand(t *testing.T) {
	repo, err := Parse("acme/app", GitHub, "")
	if err != nil {
		t.Fatal(err)
	}
	const token = "ghp_testtokenvalue1234567890"
	script, err := Script("sync", repo, "/home/desktop/work/bot/app", "origin/main", "", "GITHUB_TOKEN")
	if err != nil || strings.Contains(script, token) || !strings.Contains(script, "fetch origin '+refs/heads/main:refs/remotes/origin/main'") || !strings.Contains(script, "merge --no-edit 'origin/main'") || !strings.Contains(script, "user.email=maclaw-bot@local") || !strings.Contains(script, "status --short") || !strings.Contains(script, "$GITHUB_TOKEN") {
		t.Fatalf("sync: %s %v", script, err)
	}
	if _, err := Script("sync", repo, "/home/desktop/work/bot/app", "main:refs/heads/other", "", "GITHUB_TOKEN"); err == nil {
		t.Fatal("refspec was accepted as a branch")
	}
	for _, ref := range []string{"refs/heads/main", "refs/remotes/origin/main"} {
		full, fullErr := Script("sync", repo, "/home/desktop/work/bot/app", ref, "", "GITHUB_TOKEN")
		if fullErr != nil || !strings.Contains(full, "fetch origin '+refs/heads/main:refs/remotes/origin/main'") {
			t.Fatalf("%s: %s %v", ref, full, fullErr)
		}
	}
}

func TestBranchDiffAndUploadStayFreeOfTheToken(t *testing.T) {
	repo, err := Parse("acme/app", GitHub, "")
	if err != nil {
		t.Fatal(err)
	}
	branch, err := Script("branch", repo, "/home/desktop/work/bot/app", "fix-login", "", "")
	if err != nil || !strings.Contains(branch, "switch -c 'fix-login'") || !strings.Contains(branch, "|| git -C '/home/desktop/work/bot/app' switch 'fix-login'") || strings.Contains(branch, "GITHUB_TOKEN") {
		t.Fatalf("branch: %s %v", branch, err)
	}
	const token = "ghp_testtokenvalue1234567890"
	script, err := UploadScript(GitHub, "GITHUB_TOKEN", "/home/desktop/work/bot/app/app.tar.gz", "https://uploads.github.com/repos/acme/app/releases/1/assets?name=app.tar.gz", "app.tar.gz", false)
	if err != nil || strings.Contains(script, token) || !strings.Contains(script, "${GITHUB_TOKEN}") || !strings.Contains(script, "67108864") {
		t.Fatalf("upload: %s %v", script, err)
	}
	if err := AllowedUpload(repo, "https://evil.example/assets"); err == nil {
		t.Fatal("foreign upload host was accepted")
	}
}

func TestPullRequestAndFailedStep(t *testing.T) {
	const token = "ghp_testtokenvalue1234567890"
	client := &Client{Token: token, HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/repos/acme/app/pulls":
			if r.Method == http.MethodGet {
				return jsonResponse(`[]`), nil
			}
			raw, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(raw), `"head":"fix-login"`) || !strings.Contains(string(raw), `"base":"main"`) {
				t.Fatalf("pull body %s", raw)
			}
			return jsonResponse(`{"html_url":"https://github.com/acme/app/pull/9"}`), nil
		case "/repos/acme/app/actions/runs/4/jobs":
			return jsonResponse(`{"jobs":[{"name":"test","conclusion":"failure","check_run_url":"https://api.github.com/repos/acme/app/check-runs/77","steps":[{"name":"go test","conclusion":"failure"},{"name":"setup","conclusion":"success"}]}]}`), nil
		case "/repos/acme/app/check-runs/77/annotations":
			return jsonResponse(`[{"path":"login.go","start_line":42,"message":"undefined: password"}]`), nil
		default:
			t.Errorf("path %s", r.URL.Path)
			return jsonResponse(`{}`), nil
		}
	})}}
	repo, err := Parse("acme/app", GitHub, "")
	if err != nil {
		t.Fatal(err)
	}
	text, err := client.PullRequest(context.Background(), repo, "fix-login", "main", "Fix empty login", "closes #3")
	if err != nil || !strings.Contains(text, "pull/9") || strings.Contains(text, token) {
		t.Fatalf("pr: %s %v", text, err)
	}
	logText, err := client.RunLog(context.Background(), repo, "4")
	if err != nil || !strings.Contains(logText, "failed step: go test") || !strings.Contains(logText, "login.go:42 undefined: password") || strings.Contains(logText, "setup") {
		t.Fatalf("log: %s %v", logText, err)
	}
}

func TestExistingPullIsKeptAndMergeLandsIt(t *testing.T) {
	const token = "ghp_testtokenvalue1234567890"
	var posts, puts int
	client := &Client{Token: token, HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/app/pulls":
			if !strings.Contains(r.URL.RawQuery, "head=acme%3Afix-login") && !strings.Contains(r.URL.RawQuery, "head=acme:fix-login") {
				t.Errorf("query %s", r.URL.RawQuery)
			}
			return jsonResponse(`[{"number":9,"html_url":"https://github.com/acme/app/pull/9","head":{"ref":"fix-login"},"base":{"ref":"main"}}]`), nil
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/app/pulls":
			posts++
			return jsonResponse(`{}`), nil
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/app/pulls/9":
			return jsonResponse(`{"head":{"sha":"0123456789abcdef0123456789abcdef01234567"}}`), nil
		case r.Method == http.MethodPut && r.URL.Path == "/repos/acme/app/pulls/9/merge":
			puts++
			raw, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(raw), `"sha":"0123456789abcdef0123456789abcdef01234567"`) {
				t.Errorf("merge body %s", raw)
			}
			return jsonResponse(`{"merged":true,"sha":"abc123"}`), nil
		case r.URL.Path == "/repos/acme/app/actions/runs":
			if !strings.Contains(r.URL.RawQuery, "branch=fix-login") {
				t.Errorf("runs query %s", r.URL.RawQuery)
			}
			return jsonResponse(`{"workflow_runs":[{"id":4,"status":"completed","conclusion":"failure","head_branch":"fix-login","name":"ci","html_url":"https://github.com/acme/app/actions/runs/4"}]}`), nil
		case r.URL.Path == "/repos/acme/app/issues/3":
			return jsonResponse(`{"title":"login crash","state":"open","body":"step one\nstep two","html_url":"https://github.com/acme/app/issues/3"}`), nil
		case r.URL.Path == "/repos/acme/app/issues/3/comments":
			if !strings.Contains(r.URL.RawQuery, "direction=desc") {
				t.Errorf("comments query %s", r.URL.RawQuery)
			}
			return jsonResponse(`[{"body":"steps: open login","user":{"login":"octo"}},{"body":"note two","user":{"login":"a"}},{"body":"note three","user":{"login":"a"}},{"body":"note four","user":{"login":"a"}},{"body":"note five","user":{"login":"a"}},{"body":"old issue note","user":{"login":"old"}}]`), nil
		default:
			t.Errorf("path %s %s", r.Method, r.URL.Path)
			return jsonResponse(`{}`), nil
		}
	})}}
	repo, err := Parse("acme/app", GitHub, "")
	if err != nil {
		t.Fatal(err)
	}
	text, err := client.PullRequest(context.Background(), repo, "fix-login", "main", "Fix empty login", "")
	if err != nil || posts != 0 || !strings.Contains(text, "already open") || !strings.Contains(text, "pull/9") || strings.Contains(text, token) {
		t.Fatalf("existing: %s posts=%d %v", text, posts, err)
	}
	merged, err := client.Merge(context.Background(), repo, "9")
	if err != nil || puts != 1 || !strings.Contains(merged, "merged #9") || !strings.Contains(merged, "abc123") || strings.Contains(merged, token) {
		t.Fatalf("merge: %s puts=%d %v", merged, puts, err)
	}
	runs, err := client.Runs(context.Background(), repo, "fix-login")
	if err != nil || !strings.Contains(runs, "fix-login") || !strings.Contains(runs, "failure") || strings.Contains(runs, "completed") || strings.Contains(runs, token) {
		t.Fatalf("runs: %s %v", runs, err)
	}
	issue, err := client.Issue(context.Background(), repo, "3")
	if err != nil || !strings.Contains(issue, "step one\nstep two") || !strings.Contains(issue, "steps: open login") || !strings.Contains(issue, "octo") || strings.Contains(issue, "old issue note") || strings.Contains(issue, token) {
		t.Fatalf("issue: %s %v", issue, err)
	}
}

func TestRequestShowsWhetherItCanMergeAndTheReview(t *testing.T) {
	const token = "ghp_testtokenvalue1234567890"
	client := &Client{Token: token, HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/repos/acme/app/pulls/9":
			return jsonResponse(`{"number":9,"title":"Fix empty login","state":"open","draft":false,"mergeable":false,"mergeable_state":"blocked","html_url":"https://github.com/acme/app/pull/9","head":{"ref":"fix-login"},"base":{"ref":"main"}}`), nil
		case "/repos/acme/app/pulls/9/reviews":
			return jsonResponse(`[{"state":"COMMENTED","body":"old note one","user":{"login":"a"}},{"state":"COMMENTED","body":"old note two","user":{"login":"a"}},{"state":"COMMENTED","body":"old note three","user":{"login":"a"}},{"state":"COMMENTED","body":"old note four","user":{"login":"a"}},{"state":"COMMENTED","body":"old note five","user":{"login":"a"}},{"state":"CHANGES_REQUESTED","body":"handle the empty password","user":{"login":"octo"}}]`), nil
		case "/repos/acme/app/pulls/9/comments":
			return jsonResponse(`[{"body":"still panics","path":"login.go","line":42,"user":{"login":"octo"}}]`), nil
		default:
			t.Errorf("path %s", r.URL.Path)
			return jsonResponse(`[]`), nil
		}
	})}}
	repo, err := Parse("acme/app", GitHub, "")
	if err != nil {
		t.Fatal(err)
	}
	text, err := client.Request(context.Background(), repo, "9")
	if err != nil || !strings.Contains(text, "mergeable no (blocked)") || !strings.Contains(text, "CHANGES_REQUESTED") || strings.Contains(text, "old note one") || !strings.Contains(text, "login.go:42") || strings.Contains(text, token) {
		t.Fatalf("request: %s %v", text, err)
	}
}

func TestRequestReadsAgainWhenMergeabilityIsNotComputedYet(t *testing.T) {
	previous := requestMergeWait
	requestMergeWait = 0
	t.Cleanup(func() { requestMergeWait = previous })
	const token = "ghp_testtokenvalue1234567890"
	var reads int
	client := &Client{Token: token, HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/repos/acme/app/pulls/9":
			reads++
			if reads == 1 {
				return jsonResponse(`{"number":9,"title":"Fix","state":"open","mergeable":null,"mergeable_state":"unknown","html_url":"https://github.com/acme/app/pull/9","head":{"ref":"fix"},"base":{"ref":"main"}}`), nil
			}
			return jsonResponse(`{"number":9,"title":"Fix","state":"open","mergeable":false,"mergeable_state":"behind","html_url":"https://github.com/acme/app/pull/9","head":{"ref":"fix"},"base":{"ref":"main"}}`), nil
		case "/repos/acme/app/pulls/9/reviews", "/repos/acme/app/pulls/9/comments":
			return jsonResponse(`[]`), nil
		default:
			t.Errorf("path %s", r.URL.Path)
			return jsonResponse(`{}`), nil
		}
	})}}
	repo, err := Parse("acme/app", GitHub, "")
	if err != nil {
		t.Fatal(err)
	}
	text, err := client.Request(context.Background(), repo, "9")
	if err != nil || reads != 2 || !strings.Contains(text, "mergeable no (behind)") || strings.Contains(text, token) {
		t.Fatalf("request: %s reads=%d %v", text, reads, err)
	}
}

func TestGitLabOAuthUploadUsesBearer(t *testing.T) {
	script, err := UploadScript(GitLab, "GITLAB_TOKEN", "/home/desktop/work/bot/app/app.tar.gz", "https://gitlab.example.com/api/v4/projects/1/packages/generic/release/v1/app.tar.gz", "app.tar.gz", true)
	if err != nil || strings.Contains(script, "PRIVATE-TOKEN") || !strings.Contains(script, "Authorization: Bearer ${GITLAB_TOKEN}") || !strings.Contains(script, "-X PUT") {
		t.Fatalf("upload: %s %v", script, err)
	}
	pat, err := UploadScript(GitLab, "GITLAB_TOKEN", "/home/desktop/work/bot/app/app.tar.gz", "https://gitlab.example.com/api/v4/projects/1/packages/generic/release/v1/app.tar.gz", "app.tar.gz", false)
	if err != nil || !strings.Contains(pat, "PRIVATE-TOKEN: ${GITLAB_TOKEN}") || strings.Contains(pat, "Authorization: Bearer") {
		t.Fatalf("pat upload: %s %v", pat, err)
	}
}

func TestGitHubLogHostIsTheSignedLogStore(t *testing.T) {
	if !githubLogHost("productionresultssa0.blob.core.windows.net") || !githubLogHost("objects.githubusercontent.com") {
		t.Fatal("log store was rejected")
	}
	if githubLogHost("127.0.0.1") || githubLogHost("169.254.169.254") || githubLogHost("localhost") || githubLogHost("evil.example") || githubLogHost("blob.core.windows.net.evil.example") {
		t.Fatal("log host allowlist accepted something else")
	}
}

func TestGitHubJobLogFollowsTheSignedURLWithoutTheToken(t *testing.T) {
	const token = "ghp_testtokenvalue1234567890"
	var logs *httptest.Server
	logs = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("PRIVATE-TOKEN") != "" {
			t.Error("token was sent to the log host")
		}
		switch r.URL.Path {
		case "/log":
			w.Header().Set("Location", logs.URL+"/file?sig=supersecret")
			w.WriteHeader(http.StatusFound)
		case "/file":
			if r.URL.Query().Get("sig") != "supersecret" {
				t.Errorf("query %s", r.URL.RawQuery)
			}
			w.WriteHeader(http.StatusPartialContent)
			_, _ = io.WriteString(w, "setup ok\n--- FAIL: TestLogin\npassword is nil\n")
		default:
			t.Errorf("log path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(logs.Close)
	previousHost := githubLogHostAllowed
	githubLogHostAllowed = func(host string) bool { return host == "127.0.0.1" }
	previousClient := githubLogClient
	githubLogClient = func() *http.Client { return logs.Client() }
	t.Cleanup(func() {
		githubLogHostAllowed = previousHost
		githubLogClient = previousClient
	})
	api := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Errorf("api auth %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/repos/acme/app/actions/runs/4/jobs":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"jobs":[{"id":5,"name":"test","conclusion":"failure","steps":[{"name":"go test","conclusion":"failure"}]}]}`)
		case "/repos/acme/app/actions/jobs/5/logs":
			w.Header().Set("Location", logs.URL+"/log?sig=hop")
			w.WriteHeader(http.StatusFound)
		default:
			t.Errorf("path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(api.Close)
	client := &Client{Token: token, HTTP: api.Client()}
	repo := Repo{Service: GitHub, API: api.URL, Path: "acme/app", Web: "https://github.com/acme/app"}
	text, err := client.RunLog(context.Background(), repo, "4")
	if err != nil || !strings.Contains(text, "failed step: go test") || !strings.Contains(text, "password is nil") || strings.Contains(text, "supersecret") || strings.Contains(text, token) || strings.Contains(text, logs.URL) {
		t.Fatalf("log: %s %v", text, err)
	}
}

func TestPullRequestBaseDropsTheRemotePrefix(t *testing.T) {
	const token = "ghp_testtokenvalue1234567890"
	var posted string
	client := &Client{Token: token, HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodGet {
			return jsonResponse(`[]`), nil
		}
		raw, _ := io.ReadAll(r.Body)
		posted = string(raw)
		return jsonResponse(`{"html_url":"https://github.com/acme/app/pull/3"}`), nil
	})}}
	repo, err := Parse("acme/app", GitHub, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, base := range []string{"refs/heads/main", "origin/main", "refs/remotes/origin/main"} {
		posted = ""
		text, err := client.PullRequest(context.Background(), repo, "fix-login", base, "Fix", "")
		if err != nil || !strings.Contains(posted, `"base":"main"`) || strings.Contains(posted, "refs/") || strings.Contains(text, token) {
			t.Fatalf("%s: %s body=%s %v", base, text, posted, err)
		}
	}
}

func TestDispatchAcceptsAWorkflowPathAndRejectsANestedFile(t *testing.T) {
	const token = "ghp_testtokenvalue1234567890"
	var path string
	client := &Client{Token: token, HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		path = r.URL.EscapedPath()
		return jsonResponse(`{}`), nil
	})}}
	repo, err := Parse("acme/app", GitHub, "")
	if err != nil {
		t.Fatal(err)
	}
	text, err := client.Dispatch(context.Background(), repo, ".github/workflows/ci.yml", "main")
	if err != nil || path != "/repos/acme/app/actions/workflows/ci.yml/dispatches" || strings.Contains(text, token) {
		t.Fatalf("dispatch: %s path=%s %v", text, path, err)
	}
	if _, err := client.Dispatch(context.Background(), repo, "nested/ci.yml", "main"); err == nil {
		t.Fatal("nested workflow file was accepted")
	}
}

func TestGitLabPipelineUsesThePrivateToken(t *testing.T) {
	const token = "glpat-testtokenvalue123456"
	client := &Client{Token: token, HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("PRIVATE-TOKEN") != token {
			t.Errorf("token header %q", r.Header.Get("PRIVATE-TOKEN"))
		}
		if r.URL.RawPath != "/projects/group%2Fapp/pipelines" && r.URL.Path != "/projects/group/app/pipelines" && !strings.Contains(r.URL.EscapedPath(), "group%2Fapp") {
			t.Errorf("path %s", r.URL.EscapedPath())
		}
		return jsonResponse(`[{"id":4,"status":"success","ref":"main","web_url":"https://gitlab.com/group/app/-/pipelines/4"}]`), nil
	})}}
	repo, err := Parse("https://gitlab.com/group/app", "", "")
	if err != nil {
		t.Fatal(err)
	}
	text, err := client.Runs(context.Background(), repo, "")
	if err != nil || !strings.Contains(text, "#4 success main") || strings.Contains(text, token) {
		t.Fatalf("runs: %s %v", text, err)
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func jsonResponse(body string) *http.Response {
	h := make(http.Header)
	h.Set("Content-Type", "application/json")
	return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(strings.NewReader(body))}
}
