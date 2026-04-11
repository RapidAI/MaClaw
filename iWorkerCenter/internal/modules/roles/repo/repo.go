package repo

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/roles/domain"
)

// RoleRepo provides persistence for roles and assignment logs.
type RoleRepo struct {
	write *sql.DB
	read  *sql.DB
}

// New creates a RoleRepo.
func New(write, read *sql.DB) *RoleRepo {
	return &RoleRepo{write: write, read: read}
}

// Insert creates a new role.
func (r *RoleRepo) Insert(role *domain.Role) error {
	strengths, _ := json.Marshal(role.DefaultStrengths)
	tasks, _ := json.Marshal(role.ApplicableTasks)
	_, err := r.write.Exec(`INSERT INTO roles (id, name, code, description, default_strengths, applicable_tasks, status, sort_order, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		role.ID, role.Name, role.Code, role.Description,
		string(strengths), string(tasks), role.Status, role.SortOrder,
		role.CreatedAt.Format(time.RFC3339), role.UpdatedAt.Format(time.RFC3339))
	return err
}

// Update modifies an existing role.
func (r *RoleRepo) Update(role *domain.Role) error {
	strengths, _ := json.Marshal(role.DefaultStrengths)
	tasks, _ := json.Marshal(role.ApplicableTasks)
	res, err := r.write.Exec(`UPDATE roles SET name=?, code=?, description=?, default_strengths=?, applicable_tasks=?, status=?, sort_order=?, updated_at=? WHERE id=?`,
		role.Name, role.Code, role.Description,
		string(strengths), string(tasks), role.Status, role.SortOrder,
		role.UpdatedAt.Format(time.RFC3339), role.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("role %s not found", role.ID)
	}
	return nil
}

// GetByID returns a role by ID.
func (r *RoleRepo) GetByID(id string) (*domain.Role, error) {
	row := r.read.QueryRow("SELECT id, name, code, description, default_strengths, applicable_tasks, status, sort_order, created_at, updated_at FROM roles WHERE id=?", id)
	return scanRole(row)
}

// GetByCode returns a role by its unique code.
func (r *RoleRepo) GetByCode(code string) (*domain.Role, error) {
	row := r.read.QueryRow("SELECT id, name, code, description, default_strengths, applicable_tasks, status, sort_order, created_at, updated_at FROM roles WHERE code=?", code)
	return scanRole(row)
}

// List returns all roles ordered by sort_order.
func (r *RoleRepo) List() ([]*domain.Role, error) {
	rows, err := r.read.Query("SELECT id, name, code, description, default_strengths, applicable_tasks, status, sort_order, created_at, updated_at FROM roles ORDER BY sort_order, name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.Role
	for rows.Next() {
		role, err := scanRoleRows(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, role)
	}
	return result, rows.Err()
}

// ListActive returns only active roles.
func (r *RoleRepo) ListActive() ([]*domain.Role, error) {
	rows, err := r.read.Query("SELECT id, name, code, description, default_strengths, applicable_tasks, status, sort_order, created_at, updated_at FROM roles WHERE status='active' ORDER BY sort_order, name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.Role
	for rows.Next() {
		role, err := scanRoleRows(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, role)
	}
	return result, rows.Err()
}

// UpdateStatus changes a role's status.
func (r *RoleRepo) UpdateStatus(id, status string) error {
	res, err := r.write.Exec("UPDATE roles SET status=?, updated_at=? WHERE id=?",
		status, time.Now().Format(time.RFC3339), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("role %s not found", id)
	}
	return nil
}

// InsertAssignmentLog records a role assignment change.
func (r *RoleRepo) InsertAssignmentLog(log *domain.RoleAssignmentLog) error {
	_, err := r.write.Exec(`INSERT INTO role_assignment_log (id, colleague_id, old_role_id, new_role_id, reason, assigned_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		log.ID, log.ColleagueID, log.OldRoleID, log.NewRoleID, log.Reason,
		log.AssignedAt.Format(time.RFC3339))
	return err
}

// ListAssignmentLogs returns assignment history for a colleague.
func (r *RoleRepo) ListAssignmentLogs(colleagueID string) ([]*domain.RoleAssignmentLog, error) {
	rows, err := r.read.Query("SELECT id, colleague_id, old_role_id, new_role_id, reason, assigned_at FROM role_assignment_log WHERE colleague_id=? ORDER BY assigned_at DESC", colleagueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.RoleAssignmentLog
	for rows.Next() {
		var l domain.RoleAssignmentLog
		var at string
		if err := rows.Scan(&l.ID, &l.ColleagueID, &l.OldRoleID, &l.NewRoleID, &l.Reason, &at); err != nil {
			return nil, err
		}
		l.AssignedAt, _ = time.Parse(time.RFC3339, at)
		result = append(result, &l)
	}
	return result, rows.Err()
}

// --- scan helpers ---

func scanRole(row *sql.Row) (*domain.Role, error) {
	var r domain.Role
	var strengths, tasks, createdAt, updatedAt string
	if err := row.Scan(&r.ID, &r.Name, &r.Code, &r.Description, &strengths, &tasks, &r.Status, &r.SortOrder, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(strengths), &r.DefaultStrengths)
	_ = json.Unmarshal([]byte(tasks), &r.ApplicableTasks)
	r.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	r.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	if r.DefaultStrengths == nil {
		r.DefaultStrengths = []string{}
	}
	if r.ApplicableTasks == nil {
		r.ApplicableTasks = []string{}
	}
	return &r, nil
}

func scanRoleRows(rows *sql.Rows) (*domain.Role, error) {
	var r domain.Role
	var strengths, tasks, createdAt, updatedAt string
	if err := rows.Scan(&r.ID, &r.Name, &r.Code, &r.Description, &strengths, &tasks, &r.Status, &r.SortOrder, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(strengths), &r.DefaultStrengths)
	_ = json.Unmarshal([]byte(tasks), &r.ApplicableTasks)
	r.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	r.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	if r.DefaultStrengths == nil {
		r.DefaultStrengths = []string{}
	}
	if r.ApplicableTasks == nil {
		r.ApplicableTasks = []string{}
	}
	return &r, nil
}
