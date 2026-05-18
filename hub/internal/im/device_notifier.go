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
	tenantID  string
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
	dn.MarkUserActiveForTenant("", userID, platformName, platformUID)
}

func (dn *DeviceNotifier) MarkUserActiveForTenant(tenantID, userID, platformName, platformUID string) {
	dn.mu.Lock()
	dn.activeUsers[tenantUserRuntimeKey(tenantID, userID)] = activeUserInfo{
		PlatformName: platformName,
		PlatformUID:  platformUID,
	}
	dn.mu.Unlock()
}

// GetActiveUser returns the last active IM platform info for a user.
// Returns ("", "", false) if the user has no recorded IM activity.
func (dn *DeviceNotifier) GetActiveUser(userID string) (platformName, platformUID string, ok bool) {
	return dn.GetActiveUserForTenant("", userID)
}

func (dn *DeviceNotifier) GetActiveUserForTenant(tenantID, userID string) (platformName, platformUID string, ok bool) {
	dn.mu.Lock()
	info, ok := dn.activeUsers[tenantUserRuntimeKey(tenantID, userID)]
	dn.mu.Unlock()
	if !ok {
		return "", "", false
	}
	return info.PlatformName, info.PlatformUID, true
}

// NotifyDeviceOnline queues an online notification with debouncing.
func (dn *DeviceNotifier) NotifyDeviceOnline(userID, machineID, name string) {
	dn.NotifyDeviceOnlineForTenant("", userID, machineID, name)
}

func (dn *DeviceNotifier) NotifyDeviceOnlineForTenant(tenantID, userID, machineID, name string) {
	dn.scheduleNotification(tenantID, userID, machineID, name, true)
}

// NotifyDeviceOffline queues an offline notification with debouncing.
func (dn *DeviceNotifier) NotifyDeviceOffline(userID, machineID, name string) {
	dn.NotifyDeviceOfflineForTenant("", userID, machineID, name)
}

func (dn *DeviceNotifier) NotifyDeviceOfflineForTenant(tenantID, userID, machineID, name string) {
	dn.scheduleNotification(tenantID, userID, machineID, name, false)
}

func (dn *DeviceNotifier) scheduleNotification(tenantID, userID, machineID, name string, online bool) {
	tenantID = normalizeTenantID(tenantID)
	userKey := tenantUserRuntimeKey(tenantID, userID)
	debounceKey := userKey + "\x00" + machineID
	dn.mu.Lock()
	defer dn.mu.Unlock()

	// Check if user is active.
	if _, ok := dn.activeUsers[userKey]; !ok {
		return
	}

	// Cancel any pending notification for this machine.
	if existing, ok := dn.debounce[debounceKey]; ok {
		existing.timer.Stop()
		delete(dn.debounce, debounceKey)
	}

	entry := &debounceEntry{
		tenantID:  tenantID,
		userID:    userID,
		machineID: machineID,
		name:      name,
		online:    online,
	}
	entry.timer = time.AfterFunc(debounceDuration, func() {
		dn.fireNotification(entry)
	})
	dn.debounce[debounceKey] = entry
}

func (dn *DeviceNotifier) fireNotification(entry *debounceEntry) {
	dn.mu.Lock()
	// Remove from debounce map.
	delete(dn.debounce, tenantUserRuntimeKey(entry.tenantID, entry.userID)+"\x00"+entry.machineID)
	info, ok := dn.activeUsers[tenantUserRuntimeKey(entry.tenantID, entry.userID)]
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
		dn.adapter.DeliverProgress(WithTenant(context.Background(), entry.tenantID), info.PlatformName, entry.userID, info.PlatformUID, msg)
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
	state := ss.GetOrCreateForTenant(entry.tenantID, entry.userID)

	switch state.State {
	case SpacePrivate:
		if state.PrivateTarget == entry.machineID {
			if err := ss.ExitPrivateForTenant(entry.tenantID, entry.userID); err != nil {
				log.Printf("[DeviceNotifier] ExitPrivate failed for user=%s: %v", entry.userID, err)
				return fmt.Sprintf("📴 %s 已离线", entry.name)
			}
			dn.coordinator.router.ClearSelectedMachineForTenant(entry.tenantID, entry.userID)
			return fmt.Sprintf("📴 %s 已离线，已自动返回大厅。", entry.name)
		}

	case SpaceMeeting:
		if containsParticipant(state.Participants, entry.machineID) {
			remaining := ss.RemoveParticipantForTenant(entry.tenantID, entry.userID, entry.machineID)
			switch {
			case remaining == 0:
				_ = ss.ExitMeetingForTenant(entry.tenantID, entry.userID)
				dn.coordinator.router.StopDiscussionForTenant(entry.tenantID, entry.userID)
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
