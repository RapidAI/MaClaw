package agentservice

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestCoreDynamicSemanticBindingStalePublishesOneChildRevision(t *testing.T) {
	routing := testDynamicSemanticRouting(t)
	high := testDynamicCapabilityContract()
	high.Provisions[0].Quality = 2
	low := testDynamicCapabilityContract()
	low.Provisions[0].Quality = 0.4
	provider := &boundMCPProviderStub{entries: []MCPToolEntry{
		{ServerID: "primary", ToolName: "lookup", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false}, Contract: high},
		{ServerID: "fallback", ToolName: "lookup", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false}, Contract: low},
	}}
	callback := testManagedDynamicCallback(routing, provider, nil)
	defs, managed := callback.dynamicSemanticToolDefinitions()
	if !managed || len(defs) != 1 || callback.dynamicSemanticSurface == nil {
		t.Fatalf("parent defs=%#v managed=%v", defs, managed)
	}
	parentName := defs[0]["function"].(map[string]interface{})["name"].(string)
	parent := callback.dynamicSemanticSurface
	if parent.replan == nil || parent.replan.Attempts != 0 {
		t.Fatalf("parent replan input missing: %#v", parent.replan)
	}
	parentRevision, err := routing.RouteState.CurrentRevision(parent.scope)
	if err != nil {
		t.Fatalf("load parent revision: %v", err)
	}
	provider.err = errors.New("mcp_binding_stale")
	provider.entries = []MCPToolEntry{{
		ServerID: "fallback", ToolName: "lookup", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false}, Contract: low,
	}}
	result := callback.ExecuteToolCall(parentName, `{}`, "call-stale")
	if result.Outcome == "ok" || !strings.Contains(result.Result, "mcp_binding_stale") {
		t.Fatalf("stale call=%+v", result)
	}
	child := callback.dynamicSemanticSurface
	if child == nil || child == parent || child.replan == nil || child.replan.Attempts != 1 {
		t.Fatalf("expected one child revision, child=%#v", child)
	}
	if child.scope.RootTaskID != parent.scope.RootTaskID || child.scope.PlanID == parent.scope.PlanID {
		t.Fatalf("child scope=%+v parent=%+v", child.scope, parent.scope)
	}
	childRevision, err := routing.RouteState.CurrentRevision(child.scope)
	if err != nil {
		t.Fatalf("load child revision: %v", err)
	}
	if childRevision.Revision != parentRevision.Revision+1 {
		t.Fatalf("replan published %d revisions: parent=%+v child=%+v", childRevision.Revision-parentRevision.Revision, parentRevision, childRevision)
	}
	foundRecovery := false
	for _, event := range child.plan.Trace.Events {
		if event.Stage == coretool.TraceStageRecovery && event.Event == "child_published" {
			foundRecovery = true
		}
	}
	if !foundRecovery {
		t.Fatalf("child missing TP6 recovery event: %#v", child.plan.Trace.Events)
	}
	childDefs, err := child.Definitions()
	if err != nil || len(childDefs) != 1 {
		t.Fatalf("child defs=%#v err=%v", childDefs, err)
	}
	childName := childDefs[0]["function"].(map[string]interface{})["name"].(string)
	if childName == parentName {
		t.Fatal("child reused the retired parent function name")
	}
	provider.err = nil
	if got := callback.ExecuteToolCall(childName, `{}`, "call-child"); got.Outcome != "ok" {
		t.Fatalf("child execution=%+v", got)
	}
	if _, err := child.ReplanAfterBindingFailure(context.Background(), callback.principal, provider, nil, "mcp_binding_stale"); err == nil || !strings.Contains(err.Error(), "attempt exhausted") {
		t.Fatalf("second replan must fail closed: %v", err)
	}
}

func TestCoreDynamicSemanticReplanRejectsUnknownAndSchema(t *testing.T) {
	routing := testDynamicSemanticRouting(t)
	provider := &boundMCPProviderStub{entries: []MCPToolEntry{{
		ServerID: "server", ToolName: "lookup", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false}, Contract: testDynamicCapabilityContract(),
	}}}
	callback := testManagedDynamicCallback(routing, provider, nil)
	if _, managed := callback.dynamicSemanticToolDefinitions(); !managed || callback.dynamicSemanticSurface == nil {
		t.Fatal("expected managed surface")
	}
	parent := callback.dynamicSemanticSurface
	if _, err := parent.ReplanAfterBindingFailure(context.Background(), callback.principal, provider, nil, "mcp_execution_unknown"); err == nil || !strings.Contains(err.Error(), "not eligible") {
		t.Fatalf("unknown must not replan: %v", err)
	}
	if _, err := parent.ReplanAfterBindingFailure(context.Background(), callback.principal, provider, nil, "parameter_schema_invalid"); err == nil || !strings.Contains(err.Error(), "not eligible") {
		t.Fatalf("schema reject must not replan: %v", err)
	}
	if _, err := parent.ReplanAfterBindingFailure(context.Background(), callback.principal, provider, nil, "dynamic_effect_awaiting_receipt"); err == nil || !strings.Contains(err.Error(), "not eligible") {
		t.Fatalf("awaiting receipt must not replan: %v", err)
	}
	if parent.replan.Attempts != 0 {
		t.Fatalf("ineligible reasons must not consume the attempt: %#v", parent.replan)
	}
}

func TestCoreDynamicSemanticReplanRejectsCompetingRevisionAfterPlan(t *testing.T) {
	routing := testDynamicSemanticRouting(t)
	provider := &boundMCPProviderStub{entries: []MCPToolEntry{{
		ServerID: "primary", ToolName: "lookup", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false}, Contract: testDynamicCapabilityContract(),
	}}}
	callback := testManagedDynamicCallback(routing, provider, nil)
	if _, managed := callback.dynamicSemanticToolDefinitions(); !managed || callback.dynamicSemanticSurface == nil {
		t.Fatal("expected managed surface")
	}
	parent := callback.dynamicSemanticSurface
	parentRef, err := routing.RouteState.CurrentRevision(parent.scope)
	if err != nil {
		t.Fatal(err)
	}
	competingPlan := parent.plan
	competingPlan.ID = "plan:competing"
	competingScope := parent.scope
	competingScope.PlanID, competingScope.TurnID = competingPlan.ID, "competing"
	if _, err := routing.RouteState.PublishRevision(coretool.RouteRevisionPublishRequest{Scope: competingScope, Plan: competingPlan, ExpectedParent: &parentRef, SnapshotDigest: competingPlan.SnapshotDigest}, time.Now().UTC()); err != nil {
		t.Fatalf("publish competing revision: %v", err)
	}
	if _, err := parent.replanAfterPrepared(parent.plan, parent.catalog, parentRef, "mcp_binding_stale"); err == nil || !strings.Contains(err.Error(), "route_revision_conflict") {
		t.Fatalf("stale replan must reject competing revision: %v", err)
	}
	current, err := routing.RouteState.CurrentRevision(parent.scope)
	if err != nil {
		t.Fatal(err)
	}
	if current.PlanID != competingPlan.ID || current.Revision != parentRef.Revision+1 {
		t.Fatalf("stale replan advanced current route: parent=%+v current=%+v", parentRef, current)
	}
}

func TestCoreDynamicSemanticReplanFailsClosedWithoutInventory(t *testing.T) {
	routing := testDynamicSemanticRouting(t)
	provider := &boundMCPProviderStub{entries: []MCPToolEntry{{
		ServerID: "server", ToolName: "lookup", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false}, Contract: testDynamicCapabilityContract(),
	}}}
	callback := testManagedDynamicCallback(routing, provider, nil)
	if _, managed := callback.dynamicSemanticToolDefinitions(); !managed {
		t.Fatal("expected managed surface")
	}
	if _, err := callback.dynamicSemanticSurface.ReplanAfterBindingFailure(context.Background(), callback.principal, nil, nil, "dynamic_binding_stale"); err == nil || !strings.Contains(err.Error(), "inventory unavailable") {
		t.Fatalf("missing inventory must fail closed: %v", err)
	}
}

func TestReplanFailureEligibleVocabulary(t *testing.T) {
	if !coretool.ReplanFailureEligible("dynamic_binding_stale") || !coretool.ReplanFailureEligible("skill_bound_execution_unavailable") {
		t.Fatal("expected binding lifecycle reasons to be eligible")
	}
	if coretool.ReplanFailureEligible("host_call_conflict") || coretool.ReplanFailureEligible("") {
		t.Fatal("non-lifecycle reasons must not be eligible")
	}
	_ = time.Minute
}
