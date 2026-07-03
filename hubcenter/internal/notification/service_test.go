package notification

import (
	"context"
	"testing"
	"time"
)

type memoryNotificationStore struct {
	created []*Notification
	items   map[string]*Notification
}

type recordingSyncRecorder struct {
	upserts []*Notification
	deletes []string
}

func (r *recordingSyncRecorder) AppendNotification(ctx context.Context, item *Notification) {
	copy := *item
	r.upserts = append(r.upserts, &copy)
}

func (r *recordingSyncRecorder) DeleteNotification(ctx context.Context, id string) {
	r.deletes = append(r.deletes, id)
}

func (s *memoryNotificationStore) Create(ctx context.Context, n *Notification) error {
	if s.items == nil {
		s.items = map[string]*Notification{}
	}
	copy := *n
	s.created = append(s.created, &copy)
	s.items[n.ID] = &copy
	return nil
}

func (s *memoryNotificationStore) Upsert(ctx context.Context, n *Notification) error {
	if s.items == nil {
		s.items = map[string]*Notification{}
	}
	copy := *n
	s.items[n.ID] = &copy
	return nil
}

func (s *memoryNotificationStore) GetByID(ctx context.Context, id string) (*Notification, error) {
	if s.items != nil {
		if item := s.items[id]; item != nil {
			copy := *item
			return &copy, nil
		}
	}
	return nil, nil
}

func (s *memoryNotificationStore) List(ctx context.Context, filter ListFilter) ([]*Notification, int, error) {
	return nil, 0, nil
}

func (s *memoryNotificationStore) UpdateStatus(ctx context.Context, id string, status Status, updatedAt time.Time) error {
	if s.items != nil {
		if item := s.items[id]; item != nil {
			item.Status = status
			item.UpdatedAt = updatedAt
		}
	}
	return nil
}

func (s *memoryNotificationStore) Delete(ctx context.Context, id string) error {
	if s.items != nil {
		delete(s.items, id)
	}
	return nil
}

func (s *memoryNotificationStore) RecordCascadeResult(ctx context.Context, result *CascadeResult) error {
	return nil
}

func (s *memoryNotificationStore) GetCascadeResults(ctx context.Context, notificationID string) ([]*CascadeResult, error) {
	return nil, nil
}

func TestCreateNotificationHonorsDraftStatus(t *testing.T) {
	store := &memoryNotificationStore{}
	svc := NewService(store, nil, nil)

	notif, err := svc.CreateNotification(context.Background(), CreateRequest{
		Title:        "Scheduled maintenance",
		Content:      "Service will restart tonight.",
		Category:     CategoryMaintenance,
		Priority:     PriorityNormal,
		AudienceType: AudienceAll,
		Status:       StatusDraft,
	})
	if err != nil {
		t.Fatalf("CreateNotification returned error: %v", err)
	}
	if notif.Status != StatusDraft {
		t.Fatalf("status = %q, want %q", notif.Status, StatusDraft)
	}
	if len(store.created) != 1 || store.created[0].Status != StatusDraft {
		t.Fatalf("persisted status = %#v, want draft", store.created)
	}
}

func TestCreateNotificationRejectsInvalidStatus(t *testing.T) {
	store := &memoryNotificationStore{}
	svc := NewService(store, nil, nil)

	_, err := svc.CreateNotification(context.Background(), CreateRequest{
		Title:        "Bad status",
		Content:      "This should not be accepted.",
		Category:     CategoryCustom,
		AudienceType: AudienceAll,
		Status:       Status("archived"),
	})
	if err != ErrInvalidStatus {
		t.Fatalf("error = %v, want %v", err, ErrInvalidStatus)
	}
}

func TestCreateNotificationPublishedWithoutCascadeDependencyDoesNotPanic(t *testing.T) {
	store := &memoryNotificationStore{}
	svc := NewService(store, nil, nil)

	notif, err := svc.CreateNotification(context.Background(), CreateRequest{
		Title:        "Immediate notice",
		Content:      "Publish now.",
		Category:     CategorySystemAnnouncement,
		AudienceType: AudienceAll,
		Status:       StatusPublished,
	})
	if err != nil {
		t.Fatalf("CreateNotification returned error: %v", err)
	}
	if notif.Status != StatusPublished {
		t.Fatalf("status = %q, want %q", notif.Status, StatusPublished)
	}

	time.Sleep(10 * time.Millisecond)
}

func TestNotificationServiceRecordsHASyncLifecycle(t *testing.T) {
	store := &memoryNotificationStore{}
	recorder := &recordingSyncRecorder{}
	svc := NewService(store, nil, nil)
	svc.SetSyncRecorder(recorder)
	ctx := context.Background()

	notif, err := svc.CreateNotification(ctx, CreateRequest{
		Title:        "Lifecycle notice",
		Content:      "Create, revoke, then delete.",
		Category:     CategorySystemAnnouncement,
		AudienceType: AudienceAll,
		Status:       StatusPublished,
	})
	if err != nil {
		t.Fatalf("CreateNotification returned error: %v", err)
	}
	if len(recorder.upserts) != 1 || recorder.upserts[0].ID != notif.ID || recorder.upserts[0].Status != StatusPublished {
		t.Fatalf("create sync upserts = %#v, want published notification", recorder.upserts)
	}

	if err := svc.RevokeNotification(ctx, notif.ID); err != nil {
		t.Fatalf("RevokeNotification returned error: %v", err)
	}
	if len(recorder.upserts) != 2 || recorder.upserts[1].Status != StatusRevoked {
		t.Fatalf("revoke sync upserts = %#v, want revoked notification", recorder.upserts)
	}

	if err := svc.DeleteNotification(ctx, notif.ID); err != nil {
		t.Fatalf("DeleteNotification returned error: %v", err)
	}
	if len(recorder.deletes) != 1 || recorder.deletes[0] != notif.ID {
		t.Fatalf("delete sync = %#v, want %q", recorder.deletes, notif.ID)
	}
}

func TestDeleteNotificationRequiresInactiveStatus(t *testing.T) {
	store := &memoryNotificationStore{}
	svc := NewService(store, nil, nil)
	ctx := context.Background()

	notif, err := svc.CreateNotification(ctx, CreateRequest{
		Title:        "Delete lifecycle",
		Content:      "Published notifications must be revoked first.",
		Category:     CategorySystemAnnouncement,
		AudienceType: AudienceAll,
		Status:       StatusPublished,
	})
	if err != nil {
		t.Fatalf("CreateNotification returned error: %v", err)
	}
	if err := svc.DeleteNotification(ctx, notif.ID); err != ErrCannotDelete {
		t.Fatalf("DeleteNotification published error = %v, want %v", err, ErrCannotDelete)
	}

	if err := store.UpdateStatus(ctx, notif.ID, StatusRevoked, time.Now().UTC()); err != nil {
		t.Fatalf("UpdateStatus returned error: %v", err)
	}
	if err := svc.DeleteNotification(ctx, notif.ID); err != nil {
		t.Fatalf("DeleteNotification revoked returned error: %v", err)
	}
	if got, err := store.GetByID(ctx, notif.ID); err != nil || got != nil {
		t.Fatalf("GetByID after delete = %#v, %v; want nil, nil", got, err)
	}
}
