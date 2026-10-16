package tool

import (
	"sort"
	"strings"
)

// ToolScopePlan is the host-owned input to scope routing. It contains the
// already-resolved capability set; user wording is intentionally absent.
//
// AllowedNames is retained as a rendering-friendly projection of the plan.
// RequiredNames identifies roots that must be present for the plan to be
// executable. Dependencies are allowed only when they are also present in
// AllowedNames, so this API cannot silently widen a host admission decision.
type ToolScopePlan struct {
	ScopeID           string
	ScopeVersion      uint64
	CatalogDigest     string
	CatalogGeneration uint64
	AllowedNames      []string
	RequiredNames     []string
	Dependencies      map[string][]string
	DisabledReasons   map[string]string
	MaxSelections     int
}

// ToolScopeOmission explains why an admitted name was not rendered.
type ToolScopeOmission struct {
	Name   string
	Reason string
}

// ToolScopeRouteResult is a deterministic, diagnostic scope-routing result.
// Tools contains only definitions present in the host catalog. Missing and
// omitted names are returned explicitly instead of disappearing silently.
type ToolScopeRouteResult struct {
	Tools         []map[string]interface{}
	SelectedNames []string
	MissingNames  []string
	// Missing carries the reason paired with every MissingNames entry. Keep
	// MissingNames for callers that only need the stable name projection, but
	// never force a caller to infer whether a name was absent from the catalog
	// or omitted from the admission closure.
	Missing          []ToolScopeOmission
	Omitted          []ToolScopeOmission
	DependencyClosed bool
	Valid            bool
	Error            string
}

// RouteForScopePlan renders a host-admitted scope plan without consulting
// message text, classifiers, embeddings, BM25, or rerankers. Dependencies are
// emitted before their dependants in stable order.
func (r *Router) RouteForScopePlan(allTools []map[string]interface{}, plan ToolScopePlan) ToolScopeRouteResult {
	result := ToolScopeRouteResult{DependencyClosed: true}
	if strings.TrimSpace(plan.ScopeID) == "" {
		result.Error = "scope_id_required"
		result.DependencyClosed = false
	}
	if strings.TrimSpace(plan.CatalogDigest) == "" {
		if result.Error == "" {
			result.Error = "catalog_digest_required"
		} else {
			result.Error += ",catalog_digest_required"
		}
		result.DependencyClosed = false
	}

	allowed := normalizedNameSet(plan.AllowedNames)
	required := normalizedNameSet(plan.RequiredNames)
	if len(allowed) == 0 {
		if result.Error == "" {
			result.Error = "scope_names_required"
		} else {
			result.Error += ",scope_names_required"
		}
		result.DependencyClosed = false
	}
	if len(required) == 0 {
		// A plan that does not specify roots treats every admitted name as a
		// root. This keeps the method useful for read-only complete surfaces.
		for name := range allowed {
			required[name] = true
		}
	}

	definitions := make(map[string]map[string]interface{}, len(allTools))
	duplicateDefinitions := make(map[string]bool)
	for _, definition := range allTools {
		name := strings.TrimSpace(ExtractToolName(definition))
		if name == "" {
			continue
		}
		// Duplicate definitions are an authoring/catalog fault. Silently
		// choosing one would make a supposedly immutable scope depend on
		// registry iteration order and could pair a grant with the wrong schema.
		if _, exists := definitions[name]; !exists {
			definitions[name] = definition
		} else {
			duplicateDefinitions[name] = true
		}
	}

	// Keep the reason alongside the name. A boolean-only set would make a
	// missing dependency indistinguishable from a missing catalog definition,
	// which is exactly the kind of silent disappearance this API is meant to
	// prevent.
	missing := make(map[string]string)
	omitted := make(map[string]string)
	for name := range allowed {
		if reason := strings.TrimSpace(plan.DisabledReasons[name]); reason != "" {
			omitted[name] = reason
			continue
		}
		if duplicateDefinitions[name] {
			missing[name] = "duplicate_definition"
			continue
		}
		if _, ok := definitions[name]; !ok {
			missing[name] = "definition_missing"
		}
	}

	// Verify the admitted dependency closure. A dependency not included in
	// AllowedNames is reported as a plan error rather than being auto-added.
	closureReady := true
	var visit func(string, map[string]bool)
	visit = func(name string, stack map[string]bool) {
		if stack[name] {
			closureReady = false
			omitted[name] = "dependency_cycle"
			return
		}
		if !allowed[name] {
			closureReady = false
			if _, exists := missing[name]; !exists {
				missing[name] = "dependency_not_admitted"
			}
			return
		}
		if reason := omitted[name]; reason != "" {
			closureReady = false
			return
		}
		if _, ok := definitions[name]; !ok {
			closureReady = false
			missing[name] = "definition_missing"
			return
		}
		nextStack := make(map[string]bool, len(stack)+1)
		for k, v := range stack {
			nextStack[k] = v
		}
		nextStack[name] = true
		deps := append([]string(nil), plan.Dependencies[name]...)
		sort.Strings(deps)
		for _, dep := range deps {
			dep = strings.TrimSpace(dep)
			if dep == "" {
				continue
			}
			visit(dep, nextStack)
		}
	}

	roots := sortedNames(required)
	for _, name := range roots {
		if !allowed[name] {
			closureReady = false
			missing[name] = "required_not_admitted"
			continue
		}
		visit(name, nil)
	}
	// Include optional admitted names after required roots. They still receive
	// dependency validation and deterministic ordering.
	optional := make([]string, 0, len(allowed))
	for name := range allowed {
		if !required[name] {
			optional = append(optional, name)
		}
	}
	sort.Strings(optional)
	for _, name := range optional {
		visit(name, nil)
	}

	// Topologically render only names that are actually usable. Required roots
	// are visited first, so a selection budget cannot silently evict them.
	ordered := make([]string, 0, len(allowed))
	seen := make(map[string]bool, len(allowed))
	var emit func(string)
	emit = func(name string) {
		if seen[name] || !allowed[name] || missing[name] != "" || omitted[name] != "" {
			return
		}
		seen[name] = true
		deps := append([]string(nil), plan.Dependencies[name]...)
		sort.Strings(deps)
		for _, dep := range deps {
			emit(strings.TrimSpace(dep))
		}
		ordered = append(ordered, name)
	}
	for _, name := range roots {
		emit(name)
	}
	for _, name := range optional {
		emit(name)
	}

	maxSelections := plan.MaxSelections
	if maxSelections < 0 {
		result.Error = "max_selections_invalid"
		result.DependencyClosed = false
	} else if maxSelections > 0 && len(ordered) > maxSelections {
		for _, name := range ordered[maxSelections:] {
			omitted[name] = "selection_budget"
		}
		ordered = ordered[:maxSelections]
		closureReady = false
	}

	// Rebuild selected definitions from the topological order. A name may have
	// been omitted by the budget or a dependency error after ordering.
	for _, name := range ordered {
		if reason := omitted[name]; reason != "" {
			continue
		}
		if missing[name] != "" {
			continue
		}
		result.SelectedNames = append(result.SelectedNames, name)
		result.Tools = append(result.Tools, definitions[name])
	}

	for name, reason := range missing {
		result.MissingNames = append(result.MissingNames, name)
		result.Missing = append(result.Missing, ToolScopeOmission{Name: name, Reason: reason})
	}
	sort.Strings(result.MissingNames)
	sort.Slice(result.Missing, func(i, j int) bool { return result.Missing[i].Name < result.Missing[j].Name })
	omittedNames := make([]string, 0, len(omitted))
	for name := range omitted {
		omittedNames = append(omittedNames, name)
	}
	sort.Strings(omittedNames)
	for _, name := range omittedNames {
		result.Omitted = append(result.Omitted, ToolScopeOmission{Name: name, Reason: omitted[name]})
	}
	result.DependencyClosed = closureReady && len(result.MissingNames) == 0 && result.Error == ""
	result.Valid = result.DependencyClosed && result.Error == ""
	if !result.Valid && result.Error == "" {
		result.Error = "scope_plan_incomplete"
	}
	return result
}

// RouteForScopePlanByAdapter validates a scope against a trusted adapter-keyed
// definition catalog. Dynamic Skill/MCP definitions intentionally carry the
// fixed model-facing placeholder name "dynamic_provider"; their map key is
// the resolver identity selected by ToolPlan and is therefore the only valid
// identity for scope closure. The returned Tools retain the original trusted
// definitions so callers can render them through CatalogRenderer later.
func (r *Router) RouteForScopePlanByAdapter(definitions map[string]map[string]interface{}, plan ToolScopePlan) ToolScopeRouteResult {
	if len(definitions) == 0 {
		return r.RouteForScopePlan(nil, plan)
	}
	keys := make([]string, 0, len(definitions))
	originals := make(map[string]map[string]interface{}, len(definitions))
	allTools := make([]map[string]interface{}, 0, len(definitions))
	for rawName, definition := range definitions {
		name := strings.TrimSpace(rawName)
		if name == "" {
			continue
		}
		keys = append(keys, name)
		// A map key is an identity claim, not a substitute for a renderable
		// trusted definition. Malformed/nil entries must remain missing so a
		// caller cannot make an incomplete scope appear valid by key alone.
		cloned, ok := cloneJSONValue(definition).(map[string]interface{})
		if !ok || cloned == nil {
			continue
		}
		fn, ok := cloned["function"].(map[string]interface{})
		if !ok || fn == nil {
			continue
		}
		if _, exists := originals[name]; !exists {
			originals[name] = definition
		}
		// RouteForScopePlan's legacy slice API derives identity from
		// function.name. Clone only the small identity envelope so dynamic
		// placeholder names do not mutate the trusted source definition.
		fn["name"] = name
		allTools = append(allTools, cloned)
	}
	result := r.RouteForScopePlan(allTools, plan)
	if len(result.SelectedNames) == 0 {
		return result
	}
	result.Tools = make([]map[string]interface{}, 0, len(result.SelectedNames))
	for _, name := range result.SelectedNames {
		if definition, ok := originals[name]; ok {
			result.Tools = append(result.Tools, definition)
		}
	}
	return result
}

func normalizedNameSet(names []string) map[string]bool {
	out := make(map[string]bool, len(names))
	for _, name := range names {
		if name = strings.TrimSpace(name); name != "" {
			out[name] = true
		}
	}
	return out
}

func sortedNames(set map[string]bool) []string {
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
