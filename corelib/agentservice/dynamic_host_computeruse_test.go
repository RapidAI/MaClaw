package agentservice

import (
	"context"
	"fmt"
	"testing"
	"time"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

type fakeHostComputerUseController struct {
	action    string
	principal Principal
	result    string
	err       error
}

func (f *fakeHostComputerUseController) ControlReviewedHostDesktop(_ context.Context, principal Principal, action string) (string, error) {
	f.principal = principal
	f.action = action
	return f.result, f.err
}

func TestReviewedHostComputerUseExecutesWithoutCoordinatorAndRejectsSoup(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	controller := &fakeHostComputerUseController{result: "desktop observed"}
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{ComputerUse: controller})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "cu", Capability: CapabilityComputerUse, Required: true}},
	})
	if err != nil || len(plan.Selections) != 1 || len(plan.Unmet) != 0 {
		t.Fatalf("cu plan=%#v err=%v", plan, err)
	}
	if plan.Selections[0].AdapterName == "computer_use" || plan.Selections[0].Provider.Kind != reviewedHostProviderKind {
		t.Fatalf("selection=%#v", plan.Selections[0])
	}
	if dynamicHostLocalMutationSelection(plan.Selections[0]) || dynamicSelectionRequiresReceipt(plan.Selections[0]) || !dynamicHostObservedExternalSelection(plan.Selections[0]) {
		t.Fatalf("host CU is observed external, selection=%#v", plan.Selections[0])
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	result := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"action":"observe"}`)
	if !result.Succeeded || result.Unknown || result.Result != controller.result {
		t.Fatalf("cu result=%#v", result)
	}
	if controller.action != "observe" {
		t.Fatalf("controller=%#v", controller)
	}
	rejected := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"action":"observe","x":1,"y":2}`)
	if rejected.Succeeded || rejected.Unknown {
		t.Fatalf("click coordinates must fail closed, result=%#v", rejected)
	}
}

func TestReviewedHostComputerUseClickWithoutTargetFailsClosed(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	cb := &coreAgentCallbacks{
		principal: principal,
		trustedComputerUse: func(_ context.Context, _ Principal, _ string) (string, error) {
			return "should not run", nil
		},
	}
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{ComputerUse: cb})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "cu", Capability: CapabilityComputerUse, Required: true}},
	})
	if err != nil || len(plan.Selections) != 1 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	clicked := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"action":"click"}`)
	if clicked.Succeeded || clicked.Unknown || clicked.ReasonCode != "host_computer_use_click_target_missing" {
		t.Fatalf("click without target must fail closed, result=%#v", clicked)
	}
}

func TestReviewedHostComputerUseUnavailableIsUnknown(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	cb := &coreAgentCallbacks{
		principal: principal,
		trustedComputerUse: func(_ context.Context, _ Principal, _ string) (string, error) {
			return "", fmt.Errorf("host_computer_use_runtime_unavailable")
		},
	}
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{ComputerUse: cb})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "cu", Capability: CapabilityComputerUse, Required: true}},
	})
	if err != nil || len(plan.Selections) != 1 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	unknown := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"action":"observe"}`)
	if !unknown.Unknown || unknown.Succeeded || unknown.ReasonCode != "host_computer_use_runtime_unavailable" {
		t.Fatalf("missing runtime after publish must be unknown, result=%#v", unknown)
	}
}

func TestReviewedHostComputerUseIsAbsentWithoutRuntime(t *testing.T) {
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
		Needs: []coretool.CapabilityNeed{{ID: "cu", Capability: CapabilityComputerUse, Required: true}},
	})
	if err != nil || len(plan.Selections) != 0 {
		t.Fatalf("CU without runtime must stay unmet, plan=%#v err=%v", plan, err)
	}
	empty := (&coreAgentCallbacks{principal: Principal{TenantID: "t", UserID: "u"}}).reviewedHostOwnedServices()
	if empty.ComputerUse != nil {
		t.Fatal("CU without hook must stay unpublished")
	}
}

func TestProjectReviewedHostComputerUseRejectsTargetFields(t *testing.T) {
	provider, definition, _, err := ProjectReviewedHostComputerUseProvider(&fakeHostComputerUseController{})
	if err != nil {
		t.Fatal(err)
	}
	if provider.Provides[0].Capability != CapabilityComputerUse || provider.AdapterName == "computer_use" {
		t.Fatalf("provider=%#v", provider)
	}
	fn, _ := definition["function"].(map[string]interface{})
	params, _ := fn["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	if _, ok := props["action"]; !ok || len(props) != 1 {
		t.Fatalf("cu schema=%#v", props)
	}
	for _, key := range []string{"x", "y", "ref", "channel", "destination", "session_id"} {
		if _, ok := props[key]; ok {
			t.Fatalf("cu schema leaked %s", key)
		}
	}
}
