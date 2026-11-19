package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/notification"
	_ "modernc.org/sqlite"
)

func newNotificationHandlerTestService(t *testing.T) (*NotificationHandler, *notification.Service, *notification.Store) {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	store := notification.NewStore(db)
	if err := store.InitSchema(context.Background()); err != nil {
		t.Fatalf("init notification schema: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS machines (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'online'
	)`); err != nil {
		t.Fatalf("create machines table: %v", err)
	}

	svc := notification.NewService(store, nil, nil)
	return NewNotificationHandler(svc), svc, store
}

func TestHandleDeleteNotificationRejectsPublishedAndDeletesRevoked(t *testing.T) {
	handler, svc, store := newNotificationHandlerTestService(t)
	ctx := context.Background()

	item, err := svc.CreateNotification(ctx, notification.CreateRequest{
		Title:        "HTTP delete notification",
		Content:      "Published notifications must be revoked before physical delete.",
		Category:     notification.CategorySystemAnnouncement,
		Priority:     notification.PriorityNormal,
		AudienceType: notification.AudienceAll,
		CreatedBy:    "admin-http-delete-test",
	})
	if err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}
	if err := svc.PublishNotification(ctx, item.ID); err != nil {
		t.Fatalf("PublishNotification: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/notifications/"+item.ID, nil)
	req.SetPathValue("id", item.ID)
	rec := httptest.NewRecorder()
	handler.HandleDeleteNotification(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected published delete HTTP 400, got %d body=%s", rec.Code, rec.Body.String())
	}

	if err := svc.RevokeNotification(ctx, item.ID); err != nil {
		t.Fatalf("RevokeNotification: %v", err)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/admin/notifications/"+item.ID, nil)
	req.SetPathValue("id", item.ID)
	rec = httptest.NewRecorder()
	handler.HandleDeleteNotification(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected revoked delete HTTP 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	deleted, err := store.GetByID(ctx, item.ID)
	if err != nil {
		t.Fatalf("GetByID after delete: %v", err)
	}
	if deleted != nil {
		t.Fatalf("expected notification to be deleted, got status=%s", deleted.Status)
	}
}

// HandlePinned is the endpoint the GUI banner polls. Its payload shape
// {"ok","items"} is parsed by guiapp's pinnedHubResponse — the contract test
// pins the shape, the permanent-first order, and the card fields.
func TestHandlePinnedReturnsPermanentFirstThenLatestPins(t *testing.T) {
	handler, svc, _ := newNotificationHandlerTestService(t)
	ctx := context.Background()

	seed := []notification.CreateRequest{
		{Title: "plain notice", Content: "c", Category: notification.CategoryCustom, AudienceType: notification.AudienceAll},
		{Title: "pin-old", Content: "c", Category: notification.CategorySystemAnnouncement, AudienceType: notification.AudienceAll, Pinned: true, LinkURL: "https://example.com/old"},
		{Title: "pin-new", Content: "c", Category: notification.CategorySystemAnnouncement, AudienceType: notification.AudienceAll, Pinned: true, ButtonText: "立即使用"},
		{Title: "permanent", Content: "c", Category: notification.CategoryFeatureUpdate, AudienceType: notification.AudienceAll, PinnedForever: true, LinkURL: "https://example.com/launch"},
	}
	created := make([]*notification.Notification, 0, len(seed))
	for _, req := range seed {
		n, err := svc.CreateNotification(ctx, req)
		if err != nil {
			t.Fatalf("CreateNotification %q: %v", req.Title, err)
		}
		if err := svc.PublishNotification(ctx, n.ID); err != nil {
			t.Fatalf("PublishNotification %q: %v", req.Title, err)
		}
		created = append(created, n)
	}

	// Machine auth missing → the handler must refuse.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/notifications/pinned", nil)
	rec := httptest.NewRecorder()
	handler.HandlePinned(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing machine auth: expected 401, got %d", rec.Code)
	}

	// Inject the machine identity the middleware would set.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/notifications/pinned", nil)
	req = req.WithContext(context.WithValue(req.Context(), machineIDContextKey{}, "machine-001"))
	rec = httptest.NewRecorder()
	handler.HandlePinned(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Ok    bool                              `json:"ok"`
		Items []notification.ClientNotification `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v body=%s", err, rec.Body.String())
	}
	if !resp.Ok {
		t.Fatal("response ok=false")
	}
	if len(resp.Items) != 3 {
		t.Fatalf("expected 3 pinned rows (plain excluded), got %d: %+v", len(resp.Items), resp.Items)
	}
	if resp.Items[0].Title != "permanent" || !resp.Items[0].PinnedForever {
		t.Fatalf("permanent pin must be first, got %+v", resp.Items[0])
	}
	if resp.Items[1].Title != "pin-new" {
		t.Fatalf("newest regular pin second, got %+v", resp.Items[1])
	}
	if resp.Items[1].ButtonText != "立即使用" {
		t.Fatalf("button text lost: %+v", resp.Items[1])
	}
	if resp.Items[2].Title != "pin-old" || resp.Items[2].LinkURL != "https://example.com/old" {
		t.Fatalf("oldest pin last with its link, got %+v", resp.Items[2])
	}
}
