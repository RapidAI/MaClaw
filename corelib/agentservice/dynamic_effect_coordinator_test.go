package agentservice

import (
	"context"
	"testing"
	"time"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

type dynamicEffectEventCollector struct {
	events []DynamicEffectEvent
}

func (c *dynamicEffectEventCollector) OnDynamicEffectEvent(event DynamicEffectEvent) {
	c.events = append(c.events, event)
}

func receiptBoundDynamicInvocation(t *testing.T) DynamicExternalEffectInvocation {
	t.Helper()
	registry := dynamicSemanticRegistry(t)
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
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{RootTaskID: "root", TurnID: "turn", Snapshot: snapshot, Needs: []coretool.CapabilityNeed{{ID: "need", Capability: "test.dynamic.execute", Required: true}}})
	if err != nil || len(plan.Selections) != 1 {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	return DynamicExternalEffectInvocation{
		Scope:     coretool.InvocationScope{RootTaskID: "root", PlanID: plan.ID, SessionID: "session", TurnID: "turn", PrincipalID: "tenant:user"},
		Principal: Principal{TenantID: "tenant", UserID: "user"}, Selection: plan.Selections[0], Arguments: map[string]interface{}{},
	}
}

func TestLedgerDynamicExternalEffectCoordinatorDoesNotRedispatchAwaitingOperation(t *testing.T) {
	ledger := NewMemoryDynamicOperationLedger()
	coordinator := LedgerDynamicExternalEffectCoordinator{Ledger: ledger}
	invocation := receiptBoundDynamicInvocation(t)
	calls := 0
	dispatch := func() (string, error) { calls++; return "provider returned", nil }
	first, err := coordinator.CoordinateDynamicExternalEffect(context.Background(), invocation, dispatch)
	if err != nil || first.State != DynamicEffectReceiptAwaiting || first.OperationID == "" || calls != 1 {
		t.Fatalf("first receipt=%#v err=%v calls=%d", first, err, calls)
	}
	second, err := coordinator.CoordinateDynamicExternalEffect(context.Background(), invocation, dispatch)
	if err != nil || second.State != DynamicEffectReceiptAwaiting || second.OperationID != first.OperationID || calls != 1 {
		t.Fatalf("replayed receipt=%#v first=%#v err=%v calls=%d", second, first, err, calls)
	}
}

func TestLedgerDynamicExternalEffectCoordinatorSettlesWithoutDispatch(t *testing.T) {
	ledger := NewMemoryDynamicOperationLedger()
	coordinator := LedgerDynamicExternalEffectCoordinator{Ledger: ledger, ReceiptStore: NewMemoryDynamicEffectReceiptStore()}
	invocation := receiptBoundDynamicInvocation(t)
	calls := 0
	first, err := coordinator.CoordinateDynamicExternalEffect(context.Background(), invocation, func() (string, error) { calls++; return "provider returned", nil })
	if err != nil || first.State != DynamicEffectReceiptAwaiting {
		t.Fatalf("prepare receipt=%#v err=%v", first, err)
	}
	settled, err := coordinator.SettleDynamicExternalEffect(DynamicExternalEffectSettlement{Scope: invocation.Scope, Principal: invocation.Principal, Selection: invocation.Selection, OperationID: first.OperationID, State: DynamicEffectReceiptAccepted, ReasonCode: "channel_accepted", Receipt: "receipt-1"})
	if err != nil || settled.State != DynamicEffectReceiptAccepted || !settled.Reconciled {
		t.Fatalf("settled receipt=%#v err=%v", settled, err)
	}
	if record, err := ledger.Get(first.OperationID); err != nil || record.ReceiptDigest == "" {
		t.Fatalf("settled ledger record=%#v err=%v", record, err)
	}
	recovered, err := coordinator.CoordinateDynamicExternalEffect(context.Background(), invocation, func() (string, error) { calls++; return "must not dispatch", nil })
	if err != nil || recovered.State != DynamicEffectReceiptAccepted || !recovered.Reconciled || calls != 1 {
		t.Fatalf("recovered receipt=%#v err=%v calls=%d", recovered, err, calls)
	}
}

func TestLedgerDynamicExternalEffectCoordinatorTransportFailureBecomesUnknown(t *testing.T) {
	ledger := NewMemoryDynamicOperationLedger()
	collector := &dynamicEffectEventCollector{}
	coordinator := LedgerDynamicExternalEffectCoordinator{Ledger: ledger, EventObserver: collector}
	invocation := receiptBoundDynamicInvocation(t)
	receipt, err := coordinator.CoordinateDynamicExternalEffect(context.Background(), invocation, func() (string, error) { return "", context.DeadlineExceeded })
	if err != nil || receipt.State != DynamicEffectReceiptUnknown || receipt.OperationID == "" {
		t.Fatalf("unknown receipt=%#v err=%v", receipt, err)
	}
	if record, err := ledger.Get(receipt.OperationID); err != nil || record.State != DynamicOperationUnknown {
		t.Fatalf("unknown record=%#v err=%v", record, err)
	}
	if len(collector.events) != 1 || collector.events[0] != (DynamicEffectEvent{Kind: DynamicEffectEventUnknown, ReasonCode: "dynamic_effect_dispatch_unknown"}) {
		t.Fatalf("effect events=%#v", collector.events)
	}
}

func TestLedgerDynamicExternalEffectCoordinatorRequiresTrustedReceiptForAcceptance(t *testing.T) {
	ledger := NewMemoryDynamicOperationLedger()
	invocation := receiptBoundDynamicInvocation(t)
	withoutReceipts := LedgerDynamicExternalEffectCoordinator{Ledger: ledger}
	prepared, err := withoutReceipts.CoordinateDynamicExternalEffect(context.Background(), invocation, func() (string, error) { return "provider returned", nil })
	if err != nil || prepared.State != DynamicEffectReceiptAwaiting {
		t.Fatalf("prepared receipt=%#v err=%v", prepared, err)
	}
	if _, err := withoutReceipts.SettleDynamicExternalEffect(DynamicExternalEffectSettlement{Scope: invocation.Scope, Principal: invocation.Principal, Selection: invocation.Selection, OperationID: prepared.OperationID, State: DynamicEffectReceiptAccepted, Receipt: "receipt"}); err == nil {
		t.Fatal("accepted settlement without receipt store")
	}
	if record, err := ledger.Get(prepared.OperationID); err != nil || record.State != DynamicOperationAwaitingReceipt {
		t.Fatalf("untrusted acceptance changed ledger record=%#v err=%v", record, err)
	}
}

func TestLedgerDynamicExternalEffectCoordinatorLateReceiptResolvesUnknownWithoutRedispatch(t *testing.T) {
	ledger := NewMemoryDynamicOperationLedger()
	coordinator := LedgerDynamicExternalEffectCoordinator{Ledger: ledger, ReceiptStore: NewMemoryDynamicEffectReceiptStore()}
	invocation := receiptBoundDynamicInvocation(t)
	calls := 0
	first, err := coordinator.CoordinateDynamicExternalEffect(context.Background(), invocation, func() (string, error) { calls++; return "", context.DeadlineExceeded })
	if err != nil || first.State != DynamicEffectReceiptUnknown || calls != 1 {
		t.Fatalf("unknown receipt=%#v err=%v calls=%d", first, err, calls)
	}
	settled, err := coordinator.SettleDynamicExternalEffect(DynamicExternalEffectSettlement{Scope: invocation.Scope, Principal: invocation.Principal, Selection: invocation.Selection, OperationID: first.OperationID, State: DynamicEffectReceiptAccepted, ReasonCode: "late_remote_receipt", Receipt: "receipt-after-timeout"})
	if err != nil || settled.State != DynamicEffectReceiptAccepted || !settled.Reconciled {
		t.Fatalf("late settled receipt=%#v err=%v", settled, err)
	}
	recovered, err := coordinator.CoordinateDynamicExternalEffect(context.Background(), invocation, func() (string, error) { calls++; return "must not dispatch", nil })
	if err != nil || recovered.State != DynamicEffectReceiptAccepted || !recovered.Reconciled || calls != 1 {
		t.Fatalf("recovered receipt=%#v err=%v calls=%d", recovered, err, calls)
	}
}
