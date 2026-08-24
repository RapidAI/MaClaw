package agentservice

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/remote"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

type fakeHostTemplateManager struct {
	name      string
	tool      string
	principal Principal
	result    string
	err       error
}

func (f *fakeHostTemplateManager) ManageReviewedHostTemplate(_ context.Context, principal Principal, name, tool string) (string, error) {
	f.principal = principal
	f.name = name
	f.tool = tool
	return f.result, f.err
}

func TestReviewedHostTemplateExecutesWithoutCoordinatorAndRejectsSessionMapping(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	manager := &fakeHostTemplateManager{result: "模板已创建: codex-default（工具=codex）"}
	observed := dynamicCatalogLifecycleForKind("mcp", IncompleteDynamicCatalogLifecycle(coretool.CatalogCoverageReasonNotReady))
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, observed, reviewedHostOwnedServices{Template: manager})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "template", Capability: CapabilityTemplateManage, Required: true}},
	})
	if err != nil || len(plan.Selections) != 1 || len(plan.Unmet) != 0 {
		t.Fatalf("template plan=%#v err=%v", plan, err)
	}
	if plan.Selections[0].Provider.Kind != reviewedHostProviderKind || plan.Selections[0].FitProof.MatchedCapability != CapabilityTemplateManage {
		t.Fatalf("selection=%#v", plan.Selections[0])
	}
	if !dynamicHostLocalMutationSelection(plan.Selections[0]) || dynamicSelectionRequiresReceipt(plan.Selections[0]) {
		t.Fatalf("host template must use the local mutation receipt, not the external coordinator, selection=%#v", plan.Selections[0])
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	created := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"name":"codex-default","coding_tool":"codex"}`)
	if !created.Succeeded || created.Result != manager.result || created.Unknown {
		t.Fatalf("template create result=%#v", created)
	}
	if manager.name != "codex-default" || manager.tool != "codex" || manager.principal.TenantID != principal.TenantID || manager.principal.UserID != principal.UserID {
		t.Fatalf("manager=%#v", manager)
	}
	got := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"name":"codex-default"}`)
	if !got.Succeeded || manager.name != "codex-default" || manager.tool != "" {
		t.Fatalf("template get result=%#v manager=%#v", got, manager)
	}
	listed := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{}`)
	if !listed.Succeeded || manager.name != "" || manager.tool != "" {
		t.Fatalf("template list result=%#v manager=%#v", listed, manager)
	}
	toolOnly := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"coding_tool":"codex"}`)
	if toolOnly.Succeeded {
		t.Fatalf("coding_tool without name must fail closed, result=%#v", toolOnly)
	}
	reservedTool := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"name":"codex-default","tool":"codex"}`)
	if reservedTool.Succeeded || reservedTool.Unknown {
		t.Fatalf("reserved tool field must fail closed, result=%#v", reservedTool)
	}
	rejected := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"name":"codex-default","coding_tool":"codex","yolo_mode":true}`)
	if rejected.Succeeded || rejected.Unknown {
		t.Fatalf("yolo_mode must fail closed, result=%#v", rejected)
	}
	actionSoup := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"action":"launch"}`)
	if actionSoup.Succeeded || actionSoup.Unknown {
		t.Fatalf("action soup must fail closed, result=%#v", actionSoup)
	}

	sessionPlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-session", TurnID: "turn-session", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "session", Capability: coretool.CapabilitySessionManageCoding, Required: true}},
	})
	if err != nil || len(sessionPlan.Selections) != 0 {
		t.Fatalf("session.manage.coding must not be satisfied by host template, plan=%#v err=%v", sessionPlan, err)
	}
	configPlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-config", TurnID: "turn-config", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "config", Capability: coretool.CapabilityConfigManageSelf, Required: true}},
	})
	if err != nil || len(configPlan.Selections) != 0 {
		t.Fatalf("config.manage.self must not be satisfied by host template, plan=%#v err=%v", configPlan, err)
	}
}

func TestReviewedHostTemplateIsAbsentWithoutManager(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{Goal: &fakeHostGoalManager{}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "template", Capability: CapabilityTemplateManage, Required: true}},
	})
	if err != nil || len(plan.Selections) != 0 {
		t.Fatalf("template without manager must stay unmet, plan=%#v err=%v", plan, err)
	}
}

func TestProjectReviewedHostTemplateRejectsLaunchAndActionFields(t *testing.T) {
	provider, definition, _, err := ProjectReviewedHostTemplateProvider(&fakeHostTemplateManager{})
	if err != nil {
		t.Fatal(err)
	}
	if provider.Provides[0].Capability != CapabilityTemplateManage || provider.AdapterName == "manage_template" {
		t.Fatalf("provider=%#v", provider)
	}
	fn, _ := definition["function"].(map[string]interface{})
	params, _ := fn["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	if _, ok := props["name"]; !ok {
		t.Fatalf("template schema missing name: %#v", props)
	}
	if _, ok := props["coding_tool"]; !ok || len(props) != 2 {
		t.Fatalf("template schema=%#v", props)
	}
	for _, key := range []string{"action", "launch", "tool", "tool_name", "yolo_mode", "model_config", "env_vars", "project_path", "template_name", "channel", "destination", "id"} {
		if _, ok := props[key]; ok {
			t.Fatalf("template schema leaked %s", key)
		}
	}
}

func TestReviewedHostTemplateUsesTrustedPrincipalAndStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session_templates.json")
	store, err := remote.NewSessionTemplateManager(path)
	if err != nil {
		t.Fatal(err)
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	other := Principal{TenantID: "tenant", UserID: "other"}
	cb := &coreAgentCallbacks{principal: principal, templates: store}
	out, err := cb.ManageReviewedHostTemplate(context.Background(), principal, "codex-default", "codex")
	if err != nil || !strings.Contains(out, "codex-default") || !strings.Contains(out, "codex") {
		t.Fatalf("create=%q err=%v", out, err)
	}
	if _, err := cb.ManageReviewedHostTemplate(context.Background(), principal, "codex-default", "codex"); err == nil {
		t.Fatal("duplicate template must fail closed")
	}
	got, err := cb.ManageReviewedHostTemplate(context.Background(), principal, "codex-default", "")
	if err != nil || !strings.Contains(got, "codex-default") {
		t.Fatalf("get=%q err=%v", got, err)
	}
	if _, err := cb.ManageReviewedHostTemplate(context.Background(), principal, "", "codex"); err == nil {
		t.Fatal("coding_tool without name must fail closed")
	}
	if _, err := cb.ManageReviewedHostTemplate(context.Background(), other, "other", "codex"); err == nil {
		t.Fatal("principal mismatch must fail closed")
	}
	otherStore, err := remote.NewSessionTemplateManager(filepath.Join(t.TempDir(), "session_templates.json"))
	if err != nil {
		t.Fatal(err)
	}
	otherCB := &coreAgentCallbacks{principal: other, templates: otherStore}
	hidden, err := otherCB.ManageReviewedHostTemplate(context.Background(), other, "", "")
	if err != nil || strings.Contains(hidden, "codex-default") {
		t.Fatalf("other principal must not see a foreign template store: %q err=%v", hidden, err)
	}
	listed, err := cb.ManageReviewedHostTemplate(context.Background(), principal, "", "")
	if err != nil || !strings.Contains(listed, "codex-default") {
		t.Fatalf("list=%q err=%v", listed, err)
	}
	services := (&coreAgentCallbacks{principal: principal}).reviewedHostOwnedServices()
	if services.Template != nil {
		t.Fatal("host without a template manager must not attach template.manage.session")
	}
}

func TestTemplateManagerForDataDirRequiresPersistPath(t *testing.T) {
	e := &CoreAgentExecutor{}
	if e.templateManagerForDataDir("") != nil {
		t.Fatal("empty dataDir must not attach template.manage.session")
	}
	dir := t.TempDir()
	if e.templateManagerForDataDir(dir) == nil {
		t.Fatal("non-empty dataDir must open a template manager")
	}
}
