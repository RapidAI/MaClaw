package notification

import (
	"context"
	"testing"
	"time"
)

type memoryNotificationStore struct {
	created []*Notification
}

func (s *memoryNotificationStore) Create(ctx context.Context, n *Notification) error {
	copy := *n
	s.created = append(s.created, &copy)
	return nil
}

func (s *memoryNotificationStore) GetByID(ctx context.Context, id string) (*Notification, error) {
	return nil, nil
}

func (s *memoryNotificationStore) List(ctx context.Context, filter ListFilter) ([]*Notification, int, error) {
	return nil, 0, nil
}

func (s *memoryNotificationStore) UpdateStatus(ctx context.Context, id string, status Status, updatedAt time.Time) error {
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
