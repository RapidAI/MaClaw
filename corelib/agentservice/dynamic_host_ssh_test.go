package agentservice

import (
	"context"
	"fmt"
	"testing"
	"time"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

type fakeHostSSHExecutor struct {
	command   string
	principal Principal
	result    string
	err       error
}

func (f *fakeHostSSHExecutor) ExecuteReviewedHostSSH(_ context.Context, principal Principal, command string) (string, error) {
	f.principal = principal
	f.command = command
	return f.result, f.err
}

func TestReviewedHostSSHExecutesWithoutCoordinatorAndRejectsSoup(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	executor := &fakeHostSSHExecutor{result: "Linux 6.1"}
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{SSH: executor})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "ssh", Capability: CapabilitySSHExecute, Required: true}},
	})
	if err != nil || len(plan.Selections) != 1 || len(plan.Unmet) != 0 {
		t.Fatalf("ssh plan=%#v err=%v", plan, err)
	}
	if plan.Selections[0].AdapterName == "ssh" || plan.Selections[0].Provider.Kind != reviewedHostProviderKind {
		t.Fatalf("selection=%#v", plan.Selections[0])
	}
	if dynamicHostLocalMutationSelection(plan.Selections[0]) || dynamicSelectionRequiresReceipt(plan.Selections[0]) {
		t.Fatalf("host ssh is observed external, not an IM send, selection=%#v", plan.Selections[0])
	}
	if !dynamicHostObservedExternalSelection(plan.Selections[0]) {
		t.Fatalf("host ssh must be observed-external, selection=%#v", plan.Selections[0])
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	result := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"command":"uname -r"}`)
	if !result.Succeeded || result.Unknown || result.Result != executor.result {
		t.Fatalf("ssh result=%#v", result)
	}
	if executor.command != "uname -r" || executor.principal.UserID != principal.UserID {
		t.Fatalf("executor=%#v", executor)
	}
	rejected := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"command":"uname","host":"10.0.0.1","session_id":"s1"}`)
	if rejected.Succeeded || rejected.Unknown {
		t.Fatalf("ssh soup fields must fail closed, result=%#v", rejected)
	}
}

func TestReviewedHostSSHTimeoutAndDisconnectAreUnknown(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	cb := &coreAgentCallbacks{
		principal: principal,
		trustedSSH: func(_ context.Context, _ Principal, _ string) (string, error) {
			return "", fmt.Errorf("host_ssh_session_disconnected")
		},
	}
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{SSH: cb})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-disc", TurnID: "turn-disc", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "ssh", Capability: CapabilitySSHExecute, Required: true}},
	})
	if err != nil || len(plan.Selections) != 1 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	disconnected := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"command":"uname"}`)
	if !disconnected.Unknown || disconnected.Succeeded || disconnected.ReasonCode != "host_ssh_session_disconnected" {
		t.Fatalf("disconnect must be unknown, result=%#v", disconnected)
	}

	timeoutCB := &coreAgentCallbacks{
		principal: principal,
		trustedSSH: func(ctx context.Context, _ Principal, _ string) (string, error) {
			<-ctx.Done()
			return "", ctx.Err()
		},
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	timeoutCatalog, _, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{SSH: timeoutCB})
	if err != nil {
		t.Fatal(err)
	}
	timed := timeoutCatalog.ExecuteSelection(cancelled, principal, nil, nil, plan.Selections[0], `{"command":"uname"}`)
	if !timed.Unknown || timed.Succeeded || timed.ReasonCode != "host_ssh_timeout" {
		t.Fatalf("timeout must be unknown, result=%#v", timed)
	}
}

// A driver that separates "the command was written and then the session died"
// from "the session never carried it" says so by name. Hub's own vocabulary
// has no rung matching an unobserved name unless one is kept for it, and
// without that rung the verdict would fall through to a definite failure --
// telling the model to run again a command that may already have run.
func TestReviewedHostSSHUnobservedOutcomeIsUnknownNotAFailure(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	cb := &coreAgentCallbacks{
		principal: principal,
		trustedSSH: func(_ context.Context, _ Principal, _ string) (string, error) {
			return "", fmt.Errorf("trusted_ssh_outcome_unobserved")
		},
	}
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{SSH: cb})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-unobserved", TurnID: "turn-unobserved", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "ssh", Capability: CapabilitySSHExecute, Required: true}},
	})
	if err != nil || len(plan.Selections) != 1 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	lost := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"command":"uname"}`)
	if lost.Succeeded {
		t.Fatalf("a lost answer must not read as success, result=%#v", lost)
	}
	if !lost.Unknown {
		t.Fatalf("dispatched-then-lost command must be unknown, result=%#v", lost)
	}
	if lost.ReasonCode != "host_ssh_outcome_unobserved" {
		t.Fatalf("verdict must keep its own name, result=%#v", lost)
	}
}

func TestReviewedHostSSHIsAbsentWithoutSession(t *testing.T) {
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
		Needs: []coretool.CapabilityNeed{{ID: "ssh", Capability: CapabilitySSHExecute, Required: true}},
	})
	if err != nil || len(plan.Selections) != 0 {
		t.Fatalf("ssh without session must stay unmet, plan=%#v err=%v", plan, err)
	}
	child := (&coreAgentCallbacks{
		principal: Principal{TenantID: "t", UserID: "u"},
		trustedSSH: func(context.Context, Principal, string) (string, error) {
			return "secret", nil
		},
		delegateChild: true,
	}).reviewedHostOwnedServices()
	if child.SSH != nil {
		t.Fatal("delegate child must not republish ssh")
	}
	empty := (&coreAgentCallbacks{principal: Principal{TenantID: "t", UserID: "u"}}).reviewedHostOwnedServices()
	if empty.SSH != nil {
		t.Fatal("ssh without hook or live session must stay unpublished")
	}
}

func TestProjectReviewedHostSSHRejectsHostFields(t *testing.T) {
	provider, definition, _, err := ProjectReviewedHostSSHProvider(&fakeHostSSHExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	if provider.Provides[0].Capability != CapabilitySSHExecute || provider.AdapterName == "ssh" {
		t.Fatalf("provider=%#v", provider)
	}
	fn, _ := definition["function"].(map[string]interface{})
	params, _ := fn["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	if _, ok := props["command"]; !ok || len(props) != 1 {
		t.Fatalf("ssh schema=%#v", props)
	}
	for _, key := range []string{"host", "label", "session_id", "user", "channel", "destination", "cookie"} {
		if _, ok := props[key]; ok {
			t.Fatalf("ssh schema leaked %s", key)
		}
	}
}
