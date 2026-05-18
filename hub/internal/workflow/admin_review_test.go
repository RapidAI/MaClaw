package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// mockWorkflowStoreForAdmin implements WorkflowStore for admin review tests.
type mockWorkflowStoreForAdmin struct {
	versions   map[string]*WorkflowVersion
	workflows  map[string]*WorkflowDefinition
	statusLog  []statusUpdate
}

type statusUpdate struct {
	ID     string
	Status VersionStatus
	Reason string
}

func newMockStoreForAdmin() *mockWorkflowStoreForAdmin {
	return &mockWorkflowStoreForAdmin{
		versions:  make(map[string]*WorkflowVersion),
		workflows: make(map[string]*WorkflowDefinition),
	}
}

func (m *mockWorkflowStoreForAdmin) CreateWorkflow(_ context.Context, def *WorkflowDefinition) error {
	m.workflows[def.ID] = def
	return nil
}

func (m *mockWorkflowStoreForAdmin) GetWorkflow(_ context.Context, id string) (*WorkflowDefinition, error) {
	return m.workflows[id], nil
}

func (m *mockWorkflowStoreForAdmin) ListWorkflows(_ context.Context, _ string) ([]WorkflowDefinition, error) {
	return nil, nil
}

func (m *mockWorkflowStoreForAdmin) CreateVersion(_ context.Context, ver *WorkflowVersion) error {
	m.versions[ver.ID] = ver
	return nil
}

func (m *mockWorkflowStoreForAdmin) GetVersion(_ context.Context, id string) (*WorkflowVersion, error) {
	v, ok := m.versions[id]
	if !ok {
		return nil, nil
	}
	return v, nil
}

func (m *mockWorkflowStoreForAdmin) GetPublishedVersion(_ context.Context, workflowID string) (*WorkflowVersion, error) {
	for _, v := range m.versions {
		if v.WorkflowID == workflowID && v.Status == VersionPublished {
			return v, nil
		}
	}
	return nil, nil
}

func (m *mockWorkflowStoreForAdmin) UpdateVersionStatus(_ context.Context, id string, status VersionStatus, reason string) error {
	v, ok := m.versions[id]
	if !ok {
		return errors.New("version not found")
	}
	v.Status = status
	if reason != "" {
		v.RejectionReason = reason
	}
	m.statusLog = append(m.statusLog, statusUpdate{ID: id, Status: status, Reason: reason})
	return nil
}

func (m *mockWorkflowStoreForAdmin) ListVersions(_ context.Context, _ string) ([]WorkflowVersion, error) {
	return nil, nil
}

func (m *mockWorkflowStoreForAdmin) ListPendingReviews(_ context.Context, page, pageSize int) ([]WorkflowVersion, int, error) {
	var pending []WorkflowVersion
	for _, v := range m.versions {
		if v.Status == VersionPendingReview {
			pending = append(pending, *v)
		}
	}
	// Sort by submitted_at ASC (simplified for test).
	total := len(pending)
	offset := (page - 1) * pageSize
	if offset >= total {
		return nil, total, nil
	}
	end := offset + pageSize
	if end > total {
		end = total
	}
	return pending[offset:end], total, nil
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestAdminReview_ListPendingSubmissions(t *testing.T) {
	store := newMockStoreForAdmin()
	svc := NewAdminReviewService(store, nil)
	ctx := context.Background()

	// Add a workflow definition.
	store.workflows["wf1"] = &WorkflowDefinition{
		ID:      "wf1",
		OwnerID: "user_alice",
		Name:    "采购审批流程",
	}

	// Add pending review versions.
	now := time.Now().UTC()
	store.versions["v1"] = &WorkflowVersion{
		ID:            "v1",
		WorkflowID:    "wf1",
		VersionNumber: "1.0.0",
		Status:        VersionPendingReview,
		SubmittedAt:   &now,
		CreatedAt:     now,
		UpdatedAt:     now,
		Graph:         WorkflowGraph{Nodes: []WorkflowNode{{ID: "n1", Type: NodeTrigger}}},
	}
	store.versions["v2"] = &WorkflowVersion{
		ID:            "v2",
		WorkflowID:    "wf1",
		VersionNumber: "1.1.0",
		Status:        VersionDraft, // not pending
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	page, err := svc.ListPendingSubmissions(ctx, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if page.Total != 1 {
		t.Errorf("expected total=1, got %d", page.Total)
	}
	if len(page.Submissions) != 1 {
		t.Fatalf("expected 1 submission, got %d", len(page.Submissions))
	}
	if page.Submissions[0].WorkflowName != "采购审批流程" {
		t.Errorf("expected workflow name '采购审批流程', got %q", page.Submissions[0].WorkflowName)
	}
	if page.Submissions[0].AuthorID != "user_alice" {
		t.Errorf("expected author 'user_alice', got %q", page.Submissions[0].AuthorID)
	}
	if page.PageSize != 50 {
		t.Errorf("expected page size 50, got %d", page.PageSize)
	}
}

func TestAdminReview_GetSubmissionForReview(t *testing.T) {
	store := newMockStoreForAdmin()
	svc := NewAdminReviewService(store, nil)
	ctx := context.Background()

	store.workflows["wf1"] = &WorkflowDefinition{
		ID:          "wf1",
		OwnerID:     "user_bob",
		Name:        "请假审批",
		Description: "员工请假审批流程",
	}

	approvalCfg, _ := json.Marshal(ApprovalNodeConfig{
		ApproverIDs:  []string{"ve_001"},
		Mode:         ModeSingle,
		TimeoutHours: 24,
	})

	now := time.Now().UTC()
	store.versions["v1"] = &WorkflowVersion{
		ID:            "v1",
		WorkflowID:    "wf1",
		VersionNumber: "1.0.0",
		Status:        VersionPendingReview,
		SubmittedAt:   &now,
		CreatedAt:     now,
		UpdatedAt:     now,
		Graph: WorkflowGraph{
			Nodes: []WorkflowNode{
				{ID: "n1", Type: NodeTrigger, Label: "Start"},
				{ID: "n2", Type: NodeApproval, Label: "Manager Approval", Config: approvalCfg},
			},
			Edges: []WorkflowEdge{
				{ID: "e1", SourceID: "n1", TargetID: "n2"},
			},
		},
	}

	detail, err := svc.GetSubmissionForReview(ctx, "v1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if detail.WorkflowName != "请假审批" {
		t.Errorf("expected name '请假审批', got %q", detail.WorkflowName)
	}
	if detail.WorkflowDesc != "员工请假审批流程" {
		t.Errorf("expected desc '员工请假审批流程', got %q", detail.WorkflowDesc)
	}
	if detail.AuthorID != "user_bob" {
		t.Errorf("expected author 'user_bob', got %q", detail.AuthorID)
	}
	if len(detail.Graph.Nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(detail.Graph.Nodes))
	}
	if len(detail.NodeConfigs) != 2 {
		t.Errorf("expected 2 node configs, got %d", len(detail.NodeConfigs))
	}
}

func TestAdminReview_GetSubmissionForReview_NotPending(t *testing.T) {
	store := newMockStoreForAdmin()
	svc := NewAdminReviewService(store, nil)
	ctx := context.Background()

	now := time.Now().UTC()
	store.versions["v1"] = &WorkflowVersion{
		ID:         "v1",
		WorkflowID: "wf1",
		Status:     VersionDraft,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	_, err := svc.GetSubmissionForReview(ctx, "v1")
	if err == nil {
		t.Fatal("expected error for non-pending version")
	}
}

func TestAdminReview_ApproveSubmission(t *testing.T) {
	store := newMockStoreForAdmin()
	svc := NewAdminReviewService(store, nil)
	ctx := context.Background()

	store.workflows["wf1"] = &WorkflowDefinition{
		ID:      "wf1",
		OwnerID: "user_alice",
		Name:    "采购审批",
	}

	now := time.Now().UTC()
	store.versions["v1"] = &WorkflowVersion{
		ID:            "v1",
		WorkflowID:    "wf1",
		VersionNumber: "1.0.0",
		Status:        VersionPendingReview,
		SubmittedAt:   &now,
		CreatedAt:     now,
		UpdatedAt:     now,
		Graph:         WorkflowGraph{Nodes: []WorkflowNode{{ID: "n1", Type: NodeTrigger}}},
	}

	err := svc.ApproveSubmission(ctx, "v1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify status transitioned to published.
	if store.versions["v1"].Status != VersionPublished {
		t.Errorf("expected status 'published', got %q", store.versions["v1"].Status)
	}
}

func TestAdminReview_ApproveSubmission_SupersedesPrevious(t *testing.T) {
	store := newMockStoreForAdmin()
	svc := NewAdminReviewService(store, nil)
	ctx := context.Background()

	store.workflows["wf1"] = &WorkflowDefinition{
		ID:      "wf1",
		OwnerID: "user_alice",
		Name:    "采购审批",
	}

	now := time.Now().UTC()
	// Previous published version.
	store.versions["v_old"] = &WorkflowVersion{
		ID:            "v_old",
		WorkflowID:    "wf1",
		VersionNumber: "1.0.0",
		Status:        VersionPublished,
		PublishedAt:   &now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	// New pending version.
	store.versions["v_new"] = &WorkflowVersion{
		ID:            "v_new",
		WorkflowID:    "wf1",
		VersionNumber: "1.1.0",
		Status:        VersionPendingReview,
		SubmittedAt:   &now,
		CreatedAt:     now,
		UpdatedAt:     now,
		Graph:         WorkflowGraph{Nodes: []WorkflowNode{{ID: "n1", Type: NodeTrigger}}},
	}

	err := svc.ApproveSubmission(ctx, "v_new")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Old version should be superseded.
	if store.versions["v_old"].Status != VersionSuperseded {
		t.Errorf("expected old version status 'superseded', got %q", store.versions["v_old"].Status)
	}
	// New version should be published.
	if store.versions["v_new"].Status != VersionPublished {
		t.Errorf("expected new version status 'published', got %q", store.versions["v_new"].Status)
	}
}

func TestAdminReview_ApproveSubmission_NotPending(t *testing.T) {
	store := newMockStoreForAdmin()
	svc := NewAdminReviewService(store, nil)
	ctx := context.Background()

	now := time.Now().UTC()
	store.versions["v1"] = &WorkflowVersion{
		ID:         "v1",
		WorkflowID: "wf1",
		Status:     VersionDraft,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	err := svc.ApproveSubmission(ctx, "v1")
	if err == nil {
		t.Fatal("expected error for non-pending version")
	}
}

func TestAdminReview_RejectSubmission(t *testing.T) {
	store := newMockStoreForAdmin()
	svc := NewAdminReviewService(store, nil)
	ctx := context.Background()

	now := time.Now().UTC()
	store.versions["v1"] = &WorkflowVersion{
		ID:            "v1",
		WorkflowID:    "wf1",
		VersionNumber: "1.0.0",
		Status:        VersionPendingReview,
		SubmittedAt:   &now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	reason := "工作流缺少必要的错误处理节点，请添加异常分支后重新提交。"
	err := svc.RejectSubmission(ctx, "v1", reason)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if store.versions["v1"].Status != VersionRejected {
		t.Errorf("expected status 'rejected', got %q", store.versions["v1"].Status)
	}
	if store.versions["v1"].RejectionReason != reason {
		t.Errorf("expected rejection reason %q, got %q", reason, store.versions["v1"].RejectionReason)
	}
}

func TestAdminReview_RejectSubmission_ReasonTooShort(t *testing.T) {
	store := newMockStoreForAdmin()
	svc := NewAdminReviewService(store, nil)
	ctx := context.Background()

	now := time.Now().UTC()
	store.versions["v1"] = &WorkflowVersion{
		ID:         "v1",
		WorkflowID: "wf1",
		Status:     VersionPendingReview,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	err := svc.RejectSubmission(ctx, "v1", "too short")
	if err == nil {
		t.Fatal("expected error for short reason")
	}
	// Version status should not change.
	if store.versions["v1"].Status != VersionPendingReview {
		t.Errorf("status should remain pending_review, got %q", store.versions["v1"].Status)
	}
}

func TestAdminReview_RejectSubmission_ReasonTooLong(t *testing.T) {
	store := newMockStoreForAdmin()
	svc := NewAdminReviewService(store, nil)
	ctx := context.Background()

	now := time.Now().UTC()
	store.versions["v1"] = &WorkflowVersion{
		ID:         "v1",
		WorkflowID: "wf1",
		Status:     VersionPendingReview,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	// Create a reason that exceeds 2000 runes.
	longReason := make([]rune, 2001)
	for i := range longReason {
		longReason[i] = '中'
	}
	err := svc.RejectSubmission(ctx, "v1", string(longReason))
	if err == nil {
		t.Fatal("expected error for long reason")
	}
}

func TestAdminReview_UnpublishVersion(t *testing.T) {
	store := newMockStoreForAdmin()
	svc := NewAdminReviewService(store, nil)
	ctx := context.Background()

	now := time.Now().UTC()
	store.versions["v1"] = &WorkflowVersion{
		ID:            "v1",
		WorkflowID:    "wf1",
		VersionNumber: "1.0.0",
		Status:        VersionPublished,
		PublishedAt:   &now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	err := svc.UnpublishVersion(ctx, "v1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if store.versions["v1"].Status != VersionUnpublished {
		t.Errorf("expected status 'unpublished', got %q", store.versions["v1"].Status)
	}
}

func TestAdminReview_UnpublishVersion_NotPublished(t *testing.T) {
	store := newMockStoreForAdmin()
	svc := NewAdminReviewService(store, nil)
	ctx := context.Background()

	now := time.Now().UTC()
	store.versions["v1"] = &WorkflowVersion{
		ID:         "v1",
		WorkflowID: "wf1",
		Status:     VersionDraft,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	err := svc.UnpublishVersion(ctx, "v1")
	if err == nil {
		t.Fatal("expected error for non-published version")
	}
}

func TestAdminReview_ApproveSubmission_NotFound(t *testing.T) {
	store := newMockStoreForAdmin()
	svc := NewAdminReviewService(store, nil)
	ctx := context.Background()

	err := svc.ApproveSubmission(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent version")
	}
}

func TestAdminReview_ExtractApprovalModes(t *testing.T) {
	singleCfg, _ := json.Marshal(ApprovalNodeConfig{Mode: ModeSingle})
	counterCfg, _ := json.Marshal(ApprovalNodeConfig{Mode: ModeCountersign})

	graph := WorkflowGraph{
		Nodes: []WorkflowNode{
			{ID: "n1", Type: NodeTrigger},
			{ID: "n2", Type: NodeApproval, Config: singleCfg},
			{ID: "n3", Type: NodeApproval, Config: counterCfg},
			{ID: "n4", Type: NodeApproval, Config: singleCfg}, // duplicate mode
		},
	}

	modes := extractApprovalModes(graph)
	if len(modes) != 2 {
		t.Errorf("expected 2 distinct modes, got %d: %v", len(modes), modes)
	}

	modeSet := map[string]bool{}
	for _, m := range modes {
		modeSet[m] = true
	}
	if !modeSet["single"] {
		t.Error("expected 'single' mode in result")
	}
	if !modeSet["countersign"] {
		t.Error("expected 'countersign' mode in result")
	}
}
