package agentservice

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
)

type countingExecutor struct{ calls atomic.Int32 }

func (e *countingExecutor) Execute(context.Context, ExecuteRequest) (*ExecuteResult, error) {
	e.calls.Add(1)
	return &ExecuteResult{Content: "ok", OutputType: "text/plain"}, nil
}

func TestSendMessageClientKeysAreAtomicUnderConcurrency(t *testing.T) {
	executor := &countingExecutor{}
	svc, principal, instance := setupCaptureAgentService(t, executor)
	defer svc.Close()

	const callers = 16
	results := make([]*Session, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			sess, _, _, err := svc.SendMessage(context.Background(), principal, instance.ID, SendMessageInput{
				ClientSessionKey: "client-session-1",
				ClientMessageID:  "client-message-1",
				Content:          "hello",
			})
			results[index], errs[index] = sess, err
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("caller %d failed: %v", i, err)
		}
	}
	if got := executor.calls.Load(); got != 1 {
		t.Fatalf("executor called %d times, want exactly once", got)
	}
	sessions, err := svc.ListSessions(context.Background(), principal, instance.ID, ListSessionsInput{})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("created %d sessions, want one", len(sessions))
	}
	for i, sess := range results {
		if sess == nil || sess.ID != sessions[0].ID {
			t.Fatalf("caller %d returned session %v, want %s", i, sess, sessions[0].ID)
		}
	}
	runs, err := svc.ListRuns(context.Background(), principal, instance.ID, ListRunsInput{})
	if err != nil || len(runs) != 1 {
		t.Fatalf("ListRuns = %#v, %v; want one run", runs, err)
	}
	events, err := svc.ListRunEvents(context.Background(), principal, runs[0].ID, 0, 20)
	if err != nil || len(events) < 2 || events[0].Type != "run.started" || events[len(events)-1].Type != "run.completed" {
		t.Fatalf("run events = %#v, %v; want started and completed", events, err)
	}
}
