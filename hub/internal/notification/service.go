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
	store      *Store
	wsHub      WSBroadcaster
	imPush     IMPusher
	translator Translator
}

// NewService creates a new notification Service.
func NewService(store *Store, wsBroadcaster WSBroadcaster, imPusher IMPusher) *Service {
	return &Service{
		store:  store,
		wsHub:  wsBroadcaster,
		imPush: imPusher,
	}
}

// SetTranslator installs the system-free translator used when an admin
// publishes a notice on this Hub. Cascade notices already carry locales and
// do not call it.
func (s *Service) SetTranslator(tr Translator) {
	if s == nil {
		return
	}
	s.translator = tr
}

// localesForCreate translates an admin notice. A subsystem notice keeps a
// single language. A request that already carries locales is stored as-is.
func (s *Service) localesForCreate(ctx context.Context, req CreateRequest) (string, map[string]LocaleCopy) {
	source := strings.TrimSpace(req.Source)
	if source != "" && source != "hub" {
		return "", nil
	}
	if len(req.Locales) > 0 {
		return NormalizeNotificationLang(req.SourceLang), cloneLocales(req.Locales)
	}
	var tr Translator
	if s != nil {
		tr = s.translator
	}
	return fillNotificationLocales(ctx, req.SourceLang, req.TargetLangs, LocaleCopy{
		Title:      strings.TrimSpace(req.Title),
		Content:    strings.TrimSpace(req.Content),
		ButtonText: strings.TrimSpace(req.ButtonText),
	}, tr)
}

// validatePinnedFields validates the pinned-card extras and applies defaults:
// an empty button label falls back to the source language's "View details",
// the link must be http(s) so BrowserOpenURL has a real target, and both stay
// within caps.
func validatePinnedFields(req *CreateRequest) error {
	req.LinkURL = strings.TrimSpace(req.LinkURL)
	if req.LinkURL != "" {
		lower := strings.ToLower(req.LinkURL)
		if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
			return fmt.Errorf("link_url must start with http:// or https://")
		}
		if utf8.RuneCountInString(req.LinkURL) > 500 {
			return fmt.Errorf("link_url exceeds 500 characters")
		}
	}
	req.ButtonText = strings.TrimSpace(req.ButtonText)
	if req.ButtonText == "" && (req.Pinned || req.PinnedForever) {
		req.ButtonText = DefaultPinnedButtonForLang(req.SourceLang)
	}
	if utf8.RuneCountInString(req.ButtonText) > 20 {
		return fmt.Errorf("button_text exceeds 20 characters")
	}
	if req.PinnedForever {
		// A permanent pin is still a pin; keep the flag pair consistent.
		req.Pinned = true
	}
	return nil
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

	if err := validatePinnedFields(&req); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	sourceLang, locales := s.localesForCreate(ctx, req)
	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = "hub"
	}
	n := &Notification{
		ID:            uuid.New().String(),
		Title:         req.Title,
		Content:       req.Content,
		Category:      req.Category,
		Priority:      req.Priority,
		AudienceType:  req.AudienceType,
		AudienceIDs:   req.AudienceIDs,
		Status:        StatusDraft,
		IMPush:        req.IMPush,
		Pinned:        req.Pinned,
		PinnedForever: req.PinnedForever,
		LinkURL:       req.LinkURL,
		ButtonText:    req.ButtonText,
		SourceLang:    sourceLang,
		Locales:       locales,
		Source:        source,
		SourceID:      strings.TrimSpace(req.SourceID),
		CreatedBy:     req.CreatedBy,
		PublishAt:     req.PublishAt,
		ExpireAt:      req.ExpireAt,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if n.AudienceIDs == nil {
		n.AudienceIDs = []string{}
	}

	if err := s.store.Create(ctx, n); err != nil {
		return nil, fmt.Errorf("persist notification: %w", err)
	}

	return n, nil
}

// PublishDue releases drafts whose publish_at has arrived and broadcasts them.
// A draft with no publish_at stays a draft. A due draft that has already
// expired is marked expired and is not broadcast.
func (s *Service) PublishDue(ctx context.Context, now time.Time) error {
	if s == nil || s.store == nil {
		return nil
	}
	now = now.UTC()
	due, err := s.store.ListDueDrafts(ctx, now)
	if err != nil {
		return err
	}
	for _, item := range due {
		if item == nil || item.ID == "" {
			continue
		}
		next := StatusPublished
		if item.ExpireAt != nil && !item.ExpireAt.After(now) {
			next = StatusExpired
		}
		ok, err := s.store.ClaimDraft(ctx, item.ID, next)
		if err != nil {
			log.Printf("[notification] claim due draft %s: %v", item.ID, err)
			continue
		}
		if !ok || next != StatusPublished {
			continue
		}
		if err := s.PublishNotification(ctx, item.ID); err != nil {
			log.Printf("[notification] publish due draft %s: %v", item.ID, err)
		}
	}
	return nil
}

// RunScheduledPublisher releases due drafts once a minute until ctx ends.
func (s *Service) RunScheduledPublisher(ctx context.Context) {
	if s == nil {
		return
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if err := s.PublishDue(ctx, time.Now()); err != nil {
			log.Printf("[notification] publish due: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
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

// clientNotificationFrom builds the GUI view of a stored notification.
func clientNotificationFrom(n *Notification, isRead bool) ClientNotification {
	return ClientNotification{
		ID:            n.ID,
		Title:         n.Title,
		Content:       n.Content,
		Category:      string(n.Category),
		Priority:      string(n.Priority),
		IsRead:        isRead,
		CreatedAt:     n.CreatedAt.UTC().Format(time.RFC3339),
		Pinned:        n.Pinned,
		PinnedForever: n.PinnedForever,
		LinkURL:       n.LinkURL,
		ButtonText:    n.ButtonText,
		SourceLang:    n.SourceLang,
		Locales:       n.Locales,
		Source:        n.Source,
		SourceID:      n.SourceID,
	}
}

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
		result = append(result, clientNotificationFrom(n, false))
	}
	return result, nil
}

// GetPinnedForMachine returns the pinned messages for a machine as client
// views: the permanent pin first, then the latest regular pinned messages.
func (s *Service) GetPinnedForMachine(ctx context.Context, machineID string) ([]ClientNotification, error) {
	notifications, err := s.store.GetPinnedForMachine(ctx, machineID)
	if err != nil {
		return nil, fmt.Errorf("get pinned: %w", err)
	}
	result := make([]ClientNotification, 0, len(notifications))
	for _, n := range notifications {
		result = append(result, clientNotificationFrom(n, false))
	}
	return result, nil
}

// FindBySource returns the notification raised by a Hub subsystem under
// source/source_id, or nil when it does not exist.
func (s *Service) FindBySource(ctx context.Context, source, sourceID string) (*Notification, error) {
	source = strings.TrimSpace(source)
	sourceID = strings.TrimSpace(sourceID)
	if source == "" || sourceID == "" {
		return nil, nil
	}
	return s.store.FindBySource(ctx, source, sourceID)
}

// AddAudienceUser appends userID to a notification's audience. Reports false
// when the user is already listed or the notification is gone.
func (s *Service) AddAudienceUser(ctx context.Context, id, userID string) (bool, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return false, nil
	}
	n, err := s.store.GetByID(ctx, id)
	if err != nil {
		return false, fmt.Errorf("get notification: %w", err)
	}
	for _, existing := range n.AudienceIDs {
		if existing == userID {
			return true, nil
		}
	}
	n.AudienceIDs = append(n.AudienceIDs, userID)
	if err := s.store.SetAudienceUsers(ctx, id, n.AudienceIDs); err != nil {
		return false, err
	}
	return true, nil
}

// RemoveAudienceUser drops userID from a notification's audience and reports
// how many audience members remain.
func (s *Service) RemoveAudienceUser(ctx context.Context, id, userID string) (int, error) {
	n, err := s.store.GetByID(ctx, id)
	if err != nil {
		return 0, fmt.Errorf("get notification: %w", err)
	}
	kept := make([]string, 0, len(n.AudienceIDs))
	removed := false
	for _, existing := range n.AudienceIDs {
		if existing == userID {
			removed = true
			continue
		}
		kept = append(kept, existing)
	}
	if !removed {
		return len(n.AudienceIDs), nil
	}
	if err := s.store.SetAudienceUsers(ctx, id, kept); err != nil {
		return 0, err
	}
	return len(kept), nil
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
		// Same normalization as the insert path. A repeat push must not keep a
		// raw language key or an over-long button that the first insert clamped.
		incoming.SourceLang = NormalizeNotificationLang(incoming.SourceLang)
		incoming.Locales = cloneLocales(incoming.Locales)
		if err := s.store.UpdateFromCascade(ctx, existing.ID, incoming, now); err != nil {
			return fmt.Errorf("update from cascade: %w", err)
		}
		return nil
	}

	// Create new notification from cascade
	n := &Notification{
		ID:            uuid.New().String(),
		Title:         incoming.Title,
		Content:       incoming.Content,
		Category:      incoming.Category,
		Priority:      incoming.Priority,
		AudienceType:  incoming.AudienceType,
		AudienceIDs:   incoming.AudienceIDs,
		Status:        StatusPublished,
		IMPush:        incoming.IMPush,
		Pinned:        incoming.Pinned,
		PinnedForever: incoming.PinnedForever,
		LinkURL:       incoming.LinkURL,
		ButtonText:    incoming.ButtonText,
		SourceLang:    NormalizeNotificationLang(incoming.SourceLang),
		Locales:       cloneLocales(incoming.Locales),
		Source:        "hubcenter",
		SourceID:      incoming.ID,
		CreatedBy:     incoming.CreatedBy,
		PublishAt:     incoming.PublishAt,
		ExpireAt:      incoming.ExpireAt,
		CreatedAt:     now,
		UpdatedAt:     now,
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
