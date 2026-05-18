package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// HubInAppNotifier sends in-app notifications visible on the Hub web UI.
type HubInAppNotifier interface {
	Send(ctx context.Context, recipientID string, notif *InAppNotification) error
}

// IMPushNotifier sends push notifications to connected IM channels.
type IMPushNotifier interface {
	Push(ctx context.Context, recipientID string, msg *IMPushMessage) error
	IsConnected(ctx context.Context, recipientID string) bool
}

// InAppNotification is the payload for Hub in-app notifications.
type InAppNotification struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url"`
	Type  string `json:"type"`
}

// IMPushMessage is the payload for IM push notifications.
type IMPushMessage struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url"`
}

// WorkflowNotification is the payload delivered to executors/notifiers.
type WorkflowNotification struct {
	ID              string     `json:"id"`
	InstanceID      string     `json:"instance_id"`
	Type            NotifType  `json:"type"`
	RecipientID     string     `json:"recipient_id"`
	WorkflowName    string     `json:"workflow_name"`
	Result          string     `json:"result"`
	FormDataSummary string     `json:"form_data_summary"`
	InitiatorID     string     `json:"initiator_id,omitempty"`
	InitiatorName   string     `json:"initiator_name,omitempty"`
	InstanceURL     string     `json:"instance_url"`
	DeliveredAt     *time.Time `json:"delivered_at,omitempty"`
	DeliveryChannel string     `json:"delivery_channel,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

// NotificationDispatcher sends notifications to users through multiple channels.
// It fans out to Hub in-app notifications and IM push simultaneously.
type NotificationDispatcher struct {
	hubNotifier HubInAppNotifier
	imPusher    IMPushNotifier
	auditStore  AuditStore
	notifStore  NotificationStore
}

// NewNotificationDispatcher creates a new NotificationDispatcher with the given dependencies.
func NewNotificationDispatcher(hubNotifier HubInAppNotifier, imPusher IMPushNotifier, auditStore AuditStore, notifStore NotificationStore) *NotificationDispatcher {
	return &NotificationDispatcher{
		hubNotifier: hubNotifier,
		imPusher:    imPusher,
		auditStore:  auditStore,
		notifStore:  notifStore,
	}
}

// dispatchTimeout is the maximum time allowed for a single notification dispatch.
const dispatchTimeout = 60 * time.Second

// Dispatch sends a notification through all available channels.
// It always attempts Hub in-app delivery. IM push is attempted only if the
// recipient's IM channel is connected. IM failure is non-fatal — only Hub
// in-app failure causes an error return.
// Records delivery status in the notifications table and audit trail.
func (d *NotificationDispatcher) Dispatch(ctx context.Context, notif *WorkflowNotification) error {
	if d.hubNotifier == nil {
		return fmt.Errorf("hub notifier not configured")
	}

	ctx, cancel := context.WithTimeout(ctx, dispatchTimeout)
	defer cancel()

	if notif.ID == "" {
		notif.ID = GenerateNotificationID()
	}
	if notif.CreatedAt.IsZero() {
		notif.CreatedAt = time.Now().UTC().Truncate(time.Millisecond)
	}

	inApp := buildInAppNotification(notif)
	imMsg := buildIMPushMessage(notif)

	// Always attempt Hub in-app notification.
	hubErr := d.hubNotifier.Send(ctx, notif.RecipientID, inApp)

	// Record Hub in-app delivery status.
	hubNotifRecord := d.buildNotificationRecord(notif, ChannelHubInApp)
	if hubErr == nil {
		hubNotifRecord.Delivered = true
		now := time.Now().UTC().Truncate(time.Millisecond)
		hubNotifRecord.DeliveredAt = &now
	} else {
		hubNotifRecord.FailureReason = hubErr.Error()
	}
	if d.notifStore != nil {
		_ = d.notifStore.Create(ctx, hubNotifRecord)
	}

	// Attempt IM push if connected (IM push is optional — skip silently if not configured).
	var imErr error
	imConnected := false
	if d.imPusher != nil {
		imConnected = d.imPusher.IsConnected(ctx, notif.RecipientID)
	}
	if imConnected {
		imErr = d.imPusher.Push(ctx, notif.RecipientID, imMsg)

		// Record IM delivery status.
		imNotifRecord := d.buildNotificationRecord(notif, d.resolveIMChannel(notif.RecipientID))
		if imErr == nil {
			imNotifRecord.Delivered = true
			now := time.Now().UTC().Truncate(time.Millisecond)
			imNotifRecord.DeliveredAt = &now
		} else {
			imNotifRecord.FailureReason = imErr.Error()
		}
		if d.notifStore != nil {
			_ = d.notifStore.Create(ctx, imNotifRecord)
		}
	}

	// If IM is not connected or IM push failed, record in audit trail.
	if !imConnected || imErr != nil {
		reason := "im_channel_not_connected"
		if imConnected && imErr != nil {
			reason = "im_push_failed: " + imErr.Error()
		}
		if d.auditStore != nil {
			_ = d.auditStore.Append(ctx, &AuditEntry{
				InstanceID: notif.InstanceID,
				EventType:  "im_delivery_failed",
				ActorID:    notif.RecipientID,
				Details:    reason,
			})
		}
	}

	// Update notification metadata on success.
	if hubErr == nil {
		now := time.Now().UTC().Truncate(time.Millisecond)
		notif.DeliveredAt = &now
		if imConnected && imErr == nil {
			notif.DeliveryChannel = "hub_inapp,im"
		} else {
			notif.DeliveryChannel = "hub_inapp"
		}
	}

	// IM failure is non-fatal. Only return error if Hub in-app also fails.
	if hubErr != nil {
		return fmt.Errorf("hub in-app notification failed for recipient %s: %w", notif.RecipientID, hubErr)
	}
	return nil
}

// maxDispatchConcurrency is the maximum number of concurrent Dispatch goroutines.
const maxDispatchConcurrency = 10

// DispatchBatch sends notifications to multiple recipients in parallel.
// It uses goroutines with a semaphore to limit concurrency to 10.
// Errors are collected but dispatching continues for remaining recipients.
// Returns a combined error if any dispatches failed.
func (d *NotificationDispatcher) DispatchBatch(ctx context.Context, notifs []*WorkflowNotification) error {
	if len(notifs) == 0 {
		return nil
	}

	var (
		mu   sync.Mutex
		errs []string
		wg   sync.WaitGroup
	)

	sem := make(chan struct{}, maxDispatchConcurrency)

	for _, notif := range notifs {
		wg.Add(1)
		go func(n *WorkflowNotification) {
			defer wg.Done()

			// Acquire semaphore slot.
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				mu.Lock()
				errs = append(errs, fmt.Sprintf("context cancelled for recipient %s: %v", n.RecipientID, ctx.Err()))
				mu.Unlock()
				return
			}

			if err := d.Dispatch(ctx, n); err != nil {
				mu.Lock()
				errs = append(errs, err.Error())
				mu.Unlock()
			}
		}(notif)
	}

	wg.Wait()

	if len(errs) > 0 {
		return fmt.Errorf("batch dispatch errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

// buildNotificationRecord creates a Notification record for persistence.
func (d *NotificationDispatcher) buildNotificationRecord(notif *WorkflowNotification, channel NotificationChannel) *Notification {
	payload, _ := json.Marshal(notif)
	return &Notification{
		ID:          GenerateNotificationID(),
		InstanceID:  notif.InstanceID,
		Type:        notif.Type,
		RecipientID: notif.RecipientID,
		Channel:     channel,
		PayloadJSON: payload,
		Delivered:   false,
		CreatedAt:   time.Now().UTC().Truncate(time.Millisecond),
	}
}

// resolveIMChannel determines the actual IM channel for a recipient.
// Since the IMPushNotifier interface doesn't expose channel type,
// we use a generic "im" channel identifier.
func (d *NotificationDispatcher) resolveIMChannel(recipientID string) NotificationChannel {
	// TODO: When IMPushNotifier exposes channel type, use it here.
	// For now, use generic "im" to avoid hardcoding a specific platform.
	return ChannelIM
}

// buildInAppNotification converts a WorkflowNotification to an InAppNotification
// suitable for the Hub in-app notification system.
func buildInAppNotification(notif *WorkflowNotification) *InAppNotification {
	title := buildNotificationTitle(notif)
	body := buildNotificationBody(notif)
	return &InAppNotification{
		Title: title,
		Body:  body,
		URL:   notif.InstanceURL,
		Type:  string(notif.Type),
	}
}

// buildIMPushMessage converts a WorkflowNotification to an IMPushMessage
// suitable for IM channel push delivery.
func buildIMPushMessage(notif *WorkflowNotification) *IMPushMessage {
	title := buildNotificationTitle(notif)
	body := buildNotificationBody(notif)
	return &IMPushMessage{
		Title: title,
		Body:  body,
		URL:   notif.InstanceURL,
	}
}

// buildNotificationTitle generates a human-readable title based on notification type.
func buildNotificationTitle(notif *WorkflowNotification) string {
	switch notif.Type {
	case NotifTypeResultExecutor:
		return fmt.Sprintf("【待执行】%s - %s", notif.WorkflowName, notif.Result)
	case NotifTypeNotifier:
		return fmt.Sprintf("【通知】%s - %s", notif.WorkflowName, notif.Result)
	case NotifTypeWithdrawal:
		return fmt.Sprintf("【已撤回】%s", notif.WorkflowName)
	case NotifTypeReminder:
		return fmt.Sprintf("【提醒】%s - 待确认", notif.WorkflowName)
	case NotifTypeEscalation:
		return fmt.Sprintf("【升级】%s - 确认超时", notif.WorkflowName)
	default:
		return fmt.Sprintf("【工作流】%s", notif.WorkflowName)
	}
}

// buildNotificationBody generates a human-readable body based on notification type.
func buildNotificationBody(notif *WorkflowNotification) string {
	var parts []string

	if notif.Result != "" {
		parts = append(parts, "审批结果: "+notif.Result)
	}
	if notif.FormDataSummary != "" {
		parts = append(parts, "摘要: "+notif.FormDataSummary)
	}
	if notif.InitiatorName != "" {
		parts = append(parts, "发起人: "+notif.InitiatorName)
	}

	switch notif.Type {
	case NotifTypeResultExecutor:
		parts = append(parts, "请确认已操作")
	case NotifTypeNotifier:
		parts = append(parts, "请确认已知会")
	case NotifTypeWithdrawal:
		parts = []string{"发起人已撤回此审批流程，无需进一步操作。"}
	case NotifTypeReminder:
		parts = append(parts, "请尽快确认")
	case NotifTypeEscalation:
		parts = append(parts, "执行人未在规定时间内确认，已升级通知")
	}

	return strings.Join(parts, "\n")
}
