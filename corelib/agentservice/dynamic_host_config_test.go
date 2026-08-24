package agentservice

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

type fakeHostConfigManager struct {
	principal     Principal
	maxIterations int
	hasMax        bool
	thinkingMode  string
	hasThinking   bool
	result        string
	err           error
}

func (f *fakeHostConfigManager) AdministerReviewedHostConfig(_ context.Context, principal Principal, maxIterations int, hasMax bool, thinkingMode string, hasThinking bool) (string, error) {
	f.principal = principal
	f.maxIterations = maxIterations
	f.hasMax = hasMax
	f.thinkingMode = thinkingMode
	f.hasThinking = hasThinking
	return f.result, f.err
}

func TestReviewedHostConfigExecutesWithoutCoordinatorAndRejectsLookup(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	manager := &fakeHostConfigManager{result: "当前配置:\n- max_iterations: 50\n- thinking_mode: disabled"}
	observed := dynamicCatalogLifecycleForKind("mcp", IncompleteDynamicCatalogLifecycle(coretool.CatalogCoverageReasonNotReady))
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, observed, reviewedHostOwnedServices{Config: manager})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "config", Capability: CapabilityConfigManage, Required: true}},
	})
	if err != nil || len(plan.Selections) != 1 || len(plan.Unmet) != 0 {
		t.Fatalf("config plan=%#v err=%v", plan, err)
	}
	if plan.Selections[0].Provider.Kind != reviewedHostProviderKind || plan.Selections[0].FitProof.MatchedCapability != CapabilityConfigManage {
		t.Fatalf("selection=%#v", plan.Selections[0])
	}
	if !dynamicHostLocalMutationSelection(plan.Selections[0]) || dynamicSelectionRequiresReceipt(plan.Selections[0]) {
		t.Fatalf("host config must use the local mutation receipt, selection=%#v", plan.Selections[0])
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	listed := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{}`)
	if !listed.Succeeded || manager.hasMax || manager.hasThinking {
		t.Fatalf("get result=%#v manager=%#v", listed, manager)
	}
	updated := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"max_iterations":50}`)
	if !updated.Succeeded || !manager.hasMax || manager.maxIterations != 50 || manager.hasThinking {
		t.Fatalf("max_iterations result=%#v manager=%#v", updated, manager)
	}
	thinking := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"thinking_mode":"disabled"}`)
	if !thinking.Succeeded || !manager.hasThinking || manager.thinkingMode != "disabled" {
		t.Fatalf("thinking_mode result=%#v manager=%#v", thinking, manager)
	}
	both := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"max_iterations":50,"thinking_mode":"disabled"}`)
	if both.Succeeded {
		t.Fatalf("max_iterations and thinking_mode together must fail closed, result=%#v", both)
	}
	providerSoup := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"provider":"zhipu"}`)
	if providerSoup.Succeeded || providerSoup.Unknown {
		t.Fatalf("provider switch must fail closed, result=%#v", providerSoup)
	}
	actionSoup := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"action":"set"}`)
	if actionSoup.Succeeded || actionSoup.Unknown {
		t.Fatalf("action soup must fail closed, result=%#v", actionSoup)
	}

	lookupPlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-lookup", TurnID: "turn-lookup", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "lookup", Capability: CapabilityInformationLookup, Required: true}},
	})
	if err != nil || len(lookupPlan.Selections) != 0 {
		t.Fatalf("information.lookup must not be satisfied by host config, plan=%#v err=%v", lookupPlan, err)
	}
	sessionPlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-session", TurnID: "turn-session", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "session", Capability: coretool.CapabilitySessionManageCoding, Required: true}},
	})
	if err != nil || len(sessionPlan.Selections) != 0 {
		t.Fatalf("session.manage.coding must not be satisfied by host config, plan=%#v err=%v", sessionPlan, err)
	}
}

func TestReviewedHostConfigIsAbsentWithoutManager(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{KnowledgeAdmin: &fakeHostKnowledgeAdministrator{}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "config", Capability: CapabilityConfigManage, Required: true}},
	})
	if err != nil || len(plan.Selections) != 0 {
		t.Fatalf("config without manager must stay unmet, plan=%#v err=%v", plan, err)
	}
}

func TestProjectReviewedHostConfigRejectsProviderAndGUIFields(t *testing.T) {
	provider, definition, _, err := ProjectReviewedHostConfigProvider(&fakeHostConfigManager{})
	if err != nil {
		t.Fatal(err)
	}
	if provider.Provides[0].Capability != CapabilityConfigManage || provider.AdapterName == "manage_config" || provider.AdapterName == "switch_llm_provider" {
		t.Fatalf("provider=%#v", provider)
	}
	fn, _ := definition["function"].(map[string]interface{})
	params, _ := fn["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	if _, ok := props["max_iterations"]; !ok || len(props) != 2 {
		t.Fatalf("config schema=%#v", props)
	}
	for _, key := range []string{"action", "provider", "url", "key", "model", "llm_url", "llm_key", "llm_model", "channel", "destination", "export", "import", "profile"} {
		if _, ok := props[key]; ok {
			t.Fatalf("config schema leaked %s", key)
		}
	}
}

func TestReviewedHostConfigUsesServiceAndRejectsProviderSwitch(t *testing.T) {
	executor := &CoreAgentExecutor{}
	svc, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "test"}, NewMemoryStore(), executor)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	tenant, err := svc.CreateTenant(context.Background(), CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatal(err)
	}
	user, err := svc.CreateUser(context.Background(), CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatal(err)
	}
	principal := Principal{TenantID: tenant.ID, UserID: user.ID}
	listed, err := svc.AdministerReviewedHostConfig(context.Background(), principal, 0, false, "", false)
	if err != nil || !strings.Contains(listed, "max_iterations") || strings.Contains(listed, "http") || strings.Contains(listed, "key") {
		t.Fatalf("get=%q err=%v", listed, err)
	}
	updated, err := svc.AdministerReviewedHostConfig(context.Background(), principal, 50, true, "", false)
	if err != nil || !strings.Contains(updated, "50") {
		t.Fatalf("set iterations=%q err=%v", updated, err)
	}
	cfg, err := svc.GetUserConfig(context.Background(), principal)
	if err != nil || cfg.AppConfig.MaclawAgentMaxIterations != 50 {
		t.Fatalf("persisted iterations=%#v err=%v", cfg, err)
	}
	if _, err := svc.AdministerReviewedHostConfig(context.Background(), principal, 10, true, "", false); err == nil {
		t.Fatal("max_iterations below minimum must fail closed")
	}
	if _, err := svc.AdministerReviewedHostConfig(context.Background(), principal, 0, false, "zhipu", true); err == nil {
		t.Fatal("unknown thinking_mode must fail closed")
	}
	if _, err := svc.AdministerReviewedHostConfig(context.Background(), Principal{TenantID: tenant.ID, UserID: "other"}, 50, true, "", false); err == nil {
		t.Fatal("missing user must fail closed")
	}
	services := (&coreAgentCallbacks{principal: principal}).reviewedHostOwnedServices()
	if services.Config != nil {
		t.Fatal("host without a config manager must not attach config.manage.self")
	}
}

func TestReviewedHostConfigProjectionHidesSecrets(t *testing.T) {
	got := reviewedHostConfigProjection(corelib.AppConfig{
		MaclawAgentMaxIterations: 80,
		MaclawLLMThinkingMode:    "disabled",
		MaclawLLMUrl:             "http://secret.example/v1",
		MaclawLLMKey:             "sk-secret",
		MaclawLLMModel:           "secret-model",
	})
	if strings.Contains(got, "secret") || strings.Contains(got, "http") || strings.Contains(got, "sk-") {
		t.Fatalf("projection leaked secrets: %q", got)
	}
	if !strings.Contains(got, "80") || !strings.Contains(got, "disabled") {
		t.Fatalf("projection=%q", got)
	}
}
