package agentservice

// Permission → planner bridge (docs/design/tool-routing-improvement-plan-zh.md
// Phase 1, item 3): maps the merged permission snapshot onto the agentservice
// semantic planner path so the snapshot becomes CONSTRAINT INPUT instead of a
// second Deny truth source. The planner's existing deny constraints and
// confirmation requirements stay the single execution semantics; permission
// rules are projected onto them.
//
// Dual-run discipline (this slice changes NO behavior): at managed-surface
// build time the bridge computes the tools an unconditional name-level deny
// rule would remove and the capabilities an ask rule would mark
// require_confirmation, then logs each divergence as
// [permission-dual-eval] gate=planner_surface. Nothing is filtered and no
// constraint reaches the planner yet.

import (
	"fmt"
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/permission"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
	"github.com/RapidAI/CodeClaw/corelib/tooldef"
)

// plannerSurfaceDualEvalGate names the surface-build gate in dual-eval logs.
const plannerSurfaceDualEvalGate = "planner_surface"

// plannerSurfaceDualEvalHook mirrors corelib/agent's dualEvalMismatchHook:
// tests capture reports instead of parsing logs.
var plannerSurfaceDualEvalHook = func(tool string, legacyEffect, newEffect permission.Effect, rule *permission.Rule) {
	coretool.RecordPermissionDualEvalMismatch(plannerSurfaceDualEvalGate, tool, string(legacyEffect), string(newEffect))
	src := ""
	reason := ""
	if rule != nil {
		src = rule.Source
		reason = rule.Reason
	}
	log.Printf("[permission-dual-eval] gate=%s tool=%q legacy=%s new=%s rule_source=%s reason=%q",
		plannerSurfaceDualEvalGate, tool, legacyEffect, newEffect, src, reason)
}

// plannerSurfaceEffect is one surface tool the snapshot matched with a
// concrete (unconditional) rule.
type plannerSurfaceEffect struct {
	// Tool is the model-visible surface name.
	Tool string
	// Adapter is the underlying adapter name when the match came through the
	// grant's adapter ("" when the model-visible name matched directly or no
	// grant exists).
	Adapter string
	// Capability is the planned capability behind the surface name ("" when
	// the name has no grant/selection, in which case no planner constraint can
	// be built).
	Capability coretool.CapabilityID
	Effect     permission.Effect
	Rule       permission.Rule
}

// plannerPermissionBridgeResult is the bridge output: per-tool surface effects
// plus the planner-consumable constraint projection for a future slice.
type plannerPermissionBridgeResult struct {
	Denies []plannerSurfaceEffect
	Asks   []plannerSurfaceEffect
	// Constraints projects deny rules to Effect "deny" and ask rules to Effect
	// "require_confirmation" (one per capability, AuthorityPolicy so the
	// planner's trusted-authority check accepts them). Not yet fed to the
	// planner — dual-run only.
	Constraints []coretool.RoutingConstraint
}

// bridgePlannerPermissions maps a permission snapshot onto the managed
// surface. Args are unknown at surface time, so each name is evaluated with
// nil args: ArgsPredicate rules can never match, and only unconditional
// name-level (or kind-level) rules surface here. Args-conditional rules stay
// at the execution-time gates, which already dual-evaluate them. Allow rules
// match legacy surface behavior and are not reported.
func bridgePlannerPermissions(snap *permission.Snapshot, plan coretool.ToolPlan, grants map[string]coretool.InvocationGrant, surfaceNames []string) plannerPermissionBridgeResult {
	var out plannerPermissionBridgeResult
	if snap == nil {
		return out
	}
	seenConstraint := map[string]bool{}
	for _, rawName := range surfaceNames {
		name := strings.TrimSpace(rawName)
		if name == "" {
			continue
		}
		decision, matchedName := plannerSurfacePermissionDecision(snap, grants, name)
		if decision.Rule == nil {
			continue
		}
		if decision.Effect != permission.EffectDeny && decision.Effect != permission.EffectAsk {
			continue
		}
		capability := plannerSurfaceCapability(plan, grants, name)
		effect := plannerSurfaceEffect{Tool: name, Capability: capability, Effect: decision.Effect, Rule: *decision.Rule}
		if matchedName != name {
			effect.Adapter = matchedName
		}
		switch decision.Effect {
		case permission.EffectDeny:
			out.Denies = append(out.Denies, effect)
		default:
			out.Asks = append(out.Asks, effect)
		}
		if capability == "" {
			continue
		}
		plannerEffect := "deny"
		if decision.Effect == permission.EffectAsk {
			plannerEffect = "require_confirmation"
		}
		key := string(capability) + "\x00" + plannerEffect
		if seenConstraint[key] {
			continue
		}
		seenConstraint[key] = true
		out.Constraints = append(out.Constraints, coretool.RoutingConstraint{
			ID:         fmt.Sprintf("permission:%s:%s:%s", decision.Rule.Source, plannerEffect, matchedName),
			Capability: capability,
			Effect:     plannerEffect,
			Attributes: map[string]string{"tool": matchedName, "rule_source": decision.Rule.Source},
			Authority:  coretool.AuthorityPolicy,
		})
	}
	return out
}

// plannerSurfacePermissionDecision evaluates one surface name against the
// snapshot. The model-visible name is evaluated first (it is what the model
// sees and what the execution-time dual-eval gate sees); only when it matches
// no rule does the grant's underlying adapter name get a chance, so a user
// rule naming the real tool (e.g. "web_search") still reaches a grant-token
// surface name. Kind falls back to the builtin table; unknown names keep kind
// "" so kind-scoped rules cannot match them (the surface is subject-less).
func plannerSurfacePermissionDecision(snap *permission.Snapshot, grants map[string]coretool.InvocationGrant, name string) (permission.Decision, string) {
	decision := snap.Decide(name, permission.BuiltinKind(name), nil)
	if decision.Rule != nil || grants == nil {
		return decision, name
	}
	adapter := ""
	if grant, ok := grants[name]; ok {
		adapter = strings.TrimSpace(grant.AdapterName)
	}
	if adapter == "" || adapter == name {
		return decision, name
	}
	adapterDecision := snap.Decide(adapter, permission.BuiltinKind(adapter), nil)
	if adapterDecision.Rule == nil {
		return decision, name
	}
	return adapterDecision, adapter
}

func plannerSurfaceCapability(plan coretool.ToolPlan, grants map[string]coretool.InvocationGrant, name string) coretool.CapabilityID {
	if grants == nil {
		return ""
	}
	grant, ok := grants[name]
	if !ok {
		return ""
	}
	selection, found := coretool.PlanSelectionByID(plan, grant.SelectionID)
	if !found {
		return ""
	}
	return selection.FitProof.MatchedCapability
}

// dualEvalManagedSurfacePermissions runs the planner-surface dual-eval at
// managed-surface build time: tools the snapshot would unconditionally deny
// (or mark ask) are logged as divergences; the surface itself is untouched.
func (c *coreAgentCallbacks) dualEvalManagedSurfacePermissions(managedTools []map[string]interface{}, surface *coreDynamicSemanticSurface) {
	if c == nil || c.executor == nil || len(managedTools) == 0 {
		return
	}
	snap := c.executor.permissionSnapshot()
	if snap == nil {
		return
	}
	var plan coretool.ToolPlan
	var grants map[string]coretool.InvocationGrant
	if surface != nil {
		plan = surface.plan
		grants = surface.grants
	}
	names := make([]string, 0, len(managedTools))
	for _, def := range managedTools {
		names = append(names, tooldef.Name(def))
	}
	result := bridgePlannerPermissions(snap, plan, grants, names)
	for _, deny := range result.Denies {
		rule := deny.Rule
		plannerSurfaceDualEvalHook(deny.Tool, permission.EffectAllow, permission.EffectDeny, &rule)
	}
	for _, ask := range result.Asks {
		rule := ask.Rule
		plannerSurfaceDualEvalHook(ask.Tool, permission.EffectAllow, permission.EffectAsk, &rule)
	}
}
