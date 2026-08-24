package agentservice

import (
	"context"
	"strings"
	"testing"
	"time"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

type fakeHostSessionInspector struct {
	id        string
	principal Principal
	result    string
	err       error
}

func (f *fakeHostSessionInspector) InspectReviewedHostSessions(_ context.Context, principal Principal, id string) (string, error) {
	f.principal = principal
	f.id = id
	return f.result, f.err
}

func TestReviewedHostSessionExecutesWithoutCoordinatorAndRejectsDrive(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	inspector := &fakeHostSessionInspector{result: "s1 [running]"}
	observed := dynamicCatalogLifecycleForKind("mcp", IncompleteDynamicCatalogLifecycle(coretool.CatalogCoverageReasonNotReady))
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, observed, reviewedHostOwnedServices{Session: inspector})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "session", Capability: CapabilitySessionManage, Required: true}},
	})
	if err != nil || len(plan.Selections) != 1 || len(plan.Unmet) != 0 {
		t.Fatalf("session plan=%#v err=%v", plan, err)
	}
	if plan.Selections[0].Provider.Kind != reviewedHostProviderKind || plan.Selections[0].FitProof.MatchedCapability != CapabilitySessionManage {
		t.Fatalf("selection=%#v", plan.Selections[0])
	}
	if !dynamicHostLocalMutationSelection(plan.Selections[0]) || dynamicSelectionRequiresReceipt(plan.Selections[0]) {
		t.Fatalf("host session inspect must use the local mutation receipt, selection=%#v", plan.Selections[0])
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	listed := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{}`)
	if !listed.Succeeded || inspector.id != "" {
		t.Fatalf("list result=%#v inspector=%#v", listed, inspector)
	}
	got := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"id":"s1"}`)
	if !got.Succeeded || inspector.id != "s1" {
		t.Fatalf("get result=%#v inspector=%#v", got, inspector)
	}
	interrupt := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"id":"s1","interrupt":true}`)
	if interrupt.Succeeded || interrupt.Unknown {
		t.Fatalf("interrupt must fail closed, result=%#v", interrupt)
	}
	input := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"id":"s1","input":"continue"}`)
	if input.Succeeded || input.Unknown {
		t.Fatalf("send_input must fail closed, result=%#v", input)
	}
	actionSoup := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"action":"kill"}`)
	if actionSoup.Succeeded || actionSoup.Unknown {
		t.Fatalf("action soup must fail closed, result=%#v", actionSoup)
	}

	templatePlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-template", TurnID: "turn-template", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "template", Capability: CapabilityTemplateManage, Required: true}},
	})
	if err != nil || len(templatePlan.Selections) != 0 {
		t.Fatalf("template.manage.session must not be satisfied by host session, plan=%#v err=%v", templatePlan, err)
	}
	delegatePlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-delegate", TurnID: "turn-delegate", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "delegate", Capability: coretool.CapabilityAgentDelegateSubtask, Required: true}},
	})
	if err != nil || len(delegatePlan.Selections) != 0 {
		t.Fatalf("agent.delegate.subtask must not be satisfied by host session, plan=%#v err=%v", delegatePlan, err)
	}
}

func TestProjectReviewedHostSessionRejectsDriveAndGUIFields(t *testing.T) {
	provider, definition, _, err := ProjectReviewedHostSessionProvider(&fakeHostSessionInspector{})
	if err != nil {
		t.Fatal(err)
	}
	if provider.Provides[0].Capability != CapabilitySessionManage || provider.AdapterName == "list_sessions" || provider.AdapterName == "interrupt_session" {
		t.Fatalf("provider=%#v", provider)
	}
	fn, _ := definition["function"].(map[string]interface{})
	params, _ := fn["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	if _, ok := props["id"]; !ok || len(props) != 1 {
		t.Fatalf("session schema=%#v", props)
	}
	for _, key := range []string{"action", "input", "interrupt", "kill", "send", "launch", "provider", "project", "yolo_mode", "channel", "destination"} {
		if _, ok := props[key]; ok {
			t.Fatalf("session schema leaked %s", key)
		}
	}
}

func TestReviewedHostSessionInspectsWithoutDrive(t *testing.T) {
	principal := Principal{TenantID: "tenant", UserID: "user"}
	other := Principal{TenantID: "tenant", UserID: "other"}
	cb := &coreAgentCallbacks{principal: principal}
	listed, err := cb.InspectReviewedHostSessions(context.Background(), principal, "")
	if err != nil || !strings.Contains(listed, "没有编码会话") {
		t.Fatalf("list=%q err=%v", listed, err)
	}
	if _, err := cb.InspectReviewedHostSessions(context.Background(), principal, "s1"); err == nil {
		t.Fatal("unknown session id must fail closed")
	}
	if _, err := cb.InspectReviewedHostSessions(context.Background(), other, ""); err == nil {
		t.Fatal("principal mismatch must fail closed")
	}
	services := cb.reviewedHostOwnedServices()
	if services.Session == nil {
		t.Fatal("host session inspect must attach even when no GUI session manager exists")
	}
}
