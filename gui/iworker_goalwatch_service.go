package main

import (
	"context"
	"os"
	"sync"
	"time"
)

const defaultIWorkerGoalWatchInterval = 60 * time.Second

type iworkerHeartbeatSender interface {
	SendHeartbeatContext(ctx context.Context, req IWorkerCenterHeartbeatRequest) (IWorkerCenterHeartbeatResult, error)
}

type IWorkerGoalWatchService struct {
	runner      *IWorkerGoalWatchRunner
	heartbeater iworkerHeartbeatSender
	interval    time.Duration
	startedAt   time.Time
	hostID      string
	processID   int

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	done    chan struct{}
}

type IWorkerGoalWatchServiceConfig struct {
	Runner      *IWorkerGoalWatchRunner
	Heartbeater iworkerHeartbeatSender
	Interval    time.Duration
}

func NewIWorkerGoalWatchService(cfg IWorkerGoalWatchServiceConfig) *IWorkerGoalWatchService {
	interval := cfg.Interval
	if interval <= 0 {
		interval = defaultIWorkerGoalWatchInterval
	}
	hostID, _ := os.Hostname()
	return &IWorkerGoalWatchService{runner: cfg.Runner, heartbeater: cfg.Heartbeater, interval: interval, startedAt: time.Now().UTC(), hostID: hostID, processID: os.Getpid()}
}

func (s *IWorkerGoalWatchService) Start(ctx context.Context) bool {
	if s == nil || s.runner == nil {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return false
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	s.running = true
	s.cancel = cancel
	s.done = done
	s.mu.Unlock()

	go s.loop(runCtx, done)
	return true
}

func (s *IWorkerGoalWatchService) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	cancel := s.cancel
	done := s.done
	s.running = false
	s.cancel = nil
	s.done = nil
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func (s *IWorkerGoalWatchService) RunNow(ctx context.Context) IWorkerGoalWatchRunStatus {
	if s == nil || s.runner == nil {
		return IWorkerGoalWatchRunStatus{SkippedReason: "goalwatch_runner_not_configured"}
	}
	s.sendWatcherHeartbeat(ctx)
	return s.runner.RunOnce(ctx)
}

func (s *IWorkerGoalWatchService) Status() IWorkerGoalWatchRunStatus {
	if s == nil || s.runner == nil {
		return IWorkerGoalWatchRunStatus{SkippedReason: "goalwatch_runner_not_configured"}
	}
	status := s.runner.Status()
	s.mu.Lock()
	status.Running = status.Running || s.running
	s.mu.Unlock()
	return status
}

func (s *IWorkerGoalWatchService) sendWatcherHeartbeat(ctx context.Context) {
	if s == nil || s.heartbeater == nil {
		return
	}
	_, _ = s.heartbeater.SendHeartbeatContext(ctx, IWorkerCenterHeartbeatRequest{
		Role:            "watcher",
		Status:          "online",
		Capabilities:    []string{"goalwatch_pull", "workflow_recover", "local_cache"},
		MemoryAuthority: "iWorkerCenter",
		LocalCacheMode:  "cache_only",
		HostID:          s.hostID,
		ProcessID:       s.processID,
		StartedAt:       s.startedAt.Format(time.RFC3339),
	})
}

func (s *IWorkerGoalWatchService) loop(ctx context.Context, done chan struct{}) {
	defer close(done)
	s.sendWatcherHeartbeat(ctx)
	s.runner.RunOnce(ctx)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sendWatcherHeartbeat(ctx)
			s.runner.RunOnce(ctx)
		}
	}
}
