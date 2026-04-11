package security

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/idgen"
)

// GroupRepo provides persistence for security groups.
type GroupRepo struct {
	write *sql.DB
	read  *sql.DB
}

// NewGroupRepo creates a GroupRepo.
func NewGroupRepo(write, read *sql.DB) *GroupRepo {
	return &GroupRepo{write: write, read: read}
}

// InitRootGroup creates the root group ("全局") if none exists.
func (r *GroupRepo) InitRootGroup(tenantID string) error {
	var count int
	if err := r.read.QueryRow(`SELECT COUNT(*) FROM security_groups WHERE parent_id = '' AND tenant_id = ?`, tenantID).Scan(&count); err != nil {
		return fmt.Errorf("check root group: %w", err)
	}
	if count > 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	id := idgen.New("sgrp")
	_, err := r.write.Exec(
		`INSERT INTO security_groups (id, tenant_id, name, parent_id, created_at, updated_at) VALUES (?, ?, ?, '', ?, ?)`,
		id, tenantID, "全局", now, now)
	return err
}

// CreateGroup inserts a new group.
func (r *GroupRepo) CreateGroup(ctx context.Context, tenantID string, g *SecurityGroup) error {
	_, err := r.write.ExecContext(ctx,
		`INSERT INTO security_groups (id, tenant_id, name, parent_id, created_at, updated_at) VALUES (?,?,?,?,?,?)`,
		g.ID, tenantID, g.Name, g.ParentID,
		g.CreatedAt.UTC().Format(time.RFC3339),
		g.UpdatedAt.UTC().Format(time.RFC3339))
	return err
}

// GetGroupByID returns a group or nil.
func (r *GroupRepo) GetGroupByID(ctx context.Context, tenantID string, id string) (*SecurityGroup, error) {
	row := r.read.QueryRowContext(ctx,
		`SELECT id, name, parent_id, created_at, updated_at FROM security_groups WHERE id = ? AND tenant_id = ?`, id, tenantID)
	return scanGroup(row)
}

// UpdateGroupName renames a group.
func (r *GroupRepo) UpdateGroupName(ctx context.Context, tenantID string, id, name string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := r.write.ExecContext(ctx,
		`UPDATE security_groups SET name=?, updated_at=? WHERE id=? AND tenant_id=?`, name, now, id, tenantID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteGroup deletes a group and all descendants, moving members to root.
func (r *GroupRepo) DeleteGroup(ctx context.Context, tenantID string, id string) error {
	g, err := r.GetGroupByID(ctx, tenantID, id)
	if err != nil || g == nil {
		return fmt.Errorf("group not found")
	}
	if g.ParentID == "" {
		return fmt.Errorf("cannot delete root group")
	}
	// find root group
	var rootID string
	if err := r.read.QueryRowContext(ctx, `SELECT id FROM security_groups WHERE parent_id = '' AND tenant_id = ?`, tenantID).Scan(&rootID); err != nil {
		return fmt.Errorf("find root: %w", err)
	}
	// collect all descendant IDs
	ids := []string{id}
	r.collectDescendants(ctx, tenantID, id, &ids)
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, v := range ids {
		placeholders[i] = "?"
		args[i] = v
	}
	inClause := strings.Join(placeholders, ",")
	// move members to root (copy args to avoid mutating the original slice)
	moveArgs := make([]interface{}, 0, 2+len(args))
	moveArgs = append(moveArgs, rootID)
	moveArgs = append(moveArgs, args...)
	moveArgs = append(moveArgs, tenantID)
	_, _ = r.write.ExecContext(ctx,
		`UPDATE security_group_members SET group_id=? WHERE group_id IN (`+inClause+`) AND tenant_id=?`, moveArgs...)
	// delete policies
	delPolicyArgs := make([]interface{}, 0, len(args)+1)
	delPolicyArgs = append(delPolicyArgs, args...)
	delPolicyArgs = append(delPolicyArgs, tenantID)
	_, _ = r.write.ExecContext(ctx,
		`DELETE FROM security_group_policies WHERE group_id IN (`+inClause+`) AND tenant_id=?`, delPolicyArgs...)
	// delete groups
	delGroupArgs := make([]interface{}, 0, len(args)+1)
	delGroupArgs = append(delGroupArgs, args...)
	delGroupArgs = append(delGroupArgs, tenantID)
	_, err = r.write.ExecContext(ctx,
		`DELETE FROM security_groups WHERE id IN (`+inClause+`) AND tenant_id=?`, delGroupArgs...)
	return err
}

func (r *GroupRepo) collectDescendants(ctx context.Context, tenantID string, parentID string, ids *[]string) {
	rows, err := r.read.QueryContext(ctx, `SELECT id FROM security_groups WHERE parent_id = ? AND tenant_id = ?`, parentID, tenantID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var childID string
		if err := rows.Scan(&childID); err != nil {
			continue
		}
		*ids = append(*ids, childID)
		r.collectDescendants(ctx, tenantID, childID, ids)
	}
}

// GetGroupDepth returns the depth of a group in the tree (root = 0).
func (r *GroupRepo) GetGroupDepth(ctx context.Context, tenantID string, id string) (int, error) {
	depth := 0
	current := id
	for {
		var parentID string
		err := r.read.QueryRowContext(ctx,
			`SELECT parent_id FROM security_groups WHERE id = ? AND tenant_id = ?`, current, tenantID).Scan(&parentID)
		if err != nil {
			return depth, err
		}
		if parentID == "" {
			return depth, nil
		}
		depth++
		current = parentID
		if depth > 20 {
			return depth, fmt.Errorf("depth overflow")
		}
	}
}

// GetGroupTree builds the full tree starting from the root.
func (r *GroupRepo) GetGroupTree(ctx context.Context, tenantID string) (*GroupTreeNode, error) {
	rows, err := r.read.QueryContext(ctx,
		`SELECT id, name, parent_id FROM security_groups WHERE tenant_id = ? ORDER BY created_at`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type flatGroup struct {
		ID, Name, ParentID string
	}
	var groups []flatGroup
	for rows.Next() {
		var g flatGroup
		if err := rows.Scan(&g.ID, &g.Name, &g.ParentID); err != nil {
			continue
		}
		groups = append(groups, g)
	}

	// count members per group
	memberCounts := make(map[string]int)
	mcRows, err := r.read.QueryContext(ctx, `SELECT group_id, COUNT(*) FROM security_group_members WHERE tenant_id = ? GROUP BY group_id`, tenantID)
	if err == nil {
		defer mcRows.Close()
		for mcRows.Next() {
			var gid string
			var cnt int
			if mcRows.Scan(&gid, &cnt) == nil {
				memberCounts[gid] = cnt
			}
		}
	}

	// build tree
	nodeMap := make(map[string]*GroupTreeNode)
	for _, g := range groups {
		nodeMap[g.ID] = &GroupTreeNode{
			ID:          g.ID,
			Name:        g.Name,
			ParentID:    g.ParentID,
			MemberCount: memberCounts[g.ID],
		}
	}
	var root *GroupTreeNode
	for _, g := range groups {
		node := nodeMap[g.ID]
		if g.ParentID == "" {
			root = node
		} else if parent, ok := nodeMap[g.ParentID]; ok {
			parent.Children = append(parent.Children, node)
		}
	}
	return root, nil
}

// ListGroupMembers returns emails in a group.
func (r *GroupRepo) ListGroupMembers(ctx context.Context, tenantID string, groupID string) ([]string, error) {
	rows, err := r.read.QueryContext(ctx,
		`SELECT email FROM security_group_members WHERE group_id = ? AND tenant_id = ? ORDER BY created_at`, groupID, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var emails []string
	for rows.Next() {
		var e string
		if rows.Scan(&e) == nil {
			emails = append(emails, e)
		}
	}
	if emails == nil {
		emails = []string{}
	}
	return emails, nil
}

// AssignUser moves a user to a group (removes from previous group).
func (r *GroupRepo) AssignUser(ctx context.Context, tenantID string, email, groupID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := r.write.ExecContext(ctx,
		`INSERT INTO security_group_members (tenant_id, email, group_id, created_at) VALUES (?,?,?,?)
		 ON CONFLICT(email) DO UPDATE SET group_id=excluded.group_id`,
		tenantID, email, groupID, now)
	return err
}

// RemoveUser removes a user from a group (moves to root).
func (r *GroupRepo) RemoveUser(ctx context.Context, tenantID string, groupID, email string) error {
	var rootID string
	if err := r.read.QueryRowContext(ctx, `SELECT id FROM security_groups WHERE parent_id = '' AND tenant_id = ?`, tenantID).Scan(&rootID); err != nil {
		return err
	}
	_, err := r.write.ExecContext(ctx,
		`UPDATE security_group_members SET group_id=? WHERE email=? AND group_id=? AND tenant_id=?`,
		rootID, email, groupID, tenantID)
	return err
}

// GetGroupPolicy returns the sparse policy JSON for a group.
func (r *GroupRepo) GetGroupPolicy(ctx context.Context, tenantID string, groupID string) (map[string]interface{}, error) {
	var policyJSON string
	err := r.read.QueryRowContext(ctx,
		`SELECT policy_json FROM security_group_policies WHERE group_id = ? AND tenant_id = ?`, groupID, tenantID).Scan(&policyJSON)
	if err != nil {
		return map[string]interface{}{}, nil
	}
	var result map[string]interface{}
	if json.Unmarshal([]byte(policyJSON), &result) != nil {
		return map[string]interface{}{}, nil
	}
	return result, nil
}

// UpdateGroupPolicy saves the sparse policy for a group.
func (r *GroupRepo) UpdateGroupPolicy(ctx context.Context, tenantID string, groupID string, policy map[string]interface{}) error {
	data, err := json.Marshal(policy)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = r.write.ExecContext(ctx,
		`INSERT INTO security_group_policies (tenant_id, group_id, policy_json, updated_at) VALUES (?,?,?,?)
		 ON CONFLICT(group_id) DO UPDATE SET policy_json=excluded.policy_json, updated_at=excluded.updated_at`,
		tenantID, groupID, string(data), now)
	return err
}

// GetGroupChildren returns direct child nodes of a group.
func (r *GroupRepo) GetGroupChildren(ctx context.Context, tenantID string, parentID string) ([]*GroupTreeNode, error) {
	rows, err := r.read.QueryContext(ctx,
		`SELECT id, name, parent_id FROM security_groups WHERE parent_id = ? AND tenant_id = ? ORDER BY created_at`, parentID, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var children []*GroupTreeNode
	for rows.Next() {
		var id, name, pid string
		if rows.Scan(&id, &name, &pid) == nil {
			children = append(children, &GroupTreeNode{ID: id, Name: name, ParentID: pid})
		}
	}
	if children == nil {
		children = []*GroupTreeNode{}
	}
	return children, nil
}

func scanGroup(row *sql.Row) (*SecurityGroup, error) {
	var g SecurityGroup
	var createdAt, updatedAt string
	if err := row.Scan(&g.ID, &g.Name, &g.ParentID, &createdAt, &updatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	g.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	g.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &g, nil
}

// LoadSettings reads the security settings from the special key row.
func (r *GroupRepo) LoadSettings(tenantID string, key string) ([]byte, error) {
	var policyJSON string
	err := r.read.QueryRow(
		`SELECT policy_json FROM security_group_policies WHERE group_id = ? AND tenant_id = ?`, key, tenantID).Scan(&policyJSON)
	if err != nil {
		return nil, err
	}
	return []byte(policyJSON), nil
}

// SaveSettings writes the security settings to the special key row.
func (r *GroupRepo) SaveSettings(tenantID string, key string, data []byte) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := r.write.Exec(
		`INSERT INTO security_group_policies (tenant_id, group_id, policy_json, updated_at) VALUES (?,?,?,?)
		 ON CONFLICT(group_id) DO UPDATE SET policy_json=excluded.policy_json, updated_at=excluded.updated_at`,
		tenantID, key, string(data), now)
	return err
}
