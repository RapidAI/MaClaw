package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/workflow"
)

// mockAdminReviewStore implements workflow.WorkflowStore for testing.
type mockAdminReviewStore struct {
	versions  map[string]*workflow.WorkflowVersion
	workflows map[string]*workflow.WorkflowDefinition
}

func newMockAdminReviewStore() *mockAdminReviewStore {
	return &mockAdminReviewStore{
		versions:  make(map[string]*workflow.WorkflowVersion),
		workflows: make(map[string]*workflow.WorkflowDefinition),
	}
}

func (s *mockAdminReviewStore) CreateWorkflow(_ context.Context, def *workflow.WorkflowDefinition) error {
	s.workflows[def.ID] = def
	return nil
}

func (s *mockAdminReviewStore) GetWorkflow(_ context.Context, id string) (*workflow.WorkflowDefinition, error) {
	return s.workflows[id], nil
}

func (s *mockAdminReviewStore) ListWorkflows(_ context.Context, _ string) ([]workflow.WorkflowDefinition, error) {
	return nil, nil
}

func (s *mockAdminReviewStore) CreateVersion(_ context.Context, ver *workflow.WorkflowVersion) error {
	s.versions[ver.ID] = ver
	return nil
}

func (s *mockAdminReviewStore) GetVersion(_ context.Context, id string) (*workflow.WorkflowVersion, error) {
	return s.versions[id], nil
}

func (s *mockAdminReviewStore) GetPublishedVersion(_ context.Context, workflowID string) (*workflow.WorkflowVersion, error) {
	for _, v := range s.versions {
		if v.WorkflowID == workflowID && v.Status == workflow.VersionPublished {
			return v, nil
		}
	}
	return nil, nil
}

func (s *mockAdminReviewStore) UpdateVersionStatus(_ context.Context, id string, status workflow.VersionStatus, reason string) error {
	if v, ok := s.versions[id]; ok {
		v.Status = status
		v.RejectionReason = reason
	}
	return nil
}

func (s *mockAdminReviewStore) ListVersions(_ context.Context, _ string) ([]workflow.WorkflowVersion, error) {
	return nil, nil
}

func (s *mockAdminReviewStore) ListPendingReviews(_ context.Context, page, pageSize int) ([]workflow.WorkflowVersion, int, error) {
	var pending []workflow.WorkflowVersion
	for _, v := range s.versions {
		if v.Status == workflow.VersionPendingReview {
			pending = append(pending, *v)
		}
	}
	total := len(pending)
	start := (page - 1) * pageSize
	if start >= total {
		return nil, total, nil
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return pending[start:end], total, nil
}

func setupAdminReviewTest() (*workflow.AdminReviewService, *mockAdminReviewStore) {
	store := newMockAdminReviewStore()

	// Add a workflow definition
	store.workflows["wf_1"] = &workflow.WorkflowDefinition{
		ID:      "wf_1",
		OwnerID: "user_1",
		Name:    "Test Workflow",
	}

	// Add a pending review version
	store.versions["ver_1"] = &workflow.WorkflowVersion{
		ID:            "ver_1",
		WorkflowID:    "wf_1",
		VersionNumber: "1.0.0",
		Status:        workflow.VersionPendingReview,
		Graph: workflow.WorkflowGraph{
			Nodes: []workflow.WorkflowNode{
				{ID: "n1", Type: workflow.NodeTrigger, Label: "Start"},
			},
		},
	}

	// Add a published version
	store.versions["ver_2"] = &workflow.WorkflowVersion{
		ID:            "ver_2",
		WorkflowID:    "wf_1",
		VersionNumber: "0.9.0",
		Status:        workflow.VersionPublished,
		Graph:         workflow.WorkflowGraph{},
	}

	svc := workflow.NewAdminReviewService(store, nil)
	return svc, store
}

func TestWorkflowAdminReviewListHandler(t *testing.T) {
	svc, _ := setupAdminReviewTest()
	handler := WorkflowAdminReviewListHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/reviews?page=1", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp workflow.PendingSubmissionsPage
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Total != 1 {
		t.Errorf("expected 1 pending submission, got %d", resp.Total)
	}
	if resp.Page != 1 {
		t.Errorf("expected page 1, got %d", resp.Page)
	}
	if len(resp.Submissions) != 1 {
		t.Fatalf("expected 1 submission, got %d", len(resp.Submissions))
	}
	if resp.Submissions[0].Version.ID != "ver_1" {
		t.Errorf("expected version ver_1, got %s", resp.Submissions[0].Version.ID)
	}
	if resp.Submissions[0].WorkflowName != "Test Workflow" {
		t.Errorf("expected workflow name 'Test Workflow', got %s", resp.Submissions[0].WorkflowName)
	}
}

func TestWorkflowAdminReviewDetailHandler(t *testing.T) {
	svc, _ := setupAdminReviewTest()
	handler := WorkflowAdminReviewDetailHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/reviews/ver_1", nil)
	req.SetPathValue("id", "ver_1")
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp workflow.SubmissionDetail
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Version.ID != "ver_1" {
		t.Errorf("expected version ver_1, got %s", resp.Version.ID)
	}
	if resp.WorkflowName != "Test Workflow" {
		t.Errorf("expected workflow name 'Test Workflow', got %s", resp.WorkflowName)
	}
	if len(resp.NodeConfigs) != 1 {
		t.Errorf("expected 1 node config, got %d", len(resp.NodeConfigs))
	}
}

func TestWorkflowAdminReviewDetailHandler_NotFound(t *testing.T) {
	svc, _ := setupAdminReviewTest()
	handler := WorkflowAdminReviewDetailHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/reviews/nonexistent", nil)
	req.SetPathValue("id", "nonexistent")
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestWorkflowAdminReviewApproveHandler(t *testing.T) {
	svc, store := setupAdminReviewTest()
	handler := WorkflowAdminReviewApproveHandler(svc)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/reviews/ver_1/approve", nil)
	req.SetPathValue("id", "ver_1")
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify the version was published
	if store.versions["ver_1"].Status != workflow.VersionPublished {
		t.Errorf("expected version status 'published', got %s", store.versions["ver_1"].Status)
	}

	// Verify the previous published version was superseded
	if store.versions["ver_2"].Status != workflow.VersionSuperseded {
		t.Errorf("expected previous version status 'superseded', got %s", store.versions["ver_2"].Status)
	}
}

func TestWorkflowAdminReviewRejectHandler(t *testing.T) {
	svc, store := setupAdminReviewTest()
	handler := WorkflowAdminReviewRejectHandler(svc)

	body, _ := json.Marshal(map[string]string{
		"reason": "The workflow has security issues that need to be addressed before publishing.",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/reviews/ver_1/reject", bytes.NewReader(body))
	req.SetPathValue("id", "ver_1")
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify the version was rejected
	if store.versions["ver_1"].Status != workflow.VersionRejected {
		t.Errorf("expected version status 'rejected', got %s", store.versions["ver_1"].Status)
	}
}

func TestWorkflowAdminReviewRejectHandler_ShortReason(t *testing.T) {
	svc, _ := setupAdminReviewTest()
	handler := WorkflowAdminReviewRejectHandler(svc)

	body, _ := json.Marshal(map[string]string{
		"reason": "too short",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/reviews/ver_1/reject", bytes.NewReader(body))
	req.SetPathValue("id", "ver_1")
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for short reason, got %d: %s", w.Code, w.Body.String())
	}
}

func TestWorkflowAdminReviewUnpublishHandler(t *testing.T) {
	svc, store := setupAdminReviewTest()
	handler := WorkflowAdminReviewUnpublishHandler(svc)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/reviews/ver_2/unpublish", nil)
	req.SetPathValue("id", "ver_2")
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify the version was unpublished
	if store.versions["ver_2"].Status != workflow.VersionUnpublished {
		t.Errorf("expected version status 'unpublished', got %s", store.versions["ver_2"].Status)
	}
}

func TestWorkflowAdminReviewUnpublishHandler_NotPublished(t *testing.T) {
	svc, _ := setupAdminReviewTest()
	handler := WorkflowAdminReviewUnpublishHandler(svc)

	// ver_1 is pending_review, not published
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/reviews/ver_1/unpublish", nil)
	req.SetPathValue("id", "ver_1")
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 for non-published version, got %d: %s", w.Code, w.Body.String())
	}
}
