package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type tenantIMRuntimeTestPlugin struct {
	name      string
	startErr  error
	stopCount int
}

func (p *tenantIMRuntimeTestPlugin) Name() string                            { return p.name }
func (p *tenantIMRuntimeTestPlugin) ReceiveMessage(func(im.IncomingMessage)) {}
func (p *tenantIMRuntimeTestPlugin) SendText(context.Context, im.UserTarget, string) error {
	return nil
}
func (p *tenantIMRuntimeTestPlugin) SendCard(context.Context, im.UserTarget, im.OutgoingMessage) error {
	return nil
}
func (p *tenantIMRuntimeTestPlugin) SendImage(context.Context, im.UserTarget, string, string) error {
	return nil
}
func (p *tenantIMRuntimeTestPlugin) SendFile(context.Context, im.UserTarget, string, string, string) error {
	return nil
}
func (p *tenantIMRuntimeTestPlugin) ResolveUser(context.Context, string) (string, error) {
	return "", nil
}
func (p *tenantIMRuntimeTestPlugin) Capabilities() im.CapabilityDeclaration {
	return im.CapabilityDeclaration{}
}
func (p *tenantIMRuntimeTestPlugin) Start(context.Context) error { return p.startErr }
func (p *tenantIMRuntimeTestPlugin) Stop(context.Context) error  { p.stopCount++; return nil }

type tenantIMRuntimeTestSettings struct{}

func (tenantIMRuntimeTestSettings) Set(context.Context, string, string) error   { return nil }
func (tenantIMRuntimeTestSettings) Get(context.Context, string) (string, error) { return "", nil }

type tenantIMRuntimeTestUsers struct{}

func (tenantIMRuntimeTestUsers) Create(context.Context, *store.User) error { return nil }
func (tenantIMRuntimeTestUsers) GetByID(context.Context, string) (*store.User, error) {
	return nil, nil
}
func (tenantIMRuntimeTestUsers) GetByEmail(context.Context, string) (*store.User, error) {
	return nil, nil
}
func (tenantIMRuntimeTestUsers) GetByTenantEmail(context.Context, string, string) (*store.User, error) {
	return nil, nil
}
func (tenantIMRuntimeTestUsers) List(context.Context) ([]*store.User, error) { return nil, nil }
func (tenantIMRuntimeTestUsers) ListByTenant(context.Context, string) ([]*store.User, error) {
	return nil, nil
}
func (tenantIMRuntimeTestUsers) DeleteByEmail(context.Context, string) error { return nil }
func (tenantIMRuntimeTestUsers) DeleteByTenantEmail(context.Context, string, string) error {
	return nil
}
func (tenantIMRuntimeTestUsers) UpdateSmartRoute(context.Context, string, bool) error { return nil }
func (tenantIMRuntimeTestUsers) MarkEmailVerified(context.Context, string, string) error {
	return nil
}

type tenantIMRuntimeTestTenants struct{ items []*store.Tenant }

func (r tenantIMRuntimeTestTenants) Create(context.Context, *store.Tenant) error { return nil }
func (r tenantIMRuntimeTestTenants) GetByID(context.Context, string) (*store.Tenant, error) {
	return nil, nil
}
func (r tenantIMRuntimeTestTenants) GetBySlug(context.Context, string) (*store.Tenant, error) {
	return nil, nil
}
func (r tenantIMRuntimeTestTenants) List(context.Context) ([]*store.Tenant, error) {
	return r.items, nil
}
func (r tenantIMRuntimeTestTenants) EnsureDefault(context.Context) (*store.Tenant, error) {
	return nil, nil
}
func (r tenantIMRuntimeTestTenants) DeleteByID(context.Context, string) error { return nil }

func TestTenantNativeIMRuntimeReloadStopsDisabledRuntime(t *testing.T) {
	adapter := im.NewAdapter(nil, nil)
	manager := newTenantNativeIMRuntimeManager(adapter, tenantIMRuntimeTestSettings{}, tenantIMRuntimeTestUsers{}, nil, nil, "")
	plugin := &tenantIMRuntimeTestPlugin{name: "qqbot"}
	if err := adapter.RegisterTenantPlugin("tenant_a", plugin); err != nil {
		t.Fatalf("RegisterTenantPlugin: %v", err)
	}
	manager.runtimes["tenant_a"] = map[string]im.IMPlugin{"qqbot": plugin}

	if err := manager.ReloadTenantIM(context.Background(), "tenant_a", "qqbot"); err != nil {
		t.Fatalf("ReloadTenantIM: %v", err)
	}
	if plugin.stopCount != 1 {
		t.Fatalf("stopCount = %d, want 1", plugin.stopCount)
	}
	if got := adapter.GetPluginForTenant("tenant_a", "qqbot"); got != nil {
		t.Fatalf("tenant plugin still registered: %#v", got)
	}
}

func TestTenantNativeIMRuntimeStartFailureUnregistersPlugin(t *testing.T) {
	adapter := im.NewAdapter(nil, nil)
	manager := newTenantNativeIMRuntimeManager(adapter, tenantIMRuntimeTestSettings{}, tenantIMRuntimeTestUsers{}, nil, nil, "")
	plugin := &tenantIMRuntimeTestPlugin{name: "qqbot", startErr: errors.New("boom")}
	if err := manager.registerAndStartTenantPluginLocked(context.Background(), "tenant_a", "qqbot", plugin); err == nil {
		t.Fatal("expected start error")
	}
	if plugin.stopCount != 1 {
		t.Fatalf("stopCount = %d, want 1", plugin.stopCount)
	}
	if got := adapter.GetPluginForTenant("tenant_a", "qqbot"); got != nil {
		t.Fatalf("failed tenant plugin still registered: %#v", got)
	}
	if len(manager.runtimes) != 0 {
		t.Fatalf("failed runtime tracked: %#v", manager.runtimes)
	}
}

func TestTenantNativeIMRuntimeReloadAllSkipsInactiveAndDefaultTenants(t *testing.T) {
	adapter := im.NewAdapter(nil, nil)
	manager := newTenantNativeIMRuntimeManager(adapter, tenantIMRuntimeTestSettings{}, tenantIMRuntimeTestUsers{}, nil, nil, "")
	deletedAt := time.Now()
	manager.ReloadAll(context.Background(), tenantIMRuntimeTestTenants{items: []*store.Tenant{
		{ID: store.DefaultTenantID, Status: "active"},
		{ID: "tenant_inactive", Status: "disabled"},
		{ID: "tenant_deleted", Status: "active", DeletedAt: &deletedAt},
	}})
	if len(manager.runtimes) != 0 {
		t.Fatalf("unexpected runtimes: %#v", manager.runtimes)
	}
}

func TestTenantNativeIMRuntimeReloadAllStopsInactiveAndMissingTenants(t *testing.T) {
	adapter := im.NewAdapter(nil, nil)
	manager := newTenantNativeIMRuntimeManager(adapter, tenantIMRuntimeTestSettings{}, tenantIMRuntimeTestUsers{}, nil, nil, "")
	inactive := &tenantIMRuntimeTestPlugin{name: "qqbot"}
	missing := &tenantIMRuntimeTestPlugin{name: "wecom"}
	if err := adapter.RegisterTenantPlugin("tenant_inactive", inactive); err != nil {
		t.Fatalf("RegisterTenantPlugin inactive: %v", err)
	}
	if err := adapter.RegisterTenantPlugin("tenant_missing", missing); err != nil {
		t.Fatalf("RegisterTenantPlugin missing: %v", err)
	}
	manager.runtimes["tenant_inactive"] = map[string]im.IMPlugin{"qqbot": inactive}
	manager.runtimes["tenant_missing"] = map[string]im.IMPlugin{"wecom": missing}

	manager.ReloadAll(context.Background(), tenantIMRuntimeTestTenants{items: []*store.Tenant{{ID: "tenant_inactive", Status: "disabled"}}})
	if inactive.stopCount != 1 || missing.stopCount != 1 {
		t.Fatalf("stop counts inactive=%d missing=%d, want 1/1", inactive.stopCount, missing.stopCount)
	}
	if got := adapter.GetPluginForTenant("tenant_inactive", "qqbot"); got != nil {
		t.Fatalf("inactive runtime still registered: %#v", got)
	}
	if got := adapter.GetPluginForTenant("tenant_missing", "wecom"); got != nil {
		t.Fatalf("missing runtime still registered: %#v", got)
	}
	if len(manager.runtimes) != 0 {
		t.Fatalf("unexpected runtimes: %#v", manager.runtimes)
	}
}
