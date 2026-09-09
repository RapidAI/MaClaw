package agentservice

import (
	"context"
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
