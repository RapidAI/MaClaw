package security

import (
	"database/sql"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/idgen"
)

// Repo provides persistence for security policies and hit records.
type Repo struct {
	write *sql.DB
	read  *sql.DB
}

// NewRepo creates a Repo.
func NewRepo(write, read *sql.DB) *Repo {
	return &Repo{write: write, read: read}
}

// ListActivePolicies returns all active policies ordered by priority desc.
func (r *Repo) ListActivePolicies(tenantID string) ([]Policy, error) {
	rows, err := r.read.Query(`SELECT id, name, policy_type, description, rules, scope, priority, status, created_at, updated_at
		FROM security_policies WHERE tenant_id=? AND status='active' ORDER BY priority DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPolicies(rows)
}

// ListAllPolicies returns all policies.
func (r *Repo) ListAllPolicies(tenantID string) ([]Policy, error) {
	rows, err := r.read.Query(`SELECT id, name, policy_type, description, rules, scope, priority, status, created_at, updated_at
		FROM security_policies WHERE tenant_id=? ORDER BY priority DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPolicies(rows)
}

// GetPolicy returns a single policy by ID.
func (r *Repo) GetPolicy(tenantID string, id string) (*Policy, error) {
	row := r.read.QueryRow(`SELECT id, name, policy_type, description, rules, scope, priority, status, created_at, updated_at
		FROM security_policies WHERE id=? AND tenant_id=?`, id, tenantID)
	var p Policy
	if err := row.Scan(&p.ID, &p.Name, &p.PolicyType, &p.Description, &p.Rules, &p.Scope, &p.Priority, &p.Status, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}
	return &p, nil
}

// InsertPolicy creates a new policy.
func (r *Repo) InsertPolicy(tenantID string, p *Policy) error {
	_, err := r.write.Exec(`INSERT INTO security_policies (id, tenant_id, name, policy_type, description, rules, scope, priority, status, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		p.ID, tenantID, p.Name, p.PolicyType, p.Description, p.Rules, p.Scope, p.Priority, p.Status, p.CreatedAt, p.UpdatedAt)
	return err
}

// UpdatePolicy updates an existing policy.
func (r *Repo) UpdatePolicy(tenantID string, p *Policy) error {
	res, err := r.write.Exec(`UPDATE security_policies SET name=?, policy_type=?, description=?, rules=?, scope=?, priority=?, status=?, updated_at=? WHERE id=? AND tenant_id=?`,
		p.Name, p.PolicyType, p.Description, p.Rules, p.Scope, p.Priority, p.Status, p.UpdatedAt, p.ID, tenantID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// RecordHit logs a policy hit.
func (r *Repo) RecordHit(tenantID string, policyID, policyName, actorID, action, detail string) error {
	now := time.Now().Format(time.RFC3339)
	id := idgen.New("hit")
	_, err := r.write.Exec(`INSERT INTO security_policy_hit_records (id, tenant_id, policy_id, policy_name, actor_id, action, detail, created_at)
		VALUES (?,?,?,?,?,?,?,?)`, id, tenantID, policyID, policyName, actorID, action, detail, now)
	return err
}

// ListRecentHits returns the most recent hit records.
func (r *Repo) ListRecentHits(tenantID string, limit int) ([]HitRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.read.Query(`SELECT id, policy_id, policy_name, actor_id, action, detail, created_at
		FROM security_policy_hit_records WHERE tenant_id=? ORDER BY created_at DESC LIMIT ?`, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []HitRecord
	for rows.Next() {
		var h HitRecord
		if err := rows.Scan(&h.ID, &h.PolicyID, &h.PolicyName, &h.ActorID, &h.Action, &h.Detail, &h.CreatedAt); err != nil {
			continue
		}
		result = append(result, h)
	}
	if result == nil {
		result = []HitRecord{}
	}
	return result, nil
}

func scanPolicies(rows *sql.Rows) ([]Policy, error) {
	var result []Policy
	for rows.Next() {
		var p Policy
		if err := rows.Scan(&p.ID, &p.Name, &p.PolicyType, &p.Description, &p.Rules, &p.Scope, &p.Priority, &p.Status, &p.CreatedAt, &p.UpdatedAt); err != nil {
			continue
		}
		result = append(result, p)
	}
	if result == nil {
		result = []Policy{}
	}
	return result, nil
}
