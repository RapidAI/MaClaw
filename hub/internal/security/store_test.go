package security

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func newTestStore(t *testing.T) *SecurityStore {
	t.Helper()
	db := newTestDB(t)
	s := NewSecurityStore(db)
	if err := s.InitSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestInitSchema(t *testing.T) {
	s := newTestStore(t)
	// Calling InitSchema again should be idempotent
	if err := s.InitSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestInitRootGroup(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.InitRootGroup(ctx); err != nil {
		t.Fatal(err)
	}

	root, err := s.GetRootGroup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if root == nil {
		t.Fatal("expected root group, got nil")
	}
	if root.Name != "全局" {
		t.Fatalf("expected root name '全局', got %q", root.Name)
	}
	if root.ParentID != "" {
		t.Fatalf("expected empty parent_id, got %q", root.ParentID)
	}

	// Idempotent: calling again should not create a second root
	if err := s.InitRootGroup(ctx); err != nil {
		t.Fatal(err)
	}
	groups, err := s.ListGroups(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 group after double init, got %d", len(groups))
	}
}

func TestCreateAndGetGroup(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	s.InitRootGroup(ctx)

	root, _ := s.GetRootGroup(ctx)

	group := &SecurityGroup{
		ID:       "child-1",
		Name:     "研发部",
		ParentID: root.ID,
	}
	group.CreatedAt = group.UpdatedAt
	if err := s.CreateGroup(ctx, group); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetGroupByID(ctx, "child-1")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected group, got nil")
	}
	if got.Name != "研发部" {
		t.Fatalf("expected name '研发部', got %q", got.Name)
	}
	if got.ParentID != root.ID {
		t.Fatalf("expected parent_id %q, got %q", root.ID, got.ParentID)
	}
}

func TestGetGroupByID_NotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	got, err := s.GetGroupByID(ctx, "nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatal("expected nil for nonexistent group")
	}
}

func TestListGroups(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	s.InitRootGroup(ctx)
	root, _ := s.GetRootGroup(ctx)

	s.CreateGroup(ctx, &SecurityGroup{ID: "g1", Name: "A", ParentID: root.ID})
	s.CreateGroup(ctx, &SecurityGroup{ID: "g2", Name: "B", ParentID: root.ID})

	groups, err := s.ListGroups(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 3 { // root + 2 children
		t.Fatalf("expected 3 groups, got %d", len(groups))
	}
}

func TestUpdateGroupName(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	s.InitRootGroup(ctx)
	root, _ := s.GetRootGroup(ctx)

	s.CreateGroup(ctx, &SecurityGroup{ID: "g1", Name: "Old", ParentID: root.ID})

	if err := s.UpdateGroupName(ctx, "g1", "New"); err != nil {
		t.Fatal(err)
	}

	got, _ := s.GetGroupByID(ctx, "g1")
	if got.Name != "New" {
		t.Fatalf("expected name 'New', got %q", got.Name)
	}
}

func TestDeleteGroup(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	s.InitRootGroup(ctx)
	root, _ := s.GetRootGroup(ctx)

	s.CreateGroup(ctx, &SecurityGroup{ID: "g1", Name: "ToDelete", ParentID: root.ID})
	s.SetGroupPolicy(ctx, "g1", map[string]interface{}{"gossip_enabled": false})

	if err := s.DeleteGroup(ctx, "g1"); err != nil {
		t.Fatal(err)
	}

	got, _ := s.GetGroupByID(ctx, "g1")
	if got != nil {
		t.Fatal("expected nil after delete")
	}

	// Policy should also be cleaned up
	policy, _ := s.GetGroupPolicy(ctx, "g1")
	if len(policy) != 0 {
		t.Fatalf("expected empty policy after delete, got %v", policy)
	}
}

func TestGetGroupDepth(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	s.InitRootGroup(ctx)
	root, _ := s.GetRootGroup(ctx)

	// root -> g1 -> g2 -> g3
	s.CreateGroup(ctx, &SecurityGroup{ID: "g1", Name: "L1", ParentID: root.ID})
	s.CreateGroup(ctx, &SecurityGroup{ID: "g2", Name: "L2", ParentID: "g1"})
	s.CreateGroup(ctx, &SecurityGroup{ID: "g3", Name: "L3", ParentID: "g2"})

	depth, err := s.GetGroupDepth(ctx, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if depth != 0 {
		t.Fatalf("expected root depth 0, got %d", depth)
	}

	depth, _ = s.GetGroupDepth(ctx, "g1")
	if depth != 1 {
		t.Fatalf("expected depth 1, got %d", depth)
	}

	depth, _ = s.GetGroupDepth(ctx, "g3")
	if depth != 3 {
		t.Fatalf("expected depth 3, got %d", depth)
	}
}

func TestAssignAndGetUserGroup(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	s.InitRootGroup(ctx)
	root, _ := s.GetRootGroup(ctx)

	s.CreateGroup(ctx, &SecurityGroup{ID: "g1", Name: "Dev", ParentID: root.ID})

	if err := s.AssignUser(ctx, "alice@example.com", "g1"); err != nil {
		t.Fatal(err)
	}

	groupID, err := s.GetUserGroup(ctx, "alice@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if groupID != "g1" {
		t.Fatalf("expected group 'g1', got %q", groupID)
	}

	// Reassign (UPSERT) should update
	if err := s.AssignUser(ctx, "alice@example.com", root.ID); err != nil {
		t.Fatal(err)
	}
	groupID, _ = s.GetUserGroup(ctx, "alice@example.com")
	if groupID != root.ID {
		t.Fatalf("expected root group after reassign, got %q", groupID)
	}
}

func TestAssignUserSameEmailAcrossTenants(t *testing.T) {
	s := newTestStore(t)
	ctxA := WithTenant(context.Background(), "tenant_a")
	ctxB := WithTenant(context.Background(), "tenant_b")
	if err := s.InitRootGroup(ctxA); err != nil {
		t.Fatal(err)
	}
	if err := s.InitRootGroup(ctxB); err != nil {
		t.Fatal(err)
	}
	rootA, _ := s.GetRootGroup(ctxA)
	rootB, _ := s.GetRootGroup(ctxB)

	if err := s.AssignUser(ctxA, "same@example.com", rootA.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.AssignUser(ctxB, "same@example.com", rootB.ID); err != nil {
		t.Fatal(err)
	}

	groupA, err := s.GetUserGroup(ctxA, "same@example.com")
	if err != nil {
		t.Fatal(err)
	}
	groupB, err := s.GetUserGroup(ctxB, "same@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if groupA != rootA.ID || groupB != rootB.ID {
		t.Fatalf("expected tenant-scoped memberships, got A=%q B=%q", groupA, groupB)
	}
	membersA, _ := s.ListGroupMembers(ctxA, rootA.ID)
	membersB, _ := s.ListGroupMembers(ctxB, rootB.ID)
	if len(membersA) != 1 || len(membersB) != 1 {
		t.Fatalf("expected member in both tenants, got A=%v B=%v", membersA, membersB)
	}
}

func TestInitSchemaMigratesLegacySecurityTenantKeys(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `CREATE TABLE security_groups (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		parent_id TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		tenant_id TEXT NOT NULL DEFAULT 'tenant_default'
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE security_group_members (
		email TEXT PRIMARY KEY,
		group_id TEXT NOT NULL,
		created_at TEXT NOT NULL,
		tenant_id TEXT NOT NULL DEFAULT 'tenant_default'
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE security_policies (
		group_id TEXT PRIMARY KEY,
		policy_json TEXT NOT NULL DEFAULT '{}',
		updated_at TEXT NOT NULL,
		tenant_id TEXT NOT NULL DEFAULT 'tenant_default'
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO security_group_members (email, group_id, created_at, tenant_id) VALUES ('legacy@example.com', 'legacy-group', '2026-01-01T00:00:00Z', 'tenant_default')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO security_policies (group_id, policy_json, updated_at, tenant_id) VALUES ('legacy-group', '{"gossip_enabled":false}', '2026-01-01T00:00:00Z', 'tenant_default')`); err != nil {
		t.Fatal(err)
	}
	s := NewSecurityStore(db)
	if err := s.InitSchema(ctx); err != nil {
		t.Fatal(err)
	}

	ok, err := tablePrimaryKeyIs(ctx, db, "security_group_members", []string{"tenant_id", "email"})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected composite primary key on security_group_members")
	}
	ok, err = tablePrimaryKeyIs(ctx, db, "security_policies", []string{"tenant_id", "group_id"})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected composite primary key on security_policies")
	}
	groupID, err := s.GetUserGroup(ctx, "legacy@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if groupID != "legacy-group" {
		t.Fatalf("expected migrated member group, got %q", groupID)
	}
	policy, err := s.GetGroupPolicy(ctx, "legacy-group")
	if err != nil {
		t.Fatal(err)
	}
	if policy["gossip_enabled"] != false {
		t.Fatalf("expected migrated policy, got %#v", policy)
	}
}

func TestGetUserGroup_NotAssigned(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	groupID, err := s.GetUserGroup(ctx, "nobody@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if groupID != "" {
		t.Fatalf("expected empty string for unassigned user, got %q", groupID)
	}
}

func TestRemoveUser(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	s.InitRootGroup(ctx)
	root, _ := s.GetRootGroup(ctx)

	s.AssignUser(ctx, "bob@example.com", root.ID)

	if err := s.RemoveUser(ctx, "bob@example.com"); err != nil {
		t.Fatal(err)
	}

	groupID, _ := s.GetUserGroup(ctx, "bob@example.com")
	if groupID != "" {
		t.Fatalf("expected empty after remove, got %q", groupID)
	}
}

func TestListGroupMembers(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	s.InitRootGroup(ctx)
	root, _ := s.GetRootGroup(ctx)

	s.AssignUser(ctx, "a@example.com", root.ID)
	s.AssignUser(ctx, "b@example.com", root.ID)

	members, err := s.ListGroupMembers(ctx, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 2 {
		t.Fatalf("expected 2 members, got %d", len(members))
	}
}

func TestCountGroupMembers(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	s.InitRootGroup(ctx)
	root, _ := s.GetRootGroup(ctx)

	s.AssignUser(ctx, "a@example.com", root.ID)
	s.AssignUser(ctx, "b@example.com", root.ID)
	s.AssignUser(ctx, "c@example.com", root.ID)

	count, err := s.CountGroupMembers(ctx, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("expected 3, got %d", count)
	}
}

func TestMoveUsersToRoot(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	s.InitRootGroup(ctx)
	root, _ := s.GetRootGroup(ctx)

	s.CreateGroup(ctx, &SecurityGroup{ID: "g1", Name: "A", ParentID: root.ID})
	s.CreateGroup(ctx, &SecurityGroup{ID: "g2", Name: "B", ParentID: root.ID})

	s.AssignUser(ctx, "a@example.com", "g1")
	s.AssignUser(ctx, "b@example.com", "g2")
	s.AssignUser(ctx, "c@example.com", "g1")

	if err := s.MoveUsersToRoot(ctx, []string{"g1", "g2"}, root.ID); err != nil {
		t.Fatal(err)
	}

	for _, email := range []string{"a@example.com", "b@example.com", "c@example.com"} {
		gid, _ := s.GetUserGroup(ctx, email)
		if gid != root.ID {
			t.Fatalf("expected user %s in root group, got %q", email, gid)
		}
	}
}

func TestMoveUsersToRoot_EmptyList(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Should be a no-op
	if err := s.MoveUsersToRoot(ctx, nil, "root-id"); err != nil {
		t.Fatal(err)
	}
}

func TestGetGroupPolicy_Empty(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	policy, err := s.GetGroupPolicy(ctx, "nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if len(policy) != 0 {
		t.Fatalf("expected empty map, got %v", policy)
	}
}

func TestSetAndGetGroupPolicy(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	s.InitRootGroup(ctx)
	root, _ := s.GetRootGroup(ctx)

	policy := map[string]interface{}{
		"gossip_enabled":        false,
		"file_outbound_enabled": true,
		"guardrail_mode":        "strict",
	}
	if err := s.SetGroupPolicy(ctx, root.ID, policy); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetGroupPolicy(ctx, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 keys, got %d", len(got))
	}
	if got["gossip_enabled"] != false {
		t.Fatalf("expected gossip_enabled=false, got %v", got["gossip_enabled"])
	}
	if got["guardrail_mode"] != "strict" {
		t.Fatalf("expected guardrail_mode='strict', got %v", got["guardrail_mode"])
	}
}

func TestSetGroupPolicy_Overwrite(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	s.InitRootGroup(ctx)
	root, _ := s.GetRootGroup(ctx)

	// Set initial policy
	s.SetGroupPolicy(ctx, root.ID, map[string]interface{}{"gossip_enabled": false})

	// Overwrite with different policy
	s.SetGroupPolicy(ctx, root.ID, map[string]interface{}{"sandbox_mode": "docker"})

	got, _ := s.GetGroupPolicy(ctx, root.ID)
	if len(got) != 1 {
		t.Fatalf("expected 1 key after overwrite, got %d: %v", len(got), got)
	}
	if got["sandbox_mode"] != "docker" {
		t.Fatalf("expected sandbox_mode='docker', got %v", got["sandbox_mode"])
	}
}

func TestSetGroupPolicy_EmptyMap(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	s.InitRootGroup(ctx)
	root, _ := s.GetRootGroup(ctx)

	// Set then clear
	s.SetGroupPolicy(ctx, root.ID, map[string]interface{}{"gossip_enabled": false})
	s.SetGroupPolicy(ctx, root.ID, map[string]interface{}{})

	got, _ := s.GetGroupPolicy(ctx, root.ID)
	if len(got) != 0 {
		t.Fatalf("expected empty map after clear, got %v", got)
	}
}
