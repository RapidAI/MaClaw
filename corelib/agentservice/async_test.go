package agentservice

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

type blockingAsyncExecutor struct {
	started chan struct{}
	once    sync.Once
}

func (e *blockingAsyncExecutor) Execute(ctx context.Context, _ ExecuteRequest) (*ExecuteResult, error) {
	e.once.Do(func() { close(e.started) })
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestPostMessageAsyncAdmitsBeforeExecutionCompletes(t *testing.T) {
	executor := &blockingAsyncExecutor{started: make(chan struct{})}
	svc, principal, inst := setupCaptureAgentService(t, executor)
	sess, err := svc.CreateSession(context.Background(), principal, inst.ID, CreateSessionInput{Title: "async"})
	if err != nil {
		t.Fatal(err)
	}

	in := PostMessageInput{Content: "hello", Metadata: map[string]string{"client_message_id": "async-1"}}
	run, err := svc.PostMessageAsync(context.Background(), principal, inst.ID, sess.ID, in)
	if err != nil {
		t.Fatalf("PostMessageAsync: %v", err)
	}
	if run == nil || run.Status != RunStatusRunning {
		t.Fatalf("expected admitted running run, got %#v", run)
	}
	select {
	case <-executor.started:
	case <-time.After(2 * time.Second):
		t.Fatal("executor did not start")
	}

	// The idempotency gate is released at admission, so a retry can return the
	// existing run without waiting for the blocked executor.
	retryCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	retry, err := svc.PostMessageAsync(retryCtx, principal, inst.ID, sess.ID, in)
	if err != nil {
		t.Fatalf("async retry: %v", err)
	}
	if retry == nil || retry.ID != run.ID {
		t.Fatalf("retry returned %#v, want run %s", retry, run.ID)
	}

	if err := svc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	stored, err := svc.store.GetRun(principal.TenantID, principal.UserID, inst.ID, run.ID)
	if err != nil {
		t.Fatalf("GetRun after Close: %v", err)
	}
	if stored.Status != RunStatusCancelled {
		t.Fatalf("expected shutdown to cancel async run, got %s", stored.Status)
	}
}

// gateExecutor blocks until release is closed, unless its own context ends.
// Cancelling the caller that admitted the run must not end that context.
type gateExecutor struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (e *gateExecutor) Execute(ctx context.Context, _ ExecuteRequest) (*ExecuteResult, error) {
	e.once.Do(func() { close(e.started) })
	select {
	case <-e.release:
		return &ExecuteResult{Content: "finished later"}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestPostMessageAsyncSurvivesCallerCancel(t *testing.T) {
	executor := &gateExecutor{started: make(chan struct{}), release: make(chan struct{})}
	svc, principal, inst := setupCaptureAgentService(t, executor)
	sess, err := svc.CreateSession(context.Background(), principal, inst.ID, CreateSessionInput{Title: "async"})
	if err != nil {
		t.Fatal(err)
	}
	prepared := make(chan struct{}, 1)
	finished := make(chan struct{}, 1)
	admitCtx, cancelAdmit := context.WithCancel(context.Background())
	in := PostMessageInput{
		Content:  "hold the desktop",
		Metadata: map[string]string{"client_message_id": "async-hold"},
		PrepareExecution: func(ctx context.Context) context.Context {
			if ctx.Err() != nil {
				t.Errorf("prepare context already canceled")
			}
			prepared <- struct{}{}
			return ctx
		},
		FinishExecution: func(context.Context, *Run, *Message, error) {
			finished <- struct{}{}
		},
	}
	run, err := svc.PostMessageAsync(admitCtx, principal, inst.ID, sess.ID, in)
	if err != nil {
		t.Fatalf("PostMessageAsync: %v", err)
	}
	select {
	case <-executor.started:
	case <-time.After(2 * time.Second):
		t.Fatal("executor did not start")
	}
	select {
	case <-prepared:
	case <-time.After(2 * time.Second):
		t.Fatal("prepare did not run")
	}
	// The HTTP caller is gone. The run keeps the desktop until it finishes.
	cancelAdmit()
	select {
	case <-finished:
		t.Fatal("finish ran before the executor was released")
	case <-time.After(150 * time.Millisecond):
	}
	stored, err := svc.store.GetRun(principal.TenantID, principal.UserID, inst.ID, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != RunStatusRunning {
		t.Fatalf("caller cancel changed the run to %s", stored.Status)
	}
	close(executor.release)
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("finish did not run after the executor returned")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		stored, err = svc.store.GetRun(principal.TenantID, principal.UserID, inst.ID, run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.Status == RunStatusSucceeded {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("run status = %s, want succeeded", stored.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// panicAsyncExecutor starts, then panics. The admitted run must still finish.
type panicAsyncExecutor struct {
	started chan struct{}
	once    sync.Once
}

func (e *panicAsyncExecutor) Execute(context.Context, ExecuteRequest) (*ExecuteResult, error) {
	e.once.Do(func() { close(e.started) })
	panic("browser broke")
}

func TestPostMessageAsyncPanicStillFinishes(t *testing.T) {
	executor := &panicAsyncExecutor{started: make(chan struct{})}
	svc, principal, inst := setupCaptureAgentService(t, executor)
	t.Cleanup(func() { _ = svc.Close() })
	sess, err := svc.CreateSession(context.Background(), principal, inst.ID, CreateSessionInput{Title: "async"})
	if err != nil {
		t.Fatal(err)
	}
	type finishReport struct {
		runID string
		err   error
	}
	finished := make(chan finishReport, 1)
	run, err := svc.PostMessageAsync(context.Background(), principal, inst.ID, sess.ID, PostMessageInput{
		Content: "panic please",
		FinishExecution: func(_ context.Context, got *Run, _ *Message, finishErr error) {
			report := finishReport{err: finishErr}
			if got != nil {
				report.runID = got.ID
			}
			finished <- report
		},
	})
	if err != nil || run == nil || run.ID == "" {
		t.Fatalf("admit run=%v err=%v", run, err)
	}
	select {
	case report := <-finished:
		if report.runID != run.ID || report.err == nil || !strings.Contains(report.err.Error(), "execution panicked") || !strings.Contains(report.err.Error(), "browser broke") {
			t.Fatalf("finish run=%q err=%v", report.runID, report.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("finish did not run after the executor panicked")
	}
	stored, err := svc.store.GetRun(principal.TenantID, principal.UserID, inst.ID, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != RunStatusFailed || !strings.Contains(stored.Error, "execution panicked") || !strings.Contains(stored.Error, "browser broke") {
		t.Fatalf("status = %s error = %q", stored.Status, stored.Error)
	}
}

// panicHistoryStore panics on the first history read, which is after the run
// is admitted and before a cancel func exists.
type panicHistoryStore struct {
	*MemoryStore
	once sync.Once
}

func (s *panicHistoryStore) ListMessages(sessionID string) ([]Message, error) {
	panicNow := false
	s.once.Do(func() { panicNow = true })
	if panicNow {
		panic("history broke")
	}
	return s.MemoryStore.ListMessages(sessionID)
}

func TestPostMessageAsyncPanicBeforeCancelStillFailsTheRun(t *testing.T) {
	executor := &gateExecutor{started: make(chan struct{}), release: make(chan struct{})}
	svc, principal, inst := setupCaptureAgentService(t, executor)
	t.Cleanup(func() {
		close(executor.release)
		_ = svc.Close()
	})
	sess, err := svc.CreateSession(context.Background(), principal, inst.ID, CreateSessionInput{Title: "async"})
	if err != nil {
		t.Fatal(err)
	}
	base, ok := svc.store.(*MemoryStore)
	if !ok {
		t.Fatalf("store = %T", svc.store)
	}
	svc.store = &panicHistoryStore{MemoryStore: base}
	type finishReport struct {
		runID string
		err   error
	}
	finished := make(chan finishReport, 1)
	run, err := svc.PostMessageAsync(context.Background(), principal, inst.ID, sess.ID, PostMessageInput{
		Content: "panic before execute",
		FinishExecution: func(_ context.Context, got *Run, _ *Message, finishErr error) {
			report := finishReport{err: finishErr}
			if got != nil {
				report.runID = got.ID
			}
			finished <- report
		},
	})
	if err != nil || run == nil || run.ID == "" {
		t.Fatalf("admit run=%v err=%v", run, err)
	}
	select {
	case report := <-finished:
		if report.runID != run.ID || report.err == nil || !strings.Contains(report.err.Error(), "history broke") {
			t.Fatalf("finish run=%q err=%v", report.runID, report.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("finish did not run")
	}
	stored, err := base.GetRun(principal.TenantID, principal.UserID, inst.ID, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != RunStatusFailed || !strings.Contains(stored.Error, "history broke") {
		t.Fatalf("status = %s error = %q", stored.Status, stored.Error)
	}
}
