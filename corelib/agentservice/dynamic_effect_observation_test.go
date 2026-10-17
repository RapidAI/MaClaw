package agentservice

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mcphttp "github.com/RapidAI/CodeClaw/corelib/mcp"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestSynchronousDynamicEffectObservation(t *testing.T) {
	installed := coretool.PlannedSelection{
		Provider: coretool.ProviderBinding{Kind: "mcp", ProviderID: "tengyun", ImplementationID: "query_worklog"},
		FitProof: coretool.FitProof{MatchedCapability: "x:mcp:tengyun:query_worklog"},
	}
	if !synchronousDynamicEffectObservation(installed) {
		t.Fatal("installed MCP address must be observed from its own response")
	}
	lookup := installed
	lookup.Provider.Kind = "skill"
	lookup.FitProof.MatchedCapability = CapabilityInformationLookup
	if !synchronousDynamicEffectObservation(lookup) {
		t.Fatal("open lookup must be observed from its own response")
	}
	for _, capability := range []coretool.CapabilityID{"record.update.worklog", "artifact.deliver.current_channel", "test.dynamic.execute"} {
		deferred := installed
		deferred.FitProof.MatchedCapability = capability
		if synchronousDynamicEffectObservation(deferred) {
			t.Fatalf("capability %s must keep its own receipt", capability)
		}
	}
	builtin := installed
	builtin.Provider.Kind = "builtin"
	if synchronousDynamicEffectObservation(builtin) {
		t.Fatal("a builtin selection must not take the MCP observation path")
	}
	namedUpdate := installed
	namedUpdate.Provider.ImplementationID = "work_log_update"
	if synchronousDynamicEffectObservation(namedUpdate) {
		t.Fatal("a reviewed mutation addressed by its x:mcp grant must keep its receipt")
	}
	namedRead := installed
	namedRead.Provider.ImplementationID = "work_log_query"
	if !synchronousDynamicEffectObservation(namedRead) {
		t.Fatal("a reviewed read addressed by its x:mcp grant is the returned text")
	}
}

func TestCompletedDynamicToolResultFailure(t *testing.T) {
	mutation := coretool.PlannedSelection{
		Provider: coretool.ProviderBinding{Kind: "mcp", ImplementationID: "work_log_update"},
		FitProof: coretool.FitProof{MatchedCapability: "record.update.worklog"},
	}
	if !completedDynamicToolResultFailure(mutation, "Error: quota exceeded") {
		t.Fatal("a deferred MCP tool error must be a completed failure")
	}
	if completedDynamicToolResultFailure(mutation, "wrote 3 rows") {
		t.Fatal("a deferred MCP success body must keep its receipt")
	}
	skill := mutation
	skill.Provider.Kind = "skill"
	skill.Provider.ImplementationID = "lookup"
	skill.FitProof.MatchedCapability = CapabilityInformationLookup
	if !completedDynamicToolResultFailure(skill, "Error: quota exceeded") {
		t.Fatal("an observed skill error body must be a completed failure")
	}
	deferredSkill := skill
	deferredSkill.FitProof.MatchedCapability = "artifact.deliver.current_channel"
	if completedDynamicToolResultFailure(deferredSkill, "Error: quota exceeded") {
		t.Fatal("a deferred skill body must keep its own receipt")
	}
}

func TestUnifiedCoordinatorReturnsInstalledMCPObservation(t *testing.T) {
	const capability = coretool.CapabilityID("x:mcp:tengyun:query_worklog")
	registry := dynamicSemanticRegistry(t)
	if err := registry.RegisterExternalCapability("mcp", coretool.CapabilityDescriptor{
		ID: capability, Version: "v1", Owner: "vendor", Summary: "Installed MCP tool query_worklog on server tengyun.",
		Effects: []coretool.EffectClass{coretool.EffectReadOnly, coretool.EffectExternalEffect},
	}); err != nil {
		t.Fatal(err)
	}
	coordinator, err := coretool.NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer coordinator.Close()
	issuer, err := coretool.NewInvocationIssuerWithStore(make([]byte, 32), coordinator.Grants)
	if err != nil {
		t.Fatal(err)
	}
	entry := MCPToolEntry{ServerID: "tengyun", ToolName: "query_worklog", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false}, Contract: DynamicCapabilityContract{
		Provisions: []coretool.CapabilityProvision{{Capability: capability, Quality: 1}},
		Effects:    []coretool.EffectClass{coretool.EffectExternalEffect},
	}}
	catalog, err := BuildDynamicSemanticCatalog([]MCPToolEntry{entry}, nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).Publish(catalog.Providers, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "root-observed", SessionID: "session-observed", TurnID: "turn-observed", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "need", Capability: capability, Required: true}},
	})
	if err != nil || len(plan.Selections) != 1 || plan.Selections[0].FitProof.MatchedCapability != capability {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	scope := coretool.InvocationScope{RootTaskID: "root-observed", PlanID: plan.ID, SessionID: "session-observed", TurnID: "turn-observed", PrincipalID: memoryOwnerIDForPrincipal(principal)}
	routing := DynamicSemanticRouting{
		Registry: registry, Issuer: issuer, ExecutionStore: coordinator.Executions, RouteState: coordinator.Routes, HostCalls: coordinator.HostCalls,
		Coordinator: coordinator, GrantTTL: time.Minute, EffectCoordinator: LedgerDynamicExternalEffectCoordinator{SemanticCoordinator: coordinator},
	}
	surface, err := newCoreDynamicSemanticSurface(routing, catalog, plan, scope)
	if err != nil {
		t.Fatal(err)
	}
	defs, err := surface.Definitions()
	if err != nil || len(defs) != 1 {
		t.Fatalf("definitions=%#v err=%v", defs, err)
	}
	name := defs[0]["function"].(map[string]interface{})["name"].(string)
	provider := &boundMCPProviderStub{entries: []MCPToolEntry{entry}}
	result, handled := surface.Execute(context.Background(), principal, provider, nil, name, `{}`, "call-observed")
	if !handled || !result.Succeeded || result.AwaitingReceipt || result.Unknown || !strings.Contains(result.Result, "bound") || result.ReasonCode != dynamicEffectObservedReason || provider.boundCalls != 1 {
		t.Fatalf("observed result=%#v handled=%v calls=%d", result, handled, provider.boundCalls)
	}
	selectionID := plan.Selections[0].ID
	if record, err := coordinator.Executions.Execution(scope, selectionID); err != nil || record.State != coretool.PlanExecutionSucceeded || record.ReasonCode != dynamicEffectObservedReason {
		t.Fatalf("execution=%#v err=%v", record, err)
	}
	operationID := dynamicOperationKey(principal.TenantID, principal.UserID, scope.RootTaskID, "semantic:mcp:need", plan.Selections[0].Provider.StableID(), dynamicOperationDigest(map[string]interface{}{}))
	operation, err := coordinator.ExternalEffectOperation(operationID)
	if err != nil || operation.State != coretool.SemanticExternalEffectSucceeded || operation.ReceiptDigest != coretool.SchemaDigest([]byte("bound")) {
		t.Fatalf("operation=%#v err=%v", operation, err)
	}
	if completed, err := coordinator.Routes.CompletedSelections(scope); err != nil || !completed[selectionID] {
		t.Fatalf("route completion=%#v err=%v", completed, err)
	}
	replay, handled := surface.Execute(context.Background(), principal, provider, nil, name, `{}`, "call-observed")
	if !handled || !replay.Succeeded || !strings.Contains(replay.Result, "bound") || provider.boundCalls != 1 {
		t.Fatalf("replay=%#v handled=%v calls=%d", replay, handled, provider.boundCalls)
	}
}

func TestUnifiedCoordinatorRecordsInstalledMCPToolFailure(t *testing.T) {
	t.Run("tool call error", func(t *testing.T) {
		provider := &boundMCPProviderStub{err: &mcphttp.ToolCallError{Message: "quota exceeded"}}
		assertInstalledMCPToolFailure(t, provider, "quota exceeded")
	})
	t.Run("error body", func(t *testing.T) {
		provider := &boundMCPProviderStub{result: "Error: quota exceeded"}
		assertInstalledMCPToolFailure(t, provider, "quota exceeded")
	})
}

func TestUnifiedCoordinatorKeepsLostMCPTransportUnknown(t *testing.T) {
	provider := &boundMCPProviderStub{err: context.DeadlineExceeded}
	surface, coordinator, plan, scope, principal, name := newInstalledMCPObservationSurface(t, "query_worklog", provider)
	result, handled := surface.Execute(context.Background(), principal, provider, nil, name, `{}`, "call-lost")
	if !handled || result.Succeeded || result.AwaitingReceipt || !result.Unknown || result.ReasonCode == dynamicMCPToolErrorReason || provider.boundCalls != 1 {
		t.Fatalf("lost transport=%#v handled=%v calls=%d", result, handled, provider.boundCalls)
	}
	selectionID := plan.Selections[0].ID
	if record, err := coordinator.Executions.Execution(scope, selectionID); err != nil || record.State != coretool.PlanExecutionUnknown {
		t.Fatalf("execution=%#v err=%v", record, err)
	}
	if completed, err := coordinator.Routes.CompletedSelections(scope); err != nil || completed[selectionID] {
		t.Fatalf("route completion=%#v err=%v", completed, err)
	}
}

func TestUnifiedCoordinatorRecordsDeferredMCPToolFailure(t *testing.T) {
	provider := &boundMCPProviderStub{result: "Error: quota exceeded"}
	surface, coordinator, plan, scope, principal, name := newInstalledMCPObservationSurface(t, "work_log_update", provider)
	result, handled := surface.Execute(context.Background(), principal, provider, nil, name, `{}`, "call-update-failed")
	if !handled || result.Succeeded || result.AwaitingReceipt || result.Unknown || result.ReasonCode != dynamicMCPToolErrorReason || !strings.Contains(result.Result, "quota exceeded") || provider.boundCalls != 1 {
		t.Fatalf("deferred tool failure=%#v handled=%v calls=%d", result, handled, provider.boundCalls)
	}
	selectionID := plan.Selections[0].ID
	if record, err := coordinator.Executions.Execution(scope, selectionID); err != nil || record.State != coretool.PlanExecutionFailed || record.ReasonCode != dynamicMCPToolErrorReason {
		t.Fatalf("execution=%#v err=%v", record, err)
	}
	if completed, err := coordinator.Routes.CompletedSelections(scope); err != nil || completed[selectionID] {
		t.Fatalf("route completion=%#v err=%v", completed, err)
	}
}

func TestUnifiedCoordinatorDoesNotObserveReviewedMCPMutation(t *testing.T) {
	provider := &boundMCPProviderStub{result: "wrote 3 rows"}
	surface, coordinator, plan, scope, principal, name := newInstalledMCPObservationSurface(t, "work_log_update", provider)
	result, handled := surface.Execute(context.Background(), principal, provider, nil, name, `{}`, "call-update")
	if !handled || result.Succeeded || !result.AwaitingReceipt || result.Unknown || strings.Contains(result.Result, "wrote 3 rows") || provider.boundCalls != 1 {
		t.Fatalf("mutation=%#v handled=%v calls=%d", result, handled, provider.boundCalls)
	}
	selectionID := plan.Selections[0].ID
	if record, err := coordinator.Executions.Execution(scope, selectionID); err != nil || record.State != coretool.PlanExecutionAwaitingReceipt {
		t.Fatalf("execution=%#v err=%v", record, err)
	}
	if completed, err := coordinator.Routes.CompletedSelections(scope); err != nil || completed[selectionID] {
		t.Fatalf("route completion=%#v err=%v", completed, err)
	}
}

func assertInstalledMCPToolFailure(t *testing.T, provider *boundMCPProviderStub, needle string) {
	t.Helper()
	surface, coordinator, plan, scope, principal, name := newInstalledMCPObservationSurface(t, "query_worklog", provider)
	result, handled := surface.Execute(context.Background(), principal, provider, nil, name, `{}`, "call-failed")
	if !handled || result.Succeeded || result.AwaitingReceipt || result.Unknown || result.ReasonCode != dynamicMCPToolErrorReason || !strings.Contains(result.Result, "Error:") || !strings.Contains(result.Result, needle) || provider.boundCalls != 1 {
		t.Fatalf("tool failure=%#v handled=%v calls=%d", result, handled, provider.boundCalls)
	}
	selectionID := plan.Selections[0].ID
	if record, err := coordinator.Executions.Execution(scope, selectionID); err != nil || record.State != coretool.PlanExecutionFailed || record.ReasonCode != dynamicMCPToolErrorReason {
		t.Fatalf("execution=%#v err=%v", record, err)
	}
	operationID := dynamicOperationKey(principal.TenantID, principal.UserID, scope.RootTaskID, "semantic:mcp:need", plan.Selections[0].Provider.StableID(), dynamicOperationDigest(map[string]interface{}{}))
	operation, err := coordinator.ExternalEffectOperation(operationID)
	if err != nil || operation.State != coretool.SemanticExternalEffectFailed || operation.ReceiptDigest != "" {
		t.Fatalf("operation=%#v err=%v", operation, err)
	}
	if completed, err := coordinator.Routes.CompletedSelections(scope); err != nil || completed[selectionID] {
		t.Fatalf("route completion=%#v err=%v", completed, err)
	}
	replay, handled := surface.Execute(context.Background(), principal, provider, nil, name, `{}`, "call-failed")
	if !handled || replay.Succeeded || replay.Unknown || !strings.Contains(replay.Result, needle) || provider.boundCalls != 1 {
		t.Fatalf("replay=%#v handled=%v calls=%d", replay, handled, provider.boundCalls)
	}
}

func TestReadOnlyMCPToolErrorDoesNotCompleteTheSelection(t *testing.T) {
	t.Run("error body", func(t *testing.T) {
		provider := &boundMCPProviderStub{result: "Error: quota exceeded"}
		surface, coordinator, plan, scope, principal, name := newInstalledMCPObservationSurfaceEffects(t, "query_worklog", provider, []coretool.EffectClass{coretool.EffectReadOnly})
		result, handled := surface.Execute(context.Background(), principal, provider, nil, name, `{}`, "call-readonly-failed")
		if !handled || result.Succeeded || result.AwaitingReceipt || result.Unknown || result.ReasonCode != dynamicMCPToolErrorReason || !strings.Contains(result.Result, "quota exceeded") || provider.boundCalls != 1 {
			t.Fatalf("read-only tool failure=%#v handled=%v calls=%d", result, handled, provider.boundCalls)
		}
		selectionID := plan.Selections[0].ID
		if record, err := coordinator.Executions.Execution(scope, selectionID); err != nil || record.State != coretool.PlanExecutionFailed || record.ReasonCode != dynamicMCPToolErrorReason {
			t.Fatalf("execution=%#v err=%v", record, err)
		}
		if completed, err := coordinator.Routes.CompletedSelections(scope); err != nil || completed[selectionID] {
			t.Fatalf("route completion=%#v err=%v", completed, err)
		}
		replay, handled := surface.Execute(context.Background(), principal, provider, nil, name, `{}`, "call-readonly-failed")
		if !handled || replay.Succeeded || !strings.Contains(replay.Result, "quota exceeded") || provider.boundCalls != 1 {
			t.Fatalf("replay=%#v handled=%v calls=%d", replay, handled, provider.boundCalls)
		}
	})
	t.Run("success body", func(t *testing.T) {
		provider := &boundMCPProviderStub{result: "weekly log"}
		surface, coordinator, plan, scope, principal, name := newInstalledMCPObservationSurfaceEffects(t, "query_worklog", provider, []coretool.EffectClass{coretool.EffectReadOnly})
		result, handled := surface.Execute(context.Background(), principal, provider, nil, name, `{}`, "call-readonly-ok")
		if !handled || !result.Succeeded || result.AwaitingReceipt || result.Unknown || !strings.Contains(result.Result, "weekly log") || provider.boundCalls != 1 {
			t.Fatalf("read-only success=%#v handled=%v calls=%d", result, handled, provider.boundCalls)
		}
		selectionID := plan.Selections[0].ID
		if record, err := coordinator.Executions.Execution(scope, selectionID); err != nil || record.State != coretool.PlanExecutionSucceeded {
			t.Fatalf("execution=%#v err=%v", record, err)
		}
		if completed, err := coordinator.Routes.CompletedSelections(scope); err != nil || !completed[selectionID] {
			t.Fatalf("route completion=%#v err=%v", completed, err)
		}
	})
}

func newInstalledMCPObservationSurface(t *testing.T, toolName string, provider *boundMCPProviderStub) (*coreDynamicSemanticSurface, *coretool.SQLiteSemanticExecutionCoordinator, coretool.ToolPlan, coretool.InvocationScope, Principal, string) {
	t.Helper()
	return newInstalledMCPObservationSurfaceEffects(t, toolName, provider, []coretool.EffectClass{coretool.EffectExternalEffect})
}

func newInstalledMCPObservationSurfaceEffects(t *testing.T, toolName string, provider *boundMCPProviderStub, effects []coretool.EffectClass) (*coreDynamicSemanticSurface, *coretool.SQLiteSemanticExecutionCoordinator, coretool.ToolPlan, coretool.InvocationScope, Principal, string) {
	t.Helper()
	capability := coretool.CapabilityID("x:mcp:tengyun:" + toolName)
	registry := dynamicSemanticRegistry(t)
	if err := registry.RegisterExternalCapability("mcp", coretool.CapabilityDescriptor{
		ID: capability, Version: "v1", Owner: "vendor", Summary: "Installed MCP tool " + toolName + " on server tengyun.",
		Effects: []coretool.EffectClass{coretool.EffectReadOnly, coretool.EffectExternalEffect},
	}); err != nil {
		t.Fatal(err)
	}
	coordinator, err := coretool.NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = coordinator.Close() })
	issuer, err := coretool.NewInvocationIssuerWithStore(make([]byte, 32), coordinator.Grants)
	if err != nil {
		t.Fatal(err)
	}
	entry := MCPToolEntry{ServerID: "tengyun", ToolName: toolName, InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false}, Contract: DynamicCapabilityContract{
		Provisions: []coretool.CapabilityProvision{{Capability: capability, Quality: 1}},
		Effects:    effects,
	}}
	provider.entries = []MCPToolEntry{entry}
	catalog, err := BuildDynamicSemanticCatalog([]MCPToolEntry{entry}, nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).Publish(catalog.Providers, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	scope := coretool.InvocationScope{RootTaskID: "root-" + toolName, PlanID: "", SessionID: "session-" + toolName, TurnID: "turn-" + toolName, PrincipalID: memoryOwnerIDForPrincipal(Principal{TenantID: "tenant", UserID: "user"})}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: scope.RootTaskID, SessionID: scope.SessionID, TurnID: scope.TurnID, Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "need", Capability: capability, Required: true}},
	})
	if err != nil || len(plan.Selections) != 1 || plan.Selections[0].FitProof.MatchedCapability != capability {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	scope.PlanID = plan.ID
	principal := Principal{TenantID: "tenant", UserID: "user"}
	scope.PrincipalID = memoryOwnerIDForPrincipal(principal)
	routing := DynamicSemanticRouting{
		Registry: registry, Issuer: issuer, ExecutionStore: coordinator.Executions, RouteState: coordinator.Routes, HostCalls: coordinator.HostCalls,
		Coordinator: coordinator, GrantTTL: time.Minute, EffectCoordinator: LedgerDynamicExternalEffectCoordinator{SemanticCoordinator: coordinator},
	}
	surface, err := newCoreDynamicSemanticSurface(routing, catalog, plan, scope)
	if err != nil {
		t.Fatal(err)
	}
	defs, err := surface.Definitions()
	if err != nil || len(defs) != 1 {
		t.Fatalf("definitions=%#v err=%v", defs, err)
	}
	name := defs[0]["function"].(map[string]interface{})["name"].(string)
	return surface, coordinator, plan, scope, principal, name
}
