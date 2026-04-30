package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestIWorkerGoalWatchRunnerRunsRecoverer(t *testing.T) {
	fake := &fakeGoalWatchRecoverer{summary: IWorkerCenterRecoverySummary{Checked: 2, Recovered: 1, Skipped: 1}}
	runner := NewIWorkerGoalWatchRunner(IWorkerGoalWatchRunnerConfig{Recoverer: fake, Limit: 7, Note: "tick", Timeout: time.Second})

	status := runner.RunOnce(context.Background())
	if status.Summary.Checked != 2 || status.Summary.Recovered != 1 || status.Summary.Skipped != 1 || status.Running {
		t.Fatalf("status = %+v", status)
	}
	if fake.limit != 7 || fake.note != "tick" || fake.calls != 1 {
		t.Fatalf("fake = %+v", fake)
	}
}

func TestIWorkerGoalWatchRunnerSkipsOverlappingRun(t *testing.T) {
	fake := newBlockingGoalWatchRecoverer()
	runner := NewIWorkerGoalWatchRunner(IWorkerGoalWatchRunnerConfig{Recoverer: fake, Timeout: time.Second})

	done := make(chan IWorkerGoalWatchRunStatus, 1)
	go func() { done <- runner.RunOnce(context.Background()) }()
	<-fake.entered

	second := runner.RunOnce(context.Background())
	if second.SkippedReason != "previous_goalwatch_run_still_active" || !second.Running {
		t.Fatalf("second status = %+v", second)
	}
	fake.release()

	first := <-done
	if first.Summary.Recovered != 1 || len(first.Summary.Errors) != 0 {
		t.Fatalf("first status = %+v", first)
	}
}

func TestIWorkerGoalWatchRunnerTimeoutReleasesSingleFlight(t *testing.T) {
	fake := &fakeGoalWatchRecoverer{waitForContext: true}
	runner := NewIWorkerGoalWatchRunner(IWorkerGoalWatchRunnerConfig{Recoverer: fake, Timeout: 10 * time.Millisecond})

	status := runner.RunOnce(context.Background())
	if status.Running {
		t.Fatalf("status should not be running after timeout: %+v", status)
	}
	if len(status.Summary.Errors) == 0 || !strings.Contains(status.Summary.Errors[0], "deadline") {
		t.Fatalf("expected deadline error, got %+v", status.Summary.Errors)
	}

	fake.waitForContext = false
	fake.summary = IWorkerCenterRecoverySummary{Checked: 1, Recovered: 1}
	status = runner.RunOnce(context.Background())
	if status.Summary.Recovered != 1 {
		t.Fatalf("runner did not recover after timed-out run: %+v", status)
	}
}

type fakeGoalWatchRecoverer struct {
	mu             sync.Mutex
	calls          int
	limit          int
	note           string
	summary        IWorkerCenterRecoverySummary
	waitForContext bool
}

func (f *fakeGoalWatchRecoverer) RecoverEligibleGoalWatchPushesContext(ctx context.Context, limit int, note string) IWorkerCenterRecoverySummary {
	f.mu.Lock()
	f.calls++
	f.limit = limit
	f.note = note
	wait := f.waitForContext
	summary := f.summary
	f.mu.Unlock()
	if wait {
		<-ctx.Done()
		return IWorkerCenterRecoverySummary{Errors: []string{ctx.Err().Error()}}
	}
	return summary
}

type blockingGoalWatchRecoverer struct {
	entered chan struct{}
	block   chan struct{}
	once    sync.Once
}

func newBlockingGoalWatchRecoverer() *blockingGoalWatchRecoverer {
	return &blockingGoalWatchRecoverer{entered: make(chan struct{}), block: make(chan struct{})}
}

func (f *blockingGoalWatchRecoverer) RecoverEligibleGoalWatchPushesContext(ctx context.Context, limit int, note string) IWorkerCenterRecoverySummary {
	f.once.Do(func() { close(f.entered) })
	select {
	case <-f.block:
		return IWorkerCenterRecoverySummary{Checked: 1, Recovered: 1}
	case <-ctx.Done():
		return IWorkerCenterRecoverySummary{Errors: []string{ctx.Err().Error()}}
	}
}

func (f *blockingGoalWatchRecoverer) release() {
	close(f.block)
}
