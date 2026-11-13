package agentservice

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
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

func TestUserPauseNeverAutoCancels(t *testing.T) {
	svc, p, inst, run, done := livePausableRun(t)
	defer done()
	st, err := svc.PauseRun(context.Background(), p, inst.ID, run.ID, "pause", 40*time.Millisecond)
	if err != nil || st.OnTimeout != "hold" || st.TimeoutSec != 0 {
		t.Fatalf("pause: %v %+v", err, st)
	}
	held := heldIn(svc, run.ID)
	select {
	case <-held:
		t.Fatal("a user pause ended by itself")
	case <-time.After(120 * time.Millisecond):
	}
	got, _ := svc.store.GetRun(p.TenantID, p.UserID, inst.ID, run.ID)
	if got.Status != RunStatusRunning {
		t.Fatalf("status %s", got.Status)
	}
	if _, err := os.Stat(filepath.Join(svc.DataRoot(), "run-pauses.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ResumeRun(context.Background(), p, inst.ID, run.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-held:
	case <-time.After(time.Second):
		t.Fatal("resume did not release the user pause")
	}
}

func TestTakeoverPauseDoesNotCancel(t *testing.T) {
	svc, p, inst, run, done := livePausableRun(t)
	defer done()
	if _, err := svc.PauseRun(context.Background(), p, inst.ID, run.ID, "takeover", 40*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	held := heldIn(svc, run.ID)
	time.Sleep(120 * time.Millisecond)
	select {
	case <-held:
		t.Fatal("takeover pause ended on the timer")
	default:
	}
	got, _ := svc.store.GetRun(p.TenantID, p.UserID, inst.ID, run.ID)
	if got.Status != RunStatusRunning || !got.WaitingForUser {
		t.Fatalf("status %s waiting %v", got.Status, got.WaitingForUser)
	}
	if _, err := svc.HeartbeatRunPause(context.Background(), p, inst.ID, run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ResumeRun(context.Background(), p, inst.ID, run.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-held:
	case <-time.After(2 * time.Second):
		t.Fatal("resume did not release the takeover pause")
	}
}

func TestResumeAfterRestartDrivesTheSameRun(t *testing.T) {
	exec := &redriveExecutor{continued: make(chan string, 1)}
	svc, p, inst := setupCaptureAgentService(t, exec)
	defer svc.Close()
	sess, err := svc.CreateSession(context.Background(), p, inst.ID, CreateSessionInput{Title: "p"})
	if err != nil {
		t.Fatal(err)
	}
	run, _, err := svc.PostMessage(context.Background(), p, inst.ID, sess.ID, PostMessageInput{Content: "把报表下载下来", Metadata: map[string]string{"client_message_id": "restart-1"}})
	if err != nil || run == nil {
		t.Fatalf("post: %v", err)
	}
	// The process died while the run was paused. The row is still running and
	// the pause gate was restored from disk. Nothing in this process is
	// executing it.
	stored, err := svc.store.GetRun(p.TenantID, p.UserID, inst.ID, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	stored.Status = RunStatusRunning
	stored.CompletedAt = nil
	stored.WaitingForUser = true
	if err := svc.store.SaveRun(stored); err != nil {
		t.Fatal(err)
	}
	svc.pauses.mu.Lock()
	if svc.pauses.gates == nil {
		svc.pauses.gates = map[string]*runPauseGate{}
	}
	svc.pauses.gates[stored.ID] = &runPauseGate{
		principal: p, instanceID: inst.ID, reason: "takeover", onTimeout: "remind",
		pausedAt: time.Now(), timeout: time.Minute, expires: time.Now().Add(time.Minute),
		wake: make(chan struct{}),
	}
	svc.pauses.mu.Unlock()
	resumed, err := svc.ResumeRun(context.Background(), p, inst.ID, stored.ID)
	if err != nil || resumed.Outcome != "resumed" {
		t.Fatalf("resume: %v %+v", err, resumed)
	}
	select {
	case id := <-exec.continued:
		if id != stored.ID {
			t.Fatalf("continued %s, want %s", id, stored.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("resume reported success without driving the run")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		got, err := svc.store.GetRun(p.TenantID, p.UserID, inst.ID, stored.ID)
		if err == nil && got.Status == RunStatusSucceeded {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("driven run status %s err %v", got.Status, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type redriveExecutor struct {
	continued chan string
	calls     int
	mu        sync.Mutex
}

func (e *redriveExecutor) Execute(_ context.Context, req ExecuteRequest) (*ExecuteResult, error) {
	e.mu.Lock()
	e.calls++
	n := e.calls
	e.mu.Unlock()
	if n == 1 {
		return &ExecuteResult{Content: "first"}, nil
	}
	e.continued <- req.RunID
	return &ExecuteResult{Content: "继续做完了"}, nil
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
