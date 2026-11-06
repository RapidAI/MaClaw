package guiapp

import (
	"sync"
	"time"
)

// The companion process does not open a Hub websocket. BindDesktop keeps one
// connection per machine id, and a second Connect would replace the main
// window's connection: sessions, IM, mobile tasks, and the push that raises
// the system balloon. This process still has its own notification cache, which
// the title-bar bell reads. The unread list is pulled over the same HTTP call
// the main window uses after reconnect, then published as notification:sync.
// The pull does not raise an OS balloon. The main process already does that
// from the websocket push. When only this process is running, the bell still
// updates and no balloon is raised.

const fileCompanionNotificationWatchInterval = 10 * time.Second

var fileCompanionNotificationWatch struct {
	mu      sync.Mutex
	stop    chan struct{}
	running bool
}

// startFileCompanionNotificationWatch begins the unread pull. Calling it twice
// keeps the watcher that is already running.
func (a *App) startFileCompanionNotificationWatch() {
	if a == nil {
		return
	}
	fileCompanionNotificationWatch.mu.Lock()
	if fileCompanionNotificationWatch.running {
		fileCompanionNotificationWatch.mu.Unlock()
		return
	}
	stop := make(chan struct{})
	fileCompanionNotificationWatch.stop = stop
	fileCompanionNotificationWatch.running = true
	fileCompanionNotificationWatch.mu.Unlock()

	go func() {
		logFileCompanion("notification watch started")
		// A share that arrived while this window was closed is waiting on the
		// first pull. Later pulls sit on the interval.
		a.syncFileCompanionNotifications()
		ticker := time.NewTicker(fileCompanionNotificationWatchInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				a.syncFileCompanionNotifications()
			}
		}
	}()
}

// stopFileCompanionNotificationWatch ends the pull on process shutdown.
func (a *App) stopFileCompanionNotificationWatch() {
	fileCompanionNotificationWatch.mu.Lock()
	defer fileCompanionNotificationWatch.mu.Unlock()
	if !fileCompanionNotificationWatch.running {
		return
	}
	fileCompanionNotificationWatch.running = false
	if fileCompanionNotificationWatch.stop != nil {
		close(fileCompanionNotificationWatch.stop)
		fileCompanionNotificationWatch.stop = nil
	}
}

func fileCompanionNotificationWatchRunning() bool {
	fileCompanionNotificationWatch.mu.Lock()
	defer fileCompanionNotificationWatch.mu.Unlock()
	return fileCompanionNotificationWatch.running
}

// syncFileCompanionNotifications pulls the unread list into this process's
// cache and emits notification:sync. A failed pull leaves the cache as it is.
// Failures are not logged: a machine that has not linked a Hub identity would
// otherwise write a line every tick. The pull does not announce.
//
// The list is merged. A full replace, which is what the main window does once
// on reconnect, would run here every tick: a row the person just opened would
// become unread again when the Hub's unread response was already in flight,
// and a row they had read would leave the panel.
func (a *App) syncFileCompanionNotifications() {
	if a == nil || a.notifCache == nil {
		return
	}
	items, err := fileCompanionUnreadNotifications(a)
	if err != nil {
		return
	}
	out, readGen, changed := a.notifCache.mergePulledUnread(items)
	if !changed {
		return
	}
	// A click can land after the merge copy. If it did, send the cache from
	// that moment and its newer generation. A copy that lost the race keeps
	// the older generation, and the bell ignores it.
	payload, gen := a.notifCache.syncPayload(readGen, out)
	a.emitNotificationSync(payload, gen)
}

// mergePulledUnread folds a Hub unread list into the cache. Ids this process
// has marked read stay read. Read rows the Hub no longer returns stay in the
// cache, so the open panel keeps them the way the main window does between
// reconnects. Unread rows the Hub no longer returns are removed.
func (nc *notificationCache) mergePulledUnread(pulled []ClientNotification) (out []ClientNotification, readGen uint64, changed bool) {
	if nc == nil {
		return nil, 0, false
	}
	nc.mu.Lock()
	defer nc.mu.Unlock()
	merged := mergeCompanionNotificationPull(nc.items, cappedNotificationList(pulled), nc.readMark)
	readGen = nc.readGen
	if notificationListsEqual(nc.items, merged) {
		return nil, readGen, false
	}
	nc.items = append([]ClientNotification(nil), merged...)
	return append([]ClientNotification(nil), nc.items...), readGen, true
}

// syncPayload returns fallback when no mark landed after it was built.
// Otherwise it returns the cache and the generation of that mark.
func (nc *notificationCache) syncPayload(readGen uint64, fallback []ClientNotification) ([]ClientNotification, uint64) {
	if nc == nil {
		return fallback, readGen
	}
	nc.mu.Lock()
	defer nc.mu.Unlock()
	if nc.readGen == readGen {
		return fallback, readGen
	}
	return copyNotifications(nc.items), nc.readGen
}

// mergeCompanionNotificationPull places Hub rows that are not already cached
// at the front and keeps the previous order for everything else.
func mergeCompanionNotificationPull(local, pulled []ClientNotification, keepRead map[string]struct{}) []ClientNotification {
	pulledByID := make(map[string]ClientNotification, len(pulled))
	for _, item := range pulled {
		if item.ID == "" {
			continue
		}
		if _, ok := keepRead[item.ID]; ok {
			item.IsRead = true
		}
		pulledByID[item.ID] = item
	}
	localIDs := make(map[string]struct{}, len(local))
	for _, item := range local {
		if item.ID != "" {
			localIDs[item.ID] = struct{}{}
		}
	}
	out := make([]ClientNotification, 0, len(local)+len(pulled))
	seen := make(map[string]struct{}, len(local)+len(pulled))
	for _, item := range pulled {
		if item.ID == "" {
			continue
		}
		if _, exists := localIDs[item.ID]; exists {
			continue
		}
		if _, ok := seen[item.ID]; ok {
			continue
		}
		out = append(out, pulledByID[item.ID])
		seen[item.ID] = struct{}{}
	}
	for _, item := range local {
		if item.ID == "" {
			continue
		}
		if _, ok := seen[item.ID]; ok {
			continue
		}
		if pulledItem, ok := pulledByID[item.ID]; ok {
			out = append(out, pulledItem)
			seen[item.ID] = struct{}{}
			continue
		}
		if item.IsRead {
			out = append(out, item)
		}
	}
	return cappedNotificationList(out)
}
