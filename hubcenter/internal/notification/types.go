package notification

import (
	"context"
	"time"
)

// Category represents the notification category.
type Category string

const (
	CategorySystemAnnouncement Category = "system_announcement"
	CategoryFeatureUpdate      Category = "feature_update"
	CategorySecurityAlert      Category = "security_alert"
	CategoryMaintenance        Category = "maintenance"
	CategoryCustom             Category = "custom"
)

// ValidCategory returns true if the category is valid.
func ValidCategory(c Category) bool {
	switch c {
	case CategorySystemAnnouncement, CategoryFeatureUpdate, CategorySecurityAlert, CategoryMaintenance, CategoryCustom:
		return true
	}
	return false
}

// Priority represents the notification priority level.
type Priority string

const (
	PriorityNormal    Priority = "normal"
	PriorityImportant Priority = "important"
	PriorityUrgent    Priority = "urgent"
)

// ValidPriority returns true if the priority is valid.
func ValidPriority(p Priority) bool {
	switch p {
	case PriorityNormal, PriorityImportant, PriorityUrgent:
		return true
	}
	return false
}

// AudienceType represents who should receive the notification at HubCenter level.
type AudienceType string

const (
	// AudienceAll broadcasts to all registered Hub instances (全网广播).
	AudienceAll AudienceType = "all"
	// AudienceHub targets all users of one or more specific Hub instances.
	AudienceHub AudienceType = "hub"
	// AudienceHubTenant targets specific tenants within specific Hubs.
	AudienceHubTenant AudienceType = "hub_tenant"
)

// ValidAudienceType returns true if the audience type is valid for HubCenter.
func ValidAudienceType(a AudienceType) bool {
	switch a {
	case AudienceAll, AudienceHub, AudienceHubTenant:
		return true
	}
	return false
}

// Status represents the notification lifecycle state.
type Status string

const (
	StatusDraft     Status = "draft"
	StatusPublished Status = "published"
	StatusExpired   Status = "expired"
	StatusRevoked   Status = "revoked"
)

// CascadeStatus represents the push status for a specific Hub.
type CascadeStatus string

const (
	CascadeStatusPending    CascadeStatus = "pending"
	CascadeStatusSuccess    CascadeStatus = "success"
	CascadeStatusFailed     CascadeStatus = "failed"
	CascadeStatusTimeout    CascadeStatus = "timeout"
	CascadeStatusAuthFailed CascadeStatus = "auth_failed"
)

// Notification is the core domain model for a HubCenter notification.
type Notification struct {
	ID           string       `json:"id"`
	Title        string       `json:"title"`
	Content      string       `json:"content"`
	Category     Category     `json:"category"`
	Priority     Priority     `json:"priority"`
	AudienceType AudienceType `json:"audience_type"`
	AudienceIDs  []string     `json:"audience_ids"`
	Status       Status       `json:"status"`
	IMPush       bool         `json:"im_push"`
	CreatedBy    string       `json:"created_by"`
	PublishAt    *time.Time   `json:"publish_at,omitempty"`
	ExpireAt     *time.Time   `json:"expire_at,omitempty"`
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
}

// CreateRequest is the input for creating a HubCenter notification.
type CreateRequest struct {
	Title        string       `json:"title"`
	Content      string       `json:"content"`
	Category     Category     `json:"category"`
	Priority     Priority     `json:"priority"`
	AudienceType AudienceType `json:"audience_type"`
	AudienceIDs  []string     `json:"audience_ids"`
	Status       Status       `json:"status"`
	IMPush       bool         `json:"im_push"`
	CreatedBy    string       `json:"created_by"`
	PublishAt    *time.Time   `json:"publish_at,omitempty"`
	ExpireAt     *time.Time   `json:"expire_at,omitempty"`
}

// ListFilter defines the filtering criteria for listing notifications.
type ListFilter struct {
	Status   Status
	Category Category
	Offset   int
	Limit    int
}

// CascadeResult records the outcome of a cascade push to a single Hub.
type CascadeResult struct {
	NotificationID string        `json:"notification_id"`
	HubID          string        `json:"hub_id"`
	Status         CascadeStatus `json:"status"`
	ErrorMessage   string        `json:"error_message"`
	PushedAt       *time.Time    `json:"pushed_at,omitempty"`
}

// HubEndpoint represents a target Hub instance for cascade push.
type HubEndpoint struct {
	ID               string `json:"id"`   // Hub instance ID
	Name             string `json:"name"` // Hub display name
	URL              string `json:"url"`  // Hub base URL
	CascadeAuthToken string `json:"-"`    // Bearer token for cascade auth (excluded from JSON to prevent leaks)
}

// NotificationWithCascade combines a notification with its cascade results.
type NotificationWithCascade struct {
	Notification   *Notification    `json:"notification"`
	CascadeResults []*CascadeResult `json:"cascade_results,omitempty"`
}

// Store defines the persistence interface for HubCenter notifications.
type Store interface {
	Create(ctx context.Context, n *Notification) error
	Upsert(ctx context.Context, n *Notification) error
	GetByID(ctx context.Context, id string) (*Notification, error)
	List(ctx context.Context, filter ListFilter) ([]*Notification, int, error)
	UpdateStatus(ctx context.Context, id string, status Status, updatedAt time.Time) error
	Delete(ctx context.Context, id string) error
	RecordCascadeResult(ctx context.Context, result *CascadeResult) error
	GetCascadeResults(ctx context.Context, notificationID string) ([]*CascadeResult, error)
}
