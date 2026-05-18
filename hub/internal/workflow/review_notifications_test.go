package workflow

import (
	"context"
	"sync"
	"testing"
	"time"
)

// mockReviewNotifier records all notification calls for testing.
type mockReviewNotifier struct {
	mu          sync.Mutex
	authorCalls []authorNotification
	adminCalls  []adminNotification
	authorErr   error
	adminErr    error
}

type authorNotification struct {
	AuthorID        string
	WorkflowName    string
	VersionNumber   string
	Status          VersionStatus
	RejectionReason string
}

type adminNotification struct {
	AdminID        string
	WorkflowName   string
	AuthorName     string
	VersionNumber  string
	SubmissionDate time.Time
}

func (m *mockReviewNotifier) NotifyAuthor(ctx context.Context, authorID string, workflowName string, versionNumber string, status VersionStatus, rejectionReason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.authorCalls = append(m.authorCalls, authorNotification{
		AuthorID:        authorID,
		WorkflowName:    workflowName,
		VersionNumber:   versionNumber,
		Status:          status,
		RejectionReason: rejectionReason,
	})
	return m.authorErr
}

func (m *mockReviewNotifier) NotifyAdmin(ctx context.Context, adminID string, workflowName string, authorName string, versionNumber string, submissionDate time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.adminCalls = append(m.adminCalls, adminNotification{
		AdminID:        adminID,
		WorkflowName:   workflowName,
		AuthorName:     authorName,
		VersionNumber:  versionNumber,
		SubmissionDate: submissionDate,
	})
	return m.adminErr
}

// mockReviewWorkflowStore implements WorkflowStore for review notification tests.
type mockReviewWorkflowStore struct {
	workflows      map[string]*WorkflowDefinition
	pendingReviews []WorkflowVersion
}

func newMockReviewWorkflowStore() *mockReviewWorkflowStore {
	return &mockReviewWorkflowStore{
		workflows: make(map[string]*WorkflowDefinition),
	}
}

func (m *mockReviewWorkflowStore) CreateWorkflow(ctx context.Context, def *WorkflowDefinition) error {
	m.workflows[def.ID] = def
	return nil
}
func (m *mockReviewWorkflowStore) GetWorkflow(ctx context.Context, id string) (*WorkflowDefinition, error) {
	return m.workflows[id], nil
}
func (m *mockReviewWorkflowStore) ListWorkflows(ctx context.Context, ownerID string) ([]WorkflowDefinition, error) {
	return nil, nil
}
func (m *mockReviewWorkflowStore) CreateVersion(ctx context.Context, ver *WorkflowVersion) error {
	return nil
}
func (m *mockReviewWorkflowStore) GetVersion(ctx context.Context, id string) (*WorkflowVersion, error) {
	return nil, nil
}
func (m *mockReviewWorkflowStore) GetPublishedVersion(ctx context.Context, workflowID string) (*WorkflowVersion, error) {
	return nil, nil
}
func (m *mockReviewWorkflowStore) UpdateVersionStatus(ctx context.Context, id string, status VersionStatus, reason string) error {
	return nil
}
func (m *mockReviewWorkflowStore) ListVersions(ctx context.Context, workflowID string) ([]WorkflowVersion, error) {
	return nil, nil
}
func (m *mockReviewWorkflowStore) ListPendingReviews(ctx context.Context, page, pageSize int) ([]WorkflowVersion, int, error) {
	start := (page - 1) * pageSize
	if start >= len(m.pendingReviews) {
		return nil, len(m.pendingReviews), nil
	}
	end := start + pageSize
	if end > len(m.pendingReviews) {
		end = len(m.pendingReviews)
	}
	return m.pendingReviews[start:end], len(m.pendingReviews), nil
}

// --- Tests ---

func TestNotifyStatusChange_Published(t *testing.T) {
	notifier := &mockReviewNotifier{}
	store := newMockReviewWorkflowStore()
	svc := NewReviewNotificationService(notifier, store, ReviewNotificationConfig{})

	err := svc.NotifyStatusChange(context.Background(), "user_1", "采购审批", "1.0.0", VersionPublished, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(notifier.authorCalls) != 1 {
		t.Fatalf("expected 1 author notification, got %d", len(notifier.authorCalls))
	}

	call := notifier.authorCalls[0]
	if call.AuthorID != "user_1" {
		t.Errorf("expected authorID=user_1, got %s", call.AuthorID)
	}
	if call.WorkflowName != "采购审批" {
		t.Errorf("expected workflowName=采购审批, got %s", call.WorkflowName)
	}
	if call.VersionNumber != "1.0.0" {
		t.Errorf("expected versionNumber=1.0.0, got %s", call.VersionNumber)
	}
	if call.Status != VersionPublished {
		t.Errorf("expected status=published, got %s", call.Status)
	}
	if call.RejectionReason != "" {
		t.Errorf("expected empty rejectionReason, got %s", call.RejectionReason)
	}
}

func TestNotifyStatusChange_Rejected_IncludesReason(t *testing.T) {
	notifier := &mockReviewNotifier{}
	store := newMockReviewWorkflowStore()
	svc := NewReviewNotificationService(notifier, store, ReviewNotificationConfig{})

	reason := "Missing approval node configuration for the finance department branch"
	err := svc.NotifyStatusChange(context.Background(), "user_2", "报销审批", "2.1.0", VersionRejected, reason)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(notifier.authorCalls) != 1 {
		t.Fatalf("expected 1 author notification, got %d", len(notifier.authorCalls))
	}

	call := notifier.authorCalls[0]
	if call.Status != VersionRejected {
		t.Errorf("expected status=rejected, got %s", call.Status)
	}
	if call.RejectionReason != reason {
		t.Errorf("expected rejectionReason=%q, got %q", reason, call.RejectionReason)
	}
}

func TestNotifyStatusChange_IgnoresNonPublishRejectStatuses(t *testing.T) {
	notifier := &mockReviewNotifier{}
	store := newMockReviewWorkflowStore()
	svc := NewReviewNotificationService(notifier, store, ReviewNotificationConfig{})

	// Draft status should not trigger notification.
	_ = svc.NotifyStatusChange(context.Background(), "user_1", "test", "1.0.0", VersionDraft, "")
	// Pending review should not trigger notification.
	_ = svc.NotifyStatusChange(context.Background(), "user_1", "test", "1.0.0", VersionPendingReview, "")
	// Superseded should not trigger notification.
	_ = svc.NotifyStatusChange(context.Background(), "user_1", "test", "1.0.0", VersionSuperseded, "")

	if len(notifier.authorCalls) != 0 {
		t.Errorf("expected 0 author notifications for non-publish/reject statuses, got %d", len(notifier.authorCalls))
	}
}

func TestNotifyStatusChange_NilNotifier(t *testing.T) {
	store := newMockReviewWorkflowStore()
	svc := NewReviewNotificationService(nil, store, ReviewNotificationConfig{})

	// Should not panic with nil notifier.
	err := svc.NotifyStatusChange(context.Background(), "user_1", "test", "1.0.0", VersionPublished, "")
	if err != nil {
		t.Fatalf("unexpected error with nil notifier: %v", err)
	}
}

func TestAdminReminder_SendsForStaleSubmissions(t *testing.T) {
	notifier := &mockReviewNotifier{}
	store := newMockReviewWorkflowStore()

	// Add a workflow definition for name resolution.
	store.workflows["wf_1"] = &WorkflowDefinition{
		ID:   "wf_1",
		Name: "采购审批流程",
	}

	// Add a pending review submission that was submitted 7 days + 30 minutes ago.
	submittedAt := time.Now().UTC().Add(-7*24*time.Hour - 30*time.Minute)
	store.pendingReviews = []WorkflowVersion{
		{
			ID:            "ver_1",
			WorkflowID:    "wf_1",
			VersionNumber: "1.0.0",
			Status:        VersionPendingReview,
			SubmittedAt:   &submittedAt,
		},
	}

	svc := NewReviewNotificationService(notifier, store, ReviewNotificationConfig{
		AdminIDs:         []string{"admin_1", "admin_2"},
		ReminderInterval: 7 * 24 * time.Hour,
		CheckInterval:    1 * time.Hour, // boundary crossed within last hour
	})

	// Directly call checkAndSendReminders to test the logic.
	svc.checkAndSendReminders()

	notifier.mu.Lock()
	defer notifier.mu.Unlock()

	if len(notifier.adminCalls) != 2 {
		t.Fatalf("expected 2 admin notifications (one per admin), got %d", len(notifier.adminCalls))
	}

	// Verify first admin notification.
	call := notifier.adminCalls[0]
	if call.AdminID != "admin_1" {
		t.Errorf("expected adminID=admin_1, got %s", call.AdminID)
	}
	if call.WorkflowName != "采购审批流程" {
		t.Errorf("expected workflowName=采购审批流程, got %s", call.WorkflowName)
	}
	if call.VersionNumber != "1.0.0" {
		t.Errorf("expected versionNumber=1.0.0, got %s", call.VersionNumber)
	}

	// Verify second admin notification.
	call2 := notifier.adminCalls[1]
	if call2.AdminID != "admin_2" {
		t.Errorf("expected adminID=admin_2, got %s", call2.AdminID)
	}
}

func TestAdminReminder_SkipsRecentSubmissions(t *testing.T) {
	notifier := &mockReviewNotifier{}
	store := newMockReviewWorkflowStore()

	// Add a pending review submission that was submitted only 3 days ago.
	submittedAt := time.Now().UTC().Add(-3 * 24 * time.Hour)
	store.pendingReviews = []WorkflowVersion{
		{
			ID:            "ver_1",
			WorkflowID:    "wf_1",
			VersionNumber: "1.0.0",
			Status:        VersionPendingReview,
			SubmittedAt:   &submittedAt,
		},
	}

	svc := NewReviewNotificationService(notifier, store, ReviewNotificationConfig{
		AdminIDs:         []string{"admin_1"},
		ReminderInterval: 7 * 24 * time.Hour,
		CheckInterval:    1 * time.Hour,
	})

	svc.checkAndSendReminders()

	notifier.mu.Lock()
	defer notifier.mu.Unlock()

	if len(notifier.adminCalls) != 0 {
		t.Errorf("expected 0 admin notifications for recent submission, got %d", len(notifier.adminCalls))
	}
}

func TestAdminReminder_NoAdmins(t *testing.T) {
	notifier := &mockReviewNotifier{}
	store := newMockReviewWorkflowStore()

	submittedAt := time.Now().UTC().Add(-8 * 24 * time.Hour)
	store.pendingReviews = []WorkflowVersion{
		{
			ID:            "ver_1",
			WorkflowID:    "wf_1",
			VersionNumber: "1.0.0",
			Status:        VersionPendingReview,
			SubmittedAt:   &submittedAt,
		},
	}

	// No admin IDs configured — should not send any reminders.
	svc := NewReviewNotificationService(notifier, store, ReviewNotificationConfig{
		AdminIDs:         nil,
		ReminderInterval: 7 * 24 * time.Hour,
		CheckInterval:    1 * time.Hour,
	})

	svc.checkAndSendReminders()

	notifier.mu.Lock()
	defer notifier.mu.Unlock()

	if len(notifier.adminCalls) != 0 {
		t.Errorf("expected 0 admin notifications with no admins configured, got %d", len(notifier.adminCalls))
	}
}

func TestAdminReminder_NilSubmittedAt(t *testing.T) {
	notifier := &mockReviewNotifier{}
	store := newMockReviewWorkflowStore()

	// Submission with nil SubmittedAt should be skipped.
	store.pendingReviews = []WorkflowVersion{
		{
			ID:            "ver_1",
			WorkflowID:    "wf_1",
			VersionNumber: "1.0.0",
			Status:        VersionPendingReview,
			SubmittedAt:   nil,
		},
	}

	svc := NewReviewNotificationService(notifier, store, ReviewNotificationConfig{
		AdminIDs:         []string{"admin_1"},
		ReminderInterval: 7 * 24 * time.Hour,
		CheckInterval:    1 * time.Hour,
	})

	svc.checkAndSendReminders()

	notifier.mu.Lock()
	defer notifier.mu.Unlock()

	if len(notifier.adminCalls) != 0 {
		t.Errorf("expected 0 admin notifications for nil submittedAt, got %d", len(notifier.adminCalls))
	}
}

func TestStartStop(t *testing.T) {
	notifier := &mockReviewNotifier{}
	store := newMockReviewWorkflowStore()

	svc := NewReviewNotificationService(notifier, store, ReviewNotificationConfig{
		AdminIDs:      []string{"admin_1"},
		CheckInterval: 50 * time.Millisecond,
	})

	// Start and stop should not panic or deadlock.
	svc.Start()
	time.Sleep(100 * time.Millisecond)
	svc.Stop()
}

func TestResolveWorkflowName_Fallback(t *testing.T) {
	store := newMockReviewWorkflowStore()
	svc := NewReviewNotificationService(nil, store, ReviewNotificationConfig{})

	// Unknown workflow ID should return fallback name.
	name := svc.resolveWorkflowName(context.Background(), "unknown_id")
	expected := "workflow_unknown_id"
	if name != expected {
		t.Errorf("expected fallback name %q, got %q", expected, name)
	}

	// Known workflow should return its name.
	store.workflows["wf_known"] = &WorkflowDefinition{ID: "wf_known", Name: "已知流程"}
	name = svc.resolveWorkflowName(context.Background(), "wf_known")
	if name != "已知流程" {
		t.Errorf("expected name=已知流程, got %s", name)
	}
}
