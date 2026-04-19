package security

import (
	"context"
	"strings"
)

// ResolveUserGroupChain returns the user's assigned group followed by its
// ancestors up to the root group. If the user is not explicitly assigned to a
// group, the root group is returned so root-level bindings still apply.
func (s *SecurityService) ResolveUserGroupChain(ctx context.Context, email string) ([]string, error) {
	if s == nil || s.store == nil {
		return nil, nil
	}
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" {
		return nil, nil
	}
	groupID, err := s.store.GetUserGroup(ctx, email)
	if err != nil {
		return nil, err
	}
	if groupID == "" {
		root, err := s.store.GetRootGroup(ctx)
		if err != nil || root == nil {
			return nil, err
		}
		return []string{root.ID}, nil
	}
	chain := []string{}
	seen := map[string]struct{}{}
	currentID := groupID
	for currentID != "" {
		key := strings.ToLower(strings.TrimSpace(currentID))
		if _, ok := seen[key]; ok {
			break
		}
		seen[key] = struct{}{}
		chain = append(chain, currentID)
		group, err := s.store.GetGroupByID(ctx, currentID)
		if err != nil || group == nil {
			if err != nil {
				return nil, err
			}
			break
		}
		currentID = strings.TrimSpace(group.ParentID)
	}
	return chain, nil
}
