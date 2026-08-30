package cloudworkspace

import (
	"context"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/security"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const maxAncestorWalk = 32

// Granted reports whether principal may use cloud workspaces in its tenant.
func (s *Service) Granted(ctx context.Context, principal auth.MachinePrincipal) (bool, error) {
	if s == nil {
		return false, nil
	}
	settings := s.LoadTenantSettings(ctx, store.NormalizeTenantID(principal.TenantID))
	return s.granted(ctx, principal, settings)
}

func (s *Service) granted(ctx context.Context, principal auth.MachinePrincipal, settings Settings) (bool, error) {
	if s == nil || strings.TrimSpace(principal.UserID) == "" {
		return false, nil
	}
	switch settings.Mode {
	case ModeOff:
		return false, nil
	case ModeAllUsers:
		return true, nil
	case ModeDepartments:
		if s.Users == nil {
			return false, nil
		}
		user, err := s.Users.GetByID(ctx, principal.UserID)
		if err != nil {
			return false, err
		}
		if user == nil {
			return false, nil
		}
		email := strings.ToLower(strings.TrimSpace(user.Email))
		if email == "" {
			return false, nil
		}
		ctx = security.WithTenant(ctx, principal.TenantID)
		return s.grantedByDepartment(ctx, email, settings.DepartmentIDs)
	default:
		return false, nil
	}
}

func (s *Service) grantedByDepartment(ctx context.Context, email string, departmentIDs []string) (bool, error) {
	if s.Groups == nil || len(departmentIDs) == 0 {
		return false, nil
	}
	deptSet := make(map[string]struct{}, len(departmentIDs))
	for _, id := range departmentIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			deptSet[id] = struct{}{}
		}
	}
	if len(deptSet) == 0 {
		return false, nil
	}
	groupID, err := s.Groups.GetUserGroupID(ctx, email)
	if err != nil {
		return false, err
	}
	if groupID == "" {
		return false, nil
	}
	if _, ok := deptSet[groupID]; ok {
		return true, nil
	}
	visited := map[string]struct{}{groupID: {}}
	cur := groupID
	for i := 0; i < maxAncestorWalk; i++ {
		g, err := s.Groups.GetGroupByID(ctx, cur)
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
