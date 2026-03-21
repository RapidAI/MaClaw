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
		msg = fmt.Sprintf("📴 %s 已离线", entry.name)
		// Check if this was the user's selected device.
		if dn.coordinator != nil {
			selected, _ := dn.coordinator.router.GetSelectedMachine(entry.userID)
			if selected == entry.machineID {
				msg += "\n💡 这是您当前选定的设备，请使用 /call <昵称> 切换到其他设备。"
			}
		}
	}

	// Deliver via progress (lightweight, no response expected).
	if dn.adapter != nil {
		dn.adapter.DeliverProgress(context.Background(), info.PlatformName, entry.userID, info.PlatformUID, msg)
	} else {
		log.Printf("[DeviceNotifier] adapter not wired, dropping notification for user=%s", entry.userID)
	}
}
