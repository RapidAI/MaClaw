package agent

import (
	"context"
	"sync"
	"testing"
	"time"
)

type mockCheck struct {
	name   string
	alerts []HeartbeatAlert
}

func (m *mockCheck) Name() string                          { return m.name }
func (m *mockCheck) Check(ctx context.Context) []HeartbeatAlert { return m.alerts }

func TestHeartbeatEngine_RunOnce(t *testing.T) {
	engine := NewHeartbeatEngine(time.Minute, func(alerts []HeartbeatAlert) {})
	engine.AddCheck(&mockCheck{
		name: "test",
		alerts: []HeartbeatAlert{
			{ID: "a1", Priority: "high", Title: "Task done", Source: "test"},
		},
	})

	alerts := engine.RunOnce(context.Background())
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}
	if alerts[0].Title != "Task done" {
		t.Errorf("wrong title: %s", alerts[0].Title)
	}
}

func TestHeartbeatEngine_Dedup(t *testing.T) {
	engine := NewHeartbeatEngine(time.Minute, nil)
	engine.AddCheck(&mockCheck{
		name: "test",
		alerts: []HeartbeatAlert{
			{ID: "dup1", Priority: "medium", Title: "Same alert"},
		},
	})

	// First run — should produce alert
	alerts1 := engine.RunOnce(context.Background())
	if len(alerts1) != 1 {
		t.Fatalf("first run: expected 1, got %d", len(alerts1))
	}

	// Second run — same ID should be deduped
	alerts2 := engine.RunOnce(context.Background())
	if len(alerts2) != 0 {
		t.Fatalf("second run: expected 0 (dedup), got %d", len(alerts2))
	}
}

func TestHeartbeatEngine_MultipleChecks(t *testing.T) {
	engine := NewHeartbeatEngine(time.Minute, nil)
	engine.AddCheck(&mockCheck{
		name:   "ssh",
		alerts: []HeartbeatAlert{{ID: "ssh1", Title: "SSH task done"}},
	})
	engine.AddCheck(&mockCheck{
		name:   "bg",
		alerts: []HeartbeatAlert{{ID: "bg1", Title: "Build finished"}},
	})

	alerts := engine.RunOnce(context.Background())
	if len(alerts) != 2 {
		t.Fatalf("expected 2 alerts from 2 checks, got %d", len(alerts))
	}
}

func TestHeartbeatEngine_NoAlerts(t *testing.T) {
	engine := NewHeartbeatEngine(time.Minute, nil)
	engine.AddCheck(&mockCheck{name: "empty", alerts: nil})

	alerts := engine.RunOnce(context.Background())
	if len(alerts) != 0 {
		t.Fatalf("expected 0 alerts, got %d", len(alerts))
	}
}

func TestHeartbeatEngine_StartStop(t *testing.T) {
	var mu sync.Mutex
	callCount := 0
	engine := NewHeartbeatEngine(50*time.Millisecond, func(alerts []HeartbeatAlert) {
		mu.Lock()
		callCount++
		mu.Unlock()
	})
	engine.AddCheck(&mockCheck{
		name:   "tick",
		alerts: []HeartbeatAlert{{ID: "t" + time.Now().String(), Title: "tick"}},
	})

	engine.Start()
	if !engine.IsRunning() {
		t.Error("should be running after Start")
	}

	time.Sleep(200 * time.Millisecond)
	engine.Stop()

	if engine.IsRunning() {
		t.Error("should not be running after Stop")
	}
}

func TestHeartbeatEngine_NilSafe(t *testing.T) {
	var engine *HeartbeatEngine
	engine.AddCheck(&mockCheck{name: "x"})
	engine.Start()
	engine.Stop()
	alerts := engine.RunOnce(context.Background())
	if alerts != nil {
		t.Error("nil engine should return nil")
	}
}
