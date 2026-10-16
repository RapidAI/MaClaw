package tool

import (
	"strings"
	"testing"
)

func scopeTestDefinition(name string) map[string]interface{} {
	return map[string]interface{}{
		"type":     "function",
		"function": map[string]interface{}{"name": name},
	}
}

func TestRouteForScopePlanUsesDependencyClosureAndStableOrder(t *testing.T) {
	r := NewRouter(nil)
	plan := ToolScopePlan{
		ScopeID:           "scope-1",
		CatalogDigest:     "catalog-1",
		CatalogGeneration: 3,
		AllowedNames:      []string{"db.query", "db.connect", "db.inspect"},
		RequiredNames:     []string{"db.query"},
		Dependencies: map[string][]string{
			"db.query": {"db.connect", "db.inspect"},
		},
		MaxSelections: 10,
	}
	// Deliberately scramble the catalog and use wording nowhere in the API.
	got := r.RouteForScopePlan([]map[string]interface{}{
		scopeTestDefinition("db.query"),
		scopeTestDefinition("db.inspect"),
		scopeTestDefinition("db.connect"),
	}, plan)
	if !got.Valid || !got.DependencyClosed {
		t.Fatalf("result = %#v", got)
	}
	want := []string{"db.connect", "db.inspect", "db.query"}
	if len(got.SelectedNames) != len(want) {
		t.Fatalf("selected = %#v, want %#v", got.SelectedNames, want)
	}
	for i, name := range want {
		if got.SelectedNames[i] != name {
			t.Fatalf("selected[%d] = %q, want %q", i, got.SelectedNames[i], name)
		}
	}
}

func TestRouteForScopePlanReportsMissingAndUnadmittedDependencies(t *testing.T) {
	r := NewRouter(nil)
	got := r.RouteForScopePlan([]map[string]interface{}{
		scopeTestDefinition("write"),
	}, ToolScopePlan{
		ScopeID:       "scope-2",
		CatalogDigest: "catalog-2",
		AllowedNames:  []string{"write"},
		RequiredNames: []string{"write"},
		Dependencies:  map[string][]string{"write": {"connect", "missing"}},
	})
	if got.Valid || got.DependencyClosed {
		t.Fatalf("incomplete plan reported valid: %#v", got)
	}
	if got.Error == "" || len(got.MissingNames) != 2 {
		t.Fatalf("diagnostics = %#v", got)
	}
}

func TestRouteForScopePlanMakesBudgetFailureExplicit(t *testing.T) {
	r := NewRouter(nil)
	got := r.RouteForScopePlan([]map[string]interface{}{
		scopeTestDefinition("a"),
		scopeTestDefinition("b"),
	}, ToolScopePlan{
		ScopeID:       "scope-3",
		CatalogDigest: "catalog-3",
		AllowedNames:  []string{"a", "b"},
		MaxSelections: 1,
	})
	if got.Valid || got.Error == "" {
		t.Fatalf("budget truncation was not explicit: %#v", got)
	}
	if len(got.Omitted) != 1 || got.Omitted[0].Reason != "selection_budget" {
		t.Fatalf("omitted = %#v", got.Omitted)
	}
}

func TestRouteForScopePlanRejectsDuplicateDefinitions(t *testing.T) {
	r := NewRouter(nil)
	got := r.RouteForScopePlan([]map[string]interface{}{
		scopeTestDefinition("read"),
		scopeTestDefinition("read"),
	}, ToolScopePlan{
		ScopeID:       "scope-duplicate",
		CatalogDigest: "catalog-duplicate",
		AllowedNames:  []string{"read"},
		RequiredNames: []string{"read"},
	})
	if got.Valid || got.DependencyClosed || got.Error == "" {
		t.Fatalf("duplicate definition must fail closed: %#v", got)
	}
	if len(got.Missing) != 1 || got.Missing[0].Name != "read" || got.Missing[0].Reason != "duplicate_definition" {
		t.Fatalf("duplicate diagnostics = %#v", got.Missing)
	}
}

func TestRouteForScopePlanRejectsEmptyAdmission(t *testing.T) {
	r := NewRouter(nil)
	got := r.RouteForScopePlan(nil, ToolScopePlan{ScopeID: "scope-empty", CatalogDigest: "catalog-empty"})
	if got.Valid || got.Error == "" || !strings.Contains(got.Error, "scope_names_required") {
		t.Fatalf("empty admission must fail closed: %#v", got)
	}
}

func TestRouteForScopePlanByAdapterUsesTrustedMapIdentity(t *testing.T) {
	r := NewRouter(nil)
	// Dynamic definitions deliberately use a fixed placeholder function name;
	// the adapter-keyed catalog is the trusted resolver identity.
	definition := map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":       "dynamic_provider",
			"parameters": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false},
		},
	}
	adapter := "dynamic_mcp_bound_123"
	got := r.RouteForScopePlanByAdapter(map[string]map[string]interface{}{adapter: definition}, ToolScopePlan{
		ScopeID:       "scope-dynamic",
		CatalogDigest: "catalog-dynamic",
		AllowedNames:  []string{adapter},
		RequiredNames: []string{adapter},
	})
	if !got.Valid || !got.DependencyClosed {
		t.Fatalf("dynamic adapter scope rejected: %#v", got)
	}
	if len(got.SelectedNames) != 1 || got.SelectedNames[0] != adapter {
		t.Fatalf("selected names = %#v", got.SelectedNames)
	}
	if len(got.Tools) != 1 || ExtractToolName(got.Tools[0]) != "dynamic_provider" {
		t.Fatalf("returned trusted definition was rewritten: %#v", got.Tools)
	}
}

func TestRouteForScopePlanByAdapterRejectsMalformedDefinition(t *testing.T) {
	result := NewRouter(nil).RouteForScopePlanByAdapter(map[string]map[string]interface{}{
		"dynamic_mcp_malformed": nil,
	}, ToolScopePlan{
		ScopeID:       "scope-malformed",
		CatalogDigest: "catalog-malformed",
		AllowedNames:  []string{"dynamic_mcp_malformed"},
		RequiredNames: []string{"dynamic_mcp_malformed"},
	})
	if result.Valid || result.DependencyClosed || result.Error == "" {
		t.Fatalf("malformed definition must fail closed: %#v", result)
	}
	if len(result.Missing) != 1 || result.Missing[0].Reason != "definition_missing" {
		t.Fatalf("malformed definition diagnostics = %#v", result.Missing)
	}
}
