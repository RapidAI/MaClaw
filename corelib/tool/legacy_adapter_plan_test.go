package tool

import (
	"errors"
	"testing"
	"time"
)

func legacyAdapterPlanTestDefinition(name string) map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        name,
			"description": name + " test definition",
			"parameters": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"path": map[string]interface{}{"type": "string"}},
			},
		},
	}
}

func legacyAdapterPlanTestRecommendation() RoutingRecommendation {
	return RoutingRecommendation{
		SearchQuery: "read a local file",
		Confidence:  0.8,
		Evidence: []RoutingEvidence{
			{ToolName: "read_file", Capability: "workspace.file.read", AdapterContract: "legacy-workspace-read-v1", Reason: "retrieval_candidate", Score: 0.8},
			{ToolName: "async_wait", Capability: "task.wait", AdapterContract: "legacy-task-wait-v1", Reason: "bootstrap", Score: 1},
		},
	}
}

func TestBuildAndRenderLegacyAdapterPlanUsesOnlyReviewedSelections(t *testing.T) {
	now := time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC)
	definitions := []map[string]interface{}{
		legacyAdapterPlanTestDefinition("read_file"),
		legacyAdapterPlanTestDefinition("async_wait"),
		legacyAdapterPlanTestDefinition("unselected_tool"),
	}
	plan, err := BuildLegacyAdapterPlan(LegacyAdapterPlanInput{
		Recommendation: legacyAdapterPlanTestRecommendation(), Definitions: definitions, PolicyDigest: "policy-v1", Now: now,
	})
	if err != nil {
		t.Fatalf("BuildLegacyAdapterPlan() error = %v", err)
	}
	if plan.ID() == "" || plan.CatalogDigest() == "" || len(plan.Selections()) != 2 {
		t.Fatalf("incomplete legacy adapter plan: %#v", plan)
	}
	rendered, err := RenderLegacyAdapterPlan(plan, definitions, now)
	if err != nil {
		t.Fatalf("RenderLegacyAdapterPlan() error = %v", err)
	}
	if len(rendered) != 2 || ExtractToolName(rendered[0]) != "async_wait" || ExtractToolName(rendered[1]) != "read_file" {
		t.Fatalf("renderer did not replace surface with reviewed selection: %#v", rendered)
	}
	// Renderer output must be a new deep copy, so downstream normalization cannot
	// mutate the trusted snapshot used by another request.
	rendered[0]["function"].(map[string]interface{})["description"] = "mutated"
	if definitions[1]["function"].(map[string]interface{})["description"] == "mutated" {
		t.Fatal("renderer leaked mutable definition snapshot")
	}
}

func TestLegacyAdapterPlanFailsClosedForUnreviewedEvidence(t *testing.T) {
	now := time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC)
	_, err := BuildLegacyAdapterPlan(LegacyAdapterPlanInput{
		Recommendation: RoutingRecommendation{Evidence: []RoutingEvidence{{ToolName: "ssh", Capability: "remote.shell", AdapterContract: "not-reviewed"}}},
		Definitions:    []map[string]interface{}{legacyAdapterPlanTestDefinition("ssh")},
		PolicyDigest:   "policy-v1",
		Now:            now,
	})
	var planErr *LegacyAdapterPlanError
	if !errors.As(err, &planErr) || planErr.Code != LegacyAdapterPlanCatalogIncomplete {
		t.Fatalf("expected catalog_incomplete, got %v", err)
	}
}

func TestLegacyAdapterPlanRejectsDefinitionSnapshotDrift(t *testing.T) {
	now := time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC)
	definitions := []map[string]interface{}{
		legacyAdapterPlanTestDefinition("read_file"), legacyAdapterPlanTestDefinition("async_wait"),
	}
	plan, err := BuildLegacyAdapterPlan(LegacyAdapterPlanInput{Recommendation: legacyAdapterPlanTestRecommendation(), Definitions: definitions, PolicyDigest: "policy-v1", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	definitions[0]["function"].(map[string]interface{})["description"] = "changed after planning"
	_, err = RenderLegacyAdapterPlan(plan, definitions, now)
	var planErr *LegacyAdapterPlanError
	if !errors.As(err, &planErr) || planErr.Code != LegacyAdapterPlanInvalid {
		t.Fatalf("expected snapshot-drift rejection, got %v", err)
	}
}

func TestLegacyAdapterPlanSelectionCopiesAreImmutable(t *testing.T) {
	now := time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC)
	definitions := []map[string]interface{}{
		legacyAdapterPlanTestDefinition("read_file"), legacyAdapterPlanTestDefinition("async_wait"),
	}
	plan, err := BuildLegacyAdapterPlan(LegacyAdapterPlanInput{Recommendation: legacyAdapterPlanTestRecommendation(), Definitions: definitions, PolicyDigest: "policy-v1", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	first := plan.Selections()
	first[0].Effects[0] = EffectSensitive
	second := plan.Selections()
	if second[0].Effects[0] == EffectSensitive {
		t.Fatal("selection mutation leaked into immutable plan")
	}
}

func TestRecommendWithOptionsReturnsMatchingAtomicEvidence(t *testing.T) {
	router := NewRouter(NewDefinitionGenerator(nil, nil))
	tools := []map[string]interface{}{
		legacyAdapterPlanTestDefinition("task"),
		legacyAdapterPlanTestDefinition("async_wait"),
		legacyAdapterPlanTestDefinition("compress_context"),
		legacyAdapterPlanTestDefinition("read_file"),
		legacyAdapterPlanTestDefinition("unreviewed_reader"),
	}
	selected, recommendation := router.RecommendWithOptions("read a local file", tools, RouteOptions{})
	selectedNames := make(map[string]bool, len(selected))
	for _, definition := range selected {
		selectedNames[ExtractToolName(definition)] = true
	}
	if len(recommendation.Evidence) == 0 {
		t.Fatal("atomic recommendation omitted selected reviewed evidence")
	}
	for _, evidence := range recommendation.Evidence {
		if !selectedNames[evidence.ToolName] {
			t.Fatalf("evidence escaped its atomic selection: %+v", evidence)
		}
		if _, ok := LegacyAdapterProvisionForTool(evidence.ToolName, time.Now().UTC()); !ok {
			t.Fatalf("unreviewed evidence escaped recommendation: %+v", evidence)
		}
	}
}

func TestLegacyAdapterPlanPrunesOnlyOptionalForSchemaBudget(t *testing.T) {
	now := time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC)
	definitions := []map[string]interface{}{
		legacyAdapterPlanTestDefinition("async_wait"),
		legacyAdapterPlanTestDefinition("read_file"),
		legacyAdapterPlanTestDefinition("bash"),
	}
	recommendation := RoutingRecommendation{Evidence: []RoutingEvidence{
		{ToolName: "async_wait", Capability: "task.wait", AdapterContract: "legacy-task-wait-v1", Reason: "bootstrap", Score: 1},
		{ToolName: "read_file", Capability: "workspace.file.read", AdapterContract: "legacy-workspace-read-v1", Reason: "retrieval_candidate", Score: 0.9},
		{ToolName: "bash", Capability: "workspace.command.run", AdapterContract: "legacy-workspace-command-v1", Reason: "retrieval_candidate", Score: 0.1},
	}}
	baseline, err := BuildLegacyAdapterPlan(LegacyAdapterPlanInput{Recommendation: recommendation, Definitions: definitions, PolicyDigest: "policy-v1", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if baseline.SchemaTokens() < 3 {
		t.Fatalf("unexpected schema token estimate: %d", baseline.SchemaTokens())
	}
	budget := baseline.SchemaTokens() - baseline.Selections()[2].SchemaTokens
	plan, err := BuildLegacyAdapterPlan(LegacyAdapterPlanInput{Recommendation: recommendation, Definitions: definitions, PolicyDigest: "policy-v1", SchemaTokenBudget: budget, Now: now})
	if err != nil {
		t.Fatalf("BuildLegacyAdapterPlan() budget error = %v", err)
	}
	selections := plan.Selections()
	if len(selections) != 2 || selections[0].ToolName != "async_wait" || selections[1].ToolName != "read_file" {
		t.Fatalf("required/bootstrap or higher-score candidate was pruned: %+v", selections)
	}
	pruned := plan.PrunedOptionalEvidence()
	if len(pruned) != 1 || pruned[0].ToolName != "bash" {
		t.Fatalf("unexpected optional prune evidence: %+v", pruned)
	}
}

func TestLegacyAdapterPlanRejectsSchemaBudgetThatCannotFitRequired(t *testing.T) {
	now := time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC)
	_, err := BuildLegacyAdapterPlan(LegacyAdapterPlanInput{
		Recommendation: RoutingRecommendation{Evidence: []RoutingEvidence{{ToolName: "async_wait", Capability: "task.wait", AdapterContract: "legacy-task-wait-v1", Reason: "bootstrap", Score: 1}}},
		Definitions:    []map[string]interface{}{legacyAdapterPlanTestDefinition("async_wait")}, PolicyDigest: "policy-v1", SchemaTokenBudget: 1, Now: now,
	})
	var planErr *LegacyAdapterPlanError
	if !errors.As(err, &planErr) || planErr.Code != LegacyAdapterPlanOverBudget {
		t.Fatalf("expected plan_over_budget, got %v", err)
	}
}

func TestLegacyAdapterPlanKeepsHostPolicySelectionWhenOverBudget(t *testing.T) {
	now := time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC)
	definitions := []map[string]interface{}{
		legacyAdapterPlanTestDefinition("async_wait"),
		legacyAdapterPlanTestDefinition("read_file"),
	}
	_, err := BuildLegacyAdapterPlan(LegacyAdapterPlanInput{
		Recommendation: RoutingRecommendation{Evidence: []RoutingEvidence{
			{ToolName: "async_wait", Capability: "task.wait", AdapterContract: "legacy-task-wait-v1", Reason: "bootstrap", Score: 1},
			{ToolName: "read_file", Capability: "workspace.file.read", AdapterContract: "legacy-workspace-read-v1", Reason: "host_policy_required", Score: 1},
		}},
		Definitions: definitions, PolicyDigest: "policy-v1", SchemaTokenBudget: 1, Now: now,
	})
	var planErr *LegacyAdapterPlanError
	if !errors.As(err, &planErr) || planErr.Code != LegacyAdapterPlanOverBudget {
		t.Fatalf("host policy selection must not be silently pruned, got %v", err)
	}
}
