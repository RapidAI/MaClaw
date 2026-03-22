package im

import (
	"fmt"
	"strings"
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

// ---------------------------------------------------------------------------
// Helper: create a DeviceNotifier with a real Coordinator for space state tests
// ---------------------------------------------------------------------------

func newTestNotifierWithCoordinator() (*DeviceNotifier, *Coordinator) {
	df := &mockDeviceFinder{}
	router := NewMessageRouter(df)
	coord := NewCoordinator(router, df, func() *HubLLMConfig { return nil })
	dn := NewDeviceNotifier(nil, coord)
	return dn, coord
}

// ---------------------------------------------------------------------------
// Property 1: 私聊目标掉线自动返回大厅
// Tag: Feature: maclaw-auto-lobby-return, Property 1: 私聊目标掉线自动返回大厅
// ---------------------------------------------------------------------------

func TestProperty1_PrivateTargetOffline_ReturnsToLobby(t *testing.T) {
	for i := 0; i < 100; i++ {
		userID := fmt.Sprintf("user-%d", i)
		machineID := fmt.Sprintf("machine-%d", i)
		name := fmt.Sprintf("Device-%d", i)

		dn, coord := newTestNotifierWithCoordinator()
		ss := coord.SpaceStateStore()

		// Setup: user in private mode with this machine
		ss.EnterPrivate(userID, machineID, name)
		coord.router.mu.Lock()
		coord.router.selectedMachine[userID] = machineID
		coord.router.mu.Unlock()

		entry := &debounceEntry{userID: userID, machineID: machineID, name: name}
		msg := dn.buildOfflineMessage(entry)

		// Verify: state should be lobby
		state := ss.GetOrCreate(userID)
		if state.State != SpaceLobby {
			t.Fatalf("iter %d: expected SpaceLobby, got %s", i, state.State)
		}

		// Verify: selectedMachine should be cleared
		selected, ok := coord.router.GetSelectedMachine(userID)
		if ok && selected != "" && selected != broadcastMachineID {
			t.Fatalf("iter %d: expected selectedMachine cleared, got %s", i, selected)
		}

		// Verify: message contains auto-return text
		if !strings.Contains(msg, "已自动返回大厅") {
			t.Fatalf("iter %d: expected auto-return message, got %q", i, msg)
		}

		coord.router.Stop()
	}
}

// ---------------------------------------------------------------------------
// Property 2: 自动返回大厅通知格式
// Tag: Feature: maclaw-auto-lobby-return, Property 2: 自动返回大厅通知格式
// ---------------------------------------------------------------------------

func TestProperty2_AutoReturnNotificationFormat(t *testing.T) {
	names := []string{
		"MacBook", "工作站", "iPad-Pro", "设备 A", "My Device",
		"テスト", "Über-PC", "机器人_01", "dev@home", "🖥️ Server",
	}
	for iter := 0; iter < 100; iter++ {
		name := names[iter%len(names)] + fmt.Sprintf("-%d", iter)
		userID := fmt.Sprintf("user-%d", iter)
		machineID := fmt.Sprintf("m-%d", iter)

		dn, coord := newTestNotifierWithCoordinator()
		ss := coord.SpaceStateStore()
		ss.EnterPrivate(userID, machineID, name)

		entry := &debounceEntry{userID: userID, machineID: machineID, name: name}
		msg := dn.buildOfflineMessage(entry)

		// Must contain device name
		if !strings.Contains(msg, name) {
			t.Fatalf("iter %d: message should contain device name %q, got %q", iter, name, msg)
		}
		// Must contain auto-return text
		if !strings.Contains(msg, "已自动返回大厅") {
			t.Fatalf("iter %d: message should contain '已自动返回大厅', got %q", iter, msg)
		}
		// Must NOT contain /call
		if strings.Contains(msg, "/call") {
			t.Fatalf("iter %d: message should not contain '/call', got %q", iter, msg)
		}

		coord.router.Stop()
	}
}

// ---------------------------------------------------------------------------
// Property 3: 会议参与者掉线移除
// Tag: Feature: maclaw-auto-lobby-return, Property 3: 会议参与者掉线移除
// ---------------------------------------------------------------------------

func TestProperty3_MeetingParticipantRemoved(t *testing.T) {
	for iter := 0; iter < 100; iter++ {
		userID := fmt.Sprintf("user-%d", iter)
		numParticipants := (iter % 5) + 2 // 2..6 participants
		dropIdx := iter % numParticipants

		participants := make([]string, numParticipants)
		for j := 0; j < numParticipants; j++ {
			participants[j] = fmt.Sprintf("m-%d-%d", iter, j)
		}
		dropMachine := participants[dropIdx]

		dn, coord := newTestNotifierWithCoordinator()
		ss := coord.SpaceStateStore()
		ss.EnterMeeting(userID, "topic", participants)

		entry := &debounceEntry{userID: userID, machineID: dropMachine, name: "Dev"}
		dn.buildOfflineMessage(entry)

		// Verify: dropped machine not in participants
		state := ss.GetOrCreate(userID)
		for _, p := range state.Participants {
			if p == dropMachine {
				t.Fatalf("iter %d: machine %s should have been removed from participants", iter, dropMachine)
			}
		}

		coord.router.Stop()
	}
}

// ---------------------------------------------------------------------------
// Property 4: 所有会议参与者掉线自动结束会议
// Tag: Feature: maclaw-auto-lobby-return, Property 4: 所有会议参与者掉线自动结束会议
// ---------------------------------------------------------------------------

func TestProperty4_AllParticipantsOffline_MeetingEnds(t *testing.T) {
	for iter := 0; iter < 100; iter++ {
		userID := fmt.Sprintf("user-%d", iter)
		numParticipants := (iter % 4) + 1 // 1..4

		participants := make([]string, numParticipants)
		for j := 0; j < numParticipants; j++ {
			participants[j] = fmt.Sprintf("m-%d-%d", iter, j)
		}

		dn, coord := newTestNotifierWithCoordinator()
		ss := coord.SpaceStateStore()
		ss.EnterMeeting(userID, "topic", participants)

		// Drop all participants one by one
		for _, machineID := range participants {
			entry := &debounceEntry{userID: userID, machineID: machineID, name: "Dev"}
			dn.buildOfflineMessage(entry)
		}

		// Verify: state should be lobby
		state := ss.GetOrCreate(userID)
		if state.State != SpaceLobby {
			t.Fatalf("iter %d: expected SpaceLobby after all participants offline, got %s", iter, state.State)
		}

		coord.router.Stop()
	}
}

// ---------------------------------------------------------------------------
// Property 5: 非相关设备掉线简洁通知
// Tag: Feature: maclaw-auto-lobby-return, Property 5: 非相关设备掉线简洁通知
// ---------------------------------------------------------------------------

func TestProperty5_UnrelatedDeviceOffline_SimpleNotification(t *testing.T) {
	type scenario struct {
		setupState func(ss *spaceStateStore, userID string)
	}
	scenarios := []scenario{
		{func(ss *spaceStateStore, uid string) {}},                                                          // lobby
		{func(ss *spaceStateStore, uid string) { ss.EnterPrivate(uid, "other-machine", "Other") }},         // private, different target
		{func(ss *spaceStateStore, uid string) { ss.EnterMeeting(uid, "topic", []string{"other-machine"}) }}, // meeting, not a participant
	}

	for iter := 0; iter < 100; iter++ {
		sc := scenarios[iter%len(scenarios)]
		userID := fmt.Sprintf("user-%d", iter)
		unrelatedMachine := fmt.Sprintf("unrelated-%d", iter)
		name := fmt.Sprintf("Device-%d", iter)

		dn, coord := newTestNotifierWithCoordinator()
		ss := coord.SpaceStateStore()
		sc.setupState(ss, userID)

		entry := &debounceEntry{userID: userID, machineID: unrelatedMachine, name: name}
		msg := dn.buildOfflineMessage(entry)

		expected := fmt.Sprintf("📴 %s 已离线", name)
		if msg != expected {
			t.Fatalf("iter %d: expected %q, got %q", iter, expected, msg)
		}

		coord.router.Stop()
	}
}

// ---------------------------------------------------------------------------
// Unit Test: coordinator 为 nil 时的降级行为
// ---------------------------------------------------------------------------

func TestDeviceNotifier_NilCoordinator_Fallback(t *testing.T) {
	dn := &DeviceNotifier{
		debounce:    make(map[string]*debounceEntry),
		activeUsers: make(map[string]activeUserInfo),
	}

	entry := &debounceEntry{userID: "user1", machineID: "m1", name: "MacBook"}
	msg := dn.buildOfflineMessage(entry)

	expected := "📴 MacBook 已离线"
	if msg != expected {
		t.Fatalf("expected %q, got %q", expected, msg)
	}
}

// ---------------------------------------------------------------------------
// Unit Test: 会议仅剩 1 台设备时的特殊提示消息
// ---------------------------------------------------------------------------

func TestDeviceNotifier_MeetingOneRemaining(t *testing.T) {
	dn, coord := newTestNotifierWithCoordinator()
	defer coord.router.Stop()
	ss := coord.SpaceStateStore()

	ss.EnterMeeting("user1", "topic", []string{"m1", "m2"})

	entry := &debounceEntry{userID: "user1", machineID: "m1", name: "MacBook"}
	msg := dn.buildOfflineMessage(entry)

	expected := "📴 MacBook 已离线，会议仅剩 1 台设备参与。"
	if msg != expected {
		t.Fatalf("expected %q, got %q", expected, msg)
	}

	// Verify still in meeting
	state := ss.GetOrCreate("user1")
	if state.State != SpaceMeeting {
		t.Fatalf("expected still in meeting, got %s", state.State)
	}
}

// ---------------------------------------------------------------------------
// Unit Test: 防抖期间空间状态已变化的场景
// ---------------------------------------------------------------------------

func TestDeviceNotifier_StateChangedDuringDebounce(t *testing.T) {
	dn, coord := newTestNotifierWithCoordinator()
	defer coord.router.Stop()
	ss := coord.SpaceStateStore()

	// User was in private mode when notification was scheduled
	ss.EnterPrivate("user1", "m1", "MacBook")

	// But user manually returned to lobby before debounce fires
	ss.ExitPrivate("user1")

	// Now the debounce fires — state is already lobby, so simple notification
	entry := &debounceEntry{userID: "user1", machineID: "m1", name: "MacBook"}
	msg := dn.buildOfflineMessage(entry)

	expected := "📴 MacBook 已离线"
	if msg != expected {
		t.Fatalf("expected %q, got %q", expected, msg)
	}
}
