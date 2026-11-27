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

// StatusMayApply reports whether a replicated notification may replace the
// local row. A dispatch lease is still a draft; a delayed copy must not turn
// a published, expired, or revoked notice back into a draft. A delayed
// publish must not undo a revoke.
func StatusMayApply(current, incoming Status) bool {
	if current == "" || current == incoming {
		return true
	}
	switch current {
	case StatusDraft:
		return true
	case StatusPublished:
		return incoming == StatusRevoked || incoming == StatusExpired
	default:
		return false
	}
}

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
	// Pinned marks a 置顶消息 shown as a floating card above the GUI sidebar
	// status row. PinnedForever marks the ONE permanent pin that stays
	// leftmost in that card; a pinned message is implied. All four fields
	// cascade to Hubs verbatim through the JSON payload.
	Pinned        bool                  `json:"pinned"`
	PinnedForever bool                  `json:"pinned_forever"`
	LinkURL       string                `json:"link_url"`
	ButtonText    string                `json:"button_text"`
	SourceLang    string                `json:"source_lang,omitempty"`
	Locales       map[string]LocaleCopy `json:"locales,omitempty"`
	CreatedBy     string                `json:"created_by"`
	PublishAt     *time.Time            `json:"publish_at,omitempty"`
	ExpireAt      *time.Time            `json:"expire_at,omitempty"`
	CreatedAt     time.Time             `json:"created_at"`
	UpdatedAt     time.Time             `json:"updated_at"`
	// DispatchNode and DispatchUntil fence a scheduled send across HubCenter
	// nodes. The row stays a draft, so the admin status does not change.
	// Another node leaves the draft alone until DispatchUntil. Empty means
	// nobody is sending it.
	DispatchNode  string     `json:"dispatch_node,omitempty"`
	DispatchUntil *time.Time `json:"dispatch_until,omitempty"`
}

// DefaultPinnedButtonText is the Simplified Chinese label used when an admin
// publishes a pinned message without naming its action button. Other source
// languages use DefaultPinnedButtonForLang.
const DefaultPinnedButtonText = "查看详情"

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
	// Pinned-card fields. An empty ButtonText on a pin becomes the source
	// language's "View details" label.
	Pinned        bool                  `json:"pinned"`
	PinnedForever bool                  `json:"pinned_forever"`
	LinkURL       string                `json:"link_url"`
	ButtonText    string                `json:"button_text"`
	SourceLang    string                `json:"source_lang,omitempty"`
	TargetLangs   []string              `json:"target_langs,omitempty"`
	Locales       map[string]LocaleCopy `json:"locales,omitempty"`
	CreatedBy     string                `json:"created_by"`
	PublishAt     *time.Time            `json:"publish_at,omitempty"`
	ExpireAt      *time.Time            `json:"expire_at,omitempty"`
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
	// ListDueDrafts returns drafts that carry a publish_at at or before now.
	// A draft with no publish_at stays a draft until someone publishes it.
	ListDueDrafts(ctx context.Context, now time.Time) ([]*Notification, error)
	// ClaimDraft moves a draft to next. ok is false when the row is no longer a draft.
	ClaimDraft(ctx context.Context, id string, next Status, updatedAt time.Time) (bool, error)
	// ClaimDispatch records that node is sending this draft until the lease
	// time. ok is false when the row is no longer a draft or another node
	// still holds an unexpired lease. The same node may renew its own lease.
	ClaimDispatch(ctx context.Context, id, node string, until, updatedAt time.Time) (bool, error)
	Delete(ctx context.Context, id string) error
	RecordCascadeResult(ctx context.Context, result *CascadeResult) error
	GetCascadeResults(ctx context.Context, notificationID string) ([]*CascadeResult, error)
}
