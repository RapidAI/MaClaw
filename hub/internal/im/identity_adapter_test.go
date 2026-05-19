package im

import (
	"context"
	"strings"
	"testing"
)

type tenantAwareIdentityTestPlugin struct {
	mockPlugin
	tenantID string
	userID   string
}

func (p *tenantAwareIdentityTestPlugin) ResolveUserWithTenant(context.Context, string) (string, string, error) {
	return p.tenantID, p.userID, nil
}

func TestPluginIdentityResolverRejectsMismatchedTenantHint(t *testing.T) {
	router := NewMessageRouter(&mockDeviceFinder{})
	defer router.Stop()
	adapter := NewAdapter(router, nil)
	plugin := &tenantAwareIdentityTestPlugin{mockPlugin: mockPlugin{name: "test"}, tenantID: "tenant_a", userID: "u1"}
	if err := adapter.RegisterPlugin(plugin); err != nil {
		t.Fatalf("register plugin: %v", err)
	}

	resolver := NewPluginIdentityResolver(adapter)
	tenantID, userID, err := resolver.ResolveUserWithTenant(WithTenant(context.Background(), "tenant_b"), "test", "uid1")
	if err == nil || !strings.Contains(err.Error(), "hinted tenant") {
		t.Fatalf("expected tenant mismatch error, got tenant=%q user=%q err=%v", tenantID, userID, err)
	}
}

func TestPluginIdentityResolverAcceptsMatchingTenantHint(t *testing.T) {
	router := NewMessageRouter(&mockDeviceFinder{})
	defer router.Stop()
	adapter := NewAdapter(router, nil)
	plugin := &tenantAwareIdentityTestPlugin{mockPlugin: mockPlugin{name: "test"}, tenantID: "tenant_a", userID: "u1"}
	if err := adapter.RegisterPlugin(plugin); err != nil {
		t.Fatalf("register plugin: %v", err)
	}

	resolver := NewPluginIdentityResolver(adapter)
	tenantID, userID, err := resolver.ResolveUserWithTenant(WithTenant(context.Background(), "tenant_a"), "test", "uid1")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if tenantID != "tenant_a" || userID != "u1" {
		t.Fatalf("resolved tenant/user = %q/%q", tenantID, userID)
	}
}
