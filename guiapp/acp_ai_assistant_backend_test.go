package guiapp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
)

func TestAcpProgrammingUserText(t *testing.T) {
	got := acpProgrammingUserText(`D:\work\demo`, "add README")
	if got == "add README" {
		t.Fatal("expected workspace wrapper")
	}
	for _, part := range []string{`D:\work\demo`, "add README", "VS Code", "disk"} {
		if !strings.Contains(got, part) {
			t.Fatalf("missing %q in %q", part, got)
		}
	}
	if acpProgrammingUserText("", "hi") != "hi" {
		t.Fatal("empty cwd should pass through")
	}
	wrapped := acpProgrammingUserText(`F:\个人介绍`, "继续")
	if inner := acpInnerUserRequest(wrapped); inner != "继续" {
		t.Fatalf("acpInnerUserRequest=%q, want 继续", inner)
	}
	if acpInnerUserRequest("请继续") != "请继续" {
		t.Fatal("plain text must pass through")
	}
	nested := acpProgrammingUserText(`D:\work\demo`, "see User request: keep this")
	if inner := acpInnerUserRequest(nested); inner != "see User request: keep this" {
		t.Fatalf("inner must keep the user's own User request: phrase, got %q", inner)
	}
	if acpUserFacingText(nested) != acpInnerUserRequest(nested) {
		t.Fatal("chit-chat unwrap must use the same inner request as planning")
	}
}

func TestACPWrappedContinueStaysStructurallyFull(t *testing.T) {
	_, forced := hardStructuralFullExecutionProfile(IMUserMessage{
		RequestID: "acp-test",
		Text:      acpProgrammingUserText(`F:\个人介绍`, "继续"),
	}, false, false)
	if !forced {
		t.Fatal("ACP cwd path must keep 继续 on the full agent so markdown write_file can still plan")
	}
}

func TestACPPreferLightProfileSkipsBareContinue(t *testing.T) {
	for _, inner := range []string{"继续", "继续。", "请继续", "重试", "continue", "继续…"} {
		msg := IMUserMessage{RequestID: "acp-test", Text: acpProgrammingUserText(`F:\个人介绍`, inner)}
		if acpPreferLightProfile(msg) {
			t.Fatalf("ACP %q must not take the light profile; markdown write_file planning needs full", inner)
		}
	}
	hi := IMUserMessage{RequestID: "acp-test", Text: acpProgrammingUserText(`F:\个人介绍`, "hi")}
	if !acpPreferLightProfile(hi) {
		t.Fatal("short ACP greeting can stay light")
	}
}

func TestCollectACPResultPaths(t *testing.T) {
	resp := &IMAgentResponse{
		LocalFilePath:  `D:\a\x.go`,
		LocalFilePaths: []string{`D:\a\x.go`, `D:\a\y.go`, ""},
		FileName:       "ignored-when-local-set",
	}
	paths := collectACPResultPaths(resp)
	if len(paths) != 2 {
		t.Fatalf("paths=%v", paths)
	}
}

func TestACPProgrammingRuntimeTaskIDIsOpaqueAndSessionScoped(t *testing.T) {
	first := acpProgrammingRuntimeTaskID("desktop-user:acp:one", `D:\work\demo`, "acp-one")
	if !strings.HasPrefix(first, "acp-coding-") || strings.Contains(first, "demo") {
		t.Fatalf("runtime task ID must be opaque, got %q", first)
	}
	if again := acpProgrammingRuntimeTaskID("desktop-user:acp:one", `D:\work\demo`, "acp-one"); again != first {
		t.Fatalf("runtime task ID is not stable: %q != %q", again, first)
	}
	if other := acpProgrammingRuntimeTaskID("desktop-user:acp:two", `D:\work\demo`, "acp-one"); other == first {
		t.Fatal("independent ACP sessions must not share a runtime task")
	}
}

func TestACPPromptMayMutateWorkspaceIsConservative(t *testing.T) {
	if !acpPromptMayMutateWorkspace("please implement login validation") {
		t.Fatal("implementation request must enter ACP runtime")
	}
	if acpPromptMayMutateWorkspace("what does this repository do?") {
		t.Fatal("read-only ACP question must not be upgraded to a writer runtime")
	}
}

// ACP cancellation must first close the durable Runtime attempt. The live
// request context can stop a current model/tool call, but only this Ledger
// transition also prevents a late callback from reviving the task after the
// ACP connection or GUI process is gone.
func TestACPHostSessionCancelClosesActiveRuntimeTask(t *testing.T) {
	store := codingruntime.NewMemoryStore()
	project := t.TempDir()
	policy := codingruntime.PolicySnapshot{ProjectRoot: project, Mode: "acp", FinalWorkspaceGateRequired: true}
	digest, err := codingruntime.PolicyDigest(policy)
	if err != nil {
		t.Fatal(err)
	}
	policy.Digest = digest
	taskID := acpProgrammingRuntimeTaskID("desktop-user:acp:cancel", project, "request-cancel")
	if _, err = store.CreateTask(codingruntime.Task{TaskID: taskID, OwnerID: "gui:acp:test", ProjectRef: project, Mode: "acp", RequestedWork: "implement a change", PolicyDigest: digest}); err != nil {
		t.Fatal(err)
	}
	if _, err = store.StartAttempt(taskID, "gui:acp:test", time.Minute, policy, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	if err := cancelACPProgrammingRuntimeTaskInStore(store, taskID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	task, err := store.GetTask(taskID)
	if err != nil || task.Status != codingruntime.TaskCancelled {
		t.Fatalf("task after ACP cancel = %+v, err=%v", task, err)
	}
	attempts, err := store.ListAttempts(taskID)
	if err != nil || len(attempts) != 1 || attempts[0].Status != codingruntime.TaskCancelled || attempts[0].SideEffectState != codingruntime.SideEffectUncertain {
		t.Fatalf("attempts after ACP cancel = %+v, err=%v", attempts, err)
	}
}

// A cancelled ACP attempt may still return from a model/tool callback. The
// Runtime must keep cancellation terminal and record that return as stale
// instead of allowing it to overwrite the durable task state.
func TestACPProgrammingRuntimeLateCallbackIsDiscardedAfterCancel(t *testing.T) {
	store := codingruntime.NewMemoryStore()
	project := t.TempDir()
	policy := codingruntime.PolicySnapshot{ProjectRoot: project, Mode: "acp"}
	digest, err := codingruntime.PolicyDigest(policy)
	if err != nil {
		t.Fatal(err)
	}
	policy.Digest = digest
	taskID := acpProgrammingRuntimeTaskID("desktop-user:acp:late", project, "request-late")

	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, _, runErr := (codingruntime.Runner{Store: store, LeaseOwner: "gui:acp:late", LeaseDuration: time.Minute}).Run(context.Background(), codingruntime.Task{
			TaskID: taskID, OwnerID: "gui:acp:late", ProjectRef: project, Mode: "acp", RequestedWork: "implement a change", PolicyDigest: digest,
		}, policy, acpProgrammingRuntimeExecutor(func(context.Context, codingruntime.ExecutionRequest) codingruntime.ExecutionResult {
			close(started)
			<-release
			return codingruntime.ExecutionResult{Status: codingruntime.TaskCompleted, SideEffectState: codingruntime.SideEffectObserved}
		}))
		done <- runErr
	}()
	<-started
	if err := cancelACPProgrammingRuntimeTaskInStore(store, taskID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; !errors.Is(err, codingruntime.ErrStaleAttempt) {
		t.Fatalf("late ACP callback error = %v, want ErrStaleAttempt", err)
	}
	task, err := store.GetTask(taskID)
	if err != nil || task.Status != codingruntime.TaskCancelled {
		t.Fatalf("task after late callback = %+v, err=%v", task, err)
	}
	attempts, err := store.ListAttempts(taskID)
	if err != nil || len(attempts) != 1 || attempts[0].Status != codingruntime.TaskCancelled {
		t.Fatalf("attempts after late callback = %+v, err=%v", attempts, err)
	}
	events, err := store.ListEvents(attempts[0].AttemptID)
	if err != nil {
		t.Fatal(err)
	}
	foundStale := false
	for _, event := range events {
		if event.Type == "stale_callback_discarded" {
			foundStale = true
		}
	}
	if !foundStale {
		t.Fatalf("late callback audit event missing: %+v", events)
	}
}
