package cloudagent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
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

// ConnectGit during a running turn used to leave the host on the bare
// workspace. The next tool has to fold those files into the checkout and
// write the rest there.
func TestSyncRootFoldsLooseFilesIntoCheckout(t *testing.T) {
	dir := t.TempDir()
	workspace := filepath.Join(dir, "workspace")
	if err := os.MkdirAll(filepath.Join(workspace, "snake"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "snake", "game.cpp"), []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	host := &agentHost{
		inst:   &instance{dir: dir, meta: Spec{ID: "cag_sync", Kind: KindCoding}},
		root:   workspace,
		coding: true,
	}
	host.syncRoot()
	if host.root != workspace {
		t.Fatalf("root before connect = %s", host.root)
	}
	repo := filepath.Join(workspace, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeGit(dir, gitSecret{Token: "bind-token-01234567", Clone: "https://github.com/acme/repo.git", Repo: "acme/repo"}); err != nil {
		t.Fatal(err)
	}
	host.syncRoot()
	if host.root != repo {
		t.Fatalf("root after connect = %s", host.root)
	}
	raw, err := os.ReadFile(filepath.Join(repo, "snake", "game.cpp"))
	if err != nil || string(raw) != "before\n" {
		t.Fatalf("folded file %q %v", raw, err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "snake", "game.cpp")); !os.IsNotExist(err) {
		t.Fatalf("loose file still outside the checkout: %v", err)
	}
	_, path, _, _, _, _, _, _ := (&Runtime{}).execWriteFile(host.inst, host.root, `{"path":"snake/food.cpp","content":"after\n"}`)
	if path != filepath.Join(repo, "snake", "food.cpp") {
		t.Fatalf("write landed at %s", path)
	}

	// The next turn is constructed with root already equal to the published
	// checkout. A file left in the parent workspace still has to fold in.
	if err := os.WriteFile(filepath.Join(workspace, "later.txt"), []byte("later\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	host.syncRoot()
	if host.root != repo {
		t.Fatalf("root while already on the checkout = %s", host.root)
	}
	raw, err = os.ReadFile(filepath.Join(repo, "later.txt"))
	if err != nil || string(raw) != "later\n" {
		t.Fatalf("later = %q %v", raw, err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "later.txt")); !os.IsNotExist(err) {
		t.Fatalf("later still outside the checkout: %v", err)
	}
	raw, err = os.ReadFile(filepath.Join(repo, "snake", "game.cpp"))
	if err != nil || string(raw) != "before\n" {
		t.Fatalf("folded file after a second sync = %q %v", raw, err)
	}

	// Disconnect widens the published jail back to the workspace. This turn
	// keeps the checkout and does not pull the new sibling into it.
	if err := os.Remove(gitPath(dir)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "stay.txt"), []byte("stay\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	host.syncRoot()
	if host.root != repo {
		t.Fatalf("disconnect widened root to %s", host.root)
	}
	if _, err := os.Stat(filepath.Join(repo, "stay.txt")); !os.IsNotExist(err) {
		t.Fatalf("disconnect moved stay.txt: %v", err)
	}
	raw, err = os.ReadFile(filepath.Join(workspace, "stay.txt"))
	if err != nil || string(raw) != "stay\n" {
		t.Fatalf("stay = %q %v", raw, err)
	}

	// An empty root adopts the checkout and folds what is still outside.
	if err := writeGit(dir, gitSecret{Token: "bind-token-01234567", Clone: "https://github.com/acme/repo.git", Repo: "acme/repo"}); err != nil {
		t.Fatal(err)
	}
	host.root = ""
	host.syncRoot()
	if host.root != repo {
		t.Fatalf("empty root adopted %s", host.root)
	}
	raw, err = os.ReadFile(filepath.Join(repo, "stay.txt"))
	if err != nil || string(raw) != "stay\n" {
		t.Fatalf("empty root left stay = %q %v", raw, err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "stay.txt")); !os.IsNotExist(err) {
		t.Fatalf("stay still outside after adopt: %v", err)
	}
}

// A workspace write and a ConnectGit publish share one jail. The write does
// not hold the lock for the whole call — a file fetch must stay possible —
// but the publish waits until that write has landed, then folds it into the
// checkout. The turn's next write and read use that checkout.
func TestConnectGitWaitsForInFlightToolThenTurnAdoptsCheckout(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatal("git is not installed")
	}
	bare := seedBare(t)
	pointCloneAt(t, bare)
	rt := openRuntime(t)
	coding := putKind(t, rt, KindCoding)
	inst, err := rt.instance(coding.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(inst.dir, "workspace")
	end := inst.beginJailWork(nil)
	released := false
	t.Cleanup(func() {
		if !released {
			end()
		}
	})
	var waitOnce sync.Once
	waiting := make(chan struct{})
	jailInflightWaitHook = func() {
		waitOnce.Do(func() { close(waiting) })
	}
	t.Cleanup(func() { jailInflightWaitHook = nil })
	entered := make(chan struct{})
	jailConnectHook = func() { close(entered) }
	t.Cleanup(func() { jailConnectHook = nil })
	done := make(chan error, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		_, err := rt.ConnectGit(context.Background(), coding.ID, demoConnect())
		done <- err
	}()
	t.Cleanup(func() {
		if !released {
			released = true
			end()
		}
		select {
		case <-finished:
		case <-time.After(20 * time.Second):
		}
	})
	select {
	case err := <-done:
		t.Fatalf("ConnectGit finished while a workspace write was still in flight: %v", err)
	case <-entered:
		t.Fatal("ConnectGit published while a workspace write was still in flight")
	case <-waiting:
	case <-time.After(30 * time.Second):
		t.Fatal("ConnectGit did not wait for the in-flight workspace write")
	}
	if err := os.MkdirAll(filepath.Join(workspace, "snake"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "snake", "game.cpp"), []byte("int game;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
		t.Fatal("ConnectGit published before the in-flight write finished")
	default:
	}
	released = true
	end()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(workspace, "demo")
	if raw, err := os.ReadFile(filepath.Join(repo, "snake", "game.cpp")); err != nil || string(raw) != "int game;\n" {
		t.Fatalf("connect left game.cpp outside the checkout: %q %v", raw, err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "snake", "game.cpp")); !os.IsNotExist(err) {
		t.Fatalf("game.cpp still beside the checkout: %v", err)
	}
	host := &agentHost{rt: rt, inst: inst, root: workspace, coding: true, turn: context.Background()}
	if out, failed := host.runTool("write_file", `{"path":"snake/food.cpp","content":"after\n"}`); failed {
		t.Fatalf("write: %s", out)
	}
	if host.root != repo {
		t.Fatalf("turn root = %s", host.root)
	}
	if raw, err := os.ReadFile(filepath.Join(repo, "snake", "food.cpp")); err != nil || string(raw) != "after\n" {
		t.Fatalf("food.cpp = %q %v", raw, err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "snake", "food.cpp")); !os.IsNotExist(err) {
		t.Fatalf("food.cpp written beside the checkout: %v", err)
	}
	out, failed := host.runTool("read_file", `{"path":"snake/game.cpp"}`)
	if failed || !strings.Contains(out, "int game;") {
		t.Fatalf("read = %s failed=%v", out, failed)
	}
}

// A clone that already has snake/ must absorb the agent's snake/game.cpp.
// Renaming the whole directory to snake.pre-checkout drops the book key.
func TestMergeWorkspaceLooseRecursesSharedDirectory(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	dest := filepath.Join(workspace, "demo")
	writeTree := func(path, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeTree(filepath.Join(dest, "snake", "other.hpp"), "clone\n")
	writeTree(filepath.Join(dest, "README.md"), "hello\n")
	writeTree(filepath.Join(workspace, "snake", "game.cpp"), "int game;\n")
	writeTree(filepath.Join(workspace, "snake", "other.hpp"), "loose hpp\n")
	writeTree(filepath.Join(workspace, "snake", "src", "deep.cpp"), "deep\n")
	writeTree(filepath.Join(workspace, "README.md"), "loose readme\n")
	writeTree(filepath.Join(workspace, "zombie", ".git", "HEAD"), "ref: refs/heads/main\n")
	writeTree(filepath.Join(workspace, ".git", "HEAD"), "nope\n")

	mergeWorkspaceLoose(workspace, dest, "", "cag_merge")

	read := func(path, want string) {
		t.Helper()
		raw, err := os.ReadFile(path)
		if err != nil || string(raw) != want {
			t.Fatalf("%s = %q %v", path, raw, err)
		}
	}
	read(filepath.Join(dest, "snake", "game.cpp"), "int game;\n")
	read(filepath.Join(dest, "snake", "src", "deep.cpp"), "deep\n")
	read(filepath.Join(dest, "snake", "other.hpp"), "clone\n")
	read(filepath.Join(dest, "snake", "other.hpp.pre-checkout"), "loose hpp\n")
	read(filepath.Join(dest, "README.md"), "hello\n")
	read(filepath.Join(dest, "README.md.pre-checkout"), "loose readme\n")
	read(filepath.Join(dest, "zombie", ".git", "HEAD"), "ref: refs/heads/main\n")
	read(filepath.Join(workspace, ".git", "HEAD"), "nope\n")
	if _, err := os.Stat(filepath.Join(dest, ".git")); !os.IsNotExist(err) {
		t.Fatalf("top-level .git entered the clone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "snake")); !os.IsNotExist(err) {
		t.Fatalf("loose snake still outside the checkout: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "zombie")); !os.IsNotExist(err) {
		t.Fatalf("zombie stayed outside the checkout: %v", err)
	}
}

// A directory symlink in the clone must not receive the loose tree.
func TestMergeWorkspaceLooseDoesNotFollowDirectoryLink(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(root, "outside")
	workspace := filepath.Join(root, "workspace")
	dest := filepath.Join(workspace, "demo")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dest, "snake")
	if err := os.Symlink(outside, link); err != nil {
		// A directory junction does not need the symlink privilege. Go reports
		// it with the same mode bit plainDir refuses to walk.
		out, jerr := exec.Command("cmd", "/c", "mklink", "/J", link, outside).CombinedOutput()
		if jerr != nil {
			t.Skip("symlink: " + err.Error() + "; junction: " + strings.TrimSpace(string(out)))
		}
	}
	loose := filepath.Join(workspace, "snake")
	if err := os.MkdirAll(loose, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(loose, "game.cpp"), []byte("int game;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mergeWorkspaceLoose(workspace, dest, "", "cag_link")
	if _, err := os.Stat(filepath.Join(outside, "game.cpp")); err == nil {
		t.Fatal("followed a directory symlink out of the checkout")
	}
	raw, err := os.ReadFile(filepath.Join(dest, "snake.pre-checkout", "game.cpp"))
	if err != nil || string(raw) != "int game;\n" {
		t.Fatalf("parked link collision = %q %v", raw, err)
	}
}

// A loose directory whose name is a file in the clone parks wholesale.
// Merging a directory into a file would destroy the clone's file.
func TestMergeWorkspaceLooseParksDirectoryOnFile(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	dest := filepath.Join(workspace, "demo")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "snake"), []byte("clone-file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	loose := filepath.Join(workspace, "snake")
	if err := os.MkdirAll(loose, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(loose, "game.cpp"), []byte("int game;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mergeWorkspaceLoose(workspace, dest, "", "cag_file")
	raw, err := os.ReadFile(filepath.Join(dest, "snake"))
	if err != nil || string(raw) != "clone-file\n" {
		t.Fatalf("clone file = %q %v", raw, err)
	}
	raw, err = os.ReadFile(filepath.Join(dest, "snake.pre-checkout", "game.cpp"))
	if err != nil || string(raw) != "int game;\n" {
		t.Fatalf("parked directory = %q %v", raw, err)
	}
}

func initRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Seed", "GIT_AUTHOR_EMAIL=seed@example.com",
			"GIT_COMMITTER_NAME=Seed", "GIT_COMMITTER_EMAIL=seed@example.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v %s", strings.Join(args, " "), err, out)
		}
	}
	git("init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "README.md")
	git("commit", "-m", "init")
}

func adoptedBinding() gitSecret {
	return gitSecret{
		Service: "github", Origin: "https://github.com", Repo: "acme/other",
		Clone: "https://github.com/acme/other.git", Token: "git-secret-token",
		AuthorName: "Ada", AuthorEmail: "ada@example.com",
	}
}

// The turn already writes the checkout it adopted. Commit and the HTTP
// path must not share one root: jailRoot can name a different directory
// while this turn's files still live in the adopted one.
func TestTurnGitCommitUsesAdoptedRootNotJailRoot(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatal("git is not installed")
	}
	rt := openRuntime(t)
	spec := putKind(t, rt, KindCoding)
	inst, err := rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(inst.dir, "workspace")
	adopted := filepath.Join(workspace, "demo")
	decoy := filepath.Join(workspace, "other")
	initRepo(t, adopted)
	initRepo(t, decoy)
	if err := writeGit(inst.dir, adoptedBinding()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(adopted, "turned.txt"), []byte("from-turn\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(decoy, "decoy.txt"), []byte("from-decoy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	published, err := jailRoot(inst.dir, KindCoding)
	if err != nil || filepath.Clean(published) != filepath.Clean(decoy) {
		t.Fatalf("published jail = %s err=%v", published, err)
	}
	host := &agentHost{rt: rt, inst: inst, root: adopted, coding: true, turn: context.Background()}
	out, failed := host.runTool("git_commit", `{"message":"from the turn"}`)
	if failed {
		t.Fatal(out)
	}
	if filepath.Clean(host.root) != filepath.Clean(adopted) {
		t.Fatalf("turn root moved to %s", host.root)
	}
	if got := gitOut(t, "-C", adopted, "ls-files", "--", "turned.txt"); got != "turned.txt" {
		t.Fatalf("adopted checkout was not committed: %q", got)
	}
	if got := gitOut(t, "-C", decoy, "ls-files", "--", "decoy.txt"); got != "" {
		t.Fatalf("turn commit used the published jail: %q", got)
	}
	if _, err := rt.Commit(context.Background(), spec.ID, "published jail"); err != nil {
		t.Fatal(err)
	}
	if got := gitOut(t, "-C", decoy, "ls-files", "--", "decoy.txt"); got != "decoy.txt" {
		t.Fatalf("HTTP commit missed the published jail: %q", got)
	}
	if got := gitOut(t, "-C", adopted, "log", "-1", "--format=%s"); got != "from the turn" {
		t.Fatalf("adopted log = %q", got)
	}
}

func TestTurnGitCommitFailsClosedWithoutBinding(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatal("git is not installed")
	}
	rt := openRuntime(t)
	spec := putKind(t, rt, KindCoding)
	inst, err := rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	adopted := filepath.Join(inst.dir, "workspace", "demo")
	initRepo(t, adopted)
	if err := os.WriteFile(filepath.Join(adopted, "turned.txt"), []byte("from-turn\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	host := &agentHost{rt: rt, inst: inst, root: adopted, coding: true, turn: context.Background()}
	out, failed := host.runTool("git_commit", `{"message":"from the turn"}`)
	if !failed || !strings.Contains(out, "还没有连接代码库") {
		t.Fatalf("commit without binding: failed=%v out=%s", failed, out)
	}
	if got := gitOut(t, "-C", adopted, "ls-files", "--", "turned.txt"); got != "" {
		t.Fatalf("unbound turn committed: %q", got)
	}
}

func TestTurnGitCommitRejectsRootOutsideWorkspace(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatal("git is not installed")
	}
	rt := openRuntime(t)
	spec := putKind(t, rt, KindCoding)
	inst, err := rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	decoy := filepath.Join(inst.dir, "workspace", "other")
	initRepo(t, decoy)
	if err := writeGit(inst.dir, adoptedBinding()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(decoy, "decoy.txt"), []byte("from-decoy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	initRepo(t, outside)
	if err := os.WriteFile(filepath.Join(outside, "escaped.txt"), []byte("nope\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	host := &agentHost{rt: rt, inst: inst, root: outside, coding: true, turn: context.Background()}
	out, failed := host.runTool("git_commit", `{"message":"from the turn"}`)
	if !failed || !strings.Contains(out, "路径超出工作区") {
		t.Fatalf("outside root: failed=%v out=%s", failed, out)
	}
	if got := gitOut(t, "-C", outside, "ls-files", "--", "escaped.txt"); got != "" {
		t.Fatalf("commit left the workspace: %q", got)
	}
	if got := gitOut(t, "-C", decoy, "ls-files", "--", "decoy.txt"); got != "" {
		t.Fatalf("rejected root fell back to the published jail: %q", got)
	}
}

// A shared directory must not pour a loose .git into the clone's object store.
func TestMergeWorkspaceLooseDoesNotMixGitDirectories(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	dest := filepath.Join(workspace, "demo")
	write := func(path, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(dest, "snake", ".git", "HEAD"), "real\n")
	write(filepath.Join(workspace, "snake", ".git", "objects", "x"), "loose-object\n")
	write(filepath.Join(workspace, "snake", "game.cpp"), "int game;\n")

	mergeWorkspaceLoose(workspace, dest, "", "cag_gitdir")

	raw, err := os.ReadFile(filepath.Join(dest, "snake", ".git", "HEAD"))
	if err != nil || string(raw) != "real\n" {
		t.Fatalf("clone HEAD = %q %v", raw, err)
	}
	if _, err := os.Stat(filepath.Join(dest, "snake", ".git", "objects", "x")); !os.IsNotExist(err) {
		t.Fatalf("loose git object merged into the clone: %v", err)
	}
	raw, err = os.ReadFile(filepath.Join(workspace, "snake", ".git", "objects", "x"))
	if err != nil || string(raw) != "loose-object\n" {
		t.Fatalf("loose git dir = %q %v", raw, err)
	}
	if _, err := os.Stat(filepath.Join(dest, "snake", ".git.pre-checkout")); !os.IsNotExist(err) {
		t.Fatalf("loose git dir parked inside the work tree: %v", err)
	}
	raw, err = os.ReadFile(filepath.Join(dest, "snake", "game.cpp"))
	if err != nil || string(raw) != "int game;\n" {
		t.Fatalf("game.cpp = %q %v", raw, err)
	}
}

// git.json is the jail's publication bit. Loose files must stay at their
// open-jail paths when that write fails, and a retry must still pick them up.
func TestConnectGitWriteFailureRestoresLooseFiles(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatal("git is not installed")
	}
	bare := seedBare(t)
	pointCloneAt(t, bare)
	rt := openRuntime(t)
	coding := putKind(t, rt, KindCoding)
	dir := instanceDir(rt, coding)
	workspace := filepath.Join(dir, "workspace")
	write := func(path, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(workspace, "snake", "game.cpp"), "int game;\n")
	write(filepath.Join(workspace, "demo", "mine.cpp"), "mine\n")

	connectGitWriteHook = func() error { return errors.New("git.json 写入失败") }
	t.Cleanup(func() { connectGitWriteHook = nil })
	if _, err := rt.ConnectGit(context.Background(), coding.ID, demoConnect()); err == nil || !strings.Contains(err.Error(), "git.json 写入失败") {
		t.Fatalf("write failure = %v", err)
	}
	read := func(path, want string) {
		t.Helper()
		raw, err := os.ReadFile(path)
		if err != nil || string(raw) != want {
			t.Fatalf("%s = %q %v", path, raw, err)
		}
	}
	read(filepath.Join(workspace, "snake", "game.cpp"), "int game;\n")
	read(filepath.Join(workspace, "demo", "mine.cpp"), "mine\n")
	if _, err := os.Stat(gitPath(dir)); !os.IsNotExist(err) {
		t.Fatalf("git.json published after a failed write: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "demo", ".git")); !os.IsNotExist(err) {
		t.Fatalf("unpublished checkout left in place: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "demo", "snake", "game.cpp")); !os.IsNotExist(err) {
		t.Fatalf("file stranded inside the unpublished checkout: %v", err)
	}

	connectGitWriteHook = nil
	if _, err := rt.ConnectGit(context.Background(), coding.ID, demoConnect()); err != nil {
		t.Fatal(err)
	}
	read(filepath.Join(workspace, "demo", "snake", "game.cpp"), "int game;\n")
	read(filepath.Join(workspace, "demo", "mine.cpp"), "mine\n")
	readme, err := os.ReadFile(filepath.Join(workspace, "demo", "README.md"))
	if err != nil || strings.TrimSpace(string(readme)) != "hello" {
		t.Fatalf("README = %q %v", readme, err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "snake", "game.cpp")); !os.IsNotExist(err) {
		t.Fatalf("retry left game.cpp outside the checkout: %v", err)
	}
}

func TestWriteFileReplacesWithoutLeavingTmp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "git.json")
	if err := os.WriteFile(path, []byte("old-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(path, []byte("new-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "new-token\n" {
		t.Fatalf("replaced = %q %v", raw, err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("tmp left beside the replaced file: %v", err)
	}
	fresh := filepath.Join(dir, "meta.json")
	if err := writeFile(fresh, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(fresh)
	if err != nil || string(raw) != "{}\n" {
		t.Fatalf("created = %q %v", raw, err)
	}
	if _, err := os.Stat(fresh + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("tmp left beside the new file: %v", err)
	}
}
