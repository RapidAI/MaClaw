package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestLongJobRelaunchSupersedesTheOpenOnes(t *testing.T) {
	d100Env(t)
	old := &desktopLongJob{
		ID: "job_d153aa18", InstanceID: "inst", SessionID: "sess", SourceRun: "run_6ec4c027",
		Status: "running", Alive: true, Started: time.Now().Add(-2 * time.Hour),
	}
	mid := &desktopLongJob{
		ID: "job_9e4197aa", InstanceID: "inst", SessionID: "sess", SourceRun: "run_6ec4c027",
		Status: "running", Alive: true, Started: time.Now().Add(-time.Hour),
	}
	keep := &desktopLongJob{
		ID: "job_keep", InstanceID: "inst", SessionID: "sess", SourceRun: "run_6ec4c027",
		Status: "running", Alive: true, KeepRunning: true, Started: time.Now().Add(-time.Hour),
	}
	other := &desktopLongJob{
		ID: "job_other", InstanceID: "inst", SessionID: "other-session", SourceRun: "run_other",
		Status: "running", Alive: true, Started: time.Now().Add(-time.Hour),
	}
	for _, j := range []*desktopLongJob{old, mid, keep, other} {
		if err := desktopLongJobSave(j); err != nil {
			t.Fatal(err)
		}
	}
	desktopLongJobSupersede(mid)
	if got := desktopLongJobGet(old.ID); got.Status != "cancelled" || !strings.Contains(got.EndReason, "superseded by "+mid.ID) {
		t.Fatalf("first relaunch left the old job %+v", got)
	}
	newest := &desktopLongJob{
		ID: "job_7a4f01d5", InstanceID: "inst", SessionID: "sess", SourceRun: "run_6ec4c027",
		Status: "running", Alive: true, Started: time.Now(),
	}
	if err := desktopLongJobSave(newest); err != nil {
		t.Fatal(err)
	}
	desktopLongJobSupersede(newest)
	if got := desktopLongJobGet(mid.ID); got.Status != "cancelled" || !strings.Contains(got.EndReason, newest.ID) {
		t.Fatalf("second relaunch left the previous job %+v", got)
	}
	if got := desktopLongJobGet(newest.ID); got.Status != "running" {
		t.Fatalf("newest was cancelled %+v", got)
	}
	if got := desktopLongJobGet(keep.ID); got.Status != "running" {
		t.Fatalf("keep_running was cancelled %+v", got)
	}
	if got := desktopLongJobGet(other.ID); got.Status != "running" {
		t.Fatalf("another session was cancelled %+v", got)
	}
}

func TestLongJobAfterTheOwnerConcludedDoesNotSpeak(t *testing.T) {
	d100Env(t)
	var said []string
	prevSay := desktopLongJobSayFunc
	t.Cleanup(func() { desktopLongJobSayFunc = prevSay })
	desktopLongJobSayFunc = func(_ context.Context, body map[string]any) error {
		said = append(said, body["text"].(string))
		return nil
	}
	var prompts []string
	prevSend := desktopLongJobOutcomeSend
	t.Cleanup(func() { desktopLongJobOutcomeSend = prevSend })
	desktopLongJobOutcomeSend = func(_ context.Context, _ *desktopLongJob, prompt string) (string, error) {
		prompts = append(prompts, prompt)
		return "run_follow", nil
	}
	run := "run_6ec4c027"
	desktopLongJobReconcileRun(run, "success")
	same := &desktopLongJob{
		ID: "job_same", InstanceID: "inst", SessionID: "sess", SourceRun: run,
		Status: "done", Alive: false, ExitCode: ownerExit(0),
		LastTail: "TAIL_SAME epoch 3/3 BEST_VAL_METRICS {\"f1\":0.9}",
		Started:  time.Now().Add(-time.Hour), Dir: t.TempDir(),
	}
	if err := desktopLongJobSave(same); err != nil {
		t.Fatal(err)
	}
	if err := desktopLongJobTalkDefault(context.Background(), same, longJobResume); err != nil {
		t.Fatal(err)
	}
	changed := &desktopLongJob{
		ID: "job_changed", InstanceID: "inst", SessionID: "sess", SourceRun: run,
		Status: "failed", Alive: false, PID: "9",
		LastTail: "TAIL_CHANGED process exited without a code",
		Started:  time.Now(), Dir: t.TempDir(),
	}
	if err := desktopLongJobSave(changed); err != nil {
		t.Fatal(err)
	}
	if err := desktopLongJobTalkDefault(context.Background(), changed, longJobResume); err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 1 || !strings.Contains(prompts[0], "TAIL_CHANGED") || strings.Contains(prompts[0], "我接着看一下") {
		t.Fatalf("follow-up %v", prompts)
	}
	for _, text := range said {
		if strings.Contains(text, "TAIL_SAME") || strings.Contains(text, "TAIL_CHANGED") || strings.Contains(text, "我接着看一下") || strings.Contains(text, "BEST_VAL_METRICS") {
			t.Fatalf("watcher spoke after the owner concluded: %q", text)
		}
	}
	if got := desktopLongJobGet(same.ID); got.OutcomeState != "record" {
		t.Fatalf("same result was not folded %+v", got)
	}
}

func TestLongJobPromiseRequiresAScheduledFollowUp(t *testing.T) {
	d100Env(t)
	var said []string
	prevSay := desktopLongJobSayFunc
	t.Cleanup(func() { desktopLongJobSayFunc = prevSay })
	desktopLongJobSayFunc = func(_ context.Context, body map[string]any) error {
		said = append(said, body["text"].(string))
		return nil
	}
	j := &desktopLongJob{
		ID: "job_stall", InstanceID: "inst_missing", SessionID: "sess", Status: "stalled", Alive: true,
		Started: time.Now().Add(-time.Hour), LastPoll: time.Now(),
	}
	if err := desktopLongJobSave(j); err != nil {
		t.Fatal(err)
	}
	_ = desktopLongJobTalkDefault(context.Background(), j, longJobResume)
	if len(said) != 1 || strings.Contains(said[0], "我接着看一下原因和下一步。") {
		t.Fatalf("promised a follow-up that was not scheduled: %v", said)
	}
}

func ownerExit(n int) *int { return &n }
