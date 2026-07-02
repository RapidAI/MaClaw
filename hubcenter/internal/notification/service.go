package notification

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// HubResolver resolves target Hub endpoints based on audience type and IDs.
type HubResolver interface {
	// AllHubs returns all registered and active Hub endpoints.
	AllHubs(ctx context.Context) ([]HubEndpoint, error)
	// HubsByIDs returns Hub endpoints for the given Hub IDs.
	HubsByIDs(ctx context.Context, hubIDs []string) ([]HubEndpoint, error)
	// HubsByTenantPairs returns Hub endpoints derived from "hub_id:tenant_id" pairs.
	// The returned endpoints correspond to the unique Hub IDs extracted from the pairs.
	HubsByTenantPairs(ctx context.Context, pairs []string) ([]HubEndpoint, error)
}

// Service implements HubCenter notification business logic.
type Service struct {
	store    Store
	cascade  *CascadeService
	resolver HubResolver
}

// NewService creates a new HubCenter notification service.
func NewService(store Store, cascade *CascadeService, resolver HubResolver) *Service {
	return &Service{
		store:    store,
		cascade:  cascade,
		resolver: resolver,
	}
}

// Validation errors.
var (
	ErrTitleRequired       = errors.New("title is required")
	ErrTitleTooLong        = errors.New("title exceeds 100 characters")
	ErrContentRequired     = errors.New("content is required")
	ErrContentTooLong      = errors.New("content exceeds 2000 characters")
	ErrInvalidCategory     = errors.New("invalid category")
	ErrInvalidAudience     = errors.New("invalid audience type")
	ErrInvalidPriority     = errors.New("invalid priority")
	ErrInvalidStatus       = errors.New("invalid notification status")
	ErrAudienceIDsRequired = errors.New("audience_ids required for hub/hub_tenant audience")
	ErrNotFound            = errors.New("notification not found")
	ErrCannotRevoke        = errors.New("only published notifications can be revoked")
)

// CreateNotification validates the request, persists the notification, and triggers
// cascade dispatch to target Hubs. The cascade dispatch is done asynchronously
// (fire and forget with status recording).
func (s *Service) CreateNotification(ctx context.Context, req CreateRequest) (*Notification, error) {
	if err := validateCreateRequest(req); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	notif := &Notification{
		ID:           uuid.New().String(),
		Title:        strings.TrimSpace(req.Title),
		Content:      strings.TrimSpace(req.Content),
		Category:     req.Category,
		Priority:     req.Priority,
		AudienceType: req.AudienceType,
		AudienceIDs:  req.AudienceIDs,
		Status:       StatusPublished,
		IMPush:       req.IMPush,
		CreatedBy:    req.CreatedBy,
		PublishAt:    req.PublishAt,
		ExpireAt:     req.ExpireAt,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if req.Status == StatusDraft {
		notif.Status = StatusDraft
	}
	// If publish_at is in the future, save as draft instead.
	if notif.Status == StatusPublished && req.PublishAt != nil && req.PublishAt.After(now) {
		notif.Status = StatusDraft
	}

	// Default priority to normal if not specified.
	if notif.Priority == "" {
		notif.Priority = PriorityNormal
	}

	if err := s.store.Create(ctx, notif); err != nil {
		return nil, fmt.Errorf("persist notification: %w", err)
	}

	// If published immediately, trigger cascade dispatch asynchronously.
	if notif.Status == StatusPublished {
		go s.dispatchCascade(notif)
	}

	return notif, nil
}

// ListNotifications returns a paginated list of notifications with cascade results.
func (s *Service) ListNotifications(ctx context.Context, filter ListFilter) ([]*NotificationWithCascade, int, error) {
	notifications, total, err := s.store.List(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("list notifications: %w", err)
	}

	results := make([]*NotificationWithCascade, 0, len(notifications))
	for _, n := range notifications {
		cascadeResults, _ := s.store.GetCascadeResults(ctx, n.ID)
		results = append(results, &NotificationWithCascade{
			Notification:   n,
			CascadeResults: cascadeResults,
		})
	}

	return results, total, nil
}

// GetByID retrieves a notification by ID along with its cascade results.
func (s *Service) GetByID(ctx context.Context, id string) (*NotificationWithCascade, error) {
	notif, err := s.store.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get notification: %w", err)
	}
	if notif == nil {
		return nil, ErrNotFound
	}

	cascadeResults, _ := s.store.GetCascadeResults(ctx, id)
	return &NotificationWithCascade{
		Notification:   notif,
		CascadeResults: cascadeResults,
	}, nil
}

// RevokeNotification sets a notification's status to revoked and cascades
// the revoke signal to all Hubs that received this notification.
func (s *Service) RevokeNotification(ctx context.Context, id string) error {
	notif, err := s.store.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get notification: %w", err)
	}
	if notif == nil {
		return ErrNotFound
	}
	if notif.Status != StatusPublished {
		return ErrCannotRevoke
	}

	now := time.Now().UTC()
	if err := s.store.UpdateStatus(ctx, id, StatusRevoked, now); err != nil {
		return fmt.Errorf("update status: %w", err)
	}

	// Cascade revoke to all Hubs that received this notification (async).
	go s.dispatchRevoke(notif)

	return nil
}

// dispatchCascade resolves target Hubs and dispatches the notification asynchronously.
func (s *Service) dispatchCascade(notif *Notification) {
	if s == nil || s.cascade == nil || s.resolver == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	targetHubs, err := s.resolveAudience(ctx, notif.AudienceType, notif.AudienceIDs)
	if err != nil {
		// Log but do not propagate — async fire-and-forget.
		fmt.Printf("[notification] dispatchCascade: resolve audience failed for %s: %v\n", notif.ID, err)
		return
	}
	if len(targetHubs) == 0 {
		return
	}

	_ = s.cascade.DispatchToHubs(ctx, notif, targetHubs)
}

// dispatchRevoke resolves target Hubs and sends revoke signals asynchronously.
func (s *Service) dispatchRevoke(notif *Notification) {
	if s == nil || s.cascade == nil || s.resolver == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	targetHubs, err := s.resolveAudience(ctx, notif.AudienceType, notif.AudienceIDs)
	if err != nil {
		// Log but do not propagate — async fire-and-forget.
		fmt.Printf("[notification] dispatchRevoke: resolve audience failed for %s: %v\n", notif.ID, err)
		return
	}
	if len(targetHubs) == 0 {
		return
	}

	_ = s.cascade.DispatchRevoke(ctx, notif.ID, targetHubs)
}

// resolveAudience determines which Hub endpoints should receive the notification
// based on the audience type:
//   - "all" → all registered Hub URLs
//   - "hub" → specific Hub URLs from audience_ids
//   - "hub_tenant" → Hub URLs extracted from "hub_id:tenant_id" pairs in audience_ids
func (s *Service) resolveAudience(ctx context.Context, audienceType AudienceType, audienceIDs []string) ([]HubEndpoint, error) {
	switch audienceType {
	case AudienceAll:
		return s.resolver.AllHubs(ctx)
	case AudienceHub:
		return s.resolver.HubsByIDs(ctx, audienceIDs)
	case AudienceHubTenant:
		return s.resolver.HubsByTenantPairs(ctx, audienceIDs)
	default:
		return nil, fmt.Errorf("unknown audience type: %s", audienceType)
	}
}

// validateCreateRequest validates all fields of a CreateRequest.
func validateCreateRequest(req CreateRequest) error {
	// Title validation.
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return ErrTitleRequired
	}
	if len([]rune(title)) > 100 {
		return ErrTitleTooLong
	}

	// Content validation.
	content := strings.TrimSpace(req.Content)
	if content == "" {
		return ErrContentRequired
	}
	if len([]rune(content)) > 2000 {
		return ErrContentTooLong
	}

	// Category validation.
	if !ValidCategory(req.Category) {
		return ErrInvalidCategory
	}

	// Priority validation (empty defaults to normal, non-empty must be valid).
	if req.Priority != "" && !ValidPriority(req.Priority) {
		return ErrInvalidPriority
	}
	if req.Status != "" && req.Status != StatusDraft && req.Status != StatusPublished {
		return ErrInvalidStatus
	}

	// Audience validation.
	if !ValidAudienceType(req.AudienceType) {
		return ErrInvalidAudience
	}
	// "hub" and "hub_tenant" require audience_ids.
	if (req.AudienceType == AudienceHub || req.AudienceType == AudienceHubTenant) && len(req.AudienceIDs) == 0 {
		return ErrAudienceIDsRequired
	}

	return nil
}
