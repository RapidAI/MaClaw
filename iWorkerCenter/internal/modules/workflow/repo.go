package workflow

import (
	"database/sql"
	"fmt"
	"time"
)

// Repo provides persistence for workflow definitions, instances, steps, and events.
type Repo struct {
	write *sql.DB
	read  *sql.DB
}

// NewRepo creates a Repo.
func NewRepo(write, read *sql.DB) *Repo {
	return &Repo{write: write, read: read}
}

// --- Definition CRUD ---

func (r *Repo) InsertDefinition(tenantID string, d *Definition) error {
	_, err := r.write.Exec(`INSERT INTO workflow_definitions (tenant_id, id, name, description, trigger_type, status, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?)`, tenantID, d.ID, d.Name, d.Description, d.TriggerType, d.Status,
		d.CreatedAt.Format(time.RFC3339), d.UpdatedAt.Format(time.RFC3339))
	return err
}

func (r *Repo) InsertDefinitionTx(tenantID string, tx *sql.Tx, d *Definition) error {
	_, err := tx.Exec(`INSERT INTO workflow_definitions (tenant_id, id, name, description, trigger_type, status, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?)`, tenantID, d.ID, d.Name, d.Description, d.TriggerType, d.Status,
		d.CreatedAt.Format(time.RFC3339), d.UpdatedAt.Format(time.RFC3339))
	return err
}

func (r *Repo) UpdateDefinition(tenantID string, d *Definition) error {
	res, err := r.write.Exec(`UPDATE workflow_definitions SET name=?, description=?, trigger_type=?, status=?, updated_at=? WHERE id=? AND tenant_id=?`,
		d.Name, d.Description, d.TriggerType, d.Status, d.UpdatedAt.Format(time.RFC3339), d.ID, tenantID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("definition %s not found", d.ID)
	}
	return nil
}

func (r *Repo) GetDefinition(tenantID string, id string) (*Definition, error) {
	row := r.read.QueryRow(`SELECT id, name, description, trigger_type, status, created_at, updated_at FROM workflow_definitions WHERE id=? AND tenant_id=?`, id, tenantID)
	return scanDefinition(row)
}

func (r *Repo) ListDefinitions(tenantID string) ([]*Definition, error) {
	rows, err := r.read.Query(`SELECT id, name, description, trigger_type, status, created_at, updated_at FROM workflow_definitions WHERE tenant_id=? ORDER BY created_at DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*Definition
	for rows.Next() {
		var d Definition
		var ca, ua string
		if err := rows.Scan(&d.ID, &d.Name, &d.Description, &d.TriggerType, &d.Status, &ca, &ua); err != nil {
			return nil, err
		}
		if err := setDefinitionTimes(&d, ca, ua); err != nil {
			return nil, err
		}
		result = append(result, &d)
	}
	return result, rows.Err()
}

// --- Step Definition CRUD ---

func (r *Repo) InsertStepDefinition(tenantID string, s *StepDefinition) error {
	_, err := r.write.Exec(`INSERT INTO workflow_step_definitions
		(tenant_id, id, workflow_id, step_code, step_name, step_type, assignee_mode, assignee_role_code, assignee_colleague_id, timeout_minutes, reject_rule, sort_order)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		tenantID, s.ID, s.WorkflowID, s.StepCode, s.StepName, s.StepType,
		s.AssigneeMode, s.AssigneeRoleCode, s.AssigneeColleagueID,
		s.TimeoutMinutes, s.RejectRule, s.SortOrder)
	return err
}

func (r *Repo) InsertStepDefinitionTx(tenantID string, tx *sql.Tx, s *StepDefinition) error {
	_, err := tx.Exec(`INSERT INTO workflow_step_definitions
		(tenant_id, id, workflow_id, step_code, step_name, step_type, assignee_mode, assignee_role_code, assignee_colleague_id, timeout_minutes, reject_rule, sort_order)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		tenantID, s.ID, s.WorkflowID, s.StepCode, s.StepName, s.StepType,
		s.AssigneeMode, s.AssigneeRoleCode, s.AssigneeColleagueID,
		s.TimeoutMinutes, s.RejectRule, s.SortOrder)
	return err
}

func (r *Repo) ListStepDefinitions(tenantID string, workflowID string) ([]*StepDefinition, error) {
	rows, err := r.read.Query(`SELECT id, workflow_id, step_code, step_name, step_type, assignee_mode,
		assignee_role_code, assignee_colleague_id, timeout_minutes, reject_rule, sort_order
		FROM workflow_step_definitions WHERE workflow_id=? AND tenant_id=? ORDER BY sort_order`, workflowID, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*StepDefinition
	for rows.Next() {
		var s StepDefinition
		if err := rows.Scan(&s.ID, &s.WorkflowID, &s.StepCode, &s.StepName, &s.StepType,
			&s.AssigneeMode, &s.AssigneeRoleCode, &s.AssigneeColleagueID,
			&s.TimeoutMinutes, &s.RejectRule, &s.SortOrder); err != nil {
			return nil, err
		}
		result = append(result, &s)
	}
	return result, rows.Err()
}

func (r *Repo) GetStepDefinition(tenantID string, id string) (*StepDefinition, error) {
	row := r.read.QueryRow(`SELECT id, workflow_id, step_code, step_name, step_type, assignee_mode,
		assignee_role_code, assignee_colleague_id, timeout_minutes, reject_rule, sort_order
		FROM workflow_step_definitions WHERE id=? AND tenant_id=?`, id, tenantID)
	var s StepDefinition
	if err := row.Scan(&s.ID, &s.WorkflowID, &s.StepCode, &s.StepName, &s.StepType,
		&s.AssigneeMode, &s.AssigneeRoleCode, &s.AssigneeColleagueID,
		&s.TimeoutMinutes, &s.RejectRule, &s.SortOrder); err != nil {
		return nil, err
	}
	return &s, nil
}

// --- Instance CRUD ---

func (r *Repo) InsertInstanceTx(tenantID string, tx *sql.Tx, inst *Instance) error {
	_, err := tx.Exec(`INSERT INTO workflow_instances (tenant_id, id, definition_id, title, initiator_id, current_step_id, status, input_data, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`, tenantID, inst.ID, inst.DefinitionID, inst.Title, inst.InitiatorID,
		inst.CurrentStepID, inst.Status, inst.InputData,
		inst.CreatedAt.Format(time.RFC3339), inst.UpdatedAt.Format(time.RFC3339))
	return err
}

func (r *Repo) UpdateInstanceTx(tenantID string, tx *sql.Tx, inst *Instance) error {
	_, err := tx.Exec(`UPDATE workflow_instances SET current_step_id=?, status=?, updated_at=? WHERE id=? AND tenant_id=?`,
		inst.CurrentStepID, inst.Status, inst.UpdatedAt.Format(time.RFC3339), inst.ID, tenantID)
	return err
}

func (r *Repo) GetInstance(tenantID string, id string) (*Instance, error) {
	row := r.read.QueryRow(`SELECT wi.id, wi.definition_id, wi.title, wi.initiator_id, wi.current_step_id, COALESCE(si.assignee_colleague_id,''), wi.status, wi.input_data, wi.created_at, wi.updated_at
		FROM workflow_instances wi
		LEFT JOIN workflow_step_instances si ON si.tenant_id=wi.tenant_id AND si.id=wi.current_step_id
		WHERE wi.id=? AND wi.tenant_id=?`, id, tenantID)
	return scanInstance(row)
}

func (r *Repo) ListInstances(tenantID string) ([]*Instance, error) {
	rows, err := r.read.Query(`SELECT wi.id, wi.definition_id, wi.title, wi.initiator_id, wi.current_step_id, COALESCE(si.assignee_colleague_id,''), wi.status, wi.input_data, wi.created_at, wi.updated_at
		FROM workflow_instances wi
		LEFT JOIN workflow_step_instances si ON si.tenant_id=wi.tenant_id AND si.id=wi.current_step_id
		WHERE wi.tenant_id=? ORDER BY wi.created_at DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*Instance
	for rows.Next() {
		var inst Instance
		var ca, ua string
		if err := rows.Scan(&inst.ID, &inst.DefinitionID, &inst.Title, &inst.InitiatorID,
			&inst.CurrentStepID, &inst.CurrentStepAssigneeColleagueID, &inst.Status, &inst.InputData, &ca, &ua); err != nil {
			return nil, err
		}
		if err := setInstanceTimes(&inst, ca, ua); err != nil {
			return nil, err
		}
		result = append(result, &inst)
	}
	return result, rows.Err()
}

func (r *Repo) ListInstancesForColleague(tenantID string, colleagueID string) ([]*Instance, error) {
	rows, err := r.read.Query(`SELECT DISTINCT wi.id, wi.definition_id, wi.title, wi.initiator_id, wi.current_step_id, COALESCE(current_si.assignee_colleague_id,''), wi.status, wi.input_data, wi.created_at, wi.updated_at
		FROM workflow_instances wi
		LEFT JOIN workflow_step_instances si ON si.tenant_id=wi.tenant_id AND si.instance_id=wi.id
		LEFT JOIN workflow_step_instances current_si ON current_si.tenant_id=wi.tenant_id AND current_si.id=wi.current_step_id
		WHERE wi.tenant_id=? AND (wi.initiator_id=? OR si.assignee_colleague_id=?)
		ORDER BY wi.created_at DESC`, tenantID, colleagueID, colleagueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*Instance
	for rows.Next() {
		var inst Instance
		var ca, ua string
		if err := rows.Scan(&inst.ID, &inst.DefinitionID, &inst.Title, &inst.InitiatorID,
			&inst.CurrentStepID, &inst.CurrentStepAssigneeColleagueID, &inst.Status, &inst.InputData, &ca, &ua); err != nil {
			return nil, err
		}
		if err := setInstanceTimes(&inst, ca, ua); err != nil {
			return nil, err
		}
		result = append(result, &inst)
	}
	return result, rows.Err()
}

// --- Step Instance CRUD ---

func (r *Repo) InsertStepInstanceTx(tenantID string, tx *sql.Tx, si *StepInstance) error {
	_, err := tx.Exec(`INSERT INTO workflow_step_instances
		(tenant_id, id, instance_id, step_definition_id, assignee_colleague_id, collaboration_task_id, status, result, sort_order, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		tenantID, si.ID, si.InstanceID, si.StepDefinitionID, si.AssigneeColleagueID,
		si.CollaborationTaskID, si.Status, si.Result, si.SortOrder,
		si.CreatedAt.Format(time.RFC3339), si.UpdatedAt.Format(time.RFC3339))
	return err
}

func (r *Repo) UpdateStepInstanceTx(tenantID string, tx *sql.Tx, si *StepInstance) error {
	_, err := tx.Exec(`UPDATE workflow_step_instances SET status=?, result=?, collaboration_task_id=?, updated_at=? WHERE id=? AND tenant_id=?`,
		si.Status, si.Result, si.CollaborationTaskID, si.UpdatedAt.Format(time.RFC3339), si.ID, tenantID)
	return err
}

func (r *Repo) GetStepInstance(tenantID string, id string) (*StepInstance, error) {
	row := r.read.QueryRow(`SELECT id, instance_id, step_definition_id, assignee_colleague_id, collaboration_task_id, status, result, sort_order, created_at, updated_at
		FROM workflow_step_instances WHERE id=? AND tenant_id=?`, id, tenantID)
	return scanStepInstance(row)
}

func (r *Repo) ListStepInstances(tenantID string, instanceID string) ([]*StepInstance, error) {
	rows, err := r.read.Query(`SELECT id, instance_id, step_definition_id, assignee_colleague_id, collaboration_task_id, status, result, sort_order, created_at, updated_at
		FROM workflow_step_instances WHERE instance_id=? AND tenant_id=? ORDER BY sort_order`, instanceID, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*StepInstance
	for rows.Next() {
		si, err := scanStepInstanceRows(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, si)
	}
	return result, rows.Err()
}

// --- Instance Events ---

func (r *Repo) InsertEventTx(tenantID string, tx *sql.Tx, e *InstanceEvent) error {
	_, err := tx.Exec(`INSERT INTO workflow_instance_events (tenant_id, id, instance_id, step_id, event, actor_id, note, created_at)
		VALUES (?,?,?,?,?,?,?,?)`, tenantID, e.ID, e.InstanceID, e.StepID, e.Event, e.ActorID, e.Note, e.CreatedAt.Format(time.RFC3339))
	return err
}

func (r *Repo) ListEvents(tenantID string, instanceID string) ([]*InstanceEvent, error) {
	rows, err := r.read.Query(`SELECT id, instance_id, step_id, event, actor_id, note, created_at
		FROM workflow_instance_events WHERE instance_id=? AND tenant_id=? ORDER BY created_at`, instanceID, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*InstanceEvent
	for rows.Next() {
		var e InstanceEvent
		var ca string
		if err := rows.Scan(&e.ID, &e.InstanceID, &e.StepID, &e.Event, &e.ActorID, &e.Note, &ca); err != nil {
			return nil, err
		}
		createdAt, err := time.Parse(time.RFC3339, ca)
		if err != nil {
			return nil, fmt.Errorf("parse workflow event %s created_at: %w", e.ID, err)
		}
		e.CreatedAt = createdAt
		result = append(result, &e)
	}
	return result, rows.Err()
}

// --- scan helpers ---

func scanDefinition(row *sql.Row) (*Definition, error) {
	var d Definition
	var ca, ua string
	if err := row.Scan(&d.ID, &d.Name, &d.Description, &d.TriggerType, &d.Status, &ca, &ua); err != nil {
		return nil, err
	}
	if err := setDefinitionTimes(&d, ca, ua); err != nil {
		return nil, err
	}
	return &d, nil
}

func scanInstance(row *sql.Row) (*Instance, error) {
	var inst Instance
	var ca, ua string
	if err := row.Scan(&inst.ID, &inst.DefinitionID, &inst.Title, &inst.InitiatorID,
		&inst.CurrentStepID, &inst.CurrentStepAssigneeColleagueID, &inst.Status, &inst.InputData, &ca, &ua); err != nil {
		return nil, err
	}
	if err := setInstanceTimes(&inst, ca, ua); err != nil {
		return nil, err
	}
	return &inst, nil
}

func scanStepInstance(row *sql.Row) (*StepInstance, error) {
	var si StepInstance
	var ca, ua string
	if err := row.Scan(&si.ID, &si.InstanceID, &si.StepDefinitionID, &si.AssigneeColleagueID,
		&si.CollaborationTaskID, &si.Status, &si.Result, &si.SortOrder, &ca, &ua); err != nil {
		return nil, err
	}
	if err := setStepInstanceTimes(&si, ca, ua); err != nil {
		return nil, err
	}
	return &si, nil
}

func scanStepInstanceRows(rows *sql.Rows) (*StepInstance, error) {
	var si StepInstance
	var ca, ua string
	if err := rows.Scan(&si.ID, &si.InstanceID, &si.StepDefinitionID, &si.AssigneeColleagueID,
		&si.CollaborationTaskID, &si.Status, &si.Result, &si.SortOrder, &ca, &ua); err != nil {
		return nil, err
	}
	if err := setStepInstanceTimes(&si, ca, ua); err != nil {
		return nil, err
	}
	return &si, nil
}

func setDefinitionTimes(d *Definition, createdAtRaw, updatedAtRaw string) error {
	createdAt, err := time.Parse(time.RFC3339, createdAtRaw)
	if err != nil {
		return fmt.Errorf("parse workflow definition %s created_at: %w", d.ID, err)
	}
	updatedAt, err := time.Parse(time.RFC3339, updatedAtRaw)
	if err != nil {
		return fmt.Errorf("parse workflow definition %s updated_at: %w", d.ID, err)
	}
	d.CreatedAt = createdAt
	d.UpdatedAt = updatedAt
	return nil
}

func setInstanceTimes(inst *Instance, createdAtRaw, updatedAtRaw string) error {
	createdAt, err := time.Parse(time.RFC3339, createdAtRaw)
	if err != nil {
		return fmt.Errorf("parse workflow instance %s created_at: %w", inst.ID, err)
	}
	updatedAt, err := time.Parse(time.RFC3339, updatedAtRaw)
	if err != nil {
		return fmt.Errorf("parse workflow instance %s updated_at: %w", inst.ID, err)
	}
	inst.CreatedAt = createdAt
	inst.UpdatedAt = updatedAt
	return nil
}

func setStepInstanceTimes(si *StepInstance, createdAtRaw, updatedAtRaw string) error {
	createdAt, err := time.Parse(time.RFC3339, createdAtRaw)
	if err != nil {
		return fmt.Errorf("parse workflow step instance %s created_at: %w", si.ID, err)
	}
	updatedAt, err := time.Parse(time.RFC3339, updatedAtRaw)
	if err != nil {
		return fmt.Errorf("parse workflow step instance %s updated_at: %w", si.ID, err)
	}
	si.CreatedAt = createdAt
	si.UpdatedAt = updatedAt
	return nil
}
