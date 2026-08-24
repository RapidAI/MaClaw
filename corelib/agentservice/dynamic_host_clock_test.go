package agentservice

import (
	"context"
	"strings"
	"testing"
	"time"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestProjectReviewedHostClockRejectsChannelFields(t *testing.T) {
	provider, definition, host, err := ProjectReviewedHostClockProvider()
	if err != nil {
		t.Fatal(err)
	}
	if provider.Binding.Kind != reviewedHostProviderKind || provider.Provides[0].Capability != CapabilityCurrentTime {
		t.Fatalf("provider=%#v", provider)
	}
	if host.execute == nil {
		t.Fatal("host clock executor missing")
	}
	fn, _ := definition["function"].(map[string]interface{})
	params, _ := fn["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	if len(props) != 0 {
		t.Fatalf("clock schema must be empty, got %#v", props)
	}
	for _, key := range []string{"channel", "destination", "group_name", "group_id"} {
		if _, ok := props[key]; ok {
			t.Fatalf("clock schema leaked %s", key)
		}
	}
}

func TestReviewedHostClockExecutesLocalTimeAndRejectsLookupMapping(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	observed := dynamicCatalogLifecycleForKind("mcp", IncompleteDynamicCatalogLifecycle(coretool.CatalogCoverageReasonNotReady))
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, observed, reviewedHostOwnedServices{})
	if err != nil {
		t.Fatal(err)
	}
	if lifecycle.Coverage.ForProviderKind(reviewedHostProviderKind).State != coretool.CatalogCoverageComplete {
		t.Fatalf("host coverage=%#v", lifecycle.Coverage)
	}
	if lifecycle.Coverage.ForProviderKind("mcp").State != coretool.CatalogCoverageIncomplete {
		t.Fatalf("mcp coverage should stay incomplete: %#v", lifecycle.Coverage)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	clockPlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "clock", Capability: CapabilityCurrentTime, Required: true}},
	})
	if err != nil || len(clockPlan.Selections) != 1 || len(clockPlan.Unmet) != 0 {
		t.Fatalf("clock plan=%#v err=%v", clockPlan, err)
	}
	if clockPlan.Selections[0].Provider.Kind != reviewedHostProviderKind {
		t.Fatalf("clock selection=%#v", clockPlan.Selections[0])
	}
	if clockPlan.Selections[0].FitProof.MatchedCapability != CapabilityCurrentTime {
		t.Fatalf("matched=%q", clockPlan.Selections[0].FitProof.MatchedCapability)
	}
	result := catalog.ExecuteSelection(context.Background(), Principal{}, nil, nil, clockPlan.Selections[0], `{}`)
	if !result.Succeeded || !strings.Contains(result.Result, "ISO week") || !strings.Contains(result.Result, "timezone:") {
		t.Fatalf("clock result=%#v", result)
	}
	rejected := catalog.ExecuteSelection(context.Background(), Principal{}, nil, nil, clockPlan.Selections[0], `{"channel":"lansenger","destination":"group:1"}`)
	if rejected.Succeeded || rejected.Unknown {
		t.Fatalf("channel args must fail closed, result=%#v", rejected)
	}

	lookupPlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-lookup", TurnID: "turn-lookup", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{
			ID: "lookup", Capability: CapabilityInformationLookup, Required: true,
			Qualifiers: map[string]string{QualifierInformationScope: InformationScopeCurrent},
		}},
	})
	if err != nil || len(lookupPlan.Selections) != 0 {
		t.Fatalf("lookup must not be satisfied by the host clock, plan=%#v err=%v", lookupPlan, err)
	}
}

func TestBuildDynamicSemanticCatalogDoesNotImportHostClock(t *testing.T) {
	catalog, err := BuildDynamicSemanticCatalog(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Providers) != 0 {
		t.Fatalf("MCP/Skill catalog must stay empty without discovery, got %#v", catalog.Providers)
	}
}
