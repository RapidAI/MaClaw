package repo

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/colleagues/domain"
)

// ColleagueRepo provides persistence for colleagues.
type ColleagueRepo struct {
	write *sql.DB
	read  *sql.DB
}

// New creates a ColleagueRepo.
func New(write, read *sql.DB) *ColleagueRepo {
	return &ColleagueRepo{write: write, read: read}
}

const cols = "id, name, avatar, role_id, description, strengths, tasks, status, created_at, updated_at"

// Insert creates a new colleague record.
func (r *ColleagueRepo) Insert(tenantID string, c *domain.Colleague) error {
	strengths, _ := json.Marshal(c.Strengths)
	tasks, _ := json.Marshal(c.Tasks)
	_, err := r.write.Exec(`INSERT INTO colleagues (`+cols+`, tenant_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.Name, c.Avatar, c.RoleID, c.Description,
		string(strengths), string(tasks), c.Status,
		c.CreatedAt.Format(time.RFC3339), c.UpdatedAt.Format(time.RFC3339),
		tenantID)
	return err
}

// Update modifies an existing colleague.
func (r *ColleagueRepo) Update(tenantID string, c *domain.Colleague) error {
	strengths, _ := json.Marshal(c.Strengths)
	tasks, _ := json.Marshal(c.Tasks)
	res, err := r.write.Exec(`UPDATE colleagues SET name=?, avatar=?, role_id=?, description=?, strengths=?, tasks=?, status=?, updated_at=? WHERE id=? AND tenant_id=?`,
		c.Name, c.Avatar, c.RoleID, c.Description,
		string(strengths), string(tasks), c.Status,
		c.UpdatedAt.Format(time.RFC3339), c.ID, tenantID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("colleague %s not found", c.ID)
	}
	return nil
}

// UpdateRoleID changes only the role_id of a colleague.
func (r *ColleagueRepo) UpdateRoleID(tenantID string, id, roleID string) error {
	res, err := r.write.Exec("UPDATE colleagues SET role_id=?, updated_at=? WHERE id=? AND tenant_id=?",
		roleID, time.Now().Format(time.RFC3339), id, tenantID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("colleague %s not found", id)
	}
	return nil
}

// GetByID returns a single colleague by ID.
func (r *ColleagueRepo) GetByID(tenantID string, id string) (*domain.Colleague, error) {
	row := r.read.QueryRow("SELECT "+cols+" FROM colleagues WHERE id=? AND tenant_id=?", id, tenantID)
	return scanColleague(row)
}

// List returns all colleagues ordered by name.
func (r *ColleagueRepo) List(tenantID string) ([]*domain.Colleague, error) {
	return r.queryMany("SELECT "+cols+" FROM colleagues WHERE tenant_id=? ORDER BY name", tenantID)
}

// ListActive returns only active colleagues.
func (r *ColleagueRepo) ListActive(tenantID string) ([]*domain.Colleague, error) {
	return r.queryMany("SELECT "+cols+" FROM colleagues WHERE status='active' AND tenant_id=? ORDER BY name", tenantID)
}

// ListByRoleID returns colleagues assigned to a specific role.
func (r *ColleagueRepo) ListByRoleID(tenantID string, roleID string) ([]*domain.Colleague, error) {
	rows, err := r.read.Query("SELECT "+cols+" FROM colleagues WHERE role_id=? AND status='active' AND tenant_id=? ORDER BY name", roleID, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMany(rows)
}

// ListByRoleCode returns active colleagues whose role matches the given role code.
func (r *ColleagueRepo) ListByRoleCode(tenantID string, roleCode string) ([]*domain.Colleague, error) {
	rows, err := r.read.Query(`SELECT c.id, c.name, c.avatar, c.role_id, c.description, c.strengths, c.tasks, c.status, c.created_at, c.updated_at
		FROM colleagues c JOIN roles r ON c.role_id = r.id
		WHERE r.code=? AND c.status='active' AND c.tenant_id=? ORDER BY c.name`, roleCode, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMany(rows)
}

// UpdateStatus changes the status of a colleague.
func (r *ColleagueRepo) UpdateStatus(tenantID string, id, status string) error {
	res, err := r.write.Exec("UPDATE colleagues SET status=?, updated_at=? WHERE id=? AND tenant_id=?",
		status, time.Now().Format(time.RFC3339), id, tenantID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("colleague %s not found", id)
	}
	return nil
}

func (r *ColleagueRepo) queryMany(query string, args ...any) ([]*domain.Colleague, error) {
	rows, err := r.read.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMany(rows)
}

func scanMany(rows *sql.Rows) ([]*domain.Colleague, error) {
	var result []*domain.Colleague
	for rows.Next() {
		c, err := scanColleagueRows(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

func scanColleague(row *sql.Row) (*domain.Colleague, error) {
	var c domain.Colleague
	var strengths, tasks, createdAt, updatedAt string
	if err := row.Scan(&c.ID, &c.Name, &c.Avatar, &c.RoleID, &c.Description, &strengths, &tasks, &c.Status, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	fillJSON(&c, strengths, tasks, createdAt, updatedAt)
	return &c, nil
}

func scanColleagueRows(rows *sql.Rows) (*domain.Colleague, error) {
	var c domain.Colleague
	var strengths, tasks, createdAt, updatedAt string
	if err := rows.Scan(&c.ID, &c.Name, &c.Avatar, &c.RoleID, &c.Description, &strengths, &tasks, &c.Status, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	fillJSON(&c, strengths, tasks, createdAt, updatedAt)
	return &c, nil
}

func fillJSON(c *domain.Colleague, strengths, tasks, createdAt, updatedAt string) {
	_ = json.Unmarshal([]byte(strengths), &c.Strengths)
	_ = json.Unmarshal([]byte(tasks), &c.Tasks)
	c.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	c.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	if c.Strengths == nil {
		c.Strengths = []string{}
	}
	if c.Tasks == nil {
		c.Tasks = []string{}
	}
}
