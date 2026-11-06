package guiapp

import (
	"errors"
	"testing"
)

func TestFileCompanionNotificationSyncUpdatesCacheWithoutABalloon(t *testing.T) {
	events := captureNotificationEvents(t)
	originalFetch := fileCompanionUnreadNotifications
	t.Cleanup(func() { fileCompanionUnreadNotifications = originalFetch })
	id := "companion-sync-" + t.Name()
	fileCompanionUnreadNotifications = func(*App) ([]ClientNotification, error) {
		return []ClientNotification{{
			ID:      id,
			Title:   "分享文档",
			Content: "论文.pdf",
			Source:  companionShareNotificationSource,
		}}, nil
	}
	originalShow, originalFlash := ShowNotification, FlashAndBeep
	t.Cleanup(func() { ShowNotification, FlashAndBeep = originalShow, originalFlash })
	shown := 0
	ShowNotification = func(string, string, uint32) { shown++ }
	FlashAndBeep = func() {}

	app := &App{notifCache: newNotificationCache()}
	app.syncFileCompanionNotifications()
	app.syncFileCompanionNotifications()

	if shown != 0 {
		t.Fatalf("balloons = %d, want 0", shown)
	}
	unread := app.GetUnreadNotifications()
	if len(unread) != 1 || unread[0].ID != id {
		t.Fatalf("cache = %+v", unread)
	}
	if len(*events) != 1 || (*events)[0].name != "notification:sync" {
		t.Fatalf("events = %+v, want one notification:sync", *events)
	}
}

func TestPullUnreadNotificationsStillAnnounces(t *testing.T) {
	originalFetch := fileCompanionUnreadNotifications
	t.Cleanup(func() { fileCompanionUnreadNotifications = originalFetch })
	id := "pull-announce-" + t.Name()
	fileCompanionUnreadNotifications = func(*App) ([]ClientNotification, error) {
		return []ClientNotification{{
			ID:      id,
			Title:   "分享文档",
			Content: "论文.pdf",
			Source:  companionShareNotificationSource,
		}}, nil
	}
	originalShow, originalFlash := ShowNotification, FlashAndBeep
	t.Cleanup(func() { ShowNotification, FlashAndBeep = originalShow, originalFlash })
	shown := 0
	ShowNotification = func(string, string, uint32) { shown++ }
	FlashAndBeep = func() {}

	app := &App{notifCache: newNotificationCache()}
	if err := app.PullUnreadNotifications(); err != nil {
		t.Fatal(err)
	}
	// The seen map would also suppress a second balloon. Clearing it checks
	// that an unchanged pull returns before announce.
	resetCompanionShareAnnouncements()
	if err := app.PullUnreadNotifications(); err != nil {
		t.Fatal(err)
	}
	if shown != 1 {
		t.Fatalf("balloons = %d, want 1", shown)
	}
	unread := app.GetUnreadNotifications()
	if len(unread) != 1 || unread[0].ID != id {
		t.Fatalf("cache = %+v", unread)
	}
}

func TestFileCompanionNotificationSyncKeepsCacheWhenThePullFails(t *testing.T) {
	originalFetch := fileCompanionUnreadNotifications
	t.Cleanup(func() { fileCompanionUnreadNotifications = originalFetch })
	fileCompanionUnreadNotifications = func(*App) ([]ClientNotification, error) {
		return nil, errors.New("remote hub machine identity is incomplete")
	}
	originalShow, originalFlash := ShowNotification, FlashAndBeep
	t.Cleanup(func() { ShowNotification, FlashAndBeep = originalShow, originalFlash })
	shown := 0
	ShowNotification = func(string, string, uint32) { shown++ }
	FlashAndBeep = func() {}

	app := &App{notifCache: newNotificationCache()}
	app.notifCache.Replace([]ClientNotification{{ID: "kept", Title: "still here"}})
	app.syncFileCompanionNotifications()

	unread := app.GetUnreadNotifications()
	if len(unread) != 1 || unread[0].ID != "kept" || shown != 0 {
		t.Fatalf("unread=%+v balloons=%d", unread, shown)
	}
}

func TestFileCompanionNotificationSyncKeepsALocalRead(t *testing.T) {
	events := captureNotificationEvents(t)
	originalFetch := fileCompanionUnreadNotifications
	t.Cleanup(func() { fileCompanionUnreadNotifications = originalFetch })
	item := ClientNotification{
		ID: "companion-read-kept", Title: "分享文档", Content: "论文.pdf", Source: companionShareNotificationSource,
	}
	// The unread response still carries the row. It was sent before the click.
	fileCompanionUnreadNotifications = func(*App) ([]ClientNotification, error) {
		return []ClientNotification{item}, nil
	}
	originalShow, originalFlash := ShowNotification, FlashAndBeep
	t.Cleanup(func() { ShowNotification, FlashAndBeep = originalShow, originalFlash })
	shown := 0
	ShowNotification = func(string, string, uint32) { shown++ }
	FlashAndBeep = func() {}

	app := &App{notifCache: newNotificationCache()}
	app.notifCache.Replace([]ClientNotification{item})
	app.notifCache.MarkRead(item.ID)
	app.syncFileCompanionNotifications()

	if shown != 0 {
		t.Fatalf("balloons = %d, want 0", shown)
	}
	if unread := app.GetUnreadNotifications(); len(unread) != 0 {
		t.Fatalf("unread = %+v, want none", unread)
	}
	cached := app.notifCache.snapshot()
	if len(cached) != 1 || cached[0].ID != item.ID || !cached[0].IsRead {
		t.Fatalf("cache = %+v", cached)
	}
	if len(*events) != 0 {
		t.Fatalf("events = %+v, want none", *events)
	}
}

func TestFileCompanionNotificationSyncKeepsReadRowWhenHubOmitsIt(t *testing.T) {
	originalFetch := fileCompanionUnreadNotifications
	t.Cleanup(func() { fileCompanionUnreadNotifications = originalFetch })
	readItem := ClientNotification{ID: "companion-read-kept-omitted", Title: "分享文档", Content: "论文.pdf", Source: companionShareNotificationSource}
	fresh := ClientNotification{ID: "companion-read-new", Title: "新分享", Content: "新论文.pdf", Source: companionShareNotificationSource}
	fileCompanionUnreadNotifications = func(*App) ([]ClientNotification, error) {
		return []ClientNotification{fresh}, nil
	}
	app := &App{notifCache: newNotificationCache()}
	app.notifCache.Replace([]ClientNotification{readItem})
	app.notifCache.MarkRead(readItem.ID)
	app.syncFileCompanionNotifications()

	unread := app.GetUnreadNotifications()
	if len(unread) != 1 || unread[0].ID != fresh.ID {
		t.Fatalf("unread = %+v", unread)
	}
	cached := app.notifCache.snapshot()
	if len(cached) != 2 || cached[0].ID != fresh.ID || cached[1].ID != readItem.ID || !cached[1].IsRead {
		t.Fatalf("cache = %+v", cached)
	}
}

func TestFileCompanionNotificationSyncDropsUnreadHubOmitted(t *testing.T) {
	events := captureNotificationEvents(t)
	originalFetch := fileCompanionUnreadNotifications
	t.Cleanup(func() { fileCompanionUnreadNotifications = originalFetch })
	fileCompanionUnreadNotifications = func(*App) ([]ClientNotification, error) {
		return nil, nil
	}
	app := &App{notifCache: newNotificationCache()}
	app.notifCache.Replace([]ClientNotification{{
		ID: "companion-revoked", Title: "分享文档", Source: companionShareNotificationSource,
	}})
	app.syncFileCompanionNotifications()
	if len(app.GetUnreadNotifications()) != 0 || len(app.notifCache.snapshot()) != 0 {
		t.Fatalf("cache = %+v", app.notifCache.snapshot())
	}
	if len(*events) != 1 || (*events)[0].name != "notification:sync" {
		t.Fatalf("events = %+v", *events)
	}
}

func TestMarkNotificationReadFailureLetsTheNextPullRestoreUnread(t *testing.T) {
	events := captureNotificationEvents(t)
	originalFetch := fileCompanionUnreadNotifications
	t.Cleanup(func() { fileCompanionUnreadNotifications = originalFetch })
	item := ClientNotification{ID: "companion-mark-failed", Title: "分享文档", Content: "论文.pdf", Source: companionShareNotificationSource}
	fileCompanionUnreadNotifications = func(*App) ([]ClientNotification, error) {
		return []ClientNotification{item}, nil
	}
	app := &App{notifCache: newNotificationCache(), testHomeDir: t.TempDir()}
	app.notifCache.Replace([]ClientNotification{item})
	if err := app.MarkNotificationRead(item.ID); err == nil {
		t.Fatal("expected the mark to fail without a hub identity")
	}
	if len(*events) != 1 || (*events)[0].name != "notification:sync" {
		t.Fatalf("events after mark = %+v", *events)
	}
	marked := notificationEventList(t, (*events)[0])
	markedGen := notificationEventGen(t, (*events)[0])
	if len(marked) != 1 || marked[0].ID != item.ID || !marked[0].IsRead {
		t.Fatalf("mark payload = %+v", marked)
	}
	app.syncFileCompanionNotifications()
	unread := app.GetUnreadNotifications()
	if len(unread) != 1 || unread[0].ID != item.ID || unread[0].IsRead {
		t.Fatalf("unread = %+v", unread)
	}
	if len(*events) != 2 {
		t.Fatalf("events after pull = %+v", *events)
	}
	restored := notificationEventList(t, (*events)[1])
	restoredGen := notificationEventGen(t, (*events)[1])
	if len(restored) != 1 || restored[0].ID != item.ID || restored[0].IsRead {
		t.Fatalf("pull payload = %+v", restored)
	}
	if restoredGen <= markedGen {
		t.Fatalf("pull gen %d, mark gen %d", restoredGen, markedGen)
	}
}

func notificationEventList(t *testing.T, event capturedNotificationEvent) []ClientNotification {
	t.Helper()
	if len(event.data) == 0 {
		t.Fatal("missing sync list")
	}
	list, ok := event.data[0].([]ClientNotification)
	if !ok {
		t.Fatalf("sync list = %#v", event.data[0])
	}
	return list
}

func notificationEventGen(t *testing.T, event capturedNotificationEvent) uint64 {
	t.Helper()
	if len(event.data) < 2 {
		t.Fatalf("sync data = %#v, want list and generation", event.data)
	}
	gen, ok := event.data[1].(uint64)
	if !ok {
		t.Fatalf("generation = %#v", event.data[1])
	}
	return gen
}

func TestFileCompanionNotificationSyncNilCacheDoesNotFetch(t *testing.T) {
	originalFetch := fileCompanionUnreadNotifications
	t.Cleanup(func() { fileCompanionUnreadNotifications = originalFetch })
	called := 0
	fileCompanionUnreadNotifications = func(*App) ([]ClientNotification, error) {
		called++
		return nil, errors.New("should not be called")
	}
	(&App{}).syncFileCompanionNotifications()
	if called != 0 {
		t.Fatalf("fetch calls = %d, want 0", called)
	}
}
