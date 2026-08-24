package agentservice

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

type fakeHostRepoMutator struct {
	action    string
	message   string
	principal Principal
	result    string
	err       error
}

func (f *fakeHostRepoMutator) MutateReviewedHostRepo(_ context.Context, principal Principal, action, message string) (string, error) {
	f.principal = principal
	f.action = action
	f.message = message
	return f.result, f.err
}

func TestReviewedHostRepoMutateExecutesCommitWithoutCoordinatorAndRejectsSoup(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	mutator := &fakeHostRepoMutator{result: "commit abcdef"}
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{RepoMutate: mutator})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "mutate", Capability: CapabilityRepoMutate, Required: true}},
	})
	if err != nil || len(plan.Selections) != 1 || len(plan.Unmet) != 0 {
		t.Fatalf("repo mutate plan=%#v err=%v", plan, err)
	}
	if plan.Selections[0].AdapterName == "git_commit" || plan.Selections[0].AdapterName == "git_push" {
		t.Fatalf("selection=%#v", plan.Selections[0])
	}
	if dynamicHostLocalMutationSelection(plan.Selections[0]) || dynamicSelectionRequiresReceipt(plan.Selections[0]) || !dynamicHostObservedExternalSelection(plan.Selections[0]) {
		t.Fatalf("host repo mutate is observed external, selection=%#v", plan.Selections[0])
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	result := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"action":"commit","message":"save work"}`)
	if !result.Succeeded || result.Unknown || result.Result != mutator.result {
		t.Fatalf("commit result=%#v", result)
	}
	if mutator.action != "commit" || mutator.message != "save work" {
		t.Fatalf("mutator=%#v", mutator)
	}
	rejected := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"action":"commit","message":"save","project_path":"/tmp"}`)
	if rejected.Succeeded || rejected.Unknown {
		t.Fatalf("project_path soup must fail closed, result=%#v", rejected)
	}
}

// TestReviewedHostRepoMutatePushIsUnknownWhenTheRemoteCannotBeRead covers the
// case the receipt exists for: the push ran, and the host then failed to read
// the remote back. The effect may or may not have landed, so the selection must
// surface as unknown rather than as a failure that something could retry.
func TestReviewedHostRepoMutatePushIsUnknownWhenTheRemoteCannotBeRead(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	work, base := initReviewedGitWorkspaceWithRemote(t)
	// Point the tracked remote at a path that does not exist. Both the push and
	// the receipt read fail, which is exactly an unobservable outcome.
	runGit(t, work, "remote", "set-url", "origin", filepath.Join(base, "missing.git"))
	cb := &coreAgentCallbacks{principal: principal, workspace: work}
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{RepoMutate: cb})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "mutate", Capability: CapabilityRepoMutate, Required: true}},
	})
	if err != nil || len(plan.Selections) != 1 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	unknown := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"action":"push"}`)
	if !unknown.Unknown || unknown.Succeeded || unknown.ReasonCode != "host_repo_mutate_push_receipt_unknown" {
		t.Fatalf("push without remote receipt must be unknown, result=%#v", unknown)
	}
}

func TestReviewedHostRepoMutateCommitObservesHEAD(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "note.txt")
	principal := Principal{TenantID: "tenant", UserID: "user"}
	cb := &coreAgentCallbacks{principal: principal, workspace: dir}
	out, err := cb.MutateReviewedHostRepo(context.Background(), principal, "commit", "save note")
	if err != nil || !strings.HasPrefix(out, "commit ") || strings.Contains(out, "git_commit") {
		t.Fatalf("commit=%q err=%v", out, err)
	}
	head := strings.TrimSpace(runGit(t, dir, "rev-parse", "HEAD"))
	if !strings.Contains(out, head) {
		t.Fatalf("commit must observe HEAD %q, got %q", head, out)
	}
}

func TestReviewedHostRepoMutateIsAbsentWithoutWorkspace(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "mutate", Capability: CapabilityRepoMutate, Required: true}},
	})
	if err != nil || len(plan.Selections) != 0 {
		t.Fatalf("repo mutate without workspace must stay unmet, plan=%#v err=%v", plan, err)
	}
	child := (&coreAgentCallbacks{
		principal: Principal{TenantID: "t", UserID: "u"}, workspace: t.TempDir(), runtimeReadOnlyChild: true,
	}).reviewedHostOwnedServices()
	if child.RepoMutate != nil {
		t.Fatal("read-only child must not republish repo mutate")
	}
}

func TestProjectReviewedHostRepoMutateRejectsPathFields(t *testing.T) {
	provider, definition, _, err := ProjectReviewedHostRepoMutateProvider(&fakeHostRepoMutator{})
	if err != nil {
		t.Fatal(err)
	}
	if provider.Provides[0].Capability != CapabilityRepoMutate || provider.AdapterName == "git_commit" {
		t.Fatalf("provider=%#v", provider)
	}
	fn, _ := definition["function"].(map[string]interface{})
	params, _ := fn["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	if _, ok := props["action"]; !ok || len(props) != 2 {
		t.Fatalf("repo mutate schema=%#v", props)
	}
	for _, key := range []string{"project_path", "path", "channel", "destination", "remote"} {
		if _, ok := props[key]; ok {
			t.Fatalf("repo mutate schema leaked %s", key)
		}
	}
}

// initReviewedGitWorkspaceWithRemote builds a workspace whose branch tracks a
// local bare repository, so a push and its receipt can be exercised end to end
// without a network.
func initReviewedGitWorkspaceWithRemote(t *testing.T) (work, base string) {
	t.Helper()
	base = t.TempDir()
	work = filepath.Join(base, "work")
	runGit(t, base, "init", "--bare", "remote.git")
	runGit(t, base, "clone", "remote.git", "work")
	runGit(t, work, "config", "user.email", "test@example.com")
	runGit(t, work, "config", "user.name", "Test")
	writeReviewedGitFile(t, work, "note.txt", "one\n")
	runGit(t, work, "add", "note.txt")
	runGit(t, work, "commit", "-m", "first")
	runGit(t, work, "push", "-u", "origin", "HEAD")
	return work, base
}

func writeReviewedGitFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestReviewedHostRepoMutateCommitStagesTrackedEditsOnly pins what "commit"
// means when the only input is a message.
//
// Staging has to happen inside the adapter, because a caller that can pass
// nothing but a message has no way to stage first, and a commit that refused
// every unstaged edit would be unusable. It stops at tracked files: sweeping in
// untracked ones would commit content nobody named.
func TestReviewedHostRepoMutateCommitStagesTrackedEditsOnly(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	work, _ := initReviewedGitWorkspaceWithRemote(t)
	before := strings.TrimSpace(runGit(t, work, "rev-parse", "HEAD"))
	writeReviewedGitFile(t, work, "note.txt", "edited\n")
	writeReviewedGitFile(t, work, "stray.txt", "unnamed\n")

	principal := Principal{TenantID: "tenant", UserID: "user"}
	cb := &coreAgentCallbacks{principal: principal, workspace: work}
	out, err := cb.MutateReviewedHostRepo(context.Background(), principal, "commit", "edit note")
	if err != nil {
		t.Fatalf("commit err=%v", err)
	}
	head := strings.TrimSpace(runGit(t, work, "rev-parse", "HEAD"))
	if head == before || !strings.Contains(out, head) {
		t.Fatalf("receipt %q must name a moved HEAD (was %s, now %s)", out, before, head)
	}
	committed := runGit(t, work, "show", "--name-only", "--pretty=format:", "HEAD")
	if !strings.Contains(committed, "note.txt") {
		t.Fatalf("tracked edit was not committed: %q", committed)
	}
	if strings.Contains(committed, "stray.txt") {
		t.Fatalf("untracked file was swept into the commit: %q", committed)
	}
	if !strings.Contains(runGit(t, work, "status", "--porcelain"), "?? stray.txt") {
		t.Fatal("untracked file must still be untracked after the commit")
	}
}

// TestReviewedHostRepoMutateCommitRefusesWhenNothingIsStaged keeps an empty
// commit from being reported as a mutation. The check reads the index rather
// than git's refusal text, which is localised.
func TestReviewedHostRepoMutateCommitRefusesWhenNothingIsStaged(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	work, _ := initReviewedGitWorkspaceWithRemote(t)
	before := strings.TrimSpace(runGit(t, work, "rev-parse", "HEAD"))

	principal := Principal{TenantID: "tenant", UserID: "user"}
	cb := &coreAgentCallbacks{principal: principal, workspace: work}
	_, err := cb.MutateReviewedHostRepo(context.Background(), principal, "commit", "nothing changed")
	if err == nil || err.Error() != "host_repo_mutate_nothing_to_commit" {
		t.Fatalf("err=%v, want host_repo_mutate_nothing_to_commit", err)
	}
	if head := strings.TrimSpace(runGit(t, work, "rev-parse", "HEAD")); head != before {
		t.Fatalf("HEAD moved on a refused commit: %s -> %s", before, head)
	}
}

// TestReviewedHostRepoMutatePushObservesRemoteReceipt is the positive case: the
// receipt is the commit read back from the remote, not anything the local push
// command reported.
func TestReviewedHostRepoMutatePushObservesRemoteReceipt(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	work, _ := initReviewedGitWorkspaceWithRemote(t)
	writeReviewedGitFile(t, work, "note.txt", "two\n")
	runGit(t, work, "commit", "-am", "second")

	principal := Principal{TenantID: "tenant", UserID: "user"}
	cb := &coreAgentCallbacks{principal: principal, workspace: work}
	out, err := cb.MutateReviewedHostRepo(context.Background(), principal, "push", "")
	if err != nil {
		t.Fatalf("push err=%v", err)
	}
	head := strings.TrimSpace(runGit(t, work, "rev-parse", "HEAD"))
	branch := strings.TrimSpace(runGit(t, work, "rev-parse", "--abbrev-ref", "HEAD"))
	if !strings.Contains(out, head) || !strings.Contains(out, "origin/"+branch) {
		t.Fatalf("receipt %q must name the pushed commit %s and origin/%s", out, head, branch)
	}
	if strings.Contains(out, "git_push") {
		t.Fatalf("receipt leaked a legacy tool name: %q", out)
	}

	// Pushing again moves nothing, but the remote still holds the commit, so
	// the receipt is observable and the selection must not fail.
	repeat, err := cb.MutateReviewedHostRepo(context.Background(), principal, "push", "")
	if err != nil || !strings.Contains(repeat, head) {
		t.Fatalf("re-push receipt=%q err=%v", repeat, err)
	}
}

// TestReviewedHostRepoMutatePushClassifiesDefiniteFailures keeps the three
// outcomes the host can decide without ambiguity distinct from the unknown one.
// Reporting any of these as unknown would strand the turn; reporting the
// unknown one as a failure would invite a replay of an effect that may have
// landed.
func TestReviewedHostRepoMutatePushClassifiesDefiniteFailures(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}

	t.Run("no upstream", func(t *testing.T) {
		dir := t.TempDir()
		runGit(t, dir, "init")
		runGit(t, dir, "config", "user.email", "test@example.com")
		runGit(t, dir, "config", "user.name", "Test")
		writeReviewedGitFile(t, dir, "note.txt", "one\n")
		runGit(t, dir, "add", "note.txt")
		runGit(t, dir, "commit", "-m", "first")
		cb := &coreAgentCallbacks{principal: principal, workspace: dir}
		_, err := cb.MutateReviewedHostRepo(context.Background(), principal, "push", "")
		if err == nil || err.Error() != "host_repo_mutate_upstream_unset" {
			t.Fatalf("err=%v, want host_repo_mutate_upstream_unset", err)
		}
	})

	t.Run("detached head", func(t *testing.T) {
		work, _ := initReviewedGitWorkspaceWithRemote(t)
		runGit(t, work, "checkout", "--detach", "HEAD")
		cb := &coreAgentCallbacks{principal: principal, workspace: work}
		_, err := cb.MutateReviewedHostRepo(context.Background(), principal, "push", "")
		if err == nil || err.Error() != "host_repo_mutate_branch_unresolved" {
			t.Fatalf("err=%v, want host_repo_mutate_branch_unresolved", err)
		}
	})

	t.Run("remote moved ahead", func(t *testing.T) {
		work, base := initReviewedGitWorkspaceWithRemote(t)
		other := filepath.Join(base, "other")
		runGit(t, base, "clone", "remote.git", "other")
		runGit(t, other, "config", "user.email", "other@example.com")
		runGit(t, other, "config", "user.name", "Other")
		writeReviewedGitFile(t, other, "note.txt", "theirs\n")
		runGit(t, other, "commit", "-am", "theirs")
		runGit(t, other, "push")

		writeReviewedGitFile(t, work, "note.txt", "ours\n")
		runGit(t, work, "commit", "-am", "ours")
		cb := &coreAgentCallbacks{principal: principal, workspace: work}
		_, err := cb.MutateReviewedHostRepo(context.Background(), principal, "push", "")
		if err == nil || err.Error() != "host_repo_mutate_push_rejected" {
			t.Fatalf("err=%v, want host_repo_mutate_push_rejected", err)
		}
	})
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = reviewedHostGitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %s (%v)", strings.Join(args, " "), out, err)
	}
	return string(out)
}
