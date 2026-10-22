package agentservice

import (
	"context"
	"errors"
	"testing"
	"time"
)

func livePausableRun(t *testing.T) (*Service, Principal, Instance, *Run, func()) {
	t.Helper()
	executor := &blockingAsyncExecutor{started: make(chan struct{})}
	svc, principal, inst := setupCaptureAgentService(t, executor)
	sess, err := svc.CreateSession(context.Background(), principal, inst.ID, CreateSessionInput{Title: "p"})
	if err != nil {
		t.Fatal(err)
	}
	run, err := svc.PostMessageAsync(context.Background(), principal, inst.ID, sess.ID, PostMessageInput{Content: "x", Metadata: map[string]string{"client_message_id": "p1"}})
	if err != nil || run == nil {
		t.Fatalf("post: %v", err)
	}
	<-executor.started
	return svc, principal, inst, run, func() { svc.Close() }
}

func heldIn(svc *Service, runID string) <-chan struct{} {
	done := make(chan struct{})
	go func() { svc.holdAtStepBoundary(runID); close(done) }()
	return done
}

func TestRunPauseHoldsUntilResumeSameRun(t *testing.T) {
	svc, p, inst, run, done := livePausableRun(t)
	defer done()
	// Not paused: the boundary passes straight through.
	select {
	case <-heldIn(svc, run.ID):
	case <-time.After(time.Second):
		t.Fatal("unpaused run held")
	}
	st, err := svc.PauseRun(context.Background(), p, inst.ID, run.ID, "check", 0)
	if err != nil || !st.Paused || st.TimeoutSec != int64(DefaultRunPauseTimeout/time.Second) || st.OnTimeout != "cancel" {
		t.Fatalf("pause: %v %+v", err, st)
	}
	// Pausing twice is idempotent.
	if again, _ := svc.PauseRun(context.Background(), p, inst.ID, run.ID, "", 0); !again.PausedAt.Equal(*st.PausedAt) {
		t.Fatal("second pause reset the gate")
	}
	held := heldIn(svc, run.ID)
	select {
	case <-held:
		t.Fatal("paused run passed the boundary")
	case <-time.After(100 * time.Millisecond):
	}
	if cur, _ := svc.RunPause(context.Background(), p, inst.ID, run.ID); !cur.Held {
		t.Fatalf("state not held: %+v", cur)
	}
	if r, err := svc.ResumeRun(context.Background(), p, inst.ID, run.ID); err != nil || r.Outcome != "resumed" {
		t.Fatalf("resume: %v %+v", err, r)
	}
	select {
	case <-held:
	case <-time.After(time.Second):
		t.Fatal("resume did not release")
	}
	got, _ := svc.store.GetRun(p.TenantID, p.UserID, inst.ID, run.ID)
	if got.Status != RunStatusRunning {
		t.Fatalf("resumed run not still running: %s", got.Status)
	}
	if r, _ := svc.ResumeRun(context.Background(), p, inst.ID, run.ID); r.Outcome != "not_paused" {
		t.Fatalf("resume twice: %+v", r)
	}
}

func TestRunPauseCancelWakesHeldRun(t *testing.T) {
	svc, p, inst, run, done := livePausableRun(t)
	defer done()
	if _, err := svc.PauseRun(context.Background(), p, inst.ID, run.ID, "", 0); err != nil {
		t.Fatal(err)
	}
	held := heldIn(svc, run.ID)
	time.Sleep(50 * time.Millisecond)
	if _, err := svc.CancelRun(context.Background(), p, inst.ID, run.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-held:
	case <-time.After(time.Second):
		t.Fatal("cancel left run held")
	}
	if st, _ := svc.RunPause(context.Background(), p, inst.ID, run.ID); st.Paused || st.Outcome != "cancelled" {
		t.Fatalf("state after cancel: %+v", st)
	}
	if _, err := svc.PauseRun(context.Background(), p, inst.ID, run.ID, "", 0); !errors.Is(err, ErrRunNotRunning) {
		t.Fatalf("pause cancelled run: %v", err)
	}
}

func TestRunPauseTimeoutCancels(t *testing.T) {
	svc, p, inst, run, done := livePausableRun(t)
	defer done()
	if _, err := svc.PauseRun(context.Background(), p, inst.ID, run.ID, "", 80*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	select {
	case <-heldIn(svc, run.ID):
	case <-time.After(3 * time.Second):
		t.Fatal("pause never timed out")
	}
	got, _ := svc.store.GetRun(p.TenantID, p.UserID, inst.ID, run.ID)
	if got.Status != RunStatusCancelled {
		t.Fatalf("timed-out pause left run %s", got.Status)
	}
	if st, _ := svc.RunPause(context.Background(), p, inst.ID, run.ID); st.Outcome != "timed_out" {
		t.Fatalf("outcome %+v", st)
	}
}

func TestRunPauseTimeoutEnv(t *testing.T) {
	t.Setenv("MACLAW_RUN_PAUSE_TIMEOUT_SEC", "90")
	if runPauseTimeoutFromEnv() != 90*time.Second {
		t.Fatal("env ignored")
	}
	t.Setenv("MACLAW_RUN_PAUSE_TIMEOUT_SEC", "999999")
	if runPauseTimeoutFromEnv() != MaxRunPauseTimeout {
		t.Fatal("cap ignored")
	}
}

func TestSupplementHookCallsHold(t *testing.T) {
	called := 0
	h := &supplementHook{take: func() []*RunSupplement { return nil }, hold: func() { called++ }}
	h.TransformConversation(nil)
	if called != 1 {
		t.Fatal("hook did not hold at boundary")
	}
}
