package agentservice

import (
	"errors"
	"strings"
)

// InstanceOwner finds the principal that owns an instance by its id alone.
// It walks every tenant and user, so it is for rare server-side follow-ups
// (a background job's watcher resuming a bot after a restart), not for
// request paths. A store failure is returned as that error. ErrInstanceNotFound
// means the instance is not there. Callers must not treat a store error as
// "no such instance".
func (s *Service) InstanceOwner(instanceID string) (Principal, error) {
	instanceID = strings.TrimSpace(instanceID)
	if s == nil || s.store == nil || instanceID == "" {
		return Principal{}, ErrInstanceNotFound
	}
	tenants, err := s.store.ListTenants()
	if err != nil {
		return Principal{}, err
	}
	for _, tenant := range tenants {
		users, err := s.store.ListUsers(tenant.ID)
		if err != nil {
			return Principal{}, err
		}
		for _, user := range users {
			_, err := s.store.GetInstance(tenant.ID, user.ID, instanceID)
			if err == nil {
				return Principal{TenantID: tenant.ID, UserID: user.ID}, nil
			}
			if !errors.Is(err, ErrInstanceNotFound) {
				return Principal{}, err
			}
		}
	}
	return Principal{}, ErrInstanceNotFound
}
