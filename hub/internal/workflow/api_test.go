package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// --- In-memory store for testing ---

type memWorkflowStore struct {
	workflows map[string]*WorkflowDefinition
	versions  map[string]*WorkflowVersion
}

func newMemWorkflowStore() *memWorkflowStore {
	return &memWorkflowStore{
		workflows: make(map[string]*WorkflowDefinition),
		versions:  make(map[string]*WorkflowVersion),
	}
}

func (s *memWorkflowStore) CreateWorkflow(_ context.Context, def *WorkflowDefinition) error {
	s.workflows[def.ID] = def
	return nil
}

func (s *memWorkflowStore) GetWorkflow(_ context.Context, id string) (*WorkflowDefinition, error) {
	return s.workflows[id], nil
}

func (s *memWorkflowStore) ListWorkflows(_ context.Context, ownerID string) ([]WorkflowDefinition, error) {
	var result []WorkflowDefinition
	for _, def := range s.workflows {
		if def.OwnerID == ownerID {
			result = append(result, *def)
		}
	}
	return result, nil
}

func (s *memWorkflowStore) CreateVersion(_ context.Context, ver *WorkflowVersion) error {
	s.versions[ver.ID] = ver
	return nil
}

func (s *memWorkflowStore) UpdateVersion(_ context.Context, ver *WorkflowVersion) error {
	existing, ok := s.versions[ver.ID]
	if !ok {
		return nil
	}
	existing.Graph = ver.Graph
	existing.VersionNumber = ver.VersionNumber
	existing.UpdatedAt = ver.UpdatedAt
	return nil
}

func (s *memWorkflowStore) GetVersion(_ context.Context, id string) (*WorkflowVersion, error) {
	return s.versions[id], nil
}

func (s *memWorkflowStore) GetPublishedVersion(_ context.Context, workflowID string) (*WorkflowVersion, error) {
	for _, ver := range s.versions {
		if ver.WorkflowID == workflowID && ver.Status == VersionPublished {
			return ver, nil
		}
	}
	return nil, ErrNoPublishedVersion
}

func (s *memWorkflowStore) UpdateVersionStatus(_ context.Context, id string, status VersionStatus, reason string) error {
	ver := s.versions[id]
	if ver == nil {
		return nil
	}
	ver.Status = status
	ver.RejectionReason = reason
	return nil
}

func (s *memWorkflowStore) ListVersions(_ context.Context, workflowID string) ([]WorkflowVersion, error) {
	var result []WorkflowVersion
	for _, ver := range s.versions {
		if ver.WorkflowID == workflowID {
			result = append(result, *ver)
		}
	}
	return result, nil
}

func (s *memWorkflowStore) ListPendingReviews(_ context.Context, page, pageSize int) ([]WorkflowVersion, int, error) {
	var result []WorkflowVersion
	for _, ver := range s.versions {
		if ver.Status == VersionPendingReview {
			result = append(result, *ver)
		}
	}
	return result, len(result), nil
}

func (s *memWorkflowStore) UpdateWorkflow(_ context.Context, def *WorkflowDefinition) error {
	s.workflows[def.ID] = def
	return nil
}

func (s *memWorkflowStore) DeleteWorkflow(_ context.Context, id string) error {
	delete(s.workflows, id)
	return nil
}

// testAuthMiddleware sets X-Owner-ID from a fixed test user.
func testAuthMiddleware(ownerID string) func(http.HandlerFunc) http.HandlerFunc {
	return func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			r.Header.Set("X-Owner-ID", ownerID)
			h(w, r)
		}
	}
}

func setupTestAPI() (*WorkflowAPI, *http.ServeMux, *memWorkflowStore) {
	store := newMemWorkflowStore()
	vm := NewVersionManager(store)
	api := NewWorkflowAPI(store, vm)
	mux := http.NewServeMux()
	api.RegisterRoutes(mux, testAuthMiddleware("user-001"))
	return api, mux, store
}

func TestCreateWorkflow(t *testing.T) {
	_, mux, _ := setupTestAPI()

	body := `{"name":"My Approval Flow","description":"Test workflow"}`
	req := httptest.NewRequest("POST", "/api/v1/workflows", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var result WorkflowDefinition
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result.Name != "My Approval Flow" {
		t.Errorf("expected name 'My Approval Flow', got %q", result.Name)
	}
	if result.OwnerID != "user-001" {
		t.Errorf("expected owner_id 'user-001', got %q", result.OwnerID)
	}
	if result.ID == "" {
		t.Error("expected non-empty ID")
	}
}

func TestCreateWorkflow_EmptyName(t *testing.T) {
	_, mux, _ := setupTestAPI()

	body := `{"name":"","description":"Test"}`
	req := httptest.NewRequest("POST", "/api/v1/workflows", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListWorkflows_OwnerIsolation(t *testing.T) {
	_, mux, store := setupTestAPI()

	// Create workflows for two different owners
	store.workflows["wf-1"] = &WorkflowDefinition{ID: "wf-1", OwnerID: "user-001", Name: "My Flow"}
	store.workflows["wf-2"] = &WorkflowDefinition{ID: "wf-2", OwnerID: "user-002", Name: "Other Flow"}

	req := httptest.NewRequest("GET", "/api/v1/workflows", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result struct {
		Workflows []WorkflowDefinition `json:"workflows"`
		Total     int                  `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	// Should only see user-001's workflow
	if result.Total != 1 {
		t.Errorf("expected 1 workflow, got %d", result.Total)
	}
	if len(result.Workflows) != 1 || result.Workflows[0].ID != "wf-1" {
		t.Errorf("expected only wf-1, got %+v", result.Workflows)
	}
}

func TestGetWorkflow_OwnerIsolation(t *testing.T) {
	_, mux, store := setupTestAPI()

	store.workflows["wf-other"] = &WorkflowDefinition{ID: "wf-other", OwnerID: "user-002", Name: "Other"}

	req := httptest.NewRequest("GET", "/api/v1/workflows/wf-other", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// Should return 404 because user-001 doesn't own this workflow
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetWorkflow_Found(t *testing.T) {
	_, mux, store := setupTestAPI()

	store.workflows["wf-mine"] = &WorkflowDefinition{ID: "wf-mine", OwnerID: "user-001", Name: "Mine"}

	req := httptest.NewRequest("GET", "/api/v1/workflows/wf-mine", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result WorkflowDefinition
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if result.Name != "Mine" {
		t.Errorf("expected name 'Mine', got %q", result.Name)
	}
}

func TestUpdateWorkflow(t *testing.T) {
	_, mux, store := setupTestAPI()

	store.workflows["wf-mine"] = &WorkflowDefinition{ID: "wf-mine", OwnerID: "user-001", Name: "Old Name"}

	body := `{"name":"New Name","description":"Updated"}`
	req := httptest.NewRequest("PUT", "/api/v1/workflows/wf-mine", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result WorkflowDefinition
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if result.Name != "New Name" {
		t.Errorf("expected name 'New Name', got %q", result.Name)
	}
}

func TestDeleteWorkflow(t *testing.T) {
	_, mux, store := setupTestAPI()

	store.workflows["wf-mine"] = &WorkflowDefinition{ID: "wf-mine", OwnerID: "user-001", Name: "To Delete"}

	req := httptest.NewRequest("DELETE", "/api/v1/workflows/wf-mine", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if _, exists := store.workflows["wf-mine"]; exists {
		t.Error("expected workflow to be deleted from store")
	}
}

func TestDeleteWorkflow_RejectsPublishedVersion(t *testing.T) {
	_, mux, store := setupTestAPI()

	store.workflows["wf-mine"] = &WorkflowDefinition{ID: "wf-mine", OwnerID: "user-001", Name: "Published"}
	store.versions["ver-pub"] = &WorkflowVersion{ID: "ver-pub", WorkflowID: "wf-mine", VersionNumber: "1.0.0", Status: VersionPublished}

	req := httptest.NewRequest("DELETE", "/api/v1/workflows/wf-mine", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
	if _, exists := store.workflows["wf-mine"]; !exists {
		t.Error("published workflow should not have been deleted")
	}
}

func TestDeleteWorkflow_RejectsPreviouslyPublishedVersion(t *testing.T) {
	_, mux, store := setupTestAPI()

	for _, status := range []VersionStatus{VersionSuperseded, VersionUnpublished} {
		workflowID := "wf-" + string(status)
		store.workflows[workflowID] = &WorkflowDefinition{ID: workflowID, OwnerID: "user-001", Name: "Previously Published"}
		store.versions["ver-"+string(status)] = &WorkflowVersion{ID: "ver-" + string(status), WorkflowID: workflowID, VersionNumber: "1.0.0", Status: status}

		req := httptest.NewRequest("DELETE", "/api/v1/workflows/"+workflowID, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusConflict {
			t.Fatalf("status %s: expected 409, got %d: %s", status, w.Code, w.Body.String())
		}
		if _, exists := store.workflows[workflowID]; !exists {
			t.Fatalf("status %s: previously published workflow should not have been deleted", status)
		}
	}
}

func TestDeleteWorkflow_OwnerIsolation(t *testing.T) {
	_, mux, store := setupTestAPI()

	store.workflows["wf-other"] = &WorkflowDefinition{ID: "wf-other", OwnerID: "user-002", Name: "Other"}

	req := httptest.NewRequest("DELETE", "/api/v1/workflows/wf-other", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}

	// Should NOT be deleted
	if _, exists := store.workflows["wf-other"]; !exists {
		t.Error("workflow should not have been deleted")
	}
}

func TestCreateVersion(t *testing.T) {
	_, mux, store := setupTestAPI()

	store.workflows["wf-mine"] = &WorkflowDefinition{ID: "wf-mine", OwnerID: "user-001", Name: "Mine"}

	body := `{"graph":{"nodes":[{"id":"n1","type":"trigger","label":"Start","position":{"x":0,"y":0},"config":null}],"edges":[]}}`
	req := httptest.NewRequest("POST", "/api/v1/workflows/wf-mine/versions", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var result WorkflowVersion
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if result.Status != VersionDraft {
		t.Errorf("expected status 'draft', got %q", result.Status)
	}
	if result.VersionNumber != "0.1.0" {
		t.Errorf("expected version '0.1.0', got %q", result.VersionNumber)
	}
}

func TestCreateVersion_OwnerIsolation(t *testing.T) {
	_, mux, store := setupTestAPI()

	store.workflows["wf-other"] = &WorkflowDefinition{ID: "wf-other", OwnerID: "user-002", Name: "Other"}

	body := `{"graph":{"nodes":[],"edges":[]}}`
	req := httptest.NewRequest("POST", "/api/v1/workflows/wf-other/versions", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListVersions(t *testing.T) {
	_, mux, store := setupTestAPI()

	store.workflows["wf-mine"] = &WorkflowDefinition{ID: "wf-mine", OwnerID: "user-001", Name: "Mine"}
	store.versions["ver-1"] = &WorkflowVersion{ID: "ver-1", WorkflowID: "wf-mine", VersionNumber: "0.1.0", Status: VersionDraft}
	store.versions["ver-2"] = &WorkflowVersion{ID: "ver-2", WorkflowID: "wf-mine", VersionNumber: "0.2.0", Status: VersionPublished}

	req := httptest.NewRequest("GET", "/api/v1/workflows/wf-mine/versions", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result struct {
		Versions []WorkflowVersion `json:"versions"`
		Total    int               `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if result.Total != 2 {
		t.Errorf("expected 2 versions, got %d", result.Total)
	}
}

func TestSubmitForReview(t *testing.T) {
	_, mux, store := setupTestAPI()

	store.workflows["wf-mine"] = &WorkflowDefinition{ID: "wf-mine", OwnerID: "user-001", Name: "Mine"}
	store.versions["ver-1"] = &WorkflowVersion{
		ID:            "ver-1",
		WorkflowID:    "wf-mine",
		VersionNumber: "0.1.0",
		Status:        VersionDraft,
		Graph: WorkflowGraph{
			Nodes: []WorkflowNode{
				{ID: "n1", Type: NodeTrigger, Label: "Start"},
				{ID: "n2", Type: NodeApproval, Label: "Approve", Config: approvalConfigRaw("role:function:finance:finance_approver")},
			},
			Edges: []WorkflowEdge{
				{ID: "e1", SourceID: "n1", TargetID: "n2"},
			},
		},
	}

	req := httptest.NewRequest("POST", "/api/v1/workflows/wf-mine/versions/ver-1/submit", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify status changed
	if store.versions["ver-1"].Status != VersionPendingReview {
		t.Errorf("expected status pending_review, got %q", store.versions["ver-1"].Status)
	}
}

func TestSubmitForReview_NotDraft(t *testing.T) {
	_, mux, store := setupTestAPI()

	store.workflows["wf-mine"] = &WorkflowDefinition{ID: "wf-mine", OwnerID: "user-001", Name: "Mine"}
	store.versions["ver-1"] = &WorkflowVersion{
		ID:         "ver-1",
		WorkflowID: "wf-mine",
		Status:     VersionPublished,
	}

	req := httptest.NewRequest("POST", "/api/v1/workflows/wf-mine/versions/ver-1/submit", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSubmitForReview_ValidationError(t *testing.T) {
	_, mux, store := setupTestAPI()

	store.workflows["wf-mine"] = &WorkflowDefinition{ID: "wf-mine", OwnerID: "user-001", Name: "Mine"}
	store.versions["ver-1"] = &WorkflowVersion{
		ID:            "ver-1",
		WorkflowID:    "wf-mine",
		VersionNumber: "0.1.0",
		Status:        VersionDraft,
		Graph: WorkflowGraph{
			Nodes: []WorkflowNode{
				{ID: "n1", Type: NodeTrigger, Label: "Start"},
				{ID: "n2", Type: NodeApproval, Label: "Approve"},
			},
			Edges: []WorkflowEdge{
				{ID: "e1", SourceID: "n1", TargetID: "n2"},
				{ID: "e2", SourceID: "n2", TargetID: "n1"},
			},
		},
	}

	req := httptest.NewRequest("POST", "/api/v1/workflows/wf-mine/versions/ver-1/submit", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Code != "VALIDATION_FAILED" {
		t.Fatalf("code = %q, want VALIDATION_FAILED", body.Code)
	}
	if store.versions["ver-1"].Status != VersionDraft {
		t.Fatalf("status = %q, want draft", store.versions["ver-1"].Status)
	}
}

func TestSubmitForReview_RejectsApprovalWithoutApprover(t *testing.T) {
	_, mux, store := setupTestAPI()

	store.workflows["wf-mine"] = &WorkflowDefinition{ID: "wf-mine", OwnerID: "user-001", Name: "Mine"}
	store.versions["ver-1"] = &WorkflowVersion{
		ID:            "ver-1",
		WorkflowID:    "wf-mine",
		VersionNumber: "0.1.0",
		Status:        VersionDraft,
		Graph: WorkflowGraph{
			Nodes: []WorkflowNode{
				{ID: "n1", Type: NodeTrigger, Label: "Start"},
				{ID: "n2", Type: NodeApproval, Label: "Approve", Config: approvalConfigRaw()},
			},
			Edges: []WorkflowEdge{{ID: "e1", SourceID: "n1", TargetID: "n2"}},
		},
	}

	req := httptest.NewRequest("POST", "/api/v1/workflows/wf-mine/versions/ver-1/submit", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Code != "VALIDATION_FAILED" {
		t.Fatalf("code = %q, want VALIDATION_FAILED", body.Code)
	}
	if store.versions["ver-1"].Status != VersionDraft {
		t.Fatalf("status = %q, want draft", store.versions["ver-1"].Status)
	}
}

func TestSubmitForReview_OwnerIsolation(t *testing.T) {
	_, mux, store := setupTestAPI()

	store.workflows["wf-other"] = &WorkflowDefinition{ID: "wf-other", OwnerID: "user-002", Name: "Other"}
	store.versions["ver-1"] = &WorkflowVersion{ID: "ver-1", WorkflowID: "wf-other", Status: VersionDraft}

	req := httptest.NewRequest("POST", "/api/v1/workflows/wf-other/versions/ver-1/submit", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestNoAuth_Returns401(t *testing.T) {
	store := newMemWorkflowStore()
	vm := NewVersionManager(store)
	api := NewWorkflowAPI(store, vm)
	mux := http.NewServeMux()
	// Register with a middleware that does NOT set X-Owner-ID
	noAuth := func(h http.HandlerFunc) http.HandlerFunc {
		return h
	}
	api.RegisterRoutes(mux, noAuth)

	req := httptest.NewRequest("GET", "/api/v1/workflows", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}
