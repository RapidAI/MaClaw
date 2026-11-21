package notification

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Pinned (置顶) message behaviour: the GUI banner shows ONE permanent pin
// leftmost plus the latest 3 regular pinned messages. Read state is ignored;
// revoked/expired/unpublished rows never come back from the query.

func createPinnedTestNotification(t *testing.T, svc *Service, ctx context.Context, title string, pinned, pinnedForever bool, createdAt time.Time) *Notification {
	t.Helper()
	n, err := svc.CreateNotification(ctx, CreateRequest{
		Title:         title,
		Content:       "content of " + title,
		Category:      CategorySystemAnnouncement,
		Priority:      PriorityNormal,
		AudienceType:  AudienceAll,
		Pinned:        pinned,
		PinnedForever: pinnedForever,
	})
	if err != nil {
		t.Fatalf("create notification %q: %v", title, err)
	}
	if err := svc.PublishNotification(ctx, n.ID); err != nil {
		t.Fatalf("publish notification %q: %v", title, err)
	}
	// CreateNotification/Publish stamp their own timestamps; pin ordering is
	// defined by created_at, so backdate after publish for deterministic tests.
	if err := svc.store.UpdateStatus(ctx, n.ID, StatusPublished); err != nil {
		t.Fatalf("touch status: %v", err)
	}
	_, err = svc.store.db.ExecContext(ctx,
		`UPDATE admin_notifications SET created_at = ?, updated_at = ? WHERE id = ?`,
		createdAt.UTC().Format(time.RFC3339), createdAt.UTC().Format(time.RFC3339), n.ID)
	if err != nil {
		t.Fatalf("backdate notification %q: %v", title, err)
	}
	return n
}

func TestGetPinnedForMachineOrdersPermanentFirstThenLatestThree(t *testing.T) {
	svc, store, _ := setupTestService(t)
	ctx := context.Background()
	seedMachineUser(t, store, "machine-001", "user-001", "tenant_default")

	base := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	// Five regular pinned messages — only the latest three survive the cap.
	createPinnedTestNotification(t, svc, ctx, "pin-old-1", true, false, base)
	createPinnedTestNotification(t, svc, ctx, "pin-old-2", true, false, base.Add(time.Hour))
	createPinnedTestNotification(t, svc, ctx, "pin-1", true, false, base.Add(2*time.Hour))
	createPinnedTestNotification(t, svc, ctx, "pin-2", true, false, base.Add(3*time.Hour))
	createPinnedTestNotification(t, svc, ctx, "pin-3", true, false, base.Add(4*time.Hour))
	// The permanent pin outranks them all regardless of age.
	createPinnedTestNotification(t, svc, ctx, "permanent", true, true, base.Add(-24*time.Hour))
	// Plain notifications must never appear in the pinned list.
	createPinnedTestNotification(t, svc, ctx, "plain", false, false, base.Add(5*time.Hour))

	pinned, err := svc.GetPinnedForMachine(ctx, "machine-001")
	if err != nil {
		t.Fatalf("GetPinnedForMachine: %v", err)
	}
	if len(pinned) != 4 {
		t.Fatalf("expected 4 pinned rows (1 permanent + 3 latest), got %d: %+v", len(pinned), pinned)
	}
	if pinned[0].Title != "permanent" {
		t.Errorf("permanent pin must be first, got %q", pinned[0].Title)
	}
	want := []string{"pin-3", "pin-2", "pin-1"}
	for i, title := range want {
		if pinned[i+1].Title != title {
			t.Errorf("pinned[%d] = %q, want %q", i+1, pinned[i+1].Title, title)
		}
	}
}

func TestGetPinnedForMachineExcludesRevokedAndExpired(t *testing.T) {
	svc, store, _ := setupTestService(t)
	ctx := context.Background()
	seedMachineUser(t, store, "machine-001", "user-001", "tenant_default")

	base := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	revoked := createPinnedTestNotification(t, svc, ctx, "revoked-pin", true, false, base)
	if err := svc.RevokeNotification(ctx, revoked.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	expired := createPinnedTestNotification(t, svc, ctx, "expired-pin", true, false, base.Add(time.Hour))
	past := base.Add(time.Hour).UTC().Format(time.RFC3339)
	if _, err := store.db.ExecContext(ctx,
		`UPDATE admin_notifications SET expire_at = ? WHERE id = ?`, past, expired.ID); err != nil {
		t.Fatalf("expire: %v", err)
	}
	createPinnedTestNotification(t, svc, ctx, "keep-pin", true, false, base.Add(2*time.Hour))

	pinned, err := svc.GetPinnedForMachine(ctx, "machine-001")
	if err != nil {
		t.Fatalf("GetPinnedForMachine: %v", err)
	}
	if len(pinned) != 1 || pinned[0].Title != "keep-pin" {
		t.Fatalf("expected only keep-pin, got %+v", pinned)
	}
}

func TestCreateNotificationDefaultsPinnedButtonText(t *testing.T) {
	svc, _, _ := setupTestService(t)
	ctx := context.Background()

	n, err := svc.CreateNotification(ctx, CreateRequest{
		Title:        "banner",
		Content:      "c",
		Category:     CategorySystemAnnouncement,
		AudienceType: AudienceAll,
		Pinned:       true,
		LinkURL:      "https://example.com/launch",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if n.ButtonText != DefaultPinnedButtonText {
		t.Errorf("button text default = %q, want %q", n.ButtonText, DefaultPinnedButtonText)
	}
	if n.LinkURL != "https://example.com/launch" {
		t.Errorf("link url = %q", n.LinkURL)
	}
}

func TestValidatePinnedFields(t *testing.T) {
	// A pinned message without a button label gets the default.
	req := CreateRequest{Pinned: true}
	if err := validatePinnedFields(&req); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if req.ButtonText != DefaultPinnedButtonText || !req.Pinned {
		t.Errorf("default not applied: %+v", req)
	}
	// A permanent pin implies pinned.
	req = CreateRequest{PinnedForever: true}
	if err := validatePinnedFields(&req); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !req.Pinned {
		t.Errorf("pinned_forever must imply pinned")
	}
	// Non-http links are rejected.
	req = CreateRequest{Pinned: true, LinkURL: "javascript:alert(1)"}
	if err := validatePinnedFields(&req); err == nil || !strings.Contains(err.Error(), "http") {
		t.Errorf("javascript: link must be rejected, got %v", err)
	}
	// Button text over the cap is rejected.
	req = CreateRequest{Pinned: true, ButtonText: strings.Repeat("长", 21)}
	if err := validatePinnedFields(&req); err == nil {
		t.Errorf("over-long button_text must be rejected")
	}
	// The default label follows the source language. An empty source stays
	// Simplified Chinese, which the case above already checks.
	req = CreateRequest{Pinned: true, SourceLang: LangEn}
	if err := validatePinnedFields(&req); err != nil {
		t.Fatalf("validate en: %v", err)
	}
	if req.ButtonText != "View details" {
		t.Errorf("english default = %q", req.ButtonText)
	}
	req = CreateRequest{Pinned: true, SourceLang: "zh-TW"}
	if err := validatePinnedFields(&req); err != nil {
		t.Fatalf("validate hant: %v", err)
	}
	if req.ButtonText != "查看詳情" {
		t.Errorf("traditional default = %q", req.ButtonText)
	}
	req = CreateRequest{Pinned: true, SourceLang: "mn-Cyrl"}
	if err := validatePinnedFields(&req); err != nil {
		t.Fatalf("validate mn: %v", err)
	}
	if req.ButtonText != "Дэлгэрэнгүй харах" {
		t.Errorf("mongolian default = %q", req.ButtonText)
	}
	// An admin-written label is kept, and a notice that is not pinned stays empty.
	req = CreateRequest{Pinned: true, SourceLang: LangEn, ButtonText: "Open"}
	if err := validatePinnedFields(&req); err != nil {
		t.Fatalf("validate custom: %v", err)
	}
	if req.ButtonText != "Open" {
		t.Errorf("custom button = %q", req.ButtonText)
	}
	req = CreateRequest{SourceLang: LangEn}
	if err := validatePinnedFields(&req); err != nil {
		t.Fatalf("validate unpinned: %v", err)
	}
	if req.ButtonText != "" {
		t.Errorf("unpinned button = %q", req.ButtonText)
	}
}

func TestCascadePreservesPinnedFields(t *testing.T) {
	svc, store, broadcaster := setupTestService(t)
	ctx := context.Background()
	seedMachineUser(t, store, "machine-001", "user-001", "tenant_default")

	incoming := &Notification{
		ID:            "hc-notif-1",
		Title:         "hc banner",
		Content:       "hc content",
		Category:      CategorySystemAnnouncement,
		Priority:      PriorityNormal,
		AudienceType:  AudienceAll,
		AudienceIDs:   []string{},
		Status:        StatusPublished,
		Pinned:        true,
		PinnedForever: true,
		LinkURL:       "https://example.com/x",
		ButtonText:    "打开",
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}
	if err := svc.CreateFromCascade(ctx, CascadeRequest{Notification: incoming}); err != nil {
		t.Fatalf("CreateFromCascade: %v", err)
	}

	pinned, err := svc.GetPinnedForMachine(ctx, "machine-001")
	if err != nil {
		t.Fatalf("GetPinnedForMachine: %v", err)
	}
	if len(pinned) != 1 {
		t.Fatalf("expected the cascaded permanent pin, got %d rows", len(pinned))
	}
	got := pinned[0]
	if !got.Pinned || !got.PinnedForever || got.LinkURL != incoming.LinkURL || got.ButtonText != incoming.ButtonText {
		t.Errorf("cascade lost pinned fields: %+v", got)
	}
	if got.Source != "hubcenter" || got.SourceID != incoming.ID {
		t.Errorf("cascade source tags wrong: %q/%q", got.Source, got.SourceID)
	}
	if len(broadcaster.envelopes) == 0 {
		t.Errorf("cascaded pinned notification must broadcast to clients")
	}
}

func TestUnreadExcludesPinnedRows(t *testing.T) {
	svc, store, _ := setupTestService(t)
	ctx := context.Background()
	seedMachineUser(t, store, "machine-001", "user-001", "tenant_default")

	base := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	createPinnedTestNotification(t, svc, ctx, "banner-pin", true, false, base)
	createPinnedTestNotification(t, svc, ctx, "permanent-pin", true, true, base.Add(time.Hour))
	createPinnedTestNotification(t, svc, ctx, "bell-plain", false, false, base.Add(2*time.Hour))

	unread, err := svc.GetUnreadForMachine(ctx, "machine-001", 10)
	if err != nil {
		t.Fatalf("GetUnreadForMachine: %v", err)
	}
	if len(unread) != 1 || unread[0].Title != "bell-plain" {
		t.Fatalf("unread must carry only the plain row (pinned rides the banner), got %+v", unread)
	}

	// MarkAllRead walks the same unread query — pinned rows are not touched.
	if err := svc.MarkAllRead(ctx, "machine-001"); err != nil {
		t.Fatalf("MarkAllRead: %v", err)
	}
	pinned, err := svc.GetPinnedForMachine(ctx, "machine-001")
	if err != nil {
		t.Fatalf("GetPinnedForMachine after MarkAllRead: %v", err)
	}
	if len(pinned) != 2 {
		t.Fatalf("MarkAllRead must not consume pinned rows, got %+v", pinned)
	}
}

// The test DB only has `machines` (no users/org_members). The unread query
// must fall back to the legacy variant instead of surfacing the missing-table
// error as a 500 — same fallback contract the pinned query follows.
func TestGetUnreadForMachineFallsBackWithoutUsersTable(t *testing.T) {
	svc, _, _ := setupTestService(t)
	ctx := context.Background()

	req := CreateRequest{
		Title:        "plain notice",
		Content:      "unread fallback",
		Category:     CategorySystemAnnouncement,
		Priority:     PriorityNormal,
		AudienceType: AudienceAll,
	}
	n, err := svc.CreateNotification(ctx, req)
	if err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}
	if err := svc.PublishNotification(ctx, n.ID); err != nil {
		t.Fatalf("PublishNotification: %v", err)
	}

	unread, err := svc.GetUnreadForMachine(ctx, "machine-001", 10)
	if err != nil {
		t.Fatalf("GetUnreadForMachine without users table: %v", err)
	}
	if len(unread) != 1 || unread[0].Title != "plain notice" {
		t.Fatalf("legacy fallback must still return the row, got %+v", unread)
	}
}
