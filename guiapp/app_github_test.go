package guiapp

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/oauth"
)

func TestNormalizeForgeCloneURL(t *testing.T) {
	cases := []struct {
		name, in, gitlabOrigin, wantURL, wantRepo, wantService string
		wantErr                                                bool
	}{
		// GitHub forms (unchanged behavior).
		{name: "https plain", in: "https://github.com/octocat/Hello-World", wantURL: "https://github.com/octocat/Hello-World.git", wantRepo: "Hello-World", wantService: cloneServiceGitHub},
		{name: "https with .git", in: "https://github.com/octocat/Hello-World.git", wantURL: "https://github.com/octocat/Hello-World.git", wantRepo: "Hello-World", wantService: cloneServiceGitHub},
		{name: "https deep path normalized", in: "https://github.com/octocat/Hello-World/tree/main/docs", wantURL: "https://github.com/octocat/Hello-World.git", wantRepo: "Hello-World", wantService: cloneServiceGitHub},
		{name: "https tree on .git name", in: "https://github.com/octocat/Hello-World.git/tree/main", wantURL: "https://github.com/octocat/Hello-World.git", wantRepo: "Hello-World", wantService: cloneServiceGitHub},
		{name: "ssh shorthand", in: "git@github.com:octocat/Hello-World.git", wantURL: "https://github.com/octocat/Hello-World.git", wantRepo: "Hello-World", wantService: cloneServiceGitHub},
		{name: "ssh scheme", in: "ssh://git@github.com/octocat/Hello-World.git", wantURL: "https://github.com/octocat/Hello-World.git", wantRepo: "Hello-World", wantService: cloneServiceGitHub},
		{name: "bare host", in: "github.com/octocat/Hello-World", wantURL: "https://github.com/octocat/Hello-World.git", wantRepo: "Hello-World", wantService: cloneServiceGitHub},
		{name: "www bare host", in: "www.github.com/octocat/Hello-World", wantURL: "https://github.com/octocat/Hello-World.git", wantRepo: "Hello-World", wantService: cloneServiceGitHub},
		{name: "uppercase host", in: "GitHub.COM/octocat/Hello-World", wantURL: "https://github.com/octocat/Hello-World.git", wantRepo: "Hello-World", wantService: cloneServiceGitHub},
		{name: "owner/repo shorthand", in: "octocat/Hello-World", wantURL: "https://github.com/octocat/Hello-World.git", wantRepo: "Hello-World", wantService: cloneServiceGitHub},
		// GitLab SaaS forms.
		{name: "gitlab https", in: "https://gitlab.com/gitlab-org/gitlab", wantURL: "https://gitlab.com/gitlab-org/gitlab.git", wantRepo: "gitlab", wantService: cloneServiceGitLab},
		{name: "gitlab nested group", in: "https://gitlab.com/group/sub/repo.git", wantURL: "https://gitlab.com/group/sub/repo.git", wantRepo: "repo", wantService: cloneServiceGitLab},
		{name: "gitlab file link cut", in: "https://gitlab.com/gitlab-org/gitlab/-/blob/main/README.md", wantURL: "https://gitlab.com/gitlab-org/gitlab.git", wantRepo: "gitlab", wantService: cloneServiceGitLab},
		{name: "gitlab ssh shorthand", in: "git@gitlab.com:group/repo.git", wantURL: "https://gitlab.com/group/repo.git", wantRepo: "repo", wantService: cloneServiceGitLab},
		{name: "gitlab ssh scheme", in: "ssh://git@gitlab.com/group/repo.git", wantURL: "https://gitlab.com/group/repo.git", wantRepo: "repo", wantService: cloneServiceGitLab},
		{name: "gitlab bare host", in: "gitlab.com/group/repo", wantURL: "https://gitlab.com/group/repo.git", wantRepo: "repo", wantService: cloneServiceGitLab},
		{name: "gitlab group only", in: "https://gitlab.com/gitlab-org", wantErr: true},
		{name: "gitlab root", in: "https://gitlab.com/", wantErr: true},
		// Self-hosted GitLab: exact saved-origin match only.
		{name: "self-hosted https", in: "https://gitlab.example.com/group/repo", gitlabOrigin: "https://gitlab.example.com", wantURL: "https://gitlab.example.com/group/repo.git", wantRepo: "repo", wantService: cloneServiceGitLab},
		{name: "self-hosted file link", in: "https://gitlab.example.com/group/repo/-/tree/main", gitlabOrigin: "https://gitlab.example.com", wantURL: "https://gitlab.example.com/group/repo.git", wantRepo: "repo", wantService: cloneServiceGitLab},
		{name: "self-hosted ssh", in: "git@gitlab.example.com:group/repo.git", gitlabOrigin: "https://gitlab.example.com", wantURL: "https://gitlab.example.com/group/repo.git", wantRepo: "repo", wantService: cloneServiceGitLab},
		{name: "self-hosted ssh scheme", in: "ssh://git@gitlab.example.com/group/repo.git", gitlabOrigin: "https://gitlab.example.com", wantURL: "https://gitlab.example.com/group/repo.git", wantRepo: "repo", wantService: cloneServiceGitLab},
		{name: "self-hosted bare host", in: "gitlab.example.com/group/repo", gitlabOrigin: "https://gitlab.example.com", wantURL: "https://gitlab.example.com/group/repo.git", wantRepo: "repo", wantService: cloneServiceGitLab},
		{name: "self-hosted bare host with port", in: "gitlab.example.com:8443/group/repo", gitlabOrigin: "https://gitlab.example.com:8443", wantURL: "https://gitlab.example.com:8443/group/repo.git", wantRepo: "repo", wantService: cloneServiceGitLab},
		{name: "self-hosted with port", in: "https://gitlab.example.com:8443/group/repo", gitlabOrigin: "https://gitlab.example.com:8443", wantURL: "https://gitlab.example.com:8443/group/repo.git", wantRepo: "repo", wantService: cloneServiceGitLab},
		{name: "self-hosted uppercase origin", in: "https://GITLAB.EXAMPLE.COM/group/repo", gitlabOrigin: "https://GitLab.Example.com", wantURL: "https://gitlab.example.com/group/repo.git", wantRepo: "repo", wantService: cloneServiceGitLab},
		{name: "self-hosted uppercase ssh", in: "git@GitLab.Example.com:group/repo.git", gitlabOrigin: "https://GitLab.Example.com", wantURL: "https://gitlab.example.com/group/repo.git", wantRepo: "repo", wantService: cloneServiceGitLab},
		{name: "self-hosted port mismatch", in: "https://gitlab.example.com:8443/group/repo", gitlabOrigin: "https://gitlab.example.com", wantErr: true},
		{name: "self-hosted host mismatch", in: "https://gitlab.other.com/group/repo", gitlabOrigin: "https://gitlab.example.com", wantErr: true},
		{name: "self-hosted without connection", in: "https://gitlab.example.com/group/repo", gitlabOrigin: "", wantErr: true},
		// Refusals.
		{name: "empty", in: "", wantErr: true},
		{name: "whitespace only", in: "   ", wantErr: true},
		{name: "lookalike host rejected", in: "https://github.com.evil.com/o/r", wantErr: true},
		{name: "lookalike gitlab rejected", in: "https://gitlab.com.evil.com/g/r", wantErr: true},
		{name: "dotted owner shorthand rejected", in: "github.com.evil.com/o/r", wantErr: true},
		{name: "no repo path", in: "https://github.com/", wantErr: true},
		{name: "owner only", in: "https://github.com/octocat", wantErr: true},
		{name: "ssh non-forge host", in: "git@example.com:o/r.git", wantErr: true},
		{name: "dot segment", in: "https://github.com/octocat/../Hello-World", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotURL, gotRepo, gotService, err := normalizeForgeCloneURL(tc.in, tc.gitlabOrigin)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got url=%q repo=%q service=%q", gotURL, gotRepo, gotService)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotURL != tc.wantURL || gotRepo != tc.wantRepo || gotService != tc.wantService {
				t.Fatalf("got (%q, %q, %q), want (%q, %q, %q)", gotURL, gotRepo, gotService, tc.wantURL, tc.wantRepo, tc.wantService)
			}
		})
	}
}

func TestForgeCloneTokenOK(t *testing.T) {
	if err := forgeCloneTokenOK(""); err != nil {
		t.Fatal(err)
	}
	if err := forgeCloneTokenOK("gho_example.token-1"); err != nil {
		t.Fatal(err)
	}
	if err := forgeCloneTokenOK("glpat-AbC123xyz_-"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"glpat bad", "glpat\ntoken", "glpat:token", "glpat\"token", "glpat$token", "glpat`token"} {
		if err := forgeCloneTokenOK(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func TestForgeCloneEnvDropsInheritedHelpers(t *testing.T) {
	const token = "gho_exampletoken"
	base := []string{
		"PATH=C:\\Windows",
		"GIT_CONFIG_COUNT=1",
		"git_config_key_0=http.extraheader",
		"GIT_CONFIG_VALUE_0=AUTHORIZATION: bearer stale",
		"GIT_CONFIG_PARAMETERS=http.extraheader=bearer",
		"GIT_TERMINAL_PROMPT=1",
		"GCM_INTERACTIVE=auto",
		githubCloneTokenEnv + "=old",
		gitlabCloneTokenEnv + "=older",
	}
	anon := forgeCloneEnv(base, githubCloneTokenEnv, "")
	joined := strings.Join(anon, "\n")
	if !strings.Contains(joined, "GIT_CONFIG_COUNT=1") || !strings.Contains(joined, "bearer stale") || !strings.Contains(joined, "PATH=C:\\Windows") {
		t.Fatalf("anonymous clone changed the user's git config:\n%s", joined)
	}
	if args := forgeCloneAuthArgs(cloneServiceGitHub, "https://github.com/o/r.git", ""); len(args) != 2 || args[0] != "-c" || args[1] != "core.longpaths=true" {
		t.Fatalf("anonymous args = %#v", args)
	}

	env := forgeCloneEnv(base, githubCloneTokenEnv, token)
	got := map[string]string{}
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			t.Fatalf("bad entry %q", entry)
		}
		if _, exists := got[strings.ToUpper(key)]; exists {
			t.Fatalf("duplicate %s", key)
		}
		got[strings.ToUpper(key)] = value
	}
	if got["GIT_TERMINAL_PROMPT"] != "0" || got["GCM_INTERACTIVE"] != "Never" || got[githubCloneTokenEnv] != token {
		t.Fatalf("env flags = %#v", got)
	}
	if got[gitlabCloneTokenEnv] == "older" {
		t.Fatal("authenticated clone kept a stale GitLab token")
	}
	if _, ok := got["GIT_CONFIG_COUNT"]; ok {
		t.Fatal("authenticated clone kept GIT_CONFIG_COUNT")
	}
	if strings.Contains(strings.Join(env, "\n"), "stale") || strings.Contains(strings.Join(env, "\n"), "bearer") {
		t.Fatal("env kept a stale Authorization header")
	}
	args := strings.Join(forgeCloneAuthArgs(cloneServiceGitHub, "https://github.com/o/r.git", token), "\n")
	for _, want := range []string{
		"-c\ncredential.helper=",
		"-c\ncredential.https://github.com.helper=",
		"-c\nhttp.extraheader=",
		"-c\nhttp.https://github.com/.extraheader=",
		"-c\nhttp.proactiveAuth=basic",
		"username=x-access-token",
		`password=$MACLAW_GITHUB_TOKEN`,
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("auth args missing %q:\n%s", want, args)
		}
	}
	if strings.Contains(args, token) || strings.Contains(strings.ToLower(args), "bearer") {
		t.Fatal("auth args contain the token or a bearer header")
	}
	// The helper has to be the last credential.helper assignment. An earlier
	// empty value clears the list, and one after the helper would drop it.
	if idx := strings.LastIndex(args, "credential.helper="); idx < 0 || !strings.Contains(args[idx:], "username=x-access-token") {
		t.Fatal("helper is not the last credential.helper value")
	}
}

func TestForgeCloneAuthArgsGitLab(t *testing.T) {
	const token = "glpat_exampletoken"
	args := strings.Join(forgeCloneAuthArgs(cloneServiceGitLab, "https://gitlab.example.com/g/r.git", token), "\n")
	for _, want := range []string{
		"-c\ncredential.helper=",
		"-c\ncredential.https://gitlab.example.com.helper=",
		"-c\nhttp.https://gitlab.example.com/.extraheader=",
		"username=oauth2",
		`password=$MACLAW_GITLAB_TOKEN`,
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("gitlab auth args missing %q:\n%s", want, args)
		}
	}
	if strings.Contains(args, token) {
		t.Fatal("auth args contain the token")
	}
	if idx := strings.LastIndex(args, "credential.helper="); idx < 0 || !strings.Contains(args[idx:], "username=oauth2") {
		t.Fatal("helper is not the last credential.helper value")
	}
}

func TestCloneCredentialsNeverLeavesTheSavedOrigin(t *testing.T) {
	a := &App{}
	saved := &oauth.StoredCredential{BaseURL: "https://gitlab.example.com", AccessToken: "glpat_saved"}
	// Matching origin → the saved PAT.
	token, env := a.cloneCredentials(cloneServiceGitLab, "https://gitlab.example.com/g/r.git", saved)
	if token != "glpat_saved" || env != gitlabCloneTokenEnv {
		t.Fatalf("matching origin: token=%q env=%q", token, env)
	}
	// A different host (only reachable via a mismatched normalize result)
	// never receives the token.
	token, _ = a.cloneCredentials(cloneServiceGitLab, "https://gitlab.com/g/r.git", saved)
	if token != "" {
		t.Fatalf("foreign host got the token %q", token)
	}
	// No connected account → anonymous.
	token, _ = a.cloneCredentials(cloneServiceGitLab, "https://gitlab.example.com/g/r.git", nil)
	if token != "" {
		t.Fatalf("anonymous clone got a token %q", token)
	}
}

func TestRedactCloneOutput(t *testing.T) {
	const token = "gho_exampletoken"
	got := redactCloneOutput("failed "+token+" tail", token)
	if strings.Contains(got, token) || got != "failed *** tail" {
		t.Fatalf("redacted = %q", got)
	}
	if redactCloneOutput("plain", "") != "plain" {
		t.Fatal("empty token rewrote the output")
	}
}

func TestGitHubCloneHelperFillsAccessToken(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	const token = "gho_exampletoken"
	if err := forgeCloneTokenOK(token); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", append(forgeCloneAuthArgs(cloneServiceGitHub, "https://github.com/o/r.git", token), "credential", "fill")...)
	cmd.Env = forgeCloneEnv([]string{"PATH=" + os.Getenv("PATH")}, githubCloneTokenEnv, token)
	cmd.Stdin = strings.NewReader("protocol=https\nhost=github.com\n\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("credential fill: %v\n%s", err, out)
	}
	text := string(out)
	if !strings.Contains(text, "username=x-access-token") || !strings.Contains(text, "password="+token) {
		t.Fatalf("fill = %q", text)
	}
	if strings.Contains(text, "bearer") {
		t.Fatalf("fill used bearer: %q", text)
	}
}

func TestGitLabCloneHelperFillsAccessToken(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	const token = "glpat_exampletoken"
	if err := forgeCloneTokenOK(token); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", append(forgeCloneAuthArgs(cloneServiceGitLab, "https://gitlab.com/g/r.git", token), "credential", "fill")...)
	cmd.Env = forgeCloneEnv([]string{"PATH=" + os.Getenv("PATH")}, gitlabCloneTokenEnv, token)
	cmd.Stdin = strings.NewReader("protocol=https\nhost=gitlab.com\n\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("credential fill: %v\n%s", err, out)
	}
	text := string(out)
	if !strings.Contains(text, "username=oauth2") || !strings.Contains(text, "password="+token) {
		t.Fatalf("fill = %q", text)
	}
}

// TestGitHubCloneHelperFillsWithoutShOnPath is the desktop case: Git\cmd is
// on PATH and sh.exe is not. credential fill still has to return the token,
// because cmd\git.exe injects Git\usr\bin before it runs a `!` helper.
func TestGitHubCloneHelperFillsWithoutShOnPath(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Git for Windows cmd wrapper")
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	systemRoot := os.Getenv("SYSTEMROOT")
	if systemRoot == "" {
		systemRoot = `C:\Windows`
	}
	const token = "gho_exampletoken"
	narrow := forgeCloneEnv([]string{
		"PATH=" + filepath.Dir(gitPath) + ";" + filepath.Join(systemRoot, "System32"),
		"SYSTEMROOT=" + systemRoot,
	}, githubCloneTokenEnv, token)
	where := exec.Command("where", "sh")
	where.Env = narrow
	if out, err := where.CombinedOutput(); err == nil {
		t.Fatalf("narrow PATH still contains sh:\n%s", out)
	}
	cmd := exec.Command("git", append(forgeCloneAuthArgs(cloneServiceGitHub, "https://github.com/o/r.git", token), "credential", "fill")...)
	cmd.Env = narrow
	cmd.Stdin = strings.NewReader("protocol=https\nhost=github.com\n\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("credential fill without sh on PATH: %v\n%s", err, out)
	}
	text := string(out)
	if !strings.Contains(text, "username=x-access-token") || !strings.Contains(text, "password="+token) {
		t.Fatalf("fill = %q", text)
	}
}
