package cloudagent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestProgrammingGitRejectsSessionAndInsecureURL(t *testing.T) {
	rt := openRuntime(t)
	session := putKind(t, rt, KindSession)
	in := GitConnect{Service: "github", Origin: "https://github.com", Repo: "acme/demo", Token: "git-secret-token", AuthorName: "Ada", AuthorEmail: "ada@example.com"}
	if _, err := rt.ConnectGit(context.Background(), session.ID, in); !errors.Is(err, errSessionGit) {
		t.Fatalf("session git err=%v", err)
	}
	if _, err := os.Stat(gitPath(instanceDir(rt, session))); !os.IsNotExist(err) {
		t.Fatalf("session git.json: %v", err)
	}
	coding := putKind(t, rt, KindCoding)
	for _, bad := range []GitConnect{
		{Service: "github", Origin: "https://github.com", Repo: "http://github.com/acme/demo", Token: "git-secret-token", AuthorName: "Ada", AuthorEmail: "ada@example.com"},
		{Service: "github", Origin: "https://github.com", Repo: "git@github.com:acme/demo.git", Token: "git-secret-token", AuthorName: "Ada", AuthorEmail: "ada@example.com"},
		{Service: "github", Origin: "http://github.com/acme/demo", Repo: "acme/demo", Token: "git-secret-token", AuthorName: "Ada", AuthorEmail: "ada@example.com"},
		{Service: "github", Origin: "git@github.com:acme/demo.git", Repo: "acme/demo", Token: "git-secret-token", AuthorName: "Ada", AuthorEmail: "ada@example.com"},
		{Service: "github", Origin: "https://github.com", Repo: "acme/demo", Token: "git-secret-token", AuthorName: "Ada", AuthorEmail: "maclaw-bot@local"},
	} {
		if _, err := rt.ConnectGit(context.Background(), coding.ID, bad); err == nil {
			t.Fatalf("accepted unsafe git connect %+v", bad)
		}
	}
	if _, err := os.Stat(gitPath(instanceDir(rt, coding))); !os.IsNotExist(err) {
		t.Fatalf("rejected connect wrote git.json: %v", err)
	}
	script, err := writeAskpass(t.TempDir(), "github", "GITHUB_TOKEN")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "git-secret-token") || !strings.Contains(string(raw), "GITHUB_TOKEN") || !strings.Contains(string(raw), "x-access-token") {
		t.Fatalf("askpass script = %s", raw)
	}
}

func TestProgrammingGitCheckoutCommitAndGatedPush(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatal("git is not installed")
	}
	bare := seedBare(t)
	pointCloneAt(t, bare)
	rt := openRuntime(t)
	coding := putKind(t, rt, KindCoding)
	other := putKind(t, rt, KindCoding)
	t.Setenv("GIT_AUTHOR_NAME", "maclaw-bot")
	t.Setenv("GIT_AUTHOR_EMAIL", "maclaw-bot@local")
	t.Setenv("GIT_COMMITTER_NAME", "maclaw-bot")
	t.Setenv("GIT_COMMITTER_EMAIL", "maclaw-bot@local")
	t.Setenv("GITHUB_TOKEN", "git-secret-token")
	t.Setenv("SIDE_CHANNEL", "git-secret-token")
	globalBefore := globalGitIdentity(t)

	view, err := rt.ConnectGit(context.Background(), coding.ID, demoConnect())
	if err != nil {
		t.Fatal(err)
	}
	if !view.Connected || view.Repo != "acme/demo" || strings.Contains(mustJSON(t, view), "git-secret-token") {
		t.Fatalf("view = %+v", view)
	}
	dir := instanceDir(rt, coding)
	repo := filepath.Join(dir, "workspace", "demo")
	secretRaw, err := os.ReadFile(gitPath(dir))
	if err != nil || !strings.Contains(string(secretRaw), "git-secret-token") {
		t.Fatalf("git.json = %s %v", secretRaw, err)
	}
	meta, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil || strings.Contains(string(meta), "git-secret-token") {
		t.Fatalf("meta leaked: %s", meta)
	}
	if book, err := os.ReadFile(filepath.Join(dir, "transcript.db")); err == nil && strings.Contains(string(book), "git-secret-token") {
		t.Fatal("token is in the transcript")
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "askpass-*"))
	if len(matches) != 0 {
		t.Fatalf("askpass left behind: %v", matches)
	}
	if treeContains(instanceDir(rt, other), "git-secret-token") {
		t.Fatal("token leaked into another instance")
	}
	loaded := secretValues(dir)
	for _, kv := range bashEnv(loaded...) {
		if strings.Contains(kv, "git-secret-token") || strings.HasPrefix(kv, "GITHUB_TOKEN=") || strings.HasPrefix(kv, "SIDE_CHANNEL=") {
			t.Fatalf("shell env leaked: %s", kv)
		}
	}
	for _, kv := range gitEnv(loaded) {
		if strings.Contains(kv, "git-secret-token") || strings.HasPrefix(kv, "GITHUB_TOKEN=") || strings.HasPrefix(kv, "SIDE_CHANNEL=") {
			t.Fatalf("git env leaked: %s", kv)
		}
	}
	if _, err := rt.ConnectGit(context.Background(), coding.ID, GitConnect{
		Service: "github", Origin: "https://github.com", Repo: "acme/other", Token: "git-secret-token", AuthorName: "Ada", AuthorEmail: "ada@example.com",
	}); err == nil {
		t.Fatal("second repository was accepted")
	}

	seed := gitOut(t, "--git-dir", bare, "rev-parse", "refs/heads/main")
	if _, err := approvePush(context.Background(), nil, repo, gitSecret{Token: "git-secret-token"}, nil); err == nil || gitOut(t, "--git-dir", bare, "rev-parse", "refs/heads/main") != seed {
		t.Fatalf("nil gate err=%v", err)
	}

	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hello\nchanged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	receipt, err := rt.Commit(context.Background(), coding.ID, "repo local author")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(receipt, "commit ") || gitOut(t, "--git-dir", bare, "rev-parse", "refs/heads/main") != seed {
		t.Fatalf("commit pushed or failed: %s bare=%s", receipt, seed)
	}
	if got := gitOut(t, "-C", repo, "log", "-1", "--format=%an%n%ae"); got != "Ada\nada@example.com" {
		t.Fatalf("commit author = %q", got)
	}
	if got := gitOut(t, "-C", repo, "config", "--local", "--get", "user.email"); got != "ada@example.com" {
		t.Fatalf("local email = %q", got)
	}
	if got := gitOut(t, "-C", repo, "config", "--local", "--get", "user.name"); got != "Ada" {
		t.Fatalf("local name = %q", got)
	}
	if globalGitIdentity(t) != globalBefore {
		t.Fatalf("global git identity changed: %q -> %q", globalBefore, globalGitIdentity(t))
	}
	local := gitOut(t, "-C", repo, "rev-parse", "HEAD")

	denied := newTask(t, rt, coding.ID)
	text, err := rt.Push(context.Background(), coding.ID, denied)
	if err == nil || !strings.Contains(text, "需要你的确认") || gitOut(t, "--git-dir", bare, "rev-parse", "refs/heads/main") != seed {
		t.Fatalf("ungated push text=%s err=%v", text, err)
	}
	inst, err := rt.instance(coding.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := inst.book.RecordAnswer(denied, "拒绝"); err != nil {
		t.Fatal(err)
	}
	text, err = rt.Push(context.Background(), coding.ID, denied)
	if err == nil || strings.Contains(text, "需要你的确认") || !strings.Contains(text, "已拒绝") {
		t.Fatalf("denied push text=%s err=%v", text, err)
	}
	if gitOut(t, "--git-dir", bare, "rev-parse", "refs/heads/main") != seed {
		t.Fatal("denied push published")
	}

	once := newTask(t, rt, coding.ID)
	if _, err := rt.Push(context.Background(), coding.ID, once); err == nil {
		t.Fatal("push ran before the answer")
	}
	if err := inst.book.RecordAnswer(once, "仅这一次"); err != nil {
		t.Fatal(err)
	}
	pushed, err := rt.Push(context.Background(), coding.ID, once)
	if err != nil || !strings.Contains(pushed, local) {
		t.Fatalf("allowed push = %s %v", pushed, err)
	}
	if gitOut(t, "--git-dir", bare, "rev-parse", "refs/heads/main") != local {
		t.Fatal("remote does not match the local commit")
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hello\nagain\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Commit(context.Background(), coding.ID, "second"); err != nil {
		t.Fatal(err)
	}
	second := gitOut(t, "-C", repo, "rev-parse", "HEAD")
	text, err = rt.Push(context.Background(), coding.ID, once)
	if err == nil || !strings.Contains(text, "需要你的确认") || gitOut(t, "--git-dir", bare, "rev-parse", "refs/heads/main") != local {
		t.Fatalf("once grant was reused text=%s err=%v", text, err)
	}

	unknownTask := newTask(t, rt, coding.ID)
	if _, err := rt.Push(context.Background(), coding.ID, unknownTask); err == nil {
		t.Fatal("push ran before the unknown-receipt answer")
	}
	if err := inst.book.RecordAnswer(unknownTask, "本任务内允许"); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "missing.git")
	gitOut(t, "-C", repo, "remote", "set-url", "origin", missing)
	// origin now points at a dead path, but the push is pinned to the stored
	// clone URL: it must still reach the real remote, not the configured one.
	text, err = rt.Push(context.Background(), coding.ID, unknownTask)
	if err != nil || !strings.Contains(text, second) {
		t.Fatalf("pinned push text=%s err=%v", text, err)
	}
	if gitOut(t, "--git-dir", bare, "rev-parse", "refs/heads/main") != second {
		t.Fatal("tampered origin redirected the push")
	}

	fresh := putKind(t, rt, KindCoding)
	if _, err := rt.ConnectGit(context.Background(), fresh.ID, demoConnect()); err != nil {
		t.Fatal(err)
	}
	freshRepo := filepath.Join(instanceDir(rt, fresh), "workspace", "demo")
	gitOut(t, "-C", freshRepo, "branch", "--unset-upstream")
	held := gitOut(t, "--git-dir", bare, "rev-parse", "refs/heads/main")
	unsetTask := newTask(t, rt, fresh.ID)
	freshInst, err := rt.instance(fresh.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Push(context.Background(), fresh.ID, unsetTask); err == nil {
		t.Fatal("unset upstream asked nothing")
	}
	if err := freshInst.book.RecordAnswer(unsetTask, "仅这一次"); err != nil {
		t.Fatal(err)
	}
	text, err = rt.Push(context.Background(), fresh.ID, unsetTask)
	if err == nil || !strings.Contains(err.Error(), "host_repo_mutate_upstream_unset") {
		t.Fatalf("upstream text=%s err=%v", text, err)
	}
	if gitOut(t, "--git-dir", bare, "rev-parse", "refs/heads/main") != held {
		t.Fatal("unset upstream still pushed")
	}
}

func demoConnect() GitConnect {
	return GitConnect{Service: "github", Origin: "https://github.com", Repo: "acme/demo", Token: "git-secret-token", AuthorName: "Ada", AuthorEmail: "ada@example.com"}
}

func putKind(t *testing.T, rt *Runtime, kind string) Spec {
	t.Helper()
	spec := Spec{
		ID: newID("cag_"), TenantID: "tenant-a", OwnerUserID: "user-a", Name: kind, Kind: kind,
		HubOrigin: "https://hub.example", HubLoopback: "http://127.0.0.1:9",
	}
	if err := rt.Put(spec); err != nil {
		t.Fatal(err)
	}
	return spec
}

func instanceDir(rt *Runtime, spec Spec) string {
	return filepath.Join(rt.root, slug(spec.TenantID), slug(spec.OwnerUserID), spec.ID)
}

func newTask(t *testing.T, rt *Runtime, id string) string {
	t.Helper()
	raw, err := rt.Control(context.Background(), id, "user-a", "cloud_create_task", []byte(`{"title":"ship"}`))
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(raw, &body); err != nil || body.TaskID == "" {
		t.Fatalf("task: %s", raw)
	}
	return body.TaskID
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func seedBare(t *testing.T) string {
	t.Helper()
	bare := filepath.Join(t.TempDir(), "origin.git")
	work := filepath.Join(t.TempDir(), "seed")
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		if dir != "" {
			cmd.Dir = dir
		}
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Seed", "GIT_AUTHOR_EMAIL=seed@example.com", "GIT_COMMITTER_NAME=Seed", "GIT_COMMITTER_EMAIL=seed@example.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v %s", strings.Join(args, " "), err, out)
		}
	}
	run("", "init", "--bare", bare)
	run("", "init", "-b", "main", work)
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(work, "add", "README.md")
	run(work, "commit", "-m", "init")
	run(work, "remote", "add", "origin", bare)
	run(work, "push", "-u", "origin", "main")
	// git init --bare leaves HEAD on master. A clone of that remote checks
	// out nothing when the only branch is main.
	run("", "--git-dir", bare, "symbolic-ref", "HEAD", "refs/heads/main")
	return bare
}

func pointCloneAt(t *testing.T, bare string) {
	t.Helper()
	slash := filepath.ToSlash(bare)
	if !strings.HasPrefix(slash, "/") {
		slash = "/" + slash
	}
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "url.file://"+slash+".insteadof")
	t.Setenv("GIT_CONFIG_VALUE_0", "https://github.com/acme/demo.git")
}

func gitOut(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v %s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func globalGitIdentity(t *testing.T) string {
	t.Helper()
	name, _ := exec.Command("git", "config", "--global", "--get", "user.name").Output()
	email, _ := exec.Command("git", "config", "--global", "--get", "user.email").Output()
	return strings.TrimSpace(string(name)) + "\n" + strings.TrimSpace(string(email))
}

func treeContains(root, needle string) bool {
	found := false
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr == nil && strings.Contains(string(raw), needle) {
			found = true
			return errors.New("found")
		}
		return nil
	})
	return found
}

func TestConnectGitMergesPreCheckoutWorkspace(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatal("git is not installed")
	}
	bare := seedBare(t)
	pointCloneAt(t, bare)
	rt := openRuntime(t)
	coding := putKind(t, rt, KindCoding)
	dir := instanceDir(rt, coding)
	workspace := filepath.Join(dir, "workspace")
	// Repo-less work: loose files plus a directory named exactly like the
	// repository all have to survive the connect.
	if err := os.MkdirAll(filepath.Join(workspace, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"main.cpp":       "int main(){}",
		"notes/todo.txt": "ship it",
		"demo/snake.cpp": "// snake",
		"README.md":      "loose readme",
	} {
		path := filepath.Join(workspace, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A scripted .git directory masquerades as a checkout on disk, but
	// git.json is the authority: the connect must still proceed and fold
	// the impostor into the repo as ordinary data.
	zombie := filepath.Join(workspace, "zombie", ".git")
	if err := os.MkdirAll(zombie, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(zombie, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.ConnectGit(context.Background(), coding.ID, demoConnect()); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(workspace, "demo")
	if info, err := os.Stat(filepath.Join(repo, "zombie", ".git", "HEAD")); err != nil || info.IsDir() {
		t.Fatalf("impostor checkout not merged: %v", err)
	}
	for _, rel := range []string{"main.cpp", filepath.Join("notes", "todo.txt"), "snake.cpp"} {
		if _, err := os.Stat(filepath.Join(repo, rel)); err != nil {
			t.Fatalf("merged %s: %v", rel, err)
		}
	}
	// The clone's README keeps its name; the scratch copy is parked beside it
	// inside the jail instead of being deleted or stranded in the workspace.
	raw, err := os.ReadFile(filepath.Join(repo, "README.md"))
	if err != nil || strings.TrimSpace(string(raw)) != "hello" {
		t.Fatalf("README = %q err=%v", raw, err)
	}
	raw, err = os.ReadFile(filepath.Join(repo, "README.md.pre-checkout"))
	if err != nil || strings.TrimSpace(string(raw)) != "loose readme" {
		t.Fatalf("loose README = %q err=%v", raw, err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "README.md")); !os.IsNotExist(err) {
		t.Fatalf("loose README stranded at workspace root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "demo", ".git")); err != nil {
		t.Fatal("checkout lost its .git")
	}
	// The scaffold→connect→commit flow: merged files are untracked, so
	// git_commit must stage them (-A), not just tracked updates.
	receipt, err := rt.Commit(context.Background(), coding.ID, "scaffold")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(receipt, "commit ") {
		t.Fatalf("receipt=%q", receipt)
	}
	if got := gitOut(t, "-C", repo, "ls-files", "--", "main.cpp"); got != "main.cpp" {
		t.Fatalf("merged file not committed: %q", got)
	}
	if got := gitOut(t, "-C", repo, "log", "-1", "--format=%an"); got != "Ada" {
		t.Fatalf("author=%q", got)
	}
}

// A loose FILE named like the repository parks under a unique name instead
// of blocking the connect or being deleted by the stash cleanup.
func TestConnectGitParksLooseFileNamedLikeRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatal("git is not installed")
	}
	bare := seedBare(t)
	pointCloneAt(t, bare)
	rt := openRuntime(t)
	coding := putKind(t, rt, KindCoding)
	dir := instanceDir(rt, coding)
	workspace := filepath.Join(dir, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "demo"), []byte("binary-out"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.ConnectGit(context.Background(), coding.ID, demoConnect()); err != nil {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(filepath.Join(workspace, "demo", "demo.pre-checkout-*"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("parked file glob=%v err=%v", matches, err)
	}
	if raw, err := os.ReadFile(matches[0]); err != nil || string(raw) != "binary-out" {
		t.Fatalf("parked content: %q %v", raw, err)
	}
}

func TestGitConnectSourceRoundTrip(t *testing.T) {
	account, err := parseGitConnect(GitConnect{
		Service: "github", Origin: "https://github.com", Repo: "acme/demo",
		Token: "tok", AuthorName: "Ada", AuthorEmail: "ada@example.com", Source: "account",
	})
	if err != nil {
		t.Fatal(err)
	}
	if account.Source != "account" || account.public().Source != "account" {
		t.Fatalf("account source = %q", account.Source)
	}
	manual, err := parseGitConnect(GitConnect{
		Service: "github", Repo: "acme/demo",
		Token: "tok", AuthorName: "Ada", AuthorEmail: "ada@example.com", Source: "anything-else",
	})
	if err != nil {
		t.Fatal(err)
	}
	if manual.Source != "" || manual.public().Source != "" {
		t.Fatalf("manual source = %q", manual.Source)
	}
	dir := t.TempDir()
	if err := writeGit(dir, account); err != nil {
		t.Fatal(err)
	}
	loaded, err := readGit(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Source != "account" {
		t.Fatalf("git.json source = %q", loaded.Source)
	}
}
