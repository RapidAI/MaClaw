package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// --- In-memory stores for instance API testing ---

type memInstanceStoreForAPI struct {
	instances map[string]*WorkflowInstance
}

func newMemInstanceStoreForAPI() *memInstanceStoreForAPI {
	return &memInstanceStoreForAPI{instances: make(map[string]*WorkflowInstance)}
}

func (s *memInstanceStoreForAPI) Create(_ context.Context, inst *WorkflowInstance) error {
	s.instances[inst.ID] = inst
	return nil
}

func (s *memInstanceStoreForAPI) Get(_ context.Context, id string) (*WorkflowInstance, error) {
	return s.instances[id], nil
}

func (s *memInstanceStoreForAPI) UpdateStatus(_ context.Context, id string, status InstanceStatus) error {
	if inst := s.instances[id]; inst != nil {
		inst.Status = status
	}
	return nil
}

func (s *memInstanceStoreForAPI) UpdateCurrentNode(_ context.Context, id, nodeID string) error {
	if inst := s.instances[id]; inst != nil {
		inst.CurrentNodeID = nodeID
	}
	return nil
}

func (s *memInstanceStoreForAPI) UpdateInstanceData(_ context.Context, id string, data map[string]interface{}) error {
	if inst := s.instances[id]; inst != nil {
		inst.InstanceData = data
	}
	return nil
}

func (s *memInstanceStoreForAPI) CreateNodeExecution(_ context.Context, _ *NodeExecution) error {
	return nil
}

func (s *memInstanceStoreForAPI) UpdateNodeExecution(_ context.Context, _ string, _ NodeStatus, _ json.RawMessage, _ string) error {
	return nil
}

func (s *memInstanceStoreForAPI) GetPendingApprovals(_ context.Context, _ string) ([]NodeExecution, error) {
	return nil, nil
}

func (s *memInstanceStoreForAPI) QueryMyInitiated(_ context.Context, _ string, _ DirectoryFilter) ([]DirectoryItem, int, error) {
	return nil, 0, nil
}

func (s *memInstanceStoreForAPI) QueryPendingMyAction(_ context.Context, _ string, _ DirectoryFilter) ([]DirectoryItem, int, error) {
	return nil, 0, nil
}

func (s *memInstanceStoreForAPI) QueryPendingMyConfirmation(_ context.Context, _ string, _ DirectoryFilter) ([]DirectoryItem, int, error) {
	return nil, 0, nil
}

func (s *memInstanceStoreForAPI) QueryCompleted(_ context.Context, _ string, _ DirectoryFilter) ([]DirectoryItem, int, error) {
	return nil, 0, nil
}

type memAuditStoreForAPI struct {
	entries []AuditEntry
}

func newMemAuditStoreForAPI() *memAuditStoreForAPI {
	return &memAuditStoreForAPI{entries: []AuditEntry{}}
}

func (s *memAuditStoreForAPI) Append(_ context.Context, entry *AuditEntry) error {
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC().Truncate(time.Millisecond)
	}
	s.entries = append(s.entries, *entry)
	return nil
}

func (s *memAuditStoreForAPI) QueryByInstance(_ context.Context, instanceID string, page, pageSize int) ([]AuditEntry, int, error) {
	if pageSize <= 0 || pageSize > DefaultAuditPageSize {
		pageSize = DefaultAuditPageSize
	}
	var matched []AuditEntry
	for _, e := range s.entries {
		if e.InstanceID == instanceID {
			matched = append(matched, e)
		}
	}
	total := len(matched)
	start := (page - 1) * pageSize
	if start >= total {
		return []AuditEntry{}, total, nil
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return matched[start:end], total, nil
}

func (s *memAuditStoreForAPI) QueryByApprover(_ context.Context, _ string, _, _ int) ([]AuditEntry, int, error) {
	return nil, 0, nil
}

func (s *memAuditStoreForAPI) QueryByTimeRange(_ context.Context, _, _ time.Time, _, _ int) ([]AuditEntry, int, error) {
	return nil, 0, nil
}

func (s *memAuditStoreForAPI) QueryByDecision(_ context.Context, _ string, _, _ int) ([]AuditEntry, int, error) {
	return nil, 0, nil
}

// noopDispatcher is a no-op implementation of ApprovalDispatcher for testing.
type noopDispatcher struct{}

func (d *noopDispatcher) Dispatch(_ context.Context, _ *ApprovalRequest, _ string) error {
	return nil
}

func (d *noopDispatcher) DispatchFallback(_ context.Context, _ *ApprovalRequest, _ string, _ string) error {
	return nil
}

// --- Helper to set up test infrastructure ---

func setupInstanceAPITest(t *testing.T) (*InstanceAPI, *memWorkflowStore, *memInstanceStoreForAPI, *memAuditStoreForAPI) {
	t.Helper()
	wfStore := newMemWorkflowStore()
	instStore := newMemInstanceStoreForAPI()
	auditStore := newMemAuditStoreForAPI()
	dispatcher := &noopDispatcher{}
	executor := NewWorkflowExecutor(wfStore, instStore, auditStore, dispatcher)
	api := NewInstanceAPI(executor, instStore, auditStore)
	return api, wfStore, instStore, auditStore
}

func noAuthMiddleware(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Set a default owner ID for testing.
		if r.Header.Get("X-Owner-ID") == "" {
			r.Header.Set("X-Owner-ID", "test-user")
		}
		h(w, r)
	}
}

// --- Tests ---

func TestInstanceAPI_TriggerWorkflow_Success(t *testing.T) {
	api, wfStore, _, _ := setupInstanceAPITest(t)

	// Create a workflow with a published version that has a valid trigger node.
	wfStore.workflows["wf1"] = &WorkflowDefinition{
		ID:      "wf1",
		OwnerID: "author1",
		Name:    "Test Workflow",
	}
	wfStore.versions["ver1"] = &WorkflowVersion{
		ID:            "ver1",
		WorkflowID:    "wf1",
		VersionNumber: "1.0.0",
		Status:        VersionPublished,
		Graph: WorkflowGraph{
			Nodes: []WorkflowNode{
				{ID: "n1", Type: NodeTrigger, Label: "Start"},
			},
			Edges: []WorkflowEdge{},
		},
	}

	mux := http.NewServeMux()
	api.RegisterRoutes(mux, noAuthMiddleware)

	body := `{"trigger_data": {"key": "value"}}`
	req := httptest.NewRequest("POST", "/api/v1/workflows/wf1/trigger", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	instance, ok := resp["instance"].(map[string]any)
	if !ok {
		t.Fatal("response missing 'instance' field")
	}
	if instance["workflow_id"] != "wf1" {
		t.Errorf("expected workflow_id=wf1, got %v", instance["workflow_id"])
	}
	// The workflow has only a trigger node with no outgoing edges,
	// so the executor completes the instance immediately after starting.
	status := instance["status"].(string)
	if status != "running" && status != "completed" {
		t.Errorf("expected status=running or completed, got %v", status)
	}
}

func TestInstanceAPI_TriggerWorkflow_NotPublished(t *testing.T) {
	api, wfStore, _, _ := setupInstanceAPITest(t)

	// Create a workflow with only a draft version (no published version).
	wfStore.workflows["wf1"] = &WorkflowDefinition{
		ID:      "wf1",
		OwnerID: "author1",
		Name:    "Draft Workflow",
	}
	wfStore.versions["ver1"] = &WorkflowVersion{
		ID:            "ver1",
		WorkflowID:    "wf1",
		VersionNumber: "0.1.0",
		Status:        VersionDraft,
	}

	mux := http.NewServeMux()
	api.RegisterRoutes(mux, noAuthMiddleware)

	req := httptest.NewRequest("POST", "/api/v1/workflows/wf1/trigger", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	// The store returns ErrNoPublishedVersion which is wrapped by TriggerFromMarket.
	// The handler uses errors.Is to detect it and returns 409.
	if w.Code != http.StatusConflict {
		t.Fatalf("expected status 409, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp["code"] != "NOT_PUBLISHED" {
		t.Errorf("expected code=NOT_PUBLISHED, got %v", resp["code"])
	}
}

func TestInstanceAPI_TriggerWorkflow_Unauthorized(t *testing.T) {
	api, _, _, _ := setupInstanceAPITest(t)

	mux := http.NewServeMux()
	// Use a middleware that does NOT set X-Owner-ID.
	api.RegisterRoutes(mux, func(h http.HandlerFunc) http.HandlerFunc { return h })

	req := httptest.NewRequest("POST", "/api/v1/workflows/wf1/trigger", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestInstanceAPI_GetInstance_Success(t *testing.T) {
	api, _, instStore, _ := setupInstanceAPITest(t)

	now := time.Now().UTC()
	instStore.instances["inst1"] = &WorkflowInstance{
		ID:            "inst1",
		WorkflowID:    "wf1",
		VersionID:     "ver1",
		Status:        InstanceRunning,
		CurrentNodeID: "node2",
		InstanceData:  map[string]interface{}{"key": "value"},
		TriggerData:   `{"requester_id":"user1"}`,
		CreatedAt:     now,
	}

	mux := http.NewServeMux()
	api.RegisterRoutes(mux, noAuthMiddleware)

	req := httptest.NewRequest("GET", "/api/v1/instances/inst1", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp WorkflowInstance
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp.ID != "inst1" {
		t.Errorf("expected id=inst1, got %s", resp.ID)
	}
	if resp.Status != InstanceRunning {
		t.Errorf("expected status=running, got %s", resp.Status)
	}
	if resp.CurrentNodeID != "node2" {
		t.Errorf("expected current_node_id=node2, got %s", resp.CurrentNodeID)
	}
}

func TestInstanceAPI_GetInstance_NotFound(t *testing.T) {
	api, _, _, _ := setupInstanceAPITest(t)

	mux := http.NewServeMux()
	api.RegisterRoutes(mux, noAuthMiddleware)

	req := httptest.NewRequest("GET", "/api/v1/instances/nonexistent", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestInstanceAPI_GetInstanceAudit_Success(t *testing.T) {
	api, _, instStore, auditStore := setupInstanceAPITest(t)

	// Seed the instance so authorization check passes.
	instStore.instances["inst1"] = &WorkflowInstance{
		ID: "inst1", Status: InstanceRunning,
		InstanceData: map[string]interface{}{},
	}

	// Seed audit entries for instance "inst1".
	for i := 0; i < 5; i++ {
		_ = auditStore.Append(context.Background(), &AuditEntry{
			ID:         generateID("audit"),
			InstanceID: "inst1",
			EventType:  "node_completed",
			Timestamp:  time.Now().UTC().Truncate(time.Millisecond),
		})
	}
	// Add an entry for a different instance (should not appear).
	_ = auditStore.Append(context.Background(), &AuditEntry{
		ID:         generateID("audit"),
		InstanceID: "inst2",
		EventType:  "instance_created",
		Timestamp:  time.Now().UTC().Truncate(time.Millisecond),
	})

	mux := http.NewServeMux()
	api.RegisterRoutes(mux, noAuthMiddleware)

	req := httptest.NewRequest("GET", "/api/v1/instances/inst1/audit", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	total := int(resp["total"].(float64))
	if total != 5 {
		t.Errorf("expected total=5, got %d", total)
	}

	entries := resp["entries"].([]any)
	if len(entries) != 5 {
		t.Errorf("expected 5 entries, got %d", len(entries))
	}

	pageSize := int(resp["page_size"].(float64))
	if pageSize != DefaultAuditPageSize {
		t.Errorf("expected page_size=%d, got %d", DefaultAuditPageSize, pageSize)
	}
}

func TestInstanceAPI_GetInstanceAudit_Pagination(t *testing.T) {
	api, _, instStore, auditStore := setupInstanceAPITest(t)

	// Seed the instance so authorization check passes.
	instStore.instances["inst1"] = &WorkflowInstance{
		ID: "inst1", Status: InstanceRunning,
		InstanceData: map[string]interface{}{},
	}

	// Seed 150 audit entries (more than one page).
	for i := 0; i < 150; i++ {
		_ = auditStore.Append(context.Background(), &AuditEntry{
			ID:         generateID("audit"),
			InstanceID: "inst1",
			EventType:  "node_completed",
			Timestamp:  time.Now().UTC().Truncate(time.Millisecond),
		})
	}

	mux := http.NewServeMux()
	api.RegisterRoutes(mux, noAuthMiddleware)

	// Request page 2.
	req := httptest.NewRequest("GET", "/api/v1/instances/inst1/audit?page=2&page_size=100", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	total := int(resp["total"].(float64))
	if total != 150 {
		t.Errorf("expected total=150, got %d", total)
	}

	entries := resp["entries"].([]any)
	if len(entries) != 50 {
		t.Errorf("expected 50 entries on page 2, got %d", len(entries))
	}

	page := int(resp["page"].(float64))
	if page != 2 {
		t.Errorf("expected page=2, got %d", page)
	}
}

func TestInstanceAPI_GetInstanceAudit_PageSizeCapped(t *testing.T) {
	api, _, instStore, auditStore := setupInstanceAPITest(t)

	// Seed the instance so authorization check passes.
	instStore.instances["inst1"] = &WorkflowInstance{
		ID: "inst1", Status: InstanceRunning,
		InstanceData: map[string]interface{}{},
	}

	// Seed 200 audit entries.
	for i := 0; i < 200; i++ {
		_ = auditStore.Append(context.Background(), &AuditEntry{
			ID:         generateID("audit"),
			InstanceID: "inst1",
			EventType:  "node_completed",
			Timestamp:  time.Now().UTC().Truncate(time.Millisecond),
		})
	}

	mux := http.NewServeMux()
	api.RegisterRoutes(mux, noAuthMiddleware)

	// Request with page_size=500 (should be capped to 100).
	req := httptest.NewRequest("GET", "/api/v1/instances/inst1/audit?page_size=500", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	pageSize := int(resp["page_size"].(float64))
	if pageSize != DefaultAuditPageSize {
		t.Errorf("expected page_size capped to %d, got %d", DefaultAuditPageSize, pageSize)
	}

	entries := resp["entries"].([]any)
	if len(entries) != 100 {
		t.Errorf("expected 100 entries (capped), got %d", len(entries))
	}
}

func TestInstanceAPI_GetInstanceAudit_EmptyResult(t *testing.T) {
	api, _, _, _ := setupInstanceAPITest(t)

	mux := http.NewServeMux()
	api.RegisterRoutes(mux, noAuthMiddleware)

	req := httptest.NewRequest("GET", "/api/v1/instances/nonexistent/audit", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	// Instance not found returns 404 due to authorization check.
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d: %s", w.Code, w.Body.String())
	}
}
