package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func requireGitForVRepoTest(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not available")
	}
	return path
}

func gitTestRun(t *testing.T, git, dir string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	out, err := runVCSCommand(ctx, git, dir, args...)
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return out
}

func TestGitVirtualRepositoryCommitPushAndRevert(t *testing.T) {
	git := requireGitForVRepoTest(t)
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	work := filepath.Join(root, "work")
	gitTestRun(t, git, root, "init", "--bare", remote)
	gitTestRun(t, git, root, "clone", remote, work)
	gitTestRun(t, git, work, "config", "user.email", "test@example.com")
	gitTestRun(t, git, work, "config", "user.name", "VRepo Test")
	file := filepath.Join(work, "hello.txt")
	if err := os.WriteFile(file, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	req := VirtualRepositoryOperationRequest{Action: "commit_push", Message: "initial"}
	if _, err := executeGitVirtualRepositoryOperation(context.Background(), git, work, req, nil, "", remote); err != nil {
		t.Fatal(err)
	}
	if got := gitTestRun(t, git, work, "status", "--porcelain"); got != "" {
		t.Fatalf("working tree not clean: %q", got)
	}
	if err := os.WriteFile(file, []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "untracked.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := executeGitVirtualRepositoryOperation(context.Background(), git, work, VirtualRepositoryOperationRequest{Action: "revert"}, nil, "", ""); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(file)
	if err != nil || strings.ReplaceAll(string(data), "\r\n", "\n") != "one\n" {
		t.Fatalf("tracked file was not reverted: %q %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(work, "untracked.txt")); err != nil {
		t.Fatalf("untracked file should remain: %v", err)
	}
}

func TestGitAskPassScriptDoesNotContainSecret(t *testing.T) {
	askpass, err := createGitAskPassScript()
	if err != nil {
		t.Fatal(err)
	}
	defer askpass.cleanup()
	data, err := os.ReadFile(askpass.path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "super-secret") {
		t.Fatal("askpass script contains a secret")
	}
}

func TestGitVirtualRepositoryCheckoutBranchAndDefault(t *testing.T) {
	git := requireGitForVRepoTest(t)
	root := t.TempDir()
	remote, seed := filepath.Join(root, "remote.git"), filepath.Join(root, "seed")
	gitTestRun(t, git, root, "init", "--bare", remote)
	gitTestRun(t, git, root, "clone", remote, seed)
	gitTestRun(t, git, seed, "config", "user.email", "test@example.com")
	gitTestRun(t, git, seed, "config", "user.name", "VRepo Test")
	if err := os.WriteFile(filepath.Join(seed, "main.txt"), []byte("main"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTestRun(t, git, seed, "add", ".")
	gitTestRun(t, git, seed, "commit", "-m", "main")
	gitTestRun(t, git, seed, "push", "origin", "HEAD")
	gitTestRun(t, git, seed, "checkout", "-b", "feature/test")
	if err := os.WriteFile(filepath.Join(seed, "feature.txt"), []byte("feature"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTestRun(t, git, seed, "add", ".")
	gitTestRun(t, git, seed, "commit", "-m", "feature")
	gitTestRun(t, git, seed, "push", "origin", "feature/test")

	repo := &VirtualRepository{Version: 1, ID: "repo", Name: "Repo", RootPath: root, Nodes: []VirtualRepositoryNode{{ID: "feature", Name: "Feature", Repository: &VirtualRepositoryBinding{Kind: "git", RelativePath: "feature", RemoteURL: remote, RefType: "branch", RefName: "feature/test", Enabled: true}}}}
	if err := (&App{}).checkoutVirtualRepositoryNode(context.Background(), repo, "feature"); err != nil {
		t.Fatal(err)
	}
	if got := gitTestRun(t, git, filepath.Join(root, "feature"), "branch", "--show-current"); got != "feature/test" {
		t.Fatalf("branch=%q", got)
	}

	repo.Nodes = []VirtualRepositoryNode{{ID: "default", Name: "Default", Repository: &VirtualRepositoryBinding{Kind: "git", RelativePath: "default", RemoteURL: remote, Enabled: true}}}
	if err := (&App{}).checkoutVirtualRepositoryNode(context.Background(), repo, "default"); err != nil {
		t.Fatal(err)
	}
	if got := gitTestRun(t, git, filepath.Join(root, "default"), "branch", "--show-current"); got == "feature/test" {
		t.Fatalf("blank ref cloned feature branch")
	}
}

func TestInspectGitVirtualRepositoryReportsTagMismatch(t *testing.T) {
	git := requireGitForVRepoTest(t)
	root := t.TempDir()
	remote, work := filepath.Join(root, "remote.git"), filepath.Join(root, "work")
	gitTestRun(t, git, root, "init", "--bare", remote)
	gitTestRun(t, git, root, "clone", remote, work)
	gitTestRun(t, git, work, "config", "user.email", "test@example.com")
	gitTestRun(t, git, work, "config", "user.name", "VRepo Test")
	if err := os.WriteFile(filepath.Join(work, "file.txt"), []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTestRun(t, git, work, "add", ".")
	gitTestRun(t, git, work, "commit", "-m", "one")
	gitTestRun(t, git, work, "tag", "v1")
	if err := os.WriteFile(filepath.Join(work, "file.txt"), []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTestRun(t, git, work, "commit", "-am", "two")
	status := (&App{}).inspectVirtualRepositoryNodeContext(context.Background(), root, VirtualRepositoryNode{
		ID: "tag", Name: "Tag",
		Repository: &VirtualRepositoryBinding{Kind: "git", RelativePath: "work", RemoteURL: remote, RefType: "tag", RefName: "v1", Enabled: true},
	})
	if status.ErrorCode != "ref_mismatch" {
		t.Fatalf("tag mismatch code=%q error=%q", status.ErrorCode, status.Error)
	}
}

func TestInspectEmptyGitVirtualRepositoryUsesUnbornDefaultBranch(t *testing.T) {
	git := requireGitForVRepoTest(t)
	root := t.TempDir()
	remote, work := filepath.Join(root, "remote.git"), filepath.Join(root, "work")
	gitTestRun(t, git, root, "init", "--bare", remote)
	gitTestRun(t, git, root, "clone", remote, work)
	wantBranch := gitTestRun(t, git, work, "symbolic-ref", "--short", "HEAD")
	status := (&App{}).inspectVirtualRepositoryNodeContext(context.Background(), root, VirtualRepositoryNode{
		ID: "empty", Name: "Empty",
		Repository: &VirtualRepositoryBinding{Kind: "git", RelativePath: "work", RemoteURL: remote, Enabled: true},
	})
	if status.ErrorCode != "" || !status.Clean || status.Branch != wantBranch {
		t.Fatalf("empty repository status=%+v, want clean unborn branch %q", status, wantBranch)
	}
}
