package guiapp

import (
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// The graft must actually RESCUE the turn, not just append a second need.
// If the original required need stays in the plan with no provider, the
// planner records it as Unmet and the whole turn is still refused — the graft
// would be decoration.
func TestZZGraftActuallyRescuesThroughPlanner(t *testing.T) {
	registry := newIMSemanticCapabilityRegistry()
	catalog := tool.NewToolCatalog(registry)

	auth, err := tool.NewParameterAuthorization(map[string]interface{}{
		"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	lookupProvider := tool.ProviderSpec{
		AdapterName: "vendor_lookup",
		Binding: tool.ProviderBinding{
			Kind: "mcp", ProviderID: "srv", ImplementationID: "vendor_lookup",
			SchemaDigest: tool.SchemaDigest([]byte("vendor_lookup")),
		},
		ParameterAuthorization: auth,
		Provides: []tool.CapabilityProvision{{
			Capability: agentservice.CapabilityInformationLookup,
			Qualifiers: map[string]string{agentservice.QualifierInformationScope: agentservice.InformationScopeReference},
			Quality:    1,
		}},
		Effects: []tool.EffectClass{tool.EffectReadOnly},
		Ready:   true,
	}
	snapshot, err := catalog.PublishWithCoverage(
		[]tool.ProviderSpec{lookupProvider},
		tool.CatalogCoverage{State: tool.CatalogCoverageComplete, Families: []tool.CatalogCoverageFamily{
			{Kind: "mcp", State: tool.CatalogCoverageComplete},
		}},
		time.Now().UTC(),
	)
	if err != nil {
		t.Fatal(err)
	}

	needs := []tool.CapabilityNeed{{
		ID: "need:record.read.worklog:6d6f8a6a31c1", Capability: tool.CapabilityRecordReadWorklog,
		Polarity: tool.NeedRequire, Required: true,
	}}
	needs = semanticNeedsWithContractOnlyGraft(needs, []tool.ProviderSpec{lookupProvider}, nil, newIMSemanticCapabilityRegistry())

	plan, err := tool.NewToolPlanner(registry).Plan(tool.RouteRequest{
		RootTaskID: "r", SessionID: "s", TurnID: "t",
		ChannelScope: "desktop", Snapshot: snapshot, Needs: needs,
	})
	if err != nil {
		t.Fatalf("planner error: %v", err)
	}
	if len(plan.Unmet) > 0 {
		t.Fatalf("GRAFT DID NOT RESCUE: plan.Unmet=%#v selections=%d", plan.Unmet, len(plan.Selections))
	}
	if len(plan.Selections) == 0 {
		t.Fatal("graft produced no selection; the model would have no tool")
	}
	t.Logf("rescued: selections=%d", len(plan.Selections))
}

// The other half: with nothing installed that can answer, the turn must STILL
// be refused. Relaxing the obligation is only ever licensed by an installed
// replacement; it must never turn a genuine capability gap into a silent pass.
func TestZZGraftStaysClosedWithNothingInstalled(t *testing.T) {
	registry := newIMSemanticCapabilityRegistry()
	catalog := tool.NewToolCatalog(registry)
	snapshot, err := catalog.PublishWithCoverage(nil, tool.CatalogCoverage{
		State: tool.CatalogCoverageComplete,
		Families: []tool.CatalogCoverageFamily{
			{Kind: "mcp", State: tool.CatalogCoverageComplete},
			{Kind: "builtin", State: tool.CatalogCoverageComplete},
		},
	}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	needs := []tool.CapabilityNeed{{
		ID: "need:record.read.worklog:6d6f8a6a31c1", Capability: tool.CapabilityRecordReadWorklog,
		Polarity: tool.NeedRequire, Required: true,
	}}
	needs = semanticNeedsWithContractOnlyGraft(needs, nil, nil, newIMSemanticCapabilityRegistry())
	plan, err := tool.NewToolPlanner(registry).Plan(tool.RouteRequest{
		RootTaskID: "r", SessionID: "s", TurnID: "t",
		ChannelScope: "desktop", Snapshot: snapshot, Needs: needs,
	})
	if err != nil {
		t.Fatalf("planner error: %v", err)
	}
	if len(plan.Unmet) == 0 {
		t.Fatalf("nothing installed must still fail closed, got unmet=%#v selections=%d", plan.Unmet, len(plan.Selections))
	}
}
