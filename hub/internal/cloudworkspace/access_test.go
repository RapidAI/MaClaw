package cloudworkspace

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/security"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type fakeUsers struct {
	byID map[string]*store.User
}

func (f *fakeUsers) GetByID(ctx context.Context, id string) (*store.User, error) {
	if f == nil || f.byID == nil {
		return nil, nil
	}
	return f.byID[id], nil
}

type fakeGroups struct {
	userGroup map[string]string
	groups    map[string]*security.SecurityGroup
	tree      *security.GroupTreeNode
	members   map[string][]string
}

func (f *fakeGroups) GetUserGroupID(ctx context.Context, email string) (string, error) {
	if f == nil || f.userGroup == nil {
		return "", nil
	}
	return f.userGroup[email], nil
}

func (f *fakeGroups) GetGroupByID(ctx context.Context, id string) (*security.SecurityGroup, error) {
	if f == nil || f.groups == nil {
		return nil, nil
	}
	return f.groups[id], nil
}

func (f *fakeGroups) GetGroupTree(ctx context.Context) (*security.GroupTreeNode, error) {
	if f == nil {
		return nil, nil
	}
	return f.tree, nil
}

func (f *fakeGroups) ListGroupMembers(ctx context.Context, groupID string) ([]string, error) {
	if f == nil || f.members == nil {
		return nil, nil
	}
	return f.members[groupID], nil
}

type memorySettings map[string]string

func (m memorySettings) Set(ctx context.Context, key, valueJSON string) error {
	m[key] = valueJSON
	return nil
}

func (m memorySettings) Get(ctx context.Context, key string) (string, error) {
	return m[key], nil
}

func testPrincipal() auth.MachinePrincipal {
	return auth.MachinePrincipal{TenantID: "t1", UserID: "u1", MachineID: "m1"}
}

func testUser() *store.User {
	return &store.User{ID: "u1", TenantID: "t1", Email: "dev@x.com"}
}

func orgTree() *fakeGroups {
	return &fakeGroups{
		userGroup: map[string]string{
			"dev@x.com":   "backend",
			"eng@x.com":   "eng",
			"sales@x.com": "sales",
		},
		groups: map[string]*security.SecurityGroup{
			"root":    {ID: "root", ParentID: ""},
			"eng":     {ID: "eng", ParentID: "root"},
			"backend": {ID: "backend", ParentID: "eng"},
			"sales":   {ID: "sales", ParentID: "root"},
		},
		tree: &security.GroupTreeNode{
			ID: "root",
			Children: []*security.GroupTreeNode{
				{
					ID: "eng",
					Children: []*security.GroupTreeNode{
						{ID: "backend"},
					},
				},
				{ID: "sales"},
			},
		},
		members: map[string][]string{
			"eng":     {"eng@x.com"},
			"backend": {"dev@x.com"},
			"sales":   {"sales@x.com"},
		},
	}
}

func newTestService(users *fakeUsers, groups *fakeGroups) *Service {
	s := &Service{System: memorySettings{}, Users: users, Groups: groups, Org: groups}
	if users == nil {
		s.Users = &fakeUsers{byID: map[string]*store.User{"u1": testUser()}}
	}
	if groups == nil {
		g := orgTree()
		s.Groups = g
		s.Org = g
	}
	return s
}

func mustGrant(t *testing.T, svc *Service, settings Settings, principal auth.MachinePrincipal) bool {
	t.Helper()
	ok, err := svc.granted(context.Background(), principal, settings)
	if err != nil {
		t.Fatalf("granted err=%v", err)
	}
	return ok
}

func TestGranted_ModeOffDenies(t *testing.T) {
	svc := newTestService(nil, nil)
	if mustGrant(t, svc, Settings{Mode: ModeOff, Quota: 5}, testPrincipal()) {
		t.Fatal("mode off should deny")
	}
}

func TestGranted_AllUsersAllowsUngrouped(t *testing.T) {
	svc := newTestService(nil, &fakeGroups{userGroup: map[string]string{}})
	if !mustGrant(t, svc, Settings{Mode: ModeAllUsers, Quota: 5}, testPrincipal()) {
		t.Fatal("all_users should allow even ungrouped")
	}
}

func TestGranted_DepartmentsUngroupedDenies(t *testing.T) {
	svc := newTestService(nil, &fakeGroups{userGroup: map[string]string{}})
	if mustGrant(t, svc, Settings{Mode: ModeDepartments, DepartmentIDs: []string{"eng"}}, testPrincipal()) {
		t.Fatal("ungrouped user should be denied in departments mode")
	}
}

func TestGranted_DepartmentsExactMatchAllows(t *testing.T) {
	groups := orgTree()
	svc := newTestService(&fakeUsers{byID: map[string]*store.User{
		"u1": {ID: "u1", Email: "eng@x.com"},
	}}, groups)
	if !mustGrant(t, svc, Settings{Mode: ModeDepartments, DepartmentIDs: []string{"eng"}}, testPrincipal()) {
		t.Fatal("user in selected department should be allowed")
	}
}

func TestGranted_DepartmentsChildOfSelectedAllows(t *testing.T) {
	svc := newTestService(nil, nil)
	if !mustGrant(t, svc, Settings{Mode: ModeDepartments, DepartmentIDs: []string{"eng"}}, testPrincipal()) {
		t.Fatal("user in child of selected parent should be allowed")
	}
}

func TestGranted_DepartmentsSiblingDenies(t *testing.T) {
	svc := newTestService(&fakeUsers{byID: map[string]*store.User{
		"u1": {ID: "u1", Email: "sales@x.com"},
	}}, nil)
	if mustGrant(t, svc, Settings{Mode: ModeDepartments, DepartmentIDs: []string{"eng"}}, testPrincipal()) {
		t.Fatal("sibling/unrelated department should deny")
	}
}

func TestGranted_DepartmentsEmptyIDsDenies(t *testing.T) {
	svc := newTestService(nil, nil)
	if mustGrant(t, svc, Settings{Mode: ModeDepartments, DepartmentIDs: nil}, testPrincipal()) {
		t.Fatal("empty department_ids should deny")
	}
	if mustGrant(t, svc, Settings{Mode: ModeDepartments, DepartmentIDs: []string{}}, testPrincipal()) {
		t.Fatal("empty department_ids should deny")
	}
}

func TestGranted_UnknownDepartmentIDsSkipped(t *testing.T) {
	svc := newTestService(nil, nil)
	if !mustGrant(t, svc, Settings{Mode: ModeDepartments, DepartmentIDs: []string{"missing", "eng"}}, testPrincipal()) {
		t.Fatal("unknown ids should be skipped; valid ancestor should still match")
	}
	svc = newTestService(&fakeUsers{byID: map[string]*store.User{
		"u1": {ID: "u1", Email: "sales@x.com"},
	}}, nil)
	if mustGrant(t, svc, Settings{Mode: ModeDepartments, DepartmentIDs: []string{"missing"}}, testPrincipal()) {
		t.Fatal("only unknown ids should deny")
	}
}

func TestGranted_MissingUserOrEmailDenies(t *testing.T) {
	svc := newTestService(&fakeUsers{byID: map[string]*store.User{}}, nil)
	if mustGrant(t, svc, Settings{Mode: ModeAllUsers}, testPrincipal()) {
		t.Fatal("missing user should deny")
	}
	svc = newTestService(&fakeUsers{byID: map[string]*store.User{"u1": {ID: "u1", Email: ""}}}, nil)
	if mustGrant(t, svc, Settings{Mode: ModeAllUsers}, testPrincipal()) {
		t.Fatal("empty email should deny")
	}
	if mustGrant(t, svc, Settings{Mode: ModeAllUsers}, auth.MachinePrincipal{TenantID: "t1"}) {
		t.Fatal("empty user id should deny")
	}
}

func TestPreview_DepartmentsCountsDescendantsAndUsers(t *testing.T) {
	svc := newTestService(nil, nil)
	preview := svc.BuildPreview(context.Background(), "t1", Settings{
		Mode:          ModeDepartments,
		DepartmentIDs: []string{"eng"},
	})
	if preview.DepartmentCount != 2 {
		t.Fatalf("department_count=%d want 2 (eng + backend)", preview.DepartmentCount)
	}
	if preview.UserCount != 2 {
		t.Fatalf("user_count=%d want 2", preview.UserCount)
	}
	if preview.OverQuotaUsers == nil || len(preview.OverQuotaUsers) != 0 {
		t.Fatalf("over_quota_users=%v want empty slice", preview.OverQuotaUsers)
	}
	if preview.UsedBytes != 0 {
		t.Fatalf("used_bytes=%d want 0", preview.UsedBytes)
	}

	preview = svc.BuildPreview(context.Background(), "t1", Settings{Mode: ModeOff, DepartmentIDs: []string{"eng"}})
	if preview.DepartmentCount != 0 || preview.UserCount != 0 {
		t.Fatalf("preview when not departments: %+v", preview)
	}
}
