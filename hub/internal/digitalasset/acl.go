package digitalasset

import (
	"context"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/security"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// GroupLookup resolves a user's security group and group ancestry within a tenant.
type GroupLookup interface {
	GetUserGroupID(ctx context.Context, email string) (string, error)
	GetGroupByID(ctx context.Context, id string) (*security.SecurityGroup, error)
}

// SecurityGroupLookup adapts SecurityService to GroupLookup.
type SecurityGroupLookup struct {
	Service *security.SecurityService
}

func (l SecurityGroupLookup) GetUserGroupID(ctx context.Context, email string) (string, error) {
	if l.Service == nil {
		return "", nil
	}
	return l.Service.GetUserGroupID(ctx, email)
}

func (l SecurityGroupLookup) GetGroupByID(ctx context.Context, id string) (*security.SecurityGroup, error) {
	if l.Service == nil {
		return nil, nil
	}
	return l.Service.GetGroupByID(ctx, id)
}

// Evaluator checks per-library ACL.
type Evaluator struct {
	Groups        GroupLookup
	AncestorMatch bool // default true per product decision
}

// CanAccess reports whether email may read/sync the library ACL within tenantID.
// Must be called with tenant-scoped group lookups (caller injects security.WithTenant).
func (e *Evaluator) CanAccess(ctx context.Context, tenantID, email string, acl ACL) (bool, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	mode := strings.TrimSpace(acl.Mode)
	if mode == "" {
		mode = ACLModeAllMembers
	}
	if mode == ACLModeAllMembers {
		return email != "", nil
	}
	if mode != ACLModeRestricted {
		return false, fmt.Errorf("unknown acl mode %q", mode)
	}
	// Restricted with no selected departments denies all non-admins
	// (admins bypass at handler layer).
	if len(acl.Departments) == 0 {
		return false, nil
	}
	if email == "" || e.Groups == nil {
		return false, nil
	}
	// Ensure tenant context for group queries.
	ctx = security.WithTenant(ctx, tenantID)
	groupID, err := e.Groups.GetUserGroupID(ctx, email)
	if err != nil {
		return false, err
	}
	if groupID == "" {
		// Ungrouped users cannot match a department-scoped library.
		return false, nil
	}
	deptSet := make(map[string]struct{}, len(acl.Departments))
	for _, d := range acl.Departments {
		d = strings.TrimSpace(d)
		if d != "" {
			deptSet[d] = struct{}{}
		}
	}
	if len(deptSet) == 0 {
		return false, nil
	}
	// Exact match
	if _, ok := deptSet[groupID]; ok {
		return true, nil
	}
	// Ancestor match: walk user group upward; authorized if any ancestor is in ACL depts
	// (selecting parent department grants child members).
	ancestorMatch := e.AncestorMatch
	if !ancestorMatch {
		return false, nil
	}
	visited := map[string]struct{}{groupID: {}}
	cur := groupID
	for i := 0; i < 32; i++ {
		g, err := e.Groups.GetGroupByID(ctx, cur)
		if err != nil {
			return false, err
		}
		if g == nil {
			break
		}
		parent := strings.TrimSpace(g.ParentID)
		if parent == "" {
			break
		}
		if _, ok := deptSet[parent]; ok {
			return true, nil
		}
		if _, seen := visited[parent]; seen {
			break
		}
		visited[parent] = struct{}{}
		cur = parent
	}
	return false, nil
}

// CanAccessLibrary is a convenience wrapper using store.DigitalAssetLibrary fields.
func (e *Evaluator) CanAccessLibrary(ctx context.Context, lib *store.DigitalAssetLibrary, email string) (bool, error) {
	if e == nil || lib == nil {
		return false, nil
	}
	acl := ParseACL(lib.ACLMode, lib.ACLDepartmentsJSON, lib.ACLUsersJSON)
	return e.CanAccess(ctx, lib.TenantID, email, acl)
}
