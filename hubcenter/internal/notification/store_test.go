package notification

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// The SQLite store's INSERT/SELECT column lists must stay in lockstep with
// scanNotificationRow — a drifted placeholder count only fails at runtime,
// so a real-database roundtrip guards the pinned-card fields.

func newStoreTestDB(t *testing.T) *SQLiteStore {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store := NewSQLiteStore(db)
	if err := store.InitSchema(context.Background()); err != nil {
		t.Fatalf("init schema: %v", err)
	}
	return store
}

func TestSQLiteStorePinnedRoundtrip(t *testing.T) {
	store := newStoreTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC()

	in := &Notification{
		ID:            "hc-pin-1",
		Title:         "hc banner",
		Content:       "roundtrip",
		Category:      CategorySystemAnnouncement,
		Priority:      PriorityNormal,
		AudienceType:  AudienceAll,
		AudienceIDs:   []string{},
		Status:        StatusPublished,
		Pinned:        true,
		PinnedForever: true,
		LinkURL:       "https://example.com/launch",
		ButtonText:    "打开",
		CreatedBy:     "admin",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := store.Create(ctx, in); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := store.GetByID(ctx, in.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got == nil {
		t.Fatal("GetByID returned nil")
	}
	if !got.Pinned || !got.PinnedForever || got.LinkURL != in.LinkURL || got.ButtonText != in.ButtonText {
		t.Fatalf("roundtrip lost pinned fields: %+v", got)
	}

	// Upsert (HA apply path) must carry the fields through the conflict branch.
	in.ButtonText = "更新"
	if err := store.Upsert(ctx, in); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	got, err = store.GetByID(ctx, in.ID)
	if err != nil {
		t.Fatalf("GetByID after upsert: %v", err)
	}
	if got == nil || got.ButtonText != "更新" || !got.PinnedForever {
		t.Fatalf("upsert lost pinned fields: %+v", got)
	}

	// List shares the same SELECT list.
	items, total, err := store.List(ctx, ListFilter{Status: StatusPublished})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 1 || len(items) != 1 || !items[0].Pinned || items[0].LinkURL != in.LinkURL {
		t.Fatalf("List lost pinned fields: total=%d items=%+v", total, items)
	}
}
