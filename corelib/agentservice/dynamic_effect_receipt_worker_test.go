package agentservice

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// workerReceiptSourceStub is a fake binding-specific receipt integration. It
// only observes; the preparation counter on the provider stub proves the
// worker never enters the dispatch path.
type workerReceiptSourceStub struct {
	bindingID    string
	observations []DynamicEffectReceiptObservation
	err          error
	calls        atomic.Int32
}

func (s *workerReceiptSourceStub) BindingID() string { return s.bindingID }

func (s *workerReceiptSourceStub) ObserveDynamicEffectReceipts(_ context.Context, accept func(DynamicEffectReceiptObservation) error) error {
	s.calls.Add(1)
	if s.err != nil {
		return s.err
	}
	for _, observation := range s.observations {
		if err := accept(observation); err != nil {
			return err
		}
	}
	return nil
}

// prepareWorkerAwaitingReceiptOperation routes one external_effect selection
// and prepares its durable operation, which remains awaiting_receipt until a
// trusted source settles it.
func prepareWorkerAwaitingReceiptOperation(t *testing.T) (DynamicSemanticRouting, coretool.InvocationScope, Principal, string, string, string, *boundMCPProviderStub) {
	t.Helper()
	registry := dynamicSemanticRegistry(t)
	issuer, err := coretool.NewInvocationIssuer(make([]byte, 32))
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
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{RootTaskID: "root", SessionID: "session", TurnID: "turn", Snapshot: snapshot, Needs: []coretool.CapabilityNeed{{ID: "need", Capability: "test.dynamic.execute", Required: true}}})
	if err != nil || len(plan.Selections) != 1 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	ledger := NewMemoryDynamicOperationLedger()
	routing := DynamicSemanticRouting{Registry: registry, Issuer: issuer, ExecutionStore: coretool.NewMemoryPlanExecutionStore(), RouteState: coretool.NewMemoryRouteStateStore(), HostCalls: coretool.NewMemoryHostCallJournal(), GrantTTL: time.Minute, EffectCoordinator: LedgerDynamicExternalEffectCoordinator{Ledger: ledger, ReceiptStore: NewMemoryDynamicEffectReceiptStore()}}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	scope := coretool.InvocationScope{RootTaskID: "root", PlanID: plan.ID, SessionID: "session", TurnID: "turn", PrincipalID: memoryOwnerIDForPrincipal(principal)}
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
	result, handled := surface.Execute(context.Background(), principal, provider, nil, name, `{}`, "call-1")
	if !handled || !result.AwaitingReceipt || provider.boundCalls != 1 {
		t.Fatalf("prepared result=%#v handled=%v calls=%d", result, handled, provider.boundCalls)
	}
	selectionID := plan.Selections[0].ID
	bindingID := plan.Selections[0].Provider.StableID()
	operationID := dynamicOperationKey(principal.TenantID, principal.UserID, scope.RootTaskID, "semantic:mcp:need", bindingID, dynamicOperationDigest(map[string]interface{}{}))
	return routing, scope, principal, selectionID, operationID, bindingID, provider
}

func TestDynamicEffectReceiptWorkerSettlesTrustedReceipt(t *testing.T) {
	routing, scope, _, selectionID, operationID, bindingID, provider := prepareWorkerAwaitingReceiptOperation(t)
	worker, err := NewDynamicEffectReceiptWorker(routing.ReconcileDynamicEffectReceiptSource, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	source := &workerReceiptSourceStub{bindingID: bindingID, observations: []DynamicEffectReceiptObservation{{OperationID: operationID, State: DynamicEffectReceiptAccepted, ReasonCode: "trusted_provider_accepted", Receipt: "receipt-accepted"}}}
	if err := worker.RegisterSource(source); err != nil {
		t.Fatal(err)
	}
	worker.ReconcileNow(context.Background())
	if record, err := routing.ExecutionStore.Execution(scope, selectionID); err != nil || record.State != coretool.PlanExecutionSucceeded {
		t.Fatalf("settled execution=%#v err=%v", record, err)
	}
	if completed, err := routing.RouteState.CompletedSelections(scope); err != nil || !completed[selectionID] {
		t.Fatalf("settled route completion=%#v err=%v", completed, err)
	}
	if source.calls.Load() != 1 || provider.boundCalls != 1 {
		t.Fatalf("source=%d provider calls=%d; reconciliation redispatched", source.calls.Load(), provider.boundCalls)
	}
}

func TestDynamicEffectReceiptWorkerRejectsConflictingReceipt(t *testing.T) {
	routing, scope, _, selectionID, operationID, bindingID, _ := prepareWorkerAwaitingReceiptOperation(t)
	worker, err := NewDynamicEffectReceiptWorker(routing.ReconcileDynamicEffectReceiptSource, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	source := &workerReceiptSourceStub{bindingID: bindingID, observations: []DynamicEffectReceiptObservation{{OperationID: operationID, State: DynamicEffectReceiptAccepted, Receipt: "receipt-accepted"}}}
	if err := worker.RegisterSource(source); err != nil {
		t.Fatal(err)
	}
	worker.ReconcileNow(context.Background())
	if record, err := routing.ExecutionStore.Execution(scope, selectionID); err != nil || record.State != coretool.PlanExecutionSucceeded {
		t.Fatalf("settled execution=%#v err=%v", record, err)
	}
	// A later observation carrying a different receipt digest for the same
	// operation is rejected by the append-once receipt store. The worker
	// isolates the failure: the settled success is neither reopened nor
	// overwritten.
	source.observations[0].Receipt = "conflicting-receipt"
	worker.ReconcileNow(context.Background())
	if record, err := routing.ExecutionStore.Execution(scope, selectionID); err != nil || record.State != coretool.PlanExecutionSucceeded {
		t.Fatalf("execution after conflicting receipt=%#v err=%v", record, err)
	}
}

func TestDynamicEffectReceiptWorkerNeverPromotesFailedObservation(t *testing.T) {
	routing, scope, _, selectionID, operationID, bindingID, _ := prepareWorkerAwaitingReceiptOperation(t)
	worker, err := NewDynamicEffectReceiptWorker(routing.ReconcileDynamicEffectReceiptSource, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	failing := &workerReceiptSourceStub{bindingID: bindingID, err: errors.New("receipt endpoint unavailable")}
	if err := worker.RegisterSource(failing); err != nil {
		t.Fatal(err)
	}
	worker.ReconcileNow(context.Background())
	if record, err := routing.ExecutionStore.Execution(scope, selectionID); err != nil || record.State != coretool.PlanExecutionAwaitingReceipt {
		t.Fatalf("execution after source error=%#v err=%v", record, err)
	}
	// An observation without an operation identity is invalid evidence: the
	// settlement path rejects it and the selection stays awaiting_receipt.
	failing.err = nil
	failing.observations = []DynamicEffectReceiptObservation{{OperationID: "  ", State: DynamicEffectReceiptAccepted, Receipt: "receipt"}}
	worker.ReconcileNow(context.Background())
	if record, err := routing.ExecutionStore.Execution(scope, selectionID); err != nil || record.State != coretool.PlanExecutionAwaitingReceipt {
		t.Fatalf("execution after empty operation id=%#v err=%v", record, err)
	}
	// A valid late receipt still settles the same operation afterwards.
	failing.observations = []DynamicEffectReceiptObservation{{OperationID: operationID, State: DynamicEffectReceiptAccepted, Receipt: "receipt-accepted"}}
	worker.ReconcileNow(context.Background())
	if record, err := routing.ExecutionStore.Execution(scope, selectionID); err != nil || record.State != coretool.PlanExecutionSucceeded {
		t.Fatalf("execution after valid receipt=%#v err=%v", record, err)
	}
}

func TestDynamicEffectReceiptWorkerIsolatesSourceFailure(t *testing.T) {
	routing, scope, _, selectionID, operationID, bindingID, _ := prepareWorkerAwaitingReceiptOperation(t)
	var logCount atomic.Int32
	worker, err := NewDynamicEffectReceiptWorker(routing.ReconcileDynamicEffectReceiptSource, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	worker.Logf = func(string, ...interface{}) { logCount.Add(1) }
	failing := &workerReceiptSourceStub{bindingID: "aaa:failing", err: errors.New("observe failed")}
	settling := &workerReceiptSourceStub{bindingID: bindingID, observations: []DynamicEffectReceiptObservation{{OperationID: operationID, State: DynamicEffectReceiptAccepted, Receipt: "receipt-accepted"}}}
	if err := worker.RegisterSource(failing); err != nil {
		t.Fatal(err)
	}
	if err := worker.RegisterSource(settling); err != nil {
		t.Fatal(err)
	}
	worker.ReconcileNow(context.Background())
	if failing.calls.Load() != 1 || settling.calls.Load() != 1 {
		t.Fatalf("source calls failing=%d settling=%d; failure blocked another binding", failing.calls.Load(), settling.calls.Load())
	}
	if logCount.Load() != 1 {
		t.Fatalf("expected exactly one logged source failure, got %d", logCount.Load())
	}
	if record, err := routing.ExecutionStore.Execution(scope, selectionID); err != nil || record.State != coretool.PlanExecutionSucceeded {
		t.Fatalf("settled execution=%#v err=%v", record, err)
	}
}

func TestDynamicEffectReceiptWorkerLifecycleAndRegistration(t *testing.T) {
	routing, scope, _, selectionID, operationID, bindingID, _ := prepareWorkerAwaitingReceiptOperation(t)
	worker, err := NewDynamicEffectReceiptWorker(routing.ReconcileDynamicEffectReceiptSource, 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewDynamicEffectReceiptWorker(nil, time.Second); err == nil {
		t.Fatal("worker without a reconciler was accepted")
	}
	first := &workerReceiptSourceStub{bindingID: bindingID}
	replacement := &workerReceiptSourceStub{bindingID: bindingID, observations: []DynamicEffectReceiptObservation{{OperationID: operationID, State: DynamicEffectReceiptAccepted, Receipt: "receipt-accepted"}}}
	if err := worker.RegisterSource(nil); err == nil {
		t.Fatal("nil source was accepted")
	}
	if err := worker.RegisterSource(&workerReceiptSourceStub{bindingID: " "}); err == nil {
		t.Fatal("source without a binding was accepted")
	}
	if err := worker.RegisterSource(first); err != nil {
		t.Fatal(err)
	}
	// Registration deduplicates by immutable binding ID: a re-registered
	// binding replaces its source instead of doubling the observation loop.
	if err := worker.RegisterSource(replacement); err != nil {
		t.Fatal(err)
	}
	if worker.SourceCount() != 1 {
		t.Fatalf("source count=%d after re-registration", worker.SourceCount())
	}
	if err := worker.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := worker.Start(context.Background()); err == nil {
		t.Fatal("second start was accepted")
	}
	deadline := time.Now().Add(5 * time.Second)
	for replacement.calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if first.calls.Load() != 0 || replacement.calls.Load() == 0 {
		t.Fatalf("replaced source observed first=%d replacement=%d", first.calls.Load(), replacement.calls.Load())
	}
	worker.Stop()
	callsAfterStop := replacement.calls.Load()
	time.Sleep(50 * time.Millisecond)
	if replacement.calls.Load() != callsAfterStop {
		t.Fatalf("worker reconciled after stop: %d -> %d", callsAfterStop, replacement.calls.Load())
	}
	worker.Stop() // idempotent
	if record, err := routing.ExecutionStore.Execution(scope, selectionID); err != nil || record.State != coretool.PlanExecutionSucceeded {
		t.Fatalf("settled execution=%#v err=%v", record, err)
	}
	worker.UnregisterSource(bindingID)
	if worker.SourceCount() != 0 {
		t.Fatalf("source count=%d after unregister", worker.SourceCount())
	}
}

func TestDynamicEffectReceiptWorkerStopsOnContextCancel(t *testing.T) {
	routing, scope, _, selectionID, _, bindingID, _ := prepareWorkerAwaitingReceiptOperation(t)
	worker, err := NewDynamicEffectReceiptWorker(routing.ReconcileDynamicEffectReceiptSource, 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	source := &countingCancelAwareSource{bindingID: bindingID, calls: &calls}
	if err := worker.RegisterSource(source); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := worker.Start(ctx); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	worker.Stop()
	observed := calls.Load()
	time.Sleep(50 * time.Millisecond)
	if calls.Load() != observed {
		t.Fatalf("worker reconciled after parent cancel: %d -> %d", observed, calls.Load())
	}
	// Cancellation never settles: the prepared operation is still awaiting a
	// trusted receipt.
	if record, err := routing.ExecutionStore.Execution(scope, selectionID); err != nil || record.State != coretool.PlanExecutionAwaitingReceipt {
		t.Fatalf("execution after cancel=%#v err=%v", record, err)
	}
}

// countingCancelAwareSource blocks in observation until the context ends, so
// the test can prove cancellation unwinds an in-flight sweep.
type countingCancelAwareSource struct {
	bindingID string
	calls     *atomic.Int32
}

func (s *countingCancelAwareSource) BindingID() string { return s.bindingID }

func (s *countingCancelAwareSource) ObserveDynamicEffectReceipts(ctx context.Context, _ func(DynamicEffectReceiptObservation) error) error {
	s.calls.Add(1)
	<-ctx.Done()
	return ctx.Err()
}
