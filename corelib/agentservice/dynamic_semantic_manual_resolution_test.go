package agentservice

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// unknownDynamicEffectFixture walks one external effect the whole way to the
// state the exit exists for: dispatched, awaited, then given up on.
func unknownDynamicEffectFixture(t *testing.T) (DynamicSemanticRouting, *coretool.SQLiteSemanticExecutionCoordinator, coretool.InvocationScope, string, string, *boundMCPProviderStub) {
	t.Helper()
	routing, coordinator, scope, selectionID, operationID, provider := awaitingDynamicEffectFixture(t)
	principal := Principal{TenantID: "tenant", UserID: "user"}
	if err := routing.SettleDynamicSemanticExternalEffect(scope, principal, selectionID, operationID, DynamicEffectReceiptUnknown, "receipt_never_arrived", ""); err != nil {
		t.Fatalf("give up on the receipt: %v", err)
	}
	if record, err := coordinator.Executions.Execution(scope, selectionID); err != nil || record.State != coretool.PlanExecutionUnknown {
		t.Fatalf("fixture execution=%#v err=%v", record, err)
	}
	return routing, coordinator, scope, selectionID, operationID, provider
}

// awaitingDynamicEffectFixture stops one step earlier, at the state a real
// deployment actually reaches: dispatched, awaiting a receipt that no
// registered source will ever deliver.
func awaitingDynamicEffectFixture(t *testing.T) (DynamicSemanticRouting, *coretool.SQLiteSemanticExecutionCoordinator, coretool.InvocationScope, string, string, *boundMCPProviderStub) {
	t.Helper()
	registry := dynamicSemanticRegistry(t)
	coordinator, err := coretool.NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = coordinator.Close() })
	issuer, err := coretool.NewInvocationIssuerWithStore(make([]byte, 32), coordinator.Grants)
	if err != nil {
		t.Fatal(err)
	}
	entry := MCPToolEntry{ServerID: "server", ToolName: "send", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false}, Contract: DynamicCapabilityContract{
		Provisions: []coretool.CapabilityProvision{{Capability: "test.dynamic.execute", Quality: 1}},
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
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{RootTaskID: "root-resolve", SessionID: "session-resolve", TurnID: "turn-resolve", Snapshot: snapshot, Needs: []coretool.CapabilityNeed{{ID: "need", Capability: "test.dynamic.execute", Required: true}}})
	if err != nil || len(plan.Selections) != 1 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	scope := coretool.InvocationScope{RootTaskID: "root-resolve", PlanID: plan.ID, SessionID: "session-resolve", TurnID: "turn-resolve", PrincipalID: memoryOwnerIDForPrincipal(principal)}
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
	result, handled := surface.Execute(context.Background(), principal, provider, nil, name, `{}`, "call-resolve")
	if !handled || !result.AwaitingReceipt || provider.boundCalls != 1 {
		t.Fatalf("prepared result=%#v handled=%v calls=%d", result, handled, provider.boundCalls)
	}
	selectionID := plan.Selections[0].ID
	operationID := dynamicOperationKey(principal.TenantID, principal.UserID, scope.RootTaskID, "semantic:mcp:need", plan.Selections[0].Provider.StableID(), dynamicOperationDigest(map[string]interface{}{}))
	if record, err := coordinator.Executions.Execution(scope, selectionID); err != nil || record.State != coretool.PlanExecutionAwaitingReceipt {
		t.Fatalf("fixture execution=%#v err=%v", record, err)
	}
	return routing, coordinator, scope, selectionID, operationID, provider
}

// The two slices only mean something together. Dispatch parks the operation in
// awaiting_receipt, where the exit deliberately refuses to touch it; the lease
// converges it to unknown; and only then can a person settle it. Before the
// lease existed this sequence had no first step, so the exit could not reach
// the operations that actually accumulate.
func TestReceiptLeaseHandsTheOperationToTheManualExit(t *testing.T) {
	routing, coordinator, scope, selectionID, operationID, _ := awaitingDynamicEffectFixture(t)
	finding := DynamicSemanticManualResolution{OperationID: operationID, Succeeded: true, Evidence: "gateway console entry 8831", ResolvedBy: "operator-ana"}

	if err := routing.ResolveUnknownDynamicSemanticExternalEffect(finding); err == nil || !strings.Contains(err.Error(), "not_unknown") {
		t.Fatalf("a live receipt expectation was resolved by hand: %v", err)
	}
	changed, err := coordinator.ReconcileExpiredReceiptWaits(time.Now().UTC().Add(25*time.Hour), coretool.ExternalEffectReceiptLease)
	if err != nil || changed != 1 {
		t.Fatalf("lease changed=%d err=%v", changed, err)
	}
	if err := routing.ResolveUnknownDynamicSemanticExternalEffect(finding); err != nil {
		t.Fatalf("resolve after the lease expired: %v", err)
	}
	if record, err := coordinator.Executions.Execution(scope, selectionID); err != nil || record.State != coretool.PlanExecutionSucceeded {
		t.Fatalf("resolved execution=%#v err=%v", record, err)
	}
	if completed, err := coordinator.Routes.CompletedSelections(scope); err != nil || !completed[selectionID] {
		t.Fatalf("resolved completion=%#v err=%v", completed, err)
	}
}

// The point of the whole slice: an operation that nothing could ever move now
// has exactly one thing that can move it, and it settles the plan execution
// and the route the same way a real receipt would.
func TestManualResolutionIsTheExitFromAnUnknownDynamicEffect(t *testing.T) {
	routing, coordinator, scope, selectionID, operationID, provider := unknownDynamicEffectFixture(t)

	if err := routing.ResolveUnknownDynamicSemanticExternalEffect(DynamicSemanticManualResolution{
		OperationID: operationID, Succeeded: true,
		Evidence: "gateway console lists message 8831 as delivered", ResolvedBy: "operator-ana", ReasonCode: "manually_resolved",
	}); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if record, err := coordinator.Executions.Execution(scope, selectionID); err != nil || record.State != coretool.PlanExecutionSucceeded {
		t.Fatalf("resolved execution=%#v err=%v", record, err)
	}
	if completed, err := coordinator.Routes.CompletedSelections(scope); err != nil || !completed[selectionID] {
		t.Fatalf("resolved route completion=%#v err=%v", completed, err)
	}
	// Resolution writes down what happened; it must never make it happen
	// again. The effect was dispatched once, before anyone was uncertain.
	if provider.boundCalls != 1 {
		t.Fatalf("resolution redispatched the provider: calls=%d", provider.boundCalls)
	}
	record, ok, err := coordinator.ExternalEffectResolution(operationID)
	if err != nil || !ok || record.ResolvedBy != "operator-ana" {
		t.Fatalf("audit record=%#v ok=%v err=%v", record, ok, err)
	}
}

// Once resolved, the operation is settled like any other. A second operator
// with a different reading does not get to overturn it here.
func TestManualResolutionCannotOverturnASettledOperation(t *testing.T) {
	routing, _, _, _, operationID, _ := unknownDynamicEffectFixture(t)
	finding := DynamicSemanticManualResolution{OperationID: operationID, Succeeded: true, Evidence: "console entry 8831", ResolvedBy: "operator-ana"}

	if err := routing.ResolveUnknownDynamicSemanticExternalEffect(finding); err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	finding.Succeeded, finding.ResolvedBy, finding.Evidence = false, "operator-bo", "I think it bounced"
	if err := routing.ResolveUnknownDynamicSemanticExternalEffect(finding); err == nil || !strings.Contains(err.Error(), "not_unknown") {
		t.Fatalf("overturning err=%v", err)
	}
}

// A verdict is only worth the identity and the evidence behind it. Without
// either, the honest unknown it would replace is the better record.
func TestManualResolutionDemandsAnIdentifiedOperatorAndEvidence(t *testing.T) {
	routing, coordinator, scope, selectionID, operationID, _ := unknownDynamicEffectFixture(t)

	for _, missing := range []DynamicSemanticManualResolution{
		{OperationID: operationID, Succeeded: true, Evidence: "  ", ResolvedBy: "operator-ana"},
		{OperationID: operationID, Succeeded: true, Evidence: "console entry 8831", ResolvedBy: " "},
		{OperationID: "  ", Succeeded: true, Evidence: "console entry 8831", ResolvedBy: "operator-ana"},
	} {
		if err := routing.ResolveUnknownDynamicSemanticExternalEffect(missing); err == nil {
			t.Fatalf("incomplete resolution was accepted: %#v", missing)
		}
	}
	if record, err := coordinator.Executions.Execution(scope, selectionID); err != nil || record.State != coretool.PlanExecutionUnknown {
		t.Fatalf("execution moved on a refused verdict=%#v err=%v", record, err)
	}
	if _, ok, err := coordinator.ExternalEffectResolution(operationID); err != nil || ok {
		t.Fatalf("a refused verdict was recorded ok=%v err=%v", ok, err)
	}
}

// The operation named in the request is the only one that can be settled, and
// the scope, selection and provider it is checked against come from the
// ledger. An operator cannot supply them, so there is no operation they can
// aim a verdict at other than the one they wrote down.
func TestManualResolutionStaysBoundToTheOperationItNames(t *testing.T) {
	routing, coordinator, scope, selectionID, _, _ := unknownDynamicEffectFixture(t)

	err := routing.ResolveUnknownDynamicSemanticExternalEffect(DynamicSemanticManualResolution{
		OperationID: "operation-from-another-turn", Succeeded: true, Evidence: "console entry 8831", ResolvedBy: "operator-ana",
	})
	if err == nil {
		t.Fatal("a verdict landed on an operation that does not exist")
	}
	if record, err := coordinator.Executions.Execution(scope, selectionID); err != nil || record.State != coretool.PlanExecutionUnknown {
		t.Fatalf("unrelated execution=%#v err=%v", record, err)
	}
}

// A host still on the legacy in-memory ledger has nowhere to record who
// decided and no unknown-only guard to enforce. It gets no door rather than an
// unaudited one.
func TestManualResolutionIsUnavailableWithoutTheDurableCoordinator(t *testing.T) {
	routing := DynamicSemanticRouting{
		ExecutionStore: coretool.NewMemoryPlanExecutionStore(), RouteState: coretool.NewMemoryRouteStateStore(),
		EffectCoordinator: LedgerDynamicExternalEffectCoordinator{Ledger: NewMemoryDynamicOperationLedger(), ReceiptStore: NewMemoryDynamicEffectReceiptStore()},
	}
	err := routing.ResolveUnknownDynamicSemanticExternalEffect(DynamicSemanticManualResolution{
		OperationID: "operation", Succeeded: true, Evidence: "console entry 8831", ResolvedBy: "operator-ana",
	})
	if err == nil {
		t.Fatal("the legacy ledger accepted an unaudited resolution")
	}
}
