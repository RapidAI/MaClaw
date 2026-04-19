package main

import (
	"context"
	"time"
)

type MockAppManagers struct {
	*AppManagers
}

func NewMockAppManagers() *MockAppManagers {
	managers := &MockAppManagers{AppManagers: NewAppManagers(context.Background())}
	_ = managers.InitializeAll()
	return managers
}

type HealthChecker struct {
	managers interface{ HealthCheck() map[string]bool }
	interval time.Duration
	timeout  time.Duration
}

func NewHealthChecker(managers interface{ HealthCheck() map[string]bool }, interval, timeout time.Duration) *HealthChecker {
	return &HealthChecker{managers: managers, interval: interval, timeout: timeout}
}

func (h *HealthChecker) CheckHealth(ctx context.Context) map[string]bool {
	if h == nil || h.managers == nil {
		return map[string]bool{}
	}
	return h.managers.HealthCheck()
}

func (h *HealthChecker) StartHealthChecking(ctx context.Context) <-chan map[string]bool {
	out := make(chan map[string]bool)
	go func() {
		defer close(out)
		interval := h.interval
		if interval <= 0 {
			interval = 100 * time.Millisecond
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case out <- h.CheckHealth(ctx):
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return out
}
