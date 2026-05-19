package im

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type tenantEmailTestUsers struct {
	users []*store.User
}

func (r tenantEmailTestUsers) Create(context.Context, *store.User) error            { return nil }
func (r tenantEmailTestUsers) GetByID(context.Context, string) (*store.User, error) { return nil, nil }
func (r tenantEmailTestUsers) GetByEmail(ctx context.Context, email string) (*store.User, error) {
	return r.GetByTenantEmail(ctx, store.DefaultTenantID, email)
}
func (r tenantEmailTestUsers) GetByTenantEmail(_ context.Context, tenantID, email string) (*store.User, error) {
	tenantID = store.NormalizeTenantID(tenantID)
	for _, user := range r.users {
		if user != nil && store.NormalizeTenantID(user.TenantID) == tenantID && user.Email == email {
			return user, nil
		}
	}
	return nil, nil
}
func (r tenantEmailTestUsers) List(context.Context) ([]*store.User, error) { return r.users, nil }
func (r tenantEmailTestUsers) ListByTenant(_ context.Context, tenantID string) ([]*store.User, error) {
	tenantID = store.NormalizeTenantID(tenantID)
	out := []*store.User{}
	for _, user := range r.users {
		if user != nil && store.NormalizeTenantID(user.TenantID) == tenantID {
			out = append(out, user)
		}
	}
	return out, nil
}
func (r tenantEmailTestUsers) DeleteByEmail(context.Context, string) error               { return nil }
func (r tenantEmailTestUsers) DeleteByTenantEmail(context.Context, string, string) error { return nil }
func (r tenantEmailTestUsers) UpdateSmartRoute(context.Context, string, bool) error      { return nil }

func TestResolveUniqueTenantByEmail(t *testing.T) {
	repo := tenantEmailTestUsers{users: []*store.User{
		{ID: "u1", TenantID: "tenant_a", Email: "same@example.com"},
		{ID: "u2", TenantID: "tenant_b", Email: "other@example.com"},
	}}

	tenantID, user, err := ResolveUniqueTenantByEmail(context.Background(), repo, " same@example.com ")
	if err != nil {
		t.Fatalf("resolve unique tenant: %v", err)
	}
	if tenantID != "tenant_a" || user == nil || user.ID != "u1" {
		t.Fatalf("resolved tenant/user = %q/%v", tenantID, user)
	}
}

func TestResolveUniqueTenantByEmailRejectsAmbiguousEmail(t *testing.T) {
	repo := tenantEmailTestUsers{users: []*store.User{
		{ID: "u1", TenantID: "tenant_a", Email: "same@example.com"},
		{ID: "u2", TenantID: "tenant_b", Email: "same@example.com"},
	}}

	tenantID, user, err := ResolveUniqueTenantByEmail(context.Background(), repo, "same@example.com")
	if err != ErrAmbiguousTenantEmail {
		t.Fatalf("expected ErrAmbiguousTenantEmail, got tenant=%q user=%v err=%v", tenantID, user, err)
	}
}
