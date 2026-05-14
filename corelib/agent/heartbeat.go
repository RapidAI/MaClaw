package agent

// Heartbeat: proactive notification engine.
// Inspired by OpenHuman's heartbeat/ module — periodically checks for
// conditions that warrant proactive user notification, rather than waiting
// for the user to ask.
//
// Use cases:
// - SSH background task completed/failed
// - Local background task completed/failed
// - Scheduled reminder triggered
// - Long-running operation finished
// - Daily cost budget warning
//
// The engine runs checks at a configurable interval and emits alerts
// through a callback. The GUI/TUI/IM layer decides how to deliver
// (toast, system notification, IM message push).

import (
	"context"
	"log"
	"sync"
	"time"
)

// HeartbeatAlert represents a proactive notification to the user.
type HeartbeatAlert struct {
	ID        string    // unique alert ID (for dedup — MUST include instance-specific info like task_id)
	Priority  string    // "high" | "medium" | "low"
	Title     string    // short title
	Body      string    // detail text
	Source    string    // which check produced this
	Timestamp time.Time
}

// HeartbeatCheck is a pluggable check that runs periodically.
type HeartbeatCheck interface {
	Name() string
	Check(ctx context.Context) []HeartbeatAlert
}

// HeartbeatNotifier is called when alerts are produced.
type HeartbeatNotifier func(alerts []HeartbeatAlert)

// HeartbeatEngine runs periodic checks and emits alerts.
type HeartbeatEngine struct {
	mu       sync.Mutex
	checks   []HeartbeatCheck
	notifier HeartbeatNotifier
	interval time.Duration
	stopCh   chan struct{}
	running  bool

	// Dedup: track recently emitted alert IDs to avoid spamming.
	recentAlerts map[string]time.Time
	dedupWindow  time.Duration
}

// NewHeartbeatEngine creates an engine with the given interval and notifier.
func NewHeartbeatEngine(interval time.Duration, notifier HeartbeatNotifier) *HeartbeatEngine {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	return &HeartbeatEngine{
		interval:     interval,
		notifier:     notifier,
		stopCh:       make(chan struct{}),
		recentAlerts: make(map[string]time.Time),
		dedupWindow:  2 * time.Minute,
	}
}

// AddCheck registers a check to run on each heartbeat tick.
func (e *HeartbeatEngine) AddCheck(check HeartbeatCheck) {
	if e == nil || check == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.checks = append(e.checks, check)
}

// Start begins the periodic check loop in a goroutine.
func (e *HeartbeatEngine) Start() {
	if e == nil {
		return
	}
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return
	}
	e.running = true
	e.mu.Unlock()

	go e.loop()
	log.Printf("[heartbeat] started with interval=%s, checks=%d", e.interval, len(e.checks))
}

// Stop terminates the heartbeat loop.
func (e *HeartbeatEngine) Stop() {
	if e == nil {
		return
	}
	e.mu.Lock()
	if !e.running {
		e.mu.Unlock()
		return
	}
	e.running = false
	e.mu.Unlock()
	close(e.stopCh)
	log.Printf("[heartbeat] stopped")
}

// IsRunning returns whether the engine is active.
func (e *HeartbeatEngine) IsRunning() bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.running
}

// RunOnce executes all checks once (for testing or manual trigger).
func (e *HeartbeatEngine) RunOnce(ctx context.Context) []HeartbeatAlert {
	if e == nil {
		return nil
	}
	return e.tick(ctx)
}

func (e *HeartbeatEngine) loop() {
	ticker := time.NewTicker(e.interval)
	defer ticker.Stop()

	for {
		select {
		case <-e.stopCh:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			alerts := e.tick(ctx)
			cancel()
			if len(alerts) > 0 && e.notifier != nil {
				e.notifier(alerts)
			}
		}
	}
}

func (e *HeartbeatEngine) tick(ctx context.Context) []HeartbeatAlert {
	e.mu.Lock()
	checks := make([]HeartbeatCheck, len(e.checks))
	copy(checks, e.checks)
	e.mu.Unlock()

	var allAlerts []HeartbeatAlert
	for _, check := range checks {
		alerts := check.Check(ctx)
		for _, a := range alerts {
			if !e.isDuplicate(a) {
				allAlerts = append(allAlerts, a)
				e.markEmitted(a)
			}
		}
	}
	e.cleanupOldAlerts()
	return allAlerts
}

func (e *HeartbeatEngine) isDuplicate(a HeartbeatAlert) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if lastTime, ok := e.recentAlerts[a.ID]; ok {
		return time.Since(lastTime) < e.dedupWindow
	}
	return false
}

func (e *HeartbeatEngine) markEmitted(a HeartbeatAlert) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.recentAlerts[a.ID] = time.Now()
}

func (e *HeartbeatEngine) cleanupOldAlerts() {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := time.Now()
	for id, t := range e.recentAlerts {
		if now.Sub(t) > e.dedupWindow*2 {
			delete(e.recentAlerts, id)
		}
	}
}
