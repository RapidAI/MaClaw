package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// notifHTTPClient is a shared HTTP client for notification API calls with a
// 30-second timeout. Using http.DefaultClient would hang indefinitely if the
// Hub is unreachable.
var notifHTTPClient = &http.Client{
	Timeout: 30 * time.Second,
}

// ClientNotification is the view returned to GUI clients for display.
// Mirrors hub/internal/notification.ClientNotification but defined locally
// because Go's internal package rule prevents cross-module import.
type ClientNotification struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Category  string `json:"category"`
	Priority  string `json:"priority"`
	IsRead    bool   `json:"is_read"`
	CreatedAt string `json:"created_at"`
}

// NotificationPushPayload is the payload for WebSocket notification.push envelope.
// Mirrors hub/internal/notification.NotificationPushPayload.
type NotificationPushPayload struct {
	Action       string               `json:"action"`                    // "new" | "revoke"
	Notification *PushNotificationDTO `json:"notification,omitempty"`    // action=new
	NotifID      string               `json:"notification_id,omitempty"` // action=revoke
}

// PushNotificationDTO is the notification object received via WebSocket push.
type PushNotificationDTO struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Category  string `json:"category"`
	Priority  string `json:"priority"`
	CreatedAt string `json:"created_at"`
}

// ---------------------------------------------------------------------------
// notificationCache — LRU cache holding at most 10 unread ClientNotifications.
// Thread-safe via a mutex. Oldest entries are evicted when capacity is exceeded.
// ---------------------------------------------------------------------------

const notificationCacheCapacity = 10

type notificationCache struct {
	mu    sync.RWMutex
	items []ClientNotification
}

func newNotificationCache() *notificationCache {
	return &notificationCache{
		items: make([]ClientNotification, 0, notificationCacheCapacity),
	}
}

// Add inserts a notification at the front (newest-first). If the cache exceeds
// capacity, the oldest entry is evicted (LRU).
func (nc *notificationCache) Add(n *PushNotificationDTO) {
	if n == nil {
		return
	}
	cn := ClientNotification{
		ID:        n.ID,
		Title:     n.Title,
		Content:   n.Content,
		Category:  n.Category,
		Priority:  n.Priority,
		IsRead:    false,
		CreatedAt: n.CreatedAt,
	}
	nc.mu.Lock()
	defer nc.mu.Unlock()

	// Deduplicate — if same ID already exists, remove old entry first.
	for i, item := range nc.items {
		if item.ID == cn.ID {
			nc.items = append(nc.items[:i], nc.items[i+1:]...)
			break
		}
	}

	// Prepend (newest first).
	nc.items = append([]ClientNotification{cn}, nc.items...)

	// Evict oldest if over capacity.
	if len(nc.items) > notificationCacheCapacity {
		nc.items = nc.items[:notificationCacheCapacity]
	}
}

// AddClient inserts a ClientNotification directly (used when pulling from Hub).
func (nc *notificationCache) AddClient(cn ClientNotification) {
	nc.mu.Lock()
	defer nc.mu.Unlock()

	// Deduplicate.
	for i, item := range nc.items {
		if item.ID == cn.ID {
			nc.items = append(nc.items[:i], nc.items[i+1:]...)
			break
		}
	}

	nc.items = append(nc.items, cn)

	// Evict oldest (tail) if over capacity.
	if len(nc.items) > notificationCacheCapacity {
		nc.items = nc.items[:notificationCacheCapacity]
	}
}

// Remove removes a notification by ID (e.g. on revoke).
func (nc *notificationCache) Remove(id string) {
	nc.mu.Lock()
	defer nc.mu.Unlock()
	for i, item := range nc.items {
		if item.ID == id {
			nc.items = append(nc.items[:i], nc.items[i+1:]...)
			return
		}
	}
}

// MarkRead marks a single notification as read.
func (nc *notificationCache) MarkRead(id string) {
	nc.mu.Lock()
	defer nc.mu.Unlock()
	for i := range nc.items {
		if nc.items[i].ID == id {
			nc.items[i].IsRead = true
			return
		}
	}
}

// MarkAllRead marks all cached notifications as read.
func (nc *notificationCache) MarkAllRead() {
	nc.mu.Lock()
	defer nc.mu.Unlock()
	for i := range nc.items {
		nc.items[i].IsRead = true
	}
}

// GetUnread returns all unread notifications in the cache (newest-first order).
func (nc *notificationCache) GetUnread() []ClientNotification {
	nc.mu.RLock()
	defer nc.mu.RUnlock()
	result := make([]ClientNotification, 0, len(nc.items))
	for _, item := range nc.items {
		if !item.IsRead {
			result = append(result, item)
		}
	}
	return result
}

// UnreadCount returns the number of unread notifications in the cache.
func (nc *notificationCache) UnreadCount() int {
	nc.mu.RLock()
	defer nc.mu.RUnlock()
	count := 0
	for _, item := range nc.items {
		if !item.IsRead {
			count++
		}
	}
	return count
}

// Replace replaces the entire cache with the given list (used after pull).
func (nc *notificationCache) Replace(items []ClientNotification) {
	nc.mu.Lock()
	defer nc.mu.Unlock()
	if len(items) > notificationCacheCapacity {
		items = items[:notificationCacheCapacity]
	}
	nc.items = items
}

// ---------------------------------------------------------------------------
// Wails Bindings — exposed as App methods for frontend consumption.
// ---------------------------------------------------------------------------

// GetUnreadNotifications returns the in-memory cached unread notification list.
func (a *App) GetUnreadNotifications() []ClientNotification {
	if a == nil || a.notifCache == nil {
		return nil
	}
	return a.notifCache.GetUnread()
}

// GetUnreadCount returns the number of unread notifications in the local cache.
func (a *App) GetUnreadCount() int {
	if a == nil || a.notifCache == nil {
		return 0
	}
	return a.notifCache.UnreadCount()
}

// MarkNotificationRead marks a single notification as read locally and on the Hub.
func (a *App) MarkNotificationRead(notificationID string) error {
	notificationID = strings.TrimSpace(notificationID)
	if notificationID == "" {
		return fmt.Errorf("notification ID is required")
	}
	if a == nil || a.notifCache == nil {
		return fmt.Errorf("notification system not initialized")
	}

	// Optimistic local update.
	a.notifCache.MarkRead(notificationID)

	// Call Hub API.
	err := a.doNotificationHubRequest(
		context.Background(),
		http.MethodPost,
		"/api/v1/notifications/"+notificationID+"/read",
		nil,
		nil,
	)
	if err != nil {
		// Rollback: The notification is still marked read locally for UX;
		// next pull will reconcile. Log the error.
		a.log("[notification] MarkNotificationRead API failed: " + err.Error())
		return err
	}
	return nil
}

// MarkAllNotificationsRead marks all notifications as read locally and on the Hub.
func (a *App) MarkAllNotificationsRead() error {
	if a == nil || a.notifCache == nil {
		return fmt.Errorf("notification system not initialized")
	}

	// Optimistic local update.
	a.notifCache.MarkAllRead()

	// Call Hub API.
	err := a.doNotificationHubRequest(
		context.Background(),
		http.MethodPost,
		"/api/v1/notifications/read-all",
		nil,
		nil,
	)
	if err != nil {
		a.log("[notification] MarkAllNotificationsRead API failed: " + err.Error())
		return err
	}
	return nil
}

// PullUnreadNotifications fetches unread notifications from the Hub API and
// replaces the local cache. Called on WebSocket reconnect (after auth.ok).
func (a *App) PullUnreadNotifications() error {
	if a == nil || a.notifCache == nil {
		return fmt.Errorf("notification system not initialized")
	}

	var result []ClientNotification
	err := a.doNotificationHubRequest(
		context.Background(),
		http.MethodGet,
		"/api/v1/notifications/unread",
		nil,
		&result,
	)
	if err != nil {
		a.log("[notification] PullUnreadNotifications API failed: " + err.Error())
		return err
	}

	a.notifCache.Replace(result)
	return nil
}

// ---------------------------------------------------------------------------
// Internal HTTP helper — reuses the same pattern as doMobileDigitalEmployeeTaskRequest.
// ---------------------------------------------------------------------------

func (a *App) doNotificationHubRequest(ctx context.Context, method, path string, payload any, out any) error {
	if a == nil {
		return fmt.Errorf("app is nil")
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return err
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	machineID := strings.TrimSpace(cfg.RemoteMachineID)
	token := strings.TrimSpace(cfg.RemoteMachineToken)
	if base == "" || machineID == "" || token == "" {
		return fmt.Errorf("remote hub machine identity is incomplete")
	}

	var body *bytes.Reader
	if payload == nil {
		body = bytes.NewReader(nil)
	} else {
		raw, merr := json.Marshal(payload)
		if merr != nil {
			return merr
		}
		body = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Machine-ID", machineID)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := notifHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("hub returned HTTP %d for %s %s", resp.StatusCode, method, path)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
