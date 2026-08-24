package agentservice

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/scheduler"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

type fakeHostMessageSender struct {
	principal     Principal
	destinationID string
	text          string
	result        string
	err           error
}

func (f *fakeHostMessageSender) PrepareReviewedHostMessageSend(_ context.Context, principal Principal, destinationID, text string) (string, error) {
	f.principal = principal
	f.destinationID = destinationID
	f.text = text
	return f.result, f.err
}

func TestReviewedHostMessageSendRequiresTrustedDestinationAndRejectsSoup(t *testing.T) {
	if _, _, _, err := ProjectReviewedHostMessageSendProvider(&fakeHostMessageSender{}, ""); err == nil {
		t.Fatal("empty destination must not project message send")
	}
	if _, _, _, err := ProjectReviewedHostMessageSendProvider(&fakeHostMessageSender{}, "ops"); err == nil {
		t.Fatal("bare group name must not project message send")
	}
	provider, definition, _, err := ProjectReviewedHostMessageSendProvider(&fakeHostMessageSender{result: "prepared"}, "group:ops")
	if err != nil {
		t.Fatal(err)
	}
	if provider.Provides[0].Capability != CapabilityMessageSend || provider.AdapterName == "send_to_im" || provider.AdapterName == "im_message" {
		t.Fatalf("provider=%#v", provider)
	}
	if provider.Provides[0].Qualifiers[QualifierMessageFormat] != MessageFormatText {
		t.Fatalf("qualifiers=%#v", provider.Provides[0].Qualifiers)
	}
	fn, _ := definition["function"].(map[string]interface{})
	params, _ := fn["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	if _, ok := props["text"]; !ok || len(props) != 1 {
		t.Fatalf("message send schema=%#v", props)
	}
	for _, key := range []string{"channel", "destination", "group_name", "group_id", "user_id"} {
		if _, ok := props[key]; ok {
			t.Fatalf("message send schema leaked %s", key)
		}
	}
}

func TestReviewedHostMessageSendPlansOnlyWithTrustedDestination(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	sender := &fakeHostMessageSender{result: "IM message prepared for group:ops. This is not a send."}
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{
		MessageSend: sender, DestinationID: "group:ops",
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-send", TurnID: "turn-send", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{
			ID: "send", Capability: CapabilityMessageSend, Required: true,
			Qualifiers: map[string]string{QualifierMessageFormat: MessageFormatText},
		}},
	})
	if err != nil || len(plan.Selections) != 1 || len(plan.Unmet) != 0 {
		t.Fatalf("message send plan=%#v err=%v", plan, err)
	}
	if plan.Selections[0].AdapterName == "send_to_im" || plan.Selections[0].AdapterName == "im_message" {
		t.Fatalf("selection=%#v", plan.Selections[0])
	}
	if dynamicHostLocalMutationSelection(plan.Selections[0]) || dynamicHostObservedExternalSelection(plan.Selections[0]) || !dynamicSelectionRequiresReceipt(plan.Selections[0]) {
		t.Fatalf("message send must require the IM receipt coordinator, selection=%#v", plan.Selections[0])
	}
	principal := Principal{TenantID: "t", UserID: "u"}
	rejected := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"text":"hi","channel":"lansenger","group_name":"ops"}`)
	if rejected.Succeeded || rejected.Unknown {
		t.Fatalf("channel soup must fail closed, result=%#v", rejected)
	}
	withoutCoordinator := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"text":"hi"}`)
	if withoutCoordinator.Succeeded || withoutCoordinator.ReasonCode != "dynamic_effect_coordinator_unavailable" {
		t.Fatalf("valid send without coordinator must fail closed, result=%#v", withoutCoordinator)
	}

	unmetCatalog, unmetLifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{})
	if err != nil {
		t.Fatal(err)
	}
	unmetSnapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(unmetCatalog.Providers, unmetLifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	unmetPlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-unmet", TurnID: "turn-unmet", Snapshot: unmetSnapshot,
		Needs: []coretool.CapabilityNeed{{
			ID: "send", Capability: CapabilityMessageSend, Required: true,
			Qualifiers: map[string]string{QualifierMessageFormat: MessageFormatText},
		}},
	})
	if err != nil || len(unmetPlan.Selections) != 0 {
		t.Fatalf("message send without destination must stay unmet, plan=%#v err=%v", unmetPlan, err)
	}
}

func TestReviewedHostMessageSendPrepareIsNotASend(t *testing.T) {
	principal := Principal{TenantID: "t", UserID: "u"}
	cb := &coreAgentCallbacks{
		principal:            principal,
		trustedDestinationID: "user:alice",
		imMessageHandler: func(map[string]interface{}) string {
			t.Fatal("message send must not call im_message soup")
			return "sent"
		},
	}
	services := cb.reviewedHostOwnedServices()
	if services.MessageSend == nil || services.DestinationID != "user:alice" {
		t.Fatalf("services=%#v", services)
	}
	out, err := cb.PrepareReviewedHostMessageSend(context.Background(), principal, "user:alice", "hello")
	if err != nil || !strings.Contains(out, "This is not a send.") || !strings.Contains(out, "user:alice") {
		t.Fatalf("prepare=%q err=%v", out, err)
	}
	if _, err := cb.PrepareReviewedHostMessageSend(context.Background(), principal, "user:bob", "hello"); err == nil {
		t.Fatal("mismatched destination must fail closed")
	}
	empty := (&coreAgentCallbacks{principal: principal}).reviewedHostOwnedServices()
	if empty.MessageSend != nil {
		t.Fatal("message send without destination must stay unpublished")
	}
}

func TestFireReviewedHostMessageSendCASUnknownAndNoResend(t *testing.T) {
	dir := t.TempDir()
	coordinator, err := coretool.NewSQLiteSemanticExecutionCoordinator(filepath.Join(dir, "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = coordinator.Close() })
	sent := 0
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	deps := reviewedHostMessageSendFireDeps{
		Coordinator: coordinator, ChannelScope: "lansenger", DestinationID: "group:ops", PrincipalID: "u", Text: "hello ops",
		Send: func(_ context.Context, channel string, targets []scheduler.DeliveryTarget, text string) error {
			if channel != scheduler.DeliveryChannelLansenger || len(targets) != 1 || targets[0].GroupID != "ops" || text != "hello ops" {
				t.Fatalf("send channel=%s targets=%#v text=%q", channel, targets, text)
			}
			sent++
			return nil
		},
		Now: func() time.Time { return now },
	}
	if err := FireReviewedHostMessageSend(context.Background(), deps); err == nil || err.Error() != "host_message_send_unknown" {
		t.Fatalf("nil send must settle unknown, err=%v", err)
	}
	if sent != 1 {
		t.Fatalf("sent=%d", sent)
	}
	textKey := coretool.SchemaDigest([]byte("hello ops"))[:16]
	scope := coretool.InvocationScope{
		RootTaskID: "message-send:group:ops", PlanID: "run:" + now.Format("20060102T150405.000000000Z"),
		SessionID: "u", TurnID: "send:" + now.Format("20060102T150405.000000000Z") + ":" + textKey, PrincipalID: "u",
	}
	if _, err := coordinator.SettleStandaloneDelivery(scope, reviewedHostMessageSendFireSelectionID, coretool.DeliveryAccepted, "late-receipt", "late", now); err == nil {
		t.Fatal("late accepted settle after terminal unknown must not succeed")
	}
	if err := FireReviewedHostMessageSend(context.Background(), deps); err == nil || err.Error() != "host_message_send_unknown" {
		t.Fatalf("same prepared delivery must not resend, err=%v", err)
	}
	if sent != 1 {
		t.Fatalf("same run must not resend, sent=%d", sent)
	}
	now = now.Add(24 * time.Hour)
	if err := FireReviewedHostMessageSend(context.Background(), deps); err == nil || err.Error() != "host_message_send_unknown" {
		t.Fatalf("next occurrence must still be unknown, err=%v", err)
	}
	if sent != 2 {
		t.Fatalf("next occurrence sent=%d", sent)
	}
}

func TestReviewedHostMessageSendFireUsesScheduleDispatchSendOnce(t *testing.T) {
	dir := t.TempDir()
	coordinator, err := coretool.NewSQLiteSemanticExecutionCoordinator(filepath.Join(dir, "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = coordinator.Close() })
	principal := Principal{TenantID: "t", UserID: "u"}
	sent := 0
	cb := &coreAgentCallbacks{
		principal:            principal,
		trustedDestinationID: "user:alice",
		inboundChannelScope:  "lansenger",
		imMessageHandler: func(map[string]interface{}) string {
			t.Fatal("message send must not call im_message soup")
			return "sent"
		},
		executor: &CoreAgentExecutor{
			ScheduleDispatchSend: func(_ context.Context, got Principal, channel string, targets []scheduler.DeliveryTarget, text string) error {
				if got.TenantID != principal.TenantID || got.UserID != principal.UserID || channel != scheduler.DeliveryChannelLansenger || len(targets) != 1 || targets[0].UserID != "alice" || text != "hello" {
					t.Fatalf("send principal=%#v channel=%s targets=%#v text=%q", got, channel, targets, text)
				}
				sent++
				return nil
			},
			dynamicSemanticRouting: &DynamicSemanticRouting{Coordinator: coordinator},
		},
	}
	out, err := cb.PrepareReviewedHostMessageSend(context.Background(), principal, "user:alice", "hello")
	if out != "" || err == nil || err.Error() != "host_message_send_unknown" {
		t.Fatalf("armed fire must stay unknown, out=%q err=%v", out, err)
	}
	if sent != 1 {
		t.Fatalf("send called %d times", sent)
	}
}

func TestReviewedHostMessageSendCoreAgentDoesNotFire(t *testing.T) {
	dir := t.TempDir()
	coordinator, err := coretool.NewSQLiteSemanticExecutionCoordinator(filepath.Join(dir, "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = coordinator.Close() })
	principal := Principal{TenantID: "t", UserID: "u"}
	cb := &coreAgentCallbacks{
		principal:            principal,
		trustedDestinationID: "user:alice",
		inboundChannelScope:  "core-agent",
		imMessageHandler: func(map[string]interface{}) string {
			t.Fatal("message send must not call im_message soup")
			return "sent"
		},
		executor: &CoreAgentExecutor{
			ScheduleDispatchSend: func(context.Context, Principal, string, []scheduler.DeliveryTarget, string) error {
				t.Fatal("core-agent must not fire a channel send")
				return nil
			},
			dynamicSemanticRouting: &DynamicSemanticRouting{Coordinator: coordinator},
		},
	}
	out, err := cb.PrepareReviewedHostMessageSend(context.Background(), principal, "user:alice", "hello")
	if err != nil || !strings.Contains(out, "This is not a send.") {
		t.Fatalf("core-agent must stay prepare-only, out=%q err=%v", out, err)
	}
}
