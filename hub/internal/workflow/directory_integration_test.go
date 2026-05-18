package workflow

import (
	"context"
	"encoding/json"
	"sort"
	"testing"
	"time"
)

// --- Full-featured mock stores for integration tests ---

// fullMockInstanceStore implements InstanceStore with in-memory data for integration tests.
type fullMockInstanceStore struct {
	instances []*WorkflowInstance
}

func (s *fullMockInstanceStore) Create(ctx context.Context, inst *WorkflowInstance) error {
	s.instances = append(s.instances, inst)
	return nil
}

func (s *fullMockInstanceStore) Get(ctx context.Context, id string) (*WorkflowInstance, error) {
	for _, inst := range s.instances {
		if inst.ID == id {
			return inst, nil
		}
	}
	return nil, nil
}

func (s *fullMockInstanceStore) UpdateStatus(ctx context.Context, id string, status InstanceStatus) error {
	return nil
}
func (s *fullMockInstanceStore) UpdateCurrentNode(ctx context.Context, id, nodeID string) error {
	return nil
}
func (s *fullMockInstanceStore) UpdateInstanceData(ctx context.Context, id string, data map[string]interface{}) error {
	return nil
}
func (s *fullMockInstanceStore) CreateNodeExecution(ctx context.Context, exec *NodeExecution) error {
	return nil
}
func (s *fullMockInstanceStore) UpdateNodeExecution(ctx context.Context, id string, status NodeStatus, result json.RawMessage, failReason string) error {
	return nil
}
func (s *fullMockInstanceStore) GetPendingApprovals(ctx context.Context, approverID string) ([]NodeExecution, error) {
	return nil, nil
}

// QueryMyInitiated filters instances by initiator_id, applies filters, sorts by initiated_at DESC, paginates.
func (s *fullMockInstanceStore) QueryMyInitiated(ctx context.Context, userID string, filter DirectoryFilter) ([]DirectoryItem, int, error) {
	var items []DirectoryItem
	for _, inst := range s.instances {
		initiatorID := extractStringFromInstanceData(inst.InstanceData, "initiator_id")
		if initiatorID != userID {
			continue
		}
		// Apply status filter.
		if filter.Status != "" && string(inst.Status) != filter.Status {
			continue
		}
		// Apply date range filter.
		if filter.DateFrom != nil && inst.CreatedAt.Before(*filter.DateFrom) {
			continue
		}
		if filter.DateTo != nil && inst.CreatedAt.After(*filter.DateTo) {
			continue
		}
		// Apply workflow type filter.
		wfType := extractStringFromInstanceData(inst.InstanceData, "workflow_type")
		if filter.WorkflowType != "" && wfType != filter.WorkflowType {
			continue
		}
		items = append(items, DirectoryItem{
			InstanceID:   inst.ID,
			WorkflowName: extractStringFromInstanceData(inst.InstanceData, "workflow_name"),
			Status:       string(inst.Status),
			CurrentNode:  inst.CurrentNodeID,
			InitiatedAt:  inst.CreatedAt,
		})
	}
	// Sort by initiated_at DESC.
	sort.Slice(items, func(i, j int) bool {
		return items[i].InitiatedAt.After(items[j].InitiatedAt)
	})
	total := len(items)
	// Paginate.
	start := (filter.Page - 1) * filter.PageSize
	if start >= len(items) {
		return []DirectoryItem{}, total, nil
	}
	end := start + filter.PageSize
	if end > len(items) {
		end = len(items)
	}
	return items[start:end], total, nil
}

func (s *fullMockInstanceStore) QueryPendingMyAction(ctx context.Context, userID string, filter DirectoryFilter) ([]DirectoryItem, int, error) {
	return nil, 0, nil
}

// QueryPendingMyConfirmation returns pending confirmations for the user.
// This is delegated from the confirmStore data in the integration test.
func (s *fullMockInstanceStore) QueryPendingMyConfirmation(ctx context.Context, userID string, filter DirectoryFilter) ([]DirectoryItem, int, error) {
	// This will be overridden by fullMockInstanceStoreWithConfirmations.
	return nil, 0, nil
}

// QueryCompleted returns completed instances where user participated.
func (s *fullMockInstanceStore) QueryCompleted(ctx context.Context, userID string, filter DirectoryFilter) ([]DirectoryItem, int, error) {
	return nil, 0, nil
}

// fullMockInstanceStoreWithConfirmations extends fullMockInstanceStore with confirmation-aware queries.
type fullMockInstanceStoreWithConfirmations struct {
	fullMockInstanceStore
	confirmations []*Confirmation
	// participations maps instanceID -> list of userIDs who participated
	participations map[string][]string
}

func (s *fullMockInstanceStoreWithConfirmations) QueryPendingMyConfirmation(ctx context.Context, userID string, filter DirectoryFilter) ([]DirectoryItem, int, error) {
	now := time.Now().UTC()
	var items []DirectoryItem
	for _, conf := range s.confirmations {
		if conf.RecipientID != userID || conf.Status != ConfirmPending {
			continue
		}
		// Find the instance.
		var inst *WorkflowInstance
		for _, i := range s.instances {
			if i.ID == conf.InstanceID {
				inst = i
				break
			}
		}
		if inst == nil {
			continue
		}
		// Calculate time remaining.
		deadline := conf.CreatedAt.Add(time.Duration(conf.TimeoutHours) * time.Hour)
		remaining := int(deadline.Sub(now).Hours())
		if remaining < 0 {
			remaining = 0
		}
		items = append(items, DirectoryItem{
			InstanceID:    inst.ID,
			WorkflowName:  extractStringFromInstanceData(inst.InstanceData, "workflow_name"),
			Status:        string(inst.Status),
			InitiatedAt:   inst.CreatedAt,
			CompletedAt:   inst.CompletedAt,
			Result:        extractStringFromInstanceData(inst.InstanceData, "result"),
			ConfirmType:   string(conf.Type),
			TimeRemaining: &remaining,
		})
	}
	// Sort by time_remaining ASC.
	sort.Slice(items, func(i, j int) bool {
		if items[i].TimeRemaining == nil {
			return false
		}
		if items[j].TimeRemaining == nil {
			return true
		}
		return *items[i].TimeRemaining < *items[j].TimeRemaining
	})
	total := len(items)
	start := (filter.Page - 1) * filter.PageSize
	if start >= len(items) {
		return []DirectoryItem{}, total, nil
	}
	end := start + filter.PageSize
	if end > len(items) {
		end = len(items)
	}
	return items[start:end], total, nil
}

func (s *fullMockInstanceStoreWithConfirmations) QueryCompleted(ctx context.Context, userID string, filter DirectoryFilter) ([]DirectoryItem, int, error) {
	var items []DirectoryItem
	for _, inst := range s.instances {
		// Only terminal states.
		if inst.Status != InstanceCompleted && inst.Status != InstanceCancelled && inst.Status != InstanceWithdrawn {
			continue
		}
		// Check if user participated.
		participated := false
		initiatorID := extractStringFromInstanceData(inst.InstanceData, "initiator_id")
		userRole := ""
		if initiatorID == userID {
			participated = true
			userRole = "initiator"
		}
		if !participated {
			if parts, ok := s.participations[inst.ID]; ok {
				for _, p := range parts {
					if p == userID {
						participated = true
						break
					}
				}
			}
		}
		if !participated {
			// Check confirmations.
			for _, conf := range s.confirmations {
				if conf.InstanceID == inst.ID && conf.RecipientID == userID {
					participated = true
					break
				}
			}
		}
		if !participated {
			continue
		}
		// Apply date range filter on CompletedAt.
		if inst.CompletedAt != nil {
			if filter.DateFrom != nil && inst.CompletedAt.Before(*filter.DateFrom) {
				continue
			}
			if filter.DateTo != nil && inst.CompletedAt.After(*filter.DateTo) {
				continue
			}
		}
		// Apply workflow type filter.
		wfType := extractStringFromInstanceData(inst.InstanceData, "workflow_type")
		if filter.WorkflowType != "" && wfType != filter.WorkflowType {
			continue
		}
		// Apply result filter.
		result := extractStringFromInstanceData(inst.InstanceData, "result")
		if filter.Result != "" && result != filter.Result {
			continue
		}
		items = append(items, DirectoryItem{
			InstanceID:   inst.ID,
			WorkflowName: extractStringFromInstanceData(inst.InstanceData, "workflow_name"),
			Status:       string(inst.Status),
			InitiatedAt:  inst.CreatedAt,
			CompletedAt:  inst.CompletedAt,
			Result:       result,
			UserRole:     userRole, // will be enriched by DirectoryService.Completed
		})
	}
	// Sort by completed_at DESC.
	sort.Slice(items, func(i, j int) bool {
		if items[i].CompletedAt == nil {
			return false
		}
		if items[j].CompletedAt == nil {
			return true
		}
		return items[i].CompletedAt.After(*items[j].CompletedAt)
	})
	total := len(items)
	start := (filter.Page - 1) * filter.PageSize
	if start >= len(items) {
		return []DirectoryItem{}, total, nil
	}
	end := start + filter.PageSize
	if end > len(items) {
		end = len(items)
	}
	return items[start:end], total, nil
}

// fullMockConfirmStore implements ConfirmationStore with in-memory data.
type fullMockConfirmStore struct {
	confirmations []*Confirmation
}

func (s *fullMockConfirmStore) Create(ctx context.Context, conf *Confirmation) error {
	s.confirmations = append(s.confirmations, conf)
	return nil
}
func (s *fullMockConfirmStore) Get(ctx context.Context, id string) (*Confirmation, error) {
	for _, c := range s.confirmations {
		if c.ID == id {
			return c, nil
		}
	}
	return nil, nil
}
func (s *fullMockConfirmStore) UpdateStatus(ctx context.Context, id string, status ConfirmationStatus, notes string) error {
	return nil
}
func (s *fullMockConfirmStore) IncrementReminders(ctx context.Context, id string) error {
	return nil
}
func (s *fullMockConfirmStore) ListPending(ctx context.Context, recipientID string) ([]Confirmation, error) {
	var result []Confirmation
	for _, c := range s.confirmations {
		if c.RecipientID == recipientID && c.Status == ConfirmPending {
			result = append(result, *c)
		}
	}
	return result, nil
}
func (s *fullMockConfirmStore) ListByInstance(ctx context.Context, instanceID string) ([]Confirmation, error) {
	var result []Confirmation
	for _, c := range s.confirmations {
		if c.InstanceID == instanceID {
			result = append(result, *c)
		}
	}
	return result, nil
}
func (s *fullMockConfirmStore) FindOverdue(ctx context.Context) ([]Confirmation, error) {
	return nil, nil
}

// fullMockNodeExecStore implements NodeExecutionStore with configurable per-user results.
type fullMockNodeExecStore struct {
	// pendingByUser maps userID -> pending node executions
	pendingByUser map[string][]NodeExecution
}

func (s *fullMockNodeExecStore) GetPendingApprovalsForUser(ctx context.Context, userID string) ([]NodeExecution, error) {
	if execs, ok := s.pendingByUser[userID]; ok {
		return execs, nil
	}
	return nil, nil
}

// --- Integration Tests ---

func TestIntegration_Directory_MyInitiated(t *testing.T) {
	now := time.Now().UTC()

	// Create instances with different initiators.
	store := &fullMockInstanceStore{
		instances: []*WorkflowInstance{
			{ID: "inst-a1", Status: InstanceRunning, CreatedAt: now.Add(-1 * time.Hour),
				InstanceData: map[string]interface{}{"initiator_id": "userA", "workflow_name": "Leave Request"}},
			{ID: "inst-a2", Status: InstanceCompleted, CreatedAt: now.Add(-5 * time.Hour),
				InstanceData: map[string]interface{}{"initiator_id": "userA", "workflow_name": "Purchase Order"}},
			{ID: "inst-a3", Status: InstanceRunning, CreatedAt: now.Add(-3 * time.Hour),
				InstanceData: map[string]interface{}{"initiator_id": "userA", "workflow_name": "Travel Request"}},
			{ID: "inst-b1", Status: InstanceRunning, CreatedAt: now.Add(-2 * time.Hour),
				InstanceData: map[string]interface{}{"initiator_id": "userB", "workflow_name": "Expense Report"}},
			{ID: "inst-a4", Status: InstanceWithdrawn, CreatedAt: now.Add(-10 * time.Hour),
				InstanceData: map[string]interface{}{"initiator_id": "userA", "workflow_name": "Old Request"}},
		},
	}

	ds := NewDirectoryService(store, &fullMockConfirmStore{}, &fullMockNodeExecStore{})

	t.Run("only returns user A instances", func(t *testing.T) {
		items, total, err := ds.MyInitiated(context.Background(), "userA", DirectoryFilter{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if total != 4 {
			t.Errorf("expected total=4 for userA, got %d", total)
		}
		for _, item := range items {
			// Verify all returned items belong to userA by checking instance IDs.
			if item.InstanceID == "inst-b1" {
				t.Errorf("userB's instance should not appear in userA's view")
			}
		}
	})

	t.Run("sorted by initiated_at DESC", func(t *testing.T) {
		items, _, err := ds.MyInitiated(context.Background(), "userA", DirectoryFilter{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(items) < 2 {
			t.Fatalf("expected at least 2 items, got %d", len(items))
		}
		// Verify descending order.
		for i := 1; i < len(items); i++ {
			if items[i].InitiatedAt.After(items[i-1].InitiatedAt) {
				t.Errorf("items not sorted DESC: item[%d].InitiatedAt=%v > item[%d].InitiatedAt=%v",
					i, items[i].InitiatedAt, i-1, items[i-1].InitiatedAt)
			}
		}
	})

	t.Run("pagination works", func(t *testing.T) {
		// Page 1 with page size 2.
		page1, total, err := ds.MyInitiated(context.Background(), "userA", DirectoryFilter{Page: 1, PageSize: 2})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if total != 4 {
			t.Errorf("expected total=4, got %d", total)
		}
		if len(page1) != 2 {
			t.Errorf("expected 2 items on page 1, got %d", len(page1))
		}

		// Page 2 with page size 2.
		page2, total2, err := ds.MyInitiated(context.Background(), "userA", DirectoryFilter{Page: 2, PageSize: 2})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if total2 != 4 {
			t.Errorf("expected total=4, got %d", total2)
		}
		if len(page2) != 2 {
			t.Errorf("expected 2 items on page 2, got %d", len(page2))
		}

		// Verify no overlap between pages.
		for _, p1 := range page1 {
			for _, p2 := range page2 {
				if p1.InstanceID == p2.InstanceID {
					t.Errorf("duplicate instance %s across pages", p1.InstanceID)
				}
			}
		}
	})

	t.Run("userB only sees own instances", func(t *testing.T) {
		items, total, err := ds.MyInitiated(context.Background(), "userB", DirectoryFilter{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if total != 1 {
			t.Errorf("expected total=1 for userB, got %d", total)
		}
		if len(items) != 1 || items[0].InstanceID != "inst-b1" {
			t.Errorf("expected only inst-b1 for userB, got %v", items)
		}
	})
}

func TestIntegration_Directory_PendingMyAction(t *testing.T) {
	now := time.Now().UTC()

	// Create instances with different states.
	instances := map[string]*WorkflowInstance{
		"inst-1": {ID: "inst-1", Status: InstanceRunning, CreatedAt: now.Add(-72 * time.Hour),
			InstanceData: map[string]interface{}{"workflow_name": "Leave Request", "initiator_name": "Alice"}},
		"inst-2": {ID: "inst-2", Status: InstanceRunning, CreatedAt: now.Add(-48 * time.Hour),
			InstanceData: map[string]interface{}{"workflow_name": "Purchase Order", "initiator_name": "Bob"}},
		"inst-3": {ID: "inst-3", Status: InstanceRunning, CreatedAt: now.Add(-24 * time.Hour),
			InstanceData: map[string]interface{}{"workflow_name": "Travel Request", "initiator_name": "Charlie"}},
		"inst-4": {ID: "inst-4", Status: InstanceRunning, CreatedAt: now.Add(-12 * time.Hour),
			InstanceData: map[string]interface{}{"workflow_name": "Expense Report", "initiator_name": "Dave"}},
	}

	// Pending approvals assigned to different users.
	// userB has 3 pending approvals with different urgencies.
	// userC has 1 pending approval.
	nodeExecStore := &fullMockNodeExecStore{
		pendingByUser: map[string][]NodeExecution{
			"userB": {
				// inst-1: started 72h ago, timeout 48h → overdue
				{ID: "exec-1", InstanceID: "inst-1", NodeID: "approval-1", Status: NodeRunning,
					StartedAt: now.Add(-72 * time.Hour), Result: json.RawMessage(`{"timeout_hours": 48}`)},
				// inst-2: started 48h ago, timeout 54h → approaching (6h remaining, threshold=13.5h)
				{ID: "exec-2", InstanceID: "inst-2", NodeID: "approval-2", Status: NodeRunning,
					StartedAt: now.Add(-48 * time.Hour), Result: json.RawMessage(`{"timeout_hours": 54}`)},
				// inst-3: started 24h ago, timeout 72h → normal (48h remaining)
				{ID: "exec-3", InstanceID: "inst-3", NodeID: "approval-3", Status: NodeRunning,
					StartedAt: now.Add(-24 * time.Hour), Result: json.RawMessage(`{"timeout_hours": 72}`)},
			},
			"userC": {
				{ID: "exec-4", InstanceID: "inst-4", NodeID: "approval-4", Status: NodeRunning,
					StartedAt: now.Add(-12 * time.Hour), Result: json.RawMessage(`{"timeout_hours": 72}`)},
			},
		},
	}

	instStore := &mockDirectoryInstanceStore{instances: instances}
	ds := NewDirectoryService(instStore, &mockConfirmStore{}, nodeExecStore)

	t.Run("only returns userB pending approvals", func(t *testing.T) {
		items, err := ds.PendingMyAction(context.Background(), "userB")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(items) != 3 {
			t.Fatalf("expected 3 items for userB, got %d", len(items))
		}
		// Verify all items are for instances assigned to userB.
		expectedIDs := map[string]bool{"inst-1": true, "inst-2": true, "inst-3": true}
		for _, item := range items {
			if !expectedIDs[item.InstanceID] {
				t.Errorf("unexpected instance %s in userB's pending actions", item.InstanceID)
			}
		}
	})

	t.Run("urgency calculation correct", func(t *testing.T) {
		items, err := ds.PendingMyAction(context.Background(), "userB")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Verify urgency levels.
		urgencyMap := make(map[string]string)
		for _, item := range items {
			urgencyMap[item.InstanceID] = item.Urgency
		}
		if urgencyMap["inst-1"] != UrgencyOverdue {
			t.Errorf("inst-1 expected overdue, got %s", urgencyMap["inst-1"])
		}
		if urgencyMap["inst-2"] != UrgencyApproachingTimeout {
			t.Errorf("inst-2 expected approaching_timeout, got %s", urgencyMap["inst-2"])
		}
		if urgencyMap["inst-3"] != UrgencyNormal {
			t.Errorf("inst-3 expected normal, got %s", urgencyMap["inst-3"])
		}
	})

	t.Run("sorted by urgency then date", func(t *testing.T) {
		items, err := ds.PendingMyAction(context.Background(), "userB")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Expected order: overdue (inst-1) → approaching (inst-2) → normal (inst-3)
		if items[0].Urgency != UrgencyOverdue {
			t.Errorf("first item should be overdue, got %s", items[0].Urgency)
		}
		if items[1].Urgency != UrgencyApproachingTimeout {
			t.Errorf("second item should be approaching_timeout, got %s", items[1].Urgency)
		}
		if items[2].Urgency != UrgencyNormal {
			t.Errorf("third item should be normal, got %s", items[2].Urgency)
		}
	})

	t.Run("userC only sees own pending", func(t *testing.T) {
		items, err := ds.PendingMyAction(context.Background(), "userC")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(items) != 1 {
			t.Fatalf("expected 1 item for userC, got %d", len(items))
		}
		if items[0].InstanceID != "inst-4" {
			t.Errorf("expected inst-4, got %s", items[0].InstanceID)
		}
	})
}

func TestIntegration_Directory_PendingMyConfirmation(t *testing.T) {
	now := time.Now().UTC()

	completedAt1 := now.Add(-2 * time.Hour)
	completedAt2 := now.Add(-5 * time.Hour)
	completedAt3 := now.Add(-1 * time.Hour)

	instances := []*WorkflowInstance{
		{ID: "inst-1", Status: InstanceCompleted, CreatedAt: now.Add(-48 * time.Hour), CompletedAt: &completedAt1,
			InstanceData: map[string]interface{}{"workflow_name": "Leave Request", "result": "approved"}},
		{ID: "inst-2", Status: InstanceCompleted, CreatedAt: now.Add(-72 * time.Hour), CompletedAt: &completedAt2,
			InstanceData: map[string]interface{}{"workflow_name": "Purchase Order", "result": "approved"}},
		{ID: "inst-3", Status: InstanceCompleted, CreatedAt: now.Add(-24 * time.Hour), CompletedAt: &completedAt3,
			InstanceData: map[string]interface{}{"workflow_name": "Travel Request", "result": "rejected"}},
	}

	// Pending confirmations for different users with different timeouts.
	confirmations := []*Confirmation{
		// userC: executor confirmation, 48h timeout, created 40h ago → 8h remaining
		{ID: "conf-1", InstanceID: "inst-1", RecipientID: "userC", Type: ConfirmTypeExecutor,
			Status: ConfirmPending, TimeoutHours: 48, CreatedAt: now.Add(-40 * time.Hour)},
		// userC: notifier confirmation, 72h timeout, created 10h ago → 62h remaining
		{ID: "conf-2", InstanceID: "inst-2", RecipientID: "userC", Type: ConfirmTypeNotifier,
			Status: ConfirmPending, TimeoutHours: 72, CreatedAt: now.Add(-10 * time.Hour)},
		// userC: executor confirmation, 24h timeout, created 22h ago → 2h remaining (most urgent)
		{ID: "conf-3", InstanceID: "inst-3", RecipientID: "userC", Type: ConfirmTypeExecutor,
			Status: ConfirmPending, TimeoutHours: 24, CreatedAt: now.Add(-22 * time.Hour)},
		// userD: notifier confirmation (should not appear for userC)
		{ID: "conf-4", InstanceID: "inst-1", RecipientID: "userD", Type: ConfirmTypeNotifier,
			Status: ConfirmPending, TimeoutHours: 72, CreatedAt: now.Add(-5 * time.Hour)},
		// userC: already confirmed (should not appear)
		{ID: "conf-5", InstanceID: "inst-1", RecipientID: "userC", Type: ConfirmTypeNotifier,
			Status: ConfirmConfirmed, TimeoutHours: 72, CreatedAt: now.Add(-48 * time.Hour)},
	}

	instStore := &fullMockInstanceStoreWithConfirmations{
		fullMockInstanceStore: fullMockInstanceStore{instances: instances},
		confirmations:         confirmations,
	}
	confStore := &fullMockConfirmStore{confirmations: confirmations}
	ds := NewDirectoryService(instStore, confStore, &fullMockNodeExecStore{})

	t.Run("only returns userC pending confirmations", func(t *testing.T) {
		items, total, err := ds.PendingMyConfirmation(context.Background(), "userC", DirectoryFilter{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// userC has 3 pending (conf-1, conf-2, conf-3). conf-5 is confirmed, conf-4 is userD's.
		if total != 3 {
			t.Errorf("expected total=3 for userC, got %d", total)
		}
		for _, item := range items {
			if item.InstanceID == "" {
				t.Error("item has empty instance_id")
			}
		}
	})

	t.Run("sorted by time_remaining ASC", func(t *testing.T) {
		items, _, err := ds.PendingMyConfirmation(context.Background(), "userC", DirectoryFilter{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(items) < 2 {
			t.Fatalf("expected at least 2 items, got %d", len(items))
		}
		// Verify ascending order by time_remaining.
		for i := 1; i < len(items); i++ {
			if items[i].TimeRemaining == nil || items[i-1].TimeRemaining == nil {
				continue
			}
			if *items[i].TimeRemaining < *items[i-1].TimeRemaining {
				t.Errorf("items not sorted ASC by time_remaining: item[%d]=%d < item[%d]=%d",
					i, *items[i].TimeRemaining, i-1, *items[i-1].TimeRemaining)
			}
		}
		// The most urgent (least time remaining) should be first.
		// conf-3: 24h timeout, created 22h ago → ~2h remaining
		if items[0].InstanceID != "inst-3" {
			t.Errorf("expected first item to be inst-3 (least time remaining), got %s", items[0].InstanceID)
		}
	})

	t.Run("userD only sees own confirmations", func(t *testing.T) {
		items, total, err := ds.PendingMyConfirmation(context.Background(), "userD", DirectoryFilter{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if total != 1 {
			t.Errorf("expected total=1 for userD, got %d", total)
		}
		if len(items) != 1 || items[0].InstanceID != "inst-1" {
			t.Errorf("expected only inst-1 for userD, got %v", items)
		}
	})
}

func TestIntegration_Directory_Completed(t *testing.T) {
	now := time.Now().UTC()

	completedAt1 := now.Add(-1 * time.Hour)
	completedAt2 := now.Add(-5 * time.Hour)
	completedAt3 := now.Add(-3 * time.Hour)
	completedAt4 := now.Add(-10 * time.Hour)

	instances := []*WorkflowInstance{
		// inst-1: userD is initiator
		{ID: "inst-1", Status: InstanceCompleted, CreatedAt: now.Add(-48 * time.Hour), CompletedAt: &completedAt1,
			InstanceData: map[string]interface{}{"initiator_id": "userD", "workflow_name": "Leave Request", "result": "approved", "workflow_type": "leave"}},
		// inst-2: userD is executor (via confirmation)
		{ID: "inst-2", Status: InstanceCompleted, CreatedAt: now.Add(-72 * time.Hour), CompletedAt: &completedAt2,
			InstanceData: map[string]interface{}{"initiator_id": "userA", "workflow_name": "Purchase Order", "result": "approved", "workflow_type": "purchase"}},
		// inst-3: userD is notifier (via confirmation)
		{ID: "inst-3", Status: InstanceCompleted, CreatedAt: now.Add(-24 * time.Hour), CompletedAt: &completedAt3,
			InstanceData: map[string]interface{}{"initiator_id": "userB", "workflow_name": "Travel Request", "result": "rejected", "workflow_type": "travel"}},
		// inst-4: userD is approver (via participation)
		{ID: "inst-4", Status: InstanceCompleted, CreatedAt: now.Add(-96 * time.Hour), CompletedAt: &completedAt4,
			InstanceData: map[string]interface{}{"initiator_id": "userC", "workflow_name": "Expense Report", "result": "approved", "workflow_type": "expense"}},
		// inst-5: userD not involved at all
		{ID: "inst-5", Status: InstanceCompleted, CreatedAt: now.Add(-120 * time.Hour), CompletedAt: &completedAt4,
			InstanceData: map[string]interface{}{"initiator_id": "userE", "workflow_name": "Other", "result": "approved", "workflow_type": "other"}},
		// inst-6: running (not terminal, should not appear)
		{ID: "inst-6", Status: InstanceRunning, CreatedAt: now.Add(-12 * time.Hour),
			InstanceData: map[string]interface{}{"initiator_id": "userD", "workflow_name": "Running One", "workflow_type": "leave"}},
	}

	confirmations := []*Confirmation{
		// userD is executor for inst-2
		{ID: "conf-1", InstanceID: "inst-2", RecipientID: "userD", Type: ConfirmTypeExecutor,
			Status: ConfirmConfirmed, TimeoutHours: 48, CreatedAt: now.Add(-70 * time.Hour)},
		// userD is notifier for inst-3
		{ID: "conf-2", InstanceID: "inst-3", RecipientID: "userD", Type: ConfirmTypeNotifier,
			Status: ConfirmConfirmed, TimeoutHours: 72, CreatedAt: now.Add(-22 * time.Hour)},
	}

	instStore := &fullMockInstanceStoreWithConfirmations{
		fullMockInstanceStore: fullMockInstanceStore{instances: instances},
		confirmations:         confirmations,
		participations: map[string][]string{
			"inst-4": {"userD"}, // userD was approver
		},
	}
	confStore := &fullMockConfirmStore{confirmations: confirmations}
	ds := NewDirectoryService(instStore, confStore, &fullMockNodeExecStore{})

	t.Run("returns instances where userD participated in different roles", func(t *testing.T) {
		items, total, err := ds.Completed(context.Background(), "userD", DirectoryFilter{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// userD participated in inst-1 (initiator), inst-2 (executor), inst-3 (notifier), inst-4 (approver).
		// inst-5 (not involved) and inst-6 (running) should not appear.
		if total != 4 {
			t.Errorf("expected total=4 for userD, got %d", total)
		}
		if len(items) != 4 {
			t.Fatalf("expected 4 items, got %d", len(items))
		}
		// Verify inst-5 and inst-6 are not present.
		for _, item := range items {
			if item.InstanceID == "inst-5" {
				t.Error("inst-5 should not appear (userD not involved)")
			}
			if item.InstanceID == "inst-6" {
				t.Error("inst-6 should not appear (still running)")
			}
		}
	})

	t.Run("role determination is correct", func(t *testing.T) {
		items, _, err := ds.Completed(context.Background(), "userD", DirectoryFilter{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		roleMap := make(map[string]string)
		for _, item := range items {
			roleMap[item.InstanceID] = item.UserRole
		}
		// inst-1: userD is initiator (set by QueryCompleted mock).
		if roleMap["inst-1"] != "initiator" {
			t.Errorf("inst-1: expected role=initiator, got %s", roleMap["inst-1"])
		}
		// inst-2: userD is executor (determined via confirmations).
		if roleMap["inst-2"] != "executor" {
			t.Errorf("inst-2: expected role=executor, got %s", roleMap["inst-2"])
		}
		// inst-3: userD is notifier (determined via confirmations).
		if roleMap["inst-3"] != "notifier" {
			t.Errorf("inst-3: expected role=notifier, got %s", roleMap["inst-3"])
		}
		// inst-4: userD is approver (determined via fallback in determineCompletedRole).
		if roleMap["inst-4"] != "approver" {
			t.Errorf("inst-4: expected role=approver, got %s", roleMap["inst-4"])
		}
	})

	t.Run("sorted by completed_at DESC", func(t *testing.T) {
		items, _, err := ds.Completed(context.Background(), "userD", DirectoryFilter{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Verify descending order by CompletedAt.
		for i := 1; i < len(items); i++ {
			if items[i].CompletedAt == nil || items[i-1].CompletedAt == nil {
				continue
			}
			if items[i].CompletedAt.After(*items[i-1].CompletedAt) {
				t.Errorf("items not sorted DESC by completed_at: item[%d]=%v > item[%d]=%v",
					i, items[i].CompletedAt, i-1, items[i-1].CompletedAt)
			}
		}
		// First item should be the most recently completed (inst-1, completed 1h ago).
		if items[0].InstanceID != "inst-1" {
			t.Errorf("expected first item to be inst-1 (most recent), got %s", items[0].InstanceID)
		}
	})

	t.Run("pagination works", func(t *testing.T) {
		page1, total, err := ds.Completed(context.Background(), "userD", DirectoryFilter{Page: 1, PageSize: 2})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if total != 4 {
			t.Errorf("expected total=4, got %d", total)
		}
		if len(page1) != 2 {
			t.Errorf("expected 2 items on page 1, got %d", len(page1))
		}

		page2, _, err := ds.Completed(context.Background(), "userD", DirectoryFilter{Page: 2, PageSize: 2})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(page2) != 2 {
			t.Errorf("expected 2 items on page 2, got %d", len(page2))
		}

		// No overlap.
		for _, p1 := range page1 {
			for _, p2 := range page2 {
				if p1.InstanceID == p2.InstanceID {
					t.Errorf("duplicate instance %s across pages", p1.InstanceID)
				}
			}
		}
	})
}
