package notification

import "time"

// Category represents the notification category.
type Category string

const (
	CategorySystemAnnouncement Category = "system_announcement"
	CategoryFeatureUpdate      Category = "feature_update"
	CategorySecurityAlert      Category = "security_alert"
	CategoryMaintenance        Category = "maintenance"
	CategoryCustom             Category = "custom"
)

// ValidCategory returns true if the category is one of the known values.
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

// ValidPriority returns true if the priority is one of the known values.
func ValidPriority(p Priority) bool {
	switch p {
	case PriorityNormal, PriorityImportant, PriorityUrgent:
		return true
	}
	return false
}

// AudienceType represents who should receive the notification.
type AudienceType string

const (
	AudienceAll        AudienceType = "all"
	AudienceTenant     AudienceType = "tenant"
	AudienceDepartment AudienceType = "department"
	AudienceUser       AudienceType = "user"
)

// ValidAudienceType returns true if the audience type is one of the known values.
func ValidAudienceType(a AudienceType) bool {
	switch a {
	case AudienceAll, AudienceTenant, AudienceDepartment, AudienceUser:
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

// SourceCompanionGroupInvite marks the delivery notice for a reading-group
// invitation. It is raised by the companion module rather than by an admin, so
// the admin console excludes it.
const SourceCompanionGroupInvite = "companion_group_invite"

// SourceCompanionShare marks the delivery notice for a document shared with a
// person or a reading group. SourceID is the share token.
const SourceCompanionShare = "companion_share"

// SourceCompanionHostTransfer is the actionable request to the nominated host.
// SourceCompanionHostTransferNotice is the informational copy for participants.
const SourceCompanionHostTransfer = "companion_host_transfer"
const SourceCompanionHostTransferNotice = "companion_host_transfer_notice"

// Notification is the core domain model for an admin notification.
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
	Source       string       `json:"source"`
	SourceID     string       `json:"source_id"`
	CreatedBy    string       `json:"created_by"`
	PublishAt    *time.Time   `json:"publish_at,omitempty"`
	ExpireAt     *time.Time   `json:"expire_at,omitempty"`
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
}

// CreateRequest is the input for creating a new notification.
type CreateRequest struct {
	Title        string       `json:"title"`
	Content      string       `json:"content"`
	Category     Category     `json:"category"`
	Priority     Priority     `json:"priority"`
	AudienceType AudienceType `json:"audience_type"`
	AudienceIDs  []string     `json:"audience_ids"`
	IMPush       bool         `json:"im_push"`
	CreatedBy    string       `json:"created_by"`
	PublishAt    *time.Time   `json:"publish_at,omitempty"`
	ExpireAt     *time.Time   `json:"expire_at,omitempty"`
	// Source and SourceID tag a notice raised by another Hub subsystem instead
	// of by an admin. They default to "hub" and "" for admin notices.
	Source   string `json:"source,omitempty"`
	SourceID string `json:"source_id,omitempty"`
}

// CascadeRequest is the input from HubCenter cascade push.
type CascadeRequest struct {
	Action         string        `json:"action,omitempty"`
	Notification   *Notification `json:"notification,omitempty"`
	NotificationID string        `json:"notification_id,omitempty"`
}

// ClientNotification is the view returned to GUI clients.
type ClientNotification struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Category  string `json:"category"`
	Priority  string `json:"priority"`
	IsRead    bool   `json:"is_read"`
	CreatedAt string `json:"created_at"`
	// Source and SourceID let a client recognize a notice raised by a Hub
	// subsystem, such as a reading-group invitation, and act on it.
	Source   string `json:"source,omitempty"`
	SourceID string `json:"source_id,omitempty"`
}

// NotificationPushPayload is the payload for WebSocket notification.push envelope.
type NotificationPushPayload struct {
	Action       string        `json:"action"`                    // "new" | "revoke"
	Notification *Notification `json:"notification,omitempty"`    // action=new
	NotifID      string        `json:"notification_id,omitempty"` // action=revoke
}

// ReadStats holds delivery/read statistics for a notification.
type ReadStats struct {
	TotalPush int     `json:"total_push"`
	ReadCount int     `json:"read_count"`
	ReadRate  float64 `json:"read_rate"`
}

// ListFilter defines the filtering criteria for listing notifications.
type ListFilter struct {
	Status   Status
	Category Category
	Offset   int
	Limit    int
	// ExcludeSources hides notices raised by a Hub subsystem. The admin console
	// lists admin-created notices only.
	ExcludeSources []string
}
