package codingruntime

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalGitWorkspaceProberFailsClosedOutsideRepository(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("untracked"), 0o600); err != nil {
		t.Fatal(err)
	}
	prober := NewLocalGitWorkspaceProber(dir)
	if prober == nil {
		t.Fatal("expected prober")
	}
	if _, err := prober.ProbeWorkspace(context.Background(), Task{ProjectRef: dir}, Attempt{}); err == nil {
		t.Fatal("expected non-git workspace probe failure")
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read-only probe unexpectedly created .git: %v", err)
	}
}

func TestLocalGitWorkspaceProberCapturesHeadAndDirtyStatus(t *testing.T) {
	dir := initLocalWorkspaceGitRepo(t)
	clean, err := NewLocalGitWorkspaceProber(dir).ProbeWorkspace(context.Background(), Task{}, Attempt{})
	if err != nil {
		t.Fatal(err)
	}
	if clean.ProjectRef != dir || len(clean.Head) != 40 || !strings.HasPrefix(clean.StatusHash, "sha256:") || clean.ObservedAt.IsZero() {
		t.Fatalf("unexpected clean probe: %#v", clean)
	}
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	probe, err := NewLocalGitWorkspaceProber(dir).ProbeWorkspace(context.Background(), Task{ProjectRef: "logical-project"}, Attempt{})
	if err != nil {
		t.Fatal(err)
	}
	if probe.ProjectRef != "logical-project" || len(probe.Head) != 40 || !strings.HasPrefix(probe.StatusHash, "sha256:") || probe.ObservedAt.IsZero() {
		t.Fatalf("unexpected probe: %#v", probe)
	}
	if clean.Head != probe.Head || clean.StatusHash == probe.StatusHash {
		t.Fatalf("workspace mutation was not reflected in the probe: clean=%#v dirty=%#v", clean, probe)
	}
}

func TestLocalGitWorkspaceProberHonorsCancelledContextWithoutWriting(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewLocalGitWorkspaceProber(dir).ProbeWorkspace(ctx, Task{}, Attempt{})
	if err == nil {
		t.Fatal("expected canceled probe to fail")
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".git")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("cancelled read-only probe unexpectedly created .git: %v", statErr)
	}
}

func TestEnsureLocalGitBaselineInitializesEmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureLocalGitBaseline(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	probe, err := NewLocalGitWorkspaceProber(dir).ProbeWorkspace(context.Background(), Task{ProjectRef: dir}, Attempt{})
	if err != nil {
		t.Fatal(err)
	}
	if len(probe.Head) != 40 || !strings.HasPrefix(probe.StatusHash, "sha256:") {
		t.Fatalf("unexpected probe after baseline init: %#v", probe)
	}
}

func TestEnsureLocalGitBaselineIsIdempotentOnExistingRepository(t *testing.T) {
	dir := initLocalWorkspaceGitRepo(t)
	before, err := NewLocalGitWorkspaceProber(dir).ProbeWorkspace(context.Background(), Task{}, Attempt{})
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureLocalGitBaseline(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	if err := EnsureLocalGitBaseline(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	after, err := NewLocalGitWorkspaceProber(dir).ProbeWorkspace(context.Background(), Task{}, Attempt{})
	if err != nil {
		t.Fatal(err)
	}
	if before.Head != after.Head {
		t.Fatalf("baseline init rewrote an existing HEAD: before=%s after=%s", before.Head, after.Head)
	}
}

func TestEnsureLocalGitBaselineCommitsRepositoryWithoutHead(t *testing.T) {
	dir := t.TempDir()
	runLocalWorkspaceGit(t, dir, "init")
	if err := EnsureLocalGitBaseline(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	if _, err := NewLocalGitWorkspaceProber(dir).ProbeWorkspace(context.Background(), Task{}, Attempt{}); err != nil {
		t.Fatalf("probe after baseline commit: %v", err)
	}
}

func TestEnsureLocalGitBaselineRejectsInvalidPaths(t *testing.T) {
	if err := EnsureLocalGitBaseline(context.Background(), "  "); err == nil {
		t.Fatal("expected empty path to fail")
	}
	if err := EnsureLocalGitBaseline(context.Background(), filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("expected missing path to fail")
	}
	file := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureLocalGitBaseline(context.Background(), file); err == nil {
		t.Fatal("expected non-directory path to fail")
	}
}

func initLocalWorkspaceGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runLocalWorkspaceGit(t, dir, "init")
	runLocalWorkspaceGit(t, dir, "config", "user.email", "runtime-test@example.invalid")
	runLocalWorkspaceGit(t, dir, "config", "user.name", "Coding Runtime Test")
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("initial\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runLocalWorkspaceGit(t, dir, "add", "tracked.txt")
	runLocalWorkspaceGit(t, dir, "commit", "-m", "initial")
	return dir
}

func runLocalWorkspaceGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}
