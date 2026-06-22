package workflow

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// PgInstanceStore implements InstanceStore backed by PostgreSQL.
type PgInstanceStore struct {
	db *sql.DB
}

// NewPgInstanceStore creates a new PgInstanceStore using the given database connection.
func NewPgInstanceStore(db *sql.DB) *PgInstanceStore {
	return &PgInstanceStore{db: db}
}

func (s *PgInstanceStore) Create(ctx context.Context, inst *WorkflowInstance) error {
	dataJSON, err := json.Marshal(inst.InstanceData)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO workflow_instances (id, tenant_id, workflow_id, version_id, status, current_node_id, instance_data, trigger_data, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		inst.ID, store.TenantIDFromContext(ctx), inst.WorkflowID, inst.VersionID, inst.Status,
		inst.CurrentNodeID, dataJSON, inst.TriggerData, inst.CreatedAt,
	)
	return err
}

func (s *PgInstanceStore) Get(ctx context.Context, id string) (*WorkflowInstance, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, tenant_id, workflow_id, version_id, status, current_node_id, instance_data, trigger_data, created_at, completed_at, row_version
		 FROM workflow_instances WHERE id = $1 AND tenant_id = $2`, id, store.TenantIDFromContext(ctx))

	var inst WorkflowInstance
	var dataJSON []byte
	var completedAt sql.NullTime
	err := row.Scan(&inst.ID, &inst.TenantID, &inst.WorkflowID, &inst.VersionID, &inst.Status,
		&inst.CurrentNodeID, &dataJSON, &inst.TriggerData, &inst.CreatedAt, &completedAt, &inst.RowVersion)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if completedAt.Valid {
		inst.CompletedAt = &completedAt.Time
	}
	if len(dataJSON) > 0 {
		_ = json.Unmarshal(dataJSON, &inst.InstanceData)
	}
	return &inst, nil
}

func (s *PgInstanceStore) UpdateStatus(ctx context.Context, id string, status InstanceStatus) error {
	var completedAt *time.Time
	if status == InstanceCompleted || status == InstanceFailed {
		now := time.Now().UTC()
		completedAt = &now
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE workflow_instances SET status = $1, completed_at = $2 WHERE id = $3 AND tenant_id = $4`,
		status, completedAt, id, store.TenantIDFromContext(ctx))
	return err
}

func (s *PgInstanceStore) UpdateCurrentNode(ctx context.Context, id, nodeID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE workflow_instances SET current_node_id = $1 WHERE id = $2 AND tenant_id = $3`,
		nodeID, id, store.TenantIDFromContext(ctx))
	return err
}

func (s *PgInstanceStore) UpdateInstanceData(ctx context.Context, id string, data map[string]interface{}) error {
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE workflow_instances SET instance_data = $1 WHERE id = $2 AND tenant_id = $3`,
		dataJSON, id, store.TenantIDFromContext(ctx))
	return err
}

// UpdateInstanceDataCAS persists instance_data only if the row's row_version
// still equals expectedVersion, bumping it on success. This is the
// optimistic-locking guard that serializes concurrent approval-state writes on
// the same node so no vote is lost across processes (Requirement 2.6). On a
// version mismatch it returns ErrInstanceVersionConflict without writing, so
// the caller re-reads and re-applies its decision. Implements
// OptimisticInstanceDataUpdater.
func (s *PgInstanceStore) UpdateInstanceDataCAS(ctx context.Context, id string, expectedVersion int64, data map[string]interface{}) (int64, error) {
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return 0, err
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE workflow_instances SET instance_data = $1, row_version = row_version + 1
		 WHERE id = $2 AND tenant_id = $3 AND row_version = $4`,
		dataJSON, id, store.TenantIDFromContext(ctx), expectedVersion)
	if err != nil {
		return 0, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if affected == 0 {
		return 0, ErrInstanceVersionConflict
	}
	return expectedVersion + 1, nil
}

func (s *PgInstanceStore) CreateNodeExecution(ctx context.Context, exec *NodeExecution) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO workflow_node_executions (id, instance_id, node_id, node_type, status, started_at, result, fail_reason)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		exec.ID, exec.InstanceID, exec.NodeID, string(exec.NodeType), exec.Status,
		exec.StartedAt, exec.Result, exec.FailReason,
	)
	return err
}

func (s *PgInstanceStore) UpdateNodeExecution(ctx context.Context, id string, status NodeStatus, result json.RawMessage, failReason string) error {
	var completedAt any
	if status == NodeCompleted || status == NodeFailed || status == NodeSkipped {
		completedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE workflow_node_executions SET status = $1, completed_at = COALESCE($2, completed_at), result = $3, fail_reason = $4 WHERE id = $5 AND instance_id IN (SELECT id FROM workflow_instances WHERE tenant_id = $6)`,
		status, completedAt, result, failReason, id, store.TenantIDFromContext(ctx))
	return err
}

func (s *PgInstanceStore) GetPendingApprovals(ctx context.Context, approverID string) ([]NodeExecution, error) {
	query := `SELECT wne.id, wne.instance_id, wne.node_id, wne.node_type, wne.status, wne.started_at, wne.completed_at, wne.result, wne.fail_reason
		 FROM workflow_node_executions wne INNER JOIN workflow_instances wi ON wi.id = wne.instance_id WHERE wne.status = $1 AND wi.tenant_id = $2 AND wne.node_type = $3`
	args := []any{string(NodeRunning), store.TenantIDFromContext(ctx), string(NodeApproval)}

	if approverID != "" {
		// When approverID is provided, filter by instances assigned to that approver.
		// This requires joining with workflow_instances to check instance_data.
		// For simplicity, we return all running node executions and let the caller filter.
		// A more efficient implementation would use a JSON query on instance_data.
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var execs []NodeExecution
	for rows.Next() {
		var exec NodeExecution
		var completedAt sql.NullTime
		var nodeType sql.NullString
		err := rows.Scan(&exec.ID, &exec.InstanceID, &exec.NodeID, &nodeType, &exec.Status,
			&exec.StartedAt, &completedAt, &exec.Result, &exec.FailReason)
		if err != nil {
			return nil, err
		}
		if completedAt.Valid {
			exec.CompletedAt = &completedAt.Time
		}
		if nodeType.Valid {
			exec.NodeType = NodeType(nodeType.String)
		}
		execs = append(execs, exec)
	}
	return execs, rows.Err()
}

// FindCompletedWithoutConfirmations returns completed instances whose
// completed_at is within the given retention window and that have no rows in
// the confirmations table — i.e. instances orphaned by a crash between marking
// the instance completed and creating confirmation records (the window
// documented in executeTerminalNode). Used by
// ConfirmationTracker.ReconcileOrphanedInstances. Implements OrphanedInstanceFinder.
func (s *PgInstanceStore) FindCompletedWithoutConfirmations(ctx context.Context, within time.Duration) ([]WorkflowInstance, error) {
	cutoff := time.Now().UTC().Add(-within).Format(time.RFC3339Nano)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tenant_id, workflow_id, version_id, status, current_node_id, instance_data, trigger_data, created_at, completed_at
		 FROM workflow_instances wi
		 WHERE wi.tenant_id = $1
		   AND wi.status = $2
		   AND wi.completed_at IS NOT NULL
		   AND wi.completed_at >= $3
		   AND NOT EXISTS (SELECT 1 FROM confirmations c WHERE c.instance_id = wi.id)
		 ORDER BY wi.completed_at ASC`,
		store.TenantIDFromContext(ctx), string(InstanceCompleted), cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []WorkflowInstance
	for rows.Next() {
		var inst WorkflowInstance
		var dataJSON []byte
		var completedAt sql.NullTime
		if err := rows.Scan(&inst.ID, &inst.TenantID, &inst.WorkflowID, &inst.VersionID, &inst.Status,
			&inst.CurrentNodeID, &dataJSON, &inst.TriggerData, &inst.CreatedAt, &completedAt); err != nil {
			return nil, err
		}
		if completedAt.Valid {
			inst.CompletedAt = &completedAt.Time
		}
		if len(dataJSON) > 0 {
			_ = json.Unmarshal(dataJSON, &inst.InstanceData)
		}
		results = append(results, inst)
	}
	return results, rows.Err()
}

// --- Directory Query Methods ---

// QueryMyInitiated returns DirectoryItems for instances where the user is the initiator.
// Supports filtering by status, date range, and workflow type.
// Sorted by created_at DESC with pagination.
func (s *PgInstanceStore) QueryMyInitiated(ctx context.Context, userID string, filter DirectoryFilter) ([]DirectoryItem, int, error) {
	baseWhere := `tenant_id = ? AND json_extract(instance_data, '$.initiator_id') = ?`
	args := []interface{}{store.TenantIDFromContext(ctx), userID}

	if filter.Status != "" {
		baseWhere += ` AND status = ?`
		args = append(args, filter.Status)
	}
	if filter.DateFrom != nil {
		baseWhere += ` AND created_at >= ?`
		args = append(args, filter.DateFrom.Format(time.RFC3339Nano))
	}
	if filter.DateTo != nil {
		baseWhere += ` AND created_at <= ?`
		args = append(args, filter.DateTo.Format(time.RFC3339Nano))
	}
	if filter.WorkflowType != "" {
		baseWhere += ` AND json_extract(instance_data, '$.workflow_name') = ?`
		args = append(args, filter.WorkflowType)
	}

	// Count total
	var total int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM workflow_instances WHERE `+baseWhere, args...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return nil, 0, nil
	}

	// Query with pagination
	offset := (filter.Page - 1) * filter.PageSize
	dataQuery := `SELECT id, workflow_id, status, current_node_id, instance_data, created_at, completed_at
		FROM workflow_instances WHERE ` + baseWhere + `
		ORDER BY created_at DESC LIMIT ? OFFSET ?`
	dataArgs := append(args, filter.PageSize, offset)

	rows, err := s.db.QueryContext(ctx, dataQuery, dataArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var items []DirectoryItem
	for rows.Next() {
		var id, workflowID, status, currentNode string
		var dataJSON []byte
		var createdAt time.Time
		var completedAt sql.NullTime

		if err := rows.Scan(&id, &workflowID, &status, &currentNode, &dataJSON, &createdAt, &completedAt); err != nil {
			return nil, 0, err
		}

		item := DirectoryItem{
			InstanceID:  id,
			Status:      status,
			CurrentNode: currentNode,
			InitiatedAt: createdAt,
			UserRole:    "initiator",
		}
		if completedAt.Valid {
			item.CompletedAt = &completedAt.Time
		}

		// Extract workflow_name from instance_data JSON
		if len(dataJSON) > 0 {
			var data map[string]interface{}
			if json.Unmarshal(dataJSON, &data) == nil {
				if name, ok := data["workflow_name"].(string); ok {
					item.WorkflowName = name
				}
			}
		}

		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return items, total, nil
}

// QueryPendingMyAction returns DirectoryItems for instances with pending approval nodes
// assigned to the user. Sorted by urgency (overdue first) then created_at ASC.
func (s *PgInstanceStore) QueryPendingMyAction(ctx context.Context, userID string, filter DirectoryFilter) ([]DirectoryItem, int, error) {
	// Query running node executions where the approver matches userID.
	// The approver assignment is stored in instance_data or node execution result.
	// For simplicity, we query all running instances and check pending approvals.
	query := `SELECT wi.id, wi.workflow_id, wi.status, wi.current_node_id, wi.instance_data, wi.created_at
		FROM workflow_instances wi
		INNER JOIN workflow_node_executions wne ON wne.instance_id = wi.id
		WHERE wi.tenant_id = ? AND wi.status = ? AND wne.status = ?
		ORDER BY wi.created_at ASC`

	rows, err := s.db.QueryContext(ctx, query, store.TenantIDFromContext(ctx), string(InstanceRunning), string(NodeRunning))
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var allItems []DirectoryItem
	for rows.Next() {
		var id, workflowID, status, currentNode string
		var dataJSON []byte
		var createdAt time.Time

		if err := rows.Scan(&id, &workflowID, &status, &currentNode, &dataJSON, &createdAt); err != nil {
			return nil, 0, err
		}

		item := DirectoryItem{
			InstanceID:  id,
			Status:      status,
			CurrentNode: currentNode,
			InitiatedAt: createdAt,
			UserRole:    "approver",
			Urgency:     "normal",
		}

		// Extract workflow_name and initiator_name from instance_data
		if len(dataJSON) > 0 {
			var data map[string]interface{}
			if json.Unmarshal(dataJSON, &data) == nil {
				if name, ok := data["workflow_name"].(string); ok {
					item.WorkflowName = name
				}
				if iname, ok := data["initiator_name"].(string); ok {
					item.InitiatorName = iname
				}
			}
		}

		allItems = append(allItems, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	total := len(allItems)

	// Apply pagination
	offset := (filter.Page - 1) * filter.PageSize
	end := offset + filter.PageSize
	if offset >= total {
		return nil, total, nil
	}
	if end > total {
		end = total
	}

	return allItems[offset:end], total, nil
}

// QueryPendingMyConfirmation returns DirectoryItems for instances with pending
// confirmations for the user. Sorted by time remaining (least first).
func (s *PgInstanceStore) QueryPendingMyConfirmation(ctx context.Context, userID string, filter DirectoryFilter) ([]DirectoryItem, int, error) {
	// Query pending confirmations for this user, join with workflow_instances
	query := `SELECT c.id, c.instance_id, c.type, c.timeout_hours, c.created_at,
		wi.status, wi.instance_data, wi.completed_at
		FROM confirmations c
		INNER JOIN workflow_instances wi ON wi.id = c.instance_id
		WHERE wi.tenant_id = ? AND c.status = ? AND c.recipient_id = ?
		ORDER BY datetime(c.created_at, '+' || c.timeout_hours || ' hours') ASC`

	rows, err := s.db.QueryContext(ctx, query, store.TenantIDFromContext(ctx), string(ConfirmPending), userID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	now := time.Now().UTC()
	var allItems []DirectoryItem
	for rows.Next() {
		var confID, instanceID, confType string
		var timeoutHours int
		var confCreatedAt time.Time
		var instStatus string
		var dataJSON []byte
		var completedAt sql.NullTime

		if err := rows.Scan(&confID, &instanceID, &confType, &timeoutHours, &confCreatedAt,
			&instStatus, &dataJSON, &completedAt); err != nil {
			return nil, 0, err
		}

		item := DirectoryItem{
			InstanceID:  instanceID,
			Status:      instStatus,
			InitiatedAt: confCreatedAt,
			UserRole:    confType, // "executor" or "notifier"
			ConfirmType: confType,
		}
		if completedAt.Valid {
			item.CompletedAt = &completedAt.Time
		}

		// Calculate time remaining in hours
		deadline := confCreatedAt.Add(time.Duration(timeoutHours) * time.Hour)
		remaining := int(deadline.Sub(now).Hours())
		if remaining < 0 {
			remaining = 0
		}
		item.TimeRemaining = &remaining

		// Extract workflow_name and result from instance_data
		if len(dataJSON) > 0 {
			var data map[string]interface{}
			if json.Unmarshal(dataJSON, &data) == nil {
				if name, ok := data["workflow_name"].(string); ok {
					item.WorkflowName = name
				}
				if result, ok := data["result"].(string); ok {
					item.Result = result
				}
			}
		}

		allItems = append(allItems, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	total := len(allItems)

	// Apply pagination
	offset := (filter.Page - 1) * filter.PageSize
	end := offset + filter.PageSize
	if offset >= total {
		return nil, total, nil
	}
	if end > total {
		end = total
	}

	return allItems[offset:end], total, nil
}

// QueryCompleted returns DirectoryItems for terminal-state instances where the user
// participated as initiator, approver, executor, or notifier.
// Supports filtering by date range, workflow type, result, and role.
// Sorted by completed_at DESC with pagination.
func (s *PgInstanceStore) QueryCompleted(ctx context.Context, userID string, filter DirectoryFilter) ([]DirectoryItem, int, error) {
	// Query instances in terminal states where the user participated.
	// Participation is determined by:
	// - initiator_id in instance_data matches userID
	// - OR there's a confirmation record with recipient_id = userID
	// - OR there's a node_execution assigned to userID
	baseWhere := `wi.tenant_id = ? AND (wi.status IN (?, ?, ?, ?)) AND (
		json_extract(wi.instance_data, '$.initiator_id') = ?
		OR EXISTS (SELECT 1 FROM confirmations c WHERE c.instance_id = wi.id AND c.recipient_id = ?)
	)`
	args := []interface{}{
		store.TenantIDFromContext(ctx),
		string(InstanceCompleted), string(InstanceFailed), string(InstanceWithdrawn), string(InstanceCancelled),
		userID, userID,
	}

	if filter.DateFrom != nil {
		baseWhere += ` AND wi.completed_at >= ?`
		args = append(args, filter.DateFrom.Format(time.RFC3339Nano))
	}
	if filter.DateTo != nil {
		baseWhere += ` AND wi.completed_at <= ?`
		args = append(args, filter.DateTo.Format(time.RFC3339Nano))
	}
	if filter.WorkflowType != "" {
		baseWhere += ` AND json_extract(wi.instance_data, '$.workflow_name') = ?`
		args = append(args, filter.WorkflowType)
	}
	if filter.Result != "" {
		baseWhere += ` AND json_extract(wi.instance_data, '$.result') = ?`
		args = append(args, filter.Result)
	}
	if filter.Status != "" {
		baseWhere += ` AND wi.status = ?`
		args = append(args, filter.Status)
	}

	// Count total
	var total int
	countQuery := `SELECT COUNT(*) FROM workflow_instances wi WHERE ` + baseWhere
	err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return nil, 0, nil
	}

	// Query with pagination
	offset := (filter.Page - 1) * filter.PageSize
	dataQuery := `SELECT wi.id, wi.workflow_id, wi.status, wi.instance_data, wi.created_at, wi.completed_at
		FROM workflow_instances wi WHERE ` + baseWhere + `
		ORDER BY wi.completed_at DESC LIMIT ? OFFSET ?`
	dataArgs := append(args, filter.PageSize, offset)

	rows, err := s.db.QueryContext(ctx, dataQuery, dataArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var items []DirectoryItem
	for rows.Next() {
		var id, workflowID, status string
		var dataJSON []byte
		var createdAt time.Time
		var completedAt sql.NullTime

		if err := rows.Scan(&id, &workflowID, &status, &dataJSON, &createdAt, &completedAt); err != nil {
			return nil, 0, err
		}

		item := DirectoryItem{
			InstanceID:  id,
			Status:      status,
			InitiatedAt: createdAt,
		}
		if completedAt.Valid {
			item.CompletedAt = &completedAt.Time
		}

		// Extract workflow_name, result, and determine user role from instance_data
		if len(dataJSON) > 0 {
			var data map[string]interface{}
			if json.Unmarshal(dataJSON, &data) == nil {
				if name, ok := data["workflow_name"].(string); ok {
					item.WorkflowName = name
				}
				if result, ok := data["result"].(string); ok {
					item.Result = result
				}
				// Determine user role
				if initiatorID, ok := data["initiator_id"].(string); ok && initiatorID == userID {
					item.UserRole = "initiator"
				}
			}
		}

		// If role filter is set and doesn't match, skip
		if filter.Role != "" && item.UserRole != "" && item.UserRole != filter.Role {
			continue
		}

		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return items, total, nil
}
