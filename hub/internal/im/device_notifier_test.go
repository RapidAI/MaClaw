package im

import (
	"testing"
	"time"
)

func TestDeviceNotifier_OnlyNotifiesActiveUsers(t *testing.T) {
	dn := &DeviceNotifier{
		debounce:    make(map[string]*debounceEntry),
		activeUsers: make(map[string]activeUserInfo),
	}

	// User not active — notification should be silently dropped.
	dn.NotifyDeviceOnline("user1", "m1", "MacBook")

	dn.mu.Lock()
	count := len(dn.debounce)
	dn.mu.Unlock()
	if count != 0 {
		t.Fatalf("expected 0 debounce entries for inactive user, got %d", count)
	}
}

func TestDeviceNotifier_DebouncesCancelsPrevious(t *testing.T) {
	dn := &DeviceNotifier{
		debounce:    make(map[string]*debounceEntry),
		activeUsers: make(map[string]activeUserInfo),
	}
	dn.MarkUserActive("user1", "feishu", "uid1")

	// Online then offline quickly — should cancel the online notification.
	dn.NotifyDeviceOnline("user1", "m1", "MacBook")
	dn.NotifyDeviceOffline("user1", "m1", "MacBook")

	dn.mu.Lock()
	entry := dn.debounce["m1"]
	dn.mu.Unlock()

	if entry == nil {
		t.Fatal("expected debounce entry")
	}
	if entry.online {
		t.Fatal("expected final state to be offline (last write wins)")
	}

	// Clean up timer.
	entry.timer.Stop()
}

func TestDeviceNotifier_MarkUserActive(t *testing.T) {
	dn := &DeviceNotifier{
		debounce:    make(map[string]*debounceEntry),
		activeUsers: make(map[string]activeUserInfo),
	}

	dn.MarkUserActive("user1", "feishu", "uid1")

	dn.mu.Lock()
	info, ok := dn.activeUsers["user1"]
	dn.mu.Unlock()

	if !ok {
		t.Fatal("expected user to be active")
	}
	if info.PlatformName != "feishu" {
		t.Fatalf("expected feishu, got %s", info.PlatformName)
	}
}

func TestDeviceNotifier_DebounceSchedulesTimer(t *testing.T) {
	dn := &DeviceNotifier{
		debounce:    make(map[string]*debounceEntry),
		activeUsers: make(map[string]activeUserInfo),
	}
	dn.MarkUserActive("user1", "feishu", "uid1")

	dn.NotifyDeviceOnline("user1", "m1", "MacBook")

	dn.mu.Lock()
	entry := dn.debounce["m1"]
	dn.mu.Unlock()

	if entry == nil {
		t.Fatal("expected debounce entry to be scheduled")
	}
	if !entry.online {
		t.Fatal("expected online=true")
	}

	// Clean up.
	entry.timer.Stop()
}

func TestDeviceNotifier_RapidFlapping(t *testing.T) {
	dn := &DeviceNotifier{
		debounce:    make(map[string]*debounceEntry),
		activeUsers: make(map[string]activeUserInfo),
	}
	dn.MarkUserActive("user1", "feishu", "uid1")

	// Simulate rapid flapping: online → offline → online → offline
	for i := 0; i < 10; i++ {
		if i%2 == 0 {
			dn.NotifyDeviceOnline("user1", "m1", "MacBook")
		} else {
			dn.NotifyDeviceOffline("user1", "m1", "MacBook")
		}
		time.Sleep(1 * time.Millisecond)
	}

	dn.mu.Lock()
	count := len(dn.debounce)
	entry := dn.debounce["m1"]
	dn.mu.Unlock()

	// Should only have 1 pending entry (last state wins).
	if count != 1 {
		t.Fatalf("expected 1 debounce entry, got %d", count)
	}
	if entry.online {
		t.Fatal("expected final state offline (10 iterations, last is offline)")
	}

	entry.timer.Stop()
}
