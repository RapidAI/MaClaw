package agentservice

import "strings"

// InstanceOwner finds the principal that owns an instance by its id alone.
// It walks every tenant and user, so it is for rare server-side follow-ups
// (a background job's watcher resuming a bot after a restart), not for
// request paths.
func (s *Service) InstanceOwner(instanceID string) (Principal, bool) {
	instanceID = strings.TrimSpace(instanceID)
	if s == nil || s.store == nil || instanceID == "" {
		return Principal{}, false
	}
	tenants, err := s.store.ListTenants()
	if err != nil {
		return Principal{}, false
	}
	for _, tenant := range tenants {
		users, err := s.store.ListUsers(tenant.ID)
		if err != nil {
			continue
		}
		for _, user := range users {
			if _, err := s.store.GetInstance(tenant.ID, user.ID, instanceID); err == nil {
				return Principal{TenantID: tenant.ID, UserID: user.ID}, true
			}
		}
	}
	return Principal{}, false
}
