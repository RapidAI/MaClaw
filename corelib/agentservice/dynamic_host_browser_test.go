package agentservice

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

type fakeHostBrowserController struct {
	action    string
	url       string
	principal Principal
	result    string
	err       error
}

func (f *fakeHostBrowserController) ControlReviewedHostBrowser(_ context.Context, principal Principal, action, url string) (string, error) {
	f.principal = principal
	f.action = action
	f.url = url
	return f.result, f.err
}

func TestReviewedHostBrowserExecutesWithoutCoordinatorAndRejectsCookie(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	controller := &fakeHostBrowserController{result: "title Example"}
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{Browser: controller})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "browser", Capability: CapabilityBrowserControl, Required: true}},
	})
	if err != nil || len(plan.Selections) != 1 || len(plan.Unmet) != 0 {
		t.Fatalf("browser plan=%#v err=%v", plan, err)
	}
	if plan.Selections[0].AdapterName == "browser" || plan.Selections[0].Provider.Kind != reviewedHostProviderKind {
		t.Fatalf("selection=%#v", plan.Selections[0])
	}
	if dynamicHostLocalMutationSelection(plan.Selections[0]) || dynamicSelectionRequiresReceipt(plan.Selections[0]) || !dynamicHostObservedExternalSelection(plan.Selections[0]) {
		t.Fatalf("host browser is observed external, selection=%#v", plan.Selections[0])
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	result := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"action":"navigate","url":"https://example.com"}`)
	if !result.Succeeded || result.Unknown || result.Result != controller.result {
		t.Fatalf("browser result=%#v", result)
	}
	if controller.action != "navigate" || controller.url != "https://example.com" {
		t.Fatalf("controller=%#v", controller)
	}
	cookie := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"action":"navigate","url":"https://example.com","cookie":"sid=1"}`)
	if cookie.Succeeded || cookie.Unknown {
		t.Fatalf("cookie field must fail closed, result=%#v", cookie)
	}
	cookieURL := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"action":"navigate","url":"https://example.com/?cookie=1"}`)
	if cookieURL.Succeeded || cookieURL.Unknown {
		t.Fatalf("cookie in url must fail closed, result=%#v", cookieURL)
	}
}

func TestReviewedHostBrowserDisconnectIsUnknown(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	cb := &coreAgentCallbacks{
		principal: principal,
		trustedBrowser: func(_ context.Context, _ Principal, _, _ string) (string, error) {
			return "", fmt.Errorf("host_browser_session_disconnected")
		},
	}
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{Browser: cb})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "browser", Capability: CapabilityBrowserControl, Required: true}},
	})
	if err != nil || len(plan.Selections) != 1 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	disconnected := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"action":"snapshot"}`)
	if !disconnected.Unknown || disconnected.Succeeded || disconnected.ReasonCode != "host_browser_session_disconnected" {
		t.Fatalf("disconnect must be unknown, result=%#v", disconnected)
	}
}

// A driver may already know that a navigation was dispatched before its answer
// was lost, and say so by name. Hub's vocabulary has to carry that verdict:
// none of its session/timeout names match an unobserved one, so an unrecognised
// name would fall through to a definite failure and invite the model to repeat
// a navigation that may already have taken effect.
func TestReviewedHostBrowserUnobservedOutcomeIsUnknownNotAFailure(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	cb := &coreAgentCallbacks{
		principal: principal,
		trustedBrowser: func(_ context.Context, _ Principal, _, _ string) (string, error) {
			return "", fmt.Errorf("trusted_browser_outcome_unobserved")
		},
	}
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{Browser: cb})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "browser", Capability: CapabilityBrowserControl, Required: true}},
	})
	if err != nil || len(plan.Selections) != 1 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	lost := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"action":"navigate","url":"https://example.com"}`)
	if lost.Succeeded {
		t.Fatalf("a lost answer must not read as success, result=%#v", lost)
	}
	if !lost.Unknown {
		t.Fatalf("dispatched-then-lost navigation must be unknown, result=%#v", lost)
	}
	if lost.ReasonCode != "host_browser_outcome_unobserved" {
		t.Fatalf("verdict must keep its own name, result=%#v", lost)
	}
}

func TestReviewedHostBrowserIsAbsentWithoutDriver(t *testing.T) {
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
		Needs: []coretool.CapabilityNeed{{ID: "browser", Capability: CapabilityBrowserControl, Required: true}},
	})
	if err != nil || len(plan.Selections) != 0 {
		t.Fatalf("browser without driver must stay unmet, plan=%#v err=%v", plan, err)
	}
	empty := (&coreAgentCallbacks{principal: Principal{TenantID: "t", UserID: "u"}}).reviewedHostOwnedServices()
	if empty.Browser != nil {
		t.Fatal("browser without hook must stay unpublished")
	}
}

// TestReviewedHostBrowserActionEnumMatchesWhatTheHostPerforms pins the same
// defect the GUI surface hit on 2026-10-10: the action was an unlabelled
// string, so a model guessed "open" and was refused without ever learning the
// name that would have worked. The enum is documentation the canonicalizer
// ignores, so this holds it against the execution check -- every advertised
// name must be accepted, and a guessed one must still be refused.
func TestReviewedHostBrowserActionEnumMatchesWhatTheHostPerforms(t *testing.T) {
	schema := reviewedHostBrowserInvocationSchema()
	properties, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("browser schema has no properties: %#v", schema)
	}
	action, ok := properties["action"].(map[string]interface{})
	if !ok {
		t.Fatalf("browser schema has no action property: %#v", properties)
	}
	enum, ok := action["enum"].([]string)
	if !ok || len(enum) == 0 {
		t.Fatalf("action must publish a non-empty enum, got %#v", action["enum"])
	}
	// The set and the url pairing both come from the shared contract, so this
	// host cannot drift from the GUI surface serving the same capability.
	for _, name := range enum {
		if !coretool.BrowserControlActionAllowed(name) {
			t.Fatalf("schema advertises action %q but the execution check refuses it", name)
		}
	}
	for _, name := range coretool.BrowserControlActions() {
		advertised := false
		for _, candidate := range enum {
			if candidate == name {
				advertised = true
				break
			}
		}
		if !advertised {
			t.Fatalf("execution check accepts action %q but the schema never shows it", name)
		}
	}
	for _, guessed := range []string{"open", "goto", "visit"} {
		args := map[string]interface{}{"action": guessed, "url": "https://example.com"}
		_, _, err := reviewedHostBrowserArgsAllowed(args)
		if err == nil || !strings.Contains(err.Error(), "host_browser_action_rejected") {
			t.Fatalf("action %q = %v, want host_browser_action_rejected", guessed, err)
		}
		// The refusal must name the real vocabulary so the first failure can
		// redirect the turn instead of ending it.
		for _, valid := range coretool.BrowserControlActions() {
			if !strings.Contains(err.Error(), valid) {
				t.Fatalf("refusal for %q = %v does not mention the valid name %q", guessed, err, valid)
			}
		}
	}
	for _, name := range enum {
		if !coretool.BrowserControlNeedsURL(name) {
			continue
		}
		if _, _, err := reviewedHostBrowserArgsAllowed(map[string]interface{}{"action": name}); err == nil ||
			!strings.Contains(err.Error(), "host_browser_url_required") {
			t.Fatalf("%s without url = %v, want host_browser_url_required", name, err)
		}
	}
}

func TestProjectReviewedHostBrowserRejectsCookieFields(t *testing.T) {
	provider, definition, _, err := ProjectReviewedHostBrowserProvider(&fakeHostBrowserController{})
	if err != nil {
		t.Fatal(err)
	}
	if provider.Provides[0].Capability != CapabilityBrowserControl || provider.AdapterName == "browser" {
		t.Fatalf("provider=%#v", provider)
	}
	fn, _ := definition["function"].(map[string]interface{})
	params, _ := fn["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	if _, ok := props["action"]; !ok || len(props) != 2 {
		t.Fatalf("browser schema=%#v", props)
	}
	for _, key := range []string{"cookie", "headers", "channel", "destination", "session_id"} {
		if _, ok := props[key]; ok {
			t.Fatalf("browser schema leaked %s", key)
		}
	}
}
