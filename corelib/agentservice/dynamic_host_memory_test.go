package agentservice

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/memory"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

type fakeHostMemoryManager struct {
	content   string
	query     string
	id        string
	principal Principal
	result    string
	err       error
}

func (f *fakeHostMemoryManager) ManageReviewedHostMemory(_ context.Context, principal Principal, content, query, id string) (string, error) {
	f.principal = principal
	f.content = content
	f.query = query
	f.id = id
	return f.result, f.err
}

func TestReviewedHostMemoryExecutesWithoutCoordinatorAndRejectsKnowledgeMapping(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	manager := &fakeHostMemoryManager{result: "Memory saved: prefer Chinese"}
	observed := dynamicCatalogLifecycleForKind("mcp", IncompleteDynamicCatalogLifecycle(coretool.CatalogCoverageReasonNotReady))
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, observed, reviewedHostOwnedServices{Memory: manager})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "memory", Capability: CapabilityMemoryManage, Required: true}},
	})
	if err != nil || len(plan.Selections) != 1 || len(plan.Unmet) != 0 {
		t.Fatalf("memory plan=%#v err=%v", plan, err)
	}
	if plan.Selections[0].Provider.Kind != reviewedHostProviderKind || plan.Selections[0].FitProof.MatchedCapability != CapabilityMemoryManage {
		t.Fatalf("selection=%#v", plan.Selections[0])
	}
	if !dynamicHostLocalMutationSelection(plan.Selections[0]) || dynamicSelectionRequiresReceipt(plan.Selections[0]) {
		t.Fatalf("host memory must use the local mutation receipt, not the external coordinator, selection=%#v", plan.Selections[0])
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	saved := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"content":"prefer Chinese"}`)
	if !saved.Succeeded || saved.Result != manager.result || saved.Unknown {
		t.Fatalf("memory save result=%#v", saved)
	}
	if manager.content != "prefer Chinese" || manager.query != "" || manager.id != "" || manager.principal.TenantID != principal.TenantID || manager.principal.UserID != principal.UserID {
		t.Fatalf("manager=%#v", manager)
	}
	recalled := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"query":"Chinese"}`)
	if !recalled.Succeeded || manager.query != "Chinese" || manager.content != "" || manager.id != "" {
		t.Fatalf("memory recall result=%#v manager=%#v", recalled, manager)
	}
	listed := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{}`)
	if !listed.Succeeded || manager.content != "" || manager.query != "" || manager.id != "" {
		t.Fatalf("memory list result=%#v manager=%#v", listed, manager)
	}
	deleted := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"id":"mem-1"}`)
	if !deleted.Succeeded || manager.id != "mem-1" || manager.content != "" || manager.query != "" {
		t.Fatalf("memory delete result=%#v manager=%#v", deleted, manager)
	}
	both := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"content":"note","query":"note"}`)
	if both.Succeeded {
		t.Fatalf("content and query together must fail closed, result=%#v", both)
	}
	rejected := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"content":"note","channel":"lansenger"}`)
	if rejected.Succeeded || rejected.Unknown {
		t.Fatalf("channel args must fail closed, result=%#v", rejected)
	}
	actionSoup := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"action":"derived_surgery"}`)
	if actionSoup.Succeeded || actionSoup.Unknown {
		t.Fatalf("action soup must fail closed, result=%#v", actionSoup)
	}

	readPlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-read", TurnID: "turn-read", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "read", Capability: CapabilityKnowledgeRead, Required: true}},
	})
	if err != nil || len(readPlan.Selections) != 0 {
		t.Fatalf("knowledge_read must not be satisfied by host memory, plan=%#v err=%v", readPlan, err)
	}
	ingestPlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-ingest", TurnID: "turn-ingest", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "ingest", Capability: CapabilityKnowledgeWrite, Required: true}},
	})
	if err != nil || len(ingestPlan.Selections) != 0 {
		t.Fatalf("knowledge_write must not be satisfied by host memory, plan=%#v err=%v", ingestPlan, err)
	}
}

func TestReviewedHostMemoryRecallIsReadOnlyQuery(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	manager := &fakeHostMemoryManager{result: "remembered: prefer Chinese"}
	observed := dynamicCatalogLifecycleForKind("mcp", IncompleteDynamicCatalogLifecycle(coretool.CatalogCoverageReasonNotReady))
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, observed, reviewedHostOwnedServices{Memory: manager})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "recall", Capability: CapabilityMemoryRecall, Required: true}},
	})
	if err != nil || len(plan.Selections) != 1 || len(plan.Unmet) != 0 {
		t.Fatalf("recall plan=%#v err=%v", plan, err)
	}
	if plan.Selections[0].AdapterName != reviewedHostMemoryRecallAdapterName || plan.Selections[0].FitProof.MatchedCapability != CapabilityMemoryRecall {
		t.Fatalf("selection=%#v", plan.Selections[0])
	}
	if len(plan.Selections[0].Effects) != 1 || plan.Selections[0].Effects[0] != coretool.EffectReadOnly {
		t.Fatalf("recall must be read-only: %#v", plan.Selections[0].Effects)
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	recalled := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"query":"Chinese"}`)
	if !recalled.Succeeded || manager.query != "Chinese" || manager.content != "" || manager.id != "" {
		t.Fatalf("recall result=%#v manager=%#v", recalled, manager)
	}
	write := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"content":"prefer Chinese"}`)
	if write.Succeeded {
		t.Fatalf("recall adapter must reject save: %#v", write)
	}
}

func TestReviewedHostMemoryIsAbsentWithoutManager(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{Knowledge: &fakeHostKnowledgeSearcher{}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "memory", Capability: CapabilityMemoryManage, Required: true}},
	})
	if err != nil || len(plan.Selections) != 0 {
		t.Fatalf("memory without manager must stay unmet, plan=%#v err=%v", plan, err)
	}
}

func TestProjectReviewedHostMemoryRejectsActionAndChannelFields(t *testing.T) {
	provider, definition, _, err := ProjectReviewedHostMemoryProvider(&fakeHostMemoryManager{})
	if err != nil {
		t.Fatal(err)
	}
	if provider.Provides[0].Capability != CapabilityMemoryManage || provider.AdapterName == "memory" {
		t.Fatalf("provider=%#v", provider)
	}
	fn, _ := definition["function"].(map[string]interface{})
	params, _ := fn["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	if _, ok := props["content"]; !ok {
		t.Fatalf("memory schema missing content: %#v", props)
	}
	if _, ok := props["query"]; !ok {
		t.Fatalf("memory schema missing query: %#v", props)
	}
	if _, ok := props["id"]; !ok || len(props) != 3 {
		t.Fatalf("memory schema=%#v", props)
	}
	for _, key := range []string{"action", "channel", "destination", "group_name", "path", "file_path", "save_path", "project_path", "owner_id", "tenant", "themes", "apply", "surgery"} {
		if _, ok := props[key]; ok {
			t.Fatalf("memory schema leaked %s", key)
		}
	}
}

func TestReviewedHostMemoryUsesTrustedPrincipalAndStore(t *testing.T) {
	store, err := memory.NewStoreWithMode(t.TempDir(), memory.StoreModeJSON)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	principal := Principal{TenantID: "tenant", UserID: "user"}
	other := Principal{TenantID: "tenant", UserID: "other"}
	cb := &coreAgentCallbacks{principal: principal, memory: store, userText: "remember that I prefer Chinese"}
	out, err := cb.ManageReviewedHostMemory(context.Background(), principal, "I prefer Chinese", "", "")
	if err != nil || !strings.Contains(out, "Memory saved") {
		t.Fatalf("save=%q err=%v", out, err)
	}
	listed, err := cb.ManageReviewedHostMemory(context.Background(), principal, "", "", "")
	if err != nil || !strings.Contains(listed, "prefer Chinese") {
		t.Fatalf("list=%q err=%v", listed, err)
	}
	recalled, err := cb.ManageReviewedHostMemory(context.Background(), principal, "", "Chinese", "")
	if err != nil || !strings.Contains(recalled, "prefer Chinese") {
		t.Fatalf("recall=%q err=%v", recalled, err)
	}
	if _, err := cb.ManageReviewedHostMemory(context.Background(), principal, "note", "note", ""); err == nil {
		t.Fatal("content and query together must fail closed")
	}
	if _, err := cb.ManageReviewedHostMemory(context.Background(), other, "note", "", ""); err == nil {
		t.Fatal("principal mismatch must fail closed")
	}
	otherCB := &coreAgentCallbacks{principal: other, memory: store}
	hidden, err := otherCB.ManageReviewedHostMemory(context.Background(), other, "", "Chinese", "")
	if err == nil && strings.Contains(hidden, "prefer Chinese") {
		t.Fatalf("other principal must not recall foreign memory: %q", hidden)
	}
	services := (&coreAgentCallbacks{principal: principal}).reviewedHostOwnedServices()
	if services.Memory != nil {
		t.Fatal("host without a memory store must not attach memory.manage.agent")
	}
}
