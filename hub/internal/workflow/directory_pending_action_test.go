package workflow

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// mockNodeExecStore implements NodeExecutionStore for testing.
type mockNodeExecStore struct {
	pendingApprovals []NodeExecution
	err              error
}

func (m *mockNodeExecStore) GetPendingApprovalsForUser(ctx context.Context, userID string) ([]NodeExecution, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.pendingApprovals, nil
}

// mockDirectoryInstanceStore implements InstanceStore for testing PendingMyAction.
// Only Get is used by PendingMyAction.
type mockDirectoryInstanceStore struct {
	instances map[string]*WorkflowInstance
}

func (m *mockDirectoryInstanceStore) Get(ctx context.Context, id string) (*WorkflowInstance, error) {
	if inst, ok := m.instances[id]; ok {
		return inst, nil
	}
	return nil, nil
}

// Stub methods to satisfy InstanceStore interface.
func (m *mockDirectoryInstanceStore) Create(ctx context.Context, inst *WorkflowInstance) error {
	return nil
}
func (m *mockDirectoryInstanceStore) UpdateStatus(ctx context.Context, id string, status InstanceStatus) error {
	return nil
}
func (m *mockDirectoryInstanceStore) UpdateCurrentNode(ctx context.Context, id, nodeID string) error {
	return nil
}
func (m *mockDirectoryInstanceStore) UpdateInstanceData(ctx context.Context, id string, data map[string]interface{}) error {
	return nil
}
func (m *mockDirectoryInstanceStore) CreateNodeExecution(ctx context.Context, exec *NodeExecution) error {
	return nil
}
func (m *mockDirectoryInstanceStore) UpdateNodeExecution(ctx context.Context, id string, status NodeStatus, result json.RawMessage, failReason string) error {
	return nil
}
func (m *mockDirectoryInstanceStore) GetPendingApprovals(ctx context.Context, approverID string) ([]NodeExecution, error) {
	return nil, nil
}
func (m *mockDirectoryInstanceStore) QueryMyInitiated(ctx context.Context, userID string, filter DirectoryFilter) ([]DirectoryItem, int, error) {
	return nil, 0, nil
}
func (m *mockDirectoryInstanceStore) QueryPendingMyAction(ctx context.Context, userID string, filter DirectoryFilter) ([]DirectoryItem, int, error) {
	return nil, 0, nil
}
func (m *mockDirectoryInstanceStore) QueryPendingMyConfirmation(ctx context.Context, userID string, filter DirectoryFilter) ([]DirectoryItem, int, error) {
	return nil, 0, nil
}
func (m *mockDirectoryInstanceStore) QueryCompleted(ctx context.Context, userID string, filter DirectoryFilter) ([]DirectoryItem, int, error) {
	return nil, 0, nil
}

// mockConfirmStore implements ConfirmationStore for testing (not used by PendingMyAction).
type mockConfirmStore struct{}

func (m *mockConfirmStore) Create(ctx context.Context, conf *Confirmation) error { return nil }
func (m *mockConfirmStore) Get(ctx context.Context, id string) (*Confirmation, error) {
	return nil, nil
}
func (m *mockConfirmStore) UpdateStatus(ctx context.Context, id string, status ConfirmationStatus, notes string) error {
	return nil
}
func (m *mockConfirmStore) IncrementReminders(ctx context.Context, id string) error { return nil }
func (m *mockConfirmStore) ListPending(ctx context.Context, recipientID string) ([]Confirmation, error) {
	return nil, nil
}
func (m *mockConfirmStore) ListByInstance(ctx context.Context, instanceID string) ([]Confirmation, error) {
	return nil, nil
}
func (m *mockConfirmStore) FindOverdue(ctx context.Context) ([]Confirmation, error) { return nil, nil }

func TestCalculateUrgency(t *testing.T) {
	now := time.Date(2025, 5, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name         string
		startedAt    time.Time
		timeoutHours int
		want         string
	}{
		{
			name:         "no timeout configured",
			startedAt:    now.Add(-100 * time.Hour),
			timeoutHours: 0,
			want:         UrgencyNormal,
		},
		{
			name:         "well within timeout (50% remaining)",
			startedAt:    now.Add(-12 * time.Hour),
			timeoutHours: 24,
			want:         UrgencyNormal,
		},
		{
			name:         "approaching timeout (20% remaining)",
			startedAt:    now.Add(-20 * time.Hour),
			timeoutHours: 24, // 4h remaining, threshold is 6h (25%)
			want:         UrgencyApproachingTimeout,
		},
		{
			name:         "exactly at 25% threshold",
			startedAt:    now.Add(-18 * time.Hour),
			timeoutHours: 24, // 6h remaining = 25% of 24h
			want:         UrgencyApproachingTimeout,
		},
		{
			name:         "just above 25% threshold",
			startedAt:    now.Add(-17*time.Hour - 59*time.Minute),
			timeoutHours: 24, // 6h1m remaining > 6h threshold
			want:         UrgencyNormal,
		},
		{
			name:         "overdue (past deadline)",
			startedAt:    now.Add(-25 * time.Hour),
			timeoutHours: 24,
			want:         UrgencyOverdue,
		},
		{
			name:         "exactly at deadline",
			startedAt:    now.Add(-24 * time.Hour),
			timeoutHours: 24,
			want:         UrgencyOverdue,
		},
		{
			name:         "large timeout normal",
			startedAt:    now.Add(-100 * time.Hour),
			timeoutHours: 720, // 30 days, 620h remaining
			want:         UrgencyNormal,
		},
		{
			name:         "large timeout approaching",
			startedAt:    now.Add(-600 * time.Hour),
			timeoutHours: 720, // 120h remaining, threshold is 180h (25%)
			want:         UrgencyApproachingTimeout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateUrgency(tt.startedAt, tt.timeoutHours, now)
			if got != tt.want {
				t.Errorf("calculateUrgency() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPendingMyAction_EmptyResult(t *testing.T) {
	ds := NewDirectoryService(
		&mockDirectoryInstanceStore{instances: map[string]*WorkflowInstance{}},
		&mockConfirmStore{},
		&mockNodeExecStore{pendingApprovals: nil},
	)

	items, err := ds.PendingMyAction(context.Background(), "user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected 0 items, got %d", len(items))
	}
}

func TestPendingMyAction_SortByUrgencyThenDate(t *testing.T) {
	now := time.Now().UTC()

	// Create instances with different creation times.
	instances := map[string]*WorkflowInstance{
		"inst-1": {
			ID: "inst-1", Status: InstanceRunning,
			CreatedAt:    now.Add(-72 * time.Hour), // oldest
			InstanceData: map[string]interface{}{"workflow_name": "Leave Request", "initiator_name": "Alice"},
		},
		"inst-2": {
			ID: "inst-2", Status: InstanceRunning,
			CreatedAt:    now.Add(-48 * time.Hour), // middle
			InstanceData: map[string]interface{}{"workflow_name": "Purchase Order", "initiator_name": "Bob"},
		},
		"inst-3": {
			ID: "inst-3", Status: InstanceRunning,
			CreatedAt:    now.Add(-24 * time.Hour), // newest
			InstanceData: map[string]interface{}{"workflow_name": "Travel Request", "initiator_name": "Charlie"},
		},
	}

	// Create node executions with different timeouts to produce different urgencies.
	// inst-1: started 72h ago, timeout 48h → overdue
	// inst-2: started 48h ago, timeout 72h → approaching (24h remaining, threshold=18h)
	// inst-3: started 24h ago, timeout 72h → normal (48h remaining, threshold=18h)
	pendingExecs := []NodeExecution{
		{
			ID: "exec-3", InstanceID: "inst-3", NodeID: "approval-3",
			Status: NodeRunning, StartedAt: now.Add(-24 * time.Hour),
			Result: json.RawMessage(`{"timeout_hours": 72}`),
		},
		{
			ID: "exec-1", InstanceID: "inst-1", NodeID: "approval-1",
			Status: NodeRunning, StartedAt: now.Add(-72 * time.Hour),
			Result: json.RawMessage(`{"timeout_hours": 48}`),
		},
		{
			ID: "exec-2", InstanceID: "inst-2", NodeID: "approval-2",
			Status: NodeRunning, StartedAt: now.Add(-48 * time.Hour),
			Result: json.RawMessage(`{"timeout_hours": 54}`), // 6h remaining, threshold=13.5h → approaching
		},
	}

	ds := NewDirectoryService(
		&mockDirectoryInstanceStore{instances: instances},
		&mockConfirmStore{},
		&mockNodeExecStore{pendingApprovals: pendingExecs},
	)

	items, err := ds.PendingMyAction(context.Background(), "user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}

	// Expected order: overdue (inst-1) → approaching (inst-2) → normal (inst-3)
	if items[0].InstanceID != "inst-1" {
		t.Errorf("expected first item to be inst-1 (overdue), got %s with urgency %s", items[0].InstanceID, items[0].Urgency)
	}
	if items[0].Urgency != UrgencyOverdue {
		t.Errorf("expected first item urgency=overdue, got %s", items[0].Urgency)
	}

	if items[1].InstanceID != "inst-2" {
		t.Errorf("expected second item to be inst-2 (approaching), got %s with urgency %s", items[1].InstanceID, items[1].Urgency)
	}
	if items[1].Urgency != UrgencyApproachingTimeout {
		t.Errorf("expected second item urgency=approaching_timeout, got %s", items[1].Urgency)
	}

	if items[2].InstanceID != "inst-3" {
		t.Errorf("expected third item to be inst-3 (normal), got %s with urgency %s", items[2].InstanceID, items[2].Urgency)
	}
	if items[2].Urgency != UrgencyNormal {
		t.Errorf("expected third item urgency=normal, got %s", items[2].Urgency)
	}
}

func TestPendingMyAction_FiltersByResolvedApproverIDs(t *testing.T) {
	now := time.Now().UTC()

	instances := map[string]*WorkflowInstance{
		"inst-mine": {
			ID:           "inst-mine",
			Status:       InstanceRunning,
			CreatedAt:    now.Add(-2 * time.Hour),
			InstanceData: map[string]interface{}{"workflow_name": "Mine"},
		},
		"inst-other": {
			ID:           "inst-other",
			Status:       InstanceRunning,
			CreatedAt:    now.Add(-1 * time.Hour),
			InstanceData: map[string]interface{}{"workflow_name": "Other"},
		},
	}

	pendingExecs := []NodeExecution{
		{
			ID:         "exec-mine",
			InstanceID: "inst-mine",
			NodeID:     "approval-mine",
			Status:     NodeRunning,
			StartedAt:  now.Add(-2 * time.Hour),
			Result:     json.RawMessage(`{"timeout_hours":24,"approver_ids":["machine-me"],"original_approvers":["role:function:finance:finance_approver"]}`),
		},
		{
			ID:         "exec-other",
			InstanceID: "inst-other",
			NodeID:     "approval-other",
			Status:     NodeRunning,
			StartedAt:  now.Add(-1 * time.Hour),
			Result:     json.RawMessage(`{"timeout_hours":24,"approver_ids":["machine-other"],"original_approvers":["role:function:finance:finance_approver"]}`),
		},
	}

	ds := NewDirectoryService(
		&mockDirectoryInstanceStore{instances: instances},
		&mockConfirmStore{},
		&mockNodeExecStore{pendingApprovals: pendingExecs},
	)

	items, err := ds.PendingMyAction(context.Background(), "machine-me")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d: %#v", len(items), items)
	}
	if items[0].InstanceID != "inst-mine" {
		t.Fatalf("expected only inst-mine, got %#v", items)
	}
}

func TestPendingMyAction_SameUrgencySortByDateAscending(t *testing.T) {
	now := time.Now().UTC()

	// All instances are normal urgency but with different creation dates.
	instances := map[string]*WorkflowInstance{
		"inst-new": {
			ID: "inst-new", Status: InstanceRunning,
			CreatedAt:    now.Add(-1 * time.Hour),
			InstanceData: map[string]interface{}{"workflow_name": "New"},
		},
		"inst-old": {
			ID: "inst-old", Status: InstanceRunning,
			CreatedAt:    now.Add(-10 * time.Hour),
			InstanceData: map[string]interface{}{"workflow_name": "Old"},
		},
		"inst-mid": {
			ID: "inst-mid", Status: InstanceRunning,
			CreatedAt:    now.Add(-5 * time.Hour),
			InstanceData: map[string]interface{}{"workflow_name": "Mid"},
		},
	}

	// All have plenty of time remaining (normal urgency).
	pendingExecs := []NodeExecution{
		{ID: "e1", InstanceID: "inst-new", NodeID: "n1", Status: NodeRunning, StartedAt: now.Add(-1 * time.Hour), Result: json.RawMessage(`{"timeout_hours": 72}`)},
		{ID: "e2", InstanceID: "inst-old", NodeID: "n2", Status: NodeRunning, StartedAt: now.Add(-10 * time.Hour), Result: json.RawMessage(`{"timeout_hours": 72}`)},
		{ID: "e3", InstanceID: "inst-mid", NodeID: "n3", Status: NodeRunning, StartedAt: now.Add(-5 * time.Hour), Result: json.RawMessage(`{"timeout_hours": 72}`)},
	}

	ds := NewDirectoryService(
		&mockDirectoryInstanceStore{instances: instances},
		&mockConfirmStore{},
		&mockNodeExecStore{pendingApprovals: pendingExecs},
	)

	items, err := ds.PendingMyAction(context.Background(), "user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}

	// Within same urgency (normal), sorted by InitiatedAt ascending (oldest first).
	if items[0].InstanceID != "inst-old" {
		t.Errorf("expected first item to be inst-old (oldest), got %s", items[0].InstanceID)
	}
	if items[1].InstanceID != "inst-mid" {
		t.Errorf("expected second item to be inst-mid, got %s", items[1].InstanceID)
	}
	if items[2].InstanceID != "inst-new" {
		t.Errorf("expected third item to be inst-new (newest), got %s", items[2].InstanceID)
	}
}

func TestPendingMyAction_SkipsNonRunningInstances(t *testing.T) {
	now := time.Now().UTC()

	instances := map[string]*WorkflowInstance{
		"inst-running": {
			ID: "inst-running", Status: InstanceRunning,
			CreatedAt:    now.Add(-5 * time.Hour),
			InstanceData: map[string]interface{}{"workflow_name": "Running"},
		},
		"inst-completed": {
			ID: "inst-completed", Status: InstanceCompleted,
			CreatedAt:    now.Add(-10 * time.Hour),
			InstanceData: map[string]interface{}{"workflow_name": "Completed"},
		},
		"inst-withdrawn": {
			ID: "inst-withdrawn", Status: InstanceWithdrawn,
			CreatedAt:    now.Add(-8 * time.Hour),
			InstanceData: map[string]interface{}{"workflow_name": "Withdrawn"},
		},
	}

	pendingExecs := []NodeExecution{
		{ID: "e1", InstanceID: "inst-running", NodeID: "n1", Status: NodeRunning, StartedAt: now.Add(-5 * time.Hour)},
		{ID: "e2", InstanceID: "inst-completed", NodeID: "n2", Status: NodeRunning, StartedAt: now.Add(-10 * time.Hour)},
		{ID: "e3", InstanceID: "inst-withdrawn", NodeID: "n3", Status: NodeRunning, StartedAt: now.Add(-8 * time.Hour)},
	}

	ds := NewDirectoryService(
		&mockDirectoryInstanceStore{instances: instances},
		&mockConfirmStore{},
		&mockNodeExecStore{pendingApprovals: pendingExecs},
	)

	items, err := ds.PendingMyAction(context.Background(), "user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only the running instance should be included.
	if len(items) != 1 {
		t.Fatalf("expected 1 item (only running), got %d", len(items))
	}
	if items[0].InstanceID != "inst-running" {
		t.Errorf("expected inst-running, got %s", items[0].InstanceID)
	}
}

func TestPendingMyAction_ExtractsWorkflowNameAndInitiator(t *testing.T) {
	now := time.Now().UTC()

	instances := map[string]*WorkflowInstance{
		"inst-1": {
			ID: "inst-1", Status: InstanceRunning,
			CreatedAt: now.Add(-5 * time.Hour),
			InstanceData: map[string]interface{}{
				"workflow_name":  "请假审批",
				"initiator_name": "张三",
			},
		},
	}

	pendingExecs := []NodeExecution{
		{ID: "e1", InstanceID: "inst-1", NodeID: "approval-node-1", Status: NodeRunning, StartedAt: now.Add(-5 * time.Hour)},
	}

	ds := NewDirectoryService(
		&mockDirectoryInstanceStore{instances: instances},
		&mockConfirmStore{},
		&mockNodeExecStore{pendingApprovals: pendingExecs},
	)

	items, err := ds.PendingMyAction(context.Background(), "user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}

	if items[0].WorkflowName != "请假审批" {
		t.Errorf("expected workflow_name=请假审批, got %s", items[0].WorkflowName)
	}
	if items[0].InitiatorName != "张三" {
		t.Errorf("expected initiator_name=张三, got %s", items[0].InitiatorName)
	}
	if items[0].UserRole != "approver" {
		t.Errorf("expected user_role=approver, got %s", items[0].UserRole)
	}
	if items[0].CurrentNode != "approval-node-1" {
		t.Errorf("expected current_node=approval-node-1, got %s", items[0].CurrentNode)
	}
}

func TestExtractApprovalTimeoutHours(t *testing.T) {
	tests := []struct {
		name   string
		result json.RawMessage
		want   int
	}{
		{
			name:   "nil result",
			result: nil,
			want:   0,
		},
		{
			name:   "empty result",
			result: json.RawMessage(`{}`),
			want:   0,
		},
		{
			name:   "valid timeout_hours",
			result: json.RawMessage(`{"timeout_hours": 48}`),
			want:   48,
		},
		{
			name:   "timeout_hours as float",
			result: json.RawMessage(`{"timeout_hours": 72.0}`),
			want:   72,
		},
		{
			name:   "invalid json",
			result: json.RawMessage(`not json`),
			want:   0,
		},
		{
			name:   "timeout_hours not a number",
			result: json.RawMessage(`{"timeout_hours": "48"}`),
			want:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exec := NodeExecution{Result: tt.result}
			got := extractApprovalTimeoutHours(exec)
			if got != tt.want {
				t.Errorf("extractApprovalTimeoutHours() = %d, want %d", got, tt.want)
			}
		})
	}
}
