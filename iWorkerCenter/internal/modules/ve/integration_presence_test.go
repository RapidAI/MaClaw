package ve

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestIntegration_OnlineStatus tests the full online status lifecycle:
// WebSocket connect → online → disconnect → offline → reconnect → online.
func TestIntegration_OnlineStatus(t *testing.T) {
	tmpDir := t.TempDir()
	keyMat := []byte("test-key-material-32-bytes-long!")

	qs := NewQuotaStore(keyMat, "hub-presence-test", tmpDir+"/quota.enc")
	_ = qs.SaveQuota(10)
	registry := NewRegistry(qs, "")
	presence := NewPresenceManager()

	// Register and approve VE
	ve, _ := registry.Register(VERegistrationRequest{
		OwnerMachineID: "machine-ve-1",
		Name:           "Status Test VE",
		SkillDesc:      "Testing presence",
		AccessPolicy:   PolicyPublic,
	})
	_ = registry.Approve(ve.ID)

	// Initially offline
	if presence.IsOnline(ve.ID) {
		t.Fatal("VE should be offline initially")
	}
	if presence.GetStatus(ve.ID) != "offline" {
		t.Fatalf("expected status=offline, got %s", presence.GetStatus(ve.ID))
	}

	// Track status changes
	var statusChanges []struct {
		veID   string
		status string
	}
	var mu sync.Mutex
	presence.SetOnStatusChange(func(veID, status string) {
		mu.Lock()
		statusChanges = append(statusChanges, struct {
			veID   string
			status string
		}{veID, status})
		mu.Unlock()
	})

	// Step 1: WebSocket connect → online
	presence.OnWebSocketConnect(ve.ID, "machine-ve-1")
	if !presence.IsOnline(ve.ID) {
		t.Fatal("VE should be online after connect")
	}

	time.Sleep(50 * time.Millisecond) // wait for async callback
	mu.Lock()
	if len(statusChanges) != 1 || statusChanges[0].status != "online" {
		t.Fatalf("expected 1 status change to online, got %v", statusChanges)
	}
	mu.Unlock()

	// Step 2: WebSocket disconnect → offline (single instance)
	presence.OnWebSocketDisconnect(ve.ID, "machine-ve-1")
	if presence.IsOnline(ve.ID) {
		t.Fatal("VE should be offline after disconnect")
	}

	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	if len(statusChanges) != 2 || statusChanges[1].status != "offline" {
		t.Fatalf("expected 2nd status change to offline, got %v", statusChanges)
	}
	mu.Unlock()

	// Step 3: Reconnect → online again
	presence.OnWebSocketConnect(ve.ID, "machine-ve-1")
	if !presence.IsOnline(ve.ID) {
		t.Fatal("VE should be online after reconnect")
	}

	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	if len(statusChanges) != 3 || statusChanges[2].status != "online" {
		t.Fatalf("expected 3rd status change to online, got %v", statusChanges)
	}
	mu.Unlock()
}

// TestIntegration_OnlineStatus_MultiInstance tests multi-instance presence:
// 2 machines online → 1 disconnects → VE still online → last disconnects → offline.
func TestIntegration_OnlineStatus_MultiInstance(t *testing.T) {
	presence := NewPresenceManager()
	veID := "ve-multi-instance"

	// Connect 2 instances
	presence.OnWebSocketConnect(veID, "machine-A")
	presence.OnWebSocketConnect(veID, "machine-B")

	if !presence.IsOnline(veID) {
		t.Fatal("VE should be online with 2 instances")
	}
	if presence.InstanceCount(veID) != 2 {
		t.Fatalf("expected 2 instances, got %d", presence.InstanceCount(veID))
	}

	// Disconnect one → still online
	presence.OnWebSocketDisconnect(veID, "machine-A")
	if !presence.IsOnline(veID) {
		t.Fatal("VE should still be online with 1 remaining instance")
	}
	if presence.InstanceCount(veID) != 1 {
		t.Fatalf("expected 1 instance, got %d", presence.InstanceCount(veID))
	}

	// Disconnect last → offline
	presence.OnWebSocketDisconnect(veID, "machine-B")
	if presence.IsOnline(veID) {
		t.Fatal("VE should be offline after all instances disconnect")
	}
	if presence.InstanceCount(veID) != 0 {
		t.Fatalf("expected 0 instances, got %d", presence.InstanceCount(veID))
	}
}

// TestIntegration_OnlineStatus_HeartbeatMonitor tests the heartbeat monitor:
// missed heartbeats → machine removed → VE goes offline.
func TestIntegration_OnlineStatus_HeartbeatMonitor(t *testing.T) {
	presence := NewPresenceManager()
	// Use short interval for testing
	presence.heartbeatInterval = 50 * time.Millisecond
	presence.missThreshold = 2

	veID := "ve-heartbeat-test"

	// Record initial heartbeat → online
	presence.RecordHeartbeat(veID, "machine-1")
	if !presence.IsOnline(veID) {
		t.Fatal("VE should be online after heartbeat")
	}

	// Track status changes
	var offlineDetected bool
	var mu sync.Mutex
	presence.SetOnStatusChange(func(id, status string) {
		if id == veID && status == "offline" {
			mu.Lock()
			offlineDetected = true
			mu.Unlock()
		}
	})

	// Start monitor
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	presence.StartMonitor(ctx)

	// Wait for 2 missed heartbeats (2 * 50ms = 100ms) + some buffer
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	detected := offlineDetected
	mu.Unlock()

	if !detected {
		t.Fatal("VE should go offline after missing heartbeats")
	}
	if presence.IsOnline(veID) {
		t.Fatal("VE should be offline after heartbeat timeout")
	}

	// Record heartbeat again → back online
	presence.RecordHeartbeat(veID, "machine-1")
	if !presence.IsOnline(veID) {
		t.Fatal("VE should be back online after new heartbeat")
	}
}
