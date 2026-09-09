package guiapp

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// semanticScopeSurfaceIncompleteError is returned before a managed surface is
// published when the immutable planner decision cannot be rendered from the
// same catalog snapshot.  Keeping this as a typed error lets the host reject
// the turn without entering the legacy text router.
type semanticScopeSurfaceIncompleteError struct {
	Scope  tool.ToolScopePlan
	Result tool.ToolScopeRouteResult
}

func (e semanticScopeSurfaceIncompleteError) Error() string {
	reason := strings.TrimSpace(e.Result.Error)
	if reason == "" {
		reason = "scope_plan_incomplete"
	}
	return "semantic_scope_surface_incomplete:" + reason
}

// validateSemanticToolScope builds the host-admitted scope projection from a
// planner result and validates its complete definition/dependency closure.
// Definitions are sorted before validation to make diagnostics independent of
// registry iteration order. The router receives no user text and therefore
// cannot perform keyword, embedding, BM25, or reranker retrieval here.
func validateSemanticToolScope(plan tool.ToolPlan, definitions map[string]map[string]interface{}, rootTaskID, sessionID, turnID string) (tool.ToolScopePlan, tool.ToolScopeRouteResult) {
	identity := strings.Join([]string{strings.TrimSpace(rootTaskID), strings.TrimSpace(sessionID), strings.TrimSpace(turnID), strings.TrimSpace(plan.ID), strings.TrimSpace(plan.CatalogDigest)}, "\x00")
	scopeID := "semantic-scope:" + tool.SchemaDigest([]byte(identity))
	scope := tool.ToolScopePlanFromToolPlan(plan, scopeID, len(plan.Selections))
	return scope, routeSemanticToolScope(scope, definitions)
}

func routeSemanticToolScope(scope tool.ToolScopePlan, definitions map[string]map[string]interface{}) tool.ToolScopeRouteResult {
	return tool.NewRouter(nil).RouteForScopePlanByAdapter(definitions, scope)
}

func validatePreparedSemanticToolScope(prepared *semanticPlanPreparation) error {
	if prepared == nil {
		return fmt.Errorf("semantic_scope_preparation_required")
	}
	scope := prepared.scopePlan
	// The projection is immutable for the lifetime of a preparation.  A stale
	// or hand-built caller must not be able to swap the catalog generation (or
	// digest) between planning and publication; otherwise the directory could
	// validate against one snapshot while grants are issued for another.
	if strings.TrimSpace(scope.CatalogDigest) != strings.TrimSpace(prepared.plan.CatalogDigest) {
		return semanticScopeSurfaceError(scope, tool.ToolScopeRouteResult{
			Error:            "scope_catalog_digest_mismatch",
			DependencyClosed: false,
		})
	}
	if scope.CatalogGeneration != prepared.plan.CatalogGeneration {
		return semanticScopeSurfaceError(scope, tool.ToolScopeRouteResult{
			Error:            "scope_catalog_generation_mismatch",
			DependencyClosed: false,
		})
	}
	if strings.TrimSpace(scope.ScopeID) == "" {
		var result tool.ToolScopeRouteResult
		scope, result = validateSemanticToolScope(prepared.plan, prepared.definitions, prepared.rootTaskID, "", prepared.turnID)
		return semanticScopeSurfaceError(scope, result)
	}
	return semanticScopeSurfaceError(scope, routeSemanticToolScope(scope, prepared.definitions))
}

func semanticScopeSurfaceError(scope tool.ToolScopePlan, result tool.ToolScopeRouteResult) error {
	if result.Valid {
		return nil
	}
	return semanticScopeSurfaceIncompleteError{Scope: scope, Result: result}
}

// semanticScopeSurfaceDiagnostics is intentionally compact and safe for logs;
// it contains only adapter names/reason codes and no user text or arguments.
func semanticScopeSurfaceDiagnostics(err error) string {
	var incomplete semanticScopeSurfaceIncompleteError
	if !errors.As(err, &incomplete) {
		return ""
	}
	parts := make([]string, 0, len(incomplete.Result.MissingNames)+len(incomplete.Result.Omitted))
	for _, name := range incomplete.Result.MissingNames {
		parts = append(parts, "missing="+name)
	}
	for _, item := range incomplete.Result.Omitted {
		parts = append(parts, "omitted="+item.Name+":"+item.Reason)
	}
	sort.Strings(parts)
	return fmt.Sprintf("scope=%s %s", incomplete.Scope.ScopeID, strings.Join(parts, " "))
}
