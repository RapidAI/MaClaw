package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestTrustedDestinationToSchedulerTarget(t *testing.T) {
	group, err := trustedDestinationToSchedulerTarget("group:ops")
	if err != nil || group.Kind != scheduler.DeliveryKindGroup || group.GroupID != "ops" {
		t.Fatalf("group=%#v err=%v", group, err)
	}
	user, err := trustedDestinationToSchedulerTarget("user:u1")
	if err != nil || user.Kind != scheduler.DeliveryKindUser || user.UserID != "u1" {
		t.Fatalf("user=%#v err=%v", user, err)
	}
	if _, err := trustedDestinationToSchedulerTarget("ops"); err == nil {
		t.Fatal("bare name must not become a target")
	}
	if _, err := trustedDestinationToSchedulerTarget("group:"); err == nil {
		t.Fatal("empty group id must fail")
	}
}

func TestFireManagedScheduleDispatchCASAndNoResend(t *testing.T) {
	bindings := newScheduleDispatchBindingStore("")
	if err := bindings.Put(scheduleDispatchBinding{
		TaskID: "task-1", ChannelScope: "lansenger", DestinationID: "group:ops", PrincipalID: "user-1",
	}); err != nil {
		t.Fatal(err)
	}
	store := tool.NewMemoryArtifactStore()
	var mu sync.Mutex
	var sent []string
	now := time.Date(2026, 8, 16, 9, 0, 0, 0, time.UTC)
	deps := scheduleDispatchFireDeps{
		Bindings: bindings,
		Store:    store,
		Now:      func() time.Time { return now },
		Send: func(_ context.Context, channel string, targets []scheduler.DeliveryTarget, text string) error {
			mu.Lock()
			defer mu.Unlock()
			if channel != scheduler.DeliveryChannelLansenger || len(targets) != 1 || targets[0].GroupID != "ops" {
				t.Fatalf("send channel=%s targets=%#v", channel, targets)
			}
			sent = append(sent, text)
			return nil
		},
	}
	task := &scheduler.ScheduledTask{ID: "task-1", Name: "morning"}
	if err := fireManagedScheduleDispatch(context.Background(), deps, task, "standup notes", nil); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 || sent[0] != "standup notes" {
		t.Fatalf("first fire sent=%#v", sent)
	}
	if task.Delivery != nil {
		t.Fatal("fire must not write task.Delivery")
	}
	if err := fireManagedScheduleDispatch(context.Background(), deps, task, "standup notes", nil); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 {
		t.Fatalf("same occurrence must not resend: %#v", sent)
	}
	now = now.Add(24 * time.Hour)
	if err := fireManagedScheduleDispatch(context.Background(), deps, task, "next day", nil); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 2 || sent[1] != "next day" {
		t.Fatalf("next occurrence sent=%#v", sent)
	}
}

func TestFireManagedScheduleDispatchUnknownDoesNotResendSameRun(t *testing.T) {
	bindings := newScheduleDispatchBindingStore("")
	if err := bindings.Put(scheduleDispatchBinding{
		TaskID: "task-2", ChannelScope: "lansenger", DestinationID: "user:u1", PrincipalID: "user-1",
	}); err != nil {
		t.Fatal(err)
	}
	store := tool.NewMemoryArtifactStore()
	var sent int
	now := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	deps := scheduleDispatchFireDeps{
		Bindings: bindings,
		Store:    store,
		Now:      func() time.Time { return now },
		Send: func(context.Context, string, []scheduler.DeliveryTarget, string) error {
			sent++
			return errors.New("network timeout after accept")
		},
	}
	task := &scheduler.ScheduledTask{ID: "task-2"}
	if err := fireManagedScheduleDispatch(context.Background(), deps, task, "hello", nil); err == nil {
		t.Fatal("unknown send must surface the transport error")
	}
	if sent != 1 {
		t.Fatalf("sent=%d", sent)
	}
	if err := fireManagedScheduleDispatch(context.Background(), deps, task, "hello", nil); err != nil {
		t.Fatal(err)
	}
	if sent != 1 {
		t.Fatalf("unknown must not auto-resend the same run: sent=%d", sent)
	}
}

func TestFireManagedScheduleDispatchSkipsUnboundAndDesktop(t *testing.T) {
	bindings := newScheduleDispatchBindingStore("")
	store := tool.NewMemoryArtifactStore()
	var sent int
	deps := scheduleDispatchFireDeps{
		Bindings: bindings,
		Store:    store,
		Send: func(context.Context, string, []scheduler.DeliveryTarget, string) error {
			sent++
			return nil
		},
	}
	if err := fireManagedScheduleDispatch(context.Background(), deps, &scheduler.ScheduledTask{ID: "missing"}, "text", nil); err != nil || sent != 0 {
		t.Fatalf("unbound sent=%d err=%v", sent, err)
	}
	if err := bindings.Put(scheduleDispatchBinding{
		TaskID: "desk", ChannelScope: "desktop", DestinationID: "group:ops", PrincipalID: "user",
	}); err != nil {
		t.Fatal(err)
	}
	if err := fireManagedScheduleDispatch(context.Background(), deps, &scheduler.ScheduledTask{ID: "desk"}, "text", nil); err != nil || sent != 0 {
		t.Fatalf("desktop fire must not SendMedia sent=%d err=%v", sent, err)
	}
	if err := fireManagedScheduleDispatch(context.Background(), deps, &scheduler.ScheduledTask{ID: "desk"}, "", nil); err != nil || sent != 0 {
		t.Fatalf("empty body sent=%d err=%v", sent, err)
	}
}

func TestIMSemanticScheduleDispatchBindsTrustedDestinationForFire(t *testing.T) {
	manager, err := scheduler.NewManager(t.TempDir() + "/scheduled_tasks.json")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Stop)
	h := &IMMessageHandler{registry: NewToolRegistry(), scheduledTaskManager: manager}
	registerBuiltinTools(h.registry, h)
	ctx := scheduleDispatchDestinationCtx("group:ops")
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithContextAndIdentityAndClassificationAndAttachments(
		ctx, "user", "send to the group every morning", "lansenger", "root-sched-bind", "turn-sched-bind", scheduleDispatchClassification(), nil,
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	var administerName, dispatchName string
	for name, grant := range surface.grants {
		if grant.AdapterName == semanticTrustedScheduleAdapter {
			administerName = name
		}
	}
	cb := &sharedAgentLoopCallbacks{
		handler: h, semanticSurface: surface, platform: "lansenger",
		loopCtx: &LoopContext{DeliveryTarget: &agent.DeliveryTarget{ChannelScope: "lansenger", DestinationID: "group:ops"}},
	}
	created := cb.ExecuteTool(administerName, `{"name":"brief","task_action":"remind","hour":9}`)
	if !strings.Contains(created, "定时任务已创建") {
		t.Fatalf("create=%q", created)
	}
	for name, grant := range surface.grants {
		if grant.AdapterName == "semantic_schedule_dispatch" {
			dispatchName = name
		}
	}
	if dispatchName == "" {
		t.Fatalf("dispatch grant missing: %#v", surface.grants)
	}
	if got := cb.ExecuteTool(dispatchName, `{}`); !strings.Contains(got, "not a send") {
		t.Fatalf("dispatch=%q", got)
	}
	tasks := manager.List()
	if len(tasks) != 1 || tasks[0].Delivery != nil {
		t.Fatalf("tasks=%#v", tasks)
	}
	binding, ok := h.scheduleDispatchBindingStore().Get(tasks[0].ID)
	if !ok || binding.ChannelScope != "lansenger" || binding.DestinationID != "group:ops" {
		t.Fatalf("binding=%#v ok=%v", binding, ok)
	}
	var sent int
	if err := fireManagedScheduleDispatch(context.Background(), scheduleDispatchFireDeps{
		Bindings: h.scheduleDispatchBindingStore(),
		Store:    tool.NewMemoryArtifactStore(),
		Send: func(context.Context, string, []scheduler.DeliveryTarget, string) error {
			sent++
			return nil
		},
	}, &tasks[0], "brief body", nil); err != nil {
		t.Fatal(err)
	}
	if sent != 1 {
		t.Fatalf("bound fire sent=%d", sent)
	}
	if tasks[0].Delivery != nil {
		t.Fatal("bound fire must not write Delivery")
	}
}

func TestFireManagedScheduleDispatchUsesCoordinatorCASAndStaleSettle(t *testing.T) {
	bindings := newScheduleDispatchBindingStore("")
	if err := bindings.Put(scheduleDispatchBinding{
		TaskID: "task-coord", ChannelScope: "lansenger", DestinationID: "group:ops", PrincipalID: "user-1",
	}); err != nil {
		t.Fatal(err)
	}
	coordinator, err := tool.NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = coordinator.Close() })
	var sent []string
	now := time.Date(2026, 8, 17, 9, 0, 0, 0, time.UTC)
	deps := scheduleDispatchFireDeps{
		Bindings:    bindings,
		Coordinator: coordinator,
		Now:         func() time.Time { return now },
		Send: func(_ context.Context, channel string, targets []scheduler.DeliveryTarget, text string) error {
			if channel != scheduler.DeliveryChannelLansenger || len(targets) != 1 || targets[0].GroupID != "ops" {
				t.Fatalf("send channel=%s targets=%#v", channel, targets)
			}
			sent = append(sent, text)
			return nil
		},
	}
	task := &scheduler.ScheduledTask{ID: "task-coord"}
	if err := fireManagedScheduleDispatch(context.Background(), deps, task, "standup notes", nil); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 || sent[0] != "standup notes" {
		t.Fatalf("first fire sent=%#v", sent)
	}
	if err := fireManagedScheduleDispatch(context.Background(), deps, task, "standup notes", nil); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 {
		t.Fatalf("same occurrence must not resend: %#v", sent)
	}
	scope := tool.InvocationScope{
		RootTaskID: "schedule-fire:task-coord", PlanID: "run:" + now.Format("20060102T150405.000000000Z"),
		SessionID: "user-1", TurnID: "fire:" + now.Format("20060102T150405.000000000Z"), PrincipalID: "user-1",
	}
	if _, err := coordinator.SettleStandaloneDelivery(scope, scheduleDispatchFireSelectionID, tool.DeliveryAccepted, "late-receipt", "late", now); err == nil || !strings.Contains(err.Error(), "delivery_outcome_conflict") && !strings.Contains(err.Error(), "delivery_fencing_stale") {
		if err == nil {
			t.Fatal("late accepted settle after terminal unknown must not succeed")
		}
	}
	now = now.Add(24 * time.Hour)
	if err := fireManagedScheduleDispatch(context.Background(), deps, task, "next day", nil); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 2 || sent[1] != "next day" {
		t.Fatalf("next occurrence sent=%#v", sent)
	}
}

func TestFireManagedScheduleDispatchReconcileExpiredLeaseDoesNotResend(t *testing.T) {
	bindings := newScheduleDispatchBindingStore("")
	if err := bindings.Put(scheduleDispatchBinding{
		TaskID: "task-lease", ChannelScope: "lansenger", DestinationID: "user:u1", PrincipalID: "user-1",
	}); err != nil {
		t.Fatal(err)
	}
	coordinator, err := tool.NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution-lease.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = coordinator.Close() })
	now := time.Date(2026, 8, 17, 11, 0, 0, 0, time.UTC)
	scope := tool.InvocationScope{
		RootTaskID: "schedule-fire:task-lease", PlanID: "run:" + now.Format("20060102T150405.000000000Z"),
		SessionID: "user-1", TurnID: "fire:" + now.Format("20060102T150405.000000000Z"), PrincipalID: "user-1",
	}
	payload, err := tool.NewArtifactPayload(scope, scheduleDispatchFireSelectionID, "document", "text/plain", "aGVsbG8=", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.Artifacts.Publish(payload); err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.PrepareStandaloneDelivery(tool.DeliveryRecord{
		Scope: scope, SelectionID: scheduleDispatchFireSelectionID, ArtifactID: payload.Ref.ID, ArtifactSourceScope: payload.Ref.Scope,
		ChannelScope: "lansenger", DestinationID: "user:u1", State: tool.DeliveryPrepared, CreatedAt: now,
	}, now); err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := coordinator.ClaimDelivery(scope, scheduleDispatchFireSelectionID, now); err != nil || !claimed {
		t.Fatalf("claim claimed=%v err=%v", claimed, err)
	}
	if n, err := coordinator.ReconcileStaleDeliveryDispatches(now.Add(time.Hour), time.Minute); err != nil || n < 1 {
		t.Fatalf("reconcile n=%d err=%v", n, err)
	}
	record, err := coordinator.Artifacts.Delivery(scope, scheduleDispatchFireSelectionID)
	if err != nil || record.State != tool.DeliveryUnknown {
		t.Fatalf("expired lease must be unknown: %#v err=%v", record, err)
	}
	var sent int
	if err := fireManagedScheduleDispatch(context.Background(), scheduleDispatchFireDeps{
		Bindings: bindings, Coordinator: coordinator, Now: func() time.Time { return now },
		Send: func(context.Context, string, []scheduler.DeliveryTarget, string) error {
			sent++
			return nil
		},
	}, &scheduler.ScheduledTask{ID: "task-lease"}, "hello", nil); err != nil {
		t.Fatal(err)
	}
	if sent != 0 {
		t.Fatalf("reconciled unknown must not resend: sent=%d", sent)
	}
}
