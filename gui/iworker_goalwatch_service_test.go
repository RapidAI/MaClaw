package main

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestIWorkerGoalWatchServiceStartRunsImmediatelyAndStops(t *testing.T) {
	fake := &fakeGoalWatchRecoverer{summary: IWorkerCenterRecoverySummary{Checked: 1, Recovered: 1}}
	runner := NewIWorkerGoalWatchRunner(IWorkerGoalWatchRunnerConfig{Recoverer: fake, Timeout: time.Second})
	service := NewIWorkerGoalWatchService(IWorkerGoalWatchServiceConfig{Runner: runner, Interval: time.Hour})

	if !service.Start(context.Background()) {
		t.Fatal("expected service to start")
	}
	waitForGoalWatchCalls(t, fake, 1)
	if service.Start(context.Background()) {
		t.Fatal("second start should be ignored")
	}
	service.Stop()
	if status := service.Status(); status.Running {
		t.Fatalf("service still running: %+v", status)
	}
}

func TestIWorkerGoalWatchServiceTicksUntilStopped(t *testing.T) {
	fake := &fakeGoalWatchRecoverer{summary: IWorkerCenterRecoverySummary{Checked: 1}}
	runner := NewIWorkerGoalWatchRunner(IWorkerGoalWatchRunnerConfig{Recoverer: fake, Timeout: time.Second})
	service := NewIWorkerGoalWatchService(IWorkerGoalWatchServiceConfig{Runner: runner, Interval: 10 * time.Millisecond})

	if !service.Start(context.Background()) {
		t.Fatal("expected service to start")
	}
	waitForGoalWatchCalls(t, fake, 2)
	service.Stop()
	calls := fakeCallCount(fake)
	time.Sleep(30 * time.Millisecond)
	if got := fakeCallCount(fake); got != calls {
		t.Fatalf("calls after stop = %d, want %d", got, calls)
	}
}

func TestIWorkerGoalWatchServiceRunNowWithoutRunner(t *testing.T) {
	service := NewIWorkerGoalWatchService(IWorkerGoalWatchServiceConfig{})
	status := service.RunNow(context.Background())
	if status.SkippedReason != "goalwatch_runner_not_configured" {
		t.Fatalf("status = %+v", status)
	}
}

func waitForGoalWatchCalls(t *testing.T, fake *fakeGoalWatchRecoverer, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if fakeCallCount(fake) >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("calls = %d, want at least %d", fakeCallCount(fake), want)
}

func fakeCallCount(fake *fakeGoalWatchRecoverer) int {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return fake.calls
}

func TestIWorkerGoalWatchServiceRunNowSendsHeartbeatBeforeRecovering(t *testing.T) {
	recoverer := &fakeGoalWatchRecoverer{summary: IWorkerCenterRecoverySummary{Checked: 1, Recovered: 1}}
	heartbeater := &fakeIWorkerHeartbeater{}
	runner := NewIWorkerGoalWatchRunner(IWorkerGoalWatchRunnerConfig{Recoverer: recoverer, Timeout: time.Second})
	service := NewIWorkerGoalWatchService(IWorkerGoalWatchServiceConfig{Runner: runner, Heartbeater: heartbeater, Interval: time.Hour})

	status := service.RunNow(context.Background())
	if status.Summary.Recovered != 1 {
		t.Fatalf("status = %+v", status)
	}
	reqs := heartbeater.requestsSnapshot()
	if len(reqs) != 1 {
		t.Fatalf("heartbeats = %d, want 1", len(reqs))
	}
	assertWatcherHeartbeat(t, reqs[0])
	if fakeCallCount(recoverer) != 1 {
		t.Fatalf("recover calls = %d, want 1", fakeCallCount(recoverer))
	}
}

func TestIWorkerGoalWatchServiceLoopSendsHeartbeatOnEachRun(t *testing.T) {
	recoverer := &fakeGoalWatchRecoverer{summary: IWorkerCenterRecoverySummary{Checked: 1}}
	heartbeater := &fakeIWorkerHeartbeater{}
	runner := NewIWorkerGoalWatchRunner(IWorkerGoalWatchRunnerConfig{Recoverer: recoverer, Timeout: time.Second})
	service := NewIWorkerGoalWatchService(IWorkerGoalWatchServiceConfig{Runner: runner, Heartbeater: heartbeater, Interval: 10 * time.Millisecond})

	if !service.Start(context.Background()) {
		t.Fatal("expected service to start")
	}
	waitForGoalWatchCalls(t, recoverer, 2)
	service.Stop()
	if got := len(heartbeater.requestsSnapshot()); got < 2 {
		t.Fatalf("heartbeats = %d, want at least 2", got)
	}
}

type fakeIWorkerHeartbeater struct {
	mu       sync.Mutex
	requests []IWorkerCenterHeartbeatRequest
}

func (f *fakeIWorkerHeartbeater) SendHeartbeatContext(ctx context.Context, req IWorkerCenterHeartbeatRequest) (IWorkerCenterHeartbeatResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)
	return IWorkerCenterHeartbeatResult{Instance: IWorkerCenterInstance{WorkerID: req.WorkerID, Role: req.Role, Status: req.Status}}, nil
}

func (f *fakeIWorkerHeartbeater) requestsSnapshot() []IWorkerCenterHeartbeatRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]IWorkerCenterHeartbeatRequest, len(f.requests))
	copy(out, f.requests)
	return out
}

func assertWatcherHeartbeat(t *testing.T, req IWorkerCenterHeartbeatRequest) {
	t.Helper()
	if req.Role != "watcher" || req.Status != "online" {
		t.Fatalf("heartbeat role/status = %+v", req)
	}
	if req.MemoryAuthority != "iWorkerCenter" || req.LocalCacheMode != "cache_only" {
		t.Fatalf("heartbeat memory/cache = %+v", req)
	}
	if req.ProcessID == 0 || req.StartedAt == "" {
		t.Fatalf("heartbeat runtime fields = %+v", req)
	}
}
