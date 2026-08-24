package agentservice

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/scheduler"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

type fakeHostScheduleDispatcher struct {
	principal     Principal
	destinationID string
	result        string
	err           error
}

func (f *fakeHostScheduleDispatcher) DispatchReviewedHostSchedule(_ context.Context, principal Principal, destinationID string) (string, error) {
	f.principal = principal
	f.destinationID = destinationID
	return f.result, f.err
}

func TestReviewedHostScheduleDispatchRequiresTrustedDestination(t *testing.T) {
	if _, _, _, err := ProjectReviewedHostScheduleDispatchProvider(&fakeHostScheduleDispatcher{}, ""); err == nil {
		t.Fatal("empty destination must not project dispatch")
	}
	if _, _, _, err := ProjectReviewedHostScheduleDispatchProvider(&fakeHostScheduleDispatcher{}, "ops"); err == nil {
		t.Fatal("bare group name must not project dispatch")
	}
	provider, definition, _, err := ProjectReviewedHostScheduleDispatchProvider(&fakeHostScheduleDispatcher{result: "prepared"}, "group:ops")
	if err != nil {
		t.Fatal(err)
	}
	if provider.Provides[0].Capability != coretool.CapabilityScheduleDispatchChannel || provider.AdapterName == "manage_schedule" {
		t.Fatalf("provider=%#v", provider)
	}
	params := definition["function"].(map[string]interface{})["parameters"].(map[string]interface{})
	if len(params["properties"].(map[string]interface{})) != 0 {
		t.Fatalf("dispatch schema must be empty: %#v", params)
	}
}

func TestReviewedHostScheduleDispatchPlansOnlyWithTrustedDestination(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	dispatcher := &fakeHostScheduleDispatcher{result: "Schedule dispatch prepared. This is not a send."}
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{
		Schedule: &fakeHostScheduleAdministrator{result: "listed"}, ScheduleDispatch: dispatcher, DestinationID: "group:ops",
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-dispatch", TurnID: "turn-dispatch", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "dispatch", Capability: coretool.CapabilityScheduleDispatchChannel, Required: true}},
	})
	if err != nil || len(plan.Selections) != 1 || len(plan.Unmet) != 0 {
		t.Fatalf("dispatch plan=%#v err=%v", plan, err)
	}
	if plan.Selections[0].AdapterName != reviewedHostScheduleDispatchAdapterName {
		t.Fatalf("selection=%#v", plan.Selections[0])
	}
	rejected := catalog.ExecuteSelection(context.Background(), Principal{TenantID: "t", UserID: "u"}, nil, nil, plan.Selections[0], `{"group_name":"ops"}`)
	if rejected.Succeeded || rejected.Unknown && !strings.Contains(rejected.Result, "parameter") && !strings.Contains(rejected.ReasonCode, "parameter") && !strings.Contains(rejected.Result, "unknown_field") && !strings.Contains(rejected.ReasonCode, "unknown_field") && !strings.Contains(rejected.Result, "rejected") {
		if rejected.Succeeded {
			t.Fatalf("group_name must fail closed: %#v", rejected)
		}
	}

	unmetCatalog, unmetLifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{
		Schedule: &fakeHostScheduleAdministrator{result: "listed"},
	})
	if err != nil {
		t.Fatal(err)
	}
	unmetSnapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(unmetCatalog.Providers, unmetLifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	unmetPlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-unmet", TurnID: "turn-unmet", Snapshot: unmetSnapshot,
		Needs: []coretool.CapabilityNeed{{ID: "dispatch", Capability: coretool.CapabilityScheduleDispatchChannel, Required: true}},
	})
	if err != nil || len(unmetPlan.Selections) != 0 {
		t.Fatalf("dispatch without destination must be unmet, plan=%#v err=%v", unmetPlan, err)
	}
}

func TestReviewedHostScheduleDispatchUICPlansAdministerThenDispatch(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{
		Schedule: &fakeHostScheduleAdministrator{result: "created"}, ScheduleDispatch: &fakeHostScheduleDispatcher{result: "prepared"}, DestinationID: "group:ops",
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-uic", TurnID: "turn-uic", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{
			{ID: "administer", Capability: CapabilityScheduleAdminister, Required: true},
			{ID: "dispatch", Capability: CapabilityScheduleDispatch, Required: true},
		},
	})
	if err != nil || len(plan.Selections) != 2 || len(plan.Unmet) != 0 {
		t.Fatalf("uic dispatch plan=%#v err=%v", plan, err)
	}
	var administerID string
	var dispatchSel coretool.PlannedSelection
	for _, selection := range plan.Selections {
		if selection.AdapterName == "manage_schedule" || selection.AdapterName == "send_to_im" {
			t.Fatalf("selection leaked soup name: %#v", selection)
		}
		if selection.FitProof.MatchedCapability == CapabilityScheduleAdminister {
			administerID = selection.ID
		}
		if selection.FitProof.MatchedCapability == CapabilityScheduleDispatch {
			dispatchSel = selection
		}
	}
	if administerID == "" || dispatchSel.ID == "" {
		t.Fatalf("selections=%#v", plan.Selections)
	}
	found := false
	for _, requirement := range dispatchSel.Requires {
		if requirement == administerID {
			found = true
		}
	}
	if !found {
		t.Fatalf("dispatch must wait for administer, requires=%#v administer=%s", dispatchSel.Requires, administerID)
	}

	unmetCatalog, unmetLifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{
		Schedule: &fakeHostScheduleAdministrator{result: "created"},
	})
	if err != nil {
		t.Fatal(err)
	}
	unmetSnapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(unmetCatalog.Providers, unmetLifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	unmetPlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-unmet-uic", TurnID: "turn-unmet-uic", Snapshot: unmetSnapshot,
		Needs: []coretool.CapabilityNeed{
			{ID: "administer", Capability: CapabilityScheduleAdminister, Required: true},
			{ID: "dispatch", Capability: CapabilityScheduleDispatch, Required: true},
		},
	})
	if err != nil || len(unmetPlan.Unmet) == 0 {
		t.Fatalf("dispatch without destination must stay unmet, plan=%#v err=%v", unmetPlan, err)
	}
}

func TestTrustedChannelDestinationIDUsesInboundMetadataOnly(t *testing.T) {
	if got := TrustedChannelDestinationID(nil); got != "" {
		t.Fatalf("empty=%q", got)
	}
	if got := TrustedChannelDestinationID(map[string]string{"destination_id": "group:ops"}); got != "group:ops" {
		t.Fatalf("typed dest=%q", got)
	}
	if got := TrustedChannelDestinationID(map[string]string{"destination_id": "ops"}); got != "" {
		t.Fatalf("bare destination_id must not be trusted: %q", got)
	}
	if got := TrustedChannelDestinationID(map[string]string{"group_id": "ops"}); got != "group:ops" {
		t.Fatalf("group_id=%q", got)
	}
	if got := TrustedChannelDestinationID(map[string]string{"im_user_id": "alice"}); got != "user:alice" {
		t.Fatalf("im_user_id=%q", got)
	}
	if got := TrustedChannelDestinationID(map[string]string{"contact_id": "wx-1"}); got != "user:wx-1" {
		t.Fatalf("contact_id=%q", got)
	}
	if got := TrustedChannelDestinationID(map[string]string{"group_id": "g1", "contact_id": "u1"}); got != "group:g1" {
		t.Fatalf("group wins over contact: %q", got)
	}
	if got := TrustedChannelDestinationID(map[string]string{"group_name": "ops", "channel": "lansenger", "llm_service_group_id": "billing"}); got != "" {
		t.Fatalf("display/billing keys must not become dest: %q", got)
	}
	if got := TrustedChannelDestinationID(map[string]string{"contact_id": "client:conv"}); got != "user:client:conv" {
		t.Fatalf("typed transport contact=%q", got)
	}
}

func TestReviewedHostOwnedServicesWiresTrustedDestination(t *testing.T) {
	principal := Principal{TenantID: "t", UserID: "u"}
	cb := &coreAgentCallbacks{
		principal:            principal,
		trustedDestinationID: "group:ops",
		imMessageHandler:     func(map[string]interface{}) string { t.Fatal("dispatch must not send"); return "sent" },
		scheduleHandler:      func(map[string]interface{}) string { t.Fatal("dispatch must not use soup schedule"); return "listed" },
	}
	services := cb.reviewedHostOwnedServices()
	if services.ScheduleDispatch == nil || services.DestinationID != "group:ops" {
		t.Fatalf("services=%#v", services)
	}
	out, err := cb.DispatchReviewedHostSchedule(context.Background(), principal, "group:ops")
	if err != nil || !strings.Contains(out, "This is not a send.") || !strings.Contains(out, "group:ops") {
		t.Fatalf("prepare=%q err=%v", out, err)
	}
	if _, err := cb.DispatchReviewedHostSchedule(context.Background(), principal, "user:other"); err == nil {
		t.Fatal("mismatched destination must fail closed")
	}
	empty := (&coreAgentCallbacks{principal: principal}).reviewedHostOwnedServices()
	if empty.ScheduleDispatch != nil || empty.DestinationID != "" {
		t.Fatalf("no dest must stay unpublished: %#v", empty)
	}
}

func TestReviewedHostScheduleDispatchBindsLastAdministeredTask(t *testing.T) {
	principal := Principal{TenantID: "t", UserID: "u"}
	bindings := NewScheduleDispatchBindingStore("")
	cb := &coreAgentCallbacks{
		principal:                  principal,
		trustedDestinationID:       "group:ops",
		inboundChannelScope:        "lansenger",
		scheduleDispatchBindings:   bindings,
		lastAdministeredScheduleID: "task-1",
	}
	out, err := cb.DispatchReviewedHostSchedule(context.Background(), principal, "group:ops")
	if err != nil || !strings.Contains(out, "This is not a send.") {
		t.Fatalf("prepare=%q err=%v", out, err)
	}
	binding, ok := bindings.Get("task-1")
	if !ok || binding.DestinationID != "group:ops" || binding.ChannelScope != "lansenger" {
		t.Fatalf("binding=%#v ok=%v", binding, ok)
	}
	if _, err := cb.DispatchReviewedHostSchedule(context.Background(), principal, "group:ops"); err != nil {
		t.Fatal(err)
	}
	if _, ok := bindings.Get("task-1"); !ok {
		t.Fatal("second prepare without a new administer must not require a bind")
	}
}

func TestReviewedHostScheduleDispatchRequiresSendableChannelWhenBinding(t *testing.T) {
	principal := Principal{TenantID: "t", UserID: "u"}
	cb := &coreAgentCallbacks{
		principal:                  principal,
		trustedDestinationID:       "user:wx-1",
		inboundChannelScope:        "core-agent",
		scheduleDispatchBindings:   NewScheduleDispatchBindingStore(""),
		lastAdministeredScheduleID: "task-2",
	}
	if _, err := cb.DispatchReviewedHostSchedule(context.Background(), principal, "user:wx-1"); err == nil || !strings.Contains(err.Error(), "trusted_dispatch_channel_unavailable") {
		t.Fatalf("core-agent must not bind a fire channel: %v", err)
	}
}

func TestFireReviewedHostScheduleDispatchCASUnknownAndNoResend(t *testing.T) {
	dir := t.TempDir()
	coordinator, err := coretool.NewSQLiteSemanticExecutionCoordinator(filepath.Join(dir, "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = coordinator.Close() })
	bindings := NewScheduleDispatchBindingStore("")
	if err := bindings.Put(ScheduleDispatchBinding{TaskID: "task-fire", ChannelScope: "lansenger", DestinationID: "group:ops", PrincipalID: "u"}); err != nil {
		t.Fatal(err)
	}
	sent := 0
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	deps := reviewedHostScheduleDispatchFireDeps{
		Bindings: bindings, Coordinator: coordinator,
		Send: func(_ context.Context, channel string, targets []scheduler.DeliveryTarget, text string) error {
			if channel != scheduler.DeliveryChannelLansenger || len(targets) != 1 || targets[0].GroupID != "ops" {
				t.Fatalf("send channel=%s targets=%#v", channel, targets)
			}
			sent++
			return nil
		},
		Now: func() time.Time { return now },
	}
	task := &scheduler.ScheduledTask{ID: "task-fire", Action: "standup notes"}
	if err := FireReviewedHostScheduleDispatch(context.Background(), deps, task, "standup notes", nil); err != nil {
		t.Fatal(err)
	}
	if sent != 1 {
		t.Fatalf("sent=%d", sent)
	}
	scope := coretool.InvocationScope{
		RootTaskID: "schedule-fire:task-fire", PlanID: "run:" + now.Format("20060102T150405.000000000Z"),
		SessionID: "u", TurnID: "fire:" + now.Format("20060102T150405.000000000Z"), PrincipalID: "u",
	}
	if _, err := coordinator.SettleStandaloneDelivery(scope, reviewedHostScheduleDispatchFireSelectionID, coretool.DeliveryAccepted, "late-receipt", "late", now); err == nil {
		t.Fatal("late accepted settle after terminal unknown must not succeed")
	}
	if err := FireReviewedHostScheduleDispatch(context.Background(), deps, task, "standup notes", nil); err != nil {
		t.Fatal(err)
	}
	if sent != 1 {
		t.Fatalf("same run must not resend, sent=%d", sent)
	}
	now = now.Add(24 * time.Hour)
	if err := FireReviewedHostScheduleDispatch(context.Background(), deps, task, "next day", nil); err != nil {
		t.Fatal(err)
	}
	if sent != 2 {
		t.Fatalf("next occurrence sent=%d", sent)
	}
}

func TestDiscoverReviewedHostScheduleDispatchDataDirsSkipsUnarmed(t *testing.T) {
	root := t.TempDir()
	armedDir := filepath.Join(root, "tenants", "t", "users", "u", "data")
	store := NewScheduleDispatchBindingStore(filepath.Join(armedDir, "semantic-routing", "schedule-dispatch-bindings.json"))
	if err := store.Put(ScheduleDispatchBinding{TaskID: "task-armed", ChannelScope: "weixin", DestinationID: "user:wx-1"}); err != nil {
		t.Fatal(err)
	}
	emptyDir := filepath.Join(root, "tenants", "t", "users", "empty", "data")
	if err := os.MkdirAll(filepath.Join(emptyDir, "semantic-routing"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(emptyDir, "semantic-routing", "schedule-dispatch-bindings.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	unarmedDir := filepath.Join(root, "tenants", "t", "users", "unarmed", "data")
	if err := os.MkdirAll(filepath.Join(unarmedDir, "semantic-routing"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unarmedDir, "semantic-routing", "schedule-dispatch-bindings.json"), []byte(`{"task-1":{"task_id":"task-1","channel_scope":"core-agent","destination_id":"group:ops"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	found := DiscoverReviewedHostScheduleDispatchDataDirs(root, "")
	if len(found) != 1 || filepath.Clean(found[0]) != filepath.Clean(armedDir) {
		t.Fatalf("discover=%#v want %q", found, armedDir)
	}
}

func TestRecoverReviewedHostScheduleDispatchFireStartsAfterRestart(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "tenants", "t", "users", "u", "data")
	store := NewScheduleDispatchBindingStore(filepath.Join(dataDir, "semantic-routing", "schedule-dispatch-bindings.json"))
	if err := store.Put(ScheduleDispatchBinding{TaskID: "task-recover", ChannelScope: "lansenger", DestinationID: "group:ops"}); err != nil {
		t.Fatal(err)
	}
	coordinator, err := coretool.NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = coordinator.Close() })

	cold := &CoreAgentExecutor{dynamicSemanticRouting: &DynamicSemanticRouting{Coordinator: coordinator}}
	if n := cold.RecoverReviewedHostScheduleDispatchFire(root); n != 0 {
		t.Fatalf("recover without send must not start, n=%d", n)
	}
	if cold.ReviewedHostScheduleDispatchFireStarted(dataDir) {
		t.Fatal("missing send must not mark fire started")
	}

	noCoord := &CoreAgentExecutor{
		ScheduleDispatchSend: func(context.Context, Principal, string, []scheduler.DeliveryTarget, string) error { return nil },
	}
	if n := noCoord.RecoverReviewedHostScheduleDispatchFire(root); n != 0 {
		t.Fatalf("recover without coordinator must not start, n=%d", n)
	}

	e := &CoreAgentExecutor{
		ScheduleDispatchSend:   func(context.Context, Principal, string, []scheduler.DeliveryTarget, string) error { return nil },
		dynamicSemanticRouting: &DynamicSemanticRouting{Coordinator: coordinator},
	}
	if n := e.RecoverReviewedHostScheduleDispatchFire(root); n != 1 {
		t.Fatalf("recover started=%d", n)
	}
	found := DiscoverReviewedHostScheduleDispatchDataDirs(root)
	if len(found) != 1 || !e.ReviewedHostScheduleDispatchFireStarted(found[0]) {
		t.Fatalf("armed binding must start fire after recover, found=%#v", found)
	}
	if n := e.RecoverReviewedHostScheduleDispatchFire(root); n != 0 {
		t.Fatalf("second recover must be idempotent, n=%d", n)
	}
	e.mu.Lock()
	for _, mgr := range e.userSchedules {
		if mgr != nil {
			t.Cleanup(mgr.Stop)
		}
	}
	e.mu.Unlock()

	admin := &CoreAgentExecutor{}
	if admin.scheduleManagerForDataDir(dataDir) == nil {
		t.Fatal("administer store must still open after recover")
	}
	if admin.ReviewedHostScheduleDispatchFireStarted(dataDir) {
		t.Fatal("scheduleManagerForDataDir must not start fire")
	}
}
