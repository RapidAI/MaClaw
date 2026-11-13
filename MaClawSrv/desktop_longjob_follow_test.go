package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestLongJobRunningRunGetsAFollowUp(t *testing.T) {
	d100Env(t)
	svc, store, p, inst := longJobOwnerService(t)
	sess, err := svc.CreateSession(context.Background(), p, inst.ID, agentservice.CreateSessionInput{Title: "s"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRun(agentservice.Run{
		ID: "run_busy", TenantID: p.TenantID, UserID: p.UserID, InstanceID: inst.ID,
		SessionID: sess.ID, Status: agentservice.RunStatusRunning, StartedAt: time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	prev := desktopLongJobService
	t.Cleanup(func() { desktopLongJobService = prev })
	desktopLongJobService = svc
	var said string
	prevSay := desktopLongJobSayFunc
	t.Cleanup(func() { desktopLongJobSayFunc = prevSay })
	desktopLongJobSayFunc = func(_ context.Context, body map[string]any) error {
		said, _ = body["text"].(string)
		return nil
	}
	j := &desktopLongJob{
		ID: "job_follow", TenantID: p.TenantID, UserID: "u::desk::bot", InstanceID: inst.ID,
		SessionID: sess.ID, SourceRun: "run_busy", PrincipalTenant: p.TenantID, PrincipalUser: p.UserID,
		Status: "failed", ExitUnknown: "no_exit", Started: time.Now().Add(-time.Hour),
	}
	if err := desktopLongJobSave(j); err != nil {
		t.Fatal(err)
	}
	if err := desktopLongJobTalkDefault(context.Background(), j, longJobResume); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(said, "我接着看一下原因和下一步。") {
		t.Fatalf("promise missing from %q", said)
	}
	if !strings.Contains(said, "没有 job.exit") {
		t.Fatalf("exit reason missing from %q", said)
	}
	items, err := svc.RunSupplements(context.Background(), p, inst.ID, "run_busy")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || !strings.Contains(items[0].Content, "job job_follow finished: verify and report") {
		t.Fatalf("supplements %#v", items)
	}
	if got := desktopLongJobGet(j.ID); got.FollowUp != "injected" {
		t.Fatalf("follow-up %+v", got)
	}

	// The source run ends without a user-facing report: a new turn is scheduled.
	busy := agentservice.Run{
		ID: "run_busy", TenantID: p.TenantID, UserID: p.UserID, InstanceID: inst.ID,
		SessionID: sess.ID, Status: agentservice.RunStatusSucceeded, StartedAt: time.Now().Add(-time.Minute),
	}
	if err := store.SaveRun(busy); err != nil {
		t.Fatal(err)
	}
	desktopLongJobRunEnded("run_busy")
	if got := desktopLongJobGet(j.ID); got.FollowUp != "started" || got.FollowRun == "" || got.FollowRun == "run_busy" {
		t.Fatalf("follow-up after the source run %+v", got)
	}
}

func TestLongJobSkippedFollowUpMakesNoPromise(t *testing.T) {
	d100Env(t)
	prev := desktopLongJobService
	t.Cleanup(func() { desktopLongJobService = prev })
	desktopLongJobService = nil
	var said string
	prevSay := desktopLongJobSayFunc
	t.Cleanup(func() { desktopLongJobSayFunc = prevSay })
	desktopLongJobSayFunc = func(_ context.Context, body map[string]any) error {
		said, _ = body["text"].(string)
		return nil
	}
	j := &desktopLongJob{ID: "job_skip", InstanceID: "inst_x", Status: "failed", Started: time.Now()}
	_ = desktopLongJobTalkDefault(context.Background(), j, longJobResume)
	if strings.Contains(said, "我接着看一下原因和下一步。") {
		t.Fatalf("promise without a follow-up: %q", said)
	}
	if got := desktopLongJobGet(j.ID); got == nil || got.FollowUp != "skipped" {
		t.Fatalf("stored %+v", got)
	}
}

func TestLongJobExitUnknownIsSpecific(t *testing.T) {
	noExit := desktopLongJobParsePoll("LONGJOB_DEAD pid=9\nLONGJOB_NO_EXIT\n")
	if !noExit.NoExit || noExit.ExitCode != nil || noExit.PID != "9" {
		t.Fatalf("%+v", noExit)
	}
	if got := desktopLongJobUnknownExit(noExit); got != "no_exit" {
		t.Fatalf("no exit file: %s", got)
	}
	gone := desktopLongJobParsePoll("LONGJOB_DEAD pid=\nLONGJOB_NO_EXIT\n")
	if got := desktopLongJobUnknownExit(gone); got != "process_gone,no_exit" {
		t.Fatalf("process gone: %s", got)
	}
	j := &desktopLongJob{Status: "failed", ExitUnknown: "poll_failed"}
	if phrase := desktopLongJobExitPhrase(j); phrase != "这次检查没能读到状态" {
		t.Fatalf("poll: %s", phrase)
	}
	j.ExitUnknown = "no_exit"
	if phrase := desktopLongJobExitPhrase(j); !strings.Contains(phrase, "没有 job.exit") {
		t.Fatalf("phrase %s", phrase)
	}
	code := 7
	j.ExitCode = &code
	if phrase := desktopLongJobExitPhrase(j); phrase != "退出码 7" {
		t.Fatalf("code %s", phrase)
	}
}
