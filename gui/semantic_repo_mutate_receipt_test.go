package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initTrustedGitWorkspaceWithRemote builds a workspace whose branch tracks a
// local bare repository, so a push and its receipt can be exercised end to end
// without a network.
func initTrustedGitWorkspaceWithRemote(t *testing.T) (work, base string) {
	t.Helper()
	base = t.TempDir()
	work = filepath.Join(base, "work")
	runTrustedTestGit(t, base, "init", "--bare", "remote.git")
	runTrustedTestGit(t, base, "clone", "remote.git", "work")
	runTrustedTestGit(t, work, "config", "user.email", "test@example.com")
	runTrustedTestGit(t, work, "config", "user.name", "Test")
	writeTrustedGitFile(t, work, "note.txt", "one\n")
	runTrustedTestGit(t, work, "add", "note.txt")
	runTrustedTestGit(t, work, "commit", "-m", "first")
	runTrustedTestGit(t, work, "push", "-u", "origin", "HEAD")
	return work, base
}

func writeTrustedGitFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runTrustedTestGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = trustedRepoGitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %s (%v)", strings.Join(args, " "), out, err)
	}
	return string(out)
}

// TestTrustedRepoPushObservesRemoteReceipt is the positive case for the managed
// push. The receipt is the commit read back from the remote, not anything the
// local push command reported, which is what makes a push expressible as a
// managed selection at all: an external effect the host can authoritatively
// observe is no longer an unknown one.
func TestTrustedRepoPushObservesRemoteReceipt(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	work, _ := initTrustedGitWorkspaceWithRemote(t)
	writeTrustedGitFile(t, work, "note.txt", "two\n")
	runTrustedTestGit(t, work, "commit", "-am", "second")

	h := &IMMessageHandler{}
	out, err := h.mutateTrustedRepo(desktopUserID+":"+work, "push", "")
	if err != nil {
		t.Fatalf("push err=%v", err)
	}
	head := strings.TrimSpace(runTrustedTestGit(t, work, "rev-parse", "HEAD"))
	branch := strings.TrimSpace(runTrustedTestGit(t, work, "rev-parse", "--abbrev-ref", "HEAD"))
	if !strings.Contains(out, head) || !strings.Contains(out, "origin/"+branch) {
		t.Fatalf("receipt %q must name the pushed commit %s and origin/%s", out, head, branch)
	}
	if strings.Contains(out, "git_push") {
		t.Fatalf("receipt leaked a legacy tool name: %q", out)
	}

	// The remote holds the commit whether or not this push moved anything, so a
	// repeat stays observable and must not fail.
	repeat, err := h.mutateTrustedRepo(desktopUserID+":"+work, "push", "")
	if err != nil || !strings.Contains(repeat, head) {
		t.Fatalf("re-push receipt=%q err=%v", repeat, err)
	}
}

// TestTrustedRepoPushIsUnknownWhenTheRemoteCannotBeRead covers the case the
// receipt exists for. The push ran and the host then failed to read the remote
// back, so the effect may or may not have landed. That must surface as unknown
// rather than as a failure, because an unknown external effect must not be
// replayed.
func TestTrustedRepoPushIsUnknownWhenTheRemoteCannotBeRead(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	work, base := initTrustedGitWorkspaceWithRemote(t)
	runTrustedTestGit(t, work, "remote", "set-url", "origin", filepath.Join(base, "missing.git"))

	h := &IMMessageHandler{}
	_, err := h.mutateTrustedRepo(desktopUserID+":"+work, "push", "")
	if err == nil || err.Error() != "trusted_repo_mutate_push_receipt_unknown" {
		t.Fatalf("err=%v, want trusted_repo_mutate_push_receipt_unknown", err)
	}
}

// TestTrustedRepoPushClassifiesDefiniteFailures keeps the outcomes the host can
// decide without ambiguity distinct from the unknown one. Reporting any of
// these as unknown would strand the turn, and reporting the unknown one as a
// definite failure would invite a replay of an effect that may have landed.
func TestTrustedRepoPushClassifiesDefiniteFailures(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	h := &IMMessageHandler{}

	t.Run("no upstream", func(t *testing.T) {
		dir := t.TempDir()
		runTrustedTestGit(t, dir, "init")
		runTrustedTestGit(t, dir, "config", "user.email", "test@example.com")
		runTrustedTestGit(t, dir, "config", "user.name", "Test")
		writeTrustedGitFile(t, dir, "note.txt", "one\n")
		runTrustedTestGit(t, dir, "add", "note.txt")
		runTrustedTestGit(t, dir, "commit", "-m", "first")
		_, err := h.mutateTrustedRepo(desktopUserID+":"+dir, "push", "")
		if err == nil || err.Error() != "trusted_repo_mutate_upstream_unset" {
			t.Fatalf("err=%v, want trusted_repo_mutate_upstream_unset", err)
		}
	})

	t.Run("detached head", func(t *testing.T) {
		work, _ := initTrustedGitWorkspaceWithRemote(t)
		runTrustedTestGit(t, work, "checkout", "--detach", "HEAD")
		_, err := h.mutateTrustedRepo(desktopUserID+":"+work, "push", "")
		if err == nil || err.Error() != "trusted_repo_mutate_branch_unresolved" {
			t.Fatalf("err=%v, want trusted_repo_mutate_branch_unresolved", err)
		}
	})

	t.Run("remote moved ahead", func(t *testing.T) {
		work, base := initTrustedGitWorkspaceWithRemote(t)
		other := filepath.Join(base, "other")
		runTrustedTestGit(t, base, "clone", "remote.git", "other")
		runTrustedTestGit(t, other, "config", "user.email", "other@example.com")
		runTrustedTestGit(t, other, "config", "user.name", "Other")
		writeTrustedGitFile(t, other, "note.txt", "theirs\n")
		runTrustedTestGit(t, other, "commit", "-am", "theirs")
		runTrustedTestGit(t, other, "push")

		writeTrustedGitFile(t, work, "note.txt", "ours\n")
		runTrustedTestGit(t, work, "commit", "-am", "ours")
		_, err := h.mutateTrustedRepo(desktopUserID+":"+work, "push", "")
		if err == nil || err.Error() != "trusted_repo_mutate_push_rejected" {
			t.Fatalf("err=%v, want trusted_repo_mutate_push_rejected", err)
		}
	})
}

// TestTrustedRepoCommitObservesHEAD guards the commit half, which now runs
// through the same hardened runner as inspection rather than a bare exec: it
// strips inherited GIT_DIR-style variables, bounds the call, and suppresses the
// console window on Windows.
func TestTrustedRepoCommitObservesHEAD(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	dir := t.TempDir()
	runTrustedTestGit(t, dir, "init")
	runTrustedTestGit(t, dir, "config", "user.email", "test@example.com")
	runTrustedTestGit(t, dir, "config", "user.name", "Test")
	writeTrustedGitFile(t, dir, "note.txt", "one\n")
	runTrustedTestGit(t, dir, "add", "note.txt")

	h := &IMMessageHandler{}
	out, err := h.mutateTrustedRepo(desktopUserID+":"+dir, "commit", "save note")
	if err != nil || !strings.HasPrefix(out, "commit ") || strings.Contains(out, "git_commit") {
		t.Fatalf("commit=%q err=%v", out, err)
	}
	head := strings.TrimSpace(runTrustedTestGit(t, dir, "rev-parse", "HEAD"))
	if !strings.Contains(out, head) {
		t.Fatalf("commit must observe HEAD %q, got %q", head, out)
	}
}

// TestTrustedRepoCommitStagesTrackedEditsOnly pins what "commit" means when the
// only input is a message.
//
// Staging has to happen inside the adapter, because a caller that can pass
// nothing but a message has no way to stage first, and a commit that refused
// every unstaged edit would be unusable. It stops at tracked files: sweeping in
// untracked ones would commit content nobody named.
func TestTrustedRepoCommitStagesTrackedEditsOnly(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	work, _ := initTrustedGitWorkspaceWithRemote(t)
	before := strings.TrimSpace(runTrustedTestGit(t, work, "rev-parse", "HEAD"))
	writeTrustedGitFile(t, work, "note.txt", "edited\n")
	writeTrustedGitFile(t, work, "stray.txt", "unnamed\n")

	h := &IMMessageHandler{}
	out, err := h.mutateTrustedRepo(desktopUserID+":"+work, "commit", "edit note")
	if err != nil {
		t.Fatalf("commit err=%v", err)
	}
	head := strings.TrimSpace(runTrustedTestGit(t, work, "rev-parse", "HEAD"))
	if head == before || !strings.Contains(out, head) {
		t.Fatalf("receipt %q must name a moved HEAD (was %s, now %s)", out, before, head)
	}
	committed := runTrustedTestGit(t, work, "show", "--name-only", "--pretty=format:", "HEAD")
	if !strings.Contains(committed, "note.txt") {
		t.Fatalf("tracked edit was not committed: %q", committed)
	}
	if strings.Contains(committed, "stray.txt") {
		t.Fatalf("untracked file was swept into the commit: %q", committed)
	}
	if !strings.Contains(runTrustedTestGit(t, work, "status", "--porcelain"), "?? stray.txt") {
		t.Fatal("untracked file must still be untracked after the commit")
	}
}

// TestTrustedRepoCommitRefusesWhenNothingIsStaged keeps an empty commit from
// being reported as a mutation. The check reads the index rather than git's
// refusal text, which is localised.
func TestTrustedRepoCommitRefusesWhenNothingIsStaged(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	work, _ := initTrustedGitWorkspaceWithRemote(t)
	before := strings.TrimSpace(runTrustedTestGit(t, work, "rev-parse", "HEAD"))

	h := &IMMessageHandler{}
	_, err := h.mutateTrustedRepo(desktopUserID+":"+work, "commit", "nothing changed")
	if err == nil || err.Error() != "trusted_repo_mutate_nothing_to_commit" {
		t.Fatalf("err=%v, want trusted_repo_mutate_nothing_to_commit", err)
	}
	if head := strings.TrimSpace(runTrustedTestGit(t, work, "rev-parse", "HEAD")); head != before {
		t.Fatalf("HEAD moved on a refused commit: %s -> %s", before, head)
	}
}

// TestTrustedRepoUpstreamSplitKeepsBranchesWithSlashes pins the parse that
// decides which remote the receipt is read from. Git forbids `/` in a remote
// name, so the first separator is the boundary; splitting on the last one would
// read the receipt from the wrong remote for any `feature/x` branch.
func TestTrustedRepoUpstreamSplitKeepsBranchesWithSlashes(t *testing.T) {
	for _, tc := range []struct {
		upstream, remote, branch string
		ok                       bool
	}{
		{"origin/main", "origin", "main", true},
		{"upstream/feature/nested/x", "upstream", "feature/nested/x", true},
		{"", "", "", false},
		{"origin", "", "", false},
		{"origin/", "", "", false},
		{"/main", "", "", false},
	} {
		remote, branch, ok := splitTrustedRepoUpstream(tc.upstream)
		if ok != tc.ok || remote != tc.remote || branch != tc.branch {
			t.Errorf("split(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tc.upstream, remote, branch, ok, tc.remote, tc.branch, tc.ok)
		}
	}
}

// TestTrustedRepoRemoteEnvRefusesInteractivePrompts keeps a network git call
// from blocking on a credential prompt. Without this a push against a
// repository the host cannot authenticate to would hold the turn open until its
// timeout instead of failing immediately.
func TestTrustedRepoRemoteEnvRefusesInteractivePrompts(t *testing.T) {
	t.Setenv("GIT_TERMINAL_PROMPT", "1")
	var prompt, ssh int
	for _, entry := range trustedRepoGitRemoteEnv() {
		switch {
		case entry == "GIT_TERMINAL_PROMPT=0":
			prompt++
		case strings.HasPrefix(entry, "GIT_TERMINAL_PROMPT="):
			t.Errorf("inherited prompt setting survived: %q", entry)
		case strings.HasPrefix(entry, "GIT_SSH_COMMAND="):
			ssh++
			if !strings.Contains(entry, "BatchMode=yes") {
				t.Errorf("ssh command must not prompt: %q", entry)
			}
		}
	}
	if prompt != 1 || ssh != 1 {
		t.Fatalf("want exactly one prompt and ssh setting, got prompt=%d ssh=%d", prompt, ssh)
	}
}
