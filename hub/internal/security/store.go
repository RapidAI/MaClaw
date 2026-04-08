package security

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// SecurityStore provides SQLite-backed persistence for security groups,
// group members, and group policies.
type SecurityStore struct {
	db *sql.DB
}

// NewSecurityStore creates a new SecurityStore using the given database connection.
func NewSecurityStore(db *sql.DB) *SecurityStore {
	return &SecurityStore{db: db}
}

// InitSchema creates the security-related tables and indexes if they don't exist.
func (s *SecurityStore) InitSchema(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS security_groups (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			parent_id TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_security_groups_parent ON security_groups(parent_id)`,
		`CREATE TABLE IF NOT EXISTS security_group_members (
			email TEXT PRIMARY KEY,
			group_id TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sgm_group ON security_group_members(group_id)`,
		`CREATE TABLE IF NOT EXISTS security_policies (
			group_id TEXT PRIMARY KEY,
			policy_json TEXT NOT NULL DEFAULT '{}',
			updated_at TEXT NOT NULL
		)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("init security schema: %w", err)
		}
	}
	return nil
}

// InitRootGroup creates the root group ("全局") if it doesn't already exist.
// This is idempotent — calling it multiple times is safe.
func (s *SecurityStore) InitRootGroup(ctx context.Context) error {
	// Check if a root group already exists (parent_id = '')
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM security_groups WHERE parent_id = ''`).Scan(&count)
	if err != nil {
		return fmt.Errorf("check root group: %w", err)
	}
	if count > 0 {
		return nil
	}

	now := time.Now().UTC().Format(time.RFC3339)
	id := uuid.New().String()
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO security_groups (id, name, parent_id, created_at, updated_at)
		 VALUES (?, ?, '', ?, ?)`,
		id, "全局", now, now)
	if err != nil {
		return fmt.Errorf("create root group: %w", err)
	}
	return nil
}

// CreateGroup inserts a new security group.
func (s *SecurityStore) CreateGroup(ctx context.Context, group *SecurityGroup) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO security_groups (id, name, parent_id, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)`,
		group.ID, group.Name, group.ParentID,
		group.CreatedAt.UTC().Format(time.RFC3339),
		group.UpdatedAt.UTC().Format(time.RFC3339))
	return err
}

// GetGroupByID retrieves a security group by its ID. Returns nil if not found.
func (s *SecurityStore) GetGroupByID(ctx context.Context, id string) (*SecurityGroup, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, parent_id, created_at, updated_at FROM security_groups WHERE id = ?`, id)
	return scanGroup(row)
}

// ListGroups returns all security groups.
func (s *SecurityStore) ListGroups(ctx context.Context) ([]*SecurityGroup, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, parent_id, created_at, updated_at FROM security_groups ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanGroups(rows)
}

// UpdateGroupName changes the name of a security group.
func (s *SecurityStore) UpdateGroupName(ctx context.Context, id, name string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`UPDATE security_groups SET name = ?, updated_at = ? WHERE id = ?`,
		name, now, id)
	return err
}

// DeleteGroup removes a security group by ID.
func (s *SecurityStore) DeleteGroup(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM security_groups WHERE id = ?`, id)
	if err != nil {
		return err
	}
	// Also clean up the policy for this group
	_, err = s.db.ExecContext(ctx, `DELETE FROM security_policies WHERE group_id = ?`, id)
	return err
}

// GetRootGroup returns the root group (the one with empty parent_id).
func (s *SecurityStore) GetRootGroup(ctx context.Context) (*SecurityGroup, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, parent_id, created_at, updated_at FROM security_groups WHERE parent_id = '' LIMIT 1`)
	return scanGroup(row)
}

// GetGroupDepth calculates the depth of a group in the tree by walking
// the parent_id chain up to the root. Root group has depth 0.
func (s *SecurityStore) GetGroupDepth(ctx context.Context, groupID string) (int, error) {
	depth := 0
	currentID := groupID
	for {
		var parentID string
		err := s.db.QueryRowContext(ctx,
			`SELECT parent_id FROM security_groups WHERE id = ?`, currentID).Scan(&parentID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return 0, fmt.Errorf("group not found: %s", currentID)
			}
			return 0, err
		}
		if parentID == "" {
			// Reached root
			return depth, nil
		}
		depth++
		currentID = parentID
	}
}

// --- Group Members ---

// AssignUser assigns a user (by email) to a group. Uses UPSERT since email is PRIMARY KEY.
func (s *SecurityStore) AssignUser(ctx context.Context, email, groupID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO security_group_members (email, group_id, created_at)
		 VALUES (?, ?, ?)`,
		email, groupID, now)
	return err
}

// RemoveUser removes a user from their assigned group.
func (s *SecurityStore) RemoveUser(ctx context.Context, email string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM security_group_members WHERE email = ?`, email)
	return err
}

// GetUserGroup returns the group ID the user is assigned to.
// Returns empty string if the user is not assigned to any group.
func (s *SecurityStore) GetUserGroup(ctx context.Context, email string) (string, error) {
	var groupID string
	err := s.db.QueryRowContext(ctx,
		`SELECT group_id FROM security_group_members WHERE email = ?`, email).Scan(&groupID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return groupID, nil
}

// ListGroupMembers returns all member emails for a given group.
func (s *SecurityStore) ListGroupMembers(ctx context.Context, groupID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT email FROM security_group_members WHERE group_id = ? ORDER BY created_at`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var emails []string
	for rows.Next() {
		var email string
		if err := rows.Scan(&email); err != nil {
			return nil, err
		}
		emails = append(emails, email)
	}
	return emails, rows.Err()
}

// CountGroupMembers returns the number of members in a group.
func (s *SecurityStore) CountGroupMembers(ctx context.Context, groupID string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM security_group_members WHERE group_id = ?`, groupID).Scan(&count)
	return count, err
}

// CountGroupMembersMap returns member counts for all groups in one query.
func (s *SecurityStore) CountGroupMembersMap(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT group_id, COUNT(*) FROM security_group_members GROUP BY group_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var groupID string
		var count int
		if err := rows.Scan(&groupID, &count); err != nil {
			return nil, err
		}
		counts[groupID] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return counts, nil
}

// ListAllAssignedEmails returns all emails that have a record in security_group_members.
func (s *SecurityStore) ListAllAssignedEmails(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT email FROM security_group_members`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var emails []string
	for rows.Next() {
		var email string
		if err := rows.Scan(&email); err != nil {
			return nil, err
		}
		emails = append(emails, email)
	}
	return emails, rows.Err()
}

// MoveUsersToRoot moves all members from the given group IDs to the root group.
func (s *SecurityStore) MoveUsersToRoot(ctx context.Context, fromGroupIDs []string, rootGroupID string) error {
	if len(fromGroupIDs) == 0 {
		return nil
	}
	placeholders := make([]string, len(fromGroupIDs))
	args := make([]any, 0, len(fromGroupIDs)+1)
	args = append(args, rootGroupID)
	for i, id := range fromGroupIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	query := fmt.Sprintf(
		`UPDATE security_group_members SET group_id = ? WHERE group_id IN (%s)`,
		strings.Join(placeholders, ","))
	_, err := s.db.ExecContext(ctx, query, args...)
	return err
}

// --- Group Policies ---

// GetGroupPolicy returns the sparse policy JSON for a group as a map.
// Returns an empty map if no policy is set.
func (s *SecurityStore) GetGroupPolicy(ctx context.Context, groupID string) (map[string]interface{}, error) {
	var policyJSON string
	err := s.db.QueryRowContext(ctx,
		`SELECT policy_json FROM security_policies WHERE group_id = ?`, groupID).Scan(&policyJSON)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return map[string]interface{}{}, nil
		}
		return nil, err
	}
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(policyJSON), &result); err != nil {
		return nil, fmt.Errorf("unmarshal policy json: %w", err)
	}
	return result, nil
}

// SetGroupPolicy stores the sparse policy JSON for a group.
// Uses UPSERT (INSERT OR REPLACE) since group_id is PRIMARY KEY.
func (s *SecurityStore) SetGroupPolicy(ctx context.Context, groupID string, policy map[string]interface{}) error {
	data, err := json.Marshal(policy)
	if err != nil {
		return fmt.Errorf("marshal policy json: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO security_policies (group_id, policy_json, updated_at)
		 VALUES (?, ?, ?)`,
		groupID, string(data), now)
	return err
}

// --- Helpers ---

func scanGroup(row *sql.Row) (*SecurityGroup, error) {
	var g SecurityGroup
	var createdAt, updatedAt string
	if err := row.Scan(&g.ID, &g.Name, &g.ParentID, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	g.CreatedAt = mustParseTime(createdAt)
	g.UpdatedAt = mustParseTime(updatedAt)
	return &g, nil
}

func scanGroups(rows *sql.Rows) ([]*SecurityGroup, error) {
	var result []*SecurityGroup
	for rows.Next() {
		var g SecurityGroup
		var createdAt, updatedAt string
		if err := rows.Scan(&g.ID, &g.Name, &g.ParentID, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		g.CreatedAt = mustParseTime(createdAt)
		g.UpdatedAt = mustParseTime(updatedAt)
		result = append(result, &g)
	}
	return result, rows.Err()
}

func mustParseTime(v string) time.Time {
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}
	}
	return t
}
