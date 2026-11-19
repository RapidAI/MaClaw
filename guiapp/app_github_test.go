package guiapp

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNormalizeGitHubCloneURL(t *testing.T) {
	cases := []struct {
		name, in, wantURL, wantRepo string
		wantErr                     bool
	}{
		{name: "https plain", in: "https://github.com/octocat/Hello-World", wantURL: "https://github.com/octocat/Hello-World.git", wantRepo: "Hello-World"},
		{name: "https with .git", in: "https://github.com/octocat/Hello-World.git", wantURL: "https://github.com/octocat/Hello-World.git", wantRepo: "Hello-World"},
		{name: "https deep path normalized", in: "https://github.com/octocat/Hello-World/tree/main/docs", wantURL: "https://github.com/octocat/Hello-World.git", wantRepo: "Hello-World"},
		{name: "ssh shorthand", in: "git@github.com:octocat/Hello-World.git", wantURL: "https://github.com/octocat/Hello-World.git", wantRepo: "Hello-World"},
		{name: "ssh scheme", in: "ssh://git@github.com/octocat/Hello-World.git", wantURL: "https://github.com/octocat/Hello-World.git", wantRepo: "Hello-World"},
		{name: "bare host", in: "github.com/octocat/Hello-World", wantURL: "https://github.com/octocat/Hello-World.git", wantRepo: "Hello-World"},
		{name: "uppercase host", in: "GitHub.COM/octocat/Hello-World", wantURL: "https://github.com/octocat/Hello-World.git", wantRepo: "Hello-World"},
		{name: "owner/repo shorthand", in: "octocat/Hello-World", wantURL: "https://github.com/octocat/Hello-World.git", wantRepo: "Hello-World"},
		{name: "empty", in: "", wantErr: true},
		{name: "whitespace only", in: "   ", wantErr: true},
		{name: "not github", in: "https://gitlab.com/octocat/Hello-World", wantErr: true},
		{name: "lookalike host rejected", in: "https://github.com.evil.com/o/r", wantErr: true},
		{name: "no repo path", in: "https://github.com/", wantErr: true},
		{name: "owner only", in: "https://github.com/octocat", wantErr: true},
		{name: "ssh non-github", in: "git@gitlab.com:o/r.git", wantErr: true},
		{name: "ssh scheme non-github", in: "ssh://git@gitlab.com/o/r.git", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotURL, gotRepo, err := normalizeGitHubCloneURL(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got url=%q repo=%q", gotURL, gotRepo)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotURL != tc.wantURL || gotRepo != tc.wantRepo {
				t.Fatalf("got (%q, %q), want (%q, %q)", gotURL, gotRepo, tc.wantURL, tc.wantRepo)
			}
		})
	}
}

func TestGitHubCloneTokenOK(t *testing.T) {
	if err := githubCloneTokenOK(""); err != nil {
		t.Fatal(err)
	}
	if err := githubCloneTokenOK("gho_example.token-1"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"gho bad", "gho\ntoken", "gho:token", "gho\"token", "gho$token", "gho`token"} {
		if err := githubCloneTokenOK(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func TestGitHubCloneEnvDropsInheritedHelpers(t *testing.T) {
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
	}
	anon := githubCloneEnv(base, "")
	joined := strings.Join(anon, "\n")
	if !strings.Contains(joined, "GIT_CONFIG_COUNT=1") || !strings.Contains(joined, "bearer stale") || !strings.Contains(joined, "PATH=C:\\Windows") {
		t.Fatalf("anonymous clone changed the user's git config:\n%s", joined)
	}
	if args := githubCloneAuthArgs(""); len(args) != 2 || args[0] != "-c" || args[1] != "core.longpaths=true" {
		t.Fatalf("anonymous args = %#v", args)
	}

	env := githubCloneEnv(base, token)
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
	if _, ok := got["GIT_CONFIG_COUNT"]; ok {
		t.Fatal("authenticated clone kept GIT_CONFIG_COUNT")
	}
	if strings.Contains(strings.Join(env, "\n"), "stale") || strings.Contains(strings.Join(env, "\n"), "bearer") {
		t.Fatal("env kept a stale Authorization header")
	}
	args := strings.Join(githubCloneAuthArgs(token), "\n")
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

func TestRedactGitHubCloneOutput(t *testing.T) {
	const token = "gho_exampletoken"
	got := redactGitHubCloneOutput("failed "+token+" tail", token)
	if strings.Contains(got, token) || got != "failed *** tail" {
		t.Fatalf("redacted = %q", got)
	}
	if redactGitHubCloneOutput("plain", "") != "plain" {
		t.Fatal("empty token rewrote the output")
	}
}

func TestGitHubCloneHelperFillsAccessToken(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	const token = "gho_exampletoken"
	if err := githubCloneTokenOK(token); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", append(githubCloneAuthArgs(token), "credential", "fill")...)
	cmd.Env = githubCloneEnv([]string{"PATH=" + os.Getenv("PATH")}, token)
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
	narrow := githubCloneEnv([]string{
		"PATH=" + filepath.Dir(gitPath) + ";" + filepath.Join(systemRoot, "System32"),
		"SYSTEMROOT=" + systemRoot,
	}, token)
	where := exec.Command("where", "sh")
	where.Env = narrow
	if out, err := where.CombinedOutput(); err == nil {
		t.Fatalf("narrow PATH still contains sh:\n%s", out)
	}
	cmd := exec.Command("git", append(githubCloneAuthArgs(token), "credential", "fill")...)
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
