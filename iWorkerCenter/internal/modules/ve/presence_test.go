package ve

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestPresence_ConnectDisconnect(t *testing.T) {
	pm := NewPresenceManager()

	// Initially offline
	if pm.IsOnline("ve-1") {
		t.Error("VE should be offline initially")
	}

	// Connect → online
	pm.OnWebSocketConnect("ve-1", "machine-A")
	if !pm.IsOnline("ve-1") {
		t.Error("VE should be online after connect")
	}

	// Disconnect → offline
	pm.OnWebSocketDisconnect("ve-1", "machine-A")
	if pm.IsOnline("ve-1") {
		t.Error("VE should be offline after disconnect")
	}
}

func TestPresence_MultiInstance(t *testing.T) {
	pm := NewPresenceManager()

	// Two instances connect
	pm.OnWebSocketConnect("ve-1", "machine-A")
	pm.OnWebSocketConnect("ve-1", "machine-B")

	if pm.InstanceCount("ve-1") != 2 {
		t.Errorf("InstanceCount = %d, want 2", pm.InstanceCount("ve-1"))
	}

	// Disconnect one — still online
	pm.OnWebSocketDisconnect("ve-1", "machine-A")
	if !pm.IsOnline("ve-1") {
		t.Error("VE should still be online with one instance remaining")
	}
	if pm.InstanceCount("ve-1") != 1 {
		t.Errorf("InstanceCount = %d, want 1", pm.InstanceCount("ve-1"))
	}

	// Disconnect last — offline
	pm.OnWebSocketDisconnect("ve-1", "machine-B")
	if pm.IsOnline("ve-1") {
		t.Error("VE should be offline after all instances disconnect")
	}
}

func TestPresence_HeartbeatTimeout(t *testing.T) {
	pm := NewPresenceManager()
	pm.heartbeatInterval = 50 * time.Millisecond
	pm.missThreshold = 2

	// Connect
	pm.OnWebSocketConnect("ve-1", "machine-A")
	if !pm.IsOnline("ve-1") {
		t.Fatal("VE should be online")
	}

	// Wait for 2 missed heartbeats (50ms * 2 = 100ms)
	time.Sleep(120 * time.Millisecond)

	// Manual check (simulating the monitor tick)
	pm.checkHeartbeats()

	if pm.IsOnline("ve-1") {
		t.Error("VE should be offline after heartbeat timeout")
	}
}

func TestPresence_HeartbeatKeepsAlive(t *testing.T) {
	pm := NewPresenceManager()
	pm.heartbeatInterval = 50 * time.Millisecond
	pm.missThreshold = 2

	pm.OnWebSocketConnect("ve-1", "machine-A")

	// Send heartbeats to keep alive
	for i := 0; i < 5; i++ {
		time.Sleep(30 * time.Millisecond)
		pm.RecordHeartbeat("ve-1", "machine-A")
	}

	pm.checkHeartbeats()

	if !pm.IsOnline("ve-1") {
		t.Error("VE should still be online with regular heartbeats")
	}
}

func TestPresence_StatusChangeCallback(t *testing.T) {
	pm := NewPresenceManager()

	var changes []string
	var changeCount int32
	pm.SetOnStatusChange(func(veID, status string) {
		atomic.AddInt32(&changeCount, 1)
		changes = append(changes, veID+":"+status)
	})

	pm.OnWebSocketConnect("ve-1", "machine-A")
	time.Sleep(10 * time.Millisecond) // let goroutine run

	pm.OnWebSocketDisconnect("ve-1", "machine-A")
	time.Sleep(10 * time.Millisecond)

	count := atomic.LoadInt32(&changeCount)
	if count != 2 {
		t.Errorf("expected 2 status change callbacks, got %d", count)
	}
}

func TestPresence_MonitorContext(t *testing.T) {
	pm := NewPresenceManager()
	pm.heartbeatInterval = 20 * time.Millisecond
	pm.missThreshold = 2

	ctx, cancel := context.WithCancel(context.Background())
	pm.StartMonitor(ctx)

	pm.OnWebSocketConnect("ve-1", "machine-A")

	// Wait for timeout
	time.Sleep(80 * time.Millisecond)

	if pm.IsOnline("ve-1") {
		t.Error("VE should be offline after monitor detects timeout")
	}

	cancel() // stop monitor
}

func TestPresence_GetStatus(t *testing.T) {
	pm := NewPresenceManager()

	if s := pm.GetStatus("nonexistent"); s != "offline" {
		t.Errorf("GetStatus(nonexistent) = %q, want offline", s)
	}

	pm.OnWebSocketConnect("ve-1", "m1")
	if s := pm.GetStatus("ve-1"); s != "online" {
		t.Errorf("GetStatus(ve-1) = %q, want online", s)
	}
}
