package workflow

import (
	"context"
	"fmt"
	"log"
	"time"
)

// HubReviewNotifier implements ReviewNotifier using the Hub's notification infrastructure.
// It sends notifications via the Hub's internal event system (which routes to IM channels,
// in-app notifications, etc. depending on user preferences).
type HubReviewNotifier struct {
	// notifySender is a function that sends a notification to a user.
	// This is injected from the Hub's existing notification service.
	notifySender func(ctx context.Context, userID, title, body string) error
}

// NewHubReviewNotifier creates a HubReviewNotifier with the given notification sender function.
// If sender is nil, notifications are logged but not delivered (graceful degradation).
func NewHubReviewNotifier(sender func(ctx context.Context, userID, title, body string) error) *HubReviewNotifier {
	return &HubReviewNotifier{notifySender: sender}
}

func (n *HubReviewNotifier) NotifyAuthor(ctx context.Context, authorID string, workflowName string, versionNumber string, status VersionStatus, rejectionReason string) error {
	var title, body string
	switch status {
	case VersionPublished:
		title = "工作流已发布"
		body = fmt.Sprintf("您的工作流「%s」(v%s) 已通过审核并发布到能力市场。", workflowName, versionNumber)
	case VersionRejected:
		title = "工作流审核未通过"
		body = fmt.Sprintf("您的工作流「%s」(v%s) 未通过审核。\n原因：%s", workflowName, versionNumber, rejectionReason)
	default:
		return nil
	}

	if n.notifySender == nil {
		log.Printf("[ReviewNotifier] (no sender configured) → %s: %s — %s", authorID, title, body)
		return nil
	}
	return n.notifySender(ctx, authorID, title, body)
}

func (n *HubReviewNotifier) NotifyAdmin(ctx context.Context, adminID string, workflowName string, authorName string, versionNumber string, submissionDate time.Time) error {
	title := "工作流待审核提醒"
	daysSince := int(time.Since(submissionDate).Hours() / 24)
	body := fmt.Sprintf("工作流「%s」(v%s) 由 %s 提交，已等待 %d 天未处理。请及时审核。",
		workflowName, versionNumber, authorName, daysSince)

	if n.notifySender == nil {
		log.Printf("[ReviewNotifier] (no sender configured) → %s: %s — %s", adminID, title, body)
		return nil
	}
	return n.notifySender(ctx, adminID, title, body)
}
