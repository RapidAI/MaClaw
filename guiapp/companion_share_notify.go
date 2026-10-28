package guiapp

import (
	"strings"
	"sync"
	"time"
)

// A document shared with this person or their reading group is delivered as a
// Hub notification. The bell only exists on the assistant panel, so a reader
// working elsewhere would miss it; raise the operating system's notice too.
// Opening the notice in the bell offers 打开, which imports the share (if
// needed) and opens it in the companion.
//
// The same notice arrives twice on a reconnect (a WebSocket push and the unread
// pull). It is announced once per notification id while this process runs.

const (
	companionShareNotificationSource  = "companion_share"
	companionHostTransferSource       = "companion_host_transfer"
	companionHostTransferNoticeSource = "companion_host_transfer_notice"
	companionShareAnnounceTTL         = 24 * time.Hour
	companionShareAnnounceMapMax      = 4096
)

var companionShareAnnounce = struct {
	mu   sync.Mutex
	seen map[string]time.Time
}{seen: map[string]time.Time{}}

func isCompanionShareNotification(source string) bool {
	return strings.TrimSpace(source) == companionShareNotificationSource
}

func isCompanionHostTransferNotification(source string) bool {
	switch strings.TrimSpace(source) {
	case companionHostTransferSource, companionHostTransferNoticeSource:
		return true
	default:
		return false
	}
}

// announceCompanionShareNotification raises one system notification. It
// returns false when this id was already announced or the OS channel is
// unavailable.
func (a *App) announceCompanionShareNotification(notificationID, title, content string) bool {
	key := strings.TrimSpace(notificationID)
	if key == "" || ShowNotification == nil {
		return false
	}
	now := time.Now()
	companionShareAnnounce.mu.Lock()
	if _, seen := companionShareAnnounce.seen[key]; seen {
		companionShareAnnounce.mu.Unlock()
		return false
	}
	companionShareAnnounce.seen[key] = now
	if len(companionShareAnnounce.seen) > companionShareAnnounceMapMax {
		cutoff := now.Add(-companionShareAnnounceTTL)
		for id, at := range companionShareAnnounce.seen {
			if at.Before(cutoff) {
				delete(companionShareAnnounce.seen, id)
			}
		}
	}
	companionShareAnnounce.mu.Unlock()

	body := strings.TrimSpace(content)
	if body == "" {
		body = "有文档分享给你。在伴读通知里点「打开」即可查看。"
	}
	if FlashAndBeep != nil {
		FlashAndBeep()
	}
	// iconFlag 2 is informational: a share is new content, not an error.
	noticeTitle := strings.TrimSpace(title)
	if noticeTitle == "" {
		noticeTitle = "分享文档"
	}
	ShowNotification(noticeTitle, trayBalloonBody(body), 2)
	return true
}

// announceCompanionShareNotices raises the unread share notices pulled after a
// reconnect. A notice already pushed over the WebSocket is skipped by id. At
// most three pop up; the rest wait in the bell, so a long offline period does
// not bury the desktop in balloons.
func (a *App) announceCompanionShareNotices(items []ClientNotification) {
	shown := 0
	for _, item := range items {
		if item.IsRead || !isCompanionShareNotification(item.Source) {
			continue
		}
		if a.announceCompanionShareNotification(item.ID, item.Title, item.Content) {
			shown++
			if shown >= 3 {
				return
			}
		}
	}
}

// announceCompanionHostTransferNotices raises the transfer request and outcome
// notices pulled after a reconnect. The actionable request is answered in the
// bell, so the system notice only points there.
func (a *App) announceCompanionHostTransferNotices(items []ClientNotification) {
	shown := 0
	for _, item := range items {
		if item.IsRead || !isCompanionHostTransferNotification(item.Source) {
			continue
		}
		if a.announceCompanionShareNotification(item.ID, item.Title, item.Content) {
			shown++
			if shown >= 3 {
				return
			}
		}
	}
}
