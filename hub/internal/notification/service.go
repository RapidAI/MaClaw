package notification

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Service implements the notification business logic layer.
type Service struct {
	store  *Store
	wsHub  WSBroadcaster
	imPush IMPusher
}

// NewService creates a new notification Service.
func NewService(store *Store, wsBroadcaster WSBroadcaster, imPusher IMPusher) *Service {
	return &Service{
		store:  store,
		wsHub:  wsBroadcaster,
		imPush: imPusher,
	}
}

// ---------------------------------------------------------------------------
// CreateNotification
// ---------------------------------------------------------------------------

// CreateNotification validates input and persists a new notification.
func (s *Service) CreateNotification(ctx context.Context, req CreateRequest) (*Notification, error) {
	// Validate title
	if req.Title == "" {
		return nil, fmt.Errorf("title is required")
	}
	if utf8.RuneCountInString(req.Title) > 100 {
		return nil, fmt.Errorf("title exceeds 100 characters")
	}

	// Validate content
	if req.Content == "" {
		return nil, fmt.Errorf("content is required")
	}
	if utf8.RuneCountInString(req.Content) > 2000 {
		return nil, fmt.Errorf("content exceeds 2000 characters")
	}

	// Validate category
	if !ValidCategory(req.Category) {
		return nil, fmt.Errorf("invalid category: %s", req.Category)
	}

	// Validate audience type
	if !ValidAudienceType(req.AudienceType) {
		return nil, fmt.Errorf("invalid audience_type: %s", req.AudienceType)
	}

	// Validate/default priority
	if req.Priority == "" {
		req.Priority = PriorityNormal
	}
	if !ValidPriority(req.Priority) {
		return nil, fmt.Errorf("invalid priority: %s", req.Priority)
	}

	now := time.Now().UTC()
	n := &Notification{
		ID:           uuid.New().String(),
		Title:        req.Title,
		Content:      req.Content,
		Category:     req.Category,
		Priority:     req.Priority,
		AudienceType: req.AudienceType,
		AudienceIDs:  req.AudienceIDs,
		Status:       StatusDraft,
		IMPush:       req.IMPush,
		Source:       "hub",
		SourceID:     "",
		CreatedBy:    req.CreatedBy,
		PublishAt:    req.PublishAt,
		ExpireAt:     req.ExpireAt,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if n.AudienceIDs == nil {
		n.AudienceIDs = []string{}
	}

	if err := s.store.Create(ctx, n); err != nil {
		return nil, fmt.Errorf("persist notification: %w", err)
	}

	return n, nil
}

// ---------------------------------------------------------------------------
// PublishNotification
// ---------------------------------------------------------------------------

// PublishNotification sets the notification status to published, resolves audience,
// and broadcasts via WebSocket.
func (s *Service) PublishNotification(ctx context.Context, id string) error {
	n, err := s.store.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get notification: %w", err)
	}
	if n == nil {
		return fmt.Errorf("notification not found: %s", id)
	}

	// Update status to published
	if err := s.store.UpdateStatus(ctx, id, StatusPublished); err != nil {
		return fmt.Errorf("update status: %w", err)
	}
	n.Status = StatusPublished
	n.UpdatedAt = time.Now().UTC()

	// Resolve audience machine IDs
	machineIDs, err := s.resolveAudienceMachines(ctx, n)
	if err != nil {
		return fmt.Errorf("resolve audience: %w", err)
	}

	// Build push envelope
	payload := NotificationPushPayload{
		Action:       "new",
		Notification: n,
	}
	envelope, err := BuildNotificationEnvelope(payload)
	if err != nil {
		return fmt.Errorf("build envelope: %w", err)
	}

	// Broadcast — errors are logged but not propagated (push+pull dual guarantee:
	// offline machines will pull on reconnect, so broadcast failure is non-fatal).
	if s.wsHub != nil {
		if n.AudienceType == AudienceAll {
			if err := s.wsHub.BroadcastToAll(envelope); err != nil {
				log.Printf("[notification] BroadcastToAll failed for %s: %v", n.ID, err)
			}
		} else if len(machineIDs) > 0 {
			if err := s.wsHub.BroadcastToMachines(machineIDs, envelope); err != nil {
				log.Printf("[notification] BroadcastToMachines failed for %s (%d targets): %v", n.ID, len(machineIDs), err)
			}
		}
	}

	// Optional IM push for urgent notifications
	if n.IMPush && n.Priority == PriorityUrgent && s.imPush != nil && len(machineIDs) > 0 {
		if err := s.imPush.PushToIM(machineIDs, n.Title, n.Content); err != nil {
			log.Printf("[notification] IM push failed for %s: %v", n.ID, err)
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// RevokeNotification
// ---------------------------------------------------------------------------

// RevokeNotification sets the notification status to revoked and broadcasts
// a revoke message to all connected clients.
func (s *Service) RevokeNotification(ctx context.Context, id string) error {
	n, err := s.store.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get notification: %w", err)
	}
	if n == nil {
		return fmt.Errorf("notification not found: %s", id)
	}

	if err := s.store.UpdateStatus(ctx, id, StatusRevoked); err != nil {
		return fmt.Errorf("update status: %w", err)
	}

	// Broadcast revoke to all clients
	payload := NotificationPushPayload{
		Action:  "revoke",
		NotifID: id,
	}
	envelope, err := BuildNotificationEnvelope(payload)
	if err != nil {
		return fmt.Errorf("build envelope: %w", err)
	}

	if s.wsHub != nil {
		if err := s.wsHub.BroadcastToAll(envelope); err != nil {
			log.Printf("[notification] BroadcastToAll revoke failed for %s: %v", id, err)
		}
	}

	return nil
}

// DeleteNotification permanently removes a notification that is no longer
// active. Published notifications must be revoked first so clients receive the
// revoke event before the admin history entry is cleaned up.
func (s *Service) DeleteNotification(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("notification id is required")
	}

	n, err := s.store.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get notification: %w", err)
	}
	if n == nil {
		return fmt.Errorf("notification not found: %s", id)
	}
	if n.Status == StatusPublished {
		return fmt.Errorf("published notifications must be revoked before delete")
	}

	if err := s.store.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete notification: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// GetUnreadForMachine
// ---------------------------------------------------------------------------

// GetUnreadForMachine returns unread notifications for a machine as client views.
func (s *Service) GetUnreadForMachine(ctx context.Context, machineID string, limit int) ([]ClientNotification, error) {
	notifications, err := s.store.GetUnreadForMachine(ctx, machineID)
	if err != nil {
		return nil, fmt.Errorf("get unread: %w", err)
	}

	// Apply limit
	if limit > 0 && len(notifications) > limit {
		notifications = notifications[:limit]
	}

	result := make([]ClientNotification, 0, len(notifications))
	for _, n := range notifications {
		result = append(result, ClientNotification{
			ID:        n.ID,
			Title:     n.Title,
			Content:   n.Content,
			Category:  string(n.Category),
			Priority:  string(n.Priority),
			IsRead:    false,
			CreatedAt: n.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// ListNotifications
// ---------------------------------------------------------------------------

// ListNotifications delegates to the store's List method with filtering.
func (s *Service) ListNotifications(ctx context.Context, filter ListFilter) ([]*Notification, error) {
	return s.store.List(ctx, filter)
}

// ---------------------------------------------------------------------------
// GetByID
// ---------------------------------------------------------------------------

// GetByID retrieves a single notification by ID.
func (s *Service) GetByID(ctx context.Context, id string) (*Notification, error) {
	return s.store.GetByID(ctx, id)
}

// ---------------------------------------------------------------------------
// GetReadStats
// ---------------------------------------------------------------------------

// GetReadStats returns delivery/read statistics for a notification.
func (s *Service) GetReadStats(ctx context.Context, notificationID string) (*ReadStats, error) {
	return s.store.GetReadStats(ctx, notificationID)
}

// ---------------------------------------------------------------------------
// MarkRead / MarkAllRead
// ---------------------------------------------------------------------------

// MarkRead marks a single notification as read for a machine.
func (s *Service) MarkRead(ctx context.Context, machineID, notificationID string) error {
	return s.store.MarkRead(ctx, machineID, notificationID)
}

// MarkAllRead marks all published, non-expired notifications as read for a machine.
func (s *Service) MarkAllRead(ctx context.Context, machineID string) error {
	return s.store.MarkAllRead(ctx, machineID)
}

// ---------------------------------------------------------------------------
// CreateFromCascade
// ---------------------------------------------------------------------------

// CreateFromCascade handles HubCenter cascade push with idempotent behavior.
// If a notification with the same source+source_id already exists, it is updated;
// otherwise a new notification is created with source="hubcenter".
func (s *Service) CreateFromCascade(ctx context.Context, req CascadeRequest) error {
	if req.Notification == nil {
		return fmt.Errorf("cascade request notification is nil")
	}

	incoming := req.Notification

	// Try to find existing notification by source+source_id
	existing, err := s.store.FindBySource(ctx, "hubcenter", incoming.ID)
	if err != nil {
		return fmt.Errorf("find existing: %w", err)
	}

	now := time.Now().UTC()

	if existing != nil {
		// Update existing notification fields
		if err := s.store.UpdateFromCascade(ctx, existing.ID, incoming, now); err != nil {
			return fmt.Errorf("update from cascade: %w", err)
		}
		return nil
	}

	// Create new notification from cascade
	n := &Notification{
		ID:           uuid.New().String(),
		Title:        incoming.Title,
		Content:      incoming.Content,
		Category:     incoming.Category,
		Priority:     incoming.Priority,
		AudienceType: incoming.AudienceType,
		AudienceIDs:  incoming.AudienceIDs,
		Status:       StatusPublished,
		IMPush:       incoming.IMPush,
		Source:       "hubcenter",
		SourceID:     incoming.ID,
		CreatedBy:    incoming.CreatedBy,
		PublishAt:    incoming.PublishAt,
		ExpireAt:     incoming.ExpireAt,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if n.AudienceIDs == nil {
		n.AudienceIDs = []string{}
	}

	if err := s.store.Create(ctx, n); err != nil {
		return fmt.Errorf("create from cascade: %w", err)
	}

	// Immediately push to clients since cascade notifications are published directly
	machineIDs, err := s.resolveAudienceMachines(ctx, n)
	if err != nil {
		return fmt.Errorf("resolve audience for cascade: %w", err)
	}

	payload := NotificationPushPayload{
		Action:       "new",
		Notification: n,
	}
	envelope, err := BuildNotificationEnvelope(payload)
	if err != nil {
		return fmt.Errorf("build envelope: %w", err)
	}

	if s.wsHub != nil {
		if n.AudienceType == AudienceAll {
			if err := s.wsHub.BroadcastToAll(envelope); err != nil {
				log.Printf("[notification] cascade BroadcastToAll failed for %s: %v", n.ID, err)
			}
		} else if len(machineIDs) > 0 {
			if err := s.wsHub.BroadcastToMachines(machineIDs, envelope); err != nil {
				log.Printf("[notification] cascade BroadcastToMachines failed for %s (%d targets): %v", n.ID, len(machineIDs), err)
			}
		}
	}

	// IM push for urgent
	if n.IMPush && n.Priority == PriorityUrgent && s.imPush != nil && len(machineIDs) > 0 {
		_ = s.imPush.PushToIM(machineIDs, n.Title, n.Content)
	}

	return nil
}

// RevokeFromCascade handles a HubCenter cascade revoke. The HubCenter
// notification ID is stored locally as source_id, while clients know the local
// Hub notification ID, so the revoke must translate before broadcasting.
func (s *Service) RevokeFromCascade(ctx context.Context, sourceID string) error {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return fmt.Errorf("cascade revoke notification_id is required")
	}

	existing, err := s.store.FindBySource(ctx, "hubcenter", sourceID)
	if err != nil {
		return fmt.Errorf("find cascade notification: %w", err)
	}
	if existing == nil {
		return nil
	}

	return s.RevokeNotification(ctx, existing.ID)
}

// ---------------------------------------------------------------------------
// CheckExpired
// ---------------------------------------------------------------------------

// CheckExpired finds published notifications that have passed their expire_at
// time and marks them as expired. Intended to be called periodically by a ticker.
func (s *Service) CheckExpired(ctx context.Context) error {
	return s.store.ExpirePublishedBefore(ctx, time.Now().UTC())
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// resolveAudienceMachines resolves target machine IDs based on audience configuration.
func (s *Service) resolveAudienceMachines(ctx context.Context, n *Notification) ([]string, error) {
	switch n.AudienceType {
	case AudienceAll:
		return s.store.AllActiveMachineIDs(ctx)
	case AudienceTenant:
		return s.store.MachineIDsByTenantIDs(ctx, n.AudienceIDs)
	case AudienceDepartment:
		return s.store.MachineIDsByDepartmentIDs(ctx, n.AudienceIDs)
	case AudienceUser:
		return s.store.MachineIDsByUserIDs(ctx, n.AudienceIDs)
	}
	return nil, fmt.Errorf("unknown audience type: %s", n.AudienceType)
}

// mustMarshalJSON marshals v to JSON string, returning "[]" on error.
func mustMarshalJSON(v interface{}) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(data)
}

// boolToInt converts a boolean to SQLite integer representation.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
