package guiapp

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoteSetupScriptsAreProcessesNotLoginShellInput(t *testing.T) {
	worktree := remoteGitWorktreeAddCommand("/home/testprj", "maclaw/coding-1", "/tmp/maclaw-wt-1")
	if strings.Contains(worktree, "__MACLAW_WT_OK__") {
		t.Fatalf("worktree success is the process exit status, not a marker inside the script: %s", worktree)
	}
	if !strings.Contains(worktree, "git -C '/home/testprj' worktree add -b") || !strings.HasPrefix(worktree, "set -e\n") || strings.Contains(worktree, "\ncd ") {
		t.Fatalf("worktree script=%s", worktree)
	}

	copyCmd := remoteCodingCopyIsolateCommand("/home/testprj", "/tmp/maclaw-coding-1")
	if strings.Contains(copyCmd, "__MACLAW_ISO_OK__") || strings.Contains(copyCmd, ".git/objects/pack") || !strings.HasPrefix(copyCmd, "set -e\n") {
		t.Fatalf("copy script=%s", copyCmd)
	}

	dirty := remoteIsolateDirtyCommand("/tmp/maclaw-wt-1")
	if dirty != "git -C '/tmp/maclaw-wt-1' status --porcelain" {
		t.Fatalf("dirty script=%s", dirty)
	}

	cleanup := strings.TrimSpace(remoteWorktreeCleanupCommand("/home/testprj", "/tmp/maclaw-wt-1"))
	if !strings.Contains(cleanup, `if [ -e "$WT" ]; then fail=1; fi`) || !strings.HasSuffix(cleanup, `exit "$fail"`) {
		t.Fatalf("cleanup exit status must be whether the worktree directory is gone: %s", cleanup)
	}
}

func TestRemoteGitWorktreeMergeAppliesCommittedIsolateWork(t *testing.T) {
	bash, gitBin := remoteMergeTestTools(t)
	root := t.TempDir()
	src := filepath.Join(root, "src")
	wt := filepath.Join(root, "wt")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command(gitBin, args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
		}
		return string(out)
	}
	run(src, "init", "-b", "master")
	run(src, "config", "user.name", "Test")
	run(src, "config", "user.email", "t@example.com")
	run(src, "config", "commit.gpgsign", "false")
	run(src, "config", "core.autocrlf", "false")
	if err := os.WriteFile(filepath.Join(src, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(src, "add", "main.go")
	run(src, "commit", "-m", "init")
	run(src, "worktree", "add", "-b", "maclaw/coding-1", wt, "HEAD")
	if err := os.WriteFile(filepath.Join(wt, "main.go"), []byte("package main\n\nfunc remote() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, "说明.go"), []byte("package note\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(wt, "add", "main.go", "说明.go")
	run(wt, "commit", "-m", "isolate")

	command := remoteGitWorktreeMergeCommand(bashPosixPath(wt), bashPosixPath(src), 1, []string{"main.go", "说明.go"}, "BEGIN_marker", "END_marker")
	cmd := exec.Command(bash, "-c", command)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("merge script: %v\n%s", err, out)
	}
	got, err := os.ReadFile(filepath.Join(src, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "func remote()") {
		t.Fatalf("primary did not receive the isolate commit: %s\nscript output:\n%s", got, out)
	}
	note, err := os.ReadFile(filepath.Join(src, "说明.go"))
	if err != nil || !strings.Contains(string(note), "package note") {
		t.Fatalf("primary did not receive the non-ASCII path: %v\n%s\nscript output:\n%s", err, note, out)
	}

	secretSrc := filepath.Join(root, "secret-src")
	secretWT := filepath.Join(root, "secret-wt")
	if err := os.MkdirAll(secretSrc, 0o755); err != nil {
		t.Fatal(err)
	}
	run(secretSrc, "init", "-b", "master")
	run(secretSrc, "config", "user.name", "Test")
	run(secretSrc, "config", "user.email", "t@example.com")
	run(secretSrc, "config", "commit.gpgsign", "false")
	run(secretSrc, "config", "core.autocrlf", "false")
	if err := os.WriteFile(filepath.Join(secretSrc, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(secretSrc, "add", "main.go")
	run(secretSrc, "commit", "-m", "init")
	run(secretSrc, "worktree", "add", "-b", "maclaw/coding-2", secretWT, "HEAD")
	if err := os.WriteFile(filepath.Join(secretWT, "secret.go"), []byte("package secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(secretWT, "add", "secret.go")
	run(secretWT, "commit", "-m", "undeclared")
	denied := remoteGitWorktreeMergeCommand(bashPosixPath(secretWT), bashPosixPath(secretSrc), 2, []string{"main.go"}, "BEGIN_marker", "END_marker")
	deniedCmd := exec.Command(bash, "-c", denied)
	deniedOut, deniedErr := deniedCmd.CombinedOutput()
	if deniedErr == nil {
		t.Fatalf("undeclared isolate commit was merged: %s", deniedOut)
	}
	if _, err := os.Stat(filepath.Join(secretSrc, "secret.go")); !os.IsNotExist(err) {
		t.Fatalf("primary received an undeclared file, stat err=%v", err)
	}
}

func TestRemoteGitWorktreeMergeKeepsTheWorktreeIndex(t *testing.T) {
	bash, gitBin := remoteMergeTestTools(t)
	root := t.TempDir()
	src := filepath.Join(root, "src")
	wt := filepath.Join(root, "wt")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command(gitBin, args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run(src, "init", "-b", "master")
	run(src, "config", "user.name", "Test")
	run(src, "config", "user.email", "t@example.com")
	run(src, "config", "commit.gpgsign", "false")
	run(src, "config", "core.autocrlf", "false")
	if err := os.WriteFile(filepath.Join(src, "main.go"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(src, "add", "main.go")
	run(src, "commit", "-m", "init")
	run(src, "worktree", "add", "-b", "maclaw/coding-3", wt, "HEAD")
	if err := os.WriteFile(filepath.Join(wt, "main.go"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(wt, "add", "main.go")
	if err := os.WriteFile(filepath.Join(wt, "main.go"), []byte("v3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	staged := run(wt, "rev-parse", ":main.go")

	command := remoteGitWorktreeMergeCommand(bashPosixPath(wt), bashPosixPath(src), 3, []string{"main.go"}, "BEGIN_marker", "END_marker")
	cmd := exec.Command(bash, "-c", command)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("merge script: %v\n%s", err, out)
	}
	got, err := os.ReadFile(filepath.Join(src, "main.go"))
	if err != nil || string(got) != "v3\n" {
		t.Fatalf("primary = %q, err=%v\n%s", got, err, out)
	}
	if again := run(wt, "rev-parse", ":main.go"); again != staged {
		t.Fatalf("worktree index changed from %s to %s", staged, again)
	}
}

func remoteMergeTestTools(t *testing.T) (bash, gitBin string) {
	t.Helper()
	var err error
	if bash, err = exec.LookPath("bash"); err != nil {
		t.Skip("bash is required to run the remote merge script")
	}
	if gitBin, err = exec.LookPath("git"); err != nil {
		t.Skip("git is required to run the remote merge script")
	}
	return bash, gitBin
}

func bashPosixPath(path string) string {
	path = filepath.ToSlash(filepath.Clean(path))
	if len(path) >= 2 && path[1] == ':' {
		return "/" + strings.ToLower(path[:1]) + path[2:]
	}
	return path
}

func TestRemoteGitWorktreeMergeCommandParses(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is required to syntax-check the remote merge script")
	}
	command := remoteGitWorktreeMergeCommand("/tmp/maclaw-wt-1", "/home/testprj", 1, []string{"src/main.cpp"}, "BEGIN_marker", "END_marker")
	out, err := exec.Command("bash", "-n", "-c", command).CombinedOutput()
	if err != nil {
		t.Fatalf("bash -n: %v\n%s\n%s", err, out, command)
	}
}
