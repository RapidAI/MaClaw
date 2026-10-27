package guiapp

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type capturedNotificationEvent struct {
	name string
	data []interface{}
}

func captureNotificationEvents(t *testing.T) *[]capturedNotificationEvent {
	t.Helper()
	events := []capturedNotificationEvent{}
	original := emitNotificationFrontendEvent
	emitNotificationFrontendEvent = func(_ *App, name string, data ...interface{}) {
		events = append(events, capturedNotificationEvent{name: name, data: append([]interface{}{}, data...)})
	}
	t.Cleanup(func() {
		emitNotificationFrontendEvent = original
	})
	return &events
}

func TestReplaceUnreadNotificationsEmitsSync(t *testing.T) {
	events := captureNotificationEvents(t)
	app := &App{notifCache: newNotificationCache()}
	items := []ClientNotification{
		{ID: "n-1", Title: "Hub broadcast", Content: "hello", Category: "system_announcement", Priority: "normal", CreatedAt: "2026-07-03T00:00:00Z"},
	}

	app.replaceUnreadNotifications(items)

	unread := app.GetUnreadNotifications()
	if len(unread) != 1 || unread[0].ID != "n-1" {
		t.Fatalf("cached unread = %+v, want n-1", unread)
	}
	if len(*events) != 1 {
		t.Fatalf("events len = %d, want 1", len(*events))
	}
	event := (*events)[0]
	if event.name != "notification:sync" {
		t.Fatalf("event name = %q, want notification:sync", event.name)
	}
	got, ok := event.data[0].([]ClientNotification)
	if !ok || len(got) != 1 || got[0].ID != "n-1" {
		t.Fatalf("sync payload = %#v, want []ClientNotification with n-1", event.data)
	}
}

func TestHandleNotificationPushEmitsFrontendEvents(t *testing.T) {
	events := captureNotificationEvents(t)
	app := &App{notifCache: newNotificationCache()}

	push := NotificationPushPayload{
		Action: "new",
		Notification: &PushNotificationDTO{
			ID:        "n-urgent",
			Title:     "Urgent notice",
			Content:   "please read",
			Category:  "security_alert",
			Priority:  "urgent",
			CreatedAt: "2026-07-03T00:00:00Z",
		},
	}
	raw, err := json.Marshal(push)
	if err != nil {
		t.Fatalf("marshal push: %v", err)
	}
	app.handleNotificationPush(raw)

	if app.GetUnreadCount() != 1 {
		t.Fatalf("unread count = %d, want 1", app.GetUnreadCount())
	}
	if len(*events) != 2 {
		t.Fatalf("events len after new = %d, want 2", len(*events))
	}
	if (*events)[0].name != "notification:new" || (*events)[1].name != "notification:urgent-toast" {
		t.Fatalf("events after new = %+v, want notification:new and notification:urgent-toast", *events)
	}

	revoke := NotificationPushPayload{Action: "revoke", NotifID: "n-urgent"}
	raw, err = json.Marshal(revoke)
	if err != nil {
		t.Fatalf("marshal revoke: %v", err)
	}
	app.handleNotificationPush(raw)

	if app.GetUnreadCount() != 0 {
		t.Fatalf("unread count after revoke = %d, want 0", app.GetUnreadCount())
	}
	if len(*events) != 3 || (*events)[2].name != "notification:revoke" {
		t.Fatalf("events after revoke = %+v, want trailing notification:revoke", *events)
	}
}

// TestNotificationCacheKeepsSourceFields covers the push-to-cache hop for a
// notice raised by a Hub subsystem: the source is what later tells the desktop
// to raise the system notification instead of a plain bell entry.
func TestNotificationCacheKeepsSourceFields(t *testing.T) {
	cache := newNotificationCache()
	cache.Add(&PushNotificationDTO{
		ID:        "n-invite",
		Title:     "入群邀请",
		Content:   "owner@example.com 邀请你加入阅读群组「论文组」。",
		Category:  "custom",
		Priority:  "important",
		CreatedAt: "2026-10-01T00:00:00Z",
		Source:    companionGroupInviteNotificationSource,
		SourceID:  "cg_0123456789abcdef0123456789abcdef:user-1",
	})
	unread := cache.GetUnread()
	if len(unread) != 1 {
		t.Fatalf("unread %d want 1", len(unread))
	}
	if unread[0].Source != companionGroupInviteNotificationSource {
		t.Fatalf("source %q", unread[0].Source)
	}
	if unread[0].SourceID != "cg_0123456789abcdef0123456789abcdef:user-1" {
		t.Fatalf("source id %q", unread[0].SourceID)
	}
}

// TestReplaceUnreadNotificationsAnnouncesInvite covers the offline path: the
// unread pull after reconnecting raises the system notification when the
// pending list itself cannot be read.
func TestReplaceUnreadNotificationsAnnouncesInvite(t *testing.T) {
	resetCompanionGroupInviteWatch(t)
	notifs, _, restore := stubTrayNotifyEnv(t, false)
	defer restore()
	stubCompanionGroupInviteList(t, func(*App) ([]FileCompanionGroupInvite, error) {
		return nil, errors.New("暂时无法连接云端")
	})
	_ = captureNotificationEvents(t)
	app := &App{notifCache: newNotificationCache()}

	app.replaceUnreadNotifications([]ClientNotification{{
		ID:        "n-invite",
		Title:     "入群邀请",
		Content:   "owner@example.com 邀请你加入阅读群组「论文组」。",
		Category:  "custom",
		Priority:  "important",
		CreatedAt: "2026-10-01T00:00:00Z",
		Source:    companionGroupInviteNotificationSource,
		SourceID:  "cg_0123456789abcdef0123456789abcdef:user-1",
	}})

	if len(*notifs) != 1 {
		t.Fatalf("notifications %d want 1", len(*notifs))
	}
	if (*notifs)[0].title != "入群邀请" || !strings.Contains((*notifs)[0].msg, "论文组") {
		t.Fatalf("notice %+v", (*notifs)[0])
	}
}
