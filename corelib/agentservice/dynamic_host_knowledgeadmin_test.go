package agentservice

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

type fakeHostKnowledgeAdministrator struct {
	id         string
	status     string
	refresh    bool
	hasRefresh bool
	principal  Principal
	result     string
	err        error
}

func (f *fakeHostKnowledgeAdministrator) AdministerReviewedHostKnowledge(_ context.Context, principal Principal, id, status string, refresh bool, hasRefresh bool) (string, error) {
	f.principal = principal
	f.id = id
	f.status = status
	f.refresh = refresh
	f.hasRefresh = hasRefresh
	return f.result, f.err
}

func TestReviewedHostKnowledgeAdminExecutesWithoutCoordinatorAndRejectsReadMapping(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	admin := &fakeHostKnowledgeAdministrator{result: "知识来源已更新: notes"}
	observed := dynamicCatalogLifecycleForKind("mcp", IncompleteDynamicCatalogLifecycle(coretool.CatalogCoverageReasonNotReady))
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, observed, reviewedHostOwnedServices{KnowledgeAdmin: admin})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "admin", Capability: CapabilityKnowledgeAdmin, Required: true}},
	})
	if err != nil || len(plan.Selections) != 1 || len(plan.Unmet) != 0 {
		t.Fatalf("knowledge admin plan=%#v err=%v", plan, err)
	}
	if plan.Selections[0].Provider.Kind != reviewedHostProviderKind || plan.Selections[0].FitProof.MatchedCapability != CapabilityKnowledgeAdmin {
		t.Fatalf("selection=%#v", plan.Selections[0])
	}
	if !dynamicHostLocalMutationSelection(plan.Selections[0]) || dynamicSelectionRequiresReceipt(plan.Selections[0]) {
		t.Fatalf("host knowledge admin must use the local mutation receipt, selection=%#v", plan.Selections[0])
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	listed := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{}`)
	if !listed.Succeeded || admin.id != "" || admin.hasRefresh {
		t.Fatalf("list result=%#v admin=%#v", listed, admin)
	}
	disabled := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"id":"src-1","status":"disabled"}`)
	if !disabled.Succeeded || admin.id != "src-1" || admin.status != "disabled" {
		t.Fatalf("disable result=%#v admin=%#v", disabled, admin)
	}
	refreshed := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"id":"src-1","refresh":true}`)
	if !refreshed.Succeeded || !admin.refresh || !admin.hasRefresh {
		t.Fatalf("refresh result=%#v admin=%#v", refreshed, admin)
	}
	refreshFalse := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"id":"src-1","refresh":false}`)
	if refreshFalse.Succeeded {
		t.Fatalf("refresh=false must fail closed, result=%#v", refreshFalse)
	}
	both := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"id":"src-1","status":"disabled","refresh":true}`)
	if both.Succeeded {
		t.Fatalf("status and refresh together must fail closed, result=%#v", both)
	}
	actionSoup := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"action":"maintain"}`)
	if actionSoup.Succeeded || actionSoup.Unknown {
		t.Fatalf("action soup must fail closed, result=%#v", actionSoup)
	}

	readPlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-read", TurnID: "turn-read", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "read", Capability: CapabilityKnowledgeRead, Required: true}},
	})
	if err != nil || len(readPlan.Selections) != 0 {
		t.Fatalf("knowledge.read.local must not be satisfied by host admin, plan=%#v err=%v", readPlan, err)
	}
	writePlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-write", TurnID: "turn-write", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "ingest", Capability: CapabilityKnowledgeWrite, Required: true}},
	})
	if err != nil || len(writePlan.Selections) != 0 {
		t.Fatalf("knowledge.ingest.local must not be satisfied by host admin, plan=%#v err=%v", writePlan, err)
	}
}

func TestReviewedHostKnowledgeAdminIsAbsentWithoutStore(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{KnowledgeWrite: &fakeHostKnowledgeIngester{}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "admin", Capability: CapabilityKnowledgeAdmin, Required: true}},
	})
	if err != nil || len(plan.Selections) != 0 {
		t.Fatalf("knowledge admin without store must stay unmet, plan=%#v err=%v", plan, err)
	}
}

func TestProjectReviewedHostKnowledgeAdminRejectsIngestAndQualityFields(t *testing.T) {
	provider, definition, _, err := ProjectReviewedHostKnowledgeAdminProvider(&fakeHostKnowledgeAdministrator{})
	if err != nil {
		t.Fatal(err)
	}
	if provider.Provides[0].Capability != CapabilityKnowledgeAdmin || provider.AdapterName == "knowledge_maintain" || provider.AdapterName == "knowledge_disable_source" {
		t.Fatalf("provider=%#v", provider)
	}
	fn, _ := definition["function"].(map[string]interface{})
	params, _ := fn["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	if _, ok := props["id"]; !ok || len(props) != 3 {
		t.Fatalf("knowledge admin schema=%#v", props)
	}
	for _, key := range []string{"action", "source_id", "query", "text", "url", "path", "labels", "snapshot", "channel", "destination"} {
		if _, ok := props[key]; ok {
			t.Fatalf("knowledge admin schema leaked %s", key)
		}
	}
}

func TestReviewedHostKnowledgeAdminUsesTrustedPrincipalAndStore(t *testing.T) {
	store := &mgmtFakeKnowledgeStore{sources: map[string]knowledge.Source{
		"s1": {ID: "s1", Title: "notes", Status: "active", TenantID: "tenant", OwnerID: "user"},
		"s2": {ID: "s2", Title: "foreign", Status: "active", TenantID: "tenant", OwnerID: "other"},
	}}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	other := Principal{TenantID: "tenant", UserID: "other"}
	cb := &coreAgentCallbacks{principal: principal, knowledgeStore: store}
	listed, err := cb.AdministerReviewedHostKnowledge(context.Background(), principal, "", "", false, false)
	if err != nil || !strings.Contains(listed, "s1") || strings.Contains(listed, "foreign") {
		t.Fatalf("list=%q err=%v", listed, err)
	}
	if store.lastListOpts.TenantID != principal.TenantID || store.lastListOpts.OwnerID != principal.UserID {
		t.Fatalf("list opts=%#v", store.lastListOpts)
	}
	got, err := cb.AdministerReviewedHostKnowledge(context.Background(), principal, "s1", "", false, false)
	if err != nil || !strings.Contains(got, "s1") {
		t.Fatalf("get=%q err=%v", got, err)
	}
	if _, err := cb.AdministerReviewedHostKnowledge(context.Background(), principal, "s2", "disabled", false, false); err == nil {
		t.Fatal("foreign source must fail closed")
	}
	if _, err := cb.AdministerReviewedHostKnowledge(context.Background(), other, "s1", "disabled", false, false); err == nil {
		t.Fatal("principal mismatch must fail closed")
	}
	if _, err := cb.AdministerReviewedHostKnowledge(context.Background(), principal, "s1", "disabled", false, false); err != nil {
		t.Fatalf("disable=%v", err)
	}
	if len(store.disabledIDs) != 1 || store.disabledIDs[0] != "s1" {
		t.Fatalf("disabled=%v", store.disabledIDs)
	}
	if _, err := cb.AdministerReviewedHostKnowledge(context.Background(), principal, "s1", "", true, true); err != nil {
		t.Fatalf("refresh=%v", err)
	}
	if len(store.refreshedIDs) != 1 || store.refreshedIDs[0] != "s1" {
		t.Fatalf("refreshed=%v", store.refreshedIDs)
	}
	services := (&coreAgentCallbacks{principal: principal}).reviewedHostOwnedServices()
	if services.KnowledgeAdmin != nil {
		t.Fatal("host without a knowledge store must not attach knowledge.admin.maintenance")
	}
}
