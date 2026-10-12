package guiapp

import (
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// The replacement must not be the thing the budget drops. The original need
// yields no selection (nothing can serve it), so the graft is the only optional
// selection and survives even at MaxSelections=1 — verified here rather than
// assumed, because "relaxed the obligation AND lost the replacement" would be a
// silent no-op turn.
func TestInstalledMCPGraftSurvivesATightBudget(t *testing.T) {
	registry := newIMSemanticCapabilityRegistry()
	catalog := tool.NewToolCatalog(registry)
	auth, _ := tool.NewParameterAuthorization(map[string]interface{}{
		"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false,
	})
	lookup := tool.ProviderSpec{
		AdapterName: "v",
		Binding: tool.ProviderBinding{Kind: "mcp", ProviderID: "s", ImplementationID: "v",
			SchemaDigest: tool.SchemaDigest([]byte("v"))},
		ParameterAuthorization: auth,
		Provides: []tool.CapabilityProvision{{
			Capability: agentservice.CapabilityInformationLookup,
			Qualifiers: map[string]string{agentservice.QualifierInformationScope: agentservice.InformationScopeReference},
			Quality:    1,
		}},
		Effects: []tool.EffectClass{tool.EffectReadOnly}, Ready: true,
	}
	snap, err := catalog.PublishWithCoverage([]tool.ProviderSpec{lookup},
		tool.CatalogCoverage{State: tool.CatalogCoverageComplete, Families: []tool.CatalogCoverageFamily{
			{Kind: "mcp", State: tool.CatalogCoverageComplete}}}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	needs := semanticNeedsWithContractOnlyGraft([]tool.CapabilityNeed{{
		ID: "need:record.read.worklog:x", Capability: tool.CapabilityRecordReadWorklog,
		Polarity: tool.NeedRequire, Required: true,
	}}, []tool.ProviderSpec{lookup}, nil, registry)

	plan, err := tool.NewToolPlanner(registry).Plan(tool.RouteRequest{
		RootTaskID: "r", SessionID: "s", TurnID: "t", ChannelScope: "desktop", Snapshot: snap, Needs: needs,
		Budget: tool.PlanningBudget{MaxSelections: 1},
	})
	if err != nil {
		t.Fatalf("plan err: %v", err)
	}
	t.Logf("budget=1 -> selections=%d unmet=%v omitted=%v", len(plan.Selections), plan.Unmet, plan.Omitted)
	if len(plan.Selections) == 0 {
		t.Fatalf("BUDGET DROPPED THE REPLACEMENT: turn proceeds with no tool for a required need")
	}
}
