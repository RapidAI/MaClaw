package guiapp

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

var emitNotificationFrontendEvent = func(a *App, name string, data ...interface{}) {
	if a == nil {
		return
	}
	a.emitEvent(name, data...)
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
	// Source and SourceID identify a notice raised by a Hub subsystem rather
	// than an admin, such as a reading-group invitation.
	Source   string `json:"source,omitempty"`
	SourceID string `json:"source_id,omitempty"`
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
	Source    string `json:"source,omitempty"`
	SourceID  string `json:"source_id,omitempty"`
}

// ---------------------------------------------------------------------------
// notificationCache — LRU cache holding at most 10 unread ClientNotifications.
// Thread-safe via a mutex. Oldest entries are evicted when capacity is exceeded.
// ---------------------------------------------------------------------------

const notificationCacheCapacity = 10

type notificationCache struct {
	mu    sync.RWMutex
	items []ClientNotification
	// readMark records ids this process has marked read and the Hub has not
	// yet been allowed to show as unread again. A failed mark drops the id so
	// the next pull can put it back. readGen changes on every mark and forget
	// so a list copied for the bell can be discarded when a click lands after
	// the copy.
	readGen  uint64
	readMark map[string]struct{}
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
		Source:    n.Source,
		SourceID:  n.SourceID,
	}
	nc.mu.Lock()
	defer nc.mu.Unlock()

	// Deduplicate — if same ID already exists, remove old entry first.
	// A push is a new delivery of this id, so an older local read does not hide it.
	nc.clearReadMarkLocked(cn.ID)
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
	nc.clearReadMarkLocked(id)
	for i, item := range nc.items {
		if item.ID == id {
			nc.items = append(nc.items[:i], nc.items[i+1:]...)
			return
		}
	}
}

// MarkRead marks a single notification as read and remembers the id. The
// companion poll must not show that id as unread while the mark stands.
func (nc *notificationCache) MarkRead(id string) {
	nc.mu.Lock()
	defer nc.mu.Unlock()
	nc.noteReadLocked(id)
	for i := range nc.items {
		if nc.items[i].ID == id {
			nc.items[i].IsRead = true
			return
		}
	}
}

// MarkAllRead marks all cached notifications as read. The returned ids are the
// ones a failed Hub call should release.
func (nc *notificationCache) MarkAllRead() []string {
	nc.mu.Lock()
	defer nc.mu.Unlock()
	ids := make([]string, 0, len(nc.items))
	for i := range nc.items {
		nc.items[i].IsRead = true
		if nc.items[i].ID == "" {
			continue
		}
		nc.noteReadLocked(nc.items[i].ID)
		ids = append(ids, nc.items[i].ID)
	}
	return ids
}

func (nc *notificationCache) noteReadLocked(id string) {
	nc.readGen++
	if id == "" {
		return
	}
	if nc.readMark == nil {
		nc.readMark = map[string]struct{}{}
	}
	nc.readMark[id] = struct{}{}
}

func (nc *notificationCache) clearReadMarkLocked(id string) {
	if nc.readMark == nil {
		return
	}
	if _, ok := nc.readMark[id]; !ok {
		return
	}
	delete(nc.readMark, id)
	nc.readGen++
}

// ForgetReadMarks releases ids after the Hub rejected the mark. The rows stay
// read in the cache until a later pull says they are still unread.
func (nc *notificationCache) ForgetReadMarks(ids []string) {
	if nc == nil || len(ids) == 0 {
		return
	}
	nc.mu.Lock()
	defer nc.mu.Unlock()
	if len(nc.readMark) == 0 {
		return
	}
	for _, id := range ids {
		if _, ok := nc.readMark[id]; !ok {
			continue
		}
		delete(nc.readMark, id)
		nc.readGen++
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

// snapshot copies every cached row, including ones already marked read.
// A pull that drops those rows is a real change: the bell has to forget them.
func (nc *notificationCache) snapshot() []ClientNotification {
	if nc == nil {
		return nil
	}
	nc.mu.RLock()
	defer nc.mu.RUnlock()
	if len(nc.items) == 0 {
		return nil
	}
	out := make([]ClientNotification, len(nc.items))
	copy(out, nc.items)
	return out
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

	// Optimistic local update. Publish it before the Hub call so a poll that
	// already copied the unread list cannot be the last thing the bell applies.
	a.notifCache.MarkRead(notificationID)
	a.publishNotificationCache()

	// Call Hub API.
	err := a.doNotificationHubRequest(
		context.Background(),
		http.MethodPost,
		"/api/v1/notifications/"+notificationID+"/read",
		nil,
		nil,
	)
	if err != nil {
		// The row stays read for now. Releasing the mark lets the next unread
		// pull show it again when the Hub still has it.
		a.notifCache.ForgetReadMarks([]string{notificationID})
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

	// Optimistic local update. Same generation ordering as a single mark.
	marked := a.notifCache.MarkAllRead()
	a.publishNotificationCache()

	// Call Hub API.
	err := a.doNotificationHubRequest(
		context.Background(),
		http.MethodPost,
		"/api/v1/notifications/read-all",
		nil,
		nil,
	)
	if err != nil {
		a.notifCache.ForgetReadMarks(marked)
		a.log("[notification] MarkAllNotificationsRead API failed: " + err.Error())
		return err
	}
	return nil
}

// fileCompanionUnreadNotifications reads GET /api/v1/notifications/unread.
// The reconnect pull and the companion title-bar watch both go through it.
// Tests replace the function; the default uses the machine token in config.
var fileCompanionUnreadNotifications = func(a *App) ([]ClientNotification, error) {
	var result []ClientNotification
	err := a.doNotificationHubRequest(
		context.Background(),
		http.MethodGet,
		"/api/v1/notifications/unread",
		nil,
		&result,
	)
	return result, err
}

// PullUnreadNotifications fetches unread notifications from the Hub API and
// replaces the local cache. Called on WebSocket reconnect (after auth.ok).
// A changed list also raises the system notices. The companion watch uses the
// same fetch with announcements left off, so the two processes do not each
// pop a balloon for one share.
func (a *App) PullUnreadNotifications() error {
	if a == nil || a.notifCache == nil {
		return fmt.Errorf("notification system not initialized")
	}

	result, err := fileCompanionUnreadNotifications(a)
	if err != nil {
		a.log("[notification] PullUnreadNotifications API failed: " + err.Error())
		return err
	}

	a.applyUnreadNotifications(result, true)
	return nil
}

func (a *App) replaceUnreadNotifications(items []ClientNotification) {
	a.applyUnreadNotifications(items, true)
}

// applyUnreadNotifications stores a pulled list and tells the open bell.
// announce is set on the reconnect pull, which is the path that reaches a
// machine that was offline when it was sent. An unchanged list is left alone.
// The event carries the cache generation taken with the stored list. The bell
// drops an event whose generation is older, so a list copied before a click
// cannot mark that row unread again.
func (a *App) applyUnreadNotifications(items []ClientNotification, announce bool) {
	if a == nil || a.notifCache == nil {
		return
	}
	stored, gen, changed := a.notifCache.replaceIfChanged(cappedNotificationList(items))
	if !changed {
		return
	}
	if announce {
		a.announceCompanionGroupInviteNotices(stored)
		a.announceCompanionShareNotices(stored)
		a.announceCompanionHostTransferNotices(stored)
	}
	a.emitNotificationSync(stored, gen)
}

// publishNotificationCache tells the open bell what the cache holds now.
func (a *App) publishNotificationCache() {
	if a == nil || a.notifCache == nil {
		return
	}
	items, gen := a.notifCache.snapshotWithGen()
	a.emitNotificationSync(items, gen)
}

func (a *App) emitNotificationSync(items []ClientNotification, gen uint64) {
	if items == nil {
		items = []ClientNotification{}
	}
	emitNotificationFrontendEvent(a, "notification:sync", items, gen)
}

// replaceIfChanged stores items when they differ from the cache. The returned
// generation is the one current at that store, so a later mark publishes a
// higher one.
func (nc *notificationCache) replaceIfChanged(items []ClientNotification) (stored []ClientNotification, gen uint64, changed bool) {
	if nc == nil {
		return nil, 0, false
	}
	nc.mu.Lock()
	defer nc.mu.Unlock()
	if notificationListsEqual(nc.items, items) {
		return nil, nc.readGen, false
	}
	nc.items = copyNotifications(items)
	return copyNotifications(nc.items), nc.readGen, true
}

// snapshotWithGen copies the cache and the generation under one lock.
func (nc *notificationCache) snapshotWithGen() ([]ClientNotification, uint64) {
	if nc == nil {
		return []ClientNotification{}, 0
	}
	nc.mu.RLock()
	defer nc.mu.RUnlock()
	return copyNotifications(nc.items), nc.readGen
}

func copyNotifications(items []ClientNotification) []ClientNotification {
	if len(items) == 0 {
		return []ClientNotification{}
	}
	return append([]ClientNotification(nil), items...)
}

func cappedNotificationList(items []ClientNotification) []ClientNotification {
	if len(items) == 0 {
		return []ClientNotification{}
	}
	if len(items) > notificationCacheCapacity {
		return append([]ClientNotification(nil), items[:notificationCacheCapacity]...)
	}
	return items
}

func notificationListsEqual(left, right []ClientNotification) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
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
