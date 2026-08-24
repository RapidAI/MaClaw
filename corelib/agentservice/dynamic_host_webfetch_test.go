package agentservice

import (
	"context"
	"testing"
	"time"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

type fakeHostWebFetcher struct {
	rawURL    string
	principal Principal
	result    string
	err       error
}

func (f *fakeHostWebFetcher) FetchReviewedHostWeb(_ context.Context, principal Principal, rawURL string) (string, error) {
	f.principal = principal
	f.rawURL = rawURL
	return f.result, f.err
}

func TestReviewedHostWebFetchExecutesURLAndRejectsLookupMapping(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	fetcher := &fakeHostWebFetcher{result: "Title: Example\nURL: https://example.com"}
	observed := dynamicCatalogLifecycleForKind("mcp", IncompleteDynamicCatalogLifecycle(coretool.CatalogCoverageReasonNotReady))
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, observed, reviewedHostOwnedServices{WebFetch: fetcher})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "fetch", Capability: CapabilityWebFetch, Required: true}},
	})
	if err != nil || len(plan.Selections) != 1 || len(plan.Unmet) != 0 {
		t.Fatalf("web fetch plan=%#v err=%v", plan, err)
	}
	if plan.Selections[0].Provider.Kind != reviewedHostProviderKind || plan.Selections[0].FitProof.MatchedCapability != CapabilityWebFetch {
		t.Fatalf("selection=%#v", plan.Selections[0])
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	result := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"url":"https://example.com/page"}`)
	if !result.Succeeded || result.Result != fetcher.result {
		t.Fatalf("web fetch result=%#v", result)
	}
	if fetcher.rawURL != "https://example.com/page" || fetcher.principal.TenantID != principal.TenantID || fetcher.principal.UserID != principal.UserID {
		t.Fatalf("fetcher=%#v", fetcher)
	}
	rejected := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"url":"https://example.com","save_path":"out.html"}`)
	if rejected.Succeeded || rejected.Unknown {
		t.Fatalf("save_path must fail closed, result=%#v", rejected)
	}
	channelRejected := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"url":"https://example.com","channel":"lansenger"}`)
	if channelRejected.Succeeded || channelRejected.Unknown {
		t.Fatalf("channel args must fail closed, result=%#v", channelRejected)
	}

	lookupPlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-lookup", TurnID: "turn-lookup", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{
			ID: "lookup", Capability: CapabilityInformationLookup, Required: true,
			Qualifiers: map[string]string{QualifierInformationScope: InformationScopeReference},
		}},
	})
	if err != nil || len(lookupPlan.Selections) != 0 {
		t.Fatalf("lookup must not be satisfied by host web fetch, plan=%#v err=%v", lookupPlan, err)
	}
}

func TestReviewedHostWebFetchIsAbsentWithoutFetcher(t *testing.T) {
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
		Needs: []coretool.CapabilityNeed{{ID: "fetch", Capability: CapabilityWebFetch, Required: true}},
	})
	if err != nil || len(plan.Selections) != 0 {
		t.Fatalf("web fetch without fetcher must stay unmet, plan=%#v err=%v", plan, err)
	}
	clockPlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-clock", TurnID: "turn-clock", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "clock", Capability: CapabilityCurrentTime, Required: true}},
	})
	if err != nil || len(clockPlan.Selections) != 1 {
		t.Fatalf("clock must still plan without a web fetcher, plan=%#v err=%v", clockPlan, err)
	}
}

func TestProjectReviewedHostWebFetchRejectsChannelAndSaveFields(t *testing.T) {
	provider, definition, _, err := ProjectReviewedHostWebFetchProvider(&fakeHostWebFetcher{})
	if err != nil {
		t.Fatal(err)
	}
	if provider.Provides[0].Capability != CapabilityWebFetch {
		t.Fatalf("provider=%#v", provider)
	}
	fn, _ := definition["function"].(map[string]interface{})
	params, _ := fn["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	if _, ok := props["url"]; !ok || len(props) != 1 {
		t.Fatalf("web fetch schema=%#v", props)
	}
	for _, key := range []string{"channel", "destination", "group_name", "path", "save_path", "dest", "filename", "query"} {
		if _, ok := props[key]; ok {
			t.Fatalf("web fetch schema leaked %s", key)
		}
	}
}
