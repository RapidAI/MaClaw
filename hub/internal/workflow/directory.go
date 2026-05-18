package workflow

import (
	"context"
	"encoding/json"
	"sort"
	"time"
)

// DirectoryService provides categorized views of workflow instances for a user.
type DirectoryService struct {
	instanceStore InstanceStore
	confirmStore  ConfirmationStore
	nodeExecStore NodeExecutionStore
}

// NodeExecutionStore provides query access to node execution records for directory views.
type NodeExecutionStore interface {
	// GetPendingApprovalsForUser returns pending approval node executions assigned to the user.
	GetPendingApprovalsForUser(ctx context.Context, userID string) ([]NodeExecution, error)
}

// NewDirectoryService creates a new DirectoryService with the given dependencies.
func NewDirectoryService(
	instanceStore InstanceStore,
	confirmStore ConfirmationStore,
	nodeExecStore NodeExecutionStore,
) *DirectoryService {
	return &DirectoryService{
		instanceStore: instanceStore,
		confirmStore:  confirmStore,
		nodeExecStore: nodeExecStore,
	}
}

// DirectoryFilter contains common filter parameters for directory queries.
type DirectoryFilter struct {
	Status       string     `json:"status,omitempty"`
	DateFrom     *time.Time `json:"date_from,omitempty"`
	DateTo       *time.Time `json:"date_to,omitempty"`
	WorkflowType string     `json:"workflow_type,omitempty"`
	Role         string     `json:"role,omitempty"`   // for completed view
	Result       string     `json:"result,omitempty"` // for completed view
	Page         int        `json:"page"`
	PageSize     int        `json:"page_size"`
}

// DirectoryItem represents a single item in a directory view.
type DirectoryItem struct {
	InstanceID    string     `json:"instance_id"`
	WorkflowName  string     `json:"workflow_name"`
	Status        string     `json:"status"`
	CurrentNode   string     `json:"current_node,omitempty"`
	InitiatorName string     `json:"initiator_name,omitempty"`
	InitiatedAt   time.Time  `json:"initiated_at"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
	Result        string     `json:"result,omitempty"`
	UserRole      string     `json:"user_role,omitempty"`            // initiator/approver/executor/notifier
	Urgency       string     `json:"urgency,omitempty"`              // normal/approaching_timeout/overdue
	TimeRemaining *int       `json:"time_remaining_hours,omitempty"` // hours until timeout
	ConfirmType   string     `json:"confirm_type,omitempty"`         // executor/notifier
}

// DirectoryResponse is the paginated response for directory queries.
type DirectoryResponse struct {
	Items    []DirectoryItem `json:"items"`
	Total    int             `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"page_size"`
}

// NormalizeFilter applies defaults to a DirectoryFilter.
// Page defaults to 1, PageSize defaults to 20 (max 100).
func (f *DirectoryFilter) NormalizeFilter() {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize <= 0 {
		f.PageSize = 20
	}
	if f.PageSize > 100 {
		f.PageSize = 100
	}
}

// MyInitiated returns instances where the user is the initiator.
// Supports filtering by status, date range, and workflow type.
// Returns items for the requested page and total count.
func (ds *DirectoryService) MyInitiated(ctx context.Context, userID string, filter DirectoryFilter) ([]DirectoryItem, int, error) {
	filter.NormalizeFilter()

	// Delegate to instance store for the actual query.
	// The store implementation handles SQL filtering, sorting (initiation date desc),
	// and pagination.
	return ds.instanceStore.QueryMyInitiated(ctx, userID, filter)
}

// Urgency levels for pending action items.
const (
	UrgencyOverdue            = "overdue"
	UrgencyApproachingTimeout = "approaching_timeout"
	UrgencyNormal             = "normal"
)

// urgencyRank returns a numeric rank for sorting: overdue=0 (highest), approaching=1, normal=2.
func urgencyRank(urgency string) int {
	switch urgency {
	case UrgencyOverdue:
		return 0
	case UrgencyApproachingTimeout:
		return 1
	default:
		return 2
	}
}

// calculateUrgency determines the urgency level for a pending approval node.
// - overdue: past the node's timeout
// - approaching_timeout: within 25% of timeout remaining
// - normal: otherwise
func calculateUrgency(startedAt time.Time, timeoutHours int, now time.Time) string {
	if timeoutHours <= 0 {
		// No timeout configured, always normal.
		return UrgencyNormal
	}

	deadline := startedAt.Add(time.Duration(timeoutHours) * time.Hour)
	remaining := deadline.Sub(now)

	if remaining <= 0 {
		return UrgencyOverdue
	}

	totalDuration := time.Duration(timeoutHours) * time.Hour
	// Approaching timeout: within 25% of total duration remaining.
	threshold := totalDuration / 4
	if remaining <= threshold {
		return UrgencyApproachingTimeout
	}

	return UrgencyNormal
}

// PendingMyAction returns instances with pending approval nodes assigned to the user.
// It queries pending approvals via nodeExecStore, enriches each with instance data,
// calculates urgency (overdue/approaching_timeout/normal), and sorts by urgency
// descending then submission date ascending (oldest first within each urgency level).
func (ds *DirectoryService) PendingMyAction(ctx context.Context, userID string) ([]DirectoryItem, error) {
	// 1. Get all pending approval node executions assigned to this user.
	pendingExecs, err := ds.nodeExecStore.GetPendingApprovalsForUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	if len(pendingExecs) == 0 {
		return []DirectoryItem{}, nil
	}

	now := time.Now().UTC()
	items := make([]DirectoryItem, 0, len(pendingExecs))

	// Pre-fetch all unique instances to avoid N+1 queries.
	instanceIDs := make(map[string]struct{})
	for _, exec := range pendingExecs {
		instanceIDs[exec.InstanceID] = struct{}{}
	}

	instanceCache := make(map[string]*WorkflowInstance, len(instanceIDs))
	for id := range instanceIDs {
		inst, err := ds.instanceStore.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		if inst != nil {
			instanceCache[id] = inst
		}
	}

	for _, exec := range pendingExecs {
		// 2. Get the instance from cache to extract workflow_name, initiator_name, status, etc.
		inst := instanceCache[exec.InstanceID]
		if inst == nil {
			// Instance not found (possibly deleted), skip.
			continue
		}

		// Only include running instances.
		if inst.Status != InstanceRunning {
			continue
		}

		// Extract timeout from the node execution result or use default.
		// The approval node config timeout is stored in the graph; we extract it
		// from the node execution's Result field if available, otherwise use default.
		timeoutHours := extractApprovalTimeoutHours(exec)

		// 3. Calculate urgency.
		urgency := calculateUrgency(exec.StartedAt, timeoutHours, now)

		// Calculate time remaining in hours (nil if no timeout).
		var timeRemaining *int
		if timeoutHours > 0 {
			deadline := exec.StartedAt.Add(time.Duration(timeoutHours) * time.Hour)
			remaining := int(deadline.Sub(now).Hours())
			if remaining < 0 {
				remaining = 0
			}
			timeRemaining = &remaining
		}

		// Extract workflow_name and initiator_name from instance data.
		workflowName := extractStringFromInstanceData(inst.InstanceData, "workflow_name")
		initiatorName := extractStringFromInstanceData(inst.InstanceData, "initiator_name")

		item := DirectoryItem{
			InstanceID:    inst.ID,
			WorkflowName:  workflowName,
			Status:        string(inst.Status),
			CurrentNode:   exec.NodeID,
			InitiatorName: initiatorName,
			InitiatedAt:   inst.CreatedAt,
			UserRole:      "approver",
			Urgency:       urgency,
			TimeRemaining: timeRemaining,
		}

		items = append(items, item)
	}

	// 4. Sort by urgency (overdue first) then by submission date ascending (oldest first).
	sort.Slice(items, func(i, j int) bool {
		ri := urgencyRank(items[i].Urgency)
		rj := urgencyRank(items[j].Urgency)
		if ri != rj {
			return ri < rj // lower rank = higher urgency = comes first
		}
		// Within same urgency level, oldest first (ascending by InitiatedAt).
		return items[i].InitiatedAt.Before(items[j].InitiatedAt)
	})

	return items, nil
}

// extractApprovalTimeoutHours extracts the timeout_hours from a node execution's Result field.
// The executor stores the approval node config in the Result when dispatching.
// Returns 0 if not available (meaning no timeout configured).
func extractApprovalTimeoutHours(exec NodeExecution) int {
	if len(exec.Result) == 0 {
		return 0
	}

	// Try to parse the result as a map containing timeout_hours.
	var resultData map[string]interface{}
	if err := json.Unmarshal(exec.Result, &resultData); err != nil {
		return 0
	}

	if v, ok := resultData["timeout_hours"]; ok {
		switch t := v.(type) {
		case float64:
			return int(t)
		case json.Number:
			if n, err := t.Int64(); err == nil {
				return int(n)
			}
		}
	}

	// Default timeout for approval nodes if not specified in result.
	return 0
}

// extractStringFromInstanceData safely extracts a string value from instance data.
func extractStringFromInstanceData(data map[string]interface{}, key string) string {
	if data == nil {
		return ""
	}
	if v, ok := data[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// PendingMyConfirmation returns instances with pending confirmations for the user.
// Sorted by time remaining (least time remaining first).
func (ds *DirectoryService) PendingMyConfirmation(ctx context.Context, userID string, filter DirectoryFilter) ([]DirectoryItem, int, error) {
	filter.NormalizeFilter()
	return ds.instanceStore.QueryPendingMyConfirmation(ctx, userID, filter)
}

// Completed returns instances where the user participated and the instance is terminal.
// It queries terminal-state instances (completed/cancelled/withdrawn) where the user
// participated as initiator, approver, executor, or notifier.
// Supports filtering by date_from/date_to (on completed_at), workflow_type, result, and role.
// Results are ordered by completed_at DESC with pagination (default 20 items/page).
func (ds *DirectoryService) Completed(ctx context.Context, userID string, filter DirectoryFilter) ([]DirectoryItem, int, error) {
	filter.NormalizeFilter()

	// Delegate the main query to the instance store.
	// The store handles SQL-level filtering (terminal states, date range, workflow type,
	// result), participation check (initiator/executor/notifier), ordering by
	// completed_at DESC, and pagination.
	items, total, err := ds.instanceStore.QueryCompleted(ctx, userID, filter)
	if err != nil {
		return nil, 0, err
	}

	// For items where the store couldn't determine the user's role (UserRole is empty),
	// determine it by checking confirmations. The store already sets "initiator" when
	// the user is the initiator. We need to check executor/notifier/approver for others.
	if ds.confirmStore != nil {
		for i := range items {
			if items[i].UserRole != "" {
				continue
			}
			role := ds.determineCompletedRole(ctx, userID, items[i].InstanceID)
			items[i].UserRole = role
		}
	}

	// Apply role filter in-memory if specified.
	// The SQL query cannot efficiently filter by role because role determination
	// requires joining multiple tables. We filter after role enrichment.
	if filter.Role != "" {
		filtered := make([]DirectoryItem, 0, len(items))
		for _, item := range items {
			if item.UserRole == filter.Role {
				filtered = append(filtered, item)
			}
		}
		// Adjust total: the SQL total doesn't account for role filtering.
		// For accurate pagination with role filter, we return the filtered count.
		return filtered, len(filtered), nil
	}

	return items, total, nil
}

// determineCompletedRole determines the user's role in a completed instance.
// Checks in priority order: initiator > approver > executor > notifier.
func (ds *DirectoryService) determineCompletedRole(ctx context.Context, userID, instanceID string) string {
	// Check confirmations to see if user is executor or notifier.
	confs, err := ds.confirmStore.ListByInstance(ctx, instanceID)
	if err != nil {
		return "participant"
	}
	for _, conf := range confs {
		if conf.RecipientID == userID {
			if conf.Type == ConfirmTypeExecutor {
				return "executor"
			}
			return "notifier"
		}
	}

	// If not found in confirmations, the user may have been an approver.
	// The store's participation query includes confirmation recipients,
	// so if we reach here, the user was likely an approver (had node executions).
	return "approver"
}
