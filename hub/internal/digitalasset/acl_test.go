package digitalasset

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/security"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type fakeGroups struct {
	userGroup map[string]string
	groups    map[string]*security.SecurityGroup
}

func (f *fakeGroups) GetUserGroupID(ctx context.Context, email string) (string, error) {
	return f.userGroup[email], nil
}

func (f *fakeGroups) GetGroupByID(ctx context.Context, id string) (*security.SecurityGroup, error) {
	return f.groups[id], nil
}

func TestACL_AllMembers(t *testing.T) {
	e := &Evaluator{Groups: &fakeGroups{}}
	ok, err := e.CanAccess(context.Background(), "t1", "a@x.com", ACL{Mode: ACLModeAllMembers})
	if err != nil || !ok {
		t.Fatalf("all_members: ok=%v err=%v", ok, err)
	}
}

func TestACL_RestrictedEmptyDeny(t *testing.T) {
	e := &Evaluator{Groups: &fakeGroups{}, AncestorMatch: true}
	ok, err := e.CanAccess(context.Background(), "t1", "a@x.com", ACL{Mode: ACLModeRestricted})
	if err != nil || ok {
		t.Fatalf("empty restricted should deny: ok=%v err=%v", ok, err)
	}
}

func TestACL_DepartmentExactAndAncestor(t *testing.T) {
	// tree: root -> eng -> backend
	// ACL departments = [eng]; user in backend should pass with ancestor match
	f := &fakeGroups{
		userGroup: map[string]string{"dev@x.com": "backend", "sales@x.com": "sales"},
		groups: map[string]*security.SecurityGroup{
			"root":    {ID: "root", ParentID: ""},
			"eng":     {ID: "eng", ParentID: "root"},
			"backend": {ID: "backend", ParentID: "eng"},
			"sales":   {ID: "sales", ParentID: "root"},
		},
	}
	e := &Evaluator{Groups: f, AncestorMatch: true}
	ok, err := e.CanAccess(context.Background(), "t1", "dev@x.com", ACL{
		Mode:        ACLModeRestricted,
		Departments: []string{"eng"},
	})
	if err != nil || !ok {
		t.Fatalf("child of eng should allow: ok=%v err=%v", ok, err)
	}
	ok, err = e.CanAccess(context.Background(), "t1", "sales@x.com", ACL{
		Mode:        ACLModeRestricted,
		Departments: []string{"eng"},
	})
	if err != nil || ok {
		t.Fatalf("sibling sales should deny: ok=%v err=%v", ok, err)
	}

	e.AncestorMatch = false
	ok, err = e.CanAccess(context.Background(), "t1", "dev@x.com", ACL{
		Mode:        ACLModeRestricted,
		Departments: []string{"eng"},
	})
	if err != nil || ok {
		t.Fatalf("without ancestor match exact eng only: ok=%v err=%v", ok, err)
	}
	ok, _ = e.CanAccess(context.Background(), "t1", "dev@x.com", ACL{
		Mode:        ACLModeRestricted,
		Departments: []string{"backend"},
	})
	if !ok {
		t.Fatal("exact backend should allow")
	}
}

func TestACL_UngroupedUserDenied(t *testing.T) {
	f := &fakeGroups{userGroup: map[string]string{}, groups: map[string]*security.SecurityGroup{}}
	e := &Evaluator{Groups: f, AncestorMatch: true}
	ok, _ := e.CanAccess(context.Background(), "t1", "lone@x.com", ACL{
		Mode:        ACLModeRestricted,
		Departments: []string{"eng"},
	})
	if ok {
		t.Fatal("ungrouped should not match department")
	}
}

func TestACL_LibraryIncludesChildDepartments(t *testing.T) {
	f := &fakeGroups{
		userGroup: map[string]string{"dev@x.com": "backend"},
		groups: map[string]*security.SecurityGroup{
			"backend": {ID: "backend", ParentID: "eng"},
			"eng":     {ID: "eng"},
		},
	}
	e := &Evaluator{Groups: f, AncestorMatch: true}
	lib := &store.DigitalAssetLibrary{TenantID: "t1", ACLMode: ACLModeRestricted, ACLDepartmentsJSON: `["eng"]`}
	ok, err := e.CanAccessLibrary(context.Background(), lib, "dev@x.com")
	if err != nil || !ok {
		t.Fatalf("child department member should be allowed: ok=%v err=%v", ok, err)
	}
}

func TestACL_FingerprintStable(t *testing.T) {
	a := ACL{Mode: ACLModeRestricted, Departments: []string{"b", " a ", "b", ""}}
	b := ACL{Mode: ACLModeRestricted, Departments: []string{"a", "b"}}
	if a.Fingerprint() != b.Fingerprint() {
		t.Fatalf("fingerprint should be order-insensitive: %s vs %s", a.Fingerprint(), b.Fingerprint())
	}
}

func TestParseACLAllMembersDropsLegacyDepartments(t *testing.T) {
	acl := ParseACL(ACLModeAllMembers, `["dept_a"]`, `["legacy@example.com"]`)
	if len(acl.Departments) != 0 {
		t.Fatalf("all-members ACL should not retain departments: %#v", acl.Departments)
	}
}

func TestACL_LegacyUserGrantDoesNotAllowRestrictedAccess(t *testing.T) {
	e := &Evaluator{Groups: &fakeGroups{userGroup: map[string]string{}}, AncestorMatch: true}
	lib := &store.DigitalAssetLibrary{
		TenantID:           "t1",
		ACLMode:            ACLModeRestricted,
		ACLDepartmentsJSON: `["eng"]`,
		ACLUsersJSON:       `["legacy@example.com"]`,
	}
	ok, err := e.CanAccessLibrary(context.Background(), lib, "legacy@example.com")
	if err != nil || ok {
		t.Fatalf("legacy user ACL grant must not bypass department ACL: ok=%v err=%v", ok, err)
	}
}
