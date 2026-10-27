package guiapp

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// resetCompanionGroupInviteWatch clears reminder state between tests.
func resetCompanionGroupInviteWatch(t *testing.T) {
	t.Helper()
	companionGroupInviteWatch.mu.Lock()
	companionGroupInviteWatch.announced = map[string]time.Time{}
	companionGroupInviteWatch.mu.Unlock()
	t.Cleanup(func() {
		companionGroupInviteWatch.mu.Lock()
		companionGroupInviteWatch.announced = map[string]time.Time{}
		companionGroupInviteWatch.mu.Unlock()
	})
}

// TestCompanionGroupInviteAnnouncedOnce covers the "tell them, but do not
// repeat on every poll" half of the reminder rule.
func TestCompanionGroupInviteAnnouncedOnce(t *testing.T) {
	resetCompanionGroupInviteWatch(t)
	notifs, flashes, restore := stubTrayNotifyEnv(t, false)
	defer restore()
	a := &App{}

	invite := FileCompanionGroupInvite{ID: "cg_0123456789abcdef0123456789abcdef", Name: "论文组", Owner: "owner@example.com", InvitedAt: "2026-10-01T00:00:00Z"}
	key := companionGroupInviteAnnounceKey(invite)
	body := companionGroupInviteNoticeBody(invite)
	if !strings.Contains(body, "论文组") || !strings.Contains(body, "owner@example.com") {
		t.Fatalf("notice body %q", body)
	}

	if !a.announceCompanionGroupInvite(key, body) {
		t.Fatal("first announcement was skipped")
	}
	if a.announceCompanionGroupInvite(key, body) {
		t.Fatal("second announcement was not suppressed")
	}
	if len(*notifs) != 1 {
		t.Fatalf("notifications %d want 1", len(*notifs))
	}
	if (*notifs)[0].title != "入群邀请" {
		t.Fatalf("title %q", (*notifs)[0].title)
	}
	if (*notifs)[0].icon != 2 {
		t.Fatalf("icon flag %d want 2 (informational)", (*notifs)[0].icon)
	}
	if *flashes != 1 {
		t.Fatalf("flashes %d want 1", *flashes)
	}
}

// TestCompanionGroupInviteRearmAnnouncesAgain covers a re-invitation after the
// first one was ignored: a new invited_at must produce a new notice at once.
func TestCompanionGroupInviteRearmAnnouncesAgain(t *testing.T) {
	resetCompanionGroupInviteWatch(t)
	notifs, _, restore := stubTrayNotifyEnv(t, false)
	defer restore()
	a := &App{}
	first := FileCompanionGroupInvite{ID: "cg_0123456789abcdef0123456789abcdef", Name: "论文组", InvitedAt: "2026-10-01T00:00:00Z"}
	second := first
	second.InvitedAt = "2026-10-02T00:00:00Z"

	if !a.announceCompanionGroupInvite(companionGroupInviteAnnounceKey(first), "") {
		t.Fatal("first invite was not announced")
	}
	if a.announceCompanionGroupInvite(companionGroupInviteAnnounceKey(first), "") {
		t.Fatal("same invitation was announced twice")
	}
	if !a.announceCompanionGroupInvite(companionGroupInviteAnnounceKey(second), "") {
		t.Fatal("re-invitation was not announced")
	}
	if len(*notifs) != 2 {
		t.Fatalf("notifications %d want 2", len(*notifs))
	}
}

// TestCompanionGroupInviteRemindedUntilDecided covers "keep notifying until the
// person answers": an undecided invitation is announced again after the
// reminder interval.
func TestCompanionGroupInviteRemindedUntilDecided(t *testing.T) {
	resetCompanionGroupInviteWatch(t)
	notifs, _, restore := stubTrayNotifyEnv(t, false)
	defer restore()
	a := &App{}
	id := "cg_0123456789abcdef0123456789abcdef|2026-10-01T00:00:00Z"

	if !a.companionGroupInviteDue(id) {
		t.Fatal("a never-announced invite is not due")
	}
	a.announceCompanionGroupInvite(id, "")
	if a.companionGroupInviteDue(id) {
		t.Fatal("a just-announced invite is due again immediately")
	}

	// Pretend the reminder interval has passed.
	companionGroupInviteWatch.mu.Lock()
	companionGroupInviteWatch.announced[id] = time.Now().Add(-companionGroupInviteRemindInterval - time.Minute)
	companionGroupInviteWatch.mu.Unlock()
	if !a.companionGroupInviteDue(id) {
		t.Fatal("an overdue invite is not due")
	}
	a.announceCompanionGroupInvite(id, "")
	if len(*notifs) != 2 {
		t.Fatalf("notifications %d want 2", len(*notifs))
	}
}

// TestCompanionGroupInviteForgottenWhenSettled covers a decided invitation: once
// the row leaves the pending list, a later re-invitation is announced at once.
func TestCompanionGroupInviteForgottenWhenSettled(t *testing.T) {
	resetCompanionGroupInviteWatch(t)
	_, _, restore := stubTrayNotifyEnv(t, false)
	defer restore()
	a := &App{}
	invite := FileCompanionGroupInvite{ID: "cg_0123456789abcdef0123456789abcdef", Name: "论文组", InvitedAt: "2026-10-01T00:00:00Z"}
	key := companionGroupInviteAnnounceKey(invite)

	a.announceCompanionGroupInvite(key, "")
	if a.companionGroupInviteDue(key) {
		t.Fatal("just-announced invite should not be due")
	}
	// The invitee accepted; the invite list no longer contains the group.
	a.pruneCompanionGroupInviteAnnouncements(nil)
	if !a.companionGroupInviteDue(key) {
		t.Fatal("a settled invite stayed remembered")
	}
}

// TestCompanionGroupInviteNoticesAnnouncedFromPull covers the offline path: an
// stubCompanionGroupInviteList pins the pending-list read for the watcher.
func stubCompanionGroupInviteList(t *testing.T, fn func(*App) ([]FileCompanionGroupInvite, error)) {
	t.Helper()
	original := companionGroupInvitePendingList
	companionGroupInvitePendingList = fn
	t.Cleanup(func() { companionGroupInvitePendingList = original })
}

// TestCompanionGroupInviteNoticesAnnouncedFromPull covers the offline path: when
// the pending list cannot be read, a pulled notice is announced directly.
func TestCompanionGroupInviteNoticesAnnouncedFromPull(t *testing.T) {
	resetCompanionGroupInviteWatch(t)
	notifs, _, restore := stubTrayNotifyEnv(t, false)
	defer restore()
	unreachable := errors.New("暂时无法连接云端")
	stubCompanionGroupInviteList(t, func(*App) ([]FileCompanionGroupInvite, error) {
		return nil, unreachable
	})
	a := &App{}
	groupID := "cg_0123456789abcdef0123456789abcdef"

	a.announceCompanionGroupInviteNotices([]ClientNotification{
		{ID: "n1", Title: "入群邀请", Content: "owner@example.com 邀请你加入阅读群组「论文组」。", Source: companionGroupInviteNotificationSource, SourceID: groupID + ":user-1"},
		// An ordinary announcement is not an invitation.
		{ID: "n2", Title: "系统维护", Content: "今晚维护。", Source: "hub"},
	})
	if len(*notifs) != 1 {
		t.Fatalf("notifications %d want 1", len(*notifs))
	}
	if !strings.Contains((*notifs)[0].msg, "论文组") {
		t.Fatalf("body %q", (*notifs)[0].msg)
	}
	// The same invitation delivered again stays quiet.
	a.announceCompanionGroupInviteNotices([]ClientNotification{
		{ID: "n1", Title: "入群邀请", Content: "x", Source: companionGroupInviteNotificationSource, SourceID: groupID + ":user-1"},
	})
	if len(*notifs) != 1 {
		t.Fatalf("repeat pull made %d notifications", len(*notifs))
	}
}

// TestCompanionGroupInviteListIsAuthoritative pins the other half: when the list
// answers, a pulled notice must not announce a second time on top of it.
func TestCompanionGroupInviteListIsAuthoritative(t *testing.T) {
	resetCompanionGroupInviteWatch(t)
	notifs, _, restore := stubTrayNotifyEnv(t, false)
	defer restore()
	groupID := "cg_0123456789abcdef0123456789abcdef"
	stubCompanionGroupInviteList(t, func(*App) ([]FileCompanionGroupInvite, error) {
		return []FileCompanionGroupInvite{{ID: groupID, Name: "论文组", InvitedAt: "2026-10-01T00:00:00Z"}}, nil
	})
	a := &App{}

	a.announceCompanionGroupInviteNotices([]ClientNotification{
		{ID: "n1", Title: "入群邀请", Content: "x", Source: companionGroupInviteNotificationSource, SourceID: groupID + ":user-1"},
	})
	if len(*notifs) != 1 {
		t.Fatalf("notifications %d want 1 (the list's own announcement)", len(*notifs))
	}
	// A second delivery of the same push must not add another.
	a.announceCompanionGroupInviteNotice("n1", groupID+":user-1", "x")
	if len(*notifs) != 1 {
		t.Fatalf("repeat delivery made %d notifications", len(*notifs))
	}
}

// TestCompanionGroupInviteGroupIDFromSourceID pins the source id shape.
func TestCompanionGroupInviteGroupIDFromSourceID(t *testing.T) {
	groupID := "cg_0123456789abcdef0123456789abcdef"
	if got := companionGroupInviteGroupID(groupID + ":user-1"); got != groupID {
		t.Fatalf("group id %q want %q", got, groupID)
	}
	if got := companionGroupInviteGroupID("  "); got != "" {
		t.Fatalf("blank source id gave %q", got)
	}
}
