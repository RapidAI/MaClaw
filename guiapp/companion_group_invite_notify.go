package guiapp

import (
	"log"
	"strings"
	"sync"
	"time"
)

// A reading-group invitation must reach the invitee even when they were offline
// when it was sent, and it must keep asking until they accept, decline, or
// ignore it. The Hub stores the invitation and pushes a notification for the
// bell, but the operating system's notification is raised here: the bell only
// exists on the assistant panel, and an invitee who is working elsewhere would
// never look at it.
//
// This watcher is the reliable delivery path. It polls the pending invite list
// with the same viewer token the companion library uses, so it works without a
// machine identity, and it keeps re-announcing an undecided invitation on a
// reminder interval.

const (
	companionGroupInviteNotificationSource = "companion_group_invite"
	// The bell also polls the same list. A minute apart keeps the reminder
	// timely without doubling the request rate.
	companionGroupInviteWatchInterval = 60 * time.Second
	// With nothing pending there is nothing to remind about, so the poll backs
	// off instead of running at full rate all day.
	companionGroupInviteIdleInterval   = 10 * time.Minute
	companionGroupInviteRemindInterval = 30 * time.Minute
)

var companionGroupInviteWatch struct {
	mu        sync.Mutex
	announced map[string]time.Time
	stop      chan struct{}
	running   bool
}

func init() {
	companionGroupInviteWatch.announced = map[string]time.Time{}
}

func isCompanionGroupInviteNotification(source string) bool {
	return strings.TrimSpace(source) == companionGroupInviteNotificationSource
}

// companionGroupInviteGroupID reads the group id out of a notice's source id,
// which the Hub sets to "<group id>:<user id>".
func companionGroupInviteGroupID(sourceID string) string {
	groupID, _, _ := strings.Cut(strings.TrimSpace(sourceID), ":")
	return strings.TrimSpace(groupID)
}

// startCompanionGroupInviteWatch begins polling pending invitations. Calling it
// twice is harmless; the second call keeps the running watcher.
func (a *App) startCompanionGroupInviteWatch() {
	if a == nil {
		return
	}
	companionGroupInviteWatch.mu.Lock()
	if companionGroupInviteWatch.running {
		companionGroupInviteWatch.mu.Unlock()
		return
	}
	stop := make(chan struct{})
	companionGroupInviteWatch.stop = stop
	companionGroupInviteWatch.running = true
	companionGroupInviteWatch.mu.Unlock()

	go func() {
		logCompanionGroupInviteWatchStart()
		// Announce immediately: an invitation that arrived while the app was
		// closed is waiting on the first poll.
		wait := companionGroupInviteWatchInterval
		if count, _ := a.pollCompanionGroupInvites(); count == 0 {
			wait = companionGroupInviteIdleInterval
		}
		for {
			select {
			case <-stop:
				return
			case <-time.After(wait):
			}
			count, _ := a.pollCompanionGroupInvites()
			if count > 0 {
				wait = companionGroupInviteWatchInterval
			} else {
				wait = companionGroupInviteIdleInterval
			}
		}
	}()
}

// stopCompanionGroupInviteWatch ends polling on shutdown.
func (a *App) stopCompanionGroupInviteWatch() {
	companionGroupInviteWatch.mu.Lock()
	defer companionGroupInviteWatch.mu.Unlock()
	if !companionGroupInviteWatch.running {
		return
	}
	companionGroupInviteWatch.running = false
	if companionGroupInviteWatch.stop != nil {
		close(companionGroupInviteWatch.stop)
		companionGroupInviteWatch.stop = nil
	}
}

// companionGroupInvitePendingList reads the pending invitations for the watcher.
// It is a variable so tests can pin both the answered-list and unreachable-Hub
// paths without a live Hub.
var companionGroupInvitePendingList = func(a *App) ([]FileCompanionGroupInvite, error) {
	hub, viewer, err := a.fileCompanionHubLogin()
	if err != nil {
		return nil, err
	}
	return a.fetchFileCompanionGroupInvites(hub, viewer)
}

// pollCompanionGroupInvites reads the pending list and raises a system
// notification for every invitation that is new or due for a reminder. It
// returns how many invitations the Hub reported.
func (a *App) pollCompanionGroupInvites() (int, error) {
	invites, err := companionGroupInvitePendingList(a)
	if err != nil {
		// A failed poll is not an empty inbox: keep the reminder state so a
		// transient network error does not silence a pending invitation.
		return 0, err
	}
	for _, invite := range invites {
		key := companionGroupInviteAnnounceKey(invite)
		if key == "" || !a.companionGroupInviteDue(key) {
			continue
		}
		a.announceCompanionGroupInvite(key, companionGroupInviteNoticeBody(invite))
	}
	a.pruneCompanionGroupInviteAnnouncements(invites)
	return len(invites), nil
}

// companionGroupInviteAnnounceKey identifies one invitation, not just the group.
// A re-invitation after the first was ignored carries a new invited_at, so it
// announces immediately instead of waiting out the reminder interval.
func companionGroupInviteAnnounceKey(invite FileCompanionGroupInvite) string {
	groupID := strings.TrimSpace(invite.ID)
	if groupID == "" {
		return ""
	}
	return groupID + "|" + strings.TrimSpace(invite.InvitedAt)
}

// companionGroupInviteNoticeBody is the sentence shown in the system notice.
func companionGroupInviteNoticeBody(invite FileCompanionGroupInvite) string {
	owner := strings.TrimSpace(invite.Owner)
	if owner == "" {
		owner = "一位用户"
	}
	name := strings.TrimSpace(invite.Name)
	if name == "" {
		name = "阅读群组"
	}
	return owner + " 邀请你加入阅读群组「" + name + "」。请在伴读的通知里接受、拒绝或忽略。"
}

// companionGroupInviteDue reports whether this invitation should be announced
// now: never announced, or last announced longer than the reminder interval ago.
func (a *App) companionGroupInviteDue(groupID string) bool {
	companionGroupInviteWatch.mu.Lock()
	defer companionGroupInviteWatch.mu.Unlock()
	last, seen := companionGroupInviteWatch.announced[groupID]
	return !seen || time.Since(last) >= companionGroupInviteRemindInterval
}

// announceCompanionGroupInvite raises the system notification for one
// invitation and records when it was shown. It returns true when a notice was
// actually shown. The reminder slot is claimed under the lock, so a poll and a
// push that land together cannot both notify.
func (a *App) announceCompanionGroupInvite(groupID, content string) bool {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" || ShowNotification == nil {
		return false
	}
	companionGroupInviteWatch.mu.Lock()
	if last, seen := companionGroupInviteWatch.announced[groupID]; seen && time.Since(last) < companionGroupInviteRemindInterval {
		companionGroupInviteWatch.mu.Unlock()
		return false
	}
	companionGroupInviteWatch.announced[groupID] = time.Now()
	companionGroupInviteWatch.mu.Unlock()

	body := strings.TrimSpace(content)
	if body == "" {
		body = "有人邀请你加入阅读群组，请在伴读的通知里处理。"
	}
	if FlashAndBeep != nil {
		FlashAndBeep()
	}
	// iconFlag 2 is the informational balloon: an invitation needs an answer,
	// not the error styling of 3.
	ShowNotification("入群邀请", trayBalloonBody(body), 2)
	return true
}

// pruneCompanionGroupInviteAnnouncements forgets invitations that are no longer
// pending, so a later re-invitation to the same group is announced at once. A
// fallback key from a notice that the list did not confirm is dropped too.
func (a *App) pruneCompanionGroupInviteAnnouncements(invites []FileCompanionGroupInvite) {
	live := make(map[string]struct{}, len(invites))
	for _, invite := range invites {
		if key := companionGroupInviteAnnounceKey(invite); key != "" {
			live[key] = struct{}{}
		}
	}
	companionGroupInviteWatch.mu.Lock()
	defer companionGroupInviteWatch.mu.Unlock()
	for key := range companionGroupInviteWatch.announced {
		if _, ok := live[key]; !ok {
			delete(companionGroupInviteWatch.announced, key)
		}
	}
}

// announceCompanionGroupInviteNotice re-reads the pending list when a pushed
// invitation notice arrives. The list is the only place with invited_at, and it
// tells a fresh invitation apart from one already announced.
func (a *App) announceCompanionGroupInviteNotice(notificationID, sourceID, content string) bool {
	if _, err := a.pollCompanionGroupInvites(); err == nil {
		// The list answered, so it is authoritative: it either announced the
		// invitation or already knew about it. The notice adds nothing.
		return false
	}
	// The list is unreachable. Fall back to the notice itself so the person is
	// still told, keyed by the group (or the notice id) to stay quiet on repeat.
	groupID := companionGroupInviteGroupID(sourceID)
	if groupID == "" {
		groupID = "notice:" + strings.TrimSpace(notificationID)
	}
	if !a.companionGroupInviteDue(groupID) {
		return false
	}
	return a.announceCompanionGroupInvite(groupID, content)
}

// announceCompanionGroupInviteNotices raises system notices for the invitation
// notices in a list pulled after connecting. The pending list is read once for
// the whole batch.
func (a *App) announceCompanionGroupInviteNotices(items []ClientNotification) {
	notices := make([]ClientNotification, 0, len(items))
	for _, item := range items {
		if isCompanionGroupInviteNotification(item.Source) {
			notices = append(notices, item)
		}
	}
	if len(notices) == 0 {
		return
	}
	if _, err := a.pollCompanionGroupInvites(); err == nil {
		return
	}
	for _, notice := range notices {
		groupID := companionGroupInviteGroupID(notice.SourceID)
		if groupID == "" {
			groupID = "notice:" + strings.TrimSpace(notice.ID)
		}
		if !a.companionGroupInviteDue(groupID) {
			continue
		}
		a.announceCompanionGroupInvite(groupID, notice.Content)
	}
}

// logCompanionGroupInviteWatchStart records the watcher starting once, so a
// missing poll is visible in the log.
func logCompanionGroupInviteWatchStart() {
	log.Printf("[companion-group] invite watcher started")
}
