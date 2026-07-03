package httpapi

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/notification"
	_ "modernc.org/sqlite"
)

func newHubCenterNotificationHandlerTestService(t *testing.T) (*NotificationHandlers, *notification.Service, *notification.SQLiteStore) {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	store := notification.NewSQLiteStore(db)
	if err := store.InitSchema(context.Background()); err != nil {
		t.Fatalf("init notification schema: %v", err)
	}

	svc := notification.NewService(store, nil, nil)
	return NewNotificationHandlers(svc), svc, store
}

func TestHubCenterDeleteNotificationRejectsPublishedAndDeletesRevoked(t *testing.T) {
	handler, svc, store := newHubCenterNotificationHandlerTestService(t)
	ctx := context.Background()

	item, err := svc.CreateNotification(ctx, notification.CreateRequest{
		Title:        "HubCenter delete lifecycle",
		Content:      "Published notifications must be revoked before delete.",
		Category:     notification.CategorySystemAnnouncement,
		Priority:     notification.PriorityNormal,
		AudienceType: notification.AudienceAll,
		Status:       notification.StatusPublished,
		CreatedBy:    "admin-http-delete-test",
	})
	if err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/notifications/"+item.ID, nil)
	req.SetPathValue("id", item.ID)
	rec := httptest.NewRecorder()
	handler.DeleteNotification(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected published delete HTTP 400, got %d body=%s", rec.Code, rec.Body.String())
	}

	if err := svc.RevokeNotification(ctx, item.ID); err != nil {
		t.Fatalf("RevokeNotification: %v", err)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/admin/notifications/"+item.ID, nil)
	req.SetPathValue("id", item.ID)
	rec = httptest.NewRecorder()
	handler.DeleteNotification(rec, req)
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
