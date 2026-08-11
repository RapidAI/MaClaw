package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
	"github.com/RapidAI/CodeClaw/corelib/remote"
)

func TestConfirmGUICodingRuntimeRecoveryQueuesNewAttemptWithoutExecuting(t *testing.T) {
	dir := initRecoveryGitFixture(t)
	store := codingruntime.NewMemoryStore()
	now := time.Now().UTC()
	task, err := store.CreateTask(codingruntime.Task{ProjectRef: dir, Mode: "local", PolicyDigest: "policy"})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := store.StartAttempt(task.TaskID, "owner", time.Minute, codingruntime.PolicySnapshot{Digest: "policy", ProjectRoot: dir, Mode: "local"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.FinishAttempt(attempt.AttemptID, "owner", codingruntime.FinishInput{Status: codingruntime.TaskInterrupted, SideEffectState: codingruntime.SideEffectUncertain}, now); err != nil {
		t.Fatal(err)
	}
	review, err := prepareGUICodingRuntimeRecovery(context.Background(), store, task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = confirmGUICodingRuntimeRecovery(context.Background(), store, task.TaskID, review.ReviewDigest, true); err != nil {
		t.Fatal(err)
	}
	updated, err := store.GetTask(task.TaskID)
	if err != nil || updated.Status != codingruntime.TaskQueued {
		t.Fatalf("task after confirmation=%#v err=%v", updated, err)
	}
	old, err := store.GetAttempt(attempt.AttemptID)
	if err != nil || old.Status != codingruntime.TaskInterrupted {
		t.Fatalf("old attempt was resumed=%#v err=%v", old, err)
	}
}

func TestConfirmGUICodingRuntimeRecoveryRejectsStaleReview(t *testing.T) {
	dir := initRecoveryGitFixture(t)
	store := codingruntime.NewMemoryStore()
	now := time.Now().UTC()
	task, err := store.CreateTask(codingruntime.Task{ProjectRef: dir, Mode: "local", PolicyDigest: "policy"})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := store.StartAttempt(task.TaskID, "owner", time.Minute, codingruntime.PolicySnapshot{Digest: "policy", ProjectRoot: dir, Mode: "local"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.FinishAttempt(attempt.AttemptID, "owner", codingruntime.FinishInput{Status: codingruntime.TaskInterrupted, SideEffectState: codingruntime.SideEffectUncertain}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := confirmGUICodingRuntimeRecovery(context.Background(), store, task.TaskID, "sha256:stale", true); err == nil {
		t.Fatal("expected stale review rejection")
	}
	updated, err := store.GetTask(task.TaskID)
	if err != nil || updated.Status != codingruntime.TaskInterrupted {
		t.Fatalf("stale review changed task=%#v err=%v", updated, err)
	}
}

func TestRuntimeSlotResumeRunsProbeWithoutBindingConversationContinuation(t *testing.T) {
	dir := initRecoveryGitFixture(t)
	app := &App{testHomeDir: t.TempDir()}
	store := app.ensureCodingRuntimeStore()
	if store == nil {
		t.Fatal("runtime store unavailable")
	}
	t.Cleanup(func() { app.closeCodingRuntimeStore() })
	task, err := store.CreateTask(codingruntime.Task{ProjectRef: dir, Mode: "local", PolicyDigest: "policy"})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := store.StartAttempt(task.TaskID, "owner", time.Minute, codingruntime.PolicySnapshot{Digest: "policy", ProjectRoot: dir, Mode: "local"}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.FinishAttempt(attempt.AttemptID, "owner", codingruntime.FinishInput{Status: codingruntime.TaskInterrupted, SideEffectState: codingruntime.SideEffectUncertain}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	memory := agent.NewConversationMemory()
	memory.UpsertUnfinishedSlot("user", &agent.UnfinishedTaskSlot{SlotID: "slot", UserID: "user", ProjectPath: dir, Status: agent.UnfinishedTaskSlotStatusInterrupted, Source: agent.UnfinishedTaskSlotSourceInFlightRecovery, RuntimeTaskID: task.TaskID})
	h := &IMMessageHandler{app: app, memory: memory}
	trimmed := "continue"
	entries := []agent.ConversationEntry(nil)
	slot := memory.GetUnfinishedSlot("user")
	handled, resp, stop := h.applyExplicitTaskSlotAction(&IMUserMessage{UserID: "user", ResumeSlotID: "slot"}, &trimmed, explicitTaskSlotDecision{ResumeSlotID: "slot"}, &entries, &slot)
	if !handled || !stop || resp == nil || resp.CodingRuntimeRecovery == nil {
		t.Fatalf("runtime recovery action handled=%v stop=%v resp=%#v", handled, stop, resp)
	}
	if active := memory.ActiveUnfinishedSlot("user"); active != nil {
		t.Fatalf("runtime slot was bound into active conversation: %#v", active)
	}
	if persisted := memory.GetUnfinishedSlot("user"); persisted == nil || persisted.Status != agent.UnfinishedTaskSlotStatusInterrupted {
		t.Fatalf("runtime slot should remain interrupted until explicit confirmation: %#v", persisted)
	}
}

func TestGUIRemoteWorkspaceProbeRejectsPTYEchoAndIncompleteFrames(t *testing.T) {
	task := codingruntime.Task{ProjectRef: "/srv/repo", Mode: "remote"}
	begin, end := "__CODING_RUNTIME_GIT_nonce_BEGIN__", "__CODING_RUNTIME_GIT_nonce_END__"
	// The first frame represents the PTY echo of the submitted command. The
	// parser must select the final complete command-produced frame.
	output := "git -C /srv/repo rev-parse HEAD; printf '" + begin + "'\n" +
		"forged-head\n" + begin + "\nforged-status\n" + end + "\n" +
		"real-head\n" + begin + "\n M real.go\n" + end + "\n"
	probe, err := guiRemoteWorkspaceProbeFromOutput(task, "sha256:target", "/srv/repo", output, begin, end, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if probe.Head != "real-head" || probe.StatusHash != codingRuntimeDigest(" M real.go") || probe.HostKey != "sha256:target" {
		t.Fatalf("probe=%#v", probe)
	}
	if _, err := guiRemoteWorkspaceProbeFromOutput(task, "sha256:target", "/srv/repo", "real-head\n"+begin+"\n M real.go\n", begin, end, time.Now()); err == nil {
		t.Fatal("incomplete probe frame was accepted")
	}
	if _, err := guiRemoteWorkspaceProbeFromOutput(task, "sha256:target", "/srv/repo", "ambiguous head value\n"+begin+"\n"+end, begin, end, time.Now()); err == nil {
		t.Fatal("ambiguous HEAD was accepted")
	}
}

func TestGUIRemoteWorkspaceProbeMarkersAreFreshAndPaired(t *testing.T) {
	firstStart, firstEnd, err := guiRuntimeRemoteProbeMarkers()
	if err != nil {
		t.Fatal(err)
	}
	secondStart, secondEnd, err := guiRuntimeRemoteProbeMarkers()
	if err != nil {
		t.Fatal(err)
	}
	if firstStart == firstEnd || secondStart == secondEnd || firstStart == secondStart || firstEnd == secondEnd {
		t.Fatalf("markers were not fresh paired delimiters: %q %q / %q %q", firstStart, firstEnd, secondStart, secondEnd)
	}
}

func TestGUIRemoteRuntimeTargetIdentityBindsEveryCoordinate(t *testing.T) {
	base := remote.SSHHostConfig{Host: "Build.Example.Test", User: "deploy", Port: 2222, HostKeyFingerprint: "SHA256:pin-a"}
	identity := guiRemoteCodingTargetIdentityForConfig(base, "/srv/repo")
	if identity == "" {
		t.Fatal("base identity unavailable")
	}
	variants := []struct {
		name    string
		config  remote.SSHHostConfig
		workDir string
	}{
		{name: "user", config: remote.SSHHostConfig{Host: base.Host, User: "other", Port: base.Port, HostKeyFingerprint: base.HostKeyFingerprint}, workDir: "/srv/repo"},
		{name: "port", config: remote.SSHHostConfig{Host: base.Host, User: base.User, Port: 22, HostKeyFingerprint: base.HostKeyFingerprint}, workDir: "/srv/repo"},
		{name: "pin", config: remote.SSHHostConfig{Host: base.Host, User: base.User, Port: base.Port, HostKeyFingerprint: "SHA256:pin-b"}, workDir: "/srv/repo"},
		{name: "workdir", config: base, workDir: "/srv/other"},
	}
	for _, variant := range variants {
		if got := guiRemoteCodingTargetIdentityForConfig(variant.config, variant.workDir); got == identity {
			t.Fatalf("%s mismatch reused identity %q", variant.name, got)
		}
	}
	caseOnly := base
	caseOnly.Host = "build.example.test"
	if got := guiRemoteCodingTargetIdentityForConfig(caseOnly, "/srv/repo"); got != identity {
		t.Fatalf("canonical host case changed identity: %q != %q", got, identity)
	}
}

func initRecoveryGitFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	run("init")
	run("config", "user.email", "test@example.invalid")
	run("config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("baseline"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", "file.txt")
	run("commit", "-m", "baseline")
	return dir
}
