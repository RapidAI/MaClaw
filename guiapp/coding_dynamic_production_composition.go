package guiapp

// coding_dynamic_production_composition.go implements remediation doc
// §9.12 gate #3 slice 1: the dynamic factory's SINGLE IMMUTABLE production
// composition input. The factory consumes exactly {ToolScopePlan, admitted
// dynamic bindings, durable surface publish fn, coordinator route}; a
// candidate missing any component is rejected — there is NO by-name recovery.
// This slice makes the input contract real so eligibility becomes
// meaningful; it does NOT enable materialization (codingDynamicAliasesMay-
// Materialize stays false; slice 2 owns the cutover).

import (
	"encoding/json"
	"log"
	"strings"
	"time"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// codingDynamicAdmittedBindingSnapshot is the admitted-dynamic-bindings
// component of the composition. admitted=false means admission is
// unavailable and the candidate must be rejected; admitted=true with nil
// slices is the meaningful explicit "this scope has no dynamic providers"
// admission from setHostAdmittedDynamicBindings. The slices are cloned at
// construction so the snapshot cannot be mutated through the caller's maps.
type codingDynamicAdmittedBindingSnapshot struct {
	admitted bool
	skills   []codingSubAgentSkillMatch
	mcpTools []codingSubAgentMCPToolMatch
}

// codingDurableDynamicSurfacePublishFunc renders and durably publishes one
// surface revision. It mirrors App.publishCodingDurableDynamicSurface; the
// composition carries the bound method, never a name lookup.
type codingDurableDynamicSurfacePublishFunc func(identity *trustedCodingInvocationIdentity, prepared codingDynamicPlanPreparation, dynamic codingDynamicCatalogSnapshot, protocol, connectionID string, now time.Time) (*codingDurableDynamicSurface, error)

// codingDynamicProductionComposition is the single immutable production
// composition input the dynamic factory consumes. All four components must be
// present; a candidate missing any one is rejected with a logged reason and
// never falls back to by-name dispatch. The plan digest is captured at
// construction and re-verified at eligibility time, so mutating the plan
// after construction (digest drift) rejects the candidate.
type codingDynamicProductionComposition struct {
	planDigest string
	plan       codingDynamicPlanPreparation
	bindings   codingDynamicAdmittedBindingSnapshot
	publish    codingDurableDynamicSurfacePublishFunc
	route      *coretool.RouteRevisionRef
}

func newCodingDynamicProductionComposition(plan codingDynamicPlanPreparation, bindings codingDynamicAdmittedBindingSnapshot, publish codingDurableDynamicSurfacePublishFunc, route *coretool.RouteRevisionRef) *codingDynamicProductionComposition {
	return &codingDynamicProductionComposition{
		planDigest: codingDynamicPlanPreparationDigest(plan),
		plan:       plan,
		bindings: codingDynamicAdmittedBindingSnapshot{
			admitted: bindings.admitted,
			skills:   cloneCodingScopeSkillMatches(bindings.skills),
			mcpTools: cloneCodingScopeMCPMatches(bindings.mcpTools),
		},
		publish: publish,
		route:   route,
	}
}

// codingDynamicPlanPreparationDigest pins the immutable plan identity. An
// unmarshalable plan (empty digest) can never become eligible.
func codingDynamicPlanPreparationDigest(prepared codingDynamicPlanPreparation) string {
	encoded, err := json.Marshal(prepared.Plan)
	if err != nil {
		return ""
	}
	return coretool.SchemaDigest(encoded)
}

// missingComponents reports every absent component. An empty result means the
// candidate is complete. Explicit-empty admission (admitted=true, no
// bindings) is complete by design.
func (c *codingDynamicProductionComposition) missingComponents() []string {
	if c == nil {
		return []string{"composition candidate"}
	}
	var missing []string
	if strings.TrimSpace(c.planDigest) == "" || len(c.plan.Plan.Selections) == 0 {
		missing = append(missing, "tool scope plan")
	}
	if !c.bindings.admitted {
		missing = append(missing, "admitted dynamic bindings")
	}
	if c.publish == nil {
		missing = append(missing, "durable surface publish function")
	}
	if c.route == nil {
		missing = append(missing, "coordinator route")
	}
	return missing
}

// rejectionReason reports why a candidate is not eligible: a nil candidate, a
// missing component, or plan digest drift (the plan was mutated after
// construction). "" means eligible.
func (c *codingDynamicProductionComposition) rejectionReason() string {
	if c == nil {
		return "composition candidate is nil"
	}
	if missing := c.missingComponents(); len(missing) > 0 {
		return "composition is missing: " + strings.Join(missing, ", ")
	}
	if recomputed := codingDynamicPlanPreparationDigest(c.plan); recomputed != c.planDigest {
		return "composition plan digest drifted from its construction-time value"
	}
	return ""
}

// codingDynamicProductionCompositionEligible is the slice-1 eligibility gate.
// A complete input is accepted as an eligible candidate; anything else is
// rejected with a log line and a false return. There is no by-name recovery
// path: a rejected candidate stays rejected regardless of what the connected
// registry, matched skills, or MCP lists happen to contain.
func codingDynamicProductionCompositionEligible(c *codingDynamicProductionComposition) bool {
	if reason := c.rejectionReason(); reason != "" {
		log.Printf("[coding-dynamic] production composition rejected: %s", reason)
		return false
	}
	return true
}
