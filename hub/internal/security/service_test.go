package security

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// --- Mock repositories ---

type mockSystemSettings struct {
	mu   sync.Mutex
	data map[string]string
}

func newMockSystemSettings() *mockSystemSettings {
	return &mockSystemSettings{data: make(map[string]string)}
}

func (m *mockSystemSettings) Set(_ context.Context, key, valueJSON string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = valueJSON
	return nil
}

func (m *mockSystemSettings) Get(_ context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.data[key]
	if !ok {
		return "", fmt.Errorf("not found")
	}
	return v, nil
}

type mockAuditRepo struct {
	mu   sync.Mutex
	logs []*store.AdminAuditLog
}

func (m *mockAuditRepo) Create(_ context.Context, log *store.AdminAuditLog) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs = append(m.logs, log)
	return nil
}

func (m *mockAuditRepo) List(_ context.Context, _ store.AdminAuditLogFilter) ([]*store.AdminAuditLog, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]*store.AdminAuditLog, len(m.logs))
	copy(cp, m.logs)
	return cp, nil
}

func (m *mockAuditRepo) getLogs() []*store.AdminAuditLog {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]*store.AdminAuditLog, len(m.logs))
	copy(cp, m.logs)
	return cp
}

type mockUserRepo struct {
	items []*store.User
}

func (m *mockUserRepo) Create(_ context.Context, user *store.User) error {
	m.items = append(m.items, user)
	return nil
}

func (m *mockUserRepo) GetByID(_ context.Context, id string) (*store.User, error) {
	for _, user := range m.items {
		if user != nil && user.ID == id {
			return user, nil
		}
	}
	return nil, nil
}

func (m *mockUserRepo) GetByEmail(_ context.Context, email string) (*store.User, error) {
	for _, user := range m.items {
		if user != nil && strings.EqualFold(user.Email, email) {
			return user, nil
		}
	}
	return nil, nil
}

func (m *mockUserRepo) GetByTenantEmail(_ context.Context, tenantID, email string) (*store.User, error) {
	for _, user := range m.items {
		if user != nil && strings.EqualFold(user.TenantID, tenantID) && strings.EqualFold(user.Email, email) {
			return user, nil
		}
	}
	return nil, nil
}

func (m *mockUserRepo) List(_ context.Context) ([]*store.User, error) {
	return m.items, nil
}

func (m *mockUserRepo) ListByTenant(_ context.Context, tenantID string) ([]*store.User, error) {
	var out []*store.User
	for _, user := range m.items {
		if user != nil && strings.EqualFold(user.TenantID, tenantID) {
			out = append(out, user)
		}
	}
	return out, nil
}

func (m *mockUserRepo) DeleteByEmail(_ context.Context, email string) error {
	var next []*store.User
	for _, user := range m.items {
		if user == nil || strings.EqualFold(user.Email, email) {
			continue
		}
		next = append(next, user)
	}
	m.items = next
	return nil
}

func (m *mockUserRepo) DeleteByTenantEmail(_ context.Context, tenantID, email string) error {
	var next []*store.User
	for _, user := range m.items {
		if user == nil || (strings.EqualFold(user.TenantID, tenantID) && strings.EqualFold(user.Email, email)) {
			continue
		}
		next = append(next, user)
	}
	m.items = next
	return nil
}

func (m *mockUserRepo) UpdateSmartRoute(_ context.Context, userID string, enabled bool) error {
	for _, user := range m.items {
		if user != nil && user.ID == userID {
			user.SmartRoute = enabled
		}
	}
	return nil
}

func (m *mockUserRepo) MarkEmailVerified(_ context.Context, _, _ string) error {
	return nil
}

// --- Test helpers ---

func newTestService(t *testing.T) (*SecurityService, *mockAuditRepo) {
	t.Helper()
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.InitRootGroup(ctx); err != nil {
		t.Fatal(err)
	}
	sys := newMockSystemSettings()
	audit := &mockAuditRepo{}
	svc := NewSecurityService(st, sys, audit)
	return svc, audit
}

func TestServiceUpdateGroupPolicyValidatesCanonicalValues(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	root, err := svc.store.GetRootGroup(ctx)
	if err != nil || root == nil {
		t.Fatalf("root group: %v", err)
	}

	if err := svc.UpdateGroupPolicy(ctx, root.ID, map[string]interface{}{"sandbox_mode": "docker", "network_level": "allowlist", "network_allowlist": []interface{}{"api.example.com"}, "guardrail_mode": "relaxed"}); err != nil {
		t.Fatalf("valid policy rejected: %v", err)
	}
	if err := svc.UpdateGroupPolicy(ctx, root.ID, map[string]interface{}{"sandbox_mode": "strict"}); err == nil {
		t.Fatal("expected legacy UI sandbox value to be rejected")
	}
	if err := svc.UpdateGroupPolicy(ctx, root.ID, map[string]interface{}{"network_level": "limited"}); err == nil {
		t.Fatal("expected legacy UI network value to be rejected")
	}
	if err := svc.UpdateGroupPolicy(ctx, root.ID, map[string]interface{}{"network_allowlist": []interface{}{"ok.example", 42}}); err == nil {
		t.Fatal("expected non-string network allowlist entry to be rejected")
	}
	if err := svc.UpdateGroupPolicy(ctx, root.ID, map[string]interface{}{"skill_sources_allowed": []interface{}{"skillhub", "unknown"}}); err == nil {
		t.Fatal("expected invalid skill source to be rejected")
	}
	if err := svc.UpdateGroupPolicy(ctx, root.ID, map[string]interface{}{"skill_sources_allowed": []interface{}{"hubcenter", "git_hub", "enterprise"}}); err != nil {
		t.Fatalf("valid skill source aliases rejected: %v", err)
	}
}

// --- CreateGroup tests ---

func TestServiceCreateGroup(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	root, _ := svc.store.GetRootGroup(ctx)

	group, err := svc.CreateGroup(ctx, "\u7814\u53d1\u90e8", root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if group.Name != "\u7814\u53d1\u90e8" {
		t.Fatalf("expected name '\u7814\u53d1\u90e8', got %q", group.Name)
	}
	if group.ParentID != root.ID {
		t.Fatalf("expected parent %q, got %q", root.ID, group.ParentID)
	}
}

func TestServiceCreateGroup_ParentNotFound(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	_, err := svc.CreateGroup(ctx, "Orphan", "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent parent")
	}
}

func TestServiceCreateGroup_DepthLimit(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	root, _ := svc.store.GetRootGroup(ctx)
	parentID := root.ID

	// Create 9 levels (root=0, so children at depth 1..9)
	for i := 1; i <= 9; i++ {
		g, err := svc.CreateGroup(ctx, fmt.Sprintf("Level%d", i), parentID)
		if err != nil {
			t.Fatalf("failed to create level %d: %v", i, err)
		}
		parentID = g.ID
	}

	// Level 10 should fail (parent at depth 9, child would be depth 10)
	_, err := svc.CreateGroup(ctx, "TooDeep", parentID)
	if err == nil {
		t.Fatal("expected error for depth exceeding 10")
	}
}

// --- RenameGroup tests ---

func TestServiceRenameGroup(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	root, _ := svc.store.GetRootGroup(ctx)
	g, _ := svc.CreateGroup(ctx, "Old", root.ID)

	if err := svc.RenameGroup(ctx, g.ID, "New"); err != nil {
		t.Fatal(err)
	}

	got, _ := svc.store.GetGroupByID(ctx, g.ID)
	if got.Name != "New" {
		t.Fatalf("expected 'New', got %q", got.Name)
	}
}

// --- DeleteGroup tests ---

func TestServiceDeleteGroup(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	root, _ := svc.store.GetRootGroup(ctx)
	g, _ := svc.CreateGroup(ctx, "ToDelete", root.ID)

	if err := svc.DeleteGroup(ctx, g.ID); err != nil {
		t.Fatal(err)
	}

	got, _ := svc.store.GetGroupByID(ctx, g.ID)
	if got != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestServiceDeleteGroup_CascadeUsersToRoot(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	root, _ := svc.store.GetRootGroup(ctx)
	parent, _ := svc.CreateGroup(ctx, "Parent", root.ID)
	child, _ := svc.CreateGroup(ctx, "Child", parent.ID)

	// Assign users to parent and child
	svc.AssignUser(ctx, "alice@test.com", parent.ID)
	svc.AssignUser(ctx, "bob@test.com", child.ID)

	// Delete parent (should cascade to child)
	if err := svc.DeleteGroup(ctx, parent.ID); err != nil {
		t.Fatal(err)
	}

	// Both users should be in root
	for _, email := range []string{"alice@test.com", "bob@test.com"} {
		gid, _ := svc.store.GetUserGroup(ctx, email)
		if gid != root.ID {
			t.Fatalf("expected user %s in root, got %q", email, gid)
		}
	}

	// Child should also be deleted
	got, _ := svc.store.GetGroupByID(ctx, child.ID)
	if got != nil {
		t.Fatal("expected child to be deleted")
	}
}

func TestServiceDeleteGroup_RefuseRoot(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	root, _ := svc.store.GetRootGroup(ctx)
	err := svc.DeleteGroup(ctx, root.ID)
	if err == nil {
		t.Fatal("expected error when deleting root")
	}
}

// --- GetGroupTree tests ---

func TestServiceGetGroupTree(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	root, _ := svc.store.GetRootGroup(ctx)
	a, _ := svc.CreateGroup(ctx, "A", root.ID)
	svc.CreateGroup(ctx, "B", root.ID)
	svc.CreateGroup(ctx, "A1", a.ID)

	svc.AssignUser(ctx, "user@test.com", a.ID)

	tree, err := svc.GetGroupTree(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if tree == nil {
		t.Fatal("expected tree, got nil")
	}
	if tree.Name != "\u5168\u5c40" {
		t.Fatalf("expected root name '\u5168\u5c40', got %q", tree.Name)
	}
	if len(tree.Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(tree.Children))
	}

	// Find group A and check member count
	var nodeA *GroupTreeNode
	for _, c := range tree.Children {
		if c.Name == "A" {
			nodeA = c
			break
		}
	}
	if nodeA == nil {
		t.Fatal("expected to find group A")
	}
	if nodeA.MemberCount != 1 {
		t.Fatalf("expected 1 member in A, got %d", nodeA.MemberCount)
	}
	if len(nodeA.Children) != 1 {
		t.Fatalf("expected 1 child of A, got %d", len(nodeA.Children))
	}
}

func TestServiceGetGroupTreeInitializesTenantRoot(t *testing.T) {
	st := newTestStore(t)
	svc := NewSecurityService(st, newMockSystemSettings(), &mockAuditRepo{})
	ctx := WithTenant(context.Background(), "tenant_lazy")

	tree, err := svc.GetGroupTree(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if tree == nil || tree.ID == "" || tree.TenantID != "tenant_lazy" {
		t.Fatalf("expected lazy tenant root, got %#v", tree)
	}
	rootID, err := svc.GetRootGroupID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rootID != tree.ID {
		t.Fatalf("expected root id %q, got %q", tree.ID, rootID)
	}
}

func TestServiceGetGroupTree_CountsAssignableUsersAndDescendants(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.InitRootGroup(ctx); err != nil {
		t.Fatal(err)
	}
	users := &mockUserRepo{items: []*store.User{
		{ID: "u1", TenantID: store.DefaultTenantID, Email: "active@test.com", Status: "active"},
		{ID: "u2", TenantID: store.DefaultTenantID, Email: "pending@test.com", Status: "pending"},
		{ID: "u3", TenantID: store.DefaultTenantID, Email: "disabled@test.com", Status: "disabled"},
	}}
	svc := NewSecurityService(st, newMockSystemSettings(), &mockAuditRepo{}, users)
	root, _ := svc.store.GetRootGroup(ctx)
	parent, _ := svc.CreateGroup(ctx, "Parent", root.ID)
	child, _ := svc.CreateGroup(ctx, "Child", parent.ID)
	if err := svc.AssignUser(ctx, "active@test.com", child.ID); err != nil {
		t.Fatal(err)
	}

	tree, err := svc.GetGroupTree(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if tree.MemberCount != 3 {
		t.Fatalf("expected root to count all assignable users, got %d", tree.MemberCount)
	}
	if len(tree.Children) != 1 || tree.Children[0].MemberCount != 1 {
		t.Fatalf("expected parent count to include child member, got tree=%+v", tree.Children)
	}

	members, err := svc.ListGroupMembers(ctx, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 2 {
		t.Fatalf("expected root members to include two unassigned users, got %v", members)
	}
}

func TestServiceGetGroupTree_UsesCachedSnapshotUntilInvalidated(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	root, _ := svc.store.GetRootGroup(ctx)
	child, err := svc.CreateGroup(ctx, "Cached", root.ID)
	if err != nil {
		t.Fatal(err)
	}

	first, err := svc.GetGroupTree(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if first == nil {
		t.Fatal("expected initial tree")
	}

	if err := svc.store.CreateGroup(ctx, &SecurityGroup{
		ID:        "direct-child",
		Name:      "DirectlyInserted",
		ParentID:  root.ID,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	cached, err := svc.GetGroupTree(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if containsGroupByName(cached, "DirectlyInserted") {
		t.Fatal("expected cached tree to hide direct store mutation before invalidation")
	}

	if err := svc.AssignUser(ctx, "cache@test.com", child.ID); err != nil {
		t.Fatal(err)
	}

	refreshed, err := svc.GetGroupTree(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !containsGroupByName(refreshed, "DirectlyInserted") {
		t.Fatal("expected invalidation to refresh cached tree")
	}
}

func containsGroupByName(node *GroupTreeNode, name string) bool {
	if node == nil {
		return false
	}
	if node.Name == name {
		return true
	}
	for _, child := range node.Children {
		if containsGroupByName(child, name) {
			return true
		}
	}
	return false
}

// --- AssignUser / RemoveUser tests ---

func TestServiceAssignUser_SingleGroupInvariant(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	root, _ := svc.store.GetRootGroup(ctx)
	a, _ := svc.CreateGroup(ctx, "A", root.ID)
	b, _ := svc.CreateGroup(ctx, "B", root.ID)

	svc.AssignUser(ctx, "user@test.com", a.ID)
	svc.AssignUser(ctx, "user@test.com", b.ID) // should move from A to B

	gid, _ := svc.store.GetUserGroup(ctx, "user@test.com")
	if gid != b.ID {
		t.Fatalf("expected user in B, got %q", gid)
	}

	// A should have 0 members
	count, _ := svc.store.CountGroupMembers(ctx, a.ID)
	if count != 0 {
		t.Fatalf("expected 0 members in A, got %d", count)
	}
}

func TestServiceRemoveUser(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	root, _ := svc.store.GetRootGroup(ctx)
	svc.AssignUser(ctx, "user@test.com", root.ID)
	svc.RemoveUser(ctx, root.ID, "user@test.com")

	gid, _ := svc.store.GetUserGroup(ctx, "user@test.com")
	if gid != "" {
		t.Fatalf("expected empty after remove, got %q", gid)
	}
}

// --- Policy tests ---

func TestServiceGetEffectivePolicy_Default(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	// Unassigned user should get default policy
	policy, err := svc.GetEffectivePolicy(ctx, "nobody@test.com")
	if err != nil {
		t.Fatal(err)
	}
	if !policy.FileOutboundEnabled {
		t.Fatal("expected file_outbound_enabled=true")
	}
	if policy.GuardrailMode != "standard" {
		t.Fatalf("expected guardrail_mode='standard', got %q", policy.GuardrailMode)
	}
}

func TestServiceGetEffectivePolicy_Inheritance(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	root, _ := svc.store.GetRootGroup(ctx)

	// Set root policy: disable gossip
	svc.UpdateGroupPolicy(ctx, root.ID, map[string]interface{}{
		"gossip_enabled": false,
	})

	// Create child with its own override
	child, _ := svc.CreateGroup(ctx, "Child", root.ID)
	svc.UpdateGroupPolicy(ctx, child.ID, map[string]interface{}{
		"guardrail_mode": "strict",
	})

	// Assign user to child
	svc.AssignUser(ctx, "user@test.com", child.ID)

	policy, err := svc.GetEffectivePolicy(ctx, "user@test.com")
	if err != nil {
		t.Fatal(err)
	}

	// gossip_enabled should be inherited from root (false)
	if policy.GossipEnabled {
		t.Fatal("expected gossip_enabled=false (inherited from root)")
	}
	// guardrail_mode should be overridden by child
	if policy.GuardrailMode != "strict" {
		t.Fatalf("expected guardrail_mode='strict', got %q", policy.GuardrailMode)
	}
	// Other defaults should remain
	if !policy.FileOutboundEnabled {
		t.Fatal("expected file_outbound_enabled=true (default)")
	}
}

func TestServiceGetUserPolicyViewIncludesSourceAnnotations(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	root, _ := svc.store.GetRootGroup(ctx)
	if err := svc.UpdateGroupPolicy(ctx, root.ID, map[string]interface{}{"gossip_enabled": false}); err != nil {
		t.Fatal(err)
	}
	child, _ := svc.CreateGroup(ctx, "Legal", root.ID)
	if err := svc.UpdateGroupPolicy(ctx, child.ID, map[string]interface{}{"guardrail_mode": "strict"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.AssignUser(ctx, "lawyer@test.com", child.ID); err != nil {
		t.Fatal(err)
	}

	groupID, groupPath, policy, view, err := svc.GetUserPolicyView(ctx, "lawyer@test.com")
	if err != nil {
		t.Fatal(err)
	}
	if groupID != child.ID {
		t.Fatalf("groupID = %q, want %q", groupID, child.ID)
	}
	if len(groupPath) != 2 || groupPath[0].ID != root.ID || groupPath[1].ID != child.ID || groupPath[1].Name != "Legal" {
		t.Fatalf("unexpected group path: %#v", groupPath)
	}
	if policy == nil || policy.GuardrailMode != "strict" || policy.GossipEnabled {
		t.Fatalf("unexpected policy: %#v", policy)
	}
	if view == nil || view.Items["guardrail_mode"].Source != "self" {
		t.Fatalf("expected self source for guardrail_mode, got %#v", view)
	}
	if view.Items["gossip_enabled"].Source != "inherited" || view.Items["gossip_enabled"].SourceGroup != root.ID {
		t.Fatalf("expected inherited root source for gossip_enabled, got %#v", view.Items["gossip_enabled"])
	}
}

func TestServiceGetEffectivePolicy_ThreeLevels(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	root, _ := svc.store.GetRootGroup(ctx)

	// Root: disable file outbound
	svc.UpdateGroupPolicy(ctx, root.ID, map[string]interface{}{
		"file_outbound_enabled": false,
	})

	// Level 1: set sandbox_mode
	l1, _ := svc.CreateGroup(ctx, "L1", root.ID)
	svc.UpdateGroupPolicy(ctx, l1.ID, map[string]interface{}{
		"sandbox_mode": "docker",
	})

	// Level 2: re-enable file outbound
	l2, _ := svc.CreateGroup(ctx, "L2", l1.ID)
	svc.UpdateGroupPolicy(ctx, l2.ID, map[string]interface{}{
		"file_outbound_enabled": true,
	})

	svc.AssignUser(ctx, "deep@test.com", l2.ID)

	policy, err := svc.GetEffectivePolicy(ctx, "deep@test.com")
	if err != nil {
		t.Fatal(err)
	}

	if !policy.FileOutboundEnabled {
		t.Fatal("expected file_outbound_enabled=true (overridden at L2)")
	}
	if policy.SandboxMode != "docker" {
		t.Fatalf("expected sandbox_mode='docker' (from L1), got %q", policy.SandboxMode)
	}
}

func TestServiceGetGroupEffectivePolicy(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	root, _ := svc.store.GetRootGroup(ctx)
	svc.UpdateGroupPolicy(ctx, root.ID, map[string]interface{}{
		"gossip_enabled": false,
	})

	child, _ := svc.CreateGroup(ctx, "Child", root.ID)
	svc.UpdateGroupPolicy(ctx, child.ID, map[string]interface{}{
		"network_level": "intranet",
	})

	policy, err := svc.GetGroupEffectivePolicy(ctx, child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if policy.GossipEnabled {
		t.Fatal("expected gossip_enabled=false")
	}
	if policy.NetworkLevel != "intranet" {
		t.Fatalf("expected network_level='intranet', got %q", policy.NetworkLevel)
	}
}

func TestServiceGetGroupPolicy_View(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	root, _ := svc.store.GetRootGroup(ctx)
	svc.UpdateGroupPolicy(ctx, root.ID, map[string]interface{}{
		"gossip_enabled": false,
	})

	child, _ := svc.CreateGroup(ctx, "Child", root.ID)
	svc.UpdateGroupPolicy(ctx, child.ID, map[string]interface{}{
		"guardrail_mode": "strict",
	})

	view, err := svc.GetGroupPolicy(ctx, child.ID)
	if err != nil {
		t.Fatal(err)
	}

	// guardrail_mode should be "self"
	gm := view.Items["guardrail_mode"]
	if gm.Source != "self" {
		t.Fatalf("expected guardrail_mode source 'self', got %q", gm.Source)
	}

	// gossip_enabled should be "inherited"
	ge := view.Items["gossip_enabled"]
	if ge.Source != "inherited" {
		t.Fatalf("expected gossip_enabled source 'inherited', got %q", ge.Source)
	}
	if ge.SourceGroup != root.ID {
		t.Fatalf("expected gossip_enabled source group %q, got %q", root.ID, ge.SourceGroup)
	}
}

// --- Settings tests ---

func TestServiceGetSettings_Default(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	settings, err := svc.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if settings.CentralizedSecurityEnabled {
		t.Fatal("expected centralized_security_enabled=false by default")
	}
	if settings.OrgStructureEnabled {
		t.Fatal("expected org_structure_enabled=false by default")
	}
}

func TestServiceUpdateSettings_AuditLog(t *testing.T) {
	svc, audit := newTestService(t)
	ctx := context.Background()

	// Enable centralized security
	err := svc.UpdateSettings(ctx, &SecuritySettings{
		CentralizedSecurityEnabled: true,
	}, "admin-1")
	if err != nil {
		t.Fatal(err)
	}

	logs := audit.getLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 audit log, got %d", len(logs))
	}
	if logs[0].Action != "centralized_security_enabled" {
		t.Fatalf("expected action 'centralized_security_enabled', got %q", logs[0].Action)
	}

	// Now also enable org_structure
	err = svc.UpdateSettings(ctx, &SecuritySettings{
		CentralizedSecurityEnabled: true,
		OrgStructureEnabled:        true,
	}, "admin-1")
	if err != nil {
		t.Fatal(err)
	}

	logs = audit.getLogs()
	if len(logs) != 2 {
		t.Fatalf("expected 2 audit logs, got %d", len(logs))
	}
	if logs[1].Action != "org_structure_enabled" {
		t.Fatalf("expected action 'org_structure_enabled', got %q", logs[1].Action)
	}
}

func TestServiceUpdateSettings_NoAuditWhenUnchanged(t *testing.T) {
	svc, audit := newTestService(t)
	ctx := context.Background()

	// Set initial
	svc.UpdateSettings(ctx, &SecuritySettings{CentralizedSecurityEnabled: true}, "admin-1")

	// Update with same value
	svc.UpdateSettings(ctx, &SecuritySettings{CentralizedSecurityEnabled: true}, "admin-1")

	logs := audit.getLogs()
	// Only 1 log from the first change
	if len(logs) != 1 {
		t.Fatalf("expected 1 audit log (no change), got %d", len(logs))
	}
}

func TestServiceGetSettings_UsesCachedSnapshotUntilInvalidated(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	first, err := svc.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if first == nil {
		t.Fatal("expected initial settings")
	}

	payload, err := json.Marshal(&SecuritySettings{CentralizedSecurityEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.system.Set(ctx, settingsKey, string(payload)); err != nil {
		t.Fatal(err)
	}

	cached, err := svc.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cached.CentralizedSecurityEnabled {
		t.Fatal("expected cached settings to hide direct store mutation before invalidation")
	}

	if err := svc.UpdateSettings(ctx, &SecuritySettings{CentralizedSecurityEnabled: true}, "admin-1"); err != nil {
		t.Fatal(err)
	}

	refreshed, err := svc.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !refreshed.CentralizedSecurityEnabled {
		t.Fatal("expected settings cache to refresh after update")
	}
}

func TestServiceUpdateSettings_TenantContextDoesNotPoisonDefaultCache(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	initial, err := svc.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if initial.CentralizedSecurityEnabled {
		t.Fatal("expected initial default settings to be disabled")
	}

	tenantCtx := WithTenant(ctx, "tenant_a")
	if err := svc.UpdateSettings(tenantCtx, &SecuritySettings{CentralizedSecurityEnabled: true}, "tenant-admin"); err != nil {
		t.Fatal(err)
	}

	defaultSettings, err := svc.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if defaultSettings.CentralizedSecurityEnabled {
		t.Fatal("tenant settings update must not replace cached default-tenant settings")
	}
}

func TestServiceGetSettings_TenantSettingsAreIsolated(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	ctxA := WithTenant(ctx, "tenant_a")
	ctxB := WithTenant(ctx, "tenant_b")

	if err := svc.UpdateSettings(ctxA, &SecuritySettings{CentralizedSecurityEnabled: true, OrgStructureEnabled: true}, "tenant-a-admin"); err != nil {
		t.Fatal(err)
	}

	settingsA, err := svc.GetSettings(ctxA)
	if err != nil {
		t.Fatal(err)
	}
	if !settingsA.CentralizedSecurityEnabled || !settingsA.OrgStructureEnabled {
		t.Fatalf("expected tenant A settings to persist, got %+v", settingsA)
	}

	settingsB, err := svc.GetSettings(ctxB)
	if err != nil {
		t.Fatal(err)
	}
	if settingsB.CentralizedSecurityEnabled || settingsB.OrgStructureEnabled {
		t.Fatalf("expected tenant B settings to stay empty, got %+v", settingsB)
	}

	defaultSettings, err := svc.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if defaultSettings.CentralizedSecurityEnabled || defaultSettings.OrgStructureEnabled {
		t.Fatalf("expected default settings to stay empty, got %+v", defaultSettings)
	}
}

func TestServiceSetDefaultGroup_RefreshesCachedSettings(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	root, _ := svc.store.GetRootGroup(ctx)
	child, _ := svc.CreateGroup(ctx, "Default", root.ID)

	initial, err := svc.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if initial.DefaultGroupID != "" {
		t.Fatalf("expected empty default group, got %q", initial.DefaultGroupID)
	}

	if err := svc.SetDefaultGroup(ctx, child.ID); err != nil {
		t.Fatal(err)
	}

	updated, err := svc.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if updated.DefaultGroupID != child.ID {
		t.Fatalf("expected default_group_id=%q, got %q", child.ID, updated.DefaultGroupID)
	}
}

// --- SetDefaultGroup tests ---

func TestServiceSetDefaultGroup(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	root, _ := svc.store.GetRootGroup(ctx)
	child, _ := svc.CreateGroup(ctx, "Default", root.ID)

	if err := svc.SetDefaultGroup(ctx, child.ID); err != nil {
		t.Fatal(err)
	}

	settings, _ := svc.GetSettings(ctx)
	if settings.DefaultGroupID != child.ID {
		t.Fatalf("expected default_group_id=%q, got %q", child.ID, settings.DefaultGroupID)
	}
}

func TestServiceSetDefaultGroup_NotFound(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	err := svc.SetDefaultGroup(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent group")
	}
}

// --- Cache tests ---

func TestServiceCacheInvalidation(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	root, _ := svc.store.GetRootGroup(ctx)
	svc.AssignUser(ctx, "user@test.com", root.ID)

	// First call populates cache
	p1, _ := svc.GetEffectivePolicy(ctx, "user@test.com")

	// Update policy
	svc.store.SetGroupPolicy(ctx, root.ID, map[string]interface{}{
		"gossip_enabled": false,
	})

	// Cached value should still be old
	p2, _ := svc.GetEffectivePolicy(ctx, "user@test.com")
	if p1.GossipEnabled != p2.GossipEnabled {
		t.Fatal("expected cached value to be same")
	}

	// Invalidate and re-fetch
	svc.InvalidateCache("user@test.com")
	p3, _ := svc.GetEffectivePolicy(ctx, "user@test.com")
	if p3.GossipEnabled {
		t.Fatal("expected gossip_enabled=false after cache invalidation")
	}
}

func TestServiceEffectivePolicyCacheIsTenantScoped(t *testing.T) {
	svc, _ := newTestService(t)
	ctxA := WithTenant(context.Background(), "tenant_a")
	ctxB := WithTenant(context.Background(), "tenant_b")

	if err := svc.store.InitRootGroup(ctxA); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.InitRootGroup(ctxB); err != nil {
		t.Fatal(err)
	}
	rootA, _ := svc.store.GetRootGroup(ctxA)
	rootB, _ := svc.store.GetRootGroup(ctxB)
	if err := svc.AssignUser(ctxA, "same@example.com", rootA.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.AssignUser(ctxB, "same@example.com", rootB.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.UpdateGroupPolicy(ctxA, rootA.ID, map[string]interface{}{"gossip_enabled": false}); err != nil {
		t.Fatal(err)
	}

	policyA, err := svc.GetEffectivePolicy(ctxA, "same@example.com")
	if err != nil {
		t.Fatal(err)
	}
	policyB, err := svc.GetEffectivePolicy(ctxB, "same@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if policyA.GossipEnabled {
		t.Fatalf("expected tenant A gossip disabled")
	}
	if !policyB.GossipEnabled {
		t.Fatalf("expected tenant B to keep default gossip enabled")
	}
}

func TestServiceGroupTreeCacheIsTenantScoped(t *testing.T) {
	svc, _ := newTestService(t)
	ctxA := WithTenant(context.Background(), "tenant_a")
	ctxB := WithTenant(context.Background(), "tenant_b")

	if err := svc.store.InitRootGroup(ctxA); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.InitRootGroup(ctxB); err != nil {
		t.Fatal(err)
	}
	rootA, _ := svc.store.GetRootGroup(ctxA)
	rootB, _ := svc.store.GetRootGroup(ctxB)
	if _, err := svc.CreateGroup(ctxA, "Only A", rootA.ID); err != nil {
		t.Fatal(err)
	}

	treeA, err := svc.GetGroupTree(ctxA)
	if err != nil {
		t.Fatal(err)
	}
	treeB, err := svc.GetGroupTree(ctxB)
	if err != nil {
		t.Fatal(err)
	}
	if treeA == nil || treeA.ID != rootA.ID || len(treeA.Children) != 1 {
		t.Fatalf("unexpected tenant A tree: %#v", treeA)
	}
	if treeB == nil || treeB.ID != rootB.ID || len(treeB.Children) != 0 {
		t.Fatalf("tenant B tree should not reuse tenant A cache: %#v", treeB)
	}
}

// --- HeartbeatPolicy tests ---

func TestServiceGetHeartbeatPolicy_Disabled(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	payload, err := svc.GetHeartbeatPolicy(ctx, "user@test.com")
	if err != nil {
		t.Fatal(err)
	}
	if payload.CentralizedSecurity {
		t.Fatal("expected centralized_security=false")
	}
	if payload.Policy != nil {
		t.Fatal("expected no policy when disabled")
	}
}

func TestServiceGetHeartbeatPolicy_Enabled(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	svc.UpdateSettings(ctx, &SecuritySettings{
		CentralizedSecurityEnabled: true,
	}, "admin")

	root, _ := svc.store.GetRootGroup(ctx)
	svc.AssignUser(ctx, "user@test.com", root.ID)

	payload, err := svc.GetHeartbeatPolicy(ctx, "user@test.com")
	if err != nil {
		t.Fatal(err)
	}
	if !payload.CentralizedSecurity {
		t.Fatal("expected centralized_security=true")
	}
	if payload.Policy == nil {
		t.Fatal("expected policy when enabled")
	}
}

func TestServiceGetHeartbeatPolicyResolvesUserIDToEmail(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.InitRootGroup(ctx); err != nil {
		t.Fatal(err)
	}
	sys := newMockSystemSettings()
	users := &mockUserRepo{items: []*store.User{{ID: "u-1", TenantID: store.DefaultTenantID, Email: "user@test.com"}}}
	svc := NewSecurityService(st, sys, &mockAuditRepo{}, users)

	if err := svc.UpdateSettings(ctx, &SecuritySettings{CentralizedSecurityEnabled: true}, "admin"); err != nil {
		t.Fatal(err)
	}
	root, _ := svc.store.GetRootGroup(ctx)
	if err := svc.AssignUser(ctx, "user@test.com", root.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.UpdateGroupPolicy(ctx, root.ID, map[string]interface{}{"smart_route_enabled": false}); err != nil {
		t.Fatal(err)
	}

	payload, err := svc.GetHeartbeatPolicy(ctx, "u-1")
	if err != nil {
		t.Fatal(err)
	}
	if payload.Policy == nil || payload.Policy.SmartRouteEnabled {
		t.Fatalf("policy smart_route_enabled = %#v, want false", payload.Policy)
	}
}

// --- IsCentralizedEnabled tests ---

func TestServiceIsCentralizedEnabled(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	enabled, _ := svc.IsCentralizedEnabled(ctx)
	if enabled {
		t.Fatal("expected false by default")
	}

	svc.UpdateSettings(ctx, &SecuritySettings{CentralizedSecurityEnabled: true}, "admin")

	enabled, _ = svc.IsCentralizedEnabled(ctx)
	if !enabled {
		t.Fatal("expected true after enabling")
	}
}

// --- GetEffectivePolicyByUserID tests ---

func TestServiceGetEffectivePolicyByUserID(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	// Should work same as GetEffectivePolicy
	policy, err := svc.GetEffectivePolicyByUserID(ctx, "user@test.com")
	if err != nil {
		t.Fatal(err)
	}
	if policy == nil {
		t.Fatal("expected non-nil policy")
	}
}

// --- Verify SecurityPolicyProvider interface ---

func TestServiceImplementsSecurityPolicyProvider(t *testing.T) {
	svc, _ := newTestService(t)
	// Compile-time check
	var _ SecurityPolicyProvider = svc
}

// --- policyToMap / applyPolicyOverrides helpers ---

func TestPolicyToMapRoundTrip(t *testing.T) {
	m := policyToMap(DefaultPolicy)
	var p EffectivePolicy
	p = DefaultPolicy // start from default
	applyPolicyOverrides(&p, m)

	if !reflect.DeepEqual(p, DefaultPolicy) {
		t.Fatal("round-trip through policyToMap/applyPolicyOverrides should preserve DefaultPolicy")
	}
}

// --- Unused import guard for time ---
var _ = time.Now
var _ = json.Marshal

// --- SkillSourcesProvider integration tests ---

// mockSkillSourcesProvider implements SkillSourcesResolver for testing.
type mockSkillSourcesProvider struct {
	result       []string
	seenUserID   string
	seenTenantID string
}

func (m *mockSkillSourcesProvider) ResolveForUser(_ context.Context, userID, tenantID string) []string {
	m.seenUserID = userID
	m.seenTenantID = tenantID
	return m.result
}

func TestServiceGetHeartbeatPolicy_SkillSourcesUsesRuntimeUserID(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := WithTenant(context.Background(), "tenant-a")
	svc.users = &mockUserRepo{items: []*store.User{{TenantID: "tenant-a", ID: "runtime-user-1", Email: "employee@example.com"}}}
	provider := &mockSkillSourcesProvider{result: []string{"local"}}
	svc.SetSkillSourcesProvider(provider)

	payload, err := svc.GetHeartbeatPolicy(ctx, "runtime-user-1")
	if err != nil {
		t.Fatal(err)
	}
	if provider.seenUserID != "runtime-user-1" || provider.seenTenantID != "tenant-a" {
		t.Fatalf("provider saw user=%q tenant=%q, want runtime-user-1 tenant-a", provider.seenUserID, provider.seenTenantID)
	}
	if !reflect.DeepEqual(payload.SkillSourcesAllowed, []string{"local"}) {
		t.Fatalf("SkillSourcesAllowed = %#v, want local", payload.SkillSourcesAllowed)
	}
}

func TestServiceUpdateGroupPolicyAcceptsLocalSkillSource(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	root, _ := svc.store.GetRootGroup(ctx)
	if err := svc.UpdateGroupPolicy(ctx, root.ID, map[string]interface{}{"skill_sources_allowed": []interface{}{"local"}}); err != nil {
		t.Fatalf("local skill source should be valid: %v", err)
	}
}

func TestServiceGetHeartbeatPolicy_SkillSources_CentralizedOff(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	// Inject skill source provider that restricts to skillhub only.
	svc.SetSkillSourcesProvider(&mockSkillSourcesProvider{result: []string{"skillhub"}})

	payload, err := svc.GetHeartbeatPolicy(ctx, "user@test.com")
	if err != nil {
		t.Fatal(err)
	}
	if payload.CentralizedSecurity {
		t.Fatal("expected centralized_security=false")
	}
	// SkillSourcesAllowed should be set at payload level (independent control).
	if !reflect.DeepEqual(payload.SkillSourcesAllowed, []string{"skillhub"}) {
		t.Fatalf("expected [skillhub], got %v", payload.SkillSourcesAllowed)
	}
}

func TestServiceGetHeartbeatPolicy_SkillSources_CentralizedOff_NoRestriction(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	// Provider returns nil (all allowed) — should not set SkillSourcesAllowed.
	svc.SetSkillSourcesProvider(&mockSkillSourcesProvider{result: nil})

	payload, err := svc.GetHeartbeatPolicy(ctx, "user@test.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(payload.SkillSourcesAllowed) != 0 {
		t.Fatalf("expected empty SkillSourcesAllowed, got %v", payload.SkillSourcesAllowed)
	}
}

func TestServiceGetHeartbeatPolicy_SkillSources_CentralizedOff_BlockAll(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	svc.SetSkillSourcesProvider(&mockSkillSourcesProvider{result: []string{}})

	payload, err := svc.GetHeartbeatPolicy(ctx, "user@test.com")
	if err != nil {
		t.Fatal(err)
	}
	if payload.CentralizedSecurity {
		t.Fatal("expected centralized_security=false")
	}
	if !payload.SkillSourcesRestricted || payload.SkillSourcesAllowed == nil || len(payload.SkillSourcesAllowed) != 0 {
		t.Fatalf("expected restricted empty source policy, got restricted=%v allowed=%#v", payload.SkillSourcesRestricted, payload.SkillSourcesAllowed)
	}
}

func TestServiceGetHeartbeatPolicy_SkillSources_CentralizedOn_Merge(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	// Enable centralized security.
	svc.UpdateSettings(ctx, &SecuritySettings{CentralizedSecurityEnabled: true}, "admin")
	root, _ := svc.store.GetRootGroup(ctx)
	svc.AssignUser(ctx, "user@test.com", root.ID)

	// Set group policy to allow skillhub + clawhub.
	svc.UpdateGroupPolicy(ctx, root.ID, map[string]interface{}{
		"skill_sources_allowed": []interface{}{"skillhub", "clawhub"},
	})
	svc.InvalidateCache("user@test.com")

	// Inject provider that restricts to skillhub + github.
	svc.SetSkillSourcesProvider(&mockSkillSourcesProvider{result: []string{"skillhub", "github"}})

	payload, err := svc.GetHeartbeatPolicy(ctx, "user@test.com")
	if err != nil {
		t.Fatal(err)
	}
	if !payload.CentralizedSecurity {
		t.Fatal("expected centralized_security=true")
	}
	// Intersection of [skillhub, clawhub] and [skillhub, github] = [skillhub].
	if !reflect.DeepEqual(payload.Policy.SkillSourcesAllowed, []string{"skillhub"}) {
		t.Fatalf("expected intersection [skillhub], got %v", payload.Policy.SkillSourcesAllowed)
	}
}

func TestServiceGetHeartbeatPolicy_SkillSources_CentralizedOn_MergeBlocksAll(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	svc.UpdateSettings(ctx, &SecuritySettings{CentralizedSecurityEnabled: true}, "admin")
	root, _ := svc.store.GetRootGroup(ctx)
	svc.AssignUser(ctx, "user@test.com", root.ID)
	svc.UpdateGroupPolicy(ctx, root.ID, map[string]interface{}{"skill_sources_allowed": []interface{}{"github"}})
	svc.InvalidateCache("user@test.com")
	svc.SetSkillSourcesProvider(&mockSkillSourcesProvider{result: []string{}})

	payload, err := svc.GetHeartbeatPolicy(ctx, "user@test.com")
	if err != nil {
		t.Fatal(err)
	}
	if !payload.CentralizedSecurity || payload.Policy == nil {
		t.Fatalf("expected centralized policy, got %#v", payload)
	}
	if !payload.Policy.SkillSourcesRestricted || payload.Policy.SkillSourcesAllowed == nil || len(payload.Policy.SkillSourcesAllowed) != 0 {
		t.Fatalf("expected restricted empty merged policy, got restricted=%v allowed=%#v", payload.Policy.SkillSourcesRestricted, payload.Policy.SkillSourcesAllowed)
	}
}

func TestServiceGetHeartbeatPolicy_SkillSources_MergeNormalizesAliases(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	svc.UpdateSettings(ctx, &SecuritySettings{CentralizedSecurityEnabled: true}, "admin")
	root, _ := svc.store.GetRootGroup(ctx)
	svc.AssignUser(ctx, "user@test.com", root.ID)

	svc.UpdateGroupPolicy(ctx, root.ID, map[string]interface{}{
		"skill_sources_allowed": []interface{}{"hubcenter", "git_hub"},
	})
	svc.InvalidateCache("user@test.com")
	svc.SetSkillSourcesProvider(&mockSkillSourcesProvider{result: []string{"skillhub", "github"}})

	payload, err := svc.GetHeartbeatPolicy(ctx, "user@test.com")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"skillhub", "github"}
	if !reflect.DeepEqual(payload.Policy.SkillSourcesAllowed, want) {
		t.Fatalf("expected normalized intersection %v, got %v", want, payload.Policy.SkillSourcesAllowed)
	}
}

func TestServiceGetHeartbeatPolicy_SkillSources_NoCachePollution(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	// Enable centralized security.
	svc.UpdateSettings(ctx, &SecuritySettings{CentralizedSecurityEnabled: true}, "admin")
	root, _ := svc.store.GetRootGroup(ctx)
	svc.AssignUser(ctx, "user@test.com", root.ID)

	// Inject provider that restricts to clawhub.
	svc.SetSkillSourcesProvider(&mockSkillSourcesProvider{result: []string{"clawhub"}})

	// First call — merges into policy.
	payload1, _ := svc.GetHeartbeatPolicy(ctx, "user@test.com")
	if !reflect.DeepEqual(payload1.Policy.SkillSourcesAllowed, []string{"clawhub"}) {
		t.Fatalf("first call: expected [clawhub], got %v", payload1.Policy.SkillSourcesAllowed)
	}

	// Change provider to allow all (nil).
	svc.SetSkillSourcesProvider(&mockSkillSourcesProvider{result: nil})

	// Second call — should NOT have stale [clawhub] from cache pollution.
	// The cached EffectivePolicy should have nil SkillSourcesAllowed (default).
	payload2, _ := svc.GetHeartbeatPolicy(ctx, "user@test.com")
	if len(payload2.Policy.SkillSourcesAllowed) != 0 {
		t.Fatalf("second call: expected nil/empty (no restriction), got %v — cache pollution detected", payload2.Policy.SkillSourcesAllowed)
	}
}
