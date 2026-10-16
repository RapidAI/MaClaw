package codingruntime

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestNormalizeWriteSetFailsClosedForUnknownAndEscapes(t *testing.T) {
	scope := WriteScope{Mode: "local", ProjectRef: filepath.Join("D:", "repo")}
	unknown, err := NormalizeWriteSet(scope, nil)
	if err != nil || !unknown.Unknown {
		t.Fatalf("unknown=%+v err=%v", unknown, err)
	}
	if _, err := NormalizeWriteSet(scope, []string{"../outside.go"}); err == nil {
		t.Fatal("root escape was accepted")
	}
	if _, err := NormalizeWriteSet(WriteScope{Mode: "remote", ProjectRef: "/srv/repo"}, []string{"a.go"}); err == nil {
		t.Fatal("remote scope without stable target was accepted")
	}
	for _, path := range []string{"*.go", "internal/[ab].go", "~/outside.go", "${REPO}/a.go", "$(pwd)/a.go"} {
		if _, err := NormalizeWriteSet(scope, []string{path}); err == nil {
			t.Fatalf("unsafe declaration accepted: %q", path)
		}
	}
}

func TestNormalizeWriteSetAcceptsExplicitLocalRootDirectoryClaim(t *testing.T) {
	root := filepath.Join("D:\\", "repo")
	scope := WriteScope{Mode: "local", ProjectRef: root}
	set, err := NormalizeWriteSet(scope, []string{root + string(filepath.Separator)})
	if err != nil {
		t.Fatal(err)
	}
	if set.Unknown || len(set.Claims) != 1 || !set.Claims[0].Directory || set.Claims[0].Path != "." {
		t.Fatalf("root directory claim=%+v", set)
	}
	child, err := NormalizeWriteSet(scope, []string{"src/main.go"})
	if err != nil {
		t.Fatal(err)
	}
	if got := set.ConflictsWith(child); !got.Conflicts {
		t.Fatalf("root directory claim did not cover child: %+v", got)
	}
	relative, err := NormalizeWriteSet(WriteScope{Mode: "local", ProjectRef: "repo"}, []string{"." + string(filepath.Separator)})
	if err != nil || relative.Unknown || len(relative.Claims) != 1 || relative.Claims[0].Path != "." || !relative.Claims[0].Directory {
		t.Fatalf("relative root directory claim=%+v err=%v", relative, err)
	}
}

func TestNormalizeRemoteWriteSetUsesPOSIXPathsOnWindowsHost(t *testing.T) {
	scope := WriteScope{Mode: "remote", ProjectRef: "/srv/repo/", RemoteTarget: "sha256:host"}
	set, err := NormalizeWriteSet(scope, []string{"/srv/repo/src/main.go", "docs/README.md"})
	if err != nil {
		t.Fatal(err)
	}
	if got := set.Scope.ProjectRef; got != "/srv/repo" {
		t.Fatalf("project ref=%q, want canonical POSIX root", got)
	}
	if len(set.Claims) != 2 || set.Claims[0].Path != "docs/README.md" || set.Claims[1].Path != "src/main.go" {
		t.Fatalf("normalized remote claims=%+v", set.Claims)
	}
}

func TestNormalizeRemoteWriteSetRejectsRootEscapesAndWindowsSeparators(t *testing.T) {
	scope := WriteScope{Mode: "remote", ProjectRef: "/srv/repo", RemoteTarget: "sha256:host"}
	for _, declaration := range []string{
		"/etc/passwd",
		"/srv/repo2/other.go",
		"/srv/repo/../etc/passwd",
		"/srv/repo/./src/main.go",
		"../outside.go",
		"src/../../outside.go",
		"src/../main.go",
		"src\t/main.go",
		`\\srv\\repo\\main.go`,
	} {
		if _, err := NormalizeWriteSet(scope, []string{declaration}); err == nil {
			t.Fatalf("remote escape/separator declaration accepted: %q", declaration)
		}
	}
}

func TestNormalizeRemoteWriteSetRejectsUnsafeProjectReference(t *testing.T) {
	for _, project := range []string{
		"/srv/repo/../other",
		"/srv/repo/./nested",
		`/srv\repo`,
		"/srv/repo\x00",
		"relative/repo",
		"/",
	} {
		if _, err := NormalizeWriteSet(WriteScope{Mode: "remote", ProjectRef: project, RemoteTarget: "sha256:host"}, []string{"main.go"}); err == nil {
			t.Fatalf("unsafe remote project reference accepted: %q", project)
		}
	}
}

func TestRemoteWriteSetPreservesCaseSensitiveClaims(t *testing.T) {
	scope := WriteScope{Mode: "remote", ProjectRef: "/srv/repo", RemoteTarget: "sha256:host"}
	left, err := NormalizeWriteSet(scope, []string{"Foo.go"})
	if err != nil {
		t.Fatal(err)
	}
	right, err := NormalizeWriteSet(scope, []string{"foo.go"})
	if err != nil {
		t.Fatal(err)
	}
	if got := left.ConflictsWith(right); got.Conflicts {
		t.Fatalf("case-distinct remote claims conflicted: %+v", got)
	}
	localScope := WriteScope{Mode: "local", ProjectRef: filepath.Join("D:", "repo")}
	localLeft, _ := NormalizeWriteSet(localScope, []string{"Foo.go"})
	localRight, _ := NormalizeWriteSet(localScope, []string{"foo.go"})
	if got := localLeft.ConflictsWith(localRight); !got.Conflicts {
		t.Fatalf("case-distinct local claims did not conflict: %+v", got)
	}
}

func TestWriteSetConflictDetectsDirectoryAndUnknownClaims(t *testing.T) {
	scope := WriteScope{Mode: "local", ProjectRef: filepath.Join("D:", "repo")}
	left, err := NormalizeWriteSet(scope, []string{"internal/auth/"})
	if err != nil {
		t.Fatal(err)
	}
	right, err := NormalizeWriteSet(scope, []string{"internal/auth/token.go"})
	if err != nil {
		t.Fatal(err)
	}
	if got := left.ConflictsWith(right); !got.Conflicts || got.Reason != "overlapping write claim" {
		t.Fatalf("conflict=%+v", got)
	}
	other, err := NormalizeWriteSet(scope, []string{"cmd/main.go"})
	if err != nil {
		t.Fatal(err)
	}
	if got := right.ConflictsWith(other); got.Conflicts {
		t.Fatalf("distinct file claims conflicted: %+v", got)
	}
	unknown, _ := NormalizeWriteSet(scope, nil)
	if got := unknown.ConflictsWith(other); !got.Conflicts || got.Reason != "unknown write set" {
		t.Fatalf("unknown conflict=%+v", got)
	}
}

func TestWriteSetConflictSeparatesRemoteTargets(t *testing.T) {
	left, err := NormalizeWriteSet(WriteScope{Mode: "remote", ProjectRef: "/srv/repo", RemoteTarget: "sha256:host-a"}, []string{"a.go"})
	if err != nil {
		t.Fatal(err)
	}
	right, err := NormalizeWriteSet(WriteScope{Mode: "remote", ProjectRef: "/srv/repo", RemoteTarget: "sha256:host-b"}, []string{"a.go"})
	if err != nil {
		t.Fatal(err)
	}
	if got := left.ConflictsWith(right); got.Conflicts {
		t.Fatalf("different remote target conflicted: %+v", got)
	}
}

func TestCanAdmitParallelWritersRequiresIsolationAndFinalDiffGate(t *testing.T) {
	scope := WriteScope{Mode: "local", ProjectRef: filepath.Join("D:", "repo")}
	left, _ := NormalizeWriteSet(scope, []string{"a.go"})
	right, _ := NormalizeWriteSet(scope, []string{"b.go"})
	if got := CanAdmitParallelWriters(left, right, false, true, true); !got.Conflicts || got.Reason != "isolated workspace required" {
		t.Fatalf("missing isolation=%+v", got)
	}
	if got := CanAdmitParallelWriters(left, right, true, true, false); !got.Conflicts || got.Reason != "final diff gate required" {
		t.Fatalf("missing diff gate=%+v", got)
	}
	if got := CanAdmitParallelWriters(left, right, true, true, true); got.Conflicts {
		t.Fatalf("fully guarded disjoint writers rejected: %+v", got)
	}
}

func TestNormalizeWriterPolicyDefaultsUndeclaredWritersToUnknown(t *testing.T) {
	policy, err := NormalizeWriterPolicy(Task{ProjectRef: filepath.Join("D:", "repo"), Mode: "local"}, PolicySnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if !policy.WriteSet.Unknown || policy.WriteSet.Scope.ProjectRef == "" {
		t.Fatalf("policy=%+v", policy)
	}
}

func TestNormalizeWriterPolicyRejectsIsolatedWriterWithoutFinalGate(t *testing.T) {
	_, err := NormalizeWriterPolicy(Task{ProjectRef: filepath.Join("D:", "repo"), Mode: "local"}, PolicySnapshot{WorkspaceIsolated: true})
	if err == nil {
		t.Fatal("isolated writer without final diff gate was accepted")
	}
}

func TestNormalizeWriterPolicyRejectsFinalDiffGateWithoutIsolation(t *testing.T) {
	_, err := NormalizeWriterPolicy(Task{ProjectRef: filepath.Join("D:", "repo"), Mode: "local"}, PolicySnapshot{FinalDiffGateRequired: true})
	if err == nil {
		t.Fatal("final diff gate without isolated workspace was accepted")
	}
}

func TestNormalizeWriterPolicyRejectsUnknownIsolatedWriter(t *testing.T) {
	_, err := NormalizeWriterPolicy(Task{ProjectRef: filepath.Join("D:", "repo"), Mode: "local"}, PolicySnapshot{
		WorkspaceIsolated:     true,
		FinalDiffGateRequired: true,
		WriteSet:              WriteSet{Unknown: true},
	})
	if err == nil {
		t.Fatal("isolated writer with unknown write set was accepted")
	}
}

func TestNormalizeWriterPolicyRejectsGatedWriterWithoutWorkspaceIdentity(t *testing.T) {
	if _, err := NormalizeWriterPolicy(Task{Mode: "local"}, PolicySnapshot{FinalWorkspaceGateRequired: true}); err == nil {
		t.Fatal("gated writer without project reference was accepted")
	}
	if _, err := NormalizeWriterPolicy(Task{ProjectRef: "/srv/repo", Mode: "remote"}, PolicySnapshot{FinalWorkspaceGateRequired: true}); err == nil {
		t.Fatal("gated remote writer without stable target was accepted")
	}
}

func TestNormalizeWriterPolicyDoesNotDropDeclaredClaimsWhenScopeIsIncomplete(t *testing.T) {
	if _, err := NormalizeWriterPolicy(Task{Mode: "local"}, PolicySnapshot{WriteSet: WriteSet{Claims: []WriteClaim{{Path: "main.go"}}}}); err == nil {
		t.Fatal("declared claim without project reference was silently converted to unknown")
	}
	if _, err := NormalizeWriterPolicy(Task{ProjectRef: "/srv/repo", Mode: "remote"}, PolicySnapshot{Mode: "remote", WriteSet: WriteSet{Claims: []WriteClaim{{Path: "main.go"}}}}); err == nil {
		t.Fatal("declared remote claim without stable target was silently dropped")
	}
}

func TestPolicyDigestCoversWriteScopeAndIsolation(t *testing.T) {
	base := PolicySnapshot{ProjectRoot: filepath.Join("D:", "repo"), Mode: "local", WriteSet: WriteSet{Claims: []WriteClaim{{Path: "a.go"}}}}
	first, err := PolicyDigest(base)
	if err != nil {
		t.Fatal(err)
	}
	changedScope := base
	changedScope.WriteSet.Claims = []WriteClaim{{Path: "b.go"}}
	second, err := PolicyDigest(changedScope)
	if err != nil || first == second {
		t.Fatalf("scope digest first=%q second=%q err=%v", first, second, err)
	}
	changedIsolation := base
	changedIsolation.WorkspaceIsolated, changedIsolation.FinalDiffGateRequired = true, true
	third, err := PolicyDigest(changedIsolation)
	if err != nil || first == third {
		t.Fatalf("isolation digest first=%q third=%q err=%v", first, third, err)
	}
}

func TestMemoryStoreWriterAdmissionSerializesUnknownAndAllowsGuardedDisjoint(t *testing.T) {
	store := NewMemoryStore()
	now := time.Date(2026, 8, 10, 1, 2, 3, 0, time.UTC)
	firstTask, err := store.CreateTask(Task{ProjectRef: filepath.Join("D:", "repo"), Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	secondTask, err := store.CreateTask(Task{ProjectRef: filepath.Join("D:", "repo"), Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	unknown, err := store.StartAttempt(firstTask.TaskID, "one", time.Minute, PolicySnapshot{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartAttempt(secondTask.TaskID, "two", time.Minute, PolicySnapshot{}, now); !errors.Is(err, ErrWriterConflict) {
		t.Fatalf("unknown writer err=%v, want writer conflict", err)
	}
	if _, err := store.FinishAttempt(unknown.AttemptID, "one", FinishInput{Status: TaskCompleted}, now); err != nil {
		t.Fatal(err)
	}

	leftTask, err := store.CreateTask(Task{ProjectRef: filepath.Join("D:", "repo2"), Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	rightTask, err := store.CreateTask(Task{ProjectRef: filepath.Join("D:", "repo2"), Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	leftPolicy := PolicySnapshot{WorkspaceIsolated: true, FinalDiffGateRequired: true, WriteSet: WriteSet{Claims: []WriteClaim{{Path: "a.go"}}}}
	rightPolicy := PolicySnapshot{WorkspaceIsolated: true, FinalDiffGateRequired: true, WriteSet: WriteSet{Claims: []WriteClaim{{Path: "b.go"}}}}
	if _, err := store.StartAttempt(leftTask.TaskID, "three", time.Minute, leftPolicy, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartAttempt(rightTask.TaskID, "four", time.Minute, rightPolicy, now); err != nil {
		t.Fatalf("guarded disjoint writer err=%v", err)
	}
}

func TestMemoryStoreWriterAdmissionSeparatesDistinctRemoteTargets(t *testing.T) {
	store := NewMemoryStore()
	now := time.Date(2026, 8, 10, 1, 2, 3, 0, time.UTC)
	first, err := store.CreateTask(Task{ProjectRef: "/srv/app", Mode: "remote"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateTask(Task{ProjectRef: "/srv/app", Mode: "remote"})
	if err != nil {
		t.Fatal(err)
	}
	policy := func(target string) PolicySnapshot {
		return PolicySnapshot{ProjectRoot: "/srv/app", Mode: "remote", RemoteTarget: target, WorkspaceIsolated: true, FinalDiffGateRequired: true, WriteSet: WriteSet{Claims: []WriteClaim{{Path: "main.go"}}}}
	}
	if _, err := store.StartAttempt(first.TaskID, "one", time.Minute, policy("sha256:host-a"), now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartAttempt(second.TaskID, "two", time.Minute, policy("sha256:host-b"), now); err != nil {
		t.Fatalf("distinct remote targets must not share a write lock: %v", err)
	}
}

func TestMemoryStoreWriterAdmissionSerializesSameRemoteTargetEvenWithDisjointClaimsWithoutIsolation(t *testing.T) {
	store := NewMemoryStore()
	now := time.Date(2026, 8, 10, 1, 2, 3, 0, time.UTC)
	first, err := store.CreateTask(Task{ProjectRef: "/srv/app", Mode: "remote"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateTask(Task{ProjectRef: "/srv/app", Mode: "remote"})
	if err != nil {
		t.Fatal(err)
	}
	base := PolicySnapshot{ProjectRoot: "/srv/app", Mode: "remote", RemoteTarget: "sha256:host-a", WriteSet: WriteSet{Claims: []WriteClaim{{Path: "a.go"}}}}
	if _, err := store.StartAttempt(first.TaskID, "one", time.Minute, base, now); err != nil {
		t.Fatal(err)
	}
	base.WriteSet.Claims = []WriteClaim{{Path: "b.go"}}
	if _, err := store.StartAttempt(second.TaskID, "two", time.Minute, base, now); !errors.Is(err, ErrWriterConflict) {
		t.Fatalf("unisolated disjoint remote writers err=%v, want writer conflict", err)
	}
}

type finalGateExecutor struct{ passed bool }

func (e finalGateExecutor) Execute(_ context.Context, _ ExecutionRequest) ExecutionResult {
	return ExecutionResult{Status: TaskCompleted, SideEffectState: SideEffectConfirmed, FinalDiffGatePassed: e.passed}
}

func TestRunnerBlocksIsolatedWriterWithoutFinalDiffGateResult(t *testing.T) {
	store := NewMemoryStore()
	policy := PolicySnapshot{
		ProjectRoot:           filepath.Join("D:", "repo"),
		Mode:                  "local",
		WorkspaceIsolated:     true,
		FinalDiffGateRequired: true,
		WriteSet:              WriteSet{Claims: []WriteClaim{{Path: "a.go"}}},
	}
	task, attempt, err := (Runner{Store: store, LeaseOwner: "worker"}).Run(context.Background(), Task{ProjectRef: policy.ProjectRoot, Mode: "local"}, policy, finalGateExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != TaskBlocked || attempt.Status != TaskBlocked || attempt.ErrorCode != "final_diff_gate_missing" {
		t.Fatalf("task=%+v attempt=%+v", task, attempt)
	}
}
