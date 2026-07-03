package main

import (
	"encoding/json"
	"log"
)

// handleNotificationPush processes a notification.push envelope received from
// the Hub WebSocket. It updates the local notification cache and emits frontend
// events so the React notification components can react in real time.
func (a *App) handleNotificationPush(payload json.RawMessage) {
	if a == nil || a.notifCache == nil {
		return
	}

	var push NotificationPushPayload
	if err := json.Unmarshal(payload, &push); err != nil {
		log.Printf("[notification] handleNotificationPush: unmarshal error: %v", err)
		return
	}

	switch push.Action {
	case "new":
		if push.Notification == nil {
			return
		}
		// Add to local unread cache (LRU, max 10).
		a.notifCache.Add(push.Notification)
		// Notify frontend of the new notification.
		emitNotificationFrontendEvent(a, "notification:new", push.Notification)
		// Urgent notifications get an additional toast event.
		if push.Notification.Priority == "urgent" {
			emitNotificationFrontendEvent(a, "notification:urgent-toast", push.Notification)
		}

	case "revoke":
		if push.NotifID == "" {
			return
		}
		a.notifCache.Remove(push.NotifID)
		emitNotificationFrontendEvent(a, "notification:revoke", push.NotifID)
	}
}
