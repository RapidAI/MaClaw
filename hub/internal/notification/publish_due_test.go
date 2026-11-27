package notification

import (
	"context"
	"testing"
	"time"
)

func TestPublishDueReleasesScheduledDraftOnce(t *testing.T) {
	svc, store, broadcaster := setupTestService(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	future := now.Add(time.Hour)
	past := now.Add(-time.Minute)
	expirePast := now.Add(-time.Second)

	scheduled := draftNotif("sched", &future, nil, now)
	manual := draftNotif("manual", nil, nil, now)
	expired := draftNotif("expired", &past, &expirePast, now)
	for _, n := range []*Notification{scheduled, manual, expired} {
		if err := store.Create(ctx, n); err != nil {
			t.Fatalf("create %s: %v", n.ID, err)
		}
	}

	if err := svc.PublishDue(ctx, now); err != nil {
		t.Fatalf("PublishDue early: %v", err)
	}
	assertStatus(t, store, "sched", StatusDraft)
	assertStatus(t, store, "manual", StatusDraft)
	assertStatus(t, store, "expired", StatusExpired)
	if got := len(broadcaster.getEnvelopes()); got != 0 {
		t.Fatalf("early broadcasts = %d", got)
	}

	if err := svc.PublishDue(ctx, future.Add(time.Minute)); err != nil {
		t.Fatalf("PublishDue due: %v", err)
	}
	assertStatus(t, store, "sched", StatusPublished)
	assertStatus(t, store, "manual", StatusDraft)
	if got := len(broadcaster.getEnvelopes()); got != 1 {
		t.Fatalf("broadcasts = %d", got)
	}

	if err := svc.PublishDue(ctx, future.Add(time.Minute)); err != nil {
		t.Fatalf("PublishDue again: %v", err)
	}
	if got := len(broadcaster.getEnvelopes()); got != 1 {
		t.Fatalf("second pass broadcasts = %d", got)
	}
}

func draftNotif(id string, publishAt, expireAt *time.Time, now time.Time) *Notification {
	return &Notification{
		ID:           id,
		Title:        id,
		Content:      "body",
		Category:     CategorySystemAnnouncement,
		Priority:     PriorityNormal,
		AudienceType: AudienceAll,
		AudienceIDs:  []string{},
		Status:       StatusDraft,
		Source:       "hub",
		PublishAt:    publishAt,
		ExpireAt:     expireAt,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

func assertStatus(t *testing.T, store *Store, id string, want Status) {
	t.Helper()
	got, err := store.GetByID(context.Background(), id)
	if err != nil {
		t.Fatalf("get %s: %v", id, err)
	}
	if got == nil || got.Status != want {
		t.Fatalf("%s status = %#v, want %s", id, got, want)
	}
}
