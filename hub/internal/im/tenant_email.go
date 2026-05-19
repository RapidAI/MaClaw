package im

import (
	"context"
	"errors"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

var ErrAmbiguousTenantEmail = errors.New("im: email belongs to multiple tenants")

func ResolveUniqueTenantByEmail(ctx context.Context, users store.UserRepository, email string) (string, *store.User, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if users == nil || email == "" {
		return "", nil, nil
	}

	all, err := users.List(ctx)
	if err != nil {
		user, getErr := users.GetByTenantEmail(ctx, store.DefaultTenantID, email)
		if getErr != nil || user == nil {
			return "", nil, getErr
		}
		return store.NormalizeTenantID(user.TenantID), user, nil
	}

	var matched *store.User
	matchedTenant := ""
	seen := map[string]struct{}{}
	for _, user := range all {
		if user == nil || !strings.EqualFold(strings.TrimSpace(user.Email), email) {
			continue
		}
		tenantID := store.NormalizeTenantID(user.TenantID)
		seen[tenantID] = struct{}{}
		if len(seen) > 1 {
			return "", nil, ErrAmbiguousTenantEmail
		}
		matched = user
		matchedTenant = tenantID
	}
	if matched == nil {
		return "", nil, nil
	}
	return matchedTenant, matched, nil
}
