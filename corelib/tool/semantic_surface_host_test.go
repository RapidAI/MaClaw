package tool

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestNewSurfacePublishRequestFillsDefaultTTL(t *testing.T) {
	got := NewSurfacePublishRequest(RouteRevisionPublishRequest{}, "tenant", nil, 0, time.Time{})
	if got.GrantTTL != DefaultInvocationGrantTTL {
		t.Fatalf("GrantTTL=%s want %s", got.GrantTTL, DefaultInvocationGrantTTL)
	}
	if got.TenantID != "tenant" || got.Now.IsZero() {
		t.Fatalf("request=%+v", got)
	}
	got = NewSurfacePublishRequest(RouteRevisionPublishRequest{}, "tenant", nil, time.Minute, time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC))
	if got.GrantTTL != time.Minute {
		t.Fatalf("explicit TTL=%s", got.GrantTTL)
	}
}

func TestPublishCurrentSurfaceFallsBackToRouteState(t *testing.T) {
	plan, scope, _ := routeStateTestPlan(t)
	store := NewMemoryRouteStateStore()
	state, grants, err := PublishCurrentSurface(nil, store, NewSurfacePublishRequest(
		RouteRevisionPublishRequest{Scope: scope, Plan: plan, SnapshotDigest: plan.SnapshotDigest},
		"tenant", nil, time.Minute, time.Now().UTC(),
	))
	if err != nil || state.Plan.ID != plan.ID || len(grants) != 0 {
		t.Fatalf("state=%+v grants=%#v err=%v", state, grants, err)
	}
}

func TestIssueReadySurfaceRecordsMemoryMaterialization(t *testing.T) {
	registry := semanticRegistry(t)
	snapshot := semanticSnapshot(t, registry, []ProviderSpec{
		semanticProvider("capture_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly),
	})
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true}},
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	scope := InvocationScope{RootTaskID: "task", PlanID: plan.ID, SessionID: "session", TurnID: "turn", PrincipalID: "principal"}
	store := NewMemoryRouteStateStore()
	if _, err := store.PublishRevision(RouteRevisionPublishRequest{Scope: scope, Plan: plan, SnapshotDigest: plan.SnapshotDigest}, time.Now().UTC()); err != nil {
		t.Fatalf("publish: %v", err)
	}
	issuer, err := NewInvocationIssuer([]byte(strings.Repeat("s", 32)))
	if err != nil {
		t.Fatal(err)
	}
	needed := map[string]bool{plan.Selections[0].ID: true}
	grants, err := IssueReadySurface(nil, store, issuer, plan, scope, time.Minute, nil, needed, time.Now().UTC())
	if err != nil || len(grants) != 1 {
		t.Fatalf("grants=%#v err=%v", grants, err)
	}
	state, err := store.Open(scope, plan, time.Now().UTC())
	if err != nil || len(state.Materializations) != 1 || state.Materializations[0].State != RouteMaterializationExposed {
		t.Fatalf("state=%+v err=%v", state, err)
	}
}

func TestBindIssuedGrantRejectsEmptyNameAndCollision(t *testing.T) {
	table := map[string]InvocationGrant{}
	first := InvocationGrant{SelectionID: "sel-1", AdapterName: "web_search", Token: "tok-a"}
	name, err := BindIssuedGrant(first, table)
	if err != nil || name != "web_search" || table[name].SelectionID != "sel-1" {
		t.Fatalf("first bind name=%q err=%v table=%#v", name, err, table)
	}
	if _, err := BindIssuedGrant(first, table); err != nil {
		t.Fatalf("same token must be idempotent: %v", err)
	}
	if _, err := BindIssuedGrant(InvocationGrant{SelectionID: "sel-2", AdapterName: "web_search", Token: "tok-b"}, table); err == nil || !strings.Contains(err.Error(), "function-name collision") {
		t.Fatalf("collision err=%v", err)
	}
	if _, err := BindIssuedGrant(InvocationGrant{SelectionID: "sel-3"}, nil); err == nil {
		t.Fatal("nil table must fail")
	}
	if _, err := BindIssuedGrant(InvocationGrant{SelectionID: "sel-4"}, map[string]InvocationGrant{}); err == nil {
		t.Fatal("empty name must fail")
	}
}

func TestPrepareReadySurfaceCompletionRequiresStores(t *testing.T) {
	if _, _, err := PrepareReadySurfaceCompletion(nil, NewMemoryRouteStateStore(), InvocationScope{}, nil); err == nil {
		t.Fatal("nil executor must fail")
	}
}

func TestApplyReadySurfaceCompletionUnionsFactsAndDurableLoad(t *testing.T) {
	if err := ApplyReadySurfaceCompletion(nil); err == nil {
		t.Fatal("nil request must fail")
	}
	completed := map[string]bool{"memory": true}
	req := ReadySurfaceRequest{Completed: completed, TrustedFacts: map[string]bool{"fact": true}}
	if err := ApplyReadySurfaceCompletion(&req); err != nil {
		t.Fatal(err)
	}
	if !req.Satisfied["memory"] || !req.Satisfied["fact"] {
		t.Fatalf("satisfied=%#v", req.Satisfied)
	}

	plan, scope, _ := routeStateTestPlan(t)
	store := NewMemoryRouteStateStore()
	if _, err := store.PublishRevision(RouteRevisionPublishRequest{Scope: scope, Plan: plan, SnapshotDigest: plan.SnapshotDigest}, time.Now().UTC()); err != nil {
		t.Fatalf("publish: %v", err)
	}
	issuer, err := NewInvocationIssuer([]byte(strings.Repeat("s", 32)))
	if err != nil {
		t.Fatal(err)
	}
	execStore := NewMemoryPlanExecutionStore()
	executor, err := NewPlanExecutorWithRouteState(issuer, execStore, store)
	if err != nil {
		t.Fatal(err)
	}
	selectionID := plan.Selections[0].ID
	if _, acquired, err := execStore.Acquire(PlanExecutionRecord{Scope: scope, SelectionID: selectionID}); err != nil || !acquired {
		t.Fatalf("acquire acquired=%t err=%v", acquired, err)
	}
	if _, err := execStore.Complete(scope, selectionID, PlanExecutionSucceeded, "digest", "", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	host := map[string]bool{"memory": true}
	req = ReadySurfaceRequest{Executor: executor, RouteState: store, Scope: scope, Completed: host, TrustedFacts: map[string]bool{"fact": true}}
	if err := ApplyReadySurfaceCompletion(&req); err != nil {
		t.Fatal(err)
	}
	if !host["memory"] || !host[selectionID] || !req.Satisfied["fact"] || !req.Satisfied[selectionID] {
		t.Fatalf("host=%#v satisfied=%#v", host, req.Satisfied)
	}
}

func TestMergeCompletedSelectionsUnionsProjectedIDs(t *testing.T) {
	got := MergeCompletedSelections(map[string]bool{"a": true}, map[string]bool{"b": true, "c": false})
	if !got["a"] || !got["b"] || got["c"] {
		t.Fatalf("merged=%#v", got)
	}
	got = MergeCompletedSelections(nil, map[string]bool{"x": true})
	if !got["x"] {
		t.Fatalf("nil base merged=%#v", got)
	}
}

func TestUnionSatisfiedIDsDoesNotMutateInputs(t *testing.T) {
	left := map[string]bool{"a": true, "skip": false}
	right := map[string]bool{"b": true}
	got := UnionSatisfiedIDs(left, right)
	if !got["a"] || !got["b"] || got["skip"] {
		t.Fatalf("union=%#v", got)
	}
	if left["b"] {
		t.Fatal("union must not mutate inputs")
	}
}

func TestVisibleReadyGrantsSkipsCompletedAndUngranted(t *testing.T) {
	ready := []PlannedSelection{{ID: "done"}, {ID: "live"}, {ID: "ungranted"}}
	ids, grants := VisibleReadyGrants(ready, map[string]bool{"done": true}, map[string]InvocationGrant{
		"fn": {SelectionID: "live", Token: "tok"},
	})
	if !ids["live"] || ids["done"] || ids["ungranted"] || len(grants) != 1 || grants[0].Token != "tok" {
		t.Fatalf("ids=%#v grants=%#v", ids, grants)
	}
}

func TestFilterSelectionIDsAndUnrenderedReadyGrants(t *testing.T) {
	plan := ToolPlan{Selections: []PlannedSelection{{ID: "keep"}, {ID: "skip-me"}}}
	ids := FilterSelectionIDs(map[string]bool{"keep": true, "skip-me": true}, plan, func(sel PlannedSelection) bool {
		return sel.ID == "skip-me"
	})
	if !ids["keep"] || ids["skip-me"] {
		t.Fatalf("filtered=%#v", ids)
	}
	ready := []PlannedSelection{{ID: "live"}, {ID: "already"}}
	grants := map[string]InvocationGrant{
		"web_search": {SelectionID: "live", AdapterName: "web_search", Token: "tok-live"},
		"web_fetch":  {SelectionID: "already", AdapterName: "web_fetch", Token: "tok-old"},
	}
	visible, out := UnrenderedReadyGrants(ready, nil, grants, map[string]bool{"web_fetch": true})
	if !visible["live"] || visible["already"] || len(out) != 1 || out[0].SelectionID != "live" {
		t.Fatalf("unrendered ids=%#v grants=%#v", visible, out)
	}
}

func TestMaterializeReadySurfaceRequiresRegistry(t *testing.T) {
	if _, err := MaterializeReadySurface(ReadySurfaceRequest{}); err == nil {
		t.Fatal("nil registry must fail")
	}
	registry := semanticRegistry(t)
	out, err := MaterializeReadySurface(ReadySurfaceRequest{Registry: registry, Plan: ToolPlan{ID: "plan"}})
	if err != nil || out != nil {
		t.Fatalf("empty needed/grants out=%#v err=%v", out, err)
	}
}

func TestProjectRenderedDefinitionsMarksAndIndexes(t *testing.T) {
	mark := map[string]bool{}
	byName := map[string]map[string]interface{}{}
	defs := ProjectRenderedDefinitions([]RenderedTool{{
		FunctionName: "web_search",
		Definition:   map[string]interface{}{"type": "function"},
	}}, mark, byName)
	if len(defs) != 1 || !mark["web_search"] || byName["web_search"]["type"] != "function" {
		t.Fatalf("defs=%#v mark=%#v byName=%#v", defs, mark, byName)
	}
}

func TestPlaceMaterializedGrantAndRetireLiveGrant(t *testing.T) {
	live := map[string]InvocationGrant{}
	retired := map[string]InvocationGrant{}
	exposed := RouteMaterialization{State: RouteMaterializationExposed, Grant: InvocationGrant{SelectionID: "sel-1", AdapterName: "web_search", Token: "tok-a"}}
	if err := PlaceMaterializedGrant(exposed, live, retired); err != nil || live["web_search"].Token != "tok-a" {
		t.Fatalf("exposed bind live=%#v err=%v", live, err)
	}
	retiredMat := RouteMaterialization{State: RouteMaterializationRetired, Grant: InvocationGrant{SelectionID: "sel-2", AdapterName: "web_fetch", Token: "tok-b"}}
	if err := PlaceMaterializedGrant(retiredMat, live, retired); err != nil || retired["web_fetch"].Token != "tok-b" {
		t.Fatalf("retired place=%#v err=%v", retired, err)
	}
	called := false
	if err := RetireLiveGrant(live, retired, "web_search", live["web_search"], func() error {
		called = true
		return nil
	}); err != nil || !called || live["web_search"].Token != "" || retired["web_search"].Token != "tok-a" {
		t.Fatalf("retire live=%#v retired=%#v called=%t err=%v", live, retired, called, err)
	}
	live["web_search"] = InvocationGrant{SelectionID: "sel-3", AdapterName: "web_search", Token: "tok-c"}
	if err := RetireLiveGrantsForSelection(live, retired, "sel-3", nil); err != nil || live["web_search"].Token != "" || retired["web_search"].Token != "tok-c" {
		t.Fatalf("retire by selection live=%#v retired=%#v err=%v", live, retired, err)
	}
}

func TestRetireConsumedGrantMovesEvenWhenDurableFails(t *testing.T) {
	live := map[string]InvocationGrant{
		"web_search": {SelectionID: "sel-4", AdapterName: "web_search", Token: "tok-d"},
	}
	retired := map[string]InvocationGrant{}
	if err := RetireLiveGrant(live, retired, "web_search", live["web_search"], func() error {
		return fmt.Errorf("durable retire failed")
	}); err == nil || live["web_search"].Token == "" {
		t.Fatalf("strict retire must keep live on durable failure: live=%#v err=%v", live, err)
	}
	if err := RetireConsumedGrant(live, retired, "web_search", live["web_search"], func() error {
		return fmt.Errorf("durable retire failed")
	}); err == nil || !strings.Contains(err.Error(), "durable retire failed") || live["web_search"].Token != "" || retired["web_search"].Token != "tok-d" {
		t.Fatalf("consumed retire must drop live: live=%#v retired=%#v err=%v", live, retired, err)
	}
	live["web_fetch"] = InvocationGrant{SelectionID: "sel-5", AdapterName: "web_fetch", Token: "tok-e"}
	if err := RetireLiveGrantsForSelection(live, retired, "sel-5", func(string, InvocationGrant) error {
		return fmt.Errorf("selection durable failed")
	}); err == nil || live["web_fetch"].Token != "" || retired["web_fetch"].Token != "tok-e" {
		t.Fatalf("selection consumed retire must drop live: live=%#v retired=%#v err=%v", live, retired, err)
	}
}

func TestRetireConsumedLiveGrantsHidesFailedExecution(t *testing.T) {
	issuer, err := NewInvocationIssuer([]byte(strings.Repeat("s", 32)))
	if err != nil {
		t.Fatal(err)
	}
	store := NewMemoryPlanExecutionStore()
	executor, err := NewPlanExecutor(issuer, store)
	if err != nil {
		t.Fatal(err)
	}
	scope := InvocationScope{RootTaskID: "task", PlanID: "plan", SessionID: "session", TurnID: "turn", PrincipalID: "principal"}
	live := map[string]InvocationGrant{
		"web_search": {SelectionID: "sel-fail", AdapterName: "web_search", Token: "tok-f"},
		"web_fetch":  {SelectionID: "sel-open", AdapterName: "web_fetch", Token: "tok-o"},
	}
	retired := map[string]InvocationGrant{}
	if _, acquired, err := store.Acquire(PlanExecutionRecord{Scope: scope, SelectionID: "sel-fail"}); err != nil || !acquired {
		t.Fatalf("acquire acquired=%t err=%v", acquired, err)
	}
	if _, err := store.Complete(scope, "sel-fail", PlanExecutionFailed, "digest", "boom", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	called := false
	if err := RetireConsumedLiveGrants(live, retired, executor, scope, func(grant InvocationGrant) error {
		if grant.SelectionID == "sel-fail" {
			called = true
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !called || live["web_search"].Token != "" || retired["web_search"].Token != "tok-f" || live["web_fetch"].Token != "tok-o" {
		t.Fatalf("consumed live=%#v retired=%#v called=%t", live, retired, called)
	}
}

func TestClosedManagedDefinitionsDropsGatewaysAndOptionalGrants(t *testing.T) {
	defs := []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "web_search"}},
		{"type": "function", "function": map[string]interface{}{"name": "call_mcp_tool"}},
		{"type": "function", "function": map[string]interface{}{"name": "generate_pdf"}},
	}
	open := ClosedManagedDefinitions(defs, nil)
	if len(open) != 2 {
		t.Fatalf("nil grants should only drop gateways, got %#v", open)
	}
	closed := ClosedManagedDefinitions(defs, map[string]InvocationGrant{"web_search": {}})
	if len(closed) != 1 {
		t.Fatalf("grant filter=%#v", closed)
	}
	if empty := ClosedManagedDefinitions(defs, map[string]InvocationGrant{}); empty != nil {
		t.Fatalf("empty grants must yield nothing, got %#v", empty)
	}
}

func TestClosedManagedDefinitionsForProfileFiltersLight(t *testing.T) {
	plan := ToolPlan{Selections: []PlannedSelection{
		{ID: "read", Effects: []EffectClass{EffectReadOnly}},
		{ID: "write", Effects: []EffectClass{EffectLocalMutation}},
	}}
	grants := map[string]InvocationGrant{
		"invoke_read":  {SelectionID: "read"},
		"invoke_write": {SelectionID: "write"},
	}
	defs := []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "invoke_read"}},
		{"type": "function", "function": map[string]interface{}{"name": "invoke_write"}},
		{"type": "function", "function": map[string]interface{}{"name": "write_file"}},
		{"type": "function", "function": map[string]interface{}{"name": "call_mcp_tool"}},
	}
	full := ClosedManagedDefinitionsForProfile(defs, plan, grants, false)
	if len(full) != 2 {
		t.Fatalf("full profile=%#v, want granted read+write", full)
	}
	light := ClosedManagedDefinitionsForProfile(defs, plan, grants, true)
	if len(light) != 1 {
		t.Fatalf("light profile=%#v", light)
	}
	fn, _ := light[0]["function"].(map[string]interface{})
	if name, _ := fn["name"].(string); name != "invoke_read" || !GrantSelectionIsLightPromptSafe(plan, grants, name) {
		t.Fatalf("light remaining=%q", name)
	}
	if GrantSelectionIsLightPromptSafe(plan, grants, "invoke_write") || GrantSelectionIsLightPromptSafe(plan, grants, "write_file") || GrantSelectionIsLightPromptSafe(plan, grants, "") {
		t.Fatal("mutating or unknown names must not be light-safe")
	}
}
