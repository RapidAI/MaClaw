package agentservice

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/permission"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
	"github.com/RapidAI/CodeClaw/corelib/tooldef"
)

func bridgeTestSnapshot(t *testing.T, rules ...permission.Rule) *permission.Snapshot {
	t.Helper()
	snap, err := permission.Load(permission.Options{ManagedRules: rules})
	if err != nil {
		t.Fatalf("permission.Load: %v", err)
	}
	return snap
}

func bridgeTestPlanAndGrants(t *testing.T) (coretool.ToolPlan, map[string]coretool.InvocationGrant) {
	t.Helper()
	plan := coretool.ToolPlan{Selections: []coretool.PlannedSelection{{
		ID:          "sel-web",
		AdapterName: "web_search",
		Effects:     []coretool.EffectClass{coretool.EffectReadOnly},
		FitProof:    coretool.FitProof{NeedID: "need-web", MatchedCapability: "information.search.web"},
	}}}
	grants := map[string]coretool.InvocationGrant{
		"invoke_web_search": {Token: "tok-1", AdapterName: "web_search", SelectionID: "sel-web"},
	}
	return plan, grants
}

func TestBridgePlannerPermissionsUnconditionalDenyViaAdapterName(t *testing.T) {
	snap := bridgeTestSnapshot(t, permission.Rule{Tool: "web_search", Effect: permission.EffectDeny, Reason: "no search"})
	plan, grants := bridgeTestPlanAndGrants(t)
	result := bridgePlannerPermissions(snap, plan, grants, []string{"invoke_web_search"})
	if len(result.Denies) != 1 {
		t.Fatalf("Denies = %d, want 1", len(result.Denies))
	}
	deny := result.Denies[0]
	if deny.Tool != "invoke_web_search" || deny.Adapter != "web_search" || deny.Effect != permission.EffectDeny {
		t.Fatalf("deny = %+v", deny)
	}
	if deny.Capability != "information.search.web" {
		t.Fatalf("Capability = %q, want information.search.web", deny.Capability)
	}
	if len(result.Asks) != 0 {
		t.Fatalf("Asks = %d, want 0", len(result.Asks))
	}
	if len(result.Constraints) != 1 {
		t.Fatalf("Constraints = %d, want 1", len(result.Constraints))
	}
	c := result.Constraints[0]
	if c.Capability != "information.search.web" || c.Effect != "deny" || c.Authority != coretool.AuthorityPolicy {
		t.Fatalf("constraint = %+v", c)
	}
	if c.Attributes["tool"] != "web_search" || c.Attributes["rule_source"] != "managed" {
		t.Fatalf("constraint attributes = %#v", c.Attributes)
	}
}

func TestBridgePlannerPermissionsArgsConditionalDenyNotSurfaceApplied(t *testing.T) {
	snap := bridgeTestSnapshot(t, permission.Rule{
		Tool: "web_search", Effect: permission.EffectDeny,
		When: &permission.ArgsPredicate{Field: "query", Equals: "secret"},
	})
	plan, grants := bridgeTestPlanAndGrants(t)
	result := bridgePlannerPermissions(snap, plan, grants, []string{"invoke_web_search"})
	if len(result.Denies) != 0 || len(result.Asks) != 0 || len(result.Constraints) != 0 {
		t.Fatalf("args-conditional deny must not surface-apply: %+v", result)
	}
}

func TestBridgePlannerPermissionsAskMapsToRequireConfirmation(t *testing.T) {
	snap := bridgeTestSnapshot(t, permission.Rule{Tool: "web_search", Effect: permission.EffectAsk, Reason: "confirm first"})
	plan, grants := bridgeTestPlanAndGrants(t)
	result := bridgePlannerPermissions(snap, plan, grants, []string{"invoke_web_search"})
	if len(result.Asks) != 1 || result.Asks[0].Effect != permission.EffectAsk {
		t.Fatalf("Asks = %+v", result.Asks)
	}
	if len(result.Denies) != 0 {
		t.Fatalf("Denies = %+v, want none", result.Denies)
	}
	if len(result.Constraints) != 1 || result.Constraints[0].Effect != "require_confirmation" {
		t.Fatalf("Constraints = %+v, want one require_confirmation", result.Constraints)
	}
}

func TestBridgePlannerPermissionsKindScopedDenyMatchesBuiltinKindOnly(t *testing.T) {
	execute := permission.KindExecute
	read := permission.KindRead
	snap := bridgeTestSnapshot(t,
		permission.Rule{Tool: "*", Effect: permission.EffectDeny, Kind: &execute},
		permission.Rule{Tool: "*", Effect: permission.EffectDeny, Kind: &read},
	)
	// bash is builtin execute kind, web_search builtin read; the unknown
	// invoke_* name has kind "" and cannot match a kind-scoped rule — the
	// surface is subject-less, so unclassifiable names escape kind rules.
	result := bridgePlannerPermissions(snap, coretool.ToolPlan{}, nil, []string{"bash", "web_search", "invoke_web_search"})
	if len(result.Denies) != 2 || result.Denies[0].Tool != "bash" || result.Denies[1].Tool != "web_search" {
		t.Fatalf("kind-scoped deny semantics wrong: %+v", result.Denies)
	}
}

func TestBridgePlannerPermissionsGlobalStarDeny(t *testing.T) {
	snap := bridgeTestSnapshot(t, permission.Rule{Tool: "*", Effect: permission.EffectDeny})
	result := bridgePlannerPermissions(snap, coretool.ToolPlan{}, nil, []string{"anything", "whatever"})
	if len(result.Denies) != 2 {
		t.Fatalf("global * deny must hit every surface name: %+v", result.Denies)
	}
}

func TestBridgePlannerPermissionsSurfaceNameDenyWithoutGrantBuildsNoConstraint(t *testing.T) {
	snap := bridgeTestSnapshot(t, permission.Rule{Tool: "invoke_web_search", Effect: permission.EffectDeny})
	result := bridgePlannerPermissions(snap, coretool.ToolPlan{}, nil, []string{"invoke_web_search"})
	if len(result.Denies) != 1 {
		t.Fatalf("Denies = %+v", result.Denies)
	}
	if len(result.Constraints) != 0 {
		t.Fatalf("no grant → no capability → no constraint, got %+v", result.Constraints)
	}
}

func TestBridgePlannerPermissionsAllowRuleNotReported(t *testing.T) {
	snap := bridgeTestSnapshot(t, permission.Rule{Tool: "web_search", Effect: permission.EffectAllow})
	plan, grants := bridgeTestPlanAndGrants(t)
	result := bridgePlannerPermissions(snap, plan, grants, []string{"invoke_web_search"})
	if len(result.Denies) != 0 || len(result.Asks) != 0 || len(result.Constraints) != 0 {
		t.Fatalf("allow rule must not be reported: %+v", result)
	}
}

func TestBridgePlannerPermissionsNilSnapshot(t *testing.T) {
	result := bridgePlannerPermissions(nil, coretool.ToolPlan{}, nil, []string{"bash"})
	if len(result.Denies) != 0 || len(result.Asks) != 0 || len(result.Constraints) != 0 {
		t.Fatalf("nil snapshot must yield empty result: %+v", result)
	}
}

// capturePlannerSurfaceHook swaps the dual-eval hook and returns the captures.
func capturePlannerSurfaceHook(t *testing.T) *[][]any {
	t.Helper()
	var captured [][]any
	prev := plannerSurfaceDualEvalHook
	plannerSurfaceDualEvalHook = func(tool string, legacyEffect, newEffect permission.Effect, rule *permission.Rule) {
		captured = append(captured, []any{tool, legacyEffect, newEffect, rule})
	}
	t.Cleanup(func() { plannerSurfaceDualEvalHook = prev })
	return &captured
}

func TestDualEvalManagedSurfacePermissionsSeam(t *testing.T) {
	t.Setenv(permissionDualEvalEnvKey, "")
	swapPermissionLoadFunc(t, func(permission.Options) (*permission.Snapshot, error) {
		return permission.Load(permission.Options{ManagedRules: []permission.Rule{
			{Tool: "web_search", Effect: permission.EffectDeny, Reason: "no search"},
		}})
	})
	captured := capturePlannerSurfaceHook(t)
	plan, grants := bridgeTestPlanAndGrants(t)
	cb := &coreAgentCallbacks{
		executor:               &CoreAgentExecutor{},
		dynamicSemanticSurface: &coreDynamicSemanticSurface{plan: plan, grants: grants},
	}
	defs := []map[string]interface{}{functionToolDefinition("invoke_web_search", "search", nil)}
	cb.dualEvalManagedSurfacePermissions(defs, cb.dynamicSemanticSurface)
	if len(*captured) != 1 {
		t.Fatalf("captured %d dual-eval reports, want 1", len(*captured))
	}
	report := (*captured)[0]
	if report[0] != "invoke_web_search" || report[1] != permission.EffectAllow || report[2] != permission.EffectDeny {
		t.Fatalf("report = %v", report)
	}
	rule, ok := report[3].(*permission.Rule)
	if !ok || rule == nil || rule.Source != "managed" || rule.Reason != "no search" {
		t.Fatalf("rule = %v", report[3])
	}
}

func TestDualEvalManagedSurfacePermissionsNilSafe(t *testing.T) {
	captured := capturePlannerSurfaceHook(t)
	var nilCallbacks *coreAgentCallbacks
	nilCallbacks.dualEvalManagedSurfacePermissions([]map[string]interface{}{{"function": map[string]interface{}{"name": "bash"}}}, nil)
	(&coreAgentCallbacks{}).dualEvalManagedSurfacePermissions([]map[string]interface{}{{"function": map[string]interface{}{"name": "bash"}}}, nil)
	// Kill switch on → snapshot nil → no reports.
	t.Setenv(permissionDualEvalEnvKey, "off")
	cb := &coreAgentCallbacks{executor: &CoreAgentExecutor{}}
	cb.dualEvalManagedSurfacePermissions([]map[string]interface{}{{"function": map[string]interface{}{"name": "bash"}}}, nil)
	if len(*captured) != 0 {
		t.Fatalf("captured %d reports, want 0", len(*captured))
	}
}

// Guard: the seam must stay behavior-neutral — the managed branch still
// returns the full surface while dual-eval only logs.
func TestDualEvalManagedSurfacePermissionsDoesNotChangeSurface(t *testing.T) {
	t.Setenv(permissionDualEvalEnvKey, "")
	issuer, err := coretool.NewInvocationIssuer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	resolver := dynamicNeedResolverFunc(func(_ context.Context, request DynamicCapabilityNeedRequest) (DynamicCapabilityNeedResolution, error) {
		return DynamicCapabilityNeedResolution{Managed: true, Needs: []coretool.CapabilityNeed{{
			ID: "need-web", Capability: "test.dynamic.execute", Polarity: coretool.NeedRequire, Required: true,
		}}}, nil
	})
	routing := DynamicSemanticRouting{
		Registry: dynamicSemanticRegistry(t), Resolver: resolver, Issuer: issuer,
		ExecutionStore: coretool.NewMemoryPlanExecutionStore(), RouteState: coretool.NewMemoryRouteStateStore(),
		HostCalls: coretool.NewMemoryHostCallJournal(), GrantTTL: time.Minute,
	}
	provider := &boundMCPProviderStub{entries: []MCPToolEntry{
		{ServerID: "approved", ToolName: "report", InputSchema: map[string]interface{}{
			"type": "object", "properties": map[string]interface{}{"q": map[string]interface{}{"type": "string"}}, "required": []string{"q"}, "additionalProperties": false,
		}, Contract: testDynamicCapabilityContract()},
	}}
	cb := &coreAgentCallbacks{
		ctx: context.Background(), principal: Principal{TenantID: "tenant", UserID: "user"},
		userText: "find the approved report", loopID: "session", dynamicOperationScope: "root",
		executor: &CoreAgentExecutor{}, dynamicSemanticRouting: &routing, mcpProvider: provider,
		dynamicSemanticManaged: true,
		lastPromptProfile:      agent.PromptProfileFull,
	}
	defs, managed := cb.dynamicSemanticToolDefinitions()
	if !managed || len(defs) != 1 {
		t.Fatalf("semantic defs=%#v managed=%v", defs, managed)
	}
	name := tooldef.Name(defs[0])
	surface := cb.dynamicSemanticSurface
	if surface == nil {
		t.Fatal("dynamic semantic surface missing")
	}
	grant, ok := surface.grants[name]
	if !ok {
		t.Fatalf("grant for %q missing", name)
	}
	adapter := strings.TrimSpace(grant.AdapterName)
	if adapter == "" {
		t.Fatal("grant adapter name empty")
	}
	// Deny the underlying adapter: dual-eval must log it, but the surface
	// must come back unchanged.
	swapPermissionLoadFunc(t, func(permission.Options) (*permission.Snapshot, error) {
		return permission.Load(permission.Options{ManagedRules: []permission.Rule{
			{Tool: adapter, Effect: permission.EffectDeny, Reason: "deny adapter"},
		}})
	})
	captured := capturePlannerSurfaceHook(t)
	built := cb.BuildTools("find the approved report")
	if len(built) != 1 || tooldef.Name(built[0]) != name {
		t.Fatalf("deny rule must not change the surface yet: %#v", built)
	}
	if len(*captured) != 1 || (*captured)[0][0] != name || (*captured)[0][2] != permission.EffectDeny {
		t.Fatalf("dual-eval reports = %v, want one deny for %q", *captured, name)
	}
}
