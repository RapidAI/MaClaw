package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/workflow"
)

// instanceStore implements workflow.InstanceStore using SQLite.
type instanceStore struct {
	db *sql.DB
}

// NewInstanceStore creates a new InstanceStore backed by the given write DB.
func NewInstanceStore(db *sql.DB) workflow.InstanceStore {
	return &instanceStore{db: db}
}

// ---------------------------------------------------------------------------
// WorkflowInstance CRUD
// ---------------------------------------------------------------------------

func (s *instanceStore) Create(ctx context.Context, inst *workflow.WorkflowInstance) error {
	dataJSON, err := json.Marshal(inst.InstanceData)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO workflow_instances (id, tenant_id, workflow_id, version_id, status, current_node_id, instance_data, trigger_data, created_at, completed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		inst.ID,
		store.TenantIDFromContext(ctx),
		inst.WorkflowID,
		inst.VersionID,
		string(inst.Status),
		inst.CurrentNodeID,
		string(dataJSON),
		inst.TriggerData,
		inst.CreatedAt.UTC().Format(time.RFC3339),
		formatNullableTime(inst.CompletedAt),
	)
	return err
}

func (s *instanceStore) Get(ctx context.Context, id string) (*workflow.WorkflowInstance, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, tenant_id, workflow_id, version_id, status, current_node_id, instance_data, trigger_data, created_at, completed_at
		 FROM workflow_instances
		 WHERE id = ? AND tenant_id = ?`,
		id,
		store.TenantIDFromContext(ctx),
	)
	return scanWorkflowInstance(row)
}

func (s *instanceStore) UpdateStatus(ctx context.Context, id string, status workflow.InstanceStatus) error {
	var completedAt sql.NullString
	if status == workflow.InstanceCompleted || status == workflow.InstanceFailed {
		completedAt = sql.NullString{String: time.Now().UTC().Format(time.RFC3339), Valid: true}
	}

	_, err := s.db.ExecContext(ctx,
		`UPDATE workflow_instances SET status = ?, completed_at = COALESCE(?, completed_at) WHERE id = ? AND tenant_id = ?`,
		string(status),
		completedAt,
		id,
		store.TenantIDFromContext(ctx),
	)
	return err
}

func (s *instanceStore) UpdateCurrentNode(ctx context.Context, id, nodeID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE workflow_instances SET current_node_id = ? WHERE id = ? AND tenant_id = ?`,
		nodeID,
		id,
		store.TenantIDFromContext(ctx),
	)
	return err
}

func (s *instanceStore) UpdateInstanceData(ctx context.Context, id string, data map[string]interface{}) error {
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE workflow_instances SET instance_data = ? WHERE id = ? AND tenant_id = ?`,
		string(dataJSON),
		id,
		store.TenantIDFromContext(ctx),
	)
	return err
}

// ---------------------------------------------------------------------------
// NodeExecution CRUD
// ---------------------------------------------------------------------------

func (s *instanceStore) CreateNodeExecution(ctx context.Context, exec *workflow.NodeExecution) error {
	var resultJSON sql.NullString
	if exec.Result != nil {
		resultJSON = sql.NullString{String: string(exec.Result), Valid: true}
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO node_executions (id, instance_id, node_id, node_type, status, started_at, completed_at, result_json, fail_reason)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		exec.ID,
		exec.InstanceID,
		exec.NodeID,
		string(exec.NodeType),
		string(exec.Status),
		exec.StartedAt.UTC().Format(time.RFC3339),
		formatNullableTime(exec.CompletedAt),
		resultJSON,
		exec.FailReason,
	)
	return err
}

func (s *instanceStore) UpdateNodeExecution(ctx context.Context, id string, status workflow.NodeStatus, result json.RawMessage, failReason string) error {
	var resultJSON sql.NullString
	if result != nil {
		resultJSON = sql.NullString{String: string(result), Valid: true}
	}

	var completedAt sql.NullString
	if status == workflow.NodeCompleted || status == workflow.NodeFailed || status == workflow.NodeSkipped {
		completedAt = sql.NullString{String: time.Now().UTC().Format(time.RFC3339), Valid: true}
	}

	_, err := s.db.ExecContext(ctx,
		`UPDATE node_executions SET status = ?, result_json = ?, fail_reason = ?, completed_at = COALESCE(?, completed_at) WHERE id = ? AND instance_id IN (SELECT id FROM workflow_instances WHERE tenant_id = ?)`,
		string(status),
		resultJSON,
		failReason,
		completedAt,
		id,
		store.TenantIDFromContext(ctx),
	)
	return err
}

func (s *instanceStore) GetPendingApprovals(ctx context.Context, approverID string) ([]workflow.NodeExecution, error) {
	// Query all pending node executions. The approverID filtering is done by
	// the caller based on the workflow graph's approval node config (the
	// approver info is stored in the workflow graph, not in node_executions).
	// For now, return all pending nodes -the caller will filter by approver.
	_ = approverID // reserved for future use when approver info is denormalized

	rows, err := s.db.QueryContext(ctx,
		`SELECT ne.id, ne.instance_id, ne.node_id, ne.node_type, ne.status, ne.started_at, ne.completed_at, ne.result_json, ne.fail_reason
		 FROM node_executions ne INNER JOIN workflow_instances wi ON wi.id = ne.instance_id
		 WHERE ne.status = 'running' AND ne.node_type = 'approval' AND wi.tenant_id = ?
		 ORDER BY ne.started_at ASC`,
		store.TenantIDFromContext(ctx),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []workflow.NodeExecution
	for rows.Next() {
		exec, err := scanNodeExecution(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, *exec)
	}
	return results, rows.Err()
}

func (s *instanceStore) QueryMyInitiated(ctx context.Context, userID string, filter workflow.DirectoryFilter) ([]workflow.DirectoryItem, int, error) {
	filter.NormalizeFilter()
	where := `tenant_id = ? AND json_extract(instance_data, '$.initiator_id') = ?`
	args := []any{store.TenantIDFromContext(ctx), userID}
	if filter.Status != "" {
		where += ` AND status = ?`
		args = append(args, filter.Status)
	}
	if filter.DateFrom != nil {
		where += ` AND created_at >= ?`
		args = append(args, filter.DateFrom.Format(time.RFC3339))
	}
	if filter.DateTo != nil {
		where += ` AND created_at <= ?`
		args = append(args, filter.DateTo.Format(time.RFC3339))
	}
	if filter.WorkflowType != "" {
		where += ` AND json_extract(instance_data, '$.workflow_name') = ?`
		args = append(args, filter.WorkflowType)
	}
	return s.queryDirectory(ctx, where, args, filter, `created_at DESC`, "initiator")
}

func (s *instanceStore) QueryPendingMyAction(ctx context.Context, userID string, filter workflow.DirectoryFilter) ([]workflow.DirectoryItem, int, error) {
	filter.NormalizeFilter()
	where := `tenant_id = ? AND status = ? AND json_extract(instance_data, '$.approver_id') = ?`
	args := []any{store.TenantIDFromContext(ctx), string(workflow.InstanceRunning), userID}
	return s.queryDirectory(ctx, where, args, filter, `created_at ASC`, "approver")
}

func (s *instanceStore) QueryPendingMyConfirmation(ctx context.Context, userID string, filter workflow.DirectoryFilter) ([]workflow.DirectoryItem, int, error) {
	filter.NormalizeFilter()
	countQuery := `SELECT COUNT(*) FROM confirmations c INNER JOIN workflow_instances wi ON wi.id = c.instance_id WHERE wi.tenant_id = ? AND c.status = ? AND c.recipient_id = ?`
	args := []any{store.TenantIDFromContext(ctx), string(workflow.ConfirmPending), userID}
	var total int
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return nil, 0, nil
	}
	offset := (filter.Page - 1) * filter.PageSize
	rows, err := s.db.QueryContext(ctx, `SELECT c.type, c.timeout_hours, c.created_at, wi.id, wi.workflow_id, wi.status, wi.current_node_id, wi.instance_data, wi.created_at, wi.completed_at
		FROM confirmations c INNER JOIN workflow_instances wi ON wi.id = c.instance_id
		WHERE wi.tenant_id = ? AND c.status = ? AND c.recipient_id = ?
		ORDER BY c.created_at ASC LIMIT ? OFFSET ?`, append(args, filter.PageSize, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	now := time.Now().UTC()
	items := make([]workflow.DirectoryItem, 0)
	for rows.Next() {
		var confirmType, confirmCreatedAt, workflowID, dataJSON, instanceCreatedAt string
		var timeoutHours int
		var item workflow.DirectoryItem
		var completedAt sql.NullString
		if err := rows.Scan(&confirmType, &timeoutHours, &confirmCreatedAt, &item.InstanceID, &workflowID, &item.Status, &item.CurrentNode, &dataJSON, &instanceCreatedAt, &completedAt); err != nil {
			return nil, 0, err
		}
		item.UserRole = confirmType
		item.ConfirmType = confirmType
		item.InitiatedAt = mustParseTime(instanceCreatedAt)
		deadline := mustParseTime(confirmCreatedAt).Add(time.Duration(timeoutHours) * time.Hour)
		remaining := int(deadline.Sub(now).Hours())
		if remaining < 0 {
			remaining = 0
		}
		item.TimeRemaining = &remaining
		if completedAt.Valid && completedAt.String != "" {
			t := mustParseTime(completedAt.String)
			item.CompletedAt = &t
		}
		populateDirectoryItemFromJSON(&item, dataJSON, "")
		_ = workflowID
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (s *instanceStore) QueryCompleted(ctx context.Context, userID string, filter workflow.DirectoryFilter) ([]workflow.DirectoryItem, int, error) {
	filter.NormalizeFilter()
	where := `tenant_id = ? AND (status IN (?, ?, ?, ?)) AND json_extract(instance_data, '$.initiator_id') = ?`
	args := []any{store.TenantIDFromContext(ctx), string(workflow.InstanceCompleted), string(workflow.InstanceFailed), string(workflow.InstanceWithdrawn), string(workflow.InstanceCancelled), userID}
	if filter.DateFrom != nil {
		where += ` AND completed_at >= ?`
		args = append(args, filter.DateFrom.Format(time.RFC3339))
	}
	if filter.DateTo != nil {
		where += ` AND completed_at <= ?`
		args = append(args, filter.DateTo.Format(time.RFC3339))
	}
	if filter.WorkflowType != "" {
		where += ` AND json_extract(instance_data, '$.workflow_name') = ?`
		args = append(args, filter.WorkflowType)
	}
	if filter.Result != "" {
		where += ` AND json_extract(instance_data, '$.result') = ?`
		args = append(args, filter.Result)
	}
	if filter.Status != "" {
		where += ` AND status = ?`
		args = append(args, filter.Status)
	}

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM workflow_instances WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return nil, 0, nil
	}

	offset := (filter.Page - 1) * filter.PageSize
	rows, err := s.db.QueryContext(ctx, `SELECT id, workflow_id, status, instance_data, created_at, completed_at FROM workflow_instances WHERE `+where+` ORDER BY completed_at DESC LIMIT ? OFFSET ?`, append(args, filter.PageSize, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]workflow.DirectoryItem, 0)
	for rows.Next() {
		var item workflow.DirectoryItem
		var workflowID, status, dataJSON, createdAt string
		var completedAt sql.NullString
		if err := rows.Scan(&item.InstanceID, &workflowID, &status, &dataJSON, &createdAt, &completedAt); err != nil {
			return nil, 0, err
		}
		item.Status = status
		item.InitiatedAt = mustParseTime(createdAt)
		if completedAt.Valid && completedAt.String != "" {
			t := mustParseTime(completedAt.String)
			item.CompletedAt = &t
		}
		populateDirectoryItemFromJSON(&item, dataJSON, userID)
		if filter.Role != "" && item.UserRole != filter.Role {
			continue
		}
		_ = workflowID
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (s *instanceStore) queryDirectory(ctx context.Context, where string, args []any, filter workflow.DirectoryFilter, orderBy, role string) ([]workflow.DirectoryItem, int, error) {
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM workflow_instances WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return nil, 0, nil
	}
	offset := (filter.Page - 1) * filter.PageSize
	rows, err := s.db.QueryContext(ctx, `SELECT id, workflow_id, status, current_node_id, instance_data, created_at, completed_at FROM workflow_instances WHERE `+where+` ORDER BY `+orderBy+` LIMIT ? OFFSET ?`, append(args, filter.PageSize, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]workflow.DirectoryItem, 0)
	for rows.Next() {
		var item workflow.DirectoryItem
		var workflowID, dataJSON, createdAt string
		var completedAt sql.NullString
		if err := rows.Scan(&item.InstanceID, &workflowID, &item.Status, &item.CurrentNode, &dataJSON, &createdAt, &completedAt); err != nil {
			return nil, 0, err
		}
		item.UserRole = role
		item.InitiatedAt = mustParseTime(createdAt)
		if completedAt.Valid && completedAt.String != "" {
			t := mustParseTime(completedAt.String)
			item.CompletedAt = &t
		}
		populateDirectoryItemFromJSON(&item, dataJSON, "")
		_ = workflowID
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func populateDirectoryItemFromJSON(item *workflow.DirectoryItem, dataJSON string, userID string) {
	if item == nil || dataJSON == "" {
		return
	}
	var data map[string]any
	if json.Unmarshal([]byte(dataJSON), &data) != nil {
		return
	}
	if name, ok := data["workflow_name"].(string); ok {
		item.WorkflowName = name
	}
	if result, ok := data["result"].(string); ok {
		item.Result = result
	}
	if initiatorName, ok := data["initiator_name"].(string); ok {
		item.InitiatorName = initiatorName
	}
	if userID != "" {
		if initiatorID, ok := data["initiator_id"].(string); ok && initiatorID == userID {
			item.UserRole = "initiator"
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// scanWorkflowInstance scans a single row into a WorkflowInstance.
func scanWorkflowInstance(row *sql.Row) (*workflow.WorkflowInstance, error) {
	var (
		inst             workflow.WorkflowInstance
		status           string
		instanceDataJSON string
		createdAt        string
		completedAt      sql.NullString
	)

	if err := row.Scan(
		&inst.ID,
		&inst.TenantID,
		&inst.WorkflowID,
		&inst.VersionID,
		&status,
		&inst.CurrentNodeID,
		&instanceDataJSON,
		&inst.TriggerData,
		&createdAt,
		&completedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	inst.Status = workflow.InstanceStatus(status)
	inst.CreatedAt = mustParseTime(createdAt)
	if completedAt.Valid && completedAt.String != "" {
		t := mustParseTime(completedAt.String)
		inst.CompletedAt = &t
	}

	if instanceDataJSON != "" && instanceDataJSON != "{}" {
		if err := json.Unmarshal([]byte(instanceDataJSON), &inst.InstanceData); err != nil {
			return nil, err
		}
	}
	if inst.InstanceData == nil {
		inst.InstanceData = make(map[string]interface{})
	}

	return &inst, nil
}

// scanNodeExecution scans a row from node_executions into a NodeExecution.
func scanNodeExecution(rows *sql.Rows) (*workflow.NodeExecution, error) {
	var (
		exec        workflow.NodeExecution
		status      string
		nodeType    sql.NullString
		startedAt   string
		completedAt sql.NullString
		resultJSON  sql.NullString
	)

	if err := rows.Scan(
		&exec.ID,
		&exec.InstanceID,
		&exec.NodeID,
		&nodeType,
		&status,
		&startedAt,
		&completedAt,
		&resultJSON,
		&exec.FailReason,
	); err != nil {
		return nil, err
	}

	exec.Status = workflow.NodeStatus(status)
	if nodeType.Valid {
		exec.NodeType = workflow.NodeType(nodeType.String)
	}
	exec.StartedAt = mustParseTime(startedAt)
	if completedAt.Valid && completedAt.String != "" {
		t := mustParseTime(completedAt.String)
		exec.CompletedAt = &t
	}
	if resultJSON.Valid && resultJSON.String != "" {
		exec.Result = json.RawMessage(resultJSON.String)
	}

	return &exec, nil
}

// formatNullableTime formats a *time.Time as an ISO 8601 string for SQLite,
// or returns nil if the pointer is nil.
func formatNullableTime(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}
