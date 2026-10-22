package agentservice

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestOrphanRunsReconciledAndCancellable(t *testing.T) {
	executor := &blockingAsyncExecutor{started: make(chan struct{})}
	svc, principal, inst := setupCaptureAgentService(t, executor)
	defer svc.Close()
	mk := func(id string) *Run {
		sess, err := svc.CreateSession(context.Background(), principal, inst.ID, CreateSessionInput{Title: id})
		if err != nil {
			t.Fatal(err)
		}
		run, err := svc.PostMessageAsync(context.Background(), principal, inst.ID, sess.ID, PostMessageInput{Content: "x", Metadata: map[string]string{"client_message_id": id}})
		if err != nil || run == nil {
			t.Fatalf("post: %v", err)
		}
		return run
	}
	live := mk("live")
	<-executor.started
	// Live run: this process holds its cancel func; reconcile leaves it.
	if n := svc.reconcileOrphanRuns(); n != 0 {
		t.Fatalf("live run reconciled: %d", n)
	}
	// Simulate a run left by a dead process: no cancel func here.
	orphan, _ := svc.store.GetRun(principal.TenantID, principal.UserID, inst.ID, live.ID)
	cancel, _ := svc.takeRunCancel(orphan.ID)
	if n := svc.reconcileOrphanRuns(); n != 1 {
		t.Fatalf("orphans=%d", n)
	}
	got, _ := svc.store.GetRun(principal.TenantID, principal.UserID, inst.ID, orphan.ID)
	if got.Status != RunStatusFailed || got.Error != runInterruptedError || got.CompletedAt == nil {
		t.Fatalf("got %+v", got)
	}
	if busy, _ := svc.hasRunningRuns(principal.TenantID, principal.UserID, inst.ID, got.SessionID); busy {
		t.Fatal("session still blocked")
	}
	cancel()

	// CancelRun on an orphan older than the grace window cancels it.
	old := got
	old.Status, old.Error, old.CompletedAt = RunStatusRunning, "", nil
	old.StartedAt = time.Now().Add(-time.Minute)
	if err := svc.store.SaveRun(old); err != nil {
		t.Fatal(err)
	}
	out, err := svc.CancelRun(context.Background(), principal, inst.ID, old.ID)
	if err != nil || out.Status != RunStatusCancelled {
		t.Fatalf("cancel orphan: %v %+v", err, out)
	}
	// Inside the grace window (cancel not yet registered) the old answer stays.
	fresh := old
	fresh.Status, fresh.Error, fresh.CompletedAt = RunStatusRunning, "", nil
	fresh.StartedAt = time.Now()
	_ = svc.store.SaveRun(fresh)
	if _, err := svc.CancelRun(context.Background(), principal, inst.ID, fresh.ID); !errors.Is(err, ErrRunNotRunning) {
		t.Fatalf("fresh: %v", err)
	}
	t.Setenv("MACLAW_RECONCILE_ORPHAN_RUNS", "off")
	if n := svc.reconcileOrphanRuns(); n != 0 {
		t.Fatalf("disabled but reconciled %d", n)
	}
}
