package agentservice

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestDynamicCapabilityRegistryScopesMCPAndSkillContracts(t *testing.T) {
	registry := NewDynamicCapabilityRegistry()
	a := Principal{TenantID: "tenant-a", UserID: "user-a"}
	b := Principal{TenantID: "tenant-b", UserID: "user-b"}
	contract := DynamicCapabilityContract{
		Provisions: []coretool.CapabilityProvision{{Capability: "test.dynamic.execute", Quality: 1}},
		Effects:    []coretool.EffectClass{coretool.EffectReadOnly},
	}
	if err := registry.PublishMCPContract(a, "server", "lookup", contract); err != nil {
		t.Fatalf("PublishMCPContract: %v", err)
	}
	if err := registry.PublishSkillContract(a, "acme.lookup", contract); err != nil {
		t.Fatalf("PublishSkillContract: %v", err)
	}
	if got, ok := registry.ResolveMCPDynamicContract(context.Background(), a, "server", "lookup"); !ok || got.Digest() != contract.Digest() {
		t.Fatalf("MCP contract=%#v ok=%v", got, ok)
	}
	if got, ok := registry.ResolveSkillDynamicContract(context.Background(), a, "acme.lookup"); !ok || got.Digest() != contract.Digest() {
		t.Fatalf("Skill contract=%#v ok=%v", got, ok)
	}
	if _, ok := registry.ResolveMCPDynamicContract(context.Background(), b, "server", "lookup"); ok {
		t.Fatal("MCP contract leaked across principal scope")
	}
	if _, ok := registry.ResolveSkillDynamicContract(context.Background(), b, "acme.lookup"); ok {
		t.Fatal("Skill contract leaked across principal scope")
	}
	if err := registry.RevokeMCPContract(a, "server", "lookup"); err != nil {
		t.Fatalf("RevokeMCPContract: %v", err)
	}
	if err := registry.RevokeSkillContract(a, "acme.lookup"); err != nil {
		t.Fatalf("RevokeSkillContract: %v", err)
	}
	if _, ok := registry.ResolveMCPDynamicContract(context.Background(), a, "server", "lookup"); ok {
		t.Fatal("revoked MCP contract remained resolvable")
	}
	if _, ok := registry.ResolveSkillDynamicContract(context.Background(), a, "acme.lookup"); ok {
		t.Fatal("revoked Skill contract remained resolvable")
	}
}

func TestDynamicCapabilityRegistryRejectsUnscopedOrInvalidPublication(t *testing.T) {
	registry := NewDynamicCapabilityRegistry()
	contract := DynamicCapabilityContract{Provisions: []coretool.CapabilityProvision{{Capability: "test.dynamic.execute"}}, Effects: []coretool.EffectClass{coretool.EffectReadOnly}}
	if err := registry.PublishMCPContract(Principal{}, "server", "tool", contract); err == nil {
		t.Fatal("unscoped MCP publication was accepted")
	}
	if err := registry.PublishSkillContract(Principal{TenantID: "tenant", UserID: "user"}, "acme.skill", DynamicCapabilityContract{}); err == nil {
		t.Fatal("undeclared Skill contract was accepted")
	}
}

func TestDynamicCapabilityRegistryClearPrincipalDoesNotLeakOrRetainContracts(t *testing.T) {
	registry := NewDynamicCapabilityRegistry()
	a := Principal{TenantID: "tenant-a", UserID: "user-a"}
	b := Principal{TenantID: "tenant-b", UserID: "user-b"}
	contract := DynamicCapabilityContract{Provisions: []coretool.CapabilityProvision{{Capability: "test.dynamic.execute"}}, Effects: []coretool.EffectClass{coretool.EffectReadOnly}}
	for _, p := range []Principal{a, b} {
		if err := registry.PublishMCPContract(p, "server", "lookup", contract); err != nil {
			t.Fatal(err)
		}
	}
	if err := registry.ClearPrincipal(a); err != nil {
		t.Fatalf("ClearPrincipal: %v", err)
	}
	if _, ok := registry.ResolveMCPDynamicContract(context.Background(), a, "server", "lookup"); ok {
		t.Fatal("principal contract survived ClearPrincipal")
	}
	if _, ok := registry.ResolveMCPDynamicContract(context.Background(), b, "server", "lookup"); !ok {
		t.Fatal("ClearPrincipal removed another principal's contract")
	}
}

func TestDynamicCapabilityRegistryRevokesOnlyOneMCPServerScope(t *testing.T) {
	registry := NewDynamicCapabilityRegistry()
	p := Principal{TenantID: "tenant", UserID: "user"}
	contract := testDynamicCapabilityContract()
	for _, binding := range [][2]string{{"server-a", "first"}, {"server-a", "second"}, {"server-b", "first"}} {
		if err := registry.PublishMCPContract(p, binding[0], binding[1], contract); err != nil {
			t.Fatal(err)
		}
	}
	if err := registry.RevokeMCPServerContracts(p, "server-a"); err != nil {
		t.Fatalf("RevokeMCPServerContracts: %v", err)
	}
	for _, toolName := range []string{"first", "second"} {
		if _, ok := registry.ResolveMCPDynamicContract(context.Background(), p, "server-a", toolName); ok {
			t.Fatalf("revoked server-a tool %q remained routable", toolName)
		}
	}
	if _, ok := registry.ResolveMCPDynamicContract(context.Background(), p, "server-b", "first"); !ok {
		t.Fatal("server-scoped revocation removed another MCP server contract")
	}
}

func TestDynamicCapabilityContractSnapshotIsPrincipalScopedAndImmutable(t *testing.T) {
	registry := NewDynamicCapabilityRegistry()
	p := Principal{TenantID: "tenant", UserID: "user"}
	first := testDynamicCapabilityContract()
	if err := registry.PublishMCPContract(p, "server", "lookup", first); err != nil {
		t.Fatal(err)
	}
	snapshot, err := registry.SnapshotDynamicCapabilityContracts(p)
	if err != nil {
		t.Fatal(err)
	}
	updated := first
	updated.Effects = []coretool.EffectClass{coretool.EffectExternalEffect}
	if err := registry.PublishMCPContract(p, "server", "lookup", updated); err != nil {
		t.Fatal(err)
	}
	got, ok := snapshot.ResolveMCPDynamicContract(context.Background(), p, "server", "lookup")
	if !ok || got.Digest() != first.Digest() {
		t.Fatalf("snapshot did not retain first publication: %#v ok=%v", got, ok)
	}
	if _, ok := snapshot.ResolveMCPDynamicContract(context.Background(), Principal{TenantID: "tenant", UserID: "other"}, "server", "lookup"); ok {
		t.Fatal("snapshot leaked contract across principal")
	}
}

func TestSQLiteDynamicCapabilityRegistryPersistsScopesAndRevokes(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "capabilities", "contracts.db")
	a := Principal{TenantID: "tenant-a", UserID: "user-a"}
	b := Principal{TenantID: "tenant-b", UserID: "user-b"}
	contract := testDynamicCapabilityContract()
	registry, err := NewSQLiteDynamicCapabilityRegistry(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteDynamicCapabilityRegistry: %v", err)
	}
	if err := registry.PublishMCPContract(a, "server", "lookup", contract); err != nil {
		t.Fatalf("PublishMCPContract: %v", err)
	}
	if err := registry.PublishSkillContract(a, "acme.lookup", contract); err != nil {
		t.Fatalf("PublishSkillContract: %v", err)
	}
	if err := registry.PublishMCPContract(b, "server", "lookup", contract); err != nil {
		t.Fatalf("PublishMCPContract other principal: %v", err)
	}
	if err := registry.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := NewSQLiteDynamicCapabilityRegistry(dbPath)
	if err != nil {
		t.Fatalf("reopen registry: %v", err)
	}
	defer reopened.Close()
	if got, ok := reopened.ResolveMCPDynamicContract(context.Background(), a, "server", "lookup"); !ok || got.Digest() != contract.Digest() {
		t.Fatalf("persisted MCP contract=%#v ok=%v", got, ok)
	}
	if got, ok := reopened.ResolveSkillDynamicContract(context.Background(), a, "acme.lookup"); !ok || got.Digest() != contract.Digest() {
		t.Fatalf("persisted Skill contract=%#v ok=%v", got, ok)
	}
	if _, ok := reopened.ResolveSkillDynamicContract(context.Background(), b, "acme.lookup"); ok {
		t.Fatal("Skill contract leaked across principal scope")
	}
	if err := reopened.RevokeMCPContract(a, "server", "lookup"); err != nil {
		t.Fatalf("RevokeMCPContract: %v", err)
	}
	if err := reopened.ClearPrincipal(a); err != nil {
		t.Fatalf("ClearPrincipal: %v", err)
	}
	if _, ok := reopened.ResolveMCPDynamicContract(context.Background(), a, "server", "lookup"); ok {
		t.Fatal("revoked MCP contract remained resolvable")
	}
	if _, ok := reopened.ResolveSkillDynamicContract(context.Background(), a, "acme.lookup"); ok {
		t.Fatal("cleared Skill contract remained resolvable")
	}
	if _, ok := reopened.ResolveMCPDynamicContract(context.Background(), b, "server", "lookup"); !ok {
		t.Fatal("ClearPrincipal removed another principal's durable contract")
	}
}

func TestSQLiteDynamicCapabilityContractSnapshotQuarantinesCorruptRows(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "contracts.db")
	p := Principal{TenantID: "tenant", UserID: "user"}
	registry, err := NewSQLiteDynamicCapabilityRegistry(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	if err := registry.PublishMCPContract(p, "valid", "lookup", testDynamicCapabilityContract()); err != nil {
		t.Fatal(err)
	}
	if err := registry.PublishMCPContract(p, "corrupt", "lookup", testDynamicCapabilityContract()); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.db.Exec(`UPDATE dynamic_capability_contracts SET contract_digest = ? WHERE tenant_id = ? AND user_id = ? AND binding_key = ?`, "tampered", "tenant", "user", dynamicContractBindingKey("corrupt", "lookup")); err != nil {
		t.Fatal(err)
	}
	snapshot, err := registry.SnapshotDynamicCapabilityContracts(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := snapshot.ResolveMCPDynamicContract(context.Background(), p, "corrupt", "lookup"); ok {
		t.Fatal("corrupt contract was included in snapshot")
	}
	if _, ok := snapshot.ResolveMCPDynamicContract(context.Background(), p, "valid", "lookup"); !ok {
		t.Fatal("valid contract disappeared from snapshot")
	}
}

func TestSQLiteDynamicCapabilityRegistryFailsClosedForCorruptRow(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "contracts.db")
	p := Principal{TenantID: "tenant", UserID: "user"}
	registry, err := NewSQLiteDynamicCapabilityRegistry(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteDynamicCapabilityRegistry: %v", err)
	}
	defer registry.Close()
	if err := registry.PublishMCPContract(p, "server", "lookup", testDynamicCapabilityContract()); err != nil {
		t.Fatalf("PublishMCPContract: %v", err)
	}
	if _, err := registry.db.Exec(`UPDATE dynamic_capability_contracts SET contract_digest = ? WHERE tenant_id = ? AND user_id = ?`, "tampered", "tenant", "user"); err != nil {
		t.Fatalf("tamper row: %v", err)
	}
	if _, ok := registry.ResolveMCPDynamicContract(context.Background(), p, "server", "lookup"); ok {
		t.Fatal("corrupt contract row was routable")
	}
}

func TestServiceDurablyStoresAndInvalidatesDynamicCapabilityContracts(t *testing.T) {
	dataRoot := t.TempDir()
	p := Principal{TenantID: "tenant", UserID: "user"}
	service, err := NewService(Config{DataRoot: dataRoot}, nil, EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if err := service.EnsurePrincipal(context.Background(), p, "user@example.test", "User"); err != nil {
		t.Fatalf("EnsurePrincipal: %v", err)
	}
	contract := testDynamicCapabilityContract()
	if err := service.dynamicCapabilities.PublishMCPContract(p, "server", "lookup", contract); err != nil {
		t.Fatalf("PublishMCPContract: %v", err)
	}
	if err := service.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	restarted, err := NewService(Config{DataRoot: dataRoot}, nil, EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService restart: %v", err)
	}
	defer restarted.Close()
	if _, ok := restarted.DynamicCapabilityContracts().ResolveMCPDynamicContract(context.Background(), p, "server", "lookup"); !ok {
		t.Fatal("file-backed Service lost dynamic contract after restart")
	}
	if _, err := restarted.UpdateUserConfig(context.Background(), p, corelib.AppConfig{MaclawLLMUrl: "https://llm.example/v1", MaclawLLMKey: "key", MaclawLLMModel: "model"}); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	if _, ok := restarted.DynamicCapabilityContracts().ResolveMCPDynamicContract(context.Background(), p, "server", "lookup"); ok {
		t.Fatal("config update retained stale dynamic contract")
	}
}

func TestMCPConfigurationLifecycleRevokesServerContracts(t *testing.T) {
	service, err := NewService(Config{DataRoot: t.TempDir()}, nil, EchoExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	p := Principal{TenantID: "tenant", UserID: "user"}
	if err := service.EnsurePrincipal(context.Background(), p, "user@example.test", "User"); err != nil {
		t.Fatal(err)
	}
	server, err := service.CreateMCPServer(context.Background(), p, MCPServerCreateInput{Kind: "local", Name: "original", Command: "untrusted-mcp"})
	if err != nil {
		t.Fatalf("CreateMCPServer: %v", err)
	}
	if err := service.dynamicCapabilities.PublishMCPContract(p, server.ID, "lookup", testDynamicCapabilityContract()); err != nil {
		t.Fatal(err)
	}
	nextName := "changed"
	if _, err := service.UpdateMCPServer(context.Background(), p, server.ID, MCPServerUpdateInput{Name: &nextName}); err != nil {
		t.Fatalf("UpdateMCPServer: %v", err)
	}
	if _, ok := service.DynamicCapabilityContracts().ResolveMCPDynamicContract(context.Background(), p, server.ID, "lookup"); ok {
		t.Fatal("updated MCP server retained its old dynamic contract")
	}
	if err := service.dynamicCapabilities.PublishMCPContract(p, server.ID, "lookup", testDynamicCapabilityContract()); err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteMCPServer(context.Background(), p, server.ID); err != nil {
		t.Fatalf("DeleteMCPServer: %v", err)
	}
	if _, ok := service.DynamicCapabilityContracts().ResolveMCPDynamicContract(context.Background(), p, server.ID, "lookup"); ok {
		t.Fatal("deleted MCP server retained its dynamic contract")
	}
}

func TestUserDeletionLifecycleClearsDynamicContracts(t *testing.T) {
	service, err := NewService(Config{DataRoot: t.TempDir()}, nil, EchoExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	p := Principal{TenantID: "tenant", UserID: "user"}
	if err := service.EnsurePrincipal(context.Background(), p, "user@example.test", "User"); err != nil {
		t.Fatal(err)
	}
	if err := service.dynamicCapabilities.PublishSkillContract(p, "acme.lookup", testDynamicCapabilityContract()); err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteUser(context.Background(), p.TenantID, p.UserID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if _, ok := service.DynamicCapabilityContracts().ResolveSkillDynamicContract(context.Background(), p, "acme.lookup"); ok {
		t.Fatal("deleted user retained a dynamic Skill contract")
	}
}

func TestTenantDeletionLifecycleClearsEveryPrincipalContract(t *testing.T) {
	service, err := NewService(Config{DataRoot: t.TempDir()}, nil, EchoExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	first := Principal{TenantID: "tenant", UserID: "first"}
	second := Principal{TenantID: "tenant", UserID: "second"}
	for _, p := range []Principal{first, second} {
		if err := service.EnsurePrincipal(context.Background(), p, p.UserID+"@example.test", p.UserID); err != nil {
			t.Fatal(err)
		}
		if err := service.dynamicCapabilities.PublishSkillContract(p, "acme.lookup", testDynamicCapabilityContract()); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.DeleteTenant(context.Background(), "tenant"); err != nil {
		t.Fatalf("DeleteTenant: %v", err)
	}
	for _, p := range []Principal{first, second} {
		if _, ok := service.DynamicCapabilityContracts().ResolveSkillDynamicContract(context.Background(), p, "acme.lookup"); ok {
			t.Fatalf("deleted tenant principal %q retained a dynamic Skill contract", p.UserID)
		}
	}
}

func TestServiceOwnsDynamicCapabilityRegistry(t *testing.T) {
	svc, err := NewService(Config{DataRoot: t.TempDir()}, NewMemoryStore(), EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if svc.DynamicCapabilityContracts() == nil {
		t.Fatal("Service did not initialize dynamic capability registry")
	}
	if NewMCPToolBridge(svc).contracts != svc.DynamicCapabilityContracts() {
		t.Fatal("MCP bridge is not wired to Service registry")
	}
	if NewSkillToolBridge(svc).contracts != svc.DynamicCapabilityContracts() {
		t.Fatal("Skill bridge is not wired to Service registry")
	}
}
