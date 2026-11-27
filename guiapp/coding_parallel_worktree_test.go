package guiapp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func skipWithoutGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
}

func initParallelRepo(t *testing.T) string {
	t.Helper()
	skipWithoutGit(t)
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "init")
	mustGit(t, repo, "config", "user.email", "test@example.com")
	mustGit(t, repo, "config", "user.name", "test")
	mustGit(t, repo, "config", "core.autocrlf", "false")
	writeParallelFile(t, repo, "README", "base\n")
	mustGit(t, repo, "add", "README")
	mustGit(t, repo, "commit", "-m", "init")
	return repo
}

func writeParallelFile(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readParallelFile(t *testing.T, dir, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(body)
}

func commitParallelFile(t *testing.T, repo, name, body string) {
	t.Helper()
	writeParallelFile(t, repo, name, body)
	mustGit(t, repo, "add", name)
	mustGit(t, repo, "commit", "-m", name)
}

func parallelHead(t *testing.T, repo string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", repo, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

type parallelPair struct {
	app         *App
	handler     *IMMessageHandler
	repo        string
	holder      ProjectSearchResult
	later       ProjectSearchResult
	holderOwner string
	laterOwner  string
	turn        *codingParallelTurn
}

func newParallelPair(t *testing.T, repo string) *parallelPair {
	t.Helper()
	app := newProjectSearchTestApp(t)
	holder := app.createTaskRecordWithWorkingDir("holder", "", []string{taskManagementTag, taskUserCreatedTag}, repo, true)
	later := app.createTaskRecordWithWorkingDir("later", "", []string{taskManagementTag, taskUserCreatedTag}, repo, true)
	if holder.ProjectPath == "" || later.ProjectPath == "" {
		t.Fatal("task record was not created")
	}
	pair := &parallelPair{
		app:         app,
		handler:     &IMMessageHandler{app: app},
		repo:        repo,
		holder:      holder,
		later:       later,
		holderOwner: projectSessionOwnerID(holder.ProjectPath),
		laterOwner:  projectSessionOwnerID(later.ProjectPath),
	}
	t.Cleanup(func() {
		if pair.turn == nil {
			return
		}
		// Owner release is registered after this cleanup, so the holder is
		// already gone and finish does not wait.
		turn := pair.turn
		wt := turn.wt
		turn.finish(context.Background())
		if wt == nil || wt.Path == "" {
			return
		}
		if wt.GitRoot != "" {
			_ = exec.Command("git", "-C", wt.GitRoot, "worktree", "remove", "--force", wt.Path).Run()
			if wt.Branch != "" {
				_ = exec.Command("git", "-C", wt.GitRoot, "branch", "-D", wt.Branch).Run()
			}
		}
		_ = os.RemoveAll(wt.Path)
	})
	return pair
}

func (p *parallelPair) hold(t *testing.T) func() {
	t.Helper()
	beginForegroundAgentOwner(p.holderOwner)
	released := false
	release := func() {
		if released {
			return
		}
		released = true
		endForegroundAgentOwner(p.holderOwner)
	}
	t.Cleanup(release)
	return release
}

func (p *parallelPair) beginLater(t *testing.T, kind codingRequestKind) *codingParallelTurn {
	t.Helper()
	p.turn = p.handler.beginCodingParallelTurn(p.laterOwner, p.repo, kind, nil)
	if p.turn == nil {
		t.Fatal("later turn was not bound")
	}
	return p.turn
}

func assertWaitBlocksUntilRelease(t *testing.T, wait func() error, release func()) {
	t.Helper()
	done := make(chan error, 1)
	started := make(chan struct{})
	go func() {
		close(started)
		done <- wait()
	}()
	<-started
	time.Sleep(150 * time.Millisecond)
	select {
	case err := <-done:
		t.Fatalf("wait returned while the holder was live: %v", err)
	default:
	}
	release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("wait did not return after the holder left")
	}
}

func TestCodingParallelLaterTaskKeepsPrimaryBytes(t *testing.T) {
	repo := initParallelRepo(t)
	commitParallelFile(t, repo, "kept.go", "committed\n")
	writeParallelFile(t, repo, "kept.go", "holder-uncommitted\n")
	writeParallelFile(t, repo, "dirty.txt", "only-on-primary\n")
	head := parallelHead(t, repo)
	pair := newParallelPair(t, repo)
	release := pair.hold(t)
	turn := pair.beginLater(t, codingRequestImplementation)
	if !turn.deferred || turn.wt == nil {
		t.Fatalf("later turn deferred=%v wt=%v", turn.deferred, turn.wt)
	}
	if normalizeProjectSessionPath(turn.execDir) == normalizeProjectSessionPath(repo) || !pathInsideWorkRoot(turn.wt.Path, turn.execDir) {
		t.Fatalf("exec dir = %q, want a worktree of %q", turn.execDir, repo)
	}
	if got := readParallelFile(t, turn.execDir, "kept.go"); got != "committed\n" {
		t.Fatalf("worktree imported the holder's uncommitted edit: %q", got)
	}
	if _, err := os.Stat(filepath.Join(turn.execDir, "dirty.txt")); !os.IsNotExist(err) {
		t.Fatalf("worktree contains the holder's untracked file, stat err=%v", err)
	}
	writeParallelFile(t, turn.execDir, "later.go", "from-later\n")
	if _, err := os.Stat(filepath.Join(repo, "later.go")); !os.IsNotExist(err) {
		t.Fatalf("later write landed on the primary while the holder is live, stat err=%v", err)
	}
	if got := readParallelFile(t, repo, "kept.go"); got != "holder-uncommitted\n" {
		t.Fatalf("primary kept.go = %q", got)
	}
	options := turn.runtimeOptions(&TaskItem{Title: "edit"})
	if options == nil || !options.DeferredWorktree || !options.WorkspaceIsolated || !options.RequireFinalDiffGate {
		t.Fatalf("runtime options = %+v", options)
	}
	if normalizeProjectSessionPath(options.PolicyProjectRoot) != normalizeProjectSessionPath(repo) {
		t.Fatalf("policy root = %q, want %q", options.PolicyProjectRoot, repo)
	}
	if len(options.DeclaredWrites) == 0 {
		t.Fatal("deferred turn dropped the declared write claim")
	}
	release()
	if _, err := turn.integrate(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if got := readParallelFile(t, repo, "later.go"); got != "from-later\n" {
		t.Fatalf("merged later.go = %q", got)
	}
	if got := readParallelFile(t, repo, "kept.go"); got != "holder-uncommitted\n" {
		t.Fatalf("merge rewrote the holder's uncommitted file: %q", got)
	}
	if got := readParallelFile(t, repo, "dirty.txt"); got != "only-on-primary\n" {
		t.Fatalf("merge removed the holder's untracked file: %q", got)
	}
	if parallelHead(t, repo) != head {
		t.Fatal("merge committed the primary")
	}
}

func TestCodingParallelMergesDisjointFiles(t *testing.T) {
	repo := initParallelRepo(t)
	commitParallelFile(t, repo, "a.go", "a-base\n")
	commitParallelFile(t, repo, "b.go", "b-base\n")
	pair := newParallelPair(t, repo)
	release := pair.hold(t)
	writeParallelFile(t, repo, "a.go", "a-holder\n")
	turn := pair.beginLater(t, codingRequestImplementation)
	writeParallelFile(t, turn.execDir, "b.go", "b-later\n")
	writeParallelFile(t, turn.execDir, "sub/c.go", "c-later\n")
	done := make(chan error, 1)
	started := make(chan struct{})
	go func() {
		close(started)
		_, err := turn.integrate(context.Background(), nil)
		done <- err
	}()
	<-started
	time.Sleep(150 * time.Millisecond)
	select {
	case err := <-done:
		t.Fatalf("merge finished while the holder was live: %v", err)
	default:
	}
	if got := readParallelFile(t, repo, "b.go"); got != "b-base\n" {
		t.Fatalf("primary b.go changed before the holder left: %q", got)
	}
	release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("merge did not finish after the holder left")
	}
	if got := readParallelFile(t, repo, "a.go"); got != "a-holder\n" {
		t.Fatalf("a.go = %q", got)
	}
	if got := readParallelFile(t, repo, "b.go"); got != "b-later\n" {
		t.Fatalf("b.go = %q", got)
	}
	if got := readParallelFile(t, repo, "sub/c.go"); got != "c-later\n" {
		t.Fatalf("sub/c.go = %q", got)
	}
}

func TestCodingParallelMergesDifferentRegions(t *testing.T) {
	repo := initParallelRepo(t)
	commitParallelFile(t, repo, "note.txt", "alpha\nmiddle\nomega\n")
	pair := newParallelPair(t, repo)
	release := pair.hold(t)
	writeParallelFile(t, repo, "note.txt", "ALPHA\nmiddle\nomega\n")
	turn := pair.beginLater(t, codingRequestImplementation)
	writeParallelFile(t, turn.execDir, "note.txt", "alpha\nmiddle\nOMEGA\n")
	release()
	result := &CodingSubAgentResult{FilesModified: []string{filepath.Join(turn.execDir, "note.txt")}}
	if _, err := turn.integrate(context.Background(), result); err != nil {
		t.Fatal(err)
	}
	if len(result.FilesModified) != 1 || result.FilesModified[0] != filepath.Join(repo, "note.txt") {
		t.Fatalf("result path = %#v, want %q", result.FilesModified, filepath.Join(repo, "note.txt"))
	}
	got := readParallelFile(t, repo, "note.txt")
	if got != "ALPHA\nmiddle\nOMEGA\n" {
		t.Fatalf("merged note = %q", got)
	}
	if len(pair.handler.listStickyCodingConflicts(pair.laterOwner)) != 0 {
		t.Fatal("a clean region merge recorded a conflict")
	}
}

func TestCodingParallelConflictKeepsPrimaryBytes(t *testing.T) {
	repo := initParallelRepo(t)
	commitParallelFile(t, repo, "same.txt", "alpha\nmiddle\nomega\n")
	commitParallelFile(t, repo, "other.txt", "other-base\n")
	pair := newParallelPair(t, repo)
	release := pair.hold(t)
	writeParallelFile(t, repo, "same.txt", "alpha\nPRIMARY\nomega\n")
	turn := pair.beginLater(t, codingRequestImplementation)
	writeParallelFile(t, turn.execDir, "same.txt", "alpha\nWORKTREE\nomega\n")
	writeParallelFile(t, turn.execDir, "other.txt", "other-later\n")
	var notes []string
	turn.onProgress = func(message string) { notes = append(notes, message) }
	release()
	result := &CodingSubAgentResult{FilesModified: []string{"same.txt", "other.txt", filepath.Join(turn.execDir, "other.txt")}}
	ok, err := turn.integrate(context.Background(), result)
	if ok || err == nil || !strings.Contains(err.Error(), "自动合入 1 个文件，还有 1 个要人看") {
		t.Fatalf("integrate ok=%v err=%v", ok, err)
	}
	got := readParallelFile(t, repo, "same.txt")
	if got != "alpha\nPRIMARY\nomega\n" || strings.Contains(got, "<<<<<<<") || strings.Contains(got, ">>>>>>>") {
		t.Fatalf("primary same.txt = %q", got)
	}
	if readParallelFile(t, repo, "other.txt") != "other-later\n" {
		t.Fatalf("auto-merged other.txt = %q", readParallelFile(t, repo, "other.txt"))
	}
	if _, statErr := os.Stat(turn.wt.Path); statErr != nil {
		t.Fatalf("conflict worktree was removed: %v", statErr)
	}
	conflicts := pair.handler.listStickyCodingConflicts(pair.laterOwner)
	if len(conflicts) != 1 || conflicts[0].Kind != "local_worktree" || len(conflicts[0].Files) != 1 || conflicts[0].Files[0] != "same.txt" {
		t.Fatalf("conflicts = %+v", conflicts)
	}
	if len(result.FilesModified) != 2 {
		t.Fatalf("reported modified = %#v", result.FilesModified)
	}
	sawPrimary := false
	for _, path := range result.FilesModified {
		if !strings.HasSuffix(strings.ToLower(filepath.ToSlash(path)), "other.txt") {
			t.Fatalf("reported modified = %#v", result.FilesModified)
		}
		if path == filepath.Join(turn.execDir, "other.txt") {
			t.Fatalf("reported the checkout path %q", path)
		}
		if path == filepath.Join(repo, "other.txt") {
			sawPrimary = true
		}
	}
	if !sawPrimary {
		t.Fatalf("reported modified = %#v, want %q", result.FilesModified, filepath.Join(repo, "other.txt"))
	}
	joined := strings.Join(notes, "\n")
	if !strings.Contains(joined, "自动合入 1 个文件，还有 1 个要人看") {
		t.Fatalf("progress = %q", joined)
	}
}

func TestCodingParallelPathStaysUntilMerge(t *testing.T) {
	repo := initParallelRepo(t)
	pair := newParallelPair(t, repo)
	release := pair.hold(t)
	turn := pair.beginLater(t, codingRequestImplementation)
	exec := turn.execDir
	if got := pair.app.exclusiveWorkRoot(pair.later.ProjectPath, repo); normalizeProjectSessionPath(got) != normalizeProjectSessionPath(exec) {
		t.Fatalf("path while the holder is live = %q, want worktree %q", got, exec)
	}
	release()
	if got := pair.app.exclusiveWorkRoot(pair.later.ProjectPath, repo); normalizeProjectSessionPath(got) != normalizeProjectSessionPath(exec) {
		t.Fatalf("path after the holder left = %q, want worktree %q", got, exec)
	}
	turn.finish(context.Background())
	if got := normalizeProjectSessionPath(pair.app.exclusiveWorkRoot(pair.later.ProjectPath, repo)); got != normalizeProjectSessionPath(repo) {
		t.Fatalf("path after merge = %q, want primary %q", got, repo)
	}
	pair.turn = nil
}

func TestCodingParallelLaterStartMissesPrimary(t *testing.T) {
	repo := initParallelRepo(t)
	pair := newParallelPair(t, repo)
	beginForegroundAgentOwner(pair.holderOwner)
	t.Cleanup(func() { endForegroundAgentOwner(pair.holderOwner) })
	time.Sleep(30 * time.Millisecond)
	beginForegroundAgentOwner(pair.laterOwner)
	t.Cleanup(func() { endForegroundAgentOwner(pair.laterOwner) })
	if earlier := pair.handler.beginCodingParallelTurn(pair.holderOwner, repo, codingRequestImplementation, nil); earlier != nil {
		t.Fatalf("earlier registrant was moved off the primary: %+v", earlier)
	}
	later := pair.beginLater(t, codingRequestImplementation)
	if !later.deferred || normalizeProjectSessionPath(later.execDir) == normalizeProjectSessionPath(repo) {
		t.Fatalf("later registrant exec = %q deferred=%v", later.execDir, later.deferred)
	}
}

func TestCodingParallelCreateFailureWaitsOnPrimary(t *testing.T) {
	repo := initParallelRepo(t)
	writeParallelFile(t, repo, "kept.go", "holder-bytes\n")
	previous := codingParallelWorktreeCreate
	t.Cleanup(func() { codingParallelWorktreeCreate = previous })
	codingParallelWorktreeCreate = func(string, int, string) (*codingWorkbenchWorktree, error) {
		return nil, os.ErrClosed
	}
	pair := newParallelPair(t, repo)
	release := pair.hold(t)
	var notes []string
	turn := pair.handler.beginCodingParallelTurn(pair.laterOwner, repo, codingRequestImplementation, func(message string) {
		notes = append(notes, message)
	})
	pair.turn = turn
	if turn == nil || turn.deferred || turn.wt != nil || !turn.waitPrimary || !turn.stayPut {
		t.Fatalf("turn = %+v", turn)
	}
	if normalizeProjectSessionPath(turn.execDir) != normalizeProjectSessionPath(repo) {
		t.Fatalf("exec = %q, want primary", turn.execDir)
	}
	if !strings.Contains(strings.Join(notes, "\n"), "独立检出创建失败") {
		t.Fatalf("progress = %q", notes)
	}
	assertWaitBlocksUntilRelease(t, func() error { return turn.waitReady(context.Background()) }, release)
	if got := readParallelFile(t, repo, "kept.go"); got != "holder-bytes\n" {
		t.Fatalf("primary changed while waiting: %q", got)
	}
}

func TestCodingParallelNonGitWaitsWithoutCopy(t *testing.T) {
	skipWithoutGit(t)
	dir := filepath.Join(t.TempDir(), "plain")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Temp lives under the user profile. Stop git from treating a parent
	// checkout as this directory's repository.
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	writeParallelFile(t, dir, "notes.txt", "holder-bytes\n")
	pair := newParallelPair(t, dir)
	release := pair.hold(t)
	var notes []string
	turn := pair.handler.beginCodingParallelTurn(pair.laterOwner, dir, codingRequestImplementation, func(message string) {
		notes = append(notes, message)
	})
	pair.turn = turn
	if turn == nil || turn.wt != nil || turn.deferred || !turn.waitPrimary {
		t.Fatalf("turn = %+v", turn)
	}
	if !strings.Contains(strings.Join(notes, "\n"), "不是 git 仓库") {
		t.Fatalf("progress = %q", notes)
	}
	assertWaitBlocksUntilRelease(t, func() error { return turn.waitReady(context.Background()) }, release)
	if got := readParallelFile(t, dir, "notes.txt"); got != "holder-bytes\n" {
		t.Fatalf("primary changed: %q", got)
	}
}

func TestCodingParallelSeparateReposDoNotWait(t *testing.T) {
	repoA := initParallelRepo(t)
	repoB := initParallelRepo(t)
	app := newProjectSearchTestApp(t)
	holder := app.createTaskRecordWithWorkingDir("holder", "", []string{taskManagementTag, taskUserCreatedTag}, repoA, true)
	later := app.createTaskRecordWithWorkingDir("later", "", []string{taskManagementTag, taskUserCreatedTag}, repoB, true)
	handler := &IMMessageHandler{app: app}
	beginForegroundAgentOwner(projectSessionOwnerID(holder.ProjectPath))
	t.Cleanup(func() { endForegroundAgentOwner(projectSessionOwnerID(holder.ProjectPath)) })
	if turn := handler.beginCodingParallelTurn(projectSessionOwnerID(later.ProjectPath), repoB, codingRequestImplementation, nil); turn != nil {
		t.Cleanup(func() { turn.finish(context.Background()) })
		t.Fatalf("a different repo was bound: exec=%q deferred=%v", turn.execDir, turn.deferred)
	}
}

func TestCodingParallelInquiryStaysOnPrimary(t *testing.T) {
	repo := initParallelRepo(t)
	pair := newParallelPair(t, repo)
	pair.hold(t)
	turn := pair.beginLater(t, codingRequestInquiry)
	if turn.wt != nil || turn.deferred || turn.waitPrimary || !turn.stayPut {
		t.Fatalf("inquiry turn = %+v", turn)
	}
	if normalizeProjectSessionPath(turn.execDir) != normalizeProjectSessionPath(repo) {
		t.Fatalf("inquiry exec = %q", turn.execDir)
	}
	if got := normalizeProjectSessionPath(pair.app.exclusiveWorkRoot(pair.later.ProjectPath, repo)); got != normalizeProjectSessionPath(repo) {
		t.Fatalf("inquiry was detached to %q", got)
	}
}

func TestCodingParallelOperationalWaitsOnPrimary(t *testing.T) {
	repo := initParallelRepo(t)
	pair := newParallelPair(t, repo)
	release := pair.hold(t)
	var notes []string
	turn := pair.handler.beginCodingParallelTurn(pair.laterOwner, repo, codingRequestOperational, func(message string) {
		notes = append(notes, message)
	})
	pair.turn = turn
	if turn == nil || turn.wt != nil || turn.deferred || !turn.waitPrimary {
		t.Fatalf("operational turn = %+v", turn)
	}
	if normalizeProjectSessionPath(turn.execDir) != normalizeProjectSessionPath(repo) {
		t.Fatalf("operational exec = %q", turn.execDir)
	}
	if !strings.Contains(strings.Join(notes, "\n"), "同一目录上另一个任务还在使用") {
		t.Fatalf("progress = %q", notes)
	}
	assertWaitBlocksUntilRelease(t, func() error { return turn.waitReady(context.Background()) }, release)
}

func TestCodingParallelMergesCRLFWorkingTree(t *testing.T) {
	repo := initParallelRepo(t)
	mustGit(t, repo, "config", "core.autocrlf", "true")
	commitParallelFile(t, repo, "note.txt", "alpha\r\nmiddle\r\nomega\r\n")
	commitParallelFile(t, repo, "plain.txt", "one\r\n")
	blob, err := codingParallelGit(repo, "show", "HEAD:note.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(blob) != "alpha\nmiddle\nomega\n" {
		t.Fatalf("committed blob = %q, want LF", blob)
	}
	if got := readParallelFile(t, repo, "note.txt"); got != "alpha\r\nmiddle\r\nomega\r\n" {
		t.Fatalf("working tree = %q, want CRLF", got)
	}
	pair := newParallelPair(t, repo)
	release := pair.hold(t)
	writeParallelFile(t, repo, "note.txt", "ALPHA\r\nmiddle\r\nomega\r\n")
	turn := pair.beginLater(t, codingRequestImplementation)
	writeParallelFile(t, turn.execDir, "note.txt", "alpha\r\nmiddle\r\nOMEGA\r\n")
	writeParallelFile(t, turn.execDir, "plain.txt", "two\r\n")
	release()
	if _, err := turn.integrate(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if got := readParallelFile(t, repo, "note.txt"); got != "ALPHA\r\nmiddle\r\nOMEGA\r\n" {
		t.Fatalf("merged note = %q", got)
	}
	if got := readParallelFile(t, repo, "plain.txt"); got != "two\r\n" {
		t.Fatalf("plain.txt = %q", got)
	}
	if len(pair.handler.listStickyCodingConflicts(pair.laterOwner)) != 0 {
		t.Fatal("CRLF line endings were recorded as a conflict")
	}
}

func TestCodingParallelNameStatusCopyKeepsSource(t *testing.T) {
	raw := []byte("A\x00b.txt\x00R050\x00a.txt\x00c.txt\x00C100\x00keep.txt\x00copy.txt\x00")
	changes := parseCodingParallelNameStatus(raw)
	got := make([]string, 0, len(changes))
	for _, change := range changes {
		mark := "M"
		if change.deleted {
			mark = "D"
		}
		got = append(got, mark+" "+change.path)
	}
	want := []string{"M b.txt", "D a.txt", "M c.txt", "M copy.txt"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("changes = %v", got)
	}
}

func TestCodingParallelLandedPathIsNotBaseName(t *testing.T) {
	root := t.TempDir()
	worktree := t.TempDir()
	summary := codingParallelIntegrateSummary{
		landed:        map[string]bool{"sub/other.txt": true, "pkg/a.go": true},
		roots:         []string{root, worktree},
		projectPrefix: "pkg",
	}
	got := summary.landedModified([]string{
		"sub/other.txt",
		"./sub/other.txt",
		filepath.Join(root, "sub", "other.txt"),
		filepath.Join(worktree, "sub", "other.txt"),
		"other.txt",
		filepath.Join(root, "nested", "other.txt"),
		"a.go",
		filepath.Join(root, "pkg", "a.go"),
		filepath.Join(root, "a.go"),
	})
	want := []string{
		"sub/other.txt",
		"./sub/other.txt",
		filepath.Join(root, "sub", "other.txt"),
		filepath.Join(worktree, "sub", "other.txt"),
		"a.go",
		filepath.Join(root, "pkg", "a.go"),
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("landed = %#v", got)
	}
}

func TestCodingParallelRelPathRejectsEscape(t *testing.T) {
	if _, ok := codingParallelRelPath(`..\..\outside`); ok {
		t.Fatal("parent escape accepted")
	}
	if _, ok := codingParallelRelPath(`foo/../../outside`); ok {
		t.Fatal("cleaned escape accepted")
	}
	if _, ok := codingParallelRelPath(filepath.Join(t.TempDir(), "a.go")); ok {
		t.Fatal("absolute path accepted")
	}
	got, ok := codingParallelRelPath(`sub/a.go`)
	if !ok || got != "sub/a.go" {
		t.Fatalf("rel = %q ok=%v", got, ok)
	}
}

func TestCodingParallelDirectoryConflictKeepsPrimary(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "sub")
	if err := os.Mkdir(primary, 0o755); err != nil {
		t.Fatal(err)
	}
	their := filepath.Join(dir, "their.txt")
	if err := os.WriteFile(their, []byte("later\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	applied, conflict, err := applyCodingParallelFileOnce(primary, their, []byte("base\n"), true, false, false)
	if err != nil || applied || !conflict {
		t.Fatalf("applied=%v conflict=%v err=%v", applied, conflict, err)
	}
	info, err := os.Stat(primary)
	if err != nil || !info.IsDir() {
		t.Fatalf("primary directory changed: %v", err)
	}
}

func TestCodingParallelCancelledTurnDoesNotWritePrimary(t *testing.T) {
	repo := initParallelRepo(t)
	pair := newParallelPair(t, repo)
	release := pair.hold(t)
	turn := pair.beginLater(t, codingRequestImplementation)
	writeParallelFile(t, turn.execDir, "later.go", "from-later\n")
	release()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ok, err := turn.integrate(ctx, nil)
	if ok || err != context.Canceled {
		t.Fatalf("integrate ok=%v err=%v", ok, err)
	}
	if _, statErr := os.Stat(filepath.Join(repo, "later.go")); !os.IsNotExist(statErr) {
		t.Fatalf("cancelled merge wrote the primary, stat err=%v", statErr)
	}
	if turn.hasMerged() {
		t.Fatal("cancelled merge was recorded as applied")
	}
	wtPath := turn.wt.Path
	turn.finish(ctx)
	pair.turn = nil
	if _, statErr := os.Stat(wtPath); !os.IsNotExist(statErr) {
		t.Fatalf("cancelled worktree remains: %v", statErr)
	}
}

func TestCodingParallelCancelledWaitDoesNotStart(t *testing.T) {
	repo := initParallelRepo(t)
	pair := newParallelPair(t, repo)
	release := pair.hold(t)
	turn := pair.beginLater(t, codingRequestOperational)
	if !turn.waitPrimary || turn.deferred {
		t.Fatalf("operational turn wait=%v deferred=%v", turn.waitPrimary, turn.deferred)
	}
	release()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := turn.waitReady(ctx); err != context.Canceled {
		t.Fatalf("waitReady err=%v", err)
	}
}

func TestCodingParallelFileErrorKeepsOtherFiles(t *testing.T) {
	repo := initParallelRepo(t)
	commitParallelFile(t, repo, "a.go", "a-base\n")
	commitParallelFile(t, repo, "z.go", "z-base\n")
	writeParallelFile(t, repo, "nested", "not-a-directory\n")
	pair := newParallelPair(t, repo)
	release := pair.hold(t)
	turn := pair.beginLater(t, codingRequestImplementation)
	writeParallelFile(t, turn.execDir, "a.go", "a-later\n")
	writeParallelFile(t, turn.execDir, "z.go", "z-later\n")
	writeParallelFile(t, turn.execDir, "nested/b.go", "b-later\n")
	release()
	ok, err := turn.integrate(context.Background(), nil)
	if ok || err == nil || !strings.Contains(err.Error(), "自动合入 2 个文件，还有 1 个要人看") {
		t.Fatalf("integrate ok=%v err=%v", ok, err)
	}
	if readParallelFile(t, repo, "a.go") != "a-later\n" || readParallelFile(t, repo, "z.go") != "z-later\n" {
		t.Fatalf("a.go=%q z.go=%q", readParallelFile(t, repo, "a.go"), readParallelFile(t, repo, "z.go"))
	}
	info, statErr := os.Stat(filepath.Join(repo, "nested"))
	if statErr != nil || info.IsDir() || readParallelFile(t, repo, "nested") != "not-a-directory\n" {
		t.Fatalf("nested file changed: %v dir=%v", statErr, info != nil && info.IsDir())
	}
	conflicts := pair.handler.listStickyCodingConflicts(pair.laterOwner)
	if len(conflicts) != 1 || conflicts[0].Kind != "local_worktree" || len(conflicts[0].Files) != 1 || conflicts[0].Files[0] != "nested/b.go" {
		t.Fatalf("conflicts = %+v", conflicts)
	}
	if _, statErr := os.Stat(turn.wt.Path); statErr != nil {
		t.Fatalf("worktree removed: %v", statErr)
	}
}

func TestCodingParallelMergeErrorBeforeWriteCanRetry(t *testing.T) {
	repo := initParallelRepo(t)
	commitParallelFile(t, repo, "a.go", "a-base\n")
	pair := newParallelPair(t, repo)
	release := pair.hold(t)
	turn := pair.beginLater(t, codingRequestImplementation)
	writeParallelFile(t, turn.execDir, "a.go", "a-later\n")
	release()
	real := turn.baseSHA
	turn.baseSHA = "not-a-commit"
	ok, err := turn.integrate(context.Background(), nil)
	if ok || err == nil {
		t.Fatalf("bad base integrated ok=%v err=%v", ok, err)
	}
	if readParallelFile(t, repo, "a.go") != "a-base\n" {
		t.Fatalf("primary changed on a failed diff: %q", readParallelFile(t, repo, "a.go"))
	}
	if turn.hasMerged() {
		t.Fatal("failed merge before any write was marked done")
	}
	turn.baseSHA = real
	if _, err := turn.integrate(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if readParallelFile(t, repo, "a.go") != "a-later\n" {
		t.Fatalf("retry left a.go = %q", readParallelFile(t, repo, "a.go"))
	}
}

func TestCodingParallelShellDirStaysOnCheckoutUntilMerge(t *testing.T) {
	turn := &codingParallelTurn{deferred: true, primary: `D:\repo`, execDir: `D:\wt`}
	if got := turn.shellDir(`D:\fallback`); got != `D:\wt` {
		t.Fatalf("before merge = %q", got)
	}
	turn.merged = true
	if got := turn.shellDir(`D:\fallback`); got != `D:\repo` {
		t.Fatalf("after merge = %q", got)
	}
	if got := (&codingParallelTurn{primary: `D:\repo`, execDir: `D:\wt`}).shellDir(`D:\fallback`); got != `D:\fallback` {
		t.Fatalf("shared turn = %q", got)
	}
	var none *codingParallelTurn
	if got := none.shellDir(`D:\fallback`); got != `D:\fallback` {
		t.Fatalf("nil turn = %q", got)
	}
}

func TestCodingParallelNewFileKeepsExecutableMode(t *testing.T) {
	dir := t.TempDir()
	their := filepath.Join(dir, "their.sh")
	if err := os.WriteFile(their, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(their, 0o755); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(their)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Skip("platform does not persist executable permission")
	}
	dest := filepath.Join(dir, "sub", "run.sh")
	applied, conflict, applyErr := applyCodingParallelFileOnce(dest, their, nil, false, false, false)
	if applyErr != nil || !applied || conflict {
		t.Fatalf("applied=%v conflict=%v err=%v", applied, conflict, applyErr)
	}
	got, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode().Perm()&0o111 == 0 {
		t.Fatalf("new file perm = %o, source = %o", got.Mode().Perm(), info.Mode().Perm())
	}
}

func TestCodingParallelWithinRootRejectsEscape(t *testing.T) {
	root := t.TempDir()
	sibling := filepath.Join(filepath.Dir(root), "outside.txt")
	if codingParallelWithinRoot(root, sibling) {
		t.Fatal("sibling path was accepted")
	}
	if !codingParallelWithinRoot(root, filepath.Join(root, "a.go")) {
		t.Fatal("missing child was rejected")
	}
	outside := t.TempDir()
	link := filepath.Join(root, "link")
	if !linkParallelOutside(t, link, outside) {
		return
	}
	defer os.Remove(link)
	if codingParallelWithinRoot(root, filepath.Join(link, "a.go")) {
		t.Fatal("symlink parent was accepted")
	}
}

func TestCodingParallelSymlinkParentDoesNotEscape(t *testing.T) {
	repo := initParallelRepo(t)
	outside := t.TempDir()
	link := filepath.Join(repo, "link")
	if !linkParallelOutside(t, link, outside) {
		t.Skip("symlink unavailable")
	}
	defer os.Remove(link)
	theirDir := t.TempDir()
	writeParallelFile(t, theirDir, "link/new.txt", "secret\n")
	applied, conflict, err := applyCodingParallelFile(repo, theirDir, parallelHead(t, repo), codingParallelChange{path: "link/new.txt"})
	if err != nil || applied || !conflict {
		t.Fatalf("applied=%v conflict=%v err=%v", applied, conflict, err)
	}
	if _, statErr := os.Stat(filepath.Join(outside, "new.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("write escaped the repository: %v", statErr)
	}
}

func TestCodingParallelBlobMissingPath(t *testing.T) {
	repo := initParallelRepo(t)
	body, ok, err := codingParallelBlob(repo, parallelHead(t, repo), "no-such.txt")
	if err != nil || ok || len(body) != 0 {
		t.Fatalf("missing path body=%q ok=%v err=%v", body, ok, err)
	}
	body, ok, err = codingParallelBlob(repo, parallelHead(t, repo), "README")
	if err != nil || !ok || string(body) != "base\n" {
		t.Fatalf("README body=%q ok=%v err=%v", body, ok, err)
	}
	_, ok, err = codingParallelBlob(repo, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", "README")
	if err == nil || ok {
		t.Fatalf("bad commit ok=%v err=%v", ok, err)
	}
}

func TestCodingParallelModeOnlyChangeLands(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "run.sh")
	their := filepath.Join(dir, "their.sh")
	body := []byte("#!/bin/sh\n")
	if err := os.WriteFile(primary, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(their, body, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(their, 0o755); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(their)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Skip("platform does not persist executable permission")
	}
	applied, conflict, applyErr := applyCodingParallelFileOnce(primary, their, body, true, false, false)
	if applyErr != nil || !applied || conflict {
		t.Fatalf("applied=%v conflict=%v err=%v", applied, conflict, applyErr)
	}
	got, err := os.Stat(primary)
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode().Perm()&0o111 == 0 {
		t.Fatalf("primary perm = %o", got.Mode().Perm())
	}
	edited := filepath.Join(dir, "edited.sh")
	editedTheir := filepath.Join(dir, "edited-their.sh")
	if err := os.WriteFile(edited, []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(editedTheir, []byte("edited\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(editedTheir, 0o755); err != nil {
		t.Fatal(err)
	}
	applied, conflict, applyErr = applyCodingParallelFileOnce(edited, editedTheir, []byte("base\n"), true, false, false)
	if applyErr != nil || applied || conflict {
		t.Fatalf("holder edit applied=%v conflict=%v err=%v", applied, conflict, applyErr)
	}
	got, err = os.Stat(edited)
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode().Perm()&0o111 != 0 {
		t.Fatalf("holder mode changed to %o", got.Mode().Perm())
	}
}

// linkParallelOutside points link at target. A directory junction is the
// fallback when creating a symlink needs a privilege this process does not
// have. Both report ModeSymlink, so a write through either leaves the tree.
func linkParallelOutside(t *testing.T, link, target string) bool {
	t.Helper()
	if err := os.Symlink(target, link); err == nil {
		return true
	}
	cmd := exec.Command("cmd", "/c", "mklink", "/J", link, target)
	if err := cmd.Run(); err != nil {
		t.Logf("symlink unavailable: %v", err)
		return false
	}
	return true
}

func TestCodingParallelCheckoutStaysWhileReadOnlyChildRuns(t *testing.T) {
	dir := t.TempDir()
	exec := filepath.Join(dir, "wt")
	if err := os.MkdirAll(exec, 0o755); err != nil {
		t.Fatal(err)
	}
	turn := &codingParallelTurn{
		taskPath:        dir,
		execDir:         exec,
		deferred:        true,
		merged:          true,
		discardWorktree: true,
		wt:              &codingWorkbenchWorktree{Path: exec, GitRoot: exec, created: true},
	}
	codingParallelRememberExec(turn)
	t.Cleanup(func() { codingParallelForgetExec(turn) })
	done := codingParallelTrackExec(exec)
	turn.finish(context.Background())
	if !turn.wt.created {
		t.Fatal("checkout was removed while a read-only child was still running")
	}
	marker := filepath.Join(exec, "still.txt")
	if err := os.WriteFile(marker, []byte("child\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	done()
	if turn.wt.created {
		t.Fatal("checkout stayed after the read-only child finished")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("checkout was not removed after the child finished: %v", err)
	}
}

func TestCodingParallelCheckoutRemovesWhenChildAlreadyFinished(t *testing.T) {
	dir := t.TempDir()
	exec := filepath.Join(dir, "wt")
	turn := &codingParallelTurn{
		execDir:         exec,
		deferred:        true,
		merged:          true,
		discardWorktree: true,
		wt:              &codingWorkbenchWorktree{Path: exec, created: true},
	}
	codingParallelRememberExec(turn)
	t.Cleanup(func() { codingParallelForgetExec(turn) })
	codingParallelTrackExec(exec)()
	turn.finish(context.Background())
	if turn.wt.created {
		t.Fatal("checkout stayed after the child had already finished")
	}
}

func TestCodingParallelMappedDirKeepsPathCase(t *testing.T) {
	task := filepath.Join(`D:\`, "tasks", "later")
	root := filepath.Join(`D:\`, "Repo")
	wt := filepath.Join(`D:\`, "wt")
	turn := &codingParallelTurn{
		primary:  root,
		execDir:  wt,
		deferred: true,
		wt:       &codingWorkbenchWorktree{Path: wt, GitRoot: root, ProjectPath: wt},
	}
	codingParallelTurns.Store(codingParallelTurnKey(task), turn)
	t.Cleanup(func() { codingParallelTurns.Delete(codingParallelTurnKey(task)) })
	got, ok := codingParallelMappedDir(task, filepath.Join(root, "Foo", "A.go"))
	if !ok {
		t.Fatal("path was not mapped")
	}
	want := filepath.Join(wt, "Foo", "A.go")
	if got != want {
		t.Fatalf("mapped = %q, want %q", got, want)
	}
}

func TestCodingParallelReadOnlyPrimaryStillLands(t *testing.T) {
	repo := initParallelRepo(t)
	commitParallelFile(t, repo, "a.go", "base\n")
	commitParallelFile(t, repo, "gone.go", "base\n")
	aPath := filepath.Join(repo, "a.go")
	gonePath := filepath.Join(repo, "gone.go")
	if err := os.Chmod(aPath, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(gonePath, 0o444); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(aPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o200 != 0 {
		t.Skip("platform does not persist a read-only file")
	}
	their := t.TempDir()
	writeParallelFile(t, their, "a.go", "next\n")
	head := parallelHead(t, repo)
	applied, conflict, applyErr := applyCodingParallelFile(repo, their, head, codingParallelChange{path: "a.go"})
	if applyErr != nil || !applied || conflict {
		t.Fatalf("write applied=%v conflict=%v err=%v", applied, conflict, applyErr)
	}
	if got := readParallelFile(t, repo, "a.go"); got != "next\n" {
		t.Fatalf("a.go = %q", got)
	}
	applied, conflict, applyErr = applyCodingParallelFile(repo, their, head, codingParallelChange{path: "gone.go", deleted: true})
	if applyErr != nil || !applied || conflict {
		t.Fatalf("delete applied=%v conflict=%v err=%v", applied, conflict, applyErr)
	}
	if _, statErr := os.Stat(gonePath); !os.IsNotExist(statErr) {
		t.Fatalf("read-only file still present: %v", statErr)
	}
}

func TestCodingParallelMappedDirMapsGitRootToCheckout(t *testing.T) {
	task := filepath.Join(`D:\`, "tasks", "subdir")
	root := filepath.Join(`D:\`, "Repo")
	wt := filepath.Join(`D:\`, "wt")
	pkg := filepath.Join(wt, "pkg")
	turn := &codingParallelTurn{
		primary:  filepath.Join(root, "pkg"),
		execDir:  pkg,
		deferred: true,
		wt: &codingWorkbenchWorktree{
			Path:        wt,
			GitRoot:     root,
			ProjectPath: pkg,
		},
	}
	codingParallelTurns.Store(codingParallelTurnKey(task), turn)
	t.Cleanup(func() { codingParallelTurns.Delete(codingParallelTurnKey(task)) })

	got, ok := codingParallelMappedDir(task, root)
	if !ok || got != wt {
		t.Fatalf("git root mapped to %q ok=%v, want %q", got, ok, wt)
	}
	got, ok = codingParallelMappedDir(task, filepath.Join(root, "pkg", "A.go"))
	want := filepath.Join(wt, "pkg", "A.go")
	if !ok || got != want {
		t.Fatalf("subdir file mapped to %q ok=%v, want %q", got, ok, want)
	}
}

func TestCodingParallelPreviewRemapsSiblingBesideSubdir(t *testing.T) {
	repo := filepath.Join(`D:\`, "Repo")
	wt := filepath.Join(`D:\`, "wt")
	primary := filepath.Join(repo, "pkg")
	exec := filepath.Join(wt, "pkg")
	turn := &codingParallelTurn{
		deferred: true,
		merged:   true,
		primary:  primary,
		execDir:  exec,
		wt: &codingWorkbenchWorktree{
			Path:        wt,
			GitRoot:     repo,
			ProjectPath: exec,
		},
	}
	root, modified, created := turn.previewPaths(exec, []string{
		filepath.Join(exec, "a.go"),
		"note.go",
		filepath.Join(wt, "other", "b.go"),
	}, []string{
		filepath.Join(wt, "other", "c.go"),
	})
	if root != repo {
		t.Fatalf("preview root = %q, want git root %q", root, repo)
	}
	wantModified := map[string]bool{
		filepath.Join(primary, "a.go"):       true,
		filepath.Join(primary, "note.go"):    true,
		filepath.Join(repo, "other", "b.go"): true,
	}
	if len(modified) != len(wantModified) {
		t.Fatalf("modified = %v", modified)
	}
	for _, path := range modified {
		if !wantModified[path] {
			t.Fatalf("modified path %q", path)
		}
	}
	if len(created) != 1 || created[0] != filepath.Join(repo, "other", "c.go") {
		t.Fatalf("created = %v", created)
	}

	turn.merged = false
	root, modified, created = turn.previewPaths(exec, []string{filepath.Join(wt, "other", "b.go")}, nil)
	if root != exec || len(modified) != 1 || modified[0] != filepath.Join(wt, "other", "b.go") || created != nil {
		t.Fatalf("before merge root=%q modified=%v created=%v", root, modified, created)
	}

	turn.merged = true
	root, modified, created = turn.previewPaths(exec, []string{"note.go", filepath.Join(exec, "a.go")}, nil)
	if root != primary || len(modified) != 2 || created != nil {
		t.Fatalf("subdir-only root=%q modified=%v created=%v", root, modified, created)
	}
	for _, path := range modified {
		if path != "note.go" && path != filepath.Join(primary, "a.go") {
			t.Fatalf("subdir-only path %q", path)
		}
	}
}

func TestCodingParallelPreviewSiblingEventIncludesFile(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(repo))
	primary := filepath.Join(repo, "pkg")
	if err := os.MkdirAll(filepath.Join(repo, "other"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(primary, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(primary, "a.go"), []byte("package pkg\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "other", "b.go"), []byte("package other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(repo, "wt")
	exec := filepath.Join(wt, "pkg")
	turn := &codingParallelTurn{
		deferred: true,
		merged:   true,
		primary:  primary,
		execDir:  exec,
		wt:       &codingWorkbenchWorktree{Path: wt, GitRoot: repo, ProjectPath: exec},
	}
	root, modified, _ := turn.previewPaths(exec, []string{
		"a.go",
		filepath.Join(wt, "other", "b.go"),
	}, nil)
	events := buildCodingSubAgentCodeFileEvents("sess", root, modified, nil)
	got := map[string]bool{}
	for _, evt := range events {
		got[evt.AbsPath] = true
	}
	if !got[filepath.Join(primary, "a.go")] || !got[filepath.Join(repo, "other", "b.go")] {
		t.Fatalf("events = %#v", events)
	}
}

func TestCodingParallelCheckoutStaysForChildInsideWorktree(t *testing.T) {
	dir := t.TempDir()
	exec := filepath.Join(dir, "wt")
	if err := os.MkdirAll(filepath.Join(exec, "pkg", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	turn := &codingParallelTurn{
		execDir:         filepath.Join(exec, "pkg"),
		deferred:        true,
		merged:          true,
		discardWorktree: true,
		wt:              &codingWorkbenchWorktree{Path: exec, GitRoot: exec, ProjectPath: filepath.Join(exec, "pkg"), created: true},
	}
	codingParallelRememberExec(turn)
	t.Cleanup(func() { codingParallelForgetExec(turn) })

	codingParallelTrackExec(t.TempDir())
	if turn.children != 0 {
		t.Fatalf("outside path counted as a checkout reader: %d", turn.children)
	}
	doneNested := codingParallelTrackExec(filepath.Join(exec, "pkg", "nested"))
	doneRoot := codingParallelTrackExec(exec)
	if turn.children != 2 {
		t.Fatalf("children = %d", turn.children)
	}
	turn.finish(context.Background())
	if !turn.wt.created {
		t.Fatal("checkout was removed while a reader inside the worktree was still running")
	}
	marker := filepath.Join(exec, "still.txt")
	if err := os.WriteFile(marker, []byte("child\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	doneNested()
	if !turn.wt.created {
		t.Fatal("checkout was removed while the worktree-root reader was still running")
	}
	doneRoot()
	if turn.wt.created {
		t.Fatal("checkout stayed after the readers finished")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("checkout was not removed after the readers finished: %v", err)
	}
}

func TestCodingParallelToolPathsStayOnCheckout(t *testing.T) {
	task := filepath.Join(`D:\`, "tasks", "tool-paths")
	root := filepath.Join(`D:\`, "Repo")
	wt := filepath.Join(`D:\`, "wt")
	primary := filepath.Join(root, "pkg")
	exec := filepath.Join(wt, "pkg")
	turn := &codingParallelTurn{
		primary:  primary,
		execDir:  exec,
		deferred: true,
		wt: &codingWorkbenchWorktree{
			Path:        wt,
			GitRoot:     root,
			ProjectPath: exec,
		},
	}
	codingParallelTurns.Store(codingParallelTurnKey(task), turn)
	t.Cleanup(func() { codingParallelTurns.Delete(codingParallelTurnKey(task)) })
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{
		projectPath:       exec,
		permissionOwnerID: projectSessionOwnerID(task),
	}}

	fileArgs := cb.withProjectRelativePath(map[string]interface{}{"path": filepath.Join(primary, "A.go")}, false)
	if got, _ := fileArgs["path"].(string); got != filepath.Join(exec, "A.go") {
		t.Fatalf("subdir file = %q", got)
	}
	rootArgs := cb.withProjectRelativePath(map[string]interface{}{"path": filepath.Join(root, "README.md")}, false)
	if got, _ := rootArgs["path"].(string); got != filepath.Join(wt, "README.md") {
		t.Fatalf("git root file = %q", got)
	}
	relArgs := cb.withProjectRelativePath(map[string]interface{}{"path": "A.go"}, false)
	if got, _ := relArgs["path"].(string); got != filepath.Join(exec, "A.go") {
		t.Fatalf("relative file = %q", got)
	}
	inside := filepath.Join(exec, "A.go")
	insideArgs := cb.withProjectRelativePath(map[string]interface{}{"path": inside}, false)
	if got, _ := insideArgs["path"].(string); got != inside {
		t.Fatalf("path inside checkout = %q", got)
	}
	outside := filepath.Join(`C:\`, "Windows", "notepad.exe")
	outArgs := cb.withProjectRelativePath(map[string]interface{}{"path": outside}, false)
	if got, _ := outArgs["path"].(string); got != outside {
		t.Fatalf("path outside the repo = %q", got)
	}

	bashArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": "go test", "working_dir": primary})
	if got, _ := bashArgs["working_dir"].(string); got != exec {
		t.Fatalf("working_dir = %q", got)
	}
	rootBash := cb.withDefaultWorkingDir(map[string]interface{}{"command": "go test", "working_dir": root})
	if got, _ := rootBash["working_dir"].(string); got != wt {
		t.Fatalf("git root working_dir = %q", got)
	}
	relBash := cb.withDefaultWorkingDir(map[string]interface{}{"command": "go test", "working_dir": "sub"})
	if got, _ := relBash["working_dir"].(string); got != filepath.Join(exec, "sub") {
		t.Fatalf("relative working_dir = %q", got)
	}

	raw := "cd '" + primary + "'; Copy-Item '" + filepath.Join(primary, "docs", "a.md") + "' '" + filepath.Join(root, "README.md") + "'"
	if msg := cb.parallelCheckoutCommandRejection(raw, primary); msg == "" {
		t.Fatal("raw command was allowed to name the shared tree")
	}
	rewritten := cb.withDefaultWorkingDir(map[string]interface{}{"command": raw, "working_dir": primary})
	gotCmd, _ := rewritten["command"].(string)
	if commandNamesProtectedRoot(gotCmd, root) || commandNamesProtectedRoot(stringVal(rewritten, "working_dir"), root) {
		t.Fatalf("rewritten command still names %s: %q wd=%q", root, gotCmd, rewritten["working_dir"])
	}
	if !strings.Contains(gotCmd, filepath.Join(exec, "docs", "a.md")) || !strings.Contains(gotCmd, filepath.Join(wt, "README.md")) {
		t.Fatalf("command = %q", gotCmd)
	}
	if msg := cb.parallelCheckoutCommandRejection(gotCmd, stringVal(rewritten, "working_dir")); msg != "" {
		t.Fatalf("rewritten command rejected: %s", msg)
	}
	doubled := "cd \"" + strings.ReplaceAll(primary, `\`, `\\`) + "\""
	doubledArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": doubled})
	doubledCmd, _ := doubledArgs["command"].(string)
	if doubledCmd != "cd \""+strings.ReplaceAll(exec, `\`, `\\`)+"\"" {
		t.Fatalf("doubled command = %q", doubledCmd)
	}
	forward := "cd " + filepath.ToSlash(filepath.Join(root, "README.md"))
	forwardArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": forward})
	forwardCmd, _ := forwardArgs["command"].(string)
	if forwardCmd != "cd "+filepath.ToSlash(filepath.Join(wt, "README.md")) {
		t.Fatalf("forward command = %q", forwardCmd)
	}
	spaced := "cd '" + filepath.Join(root, "my project", "file.go") + "'"
	spacedArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": spaced})
	spacedCmd, _ := spacedArgs["command"].(string)
	if spacedCmd != "cd '"+filepath.Join(wt, "my project", "file.go")+"'" {
		t.Fatalf("spaced command = %q", spacedCmd)
	}
	sibling := "cd " + root + "-extra"
	siblingArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": sibling, "working_dir": exec})
	siblingCmd, _ := siblingArgs["command"].(string)
	if siblingCmd != sibling {
		t.Fatalf("sibling command = %q", siblingCmd)
	}
	if msg := cb.parallelCheckoutCommandRejection(siblingCmd, stringVal(siblingArgs, "working_dir")); msg != "" {
		t.Fatalf("sibling rejected: %s", msg)
	}
	folded := "cd " + strings.ToLower(primary)
	foldedArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": folded, "working_dir": exec})
	foldedCmd, _ := foldedArgs["command"].(string)
	if commandNamesProtectedRoot(foldedCmd, root) || !strings.Contains(strings.ToLower(foldedCmd), strings.ToLower(exec)) {
		t.Fatalf("folded command = %q", foldedCmd)
	}

	// Relative, drive-relative, and cleaned .. paths never contain the root
	// text. They still have to land on the checkout.
	climb := `cd ..\..\Repo`
	if msg := cb.parallelCheckoutCommandRejection(climb, exec); msg == "" {
		t.Fatal("relative climb was allowed to enter the shared tree")
	}
	climbArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": climb})
	climbCmd, _ := climbArgs["command"].(string)
	if climbCmd != `cd `+wt {
		t.Fatalf("relative climb = %q", climbCmd)
	}
	if msg := cb.parallelCheckoutCommandRejection(climbCmd, stringVal(climbArgs, "working_dir")); msg != "" {
		t.Fatalf("rewritten climb rejected: %s", msg)
	}
	dotdotAbs := `cd ` + filepath.Join(`D:\`, "other") + `\..\Repo`
	dotdotArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": dotdotAbs})
	dotdotCmd, _ := dotdotArgs["command"].(string)
	if dotdotCmd != `cd `+wt {
		t.Fatalf("cleaned absolute = %q", dotdotCmd)
	}
	chainArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": `cd ..\..\..; cd Repo`})
	chainCmd, _ := chainArgs["command"].(string)
	if chainCmd != `cd ..\..\..; cd `+wt {
		t.Fatalf("chdir chain = %q", chainCmd)
	}
	wildArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": `Copy-Item ..\..\Repo\*.md .`})
	wildCmd, _ := wildArgs["command"].(string)
	if wildCmd != `Copy-Item `+wt+`\*.md .` {
		t.Fatalf("wildcard climb = %q", wildCmd)
	}
	rootRelArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": `cd \Repo\pkg`})
	rootRelCmd, _ := rootRelArgs["command"].(string)
	if rootRelCmd != `cd `+exec {
		t.Fatalf("root-relative = %q", rootRelCmd)
	}
	driveRel := `cd d:Repo\pkg`
	if msg := cb.parallelCheckoutCommandRejection(driveRel, `C:\Windows`); msg == "" {
		t.Fatal("drive-relative path was allowed to enter the shared tree")
	}
	driveArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": driveRel, "working_dir": `C:\Windows`})
	driveCmd, _ := driveArgs["command"].(string)
	if driveCmd != `cd `+exec {
		t.Fatalf("drive-relative = %q", driveCmd)
	}

	// Push-Location is a directory change with a stack. Pop-Location
	// returns to the directory before the push, not to the process start.
	pushRaw := `cd ..\..\..; Push-Location Repo; Pop-Location; cd Repo`
	if msg := cb.parallelCheckoutCommandRejection(pushRaw, exec); msg == "" {
		t.Fatal("Push-Location climb was allowed to enter the shared tree")
	}
	pushArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": pushRaw})
	pushCmd, _ := pushArgs["command"].(string)
	if pushCmd != `cd ..\..\..; Push-Location `+wt+`; Pop-Location; cd `+wt {
		t.Fatalf("pushd stack = %q", pushCmd)
	}
	namedPush := `Push-Location -StackName s ..\..\..; Pop-Location; cd Repo`
	namedArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": namedPush})
	namedCmd, _ := namedArgs["command"].(string)
	if namedCmd != `Push-Location -StackName s ..\..\..; Pop-Location; cd `+wt {
		t.Fatalf("named stack = %q", namedCmd)
	}
	stopArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": `Set-Location -ErrorAction Stop ..\..\Repo`})
	stopCmd, _ := stopArgs["command"].(string)
	if stopCmd != `Set-Location -ErrorAction Stop `+wt {
		t.Fatalf("error-action path = %q", stopCmd)
	}

	// A bare drive letter selects that drive's root when the process has
	// not been there yet. The following relative name is the shared tree.
	driveSwitch := `D:; cd Repo`
	if msg := cb.parallelCheckoutCommandRejection(driveSwitch, `C:\Windows`); msg == "" {
		t.Fatal("drive switch was allowed to enter the shared tree")
	}
	switchArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": driveSwitch, "working_dir": `C:\Windows`})
	switchCmd, _ := switchArgs["command"].(string)
	if switchCmd != `D:; cd `+wt {
		t.Fatalf("drive switch = %q", switchCmd)
	}

	// Quoted cmd/powershell/bash text is a child script. echo is not.
	group := `(cd ..\..\..); (cd Repo)`
	if msg := cb.parallelCheckoutCommandRejection(group, exec); msg == "" {
		t.Fatal("grouped cd was allowed to enter the shared tree")
	}
	groupArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": group})
	groupCmd, _ := groupArgs["command"].(string)
	if groupCmd != `(cd ..\..\..); (cd `+wt+`)` {
		t.Fatalf("grouped cd = %q", groupCmd)
	}
	subexpr := `cd (..\..\..); (cd Repo)`
	subArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": subexpr})
	subCmd, _ := subArgs["command"].(string)
	if subCmd != subexpr {
		t.Fatalf("subexpression cd = %q", subCmd)
	}
	if msg := cb.parallelCheckoutCommandRejection(subCmd, stringVal(subArgs, "working_dir")); msg != "" {
		t.Fatalf("subexpression rejected: %s", msg)
	}
	wrapped := `cmd /c "cd ..\..\..; cd Repo"; cd Repo`
	wrapArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": wrapped})
	wrapCmd, _ := wrapArgs["command"].(string)
	if wrapCmd != `cmd /c "cd ..\..\..; cd `+wt+`"; cd Repo` {
		t.Fatalf("cmd payload = %q", wrapCmd)
	}
	psWrapped := `powershell -NoProfile -ExecutionPolicy Bypass -Command "cd ..\..\..; cd Repo"`
	psArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": psWrapped})
	psCmd, _ := psArgs["command"].(string)
	if psCmd != `powershell -NoProfile -ExecutionPolicy Bypass -Command "cd ..\..\..; cd `+wt+`"` {
		t.Fatalf("powershell payload = %q", psCmd)
	}
	echoCmd := `echo "cd ..\..\..; cd Repo"`
	echoArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": echoCmd})
	if got, _ := echoArgs["command"].(string); got != echoCmd {
		t.Fatalf("echo payload = %q", got)
	}
	// A quoted path is still one span. A quoted sentence is not a shell:
	// the space keeps "cd ..\..\Repo" from being read as a cd plus a path.
	quotedClimb := `cd '..\..\Repo'`
	quotedClimbArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": quotedClimb})
	if got, _ := quotedClimbArgs["command"].(string); got != `cd '`+wt+`'` {
		t.Fatalf("quoted climb = %q", got)
	}

	// A bare name is a path once the process is on the drive that holds
	// the shared tree. From the checkout the same name is a subdirectory.
	bareHere := `Copy-Item Repo .`
	bareHereArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": bareHere})
	if got, _ := bareHereArgs["command"].(string); got != bareHere {
		t.Fatalf("bare name in checkout = %q", got)
	}
	if msg := cb.parallelCheckoutCommandRejection(bareHere, exec); msg != "" {
		t.Fatalf("bare name in checkout rejected: %s", msg)
	}
	bareClimb := `cd ..\..\..; Copy-Item Repo .`
	if msg := cb.parallelCheckoutCommandRejection(bareClimb, exec); msg == "" {
		t.Fatal("bare name after a climb was allowed to enter the shared tree")
	}
	bareArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": bareClimb})
	bareCmd, _ := bareArgs["command"].(string)
	if bareCmd != `cd ..\..\..; Copy-Item `+wt+` .` {
		t.Fatalf("bare name after climb = %q", bareCmd)
	}
	globClimb := `cd ..\..\..; Copy-Item Repo* .`
	globArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": globClimb})
	globCmd, _ := globArgs["command"].(string)
	if globCmd != `cd ..\..\..; Copy-Item `+wt+`* .` {
		t.Fatalf("glob after climb = %q", globCmd)
	}
	gitC := `cd ..\..\..; git -C Repo status`
	gitArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": gitC})
	gitCmd, _ := gitArgs["command"].(string)
	if gitCmd != `cd ..\..\..; git -C `+wt+` status` {
		t.Fatalf("git -C = %q", gitCmd)
	}
	attached := `Set-Location -Path:..\..\Repo`
	if msg := cb.parallelCheckoutCommandRejection(attached, exec); msg == "" {
		t.Fatal("attached -Path was allowed to enter the shared tree")
	}
	attachedArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": attached})
	attachedCmd, _ := attachedArgs["command"].(string)
	if attachedCmd != `Set-Location -Path:`+wt {
		t.Fatalf("attached -Path = %q", attachedCmd)
	}
	attachedBare := `Set-Location -Path:Repo`
	attachedBareArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": attachedBare})
	if got, _ := attachedBareArgs["command"].(string); got != attachedBare {
		t.Fatalf("attached -Path inside checkout = %q", got)
	}
	attachedStop := `Set-Location -ErrorAction:Stop ..\..\Repo`
	attachedStopArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": attachedStop})
	attachedStopCmd, _ := attachedStopArgs["command"].(string)
	if attachedStopCmd != `Set-Location -ErrorAction:Stop `+wt {
		t.Fatalf("attached -ErrorAction = %q", attachedStopCmd)
	}
	block := `if (Test-Path x) { cd ..\..\..; cd Repo }`
	if msg := cb.parallelCheckoutCommandRejection(block, exec); msg == "" {
		t.Fatal("script block was allowed to enter the shared tree")
	}
	blockArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": block})
	blockCmd, _ := blockArgs["command"].(string)
	if blockCmd != `if (Test-Path x) { cd ..\..\..; cd `+wt+` }` {
		t.Fatalf("script block = %q", blockCmd)
	}

	// cmd treats ^ as an escape, so ^\ is a separator the raw span hides.
	// The whole span is the shared tree. A caret that does not unescape
	// into that tree stays, and so does a quoted caret.
	caretClimb := "cd ..\\..\\..^\\Repo"
	if msg := cb.parallelCheckoutCommandRejection(caretClimb, exec); msg == "" {
		t.Fatal("caret climb was allowed to enter the shared tree")
	}
	caretArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": caretClimb})
	caretCmd, _ := caretArgs["command"].(string)
	if caretCmd != `cd `+wt {
		t.Fatalf("caret climb = %q", caretCmd)
	}
	// The caret is a line continuation, so the next line is the next path byte.
	// ..\^\nRepo is ..\Repo, not ..Repo.
	caretLine := "cd ..\\..\\..\\^\r\nRepo"
	caretLineArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": caretLine})
	if got, _ := caretLineArgs["command"].(string); got != `cd `+wt {
		t.Fatalf("caret continuation = %q", got)
	}
	backtickClimb := "cd ..\\..\\..`\\Repo"
	backtickArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": backtickClimb})
	if got, _ := backtickArgs["command"].(string); got != `cd `+wt {
		t.Fatalf("backtick climb = %q", got)
	}
	caretKeep := "cd C:\\Windows^\\System32"
	caretKeepArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": caretKeep})
	if got, _ := caretKeepArgs["command"].(string); got != caretKeep {
		t.Fatalf("caret outside = %q", got)
	}
	if msg := cb.parallelCheckoutCommandRejection(caretKeep, exec); msg != "" {
		t.Fatalf("caret outside rejected: %s", msg)
	}
	// A single-quoted caret is literal, so the cutter does not unescape it.
	// The quote still splits on ^, and the tail is kept off the shared tree.
	quotedCaret := "cd '..\\..\\..^\\Repo'"
	if msg := cb.parallelCheckoutCommandRejection(quotedCaret, exec); msg == "" {
		t.Fatal("quoted caret was allowed to enter the shared tree")
	}
	quotedCaretArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": quotedCaret})
	if got, _ := quotedCaretArgs["command"].(string); got != `cd '..\..\..^`+wt+`'` {
		t.Fatalf("quoted caret = %q", got)
	}

	// Invoke-Expression runs in this process. The climb sticks, so the next
	// bare name is the shared tree. A child cmd does not stick.
	for _, iex := range []string{
		"iex \"cd ..\\..\\..\"; Copy-Item Repo .",
		"Invoke-Expression \"cd ..\\..\\..\"; Copy-Item Repo .",
		"iex -ErrorAction Stop \"cd ..\\..\\..\"; Copy-Item Repo .",
		"iex -Verbose -ErrorAction Stop \"cd ..\\..\\..\"; Copy-Item Repo .",
		"iex `\r\n\"cd ..\\..\\..\"; Copy-Item Repo .",
	} {
		if msg := cb.parallelCheckoutCommandRejection(iex, exec); msg == "" {
			t.Fatalf("invoke-expression was allowed to enter the shared tree: %s", iex)
		}
		iexArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": iex})
		got, _ := iexArgs["command"].(string)
		want := iex
		// The script climbs to the drive root and does not itself name the
		// tree. Only the following bare name moves onto the checkout.
		const marker = "Copy-Item Repo ."
		if strings.HasSuffix(iex, marker) {
			want = strings.TrimSuffix(iex, marker) + "Copy-Item " + wt + " ."
		}
		if got != want {
			t.Fatalf("invoke-expression = %q", got)
		}
	}
	childCopy := "cmd /c \"cd ..\\..\\..\"; Copy-Item Repo ."
	childCopyArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": childCopy})
	if got, _ := childCopyArgs["command"].(string); got != childCopy {
		t.Fatalf("child cmd copy = %q", got)
	}
	if msg := cb.parallelCheckoutCommandRejection(childCopy, exec); msg != "" {
		t.Fatalf("child cmd copy rejected: %s", msg)
	}

	// PowerShell runs cd.. and cd\. cmd also glues the path to cd/chdir/pushd,
	// and a module qualifier or a quoted call is still that command.
	gluedUp := "cd..; cd..; Copy-Item Repo ."
	if msg := cb.parallelCheckoutCommandRejection(gluedUp, exec); msg == "" {
		t.Fatal("cd.. was allowed to enter the shared tree")
	}
	gluedUpArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": gluedUp})
	if got, _ := gluedUpArgs["command"].(string); got != "cd..; cd..; Copy-Item "+wt+" ." {
		t.Fatalf("cd.. = %q", got)
	}
	gluedRoot := `cd\; Copy-Item Repo .`
	if msg := cb.parallelCheckoutCommandRejection(gluedRoot, exec); msg == "" {
		t.Fatal("cd\\ was allowed to enter the shared tree")
	}
	gluedRootArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": gluedRoot})
	if got, _ := gluedRootArgs["command"].(string); got != `cd\; Copy-Item `+wt+" ." {
		t.Fatalf("cd\\ = %q", got)
	}
	gluedPath := `cd..\..\..\Repo`
	if msg := cb.parallelCheckoutCommandRejection(gluedPath, exec); msg == "" {
		t.Fatal("glued path was allowed to enter the shared tree")
	}
	gluedPathArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": gluedPath})
	if got, _ := gluedPathArgs["command"].(string); got != "cd "+wt {
		t.Fatalf("glued path = %q", got)
	}
	qualified := `Microsoft.PowerShell.Management\Set-Location ..\..\..; Copy-Item Repo .`
	if msg := cb.parallelCheckoutCommandRejection(qualified, exec); msg == "" {
		t.Fatal("qualified Set-Location was allowed to enter the shared tree")
	}
	qualifiedArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": qualified})
	if got, _ := qualifiedArgs["command"].(string); got != `Microsoft.PowerShell.Management\Set-Location ..\..\..; Copy-Item `+wt+" ." {
		t.Fatalf("qualified Set-Location = %q", got)
	}
	called := `& 'cd' ..\..\..; Copy-Item Repo .`
	calledArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": called})
	if got, _ := calledArgs["command"].(string); got != `& 'cd' ..\..\..; Copy-Item `+wt+" ." {
		t.Fatalf("quoted cd = %q", got)
	}
	qualifiedIex := `Microsoft.PowerShell.Utility\Invoke-Expression "cd ..\..\.."; Copy-Item Repo .`
	qualifiedIexArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": qualifiedIex})
	if got, _ := qualifiedIexArgs["command"].(string); got != `Microsoft.PowerShell.Utility\Invoke-Expression "cd ..\..\.."; Copy-Item `+wt+" ." {
		t.Fatalf("qualified iex = %q", got)
	}
	cmdPath := `C:\Windows\System32\cmd.exe /c "cd..\..\..& copy Repo\a.go ."`
	cmdPathArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": cmdPath})
	if got, _ := cmdPathArgs["command"].(string); got != `C:\Windows\System32\cmd.exe /c "cd..\..\..& copy `+filepath.Join(wt, "a.go")+` ."` {
		t.Fatalf("cmd.exe path = %q", got)
	}
	cdSwitch := `cd/d ..\..\..& copy Repo\a.go .`
	cdSwitchArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": cdSwitch})
	if got, _ := cdSwitchArgs["command"].(string); got != `cd/d ..\..\..& copy `+filepath.Join(wt, "a.go")+` .` {
		t.Fatalf("cd/d = %q", got)
	}

	// cmd.exe does not treat ' as a quote, so & inside the quotes still
	// runs. 2>nul is what puts the line on cmd. The same text without it
	// stays PowerShell, where the quotes hide the cd.
	cmdQuote := `echo ' & cd ..\..\.. & copy Repo\a.go . & ' 2>nul`
	if msg := cb.parallelCheckoutCommandRejection(cmdQuote, exec); msg == "" {
		t.Fatal("cmd single quote was allowed to enter the shared tree")
	}
	cmdQuoteArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": cmdQuote})
	cmdQuoteWant := `echo ' & cd ..\..\.. & copy ` + filepath.Join(wt, "a.go") + ` . & ' 2>nul`
	if got, _ := cmdQuoteArgs["command"].(string); got != cmdQuoteWant {
		t.Fatalf("cmd single quote = %q", got)
	}
	psQuote := `echo ' & cd ..\..\.. & copy Repo\a.go . & '`
	psQuoteArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": psQuote})
	if got, _ := psQuoteArgs["command"].(string); got != psQuote {
		t.Fatalf("powershell single quote = %q", got)
	}
	if msg := cb.parallelCheckoutCommandRejection(psQuote, exec); msg != "" {
		t.Fatalf("powershell single quote rejected: %s", msg)
	}
	// The outer host is PowerShell. The cmd /c body is still cmd, so the
	// single quotes inside it do not hide &. The parent's next command
	// stays on the checkout.
	nestedQuote := `cmd /c "echo ' & cd ..\..\.. & copy Repo\a.go . & '"; Copy-Item Repo .`
	if msg := cb.parallelCheckoutCommandRejection(nestedQuote, exec); msg == "" {
		t.Fatal("nested cmd single quote was allowed to enter the shared tree")
	}
	nestedQuoteArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": nestedQuote})
	nestedQuoteWant := `cmd /c "echo ' & cd ..\..\.. & copy ` + filepath.Join(wt, "a.go") + ` . & '"; Copy-Item Repo .`
	if got, _ := nestedQuoteArgs["command"].(string); got != nestedQuoteWant {
		t.Fatalf("nested cmd single quote = %q", got)
	}
	// call runs the next command in this process, including a glued cd.
	for _, called := range []string{
		`call cd ..\..\.. & copy Repo\a.go .`,
		`call cd..\..\.. & copy Repo\a.go .`,
		`cmd /c "call cd ..\..\.. & copy Repo\a.go ."`,
		`& 'call' cd ..\..\.. & copy Repo\a.go .`,
	} {
		if msg := cb.parallelCheckoutCommandRejection(called, exec); msg == "" {
			t.Fatalf("call cd was allowed to enter the shared tree: %s", called)
		}
		calledArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": called})
		got, _ := calledArgs["command"].(string)
		want := strings.Replace(called, `copy Repo\a.go .`, `copy `+filepath.Join(wt, "a.go")+` .`, 1)
		if got != want {
			t.Fatalf("call cd = %q", got)
		}
	}

	// /d/Repo is a root-relative PowerShell name, and D:\Repo to nested bash.
	msys := `bash -lc "cat /d/Repo/pkg/a.go"`
	if msg := cb.parallelCheckoutCommandRejection(msys, exec); msg == "" {
		t.Fatal("MSYS path was allowed to enter the shared tree")
	}
	msysArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": msys})
	msysCmd, _ := msysArgs["command"].(string)
	msysWant := `bash -lc "cat ` + filepath.ToSlash(filepath.Join(exec, "a.go")) + `"`
	if msysCmd != msysWant {
		t.Fatalf("MSYS path = %q", msysCmd)
	}
	msysKeep := `cat /c/Windows/notepad.exe`
	msysKeepArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": msysKeep})
	if got, _ := msysKeepArgs["command"].(string); got != msysKeep {
		t.Fatalf("foreign MSYS path = %q", got)
	}
	if msg := cb.parallelCheckoutCommandRejection(msysKeep, exec); msg != "" {
		t.Fatalf("foreign MSYS path rejected: %s", msg)
	}

	for _, keepCmd := range []string{`go test ./... 2>&1`, `cd ..`, `.\hello.exe`} {
		keepArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": keepCmd})
		gotKeep, _ := keepArgs["command"].(string)
		if gotKeep != keepCmd {
			t.Fatalf("command %q became %q", keepCmd, gotKeep)
		}
		if msg := cb.parallelCheckoutCommandRejection(gotKeep, stringVal(keepArgs, "working_dir")); msg != "" {
			t.Fatalf("command %q rejected: %s", keepCmd, msg)
		}
	}
	outsideCmd := `cd C:\Windows`
	outsideArgs := cb.withDefaultWorkingDir(map[string]interface{}{"command": outsideCmd, "working_dir": outside})
	if got, _ := outsideArgs["command"].(string); got != outsideCmd {
		t.Fatalf("outside command = %q", got)
	}
	if got, _ := outsideArgs["working_dir"].(string); got != outside {
		t.Fatalf("outside working_dir = %q", got)
	}

	searchArgs := cb.withDefaultProjectPath(map[string]interface{}{"path": filepath.Join(primary, "A.go"), "pattern": "func"})
	if got, _ := searchArgs["path"].(string); got != filepath.Join(exec, "A.go") {
		t.Fatalf("search path = %q", got)
	}

	codingParallelTurns.Delete(codingParallelTurnKey(task))
	plain := &codingSubAgentCallbacks{subagent: &CodingSubAgent{
		projectPath:       exec,
		permissionOwnerID: projectSessionOwnerID(task),
	}}
	abs := filepath.Join(primary, "A.go")
	plainArgs := plain.withProjectRelativePath(map[string]interface{}{"path": abs}, false)
	if got, _ := plainArgs["path"].(string); got != abs {
		t.Fatalf("without a turn = %q", got)
	}
	plainCmd := "cd '" + abs + "'"
	plainBash := plain.withDefaultWorkingDir(map[string]interface{}{"command": plainCmd, "working_dir": abs})
	if got, _ := plainBash["working_dir"].(string); got != abs {
		t.Fatalf("working_dir without a turn = %q", got)
	}
	if got, _ := plainBash["command"].(string); got != plainCmd {
		t.Fatalf("command without a turn = %q", got)
	}

	stay := &codingParallelTurn{primary: primary, execDir: primary, stayPut: true}
	codingParallelTurns.Store(codingParallelTurnKey(task), stay)
	stayCB := &codingSubAgentCallbacks{subagent: &CodingSubAgent{
		projectPath:       primary,
		permissionOwnerID: projectSessionOwnerID(task),
	}}
	stayArgs := stayCB.withProjectRelativePath(map[string]interface{}{"path": abs}, false)
	if got, _ := stayArgs["path"].(string); got != abs {
		t.Fatalf("stayPut path = %q", got)
	}
	stayCmd := "cd '" + abs + "'"
	stayBash := stayCB.withDefaultWorkingDir(map[string]interface{}{"command": stayCmd, "working_dir": abs})
	if got, _ := stayBash["working_dir"].(string); got != abs {
		t.Fatalf("stayPut working_dir = %q", got)
	}
	if got, _ := stayBash["command"].(string); got != stayCmd {
		t.Fatalf("stayPut command = %q", got)
	}
	if msg := stayCB.parallelCheckoutCommandRejection(stayCmd, abs); msg != "" {
		t.Fatalf("stayPut rejected: %s", msg)
	}
}

func TestCodingParallelPreviewOutsideRepoDoesNotWidenRoot(t *testing.T) {
	repo := filepath.Join(`D:\`, "Repo")
	wt := filepath.Join(`D:\`, "wt")
	primary := filepath.Join(repo, "pkg")
	exec := filepath.Join(wt, "pkg")
	turn := &codingParallelTurn{
		deferred: true,
		merged:   true,
		primary:  primary,
		execDir:  exec,
		wt: &codingWorkbenchWorktree{
			Path:        wt,
			GitRoot:     repo,
			ProjectPath: exec,
		},
	}
	outside := filepath.Join(`C:\`, "Windows", "notepad.exe")
	root, modified, created := turn.previewPaths(exec, []string{"note.go", outside}, nil)
	if root != primary || created != nil {
		t.Fatalf("outside repo root=%q created=%v", root, created)
	}
	sawNote, sawOutside := false, false
	for _, path := range modified {
		switch path {
		case "note.go":
			sawNote = true
		case outside:
			sawOutside = true
		default:
			t.Fatalf("outside repo path %q", path)
		}
	}
	if !sawNote || !sawOutside || len(modified) != 2 {
		t.Fatalf("outside repo modified=%v", modified)
	}

	root, modified, created = turn.previewPaths(exec, []string{
		"note.go",
		filepath.Join(wt, "other", "b.go"),
	}, nil)
	if root != repo || created != nil {
		t.Fatalf("sibling root=%q created=%v", root, created)
	}
	want := map[string]bool{
		filepath.Join(primary, "note.go"):    true,
		filepath.Join(repo, "other", "b.go"): true,
	}
	if len(modified) != len(want) {
		t.Fatalf("sibling modified=%v", modified)
	}
	for _, path := range modified {
		if !want[path] {
			t.Fatalf("sibling path %q", path)
		}
	}
}

func newDeferredParallelCheckout(t *testing.T) (string, *codingParallelTurn) {
	t.Helper()
	primary := initParallelRepo(t)
	wt, err := createCodingWorkbenchWorktree(primary, 1, "parallel-turn")
	if err != nil || wt == nil || strings.TrimSpace(wt.ProjectPath) == "" {
		t.Fatalf("create checkout: %v", err)
	}
	if !strings.Contains(filepath.ToSlash(wt.ProjectPath), "maclaw-coding-worktrees") {
		wt.cleanup(false)
		t.Fatalf("checkout %s is not under maclaw-coding-worktrees", wt.ProjectPath)
	}
	turn := &codingParallelTurn{
		primary:  primary,
		execDir:  wt.ProjectPath,
		wt:       wt,
		deferred: true,
	}
	codingParallelRememberExec(turn)
	t.Cleanup(func() {
		codingParallelForgetExec(turn)
		wt.cleanup(false)
	})
	return primary, turn
}

func deferredSpawnCallbacks(projectPath string) *codingSubAgentCallbacks {
	return &codingSubAgentCallbacks{subagent: &CodingSubAgent{
		fullEnvironment: true,
		projectPath:     projectPath,
	}}
}

func withCodingSpawnNestedStub(t *testing.T, fn func(*CodingSubAgent, codingSpawnSpec, *codingSubAgentCallbacks, func(string)) *CodingSubAgentResult) {
	t.Helper()
	prev := codingSpawnRunNestedAgent
	codingSpawnRunNestedAgent = fn
	t.Cleanup(func() { codingSpawnRunNestedAgent = prev })
}

func writeSpawnFile(dir, name, body string) error {
	path := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(body), 0o644)
}

func parallelFileAbsent(t *testing.T, dir, name string) {
	t.Helper()
	_, err := os.Stat(filepath.Join(dir, filepath.FromSlash(name)))
	if !os.IsNotExist(err) {
		t.Fatalf("%s still present: %v", name, err)
	}
}

func TestCodingParallelSpawnIsolateMatchesDeferredCheckout(t *testing.T) {
	primary, turn := newDeferredParallelCheckout(t)
	if codingSpawnIsolateAllowed(primary, primary) {
		t.Fatal("the user's directory must not accept a worker isolate")
	}
	if !codingSpawnIsolateAllowed(primary, filepath.Join(primary, "other")) {
		t.Fatal("a distinct isolate stays allowed")
	}
	if !codingSpawnIsolateAllowed(turn.execDir, turn.execDir) {
		t.Fatal("deferred checkout must allow a worker to edit that checkout")
	}
}

func TestCodingParallelSpawnWorkerStaysOnDeferredCheckout(t *testing.T) {
	primary, turn := newDeferredParallelCheckout(t)
	writeParallelFile(t, turn.execDir, "README", "parent\n")
	var got string
	withCodingSpawnNestedStub(t, func(_ *CodingSubAgent, spec codingSpawnSpec, _ *codingSubAgentCallbacks, _ func(string)) *CodingSubAgentResult {
		got = spec.projectPath
		if err := writeSpawnFile(spec.projectPath, "note.go", "worker\n"); err != nil {
			return &CodingSubAgentResult{Status: TaskExecFailed, Error: err.Error()}
		}
		return &CodingSubAgentResult{Status: TaskExecPassed}
	})
	res := deferredSpawnCallbacks(turn.execDir).executeSpawnCodingAgent(map[string]interface{}{
		"role":  "worker",
		"task":  "add note",
		"files": []interface{}{"note.go"},
	})
	if res.Outcome != codingToolOutcomeSuccess {
		t.Fatalf("spawn = %#v", res)
	}
	if !codingSpawnLocalPathsEqual(got, turn.execDir) {
		t.Fatalf("worker path = %q checkout = %q", got, turn.execDir)
	}
	if !strings.Contains(res.Text, "edits kept on the shared checkout") || strings.Contains(res.Text, "refusing nested") {
		t.Fatalf("text = %s", res.Text)
	}
	if readParallelFile(t, turn.execDir, "note.go") != "worker\n" {
		t.Fatal("declared edit was not left on the checkout")
	}
	if readParallelFile(t, turn.execDir, "README") != "parent\n" {
		t.Fatal("parent edit on the checkout was overwritten")
	}
	if readParallelFile(t, primary, "README") != "base\n" {
		t.Fatal("primary README changed")
	}
	parallelFileAbsent(t, primary, "note.go")
}

func TestCodingParallelSpawnUndeclaredEditRollsBack(t *testing.T) {
	primary, turn := newDeferredParallelCheckout(t)
	writeParallelFile(t, turn.execDir, "README", "parent\n")
	writeParallelFile(t, turn.execDir, "keep.go", "keep\n")
	withCodingSpawnNestedStub(t, func(_ *CodingSubAgent, spec codingSpawnSpec, _ *codingSubAgentCallbacks, _ func(string)) *CodingSubAgentResult {
		if err := writeSpawnFile(spec.projectPath, "note.go", "worker\n"); err != nil {
			return &CodingSubAgentResult{Status: TaskExecFailed, Error: err.Error()}
		}
		if err := writeSpawnFile(spec.projectPath, "extra.go", "nope\n"); err != nil {
			return &CodingSubAgentResult{Status: TaskExecFailed, Error: err.Error()}
		}
		if err := writeSpawnFile(spec.projectPath, "README", "worker\n"); err != nil {
			return &CodingSubAgentResult{Status: TaskExecFailed, Error: err.Error()}
		}
		return &CodingSubAgentResult{Status: TaskExecPassed}
	})
	res := deferredSpawnCallbacks(turn.execDir).executeSpawnCodingAgent(map[string]interface{}{
		"role":  "worker",
		"task":  "add note",
		"files": []interface{}{"note.go"},
	})
	if res.Outcome != codingToolOutcomeFailed || !strings.Contains(res.Text, "undeclared") {
		t.Fatalf("spawn = %#v", res)
	}
	if readParallelFile(t, turn.execDir, "README") != "parent\n" {
		t.Fatal("tracked parent edit was not restored")
	}
	if readParallelFile(t, turn.execDir, "keep.go") != "keep\n" {
		t.Fatal("untouched untracked file was rolled back")
	}
	parallelFileAbsent(t, turn.execDir, "note.go")
	parallelFileAbsent(t, turn.execDir, "extra.go")
	if readParallelFile(t, primary, "README") != "base\n" {
		t.Fatal("primary README changed")
	}
	parallelFileAbsent(t, primary, "note.go")
	parallelFileAbsent(t, primary, "extra.go")
}

func TestCodingParallelSpawnWithoutTurnRefusesNestedWorktree(t *testing.T) {
	skipWithoutGit(t)
	repo := filepath.Join(t.TempDir(), "maclaw-coding-worktrees", "child")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "init")
	mustGit(t, repo, "config", "user.email", "test@example.com")
	mustGit(t, repo, "config", "user.name", "test")
	writeParallelFile(t, repo, "README", "base\n")
	mustGit(t, repo, "add", "README")
	mustGit(t, repo, "commit", "-m", "init")
	calls := 0
	withCodingSpawnNestedStub(t, func(_ *CodingSubAgent, _ codingSpawnSpec, _ *codingSubAgentCallbacks, _ func(string)) *CodingSubAgentResult {
		calls++
		return &CodingSubAgentResult{Status: TaskExecPassed}
	})
	res := deferredSpawnCallbacks(repo).executeSpawnCodingAgent(map[string]interface{}{
		"role":  "worker",
		"task":  "add note",
		"files": []interface{}{"note.go"},
	})
	if calls != 0 || res.Outcome != codingToolOutcomeFailed || !strings.Contains(res.Text, "refusing nested") {
		t.Fatalf("calls=%d spawn=%#v", calls, res)
	}
}

func TestCodingParallelSpawnWorkersSerializeOnDeferredCheckout(t *testing.T) {
	primary, turn := newDeferredParallelCheckout(t)
	var mu sync.Mutex
	var active, maxActive int
	var paths []string
	withCodingSpawnNestedStub(t, func(_ *CodingSubAgent, spec codingSpawnSpec, _ *codingSubAgentCallbacks, _ func(string)) *CodingSubAgentResult {
		mu.Lock()
		active++
		if active > maxActive {
			maxActive = active
		}
		paths = append(paths, spec.projectPath)
		mu.Unlock()
		time.Sleep(40 * time.Millisecond)
		for _, name := range spec.Files {
			if err := writeSpawnFile(spec.projectPath, name, "worker\n"); err != nil {
				return &CodingSubAgentResult{Status: TaskExecFailed, Error: err.Error()}
			}
		}
		mu.Lock()
		active--
		mu.Unlock()
		return &CodingSubAgentResult{Status: TaskExecPassed}
	})
	res := deferredSpawnCallbacks(turn.execDir).executeSpawnCodingAgent(map[string]interface{}{
		"agents": []interface{}{
			map[string]interface{}{"role": "worker", "task": "add note", "files": []interface{}{"note.go"}},
			map[string]interface{}{"role": "worker", "task": "add other", "files": []interface{}{"other.go"}},
		},
	})
	if res.Outcome != codingToolOutcomeSuccess {
		t.Fatalf("spawn = %#v", res)
	}
	if maxActive != 1 || len(paths) != 2 {
		t.Fatalf("maxActive=%d paths=%v", maxActive, paths)
	}
	for _, path := range paths {
		if !codingSpawnLocalPathsEqual(path, turn.execDir) {
			t.Fatalf("worker path = %q checkout = %q", path, turn.execDir)
		}
	}
	if !strings.Contains(res.Text, "mode=sequential") || strings.Contains(res.Text, "mode=parallel") || strings.Contains(res.Text, "refusing nested") {
		t.Fatalf("text = %s", res.Text)
	}
	if readParallelFile(t, turn.execDir, "note.go") != "worker\n" || readParallelFile(t, turn.execDir, "other.go") != "worker\n" {
		t.Fatal("serialized workers did not both leave their declared file")
	}
	parallelFileAbsent(t, primary, "note.go")
	parallelFileAbsent(t, primary, "other.go")
}

func newDeferredSubdirCheckout(t *testing.T) (string, *codingParallelTurn) {
	t.Helper()
	primary := initParallelRepo(t)
	writeParallelFile(t, primary, "pkg/keep.go", "package keep\n")
	mustGit(t, primary, "add", "pkg/keep.go")
	mustGit(t, primary, "commit", "-m", "pkg")
	wt, err := createCodingWorkbenchWorktree(filepath.Join(primary, "pkg"), 1, "parallel-turn")
	if err != nil || wt == nil || strings.TrimSpace(wt.ProjectPath) == "" {
		t.Fatalf("create checkout: %v", err)
	}
	if codingSpawnLocalPathsEqual(wt.Path, wt.ProjectPath) {
		wt.cleanup(false)
		t.Fatal("subdirectory project was given the worktree root")
	}
	turn := &codingParallelTurn{
		primary:  filepath.Join(primary, "pkg"),
		execDir:  wt.ProjectPath,
		wt:       wt,
		deferred: true,
	}
	codingParallelRememberExec(turn)
	t.Cleanup(func() {
		codingParallelForgetExec(turn)
		wt.cleanup(false)
	})
	return primary, turn
}

func TestCodingParallelSpawnSubdirDeclaredFileStays(t *testing.T) {
	primary, turn := newDeferredSubdirCheckout(t)
	writeParallelFile(t, turn.execDir, "keep.go", "parent\n")
	withCodingSpawnNestedStub(t, func(_ *CodingSubAgent, spec codingSpawnSpec, _ *codingSubAgentCallbacks, _ func(string)) *CodingSubAgentResult {
		if !codingSpawnLocalPathsEqual(spec.projectPath, turn.execDir) {
			return &CodingSubAgentResult{Status: TaskExecFailed, Error: "worker left the subdirectory checkout"}
		}
		if err := writeSpawnFile(spec.projectPath, "note.go", "worker\n"); err != nil {
			return &CodingSubAgentResult{Status: TaskExecFailed, Error: err.Error()}
		}
		return &CodingSubAgentResult{Status: TaskExecPassed}
	})
	res := deferredSpawnCallbacks(turn.execDir).executeSpawnCodingAgent(map[string]interface{}{
		"role":  "worker",
		"task":  "add note",
		"files": []interface{}{"note.go"},
	})
	if res.Outcome != codingToolOutcomeSuccess {
		t.Fatalf("spawn = %#v", res)
	}
	if readParallelFile(t, turn.execDir, "note.go") != "worker\n" {
		t.Fatal("declared subdirectory edit was rolled back")
	}
	if readParallelFile(t, turn.execDir, "keep.go") != "parent\n" {
		t.Fatal("parent edit in the subdirectory was overwritten")
	}
	if readParallelFile(t, primary, "pkg/keep.go") != "package keep\n" {
		t.Fatal("primary subdirectory file changed")
	}
	parallelFileAbsent(t, primary, "pkg/note.go")
}

func TestCodingParallelSpawnOutsideSubdirRollsBack(t *testing.T) {
	primary, turn := newDeferredSubdirCheckout(t)
	writeParallelFile(t, turn.execDir, "keep.go", "parent\n")
	withCodingSpawnNestedStub(t, func(_ *CodingSubAgent, spec codingSpawnSpec, _ *codingSubAgentCallbacks, _ func(string)) *CodingSubAgentResult {
		if err := writeSpawnFile(spec.projectPath, "note.go", "worker\n"); err != nil {
			return &CodingSubAgentResult{Status: TaskExecFailed, Error: err.Error()}
		}
		if err := writeSpawnFile(spec.projectPath, "keep.go", "worker\n"); err != nil {
			return &CodingSubAgentResult{Status: TaskExecFailed, Error: err.Error()}
		}
		if err := writeSpawnFile(filepath.Dir(spec.projectPath), "README", "worker\n"); err != nil {
			return &CodingSubAgentResult{Status: TaskExecFailed, Error: err.Error()}
		}
		return &CodingSubAgentResult{Status: TaskExecPassed}
	})
	res := deferredSpawnCallbacks(turn.execDir).executeSpawnCodingAgent(map[string]interface{}{
		"role":  "worker",
		"task":  "add note",
		"files": []interface{}{"note.go"},
	})
	if res.Outcome != codingToolOutcomeFailed || !strings.Contains(res.Text, "undeclared") {
		t.Fatalf("spawn = %#v", res)
	}
	parallelFileAbsent(t, turn.execDir, "note.go")
	if readParallelFile(t, turn.execDir, "keep.go") != "parent\n" {
		t.Fatal("subdirectory parent edit was not restored")
	}
	if readParallelFile(t, filepath.Dir(turn.execDir), "README") != "base\n" {
		t.Fatal("checkout root file was not restored")
	}
	if readParallelFile(t, primary, "README") != "base\n" || readParallelFile(t, primary, "pkg/keep.go") != "package keep\n" {
		t.Fatal("primary gained worker bytes")
	}
	parallelFileAbsent(t, primary, "pkg/note.go")
}
