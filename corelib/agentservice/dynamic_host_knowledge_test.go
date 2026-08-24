package agentservice

import (
	"context"
	"testing"
	"time"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

type fakeHostKnowledgeSearcher struct {
	query     string
	principal Principal
	result    string
	err       error
}

func (f *fakeHostKnowledgeSearcher) SearchReviewedHostKnowledge(_ context.Context, principal Principal, query string) (string, error) {
	f.principal = principal
	f.query = query
	return f.result, f.err
}

func TestReviewedHostKnowledgeExecutesQueryAndRejectsLookupMapping(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	searcher := &fakeHostKnowledgeSearcher{result: "note: local-only hit"}
	observed := dynamicCatalogLifecycleForKind("mcp", IncompleteDynamicCatalogLifecycle(coretool.CatalogCoverageReasonNotReady))
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, observed, reviewedHostOwnedServices{Knowledge: searcher})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "kb", Capability: CapabilityKnowledgeRead, Required: true}},
	})
	if err != nil || len(plan.Selections) != 1 || len(plan.Unmet) != 0 {
		t.Fatalf("knowledge plan=%#v err=%v", plan, err)
	}
	if plan.Selections[0].Provider.Kind != reviewedHostProviderKind || plan.Selections[0].FitProof.MatchedCapability != CapabilityKnowledgeRead {
		t.Fatalf("selection=%#v", plan.Selections[0])
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	result := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"query":"project notes"}`)
	if !result.Succeeded || result.Result != "note: local-only hit" {
		t.Fatalf("knowledge result=%#v", result)
	}
	if searcher.query != "project notes" || searcher.principal.TenantID != principal.TenantID || searcher.principal.UserID != principal.UserID {
		t.Fatalf("searcher=%#v", searcher)
	}
	rejected := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"query":"x","channel":"lansenger"}`)
	if rejected.Succeeded || rejected.Unknown {
		t.Fatalf("channel args must fail closed, result=%#v", rejected)
	}

	lookupPlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-lookup", TurnID: "turn-lookup", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{
			ID: "lookup", Capability: CapabilityInformationLookup, Required: true,
			Qualifiers: map[string]string{QualifierInformationScope: InformationScopeReference},
		}},
	})
	if err != nil || len(lookupPlan.Selections) != 0 {
		t.Fatalf("lookup must not be satisfied by host knowledge, plan=%#v err=%v", lookupPlan, err)
	}
}

func TestReviewedHostKnowledgeIsAbsentWithoutSearcher(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "kb", Capability: CapabilityKnowledgeRead, Required: true}},
	})
	if err != nil || len(plan.Selections) != 0 {
		t.Fatalf("knowledge without searcher must stay unmet, plan=%#v err=%v", plan, err)
	}
	clockPlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-clock", TurnID: "turn-clock", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "clock", Capability: CapabilityCurrentTime, Required: true}},
	})
	if err != nil || len(clockPlan.Selections) != 1 {
		t.Fatalf("clock must still plan without a knowledge searcher, plan=%#v err=%v", clockPlan, err)
	}
}

func TestProjectReviewedHostKnowledgeRejectsChannelFields(t *testing.T) {
	provider, definition, _, err := ProjectReviewedHostKnowledgeProvider(&fakeHostKnowledgeSearcher{})
	if err != nil {
		t.Fatal(err)
	}
	if provider.Provides[0].Capability != CapabilityKnowledgeRead {
		t.Fatalf("provider=%#v", provider)
	}
	fn, _ := definition["function"].(map[string]interface{})
	params, _ := fn["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	if _, ok := props["query"]; !ok || len(props) != 1 {
		t.Fatalf("knowledge schema=%#v", props)
	}
	for _, key := range []string{"channel", "destination", "group_name", "path", "source_ids"} {
		if _, ok := props[key]; ok {
			t.Fatalf("knowledge schema leaked %s", key)
		}
	}
}
