package im

import (
	"context"
	"fmt"
)

// PluginIdentityResolver implements IdentityResolver by delegating to the
// registered IM plugins' ResolveUser methods. It looks up the plugin by
// platform name and calls its ResolveUser.
type PluginIdentityResolver struct {
	adapter *Adapter
}

// NewPluginIdentityResolver creates an IdentityResolver that delegates to
// the IM Adapter's registered plugins.
func NewPluginIdentityResolver(adapter *Adapter) *PluginIdentityResolver {
	return &PluginIdentityResolver{adapter: adapter}
}

// ResolveUser maps a platform-specific user ID to a unified internal user ID
// by delegating to the appropriate IM plugin.
func (r *PluginIdentityResolver) ResolveUser(ctx context.Context, platformName, platformUID string) (string, error) {
	_, userID, err := r.ResolveUserWithTenant(ctx, platformName, platformUID)
	return userID, err
}

// ResolveUserWithTenant maps a platform-specific user ID to tenant/user IDs.
func (r *PluginIdentityResolver) ResolveUserWithTenant(ctx context.Context, platformName, platformUID string) (string, string, error) {
	plugin := r.adapter.GetPlugin(platformName)
	if plugin == nil {
		return "", "", fmt.Errorf("im: no plugin registered for platform %q", platformName)
	}
	if tenantAware, ok := plugin.(interface {
		ResolveUserWithTenant(context.Context, string) (string, string, error)
	}); ok {
		return tenantAware.ResolveUserWithTenant(ctx, platformUID)
	}
	userID, err := plugin.ResolveUser(ctx, platformUID)
	return tenantIDFromContext(ctx), userID, err
}
