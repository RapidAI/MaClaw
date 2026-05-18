package workflow

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// NotificationChannel represents the delivery channel for a notification.
type NotificationChannel string

const (
	ChannelHubInApp  NotificationChannel = "hub_inapp"
	ChannelIMFeishu  NotificationChannel = "im_feishu"
	ChannelIMWechat  NotificationChannel = "im_wechat"
	ChannelIMQQ      NotificationChannel = "im_qq"
	ChannelIM        NotificationChannel = "im" // generic IM channel (actual platform determined at delivery time)
)

// NotifType represents the type/purpose of a notification.
type NotifType string

const (
	NotifTypeResultExecutor NotifType = "result_executor"
	NotifTypeNotifier       NotifType = "notifier"
	NotifTypeWithdrawal     NotifType = "withdrawal"
	NotifTypeReminder       NotifType = "reminder"
	NotifTypeEscalation     NotifType = "escalation"
)

// Notification represents a single notification record in the notifications table.
type Notification struct {
	ID            string              `json:"id"`
	InstanceID    string              `json:"instance_id"`
	Type          NotifType           `json:"type"`
	RecipientID   string              `json:"recipient_id"`
	Channel       NotificationChannel `json:"channel"`
	PayloadJSON   json.RawMessage     `json:"payload_json"`
	Delivered     bool                `json:"delivered"`
	DeliveredAt   *time.Time          `json:"delivered_at,omitempty"`
	FailureReason string              `json:"failure_reason,omitempty"`
	CreatedAt     time.Time           `json:"created_at"`
}

// NotificationStore provides CRUD operations for notification records.
type NotificationStore interface {
	// Create inserts a new notification record.
	// If the notification's ID is empty, a random ID is generated.
	// If CreatedAt is zero, it is set to the current UTC time.
	Create(ctx context.Context, notif *Notification) error

	// Get retrieves a notification by ID.
	Get(ctx context.Context, id string) (*Notification, error)

	// ListByInstance returns all notifications for a given workflow instance,
	// ordered by created_at ascending.
	ListByInstance(ctx context.Context, instanceID string) ([]Notification, error)

	// ListByRecipient returns all notifications for a given recipient,
	// ordered by created_at descending.
	ListByRecipient(ctx context.Context, recipientID string) ([]Notification, error)

	// MarkDelivered updates a notification's delivered status to true
	// and sets delivered_at to the current UTC time.
	MarkDelivered(ctx context.Context, id string) error

	// MarkFailed updates a notification's failure_reason field.
	// The delivered field remains false.
	MarkFailed(ctx context.Context, id string, reason string) error
}

// GenerateNotificationID creates a unique ID for a notification record.
func GenerateNotificationID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "notif_fallback_" + fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return "notif_" + hex.EncodeToString(b[:])
}
