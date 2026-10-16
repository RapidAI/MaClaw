package agentservice

import (
	"context"
	"testing"
	"time"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

type fakeHostWebSearcher struct {
	query     string
	principal Principal
	result    string
	err       error
}

func (f *fakeHostWebSearcher) SearchReviewedHostWeb(_ context.Context, principal Principal, query string) (string, error) {
	f.principal = principal
	f.query = query
	return f.result, f.err
}

func TestReviewedHostWebSearchExecutesQueryAndRejectsLookupMapping(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	searcher := &fakeHostWebSearcher{result: "Search \"golang\" — 1 results:\n\n1. Go\n   https://go.dev"}
	observed := dynamicCatalogLifecycleForKind("mcp", IncompleteDynamicCatalogLifecycle(coretool.CatalogCoverageReasonNotReady))
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, observed, reviewedHostOwnedServices{WebSearch: searcher})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{
			ID: "search", Capability: CapabilityInformationSearchWeb, Required: true,
			Qualifiers: map[string]string{QualifierSearchFreshness: SearchFreshnessReference},
		}},
	})
	if err != nil || len(plan.Selections) != 1 || len(plan.Unmet) != 0 {
		t.Fatalf("web search plan=%#v err=%v", plan, err)
	}
	if plan.Selections[0].Provider.Kind != reviewedHostProviderKind || plan.Selections[0].FitProof.MatchedCapability != CapabilityInformationSearchWeb {
		t.Fatalf("selection=%#v", plan.Selections[0])
	}
	currentPlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-current", TurnID: "turn-current", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{
			ID: "live", Capability: CapabilityInformationSearchWeb, Required: true,
			Qualifiers: map[string]string{QualifierSearchFreshness: SearchFreshnessCurrent},
		}},
	})
	if err != nil || len(currentPlan.Selections) != 1 {
		t.Fatalf("current search plan=%#v err=%v", currentPlan, err)
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	result := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"query":"golang"}`)
	if !result.Succeeded || result.Result != searcher.result {
		t.Fatalf("web search result=%#v", result)
	}
	if searcher.query != "golang" || searcher.principal.TenantID != principal.TenantID || searcher.principal.UserID != principal.UserID {
		t.Fatalf("searcher=%#v", searcher)
	}
	rejected := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"query":"golang","max_results":20}`)
	if rejected.Succeeded || rejected.Unknown {
		t.Fatalf("max_results must fail closed, result=%#v", rejected)
	}
	urlRejected := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"query":"golang","url":"https://example.com"}`)
	if urlRejected.Succeeded || urlRejected.Unknown {
		t.Fatalf("url args must fail closed, result=%#v", urlRejected)
	}

	lookupPlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-lookup", TurnID: "turn-lookup", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{
			ID: "lookup", Capability: CapabilityInformationLookup, Required: true,
			Qualifiers: map[string]string{QualifierInformationScope: InformationScopeReference},
		}},
	})
	if err != nil || len(lookupPlan.Selections) != 0 {
		t.Fatalf("lookup must not be satisfied by host web search, plan=%#v err=%v", lookupPlan, err)
	}
	fetchPlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-fetch", TurnID: "turn-fetch", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "fetch", Capability: CapabilityWebFetch, Required: true}},
	})
	if err != nil || len(fetchPlan.Selections) != 0 {
		t.Fatalf("web fetch must not be satisfied by host web search, plan=%#v err=%v", fetchPlan, err)
	}
}

func TestReviewedHostWebSearchIsAbsentWithoutSearcher(t *testing.T) {
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
		Needs: []coretool.CapabilityNeed{{
			ID: "search", Capability: CapabilityInformationSearchWeb, Required: true,
			Qualifiers: map[string]string{QualifierSearchFreshness: SearchFreshnessReference},
		}},
	})
	if err != nil || len(plan.Selections) != 0 {
		t.Fatalf("web search without searcher must stay unmet, plan=%#v err=%v", plan, err)
	}
	clockPlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-clock", TurnID: "turn-clock", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "clock", Capability: CapabilityCurrentTime, Required: true}},
	})
	if err != nil || len(clockPlan.Selections) != 1 {
		t.Fatalf("clock must still plan without a web searcher, plan=%#v err=%v", clockPlan, err)
	}
}

func TestProjectReviewedHostWebSearchRejectsEngineAndURLFields(t *testing.T) {
	provider, definition, _, err := ProjectReviewedHostWebSearchProvider(&fakeHostWebSearcher{})
	if err != nil {
		t.Fatal(err)
	}
	if provider.Provides[0].Capability != CapabilityInformationSearchWeb {
		t.Fatalf("provider=%#v", provider)
	}
	fn, _ := definition["function"].(map[string]interface{})
	params, _ := fn["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	if _, ok := props["query"]; !ok || len(props) != 1 {
		t.Fatalf("web search schema=%#v", props)
	}
	for _, key := range []string{"max_results", "provider", "engine", "url", "channel", "destination", "save_path"} {
		if _, ok := props[key]; ok {
			t.Fatalf("web search schema leaked %s", key)
		}
	}
}

func TestReviewedWebSearchMCPOutranksHostAndMapsQuery(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"search_query":          map[string]interface{}{"type": "string"},
			"content_size":          map[string]interface{}{"type": "string"},
			"location":              map[string]interface{}{"type": "string"},
			"search_domain_filter":  map[string]interface{}{"type": "string"},
			"search_recency_filter": map[string]interface{}{"type": "string"},
		},
		"required": []string{"search_query"},
	}
	entry := MCPToolEntry{
		ServerID: "web-search-prime", ToolName: ReviewedMCPWebSearchPrimeTool,
		CapabilityGlobalKey: ReviewedMCPWebSearchPrimeGlobalKey, InstalledCapabilityID: "cap_instance",
		InputSchema: schema, Contract: reviewedWebSearchPrimeContract(),
	}
	provider := &boundMCPProviderStub{entries: []MCPToolEntry{entry}}
	searcher := &fakeHostWebSearcher{result: "host"}
	observed := dynamicCatalogLifecycleForKind("mcp", CompleteDynamicCatalogLifecycle())
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, provider.entries, nil, observed, reviewedHostOwnedServices{WebSearch: searcher})
	if err != nil {
		t.Fatal(err)
	}
	// The desktop assistant publishes semantic_search_trusted_web at quality 2
	// with kind builtin. That StableID sorts before mcp, so an equal MCP rank
	// never replaces it.
	desktopAuth, err := coretool.NewParameterAuthorization(reviewedHostWebSearchInvocationSchema())
	if err != nil {
		t.Fatal(err)
	}
	catalog.Providers = append(catalog.Providers, coretool.ProviderSpec{
		AdapterName: "semantic_search_trusted_web",
		Binding: coretool.ProviderBinding{
			Kind: "builtin", ProviderID: "im", ImplementationID: "trusted-web-search-v1",
			SchemaDigest: "desktop-managed-search",
		},
		ParameterAuthorization: desktopAuth,
		Provides: []coretool.CapabilityProvision{
			{Capability: CapabilityInformationSearchWeb, Qualifiers: map[string]string{QualifierSearchFreshness: SearchFreshnessReference}, Quality: 2},
			{Capability: CapabilityInformationSearchWeb, Qualifiers: map[string]string{QualifierSearchFreshness: SearchFreshnessCurrent}, Quality: 2},
		},
		Effects: []coretool.EffectClass{coretool.EffectReadOnly},
		Ready:   true,
	})
	lifecycle.Coverage.Families = append(lifecycle.Coverage.Families, coretool.CatalogCoverageFamily{
		Kind: "builtin", State: coretool.CatalogCoverageComplete,
	})
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{
			ID: "search", Capability: CapabilityInformationSearchWeb, Required: true,
			Qualifiers: map[string]string{QualifierSearchFreshness: SearchFreshnessReference},
		}},
	})
	if err != nil || len(plan.Selections) != 1 || len(plan.Unmet) != 0 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	selection := plan.Selections[0]
	if selection.Provider.Kind != "mcp" || selection.Provider.ImplementationID != ReviewedMCPWebSearchPrimeTool {
		t.Fatalf("host search was selected: %#v", selection.Provider)
	}
	if coretool.RenderedSemanticFunctionName(selection.AdapterName, "invoke_token") != "web_search" {
		t.Fatalf("adapter %q did not render as web_search", selection.AdapterName)
	}
	definition := catalog.Definitions[selection.AdapterName]
	params := definition["function"].(map[string]interface{})["parameters"].(map[string]interface{})
	props := params["properties"].(map[string]interface{})
	if _, ok := props["query"]; !ok || len(props) != 1 {
		t.Fatalf("model schema=%#v", props)
	}
	if _, ok := props["search_query"]; ok {
		t.Fatal("vendor argument was rendered")
	}
	result := catalog.ExecuteSelection(context.Background(), Principal{TenantID: "desktop", UserID: "desktop-user"}, provider, nil, selection, `{"query":"张学友"}`)
	if !result.Succeeded || provider.boundCalls != 1 || provider.arguments["search_query"] != "张学友" || provider.arguments["query"] != nil {
		t.Fatalf("result=%#v args=%#v calls=%d", result, provider.arguments, provider.boundCalls)
	}
	if searcher.query != "" {
		t.Fatalf("host search ran: %q", searcher.query)
	}
	rejected := catalog.ExecuteSelection(context.Background(), Principal{TenantID: "desktop", UserID: "desktop-user"}, provider, nil, selection, `{"search_query":"张学友"}`)
	if rejected.Succeeded || provider.boundCalls != 1 {
		t.Fatalf("vendor argument was accepted: %#v calls=%d", rejected, provider.boundCalls)
	}

	unkeyed := entry
	unkeyed.CapabilityGlobalKey = ""
	unkeyed.InstalledCapabilityID = "cap_43f40e502b689144"
	unkeyed.InputSchema = map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"search_query": map[string]interface{}{"type": "string"},
		},
		"required": []string{"search_query"},
	}
	unkeyedCatalog, err := BuildDynamicSemanticCatalog([]MCPToolEntry{unkeyed}, nil)
	if err != nil || len(unkeyedCatalog.Providers) != 1 {
		t.Fatalf("unkeyed catalog providers=%d err=%v", len(unkeyedCatalog.Providers), err)
	}
	if coretool.SemanticModelFunctionName(unkeyedCatalog.Providers[0].AdapterName) != "" {
		t.Fatalf("instance id adopted the prompt name: %s", unkeyedCatalog.Providers[0].AdapterName)
	}
	unkeyedParams := unkeyedCatalog.Definitions[unkeyedCatalog.Providers[0].AdapterName]["function"].(map[string]interface{})["parameters"].(map[string]interface{})
	if _, ok := unkeyedParams["properties"].(map[string]interface{})["search_query"]; !ok {
		t.Fatalf("unkeyed schema=%#v", unkeyedParams)
	}

	broken := entry
	broken.InputSchema = map[string]interface{}{"type": "object", "properties": map[string]interface{}{"q": map[string]interface{}{"type": "string"}}}
	quarantined, err := BuildDynamicSemanticCatalog([]MCPToolEntry{broken}, nil)
	if err != nil || len(quarantined.Providers) != 0 {
		t.Fatalf("schema drift stayed routable: providers=%d err=%v", len(quarantined.Providers), err)
	}
}

func TestReviewedHostOwnedServicesPopulateWebSearch(t *testing.T) {
	cb := &coreAgentCallbacks{principal: Principal{TenantID: "t", UserID: "u"}}
	services := cb.reviewedHostOwnedServices()
	if services.WebSearch == nil || services.WebFetch == nil {
		t.Fatalf("web search/fetch must be wired on the host callbacks: %#v", services)
	}
}
