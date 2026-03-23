package im

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// DeviceNotifier sends device online/offline notifications to users via IM.
// It includes 30-second debouncing to avoid notification storms from
// network flapping.
type DeviceNotifier struct {
	adapter     *Adapter
	coordinator *Coordinator

	mu          sync.Mutex
	debounce    map[string]*debounceEntry // machineID → pending notification
	activeUsers map[string]activeUserInfo // userID → IM info (only notify active users)
}

type activeUserInfo struct {
	PlatformName string
	PlatformUID  string
}

type debounceEntry struct {
	userID    string
	machineID string
	name      string
	online    bool
	timer     *time.Timer
}

const debounceDuration = 30 * time.Second

// NewDeviceNotifier creates a notifier.
func NewDeviceNotifier(adapter *Adapter, coordinator *Coordinator) *DeviceNotifier {
	return &DeviceNotifier{
		adapter:     adapter,
		coordinator: coordinator,
		debounce:    make(map[string]*debounceEntry),
		activeUsers: make(map[string]activeUserInfo),
	}
}

// MarkUserActive records that a user has interacted via IM, so we know
// which platform to send notifications to. Called by the Adapter on
// each incoming message.
func (dn *DeviceNotifier) MarkUserActive(userID, platformName, platformUID string) {
	dn.mu.Lock()
	dn.activeUsers[userID] = activeUserInfo{
		PlatformName: platformName,
		PlatformUID:  platformUID,
	}
	dn.mu.Unlock()
}

// GetActiveUser returns the last active IM platform info for a user.
// Returns ("", "", false) if the user has no recorded IM activity.
func (dn *DeviceNotifier) GetActiveUser(userID string) (platformName, platformUID string, ok bool) {
	dn.mu.Lock()
	info, ok := dn.activeUsers[userID]
	dn.mu.Unlock()
	if !ok {
		return "", "", false
	}
	return info.PlatformName, info.PlatformUID, true
}


// NotifyDeviceOnline queues an online notification with debouncing.
func (dn *DeviceNotifier) NotifyDeviceOnline(userID, machineID, name string) {
	dn.scheduleNotification(userID, machineID, name, true)
}

// NotifyDeviceOffline queues an offline notification with debouncing.
func (dn *DeviceNotifier) NotifyDeviceOffline(userID, machineID, name string) {
	dn.scheduleNotification(userID, machineID, name, false)
}

func (dn *DeviceNotifier) scheduleNotification(userID, machineID, name string, online bool) {
	dn.mu.Lock()
	defer dn.mu.Unlock()

	// Check if user is active.
	if _, ok := dn.activeUsers[userID]; !ok {
		return
	}

	// Cancel any pending notification for this machine.
	if existing, ok := dn.debounce[machineID]; ok {
		existing.timer.Stop()
		delete(dn.debounce, machineID)
	}

	entry := &debounceEntry{
		userID:    userID,
		machineID: machineID,
		name:      name,
		online:    online,
	}
	entry.timer = time.AfterFunc(debounceDuration, func() {
		dn.fireNotification(entry)
	})
	dn.debounce[machineID] = entry
}

func (dn *DeviceNotifier) fireNotification(entry *debounceEntry) {
	dn.mu.Lock()
	// Remove from debounce map.
	delete(dn.debounce, entry.machineID)
	info, ok := dn.activeUsers[entry.userID]
	dn.mu.Unlock()

	if !ok {
		return
	}

	var msg string
	if entry.online {
		msg = fmt.Sprintf("📱 %s 已上线", entry.name)
	} else {
		msg = dn.buildOfflineMessage(entry)
	}

	// Deliver via progress (lightweight, no response expected).
	if dn.adapter != nil {
		dn.adapter.DeliverProgress(context.Background(), info.PlatformName, entry.userID, info.PlatformUID, msg)
	} else {
		log.Printf("[DeviceNotifier] adapter not wired, dropping notification for user=%s", entry.userID)
	}
}

// buildOfflineMessage constructs the offline notification message, handling
// automatic space state recovery when the disconnected device is relevant
// to the user's current interaction space.
func (dn *DeviceNotifier) buildOfflineMessage(entry *debounceEntry) string {
	if dn.coordinator == nil {
		return fmt.Sprintf("📴 %s 已离线", entry.name)
	}

	ss := dn.coordinator.SpaceStateStore()
	state := ss.GetOrCreate(entry.userID)

	switch state.State {
	case SpacePrivate:
		if state.PrivateTarget == entry.machineID {
			if err := ss.ExitPrivate(entry.userID); err != nil {
				log.Printf("[DeviceNotifier] ExitPrivate failed for user=%s: %v", entry.userID, err)
				return fmt.Sprintf("📴 %s 已离线", entry.name)
			}
			dn.coordinator.router.ClearSelectedMachine(entry.userID)
			return fmt.Sprintf("📴 %s 已离线，已自动返回大厅。", entry.name)
		}

	case SpaceMeeting:
		if containsParticipant(state.Participants, entry.machineID) {
			remaining := ss.RemoveParticipant(entry.userID, entry.machineID)
			switch {
			case remaining == 0:
				_ = ss.ExitMeeting(entry.userID)
				dn.coordinator.router.StopDiscussion(entry.userID)
				return "📴 所有会议设备已离线，会议已结束，已返回大厅。"
			case remaining == 1:
				return fmt.Sprintf("📴 %s 已离线，会议仅剩 1 台设备参与。", entry.name)
			default:
				return fmt.Sprintf("📴 %s 已离线", entry.name)
			}
		}
	}

	return fmt.Sprintf("📴 %s 已离线", entry.name)
}

func containsParticipant(participants []string, machineID string) bool {
	for _, p := range participants {
		if p == machineID {
			return true
		}
	}
	return false
}
