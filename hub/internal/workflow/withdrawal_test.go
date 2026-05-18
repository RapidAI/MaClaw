package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// --- Mock InstanceStore for withdrawal tests ---

type withdrawalMockInstanceStore struct {
	instance     *WorkflowInstance
	getErr       error
	updateErr    error
	pendingExecs []NodeExecution
	updatedNodes []nodeExecUpdateRecord
}

type nodeExecUpdateRecord struct {
	ID         string
	Status     NodeStatus
	FailReason string
}

func (s *withdrawalMockInstanceStore) Create(_ context.Context, _ *WorkflowInstance) error {
	return nil
}
func (s *withdrawalMockInstanceStore) Get(_ context.Context, id string) (*WorkflowInstance, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	if s.instance != nil && s.instance.ID == id {
		return s.instance, nil
	}
	return nil, nil
}
func (s *withdrawalMockInstanceStore) UpdateStatus(_ context.Context, _ string, status InstanceStatus) error {
	if s.updateErr != nil {
		return s.updateErr
	}
	if s.instance != nil {
		s.instance.Status = status
	}
	return nil
}
func (s *withdrawalMockInstanceStore) UpdateCurrentNode(_ context.Context, _, _ string) error {
	return nil
}
func (s *withdrawalMockInstanceStore) UpdateInstanceData(_ context.Context, _ string, _ map[string]interface{}) error {
	return nil
}
func (s *withdrawalMockInstanceStore) CreateNodeExecution(_ context.Context, _ *NodeExecution) error {
	return nil
}
func (s *withdrawalMockInstanceStore) UpdateNodeExecution(_ context.Context, id string, status NodeStatus, _ json.RawMessage, failReason string) error {
	s.updatedNodes = append(s.updatedNodes, nodeExecUpdateRecord{ID: id, Status: status, FailReason: failReason})
	return nil
}
func (s *withdrawalMockInstanceStore) GetPendingApprovals(_ context.Context, _ string) ([]NodeExecution, error) {
	return s.pendingExecs, nil
}
func (s *withdrawalMockInstanceStore) QueryMyInitiated(_ context.Context, _ string, _ DirectoryFilter) ([]DirectoryItem, int, error) {
	return nil, 0, nil
}
func (s *withdrawalMockInstanceStore) QueryPendingMyAction(_ context.Context, _ string, _ DirectoryFilter) ([]DirectoryItem, int, error) {
	return nil, 0, nil
}
func (s *withdrawalMockInstanceStore) QueryPendingMyConfirmation(_ context.Context, _ string, _ DirectoryFilter) ([]DirectoryItem, int, error) {
	return nil, 0, nil
}
func (s *withdrawalMockInstanceStore) QueryCompleted(_ context.Context, _ string, _ DirectoryFilter) ([]DirectoryItem, int, error) {
	return nil, 0, nil
}

// --- Mock AuditStore for withdrawal tests ---

type withdrawalMockAuditStore struct {
	entries []AuditEntry
}

func (s *withdrawalMockAuditStore) Append(_ context.Context, entry *AuditEntry) error {
	s.entries = append(s.entries, *entry)
	return nil
}
func (s *withdrawalMockAuditStore) QueryByInstance(_ context.Context, _ string, _, _ int) ([]AuditEntry, int, error) {
	return nil, 0, nil
}
func (s *withdrawalMockAuditStore) QueryByApprover(_ context.Context, _ string, _, _ int) ([]AuditEntry, int, error) {
	return nil, 0, nil
}
func (s *withdrawalMockAuditStore) QueryByTimeRange(_ context.Context, _, _ time.Time, _, _ int) ([]AuditEntry, int, error) {
	return nil, 0, nil
}
func (s *withdrawalMockAuditStore) QueryByDecision(_ context.Context, _ string, _, _ int) ([]AuditEntry, int, error) {
	return nil, 0, nil
}

// --- Tests for Withdraw method ---

func TestWithdraw_Success_RunningInstance(t *testing.T) {
	instStore := &withdrawalMockInstanceStore{
		instance: &WorkflowInstance{
			ID:     "inst-001",
			Status: InstanceRunning,
			InstanceData: map[string]interface{}{
				"initiator_id":   "user-alice",
				"workflow_name":  "请假审批",
				"initiator_name": "Alice",
			},
		},
		pendingExecs: []NodeExecution{
			{ID: "exec-1", InstanceID: "inst-001", NodeID: "approval-1", Status: NodePending},
			{ID: "exec-2", InstanceID: "inst-001", NodeID: "approval-2", Status: NodePending},
			{ID: "exec-3", InstanceID: "other-inst", NodeID: "approval-3", Status: NodePending}, // different instance
		},
	}
	auditStore := &withdrawalMockAuditStore{}

	wh := NewWithdrawalHandler(instStore, auditStore, nil, nil)
	err := wh.Withdraw(context.Background(), "inst-001", "user-alice")
	if err != nil {
		t.Fatalf("Withdraw() unexpected error: %v", err)
	}

	// Verify instance status was updated.
	if instStore.instance.Status != InstanceWithdrawn {
		t.Errorf("instance status = %q, want %q", instStore.instance.Status, InstanceWithdrawn)
	}

	// Verify pending nodes for this instance were skipped.
	if len(instStore.updatedNodes) != 2 {
		t.Fatalf("expected 2 node updates, got %d", len(instStore.updatedNodes))
	}
	for _, update := range instStore.updatedNodes {
		if update.Status != NodeSkipped {
			t.Errorf("node %s status = %q, want %q", update.ID, update.Status, NodeSkipped)
		}
		if update.FailReason != "withdrawn by initiator" {
			t.Errorf("node %s fail_reason = %q, want %q", update.ID, update.FailReason, "withdrawn by initiator")
		}
	}

	// Verify audit trail has withdrawal event.
	found := false
	for _, entry := range auditStore.entries {
		if entry.EventType == "withdrawal" && entry.InstanceID == "inst-001" && entry.ActorID == "user-alice" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'withdrawal' audit entry not found")
	}
}

func TestWithdraw_ErrNotInitiator(t *testing.T) {
	instStore := &withdrawalMockInstanceStore{
		instance: &WorkflowInstance{
			ID:     "inst-001",
			Status: InstanceRunning,
			InstanceData: map[string]interface{}{
				"initiator_id": "user-alice",
			},
		},
	}

	wh := NewWithdrawalHandler(instStore, nil, nil, nil)
	err := wh.Withdraw(context.Background(), "inst-001", "user-bob")
	if !errors.Is(err, ErrNotInitiator) {
		t.Errorf("Withdraw() error = %v, want ErrNotInitiator", err)
	}
}

func TestWithdraw_ErrAlreadyCompleted(t *testing.T) {
	instStore := &withdrawalMockInstanceStore{
		instance: &WorkflowInstance{
			ID:     "inst-001",
			Status: InstanceCompleted,
			InstanceData: map[string]interface{}{
				"initiator_id": "user-alice",
			},
		},
	}

	wh := NewWithdrawalHandler(instStore, nil, nil, nil)
	err := wh.Withdraw(context.Background(), "inst-001", "user-alice")
	if !errors.Is(err, ErrAlreadyCompleted) {
		t.Errorf("Withdraw() error = %v, want ErrAlreadyCompleted", err)
	}
}

func TestWithdraw_ErrAlreadyWithdrawn(t *testing.T) {
	instStore := &withdrawalMockInstanceStore{
		instance: &WorkflowInstance{
			ID:     "inst-001",
			Status: InstanceWithdrawn,
			InstanceData: map[string]interface{}{
				"initiator_id": "user-alice",
			},
		},
	}

	wh := NewWithdrawalHandler(instStore, nil, nil, nil)
	err := wh.Withdraw(context.Background(), "inst-001", "user-alice")
	if !errors.Is(err, ErrAlreadyWithdrawn) {
		t.Errorf("Withdraw() error = %v, want ErrAlreadyWithdrawn", err)
	}
}

func TestWithdraw_ErrInstanceNotFound(t *testing.T) {
	instStore := &withdrawalMockInstanceStore{
		instance: nil, // no instance
	}

	wh := NewWithdrawalHandler(instStore, nil, nil, nil)
	err := wh.Withdraw(context.Background(), "nonexistent", "user-alice")
	if !errors.Is(err, ErrInstanceNotFound) {
		t.Errorf("Withdraw() error = %v, want ErrInstanceNotFound", err)
	}
}

func TestWithdraw_ErrInstanceNotRunning(t *testing.T) {
	instStore := &withdrawalMockInstanceStore{
		instance: &WorkflowInstance{
			ID:     "inst-001",
			Status: InstanceBlocked,
			InstanceData: map[string]interface{}{
				"initiator_id": "user-alice",
			},
		},
	}

	wh := NewWithdrawalHandler(instStore, nil, nil, nil)
	err := wh.Withdraw(context.Background(), "inst-001", "user-alice")
	if !errors.Is(err, ErrInstanceNotRunning) {
		t.Errorf("Withdraw() error = %v, want ErrInstanceNotRunning", err)
	}
}

// --- Tests for handleWithdrawInstance API handler ---

func TestHandleWithdrawInstance_Success(t *testing.T) {
	instStore := &withdrawalMockInstanceStore{
		instance: &WorkflowInstance{
			ID:     "inst-001",
			Status: InstanceRunning,
			InstanceData: map[string]interface{}{
				"initiator_id": "user-alice",
			},
		},
	}
	wh := NewWithdrawalHandler(instStore, &withdrawalMockAuditStore{}, nil, nil)

	api := NewRuntimeAPI(nil, instStore, nil, nil, nil)
	api.SetWithdrawalHandler(wh)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances/inst-001/withdraw", nil)
	req.SetPathValue("id", "inst-001")
	req = req.WithContext(context.WithValue(req.Context(), userIDContextKey, "user-alice"))

	w := httptest.NewRecorder()
	api.handleWithdrawInstance(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body = %s", w.Code, http.StatusOK, w.Body.String())
	}
}

func TestHandleWithdrawInstance_Forbidden(t *testing.T) {
	instStore := &withdrawalMockInstanceStore{
		instance: &WorkflowInstance{
			ID:     "inst-001",
			Status: InstanceRunning,
			InstanceData: map[string]interface{}{
				"initiator_id": "user-alice",
			},
		},
	}
	wh := NewWithdrawalHandler(instStore, nil, nil, nil)

	api := NewRuntimeAPI(nil, instStore, nil, nil, nil)
	api.SetWithdrawalHandler(wh)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances/inst-001/withdraw", nil)
	req.SetPathValue("id", "inst-001")
	req = req.WithContext(context.WithValue(req.Context(), userIDContextKey, "user-bob"))

	w := httptest.NewRecorder()
	api.handleWithdrawInstance(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d; body = %s", w.Code, http.StatusForbidden, w.Body.String())
	}
}

func TestHandleWithdrawInstance_Conflict_AlreadyCompleted(t *testing.T) {
	instStore := &withdrawalMockInstanceStore{
		instance: &WorkflowInstance{
			ID:     "inst-001",
			Status: InstanceCompleted,
			InstanceData: map[string]interface{}{
				"initiator_id": "user-alice",
			},
		},
	}
	wh := NewWithdrawalHandler(instStore, nil, nil, nil)

	api := NewRuntimeAPI(nil, instStore, nil, nil, nil)
	api.SetWithdrawalHandler(wh)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances/inst-001/withdraw", nil)
	req.SetPathValue("id", "inst-001")
	req = req.WithContext(context.WithValue(req.Context(), userIDContextKey, "user-alice"))

	w := httptest.NewRecorder()
	api.handleWithdrawInstance(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d; body = %s", w.Code, http.StatusConflict, w.Body.String())
	}
}

func TestHandleWithdrawInstance_Conflict_AlreadyWithdrawn(t *testing.T) {
	instStore := &withdrawalMockInstanceStore{
		instance: &WorkflowInstance{
			ID:     "inst-001",
			Status: InstanceWithdrawn,
			InstanceData: map[string]interface{}{
				"initiator_id": "user-alice",
			},
		},
	}
	wh := NewWithdrawalHandler(instStore, nil, nil, nil)

	api := NewRuntimeAPI(nil, instStore, nil, nil, nil)
	api.SetWithdrawalHandler(wh)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances/inst-001/withdraw", nil)
	req.SetPathValue("id", "inst-001")
	req = req.WithContext(context.WithValue(req.Context(), userIDContextKey, "user-alice"))

	w := httptest.NewRecorder()
	api.handleWithdrawInstance(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d; body = %s", w.Code, http.StatusConflict, w.Body.String())
	}
}

func TestHandleWithdrawInstance_NotFound(t *testing.T) {
	instStore := &withdrawalMockInstanceStore{
		instance: nil,
	}
	wh := NewWithdrawalHandler(instStore, nil, nil, nil)

	api := NewRuntimeAPI(nil, instStore, nil, nil, nil)
	api.SetWithdrawalHandler(wh)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances/nonexistent/withdraw", nil)
	req.SetPathValue("id", "nonexistent")
	req = req.WithContext(context.WithValue(req.Context(), userIDContextKey, "user-alice"))

	w := httptest.NewRecorder()
	api.handleWithdrawInstance(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d; body = %s", w.Code, http.StatusNotFound, w.Body.String())
	}
}
