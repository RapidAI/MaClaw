package workflow

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// ReviewNotifier defines the interface for sending review-related notifications.
// Implementations can use the Hub's existing notification infrastructure
// (e.g., IM broadcast, email, in-app notifications).
type ReviewNotifier interface {
	// NotifyAuthor sends a notification to the workflow author about a status change.
	// status is the new version status (e.g., "published" or "rejected").
	// rejectionReason is non-empty only when status is "rejected".
	NotifyAuthor(ctx context.Context, authorID string, workflowName string, versionNumber string, status VersionStatus, rejectionReason string) error

	// NotifyAdmin sends a reminder notification to an admin about an unactioned submission.
	// submissionDate is when the version was submitted for review.
	NotifyAdmin(ctx context.Context, adminID string, workflowName string, authorName string, versionNumber string, submissionDate time.Time) error
}

// ReviewNotificationService manages review notifications:
// - Sends notifications to authors when their submission status changes (within 60 seconds).
// - Sends reminders to admins every 7 days for unactioned submissions.
type ReviewNotificationService struct {
	notifier ReviewNotifier
	store    WorkflowStore

	// adminIDs is the list of admin user IDs who should receive reminders.
	adminIDs []string

	// reminderInterval is the interval between admin reminders (default: 7 days).
	reminderInterval time.Duration

	// checkInterval is how often the background job checks for stale submissions.
	checkInterval time.Duration

	// stopCh signals the background goroutine to stop.
	stopCh chan struct{}
	// wg tracks the background goroutine for clean shutdown.
	wg sync.WaitGroup
}

// ReviewNotificationConfig holds configuration for the ReviewNotificationService.
type ReviewNotificationConfig struct {
	// AdminIDs is the list of admin user IDs who receive reminders for unactioned submissions.
	AdminIDs []string

	// ReminderInterval is how often reminders are sent for stale submissions.
	// Default: 7 days (168 hours).
	ReminderInterval time.Duration

	// CheckInterval is how often the background job checks for stale submissions.
	// Default: 1 hour.
	CheckInterval time.Duration
}

// NewReviewNotificationService creates a new ReviewNotificationService.
func NewReviewNotificationService(notifier ReviewNotifier, store WorkflowStore, cfg ReviewNotificationConfig) *ReviewNotificationService {
	reminderInterval := cfg.ReminderInterval
	if reminderInterval <= 0 {
		reminderInterval = 7 * 24 * time.Hour // 7 days
	}

	checkInterval := cfg.CheckInterval
	if checkInterval <= 0 {
		checkInterval = 1 * time.Hour
	}

	return &ReviewNotificationService{
		notifier:         notifier,
		store:            store,
		adminIDs:         cfg.AdminIDs,
		reminderInterval: reminderInterval,
		checkInterval:    checkInterval,
		stopCh:           make(chan struct{}),
	}
}

// NotifyStatusChange sends a notification to the workflow author when a submission
// status transitions to "published" or "rejected". This should be called within
// 60 seconds of the status change (per requirement 7.5).
//
// Parameters:
//   - authorID: the user ID of the workflow author
//   - workflowName: the name of the workflow
//   - versionNumber: the version number (e.g., "1.2.0")
//   - newStatus: the new status ("published" or "rejected")
//   - rejectionReason: the rejection reason (only relevant when newStatus is "rejected")
func (s *ReviewNotificationService) NotifyStatusChange(ctx context.Context, authorID string, workflowName string, versionNumber string, newStatus VersionStatus, rejectionReason string) error {
	if s.notifier == nil {
		return nil
	}

	// Only notify for published or rejected transitions.
	if newStatus != VersionPublished && newStatus != VersionRejected {
		return nil
	}

	return s.notifier.NotifyAuthor(ctx, authorID, workflowName, versionNumber, newStatus, rejectionReason)
}

// Start begins the background goroutine that periodically checks for unactioned
// submissions and sends reminders to admins every 7 days.
func (s *ReviewNotificationService) Start() {
	s.wg.Add(1)
	go s.reminderLoop()
}

// Stop signals the background goroutine to stop and waits for it to finish.
func (s *ReviewNotificationService) Stop() {
	close(s.stopCh)
	s.wg.Wait()
}

// reminderLoop is the background goroutine that periodically checks for
// pending_review submissions older than 7 days and sends reminders to admins.
func (s *ReviewNotificationService) reminderLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.checkAndSendReminders()
		}
	}
}

// checkAndSendReminders queries for pending_review submissions that have been
// unactioned for longer than the reminder interval, and sends reminders to admins.
func (s *ReviewNotificationService) checkAndSendReminders() {
	if s.notifier == nil || len(s.adminIDs) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Fetch all pending review submissions (paginate through all pages).
	page := 1
	pageSize := 50
	now := time.Now().UTC()

	for {
		versions, total, err := s.store.ListPendingReviews(ctx, page, pageSize)
		if err != nil {
			log.Printf("[ReviewNotifications] error listing pending reviews: %v", err)
			return
		}

		for _, ver := range versions {
			if ver.SubmittedAt == nil {
				continue
			}

			// Check if the submission has been pending for longer than the reminder interval.
			age := now.Sub(*ver.SubmittedAt)
			if age < s.reminderInterval {
				continue
			}

			// Check if it's time for a reminder (every reminderInterval).
			// We send a reminder if the age is a multiple of the reminder interval.
			// To avoid sending multiple reminders in the same check window,
			// we check if the submission crossed a reminder boundary within the
			// last check interval.
			reminderNumber := int(age / s.reminderInterval)
			if reminderNumber <= 0 {
				continue
			}

			// Calculate when the last reminder boundary was crossed.
			lastBoundary := ver.SubmittedAt.Add(time.Duration(reminderNumber) * s.reminderInterval)
			timeSinceBoundary := now.Sub(lastBoundary)

			// Only send if the boundary was crossed within the last check interval.
			if timeSinceBoundary > s.checkInterval {
				continue
			}

			// Look up the workflow name for the notification.
			workflowName := s.resolveWorkflowName(ctx, ver.WorkflowID)

			// Send reminder to all admins.
			for _, adminID := range s.adminIDs {
				err := s.notifier.NotifyAdmin(ctx, adminID, workflowName, ver.WorkflowID, ver.VersionNumber, *ver.SubmittedAt)
				if err != nil {
					log.Printf("[ReviewNotifications] error sending admin reminder: admin=%s workflow=%s version=%s err=%v",
						adminID, workflowName, ver.VersionNumber, err)
				}
			}
		}

		// Check if we've processed all pages.
		if page*pageSize >= total {
			break
		}
		page++
	}
}

// resolveWorkflowName looks up the workflow name by ID.
// Returns a fallback string if the lookup fails.
func (s *ReviewNotificationService) resolveWorkflowName(ctx context.Context, workflowID string) string {
	wf, err := s.store.GetWorkflow(ctx, workflowID)
	if err != nil || wf == nil {
		return fmt.Sprintf("workflow_%s", workflowID)
	}
	return wf.Name
}
