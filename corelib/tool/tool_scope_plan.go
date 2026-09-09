package tool

import (
	"sort"
	"strings"
)

// ToolScopePlanFromToolPlan projects an immutable planner decision into the
// name based shape consumed by RouteForScopePlan.  The projection is a
// compatibility boundary only: names come from trusted PlannedSelection
// records and never from user text, classifier output, or a mutable registry.
//
// Requires contains both ordinary selection IDs and host-only confirmation
// IDs.  Confirmation facts are deliberately excluded from the tool scope;
// they are satisfied by the approval path and are not provider dependencies.
// ArtifactDependencies are included as edges as well, so a future caller
// cannot accidentally validate a surface that renders a consumer without its
// producer in the same admitted closure.
func ToolScopePlanFromToolPlan(plan ToolPlan, scopeID string, maxSelections int) ToolScopePlan {
	scope := ToolScopePlan{
		ScopeID:           strings.TrimSpace(scopeID),
		ScopeVersion:      plan.CatalogGeneration,
		CatalogDigest:     strings.TrimSpace(plan.CatalogDigest),
		CatalogGeneration: plan.CatalogGeneration,
		Dependencies:      make(map[string][]string),
		DisabledReasons:   make(map[string]string),
		MaxSelections:     maxSelections,
	}
	selectionByID := make(map[string]PlannedSelection, len(plan.Selections))
	adapterNames := make(map[string]bool, len(plan.Selections))
	for _, selection := range plan.Selections {
		id := strings.TrimSpace(selection.ID)
		adapter := strings.TrimSpace(selection.AdapterName)
		if id != "" {
			selectionByID[id] = selection
		}
		if adapter != "" {
			adapterNames[adapter] = true
		}
	}
	for name := range adapterNames {
		scope.AllowedNames = append(scope.AllowedNames, name)
	}
	sort.Strings(scope.AllowedNames)

	// A name is a required root when at least one selected occurrence of that
	// adapter has no in-plan dependency.  Repeated selections may share an
	// adapter, so roots are unioned by adapter name rather than by selection ID.
	rootNames := make(map[string]bool, len(adapterNames))
	for _, selection := range plan.Selections {
		adapter := strings.TrimSpace(selection.AdapterName)
		if adapter == "" {
			continue
		}
		hasToolDependency := false
		for _, requirement := range selection.Requires {
			requirement = strings.TrimSpace(requirement)
			if requirement == "" || strings.HasPrefix(requirement, "confirmation:") {
				continue
			}
			if _, ok := selectionByID[requirement]; ok {
				hasToolDependency = true
				addScopeDependency(scope.Dependencies, adapter, strings.TrimSpace(selectionByID[requirement].AdapterName))
				continue
			}
			// A host may already express an edge using an adapter name. For an
			// unknown requirement retain the literal value as an unadmitted edge;
			// RouteForScopePlan will report dependency_not_admitted instead of
			// silently treating the malformed requirement as a root.
			hasToolDependency = true
			addScopeDependency(scope.Dependencies, adapter, requirement)
		}
		for _, dependency := range selection.ArtifactDependencies {
			producerID := strings.TrimSpace(dependency.ProducerSelection)
			producer, producerKnown := selectionByID[producerID]
			producerAdapter := strings.TrimSpace(producer.AdapterName)
			if producerKnown && producerAdapter != "" {
				hasToolDependency = true
				addScopeDependency(scope.Dependencies, adapter, producerAdapter)
				continue
			}
			// Do not silently drop a required artifact edge when its producer
			// selection is absent or malformed. Preserve the trusted identifier
			// as an unadmitted dependency so the scope router reports an explicit
			// closure failure before publication.
			if producerID != "" {
				hasToolDependency = true
				addScopeDependency(scope.Dependencies, adapter, producerID)
			} else if dependency.Contract.Required {
				hasToolDependency = true
				addScopeDependency(scope.Dependencies, adapter, "artifact_producer_required")
			}
		}
		if !hasToolDependency {
			rootNames[adapter] = true
		}
	}
	for name := range rootNames {
		scope.RequiredNames = append(scope.RequiredNames, name)
	}
	sort.Strings(scope.RequiredNames)
	for name, dependencies := range scope.Dependencies {
		sort.Strings(dependencies)
		scope.Dependencies[name] = dependencies
	}
	return scope
}

func addScopeDependency(dependencies map[string][]string, name, dependency string) {
	name, dependency = strings.TrimSpace(name), strings.TrimSpace(dependency)
	if name == "" || dependency == "" || name == dependency {
		return
	}
	for _, existing := range dependencies[name] {
		if existing == dependency {
			return
		}
	}
	dependencies[name] = append(dependencies[name], dependency)
}
