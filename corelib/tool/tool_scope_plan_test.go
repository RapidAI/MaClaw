package tool

import "testing"

func scopeProjectionDefinition(name string) map[string]interface{} {
	return map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": name}}
}

func TestToolScopePlanFromToolPlanProjectsSelectionDependencies(t *testing.T) {
	plan := ToolPlan{
		CatalogDigest:     "catalog-1",
		CatalogGeneration: 7,
		Selections: []PlannedSelection{
			{ID: "selection:generate", AdapterName: "generate_pdf", Requires: []string{"selection:search", "confirmation:generate"}},
			{ID: "selection:search", AdapterName: "web_search"},
		},
	}
	scope := ToolScopePlanFromToolPlan(plan, "scope-1", 4)
	if scope.ScopeID != "scope-1" || scope.CatalogDigest != "catalog-1" || scope.ScopeVersion != 7 {
		t.Fatalf("identity projection = %#v", scope)
	}
	if got, want := scope.AllowedNames, []string{"generate_pdf", "web_search"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("allowed names = %#v, want %#v", got, want)
	}
	if got := scope.Dependencies["generate_pdf"]; len(got) != 1 || got[0] != "web_search" {
		t.Fatalf("dependencies = %#v", scope.Dependencies)
	}
	if got := scope.RequiredNames; len(got) != 1 || got[0] != "web_search" {
		t.Fatalf("required roots = %#v", got)
	}
	if len(scope.Dependencies["web_search"]) != 0 {
		t.Fatalf("unexpected search dependencies = %#v", scope.Dependencies)
	}
}

func TestToolScopePlanFromToolPlanIncludesArtifactProducerEdge(t *testing.T) {
	plan := ToolPlan{Selections: []PlannedSelection{
		{ID: "selection:deliver", AdapterName: "send_file", ArtifactDependencies: []ArtifactDependency{{ProducerSelection: "selection:generate", Contract: ArtifactContract{Kind: "document", Required: true}}}},
		{ID: "selection:generate", AdapterName: "generate_pdf"},
	}}
	scope := ToolScopePlanFromToolPlan(plan, "scope-2", 0)
	if got := scope.Dependencies["send_file"]; len(got) != 1 || got[0] != "generate_pdf" {
		t.Fatalf("artifact edge = %#v", scope.Dependencies)
	}
}

func TestToolScopePlanFromToolPlanPreservesUnknownRequirement(t *testing.T) {
	plan := ToolPlan{Selections: []PlannedSelection{{
		ID: "selection:write", AdapterName: "write_file", Requires: []string{"selection:missing"},
	}}}
	scope := ToolScopePlanFromToolPlan(plan, "scope-3", 0)
	result := NewRouter(nil).RouteForScopePlan([]map[string]interface{}{scopeProjectionDefinition("write_file")}, scope)
	if result.Valid || result.DependencyClosed {
		t.Fatalf("unknown requirement was silently dropped: scope=%#v result=%#v", scope, result)
	}
	if len(result.MissingNames) != 1 || result.MissingNames[0] != "selection:missing" {
		t.Fatalf("unknown requirement diagnostics = %#v", result)
	}
}

func TestToolScopePlanFromToolPlanPreservesMissingArtifactProducer(t *testing.T) {
	plan := ToolPlan{Selections: []PlannedSelection{{
		ID:          "selection:deliver",
		AdapterName: "send_file",
		ArtifactDependencies: []ArtifactDependency{{
			ProducerSelection: "selection:missing",
			Contract:          ArtifactContract{Kind: "document", Required: true},
		}},
	}}}
	scope := ToolScopePlanFromToolPlan(plan, "scope-artifact-missing", 0)
	result := NewRouter(nil).RouteForScopePlan([]map[string]interface{}{scopeProjectionDefinition("send_file")}, scope)
	if result.Valid || result.DependencyClosed {
		t.Fatalf("missing artifact producer was silently dropped: scope=%#v result=%#v", scope, result)
	}
	if len(result.MissingNames) != 1 || result.MissingNames[0] != "selection:missing" {
		t.Fatalf("missing artifact diagnostics = %#v", result)
	}
}
