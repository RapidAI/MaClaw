package agentservice

import (
	"context"
	"testing"
	"time"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

type fakeHostDelegateRunner struct {
	task      string
	principal Principal
	result    string
	err       error
	delay     time.Duration
}

func (f *fakeHostDelegateRunner) RunReviewedHostDelegate(ctx context.Context, principal Principal, task string) (string, error) {
	f.principal = principal
	f.task = task
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return f.result, f.err
}

func TestReviewedHostDelegateExecutesWithoutCoordinatorAndRejectsSoup(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeHostDelegateRunner{result: "child completed: summarize"}
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{Delegate: runner})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "delegate", Capability: CapabilityDelegateSubtask, Required: true}},
	})
	if err != nil || len(plan.Selections) != 1 || len(plan.Unmet) != 0 {
		t.Fatalf("delegate plan=%#v err=%v", plan, err)
	}
	if plan.Selections[0].AdapterName == "delegate_task" || plan.Selections[0].Provider.Kind != reviewedHostProviderKind {
		t.Fatalf("selection=%#v", plan.Selections[0])
	}
	if !dynamicHostLocalMutationSelection(plan.Selections[0]) || dynamicSelectionRequiresReceipt(plan.Selections[0]) {
		t.Fatalf("host delegate wait is a local receipt, selection=%#v", plan.Selections[0])
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	result := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"task":"summarize"}`)
	if !result.Succeeded || result.Unknown || result.Result != runner.result {
		t.Fatalf("delegate result=%#v", result)
	}
	if runner.task != "summarize" || runner.principal.UserID != principal.UserID {
		t.Fatalf("runner=%#v", runner)
	}
	rejected := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"task":"summarize","delegate_to":"coder"}`)
	if rejected.Succeeded || rejected.Unknown {
		t.Fatalf("delegate_to must fail closed, result=%#v", rejected)
	}
}

func TestReviewedHostDelegateTimeoutAndStartedAreUnknown(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	started := &fakeHostDelegateRunner{result: "child started"}
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{Delegate: started})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-started", TurnID: "turn-started", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "delegate", Capability: CapabilityDelegateSubtask, Required: true}},
	})
	if err != nil || len(plan.Selections) != 1 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	cb := &coreAgentCallbacks{
		principal: principal,
		delegateSubtask: func(_ context.Context, _ Principal, task string) (string, error) {
			return "child started", nil
		},
	}
	startedCatalog, _, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{Delegate: cb})
	if err != nil {
		t.Fatal(err)
	}
	unknown := startedCatalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"task":"summarize"}`)
	if !unknown.Unknown || unknown.Succeeded || unknown.ReasonCode != "host_delegate_started_is_not_complete" {
		t.Fatalf("started must be unknown, result=%#v", unknown)
	}

	timeoutCB := &coreAgentCallbacks{
		principal: principal,
		delegateSubtask: func(ctx context.Context, _ Principal, _ string) (string, error) {
			<-ctx.Done()
			return "", ctx.Err()
		},
	}
	// Keep the wait short by shrinking the helper timeout via a pre-cancelled context.
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	timeoutCatalog, _, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{Delegate: timeoutCB})
	if err != nil {
		t.Fatal(err)
	}
	timed := timeoutCatalog.ExecuteSelection(cancelled, principal, nil, nil, plan.Selections[0], `{"task":"summarize"}`)
	if !timed.Unknown || timed.Succeeded || timed.ReasonCode != "host_delegate_timeout" {
		t.Fatalf("timeout must be unknown, result=%#v", timed)
	}
}

func TestReviewedHostDelegateIsAbsentWithoutRunner(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "delegate", Capability: CapabilityDelegateSubtask, Required: true}},
	})
	if err != nil || len(plan.Selections) != 0 {
		t.Fatalf("delegate without runner must stay unmet, plan=%#v err=%v", plan, err)
	}
	child := (&coreAgentCallbacks{
		principal: Principal{TenantID: "t", UserID: "u"},
		delegateSubtask: func(context.Context, Principal, string) (string, error) {
			return "child completed: nested", nil
		},
		delegateChild: true,
	}).reviewedHostOwnedServices()
	if child.Delegate != nil {
		t.Fatal("delegate child must not republish delegate")
	}
}

func TestProjectReviewedHostDelegateRejectsDelegateTo(t *testing.T) {
	provider, definition, _, err := ProjectReviewedHostDelegateProvider(&fakeHostDelegateRunner{})
	if err != nil {
		t.Fatal(err)
	}
	if provider.Provides[0].Capability != CapabilityDelegateSubtask || provider.AdapterName == "delegate_task" {
		t.Fatalf("provider=%#v", provider)
	}
	fn, _ := definition["function"].(map[string]interface{})
	params, _ := fn["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	if _, ok := props["task"]; !ok || len(props) != 1 {
		t.Fatalf("delegate schema=%#v", props)
	}
	for _, key := range []string{"delegate_to", "role", "channel", "destination", "group_name"} {
		if _, ok := props[key]; ok {
			t.Fatalf("delegate schema leaked %s", key)
		}
	}
}

func TestReviewedHostDelegateChildMetadata(t *testing.T) {
	if reviewedHostDelegateChild(nil) || reviewedHostDelegateChild(map[string]string{"channel": "lansenger"}) {
		t.Fatal("missing flag must not mark a child")
	}
	if !reviewedHostDelegateChild(map[string]string{reviewedHostDelegateChildKey: "1"}) {
		t.Fatal("delegate_child=1 must mark a child")
	}
}
