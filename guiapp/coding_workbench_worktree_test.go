package guiapp

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeCodingWorktreeMode(t *testing.T) {
	if normalizeCodingWorktreeMode("") != codingWorktreeModeAuto {
		t.Fatal("default auto")
	}
	if normalizeCodingWorktreeMode("always") != codingWorktreeModeAlways {
		t.Fatal("always")
	}
	if normalizeCodingWorktreeMode("off") != codingWorktreeModeOff {
		t.Fatal("off")
	}
}

func TestShouldUseCodingWorktree(t *testing.T) {
	if shouldUseCodingWorktree(codingWorktreeModeOff, true, "implement x", "write code", 3, 2, nil) {
		t.Fatal("off never")
	}
	if shouldUseCodingWorktree(codingWorktreeModeAuto, true, "explore auth", "探查代码", 3, 2, nil) {
		t.Fatal("explore never isolates")
	}
	// auto: actual wave size > 1 → isolate write steps
	if !shouldUseCodingWorktree(codingWorktreeModeAuto, true, "implement JWT", "写代码", 3, 2, []int{1}) {
		t.Fatal("auto+WaveSize2 should isolate even with deps")
	}
	// auto: WaveSize 1 alone → no isolate
	if shouldUseCodingWorktree(codingWorktreeModeAuto, true, "implement JWT", "写代码", 3, 1, nil) {
		t.Fatal("auto+WaveSize1 should not isolate")
	}
	// legacy: wave 0 + independent + MaxParallel>1
	if !shouldUseCodingWorktree(codingWorktreeModeAuto, true, "implement JWT", "写代码", 3, 0, nil) {
		t.Fatal("legacy independent should isolate")
	}
	if shouldUseCodingWorktree(codingWorktreeModeAuto, true, "implement JWT", "写代码", 3, 0, []int{1}) {
		t.Fatal("legacy chained should not")
	}
	if !shouldUseCodingWorktree(codingWorktreeModeAlways, true, "implement JWT", "写代码", 1, 1, []int{1}) {
		t.Fatal("always isolates implement")
	}
}

func TestRemapWorktreePaths(t *testing.T) {
	wt := filepath.Join(string(filepath.Separator), "tmp", "wt", "proj")
	main := filepath.Join(string(filepath.Separator), "repo", "proj")
	src := filepath.Join(wt, "a.go")
	got := remapWorktreePaths([]string{src, "relative.go"}, wt, main)
	if len(got) < 1 {
		t.Fatal("expected remapped paths")
	}
	// Absolute under worktree should map into main.
	found := false
	for _, p := range got {
		if strings.Contains(filepath.ToSlash(p), "repo") && strings.HasSuffix(filepath.ToSlash(p), "a.go") {
			found = true
		}
	}
	if !found {
		t.Fatalf("paths=%v", got)
	}
}

func TestCreateAndMergeCodingWorkbenchWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "init")

	wt, err := createCodingWorkbenchWorktree(root, 1, "implement-feature")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if wt == nil {
		t.Fatal("expected worktree")
	}
	defer wt.cleanup(false)

	// Modify in worktree.
	path := filepath.Join(wt.ProjectPath, "main.go")
	if err := os.WriteFile(path, []byte("package main\n\nfunc feature() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Dirty main (simulate prior step).
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	merged, sum, err := wt.mergeBack(root)
	if err == nil || merged || sum != "" {
		t.Fatalf("dirty primary merge = merged:%v summary:%q err:%v", merged, sum, err)
	}
	if err := os.Remove(filepath.Join(root, "notes.txt")); err != nil {
		t.Fatal(err)
	}
	merged, sum, err = wt.mergeBack(root)
	if err != nil {
		t.Fatalf("controlled merge: %v", err)
	}
	if !merged {
		t.Fatalf("expected controlled merge, sum=%q", sum)
	}
	data, err := os.ReadFile(filepath.Join(root, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "feature") {
		t.Fatalf("main.go not updated: %s", data)
	}
	if !strings.Contains(sum, "cherry-pick") {
		t.Fatalf("sum=%q", sum)
	}
}

func TestCodingWorkbenchWorktreeRejectsUndeclaredChangedFile(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	for _, name := range []string{"declared.go", "undeclared.go"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("package main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("add", ".")
	run("commit", "-m", "init")
	wt, err := createCodingWorkbenchWorktree(root, 2, "write-set-check")
	if err != nil || wt == nil {
		t.Fatalf("worktree=%+v err=%v", wt, err)
	}
	defer wt.cleanup(false)
	if err := os.WriteFile(filepath.Join(wt.ProjectPath, "undeclared.go"), []byte("package main\nfunc unexpected() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	merged, _, err := wt.mergeBack(root, []string{"declared.go"})
	if err == nil || merged || !strings.Contains(err.Error(), "undeclared") {
		t.Fatalf("undeclared merge = merged:%v err:%v", merged, err)
	}
	data, err := os.ReadFile(filepath.Join(root, "undeclared.go"))
	if err != nil || strings.Contains(string(data), "unexpected") {
		t.Fatalf("primary workspace was modified: %q err=%v", data, err)
	}
}

// Regression for BR-20261124-D9DCBA72D95D: pruneCodingWorkbenchWorktrees runs
// `git worktree list` against the shared git common dir, which enumerates every
// worktree of the repo — not just the ones belonging to the session being
// reset. The old prefix-only sweep removed a *live* checkout another task was
// still executing in, orphaning its guardrail working dir. A prune must leave
// any checkout still inside its create→cleanup lifetime intact while still
// removing true orphans.
func TestPruneSkipsLiveWorktreeAndRemovesOrphan(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	run := func(dir string, args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run(root, "init")
	run(root, "config", "user.email", "test@example.com")
	run(root, "config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(root, "add", ".")
	run(root, "commit", "-m", "init")

	// Live checkout: task A created a worktree and has not finished with it.
	live, err := createCodingWorkbenchWorktree(root, 1, "live-task")
	if err != nil || live == nil {
		t.Fatalf("create live worktree=%+v err=%v", live, err)
	}
	defer live.cleanup(false)

	// Orphan checkout: a worktree that exists in git metadata + on disk but is
	// not part of any create→cleanup lifetime (created directly, so it never
	// entered codingWorkbenchLiveWorktrees). Prune must still reclaim these.
	wtRoot := filepath.Join(os.TempDir(), "maclaw-coding-worktrees")
	if err := os.MkdirAll(wtRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	orphanPath := filepath.Join(wtRoot, "0-orphan-branch")
	_ = os.RemoveAll(orphanPath)
	run(root, "worktree", "add", "-b", "maclaw/coding-0-orphan-branch", orphanPath, "HEAD")
	t.Cleanup(func() {
		// Best-effort: the prune under test is expected to have removed it.
		_ = exec.Command("git", "-C", root, "worktree", "remove", "--force", orphanPath).Run()
		_ = exec.Command("git", "-C", root, "branch", "-D", "maclaw/coding-0-orphan-branch").Run()
	})

	// Prune triggered by a *different* session's reset. Before the fix this
	// deleted live.Path while task A was still writing into it.
	if _, err := pruneCodingWorkbenchWorktrees(root); err != nil {
		t.Fatalf("prune: %v", err)
	}

	if _, err := os.Stat(live.Path); err != nil {
		t.Fatalf("live worktree was pruned while still in use: %v", err)
	}
	if _, err := os.Stat(orphanPath); !os.IsNotExist(err) {
		t.Fatalf("orphan worktree was not pruned: stat err=%v", err)
	}
}

func TestIsCodingWorkbenchSlashWorktree(t *testing.T) {
	if !isCodingWorkbenchSlash("/worktree mode always") {
		t.Fatal("expected worktree slash")
	}
	if classifyImmediateIMCommand("/worktree") != imCommandCodingWorkbench {
		t.Fatal("classify")
	}
}

func TestStickyWorktreeMode(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:wt-mode"
	h.setStickyCodingWorktreeMode(userID, "always")
	if h.getStickyCodingWorktreeMode(userID) != codingWorktreeModeAlways {
		t.Fatal(h.getStickyCodingWorktreeMode(userID))
	}
	h.clearStickyCodingWorkbenchMemory(userID)
}
