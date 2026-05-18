package security

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/google/uuid"
)

// SecurityStore provides SQLite-backed persistence for security groups,
// group members, and group policies.
type SecurityStore struct {
	db *sql.DB
}

type tenantContextKey struct{}

func WithTenant(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantContextKey{}, normalizeTenantID(tenantID))
}

func tenantIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return store.DefaultTenantID
	}
	if tenantID, ok := ctx.Value(tenantContextKey{}).(string); ok {
		return normalizeTenantID(tenantID)
	}
	return store.DefaultTenantID
}

func normalizeTenantID(tenantID string) string {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return store.DefaultTenantID
	}
	return tenantID
}

// NewSecurityStore creates a new SecurityStore using the given database connection.
func NewSecurityStore(db *sql.DB) *SecurityStore {
	return &SecurityStore{db: db}
}

// InitSchema creates the security-related tables and indexes if they don't exist.
func (s *SecurityStore) InitSchema(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS security_groups (
			tenant_id TEXT NOT NULL DEFAULT 'tenant_default',
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			parent_id TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_security_groups_parent ON security_groups(parent_id)`,
		`CREATE INDEX IF NOT EXISTS idx_security_groups_tenant_parent ON security_groups(tenant_id, parent_id)`,
		`CREATE TABLE IF NOT EXISTS security_group_members (
			tenant_id TEXT NOT NULL DEFAULT 'tenant_default',
			email TEXT PRIMARY KEY,
			group_id TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sgm_group ON security_group_members(group_id)`,
		`CREATE INDEX IF NOT EXISTS idx_sgm_tenant_group ON security_group_members(tenant_id, group_id)`,
		`CREATE TABLE IF NOT EXISTS security_policies (
			tenant_id TEXT NOT NULL DEFAULT 'tenant_default',
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
	for _, stmt := range []string{
		`ALTER TABLE security_groups ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default'`,
		`ALTER TABLE security_group_members ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default'`,
		`ALTER TABLE security_policies ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default'`,
	} {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return fmt.Errorf("migrate security tenant column: %w", err)
		}
	}
	return nil
}

// InitRootGroup creates the root group ("全局") if it doesn't already exist.
// This is idempotent — calling it multiple times is safe.
func (s *SecurityStore) InitRootGroup(ctx context.Context) error {
	tenantID := tenantIDFromContext(ctx)
	// Check if a root group already exists (parent_id = '')
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM security_groups WHERE tenant_id = ? AND parent_id = ''`, tenantID).Scan(&count)
	if err != nil {
		return fmt.Errorf("check root group: %w", err)
	}
	if count > 0 {
		return nil
	}

	now := time.Now().UTC().Format(time.RFC3339)
	id := uuid.New().String()
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO security_groups (tenant_id, id, name, parent_id, created_at, updated_at)
		 VALUES (?, ?, ?, '', ?, ?)`,
		tenantID, id, "全局", now, now)
	if err != nil {
		return fmt.Errorf("create root group: %w", err)
	}
	return nil
}

// CreateGroup inserts a new security group.
func (s *SecurityStore) CreateGroup(ctx context.Context, group *SecurityGroup) error {
	group.TenantID = tenantIDFromContext(ctx)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO security_groups (tenant_id, id, name, parent_id, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		group.TenantID, group.ID, group.Name, group.ParentID,
		group.CreatedAt.UTC().Format(time.RFC3339),
		group.UpdatedAt.UTC().Format(time.RFC3339))
	return err
}

// GetGroupByID retrieves a security group by its ID. Returns nil if not found.
func (s *SecurityStore) GetGroupByID(ctx context.Context, id string) (*SecurityGroup, error) {
	tenantID := tenantIDFromContext(ctx)
	row := s.db.QueryRowContext(ctx,
		`SELECT tenant_id, id, name, parent_id, created_at, updated_at FROM security_groups WHERE tenant_id = ? AND id = ?`, tenantID, id)
	return scanGroup(row)
}

// ListGroups returns all security groups.
func (s *SecurityStore) ListGroups(ctx context.Context) ([]*SecurityGroup, error) {
	tenantID := tenantIDFromContext(ctx)
	rows, err := s.db.QueryContext(ctx,
		`SELECT tenant_id, id, name, parent_id, created_at, updated_at FROM security_groups WHERE tenant_id = ? ORDER BY created_at`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanGroups(rows)
}

// UpdateGroupName changes the name of a security group.
func (s *SecurityStore) UpdateGroupName(ctx context.Context, id, name string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	tenantID := tenantIDFromContext(ctx)
	_, err := s.db.ExecContext(ctx,
		`UPDATE security_groups SET name = ?, updated_at = ? WHERE tenant_id = ? AND id = ?`,
		name, now, tenantID, id)
	return err
}

// DeleteGroup removes a security group by ID.
func (s *SecurityStore) DeleteGroup(ctx context.Context, id string) error {
	tenantID := tenantIDFromContext(ctx)
	_, err := s.db.ExecContext(ctx, `DELETE FROM security_groups WHERE tenant_id = ? AND id = ?`, tenantID, id)
	if err != nil {
		return err
	}
	// Also clean up the policy for this group
	_, err = s.db.ExecContext(ctx, `DELETE FROM security_policies WHERE tenant_id = ? AND group_id = ?`, tenantID, id)
	return err
}

// GetRootGroup returns the root group (the one with empty parent_id).
func (s *SecurityStore) GetRootGroup(ctx context.Context) (*SecurityGroup, error) {
	tenantID := tenantIDFromContext(ctx)
	row := s.db.QueryRowContext(ctx,
		`SELECT tenant_id, id, name, parent_id, created_at, updated_at FROM security_groups WHERE tenant_id = ? AND parent_id = '' LIMIT 1`, tenantID)
	return scanGroup(row)
}

// GetGroupDepth calculates the depth of a group in the tree by walking
// the parent_id chain up to the root. Root group has depth 0.
func (s *SecurityStore) GetGroupDepth(ctx context.Context, groupID string) (int, error) {
	depth := 0
	currentID := groupID
	for {
		var parentID string
		tenantID := tenantIDFromContext(ctx)
		err := s.db.QueryRowContext(ctx,
			`SELECT parent_id FROM security_groups WHERE tenant_id = ? AND id = ?`, tenantID, currentID).Scan(&parentID)
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
	tenantID := tenantIDFromContext(ctx)
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO security_group_members (tenant_id, email, group_id, created_at)
		 VALUES (?, ?, ?, ?)`,
		tenantID, email, groupID, now)
	return err
}

// RemoveUser removes a user from their assigned group.
func (s *SecurityStore) RemoveUser(ctx context.Context, email string) error {
	tenantID := tenantIDFromContext(ctx)
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM security_group_members WHERE tenant_id = ? AND email = ?`, tenantID, email)
	return err
}

// GetUserGroup returns the group ID the user is assigned to.
// Returns empty string if the user is not assigned to any group.
func (s *SecurityStore) GetUserGroup(ctx context.Context, email string) (string, error) {
	var groupID string
	tenantID := tenantIDFromContext(ctx)
	err := s.db.QueryRowContext(ctx,
		`SELECT group_id FROM security_group_members WHERE tenant_id = ? AND email = ?`, tenantID, email).Scan(&groupID)
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
	tenantID := tenantIDFromContext(ctx)
	rows, err := s.db.QueryContext(ctx,
		`SELECT email FROM security_group_members WHERE tenant_id = ? AND group_id = ? ORDER BY created_at`, tenantID, groupID)
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
	tenantID := tenantIDFromContext(ctx)
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM security_group_members WHERE tenant_id = ? AND group_id = ?`, tenantID, groupID).Scan(&count)
	return count, err
}

// CountGroupMembersMap returns member counts for all groups in one query.
func (s *SecurityStore) CountGroupMembersMap(ctx context.Context) (map[string]int, error) {
	tenantID := tenantIDFromContext(ctx)
	rows, err := s.db.QueryContext(ctx,
		`SELECT group_id, COUNT(*) FROM security_group_members WHERE tenant_id = ? GROUP BY group_id`, tenantID)
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
	tenantID := tenantIDFromContext(ctx)
	rows, err := s.db.QueryContext(ctx, `SELECT email FROM security_group_members WHERE tenant_id = ?`, tenantID)
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
	tenantID := tenantIDFromContext(ctx)
	args := make([]any, 0, len(fromGroupIDs)+2)
	args = append(args, rootGroupID, tenantID)
	for i, id := range fromGroupIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	query := fmt.Sprintf(
		`UPDATE security_group_members SET group_id = ? WHERE tenant_id = ? AND group_id IN (%s)`,
		strings.Join(placeholders, ","))
	_, err := s.db.ExecContext(ctx, query, args...)
	return err
}

// --- Group Policies ---

// GetGroupPolicy returns the sparse policy JSON for a group as a map.
// Returns an empty map if no policy is set.
func (s *SecurityStore) GetGroupPolicy(ctx context.Context, groupID string) (map[string]interface{}, error) {
	var policyJSON string
	tenantID := tenantIDFromContext(ctx)
	err := s.db.QueryRowContext(ctx,
		`SELECT policy_json FROM security_policies WHERE tenant_id = ? AND group_id = ?`, tenantID, groupID).Scan(&policyJSON)
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
	tenantID := tenantIDFromContext(ctx)
	_, err = s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO security_policies (tenant_id, group_id, policy_json, updated_at)
		 VALUES (?, ?, ?, ?)`,
		tenantID, groupID, string(data), now)
	return err
}

// --- Helpers ---

func scanGroup(row *sql.Row) (*SecurityGroup, error) {
	var g SecurityGroup
	var createdAt, updatedAt string
	if err := row.Scan(&g.TenantID, &g.ID, &g.Name, &g.ParentID, &createdAt, &updatedAt); err != nil {
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
		if err := rows.Scan(&g.TenantID, &g.ID, &g.Name, &g.ParentID, &createdAt, &updatedAt); err != nil {
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
