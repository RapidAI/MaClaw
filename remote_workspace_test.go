package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDefaultWorkspacePreparerLocksNonGitDirectory(t *testing.T) {
	preparer := NewDefaultWorkspacePreparer()
	projectDir := t.TempDir()

	first, err := preparer.Prepare("sess-1", LaunchSpec{ProjectPath: projectDir})
	if err != nil {
		t.Fatalf("Prepare() first error = %v", err)
	}
	defer first.Release()

	if first.Mode != WorkspaceModeDirect {
		t.Fatalf("workspace mode = %q, want %q", first.Mode, WorkspaceModeDirect)
	}
	if first.IsGitRepo {
		t.Fatal("expected temp directory to be non-git")
	}
	if first.ProjectPath != filepath.Clean(projectDir) {
		t.Fatalf("project path = %q, want %q", first.ProjectPath, filepath.Clean(projectDir))
	}

	_, err = preparer.Prepare("sess-2", LaunchSpec{ProjectPath: projectDir})
	if err == nil {
		t.Fatal("Prepare() second error = nil, want busy workspace error")
	}

	first.Release()
	if _, err := preparer.Prepare("sess-3", LaunchSpec{ProjectPath: projectDir}); err != nil {
		t.Fatalf("Prepare() after release error = %v", err)
	}
}

func TestDefaultWorkspacePreparerCreatesGitWorktree(t *testing.T) {
	preparer := NewDefaultWorkspacePreparer()
	repoDir := t.TempDir()
	subDir := filepath.Join(repoDir, "src")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	initGitRepoWithCommit(t, repoDir)

	first, err := preparer.Prepare("sess-1", LaunchSpec{ProjectPath: subDir})
	if err != nil {
		t.Fatalf("Prepare() first error = %v", err)
	}

	if !first.IsGitRepo {
		t.Fatal("expected git repo to be detected")
	}
	if first.Mode != WorkspaceModeGitWorktree {
		t.Fatalf("workspace mode = %q, want %q", first.Mode, WorkspaceModeGitWorktree)
	}
	if first.GitRoot != filepath.Clean(repoDir) {
		t.Fatalf("git root = %q, want %q", first.GitRoot, filepath.Clean(repoDir))
	}
	if first.RootPath == filepath.Clean(repoDir) {
		t.Fatalf("root path = %q, want dedicated worktree path", first.RootPath)
	}
	if first.ProjectPath == filepath.Clean(subDir) {
		t.Fatalf("project path = %q, want isolated worktree path", first.ProjectPath)
	}
	if _, err := os.Stat(first.ProjectPath); err != nil {
		t.Fatalf("prepared project path stat error = %v", err)
	}

	second, err := preparer.Prepare("sess-2", LaunchSpec{ProjectPath: repoDir})
	if err != nil {
		t.Fatalf("Prepare() second error = %v, want parallel git worktree support", err)
	}

	if second.Mode != WorkspaceModeGitWorktree {
		t.Fatalf("second workspace mode = %q, want %q", second.Mode, WorkspaceModeGitWorktree)
	}
	if second.RootPath == first.RootPath {
		t.Fatalf("second worktree root = %q, want unique root path", second.RootPath)
	}

	firstRoot := first.RootPath
	secondRoot := second.RootPath
	first.Release()
	second.Release()

	if _, err := os.Stat(firstRoot); !os.IsNotExist(err) {
		t.Fatalf("first worktree root should be removed, stat err = %v", err)
	}
	if _, err := os.Stat(secondRoot); !os.IsNotExist(err) {
		t.Fatalf("second worktree root should be removed, stat err = %v", err)
	}
}

func TestDefaultWorkspacePreparerFallsBackToDirectForRepoWithoutCommits(t *testing.T) {
	preparer := NewDefaultWorkspacePreparer()
	repoDir := t.TempDir()
	initGitRepo(t, repoDir)

	first, err := preparer.Prepare("sess-1", LaunchSpec{ProjectPath: repoDir})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	defer first.Release()

	if first.Mode != WorkspaceModeDirect {
		t.Fatalf("workspace mode = %q, want %q", first.Mode, WorkspaceModeDirect)
	}
	if !first.IsGitRepo {
		t.Fatal("expected git repo flag to remain true")
	}
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()

	cmd := exec.Command("git", "init", dir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git init %q error = %v, output=%s", dir, err, string(output))
	}
}

func initGitRepoWithCommit(t *testing.T, dir string) {
	t.Helper()

	initGitRepo(t, dir)

	runGitTest(t, dir, "config", "user.email", "maclaw@example.com")
	runGitTest(t, dir, "config", "user.name", "MaClaw Test")

	readmePath := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readmePath, []byte("seed"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	runGitTest(t, dir, "add", "README.md")
	runGitTest(t, dir, "commit", "-m", "seed")
}

func runGitTest(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v error = %v, output=%s", args, err, string(output))
	}
}
